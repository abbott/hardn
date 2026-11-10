package dnfauto_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dnfauto"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// stock is dnf-automatic 4.20.0's automatic.conf, as rocky10-test-110's
// rpm holds it (probe 5, 2026-11-10).
func stock(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/automatic.conf")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestParse: the stock file is every update, not applied, never
// restarting; the forms dnf takes (upgrade_type and reboot exactly,
// apply_updates in any case); a value it doesn't, a key twice, or a key in
// another case is ErrConf; a key outside [commands] doesn't count.
func TestParse(t *testing.T) {
	if s, err := dnfauto.Parse(stock(t)); s != (dnfauto.Settings{UpgradeType: "default", Reboot: dnfauto.RebootNever}) || err != nil {
		t.Errorf("stock: %+v, %v", s, err)
	}
	for conf, want := range map[string]dnfauto.Settings{
		"[commands]\nupgrade_type = security\napply_updates = yes\n":    {dnfauto.Security, true, dnfauto.RebootNever},
		"[commands]\nupgrade_type=security\napply_updates=On\n":         {dnfauto.Security, true, dnfauto.RebootNever},
		"[commands]\n# upgrade_type = security\napply_updates = 0\n":    {"default", false, dnfauto.RebootNever},
		"[emitters]\nupgrade_type = security\nreboot = x\n[commands]\n": {"default", false, dnfauto.RebootNever},
		"":                                                 {"default", false, dnfauto.RebootNever},
		"[commands]\napply_updates = true":                 {"default", true, dnfauto.RebootNever},
		"[commands]\napply_updates = 1\n":                  {"default", true, dnfauto.RebootNever},
		"[commands]\napply_updates = FALSE\n":              {"default", false, dnfauto.RebootNever},
		"[commands]\napply_updates = off\n":                {"default", false, dnfauto.RebootNever},
		"[commands]\nreboot = when-needed\n":               {"default", false, dnfauto.RebootWhenNeeded},
		"[commands]\nreboot = when-changed\n":              {"default", false, dnfauto.RebootWhenChanged},
		"[commands]\nreboot = never\nreboot_command = x\n": {"default", false, dnfauto.RebootNever},
	} {
		if s, err := dnfauto.Parse([]byte(conf)); s != want || err != nil {
			t.Errorf("%q: %+v, %v", conf, s, err)
		}
	}
	for _, conf := range []string{"[commands]\nupgrade_type = all\n", "[commands]\nupgrade_type=Security\n", "[commands]\napply_updates = maybe\n",
		"[commands]\nupgrade_type = default\nupgrade_type = security\n", "[commands]\nUpgrade_Type = security\n", "[commands]\nApply_updates = yes\n",
		"[commands]\nreboot = always\n", "[commands]\nreboot = When-Needed\n", "[commands]\nREBOOT = never\n", "[commands]\nreboot = never\nreboot = never\n"} {
		if _, err := dnfauto.Parse([]byte(conf)); !errors.Is(err, dnfauto.ErrConf) {
			t.Errorf("%q: %v", conf, err)
		}
	}
}

// TestSetSecurityRevert: the stock file's line is replaced, and Revert
// gives back the very bytes; a [commands] without the key gets the line
// under its header, and Revert removes it; no [commands], the key twice,
// or no line of hardn's to revert is ErrConf.
func TestSetSecurityRevert(t *testing.T) {
	data := stock(t)
	out, replaced, err := dnfauto.SetSecurity(data)
	if err != nil || replaced != "upgrade_type = default" || !strings.Contains(string(out), "\nupgrade_type = security\n") {
		t.Fatalf("stock: %q, %v", replaced, err)
	}
	if s, err := dnfauto.Parse(out); s.UpgradeType != dnfauto.Security || err != nil {
		t.Errorf("stock after: %+v, %v", s, err)
	}
	if back, err := dnfauto.Revert(out, replaced); err != nil || string(back) != string(data) {
		t.Errorf("stock reverted: %v, same %v", err, string(back) == string(data))
	}
	plain := []byte("[commands]\napply_updates = no\n\n[emitters]\nemit_via = stdio\n")
	out, replaced, err = dnfauto.SetSecurity(plain)
	if err != nil || replaced != "" || string(out) != "[commands]\nupgrade_type = security\napply_updates = no\n\n[emitters]\nemit_via = stdio\n" {
		t.Fatalf("no key: %q, %q, %v", out, replaced, err)
	}
	if back, err := dnfauto.Revert(out, replaced); err != nil || string(back) != string(plain) {
		t.Errorf("no key reverted: %q, %v", back, err)
	}
	for _, conf := range []string{"[emitters]\nemit_via = stdio\n", "[commands]\nupgrade_type = default\nupgrade_type = default\n", "[commands]\nUPGRADE_TYPE = default\n"} {
		if _, _, err := dnfauto.SetSecurity([]byte(conf)); !errors.Is(err, dnfauto.ErrConf) {
			t.Errorf("set %q: %v", conf, err)
		}
	}
	for _, conf := range []string{"[commands]\nupgrade_type = default\n", "[commands]\nupgrade_type = security\nupgrade_type = security\n", "upgrade_type = security\n"} {
		if _, err := dnfauto.Revert([]byte(conf), "upgrade_type = default"); !errors.Is(err, dnfauto.ErrConf) {
			t.Errorf("revert %q: %v", conf, err)
		}
	}
}

// TestRead: not installed reads the two timers (not-found, systemctl's
// answer on rocky10-test-110) but not the file; installed, the two timers,
// the file, and which timer installs updates; a timer systemctl answers in
// a form it doesn't document is an error of the read.
func TestRead(t *testing.T) {
	ctx := context.Background()
	unit := func(h *hostfake.System, name, active, enabled string) {
		h.Respond(hostfake.Response{Stdout: active + "\n", ExitCode: map[bool]int{false: 3}[active == "active"]}, "systemctl", "is-active", name)
		h.Respond(hostfake.Response{Stdout: enabled + "\n", ExitCode: map[bool]int{false: 1}[enabled == "enabled"]}, "systemctl", "is-enabled", name)
	}
	h := hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: "package dnf-automatic is not installed\n", ExitCode: 1}, "rpm", "-q", "dnf-automatic")
	for _, tm := range []string{dnfauto.InstallTimer, dnfauto.PlainTimer} {
		h.Respond(hostfake.Response{Stdout: "inactive\n", ExitCode: 4}, "systemctl", "is-active", tm)
		h.Respond(hostfake.Response{Stdout: "not-found\n", ExitCode: 4}, "systemctl", "is-enabled", tm)
	}
	h.AddFile(dnfauto.Conf, []byte("[commands]\nupgrade_type = security\n"), 0o644, 0, 0)
	u := dnfauto.Read(ctx, h)
	if u.Package != detect.No || u.Err() != nil || len(h.Runs()) != 5 || u.Install.Enabled != "not-found" || u.Plain.Active != "inactive" || u.UpgradeType != "" {
		t.Errorf("not installed: %+v, ran %q", u, h.Runs())
	}
	for _, c := range h.Calls() {
		if c.Path == dnfauto.Conf {
			t.Errorf("not installed: %s", c)
		}
	}
	// Not installed on systemd 252 (alma9-test-111): each timer's is-active
	// inactive, exit 3, and is-enabled nothing on stdout, exit 1.
	h = hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: "package dnf-automatic is not installed\n", ExitCode: 1}, "rpm", "-q", "dnf-automatic")
	for _, tm := range []string{dnfauto.InstallTimer, dnfauto.PlainTimer} {
		h.Respond(hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, "systemctl", "is-active", tm)
		h.Respond(hostfake.Response{Stderr: "Failed to get unit file state for " + tm + ": No such file or directory\n", ExitCode: 1}, "systemctl", "is-enabled", tm)
	}
	if u = dnfauto.Read(ctx, h); u.Package != detect.No || u.Err() != nil || u.Install.Enabled != "not-found" || u.Plain.Enabled != "not-found" {
		t.Errorf("not installed, systemd 252: %+v, ran %q", u, h.Runs())
	}
	h = hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: "dnf-automatic-4.20.0-22.el10_2.rocky.0.1.noarch\n"}, "rpm", "-q", "dnf-automatic")
	unit(h, dnfauto.InstallTimer, "inactive", "disabled")
	h.Respond(hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, "systemctl", "is-active", dnfauto.PlainTimer)
	h.Respond(hostfake.Response{Stdout: "Failed to connect to bus\n", ExitCode: 1}, "systemctl", "is-enabled", dnfauto.PlainTimer)
	h.AddFile(dnfauto.Conf, stock(t), 0o644, 0, 0)
	if u := dnfauto.Read(ctx, h); u.Err() == nil || u.Plain.Err == nil || u.Install.Err != nil {
		t.Errorf("a timer unread: %+v", u)
	}
	h = hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: "dnf-automatic-4.20.0-22.el10_2.rocky.0.1.noarch\n"}, "rpm", "-q", "dnf-automatic")
	unit(h, dnfauto.PlainTimer, "inactive", "disabled")
	h.Respond(hostfake.Response{Stdout: "Failed to connect to bus\n", ExitCode: 1}, "systemctl", "is-active", dnfauto.InstallTimer)
	h.Respond(hostfake.Response{Stdout: "disabled\n", ExitCode: 1}, "systemctl", "is-enabled", dnfauto.InstallTimer)
	h.AddFile(dnfauto.Conf, stock(t), 0o644, 0, 0)
	if u := dnfauto.Read(ctx, h); u.Err() == nil || u.Install.Err == nil {
		t.Errorf("is-active unread: %+v", u)
	}
	for _, c := range []struct {
		name           string
		install, plain [2]string
		conf           string
		applies        string
	}{
		{"stock, timers off", [2]string{"inactive", "disabled"}, [2]string{"inactive", "disabled"}, string(stock(t)), ""},
		{"install timer on", [2]string{"active", "enabled"}, [2]string{"inactive", "disabled"}, string(stock(t)), dnfauto.InstallTimer},
		{"plain timer, apply_updates no", [2]string{"inactive", "disabled"}, [2]string{"active", "enabled"}, string(stock(t)), ""},
		{"plain timer, apply_updates yes", [2]string{"inactive", "disabled"}, [2]string{"active", "enabled"}, "[commands]\napply_updates = yes\n", dnfauto.PlainTimer},
		{"plain timer active only, apply_updates yes", [2]string{"inactive", "disabled"}, [2]string{"active", "disabled"}, "[commands]\napply_updates = yes\n", dnfauto.PlainTimer},
		{"plain timer enabled only, apply_updates yes", [2]string{"inactive", "disabled"}, [2]string{"inactive", "enabled"}, "[commands]\napply_updates = yes\n", dnfauto.PlainTimer},
	} {
		h := hostfake.New(host.Options{})
		h.Respond(hostfake.Response{Stdout: "dnf-automatic-4.20.0-22.el10_2.rocky.0.1.noarch\n"}, "rpm", "-q", "dnf-automatic")
		unit(h, dnfauto.InstallTimer, c.install[0], c.install[1])
		unit(h, dnfauto.PlainTimer, c.plain[0], c.plain[1])
		h.AddFile(dnfauto.Conf, []byte(c.conf), 0o644, 0, 0)
		u := dnfauto.Read(ctx, h)
		tm, ok := u.Applies()
		if u.Err() != nil || u.Package != detect.Yes || (c.applies != "") != ok || ok && tm.Name != c.applies {
			t.Errorf("%s: %+v; applies %v %q", c.name, u, ok, tm.Name)
		}
	}
}
