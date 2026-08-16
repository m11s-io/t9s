---
title: Commands
description: Commands available from the t9s command palette.
---

Press `:` to open the command palette.

| Command | Alias | Result |
| --- | --- | --- |
| `:nodes` | `:no` | Open the node explorer. |
| `:services` | `:svc` | Open Talos services. |
| `:contexts` | `:ctx` | Select another discovered context. |
| `:events` | `:ev` | Open the machine event view. |
| `:etcd` | `:et` | Open etcd membership and status. |
| `:overview` | `:ov` | Cluster-wide health summary. See [Health](/guides/health/). |
| `:problems` | | Drillable list of everything currently unhealthy. See [Health](/guides/health/). |
| `:resources` | `:res` | Generic Talos resource browser. `:resources <Kind>` (e.g. `:resources MachineStatus`) jumps straight to that kind's instances. See [Resource browser](/guides/resources/). |

Unknown commands produce a notice and do not execute a shell command. The palette has no arbitrary execution path.

## Node-scoped keys

These open from a selected row in `:nodes` rather than through the command palette:

| Key | Result |
| --- | --- |
| `Enter` / `d` | Open node detail. |
| `p` | Open processes. See [Processes, disks, and network](/guides/processes-disks-network/). |
| `k` | Open disks. |
| `n` | Open network interfaces, addresses, and routes. |

Each of these views supports `r` to refresh and `Esc`/`q` to return to `:nodes`.
