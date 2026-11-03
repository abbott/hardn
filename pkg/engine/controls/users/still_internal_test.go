package users

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestHiding: StillAdmin's System hides the files it is told, from every
// read, and blanks the lines it is told, their numbers kept; every other
// file, line and directory entry reads as on the host, and nothing is
// written.
func TestHiding(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	h.AddDir("/etc", 0o755, 0, 0)
	h.AddDir("/etc/d", 0o750, 0, 0)
	for name, data := range map[string]string{"/etc/d/a": "a1\n", "/etc/d/b": "b1\nb2\nb3\n", "/etc/d/c": "c1\nc2\n"} {
		h.AddFile(name, []byte(data), 0o440, 0, 0)
	}
	hs := hiding{System: h, files: []string{"/etc/d/a"}, lines: map[string][]int{"/etc/d/b": {2}}}
	if _, err := hs.ReadFile(ctx, "/etc/d/a"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a: %v", err)
	}
	if _, _, err := hs.ReadFileNoFollow(ctx, "/etc/d/a"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a, no follow: %v", err)
	}
	for _, stat := range []func(context.Context, string) (fs.FileInfo, error){hs.Stat, hs.Lstat} {
		if _, err := stat(ctx, "/etc/d/a"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("stat a: %v", err)
		}
		if _, err := stat(ctx, "/etc/d/b"); err != nil {
			t.Errorf("stat b: %v", err)
		}
	}
	if data, err := hs.ReadFile(ctx, "/etc/d/b"); err != nil || string(data) != "b1\n\nb3\n" {
		t.Errorf("b: %q, %v", data, err)
	}
	if data, _, err := hs.ReadFileNoFollow(ctx, "/etc/d/b"); err != nil || string(data) != "b1\n\nb3\n" {
		t.Errorf("b, no follow: %q, %v", data, err)
	}
	if data, err := hs.ReadFile(ctx, "/etc/d/c"); err != nil || string(data) != "c1\nc2\n" {
		t.Errorf("c: %q, %v", data, err)
	}
	var lines []string
	if _, err := hs.ScanLines(ctx, "/etc/d/b", func(l host.Line) bool { lines = append(lines, string(l.Text)); return true }); err != nil || !slices.Equal(lines, []string{"b1", "", "b3"}) {
		t.Errorf("scan b: %q, %v", lines, err)
	}
	if _, err := hs.ScanLines(ctx, "/etc/d/a", func(host.Line) bool { return true }); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("scan a: %v", err)
	}
	es, err := hs.ReadDir(ctx, "/etc/d")
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	if err != nil || !slices.Equal(names, []string{"b", "c"}) {
		t.Errorf("readdir: %q, %v", names, err)
	}
	for _, c := range h.Calls() {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "scan-lines":
		default:
			t.Errorf("a call that is not a read: %v", c)
		}
	}
}
