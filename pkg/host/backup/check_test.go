package backup

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestCheck: what Open and Backup would refuse, refused beforehand and
// with nothing written: the settings, a run directory that exists, a
// directory above it another user can change, and paths Backup's
// inspection refuses.
func TestCheck(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name  string
		path  string // backupPath; "" is /var/backups/hardn
		paths []string
		seed  func(w world)
		real  bool // in t.TempDir(), for modes the fake does not police
		want  error
	}{
		{"a file, a missing path and a symlink to a file", "", []string{"/etc/ssh/sshd_config", "/etc/ssh/sshd_config.d/00-hardn.conf", "/etc/pve/priv/authorized_keys"},
			func(w world) {
				w.file("etc/ssh/sshd_config", "Port 22\n", 0o644)
				w.file("etc/pve/priv/keys", "ssh-ed25519 AAAA alice@web-01.example.net\n", 0o600)
				w.symlink("etc/pve/priv/authorized_keys", "keys")
			}, false, nil},
		{"a backupPath that is not absolute", "backups", nil, nil, false, ErrSettings},
		{"a run directory that exists", "", nil, func(w world) { w.dir("var/backups/hardn/"+runID, 0o700) }, false, ErrRunExists},
		{"a path that is a directory", "", []string{"/etc/ssh"}, func(w world) { w.dir("etc/ssh", 0o755) }, false, ErrNotRegular},
		{"a path that is not clean", "", []string{"/etc/ssh/../ssh/sshd_config"}, nil, false, fs.ErrInvalid},
		{"a backup directory another user can change", "srv/shared/backups", nil, func(w world) { w.dir("srv/shared", 0o777) }, true, ErrUnsafeDir},
		{"a symlink in a directory another user can change", "", []string{"srv/shared/link/sshd_config"},
			func(w world) {
				w.dir("srv/shared", 0o777)
				w.file("etc/ssh/sshd_config", "", 0o644)
				w.symlink("srv/shared/link", "../../etc/ssh")
			}, true, host.ErrSymlinkParent},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := fakeWorld(t)
			if c.real {
				w = realWorld(t)
			}
			if c.seed != nil {
				c.seed(w)
			}
			s := Settings{Path: c.path}
			switch {
			case c.path == "":
				s.Path = w.path("var/backups/hardn")
			case c.real:
				s.Path = w.path(c.path)
			}
			var paths []string
			for _, p := range c.paths {
				if c.real {
					p = w.path(p)
				}
				paths = append(paths, p)
			}
			if err := Check(ctx, w.sys, s, runID, paths); c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("Check: %v, want %v", err, c.want)
			}
			if f, ok := w.sys.(*hostfake.System); ok {
				for _, call := range f.Calls() {
					switch call.Op {
					case "write-atomic", "write-in-place", "mkdir", "remove", "removeall", "rename", "chmod", "chown":
						t.Errorf("Check changed something: %s", call)
					}
				}
			}
		})
	}
}
