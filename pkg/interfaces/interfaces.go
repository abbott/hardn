// pkg/interfaces/interfaces.go
package interfaces

import (
	"io/fs"
	"os"
)

// FileSystem abstracts filesystem operations
type FileSystem interface {
	ReadFile(filename string) ([]byte, error)
	WriteFile(filename string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Stat(name string) (os.FileInfo, error)
	Remove(name string) error
	RemoveAll(path string) error
}

// Commander abstracts command execution
type Commander interface {
	Execute(command string, args ...string) ([]byte, error)
	ExecuteWithInput(input string, command string, args ...string) ([]byte, error)
}

// NetworkOperations abstracts network-related operations
type NetworkOperations interface {
	GetInterfaces() ([]string, error)
	CheckSubnet(subnet string) (bool, error)
}
