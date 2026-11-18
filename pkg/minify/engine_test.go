package minify

import (
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
