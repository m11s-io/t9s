package tui

import (
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
)

type detailField struct {
	label string
	value string
}

func renderDetail(fields []detailField) string {
	skin := defaultK9sSkin()
	lines := make([]string, 0, len(fields)+1)
	header := fmt.Sprintf(" %-*s %s", fieldLabelWidth, "FIELD", "VALUE")
	lines = append(lines, skin.tableHeader.Render(header))
	for _, field := range fields {
		label := fmt.Sprintf(" %-*s", fieldLabelWidth, sanitizeUntrustedText(field.label))
		value := sanitizeUntrustedText(field.value)
		lines = append(lines, skin.tableRow.Render(label)+skin.tableRow.Render(" "+value))
	}
	return strings.Join(lines, "\n")
}

func styleDetail(content string) string {
	if content == "" {
		return ""
	}
	skin := defaultK9sSkin()
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		lines[index] = skin.tableRow.Render(" " + sanitizeUntrustedText(line))
	}
	return strings.Join(lines, "\n")
}

func renderNodeDetail(node domain.NodeSnapshot) string {
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
	if node.Problem != "" {
		fields = append(fields, detailField{label: "PROBLEM", value: node.Problem})
	}
	return renderDetail(fields)
}

func renderProcessDetail(process domain.ProcessSnapshot) string {
	return renderDetail([]detailField{
		{label: "PID", value: fmt.Sprintf("%d", process.PID)},
		{label: "PPID", value: fmt.Sprintf("%d", process.PPID)},
		{label: "STATE", value: fallback(process.State)},
		{label: "THREADS", value: fmt.Sprintf("%d", process.Threads)},
		{label: "CPU TIME", value: fmt.Sprintf("%.1fs", process.CPUTime)},
		{label: "VIRTUAL MEMORY", value: formatBytes(int64(process.VirtualMemory))},
		{label: "RESIDENT MEMORY", value: formatBytes(int64(process.ResidentMemory))},
		{label: "COMMAND", value: fallback(process.Command)},
		{label: "EXECUTABLE", value: fallback(process.Executable)},
		{label: "ARGS", value: fallback(process.Args)},
		{label: "LABEL", value: fallback(process.Label)},
	})
}

func renderLinkDetail(link domain.LinkSnapshot) string {
	fields := []detailField{
		{label: "NAME", value: fallback(link.Name)},
		{label: "TYPE", value: fallback(link.Type)},
		{label: "STATE", value: fallback(link.OperationalState)},
		{label: "HW ADDRESS", value: fallback(link.HardwareAddr)},
		{label: "MTU", value: fmt.Sprintf("%d", link.MTU)},
		{label: "DRIVER", value: fallback(link.Driver)},
	}
	if len(link.Addresses) == 0 {
		fields = append(fields, detailField{label: "ADDRESSES", value: "-"})
	} else {
		for _, address := range link.Addresses {
			fields = append(fields, detailField{label: "ADDRESSES", value: fmt.Sprintf("%s (%s)", address.Address, address.Scope)})
		}
	}
	if len(link.Routes) == 0 {
		fields = append(fields, detailField{label: "ROUTES", value: "-"})
	} else {
		for _, route := range link.Routes {
			if route.Gateway == "" {
				fields = append(fields, detailField{label: "ROUTES", value: fmt.Sprintf("%s (%s)", route.Destination, route.Table)})
				continue
			}
			fields = append(fields, detailField{label: "ROUTES", value: fmt.Sprintf("%s via %s (%s)", route.Destination, route.Gateway, route.Table)})
		}
	}
	return renderDetail(fields)
}

func renderDiskDetail(disk domain.DiskSnapshot) string {
	return renderDetail([]detailField{
		{label: "DEVICE", value: fallback(disk.DeviceName)},
		{label: "MODEL", value: fallback(disk.Model)},
		{label: "SERIAL", value: fallback(disk.Serial)},
		{label: "TYPE", value: fallback(disk.Type)},
		{label: "SIZE", value: formatBytes(int64(disk.SizeBytes))},
		{label: "BUS PATH", value: fallback(disk.BusPath)},
		{label: "SYSTEM DISK", value: fmt.Sprintf("%t", disk.SystemDisk)},
		{label: "READ ONLY", value: fmt.Sprintf("%t", disk.ReadOnly)},
	})
}
