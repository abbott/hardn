package config

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestNTPServersResolve(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   NTPServers
		want []string
		bad  string // a word of the error
	}{
		{name: "none"},
		{name: "empty list", in: NTPServers{}},
		{name: "one", in: NTPServers{"192.0.2.10"}, want: []string{"192.0.2.10"}},
		{name: "order kept, names lowered", in: NTPServers{"Time2.Example.NET", "192.0.2.10"}, want: []string{"time2.example.net", "192.0.2.10"}},
		{name: "four", in: NTPServers{"a.example", "b.example", "c.example", "d.example"}, want: []string{"a.example", "b.example", "c.example", "d.example"}},
		{name: "five", in: NTPServers{"a.example", "b.example", "c.example", "d.example", "e.example"}, bad: "more than 4"},
		{name: "twice", in: NTPServers{"192.0.2.10", "192.0.2.10"}, bad: "server 2 is named twice"},
		{name: "twice by case", in: NTPServers{"ntp.example", "NTP.example"}, bad: "twice"},
		{name: "empty name", in: NTPServers{"192.0.2.10", ""}, bad: "server 2"},
		{name: "ipv6", in: NTPServers{"2001:db8::1"}, bad: "IPv6"},
		{name: "option", in: NTPServers{"192.0.2.10 prefer"}, bad: "server 1"},
		{name: "space", in: NTPServers{" 192.0.2.10"}, bad: "server 1"},
		{name: "newline", in: NTPServers{"ntp.example\npool evil.example"}, bad: "server 1"},
		{name: "flag", in: NTPServers{"-p"}, bad: "server 1"},
		{name: "shell", in: NTPServers{"$(reboot)"}, bad: "server 1"},
		{name: "quote", in: NTPServers{`a"b`}, bad: "server 1"},
		{name: "hash", in: NTPServers{"a#b"}, bad: "server 1"},
		{name: "underscore", in: NTPServers{"ntp_1"}, bad: "server 1"},
		{name: "trailing dot", in: NTPServers{"ntp.example."}, bad: "server 1"},
		{name: "five octets", in: NTPServers{"192.0.2.10.1"}, bad: "server 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.Resolve()
			if tc.bad != "" {
				if !errors.Is(err, ErrNTPServers) || !strings.Contains(err.Error(), tc.bad) || got != nil {
					t.Fatalf("Resolve() = %v, %v; want ErrNTPServers naming %q", got, err, tc.bad)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("Resolve() = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// The key as hardn.yml writes it, and a save of the defaults writes none.
func TestNTPServersYAML(t *testing.T) {
	c := DefaultConfig()
	if err := yaml.Unmarshal([]byte("ntpServers:\n  - \"192.0.2.10\"\n  - \"192.0.2.11\"\n"), c); err != nil {
		t.Fatal(err)
	}
	if want := (NTPServers{"192.0.2.10", "192.0.2.11"}); !slices.Equal(c.NTPServers, want) {
		t.Fatalf("ntpServers = %v, want %v", c.NTPServers, want)
	}
	out, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ntpServers") {
		t.Fatalf("the defaults write ntpServers:\n%s", out)
	}
}
