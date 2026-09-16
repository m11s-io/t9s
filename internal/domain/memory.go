package domain

type MemorySnapshot struct {
	TotalBytes     uint64
	FreeBytes      uint64
	AvailableBytes uint64
	UsedBytes      uint64
	BuffersBytes   uint64
	CachedBytes    uint64
	SwapTotalBytes uint64
	SwapFreeBytes  uint64
	DirtyBytes     uint64
	SlabBytes      uint64
}
