---
title: Node diagnostics
description: Inspect a node's kernel log and network sockets.
---

These read-only views open from a selected node in `:nodes` — neither is a standalone top-level command. Both are scoped to the node the view was opened for, and `r` re-fetches that same node rather than whatever is currently selected back in `:nodes`. Press `Esc` or `q` to return to `:nodes`.

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

## Read-only guarantee

Both views are read-only. They never require `--enable-writes`, and enabling writes does not add any mutation to them.
