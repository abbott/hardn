# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## New

- **The main menu warns when a release's vendor support ends within 90 days, or has ended**, and names the release to upgrade to. Every plan carries the same warning (`PLATFORM_SUPPORT_ENDS`, `PLATFORM_SUPPORT_ENDED`). It changes nothing hardn does. Proxmox VE 9 has no date yet, so no warning. The dates are in docs/platforms.md.
- **The JSON document has `host.platform.supportEnds`**, the date hardn holds for the release.
