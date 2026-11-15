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
	"github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/host/revert"
)

// lfRead reads Run All on a bare Ubuntu host whose logger is rsyslog, with
// yml as hardn.yml's syslogServers: the other rows' checks read a host that
// answers nothing, which is theirs to say; Syslog Settings' read its files.
func lfRead(t *testing.T, yml config.SyslogServers, files map[string]string) (runall.Reading, *hostfake.System) {
	t.Helper()
	f := hostfake.New(host.Options{})
	for _, d := range []string{"/etc/rsyslog.d", "/var/lib", "/var/backups", "/run"} {
		f.AddDir(d, 0o755, 0, 0)
	}
	for name, data := range files {
		f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		switch cmd := strings.Join(argv, " "); {
		case argv[0] == "dpkg-query" && argv[len(argv)-1] == "rsyslog":
			return hostfake.Response{Stdout: "install ok installed\t8.2312.0\n"}
		case cmd == "rsyslogd -N1":
			return hostfake.Response{}
		}
		return hostfake.Response{ExitCode: 127}
	}
	facts := detect.Facts{Platform: detect.Platform{Distro: detect.DistroUbuntu, Version: "24.04", Support: detect.Supported},
		Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}
	cfg := config.DefaultConfig()
	cfg.SyslogServers = yml
	now := func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }
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

// TestLogForwardingRow: Run All lists Syslog Settings only where hardn.yml
// names a server (gallery-logforward.md, decision 9 (a)), before Backup;
// plans Set servers, never Stop forwarding; says "already as asked" where the
// host forwards there; and a refusal is the row's, in its words.
func TestLogForwardingRow(t *testing.T) {
	server := config.SyslogServers{{Server: "192.0.2.50"}}
	ids := runall.IDsFor(&config.Config{SyslogServers: server})
	if n := len(ids); n != len(runall.IDs())+1 || ids[n-2] != logforward.ID || ids[n-1] != backupctl.ID {
		t.Fatalf("IDsFor with a server = %q", ids)
	}
	if got := runall.IDsFor(config.DefaultConfig()); !slices.Equal(got, runall.IDs()) {
		t.Fatalf("IDsFor with no server = %q", got)
	}

	r, f := lfRead(t, config.SyslogServers{}, nil)
	if _, ok := r.Row(logforward.ID); ok {
		t.Error("no server in hardn.yml: Run All lists Syslog Settings")
	}
	if slices.ContainsFunc(f.Runs(), func(argv []string) bool { return argv[len(argv)-1] == "rsyslog" || argv[0] == "rsyslogd" }) {
		t.Errorf("no server in hardn.yml: Run All read the logger: %q", f.Runs())
	}

	r, f = lfRead(t, server, nil)
	rw, ok := r.Row(logforward.ID)
	fw, isSet := rw.Control.(logforward.Forward)
	if !ok || rw.Kind != runall.Planned || rw.Words != "Set servers" || rw.Title != logforward.Title || !isSet || fw.Action != logforward.ActionSet {
		t.Fatalf("the row = %+v", rw)
	}
	if r.Plan == nil || !slices.ContainsFunc(r.Plan.Changes, func(c engine.Change) bool { return c.ID == logforward.ChangeFile && c.Target == logfwd.DropIn }) {
		t.Errorf("the one plan lacks hardn's forwarding file: %+v", r.Plan)
	}
	if _, err := f.Stat(context.Background(), logfwd.DropIn); err == nil {
		t.Error("reading Run All wrote hardn's file")
	}

	own, err := logfwd.RsyslogFile([]logfwd.Server{{Address: "192.0.2.50", Port: "514", Proto: logfwd.TCP}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ = lfRead(t, server, map[string]string{logfwd.DropIn: string(own)})
	if rw, _ := r.Row(logforward.ID); rw.Kind != runall.AsAsked {
		t.Errorf("forwarding there already: %v %q, want as asked", rw.Kind, rw.Words)
	}

	r, _ = lfRead(t, server, map[string]string{"/etc/rsyslog.d/50-remote.conf": "*.* @@192.0.2.60\n"})
	if rw, _ := r.Row(logforward.ID); rw.Kind != runall.Refused || !strings.Contains(rw.Refusal, "/etc/rsyslog.d/50-remote.conf already forwards logs") {
		t.Errorf("set outside hardn: %v %q, want refused in the control's words", rw.Kind, rw.Refusal)
	}
}
