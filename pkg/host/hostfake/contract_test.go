package hostfake_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// world is one System under test with a fresh root directory, and seeding
// done outside the System.
type world struct {
	sys  host.LegacySystem
	root string
	// canon is root with no symlink in it (t.TempDir() is under the
	// /var -> /private/var link on macOS).
	canon   string
	file    func(rel string, data string, perm fs.FileMode)
	dir     func(rel string)
	symlink func(rel, target string)
}

func (w world) path(rel string) string { return filepath.Join(w.root, rel) }

func realWorld(t *testing.T) world {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	canon, err := filepath.EvalSymlinks(root)
	must(err)
	return world{
		sys:   host.NewReal(host.Options{}),
		root:  root,
		canon: canon,
		file: func(rel, data string, perm fs.FileMode) {
			must(os.WriteFile(filepath.Join(root, rel), []byte(data), perm))
		},
		dir:     func(rel string) { must(os.Mkdir(filepath.Join(root, rel), 0o755)) },
		symlink: func(rel, target string) { must(os.Symlink(target, filepath.Join(root, rel))) },
	}
}

func fakeWorld(*testing.T) world {
	f := hostfake.New(host.Options{})
	root := "/srv/contract"
	f.AddDir(root, 0o755, 0, 0)
	return world{
		sys:   f,
		root:  root,
		canon: root,
		file: func(rel, data string, perm fs.FileMode) {
			f.AddFile(filepath.Join(root, rel), []byte(data), perm, 0, 0)
		},
		dir:     func(rel string) { f.AddDir(filepath.Join(root, rel), 0o755, 0, 0) },
		symlink: func(rel, target string) { f.AddSymlink(filepath.Join(root, rel), target) },
	}
}

// scanned is every line of rel, through ScanLines, and its FileInfo.
func scanned(t *testing.T, w world, rel string) ([]string, fs.FileInfo) {
	t.Helper()
	got := []string{}
	fi, err := w.sys.ScanLines(context.Background(), w.path(rel), func(l host.Line) bool {
		got = append(got, string(l.Text))
		return true
	})
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return got, fi
}

// TestContract runs each case against host.Real in a temporary directory and
// against the fake, so the fake keeps the os package's rules for the hazards
// the safety primitives guard against. Owners are not compared (the fake acts
// as root); -1 keeps them.
func TestContract(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		run  func(t *testing.T, w world)
	}{
		{"write in place follows a symlink and keeps the mode", func(t *testing.T, w world) {
			w.file("keys", "old\n", 0o600)
			w.symlink("authorized_keys", w.path("keys"))
			if err := w.sys.WriteFileInPlace(ctx, w.path("authorized_keys"), []byte("new\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "keys", "new\n", 0o600)
			wantLink(t, w, "authorized_keys", w.path("keys"))
		}},
		{"write in place creates a missing file", func(t *testing.T, w world) {
			if err := w.sys.WriteFileInPlace(ctx, w.path("new"), []byte("x"), 0o640); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "new", "x", 0o640)
		}},
		{"atomic write refuses a symlink, even a dangling one", func(t *testing.T, w world) {
			w.file("keys", "cluster\n", 0o600)
			w.symlink("authorized_keys", w.path("keys"))
			w.symlink("dangling", w.path("nowhere"))
			for _, name := range []string{"authorized_keys", "dangling"} {
				if err := w.sys.WriteFileAtomic(ctx, w.path(name), []byte("x"), 0o600, -1, -1); !errors.Is(err, host.ErrSymlink) {
					t.Errorf("%s: err = %v, want ErrSymlink", name, err)
				}
			}
			wantFile(t, w, "keys", "cluster\n", 0o600)
			wantLink(t, w, "authorized_keys", w.path("keys"))
			wantNames(t, w, "", "authorized_keys", "dangling", "keys")
		}},
		{"atomic write replaces content and mode", func(t *testing.T, w world) {
			w.file("00-hardn.conf", "Port 22\n", 0o644)
			if err := w.sys.WriteFileAtomic(ctx, w.path("00-hardn.conf"), []byte("Port 2230\n"), 0o600, -1, -1); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "00-hardn.conf", "Port 2230\n", 0o600)
			wantNames(t, w, "", "00-hardn.conf")
		}},
		{"atomic write refuses a directory and a missing parent", func(t *testing.T, w world) {
			w.dir("d")
			if err := w.sys.WriteFileAtomic(ctx, w.path("d"), nil, 0o600, -1, -1); !errors.Is(err, syscall.EISDIR) {
				t.Errorf("directory: err = %v, want EISDIR", err)
			}
			if err := w.sys.WriteFileAtomic(ctx, w.path("no/f"), nil, 0o600, -1, -1); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing parent: err = %v, want ErrNotExist", err)
			}
		}},
		{"append creates the file, then appends; the mode stays", func(t *testing.T, w world) {
			if err := w.sys.AppendFile(ctx, w.path("audit.jsonl"), []byte("{\"a\":1}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := w.sys.AppendFile(ctx, w.path("audit.jsonl"), []byte("{\"b\":2}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "audit.jsonl", "{\"a\":1}\n{\"b\":2}\n", 0o600)
		}},
		{"append refuses a symlink, a directory and a missing parent", func(t *testing.T, w world) {
			w.file("keys", "cluster\n", 0o600)
			w.symlink("audit.jsonl", w.path("keys"))
			w.symlink("dangling", w.path("nowhere"))
			w.dir("d")
			for _, name := range []string{"audit.jsonl", "dangling"} {
				if err := w.sys.AppendFile(ctx, w.path(name), []byte("x\n"), 0o600); !errors.Is(err, host.ErrSymlink) {
					t.Errorf("%s: err = %v, want ErrSymlink", name, err)
				}
			}
			if err := w.sys.AppendFile(ctx, w.path("d"), []byte("x\n"), 0o600); !errors.Is(err, syscall.EISDIR) {
				t.Errorf("directory: err = %v, want EISDIR", err)
			}
			if err := w.sys.AppendFile(ctx, w.path("no/f"), []byte("x\n"), 0o600); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing parent: err = %v, want ErrNotExist", err)
			}
			wantFile(t, w, "keys", "cluster\n", 0o600)
			wantLink(t, w, "audit.jsonl", w.path("keys"))
			wantNames(t, w, "", "audit.jsonl", "d", "dangling", "keys")
		}},
		{"append refuses a directory another user can change", func(t *testing.T, w world) {
			w.dir("open")
			w.file("open/audit.jsonl", "{\"a\":1}\n", 0o600)
			if err := w.sys.Chmod(ctx, w.path("open"), 0o777); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"open/audit.jsonl", "open/new.jsonl"} {
				if err := w.sys.AppendFile(ctx, w.path(name), []byte("x\n"), 0o600); !errors.Is(err, host.ErrUntrustedDir) {
					t.Errorf("%s: err = %v, want ErrUntrustedDir", name, err)
				}
			}
			wantFile(t, w, "open/audit.jsonl", "{\"a\":1}\n", 0o600)
			wantNames(t, w, "open", "audit.jsonl")
		}},
		{"rename over a symlink replaces the link, not its target", func(t *testing.T, w world) {
			w.file("keys", "cluster\n", 0o600)
			w.symlink("authorized_keys", w.path("keys"))
			w.file("tmp", "replaced\n", 0o600)
			if err := w.sys.Rename(ctx, w.path("tmp"), w.path("authorized_keys")); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "authorized_keys", "replaced\n", 0o600)
			wantFile(t, w, "keys", "cluster\n", 0o600)
		}},
		{"rename refuses an existing directory at newname, even an empty one", func(t *testing.T, w world) {
			w.dir("run-1")
			w.file("run-1/f", "x", 0o600)
			w.dir("current")
			w.file("f", "y", 0o600)
			if err := w.sys.Rename(ctx, w.path("run-1"), w.path("current")); !errors.Is(err, syscall.EEXIST) {
				t.Errorf("directory over an empty directory: err = %v, want EEXIST", err)
			}
			if err := w.sys.Rename(ctx, w.path("f"), w.path("current")); !errors.Is(err, syscall.EEXIST) {
				t.Errorf("file over a directory: err = %v, want EEXIST", err)
			}
			if err := w.sys.Rename(ctx, w.path("run-1"), w.path("f")); !errors.Is(err, syscall.ENOTDIR) {
				t.Errorf("directory over a file: err = %v, want ENOTDIR", err)
			}
			wantNames(t, w, "", "current", "f", "run-1")
			wantFile(t, w, "run-1/f", "x", 0o600)
		}},
		{"rename moves a directory tree", func(t *testing.T, w world) {
			w.dir("a")
			w.file("a/f", "x", 0o600)
			if err := w.sys.Rename(ctx, w.path("a"), w.path("b")); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "b/f", "x", 0o600)
			wantNames(t, w, "", "b")
		}},
		{"remove: missing, non-empty directory, symlink", func(t *testing.T, w world) {
			w.dir("d")
			w.file("d/f", "x", 0o600)
			w.symlink("l", w.path("d"))
			if err := w.sys.Remove(ctx, w.path("missing")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing: err = %v, want ErrNotExist", err)
			}
			if err := w.sys.Remove(ctx, w.path("d")); !errors.Is(err, syscall.ENOTEMPTY) {
				t.Errorf("non-empty dir: err = %v, want ENOTEMPTY", err)
			}
			if err := w.sys.Remove(ctx, w.path("l")); err != nil {
				t.Errorf("symlink: err = %v", err)
			}
			wantNames(t, w, "d", "f")
		}},
		{"remove all: missing is fine, a symlink goes but not its target", func(t *testing.T, w world) {
			w.dir("d")
			w.file("d/f", "x", 0o600)
			w.symlink("l", w.path("d"))
			if err := w.sys.RemoveAll(ctx, w.path("missing")); err != nil {
				t.Errorf("missing: err = %v", err)
			}
			if err := w.sys.RemoveAll(ctx, w.path("l")); err != nil {
				t.Fatal(err)
			}
			wantNames(t, w, "d", "f")
			if err := w.sys.RemoveAll(ctx, w.path("d")); err != nil {
				t.Fatal(err)
			}
			wantNames(t, w, "")
		}},
		{"stat follows, lstat does not, readdir sorts", func(t *testing.T, w world) {
			w.file("b", "", 0o600)
			w.dir("a")
			w.symlink("c", "a")
			fi, err := w.sys.Stat(ctx, w.path("c"))
			if err != nil || !fi.IsDir() {
				t.Errorf("Stat(symlink to dir) = %v, %v; want a directory", fi, err)
			}
			li, err := w.sys.Lstat(ctx, w.path("c"))
			if err != nil || li.Mode()&fs.ModeSymlink == 0 || li.Name() != "c" {
				t.Errorf("Lstat(symlink) = %v, %v; want a symlink named c", li, err)
			}
			wantNames(t, w, "", "a", "b", "c")
		}},
		{"read: through a symlink, a dangling one, a directory", func(t *testing.T, w world) {
			w.file("f", "data", 0o600)
			w.symlink("l", "f")
			w.symlink("dangling", "nowhere")
			w.dir("d")
			if got, err := w.sys.ReadFile(ctx, w.path("l")); err != nil || string(got) != "data" {
				t.Errorf("through symlink = %q, %v", got, err)
			}
			if _, err := w.sys.ReadFile(ctx, w.path("dangling")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("dangling: err = %v, want ErrNotExist", err)
			}
			if _, err := w.sys.ReadFile(ctx, w.path("d")); !errors.Is(err, syscall.EISDIR) {
				t.Errorf("directory: err = %v, want EISDIR", err)
			}
		}},
		{"mkdir all: nested, existing, over a file", func(t *testing.T, w world) {
			w.file("f", "", 0o600)
			if err := w.sys.MkdirAll(ctx, w.path("x/y/z"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := w.sys.MkdirAll(ctx, w.path("x/y"), 0o700); err != nil {
				t.Errorf("existing: err = %v", err)
			}
			if err := w.sys.MkdirAll(ctx, w.path("f"), 0o700); !errors.Is(err, syscall.ENOTDIR) {
				t.Errorf("over a file: err = %v, want ENOTDIR", err)
			}
			if err := w.sys.MkdirAll(ctx, w.path("f/sub"), 0o700); !errors.Is(err, syscall.ENOTDIR) {
				t.Errorf("under a file: err = %v, want ENOTDIR", err)
			}
			wantNames(t, w, "x/y", "z")
		}},
		{"chown refuses a symlink and a missing path", func(t *testing.T, w world) {
			w.file("f", "", 0o600)
			w.symlink("l", "f")
			if err := w.sys.Chown(ctx, w.path("f"), -1, -1); err != nil {
				t.Errorf("file: err = %v", err)
			}
			if err := w.sys.Chown(ctx, w.path("l"), -1, -1); !errors.Is(err, host.ErrSymlink) {
				t.Errorf("symlink: err = %v, want ErrSymlink", err)
			}
			if err := w.sys.Chown(ctx, w.path("missing"), -1, -1); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing: err = %v, want ErrNotExist", err)
			}
		}},
		{"chmod changes a file and a directory, refuses a symlink and a missing path", func(t *testing.T, w world) {
			w.file("authorized_keys", "k\n", 0o644)
			w.dir("ssh")
			w.file("cluster_keys", "c\n", 0o644)
			w.symlink("l", w.path("cluster_keys"))
			if err := w.sys.Chmod(ctx, w.path("authorized_keys"), 0o600); err != nil {
				t.Errorf("file: err = %v", err)
			}
			if err := w.sys.Chmod(ctx, w.path("ssh"), 0o700); err != nil {
				t.Errorf("directory: err = %v", err)
			}
			if err := w.sys.Chmod(ctx, w.path("l"), 0o600); !errors.Is(err, host.ErrSymlink) {
				t.Errorf("symlink: err = %v, want ErrSymlink", err)
			}
			if err := w.sys.Chmod(ctx, w.path("missing"), 0o600); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing: err = %v, want ErrNotExist", err)
			}
			wantFile(t, w, "authorized_keys", "k\n", 0o600)
			wantFile(t, w, "cluster_keys", "c\n", 0o644)
			wantLink(t, w, "l", w.path("cluster_keys"))
			if fi, err := w.sys.Lstat(ctx, w.path("ssh")); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
				t.Errorf("ssh = %v, %v; want a 0700 directory", modeOf(fi), err)
			}
		}},
		{"guarded operations refuse a symlink on the way from a world-writable directory", func(t *testing.T, w world) {
			w.dir("rootssh")
			w.file("rootssh/authorized_keys", "root-key\n", 0o600)
			w.dir("home")
			if err := w.sys.Chmod(ctx, w.path("home"), 0o777); err != nil {
				t.Fatal(err)
			}
			w.symlink("home/.ssh", w.path("rootssh"))
			via := w.path("home/.ssh/authorized_keys")
			for op, err := range map[string]error{
				"write":  w.sys.WriteFileAtomic(ctx, via, []byte("alice-key\n"), 0o600, -1, -1),
				"chown":  w.sys.Chown(ctx, via, -1, -1),
				"chmod":  w.sys.Chmod(ctx, via, 0o644),
				"remove": w.sys.Remove(ctx, via),
				"read":   second(w.sys.ReadFileNoFollow(ctx, via)),
			} {
				if !errors.Is(err, host.ErrSymlinkParent) || !errors.Is(err, host.ErrSymlink) {
					t.Errorf("%s: err = %v, want ErrSymlinkParent", op, err)
				}
			}
			wantFile(t, w, "rootssh/authorized_keys", "root-key\n", 0o600)
			if got, err := w.sys.ReadFile(ctx, via); err != nil || string(got) != "root-key\n" {
				t.Errorf("unguarded read through the link = %q, %v; want it followed", got, err)
			}
		}},
		{"guarded operations follow a symlink from a trusted directory", func(t *testing.T, w world) {
			w.dir("etc")
			w.symlink("link", w.path("etc"))
			if err := w.sys.WriteFileAtomic(ctx, w.path("link/hardn.yml"), []byte("x"), 0o600, -1, -1); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "etc/hardn.yml", "x", 0o600)
			if err := w.sys.Remove(ctx, w.path("link/hardn.yml")); err != nil {
				t.Fatal(err)
			}
			wantNames(t, w, "etc")
		}},
		{"read no-follow: a file with its mode, a symlink, a directory, a missing path", func(t *testing.T, w world) {
			w.file("f", "data", 0o640)
			w.symlink("l", "f")
			w.dir("d")
			data, fi, err := w.sys.ReadFileNoFollow(ctx, w.path("f"))
			if err != nil || string(data) != "data" || fi.Mode() != 0o640 || fi.Name() != "f" || fi.Size() != 4 {
				t.Errorf("file = %q, %v, %v", data, fi, err)
			}
			for rel, want := range map[string]error{"l": host.ErrSymlink, "d": syscall.EISDIR, "missing": fs.ErrNotExist} {
				if _, _, err := w.sys.ReadFileNoFollow(ctx, w.path(rel)); !errors.Is(err, want) {
					t.Errorf("%s: err = %v, want %v", rel, err, want)
				}
			}
		}},
		{"resolve symlink: targets joined to the directory the walk reaches, and refusals", func(t *testing.T, w world) {
			w.dir("real")
			w.dir("real/sub")
			w.file("real/target", "t\n", 0o600)
			w.symlink("alias", w.path("real/sub"))
			w.symlink("real/sub/rel", "../target")
			w.symlink("real/sub/abs", w.path("real/target"))
			w.symlink("real/sub/interior", "x/../target")
			w.dir("open")
			if err := w.sys.Chmod(ctx, w.path("open"), 0o777); err != nil {
				t.Fatal(err)
			}
			w.symlink("open/l", w.path("real/target"))
			for rel, want := range map[string][2]string{
				// "../" is the parent of real/sub, not of alias.
				"alias/rel": {"../target", filepath.Join(w.canon, "real/target")},
				"alias/abs": {w.path("real/target"), w.path("real/target")},
			} {
				target, resolved, err := w.sys.ResolveSymlink(ctx, w.path(rel))
				if err != nil || target != want[0] || resolved != want[1] {
					t.Errorf("%s = %q, %q, %v; want %q", rel, target, resolved, err, want)
				}
			}
			for rel, want := range map[string]error{
				"alias/interior": fs.ErrInvalid,
				"open/l":         host.ErrSymlinkParent,
				"real/target":    syscall.EINVAL,
				"missing":        fs.ErrNotExist,
			} {
				if _, _, err := w.sys.ResolveSymlink(ctx, w.path(rel)); !errors.Is(err, want) {
					t.Errorf("%s: err = %v, want %v", rel, err, want)
				}
			}
			if err := host.WriteFileThroughSymlink(ctx, w.sys, w.path("open/l"), []byte("x\n"), 0o600, -1, -1); !errors.Is(err, host.ErrSymlinkParent) {
				t.Errorf("write through an untrusted link: err = %v, want ErrSymlinkParent", err)
			}
			if err := host.WriteFileThroughSymlink(ctx, w.sys, w.path("alias/rel"), []byte("new\n"), 0o600, -1, -1); err != nil {
				t.Fatal(err)
			}
			wantFile(t, w, "real/target", "new\n", 0o600)
			wantLink(t, w, "real/sub/rel", "../target")
		}},
		{"scan lines: endings, empty lines, the size, in order", func(t *testing.T, w world) {
			for content, want := range map[string][]string{
				"":               {},
				"one":            {"one"},
				"one\n":          {"one"},
				"one\ntwo":       {"one", "two"},
				"\n":             {""},
				"\n\n":           {"", ""},
				"one\n\ntwo\n":   {"one", "", "two"},
				"one\r\ntwo\r\n": {"one", "two"},
				"one\r\ntwo\r":   {"one", "two"},
				"a\rb\n":         {"a\rb"},
				"one\r\r\n":      {"one\r"},
				"\r\n":           {""},
				"caf\xe9 \x00\n": {"caf\xe9 \x00"},
			} {
				w.file("log", content, 0o640)
				got, fi := scanned(t, w, "log")
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%q: lines %q, want %q", content, got, want)
				}
				if fi.Size() != int64(len(content)) || !fi.Mode().IsRegular() {
					t.Errorf("%q: FileInfo %d %v", content, fi.Size(), fi.Mode())
				}
			}
		}},
		{"scan lines: a line past the cap is cut and the read goes on", func(t *testing.T, w world) {
			long := strings.Repeat("x", host.MaxLine)
			for _, c := range []struct {
				name, content string
				want          []string
				cut           []bool
			}{
				{"at the cap", long + "\nnext\n", []string{long, "next"}, []bool{false, false}},
				{"at the cap, CRLF", long + "\r\nnext\n", []string{long, "next"}, []bool{false, false}},
				{"at the cap, no ending", long, []string{long}, []bool{false}},
				{"at the cap, a last CR", long + "\r", []string{long}, []bool{false}},
				{"one past", long + "y\nnext\n", []string{long, "next"}, []bool{true, false}},
				{"one past, CRLF", long + "y\r\nnext\n", []string{long, "next"}, []bool{true, false}},
				{"one past, no ending", long + "y", []string{long}, []bool{true}},
				{"at the cap, a CR, then more", long + "\ry\nnext\n", []string{long, "next"}, []bool{true, false}},
				{"far past", long + strings.Repeat("y", 3*host.MaxLine) + "\nnext\nlast", []string{long, "next", "last"}, []bool{true, false, false}},
				{"far past, no ending", long + strings.Repeat("y", 3*host.MaxLine), []string{long}, []bool{true}},
				{"two in a row", long + "y\n" + long + "z\n", []string{long, long}, []bool{true, true}},
			} {
				w.file("log", c.content, 0o640)
				var got []string
				var cut []bool
				if _, err := w.sys.ScanLines(ctx, w.path("log"), func(l host.Line) bool {
					got, cut = append(got, string(l.Text)), append(cut, l.Cut)
					return true
				}); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
				if !reflect.DeepEqual(got, c.want) || !reflect.DeepEqual(cut, c.cut) {
					t.Errorf("%s: %d lines, cut %v; want %d, cut %v", c.name, len(got), cut, len(c.want), c.cut)
				}
			}
		}},
		{"scan lines: false stops the read, no error, the FileInfo kept", func(t *testing.T, w world) {
			w.file("log", "one\ntwo\n", 0o640)
			n := 0
			fi, err := w.sys.ScanLines(ctx, w.path("log"), func(host.Line) bool { n++; return false })
			if err != nil || n != 1 || fi == nil || fi.Size() != 8 {
				t.Errorf("n = %d, fi = %v, err = %v", n, fi, err)
			}
		}},
		{"scan lines: refusals", func(t *testing.T, w world) {
			w.file("keys", "root:x\n", 0o600)
			w.symlink("link", w.path("keys"))
			w.symlink("dangling", w.path("nowhere"))
			w.dir("d")
			for name, want := range map[string]error{
				"link":     host.ErrSymlink,
				"dangling": host.ErrSymlink,
				"d":        syscall.EISDIR,
				"missing":  fs.ErrNotExist,
			} {
				fi, err := w.sys.ScanLines(ctx, w.path(name), func(host.Line) bool {
					t.Errorf("%s: a line", name)
					return true
				})
				if !errors.Is(err, want) || fi != nil {
					t.Errorf("%s: fi = %v, err = %v, want %v", name, fi, err, want)
				}
			}
			done, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := w.sys.ScanLines(done, w.path("keys"), func(host.Line) bool {
				t.Error("a line under a done context")
				return true
			}); !errors.Is(err, context.Canceled) {
				t.Errorf("done: err = %v", err)
			}
		}},
		{"scan lines: guarded, a link on the way only from a trusted directory", func(t *testing.T, w world) {
			w.dir("real")
			w.file("real/log", "one\n", 0o640)
			w.symlink("alias", w.path("real"))
			if got, _ := scanned(t, w, "alias/log"); !reflect.DeepEqual(got, []string{"one"}) {
				t.Errorf("through a trusted directory: %q", got)
			}
			w.dir("open")
			if err := w.sys.Chmod(ctx, w.path("open"), 0o777); err != nil {
				t.Fatal(err)
			}
			w.symlink("open/alias", w.path("real"))
			if _, err := w.sys.ScanLines(ctx, w.path("open/alias/log"), func(host.Line) bool { return true }); !errors.Is(err, host.ErrSymlinkParent) {
				t.Errorf("through an untrusted directory: err = %v, want ErrSymlinkParent", err)
			}
		}},
		{"readlink: a relative target as stored, a file, a missing path", func(t *testing.T, w world) {
			w.file("f", "", 0o600)
			w.symlink("l", "../elsewhere/f")
			if got, err := w.sys.Readlink(ctx, w.path("l")); err != nil || got != "../elsewhere/f" {
				t.Errorf("symlink = %q, %v", got, err)
			}
			if _, err := w.sys.Readlink(ctx, w.path("f")); !errors.Is(err, syscall.EINVAL) {
				t.Errorf("file: err = %v, want EINVAL", err)
			}
			if _, err := w.sys.Readlink(ctx, w.path("missing")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing: err = %v, want ErrNotExist", err)
			}
		}},
	}
	for _, c := range cases {
		for _, mk := range []struct {
			name string
			new  func(*testing.T) world
		}{{"real", realWorld}, {"fake", fakeWorld}} {
			t.Run(c.name+"/"+mk.name, func(t *testing.T) { c.run(t, mk.new(t)) })
		}
	}
}

func wantFile(t *testing.T, w world, rel, data string, perm fs.FileMode) {
	t.Helper()
	ctx := context.Background()
	got, err := w.sys.ReadFile(ctx, w.path(rel))
	fi, serr := w.sys.Lstat(ctx, w.path(rel))
	if err != nil || serr != nil || string(got) != data || !fi.Mode().IsRegular() || fi.Mode().Perm() != perm {
		t.Errorf("%s = %q (%v), mode %v (%v); want a regular file %q, mode %v", rel, got, err, modeOf(fi), serr, data, perm)
	}
}

func wantLink(t *testing.T, w world, rel, target string) {
	t.Helper()
	fi, err := w.sys.Lstat(context.Background(), w.path(rel))
	if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("%s is %v (%v), want a symlink", rel, modeOf(fi), err)
	}
	if got, err := w.sys.Readlink(context.Background(), w.path(rel)); err != nil || got != target {
		t.Errorf("%s -> %q (%v), want %q", rel, got, err, target)
	}
}

func wantNames(t *testing.T, w world, rel string, names ...string) {
	t.Helper()
	entries, err := w.sys.ReadDir(context.Background(), w.path(rel))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if len(names) == 0 {
		names = nil
	}
	if !reflect.DeepEqual(got, names) {
		t.Errorf("%s/ holds %q, want %q", rel, got, names)
	}
}

// second returns the error of a ReadFileNoFollow call.
func second(_ []byte, _ fs.FileInfo, err error) error { return err }

func modeOf(fi fs.FileInfo) any {
	if fi == nil {
		return nil
	}
	return fi.Mode()
}
