package revert

import (
	"bytes"
	"context"
	"fmt"
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

// Processes in the fake's /proc: the sshd listener; the sshd-session of
// the connection that applied the change, of a second one the user kept
// open, and of a new one; and hardn's own process.
const (
	listenerPID  = 900
	applySSHD    = 3970
	keptSSHD     = 3500
	newSSHD      = 4100
	selfPID      = 5000
	testBootID   = "8e2a3c4d-1b2c-4d3e-8f40-0123456789ab"
	ptsTTY       = 136<<8 | 3 // /dev/pts/3
	consoleTTY1  = 4<<8 | 1   // /dev/tty1
	serialTTYS0  = 4<<8 | 64  // /dev/ttyS0
	legacyPTYTTY = 3 << 8     // /dev/ttyp0
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
	setSelf(f, selfPID)
	f.AddFile(bootIDFile, []byte(testBootID+"\n"), 0o444, 0, 0)
	return f
}

// procStart is the start time the fake gives process pid.
func procStart(pid int) uint64 { return uint64(pid) * 100 }

// sshdOf names process pid as the fake starts it.
func sshdOf(pid int) ProcStart { return ProcStart{PID: pid, Start: procStart(pid)} }

// statLine is /proc/<pid>/stat as Linux writes it (proc_pid_stat(5)), with
// fields 5 to 21 filled in plausibly.
func statLine(pid, ppid int, comm string, tty int, start uint64) string {
	return fmt.Sprintf("%d (%s) S %d %d %d %d -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, pid, pid, tty, start)
}

// proc adds process pid to the fake's /proc: its stat (parent, controlling
// terminal, start time procStart(pid)) and initial environment.
func proc(f *hostfake.System, pid, ppid int, comm string, tty int, env ...string) {
	block := ""
	for _, kv := range env {
		block += kv + "\x00"
	}
	dir := fmt.Sprintf("/proc/%d", pid)
	f.AddFile(dir+"/environ", []byte(block), 0o400, 0, 0)
	f.AddFile(dir+"/stat", []byte(statLine(pid, ppid, comm, tty, procStart(pid))), 0o444, 0, 0)
}

// setSelf makes /proc/self the fake's hardn process pid.
func setSelf(f *hostfake.System, pid int) {
	f.AddFile("/proc/self/stat", []byte(statLine(pid, 1, "hardn", ptsTTY, procStart(pid))), 0o444, 0, 0)
}

// oneCommand is `ssh host "doas hardn"`: hardn directly under the
// connection's sshd-session, since the remote shell exec'd doas and doas
// exec'd hardn with SSH_CONNECTION cleared, so no ancestor has it.
func oneCommand(f *hostfake.System) {
	proc(f, listenerPID, 1, "sshd", 0)
	proc(f, newSSHD, listenerPID, "sshd-session", 0)
	proc(f, 4000, newSSHD, "hardn", 0, "PATH=/usr/bin", "DOAS_USER=alice")
}

// sockets gives process pid these open files, from descriptor 3.
func sockets(f *hostfake.System, pid int, targets ...string) {
	for i, target := range targets {
		f.AddSymlink(fmt.Sprintf("/proc/%d/fd/%d", pid, i+3), target)
	}
}

// tcpTable is /proc/net/tcp with these lines, each "local remote st inode",
// the fields around them filled in as Linux writes them (the kernel's
// proc_net_tcp.rst). Addresses are in a little-endian host's byte order, as
// on hardn's amd64 and arm64.
func tcpTable(lines ...string) string {
	out := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	for i, l := range lines {
		first, rest, _ := strings.Cut(l, "\n") // a line may carry a whole further entry
		f := strings.Fields(first)
		out += fmt.Sprintf("%4d: %s %s %s 00000000:00000000 00:00000000 00000000     0        0 %s 1 0000000000000000 20 4 0 10 -1\n", i, f[0], f[1], f[2], f[3])
		if rest != "" {
			out += rest + "\n"
		}
	}
	return out
}

// sshds adds the sshd processes running when a change is applied: the
// listener, the applying connection's and a second one kept open.
func sshds(f *hostfake.System) {
	proc(f, listenerPID, 1, "sshd", 0)
	proc(f, applySSHD, listenerPID, "sshd-session", 0)
	proc(f, keptSSHD, listenerPID, "sshd-session", 0)
}

// remote is an SSH session over conn whose sshd-session is process sshd.
func remote(conn string, sshd int) Session {
	return Session{Remote: true, Conn: conn, SSHD: sshdOf(sshd), Source: "SSH_CONNECTION"}
}

// newSession is a connection made after the apply, to port 2230.
var newSession = remote(newConn, newSSHD)

// markApplied ends the apply of the pending change on f, with sshds
// running, and returns the journal.
func markApplied(t *testing.T, f *hostfake.System, c *clock) Journal {
	t.Helper()
	sshds(f)
	j, err := MarkApplied(context.Background(), f, testID, testOptions(c, nil))
	if err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	return j
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
		Session:    remote(applyConn, applySSHD),
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
