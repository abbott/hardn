# AppArmor check fixtures (synthetic)

Each directory holds one platform's `/sys/kernel/security/apparmor/profiles`
with AppArmor on, in the kernel's form (one line per loaded profile,
`name (mode)`, or `:ns:name (mode)` for a namespace below root's; linux
v6.18 `security/apparmor/apparmorfs.c`, `label.c`). The names are written
from the vendors' packages and are not captured from a host; the counts are
illustrative, not each release's stock count:

- `debian13`: Debian 13, 7 profiles enforcing;
- `ubuntu2604`: Ubuntu 26.04, 15 enforcing, with snapd's, one in complain
  mode and four in unconfined mode;
- `pve9`: Proxmox VE 9 with three containers running, 16 of the host's
  enforcing: LXC's `lxc-container-default*` profiles and each running
  container's generated `lxc-<id>_</var/lib/lxc>`, beside one profile of a
  container's own namespace, which the check counts apart;
- `alpine324`: Alpine 3.24 with AppArmor turned on by hand, 2 enforcing and
  one in complain mode.

The tests load the file into `hostfake` with the kernel's parameters
(`/sys/module/apparmor/parameters/enabled` and `mode`), `/proc/cmdline`,
OpenRC's runlevels, and `systemctl is-enabled apparmor.service`'s answer,
as each case says. Synthetic values only.
