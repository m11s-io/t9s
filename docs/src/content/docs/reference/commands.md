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
| `space` | Mark/unmark the selected node for a bulk action. Requires `--enable-writes`. |
| `R` | Reboot the marked node(s) (or the selected node if none are marked), behind a confirm prompt. Requires `--enable-writes`. |
| `X` | Shut down the marked node(s) (or the selected node if none are marked), behind a confirm prompt. Requires `--enable-writes`. |
| `B` | Roll back the marked node(s) (or the selected node if none are marked) to the previous Talos OS install, behind a confirm prompt. Requires `--enable-writes`. |
| `U` | Upgrade the selected node to a specified Talos OS image, behind a prompt prefilled with the node's current install image and a confirm step. Requires `--enable-writes`. |

Each of these views supports `r` to refresh and `Esc`/`q` to return to `:nodes`.

`space`, `R`, `X`, `B`, and `U` are inert unless `t9s` was started with `--enable-writes` (or `T9S_ENABLE_WRITES`); see [Security](/security/).

## Service-scoped keys

These act on the selected row in `:services`:

| Key | Result |
| --- | --- |
| `S` | Start the selected service, behind a confirm prompt. Requires `--enable-writes`. |
| `T` | Stop the selected service, behind a confirm prompt. Requires `--enable-writes`. |
| `R` | Restart the selected service, behind a confirm prompt. Requires `--enable-writes`. |

`S`, `T`, and `R` are inert unless `t9s` was started with `--enable-writes` (or `T9S_ENABLE_WRITES`); see [Security](/security/).
