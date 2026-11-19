package minify

import (
	"io"
	"net/http"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/frinknet/caddy-minified/pkg/config"
	tdewolff "github.com/tdewolff/minify/v2"
)

const sniffLen = 512

type streamMode int

const (
	modeUndecided streamMode = iota
	modePass
	modeSniff
	modeMinify
)

func (e *Engine) handleStreaming(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler, state *requestState) error {
	sw := &streamWriter{
		cfg:      e.cfg,
		minifier: e.minifier,
		state:    state,
		w:        w,
		mode:     modeUndecided,
	}
	if err := next.ServeHTTP(sw, r); err != nil {
		sw.abort()
		return err
	}
	return sw.Close()
}

type streamWriter struct {
	cfg      *config.Config
	minifier *tdewolff.M
	state    *requestState
	w        http.ResponseWriter

	mode     streamMode
	status   int
	decided  bool
	pending  []byte
	pipe     *io.PipeWriter
	finished chan error
	err      error
}

func (sw *streamWriter) Header() http.Header {
	return sw.w.Header()
}

func (sw *streamWriter) WriteHeader(status int) {
	if sw.decided {
		return
	}
	sw.decided = true
	sw.status = status

	header := sw.w.Header()
	if !sw.cfg.Enabled || sw.state.disabled || !eligibleResponse(status, header) {
		sw.mode = modePass
		sw.w.WriteHeader(status)
		return
	}
	switch contentType := header.Get("Content-Type"); {
	case contentType == "":
		sw.mode = modeSniff
	case sw.cfg.Supports(contentType):
		sw.start(config.MediaType(contentType))
	default:
		sw.mode = modePass
		sw.w.WriteHeader(status)
	}
}

func (sw *streamWriter) Write(p []byte) (int, error) {
	if sw.err != nil {
		return 0, sw.err
	}
	if !sw.decided {
		sw.WriteHeader(http.StatusOK)
	}
	switch sw.mode {
	case modePass:
		return sw.w.Write(p)
	case modeSniff:
		sw.pending = append(sw.pending, p...)
		if len(sw.pending) < sniffLen {
			return len(p), nil
		}
		if err := sw.resolveSniff(); err != nil {
			return 0, err
		}
		return len(p), nil
	case modeMinify:
		return sw.pipe.Write(p)
	}
	return len(p), nil
}

func (sw *streamWriter) FlushError() error {
	if !sw.decided {
		sw.WriteHeader(http.StatusOK)
	}
	if sw.mode == modeSniff {
		if err := sw.resolveSniff(); err != nil {
			return err
		}
	}
	if sw.mode == modeMinify {
		return http.ErrNotSupported
	}
	return http.NewResponseController(sw.w).Flush()
}

func (sw *streamWriter) Unwrap() http.ResponseWriter {
	return sw.w
}

func (sw *streamWriter) Close() error {
	if sw.err != nil {
		return sw.err
	}
	if !sw.decided {
		sw.WriteHeader(http.StatusOK)
	}
	if sw.mode == modeSniff {
		if err := sw.resolveSniff(); err != nil {
			return err
		}
	}
	if sw.mode != modeMinify {
		return nil
	}
	closeErr := sw.pipe.Close()
	if err := <-sw.finished; err != nil {
		sw.err = err
		return err
	}
	sw.err = closeErr
	return closeErr
}

func (sw *streamWriter) abort() {
	if sw.pipe == nil {
		return
	}
	_ = sw.pipe.CloseWithError(io.ErrClosedPipe)
	<-sw.finished
	sw.err = io.ErrClosedPipe
}

func (sw *streamWriter) resolveSniff() error {
	detected := http.DetectContentType(sw.pending)
	body := sw.pending
	sw.pending = nil

	if sw.w.Header().Get("Content-Type") == "" {
		sw.w.Header().Set("Content-Type", detected)
	}
	mediaType := config.MediaType(detected)
	if !sw.cfg.Supports(mediaType) {
		sw.mode = modePass
		sw.w.WriteHeader(sw.status)
		if len(body) == 0 {
			return nil
		}
		_, err := sw.w.Write(body)
		return err
	}

	sw.start(mediaType)
	if len(body) == 0 {
		return nil
	}
	_, err := sw.pipe.Write(body)
	return err
}

func (sw *streamWriter) start(mediaType string) {
	header := sw.w.Header()
	header.Del("Content-Length")
	header.Del("Content-MD5")
	header.Del("Accept-Ranges")

	sw.mode = modeMinify
	sw.w.WriteHeader(sw.status)

	pr, pw := io.Pipe()
	sw.pipe = pw
	sw.finished = make(chan error, 1)
	go func() {
		defer pr.Close()
		sw.finished <- sw.minifier.Minify(mediaType, sw.w, pr)
	}()
}
