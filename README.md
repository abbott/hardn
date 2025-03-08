# Hardn

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev) [![Release](https://img.shields.io/github/v/release/abbott/hardn)](https://github.com/abbott/hardn/releases/latest) ![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/abbott/hardn/ci.yml) [![License: AGPL v3](https://img.shields.io/badge/License-AGPL_3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0) 

<!-- [![Build]((https://github.com/abbott/hardn/workflows/build/badge.svg)](https://github.com/abbott/hardn/actions)  -->

A simple hardening tool for Linux.

<p align="center">
    <img src="https://github.com/user-attachments/assets/2d4dc4d9-0379-46ad-86ef-eeb2e33f264c" width="800" alt="Hardn CLI menu">
</p>

## What is it?

A simple hardening tool that automates basic security configurations for Debian, Ubuntu, Proxmox, and Alpine Linux. The project is stable and in the **early stages of development**.

## ⚠️ Security Disclaimer

**The scope of current capabilities and support of Hardn is limited.** Regular security audits, updates, and monitoring are still required. `hardn` should be part of a broader security strategy, not a "set it and forget it" solution. While the binary distributions are [SLSA3](https://slsa.dev) compliant, they are **<ins>not suitable for enterprise deployments</ins>**.

## 🎯 Target Audience

Anyone managing a **privately owned** Linux server, container, or virtual machine
- Homelab enthusiasts, Students, Hobbyists

If you are one of the following, **refrain from deploying this tool in the public or private sector**
- System Administrator
- DevOps Engineer
- SecOps Architect or Analyst

## ✨ Features


| Feature                       | Description                                                    |
|-------------------------------|----------------------------------------------------------------|
| Tamper Protected Binary         | Releases are traceable to their source commit                                |
| SSH Hardening                  | Secure SSH configuration, key-based authentication                             |
| User Management               | Create non-root users w/sudo access                          |
| Firewall Configuration               | UFW setup w/secure defaults                          |
| DNS Configuration               | Secure DNS setup with specific resolvers                          |
| Automated Updates               | Configures unattended security updates                          |
| System Auditing               | Install Lynis for comprehensive analysis                          |
| Application Control               | Install AppArmor for application restrictions                          |
| Preferred Packages               | Specify Linux packages in batches                         |
| Backup System               | Automatic backup of modified configuration files                          |
| Interactive Menu               | User-friendly interface for system hardening                          |
| Dry-Run Mode               | Preview changes without applying them                          |
| Multi-Distribution Support               | Works with Debian, Ubuntu, Proxmox, and Alpine                          |

<!-- - **Tamper Protected Binary**: Releases are traceable to their source commit
- **SSH Hardening**: Secure SSH configuration, key-based authentication
- **User Management**: Create non-root users with sudo access
- **Firewall Configuration**: UFW setup with sensible defaults
- **DNS Configuration**: Secure DNS setup with specific resolvers
- **Automated Updates**: Configures unattended security updates
- **System Auditing**: Install Lynis for comprehensive analysis
- **Application Control**: Install AppArmor for application restrictions
- **Preferred Packages**: Specify packages for Linux and Python
- **Backup System**: Automatic backup of modified configuration files
- **Interactive Menu**: User-friendly interface for system hardening
- **Dry-Run Mode**: Preview changes without applying them
- **Multi-Distribution Support**: Works with Debian, Ubuntu, Proxmox, and Alpine -->

## 📦 Installation

You can easily install the latest release of `hardn` using the installation script. The script automatically detects your host operating system and architecture, downloads the correct binary, and installs it to `/usr/local/bin`.

### Prerequisites

- **curl:** Used to download the script and binary.
- **sh/bash:** To execute the installation script.
- **sudo:** Required for writing to `/usr/local/bin`.

### Install via Script

Run the following command in your terminal:

```bash
curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```

<!-- *Note:* Replace `main` in the URL with the appropriate branch if necessary. -->

The script will:

- Detect your operating system (e.g., Debian, Proxmox, Alpine Linux) and CPU architecture.
- Query the GitHub releases API to find the latest asset matching your system (e.g., `hardn-linux-amd64` for 64-bit Linux, etc.).
- Download the asset and install it to `/usr/local/bin` with executable permissions.

### Updating

To update `hardn` to the latest release, simply re-run the installation command:

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
   sudo hardn -h # help
   ```

### Install From Source

```bash
# Clone repository
git clone https://github.com/abbott/hardn.git
cd hardn

# Build
make build

# Example distribution (e.g. AMD64)
GOOS=linux GOARCH=amd64 go build -o build/hardn cmd/hardn/main.go

# Install
sudo make install
```

### Troubleshooting

- **Permission Issues:** If you encounter permission errors when writing to `/usr/local/bin`, ensure you’re running the command with `sudo`.
- **Missing curl:** If `curl` is not installed, use your package manager to install it (e.g., `sudo apt-get install curl` on Debian/Ubuntu).


## 🚀 Usage

### Interactive Mode

Run `hardn` without arguments to use the interactive menu for selecting hardening operations:

```bash
sudo hardn
```

### Command Line


| Function                  | Flag     | Description                                                    |
|---------------------------|----------|----------------------------------------------------------------|
| Config file (string)         | `-f, --config-file string`     | Specify configuration file path                                |
| Username (string)                  | `-u, --username string`     | Specify username to create                             |
| User (create)               | `-c, --create-user`     | Create non-root user with sudo access                          |
| Root SSH (disable)              | `-d, --disable-root`     | Disable SSH access for root user                               |
| DNS (configure)            | `-g, --configure-dns`     | Configure DNS settings                                         |
| UFW (configure)             | `-w, --configure-ufw`     | Configure firewall with SSH rules                                 |
| Run all (execute)             | `-r, --run-all`     | Run all hardening operations                                     |
| Dry run (mode)              | `-n, --dry-run`     | Preview changes without applying them                          |
| Linux packages (install)    | `-l, --install-linux`     | Install specified Linux packages                                     |
| All packages (install)     | `-a, --install-all`     | Install all specified packages                                           |
| Sources (configure)           | `-s, --configure-sources`     | Configure package sources                               |
| Logs (print)               | `-p, --print-logs`     | View logs                                          |
| Version (print)             | `-v --version`     | View version                                         |
| Help (print)             | `-h, --help`     | View usage information                                         |


CLI Examples

```bash
# Run all hardening operations
sudo hardn -r

# Create a non-root user w/SSH access
sudo hardn -u george -c

# Install linux packages
sudo hardn -l

# Configure firewall
sudo hardn -w

# Enable dry-run mode and preview all operations
sudo hardn -n -r

# Show version information
sudo hardn -v
```

### Configuration File

On first run, `hardn` will offer to create a default configuration file if no existing config is found. The following YAML configuration file locations are searched in order:

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

Example configuration:

```yaml
# User Management
username: "george"
sudoNoPassword: true
sshKeys:
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... george@example.com"

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

For a complete list of configuration options, review:
- The [example configuration](https://github.com/abbott/hardn/blob/main/hardn.yml.example) — also located at: `/etc/hardn/hardn.yml.example` after initializing the binary (e.g., `sudo hardn`).
- The [Configuration Guide](docs/configuration.md)

## Release Chain Security

[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev)

`hardn` implements [SLSA](https://slsa.dev) Level 3 supply chain security for all releases. This provides the following security guarantees:

- **Tamper Protection**: Each binary is signed and includes a provenance attestation
- **Build Integrity**: Builds are performed in GitHub's trusted environment
- **Source Verification**: Binaries are traceable back to their source commit
- **Reproducibility**: The build process is fully documented in the provenance

### Verifying Releases

To verify a `hardn` release:

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
   curl -LO https://github.com/abbott/hardn/releases/download/v0.3.2/hardn-linux-amd64
   curl -LO https://github.com/abbott/hardn/releases/download/v0.3.2/hardn-linux-amd64.intoto.jsonl
   ```

3. Verify the binary:
   ```bash
   # Using slsa-verifier directly
   slsa-verifier verify-artifact \
     --artifact-path hardn-linux-amd64 \
     --provenance hardn-linux-amd64.intoto.jsonl \
     --source-uri github.com/abbott/hardn \
     --source-tag v0.3.2
     
   # Or using our makefile target
   make verify-release VERSION=0.3.2 OS=linux ARCH=amd64
   ```

4. A successful verification will return:
   ```
   Verification succeeded! Binary artifacts were built from source revision ...
   ```

This ensures the binary was built by GitHub Actions from the official `hardn` repository at the specified tag, and that the artifact has not been tampered with since building


## 🤝 Contributing

Please review the [Contributing Guide](docs/contributing.md) prior to submitting a pull request.

## Issue Reporting

- Use the GitHub issue tracker to report bugs
- Provide detailed reproduction steps
- Include your environment details (OS, Go version, etc.)
- For security vulnerabilities, please email `641138+abbott@users.noreply.github.com` instead of creating a public issue.

## 🗺️ Future Plans

- [ ] Expanded multi-distribution and package management support (Arch, CentOS/RHEL, Fedora)
- [ ] Enhanced system integrity dashboard
- [ ] Containerized deployment
- [ ] Centralized configuration and management for multiple servers
- [ ] Extended auditing capabilities
- [ ] Web interface for remote administration
- [ ] Integration with compliance benchmarks (CIS, STIG)

## 🧪 Origin

After manually hardening Debian based containers and VMs for years, I wrote and maintained a local script to automate the essentials by way of a config and command line arguments. Within a year, a CLI menu was bolted on and the codebase needed to be refactored to ensure maintainability, so I landed on Go, and decided to publish the tool. Enjoy! 🥃

## 📝 Acknowledgments

This project builds upon:
- [Cobra](https://github.com/spf13/cobra) for CLI functionality
- [Viper](https://github.com/spf13/viper) for configuration management
- [yaml.v3](https://github.com/go-yaml/yaml) for YAML parsing
- [color](https://github.com/fatih/color) for terminal color support

Special thanks to the **Linux security community** (experts & enthusiasts) for the wisdom and knowledge over the years.

## 📄 License

This project is licensed under the GNU AGPL v3 License - see the LICENSE file for details.
