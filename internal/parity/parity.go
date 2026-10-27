// Package parity is the parity harness (refactor plan §2: every slice's
// unit (b2)), shared since slice 3 (next/s3-shared; docs/engine/screen-kit.md
// §3): origin/main's end state, read from pkg/characterization's goldens
// (or a slice's frozen copy of them), against the new path's on hostfake,
// aspect by aspect, every difference held to a table of ties
// (docs/engine/parity-dns.md §2). Nothing in it is an area's own: a slice
// supplies a Slice (DNS's is pkg/screens/dns's parity_test.go) and runs it
// from its TestParity with Run.
//
// It is test support: import it only from _test.go files (depguard rule
// test-only). It links pkg/ui/uitest, which defines -update, so that make
// golden-update passes -update to every package whose tests use it.
package parity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
	_ "github.com/abbott/hardn/pkg/ui/uitest" // defines -update (updating)
)

// Slice is what a slice supplies: its rows, its distributions, its
// table of differences and the parts of the comparison that are its own.
type Slice struct {
	Scenarios []Scenario
	Distros   []Distro
	Table     []Tie
	Header    string // the golden's first lines
	Golden    string // the golden, under testdata/golden
	Page      string // the page whose region the test writes, from the package
	Pkg       string // the package of the slice's TestParity, from the module root
	Setting   string // the hardn.yml key the slice's items save; a scenario may name its own
	// LegacyRoot stands for the root in origin/main's goldens where the
	// characterization ran under a temp dir (Backup's, "$TMP"): a path
	// under it reads as the new path's absolute one, "$TMP/x" as "/x",
	// in the golden's trace, its after line and its saved hardn.yml. ""
	// for none: DNS's goldens name the host's own paths.
	LegacyRoot string
	// UnderBackupPath compares what each path wrote under the backup path
	// (EndState.Under and After), Backup's subject. DNS's leaves the new
	// path's backups out of the end state (NewState), as their own aspect.
	UnderBackupPath bool
	// V032Sections are the sections of v032-reference.md its scenarios'
	// V032 cite, which the page names: DNS's "§4.5, §4.13, §5.1, §6".
	V032Sections string
	// Legacy is the directory of origin/main's goldens, in
	// pkg/characterization's layout under its testdata/golden, from the
	// package; Fixtures is the directory of the hardn.yml fixtures they
	// loaded.
	Legacy, Fixtures string
	// Shipped says whether d's stock install has state; Exists is why
	// state cannot be on d, "" when it can (the cell is then compared).
	Shipped func(state HostState, d Distro) string
	Exists  func(state HostState, d Distro) string
	// Probes are origin/main's commands that only read: recorded, not
	// compared.
	Probes []string
	// Run is the new path on c.
	Run func(t *testing.T, c Cell) NewRun
	// Content adds the slice's aspects of the host's files both paths
	// wrote (what they set, line by line).
	Content func(add func(name, legacy, new string), l, n []FileWrite)
}

// Scenario is a row of the table, run on every distribution.
type Scenario struct {
	Name    string
	Golden  string    // origin/main's, under Slice.Legacy; {d} is the distribution
	State   HostState // the new path's host; zero: the stock install's
	Fixture string    // hardn.yml, under Slice.Fixtures; "": none loaded
	// Lines are a screen item's answers; nil for the control's plan and
	// apply (a CLI flag), which the screen's item that applies the
	// configuration as it is must match.
	Lines  []string
	DryRun bool
	Note   string // how origin/main's golden stands for this host, when it was not run on it
	V032   string // v0.3.2 where it differs, from v032-reference.md
	// Setting is the hardn.yml key this scenario's item saves, compared as
	// an aspect of its own and left out of the other settings and the rest
	// of the file; "" is Slice.Setting. Backup's toggle saves enableBackups,
	// its path item backupPath.
	Setting string
}

// HostState is a host the new path runs on, in words and in a word or two.
type HostState struct{ Long, Short string }

// Distro is one column of origin/main's matrix (pkg/characterization's
// osCells): the facts the new path's host takes, and its stock install's
// host state.
type Distro struct {
	Name, Label string
	Facts       detect.Facts
	Stock       HostState
}

// NewRun is the new path's run of a cell: its end state, how it was
// reached, and for a scenario with no lines what its dry run left.
type NewRun struct {
	Live EndState
	Via  string
	Dry  string
}

// Cell is a scenario on a distribution, as both paths left it.
type Cell struct {
	Scenario Scenario
	Distro   Distro
	State    HostState
	Shipped  string // whether the stock install has state
	Why      string // why state cannot exist on d; "" when compared
	legacy   legacyRun
	ran      NewRun
	diffs    []aspect
	agree    []aspect
}

// aspect is one compared aspect: origin/main's value and the new path's.
type aspect struct{ Name, Legacy, New string }

// Tie is an expected difference: every (scenario, distribution) it names
// differs in aspect, for the reason its source gives. No entry may name a
// difference that does not occur.
type Tie struct {
	Scenarios []string
	Distros   []string
	Aspect    string
	Source    string // V-numbers, decisions, proposals, plan rows, pages
	Why       string // one short line
}

// key names one difference.
type key struct{ Scenario, Distro, Aspect string }

// EndState is what a run left and said.
type EndState struct {
	Refusal  string      // what stopped it before any change; "" for nothing
	Files    []FileWrite // the host's files written, in order
	Dirs     []string    // directories made ("path mode"), modes set ("chmod path mode")
	Actions  []string    // commands that change something
	Probes   []string    // commands that only read: recorded, not compared
	Backups  []string    // files backed up before the change
	Journal  bool        // the change journalled before it ran
	Findings []string    // the plan's findings, by code
	YML      *FileWrite  // hardn.yml as saved; nil when not saved
	Loaded   []string    // hardn.yml as loaded, the fixture's lines; nil for none
	// Memory: the run's configuration was made in memory with Loaded's
	// settings, no file loaded (Backup's characterization), so a save
	// whole drops no comment.
	Memory bool
	// Under is what the run wrote under the backup path (UnderBackupPath):
	// "mkdir <dir> <mode>", "write <file> <mode>", "remove <file>", in
	// order, a backup run's directory named <run id>; content not compared.
	Under []string
	// After is what is at the backup path the run started with once it
	// ended: "<path>: <words>", in pkg/characterization's words.
	After string
}

// FileWrite is one file as a run left it.
type FileWrite struct {
	Path   string
	Mode   fs.FileMode
	Lines  []string
	Atomic bool // replaced whole by a rename; false: written in place
}

func (f FileWrite) String() string { return fmt.Sprintf("%s %04o", f.Path, uint32(f.Mode.Perm())) }

// Wrote reports whether the run wrote a file, the host's or hardn.yml.
func (s EndState) Wrote() bool { return len(s.Files) > 0 || s.YML != nil }

// legacyRun is origin/main's run, read from its characterization golden.
type legacyRun struct {
	Golden string   // relative to Slice.Legacy
	Vs     []string // the V-numbers its KNOWN-DEFECT and FIXED lines name
	Live   EndState // cfg.DryRun=false, or the menu item's result
	Dry    EndState // cfg.DryRun=true (ops goldens only)
	CLI    string   // hardn -n with the flag: what pkg/cmd's refusal says
	Start  string   // what the run started from, its "Start:" line (the repository's layout)
	Tree   string   // the temp dir afterwards (the repository's layout): recorded, not compared
}

// LegacyStart is what origin/main's run of c started from, as its golden
// says it ("Start:", in the repository's layout), its paths read as the
// new path's (Slice.LegacyRoot); "" for a golden that does not say. A
// slice holds its new path's host to it.
func (c Cell) LegacyStart() string { return c.legacy.Start }

var (
	traceLine   = regexp.MustCompile(`^\s+\d+ ([a-z]+)\s+(.*?)(?: -> (ok|error: .*))?$`)
	writeTarget = regexp.MustCompile(`^(\S+) mode=(\d{4})$`)
	defectLine  = regexp.MustCompile(`^# (?:KNOWN-DEFECT|FIXED [^:]*): `)
)

// named adds to vs every V-number l names, once each, when l is a
// KNOWN-DEFECT or FIXED line: DNS's goldens put it first ("KNOWN-DEFECT:
// V18 — …"), Backup's within the line ("… (side-effects.md item 9, V18;
// …)").
func named(vs []string, l string) []string {
	if !defectLine.MatchString(l) {
		return vs
	}
	for _, v := range vNumber.FindAllString(l, -1) {
		if !slices.Contains(vs, v) {
			vs = append(vs, v)
		}
	}
	return vs
}

// readLegacy is the characterization golden rel under dir, parsed
// (parseLegacy), its paths under root read as absolute; a golden it
// cannot read or parse fails the test.
func readLegacy(t *testing.T, dir string, probes []string, rel, root string) legacyRun {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("origin/main's golden: %v", err)
	}
	r, err := parseLegacy(data, probes, rel, root)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// parseLegacy parses data, the characterization golden rel: its
// V-numbers, each trace section into an end state, and its -n refusal. An
// ops golden has both cfg.DryRun sections; a menu item's has its result,
// whose message is its refusal when it saved nothing. What the parser does
// not compare (a removal, a failed command or write, a run that failed) is
// an error rather than being dropped, and so is a golden with neither
// cfg.DryRun=false nor a result, the run compared: one in a layout it does
// not know would otherwise read as a run that did nothing. A golden in the
// repository's layout (the Backup screen's, pkg/characterization's
// menu/backup_*) is read by parseRepository, its paths under root read as
// absolute.
func parseLegacy(data []byte, probes []string, rel, root string) (legacyRun, error) {
	if bytes.Contains(data, []byte("\n"+repoCalls+"\n")) {
		return parseRepository(data, rel, root)
	}
	r := legacyRun{Golden: rel}
	live := false
	var cur, result *EndState
	var last *FileWrite
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := sc.Text()
		r.Vs = named(r.Vs, l)
		switch {
		case l == "== cfg.DryRun=false":
			cur, last, live = &r.Live, nil, true
			continue
		case l == "== result":
			cur, result, last, live = &r.Live, &r.Live, nil, true
			continue
		case l == "== cfg.DryRun=true":
			cur, last = &r.Dry, nil
			continue
		case strings.HasPrefix(l, "== "):
			cur, last = nil, nil
			continue
		case strings.HasPrefix(l, "refused: "):
			r.CLI = strings.TrimPrefix(l, "refused: ")
		case strings.HasPrefix(l, "stderr: ") && r.CLI != "":
			r.CLI += ": " + strings.TrimPrefix(l, "stderr: ")
		}
		if cur == nil {
			continue
		}
		if c, ok := strings.CutPrefix(l, "        "); ok && last != nil {
			switch {
			case c == "|":
				last.Lines = append(last.Lines, "")
			case strings.HasPrefix(c, "| "):
				last.Lines = append(last.Lines, c[2:])
			}
			continue
		}
		if v, ok := strings.CutPrefix(l, "message: "); ok {
			cur.Refusal = v // kept only when nothing was saved, below
		}
		if v, ok := strings.CutPrefix(l, "result "); ok && !strings.HasSuffix(v, ": ok") {
			return r, fmt.Errorf("%s: a run that failed, which the harness does not compare: %q", rel, l)
		}
		if v, ok := strings.CutPrefix(l, "config.SaveConfig: "); ok && v != "ok" && v != "not called" {
			return r, fmt.Errorf("%s: a save that failed, which the harness does not compare: %q", rel, l)
		}
		m := traceLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		op, arg, res := m[1], m[2], m[3]
		if strings.HasPrefix(res, "error") && !(op == "exec" && slices.Contains(probes, arg)) && op != "read" && op != "stat" {
			return r, fmt.Errorf("%s: a change that failed, which the harness does not compare: %q", rel, l)
		}
		switch op {
		case "read", "stat": // reads leave no end state
		case "exec":
			if slices.Contains(probes, arg) {
				cur.Probes = append(cur.Probes, arg+" -> "+res)
			} else {
				cur.Actions = append(cur.Actions, arg)
			}
		case "mkdir":
			p, mode, _ := strings.Cut(arg, " mode=")
			cur.Dirs = append(cur.Dirs, p+" "+mode)
		case "write":
			w := writeTarget.FindStringSubmatch(arg)
			if w == nil {
				return r, fmt.Errorf("%s: a write the parser does not know: %q", rel, l)
			}
			var mode uint32
			if _, err := fmt.Sscanf(w[2], "%o", &mode); err != nil {
				return r, fmt.Errorf("%s: %q: %w", rel, l, err)
			}
			// In place: interfaces.OSFileSystem.WriteFile and SaveConfig are
			// os.WriteFile (side-effects.md §1 item 10), whichever the trace's
			// seam recorded.
			f := FileWrite{Path: w[1], Mode: fs.FileMode(mode)}
			if f.Path == "./hardn.yml" {
				cur.YML = &f
				last = cur.YML
			} else {
				cur.Files = append(cur.Files, f)
				last = &cur.Files[len(cur.Files)-1]
			}
		default:
			return r, fmt.Errorf("%s: an operation the harness does not compare (%s): %q", rel, op, l)
		}
	}
	if err := sc.Err(); err != nil {
		return r, fmt.Errorf("%s: %w", rel, err)
	}
	if !live {
		return r, fmt.Errorf("%s: no \"== cfg.DryRun=false\" or \"== result\" section, the run the harness compares: a layout it does not read", rel)
	}
	if result == nil || r.Live.YML != nil {
		r.Live.Refusal = "" // an item's message is its refusal only when it saved nothing
	}
	return r, nil
}

// The repository's layout (pkg/characterization's TestLegacyMenuBackup:
// menu/backup_*): a header of "# " lines, the screen as printed between
// two "# ----" lines, then the repository's file system calls, what is at
// the backup path after, the temp dir afterwards, and ./hardn.yml as
// written whole, or not.
const (
	repoCalls      = "# The repository's file system calls:"
	repoNoCalls    = "    (no calls)"
	repoNoNewline  = `        \ no newline at end of content`
	repoTree       = "# The temp dir afterwards:"
	repoYMLNone    = "# ./hardn.yml: not written"
	repoYMLWritten = "# ./hardn.yml as written:"
	repoRule       = "# ----"
)

// repoHeader are the header lines the repository's layout has, each the
// start of a line: the screen, its script, its pauses and notes are what
// was shown, not an end state; KNOWN-DEFECT names V-numbers; Start is
// kept.
var repoHeader = []string{"# origin/main behaviour", "# Generated by ", "# Screen: ", "# Script: ", "# Start: ", "# Pauses: ",
	"# KNOWN-DEFECT: ", "# FIXED ", "# NOTE: ", "# The screen as printed"}

var afterLine = regexp.MustCompile(`^# (\S+) after: (.*)$`)

// parseRepository parses data, the characterization golden rel in the
// repository's layout, its paths under root read as absolute: the
// V-numbers of its KNOWN-DEFECT and FIXED lines; Start; the repository's
// calls under the backup path (mkdir, write, remove) as Under, a mkdir or
// write that failed as the run's refusal, "failed: <error>" (a second
// failure, a failed remove or another operation is an error); the after
// line as After;
// ./hardn.yml as written, whole (its mode is not in the layout:
// config.SaveConfig's os.WriteFile(…, 0644), read: pkg/config/config.go:494);
// the configuration made in memory (Memory), as TestLegacyMenuBackup makes
// it. The screen is skipped, and the temp dir recorded, not compared. A
// line or an operation it does not know is an error, and so is a golden
// without the calls, the after line or the hardn.yml line, or with one of
// the trailer's sections twice.
func parseRepository(data []byte, rel, root string) (legacyRun, error) {
	r := legacyRun{Golden: rel, Live: EndState{Memory: true}}
	abs := func(s string) string {
		if root == "" {
			return s
		}
		return strings.ReplaceAll(s, root, "")
	}
	fail := func(format string, args ...any) (legacyRun, error) {
		return r, fmt.Errorf("%s: "+format, append([]any{rel}, args...)...)
	}
	const (
		header = iota
		screen
		trailer
		calls
		yml
	)
	part, seen := header, map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := sc.Text()
		switch part {
		case header:
			switch {
			case l == repoRule:
				part = screen
			case !slices.ContainsFunc(repoHeader, func(p string) bool { return strings.HasPrefix(l, p) }):
				return fail("a header line the reader does not know: %q", l)
			case strings.HasPrefix(l, "# Start: "):
				r.Start = abs(strings.TrimPrefix(l, "# Start: "))
			}
			r.Vs = named(r.Vs, l)
			continue
		case screen:
			if l == repoRule {
				part = trailer
			}
			continue
		case yml:
			r.Live.YML.Lines = append(r.Live.YML.Lines, abs(l))
			continue
		case calls:
			if c, ok := strings.CutPrefix(l, "        "); ok && (c == "|" || strings.HasPrefix(c, "| ") || l == repoNoNewline) {
				continue // a write's content, not compared
			}
			if l == repoNoCalls {
				continue
			}
			if m := traceLine.FindStringSubmatch(l); m != nil {
				op, arg, res := m[1], abs(m[2]), m[3]
				switch op {
				case "mkdir", "write":
					w := writeTarget.FindStringSubmatch(arg)
					if w == nil {
						return fail("a %s the parser does not know: %q", op, l)
					}
					if e, ok := strings.CutPrefix(res, "error: "); ok {
						if r.Live.Refusal != "" {
							return fail("a second call that failed, which the harness does not compare: %q", l)
						}
						r.Live.Refusal = "failed: " + abs(e)
						continue
					}
					r.Live.Under = append(r.Live.Under, op+" "+w[1]+" "+w[2])
				case "remove":
					if res != "ok" {
						return fail("a remove that failed, which the harness does not compare: %q", l)
					}
					r.Live.Under = append(r.Live.Under, op+" "+arg)
				default:
					return fail("an operation the harness does not compare (%s): %q", op, l)
				}
				continue
			}
			part = trailer
		}
		// trailer: each section once
		var section string
		m := afterLine.FindStringSubmatch(l)
		switch {
		case l == repoCalls:
			section, part = "calls", calls
		case m != nil:
			section, r.Live.After = "after", abs(m[1])+": "+m[2]
		case strings.HasPrefix(l, repoTree):
			section, r.Tree = "temp dir", strings.TrimSpace(strings.TrimPrefix(l, repoTree))
		case l == repoYMLNone:
			section = "hardn.yml"
		case l == repoYMLWritten:
			section, part = "hardn.yml", yml
			r.Live.YML = &FileWrite{Path: "./hardn.yml", Mode: 0o644}
		default:
			return fail("a line the reader does not know: %q", l)
		}
		if seen[section] {
			return fail("a second %s section: %q", section, l)
		}
		seen[section] = true
	}
	if err := sc.Err(); err != nil {
		return fail("%w", err)
	}
	for _, s := range []string{"calls", "after", "hardn.yml"} {
		if !seen[s] {
			return fail("no %s section in the repository's layout", s)
		}
	}
	return r, nil
}

// The new path's own directories: its run directory, the journal and the
// backups (engine.md), which are not the host's end state; the journal and
// the backups are recorded as such.
const (
	runDir     = "/run/hardn"
	journalDir = "/var/lib/hardn"
)

func under(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// NewState is the end state h's calls from the nth on left: the host's
// files and hardn.yml (at yml) as they are now, the directories, the
// commands (those in readOnly only read), the backups (the last
// manifest's entries) and whether the journal was written; refusal and
// findings are the caller's. What was written under backup.DefaultPath is
// the backups', left out. A change it does not compare fails the test.
func NewState(t *testing.T, h *hostfake.System, n int, readOnly []string, yml string) EndState {
	t.Helper()
	return newState(t, h, n, readOnly, yml, backup.DefaultPath, false)
}

// NewStateUnder is NewState with the backups under dir, the backup path
// a slice gives (Backup's: the one the plan's backups were taken under,
// engine.Plan.Backups, decision 7 (a)): what was written there is
// recorded in Under, for Slice.UnderBackupPath, each backup run's
// directory as <run id>.
func NewStateUnder(t *testing.T, h *hostfake.System, n int, readOnly []string, yml, dir string) EndState {
	t.Helper()
	s := newState(t, h, n, readOnly, yml, dir, true)
	if s.Under == nil {
		s.Under = []string{}
	}
	return s
}

// runID is a backup run's directory name (backup.NewRunID): its time and
// a random part.
var runID = regexp.MustCompile(`\b\d{8}T\d{6}Z-[0-9a-f]{8}\b`)

// newState is NewState with the backups under dir, recorded in Under when
// record is set.
func newState(t *testing.T, h *hostfake.System, n int, readOnly []string, yml, dir string, record bool) EndState {
	t.Helper()
	var s EndState
	var order []string
	modes := map[string]hostfake.Call{}
	var manifest []byte
	for _, c := range h.Calls()[n:] {
		switch {
		case under(c.Path, runDir):
			continue
		case under(c.Path, dir):
			if c.Op == "write-atomic" && path.Base(c.Path) == backup.ManifestName {
				manifest = c.Data
			}
			if record {
				s.Under = append(s.Under, underOp(t, c)...)
			}
			continue
		case under(c.Path, journalDir):
			s.Journal = s.Journal || c.Op == "write-atomic"
			continue
		}
		switch c.Op {
		case "read", "read-nofollow", "lstat", "stat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group", "lock", "unlock":
			// reads leave no end state
		case "write-atomic", "write-in-place":
			if _, ok := modes[c.Path]; !ok {
				order = append(order, c.Path)
			}
			modes[c.Path] = c
		case "mkdir":
			s.Dirs = append(s.Dirs, fmt.Sprintf("%s %04o", c.Path, uint32(c.Mode.Perm())))
		case "chmod":
			s.Dirs = append(s.Dirs, fmt.Sprintf("chmod %s %04o", c.Path, uint32(c.Mode.Perm())))
		case "run":
			argv := host.FormatArgv(c.Argv)
			switch {
			case !slices.Contains(readOnly, argv):
				s.Actions = append(s.Actions, argv)
			case !slices.Contains(s.Probes, argv):
				s.Probes = append(s.Probes, argv)
			}
		default: // remove, removeall, rename, chown, append, start-detached, ...
			t.Fatalf("a change the harness does not compare: %s", c)
		}
	}
	for _, p := range order {
		c := modes[p]
		e, ok := h.Entry(p)
		if !ok {
			t.Fatalf("%s written, then gone", p)
		}
		f := FileWrite{Path: p, Mode: c.Mode.Perm(), Lines: strings.Split(strings.TrimSuffix(string(e.Data), "\n"), "\n"), Atomic: c.Op == "write-atomic"}
		if p == yml {
			s.YML = &f
			continue
		}
		s.Files = append(s.Files, f)
	}
	if manifest != nil {
		var m struct {
			Entries []struct {
				Path, Kind  string
				TakenAnyway bool
			}
		}
		if err := json.Unmarshal(manifest, &m); err != nil {
			t.Fatal(err)
		}
		for _, e := range m.Entries {
			b := e.Path
			switch e.Kind {
			case string(backup.KindFile):
			case string(backup.KindAbsent):
				b += " (absent before: the rollback removes it)"
			default:
				b += " (" + e.Kind + ")"
			}
			if e.TakenAnyway {
				b += " (taken anyway: backups off)"
			}
			s.Backups = append(s.Backups, b)
		}
	}
	return s
}

// underOp is c, a call under the backup path, as Under records it: a
// directory made, a file written, a file removed, with their modes; a
// read is nothing. Another change fails the test.
func underOp(t *testing.T, c hostfake.Call) []string {
	t.Helper()
	p := runID.ReplaceAllString(c.Path, "<run id>")
	switch c.Op {
	case "read", "read-nofollow", "lstat", "stat", "readdir", "readlink", "resolve-symlink":
		return nil
	case "mkdir":
		return []string{fmt.Sprintf("mkdir %s %04o", p, uint32(c.Mode.Perm()))}
	case "write-atomic", "write-in-place":
		return []string{fmt.Sprintf("write %s %04o", p, uint32(c.Mode.Perm()))}
	case "remove":
		return []string{"remove " + p}
	}
	t.Fatalf("a change under the backup path the harness does not compare: %s", c)
	return nil
}

// Planned sets s's refusal and findings from p: the refusal's code, and
// the codes of its changes' findings.
func Planned(s EndState, p *engine.Plan) EndState {
	if err := p.Err(); err != nil {
		s.Refusal = output.CodeOf(err)
	}
	for _, c := range p.Changes {
		for _, f := range c.Findings {
			if !slices.Contains(s.Findings, f.Code) {
				s.Findings = append(s.Findings, f.Code)
			}
		}
	}
	return s
}

// Run is a slice's TestParity: each scenario on each distribution,
// origin/main's end state read from its golden and the new path's run,
// compared aspect by aspect. It fails on a difference sl.Table does not
// list (UNEXPLAINED), on an entry whose difference no longer occurs, and
// on an entry tied to a V-number origin/main's golden for the cell does
// not name; then holds the whole table to sl.Golden and its summary to
// sl.Page's region. The table, the golden and the page are checked only
// when every cell ran: a -run filter checks the cells it runs and no more.
func Run(t *testing.T, sl Slice) {
	t.Helper()
	var cells []Cell
	for _, sc := range sl.Scenarios {
		for _, d := range sl.Distros {
			t.Run(sc.Name+"/"+d.Name, func(t *testing.T) { cells = append(cells, runCell(t, sl, sc, d)) })
		}
	}
	if t.Failed() {
		return
	}
	if all := len(sl.Scenarios) * len(sl.Distros); len(cells) != all {
		t.Logf("%d of %d cells ran: the table, %s and the page are checked only when every cell runs", len(cells), all, sl.Golden)
		return
	}
	checkTable(t, sl, cells)
	if t.Failed() {
		return // neither rendering is written while the table fails
	}
	golden(t, sl.Golden, []byte(renderCells(sl, cells)))
	page(t, sl.Page, renderPage(sl, cells))
}

// runCell runs sc on d: origin/main's end state from its golden, the new
// path's, and their comparison.
func runCell(t *testing.T, sl Slice, sc Scenario, d Distro) Cell {
	t.Helper()
	c := Cell{Scenario: sc, Distro: d, State: sc.State}
	if c.State == (HostState{}) {
		c.State = d.Stock
	}
	c.Shipped = sl.Shipped(c.State, d)
	c.legacy = readLegacy(t, sl.Legacy, sl.Probes, strings.ReplaceAll(sc.Golden, "{d}", d.Name), sl.LegacyRoot)
	if c.Why = sl.Exists(c.State, d); c.Why != "" {
		return c
	}
	var loaded []string
	if sc.Fixture != "" {
		src, err := os.ReadFile(filepath.Join(sl.Fixtures, sc.Fixture))
		if err != nil {
			t.Fatal(err)
		}
		loaded = strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	}
	c.ran = sl.Run(t, c)
	c.legacy.Live.Loaded, c.ran.Live.Loaded = loaded, loaded
	key := sc.Setting
	if key == "" {
		key = sl.Setting
	}
	if err := unnamedSetting(key, c.legacy.Live, c.ran.Live); err != nil {
		t.Fatal(err)
	}
	c.diffs, c.agree = compare(t, sl, key, c.legacy.Live, c.ran.Live)
	if sc.Lines == nil {
		c.diffs = append(c.diffs, aspect{"dry run", legacyDry(c.legacy), c.ran.Dry})
	}
	return c
}

// unnamedSetting is an error when both paths saved hardn.yml and key, the
// setting the scenario's item saves (Scenario.Setting, else
// Slice.Setting), is empty: its aspect would compare no key.
func unnamedSetting(key string, l, n EndState) error {
	if key == "" && l.YML != nil && n.YML != nil {
		return errors.New("both paths saved hardn.yml, and the scenario names no setting (Scenario.Setting or Slice.Setting)")
	}
	return nil
}

// legacyDry is what origin/main's cfg.DryRun=true section did, and whether
// any path reaches it.
func legacyDry(r legacyRun) string {
	did := "the service writes " + Joined(names(r.Dry.Files)) + " and runs " + Joined(r.Dry.Actions)
	if HostText(r.Dry) == HostText(r.Live) {
		did = "the service writes and runs as live (cfg.DryRun unread below the menus)"
	}
	if r.CLI == "" {
		return did
	}
	status, _, _ := strings.Cut(r.CLI, ":")
	return did + "; no path reaches it: hardn -n refused (" + status + ")"
}

// HostText is what s changed: the files with their content, the
// directories, the commands and hardn.yml.
func HostText(s EndState) string {
	var b strings.Builder
	for _, f := range append(slices.Clone(s.Files), ymlOf(s)...) {
		fmt.Fprintf(&b, "%s\n%s\n", f, strings.Join(f.Lines, "\n"))
	}
	fmt.Fprintf(&b, "dirs %q\nactions %q\n", s.Dirs, s.Actions)
	return b.String()
}

func ymlOf(s EndState) []FileWrite {
	if s.YML == nil {
		return nil
	}
	return []FileWrite{*s.YML}
}

// compare is l and n, origin/main's end state and the new path's, aspect by
// aspect: the aspects that differ and those that agree, each with both
// values. An aspect of what was written is compared only when both wrote:
// the files' content (sl.Content) when both wrote a host file, whatever
// its path; hardn.yml's when both saved it, key, the setting the item
// saves, apart. With sl.UnderBackupPath, what each wrote under the backup
// path, its directories and its files, and what is there after.
func compare(t *testing.T, sl Slice, key string, l, n EndState) (diffs, agree []aspect) {
	t.Helper()
	add := func(name, lv, nv string) {
		if lv == nv {
			agree = append(agree, aspect{name, lv, nv})
		} else {
			diffs = append(diffs, aspect{name, lv, nv})
		}
	}
	add("refusal", OrElse(l.Refusal, "none"), OrElse(n.Refusal, "none"))
	add("files", Joined(names(l.Files)), Joined(names(n.Files)))
	if len(l.Files) > 0 && len(n.Files) > 0 {
		sl.Content(add, l.Files, n.Files)
		for _, lf := range l.Files {
			if i := slices.IndexFunc(n.Files, func(f FileWrite) bool { return f.Path == lf.Path }); i >= 0 {
				add("mode", fmt.Sprintf("%04o", uint32(lf.Mode)), fmt.Sprintf("%04o", uint32(n.Files[i].Mode)))
			}
		}
	}
	add("directories", Joined(l.Dirs), Joined(n.Dirs))
	add("commands", Joined(l.Actions), Joined(n.Actions))
	if sl.UnderBackupPath {
		ld, lf := dirsAndFiles(l.Under)
		nd, nf := dirsAndFiles(n.Under)
		add("backupPath dirs", Joined(ld), Joined(nd))
		add("backupPath files", Joined(lf), Joined(nf))
		add("backupPath after", OrElse(l.After, "not read"), OrElse(n.After, "not read"))
	}
	if l.Wrote() && n.Wrote() {
		add("write", method(l), method(n))
		add("backup", Joined(l.Backups), Joined(n.Backups))
		add("journal", journal(l), journal(n))
	}
	add("findings", Joined(l.Findings), Joined(n.Findings))
	add("hardn.yml", saved(l), saved(n))
	if l.YML != nil && n.YML != nil {
		add("hardn.yml "+key, settingOf(t, l.YML.Lines, key), settingOf(t, n.YML.Lines, key))
		add("hardn.yml settings", settingsVs(t, l.YML.Lines, l.Loaded, key), settingsVs(t, n.YML.Lines, n.Loaded, key))
		add("hardn.yml rest", rest(*l.YML, l.Loaded, key, l.Memory), rest(*n.YML, n.Loaded, key, n.Memory))
		add("hardn.yml mode", fmt.Sprintf("%04o", uint32(l.YML.Mode)), fmt.Sprintf("%04o", uint32(n.YML.Mode)))
	}
	return diffs, agree
}

// dirsAndFiles are under's directories made, and its files written and
// removed.
func dirsAndFiles(under []string) (dirs, files []string) {
	for _, u := range under {
		if d, ok := strings.CutPrefix(u, "mkdir "); ok {
			dirs = append(dirs, d)
		} else {
			files = append(files, u)
		}
	}
	return dirs, files
}

// OrElse is s, or none when s is empty.
func OrElse(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

// Joined is l joined by commas, or "none" when it is empty: a value of
// the table.
func Joined(l []string) string { return OrElse(strings.Join(l, ", "), "none") }

func names(fs []FileWrite) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

func method(s EndState) string {
	for _, f := range append(slices.Clone(s.Files), ymlOf(s)...) {
		if !f.Atomic {
			return "in place"
		}
	}
	return "atomic"
}

func journal(s EndState) string {
	if s.Journal {
		return "journalled first"
	}
	return "none"
}

func saved(s EndState) string {
	switch {
	case s.YML == nil:
		return "not saved"
	case s.YML.Path == "./hardn.yml":
		return "./hardn.yml, in the working directory"
	}
	return s.YML.Path + ", the file loaded"
}

// settingOf is the value hardn.yml's lines give key, as hardn decodes it.
func settingOf(t *testing.T, lines []string, key string) string {
	t.Helper()
	v, ok := effective(t, lines)[key]
	if !ok {
		return "not set"
	}
	if l, ok := v.([]any); ok {
		var out []string
		for _, e := range l {
			out = append(out, fmt.Sprint(e))
		}
		return Joined(out)
	}
	return fmt.Sprint(v)
}

// settingsVs is every setting but key that hardn.yml's lines give, as
// hardn loads the file (the defaults, then the file's keys, then
// LoadConfig's LogsConfig line), against the file loaded's, compared by
// value: "as loaded", or each that is not, with its value.
func settingsVs(t *testing.T, lines, loaded []string, key string) string {
	t.Helper()
	got, want := effective(t, lines), effective(t, loaded)
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var not []string
	for _, k := range keys {
		if k != key && !reflect.DeepEqual(got[k], want[k]) {
			not = append(not, fmt.Sprintf("%s %v", k, got[k]))
		}
	}
	if len(not) == 0 {
		return "as loaded"
	}
	return "not as loaded: " + strings.Join(not, "; ")
}

// effective is the configuration hardn loads from lines, a hardn.yml (nil:
// none found), as its keys: config.DefaultConfig, the file's keys, then
// LoadConfig's LogsConfig line (pkg/config/config.go:362).
func effective(t *testing.T, lines []string) map[string]any {
	t.Helper()
	c := config.DefaultConfig()
	if lines != nil {
		if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")+"\n"), c); err != nil {
			t.Fatalf("hardn.yml as saved: %v", err)
		}
	}
	c.LogsConfig.LogFilePath = c.LogFile
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var topLevelKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*:`)

// rest is the form of a saved hardn.yml besides key's lines, against
// loaded, the file loaded: its other lines kept as they were, or its
// settings and how many of the file's comment lines it kept; with memory,
// its settings, no file having been loaded.
func rest(f FileWrite, loaded []string, key string, memory bool) string {
	keys, kept, comments := 0, 0, 0
	for _, l := range f.Lines {
		if topLevelKey.MatchString(l) {
			keys++
		}
	}
	if memory {
		return fmt.Sprintf("%d settings, written whole; no file was loaded", keys)
	}
	for _, l := range loaded {
		if strings.HasPrefix(l, "#") {
			comments++
			if slices.Contains(f.Lines, l) {
				kept++
			}
		}
	}
	if others := outside(f.Lines, key); loaded != nil && slices.Equal(others, outside(loaded, key)) {
		return fmt.Sprintf("the %s lines alone changed; the file's other %d lines kept (%d settings, %d comment lines)", key, len(others), keys, comments)
	}
	switch kept {
	case 0:
		return fmt.Sprintf("%d settings, written whole; the file's %d comment lines dropped", keys, comments)
	case comments:
		return fmt.Sprintf("%d settings, written whole; the file's %d comment lines kept", keys, comments)
	}
	return fmt.Sprintf("%d settings, written whole; %d of the file's %d comment lines kept", keys, kept, comments)
}

// outside are lines without key's: its line and the lines under it, those
// indented or a sequence's items.
func outside(lines []string, key string) []string {
	var out []string
	in := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, key+":"):
			in = true
		case in && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "- ")):
		default:
			in = false
			out = append(out, l)
		}
	}
	return out
}

// checkTable holds every difference to sl.Table (tableProblems), each
// problem an error of the test.
func checkTable(t *testing.T, sl Slice, cells []Cell) {
	t.Helper()
	for _, p := range tableProblems(sl.Table, cells) {
		t.Error(p)
	}
}

// tableProblems are where cells' differences and table disagree: an entry
// listed twice, a difference unlisted (UNEXPLAINED), a V-number of a
// source that origin/main's golden for the cell does not name, an entry
// whose difference no longer occurs.
func tableProblems(table []Tie, cells []Cell) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	want, twice := expand(table)
	for _, k := range twice {
		add("the table lists %s on %s, %s twice", k.Scenario, k.Distro, k.Aspect)
	}
	seen := map[key]bool{}
	for _, c := range cells {
		for _, d := range c.diffs {
			k := key{c.Scenario.Name, c.Distro.Name, d.Name}
			seen[k] = true
			e, ok := want[k]
			if !ok {
				add("UNEXPLAINED %s on %s, %s: origin/main %q, new %q", c.Scenario.Name, c.Distro.Name, d.Name, d.Legacy, d.New)
				continue
			}
			for _, v := range vNumber.FindAllString(e.Source, -1) {
				if !slices.Contains(c.legacy.Vs, v) {
					add("%s on %s, %s: tied to %s, which %s does not name (it names %v)", c.Scenario.Name, c.Distro.Name, d.Name, v, c.legacy.Golden, c.legacy.Vs)
				}
			}
		}
	}
	for k := range want {
		if !seen[k] {
			add("the table lists %s on %s, %s, which no longer differs", k.Scenario, k.Distro, k.Aspect)
		}
	}
	return out
}

var vNumber = regexp.MustCompile(`\bV\d+\b`)

// expand is table by key, and the keys it lists more than once.
func expand(table []Tie) (map[key]Tie, []key) {
	out := map[key]Tie{}
	var twice []key
	for _, e := range table {
		for _, s := range e.Scenarios {
			for _, d := range e.Distros {
				k := key{s, d, e.Aspect}
				if _, dup := out[k]; dup {
					twice = append(twice, k)
				}
				out[k] = e
			}
		}
	}
	return out, twice
}

// renderCells is the golden: every cell, both paths' end states, what
// agrees and what differs, each difference with its source.
func renderCells(sl Slice, cells []Cell) string {
	want, _ := expand(sl.Table)
	var b strings.Builder
	b.WriteString(sl.Header)
	for _, c := range cells {
		fmt.Fprintf(&b, "\n== %s on %s\n", c.Scenario.Name, c.Distro.Label)
		fmt.Fprintf(&b, "host: %s (%s)\n", c.State.Long, c.Shipped)
		if c.Scenario.Fixture == "" {
			b.WriteString("hardn.yml: none loaded\n")
		}
		fmt.Fprintf(&b, "origin/main: %s (%s)\n", c.legacy.Golden, strings.Join(c.legacy.Vs, ", "))
		if c.Scenario.Note != "" {
			fmt.Fprintf(&b, "  %s\n", c.Scenario.Note)
		}
		if c.legacy.Start != "" {
			fmt.Fprintf(&b, "  start: %s\n", c.legacy.Start)
		}
		writeState(&b, c.legacy.Live)
		if c.legacy.Tree != "" {
			fmt.Fprintf(&b, "  the temp dir afterwards (recorded, not compared): %s\n", c.legacy.Tree)
		}
		if c.legacy.CLI != "" {
			fmt.Fprintf(&b, "  hardn -n: refused, %s\n", c.legacy.CLI)
		}
		if c.Why != "" {
			fmt.Fprintf(&b, "not compared: %s\n", c.Why)
			continue
		}
		fmt.Fprintf(&b, "new path: %s\n", c.ran.Via)
		writeState(&b, c.ran.Live)
		for _, a := range c.agree {
			fmt.Fprintf(&b, "agree  %s: %s\n", a.Name, a.Legacy)
		}
		for _, d := range c.diffs {
			fmt.Fprintf(&b, "differ %s: %s | %s\n", d.Name, d.Legacy, d.New)
			if e, ok := want[key{c.Scenario.Name, c.Distro.Name, d.Name}]; ok {
				fmt.Fprintf(&b, "       %s: %s\n", e.Source, e.Why)
			} else {
				b.WriteString("       UNEXPLAINED\n")
			}
		}
		fmt.Fprintf(&b, "v0.3.2: %s\n", c.Scenario.V032)
	}
	return b.String()
}

// writeState is s, indented: the probes, each file written with its
// content (hardn.yml as its lines changed against the file loaded, or its
// size when written whole), the directories, the commands, and what the
// new path adds.
func writeState(b *strings.Builder, s EndState) {
	if len(s.Probes) > 0 {
		fmt.Fprintf(b, "  probes: %s\n", strings.Join(s.Probes, "; "))
	}
	if s.Refusal != "" {
		fmt.Fprintf(b, "  refused: %s\n", s.Refusal)
	}
	if s.Memory {
		b.WriteString("  hardn.yml: none loaded; the configuration made in memory, the fixture's settings\n")
	}
	for _, f := range s.Files {
		fmt.Fprintf(b, "  write %s, %s\n", f, method(EndState{Files: []FileWrite{f}}))
		for _, l := range f.Lines {
			b.WriteString(strings.TrimRight("    | "+l, " ") + "\n")
		}
	}
	if s.YML != nil {
		fmt.Fprintf(b, "  write %s %04o, %s\n", s.YML.Path, uint32(s.YML.Mode), method(EndState{YML: s.YML}))
		if s.YML.Path != "./hardn.yml" { // origin/main's whole file is in its own golden
			for _, l := range lineDiff(s.Loaded, s.YML.Lines) {
				fmt.Fprintf(b, "    %s\n", l)
			}
		}
	}
	for _, u := range s.Under {
		fmt.Fprintf(b, "  under the backup path: %s\n", u)
	}
	if s.After != "" {
		fmt.Fprintf(b, "  after: %s\n", s.After)
	}
	for _, d := range s.Dirs {
		if strings.HasPrefix(d, "chmod ") {
			fmt.Fprintf(b, "  %s\n", d)
		} else {
			fmt.Fprintf(b, "  mkdir %s\n", d)
		}
	}
	for _, a := range s.Actions {
		fmt.Fprintf(b, "  run %s\n", a)
	}
	if s.Journal || len(s.Backups) > 0 || len(s.Findings) > 0 {
		fmt.Fprintf(b, "  backup: %s; journal: %s; findings: %s\n", Joined(s.Backups), journal(s), Joined(s.Findings))
	}
}

// lineDiff is the lines of was that are not in now ("-"), then the lines
// of now that are not in was ("+"), each in its order: a saved hardn.yml
// against the file loaded.
func lineDiff(was, now []string) []string {
	var out []string
	for _, l := range was {
		if !slices.Contains(now, l) {
			out = append(out, "- "+l)
		}
	}
	for _, l := range now {
		if !slices.Contains(was, l) {
			out = append(out, "+ "+l)
		}
	}
	return out
}

// renderPage is the page's summary: the host states, the aspects, every
// difference grouped where both paths' values and the source agree, and
// v0.3.2.
func renderPage(sl Slice, cells []Cell) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- TestParity: begin. Generated by TestParity (%s); make golden-update rewrites it. Do not edit. -->\n\n", sl.Pkg)
	b.WriteString("**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it (real), the harness forced it, or it cannot exist.\n\n")
	b.WriteString("| Scenarios |")
	for _, d := range sl.Distros {
		b.WriteString(" " + d.Label + " |")
	}
	b.WriteString("\n|---|" + strings.Repeat("---|", len(sl.Distros)) + "\n")
	var rows, labels []string
	by := map[string][]string{}
	for _, sc := range sl.Scenarios {
		var r strings.Builder
		for _, c := range cells {
			if c.Scenario.Name == sc.Name {
				word, _, _ := strings.Cut(c.Shipped, ":")
				fmt.Fprintf(&r, " %s (%s) |", c.State.Short, word)
			}
		}
		if _, ok := by[r.String()]; !ok {
			rows = append(rows, r.String())
		}
		by[r.String()] = append(by[r.String()], sc.Name)
	}
	for _, r := range rows {
		labels = append(labels, "| "+strings.Join(by[r], ", ")+" |"+r)
	}
	b.WriteString(strings.Join(labels, "\n") + "\n")

	b.WriteString("\n**Agreeing and differing aspects** (§4): in how many compared cells each aspect agrees and differs.\n\n")
	b.WriteString("| Aspect | Agrees in | Differs in |\n|---|---|---|\n")
	var aspects []string
	count := map[string][2]int{}
	for _, c := range cells {
		for i, as := range [][]aspect{c.agree, c.diffs} {
			for _, a := range as {
				if _, ok := count[a.Name]; !ok {
					aspects = append(aspects, a.Name)
				}
				n := count[a.Name]
				n[i]++
				count[a.Name] = n
			}
		}
	}
	for _, a := range aspects {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", a, count[a][0], count[a][1])
	}
	fmt.Fprintf(&b, "\n**Differences** (§4): each with its source; every cell, both end states whole, is in [%s](../../%s/testdata/golden/%s).\n\n", sl.Golden, sl.Pkg, sl.Golden)
	b.WriteString("| Scenarios | On | Aspect | origin/main | New path | Source |\n|---|---|---|---|---|---|\n")
	type group struct {
		a     aspect
		e     Tie
		cells [][2]string // scenario, distribution
	}
	var groups []*group
	want, _ := expand(sl.Table)
	for _, c := range cells {
		for _, d := range c.diffs {
			e, ok := want[key{c.Scenario.Name, c.Distro.Name, d.Name}]
			if !ok {
				e = Tie{Source: "UNEXPLAINED"}
			}
			i := slices.IndexFunc(groups, func(g *group) bool { return g.a == d && g.e.Source == e.Source && g.e.Why == e.Why })
			if i < 0 {
				groups = append(groups, &group{a: d, e: e})
				i = len(groups) - 1
			}
			groups[i].cells = append(groups[i].cells, [2]string{c.Scenario.Name, c.Distro.Label})
		}
	}
	for _, g := range groups {
		scenarios, on := where(g.cells, len(sl.Distros))
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s: %s |\n", scenarios, on, g.a.Name, cell(g.a.Legacy), cell(g.a.New), g.e.Source, g.e.Why)
	}

	cites := ""
	if sl.V032Sections != "" {
		cites = " " + sl.V032Sections
	}
	fmt.Fprintf(&b, "\n**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md)%s.\n\n", cites)
	b.WriteString("| Scenarios | v0.3.2 |\n|---|---|\n")
	var done []string
	for _, sc := range sl.Scenarios {
		if slices.Contains(done, sc.V032) {
			continue
		}
		done = append(done, sc.V032)
		var names []string
		for _, o := range sl.Scenarios {
			if o.V032 == sc.V032 {
				names = append(names, o.Name)
			}
		}
		fmt.Fprintf(&b, "| %s | %s |\n", strings.Join(names, ", "), sc.V032)
	}
	b.WriteString("\n<!-- TestParity: end -->\n")
	return b.String()
}

// cell is a value for a Markdown table cell.
func cell(s string) string { return "`" + strings.ReplaceAll(s, "|", `\|`) + "`" }

// where is a group's cells as the table's first two columns: its
// scenarios, and the distributions they share ("all" when every one of
// n), or each scenario's own.
func where(cells [][2]string, n int) (scenarios, on string) {
	by := map[string][]string{}
	var order []string
	for _, c := range cells {
		if _, ok := by[c[0]]; !ok {
			order = append(order, c[0])
		}
		by[c[0]] = append(by[c[0]], c[1])
	}
	label := func(ds []string) string {
		if len(ds) == n {
			return "all"
		}
		return strings.Join(ds, ", ")
	}
	same := true
	for _, s := range order {
		same = same && slices.Equal(by[s], by[order[0]])
	}
	if same {
		return strings.Join(order, ", "), label(by[order[0]])
	}
	var parts []string
	for _, s := range order {
		parts = append(parts, s+" ("+label(by[s])+")")
	}
	return strings.Join(parts, "; "), "as listed"
}

// updating reports whether the test runs with -update (pkg/ui/uitest's
// flag, which make golden-update passes).
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// golden compares got with testdata/golden/<name>, or rewrites it under
// -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name)
	if updating() {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run make golden-update to create it)", err)
	}
	if d := firstDiff(string(want), string(got)); d != "" {
		t.Errorf("%s does not match: %s\nIf the change is intended, run make golden-update and give the reason in the review description.", p, d)
	}
}

// page compares region with the page's, between its markers, or rewrites
// it there under -update: the one rendering of the harness outside
// testdata (docs/design-system/README.md, the goldens).
func page(t *testing.T, file, region string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	const begin, end = "<!-- TestParity: begin", "<!-- TestParity: end -->\n"
	i, j := strings.Index(s, begin), strings.Index(s, end)
	if i < 0 || j < i {
		t.Fatalf("%s has no region between %q and %q", file, begin, end)
	}
	d := firstDiff(s[i:j+len(end)], region)
	if d == "" {
		return
	}
	if !updating() {
		t.Fatalf("%s's region is not the one TestParity makes: %s\nRun make golden-update.", file, d)
	}
	if err := os.WriteFile(file, []byte(s[:i]+region+s[j+len(end):]), 0o644); err != nil {
		t.Fatal(err)
	}
}

// firstDiff is where got first differs from want, by line, with both
// lines; "" when they are equal.
func firstDiff(want, got string) string {
	if want == got {
		return ""
	}
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return fmt.Sprintf("line %d (of %d, now %d)\nwant: %q\ngot:  %q", i+1, len(wl), len(gl), w, g)
		}
	}
	return "the line endings differ"
}
