package mainmenu_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// eolHost is a host with the os-release given ("" for none) whose
// dpkg-query answers dpkg for pve-manager; every other command is missing.
func eolHost(osRelease string, dpkg hostfake.Response) *hostfake.System {
	f := hostfake.New(host.Options{})
	if osRelease != "" {
		f.AddFile(detect.EtcOSRelease, []byte(osRelease), 0o644, 0, 0)
	}
	f.AddFile(detect.SelfMounts, nil, 0o444, 0, 0)
	f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if argv[0] == "dpkg-query" && argv[len(argv)-1] == "pve-manager" {
			return dpkg
		}
		return hostfake.Response{Err: host.ErrNotFound}
	}
	return f
}

// TestStatusNotice: the status carries the end-of-support notice for the
// release the host's os-release and pve-manager establish, on the day of
// the clock given: each state of the gallery's mocks
// (docs/engine/gallery-eol.md §2), Proxmox VE's two releases, and none
// where the release is not established: os-release missing or with no ID,
// a VERSION_ID that is no release's, dpkg failing or answering what hardn
// does not recognise on a Debian. dpkg-query missing is a Debian that is
// not Proxmox VE. Every call only reads.
func TestStatusNotice(t *testing.T) {
	var (
		noPVE  = hostfake.Response{Stderr: "dpkg-query: no packages found matching pve-manager\n", ExitCode: 1}
		noDpkg = hostfake.Response{Err: host.ErrNotFound}
		pve    = func(v string) hostfake.Response {
			return hostfake.Response{Stdout: "install ok installed\t" + v + "\n"}
		}
		alpine    = func(v string) string { return "ID=alpine\nVERSION_ID=" + v + "\n" }
		debian12  = "ID=debian\nVERSION_ID=\"12\"\n"
		debian13  = "ID=debian\nVERSION_ID=\"13\"\n"
		upgrade   = "Upgrade to Alpine 3.24."
		noNotice  *screen.Alert
		alpineEnd = "Alpine 3.22 support ends 2027-05-01"
	)
	for _, tc := range []struct {
		name      string
		osRelease string
		dpkg      hostfake.Response
		today     string
		want      *screen.Alert
	}{
		{"soon", alpine("3.22.4"), noDpkg, "2027-03-15", &screen.Alert{Title: alpineEnd, Fix: upgrade}},
		{"soon, a month", "ID=ubuntu\nVERSION_ID=\"22.04\"\n", noPVE, "2027-04-01",
			&screen.Alert{Title: "Ubuntu 22.04 support ends May 2027", Fix: "Upgrade to Ubuntu 26.04."}},
		{"ended, best effort", alpine("3.22.4"), noDpkg, "2027-05-10", &screen.Alert{Title: "Alpine 3.22 support ended 2027-05-01", Fix: upgrade}},
		{"out of scope", alpine("3.21.7"), noDpkg, "2026-11-13",
			&screen.Alert{Title: "Alpine 3.21 support ended 2026-11-01", Why: screen.OutOfScopeRefused, Fix: upgrade}},
		{"supported", debian12, noPVE, "2028-05-01", &screen.Alert{Title: "Debian 12 support ends 2028-06-30", Fix: "Upgrade to Debian 13."}},
		{"the newest release names no upgrade", debian13, noPVE, "2030-06-01", &screen.Alert{Title: "Debian 13 support ends 2030-06-30"}},
		{"more than 90 days before", alpine("3.22.4"), noDpkg, "2027-01-30", noNotice},
		{"Proxmox VE 9 has no date", debian13, pve("9.2.22"), "2040-01-01", noNotice},
		{"Proxmox VE 8", debian12, pve("8.4.1"), "2026-11-13",
			&screen.Alert{Title: "Proxmox VE 8 support ended August 2026", Why: screen.OutOfScopeRefused, Fix: "Upgrade to Proxmox VE 9."}},
		{"a Debian with no dpkg-query is not Proxmox VE", debian12, noDpkg, "2028-05-01",
			&screen.Alert{Title: "Debian 12 support ends 2028-06-30", Fix: "Upgrade to Debian 13."}},
		{"no os-release", "", noDpkg, "2040-01-01", noNotice},
		{"an os-release with no ID", "NAME=\"Something\"\nVERSION_ID=3.22.4\n", noDpkg, "2040-01-01", noNotice},
		{"an os-release that is not one", "\x00\x1b[2Jnot an os-release\n", noDpkg, "2040-01-01", noNotice},
		{"a release with no row", alpine("3.19.9"), noDpkg, "2040-01-01", noNotice},
		{"a VERSION_ID that is no release's", alpine("3.22.4\x1b[31m"), noDpkg, "2040-01-01", noNotice},
		{"an unstable build of a listed branch", alpine("3.22.0_rc1"), noDpkg, "2040-01-01", noNotice},
		{"Debian or Proxmox VE not established: dpkg failed", debian12, hostfake.Response{Stderr: "dpkg-query: error\n", ExitCode: 2}, "2040-01-01", noNotice},
		{"Debian or Proxmox VE not established: dpkg's answer not recognised", debian12, hostfake.Response{Stdout: "what\n"}, "2040-01-01", noNotice},
		{"a pve-manager version that is none", debian12, pve("abc"), "2040-01-01", noNotice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := eolHost(tc.osRelease, tc.dpkg)
			o := options(f)
			o.Now = func() time.Time {
				d, err := time.Parse(time.DateOnly, tc.today)
				if err != nil {
					t.Fatal(err)
				}
				return d.Add(9 * time.Hour)
			}
			vm, err := mainmenu.Status(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case (vm.Notice == nil) != (tc.want == nil), vm.Notice != nil && *vm.Notice != *tc.want:
				t.Fatalf("notice %+v, want %+v", vm.Notice, tc.want)
			}
			if out := plain(t, vm); strings.Contains(out, "support end") != (tc.want != nil) || strings.ContainsAny(out, "\x1b\x00") {
				t.Errorf("drawn:\n%q", out)
			}
			for _, c := range f.Calls() {
				switch c.Op {
				case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group", "run":
				default:
					t.Errorf("a call that is neither a read nor a command: %v", c)
				}
			}
		})
	}
}
