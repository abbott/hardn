package output

import (
	"strings"

	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host/detect"
)

// CodeDetailsUnreadable is the finding of a section of the System Details
// report that was not read whole (Details). It stops nothing: the rest was read.
const CodeDetailsUnreadable = "DETAILS_UNREADABLE"

// Details is the System Details report, on the document of that form alone
// (decision 1 (a), gallery-system-details.md §5, Made here, "The JSON form"): the
// one field past the nine, added as SchemaVersion's comment allows, and omitted
// from every other document and so from the audit log's lines. Here a null is a
// fact: a section or a fact not read, or not known, is null, and an array of
// none read is [], where the rest of the document's arrays are never null. Each
// section with anything not read has one finding, CodeDetailsUnreadable.
type Details struct {
	Kernel    *string         `json:"kernel"`
	Uptime    *string         `json:"uptime"`
	Addresses []DetailAddress `json:"addresses"`
	Users     []DetailUser    `json:"users"`
	CPU       *DetailCPU      `json:"cpu"`
	Disk      *DetailDisk     `json:"disk"`
	Memory    *DetailMemory   `json:"memory"`
	Logins    []DetailLogin   `json:"logins"`
}

// DetailAddress is one global address, as ip printed it.
type DetailAddress struct {
	Interface string `json:"interface"`
	Family    string `json:"family"` // inet, inet6
	Address   string `json:"address"`
}

// DetailUser is one listed account: the tool that lets them administer, sudo or
// doas (open); "" for none; "unknown" where that is not established, as where
// hardn runs as another user and cannot read the rules.
type DetailUser struct {
	Name string `json:"name"`
	Tool string `json:"tool"`
}

// DetailCPU is the CPU and Load section: CPUs are logical CPUs. Model and
// Sockets are null where /proc/cpuinfo does not say.
type DetailCPU struct {
	Model   *string    `json:"model"`
	CPUs    *int       `json:"cpus"`
	Sockets *int       `json:"sockets"`
	Load    *[3]string `json:"load"`
}

// DetailDisk is the root file system. Pool and Health are there on ZFS alone,
// Health as zpool says it (open), or "unknown".
type DetailDisk struct {
	Source *string `json:"source"`
	Type   *string `json:"type"`
	Used   *uint64 `json:"usedBytes"`
	Total  *uint64 `json:"totalBytes"`
	Pool   string  `json:"pool,omitempty"`
	Health string  `json:"health,omitempty"`
}

// DetailMemory is the Memory section; Used is null where the kernel has no
// MemAvailable.
type DetailMemory struct {
	Used  *uint64 `json:"usedBytes"`
	Total uint64  `json:"totalBytes"`
}

// DetailLogin is one account's newest login with a terminal: When as lslogins
// printed it; all three "" for none recorded. Unknown says nothing is known of
// the account: lslogins had no line for it, or a blank where no source shows
// that it could have had one.
type DetailLogin struct {
	User    string `json:"user"`
	When    string `json:"when"`
	TTY     string `json:"tty"`
	From    string `json:"from"`
	Unknown bool   `json:"unknown,omitempty"`
}

// ptr is &v.
func ptr[T any](v T) *T { return &v }

// nonEmpty is &s, or nil for "".
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// detailsOf maps a report to the document's field and its findings, one for
// each section with anything not read, in P-17's order, its source each thing
// not read. The detail is decided (gallery-system-details.md §6). It is pure.
func detailsOf(r details.Report) (*Details, []Finding) {
	d := &Details{Kernel: nonEmpty(r.OS.Kernel)}
	if r.OS.Uptime.Known {
		d.Uptime = ptr(FormatUptime(r.OS.Uptime.Duration))
	}
	if r.Host.Addresses != nil {
		d.Addresses = []DetailAddress{}
		for _, a := range r.Host.Addresses {
			d.Addresses = append(d.Addresses, DetailAddress{Interface: a.Interface, Family: a.Family, Address: a.Address})
		}
	}
	if r.Users.List != nil {
		d.Users = []DetailUser{}
		for _, u := range r.Users.List {
			d.Users = append(d.Users, detailUser(u))
		}
	}
	d.CPU = detailCPU(r.CPU)
	d.Disk = detailDisk(r.Disk)
	if m := r.Memory; m.Total > 0 {
		d.Memory = &DetailMemory{Total: m.Total}
		if used, ok := m.Used(); ok {
			d.Memory.Used = &used
		}
	}
	if r.Logins.State == details.LoginsRead {
		d.Logins = []DetailLogin{}
		for _, l := range r.Logins.List {
			d.Logins = append(d.Logins, DetailLogin{User: l.User, When: l.When, TTY: l.TTY, From: l.From, Unknown: l.State == details.LoginUnknown})
		}
	}
	var fs []Finding
	for _, s := range []struct {
		name   string
		unread []details.Unread
	}{
		{"os and kernel", r.OS.Unread},
		{"host and ips", r.Host.Unread},
		{"users", r.Users.Unread},
		{"cpu and load", r.CPU.Unread},
		{"disk", r.Disk.Unread},
		{"memory", r.Memory.Unread},
		{"last login", r.Logins.Unread},
	} {
		if len(s.unread) == 0 {
			continue
		}
		var sources []string
		for _, u := range s.unread {
			sources = append(sources, u.Source)
		}
		fs = append(fs, Finding{Code: CodeDetailsUnreadable, Detail: s.name + " not read", Source: strings.Join(sources, ", ")})
	}
	return d, fs
}

func detailUser(u details.User) DetailUser {
	tool, t := u.Admin()
	if t == detect.Unknown {
		tool = "unknown"
	}
	return DetailUser{Name: u.Name, Tool: tool}
}

// detailCPU is nil where neither of its files was read.
func detailCPU(c details.CPU) *DetailCPU {
	if c.CPUs == 0 && c.Load[0] == "" {
		return nil
	}
	d := &DetailCPU{Model: nonEmpty(c.Model)}
	if c.CPUs > 0 {
		d.CPUs = &c.CPUs
	}
	if c.Sockets > 0 {
		d.Sockets = &c.Sockets
	}
	if c.Load[0] != "" {
		d.Load = &c.Load
	}
	return d
}

// detailDisk is nil where neither the mount nor the sizes were read.
func detailDisk(s details.Disk) *DetailDisk {
	if s.Source == "" && !s.Known {
		return nil
	}
	d := &DetailDisk{Source: nonEmpty(s.Source), Type: nonEmpty(s.Type), Pool: s.Pool, Health: s.Health}
	if s.Known {
		d.Used, d.Total = &s.Used, &s.Total
	}
	if s.Pool != "" && s.Health == "" {
		d.Health = "unknown"
	}
	return d
}
