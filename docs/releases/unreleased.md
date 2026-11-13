# hardn (next release)

> **Draft: every line is proposed until the maintainer approves the words.** At the release candidate this file becomes `docs/releases/v<x.y.z>.md`, the release's whole body, and this note is cut.

## New

- **`hardn system-details --output markdown` prints a report to hand over**, for an auditor or a manager: the host and its support level, the risk level and why, each control's status with its evidence, the findings, and System Details. It is one Markdown document on stdout: save it with `> web-01.md`. It changes nothing and exits 0. Run it with `sudo` or `doas`: without root, what only root can read shows Unknown, and the report says so. Unlike the other forms of `system-details`, it reads hardn.yml, for the DNS row.
