---
title: Resource browser
description: Browse any Talos COSI resource type, not just the curated views.
---

`:resources`/`:res` is a power-user escape hatch: every curated view (`:nodes`, `:services`, `:etcd`, and the node-scoped processes/disks/network) is a hand-picked projection of a handful of Talos APIs. Talos exposes dozens of COSI resource types; the resource browser reaches the rest, read-only.

## Browsing

`:resources` on its own opens a filterable list of registered resource kinds — their display type, default namespace, and aliases. Some kinds are sensitive; they're shown but visually marked, never silently hidden.

`:resources <Kind>` jumps straight to that kind's instances if the name or an alias resolves:

```text
:resources MachineStatus
```

An unrecognized name doesn't error — it lands on the kind list with your text pre-filled as the filter, so a typo degrades to "help me find it."

## Instances and detail

Press `Enter` or `d` on a kind to list its instances (`NAMESPACE`, `ID`, `PHASE`). If a kind requires a node scope Talos hasn't been given, the view prompts for a node instead of showing a raw error.

Press `Enter` or `d` on an instance to see its full structured detail, rendered as YAML in a scrollable view.

`Esc`/`q` pops back one level at a time: detail → instances → kinds → out to `:nodes`. `r` refreshes whichever screen is active.

## Read-only

The resource browser is strictly a read path. It never creates, updates, or deletes anything, regardless of what the underlying Talos resource type otherwise supports.
