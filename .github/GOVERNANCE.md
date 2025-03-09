# Project Governance

## Overview

`hardn` is a solo-maintained project developed to provide a simple Linux hardening tool. This document outlines the governance structure and processes that guide the project's development and maintenance.

## Project Maintainer

The project is maintained by a single developer who has complete decision-making authority over all aspects of the project. The maintainer is responsible for:

- Determining the project roadmap and priorities
- Reviewing and accepting contributions
- Making all technical decisions
- Managing releases and versioning
- Maintaining project infrastructure
- Addressing security issues
- Providing support as time allows

## Decision Making Process

As a solo-maintained project, decision making is straightforward:

1. The maintainer evaluates proposed changes against the project's goals, security requirements, and quality standards
2. The maintainer implements accepted changes according to priority and available time
3. For significant changes that affect the user experience, the maintainer may seek community feedback through GitHub issues

## Contributing

Although `hardn` is solo-maintained, contributions from the community are welcome. All contributions are subject to review and approval by the maintainer. Contributors should note:

1. The maintainer has final say on whether contributions are accepted
2. Response times may vary based on the maintainer's availability
3. Contributions must align with the project's design philosophy and security standards
4. PRs may be modified by the maintainer before merging to ensure consistency and quality

## Project Goals and Values

`hardn` aims to provide:

1. **Simplicity**: Easy-to-use hardening for Linux systems
2. **Security**: Focus on implementing best practices for system security
3. **Transparency**: Clear documentation of what changes are made to systems
4. **Supply Chain Security**: Maintain SLSA Level 3 compliance and artifact signing

## Release Process

Releases follow this general process:

1. The maintainer decides when to create a new release based on accumulated changes
2. Releases follow semantic versioning (`MAJOR.MINOR.PATCH`)
3. All releases undergo SLSA Level 3 compliant builds and Sigstore signing
4. Security issues may prompt accelerated releases

## Future Governance

There are no current plans to transition to a multi-maintainer governance model. If circumstances change, this document will be updated to reflect any new governance structure.

## Code of Conduct

While there is no formal contributor community requiring extensive conduct guidelines, all interactions in project spaces should be respectful and professional. The maintainer reserves the right to moderate discussions and remove inappropriate content.

## Changes to Governance

This governance document may change as the project evolves. Significant changes will be announced through GitHub releases and commit messages.

---

Last updated: March 2025