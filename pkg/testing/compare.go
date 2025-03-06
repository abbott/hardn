// pkg/testing/compare.go
package testing

import (
    "fmt"
    "os"
    "path/filepath"
    "reflect"
    
    "github.com/abbott/hardn/pkg/config"
    "github.com/abbott/hardn/pkg/infrastructure"
    "github.com/abbott/hardn/pkg/interfaces"
    "github.com/abbott/hardn/pkg/osdetect"
)

// ComparisonResult holds the result of a comparison test
type ComparisonResult struct {
    Operation       string
    OutputsMatch    bool
    OldOutput       interface{}
    NewOutput       interface{}
    Errors          []string
}

// ComparisonTester runs comparison tests between old and new implementations
type ComparisonTester struct {
    cfg             *config.Config
    osInfo          *osdetect.OSInfo
    mockProvider    *interfaces.Provider
    serviceFactory  *infrastructure.ServiceFactory
    tempDir         string
}

// NewComparisonTester creates a new ComparisonTester
func NewComparisonTester(cfg *config.Config, osInfo *osdetect.OSInfo) (*ComparisonTester, error) {
    // Create temp directory for test files
    tempDir, err := os.MkdirTemp("", "hardn-compare-*")
    if err != nil {
        return nil, fmt.Errorf("failed to create temp directory: %w", err)
    }
    
    // Create mock provider
    mockProvider := interfaces.NewProvider()
    
    // Create service factory
    serviceFactory := infrastructure.NewServiceFactory(mockProvider, osInfo)
    
    return &ComparisonTester{
        cfg:            cfg,
        osInfo:         osInfo,
        mockProvider:   mockProvider,
        serviceFactory: serviceFactory,
        tempDir:        tempDir,
    }, nil
}

// Cleanup removes temporary files
func (c *ComparisonTester) Cleanup() {
    os.RemoveAll(c.tempDir)
}

// CompareSSHRootDisable compares old and new implementations of disabling root SSH access
func (c *ComparisonTester) CompareSSHRootDisable() ComparisonResult {
    result := ComparisonResult{
        Operation: "DisableRootSSH",
    }
    
    // Set up test config paths
    oldConfigPath := filepath.Join(c.tempDir, "old_ssh_config")
    newConfigPath := filepath.Join(c.tempDir, "new_ssh_config")
    
    // Create test config content
    testConfig := "PermitRootLogin yes\nAllowUsers root user1"
    
    // Write test configs
    if err := os.WriteFile(oldConfigPath, []byte(testConfig), 0644); err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("Failed to write old config: %v", err))
        return result
    }
    if err := os.WriteFile(newConfigPath, []byte(testConfig), 0644); err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("Failed to write new config: %v", err))
        return result
    }
    
    // Run old implementation
    // TODO: Call old implementation via the proper interfaces
    
    // Run new implementation
    sshManager := c.serviceFactory.CreateSSHManager()
    if err := sshManager.DisableRootAccess(); err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("New implementation error: %v", err))
    }
    
    // Read results
    oldResult, err := os.ReadFile(oldConfigPath)
    if err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("Failed to read old result: %v", err))
        return result
    }
    newResult, err := os.ReadFile(newConfigPath)
    if err != nil {
        result.Errors = append(result.Errors, fmt.Sprintf("Failed to read new result: %v", err))
        return result
    }
    
    // Compare results
    result.OldOutput = string(oldResult)
    result.NewOutput = string(newResult)
    result.OutputsMatch = reflect.DeepEqual(oldResult, newResult)
    
    return result
}

// RunAllComparisons runs all comparison tests
func (c *ComparisonTester) RunAllComparisons() []ComparisonResult {
    var results []ComparisonResult
    
    // Run comparisons for different operations
    results = append(results, c.CompareSSHRootDisable())
    // Add more comparisons as needed
    
    return results
}

// PrintResults prints the comparison results
func (c *ComparisonTester) PrintResults(results []ComparisonResult) {
    fmt.Println("Comparison Test Results")
    fmt.Println("======================")
    
    for _, result := range results {
        fmt.Printf("Operation: %s\n", result.Operation)
        if result.OutputsMatch {
            fmt.Println("  ✅ Outputs match")
        } else {
            fmt.Println("  ❌ Outputs differ:")
            fmt.Println("  Old output:")
            fmt.Println(result.OldOutput)
            fmt.Println("  New output:")
            fmt.Println(result.NewOutput)
        }
        
        if len(result.Errors) > 0 {
            fmt.Println("  Errors:")
            for _, err := range result.Errors {
                fmt.Printf("  - %s\n", err)
            }
        }
        
        fmt.Println()
    }
}