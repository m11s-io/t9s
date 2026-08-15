---
title: Contexts
description: Discover and switch between Talos cluster contexts.
---

Open the context selector with either command:

```text
:contexts
:ctx
```

Select a context and press `Enter`. The new context replaces the current root view and refreshes its resource data. Context-scoped state from the previous session is not shown after the switch.

When multiple Talos configuration files are supplied, all unique contexts appear in the selector. Duplicate names are rejected during startup so switching remains unambiguous.

Press `Esc` to close the selector without changing context.
