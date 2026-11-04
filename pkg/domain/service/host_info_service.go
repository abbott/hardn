// pkg/domain/service/host_info_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// HostInfoService defines operations for retrieving host information
type HostInfoService interface {
	// GetHostInfo retrieves the system information about the host
	GetHostInfo() (*model.HostInfo, error)
}

// HostInfoServiceImpl implements HostInfoService
type HostInfoServiceImpl struct {
	hostInfoRepo HostInfoRepository
	osInfo       model.OSInfo
}

// NewHostInfoServiceImpl creates a new HostInfoServiceImpl
func NewHostInfoServiceImpl(hostInfoRepo HostInfoRepository, osInfo model.OSInfo) *HostInfoServiceImpl {
	return &HostInfoServiceImpl{
		hostInfoRepo: hostInfoRepo,
		osInfo:       osInfo,
	}
}

// HostInfoRepository defines the repository operations needed by HostInfoService
type HostInfoRepository interface {
	GetHostInfo() (*model.HostInfo, error)
}

// GetHostInfo retrieves comprehensive host information
func (s *HostInfoServiceImpl) GetHostInfo() (*model.HostInfo, error) {
	return s.hostInfoRepo.GetHostInfo()
}
