// pkg/interfaces/mock_interfaces.go
package interfaces

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MockFileSystem provides a mock implementation of FileSystem for testing
type MockFileSystem struct {
	// Maps to store mock data
	Files       map[string][]byte
	FileInfos   map[string]os.FileInfo
	Directories map[string]bool

	// Error responses for specific operations
	ReadFileError map[string]error
	StatError     map[string]error
}

// NewMockFileSystem creates a new initialized MockFileSystem
func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		Files:         make(map[string][]byte),
		FileInfos:     make(map[string]os.FileInfo),
		Directories:   make(map[string]bool),
		ReadFileError: make(map[string]error),
		StatError:     make(map[string]error),
	}
}

func (m MockFileSystem) ReadFile(filename string) ([]byte, error) {
	if err, ok := m.ReadFileError[filename]; ok && err != nil {
		return nil, err
	}

	data, exists := m.Files[filename]
	if !exists {
		return nil, fmt.Errorf("file not found: %s", filename)
	}
	return data, nil
}

func (m MockFileSystem) Stat(name string) (os.FileInfo, error) {
	if err, ok := m.StatError[name]; ok && err != nil {
		return nil, err
	}

	info, exists := m.FileInfos[name]
	if exists {
		return info, nil
	}

	if _, exists := m.Files[name]; exists {
		return mockFileInfo{name: name, isDir: false}, nil
	}

	if _, exists := m.Directories[name]; exists {
		return mockFileInfo{name: name, isDir: true}, nil
	}

	return nil, os.ErrNotExist
}

// Mock implementation of os.FileInfo for testing
type mockFileInfo struct {
	name  string
	size  int64
	mode  os.FileMode
	isDir bool
}

func (m mockFileInfo) Name() string       { return filepath.Base(m.name) }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

// MockCommander provides a mock implementation of Commander for testing
type MockCommander struct {
	// Maps command + args string to output
	CommandOutputs map[string][]byte
	CommandErrors  map[string]error

	// Track executed commands for verification
	ExecutedCommands []string
}

// NewMockCommander creates a new MockCommander
func NewMockCommander() *MockCommander {
	return &MockCommander{
		CommandOutputs:   make(map[string][]byte),
		CommandErrors:    make(map[string]error),
		ExecutedCommands: []string{},
	}
}

func (m *MockCommander) Execute(command string, args ...string) ([]byte, error) {
	// Create command string for lookup
	cmdString := command
	for _, arg := range args {
		cmdString += " " + arg
	}

	// Record this command was executed
	m.ExecutedCommands = append(m.ExecutedCommands, cmdString)

	// Return mock response
	if err, ok := m.CommandErrors[cmdString]; ok && err != nil {
		return nil, err
	}

	if output, ok := m.CommandOutputs[cmdString]; ok {
		return output, nil
	}

	// Default empty response
	return []byte{}, nil
}
