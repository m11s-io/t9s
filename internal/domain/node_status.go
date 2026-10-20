package domain

import "time"

// NodeStatusSet contains the node-scoped Talos status resources used by the
// curated networking, storage, and system views. Each Known field is tracked
// independently because older or restricted nodes may expose only a subset.
type NodeStatusSet struct {
	Statuses []NodeStatusSnapshot
	Problems []NodeStatusProblem
}

type NodeStatusProblem struct {
	Node    string
	Message string
}

type NodeStatusSnapshot struct {
	Node string

	BGPKnown bool
	BGPPeers []BGPPeerSnapshot

	LVMPhysicalVolumesKnown bool
	LVMPhysicalVolumes      []LVMPhysicalVolumeSnapshot
	LVMVolumeGroupsKnown    bool
	LVMVolumeGroups         []LVMVolumeGroupSnapshot
	LVMLogicalVolumesKnown  bool
	LVMLogicalVolumes       []LVMLogicalVolumeSnapshot

	MDKnown  bool
	MDArrays []MDArraySnapshot

	VolumesKnown  bool
	Volumes       []VolumeSnapshot
	TrimKnown     bool
	TrimSchedules []TrimScheduleSnapshot
	ScrubKnown    bool
	FSScrubs      []FSScrubSnapshot

	SystemDiskKnown bool
	SystemDiskID    string
	SystemDiskPath  string

	WorkloadIsolationKnown   bool
	WorkloadIsolationEnabled bool
}

type BGPPeerSnapshot struct {
	ID         string
	Instance   string
	Peer       string
	LocalASN   uint32
	PeerASN    uint32
	State      string
	Since      time.Time
	Received   uint32
	Advertised uint32
	Accepted   uint32
	BFDState   string
}

type LVMPhysicalVolumeSnapshot struct {
	Device     string
	VGName     string
	Missing    string
	PrettySize string
	PrettyFree string
}

type LVMVolumeGroupSnapshot struct {
	Name           string
	Partial        string
	MissingPVCount string
	PrettySize     string
	PrettyFree     string
}

type LVMLogicalVolumeSnapshot struct {
	Name   string
	Path   string
	VGName string
	Layout string
	Active string
	Size   string
}

type MDArraySnapshot struct {
	ID          string
	Name        string
	Device      string
	Level       string
	Status      string
	SyncAction  string
	ArrayState  string
	RaidDevices int
	Members     []string
	Error       string
}

type VolumeSnapshot struct {
	ID             string
	System         bool
	Phase          string
	Type           string
	Location       string
	ParentLocation string
	MountLocation  string
	Filesystem     string
	SizeBytes      uint64
	Error          string
	TrimEnabled    bool
	TrimInterval   time.Duration
	ScrubEnabled   bool
	ScrubInterval  time.Duration
}

type TrimScheduleSnapshot struct {
	ID         string
	Filesystem string
	Interval   time.Duration
	NextTrim   time.Time
}

type FSScrubSnapshot struct {
	ID         string
	Mountpoint string
	Interval   time.Duration
	Time       time.Time
	Duration   time.Duration
	Status     string
}
