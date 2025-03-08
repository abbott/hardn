// pkg/domain/model/system.go
package model

// OSInfo represents operating system information
type OSInfo struct {
	Type      string // alpine, debian, ubuntu, etc.
	Version   string // version number
	Codename  string // release name
	IsProxmox bool   // whether this is a Proxmox installation
}
