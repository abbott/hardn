package interfaces

import (
	"fmt"
	"net"
	"strings"
)

// GetInterfaces returns a list of network interfaces
func (o OSNetworkOperations) GetInterfaces() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	var names []string
	for _, iface := range interfaces {
		names = append(names, iface.Name)
	}

	return names, nil
}

// CheckSubnet checks if the specified subnet is present in the system's interfaces
func (o OSNetworkOperations) CheckSubnet(subnet string) (bool, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}

			// Check if IP matches subnet
			if strings.HasPrefix(ip.String(), subnet+".") {
				return true, nil
			}
		}
	}

	return false, nil
}
