package domain

import (
	"fmt"
	"time"
)

type NodeRole string

const (
	NodeRoleUnknown NodeRole = "unknown"
	NodeRoleControl NodeRole = "control"
	NodeRoleWorker  NodeRole = "worker"
)

type Health string

const (
	HealthUnknown   Health = "unknown"
	HealthHealthy   Health = "healthy"
	HealthUnhealthy Health = "unhealthy"
)

type KubernetesState string

const (
	KubernetesUnknown  KubernetesState = "Unknown"
	KubernetesReady    KubernetesState = "Ready"
	KubernetesNotReady KubernetesState = "NotReady"
)

type KubernetesCondition struct {
	Type   string
	Status string
	Reason string
}

type KubernetesNodeSnapshot struct {
	Roles          []string
	KubeletVersion string
	Conditions     []KubernetesCondition
}

type ServiceSummary struct {
	Healthy int
	Unknown int
	Total   int
	Known   bool
}

func (s ServiceSummary) String() string {
	if !s.Known && s.Total == 0 {
		return "?"
	}

	return fmt.Sprintf("%d healthy · %d unhealthy · %d unknown", s.Healthy, s.Total-s.Healthy-s.Unknown, s.Unknown)
}

func (s ServiceSummary) CompactString() string {
	if !s.Known && s.Total == 0 {
		return "?"
	}

	return fmt.Sprintf("%d✓ %d! %d?", s.Healthy, s.Total-s.Healthy-s.Unknown, s.Unknown)
}

type NodeSnapshot struct {
	ID             string
	Name           string
	Addresses      []string
	Role           NodeRole
	Stage          string
	Health         Health
	Services       ServiceSummary
	Kubernetes     KubernetesState
	KubernetesNode *KubernetesNodeSnapshot
	Version        string
	ObservedAt     time.Time
	Problem        string
}

func (n NodeSnapshot) Target() string {
	if n.Name != "" {
		return n.Name
	}
	if len(n.Addresses) > 0 {
		return n.Addresses[0]
	}
	return ""
}

func (n NodeSnapshot) DisplayName() string {
	if n.Name != "" {
		return n.Name
	}
	if len(n.Addresses) > 0 {
		return n.Addresses[0]
	}
	if n.ID != "" {
		return n.ID
	}

	return "?"
}

type NodeSet struct {
	Nodes      []NodeSnapshot
	ObservedAt time.Time
}
