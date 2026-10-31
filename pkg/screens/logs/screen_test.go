package logs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/logs"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestScreen: every host of the gallery's through Run: its rows and section in the
// decided words, its items, one read of the log and, only where there are lines, one
// less --version; nothing but those.
func TestScreen(t *testing.T) {
	open, refreshAlone := "   1) Open                 The whole log in less\n   2) Refresh              Read the log again", "   1) Refresh              Read the log again"
	for _, tt := range []struct {
		host, section, item string
		rows                []string
		checks              int
	}{
		{"lines", "Last 3 of 4 lines", open, []string{"   -  Size                 274 bytes"}, 1},
		{"long", "Last 3 of 1204 lines", open, []string{"   -  Size                 49 KiB", "| /etc/hardn/hardn.yml", "      2026/10/20 12:05:11 SUCCESS: debian trixie detected"}, 1},
		{"missing", "", refreshAlone, []string{"   -  No log yet"}, 0},
		{"unreadable", "", refreshAlone, []string{"   -  Can't read it: is a directory"}, 0},
		{"empty", "", refreshAlone, []string{"   -  Empty"}, 0},
		{"control", "All 6 lines", open, []string{"^[[31mErr:1^[[0m", "^[]0;a title^Gdone^[[2J", "abc<U+202E>def", "caf<0xe9>", "don’t “quote” ≥ 5 → … x"}, 1},
		{"elsewhere", "Last 3 of 8 lines", open, nil, 1},
		{"alpine323", "Last 4 of 8 lines", refreshAlone, nil, 1},
		{"no-pager", "Last 4 of 8 lines", refreshAlone, nil, 1},
	} {
		h := hostNamed(t, tt.host)
		p := play(h, "0")
		if p.err != nil || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d unread", tt.host, p.err, p.unread)
		}
		for _, w := range append(tt.rows, "# Logs ###", "# File ---", "   -  Path                 /var/log/hardn.log", "  Select an option", tt.item, "   0) Return") {
			if !strings.Contains(p.out, w) {
				t.Errorf("%s: no %q in:\n%s", tt.host, w, p.out)
			}
		}
		if tt.section != "" && !strings.Contains(p.out, "# "+tt.section+" ---") {
			t.Errorf("%s: no section %q in:\n%s", tt.host, tt.section, p.out)
		}
		if tt.section == "" && strings.Contains(p.out, " lines ---") {
			t.Errorf("%s: a section of lines:\n%s", tt.host, p.out)
		}
		if tt.item == refreshAlone && !strings.Contains(p.out, "[0-1 or q]:") || tt.item == open && !strings.Contains(p.out, "[0-2 or q]:") {
			t.Errorf("%s: the prompt's hint:\n%s", tt.host, p.out)
		}
		if got := countOn(h); got != (counts{scans: 1, checks: tt.checks}) {
			t.Errorf("%s: %+v, want one read and %d checks", tt.host, got, tt.checks)
		}
		assertOnlyReads(t, tt.host, h, 0)
	}
}

// TestBlankNewestLine: a log whose newest line is blank draws its section and offers
// Open, at a height where that line is all the tail holds as at one where it is the file.
func TestBlankNewestLine(t *testing.T) {
	for _, tt := range []struct {
		log, title string
		rows       int
	}{
		{"\n", "1 line", 24},
		{"  \t\n", "1 line", 24},
		{"first line\nsecond line\n\n", "Last 1 of 3 lines", 16},
	} {
		env := off()
		env.Height = tt.rows
		p := playWith(newLog(tt.log), env, logs.Options{}, "0")
		if p.err != nil || !strings.Contains(p.out, "# "+tt.title+" ---") || !strings.Contains(p.out, "1) Open") {
			t.Errorf("%q: Run = %v:\n%s", tt.log, p.err, p.out)
		}
	}
}

// TestHeights: as many of the last lines as the terminal holds, measured as they
// draw: long's 3 at 80x24 and 18 at 80x40, a wrapped line taking two rows, within the
// terminal's rows and the content width.
func TestHeights(t *testing.T) {
	for _, tt := range []struct {
		rows  int
		lines string
	}{{24, "# Last 3 of 1204 lines ---"}, {40, "# Last 18 of 1204 lines ---"}} {
		env := off()
		env.Height = tt.rows
		p := playWith(hostNamed(t, "long"), env, logs.Options{}, "0")
		if !strings.Contains(p.out, tt.lines) {
			t.Errorf("80x%d: no %q in:\n%s", tt.rows, tt.lines, p.out)
		}
		lines := strings.Split(strings.TrimRight(p.out, "\n"), "\n")
		if len(lines) > tt.rows {
			t.Errorf("80x%d: %d lines:\n%s", tt.rows, len(lines), p.out)
		}
		for _, l := range lines {
			if len([]rune(l)) > 64 && !strings.Contains(l, "Enter selection") {
				t.Errorf("80x%d: a line past the content width: %q", tt.rows, l)
			}
		}
	}
}

// TestElsewhere: hardn.yml's logFile, where it names another path, is said in a row,
// its path safe to show; where it names the file, or nothing, no row. The file read is
// the file hardn writes either way.
func TestElsewhere(t *testing.T) {
	for _, tt := range []struct {
		logFile string
		want    string
	}{
		{"/srv/log/hardn.log", "hardn.yml's logFile, /srv/log/hardn.log, isn't used"},
		{"/srv/\x1b[2Jlog", "hardn.yml's logFile, /srv/^[[2Jlog, isn't used"},
		{"", ""},
		{logview.File, ""},
		{"/var/log/./hardn.log", ""},
	} {
		h := hostNamed(t, "elsewhere")
		p := playWith(h, off(), logs.Options{LogFile: tt.logFile, Term: "xterm"}, "0")
		if tt.want != "" && !strings.Contains(p.out, tt.want) || tt.want == "" && strings.Contains(p.out, "logFile") || strings.Contains(p.out, "\x1b") {
			t.Errorf("%q: %s", tt.logFile, p.out)
		}
		for _, c := range h.Calls() {
			if c.Op == "scan-lines" && c.Path != logview.File {
				t.Errorf("%q: read %s", tt.logFile, c.Path)
			}
		}
	}
}

// TestSafeOnATerminal: on a terminal's screen, colours and all, what a log line and
// hardn.yml's logFile hold reaches it as text: no escape but the colours, no U+202E.
func TestSafeOnATerminal(t *testing.T) {
	p := playWith(hostNamed(t, "control"), terminal(40), logs.Options{LogFile: "/srv/\x1b]0;a title\x07log\u202e"}, "0")
	plain := strip(p.out)
	if p.err != nil || strings.ContainsAny(plain, "\x1b\x07\u202e") {
		t.Errorf("Run = %v: %q", p.err, plain)
	}
	for _, w := range []string{"^[[31mErr:1^[[0m", "^[]0;a title^Gdone^[[2J", "abc<U+202E>def", "caf<0xe9>", "don’t “quote” ≥ 5 → … x", "/srv/^[]0;a title^Glog<U+202E>"} {
		if !strings.Contains(plain, w) {
			t.Errorf("no %q in:\n%s", w, plain)
		}
	}
}

// TestOpen: Open runs GNU less by its key alone, the exact argv and environment
// (readers-system-details.md item 11), TERM the caller's, checked; it asks less
// --version again at the key press; then the screen is drawn again from a fresh
// reading, what the log gained showing.
func TestOpen(t *testing.T) {
	h := hostNamed(t, "lines")
	model := h.Handler
	h.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if slices.Equal(argv, lessCommand) {
			e, _ := h.Entry(logview.File)
			h.AddFile(logview.File, append(e.Data, []byte("2026/10/20 12:06:00 INFO: a line written meanwhile\n")...), 0o644, 0, 0)
		}
		return model(argv, stdin)
	}
	p := play(h, "1", "0")
	if p.err != nil || p.unread != 0 {
		t.Fatalf("Run = %v, %d unread:\n%s", p.err, p.unread, p.out)
	}
	if !strings.Contains(p.out, "a line written meanwhile") || strings.Count(p.out, "# Logs ###") != 2 {
		t.Errorf("not drawn again from a fresh reading:\n%s", p.out)
	}
	if got := countOn(h); got != (counts{scans: 2, checks: 3, opens: 1}) {
		t.Errorf("%+v, want two reads, two draws' checks and the key's, one open", got)
	}
	var run hostfake.Call
	for _, c := range h.Calls() {
		if c.Op == "run-interactive" {
			run = c
		}
	}
	if !slices.Equal(run.Argv, lessCommand) || !slices.Equal(run.Env, lessEnv("xterm-256color")) {
		t.Errorf("ran %q, env %q", run.Argv, run.Env)
	}
	assertOnlyReads(t, "Open", h, 1)
}

// TestOpenByItsKeyAlone: nothing else runs a pager: Refresh, Return, q, a line refused,
// the end of the input and, where there is no pager, key 1, which is Refresh there.
func TestOpenByItsKeyAlone(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
	}{
		{"lines", []string{"0"}}, {"lines", []string{"2", "0"}}, {"lines", []string{"q"}}, {"lines", []string{"3", "less", "open", "o", "0"}}, {"lines", nil},
		{"alpine323", []string{"1", "0"}}, {"no-pager", []string{"1", "0"}}, {"missing", []string{"1", "0"}}, {"empty", []string{"1", "0"}}, {"unreadable", []string{"1", "0"}},
	} {
		h := hostNamed(t, tt.host)
		play(h, tt.lines...)
		assertOnlyReads(t, tt.host, h, 0)
	}
}

// TestTerm: TERM is passed only where logview.Pager takes it, vt100 otherwise, and
// the rest of the command is as it is.
func TestTerm(t *testing.T) {
	for give, want := range map[string]string{"screen-256color": "screen-256color", "../../etc/passwd": "vt100", "": "vt100", "xterm\x1b[2J": "vt100"} {
		h := hostNamed(t, "lines")
		playWith(h, off(), logs.Options{Term: give}, "1", "0")
		found := false
		for _, c := range h.Calls() {
			if c.Op == "run-interactive" {
				found = slices.Equal(c.Env, lessEnv(want))
			}
		}
		if !found {
			t.Errorf("TERM %q: %v", give, h.Calls())
		}
	}
}

// TestOpenRefused: a refusal or the pager's failure is said, the screen goes on, and
// no raw byte is shown: less no longer GNU's at the key press, and nothing run; off a
// terminal, the seam's real refusal; the pager's exit status, as it is.
func TestOpenRefused(t *testing.T) {
	const noGNU = "less is not GNU less"
	// less changes under the open menu: its check at the key press fails closed, the
	// words the decided ones, the screen then with no Open item.
	h := hostNamed(t, "lines")
	model := h.Handler
	checks := 0
	h.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if slices.Equal(argv, []string{"less", "--version"}) {
			if checks++; checks > 1 {
				return hostfake.Response{Stderr: "less: unrecognized option: version\n", ExitCode: 1}
			}
		}
		return model(argv, stdin)
	}
	p := play(h, "1", "0")
	if p.err != nil || !strings.Contains(p.out, noGNU) || strings.Contains(p.out, "logview:") {
		t.Errorf("Run = %v:\n%s", p.err, p.out)
	}
	if i := strings.LastIndex(p.out, "# Logs ###"); i < 0 || strings.Contains(p.out[i:], "The whole log in less") {
		t.Errorf("Open offered after less went:\n%s", p.out)
	}
	assertOnlyReads(t, "less changed", h, 0)

	// The seam's refusal, the real one: stdin and stdout here are no terminal.
	seam := host.NewReal(host.Options{})
	if err := seam.RunInteractive(context.Background(), nil, "true"); err == nil {
		t.Log("the seam ran: this test's stdin and stdout are a terminal")
	} else {
		p = play(realSeam{hostNamed(t, "lines"), seam}, "1", "0")
		if p.err != nil || !strings.Contains(p.out, "stdin and stdout must both be a terminal") || strings.Contains(p.out, "host: run") {
			t.Errorf("the seam's refusal: Run = %v:\n%s", p.err, p.out)
		}
	}

	// The pager's failure: the error as it is, safe.
	bad := hostNamed(t, "lines")
	model = bad.Handler
	bad.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if slices.Equal(argv, lessCommand) {
			return hostfake.Response{Stderr: "less: \x1b]0;a title\x07\n", ExitCode: 2}
		}
		return model(argv, stdin)
	}
	p = play(bad, "1", "0")
	if p.err != nil || !strings.Contains(p.out, "exit status 2") || strings.Count(p.out, "# Logs ###") != 2 || strings.ContainsAny(p.out, "\x1b\x07") {
		t.Errorf("the pager's failure: Run = %v:\n%q", p.err, p.out)
	}
	assertOnlyReads(t, "the pager's failure", bad, 1)

	// An error that holds what a terminal would act on shows it as text.
	raw := hostNamed(t, "lines")
	p = play(erringSeam{raw, errors.New("less: \x1b[2J\u202ebad")}, "1", "0")
	if p.err != nil || strings.ContainsAny(p.out, "\x1b\u202e") || !strings.Contains(p.out, "^[[2J<U+202E>bad") {
		t.Errorf("an error with raw bytes: Run = %v:\n%q", p.err, p.out)
	}
}

// realSeam is the host with the real seam's RunInteractive, whose refusal off a
// terminal is what is tested; it runs nothing else.
type realSeam struct {
	*hostfake.System
	seam *host.Real
}

func (s realSeam) RunInteractive(ctx context.Context, env []string, _ string, _ ...string) error {
	return s.seam.RunInteractive(ctx, env, "true")
}

// erringSeam is the host with a RunInteractive that fails as it says.
type erringSeam struct {
	*hostfake.System
	err error
}

func (s erringSeam) RunInteractive(context.Context, []string, string, ...string) error { return s.err }

// chunks gives each read its next chunk: what was typed before a read is one chunk.
type chunks struct{ next []string }

func (c *chunks) Read(p []byte) (int, error) {
	if len(c.next) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.next[0])
	c.next = c.next[1:]
	return n, nil
}

// TestOpenFlushes: Open itself does not call Flush, because the next round's prompt
// does: on a terminal Reader, what was typed before the screen is drawn again, a
// second q at less's end, does not reach the menu (prompt.Reader.Ask, through
// nav.Prompter.Ask). The first chunk holds the key and that q; the reader is told it
// is a terminal, and with no descriptor to flush it drops its own buffer.
func TestOpenFlushes(t *testing.T) {
	h := hostNamed(t, "lines")
	var out bytes.Buffer
	env := off()
	o := logs.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(&chunks{next: []string{"1\nq\n", "0\n"}}, true), Out: env.Writer(&out)},
		System:   h,
	}
	if err := logs.Run(context.Background(), o); err != nil {
		t.Errorf("Run = %v, the q typed under less taken as the menu's:\n%s", err, out.String())
	}
	if got := countOn(h).opens; got != 1 {
		t.Errorf("less ran %d times", got)
	}
}

// TestNavigation: 0 returns, q quits, the end of the input aborts; a line refused asks
// again, no draw and no read more.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		lines []string
		want  error
	}{{[]string{"0"}, nil}, {[]string{"q"}, nav.ErrQuit}, {nil, prompt.ErrEndOfInput}, {[]string{"2"}, prompt.ErrAborted}, {[]string{"9", "0"}, nil}} {
		h := hostNamed(t, "lines")
		p := play(h, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil && p.err != nil) || p.unread != 0 {
			t.Errorf("%q: Run = %v, want %v; %d unread", tt.lines, p.err, tt.want, p.unread)
		}
	}
	h := hostNamed(t, "lines")
	if p := play(h, "9", "0"); !strings.Contains(p.out, "Invalid option") || countOn(h).scans != 1 {
		t.Errorf("%+v:\n%s", countOn(h), p.out)
	}
}

// TestRefresh: Refresh reads the log and asks for the pager again, each draw once, and
// shows what changed: a log gone says so, and Open goes with its lines.
func TestRefresh(t *testing.T) {
	h := hostNamed(t, "lines")
	p := play(h, "2", "0")
	if got := countOn(h); got != (counts{scans: 2, checks: 2}) || p.err != nil || strings.Count(p.out, "# Logs ###") != 2 {
		t.Errorf("%+v, Run = %v:\n%s", got, p.err, p.out)
	}
	gone := hostNamed(t, "lines")
	model := gone.Handler
	gone.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if err := gone.Remove(context.Background(), logview.File); err != nil {
			t.Fatal(err)
		}
		gone.Handler = model
		return model(argv, stdin)
	}
	p = play(gone, "2", "0")
	if i := strings.LastIndex(p.out, "# Logs ###"); i < 0 || !strings.Contains(p.out[i:], "No log yet") || strings.Contains(p.out[i:], "Open") {
		t.Errorf("Run = %v:\n%s", p.err, p.out)
	}
}

// TestRunNeeds: without a Prompter's In or Out or a System, Run and Print are refused,
// writing nothing.
func TestRunNeeds(t *testing.T) {
	h := hostNamed(t, "lines")
	env := off()
	var out bytes.Buffer
	in := prompt.New(uitest.NewTypist(&out), false)
	for name, o := range map[string]logs.Options{
		"no in":     {Prompter: nav.Prompter{Ctx: component.NewCtx(env), Out: &out}, System: h},
		"no out":    {Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: in}, System: h},
		"no system": {Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: in, Out: &out}},
	} {
		if err := logs.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, o := range map[string]logs.PrintOptions{"no out": {Env: env, System: h}, "no system": {Env: env, Out: &out}} {
		if _, err := logs.Print(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Print, %s: %v", name, err)
		}
	}
	if out.Len() != 0 || len(h.Calls()) != 0 {
		t.Errorf("wrote %q, called %v", out.String(), h.Calls())
	}
}

// TestPlanOnly: Options has no mode to differ by, as neither screen plans: plan only
// opens and reads as live (gallery-system-details.md §5, Made here).
func TestPlanOnly(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(logs.Options{}), reflect.TypeOf(logs.PrintOptions{})} {
		for i := range typ.NumField() {
			if name := strings.ToLower(typ.Field(i).Name); strings.Contains(name, "dry") || strings.Contains(name, "plan") {
				t.Errorf("%s has %s", typ, typ.Field(i).Name)
			}
		}
	}
}

// printOn runs Print on sys in env and returns stdout, what it found and its error.
func printOn(sys host.System, env term.Env, logFile string) (string, logs.Printed, error) {
	var out bytes.Buffer
	p, err := logs.Print(context.Background(), logs.PrintOptions{Out: &out, Env: env, System: sys, LogFile: logFile})
	return out.String(), p, err
}

// TestPrintOffTerminal: hardn -p off a terminal: every line as written, safe to show,
// no heading, no style, whatever the environment says of width or colour (cli-pipe,
// cli-pipe-control); one pass, writing nothing, and no less --version.
func TestPrintOffTerminal(t *testing.T) {
	h := hostNamed(t, "long")
	s, p, err := printOn(h, off(), "")
	if err != nil || p.ExitCode() != 0 || len(p.Stderr("sudo")) != 0 || s != longLog() || p.Total != 1204 {
		t.Errorf("err %v, exit %d, stderr %q, total %d, %d bytes", err, p.ExitCode(), p.Stderr("sudo"), p.Total, len(s))
	}
	if got := countOn(h); got != (counts{scans: 1}) {
		t.Errorf("%+v", got)
	}
	assertOnlyReads(t, "long", h, 0)
	always := term.Resolve(term.Options{Color: term.ColorAlways}, term.Facts{Width: 40, Environ: []string{"COLORTERM=truecolor"}})
	s, _, err = printOn(hostNamed(t, "control"), always, "/srv/log/hardn.log")
	want := "2026/10/20 12:00:01 INFO: run 1: $ apt-get update\n" +
		"2026/10/20 12:00:02 INFO: run 1: | ^[[31mErr:1^[[0m\n" +
		"2026/10/20 12:00:02 INFO: run 1: | ^[]0;a title^Gdone^[[2J\n" +
		"2026/10/20 12:00:03 INFO: run 2: | abc<U+202E>def\n" +
		"2026/10/20 12:00:03 INFO: run 2: | caf<0xe9>\n" +
		"2026/10/20 12:00:03 INFO: run 2: | don’t “quote” ≥ 5 → … x\n"
	if err != nil || s != want {
		t.Errorf("err %v:\n%s", err, s)
	}
	long := "2026/10/20 12:00:05 ERROR: " + strings.Repeat("a long line ", 40)
	if s, _, _ = printOn(newLog(long+"\n"), off(), ""); s != long+"\n" {
		t.Errorf("a long line off a terminal: %q", s)
	}
}

// TestPrintTerminal: hardn -p to a terminal: the file's rows and the last lines the
// terminal holds, colour kept, no menu and no prompt, two rows left for the shell
// (cli-terminal-tail); a line too long breaks with nothing cut; hardn.yml's logFile in
// a row; an empty log its row alone. Nothing but one read.
func TestPrintTerminal(t *testing.T) {
	for _, rows := range []int{24, 40} {
		h := hostNamed(t, "long")
		s, p, err := printOn(h, terminal(rows), "")
		if err != nil || p.ExitCode() != 0 || len(p.Stderr("sudo")) != 0 {
			t.Errorf("%d rows: err %v, exit %d, %q", rows, err, p.ExitCode(), p.Stderr("sudo"))
		}
		plain := strip(s)
		if got := strings.Count(plain, "\n"); got > rows-2 {
			t.Errorf("%d rows: %d lines, no room for the shell:\n%s", rows, got, plain)
		}
		if !strings.Contains(s, "\x1b[") || !strings.Contains(plain, "File") || strings.Contains(plain, "Enter selection") || strings.Contains(plain, "Open") {
			t.Errorf("%d rows:\n%s", rows, plain)
		}
		if got := countOn(h); got != (counts{scans: 1}) {
			t.Errorf("%d rows: %+v", rows, got)
		}
		assertOnlyReads(t, "long", h, 0)
	}
	s, _, _ := printOn(hostNamed(t, "control"), terminal(24), "/srv/log/hardn.log")
	if plain := strip(s); strings.ContainsAny(plain, "\x1b\u202e\x07") || !strings.Contains(plain, "hardn.yml's logFile, /srv/log/hardn.log, isn't used") || !strings.Contains(plain, "abc<U+202E>def") || !strings.Contains(plain, "don’t “quote”") {
		t.Errorf("control:\n%q", plain)
	}
	long := "2026/10/20 12:00:05 ERROR: " + strings.Repeat("alongword ", 12) + "end"
	s, _, _ = printOn(newLog(long+"\n"), terminal(24), "")
	if plain := strip(s); !strings.Contains(plain, "↪") || !strings.Contains(plain, "end") || strings.Contains(plain, "…") || !strings.Contains(plain, "1 line") {
		t.Errorf("a long line:\n%s", plain)
	}
	s, _, _ = printOn(newLog(""), terminal(24), "")
	if plain := strip(s); !strings.Contains(plain, "Empty") || strings.Contains(plain, "line") {
		t.Errorf("empty:\n%s", plain)
	}
}

// TestPrintStates: what unit (d) needs, without reading the file again: no log, stderr
// in the decided words, exit 0; unreadable, exit 1; refused, "Run it with <tool>.",
// where the caller names one; empty, nothing on either and exit 0, off a terminal; a
// read that fails part way, the lines gone before it, exit 1; stdout empty in each
// state that has words, to a terminal or not. Nothing written.
func TestPrintStates(t *testing.T) {
	const noLog = "hardn: no log yet: /var/log/hardn.log"
	dir := "hardn: can't read /var/log/hardn.log: is a directory"
	denied := "hardn: can't read /var/log/hardn.log: permission denied"
	for _, tt := range []struct {
		name   string
		sys    func() host.System
		stderr []string
		exit   int
	}{
		{"missing", func() host.System { return hostNamed(t, "missing") }, []string{noLog}, 0},
		{"unreadable", func() host.System { return hostNamed(t, "unreadable") }, []string{dir}, 1},
		{"refused", func() host.System { return refusingScan{hostNamed(t, "lines")} }, []string{denied, "Run it with sudo."}, 1},
	} {
		for _, env := range []term.Env{off(), terminal(24)} {
			s, p, err := printOn(tt.sys(), env, "")
			if err != nil || s != "" || !slices.Equal(p.Stderr("sudo"), tt.stderr) || p.ExitCode() != tt.exit {
				t.Errorf("%s, TTY %t: err %v, stdout %q, stderr %q, exit %d", tt.name, env.TTY, err, s, p.Stderr("sudo"), p.ExitCode())
			}
		}
	}
	if _, p, _ := printOn(refusingScan{hostNamed(t, "lines")}, off(), ""); !slices.Equal(p.Stderr(""), []string{denied}) || !errors.Is(p.Err, fs.ErrPermission) {
		t.Errorf("no tool named: %q, %v", p.Stderr(""), p.Err)
	}
	if _, p, _ := printOn(refusingScan{hostNamed(t, "lines")}, off(), ""); !slices.Equal(p.Stderr("doas")[1:], []string{"Run it with doas."}) {
		t.Errorf("doas: %q", p.Stderr("doas"))
	}
	if s, p, err := printOn(hostNamed(t, "empty"), off(), ""); err != nil || s != "" || len(p.Stderr("sudo")) != 0 || p.ExitCode() != 0 {
		t.Errorf("empty: err %v, stdout %q, stderr %q, exit %d", err, s, p.Stderr("sudo"), p.ExitCode())
	}
	s, p, err := printOn(failingScan{hostNamed(t, "missing"), []string{"a line", "\x1b[2Janother"}}, off(), "")
	if err != nil || s != "a line\n^[[2Janother\n" || p.ExitCode() != 1 || !slices.Equal(p.Stderr("sudo"), []string{"hardn: can't read /var/log/hardn.log: input/output error"}) {
		t.Errorf("part way: err %v, stdout %q, stderr %q, exit %d", err, s, p.Stderr("sudo"), p.ExitCode())
	}
	if _, _, err := printOnFailing(hostNamed(t, "lines"), off()); err == nil || !strings.Contains(err.Error(), "write") {
		t.Errorf("a failed write off a terminal: %v", err)
	}
	if _, _, err := printOnFailing(hostNamed(t, "lines"), terminal(24)); err == nil || !strings.Contains(err.Error(), "write") {
		t.Errorf("a failed write to a terminal: %v", err)
	}
}

// printOnFailing runs Print with a stdout that fails, as a closed pipe does.
func printOnFailing(sys host.System, env term.Env) (string, logs.Printed, error) {
	p, err := logs.Print(context.Background(), logs.PrintOptions{Out: failing{}, Env: env, System: sys})
	return "", p, err
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
