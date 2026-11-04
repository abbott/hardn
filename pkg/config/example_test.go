package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestExampleUsername: the example configuration, embedded and in the
// repository, names no default user (refactor plan §3.11, "george"
// dropped), as the first-run save writes it: username "" (SaveConfig of
// the defaults), so that -c asks for -u.
func TestExampleUsername(t *testing.T) {
	repo, err := os.ReadFile(filepath.Join("..", "..", "hardn.yml.example"))
	if err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(t.TempDir(), "hardn.yml")
	if err := SaveConfig(DefaultConfig(), saved); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"embedded": ExampleConfigContent, "hardn.yml.example": string(repo), "the first-run save": string(first)} {
		c := DefaultConfig()
		if err := yaml.Unmarshal([]byte(data), c); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.Username != "" || strings.Contains(data, `username: "george"`) {
			t.Errorf("%s: username %q, want none", name, c.Username)
		}
	}
}
