package minify

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/frinknet/caddy-minified/pkg/config"
	tdewolff "github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/json"
	"github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
)

var (
	bufferPool    = sync.Pool{New: func() any { return new(bytes.Buffer) }}
	jsonMediaType = regexp.MustCompile(`^application/[^/]+\+json$`)
	xmlMediaType  = regexp.MustCompile(`^application/[^/]+\+xml$`)
)

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
	m.AddFunc("application/json", json.Minify)
	m.AddFuncRegexp(jsonMediaType, json.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)
	m.AddFunc("application/xml", xml.Minify)
	m.AddFunc("text/xml", xml.Minify)
	m.AddFuncRegexp(xmlMediaType, xml.Minify)

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

	if e.cfg.Streaming {
		return e.handleStreaming(w, req, next, state)
	}

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		buf.Reset()
		bufferPool.Put(buf)
	}()

	rec := caddyhttp.NewResponseRecorder(w, buf, func(status int, header http.Header) bool {
		if state.disabled {
			return false
		}
		return e.shouldBuffer(status, header)
	})
	var writer http.ResponseWriter = rec
	var limited *limitWriter
	if e.cfg.MaxBodySize > 0 {
		limited = &limitWriter{
			ResponseRecorder: rec,
			writer:           w,
			limit:            e.cfg.MaxBodySize,
			policy:           e.cfg.ExceedPolicy(),
		}
		writer = limited
	}

	if err := next.ServeHTTP(writer, req); err != nil {
		return err
	}
	if limited != nil && limited.stopped() {
		return nil
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

type limitWriter struct {
	caddyhttp.ResponseRecorder

	writer    http.ResponseWriter
	limit     int64
	policy    string
	buffered  int64
	streaming bool
	rejected  bool
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.rejected {
		return len(p), nil
	}
	if lw.streaming {
		return lw.writer.Write(p)
	}

	lw.ResponseRecorder.WriteHeader(http.StatusOK)
	if !lw.ResponseRecorder.Buffered() {
		lw.streaming = true
		return lw.ResponseRecorder.Write(p)
	}

	if lw.limit > 0 && lw.buffered+int64(len(p)) > lw.limit {
		if lw.policy == config.ExceedReject {
			return lw.reject(p)
		}
		return lw.passThrough(p)
	}

	n, err := lw.ResponseRecorder.Write(p)
	lw.buffered += int64(n)
	return n, err
}

func (lw *limitWriter) FlushError() error {
	if lw.rejected || lw.streaming {
		return http.NewResponseController(lw.writer).Flush()
	}
	return nil
}

func (lw *limitWriter) Unwrap() http.ResponseWriter {
	return lw.writer
}

func (lw *limitWriter) stopped() bool {
	return lw.streaming || lw.rejected
}

func (lw *limitWriter) passThrough(p []byte) (int, error) {
	lw.streaming = true
	status := lw.Status()
	if status == 0 {
		status = http.StatusOK
	}
	lw.writer.WriteHeader(status)

	buf := lw.Buffer()
	if buf.Len() > 0 {
		if _, err := lw.writer.Write(buf.Bytes()); err != nil {
			return 0, err
		}
		buf.Reset()
	}
	lw.buffered = 0

	return lw.writer.Write(p)
}

func (lw *limitWriter) reject(p []byte) (int, error) {
	lw.rejected = true
	lw.Buffer().Reset()
	lw.buffered = 0

	header := lw.Header()
	header.Del("Content-Length")
	header.Del("Content-MD5")
	header.Del("ETag")
	header.Del("Accept-Ranges")
	header.Set("Content-Type", "text/plain; charset=utf-8")

	lw.writer.WriteHeader(http.StatusRequestEntityTooLarge)
	if _, err := io.WriteString(lw.writer, "response body exceeds the configured minified buffer limit\n"); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (e *Engine) shouldBuffer(status int, header http.Header) bool {
	if !eligibleResponse(status, header) {
		return false
	}
	return e.cfg.Supports(header.Get("Content-Type"))
}

func eligibleResponse(status int, header http.Header) bool {
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
	return true
}
