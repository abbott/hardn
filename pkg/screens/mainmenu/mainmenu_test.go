package mainmenu_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The main menu's status over hostfake (pkg/menu's tests draw it over the
// gallery's status hosts). Nothing on this machine is read or run.

// calls are hostfake's records of the methods that only read, and of
// Run, whose commands this host answers none of: which commands a pass
// runs, and that each only reads, pkg/menu's TestLiveStatusOnHosts checks
// on hosts that answer them.
var calls = []string{"read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group", "run"}

// options are a status drawn on f, plain at 80 columns, hardn.yml listing
// one nameserver.
func options(f *hostfake.System, env term.Env) mainmenu.Options {
	cfg := config.DefaultConfig()
	cfg.Nameservers = []string{"192.0.2.53"}
	return mainmenu.Options{System: f, Detect: detect.Detect, Config: cfg, Version: "v9.9.9", Env: env}
}

// plain is output that is not a terminal, 80 columns wide.
func plain() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80}) }

// TestNothingRead: on a host where nothing can be read (no file, every
// command failing), every row is Unknown, never a guess, and the status
// is still drawn: each weighed row with "Counted as not configured" under
// it, DNS with its own "can't read" and nothing under it, the risk level
// Unknown; and every call a read or a command (calls), so no file is
// written (which commands, this test does not check).
func TestNothingRead(t *testing.T) {
	f := hostfake.New(host.Options{})
	out, err := mainmenu.Status(context.Background(), options(f, plain()), 17)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(out, "hardn  v9.9.9") || !strings.HasSuffix(out, "----------------------------------------------------------------\n\n") {
		t.Errorf("not the full form's blocks and the gap before the menu:\n%s", out)
	}
	for _, row := range []string{"User", "Firewall", "SSH Login", "SSH Auth", "SSH Port", "AppArmor", "Auto Updates"} {
		i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "  [?] "+row+" ") })
		if i < 0 || !strings.Contains(lines[i], "Unknown") || strings.TrimSpace(lines[i+1]) != "- Counted as not configured" {
			t.Errorf("%s: not Unknown, counted as not configured:\n%s", row, out)
		}
	}
	i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "  [?] DNS ") })
	if i < 0 || !strings.HasSuffix(lines[i], "Unknown can't read") || strings.TrimSpace(lines[i+1]) != "" {
		t.Errorf("DNS: want Unknown, \"can't read\", nothing under it:\n%s", out)
	}
	if !strings.Contains(out, "Risk Level           Unknown") {
		t.Errorf("the risk level is not Unknown:\n%s", out)
	}
	for _, c := range f.Calls() {
		if !slices.Contains(calls, c.Op) {
			t.Errorf("a call that is neither a read nor a command: %v", c)
		}
	}
}

// TestForm: the blocks' form is the terminal's, for a menu of the lines
// given (screen.MainMenu.StatusBlocks): in full on output that is not a
// terminal, compact on a terminal of 24 rows, in full on one of 60 rows
// that holds them and the menu; each ends with the blank line before the
// menu, and the header shows the version given.
func TestForm(t *testing.T) {
	tty := func(h int) term.Env {
		return term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: h, Environ: []string{"TERM=xterm-256color"}})
	}
	for _, tc := range []struct {
		name string
		env  term.Env
		full bool
	}{{"plain", plain(), true}, {"80x24", tty(24), false}, {"80x60", tty(60), true}} {
		out, err := mainmenu.Status(context.Background(), options(hostfake.New(host.Options{}), tc.env), 17)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "hardn") != tc.full || strings.Contains(out, "v9.9.9") != tc.full || !strings.HasSuffix(out, "\n\n") {
			t.Errorf("%s: want the full form %t:\n%s", tc.name, tc.full, out)
		}
	}
}

// TestCancelled: a context cancelled before the checks or the header
// probe finish draws nothing and returns its error.
func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := mainmenu.Status(ctx, options(hostfake.New(host.Options{}), plain()), 17)
	if out != "" || !errors.Is(err, context.Canceled) {
		t.Errorf("drew %q, error %v; want nothing and context.Canceled", out, err)
	}
}
