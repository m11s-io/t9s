---
title: First run
description: Open t9s and navigate the initial node view.
---

Start with a known context:

```bash
go run ./cmd/t9s --context mgmt
```

`t9s` opens the read-only node view. Use the arrow keys or familiar terminal navigation keys to select a row, then press `Enter` or `d` for details. Press `Esc` or `q` to return.

Press `:` to open the command palette. See [Commands](/reference/commands/) for the full list, including `:overview`, `:problems`, and `:resources`.

On a selected node in `:nodes`, press `p` for processes, `k` for disks, or `n` for network — see [Processes, disks, and network](/guides/processes-disks-network/).

Press `?` to see the controls available in the current view and `Ctrl+C` to exit.
