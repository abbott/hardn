package config

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestRead loads hardn.yml through host.System as LoadConfig loads it,
// through a link too, records the path and the bytes, refuses what
// LoadConfig refuses and a path that is not absolute and clean, and reads
// only; Save then records what it wrote.
func TestRead(t *testing.T) {
	ctx := context.Background()
	const path = "/etc/hardn/hardn.yml"
	data := []byte("# hardn\nnameservers:\n  - 192.0.2.54\n  - 2001:db8::54\npermitRootLogin: \"no\"\n")
	sys := hostfake.New(host.Options{})
	sys.AddFile(path, data, 0o600, 0, 0)
	c, err := Read(ctx, sys, path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path() != path || !slices.Equal(c.Nameservers, []string{"192.0.2.54", "2001:db8::54"}) || c.SshPort != 22 || !c.LoadedFrom(data) {
		t.Errorf("Path %q, nameservers %q, sshPort %d (the default), loaded from the bytes %v", c.Path(), c.Nameservers, c.SshPort, c.LoadedFrom(data))
	}
	for _, call := range sys.Calls() {
		if call.Op != "read" {
			t.Errorf("Read made %s", call)
		}
	}
	if c.LoadedFrom([]byte("nameservers: []\n")) || DefaultConfig().LoadedFrom(nil) {
		t.Error("LoadedFrom holds for other bytes, or for a configuration read from none")
	}
	c.Nameservers = []string{"192.0.2.53"}
	if err := Save(ctx, sys, c); err != nil {
		t.Fatal(err)
	}
	saved, _ := sys.Entry(path)
	want := "# hardn\nnameservers:\n  - 192.0.2.53\npermitRootLogin: \"no\"\n"
	if !c.LoadedFrom(saved.Data) || c.LoadedFrom(data) || string(saved.Data) != want {
		t.Errorf("after Save, LoadedFrom the saved bytes %v, the old ones %v; saved:\n%s", c.LoadedFrom(saved.Data), c.LoadedFrom(data), saved.Data)
	}

	sys.AddFile("/etc/hardn/pp.yml", []byte("permitRootLogin: prohibit-password\n"), 0o600, 0, 0)
	sys.AddFile("/etc/hardn/broken.yml", []byte("nameservers: [\n"), 0o600, 0, 0)
	sys.AddSymlink("/etc/hardn/link.yml", path)
	for _, tt := range []struct {
		path string
		want error
	}{
		{"hardn.yml", fs.ErrInvalid},
		{"/etc/hardn/../hardn/hardn.yml", fs.ErrInvalid},
		{"/etc/hardn/none.yml", fs.ErrNotExist},
		{"/etc/hardn/pp.yml", ErrRootLoginNotApplied},
		{"/etc/hardn/broken.yml", ErrInvalidFile},
	} {
		if _, err := Read(ctx, sys, tt.path); !errors.Is(err, tt.want) {
			t.Errorf("Read(%q) = %v, want %v", tt.path, err, tt.want)
		}
	}
	if c, err := Read(ctx, sys, "/etc/hardn/link.yml"); err != nil || c.Path() != "/etc/hardn/link.yml" {
		t.Errorf("Read through a link, as LoadConfig reads it: %v", err)
	}
}
