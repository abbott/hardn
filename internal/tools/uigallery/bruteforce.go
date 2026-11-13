package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Brute-force limiting's mocks (docs/engine/gallery-bruteforce.md, adopted;
// docs/engine/screen-bruteforce.md): the two limits a host has with no new
// package, sshd's penalties as a row of the SSH Login screen and ufw's limit
// as an item of the Firewall screen. "uigallery bruteforce" lists them. Each
// but firewalld's is hardn's own screen (pkg/screens/ssh,
// pkg/screens/firewall) run on a simulated host, as play runs it, with
// nothing typed after what the mock types: the row and the item are what the
// screens read from the host, and the review's plan is the engine's.
// firewalld's is drawn from rows written here: the gallery simulates no
// firewalld, and the screen there has nothing new to show.

// bfPrefix names the listing and its goldens.
const bfPrefix = "bruteforce"

// bfLabel is the source label every mock's note opens with.
const bfLabel = "Proposed: no Figma frame."

// The SSH Login hosts of the mocks, play's "ssh/<name>" (sshVariants), and
// the Firewall hosts, play's "firewall/<name>".
const (
	bfSSHOn     = "debian13"
	bfSSHOff    = "limit-off"
	bfSSHOld    = "limit-unavailable"
	bfFWOffer   = "active22"
	bfFWLimited = "limited"
)

// bfLimitOff is debian13 with a drop-in turning sshd's penalties off.
func bfLimitOff() *fakeHost {
	return sshDebian13().sshFile(sshd.DropInDir+"/20-site.conf", "PerSourcePenalties no\n", 0o644)
}

// bfLimitOld is debian13 with an sshd before OpenSSH 9.8, as Debian 12's
// 9.2p1: sshd -T prints no persourcepenalties line.
func bfLimitOld() *fakeHost {
	h := sshDebian13()
	h.oldSSHD = true
	return h
}

// bfLimited is active22 with SSH's rule a limit: after Limit SSH, or where
// the administrator ran "ufw limit 22/tcp".
func bfLimited() *fakeHost {
	return fwWith(ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "limit", port: "22"}}})()
}

// bfRun runs open, one of hardn's screens, as play does, typing lines and
// nothing after them, and returns what it drew: the gallery's note left out,
// the end of the input no error.
func bfRun(c component.Ctx, lines string, open func(*session) error) (string, error) {
	var out bytes.Buffer
	s := &session{quiet: true, p: nav.Prompter{Ctx: c, In: prompt.New(strings.NewReader(lines), false), Out: &out}}
	if err := open(s); err != nil && !errors.Is(err, prompt.ErrAborted) && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(out.String(), " \n"), nil
}

// bfSSH draws the SSH Login screen on the host named name.
func bfSSH(name string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		sh, ok := findSSHHost(name)
		if !ok {
			return "", errors.New("no ssh host " + name)
		}
		return bfRun(c, "", func(s *session) error { return s.sshScreen(ctx, sh, sh.host()) })
	}
}

// bfFW draws the Firewall screen on the host named name with lines typed:
// the whole of it for none, and for item 5 what follows the screen and its
// "Checking the host" line: the review, from its title bar to its question.
func bfFW(name, lines string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		fh, ok := findFWHost(name)
		if !ok {
			return "", errors.New("no firewall host " + name)
		}
		run := func(lines string) (string, error) {
			return bfRun(c, lines, func(s *session) error { return s.fwScreen(ctx, fh, fh.host()) })
		}
		main, err := run("")
		if err != nil || lines == "" {
			return main, err
		}
		out, err := run(lines)
		if err != nil {
			return "", err
		}
		_, review, ok := strings.Cut(strings.TrimLeft(strings.TrimPrefix(out, main), " \n"), "\n\n")
		if !ok || !strings.HasPrefix(out, main) {
			return "", errors.New("item 5's review was not drawn")
		}
		return review, nil
	}
}

// bfFirewalld is firewalld's screen on Rocky Linux 10, firewalld running
// (pkg/screens/firewall's screen/rocky-running golden): nothing added.
func bfFirewalld() screen.Firewalld {
	ok := func(k, v string) component.KeyValue {
		return component.KeyValue{Key: k, Value: v, State: component.ValueOK}
	}
	return screen.Firewalld{
		Status: component.StatusList{Rows: []component.StatusRow{{Label: "Firewall", State: component.StatusConfigured, Detail: "firewalld running"}}},
		Rows: []component.KeyValue{ok(screen.FirewalldInstalledKey, "Yes"), ok(screen.FirewalldRunningKey, "Yes"), ok(screen.FirewallBootKey, "Yes"),
			{Key: screen.FirewalldDefaultKey, Value: "public"}, {Key: screen.FirewalldActiveKey, Value: "public"}},
		First: screen.FirewalldDisable,
	}
}

// bfMocks are the listing's mocks.
func bfMocks() []listMock {
	const deb = hostFQDN + " runs Debian 13 (OpenSSH 10.0p2). "
	return []listMock{
		{name: "ssh-on", note: deb + "No file sets PerSourcePenalties, so sshd's own limit is on: the row Brute-Force Limit, under Allowed Users. play ssh/" + bfSSHOn + ".",
			draw: bfSSH(bfSSHOn)},
		{name: "ssh-off", note: deb + "20-site.conf sets PerSourcePenalties no: the row is a warning and names the file. play ssh/" + bfSSHOff + ".",
			draw: bfSSH(bfSSHOff)},
		{name: "ssh-unavailable", note: hostFQDN + " runs an sshd before OpenSSH 9.8, as Debian 12's 9.2p1: sshd has no penalties, and the row says what they need. MaxAuthTries and MaxStartups are not named as a limit. play ssh/" + bfSSHOld + ".",
			draw: bfSSH(bfSSHOld)},
		{name: "firewall-offer", note: deb + "ufw is active and allows 22/tcp from anywhere: item 5, Limit SSH. The other items keep their keys. play firewall/" + bfFWOffer + ".",
			draw: bfFW(bfFWOffer, "")},
		{name: "firewall-review", note: "firewall-offer's item 5, up to its question: what the limit does, the rules as it leaves them, the engine's plan, y/N.",
			draw: bfFW(bfFWOffer, "5\n")},
		{name: "firewall-limited", note: deb + "After it, or where the administrator ran ufw limit 22/tcp: SSH reads Rate limited, and item 5 is gone. play firewall/" + bfFWLimited + ".",
			draw: bfFW(bfFWLimited, "")},
		{name: "firewalld", note: hostFQDN + " runs Rocky Linux 10, firewalld running. firewalld has no rule like ufw's limit, so the screen has no item and no line for one. The host's limit is sshd's, the SSH Login screen's row (ssh-on). Drawn from rows written in the gallery, which simulates no firewalld.",
			draw: func(_ context.Context, c component.Ctx) (string, error) {
				return bfFirewalld().SubMenu().Render(c), nil
			}},
	}
}

// bfUsage is the listing's usage line.
const bfUsage = "usage: go run ./internal/tools/uigallery bruteforce [flags] [mock | all]..."

// bfListing is the brute-force limiting listing.
var bfListing = listing{prefix: bfPrefix, usage: bfUsage, label: bfLabel, mocks: bfMocks}
