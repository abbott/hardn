# The status symbols, in the gallery (`internal/tools/uigallery`: `glyphs`)

> **Units:** refactor plan Phase 4, `next/p4-glyph-compare` (the mock, for the maintainer's review in a real terminal and font) and `next/p4-status-glyphs` (the set adopted). **Decided** (the maintainer, 2026-10-21): "Adopt the trial set: Partial ⭘ in status.warn, Unknown ? in text.muted, Not Applicable – in text.muted, ⛬ only on the risk level." The theme holds that set ([glyphs.go](../../pkg/ui/theme/glyphs.go), [theme.go](../../pkg/ui/theme/theme.go)); the mode's `current` set is it, so the `trial` set, now its duplicate, is gone.

## 1. The adopted set

Figma draws three status symbols, each with its colour: ⛛ U+26DB, a measure in place (`status.ok`); ⭘ U+2B58, off (`status.bad`); ⛬ U+26EC, the overall risk level (by band; 00-TUI-MAIN). The maintainer's reasons: ⛬ is the risk level's own symbol, and Partial and warnings, which drew it until now, diluted it; ⭘ already means off, inactive, empty, null or incomplete. Unknown's blue (`status.unknown`) and the other symbols Figma does not draw were agent decisions of Phase 1 (DEV-010, DEV-012 to DEV-014, DEV-024, DEV-031, DEV-034), never approved.

**The status lines.** Every state a `StatusRow` can show; the words are the brief's (DEV-024), the details each check's (each `check-<area>.md` §5). No wording changed.

| State | Unicode | ASCII | Colour of glyph and word | Word | Until 2026-10-21 |
|---|---|---|---|---|---|
| Configured | ⛛ U+26DB | `[+]` | `status.ok` | Configured | unchanged |
| Partial | ⭘ U+2B58 | `[ ]` | `status.warn` | Partial | ⛬ `[*]` |
| Not Configured | ⭘ U+2B58 | `[ ]` | `status.bad` | Not Configured | unchanged |
| Not Applicable | – U+2013 | `[-]` | `text.muted` | Not Applicable | ∅ U+2205 |
| Unknown | ? U+003F | `[?]` | `text.muted` | Unknown | `status.unknown`, blue |

**The other lines that draw these marks or colours:**

| Line | Unicode | ASCII | Colour | Follows |
|---|---|---|---|---|
| a warning message, "Warning: …" | ⭘ | `[ ]` | `status.warn`, bold | `Warn`, the maintainer's "Warning: ⭘ status.warn" |
| the confirm countdown, while there is time | ⭘ | `[ ]` | `status.warn`, bold | `Warn` |
| the host block's "Reboot pending" | ⭘ | `[ ]` | `status.warn` | `Partial` |
| a key-value row's warn value | ⭘ | `[ ]` | `status.warn` | `Partial` |
| a key-value row's unknown and not-applicable values | ? and – | `[?]` and `[-]` | `text.muted` | `Unknown`, `NotApplicable` |
| a menu item's "Unavailable" | – | `[-]` | `text.faint`, the item's | `NotApplicable` |
| the risk level, band Unknown | ⛬ | `[*]` | `text.muted` | `Risk`; the other bands keep their colours |
| "Unknown" for a distribution, hostname, address or uptime not read, a plan's untitled change, a report's unnamed rolled-back change | none | none | `text.muted` | `Unknown`'s colour |
| the P-42 compare mock's rolled-back step, `-marker status` | ⭘ | `[ ]` | `status.warn` | `Partial` |

**The rule, changed.** Partial and Not Configured share ⭘ `[ ]`, and a warning takes it too. They are told apart by colour and by the word, which is always on the line: never by colour alone. Unknown and Not Applicable share `text.muted` and differ by glyph and word. A key-value row has no state word: there a warn and a bad value differ by colour, and without colour only by the value's own words, such as "None" beside "Enabled, No password" (DEV-034). A not-applicable value's – sits in the bullet's column in the bullet's grey, which §4's figures put close to `-` in some fonts; its value or "Not Applicable" is beside it. A status row on Partial or Not Configured shows its state's word, never its `Word`, which would leave the two rows alike without colour (`TestLayout`).

## 2. Where ⛬ is still drawn, for the maintainer to confirm

No other glyph is the risk icon or holds it (`TestRiskGlyphAlone`), and over every component state, in both glyph sets, ⛬ `[*]` is drawn only by these components, before these words (`TestRiskGlyphDrawn`):

| Where | Line | Status |
|---|---|---|
| `RiskSummary` | `⛬  Risk Level` | the maintainer's decision |
| `PlanView`, a lockout-risk change | `⛬  Lockout risk`, the title on the next line | **pending** the maintainer's answer; chosen 2026-10-20 (DEV-038) |
| `Menu`, a risky item's note line | `⛬ Lockout risk` | **pending** the maintainer's answer (DEV-028, P-22) |

Beyond these, the theme's swatch golden lists the `Risk` glyph, and this mode names ⛬ in its note and draws it in its strip.

## 3. What the mode shows

- **The sets** (§4). Each is the risk row, Figma's amber ⛬ Moderate, and 00-TUI-MAIN's seven rows in all five states, under a legend that names each mark's code point and colour token. The details are lines the checks give. Partial, Not Configured and Unknown are on adjacent rows, so a set that folds a state into ⭘ shows it beside a row that is off. As many sets sit side by side as the terminal holds: each is 58 cells wide, 4 apart, so 2 fit from 120 columns, 3 from 182 and 5 from 306. Otherwise they are one above the next, and below 64 columns the rows stack.
- **The strip.** Each shortlisted glyph alone in the marker column, then ⛛ ⭘ ⛬ two cells apart, all in `text.primary`, so size and weight compare without colour. Today's `?` and `–`, and Phase 1's `∅`, are included. It has no ASCII form.

The mock draws the rows itself, on the components' grid, as `compare` does. Its `current` set is what `RiskSummary` and `StatusList` draw, byte for byte (`TestGlyphsCurrent`), so the other sets differ from it only by their glyphs and colours.

## 4. The sets

| Set | Partial | Unknown | Not Applicable | ASCII | Why |
|---|---|---|---|---|---|
| `current` | ⭘ U+2B58, `status.warn` | ? U+003F, `text.muted` | – U+2013, `text.muted` | `[ ]` `[?]` `[-]` | The trial set, the maintainer's combination, adopted on 2026-10-21: Figma's ⭘ in amber for Partial, and `?` and `–` in muted grey for Unknown and Not Applicable, with no new hue. Read from the theme |
| `power` | ⏼ U+23FC, `status.warn` | ? U+003F, `text.muted` | – U+2013, `text.muted` | `[~]` `[?]` `[-]` | `current` with the power on-off symbol for Partial, which the maintainer asked to see (2026-10-21); its measurements are in `power-*.tsv` beside metrics.md. Set aside |
| `figma` | ⭘ U+2B58, `status.warn` | ⭘ U+2B58, `text.muted` | no marker; the row in `text.muted` | `[ ]` `[ ]` none | Treats no gap as real: Figma's glyphs and colours only, and the word carries the state. Set aside |
| `diamond` | ⬙ U+2B19, `status.warn` | ⁇ U+2047, `text.muted` | – U+2013, `text.muted` | `[~]` `[?]` `[-]` | A new glyph for each state: the measurements' set A. Set aside |
| `diamond-dot` | ◈ U+25C8, `status.warn` | ? U+003F, `text.muted` | ⊟ U+229F, `text.muted` | `[~]` `[?]` `[-]` | The measurements' set B, which keeps `?`. Set aside |
| `square` | ◧ U+25E7, `status.warn` | ⍰ U+2370, `text.muted` | no marker; the row in `text.muted` | `[~]` `[?]` none | The heaviest Partial and Unknown candidates. The measurements' set C also had ⛝, whose outline is ◧'s, so it is left out here. Set aside |

Until the adoption, `current` was Phase 1's set (⛬ `[*]`, `?` in `status.unknown`, `∅`), and `diamond`, `diamond-dot` and `square` drew Unknown in `status.unknown`'s blue. That token left the palette with the adoption, so those three draw Unknown in `text.muted` now, as the gallery takes every colour from the theme. In every set Configured is ⛛ and Not Configured is ⭘, in their colours, and ⛬ (`[*]` in ASCII) is on the risk row alone (`TestGlyphsRiskAlone`). `[~]` is the mock's ASCII form for a half-filled Partial, not the audit's; `~` is also `Approx`'s ASCII form, before the risk row's gloss.

The figures behind the choices are in the measurements (§6). Some of them, with "× ⛛" meaning relative to ⛛ in the same font, medians over 19 fonts, 8 of them Menlo's design (§6): ⬙ ink 0.78 × ⛛; ◈ 0.86; ◧ 1.45; ⁇ 1.00; ⍰ 1.49, at 1.39 × ⛛'s height; `?` 0.53; ⊟ 0.91; – 0.29. Among a set's three new glyphs, outline overlap is at most 0.64 in `diamond` and 0.54 in `diamond-dot`. Against ⭘, which every set also draws, it reaches 0.75 (`diamond`'s ⁇), 0.81 (`diamond-dot`'s ⊟) and 0.80 (`square`'s ◧). Every half-filled circle was ruled out because its outline is ⭘'s (0.99), and so was ∅ (0.89). On a detail row a Not Applicable value with text takes the bullet's column (DEV-034), both in `text.muted`: – overlaps `-` at 0.78, above the family's widest pair (⭘ and ⛬, 0.68), and in Berkeley Mono is 0.70 of a cell wide against the hyphen's 0.60, at the same height.

## 5. Run it

From the repository root:

```sh
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs              # every set, then the strip
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs current figma
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs strip
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs -ascii       # the ASCII forms
GOTOOLCHAIN=local GOWORK=off go run ./internal/tools/uigallery glyphs -theme light -profile 256
```

The flags are `-list`, `-theme`, `-profile`, `-ascii`, `-override` (token values; see `uigallery.local.json.example`), and `-width` and `-height`, which default to the terminal's size as `compare`'s do. `-height` changes nothing here. A name that is neither a set nor `strip`, `trial` now included, or a flag placed after a name, exits with status 2.

## 6. The measurements

`_support/hardn-audit-2026-10-16/glyphs/metrics.md` is in the sibling `_support/` folder, outside the repository. It covers coverage, size, weight, cell fit and outline overlap for every candidate in this Mac's 19 monospace fonts, with CoreText renders of sets A, B and C (`context-*.png`). The 19 are not 19 designs: Menlo and its 7 MesloLG builds measure alike, and STIX Two Math draws ⛛ and ⛬ in all of them. Berkeley Mono's own values are in `table-berkeley.md`. Those numbers come from CoreText and its fallback fonts. This mode is the check in a real terminal. Linux and Windows terminals were not measured.

## 7. Tests and goldens

The mode's:

- `TestGlyphsGolden`: one golden per set and one for the strip, on `uitest.Core()`, in `testdata/golden/glyphs/<name>.golden`.
- `TestGlyphsCurrent`: the current set against the components.
- `TestGlyphsRiskAlone`: in every set, today's too, ⛬ is on the risk row alone, and the marks differ from one another but where a set folds a state into ⭘.
- `TestGlyphsCells`: every candidate is one cell and every ASCII form three.
- `TestGlyphsSideBySide`, `TestGlyphsSmoke` and `TestGlyphsUsage` cover the layout, every environment, and the flags.

The adopted set's (`next/p4-status-glyphs`), in `pkg/ui/theme` and `pkg/ui/component`:

- `TestStatusMarkers`: every marker fits the column; only Partial shares a marker, Not Configured's, and `Warn` is Partial's. It held "no two states share a marker" until now.
- `TestSharedMarkerColours`: wherever there is colour, Partial's and Not Configured's styles differ, on a status row and on a key-value row.
- `TestStatusStates`: two states sharing a glyph differ by word and colour. `TestLayout`: a row on Partial or Not Configured keeps its state's word when given a `Word`. `TestValueStates`: a warn value's marker is a bad one's, in another colour.
- `TestRiskGlyphAlone` and `TestRiskGlyphDrawn`: §2.
- `TestColourRoles`: every Unknown slot, `Risk.Unknown` included, is `text.muted`; `TestContrast` no longer lists `status.unknown`.

**The goldens the adoption changed** (`make golden-update`; nothing else in them changed):

| Directory | Files | Why |
|---|---|---|
| `pkg/ui/theme/testdata/golden` | `swatch` | both: the `Partial`, `NotApplicable` and `Warn` glyphs; the `status.unknown` token's line gone |
| `pkg/ui/component/testdata/golden/statusrow` | `partial`, `not-applicable`, `unknown` | glyph (Partial, Not Applicable); colour (Unknown) |
| `pkg/ui/component/testdata/golden/keyvalue` | `warn`, `not-applicable`, `unknown` | glyph (warn, not applicable); colour (unknown) |
| `pkg/ui/component/testdata/golden/hostblock` | `reboot-pending`; `unknown-address`, `unknown-uptime`, `no-hostname`, `all-unknown` | glyph (Reboot pending); colour ("Unknown") |
| `pkg/ui/component/testdata/golden/contextbar` | `unknown` | colour |
| `pkg/ui/component/testdata/golden/risksummary` | `unknown` | colour (the Unknown band's ⛬ and word) |
| `pkg/ui/component/testdata/golden/planview` | `unknown` | colour (an untitled change) |
| `pkg/ui/component/testdata/golden/errorreport` | `unknown` | colour (an unnamed rolled-back change) |
| `pkg/ui/component/testdata/golden/menu` | `disabled` | glyph ("Unavailable") |
| `pkg/ui/component/testdata/golden/message` | `warn` | glyph |
| `pkg/ui/component/testdata/golden/confirmcountdown` | `waiting`, `port`, `command` | glyph |
| `pkg/ui/flow/testdata/golden/flow` | `applied`, `cancelled`, `dry-run`, `failed` | glyph (the SSH Keys warn value) |
| `pkg/ui/nav/testdata/golden/nav` | `rounds`, `sub-menu` | glyph ("Unavailable") |
| `pkg/ui/screen/testdata/golden/workflow-step/user-create-user` | `normal/5-confirm`, `cancelled/5-confirm`, `dry-run/5-planned`, `failure/5-confirm` | glyph (the SSH Keys warn value) |
| `pkg/output/testdata/golden/human` | `after-flow`, `awaiting-confirmation`, `check`, `header`, `plan`, `rolled-back`, `status` | glyph (warnings, the countdown, Partial, Not Applicable); colour (the Unknown risk band) |
| `internal/tools/uigallery/testdata/golden/status` | 13 hosts | glyph (Partial, Not Applicable); colour (`apparmor-unread`'s Unknown) |
| `internal/tools/uigallery/testdata/golden/play/status` | `lockout` | glyph (Partial) |
| `internal/tools/uigallery/testdata/golden/play/engine` | 7 scenarios | glyph (the countdown, warnings) |
| `internal/tools/uigallery/testdata/golden/compare/bullet` | `countdown`, `messages`, `success` | glyph (the countdown, warnings) |
| `internal/tools/uigallery/testdata/golden/compare/status` | `countdown`, `failure`, `messages`, `success` | glyph (the countdown, warnings, and the rolled-back marker) |
| `internal/tools/uigallery/testdata/golden/glyphs` | every set and the strip; `trial` removed | both, and the note's words: `current` is the adopted set; Unknown muted in every set; the strip's "(current)" labels |

`internal/tools/p4status`'s host files are unchanged: they hold the hosts, not the renders, and `TestMainMenuAsGallery` still matches the gallery's plain-80 cells, which changed with p4status's own output.

## 8. Written, not run (gate 5)

The glyphs and colours as the components now draw them are golden only: no terminal or font showed them. How amber ⭘ reads beside red ⭘ (Partial beside Not Configured, a warn value beside a bad one) and grey `?` and `–` beside the details' fainter grey is not verified. Unknown's and Not Applicable's words in `text.muted` are 3.95:1 in the light theme at 16 colours, under the status words' 4.5:1 (DEV-010).

## 9. API

No exported Go identifier changed. `StatusRow.Word` is not shown on Partial or Not Configured (§1). The palette's unexported `StatusUnknown` and its token `status.unknown` are gone, so the gallery's `-override` rejects `status.unknown` as an unknown token. The glyphs mode no longer has `trial`, which now exits with status 2 (§5).
