package alpine_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// vmOpenSSH are the openssh packages of the Alpine 3.23 VM (the CVE run's
// baseline, vm-runs/cve/run-alpine323.log), all at one version.
var vmOpenSSH = []string{"openssh", "openssh-client-common", "openssh-client-default", "openssh-keygen", "openssh-server",
	"openssh-server-common", "openssh-server-common-openrc", "openssh-server-pam", "openssh-sftp-server"}

// sshWorld is an Alpine 3.23 host with the VM's openssh packages at
// version, and musl, which no upgrade here changes; apk update and apk
// upgrade answer as apk-tools 3.0.8 did on the VM (vm-runs/cve/out/
// 4b-upgrade-all.txt and the 2026-11-08 probe), the repository offering
// offer.
type sshWorld struct {
	h       *hostfake.System
	version string
	offer   string
	// extra is a change the simulation lists beside openssh's (a
	// library's upgrade), and the commit makes.
	extra string
	// simOut, when set, is the simulation's whole output.
	simOut      string
	updateFails bool
	inert       bool // apk upgrade succeeds and changes nothing
}

func newSSHWorld(version, offer string) *sshWorld {
	w := &sshWorld{h: hostfake.New(host.Options{}), version: version, offer: offer}
	w.write(version)
	w.h.Handler = w.answer
	return w
}

// write writes the installed database with every openssh package at v.
func (w *sshWorld) write(v string) {
	var b strings.Builder
	b.WriteString("C:Q1abc=\nP:musl\nV:1.2.5-r21\nA:x86_64\n\n")
	for _, n := range vmOpenSSH {
		b.WriteString("C:Q1def=\nP:" + n + "\nV:" + v + "\nA:x86_64\n\n")
	}
	w.h.AddFile(alpine.InstalledDB, []byte(b.String()), 0o644, 0, 0)
	w.version = v
}

// listing is apk's list of the upgrades of every openssh package but the
// meta package's position, as the VM listed them, counted.
func (w *sshWorld) listing() string {
	var lines []string
	if w.offer != w.version {
		for _, n := range vmOpenSSH {
			lines = append(lines, "Upgrading "+n+" ("+w.version+" -> "+w.offer+")")
		}
	}
	if w.extra != "" {
		lines = append(lines, w.extra)
	}
	var b strings.Builder
	for i, l := range lines {
		b.WriteString("(" + strconv.Itoa(i+1) + "/" + strconv.Itoa(len(lines)) + ") " + l + "\n")
	}
	b.WriteString("OK: 136.9 MiB in 207 packages\n")
	return b.String()
}

func (w *sshWorld) answer(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	names := strings.Join(vmOpenSSH, " ")
	switch cmd {
	case "apk update":
		if w.updateFails {
			return hostfake.Response{Stderr: "WARNING: fetching https://dl-cdn.alpinelinux.org/alpine/v3.23/main: temporary error (try again later)\n", ExitCode: 1}
		}
		return hostfake.Response{Stdout: "v3.23.6-81-g0d6bcbc5a71 [https://dl-cdn.alpinelinux.org/alpine/v3.23/main]\nOK: 26371 distinct packages available\n"}
	case "apk upgrade --simulate " + names:
		if w.simOut != "" {
			return hostfake.Response{Stdout: w.simOut}
		}
		return hostfake.Response{Stdout: w.listing()}
	case "apk upgrade " + names:
		out := w.listing()
		if !w.inert {
			w.write(w.offer)
		}
		return hostfake.Response{Stdout: out}
	}
	return hostfake.Response{Stderr: "unexpected: " + cmd, ExitCode: 127}
}

// commands are the commands the fake ran, as lines.
func commands(h *hostfake.System) []string {
	var out []string
	for _, c := range h.Calls() {
		if c.Op == "run" {
			out = append(out, strings.Join(c.Argv, " "))
		}
	}
	return out
}

// TestPlanUpgrade: a fixed server plans nothing; one not established is
// ErrUnknown; an unfixed one plans apk update and apk upgrade of every
// openssh package, sorted, the meta package and musl's neighbours alone;
// planning runs nothing.
func TestPlanUpgrade(t *testing.T) {
	ctx := context.Background()
	t.Run("fixed", func(t *testing.T) {
		w := newSSHWorld("10.2_p1-r1", "10.2_p1-r1")
		u, err := alpine.PlanUpgrade(ctx, w.h, alpine.OpenSSHServer, "3.23.6")
		if err != nil || !u.Empty() || u.Commands() != nil || u.From.Fixed != detect.Yes {
			t.Fatalf("PlanUpgrade = %+v, %v; want empty, nil", u, err)
		}
	})
	t.Run("not established", func(t *testing.T) {
		h := hostfake.New(host.Options{})
		u, err := alpine.PlanUpgrade(ctx, h, alpine.OpenSSHServer, "3.23.6")
		if !errors.Is(err, alpine.ErrUnknown) || !strings.Contains(err.Error(), "/lib/apk/db/installed is missing") || !u.Empty() {
			t.Fatalf("PlanUpgrade = %+v, %v; want ErrUnknown, the database missing", u, err)
		}
	})
	t.Run("unfixed", func(t *testing.T) {
		w := newSSHWorld("10.2_p1-r0", "10.2_p1-r1")
		u, err := alpine.PlanUpgrade(ctx, w.h, alpine.OpenSSHServerPAM, "3.23.6")
		if err != nil || u.Empty() {
			t.Fatalf("PlanUpgrade = %+v, %v", u, err)
		}
		if got := u.Packages(); !slices.Equal(got, vmOpenSSH) {
			t.Errorf("Packages = %v, want %v", got, vmOpenSSH)
		}
		want := [][]string{{"apk", "update"}, append([]string{"apk", "upgrade"}, vmOpenSSH...)}
		if got := u.Commands(); len(got) != 2 || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
			t.Errorf("Commands = %v, want %v", got, want)
		}
		if got, want := strings.Join(u.Simulate(), " "), "apk upgrade --simulate "+strings.Join(vmOpenSSH, " "); got != want {
			t.Errorf("Simulate = %s, want %s", got, want)
		}
		if u.From.Package != alpine.OpenSSHServerPAM || u.From.Wanted != "10.2_p1-r1" || u.From.Installed != "10.2_p1-r0" {
			t.Errorf("From = %+v", u.From)
		}
		if got := commands(w.h); len(got) != 0 {
			t.Errorf("planning ran %v", got)
		}
	})
}

// TestApplyUpgrade: the upgrade runs apk update, the simulation, then apk
// upgrade, and the server then reads fixed.
func TestApplyUpgrade(t *testing.T) {
	ctx := context.Background()
	w := newSSHWorld("10.2_p1-r0", "10.2_p1-r1")
	u, err := alpine.PlanUpgrade(ctx, w.h, alpine.OpenSSHServer, "3.23.6")
	if err != nil {
		t.Fatal(err)
	}
	res, err := alpine.ApplyUpgrade(ctx, w.h, u, "3.23.6")
	if err != nil {
		t.Fatalf("ApplyUpgrade: %v", err)
	}
	names := strings.Join(vmOpenSSH, " ")
	if got, want := commands(w.h), []string{"apk update", "apk upgrade --simulate " + names, "apk upgrade " + names}; !slices.Equal(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}
	if !res.Updated || !res.Upgraded || len(res.Changes) != len(vmOpenSSH) || res.Fix.Fixed != detect.Yes || res.Fix.Installed != "10.2_p1-r1" {
		t.Errorf("result = %+v", res)
	}
	if res.Changes[4] != "Upgrading openssh-server (10.2_p1-r0 -> 10.2_p1-r1)" {
		t.Errorf("Changes[4] = %q", res.Changes[4])
	}
}

// TestApplyUpgradeRefuses: each refusal upgrades nothing (no apk upgrade
// without --simulate): the packages changed since the plan, before apk
// update; apk update failing; a change that is not an openssh package's
// upgrade, named; an openssh package from another version than read; no
// upgrade of the server; the server's upgrade to a version without the
// fix; output with no change and no summary.
func TestApplyUpgradeRefuses(t *testing.T) {
	ctx := context.Background()
	names := strings.Join(vmOpenSSH, " ")
	for _, tc := range []struct {
		name    string
		from    string // "": 10.2_p1-r0, offered 10.2_p1-r1, on 3.23.6
		offer   string
		id      string
		set     func(w *sshWorld)
		want    error
		words   string // in the error
		updated bool   // apk update ran
		other   []string
	}{
		{name: "changed since the plan", set: func(w *sshWorld) { w.write("10.2_p1-r1") }, want: alpine.ErrPackagesChanged,
			words: "the openssh packages are openssh 10.2_p1-r1"},
		{name: "apk update fails", set: func(w *sshWorld) { w.updateFails = true }, want: alpine.ErrUpdate, words: "apk update"},
		{name: "a library too", set: func(w *sshWorld) { w.extra = "Upgrading libcrypto3 (3.5.4-r0 -> 3.5.5-r0)" }, want: alpine.ErrNotOpenSSH,
			words: "Upgrading libcrypto3 (3.5.4-r0 -> 3.5.5-r0)", updated: true, other: []string{"libcrypto3"}},
		{name: "a new package", set: func(w *sshWorld) { w.extra = "Installing openssh-server-krb5 (10.2_p1-r1)" }, want: alpine.ErrNotOpenSSH,
			updated: true, other: []string{"openssh-server-krb5"}},
		{name: "from another version", set: func(w *sshWorld) {
			w.simOut = "(1/1) Upgrading openssh-server (10.2_p1-r00 -> 10.2_p1-r1)\nOK: 136.9 MiB in 207 packages\n"
		}, want: alpine.ErrNotOpenSSH, updated: true, other: []string{"openssh-server"}},
		{name: "no upgrade offered", set: func(w *sshWorld) { w.offer = w.version }, want: alpine.ErrNoFixOffered,
			words: "no upgrade of openssh-server 10.2_p1-r0 listed", updated: true},
		{name: "the server not among them", set: func(w *sshWorld) {
			w.simOut = "(1/1) Upgrading openssh-sftp-server (10.2_p1-r0 -> 10.2_p1-r1)\nOK: 136.9 MiB in 207 packages\n"
		}, want: alpine.ErrNoFixOffered, updated: true},
		{name: "an upgrade without the fix", from: "10.3_p1-r0", offer: "10.3_p1-r1", id: "3.24.2", set: func(*sshWorld) {},
			want: alpine.ErrNoFixOffered, words: "the newest openssh-server is 10.3_p1-r1, and the fix is in 10.3_p1-r2", updated: true},
		{name: "unrecognised output", set: func(w *sshWorld) { w.simOut = "ERROR: something apk says\n" }, want: detect.ErrUnrecognised, updated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, offer, id := "10.2_p1-r0", "10.2_p1-r1", "3.23.6"
			if tc.from != "" {
				from, offer, id = tc.from, tc.offer, tc.id
			}
			w := newSSHWorld(from, offer)
			u, err := alpine.PlanUpgrade(ctx, w.h, alpine.OpenSSHServer, id)
			if err != nil {
				t.Fatal(err)
			}
			tc.set(w)
			res, err := alpine.ApplyUpgrade(ctx, w.h, u, id)
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.words) {
				t.Fatalf("ApplyUpgrade = %v; want %v with %q", err, tc.want, tc.words)
			}
			if slices.Contains(commands(w.h), "apk upgrade "+names) || res.Upgraded {
				t.Errorf("apk upgrade ran: %v", commands(w.h))
			}
			if res.Updated != tc.updated {
				t.Errorf("Updated = %v, want %v", res.Updated, tc.updated)
			}
			if errors.Is(tc.want, alpine.ErrPackagesChanged) && len(commands(w.h)) > 0 {
				t.Errorf("a changed host ran %v", commands(w.h))
			}
			var oc *alpine.OtherChangesError
			if tc.other != nil && (!errors.As(err, &oc) || !slices.Equal(oc.Names(), tc.other)) {
				t.Errorf("other changes = %+v, want names %v", oc, tc.other)
			}
		})
	}
}

// TestApplyUpgradeNotEffective: apk upgrade succeeding without upgrading
// is ErrNotEffective, naming the version still installed.
func TestApplyUpgradeNotEffective(t *testing.T) {
	ctx := context.Background()
	w := newSSHWorld("10.2_p1-r0", "10.2_p1-r1")
	w.inert = true
	u, err := alpine.PlanUpgrade(ctx, w.h, alpine.OpenSSHServer, "3.23.6")
	if err != nil {
		t.Fatal(err)
	}
	res, err := alpine.ApplyUpgrade(ctx, w.h, u, "3.23.6")
	if !errors.Is(err, alpine.ErrNotEffective) || !strings.Contains(err.Error(), "openssh-server 10.2_p1-r0, fixed in 10.2_p1-r1") || !res.Upgraded {
		t.Fatalf("ApplyUpgrade = %+v, %v; want ErrNotEffective", res, err)
	}
}

// TestApplyUpgradeEmpty: an empty plan runs nothing.
func TestApplyUpgradeEmpty(t *testing.T) {
	w := newSSHWorld("10.2_p1-r1", "10.2_p1-r1")
	if _, err := alpine.ApplyUpgrade(context.Background(), w.h, alpine.Upgrade{}, "3.23.6"); err != nil || len(commands(w.h)) != 0 {
		t.Fatalf("ApplyUpgrade(empty) = %v, ran %v", err, commands(w.h))
	}
}

// TestListenerBinary: the listener's binary from the pid file's pid, stale
// once the kernel marks it deleted (the 2026-11-08 probe: "/usr/sbin/sshd
// (deleted)" after the server packages were reinstalled); errors for a pid
// file missing or not holding a pid, and a process gone.
func TestListenerBinary(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, pid, exe string
		want           string
		stale, fails   bool
	}{
		{name: "current", pid: "21717\n", exe: "/usr/sbin/sshd", want: "/usr/sbin/sshd"},
		{name: "stale", pid: "10206\n", exe: "/usr/sbin/sshd (deleted)", want: "/usr/sbin/sshd", stale: true},
		{name: "the pam build, stale", pid: "10206\n", exe: "/usr/sbin/sshd.pam (deleted)", want: "/usr/sbin/sshd.pam", stale: true},
		{name: "no pid file", fails: true},
		{name: "not a pid", pid: "sshd\n", fails: true},
		{name: "gone", pid: "4242\n", fails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			if tc.pid != "" {
				h.AddFile(alpine.SSHDPidFile, []byte(tc.pid), 0o644, 0, 0)
			}
			if tc.exe != "" {
				h.AddSymlink("/proc/"+strings.TrimSpace(tc.pid)+"/exe", tc.exe)
			}
			got, stale, err := alpine.ListenerBinary(ctx, h)
			if tc.fails {
				if err == nil {
					t.Fatalf("ListenerBinary = %q, %v, nil; want an error", got, stale)
				}
				return
			}
			if err != nil || got != tc.want || stale != tc.stale {
				t.Fatalf("ListenerBinary = %q, %v, %v; want %q, %v", got, stale, err, tc.want, tc.stale)
			}
		})
	}
}
