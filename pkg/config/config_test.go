package config

import (
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func parseGlobal(t *testing.T, input string) (*Config, error) {
	t.Helper()
	value, err := ParseGlobalOption(caddyfile.NewTestDispenser(input), nil)
	if err != nil {
		return nil, err
	}
	return value.(*Config), nil
}

func TestParseGlobalOption(t *testing.T) {
	cfg, err := parseGlobal(t, `minified {
		html {
			keep_comments
			keep_quotes true
			keep_whitespace false
		}
		css
		svg
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("global options must not enable the handler")
	}
	if !cfg.HTML.Enabled || !cfg.HTML.KeepComments || !cfg.HTML.KeepQuotes {
		t.Fatalf("unexpected html config: %+v", cfg.HTML)
	}
	if cfg.HTML.KeepWhitespace {
		t.Fatal("keep_whitespace false was not applied")
	}
	if !cfg.CSS || !cfg.SVG {
		t.Fatalf("css/svg should be enabled: %+v", cfg)
	}
	if cfg.JS {
		t.Fatal("js was not listed and must stay disabled")
	}
}

func TestParseGlobalOptionBareEnablesAllFormats(t *testing.T) {
	cfg, err := parseGlobal(t, "minified")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HTML.Enabled || !cfg.CSS || !cfg.JS || !cfg.SVG {
		t.Fatalf("bare global option should enable all formats: %+v", cfg)
	}
}

func TestParseGlobalOptionMergesExisting(t *testing.T) {
	first, err := parseGlobal(t, "minified {\ncss\n}")
	if err != nil {
		t.Fatal(err)
	}
	value, err := ParseGlobalOption(caddyfile.NewTestDispenser("minified {\nsvg\n}"), first)
	if err != nil {
		t.Fatal(err)
	}
	cfg := value.(*Config)
	if !cfg.CSS || !cfg.SVG || cfg.JS {
		t.Fatalf("expected merged css+svg config: %+v", cfg)
	}
}

func TestParseGlobalOptionErrors(t *testing.T) {
	if _, err := parseGlobal(t, "minified unexpected"); err == nil {
		t.Fatal("expected an argument error")
	}
	if _, err := parseGlobal(t, "minified {\nnope\n}"); err == nil {
		t.Fatal("expected an unknown option error")
	}
	if _, err := parseGlobal(t, "minified {\nhtml {\nnope\n}\n}"); err == nil {
		t.Fatal("expected an unknown html option error")
	}
	if _, err := parseGlobal(t, "minified {\nhtml {\nkeep_quotes maybe\n}\n}"); err == nil {
		t.Fatal("expected a boolean error")
	}
}

func parseDirective(t *testing.T, input string, global any) (*Config, error) {
	t.Helper()
	return ParseDirective(caddyfile.NewTestDispenser(input), global)
}

func TestParseDirective(t *testing.T) {
	global := &Config{CSS: true, HTML: HTML{KeepComments: true}}
	cfg, err := parseDirective(t, "minified on", global)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || !cfg.CSS || cfg.JS || !cfg.HTML.KeepComments {
		t.Fatalf("unexpected route config: %+v", cfg)
	}
	if global.Enabled {
		t.Fatal("route directive must not mutate the global config")
	}
}

func TestParseDirectiveOff(t *testing.T) {
	cfg, err := parseDirective(t, "minified off", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("expected minified off to disable the handler")
	}
}

func TestParseDirectiveDefaultsToOn(t *testing.T) {
	cfg, err := parseDirective(t, "minified", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Fatal("expected bare minified to enable the handler")
	}
}

func TestParseDirectiveErrors(t *testing.T) {
	if _, err := parseDirective(t, "minified sideways", nil); err == nil {
		t.Fatal("expected an invalid argument error")
	}
	if _, err := parseDirective(t, "minified on off", nil); err == nil {
		t.Fatal("expected too many arguments error")
	}
	if _, err := parseDirective(t, "minified on {\nfoo\n}", nil); err == nil {
		t.Fatal("expected an unexpected block error")
	}
}

func TestSupports(t *testing.T) {
	cfg := Default()
	for _, mediaType := range []string{
		"text/html; charset=utf-8",
		"application/xhtml+xml",
		"text/css",
		"text/javascript",
		"application/javascript",
		"application/x-javascript",
		"image/svg+xml",
	} {
		if !cfg.Supports(mediaType) {
			t.Errorf("expected %q to be supported", mediaType)
		}
	}
	if cfg.Supports("application/json") {
		t.Error("json must not be reported as supported")
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	cfg := Empty()
	cfg.Normalize()
	if !cfg.AnyFormat() {
		t.Fatal("normalize should enable all formats")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (*Config)(nil).Validate(); err == nil {
		t.Fatal("nil config must not validate")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	original := Default()
	copy := original.Clone()
	copy.CSS = false
	copy.HTML.KeepComments = true
	if !original.CSS {
		t.Fatal("clone mutated the original format flags")
	}
	if original.HTML.KeepComments {
		t.Fatal("clone mutated the original html options")
	}
}
