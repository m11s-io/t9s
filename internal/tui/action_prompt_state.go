package tui

import (
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

// Prompt records are view-local display data for confirmation footers.
type actionPrompt struct {
	Kind                    application.ActionKind
	Targets                 []string
	Warning, Blocked, Image string
	Reset                   *ports.ResetOptions
	DeviceWipe              *ports.DeviceWipeOptions
}

type etcdActionPrompt struct {
	Kind                               application.EtcdActionKind
	MemberHostname, Node, SnapshotPath string
	Warning, Blocked                   string
}

type serviceActionPrompt struct {
	Kind             application.ServiceActionKind
	Node, Service    string
	Warning, Blocked string
}

type actionDisplayResult struct {
	Target, Err, Warning string
	Outcome              domain.OperationOutcome
}
