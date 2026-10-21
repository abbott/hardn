package updates

import (
	"context"
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
)

// checkApk reads apk-cron through alpine.ReadUpdates, whose facts are
// next/p2-alpine's: Configured is its own rule (apk-cron installed with
// its executable daily script, crond in the default runlevel and
// started). The other lines describe the host now, as on apt's hosts
// (check-updates.md §2.6): no apk-cron, its script not run, or crond not
// running is NotConfigured, since nothing upgrades; crond running but not
// in the default runlevel is Partial, off after restart. A host OpenRC did
// not boot (the Alpine container image), which ReadUpdates refuses with
// alpine.ErrNoOpenRC, is Unknown as alpine.md §2.1 decided, with
// CodeScheduleUnknown and no error, as apt's job on a host systemd did
// not boot: nothing failed, and nothing read says whether a crond runs.
func checkApk(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	u, err := alpine.ReadUpdates(ctx, sys, f)
	if errors.Is(err, alpine.ErrNoOpenRC) {
		ev := []string{fmt.Sprintf("init: %s, running %s: OpenRC did not boot the host", f.Init.Kind, f.Init.Running)}
		why := engine.Finding{Code: CodeScheduleUnknown, Detail: "openrc not running", Source: f.Init.Source}
		return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: ev, Findings: []engine.Finding{why}}, nil
	}
	if err != nil {
		return unread(u.Evidence, wrap(err))
	}
	why := fmt.Errorf("updates: %w", errOr(u.Err, detect.ErrNoSignal)) // every field not established, joined
	tri := func(t detect.Tri) fact {
		if t == detect.Unknown {
			return fact{err: why}
		}
		return fact{t: t}
	}
	script := map[alpine.ScriptState]fact{alpine.ScriptInstalled: known(true), alpine.ScriptAbsent: known(false), alpine.ScriptOther: known(false)}[u.Script]
	crond := map[alpine.CrondState]fact{alpine.CrondStarted: known(true), alpine.CrondStopped: known(false), alpine.CrondCrashed: known(false), alpine.CrondMissing: known(false)}[u.Crond]
	if u.Script == alpine.ScriptUnknown {
		script = fact{err: why}
	}
	if u.Crond == alpine.CrondUnknown {
		crond = fact{err: why}
	}
	st, err := decide(u.Evidence,
		[]cond{{tri(u.Package), DetailNoApkCron}, {script, DetailScriptNotRun}, {crond, DetailCrondOff}},
		[]cond{{tri(u.Runlevel), DetailOffAfterRestart}},
		DetailApkCron)
	for _, a := range u.Status().Findings {
		st.Findings = append(st.Findings, engine.Finding{Code: steps.FindingCode("ALPINE", a.Kind.String()), Detail: legacyDetail(a), Source: steps.SourceAt(a.Path, 0)})
	}
	return st, err
}

// legacyDetail is a finding of alpine.ReadUpdates' about
// /etc/periodic/daily/apk-upgrade, said of the host.
func legacyDetail(a alpine.Finding) string {
	switch a.Kind {
	case alpine.FindingLegacyScript:
		return "old apk-upgrade script"
	case alpine.FindingForeignScript:
		return "other apk-upgrade file"
	}
	return "apk-upgrade file" // a kind added to alpine after this unit
}
