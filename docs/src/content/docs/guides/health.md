---
title: Health
description: Explainable cluster health with :overview and :problems.
---

`:overview`/`:ov` and `:problems` are projections over the same node, service, and etcd snapshots the rest of `t9s` already shows — they never make their own API calls, and evaluating them never changes what `:nodes`/`:services`/`:etcd` display.

Diagnoses are deterministic and cite evidence: each one has a severity, a short summary, the resource it's about, and a stable rule identity, so "unhealthy" is always traceable back to why.

## `:overview`

A compact per-resource-kind breakdown — for example `NODES: 2/3 healthy, 1 warning, 0 critical` — plus the top critical diagnoses inline as a preview. Read-only, no per-row selection; use `:problems` to drill into individual issues.

## `:problems`

A flat, filterable table of every current diagnosis: `SEVERITY`, `KIND`, `RESOURCE`, `SUMMARY`. Press `/` to filter like any other table.

Press `Enter` or `d` on a row to drill into the underlying resource:

- A node diagnosis opens that node's detail view.
- An etcd member diagnosis opens the etcd list (etcd has no per-member detail yet, so this lands you on the list rather than failing to drill in at all).

`r` refreshes the underlying data; the health evaluation re-runs against the refreshed snapshot on the next render.

## Missing or stale data

Missing, stale, or contradictory evidence produces `unknown` — it never silently reads as healthy. Objective resource exploration in `:nodes`/`:services`/`:etcd` stays fully useful even if health evaluation is incomplete.
