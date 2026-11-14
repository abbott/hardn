package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/confirm"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/spf13/pflag"
)

// The audit log (audit, output.go; docs/reference/output.md §4) on the
// recording fake. By default each test's record goes to a fake of its own,
// read by nothing, so that a test's host holds what the run did alone; the
// tests below write it on the run's own host (auditOnRunHost) and read it
// back from there.
func init() {
	auditHost = func(host.System) host.System {
		h := hostfake.New(host.Options{})
		h.AddDir("/var/log", 0o755, 0, 0)
		return h
	}
}

// auditTestVersion is hardn's version (Version) while an audit test runs, so
// that a record without it fails wantOneRecord.
const auditTestVersion = "0.4.0-audit-test"

// auditOnRunHost writes the audit log on the run's own host, as hardn
// does, for this test, hardn's version auditTestVersion.
func auditOnRunHost(t *testing.T) {
	t.Helper()
	was, version := auditHost, Version
	t.Cleanup(func() { auditHost, Version = was, version })
	auditHost = func(run host.System) host.System { return run }
	Version = auditTestVersion
}

// auditRecords are the audit log's records on h, one per line, each
// checked to be one whole JSON line.
func auditRecords(t *testing.T, h *hostfake.System) []output.Record {
	t.Helper()
	e, ok := h.Entry(output.AuditPath)
	if !ok {
		return nil
	}
	if e.Mode.Perm() != output.AuditFileMode || e.UID != 0 {
		t.Errorf("the audit log is %v, uid %d", e.Mode, e.UID)
	}
	var recs []output.Record
	sc := bufio.NewScanner(bytes.NewReader(e.Data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var rec output.Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("line %d of the audit log: %v", len(recs)+1, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

// wantOneRecord: h's audit log holds one record, of mode and outcome.
func wantOneRecord(t *testing.T, h *hostfake.System, mode output.Mode, outcome output.Outcome) output.Record {
	t.Helper()
	recs := auditRecords(t, h)
	if len(recs) != 1 {
		t.Fatalf("%d audit records, want 1: %+v", len(recs), recs)
	}
	rec := recs[0]
	if rec.Mode != mode || rec.Result.Outcome != outcome || rec.Time.IsZero() || rec.SchemaVersion != output.SchemaVersion || rec.HardnVersion != Version {
		t.Errorf("record: mode %q, outcome %q, time %v, schema %d, version %q; want %q, %q", rec.Mode, rec.Result.Outcome, rec.Time, rec.SchemaVersion, rec.HardnVersion, mode, outcome)
	}
	return rec
}

// TestAuditFlags: each operation flag's run is one record of the audit
// log, written on the host it ran on, whatever its outcome: -g applied
// (its hostname read), planned alone (-n), and refused; -c refused by its
// own guard; -w through the hostname typed (a lockout-risk
// change, awaiting its confirmation), and interrupted during its apply,
// hardn ending with 130 once the record is written.
func TestAuditFlags(t *testing.T) {
	t.Run("-g applied", func(t *testing.T) {
		auditOnRunHost(t)
		h, cfg := newDNSHost(t, "debian13", dnsTestList)
		h.AddDir("/proc/sys/kernel", 0o555, 0, 0)
		h.AddFile(header.HostnameFile, []byte("web-01\n"), 0o444, 0, 0)
		r, _ := runG(t, h, cfg, nil, "--yes", "-g")
		if r.ended {
			t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
		}
		rec := wantOneRecord(t, h.System, output.ModeApply, output.OutcomeApplied)
		if rec.Host.Hostname != "web-01" || len(rec.Changes) != 1 || rec.Changes[0].Status != output.StatusApplied {
			t.Errorf("record: host %q, changes %+v", rec.Host.Hostname, rec.Changes)
		}
		if d, _ := h.Entry("/var/log/hardn"); d.Mode.Perm() != output.AuditDirMode || d.UID != 0 {
			t.Errorf("the audit log's directory is %v, uid %d", d.Mode, d.UID)
		}
	})
	t.Run("-n -g", func(t *testing.T) {
		auditOnRunHost(t)
		h, cfg := newDNSHost(t, "debian13", dnsTestList)
		r, _ := runG(t, h, cfg, nil, "-n", "-g")
		if !r.ended || r.code != 3 {
			t.Fatalf("exit status %d (ended %v), want 3", r.code, r.ended)
		}
		wantOneRecord(t, h.System, output.ModePlan, output.OutcomeChangesNeeded)
	})
	t.Run("-g refused", func(t *testing.T) {
		auditOnRunHost(t)
		h, cfg := newDNSHost(t, "resolvconf", dnsTestList)
		r, _ := runG(t, h, cfg, nil, "--yes", "-g")
		if !r.ended || r.code != 1 {
			t.Fatalf("exit status %d (ended %v), want 1", r.code, r.ended)
		}
		rec := wantOneRecord(t, h.System, output.ModeApply, output.OutcomeRefused)
		if !strings.Contains(string(mustJSON(t, rec)), "DNS_MANAGED") {
			t.Errorf("the record lacks the refusal's code: %s", mustJSON(t, rec))
		}
	})
	t.Run("-c refused", func(t *testing.T) {
		auditOnRunHost(t)
		h, cfg := newUsersHost(t)
		was := createRefuse
		t.Cleanup(func() { createRefuse = was })
		r, _ := runC(t, h, cfg, func(*dnsSeams) {
			createRefuse = func(*engine.Plan) error { return refuseCreateLockoutRisk(&engine.Plan{Risk: engine.RiskLockout}) }
		}, "--yes", "-c", "-u", "carol")
		if !r.ended || r.code != 1 {
			t.Fatalf("exit status %d (ended %v), want 1", r.code, r.ended)
		}
		wantOneRecord(t, h.System, output.ModeApply, output.OutcomeRefused)
	})
	t.Run("-w, the hostname typed", func(t *testing.T) {
		auditOnRunHost(t)
		h := newFWHost(t, "debian13")
		r, _ := runW(t, h, onTTY, []string{"web-01"}, nil, "-w")
		if r.ended {
			t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
		}
		wantOneRecord(t, h.System, output.ModeApply, output.OutcomeAwaiting)
	})
	t.Run("-w interrupted", func(t *testing.T) {
		auditOnRunHost(t)
		h := newFWHost(t, "debian13")
		var cancel context.CancelFunc
		r, _ := runW(t, h, onTTY, []string{"web-01"}, func(s *dnsSeams) {
			engineSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
				c, cc := context.WithCancel(ctx)
				cancel = cc
				return c, cc
			}
			h.during = func() { cancel() }
		}, "-w")
		if !r.ended || r.code != 130 {
			t.Fatalf("exit status %d (ended %v), want 130", r.code, r.ended)
		}
		wantOneRecord(t, h.System, output.ModeApply, output.OutcomeCancelled)
	})
}

// TestAuditScreen: each change a screen plans is one record, once its flow
// has ended: Add nameserver on the DNS screen applied, declined, and as a
// dry run; a screen left before any plan writes none.
func TestAuditScreen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		lines   []string
		mode    output.Mode
		outcome output.Outcome // "": no record
	}{
		{"applied", false, []string{"2", "192.0.2.55", "y", "0"}, output.ModeApply, output.OutcomeApplied},
		{"declined", false, []string{"2", "192.0.2.55", "n", "0"}, output.ModeApply, output.OutcomeDeclined},
		{"a dry run", true, []string{"2", "192.0.2.55", "0"}, output.ModePlan, output.OutcomeChangesNeeded},
		{"nothing planned", false, []string{"0"}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auditOnRunHost(t)
			h, cfg := newDNSHost(t, "debian13", dnsTestList)
			s := useDNSHost(t, h, tc.lines...)
			cfg.DryRun = tc.dryRun
			if err := runDNSScreen(context.Background(), cfg); err != nil {
				t.Fatalf("runDNSScreen = %v; screen:\n%s", err, s.out.String())
			}
			if tc.outcome == "" {
				if recs := auditRecords(t, h.System); len(recs) != 0 {
					t.Errorf("%d audit records, want none", len(recs))
				}
				return
			}
			wantOneRecord(t, h.System, tc.mode, tc.outcome)
		})
	}
}

// TestAuditNotWritten: an audit log that cannot be written (its directory
// writable by others) is a warning on stderr with its code; the run goes
// on, applied, its exit status its own.
func TestAuditNotWritten(t *testing.T) {
	auditOnRunHost(t)
	h, cfg := newDNSHost(t, "debian13", dnsTestList)
	h.AddDir("/var/log/hardn", 0o777, 0, 0)
	was := h.content(hostdns.ResolvConf)
	r, s := runG(t, h, cfg, nil, "--yes", "-g")
	if r.ended {
		t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
	}
	if h.content(hostdns.ResolvConf) == was || !strings.Contains(s.out.String(), "Operation Successful") {
		t.Errorf("not applied:\n%s", s.out.String())
	}
	if !strings.Contains(r.stderr, "This run is not in the audit log (AUDIT_DIR_UNSAFE)") {
		t.Errorf("stderr lacks the warning:\n%s", r.stderr)
	}
	if _, ok := h.Entry(output.AuditPath); ok {
		t.Error("the audit log was written in a directory others can write")
	}
}

// TestAuditNotRotated: an audit log that cannot be rotated, here a copy
// that is a symlink, is one warning; the run is applied and its record
// written to the log as it is.
func TestAuditNotRotated(t *testing.T) {
	auditOnRunHost(t)
	h, cfg := newDNSHost(t, "debian13", dnsTestList)
	h.AddDir("/var/log/hardn", output.AuditDirMode, 0, 0)
	h.AddFile(output.AuditPath, bytes.Repeat([]byte("x"), output.AuditMaxSize+1), output.AuditFileMode, 0, 0)
	h.AddSymlink(output.AuditPath+".1", "/etc/shadow")
	r, s := runG(t, h, cfg, nil, "--yes", "-g")
	if r.ended || !strings.Contains(s.out.String(), "Operation Successful") {
		t.Fatalf("ended %v with %d; stderr:\n%s", r.ended, r.code, r.stderr)
	}
	said := flat(r.stderr)
	if !strings.Contains(said, output.AuditPath+" was not rotated: "+output.AuditPath+".1: refusing a symlink") || strings.Count(said, output.AuditPath) != 2 || strings.Count(said, "Warning") != 1 || strings.Contains(said, "not in the audit log") {
		t.Errorf("stderr:\n%s", r.stderr)
	}
	if e, _ := h.Entry(output.AuditPath); len(e.Data) <= output.AuditMaxSize+1 || e.Data[len(e.Data)-1] != '\n' {
		t.Errorf("the record was not appended: %d bytes", len(e.Data))
	}
}

// TestAuditNotBeforeAnOperation: --help, --version and the non-root
// refusal run no operation, so nothing reaches the audit log. A usage
// error ends hardn in flag parsing, before any of them (TestUsageErrors).
func TestAuditNotBeforeAnOperation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		nonRoot bool
	}{
		{"--help", []string{"--help"}, false},
		{"--version", []string{"--version"}, false},
		{"not root", []string{"--yes", "-g"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			was := auditHost
			rootCmd.SetOut(io.Discard)
			t.Cleanup(func() {
				auditHost = was
				rootCmd.SetOut(nil)
				for _, fs := range []*pflag.FlagSet{rootCmd.Flags(), rootCmd.PersistentFlags()} {
					if f := fs.Lookup("help"); f != nil { // cobra's, which resetFlags leaves given
						_ = f.Value.Set("false")
						f.Changed = false
					}
				}
			})
			auditHost = func(host.System) host.System {
				t.Error("the audit log was reached")
				return hostfake.New(host.Options{})
			}
			runHardnWith(t, asRoot, func() {
				if tc.nonRoot {
					lookupCurrentUser = func() (*user.User, error) { return &user.User{Uid: "1000", Username: "alice"}, nil }
				}
			}, tc.args...)
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAuditFlagFill: a flag's record carries what audit fills in where the
// run has none: hardn's version (wantOneRecord), and hardn.yml's
// deprecations, each a finding whose source is the file and line it was
// read from (configPath).
func TestAuditFlagFill(t *testing.T) {
	auditOnRunHost(t)
	h, _ := newDNSHost(t, "debian13", dnsTestList)
	yml := h.content(dnsTestYML) + "disableRoot: false\n" // disableRootSSH's old name: a deprecation
	h.AddFile(dnsTestYML, []byte(yml), 0o644, 0, 0)
	cfg, err := config.Read(context.Background(), h, dnsTestYML)
	if err != nil || len(cfg.Deprecations()) == 0 {
		t.Fatalf("config.Read = %v, deprecations %v", err, cfg.Deprecations())
	}
	r, _ := runG(t, h, cfg, nil, "--yes", "-g")
	if r.ended {
		t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
	}
	rec := wantOneRecord(t, h.System, output.ModeApply, output.OutcomeApplied)
	if !slices.ContainsFunc(rec.Findings, func(f output.Finding) bool {
		return f.Code == output.CodeDeprecated && strings.HasPrefix(f.Source, dnsTestYML+":")
	}) {
		t.Errorf("no %s finding from %s: %+v", output.CodeDeprecated, dnsTestYML, rec.Findings)
	}
}

// TestAuditTypedInterrupted: Ctrl-C at -w's typed hostname ends hardn by
// SIGINT (typedReraise, stubbed here to panic as hardn would end): its run
// is one record, written before the reraise, and none after it.
func TestAuditTypedInterrupted(t *testing.T) {
	auditOnRunHost(t)
	h := newFWHost(t, "debian13")
	// A SIGINT outside the prompt's own read must not end the test binary.
	guard := make(chan os.Signal, 64)
	signal.Notify(guard, os.Interrupt)
	t.Cleanup(func() { signal.Stop(guard) })
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	stop, sent := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-sent })
	go func() { // Ctrl-C, until the read ends
		defer close(sent)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		giveUp := time.After(10 * time.Second)
		for {
			select {
			case <-stop:
				return
			case <-giveUp:
				_, _ = pw.WriteString("0\n")
				return
			case <-tick.C:
				_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
			}
		}
	}()
	before := -1 // the records when hardn reraised; -1: it did not
	r, _ := runW(t, h, onTTY, nil, func(*dnsSeams) {
		in := prompt.New(pr, false)
		engineInput = func() *prompt.Reader { return in }
		was := typedReraise
		t.Cleanup(func() { typedReraise = was })
		typedReraise = func() {
			before = len(auditRecords(t, h.System))
			panic(exited(-2))
		}
	}, "-w")
	if !r.ended || r.code != -2 || before != 1 {
		t.Fatalf("ended %v with %d, want by the reraise; %d records at the reraise, want 1; stderr:\n%s", r.ended, r.code, before, r.stderr)
	}
	wantOneRecord(t, h.System, output.ModeApply, output.OutcomeCancelled)
}

// TestAuditScreens: a change on each screen but DNS's (TestAuditScreen),
// as a dry run, is one record of its plan, once its flow has ended
// (Options.Ran): User Management's Create, SSH Login's Disallow, the
// Firewall's Enable, Backup's turn off, Auto Updates' Enable, and Run
// All's Apply all.
func TestAuditScreens(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		run  func(t *testing.T) (*hostfake.System, error)
	}{
		{"users", func(t *testing.T) (*hostfake.System, error) {
			h, cfg := newUsersHost(t)
			useUsersHost(t, h, "2", "carol", "y", "n", "n", "0")
			cfg.DryRun = true
			return h.System, runUsersScreen(ctx, cfg)
		}},
		{"ssh", func(t *testing.T) (*hostfake.System, error) {
			h, cfg := newSSHHost(t, "debian13")
			useSSHHost(t, h, "1", "0")
			cfg.DryRun = true
			return h.System, runSSHScreen(ctx, cfg)
		}},
		{"firewall", func(t *testing.T) (*hostfake.System, error) {
			h := newFWHost(t, "debian13")
			useFWHost(t, h, "1", "0")
			cfg := config.DefaultConfig()
			cfg.DryRun = true
			return h.System, runFirewallScreen(ctx, cfg)
		}},
		{"backup", func(t *testing.T) (*hostfake.System, error) {
			h, cfg := newDNSHost(t, "debian13", dnsTestList)
			h.AddDir("/srv", 0o755, 0, 0)
			useDNSHost(t, h, "1", "0")
			cfg.DryRun = true
			return h.System, runBackupScreen(ctx, cfg)
		}},
		{"updates", func(t *testing.T) (*hostfake.System, error) {
			h, cfg := newUpdatesHost(t)
			useDNSHost(t, h.dnsHost, "1", "0")
			engineHost = func() host.System { return h }
			cfg.DryRun = true
			return h.System, runUpdatesScreen(ctx, cfg, Version)
		}},
		{"runall", func(t *testing.T) (*hostfake.System, error) {
			h, cfg := newRunAllHost(t, "debian13", runAllYML)
			useSSHHost(t, h, "1", "0")
			t.Setenv("SUDO_USER", "george")
			cfg.DryRun = true
			return h.System, runRunAllScreen(ctx, cfg, Version)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auditOnRunHost(t)
			h, err := tc.run(t)
			if err != nil {
				t.Fatalf("the screen = %v", err)
			}
			wantOneRecord(t, h, output.ModePlan, output.OutcomeChangesNeeded)
		})
	}
}

// TestAuditConfirmCleanup: hardn confirm's clean-up, once it runs from a new
// connection, is one record on the clean-up's host (confirmAndCleanUp), after
// the confirmation's own: applied, and refused; a console's confirmation,
// which runs none, writes the confirmation's alone.
func TestAuditConfirmCleanup(t *testing.T) {
	refusal := fmt.Errorf("%s: port [22]: %w", confirm.ControlID, steps.ErrNoWriteStrategy)
	for _, tc := range []struct {
		name    string
		outcome confirm.Outcome
		want    output.Outcome
	}{
		{"applied", confirm.Outcome{Plan: cleanupPlan(), Result: &engine.Result{ID: cleanupTestID, Applied: appliedAll()}}, output.OutcomeApplied},
		{"refused", confirm.Outcome{Plan: &engine.Plan{ID: cleanupTestID, Facts: cleanupPlan().Facts, Refused: []error{refusal},
			Items: []engine.Item{{Control: confirm.ControlID, Title: "SSH clean-up after the confirmation", Err: refusal}}},
			Err: fmt.Errorf("%w: %w", engine.ErrRefused, refusal)}, output.OutcomeRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auditOnRunHost(t)
			s := newCleanupSeams(t, func(confirm.Held) confirm.Outcome { return tc.outcome })
			s.f.AddDir("/var/log", 0o755, 0, 0)
			executeHardn(t, "confirm")
			if s.calls != 1 {
				t.Fatalf("clean-up called %d times:\n%s", s.calls, s.out.String())
			}
			recs := auditRecords(t, s.f)
			if len(recs) != 2 || recs[0].Mode != output.ModeConfirm || recs[0].Result.Outcome != output.OutcomeConfirmed ||
				recs[1].Mode != output.ModeApply || recs[1].Result.Outcome != tc.want || recs[1].Result.Revert != nil {
				t.Fatalf("records %s; want the confirmation's, then the clean-up's (%s)", mustJSON(t, recs), tc.want)
			}
		})
	}
	t.Run("a console", func(t *testing.T) {
		auditOnRunHost(t)
		s := newCleanupSeams(t, func(confirm.Held) confirm.Outcome {
			t.Error("the clean-up ran from a console")
			return confirm.Outcome{}
		})
		s.f.AddDir("/var/log", 0o755, 0, 0)
		revertProc(s.f, 3000, 1, "login")
		revertProc(s.f, 3980, 3000, "bash")
		executeHardn(t, "confirm")
		rec := wantOneRecord(t, s.f, output.ModeConfirm, output.OutcomeConfirmed)
		if rv := rec.Result.Revert; rv == nil || rv.Session != output.SessionConsole || rv.Client != "" {
			t.Errorf("the confirmation's record: %s", mustJSON(t, rec))
		}
	})
}

// TestAuditRunAllRefused: -r refused by its planner before any plan (a change
// pending) is one refused record, its code the refusal's, under -n too, where its
// mode is plan.
func TestAuditRunAllRefused(t *testing.T) {
	auditOnRunHost(t)
	h, cfg := newRunAllHost(t, "debian13", runAllYML)
	if r, s := runD(t, h, cfg, onTTY, []string{"web-01"}, nil, "-r"); r.ended || !h.armed() {
		t.Fatalf("the first -r: ended %v with %d, armed %t; report:\n%s\nstderr:\n%s", r.ended, r.code, h.armed(), s.out.String(), r.stderr)
	}
	for _, tc := range []struct {
		args []string
		mode output.Mode
	}{{[]string{"-r"}, output.ModeApply}, {[]string{"-n", "-r"}, output.ModePlan}} {
		before := len(auditRecords(t, h.System))
		r, _ := runD(t, h, cfg, onTTY, []string{"web-01"}, nil, tc.args...)
		recs := auditRecords(t, h.System)
		if !r.ended || r.code != 1 || len(recs) != before+1 {
			t.Fatalf("%q: ended %v with %d; %d records after %d, want one more", tc.args, r.ended, r.code, len(recs), before)
		}
		rec := recs[len(recs)-1]
		if rec.Mode != tc.mode || rec.Result.Outcome != output.OutcomeRefused || rec.HardnVersion != Version ||
			!strings.Contains(string(mustJSON(t, rec)), "CHANGE_PENDING") {
			t.Errorf("%q: record %s", tc.args, mustJSON(t, rec))
		}
	}
}

// revertRecord is the one audit record on s's host, of mode and outcome,
// with the change's id and who ran it: alice, over SSH from her new
// connection, unless the test says otherwise.
func revertRecord(t *testing.T, s *revertSeams, mode output.Mode, outcome output.Outcome, exit int) output.Record {
	t.Helper()
	rec := wantOneRecord(t, s.f, mode, outcome)
	if rec.Result.Revert == nil {
		t.Fatalf("no result.revert: %s", mustJSON(t, rec))
	}
	if rec.Result.ExitCode != exit || !rec.Time.Equal(revertNow()) || len(rec.Changes) != 0 || len(rec.Status) != 0 {
		t.Errorf("exit %d, want %d; time %v: %s", rec.Result.ExitCode, exit, rec.Time, mustJSON(t, rec))
	}
	return rec
}

// auditAsAlice writes the audit log on the fake of s, with hardn run by
// alice through sudo.
func auditAsAlice(t *testing.T, s *revertSeams) {
	t.Helper()
	auditOnRunHost(t)
	s.f.AddDir("/var/log", 0o755, 0, 0)
	revertGetenv = func(k string) string {
		if k == "SUDO_USER" {
			return "alice"
		}
		return ""
	}
}

// TestAuditRevert: hardn confirm, hardn rollback <id>, --abandon and --boot
// are each one record of the audit log (auditRevert; output.md §4.1), of a
// mode of their own, with who ran it, the change's id and how it ended,
// a refusal and a failure included; an id that is not one, -n and a user
// other than root write none.
func TestAuditRevert(t *testing.T) {
	wantWho := func(t *testing.T, rec output.Record, user, session, client string) {
		t.Helper()
		if rv := rec.Result.Revert; rv.User != user || rv.Session != session || rv.Client != client {
			t.Errorf("who: %q, %q, %q; want %q, %q, %q", rv.User, rv.Session, rv.Client, user, session, client)
		}
	}
	const summary = "SSH port 22 to 2230 on web-01.example.net"
	t.Run("confirm", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		auditAsAlice(t, s)
		executeHardn(t, "confirm")
		rec := revertRecord(t, s, output.ModeConfirm, output.OutcomeConfirmed, 0)
		wantWho(t, rec, "alice", output.SessionSSH, "192.0.2.10:50123")
		if rec.Result.ID != revertTestID || rec.Result.Revert.Summary != summary || len(rec.Errors) != 0 {
			t.Errorf("record: %s", mustJSON(t, rec))
		}
		// Nothing left to confirm: refused, with its code.
		executeHardn(t, "confirm")
		recs := auditRecords(t, s.f)
		if len(recs) != 2 {
			t.Fatalf("%d records, want 2", len(recs))
		}
		if r := recs[1]; r.Mode != output.ModeConfirm || r.Result.Outcome != output.OutcomeRefused || r.Result.ExitCode != 1 ||
			len(r.Errors) != 1 || r.Errors[0].Code != "NOTHING_PENDING" || r.Result.Revert.User != "alice" {
			t.Errorf("the second confirm's record: %s", mustJSON(t, r))
		}
	})
	t.Run("confirm, from the connection that applied", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		auditAsAlice(t, s)
		revertPID = func() int { return 3971 }
		revertProc(s.f, 3971, 3970, "hardn", "SSH_CONNECTION=192.0.2.10 50022 192.0.2.20 22")
		executeHardn(t, "confirm")
		rec := revertRecord(t, s, output.ModeConfirm, output.OutcomeRefused, 1)
		if s.exit != 1 || len(rec.Errors) != 1 || rec.Errors[0].Code == output.CodeUnclassified || rec.Result.ID != "" {
			t.Errorf("exit %d; record: %s", s.exit, mustJSON(t, rec))
		}
	})
	t.Run("rollback", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		auditAsAlice(t, s)
		executeHardn(t, "rollback", revertTestID)
		rec := revertRecord(t, s, output.ModeRollback, output.OutcomeUndone, 0)
		wantWho(t, rec, "alice", output.SessionSSH, "192.0.2.10:50123")
		if rv := rec.Result.Revert; rec.Result.ID != revertTestID || rv.Summary != summary || rv.Reason != string(revert.ReasonRequested) || rv.Resumed {
			t.Errorf("record: %s", mustJSON(t, rec))
		}
		// Again: nothing to do, and why. An id that is not one: no record.
		executeHardn(t, "rollback", revertTestID)
		executeHardn(t, "rollback", "../etc")
		recs := auditRecords(t, s.f)
		if len(recs) != 2 {
			t.Fatalf("%d records, want 2: %s", len(recs), mustJSON(t, recs))
		}
		if r := recs[1]; r.Mode != output.ModeRollback || r.Result.Outcome != output.OutcomeNothingToDo || r.Result.ID != revertTestID || r.Result.Revert.Detail == "" {
			t.Errorf("the second rollback's record: %s", mustJSON(t, r))
		}
	})
	t.Run("rollback, the timer's", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		auditOnRunHost(t)
		s.f.AddDir("/var/log", 0o755, 0, 0)
		revertPID = func() int { return 5000 }
		revertProc(s.f, 5000, 1, "hardn") // systemd runs it
		revertNow = func() time.Time { return revertTestNow.Add(time.Hour) }
		executeHardn(t, "rollback", revertTestID)
		rec := revertRecord(t, s, output.ModeRollback, output.OutcomeUndone, 0)
		wantWho(t, rec, "root", output.SessionUnknown, "")
		if rec.Result.Revert.Reason != string(revert.ReasonTimeout) {
			t.Errorf("reason %q, want timeout", rec.Result.Revert.Reason)
		}
	})
	t.Run("rollback that does not finish, then --abandon", func(t *testing.T) {
		s := newRevertSeamsWith(t, 0, unfinishable)
		auditAsAlice(t, s)
		s.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
		executeHardn(t, "rollback", revertTestID)
		rec := revertRecord(t, s, output.ModeRollback, output.OutcomeRollbackIncomplete, 1)
		want := []output.RevertPart{
			{Kind: "restore", Index: 1, Done: false, What: "/var/backups/hardn/" + revertTestID},
			{Kind: "undo", Index: 1, Done: true, What: "ufw delete allow 2230/tcp"},
			{Kind: "reload", Index: 1, Done: false, What: "ssh"},
		}
		if s.exit != 1 || len(rec.Errors) != 1 || rec.Errors[0].Code != "ROLLBACK_INCOMPLETE" || rec.Errors[0].Step != "rollback" || !slices.Equal(rec.Result.Revert.Parts, want) {
			t.Errorf("exit %d; record: %s", s.exit, mustJSON(t, rec))
		}
		// Another id: refused, nothing given up. An id that is not one, -n
		// and alice herself: no record.
		executeHardn(t, "rollback", "--abandon", "20261018T120000Z-0000aaaa")
		rollbackAbandon = false
		executeHardn(t, "rollback", "--abandon", "../etc")
		rollbackAbandon = false
		executeHardn(t, "-n", "rollback", "--abandon", revertTestID)
		rollbackAbandon, dryRun = false, false
		revertEUID = func() int { return 1000 }
		executeHardn(t, "rollback", "--abandon", revertTestID)
		rollbackAbandon = false
		revertEUID = func() int { return 0 }
		executeHardn(t, "rollback", "--abandon", revertTestID)
		recs := auditRecords(t, s.f)
		if len(recs) != 3 {
			t.Fatalf("%d records, want 3: %s", len(recs), mustJSON(t, recs))
		}
		if r := recs[1]; r.Mode != output.ModeAbandon || r.Result.Outcome != output.OutcomeRefused || r.Result.ExitCode != 1 || r.Result.ID != "20261018T120000Z-0000aaaa" ||
			len(r.Errors) != 1 || !strings.HasPrefix(r.Errors[0].Code, "ABANDON_") {
			t.Errorf("the refused --abandon's record: %s", mustJSON(t, r))
		}
		r := recs[2]
		if rv := r.Result.Revert; r.Mode != output.ModeAbandon || r.Result.Outcome != output.OutcomeAbandoned || r.Result.ExitCode != 0 || r.Result.ID != revertTestID ||
			rv.User != "alice" || rv.Kept != revert.DefaultDir+"/"+revert.AbandonedName(revertTestID) || !slices.Equal(rv.Parts, want) || rv.Summary != summary {
			t.Errorf("the --abandon's record: %s", mustJSON(t, r))
		}
	})
	t.Run("--boot", func(t *testing.T) {
		for _, tc := range []struct {
			name           string
			after          time.Duration
			setup          func(s *revertSeams)
			outcome        output.Outcome
			exit           int
			action, reason string
		}{
			{"re-armed", time.Minute, func(s *revertSeams) {
				s.f.Respond(hostfake.Response{}, "systemd-run", "--unit=hardn-revert-"+revertTestID, "--description=hardn: roll back change "+revertTestID+" unless confirmed",
					"--on-active=120s", "--timer-property=AccuracySec=1s", revertTestHardn, "rollback", revertTestID)
			}, output.OutcomeRearmed, 0, "BOOT_REARMED", "BOOT_IN_WINDOW"},
			{"left", time.Minute, func(*revertSeams) {
				revertExecutable = func() (string, error) { return "/usr/sbin/hardn", nil }
			}, output.OutcomeRefused, 1, "BOOT_LEFT", "BOOT_OTHER_BINARY"},
			{"rolled back", time.Hour, func(*revertSeams) {}, output.OutcomeUndone, 0, "BOOT_ROLLED_BACK", "BOOT_DEADLINE_PASSED"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := newRevertSeams(t, 0)
				auditOnRunHost(t)
				s.f.AddDir("/var/log", 0o755, 0, 0)
				rebootSeams(s, tc.after)
				tc.setup(s)
				executeHardn(t, "rollback", "--boot")
				rec := revertRecord(t, s, output.ModeBoot, tc.outcome, tc.exit)
				if rv := rec.Result.Revert; rv.BootAction != tc.action || rv.BootReason != tc.reason || rec.Result.ID != revertTestID || rv.Detail == "" || rv.User != "root" {
					t.Errorf("record: %s", mustJSON(t, rec))
				}
			})
		}
		t.Run("the rollback does not finish", func(t *testing.T) {
			s := newRevertSeamsWith(t, 0, unfinishable)
			auditOnRunHost(t)
			s.f.AddDir("/var/log", 0o755, 0, 0)
			s.f.Respond(hostfake.Response{Stdout: "Rule deleted\n"}, "ufw", "delete", "allow", "2230/tcp")
			rebootSeams(s, time.Hour)
			executeHardn(t, "rollback", "--boot")
			rec := revertRecord(t, s, output.ModeBoot, output.OutcomeRollbackIncomplete, 1)
			if len(rec.Errors) != 1 || rec.Errors[0].Code != "ROLLBACK_INCOMPLETE" || len(rec.Result.Revert.Parts) != 3 {
				t.Errorf("record: %s", mustJSON(t, rec))
			}
		})
	})
	t.Run("not written: a warning, the command's exit its own", func(t *testing.T) {
		s := newRevertSeams(t, 0)
		auditOnRunHost(t)
		s.f.AddDir("/var/log", 0o755, 0, 0)
		s.f.AddDir("/var/log/hardn", 0o777, 0, 0)
		out := s.output(t, "rollback", revertTestID)
		if s.exit != -1 || !strings.Contains(out, "This run is not in the audit log (AUDIT_DIR_UNSAFE)") || s.has(revert.DefaultDir+"/"+revert.PendingName) {
			t.Errorf("exit %d:\n%s", s.exit, out)
		}
	})
}
