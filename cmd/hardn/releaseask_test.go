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
func (b *asking) open(c *config.Config) int {
	before := b.asked
	releaseAsk(context.Background(), c)
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
// wins: no check, no request, and the state not read.
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
	calls := len(b.f.Calls())
	if b.open(off) != 0 || off.CheckReleases || len(b.f.Calls()) != calls {
		t.Errorf("hardn.yml says false: asked %d, on %t, host calls %v", b.asked, off.CheckReleases, b.f.Calls()[calls:])
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
// every other byte kept; a yes is on in this session. With the key set it
// is never asked about again; hardn's state holds no answer, only that the
// question was due (version.Asker.Due).
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
		if want := "Saved checkReleases: " + word + " to " + ymlPath + "\n"; b.out.String() != want || !strings.Contains(b.state(), `"answer":"none"`) {
			t.Errorf("%q: said %q, want %q; state %q", tc.answer, b.out.String(), want, b.state())
		}
		if m := withReleaseCheck(mainmenu.Options{}, c, "0.4.0"); (m.CheckRelease != nil) != tc.on {
			t.Errorf("%q: the main menu's check is %t", tc.answer, m.CheckRelease != nil)
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
			!strings.Contains(b.state(), `"answer":"none"`) {
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
