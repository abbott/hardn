package config

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestEditOfTheMenusKeys: the edits the Configuration screen saves
// (docs/engine/design-config-edit.md §6), each of one key named: a
// syslogServers block added, with only the fields it sets, one removed with
// its comment, and the last removed; a key the file does not hold added at
// the end, and one set back to its default written as that; username, which
// a save writes only when it names the key.
func TestEditOfTheMenusKeys(t *testing.T) {
	for _, tt := range []struct {
		name, in, key string
		set           func(*Config)
		want          string
	}{
		{"syslogServers: the first", "sshPort: 22 # ssh\n", "syslogServers",
			func(c *Config) { c.SyslogServers = SyslogServers{{Server: "192.0.2.50"}} },
			"sshPort: 22 # ssh\nsyslogServers:\n  - server: 192.0.2.50\n"},
		{"syslogServers: one added, each field it sets", "syslogServers:\n  - server: 192.0.2.50 # first\nsshPort: 22\n", "syslogServers",
			func(c *Config) {
				c.SyslogServers = append(c.SyslogServers, SyslogServer{Server: "logs.example.net", Port: 6515, Protocol: LogTLS, CAFile: "/etc/ssl/ca.pem"})
			},
			"syslogServers:\n  - server: 192.0.2.50 # first\n  - server: logs.example.net\n    port: 6515\n    protocol: tls\n    caFile: /etc/ssl/ca.pem\nsshPort: 22\n"},
		{"syslogServers: one removed, the other kept with its comment",
			"syslogServers:\n  - server: 192.0.2.50 # first\n  - server: 192.0.2.51 # second\n    protocol: udp\nsshPort: 22\n", "syslogServers",
			func(c *Config) { c.SyslogServers = c.SyslogServers[1:] },
			"syslogServers:\n  - server: 192.0.2.51 # second\n    protocol: udp\nsshPort: 22\n"},
		{"syslogServers: the last removed", "# site\nsyslogServers:\n  - server: 192.0.2.50\n    protocol: udp\nsshPort: 22\n", "syslogServers",
			func(c *Config) { c.SyslogServers = SyslogServers{} },
			"# site\nsyslogServers: []\nsshPort: 22\n"},
		{"ntpServers: the last removed", "ntpServers:\n  - time.example.net\n", "ntpServers",
			func(c *Config) { c.NTPServers = NTPServers{} }, "ntpServers: []\n"},
		{"a key the file does not hold, set", "username: george\n", "kernel",
			func(c *Config) { c.KernelSettings = true }, "username: george\nkernel: true\n"},
		{"a key the file does not hold, at its default: nothing", "username: george\n", "sshPort",
			func(c *Config) { c.SshPort = 22 }, "username: george\n"},
		{"a key set back to its default", "username: george\nsshPort: 2222 # moved\n", "sshPort",
			func(c *Config) { c.SshPort = 22 }, "username: george\nsshPort: 22 # moved\n"},
		{"a yes/no key set back to its default", "kernel: true\n", "kernel",
			func(c *Config) { c.KernelSettings = false }, "kernel: false\n"},
		{"confirmTimeout emptied", "confirmTimeout: 5m\n", "confirmTimeout",
			func(c *Config) { c.ConfirmTimeout = "" }, "confirmTimeout: \"\"\n"},
		{"username, named", "username: george # who\n", "username",
			func(c *Config) { c.Username = "bob" }, "username: bob # who\n"},
		{"username, not named: the file's stays", "username: george\nsshPort: 2222\n", "sshPort",
			func(c *Config) { c.Username, c.SshPort = "alice", 2022 }, "username: george\nsshPort: 2022\n"},
	} {
		c := DefaultConfig()
		if err := yaml.Unmarshal([]byte(tt.in), c); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		tt.set(c)
		got, err := Edit([]byte(tt.in), c, tt.key)
		if err != nil || string(got) != tt.want {
			t.Errorf("%s: Edit = %v\n%s\nwant\n%s", tt.name, err, got, tt.want)
		}
	}
}
