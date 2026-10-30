package output_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
)

// detailsFacts are detect's facts for the report's host.
func detailsFacts() *detect.Facts {
	return &detect.Facts{
		OS:        detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie", PrettyName: "Debian GNU/Linux 13 (trixie)"},
		Platform:  detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:      detect.Init{Kind: detect.Systemd, Running: detect.Yes},
		Proxmox:   detect.Proxmox{Installed: detect.No},
		Elevation: detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: detect.SudoOriginal, Doas: detect.Tool{Present: detect.No}},
	}
}

var sudoYes = []users.Grant{{Tool: "sudo", Grants: detect.Yes}, {Tool: "doas", Grants: detect.No}}

// wholeReport is every section read, the values the gallery's debian13 host has.
func wholeReport() details.Report {
	at := time.Date(2026, 10, 20, 11, 42, 5, 0, time.UTC)
	return details.Report{
		OS:   details.OSKernel{OS: header.OS{Name: "Debian", Version: "13", Codename: "trixie"}, Kernel: "6.12.113+deb13-cloud-amd64", Uptime: header.Uptime{Duration: 3*24*time.Hour + 5*time.Hour + 12*time.Minute, Known: true}},
		Host: details.HostIPs{Hostname: "web-01", Addresses: []details.Address{{Interface: "eth0", Family: "inet", Address: "192.0.2.20/24"}, {Interface: "eth0", Family: "inet6", Address: "2001:db8::20/64"}}},
		Users: details.Users{Root: detect.Yes, List: []details.User{{Name: "alice", Grants: sudoYes},
			{Name: "svc", Grants: []users.Grant{{Tool: "sudo", Grants: detect.No}, {Tool: "doas", Grants: detect.No}}}}},
		CPU:    details.CPU{Model: "QEMU Virtual CPU version 2.5+", CPUs: 4, Sockets: 1, Load: [3]string{"0.12", "0.08", "0.03"}},
		Disk:   details.Disk{Source: "/dev/vda1", Type: "ext4", Used: 4518241280, Total: 41442029568, Known: true},
		Memory: details.Memory{Total: 4080218931, Available: 2791728742, AvailableKnown: true},
		Logins: details.Logins{State: details.LoginsRead, List: []details.Login{
			{User: "root", State: details.LoginNone},
			{User: "alice", State: details.LoginAt, When: "2026-10-20T11:42:05+00:00", TTY: "pts/0", From: "192.0.2.10", At: at},
			{User: "svc"},
		}},
	}
}

func detailsRun(r details.Report) output.Run {
	return output.Run{Mode: output.ModeCheck, HardnVersion: "0.4.0", Hostname: "web-01", Facts: detailsFacts(), Details: &r}
}

// TestDetailsDocuments pins the whole form: read, as the gallery's cli-json draws
// it, and with every section not read.
func TestDetailsDocuments(t *testing.T) {
	refused := fs.ErrPermission
	unread := func(src string) []details.Unread { return []details.Unread{{Source: src, Err: refused}} }
	for name, r := range map[string]details.Report{
		"details": wholeReport(),
		"details-unreadable": {
			OS:     details.OSKernel{Unread: []details.Unread{{Source: "/proc/sys/kernel/osrelease", Err: refused}, {Source: "/proc/uptime", Err: refused}}},
			Host:   details.HostIPs{Unread: unread("ip addr")},
			Users:  details.Users{Unread: unread("/etc/passwd")},
			CPU:    details.CPU{Unread: []details.Unread{{Source: "/proc/cpuinfo", Err: refused}, {Source: "/proc/loadavg", Err: refused}}},
			Disk:   details.Disk{Unread: []details.Unread{{Source: "/proc/self/mounts", Err: refused}, {Source: "df", Err: refused}}},
			Memory: details.Memory{Unread: unread("/proc/meminfo")},
			Logins: details.Logins{Unread: unread("lslogins")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			if err := output.WriteJSON(&b, output.Build(detailsRun(r))); err != nil {
				t.Fatal(err)
			}
			golden(t, "json/"+name+".json", b.Bytes())
		})
	}
}

// TestDetailsOnThisFormAlone: the field is on this form's document alone: every
// other has the nine, and an audit line's bytes do not change, the same document
// being its body (output_test.go, TestStdoutIsOneDocument).
func TestDetailsOnThisFormAlone(t *testing.T) {
	keys := func(t *testing.T, v any) map[string]any {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	for _, fx := range fixtures() {
		if doc := keys(t, output.Build(fx.run)); len(doc) != 9 || doc["details"] != nil {
			t.Errorf("%s: %d fields, details %v", fx.name, len(doc), doc["details"])
		}
		if doc := keys(t, output.NewRecord(fx.run, fixtureNow)); len(doc) != 10 || doc["details"] != nil {
			t.Errorf("%s: its audit line has %d fields", fx.name, len(doc))
		}
	}
	doc := keys(t, output.Build(detailsRun(wholeReport())))
	if len(doc) != 10 || doc["details"] == nil {
		t.Errorf("%d fields: %v", len(doc), doc)
	}
}

// TestDetailsPure: Build reads nothing, and the same report gives the same bytes.
func TestDetailsPure(t *testing.T) {
	a, err := json.Marshal(output.Build(detailsRun(wholeReport())))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(output.Build(detailsRun(wholeReport())))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("two builds differ:\n%s\n%s", a, b)
	}
}

// TestDetailsRead: a report read, nothing in findings or errors, exit 0.
func TestDetailsRead(t *testing.T) {
	d := output.Build(detailsRun(wholeReport()))
	if d.Mode != output.ModeCheck || d.Result.Outcome != output.OutcomeNothingToDo || d.Result.ExitCode != 0 || len(d.Findings) != 0 || len(d.Errors) != 0 {
		t.Errorf("document = %+v", d)
	}
	got := d.Details
	if *got.Kernel != "6.12.113+deb13-cloud-amd64" || *got.Uptime != "3 days, 5 hours, 12 minutes" {
		t.Errorf("OS and Kernel = %v %v", *got.Kernel, *got.Uptime)
	}
	if want := []output.DetailUser{{Name: "alice", Tool: "sudo"}, {Name: "svc", Tool: ""}}; !reflect.DeepEqual(got.Users, want) {
		t.Errorf("users = %+v", got.Users)
	}
	want := []output.DetailLogin{{User: "root"}, {User: "alice", When: "2026-10-20T11:42:05+00:00", TTY: "pts/0", From: "192.0.2.10"}, {User: "svc", Unknown: true}}
	if !reflect.DeepEqual(got.Logins, want) {
		t.Errorf("logins = %+v", got.Logins)
	}
}

// TestDetailsUnread: a section not read is null with one finding, its detail the
// words decided (gallery-system-details.md §6), exit 0 as the rest was read; a
// fact not read in a section that read another is null beside it.
func TestDetailsUnread(t *testing.T) {
	refused := fs.ErrPermission
	one := func(src string) []details.Unread { return []details.Unread{{Source: src, Err: refused}} }
	for _, tc := range []struct {
		name   string
		change func(r *details.Report)
		detail string
		source string
		want   string // the details field's key and its JSON
	}{
		{"kernel", func(r *details.Report) { r.OS.Kernel, r.OS.Unread = "", one("/proc/sys/kernel/osrelease") }, "os and kernel not read", "/proc/sys/kernel/osrelease", `"kernel":null`},
		{"uptime", func(r *details.Report) { r.OS.Uptime, r.OS.Unread = header.Uptime{}, one("/proc/uptime") }, "os and kernel not read", "/proc/uptime", `"uptime":null`},
		{"addresses", func(r *details.Report) { r.Host.Addresses, r.Host.Unread = nil, one("ip addr") }, "host and ips not read", "ip addr", `"addresses":null`},
		{"users", func(r *details.Report) { r.Users.List, r.Users.Unread = nil, one("/etc/passwd") }, "users not read", "/etc/passwd", `"users":null`},
		{"not root", func(r *details.Report) {
			r.Users.Root = detect.No
			r.Users.List = []details.User{{Name: "alice", Grants: []users.Grant{{Tool: "sudo"}, {Tool: "doas", Grants: detect.No}}}}
			r.Users.Unread = []details.Unread{{Source: "/etc/sudoers", Err: refused}, {Source: "/etc/sudoers.d", Err: refused}}
		}, "users not read", "/etc/sudoers, /etc/sudoers.d", `"users":[{"name":"alice","tool":"unknown"}]`},
		{"cpuinfo", func(r *details.Report) { r.CPU = details.CPU{Load: r.CPU.Load, Unread: one("/proc/cpuinfo")} }, "cpu and load not read", "/proc/cpuinfo",
			`"cpu":{"model":null,"cpus":null,"sockets":null,"load":["0.12","0.08","0.03"]}`},
		{"loadavg", func(r *details.Report) { r.CPU.Load, r.CPU.Unread = [3]string{}, one("/proc/loadavg") }, "cpu and load not read", "/proc/loadavg",
			`"cpu":{"model":"QEMU Virtual CPU version 2.5+","cpus":4,"sockets":1,"load":null}`},
		{"cpu", func(r *details.Report) { r.CPU = details.CPU{Unread: one("/proc/cpuinfo")} }, "cpu and load not read", "/proc/cpuinfo", `"cpu":null`},
		{"df", func(r *details.Report) { r.Disk = details.Disk{Source: "/dev/vda1", Type: "ext4", Unread: one("df")} }, "disk not read", "df",
			`"disk":{"source":"/dev/vda1","type":"ext4","usedBytes":null,"totalBytes":null}`},
		{"mounts", func(r *details.Report) {
			r.Disk = details.Disk{Used: 1, Total: 2, Known: true, Unread: one("/proc/self/mounts")}
		}, "disk not read", "/proc/self/mounts",
			`"disk":{"source":null,"type":null,"usedBytes":1,"totalBytes":2}`},
		{"disk", func(r *details.Report) { r.Disk = details.Disk{Unread: one("df")} }, "disk not read", "df", `"disk":null`},
		{"memory", func(r *details.Report) { r.Memory = details.Memory{Unread: one("/proc/meminfo")} }, "memory not read", "/proc/meminfo", `"memory":null`},
		{"lslogins failed", func(r *details.Report) { r.Logins = details.Logins{Unread: one("lslogins")} }, "last login not read", "lslogins", `"logins":null`},
		{"lslogins not installed", func(r *details.Report) {
			r.Logins = details.Logins{State: details.LoginsNoTool, Unread: []details.Unread{{Source: "lslogins", Err: host.ErrNotFound}}}
		}, "last login not read", "lslogins", `"logins":null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := wholeReport()
			tc.change(&r)
			d := output.Build(detailsRun(r))
			if d.Result.ExitCode != 0 || len(d.Errors) != 0 {
				t.Errorf("result %+v, errors %+v", d.Result, d.Errors)
			}
			want := []output.Finding{{Code: "DETAILS_UNREADABLE", Detail: tc.detail, Source: tc.source}}
			if !reflect.DeepEqual(d.Findings, want) {
				t.Errorf("findings = %+v", d.Findings)
			}
			data, err := json.Marshal(d.Details)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.want) {
				t.Errorf("details = %s, want %s", data, tc.want)
			}
		})
	}
}

// TestDetailsNotKnown: what the host does not say is null or the word, and no
// finding: nothing failed. arm64's cpuinfo is [I, not run]; a ZFS root with no
// zpool is [unverified].
func TestDetailsNotKnown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(r *details.Report)
		want   string
	}{
		{"arm64's CPU", func(r *details.Report) { r.CPU.Model, r.CPU.Sockets = "", 0 }, `"cpu":{"model":null,"cpus":4,"sockets":null,"load":["0.12","0.08","0.03"]}`},
		{"no MemAvailable", func(r *details.Report) { r.Memory.Available, r.Memory.AvailableKnown = 0, false }, `"memory":{"usedBytes":null,"totalBytes":4080218931}`},
		{"a pool, its health not known", func(r *details.Report) {
			r.Disk = details.Disk{Source: "rpool/ROOT/pve-1", Type: "zfs", Used: 1, Total: 2, Known: true, Pool: "rpool"}
		}, `"disk":{"source":"rpool/ROOT/pve-1","type":"zfs","usedBytes":1,"totalBytes":2,"pool":"rpool","health":"unknown"}`},
		{"a pool, degraded", func(r *details.Report) {
			r.Disk = details.Disk{Source: "rpool/ROOT/pve-1", Type: "zfs", Used: 1, Total: 2, Known: true, Pool: "rpool", Health: "DEGRADED"}
		}, `"pool":"rpool","health":"DEGRADED"`},
		{"no addresses", func(r *details.Report) { r.Host.Addresses = []details.Address{} }, `"addresses":[]`},
		{"no one listed", func(r *details.Report) { r.Users.List = []details.User{} }, `"users":[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := wholeReport()
			tc.change(&r)
			d := output.Build(detailsRun(r))
			if len(d.Findings) != 0 {
				t.Errorf("findings = %+v", d.Findings)
			}
			data, err := json.Marshal(d.Details)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.want) {
				t.Errorf("details = %s, want %s", data, tc.want)
			}
		})
	}
}
