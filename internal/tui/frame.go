package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/m11s-io/t9s/internal/application"
)

type resourceFrame struct {
	Width, Height           int
	InnerWidth, InnerHeight int
	Title                   string
}

func layoutResourceFrame(width, height int, title string) resourceFrame {
	width, height = max(0, width), max(0, height)
	return resourceFrame{Width: width, Height: height, InnerWidth: max(0, width-2), InnerHeight: max(0, height-2), Title: title}
}

func renderResourceFrame(frame resourceFrame, content string, skin k9sSkin) string {
	if frame.Width == 0 || frame.Height == 0 {
		return ""
	}
	if frame.Width < 2 || frame.Height < 2 {
		return fitK9sCell(content, frame.Width)
	}
	title := " " + frame.Title + " "
	if ansi.StringWidth(title) > frame.InnerWidth {
		title = ansi.Truncate(title, frame.InnerWidth, "")
	}
	titleWidth := ansi.StringWidth(title)
	left := max(0, (frame.InnerWidth-titleWidth)/2)
	right := max(0, frame.InnerWidth-left-titleWidth)
	bottom := "└" + strings.Repeat("─", frame.InnerWidth) + "┘"
	borderColor := skin.focusedFrame.GetBorderTopForeground()
	border := skin.frame.Copy().UnsetBorderStyle().Foreground(borderColor)
	top := border.Render("┌"+strings.Repeat("─", left)) + renderResourceFrameTitle(title, skin) + border.Render(strings.Repeat("─", right)+"┐")

	contentLines := strings.Split(content, "\n")
	lines := make([]string, 0, frame.Height)
	lines = append(lines, top)
	for row := 0; row < frame.InnerHeight; row++ {
		line := ""
		if row < len(contentLines) {
			line = contentLines[row]
		}
		lines = append(lines, border.Render("│")+fitK9sCell(line, frame.InnerWidth)+border.Render("│"))
	}
	lines = append(lines, border.Render(bottom))
	return strings.Join(lines, "\n")
}

func renderResourceFrameTitle(title string, skin k9sSkin) string {
	title = sanitizeUntrustedText(title)
	counter := ""
	trailing := ""
	end := len(title)
	for end > 0 && title[end-1] == ' ' {
		end--
	}
	if start := strings.LastIndexByte(title[:end], '['); start >= 0 && end > start && title[end-1] == ']' {
		counter = title[start:end]
		trailing = title[end:]
		title = title[:start]
	}

	start := strings.IndexByte(title, '(')
	if start < 0 {
		return renderTitleBase(title, skin) + renderTitleCounter(counter, skin) + renderTitleBase(trailing, skin)
	}
	scopeEnd := strings.IndexByte(title[start:], ')')
	if scopeEnd < 0 {
		return renderTitleBase(title, skin) + renderTitleCounter(counter, skin) + renderTitleBase(trailing, skin)
	}
	scopeEnd += start
	return renderTitleBase(title[:start], skin) +
		skin.titleHighlight.Render(title[start:scopeEnd+1]) +
		renderTitleBase(title[scopeEnd+1:], skin) +
		renderTitleCounter(counter, skin) +
		renderTitleBase(trailing, skin)
}

func renderTitleBase(text string, skin k9sSkin) string {
	if text == "" {
		return ""
	}
	return skin.title.Render(text)
}

func renderTitleCounter(counter string, skin k9sSkin) string {
	if counter == "" {
		return ""
	}
	return skin.titleCounter.Render(counter)
}

func directResourceTitle(model model, kind viewKind) string {
	selected := model.views.top()
	switch kind {
	case viewServices:
		return fmt.Sprintf("services(all)[%d]", len(model.cluster.services.Value.Services))
	case viewServiceLogs:
		return "logs"
	case viewServiceDetail:
		return titleWithScope("service", selected.Label)
	case viewNodeDetail:
		return titleWithScope("node", selected.Label)
	case viewHelp:
		return "help"
	case viewContexts:
		return fmt.Sprintf("contexts(all)[%d]", len(model.contexts.values))
	case viewEvents:
		return fmt.Sprintf("events(all)[%d]", len(model.cluster.events.Value.Events))
	case viewEtcd:
		return fmt.Sprintf("etcd(all)[%d]", len(model.cluster.etcd.Value.Members))
	case viewProcesses:
		return fmt.Sprintf("processes(%s)[%d]", model.processes.node, len(model.processes.values.Processes))
	case viewProcessDetail:
		if selected.ProcessPID != 0 {
			return titleWithScope("process", fmt.Sprint(selected.ProcessPID))
		}
		return "process"
	case viewDisks:
		return fmt.Sprintf("disks(%s)[%d]", model.disks.node, len(model.disks.values.Disks))
	case viewDiskDetail:
		return titleWithScope("disk", selected.DeviceName)
	case viewNetwork:
		return fmt.Sprintf("network(%s)[%d]", model.network.node, len(model.network.values.Links))
	case viewLinkDetail:
		return titleWithScope("link", selected.LinkName)
	case viewDmesg:
		return model.dmesg.title
	case viewClusterHealth:
		return fmt.Sprintf("healthcheck[%d]", len(model.healthcheck.state.Lines))
	case viewNetstat:
		return fmt.Sprintf("netstat(%s)[%d]", model.netstat.node, len(model.netstat.values.Sockets))
	case viewMounts:
		return fmt.Sprintf("mounts(%s)[%d]", model.mounts.node, len(model.mounts.values.Mounts))
	case viewCPU:
		return fmt.Sprintf("cpu(%s)", model.cpu.node)
	case viewTime:
		return fmt.Sprintf("time(%s)", model.time.node)
	case viewMemory:
		return fmt.Sprintf("memory(%s)", model.memory.node)
	case viewDiskUsage:
		return fmt.Sprintf("diskusage(%s)[%d]", model.diskUsage.path, len(model.diskUsage.rows))
	case viewContainers:
		mode := "containers"
		count := len(model.containers.values.Containers)
		if selected.ImageMode {
			mode, count = "images", len(model.containers.images)
		}
		return fmt.Sprintf("%s(%s/%s)[%d]", mode, selected.NodeTarget, selected.ContainerNamespace, count)
	case viewContainerDetail:
		if selected.ImageMode {
			return titleWithScope("image", firstNonEmpty(selected.ImageReference, selected.ImageDigest))
		}
		return titleWithScope("container", selected.ContainerID)
	case viewOverview:
		return "overview"
	case viewProblems:
		return fmt.Sprintf("problems[%d]", len(application.EvaluateObservations(directHealthInput(model.cluster))))
	case viewResourceKinds:
		return fmt.Sprintf("resources(all)[%d]", len(model.resourceKinds.values.Kinds))
	case viewResourceInstances:
		return fmt.Sprintf("resources(%s)[%d]", model.resourceInstances.selectedKind, len(model.resourceInstances.values.Instances))
	case viewResourceDetail:
		kindName := firstNonEmpty(selected.ResourceKind, model.resourceDetail.value.Type)
		identity := selected.ResourceID
		if selected.ResourceNamespace != "" {
			identity = selected.ResourceNamespace + "/" + identity
		}
		return titleWithScope(kindName, identity)
	default:
		return fmt.Sprintf("nodes(all)[%d]", len(model.cluster.nodes.Value.Nodes))
	}
}

func titleWithScope(kind, identity string) string {
	kind = sanitizeUntrustedText(kind)
	identity = sanitizeUntrustedText(identity)
	if kind == "" {
		return ""
	}
	if identity == "" || identity == "-" {
		return kind
	}
	return kind + "(" + identity + ")"
}
