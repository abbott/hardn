// pkg/port/secondary/dns_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// DNSRepository defines the interface for DNS configuration operations
type DNSRepository interface {
    // SaveDNSConfig persists the DNS configuration
    SaveDNSConfig(config model.DNSConfig) error
    
    // GetDNSConfig retrieves the current DNS configuration
    GetDNSConfig() (*model.DNSConfig, error)
}