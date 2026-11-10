package confirm

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
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
	Finish string   // how to finish it; "" when nothing is left to do
}

// Report is h's clean-up in words, o being Cleanup's outcome (the zero
// Outcome when it did not run): nothing when the change held no port; for
// a console, or a session that may not finish it, what stays open and how
// to finish it; once applied, what was dropped; otherwise what is left,
// and how to finish it, with `hardn confirm` from a new connection or by
// hand (by hand alone when Carried: no later `hardn confirm` finds it). It
// states only what is established: a port that stays when this run changed
// nothing or rolled back, or that 00-hardn.conf was read to still have;
// after another hardn run changed the host, or a rollback that did not
// finish, whether the port is open is said not to be established. A
// refusal that forbids dropping the port (Proxmox VE's port 22, sshd left
// no port, the session's own port) gives no way to drop it.
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
	case h.Console || h.Refused != nil:
		return w.notRun()
	case o.Done() && o.Result.Nothing:
		return Report{Done: []string{"Nothing was left to clean up after change " + h.Change.ID + ": sshd is no longer configured for " + w.ports() + "."}}
	case o.Done():
		return w.done()
	case o.Result != nil && o.Result.Failed != nil && !errors.Is(o.Result.Failed, engine.ErrRolledBack):
		return w.finish(Report{Left: []string{"The clean-up of change " + h.Change.ID + " did not finish, nor did its rollback here, so whether " + w.ports() +
			" " + w.is() + " still open is not established: what to do first is above."}}, "Once that is done, "+w.again()+".")
	case errors.Is(o.Err, engine.ErrPlanChanged) || errors.Is(o.Err, host.ErrLocked) || errors.Is(o.Err, revert.ErrPending):
		return w.finish(Report{Left: []string{"Another hardn run changed the host, held its lock or left a change journalled (above), and may have dropped " +
			w.ports() + ", which change " + h.Change.ID + " held during its confirm window: whether " + w.it(true) + " " + w.is() + " still open is not established."}},
			"If `"+w.root("sshd -T")+"` still shows "+w.ports()+", "+w.again()+".")
	}
	if why, elsewhere := stays(o.Err); why != "" {
		r := Report{Left: []string{w.held() + " " + w.stays() + " open: " + why + "."}}
		switch {
		case elsewhere && h.Carried:
			return w.finish(r, "To drop "+w.it(false)+", from a new SSH connection to "+w.via()+", made from another machine, by hand: "+w.hand()+".")
		case elsewhere:
			return w.finish(r, "Run `"+w.hardn()+" confirm` again from a new SSH connection to "+w.via()+", made from another machine.")
		}
		return r
	}
	left := w.held() + " " + w.is() + " still open: " + w.lines() + " " + w.stays() + " in " + sshd.HardnDropIn
	if r := w.rules(true); r != "" {
		left += ", and ufw keeps its " + r + ", which hardn added"
	}
	return w.finish(Report{Left: []string{left + "."}}, "To finish the clean-up, put right what stopped it (above), then "+w.again()+".")
}

// stays is why the held ports stay, for a refusal that forbids dropping
// them, and whether they may be dropped from another session (elsewhere);
// "" for any other error.
func stays(err error) (why string, elsewhere bool) {
	switch {
	case errors.Is(err, pve.ErrPortChange):
		return "on Proxmox VE the SSH port stays 22 (above), so hardn does not drop port 22", false
	case errors.Is(err, reload.ErrDropsListener):
		return "sshd is configured for no other port now (above), and hardn does not leave sshd no port", false
	case errors.Is(err, steps.ErrSessionNotKept):
		return "this session is not connected to a port the clean-up keeps (above), and with no revert timer hardn does not drop the port its own session came in on", true
	}
	return "", false
}

// notRun is the report of a clean-up this session does not run: a console,
// or a session that could not have confirmed the change (Refused).
func (w words) notRun() Report {
	h := w.h
	switch {
	case h.Carried && h.Console:
		return w.finish(Report{Left: []string{w.held() + " " + w.stays() + " open: " + w.lines() + " " + w.stays() + " in " + sshd.HardnDropIn +
			"; a console shows nothing about SSH, so " + w.it(true) + " " + w.is() + " not dropped from here, and " + w.lost() + "."}},
			"To drop "+w.it(false)+", from a new SSH connection to "+w.via()+", made from another machine, by hand: "+w.hand()+".")
	case h.Carried:
		return w.finish(Report{Left: []string{fmt.Sprintf("%s %s open: %s %s in %s; this session cannot drop %s (%v), and %s.",
			w.held(), w.stays(), w.lines(), w.stays(), sshd.HardnDropIn, w.it(false), h.Refused, w.lost())}},
			"To drop "+w.it(false)+", from a new SSH connection to "+w.via()+", made from another machine, by hand: "+w.hand()+".")
	case h.Console:
		return w.finish(Report{Left: []string{w.held() + " " + w.stays() + " open: hardn drops " + w.it(false) + " only from a new SSH connection to " + w.via() +
			", made from another machine, and a console shows nothing about SSH."}},
			"Run `"+w.hardn()+" confirm` from one to drop "+w.it(false)+".")
	}
	return w.finish(Report{Left: []string{fmt.Sprintf("%s %s open: %s %s in %s; this session cannot finish its clean-up: %v.",
		w.held(), w.stays(), w.lines(), w.stays(), sshd.HardnDropIn, h.Refused)}},
		"Run `"+w.hardn()+" confirm` from a new SSH connection to "+w.via()+", made from another machine.")
}

// done is the report of a clean-up applied: the ports dropped, and ufw's
// rules for them deleted, or kept where hardn did not add them.
func (w words) done() Report {
	r := Report{Done: []string{w.held() + " " + w.is() + " dropped from " + sshd.HardnDropIn + ", and sshd, reloaded, no longer listens on " + w.it(false) + "."}}
	if w.applied(ControlID + "/firewall") {
		is := "is"
		if len(w.added()) > 1 {
			is = "are"
		}
		r.Done = append(r.Done, "ufw's "+w.rules(true)+", which hardn added, "+is+" deleted.")
	} else if kept := w.rules(false); kept != "" {
		var cmds []string
		for _, p := range w.notAdded() {
			cmds = append(cmds, "`"+w.root("ufw delete allow "+strconv.Itoa(int(p))+"/tcp")+"`")
		}
		it, removes := "it", "removes it"
		if len(cmds) > 1 {
			it, removes = "them", "remove them"
		}
		r.Done = append(r.Done, "ufw keeps any "+kept+": hardn did not add "+it+", so it leaves "+it+" ("+
			strings.Join(cmds, " and ")+" "+removes+", if nothing else needs "+it+").")
	}
	return w.finish(r, "")
}

// words are the parts of a Report's sentences.
type words struct {
	h Held
	f detect.Facts
	o Outcome
}

// finish is r with how to finish it, and, when it names sudo without the
// host's elevation tools known (no plan was made), that doas takes its
// place where sudo is not installed.
func (w words) finish(r Report, finish string) Report {
	r.Finish = finish
	if w.f.Elevation.Sudo.Present == detect.Unknown && strings.Contains(r.Finish, "`sudo ") {
		r.Finish += " Where sudo is not installed (Alpine), use doas."
	}
	return r
}

// again is how to finish the clean-up from here: `hardn confirm` again from
// a new SSH connection to the port kept, which finds it in confirmed.json,
// or by hand; by hand alone when Carried, since confirmed.json records the
// change confirmed now.
func (w words) again() string {
	if w.h.Carried {
		return "drop " + w.it(false) + " by hand, from a new SSH connection to " + w.via() + ", made from another machine (" + w.lost() + "): " + w.hand()
	}
	return "run `" + w.hardn() + " confirm` again from a new SSH connection to " + w.via() + ", made from another machine; or by hand: " + w.hand()
}

// hand is the clean-up by hand: the Port lines deleted, the configuration
// checked, the reload as sshd is started here, and ufw's rules that hardn
// added deleted.
func (w words) hand() string {
	s := "delete " + w.lines() + " from " + sshd.HardnDropIn + ", check the configuration with `" + w.root("sshd -t") + "`, then " + w.reload()
	for _, p := range w.added() {
		s += ", and `" + w.root("ufw delete allow "+strconv.Itoa(int(p))+"/tcp") + "`"
	}
	return s
}

// lost says that no later `hardn confirm` finds a Carried clean-up.
func (w words) lost() string {
	return "now that change " + w.h.Confirmed.Journal.ID + " is the last one confirmed, `hardn confirm` no longer finds " + w.it(false)
}

// it is the held ports as a pronoun: "it" or "they" (subject), "it" or
// "them" (object).
func (w words) it(subject bool) string {
	switch {
	case len(w.h.Cleanup.Drop) == 1:
		return "it"
	case subject:
		return "they"
	}
	return "them"
}

// is and stays agree with the held ports: "is", "are"; "stays", "stay".
func (w words) is() string {
	if len(w.h.Cleanup.Drop) == 1 {
		return "is"
	}
	return "are"
}

func (w words) stays() string {
	if len(w.h.Cleanup.Drop) == 1 {
		return "stays"
	}
	return "stay"
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

// held is "Port 22, which change <id> held during its confirm window,",
// naming the change as the last one confirmed (Finishing) or the one
// confirmed before this one (Carried).
func (w words) held() string {
	p := w.ports()
	s := strings.ToUpper(p[:1]) + p[1:] + ", which change " + w.h.Change.ID
	switch {
	case w.h.Carried:
		s += ", confirmed before change " + w.h.Confirmed.Journal.ID + ","
	case w.h.Finishing:
		s += ", the last one confirmed,"
	}
	return s + " held during its confirm window,"
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

// added are the held ports whose ufw rule hardn added (a journal's `ufw
// delete allow <port>/tcp`: the change's, or, carried, the one before it).
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

// rules is the ufw rules of the held ports that hardn added (added true),
// or of those it did not (false, notAdded): "rule for 22/tcp"; "" for none.
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

// hardn is how to run hardn as root here, as pkg/output words its
// commands (revert.Hardn): "doas hardn" where doas is the tool there
// (Alpine), "hardn" where neither sudo nor doas is installed (Proxmox VE as
// installed), "sudo hardn" otherwise, and where no plan established the
// tools (finish adds that doas takes sudo's place where it is not
// installed).
func (w words) hardn() string { return revert.Hardn(w.f.Elevation) }

// root is cmd run as root here: after the host's elevation tool, as hardn,
// or bare where neither sudo nor doas is installed (revert.Elevate).
func (w words) root(cmd string) string {
	if e := revert.Elevate(w.f.Elevation); e != revert.NoElevation {
		return e + " " + cmd
	}
	return cmd
}

// reload is the reload that makes sshd read 00-hardn.conf again, by hand,
// as reload.PlanFor plans it for how sshd is started (reload.md §2).
func (w words) reload() string {
	switch w.f.SSH.Mode {
	case detect.SSHService:
		return "`" + w.root("systemctl reload "+w.f.SSH.ServiceUnit()) + "`"
	case detect.SSHSocket:
		return "`" + w.root("systemctl daemon-reload") + "` and `" + w.root("systemctl restart ssh.socket") + "`"
	case detect.SSHOpenRC:
		return "`" + w.root("rc-service sshd reload") + "`"
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
