package config

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestConfirmTimeoutKey: confirmTimeout (decision §3.14) reads whole
// seconds and durations as written, for revert.ParseTimeout, and a
// Config without it is saved without it, as before.
func TestConfirmTimeoutKey(t *testing.T) {
	for _, tc := range []struct{ yaml, want string }{
		{"confirmTimeout: 240\n", "240"},
		{"confirmTimeout: 5m\n", "5m"},
		{"username: alice\n", ""},
	} {
		var c Config
		if err := yaml.Unmarshal([]byte(tc.yaml), &c); err != nil || c.ConfirmTimeout != tc.want {
			t.Errorf("%q: ConfirmTimeout %q, %v; want %q", tc.yaml, c.ConfirmTimeout, err, tc.want)
		}
	}
	out, err := yaml.Marshal(&Config{Username: "alice"})
	if err != nil || strings.Contains(string(out), "confirmTimeout") {
		t.Errorf("a Config without the key saves %q, %v", out, err)
	}
	if out, err := yaml.Marshal(&Config{ConfirmTimeout: "240"}); err != nil || !strings.Contains(string(out), "confirmTimeout: \"240\"") {
		t.Errorf("with the key: %q, %v", out, err)
	}
}
