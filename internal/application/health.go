package application

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

func EvaluateObservations(input HealthInput) []domain.Diagnosis {
	var diagnoses []domain.Diagnosis

	if input.NodesKnown {
		for _, node := range input.Nodes.Nodes {
			diagnoses = append(diagnoses, evaluateNodeReadiness(node)...)
			diagnoses = append(diagnoses, evaluateNodeServicesDegraded(node)...)
		}
	}

	if input.EtcdKnown {
		for _, member := range input.Etcd.Members {
			diagnoses = append(diagnoses, evaluateEtcdMemberUnhealthy(member)...)
			diagnoses = append(diagnoses, evaluateEtcdMemberAlarmed(member)...)
		}
	}
	if input.NodeStatusesKnown {
		for _, status := range input.NodeStatuses.Statuses {
			diagnoses = append(diagnoses, evaluateBGPPeers(status)...)
			diagnoses = append(diagnoses, evaluateLVM(status)...)
			diagnoses = append(diagnoses, evaluateMDArrays(status)...)
			diagnoses = append(diagnoses, evaluateVolumes(status)...)
			diagnoses = append(diagnoses, evaluateTrimSchedules(status)...)
			diagnoses = append(diagnoses, evaluateFSScrubs(status)...)
		}
	}
	if input.ServicesKnown {
		diagnoses = append(diagnoses, evaluateSandboxdService(input.Services)...)
	}

	sort.SliceStable(diagnoses, func(i, j int) bool {
		if diagnoses[i].Severity != diagnoses[j].Severity {
			return diagnoses[i].Severity > diagnoses[j].Severity
		}
		if diagnoses[i].ResourceKind != diagnoses[j].ResourceKind {
			return diagnoses[i].ResourceKind < diagnoses[j].ResourceKind
		}
		return diagnoses[i].ResourceName < diagnoses[j].ResourceName
	})

	return diagnoses
}

func evaluateSandboxdService(services domain.ServiceSet) []domain.Diagnosis {
	var diagnoses []domain.Diagnosis
	for _, service := range services.Services {
		if service.Name != "sandboxd" || service.Healthy == nil || *service.Healthy {
			continue
		}
		diagnoses = append(diagnoses, domain.Diagnosis{
			RuleID:       "sandboxd-service-unhealthy",
			Severity:     domain.SeverityCritical,
			Summary:      "sandboxd service is unhealthy",
			Evidence:     []string{"state=" + fallbackHealthValue(service.State), "message=" + fallbackHealthValue(service.LastMessage)},
			ResourceKind: "sandboxd",
			ResourceID:   service.Node,
			ResourceName: service.Node,
		})
	}
	return diagnoses
}

func evaluateBGPPeers(status domain.NodeStatusSnapshot) []domain.Diagnosis {
	if !status.BGPKnown {
		return nil
	}
	var diagnoses []domain.Diagnosis
	for _, peer := range status.BGPPeers {
		state := strings.ToUpper(peer.State)
		if state == "ESTABLISHED" {
			continue
		}
		severity := domain.SeverityWarning
		if state == "" || state == "UNKNOWN" || strings.HasPrefix(state, "BGPSESSIONSTATE(") {
			severity = domain.SeverityUnknown
		}
		diagnoses = append(diagnoses, domain.Diagnosis{
			RuleID:       "bgp-peer-not-established",
			Severity:     severity,
			Summary:      "BGP peer is not established",
			Evidence:     []string{"state=" + fallbackHealthValue(peer.State), "peer-asn=" + fmt.Sprint(peer.PeerASN)},
			ResourceKind: "bgp-peer",
			ResourceID:   firstHealthValue(peer.ID, peer.Peer),
			ResourceName: firstHealthValue(peer.Peer, status.Node),
		})
	}
	return diagnoses
}

func evaluateLVM(status domain.NodeStatusSnapshot) []domain.Diagnosis {
	var diagnoses []domain.Diagnosis
	if status.LVMPhysicalVolumesKnown {
		for _, pv := range status.LVMPhysicalVolumes {
			if strings.EqualFold(pv.Missing, "missing") {
				diagnoses = append(diagnoses, domain.Diagnosis{
					RuleID: "lvm-volume-missing", Severity: domain.SeverityCritical,
					Summary: "LVM physical volume is missing", Evidence: []string{"device=" + pv.Device, "volume-group=" + fallbackHealthValue(pv.VGName)},
					ResourceKind: "lvm-physical-volume", ResourceID: pv.Device, ResourceName: pv.Device,
				})
			}
		}
	}
	if status.LVMVolumeGroupsKnown {
		for _, vg := range status.LVMVolumeGroups {
			missing, _ := strconv.Atoi(vg.MissingPVCount)
			if strings.EqualFold(vg.Partial, "partial") || missing > 0 {
				diagnoses = append(diagnoses, domain.Diagnosis{
					RuleID: "lvm-volume-group-partial", Severity: domain.SeverityCritical,
					Summary: "LVM volume group is partial", Evidence: []string{"partial=" + fallbackHealthValue(vg.Partial), "missing-pvs=" + fallbackHealthValue(vg.MissingPVCount)},
					ResourceKind: "lvm-volume-group", ResourceID: vg.Name, ResourceName: vg.Name,
				})
			}
		}
	}
	return diagnoses
}

func evaluateMDArrays(status domain.NodeStatusSnapshot) []domain.Diagnosis {
	if !status.MDKnown {
		return nil
	}
	var diagnoses []domain.Diagnosis
	for _, array := range status.MDArrays {
		state := strings.ToLower(array.Status)
		if state == "ready" && array.Error == "" {
			continue
		}
		severity := domain.SeverityUnknown
		switch state {
		case "error", "failed":
			severity = domain.SeverityCritical
		case "waiting", "rebuilding":
			severity = domain.SeverityWarning
		}
		if array.Error != "" {
			severity = domain.SeverityCritical
		}
		diagnoses = append(diagnoses, domain.Diagnosis{
			RuleID: "md-array-unhealthy", Severity: severity,
			Summary: "MD RAID array is not ready", Evidence: []string{"status=" + fallbackHealthValue(array.Status), "sync=" + fallbackHealthValue(array.SyncAction), "error=" + fallbackHealthValue(array.Error)},
			ResourceKind: "md-array", ResourceID: firstHealthValue(array.ID, array.Device), ResourceName: firstHealthValue(array.Name, array.Device),
		})
	}
	return diagnoses
}

func evaluateVolumes(status domain.NodeStatusSnapshot) []domain.Diagnosis {
	if !status.VolumesKnown {
		return nil
	}
	var diagnoses []domain.Diagnosis
	for _, volume := range status.Volumes {
		phase := strings.ToLower(volume.Phase)
		if volume.Error == "" && phase == "ready" {
			continue
		}
		severity := domain.SeverityUnknown
		if volume.Error != "" || phase == "failed" || phase == "error" || phase == "missing" {
			severity = domain.SeverityCritical
		} else if phase == "waiting" || phase == "located" || phase == "provisioned" || phase == "prepared" {
			severity = domain.SeverityWarning
		}
		summary := "Talos volume phase is unknown"
		if severity == domain.SeverityWarning {
			summary = "Talos volume is still provisioning"
		} else if severity == domain.SeverityCritical {
			summary = "Talos volume is failed or missing"
		}
		diagnoses = append(diagnoses, domain.Diagnosis{
			RuleID: "volume-failed", Severity: severity,
			Summary: summary, Evidence: []string{"phase=" + fallbackHealthValue(volume.Phase), "error=" + fallbackHealthValue(volume.Error)},
			ResourceKind: "volume", ResourceID: volume.ID, ResourceName: volume.ID,
		})
	}
	return diagnoses
}

func evaluateTrimSchedules(status domain.NodeStatusSnapshot) []domain.Diagnosis {
	if !status.TrimKnown {
		return nil
	}
	now := time.Now()
	var diagnoses []domain.Diagnosis
	for _, schedule := range status.TrimSchedules {
		severity := domain.SeverityWarning
		if schedule.NextTrim.IsZero() {
			severity = domain.SeverityUnknown
		} else if !schedule.NextTrim.Before(now) {
			continue
		}
		diagnoses = append(diagnoses, domain.Diagnosis{
			RuleID: "volume-trim-overdue", Severity: severity,
			Summary: "scheduled volume trim is overdue", Evidence: []string{"filesystem=" + fallbackHealthValue(schedule.Filesystem), "next-trim=" + fallbackHealthValue(schedule.NextTrim.Format(time.RFC3339))},
			ResourceKind: "volume-trim", ResourceID: schedule.ID, ResourceName: firstHealthValue(schedule.ID, status.Node),
		})
	}
	return diagnoses
}

func evaluateFSScrubs(status domain.NodeStatusSnapshot) []domain.Diagnosis {
	if !status.ScrubKnown {
		return nil
	}
	var diagnoses []domain.Diagnosis
	for _, scrub := range status.FSScrubs {
		if strings.EqualFold(scrub.Status, "success") {
			continue
		}
		severity := domain.SeverityCritical
		if strings.TrimSpace(scrub.Status) == "" {
			severity = domain.SeverityUnknown
		}
		diagnoses = append(diagnoses, domain.Diagnosis{
			RuleID: "filesystem-scrub-failed", Severity: severity,
			Summary: "filesystem scrub did not succeed", Evidence: []string{"status=" + fallbackHealthValue(scrub.Status), "mountpoint=" + fallbackHealthValue(scrub.Mountpoint)},
			ResourceKind: "filesystem-scrub", ResourceID: firstHealthValue(scrub.ID, scrub.Mountpoint), ResourceName: firstHealthValue(scrub.Mountpoint, status.Node),
		})
	}
	return diagnoses
}

func fallbackHealthValue(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func firstHealthValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func evaluateNodeReadiness(node domain.NodeSnapshot) []domain.Diagnosis {
	var severity domain.Severity
	switch node.Health {
	case domain.HealthUnhealthy:
		severity = domain.SeverityCritical
	case domain.HealthUnknown:
		severity = domain.SeverityUnknown
	default:
		return nil
	}

	evidence := []string{"stage=" + node.Stage}
	if node.Problem != "" {
		evidence = append(evidence, "problem="+node.Problem)
	}

	return []domain.Diagnosis{{
		RuleID:       "node-readiness",
		Severity:     severity,
		Summary:      "node not ready",
		Evidence:     evidence,
		ResourceKind: "node",
		ResourceID:   node.ID,
		ResourceName: node.DisplayName(),
	}}
}

func evaluateNodeServicesDegraded(node domain.NodeSnapshot) []domain.Diagnosis {
	if !node.Services.Known {
		return nil
	}
	unhealthy := node.Services.Total - node.Services.Healthy - node.Services.Unknown
	if unhealthy <= 0 {
		return nil
	}

	severity := domain.SeverityWarning
	if unhealthy == node.Services.Total {
		severity = domain.SeverityCritical
	}

	return []domain.Diagnosis{{
		RuleID:   "node-services-degraded",
		Severity: severity,
		Summary:  "services degraded",
		Evidence: []string{
			fmt.Sprintf("%d/%d services healthy", node.Services.Healthy, node.Services.Total),
			fmt.Sprintf("%d unhealthy, %d unknown", unhealthy, node.Services.Unknown),
		},
		ResourceKind: "node",
		ResourceID:   node.ID,
		ResourceName: node.DisplayName(),
	}}
}

// evaluateEtcdMemberAlarmed reports active etcd alarms (NOSPACE, CORRUPT, ...)
// as critical: a NOSPACE alarm makes the whole cluster read-only until it is
// cleared, so it must never be hidden behind a healthy-looking member status.
func evaluateEtcdMemberAlarmed(member domain.EtcdMemberSnapshot) []domain.Diagnosis {
	if len(member.Alarms) == 0 {
		return nil
	}

	return []domain.Diagnosis{{
		RuleID:       "etcd-member-alarmed",
		Severity:     domain.SeverityCritical,
		Summary:      "etcd member has active alarm",
		Evidence:     append([]string(nil), member.Alarms...),
		ResourceKind: "etcd-member",
		ResourceID:   fmt.Sprintf("%d", member.MemberID),
		ResourceName: member.Hostname,
	}}
}

func evaluateEtcdMemberUnhealthy(member domain.EtcdMemberSnapshot) []domain.Diagnosis {
	id := fmt.Sprintf("%d", member.MemberID)

	if !member.StatusKnown {
		return []domain.Diagnosis{{
			RuleID:       "etcd-member-unhealthy",
			Severity:     domain.SeverityUnknown,
			Summary:      "etcd member status unknown",
			Evidence:     []string{"status unknown"},
			ResourceKind: "etcd-member",
			ResourceID:   id,
			ResourceName: member.Hostname,
		}}
	}

	if len(member.Errors) == 0 {
		return nil
	}

	return []domain.Diagnosis{{
		RuleID:       "etcd-member-unhealthy",
		Severity:     domain.SeverityWarning,
		Summary:      "etcd member reporting errors",
		Evidence:     append([]string(nil), member.Errors...),
		ResourceKind: "etcd-member",
		ResourceID:   id,
		ResourceName: member.Hostname,
	}}
}
