package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/drift"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
)

var checkTestNow = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

// checkSeams are a run of hardn check on a fake: its host, with hardn's
// state directory's parent and sudo, and the facts the host reads as, which
// a test changes between runs as a person would change the host.
type checkSeams struct {
	h     *hostfake.System
	facts drift.Facts
	reads int
	euid  int
	// cancelled: a signal arrives before the host is read.
	cancelled bool
}

// checkFacts are a reading, every fact established: v its value or its one
// item.
func checkFacts(v string) drift.Facts {
	var fs drift.Facts
	for _, sp := range drift.Specs {
		f := drift.Fact{ID: sp.ID, Known: true}
		if sp.Set() {
			f.Items = []string{v}
		} else {
			f.Value = v
		}
		fs = append(fs, f)
	}
	return fs
}

func newCheckSeams() *checkSeams {
	h := hostfake.New(host.Options{})
	for _, d := range []string{"/var", "/var/lib", "/var/log", "/usr", "/usr/bin", "/proc", "/proc/sys", "/proc/sys/kernel"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile("/usr/bin/sudo", []byte("ELF"), 0o755, 0, 0)
	h.AddFile(header.HostnameFile, []byte("web-01\n"), 0o444, 0, 0)
	return &checkSeams{h: h, facts: checkFacts("x")}
}

// run is hardn with args on s's host, as root unless s.euid says
// otherwise, by george through sudo. The planner's host is never given:
// reaching it fails the test (runHardnWith's engineHost).
func (s *checkSeams) run(t *testing.T, args ...string) result {
	t.Helper()
	auditOnRunHost(t)
	return runHardnWith(t, hostUnreachable, func() {
		hostWas, readWas, euidWas, rhostWas, getenvWas, nowWas, signalsWas, envWas := checkHost, checkRead, revertEUID, revertHost, revertGetenv, revertNow, confirmSignals, stdoutEnv
		t.Cleanup(func() {
			checkHost, checkRead, revertEUID, revertHost, revertGetenv, revertNow, confirmSignals, stdoutEnv = hostWas, readWas, euidWas, rhostWas, getenvWas, nowWas, signalsWas, envWas
		})
		checkHost = func() host.System { return s.h }
		revertHost = func() host.System { return s.h }
		checkRead = func(ctx context.Context, _ host.System, _ detect.Facts) (drift.Facts, error) {
			s.reads++
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return slices.Clone(s.facts), nil
		}
		revertEUID = func() int { return s.euid }
		revertGetenv = func(k string) string {
			if k == "SUDO_USER" {
				return "george"
			}
			return ""
		}
		revertNow = func() time.Time { return checkTestNow }
		confirmSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(ctx)
			if s.cancelled {
				cancel()
			}
			return ctx, cancel
		}
		stdoutEnv = func() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80, Height: 24}) }
		engineDetect = func(context.Context, host.System) (detect.Facts, error) {
			return detect.Facts{Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes}}}, nil
		}
	}, args...)
}

// changes are the calls on s's host since call n that are no read.
func (s *checkSeams) changes(n int) []string {
	reads := []string{"run", "read", "read-nofollow", "scan-lines", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group"}
	var out []string
	for _, c := range s.h.Calls()[n:] {
		if !slices.Contains(reads, c.Op) {
			out = append(out, c.String())
		}
	}
	return out
}

// TestCheckNoBaseline: on a host where --accept never ran, hardn check
// exits 1 and says to run it, so a cron job cannot pass with nothing
// checked (decision 3); the host is not even read, and nothing is written.
func TestCheckNoBaseline(t *testing.T) {
	s := newCheckSeams()
	r := s.run(t, "check")
	if !r.ended || r.code != output.ExitError || r.stdout != "" || !strings.Contains(flat(r.stderr), "No baseline: run `sudo hardn check --accept`") {
		t.Errorf("code %d (ended %v), stdout %q, stderr:\n%s", r.code, r.ended, r.stdout, r.stderr)
	}
	if s.reads != 0 || len(s.changes(0)) != 0 || len(r.reached) != 0 || logOpened {
		t.Errorf("read %d times, changed %q, reached %q, log opened %v", s.reads, s.changes(0), r.reached, logOpened)
	}

	r = s.run(t, "check", "--output", "json")
	var d output.Document
	if err := json.Unmarshal([]byte(r.stdout), &d); err != nil {
		t.Fatalf("%v: %q", err, r.stdout)
	}
	if r.code != output.ExitError || d.Result.Outcome != output.OutcomeFailed || d.Result.ExitCode != output.ExitError ||
		len(d.Errors) != 1 || d.Errors[0].Code != "BASELINE_MISSING" || d.Check == nil || d.Check.Baseline != nil || d.Check.Differences == nil {
		t.Errorf("code %d: %s", r.code, r.stdout)
	}
	if len(s.changes(0)) != 0 {
		t.Errorf("changed %q", s.changes(0))
	}
}

// TestCheckAcceptThenCheck: --accept saves the baseline, root's and 0600,
// and writes one record of the audit log, with who and when; a check after
// it finds no difference, exits 0 and writes nothing at all: no file, no
// lock, no log, no audit record. Once the host changes, it exits 3 with a
// line for the difference, as text and as the JSON document; accepting
// again clears it.
func TestCheckAcceptThenCheck(t *testing.T) {
	s := newCheckSeams()
	r := s.run(t, "check", "--accept")
	if r.ended || !strings.Contains(flat(r.stdout), "Operation Successful Baseline saved: /var/lib/hardn/baseline.json") || r.stderr != "" {
		t.Fatalf("code %d (ended %v), stdout %q, stderr:\n%s", r.code, r.ended, r.stdout, r.stderr)
	}
	e, ok := s.h.Entry(drift.Path)
	if !ok || e.Mode.Perm() != 0o600 || e.UID != 0 || !e.Mode.IsRegular() {
		t.Fatalf("the baseline: %+v, %v", e, ok)
	}
	var b drift.Baseline
	if err := json.Unmarshal(e.Data, &b); err != nil || b.Format != drift.Format || b.User != "george" || !b.Saved.Equal(checkTestNow) || len(b.Facts) != len(drift.Specs) {
		t.Errorf("the baseline's content: %+v, %v", b, err)
	}
	rec := wantOneRecord(t, s.h, output.ModeCheck, output.OutcomeApplied)
	if rec.Check == nil || !rec.Check.Accepted || rec.Check.Baseline == nil || rec.Check.Baseline.User != "george" || !rec.Check.Baseline.Saved.Equal(checkTestNow) ||
		!rec.Time.Equal(checkTestNow) || rec.Host.Hostname != "web-01" || rec.Result.ExitCode != 0 {
		t.Errorf("the audit record: %s", mustJSON(t, rec))
	}
	var wrote []string
	for _, c := range s.changes(0) {
		if !strings.HasPrefix(c, "mkdir ") {
			wrote = append(wrote, strings.Fields(c)[0]+" "+strings.Fields(c)[1])
		}
	}
	if want := []string{"write-atomic " + drift.Path, "rotate " + output.AuditPath, "append " + output.AuditPath}; !slices.Equal(wrote, want) {
		t.Errorf("--accept wrote %q, want %q", wrote, want)
	}

	n := len(s.h.Calls())
	r = s.run(t, "check")
	if r.ended || !strings.Contains(r.stdout, "No difference from the baseline of 2026-11-13 10:00 UTC.") || r.stderr != "" {
		t.Errorf("no difference: code %d (ended %v), stdout %q, stderr:\n%s", r.code, r.ended, r.stdout, r.stderr)
	}
	if got := s.changes(n); len(got) != 0 || logOpened || len(auditRecords(t, s.h)) != 1 {
		t.Errorf("a plain check changed %q; log opened %v; %d audit records", got, logOpened, len(auditRecords(t, s.h)))
	}

	s.facts[5].Value = "yes" // ssh.passwordLogin
	s.facts[12].Items = []string{"allow in 8080/tcp", "x"}
	n = len(s.h.Calls())
	r = s.run(t, "check")
	out := flat(r.stdout)
	if !r.ended || r.code != output.ExitChanges || !strings.Contains(out, "2 differences from the baseline of 2026-11-13 10:00 UTC") ||
		!strings.Contains(out, "SSH password login: was x, is yes") || !strings.Contains(out, "Firewall rule added: allow in 8080/tcp") ||
		!strings.Contains(out, "Run `sudo hardn check --accept` to keep the host as it is now.") {
		t.Errorf("differences: code %d (ended %v), stdout:\n%s\nstderr:\n%s", r.code, r.ended, r.stdout, r.stderr)
	}
	r = s.run(t, "check", "--output", "json")
	var d output.Document
	if err := json.Unmarshal([]byte(r.stdout), &d); err != nil {
		t.Fatalf("%v: %q", err, r.stdout)
	}
	if r.code != output.ExitChanges || d.Mode != output.ModeCheck || d.Result.Outcome != output.OutcomeChangesNeeded || d.Result.ExitCode != output.ExitChanges ||
		len(d.Check.Differences) != 2 || d.Check.Differences[0].Fact != drift.PasswordLogin || *d.Check.Differences[0].Was != "x" || *d.Check.Differences[0].Is != "yes" ||
		d.Check.Differences[1].Change != "added" || d.Check.Baseline.User != "george" || d.Host.Hostname != "web-01" || r.stderr != "" {
		t.Errorf("the document: code %d: %s", r.code, r.stdout)
	}
	if got := s.changes(n); len(got) != 0 || len(auditRecords(t, s.h)) != 1 {
		t.Errorf("checks with a difference changed %q; %d audit records", got, len(auditRecords(t, s.h)))
	}

	if r = s.run(t, "check", "--accept", "--output", "json"); r.ended {
		t.Fatalf("accepting again: code %d: %s", r.code, r.stderr)
	}
	if err := json.Unmarshal([]byte(r.stdout), &d); err != nil || d.Result.Outcome != output.OutcomeApplied || !d.Check.Accepted || len(d.Check.Differences) != 0 {
		t.Errorf("accepted again: %v: %s", err, r.stdout)
	}
	if r = s.run(t, "check"); r.ended || len(auditRecords(t, s.h)) != 2 {
		t.Errorf("after accepting again: code %d, %d audit records", r.code, len(auditRecords(t, s.h)))
	}
}

// TestCheckNotCompared: a fact not compared is said after the result, and
// is no difference.
func TestCheckNotCompared(t *testing.T) {
	s := newCheckSeams()
	if r := s.run(t, "check", "--accept"); r.ended {
		t.Fatal(r.stderr)
	}
	s.facts[13] = drift.Fact{ID: drift.Ports, Why: drift.NoSS, NotCompared: true}
	r := s.run(t, "check")
	if r.ended || !strings.Contains(r.stdout, "No difference") || !strings.Contains(r.stdout, "Listening ports not compared: ss is not installed") {
		t.Errorf("code %d, stdout:\n%s", r.code, r.stdout)
	}
	r = s.run(t, "check", "--output", "json")
	var d output.Document
	if err := json.Unmarshal([]byte(r.stdout), &d); err != nil || len(d.Findings) != 1 || d.Findings[0].Code != output.CodeNotCompared || d.Findings[0].Source != drift.Ports {
		t.Errorf("%v: %s", err, r.stdout)
	}
}

// TestCheckOlderBaseline: a baseline saved before the NTP servers were
// read is compared in the facts it has, and exits as if they matched; the
// report and the document say the NTP servers are not in it yet, and
// --accept saves every fact.
func TestCheckOlderBaseline(t *testing.T) {
	s := newCheckSeams()
	if r := s.run(t, "check", "--accept"); r.ended {
		t.Fatal(r.stderr)
	}
	e, _ := s.h.Entry(drift.Path)
	var b drift.Baseline
	if err := json.Unmarshal(e.Data, &b); err != nil {
		t.Fatal(err)
	}
	b.Facts = b.Facts[:drift.MinFacts]
	s.h.AddFile(drift.Path, mustJSON(t, b), 0o600, 0, 0)
	s.facts[len(s.facts)-1].Items = []string{"192.0.2.10 by hardn"}

	r := s.run(t, "check")
	if r.ended || !strings.Contains(r.stdout, "No difference") || !strings.Contains(flat(r.stdout), "NTP servers: not in the baseline yet; `sudo hardn check --accept` adds it.") {
		t.Errorf("code %d, stdout:\n%s", r.code, r.stdout)
	}
	r = s.run(t, "check", "--output", "json")
	var d output.Document
	if err := json.Unmarshal([]byte(r.stdout), &d); err != nil || r.code != 0 || d.Result.Outcome != output.OutcomeNothingToDo || len(d.Check.Differences) != 0 ||
		len(d.Findings) != 1 || d.Findings[0].Code != output.CodeNotCompared || d.Findings[0].Source != drift.TimeServers || d.Findings[0].Detail != drift.NotInBaseline {
		t.Errorf("%v: code %d: %s", err, r.code, r.stdout)
	}
	s.facts[5].Value = "yes" // ssh.passwordLogin
	if r = s.run(t, "check"); r.code != output.ExitChanges || !strings.Contains(flat(r.stdout), "1 difference") {
		t.Errorf("a difference: code %d, stdout:\n%s", r.code, r.stdout)
	}

	if r = s.run(t, "check", "--accept", "--output", "json"); r.ended || strings.Contains(r.stdout, drift.TimeServers) {
		t.Fatalf("accepted: code %d: %s", r.code, r.stdout)
	}
	e, _ = s.h.Entry(drift.Path)
	if err := json.Unmarshal(e.Data, &b); err != nil || len(b.Facts) != len(drift.Specs) || b.Facts[len(b.Facts)-1].ID != drift.TimeServers {
		t.Errorf("--accept saved %d facts: %v", len(b.Facts), err)
	}
	if r = s.run(t, "check"); r.ended || strings.Contains(r.stdout, "not in the baseline") {
		t.Errorf("after --accept: code %d, stdout:\n%s", r.code, r.stdout)
	}
}

// TestCheckNotRoot: a user other than root is refused, exit status 1,
// before the host or the baseline is looked at (decision 9), for --accept
// too, which then writes no record.
func TestCheckNotRoot(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "--accept"}, {"check", "--output", "json"}} {
		s := newCheckSeams()
		s.euid = 1000
		r := s.run(t, args...)
		if !r.ended || r.code != output.ExitError || r.stdout != "" || !strings.Contains(r.stderr, "needs to be run as root") {
			t.Errorf("%q: code %d (ended %v), stdout %q, stderr:\n%s", args, r.code, r.ended, r.stdout, r.stderr)
		}
		if s.reads != 0 || len(s.changes(0)) != 0 || len(auditRecords(t, s.h)) != 0 {
			t.Errorf("%q: read %d times, changed %q", args, s.reads, s.changes(0))
		}
		if _, ok := s.h.Entry(drift.Path); ok {
			t.Errorf("%q: a baseline was written", args)
		}
	}
}

// TestCheckFlags: hardn check with an operation flag, with -n, or with an
// --output other than json is a usage error, exit status 2, before the
// root check; nothing is read.
func TestCheckFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-n", "check"}, "hardn: -n is refused: hardn check has no dry run"},
		{[]string{"check", "--dry-run", "--accept"}, "hardn: -n is refused: hardn check has no dry run"},
		{[]string{"check", "-r"}, "hardn: -r is refused: run it without check"},
		{[]string{"-c", "check"}, "hardn: -c is refused: run it without check"},
		{[]string{"check", "-d", "--accept"}, "hardn: -d is refused: run it without check"},
		{[]string{"check", "-w"}, "hardn: -w is refused: run it without check"},
		{[]string{"check", "-g"}, "hardn: -g is refused: run it without check"},
		{[]string{"check", "-p"}, "hardn: -p is refused: run it without check"},
		{[]string{"check", "--output", "yaml"}, "hardn: --output yaml is refused: use --output json"},
		{[]string{"check", "--accept", "--output", "markdown"}, "hardn: --output markdown is refused: use --output json"},
	} {
		s := newCheckSeams()
		s.euid = 1000 // the refusal comes before the root check
		r := s.run(t, tc.args...)
		if !r.ended || r.code != output.ExitUsage || r.stdout != "" || !strings.Contains(r.stderr, tc.want) {
			t.Errorf("%q: code %d (ended %v), stdout %q, stderr:\n%s", tc.args, r.code, r.ended, r.stdout, r.stderr)
		}
		if s.reads != 0 || len(s.h.Calls()) != 0 {
			t.Errorf("%q: the host was reached: %v", tc.args, s.h.Calls())
		}
	}
}

// TestCheckRefusesBaseline: a baseline that is not root's alone, or is not
// one hardn wrote, is refused, exit status 1, and one of another format is
// said to be so: none is compared, so none reads as drift or as a pass.
func TestCheckRefusesBaseline(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(h *hostfake.System, data []byte)
		code  string
		words string
	}{
		{"planted by another user", func(h *hostfake.System, data []byte) { h.AddFile(drift.Path, data, 0o600, 1000, 1000) },
			"BASELINE_UNSAFE", "The baseline isn't root's alone, so it isn't read: /var/lib/hardn/baseline.json"},
		{"writable by others", func(h *hostfake.System, data []byte) { h.AddFile(drift.Path, data, 0o666, 0, 0) },
			"BASELINE_UNSAFE", "The baseline isn't root's alone"},
		{"a symlink", func(h *hostfake.System, data []byte) {
			h.AddFile("/var/lib/other.json", data, 0o600, 0, 0)
			h.AddSymlink(drift.Path, "/var/lib/other.json")
		}, "BASELINE_UNSAFE", "The baseline isn't root's alone"},
		{"not hardn's", func(h *hostfake.System, _ []byte) { h.AddFile(drift.Path, []byte("{}\n"), 0o600, 0, 0) },
			"BASELINE_INVALID", "The baseline isn't one hardn wrote, so it isn't read: /var/lib/hardn/baseline.json"},
		{"another format", func(h *hostfake.System, data []byte) {
			h.AddFile(drift.Path, []byte(strings.Replace(string(data), `"format": 1`, `"format": 2`, 1)), 0o600, 0, 0)
		}, "BASELINE_OTHER_FORMAT", "The baseline is from another version of hardn, so it isn't compared. Run `sudo hardn check --accept` to save a new one."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCheckSeams()
			if r := s.run(t, "check", "--accept"); r.ended {
				t.Fatal(r.stderr)
			}
			e, _ := s.h.Entry(drift.Path)
			tc.plant(s.h, e.Data)
			s.facts[5].Value = "yes" // a difference, were it compared
			s.reads = 0
			n := len(s.h.Calls())
			r := s.run(t, "check")
			if !r.ended || r.code != output.ExitError || r.stdout != "" || !strings.Contains(flat(r.stderr), tc.words) {
				t.Errorf("code %d (ended %v), stdout %q, stderr:\n%s", r.code, r.ended, r.stdout, r.stderr)
			}
			if s.reads != 0 || len(s.changes(n)) != 0 {
				t.Errorf("read %d times, changed %q", s.reads, s.changes(n))
			}
			r = s.run(t, "check", "--output", "json")
			var d output.Document
			if err := json.Unmarshal([]byte(r.stdout), &d); err != nil {
				t.Fatalf("%v: %q", err, r.stdout)
			}
			if r.code != output.ExitError || len(d.Errors) != 1 || d.Errors[0].Code != tc.code || len(d.Check.Differences) != 0 || d.Result.Outcome != output.OutcomeFailed {
				t.Errorf("code %d: %s", r.code, r.stdout)
			}
		})
	}
}

// TestCheckInterrupted: a signal while the host is read ends hardn check
// with 130, and --accept then saves nothing.
func TestCheckInterrupted(t *testing.T) {
	s := newCheckSeams()
	s.cancelled = true
	r := s.run(t, "check", "--accept")
	if !r.ended || r.code != output.ExitInterrupted {
		t.Errorf("code %d (ended %v), stderr:\n%s", r.code, r.ended, r.stderr)
	}
	if _, ok := s.h.Entry(drift.Path); ok {
		t.Error("a baseline was saved")
	}
}

// TestCheckHelp: hardn's help lists the command.
func TestCheckHelp(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"check"})
	if err != nil || c.Name() != "check" || c.Flags().Lookup("accept") == nil || c.Flags().Lookup("output") == nil {
		t.Fatalf("%v, %v", c, err)
	}
}

// TestBaselineReminderOnFlag: after -g applies a change on a host with a
// baseline, its report says to accept the host again (decision 2); on a
// host with none, it does not.
func TestBaselineReminderOnFlag(t *testing.T) {
	for _, kept := range []bool{true, false} {
		h, cfg := newDNSHost(t, "debian13", dnsTestList)
		if kept {
			h.AddDir("/var/lib/hardn", 0o700, 0, 0)
			h.AddFile(drift.Path, []byte("{}\n"), 0o600, 0, 0)
		}
		r, s := runG(t, h, cfg, nil, "--yes", "-g")
		if r.ended {
			t.Fatalf("ended with %d; stderr:\n%s", r.code, r.stderr)
		}
		const words = "check --accept` to keep this as the baseline."
		out := s.out.String()
		if got := strings.Contains(flat(out), words); got != kept || !strings.Contains(out, "Operation Successful") {
			t.Errorf("baseline %v: the reminder %v in:\n%s", kept, got, out)
		}
	}
}

// TestCheckNeverRotates: hardn check, which writes nothing, rotates
// neither log, each over its size: hardn's log is not opened, and no
// rotation, rename or append reaches the audit log. hardn check --accept,
// which writes its record, rotates the audit log first, and still leaves
// hardn's log alone (docs/engine/design-log-rotation.md, decision 6).
func TestCheckNeverRotates(t *testing.T) {
	s := newCheckSeams()
	if r := s.run(t, "check", "--accept"); r.ended {
		t.Fatalf("--accept: code %d: %s", r.code, r.stderr)
	}
	full := bytes.Repeat([]byte("x"), output.AuditMaxSize+1)
	s.h.AddFile(output.AuditPath, full, output.AuditFileMode, 0, 0)
	log := filepath.Join(t.TempDir(), "hardn.log")
	sized(t, log, output.LogMaxSize+1)
	pathWas, openedWas := startLogPath, logOpened
	t.Cleanup(func() { startLogPath, logOpened = pathWas, openedWas })
	startLogPath, logOpened = log, false
	untouched := func(when string) {
		t.Helper()
		if fi, err := os.Lstat(log); err != nil || fi.Size() != output.LogMaxSize+1 || logOpened {
			t.Errorf("%s: hardn's log: %v, %v, opened %v", when, fi, err, logOpened)
		}
		if _, err := os.Lstat(log + ".1"); err == nil {
			t.Errorf("%s: hardn's log was rotated", when)
		}
	}

	for _, args := range [][]string{{"check"}, {"check", "--output", "json"}} {
		n := len(s.h.Calls())
		if r := s.run(t, args...); r.ended {
			t.Fatalf("%v: code %d: %s", args, r.code, r.stderr)
		}
		if got := s.changes(n); len(got) != 0 {
			t.Errorf("%v changed %q", args, got)
		}
		if e, _ := s.h.Entry(output.AuditPath); len(e.Data) != len(full) {
			t.Errorf("%v: the audit log holds %d bytes", args, len(e.Data))
		}
		if _, ok := s.h.Entry(output.AuditPath + ".1"); ok {
			t.Errorf("%v rotated the audit log", args)
		}
		untouched(strings.Join(args, " "))
	}

	if r := s.run(t, "check", "--accept"); r.ended || r.stderr != "" {
		t.Fatalf("--accept: code %d: %s", r.code, r.stderr)
	}
	if e, _ := s.h.Entry(output.AuditPath + ".1"); len(e.Data) != len(full) {
		t.Errorf("--accept: audit.jsonl.1 holds %d bytes, want the full log", len(e.Data))
	}
	wantOneRecord(t, s.h, output.ModeCheck, output.OutcomeApplied)
	untouched("check --accept")
}
