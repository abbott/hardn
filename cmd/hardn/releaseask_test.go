package main

import (
	"context"
	osuser "os/user"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/version"
)

const (
	askState  = revert.DefaultDir + "/" + version.AskName
	legacyYML = "# hardn's settings\nusername: alice # the admin\n\n# SSH\nsshPort: 2230\n"
	ymlPath   = "/etc/hardn/hardn.yml"
)

// asking is the question's test bench: the host its state and hardn.yml
// are on, the clock, the answers to give (each "y", "n", "" for Enter, or
// "eof"), how often it was asked, and what was said.
type asking struct {
	f       *hostfake.System
	now     time.Time
	answers []string
	asked   int
	out     strings.Builder
}

// useAsking points the question at a new bench, on a terminal, until the
// test ends.
func useAsking(t *testing.T, answers ...string) *asking {
	t.Helper()
	b := &asking{f: hostfake.New(host.Options{}), now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC), answers: answers}
	useReleaseHost(t, b.f, &releaseServer{tag: "v0.4.1"})
	releaseNow = func() time.Time { return b.now }
	termWas, askWas, outWas := releaseAskTerminals, askCheckReleases, config.Stdout
	t.Cleanup(func() { releaseAskTerminals, askCheckReleases, config.Stdout = termWas, askWas, outWas })
	releaseAskTerminals = func() bool { return true }
	config.Stdout = &b.out
	askCheckReleases = func() (bool, bool) {
		b.asked++
		if len(b.answers) == 0 {
			t.Fatalf("asked a %d. time, with no answer to give", b.asked)
		}
		a := b.answers[0]
		b.answers = b.answers[1:]
		switch a {
		case "eof":
			return false, false
		case "":
			return true, true // Enter is yes
		}
		return a == "y", true
	}
	return b
}

// state is the state file's content, "" for none.
func (b *asking) state() string {
	e, _ := b.f.Entry(askState)
	return string(e.Data)
}

// yml puts a hardn.yml on the bench's host and reads it.
func (b *asking) yml(t *testing.T, content string) *config.Config {
	t.Helper()
	b.f.AddDir("/etc/hardn", 0o755, 0, 0)
	b.f.AddFile(ymlPath, []byte(content), 0o644, 0, 0)
	return b.read(t)
}

func (b *asking) read(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Read(context.Background(), b.f, ymlPath)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// open is one start of the main menu on c, and how often the question was
// asked in it.
func (b *asking) open(c *config.Config) int { return b.openNew(c, false) }

// openNew is open, created telling whether this run created c's hardn.yml.
func (b *asking) openNew(c *config.Config, created bool) int {
	before := b.asked
	releaseAsk(context.Background(), c, created)
	return b.asked - before
}

// TestReleaseAskNoConfig: with no hardn.yml the question is asked at the
// first open. After a no or the end of the input: not for 14 opens,
// at the 15th, and at the first open 30 days on. The check stays off.
func TestReleaseAskNoConfig(t *testing.T) {
	for _, answer := range []string{"n", "eof"} {
		b := useAsking(t, answer, "n", "n")
		c := config.DefaultConfig()
		if b.open(c) != 1 || c.CheckReleases || b.state() == "" {
			t.Fatalf("%q: asked %d, on %t, state %q", answer, b.asked, c.CheckReleases, b.state())
		}
		for i := 1; i < version.AskOpens; i++ {
			if b.open(config.DefaultConfig()) != 0 {
				t.Fatalf("%q: asked at open %d", answer, i)
			}
		}
		if b.open(config.DefaultConfig()) != 1 {
			t.Errorf("%q: not asked at open %d", answer, version.AskOpens)
		}
		b.now = b.now.Add(version.AskEvery - time.Hour)
		if b.open(config.DefaultConfig()) != 0 {
			t.Errorf("%q: asked an hour before 30 days", answer)
		}
		b.now = b.now.Add(time.Hour)
		if b.open(config.DefaultConfig()) != 1 {
			t.Errorf("%q: not asked after 30 days", answer)
		}
		if m := withReleaseCheck(mainmenu.Options{}, c, "0.4.0"); m.CheckRelease != nil || b.out.Len() != 0 {
			t.Errorf("%q: the check is on, or something was said: %q", answer, b.out.String())
		}
	}
}

// TestReleaseAskClockBackwards: after a no, a clock set back does not ask
// at once, and the 15th open still asks.
func TestReleaseAskClockBackwards(t *testing.T) {
	b := useAsking(t, "n", "n")
	b.open(config.DefaultConfig())
	b.now = b.now.Add(-90 * 24 * time.Hour)
	for i := 1; i < version.AskOpens; i++ {
		if b.open(config.DefaultConfig()) != 0 {
			t.Fatalf("asked at open %d", i)
		}
	}
	if b.open(config.DefaultConfig()) != 1 {
		t.Error("not asked at the 15th open")
	}
}

// TestReleaseAskYesNoConfig: a yes with no hardn.yml turns the check on in
// this session and in the next, is never asked again, a dry run included,
// and --version says a newer release. A hardn.yml that says false then
// wins: no check, no request, and the state not written.
func TestReleaseAskYesNoConfig(t *testing.T) {
	b := useAsking(t, "y")
	c := config.DefaultConfig()
	if b.open(c) != 1 || !c.CheckReleases || !strings.Contains(b.state(), `"answer":"yes"`) {
		t.Fatalf("asked %d, on %t, state %q", b.asked, c.CheckReleases, b.state())
	}
	if m := withReleaseCheck(mainmenu.Options{}, c, "0.4.0"); m.Release == nil || m.CheckRelease == nil {
		t.Error("the main menu has no check after a yes")
	}
	b.now = b.now.Add(10 * version.AskEvery)
	for i := 0; i < 2*version.AskOpens; i++ {
		next := config.DefaultConfig()
		next.DryRun = i%2 == 1
		if b.open(next) != 0 || !next.CheckReleases {
			t.Fatalf("open %d after a yes: asked %d, on %t", i, b.asked, next.CheckReleases)
		}
	}

	// --version, as root: what the cache holds.
	was := Version
	Version = "0.4.0"
	t.Cleanup(func() { Version = was })
	b.f.AddFile(revert.DefaultDir+"/"+version.ReleaseCacheName,
		[]byte(`{"checked":"2026-11-15T09:00:00Z","answered":true,"tag":"v0.4.1"}`+"\n"), 0o600, 0, 0)
	if got := newerRelease(context.Background()); got != "v0.4.1" {
		t.Errorf("--version says %q, want v0.4.1", got)
	}
	// As a user other than root, who cannot read the state: nothing.
	releaseHost = func() host.System { return userHost{b.f} }
	if got := newerRelease(context.Background()); got != "" {
		t.Errorf("--version says %q to a user other than root", got)
	}
	releaseHost = func() host.System { return b.f }

	// A hardn.yml, later, with checkReleases: false.
	off := b.yml(t, "checkReleases: false\n")
	state := b.state()
	if b.open(off) != 0 || off.CheckReleases || b.state() != state {
		t.Errorf("hardn.yml says false: asked %d, on %t, state %q", b.asked, off.CheckReleases, b.state())
	}
	if m := withReleaseCheck(mainmenu.Options{}, off, "0.4.0"); m.Release != nil || m.CheckRelease != nil {
		t.Error("hardn.yml says false, and the main menu has a check")
	}
	useReleaseConfig(t, false, nil)
	if got := newerRelease(context.Background()); got != "" {
		t.Errorf("--version says %q with checkReleases: false", got)
	}
}

// TestReleaseAskNotAsked: off a terminal and under -n nothing is
// asked, counted or written, with no hardn.yml and with one that lacks the
// key.
func TestReleaseAskNotAsked(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tty, dryRun bool
	}{{"no terminal", false, false}, {"-n", true, true}} {
		for _, legacy := range []bool{false, true} {
			b := useAsking(t)
			releaseAskTerminals = func() bool { return tc.tty }
			c := config.DefaultConfig()
			if legacy {
				c = b.yml(t, legacyYML)
			}
			c.ResolveDryRun(tc.dryRun, tc.dryRun)
			for i := 0; i < 2*version.AskOpens; i++ {
				b.open(c)
			}
			yml, _ := b.f.Entry(ymlPath)
			if b.asked != 0 || c.CheckReleases || b.state() != "" || b.out.Len() != 0 || legacy && string(yml.Data) != legacyYML {
				t.Errorf("%s, hardn.yml %t: asked %d, on %t, state %q, said %q, hardn.yml %q",
					tc.name, legacy, b.asked, c.CheckReleases, b.state(), b.out.String(), yml.Data)
			}
		}
	}
}

// TestReleaseAskFlagsAndUsers: a flag, a subcommand, --version and a user
// other than root never reach the question: nothing asked, nothing
// written. The main menu, as root, does.
func TestReleaseAskFlagsAndUsers(t *testing.T) {
	noConfig := func() {
		findConfig = func(string) config.Found { return config.Found{} }
		loadConfig = func(string) (*config.Config, error) { return config.DefaultConfig(), nil }
		loadExisting = loadConfig
	}
	menuWas := startMenu
	t.Cleanup(func() { startMenu = menuWas })
	var menu *mainmenu.Options
	startMenu = func(_ context.Context, o mainmenu.Options) { menu = &o }

	for _, tc := range []struct {
		name string
		mode hostMode
		args []string
	}{
		{"-g", asRoot, []string{"-g"}},
		{"-r", asRoot, []string{"-r"}},
		{"-n -g", asRoot, []string{"-n", "-g"}},
		{"--version", asRoot, []string{"--version"}},
		{"-p", asRoot, []string{"-p"}},
		{"check", asRoot, []string{"check"}},
		{"not root", hostUnreachable, nil},
	} {
		b := useAsking(t)
		menu = nil
		runHardnWith(t, tc.mode, func() {
			noConfig()
			h, _ := newDNSHost(t, "debian13", nil)
			useDNSHost(t, h)
			if tc.mode == hostUnreachable {
				lookupCurrentUser = func() (*osuser.User, error) { return &osuser.User{Uid: "1000", Username: "alice"}, nil }
			}
		}, tc.args...)
		if b.asked != 0 || b.state() != "" || menu != nil {
			t.Errorf("%s: asked %d, state %q, main menu run %t", tc.name, b.asked, b.state(), menu != nil)
		}
	}

	b := useAsking(t, "y")
	r := runHardnWith(t, asRoot, noConfig)
	if r.ended || b.asked != 1 || menu == nil || !menu.Config.CheckReleases || menu.CheckRelease == nil {
		t.Errorf("the main menu: ended %v, asked %d, menu %+v; stderr %q", r.ended, b.asked, menu, r.stderr)
	}
}

// TestReleaseAskLegacy: a hardn.yml without the key is asked about once,
// and the answer, yes or no, is written to it in place, its comments and
// every other byte kept; a yes is on in this session. After a yes it is
// never asked about again, hardn's state holding only that the question
// was due (version.Asker.Due); a no is kept as the question's own false
// (TestReleaseAskOwnFalse).
func TestReleaseAskLegacy(t *testing.T) {
	for _, tc := range []struct {
		answer string
		on     bool
	}{{"y", true}, {"n", false}, {"", true}} {
		b := useAsking(t, tc.answer)
		c := b.yml(t, legacyYML)
		word := map[bool]string{true: "true", false: "false"}[tc.on]
		if b.open(c) != 1 || c.CheckReleases != tc.on {
			t.Fatalf("%q: asked %d, on %t", tc.answer, b.asked, c.CheckReleases)
		}
		yml, _ := b.f.Entry(ymlPath)
		if want := legacyYML + "checkReleases: " + word + "\n"; string(yml.Data) != want {
			t.Errorf("%q: hardn.yml is\n%s\nwant\n%s", tc.answer, yml.Data, want)
		}
		state := `"answer":"none"`
		if !tc.on {
			state = `"answer":"no","asked":"2026-11-15T12:00:00Z","opens":0,"wroteFalse":"` + ymlPath + `"`
		}
		if want := "Saved checkReleases: " + word + " to " + ymlPath + "\n"; b.out.String() != want || !strings.Contains(b.state(), state) {
			t.Errorf("%q: said %q, want %q; state %q", tc.answer, b.out.String(), want, b.state())
		}
		if m := withReleaseCheck(mainmenu.Options{}, c, "0.4.0"); (m.CheckRelease != nil) != tc.on {
			t.Errorf("%q: the main menu's check is %t", tc.answer, m.CheckRelease != nil)
		}
		if !tc.on {
			continue
		}
		b.now = b.now.Add(10 * version.AskEvery)
		for i := 0; i < 2*version.AskOpens; i++ {
			if again := b.read(t); b.open(c) != 0 || b.open(again) != 0 || again.CheckReleases != tc.on {
				t.Fatalf("%q: asked again at open %d, or read back as %t", tc.answer, i, again.CheckReleases)
			}
		}
	}
}

// TestReleaseAskLegacyUnanswered: the end of the input, or a hardn.yml
// hardn can't edit in place, writes nothing to it, says why in one line,
// leaves the check off, and asks again by the 15 opens or 30 days.
func TestReleaseAskLegacyUnanswered(t *testing.T) {
	const anchored = "base: &b\n  x: 1\n<<: *b\nusername: alice\n"
	for _, tc := range []struct {
		name, content, answer, says string
	}{
		{"the end of the input", legacyYML, "eof", "No answer: " + ymlPath + " not changed\n"},
		{"a yes, not editable", anchored, "y", ymlPath + " not changed: it merges settings with <<. Set checkReleases there by hand\n"},
		{"a no, not editable", anchored, "n", ymlPath + " not changed: it merges settings with <<. Set checkReleases there by hand\n"},
	} {
		b := useAsking(t, tc.answer, "eof", "eof")
		c := b.yml(t, tc.content)
		yml, _ := b.f.Entry(ymlPath)
		if b.open(c) != 1 || c.CheckReleases || string(yml.Data) != tc.content || b.out.String() != tc.says ||
			!strings.Contains(b.state(), `"answer":"none"`) || strings.Contains(b.state(), "wroteFalse") {
			t.Fatalf("%s: asked %d, on %t, said %q, state %q, hardn.yml %q", tc.name, b.asked, c.CheckReleases, b.out.String(), b.state(), yml.Data)
		}
		for i := 1; i < version.AskOpens; i++ {
			if b.open(b.read(t)) != 0 {
				t.Fatalf("%s: asked at open %d", tc.name, i)
			}
		}
		if b.open(b.read(t)) != 1 {
			t.Errorf("%s: not asked at open %d", tc.name, version.AskOpens)
		}
		b.now = b.now.Add(version.AskEvery)
		if b.open(b.read(t)) != 1 {
			t.Errorf("%s: not asked after 30 days", tc.name)
		}
	}
}

// TestReleaseAskLegacyAfterYes: a yes given before there was a hardn.yml
// asks again at once once one without the key is there, where the file
// decides, and Enter writes true.
func TestReleaseAskLegacyAfterYes(t *testing.T) {
	b := useAsking(t, "y", "")
	b.open(config.DefaultConfig())
	c := b.yml(t, legacyYML)
	yml, _ := b.f.Entry(ymlPath)
	if b.open(c) != 1 || !c.CheckReleases {
		t.Fatalf("asked %d, on %t", b.asked, c.CheckReleases)
	}
	if yml, _ = b.f.Entry(ymlPath); string(yml.Data) != legacyYML+"checkReleases: true\n" {
		t.Errorf("hardn.yml is\n%s", yml.Data)
	}
}

// TestReleaseAskFileDryRun: hardn.yml's dryRun: true, a lasting setting,
// does not stop the question: asked once, the answer written to the file
// in place with dryRun as it was, and on in this session.
func TestReleaseAskFileDryRun(t *testing.T) {
	const content = "dryRun: true # plan only\n" + legacyYML
	b := useAsking(t, "y")
	c := b.yml(t, content)
	c.ResolveDryRun(false, false)
	if !c.DryRun || c.DryRunSource() != config.DryRunFile {
		t.Fatalf("not a dry run from the file: %t, %v", c.DryRun, c.DryRunSource())
	}
	asked := b.open(c)
	yml, _ := b.f.Entry(ymlPath)
	if asked != 1 || !c.CheckReleases || string(yml.Data) != content+"checkReleases: true\n" {
		t.Fatalf("asked %d, on %t, state %q, hardn.yml %q", b.asked, c.CheckReleases, b.state(), yml.Data)
	}
	again := b.read(t)
	again.ResolveDryRun(false, false)
	if b.open(again) != 0 || !again.CheckReleases || !again.DryRun {
		t.Errorf("asked again, or read back as %t, dry run %t", again.CheckReleases, again.DryRun)
	}
}

// TestReleaseAskStateNotKept: where hardn's state can't be written (its
// directory another user's) nothing is asked, said or written at any
// open, with no hardn.yml and with one that lacks the key: the question
// would otherwise come at every open.
func TestReleaseAskStateNotKept(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		b := useAsking(t)
		b.f.AddDir(revert.DefaultDir, 0o700, 1000, 1000)
		c := config.DefaultConfig()
		if legacy {
			c = b.yml(t, legacyYML)
		}
		for i := 0; i < 2*version.AskOpens; i++ {
			b.open(c)
		}
		yml, _ := b.f.Entry(ymlPath)
		if b.asked != 0 || c.CheckReleases || b.state() != "" || b.out.Len() != 0 || legacy && string(yml.Data) != legacyYML {
			t.Errorf("hardn.yml %t: asked %d, on %t, state %q, said %q, hardn.yml %q",
				legacy, b.asked, c.CheckReleases, b.state(), b.out.String(), yml.Data)
		}
	}
}

// setState puts hardn's state on the bench's host.
func (b *asking) setState(content string) {
	b.f.AddDir(revert.DefaultDir, 0o700, 0, 0)
	b.f.AddFile(askState, []byte(content), 0o600, 0, 0)
}

// TestReleaseAskOwnFalse: a checkReleases: false the question wrote to
// hardn.yml is asked about again: not for 14 opens, at the 15th, and 30
// days on. A no keeps false and restarts the count; the end of the input
// writes nothing and keeps the mark; a yes writes true, drops the mark and
// is never asked again.
func TestReleaseAskOwnFalse(t *testing.T) {
	b := useAsking(t, "n", "n", "eof", "y")
	b.open(b.yml(t, legacyYML))
	no := legacyYML + "checkReleases: false\n"
	same := func(when string) {
		t.Helper()
		if yml, _ := b.f.Entry(ymlPath); string(yml.Data) != no || !strings.Contains(b.state(), `"wroteFalse":"`+ymlPath+`"`) {
			t.Fatalf("%s: hardn.yml %q, state %q", when, yml.Data, b.state())
		}
	}
	same("the first no")
	notFor := func(when string) {
		t.Helper()
		for i := 1; i < version.AskOpens; i++ {
			if b.open(b.read(t)) != 0 {
				t.Fatalf("%s: asked at open %d", when, i)
			}
		}
	}
	notFor("the first no")
	b.out.Reset()
	if c := b.read(t); b.open(c) != 1 || c.CheckReleases || b.out.String() != "Saved checkReleases: false to "+ymlPath+"\n" {
		t.Fatalf("15th open: asked %d, on %t, said %q", b.asked, c.CheckReleases, b.out.String())
	}
	same("the second no")
	notFor("the second no")
	b.now = b.now.Add(version.AskEvery)
	if b.open(b.read(t)) != 1 {
		t.Fatal("not asked 30 days on")
	}
	same("the end of the input")
	notFor("the end of the input")
	b.now = b.now.Add(version.AskEvery)
	c := b.read(t)
	if b.open(c) != 1 || !c.CheckReleases {
		t.Fatalf("not asked, or off after a yes: %t", c.CheckReleases)
	}
	if yml, _ := b.f.Entry(ymlPath); string(yml.Data) != legacyYML+"checkReleases: true\n" || strings.Contains(b.state(), "wroteFalse") {
		t.Fatalf("after the yes: hardn.yml %q, state %q", yml.Data, b.state())
	}
	b.now = b.now.Add(10 * version.AskEvery)
	for i := 0; i < 2*version.AskOpens; i++ {
		if b.open(b.read(t)) != 0 {
			t.Fatalf("asked at open %d after the yes", i)
		}
	}
}

// TestReleaseAskOwnFalseNotSaved: where the question's own false is asked
// about again and the answer can't be saved (hardn.yml now merges with
// <<), the mark is kept and it is asked again by the count.
func TestReleaseAskOwnFalseNotSaved(t *testing.T) {
	const anchored = "base: &b\n  x: 1\n<<: *b\ncheckReleases: false\n"
	b := useAsking(t, "n", "y", "y")
	b.open(b.yml(t, legacyYML))
	b.f.AddFile(ymlPath, []byte(anchored), 0o644, 0, 0)
	for i := 1; i < version.AskOpens; i++ {
		b.open(b.read(t))
	}
	if c := b.read(t); b.open(c) != 1 || c.CheckReleases || !strings.Contains(b.state(), `"wroteFalse":"`+ymlPath+`"`) {
		t.Fatalf("15th open: asked %d, on %t, state %q", b.asked, c.CheckReleases, b.state())
	}
	for i := 1; i < version.AskOpens; i++ {
		if b.open(b.read(t)) != 0 {
			t.Fatalf("asked at open %d after the failed save", i)
		}
	}
	if b.open(b.read(t)) != 1 {
		t.Error("not asked again at the 15th open after the failed save")
	}
	if yml, _ := b.f.Entry(ymlPath); string(yml.Data) != anchored {
		t.Errorf("hardn.yml is %q", yml.Data)
	}
}

// TestReleaseAskFalseNotOwn: a false the question did not write is never
// asked about: one set by hand, one under v0.10.3's state (which can't be
// told from a no kept by hand later), one where the mark names another
// hardn.yml, one set after hardn.yml said true or left the key out, or
// one the first-run questions wrote to the hardn.yml this run created.
// The mark is dropped where the file loaded is another, none is, it says
// true or not the key, or this run created it.
func TestReleaseAskFalseNotOwn(t *testing.T) {
	const (
		marked = `{"answer":"no","asked":"2026-11-15T12:00:00Z","opens":15,"wroteFalse":"` + ymlPath + `"}` + "\n"
		other  = `{"answer":"no","asked":"2026-11-15T12:00:00Z","opens":15,"wroteFalse":"/root/hardn.yml"}` + "\n"
		v0103  = `{"answer":"none","asked":"2026-11-15T12:00:00Z","opens":15}` + "\n"
	)
	for _, tc := range []struct {
		name, state, yml string
		created          bool
	}{
		{"set by hand", "", "checkReleases: false\n", false},
		{"v0.10.3's no", v0103, legacyYML + "checkReleases: false\n", false},
		{"another hardn.yml's mark", other, "checkReleases: false\n", false},
		{"true, then false", marked, "checkReleases: true\n", false},
		{"the key left out, then false", strings.Replace(marked, `"opens":15`, `"opens":0`, 1), legacyYML, false},
		{"created by this run", marked, "checkReleases: false\n", true},
		{"no hardn.yml", strings.Replace(marked, `"opens":15`, `"opens":0`, 1), "", false},
	} {
		b := useAsking(t)
		if tc.state != "" {
			b.setState(tc.state)
		}
		load := func() *config.Config {
			if tc.yml == "" {
				return config.DefaultConfig()
			}
			return b.yml(t, tc.yml)
		}
		if b.openNew(load(), tc.created) != 0 || strings.Contains(b.state(), "wroteFalse") {
			t.Fatalf("%s: asked, or the mark kept: %q", tc.name, b.state())
		}
		if tc.yml == "" {
			continue
		}
		if tc.yml = strings.Replace(tc.yml, "true", "false", 1); tc.yml == legacyYML {
			tc.yml += "checkReleases: false\n"
		}
		b.now = b.now.Add(10 * version.AskEvery)
		for i := 0; i < 2*version.AskOpens; i++ {
			if b.open(load()) != 0 {
				t.Fatalf("%s: asked at open %d", tc.name, i)
			}
		}
		if tc.state == "" && b.state() != "" {
			t.Errorf("%s: state written: %q", tc.name, b.state())
		}
	}
}

// TestReleaseAskMarkNotAsking: off a terminal and under -n the mark is
// neither read for a question nor dropped: nothing written.
func TestReleaseAskMarkNotAsking(t *testing.T) {
	const marked = `{"answer":"no","asked":"2026-11-15T12:00:00Z","opens":15,"wroteFalse":"` + ymlPath + `"}` + "\n"
	for _, yml := range []string{"checkReleases: false\n", "checkReleases: true\n"} {
		b := useAsking(t)
		b.setState(marked)
		releaseAskTerminals = func() bool { return false }
		if b.open(b.yml(t, yml)) != 0 || b.state() != marked {
			t.Errorf("%q: asked, or state %q", yml, b.state())
		}
		releaseAskTerminals = func() bool { return true }
		c := b.yml(t, yml)
		c.ResolveDryRun(true, true)
		if b.open(c) != 0 || b.state() != marked {
			t.Errorf("%q under -n: asked, or state %q", yml, b.state())
		}
	}
}
