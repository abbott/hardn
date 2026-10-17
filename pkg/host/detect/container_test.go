package detect_test

import (
	"context"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

const (
	initPIDNS  = "pid:[4026531836]" // 0xEFFFFFFC
	childPIDNS = "pid:[4026532201]"
	hostEnv    = "HOME=/\x00TERM=linux\x00"
)

// bareHost is a host with no container signal: PID 1's environment without
// container=, and the initial PID namespace.
func bareHost(t *testing.T) *hostfake.System {
	t.Helper()
	f := newFake(t, "")
	f.AddFile(detect.InitEnviron, []byte(hostEnv), 0o400, 0, 0)
	f.AddSymlink(detect.SelfPIDNamespace, initPIDNS)
	return f
}

func TestDetectContainer(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		sys      func() host.System
		detected detect.Tri
		signal   string
		manager  string
		wantErr  error
	}{
		{"host", func() host.System { return bareHost(t) }, detect.No, "", "", nil},
		{"container manager file", func() host.System {
			f := bareHost(t)
			f.AddFile(detect.ContainerManagerFile, []byte("podman\n"), 0o444, 0, 0)
			return f
		}, detect.Yes, detect.ContainerManagerFile, "podman", nil},
		{"systemd in a container", func() host.System {
			f := bareHost(t)
			f.AddFile(detect.SystemdContainerFile, []byte("systemd-nspawn\n"), 0o444, 0, 0)
			return f
		}, detect.Yes, detect.SystemdContainerFile, "systemd-nspawn", nil},
		{"Proxmox CT (LXC) without systemd", func() host.System {
			f := bareHost(t)
			f.AddFile(detect.InitEnviron, []byte("container=lxc\x00HOME=/\x00"), 0o400, 0, 0)
			f.AddSymlink(detect.SelfPIDNamespace, childPIDNS)
			return f
		}, detect.Yes, detect.InitEnviron, "lxc", nil},
		{"podman file", func() host.System {
			f := bareHost(t)
			f.AddFile(detect.PodmanEnvFile, nil, 0o444, 0, 0)
			return f
		}, detect.Yes, detect.PodmanEnvFile, "podman", nil},
		{"docker file", func() host.System {
			f := bareHost(t)
			f.AddFile(detect.DockerEnvFile, nil, 0o755, 0, 0)
			return f
		}, detect.Yes, detect.DockerEnvFile, "docker", nil},
		{"PID namespace only", func() host.System {
			f := bareHost(t)
			f.AddSymlink(detect.SelfPIDNamespace, childPIDNS)
			return f
		}, detect.Yes, detect.SelfPIDNamespace, "", nil},
		{"earlier signal wins", func() host.System {
			f := bareHost(t)
			f.AddFile(detect.SystemdContainerFile, []byte("lxc\n"), 0o444, 0, 0)
			f.AddFile(detect.DockerEnvFile, nil, 0o755, 0, 0)
			return f
		}, detect.Yes, detect.SystemdContainerFile, "lxc", nil},
		{"empty manager file is no signal", func() host.System {
			f := bareHost(t)
			f.AddFile(detect.ContainerManagerFile, []byte("\n"), 0o444, 0, 0)
			return f
		}, detect.No, "", "", nil},
		{"positive despite an unreadable environ", func() host.System {
			f := bareHost(t)
			f.AddSymlink(detect.SelfPIDNamespace, childPIDNS)
			return deny(f, detect.InitEnviron)
		}, detect.Yes, detect.SelfPIDNamespace, "", nil},
		{"unreadable environ, nothing else: not established", func() host.System {
			return deny(bareHost(t), detect.InitEnviron)
		}, detect.Unknown, "", "", fs.ErrPermission},
		{"unparsable namespace link", func() host.System {
			f := bareHost(t)
			f.AddSymlink(detect.SelfPIDNamespace, "pid:4026531836")
			return f
		}, detect.Unknown, "", "", detect.ErrUnrecognised},
	}
	for _, tt := range tests {
		got := detect.DetectContainer(ctx, tt.sys())
		if got.Detected != tt.detected || got.Signal != tt.signal || got.Manager != tt.manager || !matches(got.Err, tt.wantErr) {
			t.Errorf("%s: DetectContainer = %+v; want %s %q %q err %v", tt.name, got, tt.detected, tt.signal, tt.manager, tt.wantErr)
		}
	}
}
