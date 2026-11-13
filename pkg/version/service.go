// Package version is hardn's own version: what --version prints (Service)
// and the release check (Checker, release.go), which asks GitHub for the
// latest release's tag where hardn.yml's checkReleases turns it on.
package version

import (
	"fmt"
	"io"
)

// Service is hardn's build info.
type Service struct {
	CurrentVersion string
	BuildDate      string
	GitCommit      string
}

// NewService creates a version service instance
func NewService(currentVersion, buildDate, gitCommit string) *Service {
	return &Service{
		CurrentVersion: currentVersion,
		BuildDate:      buildDate,
		GitCommit:      gitCommit,
	}
}

// PrintVersionInfo writes hardn --version's lines to w: the version, the
// build date and the commit, from the ldflags (main.Version,
// main.BuildDate, main.GitCommit), as v0.3.2 printed them, each "unknown"
// when the build did not set it (refactor plan §3.11, approved
// 2026-11-04: the build info restored), then the license and that it
// carries no warranty (the LICENSE's sections 15 and 16).
func (s *Service) PrintVersionInfo(w io.Writer) error {
	known := func(v string) string {
		if v == "" {
			return "unknown"
		}
		return v
	}
	_, err := fmt.Fprintf(w, "hardn - Linux hardening tool\nVersion:    %s\nBuild Date: %s\nGit Commit: %s\nLicense:    GNU AGPL v3, no warranty\n",
		known(s.CurrentVersion), known(s.BuildDate), known(s.GitCommit))
	return err
}
