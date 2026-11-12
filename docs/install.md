# Installing hardn

## Install script

```sh
curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
```
_When a host uses `doas` (Alpine), pipe to `doas sh` instead of `sudo sh`._

**The script will**:

- Find the latest release through the GitHub API (or take `HARDN_VERSION`, below), and its asset for the system: `hardn-linux-amd64`, `hardn-linux-arm64` or `hardn-darwin-amd64`. There is no armv7 build, so on armv7 it stops.
- Check the asset's SHA-256 against the release's `SHA256SUMS`, and its attestation too when gh 2.68.0 or later is installed and signed in. If a check fails, it installs nothing. Without gh, it installs on the SHA-256 alone and prints the commands that check the attestation. The SHA-256 shows the download matches the release; only the attestation shows the release was built by hardn's workflow from its tag.
- Install `/usr/local/bin/hardn` and create `/etc/hardn`.

To pin a version, set `HARDN_VERSION` (v0.4.0 and later; earlier releases have no `SHA256SUMS`):

```sh
curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo env HARDN_VERSION=0.4.0 sh
```

The first time hardn runs **live**, it writes `/etc/hardn/hardn.yml.example`. _On a terminal, a run that finds no hardn.yml offers to create one, except `setup-sudo-env` and dry runs._

## Install Manually

1. Visit the [Releases](https://github.com/abbott/hardn/releases) page.
2. Download the asset corresponding to your system (e.g., `hardn-linux-amd64`):

   ```sh
   curl -LO https://github.com/abbott/hardn/releases/latest/download/hardn-linux-amd64
   ```

3. [Verify it](verify.md#verify-a-release) (*optional, but recommended*)
4. Install, set permissions, and test:

   ```sh
   sudo install -m 0755 hardn-linux-amd64 /usr/local/bin/hardn
   hardn -h     # help
   ```

## Build from Source

You need Go 1.26 or later ([install Go](https://go.dev/doc/install)), git and make. If your Go is older than 1.27.4, `go` fetches 1.27.4 by default, because releases are built with it (the `toolchain` line in go.mod).

```sh
git clone https://github.com/abbott/hardn.git
cd hardn
make build       # build/hardn, for this machine
make linux       # build/hardn-linux-amd64 (also: `make arm` for linux-arm64, `make darwin`)
sudo cp build/hardn /usr/local/bin/hardn
```

`make build` runs `go build ./cmd/hardn` with the release's flags. A bare `make` runs the project's checks, not a build ([contributing.md](contributing.md)).

## Troubleshooting

- **Permission issues:** Not run as root, the script stops before downloading anything and prints the command to use, piped to `sudo sh` (or `doas sh`).
- **Missing curl:** If `curl` is not installed, use your package manager to install it (e.g., `sudo apt-get install curl` on Debian/Ubuntu; `doas apk add curl` or `sudo apk add curl` on Alpine).
- **hardn not found:** If `/bin/sh: hardn: not found` appears, `/usr/local/bin` is not in this shell's `PATH`. Run `/usr/local/bin/hardn`, or `export PATH=/usr/local/sbin:/usr/local/bin:$PATH` first.

## Updating

To update, run the  [Install Script](../README.md#install) command again.

