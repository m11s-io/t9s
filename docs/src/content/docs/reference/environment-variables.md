---
title: Environment variables
description: Environment variables used by t9s.
---

## `TALOSCONFIG`

The standard Talos variable selects one configuration file:

```bash
export TALOSCONFIG="$HOME/.talos/config"
```

Explicit `--talosconfig` arguments take part in file selection according to the CLI invocation.

## `TALOSCONFIGS`

The t9s-specific variable selects multiple files as an operating-system path list:

```bash
export TALOSCONFIGS="$HOME/.talos/mgmt:$HOME/.talos/stage:$HOME/.talos/test"
```

Use `:` as the separator on Unix and `;` on Windows. Repeated `--talosconfig` flags provide the equivalent explicit form. Duplicate context names are rejected.

## `T9S_ENABLE_WRITES`

Enables gated node reboot and shutdown from `:nodes` (equivalent to passing `--enable-writes`). Read-only by default:

```bash
export T9S_ENABLE_WRITES=true
```

Parsed as a standard boolean string (`1`, `t`, `T`, `TRUE`, `true`, `True` enable; `0`, `f`, `F`, `FALSE`, `false`, `False` and any unset or unparseable value leave writes disabled). See [Security](/security/) for what the write path allows.
