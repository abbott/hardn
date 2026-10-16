# hardn design system

> **For:** maintainers reviewing the design system. It records what is built, why, and where it departs from the Figma design.
>
> **Status:** Phase 1A (foundations) of the refactor plan: `pkg/ui/term` (the render environment), `pkg/ui/theme` (tokens, style slots, glyphs, grid) and `pkg/ui/uitest` (the golden harness); and from Phase 1B the first components in `pkg/ui/component`: the header group (`next/p1b-header`), the status group (`next/p1b-status`), the menu group, with the accepted-input data in `pkg/ui/keys` (`next/p1b-menu`), the block group (`next/p1b-blocks`), and the plan group, with the line diff in `pkg/diff` (`next/p1b-plan`). Screens, the gallery and the import rules come in later units; their sections below are placeholders. Nothing in `cmd/hardn` uses `pkg/ui` yet: `go list -deps ./cmd/hardn` lists no `pkg/ui`, `charm.land` or `charmbracelet` package.
>
> **Companion logs:** [deviations.md](deviations.md) (where the code departs from Figma, and why) and [../refactor/proposals.md](../refactor/proposals.md) (enhancements waiting for the maintainer).

## Contents

1. [Purpose and Figma fidelity](#1-purpose-and-figma-fidelity)
2. [Lip Gloss](#2-lip-gloss)
3. [Render environment](#3-render-environment)
4. [Tokens](#4-tokens)
5. [Typography](#5-typography)
6. [Glyph sets](#6-glyph-sets)
7. [Grid and spacing](#7-grid-and-spacing)
8. [Golden tests](#8-golden-tests)
9. [Components, screens and usage rules](#9-components-screens-and-usage-rules)
10. [Token report](#10-token-report)

## 1. Purpose and Figma fidelity

The design system is the one place that decides how hardn's terminal output looks: which colour a state takes at each colour profile, which glyph marks it, where the columns fall and how output degrades without colour, Unicode, width or a terminal. Screens are assembled from its components rather than styled by hand.

The refactor brief (kept outside the repository) sets these rules:
- **Renderers are pure functions** of a view-model and the render environment (`term.Env`). They return strings and do no I/O. `term.Detect` is the only code that looks at the terminal.
- **No raw ANSI escape codes outside `pkg/ui`.** Feature code gets its styling from the theme.
- **Muted and faint text come from gray colour tokens, never from SGR 2 (faint).** No style slot sets faint, blink or reverse (DEV-009).
- **Never colour alone.** Every state also has a word or a glyph, so it reads the same without colour.
- **Degrade gracefully** with no colour, no Unicode, a narrow terminal, no TTY or a light background.

**Figma fidelity.** The Figma screenshots and token export set the intended direction; they are not a spec, and nothing fails because output differs from them.
- **The Go theme is the runtime source of truth.** `pkg/ui/theme` is derived from the export. The export is a reference kept outside the repository: it is never loaded at runtime and never committed.
- **Drift is resolved by meaning.** Near-duplicate values for one role become one token. Values with no terminal equivalent (font family and size, line height, letter spacing, radius, shadow, opacity, sub-cell spacing) map to the nearest terminal concept or are dropped.
- **Two logs, two owners.** A *deviation* is a difference forced by a terminal limit or by drift in the export; the implementer decides it and records it in [deviations.md](deviations.md). An *enhancement* changes meaning, flow or wording; it waits for the maintainer in [proposals.md](../refactor/proposals.md). Small, obvious fixes, such as an alignment fix, are deviations.
- **Figma is not updated from the code.** The two logs and this document are the only record of where the code departs from the screenshots.

## 2. Lip Gloss

### 2.1 Choice

**`charm.land/lipgloss/v2` v2.0.6**, the latest stable tag (2026-08-11; `go list -m charm.land/lipgloss/v2@latest`). The brief leaves the major version to the implementer and asks for the latest stable release.

**Not v1.** `github.com/charmbracelet/lipgloss` is frozen at v1.1.0 (tagged 2025-03-12; still `@latest` on 2026-10-16), while v2 has had six patch releases since v2.0.0 (2026-02-24). v1 also queries the terminal background behind the caller's back: an `AdaptiveColor` asks termenv the first time it renders, unless `SetHasDarkBackground` was called first (v1.1.0 `color.go:99-104`, `renderer.go:138-151`). v2 makes that query an explicit call (§2.3).

**Go floor.** Lip Gloss v2.0.6, colorprofile v0.4.3 and ultraviolet declare `go 1.25.0` in their `go.mod`. hardn's `go.mod` already says `go 1.26.0`, set by the Phase 0 refresh because x/text v0.43.0 and x/sys v0.49.0 declare it (as does x/sync v0.24.0 below). The checks run on Go 1.27.2.

### 2.2 Modules added

Approved by the maintainer (refactor plan §3.9). From the `go.mod` diff between `next/v0.4` and this branch. None of them imports `"C"`, and `pkg/ui` builds with `CGO_ENABLED=0` for the three release targets (linux/amd64, linux/arm64, darwin/amd64).

| Module | Version | Licence | Imported by hardn |
|---|---|---|---|
| `charm.land/lipgloss/v2` | v2.0.6 | MIT | yes: `pkg/ui/term`, `pkg/ui/theme`, `pkg/ui/uitest` |
| `github.com/charmbracelet/colorprofile` | v0.4.3 | MIT | yes: profiles and `colorprofile.Writer` |
| `github.com/charmbracelet/x/ansi` | v0.11.8 | MIT | yes: colour types in the theme, width and stripping in `uitest` |
| `github.com/charmbracelet/x/term` | v0.2.2 | MIT | yes: `IsTerminal` and `GetSize` in `term.Detect` |
| `github.com/charmbracelet/ultraviolet` | v0.0.0-20260811164956-006e29f97886 | MIT | indirect |
| `github.com/charmbracelet/x/termios` | v0.1.1 | MIT | indirect |
| `github.com/charmbracelet/x/windows` | v0.2.2 | MIT | indirect |
| `github.com/clipperhouse/displaywidth` | v0.11.0 | MIT | indirect |
| `github.com/lucasb-eyer/go-colorful` | v1.4.1 | MIT | indirect |
| `github.com/muesli/cancelreader` | v0.2.2 | MIT | indirect |
| `github.com/rivo/uniseg` | v0.4.7 | MIT | indirect |
| `github.com/xo/terminfo` | v0.0.0-20220910002029-abceb7e1c41e | MIT | indirect |
| `golang.org/x/sync` | v0.24.0 | BSD-3-Clause | indirect |

One existing module moved: `github.com/clipperhouse/uax29/v2` v2.2.0 → v2.7.0 (MIT). It is the one change linked into `cmd/hardn` today, through go-runewidth; see [side-effects.md](../refactor/phase0/side-effects.md) §13.1.

### 2.3 Facts that shape the design

Checked against the module source at the versions above; citations are `file:line` in that module.

| Topic | Fact | What hardn does |
|---|---|---|
| Colour fallback | `Style.Render` emits every colour exactly as set (lipgloss `style.go:353-371`); it never downsamples. Downsampling happens only in `colorprofile.Writer` (colorprofile `writer.go:38-51`): TrueColor passes through, NoTTY strips every escape sequence, and ASCII, ANSI and ANSI256 re-encode each SGR (`writer.go:87-171`), dropping colours below ANSI and converting the rest with `Profile.Convert`, which keeps an indexed colour at ANSI256 and a basic one at ANSI (`profile.go:58-108`). The writer parses each `Write` on its own (`writer.go:54-80`). | The theme picks each token's value for the profile with `lipgloss.Complete` (`color.go:265-277`; `NoColor` at ASCII and NoTTY), so the deliberate 256- and 16-colour picks pass through the writer unchanged (`TestWriterKeepsPicks`). All output goes through `term.Env.Writer`, a `colorprofile.Writer` with the profile fixed, written one whole string at a time. |
| Lip Gloss's own printers | `Print*` write through `lipgloss.Writer`, created at init for `os.Stdout` (`writer.go:14`); `Sprint*` use that writer's profile (`writer.go:110-160`); `Fprint*` detect a profile again on every call (`writer.go:67-97`). | Never used: they ignore `term.Options`. |
| `NO_COLOR` | colorprofile parses `NO_COLOR`, `CLICOLOR` and `CLICOLOR_FORCE` with `strconv.ParseBool` (`env.go:115-128`), so `NO_COLOR=0` or `NO_COLOR=yes` leave colour on. A true `NO_COLOR` caps the profile at ASCII only for a terminal (`env.go:86-91`), so for output that is not a terminal a true `CLICOLOR_FORCE` raises the profile to at least ANSI, and to whatever `TERM`, `COLORTERM` and the like give when that is higher (`env.go:93-101`), and `Detect` returns that profile whatever `NO_COLOR` says (`env.go:40-43`). | `term.Resolve` follows no-color.org: any non-empty `NO_COLOR` turns colour off. It decides `NO_COLOR`, `TERM=dumb` and the TTY check before asking `colorprofile.Env`, so `CLICOLOR_FORCE` cannot colour piped output or a dumb terminal (§3). |
| Import side effect | Linking the root package runs `var Writer = colorprofile.NewWriter(os.Stdout, os.Environ())` at init (lipgloss `writer.go:14`), which calls `Detect` (colorprofile `writer.go:23-28`): an `ioctl` on stdout and then, on a terminal with `TERM` set and not `dumb`, unless the environment alone gives TrueColor or a true `NO_COLOR`, a terminfo read and, with `$TMUX` set, `tmux info` run with no timeout (`env.go:33-54`, `208-262`). x/ansi reads `RUNEWIDTH_EASTASIAN` at init (`method.go:20-25`). Nothing at init writes a file or changes a terminal mode. | The init-time profile is ignored. Today only the `pkg/ui` test binaries run these effects. Full inventory, including `TTY_FORCE` and the `/etc/passwd` read in cgo-less builds: [side-effects.md](../refactor/phase0/side-effects.md) §13.1. |
| Background detection | `lipgloss.HasDarkBackground` puts the terminal in raw mode with ISIG, ICANON and ECHO off (lipgloss `query.go:13`; x/term `term_unix.go:31`), sends an OSC 11 and a DA1 request (`terminal.go:45`) and waits up to 2 s for the reply on stdin and, if that fails, another 2 s on stdout (`terminal.go:49`, `query.go:60-64`): about 4 s in all. Meanwhile Ctrl-C does not interrupt, type-ahead is read and discarded, and a late reply is left in stdin. It reports dark on any error (`query.go:86-91`). The `compat` package runs this query, and a second `Detect`, at import (`compat/color.go:13`, `16`). | Opt-in (an adjustment to the brief's "detect the background where possible"): dark by default; `term.Detect` queries once, only for theme `auto` with stdin and stdout both terminals and plain mode off. `compat` must never be imported (depguard rule in `next/p1d-lint`). |
| Width | `lipgloss.Width` is the widest line's `ansi.StringWidth` (lipgloss `size.go:15-24`). x/ansi measures grapheme clusters with East Asian width off (`method.go:16-18`, `parser_decode.go:442`), so ambiguous-width characters count as one cell. The only switch is `RUNEWIDTH_EASTASIAN`, read once at init (`method.go:20-25`); the locale is not consulted. Control characters count as no cells (`width.go:79-119`), and `Style.Render` turns a tab into 4 spaces by default (`style.go:14`, `528-540`). | A terminal that draws ambiguous characters two cells wide draws ⛛ ⭘ ⛬ ≈ … █ ─ wider than Lip Gloss measures (§6). `term.Resolve` picks ASCII glyphs in zh, ja and ko locales; `HARDN_ASCII=1` covers other cases. `uitest.Golden` stops if x/ansi measures ─ as two cells, and fails a line that holds a tab. |
| Underline | An underlined style renders one SGR per rune (lipgloss `style.go:310`, `318`, `428-436`) and repeats the `4` attribute (`style.go:340` and `384`, with x/ansi `style.go:103-104`). | The second `4` sets the same attribute again. The cost is verbose goldens for underlined text (`Menu.Heading` in `swatch.golden` and the `menuheading` goldens), accepted for `MenuHeading` (§9.4): the terminal draws one unbroken underline, spaces included, as Figma's PNG does. `UnderlineSpaces(false)` would only take the underline off the spaces; every rune would still be styled alone (`style.go:318`). |
| table, list, tree | `table.New` draws a box-drawing border on every side and wraps cells (lipgloss `table/table.go:80-93`); each border can be switched off (`table.go:166-212`). It always truncates headers to one line, whatever `Wrap` says (`table.go:436-437`, `561-564`), and with `Wrap(false)` it truncates cells too (`table.go:509-510`); both use a fixed `…` (`table.go:567`), and only when the text is wider than its column (x/ansi `truncate.go:67-69`). A height-limited table marks hidden rows with `…` (`table.go:503`). Even with every border off, each cell is padded to its column's width (`table.go:512-518`) and the cells are joined with `JoinHorizontal` (`table.go:534`), so a value shorter than the last column ends in spaces. `list.New` uses the `•` bullet (`list/list.go:50-53`, `list/enumerator.go:105`). The default tree branches are `├──` and `└──` with a `│` indent (`tree/enumerator.go:23-28`, `69-74`). All of `• … ├ └ │ ─` are East Asian ambiguous width. | For the two-column rows (StatusRow, Menu, KeyValue) the refactor plan proposed a table with every border off, or a list with an enumerator from `theme.Glyphs`. In ASCII mode the bullet, box drawing and `…` must come from `Glyphs`, so there a table must never truncate: no `Height`, no header wider than its column, and no cell wider than its column under `Wrap(false)`. A component that needs to shorten text does it itself with `Glyphs.Ellipsis` before handing the text to the table. Decided per component in `next/p1b-*`. The header, status, menu and block groups use none of them: their label and value rows would end in the table's padding, which the output rules forbid, so `columns` places the values with spaces (§9.2 to §9.5). A list pads an enumerator on the left only up to a wider sibling's (`tree/renderer.go:79-81`, `100-103`) and gives each one `PaddingRight(1)` unless `EnumeratorStyle` replaces it (`tree/renderer.go:24-26`). The status markers of one glyph set share a width, so a list whose `Enumerator` returns the marker already centred puts the glyph at col 3; it still adds nothing to a status row, which needs the label and value columns, the stacking and the cut (§9.3). For the same reason a list right-aligns menu keys as `Menu` wants (" 1)", "10)"), but it joins enumerator and item with `JoinHorizontal` (`tree/renderer.go:126-131`), so an item of more than one line, stacked or with a note line, ends in spaces (§9.4). The same holds for `BulletList`: its enumerator could be the bullet, or a state's marker per row (`list/list.go:143`), but a stacked row or a wrapped bullet item ends in spaces (§9.5). The plan group's changes and steps are evaluated in §9.6: a change is many lines, and a step's glyphs differ in width in ASCII. |
| Wrap | `lipgloss.Wrap` (`wrap.go:12-19`) wraps with x/ansi's `Wrap` (x/ansi `wrap.go:276-463`): at spaces and hyphens, breaking a word longer than the line. A style's `Width` wraps the same way (`style.go:412-414`) and then pads every line to the width (`style.go:489-497`, `align.go:43-48`). | Prose (a `Message`'s text, a `Callout`'s body, a bullet item) wraps with `lipgloss.Wrap` on plain text, each line trimmed and styled alone (§9.5); `Width` is not used. Code (a plan's lines, commands, captured output) wraps with its own loop over `ansi.Truncate`: x/ansi's `Hardwrap` and `Wrap` break every line at one limit (x/ansi `wrap.go:21-31`, `276-278`), where a piece after the first has a cell less for `Glyphs.Wrap`, and `Wrap` always breaks at a hyphen (`wrap.go:270-271`), which splits options such as `--unit=` (§9.6). |
| Join and place | `JoinHorizontal` pads every line of each block to the block's width with plain spaces (lipgloss `join.go:88`); `PlaceHorizontal` pads to the whole width it is given (`position.go:43-64`). | Both leave trailing spaces after the last cell, so components place their columns with spaces up to the grid column instead (§9.2). |

## 3. Render environment

`pkg/ui/term` builds one `term.Env` at startup and every renderer takes it. Nothing else inspects the terminal or the environment.

| Field | Meaning |
|---|---|
| `Width`, `Height` | Terminal size in cells |
| `Profile` | The `colorprofile.Profile` that `Env.Writer` downsamples to: TrueColor, ANSI256, ANSI (16 colours), ASCII (no colour; bold and underline kept) or NoTTY (no escape sequences at all) |
| `Unicode` | The Unicode glyph set; false selects ASCII |
| `TTY`, `InputTTY` | stdout and stdin are terminals |
| `Dark` | The dark theme; false selects the light theme |
| `Theme` | The theme mode Resolve settled on: `ThemeDark`, `ThemeLight` or `ThemeAuto` |
| `Plain` | Plain mode: ASCII glyphs, no animation and, unless `--color=always`, no colour or attributes |
| `Animate` | Spinners and in-place updates are allowed |

`Env.HasColor` reports a profile of ANSI or better. `Env.Writer(w)` returns a `colorprofile.Writer` around `w` with the profile fixed; the profile was resolved for stdout, so it wraps stdout.

**Precedence** (`term.Resolve`, a pure function of `term.Options` and the observed `term.Facts`). An option beats an environment variable, which beats detection. A boolean variable is true for `1`, `true`, `yes` or `on`, in any case.

| Field | Rule, first match wins |
|---|---|
| `Plain` | `Options.Plain`, or `HARDN_PLAIN` true, or stdout is not a terminal |
| `Profile`, `--color=never` | NoTTY when `Plain`, else ASCII |
| `Profile`, `--color=always` | `colorprofile.Env` with `NO_COLOR` removed from the environment, raised to at least ANSI, even when `Plain` |
| `Profile`, auto or unset | NoTTY when `Plain`; ASCII when `NO_COLOR` is non-empty or `TERM=dumb`; else `colorprofile.Env`, which reads only the environment (`TERM`, `COLORTERM`, `CLICOLOR`, `CLICOLOR_FORCE`, `WT_SESSION`, `GOOGLE_CLOUD_SHELL`; no isatty, terminfo or tmux) |
| `Unicode` | false with `Options.ASCII`, `HARDN_ASCII` true or `Plain`; false when `TERM` is `linux` or `dumb`; false when the effective locale (first non-empty of `LC_ALL`, `LC_CTYPE`, `LANG`) has no UTF-8 codeset (`.UTF-8`, `.utf8`, any case; a bare `UTF-8` counts) or its language is `zh`, `ja` or `ko`; else true |
| `Theme` | `Options.Theme`, else a valid `HARDN_THEME` (an invalid value is ignored), else dark. `Dark` is false only for light |
| `Width`, `Height` | Each on its own: the terminal size when stdout is a terminal and the size is known, else `COLUMNS` / `LINES` when a positive integer, else `DefaultWidth` 80 / `DefaultHeight` 24 |
| `TTY`, `InputTTY` | As observed |
| `Animate` | stdout is a terminal, not `Plain`, and `TERM` is set, non-empty and not `dumb` |

`term.Detect` observes the process (isatty on stdin and stdout, the terminal size, `os.Environ`), calls `Resolve`, and only then, when `Theme` is `ThemeAuto`, both streams are terminals and the Env is not plain, calls `lipgloss.HasDarkBackground` once (§2.3). `term.Fixed(width, profile, unicode, dark)` builds an Env for tests and goldens without reading anything: height 24, no animation, and plain with no TTY when the profile is NoTTY or lower.

**Flags and variables waiting for §3.14.** Nothing is wired: no command calls `term.Detect`, so the binary reads none of these yet. Once the maintainer approves refactor plan §3.14 (and §3.11 for the alias), they map onto `term.Options` and the variables `Resolve` already reads:

| Option field | Flag | Variable |
|---|---|---|
| `Color` (`term.ParseColorMode`) | `--color=auto\|always\|never`; the existing `--no-color` becomes an alias of `--color=never` with a deprecation warning (§3.11) | `NO_COLOR` (standard) |
| `ASCII` | `--ascii` | `HARDN_ASCII` |
| `Plain` | `--plain` | `HARDN_PLAIN` |
| `Theme` (`term.ParseThemeMode`) | `--theme=dark\|light\|auto` | `HARDN_THEME` |

## 4. Tokens

The export is a custom fig-extract format (inferred tokens; no W3C `$value`/`$type`, no Figma variables): see [phase0-findings.md](../refactor/phase0-findings.md#81-figma-export) §8.1. The Go side is the `palette` struct in `pkg/ui/theme/palette.go`: one `token{hex, ansi256, ansi}` per semantic name, in `darkPalette` and `lightPalette`. `theme.New(env)` picks the palette by `env.Dark` and each colour by `env.Profile`.

### 4.1 Mapping from the export

Every colour token of the export, in its order. 256 is the xterm index; 16 is the terminal colour (`lipgloss.White` is 7, `BrightBlack` 8, and so on). **Bold** marks a logged departure: a lifted value, a 256-colour override of the nearest-index rule (§4.2), or DEV-004's 16-colour bar.

| Export name | Export id | Hex | Go token | Dark truecolor | 256 | 16 | Light truecolor / 256 / 16 | Deviation, drift |
|---|---|---|---|---|---|---|---|---|
| `color.text.muted` | `text:#81869C` | #81869C | `text.muted` | #81869C | 103 | 8 BrightBlack | #575C6E / 59 / 8 BrightBlack | light DEV-011 |
| `color.text.primary` | `text:#C0C4D8` | #C0C4D8 | `text.primary` | #C0C4D8 | 251 | 7 White | #1E2230 / 235 / 0 Black | light DEV-011 |
| `color.status.ok` | `text:#70D47F` | #70D47F | `status.ok` | #70D47F | 78 | 10 BrightGreen | #1A7F37 / 22 / 2 Green | light DEV-011 |
| `color.status.err` | `text:#E74669` | #E74669 | `status.bad` (renamed) | #E74669 | 204 | 9 BrightRed | #C2254A / 161 / 1 Red | light DEV-011 |
| `color.text.dim` | `text:#2B2F3B` | #2B2F3B | `text.faint` (renamed) | **#565B6B** | **59** | 8 BrightBlack | #81869C / 103 / 8 BrightBlack | DEV-001, DEV-009; light DEV-011 |
| `color.status.warn` | `text:#DBB853` | #DBB853 | `status.warn` | #DBB853 | 179 | 3 Yellow | #8A6100 / 94 / 3 Yellow | light DEV-011 (its 16-colour value is the one contrast exception) |
| `color.text.input` | `text:#DFDFDF` | #DFDFDF | `badge.fg` (merged) | #DFDFDF | 254 | 15 BrightWhite | #FFFFFF / 231 / 15 BrightWhite | DEV-003; D3, D4: used only on badges, merged with the host badge's #C0C4D8 text; echoed input is `text.muted` |
| `color.surface.base` | `surface:#000000` | #000000 | none (dropped) | | | | | D8: the terminal's own background, never painted. Dark contrast is measured against it |
| `color.surface.raised` | `surface:#101114` | #101114 | `bar.bg` | #101114 | 233 | **8 BrightBlack** | #E6E8EE / 254 / 7 White | DEV-004, DEV-005; D7: one token for the bar, section stub and version box |
| `color.cursor` | `surface:#C0C4D8` | #C0C4D8 | none (no token) | | | | | D5; candidate C8, open until the prompt component lands: the terminal draws its own cursor |
| `color.badge.host` | `surface:#0C2AC0` | #0C2AC0 | `badge.bg` | #0C2AC0 | 19 | 4 Blue | #0C2AC0 / 19 / 4 Blue | light DEV-011 |
| `color.bar.warn` | `surface:#D2BC13` | #D2BC13 | `accent.warn` | #D2BC13 | 178 | 3 Yellow | #B88A00 / 136 / 3 Yellow | D14: a pair with `status.warn`, kept separate; light DEV-011 |
| `color.bar.ok` | `surface:#008A00` | #008A00 | `accent.ok` | #008A00 | 28 | 2 Green | #008A00 / 28 / 2 Green | D14: a pair with `status.ok`, kept separate; light DEV-011 |
| `color.border.divider` | `border:#16181D` | #16181D | `divider` | **#565B6B** | **59** | 8 BrightBlack | #81869C / 103 / 8 BrightBlack | DEV-002; D6 |

**A use mapped to a different Go token, and a value outside the token export:**
- The host badge text uses the export's `color.text.primary` (#C0C4D8). That use maps to `badge.fg`, not `text.primary` (drift D3, DEV-003).
- The main-menu panel outline, #17181C, sits in a collapsed SVG in the export and is not an export token. It gets no Go token (DEV-002); whether the outline is drawn stays open (candidate C9).

**Go tokens with no export value:**
- `status.unknown`: dark #6AB0D8 / 74 / 14 BrightCyan, light #0A6E8C / 24 / 6 Cyan (DEV-010).
- Every light value (DEV-011): Figma has no light theme.

### 4.2 Rules the tests hold

- **256 colours:** the xterm index from 16 to 255 nearest the hex by CIE76 ΔE; indexes 0-15 are never used, because they are the user's own 16 colours. The only overrides are dark `text.faint` and `divider`: 59 instead of the nearest, 240, which is under 3:1 (DEV-001). `TestANSI256Nearest`.
- **16 colours:** picked by hand, never by distance, because terminals redefine them. Muted and faint share BrightBlack in both themes. `TestDeliberateANSIPicks` pins the dark picks DEV-001 to DEV-004 record; every other 16-colour pick, including DEV-010's and DEV-011's, is held by the swatch golden (§8), and its contrast floor by `TestContrast`.
- **Contrast** (WCAG, against #000000 for dark and #FFFFFF for light, at truecolor, 256 and 16 colours; 16 colours in x/ansi's RGB table, `color.go:269-284`, standing in for the user's palette). `TestContrast`:

| Token | Dark floor | Light floor |
|---|---|---|
| `text.primary` | 7:1 | 4.5:1 |
| `text.muted` | 4.5:1 | 3:1 |
| `text.faint`, `divider`, `accent.ok`, `accent.warn` | 3:1 | 3:1 |
| `status.ok`, `status.bad`, `status.warn`, `status.unknown` | 4.5:1 | 4.5:1 |
| `badge.fg` on `badge.bg` | 4.5:1 | 4.5:1 |
| `badge.bg`, `bar.bg` | none (surfaces) | none |

One exception: light `status.warn` at 16 colours, Yellow, is 4.2:1 (DEV-011).

### 4.3 Where each token is used

`theme.New` builds 104 style slots, grouped by component (`theme.Theme`). Slots carry only colours and text attributes (today bold and underline); layout belongs to the components.

| Token | Slots |
|---|---|
| `text.primary` | `Text.Primary`, `Header.Wordmark`, `TitleBar.Title`, `Risk.Label`, `Status.Label`, `Menu.Key`, `Menu.Label`, `Field.Number`, `Field.Label`, `Field.Hint`, `Message.Info`, `Callout.Info.Title`, `KeyValue.Key`, `Breadcrumb.Current`, `Plan.Title`, `Step.Running`, `Step.Title`, `Confirm.Text`, `Confirm.Command` |
| `text.muted` | `Text.Muted`, `Header.Version`, `ContextBar.Name`, `ContextBar.Version`, `Host.Address`, `Risk.Factor`, `Status.NotApplicable`, `Mode.Label`, `Menu.Heading`, `Menu.Description`, `Menu.ExitKey`, `Menu.Exit`, `Prompt.Glyph`, `Prompt.Label`, `Prompt.Hint`, `Field.Answer`, `Message.Text`, `Callout.Body`, `KeyValue.Bullet`, `KeyValue.Value`, `KeyValue.ValueNotApplicable`, `Section.Title`, `Breadcrumb.Item`, `Plan.Target`, `Plan.Context`, `Plan.Command`, `Step.Pending`, `Step.Skipped`, `Output` |
| `text.faint` | `Text.Faint`, `Header.URL`, `ContextBar.Codename`, `Host.Uptime`, `Risk.Gloss`, `Status.Detail`, `Menu.Aside`, `Menu.Disabled`, `Breadcrumb.Separator`, `Plan.Meta`, `Step.Detail` |
| `status.ok` | `Risk.Low`, `Status.Configured`, `Mode.DryRun`, `Message.OK`, `Callout.OK.Title`, `KeyValue.ValueOK`, `Plan.Added`, `Step.Done` |
| `status.bad` | `Risk.High`, `Status.NotConfigured`, `Message.Bad`, `Callout.Bad.Title`, `KeyValue.ValueBad`, `Plan.Removed`, `Step.Failed`, `Confirm.Urgent` |
| `status.warn` | `Host.Reboot`, `Risk.Moderate`, `Status.Partial`, `Mode.Live`, `Menu.Risk`, `Message.Warn`, `Message.Cancelled`, `Callout.Warn.Title`, `Callout.Cancelled.Title`, `KeyValue.ValueWarn`, `Plan.LockoutRisk`, `Step.RolledBack`, `Confirm.Waiting` |
| `status.unknown` | `ContextBar.Unknown`, `Host.Unknown`, `Risk.Unknown`, `Status.Unknown`, `KeyValue.ValueUnknown` |
| `badge.fg`, `badge.bg` | `Badge` |
| `bar.bg` (background) | `ContextBar.Fill`, `TitleBar.Fill`, `Section.Fill`; the `Header.Version` box above 16 colours only (DEV-005) |
| `accent.ok` (background) | `Callout.OK.Bar` |
| `accent.warn` (background) | `Callout.Warn.Bar`, `Callout.Cancelled.Bar` |
| `status.bad`, `text.muted` (background) | `Callout.Bad.Bar`, `Callout.Info.Bar`: kinds Figma does not draw, from existing tokens (DEV-032) |
| `divider` | `Divider`, `Section.Rule` |

Slots for states Figma does not draw (risk bands other than Moderate, Partial, messages, plan and step states, and others) are DEV-016; the header group's `ContextBar.Unknown`, `Host.Unknown` and `Risk.Factor` are DEV-022; the block group's `Callout.Bad`, `Callout.Info`, `KeyValue.ValueUnknown` and `KeyValue.ValueNotApplicable` are DEV-032 and DEV-034; the plan group's `Plan.Title`, `Plan.Target`, `Plan.Meta`, `Plan.Command`, `Step.Skipped`, `Step.Title`, `Step.Detail`, `Output` and `Confirm` are DEV-038 to DEV-041, all from existing tokens. `Field` styles the answered form field (`next/p1b-blocks`); its answer slot was `Prompt.Input` until then. The slots still missing, a form field's description and inline error for `next/p1c-form`, are listed on `theme.Theme`.

## 5. Typography

Terminals have one cell size, so the export's type styles map onto bold, underline and normal weight (DEV-006):

| Export style | Font | Terminal | Slots |
|---|---|---|---|
| `type.body` | Regular, 32 px | normal | every slot not listed below |
| `type.body.strong` | Bold, 32 px | bold | `ContextBar.Name`, `TitleBar.Title`, `Section.Title`, `Risk.Label`, `Menu.Key`, `Menu.ExitKey`, `Prompt.Label`, `Callout.OK.Title`, `Callout.Warn.Title`, `Callout.Cancelled.Title` |
| `type.glyph` | Bold, 24 px (the `❯`) | bold | `Prompt.Glyph` |
| `type.header.strong` | Bold, 28 px (one-off: wordmark, host badge; the user badge is `type.body.strong`) | bold | `Header.Wordmark`, `Badge` |
| `type.header` | Regular, 28 px (one-off: version, URL) | normal | `Header.Version`, `Header.URL` |
| not in the export (seen in the PNGs) | underline on "Select an option" | underline | `Menu.Heading` |
| no Figma value (DEV-016) | | bold | `Menu.Risk`, `Message.OK`, `Message.Warn`, `Message.Bad`, `Message.Info`, `Message.Cancelled`, `Callout.Bad.Title`, `Callout.Info.Title` (DEV-032), `Breadcrumb.Current`, `Plan.LockoutRisk`, `Confirm.Waiting`, `Confirm.Urgent`, `Confirm.Command` (DEV-041) |

That is 27 typography roles: 26 bold slots and one underlined. `TestTypography` pins exactly this set in every environment, and the swatch golden shows each one (§8). Letter spacing (0 and −0.5% within one style, drift D2) is dropped (DEV-007). Muted and faint are colours, never SGR 2 (DEV-009, `TestSlotsSetWithoutBannedAttributes`). At the ASCII profile the colours drop and bold and underline stay; at NoTTY the writer strips every escape sequence, attributes included.

## 6. Glyph sets

`theme.Glyphs` names each glyph by role. `theme.New` takes the Unicode set when `env.Unicode` is true and the ASCII set otherwise (§3 for the rule). Widths are as Lip Gloss measures them, and in brackets as a terminal draws them when it draws East Asian ambiguous characters two cells wide; the class is from `golang.org/x/text/width` (A ambiguous, N neutral, Na narrow).

| Role | Unicode | Code point, class | Width | ASCII | Width | Source |
|---|---|---|---|---|---|---|
| `Configured` | ⛛ | U+26DB, A | 1 (2) | `[+]` | 3 | Figma; ASCII DEV-012 |
| `NotConfigured` | ⭘ | U+2B58, A | 1 (2) | `[ ]` | 3 | Figma; DEV-012 |
| `Partial` | ⛬ | U+26EC, A | 1 (2) | `[*]` | 3 | Figma; DEV-012 |
| `Unknown` | `?` | U+003F, Na | 1 | `[?]` | 3 | DEV-013 |
| `NotApplicable` | ∅ | U+2205, N | 1 | `[-]` | 3 | DEV-014 |
| `Risk` | ⛬ | U+26EC, A | 1 (2) | `[*]` | 3 | Figma (shared with `Partial`); DEV-012 |
| `Prompt` | ❯ | U+276F, N | 1 | `>` | 1 | Figma; DEV-012 |
| `Approx` | ≈ | U+2248, A | 1 (2) | `~` | 1 | Figma; DEV-012 |
| `Bullet` | `-` | U+002D, Na | 1 | `-` | 1 | Figma |
| `Separator` | › | U+203A, N | 1 | `>` | 1 | DEV-015 |
| `Ellipsis` | … | U+2026, A | 1 (2) | `...` | 3 | DEV-015 |
| `Bar` | █ | U+2588, A | 1 (2) | `#` | 1 | DEV-015 |
| `Rule` | ─ | U+2500, A | 1 (2) | `-` | 1 | DEV-015 |
| `BadgeOpen`, `BadgeClose` | `[` `]` | U+005B, U+005D, Na | 1 each | `[` `]` | 1 each | DEV-019: a badge with no colour to paint it |
| `OK` | ✓ | U+2713, N | 1 | `[+]` | 3 | origin/main's success mark; DEV-031 |
| `Warn` | ⛬ | U+26EC, A | 1 (2) | `[*]` | 3 | Figma's warning marker (shared with `Partial`); DEV-031 |
| `Bad` | ✗ | U+2717, N | 1 | `[!]` | 3 | origin/main's failure mark; DEV-031 |
| `Info` | ℹ | U+2139, N | 1 | `[i]` | 3 | origin/main's information mark; DEV-031 |
| `Cancelled` | ⊘ | U+2298, N | 1 | `[-]` | 3 | DEV-031 (ASCII shared with `NotApplicable`) |
| `Pending` | ◦ | U+25E6, N | 1 | `[ ]` | 3 | DEV-039 (ASCII shared with `NotConfigured`: an unticked box) |
| `Running` | ▸ | U+25B8, N | 1 | `[>]` | 3 | DEV-039: the running step when nothing animates |
| `Spinner` | ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ | U+280B and others, N | 1 per frame | `\|/-\` | 1 per frame | DEV-039: one frame per rune, cycled by the caller |
| `RolledBack` | ↺ | U+21BA, N | 1 | `[<]` | 3 | DEV-039 |
| `Skipped` | ↷ | U+21B7, N | 1 | `[-]` | 3 | DEV-039 (ASCII shared with `NotApplicable`) |
| `Command` | `$` | U+0024, Na | 1 | `$` | 1 | DEV-038: before a command hardn runs |
| `Wrap` | ↪ | U+21AA, N | 1 | `>` | 1 | DEV-038: starts the rest of a code line too long for its line |
| `Urgent` | `!` | U+0021, Na | 1 | `[!]` | 3 | DEV-041 (ASCII shared with `Bad`) |

- The status markers and the risk icon fit the three-cell marker column even when drawn two cells wide (`TestStatusMarkers`), and no two states share a marker. The same holds for a message's five kinds (`TestMessageMarkers`), whose glyphs a `Callout` also draws before its title (DEV-032); a `KeyValue`'s states take the status markers (DEV-034). A step's six states (`OK` and `Bad` for done and failed), each spinner frame and the countdown's `Urgent` fit it too, and no two step states share a glyph (`TestStepMarkers`).
- `Bar` and `Rule` are drawn in runs, so in an ambiguous-wide terminal a 64-cell rule takes 128 cells and wraps at 80 columns (accepted in DEV-015). `term.Resolve` picks ASCII in zh, ja and ko locales; elsewhere `HARDN_ASCII=1` avoids it.
- `TestAmbiguousGlyphs` keeps the ambiguous list in the `Glyphs` doc comment in step with the Unicode set; `TestGlyphSetsComplete` checks that the ASCII set is printable ASCII.

## 7. Grid and spacing

Constants in `pkg/ui/theme/grid.go`, in cells and lines, counted from column 0 at the left edge of the content (DEV-008). The export's pixels were converted by role, not divided: a cell is 19.2 px there.

| Constant | Value | Meaning |
|---|---|---|
| `ContentWidth` | 64 | Figma's content box; a narrower terminal uses its own width |
| `Indent` | 2 | Left indent of rows, headings and the prompt; bars and the header start at column 0 |
| `MarkerCol`, `MarkerWidth` | 2, 3 | The marker column: a state glyph, centred (a one-cell glyph at column 3, DEV-020), or a menu key such as `10)`, right-aligned |
| `LabelCol` | 6 | Where a row's label starts |
| `LabelWidth` | 21 | The label column, including the gap before the value |
| `ValueCol` | 27 | Where a row's value, state or description starts, on every screen |
| `StackCol` | 6 | Where a stacked row's value starts, under the label (DEV-021) |
| `SectionGap` | 1 | Blank lines between sections |
| `PromptGap` | 1 | Blank lines before the prompt (drift D11) |

```text
0 2   6                    27
  [+] SSH Port             Configured non-default (2230)
   1) User Management      Create, Configure
  10) Logs                 View log file
```

**Export spacing and sizes:**
- `space.gutter` (17 px) and the unnamed 18 px gap are about one cell; merged (drift D1).
- `space.hairline` (1 px) and the 3, 4 and 10 px gaps are under one cell: dropped. The exception is the knockout's 10 px right padding, which becomes one cell like the 17 px padding elsewhere (drift D12).
- Row heights (`size.row.data` 36 px, `size.row.menu` 38 px, `size.prompt` 39 px, 40 px) each hold one line of text: one terminal line.
- The 1 px strokes (`border.width`) become a line of `Glyphs.Rule` in the `divider` colour (DEV-002, DEV-015).
- The alignment offsets (227, 247, 302, 342 and 440 px) position columns, which drift by frame (drift D9). `LabelCol` and `ValueCol` replace them (DEV-008).
- The header and the bars start at column 0, where the export has the header badge at x 80 and the bar at x 87 (drift D13).

**Width and height rules, pending maintainer approval (refactor plan §3.15).** The brief proposes the first three; the height budget is the plan's addition, for the main menu of about 36 lines on an 80×24 console. Each is one constant, so a refusal changes only the constants and the goldens: the component tests take their limits from the constants, and with `CapContent` or `BarsFillContent` false, or `StackBelow` 0, only `TestGolden` fails (checked with an overlay of grid.go).

| Constant | Value | Rule |
|---|---|---|
| `CapContent` | true | Lines use at most `ContentWidth` cells on a wider terminal |
| `StackBelow` | `ContentWidth` (64) | Below this width a two-column row stacks: label, then the value under it |
| `BarsFillContent` | true | Bars and section rules fill `ContentWidth`, not the terminal |
| `CompactBelow` | 40 | Below this height the main menu renders in its compact form |
| `CompactBudget` | `term.DefaultHeight` (24) | The most lines the compact main menu may use, prompt included |

`CapContent`, `StackBelow` and `BarsFillContent` are applied by the components from `next/p1b-header` on; `CompactBelow` and `CompactBudget` wait for the main-menu layout (`next/p1d-slice`).

`StackBelow` is `ContentWidth`, within the brief's "below about 70 columns". It is keyed on the terminal's width, and from 64 columns up `CapContent` makes the content box 64 cells, as at 80, so a row that fits side by side at 80 columns fits from 64 up. At 70, as first set, the header, HostBlock and RiskSummary stacked from 64 to 69 columns although their side-by-side lines fit, using 7 lines where 4 do, and Figma's seven status rows 14 lines where 7 do; `next/p1b-menu` lowered it to 64, and no golden changed (DEV-021). At 60 columns stacking is needed for some rows, not all: Figma's widest status row is 56 cells, so its seven rows would fit side by side there too, and stacking them adds 7 lines against the compact menu's budget of 24 (`CompactBudget`). Figma's menu is 23 lines stacked at 60 columns and 12 side by side; of its lines only item 1's, 61 cells, would not fit 60 columns (DEV-021).

## 8. Golden tests

Goldens lock in hardn's own rendering to catch regressions. They do not measure conformance to Figma; update them deliberately and give the reason in the review description.

**Matrices** (`pkg/ui/uitest`, from brief Phase 6):
- `uitest.Full()`, for components: 48 cells, colour {truecolor, 256, 16, none} × background {dark, light} × glyphs {unicode, ascii} × width {60, 80, 120}, the first axis varying slowest. Cells are named after their values, such as `truecolor-dark-unicode-80` or `none-light-ascii-60`. "none" is the ASCII profile: no colour, bold and underline kept.
- `uitest.Core()`, for screens and workflow steps: `truecolor-dark-unicode-80`, `plain-80` (plain mode: NoTTY, ASCII glyphs, no escape sequences) and `truecolor-dark-unicode-60`.

**Writing a golden test:** `uitest.Golden(t, "<case>", uitest.Full(), func(env term.Env) string { ... })`. For each cell it renders, writes the result through `env.Writer` in one write, checks every line against the cell's width (`uitest.AssertFits`), and compares with `testdata/golden/<case>.golden` in the test's package directory.

**Files today:**
- `pkg/ui/theme/testdata/golden/swatch.golden`: `TestSwatch` on `Full()`. Every token's name in its colour and as a background, every glyph by role, and the 27 typography roles.
- `pkg/ui/uitest/testdata/golden/harness.golden`: the harness's own check, one render at each profile and in plain mode, at width 20.
- `pkg/ui/component/testdata/golden/<component>/<state>.golden`: `TestGolden` on `Full()`, one file per component state (§9.2 to §9.6).

**Running and regenerating:**
- `make check` runs every golden in its `check-test` step, and fails on a mismatch.
- `make golden-update` rewrites the goldens of every `pkg/ui` package whose test binary links `uitest`, after an intended change. Review the diff, then commit it with the reason.
- One package: `go test ./pkg/ui/theme -update`. The make target also sets `GOTOOLCHAIN=local`, `GOWORK=off` and `-mod=readonly`.
- `-update` is defined once, in `uitest`. A package that uses `uitest` must not define its own `-update` or link `github.com/charmbracelet/x/exp/golden`, which defines one too; the flag package panics on the second definition. Packages that do not link `uitest` reject `-update`, which is why `make golden-update` selects packages instead of passing it to `./pkg/ui/...`.

**Notation.** A golden file starts with a three-line `#` header naming the test. Each cell is a section: a line `=== <cell>: <env>`, where the env text describes the whole `term.Env` (width, height, profile, background, glyphs, plain mode, and any field that differs from `term.Fixed`), then the output as the terminal receives it:
- control characters other than newline are Unicode Control Pictures (U+2400 plus the code; DEL is ␡), so ESC is the single character ␛;
- every output line ends with `$`, so trailing spaces stay visible, and header lines never do.

Golden rejects output it could not show unambiguously: invalid UTF-8, C1 controls, and the Control Pictures themselves (U+2400 to U+2421). Reading the SGR: `␛[38;2;R;G;Bm` and `␛[48;2;R;G;Bm` are truecolor foreground and background, `38;5;N` and `48;5;N` the 256-colour index, `30`-`37` and `90`-`97` the 16-colour foreground (`40`-`47` and `100`-`107` background), `1` bold, `4` underline, `␛[m` reset.

**Guards:** each cell renders twice and the two results must match; Golden stops if `RUNEWIDTH_EASTASIAN` makes x/ansi measure ─ as two cells; a line wider than the cell or holding a tab fails. On a mismatch the report names the changed, added and removed cells and shows a line diff of each, numbered by line of the golden file.

## 9. Components, screens and usage rules

### 9.1 Component conventions

`pkg/ui/component` holds the components; its package doc is the full statement of these rules, which every `next/p1b-*` unit follows.
- **Render context:** `component.NewCtx(env)` builds a `Ctx` holding the `term.Env` and the `theme.Theme` built once from it. The brief's `Render(vm, env)` is `vm.Render(ctx)`.
- **View-models** are exported structs of plain data (no funcs, actions or I/O handles) with a pure value method `Render(c Ctx) string`.
- **Output:** no line wider than the content width, `min(Env.Width, ContentWidth)`, which a bar fills; if `BarsFillContent` is refused, a bar fills the terminal's width instead. No trailing newline; no trailing space outside a painted background. The caller joins blocks with `SectionGap` blank lines.
- **Grid:** rows at `Indent`, the header and bars at column 0; a state glyph centred in the marker column (DEV-020); labels at `LabelCol`, values at `ValueCol`. Below `StackBelow` two-column layouts stack, values at `StackCol` (DEV-021).
- **Text:** view-model strings are shown as one line of plain text (escape sequences removed, control characters dropped or turned into spaces, trimmed), measured with `lipgloss.Width`, and cut with `Glyphs.Ellipsis`. Only prose wraps, at spaces, under its own first line: a `Message`'s text, a `Callout`'s body and a bullet item, which have nothing beside them (§9.5). Typographic quotes in view-model text, ‘ ’ and “ ”, are shown as ASCII `'` and `"` (C12, DEV-036); components add none.
- **Code:** a diff's lines, a plan's target and commands, and captured output keep their characters, indent and quotes (escape and control characters dropped, a tab as 4 spaces). Code is never cut: a line too long breaks at its last space after the indent, else inside a word, and each further piece starts with `Glyphs.Wrap` (DEV-038, §9.6).
- **Degrading:** without colour (`Env.HasColor` false) a bar is a run of `Glyphs.Bar` and a badge is bracketed (DEV-018, DEV-019). At every colour profile a state has a glyph and a word, or on a key-value row a marker beside the value that says it, even where Figma draws it in colour only (DEV-032, DEV-034). A value hardn could not read shows "Unknown" in `status.unknown` (DEV-022).
- **Empty lists** render as the empty string, so the caller leaves out the block and its gap.
- **Goldens:** each state on `uitest.Full()`, in `pkg/ui/component/testdata/golden/<component>/<state>.golden`.

### 9.2 Header group (`next/p1b-header`)

| Component | View-model | States (goldens) | Narrow, ASCII, no colour | Lip Gloss helpers |
|---|---|---|---|---|
| `Header` | `Version`, `URL` | `normal`, `no-version`, `no-url` | URL at `ValueCol` (C14, DEV-017); under the wordmark below 64 columns or when it does not fit. The version box only above 16 colours (DEV-005) | none: columns placed with spaces, as `JoinHorizontal` and `PlaceHorizontal` pad with trailing spaces (§2.3) |
| `ContextBar` | `Name`, `Version`, `Codename` | `normal`, `unknown`, `no-codename`, `long` | Fills the content width; text cut with the ellipsis; `Glyphs.Bar` without colour (C13, DEV-018). Empty name: "Unknown" | none |
| `TitleBar` | `Title` | `normal`, `long`, `empty` | As `ContextBar`; an empty title is all fill. P-08 decides whether sub-screens use it | none |
| `HostBlock` | `Hostname`, `Address`, `Uptime`, `RebootPending` | `normal`, `unknown-address`, `unknown-uptime`, `no-hostname`, `long-hostname`, `reboot-pending`, `all-unknown` | Stacks below 64 columns or for a badge reaching `ValueCol`; brackets without colour (DEV-019); the badge cut inside them. Pending-reboot line under the uptime, only when set: P-04 puts the flag next to the uptime, which does not fit 64 cells, so P-21 asks the maintainer to choose | `table` evaluated: a borderless table pads each cell to its column's width (§2.3), so the shorter values would end in spaces; `columns` places them |
| `RiskSummary` | `Band` (zero: `RiskUnknown`), `Level`, `Gloss`, `Factors` | `moderate`, `low`, `high`, `unknown`, `factors`, `factors-no-gloss`, `long-factor` | Stacks below 64 columns; ASCII `[*]` and `~`; factors one line each, 35 cells beside the label, cut with the ellipsis (P-06, P-14, DEV-022) | `list` evaluated: its default bullet is `•` and each line would still need the indent and the cut, so one line per factor is placed by hand. `table` as for `HostBlock` |

Not applicable and error are not separate states here: a value hardn could not read, including a failed read, is the unknown state. The panel outline around the host block, risk line and status rows is not drawn (C9, DEV-023).

### 9.3 Status group (`next/p1b-status`)

| Component | View-model | States (goldens) | Narrow, ASCII, no colour | Lip Gloss helpers |
|---|---|---|---|---|
| `StatusRow` | `Label`, `State` (`StatusState`; zero: `StatusUnknown`), `Word`, `Detail` | `configured`, `not-configured`, `partial`, `not-applicable`, `unknown`, `word`, `long-label`, `long-detail` | Glyph centred in the marker column (DEV-020), label at col 6, the state's word at col 27 and the detail one cell after it. Each state has its own glyph and word in its colour (DEV-024). A label over 20 cells is cut with the ellipsis; the value is cut at the content width (DEV-025). Stacks below 64 columns, word and detail at col 6 (DEV-021). ASCII markers `[+] [ ] [*] [-] [?]` | `table` as for `HostBlock`, and its cut is a fixed `…` |
| `StatusList` | `Rows` | `normal` (Figma's seven rows), `empty` (renders nothing) | As `StatusRow`; every row on the same grid | `list` evaluated: an `Enumerator` returning the marker already centred would draw the glyph at col 3 (§2.3), but each row still needs the label and value columns, the stacking and the cut, so a list adds nothing |
| `Divider` | none | `normal` | `Glyphs.Rule` (`─`, `-` in ASCII) in `divider` from col 0 across the bar width: 64 cells, or the terminal's width when narrower (DEV-026) | none |
| `ModeIndicator` | `DryRun` (zero: live) | `live`, `dry-run` | "Dry-run Mode" muted, then "Disabled" in `status.warn` or "Enabled" in `status.ok`, origin/main's words (P-03 decides the model). At col 27, the value column of the menu heading's line; at col 6 below 64 columns. Too narrow for it all, the label is cut before the word, which tells the modes apart, and left out once no letter of it fits; the word leaves its column only to stay whole (DEV-021) | none: placed with spaces, as `PlaceHorizontal` pads with trailing spaces (§2.3) |

**Words for the states Figma does not draw** (DEV-024): "Partial", "Not Applicable" and "Unknown", the brief's state names, spelt as Figma's "Not Configured" is. **Room for a detail beside the label** (DEV-025): 64 − 27 − the word − 1, so 26 cells after "Configured", 22 after "Not Configured" or "Not Applicable" and 29 after "Partial" or "Unknown"; stacked at 60 columns, 60 − 6 − the word − 1. The main menu's longest detail in Figma, "non-default (2230)", is 18 cells, and origin/main's, "password auth enabled", 21. 05-BACKUP's "UFW installed, deny policy" (a row copied from another screen) is 26: 29 cells are free beside its "Enabled", and beside "Configured" it would fit exactly.

`Word` shows another word in the state's colour, as Figma's SSH Login screen shows "2230/tcp" for a configured port and origin/main "Enabled" for a firewall that is on but not configured. The glyph still names the state. Proposals [P-01](../refactor/proposals.md#p-01) (rows and menu items from one control list) and [P-05](../refactor/proposals.md#p-05) (SSH states from `sshd -T`, `Unknown` when the sources disagree) change what the status model puts in the rows, not the row itself. The menu heading's row in `next/p1b-menu` places the ModeIndicator's text beside "Select an option".

Error is not a separate state: a check that failed is Unknown, with the reason in the detail, as P-05 has it for `sshd -T`. The ModeIndicator has no unknown state, since hardn always knows its own mode, and the Divider has no states.

### 9.4 Menu group (`next/p1b-menu`)

| Component | View-model | States (goldens) | Narrow, ASCII, no colour | Lip Gloss helpers |
|---|---|---|---|---|
| `MenuHeading` | `Title`, `Mode` (`*ModeIndicator`; nil: none, as on sub-menus) | `normal` (Figma's, with the mode), `no-mode`, `long`, `empty` (renders nothing) | Title at col 2 in `Menu.Heading`, underlined (C11, DEV-029); the mode at col 27, under the title at col 6 below 64 columns, for a title reaching col 27, or when the line beside would not fit (DEV-021, DEV-029), its word kept whole; a title past the content cut with the ellipsis | none: placed with spaces (§2.3). The underline is one SGR per rune, accepted (§2.3) |
| `Menu` | `Items` (`MenuItem`: `Key`, `Label`, `Description`, `Aside`, `Disabled`, `Risk`), `Exit` (a `MenuItem`) | `main` (Figma's), `sub` (01-USER), `risk`, `disabled`, `long`, `empty` (renders nothing) | Keys right-aligned to end at col 4 (`10)` at col 2), labels at col 6, descriptions at col 27, the aside in parentheses one cell after (DEV-027). The exit item after the section gap; Figma's "Press 'q' to exit immediately" is the brief's "q to quit" (DEV-030). A note line for an item's state: `∅ Unavailable` / `[-] Unavailable`, the item faint, or `⛬ Lockout risk` / `[*] Lockout risk` in `status.warn` bold (DEV-028; P-22 for the words, the place and which items). The whole menu stacks, so one description column holds per menu: below 64 columns, when the label of an item with a description or note reaches col 27 (labels are not cut beside the description; origin/main has 28 such items, DEV-027), or when col 27 has no room for a whole note or a letter of a description. A key of up to five cells grows into the indent to keep its label at col 6 | `list` evaluated: it right-aligns keys, but pads an item of more than one line into trailing spaces (§2.3). `table` as for `HostBlock` |
| `PromptLine` | `Label`, `Accepts` (`keys.Set`) | `normal` (Figma's), `no-hint`, `empty` (the glyph alone) | `❯` / `>` at col 2, the label, then `[hint]:` with the hint generated from `Accepts` (C11, DEV-029). No cursor and no trailing space: the line reader adds the space before the terminal's cursor (C8). Too narrow, the label is cut before the hint | none: one line of spans |

**Accepted inputs as data** (`pkg/ui/keys`, brief 1C's Bubbles `key.Binding` pattern). A `keys.Binding` names the inputs that select one choice (`Keys`, each a typed line such as `1`, `10` or `q`) and what it does (`Help`, the words after "to"). A `keys.Set` generates both:
- the hint, `Set.Hint`: numbers first, in order, each run of two or more consecutive numbers collapsed into a range, then the other inputs in first-seen order, joined as "x, y or z": `0-10 or q`, `1-3, 5 or q`, the plan's `0-10, b or q`;
- the help line, `Set.Help`: `q to quit`, `0 or b to go back, q to quit`.

`Menu.Bindings` gives the keys of the items that can be chosen, and the exit item's, so a screen builds its prompt as `PromptLine{Label: "Enter your choice", Accepts: append(menu.Bindings(), quit)}` and its Exit row's description as `keys.Set{quit}.Help()`: one binding, no hand-written hint. Matching a typed line to a binding, and the navigation grammar's own bindings, are `next/p1c-nav`'s. Bindings carry no actions, and neither do the menu's items: `next/p1c-nav` binds each key to what it does.

Not applicable is a menu item's `Disabled` state: an item that does not apply to the host cannot be chosen, and its aside can say why ("Proxmox" in `menu/disabled`). Unknown and error are not menu states: the screen fixes the items, and an action that fails reports through `Message` or `ErrorReport` (`next/p1b-blocks`, `next/p1b-plan`). A prompt with no accepted inputs shows no hint.

**Open for later units.** `PromptLine` always ends with a colon, and `keys.Set.Hint` joins inputs with "or" and marks no default, so two prompts the flow needs cannot be written yet:
- a continue prompt with no colon: Figma's "❯ Press any key to continue...", which `next/p1c-legacy-input` turns into an Enter prompt and P-09 limits to where output must be read; today `PromptLine{Label: "Press Enter to continue..."}` renders `> Press Enter to continue...:` (ASCII glyphs);
- a `y/N` confirmation, the capital marking the default (brief 1C; Figma draws "(y/n):"): a `keys.Set` of y and N renders the hint `[y or N]:`.

Both are additive: `next/p1c-nav`, which owns `y/N` confirmations, adds a default to `keys.Binding` and the hint form that shows it, and `next/p1c-legacy-input` a prompt that ends without the colon, its wording per P-09.

### 9.5 Block group (`next/p1b-blocks`)

| Component | View-model | States (goldens) | Narrow, ASCII, no colour | Lip Gloss helpers |
|---|---|---|---|---|
| `Badge` | `Text` | `normal`, `long`, `empty` (renders nothing) | At col 2 in `Badge`, a cell of padding either side; brackets with no colour (DEV-019); a long name cut inside them | none: one span |
| `SubjectLine` | `Label` (the colon is added), `Subject` | `normal` ("Managing: george"), `long-label`, `long`, `no-label` (the badge alone), `empty` (no subject: nothing) | The label at col 2 in `Text.Muted`, the badge one cell after the colon; when the line does not fit, the badge under the label at col 6, so neither is cut for the other (DEV-035) | none: spans placed with spaces |
| `SectionHeader` | `Title` | `normal` ("User Configuration"), `long`, `empty` (the stub and the rule) | Laid out as a bar: the stub at col 0 (`Glyphs.Bar` with no colour), the title from col 2, then a run of `Glyphs.Rule` in `Section.Rule` to the bar width (C13, DEV-033) | none |
| `Breadcrumb` | `Items` | `normal` ("hardn › Firewall"), `deep`, `long`, `empty` | At col 2, `Glyphs.Separator` between items; too long, the middle items collapse to one ellipsis, then the first, then the current item is cut (DEV-037). Proposal P-08 decides whether sub-screens use it | none: one line of spans |
| `KeyValue` | `Key`, `Value`, `State` (`ValueState`; zero: plain), `Note` | `plain`, `ok`, `warn`, `bad`, `unknown`, `not-applicable`, `note`, `long-key`, `long-value`, `item` (no value: a bullet item), `empty` (renders nothing) | `Glyphs.Bullet` centred in the marker column, the key at col 6, the value at col 27 (DEV-034); at every profile a stated value's status marker, in its colour, replaces the bullet; the value under the key at col 6 below 64 columns, for a key reaching col 27, or with no room beside (DEV-021); the value cut with the ellipsis | see `BulletList`; `table` as for `HostBlock` |
| `BulletList` | `Items` (`KeyValue`) | `user` (01-USER), `ssh` (02-SSH_LOGIN), `checklist` (the bullet items under Figma's warning), `long`, `empty` | Stacks as a whole when one of its rows would, as `Menu` does (DEV-027); a bullet item wraps under itself at col 6 | `list` evaluated: it could draw the bullet or a state's marker, but pads a stacked row or a wrapped item into trailing spaces (§2.3) |
| `Message` | `Kind` (`MessageKind`; zero: info), `Text` | `ok`, `warn`, `bad`, `info`, `cancelled`, `long`, `empty` | The kind's glyph centred in the marker column, then its word and a colon at col 6, both bold in its colour; the text after them in `Message.Text`, wrapping under the word (DEV-031) | `lipgloss.Wrap` (§2.3) |
| `Callout` | `Kind`, `Title` (empty: the kind's own), `Body` | `warn` (Figma's), `ok`, `cancelled`, `bad`, `info`, `no-title`, `long` (a title of its own) | The accent bar at col 0 beside every line, a space in `Callout.<Kind>.Bar` or `Glyphs.Bar` with no colour; from col 2 the kind's glyph and the title, a title of its own after the kind's word, cut; the body at col 2, wrapped (DEV-032) | `lipgloss.Wrap`; the bar is painted line by line, so no join |
| `AnsweredField` | `Number`, `Label`, `Accepts` (`keys.Set`), `Answer` | `text` ("1. Username: george"), `yes-no`, `confirm` (unnumbered), `unanswered`, `unnumbered` (no number, no hint), `long-answer`, `empty` (no question: nothing) | The number right-aligned to end at col 4, the question at col 6 (col 2 unnumbered); question, hint and colon laid out as `PromptLine`'s, the hint generated from `Accepts`; the answer one cell after the colon in `Field.Answer`, muted (D4), or under the question at col 6 when the line does not fit (DEV-036) | none: spans, sharing `PromptLine`'s layout |

**Value states** (`ValueState`, DEV-034). A key-value row's value says something about the host, and Figma says it in colour: "Enabled" is red beside "Root SSH Login" and green beside "Backup State". `ValueOK`, `ValueWarn` and `ValueBad` take `status.ok`, `status.warn` and `status.bad`; `ValuePlain`, the zero value, is muted; `ValueUnknown` and `ValueNotApplicable`, which Figma does not draw, show "Unknown" and "Not Applicable" when the value is empty. A `Note` follows a plain value in the state's colour, for Figma's "/var/backups/hardn Writable" and "Enabled, No password". At every colour profile a stated value's bullet gives way to the state's status marker, in its colour, as a StatusRow would mark the same verdict: ⛛ `[+]` ok, ⛬ `[*]` warn, ⭘ `[ ]` bad, `?` `[?]` unknown, ∅ `[-]` not applicable; a plain value keeps Figma's bullet. Figma draws these values in colour only, which the never-colour-alone rule does not allow (DEV-034).

**Message kinds** (`MessageKind`, DEV-031, DEV-032). Info (the zero value), ok, warn, bad and cancelled, each with its own glyph (§6), word and colour, as brief 1B asks of a message. A `Message` is origin/main's "glyph sentence" line with the kind's word before the sentence: `[!] Error: Failed to configure sudo: exit status 1`. The words (`MessageKind.String`) are "Info", "Success", "Warning", "Error" and "Cancelled". A `Callout` draws its kind's glyph before its title at every profile. With no title it shows its kind's own: Figma's "Operation Successful", "Warning" and "Operation Cancelled", and for the kinds Figma does not draw "Operation Failed" and "Note"; a title of the screen's own follows the kind's word, as in `[*] Warning: Changing the SSH port`. Proposal [P-23](../refactor/proposals.md#p-23) puts the words to the maintainer.

Not applicable, unknown and error by component: a `KeyValue` has both as value states; a `Badge` or `SubjectLine` names what the screen already holds (a user read from the password file, or chosen from a list), so it has no unknown state and renders nothing without its subject; a failure is a `Message` or `Callout` of kind bad, and a full failure report is `ErrorReport` (`next/p1b-plan`). The other components have no state of their own.

**Open for later units.** `AnsweredField` with no answer is the question as asked, so `next/p1c-form` can use it for both the asking line and its echo. Its y/n hint reads `[y or n]:` until `next/p1c-nav` gives `keys.Binding` a default and the hint form that shows it (§9.4). No screen uses `Breadcrumb` before P-08 is decided.

### 9.6 Plan group (`next/p1b-plan`)

| Component | View-model | States (goldens) | Narrow, ASCII, no colour | Lip Gloss helpers |
|---|---|---|---|---|
| `PlanView` | `Changes` (`PlanChange`: `Title`, `Target`, `LockoutRisk`, `Diff`, `Before`, `After`, `Commands`), `DryRun` | `normal`, `lockout-risk`, `dry-run`, `new-file`, `no-newline`, `long`, `unknown` (no title), `empty` (renders nothing) | `Glyphs.Bullet`, or `Glyphs.Risk` and "Lockout risk" for a lockout-risk change, then at col 6 the title (wrapped), the target, the diff (`+`, `-`, context, hunk headers; the `---`/`+++` headers left out when one of them names the target), `Before`/`After` as `-`/`+` lines and the commands after `$`, all code (DEV-038). Dry-run footer: origin/main's "No actual changes were made to your system." as an info `Message` | `list` and `tree` evaluated: a change is many lines, padded into trailing spaces by `JoinHorizontal` (`tree/renderer.go:126-131`), and `tree`'s branches are box drawing; code wraps with its own loop (§2.3) |
| `StepProgress` | `Steps` (`Step`: `Title`, `State` (`StepState`; zero: pending), `Detail`), `Frame` | `apply`, `animated` (`Env.Animate` in every cell), `failed`, `skipped`, `long`, `empty` | One line per step, as a `Message`: glyph centred, the state's word at col 6, the title, the detail in parentheses; cut, never wrapped. ◦ `[ ]` pending, ▸ `[>]` running (with `Env.Animate`, the `Frame`'th of ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ or `\|/-\`), ✓ `[+]` done, ✗ `[!]` failed, ↺ `[<]` rolled back, ↷ `[-]` skipped (DEV-039) | `list` evaluated: it pads a narrower enumerator on the left to the widest sibling's (`tree/renderer.go:79-81`, `100-103`), so an ASCII spinner frame would not be centred, and the line still needs the cut |
| `ErrorReport` | `Failed`, `RolledBack`, `Command`, `Output`, `Tail` (zero: `DefaultTail`, 10), `LogPath`, `Next` | `normal`, `tail`, `nothing-rolled-back`, `long`, `empty` (the callout alone) | A bad `Callout` ("Operation Failed", the failure as body), a rolled-back `StepProgress` line per change undone, the command and the output's last lines at col 6 in `Output` as code (an ellipsis line when lines were left out), then origin/main's "Check the log file at … for details" and the next step as info `Message`s (DEV-040) | none: built from the other components, joined with the section gap |
| `ConfirmCountdown` | `Remaining` (seconds), `Port`, `Command` (empty: `sudo hardn confirm`) | `waiting`, `port`, `urgent`, `zero`, `command` | "⛬ Warning: This change reverts automatically in 2:45", then "To keep it, open a new SSH session [on port N] and run:" and the command, bold. Under 30 seconds (`UrgentBelow`) "! Urgent:" (`[!]`) in `status.bad`: glyph and word, not colour alone (DEV-041) | none: spans; the sentences wrap with `lipgloss.Wrap` |

**The line diff** (`pkg/diff`, outside `pkg/ui` because the plan/apply engine's `Change.Diff` uses it too). `diff.Unified(oldName, newName, oldText, newText)` writes the `diff -u` format with 3 lines of context (`UnifiedContext` takes another number): hunks merged when closer than twice the context, a count of 1 left out of a hunk header, an empty range starting at the line before it, and `\ No newline at end of file` after a last line without one; identical texts give "". It trims the common start and end and runs Myers' O(ND) search for a shortest script, falling back past 1000 edits to all-removed-then-added, which is still correct. `diff.Classify` gives each line of a unified diff its kind, counting a hunk's lines from its header, so `PlanView` styles a removed `--- x` line as removed. Standard library only.

**Animation.** `StepProgress` and `ConfirmCountdown` are pure: the caller advances `StepProgress.Frame`, or counts `ConfirmCountdown.Remaining` down, and renders again. With `Env.Animate` false (no terminal, `TERM=dumb`, plain mode) the spinner is replaced by `Glyphs.Running`, and the caller prints each step's line once, as brief 1C asks (one plain line per step), and the countdown's notice once.

**Words** (proposal [P-24](../refactor/proposals.md#p-24)). The step states are brief 1B's names and "Skipped". The countdown's sentences are new, after brief §2 and Phase 2.4. `PlanView` and `ErrorReport` add none of their own: "Lockout risk" is the menu's (P-22), "Operation Failed" and the message words are P-23's, and the dry-run footer and the log sentence are origin/main's.

Not applicable, unknown and error by component: a plan change with no title shows "Unknown" (a plan never hides a change); a step has no unknown state, since the engine knows where each step stands, and a step that could not run is failed or skipped, with the reason in its detail; an `ErrorReport` is the error state of the Apply step; the countdown has none.

**Open for later units.**
- What an empty plan says, and whether the flow then asks anything, is `next/p1c-flow`'s: `PlanView` renders nothing.
- Paging a long plan, and opening the full log in `$PAGER` (P-18), are the screen's: both components render in full.
- The countdown's local-console form (brief Phase 2.4: confirm in the same session, with a warning, when hardn is not over SSH), the timeout flag and the command's path are `next/p2-revert`'s, which fills `Remaining`, `Port` and `Command`.

### 9.7 Still to come (placeholders)

- **Screen layouts:** the main-menu, sub-menu and workflow-step layouts, which screens use each, and the table from screenshot elements and export tokens to Go components land with `next/p1d-slice`.
- **Usage rules and depguard:** the rules for using the system (including the brief's 1C list of patterns not to borrow) and the depguard configuration that keeps Lip Gloss, colorprofile and fatih/color out of feature code, and `compat` out everywhere, land with `next/p1d-lint`.

## 10. Token report

The brief's optional dev-only report (export tokens with no mapping, mapped values that differ) is **deferred: the export uses a custom fig-extract format; the mapping above is maintained by hand.**
