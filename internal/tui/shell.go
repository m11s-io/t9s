package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// nodeAutoRefreshInterval paces refreshes of the active inventory or diagnostic
// table. Filtering and prompts pause reads while the heartbeat keeps running.
const nodeAutoRefreshInterval = 2 * time.Second

type nodeAutoRefreshTickMsg struct{}

type shellLayout struct {
	Width         int
	Height        int
	HeaderHeight  int
	CommandHeight int
	ContentHeight int
}

// nodeAutoRefreshTick fires once and must be rescheduled by its own handler
// to keep the heartbeat running; it self-reschedules regardless of whether a
// given tick actually triggered a refresh, so the loop survives view
// changes and filtering pauses.
func nodeAutoRefreshTick() tea.Cmd {
	return tea.Tick(nodeAutoRefreshInterval, func(time.Time) tea.Msg { return nodeAutoRefreshTickMsg{} })
}

func layoutShell(width, height int, commandActive ...bool) shellLayout {
	width = max(0, width)
	height = max(0, height)
	remaining := height

	headerHeight := min(k9sHeaderHeight, remaining)
	if height < 16 {
		headerHeight = min(2, remaining)
	}
	remaining -= headerHeight

	fixedBottom := min(2, remaining) // breadcrumb and flash
	remaining -= fixedBottom
	commandHeight := 0
	if len(commandActive) > 0 && commandActive[0] {
		commandHeight = min(3, remaining)
		remaining -= commandHeight
	}

	return shellLayout{
		Width:         width,
		Height:        height,
		HeaderHeight:  headerHeight,
		CommandHeight: commandHeight,
		ContentHeight: remaining,
	}
}

func renderStartupScreen(size contentSize, skin k9sSkin) string {
	content := []string{
		skin.logo.Render("t9s"),
		skin.actionText.Render("Inspect Talos clusters"),
		skin.flashInfo.Render("◌ Loading cluster data…"),
	}
	lines := make([]string, max(0, size.Height))
	start := max(0, (len(lines)-len(content))/2)
	for index, line := range content {
		row := start + index
		if row >= len(lines) {
			break
		}
		padding := max(0, (size.Width-ansi.StringWidth(line))/2)
		lines[row] = strings.Repeat(" ", padding) + line
	}
	return strings.Join(lines, "\n")
}

func renderShell(layout shellLayout, header, content, footer string, commandBar ...string) string {
	if layout.Height == 0 {
		return ""
	}

	lines := make([]string, 0, layout.Height)
	lines = appendRegion(lines, header, layout.HeaderHeight, layout.Width)
	if layout.CommandHeight > 0 {
		bar := ""
		if len(commandBar) > 0 {
			bar = commandBar[0]
		}
		lines = appendRegion(lines, bar, layout.CommandHeight, layout.Width)
	}
	lines = appendRegion(lines, content, layout.ContentHeight, layout.Width)

	bottomHeight := layout.Height - layout.HeaderHeight - layout.CommandHeight - layout.ContentHeight
	lines = appendRegion(lines, footer, bottomHeight, layout.Width)

	for len(lines) < layout.Height {
		lines = append(lines, fitShellLine("", layout.Width))
	}
	return strings.Join(lines[:layout.Height], "\n")
}

func appendRegion(lines []string, text string, height, width int) []string {
	region := strings.Split(text, "\n")
	if text == "" {
		region = nil
	}
	for index := 0; index < height; index++ {
		line := ""
		if index < len(region) {
			line = region[index]
		}
		lines = append(lines, fitShellLine(line, width))
	}
	return lines
}

func fitShellLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	return strings.TrimRight(ansi.Truncate(strings.ReplaceAll(line, "\t", "    "), width, "…"), " ")
}

// Automatic refresh must not repeatedly cancel a read slower than the heartbeat.
// Manual refresh still supersedes the previous request through beginWithContext.
func (m model) activeRefreshPending() bool {
	frame := m.views.top()
	var slots []string
	switch frame.Kind {
	case viewNodes, viewServices:
		slots = []string{"cluster-nodes-services", "cluster-kubernetes"}
	case viewOverview, viewProblems:
		slots = []string{"cluster-nodes-services", "cluster-kubernetes", "cluster-events", "cluster-etcd", "cluster-node-status"}
	case viewBGPPeers:
		slots = []string{"cluster-nodes-services", "cluster-node-status"}
	case viewEvents:
		slots = []string{"cluster-events"}
	case viewEtcd:
		slots = []string{"cluster-etcd"}
	case viewResourceKinds:
		slots = []string{"resources:kinds"}
	case viewResourceInstances:
		slots = []string{directResourceInstanceSlot(frame.NodeID, frame.ResourceKind)}
	default:
		scope := map[viewKind]string{viewProcesses: "processes", viewDisks: "disks", viewNetwork: "network", viewNetstat: "sockets", viewMounts: "mounts", viewMemory: "memory", viewCPU: "cpu", viewTime: "time"}[frame.Kind]
		if scope != "" {
			slots = []string{directDiagnosticSlot(frame.NodeID, scope)}
		}
	}
	m.requests.mu.Lock()
	defer m.requests.mu.Unlock()
	for _, slot := range slots {
		if pending, ok := m.requests.slots[slot]; ok && pending.request.ctx.Err() == nil {
			return true
		}
	}
	return false
}

func (m model) shellLayout() shellLayout {
	return layoutShell(m.width, m.height, m.palette.active)
}
