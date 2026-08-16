# t9s

`t9s` is a standalone, resource-first terminal UI for exploring Talos Linux
clusters. It opens on objective cluster resources and layers explainable health
information over those snapshots.

> **Explore a Talos cluster quickly, see what is healthy or unhealthy, and
> drill from any resource into the evidence.**

## Current status

This repository implements the full Phase 1 read-only MVP scope. It
discovers Talos contexts, opens a context-scoped session, renders a compact
read-only `:nodes` view with node detail, filters nodes with `/`, and
switches contexts with `:contexts` or `:ctx`. Also implemented: services
with service detail and streaming logs, machine events (`:events`), etcd
membership and status (`:etcd`), node-scoped process lists (press `p` on a
selected node), node-scoped disk devices (press `k`), node-scoped network
interfaces (press `n`), a generic read-only Talos resource browser
(`:resources`/`:res`), explainable health rules with `:overview` and
`:problems`, optional Kubernetes Node correlation on `:nodes` when a
Kubernetes context matches the active Talos context, and the k9s Node-view
launcher (`--kube-context`/`--node`, see [k9s integration](#k9s-integration)
below). Passing `--enable-writes` (or setting `T9S_ENABLE_WRITES`)
additionally enables gated node reboot and shutdown from `:nodes`
(`space` to mark, `R`/`X` to act, each behind a confirm prompt) — see
[Security](#security) below; the UI remains read-only by default.

Cross-platform release binaries, checksums, and installation documentation
are available — see [Install](#install) below. Signed artifacts are not yet
implemented.

The supported Talos version is **v1.13.3**.

## Install

```bash
brew install m11s-io/tap/t9s
```

Prebuilt binaries for Linux and macOS are also available on the
[GitHub Releases](https://github.com/m11s-io/t9s/releases) page. See
[Installation](https://t9s.m11s.io/getting-started/installation/) for
download, checksum verification, and `PATH` setup steps.

## Build, test, and run

Go 1.26.3 is required.

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
additionally allows reboot and shutdown of selected node(s) from the
nodes screen, each gated behind an inline confirmation that flags
control-plane and etcd-quorum risk. The header's `[RO]`/`[RW]` badge
always reflects the active mode. As before, the Talos credentials
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
