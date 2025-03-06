// pkg/domain/model/dns_config.go
package model

// DNSConfig represents DNS configuration settings
type DNSConfig struct {
    Nameservers []string
    Domain      string
    Search      []string
}