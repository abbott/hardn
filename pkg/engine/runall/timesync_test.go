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
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	tshost "github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/host/timesync/timefake"
)

// tsRead reads Run All on a bare Ubuntu 26.04 host whose time service is
// chrony, with yml as hardn.yml's ntpServers: the other rows' checks read a
// host that answers nothing, which is theirs to say; Time Sync's read its
// files.
func tsRead(t *testing.T, yml config.NTPServers, files map[string]string) (runall.Reading, *hostfake.System) {
	t.Helper()
	f := hostfake.New(host.Options{})
	for _, d := range []string{tshost.SourcesDir, "/var/lib", "/var/backups", "/run"} {
		f.AddDir(d, 0o755, 0, 0)
	}
	f.AddFile(tshost.ChronyConfDebian, []byte(timefake.UbuntuConf), 0o644, 0, 0)
	for name, data := range files {
		f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	sim := &timefake.Sim{Read: func(name string) ([]byte, error) { return f.ReadFile(context.Background(), name) }, Service: "chrony", Synced: "192.0.2.10", Packages: map[string]bool{"chrony": true}}
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := sim.Answer(argv); ok && (argv[0] != "dpkg-query" || argv[len(argv)-1] == "chrony") {
			return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}
		}
		return hostfake.Response{ExitCode: 127}
	}
	facts := detect.Facts{Platform: detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported},
		Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}
	cfg := config.DefaultConfig()
	cfg.NTPServers = yml
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

// TestTimeSyncRow: Run All lists Time Sync only where hardn.yml names
// servers (gallery-timesync.md, decision 15 (a)), after Log Forwarding and
// before Backup; plans Set servers, never Remove servers; says "already as
// asked" where the service uses them alone; and a refusal is the row's, in
// its words.
func TestTimeSyncRow(t *testing.T) {
	servers := config.NTPServers{"192.0.2.10", "192.0.2.11"}
	ids := runall.IDsFor(&config.Config{NTPServers: servers, LogForwarding: config.LogForwarding{Server: "192.0.2.50"}})
	if n := len(ids); n != len(runall.IDs())+2 || ids[n-3] != logforward.ID || ids[n-2] != timesync.ID || ids[n-1] != backupctl.ID {
		t.Fatalf("IDsFor with servers = %q", ids)
	}

	r, f := tsRead(t, nil, nil)
	if _, ok := r.Row(timesync.ID); ok {
		t.Error("no servers in hardn.yml: Run All lists Time Sync")
	}
	if slices.ContainsFunc(f.Runs(), func(argv []string) bool { return argv[0] == "chronyc" }) {
		t.Errorf("no servers in hardn.yml: Run All read the time service: %q", f.Runs())
	}

	r, f = tsRead(t, servers, map[string]string{timefake.UbuntuPoolsFile: timefake.UbuntuPools})
	rw, ok := r.Row(timesync.ID)
	ts, isSet := rw.Control.(timesync.Sync)
	if !ok || rw.Kind != runall.Planned || rw.Words != "Set servers" || rw.Title != timesync.Title || !isSet || ts.Action != timesync.ActionSet {
		t.Fatalf("the row = %+v", rw)
	}
	if r.Plan == nil || !slices.ContainsFunc(r.Plan.Changes, func(c engine.Change) bool { return c.ID == timesync.ChangeFile && c.Target == tshost.ChronySources }) {
		t.Errorf("the one plan lacks hardn's time servers: %+v", r.Plan)
	}
	if _, err := f.Stat(context.Background(), tshost.ChronySources); err == nil {
		t.Error("reading Run All wrote hardn's file")
	}

	own, err := tshost.SourcesFile(servers)
	if err != nil {
		t.Fatal(err)
	}
	r, _ = tsRead(t, servers, map[string]string{tshost.ChronySources: string(own)})
	if rw, _ := r.Row(timesync.ID); rw.Kind != runall.AsAsked {
		t.Errorf("set already: %v %q, want as asked", rw.Kind, rw.Words)
	}

	r, _ = tsRead(t, servers, map[string]string{tshost.SourcesDir + "/site.sources": "server 192.0.2.77\n"})
	if rw, _ := r.Row(timesync.ID); rw.Kind != runall.Refused || !strings.Contains(rw.Refusal, "/etc/chrony/sources.d/site.sources already sets time servers") {
		t.Errorf("set outside hardn: %v %q, want refused in the control's words", rw.Kind, rw.Refusal)
	}
}
