package environment_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// stableID stands for a plan's id in the goldens: the id's last part is
// random (revert.NewID).
const stableID = "20261029T120000Z-1a2b3c4d"

// stable is r with stableID for its change's id wherever the document and
// the report take it from: the plan, each change's commands (the boot
// unit's copy is checked in the run directory, named by the id), the
// result and its backup run.
func stable(r output.Run) output.Run {
	p := r.Plan
	if p == nil {
		return r
	}
	id := p.ID
	cs := make([]engine.Change, len(p.Changes))
	for i, c := range p.Changes {
		c.Commands = append([]string(nil), c.Commands...)
		for j := range c.Commands {
			c.Commands[j] = strings.ReplaceAll(c.Commands[j], id, stableID)
		}
		cs[i] = c
	}
	r.Plan = &engine.Plan{ID: stableID, Facts: p.Facts, Items: p.Items, Changes: cs, Risk: p.Risk, Findings: p.Findings,
		Refused: p.Refused, Backups: p.Backups}
	if r.Result == nil {
		return r
	}
	res := *r.Result
	if res.ID != "" {
		res.ID = stableID
	}
	if m := res.Backup; m != nil {
		mc := *m
		mc.RunID, mc.Dir = stableID, strings.TrimSuffix(m.Dir, m.RunID)+stableID
		res.Backup = &mc
	}
	r.Result = &res
	return r
}

// run is a run of the output contract on facts f.
func run(mode output.Mode, f detect.Facts) output.Run {
	return output.Run{Mode: mode, HardnVersion: "0.4.0", Hostname: hostname, Facts: &f}
}

// goldens pins r through the human renderer on the core set, and as the
// JSON document when json is set.
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

// planRun plans the control on h, with hardn's environment env, keeping
// or stopping, through the engine as cmd/hardn builds it, and returns the
// run as the output contract takes it: the plan, and its refusal.
func planRun(t *testing.T, h *testHost, env map[string]string, stop bool) output.Run {
	t.Helper()
	_, p := planOn(t, h, env, stop)
	r := run(output.ModePlan, h.facts)
	r.Plan, r.Err = p, p.Err()
	return r
}

// applyRun plans and applies the control on h as planRun plans it, and
// returns the run.
func applyRun(t *testing.T, h *testHost, env map[string]string, stop bool) output.Run {
	t.Helper()
	e, p := planOn(t, h, env, stop)
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	res, err := e.Apply(context.Background(), p)
	r := run(output.ModeApply, h.facts)
	r.Plan, r.Result, r.Err = p, &res, err
	return r
}

// TestGoldens pins what a reviewer reads, as pkg/output renders it from
// the real engine on hostfake: the control's status on each host the
// gallery draws; keeping HARDN_CONFIG on Debian 13, planned and applied;
// stopping, which says what it kept; keeping it where origin/main's file
// is, with the legacy finding; stopping on the cloud image's doas, a
// lockout-risk plan with hardn's boot unit and the later file's finding;
// and each refusal before anything changes.
func TestGoldens(t *testing.T) {
	ctx := context.Background()
	t.Run("check", func(t *testing.T) {
		var reports []engine.Report
		for _, tt := range []struct {
			host func(t *testing.T) *testHost
			env  map[string]string
		}{
			{func(t *testing.T) *testHost { return newHost(t, debian13) }, sudoAlice},
			{func(t *testing.T) *testHost {
				return newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG"))
			}, sudoKept},
			{func(t *testing.T) *testHost { return legacyHost(t, legacyLines) }, sudoKept},
			{func(t *testing.T) *testHost { return newHost(t, alpine324) }, doasAlice},
			{func(t *testing.T) *testHost {
				return newHost(t, alpineCloud).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG"))
			}, doasAlice},
			{func(t *testing.T) *testHost { return newHost(t, pve9) }, rootLogin},
			{func(t *testing.T) *testHost { return newHost(t, debian13) }, rootLogin},
		} {
			h := tt.host(t)
			reps, err := engineOn(h, nil).Check(ctx, environment.Controls(lookup(tt.env)))
			if err != nil {
				t.Fatal(err)
			}
			reports = append(reports, reps...)
		}
		r := run(output.ModeCheck, newHost(t, debian13).facts)
		r.Reports = reports
		goldens(t, "check", r, true)
	})
	t.Run("plan-keep", func(t *testing.T) {
		goldens(t, "plan-keep", planRun(t, newHost(t, debian13), sudoAlice, false), true)
	})
	t.Run("applied-keep", func(t *testing.T) {
		goldens(t, "applied-keep", applyRun(t, newHost(t, debian13), sudoAlice, false), false)
	})
	t.Run("applied-stop", func(t *testing.T) {
		h := newHost(t, debian13).with(t, grant(sudoers.GrantNone, "HARDN_CONFIG"))
		goldens(t, "applied-stop", applyRun(t, h, sudoKept, true), true)
	})
	t.Run("plan-legacy", func(t *testing.T) {
		goldens(t, "plan-legacy", planRun(t, legacyHost(t, legacyLines), sudoKept, false), false)
	})
	t.Run("plan-doas-stop", func(t *testing.T) {
		h := newHost(t, alpineCloud).with(t, grant(sudoers.GrantPassword, "HARDN_CONFIG"))
		goldens(t, "plan-doas-stop", planRun(t, h, doasAlice, true), true)
	})
	t.Run("refused-no-tool", func(t *testing.T) {
		goldens(t, "refused-no-tool", planRun(t, newHost(t, pve9), rootLogin, false), false)
	})
	t.Run("refused-no-user", func(t *testing.T) {
		goldens(t, "refused-no-user", planRun(t, newHost(t, debian13), rootLogin, false), true)
	})
	t.Run("refused-edited", func(t *testing.T) {
		h := newHost(t, debian13)
		h.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte(editedEnv), 0o440, 0, 0)
		goldens(t, "refused-edited", planRun(t, h, sudoAlice, false), false)
	})
	t.Run("refused-doas", func(t *testing.T) {
		goldens(t, "refused-doas", planRun(t, newHost(t, alpine324), doasAlice, false), true)
	})
}

// updating reports whether the test runs with -update (pkg/ui/uitest's
// flag, which make golden-update passes).
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// golden compares got with testdata/<name>, or rewrites it under -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if updating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run make golden-update to create it)", err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s does not match.\n--- want\n%s\n--- got\n%s\nIf the change is intended, run make golden-update and give the reason in the review description.", path, want, got)
	}
}
