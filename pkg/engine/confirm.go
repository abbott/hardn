package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
)

// errNoTrigger: the timer's trigger time was not in what systemctl printed.
var errNoTrigger = errors.New("no trigger time for the timer")

// confirm is how journal j, applied, is kept: the deadline from the
// timer's own trigger time where it can be read, else the journal's,
// approximate.
func (e *Engine) confirm(ctx context.Context, j revert.Journal) Confirm {
	c := Confirm{ID: j.ID, Deadline: j.Deadline(), Approximate: true, Via: j.ConfirmVia}
	if j.Timer == nil || j.Timer.Kind != revert.TimerSystemd {
		return c // the OpenRC sleeper's start is not read: approximate
	}
	t, err := timerTrigger(ctx, e.sys, j.Timer.Unit+".timer")
	switch {
	case err != nil:
		e.logf("change %s: the timer's trigger time was not read, so the deadline is approximate: %v", j.ID, err)
	case t.Before(j.Created) || t.After(j.Deadline().Add(time.Minute)):
		e.logf("change %s: the timer's trigger time %s is not near the deadline %s; the deadline is approximate", j.ID, t.Format(time.RFC3339), j.Deadline().Format(time.RFC3339))
	default:
		c.Deadline, c.Approximate = t, false
	}
	return c
}

// timerTrigger reads when systemd fires timer unit: list-timers' "next"
// column, in microseconds since the epoch in its JSON output (systemd
// v257: list-timers converts a monotonic trigger to the wall clock,
// calc_next_elapse, and table_print_json writes a timestamp cell as an
// unsigned number).
func timerTrigger(ctx context.Context, sys host.System, unit string) (time.Time, error) {
	res, err := sys.Run(ctx, "systemctl", "list-timers", "--all", "--output=json", "--no-pager", unit)
	if err != nil {
		return time.Time{}, err
	}
	var rows []struct {
		Next *uint64 `json:"next"`
		Unit string  `json:"unit"`
	}
	if err := json.Unmarshal(res.Stdout, &rows); err != nil {
		return time.Time{}, fmt.Errorf("systemctl list-timers: %w", err)
	}
	for _, r := range rows {
		if r.Unit == unit && r.Next != nil && *r.Next > 0 && *r.Next < 1<<62 {
			return time.UnixMicro(int64(*r.Next)).UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%s: %w", unit, errNoTrigger)
}
