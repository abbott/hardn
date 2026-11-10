package updates_test

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/dnfauto"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestSectionRows: each state of the section the hosts' screens don't
// draw: a job masked or missing, a timer off after restart, Enable "0", a
// record hardn can't read, an "always" interval, crond's jobs unread; and
// a host on but with apt's settings unread is offered Enable, saying so,
// not shown on outside hardn. On Rocky Linux: the install timer off after
// restart, starting after restart, or masked for this boot; automatic.conf
// unread or changed since hardn; rpm unread, drawing no timer row.
func TestSectionRows(t *testing.T) {
	for _, tt := range []struct {
		name  string
		host  func(*testing.T) *model
		shows []string
		not   string
	}{
		{"masked", func(t *testing.T) *model { return newHost(t, "masked") },
			[]string{"Upgrade Timer Masked apt-daily-upgrade.service"}, ""},
		{"missing", func(t *testing.T) *model { return newHost(t, "missing") },
			[]string{"Upgrade Timer Missing apt-daily-upgrade.service"}, ""},
		{"off after restart", func(t *testing.T) *model {
			m := aptHost(t, debian13(), autoOn)
			m.units["is-active apt-daily-upgrade.timer"], m.units["is-enabled apt-daily-upgrade.timer"] = "active", "disabled"
			return m
		}, []string{"Upgrade Timer Off after restart"}, ""},
		{"Enable 0", func(t *testing.T) *model { return newHost(t, "enable-off") },
			[]string{"Periodic Jobs Off by 99site"}, ""},
		{"record unreadable", func(t *testing.T) *model { return newHost(t, "corrupt-record") },
			[]string{"Turned on by Can't read hardn's record"}, ""},
		{"always", func(t *testing.T) *model {
			m := aptHost(t, debian13(), autoOn)
			m.file(confDir+"/99site", "APT::Periodic::Unattended-Upgrade \"always\";\n")
			return m
		}, []string{"Upgrades Each run by 99site"}, ""},
		{"jobs unread", func(t *testing.T) *model {
			m := newModel(t, alpine323())
			m.f.AddSymlink("/etc/periodic/hourly/loop", "/etc/periodic/hourly/loop")
			return m
		}, []string{"Other Jobs Can't read"}, ""},
		{"on, apt's settings unread", func(t *testing.T) *model {
			m := aptHost(t, debian13(), autoOn)
			m.file(confDir+"/99site", "Unattended-Upgrade::Origins-Pattern:: \"origin=Debian\";\n")
			return m
		}, []string{"Configured unattended-upgrades", "1) Enable automatic updates"}, "Turned on outside hardn"},
		{"dnf, off after restart", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.units["is-active "+dnfauto.InstallTimer], m.units["is-enabled "+dnfauto.InstallTimer] = "active", "disabled"
			return m
		}, []string{"Install Timer Off after restart"}, ""},
		{"dnf, starts after restart", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.units["is-enabled "+dnfauto.InstallTimer] = "enabled"
			return m
		}, []string{"Install Timer Starts after restart"}, ""},
		{"dnf, masked for this boot", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.units["is-enabled "+dnfauto.InstallTimer] = "masked-runtime"
			return m
		}, []string{"Install Timer Masked"}, ""},
		{"dnf, automatic.conf unread", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.file(dnfauto.Conf, strings.Replace(dnfConf, "upgrade_type = default", "upgrade_type = all", 1))
			return m
		}, []string{"Updates Can't read"}, "by automatic.conf"},
		{"dnf, automatic.conf changed since hardn", func(t *testing.T) *model {
			m := newHost(t, "by-hardn-rocky")
			data, err := m.f.ReadFile(t.Context(), dnfauto.Conf)
			if err != nil {
				t.Fatal(err)
			}
			m.file(dnfauto.Conf, string(data)+"# an operator's line\n")
			return m
		}, []string{"Updates Security only changed since hardn"}, ""},
		{"dnf, rpm unread", func(t *testing.T) *model {
			m := dnfHost(t, true)
			m.f.Respond(hostfake.Response{Stderr: "error: rpmdb: damaged header\n", ExitCode: 1}, "rpm", "-q", dnfauto.Package)
			return m
		}, []string{"dnf-automatic Can't read"}, "Install Timer"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := play(tt.host(t), false)
			for _, w := range tt.shows {
				if !shows(p.out, w) {
					t.Errorf("no %q in\n%s", w, p.out)
				}
			}
			if tt.not != "" && strings.Contains(p.out, tt.not) {
				t.Errorf("%q in\n%s", tt.not, p.out)
			}
		})
	}
}
