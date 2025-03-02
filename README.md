# Hardn

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0) [![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev) [![Release](https://img.shields.io/github/v/release/abbott/hardn)](https://github.com/abbott/hardn/releases/latest) 

Secure a Linux distribution in minutes.

<p align="center">
    <img src="https://github.com/user-attachments/assets/a30366b0-5c56-475e-a617-57d5cd8a212e" width="800" alt="Hardn UI">
</p>

## What is it?

A Linux hardening tool that automates security configurations for Debian, Ubuntu, Proxmox, and Alpine Linux. The project is stable, but in the **early stages of development**.

## ⚠️ Security Disclaimer

**This tool implements security best practices but is NOT a complete security solution.** Regular security audits, updates, and monitoring are still required. The software should be part of a broader security strategy, not a "set it and forget it" solution.

## 🎯 Target Audience

- System administrators and Homelab enthusiasts managing Linux servers
- DevOps engineers responsible for system hardening
- SecOps architects, analysts, and pen testers
- Organizations seeking to automate Linux security configurations

## ✨ Features

- **SSH Hardening**: Secure SSH configuration, key-based authentication
- **User Management**: Create non-root users with sudo access
- **Firewall Configuration**: UFW setup with sensible defaults
- **Package Management**: Installation of essential security packages
- **Multi-Distribution Support**: Works with Debian, Ubuntu, Proxmox, and Alpine
- **DNS Configuration**: Secure DNS setup with trusted resolvers
- **Automated Updates**: Configures unattended security updates
- **System Auditing**: Lynis integration for security analysis
- **AppArmor Setup**: Mandatory access control implementation
- **Backup System**: Automatic backup of modified configuration files
- **Dry-Run Mode**: Preview changes without applying them
- **Interactive Menu**: User-friendly interface for system hardening

## 📦 Installation

You can easily install the latest release of Hardn using our installation script. The script automatically detects your host operating system and architecture, downloads the correct binary, and installs it to `/usr/local/bin`.

### Prerequisites

- **curl:** Used to download the script and binary.
- **sh/bash:** To execute the installation script.
- **sudo:** Required for writing to `/usr/local/bin`.

### Install via Script

Run the following command in your terminal:

```bash
curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```

*Note:* Replace `main` in the URL with the appropriate branch if necessary.

The script will:

- Detect your operating system (e.g., Darwin for macOS or Linux for Linux distributions) and CPU architecture.
- Query the GitHub releases API to find the latest asset matching your system (e.g., `hardn-darwin-amd64` for macOS, `hardn-linux-amd64` for 64-bit Linux, etc.).
- Download the asset and install it to `/usr/local/bin/hardn` with executable permissions.

### Updating Hardn

To update Hardn to the latest release, simply re-run the installation command:

```bash
curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```

### Install Binary Manually

1. Visit the [GitHub Releases](https://github.com/abbott/hardn/releases) page.
2. Download the asset corresponding to your system (e.g., `hardn-linux-amd64`) (e.g., `
curl -LO https://github.com/abbott/hardn/releases/latest/download/hardn-linux-amd64`).
3. Move the downloaded binary to `/usr/local/bin` and set executable permissions:

   ```bash
   # Make executable and move to system path
   chmod +x hardn-linux-amd64
   sudo mv hardn-linux-amd64 /usr/local/bin/hardn
   
   # Test installation
   hardn --help
   ```

### Install From Source

```bash
# Clone repository
git clone https://github.com/abbott/hardn.git
cd hardn

# Build
make build

# Example distribution
GOOS=linux GOARCH=amd64 go build -o dist/hardn cmd/hardn/main.go

# Install
sudo make install
```

### Troubleshooting

- **Permission Issues:** If you encounter permission errors when writing to `/usr/local/bin`, ensure you’re running the command with `sudo`.
- **Missing curl:** If `curl` is not installed, use your package manager to install it (e.g., `sudo apt-get install curl` on Debian/Ubuntu or `brew install curl` on macOS).


## 🚀 Usage

### Interactive Mode

Run the tool without arguments to use the interactive menu:

```bash
sudo hardn
```

This will present a menu-driven interface for selecting hardening operations.

### Command Line Mode

```bash
# Run all hardening steps
sudo hardn -r

# Create a non-root user with SSH access
sudo hardn -u george -c

# Install security packages
sudo hardn -l

# Configure firewall
sudo hardn -w

# Enable dry-run mode to preview changes
sudo hardn -n -r

# Show version information
sudo hardn -v
```

### Configuration File

Hardn uses a YAML configuration file. It will search for a configuration file in these locations (in order):

1. Path specified with `--config` or `-f` flag
2. Environment variable `HARDN_CONFIG` (if set)
3. `/etc/hardn/hardn.yml` (system-wide configuration)
4. `~/.config/hardn/hardn.yml` (XDG Base Directory specification)
5. `~/.hardn.yml` (traditional dot-file in home directory)
6. `./hardn.yml` (current working directory)

You can specify a different configuration file with the `-f` flag or environment variable:

```bash
# Using command line flag
sudo hardn -f /path/to/custom-config.yml

# Using environment variable
export HARDN_CONFIG=/path/to/custom-config.yml
sudo hardn
```

When running for the first time with no configuration found, Hardn will offer to create a default configuration file.

Example configuration:

```yaml
# User Management
username: "george"
sudoNoPassword: true
sshKeys:
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@example.com"

# Network & Security
sshPort: 2208                       # Non-standard SSH port (security measure; Default: 22)
dmzSubnet: "192.168.4"
nameservers:
  - "1.1.1.1"
  - "1.0.0.1"

# Feature Toggles
enableAppArmor: true
enableLynis: true
enableUnattendedUpgrades: true
enableUfwSshPolicy: true
configureDns: true
disableRoot: true
```

For a complete list of configuration options, see:
- The [example configuration](https://github.com/abbott/hardn/blob/main/hardn.yml.example) — also located at: `/etc/hardn/hardn.yml.example` after initializing the binary (e.g., `sudo hardn`).
- The [Configuration Guide](docs/configuration.md)

## Release Chain Security

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev)

Hardn implements SLSA Level 3 supply chain security for all releases. This provides the following security guarantees:

- **Tamper Protection**: Each binary is signed and includes a provenance attestation
- **Build Integrity**: Builds are performed in GitHub's trusted environment
- **Source Verification**: Binaries are traceable back to their source commit
- **Reproducibility**: The build process is fully documented in the provenance

### Verifying a Release

To verify a Hardn release:

1. Install the SLSA verifier:
   ```bash
   # Using Go
   go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@v2.7.0
   
   # Or using our makefile target
   make install-verifier
   ```

2. Download the binary and its provenance:
   ```bash
   # Example for Linux AMD64
   curl -LO https://github.com/abbott/hardn/releases/download/v0.2.9/hardn-linux-amd64
   curl -LO https://github.com/abbott/hardn/releases/download/v0.2.9/hardn-linux-amd64.intoto.jsonl
   ```

3. Verify the binary:
   ```bash
   # Using slsa-verifier directly
   slsa-verifier verify-artifact \
     --artifact-path hardn-linux-amd64 \
     --provenance hardn-linux-amd64.intoto.jsonl \
     --source-uri github.com/abbott/hardn \
     --source-tag v0.2.9
     
   # Or using our makefile target
   make verify-release VERSION=0.2.9 OS=linux ARCH=amd64
   ```

4. A successful verification will return:
   ```
   Verification succeeded! Binary artifacts were built from source revision ...
   ```

This verification ensures that the binary was built by GitHub Actions from the official Hardn repository at the specified tag, and that the artifact has not been tampered with since building


## 🤝 Contributing

Please review the [Contributing Guide](docs/contributing.md) prior to submitting a pull request.

## Issue Reporting

- Use the GitHub issue tracker to report bugs
- Provide detailed reproduction steps
- Include your environment details (OS, Go version, etc.)
- For security vulnerabilities, please email `641138+abbott@users.noreply.github.com` instead of creating a public issue.

## 🗺️ Roadmap

- [ ] Expanded multi-distribution support (CentOS/RHEL, Fedora)
- [ ] Integration with compliance benchmarks (CIS, STIG)
- [ ] Web interface for remote administration
- [ ] Containerized deployment option
- [ ] Centralized management for multiple servers
- [ ] Extended auditing capabilities
- [ ] Role-based hardening profiles

## 🧪 Origin

After manually hardening Debian based containers and VMs for years, I wrote and maintained a local script to automate the essentials by way of a config and command line arguments. Within a year, a CLI menu was bolted on and the codebase needed to be refactored to ensure maintainability, so I landed on Go, and decided to publish the tool. Enjoy 🥃

## 📝 Acknowledgments

This project builds upon:
- [Cobra](https://github.com/spf13/cobra) for CLI functionality
- [Viper](https://github.com/spf13/viper) for configuration management
- [yaml.v3](https://github.com/go-yaml/yaml) for YAML parsing
- [color](https://github.com/fatih/color) for terminal color support

Special thanks to the **Linux security community** (experts & enthusiasts) for the wisdom and knowledge over the years.

## 📄 License

This project is licensed under the GNU AGPL v3 License - see the LICENSE file for details.
