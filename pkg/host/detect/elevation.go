package detect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// SudoFlavor is which sudo implementation is installed.
type SudoFlavor uint8

const (
	SudoFlavorUnknown SudoFlavor = iota
	SudoOriginal                 // the sudo project's sudo: "Sudo version 1.9.16p2"
	SudoRS                       // sudo-rs, Ubuntu 26.04's default: "sudo-rs 0.2.13"
)

func (f SudoFlavor) String() string {
	switch f {
	case SudoOriginal:
		return "sudo"
	case SudoRS:
		return "sudo-rs"
	}
	return "unknown"
}

// Tool is a command found the way host.Real finds one: the first regular
// file with an execute bit in host.SearchPath.
type Tool struct {
	Present Tri
	Path    string // where it was found
	Err     error
}

// Elevation is which elevation tools are installed, for p2-sudoers: sudo
// (and whether it is sudo-rs) and doas, Alpine's default.
type Elevation struct {
	Sudo        Tool
	SudoFlavor  SudoFlavor // established only when Sudo is present
	SudoVersion string     // the version sudo --version printed
	Doas        Tool
	Err         error // why SudoFlavor is unknown while Sudo is present
}

// Command is the command that runs hardn as root on the host: doas where
// sudo is not installed and doas is (Alpine's default, sudoers.md), else
// sudo, the brief's words.
func (e Elevation) Command() string {
	if e.Sudo.Present == No && e.Doas.Present == Yes {
		return "doas"
	}
	return "sudo"
}

// FindElevation looks for sudo and doas in host.SearchPath and runs nothing:
// enough for Command, where a message has no facts to hand.
func FindElevation(ctx context.Context, sys host.System) Elevation {
	return Elevation{Sudo: FindTool(ctx, sys, "sudo"), Doas: FindTool(ctx, sys, "doas")}
}

// DetectElevation looks for sudo and doas in host.SearchPath and asks sudo
// for its version.
func DetectElevation(ctx context.Context, sys host.System) Elevation {
	e := FindElevation(ctx, sys)
	if e.Sudo.Present != Yes {
		return e
	}
	res, err := sys.Run(ctx, "sudo", "--version")
	line := firstLine(res.Stdout)
	switch {
	case err != nil:
		e.Err = fmt.Errorf("detect: elevation: %w", err)
	case strings.HasPrefix(line, "sudo-rs "):
		e.SudoFlavor, e.SudoVersion = SudoRS, strings.TrimPrefix(line, "sudo-rs ")
	case strings.HasPrefix(line, "Sudo version "):
		e.SudoFlavor, e.SudoVersion = SudoOriginal, strings.TrimPrefix(line, "Sudo version ")
	default:
		e.Err = fmt.Errorf("detect: elevation: sudo --version printed %q: %w", line, ErrUnrecognised)
	}
	return e
}

// FindTool looks for name in every host.SearchPath directory, in order:
// sudo and doas here, useradd for the User control's Create on Alpine.
func FindTool(ctx context.Context, sys host.System, name string) Tool {
	var errs []error
	for _, dir := range strings.Split(host.SearchPath, ":") {
		p := dir + "/" + name
		fi, t, err := stat(ctx, sys, p)
		switch {
		case t == Yes && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0:
			return Tool{Present: Yes, Path: p}
		case t == Unknown:
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return Tool{Err: fmt.Errorf("detect: %s: %w", name, errors.Join(errs...))}
	}
	return Tool{Present: No}
}
