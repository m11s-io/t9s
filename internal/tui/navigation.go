package tui

import (
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
)

type viewKind uint8

const (
	viewNodes viewKind = iota
	viewNodeDetail
	viewServices
	viewServiceDetail
	viewContexts
	viewHelp
	viewServiceLogs
	viewEvents
	viewEtcd
	viewProcesses
	viewProcessDetail
	viewDisks
	viewDiskDetail
	viewNetwork
	viewBGPPeers
	viewLinkDetail
	viewDmesg
	viewNetstat
	viewMounts
	viewMemory
	viewTime
	viewCPU
	viewDiskUsage
	viewContainers
	viewContainerDetail
	viewOverview
	viewProblems
	viewResourceKinds
	viewResourceInstances
	viewResourceDetail
	viewClusterHealth
)

type viewFrame struct {
	Kind                      viewKind
	Label                     string
	ServiceKey                string
	ServiceName               string
	NodeID                    string
	NodeTarget                string
	ProcessPID                int32
	DeviceName                string
	LinkName                  string
	ResourceKind              string
	ResourceID                string
	ResourceNamespace         string
	MountPath                 string
	ContainerNamespace        domain.ContainerNamespace
	ContainerRuntimeNamespace string
	ContainerID               string
	ImageReference            string
	ImageDigest               string
	ImageSizeBytes            uint64
	ImageMode                 bool
}

type viewStack []viewFrame

func newViewStack(root viewFrame) viewStack { return viewStack{root} }

func (s viewStack) top() viewFrame {
	if len(s) == 0 {
		return viewFrame{Kind: viewNodes, Label: "nodes"}
	}
	return s[len(s)-1]
}

func (s viewStack) contains(kind viewKind) bool {
	for _, frame := range s {
		if frame.Kind == kind {
			return true
		}
	}
	return false
}

func (s viewStack) replaceRoot(root viewFrame) viewStack { return viewStack{root} }

func (s viewStack) push(frame viewFrame) viewStack {
	result := append(viewStack(nil), s...)
	return append(result, frame)
}

func (s viewStack) pop() (viewStack, bool) {
	if len(s) <= 1 {
		return s, false
	}
	return append(viewStack(nil), s[:len(s)-1]...), true
}

func (s viewStack) breadcrumb(contextName string) string {
	labels := []string{fallback(contextName)}
	for _, frame := range s {
		if frame.Label != "" {
			labels = append(labels, frame.Label)
		}
	}
	return strings.Join(labels, " > ")
}
