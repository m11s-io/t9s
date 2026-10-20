---
title: Node diagnostics
description: Inspect a node's kernel log, network sockets, mounts, memory, time, CPU, containers, images, and directory usage.
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

Press `/` to filter across filesystem and mount point. Press `u` to inspect
directory usage under the selected mount point.

Directory usage scans one absolute mount path to depth one and reports directory
sizes and per-entry errors. It stops after 5,000 rows or 30 seconds, preserving
received rows and marking incomplete scans. Cancel it by leaving the view or
changing context. The row/time limits bound t9s output and connection lifetime;
they do not bound the node's traversal work.

## Memory

Press `f` on a selected node to show a curated subset of its `/proc/meminfo` as `METRIC` and `VALUE` rows: total, used (with the derived percentage of total), available, free, buffers, cached, swap total, swap free, dirty, and slab. Byte values are rendered with binary unit suffixes.

Press `/` to filter across the metric label and value. There is no detail page.

## Containers and images

Press `c` on a selected node to inspect containers in the `system` namespace.
The view includes status, image, PID, memory, and cumulative CPU TIME. Missing
statistics display as unknown; a statistics failure keeps container rows and
marks the result partial.

Press `n` to cycle `system` → `cri` → `taloscontainers`. The `cri` scope uses
Talos' `k8s.io` runtime namespace and CRI driver. Press `i` to switch between
containers and images in the current namespace, `/` to filter, `d` or `Enter`
to inspect a container, and `r` to refresh the captured node and scope. Image
listing is capped at 5,000 rows or 30 seconds; partial rows remain visible on
stream errors and the view reports when a cap is reached. Leaving the view or
changing namespace/mode cancels the old request. This view is read-only.

## Time

Press `t` on a selected node to show its synchronization status, configured
server, node/server UTC samples, and **sampled difference (server - node)**.
For example, a server sample 250 ms later than the node sample is `+250ms`.
This sampled difference is not an authoritative NTP offset or a health verdict;
the synchronization status comes separately from the node's time status resource.
Missing or invalid timestamps show `unknown`. A failed configured-server
measurement leaves the synchronization status visible with a problem message.

| Key | Result |
| --- | --- |
| `r` | Refresh normal status and configured-server measurements. |
| `/` | Filter status, samples, problems, and check results. |
| `c` | Open the explicit server-check prompt. |
| `Enter` | Check the entered server. |
| `Esc` | Cancel the prompt, cancel filter entry, or leave the view. |
| `j` / `k` | Scroll when the result exceeds the available height. |

The prompt accepts a trimmed hostname or bare IP address, including IPv6.
It rejects empty values, whitespace/control characters within a target, URL
schemes, paths, and ports. A check appears in its own **SERVER CHECK** panel;
it never replaces normal node status. Refreshing normal status does not repeat
an explicit server check. Talos enforces permissions for `TimeCheck`: a denied
check displays `time check permission denied`, without retrying another server.
No arbitrary-server checks happen automatically.

Leaving the view, changing context, or quitting cancels pending time RPCs.
Opening and canceling the context picker keeps the current view. Superseded
responses cannot replace newer status or check results. Requests have a
10-second deadline.

## CPU utilization and process counts

Press `u` on a selected node to sample its aggregate and per-core CPU busy and
I/O-wait percentages. Each request reads the node's system counters twice,
with a cancellable one-second wait between reads. The view shows the observed
UTC sample time, measured interval, and latest valid running/blocked process
counts. The timestamp comes from the client's observation of the sample;
RPC latency can make the measured interval longer than one second.

The total counter is `User + Nice + System + Idle + Iowait + Irq + SoftIrq + Steal`.
Guest and GuestNice are already included in User/Nice, so they are not added
again. Busy excludes both idle and I/O wait. For example, a total delta of 100,
idle delta of 60, and I/O-wait delta of 10 produces **30% busy** and **10% I/O wait**.
These percentages describe the sampled interval, not lifetime process CPU time.

Missing, reset, decreasing, zero-delta, or nonfinite CPU counters display
`unknown`. A changed boot time, changed CPU topology, or a nonpositive sample
interval invalidates utilization for that request. Sparse CPU indices keep their
original IDs and show unknown for missing cores. Partial failures retain valid
process counts from the latest successful read with that read's timestamp;
they do not report a complete utilization measurement.

| Key | Result |
| --- | --- |
| `r` | Take a new two-read sample of the same node. |
| `/` | Filter CPU rows, counts, sample information, and problems. |
| `j` / `k`, `g` / `G` | Scroll by a row or go to the top/bottom. |
| `q` / `Esc` | Return to Nodes; Esc first cancels active filter entry. |

The view samples when opened; press `r` to take another sample. There is no
background polling. A refresh supersedes and cancels an older sample; leaving
the view, changing context, or quitting cancels pending reads and the wait. Help
preserves the underlying CPU view and request. Each read has a 15-second deadline.
CPU sampling works without `--enable-writes`.

## Read-only guarantee

All diagnostic views are read-only. They never require `--enable-writes`, and enabling writes does not add any mutation to them.

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
