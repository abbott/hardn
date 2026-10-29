package sudoers_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host/sudoers"
)

// TestInvoker: who ran hardn is SUDO_USER under sudo and sudo-rs,
// DOAS_USER under doas, each tool's own and never the other's or USER;
// root or unset is no one; a name hardn does not write into a rule is
// refused, as is a tool that is neither.
func TestInvoker(t *testing.T) {
	for _, tc := range []struct {
		name string
		t    sudoers.Tool
		env  map[string]string
		want string
		err  error
	}{
		{"sudo", sudoers.ToolSudo, map[string]string{"SUDO_USER": "alice", "USER": "root"}, "alice", nil},
		{"sudo, a dotted name", sudoers.ToolSudo, map[string]string{"SUDO_USER": "alice.smith"}, "alice.smith", nil},
		{"doas", sudoers.ToolDoas, map[string]string{"DOAS_USER": "alice", "USER": "root"}, "alice", nil},
		{"a root login", sudoers.ToolSudo, map[string]string{"USER": "root"}, "", nil},
		{"root ran sudo", sudoers.ToolSudo, map[string]string{"SUDO_USER": "root", "USER": "root"}, "", nil},
		{"root ran doas", sudoers.ToolDoas, map[string]string{"DOAS_USER": "root"}, "", nil},
		{"USER is not read", sudoers.ToolSudo, map[string]string{"USER": "alice"}, "", nil},
		{"sudo: doas's variable is not read", sudoers.ToolSudo, map[string]string{"DOAS_USER": "alice"}, "", nil},
		{"doas: sudo's variable is not read", sudoers.ToolDoas, map[string]string{"SUDO_USER": "alice"}, "", nil},
		{"a name hardn does not write", sudoers.ToolSudo, map[string]string{"SUDO_USER": "Alice"}, "", sudoers.ErrUserName},
		{"a name sudoers reads as a group", sudoers.ToolSudo, map[string]string{"SUDO_USER": "%sudo"}, "", sudoers.ErrUserName},
		{"a doas keyword", sudoers.ToolDoas, map[string]string{"DOAS_USER": "persist"}, "", sudoers.ErrUserName},
		{"no tool", sudoers.ToolNone, map[string]string{"SUDO_USER": "alice"}, "", fs.ErrInvalid},
		{"tool unknown", sudoers.ToolUnknown, map[string]string{"SUDO_USER": "alice"}, "", fs.ErrInvalid},
	} {
		got, err := sudoers.Invoker(tc.t, func(k string) string { return tc.env[k] })
		if got != tc.want || !errors.Is(err, tc.err) || (err == nil) != (tc.err == nil) {
			t.Errorf("%s: Invoker = %q, %v; want %q, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}
