---
title: Processes, disks, and network
description: Inspect a node's processes, disk devices, and network interfaces.
---

These three views open from a selected node in `:nodes` — none is a standalone top-level command. Each follows the same pattern: press a key, get a read-only list scoped to that node, press `Enter` or `d` for full detail, `r` to refresh, `Esc` or `q` to return to `:nodes`.

## Processes

Press `p` on a selected node to open its process list: `PID`, `STATE`, `CPU`, `MEM`, `COMMAND`. Select a row and press `Enter` or `d` to see every field, including the full command line and arguments (truncated in the table but shown in full in detail).

## Disks

Press `k` on a selected node (`d` already opens node detail) to open its disk device list: `DEVICE`, `TYPE`, `SIZE`, `MODEL`, `SYSTEM`. Detail adds `Serial`, `BusPath`, and `ReadOnly`. Only the size, usage, and health facts Talos exposes directly are shown — `t9s` does not infer filesystem or mount information beyond that.

### Wipe a block device

With `--enable-writes`, press `W` on a selected disk row to wipe that single device with Talos `BlockDeviceWipe`. A text prompt asks you to type the device path (for example `/dev/sdb`); `t9s` accepts either `/dev/sdb` or `sdb`. `Enter` then opens the usual `(y/n)` confirm, and `Esc` cancels.

The default wipe method is `FAST` (erase signatures, filesystem, and partition metadata without zeroing); the slower `ZEROES` method is not exposed in this version. The system disk and read-only devices (CD-ROMs, ISOs) are refused — `t9s` reports the refusal instead of opening the prompt. The wipe is verified against the currently loaded disk inventory, so an unknown or not-yet-loaded inventory is also refused. The request is sent without skipping the server's volume checks, so a device still in use by a volume is rejected by the node and the error is shown. On success the disk list refreshes.

## Network

Press `n` on a selected node to open its network interface list: `LINK`, `TYPE`, `STATE`, `MTU`, `ADDRESSES`. Detail adds every assigned route for the selected link, alongside its full address list.

## Refreshing

`r` re-fetches the node the view was opened for — not whatever is currently selected back in `:nodes` — so refreshing after navigating elsewhere still targets the node you're actually looking at.
