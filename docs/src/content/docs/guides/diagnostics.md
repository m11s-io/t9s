---
title: Node diagnostics
description: Inspect a node's kernel log, network sockets, mounts, and memory.
---

These read-only views open from a selected node in `:nodes` — none is a standalone top-level command. For a server-side, streaming cluster readiness check (not node-scoped), see [`:healthcheck`](/guides/health/#healthcheck). Each is scoped to the node the view was opened for, and `r` re-fetches that same node rather than whatever is currently selected back in `:nodes`. Press `Esc` or `q` to return to `:nodes`.

## Dmesg

Press `e` on a selected node to stream its kernel ring buffer (`dmesg`). New output arrives live as it is produced.

The view reuses the same stream controls as service logs:

| Key | Result |
| --- | --- |
| `/` | Filter the visible lines. |
| `s` | Pause or resume following. |
| `w` | Toggle line wrapping. |
| `C` | Clear the buffered lines. |
| `r` | Reconnect the stream. |

Dmesg output is bounded in memory: only the most recent 2000 lines are retained, so a busy node cannot grow the view without limit.

## Netstat

Press `s` on a selected node to list its network sockets: `PROTO`, `STATE`, `LOCAL`, `REMOTE`, and `PROCESS`. The list covers TCP, TCP6, UDP, and UDP6 sockets in the host network namespace in every state (listening and connected), matching what `ss -tunap` shows.

Press `/` to filter across protocol, state, local address, remote address, and process name. There is no detail page; the selected row carries all the surfaced fields.

## Mounts

Press `m` on a selected node to list its filesystem mounts: `FILESYSTEM`, `SIZE`, `USED`, `AVAIL`, `USE%`, and `MOUNTED`. `USED` is `SIZE - AVAIL`, and `USE%` is the used fraction of the mounted filesystem (reported as `0%` when the size is unknown rather than dividing by zero).

Press `/` to filter across filesystem and mount point. There is no detail page; the selected row carries all the surfaced fields.

## Memory

Press `f` on a selected node to show a curated subset of its `/proc/meminfo` as `METRIC` and `VALUE` rows: total, used (with the derived percentage of total), available, free, buffers, cached, swap total, swap free, dirty, and slab. Byte values are rendered with binary unit suffixes.

Press `/` to filter across the metric label and value. There is no detail page.

## Read-only guarantee

All four views are read-only. They never require `--enable-writes`, and enabling writes does not add any mutation to them.

## Live machine events

`:events` (`:ev`) opens a snapshot of recent machine events. Press `s` to start
following new events from the current cluster members, and press `s` again to
stop. `EVENTS FOLLOW` / `EVENTS PAUSED` shows the mode. Live following works in
the default read-only mode.

The snapshot stays visible when following starts. New events are appended in
receive order, and the view scrolls to the newest row. Only the most recent
2,000 event rows are retained while following. `/` still filters by node, kind,
or message; typing `s` in a filter does not change the follow mode.

A failed or ended node stream appears as a filterable `StreamError` row, and
other members continue streaming. Failures are summarized in the mode line.
Stop and restart follow with `s` to retry connections and rediscover membership.
There is no automatic reconnect or historical replay: events produced while
paused or disconnected may be missed. Use `r` while paused for a fresh recent
event snapshot; `r` has no effect during live following.

Leaving the events view, selecting another context, or quitting cancels its
streams. Returning to `:events` starts in snapshot mode again. A membership
change is picked up the next time following starts.
