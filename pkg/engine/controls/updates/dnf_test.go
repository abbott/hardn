package updates_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

const (
	dnfConfPath  = "/etc/dnf/automatic.conf"
	installTimer = "dnf-automatic-install.timer"
	plainTimer   = "dnf-automatic.timer"
)

func (m *model) timer(name, active, enabled string) *model {
	m.units["is-active "+name], m.units["is-enabled "+name] = active, enabled
	return m
}

func (m *model) read(t *testing.T) string {
	t.Helper()
	data, err := m.f.ReadFile(context.Background(), dnfConfPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestCheckDnf: the row on Rocky Linux, each line of el-updates.md §1.1.
func TestCheckDnf(t *testing.T) {
	for _, c := range []struct {
		name   string
		m      func(t *testing.T) *model
		state  engine.State
		detail string
		err    bool
	}{
		{"not installed", func(t *testing.T) *model { return dnfHost(t, false) }, engine.NotConfigured, updates.DetailNoDnfAuto, false},
		{"installed, timers off", func(t *testing.T) *model { return dnfHost(t, true) }, engine.NotConfigured, updates.DetailInstallTimerOff, false},
		{"the install timer on, every update", func(t *testing.T) *model {
			return dnfHost(t, true).timer(installTimer, "active", "enabled")
		}, engine.Configured, updates.DetailDnfAll, false},
		{"the install timer on, security", func(t *testing.T) *model {
			m := dnfHost(t, true).timer(installTimer, "active", "enabled")
			m.file(dnfConfPath, strings.Replace(dnfConf, "upgrade_type = default", "upgrade_type = security", 1))
			return m
		}, engine.Configured, updates.DetailDnfSecurity, false},
		{"the plain timer on, apply_updates no", func(t *testing.T) *model {
			return dnfHost(t, true).timer(plainTimer, "active", "enabled")
		}, engine.NotConfigured, updates.DetailInstallTimerOff, false},
		{"the plain timer on, apply_updates yes", func(t *testing.T) *model {
			m := dnfHost(t, true).timer(plainTimer, "active", "enabled")
			m.file(dnfConfPath, strings.Replace(dnfConf, "apply_updates = no", "apply_updates = yes", 1))
			return m
		}, engine.Configured, updates.DetailDnfAll, false},
		{"active, not enabled", func(t *testing.T) *model {
			return dnfHost(t, true).timer(installTimer, "active", "disabled")
		}, engine.Partial, updates.DetailOffAfterRestart, false},
		{"enabled, not active", func(t *testing.T) *model {
			return dnfHost(t, true).timer(installTimer, "inactive", "enabled")
		}, engine.Partial, updates.DetailStartsAfterRestart, false},
		{"the plain timer active only, apply_updates yes", func(t *testing.T) *model {
			m := dnfHost(t, true).timer(plainTimer, "active", "disabled")
			m.file(dnfConfPath, strings.Replace(dnfConf, "apply_updates = no", "apply_updates = yes", 1))
			return m
		}, engine.Partial, updates.DetailOffAfterRestart, false},
		{"the plain timer enabled only, apply_updates yes", func(t *testing.T) *model {
			m := dnfHost(t, true).timer(plainTimer, "inactive", "enabled")
			m.file(dnfConfPath, strings.Replace(dnfConf, "apply_updates = no", "apply_updates = yes", 1))
			return m
		}, engine.Partial, updates.DetailStartsAfterRestart, false},
		{"systemctl unread", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.f.Respond(hostfake.Response{Stdout: "Failed to connect to bus\n", ExitCode: 1}, "systemctl", "is-enabled", installTimer)
			return m
		}, engine.Unknown, updates.DetailCantRead, true},
		{"a value dnf doesn't take", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.file(dnfConfPath, strings.Replace(dnfConf, "upgrade_type = default", "upgrade_type = all", 1))
			return m
		}, engine.Unknown, updates.DetailCantRead, true},
	} {
		m := c.m(t)
		st, err := updates.Auto{}.Check(context.Background(), m.f, m.facts)
		if st.State != c.state || st.Detail != c.detail || (err != nil) != c.err {
			t.Errorf("%s: %s %q, %v; want %s %q", c.name, st.State, st.Detail, err, c.state, c.detail)
		}
	}
}

// TestDnfOnOff: on from not installed installs dnf-automatic, sets
// upgrade_type to security, enables and starts the install timer, and
// records each; off undoes each, last first: the timer stopped and
// disabled, the file put back to the very bytes, the package removed with
// no .rpmsave left; the record deleted.
func TestDnfOnOff(t *testing.T) {
	m := dnfHost(t, false)
	ids, err := m.apply(updates.AutoOn, nil)
	if err != nil || !slices.Equal(ids, []string{updates.ChangeInstall, updates.ChangeConf, updates.ChangeTimer, updates.ChangeRecord}) {
		t.Fatalf("on: %q, %v", ids, err)
	}
	st, _ := updates.Auto{}.Check(context.Background(), m.f, m.facts)
	if st.State != engine.Configured || st.Detail != updates.DetailDnfSecurity || !strings.Contains(m.read(t), "\nupgrade_type = security\n") {
		t.Errorf("on: %s %q", st.State, st.Detail)
	}
	rec := record(t, m)
	if rec == nil || len(rec.Entries) != 1 || !slices.Equal(rec.Entries[0].Steps, []string{"install", "conf", "enable", "start"}) ||
		!slices.Equal(rec.Entries[0].Lines, []string{"upgrade_type = security", "upgrade_type = default"}) || len(rec.Entries[0].Before) != 1 || len(rec.Entries[0].Files) != 1 {
		t.Fatalf("record %+v", rec)
	}
	ids, err = m.apply(updates.AutoOff, nil)
	if err != nil || !slices.Equal(ids, []string{updates.ChangeTimer, updates.ChangeConf, updates.ChangePackage, updates.ChangeRecord}) {
		t.Fatalf("off: %q, %v", ids, err)
	}
	if m.dnfAuto || m.units["is-active "+installTimer] == "active" || record(t, m) != nil {
		t.Errorf("off: installed %v, timer %s, record %v", m.dnfAuto, m.units["is-active "+installTimer], record(t, m))
	}
	if _, err := m.f.ReadFile(context.Background(), dnfConfPath+".rpmsave"); err == nil {
		t.Error("off left automatic.conf.rpmsave: the file wasn't the stock one at the remove")
	}
}

// in is Plan's input on m.
func in(m *model) engine.Input { return engine.Input{Facts: m.facts, Config: &config.Config{}} }

// planCode is the code of a's plan on m, and the plan.
func planCode(t *testing.T, m *model, a updates.AutoAction) ([]engine.Change, string) {
	t.Helper()
	cs, err := control(a).Plan(context.Background(), m.f, in(m))
	return cs, output.CodeOf(err)
}

// ran is whether m ran a command starting with prefix.
func ran(m *model, prefix string) bool {
	return slices.ContainsFunc(m.f.Runs(), func(a []string) bool { return strings.HasPrefix(strings.Join(a, " "), prefix) })
}

// TestDnfOnRefusals: on refused before anything is planned: rpm -q or a
// timer unread (UPDATES_NOT_READ); the install timer masked, for this boot
// too, or before the install (UPDATES_JOB_MASKED, nothing installed);
// automatic.conf missing or with no [commands] where the package is
// installed, or there before the package (UPDATES_DNF_CONF); the install's
// simulation refused, each in its code.
func TestDnfOnRefusals(t *testing.T) {
	gone := func(t *testing.T, m *model) {
		t.Helper()
		if ran(m, "dnf install -y") {
			t.Error("installed")
		}
	}
	upgrading := hostfake.Response{Stdout: "Dependencies resolved.\nInstalling:\n dnf-automatic  noarch  4.20.0  baseos  37 k\nUpgrading:\n dnf  noarch  4.20.1  baseos  1 M\n\nTransaction Summary\n",
		Stderr: "Operation aborted.\n", ExitCode: 1}
	kernel := hostfake.Response{Stdout: "Dependencies resolved.\nInstalling:\n dnf-automatic  noarch  4.20.0  baseos  37 k\nInstalling dependencies:\n kernel-core  x86_64  6.12.0-211.74.1.el10_2  baseos  19 M\n\nTransaction Summary\n",
		Stderr: "Operation aborted.\n", ExitCode: 1}
	for _, c := range []struct {
		name, code, words string
		m                 func(t *testing.T) *model
	}{
		{"rpm -q unread", "UPDATES_NOT_READ", "can't read dnf-automatic", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.f.Respond(hostfake.Response{Stderr: "error: rpmdb: damaged header\n", ExitCode: 1}, "rpm", "-q", "dnf-automatic")
			return m
		}},
		{"a timer unread", "UPDATES_NOT_READ", "can't read dnf-automatic", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.f.Respond(hostfake.Response{Stdout: "Failed to connect to bus\n", ExitCode: 1}, "systemctl", "is-active", plainTimer)
			return m
		}},
		{"masked for this boot", "UPDATES_JOB_MASKED", "is masked", func(t *testing.T) *model {
			return dnfHost(t, true).timer(installTimer, "inactive", "masked-runtime")
		}},
		{"masked before the install", "UPDATES_JOB_MASKED", "is masked", func(t *testing.T) *model {
			m := dnfHost(t, false)
			m.units["is-enabled "+installTimer] = "masked"
			return m
		}},
		{"automatic.conf missing", "UPDATES_DNF_CONF", "automatic.conf is missing", func(t *testing.T) *model {
			m := dnfHost(t, true)
			_ = m.f.Remove(context.Background(), dnfConfPath)
			return m
		}},
		{"no [commands]", "UPDATES_DNF_CONF", "upgrade_type in /etc/dnf/automatic.conf: no [commands] section", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.file(dnfConfPath, "[emitters]\nemit_via = stdio\n")
			return m
		}},
		{"automatic.conf before the package", "UPDATES_DNF_CONF", "automatic.conf is there before dnf-automatic", func(t *testing.T) *model {
			m := dnfHost(t, false)
			m.file(dnfConfPath, dnfConf)
			return m
		}},
		{"not in the repositories", "UPDATES_NOT_IN_REPOS", "can't find dnf-automatic", func(t *testing.T) *model {
			m := dnfHost(t, false)
			m.dnfInstall = &hostfake.Response{Stdout: "No match for argument: dnf-automatic\n", Stderr: "Error: Unable to find a match: dnf-automatic\n", ExitCode: 1}
			return m
		}},
		{"no enabled repository (an unregistered RHEL host)", "PACKAGE_NO_REPOS", "hardn can't install dnf-automatic: dnf has no enabled repositories", func(t *testing.T) *model {
			m := dnfHost(t, false)
			m.dnfInstall = &hostfake.Response{Stdout: "Updating Subscription Management repositories.\nUnable to read consumer identity\n\nThis system is not registered with an entitlement server. You can use \"rhc\" or \"subscription-manager\" to register.\n\n", Stderr: "Error: There are no enabled repositories in \"/etc/yum.repos.d\", \"/etc/yum/repos.d\", \"/etc/distro.repos.d\".\n", ExitCode: 1} // rhel10-test-114, probe 3
			return m
		}},
		{"another package changed", "UPDATES_OTHER_PACKAGES", "Upgrading dnf", func(t *testing.T) *model {
			m := dnfHost(t, false)
			m.dnfInstall = &upgrading
			return m
		}},
		{"a new kernel", "PACKAGE_KERNEL", "would also install a new kernel (kernel-core)", func(t *testing.T) *model {
			m := dnfHost(t, false)
			m.dnfInstall = &kernel
			m.f.Respond(hostfake.Response{Stdout: "6.12.0-211.16.1.el10_2.0.1.x86_64\n"}, "uname", "-r")
			return m
		}},
		{"the lock", "PACKAGE_LOCKED", "another dnf is running", func(t *testing.T) *model {
			m := dnfHost(t, false)
			m.dnfInstall = &hostfake.Response{Stderr: "Error: [Errno 11] Resource temporarily unavailable\n", ExitCode: 200}
			return m
		}},
	} {
		m := c.m(t)
		cs, err := control(updates.AutoOn).Plan(context.Background(), m.f, in(m))
		if output.CodeOf(err) != c.code || err == nil || !strings.Contains(err.Error(), c.words) || len(cs) != 0 {
			t.Errorf("%s: %+v, %v (%s)", c.name, cs, err, output.CodeOf(err))
		}
		gone(t, m)
	}
}

// TestDnfRestarts: on's review says the restarts automatic.conf asks for,
// a warning; none where it says never.
func TestDnfRestarts(t *testing.T) {
	for reboot, want := range map[string][]updates.Note{
		"never":        nil,
		"when-needed":  {{Key: "Restarts", Value: "When needed", Note: "by automatic.conf", Warn: true}},
		"when-changed": {{Key: "Restarts", Value: "When changed", Note: "by automatic.conf", Warn: true}},
	} {
		m := dnfHost(t, true)
		m.file(dnfConfPath, strings.Replace(dnfConf, "reboot = never", "reboot = "+reboot, 1))
		notes, err := control(updates.AutoOn).Notes(context.Background(), m.f, in(m))
		if err != nil || !slices.Equal(notes, want) {
			t.Errorf("%s: %+v, %v", reboot, notes, err)
		}
	}
}

// TestDnfOffStaysOn: on from Partial (the install timer enabled outside
// hardn, or started outside it) then off undoes hardn's steps alone and
// succeeds, the review saying updates stay on: the timer as it was, the
// file stock, the record gone. A stop that doesn't take still fails the
// read-back.
func TestDnfOffStaysOn(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name            string
		active, enabled string
	}{{"enabled, inactive", "inactive", "enabled"}, {"active, not enabled", "active", "disabled"}} {
		m := dnfHost(t, true).timer(installTimer, c.active, c.enabled).applied(updates.AutoOn)
		notes, err := control(updates.AutoOff).Notes(ctx, m.f, in(m))
		if err != nil || !slices.Contains(notes, updates.Note{Key: "Updates", Value: "Stay on", Note: "timer on before hardn", Warn: true}) {
			t.Errorf("%s: notes %+v, %v", c.name, notes, err)
		}
		if _, err := m.apply(updates.AutoOff, nil); err != nil {
			t.Fatalf("%s: off: %v", c.name, err)
		}
		if m.units["is-active "+installTimer] != c.active || m.units["is-enabled "+installTimer] != c.enabled || m.read(t) != dnfConf || record(t, m) != nil {
			t.Errorf("%s: off: %s %s, %q, record %v", c.name, m.units["is-active "+installTimer], m.units["is-enabled "+installTimer], m.read(t), record(t, m))
		}
	}
	m := dnfHost(t, true).timer(installTimer, "inactive", "enabled").applied(updates.AutoOn)
	m.noTake = map[string]bool{"stop": true}
	if _, err := m.apply(updates.AutoOff, nil); output.CodeOf(err) != "UPDATES_NOT_IN_EFFECT" || record(t, m) == nil {
		t.Errorf("a stop that doesn't take: %v (%s)", err, output.CodeOf(err))
	}
}

// TestDnfOnTwice: on, the timer disabled outside hardn, on again (an entry
// that only enables it): off puts automatic.conf back by the entry that
// edited it, to the stock bytes.
func TestDnfOnTwice(t *testing.T) {
	m := dnfHost(t, true).applied(updates.AutoOn)
	m.units["is-enabled "+installTimer] = "disabled"
	m.applied(updates.AutoOn)
	if rec := record(t, m); len(rec.Entries) != 2 || !slices.Equal(rec.Entries[1].Steps, []string{"enable"}) {
		t.Fatalf("record %+v", rec)
	}
	if _, err := m.apply(updates.AutoOff, nil); err != nil || m.read(t) != dnfConf || record(t, m) != nil {
		t.Errorf("off: %v, %q", err, m.read(t))
	}
}

// TestDnfOnRolledBack: a start or an enable that doesn't take fails on
// (UPDATES_NOT_IN_EFFECT), rolled back: the package removed, no
// automatic.conf nor its .rpmsave or .rpmnew, the timer not enabled, no
// record. A write of automatic.conf that doesn't take fails the conf
// change.
func TestDnfOnRolledBack(t *testing.T) {
	ctx := context.Background()
	for _, verb := range []string{"start", "enable"} {
		m := dnfHost(t, false)
		m.noTake = map[string]bool{verb: true}
		_, err := m.apply(updates.AutoOn, nil)
		var ae *engine.ApplyError
		if output.CodeOf(err) != "UPDATES_NOT_IN_EFFECT" || !errors.As(err, &ae) || ae.RollbackErr != nil {
			t.Fatalf("%s: %v (%s)", verb, err, output.CodeOf(err))
		}
		for _, p := range []string{dnfConfPath, dnfConfPath + ".rpmsave", dnfConfPath + ".rpmnew"} {
			if _, err := m.f.ReadFile(ctx, p); err == nil {
				t.Errorf("%s: %s left", verb, p)
			}
		}
		if m.dnfAuto || m.units["is-enabled "+installTimer] == "enabled" || record(t, m) != nil {
			t.Errorf("%s: installed %v, timer %s, record %v", verb, m.dnfAuto, m.units["is-enabled "+installTimer], record(t, m))
		}
	}
	m := dnfHost(t, true)
	fz := frozen{m.f}
	cs, err := control(updates.AutoOn).Plan(ctx, fz, in(m))
	i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == updates.ChangeConf })
	if err != nil || i < 0 {
		t.Fatalf("on: %v, %+v", err, cs)
	}
	if _, err := cs[i].Apply(ctx, fz, engine.Run{}); output.CodeOf(err) != "UPDATES_NOT_IN_EFFECT" {
		t.Errorf("a write that doesn't take: %v (%s)", err, output.CodeOf(err))
	}
}

// TestDnfOnApplyChecks: automatic.conf changed between plan and apply so
// that hardn can't set upgrade_type refuses the conf change, writing
// nothing; an install whose dnf isn't found fails with nothing to undo.
func TestDnfOnApplyChecks(t *testing.T) {
	ctx := context.Background()
	m := dnfHost(t, true)
	cs, err := control(updates.AutoOn).Plan(ctx, m.f, in(m))
	i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == updates.ChangeConf })
	if err != nil || i < 0 {
		t.Fatalf("on: %v, %+v", err, cs)
	}
	m.file(dnfConfPath, "[emitters]\nemit_via = stdio\n")
	if _, err := cs[i].Apply(ctx, m.f, engine.Run{}); output.CodeOf(err) != "UPDATES_NOT_READ" || m.read(t) != "[emitters]\nemit_via = stdio\n" {
		t.Errorf("changed between plan and apply: %v (%s), %q", err, output.CodeOf(err), m.read(t))
	}
	m = dnfHost(t, false)
	m.f.Respond(hostfake.Response{Err: host.ErrNotFound}, "dnf", "install", "-y", "--setopt=install_weak_deps=False", "--setopt=exit_on_lock=True", "dnf-automatic")
	_, err = m.apply(updates.AutoOn, nil)
	var ae *engine.ApplyError
	if !errors.As(err, &ae) || !errors.Is(err, host.ErrNotFound) || ae.RollbackErr != nil || ran(m, "dnf remove") {
		t.Errorf("dnf not found: %v", err)
	}
}

// frozen is a fake on which writes of automatic.conf don't take.
type frozen struct{ *hostfake.System }

func (f frozen) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if name == dnfConfPath {
		return nil
	}
	return f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// TestDnfOffAsFound: off plans only what is still as hardn left it: no
// stop where the timer was stopped outside hardn, no disable where it was
// disabled; with the package removed outside hardn, neither the put-back
// nor the remove, the record deleted (and the reading says the package is
// gone); automatic.conf deleted is UPDATES_CHANGED_SINCE; a dependent
// package removed with dnf-automatic is UPDATES_OTHER_PACKAGES.
func TestDnfOffAsFound(t *testing.T) {
	ctx := context.Background()
	for verb, want := range map[string]string{"is-active": "systemctl disable " + installTimer, "is-enabled": "systemctl stop " + installTimer} {
		m := dnfHost(t, true).applied(updates.AutoOn)
		m.units[verb+" "+installTimer] = map[string]string{"is-active": "inactive", "is-enabled": "disabled"}[verb]
		cs, code := planCode(t, m, updates.AutoOff)
		i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == updates.ChangeTimer })
		if code != "" || i < 0 || !slices.Equal(cs[i].Commands, []string{want}) {
			t.Errorf("%s outside hardn: %s, %+v", verb, code, cs)
		}
	}
	m := dnfHost(t, false).applied(updates.AutoOn)
	m.dnf(ctx, []string{"dnf", "remove", "-y", "--noautoremove", "--setopt=exit_on_lock=True", "dnf-automatic"})
	if r, _ := control(updates.AutoNone).Read(ctx, m.f, m.facts, nil); !r.PackageGone {
		t.Errorf("removed outside hardn: the reading %+v", r)
	}
	if ids, err := m.apply(updates.AutoOff, nil); err != nil || !slices.Equal(ids, []string{updates.ChangeRecord}) || record(t, m) != nil {
		t.Errorf("removed outside hardn: %q, %v", ids, err)
	}
	m = dnfHost(t, true).applied(updates.AutoOn)
	_ = m.f.Remove(ctx, dnfConfPath)
	if _, code := planCode(t, m, updates.AutoOff); code != "UPDATES_CHANGED_SINCE" {
		t.Errorf("automatic.conf deleted: %s", code)
	}
	m = dnfHost(t, false).applied(updates.AutoOn)
	m.dnfRemove = &hostfake.Response{Stdout: "Dependencies resolved.\nRemoving:\n dnf-automatic  noarch  4.20.0  @baseos  79 k\nRemoving dependent packages:\n cockpit-pkg  noarch  1-1  @appstream  10 k\n\nTransaction Summary\n",
		Stderr: "Operation aborted.\n", ExitCode: 1}
	if _, code := planCode(t, m, updates.AutoOff); code != "UPDATES_OTHER_PACKAGES" {
		t.Errorf("a dependent package: %s", code)
	}
}

// TestDnfStale: the check's finding after hardn's on: automatic.conf
// edited names it; the package gone, the file as hardn left it, names rpm's
// query.
func TestDnfStale(t *testing.T) {
	ctx := context.Background()
	source := func(m *model) string {
		st, _ := updates.Auto{}.Check(ctx, m.f, m.facts)
		for _, f := range st.Findings {
			if f.Code == updates.CodeChangedSince {
				return f.Source
			}
		}
		return ""
	}
	m := dnfHost(t, false).applied(updates.AutoOn)
	if got := source(m); got != "" {
		t.Errorf("as left: %q", got)
	}
	m.file(dnfConfPath, m.read(t)+"# an operator's line\n")
	if got := source(m); got != dnfConfPath {
		t.Errorf("edited: %q", got)
	}
	m = dnfHost(t, false).applied(updates.AutoOn)
	m.dnfAuto = false
	if got := source(m); got != "rpm -q dnf-automatic" {
		t.Errorf("package gone: %q", got)
	}
}

// TestDnfOnInstalled: installed with its timers off, on sets the line and
// the timer, no install; off, whose review doesn't say updates stay on,
// puts both back and keeps the package.
func TestDnfOnInstalled(t *testing.T) {
	m := dnfHost(t, true)
	ids, err := m.apply(updates.AutoOn, nil)
	if err != nil || !slices.Equal(ids, []string{updates.ChangeConf, updates.ChangeTimer, updates.ChangeRecord}) {
		t.Fatalf("on: %q, %v", ids, err)
	}
	notes, err := control(updates.AutoOff).Notes(context.Background(), m.f, in(m))
	if err != nil || slices.ContainsFunc(notes, func(n updates.Note) bool { return n.Key == "Updates" }) {
		t.Errorf("off's notes: %+v, %v", notes, err)
	}
	if _, err := m.apply(updates.AutoOff, nil); err != nil {
		t.Fatal(err)
	}
	if !m.dnfAuto || m.read(t) != dnfConf || m.units["is-enabled "+installTimer] != "disabled" || m.units["is-active "+installTimer] != "inactive" {
		t.Errorf("off: installed %v, %q, %s %s", m.dnfAuto, m.read(t), m.units["is-enabled "+installTimer], m.units["is-active "+installTimer])
	}
}

// TestDnfRefusals: on outside hardn plans nothing; off with no record has
// nothing to undo; on with the install timer masked is refused; off after
// automatic.conf changed is refused, naming it, and so is the put-back when
// the file changes between plan and apply.
func TestDnfRefusals(t *testing.T) {
	ctx := context.Background()
	m := dnfHost(t, true).timer(installTimer, "active", "enabled")
	if cs, err := control(updates.AutoOn).Plan(ctx, m.f, in(m)); err != nil || len(cs) != 0 {
		t.Errorf("on outside hardn: %+v, %v", cs, err)
	}
	if _, err := control(updates.AutoOff).Plan(ctx, m.f, in(m)); output.CodeOf(err) != "UPDATES_NOTHING_TO_UNDO" {
		t.Errorf("off, no record: %v (%s)", err, output.CodeOf(err))
	}
	m = dnfHost(t, true).timer(installTimer, "inactive", "masked")
	if _, err := control(updates.AutoOn).Plan(ctx, m.f, in(m)); output.CodeOf(err) != "UPDATES_JOB_MASKED" || !errors.Is(err, updates.ErrJobMasked) {
		t.Errorf("masked: %v (%s)", err, output.CodeOf(err))
	}
	m = dnfHost(t, true).applied(updates.AutoOn)
	m.file(dnfConfPath, m.read(t)+"# an operator's line\n")
	_, err := control(updates.AutoOff).Plan(ctx, m.f, in(m))
	if output.CodeOf(err) != "UPDATES_CHANGED_SINCE" || !strings.Contains(err.Error(), "automatic.conf") {
		t.Errorf("changed since: %v (%s)", err, output.CodeOf(err))
	}
	m = dnfHost(t, true).applied(updates.AutoOn)
	cs, err := control(updates.AutoOff).Plan(ctx, m.f, in(m))
	i := slices.IndexFunc(cs, func(c engine.Change) bool { return c.ID == updates.ChangeConf })
	if err != nil || i < 0 {
		t.Fatalf("off: %v, %+v", err, cs)
	}
	m.file(dnfConfPath, m.read(t)+"# between plan and apply\n")
	if _, err := cs[i].Apply(ctx, m.f, engine.Run{}); output.CodeOf(err) != "UPDATES_CHANGED_SINCE" {
		t.Errorf("changed between plan and apply: %v (%s)", err, output.CodeOf(err))
	}
	r, _ := control(updates.AutoNone).Read(ctx, m.f, m.facts, nil)
	if r.Changed != dnfConfPath || !slices.ContainsFunc(r.Status.Findings, func(f engine.Finding) bool { return f.Code == updates.CodeChangedSince }) {
		t.Errorf("the row's finding: changed %q, findings %+v", r.Changed, r.Status.Findings)
	}
}

// TestDnfOffRecordChecks: off's put-back is refused where the file isn't
// the recorded after-hash, and where the bytes put back wouldn't hash to
// the recorded before; each check refuses on its own.
func TestDnfOffRecordChecks(t *testing.T) {
	ctx := context.Background()
	for name, edit := range map[string]func(e *revert.RecordEntry){
		"after-hash":    func(e *revert.RecordEntry) { e.Files[0].SHA256 = strings.Repeat("0", 64) },
		"replaced line": func(e *revert.RecordEntry) { e.Lines[1] = "upgrade_type = default # edited" },
	} {
		m := dnfHost(t, true).applied(updates.AutoOn)
		rec := record(t, m)
		edit(&rec.Entries[0])
		path, err := revert.RecordPath(revert.Options{}, updates.ID)
		data, jerr := json.Marshal(rec)
		if err != nil || jerr != nil {
			t.Fatal(err, jerr)
		}
		m.file(path, string(data))
		_, err = control(updates.AutoOff).Plan(ctx, m.f, engine.Input{Facts: m.facts, Config: &config.Config{}})
		if output.CodeOf(err) != "UPDATES_CHANGED_SINCE" {
			t.Errorf("%s: %v (%s)", name, err, output.CodeOf(err))
		}
	}
}
