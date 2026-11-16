# Hardn

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](docs/verify.md#verify-a-release) [![Signed with Sigstore](https://img.shields.io/badge/Signed%20with-Sigstore-blue)](docs/verify.md#verify-a-release) [![zizmor](https://img.shields.io/badge/%F0%9F%8C%88-zizmor-white?labelColor=white)](https://zizmor.sh/) [![Release](https://img.shields.io/github/v/release/abbott/hardn)](https://github.com/abbott/hardn/releases/latest) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/abbott/hardn/ci.yml) [![License: AGPL v3](https://img.shields.io/badge/License-AGPL_3-blue.svg)](LICENSE)

Simple, precise hardening for Linux servers.
> For Debian, Ubuntu, Proxmox, Alpine, Rocky, Alma, RHEL, CentOS Stream and Oracle Linux. See each change before it happens. Revert if needed.

For anyone administering a Linux server, container or virtual machine without a security team: homelab enthusiasts, students, and small organizations such as schools, clinics and nonprofits.

<img width="1328" height="1678" alt="hardn menu" src="https://github.com/user-attachments/assets/46b65159-b49e-43ea-a64e-fdb0ac53566e" />

## What It Does

`sudo hardn` displays a [**risk level**](docs/engine/risk.md) for the host's users, SSH, firewall, kernel settings, automatic updates and AppArmor, and a menu for each:

| # | Option | Description |
|---|---|---|
| 1 | User | Manage: users, admin rights, SSH keys; review accounts |
| 2 | SSH | Manage: root login, password login, the port; list SSH keys |
| 3 | Firewall | Manage: `ufw` or firewalld, with SSH let through; show listening ports |
| 4 | Kernel | Manage: network and kernel settings (sysctl) |
| 5 | Auto Updates | Manage: automatic updates |
| 6 | Run All | Queue: options 1 to 5, dns, ntp, syslog |
| 7 | Backup | Manage: backups of the files hardn changes |
| 8 | Configuration | Manage: `hardn.yml`, dns, ntp, syslog |
| 9 | System Details | Display: host details |
| 10 | Logs | Display: `/var/log/hardn.log` |


AppArmor is displayed, not configured. What each option shows and changes: [docs/usage.md](docs/usage.md#the-menu).

> [!IMPORTANT]
> `hardn` should be part of a broader security strategy, not a _"set it and forget it"_ solution — regular security audits, updates, and monitoring are still required.

## How It Works

1. **Plan.** hardn reads the host and shows what will change, with diffs. Some changes are refused at this point, with the reason and what to do (see [safeguards](docs/safeguards.md)).
2. **Review.** A change that can't lock you out asks y/N. A change that could lock you out (SSH, enabling the firewall, some user changes, Run All) asks you to type the hostname.
3. **Apply.** The files a change touches are backed up first (with backups off, only those a rollback needs). A step that fails rolls the whole change back.
4. **Confirm or revert.** After a lockout-risk change, a timer starts (3 minutes by default). Open a new SSH connection from another machine, to the new port if the change moved it, and run `sudo hardn confirm`. If nobody confirms in time, the change is rolled back. `sudo hardn rollback <id>` undoes it sooner.

`sudo hardn -n` is a dry run: it shows each plan and changes nothing. What hardn refuses, the revert timer and backups: [docs/safeguards.md](docs/safeguards.md).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```
_When a host uses `doas` (Alpine), pipe to `doas sh` instead of `sudo sh`._

The script installs the latest release as `/usr/local/bin/hardn`, after checking its SHA-256 against the release's `SHA256SUMS`, and its attestation too when gh 2.68.0 or later is installed and signed in. The SHA-256 shows the download matches the release; only the attestation shows the release was built by hardn's workflow from its tag. 

> [!NOTE]
> - To update, run it again.
> - Pinning a version, installing by hand, building from source and troubleshooting: [docs/install.md](docs/install.md). With a pinned version, the script also asks GitHub whether a newer release is out.
> - Checking a release yourself, offline too: [docs/verify.md](docs/verify.md).

## Quick Start

`hardn` needs root (e.g., `sudo hardn` or `doas hardn`), except `--version`, `--help`, `--print-logs`, `completion`, `config`, and `system-details`.

```sh
hardn -h                        # help: usage flags and commands
hardn -h config                 # help for `config` command
hardn config                    # show hardn.yml

# output system details report as JSON file
hardn system-details --output json > web-01.json

# output the report to hand over, as Markdown
hardn system-details --output markdown > web-01.md

sudo hardn                      # no flag: opens the main menu
sudo hardn -n -r                # show Run All's plan
```

<details>
<summary>Full options list (<code>hardn --help</code>)</summary>

```
$ hardn --help

A simple hardening tool for Debian, Ubuntu, Proxmox, Alpine, Rocky, Alma, RHEL,
CentOS Stream and Oracle Linux.

Usage:
  hardn [flags]
  hardn [command]

Available Commands:
  check            Compare the host with its saved baseline
  completion       Generate the autocompletion script for the specified shell
  config           Show hardn.yml: the file hardn reads and each key's value
  confirm          Keep the change that is waiting for confirmation
  help             Help about any command
  rollback         Undo the journalled change
  system-details   Display detailed system summary

Flags:
      --ascii                      Use ASCII glyphs only (or set HARDN_ASCII=1)
      --color auto|always|never    When to colour the output; auto colours a
                                   terminal unless NO_COLOR is set or TERM is
                                   dumb (default auto)
  -f, --config string              Specify configuration file path
  -g, --configure-dns              Configure DNS resolvers
  -w, --configure-ufw              Configure UFW
      --confirm-timeout duration   How long a lockout-risk change waits for sudo
                                   hardn confirm before it is rolled back, such
                                   as 300 or 5m (default 3m, at least 1m;
                                   hardn.yml's confirmTimeout)
  -c, --create-user                Create non-root user with sudo access
  -d, --disable-root               Disable root SSH access
  -n, --dry-run                    Dry run mode (preview changes without
                                   applying)
  -h, --help                       help for hardn
      --no-color                   Deprecated: use --color=never
      --plain                      Plain output: ASCII glyphs, no colour, no
                                   animation (or set HARDN_PLAIN=1)
  -p, --print-logs                 Print logs
  -r, --run-all                    Run all hardening steps
      --theme dark|light|auto      The colour theme; auto asks the terminal for
                                   its background (or set HARDN_THEME; default
                                   dark)
  -u, --username string            Specify username to create
  -v, --version                    Show version information
      --yes                        Apply a plan without a terminal to confirm it
                                   on; a lockout-risk plan still needs one

Use "hardn [command] --help" for more information about a command.
```

</details>

> [!TIP]
> See [docs/usage.md](docs/usage.md) for every flag and command, scripts and cron, exit statuses, JSON and logs. 

### hardn.yml
See [docs/configuration.md](docs/configuration.md) for every `hardn.yml` key.

## Supported Distributions

| Distribution | Supported | Best effort | End of life schedule |
|---|---|---|---|
| Debian | `13`, `12` | | `13`: 2030-06-30, `12`: 2028-06-30 (the end of LTS) |
| Ubuntu | `26.04`, `24.04` | `22.04` | `26.04`: May 2031, `24.04`: May 2029, `22.04`: May 2027 (without Ubuntu Pro) |
| Alpine | `3.24`, `3.23` | `3.22` | `3.24`: 2028-06-01, `3.23`: 2027-11-01, `3.22`: 2027-05-01, `3.21`: 2026-11-01 |
| Proxmox | `9` | | `9`: not announced, so no warning; `8`: August 2026 |
| Rocky, Alma, RHEL | `10`, `9` | | `10`: 2035-05-31, `9`: 2032-05-31 (RHEL without the paid add-on) |
| CentOS Stream | `10`, `9` | | `10`: 2030-05-31, `9`: 2027-05-31 |
| Oracle | `10`, `9` | | `10`: June 2035, `9`: June 2032 (the end of Premier Support) |

- **Supported**: hardn changes it.
- **Best effort**: the vendor still supports it. hardn changes it, but makes no promises, and every plan carries a warning (`PLATFORM_BEST_EFFORT`).
- **End of life schedule**: when each release's free security updates end, as hardn knows them.
- **Out of scope**: Alpine 3.21 and 3.20, Proxmox 8, Ubuntu's interim releases, and any version not in the table. hardn shows the screens and refuses every change.
- **Any other distribution**: hardn doesn't start.
- **End of support**: from 90 days before a release's vendor support ends, the main menu says so.
- **Tested**: CI runs the tests on the container image of each Supported release but Proxmox (Debian, Ubuntu, Alpine, Rocky, Alma, CentOS Stream, Oracle, and Red Hat's UBI for RHEL), and a release waits for them. Proxmox VE has no image: its support is tested against synthetic Proxmox VE 9 files. Since v0.6.0, the release notes name the real hosts a release was tested on, or say that it changed only wording.

What differs on Alpine, on Rocky, Alma, RHEL, CentOS Stream and Oracle, and on Proxmox: [docs/platforms.md](docs/platforms.md).

## Security

- **No telemetry.** `hardn` makes no network request but the release check, which it asks about, yes by default, or which `checkReleases` sets. It asks GitHub for the latest release's tag once a day and sends nothing about the host. No account or cloud service required.
- **Releases.** Each binary has [SLSA](https://slsa.dev) Build Level 3 provenance and a [Sigstore](https://www.sigstore.dev/) signature, and a rebuild of a tag is byte-identical: [docs/verify.md](docs/verify.md).
- **CVEs.** hardn checks for the vendor's fix to a CVE that makes one of its changes unsafe, today one on Alpine: [docs/cve.md](docs/cve.md).
- **Reporting.** Report security problems as [.github/SECURITY.md](.github/SECURITY.md) says, **<ins>not in a public issue</ins>**.
- **Other bugs.** Use the [bug report form](https://github.com/abbott/hardn/issues/new?template=bug_report.yml): your OS and version, and the steps to reproduce.

## Documentation

| Page | What it covers |
|---|---|
| [docs/usage.md](docs/usage.md) | the menu, flags, commands, exit statuses, output, logs |
| [docs/configuration.md](docs/configuration.md) | `hardn.yml` and `HARDN_CONFIG` |
| [docs/safeguards.md](docs/safeguards.md) | refusals, the revert timer, backups |
| [docs/install.md](docs/install.md) | install by hand, build from source, troubleshooting |
| [docs/verify.md](docs/verify.md) | release provenance and how to check it |
| [docs/platforms.md](docs/platforms.md) | Alpine, Rocky, Alma, RHEL, CentOS Stream, Oracle and Proxmox |
| [docs/cve.md](docs/cve.md) | the CVEs hardn checks for |
| [docs/reference/output.md](docs/reference/output.md) | the JSON document and its codes |
| [docs/roadmap.md](docs/roadmap.md) | planned features |
| [docs/releases/v0.11.0.md](docs/releases/v0.11.0.md) | the release notes |

## How to Help

I provide `hardn`, and my commitment to it, as a public service. Code isn't expected from anyone. In order of what helps most:

1. **Star** [the repository](https://github.com/abbott/hardn), so others find it.
2. **Share** it with anyone who runs a Linux server.
3. **Request a feature** with the [feature request form](https://github.com/abbott/hardn/issues/new?template=feature_request.yml). Requests shape what's prioritized and built in future releases. Found a bug? Use the [bug report form](https://github.com/abbott/hardn/issues/new?template=bug_report.yml).
4. **Sponsor** through [GitHub Sponsors](https://github.com/sponsors/abbott), to fund continued development and maintenance, and the direct help that schools, clinics and nonprofits receive: [.github/SUPPORT.md](.github/SUPPORT.md#help-for-schools-clinics-and-nonprofits).
5. **Contribute** code: read [docs/contributing.md](docs/contributing.md) before opening a pull request, and [.github/GOVERNANCE.md](.github/GOVERNANCE.md) for the project's mission.

## Origin

After hardening containers and VMs by hand for years, I wrote a script, `hardn.sh`, to automate the essentials from a config file and command-line arguments. The code needed a rewrite to stay maintainable, so I adopted Go and published `hardn`. Enjoy! :tumbler_glass:

Built with the libraries in [docs/acknowledgments.md](docs/acknowledgments.md).

## License

Copyright © 2025-2026 Bruce Abbott. GNU AGPL v3: see [LICENSE](LICENSE).

hardn comes with no warranty, and its authors accept no liability for its use: see sections 15 and 16 of the license.

As an end-user, you can use hardn freely. The license’s terms apply when you pass hardn on to others, modified or otherwise, or offer a modified version as a service.
