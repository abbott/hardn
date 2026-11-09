package sshchange_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
)

// opensshVM are the openssh packages of the Alpine 3.23 VM
// (vm-runs/cve/run-alpine323.log).
var opensshVM = []string{"openssh", "openssh-client-common", "openssh-client-default", "openssh-keygen", "openssh-server",
	"openssh-server-common", "openssh-server-common-openrc", "openssh-server-pam", "openssh-sftp-server"}

// apkVM is the Alpine 3.23 VM as the upgrade sees it: apk's installed
// database, apk update and apk upgrade answering as apk-tools 3.0.8 did
// there, and OpenRC's sshd, whose listener's /proc/<pid>/exe reads
// "(deleted)" once an upgrade replaced its binary, and its file again once
// rc-service sshd reload ran (the 2026-11-08 probe).
type apkVM struct {
	t       *testing.T
	f       *hostfake.System
	version string
	offer   string
	pid     int
	// pam: sshd.pam installed and UsePAM yes, so the init script runs it.
	pam bool
	// extra is a change apk lists beside openssh's.
	extra string
	// reloadKills: rc-service sshd reload leaves sshd stopped.
	reloadKills bool
	// staleAfterReload: the reload leaves the listener on the old binary.
	staleAfterReload bool
	// restarting: the reload leaves sshd mid-restart, its pid file
	// removed, as sshd's SIGHUP handler does, until the check's next wait.
	restarting, pending bool
	waits               int
}

// upgrade is the control with the check's waits counted on v, each
// finishing a restart the reload left pending.
func (v *apkVM) upgrade() sshchange.Upgrade {
	return sshchange.Upgrade{Wait: func(context.Context, time.Duration) error {
		v.waits++
		if v.pending {
			v.pending = false
			v.listener(false)
		}
		return nil
	}}
}

func newApkVM(t *testing.T, version, offer string, pam bool) *apkVM {
	v := &apkVM{t: t, f: hostfake.New(host.Options{}), version: version, offer: offer, pid: 10206, pam: pam}
	for _, d := range []string{"/etc", "/etc/ssh", "/usr/sbin", "/run", "/var/lib", "/var/backups", "/var/log", "/usr/local/bin", "/lib/apk/db",
		detect.OpenRCRunDir, detect.OpenRCStarted, revert.OpenRCInitDir, revert.OpenRCDefault, revert.ProcDir} {
		v.f.AddDir(d, 0o755, 0, 0)
	}
	v.f.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	v.f.AddFile(reload.BinarySSHD, []byte("ELF"), 0o755, 0, 0)
	v.f.AddDir("/etc/init.d", 0o755, 0, 0)
	v.f.AddFile("/etc/init.d/sshd", []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	if pam {
		v.f.AddFile(reload.BinaryPAM, []byte("ELF"), 0o755, 0, 0)
	}
	v.f.AddSymlink(detect.OpenRCStarted+"/sshd", "/etc/init.d/sshd")
	v.f.AddDir(detect.OpenRCRunlevels+"/default", 0o755, 0, 0)
	v.f.AddSymlink(detect.OpenRCRunlevels+"/default/sshd", "/etc/init.d/sshd")
	v.write(version)
	v.listener(false)
	v.f.Handler = v.run
	return v
}

func (v *apkVM) bin() string {
	if v.pam {
		return reload.BinaryPAM
	}
	return reload.BinarySSHD
}

// listener writes the pid file and the listener's exe link.
func (v *apkVM) listener(stale bool) {
	v.f.AddFile(alpine.SSHDPidFile, []byte(strconv.Itoa(v.pid)+"\n"), 0o644, 0, 0)
	exe := v.bin()
	if stale {
		exe += " (deleted)"
	}
	_ = v.f.RemoveAll(context.Background(), "/proc/"+strconv.Itoa(v.pid))
	v.f.AddDir("/proc/"+strconv.Itoa(v.pid), 0o555, 0, 0)
	v.f.AddSymlink("/proc/"+strconv.Itoa(v.pid)+"/exe", exe)
}

func (v *apkVM) write(ver string) {
	var b strings.Builder
	b.WriteString("P:musl\nV:1.2.5-r21\n\n")
	for _, n := range opensshVM {
		b.WriteString("P:" + n + "\nV:" + ver + "\n\n")
	}
	v.f.AddFile(alpine.InstalledDB, []byte(b.String()), 0o644, 0, 0)
	v.version = ver
}

func (v *apkVM) listing() string {
	var lines []string
	for _, n := range opensshVM {
		lines = append(lines, "Upgrading "+n+" ("+v.version+" -> "+v.offer+")")
	}
	if v.extra != "" {
		lines = append(lines, v.extra)
	}
	var b strings.Builder
	for i, l := range lines {
		b.WriteString("(" + strconv.Itoa(i+1) + "/" + strconv.Itoa(len(lines)) + ") " + l + "\n")
	}
	return b.String() + "OK: 136.9 MiB in 207 packages\n"
}

func (v *apkVM) started() bool {
	_, ok := v.f.Entry(detect.OpenRCStarted + "/sshd")
	return ok
}

func (v *apkVM) run(argv []string, _ []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	names := strings.Join(opensshVM, " ")
	switch cmd {
	case reload.BinaryPAM + " -f /etc/ssh/sshd_config -G":
		return hostfake.Response{Stdout: "usepam yes\n"}
	case "apk update":
		return hostfake.Response{Stdout: "OK: 26371 distinct packages available\n"}
	case "apk upgrade --simulate " + names:
		return hostfake.Response{Stdout: v.listing()}
	case "apk upgrade " + names:
		out := v.listing()
		v.write(v.offer)
		v.listener(true)
		return hostfake.Response{Stdout: out}
	case "rc-service sshd reload":
		if v.reloadKills {
			_ = v.f.Remove(context.Background(), detect.OpenRCStarted+"/sshd")
			return hostfake.Response{Stderr: " * sshd: not running\n", ExitCode: 1}
		}
		v.pid++
		if v.restarting {
			v.pending = true
			_ = v.f.Remove(context.Background(), alpine.SSHDPidFile)
			return hostfake.Response{Stdout: " * Reloading sshd ... [ ok ]\n"}
		}
		v.listener(v.staleAfterReload)
		return hostfake.Response{Stdout: " * Reloading sshd ... [ ok ]\n"}
	case "rc-service sshd start":
		v.f.AddSymlink(detect.OpenRCStarted+"/sshd", "/etc/init.d/sshd")
		v.pid++
		v.listener(false)
		return hostfake.Response{Stdout: " * Starting sshd ... [ ok ]\n"}
	}
	if strings.HasPrefix(cmd, "sh ") || strings.HasPrefix(cmd, "setsid ") {
		return hostfake.Response{}
	}
	v.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

func alpineFacts() detect.Facts {
	return detect.Facts{
		OS:        detect.OSRelease{ID: "alpine", VersionID: "3.23.6"},
		Platform:  detect.Platform{Distro: detect.DistroAlpine, Version: "3.23", Support: detect.Supported},
		Init:      detect.Init{Kind: detect.OpenRC, Running: detect.Yes},
		SSH:       detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes, InRunlevel: detect.Yes}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}
}

func (v *apkVM) engine() *engine.Engine {
	return engine.New(v.f, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return now }, Hardn: hardnBinary},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return alpineFacts(), nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Session: revert.Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"},
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true},
	})
}

func (v *apkVM) commands(from int) []string {
	var out []string
	for _, c := range v.f.Calls()[from:] {
		if c.Op == "run" && !strings.HasSuffix(c.Argv[0], "sshd.pam") {
			out = append(out, strings.Join(c.Argv, " "))
		}
	}
	return out
}

// TestUpgradePlan: on the VM unfixed, the plan is the upgrade, the reload
// and the check, in that order, none lockout-risk, each claiming sshd, the
// upgrade with no undo and its commands apk update then apk upgrade of
// every openssh package; the server checked is the binary the init script
// runs; planning runs nothing but reads. Fixed, it plans nothing; off
// Alpine it is refused; the fix not established is SSH_UPGRADE_UNKNOWN.
func TestUpgradePlan(t *testing.T) {
	ctx := context.Background()
	for _, pam := range []bool{false, true} {
		v := newApkVM(t, "10.2_p1-r0", "10.2_p1-r1", pam)
		n := len(v.f.Calls())
		p, err := v.engine().Plan(ctx, []engine.Control{sshchange.Upgrade{}}, &config.Config{})
		if err != nil || p.Err() != nil {
			t.Fatalf("pam %v: plan: %v, %v", pam, err, p.Err())
		}
		var ids []string
		for _, c := range p.Changes {
			ids = append(ids, c.ID)
			if !slices.Equal(c.Resources, []string{steps.ResourceSSHD}) {
				t.Errorf("%s claims %v", c.ID, c.Resources)
			}
		}
		if want := []string{"ssh.auth/upgrade", "ssh.auth/reload", "ssh.auth/verify"}; !slices.Equal(ids, want) {
			t.Fatalf("pam %v: changes %v, want %v", pam, ids, want)
		}
		if p.Risk >= engine.RiskLockout {
			t.Errorf("pam %v: risk %v, want below lockout", pam, p.Risk)
		}
		up := p.Changes[0]
		want := []string{"apk update", "apk upgrade " + strings.Join(opensshVM, " ")}
		server := alpine.OpenSSHServer
		if pam {
			server = alpine.OpenSSHServerPAM
		}
		if !slices.Equal(up.Commands, want) || up.Title != sshchange.UpgradeTitle || len(up.Rollback.Steps)+len(up.Rollback.Reloads) != 0 ||
			up.StateBefore != server+" 10.2_p1-r0" || up.StateAfter != "10.2_p1-r1 or later" {
			t.Errorf("pam %v: upgrade %+v", pam, up)
		}
		if got := p.Changes[1].Commands; !slices.Equal(got, []string{"rc-service sshd reload"}) {
			t.Errorf("pam %v: reload runs %v", pam, got)
		}
		if got := p.Changes[1].Rollback.Reloads; len(got) != 1 || !got[0].Binary || !got[0].IfUp || len(got[0].Keywords) != 0 {
			t.Errorf("pam %v: the reload's journalled undo %+v; want one Binary reload, IfUp", pam, got)
		}
		for _, c := range v.f.Calls()[n:] {
			switch c.Op {
			case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group", "scan-lines":
			case "run":
				if c.Argv[0] != reload.BinaryPAM {
					t.Errorf("pam %v: planning ran %v", pam, c.Argv)
				}
			default:
				if !strings.HasPrefix(c.Path, "/run/hardn") {
					t.Errorf("pam %v: planning wrote: %v", pam, c)
				}
			}
		}
	}
	t.Run("fixed", func(t *testing.T) {
		v := newApkVM(t, "10.2_p1-r1", "10.2_p1-r1", false)
		p, err := v.engine().Plan(ctx, []engine.Control{sshchange.Upgrade{}}, &config.Config{})
		if err != nil || p.Err() != nil || !p.Empty() {
			t.Fatalf("plan: %v, %v, %d changes; want none", err, p.Err(), len(p.Changes))
		}
	})
	t.Run("not established", func(t *testing.T) {
		v := newApkVM(t, "10.2_p1-r0", "10.2_p1-r1", false)
		_ = v.f.Remove(ctx, alpine.InstalledDB)
		p, err := v.engine().Plan(ctx, []engine.Control{sshchange.Upgrade{}}, &config.Config{})
		var rf *steps.Refusal
		if err != nil || !errors.As(p.Err(), &rf) || rf.Code != "SSH_UPGRADE_UNKNOWN" || !errors.Is(rf, sshchange.ErrUpgradeUnknown) ||
			rf.Why != "hardn can't tell if openssh-server has the fix for CVE-2026-123158: /lib/apk/db/installed is missing" {
			t.Fatalf("plan: %v, %#v", err, p.Err())
		}
	})
	t.Run("off Alpine", func(t *testing.T) {
		v := newApkVM(t, "10.2_p1-r0", "10.2_p1-r1", false)
		_, err := sshchange.Upgrade{}.Plan(ctx, v.f, engine.Input{Facts: debian13()})
		if !errors.Is(err, alpine.ErrNotAlpine) {
			t.Fatalf("plan off Alpine: %v", err)
		}
	})
}

// TestUpgradeApply: applied, apk update, the simulation, apk upgrade and
// rc-service sshd reload run in that order; the package then has the fix
// and the listener runs the new binary; nothing is left journalled. With
// sshd still restarting when the check first reads it (no pid file), the
// check reads again after a wait.
func TestUpgradeApply(t *testing.T) {
	t.Run("restarting", func(t *testing.T) {
		v := newApkVM(t, "10.2_p1-r0", "10.2_p1-r1", false)
		v.restarting = true
		e := v.engine()
		p, err := e.Plan(context.Background(), []engine.Control{v.upgrade()}, &config.Config{})
		if err != nil || p.Err() != nil {
			t.Fatal(err, p.Err())
		}
		if _, err := e.Apply(context.Background(), p); err != nil || v.waits != 1 {
			t.Fatalf("apply: %v, %d waits; want nil after one", err, v.waits)
		}
	})
	ctx := context.Background()
	v := newApkVM(t, "10.2_p1-r0", "10.2_p1-r1", false)
	e := v.engine()
	p, err := e.Plan(ctx, []engine.Control{sshchange.Upgrade{}}, &config.Config{})
	if err != nil || p.Err() != nil {
		t.Fatal(err, p.Err())
	}
	n := len(v.f.Calls())
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatalf("apply: %v", err)
	}
	names := strings.Join(opensshVM, " ")
	if got, want := v.commands(n), []string{"apk update", "apk upgrade --simulate " + names, "apk upgrade " + names, "rc-service sshd reload"}; !slices.Equal(got, want) {
		t.Errorf("ran %q\nwant %q", got, want)
	}
	if fix := alpine.OpenSSHFix(ctx, v.f, alpine.OpenSSHServer, "3.23.6"); fix.Fixed != detect.Yes {
		t.Errorf("after: %+v", fix)
	}
	if _, stale, err := alpine.ListenerBinary(ctx, v.f); err != nil || stale {
		t.Errorf("listener stale %v, %v", stale, err)
	}
	if st, err := revert.ReadState(ctx, v.f, revert.Options{}); err != nil || st.Busy() {
		t.Errorf("journal after: busy %v, %v", st.Busy(), err)
	}
}

// TestUpgradeFails: a simulation listing another package's upgrade
// upgrades nothing, in its words; a reload that leaves the listener on the
// old binary fails the check, whose rollback reloads sshd again; a reload
// that stops sshd is started again by the rollback. The package stays
// upgraded either way: it has no undo.
func TestUpgradeFails(t *testing.T) {
	ctx := context.Background()
	names := strings.Join(opensshVM, " ")
	for _, tc := range []struct {
		name  string
		set   func(v *apkVM)
		text  string // the failure's words
		after []string
		fixed bool
	}{
		{name: "another package", set: func(v *apkVM) { v.extra = "Upgrading libcrypto3 (3.5.4-r0 -> 3.5.5-r0)" },
			text: "Upgrading openssh would also change libcrypto3", after: []string{"apk update", "apk upgrade --simulate " + names}},
		{name: "the listener still stale", set: func(v *apkVM) { v.staleAfterReload = true },
			text:  "sshd still runs the binary the upgrade replaced",
			after: []string{"apk update", "apk upgrade --simulate " + names, "apk upgrade " + names, "rc-service sshd reload", "rc-service sshd reload"}, fixed: true},
		{name: "the reload stops sshd", set: func(v *apkVM) { v.reloadKills = true },
			after: []string{"apk update", "apk upgrade --simulate " + names, "apk upgrade " + names, "rc-service sshd reload", "rc-service sshd start"}, fixed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newApkVM(t, "10.2_p1-r0", "10.2_p1-r1", false)
			tc.set(v)
			e := v.engine()
			p, err := e.Plan(ctx, []engine.Control{v.upgrade()}, &config.Config{})
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			n := len(v.f.Calls())
			_, err = e.Apply(ctx, p)
			var fl *steps.Failure
			if err == nil || tc.text != "" && (!errors.As(err, &fl) || fl.Text != tc.text) {
				t.Fatalf("apply: %v; want %q", err, tc.text)
			}
			if tc.name == "the reload stops sshd" {
				v.reloadKills = false
			}
			if got := v.commands(n); !slices.Equal(got, tc.after) {
				t.Errorf("ran %q\nwant %q", got, tc.after)
			}
			if fix := alpine.OpenSSHFix(ctx, v.f, alpine.OpenSSHServer, "3.23.6"); (fix.Fixed == detect.Yes) != tc.fixed {
				t.Errorf("fixed after %v, want %v", fix.Fixed, tc.fixed)
			}
			if !v.started() {
				t.Errorf("sshd left stopped")
			}
		})
	}
}

// TestUpgradeVerify: the check alone, on a package still without the fix,
// fails in its words, whatever apk said.
func TestUpgradeVerify(t *testing.T) {
	ctx := context.Background()
	v := newApkVM(t, "10.2_p1-r0", "10.2_p1-r1", false)
	p, err := v.engine().Plan(ctx, []engine.Control{v.upgrade()}, &config.Config{})
	if err != nil || p.Err() != nil || len(p.Changes) != 3 {
		t.Fatal(err, p.Err())
	}
	_, err = p.Changes[2].Apply(ctx, v.f, engine.Run{})
	var fl *steps.Failure
	if !errors.As(err, &fl) || fl.Text != "openssh-server still lacks the fix after the upgrade" || !errors.Is(err, alpine.ErrNotEffective) {
		t.Fatalf("verify: %v", err)
	}
}

// TestPasswordsOnUnfixed: on Alpine, Password login on is refused before
// anything is written where sshd lacks CVE-2026-123158's fix and checks
// passwords without PAM (SSH_PASSWORDS_UNFIXED, item 5 its remedy), as
// Create is refused from the other side; it is planned where the package
// has the fix, where its version is not established, where sshd checks
// passwords through PAM (openssh-server-pam's sshd.pam, run with UsePAM
// yes, its package unfixed), and off Alpine whatever apk's database holds.
func TestPasswordsOnUnfixed(t *testing.T) {
	alpineVM := func(t *testing.T, facts detect.Facts, version string, pam bool) *vm {
		v := newVM(t, facts, false, george)
		v.noPAM = !pam // Alpine's openssh-server: sshd built without PAM
		v.f.AddDir("/lib/apk/db", 0o755, 0, 0)
		pkg := alpine.OpenSSHServer
		if pam {
			pkg = alpine.OpenSSHServerPAM
			v.f.AddFile(reload.BinaryPAM, []byte("ELF"), 0o755, 0, 0)
			run := v.f.Handler
			v.f.Handler = func(argv []string, in []byte) hostfake.Response {
				if argv[0] != reload.BinaryPAM {
					return run(argv, in)
				}
				if slices.Contains(argv, "-G") { // reload.OpenRCFlavour: debianMain's UsePAM yes
					return hostfake.Response{Stdout: "usepam yes\n"}
				}
				return v.sshd(argv)
			}
		}
		if version != "" {
			v.f.AddFile(alpine.InstalledDB, []byte("P:"+pkg+"\nV:"+version+"\nA:x86_64\n\n"), 0o644, 0, 0)
		}
		return v
	}
	for _, c := range []struct {
		name    string
		facts   detect.Facts
		version string
		pam     bool
		refused bool
	}{
		{"unfixed", alpineFacts(), "10.2_p1-r0", false, true},
		{"fixed", alpineFacts(), "10.2_p1-r1", false, false},
		{"not established", alpineFacts(), "", false, false},
		{"PAM, its package unfixed", alpineFacts(), "10.2_p1-r0", true, false},
		{"off Alpine", debian13(), "10.2_p1-r0", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, p := planOn(t, alpineVM(t, c.facts, c.version, c.pam), passOn) // planOn fails a write
			if !c.refused {
				if err := p.Err(); err != nil || p.Empty() {
					t.Fatalf("plan: %v, %d changes; want the change", err, len(p.Changes))
				}
				return
			}
			rf := refusalOf(t, p)
			if rf.Code != "SSH_PASSWORDS_UNFIXED" || !errors.Is(rf, sshchange.ErrPasswordsUnfixed) ||
				rf.Why != "openssh-server 10.2_p1-r0 lacks the fix for CVE-2026-123158: with passwords on, anyone could log in as an account with no password" ||
				rf.Fix != "Upgrade openssh first: item 5" {
				t.Fatalf("refusal %+v", rf)
			}
		})
	}
}
