package alpine_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestCheckAppArmor: NotApplicable in a container and whenever AppArmor
// is not enabled, with the note for the bootloader found (all of them
// when none or several are); Configured when enabled; Unknown when the
// container check or the kernel's answer is not established. It only
// reads.
func TestCheckAppArmor(t *testing.T) {
	const kernelOff = "N\n" // Alpine's linux-lts: built in, not in CONFIG_LSM
	for _, tc := range []struct {
		name      string
		container detect.Tri
		param     string // "" absent; "-" absent, with /sys/module/apparmor there
		module    bool   // /sys/module exists
		boot      []string
		state     alpine.State
		enabled   detect.Tri
		note      []string // in the note
		notNote   []string // not in it
	}{
		{"enabled", detect.No, "Y\n", true, nil, alpine.StateConfigured, detect.Yes, nil, nil},
		{"extlinux", detect.No, kernelOff, true, []string{"/etc/update-extlinux.conf"}, alpine.StateNotApplicable, detect.No,
			[]string{"apparmor package", "reboot", "update-extlinux"}, []string{"grub-mkconfig", "cmdline.txt"}},
		{"grub", detect.No, kernelOff, true, []string{"/etc/default/grub"}, alpine.StateNotApplicable, detect.No,
			[]string{"GRUB_CMDLINE_LINUX_DEFAULT", "grub-mkconfig"}, []string{"update-extlinux"}},
		{"limine", detect.No, kernelOff, true, []string{"/boot/limine/limine.conf"}, alpine.StateNotApplicable, detect.No, []string{"kernel_cmdline:"}, []string{"grub"}},
		{"uki", detect.No, kernelOff, true, []string{"/etc/kernel-hooks.d/secureboot.conf"}, alpine.StateNotApplicable, detect.No, []string{"cmdline=", "efibootmgr"}, []string{"grub"}},
		{"raspberry pi", detect.No, kernelOff, true, []string{"/boot/cmdline.txt"}, alpine.StateNotApplicable, detect.No, []string{"cmdline.txt"}, []string{"grub"}},
		{"none found", detect.No, kernelOff, true, nil, alpine.StateNotApplicable, detect.No,
			[]string{"update-extlinux", "grub-mkconfig", "kernel_cmdline:", "efibootmgr", "cmdline.txt"}, nil},
		{"two found", detect.No, kernelOff, true, []string{"/etc/update-extlinux.conf", "/etc/default/grub"}, alpine.StateNotApplicable, detect.No,
			[]string{"update-extlinux", "grub-mkconfig", "cmdline.txt"}, nil},
		{"no apparmor in the kernel", detect.No, "", true, nil, alpine.StateNotApplicable, detect.No, []string{"reboot"}, nil},
		{"no /sys", detect.No, "", false, nil, alpine.StateUnknown, detect.Unknown, nil, nil},
		{"module without its parameter", detect.No, "-", true, nil, alpine.StateUnknown, detect.Unknown, nil, nil},
		{"unrecognised", detect.No, "1\n", true, nil, alpine.StateUnknown, detect.Unknown, nil, nil},
		{"container", detect.Yes, "Y\n", true, nil, alpine.StateNotApplicable, detect.Unknown, nil, nil},
		{"container unknown", detect.Unknown, "Y\n", true, nil, alpine.StateUnknown, detect.Unknown, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			if tc.module {
				h.AddDir(alpine.SysModule, 0o755, 0, 0)
			}
			switch tc.param {
			case "":
			case "-":
				h.AddDir(alpine.SysModule+"/apparmor", 0o755, 0, 0)
			default:
				h.AddFile(alpine.AppArmorEnabled, []byte(tc.param), 0o444, 0, 0)
			}
			for _, b := range tc.boot {
				h.AddFile(b, []byte("# "+b+"\n"), 0o644, 0, 0)
			}
			f := facts(t, "3.24")
			f.Container = detect.Container{Detected: tc.container, Signal: "/run/.containerenv", Manager: "podman"}
			if tc.container == detect.Unknown {
				f.Container = detect.Container{Err: fs.ErrPermission}
			}
			a, err := alpine.CheckAppArmor(context.Background(), h, f)
			if err != nil {
				t.Fatal(err)
			}
			if a.State != tc.state || a.Enabled != tc.enabled {
				t.Errorf("State %s, Enabled %s; want %s, %s (%+v)", a.State, a.Enabled, tc.state, tc.enabled, a)
			}
			if (a.State == alpine.StateUnknown) != (a.Err != nil) {
				t.Errorf("Err = %v with State %s", a.Err, a.State)
			}
			if (a.Enabled == detect.No) != (a.Note != "") {
				t.Errorf("Note = %q with Enabled %s", a.Note, a.Enabled)
			}
			for _, s := range tc.note {
				if !strings.Contains(a.Note, s) {
					t.Errorf("Note lacks %q: %s", s, a.Note)
				}
			}
			for _, s := range tc.notNote {
				if strings.Contains(a.Note, s) {
					t.Errorf("Note has %q: %s", s, a.Note)
				}
			}
			for _, c := range h.Calls() {
				if c.Op != "read" && c.Op != "stat" && c.Op != "lstat" {
					t.Errorf("not a read: %s", c)
				}
				if tc.container != detect.No {
					t.Errorf("in a container, read %s", c)
				}
			}
		})
	}
}

// TestNotAlpine: AppArmor and Lynis on another distribution are another
// unit's (ErrNotAlpine); an unestablished distribution is Unknown, never
// "not Alpine". Lynis is NotApplicable on Alpine.
func TestNotAlpine(t *testing.T) {
	f := facts(t, "3.23")
	s, err := alpine.Lynis(f)
	if err != nil || s.State != alpine.StateNotApplicable || !strings.Contains(s.Detail, "not packaged") {
		t.Errorf("Lynis = %+v, %v", s, err)
	}
	for _, tc := range []struct {
		p    detect.Platform
		want error
	}{
		{detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04"}, alpine.ErrNotAlpine},
		{detect.Platform{Err: fs.ErrPermission}, alpine.ErrUnknown},
	} {
		f.Platform = tc.p
		h := hostfake.New(host.Options{})
		a, err := alpine.CheckAppArmor(context.Background(), h, f)
		if !errors.Is(err, tc.want) || a.State != alpine.StateUnknown || len(h.Calls()) != 0 {
			t.Errorf("%s: CheckAppArmor = %s, %v", tc.p.Distro, a.State, err)
		}
		if s, err := alpine.Lynis(f); !errors.Is(err, tc.want) || s.State != alpine.StateUnknown {
			t.Errorf("%s: Lynis = %s, %v", tc.p.Distro, s.State, err)
		}
	}
	if !errors.Is(func() error { _, err := alpine.Lynis(f); return err }(), fs.ErrPermission) {
		t.Error("Lynis: the cause is lost")
	}
}

// TestBootloaderNote: one bootloader's note names its file only; Unknown
// names every one.
func TestBootloaderNote(t *testing.T) {
	files := []string{"/etc/update-extlinux.conf", "/etc/default/grub", "/boot/limine/limine.conf", "/etc/kernel-hooks.d/secureboot.conf", "/boot/cmdline.txt"}
	for b := alpine.BootExtlinux; b <= alpine.BootRPi; b++ {
		n := alpine.BootloaderNote(b)
		for i, f := range files {
			if strings.Contains(n, f) != (i == int(b)-1) {
				t.Errorf("%s: note %q and %s", b, n, f)
			}
		}
	}
	all := alpine.BootloaderNote(alpine.BootUnknown)
	if slices.ContainsFunc(files, func(f string) bool { return !strings.Contains(all, f) }) || strings.Count(all, "; ") != len(files)-1 {
		t.Errorf("BootUnknown note = %q", all)
	}
}
