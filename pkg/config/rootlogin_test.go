package config

import (
	"bytes"
	"context"
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
		{yaml: "permitRootLogin: false\n", want: RootLoginNo, dep: "false"},
		{yaml: "permitRootLogin: true\n", want: RootLoginYes, dep: "true"},
		{yaml: "permitRootLogin: on\n", want: RootLoginYes, dep: "on"},
		{yaml: "permitRootLogin: Off\n", want: RootLoginNo, dep: "Off"},
		{yaml: "permitRootLogin: Yes\n", want: RootLoginYes, dep: "Yes"},
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

// TestSave writes the loaded path atomically, keeping its mode and owner,
// with the canonical string in place of the deprecated bool.
func TestSave(t *testing.T) {
	const path = "/etc/hardn/hardn.yml"
	sys := hostfake.New(host.Options{})
	sys.AddFile(path, []byte("# comment\npermitRootLogin: false\n"), 0o600, 0, 0)
	c := DefaultConfig()
	data, _ := sys.Entry(path)
	if err := yaml.Unmarshal(data.Data, c); err != nil || len(c.Deprecations()) != 1 {
		t.Fatalf("load: %v, %v", err, c.Deprecations())
	}
	if err := Save(context.Background(), sys, c, path); err != nil {
		t.Fatal(err)
	}
	e, _ := sys.Entry(path)
	if e.Mode.Perm() != 0o600 || e.UID != 0 || !bytes.Contains(e.Data, []byte("permitRootLogin: \"no\"\n")) {
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
	if err := yaml.Unmarshal(e.Data, back); err != nil || back.RootLogin() != RootLoginNo || len(back.Deprecations()) != 0 {
		t.Errorf("read back: %v, %q, %v", err, back.RootLogin(), back.Deprecations())
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
