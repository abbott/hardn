package alpine_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestReadUpdates: on a stock host of each release, ReadUpdates runs only
// the two queries and makes only reads, and reports what is there.
func TestReadUpdates(t *testing.T) {
	for _, v := range versions {
		t.Run(v, func(t *testing.T) {
			w := stock(t, true)
			u := w.read(t, facts(t, v))
			if u.Package != detect.No || u.Script != alpine.ScriptAbsent || u.Runlevel != detect.Yes ||
				u.Crond != alpine.CrondStarted || u.Legacy != alpine.LegacyHardn || u.Err != nil {
				t.Errorf("Updates = %+v", u)
			}
			if f := u.LegacyFile; string(f.Data) != alpine.LegacyContent || f.Mode != 0o755 || f.UID != 0 || f.GID != 0 {
				t.Errorf("LegacyFile = %+v", f)
			}
			for _, c := range w.h.Calls() {
				switch c.Op {
				case "run", "lstat", "stat", "read-nofollow":
				default:
					t.Errorf("not a read: %s", c)
				}
			}
			if got, want := w.since(0), []string{"apk info -e apk-cron", "rc-service crond status"}; !slices.Equal(got, want) {
				t.Errorf("runs = %q, want %q", got, want)
			}
		})
	}
}

// TestReadUpdatesRefuses: another distribution, an unestablished one, an
// init system not established (never "OpenRC did not boot" then), and an
// Alpine that OpenRC did not boot (a container image, or systemd) are
// refused before anything is read.
func TestReadUpdatesRefuses(t *testing.T) {
	f := facts(t, "3.24")
	alter := func(p detect.Platform, in detect.Init) detect.Facts {
		g := f
		if p != (detect.Platform{}) {
			g.Platform = p
		}
		if in != (detect.Init{}) {
			g.Init = in
		}
		return g
	}
	for _, tc := range []struct {
		name  string
		f     detect.Facts
		want  error
		cause error // also wrapped
		not   error // never wrapped
	}{
		{"debian", alter(detect.Platform{Distro: detect.DistroDebian, Version: "13"}, detect.Init{}), alpine.ErrNotAlpine, nil, nil},
		{"distro unknown", alter(detect.Platform{Err: fs.ErrPermission}, detect.Init{}), alpine.ErrUnknown, fs.ErrPermission, alpine.ErrNotAlpine},
		{"init unknown", alter(detect.Platform{}, detect.Init{Err: fs.ErrPermission}), alpine.ErrUnknown, fs.ErrPermission, alpine.ErrNoOpenRC},
		{"init signals disagree", alter(detect.Platform{}, detect.Init{Err: detect.ErrConflict}), alpine.ErrUnknown, detect.ErrConflict, alpine.ErrNoOpenRC},
		{"container", alter(detect.Platform{}, detect.Init{Kind: detect.OpenRC, Running: detect.No, Source: detect.OpenRCRun}), alpine.ErrNoOpenRC, nil, alpine.ErrUnknown},
		{"systemd", alter(detect.Platform{}, detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir}), alpine.ErrNoOpenRC, nil, alpine.ErrUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := stock(t, false)
			_, err := alpine.ReadUpdates(context.Background(), w.h, tc.f)
			if !errors.Is(err, tc.want) || len(w.h.Calls()) != 0 {
				t.Errorf("err = %v (want %v), calls %v", err, tc.want, w.h.Calls())
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) || tc.not != nil && errors.Is(err, tc.not) {
				t.Errorf("err = %v: want %v wrapped, and never %v", err, tc.cause, tc.not)
			}
		})
	}
}

// errAny stands for any error in TestReadUpdatesUnknown.
var errAny = errors.New("any error")

// TestReadUpdatesUnknown: each host answer ReadUpdates cannot read for
// certain gives Unknown with the reason in Updates.Err, never a guess; a
// file replaced or removed between the Lstat and the read is read as what
// it became.
func TestReadUpdatesUnknown(t *testing.T) {
	denied := func(op, path string) func(*world) host.System {
		return func(w *world) host.System {
			return faulty{System: w.h, fail: map[string]error{op + " " + path: fs.ErrPermission}}
		}
	}
	readFails := func(err error) func(*world) host.System {
		return func(w *world) host.System {
			return faulty{System: w.h, fail: map[string]error{"read-nofollow " + alpine.LegacyScript: err}}
		}
	}
	respond := func(r hostfake.Response, argv ...string) func(*world) host.System {
		return func(w *world) host.System { w.h.Respond(r, argv[0], argv[1:]...); return w.h }
	}
	status := []string{"rc-service", "crond", "status"}
	info := []string{"apk", "info", "-e", "apk-cron"}
	pkg := func(want detect.Tri) func(alpine.Updates) bool {
		return func(u alpine.Updates) bool { return u.Package == want }
	}
	crond := func(u alpine.Updates) bool { return u.Crond == alpine.CrondUnknown }
	legacyIs := func(want alpine.Legacy) func(alpine.Updates) bool {
		return func(u alpine.Updates) bool { return u.Legacy == want }
	}
	for _, tc := range []struct {
		name  string
		setup func(*world) host.System
		want  func(alpine.Updates) bool
		cause error // Updates.Err wraps it; nil: Updates.Err is nil
	}{
		{"init script unreadable", denied("lstat", alpine.InitScript), crond, fs.ErrPermission},
		{"init script not a file", func(w *world) host.System {
			_ = w.h.Remove(context.Background(), alpine.InitScript)
			w.h.AddDir(alpine.InitScript, 0o755, 0, 0)
			return w.h
		}, crond, detect.ErrUnrecognised},
		{"crond stopping", respond(hostfake.Response{ExitCode: 4}, status...), crond, detect.ErrUnrecognised},
		{"crond starting", respond(hostfake.Response{ExitCode: 8}, status...), crond, detect.ErrUnrecognised},
		{"crond inactive", respond(hostfake.Response{ExitCode: 16}, status...), crond, detect.ErrUnrecognised},
		{"runlevel unreadable", denied("stat", alpine.RunlevelLink), func(u alpine.Updates) bool { return u.Runlevel == detect.Unknown }, fs.ErrPermission},
		{"script unreadable", denied("lstat", alpine.Script), func(u alpine.Updates) bool { return u.Script == alpine.ScriptUnknown }, fs.ErrPermission},
		{"legacy unreadable", denied("lstat", alpine.LegacyScript), legacyIs(alpine.LegacyUnknown), fs.ErrPermission},
		{"legacy read denied", readFails(fs.ErrPermission), legacyIs(alpine.LegacyUnknown), fs.ErrPermission},
		{"legacy under an untrusted link", readFails(host.ErrSymlinkParent), legacyIs(alpine.LegacyUnknown), host.ErrSymlinkParent},
		{"legacy replaced by a link", readFails(host.ErrSymlink), legacyIs(alpine.LegacyOther), nil},
		{"legacy removed meanwhile", readFails(fs.ErrNotExist), legacyIs(alpine.LegacyAbsent), nil},
		{"apk info, other output", respond(hostfake.Response{Stdout: "apk-tools\n"}, info...), pkg(detect.Unknown), detect.ErrUnrecognised},
		{"apk info, error", respond(hostfake.Response{Stderr: "ERROR: Unable to read database\n", ExitCode: 1}, info...), pkg(detect.Unknown), errAny},
		// The verbosity /etc/apk/config sets changes the output, not the
		// exit status.
		{"apk info, quiet", respond(hostfake.Response{}, info...), pkg(detect.Yes), nil},
		{"apk info, verbose", respond(hostfake.Response{Stdout: "apk-cron-1.0-r3\n"}, info...), pkg(detect.Yes), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := stock(t, true)
			u, err := alpine.ReadUpdates(context.Background(), tc.setup(w), facts(t, "3.24"))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.want(u) {
				t.Errorf("Updates = %+v", u)
			}
			if tc.cause == nil && u.Err != nil || tc.cause != nil && u.Err == nil || tc.cause != nil && tc.cause != errAny && !errors.Is(u.Err, tc.cause) {
				t.Errorf("Err = %v, want %v", u.Err, tc.cause)
			}
		})
	}
}

// configured is a host with apk-cron in place.
var configured = alpine.Updates{Package: detect.Yes, Script: alpine.ScriptInstalled, Runlevel: detect.Yes, Crond: alpine.CrondStarted, Legacy: alpine.LegacyAbsent}

func with(f func(*alpine.Updates)) alpine.Updates {
	u := configured
	f(&u)
	return u
}

// TestStatusAndPlan: the status for each state, and the steps PlanUpdates
// takes from it (pure).
func TestStatusAndPlan(t *testing.T) {
	errRead := errors.New("permission denied")
	legacyHardn := func(u *alpine.Updates) {
		u.Legacy, u.LegacyFile = alpine.LegacyHardn, alpine.File{Data: []byte(alpine.LegacyContent), Mode: 0o755}
	}
	for _, tc := range []struct {
		name    string
		u       alpine.Updates
		state   alpine.State
		detail  string
		steps   []string
		planErr error
	}{
		{"configured", configured, alpine.StateConfigured, "", nil, nil},
		{"stock setup-alpine", with(func(u *alpine.Updates) { u.Package, u.Script = detect.No, alpine.ScriptAbsent }),
			alpine.StateNotConfigured, "apk-cron is not installed", []string{"install"}, nil},
		{"origin/main host", with(func(u *alpine.Updates) { u.Package, u.Script = detect.No, alpine.ScriptAbsent; legacyHardn(u) }),
			alpine.StateNotConfigured, "", []string{"install", "remove-legacy"}, nil},
		{"legacy left after apk-cron", with(legacyHardn), alpine.StateConfigured, "", []string{"remove-legacy"}, nil},
		{"foreign apk-upgrade", with(func(u *alpine.Updates) { u.Legacy = alpine.LegacyOther }), alpine.StateConfigured, "", nil, nil},
		{"nothing", alpine.Updates{Package: detect.No, Script: alpine.ScriptAbsent, Runlevel: detect.No, Crond: alpine.CrondStopped, Legacy: alpine.LegacyAbsent},
			alpine.StateNotConfigured, "", []string{"install", "runlevel", "start"}, nil},
		{"not in runlevel", with(func(u *alpine.Updates) { u.Runlevel = detect.No }), alpine.StatePartial, "crond is not in the default runlevel", []string{"runlevel"}, nil},
		{"stopped", with(func(u *alpine.Updates) { u.Crond = alpine.CrondStopped }), alpine.StatePartial, "crond is not running", []string{"start"}, nil},
		{"crashed", with(func(u *alpine.Updates) { u.Crond = alpine.CrondCrashed }), alpine.StatePartial, "crond has crashed", []string{"restart"}, nil},
		{"no crond", with(func(u *alpine.Updates) { u.Crond = alpine.CrondMissing }), alpine.StatePartial, "crond is not installed", nil, alpine.ErrNoCrond},
		{"script deleted", with(func(u *alpine.Updates) { u.Script = alpine.ScriptAbsent }), alpine.StatePartial, "missing or not executable", nil, alpine.ErrScript},
		{"script not executable", with(func(u *alpine.Updates) { u.Script = alpine.ScriptOther }), alpine.StatePartial, "", nil, alpine.ErrScript},
		{"script without package", with(func(u *alpine.Updates) { u.Package = detect.No }), alpine.StateNotConfigured, "", nil, alpine.ErrScript},
		{"package unknown", with(func(u *alpine.Updates) { u.Package, u.Err = detect.Unknown, errRead }), alpine.StateUnknown, "", nil, alpine.ErrUnknown},
		{"crond unknown", with(func(u *alpine.Updates) { u.Crond, u.Err = alpine.CrondUnknown, errRead }), alpine.StateUnknown, "", nil, alpine.ErrUnknown},
		// A condition that fails decides Partial whatever is Unknown.
		{"partial despite unknown", with(func(u *alpine.Updates) { u.Runlevel, u.Crond, u.Err = detect.No, alpine.CrondUnknown, errRead }),
			alpine.StatePartial, "", nil, alpine.ErrUnknown},
		// Not installed decides NotConfigured whatever is Unknown.
		{"not installed, legacy unknown", with(func(u *alpine.Updates) {
			u.Package, u.Script, u.Legacy, u.Err = detect.No, alpine.ScriptAbsent, alpine.LegacyUnknown, errRead
		}),
			alpine.StateNotConfigured, "", nil, alpine.ErrUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.u.Status()
			if s.State != tc.state || !strings.Contains(s.Detail, tc.detail) {
				t.Errorf("Status = %s %q, want %s %q", s.State, s.Detail, tc.state, tc.detail)
			}
			if s.State == alpine.StateUnknown && !errors.Is(s.Err, errRead) {
				t.Errorf("Status.Err = %v, want the cause", s.Err)
			}
			p, err := alpine.PlanUpdates(tc.u)
			if !errors.Is(err, tc.planErr) || err == nil && !slices.Equal(steps(p), tc.steps) {
				t.Errorf("PlanUpdates = %q, %v; want %q, %v", steps(p), err, tc.steps, tc.planErr)
			}
			if errors.Is(err, alpine.ErrUnknown) && !errors.Is(err, errRead) {
				t.Errorf("PlanUpdates: %v, want the cause", err)
			}
		})
	}
}

// TestFindings: origin/main's file is reported as removed after apk-cron
// is in; any other file at its path as left alone.
func TestFindings(t *testing.T) {
	for _, tc := range []struct {
		content string
		mode    fs.FileMode
		uid     int
		want    string
	}{
		{alpine.LegacyContent, 0o755, 0, "alpine-legacy-script"},
		{alpine.LegacyContent, 0o700, 0, "alpine-legacy-script"},                        // a mode only root can write; the rollback keeps it
		{alpine.LegacyContent + "apk cache clean\n", 0o755, 0, "alpine-foreign-script"}, // edited by the administrator
		{"#!/bin/sh\r\napk update && apk upgrade --available\r\n", 0o755, 0, "alpine-foreign-script"},
		// hardn's bytes, but a user other than root can change them.
		{alpine.LegacyContent, 0o775, 0, "alpine-foreign-script"},
		{alpine.LegacyContent, 0o757, 0, "alpine-foreign-script"},
		{alpine.LegacyContent, 0o755, 1000, "alpine-foreign-script"},
	} {
		w := stock(t, false)
		w.h.AddFile(alpine.LegacyScript, []byte(tc.content), tc.mode, tc.uid, tc.uid)
		p := w.plan(t, facts(t, "3.24"))
		if len(p.Findings) != 1 || p.Findings[0].Kind.String() != tc.want || p.Findings[0].Path != alpine.LegacyScript {
			t.Errorf("%q %v %d: findings = %+v, want %s", tc.content, tc.mode, tc.uid, p.Findings, tc.want)
		}
		if removes := slices.Contains(steps(p), "remove-legacy"); removes != (tc.want == "alpine-legacy-script") {
			t.Errorf("%q %v %d: steps = %q", tc.content, tc.mode, tc.uid, steps(p))
		}
	}
	// A link at the path is never followed nor removed.
	w := stock(t, false)
	w.h.AddFile("/root/apk-upgrade", []byte(alpine.LegacyContent), 0o755, 0, 0)
	w.h.AddSymlink(alpine.LegacyScript, "/root/apk-upgrade")
	if u := w.read(t, facts(t, "3.24")); u.Legacy != alpine.LegacyOther {
		t.Errorf("symlink: Legacy = %v", u.Legacy)
	}
}

// TestCodes pins the words of the enums, for p3-output's mapping, and
// their numbers, which a Plan kept as JSON holds.
func TestCodes(t *testing.T) {
	var got []string
	for s := alpine.StateUnknown; s <= alpine.StateNotApplicable; s++ {
		got = append(got, s.String())
	}
	for s := alpine.StepInstall; s <= alpine.StepRemoveLegacy; s++ {
		got = append(got, s.String())
	}
	for c := alpine.CrondUnknown; c <= alpine.CrondCrashed+1; c++ {
		got = append(got, c.String())
	}
	got = append(got, alpine.FindingLegacyScript.String(), alpine.FindingForeignScript.String())
	for b := alpine.BootUnknown; b <= alpine.BootRPi; b++ {
		got = append(got, b.String())
	}
	want := []string{
		"unknown", "configured", "partial", "not-configured", "not-applicable",
		"install", "runlevel", "start", "restart", "remove-legacy",
		"unknown", "missing", "stopped", "started", "crashed", "unknown",
		"alpine-legacy-script", "alpine-foreign-script",
		"unknown", "extlinux", "grub", "limine", "efi-stub", "raspberry-pi",
	}
	if !slices.Equal(got, want) {
		t.Errorf("codes = %q\nwant    %q", got, want)
	}
	// A Plan kept in p2-revert's journal holds the numbers, not the words:
	// they are pinned too.
	nums := []uint8{
		uint8(alpine.StateUnknown), uint8(alpine.StateConfigured), uint8(alpine.StatePartial), uint8(alpine.StateNotConfigured), uint8(alpine.StateNotApplicable),
		uint8(alpine.StepInstall), uint8(alpine.StepRunlevel), uint8(alpine.StepStart), uint8(alpine.StepRestart), uint8(alpine.StepRemoveLegacy),
		uint8(alpine.CrondUnknown), uint8(alpine.CrondMissing), uint8(alpine.CrondStopped), uint8(alpine.CrondStarted), uint8(alpine.CrondCrashed),
		uint8(alpine.ScriptUnknown), uint8(alpine.ScriptAbsent), uint8(alpine.ScriptInstalled), uint8(alpine.ScriptOther),
		uint8(alpine.LegacyUnknown), uint8(alpine.LegacyAbsent), uint8(alpine.LegacyHardn), uint8(alpine.LegacyOther),
		uint8(alpine.FindingLegacyScript), uint8(alpine.FindingForeignScript),
	}
	wantNums := []uint8{0, 1, 2, 3, 4, 1, 2, 3, 4, 5, 0, 1, 2, 3, 4, 0, 1, 2, 3, 0, 1, 2, 3, 1, 2}
	if !slices.Equal(nums, wantNums) {
		t.Errorf("values = %v\nwant     %v", nums, wantNums)
	}
}
