package minify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/frinknet/caddy-minified/pkg/config"
)

const sampleHTML = "<html>\n  <body>\n    <p class=\"greeting\">Hello</p>\n  </body>\n</html>"

func run(t *testing.T, cfg *config.Config, method string, handler caddyhttp.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://example.test/", nil)
	rec := httptest.NewRecorder()
	if err := New(cfg).Handle(rec, req, handler); err != nil {
		t.Fatal(err)
	}
	return rec
}

func responder(contentType, body string) caddyhttp.Handler {
	return caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", "999")
		w.Header().Set("ETag", `"origin"`)
		_, err := w.Write([]byte(body))
		return err
	})
}

func TestMinifiesHTML(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	rec := run(t, cfg, http.MethodGet, responder("text/html; charset=utf-8", sampleHTML))

	body := rec.Body.String()
	if body != `<p class=greeting>Hello` {
		t.Fatalf("unexpected minified body: %q", body)
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must be removed after minification")
	}
	if rec.Header().Get("ETag") != "" {
		t.Fatal("ETag must be removed after minification")
	}
}

func TestKeepsQuotesAndComments(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.HTML.KeepQuotes = true
	cfg.HTML.KeepComments = true

	input := `<html><!-- keep --><body><p class="greeting">Hello</p></body></html>`
	rec := run(t, cfg, http.MethodGet, responder("text/html", input))
	if rec.Body.String() != `<!-- keep --><p class="greeting">Hello` {
		t.Fatalf("expected comments and quotes to be kept, got %q", rec.Body.String())
	}
}

func TestDisabledPassesThrough(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = false
	rec := run(t, cfg, http.MethodGet, responder("text/html", sampleHTML))
	if rec.Body.String() != sampleHTML {
		t.Fatalf("disabled handler changed the body: %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "999" {
		t.Fatal("disabled handler changed response headers")
	}
}

func TestNestedOffDisablesEnclosingOn(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	off := New(&config.Config{Enabled: false})

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return off.Handle(w, r, responder("text/html", sampleHTML))
	})
	rec := run(t, cfg, http.MethodGet, handler)
	if rec.Body.String() != sampleHTML {
		t.Fatalf("nested off must preserve the body: %q", rec.Body.String())
	}
}

func TestOuterOffDisablesInnerOn(t *testing.T) {
	on := New(&config.Config{Enabled: true})
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return on.Handle(w, r, responder("text/html", sampleHTML))
	})
	rec := run(t, &config.Config{Enabled: false}, http.MethodGet, handler)
	if rec.Body.String() != sampleHTML {
		t.Fatalf("outer off must preserve the body: %q", rec.Body.String())
	}
}

func TestNestedOnAppliesOnlyOnce(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	inner := New(cfg)

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return inner.Handle(w, r, responder("text/html", sampleHTML))
	})
	rec := run(t, cfg, http.MethodGet, handler)
	if rec.Body.String() != `<p class=greeting>Hello` {
		t.Fatalf("unexpected nested result: %q", rec.Body.String())
	}
}

func TestSkipsEncodedResponses(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		_, err := w.Write([]byte(sampleHTML))
		return err
	})
	rec := run(t, cfg, http.MethodGet, handler)
	if rec.Body.String() != sampleHTML {
		t.Fatalf("encoded response was modified: %q", rec.Body.String())
	}
}

func TestSkipsNoTransform(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "public, no-transform")
		_, err := w.Write([]byte(sampleHTML))
		return err
	})
	rec := run(t, cfg, http.MethodGet, handler)
	if rec.Body.String() != sampleHTML {
		t.Fatalf("no-transform response was modified: %q", rec.Body.String())
	}
}

func TestSkipsUnsupportedTypes(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	body := `{"hello": "world"}`
	rec := run(t, cfg, http.MethodGet, responder("application/json", body))
	if rec.Body.String() != body {
		t.Fatalf("json response was modified: %q", rec.Body.String())
	}
}

func TestMinifiesJSON(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.JSON = true

	body := "{\n  \"name\": \"caddy-minified\",\n  \"tags\": [1, 2]\n}"
	rec := run(t, cfg, http.MethodGet, responder("application/json", body))
	if rec.Body.String() != `{"name":"caddy-minified","tags":[1,2]}` {
		t.Fatalf("unexpected minified json: %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must be removed after json minification")
	}

	suffixed := run(t, cfg, http.MethodGet, responder("application/ld+json", body))
	if suffixed.Body.String() != `{"name":"caddy-minified","tags":[1,2]}` {
		t.Fatalf("unexpected minified json-suffixed body: %q", suffixed.Body.String())
	}
}

func TestSkipsJSONWhenNotEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	body := `{"hello": "world"}`
	rec := run(t, cfg, http.MethodGet, responder("application/json", body))
	if rec.Body.String() != body {
		t.Fatalf("json should be untouched without the json format: %q", rec.Body.String())
	}
}

func TestMinifiesXML(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.XML = true

	body := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<catalog>\n  <book id=\"1\">\n    <title> Caddy </title>\n  </book>\n</catalog>\n"
	rec := run(t, cfg, http.MethodGet, responder("application/xml", body))
	want := `<?xml version="1.0" encoding="UTF-8"?><catalog><book id="1"><title>Caddy</title></book></catalog>`
	if rec.Body.String() != want {
		t.Fatalf("unexpected minified xml: %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must be removed after xml minification")
	}
}

func TestXMLKeepsHTMLAndSVGPriority(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.XML = true

	xhtml := run(t, cfg, http.MethodGet, responder("application/xhtml+xml", sampleHTML))
	if xhtml.Body.String() != `<p class=greeting>Hello` {
		t.Fatalf("xhtml must still use the html minifier: %q", xhtml.Body.String())
	}

	svg := run(t, cfg, http.MethodGet, responder("image/svg+xml", `<!-- c --><svg xmlns="http://www.w3.org/2000/svg">`+"\n  <rect width=\"10\" height=\"10\" />\n</svg>"))
	if strings.Contains(svg.Body.String(), "<!--") || strings.Contains(svg.Body.String(), "\n") {
		t.Fatalf("svg must still use the svg minifier: %q", svg.Body.String())
	}
}

func TestMinifiesRSSFeed(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.XML = true

	body := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<rss version=\"2.0\">\n  <channel>\n    <title> Example </title>\n    <item>\n      <title> Hello </title>\n      <link> https://example.test/ </link>\n    </item>\n  </channel>\n</rss>\n"
	rec := run(t, cfg, http.MethodGet, responder("application/rss+xml", body))
	if strings.Contains(rec.Body.String(), "\n") {
		t.Fatalf("rss feed was not minified: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<title>Example</title>") {
		t.Fatalf("unexpected rss body: %q", rec.Body.String())
	}
}

func TestSkipsXMLWhenNotEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	body := "<?xml version=\"1.0\"?>\n<note>\n  <to>You</to>\n</note>\n"
	rec := run(t, cfg, http.MethodGet, responder("application/xml", body))
	if rec.Body.String() != body {
		t.Fatalf("xml should be untouched without the xml format: %q", rec.Body.String())
	}

	rss := run(t, cfg, http.MethodGet, responder("application/rss+xml", body))
	if rss.Body.String() != body {
		t.Fatalf("rss should be untouched without the xml format: %q", rss.Body.String())
	}
}

func TestSkipsBodylessStatuses(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	engine := New(cfg)
	header := http.Header{"Content-Type": []string{"text/html"}}
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent, http.StatusPartialContent, http.StatusNotModified} {
		if engine.shouldBuffer(status, header) {
			t.Fatalf("status %d must not be buffered", status)
		}
	}
}

func TestSkipsHead(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	rec := run(t, cfg, http.MethodHead, responder("text/html", sampleHTML))
	if rec.Body.String() != sampleHTML {
		t.Fatalf("HEAD response should pass through unchanged, got %q", rec.Body.String())
	}
}

func TestMinifiesCSSAndSVG(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true

	css := run(t, cfg, http.MethodGet, responder("text/css", "/* comment */\nbody {\n  color: red;\n}\n"))
	if css.Body.String() != "body{color:red}" {
		t.Fatalf("unexpected css: %q", css.Body.String())
	}

	svg := run(t, cfg, http.MethodGet, responder("image/svg+xml", `<!-- c --><svg xmlns="http://www.w3.org/2000/svg">`+"\n  <rect width=\"10\" height=\"10\" />\n</svg>"))
	if strings.Contains(svg.Body.String(), "<!--") || strings.Contains(svg.Body.String(), "\n") {
		t.Fatalf("unexpected svg: %q", svg.Body.String())
	}
}

func TestRespectsFormatSelection(t *testing.T) {
	cfg := &config.Config{Enabled: true, HTML: config.HTML{Enabled: true}}
	body := "/* comment */\nbody { color: red; }\n"
	rec := run(t, cfg, http.MethodGet, responder("text/css", body))
	if rec.Body.String() != body {
		t.Fatalf("css should be disabled: %q", rec.Body.String())
	}
}

func TestEmptyBody(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		return nil
	})
	rec := run(t, cfg, http.MethodGet, handler)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestPropagatesHandlerError(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	want := http.ErrHandlerTimeout
	handler := caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error {
		return want
	})
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err := New(cfg).Handle(httptest.NewRecorder(), req, handler); err != want {
		t.Fatalf("expected handler error, got %v", err)
	}
}

func limitedConfig(limit int64, policy string) *config.Config {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.MaxBodySize = limit
	cfg.MaxBodySizePolicy = policy
	return cfg
}

func TestBufferLimitPassesThroughLargeBody(t *testing.T) {
	body := strings.Repeat("  a  ", 100)
	rec := run(t, limitedConfig(64, ""), http.MethodGet, responder("text/html", body))
	if rec.Body.String() != body {
		t.Fatalf("large body was modified: %q", rec.Body.String())
	}
	if rec.Header().Get("ETag") != `"origin"` {
		t.Fatal("passthrough must keep the original headers")
	}
}

func TestBufferLimitStreamsAcrossWrites(t *testing.T) {
	chunks := []string{"<html>  <body>", "  hello  ", "  world  ", "  </body>  </html>"}
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		for _, chunk := range chunks {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return err
			}
		}
		return nil
	})
	rec := run(t, limitedConfig(16, config.ExceedPass), http.MethodGet, handler)
	if rec.Body.String() != strings.Join(chunks, "") {
		t.Fatalf("streamed body was modified: %q", rec.Body.String())
	}
}

func TestBufferLimitRejectsLargeBody(t *testing.T) {
	cfg := limitedConfig(16, config.ExceedReject)
	rec := run(t, cfg, http.MethodGet, responder("text/html", sampleHTML))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if rec.Body.String() != "response body exceeds the configured minified buffer limit\n" {
		t.Fatalf("unexpected rejection body: %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("ETag") != "" {
		t.Fatal("rejection must remove validators")
	}
}

func TestBufferLimitMinifiesSmallBody(t *testing.T) {
	rec := run(t, limitedConfig(1024, config.ExceedReject), http.MethodGet, responder("text/html", sampleHTML))
	if rec.Body.String() != `<p class=greeting>Hello` {
		t.Fatalf("small body was not minified: %q", rec.Body.String())
	}
}

func TestBufferLimitIgnoresIneligibleResponses(t *testing.T) {
	body := `{"hello": "world"}`
	rec := run(t, limitedConfig(4, config.ExceedReject), http.MethodGet, responder("application/json", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("ineligible response was rejected: %d", rec.Code)
	}
	if rec.Body.String() != body {
		t.Fatalf("ineligible response was modified: %q", rec.Body.String())
	}
}

func TestBufferLimitDoesNotRejectNestedOff(t *testing.T) {
	off := New(&config.Config{Enabled: false})
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return off.Handle(w, r, responder("text/html", sampleHTML))
	})
	rec := run(t, limitedConfig(16, config.ExceedReject), http.MethodGet, handler)
	if rec.Code != http.StatusOK {
		t.Fatalf("nested off was rejected by the enclosing limit: %d", rec.Code)
	}
	if rec.Body.String() != sampleHTML {
		t.Fatalf("nested off body was modified: %q", rec.Body.String())
	}
}

func streamingConfig() *config.Config {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.Streaming = true
	return cfg
}

func TestStreamingMinifiesHTML(t *testing.T) {
	cfg := streamingConfig()
	rec := run(t, cfg, http.MethodGet, responder("text/html; charset=utf-8", sampleHTML))

	body := rec.Body.String()
	if body != `<p class=greeting>Hello` {
		t.Fatalf("unexpected streamed body: %q", body)
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must be removed while streaming")
	}
	if rec.Header().Get("ETag") != "" {
		t.Fatal("ETag must be removed while streaming")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestStreamingMatchesBufferedOutput(t *testing.T) {
	chunks := []string{"<html>\n  <body>\n", "    <p class=\"greeting\">Hello</p>\n", "  </body>\n</html>\n"}
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		for _, chunk := range chunks {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return err
			}
		}
		return nil
	})

	buffered := run(t, func() *config.Config { cfg := streamingConfig(); cfg.Streaming = false; return cfg }(), http.MethodGet, handler)
	streamed := run(t, streamingConfig(), http.MethodGet, handler)
	if streamed.Body.String() != buffered.Body.String() {
		t.Fatalf("streamed output %q differs from buffered output %q", streamed.Body.String(), buffered.Body.String())
	}
}

func TestStreamingPassesThroughIneligibleResponses(t *testing.T) {
	cfg := streamingConfig()

	noTransform := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cache-Control", "no-transform")
		_, err := w.Write([]byte(sampleHTML))
		return err
	})
	rec := run(t, cfg, http.MethodGet, noTransform)
	if rec.Body.String() != sampleHTML {
		t.Fatalf("no-transform response was modified: %q", rec.Body.String())
	}

	json := run(t, cfg, http.MethodGet, responder("application/json", `{"a": 1}`))
	if json.Body.String() != `{"a": 1}` {
		t.Fatalf("json response was modified: %q", json.Body.String())
	}

	notModified := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotModified)
		return nil
	})
	rec = run(t, cfg, http.MethodGet, notModified)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("bodyless status wrote a body: %q", rec.Body.String())
	}
}

func TestStreamingHonorsHead(t *testing.T) {
	rec := run(t, streamingConfig(), http.MethodHead, responder("text/html", sampleHTML))
	if rec.Body.String() != sampleHTML {
		t.Fatalf("HEAD response was modified: %q", rec.Body.String())
	}
}

func TestStreamingHonorsNestedOff(t *testing.T) {
	off := New(&config.Config{Enabled: false})
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return off.Handle(w, r, responder("text/html", sampleHTML))
	})
	rec := run(t, streamingConfig(), http.MethodGet, handler)
	if rec.Body.String() != sampleHTML {
		t.Fatalf("nested off body was modified: %q", rec.Body.String())
	}
	if rec.Header().Get("ETag") != `"origin"` {
		t.Fatal("nested off must keep the original headers")
	}
}

func TestStreamingAppliesOnlyOnce(t *testing.T) {
	cfg := streamingConfig()
	outer := New(cfg)
	inner := New(cfg)

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return inner.Handle(w, r, responder("text/html", sampleHTML))
	})
	rec := run(t, cfg, http.MethodGet, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		return inner.Handle(w, r, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			return outer.Handle(w, r, handler)
		}))
	}))
	if rec.Body.String() != `<p class=greeting>Hello` {
		t.Fatalf("nested handlers must apply once: %q", rec.Body.String())
	}
}

func TestStreamingSniffsShortBodies(t *testing.T) {
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		_, err := w.Write([]byte(sampleHTML))
		return err
	})
	rec := run(t, streamingConfig(), http.MethodGet, handler)
	if rec.Body.String() != `<p class=greeting>Hello` {
		t.Fatalf("short body was not minified: %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("unexpected sniffed content type: %q", got)
	}
}

func TestStreamingSniffsLongBodies(t *testing.T) {
	body := strings.Repeat("  <p>hello</p>\n", 100)
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		_, err := io.WriteString(w, body)
		return err
	})
	rec := run(t, streamingConfig(), http.MethodGet, handler)
	if rec.Body.String() != strings.Repeat("<p>hello", 100) {
		t.Fatalf("long body was not minified: %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("unexpected sniffed content type: %q", got)
	}
}

func TestStreamingSniffsUnsupportedBodies(t *testing.T) {
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		_, err := w.Write([]byte("plain text body\n"))
		return err
	})
	rec := run(t, streamingConfig(), http.MethodGet, handler)
	if rec.Body.String() != "plain text body\n" {
		t.Fatalf("plain text body was modified: %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("unexpected sniffed content type: %q", got)
	}
}

func TestStreamingLargeBody(t *testing.T) {
	body := strings.Repeat("  <p>hello</p>\n", 50000)
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		chunk := len(body) / 50
		for i := 0; i < 50; i++ {
			if _, err := w.Write([]byte(body[i*chunk : (i+1)*chunk])); err != nil {
				return err
			}
		}
		return nil
	})
	rec := run(t, streamingConfig(), http.MethodGet, handler)
	if rec.Body.String() != strings.Repeat("<p>hello", 50000) {
		t.Fatalf("unexpected streamed size: %d", rec.Body.Len())
	}
}

func TestStreamingPropagatesHandlerError(t *testing.T) {
	want := http.ErrHandlerTimeout
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(sampleHTML)); err != nil {
			return err
		}
		return want
	})
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err := New(streamingConfig()).Handle(httptest.NewRecorder(), req, handler); err != want {
		t.Fatalf("expected handler error, got %v", err)
	}
}

func TestStreamingConfigPassesThroughWhenDisabled(t *testing.T) {
	cfg := streamingConfig()
	cfg.Enabled = false
	rec := run(t, cfg, http.MethodGet, responder("text/html", sampleHTML))
	if rec.Body.String() != sampleHTML {
		t.Fatalf("disabled streaming handler changed the body: %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "999" {
		t.Fatal("disabled streaming handler changed response headers")
	}
}
