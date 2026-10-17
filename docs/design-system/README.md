# hardn design system

> **For:** maintainers reviewing the design system. It records what is built, why, and where it departs from the Figma design.
>
> **Status:** Phase 1A (foundations) of the refactor plan: `pkg/ui/term` (the render environment), `pkg/ui/theme` (tokens, style slots, glyphs, grid) and `pkg/ui/uitest` (the golden harness); and from Phase 1B the first components in `pkg/ui/component`: the header group (`next/p1b-header`), the status group (`next/p1b-status`), the menu group, with the accepted-input data in `pkg/ui/keys` (`next/p1b-menu`), the block group (`next/p1b-blocks`), and the plan group, with the line diff in `pkg/diff` (`next/p1b-plan`); and from Phase 1C the line reader in `pkg/ui/prompt` (`next/p1c-reader`), the navigation grammar in `pkg/ui/nav` (`next/p1c-nav`), the forms in `pkg/ui/form`, with the `FieldHead` component (`next/p1c-form`), the canonical action flow in `pkg/ui/flow` (`next/p1c-flow`), and the legacy menus' input moved onto the line reader (`next/p1c-legacy-input`); and from Phase 1D the screen layouts and the main-menu vertical slice in `pkg/ui/screen`, with a menu's compact two-column form (`next/p1d-slice`), and the dev-only component gallery in `internal/tools/uigallery`, with the component goldens' states in `pkg/ui/component/fixture` and the theme's colour hook (`next/p1d-gallery-components`), and the gallery's screen mocks, with the workflows' sequences step by step (`next/p1d-gallery-screens`), and its playable mode, with the prompt tests' scripted typist moved into `pkg/ui/uitest` (`next/p1d-gallery-playable`); and the usage rules, with the import rules golangci-lint's depguard enforces, `make golden-update` reaching the gallery's goldens and `make check` vetting the module for Linux (`next/p1d-lint`). The one `pkg/ui` package `cmd/hardn` links is `pkg/ui/prompt`, through the legacy menus and the config prompts: `go list -deps ./cmd/hardn` lists no other `pkg/ui` package and no `charm.land` or `charmbracelet` one.
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
- **Renderers are pure functions** of a view-model and the render environment (`term.Env`). They return strings and do no I/O. `term.Detect` is the only code that looks at the terminal, with one exception: `prompt.Stdin` asks whether stdin is a terminal itself, with the same ioctl, because importing `term` would link Lip Gloss (§2.3, §9.7).
- **No raw ANSI escape codes outside `pkg/ui`.** Feature code gets its styling from the theme (usage and import rules: §9.15).
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

One existing module became a direct requirement, at the same version: `golang.org/x/sys` v0.49.0 (BSD-3-Clause), from `next/p1c-reader` (`pkg/ui/prompt`: `unix.Select` and the flush ioctls; §9.7).

### 2.3 Facts that shape the design

Checked against the module source at the versions above; citations are `file:line` in that module.

| Topic | Fact | What hardn does |
|---|---|---|
| Colour fallback | `Style.Render` emits every colour exactly as set (lipgloss `style.go:353-371`); it never downsamples. Downsampling happens only in `colorprofile.Writer` (colorprofile `writer.go:38-51`): TrueColor passes through, NoTTY strips every escape sequence, and ASCII, ANSI and ANSI256 re-encode each SGR (`writer.go:87-171`), dropping colours below ANSI and converting the rest with `Profile.Convert`, which keeps an indexed colour at ANSI256 and a basic one at ANSI (`profile.go:58-108`). The writer parses each `Write` on its own (`writer.go:54-80`). | The theme picks each token's value for the profile with `lipgloss.Complete` (`color.go:265-277`; `NoColor` at ASCII and NoTTY), so the deliberate 256- and 16-colour picks pass through the writer unchanged (`TestWriterKeepsPicks`). All output goes through `term.Env.Writer`, a `colorprofile.Writer` with the profile fixed, written one whole string at a time. |
| Lip Gloss's own printers | `Print*` write through `lipgloss.Writer`, created at init for `os.Stdout` (`writer.go:14`); `Sprint*` use that writer's profile (`writer.go:110-160`); `Fprint*` detect a profile again on every call (`writer.go:67-97`). | Never used: they ignore `term.Options`. |
| `NO_COLOR` | colorprofile parses `NO_COLOR`, `CLICOLOR` and `CLICOLOR_FORCE` with `strconv.ParseBool` (`env.go:115-128`), so `NO_COLOR=0` or `NO_COLOR=yes` leave colour on. A true `NO_COLOR` caps the profile at ASCII only for a terminal (`env.go:86-91`), so for output that is not a terminal a true `CLICOLOR_FORCE` raises the profile to at least ANSI, and to whatever `TERM`, `COLORTERM` and the like give when that is higher (`env.go:93-101`), and `Detect` returns that profile whatever `NO_COLOR` says (`env.go:40-43`). | `term.Resolve` follows no-color.org: any non-empty `NO_COLOR` turns colour off. It decides `NO_COLOR`, `TERM=dumb` and the TTY check before asking `colorprofile.Env`, so `CLICOLOR_FORCE` cannot colour piped output or a dumb terminal (§3). |
| Import side effect | Linking the root package runs `var Writer = colorprofile.NewWriter(os.Stdout, os.Environ())` at init (lipgloss `writer.go:14`), which calls `Detect` (colorprofile `writer.go:23-28`): an `ioctl` on stdout and then, on a terminal with `TERM` set and not `dumb`, unless the environment alone gives TrueColor or a true `NO_COLOR`, a terminfo read and, with `$TMUX` set, `tmux info` run with no timeout (`env.go:33-54`, `208-262`). x/ansi reads `RUNEWIDTH_EASTASIAN` at init (`method.go:20-25`). Nothing at init writes a file or changes a terminal mode. | The init-time profile is ignored. Today only the `pkg/ui` test binaries and the dev-only gallery (§9.14) run these effects. Full inventory, including `TTY_FORCE` and the `/etc/passwd` read in cgo-less builds: [side-effects.md](../refactor/phase0/side-effects.md) §13.1. |
| Background detection | `lipgloss.HasDarkBackground` puts the terminal in raw mode with ISIG, ICANON and ECHO off (lipgloss `query.go:13`; x/term `term_unix.go:31`), sends an OSC 11 and a DA1 request (`terminal.go:45`) and waits up to 2 s for the reply on stdin and, if that fails, another 2 s on stdout (`terminal.go:49`, `query.go:60-64`): about 4 s in all. Meanwhile Ctrl-C does not interrupt, type-ahead is read and discarded, and a late reply is left in stdin. It reports dark on any error (`query.go:86-91`). The `compat` package runs this query, and a second `Detect`, at import (`compat/color.go:13`, `16`). | Opt-in (an adjustment to the brief's "detect the background where possible"): dark by default; `term.Detect` queries once, only for theme `auto` with stdin and stdout both terminals and plain mode off. `compat` must never be imported (depguard rule `never`, §9.15). |
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

The export is a custom fig-extract format (inferred tokens; no W3C `$value`/`$type`, no Figma variables): see [phase0-findings.md](../refactor/phase0-findings.md#81-figma-export) §8.1. The Go side is the `palette` struct in `pkg/ui/theme/palette.go`: one `token{hex, ansi256, ansi}` per semantic name, in `darkPalette` and `lightPalette`. `theme.New(env)` picks the palette by `env.Dark` and each colour by `env.Profile`. To try other values without editing `palette.go`, the gallery's override file replaces tokens for one run through `theme.NewWithOverrides`, which nothing but the gallery may call (§9.14).

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
| `color.text.input` | `text:#DFDFDF` | #DFDFDF | `badge.fg` (merged) | #DFDFDF | 254 | 15 BrightWhite | #FFFFFF / 231 / 15 BrightWhite | DEV-003; D3, D4: used only on badges, merged with the host badge's #C0C4D8 text; answers shown again (`AnsweredField`) are `text.muted`, the live echo is the terminal's (DEV-043) |
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

`theme.New` builds 105 style slots, grouped by component (`theme.Theme`). Slots carry only colours and text attributes (today bold and underline); layout belongs to the components.

| Token | Slots |
|---|---|
| `text.primary` | `Text.Primary`, `Header.Wordmark`, `TitleBar.Title`, `Risk.Label`, `Status.Label`, `Menu.Key`, `Menu.Label`, `Field.Number`, `Field.Label`, `Field.Hint`, `Message.Info`, `Callout.Info.Title`, `KeyValue.Key`, `Breadcrumb.Current`, `Plan.Title`, `Step.Running`, `Step.Title`, `Confirm.Text`, `Confirm.Command` |
| `text.muted` | `Text.Muted`, `Header.Version`, `ContextBar.Name`, `ContextBar.Version`, `Host.Address`, `Risk.Factor`, `Status.NotApplicable`, `Mode.Label`, `Menu.Heading`, `Menu.Description`, `Menu.ExitKey`, `Menu.Exit`, `Prompt.Glyph`, `Prompt.Label`, `Prompt.Hint`, `Field.Answer`, `Field.Description`, `Message.Text`, `Callout.Body`, `KeyValue.Bullet`, `KeyValue.Value`, `KeyValue.ValueNotApplicable`, `Section.Title`, `Breadcrumb.Item`, `Plan.Target`, `Plan.Context`, `Plan.Command`, `Step.Pending`, `Step.Skipped`, `Output` |
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

Slots for states Figma does not draw (risk bands other than Moderate, Partial, messages, plan and step states, and others) are DEV-016; the header group's `ContextBar.Unknown`, `Host.Unknown` and `Risk.Factor` are DEV-022; the block group's `Callout.Bad`, `Callout.Info`, `KeyValue.ValueUnknown` and `KeyValue.ValueNotApplicable` are DEV-032 and DEV-034; the plan group's `Plan.Title`, `Plan.Target`, `Plan.Meta`, `Plan.Command`, `Step.Skipped`, `Step.Title`, `Step.Detail`, `Output` and `Confirm` are DEV-038 to DEV-041, all from existing tokens. `Field` styles the answered form field (`next/p1b-blocks`); its answer slot was `Prompt.Input` until then. `next/p1c-form` added `Field.Description`, muted as the other prose under a label; a field's options are `Menu` rows and its inline error a `Message` of kind bad, as every refused line is, so neither has a slot of its own (DEV-046).

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
| `Wrap` | ↪ | U+21AA, N | 1 | `\|` | 1 | DEV-038: starts the rest of a code line too long for its line. ASCII `\|` starts no shell or diff line: a pasted continuation is a syntax error, where `>` would redirect into the file it names |
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

`CapContent`, `StackBelow` and `BarsFillContent` are applied by the components from `next/p1b-header` on; `CompactBelow` and `CompactBudget` are applied by the main-menu layout from `next/p1d-slice` (§9.12, DEV-052).

`StackBelow` is `ContentWidth`, within the brief's "below about 70 columns". It is keyed on the terminal's width, and from 64 columns up `CapContent` makes the content box 64 cells, as at 80, so a row that fits side by side at 80 columns fits from 64 up. At 70, as first set, the header, HostBlock and RiskSummary stacked from 64 to 69 columns although their side-by-side lines fit, using 7 lines where 4 do, and Figma's seven status rows 14 lines where 7 do; `next/p1b-menu` lowered it to 64, and no golden changed (DEV-021). At 60 columns stacking is needed for some rows, not all: Figma's widest status row is 56 cells, so its seven rows would fit side by side there too, and stacking them adds 7 lines against the compact menu's budget of 24 (`CompactBudget`). Figma's menu is 23 lines stacked at 60 columns and 12 side by side; of its lines only item 1's, 61 cells, would not fit 60 columns (DEV-021).

## 8. Golden tests

Goldens lock in hardn's own rendering to catch regressions. They do not measure conformance to Figma; update them deliberately and give the reason in the review description.

**Matrices** (`pkg/ui/uitest`, from brief Phase 6):
- `uitest.Full()`, for components: 48 cells, colour {truecolor, 256, 16, none} × background {dark, light} × glyphs {unicode, ascii} × width {60, 80, 120}, the first axis varying slowest. Cells are named after their values, such as `truecolor-dark-unicode-80` or `none-light-ascii-60`. "none" is the ASCII profile: no colour, bold and underline kept.
- `uitest.Core()`, for screens and workflow steps: `truecolor-dark-unicode-80`, `plain-80` (plain mode: NoTTY, ASCII glyphs, no escape sequences) and `truecolor-dark-unicode-60`.

**Writing a golden test:** `uitest.Golden(t, "<case>", uitest.Full(), func(env term.Env) string { ... })`. For each cell it renders, writes the result through `env.Writer` in one write, checks every line against the cell's width (`uitest.AssertFits`), and compares with `testdata/golden/<case>.golden` in the test's package directory. A transcript's answers come from `uitest.NewTypist(out, lines...)`, a reader for `prompt.New` that types each line and echoes it into `out`, the transcript, as a terminal shows it, then reports the end of the input; the tests of `nav`, `form`, `flow`, `screen` and the gallery's playable mode share it (`next/p1d-gallery-playable`; each had its own copy before, and every golden stayed byte-identical).

**Files today:**
- `pkg/ui/theme/testdata/golden/swatch.golden`: `TestSwatch` on `Full()`. Every token's name in its colour and as a background, every glyph by role, and the 27 typography roles.
- `pkg/ui/uitest/testdata/golden/harness.golden`: the harness's own check, one render at each profile and in plain mode, at width 20.
- `pkg/ui/component/testdata/golden/<component>/<state>.golden`: `TestGolden` on `Full()`, one file per component state (§9.2 to §9.6). The states are `fixture.States()` in `pkg/ui/component/fixture`, which the gallery renders too (§9.14).
- `pkg/ui/nav/testdata/golden/nav/<prompt>.golden`: `TestGolden` on `Core()`, a transcript per prompt of the grammar, the answers echoed as a terminal shows them (§9.8).
- `pkg/ui/form/testdata/golden/form/<case>.golden`: `TestGolden` on `Core()`, a transcript per kind of field, asked, refused and answered, and of Figma's form (§9.9).
- `pkg/ui/flow/testdata/golden/flow/<path>.golden`: `TestGolden` on `Core()`, the full transcript of each path of the canonical flow (§9.10).
- `pkg/ui/screen/testdata/golden/<layout>/<screen>.golden`: `TestGolden` on `Core()`, one file per screen mock (`screen.Mocks()`), named after its layout and its PNG, and for a workflow one per step of each sequence, `<layout>/<screen>/<sequence>/<n>-<step>.golden`; the main menu also at 80x40 on both themes, where it takes its full form (§9.12). The gallery shows the same mocks (§9.14).
- `internal/tools/uigallery/testdata/golden/play/<path>.golden`: the gallery's `TestPlayGolden` on `Core()`, the whole transcript of a path through the playable mode, the answers typed and echoed, the gallery's notes and the simulated system's record included (§9.14). The only goldens outside `pkg/ui`; `make golden-update` rewrites them after `pkg/ui`'s (below).

**Running and regenerating:**
- `make check` runs every golden in its `check-test` step, and fails on a mismatch.
- `make golden-update` rewrites the goldens of every package whose test binary links `uitest`, after an intended change: `pkg/ui`'s packages, then the others, today the gallery, whose tests compare its output with `pkg/ui`'s goldens, so that no test reads a file being rewritten (`next/p1d-lint`). Review the diff, then commit it with the reason (§9.15).
- One package: `go test ./pkg/ui/theme -update`. The make target also sets `GOTOOLCHAIN=local`, `GOWORK=off` and `-mod=readonly`.
- The gallery's play goldens alone: `go test ./internal/tools/uigallery -update`. Until `next/p1d-lint` the make target listed `./pkg/ui/...` only and left them out, though their header says "regenerate with make golden-update".
- `-update` is defined once, in `uitest`. A package that uses `uitest` must not define its own `-update` or link `github.com/charmbracelet/x/exp/golden`, which defines one too; the flag package panics on the second definition. Packages that do not link `uitest` reject `-update`, which is why `make golden-update` selects packages instead of passing it to `./...`.

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
- **Code:** a diff's lines, a plan's target and commands, and captured output keep their characters, indent and quotes, typographic ones too (escape and control characters dropped, a tab as 4 spaces, and a character that draws as nothing or as a blank other than the space shown as its code point, such as `<U+200B>`); a command keeps its line breaks. Code is never cut: a line too long breaks at its last space after the indent, else inside a word, and each further piece starts with `Glyphs.Wrap`, then the spaces at the break (DEV-038, §9.6).
- **Degrading:** without colour (`Env.HasColor` false) a bar is a run of `Glyphs.Bar` and a badge is bracketed (DEV-018, DEV-019). At every colour profile a state has a glyph and a word, or on a key-value row a marker beside the value that says it, even where Figma draws it in colour only (DEV-032, DEV-034). A value hardn could not read shows "Unknown" in `status.unknown` (DEV-022).
- **Empty lists** render as the empty string, so the caller leaves out the block and its gap.
- **Goldens:** each state on `uitest.Full()`, in `pkg/ui/component/testdata/golden/<component>/<state>.golden`. A state is defined once, in `pkg/ui/component/fixture` (`fixture.States()`), which the tests and the gallery share (§9.14).

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
| `Menu` | `Items` (`MenuItem`: `Key`, `Label`, `Description`, `Aside`, `Disabled`, `Risk`), `Exit` (a `MenuItem`), `Compact` | `main` (Figma's), `sub` (01-USER), `risk`, `disabled`, `long`, `compact` (Figma's in two columns, `next/p1d-slice`), `empty` (renders nothing) | Keys right-aligned to end at col 4 (`10)` at col 2), labels at col 6, descriptions at col 27, the aside in parentheses one cell after (DEV-027). The exit item after the section gap; Figma's "Press 'q' to exit immediately" is the brief's "q to quit" (DEV-030). A note line for an item's state: `∅ Unavailable` / `[-] Unavailable`, the item faint, or `⛬ Lockout risk` / `[*] Lockout risk` in `status.warn` bold (DEV-028; P-22 for the words, the place and which items). The whole menu stacks, so one description column holds per menu: below 64 columns, when the label of an item with a description or note reaches col 27 (labels are not cut beside the description; origin/main has 28 such items, DEV-027), or when col 27 has no room for a whole note or a letter of a description. A key of up to five cells grows into the indent to keep its label at col 6. `Compact`: two columns, down then across, keys and labels alone, the second column's keys ending at col 29 and labels at col 31; one column whenever two cannot show every item whole (DEV-052) | `list` evaluated: it right-aligns keys, but pads an item of more than one line into trailing spaces (§2.3). `table` as for `HostBlock` |
| `PromptLine` | `Label`, `Accepts` (`keys.Set`) | `normal` (Figma's), `no-hint`, `empty` (the glyph alone) | `❯` / `>` at col 2, the label, then `[hint]:` with the hint generated from `Accepts` (C11, DEV-029). No cursor and no trailing space: the line reader adds the space before the terminal's cursor (C8). Too narrow, the label is cut before the hint | none: one line of spans |

**Accepted inputs as data** (`pkg/ui/keys`, brief 1C's Bubbles `key.Binding` pattern). A `keys.Binding` names the inputs that select one choice (`Keys`, each a typed line such as `1`, `10` or `q`) and what it does (`Help`, the words after "to"). A `keys.Set` generates both:
- the hint, `Set.Hint`: the prompt's own inputs (bindings with no `Help`: a menu's items, a question's answers, a phrase) before the inputs every prompt shares (bindings with `Help`, such as `keys.Navigation`'s), each input once; in each part numbers first, in order, each run of two or more consecutive numbers collapsed into a range, then the other inputs in first-seen order; joined as "x, y or z": `0-10 or q`, `1-3, 5 or q`, the plan's `0-10, b or q`, and `bos-san-l01, 0, b or q` for a phrase (since `next/p1c-nav`; before it every number came first);
- the help line, `Set.Help`: `q to quit`, `0 or b to go back, q to quit`;
- for a question, a Set with a `Default` binding, its answers as one term, the first default in capitals: `y/N`, `y/N, 0, b or q` (§9.8).

`Menu.Bindings` gives the keys of the items that can be chosen, and the exit item's, so a screen builds its prompt as `PromptLine{Label: "Enter your choice", Accepts: append(menu.Bindings(), keys.Navigation()...)}` and its Exit row's description as `keys.Set{keys.Quit()}.Help()`: one binding, no hand-written hint; `nav.Menu.Accepts` does the first. Bindings carry no actions, and neither do the menu's items: `pkg/ui/nav` binds each item to what it does, and matches a typed line to its binding with `keys.Set.Match` (§9.8).

Not applicable is a menu item's `Disabled` state: an item that does not apply to the host cannot be chosen, and its aside can say why ("Proxmox" in `menu/disabled`). Unknown and error are not menu states: the screen fixes the items, and an action that fails reports through `Message` or `ErrorReport` (`next/p1b-blocks`, `next/p1b-plan`). A prompt with no accepted inputs shows no hint.

**Open for later units.** `PromptLine` always ends with a colon, so one prompt cannot be written with it: a continue prompt with no colon, Figma's "❯ Press any key to continue...". `next/p1c-legacy-input` made the legacy screens' pauses "Press Enter ..." in their own `pkg/style` text (DEV-050, §9.7), and the canonical flow asks none (§9.10), so no screen built from the components needs one yet; `PromptLine{Label: "Press Enter to continue..."}` would render `> Press Enter to continue...:` (ASCII glyphs). A prompt that ends without the colon is additive, and waits for P-09, which decides where a pause stays and its words. (The other prompt this list held, a `y/N` confirmation with its default in capitals, was added by `next/p1c-nav`: `keys.Binding.Default` and the question's hint form `[y/N]:`, DEV-045, §9.8.)

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

**Open for later units.** `AnsweredField` with no answer is the question as asked; `pkg/ui/nav` asks its questions so (§9.8), and `pkg/ui/form` asks a one-line field so and echoes an answer with it (§9.9). Its y/N hint reads `[y/N]:`, the default in capitals, since `next/p1c-nav` (DEV-045). The sub-menu and workflow-step layouts use `Breadcrumb` where Figma draws a `TitleBar`, as brief 1B has them, while P-08 is decided (§9.12).

### 9.6 Plan group (`next/p1b-plan`)

| Component | View-model | States (goldens) | Narrow, ASCII, no colour | Lip Gloss helpers |
|---|---|---|---|---|
| `PlanView` | `Changes` (`PlanChange`: `Title`, `Target`, `LockoutRisk`, `Diff`, `Before`, `After`, `Commands`), `DryRun` | `normal`, `lockout-risk`, `dry-run`, `new-file`, `no-newline`, `long`, `unknown` (no title), `empty` (renders nothing) | `Glyphs.Bullet`, or `Glyphs.Risk` and "Lockout risk" for a lockout-risk change, then at col 6 the title (wrapped), the target, the diff (`+`, `-`, context, hunk headers; a file's `---`/`+++` pair left out when one of the two names the target, every other file's kept), `Before`/`After` as `-`/`+` lines and the commands after `$` (a command's further lines under its text), all code (DEV-038). Dry-run footer: origin/main's "No actual changes were made to your system." as an info `Message` | `list` and `tree` evaluated: a change is many lines, padded into trailing spaces by `JoinHorizontal` (`tree/renderer.go:126-131`), and `tree`'s branches are box drawing; code wraps with its own loop (§2.3) |
| `StepProgress` | `Steps` (`Step`: `Title`, `State` (`StepState`; zero: pending), `Detail`), `Frame` | `apply`, `animated` (`Env.Animate` in every cell), `failed`, `skipped`, `long`, `empty` | One line per step, as a `Message`: glyph centred, the state's word at col 6, the title, the detail in parentheses; cut, never wrapped. ◦ `[ ]` pending, ▸ `[>]` running (with `Env.Animate`, the `Frame`'th of ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ or `\|/-\`), ✓ `[+]` done, ✗ `[!]` failed, ↺ `[<]` rolled back, ↷ `[-]` skipped (DEV-039) | `list` evaluated: it pads a narrower enumerator on the left to the widest sibling's (`tree/renderer.go:79-81`, `100-103`), so an ASCII spinner frame would not be centred, and the line still needs the cut |
| `ErrorReport` | `Failed`, `RolledBack`, `Command`, `Output`, `Tail` (zero: `DefaultTail`, 10), `LogPath`, `Next` | `normal`, `tail`, `nothing-rolled-back`, `long`, `unknown` (a rolled-back change with no name), `empty` (the callout alone) | A bad `Callout` ("Operation Failed", the failure as body), a rolled-back line per change undone, laid out as `StepProgress`'s but its name wrapped under the word and an empty one "Unknown", the command and the output's last lines at col 6 in `Output` as code (an ellipsis line when lines were left out), then origin/main's "Check the log file at … for details" and the next step as info `Message`s (DEV-040) | none: built from the other components, joined with the section gap; the rolled-back names wrap with `lipgloss.Wrap`, as a `Message`'s text |
| `ConfirmCountdown` | `Remaining` (seconds), `Port`, `Command` (empty: `sudo hardn confirm`) | `waiting`, `port`, `urgent`, `zero`, `command` | "⛬ Warning: This change reverts automatically in 2:45", then "To keep it, open a new SSH session [on port N] and run:" and the command, bold. Under 30 seconds (`UrgentBelow`) "! Urgent:" (`[!]`) in `status.bad`: glyph and word, not colour alone (DEV-041). The first sentence keeps its breaks, and the block its height, for any time left under 100 minutes, urgent or not | none: spans; the sentences wrap with `lipgloss.Wrap` |

**The line diff** (`pkg/diff`, outside `pkg/ui` because the plan/apply engine's `Change.Diff` uses it too). `diff.Unified(oldName, newName, oldText, newText)` writes the `diff -u` format with 3 lines of context (`UnifiedContext` takes another number): changes at most twice the context apart sharing a hunk, a context longer than both texts (up to `math.MaxInt`) keeping every line, a count of 1 left out of a hunk header, an empty range starting at the line before it, and `\ No newline at end of file` after a last line without one; identical texts give "". It trims the common start and end and runs Myers' O(ND) search for a shortest script, falling back past 1000 edits to all-removed-then-added, which is still correct. `diff.Classify` gives each line of a unified diff its kind, counting a hunk's lines from its header, so `PlanView` styles a removed `--- x` line as removed. Standard library only.

**Animation.** `StepProgress` and `ConfirmCountdown` are pure: the caller advances `StepProgress.Frame`, or counts `ConfirmCountdown.Remaining` down, and renders again. With `Env.Animate` false (no terminal, `TERM=dumb`, plain mode) the spinner is replaced by `Glyphs.Running`, and the caller prints each step's line once, as brief 1C asks (one plain line per step), and the countdown's notice once. At a given width both keep their height from one render to the next, `StepProgress` by cutting each step to one line and `ConfirmCountdown` by wrapping its first sentence around stand-ins as wide as "Warning:" and "99:59", so that the caller redraws them in place.

**Words** (proposal [P-24](../refactor/proposals.md#p-24)). The step states are brief 1B's names and "Skipped". The countdown's sentences are new, after brief §2 and Phase 2.4. `PlanView` and `ErrorReport` add none of their own: "Lockout risk" is the menu's (P-22), "Operation Failed" and the message words are P-23's, and the dry-run footer and the log sentence are origin/main's.

Not applicable, unknown and error by component: a plan change with no title shows "Unknown" (a plan never hides a change); a step has no unknown state, since the engine knows where each step stands, and a step that could not run is failed or skipped, with the reason in its detail; an `ErrorReport` is the error state of the Apply step, and a rolled-back change with no name shows "Unknown" (a report never hides a rollback); the countdown has none.

**Open for later units.**
- What an empty plan says, and whether the flow then asks anything, was `next/p1c-flow`'s: `PlanView` renders nothing, and the flow says "Nothing needs changing." and asks nothing (§9.10).
- Paging a long plan, and opening the full log in `$PAGER` (P-18), are the screen's: both components render in full.
- The countdown's local-console form (brief Phase 2.4: confirm in the same session, with a warning, when hardn is not over SSH), the timeout flag and the command's path are `next/p2-revert`'s, which fills `Remaining`, `Port` and `Command`.

### 9.7 Line reader (`next/p1c-reader`)

`pkg/ui/prompt` is the one reader every prompt in hardn uses (brief §2 and 1C); its package doc is the full statement. It renders nothing, so it has no goldens.

| Part | What it does |
|---|---|
| `New(in, tty)` | A `Reader` over `in`, with `tty` from `term.Env.InputTTY`. For stdin, `Stdin` |
| `Stdin()` | The process-wide `Reader` over `os.Stdin`, built on first use (`sync.OnceValue`) and the same at every call (added by `next/p1c-legacy-input`): the legacy menus, the first-run config prompts and the new screens (`nav.Prompter.In`) read through it, so stdin has one buffer and no line one reader took ahead is lost to another (plan risk R17). It asks whether stdin is a terminal as `term.Detect` does, by reading the terminal's attributes (`IsTerminal` in `github.com/charmbracelet/x/term` v0.2.2, `term_unix.go:14-17`: `TCGETS` on Linux, `term_unix_other.go:9`, and `TIOCGETA` on macOS, `term_unix_bsd.go:9`; build-tagged `tty_*.go`, no terminal elsewhere), so on Linux and macOS it agrees with `term.Env.InputTTY` without importing `term`, which links Lip Gloss |
| `ReadLine` | One line without its ending (`\n` or `\r\n`), otherwise as typed: nothing trimmed, nothing checked. A last line with no ending is returned, and the call after it reports the end. A line over `MaxLine`, 64 KiB (`bufio.MaxScanTokenSize`), is `ErrLineTooLong` and is dropped up to its ending, never cut short |
| `ErrAborted`, `ErrEndOfInput`, `ErrInterrupted`, `ExitCode` | The end of the input (Ctrl-D on an empty line, the end of a pipe or file) returns `ErrEndOfInput` and Ctrl-C `ErrInterrupted`; both wrap `ErrAborted`, and a caller tells them apart with `errors.Is`. `ExitCode` maps any abort to `ExitInterrupted`, 130, brief Phase 3's code for Ctrl-C; a caller that wants another code for the end of the input tests `ErrEndOfInput` first. `pkg/menu`'s legacy reads call it (`next/p1c-legacy-input`) |
| `Flush` | On a terminal Reader, discards typeahead: the terminal's input queue (`TCFLSH` with `TCIFLUSH` on Linux, `TIOCFLUSH` with `FREAD` on macOS, nothing elsewhere; build-tagged `flush_*.go`) and the Reader's own buffer, which it keeps when the terminal flush fails. Never for piped or scripted input |
| `Ask(w, prompt)` | `Flush`, then the prompt and exactly one space in one write to `w`, the `Env.Writer` around stdout, then `ReadLine`. `PromptLine` ends at its colon (C8, DEV-029); the space puts the terminal's own cursor one cell after it. Without a terminal, after an answer ended with Ctrl-D rather than Enter, or after an abort, no echoed Enter ended the line, so `Ask` ends it. On a terminal it takes stdout to be the same terminal (below) |
| `Answer(w)` | `Ask` for a prompt the caller has already written (added by `next/p1c-legacy-input`): `Flush`, `ReadLine` and the same line end. It is for prompts that cannot be handed to `Ask`, such as the legacy menus', which `pkg/style` prints. Its flush follows the caller's write, so a key typed in the instant between the two is discarded too; `Ask` has no such window |
| `Asked` | Whether `Ask` has written a prompt, or `Answer` read the answer to one, whatever the answer (added by `next/p1c-nav`): output then stands above what is written next, so `nav.Run` sets its drawing apart with the section gap (§9.8) |

**Ctrl-C.** The terminal stays in cooked mode (DEV-042), so Ctrl-C is SIGINT. Only while `ReadLine` waits on a file does it relay `os.Interrupt` with `signal.Notify`; `signal.Stop` gives SIGINT back its default action, which ends hardn, before `ReadLine` returns, and an ignored SIGINT stays ignored. It waits with select(2) on the input and on a wake pipe, and reads only once the input is readable, so a call that Ctrl-C ends has read nothing, and no goroutine is left reading that could take the next line from the next call or from a process hardn runs. macOS's poll(2) does not support devices (poll(2), BUGS), hence select on both systems. Rejected:
- a goroutine reading ahead into a channel: it loses nothing, but after an interrupt its read stays blocked on the terminal, and a line it takes just before a flush can slip past the flush;
- `github.com/muesli/cancelreader` v0.2.2, already linked through Lip Gloss and ultraviolet: it cancels a read of a pipe or a terminal (a probe on a macOS pseudo-terminal named `/dev/stdin` took its kqueue path and cancelled), but on Linux epoll rejects a regular file (epoll_ctl(2), `EPERM`), so stdin redirected from a file fails (`cancelreader_linux.go:44-51`); its fallback for other readers drops the data of a read it cancels (`cancelreader.go:53-62`); a cancelled reader stays cancelled (`cancelreader.go:49-51`), so each call would need a new one; and it takes the descriptor with `File.Fd`, which sets the file to blocking mode.

A reader that is not a file, such as a test's `strings.Reader`, is read directly, as on systems other than Linux and macOS: there Ctrl-C keeps its default action.

**Typeahead.** On a terminal, what was typed during a long step, a line not yet ended included, is discarded before the next prompt (brief 1C). The same flush drops a late reply to the background query (§2.3) that has arrived by the first prompt, since it is pending input like any other; that case is not tested with a real reply.

**Without a terminal** (plain mode) the Reader needs nothing special: it never flushes, the end of the input aborts, and `Ask` ends each prompt's line. Whether hardn prompts at all without a terminal is `next/p3-output`'s (brief Phase 3: never prompt without a TTY).

**Stdin a terminal, stdout not** (`hardn | tee hardn.log`): brief Phase 3 rules out prompting only when stdin is not a terminal, but here the terminal echoes the answer and its Enter to itself, not to stdout, so the captured output holds neither and each prompt runs into what follows it. `Ask` does not end the line there, since that would put an empty line on the screen `tee` writes to; where hardn writes its prompts when stdout is not a terminal is `next/p3-output`'s.

**Echo.** The terminal echoes the answer in its own colour; a screen that shows it again uses `AnsweredField`, muted (DEV-043).

**The legacy menus** (`next/p1c-legacy-input`). `pkg/menu`'s `ReadInput`, `ReadKey` and `ReadMenuInput` each read one line with `Stdin().Answer(os.Stdout)`, trimmed of spaces, a menu choice in lower case so that `Q` quits as `q`: a choice, `q` included, and a pause now take effect on Enter. `pkg/config`'s first-run prompts read through `Stdin` too, with `ReadLine`, and print nothing more, as their bufio readers did. On a terminal the first question, "No configuration file found. ...", `Flush`es before it is printed, since it can follow a long step (`LoadConfig` also runs mid-session, from the main menu's status and the user screens, and asks again while no file exists); the four that may follow it are asked back to back and keep what is typed ahead of them, as the bufio reader did. No `stty` runs, and no other reader is left on stdin. Each pause says "Press Enter ..." (DEV-050); no other words change, so the legacy main menu's exit row keeps origin/main's "Press 'q' to exit immediately", now taken on Enter, until DEV-030's menu replaces the screen. The legacy screens have no error path, so an abort ends hardn in the read itself: Ctrl-C or the end of the input at a menu prompt exits with status 130 (`ExitCode`) once the prompt's line is ended, and any other read error is printed and exits with 1, as `cmd/hardn` does for an error. At the config prompts Ctrl-C exits with 130, and the end of the input still takes the default, as before. Where origin/main died of SIGINT (at a pause, a `ReadInput` prompt or a config prompt), hardn now exits with 130: a shell reports 130 for both, but a waiting parent sees a normal exit rather than death by a signal. The new screens will ask through the same `Stdin`, so the legacy screens stay reachable from them on one buffer.

**Dependencies.** `golang.org/x/sys` v0.49.0 (BSD-3-Clause), in the graph already, is now a direct requirement, for `unix.Select` and the flush ioctls; `cancelreader` stays indirect; no module was added. The package imports only the standard library and `x/sys` (`TestImports`), so the legacy menus read through it without linking Lip Gloss and its import side effect (§2.3).

**Tested.** Scripted readers (CRLF, no last line ending, empty lines, `MaxLine` exactly and one over, a runaway input, read errors), split across reads; every case again with the input going on after its end, as a terminal does after Ctrl-D; `Ask`'s writes, scripted and typed at a pseudo-terminal (Enter, Ctrl-D after the answer, Ctrl-D on an empty line); answers queued on an `os.Pipe` through `Ask`, none flushed; a failed flush keeping the buffer; Ctrl-C (SIGINT sent to the test process) during a read of an `os.Pipe` and of a pseudo-terminal, then the next line, with a direct read of the pipe proving no read outlived the call; SIGINT's default action after a read (a child process must die of it), and an ignored SIGINT left ignored (another child); the flush on a pseudo-terminal in cooked mode, against a Reader that does not flush. Since `next/p1c-legacy-input`: `TestAnswer` (its writes and its flush, scripted), `TestStdin`, `TestIsTerminal` (a pseudo-terminal, a pipe, a regular file, a closed file, a `strings.Reader`); and the legacy reads through their entry points in `pkg/menu` (`TestReads`, `TestEnterRequired`, `TestAbort`, `TestInterrupt`, `TestScreens`, and `TestSharedStdin`, a child test process whose one pipe holds a config answer and a menu choice) and `pkg/config` (`TestShouldCreateDefaultConfig`, with a terminal Reader's typeahead dropped, `TestReadAnswer`, typeahead kept between the back-to-back questions, and `TestInterrupt`). All on macOS: Linux's `TCFLSH`, `TCGETS` and the pseudo-terminal helper are cross-compiled and vetted (`GOOS=linux go vet`, `go test -c`), not run, and no terminal emulator was used. `make check`'s `check-cross` builds `cmd/hardn`, which links this package through the legacy menus, for linux/amd64, linux/arm64 and darwin/amd64, and since `next/p1d-lint` vets the whole module for linux/amd64 and linux/arm64 with `go vet`, which type-checks the test files too, so `pty_linux_test.go` compiles in `make check` on macOS (§9.15).

**Open for later units.** `next/p1c-nav` matches lines, and the end of the input never goes back (§9.8); `next/p3-output` decides the exit code for a script that ran out of answers (until then the legacy menus end with `ExitCode`'s 130, and the config prompts take their defaults, above) and where prompts go when stdout is not a terminal (above); Ctrl-C outside a read, during an apply, is the engine's (Phases 2 and 3).

### 9.8 Navigation grammar (`next/p1c-nav`)

`pkg/ui/nav` decides what each typed line does at a prompt, the same at every level (brief 1C), and asks until a line is accepted; its package doc is the full statement. The inputs are data in `pkg/ui/keys` (`Back`, `Quit`, `Navigation`, `YesNo`), defined once there because the components' tests use them and cannot import `nav`, and because `keys` imports only the standard library, so the legacy menus can use them without Lip Gloss. A `nav.Prompter` holds the render context, the one `prompt.Reader` over stdin and the `Env.Writer` around stdout. It renders nothing of its own: menus ask with `PromptLine`, questions with `AnsweredField` (no answer yet, so the line reads as Figma draws it once answered), and a refused line with `Message`, so no Lip Gloss helper applies.

| Line | At | Does |
|---|---|---|
| a number | a menu (`Choose`, `Run`) | selects the item with that key |
| `0` or `b` | every prompt | goes back (`ErrBack`); at the top level, exits. On a menu `0` is also the Exit or Return item's number, shown as "0)" |
| `q` | every prompt | quits hardn (`ErrQuit`, returned level by level to the top) |
| `y`, `yes`, `n`, `no`, or Enter | a y/N question (`Confirm`) | the answer; Enter takes the default, the capital in the hint |
| the phrase | a typed confirmation (`Typed`) | confirms: exactly, case included |
| any other line | a free-text question (`Text`) | the value, the white space around it removed |
| end of input, Ctrl-C | every prompt | aborts (`prompt.ErrAborted`, exit 130), never back |
| anything else | every prompt | an error `Message` under the prompt, then the prompt again; nothing above is drawn again |

Lines match as `keys.Set.Match` compares them: the white space around a line ignored, letters in any case, numbers as typed (`01` is not `1`).

**Hints**, generated from what each prompt accepts, list the prompt's own inputs, then the navigation inputs, so these end every hint alike (DEV-044, DEV-045): the main menu `[0-10, b or q]`, a sub-menu `[0-2, b or q]` (with item 1 disabled, `[0, 2, b or q]`), a y/N question `[y/N, 0, b or q]` (`[Y/n, 0, b or q]` when yes is the default), a typed confirmation `[bos-san-l01, 0, b or q]`, free text `[0, b or q]`. On a menu `0` is one of the menu's own keys, the Exit or Return item's, so it is not repeated.

**Decisions:**
- **0, b and q at every prompt**, as brief 1C ("the same at every level: ... `0` or `b` goes back") and the plan's p1c-nav row ("`0`/`b` back ... identical back/quit on the main menu, a sub-menu and a workflow step") have it. A `0` typed at a y/N question goes back, and is never taken for "no". The first build took `0` as back on menus only; that narrowing changes the brief's meaning, so it is now P-25 (b)'s alternative for the maintainer.
- **Back at the top level** exits, so `0` and `b` are accepted and shown on the main menu too: no input is accepted that its hint leaves out (DEV-044).
- **q from a sub-menu quits hardn**, as brief 1C decides; in origin/main it returned to the parent menu ([interaction.md](../refactor/phase0/interaction.md) §5, anti-pattern 7), so a user who types `q` from habit there now leaves hardn (DEV-044).
- **Free text:** `0`, `b` and `q` navigate there too, so a value can never be `0`, `b` or `q`, and the hint `[0, b or q]` says so. Linux allows the user names `b` and `q`; hardn's forms cannot create them. origin/main's free-text fields take no `0` as a value (their numbered prompts start at 1, and one already takes "0 to cancel", interaction.md §5, anti-pattern 7). That changes meaning, so it is built pending P-25 (a).
- **Explaining the navigation inputs:** only the main menu's Exit row says "q to quit" (DEV-030); nothing says what `0` and `b` do, and in the brackets where `[y/N]` lists answers, `1. Username [0, b or q]:` can read as if the name must be one of them. Built as it stands, pending P-25 (e), which offers a help line (`keys.Set.Help`: "0 or b to go back, q to quit") or a hint that sets the navigation apart.
- **A typed phrase** is shown as the answer the hint offers, and always whole: the prompt cuts the label first, and `Typed` asks only when the whole hint shows after some of the label. Otherwise it returns `ErrPhrase` before writing anything, and the caller asks with a shorter phrase or label (P-10); `CheckTyped` returns the same error without asking, so that a caller writes nothing above a prompt that is never asked (added by `next/p1c-flow`). With the label "Type the hostname" that is a phrase of up to 45 cells from 64 columns up and 41 at 60 in Unicode glyphs, 43 and 39 in ASCII (`TestTypedWhole`); Linux allows a hostname of 64 bytes (`HOST_NAME_MAX`), so a long one needs another phrase, such as its first label. `CheckPhrase` refuses, at any width, a phrase that is empty, not printable ASCII (components drop control characters and map typographic quotes, C12, so it would show other than it must be typed), has a space at either end, is `0`, `b` or `q` in any case (it could never be typed), or holds a comma, a slash, a bracket or " or " (the hint would read two ways). The layout is built pending P-25 (d).
- **A disabled item** stays out of the hint, as P-22 has it; its key typed anyway is answered "<Label> is unavailable.". `nav` decides which items are shown as the menu does (`component.MenuItem.Shown`).
- **The end of the input** aborts at every prompt and never goes back, so a script that runs out of answers ends as an abort rather than unwinding to the top and exiting 0.
- **Words:** origin/main's "Invalid option. Please try again." on a menu; new (P-25 (c)): "<Label> is unavailable." for a disabled item, "Invalid answer. Please try again." at a y/N question and for the one line free text refuses (longer than `prompt.MaxLine`), and "That does not match exactly. Please try again." at a typed confirmation. That one does not repeat the phrase: the prompt under it shows the phrase whole, while the message's prose would wrap at a hyphen inside it (x/ansi `wrap.go:270-276`: a hyphen is always a breakpoint). The labels of confirmations are the flow's (P-10).

**Menus bound to actions.** A `nav.Menu` pairs each `component.MenuItem` with its `Action` (`nav.Option`), after arch3's option ↔ action binding (refactor plan §6), as an idea only. The one list gives the items rendered (`View`), the accepted inputs and their hint (`Accepts`), and the dispatch, which matches against the same bindings the hint comes from, so no key reaches an action the menu does not show (origin/main's hidden options, [interaction.md](../refactor/phase0/interaction.md) §1 item 4). `Check` refuses a shown item with no action, a key that is not a whole number from 1 or is used twice, and an exit item not numbered 0. `Run` draws a menu screen (what is above the menu, the menu, then the prompt after `PromptGap`), asks, and runs the chosen action, until the user goes back (it returns nil) or quits (`ErrQuit`); the next round draws again below what the action wrote, clearing nothing. Once any prompt has been asked (`prompt.Reader.Asked`), each drawing starts after `SectionGap` blank lines, so a sub-menu stands apart from the answer that opened it and the menu drawn again from the result above it; only the first drawing of a run starts at once. An action starts on the line under its answer, so one that writes, such as a result `Message` or a flow's first step, writes the gap first, as `Run` does.

**Tested.** Matching and `Check` table-tested; the loops on scripted input, a typist that echoes each line as a terminal would: `TestGrammar` puts `0`, `b`, `B`, `q`, `Q` and the end of the input to a main menu, a sub-menu, a y/N question, a typed confirmation and free text; `TestRun` nests a sub-menu, goes back and quits from it; `TestRunGap` checks the gaps of a nested run; `TestTypedWhole` every phrase length from 1 to 64 at 40, 60, 64, 80 and 120 columns in both glyph sets, `CheckTyped` refusing exactly what `Typed` refuses. Goldens on `Core()`, each asked a line it refuses: `nav/main-menu` (left with `0`, which exits), `nav/sub-menu` (a disabled item; left with `q`), `nav/workflow-step` (free text, then y/N, left with `0`), `nav/typed-confirmation` (left with `q`) and `nav/rounds` (a result, the menu again, a sub-menu left with `b`, then `q`): so `0` or `b` and `q` are on record on the main menu, a sub-menu and a workflow step alike. No real terminal was used: the reader's own tests cover the terminal (§9.7).

**Open for later units.** `next/p1c-form` asks its fields with the same loop, exported as `Prompter.Ask`, and refuses a value its validation rejects with `Refuse`, so a validation error looks like any refused line; it added no style for it (§9.9); `next/p1c-flow` words the confirmations, chooses y/N or typed per step, picks a phrase `Typed` can show whole, and writes the gap before its first step (built: §9.10); `next/p1d-slice` composes what `Run` draws above the menu, including the breadcrumb sub-screens show (brief 1C; P-08 decides its use), as `Screen.Above`, and added `Screen.Compact` for the main menu's two-column menu (built: §9.12); `next/p3-output` decides the exit codes of a quit and of a script that ran out of answers.

### 9.9 Forms (`next/p1c-form`)

`pkg/ui/form` collects a flow's answers, the Collect step of brief 1C's canonical flow, after huh's patterns, none of huh imported; its package doc is the full statement. A form only collects: it has no actions, so no prompt is interleaved with a change, and the caller acts on the answers only once `Run` returns nil.

| Part | What it is |
|---|---|
| `Form{Fields}`, `Run(p nav.Prompter)` | The fields asked in order. nil once the last is answered; `nav.ErrBack` when the user goes back from the first field asked; `nav.ErrQuit`; `ErrAborted` (`prompt.ErrAborted`) at the end of the input or Ctrl-C; `ErrField` for a field that cannot be asked (a nil field, no `Value`, no title, a `Select` with no options or an option with no label). On any error every `Value` is put back as it was before `Run` |
| every field | `Title`, or `TitleFunc`, called each time the field is asked; `Description`; `Validate func(T) error`; `Skip func() bool`; `Value *T`, the typed binding |
| `Text` | Free text: the line without the white space around it, an empty line giving `Default`. `0`, `b` and `q` navigate there too (P-25 (a)) |
| `YesNo` | A y/N question (`keys.YesNo`): y, yes, n or no in any case, an empty line giving `Default`, the capital in the hint |
| `Select[T]`, `Option[T]` | A numbered choice the form collects: `Options`, or `OptionsFunc` called each time, listed as a menu's items keyed from 1; the number of one is taken, at a prompt that asks "Enter your choice", as a menu's does. No `Default`: a numbered hint cannot mark one, and Enter chooses nothing, as on a menu. A choice that opens a screen of its own is a `nav.Menu`, not a `Select` (**Choices** below) |
| `AnsweredField.ShowsWhole` (`pkg/ui/component`) | Whether the field, asked with no answer, shows its number, question and hint uncut on one line at the context's width; the form asks a question that would be cut under a `FieldHead` instead. `TestShowsWhole` holds it to `Render` at every width from 1 to 130 |
| `FieldHead` (`pkg/ui/component`) | View-model `Number`, `Title`, `Description`, `Options` (`MenuItem`). The title numbered as an `AnsweredField`'s question but with no hint and no colon, on a line of its own, cut with the ellipsis only when that line is too narrow; the description in `Field.Description`, wrapped from col 6; then `SectionGap` blank lines and the options as `Menu` rows, stacked as a menu stacks. States (goldens on `Full()`): `description`, `options`, `both`, `unnumbered`, `title`, `long`, `empty` (no title: nothing). Lip Gloss helpers: none, as for `Menu` and `Message`: spans, `lipgloss.Wrap`, then `Menu` |

**One field structure** (DEV-046; brief 1C: one structure, defined by Figma, for every kind of field). A text field refused by its validation, then answered; a choice with a head; a y/N question whose default Enter took. Plain, with the trailing spaces after an empty answer left out:

```text
0 2   6
   1. Username [0, b or q]: George
  [!] Error: Username must start with a lowercase letter and
      contain only lowercase letters, numbers, hyphens, and
      underscores
   1. Username [0, b or q]: george

   2. Select a user

   1) george
   2) alice

  > Enter your choice [1-2, 0, b or q]: 2
   2. Select a user: alice

   3. Grant sudo privilege? [y/N, 0, b or q]:
   3. Grant sudo privilege? [y/N]: n
```

- **Asked on one line** when the field has neither description nor options, and the line shows its question and hint whole (`AnsweredField.ShowsWhole`): an `AnsweredField` with no answer, as `nav` asks a question and as Figma draws its form.
- **Asked on a prompt line of its own** when it has either, or when the one line would cut its question: the `FieldHead`, `PromptGap` blank lines, then a `PromptLine`. A `Select`'s says Figma's "Enter your choice", as Figma's menus do (origin/main's say "Enter selection", P-29 (a)); a text or y/N field's has no label, since the question is the line right above and no Figma or origin/main screen words such a prompt. Its hint is the field's inputs, then the navigation's. The prompt must be the last line written, so whatever a field shows besides its question goes above the prompt.
- **Narrow terminals and long titles:** on one line the hint is kept whole and the question is cut first, then dropped, so the form moves such a question under its head, where the title has the content width to itself: at 40 columns "Grant sudo privilege?" is asked as `   2. Grant sudo privilege?`, a blank line, then `  > [y/N, 0, b or q]:`; and so, at 80 columns, is origin/main's "Enter new username (leave empty to keep current)" in a numbered form. The title is still one line, cut when even that is too narrow (below 27 columns for "Grant sudo privilege?"), so words a user must read whole, such as what a default is, belong in the `Description`, which wraps. `TestNarrow` checks the create-user form at every width from 20 to 130. A form can so mix one-line fields and fields under a head; at the core widths, 60 and 80, every field of Figma's form is one line.
- **Refused:** a line the field does not take, in `nav`'s words ("Invalid answer. Please try again." at text and y/N, origin/main's "Invalid option. Please try again." at a choice), or a value its `Validate` rejects, in the validation's words (such as origin/main's username rule), is answered under the prompt by an error `Message`, and the prompt alone is asked again: the head is not written again (brief 1C: re-ask only that field, keeping context). This is `nav`'s loop, exported as `Prompter.Ask`, with `Refuse` for the validation's reason; the inline error has no style of its own.
- **Answered:** the terminal's echo shows the answer (DEV-043). The form writes the field again as an `AnsweredField` with the answer, muted, under it only when the line typed does not read as the answer: after a prompt line of its own, where a `Select`'s echo names the option rather than its number, and after an empty line that took a default (DEV-047). The echo keeps a y/N's `[y/N]` and drops the navigation, as Figma's answered line shows `(y/n)`.
- **Numbers:** in a form of more than one field, by place among the fields asked, so a skipped field takes none and the numbers run on (DEV-047). Every field asked is numbered, even when `Skip` leaves only one: its number is shown as it is asked, before the later fields' `Skip`, which may read its answer, can tell. A form of one field is unnumbered, as Figma's single questions are (USER-MANAGE_SUDO-CONFIRMATION, SSH_LOGIN-TOGGLE_CONFIRMATION).
- **Gaps:** `SectionGap` blank lines before each field once any prompt has been asked (`prompt.Reader.Asked`), as Figma spaces its form and as `nav.Run` sets a drawing apart, so a form run by a menu's action starts apart from the answer that chose it. Before any prompt, `Run` cannot tell whether anything stands above its first field and writes no gap there. So the rule for `next/p1c-flow` and `next/p1d-slice`: a caller that draws something above a form, such as the "Create User" title bar Figma sets a blank line under, ends it with `SectionGap` blank lines itself when `Asked` reports false, and leaves them to `Run` when it reports true.
- **Choices: menu or form.** A choice that opens a screen of its own is a `nav.Menu` screen, drawn as Figma draws one (an underlined `MenuHeading`, the items, "0) Return", "Enter your choice"): Figma's "Select a user" (USER-MANAGE_USER-SELECT_USER), whose choice opens that user's management menu, is one, built in `next/p1d-slice`. `form.Select` is for a choice a flow collects and acts on after its review, such as origin/main's "Select a key to remove" (pkg/menu user_options.go:502), which no Figma screen draws. It keeps the form's field structure: its title is a field's question, numbered with the others, not a menu heading; Figma's blank line between heading and items is kept; it has no Return row, since `0` in a form goes back to the field before; and its prompt keeps Figma's "Enter your choice" (DEV-046).
- **Plain mode** (huh's accessible mode): the same definitions through `term.Env`, with no colour, ASCII glyphs and nothing animated (the `plain-80` cell of every form golden).

**Values and navigation:**
- A `Value` is bound as soon as its field takes a line, so a later field's `TitleFunc`, `OptionsFunc` and `Skip` read the earlier answers from the caller's own variables. On any error every `Value` is put back as it was before `Run`, and a skipped field is put back each time it is skipped, so an answer given on a path the user then left does not stay.
- `0` or `b` goes back to the field asked before, asked again from the start: its earlier answer stays on screen above, but Enter takes the field's `Default`, not that answer. From the first field asked, it leaves the form (`nav.ErrBack`). Built pending P-26 (a) and (b). `q` quits and the end of the input aborts from any field.
- `Skip` is called again when back returns to its field, so it should read only the fields before it and the host.
- A text field's `Default` shows before the answer only where the screen's words say so, as origin/main's labels do; the echo shows it after. Built pending P-26 (c). A title is one line: "Enter new username (leave empty to keep current)" shows whole as a numbered field's title from 54 columns, asked under its head, since beside the hint it needs 67 cells; but "Enter filename to export system status (default: system_status.txt)", 67 cells, never does under the 64-cell content cap, so a screen that must show such words whole puts them in the `Description`.
- `Validate` may read the system, such as whether a user exists, and must change nothing.

**Tested.** Scripted readers, a typist echoing each line as a terminal would (as `nav`'s tests): Figma's create-user form answered as Figma shows it; a value refused by its validation, then fixed, at a text field with a head (the head written once), a y/N question and a choice; refusals in `nav`'s words; defaults taken by Enter and their echo; skip rules, a skipped field put back and the numbers running on; `TitleFunc` and `OptionsFunc` called again after back; `b`, `q` and the end of the input at every field of the form, with every value put back on failure, and `0` as `b`; fields that cannot be asked (`ErrField`, nothing written for them, a nil field included); a failed write at the prompt (the reader's), and at the gap and the echo (the form's own, `form: write`); when a field is asked on one line and when under its head (`TestLayout`, a description that shows nothing staying on one line), and the create-user form at every width from 20 to 130 (`TestNarrow`). Goldens on `Core()`: `form/text`, `form/text-description`, `form/yes-no` and `form/select` (its options set apart and its prompt labelled since the review fixes), each asked, refused and answered; `form/create-user`; `form/back` (back from field 3 to 2, whose new answer skips 3, so the key question is 4). No real terminal was used: the reader's own tests cover the terminal (§9.7).

**Open for later units.** `next/p1c-flow` runs a form as its Collect step, then reviews the answers (Figma's "User Configuration" list), plans and confirms; the confirmation Figma numbers as a form's last field ("5. Create user 'george'?") is the flow's Confirm step there, unnumbered (P-10, §9.10). Whether hardn prompts at all without a terminal, and whether a form then echoes each answer for a log, is `next/p3-output`'s. No screen uses a form before `next/p1d-slice`.

### 9.10 Canonical flow (`next/p1c-flow`)

`pkg/ui/flow` runs brief 1C's canonical action flow, which every action that changes the system follows: Collect, Review, Plan, Confirm, Apply, Return. Its package doc is the full statement. It renders nothing of its own: each step is written as components, through the `Prompter`'s writer (`Env.Writer` around stdout), and asked through its reader, so no Lip Gloss helper applies.

| Part | What it is |
|---|---|
| `Flow{Form, Review, Engine, Confirm, DryRun}`, `Run(ctx, p nav.Prompter)`, `Check(ctx)` | One action. `Run` returns an `Outcome` once a result is on screen, with a nil error unless writing it failed, so a menu's action carries on after a failure; and `NotRun` with an error when the flow ends before a result: `nav.ErrBack` from the form's first field, `nav.ErrQuit`, `prompt.ErrAborted`, the context's error before Apply, `ErrFlow` (no `Engine` or `Confirm`, or a typed confirmation with neither label nor question), `form.ErrField`, `nav.ErrPhrase` (a phrase `CheckPhrase` refuses), a failed write. A typed confirmation too wide for the terminal is a result, not an error. `Check` returns what `Run` refuses before it asks or writes anything (`ErrFlow` for no `Engine` or `Confirm`, the context's error), for a caller that draws above the flow |
| `Outcome` | `NotRun`, `Applied`, `Failed` (planning or a step), `Cancelled` (declined, or too narrow to confirm), `Planned` (dry-run), `Unchanged` (no changes), for `next/p3-output`'s exit codes |
| `Review{Notice, Title, Items}`, `Notice{Callout, Items}` | The Review step, built from the answers once collected: the action's notice, if any, a `Callout` and its checklist as bullet items outside the bar, as Figma's Warning before disabling root SSH login; then a `SectionHeader`, `SectionGap` blank lines and a `BulletList` of `KeyValue` rows, as Figma's "User Configuration". No rows, no section; each block after `SectionGap` blank lines |
| `Engine`: `Plan(ctx) (Plan, error)`, `Apply(ctx, Report) error` | The minimal interface the Phase 3 engine (`next/p3-engine`) will satisfy, through an adapter per action holding its answers: `Plan` turns its changes into `PlanChange`s in the order it applies them; `Apply` applies them, one step each, reports each running, then done, and on the first failure reports it failed, rolls back the steps done in reverse, reporting each, and returns an error, a `*Failure` for the report's command, output, log path and next step. `Report` may be called from any goroutine |
| `Plan{View, Risk}`, `Plan.Typed` | The `PlanView` and the risk class, `RiskLow` (y/N) or `RiskLockout` (typed: lockout risk, and Run All); a plan with a lockout-risk change is typed whatever its class, so it never asks less than it shows |
| `Confirm{Question, Label, Phrase, Short}` | What the Confirm step asks, from the definition, given the plan: the action's question, such as Figma's "Create user 'george'?", asked as y/N, or shown on a line of its own above a typed confirmation; the typed confirmation's label and phrase; and a short phrase, such as a long hostname's first label, asked only when the phrase does not show whole (P-10 words them) |
| `Fixture` | An `Engine` that touches nothing: a fixed plan, applied by reporting each step, failing at `FailAt` with a `Failure`. Phase 1 runs only on it; nothing in the package reads or changes the system |

The failure path, plain, after the create-user form (§9.9), with `[...]` for the plan's lines:

```text
0 2   6
# User Configuration -------------------------------------------

  [ ] Sudo                 Enabled, No password
  [*] SSH Keys             None

   -  Create user george
      $ useradd --create-home --shell /bin/bash george
  [...]

  Create user 'george'? [y/N, 0, b or q]: y

  [+] Done: Create user george
  [!] Failed: Add george to the sudo group (exit status 6)
  [-] Skipped: Let george use sudo without a password

# [!] Operation Failed
# Adding george to the sudo group failed: exit status 6

  [<] Rolled back: Create user george
  [...]
```

**Decisions** (DEV-048, DEV-049; P-27 puts the flow's own to the maintainer):
- **Collect** is the form, run to its end before anything is planned, so no prompt is interleaved with a change.
- **Review:** the action's `Notice`, such as Figma's Warning and checklist before disabling root SSH login, then the answers' section; both built after the Collect step. The title bar and a subject line such as "Managing sudo method for: george" name the screen, so the workflow screen draws them above the flow (`next/p1d-slice`).
- **Plan:** dry-run stops after the plan, whose footer says "No actual changes were made to your system." (origin/main's). A plan with no changes says "Nothing needs changing." (brief Phase 3's words), an info `Message`, and asks nothing (P-27 (b)). A plan that cannot be made is an `ErrorReport` with nothing rolled back. The plan follows the review with no heading of its own (P-27 (c)).
- **Confirm:** a y/N question is asked as a one-field form, unnumbered, its default no, so an Enter that took the default is echoed (DEV-047); Figma numbers it as the form's fifth field, but it now comes after the plan (DEV-048, P-10). A plan `Typed` reports is confirmed by typing the phrase (`nav.Prompter.Typed`, P-25 (d)), under the action's question, which keeps its line (a `FieldHead`, then `PromptGap` blank lines), so the label stays short; one with no phrase is refused with `nav.ErrPhrase`, never asked as y/N. A phrase too long to show whole at the width, such as a hostname over 45 cells at 64 columns (§9.8), is asked by the short phrase the action gives, `Confirm.Short`, such as its first label; the flow never cuts a phrase itself, so 192.0.2.123 is never confirmed by 192. When neither shows, for "Type the hostname" and "bos-san-l01" below 30 columns in Unicode glyphs and 32 in ASCII and plain mode, nothing is asked or written for it: "Operation Cancelled" says "The terminal is too narrow to confirm. Widen it and try again.", and `Run` returns `Cancelled` with no error, so the menu carries on (P-27 (e)). Every check is made with `nav.Prompter.CheckTyped` before the gap is written. n, and 0 or b, decline: Figma's "Operation Cancelled" callout, nothing applied (P-27 (a)); q quits and the end of the input aborts.
- **Apply:** one step per change, titled by it. With `Env.Animate`, and fewer steps than the terminal has lines, the `StepProgress` block is drawn in place, moving up over it with CPL and erasing each line with EL, the spinner turning every 100 ms while a step runs; otherwise each step's line is written once it settles, done, failed or skipped (DEV-049, P-27 (d)). Steps never reached after a failure are skipped. Then Figma's "Operation Successful" callout, or an `ErrorReport` naming each change reported rolled back, in the order undone (DEV-040). A step whose title `StepProgress` would leave out, such as a zero-width space alone, is titled "Unknown", as `PlanView` shows the change. The terminal's echo stays on, so an Enter typed while a step is drawn in place moves the cursor and leaves the block's first line stale above the redrawn steps: a terminal limit (DEV-049), whose remedies P-27 (d) puts to the maintainer.
- **Return:** `Run` returns; nothing is cleared, and no continue prompt is asked (P-09): `nav.Run` draws the menu again under the result, its status read again.
- **Gaps:** each block after `SectionGap` blank lines once anything stands above it (a prompt asked or a block written), as for a form (§9.9): a caller that draws a title bar above a flow that asks nothing first ends it with the gap itself while `Asked` is false, and checks the flow (`Check`) before it draws.
- **Concurrency:** the spinner turns on a goroutine of its own; it and `Report` write under one lock, each drawing in one write, and stop before `Run` writes again.

**Tested.** Scripted readers, a typist echoing each line, and fixture engines: each path (`TestPaths`: applied, cancelled by n, by the default and by 0, dry-run, failed at a middle step and at the last, failed to plan, unchanged, typed after a refused line, typed for a lockout-risk change in a low-risk plan, under the question and with the question for its label, y/N with no lockout risk, back at a typed confirmation, a 57-cell hostname asked by its short phrase, and a first title of a zero-width space, a bell or a NUL); `TestReview`, the notice's and the section's blocks; `TestAbort`, nothing applied for q and the end of the input at every prompt of the create-user flow and at a typed confirmation, b at the first field, and a context done before the run, while planning and before applying; `TestRefused` (no engine, no confirm, no phrase, a short phrase that navigates, neither label nor question; nothing written for the Confirm step); `TestNarrow` (29 and 30 columns in Unicode glyphs, 31 and 32 plain, a dotted phrase never cut, a short phrase at 30); `TestReturn`, a flow as a menu's action, and one too narrow to confirm; `TestWriteFails`; `TestProgress`, the animated screen emulated (CPL, EL, SGR) against the lines written without animation, the spinner turning on its own, an Enter echoed while a step runs, and a terminal too short for the steps. Goldens on `Core()`: `flow/applied`, `flow/cancelled` (Figma's frame), `flow/dry-run`, `flow/failed`, `flow/typed` (Figma's root SSH login, at lockout risk, with its warning and question) and `flow/unchanged`; the gallery's workflow mocks play the same paths under their screens, step by step (§9.12, built in `next/p1d-gallery-screens`). No real terminal was used; the race detector ran on the package.

**Open for later units.** `next/p1d-slice` draws the workflow screen around a flow (built: the header and breadcrumb, P-08, §9.12) and `next/p1d-gallery-screens` steps through the sudo, root-login and create-user sequences on `Fixture`s (built: §9.12); `next/p2-revert` adds the confirm-or-revert countdown after a lockout-risk apply; `next/p3-engine` satisfies `Engine`; `next/p3-output` maps `Outcome` to exit codes and decides whether a flow runs without a terminal (`--yes`).

### 9.11 Components and their states

Every component of `pkg/ui/component`, the section that records it, and its golden states (`<component>/<state>.golden` on `Full()`) by brief 1B's five states. A dash is a state the component does not have, for the reason its section gives: hardn always knows its own mode and version, an engine knows where each step stands, a value hardn could not read, a failed read included, is the unknown state, and a failure is a `Message` or `Callout` of kind bad or an `ErrorReport`. "As unknown" is a state the component shows as its unknown state: an empty `ContextBar`, `HostBlock`, `RiskSummary` or `StatusRow` (its zero value) has nothing hardn read, and shows "Unknown".

| Component | § | Normal | Empty | Unknown | Not applicable | Error | Also |
|---|---|---|---|---|---|---|---|
| `Header` | 9.2 | `normal` | `no-version`, `no-url` | – | – | – | |
| `ContextBar` | 9.2 | `normal` | as unknown | `unknown` | – | – | `no-codename`, `long` |
| `TitleBar` | 9.2 | `normal` | `empty` | – | – | – | `long` |
| `HostBlock` | 9.2 | `normal` | as unknown | `unknown-address`, `unknown-uptime`, `no-hostname`, `all-unknown` | – | as unknown | `long-hostname`, `reboot-pending` |
| `RiskSummary` | 9.2 | `moderate`, `low`, `high` | as unknown | `unknown` | – | as unknown | `factors`, `factors-no-gloss`, `long-factor` |
| `StatusRow` | 9.3 | `configured`, `not-configured`, `partial` | as unknown | `unknown` | `not-applicable` | as unknown | `word`, `long-label`, `long-detail` |
| `StatusList` | 9.3 | `normal` | `empty` | per row | per row | per row | |
| `Divider` | 9.3 | `normal` | – | – | – | – | |
| `ModeIndicator` | 9.3 | `live`, `dry-run` | – | – | – | – | |
| `MenuHeading` | 9.4 | `normal` | `empty` | – | – | – | `no-mode`, `long` |
| `Menu` | 9.4 | `main`, `sub`, `compact` | `empty` | – | `disabled` | – | `risk`, `long` |
| `PromptLine` | 9.4 | `normal` | `empty` | – | – | – | `no-hint` |
| `Badge` | 9.5 | `normal` | `empty` | – | – | – | `long` |
| `SubjectLine` | 9.5 | `normal` | `empty` | – | – | – | `long-label`, `long`, `no-label` |
| `SectionHeader` | 9.5 | `normal` | `empty` | – | – | – | `long` |
| `Breadcrumb` | 9.5 | `normal`, `deep` | `empty` | – | – | – | `long` |
| `KeyValue` | 9.5 | `plain`, `ok`, `warn`, `bad`, `note`, `item` | `empty` | `unknown` | `not-applicable` | – | `long-key`, `long-value` |
| `BulletList` | 9.5 | `user`, `ssh`, `checklist` | `empty` | per row | per row | per row | `long` |
| `Message` | 9.5 | `ok`, `warn`, `info`, `cancelled` | `empty` | – | – | `bad` | `long` |
| `Callout` | 9.5 | `warn`, `ok`, `cancelled`, `info`, `no-title` | – | – | – | `bad` | `long` |
| `AnsweredField` | 9.5 | `text`, `yes-no`, `confirm`, `unanswered`, `unnumbered` | `empty` | – | – | – | `long-answer` |
| `FieldHead` | 9.9 | `description`, `options`, `both`, `unnumbered`, `title` | `empty` | – | – | – | `long` |
| `PlanView` | 9.6 | `normal`, `lockout-risk`, `dry-run`, `new-file`, `no-newline` | `empty` | `unknown` | – | – | `long` |
| `StepProgress` | 9.6 | `apply`, `animated`, `skipped` | `empty` | – | – | `failed` | `long` |
| `ErrorReport` | 9.6 | `normal`, `tail`, `nothing-rolled-back` | `empty` | `unknown` | – | the component itself | `long` |
| `ConfirmCountdown` | 9.6 | `waiting`, `port`, `urgent`, `zero`, `command` | – | – | – | – | |

### 9.12 Screen layouts (`next/p1d-slice`)

`pkg/ui/screen` holds brief 1B's three layouts; its package doc is the full statement. A layout is a view-model of component view-models with a pure `Render(c component.Ctx) string`: its blocks in one order, each after `SectionGap` blank lines, and a block that renders nothing left out with its gap, as an empty `StatusList` is. A menu layout ends with its `PromptLine` after `PromptGap` blank lines, its hint generated from the menu's keys and `keys.Navigation`, as `nav.Menu.Accepts` generates it. Every screen is one of the three filled with data; a screen that fits none is a proposal for a new layout (brief 1B). Every Figma frame with a menu or an action fits one; SSH_LOGIN-SSH_SETTINGS_DISPLAY, which only shows, fits none as built (P-28, below).

| Layout | View-model | Blocks, in order | Figma frames that use it | Goldens (`Core()`) |
|---|---|---|---|---|
| Main menu | `MainMenu`: `Header`, `Context`, `Host`, `Risk`, `Status`, `Heading`, `Mode`, `Menu`, `Prompt` | `Header`, `ContextBar`, `HostBlock`, `RiskSummary`, `StatusList`, `Divider`, `MenuHeading` with the `ModeIndicator`, `Menu`, `PromptLine`; on a short terminal the compact form below | 00-TUI-MAIN | `main-menu/00-tui-main`, plus 80x40 on both themes |
| Sub-menu | `SubMenu`: `Header`, `Breadcrumb`, `Subject`, `Status`, `Sections` (`Section`: `Header`, `Badge`, `Rows`), `Heading`, `Menu`, `Prompt` | `Header`, `Breadcrumb`, `SubjectLine`, `StatusList`, each `Section` (`SectionHeader`, `Badge`, `BulletList`), `MenuHeading` with no mode, `Menu`, `PromptLine` | 01-USER, 02-SSH_LOGIN, 05-BACKUP (status rows and a section); USER-MANAGE_USER-MANAGEMENT, USER-SSH_KEYS (a subject line and a menu); USER-MANAGE_USER-SELECT_USER (users under "Select a user"); origin/main's DNS screen, inferred (sections) | `sub-menu/01-user`, `02-ssh-login`, `03-dns`, `05-backup`, `user-manage-user-management`, `user-manage-user-select-user`, `user-ssh-keys` |
| Workflow step | `WorkflowStep`: `Header`, `Breadcrumb`, `Subject`; `Run(ctx, p, f)` | `Header`, `Breadcrumb`, `SubjectLine`, then one run of the canonical flow, which writes its steps and asks its prompts (§9.10) | USER-MANAGE_SUDO-CONFIRMATION, USER-CREATE_USER, SSH_LOGIN-TOGGLE_CONFIRMATION | `workflow-step/<screen>/<sequence>/<n>-<step>`: the sudo change's `normal`, `dry-run` and `failure` sequences, the root-login toggle's `normal` one and the create-user form's `cancelled` one, Figma's frame |

**The other screens.** SSH_LOGIN-SSH_SETTINGS_DISPLAY shows a section and ends with a continue pause, with no menu and no action, and fits no layout as built: the sub-menu layout always ends with a menu's `PromptLine`, which with no menu reads "> [0, b or q]:", a prompt the frame does not have, and the workflow step runs an action's flow. P-28 puts it to the maintainer with P-09: if the pause goes, it is a sub-menu whose menu is "0) Return" alone, or an action's result under the SSH Login menu, and needs no new layout; if P-09 keeps the pause where output must be read, it needs a fourth layout, a display that ends with a pause. origin/main's read-only views, System Details and Logs, wait on the same choice. origin/main's other screens with no frame ([interaction.md](../refactor/phase0/interaction.md) §9.4) fall in the two menu and action layouts: a screen of choices is a sub-menu, and an action that changes the system is a workflow step (brief 1C). Of the two legacy screenshots in the export, origin/main's DNS screen (images/03-dns-legacy-tui) fits the sub-menu layout and is mocked, inferred (DEV-058); its Firewall screen (04-firewall-legacy-tui) does not as built: the warning line it shows over its menu, "No firewall rules configured", has no block in the layout, nor has the DNS screen's empty state, so no Firewall mock is built and P-30 asks how to show such a line.

**The main menu's compact form** (DEV-052; refactor plan §3.15, pending). On a terminal (`Env.TTY`) shorter than `CompactBelow`, 40 rows, the main menu fits `CompactBudget`, 24 lines, with Figma's content from 64 columns up: the `Header` and the `Divider` are left out; the context bar and host block are one group and the risk line and status rows another, with no blank line inside a group; and the menu takes two columns, keys and labels alone (`Menu.Compact`, drawn by `nav.Run` through `Screen.Compact`). The host, the risk, every status row, the mode and every item stay on screen. From 40 rows, and on output that is not a terminal, the full form: Figma's 35 lines. Here plain, at 80x24:

```text
0 2   6                    27  31
# Debian 12 (Bookworm) #########################################
  [BOS-SAN-L01]            192.0.2.10
                           182 days, 2 hours, 26 minutes

  [*] Risk Level           Moderate ~ medium security
  [+] User                 Configured non-root, sudo
  [...]
  [ ] Auto Updates         Not Configured

  Select an option         Dry-run Mode Disabled

   1) User Management       6) Dry-Run
   2) SSH Login             7) Run All
   3) DNS                   8) Environment
   4) Firewall              9) System Details
   5) Backup               10) Logs

   0) Exit                 q to quit

  > Enter selection [0-10, b or q]:
```

Known limits: below 64 columns the rows stack (DEV-021) and Figma's content takes 34 lines compact (57 in full), the two columns and the exit row beside its label staying as they are; a pending-reboot line, each risk factor and each status row past Figma's seven add a line; a menu with a marked item keeps one column. Past 24 lines the top scrolls, as the full form's does on a short terminal.

**Running.** A menu layout's `Render` is what `nav.Run` draws for it: `MainMenu.Screen(c, m)` and `SubMenu.Screen(c, m)` return the `nav.Screen` to draw, with the caller's `nav.Menu` in place of the view-model's menu, `Above` holding every block down to the menu heading and `Compact` set for the compact form; call it in each round of `Run`'s draw so that the screen shows the state the last action left. `TestScreen` holds the drawing to `Render` in every golden cell. `WorkflowStep.Run(ctx, p, f)` checks the flow first (`flow.Flow.Check`), so that a flow the flow refuses before writing (no `Engine` or `Confirm`, a context already done) returns `NotRun` and its error with nothing written; otherwise it writes the screen in one write and then runs the flow. The flow and the form it opens with write their gap before their first block or field only once a prompt has been asked (§9.9, §9.10), so the screen takes the gap on the side they leave bare: after itself before any prompt, before itself once one was asked, as when a menu's action opens the workflow. Every block then stands one blank line from the next.

**Fixtures** (exported, for the tests and the gallery; nothing else uses them): `MainMenuFixture` (00-TUI-MAIN), `UserFixture` (01-USER) and `ManageSudoFixture` (USER-MANAGE_SUDO-CONFIRMATION: the step and its flow on a `flow.Fixture` engine). Figma's mock host BOS-SAN-L01 and user george, alice for a frame's other users; the addresses from 192.0.2.0/24 and the port 2230.

**Mocks** (`next/p1d-gallery-screens`). `screen.Mocks()` is the gallery's catalogue, which `TestGolden` pins: every Figma frame that fits a layout and origin/main's DNS screen, by layout, then by name, each drawn by the real layout from fixture data. A `Mock` is named `<layout>/<screen>` after its PNG (lower case, `_` as `-`) and carries its `Source`: the label, "Figma <file>", or for a screen with no frame "inferred from origin/main", "inferred from v0.3.2", or "inferred from origin/main (unreleased)" for a screen v0.3.2 never had (refactor plan §3.25, pending; no mock needs it yet, every unreleased screen mocked having a frame); the origin/main-vs-v0.3.2 delta, with the v0.3.2 code it cites; and what waits on a proposal. A menu mock's `Screen` is a `MenuScreen`, which a `MainMenu` and a `SubMenu` are: its `Render`, and the `Screen` that `nav.Run` draws.

| Mock | Source | Sequences (steps) |
|---|---|---|
| `main-menu/00-tui-main` | Figma 00-TUI-MAIN.png | – |
| `sub-menu/01-user`, `02-ssh-login`, `05-backup`, `user-manage-user-management`, `user-manage-user-select-user`, `user-ssh-keys` | Figma, each its PNG; the frames' drift drawn as they have it (DEV-055) | – |
| `sub-menu/03-dns` | inferred from origin/main; the export's legacy screenshot for comparison (DEV-058) | – |
| `workflow-step/ssh-login-toggle-confirmation` | Figma; typed confirmation pending P-10 (DEV-057) | `normal` (`1-confirm`, `2-applied`), `dry-run` (`1-planned`), `failure` (`1-confirm`, `2-failed`) |
| `workflow-step/user-create-user` | Figma; the frame's path is `cancelled` | each `1-username` to `4-ssh-key`, then: `normal` (`5-confirm`, `6-applied`), `dry-run` (`5-planned`), `failure` (`5-confirm`, `6-failed`), `cancelled` (`5-confirm`, `6-cancelled`) |
| `workflow-step/user-manage-sudo-confirmation` | Figma; `typed` pending P-10 (DEV-056) | `normal` (`1-confirm`, `2-applied`), `dry-run` (`1-planned`), `failure` (`1-confirm`, `2-failed`), `typed` (`1-confirm`, `2-applied`) |

**Sequences** (brief 1D: a workflow is shown as sequences, and a frame is one step in its sequence). A `Sequence` is a path through a workflow: its screen and flow, on a `flow.Fixture` plan, and the lines the user types. `Sequence.Play` runs it on scripted input that echoes each answer after its prompt, as a terminal shows it, and cuts the transcript at the prompts: a step for each answer, from where the last step ended to the prompt and the answer's line, then a last step, what the flow writes after the last answer, named after how the flow ends (DEV-056). A line-based screen changes only when hardn writes and waits only at a prompt, so a step is what the user sees when asked, or at the end; played in order, the steps are the whole transcript (`TestMocks`). The sudo change's plan has two steps, the drop-in's rule and a check of the whole sudo configuration, and the root-login toggle's two, the drop-in's change and the SSH service's reload, so that each failure path has one to roll back; the create-user plan is made from the form's answers (`TestCreateUserPlan`). The sudo change's `typed` sequence marks its rule a lockout risk, so the flow asks the hostname, as P-10 proposes and the refactor plan's Phase 1 acceptance asks of the user-management workflow; its other sequences keep Figma's y/N (DEV-056). A sequence that ends otherwise at the width than it says, as one whose typed confirmation the terminal is too narrow to ask (below 30 columns in Unicode glyphs, 32 in ASCII), which the flow cancels (P-27 (e)), fails with a `*Diverged` holding the whole transcript (`TestDiverged`); the gallery shows that run whole (§9.14).

**Decisions:**
- **Header and Breadcrumb on sub-screens,** as brief 1B's layouts have them, where Figma draws a `TitleBar` (P-08): two lines more, 23 for 01-USER against Figma's 21, still within 24 rows. The breadcrumb's items are "hardn" and Figma's title bars: "hardn › User Management", "hardn › User Management › Manage User" (origin/main titles the sudo screen "Manage Sudo") (DEV-053, DEV-054).
- **Words.** The main menu's items are origin/main's, the baseline, so item 2 reads "Toggle SSH root access" where Figma has "Toggle root SSH access" (P-07 (a), P-12); its Exit row says "q to quit" (DEV-030). Its prompt is origin/main's "Enter selection" too, where Figma's frames and the system's other menus say "Enter your choice" (P-29 (a)). The User Management sub-menu's fixture keeps 01-USER's words, the frame as Figma fills it, where origin/main's differ: "Manage a user" and "Create a user", "Configure sudo, SSH Keys", "Configuration", "Enabled no password" and "Enter selection" ([interaction.md](../refactor/phase0/interaction.md) §9.1, P-29 (b)). The status rows are fixture data, Figma's seven; origin/main shows "Users" and adds "Sudo Method" (P-13). The layouts add no words (DEV-051).
- **The subject line is the screen's,** the warning the flow's notice (DEV-048): the subject names what the screen acts on before anything is asked, the warning is about the change the answers make.
- **No compact form for sub-screens.** `SubMenu` lays its menu out in one column even when `Menu.Compact` is set, as `nav.Run` draws it from `SubMenu.Screen`. 01-USER fits 24 rows; a workflow's transcript scrolls as any flow's output does, the prompt always last.
- **Lip Gloss helpers:** none. Blocks are joined with line breaks: `JoinVertical` pads every line to the widest block's width with spaces (lipgloss `join.go:140-145`), trailing spaces the output rules forbid, and has no gap.

**Tested.** Goldens above, checked by eye against the PNGs (DEV-051 to DEV-058 list the cells). `TestScreen` (nav.Run draws what `Render` renders, both menu layouts, every golden cell); `TestCompact` (Figma's main menu within 24 lines at every width from 64 to 130 and 0, 1, 24 and 39 rows; in full, under 40 lines, from 40 rows and with no terminal); `TestCompactForm` (what the compact form keeps, plain); `TestEmptyBlocks` (an empty status list, an empty subject, a section with nothing under its header, an empty sub-menu); `TestSubMenuFrames` (USER-MANAGE_USER-SELECT_USER and USER-MANAGE_USER-MANAGEMENT drawn by the sub-menu layout); `TestMocks` (names unique and under their layout's, each with a label and a delta; each sequence's steps make the transcript of one run and end as it says; a golden file for every drawing and none besides); `TestCreateUserPlan` (the create-user plan for other answers, and bob's run end to end); `TestDiverged` (the toggle at 29 columns ends cancelled, with its transcript); `TestRunGap` (the workflow screen's gap before any prompt, with the review first and with a form first, and after a prompt; a failed write; no screen for a flow with no engine or no confirm, or a context done before the run); `TestOutputRules` (each mock's screen and each workflow's, at every width from 1 to 130, 24 and 40 rows, three profiles: no line wider than the content (`ContentWidth` under `CapContent`), no tab, no trailing space or newline). The race detector ran on the package. No real terminal was used.

**Open for later units.** `next/p1d-gallery-playable` runs the screens on `nav.Run` with fixture plans (a menu mock's `MenuScreen.Screen`, and the workflows' `Sequence.Build`, whose create-user plan follows the answers; built: §9.14); Phase 4 fills the main menu from the status model in place of the fixture; Phase 5 moves each legacy screen onto a layout. P-08 (title bar or breadcrumb), P-09 (pauses), P-13 (status rows) and P-22 (marked items, which keep the compact menu in one column) change what fills the layouts, not the layouts; P-28 may add a fourth, a display that ends with a pause, and P-30 a notice block to the sub-menu; P-29 and P-31 decide whose words the screens keep where Figma's and origin/main's differ.

### 9.13 Screenshot elements and export tokens → Go components

Each element the PNGs draw, the export's tokens and component candidates behind it ([phase0-findings.md](../refactor/phase0-findings.md#81-figma-export) §8.1; candidate ids from the export's `tokens.md`), and the Go component and slots that draw it. The token mapping itself is §4.1.

| Screenshot element | Frames | Export tokens, candidates | Go component | Slots |
|---|---|---|---|---|
| "hardn", the version box, the URL | 00 | `type.header.strong`, `type.header`, `color.surface.raised` (box), `color.text.dim` (URL) | `Header` | `Header.Wordmark`, `Header.Version`, `Header.URL` |
| Distro bar "Debian 12 (Bookworm)" | 00 | `color.surface.raised`, `color.text.muted`, `color.text.dim`, `type.body.strong` | `ContextBar` | `ContextBar.Fill`, `.Name`, `.Version`, `.Codename` |
| Title bar "User Management", "Manage User" | 01, 02, 05, USER-*, SSH_LOGIN-* | `b8cbe5da`, `color.surface.raised`, `type.body.strong` | `TitleBar` (built); the layouts draw `Header` and `Breadcrumb` (P-08) | `TitleBar.*`, `Breadcrumb.*` |
| Host badge, address, uptime | 00 | `color.badge.host`, `color.text.primary` on the badge (→ `badge.fg`, D3), `type.header.strong`, `color.text.muted`, `color.text.dim` | `HostBlock` | `Badge`, `Host.Address`, `Host.Uptime` |
| "⛬ Risk Level Moderate ≈ medium security" | 00 | `color.status.warn`, `type.body.strong`, `color.text.dim` | `RiskSummary` | `Risk.*` |
| Status rows "⛛ User Configured non-root, sudo" | 00, 01, 02, 05 | `d741958a`, `c1390b17`, `e8e27283`; `color.status.ok`, `color.status.err`, `color.text.primary`, `color.text.dim` | `StatusRow`, `StatusList` | `Status.*` |
| Full-width rule | 00 | `color.border.divider`, `border.width` | `Divider` | `Divider` |
| Panel outline | 00 | #17181C, no token | not drawn (DEV-023) | – |
| "Select an option", and on 00 "Dry-run Mode Disabled" | 00, 01, 02, 05, USER-MANAGE_USER-MANAGEMENT, USER-SSH_KEYS | `color.text.muted` (underline in the PNGs only), `color.status.warn` | `MenuHeading`, `ModeIndicator` | `Menu.Heading`, `Mode.*` |
| Numbered items and descriptions | every menu | `6f82f470`, `1726e2d0`, `411eadb8`; `type.body.strong` (keys), `color.text.primary`, `color.text.muted`, `color.text.dim` (aside) | `Menu`, `MenuItem` | `Menu.Key`, `.Label`, `.Description`, `.Aside` |
| "0) Exit", "0) Return" | every menu | `ad108bad`, `color.text.muted` | `Menu.Exit` | `Menu.ExitKey`, `Menu.Exit` |
| "❯ Enter your choice [0-10 or q]:" and the cursor | every menu | `213f2a96`; `type.glyph`, `type.body.strong`, `color.text.muted`, `size.prompt`; `color.cursor` (dropped, C8) | `PromptLine` | `Prompt.Glyph`, `.Label`, `.Hint` |
| Section header "User Configuration" | 01, 02, 05, SSH_LOGIN-*, USER-CREATE_USER, USER-MANAGE_SUDO-CONFIRMATION | `a4701d0e`; `color.surface.raised` (stub), `color.border.divider` | `SectionHeader` | `Section.*` |
| User badge "george" | 01 | `color.badge.host`, `color.text.input` | `Badge` | `Badge` |
| "Managing: george" and its kin | USER-MANAGE_USER-MANAGEMENT, USER-SSH_KEYS, USER-MANAGE_SUDO-CONFIRMATION | `ad50041d` | `SubjectLine` | `Text.Muted`, `Badge` |
| Dash rows "- Sudo Enabled, No password" | 01, 02, 05, SSH_LOGIN-*, USER-CREATE_USER, USER-MANAGE_SUDO-CONFIRMATION | `4f55bc9f`, `72d8f8a4`, `0b24ef06`, `abd1f7cf`; `color.status.*` for a value's state | `KeyValue`, `BulletList` | `KeyValue.*` |
| Warning, success and cancelled callouts | SSH_LOGIN-TOGGLE_CONFIRMATION, USER-MANAGE_SUDO-CONFIRMATION, USER-CREATE_USER | `bf9038e5`, `6abe89f5`; `color.bar.warn`, `color.bar.ok`, `color.status.warn`, `color.status.ok` | `Callout` (the checklist under the warning: `BulletList` items) | `Callout.*` |
| Answered fields "1. Username: george", "(y/n): y" | USER-CREATE_USER, USER-MANAGE_SUDO-CONFIRMATION, SSH_LOGIN-TOGGLE_CONFIRMATION | `85c87931`, `824bb873`; the answer in `color.text.muted` (D4) | `AnsweredField`; asked under a head, `FieldHead` | `Field.*` |
| "❯ Press any key to continue..." | SSH_LOGIN-SSH_SETTINGS_DISPLAY, SSH_LOGIN-TOGGLE_CONFIRMATION, USER-MANAGE_SUDO-CONFIRMATION, USER-CREATE_USER | `5e67fe79` | none: P-09; the legacy screens say "Press Enter ..." (DEV-050) | – |
| Plan, progress, failure, countdown, one-line messages | no frame | – | `PlanView`, `StepProgress`, `ErrorReport`, `ConfirmCountdown`, `Message` | `Plan.*`, `Step.*`, `Output`, `Confirm.*`, `Message.*` |

### 9.14 Gallery (`next/p1d-gallery-components`, `next/p1d-gallery-screens`, `next/p1d-gallery-playable`)

`internal/tools/uigallery` is brief 1D's dev-only gallery; its package doc is the full statement. It shows the components and, with `-screens`, the screen mocks, and `play` runs the screens on a simulated system (**Playable mode**, below).

**What it shows.** Every state of `fixture.States()` (`pkg/ui/component/fixture`): the 135 states of the 26 components that the component goldens pin (§9.11), in that order, each under a `SectionHeader` with its name. The goldens and the gallery take the states from that one place, so the gallery cannot show anything the goldens do not hold. `TestCatalogueMatchesGoldens` fails for a golden file with no gallery entry or an entry with no golden, and the fixture package's `TestEveryComponent` fails for a type of `pkg/ui/component` with a `Render(Ctx) string` method and no state. Moving the states there left every golden byte-identical.

**Screens** (`next/p1d-gallery-screens`, brief 1D). With `-screens` the gallery shows `screen.Mocks()`, the catalogue the screen goldens pin (§9.12), by layout, then by screen. Each mock comes under a `SectionHeader` with its name, then an info `Callout` holding its source label, the origin/main-vs-v0.3.2 delta and what waits on a proposal, then its drawing. A workflow's mock is its sequences: each step under a header naming its sequence and step, such as `failure/2-failed`, printed in order, so that a sequence reads as the transcript of one run, non-interactively; a step's own blank lines, which set it apart from the step before, give way to the header's. A sequence that ends otherwise at the width than it says (`screen.Diverged`), as one whose typed confirmation the terminal is too narrow to ask, is printed whole under a header with its name and a note naming the cell and how it ended, and the other mocks still show: below 30 columns (32 in ASCII) the root-login toggle's and the sudo change's `typed` sequences show the flow's "too narrow to confirm" cancellation. A step is drawn by playing its sequence (`Sequence.Play`): scripted answers on a `flow.Fixture` plan, which read no terminal and change nothing. `TestScreensRenderAsGoldens` holds every drawing to its golden file in each golden cell the flags can name (the core set, and the main menu at 80x40 on both themes); `screen.TestMocks` holds the catalogue to the golden files.

**Running**, from the repository root (set `GOTOOLCHAIN=local` and `GOWORK=off` as the make targets do):

```text
go run ./internal/tools/uigallery [flags] [component | component/state | all]...
go run ./internal/tools/uigallery -profile plain -width 60 menu hostblock/normal
go run ./internal/tools/uigallery -matrix stepprogress/animated
go run ./internal/tools/uigallery -screens -list
go run ./internal/tools/uigallery -screens -profile plain sub-menu/01-user workflow-step/user-manage-sudo-confirmation/failure
go run ./internal/tools/uigallery -screens -height 40 main-menu
go run ./internal/tools/uigallery play
go run ./internal/tools/uigallery play -sequence typed -profile plain main-menu
go run ./internal/tools/uigallery play -fail 2 workflow-step/user-manage-sudo-confirmation
go run ./internal/tools/uigallery play workflow-step/ssh-login-toggle-confirmation/dry-run
```

| Flag | Meaning |
|---|---|
| none, or `all` | every state; `menu` is every state of `Menu`, `menu/compact` one. States come out in the catalogue's order |
| `-list` | print the names of the states, or with `-screens` the screen drawings, it would render, and exit |
| `-theme dark\|light` | the palette; default dark |
| `-profile` | `truecolor`, `256`, `16`, `none` (ASCII profile: no colour, bold and underline kept) or `plain` (NoTTY: no escape sequences, ASCII glyphs); default truecolor |
| `-ascii` | the ASCII glyph set |
| `-width N` | the terminal width; default 80 |
| `-height N` | the terminal height; default 24. It decides the main menu's form: compact on a terminal under 40 rows (§9.12) |
| `-screens` | the screen mocks (`screen.Mocks()`, §9.12) in place of the component states: no argument or `all` is every mock, `sub-menu` every sub-menu, a workflow's name all its sequences, `<workflow>/<sequence>` one sequence's steps and `<workflow>/<sequence>/<step>` one step |
| `-matrix` | each state in every cell of `uitest.Full()`, then plain mode at 60, 80 and 120: 51 environments, each named as golden files name their cells. It replaces the five flags above, and refuses them; not with `-screens`, whose goldens are on the core set |
| `-override FILE` | a colour override file, below |

**Environment.** Each one is built with `term.Fixed` from the flags alone (height 24 unless `-height` says), as the goldens build theirs; `TestMatrix` holds `-matrix`'s cells to `uitest.Full()` and `uitest.Core()`'s plain cell. Each state is written through that environment's `Env.Writer` to stdout in one write: byte for byte what its golden file holds for the same cell, under the gallery's header (`TestRendersAsGoldens`, every state in every cell of `Full()`). Usage errors exit 2, a failed write or a bad override file 1, and so does a true `RUNEWIDTH_EASTASIAN` (below).

**Colour iteration** (brief 1D). `-override FILE` reads a JSON file of new token values for one run, per palette: a token name as §4.1 names it, and any of a truecolor hex, an xterm-256 index and a 16-colour index; what is left out keeps the palette's value. The file is decoded strictly (no unknown field, one JSON value) and both palettes are checked before anything renders. A `comment` field holds free text.

```json
{
  "comment": "why these values",
  "dark":  {"text.faint": {"hex": "#565B6B", "256": 59, "16": 8}},
  "light": {"text.faint": {"hex": "#81869C"}}
}
```

- **The hook** is `theme.NewWithOverrides(env, overrides)`: the theme `New` builds, from a copy of the palette with the overrides in place. It errors on an unknown token or a value out of range, and never changes the palettes.
- **Production code cannot reach it.** `TestOverrideReach` (in `pkg/ui/theme`) parses every Go file of the module, test files included, and fails if one names `NewWithOverrides`, `Overrides` or `TokenOverride`, under any import name, unless it is in package `theme` in `pkg/ui/theme` or in package `main` in `internal/tools/uigallery` (or their external test packages). A subdirectory of either does not count: a package there could pass the hook on, as module-root `internal/` is open to the whole module (`TestStrayUses` has such a package in each). With nothing in `cmd/hardn` referring to it, the linker leaves the hook out of the release binary, and the file-reading code lives only in the gallery.
- **The file stays local.** The repository commits only the template, `internal/tools/uigallery/uigallery.local.json.example`; a copy named `uigallery.local.json` is ignored by the allow-list `.gitignore` (`git check-ignore -v` names rule `*`; the template matches `!*.example`).

**Never released.** Every build target builds `./cmd/hardn` alone: the makefile's `build`, `linux`, `darwin`, `arm`, `dev`, `dev-build` and `check-cross`, and `deb` and `rpm` through `linux`; `main: ./cmd/hardn` in `.slsa-goreleaser/*.yml`, the `config-file` of `release.yml`'s SLSA build, and in `.slsa-goreleaser.yml`, the SLSA builder's default config, which `slsa-go-releaser.yml` uses; and `go build -o build/hardn ./cmd/hardn` in `ci.yml`. `go list -deps ./cmd/hardn` holds neither the gallery nor `pkg/ui/component/fixture`, and a main package cannot be imported. `make check` compiles, vets, tests and lints the gallery (`go build ./...` discards what it builds, and `check-cross`'s `go vet` for Linux writes no binary), as CI's `go test ./...` runs its tests; depguard's `dev-tools` rule keeps any package outside `internal/tools` from importing one inside it (§9.15).

**What it touches.** Its own code reads only its flags, the override file and, in play, stdin: no environment variable, no terminal query for its output, no root, nothing under `/etc`. Play reads stdin through `prompt.Stdin`, the line reader every hardn prompt uses (§9.7), which asks whether stdin is a terminal by reading its attributes (`TCGETS` on Linux, `TIOCGETA` on macOS), on a terminal discards typeahead before each prompt (`TCFLSH` or `TIOCFLUSH`), and while it waits for a line relays Ctrl-C (`signal.Notify`, waking its `select(2)` through a pipe of its own), giving `SIGINT` back its own handling before it returns; it never changes the terminal's mode. The package initialisation of Lip Gloss and x/ansi still runs in it, as in every binary that links Lip Gloss (§2.3: today the `pkg/ui` test binaries and this gallery; `cmd/hardn` links none): an `ioctl` on stdout and, on a terminal, a terminfo read (and `/etc/passwd` without cgo) and `tmux info` under tmux, all read-only, and x/ansi's read of `RUNEWIDTH_EASTASIAN` ([side-effects.md](../refactor/phase0/side-effects.md) §13.1). The gallery ignores the profile Lip Gloss detects. The variable is the one init effect that changes output: when it is true, x/ansi measures ambiguous-width glyphs such as `─` and `…` as two cells, and every layout moves off its golden. So the gallery, like `uitest.Golden`, checks that `─` measures one cell and otherwise refuses to render (exit 1, a message naming the variable, no output); `-list` still works (`TestEastAsian`, which runs its test binary again with the variable set).

**Playable mode** (`next/p1d-gallery-playable`, brief 1D: "the maintainer types answers and sees the actual behavior, with no root access and nothing changed on the machine"). `uigallery play [flags] [screen | workflow/sequence]` runs the real screens, `nav`'s grammar and `flow`'s canonical flow, reading the answers through `prompt.Stdin`, the line reader every hardn prompt uses, and writing through the environment's `Env.Writer`. It starts at the mock it names, or at a layout that has no other mock, taken whole as `-screens` takes it (only `main-menu` is one, and no name is the main menu too), or at a workflow's sequence, `<workflow>/<sequence>`; it opens with the gallery's header and a note, and ends with what the simulated system recorded.

| Flag | Meaning |
|---|---|
| `-sequence NAME` | the sequence each workflow plays where it has one (`normal`, `dry-run`, `failure`, `typed`), else its first; default `normal`. A start that names a sequence sets it. A sequence plays for its screen and fixture, `Sequence.Build`, and the user types the answers, so one that ends cancelled (create-user's `cancelled`, which builds as its `normal` does) is refused, as `-sequence` and as a start: declining the confirmation plays it |
| `-fail N` | step N of each plan applied fails, and the steps before it roll back; a plan with fewer steps applies as its sequence does; default 0, none |
| `-theme`, `-profile`, `-ascii`, `-width`, `-height`, `-override` | as above; `-height` decides the main menu's form |

- **Where the menus lead** (`links` in `play.go`, after origin/main's menus, [interaction.md](../refactor/phase0/interaction.md) §9): the main menu's 1, 2, 3 and 5 to `sub-menu/01-user`, `02-ssh-login`, `03-dns` and `05-backup`; 01-USER's 1 to the user list and its 2 to the create-user form; the list's george to his menu, whose 1 is the sudo change and 2 the SSH keys screen; SSH Login's 1 to the root-login toggle, confirmed by typing the hostname. Every mock is reached from the main menu (`TestPlayReaches`). An item with no mock (Firewall, Dry-Run, Run All, Environment, System Details, Logs, alice, the DNS, Backup and SSH-key actions, View SSH Settings) answers "The gallery has no screen for <item> yet.", an info `Message`, and the menu is drawn again. 0 or b goes back and q quits at every prompt, as §9.8 has it.
- **The simulated system** (`simulator`) is the apply-and-record half of the seam `next/p3-gallery-engine` fills with the real engine against the recording fake `System` (refactor plan, Phase 3). Until then a workflow plans on its sequence's fixture, `Sequence.Build`'s `flow.Fixture`, and the simulator wraps the flow's `Engine`: applying reports each step as the fixture does, failing where the sequence fails or at `-fail`, and records each step done (its file and commands), failed (why) and rolled back (its file), shown at the end as a `PlanView` under each run's `Callout`. Nothing is run, written or read: no `os/exec`, no file written, no root (`TestPlayTouchesNothing` parses the gallery and every package of the module it links, following their imports, and allows of `syscall` and `x/sys/unix` only the line reader's terminal query, flush and wait).
- **What the seam leaves to Phase 3.** The simulator gets only the `Engine` that `Sequence.Build` has bound into the flow. The real engine needs the action's answers (`flow.Engine`'s doc: "an adapter holding the action's answers"), and for the create-user form those are variables that `pkg/ui/screen` binds to the form's fields inside `Build` (`createUser`). So `next/p3-gallery-engine` must also change how a sequence is built, an exported API of `pkg/ui/screen`, such as `Build` taking a constructor of the engine over the action's answers in place of the fixture it closes over; replacing the simulator alone cannot reach the answers.
- **Exit status:** 0 when the user goes back from the first screen or quits; 130 (`prompt.ExitCode`) when the input ends or Ctrl-C interrupts a prompt, with nothing more applied; 2 for a wrong flag or name, found before stdin is read; 1 for any other error, such as a bad override file.
- **Known limits.** The screens show the fixture's state, so a menu drawn again after a change still shows it unchanged (02-SSH_LOGIN still says root login is enabled): the status model is Phase 4's, and the brief's "refreshed status" waits on it. The main menu's mode indicator is the fixture's, even with `-sequence dry-run`. No animation: `term.Fixed` sets `Animate` off, so each step's line is written once it settles (§9.10). On a terminal the terminal echoes the answers; with stdin piped nothing does, so the transcript shows the prompts without them.
- **Tested.** `TestPlayGolden` on `Core()`: `play/user-management` (an item with no screen, then main menu → User Management → Manage User → george → Sudo Method, y, and back with b, 0 and b to the main menu, left with 0), `play/user-management-typed` (`-sequence typed`: the same path confirmed by typing the hostname, then q from four levels down), `play/ssh-toggle` (the typed confirmation refusing BOS-SAN-L01, then taking bos-san-l01, and q from the sub-menu), `play/fail` (`-fail 2` on the sudo change: its first step rolled back) and `play/abort` (the end of the input at a create-user question: exit 130, "Nothing was applied."). `TestPlayEnds` (the end of the input at a menu, a sub-menu, a form's field, a y/N and a typed confirmation, and after an apply; q and Q, b and 0 at each kind of first screen; a dry-run sequence); `TestPlayFail` (`-fail` 1, 2 and past the plan, a failure sequence, and `-fail` over it); `TestPlayReaches`; `TestPlayWriteFails` (a failed write of the record exits 1 after q and after 0, and an abort keeps 130); `TestPlaySmoke` (every profile, both themes and glyph sets, at 20, 60 and 80 columns; below 30 the toggle's typed confirmation is cancelled unasked, P-27 (e)); `TestPlayUsage` (a sequence that ends cancelled refused too); `TestPlayTouchesNothing`. The typist is `uitest.Typist` (§8). No real terminal was used, nor a pipe from a shell: the line reader's own tests cover the terminal (§9.7).

**Decisions:**
- **One catalogue, in its own package.** `pkg/ui/component/fixture` holds `States()` and the named fixtures the component tests share, each a function that returns a fresh value. Because it imports `pkg/ui/component`, the component tests that use it moved to the external test package `component_test`, with the package dot-imported so that their bodies are unchanged; `export_test.go` names the unexported words and helpers they check.
- **The gallery's chrome is a component.** A state's title is a `SectionHeader`, so it degrades with the environment like everything under it; there is no hand-built layout. On a terminal too narrow for the title (below about 35 columns, and in `-matrix`'s 60-column cells for the longest names) the header is rendered as if the terminal were just wide enough, so the name is never cut and no two states look alike; that one line, chrome and no component's output, runs past the terminal's width. The content width still caps it, and every title fits (`TestNarrow`, `TestSelect`).
- **JSON for the override file** (`encoding/json`, no new module): strict decoding, and none of YAML's traps for a hex value, where an unquoted `#` starts a comment.
- **colorprofile is imported** for `term.Fixed`'s profiles, and x/ansi to measure the headers: depguard's `terminal` rule excepts `internal/tools/uigallery` for both, and only for them; Lip Gloss stays denied there, so the gallery has no layouts of its own (§9.15).

**Tested.** `TestCatalogueMatchesGoldens`; `TestRendersAsGoldens`; `TestScreensRenderAsGoldens`; `TestScreenSelect` (`-screens -list` by layout, mock, sequence and step; one step under its mock's header and note; every screen header shows its whole name within the content width); `TestMatrix`; `TestSmoke` (in process, every profile, both themes, both glyph sets, 60 and 80 columns: every state under its name, colour only where the profile keeps it, only ASCII where asked, no line wider than the terminal); `TestScreenSmoke` (the same for `-screens`, at 1, 20, 29 to 32, 60 and 80 columns: exit 0, every mock's header, every sequence step by step or, where it ends otherwise at the width, whole under its name, and no line wider than the terminal but a header and a prompt's echoed answer); `TestSelect` (`-list`, the arguments, one state's exact output, `-matrix` with each cell's whole name, for the longest state name too); `TestNarrow` (at 1, 20 and 24 columns every header holds its whole name, and is no wider than the name needs); `TestUsage`; `TestEastAsian` (a true `RUNEWIDTH_EASTASIAN`: exit 1 and no output; `-list` still lists); `TestOverrideExample` (the template changes no state in either palette at truecolor, 256 or 16 colours, and each value it holds, changed, changes some state, so a template that drifts from `palette.go` fails); `TestOverride` (a value shows at its own profile and palette only; bad files exit 1 before any output). In `pkg/ui/theme`: `TestNoOverrides`, `TestOverrides`, `TestOverrideErrors`, `TestOverrideUses`, `TestStrayUses`, `TestOverrideReach`. In `pkg/ui/component/fixture`: `TestStates`, `TestEveryComponent`, `TestFresh`.

### 9.15 Usage rules (`next/p1d-lint`)

The rules for code that shows anything to the user or reads from them. The import rules below enforce what an import can show; the tests named, and review, hold the rest.

- **Screens are built from the layouts and the components.** A screen is one of the three layouts of §9.12 filled with a view-model of plain data; a screen that fits none is a proposal for a new layout in [proposals.md](../refactor/proposals.md), never a one-off. Feature code fills view-models: it picks no colour, glyph or column.
- **No raw ANSI and no Lip Gloss outside `pkg/ui`.** Colours and attributes come from the theme's slots, glyphs from `theme.Glyphs`, columns and gaps from the grid constants (§4 to §7). Inside `pkg/ui` too a component hard-codes no colour, glyph or escape sequence: a missing one is added to `pkg/ui/theme` first (below). Widths are measured with `lipgloss.Width` or x/ansi, never `len()`.
- **Input only through `pkg/ui/prompt`:** the one `Reader` over stdin, `prompt.Stdin()`, which the new screens reach through `nav.Prompter` and `form` (§9.7 to §9.9). Never `os.Stdin`, another `bufio.Reader` on it, `stty`, raw mode or a key read: every input is a line ended by Enter.
- **Output only through `Env.Writer`.** A renderer is a pure function of its view-model and `component.Ctx`: it returns a string and does no I/O, reads no clock and no environment variable. The caller writes the string through `term.Env.Writer(w)`, `w` the `io.Writer` it was handed (stdout in hardn), one whole string per write. Never `os.Stdout` directly, `fmt.Print*`, or Lip Gloss's `Print*`, `Fprint*` and `Sprint*`, which ignore `term.Options` (§2.3).
- **One render environment:** `term.Detect` once at startup, `term.Fixed` in tests and the gallery; only `pkg/ui/term` looks at the terminal, and `prompt.Stdin` asks whether stdin is one (§3, §9.7).
- **States read without colour:** a glyph and a word for every state, muted text from gray tokens and never SGR 2 (faint), and output that degrades without colour, Unicode, width or a terminal, and on a light background (§1).
- **Don't borrow** (brief 1C): the event loop and full-screen redraws, focus/blur state machines, Tab navigation between fields, or arrow-key selection. They conflict with the line-based model; the maintainer may opt into one later, through a proposal. Bubble Tea, Bubbles and huh lend the patterns brief 1C pairs with hardn's adaptations, and are never imported.
- **Test helpers stay in tests:** `pkg/ui/uitest` and `pkg/ui/component/fixture` only in `_test.go` files and under `internal/tools`. So do the screens' and the flow's fixtures (`screen.Mocks`, `MainMenuFixture` and its kin, `flow.Fixture`), and `theme.NewWithOverrides` is the gallery's alone (§9.14).

**Adding a component.** A view-model struct of plain data in `pkg/ui/component` with a pure `Render(c Ctx) string`, by the conventions of §9.1; evaluate Lip Gloss's table, list, tree, join and place helpers first and record the outcome in its group's section. Its states go in `fixture.States()`, which gives each a golden on `Full()` and a gallery entry (`TestEveryComponent` fails for a component with none); then `make golden-update`, each new file checked by eye (at least `truecolor-dark-unicode-80`, `none-dark-ascii-60` and the light cells), a row in §9.11, a DEV row in [deviations.md](deviations.md) for each departure from Figma, and a proposal for any word Figma and origin/main do not have.

**Adding a token.** A `token{hex, ansi256, ansi}` field in the `palette` struct of `pkg/ui/theme/palette.go`, named by its `token` tag, with a value in `darkPalette` and in `lightPalette`: the 256-colour index the nearest by CIE76 (`TestANSI256Nearest`) unless an override is logged, the 16-colour pick by hand. Then its contrast floor in `TestContrast`, the slots that use it in `theme.New` (`TestColourRoles`), the swatch golden regenerated, its rows in §4.1 and §4.3, and a DEV row where it departs from the export. Values can be tried first without editing code, with the gallery's `-override` (§9.14).

**Adding a glyph.** A role in `theme.Glyphs` with a value in both sets: printable ASCII in the ASCII set (`TestGlyphSetsComplete`), an East Asian ambiguous one listed in the `Glyphs` doc comment (`TestAmbiguousGlyphs`), and a state's marker within the three-cell marker column and unlike every other state's (`TestStatusMarkers`, `TestMessageMarkers`, `TestStepMarkers`). Then the swatch golden, a row in §6, and a DEV row for a glyph Figma does not draw.

**Updating goldens deliberately.** A golden diff is a change in what the user sees. Run `make golden-update` only after a change meant to alter output, never to make a failing test pass; read the diff cell by cell, the cells above at least; the commit body says why each file changed, and the review description lists each changed golden with its reason (refactor plan, review template item 4). A golden file is never edited by hand: its header says so.

**The import rules** (`.golangci.yml`, golangci-lint v2.14.0's depguard). depguard is not in v2's standard set, so it is enabled explicitly. Each rule applies to the files its globs match (absolute paths, each rooted at `${base-path}`, the directory of `.golangci.yml`, and written below from the repository root: `$all` any Go file, `$test` a `_test.go` file, `!` excluding) and denies each import path that starts with one of its entries; `check-lint` fails on any report.

| Rule | Files | Denies | Why |
|---|---|---|---|
| `lipgloss` | all but `pkg/ui/**` | `charm.land/lipgloss` | Lip Gloss is `pkg/ui`'s alone |
| `terminal` | all but `pkg/ui/**` and `internal/tools/uigallery/**` | `github.com/charmbracelet/colorprofile`, `github.com/charmbracelet/x/ansi`, `github.com/charmbracelet/x/term` | Colour profiles, raw ANSI and terminal queries are `pkg/ui/term`'s and the components'. The gallery names profiles for `term.Fixed` and measures its headers with x/ansi (§9.14) |
| `legacy-style` | all but the seven exceptions below | `github.com/fatih/color`, `github.com/abbott/hardn/pkg/style`, `github.com/mattn/go-runewidth` | The legacy styling: its colour library, its raw-ANSI helpers and their width measure. `pkg/ui` is not excepted |
| `never` | all | `charm.land/lipgloss/v2/compat`; `github.com/charmbracelet/lipgloss` (v1); `charm.land/` and `github.com/charmbracelet/` `bubbletea`, `bubbles` and `huh` | `compat` queries the terminal and detects a profile at import (§2.3); hardn uses Lip Gloss v2 (§2.1); patterns are borrowed, not imported |
| `test-only` | all but `$test` and `internal/tools/**` | `github.com/abbott/hardn/pkg/ui/uitest`, `github.com/abbott/hardn/pkg/ui/component/fixture` | The golden harness and the component states stay out of production code |
| `dev-tools` | all but `internal/tools/**` | `github.com/abbott/hardn/internal/tools` | Dev-only tools may import `pkg/ui` freely; nothing imports them, so nothing links them into hardn (§9.14) |
| `prompt-stdlib` | `pkg/ui/prompt/**` but `$test`; strict | anything but the standard library (`$gostd`) and `golang.org/x/sys/unix` | The legacy menus read through it without linking Lip Gloss (§9.7) |
| `keys-stdlib` | `pkg/ui/keys/**` but `$test`; strict | anything but the standard library | The same for the legacy menus' key data (§9.8) |

**Exceptions** (`legacy-style` only, the refactor plan's initial list). Each exempts what its package imports today and goes when the package stops importing it, as its slice moves onto `pkg/ui` (refactor plan, Phases 3 to 5: each area's screen, unit (c), on the design system; then the one unit after slice 9 that removes the legacy paths). Nothing else is excepted: a legacy package cannot start on Lip Gloss, colorprofile or the test helpers.

| Exception | Imports today | Goes when |
|---|---|---|
| `pkg/style` | `go-runewidth` (`boxes.go`) | The library is deleted with the last legacy path; its own deny entries can then move to `never` |
| `pkg/menu` | `pkg/style` (20 files, and `main_menu_render_test.go`), `go-runewidth` (that test) | Its last screen moves onto a layout |
| `cmd/hardn` | `pkg/style`, `fatih/color` (`main.go`: `initializeColor` sets `style.UseColors` and `color.NoColor` from `--no-color`) | Its colour set-up gives way to `term.Detect` and the flags of refactor plan §3.14 and §3.11 |
| `pkg/logging` | `fatih/color` (`logging.go`: its console messages) | Its console messages go through `pkg/ui` |
| `pkg/security` | `pkg/style` (`status.go`: the security status display) | That display gives way to `StatusList`, filled from the status model (Phase 4) |
| `pkg/system` | `pkg/style` (`display.go`: System Details' box) | System Details moves onto a layout (slice 1) |
| `pkg/utils` | `pkg/style` (`utils.go`: `PrintPounds`, `PrintTilda` and `PrintLogo`, and `PrintHeader` through `PrintTilda`) | Those helpers have no caller left |

**Limits.**
- golangci-lint lints the files of the host's build only, so on a darwin host a Linux-only file goes unlinted; `pkg/ui/prompt`'s and `pkg/ui/keys`'s `TestImports` read every non-test file whatever its build tags, so they stay. `make check`'s `check-cross` type-checks and vets the whole module, tests included, for linux/amd64 and linux/arm64, so a Linux-only file at least compiles (§9.7).
- depguard matches import paths, not identifiers, so it cannot keep `theme.NewWithOverrides` to the gallery: every `pkg/ui` package imports `pkg/ui/theme`. `TestOverrideReach` holds that (§9.14), and review holds the screen and flow fixtures. Nor does it see string literals: an escape sequence written out, as in `pkg/utils`'s `ClearScreen` (`"\033[H\033[2J"`) and `pkg/style`'s constants, is review's to refuse.
- golangci-lint shows one issue per line, so an import two rules deny (`compat` outside `pkg/ui`) is reported once. An import of a module not in `go.mod`, such as Bubble Tea, fails to type-check before depguard sees it; adding the module is a reviewed `go.mod` change, and the `never` rule then reports the import.
- `${base-path}` is the directory of `.golangci.yml` (golangci-lint's default `run.relative-path-mode`, `cfg`), so the globs hold wherever the tree is checked out and from whichever directory golangci-lint runs; unrooted, as `**/pkg/ui/**` was, a glob matched a directory above the tree too, so a checkout inside a directory named `pkg/ui` took every file out of each rule that excepts `pkg/ui`. A checkout whose path holds `[` or `{` breaks the rooted globs (checked: no exception matched), so `check-lint` reports the excepted imports: it fails rather than exempting anything.

**Checked** (golangci-lint v2.14.0, Go 1.27.2). `golangci-lint config verify` passes, and fails for a misspelt key or list mode; the tree reports 0 issues. Throwaway files, never committed, in `pkg/version`, `pkg/ui/theme`, `pkg/ui/prompt`, `pkg/ui/keys`, `pkg/menu`, `cmd/hardn`, the gallery and a temporary package under `internal/tools`, test files among them, made every rule report each import it denies and pass each import it allows (26 reports with `--uniq-by-line=false`). The `never` entries for modules not in `go.mod` were checked in a temporary copy of the tree whose `go.mod` replaced them with local stubs. With the exceptions removed, the tree reports exactly the imports the table lists.

### 9.16 Still to come (placeholders)

- **freeze PNGs** of the gallery's output for side-by-side review against the screenshots (asked before first use). The screen mocks and the playable mode are built (§9.12, §9.14).

## 10. Token report

The brief's optional dev-only report (export tokens with no mapping, mapped values that differ) is **deferred: the export uses a custom fig-extract format; the mapping above is maintained by hand.**
