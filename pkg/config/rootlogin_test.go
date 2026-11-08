package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestPermitRootLogin reads each form of the key: the three strings, the
// old bool with a deprecation, YAML 1.1's spellings the bool field took,
// and the errors the bool field gave.
func TestPermitRootLogin(t *testing.T) {
	tests := []struct {
		yaml string
		want RootLogin
		dep  string // Found, "" for no deprecation
		err  string
	}{
		{yaml: "username: alice\n", want: RootLoginNo},
		{yaml: "permitRootLogin: no\n", want: RootLoginNo},
		{yaml: "permitRootLogin: yes\n", want: RootLoginYes},
		{yaml: "permitRootLogin: prohibit-password\n", want: RootLoginProhibitPassword},
		{yaml: "permitRootLogin: \"prohibit-password\"\n", want: RootLoginProhibitPassword},
		{yaml: "permitRootLogin: false\n", want: RootLoginNo},
		{yaml: "permitRootLogin: true\n", want: RootLoginYes},
		{yaml: "permitRootLogin: on\n", want: RootLoginYes},
		{yaml: "permitRootLogin: Off\n", want: RootLoginNo},
		{yaml: "permitRootLogin: Yes\n", want: RootLoginYes},
		{yaml: "permitRootLogin: ~\n", want: RootLoginNo},
		{yaml: "x: &a yes\npermitRootLogin: *a\n", want: RootLoginYes},
		{yaml: "permitRootLogin: without-password\n", err: "line 1: cannot unmarshal !!str `without...` into bool"},
		{yaml: "permitRootLogin: [no]\n", err: "line 1: cannot unmarshal !!seq into bool"},
		{yaml: "permitRootLogin: no\npermitRootLogin: yes\n", err: "line 2: field permitRootLogin already set in type config.Config"},
		{yaml: "- a\n", err: "line 1: cannot unmarshal !!seq into config.Config"},
	}
	for _, tt := range tests {
		t.Run(tt.yaml, func(t *testing.T) {
			c := DefaultConfig()
			err := yaml.Unmarshal([]byte(tt.yaml), c)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.RootLogin() != tt.want || c.PermitRootLogin != (tt.want == RootLoginYes) {
				t.Errorf("RootLogin() = %q, PermitRootLogin %v; want %q", c.RootLogin(), c.PermitRootLogin, tt.want)
			}
			deps := c.Deprecations()
			switch {
			case tt.dep == "" && len(deps) > 0:
				t.Errorf("deprecations %v, want none", deps)
			case tt.dep != "" && (len(deps) != 1 || deps[0].Found != tt.dep || deps[0].Use != string(tt.want) || deps[0].Line != 1 || deps[0].Key != "permitRootLogin"):
				t.Errorf("deprecations %+v, want %s → %q on line 1", deps, tt.dep, tt.want)
			}
			if tt.dep != "" && !strings.Contains(deps[0].String(), "deprecated") {
				t.Errorf("deprecation text %q", deps[0])
			}
		})
	}
}

// TestOtherKeysUnchanged: the other keys decode, and fail, as before.
func TestOtherKeysUnchanged(t *testing.T) {
	c := DefaultConfig()
	err := yaml.Unmarshal([]byte("username: alice\nsshPort: twenty-two\npermitRootLogin: yes\n"), c)
	want := "yaml: unmarshal errors:\n  line 2: cannot unmarshal !!str `twenty-two` into int"
	if err == nil || err.Error() != want {
		t.Fatalf("error %q, want %q", err, want)
	}
	if c.Username != "alice" || !c.PermitRootLogin {
		t.Errorf("after a type error: username %q, PermitRootLogin %v", c.Username, c.PermitRootLogin)
	}
}

// TestSaveConfigUnchanged: the legacy SaveConfig's encoding is what it was
// (yaml.v3 over the fields), permitRootLogin a bool, except that a
// prohibit-password value is kept rather than written as false.
func TestSaveConfigUnchanged(t *testing.T) {
	for _, in := range []string{"", "permitRootLogin: true\nusername: alice\n", "permitRootLogin: no\nsshAllowedUsers: [alice, bob]\n"} {
		c := DefaultConfig()
		if err := yaml.Unmarshal([]byte(in), c); err != nil {
			t.Fatal(err)
		}
		got, err := yaml.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		want, err := yaml.Marshal((*plain)(c))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("SaveConfig's encoding of %q changed:\n%s\nwant\n%s", in, got, want)
		}
		if !bytes.Contains(got, []byte("permitRootLogin: false\n")) && !bytes.Contains(got, []byte("permitRootLogin: true\n")) {
			t.Errorf("SaveConfig wrote no bool permitRootLogin for %q:\n%s", in, got)
		}
	}
	c := DefaultConfig()
	if err := c.SetRootLogin(RootLoginProhibitPassword); err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("permitRootLogin: prohibit-password\n")) {
		t.Errorf("prohibit-password not kept:\n%s", out)
	}
	back := DefaultConfig()
	if err := yaml.Unmarshal(out, back); err != nil || back.RootLogin() != RootLoginProhibitPassword || len(back.Deprecations()) > 0 {
		t.Errorf("read back: %v, %q, %v", err, back.RootLogin(), back.Deprecations())
	}
	if err := c.SetRootLogin("without-password"); err == nil {
		t.Error("SetRootLogin accepted without-password")
	}
}

// TestSave writes the loaded path atomically, keeping its mode and owner:
// the setting changed edited in place, the comment kept, a deprecated bool
// replaced by its string once its value changes; nothing written when
// nothing changed; refused for a Config loaded from no file, a file
// changed since it was read, and a symlink, writing nothing.
func TestSave(t *testing.T) {
	const path = "/etc/hardn/hardn.yml"
	ctx := context.Background()
	sys := hostfake.New(host.Options{})
	data := []byte("# comment\npermitRootLogin: false # old\n")
	sys.AddFile(path, data, 0o600, 0, 0)
	c := DefaultConfig()
	if err := yaml.Unmarshal(data, c); err != nil || len(c.Deprecations()) != 0 { // the bool is no deprecation (2026-11-07)
		t.Fatalf("load: %v, %v", err, c.Deprecations())
	}
	if err := Save(ctx, sys, c); !errors.Is(err, ErrNotLoaded) || len(sys.Calls()) != 0 {
		t.Fatalf("Save of a Config not loaded from a file = %v with %d calls, want ErrNotLoaded", err, len(sys.Calls()))
	}
	c.setLoaded(path, data) // as LoadConfig records it
	if err := Save(ctx, sys, c); err != nil {
		t.Fatal(err)
	}
	for _, call := range sys.Calls() {
		if call.Op != "read-nofollow" {
			t.Errorf("Save with nothing changed made %s", call)
		}
	}
	if err := c.SetRootLogin(RootLoginYes); err != nil {
		t.Fatal(err)
	}
	if err := Save(ctx, sys, c); err != nil {
		t.Fatal(err)
	}
	e, _ := sys.Entry(path)
	if e.Mode.Perm() != 0o600 || e.UID != 0 || string(e.Data) != "# comment\npermitRootLogin: \"yes\" # old\n" {
		t.Errorf("saved mode %v owner %d, content:\n%s", e.Mode, e.UID, e.Data)
	}
	var atomic bool
	for _, call := range sys.Calls() {
		atomic = atomic || call.Op == "write-atomic" && call.Path == path
		if call.Op == "write-in-place" {
			t.Errorf("Save wrote in place: %s", call)
		}
	}
	if !atomic {
		t.Error("Save did not write atomically")
	}
	back := DefaultConfig()
	if err := yaml.Unmarshal(e.Data, back); err != nil || back.RootLogin() != RootLoginYes || len(back.Deprecations()) != 0 || !c.LoadedFrom(e.Data) {
		t.Errorf("read back: %v, %q, %v; LoadedFrom the bytes saved %v", err, back.RootLogin(), back.Deprecations(), c.LoadedFrom(e.Data))
	}

	edited := []byte("permitRootLogin: \"no\"\n") // by hand, since
	sys.AddFile(path, edited, 0o600, 0, 0)
	c.SshPort = 2230
	if err := Save(ctx, sys, c); !errors.Is(err, ErrChanged) {
		t.Errorf("Save over a file changed since = %v, want ErrChanged", err)
	}
	sys.AddSymlink("/etc/hardn/link.yml", path)
	linked := DefaultConfig()
	linked.setLoaded("/etc/hardn/link.yml", edited)
	linked.SshPort = 2230
	if err := Save(ctx, sys, linked); !errors.Is(err, host.ErrSymlink) {
		t.Errorf("Save through a link = %v, want host.ErrSymlink", err)
	}
	if e, _ := sys.Entry(path); string(e.Data) != string(edited) {
		t.Errorf("written though refused:\n%s", e.Data)
	}
}

// TestLoadConfigRootLogin: the loader records the absolute path it read,
// and every value loads, prohibit-password too, no longer refused
// (decision 6 (a), adopted 2026-11-04), the bool too.
func TestLoadConfigRootLogin(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		yaml string
		want RootLogin
		err  error
	}{
		{"permitRootLogin: prohibit-password\n", RootLoginProhibitPassword, nil},
		{"permitRootLogin: \"no\"\n", RootLoginNo, nil},
		{"permitRootLogin: yes\n", RootLoginYes, nil},
		{"permitRootLogin: false\n", RootLoginNo, nil},
	} {
		name := filepath.Join(dir, "hardn.yml")
		if err := os.WriteFile(name, []byte(tt.yaml), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := LoadConfigWithEnvPriority(name)
		if !errors.Is(err, tt.err) {
			t.Errorf("%q: LoadConfigWithEnvPriority = %v, want %v", tt.yaml, err, tt.err)
			continue
		}
		if err != nil {
			continue
		}
		if c.RootLogin() != tt.want || c.Path() != name {
			t.Errorf("%q: RootLogin() %q, Path() %q; want %q, %q", tt.yaml, c.RootLogin(), c.Path(), tt.want, name)
		}
	}
}

// TestExampleRootLogin: the embedded example configuration gives
// permitRootLogin in its current form.
func TestExampleRootLogin(t *testing.T) {
	c := DefaultConfig()
	if err := yaml.Unmarshal([]byte(ExampleConfigContent), c); err != nil {
		t.Fatal(err)
	}
	if c.RootLogin() != RootLoginNo || len(c.Deprecations()) > 0 {
		t.Errorf("example: RootLogin() = %q, deprecations %v", c.RootLogin(), c.Deprecations())
	}
}
