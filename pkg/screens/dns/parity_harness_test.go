package dns_test

import (
	"bufio"
	"bytes"
	"encoding/json"
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
)

// The parity harness (refactor plan §2: every slice's unit (b2)):
// origin/main's end state, read from pkg/characterization's goldens (a
// frozen copy, legacyGoldens), against the new path's on hostfake,
// aspect by aspect, every difference held to a table of ties
// (docs/engine/parity-dns.md §2). Nothing in this
// file is an area's own: a slice supplies a paritySlice (DNS's is
// parity_test.go), and the next slice copies this file whole, renaming
// its package clause, until a shared package holds it (parity-dns.md §8,
// "For the next slice").

// paritySlice is what a slice supplies: its rows, its distributions, its
// table of differences and the parts of the comparison that are its own.
type paritySlice struct {
	scenarios []parityScenario
	distros   []parityDistro
	table     []tie
	header    string // the golden's first lines
	golden    string // the golden, under testdata/golden
	page      string // the page whose region the test writes, from the package
	pkg       string // the package of the slice's TestParity, from the module root
	setting   string // the hardn.yml key the slice's items save
	// shipped says whether d's stock install has state; exists is why
	// state cannot be on d, "" when it can (the cell is then compared).
	shipped func(state hostState, d parityDistro) string
	exists  func(state hostState, d parityDistro) string
	// probes are origin/main's commands that only read: recorded, not
	// compared.
	probes []string
	// run is the new path on c.
	run func(t *testing.T, c parityCell) newRun
	// content adds the slice's aspects of the host's files both paths
	// wrote (what they set, line by line).
	content func(add func(name, legacy, new string), l, n []fileWrite)
}

// parityScenario is a row of the table, run on every distribution.
type parityScenario struct {
	name    string
	golden  string    // origin/main's, under legacyGoldens; {d} is the distribution
	state   hostState // the new path's host; zero: the stock install's
	fixture string    // hardn.yml, under fixtures; "": none loaded
	// lines are a screen item's answers; nil for the control's plan and
	// apply (a CLI flag), which the screen's item that applies the
	// configuration as it is must match.
	lines  []string
	dryRun bool
	note   string // how origin/main's golden stands for this host, when it was not run on it
	v032   string // v0.3.2 where it differs, from v032-reference.md
}

// hostState is a host the new path runs on, in words and in a word or two.
type hostState struct{ long, short string }

// parityDistro is one column of origin/main's matrix (pkg/characterization's
// osCells): the facts the new path's host takes, and its stock install's
// host state.
type parityDistro struct {
	name, label string
	facts       detect.Facts
	stock       hostState
}

// newRun is the new path's run of a cell: its end state, how it was
// reached, and for a scenario with no lines what its dry run left.
type newRun struct {
	live endState
	via  string
	dry  string
}

// parityCell is a scenario on a distribution, as both paths left it.
type parityCell struct {
	sc      parityScenario
	d       parityDistro
	state   hostState
	shipped string // whether the stock install has state
	why     string // why state cannot exist on d; "" when compared
	legacy  legacyRun
	run     newRun
	diffs   []aspect
	agree   []aspect
}

// aspect is one compared aspect: origin/main's value and the new path's.
type aspect struct{ name, legacy, new string }

// tie is an expected difference: every (scenario, distribution) it names
// differs in aspect, for the reason its source gives. No entry may name a
// difference that does not occur.
type tie struct {
	scenarios []string
	distros   []string
	aspect    string
	source    string // V-numbers, decisions, proposals, plan rows, pages
	why       string // one short line
}

// key names one difference.
type key struct{ scenario, distro, aspect string }

// endState is what a run left and said.
type endState struct {
	refusal  string      // what stopped it before any change; "" for nothing
	files    []fileWrite // the host's files written, in order
	dirs     []string    // directories made ("path mode"), modes set ("chmod path mode")
	actions  []string    // commands that change something
	probes   []string    // commands that only read: recorded, not compared
	backups  []string    // files backed up before the change
	journal  bool        // the change journalled before it ran
	findings []string    // the plan's findings, by code
	yml      *fileWrite  // hardn.yml as saved; nil when not saved
	loaded   []string    // hardn.yml as loaded, the fixture's lines; nil for none
}

// fileWrite is one file as a run left it.
type fileWrite struct {
	path   string
	mode   fs.FileMode
	lines  []string
	atomic bool // replaced whole by a rename; false: written in place
}

func (f fileWrite) String() string { return fmt.Sprintf("%s %04o", f.path, uint32(f.mode.Perm())) }

// wrote reports whether the run wrote a file, the host's or hardn.yml.
func (s endState) wrote() bool { return len(s.files) > 0 || s.yml != nil }

// legacyRun is origin/main's run, read from its characterization golden.
type legacyRun struct {
	golden string   // relative to legacyGoldens
	vs     []string // the V-numbers its KNOWN-DEFECT and FIXED lines name
	live   endState // cfg.DryRun=false, or the menu item's result
	dry    endState // cfg.DryRun=true (ops goldens only)
	cli    string   // hardn -n with the flag: what pkg/cmd's refusal says
}

// legacyGoldens and fixtures are origin/main's record: pkg/characterization's
// goldens and the hardn.yml fixtures they loaded, frozen as they were at
// e666243 (slice 2 unit (d), next/s2-replace, which deleted the DNS menu
// and moved -g off the legacy service), in their layout there. Nothing
// regenerates them: no test writes under testdata/legacy, and
// pkg/characterization's -update writes its own copies of ops/dns_*.
var (
	legacyGoldens = filepath.Join("testdata", "legacy")
	fixtures      = filepath.Join("testdata", "legacy", "fixtures", "config")
)

var (
	traceLine   = regexp.MustCompile(`^\s+\d+ ([a-z]+)\s+(.*?)(?: -> (ok|error: .*))?$`)
	writeTarget = regexp.MustCompile(`^(\S+) mode=(\d{4})$`)
	defectLine  = regexp.MustCompile(`^# (?:KNOWN-DEFECT|FIXED [^:]*): (V\d+) `)
)

// readLegacy parses the characterization golden rel: its V-numbers, each
// trace section into an end state, and its -n refusal. An ops golden has
// both cfg.DryRun sections; a menu item's has its result, whose message is
// its refusal when it saved nothing. What the parser does not compare (a
// removal, a failed command or write, a run that failed) fails the test
// rather than being dropped.
func readLegacy(t *testing.T, probes []string, rel string) legacyRun {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(legacyGoldens, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("origin/main's golden: %v", err)
	}
	r := legacyRun{golden: rel}
	var cur, result *endState
	var last *fileWrite
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := sc.Text()
		if m := defectLine.FindStringSubmatch(l); m != nil && !slices.Contains(r.vs, m[1]) {
			r.vs = append(r.vs, m[1])
		}
		switch {
		case l == "== cfg.DryRun=false":
			cur, last = &r.live, nil
			continue
		case l == "== result":
			cur, result, last = &r.live, &r.live, nil
			continue
		case l == "== cfg.DryRun=true":
			cur, last = &r.dry, nil
			continue
		case strings.HasPrefix(l, "== "):
			cur, last = nil, nil
			continue
		case strings.HasPrefix(l, "refused: "):
			r.cli = strings.TrimPrefix(l, "refused: ")
		case strings.HasPrefix(l, "stderr: ") && r.cli != "":
			r.cli += ": " + strings.TrimPrefix(l, "stderr: ")
		}
		if cur == nil {
			continue
		}
		if c, ok := strings.CutPrefix(l, "        "); ok && last != nil {
			switch {
			case c == "|":
				last.lines = append(last.lines, "")
			case strings.HasPrefix(c, "| "):
				last.lines = append(last.lines, c[2:])
			}
			continue
		}
		if v, ok := strings.CutPrefix(l, "message: "); ok {
			cur.refusal = v // kept only when nothing was saved, below
		}
		if v, ok := strings.CutPrefix(l, "result "); ok && !strings.HasSuffix(v, ": ok") {
			t.Fatalf("%s: a run that failed, which the harness does not compare: %q", rel, l)
		}
		if v, ok := strings.CutPrefix(l, "config.SaveConfig: "); ok && v != "ok" && v != "not called" {
			t.Fatalf("%s: a save that failed, which the harness does not compare: %q", rel, l)
		}
		m := traceLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		op, arg, res := m[1], m[2], m[3]
		if strings.HasPrefix(res, "error") && !(op == "exec" && slices.Contains(probes, arg)) && op != "read" && op != "stat" {
			t.Fatalf("%s: a change that failed, which the harness does not compare: %q", rel, l)
		}
		switch op {
		case "read", "stat": // reads leave no end state
		case "exec":
			if slices.Contains(probes, arg) {
				cur.probes = append(cur.probes, arg+" -> "+res)
			} else {
				cur.actions = append(cur.actions, arg)
			}
		case "mkdir":
			p, mode, _ := strings.Cut(arg, " mode=")
			cur.dirs = append(cur.dirs, p+" "+mode)
		case "write":
			w := writeTarget.FindStringSubmatch(arg)
			if w == nil {
				t.Fatalf("%s: a write the parser does not know: %q", rel, l)
			}
			var mode uint32
			if _, err := fmt.Sscanf(w[2], "%o", &mode); err != nil {
				t.Fatal(err)
			}
			// In place: interfaces.OSFileSystem.WriteFile and SaveConfig are
			// os.WriteFile (side-effects.md §1 item 10), whichever the trace's
			// seam recorded.
			f := fileWrite{path: w[1], mode: fs.FileMode(mode)}
			if f.path == "./hardn.yml" {
				cur.yml = &f
				last = cur.yml
			} else {
				cur.files = append(cur.files, f)
				last = &cur.files[len(cur.files)-1]
			}
		default:
			t.Fatalf("%s: an operation the harness does not compare (%s): %q", rel, op, l)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if result == nil || r.live.yml != nil {
		r.live.refusal = "" // an item's message is its refusal only when it saved nothing
	}
	return r
}

// The new path's own directories: its run directory, the journal and the
// backups (engine.md), which are not the host's end state; the journal and
// the backups are recorded as such.
const (
	runDir     = "/run/hardn"
	journalDir = "/var/lib/hardn"
)

func under(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

// newState is the end state h's calls from the nth on left: the host's
// files and hardn.yml (at yml) as they are now, the directories, the
// commands (those in readOnly only read), the backups (the last
// manifest's entries) and whether the journal was written; refusal and
// findings are the caller's. A change it does not compare fails the test.
func newState(t *testing.T, h *hostfake.System, n int, readOnly []string, yml string) endState {
	t.Helper()
	var s endState
	var order []string
	modes := map[string]hostfake.Call{}
	var manifest []byte
	for _, c := range h.Calls()[n:] {
		switch {
		case under(c.Path, runDir):
			continue
		case under(c.Path, backup.DefaultPath):
			if c.Op == "write-atomic" && path.Base(c.Path) == "manifest.json" {
				manifest = c.Data
			}
			continue
		case under(c.Path, journalDir):
			s.journal = s.journal || c.Op == "write-atomic"
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
			s.dirs = append(s.dirs, fmt.Sprintf("%s %04o", c.Path, uint32(c.Mode.Perm())))
		case "chmod":
			s.dirs = append(s.dirs, fmt.Sprintf("chmod %s %04o", c.Path, uint32(c.Mode.Perm())))
		case "run":
			argv := host.FormatArgv(c.Argv)
			switch {
			case !slices.Contains(readOnly, argv):
				s.actions = append(s.actions, argv)
			case !slices.Contains(s.probes, argv):
				s.probes = append(s.probes, argv)
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
		f := fileWrite{path: p, mode: c.Mode.Perm(), lines: strings.Split(strings.TrimSuffix(string(e.Data), "\n"), "\n"), atomic: c.Op == "write-atomic"}
		if p == yml {
			s.yml = &f
			continue
		}
		s.files = append(s.files, f)
	}
	if manifest != nil {
		var m struct{ Entries []struct{ Path, Kind string } }
		if err := json.Unmarshal(manifest, &m); err != nil {
			t.Fatal(err)
		}
		for _, e := range m.Entries {
			switch e.Kind {
			case string(backup.KindFile):
				s.backups = append(s.backups, e.Path)
			case string(backup.KindAbsent):
				s.backups = append(s.backups, e.Path+" (absent before: the rollback removes it)")
			default:
				s.backups = append(s.backups, e.Path+" ("+e.Kind+")")
			}
		}
	}
	return s
}

// planned sets s's refusal and findings from p: the refusal's code, and
// the codes of its changes' findings.
func planned(s endState, p *engine.Plan) endState {
	if err := p.Err(); err != nil {
		s.refusal = output.CodeOf(err)
	}
	for _, c := range p.Changes {
		for _, f := range c.Findings {
			if !slices.Contains(s.findings, f.Code) {
				s.findings = append(s.findings, f.Code)
			}
		}
	}
	return s
}

// TestParity's body: each scenario on each distribution, origin/main's end
// state read from its golden and the new path's run, compared aspect by
// aspect. It fails on a difference sl.table does not list (UNEXPLAINED),
// on an entry whose difference no longer occurs, and on an entry tied to a
// V-number origin/main's golden for the cell does not name; then holds
// the whole table to sl.golden and its summary to sl.page's region. The
// table, the golden and the page are checked only when every cell ran:
// a -run filter checks the cells it runs and no more.
func runParity(t *testing.T, sl paritySlice) {
	t.Helper()
	var cells []parityCell
	for _, sc := range sl.scenarios {
		for _, d := range sl.distros {
			t.Run(sc.name+"/"+d.name, func(t *testing.T) { cells = append(cells, runCell(t, sl, sc, d)) })
		}
	}
	if t.Failed() {
		return
	}
	if all := len(sl.scenarios) * len(sl.distros); len(cells) != all {
		t.Logf("%d of %d cells ran: the table, %s and the page are checked only when every cell runs", len(cells), all, sl.golden)
		return
	}
	checkTable(t, sl, cells)
	if t.Failed() {
		return // neither rendering is written while the table fails
	}
	golden(t, sl.golden, []byte(renderCells(sl, cells)))
	page(t, sl.page, renderPage(sl, cells))
}

// runCell runs sc on d: origin/main's end state from its golden, the new
// path's, and their comparison.
func runCell(t *testing.T, sl paritySlice, sc parityScenario, d parityDistro) parityCell {
	t.Helper()
	c := parityCell{sc: sc, d: d, state: sc.state}
	if c.state == (hostState{}) {
		c.state = d.stock
	}
	c.shipped = sl.shipped(c.state, d)
	c.legacy = readLegacy(t, sl.probes, strings.ReplaceAll(sc.golden, "{d}", d.name))
	if c.why = sl.exists(c.state, d); c.why != "" {
		return c
	}
	var loaded []string
	if sc.fixture != "" {
		src, err := os.ReadFile(filepath.Join(fixtures, sc.fixture))
		if err != nil {
			t.Fatal(err)
		}
		loaded = strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	}
	c.run = sl.run(t, c)
	c.legacy.live.loaded, c.run.live.loaded = loaded, loaded
	c.diffs, c.agree = compare(t, sl, c.legacy.live, c.run.live)
	if sc.lines == nil {
		c.diffs = append(c.diffs, aspect{"dry run", legacyDry(c.legacy), c.run.dry})
	}
	return c
}

// legacyDry is what origin/main's cfg.DryRun=true section did, and whether
// any path reaches it.
func legacyDry(r legacyRun) string {
	did := "the service writes " + joined(names(r.dry.files)) + " and runs " + joined(r.dry.actions)
	if hostText(r.dry) == hostText(r.live) {
		did = "the service writes and runs as live (cfg.DryRun unread below the menus)"
	}
	if r.cli == "" {
		return did
	}
	status, _, _ := strings.Cut(r.cli, ":")
	return did + "; no path reaches it: hardn -n refused (" + status + ")"
}

// hostText is what s changed: the files with their content, the
// directories, the commands and hardn.yml.
func hostText(s endState) string {
	var b strings.Builder
	for _, f := range append(slices.Clone(s.files), ymlOf(s)...) {
		fmt.Fprintf(&b, "%s\n%s\n", f, strings.Join(f.lines, "\n"))
	}
	fmt.Fprintf(&b, "dirs %q\nactions %q\n", s.dirs, s.actions)
	return b.String()
}

func ymlOf(s endState) []fileWrite {
	if s.yml == nil {
		return nil
	}
	return []fileWrite{*s.yml}
}

// compare is l and n, origin/main's end state and the new path's, aspect by
// aspect: the aspects that differ and those that agree, each with both
// values. An aspect of what was written is compared only when both wrote:
// the files' content (sl.content) when both wrote a host file, whatever
// its path; hardn.yml's when both saved it.
func compare(t *testing.T, sl paritySlice, l, n endState) (diffs, agree []aspect) {
	t.Helper()
	add := func(name, lv, nv string) {
		if lv == nv {
			agree = append(agree, aspect{name, lv, nv})
		} else {
			diffs = append(diffs, aspect{name, lv, nv})
		}
	}
	add("refusal", orElse(l.refusal, "none"), orElse(n.refusal, "none"))
	add("files", joined(names(l.files)), joined(names(n.files)))
	if len(l.files) > 0 && len(n.files) > 0 {
		sl.content(add, l.files, n.files)
		for _, lf := range l.files {
			if i := slices.IndexFunc(n.files, func(f fileWrite) bool { return f.path == lf.path }); i >= 0 {
				add("mode", fmt.Sprintf("%04o", uint32(lf.mode)), fmt.Sprintf("%04o", uint32(n.files[i].mode)))
			}
		}
	}
	add("directories", joined(l.dirs), joined(n.dirs))
	add("commands", joined(l.actions), joined(n.actions))
	if l.wrote() && n.wrote() {
		add("write", method(l), method(n))
		add("backup", joined(l.backups), joined(n.backups))
		add("journal", journal(l), journal(n))
	}
	add("findings", joined(l.findings), joined(n.findings))
	add("hardn.yml", saved(l), saved(n))
	if l.yml != nil && n.yml != nil {
		add("hardn.yml "+sl.setting, settingOf(t, l.yml.lines, sl.setting), settingOf(t, n.yml.lines, sl.setting))
		add("hardn.yml settings", settingsVs(t, l.yml.lines, l.loaded, sl.setting), settingsVs(t, n.yml.lines, n.loaded, sl.setting))
		add("hardn.yml rest", rest(*l.yml, l.loaded, sl.setting), rest(*n.yml, n.loaded, sl.setting))
		add("hardn.yml mode", fmt.Sprintf("%04o", uint32(l.yml.mode)), fmt.Sprintf("%04o", uint32(n.yml.mode)))
	}
	return diffs, agree
}

func orElse(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

func joined(l []string) string { return orElse(strings.Join(l, ", "), "none") }

func names(fs []fileWrite) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

func method(s endState) string {
	for _, f := range append(slices.Clone(s.files), ymlOf(s)...) {
		if !f.atomic {
			return "in place"
		}
	}
	return "atomic"
}

func journal(s endState) string {
	if s.journal {
		return "journalled first"
	}
	return "none"
}

func saved(s endState) string {
	switch {
	case s.yml == nil:
		return "not saved"
	case s.yml.path == "./hardn.yml":
		return "./hardn.yml, in the working directory"
	}
	return s.yml.path + ", the file loaded"
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
		return joined(out)
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
// settings and how many of the file's comment lines it kept.
func rest(f fileWrite, loaded []string, key string) string {
	keys, kept, comments := 0, 0, 0
	for _, l := range f.lines {
		if topLevelKey.MatchString(l) {
			keys++
		}
	}
	for _, l := range loaded {
		if strings.HasPrefix(l, "#") {
			comments++
			if slices.Contains(f.lines, l) {
				kept++
			}
		}
	}
	if others := outside(f.lines, key); loaded != nil && slices.Equal(others, outside(loaded, key)) {
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

// checkTable holds every difference to sl.table: none unlisted, every
// entry occurring, each V-number of a source one origin/main's golden for
// the cell names.
func checkTable(t *testing.T, sl paritySlice, cells []parityCell) {
	t.Helper()
	want, twice := expand(sl.table)
	for _, k := range twice {
		t.Errorf("the table lists %s on %s, %s twice", k.scenario, k.distro, k.aspect)
	}
	seen := map[key]bool{}
	for _, c := range cells {
		for _, d := range c.diffs {
			k := key{c.sc.name, c.d.name, d.name}
			seen[k] = true
			e, ok := want[k]
			if !ok {
				t.Errorf("UNEXPLAINED %s on %s, %s: origin/main %q, new %q", c.sc.name, c.d.name, d.name, d.legacy, d.new)
				continue
			}
			for _, v := range vNumber.FindAllString(e.source, -1) {
				if !slices.Contains(c.legacy.vs, v) {
					t.Errorf("%s on %s, %s: tied to %s, which %s does not name (it names %v)", c.sc.name, c.d.name, d.name, v, c.legacy.golden, c.legacy.vs)
				}
			}
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("the table lists %s on %s, %s, which no longer differs", k.scenario, k.distro, k.aspect)
		}
	}
}

var vNumber = regexp.MustCompile(`\bV\d+\b`)

// expand is table by key, and the keys it lists more than once.
func expand(table []tie) (map[key]tie, []key) {
	out := map[key]tie{}
	var twice []key
	for _, e := range table {
		for _, s := range e.scenarios {
			for _, d := range e.distros {
				k := key{s, d, e.aspect}
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
func renderCells(sl paritySlice, cells []parityCell) string {
	want, _ := expand(sl.table)
	var b strings.Builder
	b.WriteString(sl.header)
	for _, c := range cells {
		fmt.Fprintf(&b, "\n== %s on %s\n", c.sc.name, c.d.label)
		fmt.Fprintf(&b, "host: %s (%s)\n", c.state.long, c.shipped)
		if c.sc.fixture == "" {
			b.WriteString("hardn.yml: none loaded\n")
		}
		fmt.Fprintf(&b, "origin/main: %s (%s)\n", c.legacy.golden, strings.Join(c.legacy.vs, ", "))
		if c.sc.note != "" {
			fmt.Fprintf(&b, "  %s\n", c.sc.note)
		}
		writeState(&b, c.legacy.live)
		if c.legacy.cli != "" {
			fmt.Fprintf(&b, "  hardn -n: refused, %s\n", c.legacy.cli)
		}
		if c.why != "" {
			fmt.Fprintf(&b, "not compared: %s\n", c.why)
			continue
		}
		fmt.Fprintf(&b, "new path: %s\n", c.run.via)
		writeState(&b, c.run.live)
		for _, a := range c.agree {
			fmt.Fprintf(&b, "agree  %s: %s\n", a.name, a.legacy)
		}
		for _, d := range c.diffs {
			fmt.Fprintf(&b, "differ %s: %s | %s\n", d.name, d.legacy, d.new)
			if e, ok := want[key{c.sc.name, c.d.name, d.name}]; ok {
				fmt.Fprintf(&b, "       %s: %s\n", e.source, e.why)
			} else {
				b.WriteString("       UNEXPLAINED\n")
			}
		}
		fmt.Fprintf(&b, "v0.3.2: %s\n", c.sc.v032)
	}
	return b.String()
}

// writeState is s, indented: the probes, each file written with its
// content (hardn.yml as its lines changed against the file loaded, or its
// size when written whole), the directories, the commands, and what the
// new path adds.
func writeState(b *strings.Builder, s endState) {
	if len(s.probes) > 0 {
		fmt.Fprintf(b, "  probes: %s\n", strings.Join(s.probes, "; "))
	}
	if s.refusal != "" {
		fmt.Fprintf(b, "  refused: %s\n", s.refusal)
	}
	for _, f := range s.files {
		fmt.Fprintf(b, "  write %s, %s\n", f, method(endState{files: []fileWrite{f}}))
		for _, l := range f.lines {
			b.WriteString(strings.TrimRight("    | "+l, " ") + "\n")
		}
	}
	if s.yml != nil {
		fmt.Fprintf(b, "  write %s %04o, %s\n", s.yml.path, uint32(s.yml.mode), method(endState{yml: s.yml}))
		if s.yml.path != "./hardn.yml" { // origin/main's whole file is in its own golden
			for _, l := range lineDiff(s.loaded, s.yml.lines) {
				fmt.Fprintf(b, "    %s\n", l)
			}
		}
	}
	for _, d := range s.dirs {
		if strings.HasPrefix(d, "chmod ") {
			fmt.Fprintf(b, "  %s\n", d)
		} else {
			fmt.Fprintf(b, "  mkdir %s\n", d)
		}
	}
	for _, a := range s.actions {
		fmt.Fprintf(b, "  run %s\n", a)
	}
	if s.journal || len(s.backups) > 0 || len(s.findings) > 0 {
		fmt.Fprintf(b, "  backup: %s; journal: %s; findings: %s\n", joined(s.backups), journal(s), joined(s.findings))
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
func renderPage(sl paritySlice, cells []parityCell) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- TestParity: begin. Generated by TestParity (%s); make golden-update rewrites it. Do not edit. -->\n\n", sl.pkg)
	b.WriteString("**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it (real), the harness forced it, or it cannot exist.\n\n")
	b.WriteString("| Scenarios |")
	for _, d := range sl.distros {
		b.WriteString(" " + d.label + " |")
	}
	b.WriteString("\n|---|" + strings.Repeat("---|", len(sl.distros)) + "\n")
	var rows, labels []string
	by := map[string][]string{}
	for _, sc := range sl.scenarios {
		var r strings.Builder
		for _, c := range cells {
			if c.sc.name == sc.name {
				word, _, _ := strings.Cut(c.shipped, ":")
				fmt.Fprintf(&r, " %s (%s) |", c.state.short, word)
			}
		}
		if _, ok := by[r.String()]; !ok {
			rows = append(rows, r.String())
		}
		by[r.String()] = append(by[r.String()], sc.name)
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
				if _, ok := count[a.name]; !ok {
					aspects = append(aspects, a.name)
				}
				n := count[a.name]
				n[i]++
				count[a.name] = n
			}
		}
	}
	for _, a := range aspects {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", a, count[a][0], count[a][1])
	}
	fmt.Fprintf(&b, "\n**Differences** (§4): each with its source; every cell, both end states whole, is in [%s](../../%s/testdata/golden/%s).\n\n", sl.golden, sl.pkg, sl.golden)
	b.WriteString("| Scenarios | On | Aspect | origin/main | New path | Source |\n|---|---|---|---|---|---|\n")
	type group struct {
		a     aspect
		e     tie
		cells [][2]string // scenario, distribution
	}
	var groups []*group
	want, _ := expand(sl.table)
	for _, c := range cells {
		for _, d := range c.diffs {
			e, ok := want[key{c.sc.name, c.d.name, d.name}]
			if !ok {
				e = tie{source: "UNEXPLAINED"}
			}
			i := slices.IndexFunc(groups, func(g *group) bool { return g.a == d && g.e.source == e.source && g.e.why == e.why })
			if i < 0 {
				groups = append(groups, &group{a: d, e: e})
				i = len(groups) - 1
			}
			groups[i].cells = append(groups[i].cells, [2]string{c.sc.name, c.d.label})
		}
	}
	for _, g := range groups {
		scenarios, on := where(g.cells, len(sl.distros))
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s: %s |\n", scenarios, on, g.a.name, cell(g.a.legacy), cell(g.a.new), g.e.source, g.e.why)
	}

	b.WriteString("\n**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md) §4.5, §4.13, §5.1, §6.\n\n")
	b.WriteString("| Scenarios | v0.3.2 |\n|---|---|\n")
	var done []string
	for _, sc := range sl.scenarios {
		if slices.Contains(done, sc.v032) {
			continue
		}
		done = append(done, sc.v032)
		var names []string
		for _, o := range sl.scenarios {
			if o.v032 == sc.v032 {
				names = append(names, o.name)
			}
		}
		fmt.Fprintf(&b, "| %s | %s |\n", strings.Join(names, ", "), sc.v032)
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
