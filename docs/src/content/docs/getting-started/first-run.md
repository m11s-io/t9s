---
title: First run
description: Open t3s and navigate the initial node view.
---

Start with a known context:

```bash
go run ./cmd/t3s --context mgmt
```

`t3s` opens the read-only node view. Use the arrow keys or familiar terminal navigation keys to select a row, then press `Enter` or `d` for details. Press `Esc` or `q` to return.

Press `:` to open the command palette. The primary commands are `:nodes`, `:services`, and `:contexts`. Short aliases include `:no`, `:svc`, and `:ctx`.

Press `?` to see the controls available in the current view and `Ctrl+C` to exit.

:::note[Current scope]
The project is an early foundation. Processes, disks, network, and other planned resource views are not available yet.
:::
