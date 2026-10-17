package migrate

import (
	"bytes"
	"context"
	"flag"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

// Every host's run directory and backup store, in the fake only.
const (
	runDir     = "/run/hardn/test-1"
	backupRoot = "/var/backups/hardn"
)

var (
	debian13   = detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported}
	ubuntu2604 = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	alpine324  = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}

	socketGen   = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes}
	socketNoGen = detect.SSH{Mode: detect.SSHSocket, Generator: detect.No}
	service     = detect.SSH{Mode: detect.SSHService}
	openrc      = detect.SSH{Mode: detect.SSHOpenRC}
)

// stockMain is shaped like Debian's and Ubuntu's sshd_config: the Include
// first, then a few stock settings.
const stockMain = "# synthetic sshd_config, shaped like Debian's\n\n" + sshd.IncludeLine + "\n\n" +
	"KbdInteractiveAuthentication no\nUsePAM yes\nX11Forwarding yes\nSubsystem sftp /usr/lib/openssh/sftp-server\n"

// cloudInit is the drop-in cloud-init writes on an Ubuntu ISO install
// without imported keys.
const cloudInit = "PasswordAuthentication yes\n"

// fixture reads testdata/legacy/name.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "legacy", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// golden compares got with testdata/golden/name, or rewrites it with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

// testHost is a fake host whose sshd is a small model of sshd's reading
// (modelT): enough to show what the migration does to the effective values.
type testHost struct {
	*hostfake.System
	failT int // fail the failT-th sshd -t (1-based); 0: never
	tRuns int
}

func newHost(t *testing.T, files map[string]string) *testHost {
	t.Helper()
	h := &testHost{System: hostfake.New(host.Options{})}
	h.AddDir(runDir, 0o700, 0, 0)
	h.AddDir(sshd.PrivsepDir, 0o755, 0, 0)
	h.AddDir(backupRoot, 0o700, 0, 0)
	h.AddDir(sshd.DropInDir, 0o755, 0, 0)
	for p, d := range files {
		h.AddFile(p, []byte(d), 0o644, 0, 0)
	}
	h.Handler = h.sshd
	return h
}

// sshd answers sshd -t and -T over the validation copy; anything else is
// unscripted.
func (h *testHost) sshd(argv []string, _ []byte) hostfake.Response {
	if len(argv) < 4 || argv[0] != "sshd" || argv[2] != "-f" || argv[3] == sshd.MainConfig {
		return hostfake.Response{Err: hostfake.ErrUnscripted}
	}
	main, drops := h.copyFiles(argv[3])
	switch argv[1] {
	case "-t":
		h.tRuns++
		if h.tRuns == h.failT || !modelValid(main, drops) {
			return hostfake.Response{Stderr: argv[3] + " line 1: Bad configuration option: model\r\n", ExitCode: 255}
		}
		return hostfake.Response{}
	case "-T":
		return hostfake.Response{Stdout: modelT(main, drops)}
	}
	return hostfake.Response{Err: hostfake.ErrUnscripted}
}

// copyFiles reads Validate's copy, with its Include and drop-in paths
// mapped back to the real ones.
func (h *testHost) copyFiles(cfg string) ([]byte, []sshd.File) {
	work := path.Dir(cfg)
	e, _ := h.Entry(cfg)
	main := strings.ReplaceAll(string(e.Data), "Include "+work+"/sshd_config.d/*.conf", sshd.IncludeLine)
	entries, _ := h.ReadDir(context.Background(), work+"/sshd_config.d")
	var drops []sshd.File
	for _, de := range entries {
		d, _ := h.Entry(work + "/sshd_config.d/" + de.Name())
		drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: d.Data})
	}
	return []byte(main), drops
}

// installed is the host's configuration as sshd reads it now.
func (h *testHost) installed(t *testing.T) ([]byte, []sshd.File) {
	t.Helper()
	e, ok := h.Entry(sshd.MainConfig)
	if !ok {
		t.Fatal("no sshd_config")
	}
	var drops []sshd.File
	entries, _ := h.ReadDir(context.Background(), sshd.DropInDir)
	for _, de := range entries {
		if d, ok := h.Entry(sshd.DropInDir + "/" + de.Name()); ok && included(de.Name()) {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: d.Data})
		}
	}
	return e.Data, drops
}

// before is what sshd.ReadEffective would give for the installed files,
// by the model, with ssh's facts and socket as systemctl show -p Listen
// ssh.socket's output.
func (h *testHost) before(t *testing.T, ssh detect.SSH, socket string) sshd.Effective {
	t.Helper()
	main, drops := h.installed(t)
	e, _ := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(modelT(main, drops)), SSH: ssh, Socket: []byte(socket)})
	return e
}

// modelValid is the model's sshd -t: an accumulating keyword with no
// argument is fatal (v0.3.x's "AllowUsers " with an empty list).
func modelValid(main []byte, drops []sshd.File) bool {
	return !slices.ContainsFunc(sshd.ReadOrder(main, drops), func(l sshd.Line) bool {
		return sshd.Accumulates(l.Keyword) && len(l.Args) == 0
	})
}

// modelT is sshd -T's output by a model of sshd's reading: the first value
// of each keyword outside Match blocks, every value of an accumulating
// keyword, sshd's defaults for the rest, and the listeners from Port and
// ListenAddress (each ListenAddress without a port on every Port, a host
// name as 192.0.2.80; with none, every Port on 0.0.0.0 and ::).
func modelT(main []byte, drops []sshd.File) string {
	first, acc := map[string]string{}, map[string][]string{}
	for _, l := range sshd.ReadOrder(main, drops) {
		switch {
		case l.Match != nil:
		case sshd.Accumulates(l.Keyword):
			acc[l.Keyword] = append(acc[l.Keyword], l.Args...)
		default:
			if _, ok := first[l.Keyword]; !ok {
				first[l.Keyword] = strings.Join(l.Args, " ")
			}
		}
	}
	var b strings.Builder
	ports := acc["port"]
	if len(ports) == 0 {
		ports = []string{"22"}
	}
	for _, p := range ports {
		b.WriteString("port " + p + "\n")
	}
	listen := func(a netip.Addr, p string) {
		port, _ := netip.ParseAddrPort("0.0.0.0:" + p)
		b.WriteString("listenaddress " + netip.AddrPortFrom(a, port.Port()).String() + "\n")
	}
	if las := acc["listenaddress"]; len(las) > 0 {
		for _, la := range las {
			if ap, err := netip.ParseAddrPort(la); err == nil {
				b.WriteString("listenaddress " + ap.String() + "\n")
				continue
			}
			a, err := netip.ParseAddr(strings.Trim(la, "[]"))
			if err != nil { // a host name: sshd resolves it; the model, to a documentation address
				a = netip.MustParseAddr("192.0.2.80")
			}
			for _, p := range ports {
				listen(a, p)
			}
		}
	} else {
		for _, p := range ports {
			listen(netip.IPv6Unspecified(), p)
			listen(netip.IPv4Unspecified(), p)
		}
	}
	defaults := []struct{ k, v string }{
		{"permitrootlogin", "prohibit-password"}, {"pubkeyauthentication", "yes"}, {"passwordauthentication", "yes"},
		{"kbdinteractiveauthentication", "yes"}, {"permitemptypasswords", "no"}, {"usepam", "no"},
		{"authorizedkeysfile", ".ssh/authorized_keys .ssh/authorized_keys2"}, {"authenticationmethods", "any"},
		{"ciphers", "chacha20-poly1305@openssh.com,aes256-gcm@openssh.com"}, {"allowtcpforwarding", "yes"},
	}
	for _, d := range defaults {
		v, ok := first[d.k]
		if !ok {
			v = d.v
		}
		if d.k != "authorizedkeysfile" {
			v = strings.ToLower(v)
		}
		b.WriteString(d.k + " " + v + "\n")
	}
	for _, k := range []string{"allowusers", "allowgroups", "denyusers", "denygroups"} {
		for _, v := range acc[k] {
			b.WriteString(k + " " + v + "\n")
		}
	}
	return b.String()
}

// readOnly reports whether c only read the host.
func readOnly(c hostfake.Call) bool {
	return slices.Contains([]string{"read", "read-nofollow", "readdir", "stat", "lstat", "readlink", "resolve-symlink"}, c.Op)
}
