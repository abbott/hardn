#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Parse arguments
if [ $# -ne 2 ]; then
    echo -e "${RED}Error: Invalid arguments${NC}"
    echo "Usage: $0 <version> <os-arch>"
    echo "Example: $0 v0.3.2 linux-amd64"
    exit 1
fi

VERSION=$1
OS_ARCH=$2

# Remove v prefix if present
VERSION_NUM=${VERSION#v}

# Check if the required tools are installed
check_tools() {
    if ! command -v slsa-verifier &> /dev/null; then
        echo -e "${YELLOW}SLSA verifier not found. Installing...${NC}"
        go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@v2.7.0
    fi

    if ! command -v cosign &> /dev/null; then
        echo -e "${YELLOW}Cosign not found. Installing...${NC}"
        if [[ "$OSTYPE" == "darwin"* ]]; then
            brew install cosign
        else
            curl -sfL https://github.com/sigstore/cosign/releases/latest/download/cosign-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m) -o /tmp/cosign
            chmod +x /tmp/cosign
            sudo mv /tmp/cosign /usr/local/bin/cosign
        fi
    fi
}

# Download required files
download_files() {
    echo -e "${YELLOW}Downloading hardn binary and verification files...${NC}"
    
    # Download binary if it doesn't exist
    if [ ! -f "hardn-$OS_ARCH" ]; then
        curl -sSL -o "hardn-$OS_ARCH" "https://github.com/abbott/hardn/releases/download/v$VERSION_NUM/hardn-$OS_ARCH"
        chmod +x "hardn-$OS_ARCH"
    fi
    
    # Download provenance
    if [ ! -f "hardn-$OS_ARCH.intoto.jsonl" ]; then
        curl -sSL -o "hardn-$OS_ARCH.intoto.jsonl" "https://github.com/abbott/hardn/releases/download/v$VERSION_NUM/hardn-$OS_ARCH.intoto.jsonl"
    fi
    
    # Download signature and certificate
    if [ ! -f "hardn-$OS_ARCH.sig" ]; then
        curl -sSL -o "hardn-$OS_ARCH.sig" "https://github.com/abbott/hardn/releases/download/v$VERSION_NUM/hardn-$OS_ARCH.sig"
    fi
    
    if [ ! -f "hardn-$OS_ARCH.crt" ]; then
        curl -sSL -o "hardn-$OS_ARCH.crt" "https://github.com/abbott/hardn/releases/download/v$VERSION_NUM/hardn-$OS_ARCH.crt"
    fi
}

# Verify SLSA provenance
verify_slsa() {
    echo -e "${YELLOW}Verifying SLSA provenance...${NC}"
    slsa-verifier verify-artifact \
        --artifact-path "hardn-$OS_ARCH" \
        --provenance "hardn-$OS_ARCH.intoto.jsonl" \
        --source-uri github.com/abbott/hardn \
        --source-tag "v$VERSION_NUM"
    
    echo -e "${GREEN}✅ SLSA provenance verification successful!${NC}"
}

# Verify Sigstore signature
verify_signature() {
    echo -e "${YELLOW}Verifying Sigstore signature...${NC}"
    cosign verify-blob \
        --certificate "hardn-$OS_ARCH.crt" \
        --signature "hardn-$OS_ARCH.sig" \
        --certificate-identity-regexp ".*github.com/workflows/.*" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com \
        "hardn-$OS_ARCH"
    
    echo -e "${GREEN}✅ Sigstore signature verification successful!${NC}"
}

# Run the verification
check_tools
download_files
verify_slsa
verify_signature

echo -e "\n${GREEN}✅ Full verification successful!${NC}"
echo -e "${GREEN}The binary hardn-$OS_ARCH has valid SLSA provenance and Sigstore signature.${NC}"
echo -e "${GREEN}It was built from the official abbott/hardn repository at tag v$VERSION_NUM.${NC}"
echo -e "${GREEN}It is safe to use this binary.${NC}"