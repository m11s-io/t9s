package domain

import "time"

type EventSnapshot struct {
	Node       string
	Kind       string
	Message    string
	ObservedAt time.Time
}

type EventSet struct {
	Events   []EventSnapshot
	Problems []EventProblem
}

// EventProblem identifies a node whose live event stream ended or failed.
type EventProblem struct {
	Node    string
	Message string
}
