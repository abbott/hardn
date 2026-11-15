package config

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestSyslogServersResolve(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   SyslogServer
		want LogServer
		bad  string // a word of the error
	}{
		{name: "no server", in: SyslogServer{}, bad: "server 1: no server"},
		{name: "no server, other keys", in: SyslogServer{Port: 514, Protocol: "udp"}, bad: "server 1: no server"},
		{name: "defaults", in: SyslogServer{Server: "192.0.2.50"}, want: LogServer{"192.0.2.50", 514, LogTCP, ""}},
		{name: "udp", in: SyslogServer{Server: "logs.example.net", Port: 1514, Protocol: "udp"}, want: LogServer{"logs.example.net", 1514, LogUDP, ""}},
		{name: "name lowered", in: SyslogServer{Server: "Logs.Example.NET"}, want: LogServer{"logs.example.net", 514, LogTCP, ""}},
		{name: "tls", in: SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/lab-ca.pem"},
			want: LogServer{"logs.example.net", 6514, LogTLS, "/etc/ssl/lab-ca.pem"}},
		{name: "tls port", in: SyslogServer{Server: "logs.example.net", Port: 10514, Protocol: "tls", CAFile: "/etc/ssl/lab-ca.pem"},
			want: LogServer{"logs.example.net", 10514, LogTLS, "/etc/ssl/lab-ca.pem"}},
		{name: "ipv6", in: SyslogServer{Server: "2001:db8::1"}, bad: "IPv6"},
		{name: "quote", in: SyslogServer{Server: `logs" port="1`}, bad: "server"},
		{name: "space", in: SyslogServer{Server: "logs example"}, bad: "server"},
		{name: "newline", in: SyslogServer{Server: "logs\n*.* @evil"}, bad: "server"},
		{name: "option", in: SyslogServer{Server: "-R"}, bad: "server"},
		{name: "shell", in: SyslogServer{Server: "$(reboot)"}, bad: "server"},
		{name: "backtick", in: SyslogServer{Server: "a`b"}, bad: "server"},
		{name: "underscore", in: SyslogServer{Server: "log_host"}, bad: "server"},
		{name: "trailing dot", in: SyslogServer{Server: "logs.example.net."}, bad: "server"},
		{name: "five octets", in: SyslogServer{Server: "192.0.2.50.1"}, bad: "server"},
		{name: "long", in: SyslogServer{Server: strings.Repeat("a.", 127) + "a"}, bad: "server"},
		{name: "protocol case", in: SyslogServer{Server: "192.0.2.50", Protocol: "TCP"}, bad: "protocol"},
		{name: "protocol", in: SyslogServer{Server: "192.0.2.50", Protocol: "relp"}, bad: "protocol"},
		{name: "port low", in: SyslogServer{Server: "192.0.2.50", Port: -1}, bad: "port"},
		{name: "port high", in: SyslogServer{Server: "192.0.2.50", Port: 65536}, bad: "port"},
		{name: "ca without tls", in: SyslogServer{Server: "192.0.2.50", CAFile: "/etc/ssl/lab-ca.pem"}, bad: "caFile"},
		{name: "tls address", in: SyslogServer{Server: "192.0.2.50", Protocol: "tls", CAFile: "/etc/ssl/lab-ca.pem"}, bad: "name"},
		{name: "tls no ca", in: SyslogServer{Server: "logs.example.net", Protocol: "tls"}, bad: "caFile"},
		{name: "ca relative", in: SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: "ca.pem"}, bad: "caFile"},
		{name: "ca quote", in: SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: `/etc/ssl/a".pem`}, bad: "caFile"},
		{name: "ca space", in: SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/a b.pem"}, bad: "caFile"},
		{name: "ca newline", in: SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/a\n.pem"}, bad: "caFile"},
		{name: "ca dotdot", in: SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/../a.pem"}, bad: "caFile"},
		{name: "ca root", in: SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: "/"}, bad: "caFile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SyslogServers{tc.in}.Resolve()
			if tc.bad != "" {
				if !errors.Is(err, ErrSyslogServers) || !strings.Contains(err.Error(), tc.bad) || got != nil {
					t.Fatalf("Resolve() = %+v, %v; want ErrSyslogServers naming %q", got, err, tc.bad)
				}
				return
			}
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("Resolve() = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

// TestSyslogServersList: the list's own rules (design-syslog-servers.md
// §2.1): none or empty sets nothing; one to four, protocols mixed, in the
// file's order; and a list with one bad entry is refused whole, saying
// which.
func TestSyslogServersList(t *testing.T) {
	tls := SyslogServer{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/example-ca.pem"}
	a, b := SyslogServer{Server: "192.0.2.51"}, SyslogServer{Server: "192.0.2.52", Protocol: "udp"}
	for _, in := range []SyslogServers{nil, {}} {
		if got, err := in.Resolve(); got != nil || err != nil {
			t.Errorf("%#v.Resolve() = %v, %v; want nothing", in, got, err)
		}
	}
	got, err := SyslogServers{tls, a, b}.Resolve()
	want := []LogServer{{"logs.example.net", 6514, LogTLS, "/etc/ssl/example-ca.pem"}, {"192.0.2.51", 514, LogTCP, ""}, {"192.0.2.52", 514, LogUDP, ""}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Resolve() = %+v, %v; want %+v", got, err, want)
	}
	four := SyslogServers{a, b, {Server: "192.0.2.53"}, {Server: "192.0.2.54"}}
	if got, err := four.Resolve(); err != nil || len(got) != 4 {
		t.Errorf("four servers: %v, %v", got, err)
	}
	for name, tc := range map[string]struct {
		in  SyslogServers
		bad string
	}{
		"five":                   {append(slices.Clone(four), SyslogServer{Server: "192.0.2.55"}), "more than 4 servers"},
		"second not valid":       {SyslogServers{a, {Server: "192.0.2.52", Port: 70000}}, "server 2: port is not between 1 and 65535"},
		"block with no server":   {SyslogServers{a, {Protocol: "udp"}}, "server 2: no server"},
		"twice":                  {SyslogServers{a, b, a}, "server 3: the same address and port as server 1"},
		"twice, other protocol":  {SyslogServers{a, {Server: "192.0.2.51", Protocol: "udp"}}, "server 2: the same address and port as server 1"},
		"twice, name's case":     {SyslogServers{{Server: "logs.example.net"}, {Server: "LOGS.example.net", Port: 514}}, "server 2: the same address and port"},
		"third's CA file is bad": {SyslogServers{a, b, {Server: "logs.example.net", Protocol: "tls"}}, "server 3: tls needs caFile"},
	} {
		got, err := tc.in.Resolve()
		if !errors.Is(err, ErrSyslogServers) || !strings.Contains(err.Error(), tc.bad) || got != nil {
			t.Errorf("%s: Resolve() = %v, %v; want ErrSyslogServers saying %q", name, got, err, tc.bad)
		}
	}
	// The same address on another port is another server.
	if got, err := (SyslogServers{a, {Server: "192.0.2.51", Port: 1514}}).Resolve(); err != nil || len(got) != 2 {
		t.Errorf("one address, two ports: %v, %v", got, err)
	}
}

// The key as hardn.yml writes it; a save of the defaults writes none; and
// logForwarding, the key before it, is a key nothing reads.
func TestSyslogServersYAML(t *testing.T) {
	c := DefaultConfig()
	in := "syslogServers:\n  - server: \"192.0.2.50\"\n    port: 514\n    protocol: tcp\n  - server: logs.example.net\n    protocol: tls\n    caFile: /etc/ssl/lab-ca.pem\n"
	if err := yaml.Unmarshal([]byte(in), c); err != nil {
		t.Fatal(err)
	}
	want := SyslogServers{{Server: "192.0.2.50", Port: 514, Protocol: "tcp"}, {Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/lab-ca.pem"}}
	if !slices.Equal(c.SyslogServers, want) {
		t.Fatalf("syslogServers = %+v, want %+v", c.SyslogServers, want)
	}
	out, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "syslogServers") {
		t.Fatalf("the defaults write syslogServers:\n%s", out)
	}
	old := DefaultConfig()
	if err := yaml.Unmarshal([]byte("logForwarding:\n  server: \"192.0.2.50\"\n"), old); err != nil {
		t.Fatal(err)
	}
	if len(old.SyslogServers) != 0 || len(old.Retired()) != 0 {
		t.Fatalf("logForwarding is read: %+v, retired %v", old.SyslogServers, old.Retired())
	}
}
