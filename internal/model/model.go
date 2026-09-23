package model

import "time"

// Host describes a Docker endpoint the TUI is connected to.
type Host struct {
	Name    string
	Address string
	Context string
}

// ContainerRates holds computed resource rates from consecutive stats samples.
type ContainerRates struct {
	CPUPercent  float64
	MemUsage    uint64
	MemLimit    uint64
	NetRxBps    float64
	NetTxBps    float64
	BlkReadBps  float64
	BlkWriteBps float64
	SampledAt   time.Time
}

// Container is an immutable view of a container for the UI.
type Container struct {
	ID       string
	Name     string
	Image    string
	State    string
	Status   string
	Labels   map[string]string
	Host     string
	Created  time.Time
	Rates    ContainerRates
	HasRates bool
}

// ShortID returns the first 12 characters of the container ID.
func (c Container) ShortID() string {
	if len(c.ID) <= 12 {
		return c.ID
	}
	return c.ID[:12]
}

// Network is an immutable network summary.
type Network struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Containers int
	Host       string
}

// Volume is an immutable volume summary.
type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	Host       string
}

// Image is an immutable image summary.
type Image struct {
	ID      string
	Tags    []string
	Size    int64
	Created time.Time
	Host    string
}

// ShortID returns a truncated image ID.
func (img Image) ShortID() string {
	id := img.ID
	if len(id) > 7 && id[:7] == "sha256:" {
		id = id[7:]
	}
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// Viewport describes which containers should be sampled eagerly.
// Prefer IDs when the UI applies filters/sorts; Start/End are fallbacks.
type Viewport struct {
	Start int
	End   int // exclusive
	IDs   []string
}

// Snapshot is an immutable point-in-time view published by the collector.
type Snapshot struct {
	Host       Host
	Containers []Container
	Networks   []Network
	Volumes    []Volume
	Images     []Image
	Sampled    int
	Total      int
	UpdatedAt  time.Time
	Err        error
}

// ActionKind identifies a lifecycle operation.
type ActionKind int

const (
	ActionStart ActionKind = iota + 1
	ActionStop
	ActionRestart
	ActionRemove
	ActionRemoveNetwork
	ActionRemoveVolume
	ActionRemoveImage
)

func (k ActionKind) String() string {
	switch k {
	case ActionStart:
		return "start"
	case ActionStop:
		return "stop"
	case ActionRestart:
		return "restart"
	case ActionRemove:
		return "remove"
	case ActionRemoveNetwork:
		return "remove network"
	case ActionRemoveVolume:
		return "remove volume"
	case ActionRemoveImage:
		return "remove image"
	default:
		return "unknown"
	}
}

// ActionRequest is a user-initiated lifecycle operation.
type ActionRequest struct {
	Kind  ActionKind
	ID    string
	Name  string
	Force bool
}

// ActionResult reports completion of an ActionRequest.
type ActionResult struct {
	Request ActionRequest
	Err     error
}
