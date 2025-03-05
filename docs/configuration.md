# Hardn Configuration

This document explains how to configure `hardn`, the simple hardening tool for Linux.

## Configuration File Locations

`hardn` searches for a YAML configuration file in the following order:

1. Path specified with `--config` or `-f` flag
2. Environment variable `HARDN_CONFIG` (if set)
3. `/etc/hardn/hardn.yml` (system-wide configuration)
4. `~/.config/hardn/hardn.yml` (XDG Base Directory specification)
5. `~/.hardn.yml` (traditional dot-file in home directory)

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

### Using with sudo

When using `sudo`, environment variables are typically not preserved. To preserve the `HARDN_CONFIG` environment variable when using `sudo`, use the `setup-sudo-env` command which does the following:

1. Create a file in `/etc/sudoers.d/` for your user
2. Add a configuration line to preserve the `HARDN_CONFIG` environment variable
3. Set the correct permissions on the file

#### Workflow example

```bash
# Set up sudo to preserve HARDN_CONFIG (only needs to be done once)
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

Here are the main configuration sections in YAML:

### Basic Configuration

```yaml
username: "george"                # Default username to create
logFile: "/var/log/hardn.log"       # Log file path
dryRun: false                       # Preview changes without applying them
enableBackups: true                 # Backup files before modifying them
backupPath: "/var/backups/hardn"    # Path to store backups
```

### Network Configuration

```yaml
dmzSubnet: "192.168.4"              # DMZ subnet for conditional package installation
nameservers:                        # DNS servers to configure
  - "1.1.1.1"
  - "1.0.0.1"
```

### SSH Configuration

```yaml
sshPort: 22                         # SSH port (this is the authoritative SSH port used throughout the configuration)
                                    # Consider using a non-standard port (e.g., 2208) as a security measure
permitRootLogin: false              # Allow or deny root SSH access
sshAllowedUsers:                    # List of users allowed to access via SSH
  - "george"
sshListenAddress: "0.0.0.0"         # IP address to listen on
sshKeyPath: ".ssh_%u"               # Path to SSH keys (%u = username)
sshConfigFile: "/etc/ssh/sshd_config.d/manage.conf"  # SSH config file location
```

**Important**: The `sshPort` setting is the single source of truth for SSH port configuration throughout the application.
Hardn will automatically set an SSH policy with your configured port.

### Feature Toggles

```yaml
enableAppArmor: false               # Set up and enable AppArmor
enableLynis: false                  # Install and run Lynis security audit
enableUnattendedUpgrades: false     # Configure automatic security updates
enableUfwSshPolicy: false           # Configure UFW with SSH rules
configureDns: false                 # Configure DNS settings
disableRoot: false                  # Disable root SSH access
```

### Firewall Configuration with UFW Application Profiles

Hardn uses UFW application profiles to configure the firewall. These profiles are written to `/etc/ufw/applications.d/hardn` and provide a flexible way to define firewall rules.

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

The default incoming policy is always set to "deny" and the default outgoing policy to "allow" for security.

## Configuration Recommendations
<!-- 
create configuration definition table with each measure linking to best practices resource (e.g., 
https://linux-audit.com/ssh/audit-and-harden-your-ssh-configuration/#do-not-use-best-practices)
 -->

1. **System Hardening**: For production servers, enable all security features:
   ```yaml
   enableAppArmor: true
   enableLynis: true
   enableUnattendedUpgrades: true
   enableUfwSshPolicy: true
   configureDns: true
   disableRoot: true
   ```

2. **Development Environment**: For testing, you may want to use:
   ```yaml
   dryRun: true
   enableBackups: true
   ```

3. **SSH Security**: Always use key-based authentication:
   ```yaml
   sshPort: 2208                    # Non-standard SSH port (security measure; Default: 22)
   permitRootLogin: false
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

- Run `hardn` with `--dry-run` to preview changes without applying them
- Check the log file (default: `/var/log/hardn.log`) for detailed information
- If you encounter issues, create a backup of your configuration before making changes