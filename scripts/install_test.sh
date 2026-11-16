#!/bin/sh
# Tests install.sh without the network and without writing outside a temporary
# directory: stubs for uname, id, curl, gh, sha256sum and shasum come first in
# PATH, a fake release is served from the temporary directory, and
# HARDN_INSTALL_ROOT points install.sh's /usr/local/bin and /etc/hardn there.
# install, mv and mkdir are wrappers that refuse any absolute path outside the
# temporary directory, then run the real tool. PATH holds only the stubs and
# links to the tools install.sh needs, so gh is absent unless a case adds it.
#
# Each case runs under every shell found of sh, dash and busybox sh, except a
# busybox that runs its own applets ahead of PATH (Ubuntu's busybox-static): it
# would bypass the stubs and the wrappers, so it is skipped, and the run says
# so. Run from anywhere: sh scripts/install_test.sh. Exits 1 if any case
# fails. make check-install runs it.
#
# It also tests the later release's line that install.sh, given
# HARDN_VERSION, and verify.sh end with (their release_notice), verify.sh
# run in a directory of the case against the same stubs.

set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
# INSTALL_SH tests another copy, such as a deliberately broken one.
script=${INSTALL_SH:-$repo/install.sh}
verify=${VERIFY_SH:-$repo/verify.sh}

work=$(mktemp -d "${TMPDIR:-/tmp}/hardn-install-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
trap 'exit 1' HUP INT TERM

# The shells.
shells=
for s in sh dash; do
	p=$(command -v "$s" 2>/dev/null) || continue
	shells="$shells $p"
done
bb=$(command -v busybox 2>/dev/null) || bb=
skipped=
if [ -n "$bb" ]; then
	# A busybox that prefers its applets finds id as an applet, not a path.
	case $("$bb" sh -c 'command -v id' 2>/dev/null) in
	/*) shells="$shells busybox" ;;
	*) skipped="$bb sh: it runs its own applets ahead of PATH, so the stubs cannot stand in" ;;
	esac
fi

# The real tools, by absolute path.
real() {
	command -v "$1" 2>/dev/null || {
		printf 'install_test.sh: %s not found\n' "$1" >&2
		exit 1
	}
}
if command -v sha256sum >/dev/null 2>&1; then
	sum_cmd="$(real sha256sum)"
else
	sum_cmd="$(real shasum) -a 256"
fi
hash() { $sum_cmd "$1" | awk '{ print $1 }'; }

# Tools install.sh uses that are not stubbed or wrapped.
mkdir -p "$work/tools"
for t in awk cat grep mktemp rm sed tr wc; do
	ln -s "$(real "$t")" "$work/tools/$t"
done

# Stubs.
stubs=$work/stubs
mkdir -p "$stubs" "$work/gh" "$work/sum-sha256sum" "$work/sum-shasum"

cat >"$stubs/uname" <<'EOF'
#!/bin/sh
case $1 in
-s) printf '%s\n' "$FAKE_OS" ;;
-m) printf '%s\n' "$FAKE_ARCH" ;;
*) exit 2 ;;
esac
EOF

cat >"$stubs/id" <<'EOF'
#!/bin/sh
[ "$1" = -u ] || exit 2
printf '%s\n' "$FAKE_UID"
EOF

# curl: -o <file> and a URL; refuses a call without --proto =https and
# --tlsv1.2 (but verify.sh's downloads, FAKE_CURL_LAX=1); logs each URL, with
# its --max-time where given; serves the fake release; a missing file is curl
# -f's HTTP error, exit 22. FAKE_API_STATUS is the exit status of a request
# to the API, such as 28, curl's for a timeout. FAKE_API_SIGNAL is a signal
# a request to the API sends the script, as Ctrl-C during it would.
cat >"$stubs/curl" <<EOF
#!/bin/sh
out=
url=
proto=
tls=
limit=
while [ \$# -gt 0 ]; do
	case \$1 in
	-o) out=\$2; shift ;;
	--proto) proto=\$2; shift ;;
	--tlsv1.2) tls=1 ;;
	--max-time) limit=" max-time=\$2"; shift ;;
	--max-filesize) shift ;;
	-*) ;;
	*) url=\$1 ;;
	esac
	shift
done
if [ "\${FAKE_CURL_LAX:-0}" != 1 ] || [ -n "\$limit" ]; then
	if [ "\$proto" != =https ] || [ -z "\$tls" ]; then
		printf 'curl stub: %s without --proto =https --tlsv1.2\n' "\$url" >&2
		exit 2
	fi
fi
printf '%s%s\n' "\$url" "\$limit" >>"\$FAKE_LOG/curl.log"
case \$url in
https://api.github.com/*)
	if [ -n "\${FAKE_API_SIGNAL:-}" ]; then
		kill -s "\$FAKE_API_SIGNAL" "\$PPID"
		exit 130
	fi
	[ -z "\${FAKE_API_STATUS:-}" ] || exit "\$FAKE_API_STATUS"
	;;
esac
case \$url in
https://api.github.com/repos/abbott/hardn/releases/latest) f=\$FAKE_REL/\${FAKE_LATEST:-latest}.json ;;
https://github.com/abbott/hardn/releases/download/*) f=\$FAKE_REL/\${url#https://github.com/abbott/hardn/releases/download/} ;;
*) f= ;;
esac
if [ -z "\$f" ] || [ ! -f "\$f" ]; then
	printf 'curl: (22) The requested URL returned error: 404\n' >&2
	exit 22
fi
$(real cat) "\$f" >"\$out"
EOF

# gh: FAKE_GH_AUTH=0 is signed out; FAKE_GH_HELP=old lacks --source-ref;
# FAKE_GH_VERIFY=1 fails the verification. Logs its arguments.
cat >"$work/gh/gh" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$FAKE_LOG/gh.log"
case $1/$2 in
auth/status) [ "${FAKE_GH_AUTH:-1}" = 1 ] ;;
attestation/verify)
	if [ "${3:-}" = --help ]; then
		if [ "${FAKE_GH_HELP:-new}" = new ]; then
			printf '  --cert-identity string\n  --deny-self-hosted-runners\n  -R, --repo string\n  --source-ref string\n'
		else
			printf '  --cert-identity string\n  --deny-self-hosted-runners\n  -R, --repo string\n'
		fi
		exit 0
	fi
	[ "${FAKE_GH_VERIFY:-0}" = 0 ]
	;;
*) exit 1 ;;
esac
EOF

printf '#!/bin/sh\nexec %s "$@"\n' "$sum_cmd" >"$work/sum-sha256sum/sha256sum"
printf '#!/bin/sh\nexec %s "$@"\n' "$(real shasum 2>/dev/null || printf '%s' "$sum_cmd")" >"$work/sum-shasum/shasum"
if ! command -v shasum >/dev/null 2>&1; then
	# No shasum here: the stub takes shasum's -a 256 and runs sha256sum.
	# shellcheck disable=SC2016 # the stub's own $1 and $@
	printf '#!/bin/sh\n[ "$1" = -a ] && shift 2\nexec %s "$@"\n' "$sum_cmd" >"$work/sum-shasum/shasum"
fi

# install, mv and mkdir: refuse an absolute path outside $work; log their
# arguments to $FAKE_LOG/<tool>.log. FAKE_INSTALL_CORRUPT=1 makes install
# append a byte to what it wrote.
for t in install mv mkdir; do
	cat >"$stubs/$t" <<EOF
#!/bin/sh
for a in "\$@"; do
	case \$a in
	"$work"/*) ;;
	/*) printf '%s: refused, outside the test directory: %s\n' "$t" "\$a" >&2; exit 99 ;;
	esac
done
printf '%s\n' "\$*" >>"\$FAKE_LOG/$t.log"
if [ "$t" = install ] && [ "\${FAKE_INSTALL_CORRUPT:-0}" = 1 ]; then
	$(real "$t") "\$@" || exit
	for last in "\$@"; do :; done
	printf 'x' >>"\$last"
	exit
fi
exec $(real "$t") "\$@"
EOF
done
# sudo and doas, only for command -v: each in its own directory, put on PATH
# by run_install's TOOLS.
for t in sudo doas; do
	mkdir -p "$work/$t"
	printf '#!/bin/sh\nexit 99\n' >"$work/$t/$t"
	chmod 0755 "$work/$t/$t"
done
chmod 0755 "$stubs"/* "$work/gh/gh" "$work/sum-sha256sum/sha256sum" "$work/sum-shasum/shasum"

# The fake releases.
rel=$work/rel
assets="hardn-linux-amd64 hardn-linux-arm64 hardn-darwin-amd64"
make_release() { # <tag>: the three binaries and a good SHA256SUMS
	mkdir -p "$rel/$1"
	: >"$rel/$1/SHA256SUMS"
	for a in $assets; do
		printf 'fake %s %s\n' "$a" "$1" >"$rel/$1/$a"
		printf '%s  %s\n' "$(hash "$rel/$1/$a")" "$a" >>"$rel/$1/SHA256SUMS"
	done
	# Each binary's attestation bundle, from v0.7.0, whose name begins with
	# the binary's: SHA256SUMS in release.yml's order.
	for a in $assets; do
		printf '{}\n' >"$rel/$1/$a.sigstore.json"
		printf '%s  %s\n' "$(hash "$rel/$1/$a.sigstore.json")" "$a.sigstore.json" >>"$rel/$1/SHA256SUMS"
	done
	printf '{}\n' >"$rel/$1/hardn-sbom.spdx.json"
	printf '%s  %s\n' "$(hash "$rel/$1/hardn-sbom.spdx.json")" hardn-sbom.spdx.json >>"$rel/$1/SHA256SUMS"
}
sums_for() { # <tag> <asset>: the asset's line
	grep " $2\$" "$rel/$1/SHA256SUMS"
}

make_release v0.4.0
# The API's JSON, pretty and compact, with download URLs a substring match
# would confuse.
cat >"$rel/latest.json" <<'EOF'
{
  "url": "https://api.github.com/repos/abbott/hardn/releases/1",
  "tag_name": "v0.4.0",
  "name": "v0.4.0",
  "assets": [
    {
      "name": "hardn-linux-amd64.intoto.jsonl",
      "browser_download_url": "https://github.com/abbott/hardn/releases/download/v0.4.0/hardn-linux-amd64.intoto.jsonl"
    },
    {
      "name": "hardn-linux-amd64-debug",
      "browser_download_url": "https://github.com/abbott/hardn/releases/download/v0.4.0/hardn-linux-amd64-debug"
    }
  ]
}
EOF
printf '{"url":"x","tag_name":"v0.4.0","assets":[{"name":"hardn-linux-amd64"}],"author":{"login":"a"}}' >"$rel/compact.json"
printf '{"url":"x","name":"v0.4.0"}\n' >"$rel/notag.json"
printf '{"tag_name":"v0.4.0-rc.1"}\n' >"$rel/rc.json"
printf '{"tag_name":"v0.3.2"}\n' >"$rel/old.json"
# For the later release's line: a later tag, two that only numbers order,
# and tags that are not v<major>.<minor>.<patch>.
make_release v0.9.0
make_release v0.10.0
printf '{"tag_name":"v0.4.1"}\n' >"$rel/newer.json"
printf '{"tag_name":"v0.9.0"}\n' >"$rel/nine.json"
printf '{"tag_name":"v0.10.0"}\n' >"$rel/ten.json"
printf '{"tag_name":"v0.4.1","x":{"tag_name":"v0.4.2"}}\n' >"$rel/two.json"
printf '{"tag_name":"v0.4.1; rm -rf x"}\n' >"$rel/shell.json"
printf '{"tag_name":"0.4.1"}\n' >"$rel/nov.json"
printf '{"tag_name":"v0.4.1234567"}\n' >"$rel/long.json"
printf '{"tag_name":"v1.0"}\n' >"$rel/short.json"
printf 'rate limit exceeded\n' >"$rel/text.json"

# v0.3.2: binaries and .intoto.jsonl, no SHA256SUMS.
mkdir -p "$rel/v0.3.2"
for a in $assets; do
	printf 'old %s\n' "$a" >"$rel/v0.3.2/$a"
	printf '{}\n' >"$rel/v0.3.2/$a.intoto.jsonl"
done

# Broken releases, each from a good one.
make_release v9.0.1 # tampered binary
printf 'tampered\n' >>"$rel/v9.0.1/hardn-linux-amd64"
make_release v9.0.2 # no line for the asset
grep -v ' hardn-linux-amd64$' "$rel/v9.0.2/SHA256SUMS" >"$work/s" && cp "$work/s" "$rel/v9.0.2/SHA256SUMS"
make_release v9.0.3 # two lines
# Read the line first: GNU grep refuses to append to the file it reads.
line=$(sums_for v9.0.3 hardn-linux-amd64)
printf '%s\n' "$line" >>"$rel/v9.0.3/SHA256SUMS"
make_release v9.0.4 # 63 hex digits
line=$(sums_for v9.0.4 hardn-linux-amd64)
grep -v ' hardn-linux-amd64$' "$rel/v9.0.4/SHA256SUMS" >"$work/s"
printf '%s  hardn-linux-amd64\n' "$(printf '%s' "$line" | cut -c2-64)" >>"$work/s"
cp "$work/s" "$rel/v9.0.4/SHA256SUMS"
make_release v9.0.5 # 64 characters, not hex
grep -v ' hardn-linux-amd64$' "$rel/v9.0.5/SHA256SUMS" >"$work/s"
printf '%s  hardn-linux-amd64\n' "zzzzzzzz$(printf '%s' "$line" | cut -c9-64)" >>"$work/s"
cp "$work/s" "$rel/v9.0.5/SHA256SUMS"
make_release v9.0.6 # the asset is missing: 404
rm "$rel/v9.0.6/hardn-linux-amd64"
make_release v9.0.7 # a line for a longer name only
grep -v ' hardn-linux-amd64$' "$rel/v9.0.7/SHA256SUMS" >"$work/s"
printf '%s  hardn-linux-amd64.bak\n' "$(hash "$rel/v9.0.7/hardn-linux-amd64")" >>"$work/s"
cp "$work/s" "$rel/v9.0.7/SHA256SUMS"

# Running a case.
failures=0
cases=0
failed_case=
fail() {
	if [ "$failed_case" != "$shname $cases" ]; then
		failures=$((failures + 1))
		failed_case="$shname $cases"
	fi
	printf 'FAIL [%s] %s: %s\n' "$shname" "$name" "$*"
	printf '  stdout: %s\n' "$(cat "$cdir/out")"
	printf '  stderr: %s\n' "$(cat "$cdir/err")"
}

# new_case <name>: a fresh root with /etc and /usr/local/bin.
new_case() {
	name=$1
	cases=$((cases + 1))
	cdir=$work/case
	rm -rf "$cdir"
	root=$cdir/root
	mkdir -p "$root/etc" "$root/usr/local/bin" "$cdir/tmp" "$cdir/log"
	: >"$cdir/out"
	: >"$cdir/err"
}

# run_install [VAR=value ...]: install.sh under $sh, defaults Linux x86_64,
# root, sha256sum, no gh. SUM=shasum puts shasum on PATH in place of
# sha256sum; GH=1 puts gh on PATH. TOOLS (default sudo) are the ones of
# sudo and doas on PATH, in order; TOOLS= puts neither.
run_install() {
	snapshot_before=$(cd "$root" && find . | sort)
	sum=sha256sum
	gh=
	tools=sudo
	for a in "$@"; do
		case $a in
		SUM=*) sum=${a#SUM=} ;;
		GH=*) gh=${a#GH=} ;;
		TOOLS=*) tools=${a#TOOLS=} ;;
		esac
	done
	path=$stubs:$work/sum-$sum:$work/tools
	for t in $tools; do
		path=$path:$work/$t
	done
	[ -z "$gh" ] || path=$work/gh:$path
	status=0
	# shellcheck disable=SC2086 # $sh is "busybox sh" for busybox
	env -i PATH="$path" HOME="$cdir" TMPDIR="$cdir/tmp" \
		HARDN_INSTALL_ROOT="$root" FAKE_REL="$rel" FAKE_LOG="$cdir/log" \
		FAKE_OS=Linux FAKE_ARCH=x86_64 FAKE_UID=0 "$@" \
		$sh "$script" >"$cdir/out" 2>"$cdir/err" || status=$?
}

# run_verify <version> [VAR=value ...]: verify.sh <version> linux-amd64 under
# $sh in the case's own directory, with gh on PATH.
run_verify() {
	ver=$1
	shift
	mkdir -p "$cdir/pwd"
	status=0
	# shellcheck disable=SC2086 # $sh is "busybox sh" for busybox
	(cd "$cdir/pwd" && env -i PATH="$work/gh:$stubs:$work/sum-sha256sum:$work/tools" HOME="$cdir" TMPDIR="$cdir/tmp" \
		FAKE_REL="$rel" FAKE_LOG="$cdir/log" FAKE_CURL_LAX=1 "$@" \
		$sh "$verify" "$ver" linux-amd64 >"$cdir/out" 2>"$cdir/err") || status=$?
}

# mode <path>: the mode as ls -l prints it (find cannot print it portably).
# shellcheck disable=SC2012
mode() { ls -ld "$1" | cut -c1-10; }

expect_status() { [ "$status" = "$1" ] || fail "exit $status, want $1"; }
expect_out() { grep -qF -- "$1" "$cdir/out" || fail "stdout lacks: $1"; }
expect_lacks() { ! grep -qF -- "$1" "$cdir/out" || fail "stdout has: $1"; }
# Provenance not checked: one line that points to docs/verify.md, with a
# blank line on each side, and no mark, Warning or command.
see="See docs/verify.md to check provenance"
expect_quiet() {
	[ "$(grep -c -i -e provenance -e 'verify' "$cdir/out")" = 1 ] || fail "more than one line on provenance"
	awk -v s="$see" '$0 == s { found = 1; ok = prev == ""; getline; ok = ok && $0 == "" } { prev = $0 } END { exit !(found && ok) }' "$cdir/out" ||
		fail "no line between blank lines: $see"
	for gone in "not checked" "[ ]" Warning "To check it" "gh " "verify.sh" "--cert-identity"; do
		expect_lacks "$gone"
	done
}
expect_err() { grep -qF -- "$1" "$cdir/err" || fail "stderr lacks: $1"; }
# The layout: every stdout line within 80 columns but the identity URL, a
# blank line before the line that starts with $1, and one at the end.
expect_fits() {
	w=$(awk -v id="$identity" 'length > 80 && index($0, id) == 0' "$cdir/out")
	[ -z "$w" ] || fail "wider than 80 columns: $w"
}
expect_blank_before() {
	awk -v p="$1" 'index($0, p) == 1 { found = 1; ok = prev == ""; exit } { prev = $0 } END { exit !(found && ok) }' "$cdir/out" ||
		fail "no blank line before: $1"
}
expect_blank_end() { awk 'END { exit !(NR && $0 == "") }' "${1:-$cdir/out}" || fail "no blank line at the end of ${1:-stdout}"; }
# stdout starts with a blank line; $1, stderr, starts with one or not (yes/no).
expect_blank_start() { awk 'NR == 1 { exit !($0 == "") } END { if (NR == 0) exit 1 }' "$cdir/out" || fail "stdout doesn't start with a blank line"; }
expect_err_starts_blank() {
	first=$(awk 'NR == 1 { print ($0 == "" ? "yes" : "no"); exit }' "$cdir/err")
	[ "$first" = "$1" ] || fail "stderr starts with a blank line: $first, want $1"
}
# stdout from the line that starts with $1 to its end is $2.
expect_ends() {
	got=$(awk -v p="$1" 'index($0, p) == 1 { on = 1 } on' "$cdir/out")
	[ "$got" = "$2" ] || fail "stdout doesn't end: $2"
}
expect_no_download() { [ ! -s "$cdir/log/curl.log" ] || fail "curl ran: $(cat "$cdir/log/curl.log")"; }
expect_downloads() { # the URLs, one per line, in order
	[ "$(cat "$cdir/log/curl.log" 2>/dev/null)" = "$1" ] ||
		fail "downloads: $(cat "$cdir/log/curl.log" 2>/dev/null), want $1"
}
# Nothing written in the root, and the temporary directory removed.
expect_nothing_installed() {
	expect_status 1
	expect_err "nothing was installed"
	[ "$(cd "$root" && find . | sort)" = "$snapshot_before" ] ||
		fail "the root changed: $(cd "$root" && find . | sort | tr '\n' ' ')"
	expect_tmp_clean
}
expect_tmp_clean() {
	[ -z "$(ls -A "$cdir/tmp")" ] || fail "temporary files left: $(ls -A "$cdir/tmp")"
}
# <asset> [<tag>]: the asset installed with mode 0755, /etc/hardn 0755,
# nothing else.
expect_installed() {
	expect_status 0
	cmp -s "$root/usr/local/bin/hardn" "$rel/${2:-v0.4.0}/$1" || fail "/usr/local/bin/hardn is not $1"
	m=$(mode "$root/usr/local/bin/hardn")
	[ "$m" = -rwxr-xr-x ] || fail "/usr/local/bin/hardn's mode is $m"
	m=$(mode "$root/etc/hardn")
	[ "$m" = drwxr-xr-x ] || fail "/etc/hardn's mode is $m"
	got=$(cd "$root" && find . | sort | tr '\n' ' ')
	want='. ./etc ./etc/hardn ./usr ./usr/local ./usr/local/bin ./usr/local/bin/hardn '
	[ "$got" = "$want" ] || fail "files: $got"
	expect_tmp_clean
	expect_atomic
}
# install wrote a temporary name beside hardn, and mv moved it onto hardn.
expect_atomic() {
	b=$root/usr/local/bin
	staged=$(sed -n "s|^-m 0755 $cdir/tmp/hardn-install\.[^/ ]*/[^ ]* \($b/\.hardn\.install\.[0-9][0-9]*\)\$|\1|p" "$cdir/log/install.log" 2>/dev/null)
	[ -n "$staged" ] && [ "$(wc -l <"$cdir/log/install.log" | tr -d ' ')" = 1 ] ||
		fail "install's arguments: $(cat "$cdir/log/install.log" 2>/dev/null), want one to $b/.hardn.install.<pid>"
	[ "$(cat "$cdir/log/mv.log" 2>/dev/null)" = "-f $staged $b/hardn" ] ||
		fail "mv's arguments: $(cat "$cdir/log/mv.log" 2>/dev/null), want -f $staged $b/hardn"
}

api=https://api.github.com/repos/abbott/hardn/releases/latest
dl=https://github.com/abbott/hardn/releases/download

for sh in $shells; do
	[ "$sh" = busybox ] && sh="$bb sh"
	shname=$sh

	# 1. The platform.
	for p in Linux/x86_64/hardn-linux-amd64 Linux/aarch64/hardn-linux-arm64 \
		Linux/arm64/hardn-linux-arm64 Darwin/x86_64/hardn-darwin-amd64 \
		Darwin/arm64/hardn-darwin-amd64; do
		os=${p%%/*}
		rest=${p#*/}
		arch=${rest%/*}
		asset=${rest#*/}
		new_case "platform $os $arch"
		run_install FAKE_OS="$os" FAKE_ARCH="$arch"
		expect_installed "$asset"
		expect_out "Installing hardn v0.4.0: $asset ($os $arch)"
		expect_downloads "$api
$dl/v0.4.0/SHA256SUMS
$dl/v0.4.0/$asset"
		if [ "$os/$arch" = Darwin/arm64 ]; then
			expect_out "Rosetta 2"
		elif grep -q Rosetta "$cdir/out"; then
			fail "mentions Rosetta"
		fi
	done
	for arch in armv7l armv6l; do
		new_case "platform Linux $arch"
		run_install FAKE_ARCH="$arch"
		expect_nothing_installed
		expect_err "Linux $arch is not supported: there is no armv7 build"
		expect_no_download
	done
	for p in Linux/i686 Linux/riscv64 FreeBSD/amd64 Darwin/i386; do
		new_case "platform $p"
		run_install FAKE_OS="${p%/*}" FAKE_ARCH="${p#*/}"
		expect_nothing_installed
		expect_err "${p%/*} ${p#*/} is not supported"
		expect_no_download
	done

	# 2. Root.
	new_case "not root"
	run_install FAKE_UID=1000
	expect_nothing_installed
	expect_blank_start
	expect_err_starts_blank no
	expect_blank_end "$cdir/err"
	expect_err "run it as root"
	expect_err "curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh"
	! grep -qF doas "$cdir/err" || fail "stderr names doas with sudo on PATH"
	expect_no_download
	new_case "not root, doas alone"
	run_install FAKE_UID=1000 TOOLS=doas
	expect_nothing_installed
	expect_err "install.sh | doas sh"
	! grep -qF sudo "$cdir/err" || fail "stderr names sudo with doas alone"
	new_case "not root, sudo and doas"
	run_install FAKE_UID=1000 TOOLS="doas sudo"
	expect_err "install.sh | sudo sh"
	new_case "not root, neither"
	run_install FAKE_UID=1000 TOOLS=
	expect_nothing_installed
	expect_err "install.sh | sudo sh"
	expect_err "(or doas in place of sudo)"
	new_case "not root, doas alone, HARDN_VERSION"
	run_install FAKE_UID=1000 TOOLS=doas HARDN_VERSION=0.4.0
	expect_err "| doas env HARDN_VERSION=0.4.0 sh"
	new_case "not root, HARDN_VERSION"
	run_install FAKE_UID=1000 HARDN_VERSION=0.4.0
	expect_nothing_installed
	expect_err "| sudo env HARDN_VERSION=0.4.0 sh"
	expect_no_download

	# 3. The release.
	new_case "latest, compact JSON"
	run_install FAKE_LATEST=compact
	expect_installed hardn-linux-amd64
	new_case "latest without tag_name"
	run_install FAKE_LATEST=notag
	expect_nothing_installed
	expect_err "no tag_name"
	new_case "latest tag not x.y.z"
	run_install FAKE_LATEST=rc
	expect_nothing_installed
	expect_err "'v0.4.0-rc.1' is not v<major>.<minor>.<patch>"
	new_case "API unreachable"
	run_install FAKE_LATEST=missing
	expect_nothing_installed
	expect_err "cannot read the latest release"
	expect_downloads "$api"
	for v in 0.4.0 v0.4.0; do
		new_case "HARDN_VERSION=$v"
		run_install HARDN_VERSION="$v"
		expect_installed hardn-linux-amd64
		expect_downloads "$dl/v0.4.0/SHA256SUMS
$dl/v0.4.0/hardn-linux-amd64
$api max-time=5"
	done
	for v in 0.4 0.4.0.1 1..2 0.4.0-rc.1 '0.4.0/../x'; do
		new_case "HARDN_VERSION=$v"
		run_install HARDN_VERSION="$v"
		expect_nothing_installed
		expect_err "is not <major>.<minor>.<patch>"
		expect_no_download
	done

	# 4. SHA256SUMS and the SHA-256.
	new_case "shasum"
	run_install SUM=shasum
	expect_installed hardn-linux-amd64
	new_case "tampered binary"
	run_install HARDN_VERSION=9.0.1
	expect_nothing_installed
	expect_err "hardn-linux-amd64's SHA-256 is"
	expect_err_starts_blank yes
	expect_blank_end "$cdir/err"
	new_case "no SHA256SUMS line"
	run_install HARDN_VERSION=9.0.2
	expect_nothing_installed
	expect_err "SHA256SUMS has 0 lines for hardn-linux-amd64, not 1"
	new_case "two SHA256SUMS lines"
	run_install HARDN_VERSION=9.0.3
	expect_nothing_installed
	expect_err "SHA256SUMS has 2 lines for hardn-linux-amd64, not 1"
	new_case "63-digit hash"
	run_install HARDN_VERSION=9.0.4
	expect_nothing_installed
	expect_err "is not a SHA-256"
	new_case "non-hex hash"
	run_install HARDN_VERSION=9.0.5
	expect_nothing_installed
	expect_err "is not a SHA-256"
	new_case "line for a longer name only"
	run_install HARDN_VERSION=9.0.7
	expect_nothing_installed
	expect_err "SHA256SUMS has 0 lines"
	new_case "asset 404"
	run_install HARDN_VERSION=9.0.6
	expect_nothing_installed
	expect_err "cannot download $dl/v9.0.6/hardn-linux-amd64"
	new_case "v0.3.2, pinned"
	run_install HARDN_VERSION=0.3.2
	expect_nothing_installed
	expect_err "cannot download $dl/v0.3.2/SHA256SUMS (curl's HTTP error is above). A 404 means there is no release v0.3.2"
	expect_err "v0.3.2 and earlier have no SHA256SUMS"
	expect_downloads "$dl/v0.3.2/SHA256SUMS"
	new_case "v0.3.2, latest"
	run_install FAKE_LATEST=old
	expect_nothing_installed
	expect_err "no SHA256SUMS"

	# 5. The attestation.
	identity=https://github.com/abbott/hardn/.github/workflows/build.yml@refs/tags/v0.4.0
	new_case "gh absent"
	run_install
	expect_installed hardn-linux-amd64
	expect_quiet
	expect_out "Downloading hardn-linux-amd64 ..."
	expect_blank_start
	expect_fits
	expect_out "  [+] Downloaded hardn-linux-amd64"
	expect_out "  [+] SHA-256 matches SHA256SUMS"
	expect_out "  [+] Created /etc/hardn"
	for part in "   -  Downloading" "$see" "  [+] Created" "hardn writes" "# Installed hardn"; do
		expect_blank_before "$part"
	done
	# Not a terminal: no colour, and ASCII marks.
	if LC_ALL=C grep -q '[^ -~]' "$cdir/out"; then fail "stdout has an escape or a non-ASCII byte"; fi
	expect_out "# Installed hardn v0.4.0 as /usr/local/bin/hardn"
	expect_blank_before "Run hardn --help or sudo hardn"
	[ "$(awk 'NF { l = $0 } END { print l }' "$cdir/out")" = "Run hardn --help or sudo hardn" ] ||
		fail "the last line doesn't say what to run"
	expect_blank_end
	for c in "doas:DOAS_USER=bob TOOLS=sudo:doas hardn" "sudo:SUDO_USER=bob TOOLS=doas:sudo hardn" "doas on PATH:TOOLS=doas:doas hardn" \
		"both on PATH:TOOLS=sudo doas:sudo hardn" "neither:TOOLS=:hardn as root"; do
		new_case "the last line, ${c%%:*}"
		vars=${c#*:}
		vars=${vars%%:*}
		case $vars in
		TOOLS=*) run_install "$vars" ;;
		*) run_install "${vars%% *}" "${vars#* }" ;;
		esac
		last=$(awk 'NF { l = $0 } END { print l }' "$cdir/out")
		case ${c##*:} in
		"hardn as root") [ "$last" = "Run hardn --help, or hardn as root" ] || fail "last line: $last" ;;
		*) [ "$last" = "Run hardn --help or ${c##*:}" ] || fail "last line: $last" ;;
		esac
	done
	new_case "gh verifies"
	run_install GH=1
	expect_installed hardn-linux-amd64
	expect_out "  [+] Provenance verified: built by"
	expect_lacks "$see"
	expect_out "      $identity"
	expect_fits
	expect_blank_before "   -  Verifying the provenance"
	expect_blank_end
	grep -q "^attestation verify $cdir/tmp/hardn-install\.[^/]*/hardn-linux-amd64 --repo abbott/hardn --cert-identity $identity --source-ref refs/tags/v0.4.0 --deny-self-hosted-runners\$" "$cdir/log/gh.log" ||
		fail "gh's arguments: $(cat "$cdir/log/gh.log")"
	new_case "gh fails"
	run_install GH=1 FAKE_GH_VERIFY=1
	expect_nothing_installed
	expect_err "gh attestation verify failed for hardn-linux-amd64 at v0.4.0"
	new_case "gh signed out"
	run_install GH=1 FAKE_GH_AUTH=0
	expect_installed hardn-linux-amd64
	expect_quiet
	! grep -q "^attestation verify /" "$cdir/log/gh.log" || fail "gh verified while signed out"
	new_case "gh too old"
	run_install GH=1 FAKE_GH_HELP=old
	expect_installed hardn-linux-amd64
	expect_quiet
	! grep -q "^attestation verify /" "$cdir/log/gh.log" || fail "gh verified without --source-ref"

	# 6. The install.
	new_case "update over an existing hardn"
	printf 'old\n' >"$root/usr/local/bin/hardn"
	chmod 0700 "$root/usr/local/bin/hardn"
	mkdir -m 0755 "$root/etc/hardn"
	run_install
	expect_installed hardn-linux-amd64
	if grep -q "Created /etc/hardn" "$cdir/out"; then fail "says it created /etc/hardn"; fi
	new_case "existing /etc/hardn keeps its mode"
	mkdir -m 0700 "$root/etc/hardn"
	run_install
	expect_status 0
	m=$(mode "$root/etc/hardn")
	[ "$m" = drwx------ ] || fail "/etc/hardn's mode changed to $m"
	new_case "no /usr/local/bin"
	rmdir "$root/usr/local/bin"
	run_install
	expect_installed hardn-linux-amd64
	new_case "/usr/local/bin/hardn is a directory"
	mkdir "$root/usr/local/bin/hardn"
	run_install
	expect_nothing_installed
	expect_err "$root/usr/local/bin/hardn is a directory (or a link to one)"
	[ ! -e "$cdir/log/install.log" ] && [ ! -e "$cdir/log/mv.log" ] || fail "install or mv ran"
	new_case "/usr/local/bin/hardn is a link to a directory"
	mkdir "$cdir/elsewhere"
	ln -s "$cdir/elsewhere" "$root/usr/local/bin/hardn"
	run_install
	expect_nothing_installed
	expect_err "$root/usr/local/bin/hardn is a directory (or a link to one)"
	[ ! -e "$cdir/log/install.log" ] && [ ! -e "$cdir/log/mv.log" ] || fail "install or mv ran"
	[ -z "$(ls -A "$cdir/elsewhere")" ] || fail "written through the link: $(ls -A "$cdir/elsewhere")"
	new_case "staged copy differs from the checked binary"
	run_install FAKE_INSTALL_CORRUPT=1
	expect_nothing_installed
	expect_err "SHA-256 is"
	expect_err "not the $(hash "$rel/v0.4.0/hardn-linux-amd64") checked: refused"
	[ ! -e "$cdir/log/mv.log" ] || fail "mv ran: $(cat "$cdir/log/mv.log")"
	new_case "HARDN_INSTALL_ROOT relative"
	run_install HARDN_INSTALL_ROOT=relative
	expect_status 1
	expect_err "HARDN_INSTALL_ROOT must be an absolute path"
	expect_no_download

	# 7. The later release's line, with HARDN_VERSION alone: one request to
	# the API, last, 5 seconds at most; said only for a later tag; never a
	# failure.
	said="hardn v0.4.1 available (https://github.com/abbott/hardn)"
	done_040="# Installed hardn v0.4.0 as /usr/local/bin/hardn"
	run_line="Run hardn --help or sudo hardn"
	new_case "notice: a later release"
	run_install HARDN_VERSION=0.4.0 FAKE_LATEST=newer
	expect_installed hardn-linux-amd64
	expect_ends "# Installed" "$done_040

$said

$run_line"
	expect_blank_end
	expect_fits
	expect_downloads "$dl/v0.4.0/SHA256SUMS
$dl/v0.4.0/hardn-linux-amd64
$api max-time=5"
	new_case "notice: later by number, not by letter"
	run_install HARDN_VERSION=0.9.0 FAKE_LATEST=ten
	expect_installed hardn-linux-amd64 v0.9.0
	expect_out "hardn v0.10.0 available (https://github.com/abbott/hardn)"
	new_case "notice: earlier by number, not by letter"
	run_install HARDN_VERSION=0.10.0 FAKE_LATEST=nine
	expect_installed hardn-linux-amd64 v0.10.0
	expect_ends "# Installed" "# Installed hardn v0.10.0 as /usr/local/bin/hardn

$run_line"
	# Nothing said, and the install stands: the same release, an earlier
	# one, a tag that is no version, an answer that is no JSON, no answer
	# (curl's 22), a timeout (curl's 28).
	for quiet in FAKE_LATEST=latest FAKE_LATEST=old FAKE_LATEST=rc FAKE_LATEST=notag \
		FAKE_LATEST=two FAKE_LATEST=shell FAKE_LATEST=nov FAKE_LATEST=long \
		FAKE_LATEST=short FAKE_LATEST=text FAKE_LATEST=missing FAKE_API_STATUS=28; do
		new_case "notice: none, $quiet"
		run_install HARDN_VERSION=0.4.0 "$quiet"
		expect_installed hardn-linux-amd64
		expect_ends "# Installed" "$done_040

$run_line"
		[ ! -s "$cdir/err" ] || fail "stderr is not empty"
		expect_downloads "$dl/v0.4.0/SHA256SUMS
$dl/v0.4.0/hardn-linux-amd64
$api max-time=5"
	done
	# An interrupt during the request: hardn is installed, so status 0.
	for sig in INT TERM HUP; do
		new_case "notice: $sig during the request"
		run_install HARDN_VERSION=0.4.0 FAKE_LATEST=newer "FAKE_API_SIGNAL=$sig"
		expect_installed hardn-linux-amd64
		expect_out "$done_040"
	done
	new_case "notice: no request without HARDN_VERSION"
	run_install FAKE_LATEST=compact
	expect_installed hardn-linux-amd64
	expect_ends "# Installed" "$done_040

$run_line"
	expect_downloads "$api
$dl/v0.4.0/SHA256SUMS
$dl/v0.4.0/hardn-linux-amd64"
	new_case "notice: no request after a refusal"
	run_install HARDN_VERSION=9.0.1 FAKE_LATEST=newer
	expect_nothing_installed
	expect_downloads "$dl/v9.0.1/SHA256SUMS
$dl/v9.0.1/hardn-linux-amd64"

	# verify.sh's: the same line after its last, by the same rules.
	verified="# Verified ./hardn-linux-amd64 (hardn v0.4.0)"
	new_case "verify.sh: a later release"
	run_verify 0.4.0 FAKE_LATEST=newer
	expect_status 0
	expect_ends "# Verified" "$verified

$said"
	cmp -s "$cdir/pwd/hardn-linux-amd64" "$rel/v0.4.0/hardn-linux-amd64" || fail "./hardn-linux-amd64 is not the binary"
	expect_tmp_clean
	expect_downloads "$dl/v0.4.0/hardn-linux-amd64
$dl/v0.4.0/SHA256SUMS
$api max-time=5"
	new_case "verify.sh: later by number, not by letter"
	run_verify v0.9.0 FAKE_LATEST=ten
	expect_status 0
	expect_out "hardn v0.10.0 available (https://github.com/abbott/hardn)"
	for quiet in FAKE_LATEST=latest FAKE_LATEST=old FAKE_LATEST=rc FAKE_LATEST=notag \
		FAKE_LATEST=two FAKE_LATEST=shell FAKE_LATEST=nov FAKE_LATEST=long \
		FAKE_LATEST=short FAKE_LATEST=text FAKE_LATEST=missing FAKE_API_STATUS=28; do
		new_case "verify.sh: none, $quiet"
		run_verify 0.4.0 "$quiet"
		expect_status 0
		expect_ends "# Verified" "$verified"
		cmp -s "$cdir/pwd/hardn-linux-amd64" "$rel/v0.4.0/hardn-linux-amd64" || fail "./hardn-linux-amd64 is not the binary"
		expect_tmp_clean
		expect_downloads "$dl/v0.4.0/hardn-linux-amd64
$dl/v0.4.0/SHA256SUMS
$api max-time=5"
	done
	for sig in INT TERM HUP; do
		new_case "verify.sh: $sig during the request"
		run_verify 0.4.0 FAKE_LATEST=newer "FAKE_API_SIGNAL=$sig"
		expect_status 0
		expect_out "$verified"
		cmp -s "$cdir/pwd/hardn-linux-amd64" "$rel/v0.4.0/hardn-linux-amd64" || fail "./hardn-linux-amd64 is not the binary"
		expect_tmp_clean
	done
	new_case "verify.sh: no request after a refusal"
	run_verify 9.0.1 FAKE_LATEST=newer
	expect_status 1
	expect_err "hardn-linux-amd64's SHA-256 is"
	expect_downloads "$dl/v9.0.1/hardn-linux-amd64
$dl/v9.0.1/SHA256SUMS"
done

[ -z "$skipped" ] || printf 'install_test.sh: skipped %s\n' "$skipped"
if [ "$failures" -gt 0 ]; then
	printf 'install_test.sh: %d of %d cases failed (shells:%s)\n' "$failures" "$cases" "$shells" >&2
	exit 1
fi
printf 'install_test.sh: %d cases passed (shells:%s)\n' "$cases" "$shells"
