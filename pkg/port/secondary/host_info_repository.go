// pkg/port/secondary/host_info_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// HostInfoRepository defines the interface for retrieving host information
type HostInfoRepository interface {
	// GetHostInfo retrieves the system information about the host
	GetHostInfo() (*model.HostInfo, error)
}
