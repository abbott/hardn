package output_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
)

// TestBestEffortRendered (P-41, decided 2026-10-19): the finding the
// engine puts on a plan made on a best-effort release (engine.Plan.Findings,
// PLATFORM_BEST_EFFORT) is a finding of the document, the plan's own (no
// control or change), and a warning with its code in the human report of
// the plan and of the apply, and below a flow (AfterFlow) that did not
// list it (since next/s2-flow-review a flow lists it before its
// confirmation: the gallery's best-effort-ubuntu2204 scenario); it changes
// no exit status. A plan on a supported release shows none.
func TestBestEffortRendered(t *testing.T) {
	const detail = "Ubuntu 22.04 is a best-effort release: its vendor still supports it, and hardn changes it but makes no promises for it"
	bestEffort := func() *engine.Plan {
		p := portPlan()
		p.Facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "22.04", Support: detect.BestEffort}
		p.Findings = []engine.Finding{{Code: engine.CodeBestEffort, Detail: detail}}
		return p
	}
	for _, tc := range []struct {
		name string
		run  output.Run
		exit int
		want bool
	}{
		{"a plan, best effort", output.Run{Mode: output.ModePlan, Plan: bestEffort()}, output.ExitChanges, true},
		{"an apply, best effort", output.Run{Mode: output.ModeApply, Plan: bestEffort(),
			Result: &engine.Result{ID: fixtureID, Risk: engine.RiskLockout, Applied: applied()}}, output.ExitOK, true},
		{"a plan, supported", output.Run{Mode: output.ModePlan, Plan: portPlan()}, output.ExitChanges, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := output.Build(tc.run)
			var found []output.Finding
			for _, f := range d.Findings {
				if f.Code == engine.CodeBestEffort {
					found = append(found, f)
				}
			}
			switch {
			case !tc.want && len(found) > 0:
				t.Fatalf("findings %+v, want no %s", found, engine.CodeBestEffort)
			case tc.want && (len(found) != 1 || found[0].Detail != detail || found[0].Control != "" || found[0].Change != ""):
				t.Fatalf("findings %+v, want one %s of the plan's own", d.Findings, engine.CodeBestEffort)
			}
			if got := output.ExitCode(tc.run); got != tc.exit {
				t.Errorf("exit %d, want %d", got, tc.exit)
			}
			warning := squash(detail + " (" + engine.CodeBestEffort + ")")
			if h := squash(humanText(t, tc.run)); strings.Contains(h, warning) != tc.want {
				t.Errorf("the human report shows the warning: %v, want %v:\n%s", !tc.want, tc.want, humanText(t, tc.run))
			}
			if tc.run.Mode != output.ModeApply {
				return
			}
			env := plainEnv()
			var b bytes.Buffer
			if _, err := io.WriteString(env.Writer(&b), output.AfterFlow(env, tc.run, fixtureNow)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(squash(b.String()), warning) {
				t.Errorf("AfterFlow does not show the warning:\n%s", b.String())
			}
		})
	}
}
