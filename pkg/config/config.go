package config

import (
	"errors"
	"fmt"
	"strings"
)

type Config struct {
	Enabled bool `json:"enabled,omitempty"`
	HTML    HTML `json:"html,omitempty"`
	CSS     bool `json:"css,omitempty"`
	JS      bool `json:"js,omitempty"`
	SVG     bool `json:"svg,omitempty"`
}

type HTML struct {
	Enabled             bool `json:"enabled,omitempty"`
	KeepComments        bool `json:"keep_comments,omitempty"`
	KeepSpecialComments bool `json:"keep_special_comments,omitempty"`
	KeepDefaultAttrVals bool `json:"keep_default_attr_vals,omitempty"`
	KeepDocumentTags    bool `json:"keep_document_tags,omitempty"`
	KeepEndTags         bool `json:"keep_end_tags,omitempty"`
	KeepQuotes          bool `json:"keep_quotes,omitempty"`
	KeepWhitespace      bool `json:"keep_whitespace,omitempty"`
}

func Default() *Config {
	cfg := &Config{}
	cfg.EnableAll()
	return cfg
}

func Empty() *Config {
	return &Config{}
}

func OrDefault(cfg *Config) *Config {
	if cfg == nil {
		return Default()
	}
	return cfg
}

func (cfg *Config) Clone() *Config {
	if cfg == nil {
		return nil
	}
	copy := *cfg
	return &copy
}

func (cfg *Config) EnableAll() {
	cfg.HTML.Enabled = true
	cfg.CSS = true
	cfg.JS = true
	cfg.SVG = true
}

func (cfg *Config) AnyFormat() bool {
	return cfg.HTML.Enabled || cfg.CSS || cfg.JS || cfg.SVG
}

func (cfg *Config) Normalize() {
	if !cfg.AnyFormat() {
		cfg.EnableAll()
	}
}

func (cfg *Config) Validate() error {
	if cfg == nil {
		return errors.New("minified: nil configuration")
	}
	if cfg.Enabled && !cfg.AnyFormat() {
		return errors.New("minified: enabled but no response format is enabled")
	}
	return nil
}

func MediaType(contentType string) string {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return strings.ToLower(strings.TrimSpace(contentType))
}

func (cfg *Config) Supports(contentType string) bool {
	switch MediaType(contentType) {
	case "text/html", "application/xhtml+xml":
		return cfg.HTML.Enabled
	case "text/css":
		return cfg.CSS
	case "text/javascript", "application/javascript", "application/x-javascript":
		return cfg.JS
	case "image/svg+xml":
		return cfg.SVG
	}
	return false
}

func parseBoolToken(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "on", "yes", "1":
		return true, nil
	case "false", "off", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("expected a boolean, got %q", value)
}
