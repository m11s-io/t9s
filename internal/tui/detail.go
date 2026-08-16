package tui

import (
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
)

func renderNodeDetail(node domain.NodeSnapshot) string {
	var view strings.Builder
	view.WriteString("NODE DETAIL\n")
	view.WriteString(fmt.Sprintf("NAME       %s\n", node.DisplayName()))
	view.WriteString(fmt.Sprintf("ID         %s\n", fallback(node.ID)))
	view.WriteString(fmt.Sprintf("ROLE       %s\n", node.Role))
	view.WriteString(fmt.Sprintf("STAGE      %s\n", fallback(node.Stage)))
	view.WriteString(fmt.Sprintf("HEALTH     %s\n", node.Health))
	view.WriteString(fmt.Sprintf("SERVICES   %s\n", node.Services.String()))
	view.WriteString(fmt.Sprintf("KUBERNETES %s\n", node.Kubernetes))
	if node.KubernetesNode != nil {
		view.WriteString(fmt.Sprintf("ROLES      %s\n", fallback(strings.Join(node.KubernetesNode.Roles, ", "))))
		view.WriteString(fmt.Sprintf("KUBELET    %s\n", fallback(node.KubernetesNode.KubeletVersion)))
		for _, condition := range node.KubernetesNode.Conditions {
			view.WriteString(fmt.Sprintf("CONDITION  %s=%s\n", condition.Type, condition.Status))
		}
	}
	view.WriteString(fmt.Sprintf("VERSION    %s\n", fallback(node.Version)))
	if len(node.Addresses) > 0 {
		view.WriteString(fmt.Sprintf("ADDRESSES  %s\n", strings.Join(node.Addresses, ", ")))
	}
	if node.Problem != "" {
		view.WriteString(fmt.Sprintf("PROBLEM    %s\n", node.Problem))
	}
	return strings.TrimSuffix(view.String(), "\n")
}

func renderProcessDetail(process domain.ProcessSnapshot) string {
	var view strings.Builder
	view.WriteString("PROCESS DETAIL\n")
	view.WriteString(fmt.Sprintf("PID              %d\n", process.PID))
	view.WriteString(fmt.Sprintf("PPID             %d\n", process.PPID))
	view.WriteString(fmt.Sprintf("STATE            %s\n", fallback(process.State)))
	view.WriteString(fmt.Sprintf("THREADS          %d\n", process.Threads))
	view.WriteString(fmt.Sprintf("CPU TIME         %.1fs\n", process.CPUTime))
	view.WriteString(fmt.Sprintf("VIRTUAL MEMORY   %s\n", formatBytes(int64(process.VirtualMemory))))
	view.WriteString(fmt.Sprintf("RESIDENT MEMORY  %s\n", formatBytes(int64(process.ResidentMemory))))
	view.WriteString(fmt.Sprintf("COMMAND          %s\n", fallback(process.Command)))
	view.WriteString(fmt.Sprintf("EXECUTABLE       %s\n", fallback(process.Executable)))
	view.WriteString(fmt.Sprintf("ARGS             %s\n", fallback(process.Args)))
	view.WriteString(fmt.Sprintf("LABEL            %s\n", fallback(process.Label)))
	return strings.TrimSuffix(view.String(), "\n")
}

func renderLinkDetail(link domain.LinkSnapshot) string {
	var view strings.Builder
	view.WriteString("LINK DETAIL\n")
	view.WriteString(fmt.Sprintf("NAME        %s\n", fallback(link.Name)))
	view.WriteString(fmt.Sprintf("TYPE        %s\n", fallback(link.Type)))
	view.WriteString(fmt.Sprintf("STATE       %s\n", fallback(link.OperationalState)))
	view.WriteString(fmt.Sprintf("HW ADDRESS  %s\n", fallback(link.HardwareAddr)))
	view.WriteString(fmt.Sprintf("MTU         %d\n", link.MTU))
	view.WriteString(fmt.Sprintf("DRIVER      %s\n", fallback(link.Driver)))
	view.WriteString("ADDRESSES\n")
	if len(link.Addresses) == 0 {
		view.WriteString("  -\n")
	}
	for _, address := range link.Addresses {
		view.WriteString(fmt.Sprintf("  %s (%s)\n", address.Address, address.Scope))
	}
	view.WriteString("ROUTES\n")
	if len(link.Routes) == 0 {
		view.WriteString("  -\n")
	}
	for _, route := range link.Routes {
		if route.Gateway == "" {
			view.WriteString(fmt.Sprintf("  %s (%s)\n", route.Destination, route.Table))
			continue
		}
		view.WriteString(fmt.Sprintf("  %s via %s (%s)\n", route.Destination, route.Gateway, route.Table))
	}
	return strings.TrimSuffix(view.String(), "\n")
}

func renderDiskDetail(disk domain.DiskSnapshot) string {
	var view strings.Builder
	view.WriteString("DISK DETAIL\n")
	view.WriteString(fmt.Sprintf("DEVICE      %s\n", fallback(disk.DeviceName)))
	view.WriteString(fmt.Sprintf("MODEL       %s\n", fallback(disk.Model)))
	view.WriteString(fmt.Sprintf("SERIAL      %s\n", fallback(disk.Serial)))
	view.WriteString(fmt.Sprintf("TYPE        %s\n", fallback(disk.Type)))
	view.WriteString(fmt.Sprintf("SIZE        %s\n", formatBytes(int64(disk.SizeBytes))))
	view.WriteString(fmt.Sprintf("BUS PATH    %s\n", fallback(disk.BusPath)))
	view.WriteString(fmt.Sprintf("SYSTEM DISK %t\n", disk.SystemDisk))
	view.WriteString(fmt.Sprintf("READ ONLY   %t\n", disk.ReadOnly))
	return strings.TrimSuffix(view.String(), "\n")
}
