# Hardn

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](#verify-a-release) [![Signed with Sigstore](https://img.shields.io/badge/Signed%20with-Sigstore-blue)](#verify-a-release) [![zizmor](https://img.shields.io/badge/%F0%9F%8C%88-zizmor-white?labelColor=white)](https://zizmor.sh/) [![Release](https://img.shields.io/github/v/release/abbott/hardn)](https://github.com/abbott/hardn/releases/latest) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/abbott/hardn/ci.yml) [![License: AGPL v3](https://img.shields.io/badge/License-AGPL_3-blue.svg)](LICENSE)

A simple hardening tool for Linux.
> Implement fundamental security measures on Debian, Ubuntu, Proxmox, Alpine, Rocky, and Alma Linux servers.

<p align="center">
<img width="1327" height="1803" alt="hardn v0.4.0" src="https://github.com/user-attachments/assets/e7b53b65-47f7-4283-aeda-1c3ec7b3d352" />
</p>

## Target Audience

Anyone managing a **privately owned** Linux server, container, or virtual machine, such as Homelab enthusiasts and Students.

> [!NOTE]
> While the binary distributions are [SLSA3](https://slsa.dev) and [Sigstore](https://www.sigstore.dev/) compliant, hardn is **<ins>not intended for enterprise use</ins>**.

## Overview

### Security Disclaimer

> [!IMPORTANT]
> `hardn` should be part of a broader security strategy, not a _"set it and forget it"_ solution — regular security audits, updates, and monitoring are still required.

### Air-Gapped

- Local installation, verification (`SHA256SUMS`, attestation), and operation. **<ins>No cloud required</ins>**.
- **<ins>ZERO</ins>** telemetry: `hardn` never phones home.

### What It Does
`sudo hardn` displays a **risk level** based on the current host configuration of users, SSH, firewall, DNS, automatic updates and AppArmor. Utilize the **menu options** to configure each of the following:

| Option | Displays | Configures |
|---|---|---|
| 1) User Management | users, their admin rights (`sudo` or `doas`), SSH keys, password state | create a user, grant or remove admin rights, ask or don't ask a password for them, add or remove an SSH key |
| 2) SSH Login | root login, password login, the port, allowed users, as sshd reads them | root login off or key-only, password login off or on, the SSH port, key-based authentication, moves hardn's old sshd settings into `00-hardn.conf` |
| 3) Firewall | `ufw` state, policy, rules, application profiles | installs `ufw`, enables secure defaults (SSH let through, incoming denied, started at boot), disables it, applies profiles. On Rocky and Alma: firewalld, without profiles or interfaces. On Proxmox: "View pve-firewall", read only |
| 4) DNS | what manages `/etc/resolv.conf`, the nameservers in use | sets nameservers (custom, or Cloudflare, Quad9, Google); where NetworkManager, resolvconf or a container manages DNS, it only reports |
| 5) Auto Updates | whether the host updates itself | turns on unattended-upgrades (Debian, Ubuntu), apk-cron and crond (Alpine) or dnf-automatic's security updates (Rocky, Alma); turns off only what hardn turned on. Read only on Proxmox |
| 6) Run All | what `hardn.yml` would change | one plan for the user, root login, the firewall, DNS and automatic updates. Where the firewall and root login both change, root login waits for the next Run All |
| 7) Backup | backups on or off, the backup path, past backup runs | automatic backup of modified configuration files, changes the path |
| 8) Environment | whether `HARDN_CONFIG` is kept under `sudo` or `doas` | keeps it in `/etc/sudoers.d/hardn-env-<user>`; under doas, adds it to hardn's own rule for the user, or, where hardn has none, says what to add |
| 9) System Details | OS, kernel, addresses, users, CPU, disk, memory, last logins | nothing |
| 10) Logs | `/var/log/hardn.log` | nothing |

_The hardn status for **AppArmor** only displays current state, nothing more._

### How It Works

1. **Plan.** hardn reads the host and shows what will change, with diffs. Some changes are refused at this point, with the reason and what to do (see [Safety](#safety)).
2. **Review.** A change that can't lock you out asks y/N. A change that could lock you out (SSH Login, enabling the firewall, some user changes, some Environment changes under doas, Run All) asks you to type the hostname.
3. **Apply.** The files a change touches are backed up first (with backups off, only those a rollback needs). A step that fails rolls the whole change back.
4. **Confirm or revert.** After a lockout-risk change, a timer starts (3 minutes by default). Open a new SSH connection from another machine, to the new port if the change moved it, and run `sudo hardn confirm`. If nobody confirms in time, the change is rolled back. `sudo hardn rollback <id>` undoes it sooner.

### Safety

- **Refused before anything changes.** hardn won't make a change it can't show to be safe:
  - **Root login.** Turning root's SSH login off is refused unless another admin can still log in and become root (by key, or by password while password login is on). It is also refused while you are logged in as root over SSH.
  - **Password login.** Turning password login off is refused if you logged in with a password, if an admin has a password and no key, or if no admin could log in by key.
  - **Firewall.** Enabling `ufw` first lets through every port sshd listens on. It is refused if hardn can't tell which ports those are, or if a rule already there would block SSH.
  - **One change at a time.** While a change waits for confirmation, or a rollback hasn't finished, new changes are refused.
  - The refusal says why, and in most cases what to do.
- **The revert timer.** A lockout-risk change runs under a systemd timer, or a process under OpenRC. It rolls the change back unless `hardn confirm` comes from a new connection in time. Where neither systemd nor OpenRC runs, such a change is refused. So is any lockout-risk change when a user other than root can change the hardn binary.
- **Across a reboot.** Before the first such change, hardn installs a boot unit, `hardn-boot`. At boot, before sshd starts, it restarts the timer for the time left, or rolls the change back if the time ran out. It is removed at the first boot with nothing waiting.
- **Backups.** Before a change, the files it touches are backed up under `backupPath` (`/var/backups/hardn` by default); with backups off (`enableBackups: false`), only the files a rollback needs are. A rollback restores them. `7) Backup` lists the runs.

Learn more: [revert.md](docs/safety/revert.md) (confirm-or-revert), [revert-boot.md](docs/safety/revert-boot.md) (the boot unit), [atomic.md](docs/safety/atomic.md) (writes and backups), [pve.md](docs/safety/pve.md) (Proxmox), and the index, [docs/safety](docs/safety/README.md).

### Dry run _(Plan mode)_
`sudo hardn -n` (or `dryRun: true` in hardn.yml) shows each operational plan and changes nothing. Operation flags exit 3 when there are changes. `--dry-run=false` overrides the file.

## Installation

### Install Script

```sh
curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```
_When a host uses `doas` (Alpine), pipe to `doas sh` instead of `sudo sh`._

**The script will**:

- Find the latest release through the GitHub API (or take `HARDN_VERSION`, below), and its asset for the system: `hardn-linux-amd64`, `hardn-linux-arm64` or `hardn-darwin-amd64`. There is no armv7 build, so on armv7 it stops.
- Check the asset's SHA-256 against the release's `SHA256SUMS`, and its attestation too when gh 2.68.0 or later is installed and signed in. If a check fails, it installs nothing. Without gh, it installs on the SHA-256 alone and prints the commands that check the attestation.
- Install `/usr/local/bin/hardn` and create `/etc/hardn`.

To pin a version, set `HARDN_VERSION` (v0.4.0 and later; earlier releases have no `SHA256SUMS`):

```sh
curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo env HARDN_VERSION=0.4.0 sh
```

The first time hardn runs **live**, it writes `/etc/hardn/hardn.yml.example`. _On a terminal, a run that finds no hardn.yml offers to create one, except `setup-sudo-env` and dry runs._

### Install Manually

1. Visit the [Releases](https://github.com/abbott/hardn/releases) page.
2. Download the asset corresponding to your system (e.g., `hardn-linux-amd64`):

   ```sh
   curl -LO https://github.com/abbott/hardn/releases/latest/download/hardn-linux-amd64
   ```

3. [Verify it](#verify-a-release) (*optional, but recommended*)
4. Install, set permissions, and test:

   ```sh
   sudo install -m 0755 hardn-linux-amd64 /usr/local/bin/hardn
   hardn -h     # help
   ```

### Build from Source

You need Go 1.26 or later ([install Go](https://go.dev/doc/install)), git and make. If your Go is older than 1.27.3, `go` fetches 1.27.3 by default, because releases are built with it (the `toolchain` line in go.mod).

```sh
git clone https://github.com/abbott/hardn.git
cd hardn
make build       # build/hardn, for this machine
make linux       # build/hardn-linux-amd64 (also: `make arm` for linux-arm64, `make darwin`)
sudo cp build/hardn /usr/local/bin/hardn
```

`make build` runs `go build ./cmd/hardn` with the release's flags. A bare `make` runs the project's checks, not a build ([docs/contributing.md](docs/contributing.md)).

### Troubleshooting

- **Permission issues:** Not run as root, the script stops before downloading anything and prints the command to use, piped to `sudo sh` (or `doas sh`).
- **Missing curl:** If `curl` is not installed, use your package manager to install it (e.g., `sudo apt-get install curl` on Debian/Ubuntu; `doas apk add curl` or `sudo apk add curl` on Alpine).
- **hardn not found:** If `/bin/sh: hardn: not found` appears, `/usr/local/bin` is not in this shell's `PATH`. Run `/usr/local/bin/hardn`, or `export PATH=/usr/local/sbin:/usr/local/bin:$PATH` first.

### Updating

To update, run the  [Install Script](#install-script) command again.

## Usage

`hardn` needs root, except `-p` alone, `--version`, `--help`, `completion` and `system-details`. Run it with `sudo hardn` or `doas hardn`.

| Flag | What it does |
|---|---|
| `-f, --config <path>` | Read this hardn.yml (otherwise `HARDN_CONFIG`, then the search path in [docs/configuration.md](docs/configuration.md)) |
| `-u, --username <name>` | The user `-c` creates; also the default for Create and Run All |
| `-c, --create-user` | Create the user with admin rights, no password, and every key in hardn.yml's `sshKeys` |
| `-d, --disable-root` | Turn root's SSH login off (key-only on Proxmox). Refused unless another admin can still log in and become root |
| `-w, --configure-ufw` | Enable `ufw` as Firewall does. Refused on Proxmox, Rocky and Alma |
| `-g, --configure-dns` | Set hardn.yml's nameservers |
| `-e, --setup-sudo-env` | Keep `HARDN_CONFIG` under sudo or doas, as `setup-sudo-env` does |
| `-r, --run-all` | Run All's plan from hardn.yml. Can't be combined with `-c`, `-d`, `-w`, `-g` or `-e` (exit 2) |
| `-n, --dry-run` | Dry run: show the plan, change nothing |
| `-p, --print-logs` | Print `/var/log/hardn.log`. On its own, it doesn't need root |
| `-v, --version` | Print the version, commit and build date |
| `--yes` | Off a terminal, apply a change that can't lock you out (see below) |
| `--confirm-timeout <time>` | How long a lockout-risk change waits for `hardn confirm`: seconds (`300`) or a duration (`5m`). Default 3m, at least 1m |
| `--color auto\|always\|never` | When to use colour. `auto` (the default) colours a terminal unless `NO_COLOR` is set or `TERM` is `dumb` |
| `--ascii` | ASCII symbols only |
| `--plain` | No animation, ASCII symbols, and no colour unless `--color=always` |
| `--theme dark\|light\|auto` | Colour theme. `auto` asks the terminal for its background. Default dark |
| `-h, --help` | Help for hardn or a command |

With no operation flag, `hardn` opens the main menu. Operation flags can be combined, and run in the order `-c`, `-d`, `-w`, `-g`, `-p`, `-e`.

| Command | What it does |
|---|---|
| `hardn completion <shell>` | Print a tab-completion script for bash, zsh, fish or powershell. Learn more: `hardn completion bash --help` |
| `hardn confirm` | Keep the change that is waiting for confirmation. Run it from a new SSH connection. At the console, it confirms with a warning. When run from a new SSH connection after a port change, it also closes the old port |
| `hardn rollback <id>` | Undo that change now, or finish a rollback that stopped partway |
| `hardn rollback --abandon <id>` | Give up a rollback that can never finish. The host stays as the rollback left it |
| `hardn rollback --boot` | What hardn's boot unit runs. You don't run it yourself |
| `hardn system-details` | Print the System Details report. `--output json` prints it as JSON. Save it with `> file` |
| `hardn setup-sudo-env` | The same as `-e` |

**On a terminal, and off one (scripts, cron).** Off a terminal, hardn never asks a question.

| Flag | On a terminal | Off a terminal |
|---|---|---|
| `-c`, `-g`, `-e`, `setup-sudo-env` | Applies the change, unasked | Applies it only with `--yes` |
| `-w`, `-d` | A change that could lock you out: type the hostname. Any other: applied, unasked | A change that could lock you out: refused (exit 1), even with `--yes`. Any other: only with `--yes` |
| `-r` | Type the hostname, for any plan with changes, `--yes` or not | A change that could lock you out: refused (exit 1), even with `--yes`. Any other: only with `--yes` |

To ask for the hostname, hardn needs stdin and stdout both on a terminal: with the output piped or redirected, a change that could lock you out is refused, and `-r` needs `--yes` for any other. Otherwise only stdin counts.

Where `--yes` is needed and not given, hardn shows the plan, changes nothing, and exits 2. A plan with nothing to change exits 0. `-e` refuses a change that could lock you out, on a terminal too: make it from main menu 8.

```sh
sudo hardn -n -r                # show Run All's plan
sudo hardn -u george -c         # create george, an admin with hardn.yml's keys
sudo hardn -c -d                # create the user, then turn root login off
sudo hardn -g --yes             # from cron: set the nameservers
sudo hardn system-details --output json > web-01.json
```

## Output

### Exit statuses

| Status | Meaning |
|---|---|
| `0` | Done: nothing to change, or every change applied (including one waiting for `hardn confirm`) |
| `1` | An error or a refusal (also: not root, a hardn.yml that doesn't load, `-c` with no username). Anything the failed change applied is rolled back; if that rollback can't finish, hardn says how to finish it. Changes the other flags in the same command applied stay, and the flags after a refused one still run: in `hardn -c -d`, a refused `-d` leaves the user `-c` created |
| `2` | A usage error (bad flags, or a bad `confirmTimeout` in hardn.yml), or changes that need `--yes` off a terminal |
| `3` | A plan has changes to make (dry run), or you declined them |
| `130` | Interrupted: Ctrl-C, SIGTERM or a hangup. An apply that was running is rolled back first |

### JSON
`hardn system-details --output json` prints one JSON document on stdout. [docs/reference/output.md](docs/reference/output.md) describes its fields and the stable error and finding codes.

### Plain output and colour
Output off a terminal is plain: no colour, no animation, ASCII symbols. `--color=always` colours it anyway, and wins over `--plain`, `NO_COLOR` and `TERM=dumb` too. hardn's messages go to stderr, so stdout holds only a command's output. The environment variables match the flags: `NO_COLOR` (any non-empty value turns colour off), `HARDN_PLAIN=1`, `HARDN_ASCII=1`, `HARDN_THEME=dark|light|auto`. `--color` and `--theme` win over their variables; `--plain` and `--ascii` can only turn plain or ASCII on.

> [!TIP]
> `TERM=linux`, as under Proxmox VE's `pct console`, gets basic colours and ASCII symbols. Where your terminal can show more, add `[ "$TERM" = linux ] && alias hardn='TERM=xterm-256color LANG=C.UTF-8 hardn'` to `/root/.profile`.

### Logs
- `/var/log/hardn/audit.jsonl` gets one JSON line for each change hardn plans: from an operation flag, a screen, or `hardn confirm`'s clean-up. That includes changes applied, refused, declined, rolled back or interrupted, and dry runs. It is root's alone. Read it with `sudo tail -n 5 /var/log/hardn/audit.jsonl`.
- `/var/log/hardn.log` is hardn's log, including the commands a rollback runs. `10) Logs` and `-p` show it. A dry run doesn't write to it; each plan it shows adds one line to the audit log, and nothing else is written.

## hardn.yml

hardn reads `username`, `sshKeys`, `dryRun`, `enableBackups`, `backupPath`, `nameservers`, `sshPort` (the default for Change SSH port), `sshConfigFile` (where hardn's old settings are), `confirmTimeout`, `ufwAppProfiles`, and Run All's switches: `enableUnattendedUpgrades`, `enableUfwSshPolicy`, `configureDns` and `disableRootSSH`.

```yaml
username: "george"
sshKeys:
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... george@example.net"
nameservers: ["192.0.2.53", "192.0.2.54"]
enableUfwSshPolicy: true
configureDns: true
disableRootSSH: true
confirmTimeout: "5m"
```

> [!TIP]
> See [docs/configuration.md](docs/configuration.md) for every key, where hardn looks for the file, and how to keep `HARDN_CONFIG` under sudo. Example: [hardn.yml.example](hardn.yml.example)

## Distribution Support

| Distribution | Supported | Best effort | Out of scope |
|---|---|---|---|
| Debian | 13, 12 | | |
| Ubuntu | 26.04, 24.04 | 22.04 | interim (non-LTS) releases |
| Alpine | 3.24, 3.23 | 3.22 | 3.21 (vendor support ended 2026-11-01), 3.20 (2026-04-01) |
| Proxmox | 9 | | 8 (end of life since 2026-08-31) |
| Rocky | 10, 9 | | |
| Alma | 10, 9 | | |

- **Supported**: hardn changes it.
- **Best effort**: the vendor still supports it. hardn changes it, but makes no promises, and every plan carries a warning (`PLATFORM_BEST_EFFORT`).
- **Out of scope**, and any version not in the table: hardn shows the screens and refuses every change.
- **Any other distribution**: hardn doesn't start.

> [!NOTE]
> On Alpine, `ufw` and `sudo` come from the community repository. On Alpine 3.22 and 3.23, the vendor supports only the main repository.

### Rocky and Alma

On Rocky and Alma:

- A new SSH port is labelled for sshd in SELinux and let through firewalld before sshd moves.
- The Firewall screen installs, enables and disables firewalld. It refuses a dnf install that brings a new kernel.
- Run All and `-w` leave the firewall to that screen. ufw is never installed or enabled.
- Auto Updates turns on dnf-automatic's daily security updates; restarts stay as automatic.conf sets them (never, as shipped). Off undoes only what hardn did.
- DNS is only read.

### Proxmox

On a Proxmox node:

- hardn never turns root's SSH login off, because the nodes log in to each other as root by key: `-d` and Run All make it key-only instead. Where it is already off, they leave it off; SSH Login's Root key-only item turns it back to key-only.
- SSH stays on port 22.
- Root's keys are edited through the cluster's shared file, and a node's own key is never removed.
- Root's password is never locked.
- The firewall is only read (pve-firewall). `ufw` is never touched, and `-w` is refused.
- Auto Updates only shows the state.

The default cluster join (`pvecm add`, or the web UI) logs in to the other node's API with root's password, and needs no SSH password login. Two steps log in over SSH as root instead. hardn does neither, so do them by hand:

- **`pvecm add <node> --use_ssh`** joins over SSH to the existing node. Where hardn made root key-only on that node (`-d`, Run All or SSH Login), root logs in there by key only. Add the joining node's root key (`/root/.ssh/id_rsa.pub`) to root's keys there first.
- **`pvecm qdevice setup <address>`** logs in to the QDevice host as root over SSH, using root's password where the node's key isn't there yet. If hardn turned root login off on that host (`-d`, or Run All), allow it again there by hand for the setup.

## CVE Mitigations
> [!IMPORTANT]
> `hardn` doesn’t write its own patches. For each CVE below, it checks whether the host has the vendor’s fix, alerts when the fix is missing, refuses any change the vulnerability would make unsafe, and installs the fix where it can.

### CVE-2026-123158, Alpine (aka "*sshole") 
OpenSSH before 10.6p2, on musl without PAM, lets a password of 257 to 1024 bytes into an account whose password field is `*`, as a key-only account's is. hardn compares the installed `openssh-server` with each Alpine branch's first fixed release:
  - **SSH Auth** reports an unfixed sshd (`SSH_SSHD_UNFIXED`, or `SSH_SSHD_UNFIXED_LATENT` with password login off), or an exposed host (`SSH_PASSWORD_BYPASS`, risk level Critical) where such an account can be reached by password. The main menu shows an alert for either.
  - **Creating a user is refused** (`USERS_PASSWORD_BYPASS`) on an unfixed sshd with password login on, since a new user's field is `*`.
  - **Turning password login on is refused** (`SSH_PASSWORDS_UNFIXED`) on an unfixed sshd, for the same reason.
  - **To fix it,** select `2) SSH Login`, then `5) Upgrade openssh`, or select `6) Run All`: hardn upgrades every openssh package, then reloads sshd. Or turn password login off: select `2) SSH Login`, then `2) Key-only login`.

Learn more: [alpine.md](docs/safety/alpine.md) (the fixed releases by branch) and [check-ssh.md](docs/engine/check-ssh.md).

## Release Chain Security

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev) [![Signed with Sigstore](https://img.shields.io/badge/Signed%20with-Sigstore-blue)](https://www.sigstore.dev/) [![zizmor](https://img.shields.io/badge/%F0%9F%8C%88-zizmor-white?labelColor=white)](https://zizmor.sh/)

A `hardn` binary has a GitHub artifact attestation [SLSA](https://slsa.dev) Level 3 supply-chain security inclusive of the following security guarantees:

- **Tamper Protection**: Each binary is signed and includes a provenance attestation
- **Build Integrity**: Builds are performed in GitHub's trusted environment
- **Source Verification**: Binaries are traceable back to their source commit
- **Reproducible**: The build process is fully documented in the provenance

In addition to SLSA provenance, all artifacts are signed using [Sigstore](https://www.sigstore.dev/):

- **Transparency Log**: Every signature is recorded in the public Rekor log
- **Keyless Signing**: Short-lived certificate tied to GitHub's OIDC identity

The workflows that build and sign a release are audited by 🌈 [zizmor](https://docs.zizmor.sh) on every push to main and every pull request; any finding fails the check.

### Verify a Release

Verification is a separate step, and recommended. From v0.4.0, each release has `SHA256SUMS`, an SPDX SBOM (`hardn-sbom.spdx.json`), and the attestation for each binary.

[verify.sh](verify.sh) downloads a binary and `SHA256SUMS`, checks the SHA-256, and then checks the attestation. It needs curl, sha256sum or shasum, and [gh](https://cli.github.com) 2.68.0 or later, signed in (`gh auth login`):

```sh
curl -fsSLO https://raw.githubusercontent.com/abbott/hardn/v0.6.0/verify.sh
sh verify.sh 0.6.0 linux-amd64      # leaves ./hardn-linux-amd64 only if both checks pass
```

To check a binary you already have, such as one installed using [install.sh](install.sh), use `gh`:

```sh
gh attestation verify /usr/local/bin/hardn --repo abbott/hardn \
  --cert-identity https://github.com/abbott/hardn/.github/workflows/build.yml@refs/tags/v0.6.0 \
  --source-ref refs/tags/v0.6.0 --deny-self-hosted-runners
```
> [!NOTE]
> To check the SHA-256 alone, download `SHA256SUMS` from the same release, then run `grep ' hardn-linux-amd64$' SHA256SUMS | sha256sum -c -`.

## Contributing and Security
> [!IMPORTANT]
> Read [docs/contributing.md](docs/contributing.md) before opening a pull request.\
> Report security problems as [.github/SECURITY.md](.github/SECURITY.md) says, **<ins>not in a public issue</ins>**. 

Report bugs in the GitHub issue tracker, with your OS and version and the steps to reproduce. 

Planned features are in [ROADMAP.md](ROADMAP.md). The release notes are in [docs/releases/v0.6.0.md](docs/releases/v0.6.0.md). 

## Origin

After hardening Debian-based containers and VMs by hand for years, I wrote a script to automate the essentials from a config file and command-line arguments. Within a year a CLI menu was bolted on, the code needed a rewrite to stay maintainable, so I adopted Go and published `hardn`. Enjoy! :tumbler_glass:

## Acknowledgments

hardn is built with [Cobra](https://github.com/spf13/cobra) and [pflag](https://github.com/spf13/pflag) (command line), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [colorprofile](https://github.com/charmbracelet/colorprofile), [x/ansi](https://github.com/charmbracelet/x) and x/term from Charm (terminal output), [go.yaml.in/yaml/v3](https://github.com/yaml/go-yaml) (hardn.yml), and Go's [x/sys](https://pkg.go.dev/golang.org/x/sys) and [x/text](https://pkg.go.dev/golang.org/x/text).

## License

GNU AGPL v3: see [LICENSE](LICENSE).
