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

The flags that change the host (`-c`, `-d`, `-g`, `-w`, `-r`, `-e`) and `setup-sudo-env` say which file they read on stderr: "Using configuration from: <path>". The main menu shows nothing before it starts.

## Creating a Configuration File

You can create a configuration file in several ways:

### 1. Interactive Creation

On first run, `hardn` will offer to create a default configuration file interactively if no existing config is found.

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

If no configuration file exists and you decline, `hardn` will create one at `/etc/hardn/hardn.yml` with default values.
<!-- show/link default values -->

## Environment Variable

You can set the `HARDN_CONFIG` environment variable to specify a configuration file location. This is particularly useful in automation scripts or when you want to maintain multiple configurations.

### Using with sudo or doas

`sudo` and `doas` drop most environment variables, `HARDN_CONFIG` among them. The main menu's Environment (8) keeps it for the user who ran `sudo hardn`, or `doas hardn`, and shows whether it is kept. `sudo hardn setup-sudo-env`, or `sudo hardn -e`, keeps it: on a terminal without asking you to confirm; off one only with `--yes`, otherwise it shows the plan and exits 2.

- **hardn writes only its own file**, `/etc/sudoers.d/hardn-env-<user>` (on doas, its rule `/etc/doas.d/zz-hardn-<user>.conf`), and leaves the others as they are.
- **An earlier hardn's line is read.** `env_keep += "HARDN_CONFIG"` in `/etc/sudoers.d/<user>` shows as kept by that file. hardn leaves the file as it is.
- **Both ways.** Once hardn's file keeps it, Environment offers to stop keeping it.
- **doas needs a rule of hardn's for you**, which this release does not write. Without one, hardn says to add `setenv { HARDN_CONFIG }` to the rule that lets you use doas, and changes nothing.
- **A root login is refused.** Run `sudo hardn` as the user to keep it for.
- **`-e` and `setup-sudo-env` apply without asking you to confirm, on a terminal.** Off one they apply only with `--yes`; without it they show the plan, change nothing and exit 2. A change that could lock you out is refused: keep it from main menu 8, which asks you to confirm. With `-n` they show the plan, and the change is not made.
- **With no hardn.yml found,** `setup-sudo-env` goes on with the defaults and asks nothing. `-e` first asks to create one, as `hardn` does at any start, with `-n` too; answer `n` to go on without one.
- **It takes effect at the next `sudo hardn`.**

#### Workflow example

```bash
# Keep HARDN_CONFIG under sudo (only needs to be done once)
sudo hardn setup-sudo-env

# Set your preferred config location for your current session
export HARDN_CONFIG=$HOME/.config/hardn/hardn.yml

# Run with sudo - the environment variable will be preserved
sudo hardn
```

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
```

#### `dryRun`: dry run

`dryRun: true` starts `hardn` as a dry run: changes are shown, not applied. `-n` does the same for one run.

- **User Management, SSH Login, Firewall, Backup, Auto Updates, Run All and Environment open.** Each of their changes stops at its plan, adding and removing a Firewall profile too.
- **The flag wins.** `-n` turns dry run on, whatever the file says. `--dry-run=false` turns it off for one run, whatever the file says.
- **Operation flags show their plan** in a dry run, as with `-n`: `-r`, `-c`, `-d`, `-g`, `-w`, `-e` and `setup-sudo-env` exit 3 when there are changes. To apply, add `--dry-run=false`.
- **`hardn confirm` and `hardn rollback` still run** under the file's `dryRun: true`: they keep or undo a change already made. `-n` refuses them.
- **Set it here, not in the menu.** The menu's saves write `dryRun` as the file had it, never the mode of the session.
- **The main menu says so.** Beside "Select an option" it shows "Mode Dry run (hardn.yml)", or "(-n)" when the flag set it.

> **Changed in v0.4.0.** `dryRun` used to be ignored: `hardn` ran live whatever the file said. It is now read. A `dryRun: true` an earlier `hardn` wrote now starts a dry run. The main menu's Dry-Run item (6) wrote one to `./hardn.yml`, as did any menu save made under `-n`. If changes are no longer applied, check the file `hardn` names at start for `dryRun: true`. Set it to `false`, or run with `--dry-run=false`. Item 6 is now removed, and Run All no longer turns dry run on or off.

#### `enableBackups` and `backupPath`: the Backup screen

The main menu's Backup (5) shows and changes these two settings.

- **Showing it writes nothing.** With backups on, it used to write a test file to the backup path each time.
- **Verify Directory writes nothing.** It checks that only root can change the path.
- **Some paths are refused** as you type them: a relative path, or one under a directory others can change, such as `/tmp`.
- **Each change is shown first.** Confirm it, and only that setting is saved, in the file `hardn` loaded. Without a loaded file, nothing can be saved.
- **A path change backs up under the new path**, as does every change after it.
- **Backups off still keeps what a rollback needs.** Changes from the DNS and Backup screens and `-g` still copy the files they change to the backup path.
- **Backup Runs lists the backups in the path in use**, newest first. It only reads.

**`logFile` is retired.** hardn always writes and reads `/var/log/hardn.log`. A file that sets `logFile` to another path is still accepted, and hardn warns: "hardn.yml's logFile, <path>, isn't used".

#### `username`, `sudoNoPassword` and `sshKeys`: User Management

```yaml
username: "george"                  # Create's default name; -c's user
sudoNoPassword: true                # not used
sshKeys:                            # -c adds them all; Create's default is the first
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... george@example.com"
```

The main menu's User Management (1) shows the users, their admin rights and SSH keys, and changes them.

- **In User Management, `username` and `sshKeys` are only Create's defaults:** they never choose which user a change is about, and no change saves them.
- **`hardn -c` creates a user with admin rights**, named by `-u`, else by `username`. It asks nothing on a terminal; off one it needs `--yes`, otherwise it shows the plan and exits 2. The user has no password, and every key in `sshKeys`. With `-n` it shows the plan. A name already taken, or a key that isn't one, is refused, exit status 1. `-c -d` creates the user first, then runs `-d`, which goes on if the new user, or another admin, can still log in.
- **`-c` no longer changes sshd_config.**
- **`sudoNoPassword` isn't used.** A new user has no password, so their admin rights never ask one, from User Management, `-c` or Run All. Where hardn.yml sets it to `false`, hardn warns: "hardn.yml's sudoNoPassword: false isn't used".

### Network Configuration

```yaml
nameservers:                        # DNS servers to configure
  - "1.1.1.1"
  - "1.0.0.1"
```

### SSH Configuration

```yaml
sshPort: 22                         # SSH Login's default for Change SSH port
permitRootLogin: "no"               # not used
                                    # true and false still work but are deprecated: write "no" or "yes"
sshAllowedUsers:                    # not used
  - "george"
sshListenAddress: "0.0.0.0"         # not used
sshKeyPath: ".ssh_%u"               # not used
sshConfigFile: "/etc/ssh/sshd_config.d/hardn.conf"  # the old file Move old settings moves
```

The main menu's SSH Login (2) shows root login, password login, the SSH port and who is allowed to log in, as sshd reads them, and changes all but the last.

- **It reads sshd, not hardn.yml.** Opening it changes nothing.
- **Each change asks you to type the hostname**, since it could lock you out. Run `sudo hardn confirm` from a new session to keep it; if you don't, it is rolled back.
- **Some changes are refused before anything is asked**, such as turning root login off where no other admin could log in by key. With hardn's old sshd settings, every change but Move old settings is refused until it moves them.
- **`hardn -d` turns root login off the same way.** On a terminal it asks you to type the hostname; 0 or q declines, exit status 3. Off one it is refused, exit status 1: run `hardn -d` from a terminal. With `-n` it shows the plan. Root login already off is nothing to change, exit status 0. It is refused where no other admin could log in afterwards, by key, or by password while password login is on, and become root. On Proxmox VE it makes root key-only, which the cluster needs.
- **`sshPort` is Change SSH port's default.** **`sshConfigFile` is read by Move old settings alone**, to find hardn's old file.
- **`permitRootLogin`, `sshAllowedUsers`, `sshListenAddress` and `sshKeyPath` aren't used.** Where hardn.yml sets one to other than its default, hardn warns, such as "hardn.yml's sshAllowedUsers isn't used". `permitRootLogin: "prohibit-password"` is warned too, as `"yes"` is; it is no longer refused.

#### `confirmTimeout`: the time to confirm

```yaml
confirmTimeout: "3m"                # seconds, such as 300, or a duration
```

A change that could lock you out rolls back unless `sudo hardn confirm` keeps it within this time. It is 3 minutes when unset; less than 60 seconds is refused. `--confirm-timeout` wins over it.

### Feature Toggles

```yaml
enableUnattendedUpgrades: false     # Run All: true turns automatic updates on
enableUfwSshPolicy: false           # Run All: true enables the firewall
configureDns: false                 # Run All: true sets the nameservers
disableRootSSH: false               # Run All: true turns root login off
```

#### What Run All reads

The main menu's Run All (7) and `hardn -r` plan these keys' changes as one plan:

- **`username` and `sshKeys`:** creates the user with admin rights and the keys, as `-c`. A user already there is left to User Management unless they are an admin with every key.
- **`disableRootSSH: true`:** turns root login off; on Proxmox VE, root key-only. Skipped while you are logged in as root, and when the firewall is enabled in the same run: run Run All again.
- **`disableRoot`**, its old name, is read as `disableRootSSH`, with a warning that it is deprecated. Rename it.
- **`enableUfwSshPolicy: true`:** enables the firewall.
- **`configureDns: true`** with **`nameservers`:** sets the nameservers.
- **`enableUnattendedUpgrades: true`:** turns automatic updates on; false changes nothing. hardn's first-run setup asks "Enable automatic security updates? [y/N]" and sets it.
- **`sshPort`:** compared with the port sshd listens on; never changed.
- **`enableBackups` and `backupPath`:** the plan's backups.

A change that is refused, doesn't apply or has nothing to change is skipped, and Run All says why. You type the hostname to apply, whatever the plan.

**`ufwAllowedPorts`, `enableAppArmor` and `enableLynis` aren't used.** Where hardn.yml sets one to other than its default, hardn warns, such as "hardn.yml's enableLynis isn't used".

The main menu's Auto Updates (6) reads the host, never `enableUnattendedUpgrades`. It turns automatic updates on, and off where hardn turned them on; on Proxmox VE it changes nothing. Turning them off saves `enableUnattendedUpgrades: false` where hardn.yml says true, so Run All doesn't turn them back on.

### Firewall Configuration with UFW Application Profiles

The main menu's Firewall (4) installs, enables and disables ufw, and keeps your application profiles. On Proxmox VE it reads "View pve-firewall": it shows pve-firewall and changes nothing.

- **Enabling asks you to type the hostname**, since a wrong rule could lock you out. It lets SSH through, denies incoming by default, starts ufw and starts it at boot. Run `sudo hardn confirm` from a new session to keep it; if you don't, it is rolled back.
- **`hardn -w` enables it the same way.** On a terminal, a change that could lock you out asks you to type the hostname; 0 or q declines, exit status 3. Off one it is refused, exit status 1: run `hardn -w` from a terminal. A change that can't lock you out, such as starting ufw at boot when it already runs, is applied without asking on a terminal; off one only with `--yes`, otherwise hardn shows the plan and exits 2. With `-n` it shows the plan. On Proxmox VE `-w` is refused, exit status 1.
- **Application Profiles (2)** adds a profile to hardn.yml, removes one, and applies them: Apply writes `/etc/ufw/applications.d/hardn` and lets their ports through ufw.

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

- **In ufw's file the ports are joined by `|`**, as ufw requires: `ports=80/tcp|443/tcp`. Keep one port per entry in hardn.yml.
- **Removing a profile leaves its rule in ufw.** It is taken out of hardn.yml only. Delete it with `ufw delete`.

Enabling, from main menu 4 or `-w`, denies incoming by default and leaves the outgoing policy as it is. Run All and `-r` enable it the same way. `ufwDefaultIncomingPolicy` and `ufwDefaultOutgoingPolicy` are not used: where hardn.yml sets them, hardn warns, such as "hardn.yml's ufwDefaultIncomingPolicy isn't used". The file is still accepted.

## Configuration Recommendations
<!-- 
create configuration definition table with each measure linking to best practices resource (e.g., 
https://linux-audit.com/ssh/audit-and-harden-your-ssh-configuration/#do-not-use-best-practices)
 -->

1. **System Hardening**: For production servers, enable all security features:
   ```yaml
   enableUnattendedUpgrades: true
   enableUfwSshPolicy: true
   configureDns: true
   ```

2. **Development Environment**: For testing, you may want to use:
   ```yaml
   dryRun: true      # dry run at every start
   enableBackups: true
   ```
   With `dryRun: true`, every start is a dry run, scripts and cron jobs included: `-r`, `-c`, `-d`, `-g`, `-w`, `-e` and `setup-sudo-env` only show their plan. Use `--dry-run=false` to apply.

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