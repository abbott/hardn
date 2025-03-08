package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MockPackageRepository implements PackageRepository interface for testing
type MockPackageRepository struct {
	// Install Packages tracking
	InstalledRequest model.PackageInstallRequest
	InstallError     error
	InstallCallCount int

	// Update package sources tracking
	UpdatedSources      model.PackageSources
	UpdateSourcesError  error
	UpdateSourcesCalled bool

	// Update Proxmox sources tracking
	UpdatedProxmoxSources model.PackageSources
	UpdateProxmoxError    error
	UpdateProxmoxCalled   bool

	// Package installed check tracking
	CheckedPackage         string
	PackageInstalledResult bool
	PackageInstalledError  error
	PackageInstalledCalled bool

	// Package sources retrieval tracking
	ReturnedSources  *model.PackageSources
	GetSourcesError  error
	GetSourcesCalled bool
}

func (m *MockPackageRepository) InstallPackages(request model.PackageInstallRequest) error {
	m.InstalledRequest = request
	m.InstallCallCount++
	return m.InstallError
}

func (m *MockPackageRepository) UpdatePackageSources(sources model.PackageSources) error {
	m.UpdatedSources = sources
	m.UpdateSourcesCalled = true
	return m.UpdateSourcesError
}

func (m *MockPackageRepository) UpdateProxmoxSources(sources model.PackageSources) error {
	m.UpdatedProxmoxSources = sources
	m.UpdateProxmoxCalled = true
	return m.UpdateProxmoxError
}

func (m *MockPackageRepository) IsPackageInstalled(packageName string) (bool, error) {
	m.CheckedPackage = packageName
	m.PackageInstalledCalled = true
	return m.PackageInstalledResult, m.PackageInstalledError
}

func (m *MockPackageRepository) GetPackageSources() (*model.PackageSources, error) {
	m.GetSourcesCalled = true
	return m.ReturnedSources, m.GetSourcesError
}

func TestNewPackageServiceImpl(t *testing.T) {
	repo := &MockPackageRepository{}
	osInfo := model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"}

	service := NewPackageServiceImpl(repo, osInfo)

	if service == nil {
		t.Fatal("Expected non-nil service")
	}

	if service.repository != repo {
		t.Error("Repository not properly set")
	}

	if !reflect.DeepEqual(service.osInfo, osInfo) {
		t.Error("OSInfo not properly set")
	}
}

func TestPackageServiceImpl_InstallPackages(t *testing.T) {
	tests := []struct {
		name         string
		request      model.PackageInstallRequest
		installError error
		osInfo       model.OSInfo
		expectError  bool
	}{
		{
			name: "debian system packages",
			request: model.PackageInstallRequest{
				Packages:    []string{"ufw", "unattended-upgrades"},
				PackageType: "Core",
				IsPython:    false,
			},
			installError: nil,
			osInfo:       model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:  false,
		},
		{
			name: "alpine system packages",
			request: model.PackageInstallRequest{
				Packages:    []string{"ufw", "python3"},
				PackageType: "Core",
				IsPython:    false,
			},
			installError: nil,
			osInfo:       model.OSInfo{Type: "alpine", Version: "3.16"},
			expectError:  false,
		},
		{
			name: "debian python packages",
			request: model.PackageInstallRequest{
				Packages:       []string{"python3-pip"},
				PipPackages:    []string{"requests", "paramiko"},
				PackageType:    "Core",
				IsPython:       true,
				IsSystemPython: true,
			},
			installError: nil,
			osInfo:       model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:  false,
		},
		{
			name: "proxmox packages",
			request: model.PackageInstallRequest{
				Packages:    []string{"ufw", "zfsutils-linux"},
				PackageType: "Core",
				IsPython:    false,
			},
			installError: nil,
			osInfo:       model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye", IsProxmox: true},
			expectError:  false,
		},
		{
			name: "repository error",
			request: model.PackageInstallRequest{
				Packages:    []string{"ufw", "fail2ban"},
				PackageType: "Core",
				IsPython:    false,
			},
			installError: errors.New("mock installation error"),
			osInfo:       model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:  true,
		},
		{
			name: "empty package request",
			request: model.PackageInstallRequest{
				Packages:    []string{},
				PipPackages: []string{},
				IsPython:    false,
			},
			installError: nil,
			osInfo:       model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockPackageRepository{
				InstallError: tc.installError,
			}

			service := NewPackageServiceImpl(repo, tc.osInfo)

			// Execute
			err := service.InstallPackages(tc.request)

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Handle the empty package case differently
			if len(tc.request.Packages) == 0 && len(tc.request.PipPackages) == 0 {
				// For empty requests, expect repository method not to be called
				if repo.InstallCallCount != 0 {
					t.Errorf("Expected InstallPackages not to be called for empty request, but was called %d times", repo.InstallCallCount)
				}
				return // Skip further checks for empty requests
			}

			// For non-empty requests, expect the method to be called once
			if repo.InstallCallCount != 1 {
				t.Errorf("Expected InstallPackages to be called once, got %d", repo.InstallCallCount)
			}

			if !reflect.DeepEqual(repo.InstalledRequest, tc.request) {
				t.Errorf("Wrong request passed to repository. Got %+v, expected %+v", repo.InstalledRequest, tc.request)
			}
		})
	}
}

func TestPackageServiceImpl_UpdatePackageSources(t *testing.T) {
	tests := []struct {
		name               string
		sources            *model.PackageSources
		getSourcesError    error
		updateSourcesError error
		osInfo             model.OSInfo
		expectError        bool
	}{
		{
			name: "debian successful update",
			sources: &model.PackageSources{
				DebianRepos: []string{
					"deb http://deb.debian.org/debian CODENAME main contrib non-free",
					"deb http://security.debian.org/debian-security CODENAME-security main contrib non-free",
				},
			},
			getSourcesError:    nil,
			updateSourcesError: nil,
			osInfo:             model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:        false,
		},
		{
			name: "alpine successful update",
			sources: &model.PackageSources{
				AlpineTestingRepo: true,
			},
			getSourcesError:    nil,
			updateSourcesError: nil,
			osInfo:             model.OSInfo{Type: "alpine", Version: "3.16"},
			expectError:        false,
		},
		{
			name:            "get sources error",
			sources:         nil,
			getSourcesError: errors.New("mock get sources error"),
			osInfo:          model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:     true,
		},
		{
			name: "update sources error",
			sources: &model.PackageSources{
				DebianRepos: []string{
					"deb http://deb.debian.org/debian CODENAME main contrib non-free",
				},
			},
			getSourcesError:    nil,
			updateSourcesError: errors.New("mock update sources error"),
			osInfo:             model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockPackageRepository{
				ReturnedSources:    tc.sources,
				GetSourcesError:    tc.getSourcesError,
				UpdateSourcesError: tc.updateSourcesError,
			}

			service := NewPackageServiceImpl(repo, tc.osInfo)

			// Execute
			err := service.UpdatePackageSources()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !repo.GetSourcesCalled {
				t.Error("Expected GetPackageSources to be called")
			}

			// Check if the update method was called when expected
			if tc.getSourcesError == nil {
				if !repo.UpdateSourcesCalled {
					t.Error("Expected UpdatePackageSources to be called")
				}

				if tc.sources != nil && !reflect.DeepEqual(repo.UpdatedSources, *tc.sources) {
					t.Errorf("Wrong sources passed to repository. Got %+v, expected %+v", repo.UpdatedSources, *tc.sources)
				}
			} else {
				if repo.UpdateSourcesCalled {
					t.Error("UpdatePackageSources should not have been called when GetPackageSources fails")
				}
			}
		})
	}
}

func TestPackageServiceImpl_UpdateProxmoxSources(t *testing.T) {
	tests := []struct {
		name               string
		sources            *model.PackageSources
		getSourcesError    error
		updateProxmoxError error
		osInfo             model.OSInfo
		expectError        bool
	}{
		{
			name: "proxmox successful update",
			sources: &model.PackageSources{
				ProxmoxCephRepo: []string{
					"deb http://download.proxmox.com/debian/ceph-pacific CODENAME main",
				},
				ProxmoxEnterpriseRepo: []string{
					"# deb https://enterprise.proxmox.com/debian/pve CODENAME pve-enterprise",
				},
			},
			getSourcesError:    nil,
			updateProxmoxError: nil,
			osInfo:             model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye", IsProxmox: true},
			expectError:        false,
		},
		{
			name:            "get sources error",
			sources:         nil,
			getSourcesError: errors.New("mock get sources error"),
			osInfo:          model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye", IsProxmox: true},
			expectError:     true,
		},
		{
			name: "update proxmox error",
			sources: &model.PackageSources{
				ProxmoxCephRepo: []string{
					"deb http://download.proxmox.com/debian/ceph-pacific CODENAME main",
				},
			},
			getSourcesError:    nil,
			updateProxmoxError: errors.New("mock update proxmox error"),
			osInfo:             model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye", IsProxmox: true},
			expectError:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockPackageRepository{
				ReturnedSources:    tc.sources,
				GetSourcesError:    tc.getSourcesError,
				UpdateProxmoxError: tc.updateProxmoxError,
			}

			service := NewPackageServiceImpl(repo, tc.osInfo)

			// Execute
			err := service.UpdateProxmoxSources()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !repo.GetSourcesCalled {
				t.Error("Expected GetPackageSources to be called")
			}

			// Check if the update method was called when expected
			if tc.getSourcesError == nil {
				if !repo.UpdateProxmoxCalled {
					t.Error("Expected UpdateProxmoxSources to be called")
				}

				if tc.sources != nil && !reflect.DeepEqual(repo.UpdatedProxmoxSources, *tc.sources) {
					t.Errorf("Wrong sources passed to repository. Got %+v, expected %+v", repo.UpdatedProxmoxSources, *tc.sources)
				}
			} else {
				if repo.UpdateProxmoxCalled {
					t.Error("UpdateProxmoxSources should not have been called when GetPackageSources fails")
				}
			}
		})
	}
}

func TestPackageServiceImpl_IsPackageInstalled(t *testing.T) {
	tests := []struct {
		name            string
		packageName     string
		isInstalled     bool
		checkError      error
		osInfo          model.OSInfo
		expectError     bool
		expectInstalled bool
	}{
		{
			name:            "package is installed",
			packageName:     "ufw",
			isInstalled:     true,
			checkError:      nil,
			osInfo:          model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:     false,
			expectInstalled: true,
		},
		{
			name:            "package not installed",
			packageName:     "nonexistent-pkg",
			isInstalled:     false,
			checkError:      nil,
			osInfo:          model.OSInfo{Type: "alpine", Version: "3.16"},
			expectError:     false,
			expectInstalled: false,
		},
		{
			name:            "repository error",
			packageName:     "ufw",
			isInstalled:     false,
			checkError:      errors.New("mock check error"),
			osInfo:          model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"},
			expectError:     true,
			expectInstalled: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockPackageRepository{
				PackageInstalledResult: tc.isInstalled,
				PackageInstalledError:  tc.checkError,
			}

			service := NewPackageServiceImpl(repo, tc.osInfo)

			// Execute
			installed, err := service.IsPackageInstalled(tc.packageName)

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if installed != tc.expectInstalled {
				t.Errorf("Wrong installed status. Got %v, expected %v", installed, tc.expectInstalled)
			}

			if !repo.PackageInstalledCalled {
				t.Error("Expected IsPackageInstalled to be called")
			}

			if repo.CheckedPackage != tc.packageName {
				t.Errorf("Wrong package name passed. Got %s, expected %s", repo.CheckedPackage, tc.packageName)
			}
		})
	}
}

func TestPackageServiceImpl_OSTypeHandling(t *testing.T) {
	osTypes := []struct {
		osType    string
		osVersion string
		isProxmox bool
	}{
		{osType: "debian", osVersion: "11", isProxmox: false},
		{osType: "ubuntu", osVersion: "20.04", isProxmox: false},
		{osType: "alpine", osVersion: "3.16", isProxmox: false},
		{osType: "debian", osVersion: "11", isProxmox: true}, // Proxmox case
	}

	for _, os := range osTypes {
		t.Run(os.osType+"_"+os.osVersion, func(t *testing.T) {
			// Setup
			repo := &MockPackageRepository{
				ReturnedSources: &model.PackageSources{
					DebianRepos: []string{"deb http://example.com CODENAME main"},
				},
			}

			osInfo := model.OSInfo{
				Type:      os.osType,
				Version:   os.osVersion,
				IsProxmox: os.isProxmox,
			}

			service := NewPackageServiceImpl(repo, osInfo)

			// Execute
			err := service.UpdatePackageSources()

			// Verify
			if err != nil {
				t.Errorf("Failed to update package sources on %s %s: %v", os.osType, os.osVersion, err)
			}

			// Just a basic test to ensure service handles different OSes gracefully
			if !repo.UpdateSourcesCalled {
				t.Errorf("Expected UpdatePackageSources to be called for %s", os.osType)
			}
		})
	}
}
