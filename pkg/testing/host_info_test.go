// pkg/testing/host_info_test.go
package testing

import (
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/stretchr/testify/assert"
)

// TestHostInfoService tests the host info service
func TestHostInfoService(t *testing.T) {
	// Create mock provider
	mockProvider := interfaces.NewProvider()

	// Set up mock file system with test data
	mockFS := &interfaces.MockFileSystem{
		Files: map[string][]byte{
			"/etc/os-release": []byte(`NAME="Test Linux"
VERSION="1.0"
ID=testlinux
VERSION_ID=1.0
PRETTY_NAME="Test Linux 1.0"
`),
			"/etc/hostname": []byte("testhost"),
			"/proc/uptime":  []byte("12345.67 23456.78"),
			"/etc/passwd": []byte(`root:x:0:0:root:/root:/bin/bash
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
testuser:x:1000:1000:Test User,,,:/home/testuser:/bin/bash
`),
			"/etc/group": []byte(`root:x:0:
sudo:x:27:testuser
testgroup:x:1001:testuser
`),
			"/etc/resolv.conf": []byte(`nameserver 1.1.1.1
nameserver 8.8.8.8
`),
		},
		Directories: map[string]bool{
			"/etc":                    true,
			"/proc":                   true,
			"/etc/sudoers.d":          true,
			"/etc/sudoers.d/testuser": true,
		},
	}
	mockProvider.FS = mockFS

	// Set up mock commander
	mockCommander := &interfaces.MockCommander{
		CommandOutputs: map[string][]byte{
			"uname -r": []byte("5.10.0-testkernel"),
			"hostname": []byte("testhost"),
			"df -k": []byte(`Filesystem     1K-blocks    Used Available Use% Mounted on
/dev/sda1       41251136 6291456  34959680  16% /
tmpfs            8198468       0   8198468   0% /dev/shm
/dev/sdb1      103885812 5242880  98642932   6% /data
`),
			"cat /proc/meminfo": []byte(`MemTotal:       16396936 kB
MemFree:         8198468 kB
MemAvailable:   10768516 kB
`),
		},
	}
	mockProvider.Commander = mockCommander

	// Mock network operations
	mockNetwork := &interfaces.MockNetworkOperations{
		Interfaces: []string{"eth0", "lo"},
		Subnets:    map[string]bool{"192.168.1": true},
	}
	mockProvider.Network = mockNetwork

	// Create host info repository
	hostInfoRepo := secondary.NewOSHostInfoRepository(mockProvider.FS, mockProvider.Commander, "testlinux")

	// Create host info service
	hostInfoService := service.NewHostInfoServiceImpl(hostInfoRepo, model.OSInfo{
		Type:      "testlinux",
		Version:   "1.0",
		Codename:  "test",
		IsProxmox: false,
	})

	// Create host info manager
	hostInfoManager := application.NewHostInfoManager(hostInfoService)

	// Test GetHostInfo
	hostInfo, err := hostInfoManager.GetHostInfo()
	assert.NoError(t, err)
	assert.NotNil(t, hostInfo)

	// Check basic info
	assert.Equal(t, "testhost", hostInfo.Hostname)
	assert.Equal(t, "Test Linux", hostInfo.OSName)
	assert.Equal(t, "1.0", hostInfo.OSVersion)
	assert.Equal(t, "5.10.0-testkernel", hostInfo.KernelInfo)
	assert.Equal(t, time.Duration(12345.67*float64(time.Second)), hostInfo.Uptime)

	// Test GetNonSystemUsers
	users, err := hostInfoManager.GetNonSystemUsers()
	assert.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "testuser", users[0].Username)
	assert.True(t, users[0].HasSudo)

	// Test GetDNSServers
	dnsServers, err := hostInfoManager.GetDNSServers()
	assert.NoError(t, err)
	assert.Len(t, dnsServers, 2)
	assert.Equal(t, "1.1.1.1", dnsServers[0])
	assert.Equal(t, "8.8.8.8", dnsServers[1])

	// Test GetHostname
	hostname, _, err := hostInfoManager.GetHostname()
	// blank identifier (_) for the domain variable since it's not used
	assert.NoError(t, err)
	assert.Equal(t, "testhost", hostname)

	// Test formatting functions
	assert.Equal(t, "5 hours, 25 minutes", hostInfoManager.FormatUptime(5*time.Hour+25*time.Minute))
	assert.Equal(t, "2 days, 3 hours, 45 minutes", hostInfoManager.FormatUptime(51*time.Hour+45*time.Minute))

	assert.Equal(t, "1.5 KiB", hostInfoManager.FormatBytes(1536))
	assert.Equal(t, "1.0 MiB", hostInfoManager.FormatBytes(1048576))
	assert.Equal(t, "1.5 GiB", hostInfoManager.FormatBytes(1610612736))
}
