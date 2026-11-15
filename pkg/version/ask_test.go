package version

import (
	"context"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
)

const askPath = revert.DefaultDir + "/" + AskName

// asker is an Asker on an empty host, with the clock it reads.
func asker() (*Asker, *hostfake.System, *time.Time) {
	f := hostfake.New(host.Options{})
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	return &Asker{System: f, Now: func() time.Time { return now }}, f, &now
}

// TestAskDue: never asked, the question is due, and due again at the next
// open with no answer recorded (Ctrl-C).
// After a no: not due for 14 opens, due at the 15th; due once 30 days
// have passed, and not a minute before. After a yes: never.
func TestAskDue(t *testing.T) {
	ctx := context.Background()
	a, f, now := asker()
	if !a.Due(ctx) || a.Yes(ctx) {
		t.Error("never asked: not due, or a yes")
	}
	if _, ok := f.Entry(askPath); !ok || !a.Due(ctx) || a.Yes(ctx) {
		t.Error("with no answer recorded: no state kept, not due again, or a yes")
	}
	for _, answer := range []AskAnswer{AskNo, AskNone} {
		if err := a.Record(ctx, answer); err != nil {
			t.Fatal(err)
		}
		for i := 1; i < AskOpens; i++ {
			if a.Due(ctx) {
				t.Fatalf("%s: due at open %d", answer, i)
			}
		}
		first := a.Due(ctx)
		if again := a.Due(ctx); !first || !again {
			t.Errorf("%s: due at open %d: %t, and at the one after it: %t", answer, AskOpens, first, again)
		}
	}

	_ = a.Record(ctx, AskNo)
	*now = now.Add(AskEvery - time.Minute)
	if a.Due(ctx) {
		t.Error("due a minute before 30 days")
	}
	*now = now.Add(time.Minute)
	if !a.Due(ctx) {
		t.Error("not due after 30 days")
	}

	_ = a.Record(ctx, AskYes)
	*now = now.Add(10 * AskEvery)
	for i := 0; i < 2*AskOpens; i++ {
		if a.Due(ctx) {
			t.Fatal("due after a yes")
		}
	}
	if !a.Yes(ctx) {
		t.Error("the yes is not kept")
	}
}

// TestAskClockBackwards: a time asked that is after now counts from now:
// the question is not due at once, is due 30 days from now, and at the
// 15th open whatever the clock says.
func TestAskClockBackwards(t *testing.T) {
	ctx := context.Background()
	a, _, now := asker()
	_ = a.Record(ctx, AskNo)
	*now = now.Add(-365 * 24 * time.Hour)
	if a.Due(ctx) {
		t.Error("due at once after the clock went backwards")
	}
	*now = now.Add(AskEvery)
	if !a.Due(ctx) {
		t.Error("not due 30 days after the clock went backwards")
	}

	_ = a.Record(ctx, AskNo)
	for i := 1; i < AskOpens; i++ {
		*now = now.Add(-24 * time.Hour) // backwards at every open
		if a.Due(ctx) {
			t.Fatalf("due at open %d", i)
		}
	}
	*now = now.Add(-24 * time.Hour)
	if !a.Due(ctx) {
		t.Error("never due while the clock keeps going backwards")
	}
}

// TestAskStateUntrusted: a state file that is another user's, or does not
// decode strictly, is no yes, and the question is due; the answer then
// replaces it, root's alone.
func TestAskStateUntrusted(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		content string
		uid     int
	}{
		"another user's":   {`{"answer":"yes","asked":"2026-11-01T00:00:00Z","opens":0}`, 1000},
		"an unknown field": {`{"answer":"yes","asked":"2026-11-01T00:00:00Z","opens":0,"x":1}`, 0},
		"an answer":        {`{"answer":"maybe","asked":"2026-11-01T00:00:00Z","opens":0}`, 0},
		"no time":          {`{"answer":"yes","opens":0}`, 0},
		"a count":          {`{"answer":"no","asked":"2026-11-01T00:00:00Z","opens":-1}`, 0},
		"two values":       {`{"answer":"yes","asked":"2026-11-01T00:00:00Z","opens":0}{}`, 0},
	} {
		a, f, _ := asker()
		f.AddDir(revert.DefaultDir, 0o700, 0, 0)
		f.AddFile(askPath, []byte(tc.content), 0o600, tc.uid, tc.uid)
		if a.Yes(ctx) || !a.Due(ctx) {
			t.Errorf("%s: read as a yes, or not due", name)
		}
		if err := a.Record(ctx, AskYes); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if e, _ := f.Entry(askPath); !a.Yes(ctx) || e.UID != 0 || e.Mode.Perm() != 0o600 {
			t.Errorf("%s: after the answer: %+v", name, e)
		}
	}
}

// TestAskStateNotKept: a state directory that fails its check, so that
// nothing can be written, is never due, at any open or time: the question
// would be asked at every open. Record says why.
func TestAskStateNotKept(t *testing.T) {
	ctx := context.Background()
	a, f, now := asker()
	f.AddDir(revert.DefaultDir, 0o777, 0, 0)
	for i := 0; i < 2*AskOpens; i++ {
		if a.Due(ctx) {
			t.Fatalf("due at open %d", i+1)
		}
	}
	*now = now.Add(2 * AskEvery)
	if a.Due(ctx) || a.Yes(ctx) || a.Record(ctx, AskNo) == nil {
		t.Error("due after 30 days, a yes, or an answer kept")
	}
	if _, ok := f.Entry(askPath); ok {
		t.Error("a state file written")
	}
}
