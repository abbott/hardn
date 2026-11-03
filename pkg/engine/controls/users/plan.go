package users

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// Action is what Admin's Plan does, for Admin.User.
type Action uint8

// The actions.
const (
	// ActionNone is the status list's (Controls): Plan is
	// engine.ErrNotMigrated with no change, as a check-only control's, so
	// that nothing that plans over the main menu's rows plans a lockout
	// change.
	ActionNone        Action = iota
	ActionGrant              // hardn's grant: the user's password asked where it has a usable one, none where not (Made here)
	ActionRemove             // hardn's grant removed, where no other rule grants (decision 2 (a)), and not the last way to root (decision 4)
	ActionAskPassword        // hardn's grant asks the user's password (decision 3 (a))
	ActionNoPassword         // hardn's grant asks no password
	ActionCreate             // a new user: engine.ErrNotMigrated until the next stage of unit (b)
	ActionAddKey             // an SSH key added: engine.ErrNotMigrated for now
	ActionRemoveKey          // an SSH key removed: engine.ErrNotMigrated for now
)

// ChangeGrant is the id of the change to hardn's grant, every action's.
const ChangeGrant = ID + "/grant"

// Errors of the refusals, testable with errors.Is, each the Err of a
// *steps.Refusal in the decided words (gallery-users.md §6), wrapping the
// primitive's error where there is one.
var (
	// ErrElevationNotRead: a tool's rules are not read (Elevation.NotRead).
	ErrElevationNotRead = errors.New("the elevation tool's rules are not read, so no grant is planned")
	// ErrNoTool: neither sudo nor doas is installed.
	ErrNoTool = errors.New("no elevation tool is installed, so no grant is planned")
	// ErrNotListed: the user is not one Read lists.
	ErrNotListed = errors.New("not a user the User Management screen lists")
	// ErrGrantEdited: hardn's file for the user holds what hardn did not write.
	ErrGrantEdited = errors.New("hardn's file for the user holds a rule hardn did not write")
	// ErrNotAdmin: a password choice for a user who is not an admin.
	ErrNotAdmin = errors.New("the user is not an admin")
	// ErrGroupGrant: a group's rule makes the user an admin, and hardn has no grant.
	ErrGroupGrant = errors.New("the user is an admin through a group's rule, which hardn leaves alone")
	// ErrNotHardns: a file's rule makes the user an admin, and hardn has no grant.
	ErrNotHardns = errors.New("the user is an admin through a rule hardn did not write")
	// ErrStillUnknown: whether another rule grants, once hardn's is gone, is not read.
	ErrStillUnknown = errors.New("whether the user stays an admin without hardn's grant is not established")
	// ErrStillAdmin: another rule would grant, once hardn's is gone.
	ErrStillAdmin = errors.New("the user stays an admin without hardn's grant")
	// ErrSessionUser: the grant is the session user's own.
	ErrSessionUser = errors.New("the grant is the session user's own way to become root")
	// ErrLastAdmin: the session user is not known, and the user is the last admin.
	ErrLastAdmin = errors.New("the session user is not known, and the user is the last admin")
	// ErrNoPasswordToAsk: the user has no usable password for the tool to ask.
	ErrNoPasswordToAsk = errors.New("the user has no password the elevation tool could ask")
)

// The refusals' codes, the registry's for the errors they wrap (pkg/output;
// pinned by the tests).
const (
	codeElevationNotRead = "USERS_ELEVATION_NOT_READ"
	codeNoTool           = "USERS_NO_TOOL"
	codeNotListed        = "USERS_NOT_LISTED"
	codeGrantEdited      = "USERS_GRANT_EDITED"
	codeNotAdmin         = "USERS_NOT_ADMIN"
	codeGroupGrant       = "USERS_GROUP_GRANT"
	codeNotHardns        = "USERS_GRANT_NOT_HARDNS"
	codeStillUnknown     = "USERS_STILL_UNKNOWN"
	codeStillAdmin       = "USERS_STILL_ADMIN"
	codeSessionUser      = "USERS_SESSION_USER"
	codeLastAdmin        = "USERS_LAST_ADMIN"
	codeNoPasswordToAsk  = "USERS_NO_PASSWORD_TO_ASK"
)

// refuse is a refusal: its code, its sentinel wrapping cause where there is
// one, and its words, a Why and a Fix that screenkit.RefusalWords joins into
// the decided sentences.
func refuse(code string, sentinel, cause error, why, fix string) error {
	err := sentinel
	if cause != nil {
		err = fmt.Errorf("%w: %w", sentinel, cause)
	}
	return &steps.Refusal{Code: code, Why: why, Fix: fix, Err: err}
}

// Plan plans Action for User on the host in.Facts describe, reading it as
// Read does (without Check's row), and refuses, before anything is written,
// in the order of the mock (gallery-users.md §5): the tool's (toolRefusal);
// a user Read does not list; hardn's file edited; then the action's own.
// Nothing to do is no change. ActionNone, ActionCreate, ActionAddKey and
// ActionRemoveKey are engine.ErrNotMigrated, reading nothing.
func (a Admin) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	switch a.Action {
	case ActionNone, ActionCreate, ActionAddKey, ActionRemoveKey:
		return nil, engine.ErrNotMigrated
	case ActionGrant, ActionRemove, ActionAskPassword, ActionNoPassword:
	default:
		return nil, fmt.Errorf("users: action %d: %w", a.Action, fs.ErrInvalid)
	}
	r, err := read(ctx, sys, in.Facts, a.Getenv, false)
	if err != nil {
		return nil, err
	}
	if err := r.toolRefusal(); err != nil {
		return nil, err
	}
	u, ok := r.user(a.User)
	switch {
	case !ok:
		return nil, refuse(codeNotListed, ErrNotListed, nil, a.User+" isn't a user hardn manages here", "choose one of the users listed")
	case errors.Is(u.PolicyErr, sudoers.ErrEdited):
		n, _ := sudoers.FileNames(r.Choice.Tool, u.Name)
		return nil, refuse(codeGrantEdited, ErrGrantEdited, u.PolicyErr, n.Grant+" holds a rule hardn didn't write", "change it there, then try again")
	case u.PolicyErr != nil:
		return nil, u.PolicyErr
	}
	tool := r.toolWord()
	var c *engine.Change
	switch a.Action {
	case ActionGrant:
		if u.Policy.Grant != sudoers.GrantNone {
			return nil, nil
		}
		g := sudoers.GrantNoPassword
		if u.Password == accounts.PasswordSet {
			g = sudoers.GrantPassword
		}
		if c, err = grantChange(ctx, sys, r, u.Name, g, "Let "+u.Name+" use "+tool); c != nil {
			c.Risk = engine.RiskPlain // a grant added takes no way to root away
		}
	case ActionRemove:
		if err := r.removeRefusal(ctx, sys, u); err != nil || u.Policy.Grant == sudoers.GrantNone {
			return nil, err
		}
		c, err = grantChange(ctx, sys, r, u.Name, sudoers.GrantNone, "Remove "+u.Name+"'s "+tool)
	case ActionAskPassword:
		switch {
		case u.Admin != detect.Yes:
			return nil, notAdmin(u.Name)
		case u.Password != accounts.PasswordSet:
			return nil, refuse(codeNoPasswordToAsk, ErrNoPasswordToAsk, nil,
				fmt.Sprintf("%s has no password, so %s couldn't ask for one", u.Name, tool), "set one with passwd "+u.Name+", then try again")
		case u.Policy.Grant == sudoers.GrantPassword:
			return nil, nil
		}
		c, err = grantChange(ctx, sys, r, u.Name, sudoers.GrantPassword, "Ask "+u.Name+"'s password")
	case ActionNoPassword:
		switch {
		case u.Admin != detect.Yes:
			return nil, notAdmin(u.Name)
		case u.Policy.Grant == sudoers.GrantNoPassword:
			return nil, nil
		}
		c, err = grantChange(ctx, sys, r, u.Name, sudoers.GrantNoPassword, "Stop asking "+u.Name+"'s password")
	}
	if err != nil || c == nil {
		return nil, err
	}
	return []engine.Change{*c}, nil
}

// notAdmin is the refusal of a password choice for a user who is not an
// admin (proposed words).
func notAdmin(name string) error {
	return refuse(codeNotAdmin, ErrNotAdmin, nil, name+" isn't an admin", "grant admin rights first")
}

// toolRefusal is why no grant can be planned on the host, before anything
// is asked: a tool's rules not read (Elevation.NotRead), no tool
// (sudoers.ErrNoTool), or the tool not established (Choose's error, as it
// is); nil when one can.
func (r Reading) toolRefusal() error {
	if len(r.NotRead) > 0 {
		nr := r.NotRead[0]
		check := "visudo -c"
		if nr.Tool == "doas" {
			check = "doas -C"
		}
		why := "its files don't check"
		if nr.Err != nil && !errors.Is(nr.Err, sudoers.ErrRejected) {
			why = nr.Err.Error()
		}
		return refuse(codeElevationNotRead, ErrElevationNotRead, nr.Err, "hardn can't read "+nr.Tool+"'s rules: "+why, "check them with "+check+", then try again")
	}
	switch {
	case errors.Is(r.Choice.Err, sudoers.ErrNoTool):
		return refuse(codeNoTool, ErrNoTool, r.Choice.Err, "sudo isn't installed, so hardn can't grant admin rights", "install sudo, then try again")
	case r.Choice.Err != nil:
		return r.Choice.Err
	}
	return nil
}

// removeRefusal is why Remove refuses for u, in the mock's order
// (removeRefusal, decisions 2 (a) and 4): hardn has no grant and another
// rule makes u an admin (a group's, with gpasswd -d's words; a file's);
// another rule would still grant once hardn's is gone, or what of them is
// not read (StillAdmin); the session user's own; with the session user not
// known, the last admin's. nil with nothing to remove too.
func (r Reading) removeRefusal(ctx context.Context, sys host.System, u User) error {
	if u.Policy.Grant == sudoers.GrantNone {
		by := u.Decided
		switch {
		case u.Admin == detect.No:
			return nil
		case u.Admin != detect.Yes:
			by = Rule{}
		case by.Kind == RuleGroup:
			return refuse(codeGroupGrant, ErrGroupGrant, nil, fmt.Sprintf("%s is an admin through the %s group, which hardn leaves alone", u.Name, by.Group),
				fmt.Sprintf("take %s out of it with gpasswd -d %s %s", u.Name, u.Name, by.Group))
		}
		return refuse(codeNotHardns, ErrNotHardns, nil, fmt.Sprintf("%s is an admin through %s, which hardn didn't write, so hardn can't remove it", u.Name, byWords(by)), "change it there")
	}
	still := StillAdmin(ctx, sys, r, u.Name)
	switch {
	case still.Unknown != "":
		return refuse(codeStillUnknown, ErrStillUnknown, nil, fmt.Sprintf("hardn can't tell whether %s would stay an admin: it can't read %s", u.Name, still.Unknown), "check it, then try again")
	case len(still.By) > 0:
		var words []string
		for _, by := range still.By {
			words = append(words, byWords(by))
		}
		all := words[len(words)-1]
		if len(words) > 1 {
			all = strings.Join(words[:len(words)-1], ", ") + " and " + all
		}
		return refuse(codeStillAdmin, ErrStillAdmin, nil, fmt.Sprintf("%s would stay an admin through %s, which hardn didn't write", u.Name, all), "change it there, then try again")
	case u.Name == r.Invoker:
		return refuse(codeSessionUser, ErrSessionUser, nil, fmt.Sprintf("You're running hardn through %s's %s. Removing it would leave no way to become root", u.Name, r.toolWord()), "grant another user first")
	case r.Invoker == "" && !slices.ContainsFunc(r.Users, func(x User) bool { return x.Name != u.Name && x.Admin == detect.Yes }):
		return refuse(codeLastAdmin, ErrLastAdmin, nil, fmt.Sprintf("hardn can't tell who you're logged in as, and %s is the last admin", u.Name), "grant another user first")
	}
	return nil
}

// byWords is a rule's source as a sentence names it: a group's, a file, or
// one not read.
func byWords(r Rule) string {
	switch r.Kind {
	case RuleGroup:
		return "the " + r.Group + " group"
	case RuleNone:
		return "a rule hardn can't read"
	}
	return r.File
}

// grantChange is user's grant set to g through the real planner: the files
// Read, hardn's Policy read back and its variables carried, PlanFor and
// steps.Sudoers (sudoers.md §6, slice 5), the plan's findings on the change.
// nil when nothing changes.
func grantChange(ctx context.Context, sys host.System, r Reading, user string, g sudoers.Grant, summary string) (*engine.Change, error) {
	st, err := sudoers.Read(ctx, sys, r.Choice, []string{user})
	if err != nil {
		return nil, err
	}
	pol, err := sudoers.PolicyOf(st, user)
	if err != nil {
		return nil, err
	}
	pol.Grant = g
	p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil || len(p.Changes) == 0 {
		return nil, err
	}
	c, err := steps.Sudoers(ChangeGrant, p, summary)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
