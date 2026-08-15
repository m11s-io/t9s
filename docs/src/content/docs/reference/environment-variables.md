---
title: Environment variables
description: Environment variables used by t3s.
---

## `TALOSCONFIG`

The standard Talos variable selects one configuration file:

```bash
export TALOSCONFIG="$HOME/.talos/config"
```

Explicit `--talosconfig` arguments take part in file selection according to the CLI invocation.

## `TALOSCONFIGS`

The t3s-specific variable selects multiple files as an operating-system path list:

```bash
export TALOSCONFIGS="$HOME/.talos/mgmt:$HOME/.talos/stage:$HOME/.talos/test"
```

Use `:` as the separator on Unix and `;` on Windows. Repeated `--talosconfig` flags provide the equivalent explicit form. Duplicate context names are rejected.
