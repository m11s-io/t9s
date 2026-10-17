package domain

import "time"

// TimeSample is a pair of sampled clocks. Difference is server minus node,
// not an authoritative NTP offset or a synchronization verdict.
type TimeSample struct {
	Server                          string
	LocalTime, RemoteTime           time.Time
	LocalTimeValid, RemoteTimeValid bool
	Difference                      time.Duration
	DifferenceValid                 bool
	Problems                        []string
}
type TimeSnapshot struct {
	TimeSample
	SyncKnown, Synced, SyncDisabled bool
}

// TimeCheckResult never replaces the node's normal synchronization status.
type TimeCheckResult struct{ TimeSample }
