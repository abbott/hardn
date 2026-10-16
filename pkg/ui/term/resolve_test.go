package term

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// tty and piped build Facts for a stdout that is or is not a terminal; stdin
// matches stdout. Sizes are unknown unless a test sets them.
func tty(environ ...string) Facts {
	return Facts{StdoutTTY: true, StdinTTY: true, Environ: environ}
}

func piped(environ ...string) Facts {
	return Facts{Environ: environ}
}

const term256 = "TERM=xterm-256color"

func TestResolveProfile(t *testing.T) {
	tests := []struct {
		name      string
		opts      Options
		facts     Facts
		want      colorprofile.Profile
		wantPlain bool
	}{
		{"auto 256-colour terminal", Options{}, tty(term256), colorprofile.ANSI256, false},
		{"explicit auto equals unset", Options{Color: ColorAuto}, tty(term256), colorprofile.ANSI256, false},
		{"auto COLORTERM truecolor", Options{}, tty(term256, "COLORTERM=truecolor"), colorprofile.TrueColor, false},
		{"auto 16-colour terminal", Options{}, tty("TERM=xterm"), colorprofile.ANSI, false},
		{"auto TERM unset", Options{}, tty(), colorprofile.NoTTY, false},
		{"auto piped", Options{}, piped(term256), colorprofile.NoTTY, true},
		{"auto piped ignores CLICOLOR_FORCE", Options{}, piped(term256, "CLICOLOR_FORCE=1"), colorprofile.NoTTY, true},
		{"NO_COLOR=1", Options{}, tty(term256, "NO_COLOR=1"), colorprofile.ASCII, false},
		{"NO_COLOR=0 still disables", Options{}, tty(term256, "NO_COLOR=0"), colorprofile.ASCII, false},
		{"NO_COLOR=yes disables", Options{}, tty(term256, "NO_COLOR=yes"), colorprofile.ASCII, false},
		{"NO_COLOR empty is unset", Options{}, tty(term256, "NO_COLOR="), colorprofile.ANSI256, false},
		{"NO_COLOR beats CLICOLOR_FORCE", Options{}, tty(term256, "NO_COLOR=1", "CLICOLOR_FORCE=1"), colorprofile.ASCII, false},
		{"TERM=dumb", Options{}, tty("TERM=dumb"), colorprofile.ASCII, false},
		{"TERM=dumb beats CLICOLOR_FORCE", Options{}, tty("TERM=dumb", "CLICOLOR_FORCE=1"), colorprofile.ASCII, false},
		{"HARDN_PLAIN", Options{}, tty(term256, "HARDN_PLAIN=1"), colorprofile.NoTTY, true},
		{"--plain", Options{Plain: true}, tty(term256), colorprofile.NoTTY, true},
		{"always piped", Options{Color: ColorAlways}, piped(term256), colorprofile.ANSI256, true},
		{"always piped TERM unset", Options{Color: ColorAlways}, piped(), colorprofile.ANSI, true},
		{"always beats NO_COLOR", Options{Color: ColorAlways}, tty(term256, "NO_COLOR=1"), colorprofile.ANSI256, false},
		{"always beats TERM=dumb", Options{Color: ColorAlways}, tty("TERM=dumb"), colorprofile.ANSI, false},
		{"always with --plain", Options{Color: ColorAlways, Plain: true}, tty(term256), colorprofile.ANSI256, true},
		{"never", Options{Color: ColorNever}, tty(term256, "COLORTERM=truecolor"), colorprofile.ASCII, false},
		{"never beats CLICOLOR_FORCE", Options{Color: ColorNever}, tty(term256, "CLICOLOR_FORCE=1"), colorprofile.ASCII, false},
		{"never piped", Options{Color: ColorNever}, piped(term256), colorprofile.NoTTY, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Resolve(tt.opts, tt.facts)
			if e.Profile != tt.want || e.Plain != tt.wantPlain {
				t.Errorf("Profile, Plain = %v, %v; want %v, %v", e.Profile, e.Plain, tt.want, tt.wantPlain)
			}
		})
	}
}

func TestResolveUnicode(t *testing.T) {
	const utf8 = "LANG=en_US.UTF-8"
	tests := []struct {
		name  string
		opts  Options
		facts Facts
		want  bool
	}{
		{"UTF-8 locale", Options{}, tty(term256, utf8), true},
		{"utf8 spelling", Options{}, tty(term256, "LANG=en_US.utf8"), true},
		{"C.UTF-8", Options{}, tty(term256, "LANG=C.UTF-8"), true},
		{"modifier", Options{}, tty(term256, "LANG=ca_ES.UTF-8@valencia"), true},
		{"Latin-1 locale", Options{}, tty(term256, "LANG=en_US.ISO-8859-1"), false},
		{"no codeset", Options{}, tty(term256, "LANG=en_US"), false},
		{"C", Options{}, tty(term256, "LANG=C"), false},
		{"POSIX", Options{}, tty(term256, "LANG=POSIX"), false},
		{"no locale", Options{}, tty(term256), false},
		{"LC_ALL overrides LANG", Options{}, tty(term256, utf8, "LC_ALL=C"), false},
		{"LC_ALL UTF-8 overrides LANG=C", Options{}, tty(term256, "LANG=C", "LC_ALL=en_US.UTF-8"), true},
		{"LC_CTYPE before LANG", Options{}, tty(term256, "LANG=C", "LC_CTYPE=en_US.UTF-8"), true},
		{"empty LC_ALL is skipped", Options{}, tty(term256, utf8, "LC_ALL="), true},
		{"Japanese", Options{}, tty(term256, "LANG=ja_JP.UTF-8"), false},
		{"Chinese", Options{}, tty(term256, "LC_ALL=zh_TW.UTF-8", utf8), false},
		{"Korean", Options{}, tty(term256, "LC_CTYPE=ko_KR.UTF-8"), false},
		{"TERM=linux", Options{}, tty("TERM=linux", utf8), false},
		{"TERM=dumb", Options{}, tty("TERM=dumb", utf8), false},
		{"--ascii", Options{ASCII: true}, tty(term256, utf8), false},
		{"HARDN_ASCII=1", Options{}, tty(term256, utf8, "HARDN_ASCII=1"), false},
		{"HARDN_ASCII=0", Options{}, tty(term256, utf8, "HARDN_ASCII=0"), true},
		{"plain when piped", Options{}, piped(term256, utf8), false},
		{"plain with --color=always", Options{Color: ColorAlways}, piped(term256, utf8), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.opts, tt.facts).Unicode; got != tt.want {
				t.Errorf("Unicode = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveTruthy(t *testing.T) {
	for v, want := range map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "On": true,
		"0": false, "false": false, "no": false, "off": false, "": false, "2": false, "y": false, " 1": false,
	} {
		if got := Resolve(Options{}, tty(term256, "HARDN_PLAIN="+v)).Plain; got != want {
			t.Errorf("HARDN_PLAIN=%q: Plain = %v, want %v", v, got, want)
		}
		if got := !Resolve(Options{}, tty(term256, "LANG=en_US.UTF-8", "HARDN_ASCII="+v)).Unicode; got != want {
			t.Errorf("HARDN_ASCII=%q: ASCII = %v, want %v", v, got, want)
		}
	}
}

func TestResolveTheme(t *testing.T) {
	tests := []struct {
		name      string
		opts      Options
		facts     Facts
		want      ThemeMode
		wantDark  bool
		wantQuery bool
	}{
		{"default dark", Options{}, tty(), ThemeDark, true, false},
		{"HARDN_THEME=light", Options{}, tty("HARDN_THEME=light"), ThemeLight, false, false},
		{"HARDN_THEME any case", Options{}, tty("HARDN_THEME=LIGHT"), ThemeLight, false, false},
		{"HARDN_THEME=auto", Options{}, tty("HARDN_THEME=auto"), ThemeAuto, true, true},
		{"invalid HARDN_THEME ignored", Options{}, tty("HARDN_THEME=solarized"), ThemeDark, true, false},
		{"--theme beats HARDN_THEME", Options{Theme: ThemeDark}, tty("HARDN_THEME=light"), ThemeDark, true, false},
		{"--theme=light", Options{Theme: ThemeLight}, tty("HARDN_THEME=dark"), ThemeLight, false, false},
		{"--theme=auto", Options{Theme: ThemeAuto}, tty(), ThemeAuto, true, true},
		{"auto, stdin piped", Options{Theme: ThemeAuto}, Facts{StdoutTTY: true}, ThemeAuto, true, false},
		{"auto, stdout piped", Options{Theme: ThemeAuto}, Facts{StdinTTY: true}, ThemeAuto, true, false},
		{"auto, plain", Options{Theme: ThemeAuto}, tty("HARDN_PLAIN=1"), ThemeAuto, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Resolve(tt.opts, tt.facts)
			if e.Theme != tt.want || e.Dark != tt.wantDark || e.queriesBackground() != tt.wantQuery {
				t.Errorf("Theme, Dark, query = %v, %v, %v; want %v, %v, %v",
					e.Theme, e.Dark, e.queriesBackground(), tt.want, tt.wantDark, tt.wantQuery)
			}
		})
	}
}

func TestResolveSize(t *testing.T) {
	sized := func(f Facts, w, h int) Facts { f.Width, f.Height = w, h; return f }
	tests := []struct {
		name  string
		facts Facts
		w, h  int
	}{
		{"terminal size", sized(tty("COLUMNS=100", "LINES=30"), 120, 40), 120, 40},
		{"unknown size uses COLUMNS and LINES", tty("COLUMNS=100", "LINES=30"), 100, 30},
		{"each dimension on its own", sized(tty("LINES=50"), 120, 0), 120, 50},
		{"piped ignores the measured size", sized(piped("COLUMNS=132"), 120, 40), 132, DefaultHeight},
		{"invalid COLUMNS and LINES", tty("COLUMNS=wide", "LINES=-5"), DefaultWidth, DefaultHeight},
		{"zero COLUMNS", tty("COLUMNS=0"), DefaultWidth, DefaultHeight},
		{"nothing known", piped(), DefaultWidth, DefaultHeight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Resolve(Options{}, tt.facts)
			if e.Width != tt.w || e.Height != tt.h {
				t.Errorf("size = %dx%d, want %dx%d", e.Width, e.Height, tt.w, tt.h)
			}
		})
	}
}

func TestResolveTerminalState(t *testing.T) {
	tests := []struct {
		name                                string
		opts                                Options
		facts                               Facts
		wantTTY, wantInput, wantPlain, anim bool
	}{
		{"interactive", Options{}, tty(term256), true, true, false, true},
		{"stdout piped", Options{}, Facts{StdinTTY: true}, false, true, true, false},
		{"stdin piped", Options{}, Facts{StdoutTTY: true}, true, false, false, true},
		{"--plain on a terminal", Options{Plain: true}, tty(term256), true, true, true, false},
		{"HARDN_PLAIN=on", Options{}, tty("HARDN_PLAIN=on"), true, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Resolve(tt.opts, tt.facts)
			if e.TTY != tt.wantTTY || e.InputTTY != tt.wantInput || e.Plain != tt.wantPlain || e.Animate != tt.anim {
				t.Errorf("TTY, InputTTY, Plain, Animate = %v, %v, %v, %v; want %v, %v, %v, %v",
					e.TTY, e.InputTTY, e.Plain, e.Animate, tt.wantTTY, tt.wantInput, tt.wantPlain, tt.anim)
			}
		})
	}
}
