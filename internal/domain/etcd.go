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
	StatusKnown      bool
}

type EtcdSet struct {
	Members []EtcdMemberSnapshot
}
