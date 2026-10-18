package tui

import "github.com/m11s-io/t9s/internal/application"

// syncScreens copies only the state affected by the application message. Stream
// batches therefore leave unrelated table selection and health diagnoses alone.
func (m model) syncScreens(message application.Message) model {
	// Stream opening results are application-private; their explicit scope
	// exposes only which existing screen owns the update.
	switch application.ResultScope(message) {
	case "logs":
		m.logs = m.logs.setState(m.application.Logs)
		return m
	case "dmesg":
		m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
		return m
	case "healthcheck":
		m.healthcheck = m.healthcheck.setState(m.application.HealthCheck)
		return m
	case "events":
		m.events = m.events.syncLegacySelection(m.application.Events)
		return m
	case "node-services":
		m.nodes = m.nodes.normalizeSelection(m.application.Nodes.Value.Nodes, m.nodes.control.Cursor())
		m.services = m.services.normalizeSelection(m.application.Services.Value.Services, m.services.control.Cursor())
		return m
	}
	switch message.(type) {
	case application.SelectContext, application.SessionOpened:
		return m.syncAllScreens()
	case application.ContextsLoaded, application.NodesLoaded, application.LoadFailed,
		application.RefreshNodes, application.UpgradeSucceeded:
		m.nodes = m.nodes.normalizeSelection(m.application.Nodes.Value.Nodes, m.nodes.control.Cursor())
	case application.ServicesLoaded, application.ServicesFailed, application.RefreshServices:
		m.services = m.services.normalizeSelection(m.application.Services.Value.Services, m.services.control.Cursor())
	case application.KubernetesNodesLoaded, application.KubernetesNodesFailed:
		m.nodes = m.nodes.normalizeSelection(m.application.Nodes.Value.Nodes, m.nodes.control.Cursor())
	case application.EventsLoaded, application.EventsFailed, application.RefreshEvents,
		application.ToggleEventFollow, application.StopEventFollow:
		m.events = m.events.syncLegacySelection(m.application.Events)
	case application.EtcdLoaded, application.EtcdFailed, application.RefreshEtcd,
		application.EtcdActionSucceeded:
		m.etcd = m.etcd.normalizeSelection(m.application.Etcd.Value.Members, m.etcd.control.Cursor())
	case application.OpenProcesses, application.RefreshProcesses, application.ProcessesLoaded, application.ProcessesFailed:
		m.processes = m.processes.setState(m.application.Processes)
	case application.OpenDisks, application.RefreshDisks, application.DisksLoaded, application.DisksFailed:
		m.disks = m.disks.setState(m.application.Disks)
	case application.ActionSucceeded:
		// Disk wipe completion starts a new disk read; other actions leave this
		// screen alone.
		if m.disks.state.Status != m.application.Disks.Status {
			m.disks = m.disks.setState(m.application.Disks)
		}
	case application.OpenNetwork, application.RefreshNetwork, application.NetworkLoaded, application.NetworkFailed:
		m.network = m.network.setState(m.application.Network)
	case application.OpenNetstat, application.RefreshNetstat, application.NetstatLoaded, application.NetstatFailed:
		m.netstat = m.netstat.setState(m.application.Netstat)
	case application.OpenMounts, application.RefreshMounts, application.MountsLoaded, application.MountsFailed:
		m.mounts = m.mounts.setState(m.application.Mounts)
	case application.OpenMemory, application.RefreshMemory, application.MemoryLoaded, application.MemoryFailed:
		m.memory = m.memory.setState(m.application.Memory)
	case application.OpenTime, application.RefreshTime, application.CloseTime, application.CheckTime,
		application.TimeLoaded, application.TimeChecked:
		m.time = m.time.setState(m.application.Time)
	case application.OpenCPU, application.RefreshCPU, application.CloseCPU, application.CPULoaded:
		m.cpu = m.cpu.setState(m.application.CPU)
	case application.OpenDmesg, application.ReconnectDmesg, application.CloseDmesg, application.ClearDmesg,
		application.DmesgBatchLoaded:
		m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
	case application.OpenServiceLogs, application.ReconnectServiceLogs, application.CloseServiceLogs, application.ClearServiceLogs,
		application.ServiceLogBatchLoaded:
		m.logs = m.logs.setState(m.application.Logs)
	case application.OpenClusterHealth, application.CloseClusterHealth, application.ClearClusterHealth,
		application.ClusterHealthProgressLoaded:
		m.healthcheck = m.healthcheck.setState(m.application.HealthCheck)
	case application.OpenResourceBrowser:
		m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
		m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
	case application.ResourceKindsLoaded, application.ResourceKindsFailed:
		m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
	case application.SelectResourceKind, application.ResourceInstancesLoaded, application.ResourceInstancesFailed:
		m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
	}
	return m
}

func (m model) syncAllScreens() model {
	m.nodes = m.nodes.normalizeSelection(m.application.Nodes.Value.Nodes, m.nodes.control.Cursor())
	m.services = m.services.normalizeSelection(m.application.Services.Value.Services, m.services.control.Cursor())
	m.events = m.events.syncLegacySelection(m.application.Events)
	m.etcd = m.etcd.normalizeSelection(m.application.Etcd.Value.Members, m.etcd.control.Cursor())
	m.processes = m.processes.setState(m.application.Processes)
	m.disks = m.disks.setState(m.application.Disks)
	m.network = m.network.setState(m.application.Network)
	m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
	m.healthcheck = m.healthcheck.setState(m.application.HealthCheck)
	m.netstat = m.netstat.setState(m.application.Netstat)
	m.mounts = m.mounts.setState(m.application.Mounts)
	m.memory = m.memory.setState(m.application.Memory)
	m.time = m.time.setState(m.application.Time)
	m.cpu = m.cpu.setState(m.application.CPU)
	m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
	m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
	m.logs = m.logs.setState(m.application.Logs)
	return m
}
