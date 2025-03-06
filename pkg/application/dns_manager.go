// pkg/application/dns_manager.go
package application

import (
    "github.com/abbott/hardn/pkg/domain/model"
    "github.com/abbott/hardn/pkg/domain/service"
)

// DNSManager is an application service for DNS configuration
type DNSManager struct {
    dnsService service.DNSService
}

// NewDNSManager creates a new DNSManager
func NewDNSManager(dnsService service.DNSService) *DNSManager {
    return &DNSManager{
        dnsService: dnsService,
    }
}

// ConfigureDNS applies DNS configuration with the specified nameservers
func (m *DNSManager) ConfigureDNS(nameservers []string, domain string) error {
    // Create DNS config
    config := model.DNSConfig{
        Nameservers: nameservers,
        Domain:      domain,
        Search:      []string{domain},
    }
    
    return m.dnsService.ConfigureDNS(config)
}

// ConfigureSecureDNS applies DNS configuration with secure default nameservers
func (m *DNSManager) ConfigureSecureDNS() error {
    // Use Cloudflare DNS by default (secure and privacy-focused)
    cloudflareNameservers := []string{"1.1.1.1", "1.0.0.1"}
    
    return m.ConfigureDNS(cloudflareNameservers, "lan")
}

// GetCurrentConfig retrieves the current DNS configuration
func (m *DNSManager) GetCurrentConfig() (*model.DNSConfig, error) {
    return m.dnsService.GetCurrentConfig()
}