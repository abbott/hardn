package configscreen_test

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

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	configscreen "github.com/abbott/hardn/pkg/screens/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The files the Configuration screen's tests show, each loaded by the real loader
// (config.Read) from a fake host that holds the file alone. Synthetic values only.

const etc = "/etc/hardn/hardn.yml"

// typical is a hardn.yml that sets some keys and leaves the rest out.
const typical = "username: george\nsshKeys:\n  - \"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAISYNTHETICKEY george@laptop-1\"\n" +
	"sshPort: 2222\ndisableRootSSH: true\nconfigureDns: true\nnameservers:\n  - 192.0.2.53\n  - 192.0.2.54\nenableUnattendedUpgrades: true\n"

// read is yml at path as hardn loads it.
func read(t *testing.T, path, yml string) *config.Config {
	t.Helper()
	h := hostfake.New(host.Options{})
	h.AddFile(path, []byte(yml), 0o600, 0, 0)
	cfg, err := config.Read(context.Background(), h, path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// found is a file that is there, looked for by by.
func found(path string, by config.FoundBy) config.Found {
	return config.Found{Path: path, By: by, Exists: true}
}

// off is hardn's environment off a terminal, 80 columns.
func off() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80, Height: 24}) }

// played is one run of the screen: what it wrote with the answers echoed, what Run
// returned, how many lines were left unread.
type played struct {
	out    string
	err    error
	unread int
}

// play runs the screen on cfg and f, plain at 80 columns, the user typing lines.
func play(cfg *config.Config, f config.Found, lines ...string) played {
	var out bytes.Buffer
	env := off()
	ty := uitest.NewTypist(&out, lines...)
	err := configscreen.Run(context.Background(), configscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		Config:   cfg, Found: f,
	})
	return played{out.String(), err, ty.Unread()}
}

// has fails t for each of want that out does not hold.
func has(t *testing.T, name, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("%s: no %q in:\n%s", name, w, out)
		}
	}
}

// TestFrom: the file and how it was found, in the decided words: -f, HARDN_CONFIG,
// the search path; then the screen's sections, Return alone and its hint.
func TestFrom(t *testing.T) {
	for _, tt := range []struct {
		path string
		by   config.FoundBy
		from string
	}{
		{"/root/site.yml", config.FoundFlag, "-f"},
		{"/home/george/.config/hardn/hardn.yml", config.FoundEnv, "HARDN_CONFIG"},
		{etc, config.FoundSearch, "Search path"},
	} {
		p := play(read(t, tt.path, typical), found(tt.path, tt.by), "0")
		if p.err != nil || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d unread", tt.from, p.err, p.unread)
		}
		has(t, tt.from, p.out, "# Configuration ###", "  File ---", "   -  Path                 "+tt.path+"\n", "   -  From                 "+tt.from+"\n",
			"  Users ---", "  SSH ---", "  Firewall ---", "  DNS ---", "  Auto Updates ---", "  Backup ---", "  General ---",
			"   0) Return", "[0 or q]:")
		for _, w := range []string{"not found", "hardn is using its defaults.", "Not used", "None found", "1)"} {
			if strings.Contains(p.out, w) {
				t.Errorf("%s: %q in:\n%s", tt.from, w, p.out)
			}
		}
	}
}

// TestNoFile: with no file anywhere, and with one that -f or HARDN_CONFIG names and
// is not there, the screen says so and that the defaults are in use, every row a
// default. A configuration loaded from no file reads so whatever found says is there.
func TestNoFile(t *testing.T) {
	for _, tt := range []struct {
		name string
		f    config.Found
		want []string
		not  string
	}{
		{"none", config.Found{}, []string{"   -  Path                 None found\n"}, "From"},
		{"-f missing", config.Found{Path: "/root/site.yml", By: config.FoundFlag},
			[]string{"  [ ] Path                 /root/site.yml not found\n", "   -  From                 -f\n"}, "None found"},
		{"HARDN_CONFIG missing", config.Found{Path: "/home/george/hardn.yml", By: config.FoundEnv},
			[]string{"  [ ] Path                 /home/george/hardn.yml not found\n", "   -  From                 HARDN_CONFIG\n"}, "None found"},
		{"none loaded, one on the search path", found(etc, config.FoundSearch), []string{"   -  Path                 None found\n"}, etc},
		{"none loaded, -f's there", found("/root/site.yml", config.FoundFlag), []string{"  [ ] Path                 /root/site.yml not found\n"}, "None found"},
	} {
		p := play(config.DefaultConfig(), tt.f, "0")
		if p.err != nil {
			t.Errorf("%s: Run = %v", tt.name, p.err)
		}
		has(t, tt.name, p.out, append(tt.want, "   -  hardn is using its defaults.\n", "   -  username             Not set (default)\n",
			"   -  sshPort              22 (default)\n", "   -  sshKeys              None (default)\n", "   -  confirmTimeout       3m (default)\n")...)
		if strings.Contains(p.out, tt.not) {
			t.Errorf("%s: %q in:\n%s", tt.name, tt.not, p.out)
		}
		for _, l := range strings.Split(p.out, "\n") {
			if rest, ok := strings.CutPrefix(l, "   -  "); ok && !strings.HasSuffix(l, "(default)") &&
				!slices.ContainsFunc([]string{"Path", "From", "hardn is using", "sshConfigFile", "enableUnattendedUpgrades"}, func(k string) bool { return strings.HasPrefix(rest, k) }) {
				t.Errorf("%s: a row that is no default: %q", tt.name, l)
			}
		}
	}
}

// TestDefaultsMarked: a key the file sets shows its value alone, a key it leaves out
// or leaves null "(default)"; an empty confirmTimeout is the default; disableRoot
// sets disableRootSSH; sshKeys is a count, never a key.
func TestDefaultsMarked(t *testing.T) {
	p := play(read(t, etc, typical), found(etc, config.FoundSearch), "0")
	has(t, "typical", p.out,
		"   -  username             george\n", "   -  adminNoPassword      false (default)\n", "   -  sshKeys              1 key\n",
		"   -  sshPort              2222\n", "   -  disableRootSSH       true\n",
		"   -  enableUfwSshPolicy   false (default)\n", "   -  ufwAppProfiles       None (default)\n", "   -  ufwTrustedInterfaces None (default)\n",
		"   -  configureDns         true\n", "   -  nameservers          2: 192.0.2.53, 192.0.2.54\n",
		"   -  enableUnattendedUpgrades\n      true\n",
		"   -  enableBackups        true (default)\n", "   -  backupPath           /var/backups/hardn (default)\n",
		"   -  dryRun               false (default)\n", "   -  confirmTimeout       3m (default)\n")
	if strings.Contains(p.out, "AAAAC3") || strings.Contains(p.out, "george@laptop-1") {
		t.Errorf("a key's text:\n%s", p.out)
	}
	p = play(read(t, etc, "sshPort:\nnameservers: ~\nconfirmTimeout: \"\"\ndisableRoot: true\nsshKeys: [a, b]\nenableBackups: true\nufwTrustedInterfaces: [tailscale0]\n"+
		"ufwAppProfiles:\n  - name: tailscale\n    ports: [\"41641/udp\"]\n  - name: web\n    ports: [\"443/tcp\"]\n"), found(etc, config.FoundSearch), "0")
	has(t, "as the loader reads", p.out, "   -  sshPort              22 (default)\n", "   -  nameservers          None (default)\n",
		"   -  confirmTimeout       3m (default)\n", "   -  disableRootSSH       true\n", "   -  sshKeys              2 keys\n",
		"   -  enableBackups        true\n", "   -  ufwTrustedInterfaces 1: tailscale0\n", "   -  ufwAppProfiles       tailscale, web\n")
}

// TestLongValueKeepsItsNote: a value too wide to sit beside its key with its note
// goes under the key, that row alone, the note whole: sshConfigFile's default, and
// a missing file's long path; one too wide even there is cut ahead of the note.
func TestLongValueKeepsItsNote(t *testing.T) {
	p := play(read(t, etc, typical), found(etc, config.FoundSearch), "0")
	has(t, "sshConfigFile", p.out, "   -  disableRootSSH       true\n   -  sshConfigFile\n      /etc/ssh/sshd_config.d/hardn.conf (default)\n")
	set := play(read(t, etc, "sshConfigFile: /etc/ssh/sshd_config.d/hardn.conf\n"), found(etc, config.FoundSearch), "0")
	has(t, "sshConfigFile set", set.out, "   -  sshConfigFile        /etc/ssh/sshd_config.d/hardn.conf\n")
	long := "/home/george/" + strings.Repeat("deep/", 12) + "hardn.yml"
	p = play(config.DefaultConfig(), config.Found{Path: long, By: config.FoundEnv}, "0")
	has(t, "a long missing path", p.out, "  [ ] Path\n      /home/george/deep/deep/deep/deep/deep/deep/de... not found\n")
	uitest.AssertFits(t, p.out, 80)
}

// TestNotUsed: the keys the file sets that nothing reads, in the start-up warning's
// order: logFile where it names another path, permitRootLogin where it is not the
// default, then the retired keys, each with its value where that is one word or
// number; a retired key at its old default, and a mistyped key, are not listed.
func TestNotUsed(t *testing.T) {
	yml := typical + "sudoNoPassword: false\nenableLynis: true\nsshListenAddress: 192.0.2.10\nsshAllowedUsers: [bob]\n" +
		"logFile: /srv/log/hardn.log\npermitRootLogin: \"yes\"\nenableAppArmor: false\nusernme: typo\n"
	p := play(read(t, etc, yml), found(etc, config.FoundSearch), "0")
	_, section, ok := strings.Cut(p.out, "  Not used ---")
	if !ok {
		t.Fatalf("no Not used section:\n%s", p.out)
	}
	rows, _, _ := strings.Cut(strings.TrimLeft(section, "-\n"), "\n\n")
	want := "  [ ] logFile              /srv/log/hardn.log\n  [ ] permitRootLogin      yes\n  [ ] sshAllowedUsers      [\"bob\"]\n" +
		"  [ ] sshListenAddress     192.0.2.10\n  [ ] enableLynis          true\n  [ ] sudoNoPassword       false"
	if rows != want {
		t.Errorf("Not used:\n%s\nwant:\n%s", rows, want)
	}
	if strings.Contains(p.out, "enableAppArmor") || strings.Contains(p.out, "usernme") {
		t.Errorf("a key at its old default, or a mistyped one, is listed:\n%s", p.out)
	}
}

// TestSafe: a control character in any string of the file's, or in the path, never
// reaches the terminal.
func TestSafe(t *testing.T) {
	yml := "username: \"a\\e[2Jb\"\nbackupPath: \"/var/\\u202Ebackups\"\nnameservers: [\"192.0.2.53\\a\"]\nufwTrustedInterfaces: [\"t\\e]0;x\\a\"]\n" +
		"confirmTimeout: \"3m\\e[1m\"\nsshConfigFile: \"/etc/ssh/\\r\\nx\"\nlogFile: \"/srv/\\e[31mlog\"\nsshListenAddress: \"192.0.2.10\\e[0m\"\n" +
		"ufwAppProfiles:\n  - name: \"web\\e[5m\"\n    ports: [\"443/tcp\"]\n"
	const odd = "/etc/hardn/\x1b[2Jhardn.yml"
	p := play(read(t, odd, yml), found(odd, config.FoundFlag), "0")
	if p.err != nil {
		t.Fatal(p.err)
	}
	for _, r := range p.out {
		if (r < 0x20 && r != '\n') || r == 0x7f || r == 0x202e {
			t.Fatalf("%U reaches the terminal:\n%q", r, p.out)
		}
	}
	has(t, "safe", p.out, "username             a^[[2Jb\n", "/var/<U+202E>backups\n", "/etc/hardn/^[[2Jhardn.yml\n", "192.0.2.10^[[0m\n")
}

// TestTheFilesOwn: dryRun and username are the file's, not the session's: -n makes
// the session plan only and -u names its user, and the screen shows neither.
func TestTheFilesOwn(t *testing.T) {
	cfg := read(t, etc, "sshPort: 2222\n")
	cfg.ResolveDryRun(true, true) // -n
	cfg.Username = "bob"          // -u bob
	p := play(cfg, found(etc, config.FoundSearch), "0")
	has(t, "-n -u bob", p.out, "   -  dryRun               false (default)\n", "   -  username             Not set (default)\n")
	cfg = read(t, etc, "username: george\ndryRun: true\n")
	cfg.ResolveDryRun(true, false) // --dry-run=false
	cfg.Username = "bob"
	p = play(cfg, found(etc, config.FoundSearch), "0")
	has(t, "the file's", p.out, "   -  dryRun               true\n", "   -  username             george\n")
}

// TestTheFileRead: the screen names the file hardn read and how it found it then,
// whatever found says of another path: -f's file, relative, by its absolute path; a
// file read where found says none was there, as the first run's offer creates one,
// with no "From", a mistyped -f's and a missing HARDN_CONFIG's too.
func TestTheFileRead(t *testing.T) {
	const site = "/root/site.yml"
	for _, tt := range []struct {
		name, path string
		f          config.Found
		want       string
		not        []string
	}{
		{"-f, relative", site, found("site.yml", config.FoundFlag), "   -  Path                 " + site + "\n   -  From                 -f\n", nil},
		{"created, said to be on the search path", etc, found(etc, config.FoundSearch), "   -  Path                 " + etc + "\n   -  From                 Search path\n", nil},
		{"another path found", etc, found("/root/other.yml", config.FoundSearch), "   -  Path                 " + etc + "\n   -  From                 Search path\n", []string{"other.yml"}},
		{"created, none found", site, config.Found{}, "   -  Path                 " + site + "\n\n", []string{"From"}},
		{"created, -f mistyped", etc, config.Found{Path: "/root/typo.yml", By: config.FoundFlag},
			"   -  Path                 " + etc + "\n\n", []string{"From", "typo.yml"}},
		{"created, HARDN_CONFIG's missing", etc, config.Found{Path: "/home/george/hardn.yml", By: config.FoundEnv},
			"   -  Path                 " + etc + "\n\n", []string{"From", "/home/george"}},
	} {
		p := play(read(t, tt.path, typical), tt.f, "0")
		if p.err != nil {
			t.Errorf("%s: Run = %v", tt.name, p.err)
		}
		has(t, tt.name, p.out, tt.want, "   -  username             george\n", "   -  sshPort              2222\n")
		for _, w := range append(tt.not, "not found", "None found", "hardn is using its defaults.", "[ ] Path") {
			if strings.Contains(p.out, w) {
				t.Errorf("%s: %q in:\n%s", tt.name, w, p.out)
			}
		}
	}
}

// TestUsernameAsLoaded: username is the string hardn uses, not the number or date
// its YAML scalar also reads as.
func TestUsernameAsLoaded(t *testing.T) {
	for _, name := range []string{"0755", "0x10", "1.5", "2026-01-01", "true", "george"} {
		cfg := read(t, etc, "username: "+name+"\n")
		if cfg.Username != name {
			t.Fatalf("username: %s loads as %q", name, cfg.Username)
		}
		p := play(cfg, found(etc, config.FoundSearch), "0")
		has(t, name, p.out, "   -  username             "+name+"\n")
	}
}

// TestMergedKeys: a key the file sets through a YAML merge shows the value hardn
// loaded and is no default.
func TestMergedKeys(t *testing.T) {
	cfg := read(t, etc, "base: &b {sshPort: 2222, username: al}\n<<: *b\n")
	if cfg.SshPort != 2222 || cfg.Username != "al" {
		t.Fatalf("loaded sshPort %d, username %q", cfg.SshPort, cfg.Username)
	}
	p := play(cfg, found(etc, config.FoundSearch), "0")
	has(t, "merged", p.out, "   -  sshPort              2222\n", "   -  username             al\n")
}

// TestKeys: 0 goes back (nil) and q quits (nav.ErrQuit); any other line is refused
// and asked again, the screen has no item; the end of the input aborts.
func TestKeys(t *testing.T) {
	cfg, f := read(t, etc, typical), found(etc, config.FoundSearch)
	if p := play(cfg, f, "0"); p.err != nil || p.unread != 0 {
		t.Errorf("0: Run = %v, %d unread", p.err, p.unread)
	}
	if p := play(cfg, f, "q", "0"); !errors.Is(p.err, nav.ErrQuit) || p.unread != 1 {
		t.Errorf("q: Run = %v, %d unread", p.err, p.unread)
	}
	p := play(cfg, f, "1", "x", "0")
	if p.err != nil || strings.Count(p.out, "Invalid option. Please try again.") != 2 || strings.Count(p.out, "# Configuration ###") != 1 {
		t.Errorf("1, x, 0: Run = %v:\n%s", p.err, p.out)
	}
	if p := play(cfg, f); !errors.Is(p.err, prompt.ErrAborted) {
		t.Errorf("the end of the input: Run = %v", p.err)
	}
}

// TestInvalidOptions: without a Prompter's In and Out or a Config, Run refuses and
// writes nothing.
func TestInvalidOptions(t *testing.T) {
	var out bytes.Buffer
	in := prompt.New(uitest.NewTypist(&out, "0"), false)
	for name, o := range map[string]configscreen.Options{
		"no In":     {Prompter: nav.Prompter{Out: &out}, Config: config.DefaultConfig()},
		"no Out":    {Prompter: nav.Prompter{In: in}, Config: config.DefaultConfig()},
		"no Config": {Prompter: nav.Prompter{In: in, Out: &out}},
	} {
		if err := configscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) || out.Len() != 0 {
			t.Errorf("%s: Run = %v, wrote %q", name, err, out.String())
		}
	}
}

// TestViewHasEveryKey: View gives a value for every key the screen has a row for,
// and no other.
func TestViewHasEveryKey(t *testing.T) {
	v := configscreen.View(config.DefaultConfig(), config.Found{})
	keys := screen.ConfigKeys()
	if len(v.Values) != len(keys) {
		t.Errorf("%d values for %d keys", len(v.Values), len(keys))
	}
	for _, k := range keys {
		if _, ok := v.Values[k]; !ok {
			t.Errorf("no value for %s", k)
		}
	}
}

// TestEveryConfigKeyHasARow: every top-level yaml key of config.Config has a row,
// but for the two shown under "Not used" alone (TestNotUsed), and every row is a key
// of Config's: a new key cannot go without a row unseen.
func TestEveryConfigKeyHasARow(t *testing.T) {
	notUsed := []string{"logFile", "permitRootLogin"}
	rows := screen.ConfigKeys()
	var keys []string
	for f := range reflect.TypeFor[config.Config]().Fields() {
		tag, ok := f.Tag.Lookup("yaml")
		if !ok {
			if f.IsExported() {
				t.Errorf("config.Config.%s has no yaml tag", f.Name)
			}
			continue
		}
		key, _, _ := strings.Cut(tag, ",")
		keys = append(keys, key)
		if slices.Contains(rows, key) == slices.Contains(notUsed, key) {
			t.Errorf("hardn.yml's %s: a row %t, Not used alone %t; want one of the two", key, slices.Contains(rows, key), slices.Contains(notUsed, key))
		}
	}
	for _, k := range append(rows, notUsed...) {
		if !slices.Contains(keys, k) {
			t.Errorf("%s is no yaml key of config.Config", k)
		}
	}
	if len(keys) < 10 {
		t.Errorf("read %d keys of config.Config: %v", len(keys), keys)
	}
}

// TestPrint: the report is View's, with no menu and no prompt; off a terminal plain,
// whatever the environment says of colour; nothing written without Out or a Config.
func TestPrint(t *testing.T) {
	cfg, found := config.DefaultConfig(), config.Found{Path: "/root/typo.yml", By: config.FoundFlag}
	var out bytes.Buffer
	env := term.Resolve(term.Options{Color: term.ColorAlways}, term.Facts{Width: 80, Height: 24})
	if err := configscreen.Print(configscreen.PrintOptions{Out: &out, Env: env, Config: cfg, Found: found}); err != nil {
		t.Fatal(err)
	}
	c := component.OffTerminal()
	var want bytes.Buffer
	_, _ = io.WriteString(c.Env.Writer(&want), configscreen.View(cfg, found).Report(c)+"\n")
	if got := out.String(); got != want.String() || strings.Contains(got, "\x1b") || strings.Contains(got, "Return") ||
		!strings.Contains(got, "/root/typo.yml") || !strings.Contains(got, "not found") {
		t.Errorf("the report:\n%s\nwant:\n%s", got, want.String())
	}
	out.Reset()
	for _, o := range []configscreen.PrintOptions{{Config: cfg}, {Out: &out}} {
		if err := configscreen.Print(o); !errors.Is(err, fs.ErrInvalid) || out.Len() != 0 {
			t.Errorf("configscreen.Print(%+v) = %v, wrote %q", o, err, out.String())
		}
	}
}
