// pkg/interfaces/provider.go
package interfaces

// Provider holds interfaces for dependency injection
type Provider struct {
	FS        FileSystem
	Commander Commander
}

// NewProvider creates a new Provider with default implementations
func NewProvider() *Provider {
	return &Provider{
		FS:        OSFileSystem{},
		Commander: OSCommander{},
	}
}

// Mock implementations are defined in mocks.go
