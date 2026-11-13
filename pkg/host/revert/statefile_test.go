package revert

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestReadStateFile: a state file is read only where the directory and the
// file are root's alone, the file regular and within its limit.
func TestReadStateFile(t *testing.T) {
	ctx := context.Background()
	const name = "release.json"
	path := DefaultDir + "/" + name
	for _, tc := range []struct {
		name string
		set  func(f *hostfake.System)
		file string
		want error
	}{
		{name: "read", set: func(f *hostfake.System) { f.AddFile(path, []byte("{}"), 0o600, 0, 0) }},
		{name: "exactly the limit", set: func(f *hostfake.System) { f.AddFile(path, []byte(strings.Repeat("x", 16)), 0o600, 0, 0) }},
		{name: "no file", set: func(*hostfake.System) {}, want: fs.ErrNotExist},
		{name: "no directory", set: func(f *hostfake.System) { _ = f.RemoveAll(ctx, DefaultDir) }, want: fs.ErrNotExist},
		{name: "another user's file", set: func(f *hostfake.System) { f.AddFile(path, []byte("{}"), 0o600, 1000, 1000) }, want: ErrUnsafe},
		{name: "group-writable file", set: func(f *hostfake.System) { f.AddFile(path, []byte("{}"), 0o620, 0, 0) }, want: ErrUnsafe},
		{name: "a symlink", set: func(f *hostfake.System) {
			f.AddFile("/var/lib/other.json", []byte("{}"), 0o600, 0, 0)
			f.AddSymlink(path, "/var/lib/other.json")
		}, want: ErrUnsafe},
		{name: "a directory", set: func(f *hostfake.System) { f.AddDir(path, 0o700, 0, 0) }, want: ErrUnsafe},
		{name: "another user's directory", set: func(f *hostfake.System) {
			f.AddDir(DefaultDir, 0o700, 1000, 1000)
			f.AddFile(path, []byte("{}"), 0o600, 0, 0)
		}, want: ErrUnsafe},
		{name: "a world-writable parent", set: func(f *hostfake.System) {
			f.AddDir("/var/lib", 0o777, 0, 0)
			f.AddFile(path, []byte("{}"), 0o600, 0, 0)
		}, want: ErrUnsafe},
		{name: "over the limit", set: func(f *hostfake.System) { f.AddFile(path, []byte(strings.Repeat("x", 17)), 0o600, 0, 0) }, want: errTooLarge},
		{name: "a path, not a name", file: "../release.json", set: func(*hostfake.System) {}, want: fs.ErrInvalid},
		{name: "no name", file: ".", set: func(*hostfake.System) {}, want: fs.ErrInvalid},
	} {
		f := bareHost(t)
		f.AddDir(DefaultDir, 0o700, 0, 0)
		tc.set(f)
		file := tc.file
		if file == "" {
			file = name
		}
		data, err := ReadStateFile(ctx, f, Options{}, file, 16)
		if !errors.Is(err, tc.want) || (tc.want == nil && len(data) == 0) || (tc.want != nil && data != nil) {
			t.Errorf("%s: %q, %v; want %v", tc.name, data, err, tc.want)
		}
	}
}

// TestWriteStateFile: the file is written 0600 in a directory made 0700,
// replaces a symlink planted at its name without following it, and is
// refused where another user can change the directory.
func TestWriteStateFile(t *testing.T) {
	ctx := context.Background()
	path := DefaultDir + "/release.json"
	f := bareHost(t)
	if err := WriteStateFile(ctx, f, Options{}, "release.json", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.Entry(DefaultDir); d.Mode.Perm() != 0o700 {
		t.Errorf("directory %v", d.Mode)
	}
	if e, _ := f.Entry(path); string(e.Data) != "one" || e.Mode.Perm() != 0o600 || e.UID != 0 {
		t.Errorf("file %+v", e)
	}

	f.AddFile("/var/lib/other.json", []byte("theirs"), 0o600, 0, 0)
	f.AddSymlink(path, "/var/lib/other.json")
	if err := WriteStateFile(ctx, f, Options{}, "release.json", []byte("two")); err != nil {
		t.Fatal(err)
	}
	if e, _ := f.Entry(path); string(e.Data) != "two" || e.Target != "" {
		t.Errorf("over a symlink: %+v", e)
	}
	if e, _ := f.Entry("/var/lib/other.json"); string(e.Data) != "theirs" {
		t.Errorf("the link's target was written: %q", e.Data)
	}

	g := bareHost(t)
	g.AddDir(DefaultDir, 0o770, 0, 0)
	if err := WriteStateFile(ctx, g, Options{}, "release.json", []byte("x")); !errors.Is(err, ErrUnsafe) {
		t.Errorf("a group-writable directory: %v", err)
	}
	if _, ok := g.Entry(path); ok {
		t.Error("written into a directory another user can change")
	}
	if err := WriteStateFile(ctx, f, Options{}, "a/b", nil); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("a path: %v", err)
	}
}
