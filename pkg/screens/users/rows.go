package users

import (
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The rows' words, the control's types mapped to what pkg/ui draws: decided
// (gallery-users.md §6), Figma's where it has them, but "Empty" (control-
// users.md §5), proposed there.
const (
	keySudo = "Sudo" // Figma's; on doas, its name
	keyDoas = "doas"

	enabled       = "Enabled"
	enabledComma  = "Enabled,"
	disabled      = "Disabled"
	cantRead      = "Can't read"
	noPassword    = "No password"
	noPassToAsk   = "No password to ask for" // a cost, not a failure (decision 3)
	noPassSet     = "No password set"        // the rights wait for one (the maintainer, 2026-11-11)
	pwSet         = "Set"
	pwNone        = "None"
	pwLocked      = "Locked,"
	pwLockedNote  = "key login refused"
	pwEmpty       = "Empty"
	byHardn       = "hardn"
	byGroupPrefix = "group "
)

// statusRow is the User row of the reading, the main menu's: Check's,
// through the main menu's own mapping, with no engine.
func statusRow(r usersctl.Reading) component.StatusList {
	c := usersctl.Admin{}
	view := output.StatusViewOf([]engine.Report{{Control: c.ID(), Title: c.Title(), Status: r.Status, Err: r.StatusErr}}, risk.Level{}).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != c.Title() })
	return view
}

// toolWord is the tool the items name: doas where Choose picks it, else
// sudo.
func toolWord(r usersctl.Reading) string {
	if r.Choice.Tool == sudoers.ToolDoas {
		return "doas"
	}
	return "sudo"
}

// toolKey is the tool row's key: Figma's Sudo, doas where Choose picks it.
func toolKey(r usersctl.Reading) string {
	if r.Choice.Tool == sudoers.ToolDoas {
		return keyDoas
	}
	return keySudo
}

// noPasswordToAsk reports whether u has no password for a tool to ask: none
// set, or locked.
func noPasswordToAsk(u usersctl.User) bool {
	return u.Locked || u.Password == accounts.PasswordNone
}

// mainView is h as the users' screen draws it: a section per user, or why
// none is read.
func mainView(h reading) screen.Users {
	v := screen.Users{Status: h.status, Tool: toolWord(h.Reading)}
	for _, u := range h.Users {
		v.Users = append(v.Users, screen.UsersSection{Name: u.Name, Rows: userRows(h.Reading, u)})
	}
	if h.err != nil {
		v.NotRead = screenkit.RefusalWords(h.err)
	}
	return v
}

// userRows are u's rows: Figma's Sudo (doas on Alpine), who grants it, SSH
// Keys and the password.
func userRows(r usersctl.Reading, u usersctl.User) []component.KeyValue {
	tool := component.KeyValue{Key: toolKey(r), Value: disabled, State: component.ValueNone}
	switch u.Admin {
	case detect.Yes:
		tool = component.KeyValue{Key: toolKey(r), Value: enabled, State: component.ValueOK}
		switch {
		case u.NoPassword:
			tool = nopassRow(r, noPasswordToAsk(u))
		case noPasswordToAsk(u):
			tool = notSetRow(r)
		}
	case detect.Unknown:
		tool.Value, tool.State = cantRead, component.ValueUnknown
	}
	rows := []component.KeyValue{tool}
	if by := byWords(u.Decided); u.Admin == detect.Yes && by != "" {
		rows = append(rows, component.KeyValue{Key: screen.UsersByKey, Value: by})
	}
	return append(rows, screen.UsersKeysRow(len(u.Keys)), passwordRow(u))
}

// byWords is the rule that makes a user an admin as Granted by says it:
// hardn, "group sudo", or the file.
func byWords(r usersctl.Rule) string {
	switch r.Kind {
	case usersctl.RuleHardn:
		return byHardn
	case usersctl.RuleGroup:
		return byGroupPrefix + r.Group
	case usersctl.RuleFile:
		return r.File
	}
	return ""
}

// passwordRow is u's Password row, by its field in /etc/shadow.
func passwordRow(u usersctl.User) component.KeyValue {
	switch {
	case u.Locked:
		return component.KeyValue{Key: screen.UsersPasswordKey, Value: pwLocked, State: component.ValueBad, Note: pwLockedNote}
	case u.Password == accounts.PasswordSet:
		return component.KeyValue{Key: screen.UsersPasswordKey, Value: pwSet}
	case u.Password == accounts.PasswordNone:
		return component.KeyValue{Key: screen.UsersPasswordKey, Value: pwNone}
	case u.Password == accounts.PasswordEmpty:
		return component.KeyValue{Key: screen.UsersPasswordKey, Value: pwEmpty, State: component.ValueBad}
	}
	return component.KeyValue{Key: screen.UsersPasswordKey, Value: cantRead, State: component.ValueUnknown}
}

// nopassRow is the tool's row for a grant that asks no password: a failure
// where the account has a password to ask, a cost where it has none
// (decision 3).
func nopassRow(r usersctl.Reading, none bool) component.KeyValue {
	if none {
		return component.KeyValue{Key: toolKey(r), Value: enabledComma, State: component.ValueWarn, Note: noPassToAsk}
	}
	return component.KeyValue{Key: toolKey(r), Value: enabledComma, State: component.ValueBad, Note: noPassword}
}

// notSetRow is the tool's row for a grant that asks a password the user
// has not got yet: a cost, as the user can't use it until passwd sets one.
func notSetRow(r usersctl.Reading) component.KeyValue {
	return component.KeyValue{Key: toolKey(r), Value: enabledComma, State: component.ValueWarn, Note: noPassSet}
}

// enabledRow is the tool's row for a grant that asks the password.
func enabledRow(r usersctl.Reading) component.KeyValue {
	return component.KeyValue{Key: toolKey(r), Value: enabled, State: component.ValueOK}
}

// disabledRow is the tool's row with no admin rights, as a review draws it.
func disabledRow(r usersctl.Reading) component.KeyValue {
	return component.KeyValue{Key: toolKey(r), Value: disabled}
}

// keyRows are u's keys by fingerprint, each with its comment.
func keyRows(u usersctl.User) []component.KeyValue {
	var rows []component.KeyValue
	for _, k := range u.Keys {
		rows = append(rows, component.KeyValue{Key: k.Fingerprint, Value: k.Comment})
	}
	return rows
}

// risky is whether a plan's changes are lockout-risk.
func risky(plan []engine.Change) bool {
	return slices.ContainsFunc(plan, func(c engine.Change) bool { return c.Risk == engine.RiskLockout })
}
