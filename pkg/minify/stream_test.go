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
	if tag := rec.Header().Get("ETag"); tag != `"origin"` {
		t.Fatalf("streaming must keep the origin validator, got %q", tag)
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

func TestETagStreamingRoundTrip(t *testing.T) {
	cfg := streamingConfig()

	var forwarded []string
	handler := conditionalHandler(&forwarded)

	engine := New(cfg)
	first := httptest.NewRecorder()
	if err := engine.Handle(first, httptest.NewRequest(http.MethodGet, "http://example.test/", nil), handler); err != nil {
		t.Fatal(err)
	}
	tag := first.Header().Get("ETag")
	if tag != `"origin"` {
		t.Fatalf("streaming must keep the origin validator, got %q", tag)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("If-None-Match", tag)
	second := httptest.NewRecorder()
	if err := engine.Handle(second, req, handler); err != nil {
		t.Fatal(err)
	}
	if len(forwarded) != 2 || forwarded[1] != `"origin"` {
		t.Fatalf("origin validator was not forwarded while streaming: %v", forwarded)
	}
	if second.Code != http.StatusNotModified {
		t.Fatalf("unexpected revalidation status: %d", second.Code)
	}
	if got := second.Header().Get("ETag"); got != tag {
		t.Fatalf("304 response lost the origin validator: %q", got)
	}
}
