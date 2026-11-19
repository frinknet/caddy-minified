package minify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	if tag := rec.Header().Get("ETag"); tag != `"origin"` {
		t.Fatalf("minification must keep the origin validator, got %q", tag)
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

func TestConcurrentRequestsShareEngine(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
	}{
		{"buffered", func() *config.Config { cfg := config.Default(); cfg.Enabled = true; return cfg }()},
		{"streaming", streamingConfig()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := New(tc.cfg)
			var wg sync.WaitGroup
			for worker := 0; worker < 16; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for round := 0; round < 32; round++ {
						req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
						rec := httptest.NewRecorder()
						if err := engine.Handle(rec, req, responder("text/html; charset=utf-8", sampleHTML)); err != nil {
							t.Errorf("handle: %v", err)
							return
						}
						if rec.Body.String() != `<p class=greeting>Hello` {
							t.Errorf("unexpected body: %q", rec.Body.String())
							return
						}
					}
				}()
			}
			wg.Wait()
		})
	}
}

func conditionalHandler(forwarded *[]string) caddyhttp.Handler {
	return caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		ifNoneMatch := r.Header.Get("If-None-Match")
		*forwarded = append(*forwarded, ifNoneMatch)
		w.Header().Set("ETag", `"origin"`)
		if ifNoneMatch == `"origin"` {
			w.WriteHeader(http.StatusNotModified)
			return nil
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, err := w.Write([]byte(sampleHTML))
		return err
	})
}

func TestETagRoundTrip(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	engine := New(cfg)

	var forwarded []string
	handler := conditionalHandler(&forwarded)

	first := httptest.NewRecorder()
	if err := engine.Handle(first, httptest.NewRequest(http.MethodGet, "http://example.test/", nil), handler); err != nil {
		t.Fatal(err)
	}
	tag := first.Header().Get("ETag")
	if tag != `"origin"` {
		t.Fatalf("minification must keep the origin validator, got %q", tag)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("If-None-Match", tag)
	second := httptest.NewRecorder()
	if err := engine.Handle(second, req, handler); err != nil {
		t.Fatal(err)
	}
	if len(forwarded) != 2 || forwarded[1] != `"origin"` {
		t.Fatalf("origin validator was not forwarded for conditional requests: %v", forwarded)
	}
	if second.Code != http.StatusNotModified {
		t.Fatalf("unexpected revalidation status: %d", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("304 response carried a body: %q", second.Body.String())
	}
	if got := second.Header().Get("ETag"); got != tag {
		t.Fatalf("304 response lost the origin validator: %q", got)
	}
}

func TestETagKeptForUntouchedResponses(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"origin"`)
		_, err := w.Write([]byte("plain text\n"))
		return err
	})
	rec := run(t, cfg, http.MethodGet, handler)
	if got := rec.Header().Get("ETag"); got != `"origin"` {
		t.Fatalf("untouched response must keep the origin validator: %q", got)
	}
}

func TestETagAbsentWhenOriginHasNone(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		_, err := w.Write([]byte(sampleHTML))
		return err
	})
	rec := run(t, cfg, http.MethodGet, handler)
	if got := rec.Header().Get("ETag"); got != "" {
		t.Fatalf("handler must not invent a validator: %q", got)
	}
}

func TestETagKeptForUntransformableNotModified(t *testing.T) {
	cfg := config.Default()
	cfg.Enabled = true
	cases := []struct {
		name   string
		header http.Header
	}{
		{"unsupported media type", http.Header{"Content-Type": {"text/plain"}}},
		{"no-transform", http.Header{"Content-Type": {"text/html"}, "Cache-Control": {"no-transform"}}},
		{"encoded", http.Header{"Content-Type": {"text/html"}, "Content-Encoding": {"gzip"}}},
		{"missing media type", http.Header{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := tc.header.Clone()
			header.Set("ETag", `"origin"`)
			handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
				for key, values := range header {
					w.Header()[key] = values
				}
				w.WriteHeader(http.StatusNotModified)
				return nil
			})
			rec := run(t, cfg, http.MethodGet, handler)
			if rec.Code != http.StatusNotModified {
				t.Fatalf("unexpected status: %d", rec.Code)
			}
			if got := rec.Header().Get("ETag"); got != `"origin"` {
				t.Fatalf("304 must keep the origin validator: %q", got)
			}
		})
	}
}
