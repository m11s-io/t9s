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
	left := max(0, (frame.InnerWidth-ansi.StringWidth(title))/2)
	right := max(0, frame.InnerWidth-left-ansi.StringWidth(title))
	bottom := "└" + strings.Repeat("─", frame.InnerWidth) + "┘"
	borderColor := skin.focusedFrame.GetBorderTopForeground()
	border := skin.frame.Copy().UnsetBorderStyle().Foreground(borderColor)
	top := border.Render("┌"+strings.Repeat("─", left)) + skin.title.Render(title) + border.Render(strings.Repeat("─", right)+"┐")

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

func resourceTitle(kind viewKind, model application.Model) string {
	switch kind {
	case viewServices:
		return fmt.Sprintf("services(all)[%d]", len(model.Services.Value.Services))
	case viewServiceLogs:
		return "logs"
	case viewServiceDetail:
		return "service"
	case viewNodeDetail:
		return "node"
	case viewHelp:
		return "help"
	case viewContexts:
		return fmt.Sprintf("contexts(all)[%d]", len(model.Contexts))
	case viewEvents:
		return fmt.Sprintf("events(all)[%d]", len(model.Events.Value.Events))
	case viewEtcd:
		return fmt.Sprintf("etcd(all)[%d]", len(model.Etcd.Value.Members))
	case viewProcesses:
		return fmt.Sprintf("processes(%s)[%d]", model.Processes.Node, len(model.Processes.Value.Processes))
	case viewProcessDetail:
		return "process"
	case viewDisks:
		return fmt.Sprintf("disks(%s)[%d]", model.Disks.Node, len(model.Disks.Value.Disks))
	case viewDiskDetail:
		return "disk"
	case viewNetwork:
		return fmt.Sprintf("network(%s)[%d]", model.Network.Node, len(model.Network.Value.Links))
	case viewLinkDetail:
		return "link"
	case viewOverview:
		return "overview"
	case viewProblems:
		return fmt.Sprintf("problems[%d]", len(application.EvaluateHealth(model)))
	case viewResourceKinds:
		return fmt.Sprintf("resources(all)[%d]", len(model.ResourceBrowser.Kinds.Kinds))
	case viewResourceInstances:
		return fmt.Sprintf("resources(%s)[%d]", model.ResourceBrowser.SelectedKind, len(model.ResourceBrowser.Instances.Instances))
	case viewResourceDetail:
		return model.ResourceBrowser.Detail.Type
	default:
		return fmt.Sprintf("nodes(all)[%d]", len(model.Nodes.Value.Nodes))
	}
}
