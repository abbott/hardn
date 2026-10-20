package revert

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestReadConfirmed (next/p3-confirm-cleanup): confirmed.json, the last
// change confirmed, read as ReadState reads the journal: nil with no
// directory or no file; the journal Confirm moved there; refused when
// another user can change it (ErrUnsafe), through a symlink, and with a
// field this version does not know (ErrJournal).
func TestReadConfirmed(t *testing.T) {
	ctx := context.Background()
	if j, err := ReadConfirmed(ctx, newHost(t), Options{}); j != nil || err != nil {
		t.Errorf("no directory: %+v, %v; want nil, nil", j, err)
	}
	f := newHost(t)
	c := &clock{testNow}
	armed(t, f, c)
	if j, err := ReadConfirmed(ctx, f, Options{}); j != nil || err != nil {
		t.Errorf("nothing confirmed yet: %+v, %v; want nil, nil", j, err)
	}
	want := markApplied(t, f, c)
	respond(f, hostfake.Response{}, []string{"systemctl", "stop", "hardn-revert-" + testID + ".timer"})
	if _, err := Confirm(ctx, f, newSession, testOptions(c, nil)); err != nil {
		t.Fatal(err)
	}
	if j, err := ReadConfirmed(ctx, f, Options{}); err != nil || j == nil || !reflect.DeepEqual(*j, want) {
		t.Errorf("ReadConfirmed = %+v, %v; want the journal confirmed", j, err)
	}
	e, _ := f.Entry(stateFile(ConfirmedName))
	for _, tt := range []struct {
		name  string
		setup func(f *hostfake.System)
		want  error // nil: refused by the guarded read itself
	}{
		{"alice's", func(f *hostfake.System) { f.AddFile(stateFile(ConfirmedName), e.Data, 0o600, 1000, 1000) }, ErrUnsafe},
		{"world-writable", func(f *hostfake.System) { f.AddFile(stateFile(ConfirmedName), e.Data, 0o666, 0, 0) }, ErrUnsafe},
		{"/var/lib writable by others", func(f *hostfake.System) {
			f.AddDir("/var/lib", 0o777, 0, 0)
			f.AddFile(stateFile(ConfirmedName), e.Data, 0o600, 0, 0)
		}, ErrUnsafe},
		{"a symlink", func(f *hostfake.System) {
			f.AddFile("/var/lib/elsewhere.json", e.Data, 0o600, 0, 0)
			f.AddSymlink(stateFile(ConfirmedName), "/var/lib/elsewhere.json")
		}, nil},
		{"an unknown field", func(f *hostfake.System) {
			f.AddFile(stateFile(ConfirmedName), bytes.Replace(e.Data, []byte(`"risk"`), []byte(`"runFirst": ["sh"], "risk"`), 1), 0o600, 0, 0)
		}, ErrJournal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newHost(t)
			f.AddDir(DefaultDir, 0o700, 0, 0)
			tt.setup(f)
			j, err := ReadConfirmed(ctx, f, Options{})
			switch {
			case j != nil || err == nil:
				t.Fatalf("ReadConfirmed = %+v, %v; want refused", j, err)
			case tt.want != nil && !errors.Is(err, tt.want):
				t.Fatalf("ReadConfirmed: %v, want %v", err, tt.want)
			}
		})
	}
}
