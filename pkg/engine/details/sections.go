package details

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The kernel's files the report reads beside the header probe's
// (proc_cpuinfo(5), proc_loadavg(5), proc_meminfo(5), proc_pid_status(5)).
const (
	CPUInfoFile    = "/proc/cpuinfo"
	LoadAvgFile    = "/proc/loadavg"
	MemInfoFile    = "/proc/meminfo"
	SelfStatusFile = "/proc/self/status"
)

// OSKernel is the OS and Kernel section.
type OSKernel struct {
	OS     header.OS     // the header probe's: Name "" when not read
	Kernel string        // "" when not read
	Uptime header.Uptime // the header probe's: Known false when not read
	Unread []Unread
}

func readOS(ctx context.Context, sys host.System, hd header.Header) OSKernel {
	s := OSKernel{OS: hd.OS, Uptime: hd.Uptime}
	if err := hd.OS.Err; err != nil {
		s.Unread = append(s.Unread, Unread{detect.EtcOSRelease, err})
	}
	data, err := sys.ReadFile(ctx, header.KernelReleaseFile)
	if err == nil {
		if s.Kernel = strings.TrimSpace(string(data)); s.Kernel == "" {
			err = fmt.Errorf("details: %s is empty: %w", header.KernelReleaseFile, detect.ErrUnrecognised)
		}
	}
	if err != nil {
		s.Unread = append(s.Unread, Unread{header.KernelReleaseFile, err})
	}
	if err := hd.Uptime.Err; err != nil {
		s.Unread = append(s.Unread, Unread{header.UptimeFile, err})
	}
	return s
}

// HostIPs is the Host and IPs section. Addresses is nil when ip was not read,
// and empty, not nil, when it answered none.
type HostIPs struct {
	Hostname  string // the header probe's: "" when not read
	Addresses []Address
	Unread    []Unread
}

// Address is one global address, as ip printed it.
type Address struct {
	Interface string
	Family    string // inet, inet6
	Address   string // with its prefix length: 192.0.2.20/24
}

func readHost(ctx context.Context, sys host.System, hd header.Header) HostIPs {
	s := HostIPs{Hostname: hd.Hostname.Name}
	if err := hd.Hostname.Err; err != nil {
		s.Unread = append(s.Unread, Unread{header.HostnameFile, err})
	}
	res, err := sys.Run(ctx, "ip", "-o", "addr", "show", "scope", "global")
	if err != nil {
		s.Unread = append(s.Unread, Unread{SourceIP, err})
		return s
	}
	s.Addresses = []Address{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		a, ok := parseAddress(line)
		if !ok {
			// Kept, and said once: a line hardn cannot read may be the
			// address the person is looking for.
			if len(s.Unread) == 0 || s.Unread[len(s.Unread)-1].Source != SourceIP {
				s.Unread = append(s.Unread, Unread{SourceIP, fmt.Errorf("details: %q: %w", line, detect.ErrUnrecognised)})
			}
			continue
		}
		s.Addresses = append(s.Addresses, a)
	}
	return s
}

// parseAddress reads one line of `ip -o addr`: "2: eth0    inet 192.0.2.20/24
// metric 100 brd ..." (vm-runs/s1/01-tools.log). Debian's and Ubuntu's carry
// "metric 100", Alpine's "noprefixroute" after the scope: all that follows the
// address is passed over. inet6's line is the same [I: no VM has a global one],
// and an address with no prefix, a peer's form, is taken as written [I].
func parseAddress(line string) (Address, bool) {
	f := strings.Fields(line)
	if len(f) < 4 || !strings.HasSuffix(f[0], ":") || f[2] != "inet" && f[2] != "inet6" {
		return Address{}, false
	}
	addr := f[3]
	if _, err := netip.ParsePrefix(addr); err != nil {
		if _, err := netip.ParseAddr(addr); err != nil {
			return Address{}, false
		}
	}
	return Address{Interface: f[1], Family: f[2], Address: addr}, true
}

// Users is the Users section. List is nil when the account files were not read,
// and empty, not nil, when no account other than root is listed.
type Users struct {
	List []User
	// Root is whether hardn's effective uid is 0, by /proc/self/status's Uid
	// line, read on the seam; Unknown where that was not read. It says how a rule
	// file not read reads: "Not root: <file> not read", a user other than root
	// being unable to read them, where the same refusal to root is another
	// matter. /etc/sudoers.d is drwxr-x--- on all three VMs, so it cannot even be
	// listed, and Alpine's /etc/doas.conf is 0640 (vm-runs/s1/01-tools.log).
	Root detect.Tri
	// Tool names the tool a user's No is said of, as "No sudo" or "No doas":
	// doas where detect found doas and not sudo, else sudo. A No means no tool
	// grants it, so this is the word's choice and not a further fact.
	Tool   string
	Unread []Unread
}

// NotRead are the rule files and directories refused, as Unread names them.
func (u Users) NotRead() []string {
	var out []string
	for _, r := range u.Unread {
		if errors.Is(r.Err, fs.ErrPermission) {
			out = append(out, r.Source)
		}
	}
	return out
}

// User is one listed account and what each elevation tool decides for it.
type User struct {
	Name   string
	Grants []users.Grant // one per tool, sudo then doas; nil where no tool was read
}

// Admin is what lets u administer: the first tool that grants it, Yes; No where
// every tool is established not to; Unknown where none grants it and one is not
// established, as the users check's elevates has it. A group is not asked: it
// says nothing that the rules do not, and as a user other than root the rules
// are not read.
func (u User) Admin() (tool string, t detect.Tri) {
	t = detect.No
	for _, g := range u.Grants {
		switch g.Grants {
		case detect.Yes:
			return g.Tool, detect.Yes
		case detect.Unknown:
			t = detect.Unknown
		}
	}
	return "", t
}

// nobodies are the accounts of uid 1000 and up that no one is, by name, as
// origin/main has them (user_info.go:43-61).
var nobodies = []string{"nobody", "nfsnobody"}

func readUsers(ctx context.Context, sys host.System, f detect.Facts) Users {
	s := Users{Tool: sudoers.ToolSudo.String()}
	if f.Elevation.Doas.Present == detect.Yes && f.Elevation.Sudo.Present != detect.Yes {
		s.Tool = sudoers.ToolDoas.String()
	}
	s.Root = readRoot(ctx, sys)
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		src := accounts.PasswdFile
		var pe *fs.PathError
		if errors.As(err, &pe) {
			src = pe.Path
		}
		s.Unread = append(s.Unread, Unread{src, err})
		return s
	}
	s.List = []User{}
	var listed []accounts.User
	seen := map[string]bool{}
	for _, u := range db.Users {
		first := !seen[u.Name]
		seen[u.Name] = true
		if first && u.UID >= users.MinUID && !slices.Contains(nobodies, u.Name) {
			listed = append(listed, u)
		}
	}
	if len(listed) == 0 { // root only needs no rule read, nor visudo run
		return s
	}
	e := users.ReadElevation(ctx, sys, f)
	for _, n := range e.NotRead() {
		s.Unread = append(s.Unread, ruleSources(ctx, sys, n)...)
	}
	for _, u := range listed {
		s.List = append(s.List, User{Name: u.Name, Grants: e.GrantsOf(db, u)})
	}
	return s
}

// readRoot reads the effective uid, the second of Uid's four numbers
// (proc_pid_status(5)). No VM printed the file [I].
func readRoot(ctx context.Context, sys host.System) detect.Tri {
	data, err := sys.ReadFile(ctx, SelfStatusFile)
	if err != nil {
		return detect.Unknown
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "Uid:" {
			continue
		}
		switch uid, err := strconv.ParseUint(f[2], 10, 32); {
		case err != nil:
		case uid == 0:
			return detect.Yes
		default:
			return detect.No
		}
	}
	return detect.Unknown
}

// ruleSources says what of a tool's rules was not read: the file the read
// stopped at, or the tool's name where no file is named; where that was a
// refusal, the other of its files where that refuses too, as the read stops at
// the first and both are the user's to know. doas's read lists /etc/doas.d first
// (sudoers' readDoas), so a refusal there is already the first.
func ruleSources(ctx context.Context, sys host.System, n users.NotRead) []Unread {
	first := n.Tool
	var ce *sudoers.CheckError
	var pe *fs.PathError
	switch {
	case errors.As(n.Err, &ce):
		first = ce.Path
	case errors.As(n.Err, &pe):
		first = pe.Path
	}
	out := []Unread{{first, n.Err}}
	if !errors.Is(n.Err, fs.ErrPermission) {
		return out
	}
	refused := func(src string, err error) {
		if errors.Is(err, fs.ErrPermission) && src != first {
			out = append(out, Unread{src, err})
		}
	}
	if n.Tool == sudoers.ToolDoas.String() {
		_, err := sys.ReadFile(ctx, sudoers.DoasConfig)
		refused(sudoers.DoasConfig, err)
	} else {
		_, err := sys.ReadDir(ctx, sudoers.DropInDir)
		refused(sudoers.DropInDir, err)
	}
	slices.SortFunc(out, func(a, b Unread) int { return strings.Compare(a.Source, b.Source) })
	return out
}

// CPU is the CPU and Load section.
type CPU struct {
	// Model is the first model name line's; "" where the file has none, as on
	// arm64 [I, not run], or it was not read.
	Model string
	// CPUs counts the processor lines, logical CPUs and not cores; 0 when not
	// read.
	CPUs int
	// Sockets counts the distinct physical id lines; 0 where the file has none:
	// no count is made up.
	Sockets int
	// Load is /proc/loadavg's 1, 5 and 15 minute figures, as written; "" when
	// not read.
	Load   [3]string
	Unread []Unread
}

func readCPU(ctx context.Context, sys host.System) CPU {
	var s CPU
	if err := s.readInfo(ctx, sys); err != nil {
		s.Unread = append(s.Unread, Unread{CPUInfoFile, err})
	}
	if err := s.readLoad(ctx, sys); err != nil {
		s.Unread = append(s.Unread, Unread{LoadAvgFile, err})
	}
	return s
}

// readInfo reads "processor\t: 0", "model name\t: ..." and "physical id\t: 0"
// (vm-runs/s1/01-tools.log). A file with no processor line is not in a form
// known.
func (s *CPU) readInfo(ctx context.Context, sys host.System) error {
	data, err := sys.ReadFile(ctx, CPUInfoFile)
	if err != nil {
		return err
	}
	sockets := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, ":")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case !ok:
		case k == "processor":
			s.CPUs++
		case k == "model name" && s.Model == "":
			s.Model = v
		case k == "physical id":
			sockets[v] = true
		}
	}
	s.Sockets = len(sockets)
	if s.CPUs == 0 {
		s.Model, s.Sockets = "", 0
		return fmt.Errorf("details: %s: no processor line: %w", CPUInfoFile, detect.ErrUnrecognised)
	}
	return nil
}

// readLoad reads "0.12 0.08 0.03 1/234 5678" (proc_loadavg(5); no VM printed the
// file [I]).
func (s *CPU) readLoad(ctx context.Context, sys host.System) error {
	data, err := sys.ReadFile(ctx, LoadAvgFile)
	if err != nil {
		return err
	}
	f := strings.Fields(string(data))
	if len(f) < 3 {
		return fmt.Errorf("details: %s: %q: %w", LoadAvgFile, data, detect.ErrUnrecognised)
	}
	for _, n := range f[:3] {
		if _, err := strconv.ParseFloat(n, 64); err != nil {
			return fmt.Errorf("details: %s: %q: %w", LoadAvgFile, data, detect.ErrUnrecognised)
		}
	}
	s.Load = [3]string{f[0], f[1], f[2]}
	return nil
}

// Disk is the Disk section: the root file system.
type Disk struct {
	// Source and Type are /proc/self/mounts' for /: the device, or on ZFS the
	// dataset; "" when not read.
	Source, Type string
	// Used and Total are in bytes, Known false when not read: df's, or on ZFS
	// the dataset's, Total being its used and available.
	Used, Total uint64
	Known       bool
	// Pool is the pool Source names, on ZFS alone. Health is what zpool says of
	// it; "" with a Pool is not known, as where no zpool is installed, as in an
	// LXC guest on ZFS [I, not run].
	Pool, Health string
	Unread       []Unread
}

func readDisk(ctx context.Context, sys host.System) Disk {
	var s Disk
	data, err := sys.ReadFile(ctx, detect.SelfMounts)
	if err == nil {
		// The last line for / is the one on top: a stacked mount's, rootfs's
		// before it (proc_pid_mounts(5) [I]).
		for _, m := range detect.ParseMounts(data) {
			if m.Dir == "/" {
				s.Source, s.Type = m.Source, m.Type
			}
		}
		if s.Source == "" {
			err = fmt.Errorf("details: %s: no line for /: %w", detect.SelfMounts, detect.ErrUnrecognised)
		}
	}
	if err != nil {
		s.Unread = append(s.Unread, Unread{detect.SelfMounts, err})
	}
	if s.Type == "zfs" && s.readZFS(ctx, sys) {
		return s
	}
	if err := s.readDF(ctx, sys); err != nil {
		s.Unread = append(s.Unread, Unread{SourceDF, err})
	}
	return s
}

// readZFS reads the pool's health and the dataset's sizes, and reports whether
// it has the sizes: the dataset is what the mount names, never one of hardn's
// (origin/main's hard-coded zroot/ROOT/os, gallery-system-details.md §2.5). A
// tool that is not installed is no failure, only a thing not known; one that
// fails is said, and df still gives the figures. Every form here is
// [unverified]: no ZFS VM has run it.
func (s *Disk) readZFS(ctx context.Context, sys host.System) bool {
	pool, _, _ := strings.Cut(s.Source, "/")
	if pool == "" || strings.HasPrefix(pool, "-") {
		s.Unread = append(s.Unread, Unread{detect.SelfMounts, fmt.Errorf("details: dataset %q: %w", s.Source, detect.ErrUnrecognised)})
		return false
	}
	s.Pool = pool
	res, err := sys.Run(ctx, "zpool", "list", "-H", "-o", "health", s.Pool)
	switch f := strings.Fields(string(res.Stdout)); {
	case errors.Is(err, host.ErrNotFound):
	case err != nil:
		s.Unread = append(s.Unread, Unread{SourceZpool, err})
	case len(f) != 1:
		s.Unread = append(s.Unread, Unread{SourceZpool, fmt.Errorf("details: %q: %w", res.Stdout, detect.ErrUnrecognised)})
	default:
		s.Health = f[0]
	}
	res, err = sys.Run(ctx, "zfs", "get", "-Hp", "-o", "value", "used,available", s.Source)
	switch {
	case errors.Is(err, host.ErrNotFound):
		return false
	case err != nil:
		s.Unread = append(s.Unread, Unread{SourceZFS, err})
		return false
	}
	f := strings.Fields(string(res.Stdout))
	if len(f) == 2 {
		used, err1 := strconv.ParseUint(f[0], 10, 64)
		avail, err2 := strconv.ParseUint(f[1], 10, 64)
		if err1 == nil && err2 == nil && avail <= math.MaxUint64-used {
			s.Used, s.Total, s.Known = used, used+avail, true
			return true
		}
	}
	s.Unread = append(s.Unread, Unread{SourceZFS, fmt.Errorf("details: %q: %w", res.Stdout, detect.ErrUnrecognised)})
	return false
}

// readDF reads df -kP /: its header (no VM printed one [I]) has no last field
// "/", and each real line does, "/dev/sda1 10113424 1409984 8242784 15% /", the
// figures in KiB (vm-runs/s1/01-tools.log, GNU's and BusyBox's).
func (s *Disk) readDF(ctx context.Context, sys host.System) error {
	res, err := sys.Run(ctx, "df", "-kP", "/")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[len(f)-1] != "/" {
			continue
		}
		total, ok1 := kib(f[len(f)-5])
		used, ok2 := kib(f[len(f)-4])
		if ok1 && ok2 {
			s.Used, s.Total, s.Known = used, total, true
			return nil
		}
	}
	return fmt.Errorf("details: df: %q: %w", res.Stdout, detect.ErrUnrecognised)
}

// kib is s, a count of KiB, in bytes.
func kib(s string) (uint64, bool) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n > math.MaxUint64/1024 {
		return 0, false
	}
	return n * 1024, true
}

// Memory is the Memory section, in bytes.
type Memory struct {
	Total uint64 // 0 when not read
	// Available is MemAvailable's; AvailableKnown is false where the kernel
	// prints none (Linux before 3.14 [I]), which is a fact and not a guess.
	Available      uint64
	AvailableKnown bool
	Unread         []Unread
}

// Used is what MemAvailable leaves of the total, so the kernel's caches are not
// counted (origin/main counts them: collectors.go:329); ok is false where
// MemAvailable is not known.
func (m Memory) Used() (used uint64, ok bool) {
	if !m.AvailableKnown {
		return 0, false
	}
	return m.Total - m.Available, true
}

// readMemory reads "MemTotal:        2021528 kB" and its MemAvailable line
// (vm-runs/s1/01-tools.log). A MemAvailable above the total is not established.
func readMemory(ctx context.Context, sys host.System) Memory {
	var s Memory
	data, err := sys.ReadFile(ctx, MemInfoFile)
	if err != nil {
		s.Unread = append(s.Unread, Unread{MemInfoFile, err})
		return s
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[2] != "kB" {
			continue
		}
		n, ok := kib(f[1])
		switch {
		case !ok:
		case f[0] == "MemTotal:":
			s.Total = n
		case f[0] == "MemAvailable:":
			s.Available, s.AvailableKnown = n, true
		}
	}
	if s.Total == 0 {
		s.Available, s.AvailableKnown = 0, false
		s.Unread = append(s.Unread, Unread{MemInfoFile, fmt.Errorf("details: %s: no MemTotal line: %w", MemInfoFile, detect.ErrUnrecognised)})
		return s
	}
	if s.AvailableKnown && s.Available > s.Total {
		s.Available, s.AvailableKnown = 0, false
	}
	return s
}
