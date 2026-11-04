# Security policy

## Supported versions

| Version | Supported |
|---|---|
| 0.4.x | yes |
| 0.3.x and older | no |

Only the latest minor version gets security fixes, in a new patch release. When 0.5.0 is out, 0.4.x is no longer supported. Update to the latest release.

## Reporting a vulnerability

**Don't open a public issue.** Report it privately, either way:

- **GitHub:** the repository's [Security tab](https://github.com/abbott/hardn/security), "Report a vulnerability".
- **Email:** `641138+abbott@users.noreply.github.com`.

Include what the problem is, the steps that show it, the hardn version (`hardn --version`) and the OS, what an attacker could do with it, and a fix if you have one.

hardn has one maintainer, so there is no bug bounty. What to expect:

1. An acknowledgment within 48 hours.
2. The report is checked and its severity assessed.
3. If it is confirmed, a fix is made and released as a patch release. Its release notes name the fix without the details.
4. The details may be published once users have had time to update.

Significant reports are credited in the release notes.

## Verifying a release

Each release from v0.4.0 has these files:

- `hardn-linux-amd64`, `hardn-linux-arm64`, `hardn-darwin-amd64`: the binaries.
- `SHA256SUMS`: the SHA-256 of each file.
- `hardn-sbom.spdx.json`: an SBOM, in SPDX JSON.

Each binary also has **SLSA Build Level 3 build provenance**: a GitHub artifact attestation, signed for the repository's build workflow (`.github/workflows/build.yml`) at the release's tag, built on a GitHub-hosted runner. It shows the binary was built by that workflow from that tag. The binaries are not signed with cosign, and there are no `.sig`, `.crt` or `.intoto.jsonl` files.

**With `verify.sh`**: it downloads the binary and `SHA256SUMS` from the release, checks the binary's SHA-256, then its attestation, and keeps the binary as `./hardn-<platform>` only when both pass. It runs nothing it downloads.

```sh
curl -fsSLO https://raw.githubusercontent.com/abbott/hardn/main/verify.sh
sh verify.sh 0.4.0 linux-amd64
```

It needs `curl`, `sha256sum` or `shasum`, and [gh](https://cli.github.com) 2.68.0 or later, signed in (`gh auth login`).

**By hand**, with the binary and `SHA256SUMS` downloaded from the release page:

```sh
sha256sum --ignore-missing -c SHA256SUMS
gh attestation verify hardn-linux-amd64 --repo abbott/hardn \
  --cert-identity https://github.com/abbott/hardn/.github/workflows/build.yml@refs/tags/v0.4.0 \
  --source-ref refs/tags/v0.4.0 --deny-self-hosted-runners
```

Releases v0.3.x and older had no `SHA256SUMS` and no GitHub artifact attestation; `verify.sh` does not check them.
