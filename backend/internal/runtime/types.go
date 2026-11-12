package runtime

import (
	"time"
)

// ContainerSpec defines parameters needed to instantiate a sandbox or sidecar container.
type ContainerSpec struct {
	Name            string
	Image           string
	Command         []string
	Env             []string
	Labels          map[string]string
	NetworkName     string
	Aliases         []string
	NetworkDisabled bool
	Init            bool
	ReadonlyRootfs  bool
	Runtime         string
	CPUs            float64
	MemoryBytes     int64
	MemorySwapBytes int64
	PidsLimit       int64
	Mounts          []Mount
	Tmpfs           map[string]string
	CapDrop         []string
	CapAdd          []string
	NoNewPrivileges bool
}

// Mount specifies a volume or bind mount point.
type Mount struct {
	Type     string // "volume" or "bind"
	Source   string
	Target   string
	ReadOnly bool
}

// ContainerState represents the execution lifecycle phase of a container.
type ContainerState string

const (
	StateRunning  ContainerState = "running"
	StateStopped  ContainerState = "stopped"
	StateNotFound ContainerState = "not_found"
)

// ContainerInfo is the platform-owned representation of a container runtime instance.
type ContainerInfo struct {
	ID        string
	Name      string
	Image     string
	State     ContainerState
	IPAddress string
	Labels    map[string]string
	StartedAt time.Time
}

// LogOptions specifies parameters for reading stdout/stderr streams.
type LogOptions struct {
	Tail       string
	Follow     bool
	Timestamps bool
}
