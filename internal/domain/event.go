package domain

import "time"

type EventSnapshot struct {
	Node       string
	Kind       string
	Message    string
	ObservedAt time.Time
}

type EventSet struct {
	Events []EventSnapshot
}
