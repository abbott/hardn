package hostfake_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

func TestStartDetached(t *testing.T) {
	ctx := context.Background()
	var log bytes.Buffer
	f := hostfake.New(host.Options{RunLog: &log})
	f.Respond(hostfake.Response{PID: 4242}, "sh", "-c", "sleep 1")
	f.Respond(hostfake.Response{}, "sh", "-c", "sleep 2")
	boom := errors.New("fixture: fork failed")
	f.Respond(hostfake.Response{Err: boom}, "sh", "-c", "sleep 3")

	if pid, err := f.StartDetached(ctx, "sh", "-c", "sleep 1"); pid != 4242 || err != nil {
		t.Errorf("scripted PID: %d, %v; want 4242, nil", pid, err)
	}
	if pid, err := f.StartDetached(ctx, "sh", "-c", "sleep 2"); pid != 1000 || err != nil {
		t.Errorf("counted PID: %d, %v; want 1000, nil", pid, err)
	}
	if pid, err := f.StartDetached(ctx, "sh", "-c", "sleep 2"); pid != 1001 || err != nil {
		t.Errorf("next counted PID: %d, %v; want 1001, nil", pid, err)
	}
	if pid, err := f.StartDetached(ctx, "sh", "-c", "sleep 3"); pid != 0 || err != boom {
		t.Errorf("scripted error: %d, %v; want 0, the error verbatim", pid, err)
	}
	if _, err := f.StartDetached(ctx, "sh", "-c", "unscripted"); !errors.Is(err, hostfake.ErrUnscripted) {
		t.Errorf("unscripted: %v, want ErrUnscripted", err)
	}
	if _, err := f.StartDetached(ctx, "./sh"); !errors.Is(err, host.ErrNotFound) {
		t.Errorf("relative name: %v, want host.ErrNotFound", err)
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.StartDetached(done, "sh", "-c", "sleep 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("done context: %v, want context.Canceled", err)
	}

	calls := f.Calls()
	if len(calls) != 7 || calls[0].Op != "start-detached" || calls[0].PID != 4242 {
		t.Fatalf("calls = %v", calls)
	}
	if got := calls[0].String(); got != `start-detached sh -c "sleep 1": pid 4242` {
		t.Errorf("Call.String = %q", got)
	}
	if len(f.Runs()) != 0 {
		t.Errorf("Runs = %q; a detached start is not a run", f.Runs())
	}
	if !strings.Contains(log.String(), "run 1: started detached, pid 4242, not waited for") ||
		!strings.Contains(log.String(), "run 4: not started: fixture: fork failed") {
		t.Errorf("run log = %q", log.String())
	}
}

// A Handler answers a detached start as it answers a run, and can model its
// effect on the fake.
func TestStartDetachedHandler(t *testing.T) {
	f := hostfake.New(host.Options{})
	f.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if stdin != nil {
			t.Errorf("stdin = %q, want nil", stdin)
		}
		f.AddFile("/run/started", []byte(strings.Join(argv, " ")), 0o600, 0, 0)
		return hostfake.Response{PID: 77}
	}
	pid, err := f.StartDetached(context.Background(), "/bin/sh", "-c", "true")
	if pid != 77 || err != nil {
		t.Fatalf("StartDetached = %d, %v", pid, err)
	}
	if e, ok := f.Entry("/run/started"); !ok || string(e.Data) != "/bin/sh -c true" {
		t.Errorf("handler effect: %+v, %v", e, ok)
	}
}
