package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The DNS screen on the simulated hosts (next/s2-screen): hardn's own
// screen (pkg/screens/dns) on the resolver model, its changes on the real
// engine. Nothing here runs a command or writes a file outside the fake.

// playDNS plays the DNS screen on a fresh host of dh's, plain, the user
// typing lines, a dry run with dryRun, and returns the session, the host,
// the transcript and the exit status.
func playDNS(t *testing.T, dh dnsScreenHost, dryRun bool, lines ...string) (*session, *fakeHost, string, int) {
	t.Helper()
	h := dh.host()
	s, out, code := playWith(t, dnsPrefix+"/"+dh.name, func(s *session) func() error {
		if dryRun {
			s.sequence = "dry-run"
		}
		return func() error { return s.dnsScreen(context.Background(), dh, h) }
	}, lines...)
	if u := h.unanswered(); len(u) > 0 {
		t.Errorf("%s: the simulated host was asked to run what it does not model: %q", dh.name, u)
	}
	return s, h, out, code
}

// dnsScreenHostNamed is the DNS screen's host named name.
func dnsScreenHostNamed(t *testing.T, name string) dnsScreenHost {
	t.Helper()
	dh, ok := findDNSHost(name)
	if !ok {
		t.Fatalf("no DNS host %q", name)
	}
	return dh
}

// TestDNSScreenHosts: every host the DNS screen plays on is what detect
// establishes, every fact, and its hardn.yml reads as hardn reads it; the
// screen draws there, reading only, and shows what manages
// /etc/resolv.conf as pkg/host/dns reads it.
func TestDNSScreenHosts(t *testing.T) {
	want := map[string]string{
		"debian13": "direct (/etc/resolv.conf)", "ubuntu2604": "systemd-resolved", "alpine324": "direct (/etc/resolv.conf)",
		"pve9": "Proxmox VE (/etc/resolv.conf)", "networkmanager": "NetworkManager, reported only", "empty": "direct (/etc/resolv.conf)",
		"one": "direct (/etc/resolv.conf)", "configured": "direct (/etc/resolv.conf)", "failed-reload": "systemd-resolved",
		"failed-save": "direct (/etc/resolv.conf)",
	}
	if len(dnsScreenHosts()) != len(want) {
		t.Errorf("%d hosts, want %d", len(dnsScreenHosts()), len(want))
	}
	for _, dh := range dnsScreenHosts() {
		t.Run(dh.name, func(t *testing.T) {
			f, err := detect.Detect(context.Background(), dh.host().system())
			if err != nil || f.Platform.Err != nil || f.Init.Err != nil || f.Container.Detected != detect.No {
				t.Fatalf("detect: %+v, %v", f, err)
			}
			s, h, out, code := playDNS(t, dh, false, "0")
			if code != exitOK || len(s.engineRuns) != 0 || s.holds != 0 || !strings.Contains(out, "Info: "+dnsPrefix+"/"+dh.name) {
				t.Errorf("exit %d, %d engines, the hold taken %d times:\n%s", code, len(s.engineRuns), s.holds, out)
			}
			if !strings.Contains(flatText(out), "DNS Implementation "+want[dh.name]) {
				t.Errorf("the screen does not name %q:\n%s", want[dh.name], out)
			}
			if w := changedOutsideRun(h); len(w) > 0 {
				t.Errorf("drawing the screen changed %q", w)
			}
		})
	}
}

// TestPlayDNS plays each item of the DNS screen on its host and checks
// what the transcript shows, in order, the host it leaves, the DNS row
// there after (the control's Check against hardn.yml as the host then
// holds it), what changed on the host, call by call, and how often the
// gallery's hold was taken: once by each failure's rollback. A refusal
// before asking, a declined change and a dry run change nothing.
func TestPlayDNS(t *testing.T) {
	const (
		configured = "configured: 192.0.2.54, 2001:db8::54"
		notHardn   = "not-configured: not hardn.yml's"
		written    = "write-atomic "
		reload     = "systemctl try-reload-or-restart systemd-resolved.service"
		redrawn    = "DNS Configuration #" // the title bar, as flatText leaves it
	)
	dropInMade := []string{"mkdir " + dns.DropInDir, "chmod " + dns.DropInDir, written + dns.DropIn, reload}
	listed := func(list ...string) func(*testing.T, *fakeHost) {
		return func(t *testing.T, h *fakeHost) {
			t.Helper()
			if got := nameserversOf(t, h); !slices.Equal(got, list) {
				t.Errorf("hardn.yml lists %q, want %q", got, list)
			}
		}
	}
	for _, tc := range []struct {
		name, host string
		dryRun     bool
		lines      []string
		holds      int
		screen     []string // what the transcript shows, in order
		check      func(*testing.T, *fakeHost)
		line       string   // the DNS row after: its state and detail
		order      []string // what changed on the host, in order
	}{
		{"add", "debian13", false, []string{"2", "999.1.1.1", "192.0.2.54", "192.0.2.53", "y", "0"}, 0,
			[]string{"(x) Invalid IP address format", "(x) Nameserver 192.0.2.54 is already configured", "Add nameserver 192.0.2.53? [y/N, 0 or q]: y",
				"Operation Successful", redrawn, "Nameservers 192.0.2.54, 2001:db8::54, 192.0.2.53", "Nameserver 3 192.0.2.53"},
			listed("192.0.2.54", "2001:db8::54", "192.0.2.53"), "configured: 192.0.2.54, 2001:db8::54, 192.0.2.53", []string{written + dns.ResolvConf, written + hardnYML}},
		{"remove", "debian13", false, []string{"3", "2", "y", "0"}, 0,
			[]string{"Select a nameserver to remove", "Remove nameserver 2001:db8::54? [y/N, 0 or q]: y", "Operation Successful", redrawn, "Nameservers 192.0.2.54"},
			listed("192.0.2.54"), "configured: 192.0.2.54", []string{written + dns.ResolvConf, written + hardnYML}},
		{"presets", "debian13", false, []string{"4", "y", "5", "n", "6", "y", "0"}, 0,
			[]string{"Use Cloudflare DNS? [y/N, 0 or q]: y", "Operation Successful", "Nameservers 1.1.1.1, 1.0.0.1", "Use Google DNS? [y/N, 0 or q]: n",
				"Operation Cancelled", "Nameservers 1.1.1.1, 1.0.0.1", "Use Quad9 DNS? [y/N, 0 or q]: y", "Operation Successful", "Nameservers 9.9.9.9, 149.112.112.112"},
			listed("9.9.9.9", "149.112.112.112"), "configured: 9.9.9.9, 149.112.112.112",
			[]string{written + dns.ResolvConf, written + hardnYML, written + dns.ResolvConf, written + hardnYML}},
		{"configure", "ubuntu2604", false, []string{"1", "y", "0"}, 0,
			[]string{"eth0 still uses its own nameserver 192.0.2.1", "$ " + reload, "Configure DNS? [y/N, 0 or q]: y", "Operation Successful", redrawn, "Nameservers 192.0.2.54, 2001:db8::54, 192.0.2."},
			func(t *testing.T, h *fakeHost) {
				if got := h.content(dns.DropIn); got != dropInWritten || !slices.Equal(h.resolved.global, dnsList) {
					t.Errorf("drop-in %q, resolved on %q", got, h.resolved.global)
				}
			}, configured, dropInMade},
		{"udhcpc", "alpine324", false, []string{"1", "y", "y", "0"}, 0,
			[]string{"Stop udhcpc rewriting /etc/resolv.conf? [y/N, 0 or q]: y", "Set nameservers; stop udhcpc rewriting", "Configure DNS? [y/N, 0 or q]: y",
				"Operation Successful", redrawn, "Nameservers 192.0.2.54, 2001:db8::54"},
			func(t *testing.T, h *fakeHost) {
				if got := h.content(dns.UDHCPCConf); got != udhcpcStock+"# hardn: keep udhcpc from rewriting /etc/resolv.conf\nRESOLV_CONF=\"no\"\n" {
					t.Errorf("udhcpc.conf:\n%s", got)
				}
			}, configured, []string{written + dns.UDHCPCConf, written + dns.ResolvConf}},
		{"node", "pve9", false, []string{"1", "y", "0"}, 0,
			[]string{"Saving Proxmox VE's DNS settings rewrites /etc/resolv.conf", "Configure DNS? [y/N, 0 or q]: y", "Operation Successful", redrawn, "Nameservers 192.0.2.54, 2001:db8::54"},
			listed(dnsList...), configured, []string{written + dns.ResolvConf}},
		{"refused", "networkmanager", false, []string{"1", "2", "3", "4", "5", "6", "0"}, 0,
			[]string{"Warning NetworkManager manages DNS here. Set the nameservers with nmcli.", "Warning NetworkManager manages DNS here.", "Nothing was applied."},
			listed(dnsList...), "not-applicable: NetworkManager", nil},
		{"empty", "empty", false, []string{"3", "1", "0"}, 0,
			[]string{"No nameservers configured", "[0-2, 4-6 or q]: 3", "(x) Invalid option. Please try again.", "Warning Please add nameservers first.", "Nothing was applied."},
			listed(), "not-applicable: none configured", nil},
		{"one", "one", false, []string{"3", "0"}, 0,
			[]string{"Warning The last nameserver cannot be removed.", "Nothing was applied."}, listed("192.0.2.54"), notHardn, nil},
		{"nothing to change", "configured", false, []string{"1", "0"}, 0,
			[]string{"Nothing needs changing.", "Nothing was applied."}, listed(dnsList...), configured, nil},
		{"dry run", "debian13", true, []string{"4", "0"}, 0,
			[]string{"Save the configuration", "No actual changes were made to your system.", redrawn, "Nameservers 192.0.2.1", "Nothing was applied."},
			listed(dnsList...), notHardn, nil},
		// dns.Apply undoes the change itself; the engine's rollback then
		// has nothing left to undo, under the hold all the same.
		{"failed reload", "failed-reload", false, []string{"1", "y", "0"}, 1,
			[]string{"Set the nameservers Failed DNS_NOT_EFFECTIVE", "Reloading systemd-resolved had no effect", "Set the nameservers Rolled back",
				"Run resolvectl dns to see the servers in use.", redrawn, "Nameservers 192.0.2.1 (eth0)"},
			func(t *testing.T, h *fakeHost) {
				if h.content(dns.DropIn) != absent || h.resolved.global != nil {
					t.Errorf("drop-in %q, resolved on %q; want neither", h.content(dns.DropIn), h.resolved.global)
				}
			}, notHardn, append(slices.Clone(dropInMade), "remove "+dns.DropIn, "remove "+dns.DropInDir, "systemctl reload-or-restart systemd-resolved.service")},
		{"failed save", "failed-save", false, []string{"4", "y", "0"}, 1,
			[]string{"Save the configuration Failed PERMISSION_DENIED", "Saving /etc/hardn/hardn.yml failed", "Set the nameservers Rolled back", redrawn, "Nameservers 192.0.2.1"},
			func(t *testing.T, h *fakeHost) {
				listed(dnsList...)(t, h)
				if got := h.content(dns.ResolvConf); got != dhcpcdHead+dhcpLease {
					t.Errorf("resolv.conf:\n%s", got)
				}
			}, notHardn, []string{written + dns.ResolvConf, written + dns.ResolvConf}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, h, out, code := playDNS(t, dnsScreenHostNamed(t, tc.host), tc.dryRun, tc.lines...)
			if w := notShown(out, tc.screen); w != "" {
				t.Errorf("the transcript does not show %q after what came before it:\n%s", w, out)
			}
			// Each review lists its plan's findings once; the presets'
			// three reviews list dhcpcd's three times.
			if w := repeatedFinding(out); w != "" && tc.name != "presets" {
				t.Errorf("the transcript says %q again, or after the flow:\n%s", w, out)
			}
			if code != exitOK || s.holds != tc.holds {
				t.Errorf("exit %d, the gallery's hold taken %d times; want %d", code, s.holds, tc.holds)
			}
			tc.check(t, h)
			if got := dnsLine(t, h); got != tc.line {
				t.Errorf("the DNS row after: %q, want %q", got, tc.line)
			}
			if all := hostChanges(h); !slices.Equal(all, tc.order) {
				t.Errorf("the host changed %q, want %q", all, tc.order)
			}
		})
	}
}

// nameserversOf is the list hardn.yml holds on h now.
func nameserversOf(t *testing.T, h *fakeHost) []string {
	t.Helper()
	c, err := config.Read(context.Background(), h.f, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return c.Nameservers
}

// TestPlayDNSUsage checks that a DNS host that is not one, -fail, and a
// sequence other than normal or dry-run stop play with exit status 2
// before stdin is read; and that the main menu's 3 leads to the screen on
// Debian 13.
func TestPlayDNSUsage(t *testing.T) {
	for _, args := range [][]string{
		{dnsPrefix + "/nope"},
		{"-fail", "2", dnsPrefix + "/debian13"},
		{"-sequence", "typed", dnsPrefix},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(append([]string{"play"}, args...), func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("play %v: exit %d, stdout %q, stderr %q, stdin read %t; want exit %d, a message, no output", args, code, out.String(), errs.String(), read, exitUsage)
		}
	}
	if dnsLinks[mainMenuMock+"/3"] != dnsMainHost {
		t.Errorf("the main menu's 3 leads to %q", dnsLinks[mainMenuMock+"/3"])
	}
	code, out, errs, unread := playGallery([]string{"3", "0", "0"}, "-profile", "plain")
	if code != exitOK || errs != "" || unread != 0 || !strings.Contains(out, "Info: "+dnsMainHost) || !strings.Contains(out, "# DNS Configuration #") {
		t.Errorf("play, 3: exit %d, stderr %q, %d unread:\n%s", code, errs, unread, out)
	}
	code, out, _, unread = playGallery([]string{"7", "0", "0"}, "-profile", "plain", dnsPrefix)
	if code != exitOK || unread != 0 || !strings.Contains(out, "DNS hosts") || !strings.Contains(out, "Info: "+dnsPrefix+"/one") {
		t.Errorf("play dns, 7: exit %d, %d unread:\n%s", code, unread, out)
	}
}
