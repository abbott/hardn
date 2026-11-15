package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestCreateDefaultConfigServers: the first-run NTP and syslog questions
// (docs/engine/design-config-edit.md, decisions 9 to 12). Enter at both
// writes neither key; a refused answer gets one line and the question
// again; the end of the input ends the series; host:port gives that port;
// the closing line names only what was given; the release question is
// still the last.
func TestCreateDefaultConfigServers(t *testing.T) {
	const (
		ntpQ    = "Enter NTP server (optional, press Enter to skip): "
		syslogQ = "Enter syslog server (optional, press Enter to skip): "
		head    = "\n\n\n\n" // username, port, updates, key
	)
	prevInteractive, prevExample, prevDefault := isInteractive, ensureExample, CheckReleasesDefault
	isInteractive = func() bool { return true }
	ensureExample = func() error { return nil }
	CheckReleasesDefault = nil
	t.Cleanup(func() { isInteractive, ensureExample, CheckReleasesDefault = prevInteractive, prevExample, prevDefault })
	for _, tc := range []struct {
		name, answers string
		ntp           []string
		syslog        []SyslogServer
		ntpAsked      int
		syslogAsked   int
		refusals      []string
		closing       string
	}{
		{"Enter at both", "\n\n\n", nil, nil, 1, 1, nil, ""},
		{"both given", "time.example.net\n192.0.2.20:1514\n\n", []string{"time.example.net"},
			[]SyslogServer{{Server: "192.0.2.20", Port: 1514}}, 1, 1, nil, "Run All sets the NTP and syslog servers."},
		{"NTP alone", " 192.0.2.10 \n\n\n", []string{"192.0.2.10"}, nil, 1, 1, nil, "Run All sets the NTP server."},
		{"syslog alone, no port", "\nlogs.example.net\n\n", nil, []SyslogServer{{Server: "logs.example.net"}}, 1, 1, nil,
			"Run All sets the syslog server."},
		{"a name and a port", "\nlogs.example.net:6000\n\n", nil, []SyslogServer{{Server: "logs.example.net", Port: 6000}}, 1, 1, nil,
			"Run All sets the syslog server."},
		{"NTP refused, then given", "2001:db8::1\nnot a host\ntime.example.net\n\n\n", []string{"time.example.net"}, nil, 3, 1,
			[]string{refusedIPv6, refusedHost}, "Run All sets the NTP server."},
		{"NTP refused, then skipped", "2001:db8::1\n\n\n\n", nil, nil, 2, 1, []string{refusedIPv6}, ""},
		{"syslog refused, then given", "\n2001:db8::1\n[2001:db8::1]:514\nlogs.example.net:0\nlogs.example.net:99999\nlogs.example.net:tcp\n:514\n192.0.2.20\n\n",
			nil, []SyslogServer{{Server: "192.0.2.20"}}, 1, 7,
			[]string{refusedIPv6, refusedHost, refusedPort, refusedPort, refusedPort, refusedHost}, "Run All sets the syslog server."},
		{"the end of the input at NTP", "", nil, nil, 1, 1, nil, ""},
		{"the end of the input after a refused NTP server", "2001:db8::1\n", nil, nil, 2, 1, []string{refusedIPv6}, ""},
		{"the end of the input after a refused last line", "\n2001:db8::1", nil, nil, 1, 2, []string{refusedIPv6}, ""},
		{"the end of the input after a syslog server", "\n192.0.2.20", nil, []SyslogServer{{Server: "192.0.2.20"}}, 1, 1, nil,
			"Run All sets the syslog server."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useReader(t, prompt.New(strings.NewReader(head+tc.answers), true))
			path := filepath.Join(t.TempDir(), "hardn.yml")
			var err error
			out, _, ended := ask(t, func() { err = CreateDefaultConfig(path, DefaultConfig()) })
			if err != nil || ended {
				t.Fatalf("CreateDefaultConfig: %v (ended %v)", err, ended)
			}
			if got := strings.Count(out, ntpQ); got != tc.ntpAsked {
				t.Errorf("the NTP question asked %d times, want %d:\n%s", got, tc.ntpAsked, out)
			}
			if got := strings.Count(out, syslogQ); got != tc.syslogAsked {
				t.Errorf("the syslog question asked %d times, want %d:\n%s", got, tc.syslogAsked, out)
			}
			// The order: the key, NTP, syslog, the release question, then the lines.
			at := 0
			for _, want := range []string{"Add SSH public key (optional, press Enter to skip): " + ntpQ, syslogQ,
				ReleaseQuestion(false) + " Created configuration file at " + path + "\n", "A complete example configuration"} {
				i := strings.Index(out[at:], want)
				if i < 0 {
					t.Fatalf("%q is missing or out of order:\n%s", want, out)
				}
				at += i
			}
			var refused []string
			for _, line := range strings.Split(out, "\n") {
				for _, r := range []string{refusedIPv6, refusedHost, refusedPort} {
					if strings.HasSuffix(line, r) {
						refused = append(refused, r)
					}
				}
			}
			if strings.Join(refused, "|") != strings.Join(tc.refusals, "|") {
				t.Errorf("refusals %q, want %q:\n%s", refused, tc.refusals, out)
			}
			if tc.closing == "" {
				if strings.Contains(out, "Run All") {
					t.Errorf("a closing line with no server given:\n%s", out)
				}
			} else if !strings.HasSuffix(out, ExampleConfigPath+"\n"+tc.closing+"\n") {
				t.Errorf("the closing line is not %q, last:\n%s", tc.closing, out)
			}

			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file := "\n" + string(b)
			if got := strings.Contains(file, "\nntpServers:"); got != (tc.ntp != nil) {
				t.Errorf("ntpServers in the file: %t:\n%s", got, b)
			}
			if got := strings.Contains(file, "\nsyslogServers:"); got != (tc.syslog != nil) {
				t.Errorf("syslogServers in the file: %t:\n%s", got, b)
			}
			c, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(c.NTPServers, "|") != strings.Join(tc.ntp, "|") {
				t.Errorf("ntpServers %q, want %q", c.NTPServers, tc.ntp)
			}
			if len(c.SyslogServers) != len(tc.syslog) || len(tc.syslog) == 1 && c.SyslogServers[0] != tc.syslog[0] {
				t.Errorf("syslogServers %+v, want %+v", c.SyslogServers, tc.syslog)
			}
			if _, err := c.NTPServers.Resolve(); err != nil {
				t.Errorf("ntpServers: %v", err)
			}
			servers, err := c.SyslogServers.Resolve()
			if err != nil {
				t.Fatalf("syslogServers: %v", err)
			}
			for _, s := range servers {
				port := tc.syslog[0].Port
				if port == 0 {
					port = LogPort
				}
				if s.Protocol != LogTCP || s.Port != port {
					t.Errorf("the syslog server is %+v, want tcp and port %d", s, port)
				}
			}
		})
	}

	// With no terminal nothing is asked and no line is added.
	isInteractive = func() bool { return false }
	ensureExample = func() error { return errors.New("test: /etc/hardn not written") }
	useReader(t, prompt.New(strings.NewReader("time.example.net\n"), true))
	c := DefaultConfig()
	c.NTPServers = NTPServers{"time.example.net"}
	out, _, _ := ask(t, func() { _ = CreateDefaultConfig(filepath.Join(t.TempDir(), "hardn.yml"), c) })
	if strings.Contains(out, "NTP") {
		t.Errorf("with no terminal:\n%s", out)
	}
}
