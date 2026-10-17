package alpine_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
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

// TestReadUpdatesRefuses: another distribution, an unestablished one, and
// an Alpine that OpenRC did not boot (a container image) are refused
// before anything is read.
func TestReadUpdatesRefuses(t *testing.T) {
	f := facts(t, "3.24")
	debian := f
	debian.Platform = detect.Platform{Distro: detect.DistroDebian, Version: "13"}
	unknown := f
	unknown.Platform = detect.Platform{Err: fs.ErrPermission}
	container := f
	container.Init = detect.Init{Kind: detect.OpenRC, Running: detect.No, Source: detect.OpenRCRun}
	for _, tc := range []struct {
		name string
		f    detect.Facts
		want error
	}{
		{"debian", debian, alpine.ErrNotAlpine},
		{"distro unknown", unknown, alpine.ErrUnknown},
		{"container", container, alpine.ErrNoOpenRC},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := stock(t, false)
			_, err := alpine.ReadUpdates(context.Background(), w.h, tc.f)
			if !errors.Is(err, tc.want) || len(w.h.Calls()) != 0 {
				t.Errorf("err = %v (want %v), calls %v", err, tc.want, w.h.Calls())
			}
			if tc.name == "distro unknown" && (!errors.Is(err, fs.ErrPermission) || errors.Is(err, alpine.ErrNotAlpine)) {
				t.Errorf("err = %v: want the cause, and never 'not Alpine'", err)
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
		want    string
	}{
		{alpine.LegacyContent, 0o755, "alpine-legacy-script"},
		{alpine.LegacyContent, 0o700, "alpine-legacy-script"},                        // the mode does not decide; the rollback keeps it
		{alpine.LegacyContent + "apk cache clean\n", 0o755, "alpine-foreign-script"}, // edited by the administrator
		{"#!/bin/sh\r\napk update && apk upgrade --available\r\n", 0o755, "alpine-foreign-script"},
	} {
		w := stock(t, false)
		w.h.AddFile(alpine.LegacyScript, []byte(tc.content), tc.mode, 0, 0)
		p := w.plan(t, facts(t, "3.24"))
		if len(p.Findings) != 1 || p.Findings[0].Kind.String() != tc.want || p.Findings[0].Path != alpine.LegacyScript {
			t.Errorf("%q: findings = %+v, want %s", tc.content, p.Findings, tc.want)
		}
		if removes := slices.Contains(steps(p), "remove-legacy"); removes != (tc.want == "alpine-legacy-script") {
			t.Errorf("%q: steps = %q", tc.content, steps(p))
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

// TestCodes pins the words of the in-memory enums, for p3-output's
// mapping.
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
}
