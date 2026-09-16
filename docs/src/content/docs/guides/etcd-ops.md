---
title: Etcd operations
description: Snapshot, defragment, and disarm alarms from the :etcd view.
---

All actions on this page require `--enable-writes` (or `T9S_ENABLE_WRITES`) and
run only after their own prompt: a path prompt for `s`, an inline `(y/n)`
confirmation for `R`/`L`/`d`/`A`/`F`. With writes disabled the keys below are inert
and the read-only membership view is unchanged.

Open `:etcd` (`:et`) and select a member row.

| Key | Action | Notes |
| --- | --- | --- |
| `s` | Snapshot the selected member's etcd to a local file. | Opens a path prompt prefilled with a timestamped default. |
| `R` | **Remove** the selected member by ID (forced). | Destructive: snapshots first, then hard-gated on quorum. Only for a dead member. |
| `L` | **Leave** the cluster gracefully. | Destructive: snapshots first, then hard-gated on quorum. For a live member. |
| `d` | Defragment the selected member's etcd data directory. | Resource-heavy; acts on one node at a time. |
| `A` | Disarm the selected member's active etcd alarms. | Does not reclaim disk and does not repair corruption. |
| `F` | **Forfeit** leadership so another member can take over. | Quorum-neutral: no snapshot and no quorum gate. Warns when no healthy follower is known. |

## Snapshot (`s`)

`s` opens a text prompt prefilled with a safe default such as
`etcd-<context>-<hostname>-20260818T231500Z.db`. Edit the path and press
`Enter` to confirm, or `Esc` to cancel. Only a single node is snapshotted;
the snapshot is never multiplexed across members.

The write is atomic and defensive:

- the payload streams to `<path>.part` (mode `0600`), created with `O_EXCL`
  so an existing staging file is never clobbered;
- an existing **final** path is refused rather than overwritten, including a
  file that appears between the check and the commit (the staging file is
  hard-linked into place, which cannot clobber);
- the trailing sha256 checksum is verified against the payload before the
  staging file is committed;
- on any failure the `.part` file is removed and no final file is left behind.

A `.part` file left by an interrupted process (for example a `SIGKILL`) must be
removed by hand before retrying the same destination path.

A completed snapshot is never deleted automatically. t9s adds no credential
material to the file, but an etcd snapshot is a full copy of the cluster's etcd
keyspace — including Kubernetes `Secret`s and service-account tokens unless
encryption at rest is configured. Store and handle it like cluster data, not
like innocuous metadata.

Failed snapshots have no effect on cluster state; the notice reports the
error and nothing on the node is changed.

## Remove member (`R`) and leave cluster (`L`)

These are the only actions that change etcd membership. They are irreversible,
so t9s enforces two safety rules before either can run.

**1. Snapshot before destructive.** Confirming `R`/`L` does not fire the
membership RPC immediately. t9s first takes a full local snapshot of the
target cluster to a timestamped path (shown in the confirm prompt, e.g.
`after snapshot to etcd-<context>-<hostname>-20260818T231500Z.db`). If that
snapshot fails or is cancelled, the removal is aborted with the error and
**no membership change happens**. The snapshot is written with the same
atomic, checksum-verified, no-clobber guarantees as `s` above. The snapshot is
taken from a healthy voter other than the target when one exists.

**2. Quorum hard gate.** Removing or leaving a voting member that would drop
the cluster below quorum is refused outright — the prompt reports the refusal
and `y` does nothing. Unknown etcd state (`Loading`/`Failed`/`Idle`) is
likewise refused for membership changes, not merely warned about, because a
removal is irreversible. Removing a **learner** is quorum-neutral and is not
blocked by the gate (it still requires the snapshot).

### `R` vs `L`

- Use **`L` (leave)** for a member that is still running and reachable. The
  RPC is executed by the member itself, so it is addressed to the selected
  member's own node, and the member shuts down cleanly as it leaves.
- Use **`R` (remove)** only for a member that is already dead/unreachable.
  The RPC is addressed to a live control-plane member. Forcing the removal of
  a member that is still reporting healthy is refused — t9s steers you to `L`.

Add `--enable-writes` first, select the member row, then `R` or `L`. After a
successful change the `:etcd` view re-reads membership so the member list
reflects the new cluster.

## Defragment (`d`)

Defragmentation releases unused space in the selected member's etcd data
directory. It is resource-heavy and runs on exactly one node per confirmation,
so run it one member at a time. The `:etcd` view refreshes after it completes.

## Disarm alarms (`A`)

Disarms active etcd alarms (`NOSPACE`, `CORRUPT`, and future alarm types) on
the selected member. A `NOSPACE` alarm makes the cluster read-only; disarming
it lifts the read-only restriction but **does not reclaim disk** — free space
first, or the alarm will return. Disarming a `CORRUPT` alarm does not repair
corruption; restore from a snapshot instead. After a successful disarm the
`:etcd` view re-reads membership so the `ALARMS` column reflects the change.

See [Health](/guides/health/) for how active alarms surface as
`etcd-member-alarmed` diagnoses.

## Forfeit leadership (`F`)

`F` asks the selected member's etcd process to give up leadership so a
preferred member can take over — for example to move leadership off a node
you are about to restart. It is addressed to the member's own node, runs on
one member per confirmation, and is quorum-neutral: the member keeps voting
through the handoff, so there is no snapshot and no quorum hard gate. The
`:etcd` view refreshes after it completes.

When no other healthy voter is known to be able to take leadership, the
confirm prompt warns (`no healthy follower is known to take leadership`). The
warning is advisory — a forfeit cannot drop quorum — so you may still proceed;
check the membership view first if the cluster looks degraded.
