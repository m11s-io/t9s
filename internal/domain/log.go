package domain

type LogRequest struct {
	Node    string
	Service string
}

type LogBatch struct {
	Lines []string
	EOF   bool
	Err   string
}
