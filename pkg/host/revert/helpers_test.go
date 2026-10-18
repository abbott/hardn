package revert

import (
	"bytes"
	"context"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Synthetic values only: alice and bob, web-01.example.net, 192.0.2.0/24,
// port 2230.
const (
	testID    = "20261018T120000Z-1a2b3c4d"
	otherID   = "20261018T130000Z-5e6f7a8b"
	testHardn = "/usr/local/bin/hardn"
	// The session that applied the change, on port 22, and a new one on
	// 2230 from the same client.
	applyConn = "192.0.2.10 50022 192.0.2.20 22"
	newConn   = "192.0.2.10 50123 192.0.2.20 2230"
)

var (
	testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	systemd = detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir}
	openrc  = detect.Init{Kind: detect.OpenRC, Running: detect.Yes, Source: detect.OpenRCRunDir}
	any2230 = Listener{Addr: netip.IPv4Unspecified(), Port: 2230}
)

// newHost is a fake host with hardn installed and the parents of the state
// and run directories, all root's.
func newHost(t *testing.T) *hostfake.System {
	t.Helper()
	f := hostfake.New(host.Options{})
	f.AddFile(testHardn, []byte("ELF"), 0o755, 0, 0)
	f.AddDir("/var/lib", 0o755, 0, 0)
	f.AddDir("/run", 0o755, 0, 0)
	return f
}

// clock is a settable Options.Now.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func testOptions(c *clock, log *bytes.Buffer) Options {
	o := Options{Now: c.now, Hardn: testHardn}
	if log != nil {
		o.Log = log
	}
	return o
}

// lockout is the engine's journal for a port change to 2230, before Arm.
func lockout() Journal {
	return Journal{
		ID: testID, Risk: RiskLockout, TimeoutSeconds: 180,
		Summary:    "SSH port 22 to 2230 on web-01.example.net",
		Session:    Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"},
		ConfirmVia: []Listener{any2230},
	}
}

// armed is lockout() armed on systemd at testNow.
func armed(t *testing.T, f *hostfake.System, c *clock) Journal {
	t.Helper()
	respond(f, hostfake.Response{}, systemdRunArgv(testID))
	j, err := Arm(context.Background(), f, lockout(), systemd, testOptions(c, nil))
	if err != nil {
		t.Fatalf("Arm: %v", err)
	}
	return j
}

func systemdRunArgv(id string) []string {
	return []string{"systemd-run", "--unit=hardn-revert-" + id, "--description=hardn: roll back change " + id + " unless confirmed",
		"--on-active=180s", "--timer-property=AccuracySec=1s", testHardn, "rollback", id}
}

// respond scripts argv on the fake.
func respond(f *hostfake.System, r hostfake.Response, argv []string) {
	f.Respond(r, argv[0], argv[1:]...)
}

func stateFile(name string) string { return filepath.Join(DefaultDir, name) }

// exists reports whether the fake has a file at name.
func exists(f *hostfake.System, name string) bool {
	_, ok := f.Entry(name)
	return ok
}

// readJournalT decodes the journal at name on the fake.
func readJournalT(t *testing.T, f *hostfake.System, name string) Journal {
	t.Helper()
	e, ok := f.Entry(name)
	if !ok {
		t.Fatalf("%s: missing", name)
	}
	if e.Mode.Perm() != 0o600 || e.UID != 0 {
		t.Errorf("%s: mode %v, uid %d; want 0600, root", name, e.Mode, e.UID)
	}
	j, err := decodeJournal(e.Data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return j
}

// runs renders the commands the fake ran or started, one per line.
func runs(f *hostfake.System) string {
	var b strings.Builder
	for _, c := range f.Calls() {
		if c.Op == "run" || c.Op == "start-detached" {
			b.WriteString(c.Op + " " + host.FormatArgv(c.Argv) + "\n")
		}
	}
	return b.String()
}

// writes lists the paths the fake wrote, removed or renamed.
func writes(f *hostfake.System) []string {
	var out []string
	for _, c := range f.Calls() {
		switch c.Op {
		case "write-atomic", "remove", "rename", "mkdir", "chmod", "chown":
			if c.Err == nil {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}
