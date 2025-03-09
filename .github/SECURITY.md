# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.3.x   | YES                |
<!-- | < 0.3.0 | NO                | -->

Only the latest minor version is actively maintained for security updates. Users are encouraged to update to the latest release.

## Reporting a Vulnerability

The security of `hardn` is taken seriously. As a tool designed to enhance Linux system security, the integrity and security of `hardn` itself is paramount.

### How to Report

If you discover a security vulnerability, please send a detailed report to:

**Email**: `641138+abbott@users.noreply.github.com`

Please **DO NOT** create a public GitHub issue for security vulnerabilities.

### What to Include

When reporting a vulnerability, please include:

1. A clear description of the vulnerability
2. Steps to reproduce the issue
3. Potential impact of the vulnerability
4. Suggested fixes if available

### Response Process

Here's what you can expect after reporting:

1. **Acknowledgment**: You will receive an acknowledgment of your report within 48 hours.
2. **Assessment**: The vulnerability will be verified and assessed for severity.
3. **Fix Development**: If validated, a fix will be developed as quickly as possible.
4. **Release**: A security patch will be released, and the fix will be mentioned in release notes without detailing the vulnerability until users have had time to update.
5. **Public Disclosure**: After a reasonable period for users to update, details may be publicly disclosed.

### Bounties

As `hardn` is a solo-maintained project, there is currently no formal bug bounty program at this time. However, significant security contributions will be acknowledged in the project's README and release notes.

## Security Features and Practices

`hardn` employs several security features:

- **SLSA Level 3 Compliance**: All releases follow Supply-chain Levels for Software Artifacts (SLSA) Level 3 requirements.
- **Sigstore Artifact Signing**: All binaries are cryptographically signed and verifiable.
- **Tamper Protection**: Binaries include provenance attestation.
- **Transparency**: Build processes are fully documented in the provenance.

Users are encouraged to verify signatures and provenance of all `hardn` releases using the provided verification tools.

## Security Updates

Security updates will be announced through:

1. GitHub releases
2. Commit messages with the `security:` prefix
3. `hardn` CLI and CLI menu header when the binay is run locally

## Third-Party Dependencies

`hardn` aims to maintain minimal dependencies to reduce attack surface. All dependencies are regularly reviewed and updated.

---

Last updated: March 2025