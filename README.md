# Hardn

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](docs/verify.md#verify-a-release) [![Signed with Sigstore](https://img.shields.io/badge/Signed%20with-Sigstore-blue)](docs/verify.md#verify-a-release) [![zizmor](https://img.shields.io/badge/%F0%9F%8C%88-zizmor-white?labelColor=white)](https://zizmor.sh/) [![Release](https://img.shields.io/github/v/release/abbott/hardn)](https://github.com/abbott/hardn/releases/latest) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/abbott/hardn/ci.yml) [![License: AGPL v3](https://img.shields.io/badge/License-AGPL_3-blue.svg)](LICENSE)

Simple, precise hardening for Linux servers.
> For Debian, Ubuntu, Proxmox, Alpine, Rocky, Alma and RHEL. See each change before it happens. Revert if needed.

For anyone administering a Linux server, container or virtual machine without a security team: homelab enthusiasts, students, and small organizations such as schools, clinics and nonprofits.

<p align="center">
<img width="1327" height="1803" alt="hardn v0.4.0" src="https://github.com/user-attachments/assets/e7b53b65-47f7-4283-aeda-1c3ec7b3d352" />
</p>

## What It Does

`sudo hardn` displays a [**risk level**](docs/engine/risk.md) for the host's users, SSH, firewall, DNS, automatic updates and AppArmor, and a menu to configure them:

| Option | Configures |
|---|---|
| 1) User Management | users, admin rights, SSH keys |
| 2) SSH Login | root login, password login, the port |
| 3) Firewall | `ufw` or firewalld, with SSH let through |
| 4) DNS | the nameservers |
| 5) Auto Updates | the distribution's automatic updates |
| 6) Run All | options 1 to 5, from `hardn.yml` |
| 7) Backup | backups of the files hardn changes |
| 8) Configuration | nothing: the `hardn.yml` it read |
| 9) System Details | nothing: a report on the host |
| 10) Logs | nothing: `/var/log/hardn.log` |

AppArmor is displayed, not configured. What each option shows and changes: [docs/usage.md](docs/usage.md#the-menu).

> [!IMPORTANT]
> `hardn` should be part of a broader security strategy, not a _"set it and forget it"_ solution — regular security audits, updates, and monitoring are still required.

## How It Works

1. **Plan.** hardn reads the host and shows what will change, with diffs. Some changes are refused at this point, with the reason and what to do (see [safeguards](docs/safeguards.md)).
2. **Review.** A change that can't lock you out asks y/N. A change that could lock you out (SSH Login, enabling the firewall, some user changes, Run All) asks you to type the hostname.
3. **Apply.** The files a change touches are backed up first (with backups off, only those a rollback needs). A step that fails rolls the whole change back.
4. **Confirm or revert.** After a lockout-risk change, a timer starts (3 minutes by default). Open a new SSH connection from another machine, to the new port if the change moved it, and run `sudo hardn confirm`. If nobody confirms in time, the change is rolled back. `sudo hardn rollback <id>` undoes it sooner.

`sudo hardn -n` is a dry run: it shows each plan and changes nothing. What hardn refuses, the revert timer and backups: [docs/safeguards.md](docs/safeguards.md).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```
_When a host uses `doas` (Alpine), pipe to `doas sh` instead of `sudo sh`._

The script installs the latest release as `/usr/local/bin/hardn`, after checking its SHA-256 against the release's `SHA256SUMS`, and its attestation too when gh 2.68.0 or later is installed and signed in. The SHA-256 shows the download matches the release; only the attestation shows the release was built by hardn's workflow from its tag. To update, run it again.

Pinning a version, installing by hand, building from source and troubleshooting: [docs/install.md](docs/install.md). Checking a release yourself, offline too: [docs/verify.md](docs/verify.md).

## Quick Start

`hardn` needs root (e.g., `sudo hardn` or `doas hardn`), except `--version`, `--help`, `--print-logs` on its own, `completion`, `config`, and `system-details`.

```sh
hardn -h                        # help: usage flags and commands
hardn -h config                 # help for `config` command
hardn config                    # show hardn.yml

# output system details report as JSON file
hardn system-details --output json > web-01.json

sudo hardn                      # no flag: opens the main menu
sudo hardn -n -r                # show Run All's plan
```
> [!TIP]
> See [docs/usage.md](docs/usage.md) for every flag and command, scripts and cron, exit statuses, JSON and logs. 

### hardn.yml
See [docs/configuration.md](docs/configuration.md) for every `hardn.yml` key.

## Supported Distributions

| Distribution | Supported | Best effort | Out of scope |
|---|---|---|---|
| Debian | 13, 12 | | |
| Ubuntu | 26.04, 24.04 | 22.04 | interim (non-LTS) releases |
| Alpine | 3.24, 3.23 | 3.22 | 3.21 (vendor support ended 2026-11-01), 3.20 (2026-04-01) |
| Proxmox | 9 | | 8 (end of life since 2026-08-31) |
| Rocky | 10, 9 | | |
| Alma | 10, 9 | | |
| RHEL | 10, 9 | | |

- **Supported**: hardn changes it.
- **Best effort**: the vendor still supports it. hardn changes it, but makes no promises, and every plan carries a warning (`PLATFORM_BEST_EFFORT`).
- **Out of scope**, and any version not in the table: hardn shows the screens and refuses every change.
- **Any other distribution**: hardn doesn't start.
- **Tested**: CI runs the tests on the container image of each Supported release but Proxmox (Debian, Ubuntu, Alpine, Rocky, Alma, and Red Hat's UBI for RHEL), and a release waits for them. Proxmox VE has no image: its support is tested against synthetic Proxmox VE 9 files. Since v0.6.0, the release notes name the real hosts a release was tested on, or say that it changed only wording.

What differs on Alpine, on Rocky, Alma and RHEL, and on Proxmox: [docs/platforms.md](docs/platforms.md).

## Security

- **No telemetry.** `hardn` never phones home, and needs no account or cloud service.
- **Releases.** Each binary has [SLSA](https://slsa.dev) Build Level 3 provenance and a [Sigstore](https://www.sigstore.dev/) signature, and a rebuild of a tag is byte-identical: [docs/verify.md](docs/verify.md).
- **CVEs.** hardn checks for the vendor's fix to a CVE that makes one of its changes unsafe, today one on Alpine: [docs/cve.md](docs/cve.md).
- **Reporting.** Report security problems as [.github/SECURITY.md](.github/SECURITY.md) says, **<ins>not in a public issue</ins>**.

## Documentation

| Page | What it covers |
|---|---|
| [docs/usage.md](docs/usage.md) | the menu, flags, commands, exit statuses, output, logs |
| [docs/configuration.md](docs/configuration.md) | `hardn.yml` and `HARDN_CONFIG` |
| [docs/safeguards.md](docs/safeguards.md) | refusals, the revert timer, backups |
| [docs/install.md](docs/install.md) | install by hand, build from source, troubleshooting |
| [docs/verify.md](docs/verify.md) | release provenance and how to check it |
| [docs/platforms.md](docs/platforms.md) | Alpine, Rocky, Alma, RHEL and Proxmox |
| [docs/cve.md](docs/cve.md) | the CVEs hardn checks for |
| [docs/reference/output.md](docs/reference/output.md) | the JSON document and its codes |
| [docs/roadmap.md](docs/roadmap.md) | planned features |
| [docs/releases/v0.8.0.md](docs/releases/v0.8.0.md) | the release notes |

## Contributing

Read [docs/contributing.md](docs/contributing.md) before opening a pull request, and [.github/GOVERNANCE.md](.github/GOVERNANCE.md) for the project's mission. Report bugs in the GitHub issue tracker, with your OS and version and the steps to reproduce.

Schools, clinics and nonprofits get direct help: [.github/SUPPORT.md](.github/SUPPORT.md#help-for-schools-clinics-and-nonprofits).

## Origin

After hardening Debian-based containers and VMs by hand for years, I wrote a script to automate the essentials from a config file and command-line arguments. The code needed a rewrite to stay maintainable, so I adopted Go and published `hardn`. Enjoy! :tumbler_glass:

Built with the libraries in [docs/acknowledgments.md](docs/acknowledgments.md).

## License

Copyright © 2025-2026 Bruce Abbott. GNU AGPL v3: see [LICENSE](LICENSE).

hardn comes with no warranty, and its authors accept no liability for its use: see sections 15 and 16 of the license.

As an end-user, you can use hardn freely. The license’s terms apply when you pass hardn on to others, modified or otherwise, or offer a modified version as a service.
