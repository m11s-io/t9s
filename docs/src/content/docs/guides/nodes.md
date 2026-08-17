---
title: Nodes
description: Inspect cluster nodes and filter the current snapshot.
---

The node explorer is the initial t9s view. Return to it with `:nodes` or `:no`.

- Move through the list with the navigation keys shown by `?`.
- Press `Enter` or `d` to open read-only node details.
- Press `r` to refresh the resource snapshot.
- Press `Esc` or `q` to return from details.

## Filter nodes

Press `/`, type a query, and press `Enter`. Filtering changes only the visible rows; it does not modify the underlying snapshot or cluster.

Press `Esc` while editing a filter to clear it. Open `/` again to replace the current query.

## Upgrade a Talos node

Start t9s with `--enable-writes` (or `T9S_ENABLE_WRITES`) before using the upgrade action. On a selected node, press `U`, review the image suggestion, edit the target tag if needed, and complete the explicit confirmation.

The suggestion preserves the running node's Image Factory repository details: factory, installer flavor, and schematic ID. It uses the running Talos version as the initial tag so an upgrade prompt cannot silently downgrade a node whose declared image is stale. On the pinned Talos v1.13.3 SDK, live schematic discovery uses the installed `ExtensionStatus` resource named `schematic`: its version supplies the schematic ID and its author supplies the flavor/factory metadata. If that resource is unavailable or undecodable, t9s falls back to the declared image. Digest references remain unchanged.

For Talos versions supporting `LifecycleService`, the notice area streams image pull, install, Kubernetes drain, reboot, readiness wait, and uncordon. Byte progress is shown when totals are available. Success requires lifecycle exit code zero and completion of cleanup; interrupted streams and non-zero exits fail. Nodes outside the lifecycle API range use the legacy upgrade RPC.

A parseable target tag more than one minor ahead receives an advisory warning that intermediate Talos minor releases are skipped. Talos remains the authority for compatibility checks. This action upgrades Talos only; Kubernetes control-plane upgrades are separate.

## Kubernetes correlation

When the active Talos context's name exactly matches a context name in your kubeconfig, `t9s` automatically enriches each node with its corresponding Kubernetes Node: a `K8S` column (`Ready`/`NotReady`/`Unknown`) in the table, and a `KUBERNETES` block (roles, kubelet version, conditions) in node detail.

No configuration is required for the exact-name-match case. If the names don't match, the column reads `Unknown` for every row rather than being hidden — Kubernetes is optional enrichment and its absence never blocks Talos views. To associate contexts with different names, or when launching from k9s, see [k9s integration](/guides/k9s-integration/).
