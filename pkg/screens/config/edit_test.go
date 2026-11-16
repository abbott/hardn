package configscreen_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	alpinepkg "github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	configscreen "github.com/abbott/hardn/pkg/screens/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

var now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

var debian = detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
	Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}

// newEngine is an engine on h, progress its Options.Progress.
func newEngine(h *hostfake.System, progress func(engine.Event)) *engine.Engine {
	return engine.New(h, engine.Options{
		Revert:   revert.Options{Now: func() time.Time { return now }, Hardn: "/usr/local/bin/hardn"},
		Detect:   func(context.Context, host.System) (detect.Facts, error) { return debian, nil },
		Hold:     func() func() { return func() {} },
		PID:      5000,
		Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Progress: progress,
	})
}

// site is a host that holds yml at etc, and the file as hardn loads it.
func site(t *testing.T, yml string) (*hostfake.System, *config.Config) {
	t.Helper()
	h := hostfake.New(host.Options{})
	for _, d := range []string{"/etc", "/etc/hardn", "/var/lib", "/var/backups", "/run", "/var/log"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(etc, []byte(yml), 0o600, 0, 0)
	cfg, err := config.Read(context.Background(), h, etc)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

// edit runs the screen on h and cfg, the user typing lines; set, when not
// nil, changes the options first.
func edit(h *hostfake.System, cfg *config.Config, set func(*configscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := off()
	ty := uitest.NewTypist(&out, lines...)
	o := configscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		Config:   cfg, Found: found(etc, config.FoundSearch), System: h, Facts: debian, LogPath: output.LegacyLogPath,
		Now:       func() time.Time { return now },
		NewEngine: func(progress func(engine.Event)) *engine.Engine { return newEngine(h, progress) },
	}
	if set != nil {
		set(&o)
	}
	err := configscreen.Run(context.Background(), o)
	return played{out.String(), err, ty.Unread()}
}

// file is hardn.yml on h now.
func file(t *testing.T, h *hostfake.System) string {
	t.Helper()
	data, err := h.ReadFile(context.Background(), etc)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// key is a whole ed25519 public key, synthetic: its blob is seed 32 times.
func key(seed byte, comment string) string {
	blob := binary.BigEndian.AppendUint32(nil, 11)
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	blob = append(blob, bytes.Repeat([]byte{seed}, 32)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

// done is the transcript's part that ends one save.
const done = "  [+] Save the configuration  Done\n\n# Operation Successful\n"

// TestEditSaves: each kind of key saved, "Edit a setting" (1) then the key:
// only its lines change, comments kept; a key the file does not hold is
// added, one set back to its default written as that; a list takes one
// entry a save and its last one leaves []; then the line saying where the
// key acts, and the screen drawn again from the saved file.
func TestEditSaves(t *testing.T) {
	k1, k2 := key(1, "george@laptop-1"), key(2, "bob@pc")
	for _, tt := range []struct {
		name  string
		yml   string
		lines []string
		file  string
		saves int
		want  []string
	}{
		{"a yes/no key the file does not hold", "# site\nusername: george\n", []string{"9", "y"}, "# site\nusername: george\nkernel: true\n", 1,
			[]string{"# kernel ####", "  Save kernel: true? [y/N, 0 or q]: y", "\n  Run All applies it.\n", "   9) kernel               true\n"}},
		{"a yes/no key back to its default", "kernel: true # on\n", []string{"9", "y"}, "kernel: false # on\n", 1, []string{"  Save kernel: false? [y/N, 0 or q]: y"}},
		{"disableRootSSH", "disableRootSSH: true\nenableUfwSshPolicy: true\nconfigureDns: true\n", []string{"4", "y"}, "disableRootSSH: false\nenableUfwSshPolicy: true\nconfigureDns: true\n", 1,
			[]string{"  Save disableRootSSH: false? [y/N, 0 or q]: y", "\n  Run All applies it.\n", "   4) disableRootSSH       false\n"}},
		{"enableUfwSshPolicy", "disableRootSSH: true\nenableUfwSshPolicy: true\nconfigureDns: true\n", []string{"5", "y"}, "disableRootSSH: true\nenableUfwSshPolicy: false\nconfigureDns: true\n", 1,
			[]string{"  Save enableUfwSshPolicy: false? [y/N, 0 or q]: y", "\n  Run All applies it.\n", "   5) enableUfwSshPolicy   false\n"}},
		{"configureDns", "disableRootSSH: true\nenableUfwSshPolicy: true\nconfigureDns: true\n", []string{"6", "y"}, "disableRootSSH: true\nenableUfwSshPolicy: true\nconfigureDns: false\n", 1,
			[]string{"  Save configureDns: false? [y/N, 0 or q]: y", "\n  Run All applies it.\n", "   6) configureDns         false\n"}},
		{"checkReleases", "username: george\n", []string{"11", "y"}, "username: george\ncheckReleases: true\n", 1, []string{"\n  From the next start.\n"}},
		{"enableUnattendedUpgrades", "username: george\n", []string{"12", "y"}, "username: george\nenableUnattendedUpgrades: true\n", 1, []string{"\n  Run All applies it.\n"}},
		{"sshPort, its comment kept", "username: george\nsshPort: 2222 # moved\ndisableRootSSH: true\n", []string{"3", "2022", "y"},
			"username: george\nsshPort: 2022 # moved\ndisableRootSSH: true\n", 1,
			[]string{"  Enter SSH port [2222] [0 or q]: 2022\n", "      -sshPort: 2222 # moved\n      +sshPort: 2022 # moved\n", "  Save sshPort? [y/N, 0 or q]: y",
				"\n  sshd's port is not changed. Change it under SSH (2).\n", "   3) sshPort              2022\n"}},
		{"sshPort back to its default", "sshPort: 2222\n", []string{"3", "22", "y"}, "sshPort: 22\n", 1, nil},
		{"sshPort as it is", "username: george\n", []string{"3", ""}, "username: george\n", 0, []string{"  Nothing needs changing.\n"}},
		{"sshPort declined", "sshPort: 2222\n", []string{"3", "2022", "n"}, "sshPort: 2222\n", 0, []string{"# Operation Cancelled"}},
		{"username", "username: george\n", []string{"1", "bob", "y"}, "username: bob\n", 1, []string{"\n  Run All applies it.\n", "   1) username             bob\n"}},
		{"username emptied", "username: george\n", []string{"1", "", "y"}, "username: \"\"\n", 1, []string{"   1) username             Not set\n"}},
		{"confirmTimeout set, then emptied", "username: george\n", []string{"10", "5m", "y", "10", "", "y"}, "username: george\nconfirmTimeout: \"\"\n", 2,
			[]string{"  10) confirmTimeout       5m\n", "+confirmTimeout: 5m\n"}},
		{"sshKeys: two added, both removed", "username: george\n", []string{"2", "1", k1, "y", "1", k2, "y", "2", "1", "y", "2", "1", "y"}, "username: george\nsshKeys: []\n", 4,
			[]string{"  Enter SSH public key [0 or q]: ", "\n  Run All applies it.\n", "ssh-ed25519 george@laptop-1 SHA256:", "  Select a key to remove\n", "   -  Key                  None\n"}},
		{"ntpServers: one added, both removed", "ntpServers:\n  - time.example.net # ours\n", []string{"8", "1", "192.0.2.10", "y", "2", "1", "y", "2", "1", "y"}, "ntpServers: []\n", 3,
			[]string{"      +  - 192.0.2.10\n", "\n  Set them under NTP Settings, or with Run All.\n", "   -  Server               time.example.net\n   -  Server               192.0.2.10\n",
				"  Select a server to remove\n", "   1) Add server\n\n   0) Return"}},
		{"syslogServers: two added, both removed", "username: george\n",
			[]string{"7", "1", "192.0.2.50", "udp", "1514", "y", "1", "logs.example.net", "", "", "y", "2", "1", "y", "2", "1", "y"}, "username: george\nsyslogServers: []\n", 4,
			[]string{"      +  - server: 192.0.2.50\n      +    port: 1514\n      +    protocol: udp\n", "      +  - server: logs.example.net\n\n",
				"\n  Set them under Syslog Settings, or with Run All.\n", "   -  Server               192.0.2.50:1514 UDP\n   -  Server               logs.example.net:514 TCP\n", "   3. Enter port [514] [0 or q]: "}},
		{"syslogServers: tls", "username: george\n", []string{"7", "1", "logs.example.net", "tls", "", "/etc/ssl/ca.pem", "y"},
			"username: george\nsyslogServers:\n  - server: logs.example.net\n    protocol: tls\n    caFile: /etc/ssl/ca.pem\n", 1,
			[]string{"   3. Enter port [6514] [0 or q]: ", "   4. Enter CA file [0 or q]: /etc/ssl/ca.pem", "   -  Server               logs.example.net:6514 TLS\n"}},
	} {
		h, cfg := site(t, tt.yml)
		p := edit(h, cfg, nil, append(append([]string{"1"}, tt.lines...), "q")...)
		if !errors.Is(p.err, nav.ErrQuit) || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d unread", tt.name, p.err, p.unread)
		}
		if got := file(t, h); got != tt.file {
			t.Errorf("%s: the file is\n%s\nwant\n%s\n%s", tt.name, got, tt.file, p.out)
		}
		if n := strings.Count(p.out, done); n != tt.saves {
			t.Errorf("%s: %d saves, want %d:\n%s", tt.name, n, tt.saves, p.out)
		}
		has(t, tt.name, p.out, tt.want...)
		if data := []byte(file(t, h)); !cfg.LoadedFrom(data) {
			t.Errorf("%s: the screen does not hold the file as saved", tt.name)
		}
	}
}

// TestEditListKept: one entry removed from a list leaves the others,
// each with its comment.
func TestEditListKept(t *testing.T) {
	h, cfg := site(t, "syslogServers:\n  - server: 192.0.2.50 # first\n  - server: 192.0.2.51 # second\n    protocol: udp\nsshPort: 2222\n")
	p := edit(h, cfg, nil, "1", "7", "2", "1", "y", "q")
	if got, want := file(t, h), "syslogServers:\n  - server: 192.0.2.51 # second\n    protocol: udp\nsshPort: 2222\n"; got != want {
		t.Errorf("the file is\n%s\n%s", got, p.out)
	}
}

// TestEditRefused: an answer that is not valid is refused under its field
// and asked again, and nothing is saved: a port, a user name, a timeout, a
// key, an NTP server and each field of a syslog server, the whole list's
// check among them.
func TestEditRefused(t *testing.T) {
	yml := "username: george\nsshKeys:\n  - \"" + key(1, "george@laptop-1") + "\"\nntpServers:\n  - time.example.net\nsyslogServers:\n  - server: 192.0.2.50\n"
	for _, tt := range []struct {
		name  string
		lines []string
		want  []string
	}{
		{"port", []string{"3", "99999", "0", "22x", "65536", "-1"}, []string{"  (x) Not a port between 1 and 65535\n  Enter SSH port [22] [0 or q]: "}},
		{"username", []string{"1", "George", "1bob", "a b"}, []string{"  (x) Use lower-case letters, digits, - and _, starting with a\n      letter or _, up to 32\n  Enter username (Enter for none) [0 or q]: "}},
		{"timeout", []string{"10", "soon", "30s", "59"}, []string{"  (x) Not a time of 1m or more, such as 90s or 5m\n  Enter confirm timeout (Enter for 3m) [0 or q]: "}},
		{"key", []string{"2", "1", "ssh-ed25519 AAAA george", "not a key", ""}, []string{"  (x) Not an SSH public key hardn takes\n  Enter SSH public key [0 or q]: "}},
		{"key twice", []string{"2", "1", key(1, "again")}, []string{"This key is in sshKeys already"}},
		{"ntp", []string{"8", "1", "2001:db8::1", "time_example", "", "TIME.example.net"},
			[]string{"  (x) An IPv6 address, which hardn does not set yet\n  Enter NTP server [0 or q]: ", "  (x) Not an IPv4 address or a host name\n", "  (x) Server 2 is named twice\n"}},
		{"syslog server", []string{"7", "1", "2001:db8::1", "[::1]:514", ""}, []string{"  (x) An IPv6 address, which hardn does not set yet\n", "  (x) Not an IPv4 address or a host name\n"}},
		{"syslog protocol", []string{"7", "1", "192.0.2.51", "relp", "TCP", "tls"}, []string{"  (x) Not udp, tcp or tls\n", "  (x) TLS needs the server's name, as its certificate has it\n"}},
		{"syslog port", []string{"7", "1", "192.0.2.50", "udp", "70000", "x", ""}, []string{"  (x) Not a port between 1 and 65535\n", "  (x) The same address and port as server 1\n"}},
		{"syslog CA file", []string{"7", "1", "logs.example.net", "tls", "", "", "ca.pem", "/etc/ssl/my ca.pem"},
			[]string{"  (x) TLS needs caFile\n", "  (x) caFile is not an absolute path of letters, digits and . _\n      + - /\n"}},
	} {
		h, cfg := site(t, yml)
		p := edit(h, cfg, nil, append(append([]string{"1"}, tt.lines...), "q")...)
		if !errors.Is(p.err, nav.ErrQuit) || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d unread", tt.name, p.err, p.unread)
		}
		has(t, tt.name, p.out, tt.want...)
		if got := file(t, h); got != yml || strings.Contains(p.out, "Save the configuration") {
			t.Errorf("%s: saved, or planned:\n%s\n%s", tt.name, got, p.out)
		}
	}
	// A fifth server is refused by the whole list's check.
	h, cfg := site(t, "ntpServers: [192.0.2.1, 192.0.2.2, 192.0.2.3, 192.0.2.4]\n")
	p := edit(h, cfg, nil, "1", "8", "1", "192.0.2.5", "q")
	has(t, "a fifth", p.out, "  (x) More than 4 servers\n")
	// A fault of an entry the file already holds keeps its number, so that
	// it does not read as the typed server's.
	const held = "syslogServers:\n  - server: logs.example.net\n    protocol: tls\n"
	h, cfg = site(t, held)
	p = edit(h, cfg, nil, "1", "7", "1", "192.0.2.60", "", "", "q")
	has(t, "an entry already not valid", p.out, "  (x) Server 1: tls needs caFile\n")
	if got := file(t, h); got != held || strings.Contains(p.out, "Save the configuration") {
		t.Errorf("an entry already not valid: saved, or planned:\n%s\n%s", got, p.out)
	}
}

// TestEditFlags: -u's username and -n's mode, held in memory, are never
// written. Under -u alice an edit of another key leaves username as the
// file had it, and an edit of username saves the name typed while the
// session keeps alice; under plan only the plan is shown and nothing is
// written.
func TestEditFlags(t *testing.T) {
	const yml = "username: george\nsshPort: 2222\n"
	h, cfg := site(t, yml)
	cfg.Username = "alice"
	cfg.ResolveDryRun(true, false)
	p := edit(h, cfg, nil, "1", "3", "2022", "y", "q")
	if got := file(t, h); got != "username: george\nsshPort: 2022\n" || cfg.Username != "alice" || cfg.FileUsername() != "george" {
		t.Errorf("-u, another key: file\n%s\nusername %q, the file's %q\n%s", got, cfg.Username, cfg.FileUsername(), p.out)
	}
	has(t, "-u", p.out, "   1) username             george\n")
	p = edit(h, cfg, nil, "1", "1", "bob", "y", "q")
	if got := file(t, h); got != "username: bob\nsshPort: 2022\n" || cfg.Username != "alice" || cfg.FileUsername() != "bob" {
		t.Errorf("-u, username: file\n%s\nusername %q, the file's %q\n%s", got, cfg.Username, cfg.FileUsername(), p.out)
	}
	has(t, "-u, username", p.out, "   1) username             bob\n")

	h, cfg = site(t, yml)
	cfg.ResolveDryRun(true, true)
	p = edit(h, cfg, func(o *configscreen.Options) { o.DryRun = true }, "1", "3", "2022", "9", "1", "bob", "q")
	if got := file(t, h); got != yml || cfg.SshPort != 2222 || strings.Contains(p.out, "[y/N") || strings.Contains(p.out, "dryRun: ") {
		t.Errorf("-n: file\n%s\n%s", got, p.out)
	}
	has(t, "-n", p.out, "      -sshPort: 2222\n      +sshPort: 2022\n", "      +kernel: true\n", "      -username: george\n      +username: bob\n")
	for _, w := range []string{"sshd's port is not changed", "Run All applies it", "Operation Successful"} {
		if strings.Contains(p.out, w) {
			t.Errorf("-n: %q in:\n%s", w, p.out)
		}
	}
}

// TestEditSaveRefused: the save's own refusals, in its words, nothing
// saved and no line after: the file a link, changed since it was read, and
// one that merges keys (<<) into the mapping edited.
func TestEditSaveRefused(t *testing.T) {
	t.Run("a link", func(t *testing.T) {
		h := hostfake.New(host.Options{})
		for _, d := range []string{"/etc", "/etc/hardn", "/srv", "/var/lib", "/var/backups", "/run", "/var/log"} {
			h.AddDir(d, 0o755, 0, 0)
		}
		h.AddFile("/srv/hardn.yml", []byte("sshPort: 2222\n"), 0o600, 0, 0)
		h.AddSymlink(etc, "/srv/hardn.yml")
		cfg, err := config.Read(context.Background(), h, etc)
		if err != nil {
			t.Fatal(err)
		}
		p := edit(h, cfg, nil, "1", "3", "2022", "q")
		has(t, "a link", p.out, "# Operation Failed\n# "+etc+" is a link", "To go on: edit the file it links to by hand.")
		if data, _ := h.ReadFile(context.Background(), "/srv/hardn.yml"); string(data) != "sshPort: 2222\n" || strings.Contains(p.out, "sshd's port") {
			t.Errorf("saved through a link:\n%s", p.out)
		}
	})
	t.Run("changed since read", func(t *testing.T) {
		h, cfg := site(t, "sshPort: 2222\n")
		h.AddFile(etc, []byte("sshPort: 2222\nkernel: true\n"), 0o600, 0, 0)
		p := edit(h, cfg, nil, "1", "3", "2022", "q")
		has(t, "changed", p.out, "# Operation Failed\n# "+etc+" changed since it was read", "To go on: start again, so that the file is read as it is now.")
		if got := file(t, h); got != "sshPort: 2222\nkernel: true\n" || strings.Contains(p.out, "sshd's port") {
			t.Errorf("saved over a change:\n%s\n%s", got, p.out)
		}
	})
	t.Run("a merge", func(t *testing.T) {
		const yml = "base: &base\n  sshPort: 2222\n<<: *base\nusername: george\n"
		h, cfg := site(t, yml)
		p := edit(h, cfg, nil, "1", "3", "2022", "q")
		has(t, "a merge", p.out, "# Operation Failed\n# "+etc+" merges settings with <<", "To go on: set sshPort in "+etc+" by hand, then")
		if got := file(t, h); got != yml || strings.Contains(p.out, "sshd's port") {
			t.Errorf("saved into a merge:\n%s\n%s", got, p.out)
		}
	})
}

// alpine is a host whose logger is busybox syslogd, with yml at etc.
func alpine(t *testing.T, yml string) (*hostfake.System, *config.Config, detect.Facts) {
	t.Helper()
	h, cfg := site(t, yml)
	for _, d := range []string{"/etc/conf.d", "/lib", "/lib/apk", "/lib/apk/db"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(alpinepkg.InstalledDB, []byte("P:busybox\nV:1.37.0-r30\n\n"), 0o644, 0, 0)
	h.AddFile(logfwd.ConfD, []byte("SYSLOGD_OPTS=\"-t\"\n"), 0o644, 0, 0)
	f := debian
	f.Platform.Distro, f.Platform.Version = detect.DistroAlpine, "3.23"
	f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes}
	return h, cfg, f
}

// TestLoggerNote: decision 13. Where the host's logger is busybox syslogd
// and hardn.yml lists a TCP or TLS syslog server, the hardn.yml screen says
// so under syslogServers, and its editor on its screen and after a save;
// nothing with UDP servers alone, on a host with another logger, or where
// the logger is not read.
func TestLoggerNote(t *testing.T) {
	// The line, as a row draws it: it wraps.
	const note = "busybox syslogd is UDP only. Change protocol under Edit a\n      setting."
	const said = "# Warning\n# busybox syslogd is UDP only. Change protocol under Edit a\n# setting.\n"
	const tcp, udp = "syslogServers:\n  - server: 192.0.2.50\n", "syslogServers:\n  - server: 192.0.2.50\n    protocol: udp\n"
	facts := func(f detect.Facts) func(*configscreen.Options) {
		return func(o *configscreen.Options) { o.Facts = f }
	}
	h, cfg, f := alpine(t, tcp)
	p := edit(h, cfg, facts(f), "1", "7", "q")
	has(t, "tcp on busybox", p.out, "   -  syslogServers        192.0.2.50\n  [ ] "+note+"\n", "   -  Server               192.0.2.50:514 TCP\n  [ ] "+note+"\n")

	h, cfg, f = alpine(t, udp)
	p = edit(h, cfg, facts(f), "1", "7", "1", "192.0.2.51", "", "", "y", "q")
	if n, m := strings.Count(p.out, said), strings.Count(p.out, note); n != 1 || m != 1 || strings.Index(p.out, said) < strings.Index(p.out, "Set them under Syslog Settings") {
		t.Errorf("after a save: the warning %d times, the row %d:\n%s", n, m, p.out)
	}
	has(t, "after a save", p.out, "  Set them under Syslog Settings, or with Run All.\n")

	for name, run := range map[string]func() played{
		"udp on busybox": func() played {
			h, cfg, f := alpine(t, udp)
			return edit(h, cfg, facts(f), "1", "7", "q")
		},
		"tcp on rsyslog's host": func() played {
			h, cfg := site(t, tcp)
			return edit(h, cfg, nil, "1", "7", "q")
		},
		"the logger not read": func() played {
			h, cfg := site(t, tcp)
			f := debian
			f.Platform.Distro = detect.DistroAlpine // no apk database on h
			return edit(h, cfg, facts(f), "1", "7", "q")
		},
	} {
		if p := run(); strings.Contains(p.out, "busybox") || !errors.Is(p.err, nav.ErrQuit) {
			t.Errorf("%s: Run = %v:\n%s", name, p.err, p.out)
		}
	}
}

// TestRunNeeds: Run refuses, writing nothing, without a System or NewEngine.
func TestRunNeeds(t *testing.T) {
	h, cfg := site(t, "sshPort: 2222\n")
	for name, set := range map[string]func(*configscreen.Options){
		"no System":    func(o *configscreen.Options) { o.System = nil },
		"no NewEngine": func(o *configscreen.Options) { o.NewEngine = nil },
	} {
		if p := edit(h, cfg, set, "0"); !errors.Is(p.err, fs.ErrInvalid) || p.out != "" {
			t.Errorf("%s: Run = %v, wrote %q", name, p.err, p.out)
		}
	}
}

// TestEditKeysAreTheScreens: every key the editor numbers has a row on the
// hardn.yml screen, and none is a key another screen saves or one that
// stays read-only (decisions 1 (a) and 3 (a)).
func TestEditKeysAreTheScreens(t *testing.T) {
	rows := screen.ConfigKeys()
	for _, k := range screen.ConfigEditKeys() {
		if !slices.Contains(rows, k) {
			t.Errorf("%s has no row", k)
		}
	}
	for _, k := range []string{"dryRun", "adminNoPassword", "sshConfigFile", "nameservers", "ufwAppProfiles", "ufwTrustedInterfaces", "enableBackups", "backupPath"} {
		if slices.Contains(screen.ConfigEditKeys(), k) {
			t.Errorf("%s is edited", k)
		}
	}
	h, cfg := site(t, "dryRun: false\nadminNoPassword: false\n")
	p := edit(h, cfg, nil, "1", "q")
	_, menu, _ := strings.Cut(p.out, "# Edit a setting")
	for _, k := range []string{"dryRun", "adminNoPassword", "sshConfigFile"} {
		if strings.Contains(menu, k) {
			t.Errorf("%s in the editor's menu:\n%s", k, menu)
		}
	}
}

// TestEditChangeProtocol: syslogServers' Change protocol. The server picked
// by number; its protocol asked with the current as the default; a port that
// was the default follows the new protocol's, an explicit one is kept and not
// asked; tls asks the CA file; a refused answer is asked again; the same
// protocol changes nothing; only that server's lines change, the file's
// comments kept, those of the changed server too; under -n only the plan
// is shown.
func TestEditChangeProtocol(t *testing.T) {
	const two = "# site\nsyslogServers:\n  - server: 192.0.2.50 # first\n  - server: logs.example.net # second\n    port: 1514\nsshPort: 2222\n"
	for _, tt := range []struct {
		name  string
		yml   string
		lines []string
		file  string
		saves int
		want  []string
	}{
		{"tcp to udp, the port follows", two, []string{"1", "udp", "", "y"},
			"# site\nsyslogServers:\n  - server: 192.0.2.50 # first\n    protocol: udp\n  - server: logs.example.net # second\n    port: 1514\nsshPort: 2222\n", 1,
			[]string{"   1. Select a server\n", "   2. Enter protocol [tcp] [0 or q]: udp\n", "   3. Enter port [514] [0 or q]: ", "   -  Server               192.0.2.50:514 UDP\n"}},
		{"an explicit port kept", two, []string{"2", "udp", "y"},
			"# site\nsyslogServers:\n  - server: 192.0.2.50 # first\n  - server: logs.example.net # second\n    port: 1514\n    protocol: udp\nsshPort: 2222\n", 1,
			[]string{"   -  Server               logs.example.net:1514 UDP\n"}},
		{"to tls, the CA file", two, []string{"2", "tls", "/etc/ssl/ca.pem", "y"},
			"# site\nsyslogServers:\n  - server: 192.0.2.50 # first\n  - server: logs.example.net # second\n    port: 1514\n    protocol: tls\n    caFile: /etc/ssl/ca.pem\nsshPort: 2222\n", 1,
			[]string{"   3. Enter CA file [0 or q]: /etc/ssl/ca.pem", "   -  Server               logs.example.net:1514 TLS\n"}},
		{"udp back to tcp", "syslogServers:\n  - server: 192.0.2.50\n    protocol: udp\n", []string{"1", "tcp", "", "y"},
			"syslogServers:\n  - server: 192.0.2.50\n", 1, []string{"   2. Enter protocol [udp] [0 or q]: tcp\n"}},
		{"a default port to tls, the port then the CA file",
			"syslogServers:\n  - server: a.example.net\n  # backup site\n  - server: logs.example.net # second\n    port: 514 # p\n",
			[]string{"2", "tls", "", "/etc/ssl/ca.pem", "y"},
			"syslogServers:\n  - server: a.example.net\n  # backup site\n  - server: logs.example.net # second\n    protocol: tls\n    caFile: /etc/ssl/ca.pem\n", 1,
			[]string{"   3. Enter port [6514] [0 or q]: \n", "   4. Enter CA file [0 or q]: /etc/ssl/ca.pem", "   -  Server               logs.example.net:6514 TLS\n"}},
		{"tls to udp, the CA file cleared, the port back to 514",
			"syslogServers:\n  - server: a.example.net\n  # backup site\n  - server: logs.example.net # second\n    protocol: tls # t\n    caFile: /etc/ssl/ca.pem\n",
			[]string{"2", "udp", "", "y"},
			"syslogServers:\n  - server: a.example.net\n  # backup site\n  - server: logs.example.net # second\n    protocol: udp # t\n", 1,
			[]string{"   3. Enter port [514] [0 or q]: \n", "   -  Server               logs.example.net:514 UDP\n"}},
		{"the same protocol", two, []string{"1", ""}, two, 0, []string{"  Nothing needs changing.\n"}},
		{"refused, then given", two, []string{"1", "relp", "tls", "UDP", "udp", "", "y"},
			"# site\nsyslogServers:\n  - server: 192.0.2.50 # first\n    protocol: udp\n  - server: logs.example.net # second\n    port: 1514\nsshPort: 2222\n", 1,
			[]string{"  (x) Not udp, tcp or tls\n", "  (x) TLS needs the server's name, as its certificate has it\n"}},
	} {
		h, cfg := site(t, tt.yml)
		p := edit(h, cfg, nil, append(append([]string{"1", "7", "3"}, tt.lines...), "q")...)
		if !errors.Is(p.err, nav.ErrQuit) || p.unread != 0 {
			t.Errorf("%s: Run = %v, %d unread", tt.name, p.err, p.unread)
		}
		if got := file(t, h); got != tt.file {
			t.Errorf("%s: the file is\n%s\nwant\n%s\n%s", tt.name, got, tt.file, p.out)
		}
		if n := strings.Count(p.out, done); n != tt.saves {
			t.Errorf("%s: %d saves, want %d:\n%s", tt.name, n, tt.saves, p.out)
		}
		has(t, tt.name, p.out, tt.want...)
	}

	// Under -n the change is planned and not written.
	h, cfg := site(t, two)
	cfg.ResolveDryRun(true, true)
	p := edit(h, cfg, func(o *configscreen.Options) { o.DryRun = true }, "1", "7", "3", "1", "udp", "", "q")
	if got := file(t, h); got != two || strings.Contains(p.out, "[y/N") {
		t.Errorf("-n: file\n%s\n%s", got, p.out)
	}
	has(t, "-n", p.out, "      +    protocol: udp\n")

	// No Change protocol with no server.
	h, cfg = site(t, "username: george\n")
	if p := edit(h, cfg, nil, "1", "7", "q"); strings.Contains(p.out, "Change protocol") {
		t.Errorf("Change protocol with no server:\n%s", p.out)
	}
}
