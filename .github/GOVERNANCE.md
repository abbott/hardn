# Project Governance

## Overview

This document sets out `hardn`'s [mission](#mission), and the governance structure and processes that guide its development and maintenance.

## Project Maintainer

The maintainer makes the project's decisions, and is responsible for:

- Determining the project roadmap and priorities
- Reviewing and accepting contributions
- Making all technical decisions
- Managing releases and versioning
- Maintaining project infrastructure
- Addressing security issues
- Providing support as time allows

## Decision Making Process

Decisions are made in this order:

1. The maintainer evaluates proposed changes against the project's goals, security requirements, and quality standards
2. The maintainer implements accepted changes according to priority and available time
3. For significant changes that affect the user experience, the maintainer may seek community feedback through GitHub issues

## Contributing

Contributions from the community are welcome. All contributions are subject to review and approval by the maintainer. Contributors should note:

1. The maintainer has final say on whether contributions are accepted
2. Response times may vary based on the maintainer's availability
3. Contributions must align with the project's design philosophy and security standards
4. PRs may be modified by the maintainer before merging to ensure consistency and quality

## Mission

**Simple, precise hardening for Linux servers.** hardn shows each change before it happens, and reverts it if needed.

hardn lowers the barrier to securing a Linux system: fundamental security hygiene, from one portable command-line program with a menu.

It is for anyone who administers Linux servers without a security team: homelab enthusiasts, students and people starting out in systems administration, and small organisations that provide public services, such as schools, clinics and nonprofits.

### What hardn commits to

- **Reversible**: every change is planned, backed up, and rolled back when a step fails.
- **Local**: no telemetry, no account, no cloud service.
- **Verifiable**: every release has SLSA Build Level 3 provenance and a Sigstore signature ([docs/verify.md](../docs/verify.md)).
- **Current**: hardn changes only the distribution versions in the README's support table, and the latest minor release gets security fixes ([SECURITY.md](SECURITY.md)).
- **Responsive**: a security report is acknowledged within 48 hours ([SECURITY.md](SECURITY.md)).

### What hardn is not

- **Not a compliance scanner**: it certifies nothing.
- **Not a fleet manager**: it works on one host at a time.
- **Not a vulnerability scanner**: it checks only for a CVE that makes one of its own changes unsafe ([docs/cve.md](../docs/cve.md)).
- **Not a substitute** for monitoring, updates and audits.

## Release Process

Releases follow this general process:

1. The maintainer decides when to create a new release based on accumulated changes
2. Releases follow semantic versioning (`MAJOR.MINOR.PATCH`)
3. All releases undergo SLSA Level 3 compliant builds and Sigstore signing
4. Security issues may prompt accelerated releases

## Future Governance

Governance will grow with the project. When contributors join as maintainers, this document will be updated to reflect the new structure.

## Code of Conduct

Everyone taking part in the project's spaces follows the [Code of Conduct](CODE_OF_CONDUCT.md). The maintainer moderates discussions and removes content that breaks it.

## Changes to Governance

This governance document may change as the project evolves. Significant changes will be announced through GitHub releases and commit messages.

---

Last updated: November 2026
