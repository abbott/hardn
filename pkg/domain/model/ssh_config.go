// pkg/domain/model/ssh_config.go
package model

// SSHConfig represents SSH server configuration settings
type SSHConfig struct {
	Port            int
	ListenAddresses []string
	PermitRootLogin bool
	AllowedUsers    []string
	KeyPaths        []string
	AuthMethods     []string
	ConfigFilePath  string
}

// SSHKey represents an SSH public key
type SSHKey struct {
	User      string
	PublicKey string
	KeyType   string
	Comment   string
}
