package system

import (
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
)

type stubHostInfoService struct{}

func (stubHostInfoService) GetHostInfo() (*model.HostInfo, error)    { return &model.HostInfo{}, nil }
func (stubHostInfoService) GetIPAddresses() ([]string, error)        { return nil, nil }
func (stubHostInfoService) GetDNSServers() ([]string, error)         { return nil, nil }
func (stubHostInfoService) GetHostname() (string, string, error)     { return "", "", nil }
func (stubHostInfoService) GetNonSystemUsers() ([]model.User, error) { return nil, nil }
func (stubHostInfoService) GetNonSystemGroups() ([]string, error)    { return nil, nil }
func (stubHostInfoService) GetUptime() (time.Duration, error)        { return 0, nil }

func TestCollectOSInfo_UsesCommander(t *testing.T) {
	mockCmd := interfaces.NewMockCommander()
	hostMgr := application.NewHostInfoManager(stubHostInfoService{})
	sd := &SystemDetails{commander: mockCmd}
	if err := sd.collectOSInfo(hostMgr); err != nil {
		t.Fatalf("err: %v", err)
	}
	found := false
	for _, cmd := range mockCmd.ExecutedCommands {
		if cmd == "uname -r" {
			found = true
		}
	}
	if !found {
		t.Errorf("uname not executed: %v", mockCmd.ExecutedCommands)
	}
}
