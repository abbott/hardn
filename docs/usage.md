# Using hardn

## The menu

`sudo hardn` opens the main menu. Each option shows the current state, and most configure it:

| Option | Displays | Configures |
|---|---|---|
| 1) User Management | users, their admin rights (`sudo` or `doas`), SSH keys, password state; under Account Review, accounts with an empty password, a password that never expires or no login in 90 days, each with the command to run | create a user, grant or remove admin rights, ask or don't ask a password for them, add or remove an SSH key |
| 2) SSH Login | root login, password login, the port, allowed users, sshd's brute-force limit, as sshd reads them; every account's SSH keys, with weak and shared ones marked | root login off or key-only, password login off or on, the SSH port, key-based authentication, moves hardn's old sshd settings into `00-hardn.conf` |
| 3) Firewall | `ufw` state, policy, rules, application profiles, and each listening port: open, blocked or local only | installs `ufw`, enables secure defaults (SSH let through, incoming denied, started at boot), disables it, applies profiles, limits repeated SSH connections. On Rocky, Alma and RHEL: firewalld, without profiles or interfaces. On Proxmox: "View pve-firewall", read only |
| 4) DNS | what manages `/etc/resolv.conf`, the nameservers in use | sets nameservers (custom, or Cloudflare, Quad9, Google); where NetworkManager, resolvconf or a container manages DNS, it only reports |
| 5) Auto Updates | whether the host updates itself | turns on unattended-upgrades (Debian, Ubuntu), apk-cron and crond (Alpine) or dnf-automatic's security updates (Rocky, Alma, RHEL); turns off only what hardn turned on. Read only on Proxmox |
| 6) Log Forwarding | the host's logger, the syslog server it sends logs to, by whose file, and the server `hardn.yml` names | sends every log to `hardn.yml`'s server over UDP, TCP or TLS; stops only what hardn set. [More](#log-forwarding) |
| 7) Run All | what `hardn.yml` would change | one plan for the user, root login, the firewall, DNS, automatic updates and log forwarding. Where the firewall and root login both change, root login waits for the next Run All |
| 8) Backup | backups on or off, the backup path, past backup runs | automatic backup of modified configuration files, changes the path |
| 9) Configuration | which `hardn.yml` hardn read and each key's value | nothing |
| 10) System Details | OS, kernel, addresses, listening ports, users, CPU, disk, memory, last logins | nothing |
| 11) Logs | `/var/log/hardn.log` | nothing |

_The hardn status for **AppArmor** only displays current state, nothing more._

### Log forwarding

Log Forwarding sends every message the host logs to the syslog server `hardn.yml` names ([`logForwarding`](configuration.md#log-forwarding)). The screen has no form: the server is typed in the file.

- **Set server** writes hardn's own file, `/etc/rsyslog.d/60-hardn-forward.conf`, has rsyslogd check it (`rsyslogd -N1`), restarts rsyslog, then logs one line, `log forwarding set`, for you to find on the server. A failed check or restart puts everything back. On a host with journald alone it installs rsyslog first (not on Proxmox, where hardn installs no package); for TLS, `rsyslog-gnutls`. On Alpine it adds `-R <server>:<port> -L` to busybox syslogd's options.
- **Stop forwarding** takes out only what hardn set, and restarts the logger. rsyslog stays installed.
- **A server set by a file hardn didn't write is shown, never changed.** Set server is then unavailable.
- **Connection** says whether a TCP or TLS connection to the server is open. The host can't know what the server received: look for the test line there. UDP shows nothing.
- **On the main menu** the Log Forwarding row shows only where `hardn.yml` names a server or the host forwards. It never changes the risk level.
- It forwards the host's logs, not hardn's audit log (`/var/log/hardn/audit.jsonl`).

From 90 days before a release's vendor support ends, the main menu says so, and every plan carries a warning. It changes nothing hardn does. The dates: [platforms.md](platforms.md#end-of-support).
### Account Review

User Management's `3) Account Review` lists accounts to look at. hardn changes none of them: each row is the command to run.

| Section | Lists | Command |
|---|---|---|
| Empty password | accounts whose password field in `/etc/shadow` is empty. A locked account (`!`) or one with `*` isn't listed | `passwd -l NAME` locks it |
| Password never expires | accounts with a password and no expiry date | `chage -M 365 NAME` sets one |
| No login in 90 days | users (uid 1000 or more, a login shell, not expired) with no login in 90 days, or none on record ("Never") | `usermod -e 1 NAME` disables the account |

- **An empty password also shows on the main menu**, as "Empty password" under User, and lowers the security level.
- **"This host keeps no login record"** means hardn found no last-login record (`/var/log/lastlog` or lastlog2's database). It then lists no one: it never guesses.
- **"The login record has no login in 90 days"** means the record is there but may no longer be written. It lists no one then too.
- **"Can't read the login record"** means the record is there but `lslogins` (util-linux) is missing or failed.

## Flags and commands

`hardn` needs root, except `-p` alone, `--version`, `--help`, `completion`, `config` and `system-details`. Run it with `sudo hardn` or `doas hardn`.

| Flag | What it does |
|---|---|
| `-f, --config <path>` | Read this hardn.yml (otherwise `HARDN_CONFIG`, then the search path in [configuration.md](configuration.md)) |
| `-u, --username <name>` | The user `-c` creates; also the default for Create and Run All |
| `-c, --create-user` | Create the user with admin rights and every key in hardn.yml's `sshKeys`. Their admin rights ask a password: set one with `passwd <name>` |
| `-d, --disable-root` | Turn root's SSH login off (key-only on Proxmox). Refused unless another admin can still log in and become root |
| `-w, --configure-ufw` | Enable `ufw` as Firewall does. Refused on Proxmox, Rocky, Alma and RHEL |
| `-g, --configure-dns` | Set hardn.yml's nameservers |
| `-r, --run-all` | Run All's plan from hardn.yml. Can't be combined with `-c`, `-d`, `-w` or `-g` (exit 2) |
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

With no operation flag, `hardn` opens the main menu. Operation flags can be combined, and run in the order `-c`, `-d`, `-w`, `-g`, `-p`.

| Command | What it does |
|---|---|
| `hardn completion <shell>` | Print a tab-completion script for bash, zsh, fish or powershell. Learn more: `hardn completion bash --help` |
| `hardn config` | Print which hardn.yml hardn reads and each key's value. It doesn't need root, but it shows the file for the user who runs it: use `sudo hardn config` to see what `sudo hardn` reads |
| `hardn confirm` | Keep the change that is waiting for confirmation. Run it from a new SSH connection. At the console, it confirms with a warning. When run from a new SSH connection after a port change, it also closes the old port |
| `hardn rollback <id>` | Undo that change now, or finish a rollback that stopped partway |
| `hardn rollback --abandon <id>` | Give up a rollback that can never finish. The host stays as the rollback left it |
| `hardn rollback --boot` | What hardn's boot unit runs. You don't run it yourself |
| `hardn system-details` | Print the System Details report. `--output json` prints it as JSON; `--output markdown` prints a [report to hand over](#markdown-report). Save it with `> file` |

**On a terminal, and off one (scripts, cron).** Off a terminal, hardn never asks a question.

| Flag | On a terminal | Off a terminal |
|---|---|---|
| `-c`, `-g` | Applies the change, unasked | Applies it only with `--yes` |
| `-w`, `-d` | A change that could lock you out: type the hostname. Any other: applied, unasked | A change that could lock you out: refused (exit 1), even with `--yes`. Any other: only with `--yes` |
| `-r` | Type the hostname, for any plan with changes, `--yes` or not | A change that could lock you out: refused (exit 1), even with `--yes`. Any other: only with `--yes` |

To ask for the hostname, hardn needs stdin and stdout both on a terminal: with the output piped or redirected, a change that could lock you out is refused, and `-r` needs `--yes` for any other. Otherwise only stdin counts.

Where `--yes` is needed and not given, hardn shows the plan, changes nothing, and exits 2. A plan with nothing to change exits 0.

```sh
sudo hardn -n -r                # show Run All's plan
sudo hardn -u george -c         # create george, an admin with hardn.yml's keys
sudo hardn -c -d                # create the user, then turn root login off
sudo hardn -g --yes             # from cron: set the nameservers
sudo hardn system-details --output json > web-01.json
sudo hardn system-details --output markdown > web-01.md
```

## Dry run

`sudo hardn -n` (or `dryRun: true` in hardn.yml) shows each operational plan and changes nothing. Operation flags exit 3 when there are changes. `--dry-run=false` overrides the file.

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
`hardn system-details --output json` prints one JSON document on stdout. [reference/output.md](reference/output.md) describes its fields and the stable error and finding codes.

### Markdown report
`sudo hardn system-details --output markdown > web-01.md` writes a report to hand to an auditor or a manager, as one Markdown document:

- the host, its platform and whether hardn supports it;
- the risk level, and each thing that raised it;
- each control's status, as the main menu shows it, with the evidence it was read from;
- the findings, and any check that failed;
- System Details: addresses, users, CPU and load, disk, memory, last login.

It changes nothing and exits 0 whatever it found. It doesn't need root, but without root what only root can read shows Unknown, and the report says so. It reads hardn.yml, for the DNS row; a hardn.yml that doesn't load is an error (exit 1).

The document has no colour. Text read from the host is escaped or shown as code, so a hostname or a file's content can't add formatting, links or HTML to the report.

### Plain output and colour
Output off a terminal is plain: no colour, no animation, ASCII symbols. `--color=always` colours it anyway, and wins over `--plain`, `NO_COLOR` and `TERM=dumb` too. hardn's messages go to stderr, so stdout holds only a command's output. The environment variables match the flags: `NO_COLOR` (any non-empty value turns colour off), `HARDN_PLAIN=1`, `HARDN_ASCII=1`, `HARDN_THEME=dark|light|auto`. `--color` and `--theme` win over their variables; `--plain` and `--ascii` can only turn plain or ASCII on.

> [!TIP]
> `TERM=linux`, as under Proxmox VE's `pct console`, gets basic colours and ASCII symbols. Where your terminal can show more, add `[ "$TERM" = linux ] && alias hardn='TERM=xterm-256color LANG=C.UTF-8 hardn'` to `/root/.profile`.

### Logs
- `/var/log/hardn/audit.jsonl` gets one JSON line for each change hardn plans: from an operation flag, a screen, or `hardn confirm`'s clean-up. That includes changes applied, refused, declined, rolled back or interrupted, and dry runs. Each `hardn confirm` and `hardn rollback` adds a line too, with who ran it, the change's id and how it ended; so does the rollback of a change nobody confirmed. It is root's alone. Read it with `sudo tail -n 5 /var/log/hardn/audit.jsonl`.
- `/var/log/hardn.log` is hardn's log, including the commands a rollback runs. `11) Logs` and `-p` show it. A dry run doesn't write to it; each plan it shows adds one line to the audit log, and nothing else is written.

## hardn.yml

hardn reads `username`, `sshKeys`, `adminNoPassword`, `dryRun`, `enableBackups`, `backupPath`, `nameservers`, `sshPort` (the default for Change SSH port), `sshConfigFile` (where hardn's old settings are), `confirmTimeout`, `checkReleases`, `ufwAppProfiles`, `ufwTrustedInterfaces`, `logForwarding`, and Run All's switches: `enableUnattendedUpgrades`, `enableUfwSshPolicy`, `configureDns` and `disableRootSSH`.

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
> See [configuration.md](configuration.md) for every key, where hardn looks for the file, and how to keep `HARDN_CONFIG` under sudo. Example: [hardn.yml.example](../hardn.yml.example)

