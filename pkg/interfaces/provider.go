// pkg/interfaces/provider.go
package interfaces

// Provider holds interfaces for dependency injection
type Provider struct {
	FS        FileSystem
	Commander Commander
	Network   NetworkOperations
}

// NewProvider creates a new Provider with default implementations
func NewProvider() *Provider {
	return &Provider{
		FS:        OSFileSystem{},
		Commander: OSCommander{},
		Network:   OSNetworkOperations{},
	}
}

// MockProvider creates a Provider with mock implementations for testing
func MockProvider() *Provider {
	return &Provider{
		FS:        MockFileSystem{},
		Commander: &MockCommander{},
		Network:   MockNetworkOperations{},
	}
}

// OSNetworkOperations implements NetworkOperations
type OSNetworkOperations struct{}

// Add implementations for OSNetworkOperations...

// Mock implementations are defined in mocks.go
