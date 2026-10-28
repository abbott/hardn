package host

import (
	"sync"
	"sync/atomic"
)

// ownSessions counts the OwnSessions not yet released.
var ownSessions atomic.Int32

// OwnSessions has Real start every command it runs (Run, RunInput) from
// now until release in a session of its own, as StartDetached does
// (setsid(2), through os/exec's SysProcAttr.Setsid): with no controlling
// terminal and outside hardn's process group, so that the hangup and the
// Ctrl-C that the terminal, a shell or sudo send to hardn's process group
// reach neither the command nor what it starts. It is for the engine's
// hold around a rollback (pkg/engine/hold), which catches the hangup in
// hardn itself: a command inherits an ignored signal, never a caught one
// (POSIX exec), so the hold's catch alone would leave the rollback's
// commands to die of the hangup. Run's other rules are unchanged; a
// cancelled context still kills only the command Run started. Calls nest:
// the sessions end with the last release, and a release counts once.
func OwnSessions() (release func()) {
	ownSessions.Add(1)
	var once sync.Once
	return func() { once.Do(func() { ownSessions.Add(-1) }) }
}

// inOwnSession reports whether a command started now gets a session of
// its own (OwnSessions).
func inOwnSession() bool { return ownSessions.Load() > 0 }
