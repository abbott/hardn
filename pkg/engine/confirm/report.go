package confirm

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// Report is what the clean-up did and what is left of it, in words, a line
// each: what cmd/hardn shows after the engine's own report of the
// clean-up (pkg/output's), so that the person learns what was and was not
// cleaned up and how to finish it by hand. The words are provisional, as
// the Phase 2 pages' are.
type Report struct {
	Done   []string // what was cleaned up
	Left   []string // what was not
	Finish string   // how to finish it; "" when nothing is left
}

// Report is h's clean-up in words, o being Cleanup's outcome (the zero
// Outcome when it did not run): nothing when the change held no port; for
// a console, or a session that may not finish it, what stays open and how
// to finish it; once applied, what was dropped; otherwise what stays open,
// and how to finish it, with `hardn confirm` from a new connection or by
// hand. A rollback of the clean-up that did not finish is said to leave
// the state unknown (the engine's report says what to do first).
func (h Held) Report(o Outcome) Report {
	if !h.Holds() {
		return Report{}
	}
	var f detect.Facts
	if o.Plan != nil {
		f = o.Plan.Facts
	}
	w := words{h: h, f: f, o: o}
	switch {
	case h.Console:
		return Report{Left: []string{w.held() + " stays open: hardn drops it only from a new SSH connection to " + w.via() +
			", made from another machine, and a console shows nothing about SSH."},
			Finish: "Run `" + w.elevate() + " hardn confirm` from one to drop it."}
	case h.Refused != nil:
		return Report{Left: []string{fmt.Sprintf("Change %s (%s), the last one confirmed, held %s during its confirm window, and its clean-up may not have run; this session cannot finish it: %v.",
			h.Change.ID, h.Change.Summary, w.ports(), h.Refused)},
			Finish: "If `sshd -T` still shows " + w.ports() + ", run `" + w.elevate() + " hardn confirm` from a new SSH connection to " + w.via() + ", made from another machine."}
	case o.Done() && o.Result.Nothing:
		return Report{Done: []string{"Nothing was left to clean up after change " + h.Change.ID + ": sshd is no longer configured for " + w.ports() + "."}}
	case o.Done():
		r := Report{Done: []string{w.held() + " is dropped from " + sshd.HardnDropIn + ", and sshd, reloaded, no longer listens on it."}}
		if w.applied(ControlID + "/firewall") {
			r.Done = append(r.Done, "ufw's "+w.rules(true)+", which change "+h.Change.ID+" added, is deleted.")
		} else if kept := w.rules(false); kept != "" {
			var cmds []string
			for _, p := range w.notAdded() {
				cmds = append(cmds, "`"+w.elevate()+" ufw delete allow "+strconv.Itoa(int(p))+"/tcp`")
			}
			r.Done = append(r.Done, "ufw keeps any "+kept+": change "+h.Change.ID+" did not add it, so hardn leaves it ("+
				strings.Join(cmds, " and ")+" removes it, if nothing else needs it).")
		}
		return r
	case o.Result != nil && o.Result.Failed != nil && !errors.Is(o.Result.Failed, engine.ErrRolledBack):
		return Report{Left: []string{"The clean-up of change " + h.Change.ID + " did not finish, nor did its rollback here, so whether " + w.ports() +
			" is still open is not established: what to do first is above."},
			Finish: "Once that is done, run `" + w.elevate() + " hardn confirm` again from a new SSH connection to " + w.via() + ", made from another machine."}
	}
	left := w.held() + " is still open: " + w.lines() + " stays in " + sshd.HardnDropIn
	if r := w.rules(true); r != "" {
		left += ", and ufw keeps its " + r + ", which change " + h.Change.ID + " added"
	}
	hand := "delete " + w.lines() + " from " + sshd.HardnDropIn + ", check the configuration with `" + w.elevate() + " sshd -t`, then " + w.reload()
	for _, p := range w.added() {
		hand += ", and `" + w.elevate() + " ufw delete allow " + strconv.Itoa(int(p)) + "/tcp`"
	}
	return Report{Left: []string{left + "."},
		Finish: "To finish it, put right what stopped it (above), then run `" + w.elevate() + " hardn confirm` again from a new SSH connection to " +
			w.via() + ", made from another machine; or by hand: " + hand + "."}
}

// words are the parts of a Report's sentences.
type words struct {
	h Held
	f detect.Facts
	o Outcome
}

// applied reports whether the clean-up's change id was applied.
func (w words) applied(id string) bool {
	return w.o.Result != nil && slices.ContainsFunc(w.o.Result.Applied, func(a engine.Applied) bool { return a.Change == id })
}

// ports is the held ports in words: "port 22", "ports 22 and 2222".
func (w words) ports() string {
	if len(w.h.Cleanup.Drop) == 1 {
		return "port " + strconv.Itoa(int(w.h.Cleanup.Drop[0]))
	}
	return "ports " + joinPorts(w.h.Cleanup.Drop)
}

// held is "Port 22, which change <id> held during its confirm window".
func (w words) held() string {
	p := w.ports()
	return strings.ToUpper(p[:1]) + p[1:] + ", which change " + w.h.Change.ID + " held during its confirm window,"
}

// via is where a new connection confirms the change: "port 2230".
func (w words) via() string {
	ps := viaPorts(w.h.Change)
	switch len(ps) {
	case 0:
		return "the new port"
	case 1:
		return "port " + strconv.Itoa(int(ps[0]))
	}
	return "port " + strings.ReplaceAll(joinPorts(ps), " and ", " or ")
}

// lines is the held ports' lines in 00-hardn.conf: "the `Port 22` line".
func (w words) lines() string {
	var ls []string
	for _, p := range w.h.Cleanup.Drop {
		ls = append(ls, "`Port "+strconv.Itoa(int(p))+"`")
	}
	if len(ls) == 1 {
		return "the " + ls[0] + " line"
	}
	return "the " + strings.Join(ls, " and ") + " lines"
}

// added are the held ports whose ufw rule the change added (its journal's
// `ufw delete allow <port>/tcp`).
func (w words) added() []uint16 {
	var out []uint16
	for _, p := range w.h.Cleanup.Drop {
		del := []string{"ufw", "delete", "allow", strconv.Itoa(int(p)) + "/tcp"}
		if slices.ContainsFunc(w.h.Cleanup.Firewall, func(a []string) bool { return slices.Equal(a, del) }) {
			out = append(out, p)
		}
	}
	return out
}

// notAdded are the held ports whose ufw rule the change did not add, when
// it ran ufw commands at all (ufw was in use): none otherwise.
func (w words) notAdded() []uint16 {
	if len(w.h.Cleanup.Firewall) == 0 {
		return nil
	}
	var out []uint16
	for _, p := range w.h.Cleanup.Drop {
		if !slices.Contains(w.added(), p) {
			out = append(out, p)
		}
	}
	return out
}

// rules is the ufw rules of the held ports that the change added (added
// true), or of those it did not (false, notAdded): "rule for 22/tcp"; ""
// for none.
func (w words) rules(added bool) string {
	ps := w.notAdded()
	if added {
		ps = w.added()
	}
	var specs []string
	for _, p := range ps {
		specs = append(specs, strconv.Itoa(int(p))+"/tcp")
	}
	switch len(specs) {
	case 0:
		return ""
	case 1:
		return "rule for " + specs[0]
	}
	return "rules for " + strings.Join(specs, " and ")
}

// elevate is the host's elevation tool: doas where it is the one there
// (Alpine), sudo otherwise, as pkg/output words its commands.
func (w words) elevate() string {
	if w.f.Elevation.Sudo.Present == detect.No && w.f.Elevation.Doas.Present == detect.Yes {
		return "doas"
	}
	return "sudo"
}

// reload is the reload that makes sshd read 00-hardn.conf again, by hand,
// as reload.PlanFor plans it for how sshd is started (reload.md §2).
func (w words) reload() string {
	e := w.elevate()
	switch w.f.SSH.Mode {
	case detect.SSHService:
		return "`" + e + " systemctl reload ssh.service`"
	case detect.SSHSocket:
		return "`" + e + " systemctl daemon-reload` and `" + e + " systemctl restart ssh.socket`"
	case detect.SSHOpenRC:
		return "`" + e + " rc-service sshd reload`"
	}
	return "reload sshd as its init system does"
}

// joinPorts is ports in words: "22", "22 and 2222".
func joinPorts(ps []uint16) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = strconv.Itoa(int(p))
	}
	return strings.Join(s, " and ")
}
