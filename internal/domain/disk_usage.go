package domain

type DiskUsageRequest struct {
	Node string
	Path string
}

type DiskUsageEntry struct {
	Path      string
	SizeBytes int64
	Error     string
}

type DiskUsageBatch struct {
	Rows      []DiskUsageEntry
	EOF       bool
	Truncated bool
	Err       string
}
