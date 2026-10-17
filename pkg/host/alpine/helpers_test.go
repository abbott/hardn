package alpine_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// versions are the supported Alpine releases the tests run on.
var versions = []string{"3.24", "3.23"}

// facts are detect's facts for an Alpine VM of version: os-release from
// detect's fixture (aports alpine-base), and init from /run/openrc, both
// detected on a fake of their own; no container.
func facts(t *testing.T, version string) detect.Facts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "detect", "testdata", "os-release", "alpine-"+version))
	if err != nil {
		t.Fatal(err)
	}
	h := hostfake.New(host.Options{})
	h.AddFile(detect.EtcOSRelease, data, 0o644, 0, 0)
	h.AddDir(detect.OpenRCRunDir, 0o755, 0, 0)
	ctx := context.Background()
	f := detect.Facts{OS: detect.ReadOSRelease(ctx, h), Init: detect.DetectInit(ctx, h), Container: detect.Container{Detected: detect.No}}
	f.Platform = detect.Classify(f.OS, detect.Proxmox{Installed: detect.No})
	if f.Platform.Distro != detect.DistroAlpine || f.Platform.Support != detect.Supported || f.Init.Kind != detect.OpenRC || f.Init.Running != detect.Yes {
		t.Fatalf("facts for %s: %+v %+v", version, f.Platform, f.Init)
	}
	return f
}

// apkCron is the script apk-cron 1.0-r3 installs (aports main/apk-cron/apk).
const apkCron = "#!/bin/sh\n\nset -eu\n\nsleep $(expr $RANDOM % 7200)\nexec apk -U upgrade\n"

// world is a modelled Alpine host: apk, rc-update and rc-service act on
// the fake as the real tools would for these tests (their messages follow
// the tools' style; they were not captured from a host).
type world struct {
	h         *hostfake.System
	installed bool
	crond     string // started, stopped, crashed
	// inert names commands that succeed without their effect.
	inert map[string]bool
	// pending is a package change apk's next commit would make too, as
	// for a world file edited by hand and not yet applied.
	pending string
	// hook runs before a command is answered.
	hook func(argv string)
}

// stock is an Alpine host after setup-alpine: crond in the default
// runlevel and started (setup-alpine adds it), apk-cron not installed.
// legacy adds origin/main's apk-upgrade, as it wrote it.
func stock(t *testing.T, legacy bool) *world {
	t.Helper()
	w := &world{h: hostfake.New(host.Options{}), crond: "started", inert: map[string]bool{}}
	w.h.AddDir(alpine.DailyDir, 0o755, 0, 0)
	w.h.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	w.h.AddDir(detect.OpenRCRunlevels+"/default", 0o755, 0, 0)
	w.h.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
	w.h.AddDir("/var/backups", 0o755, 0, 0)
	if legacy {
		w.h.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent), 0o755, 0, 0)
	}
	w.h.Handler = w.answer
	return w
}

func (w *world) answer(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	if w.hook != nil {
		w.hook(cmd)
	}
	ctx := context.Background()
	effect := !w.inert[cmd]
	switch cmd {
	case "apk info -e apk-cron":
		if w.installed {
			return hostfake.Response{Stdout: "apk-cron\n"}
		}
		return hostfake.Response{ExitCode: 1}
	case "apk add --simulate apk-cron":
		return hostfake.Response{Stdout: w.commit("Installing", 26)}
	case "apk del --simulate apk-cron":
		return hostfake.Response{Stdout: w.commit("Purging", 25)}
	case "apk add apk-cron":
		if effect {
			w.installed = true
			w.h.AddFile(alpine.Script, []byte(apkCron), 0o755, 0, 0)
		}
		return hostfake.Response{Stdout: "(1/1) Installing apk-cron (1.0-r3)\nOK: 9 MiB in 26 packages\n"}
	case "apk del apk-cron":
		if effect {
			w.installed = false
			_ = w.h.Remove(ctx, alpine.Script)
		}
		return hostfake.Response{Stdout: "(1/1) Purging apk-cron (1.0-r3)\nOK: 9 MiB in 25 packages\n"}
	case "rc-update add crond default":
		if _, ok := w.h.Entry(detect.OpenRCRunlevels + "/default"); !ok {
			// rc-update reports the missing runlevel and still exits 0.
			return hostfake.Response{Stderr: " * rc-update: runlevel `default' does not exist\n"}
		}
		if effect {
			w.h.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
		}
		return hostfake.Response{Stdout: " * service crond added to runlevel default\n"}
	case "rc-update del crond default":
		if effect {
			_ = w.h.Remove(ctx, alpine.RunlevelLink)
		}
		return hostfake.Response{Stdout: " * service crond deleted from runlevel default\n"}
	case "rc-service crond status":
		switch w.crond {
		case "started":
			return hostfake.Response{Stdout: " * status: started\n"}
		case "crashed":
			return hostfake.Response{Stderr: " * status: crashed\n", ExitCode: 32}
		}
		return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}
	case "rc-service crond start":
		// A crashed service is still marked started: start only warns.
		if w.crond != "stopped" {
			return hostfake.Response{Stderr: " * WARNING: crond has already been started\n"}
		}
		if effect {
			w.crond = "started"
		}
		return hostfake.Response{Stdout: " * Starting busybox crond ... [ ok ]\n"}
	case "rc-service crond restart":
		if effect {
			w.crond = "started"
		}
		return hostfake.Response{Stdout: " * Starting busybox crond ... [ ok ]\n"}
	case "rc-service crond stop":
		if effect {
			w.crond = "stopped"
		}
		return hostfake.Response{Stdout: " * Stopping busybox crond ... [ ok ]\n"}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// commit is apk's commit output for apk-cron's change, after the pending
// one (apk-tools v3.0.8 src/commit.c, print_change).
func (w *world) commit(verb string, pkgs int) string {
	if w.pending == "" {
		return fmt.Sprintf("(1/1) %s apk-cron (1.0-r3)\nOK: 9 MiB in %d packages\n", verb, pkgs)
	}
	return fmt.Sprintf("(1/2) %s\n(2/2) %s apk-cron (1.0-r3)\nOK: 8 MiB in %d packages\n", w.pending, verb, pkgs-1)
}

// read reads the host's update state.
func (w *world) read(t *testing.T, f detect.Facts) alpine.Updates {
	t.Helper()
	u, err := alpine.ReadUpdates(context.Background(), w.h, f)
	if err != nil {
		t.Fatalf("ReadUpdates: %v", err)
	}
	return u
}

// plan reads and plans.
func (w *world) plan(t *testing.T, f detect.Facts) alpine.Plan {
	t.Helper()
	p, err := alpine.PlanUpdates(w.read(t, f))
	if err != nil {
		t.Fatalf("PlanUpdates: %v", err)
	}
	return p
}

// mark returns the number of calls so far, for since.
func (w *world) mark() int { return len(w.h.Calls()) }

// since lists the commands run and the paths changed after mark, in order.
func (w *world) since(mark int) []string {
	var out []string
	for _, c := range w.h.Calls()[mark:] {
		switch c.Op {
		case "run":
			out = append(out, strings.Join(c.Argv, " "))
		case "write-atomic", "write-in-place", "remove", "removeall", "mkdir", "rename", "chmod", "chown":
			out = append(out, c.Op+" "+c.Path)
		}
	}
	return out
}

// queries are the commands that change nothing.
var queries = []string{"apk info -e apk-cron", "rc-service crond status", "apk add --simulate apk-cron", "apk del --simulate apk-cron"}

// changes is since without the queries.
func (w *world) changes(mark int) []string {
	var out []string
	for _, s := range w.since(mark) {
		if !slices.Contains(queries, s) {
			out = append(out, s)
		}
	}
	return out
}

// faulty is a fake on which the listed operations fail: fail["lstat
// /etc/init.d/crond"] is the error, wrapped in a *fs.PathError, that Lstat
// of that path returns. after, when set, runs once an operation succeeds,
// to model another writer.
type faulty struct {
	*hostfake.System
	fail  map[string]error
	after func(op, name string)
}

func (f faulty) err(op, name string) error {
	if err := f.fail[op+" "+name]; err != nil {
		return &fs.PathError{Op: op, Path: name, Err: err}
	}
	return nil
}

func (f faulty) done(op, name string, err error) {
	if err == nil && f.after != nil {
		f.after(op, name)
	}
}

func (f faulty) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := f.err("lstat", name); err != nil {
		return nil, err
	}
	fi, err := f.System.Lstat(ctx, name)
	f.done("lstat", name, err)
	return fi, err
}

func (f faulty) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := f.err("stat", name); err != nil {
		return nil, err
	}
	fi, err := f.System.Stat(ctx, name)
	f.done("stat", name, err)
	return fi, err
}

func (f faulty) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := f.err("read-nofollow", name); err != nil {
		return nil, nil, err
	}
	data, fi, err := f.System.ReadFileNoFollow(ctx, name)
	f.done("read-nofollow", name, err)
	return data, fi, err
}

func (f faulty) Remove(ctx context.Context, name string) error {
	if err := f.err("remove", name); err != nil {
		return err
	}
	err := f.System.Remove(ctx, name)
	f.done("remove", name, err)
	return err
}

func (f faulty) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	if err := f.err("mkdir", name); err != nil {
		return err
	}
	err := f.System.MkdirAll(ctx, name, perm)
	f.done("mkdir", name, err)
	return err
}

var errDisk = errors.New("no space left on device")

func steps(p alpine.Plan) []string {
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Step.String())
	}
	return out
}
