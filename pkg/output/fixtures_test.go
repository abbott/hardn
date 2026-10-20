package output_test

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

// The fixtures: synthetic values only (alice, web-01.example.net,
// 192.0.2.0/24, port 2230). Each is a Run as the engine returns it, built
// as data: the engine's types are what output maps, and its own tests
// cover how it fills them.
const (
	fixtureID       = "20261019T120000Z-1a2b3c4d"
	fixtureHost     = "web-01.example.net"
	fixtureVersion  = "0.4.0"
	fixtureHardn    = "/usr/local/bin/hardn"
	fixtureDropIn   = "/etc/ssh/sshd_config.d/00-hardn.conf"
	fixtureConfig   = "/etc/hardn/hardn.yml"
	fixtureListenOn = "2230"
)

// fixtureNow is the moment the fixtures' runs end: 15 s after the change
// was armed, so the countdown reads 2:45.
var fixtureNow = time.Date(2026, 10, 19, 12, 0, 15, 0, time.UTC)

func debian13() detect.Facts {
	return detect.Facts{
		OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie", PrettyName: "Debian GNU/Linux 13 (trixie)"},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH:       detect.SSH{Mode: detect.SSHService},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Container: detect.Container{Detected: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal,
			Doas: detect.Tool{Present: detect.No}},
	}
}

func ubuntu2404() detect.Facts {
	f := debian13()
	f.OS = detect.OSRelease{ID: "ubuntu", IDLike: []string{"debian"}, VersionID: "24.04", VersionCodename: "noble", PrettyName: "Ubuntu 24.04.3 LTS"}
	f.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "24.04", Support: detect.Supported}
	f.SSH = detect.SSH{Mode: detect.SSHSocket}
	return f
}

func alpine324() detect.Facts {
	f := debian13()
	f.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.1", PrettyName: "Alpine Linux v3.24"}
	f.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
	f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}
	f.SSH = detect.SSH{Mode: detect.SSHOpenRC}
	f.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}}
	return f
}

func ptr[T any](v T) *T { return &v }

// portChanges is the 2230 scenario's plan on Debian 13 with ufw active:
// the boot unit first, then the firewall, the drop-in, the reload and the
// check after it.
func portChanges() []engine.Change {
	unit, err := revert.PlanBootUnit(debian13().Init, fixtureHardn)
	if err != nil {
		panic(err)
	}
	before, after := "# Managed by hardn\nPort 22\n", "# Managed by hardn\nPort 22\nPort 2230\n"
	via := []revert.Listener{{Addr: netip.IPv4Unspecified(), Port: 2230}, {Addr: netip.IPv6Unspecified(), Port: 2230}}
	return []engine.Change{
		{ID: engine.BootChangeID, Summary: "Install hardn's boot unit, which re-arms the revert timer after a reboot", Target: unit.Path,
			Diff: engine.FileDiff(unit.Path, nil, ptr(string(unit.Data))), StateBefore: "not installed", StateAfter: "installed and enabled",
			Commands: []string{"systemd-analyze verify --recursive-errors=no /run/hardn/" + fixtureID + "/hardn-boot.service",
				"systemctl --no-reload enable hardn-boot.service"},
			Risk: engine.RiskPlain, Stage: engine.StageBoot},
		{ID: "ssh.port/firewall", Control: "ssh.port", Summary: "Allow 2230/tcp in ufw before sshd listens on it", Target: "ufw",
			StateBefore: "22/tcp ALLOW IN Anywhere", StateAfter: "22/tcp ALLOW IN Anywhere\n2230/tcp ALLOW IN Anywhere",
			Commands: []string{"ufw allow 2230/tcp"}, Risk: engine.RiskLockout, Stage: engine.StageFirewall,
			Resources: []string{steps.ResourceSSHD, steps.ResourceUFW}},
		{ID: "ssh.port/files", Control: "ssh.port", Summary: "Set Port 2230 in " + fixtureDropIn + ", keeping Port 22 until confirmed",
			Target: fixtureDropIn, Diff: engine.FileDiff(fixtureDropIn, &before, &after), Risk: engine.RiskLockout, Stage: engine.StageFiles,
			Backups: []string{fixtureDropIn}, Resources: []string{steps.ResourceSSHD, steps.ResourceUFW},
			Findings: []engine.Finding{{Code: "SSHD_OVERRIDDEN", Detail: "PasswordAuthentication yes in 50-cloud-init.conf is read before 00-hardn.conf, so sshd uses it",
				Source: "/etc/ssh/sshd_config.d/50-cloud-init.conf:1"}}},
		{ID: "ssh.port/reload", Control: "ssh.port", Summary: "Reload ssh.service, which keeps established sessions", Target: "ssh.service",
			Commands: []string{"systemctl reload ssh.service"}, Risk: engine.RiskLockout, Stage: engine.StageReload, ConfirmVia: via},
		{ID: "ssh.port/verify", Control: "ssh.port", Summary: "Check that sshd listens on 22 and 2230", Target: "sshd",
			Commands: []string{"sshd -T", "ss -Htlnp"}, Risk: engine.RiskLockout, Stage: engine.StageVerify},
	}
}

// portPlan is the 2230 scenario's plan, as Engine.Plan returns it.
func portPlan() *engine.Plan {
	cs := portChanges()
	var ids []string
	for _, c := range cs[1:] {
		ids = append(ids, c.ID)
	}
	return &engine.Plan{ID: fixtureID, Facts: debian13(), Changes: cs, Risk: engine.RiskLockout,
		Items: []engine.Item{{Control: "ssh.port", Title: "SSH Port", Changes: ids}}}
}

// applied is what Apply reports for every change of the port plan but the
// boot unit's.
func applied() []engine.Applied {
	return []engine.Applied{{Change: "ssh.port/firewall", Detail: "firewall: verified"}, {Change: "ssh.port/files", Detail: "written"},
		{Change: "ssh.port/reload", Detail: "reload: verified"}, {Change: "ssh.port/verify", Detail: "sshd -T: port 22, 2230"}}
}

var deprecation = config.Deprecation{Key: "permitRootLogin", Line: 12, Found: "false", Use: "no"}

// fixtures are the five documents docs/reference/output.md shows, by
// golden name.
func fixtures() []struct {
	name string
	run  output.Run
	exit int
} {
	facts := debian13()
	reports := []engine.Report{
		{Control: "ssh.port", Title: "SSH Port", Status: engine.Status{State: engine.NotConfigured, Detail: "port 22; hardn.yml asks for 2230",
			Evidence: []string{"sshd -T: port 22"}}},
		{Control: "ssh.root-login", Title: "Root Login", Status: engine.Status{State: engine.Configured, Detail: "key-only",
			Evidence: []string{"sshd -T: permitrootlogin without-password"},
			Findings: []engine.Finding{{Code: "SSHD_OVERRIDDEN", Detail: "PasswordAuthentication yes in 50-cloud-init.conf is read before 00-hardn.conf, so sshd uses it",
				Source: "/etc/ssh/sshd_config.d/50-cloud-init.conf:1"}}}},
		{Control: "updates", Title: "Auto Updates", Status: engine.Status{State: engine.Partial, Detail: "unattended-upgrades installed, not enabled",
			Evidence: []string{"systemctl is-enabled unattended-upgrades: disabled"}}},
		{Control: "firewall.pve", Title: "Proxmox VE Firewall", Status: engine.Status{State: engine.NotApplicable, Detail: "not a Proxmox VE host"}},
	}
	refusal := &steps.Refusal{Code: steps.CodeListenConfNotInEffect,
		Why: "/etc/systemd/system/ssh.socket.d/listen.conf (hardn v0.3.x) sets ssh.socket to port " + fixtureListenOn +
			", but it never took effect: ssh.socket is configured for [0.0.0.0:22 [::]:22] (no daemon-reload since it was written, D9). Migrating it now could not be rolled back",
		Fix: "first make sure the firewall lets port " + fixtureListenOn + " through, then run `systemctl daemon-reload` and `systemctl restart ssh.socket`, check that a new SSH connection to port " +
			fixtureListenOn + " works, and run hardn again",
		Err: fmt.Errorf("migrate: listen.conf: %w", reload.ErrRestoredListeners)}
	refusedPlan := &engine.Plan{ID: fixtureID, Facts: ubuntu2404(), Risk: engine.RiskPlain,
		Items:   []engine.Item{{Control: "ssh.migrate", Title: "Legacy SSH files", Err: refusal}},
		Refused: []error{refusal}}
	reloadErr := fmt.Errorf("reload: %w", &host.ExitError{Argv: []string{"systemctl", "reload", "ssh.service"}, ExitCode: 1,
		Tail: []string{"Job for ssh.service failed because the control process exited with error code."}})
	failed := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/reload", Step: engine.StepApply, Err: reloadErr,
		Rollback: revert.RollbackResult{
			Restored: []backup.Restored{{Seq: 1, Path: fixtureDropIn}},
			Undone: []revert.StepResult{{Step: revert.Step{Op: revert.OpRun, Argv: []string{"ufw", "delete", "allow", "2230/tcp"},
				Why: "delete the ufw rule for 2230/tcp that hardn added"}}}}}
	boot := &engine.BootInstall{Detail: "wrote /etc/systemd/system/hardn-boot.service; enabled it"}
	confirm := &engine.Confirm{ID: fixtureID, Deadline: time.Date(2026, 10, 19, 12, 3, 0, 0, time.UTC), Approximate: true,
		Via: portChanges()[3].ConfirmVia}
	return []struct {
		name string
		run  output.Run
		exit int
	}{
		{"check", output.Run{Mode: output.ModeCheck, HardnVersion: fixtureVersion, Hostname: fixtureHost, Facts: &facts, Reports: reports,
			Deprecations: []config.Deprecation{deprecation}, ConfigPath: fixtureConfig}, output.ExitChanges},
		{"plan", output.Run{Mode: output.ModePlan, HardnVersion: fixtureVersion, Hostname: fixtureHost, Plan: portPlan()}, output.ExitChanges},
		{"refused", output.Run{Mode: output.ModePlan, HardnVersion: fixtureVersion, Hostname: fixtureHost, Plan: refusedPlan,
			Err: refusedPlan.Err()}, output.ExitError},
		{"awaiting-confirmation", output.Run{Mode: output.ModeApply, HardnVersion: fixtureVersion, Hostname: fixtureHost, Plan: portPlan(),
			Result: &engine.Result{ID: fixtureID, Risk: engine.RiskLockout, Boot: boot, Applied: applied(), Confirm: confirm}}, output.ExitOK},
		{"rolled-back", output.Run{Mode: output.ModeApply, HardnVersion: fixtureVersion, Hostname: fixtureHost, Plan: portPlan(),
			Result: &engine.Result{ID: fixtureID, Risk: engine.RiskLockout, Boot: boot, Applied: applied()[:2], Failed: failed},
			Err:    failed}, output.ExitError},
	}
}
