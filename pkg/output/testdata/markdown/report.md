# hardn report: `web-01`

Generated 2026-10-20 12:30 UTC by hardn 0.4.0.

## Host

- Hostname: `web-01`
- OS: `Debian GNU/Linux 13 (trixie)`
- Platform: debian 13
- Support: Supported
- Kernel: `6.12.113+deb13-cloud-amd64`
- Uptime: 182 days, 2 hours, 26 minutes
- Time service: chrony
- Clock: In sync
- Synced to: `2.ntp.ubuntu.com`
- Init: systemd
- sshd started by: Unknown
- Container: Unknown
- Proxmox VE: No
- sudo: Yes (sudo)
- doas: No

## Risk level

**High**: weak security

- User: Second root account
- Host: Reboot pending
- Auto Updates: Up 182 days, no updates

## Status

| Control | State | Detail |
|---|---|---|
| User | Configured | non-root, sudo |
| SSH Login | Configured | root disallowed |
| SSH Auth | Configured | key-only auth |
| SSH Port | Configured | non-default (2230) |
| Firewall | Configured | deny policy |
| Kernel | Configured | 6 items applied |
| Auto Updates | Not Configured | no unattended-upgrades |
| AppArmor | Not Configured | no profiles loaded |
| DNS | Configured | 192.0.2.54, 2001:db8::54 |

### Evidence

- SSH Port: `sshd -T: port 2230`
- Firewall: `ufw status verbose: Default: deny (incoming)`
- Firewall: `ufw status verbose: 2230/tcp ALLOW IN Anywhere`

## Findings

| Code | Finding | Source | Control |
|---|---|---|---|
| `USERS_UID0` | synthetic |  | User |
| `REBOOT_REQUIRED` | package request | `/var/run/reboot-required` |  |
| `UPTIME_NO_AUTO_UPDATES` | up 182 days, no auto updates | `/proc/uptime` |  |
| `CONFIG_DEPRECATED_VALUE` | permitRootLogin: false (line 12) is deprecated; write "no", which means the same | `/etc/hardn/hardn.yml:12` |  |

## Addresses

- `eth0`: `192.0.2.20/24`
- `eth0`: `2001:db8::20/64`

## Users

| User | Admin rights |
|---|---|
| `alice` | sudo |
| `svc` | None |

## CPU and load

- Model: `QEMU Virtual CPU version 2.5+`
- CPUs: 4, 1 socket
- Load (1, 5, 15 min): 0.12, 0.08, 0.03

## Disk

- `/`: `/dev/vda1` (ext4)
- Used: 4.2 of 38.6 GiB (11%)

## Memory

- Used: 1.2 of 3.8 GiB (32%)

## Last login

| User | When | From |
|---|---|---|
| `root` | None recorded |  |
| `alice` | 2026-10-20T11:42:05+00:00 | `192.0.2.10` |
| `svc` | Not known |  |
