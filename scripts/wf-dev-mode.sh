#!/bin/bash
# Script to run commands in development mode without modifying repository files

set -e

# Check if we're in the project root directory
if [ ! -f "go.mod" ]; then
  echo "Error: This script must be run from the project root directory"
  exit 1
fi

echo "Setting up local development environment..."

# Create temporary go.mod for development
TMP_GOMOD=$(mktemp)
TMP_DIR=$(dirname "$TMP_GOMOD")
DEV_GOMOD="$TMP_DIR/dev.go.mod"

# Create dev go.mod with replace directive
cat go.mod > "$DEV_GOMOD"
if ! grep -q "replace github.com/abbott/hardn => ./" "$DEV_GOMOD"; then
  echo "replace github.com/abbott/hardn => ./" >> "$DEV_GOMOD"
fi

# Run the provided command with the dev go.mod
echo "Running in development mode: $*"
GOFLAGS="-modfile=$DEV_GOMOD" "$@"

# Clean up
rm -f "$DEV_GOMOD"
echo "✅ Development mode command complete"


# echo "✅ Development environment set up successfully"
# echo "You can now run 'make build' or 'make test' with local module resolution"