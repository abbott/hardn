// pkg/interfaces/interfaces.go
package interfaces

import (
	"os"
)

// FileSystem abstracts filesystem operations
type FileSystem interface {
	ReadFile(filename string) ([]byte, error)
	Stat(name string) (os.FileInfo, error)
}

// Commander abstracts command execution
type Commander interface {
	Execute(command string, args ...string) ([]byte, error)
}
