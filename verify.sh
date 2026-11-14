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
# are read through the GitHub API); sha256sum or shasum. 2.68.0 is the first gh
# with every flag used here (--source-ref; --repo, --cert-identity and
# --deny-self-hosted-runners are older).
#
# The signer is checked by the exact certificate identity (--cert-identity),
# not --signer-workflow, which before gh 2.102.0 is a prefix match. The build
# runs in the reusable workflow build.yml, called with ./ from release.yml, so
# the certificate names build.yml at the release's tag:
#   https://github.com/abbott/hardn/.github/workflows/build.yml@refs/tags/v<x.y.z>
# (Fulcio's SAN is https://github.com/ + the OIDC token's job_workflow_ref,
# which names the called workflow; a ./ call runs it at the caller's ref.)
#
# Steps, each fatal: download hardn-<platform> and SHA256SUMS from the release
# into a temporary directory; check the binary's SHA-256 against its one line
# in SHA256SUMS; gh attestation verify it. Only when every step passes is the
# binary moved to ./hardn-<platform>; an existing file there is never
# overwritten. Nothing is executed.
#
# Then one request to GitHub's API for the latest release's tag
# (release_notice): where it is later than <version>, a last line says so.
# It changes nothing else, and the exit status is the verification's.
#
# The output takes hardn's look (look, below): a mark for each step and one
# closing line that names the verified file, and the later release's line
# after a blank line where there is one.

set -eu

REPO=abbott/hardn
SIGNER_WORKFLOW=https://github.com/abbott/hardn/.github/workflows/build.yml

# look sets hardn's marks and colours for file descriptor $1. Piped, or on a
# dumb terminal, they are the ASCII marks with no colour. A terminal gets
# hardn's glyphs in a UTF-8 locale (not TERM=linux, a zh, ja or ko locale, or
# with HARDN_ASCII set) and its 16-colour palette unless NO_COLOR is set. The
# words are the same either way.
look() {
	m_ok='[+]' m_bad='(x)' m_run=' - '
	bar_ok='#'
	c_ok='' c_bad='' c_mute='' t_ok='' c_off=''
	[ -t "$1" ] || return 0
	case ${TERM:-dumb} in dumb) return 0 ;; esac
	if [ -z "${HARDN_ASCII:-}" ]; then
		case $TERM/${LC_ALL:-${LC_CTYPE:-${LANG:-}}} in
		linux/* | */zh* | */ja* | */ko*) ;;
		*.[Uu][Tt][Ff]-8* | *.[Uu][Tt][Ff]8*)
			m_ok=' ⛛ ' m_bad=' ⨳ '
			bar_ok='█'
			;;
		esac
	fi
	[ -z "${NO_COLOR:-}" ] || return 0
	e=$(printf '\033')
	c_ok="${e}[92m" c_bad="${e}[91m" c_mute="${e}[90m"
	t_ok="${e}[1;92m" c_off="${e}[0m"
	bar_ok="${e}[42m ${e}[0m"
}

# A step's row: its mark in columns 2 to 4, its words from column 6. running
# is a step under way, detail a line under a step's words.
ok() { printf '  %s%s%s %s\n' "$c_ok" "$m_ok" "$c_off" "$1"; }
running() { printf '  %s%s %s%s\n' "$c_mute" "$m_run" "$1" "$c_off"; }
detail() { printf '      %s%s%s\n' "$c_mute" "$1" "$c_off"; }

# die says why verify.sh stops, on stderr: the refused mark, then its lines.
die() {
	look 2
	lead="  $m_bad "
	printf 'verify.sh: %s\n' "$*" | while IFS= read -r l; do
		printf '%s%s%s%s\n' "$c_bad" "$lead" "$l" "$c_off"
		lead='      '
	done >&2
	exit 1
}

# num prints $1, one to six digits, without its leading zeros.
num() {
	case $1 in
	'' | *[!0-9]* | ???????*) return 1 ;;
	esac
	n=$1
	while :; do
		case $n in
		0?*) n=${n#0} ;;
		*) break ;;
		esac
	done
	printf '%s\n' "$n"
}

# later: version $1 is later than $2, each <major>.<minor>.<patch> of one to
# six digits, compared by number, as hardn reads a tag.
later() {
	for v in "$1" "$2"; do
		case $v in
		*.*.*.*) return 1 ;;
		*.*.*) ;;
		*) return 1 ;;
		esac
	done
	a=$1 b=$2
	for _ in 1 2 3; do
		x=$(num "${a%%.*}") || return 1
		y=$(num "${b%%.*}") || return 1
		[ "$x" -eq "$y" ] || {
			[ "$x" -gt "$y" ]
			return
		}
		a=${a#*.} b=${b#*.}
	done
	return 1
}

# release_notice says, after a blank line, that a release later than $1
# (<major>.<minor>.<patch>) is out. It asks GitHub's API once for the latest
# release's tag, for 5 seconds at most. No answer, a rate limit, or a tag
# that is not v<major>.<minor>.<patch> or is no later: it says nothing. It
# never fails.
release_notice() {
	curl -fsL --proto '=https' --tlsv1.2 --max-time 5 --max-filesize 65536 \
		-o "$tmp/newer.json" "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null || return 0
	latest=$(awk '{ gsub(/[,{}]/, "\n"); print }' "$tmp/newer.json" 2>/dev/null |
		sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)"[[:space:]]*$/\1/p') || return 0
	case $latest in
	v*) ;;
	*) return 0 ;;
	esac
	later "${latest#v}" "$1" || return 0
	printf '\nhardn %s available (https://github.com/%s)\n' "$latest" "$REPO" || :
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
help=$(gh attestation verify --help 2>/dev/null) || help=
for flag in --repo --cert-identity --source-ref --deny-self-hosted-runners; do
	printf '%s\n' "$help" | grep -q -- "$flag" ||
		die "needs gh 2.68.0 or later (gh attestation verify $flag); found: $(gh --version 2>/dev/null | head -n 1)"
done
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

look 1

# 1. Download.
running "Downloading $binary and SHA256SUMS from $tag ..."
curl -fsSL -o "$tmp/$binary" "$base/$binary" || die "cannot download $base/$binary"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || die "cannot download $base/SHA256SUMS"

ok "Downloaded $binary and SHA256SUMS"

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
ok "SHA-256 matches SHA256SUMS"
detail "$got"

# 3. Provenance: signed by build.yml in abbott/hardn at the tag (the exact
# certificate identity), from the tag, on a GitHub-hosted runner.
printf '\n'
running "Verifying the provenance attestation ..."
gh attestation verify "$tmp/$binary" \
	--repo "$REPO" \
	--cert-identity "$SIGNER_WORKFLOW@refs/tags/$tag" \
	--source-ref "refs/tags/$tag" \
	--deny-self-hosted-runners ||
	die "gh attestation verify failed for $binary at $tag"

mv "$tmp/$binary" "$dest" || die "cannot move the verified binary to $dest"
ok "Provenance verified: built by"
detail "$SIGNER_WORKFLOW@refs/tags/$tag"
printf '\n%s %sVerified %s (hardn %s%s)\n' "$bar_ok" "$t_ok" "$dest" "$tag" "$c_off"

# 4. A later release, said and nothing more: the binary is verified either
# way, so an interrupt from here on ends with status 0.
trap 'exit 0' HUP INT TERM
release_notice "$version" || :
