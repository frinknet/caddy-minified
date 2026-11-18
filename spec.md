> Historical design notes. `README.md` and the code under `pkg/` are the
> authoritative description of the current implementation.

Here is the complete specification, including the implementation for the missing `pkg/minify/engine.go` package, and practical Caddyfile examples showing how your global options block and per-route toggle work together.

---

### Complete Directory Layout

```text
caddy-minified/
├── Dockerfile
├── LICENSE
├── README.md
├── build.sh
├── go.mod
├── go.sum
├── minified.go            # Core Caddy module registration & middleware entrypoint
└── pkg
    ├── config
    │   ├── config.go      # Configuration structs
    │   └── caddyfile.go   # Global options & route directive parsers
    └── minify
        └── engine.go      # Response stream interception & tdewolff/minify logic

```

---

### 1. The Minification Engine (`pkg/minify/engine.go`)

This package wraps `tdewolff/minify`, configures HTML options (like keeping comments or quotes), intercepts the HTTP response writer buffer, and minifies based on the response's `Content-Type`.

```go
package minify

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/yourusername/caddy-minified/pkg/config"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/svg"
)

// HandleResponse intercepts the response stream and applies minification rules
func HandleResponse(w http.ResponseWriter, r *http.Request, next caddyhttp.HandlerChain, cfg *config.Config) error {
	if cfg == nil || !cfg.Enabled {
		return next.ServeHTTP(w, r)
	}

	rw := &responseWriterWrapper{
		ResponseWriter: w,
		buf:            bytes.NewBuffer(nil),
		statusCode:     http.StatusOK,
	}

	err := next.ServeHTTP(rw, r)
	if err != nil {
		return err
	}

	// Setup tdewolff minifier instance
	m := minify.New()

	// Configure HTML minifier struct rules based on config
	htmlMinifier := &html.Minifier{
		KeepComments: cfg.KeepComments,
		KeepQuotes:   cfg.KeepQuotes,
	}
	m.Add("text/html", htmlMinifier)
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/javascript", js.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)

	contentType := rw.Header().Get("Content-Type")
	originalBody := rw.buf.Bytes()
	minifiedBody := originalBody

	var mediaType string
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		mediaType = strings.TrimSpace(contentType[:idx])
	} else {
		mediaType = strings.TrimSpace(contentType)
	}

	// Run minification if format is supported
	if b, err := m.Bytes(mediaType, originalBody); err == nil {
		minifiedBody = b
		// Clean up Content-Length since body size changed
		rw.Header().Del("Content-Length")
	}

	w.WriteHeader(rw.statusCode)
	_, err = w.Write(minifiedBody)
	return err
}

type responseWriterWrapper struct {
	http.ResponseWriter
	buf        *bytes.Buffer
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(code int) {
	w.statusCode = code
}

func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

```

---

### 2. Updating `minified.go` to Call the Engine

Wire up your root entrypoint to pass the route configuration into `minify.HandleResponse`:

```go
package caddyminified

import (
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/yourusername/caddy-minified/pkg/config"
	"github.com/yourusername/caddy-minified/pkg/minify"
)

func init() {
	caddy.RegisterModule(&MinifiedModule{})
}

// MinifiedModule implements the Caddy HTTP handler middleware
type MinifiedModule struct {
	Config *config.Config `json:"config,omitempty"`
}

func (m *MinifiedModule) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.minified",
		New: func() caddy.Module { return new(MinifiedModule) },
	}
}

func (m *MinifiedModule) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.HandlerChain) error {
	return minify.HandleResponse(w, r, next, m.Config)
}

var (
	_ caddyhttp.MiddlewareHandler = (*MinifiedModule)(nil)
)

```

---

### 3. Caddyfile Examples

With this architecture fully built and compiled via `xcaddy`, your users can write clean Caddyfiles matching the design you wanted:

#### Example A: Global Options with Custom HTML Tweaks & Route Toggles

```caddyfile
{
    minified {
        html {
            KeepComments
            KeepQuotes
        }
        css
        svg
    }
}

example.com {
    # Turn minification on explicitly for this host block
    minified on

    reverse_proxy localhost:8080 {
        header_up Accept-Encoding identity
    }
}

```

#### Example B: Disabling Minification on Specific Routes (e.g., Admin Dashboards)

```caddyfile
example.com {
    # Enable by default
    minified on

    # Disable minification specifically for admin panel API or dynamic pages
    route /admin/* {
        minified off
        reverse_proxy localhost:9000
    }

    reverse_proxy localhost:8080 {
        header_up Accept-Encoding identity
    }
}

```
