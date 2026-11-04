package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	updatesctl "github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The Auto Updates item's tests (refactor plan slice 7, unit (d)): the
// Auto Updates screen on the DNS tests' Alpine 3.24 host (dns_test.go),
// apk-cron not installed and crond stopped, its hardn.yml saying
// enableUnattendedUpgrades true; apk, rc-service and rc-update answered as
// the screen's model answers them (pkg/screens/updates/fake_test.go).
// Synthetic values only.

// updatesTestVersion is hardn's version as main gives it to menuOptions.
const updatesTestVersion = "0.4.0-synthetic"

// updatesHost is the Alpine host with apk-cron's state and crond's.
type updatesHost struct {
	*dnsHost
	apkCron, crond bool
	apkFails       bool // apk add apk-cron fails
}

// newUpdatesHost returns the host, and its hardn.yml as hardn reads it,
// enableUnattendedUpgrades true.
func newUpdatesHost(t *testing.T) (*updatesHost, *config.Config) {
	t.Helper()
	d, _ := newDNSHost(t, "alpine324", dnsTestList)
	h := &updatesHost{dnsHost: d}
	for _, dir := range alpine.PeriodicDirs {
		h.AddDir(alpine.PeriodicDir+"/"+dir, 0o755, 0, 0)
	}
	h.AddDir(alpine.CrontabsDir, 0o755, 0, 0)
	h.AddFile(alpine.RootCrontab, []byte(alpine.StockCrontab), 0o600, 0, 0)
	h.AddDir("/etc/init.d", 0o755, 0, 0)
	h.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	h.AddDir("/etc/runlevels/default", 0o755, 0, 0)
	h.AddDir("/proc", 0o555, 0, 0)
	h.AddFile(header.UptimeFile, []byte("259200.00 518400.00\n"), 0o444, 0, 0)
	h.AddFile(dnsTestYML, []byte(h.content(dnsTestYML)+"enableUnattendedUpgrades: true\n"), 0o644, 0, 0)
	h.Handler = h.answer
	cfg, err := config.Read(context.Background(), h, dnsTestYML)
	if err != nil || !cfg.EnableUnattendedUpgrades {
		t.Fatalf("hardn.yml: %v, enableUnattendedUpgrades %t", err, cfg != nil && cfg.EnableUnattendedUpgrades)
	}
	return h, cfg
}

// answer is apk's, rc-service's and rc-update's commands as the screen's
// model answers them; any other is the DNS host's.
func (h *updatesHost) answer(argv []string, stdin []byte) hostfake.Response {
	ctx := context.Background()
	const added = "(1/1) Installing " + alpine.Package + " (1.0-r3)\nOK: 9 MiB in 42 packages\n"
	const purged = "(1/1) Purging " + alpine.Package + " (1.0-r3)\nOK: 9 MiB in 41 packages\n"
	switch strings.Join(argv, " ") {
	case "apk info -e " + alpine.Package:
		if h.apkCron {
			return hostfake.Response{Stdout: alpine.Package + "\n"}
		}
		return hostfake.Response{ExitCode: 1}
	case "apk add --simulate " + alpine.Package:
		return hostfake.Response{Stdout: added}
	case "apk add " + alpine.Package:
		if h.apkFails {
			return hostfake.Response{Stderr: "ERROR: unable to select packages\n", ExitCode: 1}
		}
		h.apkCron = true
		h.AddFile(alpine.Script, []byte("#!/bin/sh\n\napk upgrade --update --quiet\n"), 0o755, 0, 0)
		return hostfake.Response{Stdout: added}
	case "apk del --simulate " + alpine.Package:
		return hostfake.Response{Stdout: purged}
	case "apk del " + alpine.Package:
		h.apkCron = false
		_ = h.Remove(ctx, alpine.Script)
		return hostfake.Response{Stdout: purged}
	case "rc-service crond status":
		if h.crond {
			return hostfake.Response{Stdout: " * status: started\n"}
		}
		return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}
	case "rc-service crond start":
		h.crond = true
		return hostfake.Response{}
	case "rc-service crond stop":
		h.crond = false
		return hostfake.Response{}
	case "rc-update add crond default":
		h.AddSymlink(alpine.RunlevelLink, alpine.InitScript)
		return hostfake.Response{}
	case "rc-update del crond default":
		_ = h.Remove(ctx, alpine.RunlevelLink)
		return hostfake.Response{}
	}
	return h.dnsHost.answer(argv, stdin)
}

// TestUpdatesHook: withUpdatesScreen sets the Auto Updates item's screen
// on the configuration main gives it and hardn's version: Enable applies,
// apk-cron installed and crond started, the control's record carrying
// that version, hardn.yml not saved; at the next visit Disable applies, saves
// enableUnattendedUpgrades false through the control's plan, its backup
// under /var/backups/hardn, and the main menu's configuration adopts the
// save (screenkit.Adopt), so that the next save is not refused
// CONFIG_CHANGED; 0 goes back (nil). The signals are registered for each
// apply alone, and never while a prompt waits. No audit record is
// written.
func TestUpdatesHook(t *testing.T) {
	ctx := context.Background()
	h, cfg := newUpdatesHost(t)
	s := useDNSHost(t, h.dnsHost, "1", "y", "0", "1", "y", "0")
	yml := h.content(dnsTestYML)
	engineHost = func() host.System { return h }
	set := withUpdatesScreen(mainmenu.Options{}, cfg, updatesTestVersion)
	if set.Updates == nil {
		t.Fatal("withUpdatesScreen did not set the Auto Updates item on the menu given")
	}
	if err := set.Updates(); err != nil {
		t.Fatalf("the Auto Updates item, Enable = %v; screen:\n%s", err, s.out.String())
	}
	if !h.apkCron || !h.crond || h.content(dnsTestYML) != yml {
		t.Errorf("after Enable: apk-cron installed %t, crond started %t; hardn.yml:\n%s", h.apkCron, h.crond, h.content(dnsTestYML))
	}
	rec, err := revert.ReadRecord(ctx, h, revert.Options{}, updatesctl.ID)
	if err != nil || rec == nil || len(rec.Entries) != 1 || rec.Entries[0].Hardn != updatesTestVersion {
		t.Errorf("the control's record after Enable: %+v, %v; want one entry carrying hardn %q", rec, err, updatesTestVersion)
	}
	if err := set.Updates(); err != nil {
		t.Fatalf("the Auto Updates item, Disable = %v; screen:\n%s", err, s.out.String())
	}
	out := style.StripAnsi(s.out.String())
	if first, _, _ := strings.Cut(out, "Enter your choice"); !strings.Contains(first, "apk-cron") || !strings.Contains(first, "crond") {
		t.Errorf("the first screen does not draw Alpine's rows from the facts detected:\n%s", first)
	}
	if w := inOrder(out, "Auto Updates", "Enable automatic updates", "Turn automatic updates on?", "Operation Successful",
		"Disable automatic updates", "Turn automatic updates off?", "Operation Successful"); w != "" {
		t.Errorf("the screen lacks %q:\n%s", w, out)
	}
	if h.apkCron || h.crond {
		t.Errorf("after Disable: apk-cron installed %t, crond started %t", h.apkCron, h.crond)
	}
	yml = h.content(dnsTestYML)
	if cfg.EnableUnattendedUpgrades || !strings.Contains(yml, "enableUnattendedUpgrades: false") || !cfg.LoadedFrom([]byte(yml)) {
		t.Errorf("the main menu's configuration says enableUnattendedUpgrades %t, loaded from hardn.yml %t:\n%s", cfg.EnableUnattendedUpgrades, cfg.LoadedFrom([]byte(yml)), yml)
	}
	if !strings.HasPrefix(yml, "# hardn's configuration") || !strings.Contains(yml, "nameservers:\n  - 192.0.2.53\n") {
		t.Errorf("hardn.yml's other lines not kept:\n%s", yml)
	}
	if s.applies != 2 || s.live != 0 || s.atPrompt != 0 {
		t.Errorf("signals registered %d times (want 2), %d left, %d prompts read under them", s.applies, s.live, s.atPrompt)
	}
	for _, w := range h.writes() {
		if strings.HasPrefix(w, "append ") {
			t.Errorf("an audit record was written: %s", w)
		}
	}
}

// TestUpdatesScreen: under plan only, as the configuration says when the
// item is chosen (set after the hook), Enable stops at its plan, nothing
// written, no signals registered; q quits (nav.ErrQuit), the end of the
// input aborts (prompt.ErrAborted).
func TestUpdatesScreen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dryRun bool
		lines  []string
		want   error
		said   string
	}{
		{"Enable under plan only", true, []string{"1", "0"}, nil, "No actual changes were made to your system."},
		{"quit", false, []string{"q"}, nav.ErrQuit, "Auto Updates"},
		{"the end of the input", false, nil, prompt.ErrAborted, "Auto Updates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newUpdatesHost(t)
			s := useDNSHost(t, h.dnsHost, tc.lines...)
			engineHost = func() host.System { return h }
			set := withUpdatesScreen(mainmenu.Options{}, cfg, updatesTestVersion)
			cfg.DryRun = tc.dryRun // as -n or hardn.yml leaves it before 6 is chosen
			err := set.Updates()
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("the Auto Updates item = %v, want %v; screen:\n%s", err, tc.want, s.out.String())
			}
			if out := style.StripAnsi(s.out.String()); !strings.Contains(out, tc.said) {
				t.Errorf("the screen does not say %q:\n%s", tc.said, out)
			}
			if got := h.writes(); len(got) > 0 || h.apkCron || h.crond || s.applies != 0 {
				t.Errorf("wrote %q, apk-cron %t, crond %t, %d applies", got, h.apkCron, h.crond, s.applies)
			}
			if !cfg.EnableUnattendedUpgrades {
				t.Error("the main menu's configuration says enableUnattendedUpgrades false")
			}
		})
	}
}

// TestUpdatesHookFails: an Enable whose install fails is rolled back, and
// its report names hardn's log (output.LegacyLogPath), as the hook gives
// it; nothing is installed or recorded.
func TestUpdatesHookFails(t *testing.T) {
	ctx := context.Background()
	h, cfg := newUpdatesHost(t)
	h.apkFails = true
	s := useDNSHost(t, h.dnsHost, "1", "y", "0")
	engineHost = func() host.System { return h }
	if err := runUpdatesScreen(ctx, cfg, updatesTestVersion); err != nil {
		t.Fatalf("runUpdatesScreen = %v; screen:\n%s", err, s.out.String())
	}
	out := style.StripAnsi(s.out.String())
	_, line, _ := strings.Cut(out, "Check the log file at ")
	line, _, _ = strings.Cut(line, "\n")
	if !strings.Contains(out, "# Operation Failed") || strings.TrimSpace(line) != output.LegacyLogPath+" for details" {
		t.Errorf("the failure's report does not name %s:\n%s", output.LegacyLogPath, out)
	}
	if rec, err := revert.ReadRecord(ctx, h, revert.Options{}, updatesctl.ID); err != nil || rec != nil || h.apkCron {
		t.Errorf("after the failure: record %+v, %v; apk-cron installed %t", rec, err, h.apkCron)
	}
}
