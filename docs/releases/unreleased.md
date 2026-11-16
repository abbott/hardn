# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## Added

- **TrueNAS is detected, and every change refused there** (`PLATFORM_TRUENAS`): TrueNAS writes its system's files back from its own settings. The screens and the status still show, and the main menu says to make changes in TrueNAS's web UI.
- **Raspberry Pi OS (64-bit) is detected**, and treated as Debian with warnings: the Pi's own packages aren't upgraded by Auto Updates (`UPDATES_RPI_NOT_UPGRADED`), a lockout on a headless Pi means taking out the SD card, and most Pis have no clock without NTP.
- **Every plan on Proxmox VE and Raspberry Pi OS warns** that a change that is safe on Debian can break it (`PLATFORM_CUSTOM`). NTP's review on Proxmox VE adds that the nodes need synchronised clocks.
- **The JSON's platform has `variant`**: `truenas`, `raspios`, or `""`.
