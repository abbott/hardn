package authkeys_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Synthetic keys, made on 2026-11-03 with ssh-keygen -t ed25519, -t ecdsa
// -b 256 and -t rsa -b 2048 (the private keys deleted), and each
// fingerprint as ssh-keygen -lf printed it. skKey is built by hand (a
// 32-byte key 00..1f, application "ssh:"); ssh-keygen -lf printed its
// fingerprint as ED25519-SK.
const (
	edKey  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPv+XCjPufyLY4lN/rf4L+GBjuPX28ZtuAnrSgNwM+b/ george@web-01"
	edFP   = "SHA256:ru5aOXeDVJBEyWJLnRoNitaauPexn7NJ8ny+6qi6bQ0"
	ecKey  = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBP7/cxIzUprEPzw2yVyXxP+Z5RZPyceazijnMtwGJV8mcQe3qOA03vkt5nMRXJ/2YSn3CjHkE9JfHniQpBZzcvA= alice laptop 2026"
	ecFP   = "SHA256:JY9EzahjNsVSGR0+kGtaG1qPOjHEXNu/4JJWQ9wlQ9A"
	rsaKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDw2Y8JTPw4aKCGjJQpNIu1Zmb9JtUTOhWgfhQRyMmK1pOgpMUx8flrP3qTM70gw0Y+yVKIr0kCRyA164eeXXdMzbXoIyrdmzVFtXrPxKmfhySuSBo8CD6hzfb7BUV6EWMkFlDEp32NhDUSKbEmmGR4U5YKihzUbS0GMSlBuKc9IYsw9aIW7ZlbmSWBm/Qndc+7ElCRGGuCPb7V67mogNrrHZLLMBYhT4AFXHC4ZGvNkvaN096kgCIj1fBvHsoXCaKIPgkAYlWsJnTusVaJT4nINkG8/2eEjyjbMQjX6copl/t//JUGkRHjUEuKqmGJ0eGP3Dej4WgSRrSgx5kUp/Zb carol"
	rsaFP  = "SHA256:3cxHhNEROOPn34Bgnh6+HLZsgVmGGqf8Omn85/oC07M"
	skKey  = "sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29tAAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fAAAABHNzaDo= george yubikey"
	skFP   = "SHA256:/p0CbeE3dk2SyW1OXXsThGc12ezDVD8eGw2/vtztDfk"
)

var (
	george = accounts.User{Name: "george", UID: 1000, GID: 1000, Home: "/home/george", Shell: "/bin/bash"}
	// debian and alpine are AuthorizedKeysFile as the VMs' sshd -T printed it.
	debian = []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}
	alpine = []string{".ssh/authorized_keys"}
	ctx    = context.Background()
)

// TestParseLine: the forms sshd(8) reads, options with quoted commas and
// spaces read past, and what is not a key.
func TestParseLine(t *testing.T) {
	for _, tc := range []struct {
		name, line         string
		typ, comment, opts string
		fp                 string
		err                bool
	}{
		{"ed25519", edKey, "ssh-ed25519", "george@web-01", "", edFP, false},
		{"ecdsa, a comment with spaces", ecKey, "ecdsa-sha2-nistp256", "alice laptop 2026", "", ecFP, false},
		{"rsa", rsaKey, "ssh-rsa", "carol", "", rsaFP, false},
		{"sk", skKey, "sk-ssh-ed25519@openssh.com", "george yubikey", "", skFP, false},
		{"CRLF", edKey + "\r", "ssh-ed25519", "george@web-01", "", edFP, false},
		{"no comment", strings.TrimSuffix(edKey, " george@web-01"), "ssh-ed25519", "", "", edFP, false},
		{"options", `no-pty,from="192.0.2.1" ` + edKey, "ssh-ed25519", "george@web-01", `no-pty,from="192.0.2.1"`, edFP, false},
		{"options with quoted commas and spaces", `command="echo a, b \"c d\"",no-agent-forwarding ` + edKey,
			"ssh-ed25519", "george@web-01", `command="echo a, b \"c d\"",no-agent-forwarding`, edFP, false},
		{"an unclosed quote", `command="echo ` + edKey, "", "", "", "", true},
		{"blank", "  ", "", "", "", "", true},
		{"a comment line", "# " + edKey, "", "", "", "", true},
		{"a bad blob", "ssh-ed25519 AAAA!!!! george", "", "", "", "", true},
		{"a type mismatch", "ssh-rsa" + strings.TrimPrefix(edKey, "ssh-ed25519"), "", "", "", "", true},
		{"DSA", "ssh-dss AAAAB3NzaC1kc3MAAAA= old", "", "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, err := authkeys.ParseLine(tc.line)
			if tc.err {
				if !errors.Is(err, authkeys.ErrNotKey) {
					t.Errorf("ParseLine = %+v, %v; want ErrNotKey", k, err)
				}
				return
			}
			if err != nil || k.Type != tc.typ || k.Comment != tc.comment || k.Options != tc.opts || k.Fingerprint != tc.fp || k.Line != strings.TrimSpace(tc.line) {
				t.Errorf("ParseLine = %+v, %v", k, err)
			}
		})
	}
}

// TestParsePublic: a pasted key, trimmed; no options, one line.
func TestParsePublic(t *testing.T) {
	k, err := authkeys.ParsePublic("  " + ecKey + "\r\n")
	if err != nil || k.Line != ecKey || k.Fingerprint != ecFP || k.Options != "" {
		t.Errorf("ParsePublic = %+v, %v", k, err)
	}
	for _, s := range []string{`no-pty ` + edKey, edKey + "\n" + rsaKey, "", "ssh-ed25519", "ssh-rsa" + strings.TrimPrefix(edKey, "ssh-ed25519")} {
		if _, err := authkeys.ParsePublic(s); !errors.Is(err, authkeys.ErrNotKey) {
			t.Errorf("ParsePublic(%.30q) = %v, want ErrNotKey", s, err)
		}
	}
	a, _ := authkeys.ParsePublic(edKey)
	b, _ := authkeys.ParseLine("no-pty " + strings.TrimSuffix(edKey, "george@web-01") + "other")
	if !a.Same(b) || a.Same(k) {
		t.Errorf("Same: %v, %v", a.Same(b), a.Same(k))
	}
}

// TestPath: %h, %u and %% expanded, a relative entry under the home.
func TestPath(t *testing.T) {
	for entry, want := range map[string]string{
		".ssh/authorized_keys":    "/home/george/.ssh/authorized_keys",
		"%h/.ssh/authorized_keys": "/home/george/.ssh/authorized_keys",
		"/etc/ssh/keys/%u":        "/etc/ssh/keys/george",
		"/etc/ssh/%%/%u":          "/etc/ssh/%/george",
	} {
		if got := authkeys.Path(entry, george); got != want {
			t.Errorf("Path(%q) = %q, want %q", entry, got, want)
		}
	}
	if !authkeys.Plannable(debian) || !authkeys.Plannable([]string{"%h/.ssh/authorized_keys"}) ||
		authkeys.Plannable([]string{".ssh/authorized_keys2"}) || authkeys.Plannable([]string{"none"}) || authkeys.Plannable(nil) {
		t.Error("Plannable")
	}
}

// keyHost is a host with george's home 0700 and, unless dir is false,
// ~/.ssh 0700, both his, and the files given (0600, his).
func keyHost(dir bool, files map[string]string) *hostfake.System {
	h := hostfake.New(host.Options{})
	h.AddDir("/home", 0o755, 0, 0)
	h.AddDir(george.Home, 0o700, george.UID, george.GID)
	if dir {
		h.AddDir(george.Home+"/.ssh", 0o700, george.UID, george.GID)
	}
	for p, d := range files {
		h.AddFile(p, []byte(d), 0o600, george.UID, george.GID)
	}
	return h
}

const (
	keysFile  = "/home/george/.ssh/authorized_keys"
	keys2File = "/home/george/.ssh/authorized_keys2"
)

// TestRead: the files in order, "none" skipped, authorized_keys2 counted
// and a key in both once; a file not there has no Data.
func TestRead(t *testing.T) {
	h := keyHost(true, map[string]string{keysFile: "# george\n" + edKey + "\n\n" + ecKey + "\r\n", keys2File: edKey + "\n" + rsaKey})
	files := authkeys.Read(ctx, h, append([]string{"none"}, debian...), george)
	if len(files) != 2 || files[0].Path != keysFile || files[1].Path != keys2File || len(files[0].Keys) != 2 || len(files[1].Keys) != 2 {
		t.Fatalf("Read = %+v", files)
	}
	var fps []string
	for _, k := range authkeys.Keys(files) {
		fps = append(fps, k.Fingerprint)
	}
	if !slices.Equal(fps, []string{edFP, ecFP, rsaFP}) {
		t.Errorf("Keys = %q", fps)
	}
	files = authkeys.Read(ctx, keyHost(false, nil), alpine, george)
	if len(files) != 1 || files[0].Data != nil || files[0].Err != nil {
		t.Errorf("Read, none there = %+v", files)
	}
}

// TestCheckStrict: StrictModes' rule on the home and ~/.ssh.
func TestCheckStrict(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *hostfake.System)
		want string // "" for nil
	}{
		{"the user's, 0700", func(*hostfake.System) {}, ""},
		{"~/.ssh absent", func(h *hostfake.System) { _ = h.Remove(ctx, george.Home+"/.ssh") }, ""},
		{"a root home", func(h *hostfake.System) { h.AddDir(george.Home, 0o755, 0, 0) }, ""},
		{"a group-writable home", func(h *hostfake.System) { h.AddDir(george.Home, 0o775, george.UID, george.GID) },
			"/home/george is writable by its group (0775)"},
		{"a home anyone writes", func(h *hostfake.System) { h.AddDir(george.Home, 0o707, george.UID, george.GID) },
			"/home/george is writable by anyone (0707)"},
		{"someone else's home", func(h *hostfake.System) { h.AddDir(george.Home, 0o700, 1001, 1001) },
			"/home/george is uid 1001's, not george's"},
		{"a root ~/.ssh", func(h *hostfake.System) { h.AddDir(george.Home+"/.ssh", 0o700, 0, 0) },
			"/home/george/.ssh is uid 0's, not george's"},
		{"a group-writable ~/.ssh", func(h *hostfake.System) { h.AddDir(george.Home+"/.ssh", 0o770, george.UID, george.GID) },
			"/home/george/.ssh is writable by its group (0770)"},
		{"a symlinked ~/.ssh", func(h *hostfake.System) {
			_ = h.Remove(ctx, george.Home+"/.ssh")
			h.AddDir("/srv/keys", 0o700, george.UID, george.GID)
			h.AddSymlink(george.Home+"/.ssh", "/srv/keys")
		}, "/home/george/.ssh is a symlink"},
		{"no home", func(h *hostfake.System) { _ = h.Remove(ctx, george.Home+"/.ssh"); _ = h.Remove(ctx, george.Home) }, "/home/george isn't there"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := keyHost(true, nil)
			tc.set(h)
			err := authkeys.CheckStrict(ctx, h, george)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("CheckStrict = %v", err)
			case tc.want != "" && (!errors.Is(err, authkeys.ErrStrictModes) || !strings.HasPrefix(err.Error(), tc.want+": ")):
				t.Errorf("CheckStrict = %v, want %q", err, tc.want)
			}
		})
	}
}

func key(t *testing.T, s string) authkeys.Key {
	t.Helper()
	k, err := authkeys.ParsePublic(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestPlanAdd: appended, every other line kept byte for byte, a final
// newline added; idempotent over every file sshd reads; ~/.ssh made only
// where it is absent; not plannable refused.
func TestPlanAdd(t *testing.T) {
	before := "# george\r\nno-pty " + edKey + "\n\n" + ecKey
	for _, tc := range []struct {
		name    string
		dir     bool
		files   map[string]string
		entries []string
		k       string
		after   string // "" for an empty plan
		mkdir   bool
	}{
		{"appended, a newline added", true, map[string]string{keysFile: before}, debian, rsaKey, before + "\n" + rsaKey + "\n", false},
		{"no file", true, nil, alpine, rsaKey, rsaKey + "\n", false},
		{"no ~/.ssh", false, nil, alpine, skKey, skKey + "\n", true},
		{"already there, with options", true, map[string]string{keysFile: before}, debian, edKey, "", false},
		{"already in authorized_keys2", true, map[string]string{keys2File: rsaKey + "\n"}, debian, rsaKey, "", false},
		{"%h", true, map[string]string{keysFile: rsaKey + "\n"}, []string{"%h/.ssh/authorized_keys"}, edKey, rsaKey + "\n" + edKey + "\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := authkeys.PlanAdd(ctx, keyHost(tc.dir, tc.files), george, tc.entries, key(t, tc.k))
			if err != nil {
				t.Fatal(err)
			}
			if tc.after == "" {
				if !p.Empty() {
					t.Errorf("PlanAdd = %+v, want empty", p)
				}
				return
			}
			b, ok := tc.files[keysFile]
			wantMk := []string(nil)
			if tc.mkdir {
				wantMk = []string{"install", "-d", "-m", "700", "-o", "1000", "-g", "1000", "/home/george/.ssh"}
			}
			if p.Empty() || p.Path != keysFile || p.After != tc.after || (p.Before != nil) != ok || ok && *p.Before != b || !slices.Equal(p.MakeDir, wantMk) {
				t.Errorf("PlanAdd = %+v", p)
			}
		})
	}
	if _, err := authkeys.PlanAdd(ctx, keyHost(true, nil), george, []string{".ssh/authorized_keys2"}, key(t, edKey)); !errors.Is(err, authkeys.ErrNotPlanned) {
		t.Errorf("PlanAdd, not plannable = %v", err)
	}
}

// TestPlanRemove: every line holding the key dropped, the rest kept byte
// for byte; refused where authorized_keys2 has it too; not found.
func TestPlanRemove(t *testing.T) {
	before := "# george\r\nno-pty " + edKey + "\r\n" + ecKey + "\n\n" + edKey + "\n" + rsaKey
	h := keyHost(true, map[string]string{keysFile: before, keys2File: skKey + "\n"})
	p, err := authkeys.PlanRemove(ctx, h, george, debian, edFP)
	if want := "# george\r\n" + ecKey + "\n\n" + rsaKey; err != nil || p.Path != keysFile || p.After != want || p.Before == nil || *p.Before != before || p.MakeDir != nil {
		t.Errorf("PlanRemove = %+v, %v", p, err)
	}
	h = keyHost(true, map[string]string{keysFile: before, keys2File: edKey + "\n"})
	if _, err := authkeys.PlanRemove(ctx, h, george, debian, edFP); !errors.Is(err, fs.ErrExist) || !strings.Contains(err.Error(), keys2File) {
		t.Errorf("PlanRemove, in authorized_keys2 too = %v", err)
	}
	for _, fp := range []string{skFP, "SHA256:none"} {
		if _, err := authkeys.PlanRemove(ctx, keyHost(true, map[string]string{keysFile: before}), george, alpine, fp); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("PlanRemove(%s) = %v, want fs.ErrNotExist", fp, err)
		}
	}
	if _, err := authkeys.PlanRemove(ctx, keyHost(false, nil), george, alpine, edFP); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("PlanRemove, no file = %v", err)
	}
	if _, err := authkeys.PlanRemove(ctx, h, george, []string{"none"}, edFP); !errors.Is(err, authkeys.ErrNotPlanned) {
		t.Errorf("PlanRemove, not plannable = %v", err)
	}
}

// TestPlansOnlyRead: planning runs nothing and writes nothing.
func TestPlansOnlyRead(t *testing.T) {
	h := keyHost(false, nil)
	if _, err := authkeys.PlanAdd(ctx, h, george, debian, key(t, edKey)); err != nil {
		t.Fatal(err)
	}
	if fi, err := h.Lstat(ctx, george.Home+"/.ssh"); err == nil {
		t.Errorf("~/.ssh made: %v", fi.Mode())
	}
}

// TestTrailingSlashHome: a home "/home/george/" plans the same path both
// ways, .ssh/authorized_keys and %h/.ssh/authorized_keys; where no file
// read is the planned one (%h with no home: "/.ssh/authorized_keys"),
// both plans are ErrNotPlanned, never an empty plan.
func TestTrailingSlashHome(t *testing.T) {
	slash := george
	slash.Home = "/home/george/"
	for _, entries := range [][]string{alpine, {"%h/.ssh/authorized_keys"}} {
		if got := authkeys.Path(entries[0], slash); got != keysFile {
			t.Errorf("Path(%q) = %q", entries[0], got)
		}
		p, err := authkeys.PlanAdd(ctx, keyHost(true, map[string]string{keysFile: rsaKey + "\n"}), slash, entries, key(t, edKey))
		if err != nil || p.Path != keysFile || p.After != rsaKey+"\n"+edKey+"\n" {
			t.Errorf("%q: PlanAdd = %+v, %v", entries, p, err)
		}
		p, err = authkeys.PlanRemove(ctx, keyHost(true, map[string]string{keysFile: rsaKey + "\n" + edKey + "\n"}), slash, entries, edFP)
		if err != nil || p.Path != keysFile || p.After != rsaKey+"\n" {
			t.Errorf("%q: PlanRemove = %+v, %v", entries, p, err)
		}
	}
	none := george
	none.Home = ""
	h := keyHost(true, map[string]string{"/.ssh/authorized_keys": rsaKey + "\n"})
	if p, err := authkeys.PlanAdd(ctx, h, none, []string{"%h/.ssh/authorized_keys"}, key(t, edKey)); !errors.Is(err, authkeys.ErrNotPlanned) {
		t.Errorf("no home: PlanAdd = %+v, %v", p, err)
	}
	if p, err := authkeys.PlanRemove(ctx, h, none, []string{"%h/.ssh/authorized_keys"}, rsaFP); !errors.Is(err, authkeys.ErrNotPlanned) {
		t.Errorf("no home: PlanRemove = %+v, %v", p, err)
	}
}

// TestRestricted: a key whose options may stop a login is Restricted: each
// option outside the allow-list, an unknown one, an allowed name in the
// wrong form; the allowed ones, any case, quoted values with commas, are
// not.
func TestRestricted(t *testing.T) {
	for opts, want := range map[string]bool{
		"": false,
		"agent-forwarding,no-agent-forwarding,port-forwarding,no-port-forwarding,X11-forwarding,no-X11-forwarding": false,
		"user-rc,no-user-rc,pty,no-touch-required,verify-required":                                                false,
		`environment="A=1,2",permitopen="192.0.2.10:22",permitlisten="localhost:8080"`:                            false,
		"NO-PTY":                      true,
		"no-pty":                      true,
		`command="/bin/false"`:        true,
		`from="192.0.2.0/24"`:         true,
		`expiry-time="20261231"`:      true,
		"restrict":                    true,
		`principals="george"`:         true,
		"cert-authority":              true,
		`tunnel="0"`:                  true,
		"no-such-option":              true,
		"pty,command=\"/bin/true\"":   true,
		"environment":                 true,
		"pty=yes":                     true,
		`environment="X=\"a,b\"",pty`: false,
	} {
		line := edKey
		if opts != "" {
			line = opts + " " + edKey
		}
		k, err := authkeys.ParseLine(line)
		if err != nil {
			t.Fatalf("%q: %v", opts, err)
		}
		if k.Restricted() != want {
			t.Errorf("%q: Restricted %v, want %v", opts, k.Restricted(), want)
		}
	}
}
