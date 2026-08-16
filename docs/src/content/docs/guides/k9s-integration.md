---
title: k9s integration
description: Launch t9s directly from k9s's Node view.
---

`t9s` is a standalone binary, not a k9s fork — it doesn't link to k9s libraries. The integration is a thin launcher: a copyable k9s plugin definition that shells out to `t9s` with a Kubernetes context and node as hints.

## Add the plugin

Add this to k9s's `plugins.yaml` to open the selected Kubernetes Node in `t9s` with `Shift-T` from k9s's Node view:

```yaml
plugins:
  t9s:
    shortCut: Shift-T
    description: Open in t9s
    scopes:
      - nodes
    command: t9s
    background: false
    args:
      - --kube-context
      - $CONTEXT
      - --node
      - $NAME
```

`$CONTEXT` and `$NAME` are k9s's own plugin placeholder variables — k9s substitutes them with its active Kubernetes context and the selected Node's name before invoking `t9s`.

## How the Talos context is resolved

`t9s` resolves `--kube-context` to a Talos context using two rules, in order:

1. **Explicit mapping**, from `$XDG_CONFIG_HOME/t9s/config.yaml` (falling back to `~/.config/t9s/config.yaml`):

   ```yaml
   kubernetesAssociations:
     - kubernetesContext: prod-eu
       talosContext: prod
   ```

   This file is optional and hand-edited — `t9s` never writes to it. A missing file is not an error.

2. **Exact name match** — if no explicit mapping exists, `t9s` checks whether any discovered Talos context name exactly matches the Kubernetes context name.

If neither resolves, `t9s` opens an interactive context picker instead of guessing. Once resolved, `t9s` opens directly on the selected node's detail view.

## Manual invocation

The same flags work without k9s:

```bash
t9s --kube-context prod-eu --node worker-3
```

## Security

Inspect the plugin YAML before adding it — `t9s` documents the exact arguments it's invoked with, and the launch path never grants k9s (or `t9s`) any capability beyond what your existing talosconfig and kubeconfig already provide.
