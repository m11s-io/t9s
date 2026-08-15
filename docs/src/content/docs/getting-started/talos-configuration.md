---
title: Talos configuration
description: Select one or more Talos configuration files and contexts.
---

`t3s` uses the Talos client configuration format. Treat every talosconfig as a sensitive credential.

## One configuration file

Use the standard `TALOSCONFIG` variable:

```bash
export TALOSCONFIG="$HOME/.talos/config"
go run ./cmd/t3s --context mgmt
```

Or select the file explicitly:

```bash
go run ./cmd/t3s --talosconfig "$HOME/.talos/mgmt" --context mgmt
```

## Multiple configuration files

Repeat `--talosconfig`:

```bash
go run ./cmd/t3s \
  --talosconfig "$HOME/.talos/mgmt" \
  --talosconfig "$HOME/.talos/stage" \
  --talosconfig "$HOME/.talos/test"
```

Alternatively, set the t3s-specific `TALOSCONFIGS` path list. Use `:` on Unix and `;` on Windows.

```bash
export TALOSCONFIGS="$HOME/.talos/mgmt:$HOME/.talos/stage:$HOME/.talos/test"
go run ./cmd/t3s
```

`t3s` merges contexts in memory and rejects duplicate context names. `TALOSCONFIG` retains its standard singular behavior.
