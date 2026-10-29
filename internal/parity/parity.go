// Package parity is the parity harness (refactor plan §2: every slice's
// unit (b2)), shared since slice 3 (next/s3-shared; docs/engine/screen-kit.md
// §3): origin/main's end state, read from pkg/characterization's goldens
// (or a slice's frozen copy of them), against the new path's on hostfake,
// aspect by aspect, every difference held to a table of ties
// (docs/engine/parity-dns.md §2). Nothing in it is an area's own: a slice
// supplies a Slice (DNS's is pkg/screens/dns's parity_test.go) and runs it
// from its TestParity with Run.
//
// Its users are pkg/screens/dns, pkg/screens/backup (since next/s3-parity)
// and pkg/screens/environment (since next/s9-parity), each in a layout of
// its goldens' own: the ops and menu traces, the repository's
// (parseRepository) and the root's (parseRoot).
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
	// LegacyPaths are how origin/main's paths read as the new path's
	// where the characterization ran under a temp dir: old, new pairs,
	// the first that matches at a place taken (strings.NewReplacer), in
	// the golden's Start line, trace, screen, after line and saved
	// hardn.yml. Backup's: "$TMP/home", the run's HOME, as "$HOME", then
	// "$TMP" as the root, "$TMP/x" as "/x". None for DNS, whose goldens
	// name the host's own paths.
	LegacyPaths []string
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
	// Check names the commands that check what a run writes before it
	// takes effect (Environment's visudo and doas -C): each path's are
	// recorded as EndState.Checks, apart from the commands that change
	// something, and compared as the aspect "checks". nil (DNS, Backup):
	// none, and every command reads as before.
	Check func(argv string) bool
	// Temps compares the temp files each path wrote and removed
	// (EndState.Temps, the aspect "temp files"): origin/main's, a file its
	// run wrote and then removed (any other remove still fails the
	// reader); the new path's, what it wrote under the engine's run
	// directory (NewStateChecked). False (DNS, Backup): a remove fails
	// origin/main's reader, as before.
	Temps bool
}

// reader is how a slice's goldens are read (Slice): origin/main's commands
// that only read (Probes), its paths as the new path's (LegacyPaths), the
// commands that check (Check), and whether a file the run wrote and
// removed is a temp file (Temps).
type reader struct {
	probes []string
	paths  []string
	check  func(string) bool
	temps  bool
}

func (sl Slice) reader() reader {
	return reader{probes: sl.Probes, paths: sl.LegacyPaths, check: sl.Check, temps: sl.Temps}
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
	// Pending: no source the slice may cite explains the difference, so it
	// is carried, never explained: Source is a tie proposed, not approved,
	// and the golden and the page mark the difference UNEXPLAINED
	// (pending) with it, until a source is approved or the difference
	// goes. A slice's page lists each in its UNEXPLAINED section.
	Pending bool
}

// label is how the golden and the page give e's source: as it is, or,
// for a pending entry, UNEXPLAINED with the tie proposed.
func (e Tie) label() string {
	if e.Pending {
		return "UNEXPLAINED (pending; proposed, not approved: " + e.Source + ")"
	}
	return e.Source
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
	// Checks are the commands that checked what the run wrote
	// (Slice.Check), in order, one that failed with " -> " and its error;
	// the new path's run id as <run id>.
	Checks []string
	// Temps are the files the run wrote and then removed (Slice.Temps):
	// "<path> <mode>, removed", the new path's run id as <run id>.
	Temps []string
}

// FileWrite is one file as a run left it.
type FileWrite struct {
	Path   string
	Mode   fs.FileMode
	Lines  []string
	Atomic bool // replaced whole by a rename; false: written in place
	// NoMode: the mode is not recorded (origin/main's ./hardn.yml in the
	// repository's layout), so it is not compared.
	NoMode bool
}

func (f FileWrite) String() string {
	if f.NoMode {
		return f.Path + " (mode not recorded)"
	}
	return fmt.Sprintf("%s %04o", f.Path, uint32(f.Mode.Perm()))
}

// Wrote reports whether the run wrote a file, the host's or hardn.yml.
func (s EndState) Wrote() bool { return len(s.Files) > 0 || s.YML != nil }

// legacyRun is origin/main's run, read from its characterization golden.
type legacyRun struct {
	Golden string   // relative to Slice.Legacy
	Vs     []string // the V-numbers its KNOWN-DEFECT and FIXED lines name
	Live   EndState // cfg.DryRun=false, or the menu item's result
	Dry    EndState // cfg.DryRun=true (ops goldens only)
	CLI    []string // hardn -n with each flag: what pkg/cmd's refusal says, each section's
	// Start is what the run started from: the repository's layout's
	// "Start:" line; the root layout's environment and "== / before"
	// section; the ops layout's "seed dir:" and "seed file:" lines (DNS's
	// have none), each line of it apart, joined by "; ".
	Start string
	Tree  string // the temp dir afterwards (the repository's layout): recorded, not compared
}

// LegacyStart is what origin/main's run of c started from, as its golden
// says it (legacyRun.Start), its paths read as the new path's
// (Slice.LegacyPaths); "" for a golden that does not say. A slice holds
// its new path's host to it.
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
// (parseLegacy) as rd says; a golden it cannot read or parse fails the
// test.
func readLegacy(t *testing.T, dir, rel string, rd reader) legacyRun {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("origin/main's golden: %v", err)
	}
	r, err := parseLegacy(data, rel, rd)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// parseLegacy parses data, the characterization golden rel, as rd says:
// its V-numbers, each trace section into an end state, and its -n
// refusals, each section's. An ops golden has both cfg.DryRun sections; a menu item's has
// its result, whose message is its refusal when it saved nothing. What the
// parser does not compare (a removal, a failed command or write, a run
// that failed) is an error rather than being dropped, and so is a golden
// with neither cfg.DryRun=false nor a result, the run compared: one in a
// layout it does not know would otherwise read as a run that did nothing.
// With rd.check, a command it names is a check (EndState.Checks); with
// rd.temps, the removal of a file the section wrote and the fixture did
// not seed makes that file a temp file (EndState.Temps), any other removal
// still an error. The "seed dir:" and "seed file:" lines of its fixture
// are its Start. A golden in the
// repository's layout (the Backup screen's, pkg/characterization's
// menu/backup_*, frozen in pkg/screens/backup/testdata/legacy since
// next/s3-replace) is read by parseRepository, its paths read as
// rd.paths says; one in the root layout (the Environment area's,
// pkg/characterization's environment/keep_*) by parseRoot.
func parseLegacy(data []byte, rel string, rd reader) (legacyRun, error) {
	if bytes.Contains(data, []byte("\n"+repoCalls+"\n")) {
		return parseRepository(data, rel, rd.paths)
	}
	if bytes.Contains(data, []byte("\n"+rootAfter+"\n")) {
		return parseRoot(data, rel, rd)
	}
	probes := rd.probes
	r := legacyRun{Golden: rel}
	live, fixture, seeding := false, false, false
	var cur, result *EndState
	var last *FileWrite
	var seed []string
	seeded := map[string]bool{} // the paths the fixture seeded
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := sc.Text()
		r.Vs = named(r.Vs, l)
		switch {
		case l == "== cfg.DryRun=false":
			cur, last, live, fixture = &r.Live, nil, true, false
			continue
		case l == "== result":
			cur, result, last, live, fixture = &r.Live, &r.Live, nil, true, false
			continue
		case l == "== cfg.DryRun=true":
			cur, last, fixture = &r.Dry, nil, false
			continue
		case l == "== fixture":
			cur, last, fixture = nil, nil, true
			continue
		case strings.HasPrefix(l, "== "):
			cur, last, fixture = nil, nil, false
			continue
		case strings.HasPrefix(l, "refused: "):
			r.CLI = append(r.CLI, strings.TrimPrefix(l, "refused: "))
		case strings.HasPrefix(l, "stderr: ") && len(r.CLI) > 0:
			r.CLI[len(r.CLI)-1] += ": " + strings.TrimPrefix(l, "stderr: ")
		}
		if fixture {
			// The seed lines, and a seed file's content, are the start.
			c, content := strings.CutPrefix(l, "        ")
			switch {
			case content && seeding:
				seed = append(seed, c)
			case strings.HasPrefix(l, "seed dir: "), strings.HasPrefix(l, "seed file: "):
				seed, seeding = append(seed, l), strings.HasPrefix(l, "seed file: ")
				_, p, _ := strings.Cut(l, ": ")
				seeded[p] = true
			default:
				seeding = false
			}
			continue
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
			switch {
			case rd.check != nil && rd.check(arg):
				cur.Checks = append(cur.Checks, arg)
			case slices.Contains(probes, arg):
				cur.Probes = append(cur.Probes, arg+" -> "+res)
			default:
				cur.Actions = append(cur.Actions, arg)
			}
		case "remove":
			i := slices.IndexFunc(cur.Files, func(f FileWrite) bool { return f.Path == arg })
			if !rd.temps || i < 0 {
				return r, fmt.Errorf("%s: an operation the harness does not compare (%s): %q", rel, op, l)
			}
			if seeded[arg] {
				return r, fmt.Errorf("%s: a removal of %s, which the fixture seeded: not a temp file the run made: %q", rel, arg, l)
			}
			cur.Temps = append(cur.Temps, cur.Files[i].String()+", removed")
			cur.Files = slices.Delete(cur.Files, i, i+1)
			last = nil
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
	r.Start = strings.Join(seed, "; ")
	return r, nil
}

// The repository's layout (pkg/characterization's TestLegacyMenuBackup,
// retired in next/s3-replace, its menu/backup_* frozen in
// pkg/screens/backup/testdata/legacy): a header of "# " lines, the screen
// as printed between two "# ----" lines, then the repository's file
// system calls, what is at the backup path after, the temp dir
// afterwards, and ./hardn.yml as written whole, or not.
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

// origin/main's Backup screen's lines as printed (pkg/menu
// backup_menu.go:41-84), its escapes dropped: each draw's heading, its
// Backup Path row, and the result of its own write test, which it runs on
// each draw with backups on (VerifyBackupPath, pkg/application
// backup_manager.go:86-99, with os, outside the repository's calls): the
// path written to and removed, or nothing written (the path not there, or
// the write refused). Both files were deleted in next/s3-replace.
var (
	screenEscape   = regexp.MustCompile(`\\x1b\[[0-9;?]*[A-Za-z]`)
	drawHeading    = "Backup Settings"
	drawPath       = regexp.MustCompile(`^\S+\s+Backup Path\s+(\S+)$`)
	drawWritable   = regexp.MustCompile(`^\S+ Backup directory exists and is writable$`)
	drawUnwritable = regexp.MustCompile(`^\S+ Backup directory doesn't exist or isn't writable$`)
	drawError      = "Error checking backup path"
)

// ScreenTest marks what the screen's own write test did (draw), apart from
// the repository's calls: the screen's line "Backup directory exists and
// is writable" is os.WriteFile(<path>/.write_test, "test", 0644), then
// os.Remove (backup_manager.go:91-99, deleted in next/s3-replace).
const ScreenTest = " (screen)"

// draw is one draw of the Backup screen: its Backup Path row, and whether
// its write test wrote; status is "" before its result is printed.
type draw struct{ path, status string }

// parseRepository parses data, the characterization golden rel in the
// repository's layout, its paths read as paths says: the V-numbers of its
// KNOWN-DEFECT and FIXED lines; Start; what was written under the backup
// path, as Under: the screen's own write test on its first draw (marked
// ScreenTest), then the repository's calls (mkdir, write, remove), then the
// write tests of the draws after, a mkdir or write that failed being the
// run's refusal, "failed: <error>" (a second failure, a failed remove or
// another operation is an error); the after line as After; ./hardn.yml as
// written, whole, its mode not recorded (NoMode); the configuration made in
// memory (Memory), as TestLegacyMenuBackup made it (retired in
// next/s3-replace). Of the screen it reads each draw's path and its write
// test's result alone; the temp dir is recorded, not compared, and must
// hold no .write_test where a draw wrote one (the screen's remove is not
// checked otherwise). A line or an operation it does not know is an
// error, and so is a golden without the calls, the after line or the
// hardn.yml line, with one of the trailer's sections twice, with no draw,
// or with calls and other than two draws, between which an item's calls
// are made.
func parseRepository(data []byte, rel string, paths []string) (legacyRun, error) {
	r := legacyRun{Golden: rel, Live: EndState{Memory: true}}
	abs := strings.NewReplacer(paths...).Replace
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
	var draws []draw
	var traced []string
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
				continue
			}
			v := strings.TrimSpace(strings.TrimSuffix(screenEscape.ReplaceAllString(l, ""), "$"))
			d := len(draws) - 1
			m := drawPath.FindStringSubmatch(v)
			switch {
			case v == drawHeading:
				draws = append(draws, draw{})
			case strings.Contains(v, drawError):
				return fail("a check of the path that failed, which the harness does not compare: %q", l)
			case d < 0 && (m != nil || drawWritable.MatchString(v) || drawUnwritable.MatchString(v)):
				return fail("a Backup Path row or write test before the screen's heading: %q", l)
			case m != nil:
				draws[d].path = abs(m[1])
			case drawWritable.MatchString(v), drawUnwritable.MatchString(v):
				if draws[d].path == "" || draws[d].status != "" {
					return fail("a write test's result with no Backup Path row before it, or a second: %q", l)
				}
				draws[d].status = "nothing"
				if drawWritable.MatchString(v) {
					draws[d].status = "wrote"
				}
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
					traced = append(traced, op+" "+w[1]+" "+w[2])
				case "remove":
					if res != "ok" {
						return fail("a remove that failed, which the harness does not compare: %q", l)
					}
					traced = append(traced, op+" "+arg)
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
			r.Live.YML = &FileWrite{Path: "./hardn.yml", NoMode: true}
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
	switch {
	case len(draws) == 0:
		return fail("no draw of the screen (%q)", drawHeading)
	case (len(traced) > 0 || r.Live.Refusal != "") && len(draws) != 2:
		return fail("the repository's calls and %d draws of the screen: the reader places an item's calls between two", len(draws))
	}
	tested := func(d draw) []string {
		if d.status != "wrote" {
			return nil
		}
		f := path.Join(d.path, ".write_test")
		return []string{"write " + f + " 0644" + ScreenTest, "remove " + f + ScreenTest}
	}
	r.Live.Under = append(tested(draws[0]), traced...)
	for _, d := range draws[1:] {
		r.Live.Under = append(r.Live.Under, tested(d)...)
	}
	if slices.ContainsFunc(draws, func(d draw) bool { return d.status == "wrote" }) && strings.Contains(r.Tree, ".write_test") {
		return fail("the temp dir afterwards holds a .write_test, which the reader takes the screen's write test to have removed: %q", r.Tree)
	}
	return r, nil
}

// The root layout (pkg/characterization's TestLegacyEnvironmentKeep,
// environment/keep_*): a header of "# " lines; "== / before", the root's
// entries with their modes and a file's content, then the commands the
// mock commander fails; "== result", the call's outcome, then each
// caller's words for it, read from the code; "== calls", the file system
// and command calls in order, a write with its content; "== / afterwards",
// how the root changed. The case's temp dir stands for /, and every path
// is hardn's own (rootedFS cuts the temp dir from it; the test fails if
// the temp dir is in a golden), read then as reader.paths says.
const (
	rootBefore  = "== / before"
	rootResult  = "== result"
	rootCalls   = "== calls"
	rootAfter   = "== / afterwards"
	rootNoCalls = "    (no calls)"
	rootFails   = "commands that fail (every other command succeeds with empty output):"
)

// rootHeader are the header lines the root layout has, each the start of
// a line: the operation, its notes and its start in words are what was
// run, not an end state; KNOWN-DEFECT and FIXED name V-numbers;
// Environment is kept with the start.
var rootHeader = []string{"# origin/main behaviour", "# Generated by ", "# Operation: ", "# Environment: ", "# Start: ",
	"# KNOWN-DEFECT: ", "# FIXED ", "# NOTE: ", "# The file system is "}

// rootCallers start the result section's lines after the outcome: what
// each caller prints for it, read from the code, not run (what was shown,
// not an end state).
var rootCallers = []string{"screen, item 1 as root (read, not run: ", "hardn -e and setup-sudo-env (read, not run: "}

var (
	rootEntry   = regexp.MustCompile(`^(/\S*): (?:a directory|a symlink to (\S+)|a file, \d+ bytes)(?:, mode \d{4})?$`)
	rootFixture = regexp.MustCompile(`^  \S.* -> fixture: .+$`)
	rootOutcome = regexp.MustCompile(`^\w+: (?:ok|error: (.+))$`)
	rootNew     = regexp.MustCompile(`^(/\S*): new, a file, \d+ bytes$`)
	rootChanged = regexp.MustCompile(`^(/\S*): changed, a file, \d+ bytes, mode (\d{4}) \(was .+\)$`)
	rootGone    = regexp.MustCompile(`^(/\S*): gone \(was .+\)$`)
)

// rootWrite is one write the calls record, its content, and whether its
// file was then removed (a temp file).
type rootWrite struct {
	path    string
	mode    fs.FileMode
	lines   []string
	removed bool
}

// rootFile is a file the afterwards section shows new or changed, with
// its content; mode is its mode there, when it says it (a changed file).
type rootFile struct {
	path  string
	mode  fs.FileMode
	moded bool
	lines []string
}

// parseRoot parses data, the characterization golden rel in the root
// layout, as rd says: the V-numbers of its KNOWN-DEFECT and FIXED lines;
// Start, its environment and its "/ before" section; the end state of the
// call:
//   - its refusal, "failed: <error>" when the outcome is an error, after at
//     most one call that failed (a write or a check), or none;
//   - the files, each new or changed file the afterwards section shows,
//     with its content there, its mode the write's (a new file's; the
//     umask's is not recorded) or the afterwards section's (a changed
//     file's: os.WriteFile keeps a file's mode), written in place
//     (os.WriteFile); each must be explained by a write of the same
//     content to its path, or to a temp file whose path held a symlink to
//     it before (written through the link), and each write not removed
//     must be shown;
//   - the temp files (rd.temps): each file the run made, written, then
//     removed (a path "/ before" does not hold as a file or a directory),
//     and whether its write followed a symlink there, which the removal
//     took: the link's target must be shown changed with the write's
//     content, and the link gone; each file gone afterwards must be one
//     removed;
//   - the checks (rd.check), a failed one with its error; the other
//     commands, which must not fail.
//
// A line, section, operation or afterwards entry it does not know is an
// error, and so is a section twice or out of order, a missing section or
// outcome, a removal of a file the run did not write, of one "/ before"
// holds, or without rd.temps, a second call that failed, or one that
// failed with the outcome ok.
func parseRoot(data []byte, rel string, rd reader) (legacyRun, error) {
	r := legacyRun{Golden: rel}
	abs := strings.NewReplacer(rd.paths...).Replace
	fail := func(format string, args ...any) (legacyRun, error) {
		return r, fmt.Errorf("%s: "+format, append([]any{rel}, args...)...)
	}
	sections := []string{rootBefore, rootResult, rootCalls, rootAfter}
	part := -1 // the header
	var start []string
	env, outcome, failed := "", "", ""
	seenOutcome, fixtures := false, false
	links := map[string]string{} // a symlink before, by its path: its target, resolved
	held := map[string]bool{}    // a file or a directory before, by its path
	var writes []*rootWrite
	var files []*rootFile
	var gone []string
	var content func(line string) // where a content line goes; nil: none may come
	into := func(lines *[]string) func(string) {
		return func(line string) { *lines = append(*lines, line) }
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "== ") {
			i := slices.Index(sections, l)
			if i != part+1 {
				return fail("a section the reader does not know, or out of its order (%s): %q", strings.Join(sections, ", "), l)
			}
			part, content = i, nil
			continue
		}
		if part == -1 {
			if l == "" {
				continue
			}
			if !slices.ContainsFunc(rootHeader, func(p string) bool { return strings.HasPrefix(l, p) }) {
				return fail("a header line the reader does not know: %q", l)
			}
			if v, ok := strings.CutPrefix(l, "# Environment: "); ok {
				env = v
			}
			r.Vs = named(r.Vs, l)
			continue
		}
		if c, ok := strings.CutPrefix(l, "        "); ok && (c == "|" || strings.HasPrefix(c, "| ")) {
			if content == nil {
				return fail("content with no file before it: %q", l)
			}
			content(strings.TrimPrefix(strings.TrimPrefix(c, "|"), " "))
			continue
		}
		if l == "" {
			content = nil
			continue
		}
		switch sections[part] {
		case rootBefore:
			m := rootEntry.FindStringSubmatch(l)
			switch {
			case l == rootFails:
				fixtures, content = true, nil
				start = append(start, l)
			case fixtures && rootFixture.MatchString(l):
				start = append(start, strings.TrimSpace(l))
			case m != nil && !fixtures:
				start, content = append(start, l), nil
				if strings.Contains(l, ": a file, ") {
					content = func(line string) { start = append(start, strings.TrimSpace("| "+line)) }
				}
				if m[2] != "" {
					t := m[2]
					if !path.IsAbs(t) {
						t = path.Join(path.Dir(m[1]), t)
					}
					links[m[1]] = t
				} else {
					held[m[1]] = true
				}
			default:
				return fail("a line of / before the reader does not know: %q", l)
			}
		case rootResult:
			switch {
			case !seenOutcome:
				m := rootOutcome.FindStringSubmatch(l)
				if m == nil {
					return fail("an outcome the reader does not know: %q", l)
				}
				seenOutcome, outcome = true, m[1] // "" for ok
			case !slices.ContainsFunc(rootCallers, func(p string) bool { return strings.HasPrefix(l, p) }):
				return fail("a result line the reader does not know: %q", l)
			}
		case rootCalls:
			if l == rootNoCalls {
				continue
			}
			m := traceLine.FindStringSubmatch(l)
			if m == nil {
				return fail("a call the reader does not know: %q", l)
			}
			op, arg, res := m[1], m[2], m[3]
			e, failedCall := strings.CutPrefix(res, "error: ")
			if failedCall && op != "stat" && op != "read" {
				if failed != "" {
					return fail("a second call that failed, which the harness does not compare: %q", l)
				}
				failed = l
			}
			content = nil
			switch op {
			case "stat", "read": // reads leave no end state
			case "write":
				w := writeTarget.FindStringSubmatch(arg)
				if w == nil {
					return fail("a write the parser does not know: %q", l)
				}
				var mode uint32
				if _, err := fmt.Sscanf(w[2], "%o", &mode); err != nil {
					return fail("%q: %w", l, err)
				}
				rw := &rootWrite{path: w[1], mode: fs.FileMode(mode)}
				if !failedCall {
					writes = append(writes, rw)
				}
				content = into(&rw.lines)
			case "exec":
				switch {
				case rd.check != nil && rd.check(arg) && failedCall:
					r.Live.Checks = append(r.Live.Checks, arg+" -> "+e)
				case rd.check != nil && rd.check(arg):
					r.Live.Checks = append(r.Live.Checks, arg)
				case failedCall:
					return fail("a command that failed, which the harness does not compare: %q", l)
				case slices.Contains(rd.probes, arg):
					r.Live.Probes = append(r.Live.Probes, arg+" -> "+res)
				default:
					r.Live.Actions = append(r.Live.Actions, arg)
				}
			case "remove":
				i := slices.IndexFunc(writes, func(w *rootWrite) bool { return w.path == arg && !w.removed })
				if failedCall || !rd.temps || i < 0 {
					return fail("an operation the harness does not compare (remove): %q", l)
				}
				if held[arg] {
					return fail("a removal of %s, which / before holds: not a temp file the run made: %q", arg, l)
				}
				writes[i].removed = true
			default:
				return fail("an operation the harness does not compare (%s): %q", op, l)
			}
		case rootAfter:
			content = nil
			switch {
			case l == "unchanged", strings.HasPrefix(l, "unchanged: "):
			case rootNew.MatchString(l):
				f := &rootFile{path: rootNew.FindStringSubmatch(l)[1]}
				files, content = append(files, f), into(&f.lines)
			case rootChanged.MatchString(l):
				m := rootChanged.FindStringSubmatch(l)
				var mode uint32
				if _, err := fmt.Sscanf(m[2], "%o", &mode); err != nil {
					return fail("%q: %w", l, err)
				}
				f := &rootFile{path: m[1], mode: fs.FileMode(mode), moded: true}
				files, content = append(files, f), into(&f.lines)
			case rootGone.MatchString(l):
				gone = append(gone, rootGone.FindStringSubmatch(l)[1])
			default:
				return fail("an afterwards line the reader does not know: %q", l)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fail("%w", err)
	}
	switch {
	case part != len(sections)-1:
		return fail("no %q section in the root layout", sections[part+1])
	case !seenOutcome:
		return fail("no outcome in the result section")
	case failed != "" && outcome == "":
		return fail("a call that failed, and the outcome ok: %q", failed)
	case outcome != "":
		r.Live.Refusal = "failed: " + abs(outcome)
	}
	for _, w := range writes {
		if !w.removed {
			continue
		}
		t := abs(w.path) + fmt.Sprintf(" %04o", uint32(w.mode)) + ", removed"
		if to, ok := links[w.path]; ok {
			t = abs(w.path) + fmt.Sprintf(" %04o", uint32(w.mode)) + ", written through the symlink there to " + abs(to) + ", the symlink removed"
		}
		r.Live.Temps = append(r.Live.Temps, t)
	}
	for _, g := range gone {
		if !slices.ContainsFunc(writes, func(w *rootWrite) bool { return w.path == g && w.removed }) {
			return fail("%s gone afterwards, which no removal of the calls explains", g)
		}
	}
	for _, f := range files {
		i := slices.IndexFunc(writes, func(w *rootWrite) bool {
			return slices.Equal(w.lines, f.lines) && (w.path == f.path && !w.removed || w.removed && links[w.path] == f.path)
		})
		if i < 0 {
			return fail("%s changed afterwards, which no write of the calls explains", f.path)
		}
		mode := writes[i].mode
		if f.moded {
			mode = f.mode
		}
		r.Live.Files = append(r.Live.Files, FileWrite{Path: abs(f.path), Mode: mode, Lines: f.lines})
	}
	for _, w := range writes {
		shown := func(p string) bool {
			return slices.ContainsFunc(files, func(f *rootFile) bool { return f.path == p && slices.Equal(f.lines, w.lines) })
		}
		to, linked := links[w.path]
		switch {
		case !w.removed && !shown(w.path):
			return fail("a write of %s that the afterwards section does not show", w.path)
		case w.removed && linked && !shown(to):
			return fail("a write through the symlink at %s that the afterwards section does not show at %s", w.path, to)
		case w.removed && linked && !slices.Contains(gone, w.path):
			return fail("a removal of the symlink at %s that the afterwards section does not show gone", w.path)
		}
	}
	for i, s := range start {
		start[i] = abs(s)
	}
	r.Start = strings.Join(append([]string{"Environment: " + env}, start...), "; ")
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
	return newState(t, h, n, stateOptions{readOnly: readOnly, yml: yml, dir: backup.DefaultPath})
}

// NewStateUnder is NewState with the backups under dir, the backup path
// a slice gives (Backup's: the one the plan's backups were taken under,
// engine.Plan.Backups, decision 7 (a)): what was written there is
// recorded in Under, for Slice.UnderBackupPath, each backup run's
// directory as <run id>.
func NewStateUnder(t *testing.T, h *hostfake.System, n int, readOnly []string, yml, dir string) EndState {
	t.Helper()
	s := newState(t, h, n, stateOptions{readOnly: readOnly, yml: yml, dir: dir, under: true})
	if s.Under == nil {
		s.Under = []string{}
	}
	return s
}

// NewStateChecked is NewState for a slice that compares checks and temp
// files (Slice.Check, Slice.Temps; Environment's): the commands check
// names are Checks, in order, a failed one with its exit status, each run
// id as <run id>; every other command is one that changes something; the
// files written under the engine's run directory are Temps, "<path>
// <mode>, removed" once a removal there took them, else ", left".
func NewStateChecked(t *testing.T, h *hostfake.System, n int, check func(argv string) bool, yml string) EndState {
	t.Helper()
	return newState(t, h, n, stateOptions{yml: yml, dir: backup.DefaultPath, check: check, temps: true})
}

// stateOptions are how newState reads the calls: the commands that only
// read, hardn.yml's path, the backup path, and whether to record what was
// written under it (Under), the checks, and the temp files.
type stateOptions struct {
	readOnly []string
	yml, dir string
	under    bool
	check    func(string) bool
	temps    bool
}

// runID is a backup run's directory name (backup.NewRunID): its time and
// a random part.
var runID = regexp.MustCompile(`\b\d{8}T\d{6}Z-[0-9a-f]{8}\b`)

// newState is NewState as o says: the backups under o.dir, recorded in
// Under with o.under; the checks and the temp files with o.check and
// o.temps.
func newState(t *testing.T, h *hostfake.System, n int, o stateOptions) EndState {
	t.Helper()
	readOnly, yml, dir := o.readOnly, o.yml, o.dir
	var s EndState
	var order []string
	modes := map[string]hostfake.Call{}
	var manifest []byte
	var temps []string               // written under the run directory, in order
	tempModes := map[string]string{} // and their modes
	removed := map[string]bool{}
	for _, c := range h.Calls()[n:] {
		switch {
		case under(c.Path, runDir):
			switch {
			case !o.temps:
			case c.Op == "write-atomic" || c.Op == "write-in-place":
				if _, ok := tempModes[c.Path]; !ok {
					temps = append(temps, c.Path)
				}
				tempModes[c.Path], removed[c.Path] = fmt.Sprintf("%04o", uint32(c.Mode.Perm())), false
			case (c.Op == "remove" || c.Op == "removeall") && c.Err == nil:
				for _, p := range temps {
					if under(p, c.Path) {
						removed[p] = true
					}
				}
			}
			continue
		case under(c.Path, dir):
			if c.Op == "write-atomic" && path.Base(c.Path) == backup.ManifestName {
				manifest = c.Data
			}
			if o.under {
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
			case o.check != nil && o.check(argv):
				argv = runID.ReplaceAllString(argv, "<run id>")
				switch {
				case c.ExitCode > 0:
					argv += fmt.Sprintf(" -> exit status %d", c.ExitCode)
				case c.Err != nil:
					argv += " -> " + c.Err.Error()
				}
				s.Checks = append(s.Checks, argv)
			case !slices.Contains(readOnly, argv):
				s.Actions = append(s.Actions, argv)
			case !slices.Contains(s.Probes, argv):
				s.Probes = append(s.Probes, argv)
			}
		default: // remove, removeall, rename, chown, append, start-detached, ...
			t.Fatalf("a change the harness does not compare: %s", c)
		}
	}
	for _, p := range temps {
		state := ", left"
		if removed[p] {
			state = ", removed"
		}
		s.Temps = append(s.Temps, runID.ReplaceAllString(p, "<run id>")+" "+tempModes[p]+state)
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
	c.legacy = readLegacy(t, sl.Legacy, strings.ReplaceAll(sc.Golden, "{d}", d.Name), sl.reader())
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
// any path reaches it: "as live" when it did what the live section did,
// its checks and temp files too (Slice.Check, Slice.Temps; none for DNS
// and Backup), which it names where either section has any.
func legacyDry(r legacyRun) string {
	did := "the service writes " + Joined(names(r.Dry.Files)) + " and runs " + Joined(r.Dry.Actions)
	if len(r.Live.Checks)+len(r.Live.Temps)+len(r.Dry.Checks)+len(r.Dry.Temps) > 0 {
		did += ", checks " + Joined(r.Dry.Checks) + ", temp files " + Joined(r.Dry.Temps)
	}
	if HostText(r.Dry) == HostText(r.Live) && slices.Equal(r.Dry.Checks, r.Live.Checks) && slices.Equal(r.Dry.Temps, r.Live.Temps) {
		did = "the service writes and runs as live (cfg.DryRun unread below the menus)"
	}
	if len(r.CLI) == 0 {
		return did
	}
	var statuses []string
	for _, c := range r.CLI {
		if status, _, _ := strings.Cut(c, ":"); !slices.Contains(statuses, status) {
			statuses = append(statuses, status)
		}
	}
	return did + "; no path reaches it: hardn -n refused (" + strings.Join(statuses, ", ") + ")"
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
// saves, apart, and its mode where both record it. With
// sl.UnderBackupPath, what each wrote under the backup path, its
// directories and its files, and what is there after.
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
	if sl.Check != nil {
		add("checks", Joined(l.Checks), Joined(n.Checks))
	}
	if sl.Temps {
		add("temp files", Joined(l.Temps), Joined(n.Temps))
	}
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
		if !l.YML.NoMode && !n.YML.NoMode {
			add("hardn.yml mode", fmt.Sprintf("%04o", uint32(l.YML.Mode)), fmt.Sprintf("%04o", uint32(n.YML.Mode)))
		}
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
		for _, cli := range c.legacy.CLI {
			fmt.Fprintf(&b, "  hardn -n: refused, %s\n", cli)
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
				fmt.Fprintf(&b, "       %s: %s\n", e.label(), e.Why)
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
		fmt.Fprintf(b, "  write %s, %s\n", s.YML, method(EndState{YML: s.YML}))
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
	for _, c := range s.Checks {
		fmt.Fprintf(b, "  check %s\n", c)
	}
	for _, f := range s.Temps {
		fmt.Fprintf(b, "  temp %s\n", f)
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
			i := slices.IndexFunc(groups, func(g *group) bool {
				return g.a == d && g.e.Source == e.Source && g.e.Why == e.Why && g.e.Pending == e.Pending
			})
			if i < 0 {
				groups = append(groups, &group{a: d, e: e})
				i = len(groups) - 1
			}
			groups[i].cells = append(groups[i].cells, [2]string{c.Scenario.Name, c.Distro.Label})
		}
	}
	for _, g := range groups {
		scenarios, on := where(g.cells, len(sl.Distros))
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s: %s |\n", scenarios, on, g.a.Name, cell(g.a.Legacy), cell(g.a.New), g.e.label(), g.e.Why)
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
