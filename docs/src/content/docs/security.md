---
title: Security
description: Protect Talos credentials and understand the gated write path.
---

By default the t9s UI is read-only and offers no mutation operation and no
arbitrary command path. Passing `--enable-writes` (or setting
`T9S_ENABLE_WRITES`) additionally allows reboot, shutdown, rollback, reset, and
upgrade of selected node(s) from the `:nodes` screen (`space` to mark rows,
`R` to reboot, `X` to shut down, `B` to roll back to the previous Talos OS
install, `W` to reset/wipe, `U` to upgrade to a specified Talos OS image), as well as service
start/stop/restart from the `:services` screen (`S` to start, `T` to stop,
`R` to restart), each gated behind an inline confirmation prompt that flags
control-plane and etcd-quorum risk before it runs. The header's `[RO]`/`[RW]`
badge always reflects whether writes are active for the current session.
Control-plane actions that would drop etcd below quorum are refused outright —
the confirmation prompt reports the refusal instead of accepting a `y` — not
merely warned about.

Reset/wipe (`W`) is the most destructive node action, so it layers two
confirmation steps on top of the gate. First the operator types an exact token
— the single target's node name, or `wipe N nodes` for a bulk reset — in a
preview overlay that classifies the target's system and user disks. Only then
does the accepted reset open the final `(y/n)` confirm, which re-evaluates etcd
quorum against the current snapshot. Every reset of a control-plane node is
hard-gated on quorum, graceful or not: a reset is refused when the target is the
last known quorum holder or when the etcd snapshot is unavailable so the impact
is unknown. The `ALL` and `USER_DISKS` scopes are refused when the node's disk
inventory is unknown or for a bulk reset, because Talos wipes only the user
disks t9s explicitly enumerates — t9s never guesses which disks to erase.
`SYSTEM_DISK` remains available in those cases because it does not touch user
disks. Read-only devices are never enumerated as wipe targets (Talos rejects a
reset listing a read-only user disk). The disks reader exposes whole disks only,
so partition-level selection is not offered. A reset that reboots the node may return a connection
error after it actually succeeded; the failure text is generic and the action is
not retried.

The node-scoped diagnostics views — the kernel log (`e`/dmesg), network
sockets (`s`/netstat), filesystem mounts (`m`), and memory (`f`) — are read-only
and inert with respect to writes: they never require `--enable-writes`, and
enabling writes adds no mutation to them.
See [Node diagnostics](/guides/diagnostics/).

The same gate covers etcd maintenance: `--enable-writes` additionally allows
etcd snapshot (`s`), member removal (`R`), graceful leave (`L`), defragment
(`d`), and alarm disarm (`A`) from the `:etcd` screen, each behind its own
prompt. Etcd snapshot writes a local file atomically — it streams to a `0600`
`<path>.part` file, refuses to overwrite an existing final path, verifies the
trailing sha256 checksum against the payload, and only then commits the staging
file into place. t9s adds no credential material to the file, but the snapshot
itself is a full copy of the cluster's etcd keyspace (including Kubernetes
`Secret`s), so protect it as cluster data. Local destination paths are chosen
by the operator, so operators must own that directory. A completed snapshot is
never deleted automatically. Any node-level action that would drop etcd below
quorum is refused, not merely warned about, and no effect is built or
dispatched before a confirmation succeeds.

Etcd membership changes (`R`/`L`) carry two extra guarantees. First, they are
snapshot-before-destructive: confirming either one takes a mandatory local
snapshot of the target cluster and only fires the membership RPC after that
snapshot completes successfully; a failed or cancelled snapshot aborts the
change with nothing mutated. Second, membership changes are hard-gated on
quorum — a removal or leave that would drop voting members below quorum, or a
forced removal of a member that is still healthy, or a change attempted while
etcd state is unknown, is refused outright rather than warned about. Learners
do not vote, so removing one is never quorum-blocked, but it still requires the
snapshot. The refused prompt stays on screen until explicitly cancelled.

Talos upgrade is behind the same `--enable-writes` gate and explicit confirmation. For Talos versions that support `LifecycleService`, the selected node is drained before reboot and uncordoned after readiness, including cleanup after later-stage failure; the streamed progress is described in [Nodes](/guides/nodes/). Talos versions outside that lifecycle range use the legacy upgrade RPC and do not claim those streamed maintenance stages. Progress and errors are normalized; credentials, talosconfig contents, kubeconfig contents, and registry tokens are not stored in model state or logs. Kubernetes control-plane upgrades are not part of this action.

The supplied Talos credentials can still be privileged. Protect every talosconfig as a sensitive secret and grant only the permissions an operator needs.

- Never commit a real talosconfig, certificate, private key, token, endpoint, or internal hostname.
- Prefer narrowly scoped Talos roles where the cluster policy allows them.
- Keep configuration files readable only by the intended local user.
- Use invented identities, reserved addresses, and fake credentials in tests and examples.

Report security issues through the private security-reporting channel configured on the GitHub repository rather than a public issue.
