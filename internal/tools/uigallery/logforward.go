package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	lfctl "github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/output"
	lfscreen "github.com/abbott/hardn/pkg/screens/logforward"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Syslog Settings screen on the simulated hosts (docs/roadmap.md, "Log
// forwarding"; docs/engine/gallery-logforward.md, adopted 2026-11-13;
// docs/engine/screen-logforward.md; several servers,
// docs/engine/design-syslog-servers.md): hardn's Syslog Settings screen itself
// (pkg/screens/logforward), drawn from what the Syslog Settings control
// reads of each host, each change planned on the real engine, on the hosts
// the mock was drawn for. "uigallery logforward" lists each host's screen,
// and each change's review up to its question, with no input; the main
// menu's 8, then 4, leads to the screen on Ubuntu 24.04 in play; "play logforward"
// is the menu of the hosts and "play logforward/<host>" one.

// lfPrefix names the listing, its goldens, and the hosts in play.
const lfPrefix = "logforward"

// lfLabel is the source label every mock's note opens with.
const lfLabel = "Decided: no Figma frame."

// lfMainHost is the host the Configuration group's 4 leads to in play (main
// menu 8, then 4; config.go).
const lfMainHost = lfPrefix + "/off"

// The hosts' files and words.
const (
	lfOther   = "/etc/rsyslog.d/50-remote.conf"
	lfCAFile  = "/etc/ssl/lab-ca.pem"
	lfVersion = "8.2312.0"
	lfStock   = "module(load=\"imuxsock\")\n$IncludeConfig /etc/rsyslog.d/*.conf\n"

	lfYMLTCP   = "syslogServers:\n  - server: \"192.0.2.50\"\n"
	lfYMLUDP   = "syslogServers:\n  - server: \"192.0.2.50\"\n    protocol: udp\n"
	lfYMLTLS   = "syslogServers:\n  - server: \"logs.example.net\"\n    protocol: tls\n    caFile: \"" + lfCAFile + "\"\n"
	lfYMLMoved = "syslogServers:\n  - server: \"192.0.2.60\"\n"
	// The design's three (design-syslog-servers.md §2.1), and its first two.
	lfYMLThree  = lfYMLTLS + "  - server: \"192.0.2.51\"\n  - server: \"192.0.2.52\"\n    protocol: udp\n"
	lfYMLTwo    = "syslogServers:\n  - server: \"192.0.2.51\"\n  - server: \"192.0.2.52\"\n    protocol: udp\n"
	lfYMLAdded  = lfYMLTwo + "  - server: \"192.0.2.53\"\n"
	lfYMLLong   = "syslogServers:\n  - server: \"" + lfLongName + "\"\n    protocol: tls\n    caFile: \"" + lfCAFile + "\"\n"
	lfYMLNames  = "syslogServers:\n  - server: \"logs-a.example.net\"\n  - server: \"logs-b.example.net\"\n"
	lfYMLTwoUDP = "syslogServers:\n  - server: \"192.0.2.51\"\n    protocol: udp\n  - server: \"192.0.2.52\"\n    protocol: udp\n"
	lfYMLMixed  = "syslogServers:\n  - server: \"192.0.2.51\"\n    protocol: udp\n  - server: \"logs.example.net\"\n"
	lfYMLTwice  = "syslogServers:\n  - server: \"192.0.2.51\"\n  - server: \"192.0.2.51\"\n    protocol: udp\n"

	lfLongName = "syslog-collector.prod.example.com"
)

// The hosts' servers, as hardn writes them.
var (
	lfTCP   = logfwd.Server{Address: "192.0.2.50", Port: "514", Proto: logfwd.TCP}
	lfUDP   = logfwd.Server{Address: "192.0.2.50", Port: "514", Proto: logfwd.UDP}
	lfTLS   = logfwd.Server{Address: "logs.example.net", Port: "6514", Proto: logfwd.TLS, CAFile: lfCAFile}
	lfTCP1  = logfwd.Server{Address: "192.0.2.51", Port: "514", Proto: logfwd.TCP}
	lfUDP1  = logfwd.Server{Address: "192.0.2.51", Port: "514", Proto: logfwd.UDP}
	lfUDP2  = logfwd.Server{Address: "192.0.2.52", Port: "514", Proto: logfwd.UDP}
	lfLong  = logfwd.Server{Address: lfLongName, Port: "6514", Proto: logfwd.TLS, CAFile: lfCAFile}
	lfNameA = logfwd.Server{Address: "logs-a.example.net", Port: "514", Proto: logfwd.TCP}
	lfNameB = logfwd.Server{Address: "logs-b.example.net", Port: "514", Proto: logfwd.TCP}
)

// lfReachable are the servers of the gallery's network that accept a
// connection, as hardn.yml names them, and the peer ss then shows.
var lfReachable = map[string]string{"192.0.2.50:514": "192.0.2.50:514", "192.0.2.51:514": "192.0.2.51:514", "192.0.2.53:514": "192.0.2.53:514",
	"192.0.2.60:514": "192.0.2.60:514", "logs.example.net:6514": "192.0.2.50:6514"}

// lfSim is the logger's model on a host: what ss shows of rsyslogd's
// connections, and whether rsyslogd accepts its settings.
type lfSim struct {
	// peers are the servers rsyslogd has a connection to, "address:port".
	peers []string
	// refuses: rsyslogd -N1 fails, as with a syntax error in its files.
	refuses bool
}

// answer answers the Syslog Settings control's commands, and reports
// whether it did: none on a host with no model.
func (l *lfSim) answer(h *fakeHost, argv []string) (hostfake.Response, bool) {
	if l == nil {
		return hostfake.Response{}, false
	}
	cmd, pkg := strings.Join(argv, " "), argv[len(argv)-1]
	isPkg := pkg == "rsyslog" || pkg == logfwd.GnuTLSPackage
	switch {
	case cmd == "ss -Htnp":
		out := "ESTAB 0 0 192.0.2.20:2230 192.0.2.1:50514 users:((\"sshd\",pid=812,fd=4))\n"
		for i, p := range l.peers {
			out += fmt.Sprintf("ESTAB 0 0 192.0.2.20:%d %s users:((\"rsyslogd\",pid=612,fd=%d))\n", 41234+i, p, 9+i)
		}
		return hostfake.Response{Stdout: out}, true
	case cmd == "rsyslogd -N1":
		if l.refuses {
			return hostfake.Response{Stderr: "rsyslogd: error during parsing file /etc/rsyslog.d/50-remote.conf, on or before line 3: syntax error\n", ExitCode: 1}, true
		}
		return hostfake.Response{Stderr: "rsyslogd: End of config validation run. Bye.\n"}, true
	case cmd == "systemctl restart rsyslog": // rsyslogd connects to the server its settings name now
		l.peers = nil
		if data, err := h.f.ReadFile(context.Background(), logfwd.DropIn); err == nil {
			for _, action := range strings.Split(string(data), "*.* action(")[1:] { // in the file's order
				for name, peer := range lfReachable {
					target, port, _ := strings.Cut(name, ":")
					if strings.Contains(action, "target=\""+target+"\" port=\""+port+"\"") && !strings.Contains(action, "protocol=\"udp\"") {
						l.peers = append(l.peers, peer)
					}
				}
			}
		}
		return hostfake.Response{}, true
	case cmd == "rc-service syslog restart", argv[0] == "logger":
		return hostfake.Response{}, true
	case isPkg && strings.HasPrefix(cmd, "apt-get install -s -y "):
		return hostfake.Response{Stdout: "NOTE: This is only a simulation!\nInst " + pkg + " (" + lfVersion + " Debian:13.1/stable [amd64])\n"}, true
	case isPkg && strings.HasPrefix(cmd, "apt-get install -y "):
		h.status.packages[pkg] = lfVersion
		return hostfake.Response{}, true
	case isPkg && strings.HasPrefix(cmd, "apt-get remove -y "):
		delete(h.status.packages, pkg)
		return hostfake.Response{}, true
	}
	return hostfake.Response{}, false
}

// lfOn is a status host of platform p with the logger's model: rsyslog
// installed, with its stock file, where rsyslog is true; hardn.yml with
// yml, or none for "".
func lfOn(p platform, rsyslog bool, yml string) *fakeHost {
	h := statusOn(p)
	h.lf = &lfSim{}
	h.f.AddDir(logfwd.RsyslogDir, 0o755, 0, 0)
	h.f.AddDir("/etc/ssl", 0o755, 0, 0)
	if rsyslog {
		h.status.packages["rsyslog"] = lfVersion
		h.f.AddFile(logfwd.RsyslogConf, []byte(lfStock), 0o644, 0, 0)
	}
	if yml != "" {
		h.withHardnYML(yml)
	}
	return h
}

// lfUbuntu is Ubuntu 24.04, whose logger is rsyslog.
func lfUbuntu(yml string) *fakeHost { return lfOn(ubuntu2404Platform, true, yml) }

// lfForwards is h forwarding to servers by hardn's own file, a connection
// to each of peers open.
func lfForwards(h *fakeHost, servers []logfwd.Server, peers ...string) *fakeHost {
	data, err := logfwd.RsyslogFile(servers)
	if err != nil {
		panic(err) // the gallery's own servers
	}
	h.f.AddFile(logfwd.DropIn, data, 0o644, 0, 0)
	h.lf.peers = peers
	return h
}

// lfWithTLS is h with rsyslog's TLS driver installed and the CA file.
func lfWithTLS(h *fakeHost) *fakeHost {
	h.status.packages[logfwd.GnuTLSPackage] = lfVersion
	h.f.AddFile(lfCAFile, []byte("-----BEGIN CERTIFICATE-----\n"), 0o644, 0, 0)
	return h
}

// lfAlpine is Alpine 3.23 with busybox syslogd's stock options, and
// hardn's words in them for the servers that are set.
func lfAlpine(yml string, set ...logfwd.Server) *fakeHost {
	h := lfOn(alpine323Platform, false, yml)
	conf := []byte("SYSLOGD_OPTS=\"-t\"\n")
	if len(set) > 0 {
		var err error
		if conf, err = logfwd.SetOpts(conf, set); err != nil {
			panic(err) // the gallery's own servers
		}
	}
	h.f.AddDir("/etc/conf.d", 0o755, 0, 0)
	h.f.AddFile(logfwd.ConfD, conf, 0o644, 0, 0)
	return h
}

// lfHost is one simulated host the Syslog Settings screen runs on.
type lfHost struct {
	name, label, about, note string
	host                     func() *fakeHost
}

// lfHosts are the hosts, in the listing's and the menu's order: the mock's
// (gallery-logforward.md §2).
func lfHosts() []lfHost {
	const (
		ubuntu = hostFQDN + " runs Ubuntu 24.04, whose logger is rsyslog. "
		alpine = hostFQDN + " runs Alpine 3.23. "
	)
	one := func(s logfwd.Server) []logfwd.Server { return []logfwd.Server{s} }
	return []lfHost{
		{name: "off", label: "Ubuntu 24.04", about: "Not forwarding", host: func() *fakeHost { return lfUbuntu(lfYMLTCP) },
			note: ubuntu + "No file forwards, and hardn.yml names 192.0.2.50, port 514, TCP: Set servers, its description the server."},
		{name: "no-server", label: "No server", about: "hardn.yml names none", host: func() *fakeHost { return lfUbuntu("") },
			note: ubuntu + "No file forwards and hardn.yml names no server: the item stays, says what is missing, and refuses with words."},
		{name: "tcp", label: "TCP", about: "By hardn's file", host: func() *fakeHost { return lfForwards(lfUbuntu(lfYMLTCP), one(lfTCP), "192.0.2.50:514") },
			note: ubuntu + "hardn's file forwards over TCP, and ss lists a connection to the server: its row ends in Open. The one item undoes it."},
		{name: "tcp-down", label: "TCP, unreachable", about: "Partial", host: func() *fakeHost { return lfForwards(lfUbuntu(lfYMLTCP), one(lfTCP)) },
			note: ubuntu + "The same file, and no connection to the server: the row is Partial and the Server row a warning."},
		{name: "udp", label: "UDP", about: "A warning", host: func() *fakeHost { return lfForwards(lfUbuntu(lfYMLUDP), one(lfUDP)) },
			note: ubuntu + "hardn's file forwards over UDP: the Server row warns that losses are not seen, as UDP has no connection to show."},
		{name: "tls", label: "TLS", about: "With a CA file", host: func() *fakeHost {
			return lfWithTLS(lfForwards(lfUbuntu(lfYMLTLS), one(lfTLS), "192.0.2.50:6514"))
		}, note: ubuntu + "hardn's file forwards over TLS to the name the server's certificate carries, checked against hardn.yml's CA file, its row after the server's."},
		{name: "several", label: "Three servers", about: "Every connection open", host: func() *fakeHost {
			return lfWithTLS(lfForwards(lfUbuntu(lfYMLThree), []logfwd.Server{lfTLS, lfTCP1, lfUDP2}, "192.0.2.50:6514", "192.0.2.51:514"))
		}, note: ubuntu + "hardn's file forwards to hardn.yml's three servers, TLS, TCP and UDP: a Server row each, the status row their count."},
		{name: "several-down", label: "Three, one unreachable", about: "Partial", host: func() *fakeHost {
			return lfWithTLS(lfForwards(lfUbuntu(lfYMLThree), []logfwd.Server{lfTLS, lfTCP1, lfUDP2}, "192.0.2.50:6514"))
		}, note: ubuntu + "The same three, and no connection to 192.0.2.51: the row is Partial, 1 of 3 unreachable, and that server's row a warning."},
		{name: "several-differ", label: "Two, three asked", about: "hardn.yml differs", host: func() *fakeHost {
			return lfForwards(lfUbuntu(lfYMLAdded), []logfwd.Server{lfTCP1, lfUDP2}, "192.0.2.51:514")
		}, note: ubuntu + "hardn's file forwards to two servers and hardn.yml now names a third: the hardn.yml row warns, Set servers adds it, Stop forwarding is item 2."},
		{name: "long-name", label: "A long name", about: "The row stacks", host: func() *fakeHost {
			return lfWithTLS(lfForwards(lfUbuntu(lfYMLLong), one(lfLong), "192.0.2.50:6514"))
		}, note: ubuntu + "A server whose row is over the row's width: the key alone, and the whole value under it."},
		{name: "names", label: "Two names, one port", about: "1 of 2 open", host: func() *fakeHost {
			return lfForwards(lfUbuntu(lfYMLNames), []logfwd.Server{lfNameA, lfNameB}, "192.0.2.70:514")
		}, note: ubuntu + "Two servers given by name on port 514 and one rsyslogd connection to it: ss prints no names, so hardn says how many are open, not which."},
		{name: "foreign", label: "Set outside hardn", about: "Unavailable", host: func() *fakeHost {
			h := lfUbuntu(lfYMLTCP)
			h.f.AddFile(lfOther, []byte("# the lab's log server\n*.* @@192.0.2.50:514\n"), 0o644, 0, 0)
			h.lf.peers = []string{"192.0.2.50:514"}
			return h
		}, note: ubuntu + "50-remote.conf, not hardn's, forwards to the server: Set by is Not hardn, and Set servers is unavailable."},
		{name: "mismatch", label: "Another server", about: "hardn.yml differs", host: func() *fakeHost { return lfForwards(lfUbuntu(lfYMLMoved), one(lfTCP), "192.0.2.50:514") },
			note: ubuntu + "hardn's file forwards to 192.0.2.50 and hardn.yml now names 192.0.2.60: the hardn.yml row warns, Set servers moves it, Stop forwarding is item 2."},
		{name: "journald", label: "Debian 13", about: "journald alone", host: func() *fakeHost { return lfOn(debian13Platform, false, lfYMLTCP) },
			note: hostFQDN + " runs Debian 13 with journald alone, as Debian installs it: no syslog forwarder. Set servers would install rsyslog."},
		{name: "alpine", label: "Alpine 3.23", about: "busybox syslogd, UDP", host: func() *fakeHost { return lfAlpine(lfYMLUDP, lfUDP) },
			note: alpine + "busybox syslogd forwards over UDP by -R in /etc/conf.d/syslog, set by hardn."},
		{name: "alpine-several", label: "Alpine, two servers", about: "One -R each", host: func() *fakeHost { return lfAlpine(lfYMLTwoUDP, lfUDP1, lfUDP2) },
			note: alpine + "busybox syslogd forwards to two servers, one -R each, set by hardn."},
		{name: "alpine-tcp", label: "Alpine, TCP asked", about: "Refused", host: func() *fakeHost { return lfAlpine(lfYMLTCP) },
			note: alpine + "hardn.yml asks for TCP, which busybox syslogd has not: the item says so and refuses with words."},
		{name: "not-read", label: "Not read", about: "A file hardn can't make out", host: func() *fakeHost {
			h := lfUbuntu(lfYMLTCP)
			h.f.AddFile(lfOther, []byte("action(type=\"omfwd\" target=\"192.0.2.50\"\n"), 0o644, 0, 0)
			return h
		}, note: ubuntu + "An action in 50-remote.conf does not close: the row is Unknown, and the item refuses with words."},
	}
}

// lfVariants are hosts play opens by name ("logforward/<host>") and the
// reviews are drawn on, which the menu does not list.
func lfVariants() []lfHost {
	return []lfHost{
		{name: "tls-off", host: func() *fakeHost {
			h := lfUbuntu(lfYMLTLS)
			h.f.AddFile(lfCAFile, []byte("-----BEGIN CERTIFICATE-----\n"), 0o644, 0, 0)
			return h
		}, note: hostFQDN + " runs Ubuntu 24.04 and forwards nowhere; hardn.yml asks for TLS, and rsyslog-gnutls is not installed."},
		{name: "several-off", host: func() *fakeHost { return lfWithTLS(lfUbuntu(lfYMLThree)) },
			note: hostFQDN + " runs Ubuntu 24.04 and forwards nowhere; hardn.yml names three servers."},
		{name: "alpine-off", host: func() *fakeHost { return lfAlpine(lfYMLUDP) },
			note: hostFQDN + " runs Alpine 3.23 and forwards nowhere; hardn.yml asks for UDP."},
		{name: "alpine-several-off", host: func() *fakeHost { return lfAlpine(lfYMLTwoUDP) },
			note: hostFQDN + " runs Alpine 3.23 and forwards nowhere; hardn.yml names two UDP servers."},
		{name: "alpine-mixed", host: func() *fakeHost { return lfAlpine(lfYMLMixed) },
			note: hostFQDN + " runs Alpine 3.23 and forwards nowhere; hardn.yml names a UDP server and a TCP one."},
		{name: "twice", host: func() *fakeHost { return lfUbuntu(lfYMLTwice) },
			note: hostFQDN + " runs Ubuntu 24.04; hardn.yml names one address and port twice."},
		{name: "refused", host: func() *fakeHost { h := lfUbuntu(lfYMLTCP); h.lf.refuses = true; return h },
			note: hostFQDN + " runs Ubuntu 24.04; rsyslogd refuses its own settings (rsyslogd -N1), so Set servers refuses before it writes."},
	}
}

func findLFHost(name string) (lfHost, bool) {
	all := slices.Concat(lfHosts(), lfVariants())
	i := slices.IndexFunc(all, func(h lfHost) bool { return h.name == name })
	if i < 0 {
		return lfHost{}, false
	}
	return all[i], true
}

// lfDraw draws the screen on a host as hardn draws it: the control's
// reading through the screen's own mapping (lfscreen.View).
func lfDraw(lh lfHost) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		sys := lh.host().sys()
		facts, err := detect.Detect(ctx, sys)
		if err != nil {
			return "", err
		}
		cfg, err := statusConfig(ctx, sys)
		if err != nil {
			return "", err
		}
		return lfscreen.View(lfctl.Read(ctx, sys, facts, cfg)).SubMenu().Render(c), nil
	}
}

// lfReview runs the real screen on a host with key typed and nothing
// after it, and returns what it drew after the screen itself (reviewOf):
// the item's refusal, or its review, the engine's plan and the y/N
// question, left unanswered, so nothing is applied.
func lfReview(lh lfHost, key string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		h := lh.host()
		sys := h.sys()
		facts, err := detect.Detect(ctx, sys)
		if err != nil {
			return "", err
		}
		cfg, err := statusConfig(ctx, sys)
		if err != nil {
			return "", err
		}
		first := lfscreen.View(lfctl.Read(ctx, lh.host().sys(), facts, cfg)).SubMenu().Render(c)
		return reviewOf(c, key, first, func(p nav.Prompter) error {
			return lfscreen.Run(ctx, lfscreen.Options{
				Prompter: p, System: sys, Facts: facts, Config: cfg, LogPath: output.LegacyLogPath, Now: clock,
				NewEngine: func(progress func(engine.Event)) *engine.Engine { return updEngine(h, progress, &updQuiet) },
			})
		})
	}
}

// reviewOf runs a screen (run) with key typed and nothing after it, and
// returns what it drew after first, the screen itself as it was first
// drawn: the item's refusal, or its review, the plan and the question, left
// unanswered. The end of the input is not an error. A sub-menu's clear
// (nav.Prompter.Sub) is left out, with the blank line it adds: a listing
// shows its screens one under another.
func reviewOf(c component.Ctx, key, first string, run func(nav.Prompter) error) (string, error) {
	var out bytes.Buffer
	in := prompt.New(strings.NewReader(key+"\n"), false)
	err := run(nav.Prompter{Ctx: c, In: in, Out: &out})
	if err != nil && !errors.Is(err, prompt.ErrAborted) && !errors.Is(err, io.EOF) {
		return "", err
	}
	var clear bytes.Buffer
	_ = c.Env.Clear(&clear)
	written := out.String()
	if clear.Len() > 0 {
		written = strings.ReplaceAll(written, clear.String()+strings.Repeat("\n", theme.TopRows-theme.SectionGap), "")
	}
	drawn := strings.TrimPrefix(strings.TrimLeft(written, "\n"), strings.TrimLeft(first, "\n"))
	if i := strings.Index(drawn, "\n"); i >= 0 {
		drawn = drawn[i+1:] // the prompt's line, with the key's echo
	}
	return strings.Trim(drawn, "\n"), nil
}

// lfMocks are the listing's mocks: each host's screen, then each change's
// review up to its question, and two refusals.
func lfMocks() []listMock {
	var out []listMock
	for _, lh := range lfHosts() {
		out = append(out, listMock{name: lh.name, note: lh.note, draw: lfDraw(lh)})
	}
	host := func(name string) lfHost {
		lh, ok := findLFHost(name)
		if !ok {
			panic("logforward: no host " + name)
		}
		return lh
	}
	return append(out,
		listMock{name: "review", note: "off's Set servers, up to its question: hardn's file as a diff, rsyslogd's check, the restart, and a test line for the server to show. y/N: it cannot lock anyone out.",
			draw: lfReview(host("off"), screen.LogForwardFirstKey)},
		listMock{name: "review-tls", note: "Set servers with hardn.yml asking for TLS: rsyslog-gnutls is installed first, and the file names the driver, the server's name and the CA file.",
			draw: lfReview(host("tls-off"), screen.LogForwardFirstKey)},
		listMock{name: "review-several", note: "Set servers with three servers in hardn.yml: a Server row each, one file with an action and a queue for each, one check, one restart, one test line. The question counts them.",
			draw: lfReview(host("several-off"), screen.LogForwardFirstKey)},
		listMock{name: "review-add", note: "several-differ's Set servers: a third server is the same one change, and the diff is that server's action.",
			draw: lfReview(host("several-differ"), screen.LogForwardFirstKey)},
		listMock{name: "review-journald", note: "journald's Set servers: rsyslog is installed first, and the notice says what else that does.",
			draw: lfReview(host("journald"), screen.LogForwardFirstKey)},
		listMock{name: "review-alpine", note: "Set servers on Alpine with hardn.yml asking for UDP: no drop-in, hardn's words in /etc/conf.d/syslog under a comment naming them, and the restart that reads them.",
			draw: lfReview(host("alpine-off"), screen.LogForwardFirstKey)},
		listMock{name: "review-alpine-several", note: "Set servers on Alpine with two UDP servers: one -R each, and the comment names them all.",
			draw: lfReview(host("alpine-several-off"), screen.LogForwardFirstKey)},
		listMock{name: "review-stop", note: "tcp's Stop forwarding: hardn's file deleted and rsyslog restarted. Nothing else is undone.",
			draw: lfReview(host("tcp"), screen.LogForwardFirstKey)},
		listMock{name: "review-stop-several", note: "several's Stop forwarding: hardn's file, every server of it, deleted.",
			draw: lfReview(host("several"), screen.LogForwardFirstKey)},
		listMock{name: "review-move", note: "mismatch's Set servers: hardn's file rewritten for hardn.yml's server.",
			draw: lfReview(host("mismatch"), screen.LogForwardFirstKey)},
		listMock{name: "refuse-no-server", note: "no-server's Set servers: refused with words, before anything is asked.",
			draw: lfReview(host("no-server"), screen.LogForwardFirstKey)},
		listMock{name: "refuse-twice", note: "Set servers with one address and port twice in hardn.yml: the whole list is refused, saying which entry.",
			draw: lfReview(host("twice"), screen.LogForwardFirstKey)},
		listMock{name: "refuse-alpine-tcp", note: "alpine-tcp's Set servers: refused with words.",
			draw: lfReview(host("alpine-tcp"), screen.LogForwardFirstKey)},
		listMock{name: "refuse-alpine-mixed", note: "Set servers on Alpine with a UDP and a TCP server in hardn.yml: the whole list is refused, naming the TCP one, and nothing is set for the other.",
			draw: lfReview(host("alpine-mixed"), screen.LogForwardFirstKey)},
		listMock{name: "refuse-not-read", note: "not-read's Set servers: refused, naming the file and the line.",
			draw: lfReview(host("not-read"), screen.LogForwardFirstKey)},
		listMock{name: "refuse-rsyslogd", note: "Set servers where rsyslogd refuses its own settings: refused with rsyslogd's first line.",
			draw: lfReview(host("refused"), screen.LogForwardFirstKey)},
	)
}

// lfUsage is the listing's usage line.
const lfUsage = "usage: go run ./internal/tools/uigallery logforward [flags] [mock | all]..."

// lfListing is the log forwarding listing.
var lfListing = listing{prefix: lfPrefix, usage: lfUsage, label: lfLabel, mocks: lfMocks}

// openLogForward returns the action that opens what name names: the hosts'
// menu for lfPrefix alone, the screen on one host for "logforward/<host>".
func (s *session) openLogForward(name string) func() error {
	if lh, ok := findLFHost(strings.TrimPrefix(name, lfPrefix+"/")); ok && name != lfPrefix {
		return func() error { return s.lfScreen(context.Background(), lh, lh.host()) }
	}
	return func() error {
		exit := component.MenuItem{Key: "0", Label: "Return"}
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Syslog Settings hosts"}, Heading: "Select an option", Prompt: "Option", Menu: component.Menu{Exit: exit}}
		menu := nav.Menu{Exit: exit}
		for i, lh := range lfHosts() {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: lh.label, Description: lh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.lfScreen(context.Background(), lh, lh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// lfScreen runs the Syslog Settings screen on h, lh's host, until the user
// goes back from it, as updScreen runs the Auto Updates screen: its note
// above it, detect's facts, the host's hardn.yml, the gallery's engine over
// the host with the session's hold, its apply recorded for the end of
// play. A dry run when play plays the dry-run sequence.
func (s *session) lfScreen(ctx context.Context, lh lfHost, h *fakeHost) error {
	sys, title := h.sys(), lfPrefix+"/"+lh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	cfg, err := statusConfig(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, lh.note); err != nil {
		return err
	}
	var rec *recorder
	return lfscreen.Run(ctx, lfscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Config: cfg, DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			r.begin()
			return updEngine(h, func(ev engine.Event) { r.event(ev); progress(ev) }, s)
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, rec)
			s.mu.Unlock()
		},
	})
}

// lfStart returns the hosts' menu, or the host, that args name, and
// whether they name one; an error for a host that is not one.
func lfStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != lfPrefix && !strings.HasPrefix(args[0], lfPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], lfPrefix+"/"); args[0] != lfPrefix {
		if _, ok := findLFHost(name); !ok {
			var names []string
			for _, h := range lfHosts() {
				names = append(names, lfPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no logforward host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}
