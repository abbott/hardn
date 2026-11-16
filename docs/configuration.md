# Hardn Configuration

This document explains how to configure `hardn`, the simple hardening tool for Linux.

## Configuration File Locations

`hardn` searches for a YAML configuration file in the following order:

1. Path specified with `--config` or `-f` flag
2. Environment variable `HARDN_CONFIG` (if set)
3. `/etc/hardn/hardn.yml` (system-wide configuration)
4. `~/.config/hardn/hardn.yml` (XDG Base Directory specification)
5. `~/.hardn.yml` (traditional dot-file in home directory)
6. `./hardn.yml` (the directory `hardn` runs in)

The flags that change the host (`-c`, `-d`, `-g`, `-w`, `-r`) say which file they read on stderr: "Using configuration from: <path>". The main menu shows nothing before it starts; its `8) Configuration`, then `1) hardn.yml`, shows which file was read, or run `sudo hardn config`.

## Creating a Configuration File

You can create a configuration file in several ways:

### 1. Interactive Creation

On first run, `hardn` will offer to create a default configuration file interactively if no existing config is found.

Say yes and it asks seven things: a username, the SSH port, automatic updates, an SSH key, an NTP server, a syslog server (then its protocol), and last "Check for new hardn releases, once a day? [Y/n]:", Enter yes. The file it writes holds `checkReleases` either way.

The NTP and syslog questions take one server each, and Enter skips them. The syslog server is `host` or `host:port`, port 514 (6514 for TLS) unless given. Its protocol is asked next: udp, tcp or tls, Enter tcp; tls asks a CA file and is refused without one. An answer hardn can't set, such as an IPv6 address, is refused and asked again. The answers only fill [`ntpServers`](#ntp) and [`syslogServers`](#syslog): Run All sets them, or Set servers on each screen. On Alpine's default logger a TCP server is refused when it is set: answer udp. At the main menu, say no and it asks that one question next, when it is due: see [`checkReleases`](#checkreleases-new-releases).

### 2. Manual Creation

Copy the example configuration and customize it:

```bash
# For system-wide configuration (as root)
sudo mkdir -p /etc/hardn
sudo cp /etc/hardn/hardn.yml.example /etc/hardn/hardn.yml
sudo nano /etc/hardn/hardn.yml

# For user configuration
mkdir -p ~/.config/hardn
cp /etc/hardn/hardn.yml.example ~/.config/hardn/hardn.yml
nano ~/.config/hardn/hardn.yml
```

### 3. Using the Default Configuration

If you decline, hardn goes on with its defaults and writes no hardn.yml. A live run still writes `/etc/hardn/hardn.yml.example`.
<!-- show/link default values -->

## Editing from the menu

`8) Configuration`, then `1) hardn.yml`, then `1) Edit a setting` changes one key at a time. hardn checks what you type and asks again if it can't be used, shows the lines that would change, and asks before saving. The rest of the file and its comments stay, and the file is backed up first.

- **It edits** `username`, `sshKeys`, `sshPort`, `disableRootSSH`, `enableUfwSshPolicy`, `configureDns`, `syslogServers`, `ntpServers`, `kernel`, `confirmTimeout`, `checkReleases` and `enableAutoUpdates`.
- **Lists take one entry a save:** Add or Remove. An SSH key is shown by type, comment and fingerprint. A syslog server asks its address, protocol, port and, for TLS, its CA file.
- **Change protocol** changes one syslog server's protocol. A port left at the default follows the new protocol's; another port is kept.
- **It only saves.** A line after the save says where the key acts: Run All, NTP Settings or Syslog Settings. Saving `sshPort` never changes sshd: change the port under `2) SSH`.
- **Other keys keep their own screen:** `nameservers` (DNS Settings), the firewall's lists (Firewall), `enableBackups` and `backupPath` (Backup). `dryRun`, `adminNoPassword` and `sshConfigFile` are edited by hand.
- **`-u` and `-n` are never saved.** In a dry run the lines are shown and nothing is written.
- **With no hardn.yml there is nothing to edit:** start hardn again to create one.
- **On Alpine,** where the logger is busybox syslogd, a TCP or TLS syslog server gets a line saying it is UDP only, and to use Change protocol.

## Environment Variable

You can set the `HARDN_CONFIG` environment variable to specify a configuration file location. This is particularly useful in automation scripts or when you want to maintain multiple configurations.

### Using with sudo or doas

`sudo` and `doas` drop most environment variables, `HARDN_CONFIG` among them, so `sudo hardn` doesn't see it. Any of these gets hardn the file:

```bash
# Name the file
sudo hardn -f "$HARDN_CONFIG"
```

```bash
# Pass the variable on sudo's command line (needs a rule for ALL commands, the usual admin rule)
sudo HARDN_CONFIG="$HARDN_CONFIG" hardn
```

```bash
# Under doas
doas env HARDN_CONFIG="$HARDN_CONFIG" hardn
```

Or put the file at `/etc/hardn/hardn.yml`, where hardn looks first.

- **A line an earlier hardn (before the next release) wrote still works.** `/etc/sudoers.d/hardn-env-<user>` keeps `HARDN_CONFIG` under sudo until you delete it: `sudo rm /etc/sudoers.d/hardn-env-<user>`. Under doas, hardn's rule in `/etc/doas.d/zz-hardn-<user>.conf` keeps its `setenv { HARDN_CONFIG }` while User Management changes the rule, and loses it when Remove admin rights removes the rule.

#### **Persistent Across Sessions**
To make configuration persist across reboots and new shell sessions, add it to your shell's startup file by issuing the following command for your respective shell:

##### **For Bash**

```bash
# For persistent configuration, add to your shell profile
echo 'export HARDN_CONFIG=$HOME/.config/hardn/hardn.yml' >> ~/.bashrc
# Reload the file to apply the changes
source ~/.bashrc 
```
##### **For Zsh**

```bash
echo 'export HARDN_CONFIG=$HOME/.config/hardn/hardn.yml' >> ~/.zshrc
# Reload the file to apply the changes
source ~/.zshrc
```

##### **For Fish**

```fish
set -Ux HARDN_CONFIG $HOME/.config/hardn/hardn.yml
```

If the environment variable is unavailable, restart your terminal.

<!-- #### **Temporary (Current Session Only)**
If you just need the variable for the current terminal session, run:

```bash
export HARDN_CONFIG=$HOME/hardn.yml
```

This setting will reset when you close the terminal or log out. -->


## Command Line Flags

You can specify a different configuration file with the `-f` flag or environment variable:

```bash
# Using command line flag
sudo hardn -f /path/to/custom-config.yml
```

## Configuration Options

For a complete list of configuration options, see the example configuration file at `/etc/hardn/hardn.yml.example`.

**Keys nothing reads are warned.** `dmzSubnet`, the package lists (`linuxCorePackages`, `linuxDmzPackages`, `linuxLabPackages`, `pythonPackages`, `nonWslPythonPackages`, `pythonPipPackages`, `alpineCorePackages`, `alpineDmzPackages`, `alpineLabPackages`, `alpinePythonPackages`), the repository keys (`debianRepos`, `proxmoxSrcRepos`, `proxmoxCephRepo`, `proxmoxEnterpriseRepo`, `proxmoxPackagePatterns`, `alpineTestingRepo`), `useUvPackageManager`, `lang`, `language`, `lcAll`, `tz` and `pythonUnbuffered` are still accepted. Where hardn.yml sets one to other than its default, hardn warns, such as "hardn.yml's dmzSubnet isn't used".

Here are the main configuration sections in YAML:

### Basic Configuration

```yaml
username: "george"                # Default username to create
dryRun: false                       # Preview changes without applying them
enableBackups: true                 # Backup files before modifying them
backupPath: "/var/backups/hardn"    # Path to store backups
checkReleases: false                # true: ask GitHub for the latest release's tag
```

#### `dryRun`: dry run

`dryRun: true` starts `hardn` as a dry run: changes are shown, not applied. `-n` does the same for one run.

- **User Management, SSH, Firewall, Backup, Auto Updates and Run All open.** Each of their changes stops at its plan, adding and removing a Firewall profile too.
- **The flag wins.** `-n` turns dry run on, whatever the file says. `--dry-run=false` turns it off for one run, whatever the file says.
- **Operation flags show their plan** in a dry run, as with `-n`: `-r`, `-c`, `-d`, `-g` and `-w` exit 3 when there are changes. To apply, add `--dry-run=false`.
- **`hardn confirm` and `hardn rollback` still run** under the file's `dryRun: true`: they keep or undo a change already made. `-n` refuses them.
- **Set it here, not in the menu.** The menu's saves write `dryRun` as the file had it, never the mode of the session.
- **The main menu says so.** Beside "Select an option" it shows "Mode Dry run (hardn.yml)", or "(-n)" when the flag set it.

> **Changed in v0.4.0.** `dryRun` used to be ignored: `hardn` ran live whatever the file said. It is now read. A `dryRun: true` an earlier `hardn` wrote now starts a dry run. The main menu's Dry-Run item (6) wrote one to `./hardn.yml`, as did any menu save made under `-n`. If changes are no longer applied, check the file `hardn` names at start for `dryRun: true`. Set it to `false`, or run with `--dry-run=false`. The Dry-Run item is now removed, and Run All no longer turns dry run on or off.

#### `enableBackups` and `backupPath`: the Backup screen

The main menu's Backup (7) shows and changes these two settings.

- **Showing it writes nothing.** With backups on, it used to write a test file to the backup path each time.
- **Verify Directory writes nothing.** It checks that only root can change the path.
- **Some paths are refused** as you type them: a relative path, or one under a directory others can change, such as `/tmp`.
- **Each change is shown first.** Confirm it, and only that setting is saved, in the file `hardn` loaded. Without a loaded file, nothing can be saved.
- **A path change backs up under the new path**, as does every change after it.
- **Backups off still keeps what a rollback needs.** Changes from the DNS and Backup screens and `-g` still copy the files they change to the backup path.
- **Backup Runs lists the backups in the path in use**, newest first. It only reads.

**`logFile` is retired.** hardn always writes and reads `/var/log/hardn.log`. A file that sets `logFile` to another path is still accepted, and hardn warns: "hardn.yml's logFile, <path>, isn't used".

#### `username`, `adminNoPassword` and `sshKeys`: User Management

```yaml
username: "george"                  # Create's default name; -c's user
adminNoPassword: false              # true: the new user's admin rights ask no password
sshKeys:                            # -c adds them all; Create's default is the first
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... george@example.com"
```

The main menu's User (1) shows the users, their admin rights and SSH keys, and changes them.

- **In User Management, `username` and `sshKeys` are only Create's defaults:** they never choose which user a change is about, and no change saves them.
- **`hardn -c` creates a user with admin rights**, named by `-u`, else by `username`. It asks nothing on a terminal; off one it needs `--yes`, otherwise it shows the plan and exits 2. The user has no password, and every key in `sshKeys`. Their admin rights ask their password, so they can't use sudo (or doas) until root runs `passwd <name>`. With `-n` it shows the plan. A name already taken, or a key that isn't one, is refused, exit status 1. `-c -d` creates the user first, then runs `-d`, which goes on if another admin can still log in and become root; the new user counts only with `adminNoPassword: true`.
- **`-c` no longer changes sshd_config.**
- **Admin rights without a password are opt-in.** By default the rule hardn writes for a new admin asks their password (sudo's plain rule, doas's `persist`). `adminNoPassword: true` makes `-c` and Run All write the no-password rule (`NOPASSWD`, `nopass`), and makes no the default of Create User's "Require password for admin rights?".
- **Create User offers to set the password.** After yes to "Require password for admin rights?" it asks "Set <name>'s password now?" and runs `passwd <name>` on your terminal once the user is created. The password is typed to `passwd`, never to hardn. `-c` and Run All set none.
- **Grant admin rights asks the user's password too.** For a user with none, the rights wait until `passwd <name>` sets one. Manage User's "Don't ask password" removes the question for one user.
- **`sudoNoPassword` is retired.** Every hardn.yml an older hardn saved holds `sudoNoPassword: true`, so hardn never takes it as the opt-in. `false` warns: "hardn.yml's sudoNoPassword isn't used".
- **Users hardn already created keep their no-password rule.** To tighten one, run `passwd <name>`, then choose Manage User's "Ask for password".

### Network Configuration

```yaml
nameservers:                        # DNS servers to configure
  - "1.1.1.1"
  - "1.0.0.1"
```

### SSH Configuration

```yaml
sshPort: 22                         # SSH's default for Change SSH port
permitRootLogin: "no"               # not used
                                    # true and false still work but are deprecated: write "no" or "yes"
sshAllowedUsers:                    # not used
  - "george"
sshListenAddress: "0.0.0.0"         # not used
sshKeyPath: ".ssh_%u"               # not used
sshConfigFile: "/etc/ssh/sshd_config.d/hardn.conf"  # the old file Move old settings moves
```

The main menu's SSH (2) shows root login, password login, the SSH port and who is allowed to log in, as sshd reads them, and changes all but the last.

- **It reads sshd, not hardn.yml.** Opening it changes nothing.
- **Each change asks you to type the hostname**, since it could lock you out. Run `sudo hardn confirm` from a new session to keep it; if you don't, it is rolled back.
- **Some changes are refused before anything is asked**, such as turning root login off where no other admin could log in by key. With hardn's old sshd settings, every change but Move old settings is refused until it moves them.
- **`hardn -d` turns root login off the same way.** On a terminal it asks you to type the hostname; 0 or q declines, exit status 3. Off one it is refused, exit status 1: run `hardn -d` from a terminal. With `-n` it shows the plan. Root login already off is nothing to change, exit status 0. It is refused where no other admin could log in afterwards, by key, or by password while password login is on, and become root. On Proxmox VE it makes root key-only, which the cluster needs.
- **SSH keys (item 6) lists every key sshd reads, by account**, from the files its `AuthorizedKeysFile` names. It changes nothing. It marks a weak key (DSA, or RSA under 2048 bits), a key more than one account holds, and a key this sshd refuses. A file it can't read, and lines that are no key, are shown as such. It doesn't read a key file that is a symlink inside the account's own `~/.ssh`, is over 1 MiB, or isn't a regular file. Over 10 keys it shows the keys to review; "All keys" lists the rest. A file a `Match` block names for one user isn't listed. To remove a key, use User Management.
- **`sshPort` is Change SSH port's default.** **`sshConfigFile` is read by Move old settings alone**, to find hardn's old file.
- **`permitRootLogin`, `sshAllowedUsers`, `sshListenAddress` and `sshKeyPath` aren't used.** Where hardn.yml sets one to other than its default, hardn warns, such as "hardn.yml's sshAllowedUsers isn't used". `permitRootLogin: "prohibit-password"` is warned too, as `"yes"` is; it is no longer refused.

#### `confirmTimeout`: the time to confirm

```yaml
confirmTimeout: "3m"                # seconds, such as 300, or a duration
```

A change that could lock you out rolls back unless `sudo hardn confirm` keeps it within this time. It is 3 minutes when unset; less than 60 seconds is refused. `--confirm-timeout` wins over it.

#### `checkReleases`: new releases

```yaml
checkReleases: true                 # off unless set or answered yes
```

Off, the default where no one answered, hardn makes no network request. On, the main menu says when a newer release is out, such as "v0.9.1 available", under the repository link. As root, `hardn --version` says it too, from the last check's answer.

- **One request a day at most**, to `https://api.github.com/repos/abbott/hardn/releases/latest`, while the main menu is open. The menu never waits for it: the answer shows the next time the menu is drawn.
- **Nothing about the host is sent.** GitHub sees the host's address and hardn's version (`User-Agent: hardn/<version>`).
- **Only the release's tag is read**, and shown only where it is a version such as `v0.9.1`.
- **"Release check failed"** means the last check got no answer; `/var/log/hardn.log` has the reason. hardn tries again a day later.
- **No request for a flag or a subcommand.** `--version` only reads the last answer, which root alone can read.
- **In a dry run too.** It changes nothing on the host but its own file. A dry run writes no log, so a check that fails there leaves no reason in it.
- **hardn doesn't update itself.** Install the release with [install.sh](install.md).
- The last answer is kept in `/var/lib/hardn/release.json`, root's alone.

**hardn asks.** Before the main menu, on a terminal, as root: "Check for new hardn releases, once a day? [Y/n]:". Enter is yes, n or no is no, and any other answer is taken as no, which hardn says. The end of the input or Ctrl-C is no answer. A flag, a subcommand and `hardn check` don't ask, nor does `-n`. The one exception: a flag run that creates hardn.yml asks it with the other first-run questions.

- **Creating hardn.yml:** it is the last question, and the file holds your answer.
- **A hardn.yml without the key**, such as one from an older install: hardn asks, then writes your answer to the file, yes or no. The rest of the file and its comments stay. If the file can't be edited in place, hardn says so and writes nothing: set the key by hand.
- **No hardn.yml:** your answer is kept in `/var/lib/hardn/release-ask.json`, root's alone. A yes turns the check on until a hardn.yml exists; then the file decides. To take a yes back, delete that file or set `checkReleases: false` in a hardn.yml.
- **A key that is set is never asked about again**, nor is a yes. After a no with no hardn.yml, or no answer, hardn asks again after 30 days or 15 opens of the main menu, whichever comes first.
- **`dryRun: true` in hardn.yml doesn't stop the question**, and the answer is still written to the file.
- **If `/var/lib/hardn` can't be written, hardn doesn't ask.** It couldn't keep the answer.

### Syslog

```yaml
syslogServers:
  - server: "192.0.2.50"            # IPv4 address or name
    port: 514                       # default 514; 6514 for tls
    protocol: tcp                   # udp, tcp or tls; default tcp
  - server: "logs.example.net"
    protocol: tls
    caFile: "/etc/ssl/example-ca.pem" # tls only
```

The syslog servers Syslog Settings (main menu `8) Configuration`, then 4) and Run All send the host's logs to: one to four, each sent every message. Nothing is set by default: with no list, or an empty one, hardn changes nothing.

- **`server`** is an IPv4 address or a host name. An IPv6 address isn't set yet.
- **`protocol`** is `udp`, `tcp` or `tls`, in lower case, and may differ from server to server. UDP and TCP are not encrypted, and UDP doesn't notice a lost message. On Alpine only `udp` is set: busybox syslogd has no other.
- **`tls`** needs the server's name, as its certificate has it, and **`caFile`**: the CA's certificate, a file already on the host, each TLS server's own. hardn checks the server's certificate against it and sends no client certificate.
- **A list hardn doesn't accept is refused whole**, saying which entry ("server 2: port is not between 1 and 65535"), and nothing is written. That is more than four servers, a block with no `server`, the same address and port twice (whatever the protocol), or a value that is not plain: only letters, digits, dots and hyphens in `server`, and letters, digits and `._+-/` in `caFile`. One TCP or TLS server on Alpine, or one missing CA file, refuses the list too: hardn never sets part of it.
- **Each server has its own queue**, in memory, so that one that is away does not hold up the others or the host's own log files: rsyslog keeps retrying it, and drops that server's messages once the queue is full.
- Edit the list under `8) Configuration`, hardn.yml, [Edit a setting](#editing-from-the-menu). Stop forwarding leaves it: remove the servers too, or the next Run All sets them again.

What changes on each distribution: [platforms.md](platforms.md#syslog).

### NTP

```yaml
ntpServers:
  - "192.0.2.10"
  - "192.0.2.11"
```

The time servers NTP Settings (main menu `8) Configuration`, then 3) and Run All set on the host's time service, in place of its own. Nothing is set by default: with none, hardn changes nothing.

- **One to four servers**, each an IPv4 address or a host name, in the order the service is given them. An IPv6 address isn't set yet. A name needs DNS before the clock is set.
- **A value hardn doesn't accept is refused**, and nothing is written: the names go into the service's settings, so only letters, digits, dots and hyphens. No options (`prefer`, `nts`): hardn writes plain NTP servers.
- Edit the list under `8) Configuration`, hardn.yml, [Edit a setting](#editing-from-the-menu). Remove servers leaves it: take the servers out too, or the next Run All sets them again.

What changes on each distribution: [platforms.md](platforms.md#ntp).

### Kernel

```yaml
kernel: true
```

`true` puts Kernel Settings (main menu `4) Kernel`) in Run All. Off by default: hardn then sets no kernel setting unless you choose Apply settings on the screen, which works either way.

- Change it under `8) Configuration`, hardn.yml, [Edit a setting](#editing-from-the-menu). Remove settings leaves it: set it to `false` too, or the next Run All applies the settings again.
- There is no list of settings to skip. To keep one of your own, set it in a file read after hardn's, such as `/etc/sysctl.d/99-sysctl.conf`: hardn shows it and leaves it.

What differs by distribution: [platforms.md](platforms.md#kernel).

### Feature Toggles

```yaml
enableAutoUpdates: false            # Run All: true turns automatic updates on
enableUfwSshPolicy: false           # Run All: true enables the firewall
configureDns: false                 # Run All: true sets the nameservers
disableRootSSH: false               # Run All: true turns root login off
```

#### What Run All reads

The main menu's Run All (6) and `hardn -r` plan these keys' changes as one plan:

- **`username` and `sshKeys`:** creates the user with admin rights and the keys, as `-c`. A user already there is left to User Management unless they are an admin with every key.
- **`disableRootSSH: true`:** turns root login off; on Proxmox VE, root key-only. Skipped while you are logged in as root, and when the firewall is enabled in the same run: run Run All again.
- **`disableRoot`**, its old name, is read as `disableRootSSH`, with a warning that it is deprecated. Rename it.
- **`enableUfwSshPolicy: true`:** enables the firewall. On Rocky Linux, AlmaLinux, RHEL, CentOS Stream and Oracle Linux the firewall is left to the Firewall screen.
- **`configureDns: true`** with **`nameservers`:** sets the nameservers.
- **`enableAutoUpdates: true`:** turns automatic updates on; false changes nothing. hardn's first-run setup asks "Enable automatic security updates? [y/N]" and sets it.
- **`enableUnattendedUpgrades`**, its old name, is read as `enableAutoUpdates`, with a warning. Rename it.
- **`syslogServers`:** sends the host's logs to them, as Syslog Settings' Set servers. With none the row is not listed. Run All never stops forwarding.
- **`ntpServers`:** sets them on the host's time service, as NTP Settings' Set servers. With none the row is not listed. Run All never removes them.
- **`kernel: true`:** sets the kernel settings below hardn's baseline, as Kernel Settings' Apply settings. When false the row is not listed, and in a container never. Run All never removes them.
- **`sshPort`:** compared with the port sshd listens on; never changed. Saving it from the menu doesn't change sshd either.
- **No key, on Alpine:** where sshd lacks the fix for CVE-2026-123158, upgrades openssh, then reloads sshd. Root login off, the firewall and creating a user refused for the CVE then wait for the next run.
- **`enableBackups` and `backupPath`:** the plan's backups.

A change that is refused, doesn't apply or has nothing to change is skipped, and Run All says why. You type the hostname to apply, whatever the plan.

**`ufwAllowedPorts`, `enableAppArmor` and `enableLynis` aren't used.** Where hardn.yml sets one to other than its default, hardn warns, such as "hardn.yml's enableLynis isn't used".

The main menu's Auto Updates (5) reads the host, never `enableAutoUpdates`. It turns automatic updates on, and off where hardn turned them on; on Proxmox VE it changes nothing. Turning them on saves `enableAutoUpdates: true` where hardn.yml says false, and off saves false where it says true, so hardn.yml and the screen agree. Where hardn.yml can't be saved, on still turns them on, and its review says hardn.yml is not changed and why; off refuses.

### Firewall Configuration with UFW Application Profiles

The main menu's Firewall (3) installs, enables and disables ufw, and keeps your application profiles. On Proxmox VE it reads "View pve-firewall": it shows pve-firewall and changes nothing. On Rocky Linux, AlmaLinux, RHEL, CentOS Stream and Oracle Linux it installs, enables and disables firewalld; profiles and interfaces are refused.

- **Where `ss` is missing** (stock Alpine), "Enable firewall" installs iproute2 first, with apk or apt, after showing the change, then enables ufw. If ufw already runs, the first item is "Install iproute2". `-w` refuses without it.
- **Enabling asks you to type the hostname**, since a wrong rule could lock you out. It lets SSH through, denies incoming by default, starts ufw and starts it at boot. On a host with no SSH server, where `ss` shows nothing on port 22, it lets no SSH port through. Run `sudo hardn confirm` from a new session to keep it; if you don't, it is rolled back.
- **`hardn -w` enables it the same way.** On a terminal, a change that could lock you out asks you to type the hostname; 0 or q declines, exit status 3. Off one it is refused, exit status 1: run `hardn -w` from a terminal. A change that can't lock you out, such as starting ufw at boot when it already runs, is applied without asking on a terminal; off one only with `--yes`, otherwise hardn shows the plan and exits 2. With `-n` it shows the plan. On Proxmox VE, Rocky Linux, AlmaLinux, RHEL, CentOS Stream and Oracle Linux `-w` is refused, exit status 1.
- **Application Profiles (2)** adds a profile to hardn.yml, removes one, and applies them: Apply writes `/etc/ufw/applications.d/hardn` and lets their ports through ufw.
- **Trusted Interfaces (3)** adds an interface to hardn.yml, removes one, and applies them: Apply lets everything arriving on each through ufw, every port, as `ufw allow in on <interface>` does.

```yaml
ufwAppProfiles:
  - name: LabHTTPS
    title: Lab Web Server (HTTPS)
    description: Lab Web server secure port
    ports:
      - "30443/tcp" # non-standard 443
```

Each profile has these fields:
- `name`: Unique identifier for the profile (used in UFW commands)
- `title`: User-friendly title
- `description`: Description of the service
- `ports`: List of ports in the format "port/protocol" (e.g., "30443/tcp")
- `interface` (optional): the one network interface its ports are let through on, such as `tailscale0`; left out, every interface

- **In ufw's file the ports are joined by `|`**, as ufw requires: `ports=80/tcp|443/tcp`. Keep one port per entry in hardn.yml.
- **Removing a profile leaves its rule in ufw.** It is taken out of hardn.yml only. Delete it with `ufw delete`.

```yaml
ufwTrustedInterfaces:
  - tailscale0
```

- **A trusted interface lets everything arriving on it through:** every port, from every address on it. Trust only an interface whose peers you trust, such as your tailnet's.
- **Interface names are ufw's:** up to 15 letters, digits and `_-.+,=%@`, no `:` (no alias such as `eth0:1`).
- **Removing an interface leaves its rule in ufw**, as for a profile. Delete it with `ufw delete allow in on <interface>`.
- **Apply needs ufw running**, for profiles and interfaces alike.

Enabling, from main menu 3 or `-w`, denies incoming by default and leaves the outgoing policy as it is. Run All and `-r` enable it the same way, except on Rocky Linux, AlmaLinux, RHEL, CentOS Stream and Oracle Linux. `ufwDefaultIncomingPolicy` and `ufwDefaultOutgoingPolicy` are not used: where hardn.yml sets them, hardn warns, such as "hardn.yml's ufwDefaultIncomingPolicy isn't used". The file is still accepted.

## Configuration Recommendations
<!-- 
create configuration definition table with each measure linking to best practices resource (e.g., 
https://linux-audit.com/ssh/audit-and-harden-your-ssh-configuration/#do-not-use-best-practices)
 -->

1. **System Hardening**: For production servers, enable all security features:
   ```yaml
   enableAutoUpdates: true
   enableUfwSshPolicy: true
   configureDns: true
   ```

2. **Development Environment**: For testing, you may want to use:
   ```yaml
   dryRun: true      # dry run at every start
   enableBackups: true
   ```
   With `dryRun: true`, every start is a dry run, scripts and cron jobs included: `-r`, `-c`, `-d`, `-g` and `-w` only show their plan. Use `--dry-run=false` to apply.

3. **SSH Security**: Always use key-based authentication:
   ```yaml
   sshPort: 2208                    # Non-standard SSH port (security measure; Default: 22)
   permitRootLogin: "no"
   sshKeys:
     - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... george@example.com"
   ```
<!-- provide guide on creating and using SSH keys -->

## Best Practices

1. Keep your configuration file secure with appropriate permissions (0644 or more restrictive)
2. For portable use, maintain configurations in a secure location and explicitly reference them with `--config` or `HARDN_CONFIG`
3. Regularly review and update your configuration
4. Use version control for tracking configuration changes
5. For multi-server deployments, consider using a configuration management tool to distribute configurations

## Troubleshooting

- Run `hardn` with `--dry-run` to preview changes without applying them; if nothing is applied, check `dryRun` in your configuration file
- Check `/var/log/hardn.log` for detailed information
- If you encounter issues, create a backup of your configuration before making changes