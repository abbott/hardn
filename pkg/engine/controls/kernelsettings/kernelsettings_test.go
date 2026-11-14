package kernelsettings_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	ks "github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	khost "github.com/abbott/hardn/pkg/host/sysctl"
	"github.com/abbott/hardn/pkg/host/sysctl/sysctlfake"
	"github.com/abbott/hardn/pkg/output"
)

var clock = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// model is a fake host and its sysctl tool.
type model struct {
	t     *testing.T
	f     *hostfake.System
	sim   *sysctlfake.Sim
	facts detect.Facts
}

// rocky is Rocky Linux 10's values of 2026-11-14: redirects accepted and
// sent on every interface, and tracing unrestricted. procps, systemd.
var rocky = map[string]int{"net.ipv4.conf.*.accept_redirects": 1, "net.ipv6.conf.*.accept_redirects": 1, khost.PtraceKey: 0}

// newModel is a host with eth0 and over's values, procps's sysctl under
// systemd.
func newModel(t *testing.T, over map[string]int) *model {
	t.Helper()
	m := &model{t: t, f: hostfake.New(host.Options{}),
		facts: detect.Facts{Platform: detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}}
	for _, dir := range []string{"/etc/sysctl.d", "/var/lib", "/var/backups", "/var/log", "/run"} {
		m.f.AddDir(dir, 0o755, 0, 0)
	}
	for name, data := range sysctlfake.Files(sysctlfake.Values([]string{"eth0"}, over)) {
		m.f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	m.f.AddFile(sysctlfake.ProcpsPath, nil, 0o755, 0, 0)
	m.sim = &sysctlfake.Sim{Sys: m.f}
	m.f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := m.sim.Answer(argv); ok {
			return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}
		}
		return hostfake.Response{ExitCode: 127}
	}
	return m
}

// alpine is m with busybox's sysctl under OpenRC, the sysctl service in
// the boot runlevel where boot.
func (m *model) alpine(boot bool) *model {
	ctx := context.Background()
	if err := m.f.Remove(ctx, sysctlfake.ProcpsPath); err != nil {
		m.t.Fatal(err)
	}
	m.f.AddFile(sysctlfake.BusyboxPath, nil, 0o755, 0, 0)
	m.f.AddSymlink(sysctlfake.BusyboxLink, sysctlfake.BusyboxPath)
	if boot {
		m.f.AddFile("/etc/init.d/"+khost.Service, nil, 0o755, 0, 0)
		m.f.AddSymlink(detect.OpenRCRunlevels+"/boot/"+khost.Service, "/etc/init.d/"+khost.Service)
	}
	m.sim.Busybox = true
	m.facts.Platform.Distro, m.facts.Init.Kind = detect.DistroAlpine, detect.OpenRC
	return m
}

func (m *model) file(name, data string) *model {
	m.f.AddFile(name, []byte(data), 0o644, 0, 0)
	return m
}

// ran are the sysctl commands run, in order.
func (m *model) ran() []string {
	var out []string
	for _, argv := range m.f.Runs() {
		out = append(out, strings.Join(argv, " "))
	}
	return out
}

func (m *model) engine() *engine.Engine {
	return engine.New(m.f, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return clock }, Hardn: "/usr/local/bin/hardn"},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return m.facts, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Backups: hostbackup.Settings{Path: hostbackup.DefaultPath, Enabled: true},
	})
}

func (m *model) plan(a ks.Action) ([]engine.Change, error) {
	return ks.Settings{Action: a}.Plan(context.Background(), m.f, engine.Input{Facts: m.facts})
}

// apply plans and applies a on the engine, between run after the plan; it
// returns the plan's change ids and the apply's error.
func (m *model) apply(a ks.Action, between ...func()) ([]string, error) {
	m.t.Helper()
	ctx := context.Background()
	e := m.engine()
	p, err := e.Plan(ctx, []engine.Control{ks.Settings{Action: a}}, nil)
	if err != nil {
		m.t.Fatal(err)
	}
	if len(p.Refused) > 0 {
		m.t.Fatalf("refused: %v", p.Refused)
	}
	var ids []string
	for _, c := range p.Changes {
		ids = append(ids, strings.TrimPrefix(c.ID, ks.ID+"/"))
	}
	for _, f := range between {
		f()
	}
	_, err = e.Apply(ctx, p)
	return ids, err
}

func (m *model) content(name string) string {
	m.t.Helper()
	data, err := m.f.ReadFile(context.Background(), name)
	if err != nil {
		m.t.Fatal(err)
	}
	return string(data)
}

func (m *model) gone(name string) bool {
	_, err := m.f.Lstat(context.Background(), name)
	return err != nil
}

// value is key's value now.
func (m *model) value(key string) int {
	m.t.Helper()
	v := khost.ReadValue(context.Background(), m.f, key)
	if v.Err != nil {
		m.t.Fatal(v.Err)
	}
	return v.N
}

func (m *model) status() engine.Status {
	m.t.Helper()
	st, err := ks.Settings{}.Check(context.Background(), m.f, m.facts)
	if err != nil {
		m.t.Fatal(err)
	}
	return st
}

// snapshot is every key's value.
func (m *model) snapshot() map[string]int {
	out := map[string]int{}
	for k := range sysctlfake.Values([]string{"eth0"}, nil) {
		out[k] = m.value(k)
	}
	return out
}

func same(a, b map[string]int) string {
	for k, v := range a {
		if b[k] != v {
			return k
		}
	}
	return ""
}

const rockyFile = khost.Header +
	"# before: net.ipv4.conf.all.accept_redirects = 1\n# before: net.ipv4.conf.default.accept_redirects = 1\n# before: net.ipv4.conf.eth0.accept_redirects = 1\n# before: net.ipv4.conf.lo.accept_redirects = 1\n" +
	"net.ipv4.conf.*.accept_redirects = 0\n" +
	"# before: net.ipv6.conf.all.accept_redirects = 1\n# before: net.ipv6.conf.default.accept_redirects = 1\n# before: net.ipv6.conf.eth0.accept_redirects = 1\n# before: net.ipv6.conf.lo.accept_redirects = 1\n" +
	"net.ipv6.conf.*.accept_redirects = 0\n" +
	"# before: net.ipv4.conf.all.send_redirects = 1\n# before: net.ipv4.conf.default.send_redirects = 1\n# before: net.ipv4.conf.eth0.send_redirects = 1\n# before: net.ipv4.conf.lo.send_redirects = 1\n" +
	"net.ipv4.conf.*.send_redirects = 0\n" +
	"# before: kernel.yama.ptrace_scope = 0\nkernel.yama.ptrace_scope = 1\n"

// TestApplyAndRemove: Apply settings writes hardn's file, sets its values
// with a "sysctl -w" for each key below the baseline and reads them back,
// every interface's among them;
// the row is then Configured by hardn. Remove settings deletes the file and
// sets every recorded value back.
func TestApplyAndRemove(t *testing.T) {
	m := newModel(t, rocky)
	before := m.snapshot()
	if st := m.status(); st.State != engine.NotConfigured || st.Detail != "4 to set" {
		t.Fatalf("status = %+v", st)
	}
	ids, err := m.apply(ks.ActionApply)
	if err != nil || !slices.Equal(ids, []string{"file", "apply"}) {
		t.Fatalf("apply: %v, %v", ids, err)
	}
	if got := m.content(khost.File); got != rockyFile {
		t.Errorf("file:\n%s", got)
	}
	if ran := m.ran(); len(ran) != 13 || ran[0] != "sysctl -w net.ipv4.conf.all.accept_redirects=0" || ran[12] != "sysctl -w kernel.yama.ptrace_scope=1" {
		t.Errorf("ran %q", ran)
	}
	for _, key := range []string{"net.ipv4.conf.eth0.accept_redirects", "net.ipv6.conf.eth0.accept_redirects", "net.ipv6.conf.lo.accept_redirects", "net.ipv4.conf.all.send_redirects"} {
		if m.value(key) != 0 {
			t.Errorf("%s not set", key)
		}
	}
	if m.value(khost.PtraceKey) != 1 || m.value("net.ipv4.ip_forward") != 0 || m.value("net.ipv6.conf.all.accept_ra") != 1 {
		t.Error("tracing not set, or a key hardn never writes changed")
	}
	if st := m.status(); st.State != engine.Configured || st.Detail != ks.DetailByHardn {
		t.Errorf("status after = %+v", st)
	}
	if cs, err := m.plan(ks.ActionApply); err != nil || len(cs) != 0 {
		t.Errorf("a second Apply plans %v, %v", steps.IDs(cs), err)
	}

	ids, err = m.apply(ks.ActionRemove)
	if err != nil || !slices.Equal(ids, []string{"file", "restore"}) {
		t.Fatalf("remove: %v, %v", ids, err)
	}
	if !m.gone(khost.File) {
		t.Error("hardn's file left")
	}
	if k := same(before, m.snapshot()); k != "" {
		t.Errorf("%s not set back", k)
	}
	if ran := m.ran(); len(ran) != 13+13 || ran[13] != "sysctl -w net.ipv4.conf.all.accept_redirects=1" {
		t.Errorf("ran %q", ran)
	}
}

// TestRollback: all or nothing (decision 13). A key the kernel refuses
// fails the change, by the exit status on procps; hardn's file is gone
// again and every value as it was.
func TestRollback(t *testing.T) {
	m := newModel(t, rocky)
	before := m.snapshot()
	m.sim.Refuse = map[string]bool{khost.PtraceKey: true}
	_, err := m.apply(ks.ActionApply)
	if !errors.Is(err, ks.ErrNotSet) || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Fatalf("apply: %v", err)
	}
	if !m.gone(khost.File) {
		t.Error("hardn's file left")
	}
	if k := same(before, m.snapshot()); k != "" {
		t.Errorf("%s not set back", k)
	}
	// A file hardn wrote before is put back as it was.
	m = newModel(t, map[string]int{"kernel.kptr_restrict": 1, "fs.protected_fifos": 0, "net.ipv4.conf.*.send_redirects": 0})
	old := khost.Header + "# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n"
	m.file(khost.File, old)
	m.sim.Refuse = map[string]bool{"fs.protected_fifos": true}
	if _, err := m.apply(ks.ActionApply); !errors.Is(err, ks.ErrNotSet) {
		t.Fatalf("apply: %v", err)
	}
	if m.content(khost.File) != old || m.value("fs.protected_fifos") != 0 {
		t.Errorf("file or value not put back:\n%s", m.content(khost.File))
	}
}

// TestAlpine: busybox reads no "*", so the file names each interface (F4);
// its exit status is 0 whatever happened, so a failure is told by its
// stderr, and by reading each value back where it says nothing.
func TestAlpine(t *testing.T) {
	over := map[string]int{"net.ipv6.conf.eth0.accept_redirects": 1, "net.ipv4.conf.*.send_redirects": 0, "kernel.kptr_restrict": 0}
	m := newModel(t, over).alpine(true)
	if _, err := m.apply(ks.ActionApply); err != nil {
		t.Fatal(err)
	}
	want := khost.Header + "# before: net.ipv6.conf.eth0.accept_redirects = 1\nnet.ipv6.conf.eth0.accept_redirects = 0\n# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n"
	if got := m.content(khost.File); got != want {
		t.Errorf("file:\n%s", got)
	}
	if st := m.status(); st.State != engine.Configured {
		t.Errorf("status = %+v", st)
	}

	for name, quiet := range map[string]bool{"stderr": false, "read back": true} {
		m := newModel(t, over).alpine(true)
		before := m.snapshot()
		m.sim.Refuse, m.sim.Quiet = map[string]bool{"kernel.kptr_restrict": true}, quiet
		_, err := m.apply(ks.ActionApply)
		if !errors.Is(err, ks.ErrNotSet) {
			t.Fatalf("%s: apply: %v", name, err)
		}
		if quiet != strings.Contains(err.Error(), "kernel.kptr_restrict is 0, not 1") {
			t.Errorf("%s: %v", name, err)
		}
		if k := same(before, m.snapshot()); k != "" || !m.gone(khost.File) {
			t.Errorf("%s: %s not set back, or the file left", name, k)
		}
	}
}

// TestAlpineNotAtBoot: with the sysctl service in no runlevel (F5) Apply
// still sets the values now; the row is then Partial, and the notice says
// what to run. hardn does not enable the service.
func TestAlpineNotAtBoot(t *testing.T) {
	m := newModel(t, map[string]int{"kernel.kptr_restrict": 0, "net.ipv4.conf.*.send_redirects": 0}).alpine(false)
	r := ks.Read(context.Background(), m.f, m.facts)
	if n := ks.Notice(r, ks.ActionApply); !strings.Contains(n, ks.BootFix) {
		t.Errorf("notice = %q", n)
	}
	if _, err := m.apply(ks.ActionApply); err != nil {
		t.Fatal(err)
	}
	if m.value("kernel.kptr_restrict") != 1 {
		t.Error("not set now")
	}
	if st := m.status(); st.State != engine.Partial || st.Detail != ks.DetailNotAtBoot {
		t.Errorf("status = %+v", st)
	}
	for _, line := range m.ran() {
		if strings.Contains(line, "rc-update") {
			t.Errorf("hardn ran %q", line)
		}
	}
	if !m.gone(detect.OpenRCRunlevels + "/boot/" + khost.Service) {
		t.Error("hardn enabled the service")
	}
}

// TestRefusals: each refusal, with its code, before anything is written.
func TestRefusals(t *testing.T) {
	container := func(m *model) { m.facts.Container.Detected = detect.Yes }
	tests := []struct {
		name   string
		m      *model
		action ks.Action
		want   error
		code   string
		words  string
	}{
		{"not hardn's file", newModel(t, rocky).file(khost.File, "kernel.kptr_restrict = 0\n"), ks.ActionApply, ks.ErrOutside, "KERNEL_SETTINGS_FILE_NOT_HARDNS", khost.File + " is not hardn's file"},
		{"not hardn's file, remove", newModel(t, rocky).file(khost.File, "kernel.kptr_restrict = 0\n"), ks.ActionRemove, ks.ErrOutside, "KERNEL_SETTINGS_FILE_NOT_HARDNS", ""},
		{"no sysctl", func() *model {
			m := newModel(t, rocky)
			_ = m.f.Remove(context.Background(), sysctlfake.ProcpsPath)
			return m
		}(), ks.ActionApply, ks.ErrNoTool, "KERNEL_SETTINGS_NO_SYSCTL", "no sysctl"},
		{"container", func() *model { m := newModel(t, rocky); container(m); return m }(), ks.ActionApply, ks.ErrContainer, "KERNEL_SETTINGS_CONTAINER", "container"},
		{"unreadable key", newModel(t, rocky).file(khost.ProcPath("kernel.dmesg_restrict"), "?\n"), ks.ActionApply, ks.ErrNotRead, "KERNEL_SETTINGS_NOT_READ", "can't read Kernel log"},
		{"hardn's file, edited", newModel(t, rocky).file(khost.File, khost.Header+"net.ipv4.ip_forward = 1\n"), ks.ActionApply, ks.ErrNotRead, "KERNEL_SETTINGS_NOT_READ", khost.File + ":2"},
		{"nothing to remove", newModel(t, rocky), ks.ActionRemove, ks.ErrNothingToRemove, "KERNEL_SETTINGS_NOTHING_TO_REMOVE", "hardn set no kernel settings here"},
	}
	for _, tt := range tests {
		cs, err := tt.m.plan(tt.action)
		var ref *steps.Refusal
		if !errors.Is(err, tt.want) || !errors.As(err, &ref) || ref.Code != tt.code || !strings.Contains(ref.Why, tt.words) || len(cs) != 0 {
			t.Errorf("%s: %v, %v", tt.name, steps.IDs(cs), err)
			continue
		}
		if code := output.CodeOf(err); code != tt.code {
			t.Errorf("%s: the registry's code is %s", tt.name, code)
		}
		if ran := tt.m.ran(); len(ran) != 0 {
			t.Errorf("%s: ran %q", tt.name, ran)
		}
	}
	if _, err := (ks.Settings{}).Plan(context.Background(), newModel(t, nil).f, engine.Input{}); !errors.Is(err, engine.ErrNotMigrated) {
		t.Errorf("the status row plans: %v", err)
	}
}

// TestStatus: the row in each state.
func TestStatus(t *testing.T) {
	held := newModel(t, map[string]int{"net.ipv4.conf.all.accept_redirects": 1, "net.ipv4.conf.*.send_redirects": 0}).
		file("/etc/sysctl.d/99-sysctl.conf", "net.ipv4.conf.all.accept_redirects = 1\n").
		file(khost.File, khost.Header+"# before: net.ipv4.conf.all.accept_redirects = 1\nnet.ipv4.conf.*.accept_redirects = 0\n")
	if st := held.status(); st.State != engine.Partial || st.Detail != "1 set outside hardn" {
		t.Errorf("held = %+v", st)
	}
	if cs, err := held.plan(ks.ActionApply); err != nil || len(cs) != 0 {
		t.Errorf("held: Apply plans %v, %v", steps.IDs(cs), err)
	}
	drift := newModel(t, map[string]int{"kernel.kptr_restrict": 0, "net.ipv4.conf.*.send_redirects": 0}).
		file(khost.File, khost.Header+"# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n")
	if st := drift.status(); st.State != engine.Partial || st.Detail != "1 to set" {
		t.Errorf("below, beside hardn's file = %+v", st)
	}
	if ids, err := drift.apply(ks.ActionApply); err != nil || !slices.Equal(ids, []string{"apply"}) || drift.value("kernel.kptr_restrict") != 1 {
		t.Errorf("the file as it is, set again: %v, %v", ids, err)
	}
	clean := newModel(t, map[string]int{"net.ipv4.conf.*.send_redirects": 0})
	if st := clean.status(); st.State != engine.Configured || st.Detail != "" {
		t.Errorf("at the baseline = %+v", st)
	}
	inside := newModel(t, rocky)
	inside.facts.Container.Detected = detect.Yes
	if st := inside.status(); st.State != engine.NotApplicable || ks.Shown(st) {
		t.Errorf("container = %+v", st)
	}
	if st := newModel(t, rocky).file(khost.ProcPath("kernel.dmesg_restrict"), "?\n").status(); st.State != engine.Unknown || st.Detail != ks.DetailCantRead {
		t.Errorf("unread = %+v", st)
	}
}

// TestRoutesAndProxmox: on a host that routes, and on Proxmox VE, Sending
// redirects and Reverse-path filter are never written, the other rows are,
// and the notice says so.
func TestRoutesAndProxmox(t *testing.T) {
	routes := newModel(t, map[string]int{"net.ipv4.ip_forward": 1, "net.ipv4.conf.*.rp_filter": 0, "kernel.kptr_restrict": 0})
	pve := newModel(t, map[string]int{"net.ipv4.conf.*.rp_filter": 0, "kernel.kptr_restrict": 0})
	pve.facts.Platform.Distro = detect.DistroProxmox
	for name, m := range map[string]*model{"routes": routes, "pve": pve} {
		r := ks.Read(context.Background(), m.f, m.facts)
		if n := ks.Notice(r, ks.ActionApply); !strings.Contains(n, ks.NoticeRoutes) {
			t.Errorf("%s: notice = %q", name, n)
		}
		if _, err := m.apply(ks.ActionApply); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := m.content(khost.File); got != khost.Header+"# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n" {
			t.Errorf("%s: file:\n%s", name, got)
		}
		if m.value("net.ipv4.conf.eth0.send_redirects") != 1 || m.value("net.ipv4.conf.eth0.rp_filter") != 0 || m.value("net.ipv4.ip_forward") != routes.value("net.ipv4.ip_forward")*boolInt(name == "routes") {
			t.Errorf("%s: a row left alone changed", name)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestNeverLowered: a stricter value is left, in the file and on the host.
func TestNeverLowered(t *testing.T) {
	m := newModel(t, map[string]int{"kernel.perf_event_paranoid": 4, "fs.protected_regular": 2, "kernel.kptr_restrict": 2, "fs.protected_fifos": 0, "net.ipv4.conf.*.send_redirects": 0}).
		file(khost.File, khost.Header+"# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n")
	if _, err := m.apply(ks.ActionApply); err != nil {
		t.Fatal(err)
	}
	if m.value("kernel.perf_event_paranoid") != 4 || m.value("fs.protected_regular") != 2 || m.value("kernel.kptr_restrict") != 2 || m.value("fs.protected_fifos") != 1 {
		t.Errorf("a value was lowered, or one not set:\n%s", m.content(khost.File))
	}
}

// TestOnlyWhatIsBelow: Apply sets the keys below the baseline and no
// other, whatever hardn's file already holds. The file is not loaded
// again, so a line it has changes nothing now.
func TestOnlyWhatIsBelow(t *testing.T) {
	fifos := "# before: fs.protected_fifos = 0\nfs.protected_fifos = 1\n"
	sent := "# before: net.ipv4.conf.all.send_redirects = 1\n# before: net.ipv4.conf.eth0.send_redirects = 1\nnet.ipv4.conf.*.send_redirects = 0\n"
	rp := "# before: net.ipv4.conf.eth0.rp_filter = 0\nnet.ipv4.conf.eth0.rp_filter = 2\n"
	wg := "# before: net.ipv4.conf.wg0.rp_filter = 0\nnet.ipv4.conf.wg0.rp_filter = 2\n"
	bpf := "# before: kernel.unprivileged_bpf_disabled = 0\nkernel.unprivileged_bpf_disabled = 2\n"
	redirects := "# before: net.ipv4.conf.all.accept_redirects = 1\n# before: net.ipv4.conf.eth0.accept_redirects = 1\nnet.ipv4.conf.*.accept_redirects = 0\n"
	tests := []struct {
		name  string
		m     *model
		file  string         // hardn's file after
		stays map[string]int // values Apply must leave
	}{
		// The host started routing since: its two rows are left alone
		// (decision 6), their lines kept for Remove settings.
		{"routes since", newModel(t, map[string]int{"net.ipv4.ip_forward": 1, "net.ipv4.conf.*.send_redirects": 1, "net.ipv4.conf.eth0.rp_filter": 0, "net.ipv4.conf.all.rp_filter": 0, "fs.protected_fifos": 0}).
			file(khost.File, khost.Header+sent+rp), khost.Header + sent + rp + fifos,
			map[string]int{"net.ipv4.conf.all.send_redirects": 1, "net.ipv4.conf.eth0.send_redirects": 1, "net.ipv4.conf.eth0.rp_filter": 0}},
		// A later file holds the row since (decision 11): its line goes.
		{"held since", newModel(t, map[string]int{"net.ipv4.conf.*.send_redirects": 0, "net.ipv4.conf.all.accept_redirects": 1, "fs.protected_fifos": 0}).
			file("/etc/sysctl.d/99-sysctl.conf", "net.ipv4.conf.all.accept_redirects = 1\n").file(khost.File, khost.Header+redirects),
			khost.Header + fifos, map[string]int{"net.ipv4.conf.all.accept_redirects": 1}},
		// An interface went away: its line goes, and nothing fails.
		{"interface gone", newModel(t, map[string]int{"net.ipv4.conf.*.send_redirects": 0, "fs.protected_fifos": 0}).file(khost.File, khost.Header+wg), khost.Header + fifos, nil},
		// Values hardn never writes: BPF at 1, a strict reverse-path
		// filter. The lines stay as hardn wrote them.
		{"passes below hardn's value", newModel(t, map[string]int{"net.ipv4.conf.*.send_redirects": 0, "kernel.unprivileged_bpf_disabled": 1, "net.ipv4.conf.eth0.rp_filter": 1, "fs.protected_fifos": 0}).
			file(khost.File, khost.Header+rp+bpf), khost.Header + rp + bpf + fifos,
			map[string]int{"kernel.unprivileged_bpf_disabled": 1, "net.ipv4.conf.eth0.rp_filter": 1}},
	}
	for _, tt := range tests {
		if _, err := tt.m.apply(ks.ActionApply); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if ran := tt.m.ran(); !slices.Equal(ran, []string{"sysctl -w fs.protected_fifos=1"}) {
			t.Errorf("%s: ran %q", tt.name, ran)
		}
		if got := tt.m.content(khost.File); got != tt.file {
			t.Errorf("%s: file:\n%s", tt.name, got)
		}
		for key, n := range tt.stays {
			if tt.m.value(key) != n {
				t.Errorf("%s: %s changed to %d", tt.name, key, tt.m.value(key))
			}
		}
	}
}

// TestRemoveWithInterfaceGone: Remove settings, failing, puts the file
// back and each value as it read, though the file names an interface that
// went away.
func TestRemoveWithInterfaceGone(t *testing.T) {
	text := khost.Header + "# before: net.ipv4.conf.wg0.rp_filter = 0\nnet.ipv4.conf.wg0.rp_filter = 2\n# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n" +
		"# before: kernel.yama.ptrace_scope = 0\nkernel.yama.ptrace_scope = 1\n"
	m := newModel(t, map[string]int{"net.ipv4.conf.*.send_redirects": 0}).file(khost.File, text)
	set := m.snapshot()
	m.sim.Refuse = map[string]bool{khost.PtraceKey: true}
	if _, err := m.apply(ks.ActionRemove); !errors.Is(err, ks.ErrNotSet) {
		t.Fatalf("remove: %v", err)
	}
	if k := same(set, m.snapshot()); k != "" || m.content(khost.File) != text {
		t.Errorf("%s not set again, or the file not back", k)
	}
	m = newModel(t, map[string]int{"net.ipv4.conf.*.send_redirects": 0}).file(khost.File, text)
	if _, err := m.apply(ks.ActionRemove); err != nil || !m.gone(khost.File) || m.value("kernel.kptr_restrict") != 0 {
		t.Errorf("remove: %v", err)
	}
}

// TestChangedSince: a file of hardn's that is not as the plan read it is
// refused at the apply, and nothing is set: by the engine, which plans
// again and compares, and by the change itself, which reads the file again
// before it writes or deletes it.
func TestChangedSince(t *testing.T) {
	m := newModel(t, rocky)
	_, err := m.apply(ks.ActionApply, func() { m.file(khost.File, khost.Header) })
	if !errors.Is(err, engine.ErrPlanChanged) || m.content(khost.File) != khost.Header || len(m.ran()) != 0 || m.value(khost.PtraceKey) != 0 {
		t.Errorf("apply: %v, ran %q", err, m.ran())
	}
	for _, a := range []ks.Action{ks.ActionApply, ks.ActionRemove} {
		m = newModel(t, rocky)
		if a == ks.ActionRemove {
			m.file(khost.File, rockyFile)
		}
		cs, err := m.plan(a)
		if err != nil {
			t.Fatal(err)
		}
		edited := rockyFile + "\n"
		m.file(khost.File, edited)
		ran, err := cs[0].Apply(context.Background(), m.f, engine.Run{})
		if !errors.Is(err, ks.ErrChangedSince) || output.CodeOf(err) != "KERNEL_SETTINGS_CHANGED_SINCE" || ran.Rollback == nil || m.content(khost.File) != edited {
			t.Errorf("action %d: the change's apply = %v", a, err)
		}
	}
}

// TestRemoveRollback: a value that cannot be set back fails Remove; the
// file is back and its values set again.
func TestRemoveRollback(t *testing.T) {
	m := newModel(t, rocky)
	if _, err := m.apply(ks.ActionApply); err != nil {
		t.Fatal(err)
	}
	set := m.snapshot()
	m.sim.Refuse = map[string]bool{khost.PtraceKey: true}
	if _, err := m.apply(ks.ActionRemove); !errors.Is(err, ks.ErrNotSet) {
		t.Fatalf("remove: %v", err)
	}
	if m.content(khost.File) != rockyFile {
		t.Error("hardn's file not put back")
	}
	if k := same(set, m.snapshot()); k != "" {
		t.Errorf("%s not set again", k)
	}
}

// TestPlanWritesNothing: planning reads only.
func TestPlanWritesNothing(t *testing.T) {
	m := newModel(t, rocky)
	cs, err := m.plan(ks.ActionApply)
	if err != nil || len(cs) != 2 {
		t.Fatal(err)
	}
	for _, c := range m.f.Calls() {
		switch c.Op {
		case "write-atomic", "write-in-place", "append", "remove", "removeall", "rename", "mkdir", "chmod", "chown", "run":
			t.Errorf("planning did %s", c)
		}
	}
	if !strings.Contains(cs[0].Diff, "+net.ipv6.conf.*.accept_redirects = 0") || len(cs[1].Commands) != 13 || cs[1].Commands[6] != "sysctl -w net.ipv6.conf.eth0.accept_redirects=0" {
		t.Errorf("plan: %s\n%v", cs[0].Diff, cs[1].Commands)
	}
	if n := len(cs[1].Rollback.Steps); n != 13 {
		t.Errorf("%d undo steps, want one for each value that changes", n)
	}
}
