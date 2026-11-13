#!/bin/sh
# Install hardn: download this machine's binary from a hardn release, check it,
# and install it as /usr/local/bin/hardn.
#
#   curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh
#   curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo env HARDN_VERSION=0.4.0 sh
#
# Fails closed: any step that fails stops the script and installs nothing.
#   1. The platform: Linux x86_64, Linux aarch64 or arm64, Darwin x86_64 or
#      arm64 (the darwin-amd64 build, through Rosetta 2 on Apple silicon).
#      Anything else is refused. There is no armv7 build.
#   2. Root: it writes /usr/local/bin and /etc/hardn.
#   3. The release: the latest release's tag from the GitHub API, or
#      HARDN_VERSION (0.4.0 or v0.4.0). Its URLs are built from the tag and the
#      exact asset name.
#   4. The binary and SHA256SUMS, downloaded over HTTPS only (TLS 1.2 or
#      later, redirects included) into a temporary directory, the binary
#      with curl's progress bar where stderr is a terminal. The
#      binary's SHA-256 must match its one line in SHA256SUMS. Releases before
#      v0.4.0 have no SHA256SUMS and are refused.
#   5. The provenance attestation, checked as verify.sh does when gh 2.68.0 or
#      later is on PATH and signed in; otherwise the script says it was not
#      checked and prints the command that checks it.
#   6. Refused if /usr/local/bin/hardn is a directory or a link to one.
#      install -m 0755 to a temporary name in /usr/local/bin, its SHA-256
#      checked again, then mv over /usr/local/bin/hardn; /etc/hardn created
#      (0755) if missing. Nothing
#      else is written, and hardn is not run.
#
# The output takes hardn's look (look, below): a mark for each step, a
# Warning callout where provenance is not checked, and one closing line that
# says what was installed. It starts after a blank line and ends with one,
# whether it installs or stops, with a blank line between its parts, and
# keeps to 80 columns but for the attestation's identity: a URL can't break.
#
# HARDN_INSTALL_ROOT (default empty) is prefixed to /usr/local/bin and
# /etc/hardn. It exists for scripts/install_test.sh, which installs into a
# temporary directory; with it set, the downloads go under TMPDIR, not /tmp.

set -eu

REPO=abbott/hardn
SIGNER_WORKFLOW=https://github.com/abbott/hardn/.github/workflows/build.yml
SCRIPT_URL=https://raw.githubusercontent.com/abbott/hardn/main/install.sh

tmp=
staged=
installed=
spoke=

# look sets hardn's marks and colours for file descriptor $1. Piped, or on a
# dumb terminal, they are the ASCII marks with no colour. A terminal gets
# hardn's glyphs in a UTF-8 locale (not TERM=linux, a zh, ja or ko locale, or
# with HARDN_ASCII set) and its 16-colour palette unless NO_COLOR is set. The
# words are the same either way.
look() {
	m_ok='[+]' m_warn='[ ]' m_bad='(x)' m_run=' - '
	bar_ok='#' bar_warn='#'
	c_ok='' c_warn='' c_bad='' c_mute='' t_ok='' t_warn='' c_off=''
	[ -t "$1" ] || return 0
	case ${TERM:-dumb} in dumb) return 0 ;; esac
	if [ -z "${HARDN_ASCII:-}" ]; then
		case $TERM/${LC_ALL:-${LC_CTYPE:-${LANG:-}}} in
		linux/* | */zh* | */ja* | */ko*) ;;
		*.[Uu][Tt][Ff]-8* | *.[Uu][Tt][Ff]8*)
			m_ok=' ⛛ ' m_warn=' ⭘ ' m_bad=' ⨳ '
			bar_ok='█' bar_warn='█'
			;;
		esac
	fi
	[ -z "${NO_COLOR:-}" ] || return 0
	e=$(printf '\033')
	c_ok="${e}[92m" c_warn="${e}[33m" c_bad="${e}[91m" c_mute="${e}[90m"
	t_ok="${e}[1;92m" t_warn="${e}[1;33m" c_off="${e}[0m"
	bar_ok="${e}[42m ${e}[0m" bar_warn="${e}[43m ${e}[0m"
}

# A step's row: its mark in columns 2 to 4, its words from column 6. running
# is a step under way, detail a line under a step's words.
ok() { printf '  %s%s%s %s\n' "$c_ok" "$m_ok" "$c_off" "$1"; }
warn() { printf '  %s%s%s %s\n' "$c_warn" "$m_warn" "$c_off" "$1"; }
running() { printf '  %s%s %s%s\n' "$c_mute" "$m_run" "$1" "$c_off"; }
detail() { printf '      %s%s%s\n' "$c_mute" "$1" "$c_off"; }
muted() { printf '%s%s%s\n' "$c_mute" "$1" "$c_off"; }

# die says why install.sh stops, on stderr: a blank line apart from the
# progress above it (spoke), the refused mark, its lines, and a blank line.
die() {
	look 2
	[ -z "$spoke" ] || printf '\n' >&2
	lead="  $m_bad "
	{
		printf 'install.sh: %s\n' "$*"
		[ -n "$installed" ] || printf 'install.sh: nothing was installed.\n'
	} | while IFS= read -r l; do
		printf '%s%s%s%s\n' "$c_bad" "$lead" "$l" "$c_off"
		lead='      '
	done >&2
	printf '\n' >&2
	exit 1
}

cleanup() {
	[ -z "$staged" ] || rm -f "$staged"
	[ -z "$tmp" ] || rm -rf "$tmp"
}

check_version() {
	case $1 in
	'' | *[!0-9.]* | .* | *. | *..* | *.*.*.*) return 1 ;;
	*.*.*) return 0 ;;
	*) return 1 ;;
	esac
}

# All of it runs from the last line, so a download cut short runs nothing.
main() {
	look 1
	printf '\n'
	root=${HARDN_INSTALL_ROOT:-}
	bindir=$root/usr/local/bin
	confdir=$root/etc/hardn
	dest=$bindir/hardn

	case $root in
	'' | /*) ;;
	*) die "HARDN_INSTALL_ROOT must be an absolute path: $root" ;;
	esac

	# 1. The platform.
	os=$(uname -s)
	arch=$(uname -m)
	note=
	case $os/$arch in
	Linux/x86_64) asset=hardn-linux-amd64 ;;
	Linux/aarch64 | Linux/arm64) asset=hardn-linux-arm64 ;;
	Darwin/x86_64) asset=hardn-darwin-amd64 ;;
	Darwin/arm64)
		asset=hardn-darwin-amd64
		note="Apple silicon: the darwin-amd64 build runs through Rosetta 2."
		;;
	Linux/armv7* | Linux/armv6*)
		die "$os $arch is not supported: there is no armv7 build (releases have linux-amd64, linux-arm64 and darwin-amd64)"
		;;
	*) die "$os $arch is not supported (releases have linux-amd64, linux-arm64 and darwin-amd64)" ;;
	esac

	# 2. Root.
	if [ "$(id -u)" != 0 ]; then
		if [ -n "${HARDN_VERSION:-}" ]; then
			run="sudo env HARDN_VERSION=$HARDN_VERSION sh"
		else
			run="sudo sh"
		fi
		die "$(printf 'run it as root (it writes /usr/local/bin and /etc/hardn):\n  curl -fsSL %s | %s\n(or doas in place of sudo)' "$SCRIPT_URL" "$run")"
	fi

	# Tools.
	command -v curl >/dev/null 2>&1 || die "needs curl"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256() { sha256sum "$1" | awk '{ print $1 }'; }
	elif command -v shasum >/dev/null 2>&1; then
		sha256() { shasum -a 256 "$1" | awk '{ print $1 }'; }
	else
		die "needs sha256sum or shasum"
	fi

	# /tmp, not TMPDIR: /tmp is sticky, and in a directory another user owns,
	# that user could swap the downloads between their check and the install.
	# TMPDIR only with HARDN_INSTALL_ROOT, for the test.
	tmpbase=/tmp
	[ -z "$root" ] || tmpbase=${TMPDIR:-/tmp}
	tmp=$(mktemp -d "$tmpbase/hardn-install.XXXXXX") || die "cannot make a temporary directory"
	trap cleanup EXIT
	trap 'exit 1' HUP INT TERM

	# 3. The release.
	if [ -n "${HARDN_VERSION:-}" ]; then
		version=${HARDN_VERSION#v}
		check_version "$version" || die "HARDN_VERSION '$HARDN_VERSION' is not <major>.<minor>.<patch>"
	else
		api=https://api.github.com/repos/$REPO/releases/latest
		curl -fsSL --proto '=https' --tlsv1.2 -o "$tmp/latest.json" "$api" || die "cannot read the latest release from $api"
		# One "tag_name" key: split the JSON at commas and braces, then match the
		# whole key and value.
		tags=$(awk '{ gsub(/[,{}]/, "\n"); print }' "$tmp/latest.json" |
			sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)"[[:space:]]*$/\1/p')
		[ -n "$tags" ] || die "no tag_name in $api"
		[ "$(printf '%s\n' "$tags" | wc -l | tr -d ' ')" = 1 ] || die "more than one tag_name in $api"
		version=${tags#v}
		if [ "$tags" != "v$version" ] || ! check_version "$version"; then
			die "the latest release's tag '$tags' is not v<major>.<minor>.<patch>"
		fi
	fi
	tag=v$version
	base=https://github.com/$REPO/releases/download/$tag

	printf 'Installing hardn %s: %s (%s %s)\n' "$tag" "$asset" "$os" "$arch"
	[ -z "$note" ] || muted "$note"
	spoke=1

	# 4. Download, and the SHA-256 against SHA256SUMS.
	status=0
	curl -fsSL --proto '=https' --tlsv1.2 -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || status=$?
	case $status in
	0) ;;
	22) die "cannot download $base/SHA256SUMS (curl's HTTP error is above). A 404 means there is no release $tag, or it is older than v0.4.0: v0.3.2 and earlier have no SHA256SUMS, and install.sh installs only a release it can check" ;;
	*) die "cannot download $base/SHA256SUMS" ;;
	esac
	printf '\n'
	running "Downloading $asset ..."
	meter=--silent
	[ ! -t 2 ] || meter=--progress-bar
	curl -fSL "$meter" --proto '=https' --tlsv1.2 -o "$tmp/$asset" "$base/$asset" || die "cannot download $base/$asset"

	lines=$(awk -v f="$asset" '$2 == f || $2 == "*" f' "$tmp/SHA256SUMS" | wc -l | tr -d ' ')
	[ "$lines" = 1 ] || die "SHA256SUMS has $lines lines for $asset, not 1"
	want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print tolower($1) }' "$tmp/SHA256SUMS")
	case $want in
	'' | *[!0-9a-f]*) die "SHA256SUMS's entry for $asset is not a SHA-256" ;;
	esac
	[ "${#want}" -eq 64 ] || die "SHA256SUMS's entry for $asset is not a SHA-256"
	got=$(sha256 "$tmp/$asset" | tr 'A-F' 'a-f')
	[ "$got" = "$want" ] || die "$asset's SHA-256 is $got; SHA256SUMS says $want"
	ok "Downloaded $asset"
	ok "SHA-256 matches SHA256SUMS"
	detail "$got"

	# 5. Provenance: verify.sh's check, when gh can make it.
	identity=$SIGNER_WORKFLOW@refs/tags/$tag
	gh_ok=
	if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
		help=$(gh attestation verify --help 2>/dev/null) || help=
		gh_ok=1
		for flag in --repo --cert-identity --source-ref --deny-self-hosted-runners; do
			printf '%s\n' "$help" | grep -q -- "$flag" || gh_ok=
		done
	fi
	if [ -n "$gh_ok" ]; then
		printf '\n'
		running "Verifying the provenance attestation ..."
		gh attestation verify "$tmp/$asset" \
			--repo "$REPO" \
			--cert-identity "$identity" \
			--source-ref "refs/tags/$tag" \
			--deny-self-hosted-runners ||
			die "gh attestation verify failed for $asset at $tag"
		ok "Provenance verified: built by"
		detail "$identity"
	else
		warn "Provenance not checked"
		printf '\n%s %sWarning%s\n' "$bar_warn" "$t_warn" "$c_off"
		printf '%s Provenance needs gh 2.68.0 or later, signed in (gh auth login).\n' "$bar_warn"
		printf '\nTo check it:\n'
		printf '  gh attestation verify /usr/local/bin/hardn --repo %s \\\n' "$REPO"
		printf '    --source-ref refs/tags/%s --deny-self-hosted-runners \\\n' "$tag"
		printf '    --cert-identity \\\n'
		printf '    %s\n' "$identity"
		printf 'or, on another machine: sh verify.sh %s %s\n' "$version" "${asset#hardn-}"
	fi

	# 6. Install: a temporary name beside the destination, then mv over it.
	# mv onto a directory, or a link to one, would put hardn inside it.
	[ ! -d "$dest" ] || die "$dest is a directory (or a link to one), not the hardn binary: move it aside, then run install.sh again"
	[ -d "$bindir" ] || mkdir -m 0755 "$bindir" || die "cannot create $bindir"
	staged=$bindir/.hardn.install.$$
	install -m 0755 "$tmp/$asset" "$staged" || die "cannot write $staged"
	# The bytes moved are the bytes checked.
	got=$(sha256 "$staged" | tr 'A-F' 'a-f')
	if [ "$got" != "$want" ]; then
		rm -f "$staged"
		die "$staged's SHA-256 is $got, not the $want checked: refused"
	fi
	mv -f "$staged" "$dest" || die "cannot move $staged to $dest"
	staged=
	installed=1
	printf '\n'
	if [ ! -d "$confdir" ]; then
		mkdir -m 0755 "$confdir" || die "cannot create $confdir"
		ok "Created /etc/hardn"
		printf '\n'
	fi
	muted "hardn writes /etc/hardn/hardn.yml.example the first time it runs."
	# shellcheck disable=SC2016 # the backticks are printed, not run
	muted 'Run `hardn --help`.'
	muted "hardn comes with no warranty: GNU AGPL v3, sections 15 and 16."
	printf '\n%s %sInstalled hardn %s as /usr/local/bin/hardn.%s\n\n' "$bar_ok" "$t_ok" "$tag" "$c_off"
}

main "$@"
