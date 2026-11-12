package config

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestFileSets: a key is the file's where the file gives it a value. A key
// left out or null keeps the default and is not set; an empty string and an
// empty list are values; disableRoot sets disableRootSSH, unless the file
// has disableRootSSH too, null included, which shadows it; the defaults,
// read from no file, set nothing.
func TestFileSets(t *testing.T) {
	if DefaultConfig().FileSets("sshPort") {
		t.Error("the defaults set sshPort")
	}
	for _, tt := range []struct {
		yml  string
		want map[string]bool
	}{
		{"username: alice\nsshPort:\nnameservers: ~\nconfirmTimeout: \"\"\nsshKeys: []\n",
			map[string]bool{"username": true, "sshPort": false, "nameservers": false, "confirmTimeout": true, "sshKeys": true, "backupPath": false}},
		{"disableRoot: true\n", map[string]bool{"disableRootSSH": true, "disableRoot": true}},
		{"disableRoot:\n", map[string]bool{"disableRootSSH": false}},
		{"disableRoot: true\ndisableRootSSH: false\n", map[string]bool{"disableRootSSH": true}},
		{"disableRoot: true\ndisableRootSSH:\n", map[string]bool{"disableRootSSH": false}},
		{"base: &b [192.0.2.53]\nnameservers: *b\n", map[string]bool{"nameservers": true}},
		{"", map[string]bool{"username": false}},
	} {
		c := load(t, tt.yml)
		for k, want := range tt.want {
			if got := c.FileSets(k); got != want {
				t.Errorf("%q: FileSets(%s) = %t, want %t", tt.yml, k, got, want)
			}
		}
	}
}

// TestFileWord: a key's value as a word where it is one string, bool or
// whole number, as yaml.v3 reads it; a list and a mapping on one line, in
// flow form; nothing for a null and a key the file leaves out.
func TestFileWord(t *testing.T) {
	c := load(t, "username: alice\nenableLynis: true\nsshPort: 0x16\nsshListenAddress: 192.0.2.10\n"+
		"sshAllowedUsers: [bob]\nlogsconfig:\n  logfilepath: /srv/log\ntz:\n")
	for k, want := range map[string]string{"username": "alice", "enableLynis": "true", "sshPort": "22", "sshListenAddress": "192.0.2.10",
		"sshAllowedUsers": `["bob"]`, "logsconfig": `{"logfilepath":"/srv/log"}`, "tz": "", "lang": ""} {
		if got := c.FileWord(k); got != want {
			t.Errorf("FileWord(%s) = %q, want %q", k, got, want)
		}
	}
}

// TestFileDryRun: dryRun as the file gave it, whatever mode the session
// runs in.
func TestFileDryRun(t *testing.T) {
	c := load(t, "username: alice\n")
	c.ResolveDryRun(true, true) // -n
	if !c.DryRun || c.FileDryRun() || c.FileSets("dryRun") {
		t.Errorf("-n: DryRun %t, FileDryRun %t, FileSets %t", c.DryRun, c.FileDryRun(), c.FileSets("dryRun"))
	}
	if c := load(t, "dryRun: true\n"); !c.FileDryRun() || !c.FileSets("dryRun") {
		t.Error("dryRun: true is not the file's")
	}
}

// TestFileSetsAfterSave: once Save has written a key the file left out,
// the file sets it.
func TestFileSetsAfterSave(t *testing.T) {
	ctx := context.Background()
	const path = "/etc/hardn/hardn.yml"
	sys := hostfake.New(host.Options{})
	sys.AddFile(path, []byte("username: alice\n"), 0o600, 0, 0)
	c, err := Read(ctx, sys, path)
	if err != nil {
		t.Fatal(err)
	}
	if c.FileSets("nameservers") || !c.FileSets("username") {
		t.Fatalf("as read: nameservers %t, username %t", c.FileSets("nameservers"), c.FileSets("username"))
	}
	c.Nameservers = []string{"192.0.2.53"}
	if err := Save(ctx, sys, c); err != nil {
		t.Fatal(err)
	}
	if !c.FileSets("nameservers") || !c.FileSets("username") {
		t.Errorf("as saved: nameservers %t, username %t", c.FileSets("nameservers"), c.FileSets("username"))
	}
}

// TestFileSetsFirstRun: the file the first run writes holds every key it
// writes, as setOf reads the bytes LoadConfig records for it.
func TestFileSetsFirstRun(t *testing.T) {
	name := filepath.Join(t.TempDir(), "hardn.yml")
	if err := SaveConfig(DefaultConfig(), name); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	set := setOf(data)
	for _, k := range []string{"username", "sshPort", "sshKeys", "enableBackups"} {
		if _, ok := set[k]; !ok {
			t.Errorf("the first run's file does not set %s:\n%s", k, data)
		}
	}
	if setOf([]byte("- a\n")) != nil || setOf([]byte(":\n\t")) != nil {
		t.Error("setOf of no mapping")
	}
}

// TestFileUsername: username as the loader read it, a string whatever else
// its scalar reads as, where FileWord's is yaml.v3's own reading; the
// session's -u does not change it, and no file gives none.
func TestFileUsername(t *testing.T) {
	for _, name := range []string{"0755", "0x10", "1.5", "2026-01-01", "true", "alice"} {
		c := load(t, "username: "+name+"\n")
		c.Username = "bob" // -u bob
		if got := c.FileUsername(); got != name {
			t.Errorf("username: %s: FileUsername = %q", name, got)
		}
	}
	if got := load(t, "username: 0755\n").FileWord("username"); got != "493" {
		t.Errorf("FileWord(username) of 0755 = %q, want yaml.v3's 493", got)
	}
	if got := load(t, "sshPort: 2222\n").FileUsername() + DefaultConfig().FileUsername(); got != "" {
		t.Errorf("FileUsername with none = %q", got)
	}
}

// TestFileSetsMerged: the keys a top-level merge brings count as the file's,
// as the decoder loads them: from a mapping, an alias to one, or a sequence
// of them, the first of which wins; a key the file has itself wins, a null
// too, wherever the merge is; a merged mapping's own merge is read; a key
// merged as null is not set.
func TestFileSetsMerged(t *testing.T) {
	for _, tt := range []struct {
		yml      string
		port     int
		username string
		set      map[string]bool
	}{
		{"base: &b {sshPort: 2222, username: al}\n<<: *b\n", 2222, "al", map[string]bool{"sshPort": true, "username": true, "backupPath": false, "<<": false}},
		{"<<: {sshPort: 2222, username: al}\n", 2222, "al", map[string]bool{"sshPort": true, "username": true}},
		{"a: &a {sshPort: 2222}\nb: &b {sshPort: 2230, username: al}\n<<: [*a, *b]\n", 2222, "al", map[string]bool{"sshPort": true, "username": true}},
		{"<<: [{username: al}, {username: bo, sshPort: 2230}]\n", 2230, "al", map[string]bool{"sshPort": true, "username": true}},
		{"b: &b {sshPort: 2222, username: al}\n<<: *b\nsshPort: 2230\n", 2230, "al", map[string]bool{"sshPort": true, "username": true}},
		{"b: &b {sshPort: 2222, username: al}\nsshPort:\n<<: *b\n", 22, "al", map[string]bool{"sshPort": false, "username": true}},
		{"a: &a {username: al}\nb: &b {<<: *a, sshPort: 2222}\n<<: *b\n", 2222, "al", map[string]bool{"sshPort": true, "username": true}},
		{"b: &b {sshPort: ~, username: al}\n<<: *b\n", 22, "al", map[string]bool{"sshPort": false, "username": true}},
		{"\"<<\": {sshPort: 2222}\n", 22, "", map[string]bool{"sshPort": false}},
	} {
		c := load(t, tt.yml)
		if c.SshPort != tt.port || c.Username != tt.username {
			t.Errorf("%q: loaded sshPort %d, username %q; want %d, %q", tt.yml, c.SshPort, c.Username, tt.port, tt.username)
		}
		for k, want := range tt.set {
			if got := c.FileSets(k); got != want {
				t.Errorf("%q: FileSets(%s) = %t, want %t", tt.yml, k, got, want)
			}
		}
		if c.FileSets("sshPort") && c.FileWord("sshPort") != strconv.Itoa(tt.port) {
			t.Errorf("%q: FileWord(sshPort) = %q, want %d", tt.yml, c.FileWord("sshPort"), tt.port)
		}
		if got := c.FileUsername(); got != tt.username {
			t.Errorf("%q: FileUsername = %q, want %q", tt.yml, got, tt.username)
		}
	}
}

// TestOnSearchPath: the system-wide file is on the search path; a file -f
// would name is not.
func TestOnSearchPath(t *testing.T) {
	if !OnSearchPath("/etc/hardn/hardn.yml") || OnSearchPath("/root/site.yml") || OnSearchPath("") {
		t.Errorf("OnSearchPath: /etc/hardn/hardn.yml %t, /root/site.yml %t, none %t",
			OnSearchPath("/etc/hardn/hardn.yml"), OnSearchPath("/root/site.yml"), OnSearchPath(""))
	}
}
