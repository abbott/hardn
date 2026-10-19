// pkg/interfaces/os_filesystem.go
package interfaces

import (
	"io/fs"
	"os"
	"path/filepath"
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

func (fs OSFileSystem) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	if legacyThroughHost {
		return SystemFileSystem{Sys: realSystem()}.WriteFile(filename, data, perm)
	}
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(filename, data, perm)
}

func (fs OSFileSystem) MkdirAll(path string, perm fs.FileMode) error {
	if legacyThroughHost {
		return SystemFileSystem{Sys: realSystem()}.MkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (fs OSFileSystem) Stat(name string) (os.FileInfo, error) {
	if legacyThroughHost {
		return SystemFileSystem{Sys: realSystem()}.Stat(name)
	}
	return os.Stat(name)
}

func (fs OSFileSystem) Remove(name string) error {
	if legacyThroughHost {
		return SystemFileSystem{Sys: realSystem()}.Remove(name)
	}
	return os.Remove(name)
}

func (fs OSFileSystem) RemoveAll(path string) error {
	if legacyThroughHost {
		return SystemFileSystem{Sys: realSystem()}.RemoveAll(path)
	}
	return os.RemoveAll(path)
}
