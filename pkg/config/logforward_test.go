package config

import (
	"errors"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestLogForwardingResolve(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   LogForwarding
		want LogServer
		ok   bool
		bad  string // a word of the error
	}{
		{name: "none", in: LogForwarding{}},
		{name: "no server, other keys", in: LogForwarding{Port: 514, Protocol: "udp"}},
		{name: "defaults", in: LogForwarding{Server: "192.0.2.50"}, want: LogServer{"192.0.2.50", 514, LogTCP, ""}, ok: true},
		{name: "udp", in: LogForwarding{Server: "logs.example.net", Port: 1514, Protocol: "udp"}, want: LogServer{"logs.example.net", 1514, LogUDP, ""}, ok: true},
		{name: "name lowered", in: LogForwarding{Server: "Logs.Example.NET"}, want: LogServer{"logs.example.net", 514, LogTCP, ""}, ok: true},
		{name: "tls", in: LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/lab-ca.pem"},
			want: LogServer{"logs.example.net", 6514, LogTLS, "/etc/ssl/lab-ca.pem"}, ok: true},
		{name: "tls port", in: LogForwarding{Server: "logs.example.net", Port: 10514, Protocol: "tls", CAFile: "/etc/ssl/lab-ca.pem"},
			want: LogServer{"logs.example.net", 10514, LogTLS, "/etc/ssl/lab-ca.pem"}, ok: true},
		{name: "ipv6", in: LogForwarding{Server: "2001:db8::1"}, bad: "IPv6"},
		{name: "quote", in: LogForwarding{Server: `logs" port="1`}, bad: "server"},
		{name: "space", in: LogForwarding{Server: "logs example"}, bad: "server"},
		{name: "newline", in: LogForwarding{Server: "logs\n*.* @evil"}, bad: "server"},
		{name: "option", in: LogForwarding{Server: "-R"}, bad: "server"},
		{name: "shell", in: LogForwarding{Server: "$(reboot)"}, bad: "server"},
		{name: "backtick", in: LogForwarding{Server: "a`b"}, bad: "server"},
		{name: "underscore", in: LogForwarding{Server: "log_host"}, bad: "server"},
		{name: "trailing dot", in: LogForwarding{Server: "logs.example.net."}, bad: "server"},
		{name: "five octets", in: LogForwarding{Server: "192.0.2.50.1"}, bad: "server"},
		{name: "long", in: LogForwarding{Server: strings.Repeat("a.", 127) + "a"}, bad: "server"},
		{name: "protocol case", in: LogForwarding{Server: "192.0.2.50", Protocol: "TCP"}, bad: "protocol"},
		{name: "protocol", in: LogForwarding{Server: "192.0.2.50", Protocol: "relp"}, bad: "protocol"},
		{name: "port low", in: LogForwarding{Server: "192.0.2.50", Port: -1}, bad: "port"},
		{name: "port high", in: LogForwarding{Server: "192.0.2.50", Port: 65536}, bad: "port"},
		{name: "ca without tls", in: LogForwarding{Server: "192.0.2.50", CAFile: "/etc/ssl/lab-ca.pem"}, bad: "caFile"},
		{name: "tls address", in: LogForwarding{Server: "192.0.2.50", Protocol: "tls", CAFile: "/etc/ssl/lab-ca.pem"}, bad: "name"},
		{name: "tls no ca", in: LogForwarding{Server: "logs.example.net", Protocol: "tls"}, bad: "caFile"},
		{name: "ca relative", in: LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: "ca.pem"}, bad: "caFile"},
		{name: "ca quote", in: LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: `/etc/ssl/a".pem`}, bad: "caFile"},
		{name: "ca space", in: LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/a b.pem"}, bad: "caFile"},
		{name: "ca newline", in: LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/a\n.pem"}, bad: "caFile"},
		{name: "ca dotdot", in: LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: "/etc/ssl/../a.pem"}, bad: "caFile"},
		{name: "ca root", in: LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: "/"}, bad: "caFile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := tc.in.Resolve()
			if tc.bad != "" {
				if !errors.Is(err, ErrLogForwarding) || !strings.Contains(err.Error(), tc.bad) || ok {
					t.Fatalf("Resolve() = %+v, %v, %v; want ErrLogForwarding naming %q", got, ok, err, tc.bad)
				}
				return
			}
			if err != nil || ok != tc.ok || got != tc.want {
				t.Fatalf("Resolve() = %+v, %v, %v; want %+v, %v", got, ok, err, tc.want, tc.ok)
			}
		})
	}
}

// The key as hardn.yml writes it, and a save of the defaults writes none.
func TestLogForwardingYAML(t *testing.T) {
	c := DefaultConfig()
	in := "logForwarding:\n  server: \"192.0.2.50\"\n  port: 514\n  protocol: tcp\n  caFile: \"\"\n"
	if err := yaml.Unmarshal([]byte(in), c); err != nil {
		t.Fatal(err)
	}
	if want := (LogForwarding{Server: "192.0.2.50", Port: 514, Protocol: "tcp"}); c.LogForwarding != want {
		t.Fatalf("logForwarding = %+v, want %+v", c.LogForwarding, want)
	}
	out, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "logForwarding") {
		t.Fatalf("the defaults write logForwarding:\n%s", out)
	}
}
