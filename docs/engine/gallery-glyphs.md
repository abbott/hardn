# The status symbols, in the gallery (`internal/tools/uigallery`: `glyphs`)

> **Unit:** refactor plan Phase 4, `next/p4-glyph-compare`. A dev-only mock for the maintainer's review of the status rows' symbols, in his own terminal and font. **Nothing is decided**: no component, theme value, glyph or golden of hardn changes.

## 1. What it shows

Figma draws three status symbols, each with its colour: ⛛ U+26DB, a measure in place (`status.ok`); ⭘ U+2B58, off (`status.bad`); ⛬ U+26EC, the overall risk level (by band). Today ⛬ also marks Partial, `?` Unknown and `∅` Not Applicable (DEV-012 to DEV-014, DEV-024; [glyphs.go](../../pkg/ui/theme/glyphs.go)). `glyphs` shows:

- **The sets** (§2). Each is the risk row, Figma's amber ⛬ Moderate, and 00-TUI-MAIN's seven rows in all five states, under a legend that names each mark's code point and colour token. The details are lines the checks give. Partial, Not Configured and Unknown are on adjacent rows, so a set that folds a state into ⭘ shows it beside a row that is off. As many sets sit side by side as the terminal holds: each is 58 cells wide, 4 apart, so 2 fit from 120 columns, 3 from 182 and all 5 from 306. Otherwise they are one above the next, and below 64 columns the rows stack.
- **The strip.** Each shortlisted glyph alone in the marker column, then ⛛ ⭘ ⛬ two cells apart, all in `text.primary`, so size and weight compare without colour. Today's `?` and `∅` are included. It has no ASCII form.

The mock draws the rows itself, on the components' grid, as `compare` does. Its `current` set is what `RiskSummary` and `StatusList` draw, byte for byte (`TestGlyphsCurrent`), so the other sets differ from today's only by their glyphs and colours.

## 2. The sets

| Set | Partial | Unknown | Not Applicable | ASCII | Why |
|---|---|---|---|---|---|
| `current` | ⛬ U+26EC, `status.warn` | ? U+003F, `status.unknown` | ∅ U+2205, `text.muted` | `[*]` `[?]` `[-]` | Today's. ⛬ appears twice, on the Partial row as on the Moderate risk row, in the same colour |
| `figma` | ⭘ U+2B58, `status.warn` | ⭘ U+2B58, `text.muted` | no marker; the row in `text.muted` | `[ ]` `[ ]` none | Treats no gap as real: Figma's glyphs and colours only, and the word carries the state |
| `diamond` | ⬙ U+2B19, `status.warn` | ⁇ U+2047, `status.unknown` | – U+2013, `text.muted` | `[~]` `[?]` `[-]` | A new glyph for each state: the measurements' set A |
| `diamond-dot` | ◈ U+25C8, `status.warn` | ? U+003F, `status.unknown` | ⊟ U+229F, `text.muted` | `[~]` `[?]` `[-]` | The measurements' set B, which keeps today's `?` |
| `square` | ◧ U+25E7, `status.warn` | ⍰ U+2370, `status.unknown` | no marker; the row in `text.muted` | `[~]` `[?]` none | The heaviest Partial and Unknown candidates. The measurements' set C also had ⛝, whose outline is ◧'s, so it is left out here |

In every set Configured is ⛛ and Not Configured is ⭘, in their colours. In every set but `current`, ⛬ (and `[*]` in ASCII) is on the risk row alone (`TestGlyphsRiskAlone`). `[~]` is the audit's ASCII form for a half-filled Partial; `[/]` was the other choice. These are not shown: Partial ⭘ in red, Unknown ⭘ in red or blue, and splitting Partial by meaning, which is an engine change.

The figures behind the choices are in the measurements (§4). Some of them, with "× ⛛" meaning relative to ⛛ in the same font, medians over 19 fonts: ⬙ ink 0.78 × ⛛; ◈ 0.86; ◧ 1.45; ⁇ 1.00; ⍰ 1.49, at 1.39 × ⛛'s height; `?` 0.53; ⊟ 0.91; – 0.29. Outline overlap within a set is at most 0.64 for `diamond` and 0.54 for `diamond-dot`. Every half-filled circle was ruled out because its outline is ⭘'s (0.99), and so was ∅ (0.89).

## 3. Run it

From the repository root:

```sh
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs              # every set, then the strip
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs current figma
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs strip
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs -ascii       # the ASCII forms
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs -theme light -profile 256
```

The flags are `-list`, `-theme`, `-profile`, `-ascii`, `-override` (token values, such as `status.unknown`'s; see `uigallery.local.json.example`), and `-width` and `-height`, which default to the terminal's size as `compare`'s do. `-height` changes nothing here. A name that is neither a set nor `strip`, or a flag placed after a name, exits with status 2.

## 4. The measurements

`_support/hardn-audit-2026-10-16/glyphs/metrics.md` is in the sibling `_support/` folder, outside the repository. It covers coverage, size, weight, cell fit and outline overlap for every candidate in this Mac's 19 monospace fonts, with CoreText renders of sets A, B and C (`context-*.png`). Those numbers come from CoreText and its fallback fonts. This mode is the check in a real terminal. Linux and Windows terminals were not measured.

## 5. Tests and goldens

- `TestGlyphsGolden`: one golden per set and one for the strip, on `uitest.Core()`, in `testdata/golden/glyphs/<name>.golden`.
- `TestGlyphsCurrent`: the current set against the components.
- `TestGlyphsRiskAlone`: ⛬ is on the risk row alone, and the marks differ from one another.
- `TestGlyphsCells`: every candidate is one cell and every ASCII form three.
- `TestGlyphsSideBySide`, `TestGlyphsSmoke` and `TestGlyphsUsage` cover the layout, every environment, and the flags.
