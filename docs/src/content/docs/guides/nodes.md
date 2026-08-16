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

## Kubernetes correlation

When the active Talos context's name exactly matches a context name in your kubeconfig, `t9s` automatically enriches each node with its corresponding Kubernetes Node: a `K8S` column (`Ready`/`NotReady`/`Unknown`) in the table, and a `KUBERNETES` block (roles, kubelet version, conditions) in node detail.

No configuration is required for the exact-name-match case. If the names don't match, the column reads `Unknown` for every row rather than being hidden — Kubernetes is optional enrichment and its absence never blocks Talos views. To associate contexts with different names, or when launching from k9s, see [k9s integration](/guides/k9s-integration/).
