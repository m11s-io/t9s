package domain

type DiskSnapshot struct {
	DeviceName string
	Model      string
	Serial     string
	Type       string
	SizeBytes  uint64
	BusPath    string
	SystemDisk bool
	ReadOnly   bool
}

type DiskSet struct {
	Disks []DiskSnapshot
}
