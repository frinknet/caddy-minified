package caddyminified

import (
	"net/http"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/frinknet/caddy-minified/pkg/config"
	"github.com/frinknet/caddy-minified/pkg/minify"
)

func init() {
	caddy.RegisterModule(new(Minified))
	httpcaddyfile.RegisterGlobalOption("minified", config.ParseGlobalOption)
	httpcaddyfile.RegisterHandlerDirective("minified", parseCaddyfile)
	httpcaddyfile.RegisterDirectiveOrder("minified", httpcaddyfile.After, "encode")
}

type Minified struct {
	Config *config.Config `json:"config,omitempty"`

	engine     *minify.Engine
	engineOnce sync.Once
}

func (*Minified) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.minified",
		New: func() caddy.Module { return new(Minified) },
	}
}

func (m *Minified) Provision(caddy.Context) error {
	m.Config = config.OrDefault(m.Config)
	m.Config.Normalize()
	return m.Config.Validate()
}

func (m *Minified) Validate() error {
	if m.Config == nil {
		return nil
	}
	return m.Config.Validate()
}

func (m *Minified) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	m.engineOnce.Do(func() {
		m.engine = minify.New(m.Config)
	})
	return m.engine.Handle(w, r, next)
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	cfg, err := config.ParseDirective(h.Dispenser, h.Option("minified"))
	if err != nil {
		return nil, err
	}
	return &Minified{Config: cfg}, nil
}

var (
	_ caddy.Module                = (*Minified)(nil)
	_ caddy.Provisioner           = (*Minified)(nil)
	_ caddy.Validator             = (*Minified)(nil)
	_ caddyhttp.MiddlewareHandler = (*Minified)(nil)
)
