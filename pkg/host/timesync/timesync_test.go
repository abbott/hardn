package timesync_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/host/timesync/timefake"
)

var want = []string{"192.0.2.10", "192.0.2.11"}

const (
	site  = timesync.SourcesDir + "/site.sources"
	dhcp  = timesync.ChronyDHCP + "/eth0.sources"
	apkDB = "P:busybox\nV:1.37.0-r30\n\n"
)

func facts(d detect.Distro) detect.Facts {
	f := detect.Facts{Platform: detect.Platform{Distro: d}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}
	if d == detect.DistroAlpine {
		f.Init.Kind = detect.OpenRC
	}
	return f
}

// fake is a host with sim as its time service and files, name then text.
func fake(sim *timefake.Sim, files ...string) *hostfake.System {
	h := hostfake.New(host.Options{})
	for i := 0; i < len(files); i += 2 {
		h.AddFile(files[i], []byte(files[i+1]), 0o644, 0, 0)
	}
	sim.Read = func(name string) ([]byte, error) { return h.ReadFile(context.Background(), name) }
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := sim.Answer(argv); ok {
			return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}
		}
		return hostfake.Response{Stderr: "unscripted\n", ExitCode: 127}
	}
	return h
}

func chrony(synced string) *timefake.Sim {
	return &timefake.Sim{Service: "chrony", Synced: synced, Packages: map[string]bool{"chrony": true}}
}

func must(t *testing.T) func(b []byte, err error) string {
	return func(b []byte, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}

func names(ss []timesync.Server, from timesync.From) []string {
	var out []string
	for _, s := range ss {
		if s.From == from {
			out = append(out, s.Name)
		}
	}
	return out
}

var ubuntuPools = []string{"1.ntp.ubuntu.com", "2.ntp.ubuntu.com", "3.ntp.ubuntu.com", "4.ntp.ubuntu.com", "ntp-bootstrap.ubuntu.com"}

// TestReadChrony: every chrony state the mocks show, and what a fact not
// read adds.
func TestReadChrony(t *testing.T) {
	ctx, u := context.Background(), facts(detect.DistroUbuntu)
	ok := must(t)
	sources := ok(timesync.SourcesFile(want))
	off := ok(timesync.ChronyOff(timefake.UbuntuPoolsFile, []byte(timefake.UbuntuPools), nil))

	r := timesync.Read(ctx, fake(chrony("2.ntp.ubuntu.com"), timesync.ChronyConfDebian, timefake.UbuntuConf, timefake.UbuntuPoolsFile, timefake.UbuntuPools), u)
	if r.Err != nil || r.Service != timesync.Chrony || r.Running != detect.Yes || r.Clock != timesync.InSync || r.Synced != "2.ntp.ubuntu.com" ||
		!slices.Equal(names(r.Servers, timesync.FromFile), ubuntuPools) || len(r.Servers) != 5 || !r.DropIn || r.Mine || !r.NTS() || r.Unit != "chrony" {
		t.Fatalf("stock Ubuntu: %+v", r)
	}

	r = timesync.Read(ctx, fake(chrony("192.0.2.10"), timesync.ChronyConfDebian, timefake.UbuntuConf, timefake.UbuntuPoolsFile, off, timesync.ChronySources, sources), u)
	if r.Err != nil || !slices.Equal(r.Own(), want) || len(r.Servers) != 2 || r.File != timesync.ChronySources || !r.Mine ||
		!slices.Equal(names(r.Restores, timesync.FromFile), ubuntuPools) || r.NTS() || !r.Restores[0].NTS {
		t.Fatalf("hardn's servers: %+v", r)
	}

	r = timesync.Read(ctx, fake(chrony(""), timesync.ChronyConfDebian, timefake.UbuntuConf, timesync.ChronySources, sources), u)
	if r.Err != nil || r.Clock != timesync.NotInSync || r.Synced != "" {
		t.Fatalf("not in sync: %+v", r)
	}

	stopped := chrony("")
	stopped.Stopped = true
	r = timesync.Read(ctx, fake(stopped, timesync.ChronyConfDebian, timefake.UbuntuConf, timefake.UbuntuPoolsFile, timefake.UbuntuPools), u)
	if r.Err != nil || r.Running != detect.No || r.Clock != timesync.NotInSync || len(r.Servers) != 5 {
		t.Fatalf("stopped: %+v", r)
	}

	r = timesync.Read(ctx, fake(chrony("192.0.2.77"), timesync.ChronyConfDebian, timefake.UbuntuConf, timefake.UbuntuPoolsFile, timefake.UbuntuPools,
		site, "server 192.0.2.77 iburst\n", dhcp, "server 192.0.2.1 iburst\n"), u)
	if o := r.Outside(); r.Err != nil || o == nil || o.Name != "192.0.2.77" || o.File != site || !slices.Equal(names(r.Servers, timesync.FromDHCP), []string{"192.0.2.1"}) {
		t.Fatalf("site.sources and DHCP: %+v", r)
	}

	// hardn's name without hardn's first line is not hardn's file.
	r = timesync.Read(ctx, fake(chrony("x"), timesync.ChronyConfDebian, timefake.UbuntuConf, timesync.ChronySources, "server 192.0.2.10 iburst\n"), u)
	if r.Err != nil || r.Mine || r.Outside() == nil {
		t.Fatalf("a file at hardn's path: %+v", r)
	}

	// Rocky Linux: no sources.d, hardn's lines in the main file.
	el := &timefake.Sim{Service: "chronyd", Synced: "192.0.2.10", Packages: map[string]bool{"chrony": true}}
	set := ok(timesync.ChronyOff(timesync.ChronyConfEL, []byte(timefake.ELConf), want))
	r = timesync.Read(ctx, fake(el, timesync.ChronyConfEL, set), facts(detect.DistroRocky))
	if r.Err != nil || r.DropIn || r.Main != timesync.ChronyConfEL || !slices.Equal(r.Own(), want) || r.File != timesync.ChronyConfEL ||
		!slices.Equal(names(r.Restores, timesync.FromFile), []string{"2.rocky.pool.ntp.org"}) || r.Unit != "chronyd" || len(r.Servers) != 2 {
		t.Fatalf("Rocky, set in place: %+v", r)
	}

	// confdir and include are followed; a file of theirs is not the service's own.
	r = timesync.Read(ctx, fake(chrony("x"), timesync.ChronyConfDebian, "confdir /etc/chrony/conf.d\ninclude /etc/chrony/extra/*.inc\nPOOL a.example iburst\n",
		"/etc/chrony/conf.d/site.conf", "peer b.example\n", "/etc/chrony/conf.d/skip.txt", "server no.example\n", "/etc/chrony/extra/x.inc", "server c.example nts\n"), u)
	if got := names(r.Servers, timesync.FromOther); r.Err != nil || !slices.Equal(got, []string{"b.example", "c.example"}) || !slices.Equal(names(r.Servers, timesync.FromFile), []string{"a.example"}) || r.DropIn {
		t.Fatalf("confdir and include: %+v", r)
	}
}

// TestReadChronyNotRead: what is not read is an error, never "no servers".
func TestReadChronyNotRead(t *testing.T) {
	ctx, u := context.Background(), facts(detect.DistroUbuntu)
	for name, files := range map[string][]string{
		"no main file":                               nil,
		"a server with no name":                      {timesync.ChronyConfDebian, "server\n"},
		"hardn's comment, short":                     {timesync.ChronyConfDebian, timesync.Added + "2 lines below.\nserver 192.0.2.10 iburst\n"},
		"hardn's comment, other lines":               {timesync.ChronyConfDebian, timesync.Added + "1 line below.\nserver 192.0.2.10 iburst prefer\n"},
		"hardn's comment, twice":                     {timesync.ChronyConfDebian, timesync.Added + "1 line below.\nserver a.example iburst\n" + timesync.Added + "1 line below.\nserver b.example iburst\n"},
		"an include with a pattern in its directory": {timesync.ChronyConfDebian, "include /etc/*/x.conf\n"},
		"a file that includes itself":                {timesync.ChronyConfDebian, "include /etc/chrony/chrony.conf\n"},
	} {
		r := timesync.Read(ctx, fake(chrony("x"), files...), u)
		if r.Err == nil || r.Service != timesync.Chrony {
			t.Errorf("%s: %+v", name, r)
		}
		if files != nil && !errors.Is(r.Err, timesync.ErrMalformed) {
			t.Errorf("%s: %v is not ErrMalformed", name, r.Err)
		}
	}
	// The main file is there and cannot be read.
	h := fake(chrony("x"))
	h.AddDir(timesync.ChronyConfDebian, 0o755, 0, 0)
	if r := timesync.Read(ctx, h, u); r.Err == nil {
		t.Errorf("unreadable: %+v", r)
	}
	// chronyc is not there, or says something else: the clock is unknown.
	gone := chrony("x")
	gone.Missing = map[string]bool{"chronyc": true}
	if r := timesync.Read(ctx, fake(gone, timesync.ChronyConfDebian, timefake.UbuntuConf), u); r.Err != nil || r.Clock != timesync.ClockUnknown || r.ClockErr == nil {
		t.Errorf("no chronyc: %+v", r)
	}
	odd := fake(chrony("x"), timesync.ChronyConfDebian, timefake.UbuntuConf)
	odd.Respond(hostfake.Response{Stdout: "Leap status : Odd\n\x1b[31mjunk\n"}, "chronyc", "-N", "tracking")
	if r := timesync.Read(ctx, odd, u); r.Clock != timesync.ClockUnknown || !errors.Is(r.ClockErr, detect.ErrUnrecognised) {
		t.Errorf("odd tracking: %+v", r)
	}
	// dpkg does not say, and the service's state does not say.
	blind := hostfake.New(host.Options{})
	blind.Handler = func([]string, []byte) hostfake.Response { return hostfake.Response{ExitCode: 127} }
	if r := timesync.Read(ctx, blind, u); r.Err == nil || r.Service != timesync.NoService {
		t.Errorf("dpkg unread: %+v", r)
	}
	act := fake(chrony("x"), timesync.ChronyConfDebian, timefake.UbuntuConf)
	act.Respond(hostfake.Response{Stdout: "activating\n", ExitCode: 3}, "systemctl", "is-active", "chrony")
	if r := timesync.Read(ctx, act, u); r.Err == nil || r.Service != timesync.Chrony {
		t.Errorf("activating: %+v", r)
	}
	if r := timesync.Read(ctx, fake(chrony("x")), detect.Facts{}); r.Err == nil {
		t.Errorf("unknown distribution: %+v", r)
	}
}

func TestReadTimesyncd(t *testing.T) {
	ctx, u := context.Background(), facts(detect.DistroUbuntu)
	sim := func() *timefake.Sim {
		return &timefake.Sim{Service: "systemd-timesyncd", Synced: "ntp.ubuntu.com", Fallback: "ntp.ubuntu.com", Packages: map[string]bool{"systemd-timesyncd": true}}
	}
	const main = "/etc/systemd/timesyncd.conf"
	r := timesync.Read(ctx, fake(sim(), main, "[Time]\n#NTP=\n#FallbackNTP=ntp.ubuntu.com\n"), u)
	if r.Err != nil || r.Service != timesync.Timesyncd || r.Clock != timesync.InSync || r.Synced != "ntp.ubuntu.com" ||
		!slices.Equal(names(r.Servers, timesync.FromBuiltIn), []string{"ntp.ubuntu.com"}) || r.Unit != "systemd-timesyncd" {
		t.Fatalf("stock: %+v", r)
	}
	own := sim()
	own.Synced, own.Link = "192.0.2.10", "192.0.2.1"
	r = timesync.Read(ctx, fake(own, timesync.TimesyncdDropIn, must(t)(timesync.TimesyncdFile(want))), u)
	if r.Err != nil || !slices.Equal(r.Own(), want) || r.File != timesync.TimesyncdDropIn || !r.Mine || !slices.Equal(names(r.Servers, timesync.FromDHCP), []string{"192.0.2.1"}) ||
		!slices.Equal(names(r.Restores, timesync.FromBuiltIn), []string{"ntp.ubuntu.com"}) {
		t.Fatalf("hardn's drop-in: %+v", r)
	}
	// Another file's NTP=, the main file's too, is set outside hardn; an
	// empty NTP= takes away the servers before it, as systemd reads it.
	r = timesync.Read(ctx, fake(sim(), main, "[Time]\nNTP=a.example\n", "/etc/systemd/timesyncd.conf.d/10-x.conf", "[Time]\nNTP=\nNTP = b.example c.example\n[Other]\nNTP=no.example\n",
		"/usr/lib/systemd/timesyncd.conf.d/10-x.conf", "[Time]\nNTP=shadowed.example\n", "/run/systemd/timesyncd.conf.d/05-y.conf", "[Time]\nNTP=gone.example\n"), u)
	if r.Err != nil || !slices.Equal(names(r.Servers, timesync.FromOther), []string{"b.example", "c.example"}) || len(r.Servers) != 2 || r.Outside().File != "/etc/systemd/timesyncd.conf.d/10-x.conf" {
		t.Fatalf("drop-ins: %+v", r)
	}
	// An empty NTP= read after hardn's file clears hardn's servers: set
	// outside hardn, with hardn's file there or not. One read before it,
	// or the main file's, clears nothing of hardn's.
	const late, early = "/etc/systemd/timesyncd.conf.d/90-site.conf", "/etc/systemd/timesyncd.conf.d/10-site.conf"
	r = timesync.Read(ctx, fake(sim(), timesync.TimesyncdDropIn, must(t)(timesync.TimesyncdFile(want)), late, "[Time]\nNTP=\n"), u)
	if r.Err != nil || r.Cleared != late || len(r.Own()) != 0 || !r.Mine || !slices.Equal(names(r.Servers, timesync.FromBuiltIn), []string{"ntp.ubuntu.com"}) {
		t.Fatalf("cleared after hardn's: %+v", r)
	}
	if r = timesync.Read(ctx, fake(sim(), late, "[Time]\nNTP=\n"), u); r.Err != nil || r.Cleared != late {
		t.Fatalf("cleared, hardn's not there: %+v", r)
	}
	r = timesync.Read(ctx, fake(sim(), main, "[Time]\nNTP=\n", early, "[Time]\nNTP=\n", timesync.TimesyncdDropIn, must(t)(timesync.TimesyncdFile(want))), u)
	if r.Err != nil || r.Cleared != "" || !slices.Equal(r.Own(), want) {
		t.Fatalf("cleared before hardn's: %+v", r)
	}
	stopped := sim()
	stopped.Stopped = true
	if r = timesync.Read(ctx, fake(stopped), u); r.Err != nil || r.Running != detect.No || r.Clock != timesync.NotInSync || len(r.Servers) != 0 {
		t.Fatalf("stopped: %+v", r)
	}
	gone := sim()
	gone.Missing = map[string]bool{"timedatectl": true}
	if r = timesync.Read(ctx, fake(gone), u); r.Err == nil || r.Clock != timesync.ClockUnknown {
		t.Fatalf("no timedatectl: %+v", r)
	}
	odd := fake(sim())
	odd.Respond(hostfake.Response{Stdout: "garbage\nNTPSynchronized=maybe\n"}, "timedatectl", "show")
	if r = timesync.Read(ctx, odd, u); r.Clock != timesync.ClockUnknown || !errors.Is(r.ClockErr, detect.ErrUnrecognised) {
		t.Fatalf("odd timedatectl: %+v", r)
	}
	// Neither package: no time service, and the kernel's word on the clock.
	none := &timefake.Sim{Packages: map[string]bool{}}
	if r = timesync.Read(ctx, fake(none), u); r.Err != nil || r.Service != timesync.NoService || r.Clock != timesync.NotInSync {
		t.Fatalf("no service: %+v", r)
	}
}

func TestReadBusybox(t *testing.T) {
	ctx, a := context.Background(), facts(detect.DistroAlpine)
	sim := func() *timefake.Sim { return &timefake.Sim{Service: "ntpd", Synced: "pool.ntp.org"} }
	r := timesync.Read(ctx, fake(sim(), alpine.InstalledDB, apkDB, timesync.ConfD, timefake.AlpineConfD), a)
	if r.Err != nil || r.Service != timesync.Busybox || r.Clock != timesync.InSync || r.Synced != "" || !slices.Equal(names(r.Servers, timesync.FromFile), []string{"pool.ntp.org"}) || r.RC != "ntpd" {
		t.Fatalf("stock: %+v", r)
	}
	set := must(t)(timesync.SetPeers([]byte(timefake.AlpineConfD), want))
	r = timesync.Read(ctx, fake(sim(), alpine.InstalledDB, apkDB, timesync.ConfD, set), a)
	if r.Err != nil || !slices.Equal(r.Own(), want) || !r.Mine || r.File != timesync.ConfD || !slices.Equal(names(r.Restores, timesync.FromFile), []string{"pool.ntp.org"}) {
		t.Fatalf("hardn's peers: %+v", r)
	}
	for name, conf := range map[string]string{
		"no -p":          "NTPD_OPTS=\"-N\"\n",
		"a -p alone":     "NTPD_OPTS=\"-N -p\"\n",
		"an odd peer":    "NTPD_OPTS=\"-p -N\"\n",
		"peers apart":    "NTPD_OPTS=\"-p a.example -N -p b.example\"\n",
		"set twice":      "NTPD_OPTS=\"-p a.example\"\nNTPD_OPTS=\"-p b.example\"\n",
		"a shell's word": "NTPD_OPTS=\"-p $(hostname)\"\n",
		"an odd mark":    timesync.Mark + "-N\nNTPD_OPTS=\"-p a.example\"\n",
		"an empty mark":  timesync.Mark + "\nNTPD_OPTS=\"-p a.example\"\n",
		"not set at all": "# nothing\n",
	} {
		if r := timesync.Read(ctx, fake(sim(), alpine.InstalledDB, apkDB, timesync.ConfD, conf), a); !errors.Is(r.Err, timesync.ErrMalformed) || r.Service != timesync.Busybox {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if r := timesync.Read(ctx, fake(sim(), alpine.InstalledDB, apkDB), a); r.Err == nil {
		t.Errorf("no conf.d file: %+v", r)
	}
	// ntpd neither started nor in a runlevel: Alpine set up with "none".
	off := sim()
	off.Stopped = true
	if r := timesync.Read(ctx, fake(off, alpine.InstalledDB, apkDB, timesync.ConfD, timefake.AlpineConfD), a); r.Err != nil || r.Service != timesync.NoService || r.Clock != timesync.NotInSync {
		t.Errorf("none: %+v", r)
	}
	h := fake(off, alpine.InstalledDB, apkDB, timesync.ConfD, timefake.AlpineConfD, detect.OpenRCRunlevels+"/default/ntpd", "")
	if r := timesync.Read(ctx, h, a); r.Err != nil || r.Service != timesync.Busybox || r.Running != detect.No {
		t.Errorf("stopped, in a runlevel: %+v", r)
	}
	// chrony chosen on Alpine: its service is chronyd.
	c := &timefake.Sim{Service: "chronyd", Synced: "pool.ntp.org"}
	r = timesync.Read(ctx, fake(c, alpine.InstalledDB, apkDB+"P:chrony\nV:4.8-r0\n\n", timesync.ChronyConfDebian, "pool pool.ntp.org iburst\ncmdport 0\n"), a)
	if r.Err != nil || r.Service != timesync.Chrony || r.RC != "chronyd" || r.DropIn || r.Synced != "pool.ntp.org" {
		t.Errorf("Alpine's chrony: %+v", r)
	}
	if r := timesync.Read(ctx, fake(sim()), a); r.Err == nil {
		t.Errorf("apk's database unread: %+v", r)
	}
	gone := sim()
	gone.Missing = map[string]bool{"busybox": true}
	if r := timesync.Read(ctx, fake(gone, alpine.InstalledDB, apkDB, timesync.ConfD, timefake.AlpineConfD), a); r.Clock != timesync.ClockUnknown || r.ClockErr == nil {
		t.Errorf("no adjtimex: %+v", r)
	}
}

// TestContainer: no service is looked for, and the clock is the kernel's
// word, Unknown where the container may not ask.
func TestContainer(t *testing.T) {
	f := facts(detect.DistroUbuntu)
	f.Container.Detected = detect.Yes
	h := fake(chrony("x"))
	r := timesync.Read(context.Background(), h, f)
	if !r.Container || r.Service != timesync.NoService || r.Clock != timesync.InSync || r.Err != nil {
		t.Fatalf("container: %+v", r)
	}
	if runs := h.Runs(); len(runs) != 1 || strings.Join(runs[0], " ") != "timedatectl show" {
		t.Fatalf("ran %v", runs)
	}
	gone := chrony("x")
	gone.Missing = map[string]bool{"timedatectl": true}
	if r := timesync.Read(context.Background(), fake(gone), f); r.Clock != timesync.ClockUnknown || r.Err != nil {
		t.Fatalf("container, no timedatectl: %+v", r)
	}
}

// TestChronyOffOn: the edit of a file hardn did not write is minimal and
// exactly reversed, whatever the file's form.
func TestChronyOffOn(t *testing.T) {
	ok := must(t)
	const name = timesync.ChronyConfEL
	got := ok(timesync.ChronyOff(name, []byte(timefake.ELConf), want))
	wantText := strings.Replace(timefake.ELConf, "pool 2.rocky.pool.ntp.org iburst\n",
		"#hardn: pool 2.rocky.pool.ntp.org iburst\n# Added by hardn (NTP Settings): the 2 lines below.\nserver 192.0.2.10 iburst\nserver 192.0.2.11 iburst\n", 1)
	if got != wantText {
		t.Fatalf("ChronyOff:\n%s\nwant:\n%s", got, wantText)
	}
	if again := ok(timesync.ChronyOff(name, []byte(got), []string{"ntp.example"})); !strings.Contains(again, "the 1 line below.\nserver ntp.example iburst\n") || strings.Contains(again, "192.0.2.10") || strings.Count(again, timesync.Off) != 1 {
		t.Fatalf("a second set:\n%s", again)
	}
	for _, file := range []string{timefake.ELConf, timefake.UbuntuPools, timefake.DebianConf, "", "\n", "pool a.example", "  Server a.example  iburst\r\n\tpeer b.example\r\n",
		"#hardn: a comment of the administrator's\npool a.example\n", "makestep 1 3\n", "makestep 1 3"} {
		for _, servers := range [][]string{nil, want, {"ntp.example"}} {
			off := ok(timesync.ChronyOff(name, []byte(file), servers))
			if back := ok(timesync.ChronyOn(name, []byte(off))); back != file {
				t.Errorf("%q with %v: back as %q", file, servers, back)
			}
			if servers != nil {
				continue
			}
			for _, l := range strings.Split(off, "\n") {
				if f := strings.Fields(l); len(f) > 0 && slices.Contains([]string{"server", "pool", "peer"}, strings.ToLower(f[0])) {
					t.Errorf("%q: a line left on: %q", file, l)
				}
			}
		}
	}
	if on := ok(timesync.ChronyOn(name, []byte(timefake.ELConf))); on != timefake.ELConf {
		t.Errorf("ChronyOn of a file hardn never edited: %q", on)
	}
	for _, bad := range []string{
		timesync.Added + "2 lines below.\nserver a.example iburst\n",
		timesync.Added + "1 line below.\nserver a.example iburst minpoll 4\n",
		timesync.Added + "1 line below.\nserver a.example iburst\n" + timesync.Added + "1 line below.\nserver a.example iburst\n",
	} {
		if _, err := timesync.ChronyOn(name, []byte(bad)); !errors.Is(err, timesync.ErrMalformed) {
			t.Errorf("ChronyOn(%q) = %v", bad, err)
		}
		if _, err := timesync.ChronyOff(name, []byte(bad), want); !errors.Is(err, timesync.ErrMalformed) {
			t.Errorf("ChronyOff(%q) = %v", bad, err)
		}
	}
}

// TestPeers: /etc/conf.d/ntpd set and put back byte for byte, and refused
// in a form hardn would not write back.
func TestPeers(t *testing.T) {
	ok := must(t)
	set := ok(timesync.SetPeers([]byte(timefake.AlpineConfD), want))
	wantText := "# By default ntpd runs as a client. Add -l to run as a server on port 123.\n# Set by hardn (NTP Settings), in place of: -p pool.ntp.org\nNTPD_OPTS=\"-N -p 192.0.2.10 -p 192.0.2.11\"\n"
	if set != wantText {
		t.Fatalf("SetPeers:\n%s", set)
	}
	if back := ok(timesync.ClearPeers([]byte(set))); back != timefake.AlpineConfD {
		t.Fatalf("ClearPeers:\n%s", back)
	}
	moved := ok(timesync.SetPeers([]byte(set), []string{"ntp.example"}))
	if !strings.Contains(moved, "in place of: -p pool.ntp.org\nNTPD_OPTS=\"-N -p ntp.example\"") || ok(timesync.ClearPeers([]byte(moved))) != timefake.AlpineConfD {
		t.Fatalf("a second set:\n%s", moved)
	}
	for _, file := range []string{"NTPD_OPTS=\"-p a.example -pb.example -N\"\n", "NTPD_OPTS=\"-S /x -p 2001:db8::1\""} {
		if back := ok(timesync.ClearPeers([]byte(ok(timesync.SetPeers([]byte(file), want))))); back != file {
			t.Errorf("%q: back as %q", file, back)
		}
	}
	for name, file := range map[string]string{"single quotes": "NTPD_OPTS='-N -p pool.ntp.org'\n", "two spaces": "NTPD_OPTS=\"-N  -p pool.ntp.org\"\n", "bare": "NTPD_OPTS=-ppool.ntp.org\n"} {
		if _, err := timesync.SetPeers([]byte(file), want); !errors.Is(err, timesync.ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := timesync.ClearPeers([]byte(timefake.AlpineConfD)); !errors.Is(err, timesync.ErrNotOwn) {
		t.Errorf("ClearPeers with no mark: %v", err)
	}
	if _, err := timesync.ClearPeers([]byte(timesync.Mark + "-p $(reboot)\nNTPD_OPTS=\"-p a.example\"\n")); !errors.Is(err, timesync.ErrMalformed) {
		t.Errorf("ClearPeers with a mark hardn did not write: %v", err)
	}
}

// TestServersChecked: nothing but an address or a name reaches a file,
// whoever calls.
func TestServersChecked(t *testing.T) {
	for _, servers := range [][]string{nil, {}, {"a", "b", "c", "d", "e"}, {"192.0.2.10 prefer"}, {"a\nserver evil"}, {"-p"}, {"A.example"}, {"$(reboot)"}, {"a;b"}, {""}, {"2001:db8::1"}} {
		if _, err := timesync.SourcesFile(servers); !errors.Is(err, timesync.ErrServer) {
			t.Errorf("SourcesFile(%q) = %v", servers, err)
		}
		if _, err := timesync.TimesyncdFile(servers); !errors.Is(err, timesync.ErrServer) {
			t.Errorf("TimesyncdFile(%q) = %v", servers, err)
		}
		if _, err := timesync.SetPeers([]byte(timefake.AlpineConfD), servers); !errors.Is(err, timesync.ErrServer) {
			t.Errorf("SetPeers(%q) = %v", servers, err)
		}
		if servers != nil {
			if _, err := timesync.ChronyOff(timesync.ChronyConfEL, []byte(timefake.ELConf), servers); !errors.Is(err, timesync.ErrServer) {
				t.Errorf("ChronyOff(%q) = %v", servers, err)
			}
		}
	}
	if got := must(t)(timesync.SourcesFile(want)); got != timesync.Header+"server 192.0.2.10 iburst\nserver 192.0.2.11 iburst\n" {
		t.Errorf("SourcesFile:\n%s", got)
	}
	if got := must(t)(timesync.TimesyncdFile(want)); got != timesync.Header+"[Time]\nNTP=192.0.2.10 192.0.2.11\n" {
		t.Errorf("TimesyncdFile:\n%s", got)
	}
}
