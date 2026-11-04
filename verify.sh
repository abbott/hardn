#!/bin/sh
# Verify a hardn release binary: its SHA-256 against the release's SHA256SUMS,
# then its SLSA Build Level 3 provenance, a GitHub artifact attestation signed
# by abbott/hardn's build workflow (.github/workflows/build.yml) for the
# release's tag.
#
# Usage: ./verify.sh <version> <platform>
#   version   0.4.0 or v0.4.0. v0.4.0 and later: v0.3.x shipped .intoto.jsonl
#             files instead (slsa-verifier) and no SHA256SUMS.
#   platform  linux-amd64, linux-arm64 or darwin-amd64
#
# Needs: curl; gh 2.68.0 or later, signed in (gh auth login: the attestations
# are read through the GitHub API); sha256sum or shasum.
#
# Steps, each fatal: download hardn-<platform> and SHA256SUMS from the release
# into a temporary directory; check the binary's SHA-256 against its one line
# in SHA256SUMS; gh attestation verify it. Only when every step passes is the
# binary moved to ./hardn-<platform>; an existing file there is never
# overwritten. Nothing is executed.

set -eu

REPO=abbott/hardn
SIGNER_WORKFLOW=abbott/hardn/.github/workflows/build.yml

die() {
	printf 'verify.sh: %s\n' "$*" >&2
	exit 1
}

usage() {
	printf 'Usage: %s <version> <platform>\n' "$0" >&2
	printf '  version   e.g. 0.4.0 (v0.4.0 and later)\n' >&2
	printf '  platform  linux-amd64, linux-arm64 or darwin-amd64\n' >&2
	exit 2
}

[ "$#" -eq 2 ] || usage

version=${1#v}
platform=$2

case $version in
'' | *[!0-9.]* | .* | *. | *..*) die "version '$1' is not <major>.<minor>.<patch>" ;;
esac
# Exactly two dots.
case $version in
*.*.*.*) die "version '$1' is not <major>.<minor>.<patch>" ;;
*.*.*) ;;
*) die "version '$1' is not <major>.<minor>.<patch>" ;;
esac
tag=v$version

case $platform in
linux-amd64 | linux-arm64 | darwin-amd64) ;;
*) die "platform '$platform' is not one of linux-amd64, linux-arm64, darwin-amd64" ;;
esac

binary=hardn-$platform
dest=./$binary

# Tools.
command -v curl >/dev/null 2>&1 || die "needs curl"
command -v gh >/dev/null 2>&1 || die "needs gh 2.68.0 or later (https://cli.github.com)"
gh attestation verify --help 2>/dev/null | grep -q -- '--source-ref' ||
	die "needs gh 2.68.0 or later (gh attestation verify --source-ref); found: $(gh --version 2>/dev/null | head -n 1)"
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | awk '{ print $1 }'; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | awk '{ print $1 }'; }
else
	die "needs sha256sum or shasum"
fi

{ [ ! -e "$dest" ] && [ ! -L "$dest" ]; } || die "$dest already exists; move it away and retry"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/hardn-verify.XXXXXX") || die "cannot make a temporary directory"
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' HUP INT TERM

base=https://github.com/$REPO/releases/download/$tag

# 1. Download.
printf 'Downloading %s and SHA256SUMS from %s ...\n' "$binary" "$tag"
curl -fsSL -o "$tmp/$binary" "$base/$binary" || die "cannot download $base/$binary"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || die "cannot download $base/SHA256SUMS"

# 2. SHA-256 against SHA256SUMS: exactly one line names the binary.
lines=$(awk -v f="$binary" '$2 == f || $2 == "*" f' "$tmp/SHA256SUMS" | wc -l | tr -d ' ')
[ "$lines" = 1 ] || die "SHA256SUMS has $lines lines for $binary, not 1"
want=$(awk -v f="$binary" '$2 == f || $2 == "*" f { print tolower($1) }' "$tmp/SHA256SUMS")
got=$(sha256 "$tmp/$binary" | tr 'A-F' 'a-f')
case $want in
'' | *[!0-9a-f]*) die "SHA256SUMS's entry for $binary is not a SHA-256" ;;
esac
[ "${#want}" -eq 64 ] || die "SHA256SUMS's entry for $binary is not a SHA-256"
[ "$got" = "$want" ] || die "$binary's SHA-256 is $got; SHA256SUMS says $want"
printf 'SHA-256 matches SHA256SUMS: %s\n' "$got"

# 3. Provenance: signed by build.yml in abbott/hardn, from the tag, on a
# GitHub-hosted runner.
printf 'Verifying the provenance attestation ...\n'
gh attestation verify "$tmp/$binary" \
	--repo "$REPO" \
	--signer-workflow "$SIGNER_WORKFLOW" \
	--source-ref "refs/tags/$tag" \
	--deny-self-hosted-runners ||
	die "gh attestation verify failed for $binary at $tag"

mv "$tmp/$binary" "$dest" || die "cannot move the verified binary to $dest"
printf 'Verified: %s, %s, built by %s from %s at %s.\n' "$dest" "$tag" "$SIGNER_WORKFLOW" "$REPO" "refs/tags/$tag"
