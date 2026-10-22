package dns_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/steps"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// applyOn plans ctl with cfg on h, applies the plan, and returns the run
// as the output contract takes it, with the progress events.
func applyOn(t *testing.T, h *fakeHost, ctl dns.Nameservers, cfg *config.Config) (output.Run, []engine.Event) {
	t.Helper()
	var evs []engine.Event
	e := engineOn(h, &evs)
	p, err := e.Plan(context.Background(), []engine.Control{ctl}, cfg)
	if err != nil || p.Err() != nil {
		t.Fatalf("Plan: %v, %v", err, p.Err())
	}
	res, err := e.Apply(context.Background(), p)
	r := run(h, output.ModeApply)
	r.Plan, r.Result, r.Err = p, &res, err
	return r, evs
}

// events are the events in a line each: index, state, and a failure's
// code.
func events(evs []engine.Event) []string {
	var out []string
	for _, ev := range evs {
		s := ev.Change + " " + ev.State.String()
		if ev.Err != nil {
			s += " " + output.CodeOf(ev.Err)
		}
		out = append(out, s)
	}
	return out
}

// stableID stands for a plan's id in the goldens: the id's last part is
// random (revert.NewID).
const stableID = "20261022T120000Z-1a2b3c4d"

// stable is r with stableID for its change's id, wherever the document
// takes it from (the plan, the result, the apply's error), so that the
// goldens do not change from one run to the next, as pkg/output's own
// real-engine goldens do it.
func stable(r output.Run) output.Run {
	if p := r.Plan; p != nil {
		r.Plan = &engine.Plan{ID: stableID, Facts: p.Facts, Items: p.Items, Changes: p.Changes, Risk: p.Risk, Findings: p.Findings, Refused: p.Refused}
	}
	if r.Result == nil {
		return r
	}
	res := *r.Result
	if res.ID != "" {
		res.ID = stableID
	}
	if f := res.Failed; f != nil {
		fc := *f
		fc.ID = stableID
		if r.Err == error(f) {
			r.Err = &fc
		}
		res.Failed = &fc
	}
	r.Result = &res
	return r
}

// goldens pins r through the human renderer, and as the JSON document
// when json is set.
func goldens(t *testing.T, name string, r output.Run, json bool) {
	t.Helper()
	r = stable(r)
	uitest.Golden(t, "human/"+name, uitest.Core(), func(env term.Env) string { return output.Human(env, r, now) })
	if json {
		var b bytes.Buffer
		if err := output.WriteJSON(&b, output.Build(r)); err != nil {
			t.Fatal(err)
		}
		golden(t, "json/"+name+".json", b.Bytes())
	}
}

// TestApply applies each platform's plan, then plans again: nothing is
// left to change (idempotency), and the row reads Configured, with
// hardn.yml saved when the list changed and kept otherwise.
func TestApply(t *testing.T) {
	for _, tt := range []struct {
		name, host string
		list       []string
		stop       bool
		golden     string
		files      map[string]string // a path's content after, "" for absent
	}{
		{name: "Ubuntu 26.04", host: ubuntu2604, list: configured, golden: "applied-ubuntu2604",
			files: map[string]string{hostdns.DropIn: "[Resolve]\nDNS=\nDNS=192.0.2.54 2001:db8::54\n"}},
		{name: "Ubuntu 24.04", host: ubuntu2404, list: configured, files: map[string]string{hostdns.DropIn: "DNS=192.0.2.54 2001:db8::54\n"}},
		{name: "Debian 12 cloud", host: debian12, list: configured, files: map[string]string{hostdns.DropIn: "DNS=192.0.2.54 2001:db8::54\n"}},
		{name: "Debian 13, a preset", host: debian13, list: cloudflare, golden: "applied-debian13-cloudflare",
			files: map[string]string{hostdns.ResolvConf: "search example.net\nnameserver 1.1.1.1\nnameserver 1.0.0.1\n", hardnYML: "nameservers:\n    - 1.1.1.1\n    - 1.0.0.1\n"}},
		{name: "Alpine 3.24, udhcpc stopped", host: alpine324, list: configured, stop: true,
			files: map[string]string{hostdns.UDHCPCConf: "RESOLV_CONF=\"no\"\n", hostdns.ResolvConf: "nameserver 192.0.2.54\nnameserver 2001:db8::54\n"}},
		{name: "Alpine 3.24, udhcpc kept", host: alpine324, list: configured,
			files: map[string]string{hostdns.UDHCPCConf: "#RESOLV_CONF=\"no\"\n", hostdns.ResolvConf: "nameserver 192.0.2.54\n"}},
		{name: "Proxmox VE 9", host: pve9, list: configured, files: map[string]string{hostdns.ResolvConf: "search example.net\nnameserver 192.0.2.54\n"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, loaded := newHost(t, tt.host, configured)
			cfg := with(loaded, tt.list)
			ctl := dns.Nameservers{Config: loaded, StopUDHCPC: tt.stop}
			r, _ := applyOn(t, h, ctl, cfg)
			if r.Err != nil || len(r.Result.Applied) != len(r.Plan.Changes) {
				t.Fatalf("Apply: %v, applied %+v", r.Err, r.Result.Applied)
			}
			for path, want := range tt.files {
				if got := content(h, path); !strings.Contains(got, want) {
					t.Errorf("%s:\n%s\nwant it to hold\n%s", path, got, want)
				}
			}
			if !slices.Equal(tt.list, configured) == strings.Contains(content(h, hardnYML), "# hardn's configuration") {
				t.Errorf("hardn.yml saved %v, the list changed %v:\n%s", !strings.Contains(content(h, hardnYML), "#"), !slices.Equal(tt.list, configured), content(h, hardnYML))
			}
			again := planOn(t, h, ctl, cfg)
			if !again.Empty() {
				t.Errorf("planned again: %q", summary(again))
			}
			if tt.golden != "" {
				goldens(t, tt.golden, r, true)
			}
			saved, err := config.Read(context.Background(), h, hardnYML)
			if err != nil {
				t.Fatal(err)
			}
			if rep := check(t, h, saved); rep.Status.State != engine.Configured || rep.Status.Detail != strings.Join(tt.list, ", ") {
				t.Errorf("after: %v %q", rep.Status.State, rep.Status.Detail)
			}
		})
	}
}

// TestPlanGoldens pins the plans a reviewer reads, as the dry run shows
// them (ModePlan): Ubuntu's drop-in created against /dev/null with its
// reload and the per-link finding, Debian's preset with the save and its
// lost comments, Alpine's udhcpc stopped in the same change, Proxmox VE's
// file and the node finding; and a configured host, which needs nothing.
func TestPlanGoldens(t *testing.T) {
	for _, tt := range []struct {
		name, host string
		list       []string
		stop       bool
		json       bool
	}{
		{"plan-ubuntu2604", ubuntu2604, configured, false, true},
		{"plan-debian13-cloudflare", debian13, cloudflare, false, true},
		{"plan-alpine324-udhcpc", alpine324, configured, true, false},
		{"plan-pve9", pve9, configured, false, false},
		{"plan-configured", configuredHost, configured, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, cfg := newHost(t, tt.host, configured)
			r := run(h, output.ModePlan)
			r.Plan = planOn(t, h, dns.Nameservers{Config: cfg, StopUDHCPC: tt.stop}, with(cfg, tt.list))
			goldens(t, tt.name, r, tt.json)
			if w := writes(h); len(w) > 0 {
				t.Errorf("the dry run wrote: %v", w)
			}
		})
	}
}

// TestRefusalGoldens pins two refusals as the report shows them: a
// manager hardn only reports (P-37 (a)) and an empty list.
func TestRefusalGoldens(t *testing.T) {
	for _, tt := range []struct {
		name, host string
		list       []string
		json       bool
	}{
		{"refused-networkmanager", networkManager, configured, true},
		{"refused-empty", debian13, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, cfg := newHost(t, tt.host, tt.list)
			p, err := engineOn(h, nil).Plan(context.Background(), dns.Controls(cfg), cfg)
			if err != nil {
				t.Fatal(err)
			}
			r := run(h, output.ModePlan)
			r.Plan, r.Err = p, p.Err()
			goldens(t, tt.name, r, tt.json)
		})
	}
}

// TestFailedReload: systemd-resolved reloads with no effect. dns.Apply
// puts the drop-in back and resolved on its servers; the engine reports
// the change failed, then rolled back (D3), and the report says it in a
// few words with the next step; through the flow's adapter the failure is
// gallery-dns.md's failure-proposed sequence's.
func TestFailedReload(t *testing.T) {
	h, cfg := newHost(t, ubuntu2604, configured)
	h.inert = true
	r, evs := applyOn(t, h, dns.Nameservers{Config: cfg}, cfg)
	var ae *engine.ApplyError
	if !errors.As(r.Err, &ae) || !errors.Is(r.Err, engine.ErrRolledBack) || !ae.Undone || !errors.Is(r.Err, hostdns.ErrNotEffective) {
		t.Fatalf("Apply: %v", r.Err)
	}
	var f *steps.Failure
	if !errors.As(r.Err, &f) || f.Text != "Reloading systemd-resolved had no effect" || f.Next != "Run resolvectl dns to see the servers in use." {
		t.Errorf("failure %+v", f)
	}
	want := []string{dns.ChangeSet + " running", dns.ChangeSet + " failed DNS_NOT_EFFECTIVE", dns.ChangeSet + " rolled-back"}
	if got := events(evs); !slices.Equal(got, want) {
		t.Errorf("events %q, want %q", got, want)
	}
	if got := content(h, hostdns.DropIn); got != "<absent>" || h.global != nil {
		t.Errorf("drop-in %q, resolved's servers %q: not undone", got, h.global)
	}
	if d := output.Build(r); d.Result.Outcome != output.OutcomeRolledBack || d.Result.Rollback == nil || !d.Result.Rollback.ChangeUndone {
		t.Errorf("document: %+v", d.Result)
	}
	goldens(t, "failed-reload", r, true)

	// Through the flow's adapter, as the screen will apply it.
	h, cfg = newHost(t, ubuntu2604, configured)
	h.inert = true
	a := &output.FlowEngine{Controls: dns.Controls(cfg), Config: cfg, LogPath: output.LegacyLogPath}
	a.Engine = engineWith(h, a.Progress)
	if _, err := a.Plan(context.Background()); err != nil {
		t.Fatal(err)
	}
	var steps []string
	err := a.Apply(context.Background(), func(i int, s component.StepState, detail string) {
		steps = append(steps, strings.TrimSpace(fmt.Sprintf("%d %v %s", i, s, detail)))
	})
	var ff *flow.Failure
	if !errors.As(err, &ff) || ff.Error() != "Reloading systemd-resolved had no effect" || ff.Next != "Run resolvectl dns to see the servers in use." ||
		ff.LogPath != output.LegacyLogPath || ff.Command != "" {
		t.Errorf("the flow's failure %+v", ff)
	}
	if want := []string{"0 Running", "0 Failed DNS_NOT_EFFECTIVE", "0 Rolled back"}; !slices.Equal(steps, want) {
		t.Errorf("the flow's steps %q, want %q", steps, want)
	}
}

// TestFailedSave: the save of hardn.yml fails after the host's change
// applied, and the engine rolls the change back too (decision 1 (iii)):
// /etc/resolv.conf and hardn.yml are as they were.
func TestFailedSave(t *testing.T) {
	h, cfg := newHost(t, debian13, configured)
	before := map[string]string{hostdns.ResolvConf: content(h, hostdns.ResolvConf), hardnYML: content(h, hardnYML)}
	h.failWrite(hardnYML, fs.ErrPermission)
	r, evs := applyOn(t, h, dns.Nameservers{Config: cfg}, with(cfg, cloudflare))
	var ae *engine.ApplyError
	if !errors.As(r.Err, &ae) || !errors.Is(r.Err, engine.ErrRolledBack) || ae.Change != dns.ChangeSave {
		t.Fatalf("Apply: %v", r.Err)
	}
	want := []string{dns.ChangeSet + " running", dns.ChangeSet + " done", dns.ChangeSave + " running", dns.ChangeSave + " failed PERMISSION_DENIED",
		dns.ChangeSet + " rolled-back"}
	if got := events(evs); !slices.Equal(got, want) {
		t.Errorf("events %q, want %q", got, want)
	}
	for path, was := range before {
		if got := content(h, path); got != was {
			t.Errorf("%s after the rollback:\n%s\nwant\n%s", path, got, was)
		}
	}
	goldens(t, "failed-save", r, true)
}

// TestDeclined: n at the confirmation changes and saves nothing; the plan
// alone has written nothing on the host.
func TestDeclined(t *testing.T) {
	h, cfg := newHost(t, debian13, configured)
	r := run(h, output.ModeApply)
	r.Plan, r.Declined = planOn(t, h, dns.Nameservers{Config: cfg}, with(cfg, cloudflare)), true
	if w := writes(h); len(w) > 0 {
		t.Errorf("written before the answer: %v", w)
	}
	if d := output.Build(r); d.Result.Outcome != output.OutcomeDeclined || len(d.Changes) != 2 || d.Changes[1].Status != output.StatusPlanned {
		t.Errorf("declined: %+v", d.Result)
	}
}
