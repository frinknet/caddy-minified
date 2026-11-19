package minify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/frinknet/caddy-minified/pkg/config"
)

type benchCase struct {
	name        string
	contentType string
	body        []byte
}

func benchCases() []benchCase {
	html := `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <title> benchmark </title>
` + strings.Repeat(`    <link rel="stylesheet" href="/assets/style.css">
`, 32) + `  </head>
  <body>
    <!-- removable comment -->
    <main class="card">
` + strings.Repeat(`      <p class="line">The quick brown fox jumps over the lazy dog.</p>
`, 64) + `    </main>
  </body>
</html>
`

	css := strings.Repeat(`/* removable comment */
.card {
  margin: 0px 0px 0px 0px;
  padding: 1rem;
  color: #ffffff;
}
`, 32)

	js := strings.Repeat(`// removable comment
function addTwoValues(firstArgument, secondArgument) {
  return firstArgument + secondArgument;
}
`, 32)

	svg := `<svg xmlns="http://www.w3.org/2000/svg" version="1.1">
` + strings.Repeat(`  <rect x="1" y="2" width="3" height="4" fill="#ffffff" />
`, 32) + `</svg>
`

	json := "{\n  \"items\": [" + strings.Repeat("\n    {\"name\": \"caddy\", \"enabled\": true},", 32) + "\n  ]\n}\n"

	xml := `<?xml version="1.0" encoding="UTF-8"?>
<catalog>
` + strings.Repeat(`  <book id="1">
    <title> Caddy </title>
  </book>
`, 32) + `</catalog>
`

	return []benchCase{
		{"html", "text/html; charset=utf-8", []byte(html)},
		{"css", "text/css; charset=utf-8", []byte(css)},
		{"js", "text/javascript; charset=utf-8", []byte(js)},
		{"svg", "image/svg+xml", []byte(svg)},
		{"json", "application/json", []byte(json)},
		{"xml", "application/xml", []byte(xml)},
	}
}

func benchEngine(cfg *config.Config, c benchCase, b *testing.B) {
	b.Helper()
	engine := New(cfg)
	respond := responder(c.contentType, string(c.body))
	b.SetBytes(int64(len(c.body)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		rec := httptest.NewRecorder()
		if err := engine.Handle(rec, req, respond); err != nil {
			b.Fatal(err)
		}
		if rec.Body.Len() == 0 {
			b.Fatal("benchmark response body is empty")
		}
		if rec.Body.Len() >= len(c.body) {
			b.Fatalf("benchmark case %s is not minified: %d >= %d", c.name, rec.Body.Len(), len(c.body))
		}
	}
}

func BenchmarkBuffered(b *testing.B) {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.JSON = true
	cfg.XML = true

	for _, c := range benchCases() {
		c := c
		b.Run(c.name, func(b *testing.B) {
			benchEngine(cfg, c, b)
		})
	}
}

func BenchmarkStreaming(b *testing.B) {
	cfg := config.Default()
	cfg.Enabled = true
	cfg.Streaming = true
	cfg.JSON = true
	cfg.XML = true

	for _, c := range benchCases() {
		c := c
		b.Run(c.name, func(b *testing.B) {
			benchEngine(cfg, c, b)
		})
	}
}

func BenchmarkSkipEncoded(b *testing.B) {
	cfg := config.Default()
	cfg.Enabled = true
	body := benchCases()[0].body
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		_, err := w.Write(body)
		return err
	})
	engine := New(cfg)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		rec := httptest.NewRecorder()
		if err := engine.Handle(rec, req, handler); err != nil {
			b.Fatal(err)
		}
		if rec.Body.Len() != len(body) {
			b.Fatalf("encoded response changed length: %d", rec.Body.Len())
		}
	}
}
