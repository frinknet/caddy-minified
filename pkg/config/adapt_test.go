package config_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig"
	_ "github.com/frinknet/caddy-minified"
)

func adapt(t *testing.T, input string) string {
	t.Helper()
	adapter := caddyconfig.GetAdapter("caddyfile")
	if adapter == nil {
		t.Fatal("caddyfile adapter is not registered")
	}
	out, warnings, err := adapter.Adapt([]byte(input), nil)
	if err != nil {
		t.Fatalf("adapt: %v", err)
	}
	for _, warning := range warnings {
		if !strings.Contains(warning.String(), "not formatted") {
			t.Fatalf("unexpected warning: %+v", warning)
		}
	}
	return string(out)
}

func TestCaddyfileAdaptation(t *testing.T) {
	adapted := adapt(t, `{
	minified {
		html {
			keep_comments
		}
		css
	}
}

http://example.test {
	minified on
	minified /admin/* off
	respond "ok"
}`)

	var parsed any
	if err := json.Unmarshal([]byte(adapted), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, adapted)
	}
	if strings.Count(adapted, `"handler":"minified"`) != 2 {
		t.Fatalf("expected two minified handlers:\n%s", adapted)
	}
	if !strings.Contains(adapted, `"keep_comments":true`) {
		t.Fatalf("global html options were not applied:\n%s", adapted)
	}
	if strings.Count(adapted, `"enabled":true,"html"`) != 1 {
		t.Fatalf("minified on was not applied exactly once:\n%s", adapted)
	}
	if !strings.Contains(adapted, `"match":[{"path":["/admin/*"]}]`) {
		t.Fatalf("off directive did not keep its matcher:\n%s", adapted)
	}
	if strings.Contains(adapted, `"js":true`) {
		t.Fatalf("js was not listed in the global option:\n%s", adapted)
	}
}

func TestCaddyfileDefaults(t *testing.T) {
	adapted := adapt(t, `http://example.test {
	minified
	respond "ok"
}`)
	for _, format := range []string{`"css":true`, `"js":true`, `"svg":true`, `"html":{"enabled":true}`} {
		if !strings.Contains(adapted, format) {
			t.Fatalf("expected default format %s:\n%s", format, adapted)
		}
	}
}
