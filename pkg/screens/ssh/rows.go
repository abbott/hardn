package ssh

import (
	"context"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	sshctl "github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The rows' words, the control's reading mapped to what pkg/ui draws:
// decided (gallery-ssh.md §6).
const (
	cantRead     = "Can't read"
	locked       = "Locked"
	lockedNote   = "root's account, no PAM"
	disallowed   = "Disallowed"
	allowed      = "Allowed"
	commandsOnly = "Commands only"
	keyOnly      = "Key only"
	off          = "Off"
	on           = "On"
	onKbd        = "On, keyboard-interactive"
	anyone       = "Anyone"
	letsMore     = "Lets more in"
	notKnown     = "Not known"
	byKey        = ", by key"
	byPassword   = ", by password"

	noteDefault = "sshd's default"
	noteBy      = "by "
	noteMethods = "by AuthenticationMethods"
	noteSocket  = "ssh.socket"
)

// statusRows are the SSH Login, SSH Auth and SSH Port rows, the main
// menu's: each check's, through the main menu's own mapping and its
// assessment, as the gallery drew them (an Unknown row's "Counted as not
// configured"), with no engine and no header probe: the zero header gives
// these rows no factor of its own.
func statusRows(ctx context.Context, sys host.System, f detect.Facts) component.StatusList {
	var reports []engine.Report
	var titles []string
	for _, c := range []engine.Control{sshctl.Login{}, sshctl.Auth{}, sshctl.Port{}} {
		r := engine.Report{Control: c.ID(), Title: c.Title()}
		r.Status, r.Err = c.Check(ctx, sys, f) // each applies everywhere
		if r.Err != nil {
			r.Status.State = engine.Unknown
		}
		reports, titles = append(reports, r), append(titles, c.Title())
	}
	view := output.StatusViewOf(reports, risk.Assess(reports, header.Header{})).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return !slices.Contains(titles, row.Label) })
	return view
}

// mainView is h as the screen draws it.
func mainView(h reading) screen.SSH {
	v := screen.SSH{Status: h.status, Rows: sectionRows(h), Ports: portWords(h), OldFiles: len(h.Found.Artifacts) > 0, NotRead: h.notRead()}
	switch {
	case h.PVE:
		v.Root = screen.SSHPVERoot
	case !v.NotRead && h.RootValue() == "no":
		v.Root = screen.SSHAllowRootKeyOnly
	}
	v.PasswordsOff = !v.NotRead && !h.PasswordsOn()
	v.Unfixed = h.Fix.Fixed == detect.No
	return v
}

// portWords are the ports sshd listens on, "22" or "22 and 2230"; "" where
// they are not read.
func portWords(h reading) string { return strings.Join(ports(h), " and ") }

// ports are the ports sshd listens on, in order.
func ports(h reading) []string {
	var ps []string
	for _, p := range h.Effective.Listeners.Ports() {
		ps = append(ps, strconv.Itoa(int(p)))
	}
	return ps
}

// byNote is the file that decides keyword k as a row's note: "" for
// hardn's own, sshd's default where no file sets it.
func byNote(h reading, k string) string {
	switch f := h.DecidedBy(k); f {
	case path.Base(sshd.HardnDropIn):
		return ""
	case "":
		return noteDefault
	default:
		return noteBy + f
	}
}

// passwordMethods are the password methods sshd offers: password where
// PasswordAuthentication is yes, keyboard-interactive where it is yes and
// sshd uses PAM.
func passwordMethods(h reading) map[string]bool {
	e := h.Effective
	return map[string]bool{"password": e.PasswordAuthentication.Value == "yes",
		"keyboard-interactive": e.KbdInteractiveAuthentication.Value == "yes" && e.UsePAM.Value == "yes"}
}

// sectionRows are the SSH Configuration section: what sshd runs with, from
// sshd -T and the files, never hardn.yml; Can't read for its three values
// where sshd -T is not read.
func sectionRows(h reading) []component.KeyValue {
	if h.notRead() {
		return []component.KeyValue{{Key: screen.SSHRootKey, Value: cantRead, State: component.ValueUnknown},
			{Key: screen.SSHPasswordKey, Value: cantRead, State: component.ValueUnknown},
			{Key: screen.SSHPortKey, Value: cantRead, State: component.ValueUnknown}}
	}
	rows := []component.KeyValue{rootRow(h), passwordRow(h), portRow(h)}
	if us := h.Effective.AllowUsers.Values; len(us) > 0 {
		rows = append(rows, component.KeyValue{Key: screen.SSHUsersKey, Value: strings.Join(us, " "), Note: byNote(h, "allowusers")})
	} else {
		rows = append(rows, component.KeyValue{Key: screen.SSHUsersKey, Value: anyone})
	}
	ms := slices.Concat(h.MatchMore("permitrootlogin", h.RootValue()), h.MatchMore("passwordauthentication", h.Effective.PasswordAuthentication.Value))
	if h.Effective.UsePAM.Value == "yes" {
		ms = append(ms, h.MatchMore("kbdinteractiveauthentication", h.Effective.KbdInteractiveAuthentication.Value)...)
	}
	if len(ms) > 0 {
		rows = append(rows, component.KeyValue{Key: screen.SSHMatchKey, Value: letsMore, State: component.ValueWarn, Note: path.Base(ms[0].File)})
	}
	if len(h.Found.Artifacts) > 0 {
		rows = append(rows, component.KeyValue{Key: screen.SSHOldKey, Value: oldNames(h), State: component.ValueWarn})
	}
	return append(rows, youRow(h))
}

// rootRow is Root SSH Login: Locked where no account logs in as root, else
// PermitRootLogin's value.
func rootRow(h reading) component.KeyValue {
	row := component.KeyValue{Key: screen.SSHRootKey, Value: keyOnly, Note: byNote(h, "permitrootlogin")}
	switch v := h.RootValue(); {
	case v != "no" && h.RootLocked:
		row.Value, row.Note = locked, lockedNote
	case v == "no":
		row.Value = disallowed
	case v == "yes":
		row.Value, row.State = allowed, component.ValueBad
	case v == "forced-commands-only":
		row.Value = commandsOnly
	}
	return row
}

// passwordRow is Password Login, read through AuthenticationMethods: Off,
// by it where sshd offers a password method no list is met by alone; On, or
// On, keyboard-interactive where that is the only way.
func passwordRow(h reading) component.KeyValue {
	pm := passwordMethods(h)
	switch {
	case !h.PasswordsOn() && sshd.MethodsMet(nil, pm): // offered, but no list is met by them alone
		// "in <file>" does not fit the row at 80 columns: the file is the
		// Old Settings row's, or 00-hardn.conf.
		return component.KeyValue{Key: screen.SSHPasswordKey, Value: off, Note: noteMethods}
	case !h.PasswordsOn():
		return component.KeyValue{Key: screen.SSHPasswordKey, Value: off, Note: byNote(h, "passwordauthentication")}
	case sshd.MethodsMet(h.Methods(), map[string]bool{"password": pm["password"]}) || !pm["keyboard-interactive"]:
		return component.KeyValue{Key: screen.SSHPasswordKey, Value: on, State: component.ValueWarn, Note: byNote(h, "passwordauthentication")}
	}
	return component.KeyValue{Key: screen.SSHPasswordKey, Value: onKbd, State: component.ValueWarn, Note: byNote(h, "kbdinteractiveauthentication")}
}

// portRow is SSH Port: the ports sshd listens on, ssh.socket's noted.
func portRow(h reading) component.KeyValue {
	row := component.KeyValue{Key: screen.SSHPortKey, Value: strings.Join(ports(h), ", ")}
	if row.Value == "" {
		row.Value, row.State = cantRead, component.ValueUnknown
	}
	if h.Facts.SSH.Mode == detect.SSHSocket {
		row.Note = noteSocket
	}
	return row
}

// youRow is Logged in as: the session's line in sshd's log, Not known
// where there is none (a console, or no line).
func youRow(h reading) component.KeyValue {
	switch {
	case h.Login.User == "":
		return component.KeyValue{Key: screen.SSHYouKey, Value: notKnown, State: component.ValueUnknown}
	case h.ByKey():
		return component.KeyValue{Key: screen.SSHYouKey, Value: h.Login.User + byKey}
	}
	return component.KeyValue{Key: screen.SSHYouKey, Value: h.Login.User + byPassword, State: component.ValueWarn}
}

// oldNames are hardn's old files' names.
func oldNames(h reading) string {
	var names []string
	for _, a := range h.Found.Artifacts {
		names = append(names, path.Base(a.Path))
	}
	return strings.Join(names, ", ")
}
