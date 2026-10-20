package tui

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type statusGroup uint8

const (
	statusGroupBGP statusGroup = iota
	statusGroupStorage
	statusGroupVolumes
)

func statusRollupLine(input application.HealthInput, diagnoses []domain.Diagnosis, label, primaryKind string, group statusGroup, otherKinds ...string) string {
	if !input.NodeStatusesKnown || len(input.NodeStatuses.Statuses) == 0 {
		return fmt.Sprintf("%-9s unknown\n", label)
	}
	total := 0
	known := true
	for _, node := range input.NodeStatuses.Statuses {
		switch group {
		case statusGroupBGP:
			known = known && node.BGPKnown
			total += len(node.BGPPeers)
		case statusGroupStorage:
			known = known && node.LVMPhysicalVolumesKnown && node.LVMVolumeGroupsKnown && node.LVMLogicalVolumesKnown && node.MDKnown
			total += len(node.LVMPhysicalVolumes) + len(node.LVMVolumeGroups) + len(node.LVMLogicalVolumes) + len(node.MDArrays)
		case statusGroupVolumes:
			known = known && node.VolumesKnown && node.TrimKnown && node.ScrubKnown
			total += len(node.Volumes) + len(node.TrimSchedules) + len(node.FSScrubs)
		}
	}
	if !known {
		return fmt.Sprintf("%-9s unknown on some nodes\n", label)
	}
	if total == 0 {
		return fmt.Sprintf("%-9s no configured resources\n", label)
	}
	kinds := append([]string{primaryKind}, otherKinds...)
	warning, critical, unknown := diagnosisCounts(diagnoses, kinds...)
	return fmt.Sprintf("%-9s %d resources, %d warning, %d critical, %d unknown\n", label, total, warning, critical, unknown)
}

func diagnosisCounts(diagnoses []domain.Diagnosis, kinds ...string) (warning, critical, unknown int) {
	wanted := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		wanted[kind] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, diagnosis := range diagnoses {
		if _, ok := wanted[diagnosis.ResourceKind]; !ok {
			continue
		}
		key := diagnosis.ResourceKind + ":" + diagnosis.ResourceID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		switch diagnosis.Severity {
		case domain.SeverityWarning:
			warning++
		case domain.SeverityCritical:
			critical++
		case domain.SeverityUnknown:
			unknown++
		}
	}
	return warning, critical, unknown
}

func systemStatusRollupLine(input application.HealthInput, diagnoses []domain.Diagnosis) string {
	if !input.NodeStatusesKnown || !input.ServicesKnown || len(input.NodeStatuses.Statuses) == 0 {
		return "SYSTEM    unknown\n"
	}
	enabled, disabled, isolationUnknown := 0, 0, 0
	for _, node := range input.NodeStatuses.Statuses {
		if !node.WorkloadIsolationKnown {
			isolationUnknown++
			continue
		}
		if node.WorkloadIsolationEnabled {
			enabled++
		} else {
			disabled++
		}
	}
	sandboxdByNode := make(map[string]domain.ServiceSnapshot)
	for _, service := range input.Services.Services {
		if service.Name != "sandboxd" {
			continue
		}
		sandboxdByNode[service.Node] = service
	}
	sandboxdServices, sandboxdUnknown := 0, 0
	for _, node := range input.NodeStatuses.Statuses {
		service, ok := sandboxdByNode[node.Node]
		if !ok {
			sandboxdUnknown++
			continue
		}
		sandboxdServices++
		if service.Healthy == nil {
			sandboxdUnknown++
		}
	}
	_, sandboxdCritical, diagnosisUnknown := diagnosisCounts(diagnoses, "sandboxd")
	sandboxdUnknown += diagnosisUnknown
	return fmt.Sprintf("SYSTEM    isolation %d enabled, %d disabled, %d unknown; sandboxd %d services, %d unhealthy, %d unknown\n",
		enabled, disabled, isolationUnknown, sandboxdServices, sandboxdCritical, sandboxdUnknown)
}

func renderBGPPeers(state directNodeStatusState) string {
	if state.Status == directLoading || state.Status == "" {
		return "BGP peer status loading"
	}
	var view strings.Builder
	if state.Status == directFailed {
		return "BGP peer status unavailable: " + fallback(state.Err)
	}
	view.WriteString("NODE               PEER                         PEER AS  STATE          BFD        RX       TX\n")
	count := 0
	unknownNodes := 0
	for _, node := range state.Value.Statuses {
		if !node.BGPKnown {
			unknownNodes++
			continue
		}
		for _, peer := range node.BGPPeers {
			view.WriteString(fmt.Sprintf("%-18s %-28s %-7d %-14s %-10s %-8d %-8d\n",
				sanitizeUntrustedText(node.Node), sanitizeUntrustedText(peer.Peer), peer.PeerASN,
				sanitizeUntrustedText(peer.State), fallback(sanitizeUntrustedText(peer.BFDState)), peer.Received, peer.Advertised))
			count++
		}
	}
	if count == 0 {
		view.WriteString("No BGP peer status resources found\n")
	}
	if unknownNodes > 0 {
		view.WriteString(fmt.Sprintf("\nBGP status unknown on %d node(s)\n", unknownNodes))
	}
	return strings.TrimRight(view.String(), "\n")
}

func countBGPPeers(values domain.NodeStatusSet) int {
	count := 0
	for _, status := range values.Statuses {
		count += len(status.BGPPeers)
	}
	return count
}

func findNodeStatus(values domain.NodeStatusSet, target string) (domain.NodeStatusSnapshot, bool) {
	for _, status := range values.Statuses {
		if status.Node == target {
			return status, true
		}
	}
	return domain.NodeStatusSnapshot{}, false
}

func sandboxdServiceState(services domain.ServiceSet, node domain.NodeSnapshot) string {
	for _, service := range services.Services {
		if service.Name != "sandboxd" || !serviceBelongsToNode(service.Node, node) {
			continue
		}
		state := fallback(service.State)
		if service.Healthy == nil {
			return state + " (health unknown)"
		}
		if *service.Healthy {
			return state + " (healthy)"
		}
		return state + " (unhealthy)"
	}
	return "unknown"
}

func serviceBelongsToNode(target string, node domain.NodeSnapshot) bool {
	if target == node.Target() || target == node.Name || target == node.ID {
		return true
	}
	for _, address := range node.Addresses {
		if target == address {
			return true
		}
	}
	return false
}

func renderNodeDetailWithStatus(node domain.NodeSnapshot, status domain.NodeStatusSnapshot, statusKnown bool, sandboxd string) string {
	fields := []detailField{
		{label: "NAME", value: node.DisplayName()},
		{label: "ID", value: fallback(node.ID)},
		{label: "ROLE", value: nodeRoleDisplayValue(node)},
		{label: "STAGE", value: fallback(node.Stage)},
		{label: "HEALTH", value: string(node.Health)},
		{label: "SERVICES", value: serviceSummary(node.Services)},
		{label: "KUBERNETES", value: string(node.Kubernetes)},
	}
	if node.KubernetesNode != nil {
		fields = append(fields,
			detailField{label: "K8S ROLES", value: fallback(strings.Join(node.KubernetesNode.Roles, ", "))},
			detailField{label: "KUBELET", value: fallback(node.KubernetesNode.KubeletVersion)},
		)
		for _, condition := range node.KubernetesNode.Conditions {
			fields = append(fields, detailField{label: "CONDITION", value: condition.Type + "=" + condition.Status})
		}
	}
	fields = append(fields, detailField{label: "VERSION", value: fallback(node.Version)})
	if len(node.Addresses) > 0 {
		fields = append(fields, detailField{label: "ADDRESSES", value: strings.Join(node.Addresses, ", ")})
	}
	fields = append(fields, detailField{label: "SANDBOXD SERVICE", value: fallback(sandboxd)})
	if statusKnown {
		isolation := "unknown"
		if status.WorkloadIsolationKnown {
			isolation = "disabled"
			if status.WorkloadIsolationEnabled {
				isolation = "enabled"
			}
		}
		fields = append(fields,
			detailField{label: "WORKLOAD ISOLATION", value: isolation},
		)
		if status.SystemDiskKnown {
			fields = append(fields, detailField{label: "SYSTEM DISK", value: firstNonEmpty(status.SystemDiskPath, status.SystemDiskID)})
		} else {
			fields = append(fields, detailField{label: "SYSTEM DISK", value: "unknown"})
		}
		if !status.VolumesKnown {
			fields = append(fields, detailField{label: "SYSTEM VOLUMES", value: "unknown"})
		} else {
			found := false
			for _, volume := range status.Volumes {
				if !volume.System {
					continue
				}
				found = true
				fields = append(fields, detailField{label: "SYSTEM VOLUME", value: fmt.Sprintf("%s · %s · %s · %s · %s · %s", volume.ID, volume.Phase, fallback(volume.Location), fallback(volume.MountLocation), volumeFilesystemSize(volume), volumeOperationState(volume, status))})
			}
			if !found {
				fields = append(fields, detailField{label: "SYSTEM VOLUMES", value: "none reported"})
			}
		}
	} else {
		fields = append(fields,
			detailField{label: "WORKLOAD ISOLATION", value: "unknown"},
			detailField{label: "SYSTEM VOLUMES", value: "unknown"},
		)
	}
	if node.Problem != "" {
		fields = append(fields, detailField{label: "PROBLEM", value: node.Problem})
	}
	return renderDetail(fields)
}

func renderDiskDetailWithStatus(disk domain.DiskSnapshot, status domain.NodeStatusSnapshot, known bool) string {
	fields := []detailField{
		{label: "DEVICE", value: fallback(disk.DeviceName)},
		{label: "MODEL", value: fallback(disk.Model)},
		{label: "SERIAL", value: fallback(disk.Serial)},
		{label: "TYPE", value: fallback(disk.Type)},
		{label: "SIZE", value: formatBytes(int64(disk.SizeBytes))},
		{label: "BUS PATH", value: fallback(disk.BusPath)},
		{label: "SYSTEM DISK", value: fmt.Sprintf("%t", disk.SystemDisk)},
		{label: "READ ONLY", value: fmt.Sprintf("%t", disk.ReadOnly)},
	}
	if !known {
		fields = append(fields, detailField{label: "LVM / MD RAID", value: "unknown"}, detailField{label: "VOLUMES", value: "unknown"})
		return renderDetail(fields)
	}
	if !status.LVMPhysicalVolumesKnown || !status.LVMVolumeGroupsKnown || !status.LVMLogicalVolumesKnown {
		fields = append(fields, detailField{label: "LVM STATUS", value: "unknown"})
	} else {
		lvmFound := false
		for _, pv := range status.LVMPhysicalVolumes {
			if deviceMatches(pv.Device, disk.DeviceName) {
				lvmFound = true
				fields = append(fields, detailField{label: "LVM PHYSICAL VOLUME", value: fmt.Sprintf("%s · vg=%s · size=%s · free=%s · missing=%s", pv.Device, fallback(pv.VGName), fallback(pv.PrettySize), fallback(pv.PrettyFree), fallback(pv.Missing))})
			}
		}
		for _, vg := range status.LVMVolumeGroups {
			fields = append(fields, detailField{label: "LVM VOLUME GROUP", value: fmt.Sprintf("%s · partial=%s · missing PVs=%s · size=%s · free=%s", vg.Name, fallback(vg.Partial), fallback(vg.MissingPVCount), fallback(vg.PrettySize), fallback(vg.PrettyFree))})
		}
		for _, lv := range status.LVMLogicalVolumes {
			fields = append(fields, detailField{label: "LVM LOGICAL VOLUME", value: fmt.Sprintf("%s · %s · layout=%s · active=%s · size=%s", lv.Name, fallback(lv.Path), fallback(lv.Layout), fallback(lv.Active), fallback(lv.Size))})
		}
		if !lvmFound && len(status.LVMVolumeGroups) == 0 && len(status.LVMLogicalVolumes) == 0 {
			fields = append(fields, detailField{label: "LVM STATUS", value: "no LVM resources on this node"})
		}
	}
	if !status.MDKnown {
		fields = append(fields, detailField{label: "MD RAID", value: "unknown"})
	} else {
		mdFound := false
		for _, array := range status.MDArrays {
			if !deviceMatches(array.Device, disk.DeviceName) && !containsDevice(array.Members, disk.DeviceName) {
				continue
			}
			mdFound = true
			fields = append(fields, detailField{label: "MD RAID", value: fmt.Sprintf("%s · %s · %s · sync=%s · %d active devices · %s", fallback(array.Name), array.Device, array.Level, fallback(array.SyncAction), array.RaidDevices, fallback(array.Status))})
			if array.Error != "" {
				fields = append(fields, detailField{label: "MD RAID ERROR", value: array.Error})
			}
		}
		if !mdFound {
			fields = append(fields, detailField{label: "MD RAID", value: "no array uses this disk"})
		}
	}
	if !status.VolumesKnown {
		fields = append(fields, detailField{label: "VOLUMES", value: "unknown"})
	} else {
		volumeFound := false
		for _, volume := range status.Volumes {
			if !deviceMatches(volume.Location, disk.DeviceName) && !deviceMatches(volume.ParentLocation, disk.DeviceName) {
				continue
			}
			volumeFound = true
			fields = append(fields, detailField{label: "VOLUME", value: fmt.Sprintf("%s · %s · %s · %s · %s · %s", volume.ID, volume.Phase, volume.Type, fallback(volume.Filesystem), fallback(volume.MountLocation), volumeOperationState(volume, status))})
		}
		if !volumeFound {
			fields = append(fields, detailField{label: "VOLUMES", value: "no volume status uses this disk"})
		}
	}
	return renderDetail(fields)
}

func volumeFilesystemSize(volume domain.VolumeSnapshot) string {
	return fmt.Sprintf("%s · %s", fallback(volume.Filesystem), formatBytes(int64(volume.SizeBytes)))
}

func volumeOperationState(volume domain.VolumeSnapshot, status domain.NodeStatusSnapshot) string {
	trim := "trim disabled"
	if volume.TrimEnabled {
		trim = "trim status unknown"
		if status.TrimKnown {
			trim = "trim schedule unavailable"
			for _, schedule := range status.TrimSchedules {
				if schedule.ID == volume.ID {
					trim = "next trim " + formatStatusTime(schedule.NextTrim)
					break
				}
			}
		}
	}
	scrub := "scrub disabled"
	if volume.ScrubEnabled {
		scrub = "scrub status unknown"
		if status.ScrubKnown {
			scrub = "scrub result pending"
			for _, result := range status.FSScrubs {
				if result.Mountpoint == volume.MountLocation {
					scrub = "scrub " + fallback(result.Status)
					break
				}
			}
		}
	}
	return trim + ", " + scrub
}

func formatStatusTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.Format(time.RFC3339)
}

func deviceMatches(left, right string) bool {
	return left != "" && right != "" && (left == right || path.Base(left) == path.Base(right))
}

func containsDevice(values []string, device string) bool {
	for _, value := range values {
		if deviceMatches(value, device) {
			return true
		}
	}
	return false
}
