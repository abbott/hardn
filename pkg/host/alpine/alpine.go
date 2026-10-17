// Package alpine holds hardn's Alpine Linux rules (refactor plan P2,
// next/p2-alpine; decision §3.20): automatic updates with apk-cron and
// crond, which hardn configures, and the status of AppArmor and Lynis,
// which it does not automate on Alpine.
//
// Updates: ReadUpdates only reads, Updates.Status reports, PlanUpdates is
// pure, and Apply runs a plan over host.System, checking that each step
// took effect and undoing the steps it ran when one fails; Rollback undoes
// a plan. Once apk-cron is in, origin/main's own
// /etc/periodic/daily/apk-upgrade is removed, after a backup. AppArmor
// (CheckAppArmor) and Lynis are reports: NotApplicable unless AppArmor is
// already enabled, with a note on what enabling it takes.
//
// docs/safety/alpine.md describes the guarantees, the platform facts they
// rest on and what is written but not yet run on an Alpine host.
package alpine

import (
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/host/detect"
)

// Paths, names and the content origin/main wrote.
const (
	// Package installs Script (aports main/apk-cron).
	Package = "apk-cron"
	// DailyDir is what root's crontab runs at 02:00 with run-parts, which
	// runs only executable files whose names do not start with '.'.
	DailyDir = "/etc/periodic/daily"
	// Script is apk-cron's daily job: up to two hours of random sleep, then
	// apk -U upgrade.
	Script = DailyDir + "/apk"
	// LegacyScript is the daily job origin/main (and v0.3.2) wrote.
	LegacyScript = DailyDir + "/apk-upgrade"
	// LegacyContent is exactly what they wrote there, mode 0755: a file
	// holding anything else is not hardn's and is left alone.
	LegacyContent = "#!/bin/sh\napk update && apk upgrade --available\n"
	// Service is busybox's crond under OpenRC (busybox-openrc).
	Service    = "crond"
	InitScript = "/etc/init.d/crond"
	// Runlevel is where hardn adds crond; RunlevelLink is the link
	// rc-update add makes there.
	Runlevel     = "default"
	RunlevelLink = detect.OpenRCRunlevels + "/" + Runlevel + "/" + Service
)

// Errors, testable with errors.Is.
var (
	ErrNotAlpine = errors.New("not an Alpine host")
	// ErrUnknown: a fact the answer depends on is not established; the
	// error wraps why.
	ErrUnknown  = errors.New("a fact the answer depends on is not established")
	ErrNoOpenRC = errors.New("OpenRC did not boot the host, so it runs no crond")
	ErrNoCrond  = errors.New("crond's OpenRC service is not installed")
	// ErrScript: apk-cron and its daily script disagree (the script is
	// missing, not executable, or there without the package); hardn does
	// not guess which the administrator meant.
	ErrScript   = errors.New("apk-cron's daily script does not match the package's state")
	ErrNoBackup = errors.New("removing the legacy script needs a backup run")
	ErrChanged  = errors.New("the host changed since it was read")
	// ErrNotEffective: a command succeeded but the host does not show its
	// effect.
	ErrNotEffective = errors.New("the step ran, but the host does not show its effect")
	ErrNotRestored  = errors.New("the steps were not all undone")
)

// State is a control's status, as brief Phase 4 names them. The zero value
// is Unknown.
type State uint8

const (
	StateUnknown State = iota
	StateConfigured
	StatePartial
	StateNotConfigured
	StateNotApplicable // left out of the risk score (plan p4-risk)
)

func (s State) String() string {
	switch s {
	case StateConfigured:
		return "configured"
	case StatePartial:
		return "partial"
	case StateNotConfigured:
		return "not-configured"
	case StateNotApplicable:
		return "not-applicable"
	}
	return "unknown"
}

// FindingKind names a finding; its String is stable.
type FindingKind uint8

const (
	// FindingLegacyScript: origin/main's own LegacyScript; a plan removes
	// it once apk-cron is in, after a backup.
	FindingLegacyScript FindingKind = iota + 1
	// FindingForeignScript: something else at LegacyScript, which hardn
	// leaves alone; if it upgrades too, two daily jobs do.
	FindingForeignScript
)

func (k FindingKind) String() string {
	switch k {
	case FindingLegacyScript:
		return "alpine-legacy-script"
	case FindingForeignScript:
		return "alpine-foreign-script"
	}
	return "unknown"
}

// Finding is something a status or plan reports; it never stops a plan.
type Finding struct {
	Kind   FindingKind
	Path   string
	Detail string
}

// Status is what a check found.
type Status struct {
	State    State
	Detail   string
	Evidence []string // what was read or run, and what it said
	Findings []Finding
	Err      error // why State is Unknown
}

// Lynis is NotApplicable on Alpine: it is not packaged in any stable
// branch.
func Lynis(f detect.Facts) (Status, error) {
	if err := alpineOnly(f); err != nil {
		return Status{Err: err}, err
	}
	return Status{State: StateNotApplicable, Detail: "Lynis is not packaged in any stable Alpine branch"}, nil
}

// alpineOnly is nil on Alpine, ErrNotAlpine on another distribution, and
// ErrUnknown when the distribution is not established: never "not
// Alpine" then.
func alpineOnly(f detect.Facts) error {
	switch d := f.Platform.Distro; d {
	case detect.DistroAlpine:
		return nil
	case detect.DistroUnknown:
		return fmt.Errorf("alpine: distribution: %w: %w", ErrUnknown, errOr(f.Platform.Err, detect.ErrNoSignal))
	default:
		return fmt.Errorf("alpine: %q: %w", d, ErrNotAlpine)
	}
}

func errOr(err, def error) error {
	if err != nil {
		return err
	}
	return def
}
