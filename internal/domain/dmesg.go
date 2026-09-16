package domain

type DmesgRequest struct {
	Node   string
	Follow bool
	Tail   bool
}

type DmesgBatch struct {
	Lines []string
	EOF   bool
	Err   string
}
