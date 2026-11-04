.PHONY: build clean test all linux darwin update-deps

BINARY_NAME=hardn
BUILD_DIR=build
BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE) -X main.GitCommit=$(GIT_COMMIT)"

# Version
VERSION_MAJOR=0
VERSION_MINOR=3
VERSION_PATCH=2
VERSION=0.3.2

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
	$(SED_INPLACE) 's/^VERSION_MAJOR=.*/VERSION_MAJOR=$(VERSION_MAJOR)/' Makefile
	$(SED_INPLACE) 's/^VERSION_MINOR=.*/VERSION_MINOR=$(VERSION_MINOR)/' Makefile
	$(SED_INPLACE) 's/^VERSION_PATCH=.*/VERSION_PATCH=$(VERSION_PATCH)/' Makefile
	$(SED_INPLACE) 's/^VERSION=.*/VERSION=$(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)/' Makefile

bump-minor:
	$(eval VERSION_MINOR=$(shell echo $$(($(VERSION_MINOR)+1))))
	$(eval VERSION_PATCH=0)
	@echo "Version bumped to $(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)"
	sed -i '' 's/^VERSION_MINOR=.*/VERSION_MINOR=$(VERSION_MINOR)/' Makefile
	sed -i '' 's/^VERSION_PATCH=.*/VERSION_PATCH=$(VERSION_PATCH)/' Makefile
	sed -i '' 's/^VERSION=.*/VERSION=$(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)/' Makefile

bump-patch:
	$(eval VERSION_PATCH=$(shell echo $$(($(VERSION_PATCH)+1))))
	@echo "Version bumped to $(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)"
	sed -i '' 's/^VERSION_PATCH=.*/VERSION_PATCH=$(VERSION_PATCH)/' Makefile
	sed -i '' 's/^VERSION=.*/VERSION=$(VERSION_MAJOR).$(VERSION_MINOR).$(VERSION_PATCH)/' Makefile

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
# Usage: make verify-release VERSION=0.3.2 OS=linux ARCH=amd64
verify-release:
	@if [ -z "$(VERSION)" ] || [ -z "$(OS)" ] || [ -z "$(ARCH)" ]; then \
		echo "Error: Missing parameters"; \
		echo "Usage: make verify-release VERSION=0.3.2 OS=linux ARCH=amd64"; \
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

# Install Cosign
.PHONY: install-cosign
install-cosign:
	@echo "Installing Cosign..."
	@if [ "$$(uname)" = "Darwin" ]; then \
		brew install cosign; \
	else \
		curl -sfL https://github.com/sigstore/cosign/releases/latest/download/cosign-$$(uname -s | tr '[:upper:]' '[:lower:]')-$$(uname -m) -o /tmp/cosign && \
		chmod +x /tmp/cosign && \
		sudo mv /tmp/cosign /usr/local/bin/cosign; \
	fi
	@echo "✅ Cosign installed successfully"

# Verify artifact signature
.PHONY: verify-signature
verify-signature:
	@if [ -z "$(VERSION)" ] || [ -z "$(OS)" ] || [ -z "$(ARCH)" ]; then \
		echo "Error: Missing parameters"; \
		echo "Usage: make verify-signature VERSION=0.3.2 OS=linux ARCH=amd64"; \
		exit 1; \
	fi
	@echo "Verifying signature for hardn-$(OS)-$(ARCH) version v$(VERSION)..."
	@cosign verify-blob \
		--certificate hardn-$(OS)-$(ARCH).crt \
		--signature hardn-$(OS)-$(ARCH).sig \
		--certificate-identity-regexp ".*github.com/workflows/.*" \
		--certificate-oidc-issuer https://token.actions.githubusercontent.com \
		hardn-$(OS)-$(ARCH)
	@echo "✅ Signature verification successful!"

# Comprehensive verification of both SLSA provenance and Sigstore signature
.PHONY: verify-release-full
verify-release-full:
	@if [ -z "$(VERSION)" ] || [ -z "$(OS)" ] || [ -z "$(ARCH)" ]; then \
		echo "Error: Missing parameters"; \
		echo "Usage: make verify-release-full VERSION=0.3.2 OS=linux ARCH=amd64"; \
		exit 1; \
	fi
	@echo "Performing full verification (SLSA + Sigstore) for hardn-$(OS)-$(ARCH) version v$(VERSION)..."
	@make verify-release VERSION=$(VERSION) OS=$(OS) ARCH=$(ARCH)
	@make verify-signature VERSION=$(VERSION) OS=$(OS) ARCH=$(ARCH)
	@echo "✅ Full verification successful!"

# ---------------------------------------------------------------------------
# Local check (refactor plan P0.4; brief Phase 6 "local check script")
#
#   make check          run every step in order, print a per-step summary and
#                       exit non-zero if any step failed. check-tools runs first
#                       and stops everything if a tool is missing.
#   make check-<step>   run one step: check-tools, check-mod-verify, check-tidy,
#                       check-build, check-vet, check-test, check-lint,
#                       check-vuln, check-cross, check-actions
#   make golden-update  rewrite the golden files after an intended change
#                       (pkg/ui/uitest's -update), in every package whose
#                       tests link uitest. Not part of check: check-test
#                       compares against the committed files.
#
# Nothing here executes the hardn binary: main() writes under /var/log and /etc
# before it parses flags. check-build discards its output and check-cross builds
# into a temporary directory, so nothing touches $(BUILD_DIR). Container and VM
# tests are the separate it-containers and it-vm targets.
#
# Environment, set for the check targets and golden-update only (other
# targets are unchanged):
#   GOTOOLCHAIN=local       use the Go on PATH; never download a toolchain.
#   GOWORK=off              ignore any go.work in a parent directory.
#   GOFLAGS=$(CHECK_GOFLAGS)  default -mod=readonly. The committed vendor/ is
#                           incomplete (the allow-list .gitignore drops its .s,
#                           .c and .h files), so Go's default vendor mode cannot
#                           build a clean checkout. -mod=readonly ignores vendor/
#                           and, unlike -mod=mod, never rewrites go.mod or
#                           go.sum. It is also Go's default once vendor/ is gone,
#                           so it needs no change when vendor/ stops being
#                           committed. Override: make check CHECK_GOFLAGS=...
#
# Tools come from PATH: go, golangci-lint, govulncheck, zizmor and actionlint.
# check-tools fails if one is missing and warns when a version differs from
# the CHECK_*_VERSION values below. CI's check job (.github/workflows/ci.yml)
# installs the same versions and fails if these values differ from its own.
# ---------------------------------------------------------------------------

CHECK_GOFLAGS ?= -mod=readonly
CHECK_GOLANGCI_LINT_VERSION ?= 2.14.0
CHECK_GOVULNCHECK_VERSION ?= v1.9.0
CHECK_ZIZMOR_VERSION ?= 1.32.0
CHECK_ACTIONLINT_VERSION ?= 1.7.12
# Release targets of .slsa-goreleaser/*.yml, built with that file's env and flags.
CHECK_CROSS_TARGETS ?= linux/amd64 linux/arm64 darwin/amd64
# Targets check-cross also vets the whole module for, test files included
# (go vet type-checks them), with the same env and tags (next/p1d-lint). The
# other steps compile only the host's files and the builds above only what
# cmd/hardn links, so on a darwin host a Linux-only test file, such as
# pkg/ui/prompt's pty_linux_test.go, and the Linux side of every package
# cmd/hardn does not link are compiled here or nowhere. The list assumes a
# darwin development host, whose files the other steps compile: on a Linux
# host, a darwin-only test file, such as pkg/ui/prompt's pty_darwin_test.go,
# is compiled by no step. No file in the module is constrained by
# architecture, and the fallbacks for neither linux nor darwin
# (pkg/ui/prompt's *_other.go) are compiled by no target here.
CHECK_CROSS_VET_TARGETS ?= linux/amd64 linux/arm64
CHECK_STEPS = check-mod-verify check-tidy check-build check-vet check-test \
	check-lint check-vuln check-cross check-actions

check check-%: export GOTOOLCHAIN = local
check check-%: export GOWORK = off
check check-%: export GOFLAGS = $(CHECK_GOFLAGS)

# $(call check_need,<tool>,<step>): fail with a clear message if <tool> is not on PATH.
check_need = command -v $(1) >/dev/null 2>&1 || { \
	echo "$(2): '$(1)' not found on PATH. Install go, golangci-lint $(CHECK_GOLANGCI_LINT_VERSION), govulncheck $(CHECK_GOVULNCHECK_VERSION), zizmor $(CHECK_ZIZMOR_VERSION) and actionlint $(CHECK_ACTIONLINT_VERSION) (for example with mise) and retry." >&2; \
	exit 1; }

.PHONY: check check-tools $(CHECK_STEPS)

check: check-tools
	@results=; failed=; \
	for s in $(CHECK_STEPS); do \
		if $(MAKE) --no-print-directory $$s; then results="$$results ok:$$s"; \
		else results="$$results FAIL:$$s"; failed="$$failed $$s"; fi; \
	done; \
	echo; echo "check summary:"; \
	for r in $$results; do printf '  %-5s %s\n' "$${r%%:*}" "$${r#*:}"; done; \
	if [ -n "$$failed" ]; then echo "check: FAILED:$$failed" >&2; exit 1; fi; \
	echo "check: all steps passed"

check-tools:
	@echo "==> $@"
	@$(call check_need,go,$@)
	@$(call check_need,golangci-lint,$@)
	@$(call check_need,govulncheck,$@)
	@$(call check_need,zizmor,$@)
	@$(call check_need,actionlint,$@)
	@go version
	@v=$$(golangci-lint version --short 2>/dev/null); echo "golangci-lint $$v"; \
	[ "$$v" = "$(CHECK_GOLANGCI_LINT_VERSION)" ] || \
		echo "$@: warning: expected golangci-lint $(CHECK_GOLANGCI_LINT_VERSION)" >&2
	@v=$$(go version -m "$$(command -v govulncheck)" | awk '$$1 == "mod" && $$2 == "golang.org/x/vuln" { print $$3 }'); \
	echo "govulncheck $$v"; \
	[ "$$v" = "$(CHECK_GOVULNCHECK_VERSION)" ] || \
		echo "$@: warning: expected govulncheck $(CHECK_GOVULNCHECK_VERSION)" >&2
	@v=$$(zizmor --version 2>/dev/null | awk '{ print $$2 }'); echo "zizmor $$v"; \
	[ "$$v" = "$(CHECK_ZIZMOR_VERSION)" ] || \
		echo "$@: warning: expected zizmor $(CHECK_ZIZMOR_VERSION)" >&2
	@v=$$(actionlint --version 2>/dev/null | sed -n 1p); echo "actionlint $$v"; \
	[ "$$v" = "$(CHECK_ACTIONLINT_VERSION)" ] || \
		echo "$@: warning: expected actionlint $(CHECK_ACTIONLINT_VERSION)" >&2
	@echo "GOTOOLCHAIN=$$GOTOOLCHAIN GOWORK=$$GOWORK GOFLAGS=$$GOFLAGS"

check-mod-verify:
	@echo "==> $@"
	go mod verify

check-tidy:
	@echo "==> $@"
	go mod tidy -diff

check-build:
	@echo "==> $@"
	go build ./...

check-vet:
	@echo "==> $@"
	go vet ./...

check-test:
	@echo "==> $@"
	go test -count=1 ./...

check-lint:
	@echo "==> $@"
	@$(call check_need,golangci-lint,$@)
	golangci-lint run

check-vuln:
	@echo "==> $@"
	@$(call check_need,govulncheck,$@)
	govulncheck -show version ./...

check-cross:
	@echo "==> $@"
	@out=$$(mktemp -d "$${TMPDIR:-/tmp}/hardn-check-cross.XXXXXX") || exit 1; \
	trap 'rm -rf "$$out"' EXIT; \
	for t in $(CHECK_CROSS_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		echo "CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -tags=netgo -o <tmp>/hardn-$$os-$$arch ./cmd/hardn"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -tags=netgo -o "$$out/hardn-$$os-$$arch" ./cmd/hardn || exit 1; \
	done
	@for t in $(CHECK_CROSS_VET_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		echo "CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go vet -tags=netgo ./..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go vet -tags=netgo ./... || exit 1; \
	done

# check-actions (p7-actions) runs zizmor's offline audits and actionlint over
# the workflows in .github. zizmor reads .github/zizmor.yml and fails on any
# finding; its online audits need a GitHub token and run in CI's zizmor
# workflow instead (.github/workflows/zizmor.yml). actionlint also runs
# shellcheck on each run: script when shellcheck is on PATH, as it is on
# GitHub's ubuntu runners, and skips that check when it is not.
check-actions:
	@echo "==> $@"
	@$(call check_need,zizmor,$@)
	@$(call check_need,actionlint,$@)
	zizmor --offline --no-progress .github
	actionlint

# golden-update (refactor plan P1A, next/p1a-golden) rewrites testdata/golden
# from the current render in every package of the module whose tests use the
# golden harness: pkg/ui's and the dev-only gallery's (next/p1d-lint); review
# the diff and give its reason in the review description. pkg/ui/uitest
# defines -update, and a test binary that does not link uitest fails on the
# unknown flag, so -update goes only to the packages whose test binaries link
# uitest, directly or through another package. go list -test lists each test
# binary as <pkg>.test with all its dependencies; one recompiled for the test
# reads "<path> [<pkg>.test]", whose first word is still the path. pkg/ui's
# packages run first, the others after them: the gallery's tests compare its
# output with pkg/ui's golden files, which one go test of all of them could
# be rewriting while those tests read them. Five goldens write outside
# testdata/golden: pkg/screens/dns's, pkg/screens/backup's,
# pkg/screens/environment's, pkg/screens/systemdetails' and
# pkg/screens/firewall's TestParity (slices 2, 3, 9, 1 and 4, unit (b2)) also
# rewrite the marked region of docs/engine/parity-dns.md, parity-backup.md,
# parity-environment.md, parity-system-details.md and parity-firewall.md;
# review those diffs too.
# Its harness, internal/parity, links uitest, so every package whose tests
# use it is among those listed.
UI_PKG = github.com/abbott/hardn/pkg/ui
UITEST_PKG = $(UI_PKG)/uitest

.PHONY: golden-update
golden-update: export GOTOOLCHAIN = local
golden-update: export GOWORK = off
golden-update: export GOFLAGS = $(CHECK_GOFLAGS)
golden-update:
	@echo "==> $@"
	@list=$$(go list -test -f '{{.ImportPath}} {{join .Deps " "}}' ./...) || exit 1; \
	pkgs=$$(printf '%s\n' "$$list" | awk '$$1 ~ /\.test$$/ { for (i = 2; i <= NF; i++) if ($$i == "$(UITEST_PKG)") { sub(/\.test$$/, "", $$1); print $$1; next } }'); \
	[ -n "$$pkgs" ] || { echo "$@: no package's tests use $(UITEST_PKG)" >&2; exit 1; }; \
	ui=$$(printf '%s\n' $$pkgs | awk 'index($$0, "$(UI_PKG)/") == 1'); \
	rest=$$(printf '%s\n' $$pkgs | awk 'index($$0, "$(UI_PKG)/") != 1'); \
	status=0; \
	for p in "$$ui" "$$rest"; do \
		[ -n "$$p" ] || continue; \
		echo go test $$p -update; \
		go test $$p -update || status=1; \
	done; \
	exit $$status

# Container and VM integration tests need a disposable host and never run on a
# development machine. Placeholders until those suites exist.
.PHONY: it-containers it-vm
it-containers it-vm:
	@echo "$@: run on the VM host (see docs/refactor)"

# Safety gate (refactor plan P0.4): the first target in this file is `tag`,
# which runs `git tag -a` and `git push origin`, so a bare `make` would publish.
# Nothing is pushed during the refactor, so the default goal is `check` instead.
# `make tag` and `make release` still work when invoked explicitly.
.DEFAULT_GOAL := check
