---
title: Commands
description: Commands available from the t9s command palette.
---

Press `:` to open the command input below the header, above the main content.
Press `Enter` to run the command or `Esc` to close the input.

| Command | Alias | Result |
| --- | --- | --- |
| `:nodes` | `:no` | Open the node explorer. |
| `:services` | `:svc` | Open Talos services. |
| `:contexts` | `:ctx` | Select another discovered context. |
| `:config` | | Preview and apply a local Talos machine configuration to selected nodes. Requires `--enable-writes`; see [Security](/security/). |
| `:upgrade-k8s` | `:k8s-upgrade` | Prepare and, with `--enable-writes`, run a cluster Kubernetes upgrade through Talos. Requires `talosctl` v1.14.5; see [Security](/security/). |
| `:events` | `:ev` | Open the machine event snapshot; `s` toggles live follow. |
| `:etcd` | `:et` | Open etcd membership and status; with `--enable-writes`, snapshot/membership maintenance actions live here. See [Etcd operations](/guides/etcd-ops/). |
| `:bgp` | `:bgps` | Show BGP peer sessions across nodes, including peer ASN, FSM state, BFD, and route counters. |
| `:overview` | `:ov` | Cluster-wide health summary. See [Health](/guides/health/). |
| `:problems` | | Drillable list of everything currently unhealthy. See [Health](/guides/health/). |
| `:healthcheck` | `:hc` | Stream the server-side cluster health check; accepts an optional wait timeout (e.g. `:healthcheck 5m`, default 60s). See [Cluster health check](/guides/health/#healthcheck). |
| `:resources` | `:res` | Generic Talos resource browser. `:resources <Kind>` (e.g. `:resources MachineStatus`) jumps straight to that kind's instances. See [Resource browser](/guides/resources/). |

Unknown commands produce a notice and do not execute a shell command. The palette has no arbitrary execution path.

`:upgrade-k8s` requires an explicit stable target version and a new local etcd
snapshot path. Its preview is read-only and checks cluster identity, versions,
node mapping, permissions, Talos compatibility, and the selected `talosctl`
release. Execution takes and verifies the snapshot before invoking the standard
`talosctl upgrade-k8s` command for control-plane components and managed
manifests. t9s then upgrades kubelets one node at a time by updating their image,
waiting for the service, and verifying readiness and version. Node scheduling
is unchanged; draining is a separate maintenance operation.
See [Security](/security/) for cancellation and recovery behavior.

`:config` reads a nonempty regular file from the local machine (maximum 4 MiB),
then lets you select one or more nodes and explicitly choose `AUTO`,
`NO_REBOOT`, or `STAGED`. It sends a server-side dry-run to every selected node
and displays the file basename, a short SHA-256 digest, canonical targets,
resolved modes, and normalized preview status. Talos diffs, warning text, and
validation error text are hidden because they can contain configuration
secrets. Apply is available only when every target preview succeeds and you
type the displayed confirmation phrase. Nodes are applied sequentially; see
[Security](/security/) for partial-result behavior and safe retry guidance.

## Node-scoped keys

These open from a selected row in `:nodes` rather than through the command palette:

| Key | Result |
| --- | --- |
| `Enter` / `d` | Open node detail. |
| `p` | Open processes. See [Processes, disks, and network](/guides/processes-disks-network/). |
| `k` | Open disks (with `--enable-writes`, `W` there wipes a single device). See [Processes, disks, and network](/guides/processes-disks-network/). |
| `n` | Open network interfaces, addresses, and routes. |
| `e` | Stream the node's kernel log (dmesg). See [Node diagnostics](/guides/diagnostics/). |
| `s` | Open the node's network sockets (netstat). See [Node diagnostics](/guides/diagnostics/). |
| `m` | Open the node's filesystem mounts. See [Node diagnostics](/guides/diagnostics/). |
| `f` | Open the node's memory usage. See [Node diagnostics](/guides/diagnostics/). |
| `u` | Sample aggregate/per-core CPU utilization and running/blocked process counts. `r` takes a new sample; `/` filters; there is no background polling. See [CPU utilization](/guides/diagnostics/#cpu-utilization-and-process-counts). |
| `t` | Open time synchronization status and UTC samples. In the view, `r` refreshes normal status, `/` filters, and `c` opens an explicit server check (`Enter` submits, `Esc` cancels). Checks work without writes mode and keep a separate result panel. See [Time](/guides/diagnostics/#time). |
| `c` | Inspect containers in the `system` namespace. Inside the view, `n` cycles namespaces and `i` switches between containers and images. See [Containers and images](/guides/diagnostics/#containers-and-images). |
| `space` | Mark/unmark the selected node for a bulk action. Requires `--enable-writes`. |
| `R` | Reboot the marked node(s) (or the selected node if none are marked), behind a confirm prompt. For one target, press `f` to choose force mode, type the exact node name, then confirm again; force may interrupt workloads and cannot bypass a quorum block. Requires `--enable-writes`. |
| `X` | Shut down the marked node(s) (or the selected node if none are marked), behind a confirm prompt. For one target, press `f` to choose force mode, type the exact node name, then confirm again; force may interrupt workloads and cannot bypass a quorum block. Requires `--enable-writes`. |
| `B` | Roll back the marked node(s) (or the selected node if none are marked) to the previous Talos OS install, behind a confirm prompt. Requires `--enable-writes`. |
| `U` | Upgrade the selected node to a specified Talos OS image, behind a prompt prefilled from the running node's schematic/current version and a confirm step. Shows lifecycle progress when supported; requires `--enable-writes`. |
| `W` | Reset/wipe the marked node(s) (or the selected node if none are marked), behind a preview overlay, a typed confirmation token, and a confirm step. Requires `--enable-writes`. See [Nodes](/guides/nodes/). |

Each of these views supports `r` to refresh (reconnect for dmesg) and `Esc`/`q` to return to `:nodes`.

`space`, `R`, `X`, `B`, `W`, and `U` are inert unless `t9s` was started with `--enable-writes` (or `T9S_ENABLE_WRITES`); see [Security](/security/).

## Disks-scoped keys

These act on the selected row in `:disks` (opened with `k` from `:nodes`):

| Key | Result |
| --- | --- |
| `Enter` / `d` | Open disk detail, including matching LVM physical volumes, volume groups/logical volumes, MD arrays, and Talos volume status. |
| `W` | Wipe the selected block device, behind a typed device-path prompt and a confirm step. The system disk and read-only devices are refused. Requires `--enable-writes`. See [Processes, disks, and network](/guides/processes-disks-network/). |

`W` is inert unless `t9s` was started with `--enable-writes` (or `T9S_ENABLE_WRITES`); see [Security](/security/).

## Etcd-scoped keys

These act on the selected row in `:etcd`:

| Key | Result |
| --- | --- |
| `s` | Snapshot the selected member's etcd to a local file, behind a path prompt. Requires `--enable-writes`. See [Etcd operations](/guides/etcd-ops/). |
| `R` | Remove the selected member by ID (forced), behind a confirm prompt. Snapshots first, then refuses if it would drop etcd below quorum. Requires `--enable-writes`. |
| `L` | Make the selected member leave the cluster gracefully, behind a confirm prompt. Snapshots first, then refuses if it would drop etcd below quorum. Requires `--enable-writes`. |
| `d` | Defragment the selected member's etcd data directory, behind a confirm prompt. Requires `--enable-writes`. |
| `A` | Disarm the selected member's active etcd alarms, behind a confirm prompt. Requires `--enable-writes`. |
| `F` | Make the selected member forfeit leadership so another member can take over, behind a confirm prompt. Quorum-neutral; no snapshot. Requires `--enable-writes`. |

`s`, `R`, `L`, `d`, `A`, and `F` are inert unless `t9s` was started with `--enable-writes` (or `T9S_ENABLE_WRITES`); see [Security](/security/).

## Service-scoped keys

These act on the selected row in `:services`:

| Key | Result |
| --- | --- |
| `S` | Start the selected service, behind a confirm prompt. Requires `--enable-writes`. |
| `T` | Stop the selected service, behind a confirm prompt. Requires `--enable-writes`. |
| `R` | Restart the selected service, behind a confirm prompt. Requires `--enable-writes`. |

`S`, `T`, and `R` are inert unless `t9s` was started with `--enable-writes` (or `T9S_ENABLE_WRITES`); see [Security](/security/).

## Event keys

In `:events`, `/` filters by node, kind, or message. `s` starts or stops live
following; no write permission is required. `r` refreshes the snapshot while
paused and leaves an active follow stream unchanged. See [Live machine events](/guides/diagnostics/#live-machine-events).
