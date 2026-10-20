package engine

import "fmt"

// EventState is where a change, or the apply, stands in an Event.
type EventState uint8

// The states an Event reports.
const (
	// EventRunning: the change has started (its backups come first).
	EventRunning EventState = iota + 1
	// EventDone: its Apply returned nil and what it ran is journalled.
	EventDone
	// EventFailed: the change failed at Event.Step, or, with Index -1,
	// the apply did (a cancellation between changes, the window,
	// MarkApplied, Done).
	EventFailed
	// EventRolledBack: the rollback in this process finished, and with it
	// this change's undo. Sent for each started change, last first, once
	// revert.Rollback over the whole journal has returned: the journal is
	// rolled back as one, so a rollback that did not finish, or that
	// another process has, reports none.
	EventRolledBack
)

var eventWords = [...]string{"", "running", "done", "failed", "rolled-back"}

func (s EventState) String() string {
	if s >= EventRunning && int(s) < len(eventWords) {
		return eventWords[s]
	}
	return fmt.Sprintf("event(%d)", uint8(s))
}

// Event is one moment of an apply, as Options.Progress receives it: data,
// which the caller renders (pkg/ui/flow's Report takes a step's index and
// its state).
type Event struct {
	// Index is the change's place in Plan.Changes, the order Apply runs
	// them in; -1 for the apply as a whole.
	Index  int
	Change string // the change's ID; "" with Index -1
	State  EventState
	Step   string // with EventFailed: the step that failed (StepBoot, StepStart … StepDone)
	Detail string // with EventDone: Ran.Detail
	Err    error  // with EventFailed
}
