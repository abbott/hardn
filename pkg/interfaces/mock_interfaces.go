// pkg/interfaces/mock_interfaces.go
package interfaces

import (
	"fmt"
	"time"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MockFileSystem provides a mock implementation of FileSystem for testing
type MockFileSystem struct {
	// Maps to store mock data
	Files      map[string][]byte
	FileInfos  map[string]os.FileInfo
	Directories map[string]bool
	
	// Error responses for specific operations
	ReadFileError  map[string]error
	WriteFileError map[string]error
	MkdirAllError  map[string]error
	StatError      map[string]error
	RemoveError    map[string]error
	RemoveAllError map[string]error
}

// NewMockFileSystem creates a new initialized MockFileSystem
func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		Files:         make(map[string][]byte),
		FileInfos:     make(map[string]os.FileInfo),
		Directories:   make(map[string]bool),
		ReadFileError: make(map[string]error),
		WriteFileError: make(map[string]error),
		MkdirAllError:  make(map[string]error),
		StatError:      make(map[string]error),
		RemoveError:    make(map[string]error),
		RemoveAllError: make(map[string]error),
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

func (m MockFileSystem) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	if err, ok := m.WriteFileError[filename]; ok && err != nil {
		return err
	}
	
	// Ensure directory exists in our mock
	dir := filepath.Dir(filename)
	if dir != "." {
		m.Directories[dir] = true
	}
	
	m.Files[filename] = data
	return nil
}

func (m MockFileSystem) MkdirAll(path string, perm fs.FileMode) error {
	if err, ok := m.MkdirAllError[path]; ok && err != nil {
		return err
	}
	
	m.Directories[path] = true
	return nil
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

func (m MockFileSystem) Remove(name string) error {
	if err, ok := m.RemoveError[name]; ok && err != nil {
		return err
	}
	
	delete(m.Files, name)
	delete(m.FileInfos, name)
	delete(m.Directories, name)
	return nil
}

func (m MockFileSystem) RemoveAll(path string) error {
	if err, ok := m.RemoveAllError[path]; ok && err != nil {
		return err
	}
	
	// Remove all files and directories with this prefix
	for key := range m.Files {
		if strings.HasPrefix(key, path) {
			delete(m.Files, key)
		}
	}
	
	for key := range m.FileInfos {
		if strings.HasPrefix(key, path) {
			delete(m.FileInfos, key)
		}
	}
	
	for key := range m.Directories {
		if strings.HasPrefix(key, path) {
			delete(m.Directories, key)
		}
	}
	
	return nil
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

func (m *MockCommander) ExecuteWithInput(input string, command string, args ...string) ([]byte, error) {
	// Create command string for lookup
	cmdString := "INPUT:" + input + "|" + command
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

// MockNetworkOperations provides a mock implementation of NetworkOperations
type MockNetworkOperations struct {
	// Mock data
	Interfaces []string
	Subnets    map[string]bool
	
	// Errors
	GetInterfacesError error
	CheckSubnetError   error
}

// NewMockNetworkOperations creates a new MockNetworkOperations
func NewMockNetworkOperations() *MockNetworkOperations {
	return &MockNetworkOperations{
		Interfaces: []string{},
		Subnets:    make(map[string]bool),
	}
}

func (m MockNetworkOperations) GetInterfaces() ([]string, error) {
	if m.GetInterfacesError != nil {
		return nil, m.GetInterfacesError
	}
	return m.Interfaces, nil
}

func (m MockNetworkOperations) CheckSubnet(subnet string) (bool, error) {
	if m.CheckSubnetError != nil {
		return false, m.CheckSubnetError
	}
	return m.Subnets[subnet], nil
}