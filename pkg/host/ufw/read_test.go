package ufw_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// statusRows are "ufw status verbose" rows in get_status's forms: a port
// alone, IPv6 with "(v6)", a verbose application, an address, a range and
// a list, an interface, a restricted source, a log type and a comment.
const statusRows = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
2230/tcp                   ALLOW IN    Anywhere
22/tcp (OpenSSH)           LIMIT IN    Anywhere
192.0.2.10 2230/tcp        ALLOW IN    192.0.2.0/24
6000:6007,8080/tcp         DENY IN     Anywhere                   (log) # x11 ALLOW IN y
22 on eth0                 ALLOW IN    Anywhere
Anywhere on eth1           ALLOW IN    Anywhere
Anywhere                   DENY OUT    192.0.2.7
2230/tcp (v6)              ALLOW IN    Anywhere (v6)
22/tcp (OpenSSH (v6))      LIMIT IN    Anywhere (v6)
2001:db8::10 2230/tcp      REJECT IN   2001:db8::/32

Anywhere (v6)              ALLOW FWD   Anywhere (v6)
`

func TestParseStatus(t *testing.T) {
	st := ufw.ParseStatus([]byte(statusRows))
	if !st.Known || !st.Active || len(st.Bad) > 0 {
		t.Fatalf("Known %t Active %t Bad %q", st.Known, st.Active, st.Bad)
	}
	if st.Logging != "on (low)" || st.Incoming != ufw.PolicyDeny || st.Outgoing != ufw.PolicyAllow ||
		st.Routed != ufw.PolicyDisabled || st.NewProfiles != "skip" {
		t.Errorf("header %+v", st)
	}
	type r struct {
		action, dir, proto, dst, dport, src, iface string
		fam                                        ufw.Family
	}
	want := []r{
		{"allow", "in", "tcp", "", "2230", "", "", ufw.IPv4},
		{"limit", "in", "tcp", "", "22", "", "", ufw.IPv4},
		{"allow", "in", "tcp", "192.0.2.10", "2230", "192.0.2.0/24", "", ufw.IPv4},
		{"deny", "in", "tcp", "", "6000:6007,8080", "", "", ufw.IPv4},
		{"allow", "in", "any", "", "22", "", "eth0", ufw.IPv4},
		{"allow", "in", "any", "", "", "", "eth1", ufw.IPv4},
		{"deny", "out", "any", "", "", "192.0.2.7", "", ufw.IPv4},
		{"allow", "in", "tcp", "", "2230", "", "", ufw.IPv6},
		{"limit", "in", "tcp", "", "22", "", "", ufw.IPv6},
		{"reject", "in", "tcp", "2001:db8::10", "2230", "2001:db8::/32", "", ufw.IPv6},
		{"allow", "fwd", "any", "", "", "", "", ufw.IPv6},
	}
	var got []r
	for _, x := range st.Rules {
		got = append(got, r{x.Action, x.Dir, x.Proto, x.Dst, x.DPort, x.Src, x.Iface, x.Family})
	}
	if !slices.Equal(got, want) {
		t.Errorf("rules\n got %+v\nwant %+v", got, want)
	}
}

func TestParseStatusForms(t *testing.T) {
	for _, tc := range []struct {
		name, out     string
		known, active bool
		bad           int
	}{
		{"inactive", "Status: inactive\n", true, false, 0},
		{"active, no rules", "Status: active\nLogging: off\nDefault: allow (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n", true, true, 0},
		{"no status line", "ERROR: You need to be root to run this script\n", false, false, 1},
		{"unknown policy word", "Status: active\nLogging: off\nDefault: drop (incoming), allow (outgoing), disabled (routed)\n", false, true, 1},
		{"unreadable row", statusRows + "22/tcp ALLOWED Anywhere\n", true, true, 1},
		{"text after inactive", "Status: inactive\nsomething\n", true, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := ufw.ParseStatus([]byte(tc.out))
			if st.Known != tc.known || st.Active != tc.active || len(st.Bad) != tc.bad {
				t.Errorf("Known %t Active %t Bad %q; want %t %t %d", st.Known, st.Active, st.Bad, tc.known, tc.active, tc.bad)
			}
		})
	}
}

func TestParseTuples(t *testing.T) {
	data := `*filter
### RULES ###

### tuple ### allow tcp 2230 0.0.0.0/0 any 0.0.0.0/0 in
-A ufw-user-input -p tcp --dport 2230 -j ACCEPT

### tuple ### limit tcp 22 0.0.0.0/0 any 0.0.0.0/0 OpenSSH - in comment=5353482061636365737320666f7220616c696365
### tuple ### allow_log any 22 0.0.0.0/0 any 192.0.2.0/24 in_eth0
### tuple ### deny tcp 2230 0.0.0.0/0 1234 0.0.0.0/0 - web in
### tuple ### route:allow any any 0.0.0.0/0 any 0.0.0.0/0 in_eth0!out_eth1
### tuple ### allow tcp 22 0.0.0.0/0 in
### tuple ### drop tcp 22 0.0.0.0/0 any 0.0.0.0/0 in
### END RULES ###
`
	rules, bad := ufw.ParseTuples([]byte(data), ufw.IPv4)
	type r struct{ action, dir, proto, dst, dport, src, iface, app string }
	want := []r{
		{"allow", "in", "tcp", "", "2230", "", "", ""},
		{"limit", "in", "tcp", "", "22", "", "", "OpenSSH"},
		{"allow", "in", "any", "", "22", "192.0.2.0/24", "eth0", ""},
		{"deny", "in", "tcp", "", "2230", "port 1234 app web", "", ""},
		{"allow", "fwd", "any", "", "", "", "eth0!out_eth1", ""},
	}
	var got []r
	for _, x := range rules {
		got = append(got, r{x.Action, x.Dir, x.Proto, x.Dst, x.DPort, x.Src, x.Iface, x.App})
		if x.Family != ufw.IPv4 {
			t.Errorf("%q: family %s", x.Line, x.Family)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("rules\n got %+v\nwant %+v", got, want)
	}
	if len(bad) != 2 {
		t.Errorf("bad = %q, want the six-field tuple and the unknown action", bad)
	}
}

func TestRead(t *testing.T) {
	ctx := context.Background()
	t.Run("stock, inactive", func(t *testing.T) {
		m := newModel(t)
		m.rules = []modelRule{{fam: ufw.IPv4, action: "allow", port: "2230"}, {fam: ufw.IPv6, action: "allow", port: "2230"}}
		h := m.host()
		st, err := ufw.Read(ctx, h, notPVE)
		if err != nil || !st.Known() {
			t.Fatalf("Read: %v, Known %t, Bad %q", err, st.Known(), st.Bad)
		}
		if st.Status.Active || !st.IPv6 || st.Incoming != ufw.PolicyDeny || st.Outgoing != ufw.PolicyAllow || len(st.Rules) != 2 {
			t.Errorf("state %+v", st)
		}
		for _, c := range h.Calls() {
			if c.Op != "run" && c.Op != "read" && c.Op != "stat" {
				t.Errorf("Read changed the host: %s", c)
			}
		}
		if got := runs(h); !slices.Equal(got, []string{"ufw status verbose"}) {
			t.Errorf("runs %q", got)
		}
	})
	t.Run("IPV6=no leaves IPv6's rules unread", func(t *testing.T) {
		m := newModel(t)
		m.ipv6 = false
		h := m.host()
		st, err := ufw.Read(ctx, h, notPVE)
		if err != nil || st.IPv6 {
			t.Fatalf("Read: %v, IPv6 %t", err, st.IPv6)
		}
		for _, c := range h.Calls() {
			if c.Path == ufw.User6Rules {
				t.Errorf("read %s", c.Path)
			}
		}
	})
	// /etc/default/ufw, then /etc/ufw/ufw.conf, as ufw's _get_defaults
	// reads them: a line must start `\w+="?\w+"?` (double quotes only);
	// keys and values are lower-cased, quotes stripped, the last line wins,
	// ufw.conf's over the defaults'; accept is allow, reject reject,
	// anything else deny.
	for _, tc := range []struct {
		name, data, conf string
		ipv6, enabled    bool
		in, out          ufw.Policy
	}{
		{"stock", stockDefaults, "", true, false, ufw.PolicyDeny, ufw.PolicyAllow},
		{"double quotes and case; a single-quoted value is not read; the last line wins", "ipv6=\"YES\"\nIPV6='no'\nDEFAULT_INPUT_POLICY=\"accept\"\nDEFAULT_INPUT_POLICY=REJECT\nDEFAULT_OUTPUT_POLICY=\"DROP\"\n",
			"", true, false, ufw.PolicyReject, ufw.PolicyDeny},
		{"indented and commented lines are not read", " IPV6=yes\n#DEFAULT_INPUT_POLICY=\"ACCEPT\"\nIPV6=no\nDEFAULT_INPUT_POLICY=\"BOGUS\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n",
			"", false, false, ufw.PolicyDeny, ufw.PolicyAllow},
		{"no policy lines", "IPV6=yes\n", "", true, false, "", ""},
		{"ufw.conf's keys win", stockDefaults, "ENABLED=yes\nLOGLEVEL=low\nIPV6=no\nDEFAULT_INPUT_POLICY=\"ACCEPT\"\n",
			false, true, ufw.PolicyAllow, ufw.PolicyAllow},
	} {
		t.Run("defaults: "+tc.name, func(t *testing.T) {
			h := newModel(t).host()
			h.Handler = nil // the model would rewrite the files
			h.Respond(hostfake.Response{Stdout: "Status: inactive\n"}, "ufw", "status", "verbose")
			h.AddFile(ufw.DefaultsFile, []byte(tc.data), 0o644, 0, 0)
			if tc.conf != "" {
				h.AddFile(ufw.ConfFile, []byte(tc.conf), 0o644, 0, 0)
			}
			st, err := ufw.Read(ctx, h, notPVE)
			if err != nil || st.IPv6 != tc.ipv6 || st.Enabled != tc.enabled || st.Incoming != tc.in || st.Outgoing != tc.out {
				t.Errorf("Read: %v, IPv6 %t, Enabled %t, in %q, out %q", err, st.IPv6, st.Enabled, st.Incoming, st.Outgoing)
			}
			if st.Known() != (tc.in != "") {
				t.Errorf("Known %t", st.Known())
			}
		})
	}
	t.Run("no IPv6 in the kernel", func(t *testing.T) {
		h := newModel(t).host()
		if err := h.RemoveAll(ctx, ufw.KernelIPv6); err != nil {
			t.Fatal(err)
		}
		if st, err := ufw.Read(ctx, h, notPVE); err != nil || st.IPv6 || !st.NoKernelIPv6 {
			t.Errorf("Read: %v, IPv6 %t, NoKernelIPv6 %t", err, st.IPv6, st.NoKernelIPv6)
		}
	})
	// An I/O failure on any file Read reads is an error, never a guess.
	for _, tc := range []struct {
		name   string
		breaks func(h *hostfake.System) error
		want   error
	}{
		{"defaults missing", func(h *hostfake.System) error { return h.Remove(ctx, ufw.DefaultsFile) }, fs.ErrNotExist},
		{"ufw.conf missing", func(h *hostfake.System) error { return h.Remove(ctx, ufw.ConfFile) }, fs.ErrNotExist},
		{"user.rules missing", func(h *hostfake.System) error { return h.Remove(ctx, ufw.UserRules) }, fs.ErrNotExist},
		{"user6.rules missing", func(h *hostfake.System) error { return h.Remove(ctx, ufw.User6Rules) }, fs.ErrNotExist},
		{"the kernel's IPv6 directory unreadable", func(h *hostfake.System) error {
			if err := h.RemoveAll(ctx, ufw.KernelIPv6); err != nil {
				return err
			}
			h.AddSymlink(ufw.KernelIPv6, ufw.KernelIPv6)
			return nil
		}, syscall.ELOOP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newModel(t).host()
			h.Handler = nil // the model would write the files back
			h.Respond(hostfake.Response{Stdout: "Status: inactive\n"}, "ufw", "status", "verbose")
			if err := tc.breaks(h); err != nil {
				t.Fatal(err)
			}
			if _, err := ufw.Read(ctx, h, notPVE); !errors.Is(err, tc.want) {
				t.Errorf("Read: %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("not installed", func(t *testing.T) {
		h := hostfake.New(host.Options{})
		h.Respond(hostfake.Response{Err: host.ErrNotFound}, "ufw", "status", "verbose")
		if st, err := ufw.Read(ctx, h, notPVE); err != nil || st.Installed {
			t.Errorf("Read: %v, Installed %t", err, st.Installed)
		}
	})
	t.Run("ufw fails", func(t *testing.T) {
		h := hostfake.New(host.Options{})
		h.Respond(hostfake.Response{ExitCode: 1, Stderr: "ERROR: You need to be root to run this script\n"}, "ufw", "status", "verbose")
		var xe *host.ExitError
		if _, err := ufw.Read(ctx, h, notPVE); !errors.As(err, &xe) {
			t.Errorf("Read: %v, want *host.ExitError", err)
		}
	})
	for _, tc := range []struct {
		name string
		pve  detect.Proxmox
		want error
	}{
		{"Proxmox VE: never touched", detect.Proxmox{Installed: detect.Yes}, ufw.ErrNotApplicable},
		{"Proxmox VE unknown", detect.Proxmox{Err: detect.ErrConflict}, ufw.ErrProxmoxUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			_, err := ufw.Read(ctx, h, tc.pve)
			if !errors.Is(err, tc.want) {
				t.Errorf("Read: %v, want %v", err, tc.want)
			}
			if len(h.Calls()) != 0 {
				t.Errorf("calls %v, want none", h.Calls())
			}
		})
	}
}
