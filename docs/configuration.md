# Hardn Configuration

This document explains how to configure Hardn, the Linux hardening utility.

## Configuration File Locations

Hardn searches for a configuration file in the following locations (in order):

1. Path specified with `--config` or `-f` flag
2. Environment variable `HARDN_CONFIG` (if set)
3. `/etc/hardn/hardn.yml` (system-wide configuration)
4. `~/.config/hardn/hardn.yml` (XDG Base Directory specification)
5. `~/.hardn.yml` (traditional dot-file in home directory)

When running for the first time with no configuration found, Hardn will offer to create a default configuration file.

The first configuration file found will be used. If no configuration file is found, Hardn will offer to create a default one.

## Creating a Configuration File

You can create a configuration file in several ways:

### 1. Interactive Creation

When running Hardn for the first time with no configuration file, it will offer to create one interactively.

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

If no configuration file exists and you decline to create one, Hardn will use reasonable default values.

## Environment Variable

You can set the `HARDN_CONFIG` environment variable to specify a configuration file location. This is particularly useful in automation scripts or when you want to maintain multiple configurations.

#### **Temporary (Current Session Only)**
If you just need the variable for the current terminal session, run:

```bash
export HARDN_CONFIG=$HOME/hardn.yml
```

This setting will last only for the current shell session and will reset when you close the terminal or log out.

#### **Persistent Across Sessions**
To make this configuration persist across reboots and new shell sessions, add it to your shell's startup file by issuing the following command for your respective shell:

##### **For Bash**

```bash
echo 'export HARDN_CONFIG=$HOME/hardn.yml' >> ~/.bashrc
# Reload the file to apply the changes
source ~/.bashrc 
```
##### **For Zsh**

```bash
echo 'export HARDN_CONFIG=$HOME/hardn.yml' >> ~/.zshrc
# Reload the file to apply the changes
source ~/.zshrc
```

##### **For Fish**

```fish
set -Ux HARDN_CONFIG $HOME/hardn.yml
```

If the environment variable is unavailable, restart your terminal.

## Command Line Flags

You can specify a different configuration file with the `-f` flag or environment variable:

```bash
# Using command line flag
sudo hardn -f /path/to/custom-config.yml
```

## Configuration Options

The configuration file uses YAML format. Here are the main configuration sections:

### Basic Configuration

```yaml
username: "sysadmin"                # Default username to create
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
sshPort: 2208                       # Non-standard SSH port (security measure; Default: 22)
permitRootLogin: false              # Allow or deny root SSH access
sshAllowedUsers:                    # List of users allowed to access via SSH
  - "sysadmin"
sshListenAddress: "0.0.0.0"         # IP address to listen on
sshKeyPath: ".ssh_%u"               # Path to SSH keys (%u = username)
sshConfigFile: "/etc/ssh/sshd_config.d/manage.conf"  # SSH config file location
```

### Feature Toggles

```yaml
enableAppArmor: false               # Set up and enable AppArmor
enableLynis: false                  # Install and run Lynis security audit
enableUnattendedUpgrades: false     # Configure automatic security updates
enableUfwSshPolicy: false           # Configure UFW with SSH rules
configureDns: false                 # Configure DNS settings
disableRoot: false                  # Disable root SSH access
```

For a complete list of configuration options, see the example configuration file at `/etc/hardn/hardn.yml.example`.

## Configuration Recommendations

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
     - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@example.com"
   ```

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