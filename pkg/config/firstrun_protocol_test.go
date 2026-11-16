package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestCreateDefaultConfigSyslogProtocol: the first-run protocol question,
// asked only after a syslog server is accepted. Enter is tcp, not written;
// udp is written; tls asks the CA file and is refused without one; a bad
// answer is asked again; the end of the input keeps tcp; host:port keeps
// its port.
func TestCreateDefaultConfigSyslogProtocol(t *testing.T) {
	const head = "\n\n\n\n\n" // username, port, updates, key, NTP
	prevInteractive, prevExample := isInteractive, ensureExample
	isInteractive = func() bool { return true }
	ensureExample = func() error { return nil }
	t.Cleanup(func() { isInteractive, ensureExample = prevInteractive, prevExample })
	for _, tc := range []struct {
		name, answers string
		want          []SyslogServer
		asked         int
		refusals      []string
	}{
		{"Enter is tcp", "logs.example.net\n\n\n", []SyslogServer{{Server: "logs.example.net"}}, 1, nil},
		{"tcp typed", "logs.example.net\nTCP\n\n", []SyslogServer{{Server: "logs.example.net"}}, 1, nil},
		{"udp", "logs.example.net\nudp\n\n", []SyslogServer{{Server: "logs.example.net", Protocol: LogUDP}}, 1, nil},
		{"tls with a CA file", "logs.example.net\nTls\n/etc/ssl/ca.pem\n\n",
			[]SyslogServer{{Server: "logs.example.net", Protocol: LogTLS, CAFile: "/etc/ssl/ca.pem"}}, 1, nil},
		{"tls without a CA file", "logs.example.net\ntls\n\nudp\n\n",
			[]SyslogServer{{Server: "logs.example.net", Protocol: LogUDP}}, 2, []string{refusedNoCA}},
		{"tls to an address", "192.0.2.20\ntls\n/etc/ssl/ca.pem\n\n\n",
			[]SyslogServer{{Server: "192.0.2.20"}}, 2, []string{"TLS needs the server's name, as its certificate has it."}},
		{"a bad answer", "logs.example.net\nsctp\nudp\n\n",
			[]SyslogServer{{Server: "logs.example.net", Protocol: LogUDP}}, 2, []string{refusedProto}},
		{"the end of the input", "logs.example.net\nsctp", []SyslogServer{{Server: "logs.example.net"}}, 2, []string{refusedProto}},
		{"no question when syslog is skipped", "\n\n", nil, 0, nil},
		{"host:port and udp", "192.0.2.20:1514\nudp\n\n",
			[]SyslogServer{{Server: "192.0.2.20", Port: 1514, Protocol: LogUDP}}, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useReader(t, prompt.New(strings.NewReader(head+tc.answers), true))
			path := filepath.Join(t.TempDir(), "hardn.yml")
			var err error
			out, _, _ := ask(t, func() { err = CreateDefaultConfig(path, DefaultConfig()) })
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(out, askProtocol); got != tc.asked {
				t.Errorf("the protocol asked %d times, want %d:\n%s", got, tc.asked, out)
			}
			var refused []string
			for _, line := range strings.Split(out, "\n") {
				for _, r := range append([]string{refusedProto, refusedNoCA}, tc.refusals...) {
					if strings.HasSuffix(line, r) && !strings.HasSuffix(line, askProtocol) {
						refused = append(refused, r)
						break
					}
				}
			}
			if strings.Join(refused, "|") != strings.Join(tc.refusals, "|") {
				t.Errorf("refusals %q, want %q:\n%s", refused, tc.refusals, out)
			}
			c, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.SyslogServers) != len(tc.want) || len(tc.want) == 1 && c.SyslogServers[0] != tc.want[0] {
				t.Errorf("syslogServers %+v, want %+v", c.SyslogServers, tc.want)
			}
			b, _ := os.ReadFile(path)
			if strings.Contains(string(b), "protocol: tcp") {
				t.Errorf("tcp written:\n%s", b)
			}
			if tc.want != nil && !strings.HasSuffix(out, "Run All sets the syslog server.\n") {
				t.Errorf("the closing line changed:\n%s", out)
			}
		})
	}
}
