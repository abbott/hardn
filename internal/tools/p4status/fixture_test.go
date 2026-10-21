package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// fixtureHosts are the hosts of testdata/hosts: the gallery's status hosts
// of the same names, as its model makes them (testdata/README.md).
var fixtureHosts = []string{"debian13", "ubuntu2604", "alpine324", "pve9", "hardened"}

// fixture is one testdata/hosts file: what the checks and the probe read
// of a host, and every command they ran with its answer, in order.
type fixture struct {
	Host     string    `json:"host"`
	Setuid   []string  `json:"setuid"`
	Users    []user    `json:"users"`
	Groups   []group   `json:"groups"`
	Commands []command `json:"commands"`
	Entries  []entry   `json:"entries"`
}

type user struct {
	Name string `json:"name"`
	UID  int    `json:"uid"`
	GID  int    `json:"gid"`
	Home string `json:"home"`
}

type group struct {
	Name string `json:"name"`
	GID  int    `json:"gid"`
}

type command struct {
	Argv   []string `json:"argv"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Exit   int      `json:"exit"`
	Err    string   `json:"err"` // "notfound", or "other: <text>"
}

type entry struct {
	Path   string `json:"path"`
	Type   string `json:"type"` // dir, file, symlink
	Mode   string `json:"mode"` // octal permissions
	UID    int    `json:"uid"`
	GID    int    `json:"gid"`
	Data   string `json:"data"`
	Target string `json:"target"`
}

// loadHost returns the fixture name as a recording fake, its commands
// scripted in order, and the host the checks read: the fake with the
// setuid bit on the fixture's setuid files, which hostfake does not keep.
func loadHost(t *testing.T, name string) (*hostfake.System, host.System) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hosts", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var x fixture
	if err := json.Unmarshal(data, &x); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	f := hostfake.New(host.Options{})
	for _, e := range x.Entries {
		mode, err := strconv.ParseUint(e.Mode, 8, 32)
		if err != nil && e.Type != "symlink" {
			t.Fatalf("%s: %s: mode %q", name, e.Path, e.Mode)
		}
		switch e.Type {
		case "dir":
			f.AddDir(e.Path, fs.FileMode(mode), e.UID, e.GID)
		case "file":
			f.AddFile(e.Path, []byte(e.Data), fs.FileMode(mode), e.UID, e.GID)
		case "symlink":
			f.AddSymlink(e.Path, e.Target)
		default:
			t.Fatalf("%s: %s: type %q", name, e.Path, e.Type)
		}
	}
	for _, u := range x.Users {
		f.AddUser(host.User{Name: u.Name, UID: u.UID, GID: u.GID, HomeDir: u.Home})
	}
	for _, g := range x.Groups {
		f.AddGroup(host.Group{Name: g.Name, GID: g.GID})
	}
	for _, c := range x.Commands {
		r := hostfake.Response{Stdout: c.Stdout, Stderr: c.Stderr, ExitCode: c.Exit}
		switch text, other := strings.CutPrefix(c.Err, "other: "); {
		case c.Err == "notfound":
			r.Err = fmt.Errorf("fixture: %s: %w", c.Argv[0], host.ErrNotFound)
		case other:
			r.Err = errors.New(text)
		case c.Err != "":
			t.Fatalf("%s: %v: err %q", name, c.Argv, c.Err)
		}
		f.Respond(r, c.Argv[0], c.Argv[1:]...)
	}
	return f, setuidSys{f, x.Setuid}
}

// setuidSys is a fake whose Stat shows the setuid bit on the files a
// fixture names, as the gallery's statusSys does: the users check counts
// only an elevation tool that is setuid root.
type setuidSys struct {
	*hostfake.System
	setuid []string
}

// Stat is the fake's, with the setuid bit on the fixture's files.
func (s setuidSys) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := s.System.Stat(ctx, name)
	if err == nil && slices.Contains(s.setuid, name) {
		fi = setuidInfo{fi}
	}
	return fi, err
}

// setuidInfo is a file's information with its setuid bit set.
type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }
