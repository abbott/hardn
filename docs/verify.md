# Verifying a release

Each `hardn` binary has [SLSA](https://slsa.dev) Build Level 3 provenance: a GitHub artifact attestation, signed by `build.yml`, the reusable workflow that builds it, as GitHub's guide to Level 3 has it. It gives:

- **Tamper Protection**: Each binary is signed and includes a provenance attestation
- **Build Integrity**: Builds are performed in GitHub's trusted environment
- **Source Verification**: Binaries are traceable back to their source commit
- **Reproducible**: Rebuilding a tag with go.mod's toolchain gives byte-identical binaries ([contributing.md](contributing.md)); v0.7.0's three were rebuilt and matched

In addition to SLSA provenance, all artifacts are signed using [Sigstore](https://www.sigstore.dev/):

- **Transparency Log**: Every signature is recorded in the public Rekor log
- **Keyless Signing**: Short-lived certificate tied to GitHub's OIDC identity

The workflows that build and sign a release are audited by 🌈 [zizmor](https://docs.zizmor.sh) on every push to main and every pull request; any finding fails the check.

## Verify a Release

Verification is a separate step, and recommended. From v0.4.0, each release has `SHA256SUMS`, an SPDX SBOM (`hardn-sbom.spdx.json`), and the attestation for each binary. From v0.7.0, each attestation's bundle is a file of the release too, such as `hardn-linux-amd64.sigstore.json`.

[verify.sh](../verify.sh) downloads a binary and `SHA256SUMS`, checks the SHA-256, and then checks the attestation. It needs curl, sha256sum or shasum, and [gh](https://cli.github.com) 2.68.0 or later, signed in (`gh auth login`):

```sh
curl -fsSLO https://raw.githubusercontent.com/abbott/hardn/v0.7.1/verify.sh
sh verify.sh 0.7.1 linux-amd64      # leaves ./hardn-linux-amd64 only if both checks pass
```

To check a binary you already have, such as one installed using [install.sh](../install.sh), use `gh`:

```sh
gh attestation verify /usr/local/bin/hardn --repo abbott/hardn \
  --cert-identity https://github.com/abbott/hardn/.github/workflows/build.yml@refs/tags/v0.7.1 \
  --source-ref refs/tags/v0.7.1 --deny-self-hosted-runners
```

To check a binary on a host with no network, bring three files from a machine that has one: the binary, its bundle from the same release, and Sigstore's trusted root, from `gh attestation trusted-root > trusted_root.jsonl`. Then, with gh 2.68.0 or later, signed in or not:

```sh
gh attestation verify hardn-linux-amd64 --repo abbott/hardn \
  --bundle hardn-linux-amd64.sigstore.json --custom-trusted-root trusted_root.jsonl \
  --cert-identity https://github.com/abbott/hardn/.github/workflows/build.yml@refs/tags/v0.7.1 \
  --source-ref refs/tags/v0.7.1 --deny-self-hosted-runners \
  --format json --jq '.[].verificationResult.signature.certificate.sourceRepositoryIdentifier'
```

It prints the ID of the repository that signed the bundle, which must be `941701082`, abbott/hardn's. Offline, `--repo` is checked only against the names in the bundle, so a bundle from another repository that has had the name would pass; its ID would differ. Online, `gh api repos/abbott/hardn --jq .id` shows the ID.

> [!NOTE]
> To check the SHA-256 alone, download `SHA256SUMS` from the same release, then run `grep ' hardn-linux-amd64$' SHA256SUMS | sha256sum -c -`.

