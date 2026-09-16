---
title: Etcd operations
description: Snapshot, defragment, and disarm alarms from the :etcd view.
---

All actions on this page require `--enable-writes` (or `T9S_ENABLE_WRITES`) and
run only after their own prompt: a path prompt for `s`, an inline `(y/n)`
confirmation for `d`/`A`. With writes disabled the keys below are inert and
the read-only membership view is unchanged.

Open `:etcd` (`:et`) and select a member row.

| Key | Action | Notes |
| --- | --- | --- |
| `s` | Snapshot the selected member's etcd to a local file. | Opens a path prompt prefilled with a timestamped default. |
| `d` | Defragment the selected member's etcd data directory. | Resource-heavy; acts on one node at a time. |
| `A` | Disarm the selected member's active etcd alarms. | Does not reclaim disk and does not repair corruption. |

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
