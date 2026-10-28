---
title: Nodes
description: Inspect cluster nodes and filter the current snapshot.
---

The node explorer is the initial t9s view. Return to it with `:nodes` or `:no`.

- Move through the list with the navigation keys shown by `?`.
- Press `Enter` or `d` to open read-only node details.
- Press `r` to refresh the resource snapshot.
- Press `Esc` or `q` to return from details.
- Press `e` for the node's kernel log (dmesg), `s` for its network sockets (netstat), `m` for its filesystem mounts, `f` for memory, or `c` for containers and images; see [Node diagnostics](/guides/diagnostics/).

At startup, discovered node names and roles appear first. Details fill in as each
node inspection finishes; a slow node does not hold back completed rows.

The node inventory and selected diagnostics that poll automatically refresh
every 2 seconds. Container/image lists and directory scans refresh on demand;
press `r` for an immediate refresh. Automatic reads pause while editing filters
or prompts.
Changed and newly added table rows flash orange for two seconds; the first load
sets the baseline, and the selected row keeps its selection color.

## Filter nodes

Press `/`, type a query, and press `Enter`. Filtering changes only the visible rows; it does not modify the underlying snapshot or cluster.

Press `Esc` while editing a filter to clear it. Open `/` again to replace the current query.

## Upgrade a Talos node

Start t9s with `--enable-writes` (or `T9S_ENABLE_WRITES`) before using the upgrade action. On a selected node, press `U`, review the image suggestion, edit the target tag if needed, and complete the explicit confirmation.

The suggestion preserves the running node's Image Factory repository details: factory, installer flavor, and schematic ID. It uses the running Talos version as the initial tag so an upgrade prompt cannot silently downgrade a node whose declared image is stale. On Talos v1.14+, live schematic discovery first uses the authoritative `ImageFactorySchematic` resource. If it is unavailable or undecodable, t9s falls back to the installed `ExtensionStatus` resource named `schematic` and platform metadata, then to the declared image. Digest references remain unchanged.

For Talos versions supporting `LifecycleService`, the notice area streams image pull, install, Kubernetes drain, reboot, readiness wait, and uncordon. Byte progress is shown when totals are available. Verified completion requires lifecycle exit code zero, readiness checks, and completion of cleanup. Nodes outside the lifecycle API range use the legacy upgrade RPC; its final notice says the request was accepted and completion has not been verified.

### Single control-plane maintenance

A normal upgrade refuses to take the only etcd member offline. If the selected
node is the sole control-plane node and etcd has exactly one known healthy
member, the refused prompt offers `d allow downtime`. Press `d`, review the
maintenance panel and local snapshot destination, then press `y` to confirm or
`n`/`Esc` to cancel. The Kubernetes API and scheduling will be unavailable during
the reboot; existing workloads may also be interrupted by draining.

Confirmation takes a fresh, checksum-verified local etcd snapshot before sending
the upgrade. The default filename includes the context, node and UTC timestamp
in the current working directory; existing files are not overwritten. Snapshot
failure or a change in node identity, etcd membership or health stops the upgrade.
The terminal result retains the snapshot receipt; press `v` on the Nodes screen
to show it again. This snapshot protects cluster state, not application volume
data: confirm Longhorn volumes have recovered before maintaining the next node.

The downtime choice does not enable Force or bypass drain/PDB protection. It is
unavailable for multi-member clusters, learners, unhealthy/unknown membership,
or missing snapshot capability. During lifecycle recovery, t9s retries Kubernetes
API availability within the normal readiness timeout. Older Talos versions keep
the legacy accepted/unverified completion semantics described above.

Pull and install share a 20-minute client budget. Maintenance preparation,
cordon, and reboot requests each have a 30-second budget. Drain, readiness, and
cleanup have separate limits; the entire workflow can take longer than 20
minutes. An interrupted or failed request reports an unconfirmed remote outcome
and retains the last observed stage. Check the node before retrying: stopping a
request does not undo an installation or reboot.

If the image was applied but readiness or scheduling cleanup remains incomplete,
t9s keeps an `Applied — recovery warning` notice. Check the node's readiness and
scheduling state manually before resuming workloads. The upgrade attempts to undo
only a cordon it created; an existing operator cordon is preserved. Once the
upgrade attempt finishes, refreshing the node view does not retry uncordon or
clear the warning. Switching Talos context or starting another upgrade replaces
the notice.

A parseable target tag more than one minor ahead receives an advisory warning that intermediate Talos minor releases are skipped. Talos remains the authority for compatibility checks. This action upgrades Talos only; Kubernetes control-plane upgrades are separate.

## Force reboot or shutdown

Start t9s with `--enable-writes` (or `T9S_ENABLE_WRITES`). In the ordinary
confirmation for a single node, press `f`, type the exact node name, and press
`Enter` to prepare force mode. Review the new risk assessment and confirm with
`y` to dispatch, or `n`/`Esc` to cancel. Force can skip graceful shutdown or
drain and interrupt workloads. It is unavailable for bulk actions and cannot
bypass an etcd-quorum or other action block.

## Reset/wipe a Talos node

Start t9s with `--enable-writes` (or `T9S_ENABLE_WRITES`) before using reset. On a selected node (or the marked nodes), press `W`. A content-area overlay opens with the disk preview and risk text; choose the wipe scope, whether the node leaves etcd first (graceful), and whether the node reboots or halts. Then type the confirmation token and press `Enter`. The accepted reset still passes through the final `(y/n)` confirm, which re-evaluates etcd quorum against the current snapshot.

Wipe scope options:

- `ALL` erases the system disk and every discovered user disk (the default). It is single-node only, because the user-disk inventory is per node.
- `SYSTEM_DISK` erases only the system disk and is bulk-safe (it never touches user disks).
- `USER_DISKS` erases every discovered user disk, and is refused when the inventory is unknown, when no user disk is discovered, or for a bulk reset — so t9s never guesses which disks to erase.

The confirmation token is the single target's node name, or `wipe N nodes` for a bulk reset. This typed-intent step happens before the gated confirm: the final `(y/n)` is retained because quorum must be re-evaluated at confirm time.

Safety rules:

- Every reset of a control-plane node is hard-gated on etcd quorum, graceful or not. A reset is refused when the target is the last known quorum holder, or when the etcd snapshot is unavailable so the impact is unknown.
- `ALL` and `USER_DISKS` are refused when the disk inventory is unknown or for a bulk reset, because Talos wipes only the user disks t9s explicitly enumerates. `SYSTEM_DISK` remains available in those cases because it does not touch user disks.
- The disk preview classifies whole disks only. The disks reader does not expose individual partitions, so system-partition granularity is not selectable; `ALL`/`SYSTEM_DISK` reset the entire system disk.
- Read-only devices (for example CD-ROMs or ISOs) are never enumerated as wipe targets, because Talos refuses the whole reset request when any listed user disk is read-only.
- A reset that reboots the node may return a connection error after it actually succeeded. The failure text is generic and the action is not retried.

## Kubernetes correlation

When the active Talos context's name exactly matches a context name in your kubeconfig, `t9s` automatically enriches each node with its corresponding Kubernetes Node: a `K8S` column (`Ready`/`NotReady`/`Unknown`) in the table, and a `KUBERNETES` block (roles, kubelet version, conditions) in node detail.

No configuration is required for the exact-name-match case. If the names don't match, the column reads `Unknown` for every row rather than being hidden — Kubernetes is optional enrichment and its absence never blocks Talos views. To associate contexts with different names, or when launching from k9s, see [k9s integration](/guides/k9s-integration/).
