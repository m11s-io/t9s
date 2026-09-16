package domain

type EtcdMemberSnapshot struct {
	Hostname         string
	MemberID         uint64
	IsLearner        bool
	IsLeader         bool
	ClientURLs       []string
	PeerURLs         []string
	DBSize           int64
	DBSizeInUse      int64
	RaftIndex        uint64
	RaftTerm         uint64
	RaftAppliedIndex uint64
	StorageVersion   string
	Errors           []string
	Alarms           []string
	StatusKnown      bool
}

type EtcdSet struct {
	Members []EtcdMemberSnapshot
}

// EtcdSnapshotResult describes a completed local etcd backup. It carries no
// talosconfig, certificate, or token material: only the node it came from,
// the local path, its size, and the hex sha256 trailer.
type EtcdSnapshotResult struct {
	Node   string
	Path   string
	Size   int64
	SHA256 string
}
