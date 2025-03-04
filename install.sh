#!/bin/sh
set -e

# Repository details
REPO="abbott/hardn"

# Determine the OS and architecture
OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
  Darwin)
    ASSET="hardn-darwin-amd64"
    ;;
  Linux)
    case "$ARCH" in
      x86_64)
        ASSET="hardn-linux-amd64"
        ;;
      armv7l)
        ASSET="hardn-linux-arm"
        ;;
      aarch64)
        ASSET="hardn-linux-arm64"
        ;;
      *)
        echo "Unsupported architecture: $ARCH" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

echo "Detected OS: $OS, Architecture: $ARCH"
echo "Using asset: $ASSET"

# Query the GitHub API for the latest release and extract the download URL for the determined asset,
# while filtering out any undesired in-toto file.
DOWNLOAD_URL=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | \
  grep "browser_download_url" | grep "${ASSET}" | grep -v "\.intoto\.jsonl" | cut -d '"' -f 4)

if [ -z "$DOWNLOAD_URL" ]; then
  echo "Error: Could not find asset ${ASSET} in the latest release." >&2
  exit 1
fi

echo "Downloading ${DOWNLOAD_URL}..."
# Download the asset to /usr/local/bin and make it executable.
curl -L "$DOWNLOAD_URL" -o /usr/local/bin/hardn
chmod +x /usr/local/bin/hardn

echo ""
echo "Installed hardn to /usr/local/bin"

# Create config directory if it doesn't exist
echo "Creating configuration directory..."
mkdir -p /etc/hardn

# Run hardn once with --version to trigger config initialization
echo "Initializing configuration..."
/usr/local/bin/hardn --version > /dev/null 2>&1 || true

echo "Reload your shell, and verify 'hardn --help'"
echo ""
echo "Note: Configuration examples will be available at /etc/hardn/hardn.yml.example"
echo "      Run 'hardn' without arguments to enter interactive mode"
echo ""