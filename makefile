.PHONY: build clean test all linux darwin update-deps

BINARY_NAME=hardn
BUILD_DIR=build
BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE) -X main.GitCommit=$(GIT_COMMIT)"

# Version
VERSION_MAJOR=0
VERSION_MINOR=3
VERSION_PATCH=1
VERSION=0.3.1

.PHONY: release release-artifacts

# Git tag with version
tag:
	git tag -a v$(VERSION) -m "Release v$(VERSION)"
	git push origin v$(VERSION)

# Generate checksums
checksums:
	cd $(BUILD_DIR) && \
	for file in *; do \
		sha256sum $$file > $$file.sha256; \
	done

# Create release archives
archives:
	cd $(BUILD_DIR) && \
	tar -czvf $(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz $(BINARY_NAME)-linux-amd64 && \
	tar -czvf $(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz $(BINARY_NAME)-darwin-amd64 && \
	tar -czvf $(BINARY_NAME)-$(VERSION)-linux-arm.tar.gz $(BINARY_NAME)-linux-arm && \
	tar -czvf $(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz $(BINARY_NAME)-linux-arm64

release-artifacts: cross-compile archives checksums

release: test release-artifacts tag
	@echo "Release v$(VERSION) created successfully"


.PHONY: bump-major bump-minor bump-patch

# Determine sed in-place editing syntax based on OS
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
    SED_INPLACE = sed -i ''
else
    SED_INPLACE = sed -i
endif

bump-major:
	$(eval VERSION_MAJOR=$(shell echo $$(($(VERSION_MAJOR)+1))))
	$(eval VERSION_MINOR=0)
	$(eval VERSION_PATCH=0)
	@echo "Version bumped to $(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)"
	$(SED_INPLACE) 's/^VERSION_MAJOR=.*/VERSION_MAJOR=$(VERSION_MAJOR)/' makefile
	$(SED_INPLACE) 's/^VERSION_MINOR=.*/VERSION_MINOR=$(VERSION_MINOR)/' makefile
	$(SED_INPLACE) 's/^VERSION_PATCH=.*/VERSION_PATCH=$(VERSION_PATCH)/' makefile
	$(SED_INPLACE) 's/^VERSION=.*/VERSION=$(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)/' makefile

bump-minor:
	$(eval VERSION_MINOR=$(shell echo $$(($(VERSION_MINOR)+1))))
	$(eval VERSION_PATCH=0)
	@echo "Version bumped to $(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)"
	sed -i '' 's/^VERSION_MINOR=.*/VERSION_MINOR=$(VERSION_MINOR)/' makefile
	sed -i '' 's/^VERSION_PATCH=.*/VERSION_PATCH=$(VERSION_PATCH)/' makefile
	sed -i '' 's/^VERSION=.*/VERSION=$(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)/' makefile

bump-patch:
	$(eval VERSION_PATCH=$(shell echo $$(($(VERSION_PATCH)+1))))
	@echo "Version bumped to $(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)"
	sed -i '' 's/^VERSION_PATCH=.*/VERSION_PATCH=$(VERSION_PATCH)/' makefile
	sed -i '' 's/^VERSION=.*/VERSION=$(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)/' makefile

all: clean build

# Default build flags (can be overridden from environment)
GO_BUILD_FLAGS?=

build:
	mkdir -p $(BUILD_DIR)
	go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/hardn

linux:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/hardn

darwin:
	mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/hardn

arm:
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./cmd/hardn
	GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/hardn

cross-compile: linux darwin arm

clean:
	rm -rf $(BUILD_DIR)

# Updated test target to handle vendoring properly
test:
	@echo "Running tests with standard Go modules..."
	go test -v ./...

deps:
	go get -v -u ./...
	go mod tidy

update-deps:
	@echo "Updating Go dependencies..."
	go get -u ./...
	go mod tidy
	@echo "Checking for outdated direct dependencies..."
	go list -u -m -json all | go-mod-outdated -update -direct

install: build
	cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/

# For preparing a release by removing the replace directive
prepare-release:
	@echo "Preparing for release..."
	@if grep -q "replace github.com/abbott/hardn" go.mod; then \
		echo "Removing replace directive from go.mod..."; \
		go mod edit -dropreplace=github.com/abbott/hardn; \
		go mod tidy; \
	fi
	@echo "Verifying build..."
	@make build
	@echo "Build successful. Ready for release!"


# For restoring development configuration
restore-dev:
	@echo "Restoring development configuration..."
	@if ! grep -q "replace github.com/abbott/hardn" go.mod; then \
		echo "Adding replace directive to go.mod..."; \
		go mod edit -replace=github.com/abbott/hardn=./; \
		go mod tidy; \
		echo "Development environment restored."; \
	else \
		echo "Replace directive already exists in go.mod."; \
	fi

.PHONY: deb rpm

# Create a .deb package (requires fpm)
deb: linux
	mkdir -p $(BUILD_DIR)/deb/etc/hardn
	cp hardn.yml.example $(BUILD_DIR)/deb/etc/hardn/
	# Create a minimal default config file
	sed -n '/^[^#]/p' hardn.yml.example > $(BUILD_DIR)/deb/etc/hardn/hardn.yml
	fpm -s dir -t deb -n $(BINARY_NAME) -v $(VERSION) \
		--description "Secure a Linux distribution in minutes" \
		--url "https://github.com/abbott/hardn" \
		--license "AGPL-3.0" \
		--after-install scripts/bin-postinstall.sh \
		$(BUILD_DIR)/$(BINARY_NAME)-linux-amd64=/usr/local/bin/$(BINARY_NAME) \
		$(BUILD_DIR)/deb/etc/hardn/hardn.yml=/etc/hardn/hardn.yml \
		$(BUILD_DIR)/deb/etc/hardn/hardn.yml.example=/etc/hardn/hardn.yml.example \
		README.md=/usr/share/doc/hardn/README.md

# Create a .rpm package (requires fpm)
rpm: linux
	@echo "Creating RPM package..."
	@mkdir -p $(BUILD_DIR)/rpm/etc/hardn
	@cp hardn.yml.example $(BUILD_DIR)/rpm/etc/hardn/
	@# Create a minimal default config file
	@sed -n '/^[^#]/p' hardn.yml.example > $(BUILD_DIR)/rpm/etc/hardn/hardn.yml
	@# Check if rpm is installed
	@if ! command -v rpm > /dev/null; then \
		echo "Warning: rpm command not found, installing..."; \
		apt-get update && apt-get install -y rpm || { echo "Failed to install rpm. RPM packaging may fail."; }; \
	fi
	@# Try to create RPM with more verbose output
	fpm -s dir -t rpm -n $(BINARY_NAME) -v $(VERSION) \
		--verbose \
		--description "Secure a Linux distribution in minutes" \
		--url "https://github.com/abbott/hardn" \
		--license "AGPL-3.0" \
		--after-install scripts/bin-postinstall.sh \
		$(BUILD_DIR)/$(BINARY_NAME)-linux-amd64=/usr/local/bin/$(BINARY_NAME) \
		$(BUILD_DIR)/rpm/etc/hardn/hardn.yml=/etc/hardn/hardn.yml \
		$(BUILD_DIR)/rpm/etc/hardn/hardn.yml.example=/etc/hardn/hardn.yml.example \
		README.md=/usr/share/doc/hardn/README.md || { \
		echo "RPM build failed. This is often due to missing dependencies."; \
		echo "To troubleshoot, try: sudo apt-get install rpm libarchive-tools"; \
		exit 1; \
	}

# SLSA verification targets
.PHONY: verify-release verify-local install-verifier

# Install SLSA verifier
install-verifier:
	go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@v2.7.0

# Verify a release using SLSA verifier
# Usage: make verify-release VERSION=0.2.9 OS=linux ARCH=amd64
verify-release:
	@if [ -z "$(VERSION)" ] || [ -z "$(OS)" ] || [ -z "$(ARCH)" ]; then \
		echo "Error: Missing parameters"; \
		echo "Usage: make verify-release VERSION=0.2.9 OS=linux ARCH=amd64"; \
		exit 1; \
	fi
	@echo "Verifying release v$(VERSION) for $(OS)/$(ARCH)..."
	@slsa-verifier verify-artifact \
		hardn-$(OS)-$(ARCH) \
		--provenance hardn-$(OS)-$(ARCH).intoto.jsonl \
		--source-uri github.com/abbott/hardn \
		--source-tag v$(VERSION)
	@echo "✅ Verification successful!"

# Verify a local build using SLSA verifier
# This requires the binary and provenance file to be in the current directory
verify-local:
	@echo "Verifying local build..."
	@ls -la *.intoto.jsonl | head -1 | awk '{print $$9}' | xargs -I{} bash -c '\
		binary=$$(echo {} | sed "s/.intoto.jsonl//"); \
		tag=$$(git describe --tags --exact-match 2>/dev/null || echo "dev"); \
		echo "Binary: $$binary, Tag: $$tag"; \
		slsa-verifier verify-artifact \
			--artifact-path $$binary \
			--provenance {} \
			--source-uri github.com/abbott/hardn \
			$$([ "$$tag" != "dev" ] && echo "--source-tag $$tag" || echo "--source-branch $$(git branch --show-current)");\
		if [ $$? -eq 0 ]; then echo "✅ Verification successful!"; else echo "❌ Verification failed!"; fi;\
	'

# Add a vendor target
.PHONY: vendor

vendor:
	@echo "Syncing vendor directory with go.mod..."
	go mod vendor
	@echo "Vendor directory synced successfully."

# Fix vendor target
.PHONY: fix-vendor
fix-vendor:
	@echo "Fixing vendor directory..."
	@if [ -d "vendor" ]; then \
		echo "Removing existing vendor directory..."; \
		rm -rf vendor; \
	fi
	@echo "Running go mod vendor..."
	go mod vendor
	@echo "Running go mod tidy..."
	go mod tidy
	@echo "✅ Vendor directory fixed and synced with go.mod"

dev: setup-dev
	mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/hardn

run: dev
	$(BUILD_DIR)/$(BINARY_NAME)

# Setup local development environment
setup-dev:
	@bash scripts/wf-dev-mode.sh

# Prepare codebase for CI
prepare-ci:
	@bash scripts/wf-prepare-build.sh

# Clean development artifacts
clean-dev:
	@echo "Cleaning development artifacts..."
	@if grep -q "replace github.com/abbott/hardn" go.mod; then \
		echo "Removing replace directive from go.mod..."; \
		sed -i '/replace github.com\/abbott\/hardn/d' go.mod; \
		go mod tidy; \
	fi
	@echo "✅ Development artifacts cleaned"

# Add these new simple development targets

# Run build in development mode (without modifying go.mod)
.PHONY: dev-build
dev-build:
	@bash scripts/wf-dev-mode.sh go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/hardn

# Run tests in development mode (without modifying go.mod)
.PHONY: dev-test
dev-test:
	@bash scripts/wf-dev-mode.sh go test ./...

# Run any command in development mode with local module
.PHONY: dev-run
dev-run:
	@if [ -z "$(CMD)" ]; then \
		echo "Usage: make dev-run CMD='go run ./cmd/hardn'"; \
		exit 1; \
	fi
	@bash scripts/wf-dev-mode.sh $(CMD)
