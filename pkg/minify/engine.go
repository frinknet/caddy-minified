package minify

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/frinknet/caddy-minified/pkg/config"
	tdewolff "github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/svg"
)

var bufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

type stateKey struct{}

type requestState struct {
	disabled bool
	applied  bool
}

type Engine struct {
	cfg      *config.Config
	minifier *tdewolff.M
}

func New(cfg *config.Config) *Engine {
	cfg = config.OrDefault(cfg)
	cfg.Normalize()

	m := tdewolff.New()
	m.Add("text/html", &html.Minifier{
		KeepComments:        cfg.HTML.KeepComments,
		KeepSpecialComments: cfg.HTML.KeepSpecialComments,
		KeepDefaultAttrVals: cfg.HTML.KeepDefaultAttrVals,
		KeepDocumentTags:    cfg.HTML.KeepDocumentTags,
		KeepEndTags:         cfg.HTML.KeepEndTags,
		KeepQuotes:          cfg.HTML.KeepQuotes,
		KeepWhitespace:      cfg.HTML.KeepWhitespace,
	})
	m.AddFunc("application/xhtml+xml", html.Minify)
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/javascript", js.Minify)
	m.AddFunc("application/javascript", js.Minify)
	m.AddFunc("application/x-javascript", js.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)

	return &Engine{cfg: cfg, minifier: m}
}

func (e *Engine) Handle(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.Method == http.MethodHead {
		return next.ServeHTTP(w, r)
	}

	req, state := stateFor(r)

	if !e.cfg.Enabled {
		state.disabled = true
		return next.ServeHTTP(w, req)
	}
	if state.disabled || state.applied {
		return next.ServeHTTP(w, req)
	}
	state.applied = true

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		buf.Reset()
		bufferPool.Put(buf)
	}()

	rec := caddyhttp.NewResponseRecorder(w, buf, e.shouldBuffer)
	if err := next.ServeHTTP(rec, req); err != nil {
		return err
	}
	if !rec.Buffered() {
		return nil
	}

	if state.disabled {
		return rec.WriteResponse()
	}

	original := buf.Bytes()
	if len(original) == 0 {
		return rec.WriteResponse()
	}

	mediaType := config.MediaType(w.Header().Get("Content-Type"))
	if mediaType == "" {
		mediaType = config.MediaType(http.DetectContentType(original))
	}
	if !e.cfg.Supports(mediaType) {
		return rec.WriteResponse()
	}

	minified, err := e.minifier.Bytes(mediaType, original)
	if err != nil || len(minified) >= len(original) {
		return rec.WriteResponse()
	}

	header := w.Header()
	header.Del("Content-Length")
	header.Del("Content-MD5")
	header.Del("ETag")
	header.Del("Accept-Ranges")

	w.WriteHeader(rec.Status())
	_, err = w.Write(minified)
	return err
}

func stateFor(r *http.Request) (*http.Request, *requestState) {
	if state, ok := r.Context().Value(stateKey{}).(*requestState); ok {
		return r, state
	}
	state := new(requestState)
	return r.WithContext(context.WithValue(r.Context(), stateKey{}, state)), state
}

func (e *Engine) shouldBuffer(status int, header http.Header) bool {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return false
	}
	switch status {
	case http.StatusNoContent, http.StatusResetContent, http.StatusPartialContent:
		return false
	}
	if header.Get("Upgrade") != "" || header.Get("Content-Range") != "" {
		return false
	}
	if encoding := strings.TrimSpace(strings.ToLower(header.Get("Content-Encoding"))); encoding != "" && encoding != "identity" {
		return false
	}
	if strings.Contains(strings.ToLower(header.Get("Cache-Control")), "no-transform") {
		return false
	}
	return e.cfg.Supports(header.Get("Content-Type"))
}
