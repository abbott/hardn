package alpine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// crond's jobs (slice 7 decision 6, R2): what else busybox's crond runs,
// so that turning updates off stops crond only where nothing else needs it.
const (
	// PeriodicDir holds the directories root's stock crontab runs with
	// run-parts (PeriodicDirs).
	PeriodicDir = "/etc/periodic"
	// CrontabsDir is crond's crontab directory: /etc/init.d/crond's
	// CRON_OPTS "-c /etc/crontabs". crond loads every file in it whose
	// name has no '.', as the crontab of the user it names.
	CrontabsDir = "/etc/crontabs"
	RootCrontab = CrontabsDir + "/root"
	// StockCrontab is root's crontab as Alpine's busybox package ships it
	// (Alpine 3.23, read on the VM: 283 bytes): run-parts for each of
	// PeriodicDirs, and nothing else.
	StockCrontab = "# do daily/weekly/monthly maintenance\n" +
		"# min\thour\tday\tmonth\tweekday\tcommand\n" +
		"*/15\t*\t*\t*\t*\trun-parts /etc/periodic/15min\n" +
		"0\t*\t*\t*\t*\trun-parts /etc/periodic/hourly\n" +
		"0\t2\t*\t*\t*\trun-parts /etc/periodic/daily\n" +
		"0\t3\t*\t*\t6\trun-parts /etc/periodic/weekly\n" +
		"0\t5\t1\t*\t*\trun-parts /etc/periodic/monthly\n" +
		"\n"
)

// PeriodicDirs are the directories under PeriodicDir that StockCrontab
// runs, in its order.
var PeriodicDirs = []string{"15min", "hourly", "daily", "weekly", "monthly"}

// Job is one job crond runs besides apk-cron's: its path, and its name in
// the review's words ("logrotate", "alice's crontab").
type Job struct {
	Path string
	Name string
}

// OtherJobs are the jobs crond runs other than apk-cron's: every file
// run-parts would run under PeriodicDirs (a regular file, a link followed,
// with an execute bit and a name run-parts accepts) but Script and
// LegacyScript, then every crontab crond loads in CrontabsDir (a regular
// file whose name has no '.') but RootCrontab holding StockCrontab. A
// missing directory has no jobs; one hardn cannot read, or a file it
// cannot stat or read, is the error, never "no jobs".
func OtherJobs(ctx context.Context, sys host.System) ([]Job, error) {
	var out []Job
	for _, d := range PeriodicDirs {
		dir := path.Join(PeriodicDir, d)
		names, err := dirNames(ctx, sys, dir)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			p := path.Join(dir, name)
			if p == Script || p == LegacyScript || !runPartsName(name) {
				continue
			}
			fi, err := sys.Stat(ctx, p)
			switch {
			case errors.Is(err, fs.ErrNotExist): // a dangling link: nothing runs
				continue
			case err != nil:
				return nil, fmt.Errorf("alpine: crond's jobs: %w", err)
			case fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0:
				out = append(out, Job{Path: p, Name: name})
			}
		}
	}
	names, err := dirNames(ctx, sys, CrontabsDir)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		p := path.Join(CrontabsDir, name)
		if strings.Contains(name, ".") {
			continue
		}
		fi, err := sys.Stat(ctx, p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, fmt.Errorf("alpine: crond's jobs: %w", err)
		case !fi.Mode().IsRegular():
			continue
		}
		if p == RootCrontab {
			data, err := sys.ReadFile(ctx, p)
			if err != nil {
				return nil, fmt.Errorf("alpine: crond's jobs: %w", err)
			}
			if string(data) == StockCrontab {
				continue
			}
		}
		out = append(out, Job{Path: p, Name: name + "'s crontab"})
	}
	return out, nil
}

// dirNames are the names in dir, none when it is missing.
func dirNames(ctx context.Context, sys host.System, dir string) ([]string, error) {
	des, err := sys.ReadDir(ctx, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("alpine: crond's jobs: %w", err)
	}
	names := make([]string, 0, len(des))
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names, nil
}

// runPartsName reports whether busybox's run-parts runs a file of this
// name: the negation of invalid_name in busybox 1.37.0's
// debianutils/run_parts.c (the 1.37.0 release tarball, read 2026-11-03),
// byte for byte. A name starting with '.' is invalid; otherwise every byte
// must be an ASCII letter or digit (libbb.h's isalnum is
// bb_ascii_isalnum), '_', '-' or '.', a dot anywhere after the first byte
// allowed.
func runPartsName(name string) bool {
	if name != "" && name[0] == '.' {
		return false
	}
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}
