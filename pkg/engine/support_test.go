package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
)

// TestBestEffortFinding (P-41, decided 2026-10-19): a plan made on a
// best-effort release carries one warning finding, CodeBestEffort, naming
// the release (with its matrix note), and is applied; a supported release
// carries none; out-of-scope, unlisted and unestablished releases carry
// none and stay refused for a plan with changes (the support gate is
// unchanged). A plan with nothing to change on a best-effort release
// carries the finding too: every plan made on one.
func TestBestEffortFinding(t *testing.T) {
	ctx := context.Background()
	alpine321 := func() detect.Facts { // as detect classifies it (decision 5, adopted 2026-11-04)
		f := debian13
		f.Platform = detect.Classify(detect.OSRelease{ID: "alpine", VersionID: "3.21.5"}, detect.Proxmox{Installed: detect.No})
		return f
	}
	platform := func(d detect.Distro, v string, s detect.Support, note string) detect.Facts {
		f := debian13
		f.Platform = detect.Platform{Distro: d, Version: v, Support: s, Note: note}
		return f
	}
	for _, tc := range []struct {
		name    string
		facts   detect.Facts
		finding string // the finding's detail must hold it; "": none
		refused bool
	}{
		{"Debian 13, supported", debian13, "", false},
		{"Ubuntu 26.04, supported", platform(detect.DistroUbuntu, "26.04", detect.Supported, ""), "", false},
		{"Ubuntu 22.04, best effort", platform(detect.DistroUbuntu, "22.04", detect.BestEffort, ""),
			"Ubuntu 22.04 is a best-effort release: its vendor still supports it, and hardn changes it but makes no promises for it", false},
		{"Alpine 3.22, best effort", platform(detect.DistroAlpine, "3.22", detect.BestEffort, ""), "Alpine 3.22 is a best-effort release", false},
		{"a best-effort release with a note", platform(detect.DistroAlpine, "3.22", detect.BestEffort, "an example note"),
			"makes no promises for it (an example note)", false},
		{"Alpine 3.21, out of scope, refused with its note", alpine321(),
			"", true},
		{"Ubuntu 25.10, out of scope", platform(detect.DistroUbuntu, "25.10", detect.OutOfScope, ""), "", true},
		{"an unlisted platform", platform(detect.DistroOther, "", detect.Unlisted, ""), "", true},
		{"a platform not established", platform(detect.DistroUnknown, "", detect.SupportUnknown, ""), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			e := New(h, testOptions(h, nil, nil, tc.facts))
			p, err := e.Plan(ctx, []Control{fileControl{id: "example.file", path: confPath, want: "listen 2230\n"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			checkBestEffort(t, p, tc.finding)
			if tc.refused {
				if err := p.Err(); !errors.Is(err, ErrUnsupported) {
					t.Fatalf("Plan.Err = %v, want ErrUnsupported: the gate is unchanged", err)
				}
				if n := tc.facts.Platform.Note; n != "" && !strings.Contains(p.Err().Error(), "is out of scope ("+n+")") {
					t.Errorf("Plan.Err = %v, want the release's note, %q", p.Err(), n)
				}
				return
			}
			if err := p.Err(); err != nil {
				t.Fatalf("Plan.Err = %v, want none", err)
			}
			if res, err := e.Apply(ctx, p); err != nil || len(res.Applied) != 1 {
				t.Fatalf("Apply: %+v, %v; want the change applied", res, err)
			}
			// Planned again on the result, nothing to change: the finding
			// stays, since every plan made on the release carries it.
			p, err = e.Plan(ctx, []Control{fileControl{id: "example.file", path: confPath, want: "listen 2230\n"}}, nil)
			if err != nil || !p.Empty() {
				t.Fatalf("planned again: %v, %d changes", err, len(p.Changes))
			}
			checkBestEffort(t, p, tc.finding)
		})
	}
}

// checkBestEffort fails unless p's findings are one CodeBestEffort whose
// detail holds want, or none when want is "".
func checkBestEffort(t *testing.T, p *Plan, want string) {
	t.Helper()
	switch {
	case want == "" && len(p.Findings) != 0:
		t.Errorf("findings %+v, want none", p.Findings)
	case want != "" && (len(p.Findings) != 1 || p.Findings[0].Code != CodeBestEffort || !strings.Contains(p.Findings[0].Detail, want)):
		t.Errorf("findings %+v, want one %s with %q", p.Findings, CodeBestEffort, want)
	}
}
