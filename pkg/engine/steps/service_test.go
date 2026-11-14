package steps

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestRefuse: the sentinel alone, or wrapping its cause; the code and the
// words as given.
func TestRefuse(t *testing.T) {
	sentinel, cause := errors.New("not read"), errors.New("permission denied")
	var rf *Refusal
	if err := Refuse("X_CODE", sentinel, nil, "why", "fix"); !errors.As(err, &rf) || rf.Err != sentinel || rf.Code != "X_CODE" || rf.Why != "why" || rf.Fix != "fix" {
		t.Fatalf("Refuse = %#v", err)
	}
	err := Refuse("X_CODE", sentinel, cause, "why", "")
	if !errors.Is(err, sentinel) || !errors.Is(err, cause) || !errors.As(err, &rf) || rf.Err.Error() != "not read: permission denied" {
		t.Fatalf("Refuse with a cause = %v", err)
	}
}

// TestHostWords: one line, without the package's prefix and the sentinel's
// words, and safe to show.
func TestHostWords(t *testing.T) {
	sentinel := errors.New("forwarding hardn can't make out")
	for in, want := range map[string]string{
		"logfwd: /etc/conf.d/syslog:3: SYSLOGD_OPTS set twice: forwarding hardn can't make out": "/etc/conf.d/syslog:3: SYSLOGD_OPTS set twice",
		"logfwd: open /etc/rsyslog.conf: permission denied\nmore":                               "open /etc/rsyslog.conf: permission denied",
		"other: logfwd: x": "other: logfwd: x",
	} {
		if got := HostWords(errors.New(in), "logfwd: ", sentinel); got != want {
			t.Errorf("HostWords(%q) = %q, want %q", in, got, want)
		}
	}
	if got := HostWords(errors.New("logfwd: a\x1b[31mb"), "logfwd: ", sentinel); got == "a\x1b[31mb" {
		t.Errorf("HostWords keeps an escape: %q", got)
	}
	if FirstLine("a\nb") != "a" || FirstLine("a") != "a" || FirstLine("") != "" {
		t.Error("FirstLine")
	}
	if got := IDs([]engine.Change{{ID: "a"}, {ID: "b"}}); !slices.Equal(got, []string{"a", "b"}) || IDs(nil) != nil {
		t.Errorf("IDs = %v", got)
	}
}

// TestServiceArgv: by the init that booted the host, and only where the
// service has a name under it.
func TestServiceArgv(t *testing.T) {
	facts := func(k detect.InitKind, running detect.Tri) detect.Facts {
		return detect.Facts{Init: detect.Init{Kind: k, Running: running}}
	}
	for _, tc := range []struct {
		name          string
		f             detect.Facts
		unit, service string
		want          []string
	}{
		{"systemd", facts(detect.Systemd, detect.Yes), "rsyslog", "", []string{"systemctl", "restart", "rsyslog"}},
		{"systemd, both named", facts(detect.Systemd, detect.Yes), "chrony", "chronyd", []string{"systemctl", "restart", "chrony"}},
		{"OpenRC", facts(detect.OpenRC, detect.Yes), "", "syslog", []string{"rc-service", "syslog", "restart"}},
		{"OpenRC, both named", facts(detect.OpenRC, detect.Yes), "chrony", "chronyd", []string{"rc-service", "chronyd", "restart"}},
		{"systemd, an OpenRC service", facts(detect.Systemd, detect.Yes), "", "syslog", nil},
		{"OpenRC, a systemd unit", facts(detect.OpenRC, detect.Yes), "rsyslog", "", nil},
		{"systemd installed, not booted", facts(detect.Systemd, detect.No), "rsyslog", "", nil},
		{"OpenRC, not established", facts(detect.OpenRC, detect.Unknown), "", "syslog", nil},
		{"no init", facts(detect.InitUnknown, detect.Yes), "rsyslog", "syslog", nil},
	} {
		argv, ok := ServiceArgv(tc.f, tc.unit, tc.service, "restart")
		if !slices.Equal(argv, tc.want) || ok != (tc.want != nil) {
			t.Errorf("%s: ServiceArgv = %v, %v; want %v", tc.name, argv, ok, tc.want)
		}
	}
}

// TestRewriteFile: a file that cannot be read is Unread's refusal of the
// read's own error, edit's error is edit's, and the apply refuses a file
// changed since the plan before it writes.
func TestRewriteFile(t *testing.T) {
	ctx := context.Background()
	const path = "/etc/conf.d/syslog"
	changed, mine := errors.New("changed since"), errors.New("not mine")
	unread := errors.New("not read")
	r := Rewrite{Control: "ctl", ID: "ctl/options", Title: "Set the options", Path: path, Detail: "written", Changed: changed,
		Unread: func(err error) error { return errors.Join(unread, err) }}
	upper := func([]byte) ([]byte, error) { return []byte("B\n"), nil }

	sys := hostfake.New(host.Options{})
	sys.AddDir("/etc/conf.d", 0o755, 0, 0)
	if _, err := RewriteFile(ctx, sys, r, upper); !errors.Is(err, unread) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("no file: %v, want Unread's refusal of fs.ErrNotExist", err)
	}
	sys.AddFile(path, []byte("a\n"), 0o600, 0, 0)
	if _, err := RewriteFile(ctx, sys, r, func([]byte) ([]byte, error) { return nil, mine }); err != mine {
		t.Fatalf("edit's error: %v, want it as it is", err)
	}
	ch, err := RewriteFile(ctx, sys, r, upper)
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID != r.ID || ch.Title != r.Title || ch.Summary != "Set the options in "+path || ch.Target != path || ch.Stage != engine.StageFiles ||
		!slices.Equal(ch.Resources, []string{"ctl"}) || !slices.Equal(ch.Backups, []string{path}) || ch.Diff == "" {
		t.Fatalf("change = %+v", ch)
	}
	ran, err := ch.Apply(ctx, sys, engine.Run{})
	if got, _ := sys.ReadFile(ctx, path); err != nil || ran.Detail != "written" || string(got) != "B\n" {
		t.Fatalf("apply = %+v, %v; file %q", ran, err, got)
	}
	if fi, err := sys.Stat(ctx, path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode after the apply: %v, %v; want it kept", fi, err)
	}
	// The file is no longer as the plan read it.
	if _, err := ch.Apply(ctx, sys, engine.Run{}); err != changed {
		t.Fatalf("apply on a changed file = %v, want the Changed refusal", err)
	}
	if got, _ := sys.ReadFile(ctx, path); string(got) != "B\n" {
		t.Fatalf("a refused apply wrote %q", got)
	}
}

// TestWriteOwnFile: without a check the file is written and nothing runs.
// (With one: the Log Forwarding control's tests, through rsyslogd -N1.)
func TestWriteOwnFile(t *testing.T) {
	ctx := context.Background()
	const path = "/etc/chrony/sources.d/hardn.sources"
	sys := hostfake.New(host.Options{})
	sys.AddDir("/etc/chrony/sources.d", 0o755, 0, 0)
	ch := WriteOwnFile(OwnFile{Control: "ctl", ID: "ctl/file", Title: "Write hardn's file", Path: path, Data: []byte("server a\n"), After: []string{"ctl/install"}})
	if ch.Summary != "Write hardn's file "+path || ch.Target != path || len(ch.Commands) != 0 || !slices.Equal(ch.After, []string{"ctl/install"}) ||
		!slices.Equal(ch.Backups, []string{path}) || !slices.Equal(ch.Resources, []string{"ctl"}) {
		t.Fatalf("change = %+v", ch)
	}
	if _, err := ch.Apply(ctx, sys, engine.Run{}); err != nil {
		t.Fatal(err)
	}
	if got, err := sys.ReadFile(ctx, path); err != nil || string(got) != "server a\n" {
		t.Fatalf("file = %q, %v", got, err)
	}
}
