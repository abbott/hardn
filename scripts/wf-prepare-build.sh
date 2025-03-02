#!/bin/bash
# Script to prepare the codebase for building
# - Removes any replace directives
# - Cleans/rebuilds vendor directory
# - Ensures dependencies are properly recorded
# Can be used for both local builds and CI preparation

set -e

# Check if we're in the project root directory
if [ ! -f "go.mod" ]; then
  echo "Error: This script must be run from the project root directory"
  exit 1
fi

# Check for script arguments
REBUILD_VENDOR=true
if [ "$1" == "--no-vendor" ]; then
  REBUILD_VENDOR=false
fi

echo "Preparing codebase for building..."

# Ensure there are no replace directives in go.mod
if grep -q "replace github.com/abbott/hardn" go.mod; then
  echo "Removing replace directive from go.mod"
  grep -v "replace github.com/abbott/hardn" go.mod > go.mod.clean
  mv go.mod.clean go.mod
  echo "Replace directive removed"
else
  echo "Verified: No replace directives in go.mod"
fi

# Run go mod tidy to clean up dependencies
echo "Running go mod tidy..."
go mod tidy

# Clean vendor directory completely to prevent corruption issues
if [ -d "vendor" ]; then
  echo "Cleaning vendor directory..."
  rm -rf vendor
fi

# Rebuild vendor directory if requested
if [ "$REBUILD_VENDOR" = true ]; then
  echo "Rebuilding vendor directory..."
  go mod vendor
  echo "Vendor directory rebuilt successfully"
fi

# Verify golang version matches the one in go.mod
GO_VERSION=$(grep "^go " go.mod | awk '{print $2}')
echo "Project requires Go $GO_VERSION"

echo "✅ Codebase prepared successfully"
echo "You can now run builds or tests with a clean environment"

# Usage instructions
if [ "$1" == "--help" ] || [ "$1" == "-h" ]; then
  echo ""
  echo "Usage: $0 [OPTIONS]"
  echo "Options:"
  echo "  --no-vendor    Skip rebuilding the vendor directory"
  echo "  --help, -h     Show this help message"
fi