package updates_test

import (
	"strings"
	"testing"
)

// TestSectionRows: each state of the section the hosts' screens don't
// draw: a job masked or missing, a timer off after restart, Enable "0", a
// record hardn can't read, an "always" interval, crond's jobs unread; and
// a host on but with apt's settings unread is offered Enable, saying so,
// not shown on outside hardn.
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
