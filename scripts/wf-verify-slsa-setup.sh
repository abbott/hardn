#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Set the project root directory (one level up from scripts/)
PROJECT_ROOT="$(dirname "$(dirname "$(readlink -f "$0")")")"
cd "$PROJECT_ROOT"

echo -e "${YELLOW}Verifying SLSA Level 3 Setup${NC}"
echo "================================="

# Check workflow file
echo -n "Checking GitHub workflow... "
if [ -f ".github/workflows/release.yml" ]; then
  if grep -q "slsa-github-generator/.github/workflows/builder_go_slsa3.yml" .github/workflows/release.yml; then
    echo -e "${GREEN}✓ Found${NC}"
  else
    echo -e "${RED}✗ Missing SLSA builder reference${NC}"
    exit 1
  fi
else
  echo -e "${RED}✗ Missing${NC}"
  exit 1
fi

# Check main config file
echo -n "Checking main SLSA config... "
if [ -f ".slsa-goreleaser.yml" ]; then
  echo -e "${GREEN}✓ Found${NC}"
else
  echo -e "${YELLOW}⚠ Not found, but not critical if using matrix configs${NC}"
fi

# Check matrix config files
echo -n "Checking matrix config files... "
COUNT=$(find .slsa-goreleaser -name "*.yml" | wc -l)
if [ "$COUNT" -gt 0 ]; then
  echo -e "${GREEN}✓ Found $COUNT files${NC}"
else
  echo -e "${RED}✗ Missing${NC}"
  exit 1
fi

# Check makefile targets
echo -n "Checking makefile targets... "
if grep -q "verify-release" makefile; then
  echo -e "${GREEN}✓ Found${NC}"
else
  echo -e "${RED}✗ Missing${NC}"
  exit 1
fi

echo -e "\n${GREEN}All checks passed! Your project is ready for SLSA Level 3.${NC}"
echo -e "\nTo create a test release, run:"
echo -e "  git tag -a v0.2.9-test -m \"Test SLSA implementation\""
echo -e "  git push origin v0.2.9-test"
echo -e "\nTo create a real release, run:"
echo -e "  make bump-patch"
echo -e "  git add makefile"
echo -e "  git commit -m \"Bump version to $(grep VERSION= makefile | head -1 | cut -d= -f2)\""
echo -e "  make release"