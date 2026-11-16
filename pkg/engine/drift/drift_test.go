package drift_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/drift"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/host/timesync/timefake"
)

// line is a difference in a test's words: its fact, its change, and what
// it was and is ("?" for not established).
func line(d drift.Difference) string {
	w := func(s *string) string {
		if s == nil {
			return "?"
		}
		return *s
	}
	return fmt.Sprintf("%s %s [%s] [%s]", d.Fact, d.Change, w(d.Was), w(d.Is))
}

func lines(ds []drift.Difference) []string {
	out := []string{}
	for _, d := range ds {
		out = append(out, line(d))
	}
	return out
}

// TestReadEstablishes: on the model as hardn leaves it, every fact is
// established, on Debian (ufw) and on Rocky Linux (firewalld), each once
// and in Specs' order; and reading it twice gives no difference.
func TestReadEstablishes(t *testing.T) {
	for _, distro := range []string{"debian", "rocky"} {
		t.Run(distro, func(t *testing.T) {
			h := newHost(t, distro)
			facts := h.read()
			if len(facts) != len(drift.Specs) {
				t.Fatalf("%d facts, %d specs", len(facts), len(drift.Specs))
			}
			for i, f := range facts {
				if f.ID != drift.Specs[i].ID || !f.Known || f.NotCompared {
					t.Errorf("fact %d: %+v", i, f)
				}
			}
			if ds, notes := drift.Compare(facts, h.read()); len(ds) != 0 || len(notes) != 0 {
				t.Errorf("read twice: %q, %v", lines(ds), notes)
			}
		})
	}
}

// TestReadOnlyReads: a reading writes nothing, takes no lock and starts
// nothing: every call it makes of the host is a read, and every command a
// query.
func TestReadOnlyReads(t *testing.T) {
	for _, distro := range []string{"debian", "rocky"} {
		h := newHost(t, distro)
		h.read()
		reads := []string{"run", "read", "read-nofollow", "scan-lines", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group"}
		for _, c := range h.Calls() {
			if !slices.Contains(reads, c.Op) {
				t.Errorf("%s: %s", distro, c)
			}
		}
		for _, argv := range h.Runs() {
			cmd := strings.Join(argv, " ")
			ok := false
			for _, q := range []string{"sshd -T", "visudo -c", "ss -H", "ufw status verbose", "systemctl is-", "firewall-cmd --state", "firewall-cmd --get-",
				"firewall-cmd --list-all-zones", "firewall-cmd --permanent --service=ssh --get-ports", "apt-config dump", "dpkg-query --show", "rpm -q ",
				"chronyc -N tracking", "timedatectl show"} {
				ok = ok || strings.Contains(cmd, q)
			}
			if !ok {
				t.Errorf("%s: ran %q, not a query", distro, cmd)
			}
		}
	}
}

// TestDifferences: each kind of change a person makes to a host after its
// baseline was read is one difference, or the few its fact gives, with
// what it was and what it is; a change that reaches nothing from outside
// is none.
func TestDifferences(t *testing.T) {
	georgeKey2 := "george ssh-ed25519 " + fingerprint(2)
	for _, tc := range []struct {
		name, distro string
		before       func(*simHost)
		change       func(*simHost)
		want         []string
	}{
		{name: "a ufw rule added", change: func(h *simHost) { h.rules = append(h.rules, "8080/tcp") },
			want: []string{"firewall.rules added [] [allow in 8080/tcp]", "firewall.rules added [] [allow in 8080/tcp (IPv6)]"}},
		{name: "a ufw rule from one source added", change: func(h *simHost) { h.rules = append(h.rules, "5432/tcp 192.0.2.7") },
			want: []string{"firewall.rules added [] [allow in 5432/tcp from 192.0.2.7]"}},
		{name: "a ufw rule removed", before: func(h *simHost) { h.rules = append(h.rules, "8080/tcp") }, change: func(h *simHost) { h.rules = h.rules[:1] },
			want: []string{"firewall.rules removed [allow in 8080/tcp] []", "firewall.rules removed [allow in 8080/tcp (IPv6)] []"}},
		{name: "the ufw rules in another order", before: func(h *simHost) { h.rules = append(h.rules, "8080/tcp 192.0.2.7") }, change: func(h *simHost) { slices.Reverse(h.rules) },
			want: []string{"firewall.rules changed [allow in 22/tcp, allow in 8080/tcp from 192.0.2.7, allow in 22/tcp (IPv6)] [allow in 8080/tcp from 192.0.2.7, allow in 22/tcp, allow in 22/tcp (IPv6)]"}},
		{name: "a firewalld port added", distro: "rocky", change: func(h *simHost) { h.zonePorts = append(h.zonePorts, "9090/tcp") },
			want: []string{"firewall.rules added [] [zone public: port 9090/tcp]"}},
		{name: "password login back on", change: func(h *simHost) { h.passwords = "yes" },
			want: []string{"ssh.passwordLogin changed [no] [yes]"}},
		{name: "root login changed", change: func(h *simHost) { h.rootLogin = "yes" },
			want: []string{"ssh.rootLogin changed [prohibit-password] [yes]"}},
		{name: "root login turned off, the safer direction", change: func(h *simHost) { h.rootLogin = "no" },
			want: []string{"ssh.rootLogin changed [prohibit-password] [no]"}},
		{name: "the port changed", change: func(h *simHost) {
			h.port, h.rules, h.listen = 2230, []string{"2230/tcp"}, []string{"0.0.0.0:2230", "[::]:2230"}
		}, want: []string{"ssh.ports changed [22] [2230]", "ssh.listeners changed [0.0.0.0:22, [::]:22] [0.0.0.0:2230, [::]:2230]",
			"firewall.rules removed [allow in 22/tcp] []", "firewall.rules removed [allow in 22/tcp (IPv6)] []",
			"firewall.rules added [] [allow in 2230/tcp]", "firewall.rules added [] [allow in 2230/tcp (IPv6)]",
			"firewall.ports removed [22/tcp] []", "firewall.ports added [] [2230/tcp]"}},
		{name: "an admin added", change: func(h *simHost) {
			h.users = append(h.users, simUser{name: "carol", uid: 1002, groups: []string{"sudo"}, pw: syntheticHash})
		}, want: []string{"users.admins added [] [carol]"}},
		{name: "an account given admin rights", change: func(h *simHost) { h.users[1].groups = []string{"sudo"} },
			want: []string{"users.admins added [] [alice]"}},
		{name: "a second account with uid 0", change: func(h *simHost) {
			h.users = append(h.users, simUser{name: "toor", uid: 0, pw: syntheticHash})
		}, want: []string{"users.uid0 added [] [toor]"}},
		{name: "a key added", change: func(h *simHost) { h.users[0].keys = []byte{1, 2} },
			want: []string{"users.keys added [] [" + georgeKey2 + "]"}},
		{name: "a key removed", before: func(h *simHost) { h.users[0].keys = []byte{1, 2} }, change: func(h *simHost) { h.users[0].keys = []byte{1} },
			want: []string{"users.keys removed [" + georgeKey2 + "] []"}},
		{name: "an account that is no admin gets a key", change: func(h *simHost) { h.users[1].keys = []byte{9} }},
		{name: "an empty password", change: func(h *simHost) { h.users[1].pw = "" },
			want: []string{"users.emptyPasswords added [] [alice]"}},
		{name: "nameservers changed", change: func(h *simHost) { h.nameservers = []string{"192.0.2.1"} },
			want: []string{"dns.nameservers changed [192.0.2.53, 192.0.2.54] [192.0.2.1]"}},
		{name: "nameservers in another order", change: func(h *simHost) { slices.Reverse(h.nameservers) },
			want: []string{"dns.nameservers changed [192.0.2.53, 192.0.2.54] [192.0.2.54, 192.0.2.53]"}},
		{name: "auto updates turned off", change: func(h *simHost) { h.updates = false },
			want: []string{"updates.auto changed [on] [off]"}},
		// The set of syslog servers (design-syslog-servers.md §2.7): one
		// added or removed is one line, whatever the order.
		{name: "a syslog server added", before: func(h *simHost) { h.forwards = []string{"@@192.0.2.51"} },
			change: func(h *simHost) { h.forwards = append(h.forwards, "@192.0.2.52:1514") },
			want:   []string{"logs.servers added [] [192.0.2.52:1514 UDP]"}},
		{name: "a syslog server removed", before: func(h *simHost) { h.forwards = []string{"@@192.0.2.51", "@192.0.2.52"} },
			change: func(h *simHost) { h.forwards = h.forwards[1:] },
			want:   []string{"logs.servers removed [192.0.2.51:514 TCP] []"}},
		{name: "a syslog server's protocol changed", before: func(h *simHost) { h.forwards = []string{"@@192.0.2.51"} },
			change: func(h *simHost) { h.forwards = []string{"@192.0.2.51"} },
			want:   []string{"logs.servers removed [192.0.2.51:514 TCP] []", "logs.servers added [] [192.0.2.51:514 UDP]"}},
		{name: "the syslog servers in another order", before: func(h *simHost) { h.forwards = []string{"@@192.0.2.51", "@192.0.2.52"} },
			change: func(h *simHost) { slices.Reverse(h.forwards) }},
		{name: "forwarding on Rocky Linux", distro: "rocky", change: func(h *simHost) { h.forwards = []string{"@@logs.example.net:6514"} },
			want: []string{"logs.servers added [] [logs.example.net:6514 TCP]"}},
		{name: "rsyslog removed, its file left: journald alone forwards nowhere", before: func(h *simHost) { h.forwards = []string{"@@192.0.2.51"} },
			change: func(h *simHost) { h.noRsyslog = true },
			want:   []string{"logs.servers removed [192.0.2.51:514 TCP] []"}},
		{name: "rsyslog's forwarding hardn can't make out", before: func(h *simHost) { h.forwards = []string{"@@192.0.2.51"} },
			change: func(h *simHost) { h.forwards = []string{"@@:514"} },
			want:   []string{"logs.servers changed [192.0.2.51:514 TCP] [?]"}},
		// The NTP servers, as NTP Settings reads them: one added, removed
		// or changed, and hardn's own.
		{name: "an NTP server added", change: func(h *simHost) {
			h.ntp[timesync.SourcesDir+"/site.sources"] = "server 192.0.2.10 iburst\n"
		}, want: []string{"time.servers added [] [192.0.2.10 in site.sources]"}},
		{name: "an NTP server removed", change: func(h *simHost) {
			h.ntp[timesync.ChronyConfDebian] = strings.Replace(timefake.DebianConf, "pool 2.debian.pool.ntp.org iburst\n", "", 1)
		}, want: []string{"time.servers removed [2.debian.pool.ntp.org] []"}},
		{name: "an NTP server changed", change: func(h *simHost) {
			h.ntp[timesync.ChronyConfDebian] = strings.Replace(timefake.DebianConf, "2.debian.pool", "0.debian.pool", 1)
		}, want: []string{"time.servers removed [2.debian.pool.ntp.org] []", "time.servers added [] [0.debian.pool.ntp.org]"}},
		{name: "hardn's NTP servers added", change: func(h *simHost) { h.ntp[timesync.ChronySources] = ntpSources(h.t, "192.0.2.10") },
			want: []string{"time.servers added [] [192.0.2.10 by hardn]"}},
		{name: "an NTP server changed on Rocky Linux", distro: "rocky", change: func(h *simHost) {
			h.ntp[timesync.ChronyConfEL] = strings.Replace(timefake.ELConf, "2.rocky.pool.ntp.org", "time.example.net", 1)
		}, want: []string{"time.servers removed [2.rocky.pool.ntp.org] []", "time.servers added [] [time.example.net]"}},
		{name: "chrony's settings hardn can't make out", change: func(h *simHost) { h.ntp[timesync.ChronyConfDebian] = "server\n" },
			want: []string{"time.servers changed [2.debian.pool.ntp.org] [?]"}},
		{name: "a new reachable listening port", before: func(h *simHost) { h.rules = append(h.rules, "8080/tcp") },
			change: func(h *simHost) { h.listen = append(h.listen, "0.0.0.0:8080") },
			want:   []string{"firewall.ports added [] [8080/tcp]"}},
		{name: "a loopback-only port is not counted", change: func(h *simHost) { h.listen = append(h.listen, "127.0.0.1:6379", "[::1]:6379") }},
		{name: "a port the firewall blocks is not counted", change: func(h *simHost) { h.listen = append(h.listen, "0.0.0.0:9000") }},
		{name: "the firewall turned off", change: func(h *simHost) { h.ufwOn = false },
			want: []string{"firewall.state changed [on] [off]"}},
		{name: "the firewall turned on, the safer direction", before: func(h *simHost) { h.ufwOn = false }, change: func(h *simHost) { h.ufwOn = true },
			want: []string{"firewall.state changed [off] [on]"}},
		{name: "the firewall no longer starts at boot", change: func(h *simHost) { h.ufwBoot = false },
			want: []string{"firewall.boot changed [yes] [no]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			distro := tc.distro
			if distro == "" {
				distro = "debian"
			}
			h := newHost(t, distro)
			if tc.before != nil {
				h.change(tc.before)
			}
			base := h.read()
			h.change(tc.change)
			ds, notes := drift.Compare(base, h.read())
			got, want := lines(ds), tc.want
			if want == nil {
				want = []string{}
			}
			if !slices.Equal(got, want) || len(notes) != 0 {
				t.Errorf("got  %q\nwant %q\nnotes %v", got, want, notes)
			}
		})
	}
}

// fingerprint is the fingerprint of synthKey(seed), as ssh-keygen -l
// prints it.
func fingerprint(seed byte) string {
	blob, _ := base64.StdEncoding.DecodeString(strings.Fields(synthKey(seed, ""))[1])
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// TestFirewallOffPorts: with the firewall turned off, what listens off
// loopback is reachable, a port it blocked before too.
func TestFirewallOffPorts(t *testing.T) {
	h := newHost(t, "debian")
	h.change(func(h *simHost) { h.listen = append(h.listen, "0.0.0.0:9000") })
	base := h.read()
	h.change(func(h *simHost) { h.ufwOn = false })
	ds, _ := drift.Compare(base, h.read())
	want := []string{"firewall.state changed [on] [off]", "firewall.ports added [] [9000/tcp]"}
	if got := lines(ds); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNotEstablished: a fact that cannot be read is kept as not
// established, never as empty: against a baseline that has it, that is
// one difference, on either side; against one that lacks it too, none.
func TestNotEstablished(t *testing.T) {
	h := newHost(t, "debian")
	base := h.read()
	h.change(func(h *simHost) { h.noSudoers = true })
	now := h.read()
	admins := now[0]
	if admins.ID != drift.Admins || admins.Known || admins.Why == "" || len(admins.Items) != 0 {
		t.Fatalf("admins: %+v", admins)
	}
	want := []string{"users.admins changed [george] [?]",
		"users.keys changed [george ssh-ed25519 " + fingerprint(1) + ", root ssh-ed25519 " + fingerprint(0x72) + "] [?]"}
	ds, _ := drift.Compare(base, now)
	if got := lines(ds); !slices.Equal(got, want) {
		t.Errorf("established, then not:\ngot  %q\nwant %q", got, want)
	}
	ds, _ = drift.Compare(now, base)
	if got := lines(ds); len(got) != 2 || got[0] != "users.admins changed [?] [george]" {
		t.Errorf("not established, then established: %q", got)
	}
	if ds, _ = drift.Compare(now, h.read()); len(ds) != 0 {
		t.Errorf("established on neither side: %q", lines(ds))
	}
}

// TestNoSS: without ss the listening ports are recorded as not compared,
// with why, and are no difference against a baseline that has them, nor
// the other way round: a note says so, and why: the host has no ss, or it
// has and the baseline was saved without.
func TestNoSS(t *testing.T) {
	h := newHost(t, "debian")
	with := h.read()
	h.change(func(h *simHost) { h.noSS = true })
	without := h.read()
	ports := without[13]
	if ports.ID != drift.Ports || ports.Known || !ports.NotCompared || ports.Why != drift.NoSS {
		t.Fatalf("ports: %+v", ports)
	}
	for name, tc := range map[string]struct {
		pair [2]drift.Facts
		why  string
	}{"baseline has ss": {[2]drift.Facts{with, without}, drift.NoSS}, "baseline has none": {[2]drift.Facts{without, with}, drift.NoSSBaseline},
		"neither": {[2]drift.Facts{without, without}, drift.NoSS}} {
		ds, notes := drift.Compare(tc.pair[0], tc.pair[1])
		if len(ds) != 0 || len(notes) != 1 || notes[0].Fact != drift.Ports || notes[0].Why != tc.why {
			t.Errorf("%s: %q, %+v", name, lines(ds), notes)
		}
	}
}

// ntpSources is hardn's own chrony file naming servers.
func ntpSources(t *testing.T, servers ...string) string {
	t.Helper()
	data, err := timesync.SourcesFile(servers)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestReadNTP: the NTP servers as each time service has them, each with
// where it is set but for the service's own files; none with no time
// service or in a container; not established, with why, where the
// settings are not read.
func TestReadNTP(t *testing.T) {
	ntp := func(t *testing.T, h *simHost) drift.Fact {
		t.Helper()
		fs := h.read()
		f := fs[len(fs)-1]
		if f.ID != drift.TimeServers {
			t.Fatalf("the last fact: %+v", f)
		}
		return f
	}
	for _, tc := range []struct {
		name   string
		change func(*simHost)
		want   []string
	}{
		{"chrony", nil, []string{"2.debian.pool.ntp.org"}},
		{"chrony, hardn's servers and DHCP's", func(h *simHost) {
			h.ntp[timesync.ChronySources] = ntpSources(h.t, "192.0.2.10", "192.0.2.11")
			h.ntp[timesync.ChronyDHCP+"/eth0.sources"] = "server 192.0.2.1 iburst\n"
		}, []string{"192.0.2.1 from DHCP", "192.0.2.10 by hardn", "192.0.2.11 by hardn", "2.debian.pool.ntp.org"}},
		{"systemd-timesyncd", func(h *simHost) {
			h.time = &timefake.Sim{Service: "systemd-timesyncd", Synced: "deb.example", Fallback: "0.debian.pool.ntp.org", Packages: map[string]bool{"systemd-timesyncd": true}}
			h.ntp = map[string]string{"/etc/systemd/timesyncd.conf": "[Time]\n#NTP=\n"}
		}, []string{"0.debian.pool.ntp.org built in"}},
		{"systemd-timesyncd, servers set", func(h *simHost) {
			h.time = &timefake.Sim{Service: "systemd-timesyncd", Synced: "192.0.2.10", Fallback: "0.debian.pool.ntp.org", Link: "192.0.2.1", Packages: map[string]bool{"systemd-timesyncd": true}}
			data, err := timesync.TimesyncdFile([]string{"192.0.2.10"})
			if err != nil {
				h.t.Fatal(err)
			}
			h.ntp = map[string]string{timesync.TimesyncdDropIn: string(data), "/etc/systemd/timesyncd.conf.d/10-site.conf": "[Time]\nNTP=192.0.2.20\n"}
		}, []string{"192.0.2.1 from DHCP", "192.0.2.10 by hardn", "192.0.2.20 in 10-site.conf"}},
		{"no time service", func(h *simHost) { h.time = &timefake.Sim{Packages: map[string]bool{}} }, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, "debian")
			if tc.change != nil {
				h.change(tc.change)
				h.time.Read = func(name string) ([]byte, error) { return h.ReadFile(context.Background(), name) }
			}
			f := ntp(t, h)
			if got := f.Items; !f.Known || !slices.Equal(append([]string{}, got...), tc.want) {
				t.Errorf("got %+v, want %q", f, tc.want)
			}
		})
	}
	t.Run("not read", func(t *testing.T) {
		h := newHost(t, "debian")
		h.change(func(h *simHost) { h.ntp[timesync.ChronyConfDebian] = "server\n" })
		if f := ntp(t, h); f.Known || len(f.Items) != 0 || !strings.Contains(f.Why, "time servers hardn can't make out") {
			t.Errorf("%+v", f)
		}
	})

	// busybox ntpd, on Alpine, and a container: only this fact is looked
	// at, on a host that answers the time service's commands alone.
	bare := func(sim *timefake.Sim, files ...string) *hostfake.System {
		h := hostfake.New(host.Options{})
		for i := 0; i < len(files); i += 2 {
			h.AddFile(files[i], []byte(files[i+1]), 0o644, 0, 0)
		}
		sim.Read = func(name string) ([]byte, error) { return h.ReadFile(context.Background(), name) }
		h.Handler = func(argv []string, _ []byte) hostfake.Response {
			if r, ok := sim.Answer(argv); ok {
				return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}
			}
			return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
		}
		return h
	}
	alpine := detect.Facts{Platform: detect.Platform{Distro: detect.DistroAlpine}, Init: detect.Init{Kind: detect.OpenRC, Running: detect.Yes}}
	inBox := detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes},
		Container: detect.Container{Detected: detect.Yes}}
	for _, tc := range []struct {
		name  string
		h     *hostfake.System
		facts detect.Facts
		want  []string
	}{
		{"busybox ntpd", bare(&timefake.Sim{Service: "ntpd", Synced: "pool.ntp.org"}, "/lib/apk/db/installed", "P:busybox\nV:1.37.0-r30\n\n",
			timesync.ConfD, timefake.AlpineConfD), alpine, []string{"pool.ntp.org"}},
		{"in a container", bare(&timefake.Sim{}), inBox, []string{}},
	} {
		fs, err := drift.Read(context.Background(), tc.h, tc.facts)
		if err != nil {
			t.Fatal(err)
		}
		if f := fs[len(fs)-1]; f.ID != drift.TimeServers || !f.Known || !slices.Equal(append([]string{}, f.Items...), tc.want) {
			t.Errorf("%s: %+v", tc.name, f)
		}
	}
}

// facts are a reading made by hand: every fact established, v its value
// or its one item.
func facts(v string) drift.Facts {
	var fs drift.Facts
	for _, sp := range drift.Specs {
		f := drift.Fact{ID: sp.ID, Known: true}
		if sp.Set() {
			f.Items = []string{v}
		} else {
			f.Value = v
		}
		fs = append(fs, f)
	}
	return fs
}

// TestCompare: the comparison is pure and deterministic. The order of the
// facts and of a set's items changes nothing, but the firewall rules'
// (TestRuleOrder); a set is compared item by item, removed before added,
// sorted; why a fact is not established is not compared; a fact a reading
// lacks is not established.
func TestCompare(t *testing.T) {
	a := facts("x")
	a[12].Items = []string{"allow in 22/tcp", "allow in 80/tcp", "allow in 443/tcp"}
	a[13].Items = []string{"22/tcp", "80/tcp", "443/tcp"}
	b := slices.Clone(a)
	slices.Reverse(b)
	b[2].Items = []string{"443/tcp", "22/tcp", "80/tcp", "22/tcp"}
	if ds, notes := drift.Compare(a, b); len(ds) != 0 || len(notes) != 0 {
		t.Errorf("another order: %q", lines(ds))
	}

	b = slices.Clone(a)
	b[12].Items = []string{"allow in 9000/tcp", "allow in 22/tcp", "allow in 25/tcp"}
	want := []string{"firewall.rules removed [allow in 443/tcp] []", "firewall.rules removed [allow in 80/tcp] []",
		"firewall.rules added [] [allow in 25/tcp]", "firewall.rules added [] [allow in 9000/tcp]"}
	ds, _ := drift.Compare(a, b)
	if got := lines(ds); !slices.Equal(got, want) {
		t.Errorf("a set:\ngot  %q\nwant %q", got, want)
	}
	if ds[0].Title != "Firewall rule" {
		t.Errorf("an item's title: %q", ds[0].Title)
	}
	for range 5 {
		again, _ := drift.Compare(a, b)
		if !slices.Equal(lines(again), want) {
			t.Fatalf("not the same list twice: %q", lines(again))
		}
	}

	u1, u2 := slices.Clone(a), slices.Clone(a)
	u1[4], u2[4] = drift.Fact{ID: drift.RootLogin, Why: "sshd -T failed"}, drift.Fact{ID: drift.RootLogin, Why: "another reason"}
	if ds, _ := drift.Compare(u1, u2); len(ds) != 0 {
		t.Errorf("why is compared: %q", lines(ds))
	}
	ds, _ = drift.Compare(a, u1)
	if got := lines(ds); !slices.Equal(got, []string{"ssh.rootLogin changed [x] [?]"}) || ds[0].Title != "SSH root login" {
		t.Errorf("not established on one side: %q", got)
	}

	ds, _ = drift.Compare(a, a[:len(a)-1])
	if got := lines(ds); !slices.Equal(got, []string{"time.servers changed [x] [?]"}) {
		t.Errorf("a fact a reading lacks: %q", got)
	}
	changed := slices.Clone(a)
	changed[len(a)-1].Items = []string{"y"}
	ds, notes := drift.Compare(a[:len(a)-1], changed)
	if len(ds) != 0 || len(notes) != 1 || notes[0].Fact != drift.TimeServers || notes[0].Title != "NTP servers" || notes[0].Why != drift.NotInBaseline {
		t.Errorf("a fact the baseline lacks: %q, %+v", lines(ds), notes)
	}

	e1, e2 := slices.Clone(a), slices.Clone(a)
	e1[14].Value, e2[14].Value = "", "192.0.2.53"
	ds, _ = drift.Compare(e1, e2)
	if got := lines(ds); !slices.Equal(got, []string{"dns.nameservers changed [] [192.0.2.53]"}) {
		t.Errorf("an empty value: %q", got)
	}
}

// TestRuleOrder: ufw applies the first rule that matches, so the same
// rules in another order are one difference, with both orders. A rule
// added or removed is its own difference and moves no other rule; one
// written twice counts where it is first.
func TestRuleOrder(t *testing.T) {
	const deny, ssh, web = "deny in anything from 203.0.113.0/24", "allow in 22/tcp", "allow in 80/tcp"
	rules := func(items ...string) drift.Facts {
		fs := facts("x")
		fs[12].Items = items
		return fs
	}
	for _, tc := range []struct {
		name      string
		base, now []string
		want      []string
	}{
		{"the same order", []string{deny, ssh}, []string{deny, ssh}, nil},
		{"two rules swapped", []string{deny, ssh}, []string{ssh, deny},
			[]string{"firewall.rules changed [" + deny + ", " + ssh + "] [" + ssh + ", " + deny + "]"}},
		{"a rule added in front", []string{deny, ssh}, []string{web, deny, ssh}, []string{"firewall.rules added [] [" + web + "]"}},
		{"a rule removed from between", []string{deny, web, ssh}, []string{deny, ssh}, []string{"firewall.rules removed [" + web + "] []"}},
		{"a rule added and two swapped", []string{deny, ssh}, []string{ssh, web, deny},
			[]string{"firewall.rules added [] [" + web + "]", "firewall.rules changed [" + deny + ", " + ssh + "] [" + ssh + ", " + deny + "]"}},
		{"a rule twice", []string{deny, ssh}, []string{deny, ssh, deny}, nil},
	} {
		ds, _ := drift.Compare(rules(tc.base...), rules(tc.now...))
		want := tc.want
		if want == nil {
			want = []string{}
		}
		if got := lines(ds); !slices.Equal(got, want) {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, want)
		}
		if n := len(ds); n > 0 && ds[n-1].Change == drift.Changed && ds[n-1].Title != "Firewall rule order" {
			t.Errorf("%s: title %q", tc.name, ds[n-1].Title)
		}
	}
}

// stateHost is a fake with hardn's state directory as an install leaves
// it.
func stateHost() *hostfake.System {
	h := hostfake.New(host.Options{})
	h.AddDir("/var", 0o755, 0, 0)
	h.AddDir("/var/lib", 0o755, 0, 0)
	return h
}

var saved = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

// TestBaselineRoundTrip: Save writes the baseline root's and 0600 in a
// directory made 0700, by a rename, and Load gives it back as saved; no
// file is ErrNoBaseline. Kept says whether one is there.
func TestBaselineRoundTrip(t *testing.T) {
	ctx := context.Background()
	h := stateHost()
	if _, err := drift.Load(ctx, h, revert.Options{}); !errors.Is(err, drift.ErrNoBaseline) {
		t.Fatalf("no baseline: %v", err)
	}
	if drift.Kept(ctx, h, revert.Options{}) {
		t.Error("kept, with none")
	}
	if n := len(h.Calls()); n == 0 {
		t.Fatal("no call recorded")
	}
	for _, c := range h.Calls() {
		if c.Op != "lstat" && c.Op != "read-nofollow" {
			t.Errorf("Load and Kept do more than look: %s", c)
		}
	}
	b := drift.Baseline{HardnVersion: "0.10.0", Saved: saved, User: "george", Facts: facts("x")}
	if err := drift.Save(ctx, h, revert.Options{}, b); err != nil {
		t.Fatal(err)
	}
	e, ok := h.Entry(drift.Path)
	if !ok || e.Mode.Perm() != 0o600 || e.UID != 0 || !e.Mode.IsRegular() {
		t.Errorf("the file: %+v, %v", e, ok)
	}
	if d, _ := h.Entry("/var/lib/hardn"); d.Mode.Perm() != 0o700 || !d.Mode.IsDir() {
		t.Errorf("the directory: %+v", d)
	}
	wrote := 0
	for _, c := range h.Calls() {
		switch c.Op {
		case "write-atomic":
			wrote++
			if c.Path != drift.Path {
				t.Errorf("wrote %s", c.Path)
			}
		case "write-in-place", "append", "lock":
			t.Errorf("Save: %s", c)
		}
	}
	if wrote != 1 {
		t.Errorf("%d atomic writes", wrote)
	}
	got, err := drift.Load(ctx, h, revert.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != drift.Format || got.HardnVersion != "0.10.0" || !got.Saved.Equal(saved) || got.User != "george" {
		t.Errorf("loaded %+v", got)
	}
	if ds, _ := drift.Compare(b.Facts, got.Facts); len(ds) != 0 || len(got.Facts) != len(drift.Specs) {
		t.Errorf("the facts changed on the way: %q", lines(ds))
	}
	if !drift.Kept(ctx, h, revert.Options{}) {
		t.Error("not kept, with one")
	}
}

// TestBaselineRuleOrder: the baseline keeps the firewall rules in ufw's
// order, not sorted, so a later check can tell another order; a rule
// twice, or another set out of order, is not a baseline hardn wrote.
func TestBaselineRuleOrder(t *testing.T) {
	ctx := context.Background()
	h := stateHost()
	fs := facts("x")
	fs[12].Items = []string{"deny in anything from 203.0.113.0/24", "allow in 22/tcp"}
	if err := drift.Save(ctx, h, revert.Options{}, drift.Baseline{Saved: saved, Facts: fs}); err != nil {
		t.Fatal(err)
	}
	got, err := drift.Load(ctx, h, revert.Options{})
	if err != nil || !slices.Equal(got.Facts[12].Items, fs[12].Items) {
		t.Fatalf("loaded %q, %v", got.Facts[12].Items, err)
	}
	now := slices.Clone(fs)
	now[12].Items = []string{"allow in 22/tcp", "deny in anything from 203.0.113.0/24"}
	if ds, _ := drift.Compare(got.Facts, now); len(ds) != 1 || ds[0].Title != "Firewall rule order" {
		t.Errorf("another order after a round trip: %q", lines(ds))
	}
	twice, ports := slices.Clone(fs), slices.Clone(fs)
	twice[12].Items = []string{"allow in 22/tcp", "deny in anything", "allow in 22/tcp"}
	ports[13].Items = []string{"80/tcp", "22/tcp"}
	for name, bad := range map[string]drift.Facts{"a rule twice": twice, "ports not sorted": ports} {
		if err := drift.Save(ctx, stateHost(), revert.Options{}, drift.Baseline{Saved: saved, Facts: bad}); !errors.Is(err, drift.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestBaselineRefused: a baseline that is not root's alone, or is no
// regular file, is not read (ErrUnsafe); one that is not as Save writes it
// is ErrInvalid; one of another format ErrFormat, whatever else it holds.
func TestBaselineRefused(t *testing.T) {
	ctx := context.Background()
	good := func(t *testing.T) (*hostfake.System, []byte) {
		h := stateHost()
		if err := drift.Save(ctx, h, revert.Options{}, drift.Baseline{HardnVersion: "0.10.0", Saved: saved, User: "root", Facts: facts("x")}); err != nil {
			t.Fatal(err)
		}
		data, err := h.ReadFile(ctx, drift.Path)
		if err != nil {
			t.Fatal(err)
		}
		return h, data
	}
	replace := func(old, new string) func(*hostfake.System, []byte) {
		return func(h *hostfake.System, data []byte) {
			if !strings.Contains(string(data), old) {
				panic("the baseline has no " + old)
			}
			h.AddFile(drift.Path, []byte(strings.Replace(string(data), old, new, 1)), 0o600, 0, 0)
		}
	}
	for _, tc := range []struct {
		name  string
		plant func(h *hostfake.System, data []byte)
		want  error
	}{
		{"another user's file", func(h *hostfake.System, data []byte) { h.AddFile(drift.Path, data, 0o600, 1000, 1000) }, drift.ErrUnsafe},
		{"a file others can write", func(h *hostfake.System, data []byte) { h.AddFile(drift.Path, data, 0o666, 0, 0) }, drift.ErrUnsafe},
		{"a file its group can write", func(h *hostfake.System, data []byte) { h.AddFile(drift.Path, data, 0o660, 0, 0) }, drift.ErrUnsafe},
		{"a symlink to a good baseline", func(h *hostfake.System, data []byte) {
			h.AddFile("/var/lib/elsewhere.json", data, 0o600, 0, 0)
			h.AddSymlink(drift.Path, "/var/lib/elsewhere.json")
		}, drift.ErrUnsafe},
		{"a directory", func(h *hostfake.System, _ []byte) {
			_ = h.Remove(ctx, drift.Path)
			h.AddDir(drift.Path, 0o700, 0, 0)
		}, drift.ErrUnsafe},
		{"a state directory others can write", func(h *hostfake.System, _ []byte) { h.AddDir("/var/lib/hardn", 0o777, 0, 0) }, drift.ErrUnsafe},
		{"a state directory that is another user's", func(h *hostfake.System, _ []byte) { h.AddDir("/var/lib/hardn", 0o700, 1000, 1000) }, drift.ErrUnsafe},
		{"too large", func(h *hostfake.System, _ []byte) {
			h.AddFile(drift.Path, make([]byte, drift.MaxBaseline+1), 0o600, 0, 0)
		}, drift.ErrInvalid},
		{"not JSON", func(h *hostfake.System, _ []byte) { h.AddFile(drift.Path, []byte("baseline\n"), 0o600, 0, 0) }, drift.ErrInvalid},
		{"empty", func(h *hostfake.System, _ []byte) { h.AddFile(drift.Path, nil, 0o600, 0, 0) }, drift.ErrInvalid},
		{"no format", replace(`"format": 1,`, ``), drift.ErrInvalid},
		{"a field hardn does not know", replace(`"format": 1,`, `"format": 1, "extra": true,`), drift.ErrInvalid},
		{"a fact's field hardn does not know", replace(`"known": true`, `"known": true, "extra": 1`), drift.ErrInvalid},
		{"a second value", func(h *hostfake.System, data []byte) { h.AddFile(drift.Path, append(data, "{}\n"...), 0o600, 0, 0) }, drift.ErrInvalid},
		{"a fact missing", replace(`"id": "users.admins"`, `"id": "users.other"`), drift.ErrInvalid},
		{"a value where a set is", replace(`"items": [`, `"value": "x", "items": [`), drift.ErrInvalid},
		{"no time", replace(`"saved": "2026-11-13T10:00:00Z"`, `"saved": "0001-01-01T00:00:00Z"`), drift.ErrInvalid},
		{"another format", replace(`"format": 1,`, `"format": 2,`), drift.ErrFormat},
		{"another format, with other fields", func(h *hostfake.System, _ []byte) {
			h.AddFile(drift.Path, []byte(`{"format": 7, "snapshot": {"rules": []}}`), 0o600, 0, 0)
		}, drift.ErrFormat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, data := good(t)
			tc.plant(h, data)
			_, err := drift.Load(ctx, h, revert.Options{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			for _, other := range []error{drift.ErrUnsafe, drift.ErrInvalid, drift.ErrFormat, drift.ErrNoBaseline} {
				if other != tc.want && errors.Is(err, other) {
					t.Errorf("%v is %v too", err, other)
				}
			}
		})
	}
}

// TestOlderBaseline: a baseline of v0.10.0 to v0.10.3, Specs' first
// MinFacts facts, is read, and compared in those; the NTP servers, which
// it lacks, are a note and no difference. One with fewer facts, more, or
// out of order is not one hardn wrote; Save writes every fact.
func TestOlderBaseline(t *testing.T) {
	ctx := context.Background()
	if drift.MinFacts != 17 || len(drift.Specs) != 18 || drift.Specs[17].ID != drift.TimeServers {
		t.Fatalf("MinFacts %d, %d specs", drift.MinFacts, len(drift.Specs))
	}
	plant := func(fs drift.Facts) *hostfake.System {
		h := stateHost()
		h.AddDir("/var/lib/hardn", 0o700, 0, 0)
		data, err := json.Marshal(drift.Baseline{Format: drift.Format, HardnVersion: "0.10.3", Saved: saved, User: "root", Facts: fs})
		if err != nil {
			t.Fatal(err)
		}
		h.AddFile(drift.Path, data, 0o600, 0, 0)
		return h
	}
	all := facts("x")
	b, err := drift.Load(ctx, plant(all[:drift.MinFacts]), revert.Options{})
	if err != nil || len(b.Facts) != drift.MinFacts {
		t.Fatalf("17 facts: %v, %d", err, len(b.Facts))
	}
	now := slices.Clone(all)
	now[len(now)-1].Items = []string{"y"}
	if ds, notes := drift.Compare(b.Facts, now); len(ds) != 0 || len(notes) != 1 || notes[0].Fact != drift.TimeServers || notes[0].Why != drift.NotInBaseline {
		t.Errorf("the NTP servers changed: %q, %+v", lines(ds), notes)
	}
	now[4].Value = "y"
	if ds, notes := drift.Compare(b.Facts, now); !slices.Equal(lines(ds), []string{"ssh.rootLogin changed [x] [y]"}) || len(notes) != 1 {
		t.Errorf("root login changed: %q, %+v", lines(ds), notes)
	}
	swapped := slices.Clone(all[:drift.MinFacts])
	swapped[15], swapped[16] = swapped[16], swapped[15]
	ntpFirst := append(drift.Facts{all[17]}, all[:16]...)
	for name, fs := range map[string]drift.Facts{"16 facts": all[:16], "out of order": swapped, "the NTP servers in place of the syslog servers": ntpFirst,
		"19 facts": append(slices.Clone(all), drift.Fact{ID: "x.y", Known: true})} {
		if _, err := drift.Load(ctx, plant(fs), revert.Options{}); !errors.Is(err, drift.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := drift.Save(ctx, stateHost(), revert.Options{}, drift.Baseline{Saved: saved, Facts: all[:drift.MinFacts]}); !errors.Is(err, drift.ErrInvalid) {
		t.Errorf("saved 17 facts: %v", err)
	}
}

// TestSaveReplacesASymlink: a symlink planted at the baseline's path is
// removed itself, never written through.
func TestSaveReplacesASymlink(t *testing.T) {
	ctx := context.Background()
	h := stateHost()
	h.AddDir("/var/lib/hardn", 0o700, 0, 0)
	h.AddFile("/etc-shadow", []byte("root:x\n"), 0o600, 0, 0)
	h.AddSymlink(drift.Path, "/etc-shadow")
	if err := drift.Save(ctx, h, revert.Options{}, drift.Baseline{Saved: saved, Facts: facts("x")}); err != nil {
		t.Fatal(err)
	}
	if data, _ := h.ReadFile(ctx, "/etc-shadow"); string(data) != "root:x\n" {
		t.Errorf("wrote through the symlink: %q", data)
	}
	if e, _ := h.Entry(drift.Path); !e.Mode.IsRegular() {
		t.Errorf("the baseline: %+v", e)
	}
}

// TestSaveRefusesBadFacts: Save writes nothing for facts that are not
// Specs', so that a baseline Load would refuse is never saved.
func TestSaveRefusesBadFacts(t *testing.T) {
	h := stateHost()
	err := drift.Save(context.Background(), h, revert.Options{}, drift.Baseline{Saved: saved, Facts: facts("x")[:3]})
	if !errors.Is(err, drift.ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if _, ok := h.Entry(drift.Path); ok {
		t.Error("wrote it")
	}
}
