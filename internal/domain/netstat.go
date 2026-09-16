package domain

type SocketSnapshot struct {
	Protocol      string
	State         string
	LocalAddress  string
	RemoteAddress string
	UID           uint32
	Inode         uint64
	ProcessName   string
	PID           uint32
	Netns         string
}

type SocketSet struct {
	Sockets []SocketSnapshot
}
