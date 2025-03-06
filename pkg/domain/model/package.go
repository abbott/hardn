// pkg/domain/model/package.go
package model

// PackageInstallRequest represents a request to install packages
type PackageInstallRequest struct {
	Packages      []string
	PipPackages   []string
	PackageType   string // Core, DMZ, Lab, etc.
	UseUv         bool   // Whether to use UV for Python packages
	IsPython      bool   // Whether this is a Python package install request
	IsSystemPython bool  // Whether to install system Python packages
}

// RepositorySource represents a package repository source
type RepositorySource struct {
	URL         string
	Distribution string
	Components  []string
	Enabled     bool
}

// PackageSources represents package repository sources configuration
type PackageSources struct {
	DebianRepos           []string
	ProxmoxSrcRepos       []string
	ProxmoxCephRepo       []string
	ProxmoxEnterpriseRepo []string
	AlpineTestingRepo     bool
}