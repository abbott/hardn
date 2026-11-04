package revert

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/hostfake"
)

const recControl = "updates.auto"

// record is a record of two entries, on and off, as the updates control
// would keep them (synthetic values).
func record() ControlRecord {
	sum := strings.Repeat("ab", 32)
	return ControlRecord{SchemaVersion: RecordVersion, Entries: []RecordEntry{
		{Direction: DirectionOn, Time: testNow, Hardn: "v0.4.0", PlanID: testID, Steps: []string{"install", "file"},
			Packages: []string{"unattended-upgrades", "python3-dbus"},
			Before:   []FileHash{{Path: "/etc/apt/apt.conf.d/20auto-upgrades", By: "unattended-upgrades"}},
			Files:    []FileHash{{Path: "/etc/apt/apt.conf.d/52hardn-auto-upgrades", SHA256: sum, By: "hardn"}},
			Lines:    []string{`APT::Periodic::Unattended-Upgrade "1"`}, ReadBack: "Configured: unattended-upgrades",
			Services: []ServiceState{{Name: "crond", State: "stopped", AtBoot: "no"}}, Jobs: []string{"/etc/periodic/daily/logrotate"}},
		{Direction: DirectionOff, Time: testNow.Add(time.Hour), Hardn: "v0.4.0", PlanID: otherID, Steps: []string{"remove"}},
	}}
}

func TestRecordPath(t *testing.T) {
	if p, err := RecordPath(Options{}, recControl); err != nil || p != "/var/lib/hardn/controls/updates.auto.json" {
		t.Fatalf("RecordPath = %q, %v", p, err)
	}
	for _, id := range []string{"", ".auto", "updates..auto", "../x", "a/b", strings.Repeat("a", 65)} {
		if _, err := RecordPath(Options{}, id); !errors.Is(err, ErrID) {
			t.Errorf("RecordPath(%q) = %v, want ErrID", id, err)
		}
	}
}

func TestRecordCheck(t *testing.T) {
	if err := record().Check(); err != nil {
		t.Fatalf("valid record: %v", err)
	}
	for _, tt := range []struct {
		name string
		edit func(*ControlRecord)
	}{
		{"schemaVersion 2", func(r *ControlRecord) { r.SchemaVersion = 2 }},
		{"no entries", func(r *ControlRecord) { r.Entries = nil }},
		{"direction", func(r *ControlRecord) { r.Entries[0].Direction = "up" }},
		{"no time", func(r *ControlRecord) { r.Entries[1].Time = time.Time{} }},
		{"plan id", func(r *ControlRecord) { r.Entries[0].PlanID = "../x" }},
		{"no steps", func(r *ControlRecord) { r.Entries[1].Steps = nil }},
		{"relative path", func(r *ControlRecord) { r.Entries[0].Files[0].Path = "etc/x" }},
		{"upper-case hash", func(r *ControlRecord) { r.Entries[0].Files[0].SHA256 = strings.Repeat("AB", 32) }},
		{"short hash", func(r *ControlRecord) { r.Entries[0].Before[0].SHA256 = "ab" }},
		{"no by", func(r *ControlRecord) { r.Entries[0].Files[0].By = "" }},
		{"job path", func(r *ControlRecord) { r.Entries[0].Jobs = []string{"logrotate"} }},
		{"service state", func(r *ControlRecord) { r.Entries[0].Services[0].State = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := record()
			tt.edit(&r)
			if err := r.Check(); !errors.Is(err, ErrRecord) {
				t.Fatalf("Check = %v, want ErrRecord", err)
			}
			if _, _, err := EncodeRecord(Options{}, recControl, r); !errors.Is(err, ErrRecord) {
				t.Fatalf("EncodeRecord = %v, want ErrRecord", err)
			}
		})
	}
}

// recHost is a fake host with the records' directory, root's, 0700, and
// data as the record of recControl, mode perm, owned by uid.
func recHost(t *testing.T, data []byte, perm fs.FileMode, uid int) *hostfake.System {
	t.Helper()
	f := newHost(t)
	f.AddDir(DefaultDir, 0o700, 0, 0)
	f.AddDir(DefaultDir+"/"+ControlsName, 0o700, 0, 0)
	if data != nil {
		f.AddFile(DefaultDir+"/"+ControlsName+"/"+recControl+".json", data, perm, uid, uid)
	}
	return f
}

func TestRecordRoundTrip(t *testing.T) {
	ctx := context.Background()
	name, data, err := EncodeRecord(Options{}, recControl, record())
	if err != nil || name != "/var/lib/hardn/controls/updates.auto.json" || !bytes.HasSuffix(data, []byte("}\n")) {
		t.Fatalf("EncodeRecord = %q, %q, %v", name, data, err)
	}
	got, err := ReadRecord(ctx, recHost(t, data, 0o600, 0), Options{}, recControl)
	if err != nil || got == nil || !reflect.DeepEqual(*got, record()) {
		t.Fatalf("ReadRecord = %+v, %v", got, err)
	}
}

func TestReadRecordNone(t *testing.T) {
	ctx := context.Background()
	for name, f := range map[string]*hostfake.System{"no directory": newHost(t), "no file": recHost(t, nil, 0, 0)} {
		if got, err := ReadRecord(ctx, f, Options{}, recControl); got != nil || err != nil {
			t.Errorf("%s: ReadRecord = %+v, %v", name, got, err)
		}
	}
}

func TestReadRecordRefused(t *testing.T) {
	ctx := context.Background()
	_, data, err := EncodeRecord(Options{}, recControl, record())
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		host func(t *testing.T) *hostfake.System
		want error
	}{
		{"alice's file", func(t *testing.T) *hostfake.System { return recHost(t, data, 0o600, 1000) }, ErrUnsafe},
		{"world-writable file", func(t *testing.T) *hostfake.System { return recHost(t, data, 0o666, 0) }, ErrUnsafe},
		{"directory writable by others", func(t *testing.T) *hostfake.System {
			f := recHost(t, data, 0o600, 0)
			f.AddDir(DefaultDir+"/"+ControlsName, 0o777, 0, 0)
			return f
		}, ErrUnsafe},
		{"a symlink", func(t *testing.T) *hostfake.System {
			f := recHost(t, nil, 0, 0)
			f.AddFile("/var/lib/elsewhere.json", data, 0o600, 0, 0)
			f.AddSymlink(DefaultDir+"/"+ControlsName+"/"+recControl+".json", "/var/lib/elsewhere.json")
			return f
		}, nil},
		{"an unknown field", func(t *testing.T) *hostfake.System {
			return recHost(t, bytes.Replace(data, []byte(`"entries"`), []byte(`"apparmor": {}, "entries"`), 1), 0o600, 0)
		}, ErrRecord},
		{"schemaVersion 2", func(t *testing.T) *hostfake.System {
			return recHost(t, bytes.Replace(data, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 2`), 1), 0o600, 0)
		}, ErrRecord},
		{"cut short", func(t *testing.T) *hostfake.System { return recHost(t, data[:len(data)/2], 0o600, 0) }, ErrRecord},
		{"two documents", func(t *testing.T) *hostfake.System {
			return recHost(t, append(append([]byte{}, data...), data...), 0o600, 0)
		}, ErrRecord},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadRecord(ctx, tt.host(t), Options{}, recControl)
			if got != nil || err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("ReadRecord = %+v, %v, want %v", got, err, tt.want)
			}
		})
	}
}

// TestRecordRealFiles prepares the directory and reads a record on real
// files in t.TempDir(), through host.Real.
func TestRecordRealFiles(t *testing.T) {
	ctx := context.Background()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func(top string, uid int) { trustedTop, trustedUID = top, uid }(trustedTop, trustedUID)
	trustedTop, trustedUID = dir, os.Geteuid()
	sys := newRealT(t)
	opts := Options{Dir: filepath.Join(dir, "lib", "hardn")}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PrepareRecordDir(ctx, sys, opts); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(opts.Dir, ControlsName)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("records' directory: %v, %v", fi, err)
	}
	name, data, err := EncodeRecord(opts, recControl, record())
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.WriteFileAtomic(ctx, name, data, 0o600, -1, -1); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRecord(ctx, sys, opts, recControl)
	if err != nil || got == nil || !reflect.DeepEqual(*got, record()) {
		t.Fatalf("ReadRecord = %+v, %v", got, err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRecord(ctx, sys, opts, recControl); err != nil {
		t.Fatalf("0644 record: %v", err)
	}
	if err := os.Chmod(name, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRecord(ctx, sys, opts, recControl); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("0666 record: %v, want ErrUnsafe", err)
	}
}
