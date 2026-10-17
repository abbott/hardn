package alpine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// AppArmor paths: the kernel's parameter reads Y when AppArmor is enabled
// and N when it is built in but not selected at boot; with no AppArmor in
// the kernel there is no /sys/module/apparmor.
const (
	AppArmorEnabled = "/sys/module/apparmor/parameters/enabled"
	SysModule       = "/sys/module"
)

// Bootloader is where the host's kernel command line is set, which is
// where enabling AppArmor starts. The zero value is Unknown.
type Bootloader uint8

const (
	BootUnknown  Bootloader = iota
	BootExtlinux            // syslinux, as setup-disk installs it
	BootGRUB
	BootLimine
	BootEFIStub // a unified kernel image (secureboot-hook), or an EFI stub boot entry
	BootRPi     // Raspberry Pi firmware
)

func (b Bootloader) String() string {
	return word(uint8(b), "unknown", "extlinux", "grub", "limine", "efi-stub", "raspberry-pi")
}

// bootFiles are the files setup-disk (or secureboot-hook) writes for each
// bootloader, in Bootloader order. Finding one is a hint, never proof.
var bootFiles = []struct {
	b    Bootloader
	path string
	how  string
}{
	{BootExtlinux, "/etc/update-extlinux.conf", "add it to default_kernel_opts in /etc/update-extlinux.conf, then run update-extlinux"},
	{BootGRUB, "/etc/default/grub", "add it to GRUB_CMDLINE_LINUX_DEFAULT in /etc/default/grub, then run grub-mkconfig -o /boot/grub/grub.cfg"},
	{BootLimine, "/boot/limine/limine.conf", "add it to the kernel_cmdline: line of the Alpine entry in /boot/limine/limine.conf"},
	{BootEFIStub, "/etc/kernel-hooks.d/secureboot.conf", "for a unified kernel image, add it to cmdline= in /etc/kernel-hooks.d/secureboot.conf, which the next kernel install builds in, or, for a plain EFI stub boot entry, to the entry's arguments (efibootmgr)"},
	{BootRPi, "/boot/cmdline.txt", "add it to the one line of cmdline.txt on the boot partition (/boot/cmdline.txt)"},
}

// AppArmorNote says what enabling AppArmor on Alpine takes, which hardn
// does not automate (brief §2): the package, its service, the kernel
// parameter, set where the bootloader takes it (see BootloaderNote), and
// a reboot.
const AppArmorNote = "AppArmor is not enabled. hardn does not enable it on Alpine: it needs the apparmor package, " +
	"rc-update add apparmor boot, the kernel parameter lsm=<the modules /sys/kernel/security/lsm lists, " +
	"without capability>,apparmor (or apparmor=1 security=apparmor), and a reboot."

// BootloaderNote says where the kernel parameter goes for b; for
// BootUnknown, for every bootloader.
func BootloaderNote(b Bootloader) string {
	var out []string
	for _, f := range bootFiles {
		if b == BootUnknown || f.b == b {
			out = append(out, f.b.String()+": "+f.how)
		}
	}
	return strings.Join(out, "; ")
}

// AppArmor is what CheckAppArmor found.
type AppArmor struct {
	Status
	Enabled detect.Tri
	// Bootloaders are those whose files were found; Note names one only
	// when exactly one was.
	Bootloaders []Bootloader
	// Note, when Enabled is No: AppArmorNote, then BootloaderNote.
	Note string
}

// CheckAppArmor reports AppArmor on Alpine, and only reads: NotApplicable
// in a container (the host's kernel decides); Configured when the kernel
// says AppArmor is enabled; otherwise NotApplicable, with a Note
// (AppArmorNote and the per-bootloader note), since enabling it needs a reboot and is never
// automated (brief §2, 2.5). Unknown when the container check or the
// kernel's answer is not established. Its error is ErrNotAlpine (another
// unit's), ErrUnknown for an unestablished distribution, or ctx's.
func CheckAppArmor(ctx context.Context, sys host.System, f detect.Facts) (AppArmor, error) {
	if err := alpineOnly(f); err != nil {
		return AppArmor{Status: Status{Err: err}}, err
	}
	var a AppArmor
	switch f.Container.Detected {
	case detect.Yes:
		a.State, a.Detail = StateNotApplicable, "in a container ("+f.Container.Signal+"), AppArmor is the host's"
		return a, nil
	case detect.Unknown:
		a.Err = fmt.Errorf("alpine: apparmor: container: %w: %w", ErrUnknown, errOr(f.Container.Err, detect.ErrNoSignal))
		return a, nil
	}
	data, err := sys.ReadFile(ctx, AppArmorEnabled)
	switch {
	case err == nil && string(data) == "Y\n":
		a.Enabled, a.State, a.Detail = detect.Yes, StateConfigured, "enabled"
	case err == nil && string(data) == "N\n":
		a.Enabled, a.Detail = detect.No, "built into the kernel, not enabled at boot"
	case err == nil:
		a.Err = fmt.Errorf("alpine: apparmor: %s: %q: %w", AppArmorEnabled, data, detect.ErrUnrecognised)
	case errors.Is(err, fs.ErrNotExist):
		if _, serr := sys.Stat(ctx, SysModule); serr != nil {
			a.Err = fmt.Errorf("alpine: apparmor: %w", serr)
			break
		}
		a.Enabled, a.Detail = detect.No, "not in the running kernel (no "+SysModule+"/apparmor): a kernel built with it comes first"
	default:
		a.Err = fmt.Errorf("alpine: apparmor: %w", err)
	}
	if err == nil {
		a.Evidence = []string{fmt.Sprintf("%s: %q", AppArmorEnabled, data)}
	}
	if a.Enabled != detect.No {
		if err := ctx.Err(); err != nil {
			return a, fmt.Errorf("alpine: apparmor: %w", err)
		}
		return a, nil
	}
	a.State = StateNotApplicable
	for _, bf := range bootFiles {
		if _, err := sys.Lstat(ctx, bf.path); err == nil {
			a.Bootloaders = append(a.Bootloaders, bf.b)
			a.Evidence = append(a.Evidence, bf.path+": present")
		}
	}
	b := BootUnknown
	if len(a.Bootloaders) == 1 {
		b = a.Bootloaders[0]
	}
	a.Note = AppArmorNote + " " + BootloaderNote(b) + "."
	if err := ctx.Err(); err != nil {
		return a, fmt.Errorf("alpine: apparmor: %w", err)
	}
	return a, nil
}
