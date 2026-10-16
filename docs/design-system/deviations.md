# Design deviations log

> **What this is:** the deviation log from brief §2 (Design fidelity). A deviation is a place where the implementation differs from the Figma design because of a **terminal limit** or **drift in the export**. The implementer decides each deviation and records the reason here.
>
> **Not here:** anything that changes meaning, flow or wording. That is an enhancement and waits for the maintainer in [../refactor/proposals.md](../refactor/proposals.md). Small, obvious fixes, such as an alignment fix or a label clipped at 80 columns, are deviations.
>
> **Why it matters:** the Figma file is not updated from the code. This log, the proposals log and the design-system docs are the only record of where the code departs from the screenshots, so keep it current.
>
> **Sources:**
> - The Figma token export and screenshots are a reference kept outside the repository; they are never committed.
> - Findings: [../refactor/phase0-findings.md](../refactor/phase0-findings.md#81-figma-export) §8.1.
> - Screen-by-screen detail: [../refactor/phase0/interaction.md](../refactor/phase0/interaction.md).

## How to add an entry

1. Give it the next ID (`DEV-NNN`) and the date.
2. Name the **token or component** (Go name, e.g. `text.faint`, `StatusRow`) and the screen(s) affected.
3. Record the **Figma value** (hex, cell position, glyph, wording as drawn) and the **implemented value**, including the 256- and 16-color picks for colors.
4. Give the **reason**: the terminal limit or the drift item (`D1`–`D14` below), with any measurement, such as a contrast ratio.
5. List the **environments checked**: the color profile (truecolor / 256 / 16 / none), the background (dark / light), the glyphs (Unicode / ASCII), the width (60 / 80 / 120) and any real terminal used. Write "golden only" when nothing was checked by eye.
6. Link the change unit that implemented it, and any related proposal.

## Log

| ID | Date | Token / component | Figma value | Implemented value | Reason | Environments checked | Change unit |
|---|---|---|---|---|---|---|---|
| — | — | *(no entries yet; Phase 1 adds the first ones)* | | | | | |

---

## Candidates (not yet implemented)

These 14 candidates come from the Phase 0 inventory of the Figma export. Each becomes a log entry when the unit that implements it lands, with its environments filled in. Until then, nothing has been checked in any environment.

**Notes on the values:**
- Contrast is the WCAG ratio against #000000, the terminal default background in the export, unless stated otherwise.
- 256-color picks are the nearest xterm index by CIE76 ΔE; 16-color picks are deliberate.

| # | Token / component | Figma value | Proposed implemented value | Reason | Environments to check |
|---|---|---|---|---|---|
| C1 | `text.faint` (status details, uptime, URL, codename, `≈` text, parentheticals) | #2B2F3B: **1.57:1** on black, 1.07:1 on a typical dark theme (#282A36) | Lifted to about #565B6B (3.10:1 on black, 2.11:1 on #282A36). 256: 59 or 240 (Figma's own nearest is 236). 16: 8, bright black, which merges with muted; SGR faint is not used (brief §1A) | Nearly invisible on dark backgrounds | truecolor, 256, 16 × dark; 80 cols |
| C2 | `divider` (full-width rule, section-header rule) | #16181D (**1.18:1**); the main-menu panel outline is #17181C | #2B2F3B or the lifted `text.faint`. 256: 234. 16: 8 | An invisible rule doesn't separate anything | truecolor, 256, 16 × dark |
| C3 | `badge.fg` (host and user badges) | Host badge text #C0C4D8 (bold, 28 px); user badge text #DFDFDF (bold, 32 px); ΔE76 14.3 (drift D3) | One token, #DFDFDF (7.48:1 on #0C2AC0, vs 5.75:1 for #C0C4D8). 256: 254. 16: 15, bold | Near-duplicate values for one role | truecolor, 256, 16 × dark and light |
| C4 | `bar.bg` at 16 colors (distro bar, title bars, section stub) | #101114 (1.11:1 on black) | Background color 8 at 16 colors, which is more visible than Figma. Truecolor keeps #101114; 256 uses 233 | 16 colors have no near-black: the bar would vanish or render as plain black | 16 × dark and light |
| C5 | Header version box at 16 colors | Version text inside a #101114 box | No box at 16 colors; the version is plain muted text | Muted text on background 8 would be unreadable | 16 × dark |
| C6 | Type sizes (wordmark, host badge, `❯` prompt glyph) | 28 px header and badge text; 24 px `❯`; 32 px body | One cell size. Bold and normal weights are kept | Terminals have no font sizes | all |
| C7 | Letter spacing | 0 and −0.5% mixed inside the same styles (drift D2) | Dropped | No terminal equivalent. It also causes ~0.5-cell offsets between menu labels (col 5.26) and status labels (col 5.94) | all |
| C8 | Cursor | #C0C4D8 block rectangle after the prompt | Dropped; the terminal draws its own cursor | The terminal owns the cursor (drift D5) | all |
| C9 | Main-menu panel outline | #17181C outline around the status block (in a collapsed SVG) | Dropped | Invisible on black; duplicates the divider role (drift D6) | all |
| C10 | Value / description column (two-column rows, `StatusRow`, `KeyValue`, `Menu`) | Varies by frame: 22.5–23.5 cells on 01/02/05, 26.0 on create user, 26.56–26.72 on the main menu, 29.4–32.5 on other rows (drift D9) | One column at **col 27**: indent 2, marker cols 2–4, label col 6 with width 21 | Drift; one grid for every screen | 80 cols; plus the narrow-width rule (stacked rows below about 70 columns, if approved) |
| C11 | Menu headings and prompt position | Heading x = 0.78 / 1.30 / 1.77 cells; `❯` x = 1.51 / 1.88 / 3.33 (drift D10) | Snapped to the content indent, col 2 | Drift | 80 cols |
| C12 | Typographic quotes | ‘ ’ (U+2018/U+2019) in `Press ‘q’…`, `‘root’`, the created user's name | ASCII `'` | Both curly quotes are East Asian Ambiguous width, so they misalign in ambiguous-wide terminals; ASCII mode needs them anyway | Unicode and ASCII; a CJK locale |
| C13 | Bars (distro context bar, title bars, section stubs) | Filled #101114 rectangles (the brief's text transcription shows `▌` and `█` glyphs) | Background-colored spaces. With no color: a `#` bar and a `-` rule | Block glyphs `▌ █ ─` are ambiguous-width; a fill maps naturally to a background color | all color profiles; no-color; ASCII |
| C14 | Header URL position | URL at col ≈22.5, not aligned with the value column | Aligned to the value column, col 27 | Drift (same family as D9) | 80 cols |

### Export drift items and how they are handled

Each drift item from the export inventory is either covered by a candidate above or resolved in the token mapping itself (the design-system README records the mapping).

| Drift | What | Resolution | Candidate |
|---|---|---|---|
| D1 | Spacing 17 px vs 18 px | Both ≈ 1 cell; merged | mapping |
| D2 | Letter spacing 0 vs −0.5% | Dropped | C7 |
| D3 | Badge text #C0C4D8 vs #DFDFDF | One `badge.fg` | C3 |
| D4 | `color.text.input` (#DFDFDF) is used only on badges; echoed input is #81869C | Echoed answers use `text.muted` | mapping |
| D5 | #C0C4D8 is both primary text and the cursor | Cursor role dropped | C8 |
| D6 | Rule #16181D vs panel outline #17181C | One `divider`; outline dropped | C2, C9 |
| D7 | #101114 has three roles (bar, section stub, version box) | One `bar.bg` | C4, C5 |
| D8 | #000000 is the page background and the knockout fill | Terminal default background; never painted | mapping |
| D9 | Value column varies by screen | Col 27 | C10, C14 |
| D10 | Heading and prompt x drift | Col 2 | C11 |
| D11 | Blank lines before the prompt: 2.25 vs 2.74–2.77 pitches | One blank line | mapping |
| D12 | Knockout right padding 10 px vs 17 px | 1 cell | mapping |
| D13 | Header badge at x 80, bar at x 87 | Both at col 0 | mapping |
| D14 | Status text vs accent-bar color pairs (#70D47F/#008A00, #DBB853/#D2BC13) | **Intentional pairs, not duplicates**: `status.*` and `accent.*` stay separate | none needed |

### Departures the brief already decided

These change what the screenshots show, but brief §1–§2 already decided them, so they need no proposal. Log each one here when its unit lands.
- **Exit row wording:** Figma's `Press ‘q’ to exit immediately` no longer fits Enter-based input. It becomes "q to quit" (brief §1C).
- **Muted and faint text:** these come from gray color tokens, never from the SGR faint attribute (brief §1A).
- **ASCII glyph set:** `[+] [ ] [*] > ~` replace `⛛ ⭘ ⛬ ❯ ≈` when `TERM` is `linux` or `dumb`, when the locale isn't UTF-8, or with `--ascii` / `HARDN_ASCII=1` (brief §1A). The findings add CJK locales to this list.

The Figma patterns the brief rejects (press any key, plain `(y/n)` on lockout-risk changes, collect/confirm/result on one screen) and the sub-screen header question are **proposals**, not deviations: see [P-08](../refactor/proposals.md#p-08), [P-09](../refactor/proposals.md#p-09) and [P-10](../refactor/proposals.md#p-10).
