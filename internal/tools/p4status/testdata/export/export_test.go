// This file is not built with p4status: it is the exporter of
// ../hosts/*.json, a test of package main of internal/tools/uigallery that
// go test runs through -overlay, so that neither the gallery nor this tool
// imports the other (testdata/README.md says how to run it).

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

var exportDir = flag.String("export-dir", "", "write the status hosts' fixtures for internal/tools/p4status here")

// The fixture's form, as internal/tools/p4status's fixture_test.go reads it.
type (
	xEntry struct {
		Path   string `json:"path"`
		Type   string `json:"type"`
		Mode   string `json:"mode,omitempty"`
		UID    int    `json:"uid"`
		GID    int    `json:"gid"`
		Data   string `json:"data,omitempty"`
		Target string `json:"target,omitempty"`
	}
	xCommand struct {
		Argv   []string `json:"argv"`
		Stdout string   `json:"stdout,omitempty"`
		Stderr string   `json:"stderr,omitempty"`
		Exit   int      `json:"exit,omitempty"`
		Err    string   `json:"err,omitempty"`
	}
	xUser struct {
		Name string `json:"name"`
		UID  int    `json:"uid"`
		GID  int    `json:"gid"`
		Home string `json:"home"`
	}
	xGroup struct {
		Name string `json:"name"`
		GID  int    `json:"gid"`
	}
	xHost struct {
		Host     string     `json:"host"`
		Setuid   []string   `json:"setuid,omitempty"`
		Users    []xUser    `json:"users"`
		Groups   []xGroup   `json:"groups"`
		Commands []xCommand `json:"commands"`
		Entries  []xEntry   `json:"entries"`
	}
)

// TestExportStatusHosts reads each status host EXPORT_HOSTS names as the
// gallery does (readStatus) and writes what the reading used of it: every
// command the model answered, in order, with its answer; every file
// system entry the reading touched, the entries of each directory it
// listed, and the directories and link targets on their way; the setuid
// files; the accounts and groups of /etc/passwd and /etc/group.
func TestExportStatusHosts(t *testing.T) {
	if *exportDir == "" {
		t.Skip("no -export-dir")
	}
	ctx := context.Background()
	for _, name := range strings.Split(os.Getenv("EXPORT_HOSTS"), ",") {
		sh, ok := findStatusHost(name)
		if !ok {
			t.Fatalf("no status host %q", name)
		}
		h := sh.host()
		x := xHost{Host: name}
		answer := h.f.Handler
		h.f.Handler = func(argv []string, stdin []byte) hostfake.Response {
			r := answer(argv, stdin)
			c := xCommand{Argv: argv, Stdout: r.Stdout, Stderr: r.Stderr, Exit: r.ExitCode}
			switch {
			case errors.Is(r.Err, host.ErrNotFound):
				c.Err = "notfound"
			case r.Err != nil:
				c.Err = "other: " + r.Err.Error()
			}
			x.Commands = append(x.Commands, c)
			return r
		}
		if _, err := (&session{}).readStatus(ctx, h); err != nil {
			t.Fatal(err)
		}
		keep, listed, runs := map[string]bool{}, map[string]bool{}, 0
		var add func(p string)
		add = func(p string) {
			if p = path.Clean(p); p == "/" || keep[p] {
				return
			}
			keep[p] = true
			add(path.Dir(p))
			if e, ok := h.f.Entry(p); ok && e.Mode&fs.ModeSymlink != 0 {
				if !path.IsAbs(e.Target) {
					add(path.Join(path.Dir(p), e.Target))
				} else {
					add(e.Target)
				}
			}
		}
		for _, c := range h.f.Calls() {
			switch c.Op {
			case "run":
				runs++
			case "readdir":
				listed[c.Path] = true
				fallthrough
			case "read", "read-nofollow", "stat", "lstat", "readlink", "resolve-symlink":
				parts := strings.Split(strings.TrimPrefix(path.Clean(c.Path), "/"), "/")
				for i := range parts {
					add("/" + strings.Join(parts[:i+1], "/")) // each may be a link
				}
			case "lookup-user", "lookup-group":
			default:
				t.Fatalf("%s: a call that is not a read: %v", name, c)
			}
		}
		if runs != len(x.Commands) {
			t.Fatalf("%s: %d commands run, %d answered by the model", name, runs, len(x.Commands))
		}
		for d := range listed {
			des, _ := h.f.ReadDir(ctx, d)
			for _, de := range des {
				add(path.Join(d, de.Name()))
			}
		}
		sys := h.sys()
		for _, p := range slices.Sorted(maps.Keys(keep)) {
			e, ok := h.f.Entry(p)
			if !ok {
				continue
			}
			xe := xEntry{Path: p, UID: e.UID, GID: e.GID, Mode: fmt.Sprintf("%04o", uint32(e.Mode.Perm()))}
			switch {
			case e.Mode&fs.ModeSymlink != 0:
				xe = xEntry{Path: p, Type: "symlink", Target: e.Target}
			case e.Mode.IsDir():
				xe.Type = "dir"
			default:
				if !utf8.Valid(e.Data) {
					t.Fatalf("%s: %s is not UTF-8", name, p)
				}
				xe.Type, xe.Data = "file", string(e.Data)
				if fi, err := sys.Stat(ctx, p); err == nil && fi.Mode()&fs.ModeSetuid != 0 {
					x.Setuid = append(x.Setuid, p)
				}
			}
			x.Entries = append(x.Entries, xe)
		}
		users, groups := map[string]bool{"root": true}, map[string]bool{"root": true}
		for file, names := range map[string]map[string]bool{"/etc/passwd": users, "/etc/group": groups} {
			e, _ := h.f.Entry(file)
			for _, l := range strings.Split(string(e.Data), "\n") {
				if n, _, ok := strings.Cut(l, ":"); ok {
					names[n] = true
				}
			}
		}
		for _, n := range slices.Sorted(maps.Keys(users)) {
			if u, err := h.f.LookupUser(ctx, n); err == nil {
				x.Users = append(x.Users, xUser{u.Name, u.UID, u.GID, u.HomeDir})
			}
		}
		for _, n := range slices.Sorted(maps.Keys(groups)) {
			if g, err := h.f.LookupGroup(ctx, n); err == nil {
				x.Groups = append(x.Groups, xGroup{g.Name, g.GID})
			}
		}
		data, err := json.MarshalIndent(x, "", "\t")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(*exportDir, name+".json"), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
