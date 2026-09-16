package domain

type MountSnapshot struct {
	Filesystem     string
	MountedOn      string
	SizeBytes      uint64
	UsedBytes      uint64
	AvailableBytes uint64
	UsedPercent    float64
}

type MountSet struct {
	Mounts []MountSnapshot
}
