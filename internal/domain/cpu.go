package domain

import "time"

// CPUUtilization describes a counter delta, not lifetime CPU time. Unknown
// values must not be rendered as zero utilization.
type CPUUtilization struct {
	Known                      bool
	BusyPercent, IOWaitPercent float64
}

// CPUCoreSnapshot preserves the SDK's indexed core IDs, including sparse gaps.
type CPUCoreSnapshot struct {
	ID int
	CPUUtilization
}

// CPUSnapshot retains process counts from the latest successful observation
// even when a second observation cannot produce valid CPU deltas.
type CPUSnapshot struct {
	Aggregate                          CPUUtilization
	Cores                              []CPUCoreSnapshot
	SampledAt                          time.Time
	Interval                           time.Duration
	RunningProcesses, BlockedProcesses uint64
	ProcessCountsKnown                 bool
	Problems                           []string
}
