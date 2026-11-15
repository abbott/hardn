package sysctl_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sysctl"
	"github.com/abbott/hardn/pkg/host/sysctl/sysctlfake"
)

var (
	systemd = detect.Facts{Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}
	openRC  = detect.Facts{Platform: detect.Platform{Distro: detect.DistroAlpine}, Init: detect.Init{Kind: detect.OpenRC, Running: detect.Yes}}
)

// on is a host with eth0, over's values over the baseline's, procps's
// sysctl, and files.
func on(over map[string]int, files map[string]string) *hostfake.System {
	f := hostfake.New(host.Options{})
	for name, data := range sysctlfake.Files(sysctlfake.Values([]string{"eth0"}, over)) {
		f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	f.AddFile(sysctlfake.ProcpsPath, nil, 0o755, 0, 0)
	f.AddDir("/etc/sysctl.d", 0o755, 0, 0)
	for name, data := range files {
		f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	return f
}

func row(t *testing.T, r sysctl.Reading, name string) sysctl.Row {
	t.Helper()
	i := slices.IndexFunc(r.Rows, func(row sysctl.Row) bool { return row.Spec.Name == name })
	if i < 0 {
		t.Fatalf("no row %q", name)
	}
	return r.Rows[i]
}

func read(f *hostfake.System, facts detect.Facts) sysctl.Reading {
	return sysctl.Read(context.Background(), f, facts)
}

// TestReadInterfaces: a row reads every interface's own value and shows the
// weakest (F1): eth0 alone at 1 puts the row below the baseline.
func TestReadInterfaces(t *testing.T) {
	f := on(map[string]int{"net.ipv6.conf.eth0.accept_redirects": 1}, nil)
	r := read(f, systemd)
	if r.Err != nil || r.Tool != sysctl.Procps || r.Boot != detect.Yes || r.Routes {
		t.Fatalf("reading = %+v", r)
	}
	v6 := row(t, r, "IPv6 redirects")
	if v6.State != sysctl.Below || v6.Shown != 1 || len(v6.Low) != 1 || v6.Low[0].Key != "net.ipv6.conf.eth0.accept_redirects" || len(v6.Values) != 4 {
		t.Errorf("IPv6 redirects = %+v", v6)
	}
	if v4 := row(t, r, "ICMP redirects"); v4.State != sysctl.Pass || v4.Shown != 0 {
		t.Errorf("ICMP redirects = %+v", v4)
	}
	if n := r.Count(sysctl.Below); n != 2 { // IPv6 redirects, and Sending redirects, on on every host read
		t.Errorf("%d rows below", n)
	}
	if len(f.Runs()) != 0 {
		t.Errorf("Read ran %q", f.Runs())
	}
}

// TestBaseline: a stricter value passes; the reverse-path filter passes
// strict or loose, by the higher of "all" and the interface's own; BPF
// passes at 1.
func TestBaseline(t *testing.T) {
	r := read(on(map[string]int{"kernel.perf_event_paranoid": 4, "fs.protected_regular": 2, "kernel.unprivileged_bpf_disabled": 1,
		"net.ipv4.conf.all.rp_filter": 1, "net.ipv4.conf.eth0.rp_filter": 0, "net.ipv4.conf.default.rp_filter": 2}, nil), systemd)
	for _, name := range []string{"Performance events", "Files in /tmp", "Unprivileged BPF", "Reverse-path filter"} {
		if rw := row(t, r, name); rw.State != sysctl.Pass {
			t.Errorf("%s = %v, low %v", name, rw.State, rw.Low)
		}
	}
	r = read(on(map[string]int{"net.ipv4.conf.all.rp_filter": 0, "net.ipv4.conf.eth0.rp_filter": 0}, nil), systemd)
	rp := row(t, r, "Reverse-path filter")
	if rp.State != sysctl.Below || len(rp.Low) != 1 || rp.Low[0].Key != "net.ipv4.conf.eth0.rp_filter" {
		t.Errorf("Reverse-path filter = %+v", rp)
	}
	text := string(r.Next().Bytes())
	if !strings.Contains(text, "net.ipv4.conf.eth0.rp_filter = 2\n") || strings.Contains(text, "conf.all.rp_filter =") || strings.Contains(text, "*.rp_filter") {
		t.Errorf("file:\n%s", text)
	}
}

// TestRoutes: any forwarding key at 1 is a host that routes, one interface's
// among them, and Proxmox VE always is; Sending redirects and Reverse-path
// filter are then left alone, and the other rows judged.
func TestRoutes(t *testing.T) {
	for _, key := range []string{"net.ipv4.ip_forward", "net.ipv6.conf.all.forwarding", "net.ipv4.conf.eth0.forwarding"} {
		r := read(on(map[string]int{key: 1, "kernel.kptr_restrict": 0}, nil), systemd)
		if !r.Routes || row(t, r, "Forwarding").Shown != 1 {
			t.Errorf("%s: not a host that routes", key)
		}
		if s := row(t, r, "Sending redirects"); s.State != sysctl.LeftAlone || len(s.Low) != 0 {
			t.Errorf("%s: Sending redirects = %+v", key, s)
		}
		if got := string(r.Next().Bytes()); got != sysctl.Header+"# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n" {
			t.Errorf("%s: file:\n%s", key, got)
		}
	}
	pve := systemd
	pve.Platform.Distro = detect.DistroProxmox
	r := read(on(nil, nil), pve)
	if r.Routes || !r.Proxmox || row(t, r, "Sending redirects").State != sysctl.LeftAlone || row(t, r, "Reverse-path filter").State != sysctl.LeftAlone {
		t.Errorf("Proxmox VE = %+v", r)
	}
	if len(r.Todo()) != 0 {
		t.Errorf("Proxmox VE: to set %v", r.Todo())
	}
}

// TestContainer: every row is read and none judged.
func TestContainer(t *testing.T) {
	c := systemd
	c.Container.Detected = detect.Yes
	r := read(on(map[string]int{"kernel.kptr_restrict": 0}, nil), c)
	if !r.Container || len(r.Todo()) != 0 {
		t.Fatalf("container = %+v", r)
	}
	for _, rw := range r.Rows {
		if rw.State != sysctl.NoVerdict {
			t.Errorf("%s = %v", rw.Spec.Name, rw.State)
		}
	}
	if row(t, r, "Kernel addresses").Shown != 0 {
		t.Error("the value is not shown")
	}
}

// TestAbsentAndUnread: a key the kernel does not have is left out, its row
// Absent where it has none (IPv6 off); a key that cannot be read is Unread,
// never a pass.
func TestAbsentAndUnread(t *testing.T) {
	f := on(nil, nil)
	if err := f.RemoveAll(context.Background(), sysctl.Proc+"/net/ipv6"); err != nil {
		t.Fatal(err)
	}
	r := read(f, systemd)
	if v6 := row(t, r, "IPv6 redirects"); v6.State != sysctl.Absent {
		t.Errorf("IPv6 redirects = %+v", v6)
	}
	if sr := row(t, r, "Source routing"); sr.State != sysctl.Pass || len(sr.Values) != 1 {
		t.Errorf("Source routing = %+v", sr)
	}
	if text := string(r.Next().Bytes()); strings.Contains(text, "ipv6") {
		t.Errorf("file names an IPv6 key:\n%s", text)
	}

	f = on(nil, map[string]string{sysctl.ProcPath("kernel.dmesg_restrict"): "yes\n"})
	r = read(f, systemd)
	rw, ok := r.Unread()
	if !ok || rw.Spec.Name != "Kernel log" || !errors.Is(rw.Err, sysctl.ErrValue) {
		t.Errorf("unread = %+v, %v", rw, ok)
	}
}

// TestNoProc: a host with no key at all under /proc/sys is not read, and
// never a pass.
func TestNoProc(t *testing.T) {
	r := read(hostfake.New(host.Options{}), systemd)
	if !errors.Is(r.Err, fs.ErrNotExist) || len(r.Todo()) != 0 {
		t.Errorf("reading = %v", r.Err)
	}
}

// TestHeld: a key below the baseline that a file read after hardn's sets so
// is Held, by that file; an earlier file, and a later one whose last word
// passes, hold nothing. Under systemd a "*" line holds too; under OpenRC
// /etc/sysctl.conf is read after the directory.
func TestHeld(t *testing.T) {
	const redirects = "net.ipv4.conf.all.accept_redirects"
	low := map[string]int{redirects: 1}
	tests := []struct {
		name  string
		facts detect.Facts
		files map[string]string
		by    string
	}{
		{"later file", systemd, map[string]string{"/etc/sysctl.d/99-sysctl.conf": "# mine\n" + redirects + " = 1\n"}, "/etc/sysctl.d/99-sysctl.conf"},
		{"later file, slashes", systemd, map[string]string{"/etc/sysctl.d/99-sysctl.conf": "net/ipv4/conf/all/accept_redirects=1\n"}, "/etc/sysctl.d/99-sysctl.conf"},
		{"later glob", systemd, map[string]string{"/usr/lib/sysctl.d/95-site.conf": "net.ipv4.conf.*.accept_redirects = 1\n"}, "/usr/lib/sysctl.d/95-site.conf"},
		{"earlier file", systemd, map[string]string{"/etc/sysctl.d/50-site.conf": redirects + " = 1\n"}, ""},
		{"hidden by /etc's of the same name", systemd, map[string]string{"/usr/lib/sysctl.d/99-x.conf": redirects + " = 1\n", "/etc/sysctl.d/99-x.conf": "\n"}, ""},
		{"a later file's last word passes", systemd, map[string]string{"/etc/sysctl.d/95-a.conf": redirects + " = 1\n", "/etc/sysctl.d/99-b.conf": redirects + " = 0\n"}, ""},
		{"sysctl.conf, systemd", systemd, map[string]string{sysctl.Conf: redirects + " = 1\n"}, ""},
		{"sysctl.conf, OpenRC", openRC, map[string]string{sysctl.Conf: redirects + " = 1\n"}, sysctl.Conf},
		{"OpenRC reads no glob", openRC, map[string]string{"/etc/sysctl.d/99-x.conf": "net.ipv4.conf.*.accept_redirects = 1\n"}, ""},
	}
	for _, tt := range tests {
		r := read(on(low, tt.files), tt.facts)
		rw := row(t, r, "ICMP redirects")
		want := sysctl.Below
		if tt.by != "" {
			want = sysctl.Held
		}
		if rw.State != want || rw.By != tt.by || r.Err != nil {
			t.Errorf("%s: %v by %q (%v), want %v by %q", tt.name, rw.State, rw.By, r.Err, want, tt.by)
		}
		if tt.by != "" && strings.Contains(string(r.Next().Bytes()), "accept_redirects") {
			t.Errorf("%s: hardn's file sets a held key", tt.name)
		}
	}
}

// TestToolAndBoot: busybox's sysctl is told by its link; no tool is NoTool;
// under OpenRC the files are applied at boot only where the sysctl service
// is in a runlevel (F5).
func TestToolAndBoot(t *testing.T) {
	f := on(nil, nil)
	ctx := context.Background()
	if err := f.Remove(ctx, sysctlfake.ProcpsPath); err != nil {
		t.Fatal(err)
	}
	if r := read(f, openRC); r.Tool != sysctl.NoTool || r.Boot != detect.No || !r.OpenRC {
		t.Errorf("no tool: %v, boot %v", r.Tool, r.Boot)
	}
	f.AddFile(sysctlfake.BusyboxPath, nil, 0o755, 0, 0)
	f.AddSymlink(sysctlfake.BusyboxLink, sysctlfake.BusyboxPath)
	f.AddSymlink(detect.OpenRCRunlevels+"/boot/"+sysctl.Service, "/etc/init.d/"+sysctl.Service)
	f.AddFile("/etc/init.d/"+sysctl.Service, nil, 0o755, 0, 0)
	if r := read(f, openRC); r.Tool != sysctl.Busybox || r.Boot != detect.Yes {
		t.Errorf("busybox: %v, boot %v", r.Tool, r.Boot)
	}
}

// TestNext: procps hosts get one "*" line a row, under every key's earlier
// value that differs (F2, F3); busybox hosts a line for each key below the
// baseline (F4). Neither names a key hardn never writes.
func TestNext(t *testing.T) {
	over := map[string]int{"net.ipv6.conf.eth0.accept_redirects": 1, "net.ipv6.conf.lo.accept_redirects": 1, "net.ipv4.conf.*.send_redirects": 0,
		"kernel.yama.ptrace_scope": 0}
	f := on(over, nil)
	got := string(read(f, systemd).Next().Bytes())
	want := sysctl.Header +
		"# before: net.ipv6.conf.eth0.accept_redirects = 1\n# before: net.ipv6.conf.lo.accept_redirects = 1\nnet.ipv6.conf.*.accept_redirects = 0\n" +
		"# before: kernel.yama.ptrace_scope = 0\nkernel.yama.ptrace_scope = 1\n"
	if got != want {
		t.Errorf("procps:\n%s\nwant:\n%s", got, want)
	}
	if err := f.Remove(context.Background(), sysctlfake.ProcpsPath); err != nil {
		t.Fatal(err)
	}
	f.AddFile(sysctlfake.BusyboxPath, nil, 0o755, 0, 0)
	f.AddSymlink(sysctlfake.BusyboxLink, sysctlfake.BusyboxPath)
	got = string(read(f, openRC).Next().Bytes())
	want = sysctl.Header +
		"# before: net.ipv6.conf.eth0.accept_redirects = 1\nnet.ipv6.conf.eth0.accept_redirects = 0\n" +
		"# before: net.ipv6.conf.lo.accept_redirects = 1\nnet.ipv6.conf.lo.accept_redirects = 0\n" +
		"# before: kernel.yama.ptrace_scope = 0\nkernel.yama.ptrace_scope = 1\n"
	if got != want {
		t.Errorf("busybox:\n%s\nwant:\n%s", got, want)
	}
	for _, key := range []string{"net.ipv4.ip_forward", "net.ipv4.conf.all.forwarding", "net.ipv6.conf.eth0.forwarding", "net.ipv6.conf.all.accept_ra",
		"net.ipv4.conf.all.rp_filter", "net.ipv4.conf.*.rp_filter", "kernel.sysrq"} {
		if sysctl.Writable(key) {
			t.Errorf("%s is writable", key)
		}
	}
}

// TestNeverWritten: on a host with every setting at its weakest, routing or
// not, hardn's file names no forwarding key, no accept_ra and no "all"
// reverse-path filter, and every value it writes is at or above the one it
// records.
func TestNeverWritten(t *testing.T) {
	weak := map[string]int{"net.ipv4.conf.*.accept_redirects": 1, "net.ipv6.conf.*.accept_redirects": 1, "net.ipv4.conf.*.rp_filter": 0,
		"net.ipv4.conf.*.accept_source_route": 1, "net.ipv6.conf.*.accept_source_route": 1, "net.ipv4.tcp_syncookies": 0,
		"net.ipv4.icmp_echo_ignore_broadcasts": 0, "net.ipv4.icmp_ignore_bogus_error_responses": 0, "kernel.kptr_restrict": 0,
		"kernel.dmesg_restrict": 0, "kernel.randomize_va_space": 0, "kernel.yama.ptrace_scope": 0, "kernel.unprivileged_bpf_disabled": 0,
		"kernel.perf_event_paranoid": -1, "fs.protected_hardlinks": 0, "fs.protected_symlinks": 0, "fs.protected_fifos": 0, "fs.protected_regular": 0}
	for _, facts := range []detect.Facts{systemd, openRC} {
		r := read(on(weak, nil), facts)
		if n := len(r.Todo()); n != len(sysctl.Specs)-1 {
			t.Errorf("%d rows to set of %d", n, len(sysctl.Specs)-1)
		}
		o := r.Next()
		for _, e := range o.Entries {
			if !sysctl.Writable(e.Key) || strings.Contains(e.Key, "forwarding") || strings.Contains(e.Key, "accept_ra") || strings.HasSuffix(e.Key, "all.rp_filter") {
				t.Errorf("writes %s", e.Key)
			}
		}
		back, err := sysctl.ParseOwn(o.Bytes())
		if err != nil || string(back.Bytes()) != string(o.Bytes()) {
			t.Errorf("the file does not read back: %v", err)
		}
	}
}

// TestNeverLowered: a stricter value is not a key to set, IPv6's source
// routing below 0 among them. A line hardn wrote whose key now reads above
// it takes that value when the file is written again; a value hardn never
// writes (BPF at 1, a strict reverse-path filter) is not taken.
func TestNeverLowered(t *testing.T) {
	own := sysctl.Header + "# before: net.ipv4.conf.eth0.rp_filter = 0\nnet.ipv4.conf.eth0.rp_filter = 2\n" +
		"# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n# before: kernel.unprivileged_bpf_disabled = 0\nkernel.unprivileged_bpf_disabled = 2\n"
	r := read(on(map[string]int{"kernel.kptr_restrict": 2, "kernel.unprivileged_bpf_disabled": 1, "kernel.perf_event_paranoid": 4, "net.ipv4.conf.*.send_redirects": 0, "fs.protected_fifos": 0,
		"net.ipv4.conf.all.rp_filter": 0, "net.ipv4.conf.eth0.rp_filter": 1, "net.ipv6.conf.all.accept_source_route": -1},
		map[string]string{sysctl.File: own}), systemd)
	got := string(r.Next().Bytes())
	want := sysctl.Header + "# before: net.ipv4.conf.eth0.rp_filter = 0\nnet.ipv4.conf.eth0.rp_filter = 2\n" +
		"# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 2\n# before: kernel.unprivileged_bpf_disabled = 0\nkernel.unprivileged_bpf_disabled = 2\n" +
		"# before: fs.protected_fifos = 0\nfs.protected_fifos = 1\n"
	if got != want {
		t.Errorf("file:\n%s\nwant:\n%s", got, want)
	}
	if cs := r.Changes(); !slices.Equal(cs, []sysctl.Change{{Key: "fs.protected_fifos", From: 0, To: 1}}) {
		t.Errorf("changes = %v", cs)
	}
	if sr := row(t, r, "Source routing"); sr.State != sysctl.Pass || sr.Spec.Words[-1] != "refused" {
		t.Errorf("Source routing = %+v", sr)
	}
	// IPv4 reads any value but 0 as accepted.
	if sr := row(t, read(on(map[string]int{"net.ipv4.conf.all.accept_source_route": -1}, nil), systemd), "Source routing"); sr.State != sysctl.Below {
		t.Errorf("IPv4 source routing at -1 = %+v", sr)
	}
}

// TestLinesLeftOut: written again, the file leaves out a line that names a
// key the kernel no longer has (an interface that went away), and the
// lines of a row a later file now holds; the keys set now are the ones
// below the baseline alone.
func TestLinesLeftOut(t *testing.T) {
	fifos := "# before: fs.protected_fifos = 0\nfs.protected_fifos = 1\n"
	gone := sysctl.Header + "# before: net.ipv4.conf.eth0.rp_filter = 0\nnet.ipv4.conf.eth0.rp_filter = 2\n# before: net.ipv4.conf.wg0.rp_filter = 0\nnet.ipv4.conf.wg0.rp_filter = 2\n"
	over := map[string]int{"net.ipv4.conf.*.send_redirects": 0, "net.ipv4.conf.eth0.rp_filter": 2, "fs.protected_fifos": 0}
	r := read(on(over, map[string]string{sysctl.File: gone}), systemd)
	if got, want := string(r.Next().Bytes()), sysctl.Header+"# before: net.ipv4.conf.eth0.rp_filter = 0\nnet.ipv4.conf.eth0.rp_filter = 2\n"+fifos; got != want {
		t.Errorf("an interface gone, procps:\n%s", got)
	}
	gone = sysctl.Header + "# before: net.ipv6.conf.tun0.accept_redirects = 1\nnet.ipv6.conf.tun0.accept_redirects = 0\n"
	r = read(on(over, map[string]string{sysctl.File: gone}), openRC)
	if got := string(r.Next().Bytes()); got != sysctl.Header+fifos {
		t.Errorf("an interface gone, busybox:\n%s", got)
	}

	held := sysctl.Header + "# before: net.ipv4.conf.all.accept_redirects = 1\n# before: net.ipv4.conf.eth0.accept_redirects = 1\nnet.ipv4.conf.*.accept_redirects = 0\n"
	over = map[string]int{"net.ipv4.conf.*.send_redirects": 0, "net.ipv4.conf.all.accept_redirects": 1, "fs.protected_fifos": 0}
	r = read(on(over, map[string]string{sysctl.File: held, "/etc/sysctl.d/99-sysctl.conf": "net.ipv4.conf.all.accept_redirects = 1\n"}), systemd)
	if row(t, r, "ICMP redirects").State != sysctl.Held {
		t.Fatalf("not held: %+v", row(t, r, "ICMP redirects"))
	}
	if got := string(r.Next().Bytes()); got != sysctl.Header+fifos {
		t.Errorf("a held row:\n%s", got)
	}
	if cs := r.Changes(); len(cs) != 1 || cs[0].Key != "fs.protected_fifos" {
		t.Errorf("changes = %v", cs)
	}
}

// TestOwn: hardn's file is read back with its records; a file without the
// mark is not hardn's; a marked file with a line hardn does not write, or a
// key it never writes, is ErrMalformed.
func TestOwn(t *testing.T) {
	text := sysctl.Header + "# before: net.ipv4.conf.all.send_redirects = 1\n# before: net.ipv4.conf.eth0.send_redirects = 1\nnet.ipv4.conf.*.send_redirects = 0\n"
	r := read(on(map[string]int{"net.ipv4.conf.*.send_redirects": 0}, map[string]string{sysctl.File: text}), systemd)
	if r.Own == nil || r.Foreign || r.Err != nil || len(r.Own.Entries) != 1 {
		t.Fatalf("own = %+v, %v", r.Own, r.Err)
	}
	if back := r.Own.Restore(); len(back) != 2 || back[1] != (sysctl.KV{Key: "net.ipv4.conf.eth0.send_redirects", N: 1}) {
		t.Errorf("restore = %v", back)
	}
	if cs := r.Changes(); len(cs) != 0 {
		t.Errorf("changes = %v", cs)
	}
	if string(r.Next().Bytes()) != text {
		t.Errorf("nothing to set, and the file changes:\n%s", r.Next().Bytes())
	}
	if r := read(on(nil, map[string]string{sysctl.File: "net.ipv4.ip_forward = 1\n"}), systemd); !r.Foreign || r.Own != nil || r.Err != nil {
		t.Errorf("a file of the host's: %+v", r)
	}
	for _, bad := range []string{"net.ipv4.ip_forward = 0\n", "# before: net.ipv4.ip_forward = 1\nkernel.kptr_restrict = 1\n", "# a note\n", "kernel.kptr_restrict=1\n",
		"# before: kernel.kptr_restrict = 0\n", "net.ipv4.conf.all.rp_filter = 2\n", "# before: net.ipv4.conf.*.send_redirects = 1\nnet.ipv4.conf.*.send_redirects = 0\n"} {
		r := read(on(nil, map[string]string{sysctl.File: sysctl.Header + bad}), systemd)
		if !errors.Is(r.Err, sysctl.ErrMalformed) || r.Own != nil {
			t.Errorf("%q: %v", bad, r.Err)
		}
	}
	if _, err := sysctl.ParseOwn(nil); !errors.Is(err, sysctl.ErrMalformed) {
		t.Errorf("no mark: %v", err)
	}
}

// TestProcPath: sysctl's "." is the path's "/", and an interface's "." is
// written "/".
func TestProcPath(t *testing.T) {
	if got := sysctl.ProcPath("net.ipv4.conf.eth0/100.rp_filter"); got != "/proc/sys/net/ipv4/conf/eth0.100/rp_filter" {
		t.Error(got)
	}
	f := on(nil, map[string]string{"/proc/sys/net/ipv4/conf/eth0.100/rp_filter": "0\n", "/proc/sys/net/ipv4/conf/eth0.100/accept_redirects": "1\n"})
	f.AddFile("/proc/sys/net/ipv4/conf/all/rp_filter", []byte("0\n"), 0o644, 0, 0)
	r := read(f, openRC)
	if text := string(r.Next().Bytes()); !strings.Contains(text, "net.ipv4.conf.eth0/100.rp_filter = 2\n") {
		t.Errorf("file:\n%s", text)
	}
	if v := sysctl.ReadValue(context.Background(), f, "no.such.key"); !errors.Is(v.Err, fs.ErrNotExist) {
		t.Errorf("no such key: %v", v.Err)
	}
}

// TestOwnSettings: hardn's file counted by setting, not by line: Source
// routing's two keys are one, a per-interface setting written as a line
// for each interface is one, and a key that is no setting's is none.
func TestOwnSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		want int
	}{
		{"no line", nil, 0},
		{"one plain key", []string{sysctl.PtraceKey}, 1},
		{"source routing's two keys", []string{"net.ipv4.conf.all.accept_source_route", "net.ipv6.conf.all.accept_source_route"}, 1},
		{"a glob", []string{"net.ipv4.conf.*.accept_redirects"}, 1},
		{"a line an interface (busybox)", []string{"net.ipv4.conf.all.accept_redirects", "net.ipv4.conf.default.accept_redirects", "net.ipv4.conf.eth0.accept_redirects"}, 1},
		{"two families are two settings", []string{"net.ipv4.conf.*.accept_redirects", "net.ipv6.conf.*.accept_redirects"}, 2},
		{"several", []string{"net.ipv4.conf.*.accept_redirects", "net.ipv4.conf.*.send_redirects", "kernel.kptr_restrict", "fs.protected_fifos", sysctl.PtraceKey, "kernel.dmesg_restrict"}, 6},
		{"a key of no setting", []string{"net.ipv4.ip_forward", "kernel.kptr_restrict"}, 1},
	} {
		var o sysctl.Own
		for _, k := range tc.keys {
			o.Entries = append(o.Entries, sysctl.Entry{Key: k, N: 1})
		}
		if got := o.Settings(); got != tc.want {
			t.Errorf("%s: %d settings, want %d", tc.name, got, tc.want)
		}
	}
}
