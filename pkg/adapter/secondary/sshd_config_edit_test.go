package secondary

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetRootLoginFixtures edits each testdata/sshd_config/<name>.conf
// and compares it with <name>.want: the global PermitRootLogin set (or
// added), root out of the global AllowUsers, every other line, Include,
// comment, commented-out directive and Match block kept, and Port 2230
// still there.
func TestSetRootLoginFixtures(t *testing.T) {
	for _, name := range []string{"debian13", "ubuntu2404", "alpine323", "match-no-directive"} {
		t.Run(name, func(t *testing.T) {
			in := readFixture(t, name+".conf")
			want := readFixture(t, name+".want")
			got, err := setRootLogin(in, "no", true)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("edited %s.conf:\n%s\nwant %s.want:\n%s", name, got, name, want)
			}
			if !strings.Contains(got, "\nPort 2230\n") {
				t.Errorf("Port 2230 lost:\n%s", got)
			}
			again, err := setRootLogin(got, "no", true)
			if err != nil || again != got {
				t.Errorf("a second edit changed the file (err %v):\n%s", err, again)
			}
		})
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "sshd_config", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSetRootLogin covers the edit's rules one at a time.
func TestSetRootLogin(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		value          string // "" means "no"
		keepRoot       bool
	}{
		{name: "empty file", in: "", want: "PermitRootLogin no\n"},
		{name: "yes", in: "Port 2230\nPermitRootLogin yes\n", want: "Port 2230\nPermitRootLogin no\n"},
		{name: "without-password", in: "PermitRootLogin without-password\n", want: "PermitRootLogin no\n"},
		{name: "prohibit-password", in: "PermitRootLogin prohibit-password\n", want: "PermitRootLogin no\n"},
		{name: "already no", in: "PermitRootLogin no\n", want: "PermitRootLogin no\n"},
		{name: "case and =", in: "PERMITROOTLOGIN = yes\n", want: "PERMITROOTLOGIN = no\n"},
		{name: "quoted", in: "PermitRootLogin \"yes\" \n", want: "PermitRootLogin no \n"},
		{name: "no argument", in: "PermitRootLogin\n", want: "PermitRootLogin no\n"},
		{name: "every global occurrence", in: "PermitRootLogin yes\nPermitRootLogin no\n", want: "PermitRootLogin no\nPermitRootLogin no\n"},
		{name: "commented only", in: "#PermitRootLogin yes\n", want: "#PermitRootLogin yes\nPermitRootLogin no\n"},
		{name: "no final newline", in: "Port 2230", want: "Port 2230\nPermitRootLogin no\n"},
		{name: "CRLF kept", in: "Port 2230\r\nPermitRootLogin yes\r\n", want: "Port 2230\r\nPermitRootLogin no\r\n"},
		{name: "inserted before the first Match", in: "Port 2230\nMatch User bob\n\tPermitRootLogin yes\nMatch all\n", want: "Port 2230\nPermitRootLogin no\nMatch User bob\n\tPermitRootLogin yes\nMatch all\n"},
		{name: "lowercase match", in: "match user bob\n", want: "PermitRootLogin no\nmatch user bob\n"},
		{name: "Include kept, not followed", in: "Include /etc/ssh/sshd_config.d/*.conf\n", want: "Include /etc/ssh/sshd_config.d/*.conf\nPermitRootLogin no\n"},
		{name: "root out of AllowUsers", in: "AllowUsers alice root bob\n", want: "AllowUsers alice bob\nPermitRootLogin no\n"},
		{name: "AllowUsers comment kept", in: "AllowUsers root alice # admins\n", want: "AllowUsers alice # admins\nPermitRootLogin no\n"},
		{name: "root@host is not root", in: "AllowUsers root@192.0.2.1 alice\n", want: "AllowUsers root@192.0.2.1 alice\nPermitRootLogin no\n"},
		{name: "a root-only line goes when another keeps a user", in: "AllowUsers root\nAllowUsers alice\n", want: "AllowUsers alice\nPermitRootLogin no\n"},
		{name: "AllowUsers in Match untouched", in: "AllowUsers alice\nMatch Address 192.0.2.0/24\n\tAllowUsers root\n", want: "AllowUsers alice\nPermitRootLogin no\nMatch Address 192.0.2.0/24\n\tAllowUsers root\n"},
		{name: "prohibit-password keeps root in AllowUsers", in: "PermitRootLogin yes\nAllowUsers root alice\n", want: "PermitRootLogin prohibit-password\nAllowUsers root alice\n", value: "prohibit-password", keepRoot: true},
		{name: "prohibit-password with AllowUsers root alone", in: "AllowUsers root\n", want: "AllowUsers root\nPermitRootLogin prohibit-password\n", value: "prohibit-password", keepRoot: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.value
			if value == "" {
				value = "no"
			}
			got, err := setRootLogin(tc.in, value, !tc.keepRoot)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestSetRootLoginRefusesNoUserLeft checks that an AllowUsers allowing only
// root is refused rather than emptied, and nothing changes.
func TestSetRootLoginRefusesNoUserLeft(t *testing.T) {
	for _, in := range []string{
		"PermitRootLogin yes\nAllowUsers root\n",
		"AllowUsers root\nAllowUsers \"root\"\n",
		"AllowUsers root\nMatch User alice\n\tAllowUsers alice\n",
	} {
		got, err := setRootLogin(in, "no", true)
		if !errors.Is(err, errNoUserLeft) || got != in {
			t.Errorf("%q: got %q, %v; want it unchanged and errNoUserLeft", in, got, err)
		}
	}
}
