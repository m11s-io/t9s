package tui

import (
	"fmt"
	"slices"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

// Track values before filtering, layout and truncation, using resource identity.
// Empty first snapshots still establish a baseline for newly arriving rows.
type tableChanges struct {
	scope     string
	previous  map[string][]string
	deadlines map[string]time.Time
}

func observeTable[T any](changes *tableChanges, scope string, rows []T, key func(T) string, values func(T) []string, now time.Time) bool {
	if changes.scope != scope {
		*changes = tableChanges{scope: scope}
	}
	next := make(map[string][]string, len(rows))
	deadlines := make(map[string]time.Time, len(changes.deadlines))
	changed := false
	for _, row := range rows {
		id := key(row)
		if id == "" {
			continue
		}
		next[id] = values(row)
		if deadline, ok := changes.deadlines[id]; ok {
			deadlines[id] = deadline
		}
		if changes.previous != nil && !slices.Equal(changes.previous[id], next[id]) {
			deadlines[id] = now.Add(rowChangeDuration)
			changed = true
		}
	}
	changes.previous, changes.deadlines = next, deadlines
	return changed
}

func changedRowSkin(key string, changes []tableChanges) k9sSkin {
	skin := defaultK9sSkin()
	if len(changes) > 0 {
		if _, ok := changes[0].deadlines[key]; ok {
			skin.tableRow = skin.tableRow.Foreground(lipgloss.Color("#FFA500"))
		}
	}
	return skin
}

func processChangeKey(process domain.ProcessSnapshot) string { return fmt.Sprint(process.PID) }
func socketChangeKey(socket domain.SocketSnapshot) string {
	if socket.Inode != 0 {
		return fmt.Sprintf("%s/%s/%d", socket.Netns, socket.Protocol, socket.Inode)
	}
	return socket.Netns + "\x00" + socket.Protocol + "\x00" + socket.LocalAddress + "\x00" + socket.RemoteAddress
}
func eventChangeValues(event domain.EventSnapshot) []string {
	values := eventRowValues(event)
	return values[:len(values)-1] // Observation time changes on reads, not resource updates.
}

func (m *model) tableTrackers() []*tableChanges {
	return []*tableChanges{&m.services.changes, &m.events.changes, &m.etcd.changes, &m.problems.changes, &m.processes.changes, &m.disks.changes, &m.network.changes, &m.netstat.changes, &m.mounts.changes, &m.memory.changes, &m.resourceKinds.changes, &m.resourceInstances.changes}
}

func tableReady(status directLoadStatus) bool {
	return status == directReady || status == directPartial
}

func (m *model) observeTables(now time.Time) bool {
	changed := false
	if tableReady(m.cluster.services.Status) {
		changed = observeTable(&m.services.changes, "", m.cluster.services.Value.Services, serviceKey, serviceRowValues, now) || changed
	}
	if tableReady(m.cluster.events.Status) {
		rows := eventRows(m.cluster.events.Value)
		changed = observeTable(&m.events.changes, "", rows, nextEventKey(rows), eventChangeValues, now) || changed
	}
	if tableReady(m.cluster.etcd.Status) {
		changed = observeTable(&m.etcd.changes, "", m.cluster.etcd.Value.Members, etcdMemberKey, etcdRowValues, now) || changed
	}
	if tableReady(m.cluster.nodes.Status) {
		changed = observeTable(&m.problems.changes, "", application.EvaluateObservations(directHealthInput(m.cluster)), diagnosisKey, problemRowValues, now) || changed
	}
	if tableReady(m.processes.status) {
		changed = observeTable(&m.processes.changes, m.processes.node, m.processes.values.Processes, processChangeKey, processRowValues, now) || changed
	}
	if tableReady(m.disks.status) {
		changed = observeTable(&m.disks.changes, m.disks.node, m.disks.values.Disks, func(d domain.DiskSnapshot) string { return d.DeviceName }, diskRowValues, now) || changed
	}
	if tableReady(m.network.status) {
		changed = observeTable(&m.network.changes, m.network.node, m.network.values.Links, func(l domain.LinkSnapshot) string { return l.Name }, linkRowValues, now) || changed
	}
	if tableReady(m.netstat.status) {
		changed = observeTable(&m.netstat.changes, m.netstat.node, m.netstat.values.Sockets, socketChangeKey, netstatRowValues, now) || changed
	}
	if tableReady(m.mounts.status) {
		changed = observeTable(&m.mounts.changes, m.mounts.node, m.mounts.values.Mounts, func(v domain.MountSnapshot) string { return v.MountedOn }, mountsRowValues, now) || changed
	}
	if tableReady(m.memory.status) {
		changed = observeTable(&m.memory.changes, m.memory.node, memoryRows(m.memory.values), func(v memoryMetric) string { return v.Label }, memoryRowValues, now) || changed
	}
	if tableReady(m.resourceKinds.status) {
		changed = observeTable(&m.resourceKinds.changes, "", m.resourceKinds.values.Kinds, func(v domain.ResourceKindSnapshot) string { return v.Type }, resourceKindRowValues, now) || changed
	}
	if tableReady(m.resourceInstances.status) {
		changed = observeTable(&m.resourceInstances.changes, m.resourceInstances.selectedKind, m.resourceInstances.values.Instances, resourceInstanceIdentity, resourceInstanceRowValues, now) || changed
	}
	return changed
}

// Events have no immutable ID; match the occurrence keys used by selection.
func nextEventKey(events []domain.EventSnapshot) func(domain.EventSnapshot) string {
	keys := eventKeys(events)
	index := 0
	return func(domain.EventSnapshot) string { key := keys[index]; index++; return key }
}
