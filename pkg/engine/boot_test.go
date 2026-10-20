package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The boot unit before the first Arm (next/p3-engine-boot): planned as a
// change of its own, shown first, installed under the run lock before
// revert.Arm, idempotent, refused while a change is journalled, and never
// journalled or rolled back.

var alpine324 = detect.Facts{
	Platform: detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported},
	Init:     detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir},
}

// portControl is a lockout-risk control with one firewall change.
func portControl() Control {
	return &stub{id: "example.port", title: "Port", changes: []Change{
		command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230))}}
}

// changeIDs are p's changes' ids, in order.
func changeIDs(p *Plan) []string {
	var ids []string
	for _, c := range p.Changes {
		ids = append(ids, c.ID)
	}
	return ids
}

// TestBootUnitPlanned: on a host without the boot unit, a lockout-risk plan
// shows it first, as a change of its own (StageBoot, RiskPlain, no control),
// and the apply installs it before revert.Arm and under the run lock, with
// the check of its copy and its enable, as p2drive does; nothing of it is
// journalled.
func TestBootUnitPlanned(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts detect.Facts
		cmds  []string
		armed string // the first command or start of Arm
	}{
		{"systemd", debian13, []string{"systemd-analyze verify --recursive-errors=no /run/hardn/<id>/hardn-boot.service",
			"systemctl --no-reload enable hardn-boot.service"}, "systemd-run"},
		{"OpenRC", alpine324, []string{"sh -n /run/hardn/<id>/hardn-boot", "rc-update add hardn-boot default"}, "/bin/sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newBareHost(t)
			var evs []Event
			o := testOptions(h, nil, nil, tc.facts)
			o.Progress = func(ev Event) { evs = append(evs, ev) }
			e := New(h, o)
			p, err := e.Plan(ctx, []Control{portControl()}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			u, err := revert.PlanBootUnit(tc.facts.Init, testHardn)
			if err != nil {
				t.Fatal(err)
			}
			if got := changeIDs(p); !slices.Equal(got, []string{BootChangeID, "example.port/allow"}) {
				t.Fatalf("changes %q", got)
			}
			b := p.Changes[0]
			var cmds []string
			for _, c := range b.Commands {
				cmds = append(cmds, strings.ReplaceAll(c, p.ID, "<id>"))
			}
			if b.Stage != StageBoot || b.Risk != RiskPlain || b.Control != "" || b.Target != u.Path || len(b.Backups) > 0 ||
				len(b.Rollback.Steps)+len(b.Rollback.Reloads) > 0 || b.StateBefore != "not installed" ||
				!strings.HasPrefix(b.Summary, "Install hardn's boot unit, "+u.Path) || !slices.Equal(cmds, tc.cmds) ||
				!strings.HasPrefix(b.Diff, "--- /dev/null\n+++ "+u.Path+"\n") {
				t.Errorf("the boot unit's change: %+v", b)
			}
			if p.Risk != RiskLockout || p.Summary() != p.Changes[1].Summary || len(p.Items) != 1 ||
				!slices.Equal(p.Items[0].Changes, []string{"example.port/allow"}) {
				t.Errorf("risk %v, summary %q, items %+v", p.Risk, p.Summary(), p.Items)
			}
			res, err := e.Apply(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Applied) != 2 || res.Applied[0].Change != BootChangeID || res.Applied[0].Detail != "written, enabled" {
				t.Errorf("applied %+v", res.Applied)
			}
			// The order: the run lock, the check, the enable, then Arm.
			var order []string
			for _, c := range h.Calls() {
				switch {
				case c.Op == "lock" && c.Path == lockFile:
					order = append(order, "run lock")
				case c.Op == "run" || c.Op == "start-detached":
					order = append(order, strings.ReplaceAll(host.FormatArgv(c.Argv), p.ID, "<id>"))
				}
			}
			want := append(append([]string{"run lock"}, tc.cmds...), tc.armed)
			if len(order) < len(want) || !slices.Equal(order[:len(want)-1], want[:len(want)-1]) || !strings.HasPrefix(order[len(want)-1], tc.armed) {
				t.Errorf("order %q, want %q first", order, want)
			}
			if e, ok := h.Entry(u.Path); !ok || string(e.Data) != string(u.Data) || e.Mode.Perm() != u.Mode.Perm() || e.UID != 0 {
				t.Errorf("the unit %s: %+v (exists %v)", u.Path, e, ok)
			}
			if e, ok := h.Entry(u.Link); !ok || e.Target != u.Path {
				t.Errorf("the link %s: %+v (exists %v)", u.Link, e, ok)
			}
			st, err := revert.ReadState(ctx, h, revert.Options{})
			if err != nil || st.Pending == nil || st.Pending.Backup != nil || len(st.Pending.Undo) != 1 {
				t.Errorf("the journal: %+v, %v; want the firewall's undo alone, no backup", st.Pending, err)
			}
			if got := renderEvents(evs); !strings.HasPrefix(got, "0 "+BootChangeID+" running\n0 "+BootChangeID+" done: written, enabled\n1 example.port/allow running\n") {
				t.Errorf("events:\n%s", got)
			}
			assertReleased(t, h, p.ID)
		})
	}
}

// TestBootUnitIdempotent: the boot unit's change shows only what is not as
// planned, and nothing once it is; it is never planned for a plan without
// lockout risk. A stale or foreign file is backed up, unjournalled, into
// the change's backup run, and replaced (revert-boot.md §2.16).
func TestBootUnitIdempotent(t *testing.T) {
	u, err := revert.PlanBootUnit(debian13.Init, testHardn)
	if err != nil {
		t.Fatal(err)
	}
	other, err := revert.PlanBootUnit(debian13.Init, "/opt/hardn/bin/hardn")
	if err != nil {
		t.Fatal(err)
	}
	enable := "systemctl --no-reload enable hardn-boot.service"
	for _, tc := range []struct {
		name   string
		setup  func(h *testHost)
		plain  bool     // a plan without lockout risk
		before string   // "": no boot change
		cmds   []string // its commands, by their first words
		diff   string   // in its Diff
		detail string
		backup bool // the file replaced is in the backup run, entry 1
		off    bool // enableBackups false
	}{
		{"installed and enabled", func(h *testHost) { installBootUnit(t, h, debian13.Init) }, false, "", nil, "", "", false, false},
		{"no lockout risk", nil, true, "", nil, "", "", false, false},
		{"installed, not enabled", func(h *testHost) { h.AddFile(u.Path, u.Data, u.Mode, 0, 0) }, false,
			"installed, not enabled", []string{enable}, "", "enabled", false, false},
		{"enabled, its mode not as planned", func(h *testHost) {
			h.AddFile(u.Path, u.Data, 0o600, 0, 0)
			h.AddSymlink(u.Link, u.Path)
		}, false, "mode 0600, owner uid 0, where hardn installs it 0644, root's (backed up, then replaced)", []string{"systemd-analyze"}, "",
			"the file it replaced backed up (entry 1), written", true, false},
		{"another hardn's unit", func(h *testHost) {
			h.AddFile(u.Path, other.Data, u.Mode, 0, 0)
			h.AddSymlink(u.Link, u.Path)
		}, false, "a file that is not the unit this hardn installs (backed up, then replaced)", []string{"systemd-analyze"},
			"-ExecStart=/opt/hardn/bin/hardn rollback --boot\n+ExecStart=/usr/local/bin/hardn rollback --boot\n",
			"the file it replaced backed up (entry 1), written", true, false},
		{"another hardn's unit, backups off", func(h *testHost) {
			h.AddFile(u.Path, other.Data, u.Mode, 0, 0)
			h.AddSymlink(u.Link, u.Path)
		}, false, "a file that is not the unit this hardn installs (replaced, not backed up: enableBackups is false)", []string{"systemd-analyze"},
			"-ExecStart=/opt/hardn/bin/hardn", "the file it replaced not backed up (enableBackups is false), written", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newBareHost(t)
			if tc.setup != nil {
				tc.setup(h)
			}
			o := testOptions(h, nil, nil, debian13)
			o.Backups = backup.Settings{Path: backup.DefaultPath, Enabled: !tc.off}
			e := New(h, o)
			c := portControl()
			if tc.plain {
				c = &stub{id: "example.web", title: "Web", changes: []Change{command("example.web/reload", StageReload, RiskServiceRestart, argv(reloadWeb), argv(reloadBack))}}
			}
			p, err := e.Plan(ctx, []Control{c}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			planned := p.Changes[0].ID == BootChangeID
			if planned != (tc.before != "") {
				t.Fatalf("changes %q; want the boot unit's %v", changeIDs(p), tc.before != "")
			}
			if planned {
				b := p.Changes[0]
				ok := b.StateBefore == tc.before && len(b.Commands) == len(tc.cmds) && strings.Contains(b.Diff, tc.diff) && (tc.diff != "" || b.Diff == "")
				for i, w := range tc.cmds {
					ok = ok && strings.HasPrefix(b.Commands[i], w)
				}
				if !ok {
					t.Errorf("the boot unit's change: before %q, commands %q, diff %q", b.StateBefore, b.Commands, b.Diff)
				}
			}
			res, err := e.Apply(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if planned && res.Applied[0].Detail != tc.detail {
				t.Errorf("detail %q, want %q", res.Applied[0].Detail, tc.detail)
			}
			if st, err := revert.ReadBootUnit(ctx, h, u); err != nil || st.Installed() != !tc.plain {
				t.Errorf("installed %v, %v; want %v", st.Installed(), err, !tc.plain)
			}
			m, err := backup.Load(ctx, h, backup.DefaultPath+"/"+p.ID)
			kept := err == nil && len(m.Entries) > 0 && m.Entries[0].Path == u.Path && m.Entries[0].Seq == 1
			if kept != tc.backup {
				t.Errorf("the replaced file in the backup run %v (%+v, %v), want %v", kept, m.Entries, err, tc.backup)
			}
			if st, _ := revert.ReadState(ctx, h, revert.Options{}); st.Pending != nil && st.Pending.Backup != nil {
				t.Errorf("the journal names backups %v: the boot unit's is not journalled", st.Pending.Backup.Seqs)
			}
		})
	}
}

// TestBootUnitRefused: a change journalled refuses the plan before the boot
// unit is read or shown (its recovery is that change's); what the boot unit
// cannot be planned with is refused at plan time; a control's change in
// StageBoot is ErrChange. Nothing runs.
func TestBootUnitRefused(t *testing.T) {
	ctx := context.Background()
	u, err := revert.PlanBootUnit(debian13.Init, testHardn)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, h *testHost, o *Options)
		c     Control
		is    error
		says  string
	}{
		{"a change journalled", func(t *testing.T, h *testHost, o *Options) {
			j := revert.Journal{ID: "20261019T110000Z-5e6f7a8b", Risk: revert.RiskOther, Summary: "Example DNS", Session: applySession}
			if _, err := revert.Arm(ctx, h, j, debian13.Init, o.Revert); err != nil {
				t.Fatal(err)
			}
		}, portControl(), revert.ErrPending, "did not finish"},
		{"hardn at a path the unit cannot name", func(t *testing.T, h *testHost, o *Options) {
			o.Revert.Hardn = "/usr/local/bin/hardn%i"
			h.AddFile(o.Revert.Hardn, []byte("ELF"), 0o755, 0, 0)
		}, portControl(), revert.ErrExecutable, "engine: the boot unit"},
		{"a control's change in StageBoot", nil, &stub{id: "example.boot", title: "Boot", changes: []Change{
			command("example.boot/unit", StageBoot, RiskPlain, argv("example-boot install"), nil)}}, ErrChange, "stage boot is the engine's own"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newBareHost(t)
			o := testOptions(h, nil, nil, debian13)
			if tc.setup != nil {
				tc.setup(t, h, &o)
			}
			from := len(h.Calls())
			e := New(h, o)
			p, err := e.Plan(ctx, []Control{tc.c}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !errors.Is(p.Err(), tc.is) || !strings.Contains(p.Err().Error(), tc.says) {
				t.Fatalf("refusal %v, want %v saying %q", p.Err(), tc.is, tc.says)
			}
			if slices.Contains(changeIDs(p), BootChangeID) {
				t.Error("the boot unit is shown in a refused plan")
			}
			if tc.is == revert.ErrPending {
				for _, c := range h.Calls()[from:] {
					if c.Path == u.Path || c.Path == u.Link {
						t.Errorf("the boot unit was read: %v", c)
					}
				}
			}
			if _, err := e.Apply(ctx, p); !errors.Is(err, ErrRefused) {
				t.Errorf("Apply: %v", err)
			}
			if got := h.changedOutsideSince(runRoot, from); len(got) > 0 {
				t.Errorf("changed: %q", got)
			}
		})
	}
}

// TestBootUnitFails: an install that fails refuses the change: nothing is
// armed, the run lock is released, and the progress says so; a failure
// after Arm rolls the change back and leaves the boot unit installed, with
// no rolled-back event for it.
func TestBootUnitFails(t *testing.T) {
	check := "systemd-analyze verify --recursive-errors=no /run/hardn/<id>/hardn-boot.service"
	for _, tc := range []struct {
		name   string
		fail   string // a command refused, <id> the plan's
		is     error
		events string
	}{
		{"the check refuses the copy", check, revert.ErrBootUnit, "0 " + BootChangeID + " running\n0 " + BootChangeID + " failed at boot unit\n"},
		{"the enable fails", "systemctl --no-reload enable hardn-boot.service", nil, "0 " + BootChangeID + " running\n0 " + BootChangeID + " failed at boot unit\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newBareHost(t)
			var evs []Event
			o := testOptions(h, nil, nil, debian13)
			o.Progress = func(ev Event) { evs = append(evs, ev) }
			e := New(h, o)
			p, err := e.Plan(ctx, []Control{portControl()}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			h.failOn(strings.ReplaceAll(tc.fail, "<id>", p.ID))
			res, err := e.Apply(ctx, p)
			if err == nil || !strings.Contains(err.Error(), "not applied: "+BootChangeID) || tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("Apply = %v, want not applied at the boot unit (%v)", err, tc.is)
			}
			if res.ID != "" || res.Failed != nil || len(res.Applied) > 0 {
				t.Errorf("result %+v", res)
			}
			if strings.Contains(h.runs(p.ID), "systemd-run") || strings.Contains(h.runs(p.ID), "ufw") {
				t.Errorf("armed or applied after the boot unit failed:\n%s", h.runs(p.ID))
			}
			if st, err := revert.ReadState(ctx, h, revert.Options{}); err != nil || st.Busy() {
				t.Errorf("journalled: %+v, %v", st, err)
			}
			if got := renderEvents(evs); got != tc.events {
				t.Errorf("events:\n%s\nwant:\n%s", got, tc.events)
			}
			assertReleased(t, h, p.ID)
		})
	}
	t.Run("a change that fails after Arm", func(t *testing.T) {
		ctx := context.Background()
		h := newBareHost(t)
		var evs []Event
		o := testOptions(h, &holds{}, nil, debian13)
		o.Progress = func(ev Event) { evs = append(evs, ev) }
		e := New(h, o)
		h.failOn("example-check")
		c := &stub{id: "example.port", title: "Port", changes: []Change{
			command("example.port/allow", StageFirewall, RiskLockout, argv(allow2230), argv(delete2230)),
			command("example.port/check", StageVerify, RiskLockout, argv("example-check"), nil)}}
		p, err := e.Plan(ctx, []Control{c}, nil)
		if err != nil || p.Err() != nil {
			t.Fatal(err, p.Err())
		}
		if _, err := e.Apply(ctx, p); !errors.Is(err, ErrRolledBack) {
			t.Fatalf("Apply = %v, want rolled back", err)
		}
		want := "0 " + BootChangeID + " running\n0 " + BootChangeID + " done: written, enabled\n" +
			"1 example.port/allow running\n1 example.port/allow done\n2 example.port/check running\n2 example.port/check failed at apply\n" +
			"1 example.port/allow rolled-back\n"
		if got := renderEvents(evs); got != want {
			t.Errorf("events:\n%s\nwant:\n%s", got, want)
		}
		u, _ := revert.PlanBootUnit(debian13.Init, testHardn)
		if st, err := revert.ReadBootUnit(ctx, h, u); err != nil || !st.Installed() {
			t.Errorf("the boot unit after the rollback: installed %v, %v; want it left installed", st.Installed(), err)
		}
	})
}

// TestBootUnitPlanChanged: what was shown is what runs: the boot unit
// installed, or its link removed, between the plan and the apply makes the
// plan made again under the run lock differ (ErrPlanChanged), and nothing
// runs.
func TestBootUnitPlanChanged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		host    func(t *testing.T) *testHost
		between func(t *testing.T, h *testHost)
	}{
		{"installed meanwhile", newBareHost, func(t *testing.T, h *testHost) { installBootUnit(t, h, debian13.Init) }},
		{"disabled meanwhile", newHost, func(t *testing.T, h *testHost) {
			u, _ := revert.PlanBootUnit(debian13.Init, testHardn)
			if err := h.System.Remove(context.Background(), u.Link); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := tc.host(t)
			e := New(h, testOptions(h, nil, nil, debian13))
			p, err := e.Plan(ctx, []Control{portControl()}, nil)
			if err != nil || p.Err() != nil {
				t.Fatal(err, p.Err())
			}
			tc.between(t, h)
			if _, err := e.Apply(ctx, p); !errors.Is(err, ErrPlanChanged) {
				t.Fatalf("Apply = %v, want ErrPlanChanged", err)
			}
			if runs := h.runs(p.ID); runs != "" {
				t.Errorf("ran:\n%s", runs)
			}
		})
	}
}
