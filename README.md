# t9s

`t9s` is a standalone, resource-first terminal UI for exploring Talos Linux
clusters. It opens on objective cluster resources and layers explainable health
information over those snapshots.

> **Explore a Talos cluster quickly, see what is healthy or unhealthy, and
> drill from any resource into the evidence.**

## Current status

Phases 0–3 of the Talos v1.14 coverage roadmap are implemented. Phase 5 now has
a cluster-scoped Kubernetes upgrade workflow; its disposable-cluster release
validation remains pending. t9s discovers
Talos contexts, opens a context-scoped session, and provides read-only views for
nodes, services and logs, events, etcd, processes, disks, network links and
routes, BGP peers (`:bgp`/`:bgps`), containers and images, memory, CPU, time,
mounts, sockets, kernel logs, and bounded directory usage. Disk details include
LVM and MD RAID status; node details include system volumes, trim/XFS scrub
status, workload isolation configuration, and `sandboxd` service health. The
generic Talos resource browser remains available at `:resources`/`:res`.

Explainable health rules are summarized by `:overview`/`:ov` and listed in
`:problems`. Optional Kubernetes Node correlation appears on `:nodes` when a
Kubernetes context matches the active Talos context. The k9s Node-view launcher
(`--kube-context`/`--node`) is documented in [k9s integration](#k9s-integration).
Passing `--enable-writes` (or setting `T9S_ENABLE_WRITES`) enables gated node
reboot, shutdown, rollback, reset, and Talos OS upgrade; service start/stop/
restart; device wipe; and etcd snapshot and membership maintenance. See
[Security](#security); t9s stays read-only by default.

Cross-platform release binaries, checksums, and installation documentation
are available — see [Install](#install) below. Signed artifacts are not yet
implemented.

The Talos SDK compatibility target is **v1.14.4**. A disposable two-node Docker
smoke check ran on 2026-10-20 and verified node/service discovery, the BGP and
health views, system-volume details, workload isolation, and the resource and
disk views. The cluster had no BGP peers, LVM/MD arrays, trim or scrub schedules,
or `sandboxd` service; it also did not expose a `SystemDisk` resource. Those
hardware-backed cases remain unverified. See [CONTRIBUTING.md](CONTRIBUTING.md)
for the full manual checklist.
Talos upgrades are node-level operations: t9s upgrades the Talos OS on the
selected node. Kubernetes upgrades use a separate `:upgrade-k8s` workflow and
require `talosctl` v1.14.4; see [Security](#security) and the
[command reference](docs/src/content/docs/reference/commands.md).

## Install

```bash
brew install m11s-io/tap/t9s
```

Prebuilt binaries for Linux and macOS are also available on the
[GitHub Releases](https://github.com/m11s-io/t9s/releases) page. See
[Installation](https://t9s.m11s.io/getting-started/installation/) for
download, checksum verification, and `PATH` setup steps.

## Build, test, and run

Go 1.27.1 is required.

```bash
go build ./cmd/t9s
go test ./...
go test -race ./...
go run ./cmd/t9s --context <name>
```

The context must exist in the Talos configuration resolved by the Talos client.
Pass `--talosconfig <path>` to select a specific file.

`t9s` also accepts multiple Talos config files. Repeat `--talosconfig` or set the
`t9s`-specific `TALOSCONFIGS` path-list environment variable (`:` on Unix, `;` on Windows):

```bash
export TALOSCONFIGS="$HOME/.talos/mgmt:$HOME/.talos/ai:$HOME/.talos/stage:$HOME/.talos/test"
go run ./cmd/t9s

# Equivalent explicit form
go run ./cmd/t9s \
  --talosconfig "$HOME/.talos/mgmt" \
  --talosconfig "$HOME/.talos/ai" \
  --talosconfig "$HOME/.talos/stage" \
  --talosconfig "$HOME/.talos/test"
```

When multiple files are supplied, t9s merges contexts in memory and rejects
duplicate context names. The standard singular `TALOSCONFIG` behavior remains
unchanged.

## Security

By default the UI is read-only and offers no mutation or arbitrary
command path. Passing `--enable-writes` (or setting `T9S_ENABLE_WRITES`)
additionally allows reboot, shutdown, rollback, and upgrade of selected
node(s) from the nodes screen, start/stop/restart of the selected service from
the services screen, and etcd snapshot/defragment/alarm-disarm from the etcd
screen, each gated behind its own prompt (a path prompt for snapshot, an inline
confirmation that flags control-plane and etcd-quorum risk otherwise). The header's `[RO]`/`[RW]` badge
always reflects the active mode. Talos upgrade progress covers image pull, install, drain, reboot, readiness, and uncordon when the lifecycle API is available; unsupported versions use the legacy upgrade RPC. A target skipping more than one Talos minor release receives an advisory warning. Kubernetes control-plane upgrades are separate. As before, the Talos credentials
supplied to `t9s` may themselves be privileged — protect the talosconfig
as a sensitive secret and grant only the permissions the operator needs.
Do not commit real endpoints or credentials as test data.

## k9s integration

Add this to k9s's `plugins.yaml` to open the selected Kubernetes Node
in `t9s` with `Shift-T` from k9s's Node view:

```yaml
plugins:
  t9s:
    shortCut: Shift-T
    description: Open in t9s
    scopes:
      - nodes
    command: t9s
    background: false
    args:
      - --kube-context
      - $CONTEXT
      - --node
      - $NAME
```

`$CONTEXT` and `$NAME` are k9s's own plugin placeholder variables — k9s
substitutes them with its active Kubernetes context and the selected
Node's name before invoking `t9s`. `t9s` resolves the corresponding
Talos context (see `--kube-context` above) and opens directly on that
node's detail view.

Contributor setup, architecture boundaries, fixture rules, and benchmark
instructions are in [CONTRIBUTING.md](CONTRIBUTING.md).

Documentation: <https://t9s.m11s.io>
