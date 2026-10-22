package risk_test

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/apparmor"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The rows' control IDs.
const (
	idUser   = "users.admin"
	idFW     = "firewall.incoming"
	idLogin  = "ssh.login"
	idAuth   = "ssh.auth"
	idPort   = "ssh.port"
	idAA     = apparmor.ID
	idUpdate = updates.ID
)

const day = 24 * time.Hour

// figmaUptime is Figma's 00-TUI-MAIN uptime: 182 days, 2 hours, 26
// minutes.
const figmaUptime = 182*day + 2*time.Hour + 26*time.Minute

// rep is a report of control id in state s with detail d and a finding
// per code.
func rep(id string, s engine.State, d string, codes ...string) engine.Report {
	r := engine.Report{Control: id, Status: engine.Status{State: s, Detail: d}}
	for _, c := range codes {
		r.Status.Findings = append(r.Status.Findings, engine.Finding{Code: c, Detail: "synthetic"})
	}
	return r
}

// hardened is every row Configured, with Figma's details.
func hardened() []engine.Report {
	return []engine.Report{
		rep(idUser, engine.Configured, "non-root, sudo"),
		rep(idFW, engine.Configured, "deny policy"),
		rep(idLogin, engine.Configured, "root disallowed"),
		rep(idAuth, engine.Configured, "key-only auth"),
		rep(idPort, engine.Configured, "non-default (2230)"),
		rep(idAA, engine.Configured, "34 profiles enforced"),
		rep(idUpdate, engine.Configured, updates.DetailUnattended),
	}
}

// with is hardened's reports with each of rs in place of its control's.
func with(rs ...engine.Report) []engine.Report {
	out := hardened()
	for _, r := range rs {
		i := slices.IndexFunc(out, func(x engine.Report) bool { return x.Control == r.Control })
		out[i] = r
	}
	return out
}

// without is hardened's reports less the controls ids.
func without(ids ...string) []engine.Report {
	return slices.DeleteFunc(hardened(), func(r engine.Report) bool { return slices.Contains(ids, r.Control) })
}

// figma is Figma's host: User, Firewall and the SSH rows Configured,
// AppArmor and Auto Updates Not Configured.
func figma() []engine.Report {
	return with(rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles), rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended))
}

// uptimeOf is a header up d, no reboot pending.
func uptimeOf(d time.Duration) header.Header {
	return header.Header{Uptime: header.Uptime{Duration: d, Known: true}, Reboot: header.Reboot{Pending: detect.No}}
}

// quiet is a header up 3 days, no reboot pending.
var quiet = uptimeOf(3 * day)

// inContainer is quiet inside a container: the kernel is the host's.
func inContainer(p detect.Tri) header.Header {
	h := quiet
	h.Reboot = header.Reboot{Pending: p, HostKernel: true}
	return h
}

func pending(p detect.Tri, fs ...engine.Finding) header.Header {
	h := quiet
	h.Reboot = header.Reboot{Pending: p, Findings: fs}
	return h
}

var newerKernel = engine.Finding{Code: header.CodeNewerKernel, Detail: header.DetailNewerKernel, Source: "/boot/vmlinuz-6.12.48+deb13-amd64"}

// unread is an Unknown row's factor.
const unread = "Counted as not configured"

// unreads is n Unknown rows' factors.
func unreads(n int) []string { return slices.Repeat([]string{unread}, n) }

type want struct {
	band    risk.Band
	score   float64
	factors []string
	forced  bool
}

// TestAssess: every rule of risk.md §5, each band, NotApplicable and
// Unknown.
func TestAssess(t *testing.T) {
	cases := []struct {
		name    string
		reports []engine.Report
		h       header.Header
		want    want
	}{
		// The bands, each reachable.
		{"hardened: the top band", hardened(), quiet, want{band: risk.Minimal, score: 100}},
		{"figma's host", figma(), uptimeOf(figmaUptime), want{band: risk.Moderate, score: 60, factors: []string{"Up 182 days, no updates"}}},
		{"figma's host, up 3 days", figma(), quiet, want{band: risk.Moderate, score: 70}},
		{"AppArmor not configured", with(rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles)), quiet, want{band: risk.Low, score: 90}},
		{"weak: the SSH auth, firewall, AppArmor and updates off", with(rep(idAuth, engine.NotConfigured, "password auth on"),
			rep(idFW, engine.NotConfigured, "inactive"), rep(idAA, engine.NotConfigured, apparmor.DetailOffInKernel),
			rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)), quiet, want{band: risk.High, score: 30}},
		{"nothing configured", []engine.Report{rep(idUser, engine.NotConfigured, "root only"), rep(idFW, engine.NotConfigured, "inactive"),
			rep(idLogin, engine.NotConfigured, "root allowed"), rep(idAuth, engine.NotConfigured, "password auth on"),
			rep(idPort, engine.NotConfigured, "default (22)"), rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles),
			rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended)}, quiet, want{band: risk.Critical, score: 0}},
		{"no row established", nil, quiet, want{band: risk.Unknown, score: 0, factors: unreads(7)}},

		{"a Proxmox VE node (risk.md §5.5)", []engine.Report{rep(idUser, engine.NotConfigured, "root only"), rep(idFW, engine.NotConfigured, "disabled"),
			rep(idLogin, engine.Configured, "root key-only (Proxmox)"), rep(idAuth, engine.Configured, "key-only auth"),
			rep(idPort, engine.Configured, "default (22)"), rep(idAA, engine.Configured, "12 profiles enforced"),
			rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended)}, uptimeOf(41*day + time.Hour),
			want{band: risk.High, score: 40, factors: []string{"Up 41 days, no updates"}}},

		// The bounds.
		{"25 is High", with(rep(idFW, engine.NotConfigured, "inactive"), rep(idAuth, engine.NotConfigured, "password auth on"),
			rep(idPort, engine.NotConfigured, "default (22)"), rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles),
			rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)), quiet, want{band: risk.High, score: 25}},
		{"50 is Moderate", with(rep(idLogin, engine.NotConfigured, "root allowed"), rep(idPort, engine.NotConfigured, "default (22)"),
			rep(idAA, engine.NotConfigured, apparmor.DetailNoProfiles), rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)),
			quiet, want{band: risk.Moderate, score: 50}},
		{"75 is Low", with(rep(idPort, engine.NotConfigured, "default (22)"), rep(idUpdate, engine.NotConfigured, updates.DetailUpgradesOff)),
			quiet, want{band: risk.Low, score: 75}},
		{"95 is Minimal: the port weighs least", with(rep(idPort, engine.NotConfigured, "default (22)")), quiet, want{band: risk.Minimal, score: 95}},
		{"below 95 is Low", with(rep(idLogin, engine.Partial, "root key-only")), quiet, want{band: risk.Low, score: 92.5}},

		// The port weighs less than the auth settings.
		{"port vs login", with(rep(idLogin, engine.NotConfigured, "root allowed")), quiet, want{band: risk.Low, score: 85}},
		{"port vs auth", with(rep(idAuth, engine.NotConfigured, "password auth on")), quiet, want{band: risk.Low, score: 80}},

		// Partial earns half; a Partial that protects nothing now, none.
		{"firewall off after restart: half", with(rep(idFW, engine.Partial, "off after restart")), quiet, want{band: risk.Low, score: 90}},
		// ... and none with a fact left open, as Unknown: never above the
		// same host with the fact read (risk.md §5.2).
		{"firewall off after restart, ssh unchecked: none", with(rep(idFW, engine.Partial, "off after restart", firewall.CodeSSHUnchecked)), quiet,
			want{band: risk.Low, score: 80, factors: []string{"Can't tell if it blocks SSH"}}},
		{"firewall kept on at boot, ssh unchecked: unknown", with(rep(idFW, engine.Unknown, "ss not installed")), quiet,
			want{band: risk.Low, score: 80, factors: []string{unread}}},
		{"updates off after restart, a timer not ranked: none", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeScheduleUnknown)), quiet,
			want{band: risk.Low, score: 80, factors: []string{"Can't tell if fully on"}}},
		{"updates off after restart, a value not ranked: none", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeAptValueUnknown)), quiet,
			want{band: risk.Low, score: 80, factors: []string{"Can't tell if fully on"}}},
		{"updates kept on at boot, a value not ranked: unknown", with(rep(idUpdate, engine.Unknown, updates.DetailCantRead, updates.CodeAptValueUnknown)), quiet,
			want{band: risk.Low, score: 80, factors: []string{unread}}},
		{"updates lists not refreshed, a fact open: no factor of its own", with(rep(idUpdate, engine.Partial, updates.DetailListsStale, updates.CodeScheduleUnknown)), quiet,
			want{band: risk.Low, score: 80}},
		{"firewall allow policy: none", with(rep(idFW, engine.Partial, firewall.DetailAllowPolicy)), quiet, want{band: risk.Low, score: 80}},
		{"firewall all ports: none", with(rep(idFW, engine.Partial, firewall.DetailAllPorts)), quiet, want{band: risk.Low, score: 80}},
		{"pve-firewall accept policy: none", with(rep(idFW, engine.Partial, firewall.DetailAcceptPolicy)), quiet, want{band: risk.Low, score: 80}},
		{"AppArmor logging only: none", with(rep(idAA, engine.Partial, apparmor.DetailLoggingOnly)), quiet, want{band: risk.Low, score: 90}},
		{"AppArmor off after restart: half", with(rep(idAA, engine.Partial, apparmor.DetailOffAfterRestart)), quiet, want{band: risk.Minimal, score: 95}},
		{"updates lists not refreshed: none", with(rep(idUpdate, engine.Partial, updates.DetailListsStale)), quiet, want{band: risk.Low, score: 80}},
		{"updates off after restart: half", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart)), quiet, want{band: risk.Low, score: 90}},
		{"user without sudo: half", with(rep(idUser, engine.Partial, "non-root, no sudo")), quiet, want{band: risk.Minimal, score: 95}},

		// NotApplicable is left out of the share and the whole.
		{"AppArmor in a container", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost)), inContainer(detect.No),
			want{band: risk.Minimal, score: 100}},
		{"Alpine without AppArmor, port 22", with(rep(idAA, engine.NotApplicable, apparmor.DetailAlpineDefault), rep(idPort, engine.NotConfigured, "default (22)")),
			quiet, want{band: risk.Low, score: 100 * 85.0 / 90}},
		{"every row not applicable", []engine.Report{rep(idUser, engine.NotApplicable, "x"), rep(idFW, engine.NotApplicable, "x"),
			rep(idLogin, engine.NotApplicable, "x"), rep(idAuth, engine.NotApplicable, "x"), rep(idPort, engine.NotApplicable, "x"),
			rep(idAA, engine.NotApplicable, "x"), rep(idUpdate, engine.NotApplicable, "x")}, quiet, want{band: risk.Unknown}},

		// Unknown counts as not configured, and adds a factor under each
		// such row.
		{"port unknown", with(rep(idPort, engine.Unknown, "can't read")), quiet, want{band: risk.Low, score: 95, factors: []string{unread}}},
		{"firewall unknown", with(rep(idFW, engine.Unknown, "can't tell ssh ports")), quiet, want{band: risk.Low, score: 80, factors: []string{unread}}},
		{"a failed check is unknown", with(engine.Report{Control: idAuth, Status: engine.Status{State: engine.Configured}, Err: errors.New("sshd -T")}),
			quiet, want{band: risk.Low, score: 80, factors: []string{unread}}},
		{"a state outside the five is unknown", with(rep(idLogin, engine.State(9), "")), quiet,
			want{band: risk.Low, score: 85, factors: []string{unread}}},
		{"a row with no report is unknown", without(idUser), quiet, want{band: risk.Low, score: 90, factors: []string{unread}}},
		{"two unknown rows, a factor each", with(rep(idUpdate, engine.Unknown, "can't read"), rep(idUser, engine.Unknown, "can't read")), quiet,
			want{band: risk.Moderate, score: 70, factors: unreads(2)}},

		// The lockout forces Critical, once; the level's gloss says it, and
		// the Firewall row, so no factor repeats it.
		{"firewall blocks ssh", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked)), quiet,
			want{band: risk.Critical, score: 90, forced: true}},
		{"firewall blocks ssh, two findings", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked, firewall.CodeSSHBlocked)), quiet,
			want{band: risk.Critical, score: 90, forced: true}},
		{"firewall blocks ssh, the check failed", with(engine.Report{Control: idFW, Err: errors.New("ss"),
			Status: engine.Status{State: engine.Unknown, Findings: []engine.Finding{{Code: firewall.CodeSSHBlocked}}}}), quiet,
			want{band: risk.Critical, score: 80, forced: true, factors: []string{unread}}},
		{"blocked beats unchecked", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked, firewall.CodeSSHUnchecked)), quiet,
			want{band: risk.Critical, score: 80, forced: true}},
		{"ufw may block ssh at start", with(rep(idFW, engine.NotConfigured, "enabled, not running", "UFW_SSH_NOT_ALLOWED_AT_START")), quiet,
			want{band: risk.Moderate, score: 70, factors: []string{"SSH may be blocked after restart"}}},
		{"ufw may block ssh at start, a reboot pending", with(rep(idFW, engine.NotConfigured, "enabled, not running", "UFW_SSH_NOT_ALLOWED_AT_START")),
			pending(detect.Yes), want{band: risk.Moderate, score: 60, factors: []string{"SSH may be blocked after restart", "Reboot pending"}}},
		{"ssh through the firewall unchecked", with(rep(idFW, engine.NotConfigured, "enabled, not running", firewall.CodeSSHUnchecked)), quiet,
			want{band: risk.Low, score: 80, factors: []string{"Can't tell if it blocks SSH"}}},

		// The users' exposure.
		{"second root account", with(rep(idUser, engine.Configured, "non-root, sudo", users.CodeUID0)), quiet,
			want{band: risk.Low, score: 85, factors: []string{"Second root account"}}},
		{"no password to root: keeps the top band out", with(rep(idUser, engine.Configured, "non-root, sudo", users.CodeNoPassword, users.CodeNoPassword)), quiet,
			want{band: risk.Low, score: 95, factors: []string{"No password to become root"}}},
		// A grant the row does not establish costs as one without a
		// password: never Minimal, never above the grant read.
		{"a grant not read: as no password", with(rep(idUser, engine.Configured, "non-root, sudo", users.CodeRuleNotRead)), quiet,
			want{band: risk.Low, score: 95, factors: []string{"Maybe no password to become root"}}},
		{"a grant not read and one read: one factor", with(rep(idUser, engine.Configured, "non-root, sudo", users.CodeRuleNotRead, users.CodeNoPassword)), quiet,
			want{band: risk.Low, score: 95, factors: []string{"No password to become root"}}},
		{"a grant not read on an Unknown row: the Unknown factor", with(rep(idUser, engine.Unknown, "can't tell who has sudo", users.CodeRuleNotRead)), quiet,
			want{band: risk.Low, score: 90, factors: []string{unread}}},

		// The pending reboot.
		{"reboot pending", hardened(), pending(detect.Yes, newerKernel), want{band: risk.Low, score: 90, factors: []string{"Reboot pending"}}},
		{"reboot not established", hardened(), pending(detect.Unknown), want{band: risk.Low, score: 90, factors: []string{"Can't tell if reboot pending"}}},
		{"container: a package's request counts", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost)), inContainer(detect.Yes),
			want{band: risk.Low, score: 90, factors: []string{"Reboot pending"}}},
		{"container: no source is not counted", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost)), inContainer(detect.Unknown),
			want{band: risk.Minimal, score: 100}},
		{"the zero header establishes nothing", hardened(), header.Header{}, want{band: risk.Low, score: 90, factors: []string{"Can't tell if reboot pending"}}},
		{"the zero header, no auto updates", figma(), header.Header{}, want{band: risk.Moderate, score: 50,
			factors: []string{"Can't tell if reboot pending", "Uptime unknown"}}},

		// The long uptime without automatic updates.
		{"up 30 days, no updates", with(rep(idUpdate, engine.NotConfigured, updates.DetailTimerOff)), uptimeOf(30 * day),
			want{band: risk.Moderate, score: 70, factors: []string{"Up 30 days, no updates"}}},
		{"just under 30 days", with(rep(idUpdate, engine.NotConfigured, updates.DetailTimerOff)), uptimeOf(30*day - time.Minute),
			want{band: risk.Low, score: 80}},
		{"up long, updates configured", hardened(), uptimeOf(figmaUptime), want{band: risk.Minimal, score: 100}},
		{"up long, updates off after restart", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart)), uptimeOf(figmaUptime),
			want{band: risk.Low, score: 90}},
		// Off after restart with a fact left open: whether it upgrades is
		// not established, so "Up 182 days" alone, and no finding (risk.md
		// §6.3).
		{"up long, off after restart with a schedule not established", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeScheduleUnknown)),
			uptimeOf(figmaUptime), want{band: risk.Moderate, score: 70, factors: []string{"Can't tell if fully on", "Up 182 days"}}},
		{"up long, off after restart with a value not ranked", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeAptValueUnknown)),
			uptimeOf(figmaUptime), want{band: risk.Moderate, score: 70, factors: []string{"Can't tell if fully on", "Up 182 days"}}},
		{"up long, lists not refreshed", with(rep(idUpdate, engine.Partial, updates.DetailListsStale)), uptimeOf(figmaUptime),
			want{band: risk.Moderate, score: 70, factors: []string{"Up 182 days, no updates"}}},
		{"up long, lists not refreshed, a fact open", with(rep(idUpdate, engine.Partial, updates.DetailListsStale, updates.CodeAptValueUnknown)),
			uptimeOf(figmaUptime), want{band: risk.Moderate, score: 70, factors: []string{"Up 182 days, no updates"}}},
		{"up long, a Partial line not known here", with(rep(idUpdate, engine.Partial, "x")), uptimeOf(figmaUptime),
			want{band: risk.Low, score: 80, factors: []string{"Up 182 days"}}},
		{"up long, updates unknown", with(rep(idUpdate, engine.Unknown, updates.DetailCantRead)), uptimeOf(figmaUptime),
			want{band: risk.Moderate, score: 70, factors: []string{unread, "Up 182 days"}}},
		{"up long, no updates report", without(idUpdate), uptimeOf(figmaUptime),
			want{band: risk.Moderate, score: 70, factors: []string{unread, "Up 182 days"}}},
		{"up long, Proxmox not upgraded", with(rep(idUpdate, engine.Configured, updates.DetailUnattended, updates.CodeProxmoxNotUpgraded)),
			uptimeOf(figmaUptime), want{band: risk.Low, score: 90, factors: []string{"Up 182 days, Proxmox not upgraded"}}},
		{"up long, updates not applicable", with(rep(idUpdate, engine.NotApplicable, "x")), uptimeOf(figmaUptime), want{band: risk.Minimal, score: 100}},
		{"up long in a container", with(rep(idAA, engine.NotApplicable, apparmor.DetailByHost), rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended)),
			func() header.Header { h := inContainer(detect.No); h.Uptime.Duration = figmaUptime; return h }(),
			want{band: risk.Low, score: 100 * 70.0 / 90}},
		{"uptime not read, updates on", hardened(), pending(detect.No), want{band: risk.Minimal, score: 100}},

		// The penalties add up, and the score stays at 0 or more.
		{"every penalty", []engine.Report{rep(idUser, engine.NotConfigured, "root only", users.CodeUID0)}, header.Header{Reboot: header.Reboot{Pending: detect.Yes}},
			want{band: risk.Critical, score: 0, factors: append(unreads(6), "Second root account", "Reboot pending", "Uptime unknown")}},

		// What the score does not weigh.
		{"a control the score does not weigh", append(hardened(), rep("dns.servers", engine.NotConfigured, "x")), quiet, want{band: risk.Minimal, score: 100}},
		{"a row's first report counts", append(hardened(), rep(idFW, engine.NotConfigured, "inactive")), quiet, want{band: risk.Minimal, score: 100}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := risk.Assess(c.reports, c.h)
			var factors []string
			for _, f := range l.Factors {
				factors = append(factors, f.Detail)
			}
			if l.Band != c.want.band || l.Forced != c.want.forced || !near(l.Score, c.want.score) || !slices.Equal(factors, c.want.factors) {
				t.Errorf("got  %v forced %v score %v factors %q\nwant %v forced %v score %v factors %q",
					l.Band, l.Forced, l.Score, factors, c.want.band, c.want.forced, c.want.score, c.want.factors)
			}
		})
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

// TestFigma: Figma's 00-TUI-MAIN host is Moderate ("Moderate ≈ medium
// security"), with the long uptime its one factor, under Auto Updates, and
// its one finding, which keeps the JSON's words (risk.md §6.3).
func TestFigma(t *testing.T) {
	l := risk.Assess(figma(), uptimeOf(figmaUptime))
	if l.Band != risk.Moderate {
		t.Fatalf("band %v, want moderate", l.Band)
	}
	want := []engine.Finding{{Code: risk.CodeUptimeNoUpdates, Detail: "up 182 days, no auto updates", Source: "/proc/uptime"}}
	if !slices.Equal(l.Findings, want) {
		t.Errorf("findings %+v, want %+v", l.Findings, want)
	}
	if f := l.Factors; len(f) != 1 || f[0] != (risk.Factor{Kind: risk.KindUptime, Control: idUpdate, Detail: "Up 182 days, no updates",
		Established: true, Penalty: risk.PenaltyUptime}) {
		t.Errorf("factors %+v", f)
	}
}

// aptRow is the Auto Updates row as the real check reports it on a
// Debian 13 host whose unattended-upgrades is installed and on, with
// systemctl's answers from units by command (absent: a timer active and
// enabled, a service static). The host is hostfake's: nothing runs.
func aptRow(t *testing.T, units map[string]string) engine.Report {
	t.Helper()
	f := hostfake.New(host.Options{})
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case cmd == "apt-config dump APT::Periodic Unattended-Upgrade::Origins-Pattern Unattended-Upgrade::Allowed-Origins":
			return hostfake.Response{Stdout: "APT::Periodic \"\";\nAPT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n" +
				"Unattended-Upgrade::Origins-Pattern \"\";\nUnattended-Upgrade::Origins-Pattern:: \"origin=Debian,codename=${distro_codename},label=Debian-Security\";\n"}
		case cmd == "dpkg-query --show --showformat=${Status}\\t${Version}\\n unattended-upgrades":
			return hostfake.Response{Stdout: "install ok installed\t2.12\n"}
		case strings.HasPrefix(cmd, "systemctl is-"):
			st, ok := units[cmd]
			switch {
			case ok:
			case argv[1] == "is-active":
				st = "active"
			case strings.HasSuffix(cmd, ".service"):
				st = "static"
			default:
				st = "enabled"
			}
			code := 0 // enabled, enabled-runtime, active, static
			switch st {
			case "disabled":
				code = 1
			case "inactive":
				code = 3
			}
			return hostfake.Response{Stdout: st + "\n", ExitCode: code}
		}
		t.Errorf("unscripted: %s", cmd)
		return hostfake.Response{ExitCode: 127}
	}
	facts := detect.Facts{
		OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}
	st, err := updates.Auto{}.Check(context.Background(), f, facts)
	return engine.Report{Control: idUpdate, Title: "Auto Updates", Status: st, Err: err}
}

// TestUpdatesFromTheCheck scores the Auto Updates row as the real check
// reports it: upgrades and list refreshes running now, a timer off after
// restart, and apt-daily-upgrade.timer "enabled-runtime", which the check
// does not rank. Whether the host keeps upgrading is not established, so
// the row earns nothing and the long uptime makes no finding.
func TestUpdatesFromTheCheck(t *testing.T) {
	r := aptRow(t, map[string]string{
		"systemctl is-enabled apt-daily-upgrade.timer": "enabled-runtime",
		"systemctl is-enabled apt-daily.timer":         "disabled",
	})
	if r.Err != nil || r.Status.State != engine.Partial || r.Status.Detail != updates.DetailOffAfterRestart ||
		len(r.Status.Findings) != 1 || r.Status.Findings[0].Code != updates.CodeScheduleUnknown {
		t.Fatalf("the check's row: %s %q %+v err %v", r.Status.State, r.Status.Detail, r.Status.Findings, r.Err)
	}
	l := risk.Assess(with(r), uptimeOf(figmaUptime))
	var factors []string
	for _, f := range l.Factors {
		factors = append(factors, f.Detail)
	}
	if want := []string{"Can't tell if fully on", "Up 182 days"}; l.Band != risk.Moderate || !near(l.Score, 70) ||
		!slices.Equal(factors, want) || len(l.Findings) != 0 {
		t.Errorf("got %v %v %q %+v, want moderate 70 %q and no finding", l.Band, l.Score, factors, l.Findings, want)
	}
	// Both timers enabled: the same row the check calls Configured.
	if l := risk.Assess(with(aptRow(t, nil)), uptimeOf(figmaUptime)); l.Band != risk.Minimal {
		t.Errorf("stock: %v %v %+v", l.Band, l.Score, l.Factors)
	}
}

// userHost is a Debian 13 host for the users check: alice in sudo, bob a
// second account with a login shell, sudo installed and setuid root
// (setuidSudo), top before the %sudo line and dropIns in /etc/sudoers.d
// (name to content). The host is hostfake's: visudo -c is answered,
// nothing runs.
func userHost(t *testing.T, top string, dropIns map[string]string) (host.System, detect.Facts) {
	t.Helper()
	f := hostfake.New(host.Options{})
	for _, d := range []string{"/etc", "/bin", "/usr/bin"} {
		f.AddDir(d, 0o755, 0, 0)
	}
	f.AddDir("/etc/sudoers.d", 0o750, 0, 0)
	f.AddFile("/bin/bash", nil, 0o755, 0, 0)
	f.AddFile("/usr/bin/sudo", nil, 0o755, 0, 0)
	files := map[string]string{
		"/etc/passwd":  "root:x:0:0:root:/root:/bin/bash\nalice:x:1000:1000::/home/alice:/bin/bash\nbob:x:1001:1001::/home/bob:/bin/bash\n",
		"/etc/group":   "root:x:0:\nsudo:x:27:alice\nalice:x:1000:\nbob:x:1001:\n",
		"/etc/sudoers": top + "root ALL=(ALL:ALL) ALL\n%sudo ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n",
		"/etc/shadow":  "root:*:20000:0:99999:7:::\nalice:*:20000:0:99999:7:::\nbob:*:20000:0:99999:7:::\n", // no lock decides (next/p4-locked)
	}
	for name, data := range dropIns {
		files["/etc/sudoers.d/"+name] = data
	}
	for p, data := range files {
		f.AddFile(p, []byte(data), 0o440, 0, 0)
	}
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if slices.Equal(argv, []string{"visudo", "-c"}) {
			return hostfake.Response{Stdout: "/etc/sudoers: parsed OK\n"}
		}
		t.Errorf("unscripted: %q", argv)
		return hostfake.Response{ExitCode: 127}
	}
	return setuidSudo{f}, detect.Facts{
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Proxmox:  detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal,
			Doas: detect.Tool{Present: detect.No}},
	}
}

// userRow is the User row as the real check reports it on userHost's
// host.
func userRow(t *testing.T, top string, dropIns map[string]string) engine.Report {
	t.Helper()
	sys, facts := userHost(t, top, dropIns)
	st, err := users.Admin{}.Check(context.Background(), sys, facts)
	return engine.Report{Control: idUser, Title: "User", Status: st, Err: err}
}

// hardnGrant is the drop-in sudoers' writer plans for bob's grant with a
// password on userHost's host, name to content: hardn's own line.
func hardnGrant(t *testing.T) map[string]string {
	t.Helper()
	sys, facts := userHost(t, "", nil)
	st, err := sudoers.Read(context.Background(), sys, sudoers.Choose(facts, sudoers.ToolSudo), []string{"bob"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := sudoers.PlanFor(st, []sudoers.Policy{{User: "bob", Grant: sudoers.GrantPassword}})
	if err != nil || len(plan.Changes) != 1 {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	return map[string]string{path.Base(plan.Changes[0].Path): string(plan.Changes[0].Data)}
}

// setuidSudo shows /usr/bin/sudo setuid root, as installed: the fake has
// no setuid bit.
type setuidSudo struct{ *hostfake.System }

func (s setuidSudo) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	fi, err := s.System.Stat(ctx, name)
	if err == nil && name == "/usr/bin/sudo" {
		fi = setuidInfo{fi}
	}
	return fi, err
}

type setuidInfo struct{ fs.FileInfo }

func (i setuidInfo) Mode() fs.FileMode { return i.FileInfo.Mode() | fs.ModeSetuid }

// TestUsersFromTheCheck scores the User row as the real check reports it,
// in the risk review's cases: alice in sudo, and the same password-free
// grant for bob read, on a continued line, by an alias and in a list;
// and, from the review of next/p4-users-grants, such a grant before a
// later one for bob as root alone (sudo's "(ALL)", hardn's own line),
// which still decides "sudo -u root -g bob". A grant the check does not
// read is never Minimal, and never scores above the same grant read.
func TestUsersFromTheCheck(t *testing.T) {
	const grant = "bob ALL=(ALL) NOPASSWD: ALL\n"
	const alias = "User_Alias ADMINS = bob\nADMINS ALL=(ALL) NOPASSWD: ALL\n"
	zz := func(rules string) map[string]string { return map[string]string{"zz-site": rules} }
	read := risk.Assess(with(userRow(t, "", zz(grant))), quiet)
	noPass := want{band: risk.Low, score: 95, factors: []string{"No password to become root"}}
	notRead := want{band: risk.Low, score: 95, factors: []string{"Maybe no password to become root"}}
	for _, c := range []struct {
		name    string
		top     string
		dropIns map[string]string
		state   engine.State
		codes   []string // the User row's findings
		want    want
	}{
		{"nothing else", "", nil, engine.Configured, nil, want{band: risk.Minimal, score: 100}},
		{"read", "", zz(grant), engine.Configured, []string{users.CodeNoPassword}, noPass},
		{"a continued line", "", zz("bob ALL=(ALL) \\\n  NOPASSWD: ALL\n"), engine.Configured, []string{users.CodeRuleNotRead}, notRead},
		{"an alias", alias, nil, engine.Configured, []string{users.CodeRuleNotRead}, notRead},
		{"a list", "bob, george ALL=(ALL) NOPASSWD: ALL\n", nil, engine.Configured, []string{users.CodeRuleNotRead}, notRead},
		{"an alias after alice's grant", "", zz(alias), engine.Unknown, []string{users.CodeRuleNotRead, users.CodeRuleNotRead}, want{band: risk.Low, score: 90, factors: []string{unread}}},
		{"a list, then bob as root", "bob, george ALL=(ALL:ALL) NOPASSWD: ALL\n", zz("bob ALL=(ALL) ALL\n"), engine.Configured, []string{users.CodeRuleNotRead}, notRead},
		{"a continued line, then bob as root", "", map[string]string{"10-bob": "bob ALL=(ALL:ALL) \\\n  NOPASSWD: ALL\n", "90-bob": "bob ALL=(ALL) ALL\n"},
			engine.Configured, []string{users.CodeRuleNotRead}, notRead},
		{"read, then hardn's grant", "", merge(map[string]string{"10-bob": "bob ALL=(ALL:ALL) NOPASSWD: ALL\n"}, hardnGrant(t)), engine.Configured, []string{users.CodeNoPassword}, noPass},
		{"a list, then bob with any group", "bob, george ALL=(ALL:ALL) NOPASSWD: ALL\n", zz("bob ALL=(ALL:ALL) ALL\n"), engine.Configured, nil, want{band: risk.Minimal, score: 100}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := userRow(t, c.top, c.dropIns)
			var codes []string
			for _, f := range r.Status.Findings {
				codes = append(codes, f.Code)
			}
			if r.Err != nil || r.Status.State != c.state || !slices.Equal(codes, c.codes) {
				t.Fatalf("the check's row: %s %q %v err %v", r.Status.State, r.Status.Detail, r.Status.Findings, r.Err)
			}
			l := risk.Assess(with(r), quiet)
			var factors []string
			for _, f := range l.Factors {
				factors = append(factors, f.Detail)
			}
			if l.Band != c.want.band || !near(l.Score, c.want.score) || !slices.Equal(factors, c.want.factors) {
				t.Errorf("got %v %v %q, want %v %v %q", l.Band, l.Score, factors, c.want.band, c.want.score, c.want.factors)
			}
			if slices.Contains(codes, users.CodeRuleNotRead) && (l.Band == risk.Minimal || l.Band > read.Band || l.Score > read.Score) {
				t.Errorf("a grant not read: %v %v, above the grant read: %v %v", l.Band, l.Score, read.Band, read.Score)
			}
		})
	}
}

// merge is the files of ms, later ones replacing earlier.
func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		maps.Copy(out, m)
	}
	return out
}

// TestFindings: the cross-checks' findings, and only theirs: the header's
// on a reboot pending, the uptime's once established; none for what is
// not established, nor the reports' own.
func TestFindings(t *testing.T) {
	uptime := engine.Finding{Code: risk.CodeUptimeNoUpdates, Detail: "up 182 days, no auto updates", Source: header.UptimeFile}
	long := uptimeOf(figmaUptime)
	long.Reboot = header.Reboot{Pending: detect.Yes, Findings: []engine.Finding{newerKernel}}
	for _, c := range []struct {
		name    string
		reports []engine.Report
		h       header.Header
		want    []engine.Finding
	}{
		{"reboot and uptime", figma(), long, []engine.Finding{newerKernel, uptime}},
		{"reboot not established", hardened(), pending(detect.Unknown, newerKernel), nil},
		{"updates not established", with(rep(idUpdate, engine.Unknown, updates.DetailCantRead)), uptimeOf(figmaUptime), nil},
		{"updates off after restart, a fact open", with(rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeScheduleUnknown)),
			uptimeOf(figmaUptime), nil},
		{"uptime not established", figma(), pending(detect.No), nil},
		{"the lockout is the report's", with(rep(idFW, engine.Partial, "ssh blocked", firewall.CodeSSHBlocked)), quiet, nil},
	} {
		if got := risk.Assess(c.reports, c.h).Findings; !slices.Equal(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestFactors: each factor's kind, row, words, colour and cost, in Kind
// order.
func TestFactors(t *testing.T) {
	h := uptimeOf(figmaUptime)
	h.Reboot.Pending = detect.Yes
	l := risk.Assess([]engine.Report{
		rep(idUser, engine.Configured, "non-root, sudo", users.CodeNoPassword, users.CodeUID0),
		rep(idFW, engine.NotConfigured, "enabled, not running", "UFW_SSH_NOT_ALLOWED_AT_START", firewall.CodeSSHUnchecked),
		rep(idLogin, engine.Configured, "root disallowed"), rep(idAuth, engine.Configured, "key-only auth"),
		rep(idPort, engine.Unknown, "can't read"), rep(idAA, engine.Configured, "34 profiles enforced"),
		rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended),
	}, h)
	want := []risk.Factor{
		{Kind: risk.KindLockoutAtStart, Control: idFW, Detail: "SSH may be blocked after restart", Established: true, Penalty: risk.PenaltyAtStart},
		{Kind: risk.KindUnknown, Control: idPort, Detail: unread},
		{Kind: risk.KindSecondRoot, Control: idUser, Detail: "Second root account", Established: true, Penalty: risk.PenaltySecondRoot},
		{Kind: risk.KindSSHUnchecked, Control: idFW, Detail: "Can't tell if it blocks SSH"},
		{Kind: risk.KindReboot, Detail: "Reboot pending", Established: true, Penalty: risk.PenaltyReboot},
		{Kind: risk.KindUptime, Control: idUpdate, Detail: "Up 182 days, no updates", Established: true, Penalty: risk.PenaltyUptime},
		{Kind: risk.KindNoPassword, Control: idUser, Detail: "No password to become root", Established: true, Penalty: risk.PenaltyNoPassword},
	}
	if !slices.Equal(l.Factors, want) {
		t.Errorf("got  %+v\nwant %+v", l.Factors, want)
	}
	// User 10, SSH Login 15, SSH Auth 20 and AppArmor 10 earned, less
	// 10 + 15 + 10 + 10 + 5.
	if l.Band != risk.Critical || !near(l.Score, 5) {
		t.Errorf("band %v score %v, want critical 5", l.Band, l.Score)
	}
}

// everyLine is every line of risk.md §6.2, each kind's, on hosts up 9999
// days, the longest uptime the header shows.
func everyLine() []risk.Factor {
	var all []risk.Factor
	long := uptimeOf(9999*day + 23*time.Hour)
	for _, u := range []engine.Report{
		rep(idUpdate, engine.NotConfigured, updates.DetailNoUnattended),
		rep(idUpdate, engine.Unknown, updates.DetailCantRead),
		rep(idUpdate, engine.Configured, updates.DetailUnattended, updates.CodeProxmoxNotUpgraded),
		rep(idUpdate, engine.Partial, updates.DetailOffAfterRestart, updates.CodeScheduleUnknown),
	} {
		all = append(all, risk.Assess(with(u), long).Factors...)
	}
	all = append(all, risk.Assess([]engine.Report{
		rep(idUser, engine.Configured, "x", users.CodeNoPassword, users.CodeUID0),
		rep(idFW, engine.NotConfigured, "x", "UFW_SSH_NOT_ALLOWED_AT_START", firewall.CodeSSHUnchecked),
	}, header.Header{}).Factors...)
	all = append(all, risk.Assess(hardened(), pending(detect.Yes)).Factors...)
	return append(all, risk.Assess(with(rep(idUser, engine.Configured, "x", users.CodeRuleNotRead)), quiet).Factors...)
}

// TestFactorsFit: each line fits the 35 cells it has after the bullet and
// its space at the value column at 64 columns (64 − 27 − 2; DEV-059), the
// longest uptime the header shows included.
func TestFactorsFit(t *testing.T) {
	kinds := map[risk.Kind]bool{}
	for _, f := range everyLine() {
		kinds[f.Kind] = true
		if n := len([]rune(f.Detail)); n > 35 {
			t.Errorf("%q: %d cells, over 35", f.Detail, n)
		}
	}
	if len(kinds) != 8 {
		t.Errorf("%d kinds checked, want 8", len(kinds))
	}
}

// TestFactorLines: every line of risk.md §6.2 in its colour, status.warn
// for a cost hardn established (Established) and text.muted for one that
// rests on something hardn could not establish, and in its place: under
// the main menu's row its kind is about, an Unknown row's under that row,
// and only the pending reboot in the host block (Control ""). Every kind
// and every line is checked, so no factor is left without a place.
func TestFactorLines(t *testing.T) {
	warn := map[string]bool{
		"SSH may be blocked after restart":   true,
		unread:                               false,
		"Second root account":                true,
		"Can't tell if it blocks SSH":        false,
		"Can't tell if fully on":             false,
		"Reboot pending":                     true,
		"Can't tell if reboot pending":       false,
		"Up 9999 days, no updates":           true,
		"Up 9999 days, Proxmox not upgraded": true,
		"Up 9999 days":                       false,
		"Uptime unknown":                     false,
		"No password to become root":         true,
		"Maybe no password to become root":   false,
	}
	under := map[risk.Kind]string{risk.KindLockoutAtStart: idFW, risk.KindSecondRoot: idUser, risk.KindSSHUnchecked: idFW,
		risk.KindUpdatesOpen: idUpdate, risk.KindReboot: "", risk.KindUptime: idUpdate, risk.KindNoPassword: idUser}
	shown := map[string]bool{}
	for _, r := range risk.Rows() {
		shown[r.ID] = true
	}
	seen := map[string]bool{}
	for _, f := range everyLine() {
		seen[f.Detail] = true
		if w, ok := warn[f.Detail]; !ok || f.Established != w {
			t.Errorf("%q: established %t, want %t (listed %t)", f.Detail, f.Established, w, ok)
		}
		if f.Kind == risk.KindUnknown {
			if !shown[f.Control] {
				t.Errorf("%q under %q, not a row the main menu shows", f.Detail, f.Control)
			}
		} else if c, ok := under[f.Kind]; !ok || f.Control != c {
			t.Errorf("%s %q under %q, want %q", f.Kind, f.Detail, f.Control, c)
		}
	}
	for d := range warn {
		if !seen[d] {
			t.Errorf("%q: no host made it", d)
		}
	}
}

// TestUnknownLines: a line under each row Unknown, failed or with no
// report, in the rows' order, whatever the reports' order.
func TestUnknownLines(t *testing.T) {
	reports := slices.Concat([]engine.Report{rep(idUpdate, engine.Unknown, "can't read"),
		{Control: idFW, Status: engine.Status{State: engine.Configured}, Err: errors.New("ss")}}, without(idUser, idFW, idUpdate, idPort))
	var got []string
	for _, f := range risk.Assess(reports, quiet).Factors {
		got = append(got, f.Control)
	}
	if want := []string{idUser, idFW, idPort, idUpdate}; !slices.Equal(got, want) {
		t.Errorf("lines under %q, want %q", got, want)
	}
}

// TestRows: the rows are the five areas' controls, once each, in Figma's
// order, their weights 100 together, the port lighter than either auth
// setting, the firewall one row. Engine.Check over the areas' Controls(),
// in this order, gives its reports in Figma's order too.
func TestRows(t *testing.T) {
	var controls []engine.Control
	for _, cs := range [][]engine.Control{users.Controls(), firewall.Controls(), ssh.Controls(), apparmor.Controls(), updates.Controls()} {
		controls = append(controls, cs...)
	}
	rows := risk.Rows()
	if len(rows) != len(controls) {
		t.Fatalf("%d rows for %d controls", len(rows), len(controls))
	}
	weight := map[string]int{}
	sum := 0
	for i, r := range rows {
		if r.ID != controls[i].ID() || r.Title != controls[i].Title() {
			t.Errorf("row %d is %s %q, want %s %q", i, r.ID, r.Title, controls[i].ID(), controls[i].Title())
		}
		if r.Weight <= 0 {
			t.Errorf("%s weighs %d", r.ID, r.Weight)
		}
		weight[r.ID] = r.Weight
		sum += r.Weight
	}
	var titles []string
	for _, r := range rows {
		titles = append(titles, r.Title)
	}
	if want := []string{"User", "Firewall", "SSH Login", "SSH Auth", "SSH Port", "AppArmor", "Auto Updates"}; !slices.Equal(titles, want) {
		t.Errorf("titles %q, want Figma's %q", titles, want)
	}
	if sum != 100 {
		t.Errorf("weights sum to %d, want 100", sum)
	}
	if weight[idPort] >= weight[idLogin] || weight[idPort] >= weight[idAuth] {
		t.Errorf("port %d, login %d, auth %d: the port must weigh less", weight[idPort], weight[idLogin], weight[idAuth])
	}
	if !(0 < risk.HighFrom && risk.HighFrom < risk.ModerateFrom && risk.ModerateFrom < risk.LowFrom && risk.LowFrom < risk.MinimalFrom && risk.MinimalFrom <= 100) {
		t.Error("the bands' bounds are not increasing within 0 to 100")
	}
	rows[0].Weight = 0
	if risk.Rows()[0].Weight == 0 {
		t.Error("Rows shares its table")
	}
}

// TestBandWords: each band's word, and one outside them.
func TestBandWords(t *testing.T) {
	for b, w := range map[risk.Band]string{risk.Unknown: "unknown", risk.Critical: "critical", risk.High: "high",
		risk.Moderate: "moderate", risk.Low: "low", risk.Minimal: "minimal", risk.Band(9): "band(9)"} {
		if b.String() != w {
			t.Errorf("%d: %q, want %q", b, b.String(), w)
		}
	}
	if risk.KindNoPassword.String() != "no-password" || risk.KindUnknown.String() != "unknown" || risk.Kind(9).String() != "kind(9)" {
		t.Error("kind words")
	}
}
