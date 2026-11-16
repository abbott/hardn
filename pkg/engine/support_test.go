package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
		f.Platform = detect.Classify(detect.OSRelease{ID: "alpine", VersionID: "3.21.5"}, detect.Proxmox{Installed: detect.No}, detect.Custom{TrueNAS: detect.No, RaspiOS: detect.No})
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
	var got []Finding
	for _, f := range p.Findings {
		if f.Code == CodeBestEffort {
			got = append(got, f)
		}
	}
	switch {
	case want == "" && len(got) != 0:
		t.Errorf("findings %+v, want no %s", p.Findings, CodeBestEffort)
	case want != "" && (len(got) != 1 || !strings.Contains(got[0].Detail, want)):
		t.Errorf("findings %+v, want one %s with %q", p.Findings, CodeBestEffort, want)
	}
}

// TestEndOfSupportFinding (docs/engine/gallery-eol.md, adopted 2026-11-13):
// a plan made on a release whose vendor support ends within 90 days, or has
// ended, by the engine's clock, carries one finding of it, at every support
// level, after the best-effort one; a release further from its date, with
// no date or with no matrix row carries none. The clock moves no gate: a
// best-effort or supported release past its date is still applied, and an
// out-of-scope one is refused as before, whatever the day.
func TestEndOfSupportFinding(t *testing.T) {
	ctx := context.Background()
	classify := func(id, version string) detect.Platform {
		return detect.Classify(detect.OSRelease{ID: id, VersionID: version}, detect.Proxmox{Installed: detect.No}, detect.Custom{TrueNAS: detect.No, RaspiOS: detect.No})
	}
	pve := func(version string) detect.Platform {
		return detect.Classify(detect.OSRelease{ID: "debian", VersionID: "13"}, detect.Proxmox{Installed: detect.Yes, Version: version}, detect.Custom{})
	}
	day := func(s string) time.Time {
		d, err := time.ParseInLocation(time.DateOnly, s, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		return d.Add(15 * time.Hour)
	}
	for _, tc := range []struct {
		name     string
		platform detect.Platform
		today    string
		code     string // "": no end-of-support finding
		detail   string
		codes    int // every finding of the plan
		refused  bool
	}{
		{"Alpine 3.22, 91 days before", classify("alpine", "3.22.4"), "2027-01-30", "", "", 1, false},
		{"Alpine 3.22, 90 days before", classify("alpine", "3.22.4"), "2027-01-31", CodeSupportEnds, "Alpine 3.22 support ends 2027-05-01: upgrade to Alpine 3.24", 2, false},
		{"Alpine 3.22, its last day", classify("alpine", "3.22.4"), "2027-05-01", CodeSupportEnds, "Alpine 3.22 support ends 2027-05-01: upgrade to Alpine 3.24", 2, false},
		{"Alpine 3.22, the day after: still applied", classify("alpine", "3.22.4"), "2027-05-02", CodeSupportEnded, "Alpine 3.22 support ended 2027-05-01: upgrade to Alpine 3.24", 2, false},
		{"Debian 12, supported, past its date: still applied", classify("debian", "12"), "2029-01-01", CodeSupportEnded, "Debian 12 support ended 2028-06-30: upgrade to Debian 13", 1, false},
		{"Debian 13, the newest, names no upgrade", classify("debian", "13"), "2030-06-01", CodeSupportEnds, "Debian 13 support ends 2030-06-30", 1, false},
		{"Ubuntu 22.04, a month's last day", classify("ubuntu", "22.04"), "2027-05-31", CodeSupportEnds, "Ubuntu 22.04 support ends May 2027: upgrade to Ubuntu 26.04", 2, false},
		{"Ubuntu 22.04, the next month", classify("ubuntu", "22.04"), "2027-06-01", CodeSupportEnded, "Ubuntu 22.04 support ended May 2027: upgrade to Ubuntu 26.04", 2, false},
		{"Alpine 3.21, out of scope: refused as before", classify("alpine", "3.21.7"), "2026-11-13", CodeSupportEnded, "Alpine 3.21 support ended 2026-11-01: upgrade to Alpine 3.24", 1, true},
		{"Alpine 3.21, out of scope before its date: refused all the same", classify("alpine", "3.21.7"), "2026-01-01", "", "", 0, true},
		{"Proxmox VE 8, out of scope", pve("8.4.1"), "2026-11-13", CodeSupportEnded, "Proxmox VE 8 support ended August 2026: upgrade to Proxmox VE 9", 2, true},
		{"Proxmox VE 9, no date", pve("9.2.22"), "2040-01-01", "", "", 1, false},
		{"Alpine 3.19, no row", classify("alpine", "3.19.9"), "2031-01-01", "", "", 0, true},
		{"Alpine edge of a listed branch, unlisted", classify("alpine", "3.22.0_alpha20250101"), "2031-01-01", "", "", 0, true},
		{"a platform not established", detect.Platform{}, "2031-01-01", "", "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			facts := debian13
			facts.Platform = tc.platform
			o := testOptions(h, nil, nil, facts)
			o.Revert.Now = func() time.Time { return day(tc.today) }
			e := New(h, o)
			p, err := e.Plan(ctx, []Control{fileControl{id: "example.file", path: confPath, want: "listen 2230\n"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []Finding
			for _, f := range p.Findings {
				if f.Code == CodeSupportEnds || f.Code == CodeSupportEnded {
					got = append(got, f)
				}
			}
			switch {
			case tc.code == "" && len(got) != 0:
				t.Errorf("findings %+v, want none of the end of support", p.Findings)
			case tc.code != "" && (len(got) != 1 || got[0] != Finding{Code: tc.code, Detail: tc.detail} || p.Findings[len(p.Findings)-1] != got[0]):
				t.Errorf("findings %+v, want last %s %q", p.Findings, tc.code, tc.detail)
			}
			if len(p.Findings) != tc.codes {
				t.Errorf("%d findings, want %d: %+v", len(p.Findings), tc.codes, p.Findings)
			}
			if refused := errors.Is(p.Err(), ErrUnsupported); refused != tc.refused {
				t.Fatalf("Plan.Err = %v; refused as unsupported %v, want %v", p.Err(), refused, tc.refused)
			}
			if tc.refused {
				return
			}
			if res, err := e.Apply(ctx, p); err != nil || len(res.Applied) != 1 {
				t.Fatalf("Apply: %+v, %v; want the change applied", res, err)
			}
		})
	}
}

// TestCustomDistributions (design-custom-distros.md, adopted 2026-11-16):
// on Proxmox VE and Raspberry Pi OS every plan carries CodeCustom and is
// applied; on plain Debian none does; on TrueNAS every plan with changes is
// refused, one change or many (Run All), in the user's words, and a plan
// with nothing to change is not.
func TestCustomDistributions(t *testing.T) {
	ctx := context.Background()
	platform := func(d detect.Distro, v detect.Variant) detect.Facts {
		f := debian13
		f.Platform = detect.Platform{Distro: d, Version: "13", Support: detect.Supported, Variant: v}
		if d == detect.DistroProxmox {
			f.Platform.Version = "9"
		}
		return f
	}
	one := []Control{fileControl{id: "example.file", path: confPath, want: "listen 2230\n"}}
	many := []Control{fileControl{id: "example.file", path: confPath, want: "listen 2230\n"}, fileControl{id: "example.other", path: confPath + ".other", want: "x\n"}}
	for _, tc := range []struct {
		name     string
		facts    detect.Facts
		controls []Control
		finding  string // CodeCustom's detail; "": none
		truenas  bool
	}{
		{"Debian 13", debian13, one, "", false},
		{"Proxmox VE 9", platform(detect.DistroProxmox, detect.VariantNone), one,
			"Proxmox VE is a custom distribution: a change that is safe on Debian can break it", false},
		{"Raspberry Pi OS", platform(detect.DistroDebian, detect.VariantRaspiOS), one,
			"Raspberry Pi OS is a custom distribution: a change that is safe on Debian can break it", false},
		{"TrueNAS, one change", platform(detect.DistroDebian, detect.VariantTrueNAS), one, "", true},
		{"TrueNAS, Run All", platform(detect.DistroDebian, detect.VariantTrueNAS), many, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			e := New(h, testOptions(h, nil, nil, tc.facts))
			p, err := e.Plan(ctx, tc.controls, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []Finding
			for _, f := range p.Findings {
				if f.Code == CodeCustom {
					got = append(got, f)
				}
			}
			switch {
			case tc.finding == "" && len(got) != 0:
				t.Errorf("findings %+v, want no %s", p.Findings, CodeCustom)
			case tc.finding != "" && (len(got) != 1 || got[0].Detail != tc.finding):
				t.Errorf("findings %+v, want one %s %q", p.Findings, CodeCustom, tc.finding)
			}
			if tc.truenas {
				if len(p.Refused) != 1 || p.Refused[0] != ErrTrueNAS || !errors.Is(p.Err(), ErrTrueNAS) || errors.Is(p.Err(), ErrUnsupported) {
					t.Fatalf("Refused %v, Err %v; want ErrTrueNAS alone", p.Refused, p.Err())
				}
				const words = "TrueNAS manages its own system: hardn refuses every change. Make them in TrueNAS's web UI"
				if ErrTrueNAS.Error() != words {
					t.Errorf("ErrTrueNAS = %q, want %q", ErrTrueNAS, words)
				}
				return
			}
			if res, err := e.Apply(ctx, p); err != nil || len(res.Applied) != 1 {
				t.Fatalf("Apply: %+v, %v; want the change applied", res, err)
			}
		})
	}
	// Nothing to change on TrueNAS: not refused, as on any platform.
	h := newHost(t)
	e := New(h, testOptions(h, nil, nil, platform(detect.DistroDebian, detect.VariantTrueNAS)))
	p, err := e.Plan(ctx, nil, nil)
	if err != nil || len(p.Refused) != 0 {
		t.Errorf("an empty plan on TrueNAS: %v, refused %v", err, p.Refused)
	}
}
