# hardn report

Generated 2026-10-20 12:30 UTC by hardn 0.4.0.

> Not root: what only root can read shows Unknown.

## Host

- Hostname: Not read
- OS: Unknown
- Platform: Unknown
- Support: Unknown
- Kernel: Not read
- Uptime: Not read
- Time service: Unknown
- Clock: Not read
- Init: Unknown
- sshd started by: Unknown
- Container: Unknown
- Proxmox VE: Unknown
- sudo: Unknown
- doas: Unknown

## Risk level

**Unknown**

- User: Counted as not configured
- SSH Login: Counted as not configured
- SSH Auth: Counted as not configured
- SSH Port: Counted as not configured
- Firewall: Counted as not configured
- Auto Updates: Counted as not configured
- AppArmor: Counted as not configured
- Host: Can't tell if reboot pending
- Auto Updates: Uptime unknown

## Status

| Control | State | Detail |
|---|---|---|
| User | Unknown |  |
| SSH Login | Unknown |  |
| SSH Auth | Unknown |  |
| SSH Port | Unknown |  |
| Firewall | Unknown |  |
| DNS | Unknown |  |
| Auto Updates | Unknown |  |
| AppArmor | Unknown |  |

## Findings

| Code | Finding | Source | Control |
|---|---|---|---|
| `DETAILS_UNREADABLE` | os and kernel not read | `/proc/sys/kernel/osrelease` |  |
| `DETAILS_UNREADABLE` | host and ips not read | `ip addr` |  |
| `DETAILS_UNREADABLE` | users not read | `/etc/passwd` |  |
| `DETAILS_UNREADABLE` | cpu and load not read | `/proc/cpuinfo` |  |
| `DETAILS_UNREADABLE` | disk not read | `df` |  |
| `DETAILS_UNREADABLE` | memory not read | `/proc/meminfo` |  |
| `DETAILS_UNREADABLE` | last login not read | `lslogins` |  |

## Errors

| Code | Error | Control |
|---|---|---|
| `UNCLASSIFIED_ERROR` | sshd -T: permission denied | SSH Port |

## Addresses

Not read

## Users

Not read

## CPU and load

Not read

## Disk

Not read

## Memory

Not read

## Last login

Not read
