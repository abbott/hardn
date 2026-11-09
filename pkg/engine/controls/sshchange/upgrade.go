package sshchange

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
)

// ErrUpgradeUnknown: whether the openssh-server package sshd runs has
// CVE-2026-123158's fix is not established, so the upgrade is not planned.
var ErrUpgradeUnknown = errors.New("whether openssh-server has the fix for CVE-2026-123158 is not established")

// ErrPasswordsUnfixed: Password login on, where sshd lacks
// CVE-2026-123158's fix and checks passwords without PAM.
var ErrPasswordsUnfixed = errors.New("password login would open sshd's unfixed CVE-2026-123158")

// The CVE's refusals' codes: ErrUpgradeUnknown's, ErrPasswordsUnfixed's.
const (
	codeUpgradeUnknown   = "SSH_UPGRADE_UNKNOWN"
	codePasswordsUnfixed = "SSH_PASSWORDS_UNFIXED"
)

// The upgrade's words: the plan's, and its failures' (proposed,
// 2026-11-08).
const (
	wordsUpgradeUnknown = "hardn can't tell if %s has the fix for CVE-2026-123158: %s. Nothing is upgraded until it can."
	// wordsPassUnfixed refuses Password login on (passwordsUnfixed);
	// approved by the maintainer, 2026-11-08.
	wordsPassUnfixed = "%s lacks the fix for CVE-2026-123158: with passwords on, anyone could log in as an account with no password. Upgrade openssh first: item 5."
	// UpgradeTitle is the upgrade's change's title, and Run All's row.
	UpgradeTitle = "Upgrade openssh"
	verifyTitle  = "Check sshd runs the fix"
)

// passwordsUnfixed refuses Password login on (docs/engine/control-ssh.md
// §1) on Alpine where sshd, as the SSH rows read it (ssh.ReadBypass),
// lacks CVE-2026-123158's fix and checks passwords without PAM (its PAM
// not established as on, as Create's refusal takes it): passwords on
// would let anyone log in as an account whose field is "*", as every key
// user's Create writes is, once any account has a $5$ or $6$ hash, which
// is not asked: one may be set later. Create refuses the same host from
// the other side (pkg/engine/controls/users, bypassRefusal). A fix not
// established, or no sshd that can run (ssh.Bypass.NoSSHD), refuses
// nothing here.
func passwordsUnfixed(ctx context.Context, sys host.System, f detect.Facts) error {
	b, _ := ssh.ReadBypass(ctx, sys, f)
	if !b.Applies || b.NoSSHD || b.PAM.Uses == detect.Yes || b.Fix.Fixed != detect.No {
		return nil
	}
	return refuse(codePasswordsUnfixed, ErrPasswordsUnfixed, nil, fmt.Sprintf(wordsPassUnfixed, b.Fix.Package+" "+b.Fix.Installed))
}

// Upgrade is SSH Login's Upgrade openssh (item 5) and Run All's on Alpine
// where sshd lacks CVE-2026-123158's fix (the maintainer, 2026-11-08,
// docs/safety/alpine.md §7.1): every installed package of the openssh
// build upgraded together once apk's simulation lists their upgrades
// alone (alpine.ApplyUpgrade), sshd reloaded so that its listener runs the
// new binary (steps.BinaryReload), then checked: the package has the fix
// and the listener runs a binary that is still on disk. Its check is SSH
// Auth's, whose row carries the CVE's findings, so it is that row's
// control. The upgrade has no undo (the old packages are gone from the
// repository); a failed reload starts sshd again. Nothing of it is
// lockout-risk: no file of sshd's changes.
type Upgrade struct {
	ssh.Auth
	// Wait waits d or until ctx is done, between the check's reads of the
	// listener; nil sleeps. Tests replace it.
	Wait func(ctx context.Context, d time.Duration) error
}

// The check's reads of the listener: on SIGHUP sshd removes its pid file
// and executes itself again, writing the file once it has read its
// configuration and bound its listeners (OpenSSH sshd.c,
// sighup_restart), so a read just after rc-service sshd reload may find
// no pid file, or the old binary; the check reads again until the deadline.
const (
	settleAttempts = 50
	settleInterval = 100 * time.Millisecond
)

// sleep waits d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ReadFix is, on Alpine, whether the openssh-server package sshd runs has
// CVE-2026-123158's fix: alpine.OpenSSHFix of the package of the binary
// OpenRC's script runs (reload.OpenRCFlavour), Unknown where that is not
// read; the zero Fix off Alpine. It changes nothing.
func ReadFix(ctx context.Context, sys host.System, f detect.Facts) alpine.Fix {
	if f.Platform.Distro != detect.DistroAlpine {
		return alpine.Fix{}
	}
	var bin string
	if f.Init.Kind == detect.OpenRC {
		var err error
		if bin, err = reload.OpenRCFlavour(ctx, sys); err != nil {
			return fixUnread(f)
		}
	}
	return alpine.OpenSSHFix(ctx, sys, ssh.ServerPackage(bin), f.OS.VersionID)
}

// fixUnread is the Fix where which sshd runs is not read.
func fixUnread(f detect.Facts) alpine.Fix {
	return alpine.Fix{Wanted: alpine.FixedVersion(f.OS.VersionID), Why: "can't tell which sshd runs"}
}

// Plan plans the upgrade on Alpine: none where the server sshd runs (the
// binary OpenRC's script runs, reload.OpenRCFlavour) has the fix; a
// refusal where that is not established (codeUpgradeUnknown); else the
// upgrade (StagePackages), the reload (StageReload) and the check
// (StageVerify), each claiming sshd. It runs only reads.
func (u Upgrade) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	f := in.Facts
	if f.Platform.Distro != detect.DistroAlpine {
		return nil, fmt.Errorf("sshchange: upgrade: %w", alpine.ErrNotAlpine)
	}
	var bin string
	if f.Init.Kind == detect.OpenRC {
		var err error
		if bin, err = reload.OpenRCFlavour(ctx, sys); err != nil {
			return nil, refuse(codeUpgradeUnknown, ErrUpgradeUnknown, err, fmt.Sprintf(wordsUpgradeUnknown, alpine.OpenSSHServer, "can't tell which sshd runs"))
		}
	}
	up, err := alpine.PlanUpgrade(ctx, sys, ssh.ServerPackage(bin), f.OS.VersionID)
	switch {
	case errors.Is(err, alpine.ErrUnknown):
		return nil, refuse(codeUpgradeUnknown, ErrUpgradeUnknown, err, fmt.Sprintf(wordsUpgradeUnknown, up.From.Package, up.From.Why))
	case err != nil:
		return nil, err
	case up.Empty():
		return nil, nil
	}
	id := u.ID()
	reloadCh, err := steps.BinaryReload(id, id+"/upgrade", f.SSH)
	if err != nil {
		return nil, err
	}
	wait := u.Wait
	if wait == nil {
		wait = sleep
	}
	return []engine.Change{upgradeChange(id, up, f.OS.VersionID), reloadCh, verifyUpgrade(id, up, f.OS.VersionID, wait)}, nil
}

// upgradeChange is the upgrade itself (StagePackages, RiskPackageInstall),
// with no undo: alpine.ApplyUpgrade, its refusals at apply in the user's
// words (upgradeFailure).
func upgradeChange(id string, up alpine.Upgrade, versionID string) engine.Change {
	var cmds []string
	for _, c := range up.Commands() {
		cmds = append(cmds, host.FormatArgv(c))
	}
	from := up.From
	return engine.Change{ID: id + "/upgrade", Title: UpgradeTitle, Target: "apk",
		Summary:     fmt.Sprintf("Upgrade the openssh packages: %s %s, fixed in %s", from.Package, from.Installed, from.Wanted),
		StateBefore: from.Package + " " + from.Installed, StateAfter: from.Wanted + " or later",
		Commands: cmds, Risk: engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: []string{steps.ResourceSSHD},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			res, err := alpine.ApplyUpgrade(ctx, sys, up, versionID)
			ran := engine.Ran{Rollback: &engine.Undo{}, Detail: "upgrade: " + strings.Join(res.Changes, "; ")}
			return ran, upgradeFailure(err)
		}}
}

// upgradeFailure is alpine.ApplyUpgrade's err in the user's words where
// they are known (proposed, 2026-11-08); any other error as it is.
func upgradeFailure(err error) error {
	var other *alpine.OtherChangesError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &other):
		return &steps.Failure{Text: "Upgrading openssh would also change " + strings.Join(other.Names(), ", "),
			Next: "Run apk -U upgrade, then rc-service sshd reload.", Err: err}
	case errors.Is(err, alpine.ErrUpdate):
		return &steps.Failure{Text: "apk couldn't update its package lists", Next: "Check the network and /etc/apk/repositories, then try again.", Err: err}
	case errors.Is(err, alpine.ErrNoFixOffered):
		return &steps.Failure{Text: "apk offers no openssh-server with the fix", Next: "Check /etc/apk/repositories and /etc/apk/world, then try again.", Err: err}
	case errors.Is(err, alpine.ErrNotEffective):
		return &steps.Failure{Text: "openssh-server still lacks the fix after the upgrade", Next: "Run apk -U upgrade, then rc-service sshd reload.", Err: err}
	}
	return err
}

// verifyUpgrade checks, once sshd was reloaded (StageVerify), that the
// server package has the fix and, where OpenRC has sshd started, that its
// listener runs a binary still on disk (alpine.ListenerBinary: not one the
// upgrade replaced), read until it does or settleAttempts reads have
// passed, settleInterval apart (wait), as sshd's restart on SIGHUP takes a
// moment. It changes nothing; a failure rolls back the reload, which
// reloads sshd again or starts it.
func verifyUpgrade(id string, up alpine.Upgrade, versionID string, wait func(context.Context, time.Duration) error) engine.Change {
	return engine.Change{ID: id + "/verify", Title: verifyTitle, Summary: "Check that " + up.From.Package + " has the fix and sshd's listener runs it",
		Target: "sshd", Risk: engine.RiskServiceRestart, Stage: engine.StageVerify, Resources: []string{steps.ResourceSSHD},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			fix := alpine.OpenSSHFix(ctx, sys, up.From.Package, versionID)
			if fix.Fixed != detect.Yes {
				return engine.Ran{}, &steps.Failure{Text: up.From.Package + " still lacks the fix after the upgrade",
					Next: "Run apk -U upgrade, then rc-service sshd reload.", Err: fmt.Errorf("%s %s: %w", fix.Package, fix.Installed, alpine.ErrNotEffective)}
			}
			if _, err := sys.Stat(ctx, detect.OpenRCStarted+"/"+reload.OpenRCService); err != nil {
				return engine.Ran{Detail: "verify: " + fix.Package + " " + fix.Installed + "; sshd not started"}, nil
			}
			var path string
			var stale bool
			var err error
			for i := 1; ; i++ {
				if path, stale, err = alpine.ListenerBinary(ctx, sys); err == nil && !stale || i == settleAttempts {
					break
				}
				if werr := wait(ctx, settleInterval); werr != nil {
					return engine.Ran{}, werr
				}
			}
			switch {
			case err != nil:
				return engine.Ran{}, &steps.Failure{Text: "hardn can't tell which binary sshd runs", Next: "Run rc-service sshd restart.", Err: err}
			case stale:
				return engine.Ran{}, &steps.Failure{Text: "sshd still runs the binary the upgrade replaced", Next: "Run rc-service sshd restart.",
					Err: fmt.Errorf("sshd's listener runs %s (deleted): %w", path, alpine.ErrNotEffective)}
			}
			return engine.Ran{Detail: "verify: " + fix.Package + " " + fix.Installed + ", sshd runs " + path}, nil
		}}
}
