package domain

import "time"

type ServiceSnapshot struct {
	Node        string
	Name        string
	State       string
	Healthy     *bool
	LastMessage string
	LastChange  time.Time
}

type ServiceSet struct {
	Services   []ServiceSnapshot
	Problems   []ServiceProblem
	ObservedAt time.Time
}

type ServiceProblem struct {
	Node    string
	Message string
}
