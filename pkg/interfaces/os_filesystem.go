// pkg/interfaces/os_filesystem.go
package interfaces

import (
	"io/fs"
	"os"
	"path/filepath"
)

// OSFileSystem is an implementation of FileSystem using the os package
type OSFileSystem struct{}

func (fs OSFileSystem) ReadFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

func (fs OSFileSystem) WriteFile(filename string, data []byte, perm fs.FileMode) error {
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
	return os.MkdirAll(path, perm)
}

func (fs OSFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (fs OSFileSystem) Remove(name string) error {
	return os.Remove(name)
}

func (fs OSFileSystem) RemoveAll(path string) error {
	return os.RemoveAll(path)
}