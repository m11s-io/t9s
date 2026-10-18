package application

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type destructiveDiskFacts struct {
	Previews       []ResetPreview
	Blocked        string
	Scope          []nodeDiskIdentity
	ResetUserDisks []string
}

type nodeDiskIdentity struct {
	Node       string
	Identity   diskIdentity
	SystemDisk bool
	ReadOnly   bool
}

// readDestructiveDiskFacts captures the exact devices selected by a reset or
// device wipe. Partial inventory is retained for diagnostics, but is never
// marked known or accepted as authority for a destructive operation.
func (s *Session) readDestructiveDiskFacts(ctx context.Context, targets []domain.NodeSnapshot, request NodeActionRequest) (destructiveDiskFacts, error) {
	facts := destructiveDiskFacts{Previews: make([]ResetPreview, 0, len(targets))}
	if s.sdkSession == nil || s.sdkSession.Disks() == nil {
		for _, node := range targets {
			preview := ResetPreview{Node: node.Target(), Err: "disk inventory reader unavailable"}
			facts.Previews = append(facts.Previews, preview)
			facts.Blocked = appendActionBlock(facts.Blocked, fmt.Sprintf("refusing: disk inventory for %s is unavailable", node.Target()))
		}
		return facts, nil
	}
	reader := s.sdkSession.Disks()
	for _, node := range targets {
		target := node.Target()
		if err := ctx.Err(); err != nil {
			return facts, err
		}
		set, readErr := reader.List(ctx, target)
		if ctx.Err() != nil {
			return facts, errors.Join(ctx.Err(), readErr)
		}
		preview := BuildResetPreview(target, set)
		preview.Known = readErr == nil
		if readErr != nil {
			preview.Err = readErr.Error()
			facts.Blocked = appendActionBlock(facts.Blocked, fmt.Sprintf("refusing: disk inventory for %s is unavailable", target))
		}
		facts.Previews = append(facts.Previews, preview)
		if readErr != nil {
			continue
		}
		if request.Kind == ActionReset {
			blockedBefore := facts.Blocked
			facts.captureResetDiskScope(target, set.Disks, *request.Reset)
			if facts.Blocked == blockedBefore {
				facts.Previews[len(facts.Previews)-1].UserDisks = nil
				for _, scope := range facts.Scope {
					if scope.Node != target {
						continue
					}
					if scope.SystemDisk {
						facts.Previews[len(facts.Previews)-1].SystemDisk = scope.Identity.DeviceName
						continue
					}
					facts.Previews[len(facts.Previews)-1].UserDisks = append(facts.Previews[len(facts.Previews)-1].UserDisks, scope.Identity.DeviceName)
				}
			}
		} else {
			facts.captureDeviceWipeScope(target, set.Disks, *request.DeviceWipe)
		}
	}
	return facts, nil
}

func (f *destructiveDiskFacts) captureResetDiskScope(target string, disks []domain.DiskSnapshot, options ports.ResetOptions) {
	systems := make([]domain.DiskSnapshot, 0, 1)
	users := make([]domain.DiskSnapshot, 0, len(disks))
	for _, disk := range disks {
		if disk.SystemDisk {
			systems = append(systems, disk)
		} else if !disk.ReadOnly {
			users = append(users, disk)
		}
	}
	if len(systems) != 1 {
		f.Blocked = appendActionBlock(f.Blocked, fmt.Sprintf("refusing: system disk inventory for %s is unknown", target))
		return
	}
	if options.Mode != ports.WipeModeUserDisks && systems[0].ReadOnly {
		f.Blocked = appendActionBlock(f.Blocked, fmt.Sprintf("refusing: %s is read-only", systems[0].DeviceName))
		return
	}

	var selected []domain.DiskSnapshot
	if options.Mode == ports.WipeModeSystemDisk {
		selected = nil
	} else if len(options.UserDisks) == 0 {
		selected = users
	} else {
		seen := make(map[string]struct{}, len(options.UserDisks))
		for _, device := range options.UserDisks {
			if device == "" {
				f.Blocked = appendActionBlock(f.Blocked, "refusing: reset contains an empty user disk")
				return
			}
			if _, exists := seen[device]; exists {
				f.Blocked = appendActionBlock(f.Blocked, fmt.Sprintf("refusing: duplicate reset disk %s", device))
				return
			}
			seen[device] = struct{}{}
			matches := 0
			for _, disk := range users {
				if disk.DeviceName == device {
					selected = append(selected, disk)
					matches++
				}
			}
			if matches != 1 {
				f.Blocked = appendActionBlock(f.Blocked, fmt.Sprintf("refusing: reset disk %s is not a unique writable user disk on %s", device, target))
				return
			}
		}
	}

	if options.Mode == ports.WipeModeUserDisks && len(selected) == 0 {
		f.Blocked = appendActionBlock(f.Blocked, "refusing: no user disks discovered")
		return
	}
	if options.Mode != ports.WipeModeUserDisks {
		f.Scope = append(f.Scope, nodeDiskIdentity{Node: target, Identity: diskIdentityOf(systems[0]), SystemDisk: systems[0].SystemDisk, ReadOnly: systems[0].ReadOnly})
	}
	for _, disk := range selected {
		f.ResetUserDisks = append(f.ResetUserDisks, disk.DeviceName)
		f.Scope = append(f.Scope, nodeDiskIdentity{Node: target, Identity: diskIdentityOf(disk), SystemDisk: disk.SystemDisk, ReadOnly: disk.ReadOnly})
	}
}

func (f *destructiveDiskFacts) captureDeviceWipeScope(target string, disks []domain.DiskSnapshot, options ports.DeviceWipeOptions) {
	requested := diskIdentity{DeviceName: options.Device}
	if reason := assessDeviceWipeFacts(DiskPolicyFacts{Available: true, Node: target, Disks: disks}, target, requested); reason != "" {
		f.Blocked = appendActionBlock(f.Blocked, reason)
		return
	}
	var match *domain.DiskSnapshot
	for i := range disks {
		if !diskIdentityMatches(disks[i], requested) {
			continue
		}
		if match != nil {
			f.Blocked = appendActionBlock(f.Blocked, fmt.Sprintf("refusing: %s is not a unique inventory device", options.Device))
			return
		}
		match = &disks[i]
	}
	if match == nil {
		f.Blocked = appendActionBlock(f.Blocked, fmt.Sprintf("refusing: %s is not in the current inventory", options.Device))
		return
	}
	f.Scope = append(f.Scope, nodeDiskIdentity{Node: target, Identity: diskIdentityOf(*match), SystemDisk: match.SystemDisk, ReadOnly: match.ReadOnly})
}

func sameDestructiveDiskFacts(prepared, current *destructiveDiskFacts) bool {
	if prepared == nil || current == nil {
		return prepared == current
	}
	return slices.Equal(prepared.Scope, current.Scope) && slices.Equal(prepared.ResetUserDisks, current.ResetUserDisks)
}

func cloneResetPreviews(previews []ResetPreview) []ResetPreview {
	cloned := append([]ResetPreview(nil), previews...)
	for i := range cloned {
		cloned[i].UserDisks = append([]string(nil), cloned[i].UserDisks...)
	}
	return cloned
}

func appendActionBlock(existing, next string) string {
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

func dispatchPreparedNodeAction(ctx context.Context, controller ports.NodeController, request NodeActionRequest, diskFacts *destructiveDiskFacts, target string) TargetResult {
	if request.Kind != ActionReset && request.Kind != ActionWipeDevice {
		return dispatchNodeAction(ctx, controller, request.Kind, target)
	}
	result := TargetResult{Target: target}
	if err := ctx.Err(); err != nil {
		result.Err = err
		return result
	}
	var err error
	switch request.Kind {
	case ActionReset:
		options := *request.Reset
		options.SystemPartitions = append([]string(nil), request.Reset.SystemPartitions...)
		options.UserDisks = append([]string(nil), diskFacts.ResetUserDisks...)
		err = controller.Reset(ctx, target, options)
	case ActionWipeDevice:
		err = controller.WipeDevice(ctx, target, request.DeviceWipe.Device, request.DeviceWipe.Method)
	}
	if err == nil {
		result.Outcome = domain.OperationAccepted
		return result
	}
	if ctx.Err() != nil {
		err = mutationCallError(ctx, err)
	}
	result.Outcome = domain.OperationUnconfirmed
	result.Err = err
	return result
}
