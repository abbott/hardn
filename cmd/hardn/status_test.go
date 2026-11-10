package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// TestReadStatus: the main menu's status is read through statusHost with
// engineDetect's facts, its header showing hardn's version, its DNS row
// checked against the configuration as it is on each pass (the DNS screen
// adopts each list it saves into it); the heading, mode, items and prompt
// are pkg/screens/mainmenu's to fill; a build with no version shows none,
// not a bare "v"; it only reads, and nothing is written.
// (pkg/screens/mainmenu's tests draw the main menu with this status over the
// gallery's hosts.)
func TestReadStatus(t *testing.T) {
	h, cfg := newDNSHost(t, "configured", dnsTestList)
	useDNSHost(t, h) // engineDetect, stdoutEnv: plain at 80 columns
	hostWas := statusHost
	t.Cleanup(func() { statusHost = hostWas })
	statusHost = func() host.System { return h }
	engineHost = func() host.System { t.Error("the status read engineHost, whose run log takes every command"); return h }

	rows := func(vm screen.MainMenu) string {
		var b strings.Builder
		env := stdoutEnv()
		_, _ = io.WriteString(env.Writer(&b), vm.Status.Render(component.NewCtx(env)))
		return b.String()
	}
	vm, err := readStatus(context.Background(), cfg, "0.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if vm.Header.Version != "v0.4.0" || vm.Heading != "" || len(vm.Menu.Items) != 0 || vm.Prompt != "" ||
		!strings.Contains(rows(vm), "  [+] DNS                  Configured 192.0.2.53, 192.0.2.54") {
		t.Errorf("not the status with the DNS row Configured, and nothing of the menu:\n%+v\n%s", vm, rows(vm))
	}
	cfg.Nameservers = []string{"192.0.2.55"} // a list the DNS screen saved
	vm, err = readStatus(context.Background(), cfg, "0.4.0")
	if err != nil || !strings.Contains(rows(vm), "  [ ] DNS                  Not Configured not hardn.yml's") {
		t.Errorf("the DNS row does not follow the configuration (%v):\n%s", err, rows(vm))
	}
	if vm, err = readStatus(context.Background(), cfg, ""); err != nil || vm.Header.Version != "" {
		t.Errorf("a build with no version (%v): the header's version %q, want none", err, vm.Header.Version)
	}
	if w := h.writes(); len(w) > 0 {
		t.Errorf("the status wrote %q", w)
	}
	for _, c := range h.Calls() {
		if c.Op == "run" && !statusQuery(c.Argv) {
			t.Errorf("ran %q", c.Argv)
		}
	}
}

// TestWithStatus: withStatus, as main fills the main menu's Options, sets
// both of its fields, without either of which the main menu panics on its
// first pass (pkg/screens/mainmenu's TestLiveStatusEnds): the status as
// readStatus reads it, through statusHost against the configuration given,
// with hardn's version; and the terminal as stdoutEnv detects it, asked on
// each pass.
func TestWithStatus(t *testing.T) {
	h, cfg := newDNSHost(t, "configured", dnsTestList)
	useDNSHost(t, h) // stdoutEnv restored as the test ends
	hostWas := statusHost
	t.Cleanup(func() { statusHost = hostWas })
	statusHost = func() host.System { return h }
	columns := "61" // stdout is not a terminal: COLUMNS gives the width
	stdoutEnv = func() term.Env {
		return term.Resolve(term.Options{}, term.Facts{Environ: []string{"COLUMNS=" + columns}})
	}

	set := withStatus(mainmenu.Options{}, cfg, "0.4.0")
	if set.Status == nil || set.Terminal == nil {
		t.Fatalf("withStatus set the status %t and the terminal %t, want both", set.Status != nil, set.Terminal != nil)
	}
	c := component.NewCtx(stdoutEnv())
	vm, err := set.Status(context.Background())
	want, wantErr := readStatus(context.Background(), cfg, "0.4.0")
	if err != nil || wantErr != nil || vm.Header.Version != "v0.4.0" || vm.Render(c) != want.Render(c) {
		t.Errorf("the status set (%v) is not readStatus's (%v):\n%s\nwant\n%s", err, wantErr, vm.Render(c), want.Render(c))
	}
	if got := set.Terminal().Width; got != 61 {
		t.Errorf("the terminal set is %d columns, want stdoutEnv's, 61", got)
	}
	columns = "72"
	if got := set.Terminal().Width; got != 72 {
		t.Errorf("the terminal set is %d columns, want stdoutEnv's, asked again, 72", got)
	}
}

// statusQuery reports whether argv is a query of the status's checks on
// the DNS hosts: systemctl's state of a unit, sshd's test mode, and the
// like.
func statusQuery(argv []string) bool {
	cmd := strings.Join(argv, " ")
	for _, q := range []string{"systemctl is-active ", "systemctl is-enabled ", "sshd -T", "ss -Htln", "ufw status verbose", "dpkg-query --show",
		"sudo --version", "visudo -c", "apt-config dump", "ip -4 addr show dev ", "resolvectl --no-pager "} {
		if strings.HasPrefix(cmd, q) {
			return true
		}
	}
	return false
}

// TestStatusSeams: the status reads host.Real and detects with
// detect.Detect.
func TestStatusSeams(t *testing.T) {
	if _, ok := statusHost().(*host.Real); !ok {
		t.Error("statusHost is not host.Real")
	}
	if !samePointer(engineDetect, detect.Detect) {
		t.Error("engineDetect is not detect.Detect")
	}
}
