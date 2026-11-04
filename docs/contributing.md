# Contributing to hardn

How to build hardn, run its checks, update its goldens and cut a release. For a security problem, don't open an issue: see [SECURITY.md](../.github/SECURITY.md).

## Go and the tools

- **Go 1.26 or later** (go.mod's `go 1.26.0`). Release binaries are built with **Go 1.27.3**, go.mod's `toolchain` line; with Go's default `GOTOOLCHAIN`, `go` fetches it for you. `make check` sets `GOTOOLCHAIN=local` and uses the Go on your `PATH`, so install 1.27.3 to check with the release's Go. CI's checks fail if their Go is not the `toolchain` line's.
- **The check tools**, at the makefile's `CHECK_*_VERSION` values, which CI installs too:

  | Tool | Version |
  |---|---|
  | golangci-lint | 2.14.0 |
  | govulncheck | v1.9.0 |
  | zizmor | 1.32.0 |
  | actionlint | 1.7.12 |
  | shellcheck | 0.11.0 |

  [mise](https://mise.jdx.dev) is one way to install them. `make check` warns when a version differs and stops when a tool is missing.
- **No `replace` directive and no `vendor/`.** go.mod has no `replace`, and the repository commits no `vendor/`; the checks build with `-mod=readonly`. The makefile's older targets that add one or the other (`restore-dev`, `setup-dev`, `dev`, `dev-build`, `dev-test`, `dev-run`, `run`, `vendor`, `fix-vendor`) are not part of the workflow: don't use them.
- **The `.gitignore` is an allow-list:** it ignores everything but the files it names (`*.go`, `*.md` under `docs/`, the scripts, ...). A new kind of file needs a line there before `git add` sees it.

## Building

```sh
git clone https://github.com/abbott/hardn.git
cd hardn
make build          # build/hardn, from ./cmd/hardn
```

`make build`, `make linux`, `make arm` and `make darwin` build with the release's flags (`CGO_ENABLED=0`, `-trimpath`, `-tags=netgo`, the version, commit and commit date in the ldflags), so with go.mod's toolchain a rebuild of a commit is byte-identical to the release's binary.

**Don't run hardn on your development machine.** Run live, it writes under `/etc` and `/var/log`, and it is made for Debian, Ubuntu and Alpine hosts. Try it on a disposable VM or container. Nothing in `make check` runs the binary.

## make check

`make` with no target is `make check`. It runs eleven steps in order. `check-tools` comes first and stops everything if a tool is missing; each of the other ten runs even when one before it failed, and a summary at the end lists each step as `ok` or `FAIL`. Any one step runs alone as `make <step>`.

| Step | What it does |
|---|---|
| `check-tools` | every tool is on `PATH`; warns when a version is not the makefile's; prints `GOTOOLCHAIN`, `GOWORK` and `GOFLAGS` |
| `check-mod-verify` | `go mod verify`: the downloaded modules match go.sum |
| `check-tidy` | `go mod tidy -diff`: go.mod and go.sum need no tidying |
| `check-build` | `go build ./...`, its output discarded |
| `check-vet` | `go vet ./...` |
| `check-test` | `go test -count=1 ./...`, the goldens compared with the committed files |
| `check-lint` | `golangci-lint run`, with the repository's settings and import rules |
| `check-vuln` | `govulncheck ./...`: known vulnerabilities in code hardn calls |
| `check-cross` | builds the three release targets (linux/amd64, linux/arm64, darwin/amd64) with the release's flags into a temporary directory, then vets the whole module, tests included, for linux/amd64 and linux/arm64, whose files a macOS host's steps don't compile |
| `check-actions` | zizmor's offline audits and actionlint (which runs shellcheck on each `run:` script) over `.github` |
| `check-install` | shellcheck, `sh -n` and `dash -n` over install.sh, verify.sh and `scripts/install_test.sh`, then that test: install.sh against stubs and fake releases in a temporary directory, with no network |

Every check target runs with `GOTOOLCHAIN=local`, `GOWORK=off` and `GOFLAGS=-mod=readonly`.

CI (`.github/workflows/ci.yml`) runs `make check` on each push to `main` and each pull request, and `go vet` and `go test` on Debian 13 and 12, Ubuntu 26.04 and 24.04, and Alpine 3.24 and 3.23 images. `.github/workflows/zizmor.yml` adds zizmor's online audits.

## Goldens

Many tests compare their output with a committed golden file. After a change that alters the output on purpose, rewrite them and review the diff:

- **One package:** `go test ./<pkg>/ -update`, in a package whose tests define `-update`.
- **Every screen and component golden:** `make golden-update`, which passes `-update` to each package whose tests use the golden harness (`pkg/ui/uitest`). Some of these also rewrite a marked part of a page under `docs/engine/` (the parity pages); review those diffs too.

Say why the goldens changed in the pull request. `make check` compares; it never rewrites.

## Test data and examples

Use synthetic values only, in tests, goldens and docs: user names such as `george`, `alice` and `carol`; host names such as `web-01`; addresses from `192.0.2.0/24`; domains under `example.net`. Never a real host, address, user or key.

## Pull requests

- Branch from `main`, and keep a pull request to one change.
- `make check` passes before you open it.
- Commit messages start with the package or area changed, or a type: `cmd/hardn: ...`, `pkg/config: ...`, `docs: ...`, `fix(cmd): ...`.
- A change users will notice gets a line in `docs/releases/unreleased.md`.

## Releases

hardn uses semantic versioning. A release is cut from a tag `v<major>.<minor>.<patch>`; a tag with a suffix (`v1.2.3-rc1`) is not released.

1. **Release notes.** `docs/releases/unreleased.md` becomes `docs/releases/v<x.y.z>.md`. The release's body is that file at the tagged commit; the release fails before building anything when it is missing or empty.
2. **The version.** Set `VERSION` (and `VERSION_MAJOR`, `VERSION_MINOR`, `VERSION_PATCH`) in the makefile, and commit.
3. **The tag.** `make tag` makes the annotated tag `v$(VERSION)` locally and prints the command that pushes it, `git push origin refs/tags/v<x.y.z>`. `make release` runs `go test`, builds the three binaries with archives and `.sha256` files under `build/` for a local look, then makes the tag the same way. **Neither pushes anything.**
4. **Pushing the tag** is the maintainer's step, and it starts `.github/workflows/release.yml`:
   - checks the tag's form and the release notes;
   - builds the three binaries in `.github/workflows/build.yml`, and attests each one's build provenance (SLSA Build Level 3, a GitHub artifact attestation);
   - writes an SPDX SBOM and `SHA256SUMS`;
   - creates the release as a draft, with the notes as its body, and uploads every file;
   - downloads them back from the draft, checks them against `SHA256SUMS`, and verifies each binary's attestation;
   - only then publishes it, as the latest release unless a newer version is already out.

   A failure anywhere leaves at most a draft. A draft left by a failed run is deleted by hand before the run is retried.

The release files are `hardn-linux-amd64`, `hardn-linux-arm64`, `hardn-darwin-amd64`, `hardn-sbom.spdx.json` and `SHA256SUMS`. `verify.sh` (or `make verify-release PLATFORM=linux-amd64`) checks a binary against them; [SECURITY.md](../.github/SECURITY.md) says how.

## Issues

Report bugs in the [issue tracker](https://github.com/abbott/hardn/issues), with the steps that show the problem, the OS and its version, and `hardn --version`'s output.
