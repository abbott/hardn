package sudoers

import (
	"fmt"
	"io/fs"
)

// Invoker is the user who ran hardn through tool t, from hardn's
// environment, which getenv looks up (os.Getenv in hardn; a map in the
// tests and the gallery): SUDO_USER under sudo and sudo-rs, DOAS_USER
// under doas, the variable each sets to the invoking user's name (sudo(8),
// ENVIRONMENT; sudo-rs v0.2.13 src/sudo/env/environment.rs, add_extra_env;
// OpenDoas v6.8.2 env.c, createenv). "" when it is unset or "root": hardn
// was not run through the tool by a user (a root login), and USER, which
// origin/main fell back to, is root's own there (gallery-environment.md
// §7 item 5; decision 2 (a)). A name CheckUser refuses is ErrUserName, as
// hardn writes the user's name into a rule; a tool that is not sudo or
// doas is fs.ErrInvalid. It changes nothing.
func Invoker(t Tool, getenv func(string) string) (string, error) {
	var name string
	switch t {
	case ToolSudo:
		name = getenv("SUDO_USER")
	case ToolDoas:
		name = getenv("DOAS_USER")
	default:
		return "", fmt.Errorf("sudoers: tool %s: %w", t, fs.ErrInvalid)
	}
	if name == "" || name == "root" {
		return "", nil
	}
	if err := CheckUser(t, name); err != nil {
		return "", err
	}
	return name, nil
}
