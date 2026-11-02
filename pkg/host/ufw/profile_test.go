package ufw_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// TestPlanDisable: one command and its undo, on an active ufw whatever its
// rule files hold; nothing on an inactive one; never a reset; nothing run
// on Proxmox VE.
func TestPlanDisable(t *testing.T) {
	ctx := context.Background()
	m := newModel(t)
	m.active = true
	m.rules = []modelRule{{fam: ufw.IPv4, action: "allow", port: "22"}, {fam: ufw.IPv6, action: "allow", port: "22"}}
	h := m.host()
	st, err := ufw.Read(ctx, h, notPVE)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ufw.PlanDisable(st, notPVE)
	if err != nil {
		t.Fatal(err)
	}
	if got := argvs(p.Steps); !slices.Equal(got, []string{"ufw disable"}) {
		t.Errorf("steps %q", got)
	}
	if p.Steps[0].Kind != ufw.StepDisable || p.Steps[0].Kind.String() != "disable" {
		t.Errorf("kind %v", p.Steps[0].Kind)
	}
	if got := fmt.Sprint(p.PreUndo()); got != "[[ufw --force enable]]" {
		t.Errorf("PreUndo %s", got)
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err != nil || m.active {
		t.Fatalf("Execute: %v, active %t", err, m.active)
	}
	if got := undos(rep); !slices.Equal(got, []string{"ufw --force enable"}) {
		t.Errorf("undo %q", got)
	}
	if got := len(m.rules); got != 2 {
		t.Errorf("%d rules left saved, want 2", got)
	}
	for _, r := range runs(h) {
		if strings.Contains(r, "reset") {
			t.Errorf("ran %q", r)
		}
	}
	if st, err = ufw.Read(ctx, h, notPVE); err != nil {
		t.Fatal(err)
	}
	if p, err = ufw.PlanDisable(st, notPVE); err != nil || !p.Empty() {
		t.Errorf("inactive: %q, %v", argvs(p.Steps), err)
	}

	// A ufw with a saved line not read still has a way off.
	broken := ufw.State{Installed: true, Status: ufw.Status{Known: true, Active: true}, Bad: []string{"### tuple ### garbage"}}
	if p, err = ufw.PlanDisable(broken, notPVE); err != nil || p.Empty() {
		t.Errorf("a saved line not read: %q, %v", argvs(p.Steps), err)
	}
	if _, err = ufw.PlanDisable(ufw.State{}, notPVE); !errors.Is(err, ufw.ErrNotInstalled) {
		t.Errorf("not installed: %v", err)
	}
	if _, err = ufw.PlanDisable(ufw.State{Installed: true}, notPVE); !errors.Is(err, ufw.ErrStateUnknown) {
		t.Errorf("status not read: %v", err)
	}
	if _, err = ufw.PlanDisable(ufw.State{}, detect.Proxmox{}); !errors.Is(err, ufw.ErrProxmoxUnknown) {
		t.Errorf("Proxmox not established: %v", err)
	}
	if p, err = ufw.PlanDisable(st, detect.Proxmox{Installed: detect.Yes}); err != nil || !p.NotApplicable || !p.Empty() {
		t.Errorf("Proxmox VE: %+v, %v", p, err)
	}
}

// appHost is m's host answering an application's allow with answer, and the
// rest as the model does.
func appHost(m *model, answer hostfake.Response) *hostfake.System {
	h := m.host()
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		if slices.Contains(argv, "app") {
			return answer
		}
		return m.run(h, argv)
	}
	return h
}

// TestPlanApps: a rule for each name no saved rule has as its application,
// refused on an inactive ufw; Execute's undo reads ufw's answer as it does for
// an allow, so a kept rule is not deleted.
func TestPlanApps(t *testing.T) {
	ctx := context.Background()
	m := newModel(t)
	m.active = true
	m.rules = []modelRule{
		{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 Web - in", row: fmt.Sprintf("%-26s %-12s%-26s", "8080/tcp (Web)", "ALLOW IN", "Anywhere")},
		{fam: ufw.IPv4, tuple: "### tuple ### allow tcp 8081 0.0.0.0/0 any 0.0.0.0/0 Web%20Two - in", row: fmt.Sprintf("%-26s %-12s%-26s", "8081/tcp (Web Two)", "ALLOW IN", "Anywhere")},
	}
	h := appHost(m, hostfake.Response{Stdout: "Rule added\nRule added (v6)\n"})
	st, err := ufw.Read(ctx, h, notPVE)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Bad) > 0 {
		t.Fatalf("not read: %q", st.Bad)
	}
	names := []string{"Web", "Mail", "Web Two", "Mail"}
	p, err := ufw.PlanApps(st, names, notPVE)
	if err != nil {
		t.Fatal(err)
	}
	if got := argvs(p.Steps); !slices.Equal(got, []string{"ufw allow from any to any app Mail"}) {
		t.Errorf("steps %q", got)
	}
	if len(p.PreUndo()) != 0 {
		t.Errorf("PreUndo %q: an allow's undo depends on ufw's answer", p.PreUndo())
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err != nil || !rep.Verified {
		t.Fatalf("Execute: %v, Verified %t (%s)", err, rep.Verified, rep.Unverified)
	}
	if got := undos(rep); !slices.Equal(got, []string{"ufw delete allow from any to any app Mail"}) {
		t.Errorf("undo %q", got)
	}

	// A rule ufw kept is not deleted, nor one whose answer does not show it added it.
	for _, answer := range []string{"Skipping adding existing rule\nSkipping adding existing rule (v6)\n", "Rule added\nSkipping adding existing rule (v6)\n", "WARN: something\n"} {
		rep, err = ufw.Execute(ctx, appHost(m, hostfake.Response{Stdout: answer}), p)
		if err != nil || len(rep.Undo) != 0 || rep.Steps[0].Note == "" {
			t.Errorf("%q: %v, undo %q, note %q", answer, err, undos(rep), rep.Steps[0].Note)
		}
	}
	// A failed allow is not undone either.
	rep, err = ufw.Execute(ctx, appHost(m, hostfake.Response{Stderr: "ERROR: Could not find a profile matching 'Mail'\n", ExitCode: 1}), p)
	if err == nil || len(rep.Undo) != 0 {
		t.Errorf("failed: %v, undo %q", err, undos(rep))
	}

	inactive, err := ufw.Read(ctx, newModel(t).host(), notPVE)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := newModel(t)
	mismatch.active, mismatch.mismatch = true, true
	off, err := ufw.Read(ctx, mismatch.host(), notPVE)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		st    ufw.State
		names []string
		pve   detect.Proxmox
		want  error
	}{
		{"inactive", inactive, names, notPVE, ufw.ErrInactive},
		{"loaded, not enabled", off, names, notPVE, ufw.ErrEnabledMismatch},
		{"not installed", ufw.State{}, names, notPVE, ufw.ErrNotInstalled},
		{"status not read", ufw.State{Installed: true}, names, notPVE, ufw.ErrStateUnknown},
		{"rules not read", ufw.State{Installed: true, Enabled: true, Status: ufw.Status{Known: true, Active: true}, Bad: []string{"x"}}, names, notPVE, ufw.ErrStateUnknown},
		{"a bad name", st, []string{"-x"}, notPVE, ufw.ErrBadProfile},
		{"Proxmox not established", st, names, detect.Proxmox{}, ufw.ErrProxmoxUnknown},
	} {
		if _, err := ufw.PlanApps(tt.st, tt.names, tt.pve); !errors.Is(err, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, err, tt.want)
		}
	}
	if p, err = ufw.PlanApps(st, names, detect.Proxmox{Installed: detect.Yes}); err != nil || !p.NotApplicable {
		t.Errorf("Proxmox VE: %+v, %v", p, err)
	}
}

// TestValidProfile holds ufw 0.36.2's own rules (valid_profile_name,
// verify_profile), and the Firewall screen's port form.
func TestValidProfile(t *testing.T) {
	ok := ufw.Profile{Name: "Web", Title: "Web Server", Description: "A web server", Ports: []string{"80/tcp"}}
	with := func(f func(*ufw.Profile)) ufw.Profile {
		p := ok
		f(&p)
		return p
	}
	for _, name := range []string{"Web", "web server", "a_b-c.d+e", "1a", "0x10", "1 2"} {
		if err := ufw.ValidProfile(with(func(p *ufw.Profile) { p.Name = name })); err != nil {
			t.Errorf("name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", "all", "22", "1_000", "1 ", " web", "-web", "we/b", "a\nb", "web\n", "wéb"} {
		if err := ufw.ValidProfile(with(func(p *ufw.Profile) { p.Name = name })); !errors.Is(err, ufw.ErrBadProfile) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	for _, port := range []string{"80/tcp", "53/udp", "1/tcp", "65535/udp", "1:65535/udp", "6000:6007/tcp"} {
		if err := ufw.ValidProfile(with(func(p *ufw.Profile) { p.Ports = []string{port} })); err != nil {
			t.Errorf("port %q: %v", port, err)
		}
	}
	for _, port := range []string{"", "80", "0/tcp", "65536/tcp", "99999/tcp", "90:80/tcp", "80:80/tcp", "80/any", "80,443/tcp", "80/tcp|443/tcp", "http/tcp", "80/tcp\ntitle=x"} {
		if err := ufw.ValidProfile(with(func(p *ufw.Profile) { p.Ports = []string{"443/tcp", port} })); !errors.Is(err, ufw.ErrBadProfile) {
			t.Errorf("port %q: %v", port, err)
		}
	}
	for name, p := range map[string]ufw.Profile{
		"no title":          with(func(p *ufw.Profile) { p.Title = " " }),
		"no description":    with(func(p *ufw.Profile) { p.Description = "" }),
		"a line in a title": with(func(p *ufw.Profile) { p.Title = "a\nports=1/tcp" }),
		"a line break":      with(func(p *ufw.Profile) { p.Description = "a\r\nb" }),
		"no port":           with(func(p *ufw.Profile) { p.Ports = nil }),
	} {
		if err := ufw.ValidProfile(p); !errors.Is(err, ufw.ErrBadProfile) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestProfileBytes: each profile as openssh-server's file has it, its ports
// joined by "|", as ufw splits them (applications.py:187).
func TestProfileBytes(t *testing.T) {
	got := string(ufw.ProfileBytes([]ufw.Profile{
		{Name: "Web", Title: "Web Server", Description: "A web server", Ports: []string{"80/tcp", "443/tcp"}},
		{Name: "DNS", Title: "DNS", Description: "Names", Ports: []string{"53/udp"}},
	}))
	want := "[Web]\ntitle=Web Server\ndescription=A web server\nports=80/tcp|443/tcp\n\n[DNS]\ntitle=DNS\ndescription=Names\nports=53/udp\n\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if ufw.ProfileFile != "/etc/ufw/applications.d/hardn" {
		t.Errorf("ProfileFile %s", ufw.ProfileFile)
	}
}

// TestReadEnabled reads ufw.conf alone, in the form Debian 13's and Ubuntu
// 26.04's have it, with no ufw call and no Proxmox fact.
func TestReadEnabled(t *testing.T) {
	ctx := context.Background()
	const stock = "# /etc/ufw/ufw.conf\n#\n\n# Set to yes to start on boot. If setting this remotely, be sure to add a rule\n" +
		"# to allow your remote connection before starting ufw. Eg: 'ufw allow 22/tcp'\nENABLED=%s\n\n" +
		"# Please use the 'ufw' command to set the loglevel. Eg: 'ufw logging medium'.\n# See 'man ufw' for details.\nLOGLEVEL=low\n"
	for _, tt := range []struct {
		enabled string
		want    detect.Tri
	}{{"yes", detect.Yes}, {"no", detect.No}, {`"yes"`, detect.Yes}, {"YES", detect.Yes}} {
		m := newModel(t)
		m.conf = fmt.Sprintf(stock, tt.enabled)
		h := m.host()
		h.Handler = func(argv []string, _ []byte) hostfake.Response {
			t.Errorf("ran %q", argv)
			return hostfake.Response{}
		}
		if got, err := ufw.ReadEnabled(ctx, h); err != nil || got != tt.want {
			t.Errorf("ENABLED=%s: %v, %v", tt.enabled, got, err)
		}
		if len(h.Runs()) != 0 {
			t.Errorf("ran %q", h.Runs())
		}
	}
	h := hostfake.New(host.Options{})
	if got, err := ufw.ReadEnabled(ctx, h); err != nil || got != detect.No {
		t.Errorf("no file: %v, %v", got, err)
	}
	h.AddDir(ufw.ConfFile, 0o755, 0, 0) // a read that fails, not one of a missing file
	if got, err := ufw.ReadEnabled(ctx, h); err == nil || got != detect.Unknown {
		t.Errorf("a failed read: %v, %v", got, err)
	}
}

// TestReadBoot: systemctl is-enabled's answers on systemd, as Debian 13 and
// Ubuntu 26.04 print them; the runlevels on OpenRC.
func TestReadBoot(t *testing.T) {
	ctx := context.Background()
	systemd := detect.Facts{Init: detect.Init{Kind: detect.Systemd}}
	for answer, want := range map[string]detect.Tri{
		"enabled": detect.Yes, "disabled": detect.No, "masked": detect.No, "enabled-runtime": detect.No,
		"static": detect.Unknown, "not-found": detect.Unknown, "garbage": detect.Unknown,
	} {
		h := hostfake.New(host.Options{})
		h.Handler = func(argv []string, _ []byte) hostfake.Response {
			if strings.Join(argv, " ") != "systemctl is-enabled ufw.service" {
				t.Errorf("ran %q", argv)
			}
			exit := 0
			if answer != "enabled" {
				exit = 1
			}
			return hostfake.Response{Stdout: answer + "\n", ExitCode: exit}
		}
		if got := ufw.ReadBoot(ctx, h, systemd); got != want {
			t.Errorf("%s: %v, want %v", answer, got, want)
		}
	}
	h := hostfake.New(host.Options{})
	h.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{Stderr: "Failed to connect to bus\n", ExitCode: 1} }
	if got := ufw.ReadBoot(ctx, h, systemd); got != detect.Unknown {
		t.Errorf("no bus: %v", got)
	}

	openrc := detect.Facts{Init: detect.Init{Kind: detect.OpenRC}}
	h = hostfake.New(host.Options{})
	if got := ufw.ReadBoot(ctx, h, openrc); got != detect.No {
		t.Errorf("no runlevels: %v", got)
	}
	h.AddDir(detect.OpenRCRunlevels+"/default", 0o755, 0, 0)
	if got := ufw.ReadBoot(ctx, h, openrc); got != detect.No {
		t.Errorf("in no runlevel: %v", got)
	}
	h.AddFile(detect.OpenRCRunlevels+"/default/ufw", nil, 0o644, 0, 0)
	if got := ufw.ReadBoot(ctx, h, openrc); got != detect.Yes {
		t.Errorf("in default: %v", got)
	}
	if got := ufw.ReadBoot(ctx, h, detect.Facts{}); got != detect.Unknown {
		t.Errorf("no init: %v", got)
	}
}
