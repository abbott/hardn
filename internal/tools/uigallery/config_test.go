package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Configuration screen's mocks (config.go), listed and played on hardn's screen
// (pkg/screens/config, whose own tests hold the mapping). Nothing here reads this
// machine's hardn.yml or its environment.

// TestConfigGolden pins each mock on the core set and under LANG=C.
func TestConfigGolden(t *testing.T) { goldenListing(t, configListing) }

// TestConfigMocks holds each mock to what its note says: which keys the file sets,
// every other key a default, and the keys nothing reads.
func TestConfigMocks(t *testing.T) {
	typical := []string{"username", "sshKeys", "sshPort", "disableRootSSH", "enableUfwSshPolicy", "ufwAppProfiles", "configureDns", "nameservers", "enableUnattendedUpgrades"}
	tests := []struct {
		mock   string
		set    []string
		unused []string
	}{
		{mock: "etc", set: typical},
		{mock: "flag", set: typical},
		{mock: "env", set: typical},
		{mock: "none"},
		{mock: "minimal", set: []string{"username", "nameservers"}},
		{mock: "unused", set: typical, unused: []string{"logFile /srv/log/hardn.log", "permitRootLogin yes", "sshListenAddress 192.0.2.10", "enableLynis true", "sudoNoPassword false"}},
		{mock: "missing"},
	}
	mocks := configMocks()
	if len(tests) != len(mocks) {
		t.Fatalf("%d mocks tested, %d mocks", len(tests), len(mocks))
	}
	for i, tt := range tests {
		m := mocks[i]
		v, err := m.view(context.Background())
		if err != nil || m.name != tt.mock {
			t.Fatalf("%s: mock %s, %v", tt.mock, m.name, err)
		}
		var set, unused []string
		for _, k := range screen.ConfigKeys() {
			val, ok := v.Values[k]
			if !ok {
				t.Errorf("%s: no value for %s", tt.mock, k)
			}
			if val.Set {
				set = append(set, k)
			}
		}
		for _, u := range v.Unused {
			unused = append(unused, u.Key+" "+u.Value)
		}
		slices.Sort(set)
		slices.Sort(tt.set)
		if !slices.Equal(set, tt.set) || !slices.Equal(unused, tt.unused) {
			t.Errorf("%s: set %v, unused %v", tt.mock, set, unused)
		}
		if len(v.Values) != len(screen.ConfigKeys()) {
			t.Errorf("%s: %d values, %d keys", tt.mock, len(v.Values), len(screen.ConfigKeys()))
		}
	}
}

// TestConfigShowsNoKey: sshKeys is a count, never the key's text; and every note
// opens with the source label.
func TestConfigShowsNoKey(t *testing.T) {
	v, err := configMocks()[0].view(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Values["sshKeys"].Text; got != "1 key" {
		t.Errorf("sshKeys: %q", got)
	}
	if got := v.Values["nameservers"].Text; got != "2: 1.1.1.1, 1.0.0.1" {
		t.Errorf("nameservers: %q", got)
	}
	if got := v.Values["confirmTimeout"]; got.Text != "3m" || got.Set {
		t.Errorf("confirmTimeout: %+v", got)
	}
	for _, m := range configListing.mocks() {
		if note := configListing.note(m); !strings.HasPrefix(note, configLabel) {
			t.Errorf("%s: %q", m.name, note)
		}
	}
}

// TestConfigViewReadsAsTheLoader: a value is the file's only where the file
// gives it one. A null keeps the default, as the loader reads it, and so does
// an empty confirmTimeout; disableRoot, disableRootSSH's old name, sets it. A
// control character in a value never reaches the screen.
func TestConfigViewReadsAsTheLoader(t *testing.T) {
	m := configMock{name: "probe", path: "/etc/hardn/hardn.yml", by: config.FoundSearch,
		yml: "username: \"a\\e[2Jb\"\nsshPort:\nnameservers: ~\nconfirmTimeout: \"\"\ndisableRoot: true\n"}
	v, err := m.view(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for k, set := range map[string]bool{"username": true, "sshPort": false, "nameservers": false, "confirmTimeout": false, "disableRootSSH": true} {
		if v.Values[k].Set != set {
			t.Errorf("%s: set %v, want %v (%q)", k, v.Values[k].Set, set, v.Values[k].Text)
		}
	}
	if u := v.Values["username"].Text; strings.ContainsRune(u, 0x1b) {
		t.Errorf("username drawn raw: %q", u)
	}
	if got := v.Values["disableRootSSH"].Text; got != "true" {
		t.Errorf("disableRootSSH %q", got)
	}
}

// TestPlayConfigGolden plays hardn's Configuration screen end to end and pins each
// transcript: from the main menu's 8, on the typical file, leaving; the same under
// plan only, which draws the same screen; the keys nothing reads; a named file that
// is not there; and the mocks' menu.
func TestPlayConfigGolden(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		lines []string
	}{
		{"config", nil, []string{"8", "0", "0"}},
		{"config-plan-only", []string{"-sequence", "dry-run"}, []string{"8", "0", "0"}},
		{"config-unused", []string{"config/unused"}, []string{"q"}},
		{"config-missing", []string{"config/missing"}, []string{"1", "0"}},
		{"config-mocks", []string{"config"}, []string{"4", "0", "0"}},
	} {
		uitest.Golden(t, "play/"+tt.name, uitest.Core(), func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return out
		})
	}
}

// TestPlayConfigIsTheListing: play draws each mock as the listing does, both by the
// real screen's mapping: the listing's rows are in play's transcript.
func TestPlayConfigIsTheListing(t *testing.T) {
	for _, m := range configMocks() {
		code, out, errs, unread := playGallery([]string{"0"}, "-profile", "plain", configPrefix+"/"+m.name)
		if code != exitOK || errs != "" || unread != 0 {
			t.Fatalf("%s: exit %d, stderr %q, %d lines unread", m.name, code, errs, unread)
		}
		v, err := m.view(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		drawn := v.SubMenu().Render(component.NewCtx(plainOff()))
		if rows, _, _ := strings.Cut(ansi.Strip(drawn), "\n   0) Return"); !strings.Contains(ansi.Strip(out), rows) {
			t.Errorf("%s: play does not hold the listing's screen:\n%s\nplay:\n%s", m.name, rows, out)
		}
	}
}

// TestConfigUsage: play refuses what the Configuration screen does not take, with
// status 2 and nothing on stdout.
func TestConfigUsage(t *testing.T) {
	for _, args := range [][]string{
		{"play", "config/nope"},
		{"play", "-fail", "1", "config/etc"},
		{"play", "-sequence", "failure", "config/etc"},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(args, func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("%v: exit %d, stdout %q, stderr %q, stdin read %t", args, code, out.String(), errs.String(), read)
		}
	}
}
