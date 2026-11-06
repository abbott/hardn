package systemdetails_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/internal/parity"
	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/screens/logs"
	"github.com/abbott/hardn/pkg/screens/systemdetails"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The driver of TestParity's new path (parity-system-details.md §7): the screens'
// own Run and Print on hostfake, typed as each row needs, and a reader of what they
// drew, in plain mode, so that every row of the table's new side is a line of the
// screen. As the reader of origin/main's goldens does, it fails on a line it does
// not know, so that nothing on the screen goes unread.

// parityNow is the screens' clock: the goldens' logins are this year's.
func parityNow() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }

// tallRows is the height a Logs cell draws at, so that every line logview.Read keeps
// shows and is compared; holdFitted holds the same screen at 80x24.
const tallRows = 2*256 + 40

// plainTerm is a plain terminal of rows rows: 80 columns, no colour, ASCII glyphs.
func plainTerm(rows int) term.Env {
	env := term.Resolve(term.Options{}, term.Facts{Width: 80})
	env.Height = rows
	return env
}

// drawnRow is a row of a section as the screen drew it: the words of an item-less
// state are its key alone.
type drawnRow struct{ key, value string }

// drawnSection is a section as drawn: its rows, or its lines of code, each whole
// again where it wrapped.
type drawnSection struct {
	title string
	rows  []drawnRow
	code  []string
}

// drawing is one draw of a screen, from its title bar to its prompt.
type drawing struct {
	title    string
	sections []drawnSection
	heading  string
	items    []string
	prompt   string
}

// row is the section's row keyed key, "" and false where none is.
func (s drawnSection) row(key string) (string, bool) {
	i := slices.IndexFunc(s.rows, func(r drawnRow) bool { return r.key == key })
	if i < 0 {
		return "", false
	}
	return s.rows[i].value, true
}

// section is the section titled title, empty where none is.
func (d drawing) section(title string) drawnSection {
	i := slices.IndexFunc(d.sections, func(s drawnSection) bool { return s.title == title })
	if i < 0 {
		return drawnSection{}
	}
	return d.sections[i]
}

// sectionTitles are the sections' titles, joined.
func (d drawing) sectionTitles() string {
	var out []string
	for _, s := range d.sections {
		out = append(out, s.title)
	}
	return strings.Join(out, ", ")
}

// The lines of a screen in plain mode (pkg/ui/screen's SubMenu and Section,
// pkg/ui/component's rows, menu and prompt).
var (
	drawnTitle   = regexp.MustCompile(`^# (.+?) #+$`)
	drawnHeader  = regexp.MustCompile(`^  (\S.*?) -+$`)
	drawnBullet  = regexp.MustCompile(`^(?: {3}- {2}| {2}\[.\] )(\S.*)$`)
	drawnItem    = regexp.MustCompile(`^ {3}(\d+)\) (\S+)(?: {2,}(\S.*))?$`)
	drawnPrompt  = regexp.MustCompile(`^ {4}> (Enter selection \[0-\d+ or q\]):(?: .*)?$`)
	drawnRefused = regexp.MustCompile(`^ {2}\(x\) (.+)$`)
)

const (
	drawnCode    = "      " // a line of code's indent, theme.LabelCol
	drawnWrap    = "|"      // plain mode's Glyphs.Wrap
	drawnSubMenu = "Select an option"
)

// screenRead is what a run of a screen drew: each draw, and what it said under its
// prompt to a line it refused.
type screenRead struct {
	draws   []drawing
	refused []string
}

// readScreen reads out, a run in plain mode, failing t where readDrawn refuses it.
func readScreen(t *testing.T, out string) screenRead {
	t.Helper()
	r, err := readDrawn(out)
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	return r
}

// readDrawn reads out, a run in plain mode. An escape, a line it does not know and a
// run with no draw are errors, so that nothing on the screen goes unread.
func readDrawn(out string) (screenRead, error) {
	var r screenRead
	last := func() *drawing { return &r.draws[len(r.draws)-1] }
	section := func() *drawnSection { return &last().sections[len(last().sections)-1] }
	for _, l := range strings.Split(out, "\n") {
		if strings.ContainsAny(l, "\x1b\x07") {
			return r, fmt.Errorf("an escape in plain mode: %q", l)
		}
		m := drawnBullet.FindStringSubmatch(l)
		item, ask, no := drawnItem.FindStringSubmatch(l), drawnPrompt.FindStringSubmatch(l), drawnRefused.FindStringSubmatch(l)
		title, head := drawnTitle.FindStringSubmatch(l), drawnHeader.FindStringSubmatch(l)
		switch {
		case l == "":
		case title != nil:
			r.draws = append(r.draws, drawing{title: title[1]})
		case len(r.draws) == 0:
			return r, fmt.Errorf("a line before the title bar: %q", l)
		case head != nil:
			last().sections = append(last().sections, drawnSection{title: head[1]})
		case m != nil && len(last().sections) > 0:
			key, value, _ := strings.Cut(m[1], "  ")
			section().rows = append(section().rows, drawnRow{strings.TrimSpace(key), strings.TrimSpace(value)})
		case strings.HasPrefix(l, drawnCode) && len(last().sections) > 0:
			code := strings.TrimPrefix(l, drawnCode)
			if rest, ok := strings.CutPrefix(code, drawnWrap); ok && len(section().code) > 0 {
				section().code[len(section().code)-1] += rest
				continue
			}
			section().code = append(section().code, code)
		case l == "  "+drawnSubMenu:
			last().heading = drawnSubMenu
		case item != nil:
			words := item[1] + ") " + item[2]
			if item[3] != "" {
				words += ": " + item[3]
			}
			last().items = append(last().items, words)
		case ask != nil:
			last().prompt = ask[1]
		case no != nil:
			r.refused = append(r.refused, no[1])
		default:
			return r, fmt.Errorf("a line of the screen this reader does not know: %q", l)
		}
	}
	if len(r.draws) == 0 {
		return r, errors.New("no draw: a run that did nothing")
	}
	return r, nil
}

// TestDrawnReaderRefuses: what readDrawn does not know fails, as the reader of
// origin/main's goldens does.
func TestDrawnReaderRefuses(t *testing.T) {
	const screen = "# Logs #####\n\n  File -----\n\n   -  Path                 /var/log/hardn.log\n\n  Select an option\n\n   1) Refresh              Read the log again\n\n   0) Return\n\n    > Enter selection [0-1 or q]: 0\n"
	if _, err := readDrawn(screen); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, out, want string }{
		{"nothing", "", "no draw"},
		{"a line before the title", "a line\n" + screen, "before the title"},
		{"a line it does not know", strings.Replace(screen, "  Select an option", "  Choose one", 1), "does not know"},
		{"another prompt", strings.Replace(screen, "Enter selection", "Enter a key", 1), "does not know"},
		{"a pause", strings.Replace(screen, "   0) Return", "   0) Return\n  - Press Enter to continue...", 1), "does not know"},
		{"an escape", strings.Replace(screen, "/var/log/hardn.log", "\x1b[31m/var/log/hardn.log", 1), "an escape"},
		{"a row before a section", strings.Replace(screen, "  File -----\n", "", 1), "does not know"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := readDrawn(tt.out); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want an error with %q", err, tt.want)
			}
		})
	}
}

// holdLater fails unless each later draw is the first: nothing on these hosts moves
// between two draws.
func holdLater(t *testing.T, r screenRead) {
	t.Helper()
	for _, d := range r.draws[1:] {
		if fmt.Sprintf("%+v", d) != fmt.Sprintf("%+v", r.draws[0]) {
			t.Errorf("a later draw differs from the first:\n%+v\n%+v", r.draws[0], d)
		}
	}
}

// typed runs run with a prompter that types lines, and fails t unless it ends as a
// typed line ended it, every line read.
func typed(t *testing.T, env term.Env, lines []string, run func(nav.Prompter) error) string {
	t.Helper()
	var out bytes.Buffer
	ty := uitest.NewTypist(&out, lines...)
	err := run(nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)})
	if err != nil && !errors.Is(err, nav.ErrQuit) {
		t.Errorf("typed %q: %v", lines, err)
	}
	if ty.Unread() != 0 {
		t.Errorf("typed %q: %d lines unread", lines, ty.Unread())
	}
	return out.String()
}

// runReport is the System Details screen's Run on sys, typed lines, in plain mode.
func runReport(t *testing.T, sys host.System, facts detect.Facts, lines []string) string {
	t.Helper()
	return typed(t, plainTerm(24), lines, func(p nav.Prompter) error {
		return systemdetails.Run(context.Background(), systemdetails.Options{Prompter: p, System: sys, Facts: facts, Now: parityNow})
	})
}

// runLogs is the Logs screen's Run on sys, typed lines, at rows rows.
func runLogs(t *testing.T, sys host.System, logFile string, rows int, lines []string) string {
	t.Helper()
	return typed(t, plainTerm(rows), lines, func(p nav.Prompter) error {
		return logs.Run(context.Background(), logs.Options{Prompter: p, System: sys, LogFile: logFile, Term: parityTerm})
	})
}

// parityTerm is the TERM the Logs cells give the screen.
const parityTerm = "xterm-256color"

// holdWhole fails unless Print off a terminal, on sys, draws the screen's rows, each
// value whole: the screen's own row is the same, or its cut. The report's value too
// long for a row is cut on a terminal and whole off one (gallery-system-details.md
// §5, Made here).
func holdWhole(t *testing.T, sys host.System, facts detect.Facts, d drawing) {
	t.Helper()
	var out bytes.Buffer
	if err := systemdetails.Print(context.Background(), systemdetails.PrintOptions{Out: &out, Env: plainTerm(24), System: sys, Facts: facts, Now: parityNow}); err != nil {
		t.Fatal(err)
	}
	off := readScreen(t, out.String()).draws[0]
	if off.sectionTitles() != d.sectionTitles() || off.title != d.title || len(off.items) != 0 || off.prompt != "" {
		t.Errorf("Print off a terminal: %+v, where the screen draws %+v", off, d)
	}
	for _, s := range d.sections {
		o := off.section(s.title).rows
		if len(o) != len(s.rows) {
			t.Errorf("%s: Print's rows %+v, the screen's %+v", s.title, o, s.rows)
			continue
		}
		for i, r := range s.rows {
			if o[i].key != r.key {
				t.Errorf("%s: Print's rows %+v, the screen's %+v", s.title, o, s.rows)
				break
			}
			cut, isCut := strings.CutSuffix(r.value, "...")
			if whole := o[i].value; !isCut && whole != r.value || isCut && (len(whole) <= len(r.value) || !strings.HasPrefix(whole, cut)) {
				t.Errorf("%s, %s: Print says %q, the screen %q", s.title, r.key, whole, r.value)
			}
		}
	}
}

// reportRowsOf are the report's rows as the screen drew them, under the label of
// origin/main's row each replaces, and beside the fact where origin/main's form
// holds figures the drawn words round: Cores, Volume and Memory stay the fact in
// origin/main's form, so that the same numbers agree and a wrong reading fails, and
// the row drawn is its own aspect, held to draw that fact (holdDrawn). facts is the
// report on a twin of the screen's host, so that the screen's host holds its own
// reads alone. The first row is the report.
func reportRowsOf(t *testing.T, d drawing, facts details.Report) []parity.Row {
	t.Helper()
	var rows []parity.Row
	add := func(name, value string) {
		if value != "" {
			rows = append(rows, parity.Row{Name: name, Value: value})
		}
	}
	holdKnown(t, d)
	osKernel, hostIPs, cpu, disk, memory := d.section("OS and Kernel"), d.section("Host and IPs"), d.section("CPU and Load"), d.section("Disk"), d.section("Memory")
	drawnValue := func(s drawnSection, key string) string {
		v, _ := s.row(key)
		return v
	}
	add("OS", drawnValue(osKernel, "OS"))
	add("Kernel", drawnValue(osKernel, "Kernel"))
	add("Hostname", drawnValue(hostIPs, "Hostname"))
	var ips []string
	for _, r := range hostIPs.rows {
		if r.key != "Hostname" && !strings.HasPrefix(r.key, "Can't read ") {
			ips = append(ips, strings.TrimSpace(r.key+" "+r.value))
		}
	}
	add("IP Addresses", strings.Join(ips, ", "))
	var users []string
	for _, r := range d.section("Users").rows {
		users = append(users, r.key+" ("+r.value+")")
	}
	add("Non-System Users", strings.Join(users, ", "))
	add("Processor", drawnValue(cpu, "Model"))
	add(rowCPUs, drawnValue(cpu, "CPUs"))
	if facts.CPU.CPUs > 0 {
		add("Cores", fmt.Sprintf("%d vCPU(s) / %d Socket(s)", facts.CPU.CPUs, facts.CPU.Sockets))
	}
	load := strings.SplitN(drawnValue(cpu, "Load (1, 5, 15 min)"), ", ", 3)
	for i, name := range []string{"Load 1m", "Load 5m", "Load 15m"} {
		if len(load) == 3 {
			add(name, load[i])
		}
	}
	add("/", drawnValue(disk, "/"))
	used := drawnValue(disk, "Used")
	add(rowDiskUsed, used)
	if facts.Disk.Known {
		add("Volume", sizes(facts.Disk.Used, facts.Disk.Total, ""))
	}
	if unit := usedUnit.FindStringSubmatch(used); unit != nil {
		add(rowVolumeUnit, unit[1])
	}
	for _, r := range disk.rows {
		if pool, ok := strings.CutPrefix(r.key, "Pool "); ok {
			add("ZFS Health", pool+" "+r.value)
		}
	}
	add(rowMemoryUsed, drawnValue(memory, "Used"))
	if u, ok := facts.Memory.Used(); ok {
		add("Memory", sizes(u, facts.Memory.Total, "GiB"))
	}
	when, from := loginRowsOf(d.section("Last Login"))
	add("Last Login", when)
	add("From", from)
	add("Uptime", drawnValue(osKernel, "Uptime"))
	holdDrawn(t, d, facts)
	return append([]parity.Row{{Name: reportRow, Value: keptWords(d, rows)}}, rows...)
}

// knownRows are the rows a section with fixed words may hold, so that no row of them
// goes unread; "Can't read <source>" stands in any section, and the others' rows are
// the host's own names.
var knownRows = map[string][]string{
	"OS and Kernel": {"OS", "Kernel", "Uptime"},
	"CPU and Load":  {"Model", "CPUs", "Load (1, 5, 15 min)"},
	"Disk":          {"/", "Used"},
	"Memory":        {"Used"},
}

// holdKnown fails on a row of the report's sections its rows do not read.
func holdKnown(t *testing.T, d drawing) {
	t.Helper()
	for _, s := range d.sections {
		for _, r := range s.rows {
			known, fixed := knownRows[s.title]
			if fixed && !slices.Contains(known, r.key) && !strings.HasPrefix(r.key, "Can't read ") && !strings.HasPrefix(r.key, "Pool ") {
				t.Errorf("%s: a row this reader does not know: %q", s.title, r.key)
			}
		}
	}
}

// usedUnit is the unit in a Used row's words.
var usedUnit = regexp.MustCompile(`^[0-9.]+ of [0-9.]+ (\S+) \(\d+%\)$`)

// loginFrom is the host in a login's row.
var loginFrom = regexp.MustCompile(` from (\S+)$`)

// loginRowsOf are Last Login's rows: each account and what the row says of its
// newest login, then each account's address, as origin/main's two rows are.
func loginRowsOf(s drawnSection) (when, from string) {
	var w, f []string
	for _, r := range s.rows {
		w = append(w, strings.TrimSpace(r.key+" "+r.value))
		if m := loginFrom.FindStringSubmatch(r.value); m != nil {
			f = append(f, r.key+" "+m[1])
		}
	}
	return strings.Join(w, ", "), strings.Join(f, ", ")
}

// holdDrawn fails unless the rows whose fact is apart from them draw it: CPUs, the
// disk's and the memory's Used, in gallery-system-details.md §6's words. It is the
// test's own sum, so that a screen drawing a wrong figure fails here and not only in
// the golden, where a difference already tied would pass with -update.
func holdDrawn(t *testing.T, d drawing, facts details.Report) {
	t.Helper()
	sockets := func(n int) string {
		if n == 1 {
			return "1 socket"
		}
		return strconv.Itoa(n) + " sockets"
	}
	if got, _ := d.section("CPU and Load").row("CPUs"); facts.CPU.CPUs > 0 && got != fmt.Sprintf("%d, %s", facts.CPU.CPUs, sockets(facts.CPU.Sockets)) {
		t.Errorf("CPUs %q does not draw %d CPUs on %d sockets", got, facts.CPU.CPUs, facts.CPU.Sockets)
	}
	words := func(used, total uint64) string {
		const gib = float64(1 << 30)
		return fmt.Sprintf("%.1f of %.1f GiB (%d%%)", float64(used)/gib, float64(total)/gib, (used*100+total/2)/total)
	}
	if got, _ := d.section("Disk").row("Used"); facts.Disk.Known && got != words(facts.Disk.Used, facts.Disk.Total) {
		t.Errorf("the disk's Used %q does not draw %d of %d bytes", got, facts.Disk.Used, facts.Disk.Total)
	}
	if u, ok := facts.Memory.Used(); ok {
		if got, _ := d.section("Memory").row("Used"); got != words(u, facts.Memory.Total) {
			t.Errorf("the memory's Used %q does not draw %d of %d bytes", got, u, facts.Memory.Total)
		}
	}
}

// keptWords says what the report kept: whole, or the rows that hold a fact and the
// words each section said in place of what it could not read.
func keptWords(d drawing, rows []parity.Row) string {
	var not []string
	for _, s := range d.sections {
		for _, r := range s.rows {
			if strings.HasPrefix(r.key, "Can't read ") {
				not = append(not, s.title+": "+r.key)
			}
			if r.key == "Logins" {
				not = append(not, s.title+": "+r.value)
			}
		}
	}
	if len(not) == 0 {
		return reportWhole
	}
	var kept []string
	for _, row := range rows {
		if !slices.Contains([]string{rowVolumeUnit, rowCPUs, rowDiskUsed, rowMemoryUsed}, row.Name) && !strings.Contains(row.Value, "lslogins isn't installed") {
			kept = append(kept, row.Name)
		}
	}
	return "kept " + strings.Join(kept, ", ") + "; " + strings.Join(not, ", ")
}

// menuRowsOf are the rows of the screen's menu, among the lines
// parity-system-details.md §2 lists: its items, heading and prompt.
func menuRowsOf(d drawing) []parity.Row {
	return []parity.Row{{Name: rowItems, Value: strings.Join(d.items, "; ")}, {Name: rowMenuHeading, Value: d.heading}, {Name: rowPrompt, Value: d.prompt}}
}

// holdFitted fails unless the Logs screen at 80x24 holds its prompt within the
// terminal, under the last lines of those drawn at tallRows, its header their count:
// the last lines fitted from the end of what logview.Read keeps (decision 8 (a)).
func holdFitted(t *testing.T, sys host.System, logFile string, all []string, total int) {
	t.Helper()
	out := runLogs(t, sys, logFile, 24, []string{"0"})
	if n := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; n > 24 {
		t.Errorf("%d rows at 80x24:\n%s", n, out)
	}
	d := readScreen(t, out).draws[0]
	s := d.sections[len(d.sections)-1]
	if len(s.code) == 0 || !slices.Equal(s.code, all[len(all)-len(s.code):]) {
		t.Errorf("at 80x24 %q are not the end of %q", s.code, all)
	}
	want := fmt.Sprintf("Last %d of %d lines", len(s.code), total)
	switch {
	case total == 1:
		want = "1 line"
	case len(s.code) == total:
		want = fmt.Sprintf("All %d lines", total)
	}
	if s.title != want {
		t.Errorf("at 80x24 the header %q over %d of %d lines, want %q", s.title, len(s.code), total, want)
	}
}
