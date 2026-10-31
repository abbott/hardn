package screen

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The System Details and Logs screens' readings the tests draw, as the gallery's
// hosts read (gallery-system-details.md §3.1): synthetic values, alice, web-01.
var sdDebian = SystemDetails{
	OS:     []component.KeyValue{{Key: "OS", Value: "Debian 13 (Trixie)"}, {Key: "Kernel", Value: "6.12.113+deb13-cloud-amd64"}, {Key: "Uptime", Value: "3 days, 5 hours, 12 minutes"}},
	Host:   []component.KeyValue{{Key: "Hostname", Value: "web-01"}, {Key: "eth0", Value: "192.0.2.20/24"}},
	Users:  []component.KeyValue{{Key: "alice", Value: "sudo"}},
	CPU:    []component.KeyValue{{Key: "Model", Value: "QEMU Virtual CPU version 2.5+"}, {Key: "CPUs", Value: "4, 1 socket"}, {Key: "Load (1, 5, 15 min)", Value: "0.12, 0.08, 0.03"}},
	Disk:   []component.KeyValue{{Key: "/", Value: "/dev/vda1 ext4"}, {Key: "Used", Value: "4.2 of 38.6 GiB (11%)"}},
	Memory: []component.KeyValue{{Key: "Used", Value: "1.2 of 3.8 GiB (30%)"}},
	Logins: []component.KeyValue{{Key: "root", Value: "None recorded", State: component.ValueNone}, {Key: "alice", Value: "Oct 20 11:42 from 192.0.2.10"}},
}

// logLines are n lines of the shape hardn writes, each under the row's width.
func logLines(n int) []component.CodeLine {
	var out []component.CodeLine
	for i := range n {
		out = append(out, component.CodeLine{Text: fmt.Sprintf("2026/10/20 12:00:%02d INFO: run %d: exit 0", i%60, i)})
	}
	return out
}

// plainAt is the render context of a terminal of that height, in Unicode or ASCII.
func plainAt(rows int, unicode bool) component.Ctx {
	env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
	env.Height = rows
	return component.NewCtx(env)
}

// drawn is a drawing in plain text, line by line.
func drawn(s string) []string { return strings.Split(ansi.Strip(s), "\n") }

// TestSystemDetailsScreen: the report's seven sections in P-17's order, in the
// decided words; item 1 Refresh; Return; the decided heading and prompt; one form
// at every height, 44 lines on Debian 13, its top scrolling off, the prompt last
// (decision 4 (a)); no menu and no prompt in the report; Screen draws what Render
// renders.
func TestSystemDetailsScreen(t *testing.T) {
	titles := []string{"OS and Kernel", "Host and IPs", "Users", "CPU and Load", "Disk", "Memory", "Last Login"}
	sub := sdDebian.SubMenu()
	var got []string
	for _, s := range sub.Sections {
		got = append(got, s.Header.Title)
	}
	if !slices.Equal(got, titles) || sub.Title.Title != "System Details" || sub.Heading != "Select an option" || sub.Prompt != "Enter selection" {
		t.Errorf("sections %q, title %q, heading %q, prompt %q", got, sub.Title.Title, sub.Heading, sub.Prompt)
	}
	want := []component.MenuItem{{Key: SystemDetailsRefreshItem, Label: "Refresh", Description: "Read the host again"}}
	if !slices.Equal(sub.Menu.Items, want) || sub.Menu.Exit.Key != "0" || sub.Menu.Exit.Label != "Return" {
		t.Errorf("items %+v, exit %+v", sub.Menu.Items, sub.Menu.Exit)
	}
	for _, rows := range []int{24, 40} {
		for _, unicode := range []bool{true, false} {
			c := plainAt(rows, unicode)
			lines := drawn(sub.Render(c))
			if len(lines) != 44 || !strings.HasSuffix(lines[43], " or q]:") {
				t.Errorf("80x%d, unicode %t: %d lines, the last %q", rows, unicode, len(lines), lines[len(lines)-1])
			}
			report := sdDebian.Report(c)
			if l := drawn(report); len(l) != 36 || strings.Contains(report, "Refresh") || strings.Contains(report, "Enter selection") {
				t.Errorf("80x%d, unicode %t: the report, %d lines:\n%s", rows, unicode, len(l), ansi.Strip(report))
			}
		}
	}
	// A section with no row is left out with its gap.
	if s := ansi.Strip((SystemDetails{OS: sdDebian.OS}).Report(plainAt(24, false))); strings.Contains(s, "Memory") || !strings.Contains(s, "OS and Kernel") {
		t.Errorf("a section with no row:\n%s", s)
	}
}

// TestSystemDetailsWhole: a value longer than its row is cut on a terminal, as
// every screen's is, and whole under component.OffTerminal (DEV-067): the bars and
// every other row as they are.
func TestSystemDetailsWhole(t *testing.T) {
	const model = "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz"
	d := sdDebian
	d.CPU = []component.KeyValue{{Key: "Model", Value: model}}
	on := ansi.Strip(d.Report(plainAt(24, false)))
	if strings.Contains(on, model) || !strings.Contains(on, "Intel(R) Xeon(R) Gold 6338 CPU @ 2...") {
		t.Errorf("on a terminal:\n%s", on)
	}
	off := ansi.Strip(d.Report(component.OffTerminal()))
	if !strings.Contains(off, "   -  Model                "+model+"\n") {
		t.Errorf("off a terminal:\n%s", off)
	}
	if got, want := strings.ReplaceAll(off, model, ""), strings.ReplaceAll(on, "Intel(R) Xeon(R) Gold 6338 CPU @ 2...", ""); got != want {
		t.Errorf("off a terminal differs but for the value:\n%s\n--\n%s", got, want)
	}
}

// TestLogsStates: each state's rows and items in the decided words: lines, Open
// and Refresh, or Refresh alone with no pager; no log, empty and unreadable,
// Refresh alone and no lines' section; hardn.yml's logFile said in a row.
func TestLogsStates(t *testing.T) {
	path := component.KeyValue{Key: "Path", Value: "/var/log/hardn.log"}
	open := component.MenuItem{Key: "1", Label: "Open", Description: "The whole log in less"}
	refresh := func(key string) component.MenuItem {
		return component.MenuItem{Key: key, Label: "Refresh", Description: "Read the log again"}
	}
	elsewhere := component.KeyValue{Key: "hardn.yml's logFile, /srv/log/hardn.log, isn't used", State: component.ValueWarn}
	lines := logLines(2)
	for _, tt := range []struct {
		name  string
		l     Logs
		rows  []component.KeyValue
		items []component.MenuItem
		code  string
	}{
		{"lines", Logs{Path: path.Value, Size: 49 << 10, Total: 2, Last: lines, Pager: "less"},
			[]component.KeyValue{path, {Key: "Size", Value: "49 KiB"}}, []component.MenuItem{open, refresh("2")}, "All 2 lines"},
		{"no pager", Logs{Path: path.Value, Size: 900, Total: 2, Last: lines},
			[]component.KeyValue{path, {Key: "Size", Value: "900 bytes"}}, []component.MenuItem{refresh("1")}, "All 2 lines"},
		{"one line", Logs{Path: path.Value, Size: 3 << 20, Total: 1, Last: lines[:1]},
			[]component.KeyValue{path, {Key: "Size", Value: "3 MiB"}}, []component.MenuItem{refresh("1")}, "1 line"},
		{"elsewhere", Logs{Path: path.Value, Size: 1024, Total: 2, Last: lines, Elsewhere: "/srv/log/hardn.log", Pager: "less"},
			[]component.KeyValue{path, {Key: "Size", Value: "1 KiB"}, elsewhere}, []component.MenuItem{open, refresh("2")}, "All 2 lines"},
		{"no log", Logs{Path: path.Value, State: LogsNoLog},
			[]component.KeyValue{path, {Key: "No log yet", State: component.ValueNone}}, []component.MenuItem{refresh("1")}, ""},
		{"empty", Logs{Path: path.Value, State: LogsEmpty},
			[]component.KeyValue{path, {Key: "Empty", State: component.ValueNone}}, []component.MenuItem{refresh("1")}, ""},
		{"unreadable", Logs{Path: path.Value, State: LogsUnreadable, Reason: "is a directory"},
			[]component.KeyValue{path, {Key: "Can't read it: is a directory", State: component.ValueWarn}}, []component.MenuItem{refresh("1")}, ""},
	} {
		sub := tt.l.SubMenu(plainAt(40, true))
		if sub.Title.Title != "Logs" || sub.Heading != "Select an option" || sub.Prompt != "Enter selection" || sub.Menu.Exit.Label != "Return" {
			t.Errorf("%s: %+v", tt.name, sub)
		}
		if got := sub.Sections[0]; got.Header.Title != "File" || !slices.Equal(got.Rows.Items, tt.rows) {
			t.Errorf("%s: %q, rows %+v", tt.name, got.Header.Title, got.Rows.Items)
		}
		if !slices.Equal(sub.Menu.Items, tt.items) {
			t.Errorf("%s: items %+v", tt.name, sub.Menu.Items)
		}
		if tt.code == "" && len(sub.Sections) != 1 || tt.code != "" && (len(sub.Sections) != 2 || sub.Sections[1].Header.Title != tt.code) {
			t.Errorf("%s: sections %+v", tt.name, sub.Sections)
		}
		if want := ""; tt.l.Pager != "" && tt.l.OpenKey() != "1" || tt.l.Pager == "" && tt.l.OpenKey() != want {
			t.Errorf("%s: OpenKey %q", tt.name, tt.l.OpenKey())
		}
	}
}

// TestLogsFit: as many of the last lines as the terminal holds under the screen's
// other blocks, a line that wraps counting each of its rows: on 1204 lines of one
// row each the drawing ends with the prompt on the terminal's last row, at either
// height, in both glyph sets; a line too long for the room still shows, whole; no
// section is cut.
func TestLogsFit(t *testing.T) {
	l := Logs{Path: "/var/log/hardn.log", Size: 49 << 10, Total: 1204, Last: logLines(256), Pager: "less"}
	header := regexp.MustCompile(`Last (\d+) of 1204 lines`)
	for _, tt := range []struct{ rows, n int }{{24, 6}, {40, 22}} {
		for _, unicode := range []bool{true, false} {
			c := plainAt(tt.rows, unicode)
			lines := drawn(l.SubMenu(c).Render(c))
			if len(lines) != tt.rows || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
				t.Errorf("80x%d, unicode %t: %d lines, the last %q", tt.rows, unicode, len(lines), lines[len(lines)-1])
			}
			m := header.FindStringSubmatch(strings.Join(lines, "\n"))
			if m == nil || m[1] != fmt.Sprint(tt.n) {
				t.Errorf("80x%d: %q, want Last %d", tt.rows, m, tt.n)
			}
			if last := lines[len(lines)-10]; !strings.Contains(last, "run 255: exit 0") {
				t.Errorf("80x%d: the newest line is not the last: %q", tt.rows, last)
			}
		}
	}
	long := strings.Repeat("long line ", 60)
	w := Logs{Path: "/var/log/hardn.log", Total: 2, Last: []component.CodeLine{{Text: "older"}, {Text: long}}}
	c := plainAt(10, true)
	s := ansi.Strip(w.SubMenu(c).Render(c))
	if !strings.Contains(s, "Last 1 of 2 lines") || strings.Contains(s, "older") || strings.Contains(s, "...") || strings.Contains(s, "…") {
		t.Errorf("a line longer than the room:\n%s", s)
	}
	if got := strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), " ", ""); !strings.Contains(strings.ReplaceAll(got, c.Theme.Glyphs.Wrap, ""), strings.ReplaceAll(long, " ", "")) {
		t.Errorf("not every character shows:\n%s", s)
	}
}

// TestLogsBlankLine: a blank line is a line: where it is all the tail holds, the
// section's header and its empty row stay, the row counted, as Open offers it. A
// Section of no lines is still no section.
func TestLogsBlankLine(t *testing.T) {
	blank := []component.CodeLine{{}}
	for _, tt := range []struct {
		name  string
		rows  int
		l     Logs
		title string
	}{
		{"alone", 24, Logs{Path: "/var/log/hardn.log", Size: 1, Total: 1, Last: blank, Pager: "less"}, "1 line"},
		{"the newest, all that fits", 19, Logs{Path: "/var/log/hardn.log", Size: 24, Total: 3, Last: append(logLines(2), blank...), Pager: "less"}, "Last 1 of 3 lines"},
	} {
		c := plainAt(tt.rows, true)
		lines := drawn(tt.l.SubMenu(c).Render(c))
		i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, tt.title) })
		if i < 0 || lines[i+1] != "" || lines[i+2] != "" || lines[i+3] != "" || !strings.Contains(lines[i+4], "Select an option") {
			t.Errorf("%s: no section of %q over an empty row:\n%s", tt.name, tt.title, strings.Join(lines, "\n"))
		}
		if len(lines) > tt.rows {
			t.Errorf("%s: %d lines at %d rows", tt.name, len(lines), tt.rows)
		}
	}
	alone := Logs{Path: "/var/log/hardn.log", Size: 1, Total: 1, Last: blank}
	if s := ansi.Strip(alone.Terminal(plainAt(24, true))); !strings.Contains(s, "1 line") {
		t.Errorf("Terminal:\n%s", s)
	}
	if s := (Section{Header: component.SectionHeader{Title: "Lines"}}).Render(plainAt(24, true)); s != "" {
		t.Errorf("no lines: %q", s)
	}
}

// TestLogsTerminal: hardn -p to a terminal holds the title, the file's rows and the
// last lines, no menu, no prompt, with two rows left for the shell, at 80 and at
// 60 columns, where the rows stack; with no lines, the title and the rows alone.
func TestLogsTerminal(t *testing.T) {
	l := Logs{Path: "/var/log/hardn.log", Size: 49 << 10, Total: 1204, Last: logLines(256), Pager: "less"}
	for _, width := range []int{80, 60} {
		for _, rows := range []int{24, 40} {
			env := term.Fixed(width, colorprofile.TrueColor, true, true)
			env.Height = rows
			s := l.Terminal(component.NewCtx(env))
			if got := len(drawn(s)); got != rows-logsShellRows {
				t.Errorf("%dx%d: %d lines, want %d", width, rows, got, rows-logsShellRows)
			}
			if strings.Contains(s, "Open") || strings.Contains(s, "Enter selection") {
				t.Errorf("%dx%d: a menu:\n%s", width, rows, ansi.Strip(s))
			}
		}
	}
	// A reading with no lines holds no section of them, in any state.
	if s := ansi.Strip((Logs{Path: "/var/log/hardn.log"}).Terminal(plainAt(24, false))); strings.Contains(s, "line") {
		t.Errorf("no lines:\n%s", s)
	}
	empty := Logs{Path: "/var/log/hardn.log", State: LogsEmpty}
	if s := ansi.Strip(empty.Terminal(plainAt(24, false))); strings.Contains(s, "line") || !strings.Contains(s, "Empty") || len(strings.Split(s, "\n")) != 6 {
		t.Errorf("empty:\n%s", s)
	}
}

// TestDetailsScreens: nav.Run draws what Render renders, in every golden cell of
// either screen, a code section among them.
func TestDetailsScreens(t *testing.T) {
	l := Logs{Path: "/var/log/hardn.log", Size: 49 << 10, Total: 1204, Last: logLines(30), Pager: "less"}
	for _, cell := range uitest.Core() {
		c := component.NewCtx(cell.Env)
		for name, sub := range map[string]SubMenu{"system details": sdDebian.SubMenu(), "logs": l.SubMenu(c)} {
			want := sub.Render(c)
			menu := nav.Menu{Exit: sub.Menu.Exit}
			for _, it := range sub.Menu.Items {
				menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return nil }})
			}
			if s := sub.Screen(c, menu); !strings.HasPrefix(want, s.Above) {
				t.Errorf("%s, %s: Screen's Above is not Render's top:\n%s\n--\n%s", name, cell.Name, s.Above, want)
			}
		}
	}
}
