// pkg/domain/model/environment.go
package model

// EnvironmentConfig represents environment variable configuration settings
type EnvironmentConfig struct {
	// ConfigPath is the path to the configuration file specified by HARDN_CONFIG
	ConfigPath string

	// PreserveSudo indicates whether HARDN_CONFIG should be preserved in sudo
	PreserveSudo bool

	// Username of the current user for sudo configuration
	Username string
}
