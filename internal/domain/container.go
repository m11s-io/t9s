package domain

import "time"

type ContainerNamespace string

const (
	ContainerNamespaceSystem          ContainerNamespace = "system"
	ContainerNamespaceCRI             ContainerNamespace = "cri"
	ContainerNamespaceTalosContainers ContainerNamespace = "taloscontainers"
)

type ContainerRequest struct {
	Node      string
	Namespace ContainerNamespace
}

type ContainerSnapshot struct {
	Namespace    string
	ID           string
	Name         string
	PodID        string
	Image        string
	PID          uint32
	Status       string
	MemoryBytes  uint64
	MemoryKnown  bool
	CPUTime      time.Duration
	CPUTimeKnown bool
}

type ContainerSet struct {
	Node       string
	Namespace  ContainerNamespace
	Containers []ContainerSnapshot
	Problems   []string
}

type ImageRequest struct {
	Node      string
	Namespace ContainerNamespace
}

type ImageSnapshot struct {
	Reference string
	Digest    string
	SizeBytes uint64
}

type ImageBatch struct {
	Images    []ImageSnapshot
	EOF       bool
	Truncated bool
	Err       string
}
