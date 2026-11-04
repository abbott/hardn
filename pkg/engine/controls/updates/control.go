package updates

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/aptconf"
)

// The Auto Updates control both ways (refactor plan slice 7, unit (b);
// docs/engine/control-updates.md, gallery-updates.md §4 and §5): on, off,
// the record and the refusals, as the mock (internal/tools/uigallery,
// updates.go) specified them.

// AutoAction is what Auto's Plan does. The direction is the control's, not
// an engine.Input field (a call of this unit): Run All builds Auto with
// AutoOn alone, so it never turns updates off (decision 2).
type AutoAction uint8

// The actions.
const (
	// AutoNone is the status list's: Plan is engine.ErrNotMigrated.
	AutoNone AutoAction = iota
	// AutoOn turns updates on: unattended-upgrades and hardn's apt file, or
	// apk-cron and crond, then the record.
	AutoOn
	// AutoOff undoes what the record says hardn did, saves
	// enableUnattendedUpgrades false and deletes the record.
	AutoOff
)

// What hardn writes and reads.
const (
	// HardnFile is hardn's own apt file (decision 3): read after
	// 50unattended-upgrades and before 70debconf.
	HardnFile = aptconf.PartsDir + "/52hardn-auto-upgrades"
	// AutoUpgrades and UnattendedConfig are the files unattended-upgrades'
	// postinst writes (through ucf) and ships.
	AutoUpgrades     = aptconf.PartsDir + "/20auto-upgrades"
	UnattendedConfig = aptconf.PartsDir + "/50unattended-upgrades"
	// Package is the package apt's upgrades need.
	Package = unattended
	// Setting is hardn.yml's key off saves as false (decision 2).
	Setting = "enableUnattendedUpgrades"

	hardnHead    = "// Written by hardn (Auto Updates).\n"
	upgradeTimer = upgradeJob + ".timer"
	listsTimer   = listsJob + ".timer"
	upgradeSvc   = upgradeJob + ".service"
	listsSvc     = listsJob + ".service"
)

// hardnLines are the lines hardn's file can hold, in its order.
var hardnLines = []string{keyLists, keyUpgrade}

// hardnBytes is hardn's file holding keys' lines, each "1", in hardnLines'
// order.
func hardnBytes(keys []string) []byte {
	var b strings.Builder
	b.WriteString(hardnHead)
	for _, k := range hardnLines {
		for _, w := range keys {
			if strings.EqualFold(w, k) {
				fmt.Fprintf(&b, "%s \"1\";\n", k)
				break
			}
		}
	}
	return []byte(b.String())
}

// IsHardnFile reports whether data is one of the files hardn writes: with
// no record, off deletes hardn's file only while it holds such bytes.
func IsHardnFile(data []byte) bool {
	for _, ks := range [][]string{{keyLists}, {keyUpgrade}, hardnLines} {
		if string(data) == string(hardnBytes(ks)) {
			return true
		}
	}
	return false
}

// Errors of the refusals, testable with errors.Is, each the Err of a
// *steps.Refusal in the decided words (gallery-updates.md §5), wrapping
// the reader's error where there is one.
var (
	// ErrNotRead: how updates run here is not read (apt's settings, the
	// check, apk-cron's state).
	ErrNotRead = errors.New("how updates run is not read, so no change is planned")
	// ErrPeriodicOff: APT::Periodic::Enable "0" turns apt's daily jobs off.
	ErrPeriodicOff = errors.New("APT::Periodic::Enable turns apt's daily jobs off")
	// ErrTimerOff: one of apt's timers is off.
	ErrTimerOff = errors.New("an apt timer is off")
	// ErrJobMasked: one of apt's daily services is masked.
	ErrJobMasked = errors.New("an apt daily service is masked")
	// ErrJobMissing: one of apt's daily services is not installed.
	ErrJobMissing = errors.New("an apt daily service is missing")
	// ErrNoOrigins: 50unattended-upgrades allows no origin.
	ErrNoOrigins = errors.New("unattended-upgrades allows no origin")
	// ErrProxmox: Proxmox VE is updated by hand.
	ErrProxmox = errors.New("hardn does not change updates on Proxmox VE")
	// ErrOtherPackages: installing or removing the package would change
	// other packages.
	ErrOtherPackages = errors.New("the package change would change other packages")
	// ErrNotInRepos: unattended-upgrades is not in apt's package lists.
	ErrNotInRepos = errors.New("unattended-upgrades is not in apt's package lists")
	// ErrNotInEffect: apt does not read the setting back as the change set it.
	ErrNotInEffect = errors.New("apt does not read the update setting as changed")
	// ErrRecordUnreadable: hardn's record of the control cannot be read.
	ErrRecordUnreadable = errors.New("hardn's record of the updates it turned on cannot be read")
	// ErrChangedSince: a file hardn left changed since.
	ErrChangedSince = errors.New("a file hardn left changed since")
	// ErrNothingToUndo: hardn did not turn updates on.
	ErrNothingToUndo = errors.New("hardn did not turn updates on")
	// ErrStaysOn: with no record, another file keeps updates on without
	// hardn's.
	ErrStaysOn = errors.New("another file keeps updates on without hardn's")
)

// The refusals' codes, the registry's for the errors they wrap
// (pkg/output; pinned by the tests).
const (
	codeNotRead       = "UPDATES_NOT_READ"
	codePeriodicOff   = "UPDATES_PERIODIC_OFF"
	codeTimerOff      = "UPDATES_TIMER_OFF"
	codeJobMasked     = "UPDATES_JOB_MASKED"
	codeJobMissing    = "UPDATES_JOB_MISSING"
	codeNoOrigins     = "UPDATES_NO_ORIGINS"
	codeProxmox       = "UPDATES_PROXMOX"
	codeOtherPackages = "UPDATES_OTHER_PACKAGES"
	codeNotInRepos    = "UPDATES_NOT_IN_REPOS"
	codeNotInEffect   = "UPDATES_NOT_IN_EFFECT"
	codeRecord        = "UPDATES_RECORD_UNREADABLE"
	codeChangedSince  = "UPDATES_CHANGED_SINCE"
	codeNothing       = "UPDATES_NOTHING_TO_UNDO"
	codeStaysOn       = "UPDATES_STAY_ON"
)

// refuse is a refusal: its code, its sentinel wrapping cause where there
// is one, and the mock's Why and Fix, which screenkit.RefusalWords joins.
func refuse(code string, sentinel, cause error, why, fix string) error {
	err := sentinel
	if cause != nil {
		err = fmt.Errorf("%w: %w", sentinel, cause)
	}
	return &steps.Refusal{Code: code, Why: why, Fix: fix, Err: err}
}

// The changes' ids, under ID.
const (
	ChangeInstall   = ID + "/install"
	ChangeFile      = ID + "/file"
	ChangeApk       = ID + "/apk"
	ChangeRecord    = ID + "/record"
	ChangePackage   = ID + "/package"
	ChangeCrondStop = ID + "/crond-stop"
	ChangeCrondBoot = ID + "/crond-boot"
	ChangeSave      = ID + "/save"
)

// Plan reads the host (Read) and plans the direction Action asks: nothing
// at AutoNone (engine.ErrNotMigrated); on (planOn) or off (planOff), each
// refusing, before anything is written, with a *steps.Refusal in the
// decided words. A host already as asked plans nothing.
func (c Auto) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	switch c.Action {
	case AutoNone:
		return nil, engine.ErrNotMigrated
	case AutoOn, AutoOff:
	default:
		return nil, fmt.Errorf("updates: action %d: %w", c.Action, fs.ErrInvalid)
	}
	r, err := c.Read(ctx, sys, in.Facts, in.Config)
	if err != nil {
		return nil, err
	}
	if c.Action == AutoOn {
		return c.planOn(ctx, sys, in, r)
	}
	cs, _, err := c.planOff(ctx, sys, in, r)
	return cs, err
}

// Note is one line of a review beyond the plan's changes, in the decided
// words (gallery-updates.md §5): "Also starts", "<file> Kept", a kept
// dependency, crond kept, the rollback's lines. Warn marks one the screen
// draws as a warning.
type Note struct {
	Key, Value, Note string
	Warn             bool
}

// Notes are the review's lines for Action's direction on the host as Plan
// reads it: on, the jobs a stopped crond also starts (decision 6) or the
// package's files there before its install (R2); off, what stays and why,
// and what a rollback does (decision 5). None at AutoNone, and none where
// the direction refuses: the refusal says why.
func (c Auto) Notes(ctx context.Context, sys host.System, in engine.Input) ([]Note, error) {
	r, err := c.Read(ctx, sys, in.Facts, in.Config)
	if err != nil {
		return nil, err
	}
	switch c.Action {
	case AutoOn:
		return r.onNotes(), nil
	case AutoOff:
		_, notes, err := c.planOff(ctx, sys, in, r)
		var ref *steps.Refusal
		if errors.As(err, &ref) {
			return nil, nil
		}
		return notes, err
	}
	return nil, nil
}

// firstLine is s to its first line break.
func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}
