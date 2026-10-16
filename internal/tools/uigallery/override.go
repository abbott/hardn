package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// overrideFile is the colour override file: new values for some tokens
// of each palette, by token name ("text.muted", "status.ok", ...; the
// design-system README's §4 lists them). A value left out keeps the
// palette's. Comment is free text, for the file's own notes.
//
//	{
//	  "comment": "why these values",
//	  "dark":  {"text.faint": {"hex": "#6B7080", "256": 242, "16": 8}},
//	  "light": {"status.warn": {"hex": "#7A5600"}}
//	}
type overrideFile struct {
	Comment string                 `json:"comment"`
	Dark    map[string]tokenValues `json:"dark"`
	Light   map[string]tokenValues `json:"light"`
}

// tokenValues are one token's new values: the truecolor hex "#RRGGBB",
// an xterm-256 index (0-255) and a 16-colour index (0-15).
type tokenValues struct {
	Hex     string `json:"hex"`
	ANSI256 *int   `json:"256"`
	ANSI    *int   `json:"16"`
}

// forPalette returns the file's overrides for the dark or the light
// palette.
func (f *overrideFile) forPalette(dark bool) theme.Overrides {
	values := f.Light
	if dark {
		values = f.Dark
	}
	o := theme.Overrides{}
	for name, v := range values {
		o[name] = theme.TokenOverride{Hex: v.Hex, ANSI256: v.ANSI256, ANSI: v.ANSI}
	}
	return o
}

// readOverride reads and checks the override file at path: strict JSON
// (no unknown field, nothing after the object), and every token and value
// valid for its palette, so that a mistake shows before anything renders,
// whichever palette the flags pick.
func readOverride(path string) (*overrideFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("override file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f overrideFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("override file %s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("override file %s: more than one JSON value", path)
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		if _, err := theme.NewWithOverrides(term.Fixed(term.DefaultWidth, colorprofile.TrueColor, true, dark), f.forPalette(dark)); err != nil {
			return nil, fmt.Errorf("override file %s, %s palette: %w", path, name, err)
		}
	}
	return &f, nil
}
