# Hardn

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](#verify-a-release) [![Release](https://img.shields.io/github/v/release/abbott/hardn)](https://github.com/abbott/hardn/releases/latest) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/abbott/hardn/ci.yml) [![License: AGPL v3](https://img.shields.io/badge/License-AGPL_3-blue.svg)](LICENSE)

A hardening tool for Debian, Ubuntu, Proxmox VE and Alpine Linux servers. It sets up an admin user, SSH login, ufw, DNS and automatic updates. It shows each change before making it and can roll back a change that might lock you out.

hardn covers a few basics. You still need to audit, update and monitor the host yourself. It is written for privately owned servers, homelabs and VMs.

## What it does

`sudo hardn` opens the main menu. At the top it shows the state of users, SSH, the firewall, AppArmor and automatic updates, and a risk level. Next to the prompt it shows the mode: "Mode Live" or "Mode Plan only". Opening a screen changes nothing.

| Item | Shows | Changes |
|---|---|---|
| 1 User Management | users, their admin rights (sudo or doas), SSH keys, password state | create a user, grant or remove admin rights, ask or don't ask a password for them, add or remove an SSH key |
| 2 SSH Login | root login, password login, the port, allowed users, as sshd reads them | root login off or key-only, password login off or on, the SSH port, moves hardn's old sshd settings into `00-hardn.conf` |
| 3 DNS | what manages `/etc/resolv.conf`, the nameservers in use | sets nameservers (yours, or Cloudflare, Google, Quad9); where NetworkManager, resolvconf or a container manages DNS, it only reports |
| 4 Firewall | ufw's state, policy, rules, application profiles | installs ufw, enables it (SSH let through, incoming denied, started at boot), disables it, applies profiles. On Proxmox VE: "View pve-firewall", read only |
| 5 Backup | backups on or off, the backup path, past backup runs | turns backups on or off, changes the path (no restore yet) |
| 6 Auto Updates | whether the host updates itself | turns on unattended-upgrades (Debian, Ubuntu) or apk-cron and crond (Alpine); turns off only what hardn turned on. Read only on Proxmox VE |
| 7 Run All | what hardn.yml would change | one plan for the user, root login, the firewall, DNS and automatic updates |
| 8 Environment | whether `HARDN_CONFIG` is kept under sudo or doas | keeps it in `/etc/sudoers.d/hardn-env-<user>`; under doas, says what to add |
| 9 System Details | OS, kernel, addresses, users, CPU, disk, memory, last logins | nothing |
| 10 Logs | `/var/log/hardn.log` | nothing |

AppArmor and Lynis are not set up: AppArmor's state is shown, nothing more.

**Every change goes through the same steps:**

1. **Plan.** hardn reads the host and shows what will change, with diffs. Some changes are refused at this point, with the reason and what to do (see [Safety](#safety)).
2. **Review.** A change that can't lock you out asks y/N. A change that could lock you out (SSH Login, enabling the firewall, some user changes, Run All) asks you to type the hostname.
3. **Apply.** Files are backed up first. A step that fails rolls the whole change back.
4. **Confirm or revert.** After a lockout-risk change, a timer starts (3 minutes by default). Open a new SSH connection from another machine, to the new port if the change moved it, and run `sudo hardn confirm`. If nobody confirms in time, the change is rolled back. `sudo hardn rollback <id>` undoes it sooner.

**Plan only.** `-n` (or `dryRun: true` in hardn.yml) shows each plan and changes nothing. Screens stop at their plan. Operation flags exit 3 when there are changes. `--dry-run=false` overrides the file.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```

Use `doas sh` where the host uses doas (Alpine). The script fetches the latest release's binary for this machine (`hardn-linux-amd64`, `hardn-linux-arm64` or `hardn-darwin-amd64`; there is no armv7 build) and installs it as `/usr/local/bin/hardn`. It also creates `/etc/hardn`. It doesn't run hardn. hardn writes `/etc/hardn/hardn.yml.example` itself the first time it runs live. On a terminal, hardn also offers to create `hardn.yml`.

To update, run the same command again. To install by hand, download the binary from [Releases](https://github.com/abbott/hardn/releases), verify it (below), then run `sudo install -m 0755 hardn-linux-amd64 /usr/local/bin/hardn`.

hardn changes Linux hosts only. The darwin-amd64 build exists, but it hardens nothing on macOS.

### Verify a release

Verification is a separate step, and recommended. From v0.4.0, each release has `SHA256SUMS`, an SPDX SBOM (`hardn-sbom.spdx.json`), and a GitHub artifact attestation for each binary. That attestation is SLSA Build Level 3 provenance: it was signed by the repository's build workflow, from the release's tag, on a GitHub-hosted runner.

[verify.sh](verify.sh) downloads a binary and `SHA256SUMS`, checks the SHA-256, and then checks the attestation. It needs curl, sha256sum or shasum, and [gh](https://cli.github.com) 2.68.0 or later, signed in (`gh auth login`):

```sh
curl -fsSLO https://raw.githubusercontent.com/abbott/hardn/v0.4.0/verify.sh
sh verify.sh 0.4.0 linux-amd64      # leaves ./hardn-linux-amd64 only if both checks pass
```

To check a binary you already have, such as the one install.sh installed, use gh:

```sh
gh attestation verify /usr/local/bin/hardn --repo abbott/hardn \
  --cert-identity https://github.com/abbott/hardn/.github/workflows/build.yml@refs/tags/v0.4.0 \
  --source-ref refs/tags/v0.4.0 --deny-self-hosted-runners
```

To check the SHA-256 alone, download `SHA256SUMS` from the same release, then run `grep ' hardn-linux-amd64$' SHA256SUMS | sha256sum -c -`.

### Build from source

You need Go 1.26 or later ([install Go](https://go.dev/doc/install)), git and make. If your Go is older than 1.27.3, `go` fetches 1.27.3 by default, because releases are built with it (the `toolchain` line in go.mod).

```sh
git clone https://github.com/abbott/hardn.git
cd hardn
make build                      # build/hardn, for this machine
make linux                      # build/hardn-linux-amd64 (also: make arm, make darwin)
sudo cp build/hardn /usr/local/bin/hardn
```

`make build` runs `go build ./cmd/hardn` with the release's flags. A bare `make` runs the project's checks, not a build ([docs/contributing.md](docs/contributing.md)).

## Usage

hardn needs root, except `-p` alone, `--version`, `--help` and `system-details`. Run it with `sudo hardn` or `doas hardn`.

| Flag | What it does |
|---|---|
| `-f, --config <path>` | Read this hardn.yml (otherwise `HARDN_CONFIG`, then the search path in [docs/configuration.md](docs/configuration.md)) |
| `-u, --username <name>` | The user `-c` creates; also the default for Create and Run All |
| `-c, --create-user` | Create the user with admin rights, no password, and every key in hardn.yml's `sshKeys` |
| `-d, --disable-root` | Turn root's SSH login off (key-only on Proxmox VE). Refused unless another admin can still log in and become root |
| `-w, --configure-ufw` | Enable ufw as Firewall does. Refused on Proxmox VE |
| `-g, --configure-dns` | Set hardn.yml's nameservers |
| `-e, --setup-sudo-env` | Keep `HARDN_CONFIG` under sudo or doas, as `setup-sudo-env` does |
| `-r, --run-all` | Run All's plan from hardn.yml. Can't be combined with `-c`, `-d`, `-w`, `-g` or `-e` (exit 2) |
| `-n, --dry-run` | Plan only: show the plan, change nothing |
| `-p, --print-logs` | Print `/var/log/hardn.log`. On its own, it doesn't need root |
| `-v, --version` | Print the version, commit and build date |
| `--yes` | Off a terminal, apply a change that can't lock you out (see below) |
| `--confirm-timeout <time>` | How long a lockout-risk change waits for `hardn confirm`: seconds (`300`) or a duration (`5m`). Default 3m, at least 1m |
| `--color auto\|always\|never` | When to use colour. `auto` (the default) colours a terminal unless `NO_COLOR` is set or `TERM` is `dumb` |
| `--ascii` | ASCII symbols only |
| `--plain` | No colour, no animation, ASCII symbols |
| `--theme dark\|light\|auto` | Colour theme. `auto` asks the terminal for its background. Default dark |
| `--no-color` | Deprecated: use `--color=never`. It still works, with a warning |
| `-h, --help` | Help for hardn or a command |

With no operation flag, hardn opens the main menu. Operation flags can be combined, and run in the order `-c`, `-d`, `-w`, `-g`, `-p`, `-e`.

| Command | What it does |
|---|---|
| `hardn confirm` | Keep the change that is waiting for confirmation. Run it from a new SSH connection. At the console, it confirms with a warning. When run from a new SSH connection after a port change, it also closes the old port |
| `hardn rollback <id>` | Undo that change now, or finish a rollback that stopped partway |
| `hardn rollback --abandon <id>` | Give up a rollback that can never finish. The host stays as the rollback left it |
| `hardn rollback --boot` | What hardn's boot unit runs. You don't run it yourself |
| `hardn system-details` | Print the System Details report. `--output json` prints it as JSON. Save it with `> file` |
| `hardn setup-sudo-env` | The same as `-e` |

**Off a terminal (scripts, cron).** hardn never asks a question there.

- `-c`, `-g`, `-e` and `setup-sudo-env` apply a change only with `--yes`. So do `-w` and `-d`, and `-r` for a plan that can't lock you out. Without `--yes`, hardn shows the plan, changes nothing, and exits 2. A plan with nothing to change exits 0.
- `--yes` never applies a change that could lock you out. Off a terminal, such a plan from `-w`, `-d` or `-r` is refused (exit 1). Run it from a terminal and type the hostname.
- On a terminal, `-r` asks you to type the hostname whatever the plan, `--yes` or not. `-w` and `-d` ask for a change that could lock you out. `-c` and `-g` ask nothing. `-e` refuses a change that could lock you out: make it from main menu 8.

```sh
sudo hardn -n -r                # show Run All's plan
sudo hardn -u alice -c          # create alice, an admin with hardn.yml's keys
sudo hardn -c -d                # create the user, then turn root login off
sudo hardn -g --yes             # from cron: set the nameservers
sudo hardn system-details --output json > web-01.json
```

## Output

**Exit statuses** (they won't change once released):

| Status | Meaning |
|---|---|
| `0` | Done: nothing to change, or every change applied (including one waiting for `hardn confirm`) |
| `1` | An error or a refusal. Anything this run applied is rolled back; if that rollback can't finish, hardn says how to finish it |
| `2` | A usage error (bad flags or hardn.yml), or changes that need `--yes` off a terminal |
| `3` | A plan has changes to make (plan only), or you declined them |
| `130` | Interrupted: Ctrl-C, SIGTERM or a hangup. An apply that was running is rolled back first |

**JSON.** `hardn system-details --output json` prints one JSON document on stdout. [docs/reference/output.md](docs/reference/output.md) describes its fields and the stable error and finding codes.

**Plain output and colour.** Output off a terminal is plain: no colour, no animation, ASCII symbols. hardn's messages go to stderr, so stdout holds only a command's output. The environment variables match the flags: `NO_COLOR` (any non-empty value turns colour off), `HARDN_PLAIN=1`, `HARDN_ASCII=1`, `HARDN_THEME=dark|light|auto`. A flag wins over its variable.

**Logs.**

- `/var/log/hardn/audit.jsonl` gets one JSON line for each change hardn plans: from an operation flag, a screen, or `hardn confirm`'s clean-up. That includes changes applied, refused, declined, rolled back or interrupted, and plan only. It is root's alone. Read it with `sudo tail -n 5 /var/log/hardn/audit.jsonl`. Not recorded yet: the confirmation itself and `hardn rollback`.
- `/var/log/hardn.log` is hardn's log, including the commands a rollback runs. Logs (10) and `-p` show it. Plan only doesn't write to it; it adds one line to the audit log and nothing else.

## Safety

- **Refused before anything changes.** hardn won't make a change it can't show to be safe:
  - **Root login.** Turning root's SSH login off is refused unless another admin can still log in and become root (by key, or by password while password login is on). It is also refused while you are logged in as root over SSH.
  - **Password login.** Turning password login off is refused if you logged in with a password, if an admin has a password and no key, or if no admin could log in by key.
  - **Firewall.** Enabling ufw first lets through every port sshd listens on. It is refused if hardn can't tell which ports those are, or if a rule already there would block SSH.
  - **One change at a time.** While a change waits for confirmation, or a rollback hasn't finished, new changes are refused.
  - The refusal says why and what to do.
- **The revert timer.** A lockout-risk change runs under a systemd timer, or a process under OpenRC. It rolls the change back unless `hardn confirm` comes from a new connection in time. Where neither systemd nor OpenRC runs, such a change is refused. So is any lockout-risk change when a user other than root can change the hardn binary.
- **Across a reboot.** Before the first such change, hardn installs a boot unit, `hardn-boot`. At boot, before sshd starts, it restarts the timer for the time left, or rolls the change back if the time ran out. It is removed at the first boot with nothing waiting.
- **Backups.** Before a change, the files it touches are backed up under `backupPath` (`/var/backups/hardn` by default). A rollback restores them. Backup (5) lists the runs. Restoring a run by hand isn't built yet.

More detail: [revert.md](docs/safety/revert.md) (confirm-or-revert), [revert-boot.md](docs/safety/revert-boot.md) (the boot unit), [atomic.md](docs/safety/atomic.md) (writes and backups), [pve.md](docs/safety/pve.md) (Proxmox VE), and the index, [docs/safety](docs/safety/README.md).

## hardn.yml

hardn reads `username`, `sshKeys`, `dryRun`, `enableBackups`, `backupPath`, `nameservers`, `sshPort` (the default for Change SSH port), `sshConfigFile` (where hardn's old settings are), `confirmTimeout`, `ufwAppProfiles`, and Run All's switches: `enableUnattendedUpgrades`, `enableUfwSshPolicy`, `configureDns` and `disableRootSSH`.

Keys hardn no longer reads are still accepted. Where one is set to something other than its default, hardn warns, for example "hardn.yml's enableLynis isn't used". The username has no default.

```yaml
username: "alice"
sshKeys:
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... alice@example.net"
nameservers: ["192.0.2.53", "192.0.2.54"]
enableUfwSshPolicy: true
configureDns: true
disableRootSSH: true
confirmTimeout: "5m"
```

See [docs/configuration.md](docs/configuration.md) for every key, where hardn looks for the file, and how to keep `HARDN_CONFIG` under sudo. The example file is [hardn.yml.example](hardn.yml.example).

## Supported versions

| Distribution | Supported | Best effort | Out of scope |
|---|---|---|---|
| Debian | 13, 12 | | |
| Ubuntu | 26.04, 24.04 | 22.04 | interim (non-LTS) releases |
| Alpine | 3.24, 3.23 | 3.22 | 3.21 (vendor support ended 2026-11-01) |
| Proxmox VE | 9 | | 8 (end of life since 2026-08-31) |

- **Supported**: hardn changes it.
- **Best effort**: the vendor still supports it. hardn changes it, but makes no promises, and every plan carries a warning (`PLATFORM_BEST_EFFORT`).
- **Out of scope**, and any version not in the table: hardn shows the screens and refuses every change.
- **Any other distribution**: hardn doesn't start.

**Tested so far** on VMs during development: Debian 13, Ubuntu 26.04 and Alpine 3.23. The release's VM suite will add to this list. Proxmox VE 9 and the other supported versions haven't been run on a VM yet.

On Alpine, ufw and sudo come from the community repository. On Alpine 3.21 to 3.23, the vendor supports only the main repository.

### Proxmox VE

On a Proxmox VE node:

- Root's SSH login stays key-only, never off, because the nodes log in to each other as root by key. `-d` and Run All make root key-only.
- SSH stays on port 22.
- Root's keys are edited through the cluster's shared file, and a node's own key is never removed.
- Root's password is never locked.
- The firewall is only read (pve-firewall). ufw is never touched, and `-w` is refused.
- Auto Updates only shows the state.

The default cluster join (`pvecm add`, or the web UI) logs in to the other node's API with root's password, and needs no SSH password login. Two steps log in over SSH as root instead. hardn does neither, so do them by hand:

- **`pvecm add <node> --use_ssh`** joins over SSH to the existing node. Where hardn runs on that node, root logs in there by key only. Add the joining node's root key (`/root/.ssh/id_rsa.pub`) to root's keys there first.
- **`pvecm qdevice setup <address>`** logs in to the QDevice host as root over SSH, using root's password where the node's key isn't there yet. If hardn turned root login off on that host (`-d`, or Run All), allow it again there by hand for the setup.

## Contributing and security

Read [docs/contributing.md](docs/contributing.md) before opening a pull request. Report bugs in the GitHub issue tracker, with your OS and version and the steps to reproduce. Report security problems as [.github/SECURITY.md](.github/SECURITY.md) says, not in a public issue. Changes since v0.3.2 are listed in [docs/releases/unreleased.md](docs/releases/unreleased.md).

## Origin

After hardening Debian-based containers and VMs by hand for years, I wrote a local script to automate the essentials from a config file and command-line arguments. Within a year a CLI menu was bolted on, the code needed a rewrite to stay maintainable, so I moved it to Go and published it.

## Acknowledgments

hardn is built with [Cobra](https://github.com/spf13/cobra) and [pflag](https://github.com/spf13/pflag) (command line), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [colorprofile](https://github.com/charmbracelet/colorprofile), [x/ansi](https://github.com/charmbracelet/x) and x/term from Charm (terminal output), [go.yaml.in/yaml/v3](https://github.com/yaml/go-yaml) (hardn.yml), and Go's [x/sys](https://pkg.go.dev/golang.org/x/sys) and [x/text](https://pkg.go.dev/golang.org/x/text).

Thanks to the Linux security community for what they have shared over the years.

## License

GNU AGPL v3: see [LICENSE](LICENSE).
