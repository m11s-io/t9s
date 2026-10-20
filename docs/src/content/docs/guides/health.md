---
title: Health
description: Explainable cluster health with :overview and :problems.
---

`:overview`/`:ov` and `:problems` are projections over the same node, service, etcd, and node-scoped Talos status snapshots the rest of `t9s` already shows — they never make their own API calls, and evaluating them never changes the underlying observations.

Diagnoses are deterministic and cite evidence: each one has a severity, a short summary, the resource it's about, and a stable rule identity, so "unhealthy" is always traceable back to why.

Service health summaries distinguish explicitly unhealthy services from services whose health is unknown. Unknown service health remains visible in the node service counters but does not by itself mark the cluster degraded; only an explicit unhealthy service contributes a `node-services-degraded` diagnosis. If the service list or node observations cannot be loaded, the existing unavailable/unknown data handling still applies.

Active etcd alarms (`NOSPACE`, `CORRUPT`, and future alarm types) are treated as critical `etcd-member-alarmed` diagnoses — a `NOSPACE` alarm makes the cluster read-only until it is cleared — and are also shown in the `:etcd` view's `ALARMS` column. From `:etcd`, `A` disarms the selected member's alarms behind `--enable-writes`; disarming lifts the read-only restriction but does not reclaim disk, so free space first or the alarm returns. A `CORRUPT` alarm still requires restoring from a snapshot — disarming does not repair corruption. See [Etcd operations](/guides/etcd-ops/).

The overview also rolls up BGP, storage, volume maintenance, and system status.
Non-established BGP sessions warn; unknown peer state remains unknown. Missing
LVM physical volumes, partial volume groups, failed MD arrays, failed Talos
volumes, unsuccessful filesystem scrubs, and unhealthy `sandboxd` services are
reported from explicit status evidence. Rebuilding MD arrays and waiting volumes
warn while Talos reports those transitions. Volume location, provisioning, and
preparation phases also warn; unknown and closed phases remain unknown. A trim
schedule that is past its next scheduled time warns. Workload isolation is shown
as enabled, disabled, or unknown; disabled is not treated as unhealthy because
older clusters may have that policy intentionally.

Use `:bgp`/`:bgps` to inspect peer sessions. Node detail separates workload
isolation configuration from `sandboxd` service health and lists system volumes.
Disk detail shows matching LVM, MD RAID, volume, trim, and scrub evidence.

## `:overview`

A compact per-resource-kind breakdown — for example `NODES: 2/3 healthy, 1 warning, 0 critical` — plus the top critical diagnoses inline as a preview. Read-only, no per-row selection; use `:problems` to drill into individual issues.

## `:problems`

A flat, filterable table of every current diagnosis: `SEVERITY`, `KIND`, `RESOURCE`, `SUMMARY`. Press `/` to filter like any other table.

Press `Enter` or `d` on a row to drill into the underlying resource:

- A node diagnosis opens that node's detail view.
- An etcd member diagnosis opens the etcd list (etcd has no per-member detail yet, so this lands you on the list rather than failing to drill in at all).

`r` refreshes the underlying data; the health evaluation re-runs against the refreshed snapshot on the next render.

## `:healthcheck`

`:healthcheck`/`:hc` streams the Talos server-side cluster health check. Unlike `:overview`/`:problems`, it is a **live RPC**: it opens a stream to the current Talos endpoint, waits for the cluster to converge, and prints the server's progress lines (`discovered nodes: …`, `waiting for …`) as they arrive. A clean end of stream renders `✔ cluster is healthy`; a failure renders `✘ cluster health check unavailable`. Raw gRPC error text is never shown.

- The session endpoint must itself be a **control-plane node** — the server rejects a worker endpoint.
- The optional wait timeout defaults to **60s**; override it with a Go duration, e.g. `:healthcheck 5m`. A longer timeout is appropriate for a cluster you expect to become ready. An invalid duration is treated as an unknown command.
- `r` reconnects, rebuilding the node lists from the current `:nodes` snapshot. A running check reflects the node set captured when it started; it does not follow live membership changes.
- Keys: `/` filter, `C` clear, `r` reconnect, `q`/`Esc` back.

Only IP addresses are sent to the server (the server rejects hostnames). If a list is empty the header shows `?` for it; the server falls back to its own discovery only when **both** the control-plane and worker lists are empty. A worker-only or worker endpoint still fails server-side.

`:healthcheck` is an explicit, additional signal: it never changes `:overview` or `:problems`, and its verdict lives only in this view. Missing or unknown data never becomes "healthy".

## Missing or stale data

Missing, stale, or contradictory evidence produces `unknown` — it never silently reads as healthy. Objective resource exploration in `:nodes`/`:services`/`:etcd` stays fully useful even if health evaluation is incomplete.
