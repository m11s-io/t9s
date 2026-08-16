package domain

type ProcessSnapshot struct {
	PID            int32
	PPID           int32
	State          string
	Threads        int32
	CPUTime        float64
	VirtualMemory  uint64
	ResidentMemory uint64
	Command        string
	Executable     string
	Args           string
	Label          string
}

type ProcessSet struct {
	Processes []ProcessSnapshot
}
