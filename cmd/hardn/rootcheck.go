package main

// The root check's words and the exit status of two flags in one run. Until
// next/remove-environment this file was environment.go, which also held
// -e, setup-sudo-env and the Environment screen's hook (removed: the
// maintainer, 2026-11-12).

import (
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
)

// runStatus is hardn's exit status as several flags run in one invocation
// (-c, -d, -w, -g, -p; -r with -p): the status so far (0 before the first)
// and the next flag's, each output.ExitCode's: a refusal or a failure (1)
// over a plan with changes to make (3), over 0. So that a later flag's
// status never hides an earlier refusal (cmd-engine.md §2.5). 130 never reaches it: an interrupted flag ends
// hardn at once (runControls).
func runStatus(first, second int) int {
	rank := func(code int) int {
		switch code {
		case output.ExitOK:
			return 0
		case output.ExitChanges:
			return 1
		}
		return 2 // a refusal or a failure
	}
	if rank(first) > rank(second) {
		return first
	}
	return second
}

// rootHint says how to run hardn as root on this host (revert.Hardn): with
// sudo, sudo -i to switch to root; with doas, or neither, su.
func rootHint() string {
	su := "su"
	if revertElevate() == "sudo" {
		su = "sudo -i"
	}
	return "Run `" + revert.Hardn(revertElevation()) + "` or switch to root `" + su + "`"
}

// sayNotRoot is the root check's refusal of a user other than root, and
// how to run hardn as root (rootHint), one report on stderr (say).
func sayNotRoot() {
	sayRun(component.Message{Kind: component.MessageBad, Text: "This script needs to be run as root."}, component.Message{Text: rootHint()})
}
