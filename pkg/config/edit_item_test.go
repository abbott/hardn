package config

import (
	"strings"
	"testing"
)

// TestEditItemInPlace: a list item whose fields alone change is edited in
// place, its comments and every other line kept.
func TestEditItemInPlace(t *testing.T) {
	head := "syslogServers:\n  # primary\n  - server: a.example.net # one\n  # backup site\n"
	for _, tc := range []struct {
		name, in, want string
		set            func(*SyslogServer)
	}{
		{name: "a field added",
			in:   head + "  - server: b.example.net # two\n    port: 1514 # p\n",
			set:  func(s *SyslogServer) { s.Protocol = LogUDP },
			want: head + "  - server: b.example.net # two\n    port: 1514 # p\n    protocol: udp\n"},
		{name: "a field changed, one removed",
			in:   head + "  - server: b.example.net # two\n    protocol: tls # t\n    caFile: /etc/ca.pem # c\n    port: 6514 # p\n",
			set:  func(s *SyslogServer) { s.Protocol, s.CAFile, s.Port = LogUDP, "", 514 },
			want: head + "  - server: b.example.net # two\n    protocol: udp # t\n    port: 514 # p\n"},
		{name: "a field changed, two added",
			in:   head + "  - server: b.example.net # two\n    port: 514 # p\n",
			set:  func(s *SyslogServer) { s.Protocol, s.CAFile, s.Port = LogTLS, "/etc/ca.pem", 6514 },
			want: head + "  - server: b.example.net # two\n    port: 6514 # p\n    protocol: tls\n    caFile: /etc/ca.pem\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := load(t, tc.in)
			c.SyslogServers = append(SyslogServers(nil), c.SyslogServers...)
			tc.set(&c.SyslogServers[1])
			got, err := Edit([]byte(tc.in), c)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestEditItemMoved: an item moved, swapped or replaced is not edited in
// place, so a comment never stays at a place whose item changed.
func TestEditItemMoved(t *testing.T) {
	in := "syslogServers:\n  # primary\n  - server: a.example.net # one\n  # backup site\n  - server: b.example.net # two\n    port: 1514\n  - server: c.example.net # three\n"
	for _, tc := range []struct {
		name    string
		set     func(SyslogServers) SyslogServers
		without []string
	}{
		{"swapped", func(s SyslogServers) SyslogServers { s[0], s[1] = s[1], s[0]; return s },
			[]string{"server: b.example.net # one", "server: a.example.net # two"}},
		{"replaced", func(s SyslogServers) SyslogServers { s[1] = SyslogServer{Server: "d.example.net"}; return s },
			[]string{"server: d.example.net # two"}},
		{"one removed, one added", func(s SyslogServers) SyslogServers {
			return SyslogServers{s[0], s[2], {Server: "d.example.net"}}
		}, []string{"server: c.example.net # two", "server: d.example.net # three"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := load(t, in)
			c.SyslogServers = tc.set(append(SyslogServers(nil), c.SyslogServers...))
			got, err := Edit([]byte(in), c)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.without {
				if strings.Contains(string(got), w) {
					t.Errorf("%q in\n%s", w, got)
				}
			}
		})
	}
}

// TestEditItemMultiLine: a field removed whose value goes on to the next
// line is not removed in place, and the save still works.
func TestEditItemMultiLine(t *testing.T) {
	in := "syslogServers:\n  - server: a.example.net\n  - server: b.example.net\n    caFile: \"/etc/\n      ca.pem\"\n    protocol: tls\n"
	c := load(t, in)
	c.SyslogServers = append(SyslogServers(nil), c.SyslogServers...)
	c.SyslogServers[1].CAFile, c.SyslogServers[1].Protocol = "", LogUDP
	got, err := Edit([]byte(in), c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "ca.pem") || !strings.Contains(string(got), "protocol: udp") {
		t.Errorf("got\n%s", got)
	}
}
