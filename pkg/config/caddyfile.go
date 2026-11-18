package config

import (
	"strings"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func ParseGlobalOption(d *caddyfile.Dispenser, existing any) (any, error) {
	cfg := Empty()
	if current, ok := existing.(*Config); ok && current != nil {
		cfg = current.Clone()
	}

	if !d.Next() {
		return nil, d.ArgErr()
	}
	if d.NextArg() {
		return nil, d.Errf("the minified global option does not take arguments")
	}

	for d.NextBlock(0) {
		switch strings.ToLower(d.Val()) {
		case "html":
			cfg.HTML.Enabled = true
			if err := parseHTML(d, &cfg.HTML); err != nil {
				return nil, err
			}
		case "css":
			cfg.CSS = true
		case "js":
			cfg.JS = true
		case "svg":
			cfg.SVG = true
		default:
			return nil, d.Errf("unrecognized minified option %q", d.Val())
		}
	}

	cfg.Normalize()
	return cfg, nil
}

func ParseDirective(d *caddyfile.Dispenser, global any) (*Config, error) {
	cfg := Default()
	if shared, ok := global.(*Config); ok && shared != nil {
		cfg = shared.Clone()
	}

	if !d.Next() {
		return nil, d.ArgErr()
	}

	if d.NextArg() {
		switch strings.ToLower(d.Val()) {
		case "on", "true", "enable":
			cfg.Enabled = true
		case "off", "false", "disable":
			cfg.Enabled = false
		default:
			return nil, d.Errf("expected on or off, got %q", d.Val())
		}
		if d.NextArg() {
			return nil, d.Errf("the minified directive takes only one argument")
		}
	} else {
		cfg.Enabled = true
	}

	if d.NextBlock(0) {
		return nil, d.Errf("the minified directive does not take a block")
	}

	cfg.Normalize()
	return cfg, nil
}

func parseHTML(d *caddyfile.Dispenser, html *HTML) error {
	for d.NextBlock(1) {
		option := strings.ToLower(d.Val())
		var target *bool
		switch option {
		case "keep_comments":
			target = &html.KeepComments
		case "keep_special_comments":
			target = &html.KeepSpecialComments
		case "keep_default_attr_vals":
			target = &html.KeepDefaultAttrVals
		case "keep_document_tags":
			target = &html.KeepDocumentTags
		case "keep_end_tags":
			target = &html.KeepEndTags
		case "keep_quotes":
			target = &html.KeepQuotes
		case "keep_whitespace":
			target = &html.KeepWhitespace
		default:
			return d.Errf("unrecognized html option %q", d.Val())
		}

		value := true
		if d.NextArg() {
			parsed, err := parseBoolToken(d.Val())
			if err != nil {
				return d.Errf("html option %s: %v", option, err)
			}
			value = parsed
			if d.NextArg() {
				return d.Errf("html option %s takes at most one value", option)
			}
		}
		*target = value
	}
	return nil
}
