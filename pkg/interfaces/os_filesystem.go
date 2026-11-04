// pkg/interfaces/os_filesystem.go
package interfaces

import (
	"os"
)

// OSFileSystem is an implementation of FileSystem using the os package. With
// legacyThroughHost it goes through the shared host.Real (SystemFileSystem),
// with the same semantics.
type OSFileSystem struct{}

func (fs OSFileSystem) ReadFile(filename string) ([]byte, error) {
	if legacyThroughHost {
		return SystemFileSystem{Sys: realSystem()}.ReadFile(filename)
	}
	return os.ReadFile(filename)
}

func (fs OSFileSystem) Stat(name string) (os.FileInfo, error) {
	if legacyThroughHost {
		return SystemFileSystem{Sys: realSystem()}.Stat(name)
	}
	return os.Stat(name)
}
