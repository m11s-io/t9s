---
title: Services and logs
description: Inspect Talos services and follow their logs.
---

Open services with `:services` or `:svc`. The list is read-only and scoped to the active context.

- Press `Enter` or `d` for service details.
- Press `l` on a service to open its streaming logs.
- Press `r` to refresh services or restart the current log stream.
- Press `/` to filter visible services or log lines.
- Press `Esc` or `q` to move back through details and logs.

## Log controls

Logs begin in follow mode. Use:

- `s` to pause or resume following.
- `w` to toggle line wrapping.
- `C` to clear displayed lines.
- `g` to jump to the top.
- `G` to jump to the bottom and resume following.

Incoming batches preserve a user pause. The view does not invoke arbitrary commands on the node.
