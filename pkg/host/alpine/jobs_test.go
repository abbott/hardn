package alpine_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// jobsHost is the VM's Alpine 3.23 as read on 2026-11-03: the periodic
// directories empty, root's stock crontab alone.
func jobsHost() *hostfake.System {
	h := hostfake.New(host.Options{})
	for _, d := range alpine.PeriodicDirs {
		h.AddDir(alpine.PeriodicDir+"/"+d, 0o755, 0, 0)
	}
	h.AddDir(alpine.CrontabsDir, 0o755, 0, 0)
	h.AddFile(alpine.RootCrontab, []byte(alpine.StockCrontab), 0o600, 0, 0)
	return h
}

func TestStockCrontab(t *testing.T) {
	// 283 bytes, as ls -l showed root's crontab on the VM.
	if n := len(alpine.StockCrontab); n != 283 {
		t.Fatalf("StockCrontab is %d bytes, the VM's 283", n)
	}
	for _, d := range alpine.PeriodicDirs {
		if !strings.Contains(alpine.StockCrontab, "run-parts /etc/periodic/"+d+"\n") {
			t.Errorf("StockCrontab does not run %s", d)
		}
	}
}

func TestOtherJobs(t *testing.T) {
	ctx := context.Background()
	script := []byte("#!/bin/sh\n")
	for _, tt := range []struct {
		name  string
		setup func(h *hostfake.System)
		want  []alpine.Job
	}{
		{"the VM", func(*hostfake.System) {}, nil},
		{"apk-cron's and origin/main's scripts", func(h *hostfake.System) {
			h.AddFile(alpine.Script, []byte(apkCron), 0o755, 0, 0)
			h.AddFile(alpine.LegacyScript, []byte(alpine.LegacyContent), 0o755, 0, 0)
		}, nil},
		{"logrotate, and a weekly job", func(h *hostfake.System) {
			h.AddFile("/etc/periodic/daily/logrotate", script, 0o755, 0, 0)
			h.AddFile("/etc/periodic/weekly/backup-web-01", script, 0o700, 0, 0)
		}, []alpine.Job{{"/etc/periodic/daily/logrotate", "logrotate"}, {"/etc/periodic/weekly/backup-web-01", "backup-web-01"}}},
		{"run-parts skips these", func(h *hostfake.System) {
			h.AddFile("/etc/periodic/daily/notes", script, 0o644, 0, 0)
			h.AddFile("/etc/periodic/daily/.hidden", script, 0o755, 0, 0)
			h.AddFile("/etc/periodic/daily/job~", script, 0o755, 0, 0)
			h.AddDir("/etc/periodic/daily/sub", 0o755, 0, 0)
			h.AddSymlink("/etc/periodic/hourly/gone", "/usr/local/bin/gone")
		}, nil},
		{"a dotted name run-parts runs", func(h *hostfake.System) {
			h.AddFile("/etc/periodic/hourly/sync.sh", script, 0o755, 0, 0)
		}, []alpine.Job{{"/etc/periodic/hourly/sync.sh", "sync.sh"}}},
		// busybox 1.37.0's invalid_name: a dot is allowed anywhere but first,
		// a trailing one too; a byte outside ASCII letters, digits, '_', '-'
		// and '.' is not.
		{"dots either way, and a byte run-parts refuses", func(h *hostfake.System) {
			h.AddFile("/etc/periodic/weekly/.sync.sh", script, 0o755, 0, 0)
			h.AddFile("/etc/periodic/weekly/tidy.", script, 0o755, 0, 0)
			h.AddFile("/etc/periodic/weekly/caf\xc3\xa9", script, 0o755, 0, 0)
			h.AddFile("/etc/periodic/weekly/a+b", script, 0o755, 0, 0)
		}, []alpine.Job{{"/etc/periodic/weekly/tidy.", "tidy."}}},
		{"a link to an executable", func(h *hostfake.System) {
			h.AddFile("/usr/local/bin/report", script, 0o755, 0, 0)
			h.AddSymlink("/etc/periodic/monthly/report", "/usr/local/bin/report")
		}, []alpine.Job{{"/etc/periodic/monthly/report", "report"}}},
		{"a 15min job", func(h *hostfake.System) {
			h.AddFile("/etc/periodic/15min/poll", script, 0o755, 0, 0)
		}, []alpine.Job{{"/etc/periodic/15min/poll", "poll"}}},
		{"root's crontab edited", func(h *hostfake.System) {
			h.AddFile(alpine.RootCrontab, []byte(alpine.StockCrontab+"30\t4\t*\t*\t*\t/usr/local/bin/report\n"), 0o600, 0, 0)
		}, []alpine.Job{{alpine.RootCrontab, "root's crontab"}}},
		{"alice's crontab", func(h *hostfake.System) {
			h.AddFile(alpine.CrontabsDir+"/alice", []byte("0 1 * * * true\n"), 0o600, 0, 0)
		}, []alpine.Job{{alpine.CrontabsDir + "/alice", "alice's crontab"}}},
		{"crond skips dotted names", func(h *hostfake.System) {
			h.AddFile(alpine.CrontabsDir+"/cron.update", []byte("alice\n"), 0o600, 0, 0)
			h.AddFile(alpine.CrontabsDir+"/alice.bak", []byte("0 1 * * * true\n"), 0o600, 0, 0)
			h.AddDir(alpine.CrontabsDir+"/carol", 0o700, 0, 0)
		}, nil},
		{"both kinds, in order", func(h *hostfake.System) {
			h.AddFile(alpine.CrontabsDir+"/george", []byte("0 1 * * * true\n"), 0o600, 0, 0)
			h.AddFile("/etc/periodic/daily/logrotate", script, 0o755, 0, 0)
			h.AddFile(alpine.Script, []byte(apkCron), 0o755, 0, 0)
		}, []alpine.Job{{"/etc/periodic/daily/logrotate", "logrotate"}, {alpine.CrontabsDir + "/george", "george's crontab"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := jobsHost()
			tt.setup(h)
			got, err := alpine.OtherJobs(ctx, h)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("OtherJobs = %v, %v, want %v", got, err, tt.want)
			}
		})
	}
	t.Run("nothing there", func(t *testing.T) {
		if got, err := alpine.OtherJobs(ctx, hostfake.New(host.Options{})); got != nil || err != nil {
			t.Fatalf("OtherJobs = %v, %v", got, err)
		}
	})
}

// unreadable fails ReadDir and ReadFile of the paths it names, and Stat as
// faulty does.
type unreadable struct {
	faulty
}

func (u unreadable) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := u.err("readdir", name); err != nil {
		return nil, err
	}
	return u.System.ReadDir(ctx, name)
}

func (u unreadable) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := u.err("read", name); err != nil {
		return nil, err
	}
	return u.System.ReadFile(ctx, name)
}

// TestOtherJobsUnreadable: what hardn cannot read is an error, never "no
// jobs", which would let off stop a crond another job needs.
func TestOtherJobsUnreadable(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []string{"readdir /etc/periodic/daily", "readdir " + alpine.CrontabsDir, "stat /etc/periodic/daily/logrotate", "read " + alpine.RootCrontab} {
		t.Run(fail, func(t *testing.T) {
			h := jobsHost()
			h.AddFile("/etc/periodic/daily/logrotate", []byte("#!/bin/sh\n"), 0o755, 0, 0)
			got, err := alpine.OtherJobs(ctx, unreadable{faulty{System: h, fail: map[string]error{fail: fs.ErrPermission}}})
			if got != nil || !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("OtherJobs = %v, %v, want fs.ErrPermission", got, err)
			}
		})
	}
}
