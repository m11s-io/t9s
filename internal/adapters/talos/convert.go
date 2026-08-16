package talos

import (
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

type rawService struct {
	Healthy *bool
}

type rawNode struct {
	ID              string
	Hostname        string
	Addresses       []string
	MachineType     string
	OperatingSystem string
	Stage           string
	Ready           *bool
	Services        []rawService
	ServicesKnown   bool
	Version         string
	ObservedAt      time.Time
	Problem         string
}

func convertNode(raw rawNode) domain.NodeSnapshot {
	health := domain.HealthUnknown
	if raw.Ready != nil {
		if *raw.Ready {
			health = domain.HealthHealthy
		} else {
			health = domain.HealthUnhealthy
		}
	}

	services := domain.ServiceSummary{Total: len(raw.Services), Known: raw.ServicesKnown}
	for _, service := range raw.Services {
		if service.Healthy == nil {
			services.Unknown++
			continue
		}
		if *service.Healthy {
			services.Healthy++
		}
	}

	return domain.NodeSnapshot{
		ID:         raw.ID,
		Name:       raw.Hostname,
		Addresses:  append([]string(nil), raw.Addresses...),
		Role:       convertRole(raw.MachineType),
		Stage:      raw.Stage,
		Health:     health,
		Services:   services,
		Kubernetes: domain.KubernetesUnknown,
		Version:    raw.Version,
		ObservedAt: raw.ObservedAt,
		Problem:    raw.Problem,
	}
}

func convertRole(machineType string) domain.NodeRole {
	switch machineType {
	case "controlplane":
		return domain.NodeRoleControl
	case "worker":
		return domain.NodeRoleWorker
	default:
		return domain.NodeRoleUnknown
	}
}
