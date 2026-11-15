package runall_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	backupctl "github.com/abbott/hardn/pkg/engine/controls/backup"
	"github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	khost "github.com/abbott/hardn/pkg/host/sysctl"
	"github.com/abbott/hardn/pkg/host/sysctl/sysctlfake"
)

// ksRead reads Run All on a bare Rocky Linux host with over's kernel
// values and files, kernel as on: the other rows' checks read a
// host that answers nothing, which is theirs to say.
func ksRead(t *testing.T, on bool, container bool, over map[string]int, files map[string]string) (runall.Reading, *hostfake.System) {
	t.Helper()
	f := hostfake.New(host.Options{})
	for _, d := range []string{"/etc/sysctl.d", "/var/lib", "/var/backups", "/run"} {
		f.AddDir(d, 0o755, 0, 0)
	}
	for name, data := range sysctlfake.Files(sysctlfake.Values([]string{"eth0"}, over)) {
		f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	f.AddFile(sysctlfake.ProcpsPath, nil, 0o755, 0, 0)
	for name, data := range files {
		f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	f.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{ExitCode: 127} }
	facts := detect.Facts{Platform: detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported},
		Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}
	if container {
		facts.Container.Detected = detect.Yes
	}
	cfg := config.DefaultConfig()
	cfg.KernelSettings = on
	now := func() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) }
	r, err := runall.Read(context.Background(), runall.Input{System: f, Facts: facts, Config: cfg, Revert: revert.Options{Now: now},
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			return engine.New(f, engine.Options{Revert: revert.Options{Now: now, Hardn: "/usr/local/bin/hardn"}, Progress: progress,
				Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil },
				Hold:   func() func() { return func() {} }, PID: 5000, Backups: hostbackup.Settings{Path: hostbackup.DefaultPath, Enabled: true}})
		}})
	if err != nil {
		t.Fatal(err)
	}
	return r, f
}

// TestKernelSettingsRow: Run All lists Kernel Settings only where
// hardn.yml's kernel is true (gallery-sysctl.md, decisions 17 and
// 18 (a)), after NTP Settings and before Backup; plans Apply settings, never
// Remove settings; says "already as asked" with nothing below the baseline;
// a refusal is the row's, in its words; and a container has no row.
func TestKernelSettingsRow(t *testing.T) {
	ids := runall.IDsFor(&config.Config{KernelSettings: true, NTPServers: config.NTPServers{"192.0.2.10"}})
	if n := len(ids); n != len(runall.IDs())+2 || ids[n-3] != timesync.ID || ids[n-2] != kernelsettings.ID || ids[n-1] != backupctl.ID {
		t.Fatalf("IDsFor with kernel = %q", ids)
	}
	if slices.Contains(runall.IDsFor(config.DefaultConfig()), kernelsettings.ID) {
		t.Error("kernel is off by default, and Run All lists it")
	}
	low := map[string]int{"kernel.yama.ptrace_scope": 0}

	r, f := ksRead(t, false, false, low, nil)
	if _, ok := r.Row(kernelsettings.ID); ok {
		t.Error("kernel false: Run All lists Kernel Settings")
	}
	if slices.ContainsFunc(f.Calls(), func(c hostfake.Call) bool { return strings.HasPrefix(c.Path, khost.Proc+"/kernel/yama") }) {
		t.Error("kernel false: Run All read the kernel's settings")
	}

	r, f = ksRead(t, true, false, low, nil)
	rw, ok := r.Row(kernelsettings.ID)
	ks, isSet := rw.Control.(kernelsettings.Settings)
	if !ok || rw.Kind != runall.Planned || rw.Words != "Apply settings" || rw.Title != kernelsettings.Title || !isSet || ks.Action != kernelsettings.ActionApply {
		t.Fatalf("the row = %+v", rw)
	}
	if r.Plan == nil || !slices.ContainsFunc(r.Plan.Changes, func(c engine.Change) bool { return c.ID == kernelsettings.ChangeFile && c.Target == khost.File }) ||
		!slices.ContainsFunc(r.Plan.Changes, func(c engine.Change) bool { return c.ID == kernelsettings.ChangeApply }) {
		t.Errorf("the one plan lacks hardn's kernel settings: %+v", r.Plan)
	}
	if _, err := f.Stat(context.Background(), khost.File); err == nil {
		t.Error("reading Run All wrote hardn's file")
	}

	r, _ = ksRead(t, true, false, map[string]int{"net.ipv4.conf.*.send_redirects": 0}, nil)
	if rw, _ := r.Row(kernelsettings.ID); rw.Kind != runall.AsAsked {
		t.Errorf("at the baseline: %v %q, want as asked", rw.Kind, rw.Words)
	}

	r, _ = ksRead(t, true, false, low, map[string]string{khost.File: "kernel.sysrq = 0\n"})
	if rw, _ := r.Row(kernelsettings.ID); rw.Kind != runall.Refused || !strings.Contains(rw.Refusal, khost.File+" is not hardn's file") {
		t.Errorf("a file that is not hardn's: %v %q, want refused in the control's words", rw.Kind, rw.Refusal)
	}

	r, _ = ksRead(t, true, true, low, nil)
	if rw, ok := r.Row(kernelsettings.ID); ok {
		t.Errorf("a container: Run All lists Kernel Settings: %+v", rw)
	}
	if r.Plan != nil && slices.ContainsFunc(r.Plan.Changes, func(c engine.Change) bool { return strings.HasPrefix(c.ID, kernelsettings.ID) }) {
		t.Error("a container: Run All plans kernel settings")
	}
}
