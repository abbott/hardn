# Using hardn

## The menu

`sudo hardn` opens the main menu. Above the options it shows the risk level and a status row for each of options 1 to 5 and for AppArmor: [The status rows](#the-status-rows). Each option shows the current state, and most configure it:

| Option | Displays | Configures |
|---|---|---|
| 1) User | users, their admin rights (`sudo` or `doas`), SSH keys, password state; under Account Review, accounts with an empty password, a password that never expires or no login in 90 days, each with the command to run | create a user, grant or remove admin rights, ask or don't ask a password for them, add or remove an SSH key |
| 2) SSH | root login, password login, the port, allowed users, sshd's brute-force limit, as sshd reads them; every account's SSH keys, with weak and shared ones marked | root login off or key-only, password login off or on, the SSH port, key-based authentication, moves hardn's old sshd settings into `00-hardn.conf` |
| 3) Firewall | `ufw` state, policy, rules, application profiles, and each listening port: open, blocked or local only | installs `ufw`, enables secure defaults (SSH let through, incoming denied, started at boot), disables it, applies profiles, limits repeated SSH connections. On Rocky, Alma, RHEL, CentOS Stream and Oracle: firewalld, without profiles or interfaces. On Proxmox: "View pve-firewall", read only |
| 4) Kernel | the kernel's network and kernel settings against hardn's baseline, each with its value and what it means | sets the ones below the baseline in one file, `/etc/sysctl.d/90-hardn.conf`, and at once; puts the earlier values back. [More](#kernel-settings) |
| 5) Auto Updates | whether the host updates itself | turns on unattended-upgrades (Debian, Ubuntu), apk-cron and crond (Alpine) or dnf-automatic's security updates (Rocky, Alma, RHEL, CentOS Stream, Oracle); turns off only what hardn turned on. Read only on Proxmox |
| 6) Run All | what `hardn.yml` would change | one plan for the user, root login, the firewall, kernel settings, automatic updates, DNS, log forwarding and time servers. Where the firewall and root login both change, root login waits for the next Run All |
| 7) Backup | backups on or off, the backup path, past backup runs | automatic backup of modified configuration files, changes the path |
| 8) Configuration | a menu of four screens, below | nameservers, time servers and log forwarding |
| 9) System Details | OS, kernel, the time service and the clock, addresses, listening ports, users, CPU, disk, memory, last logins | nothing |
| 10) Logs | `/var/log/hardn.log` | nothing |

_The hardn status for **AppArmor** only displays current state, nothing more._

`8) Configuration` opens a menu of its own:

| Option | Displays | Configures |
|---|---|---|
| 1) hardn.yml | which `hardn.yml` hardn read and each key's value | nothing |
| 2) DNS Settings | what manages `/etc/resolv.conf`, the nameservers in use | sets nameservers (custom, or Cloudflare, Quad9, Google); where NetworkManager, resolvconf or a container manages DNS, it only reports |
| 3) NTP Settings | the service that keeps the host's time, whether the clock is in sync, the servers it uses and where each is set | makes that service use `hardn.yml`'s servers in place of its own; puts its own back. [More](#ntp-settings) |
| 4) Syslog Settings | the host's logger, the syslog servers it sends logs to, by whose file, and the servers `hardn.yml` names | sends every log to `hardn.yml`'s servers over UDP, TCP or TLS; stops only what hardn set. [More](#syslog-settings) |

### The status rows

```
  [+] User                 + non-root, sudo
                             ~ No password to become root
  [ ] SSH                  Not Configured password auth on
                             ~ Default port (22)
  [+] Firewall             + deny policy
  [ ] Kernel               Not Configured 6 to set
  [+] Auto Updates         + unattended-upgrades
  [+] AppArmor             + 10 profiles enforced

   -  Syslog               192.0.2.50:514 TCP
                             ~ Unreachable
```

- **`+` means Configured.** A row that needs work keeps its word: Partial, Not Configured, Not Applicable or Unknown. The same on every screen's own row. `hardn check`, the JSON and the Markdown report keep the word "Configured".
- **A line starting with `~`** under a row says what else lowers the risk level, or what hardn could not read.
- **SSH** is root login, password login and the port in one row.
  - All three set: `+ key-only, no root, port 2230`.
  - Only the port left on 22: `Partial default port (22)`.
  - Root or password login not set: the worse of the two and what is off, with `~ Default port (22)` under it where the port is 22.
  - `hardn check`, the JSON and the report keep the three controls, `ssh.login`, `ssh.auth` and `ssh.port`.
- **Kernel** is on every host: "6 to set", "6 items applied" once hardn's file sets them, "baseline default" where the host met the baseline as it was, and Not Applicable in a container. It counts 5 points of the [risk level](engine/risk.md).
- **DNS, NTP and Syslog have a row only with a problem**, under the others: the host's value, and the problem under it. For example nameservers that are not `hardn.yml`'s, a clock not in sync, or a syslog server that is unreachable. No time service, and no log forwarding, are a problem only where `hardn.yml` names servers for it. These rows never change the risk level.

### Syslog Settings

Syslog Settings sends every message the host logs to each syslog server `hardn.yml` names ([`syslogServers`](configuration.md#syslog), one to four). The screen has no form: the servers are typed in the file.

- **Set servers** makes hardn's own file, `/etc/rsyslog.d/60-hardn-forward.conf`, hold exactly `hardn.yml`'s list, has rsyslogd check it (`rsyslogd -N1`), restarts rsyslog, then logs one line, `log forwarding set`, for you to find on each server. Adding or removing a server is the same one change. A failed check or restart puts everything back. On a host with journald alone it installs rsyslog first (not on Proxmox, where hardn installs no package); for TLS, `rsyslog-gnutls`. On Alpine it adds one `-R <server>:<port>` for each server, and `-L`, to busybox syslogd's options.
- **A list hardn can't set whole is refused**, before anything is asked: an entry that is not valid, a CA file that is not there, a TCP or TLS server on Alpine.
- **Stop forwarding** takes out every server hardn set, and nothing else, and restarts the logger. rsyslog stays installed.
- **Servers set by a file hardn didn't write are shown, never changed.** Set servers is then unavailable.
- **Each Server row** ends in its connection: Open, or unreachable, for TCP and TLS. The host can't know what the server received: look for the test line there. UDP has no connection: its row says losses are unseen.
- **One server unreachable makes the row Partial**, "1 of 3 unreachable". Where two servers given by name share a port and one connection is open, hardn can say how many are open, not which: "1 of 2 open".
- **On the main menu** a Syslog row shows only with a problem: a server unreachable, servers that are not `hardn.yml`'s, settings hardn can't read, or no forwarding where `hardn.yml` names a server. It never changes the risk level.
- It forwards the host's logs, not hardn's audit log (`/var/log/hardn/audit.jsonl`).

### NTP Settings

NTP Settings shows which service keeps the host's time (chrony, systemd-timesyncd, or busybox ntpd on Alpine), whether it runs, whether the clock is in sync and to which server. It sets the servers `hardn.yml` names ([`ntpServers`](configuration.md#ntp)) on that service. The screen has no form: the servers are typed in the file.

- **Set servers** makes the service use your servers in place of its own. Servers handed out by DHCP stay. What is written differs by service: [platforms.md](platforms.md#ntp).
- **Remove servers** undoes only what hardn set: the host's own servers are used again, each file as it was.
- **hardn installs, starts and swaps no time service.** With none, or one that is not running, Set servers is unavailable. In a container it is too: the host keeps the time.
- **Servers set by a file hardn didn't write are shown, never changed.** Set servers is then unavailable.
- **hardn's servers are plain NTP.** Where the host's own are authenticated (NTS, Ubuntu 26.04's default), the review says so before you answer.
- **Clock** is the service's or the kernel's own word, read again after each change. Right after a restart it can read "Not in sync" for a while.
- **On the main menu** an NTP row shows only with a problem: the clock not in sync, the service not running, servers that are not `hardn.yml`'s, or no time service where `hardn.yml` names servers. It never changes the risk level.

### Kernel Settings

Kernel Settings shows 19 settings of the kernel (sysctl): redirects, source routing, SYN cookies, the reverse-path filter, kernel addresses and the kernel log, address randomizing, process tracing, BPF, and link and `/tmp` protection. Each row has its value, what it means and, below hardn's baseline, what hardn sets. A row reads every network interface and shows the weakest.

- **Apply settings** writes only the settings below the baseline to hardn's own file, `/etc/sysctl.d/90-hardn.conf`, each under the value it had, sets each with `sysctl -w`, and reads each back. If one doesn't take, the file and every value are put back.
- **Remove settings** deletes the file and sets each earlier value again.
- **A stricter value is never lowered**, and hardn never changes forwarding or IPv6 router advertisements.
- **A host that routes** (forwarding on, or a Proxmox VE node) keeps Sending redirects and Reverse-path filter as they are. The rows say why.
- **A setting another file sets back** after hardn's (`99-sysctl.conf`) is shown with that file's name, and left. That is the way to keep a setting of your own.
- **With Process tracing set**, `gdb -p` and `strace -p` need root. The review says so before you answer.
- **In a container** the values are shown and nothing is set: the kernel is the host's.
- **On Alpine** with the `sysctl` service not enabled, the values are set now and not at boot. The screen says so, with the fix: `rc-update add sysctl boot`.
- **On the main menu** the Kernel row shows on every host, and counts 5 points of the risk level. **In Run All** only with [`kernel: true`](configuration.md#kernel) in `hardn.yml`.

What differs by distribution: [platforms.md](platforms.md#kernel).

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
| `-w, --configure-ufw` | Enable `ufw` as Firewall does. Refused on Proxmox, Rocky, Alma, RHEL, CentOS Stream and Oracle |
| `-g, --configure-dns` | Set hardn.yml's nameservers |
| `-r, --run-all` | Run All's plan from hardn.yml. Can't be combined with `-c`, `-d`, `-w` or `-g` (exit 2) |
| `-n, --dry-run` | Dry run: show the plan, change nothing |
| `-p, --print-logs` | Print `/var/log/hardn.log`. On its own, it doesn't need root |
| `-v, --version` | Print the version, commit, build date and repository; as root with `checkReleases` on, a newer release too |
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
| `hardn check` | Compare the host with its saved baseline: one line for each difference. `--accept` saves the host as the baseline. `--output json` prints the result as JSON. See [hardn check](#hardn-check) |
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

## hardn check

`hardn check` tells you when a host is no longer as you left it. It reads the host, changes nothing and makes no network request.

```sh
sudo hardn check --accept       # save the host as it is now: the baseline
sudo hardn check                # compare the host with the baseline
sudo hardn check --output json  # the same, as JSON
```

The baseline is `/var/lib/hardn/baseline.json`, root's alone. Each difference is one line, with what it was and what it is:

```text
SSH password login: was no, is yes
Firewall rule added: allow in 8080/tcp
Admin added: carol
```

| Compared | What |
|---|---|
| Users | who can become root (`sudo`, `doas`), accounts with uid 0, their SSH keys (type and fingerprint), accounts with an empty password |
| SSH | root login, password login, the port and listeners, allowed users, as `sshd -T` prints them |
| Firewall | on or off, the incoming policy, whether it starts at boot, every rule (ufw's and their order, or each firewalld zone's) |
| Listening ports | each port the firewall lets in from outside. A port on loopback alone is never counted. It needs `ss`: without it, ports aren't compared, and the result says so. After installing `ss`, run `--accept` again |
| DNS | the nameservers in use |
| Auto updates | on or off |
| Syslog servers | each server the host forwards its logs to, hardn's or not, with its port and protocol. Connections aren't compared |

Not compared: AppArmor, sysctl, packages, rules written straight into nftables, a cloud provider's firewall, sshd's `Match` blocks, firewalld's rich rules.

| Status | Meaning |
|---|---|
| `0` | No difference; or, with `--accept`, the baseline is saved |
| `1` | An error, not root, no baseline yet, or a baseline hardn won't read (not root's alone, or from another version of hardn) |
| `2` | A usage error: an operation flag, `-n`, or an `--output` other than `json` |
| `3` | At least one difference |
| `130` | Interrupted |

- **A safer change counts too**, such as the firewall turned on: the host isn't as you left it.
- **What hardn can't read is "not established"**, never assumed. It is a difference only when the baseline had it, or the other way round.
- **After hardn applies a change**, its result says to run `hardn check --accept`. Until you do, `hardn check` reports that change.
- `hardn check` writes nothing. `--accept` writes the baseline and one line in the audit log, with who ran it.

From cron, every hour. It prints only when the host differs or the check fails, and cron mails what it prints:

```sh
# /etc/cron.d/hardn-check
MAILTO=root
0 * * * * root out=$(/usr/local/bin/hardn check 2>&1) || echo "$out"
```

**Known false alarms:**

- **DHCP rewrites the nameservers.** Where a DHCP client manages `/etc/resolv.conf`, a new lease can change them.
- **A change you made on purpose**, until you run `sudo hardn check --accept`.
- **IPv6 switched on**, which gives each ufw rule an IPv6 twin.

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
`hardn system-details --output json` and `hardn check --output json` each print one JSON document on stdout. [reference/output.md](reference/output.md) describes its fields and the stable error and finding codes.

### Markdown report
`sudo hardn system-details --output markdown > web-01.md` writes a report to hand to an auditor or a manager, as one Markdown document:

- the host, its platform and whether hardn supports it;
- the risk level, and each thing that raised it;
- each control's status, with the evidence it was read from (SSH Login, SSH Auth and SSH Port each on its own row, and "Configured" in words);
- the findings, and any check that failed;
- System Details: addresses, users, CPU and load, disk, memory, last login.

It changes nothing and exits 0 whatever it found. It doesn't need root, but without root what only root can read shows Unknown, and the report says so. It reads hardn.yml, for the DNS row, which the report lists whether it has a problem or not; a hardn.yml that doesn't load is an error (exit 1).

The document has no colour. Text read from the host is escaped or shown as code, so a hostname or a file's content can't add formatting, links or HTML to the report.

### Plain output and colour
Output off a terminal is plain: no colour, no animation, ASCII symbols. `--color=always` colours it anyway, and wins over `--plain`, `NO_COLOR` and `TERM=dumb` too. hardn's messages go to stderr, so stdout holds only a command's output. The environment variables match the flags: `NO_COLOR` (any non-empty value turns colour off), `HARDN_PLAIN=1`, `HARDN_ASCII=1`, `HARDN_THEME=dark|light|auto`. `--color` and `--theme` win over their variables; `--plain` and `--ascii` can only turn plain or ASCII on.

> [!TIP]
> `TERM=linux`, as under Proxmox VE's `pct console`, gets basic colours and ASCII symbols. Where your terminal can show more, add `[ "$TERM" = linux ] && alias hardn='TERM=xterm-256color LANG=C.UTF-8 hardn'` to `/root/.profile`.

### Logs
- `/var/log/hardn/audit.jsonl` gets one JSON line for each change hardn plans: from an operation flag, a screen, or `hardn confirm`'s clean-up. That includes changes applied, refused, declined, rolled back or interrupted, and dry runs. Each `hardn confirm` and `hardn rollback` adds a line too, with who ran it, the change's id and how it ended; so does the rollback of a change nobody confirmed, and each `hardn check --accept`. A plain `hardn check` adds none. It is root's alone. Read it with `sudo tail -n 5 /var/log/hardn/audit.jsonl`.
- `/var/log/hardn.log` is hardn's log, including the commands a rollback runs. `10) Logs` and `-p` show it. A dry run doesn't write to it; each plan it shows adds one line to the audit log, and nothing else is written.
- hardn rotates both. Over 10 MiB, `hardn.log` becomes `hardn.log.1` and a new file starts; four older copies are kept (`.1` to `.4`). `audit.jsonl` does the same over 5 MiB, with nine. Copies are plain files, not compressed. `10) Logs` and `-p` to a terminal show the current file and name `hardn.log.1` where there is one. `hardn check` never rotates.

## hardn.yml

hardn reads `username`, `sshKeys`, `adminNoPassword`, `dryRun`, `enableBackups`, `backupPath`, `nameservers`, `sshPort` (the default for Change SSH port), `sshConfigFile` (where hardn's old settings are), `confirmTimeout`, `checkReleases`, `ufwAppProfiles`, `ufwTrustedInterfaces`, `syslogServers`, `ntpServers`, `kernel`, and Run All's switches: `enableUnattendedUpgrades`, `enableUfwSshPolicy`, `configureDns` and `disableRootSSH`.

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

