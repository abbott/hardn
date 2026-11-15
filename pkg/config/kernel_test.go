package config

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestKernelYAML: hardn.yml's key is "kernel"; "kernelSettings", its name
// before any release, is an unknown key and sets nothing.
func TestKernelYAML(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"kernel: true\n", true},
		{"kernel: false\n", false},
		{"", false},
		{"kernelSettings: true\n", false},
	} {
		var c Config
		if err := yaml.Unmarshal([]byte(tc.text), &c); err != nil {
			t.Fatalf("%q: %v", tc.text, err)
		}
		if c.KernelSettings != tc.want {
			t.Errorf("%q: KernelSettings = %v, want %v", tc.text, c.KernelSettings, tc.want)
		}
	}
}
