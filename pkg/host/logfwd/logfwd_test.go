package logfwd_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/logfwd"
)

var (
	tcp  = logfwd.Server{Address: "192.0.2.50", Port: "514", Proto: logfwd.TCP}
	udp  = logfwd.Server{Address: "192.0.2.50", Port: "514", Proto: logfwd.UDP}
	udp2 = logfwd.Server{Address: "192.0.2.52", Port: "514", Proto: logfwd.UDP}
	tls  = logfwd.Server{Address: "logs.example.net", Port: "6514", Proto: logfwd.TLS, CAFile: "/etc/ssl/lab-ca.pem"}
)

func facts(d detect.Distro) detect.Facts {
	f := detect.Facts{Platform: detect.Platform{Distro: d}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}
	if d == detect.DistroAlpine {
		f.Init.Kind = detect.OpenRC
	}
	return f
}

const dpkgFormat = "--showformat=${Status}\\t${Version}\\n"

// debian is a fake Debian or Ubuntu host, rsyslog installed or not.
func debian(t *testing.T, rsyslog bool) *hostfake.System {
	t.Helper()
	h := hostfake.New(host.Options{})
	r := hostfake.Response{Stdout: "install ok installed\t8.2312.0\n"}
	if !rsyslog {
		r = hostfake.Response{Stderr: "dpkg-query: no packages found matching rsyslog\n", ExitCode: 1}
	}
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		if argv[0] == "dpkg-query" {
			return r
		}
		return hostfake.Response{ExitCode: 127}
	}
	return h
}

func file(t *testing.T, s ...logfwd.Server) string {
	t.Helper()
	b, err := logfwd.RsyslogFile(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadRsyslog(t *testing.T) {
	const stock = "module(load=\"imuxsock\")\n$IncludeConfig /etc/rsyslog.d/*.conf\n"
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  []logfwd.Forward
		bad   error
	}{
		{name: "nothing forwards", files: map[string]string{logfwd.RsyslogConf: stock, "/etc/rsyslog.d/50-default.conf": "*.* /var/log/syslog\nmail.* -/var/log/mail.log\n"}},
		{name: "no files at all"},
		{name: "hardn tcp", files: map[string]string{logfwd.RsyslogConf: stock, logfwd.DropIn: file(t, tcp)},
			want: []logfwd.Forward{{Server: tcp, File: logfwd.DropIn, Line: 2, ByHardn: true}}},
		{name: "hardn udp", files: map[string]string{logfwd.DropIn: file(t, udp)},
			want: []logfwd.Forward{{Server: udp, File: logfwd.DropIn, Line: 2, ByHardn: true}}},
		{name: "hardn tls", files: map[string]string{logfwd.DropIn: file(t, tls)},
			want: []logfwd.Forward{{Server: tls, File: logfwd.DropIn, Line: 2, ByHardn: true}}},
		{name: "hardn's name, not its header", files: map[string]string{logfwd.DropIn: "*.* @@192.0.2.50\n"},
			want: []logfwd.Forward{{Server: tcp, File: logfwd.DropIn, Line: 1}}},
		{name: "modern, several lines, any case", files: map[string]string{"/etc/rsyslog.d/50-remote.conf": "# to the lab\nif $syslogseverity <= 4 then {\n  Action(Type=\"omfwd\"\n    Target=\"192.0.2.50\" # the server\n    Port=\"514\" Protocol=\"TCP\")\n}\n"},
			want: []logfwd.Forward{{Server: tcp, File: "/etc/rsyslog.d/50-remote.conf", Line: 3}}},
		{name: "modern defaults", files: map[string]string{"/etc/rsyslog.d/50-remote.conf": "action(type=\"omfwd\" target=\"192.0.2.50\")\n"},
			want: []logfwd.Forward{{Server: udp, File: "/etc/rsyslog.d/50-remote.conf", Line: 1}}},
		{name: "another action", files: map[string]string{logfwd.RsyslogConf: "*.* action(type=\"omfile\" file=\"/var/log/all (1).log\")\n"}},
		{name: "legacy forms", files: map[string]string{logfwd.RsyslogConf: "*.*  @192.0.2.50\nauth.* @@192.0.2.60:1514\n*.* @@(o,z9)logs.example.net:1470;RSYSLOG_ForwardFormat\n*.* @[2001:db8::1]:515\n#*.* @192.0.2.99\n"},
			want: []logfwd.Forward{
				{Server: udp, File: logfwd.RsyslogConf, Line: 1},
				{Server: logfwd.Server{Address: "192.0.2.60", Port: "1514", Proto: logfwd.TCP}, File: logfwd.RsyslogConf, Line: 2},
				{Server: logfwd.Server{Address: "logs.example.net", Port: "1470", Proto: logfwd.TCP}, File: logfwd.RsyslogConf, Line: 3},
				{Server: logfwd.Server{Address: "2001:db8::1", Port: "515", Proto: logfwd.UDP}, File: logfwd.RsyslogConf, Line: 4},
			}},
		{name: "legacy tls, set in an earlier file", files: map[string]string{logfwd.RsyslogConf: "$ActionSendStreamDriverMode 1\n", "/etc/rsyslog.d/50-remote.conf": "*.* @@logs.example.net:6514\n"},
			want: []logfwd.Forward{{Server: logfwd.Server{Address: "logs.example.net", Port: "6514", Proto: logfwd.TLS}, File: "/etc/rsyslog.d/50-remote.conf", Line: 1}}},
		{name: "foreign before hardn's", files: map[string]string{"/etc/rsyslog.d/50-remote.conf": "*.* @@192.0.2.60\n", logfwd.DropIn: file(t, tcp)},
			want: []logfwd.Forward{
				{Server: logfwd.Server{Address: "192.0.2.60", Port: "514", Proto: logfwd.TCP}, File: "/etc/rsyslog.d/50-remote.conf", Line: 1},
				{Server: tcp, File: logfwd.DropIn, Line: 2, ByHardn: true}}},
		{name: "not a .conf", files: map[string]string{"/etc/rsyslog.d/50-remote.conf.bak": "*.* @@192.0.2.60\n"}},
		{name: "malformed: unclosed action", files: map[string]string{"/etc/rsyslog.d/50-remote.conf": "action(type=\"omfwd\" target=\"192.0.2.50\"\n"}, bad: logfwd.ErrMalformed},
		{name: "malformed: no target", files: map[string]string{"/etc/rsyslog.d/50-remote.conf": "action(type=\"omfwd\" port=\"514\")\n"}, bad: logfwd.ErrMalformed},
		{name: "malformed: no host", files: map[string]string{"/etc/rsyslog.d/50-remote.conf": "*.* @@:514\n"}, bad: logfwd.ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := debian(t, true)
			for name, data := range tc.files {
				h.AddFile(name, []byte(data), 0o644, 0, 0)
			}
			r := logfwd.Read(context.Background(), h, facts(detect.DistroUbuntu))
			if r.Logger != logfwd.Rsyslog || !errors.Is(r.Err, tc.bad) || (tc.bad == nil && r.Err != nil) {
				t.Fatalf("Read = %+v, want rsyslog and error %v", r, tc.bad)
			}
			if len(r.Forwards) != len(tc.want) {
				t.Fatalf("forwards = %+v, want %+v", r.Forwards, tc.want)
			}
			for i, w := range tc.want {
				if r.Forwards[i] != w {
					t.Errorf("forward %d = %+v, want %+v", i, r.Forwards[i], w)
				}
			}
		})
	}
}

func TestShownPrefersHardn(t *testing.T) {
	h := debian(t, true)
	h.AddFile("/etc/rsyslog.d/50-remote.conf", []byte("*.* @@192.0.2.60\n"), 0o644, 0, 0)
	h.AddFile(logfwd.DropIn, []byte(file(t, tcp, udp2)), 0o644, 0, 0)
	r := logfwd.Read(context.Background(), h, facts(detect.DistroDebian))
	own := r.Own()
	if len(own) != 2 || own[0].Server != tcp || own[1].Server != udp2 || own[0].Line != 2 || own[1].Line != 6 || !slices.Equal(r.Shown(), own) {
		t.Fatalf("Own = %+v, Shown = %+v; want hardn's two", own, r.Shown())
	}
	if f := r.Foreign(); f == nil || f.Server.Address != "192.0.2.60" {
		t.Fatalf("Foreign = %+v", f)
	}
	h = debian(t, true)
	h.AddFile("/etc/rsyslog.d/50-remote.conf", []byte("*.* @@192.0.2.60\n*.* @192.0.2.61\n"), 0o644, 0, 0)
	r = logfwd.Read(context.Background(), h, facts(detect.DistroDebian))
	if len(r.Own()) != 0 || len(r.Shown()) != 2 {
		t.Fatalf("with none of hardn's: Own = %+v, Shown = %+v; want every one shown", r.Own(), r.Shown())
	}
}

func TestReadUnreadable(t *testing.T) {
	h := debian(t, true)
	h.AddFile(logfwd.RsyslogConf, []byte("x"), 0o644, 0, 0)
	h.AddSymlink("/etc/rsyslog.d/50-loop.conf", "/etc/rsyslog.d/50-loop.conf")
	r := logfwd.Read(context.Background(), h, facts(detect.DistroUbuntu))
	if r.Logger != logfwd.Rsyslog || r.Err == nil || r.Forwards != nil {
		t.Fatalf("Read = %+v, want an error for the file that cannot be read", r)
	}
}

func TestReadLogger(t *testing.T) {
	ctx := context.Background()
	if r := logfwd.Read(ctx, debian(t, false), facts(detect.DistroDebian)); r.Logger != logfwd.Journald || r.Err != nil || r.NoInstall {
		t.Errorf("Debian without rsyslog = %+v, want journald alone", r)
	}
	if r := logfwd.Read(ctx, debian(t, false), facts(detect.DistroProxmox)); r.Logger != logfwd.Journald || r.Err != nil || r.NoInstall {
		t.Errorf("Proxmox VE without rsyslog = %+v, want journald alone, rsyslog installable", r)
	}
	noInit := facts(detect.DistroDebian)
	noInit.Init = detect.Init{}
	if r := logfwd.Read(ctx, debian(t, false), noInit); r.Logger != logfwd.LoggerUnknown || !errors.Is(r.Err, logfwd.ErrNoLogger) {
		t.Errorf("no rsyslog and no systemd = %+v, want ErrNoLogger", r)
	}
	if r := logfwd.Read(ctx, debian(t, true), facts("arch")); !errors.Is(r.Err, logfwd.ErrNoLogger) {
		t.Errorf("another distribution = %+v, want ErrNoLogger", r)
	}
	half := hostfake.New(host.Options{})
	half.Respond(hostfake.Response{Stdout: "install ok half-installed\t8.2312.0\n"}, "dpkg-query", "--show", dpkgFormat, "rsyslog")
	if r := logfwd.Read(ctx, half, facts(detect.DistroDebian)); r.Logger != logfwd.LoggerUnknown || r.Err == nil {
		t.Errorf("rsyslog half installed = %+v, want unknown with an error", r)
	}
	el := hostfake.New(host.Options{})
	el.Respond(hostfake.Response{Stdout: "rsyslog-8.2604.0-1.el10.x86_64\n"}, "rpm", "-q", "rsyslog")
	if r := logfwd.Read(ctx, el, facts(detect.DistroRocky)); r.Logger != logfwd.Rsyslog || r.Err != nil {
		t.Errorf("Rocky Linux = %+v, want rsyslog", r)
	}
}

// apkDB is apk's database on an Alpine host without rsyslog.
const apkDB = "P:busybox\nV:1.37.0-r30\n\n"

func alpineHost(t *testing.T, confd string) *hostfake.System {
	t.Helper()
	h := hostfake.New(host.Options{})
	h.AddFile(alpine.InstalledDB, []byte(apkDB), 0o644, 0, 0)
	if confd != "" {
		h.AddFile(logfwd.ConfD, []byte(confd), 0o644, 0, 0)
	}
	return h
}

func TestReadBusybox(t *testing.T) {
	set, err := logfwd.SetOpts([]byte("SYSLOGD_OPTS=\"-t\"\n"), []logfwd.Server{udp})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, confd string
		want        []logfwd.Forward
		bad         error
	}{
		{name: "stock", confd: "SYSLOGD_OPTS=\"-t\"\n"},
		{name: "hardn's", confd: string(set), want: []logfwd.Forward{{Server: udp, File: logfwd.ConfD, Line: 2, ByHardn: true}}},
		{name: "foreign", confd: "# remote\nSYSLOGD_OPTS=\"-t -R 192.0.2.60:1514 -L\"\n",
			want: []logfwd.Forward{{Server: logfwd.Server{Address: "192.0.2.60", Port: "1514"}, File: logfwd.ConfD, Line: 2}}},
		{name: "foreign, joined, single quotes, no port", confd: "SYSLOGD_OPTS='-Rlogs.example.net'\n",
			want: []logfwd.Forward{{Server: logfwd.Server{Address: "logs.example.net", Port: "514"}, File: logfwd.ConfD, Line: 1}}},
		{name: "bare", confd: "SYSLOGD_OPTS=-t\n"},
		{name: "mark whose words are gone", confd: logfwd.Mark + "-R 192.0.2.50:514 -L\nSYSLOGD_OPTS=\"-t -R 192.0.2.60:514\"\n",
			want: []logfwd.Forward{{Server: logfwd.Server{Address: "192.0.2.60", Port: "514"}, File: logfwd.ConfD, Line: 2}}},
		{name: "no file", bad: fs.ErrNotExist},
		{name: "not set", confd: "# SYSLOGD_OPTS=\"-t\"\n", bad: logfwd.ErrMalformed},
		{name: "set twice", confd: "SYSLOGD_OPTS=\"-t\"\nSYSLOGD_OPTS=\"-S\"\n", bad: logfwd.ErrMalformed},
		{name: "expansion", confd: "SYSLOGD_OPTS=\"-t $EXTRA\"\n", bad: logfwd.ErrMalformed},
		{name: "unclosed quote", confd: "SYSLOGD_OPTS=\"-t\n", bad: logfwd.ErrMalformed},
		{name: "-R with no host", confd: "SYSLOGD_OPTS=\"-R :514\"\n", bad: logfwd.ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := logfwd.Read(context.Background(), alpineHost(t, tc.confd), facts(detect.DistroAlpine))
			if r.Logger != logfwd.Busybox || !errors.Is(r.Err, tc.bad) || (tc.bad == nil && r.Err != nil) {
				t.Fatalf("Read = %+v, want busybox and error %v", r, tc.bad)
			}
			if len(r.Forwards) != len(tc.want) {
				t.Fatalf("forwards = %+v, want %+v", r.Forwards, tc.want)
			}
			for i, w := range tc.want {
				if r.Forwards[i] != w {
					t.Errorf("forward %d = %+v, want %+v", i, r.Forwards[i], w)
				}
			}
		})
	}
}

// Alpine with rsyslog installed, or apk's database not read: which logger
// runs is not established, and syslogd's options are not read as the
// host's forwarding.
func TestReadAlpineLogger(t *testing.T) {
	ctx := context.Background()
	const forwarding = "SYSLOGD_OPTS=\"-R 192.0.2.60\"\n"
	two := alpineHost(t, forwarding)
	two.AddFile(alpine.InstalledDB, []byte(apkDB+"P:rsyslog\nV:8.2410.0-r1\n\n"), 0o644, 0, 0)
	if r := logfwd.Read(ctx, two, facts(detect.DistroAlpine)); r.Logger != logfwd.LoggerUnknown || !errors.Is(r.Err, logfwd.ErrTwoLoggers) || r.Forwards != nil {
		t.Errorf("rsyslog installed = %+v, want ErrTwoLoggers and nothing read", r)
	}
	noDB := hostfake.New(host.Options{})
	noDB.AddFile(logfwd.ConfD, []byte(forwarding), 0o644, 0, 0)
	if r := logfwd.Read(ctx, noDB, facts(detect.DistroAlpine)); r.Logger != logfwd.LoggerUnknown || !errors.Is(r.Err, fs.ErrNotExist) || r.Forwards != nil {
		t.Errorf("no database = %+v, want unknown with the error", r)
	}
}

// An administrator's own -R, put before hardn's, stays the administrator's:
// hardn's flag and address are matched as a pair.
func TestOwnPair(t *testing.T) {
	const confd = logfwd.Mark + "-R 192.0.2.50:514 -L\nSYSLOGD_OPTS=\"-R 10.0.0.9:514 -t -R 192.0.2.50:514 -L\"\n"
	r := logfwd.Read(context.Background(), alpineHost(t, confd), facts(detect.DistroAlpine))
	want := []logfwd.Forward{
		{Server: logfwd.Server{Address: "10.0.0.9", Port: "514"}, File: logfwd.ConfD, Line: 2},
		{Server: udp, File: logfwd.ConfD, Line: 2, ByHardn: true},
	}
	if r.Err != nil || !slices.Equal(r.Forwards, want) {
		t.Fatalf("Read = %+v, want %+v", r, want)
	}
	back, err := logfwd.ClearOpts([]byte(confd))
	if want := "SYSLOGD_OPTS=\"-R 10.0.0.9:514 -t\"\n"; err != nil || string(back) != want {
		t.Fatalf("ClearOpts = %q, %v; want %q", back, err, want)
	}
	// hardn's address after another's flag is not hardn's pair.
	for _, opts := range []string{"-R 10.0.0.9:514 192.0.2.50:514 -L", "-R192.0.2.50:514 -L", "-R -R 192.0.2.50:514 -L"} {
		gone := logfwd.Mark + "-R 192.0.2.50:514 -L\nSYSLOGD_OPTS=\"" + opts + "\"\n"
		if out, err := logfwd.ClearOpts([]byte(gone)); !errors.Is(err, logfwd.ErrNotOwn) {
			t.Errorf("ClearOpts of %q = %q, %v; want ErrNotOwn", opts, out, err)
		}
	}
}

func TestSetAndClearOpts(t *testing.T) {
	const stock = "# Alpine's\nSYSLOGD_OPTS=\"-t\"\n"
	set, err := logfwd.SetOpts([]byte(stock), []logfwd.Server{udp})
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Alpine's\n" + logfwd.Mark + "-R 192.0.2.50:514 -L\nSYSLOGD_OPTS=\"-t -R 192.0.2.50:514 -L\"\n"; string(set) != want {
		t.Fatalf("SetOpts =\n%s\nwant\n%s", set, want)
	}
	back, err := logfwd.ClearOpts(set)
	if err != nil || string(back) != stock {
		t.Fatalf("ClearOpts = %q, %v; want the file as it was", back, err)
	}
	// -L there before is the administrator's: not added, not taken out.
	kept, err := logfwd.SetOpts([]byte("SYSLOGD_OPTS=\"-L -t\"\n"), []logfwd.Server{udp})
	if err != nil || !strings.Contains(string(kept), logfwd.Mark+"-R 192.0.2.50:514\n") {
		t.Fatalf("SetOpts with -L = %q, %v", kept, err)
	}
	if back, err := logfwd.ClearOpts(kept); err != nil || string(back) != "SYSLOGD_OPTS=\"-L -t\"\n" {
		t.Fatalf("ClearOpts with -L = %q, %v", back, err)
	}
	// A second server replaces hardn's first.
	moved, err := logfwd.SetOpts(set, []logfwd.Server{{Address: "192.0.2.60", Port: "514"}})
	if err != nil || strings.Contains(string(moved), "192.0.2.50") || strings.Count(string(moved), logfwd.Mark) != 1 {
		t.Fatalf("SetOpts over hardn's = %q, %v", moved, err)
	}
	if _, err := logfwd.SetOpts([]byte("SYSLOGD_OPTS=\"-R 192.0.2.60\"\n"), []logfwd.Server{udp}); !errors.Is(err, logfwd.ErrForeign) {
		t.Errorf("SetOpts over a foreign -R: %v, want ErrForeign", err)
	}
	if _, err := logfwd.SetOpts([]byte(stock), []logfwd.Server{tcp}); !errors.Is(err, logfwd.ErrServer) {
		t.Errorf("SetOpts with TCP: %v, want ErrServer", err)
	}
	if _, err := logfwd.ClearOpts([]byte(stock)); !errors.Is(err, logfwd.ErrNotOwn) {
		t.Errorf("ClearOpts with no mark: %v, want ErrNotOwn", err)
	}
	edited := strings.Replace(string(set), "192.0.2.50:514 -L\"", "192.0.2.77:514 -L\"", 1)
	if _, err := logfwd.ClearOpts([]byte(edited)); !errors.Is(err, logfwd.ErrNotOwn) {
		t.Errorf("ClearOpts after an edit: %v, want ErrNotOwn", err)
	}
}

// Nothing but plain characters reaches the logger's settings.
func TestServerCheck(t *testing.T) {
	for _, s := range []logfwd.Server{
		{Address: "192.0.2.50\" port=\"1", Port: "514"},
		{Address: "logs example", Port: "514"},
		{Address: "logs\n*.* @evil", Port: "514"},
		{Address: "-R", Port: "514"},
		{Address: "$(reboot)", Port: "514"},
		{Address: "Logs.Example.Net", Port: "514"},
		{Address: "", Port: "514"},
		{Address: "192.0.2.50", Port: "514 -O /etc/passwd"},
		{Address: "192.0.2.50", Port: "0"},
		{Address: "192.0.2.50", Port: "65536"},
		{Address: "192.0.2.50", Port: "0514"},
		{Address: "192.0.2.50", Port: "514", Proto: logfwd.TCP, CAFile: "/etc/ssl/ca.pem"},
		{Address: "logs.example.net", Port: "6514", Proto: logfwd.TLS},
		{Address: "logs.example.net", Port: "6514", Proto: logfwd.TLS, CAFile: "/etc/ssl/a\" b.pem"},
		{Address: "logs.example.net", Port: "6514", Proto: logfwd.TLS, CAFile: "ca.pem"},
		{Address: "192.0.2.50", Port: "514", Proto: 9},
	} {
		if err := s.Check(); !errors.Is(err, logfwd.ErrServer) {
			t.Errorf("Check(%+v) = %v, want ErrServer", s, err)
		}
		// One bad server refuses the whole list, wherever it stands.
		for _, list := range [][]logfwd.Server{{s}, {udp, s}, {s, udp}} {
			if b, err := logfwd.RsyslogFile(list); err == nil {
				t.Errorf("RsyslogFile(%+v) wrote %q", list, b)
			}
			if b, err := logfwd.SetOpts([]byte("SYSLOGD_OPTS=\"-t\"\n"), list); err == nil {
				t.Errorf("SetOpts(%+v) wrote %q", list, b)
			}
		}
	}
	for _, s := range []logfwd.Server{tcp, udp, tls} {
		if err := s.Check(); err != nil {
			t.Errorf("Check(%+v) = %v", s, err)
		}
	}
}

// hardn's file (design-syslog-servers.md §2.2): one action per server, in
// order, each with its own queue, UDP too.
func TestRsyslogFile(t *testing.T) {
	const queue = "    queue.type=\"linkedList\" queue.timeoutEnqueue=\"0\"\n    action.resumeRetryCount=\"-1\")\n"
	one := logfwd.Header + "*.* action(type=\"omfwd\" protocol=\"tcp\"\n    target=\"192.0.2.50\" port=\"514\"\n" + queue
	if got := file(t, tcp); got != one {
		t.Errorf("tcp =\n%s\nwant\n%s", got, one)
	}
	want := one + "*.* action(type=\"omfwd\" protocol=\"udp\"\n    target=\"192.0.2.52\" port=\"514\"\n" + queue +
		"*.* action(type=\"omfwd\" protocol=\"tcp\"\n    target=\"logs.example.net\" port=\"6514\"\n" +
		"    StreamDriver=\"gtls\" StreamDriverMode=\"1\"\n    StreamDriverAuthMode=\"x509/name\"\n" +
		"    StreamDriverPermittedPeers=\"logs.example.net\"\n    StreamDriver.CAFile=\"/etc/ssl/lab-ca.pem\"\n" + queue
	if got := file(t, tcp, udp2, tls); got != want {
		t.Errorf("three =\n%s\nwant\n%s", got, want)
	}
	if _, err := logfwd.RsyslogFile(nil); !errors.Is(err, logfwd.ErrServer) {
		t.Errorf("no server: %v, want ErrServer", err)
	}
	// What hardn writes, hardn reads back: each server, hardn's own.
	h := debian(t, true)
	h.AddFile(logfwd.DropIn, []byte(want), 0o644, 0, 0)
	r := logfwd.Read(context.Background(), h, facts(detect.DistroDebian))
	if r.Err != nil || len(r.Forwards) != 3 || r.Forwards[0].Server != tcp || r.Forwards[1].Server != udp2 || r.Forwards[2].Server != tls || len(r.Own()) != 3 {
		t.Errorf("read back = %+v", r)
	}
}

// Several servers on Alpine (§2.3): one -R each, the mark naming them all,
// and ClearOpts takes every one back out.
func TestSetOptsSeveral(t *testing.T) {
	const stock = "SYSLOGD_OPTS=\"-t\"\n"
	set, err := logfwd.SetOpts([]byte(stock), []logfwd.Server{udp, udp2})
	if want := logfwd.Mark + "-R 192.0.2.50:514 -R 192.0.2.52:514 -L\nSYSLOGD_OPTS=\"-t -R 192.0.2.50:514 -R 192.0.2.52:514 -L\"\n"; err != nil || string(set) != want {
		t.Fatalf("SetOpts = %q, %v; want %q", set, err, want)
	}
	r := logfwd.Read(context.Background(), alpineHost(t, string(set)), facts(detect.DistroAlpine))
	if own := r.Own(); r.Err != nil || len(own) != 2 || own[0].Server != udp || own[1].Server != udp2 || r.Foreign() != nil {
		t.Fatalf("Read = %+v", r)
	}
	// The list changed is one change: the old words out, the new in.
	moved, err := logfwd.SetOpts(set, []logfwd.Server{udp2})
	if want := logfwd.Mark + "-R 192.0.2.52:514 -L\nSYSLOGD_OPTS=\"-t -R 192.0.2.52:514 -L\"\n"; err != nil || string(moved) != want {
		t.Fatalf("SetOpts over two = %q, %v; want %q", moved, err, want)
	}
	if back, err := logfwd.ClearOpts(set); err != nil || string(back) != stock {
		t.Fatalf("ClearOpts = %q, %v; want the file as it was", back, err)
	}
	// One TCP or TLS server refuses the list whole.
	for _, list := range [][]logfwd.Server{{udp, tcp}, {tls, udp}} {
		if out, err := logfwd.SetOpts([]byte(stock), list); !errors.Is(err, logfwd.ErrServer) || out != nil {
			t.Errorf("SetOpts(%+v) = %q, %v; want ErrServer", list, out, err)
		}
	}
	if _, err := logfwd.SetOpts([]byte(stock), nil); !errors.Is(err, logfwd.ErrServer) {
		t.Errorf("no server: %v, want ErrServer", err)
	}
}

func TestReadConns(t *testing.T) {
	const table = "ESTAB 0 0 192.0.2.10:22 192.0.2.1:50514 users:((\"sshd\",pid=700,fd=4))\n" +
		"ESTAB 0 0 192.0.2.10:41234 192.0.2.50:514 users:((\"rsyslogd\",pid=612,fd=9))\n" +
		"ESTAB 0 0 [2001:db8::10]:41236 [2001:db8::50]:6514 users:((\"rsyslogd\",pid=612,fd=10))\n" +
		"SYN-SENT 0 1 192.0.2.10:41238 192.0.2.60:514 users:((\"rsyslogd\",pid=612,fd=11))\n"
	ss := func(r hostfake.Response) *hostfake.System {
		h := hostfake.New(host.Options{})
		h.Respond(r, "ss", "-Htnp")
		return h
	}
	ctx := context.Background()
	open, down, untold := logfwd.ConnState{Conn: logfwd.ConnOpen}, logfwd.ConnState{Conn: logfwd.ConnDown}, logfwd.ConnState{Conn: logfwd.ConnUnknown}
	const to60, to77 = "ESTAB 0 0 192.0.2.10:41242 192.0.2.60:514 users:((\"rsyslogd\",pid=612,fd=13))\n",
		"ESTAB 0 0 192.0.2.10:41240 192.0.2.77:514 users:((\"rsyslogd\",pid=612,fd=12))\n"
	out60 := []logfwd.Server{{Address: "192.0.2.60", Port: "514", Proto: logfwd.TCP}}
	name := func(n, port string) logfwd.Server { return logfwd.Server{Address: n, Port: port, Proto: logfwd.TCP} }
	for _, tc := range []struct {
		name  string
		table string
		s     []logfwd.Server
		want  []logfwd.ConnState
	}{
		{"address, open", table, []logfwd.Server{tcp}, []logfwd.ConnState{open}},
		{"address, another port", table, []logfwd.Server{{Address: "192.0.2.50", Port: "1514", Proto: logfwd.TCP}}, []logfwd.ConnState{down}},
		{"address, still connecting", table, []logfwd.Server{{Address: "192.0.2.60", Port: "514", Proto: logfwd.TCP}}, []logfwd.ConnState{down}},
		{"name, rsyslogd's to that port", table, []logfwd.Server{tls}, []logfwd.ConnState{open}},
		{"name, no rsyslogd socket to that port", table, []logfwd.Server{name("logs.example.net", "50514")}, []logfwd.ConnState{down}},
		{"udp", table, []logfwd.Server{udp}, []logfwd.ConnState{{}}},
		{"mixed list", table, []logfwd.Server{tls, tcp, udp2, {Address: "192.0.2.60", Port: "514", Proto: logfwd.TCP}},
			[]logfwd.ConnState{open, open, {}, down}},
		// §2.4: beside 192.0.2.50 on port 514, a dead named server is not
		// Open on the address server's socket.
		{"name beside an address on its port, down", table, []logfwd.Server{tcp, name("logs.example.net", "514")}, []logfwd.ConnState{open, down}},
		{"name beside an address on its port, open", table + "ESTAB 0 0 192.0.2.10:41240 192.0.2.77:514 users:((\"rsyslogd\",pid=612,fd=12))\n",
			[]logfwd.Server{tcp, name("logs.example.net", "514")}, []logfwd.ConnState{open, open}},
		{"two names on a port, one socket", table, []logfwd.Server{name("a.example.net", "514"), name("b.example.net", "514")},
			[]logfwd.ConnState{{Conn: logfwd.ConnDown, Open: 1, Of: 2}, {Conn: logfwd.ConnDown, Open: 1, Of: 2}}},
		{"two names on a port, two sockets", table + "ESTAB 0 0 192.0.2.10:41240 192.0.2.77:514 users:((\"rsyslogd\",pid=612,fd=12))\n",
			[]logfwd.Server{name("a.example.net", "514"), name("b.example.net", "514")}, []logfwd.ConnState{open, open}},
		{"two names on a port, none", "", []logfwd.Server{name("a.example.net", "514"), name("b.example.net", "514")}, []logfwd.ConnState{down, down}},
		{"a name is not open on another program's socket", "ESTAB 0 0 192.0.2.10:41240 192.0.2.77:514 users:((\"nc\",pid=9,fd=3))\n",
			[]logfwd.Server{name("a.example.net", "514")}, []logfwd.ConnState{down}},
	} {
		if got, err := logfwd.ReadConns(ctx, ss(hostfake.Response{Stdout: tc.table}), tc.s, nil); !slices.Equal(got, tc.want) || err != nil {
			t.Errorf("%s: ReadConns = %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
	// A forward outside the list, to the same port: its socket is not a
	// named server's (§2.4), and what the sockets do not tell is not Open.
	a, b := []logfwd.Server{name("a.example.net", "514")}, []logfwd.Server{name("b.example.net", "514")}
	for _, tc := range []struct {
		name   string
		table  string
		beside []logfwd.Server
		want   logfwd.ConnState
	}{
		{"an address outside the list, the name down", to60, out60, down},
		{"an address outside the list, the name open", to60 + to77, out60, open},
		{"more sockets than names", to60 + to77, nil, untold},
		{"a name outside the list, one socket", to77, b, untold},
		{"a name outside the list, two sockets", to60 + to77, b, open},
		{"a name outside the list, no socket", "", b, down},
		{"udp outside the list", to77, []logfwd.Server{udp2}, open},
	} {
		if got, err := logfwd.ReadConns(ctx, ss(hostfake.Response{Stdout: tc.table}), a, tc.beside); len(got) != 1 || got[0] != tc.want || err != nil {
			t.Errorf("%s: ReadConns = %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
	unknown := []logfwd.ConnState{{Conn: logfwd.ConnUnknown}, {}}
	if got, err := logfwd.ReadConns(ctx, ss(hostfake.Response{Err: host.ErrNotFound}), []logfwd.Server{tcp, udp2}, nil); !slices.Equal(got, unknown) || !errors.Is(err, host.ErrNotFound) {
		t.Errorf("no ss: ReadConns = %+v, %v; want unknown", got, err)
	}
	if got, err := logfwd.ReadConns(ctx, ss(hostfake.Response{ExitCode: 1}), []logfwd.Server{tcp, udp2}, nil); !slices.Equal(got, unknown) || err == nil {
		t.Errorf("ss failing: ReadConns = %+v, %v; want unknown", got, err)
	}
	// UDP alone asks ss nothing.
	if got, err := logfwd.ReadConns(ctx, hostfake.New(host.Options{}), []logfwd.Server{udp, udp2}, nil); err != nil || !slices.Equal(got, []logfwd.ConnState{{}, {}}) {
		t.Errorf("udp alone: ReadConns = %+v, %v", got, err)
	}
	if !(logfwd.ConnState{Conn: logfwd.ConnDown, Open: 1, Of: 2}).Some() || down.Some() {
		t.Error("Some")
	}
}
