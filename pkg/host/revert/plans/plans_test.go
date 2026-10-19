package plans_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// Nothing here runs a command or touches the machine: every host is
// hostfake's, its commands answered as each unit's own tests answer them.
// Synthetic values only: alice and bob, web-01.example.net, 192.0.2.0/24.

var update = flag.Bool("update", false, "rewrite testdata/<unit>.json from the plans the step functions make")

const (
	testID = "20261018T120000Z-1a2b3c4d"
	runDir = revert.DefaultRunDir + "/" + testID
)

var (
	ctx     = context.Background()
	testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	pending = path.Join(revert.DefaultDir, revert.PendingName)
)

// newHost is a fake host with the state directory's parent and the
// process files a rollback reads, all root's; handle answers commands.
func newHost(handle func(cmd string) (hostfake.Response, bool)) *hostfake.System {
	f := hostfake.New(host.Options{})
	f.AddDir("/var/lib", 0o755, 0, 0)
	f.AddDir("/var/backups", 0o755, 0, 0)
	f.AddDir(runDir, 0o700, 0, 0)
	f.AddFile("/proc/self/stat", []byte("5000 (hardn) S 1 5000 5000 34819 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 500000 0 0\n"), 0o444, 0, 0)
	f.AddFile("/proc/sys/kernel/random/boot_id", []byte("8e2a3c4d-1b2c-4d3e-8f40-0123456789ab\n"), 0o444, 0, 0)
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := handle(strings.Join(argv, " ")); ok {
			return r
		}
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	return f
}

func options() revert.Options {
	return revert.Options{Now: func() time.Time { return testNow }, Plans: plans.Registry()}
}

// journal arms a change of no lockout risk on f and records step as its
// inverse step, as the engine does before the unit's Apply (revert.md §7).
func journal(t *testing.T, f *hostfake.System, step revert.Step) {
	t.Helper()
	j, err := revert.Arm(ctx, f, revert.Journal{ID: testID, Risk: revert.RiskOther, Summary: "a change of " + step.Unit}, detect.Init{}, options())
	if err != nil {
		t.Fatalf("Arm: %v", err)
	}
	j.Undo = []revert.Step{step}
	if err := revert.Record(ctx, f, j, options()); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

// editPlan rewrites the journalled step's plan as a hand edit would: the
// plan decoded into P, edited, encoded again.
func editPlan[P any](t *testing.T, f *hostfake.System, edit func(*P)) {
	t.Helper()
	rewrite(t, f, func(raw json.RawMessage) json.RawMessage {
		var p P
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		edit(&p)
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return data
	})
}

// addField rewrites the journalled plan with a field its type does not
// have.
func addField(t *testing.T, f *hostfake.System) {
	t.Helper()
	rewrite(t, f, func(raw json.RawMessage) json.RawMessage {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		m["PreRollback"] = []string{"sh", "-c", "id"}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return data
	})
}

// rewrite replaces pending.json's first plan with edit's, keeping the file
// root's and 0600, as the journal's checks require.
func rewrite(t *testing.T, f *hostfake.System, edit func(json.RawMessage) json.RawMessage) {
	t.Helper()
	e, ok := f.Entry(pending)
	if !ok {
		t.Fatal("pending.json missing")
	}
	var j revert.Journal
	if err := json.Unmarshal(e.Data, &j); err != nil {
		t.Fatal(err)
	}
	j.Undo[0].Plan = edit(j.Undo[0].Plan)
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	f.AddFile(pending, append(data, '\n'), 0o600, 0, 0)
}

// changed lists the writes, removals and commands from call from on that
// are not the journal's own.
func changed(f *hostfake.System, from int) []string {
	var out []string
	for _, c := range f.Calls()[from:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "rename", "chmod", "chown", "mkdir":
			if !strings.HasPrefix(c.Path, revert.DefaultDir+"/") {
				out = append(out, c.String())
			}
		case "run":
			out = append(out, c.String())
		}
	}
	return out
}

// unit is one registered kind's scenario: a host with the unit's change
// planned, applied after its step is journalled, and what to check.
type unit struct {
	name string
	// setup returns the host, the step, an apply that makes the change, and
	// restored, which fails unless the host is back as it was.
	setup func(t *testing.T) (f *hostfake.System, step revert.Step, apply func(), restored func(t *testing.T))
	// tampers are edits of the journalled plan that its rollback refuses.
	tampers map[string]func(t *testing.T, f *hostfake.System)
}

// TestRoundTrip: for each registered unit, the plan journalled by its
// step function survives Record (pending.json, JSON), and Rollback, through
// Registry, undoes the applied change from the journal alone. Then each
// tampered journal entry is refused before the unit's rollback reads or
// writes anything, and the rollback stays incomplete.
func TestRoundTrip(t *testing.T) {
	for _, u := range units() {
		t.Run(u.name, func(t *testing.T) {
			f, step, apply, restored := u.setup(t)
			journal(t, f, step)
			apply()
			res, err := revert.Rollback(ctx, f, testID, options())
			if err != nil || len(res.Undone) != 1 || res.Undone[0].Step.Unit != step.Unit {
				t.Fatalf("Rollback = %+v, %v", res, err)
			}
			restored(t)
			if _, ok := f.Entry(path.Join(revert.DefaultDir, revert.RollbackName)); ok {
				t.Error("rollback.json left behind")
			}
			for name, tamper := range u.tampers {
				t.Run(name, func(t *testing.T) {
					f, step, apply, _ := u.setup(t)
					journal(t, f, step)
					apply()
					tamper(t, f)
					before := len(f.Calls())
					_, err := revert.Rollback(ctx, f, testID, options())
					if !errors.Is(err, revert.ErrIncomplete) || !errors.Is(err, plans.ErrPlan) {
						t.Fatalf("Rollback = %v, want ErrIncomplete and ErrPlan", err)
					}
					if ch := changed(f, before); len(ch) > 0 {
						t.Errorf("the refused rollback changed the host: %q", ch)
					}
				})
			}
		})
	}
}

func units() []unit {
	unknownField := func(t *testing.T, f *hostfake.System) { addField(t, f) }
	return []unit{
		{"sudoers", sudoersSetup, map[string]func(*testing.T, *hostfake.System){
			"the main sudoers file named": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *sudoers.Plan) {
					p.Changes[0].Path, p.Changes[0].Old.Path = sudoers.MainConfig, sudoers.MainConfig
				})
			},
			"another user's file in its place": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *sudoers.Plan) { p.Changes[0].Old.Path = sudoers.DropInDir + "/hardn-grant-bob" })
			},
			// The old state is written back with its mode and owner.
			"its old state world-writable": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *sudoers.Plan) { p.Changes[0].Old.Mode = 0o666 })
			},
			"its old state alice's": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *sudoers.Plan) { p.Changes[0].Old.UID = 1000 })
			},
			"an unknown field": unknownField,
		}},
		{"dns", dnsSetup, map[string]func(*testing.T, *hostfake.System){
			"other bytes to write back": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *dns.Plan) { p.From.ResolvConf.Data = []byte("nameserver 192.0.2.99\n") })
			},
			"other nameservers": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *dns.Plan) { p.Want.Nameservers = []string{"192.0.2.99"} })
			},
			"an unknown field": unknownField,
		}},
		{"alpine-updates", alpineSetup, map[string]func(*testing.T, *hostfake.System){
			"a script of its own to write back": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *alpine.Plan) { p.From.LegacyFile.Data = []byte("#!/bin/sh\nid\n") })
			},
			"the legacy file world-writable": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *alpine.Plan) { p.From.LegacyFile.Mode = 0o777 })
			},
			"a step it did not plan": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *alpine.Plan) {
					p.Changes = append([]alpine.Change{{Step: alpine.StepInstall, Argv: []string{"apk", "add", alpine.Package}, Undo: []string{"apk", "del", alpine.Package}}}, p.Changes...)
				})
			},
			"an unknown field": unknownField,
		}},
		{"pve-keys", pveSetup, map[string]func(*testing.T, *hostfake.System){
			// UndoKeys removes the keys Added names: root@web-01's own here.
			"the node's key in Added": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *pve.KeysPlan) { p.Added = []string{keyWeb01} })
			},
			"Old edited, Before not": func(t *testing.T, f *hostfake.System) {
				editPlan(t, f, func(p *pve.KeysPlan) { p.Old = []byte(keyWeb01 + "\n" + keyBob + "\n") })
			},
			"an unknown field": unknownField,
		}},
	}
}

// TestWireFormat pins each unit's journalled plan, as its step function
// encodes it, in testdata/<unit>.json, and rolls the change back from that
// file, journalled as it is: a journal written by one hardn is rolled back
// by the next, so a change of a plan type's shape (a field renamed or
// dropped, an enum value renumbered, display fields included, since
// decoding refuses unknown fields) fails here before it strands a journal
// already written (its rollback refused with ErrPlan until abandoned). For
// an intended change, rerun with -update and review the diff for what an
// older journal would no longer decode or would decode as something else.
func TestWireFormat(t *testing.T) {
	for _, u := range units() {
		t.Run(u.name, func(t *testing.T) {
			f, step, apply, restored := u.setup(t)
			var got bytes.Buffer
			if err := json.Indent(&got, step.Plan, "", "  "); err != nil {
				t.Fatal(err)
			}
			got.WriteByte('\n')
			name := filepath.Join("testdata", u.name+".json")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, got.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(name)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Errorf("%s: the journalled plan's shape changed:\n--- got\n%s--- want\n%s", name, got.Bytes(), want)
			}
			step.Plan = json.RawMessage(want)
			journal(t, f, step)
			apply()
			if _, err := revert.Rollback(ctx, f, testID, options()); err != nil {
				t.Fatalf("Rollback from %s: %v", name, err)
			}
			restored(t)
		})
	}
}

// TestPVENodeKeyInAdded: a journalled keys plan edited consistently, so
// that it names root@web-01's own key in Added and passes Check (Old,
// Before and New made from a file without it), is refused by UndoKeys
// itself, which never removes a node's key: the rollback stays
// incomplete, and the key stays.
func TestPVENodeKeyInAdded(t *testing.T) {
	f, step, apply, _ := pveSetup(t)
	journal(t, f, step)
	apply()
	editPlan(t, f, func(p *pve.KeysPlan) {
		q, err := pve.PlanKeys(pve.Keys{Target: p.Target, Data: []byte(keyBob + "\n")}, pve.KeyEdit{Add: []string{keyWeb01}})
		if err != nil || q.Check() != nil {
			t.Fatalf("PlanKeys = %+v, %v", q, err)
		}
		*p = q
	})
	before := len(f.Calls())
	_, err := revert.Rollback(ctx, f, testID, options())
	if !errors.Is(err, revert.ErrIncomplete) || !errors.Is(err, pve.ErrClusterKey) {
		t.Fatalf("Rollback = %v, want ErrIncomplete and ErrClusterKey", err)
	}
	if e, _ := f.Entry(pve.ClusterKeys); !bytes.Contains(e.Data, []byte(keyWeb01)) {
		t.Errorf("keys %q: root@web-01's key removed", e.Data)
	}
	// It detected Proxmox VE (dpkg-query) and read the keys; it wrote
	// nothing.
	if ch := slices.DeleteFunc(changed(f, before), func(c string) bool { return strings.HasPrefix(c, "run dpkg-query ") }); len(ch) > 0 {
		t.Errorf("the refused rollback changed the host: %q", ch)
	}
}

// TestRegistry: Registry names the four units, each with a rollback, and
// each step function names its unit.
func TestRegistry(t *testing.T) {
	r := plans.Registry()
	var names []string
	for n, fn := range r {
		if fn == nil {
			t.Errorf("%s: no rollback", n)
		}
		names = append(names, n)
	}
	slices.Sort(names)
	if want := []string{"alpine-updates", "dns", "pve-keys", "sudoers"}; !slices.Equal(names, want) {
		t.Errorf("Registry = %q, want %q", names, want)
	}
	if plans.UnitPVEKeys != pve.KeysUnit {
		t.Errorf("UnitPVEKeys = %q, pve.KeysUnit %q", plans.UnitPVEKeys, pve.KeysUnit)
	}
	for _, u := range units() {
		_, step, _, _ := u.setup(t)
		if step.Op != revert.OpPlan || step.Unit != u.name || r[step.Unit] == nil || step.Why == "" {
			t.Errorf("%s: step %+v", u.name, step)
		}
	}
}

// TestStepRefuses: a plan its rollback would refuse is not made a step.
func TestStepRefuses(t *testing.T) {
	_, step, _, _ := sudoersSetup(t)
	var p sudoers.Plan
	if err := json.Unmarshal(step.Plan, &p); err != nil {
		t.Fatal(err)
	}
	p.Changes[0].Path = sudoers.MainConfig
	if _, err := plans.Sudoers(p, "x"); !errors.Is(err, plans.ErrPlan) || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Sudoers = %v, want ErrPlan and fs.ErrInvalid", err)
	}
	if _, err := plans.PVEKeys(pve.KeysPlan{Added: []string{keyAlice}}, "x"); !errors.Is(err, plans.ErrPlan) || !errors.Is(err, pve.ErrPlanMismatch) {
		t.Errorf("PVEKeys = %v, want ErrPlan and ErrPlanMismatch", err)
	}
	if _, err := plans.DNS(dns.Plan{}, "x"); !errors.Is(err, plans.ErrPlan) {
		t.Errorf("DNS of the zero plan = %v, want ErrPlan", err)
	}
	if _, err := plans.Alpine(alpine.Plan{}, "x"); !errors.Is(err, plans.ErrPlan) {
		t.Errorf("Alpine of the zero plan = %v, want ErrPlan", err)
	}
}

// TestDecode: the registered rollback refuses what is not one plan
// document, before the unit's rollback runs.
func TestDecode(t *testing.T) {
	f, step, _, _ := sudoersSetup(t)
	before := len(f.Calls())
	for name, raw := range map[string]string{
		"two documents":            string(step.Plan) + " {}",
		"a stray brace after it":   string(step.Plan) + "}",
		"a stray bracket after it": string(step.Plan) + " ]]",
		"not an object":            `["sudo"]`,
		"a field of another type":  `{"Tool":"sudo"}`,
	} {
		if err := plans.Registry()[plans.UnitSudoers](ctx, f, json.RawMessage(raw)); !errors.Is(err, plans.ErrPlan) {
			t.Errorf("%s: %v, want ErrPlan", name, err)
		}
	}
	if len(f.Calls()) != before {
		t.Errorf("calls %q", f.Calls()[before:])
	}
}

// sudoers: alice's grant goes from a password to NOPASSWD.
const (
	grantFile = sudoers.DropInDir + "/hardn-grant-alice"
	oldGrant  = sudoers.Header + "alice ALL=(ALL) ALL\n"
)

func sudoersSetup(t *testing.T) (*hostfake.System, revert.Step, func(), func(*testing.T)) {
	f := newHost(func(cmd string) (hostfake.Response, bool) {
		return hostfake.Response{Stdout: "parsed OK\n"}, strings.HasPrefix(cmd, "visudo -c")
	})
	f.AddFile(sudoers.MainConfig, []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
	f.AddDir(sudoers.DropInDir, 0o755, 0, 0)
	f.AddFile(grantFile, []byte(oldGrant), 0o440, 0, 0)
	c := sudoers.Choice{Tool: sudoers.ToolSudo, Flavor: detect.SudoOriginal}
	st, err := sudoers.Read(ctx, f, c, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := sudoers.PlanFor(st, []sudoers.Policy{{User: "alice", Grant: sudoers.GrantNoPassword}})
	if err != nil || len(p.Changes) != 1 {
		t.Fatalf("PlanFor = %+v, %v", p, err)
	}
	step, err := plans.Sudoers(p, "alice's grant")
	if err != nil {
		t.Fatal(err)
	}
	apply := func() {
		if _, err := sudoers.Apply(ctx, f, p, sudoers.Options{RunDir: runDir}); err != nil {
			t.Fatal(err)
		}
	}
	restored := func(t *testing.T) {
		if e, ok := f.Entry(grantFile); !ok || string(e.Data) != oldGrant || e.Mode.Perm() != 0o440 {
			t.Errorf("%s = %q %v, want %q 0440", grantFile, e.Data, e.Mode, oldGrant)
		}
	}
	return f, step, apply, restored
}

// dns: a Debian 13 host whose /etc/resolv.conf is a file, no
// systemd-resolved.
const resolvConf = "search example.net\nnameserver 192.0.2.1\n"

func dnsSetup(t *testing.T) (*hostfake.System, revert.Step, func(), func(*testing.T)) {
	f := newHost(func(cmd string) (hostfake.Response, bool) {
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, cmd == "systemctl is-active "+dns.ResolvedUnit
	})
	f.AddFile(dns.ResolvConf, []byte(resolvConf), 0o644, 0, 0)
	facts := detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}
	st, err := dns.Read(ctx, f, facts)
	if err != nil {
		t.Fatal(err)
	}
	p, err := dns.PlanDNS(st, dns.Config{Nameservers: []string{"192.0.2.53"}})
	if err != nil || len(p.Changes) != 1 {
		t.Fatalf("PlanDNS = %+v, %v", p, err)
	}
	step, err := plans.DNS(p, "the nameservers")
	if err != nil {
		t.Fatal(err)
	}
	apply := func() {
		run, err := backup.Open(f, backup.Settings{Path: backup.DefaultPath, Enabled: true}, testID)
		if err == nil {
			_, err = dns.Apply(ctx, f, p, dns.Options{Backups: run})
		}
		if e, _ := f.Entry(dns.ResolvConf); err != nil || !strings.Contains(string(e.Data), "192.0.2.53") {
			t.Fatalf("Apply = %v; %s %q", err, dns.ResolvConf, e.Data)
		}
	}
	restored := func(t *testing.T) {
		if e, _ := f.Entry(dns.ResolvConf); string(e.Data) != resolvConf || e.Mode.Perm() != 0o644 {
			t.Errorf("%s = %q %v, want %q 0644", dns.ResolvConf, e.Data, e.Mode, resolvConf)
		}
	}
	return f, step, apply, restored
}

// alpine: apk-cron installed and in the runlevel, crond stopped, and
// origin/main's apk-upgrade there: the plan starts crond and removes it.
func alpineSetup(t *testing.T) (*hostfake.System, revert.Step, func(), func(*testing.T)) {
	crond := "stopped"
	f := newHost(func(cmd string) (hostfake.Response, bool) {
		switch cmd {
		case "apk info -e " + alpine.Package:
			return hostfake.Response{Stdout: alpine.Package + "\n"}, true
		case "rc-service crond status":
			if crond == "started" {
				return hostfake.Response{Stdout: " * status: started\n"}, true
			}
			return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}, true
		case "rc-service crond start":
			crond = "started"
			return hostfake.Response{}, true
		case "rc-service crond stop":
			crond = "stopped"
			return hostfake.Response{}, true
		}
		return hostfake.Response{}, false
	})
	f.AddFile(alpine.Script, []byte("#!/bin/sh\nexec apk -U upgrade\n"), 0o755, 0, 0)
	f.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent), 0o755, 0, 0)
	f.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	f.AddFile(alpine.RunlevelLink, nil, 0o755, 0, 0)
	facts := detect.Facts{Platform: detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported},
		Init: detect.Init{Kind: detect.OpenRC, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}
	u, err := alpine.ReadUpdates(ctx, f, facts)
	if err != nil {
		t.Fatal(err)
	}
	p, err := alpine.PlanUpdates(u)
	if err != nil || len(p.Changes) != 2 {
		t.Fatalf("PlanUpdates = %+v, %v", p, err)
	}
	step, err := plans.Alpine(p, "crond, and origin/main's apk-upgrade")
	if err != nil {
		t.Fatal(err)
	}
	apply := func() {
		run, err := backup.Open(f, backup.Settings{Path: backup.DefaultPath, Enabled: true}, testID)
		if err == nil {
			_, err = alpine.Apply(ctx, f, p, alpine.Options{Backups: run})
		}
		if _, ok := f.Entry(alpine.LegacyScript); err != nil || ok || crond != "started" {
			t.Fatalf("Apply = %v; apk-upgrade left %v, crond %s", err, ok, crond)
		}
	}
	restored := func(t *testing.T) {
		if e, ok := f.Entry(alpine.LegacyScript); !ok || string(e.Data) != alpine.LegacyContent || e.Mode.Perm() != 0o755 || crond != "stopped" {
			t.Errorf("apk-upgrade back %v (%q %v), crond %s; want hardn's file 0755 and crond stopped", ok, e.Data, e.Mode, crond)
		}
	}
	return f, step, apply, restored
}

// pve: node web-01 of a Proxmox VE 9 cluster, root's keys through the
// link; alice's key is added. Synthetic keys (made-up bytes in the SSH
// public key format), as pkg/host/pve's tests have them.
const (
	keyWeb01 = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAQQCRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGR root@web-01"
	keyAlice = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGh alice@web-01.example.net"
	keyBob   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCw bob@web-01.example.net"
	wwwData  = 33 // the group pmxcfs reports under /etc/pve
)

func pveSetup(t *testing.T) (*hostfake.System, revert.Step, func(), func(*testing.T)) {
	f := newHost(func(cmd string) (hostfake.Response, bool) {
		return hostfake.Response{Stdout: "install ok installed\t9.2.22\n"}, strings.HasPrefix(cmd, "dpkg-query --show ")
	})
	f.AddFile("/usr/bin/pveversion", []byte("#!/usr/bin/perl\n"), 0o755, 0, 0)
	f.AddFile("/proc/self/mounts", []byte("/dev/fuse /etc/pve fuse rw,nosuid,nodev,relatime,user_id=0,group_id=0,default_permissions,allow_other 0 0\n"), 0o444, 0, 0)
	f.AddDir("/root", 0o700, 0, 0)
	f.AddDir("/root/.ssh", 0o700, 0, 0)
	f.AddDir("/etc/pve", 0o755, 0, wwwData)
	f.AddDir("/etc/pve/priv", 0o700, 0, wwwData)
	f.AddDir(pve.NodesDir+"/web-01", 0o755, 0, wwwData)
	f.AddFile(pve.ClusterKeys, []byte(keyWeb01+"\n"), 0o600, 0, wwwData)
	f.AddSymlink(pve.RootKeys, pve.ClusterKeys)
	f.AddFile(pve.NodeKey, []byte(keyWeb01+"\n"), 0o644, 0, 0)
	up := detect.Proxmox{Installed: detect.Yes, Version: "9.2.22", ClusterFS: detect.Yes, ClusterFSType: "fuse", EtcPVE: detect.Yes}
	k, err := pve.ReadKeys(ctx, f, up)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyAlice}})
	if err != nil {
		t.Fatal(err)
	}
	step, err := plans.PVEKeys(p, "alice's key for root")
	if err != nil {
		t.Fatal(err)
	}
	apply := func() {
		run, err := backup.Open(f, backup.Settings{Path: backup.DefaultPath, Enabled: true}, testID)
		if err == nil {
			_, err = pve.ApplyKeys(ctx, f, up, run, p)
		}
		if e, _ := f.Entry(pve.ClusterKeys); err != nil || !bytes.Contains(e.Data, []byte(keyAlice)) {
			t.Fatalf("ApplyKeys = %v; keys %q", err, e.Data)
		}
	}
	restored := func(t *testing.T) {
		e, _ := f.Entry(pve.ClusterKeys)
		l, ok := f.Entry(pve.RootKeys)
		if string(e.Data) != keyWeb01+"\n" || !ok || l.Mode&fs.ModeSymlink == 0 {
			t.Errorf("keys %q, %s a link %v; want root@web-01's alone, through the link", e.Data, pve.RootKeys, ok && l.Mode&fs.ModeSymlink != 0)
		}
	}
	return f, step, apply, restored
}
