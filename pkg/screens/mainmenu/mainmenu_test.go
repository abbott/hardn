package mainmenu_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The main menu's status over hostfake (pkg/screens/mainmenu's tests draw the main
// menu with it over the gallery's status hosts). Nothing on this machine
// is read or run.

// calls are hostfake's records of the methods that only read, and of
// Run, whose commands this host answers none of: which commands a pass
// runs, and that each only reads, pkg/screens/mainmenu's TestLiveStatusOnHosts checks
// on hosts that answer them.
var calls = []string{"read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group", "run"}

// options are a status read on f, hardn.yml listing one nameserver.
func options(f *hostfake.System) mainmenu.StatusOptions {
	cfg := config.DefaultConfig()
	cfg.Nameservers = []string{"192.0.2.53"}
	return mainmenu.StatusOptions{System: f, Detect: detect.Detect, Config: cfg, Version: "v9.9.9"}
}

// plain renders vm plain, as written to output that is not a terminal,
// 80 columns wide.
func plain(t *testing.T, vm interface{ Render(component.Ctx) string }) string {
	t.Helper()
	env := term.Resolve(term.Options{}, term.Facts{Width: 80})
	var b strings.Builder
	if _, err := io.WriteString(env.Writer(&b), vm.Render(component.NewCtx(env))); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestNothingRead: on a host where nothing can be read (no file, every
// command failing), every row is Unknown, never a guess, and the status
// is still returned: each weighed row with "Counted as not configured"
// under it, DNS with its own "can't read" and nothing under it, the risk
// level Unknown, the header the version given; the heading, mode, menu
// and prompt left for pkg/screens/mainmenu; and every call a read or a command
// (calls), so no file is written (which commands, this test does not
// check).
func TestNothingRead(t *testing.T) {
	f := hostfake.New(host.Options{})
	vm, err := mainmenu.Status(context.Background(), options(f))
	if err != nil {
		t.Fatal(err)
	}
	if vm.Header.Version != "v9.9.9" || vm.Heading != "" || vm.Mode != (component.ModeIndicator{}) || len(vm.Menu.Items) != 0 || vm.Menu.Exit.Label != "" || vm.Prompt != "" {
		t.Errorf("want the version given and no heading, mode, menu or prompt: %+v", vm)
	}
	out := plain(t, vm.Status)
	lines := strings.Split(out, "\n")
	for _, row := range []string{"User", "Firewall", "SSH Login", "SSH Auth", "SSH Port", "AppArmor", "Auto Updates"} {
		i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "  [?] "+row+" ") })
		if i < 0 || !strings.Contains(lines[i], "Unknown") || strings.TrimSpace(lines[i+1]) != "- Counted as not configured" {
			t.Errorf("%s: not Unknown, counted as not configured:\n%s", row, out)
		}
	}
	i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "  [?] DNS ") })
	if i < 0 || !strings.HasSuffix(lines[i], "Unknown can't read") || i != len(lines)-1 {
		t.Errorf("DNS: want Unknown, \"can't read\", last, nothing under it:\n%s", out)
	}
	if risk := plain(t, vm.Risk); !strings.Contains(risk, "Risk Level           Unknown") {
		t.Errorf("the risk level is not Unknown:\n%s", risk)
	}
	for _, c := range f.Calls() {
		if !slices.Contains(calls, c.Op) {
			t.Errorf("a call that is neither a read nor a command: %v", c)
		}
	}
}

// TestCancelled: a context cancelled before the checks or the header
// probe finish returns its error and the zero view-model.
func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	vm, err := mainmenu.Status(ctx, options(hostfake.New(host.Options{})))
	if len(vm.Status.Rows) != 0 || vm.Header.Version != "" || !errors.Is(err, context.Canceled) {
		t.Errorf("returned %+v, error %v; want nothing and context.Canceled", vm, err)
	}
}
