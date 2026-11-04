package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/user"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
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

// auditOnRunHost writes the audit log on the run's own host, as hardn
// does, for this test.
func auditOnRunHost(t *testing.T) {
	t.Helper()
	was := auditHost
	t.Cleanup(func() { auditHost = was })
	auditHost = func(run host.System) host.System { return run }
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
// own guard; -e applied; -w through the hostname typed (a lockout-risk
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
	t.Run("-e applied", func(t *testing.T) {
		auditOnRunHost(t)
		h := newEnvHost(t, "debian13")
		r, _ := runE(t, h, nil, "--yes", "-e")
		if r.ended {
			t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
		}
		wantOneRecord(t, h.System, output.ModeApply, output.OutcomeApplied)
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
