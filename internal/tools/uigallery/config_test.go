package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Configuration screen's mocks (config.go; unit (a), next/config-gallery).
// Nothing here reads this machine's hardn.yml or its environment.

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
	m := configMock{name: "probe", path: "/etc/hardn/hardn.yml", by: screen.ConfigBySearch,
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
