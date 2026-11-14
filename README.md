# t9s

**Explore your Talos cluster from the terminal.**

`t9s` is a terminal UI for exploring and managing Talos Linux clusters.
Browse nodes, follow logs, investigate health problems, and work through
maintenance tasks without leaving the terminal.

[Documentation](https://t9s.m11s.io/) · [Installation](https://t9s.m11s.io/getting-started/installation/) · [Releases](https://github.com/m11s-io/t9s/releases)

## See what is happening

- **Explore your cluster.** Move between nodes, services, events, etcd,
  storage, and networking. Open a resource to see its details.
- **Investigate a problem.** Follow service and kernel logs, inspect processes
  and containers, and check CPU, memory, disks, and connections.
- **Understand cluster health.** See which resources need attention and the
  observations behind each diagnosis.
- **Go deeper when you need to.** Browse Talos resources directly, switch
  between cluster contexts, or open a node from k9s.

## Get started

Install with Homebrew on macOS or Linux:

```bash
brew install m11s-io/tap/t9s
```

Then open the cluster configured in your Talos configuration:

```bash
t9s
```

To choose a context or configuration file explicitly:

```bash
t9s --context <context>
t9s --talosconfig <path> --context <context>
```

Prefer a standalone binary? Download one from
[GitHub Releases](https://github.com/m11s-io/t9s/releases), then follow the
[installation guide](https://t9s.m11s.io/getting-started/installation/) for
checksum verification and setup.

## Find your way around

Use the arrow keys to select a row, `Enter` to open details, and `Esc` to go
back. Press `/` to filter, `?` for help, or `:` to open the command palette.

| Command | Explore |
| --- | --- |
| `:nodes` | Cluster nodes and their diagnostics |
| `:services` | Services, state, and logs |
| `:events` | Machine events and live follow |
| `:etcd` | Etcd members, status, and alarms |
| `:overview` | Cluster health at a glance |
| `:problems` | Health findings and supporting evidence |
| `:resources` | Talos resource types and instances |

The [command reference](https://t9s.m11s.io/reference/commands/) covers the
full command list and view-specific controls.

## Choose when to make changes

`t9s` starts in read-only mode. Maintenance actions are opt-in through
`--enable-writes` and require confirmation. These include node and service
operations, etcd maintenance, configuration applies, and upgrades.

Read the [security guide](https://t9s.m11s.io/security/) before enabling writes.
It explains permissions, safeguards, and how to handle interrupted operations.
Compatibility and live validation are documented in the
[contributor guide](https://t9s.m11s.io/contributing/#compatibility-and-manual-validation).

## Bring it into your workflow

Use one Talos configuration or combine several, and switch contexts from
inside the UI. See [Talos configuration](https://t9s.m11s.io/getting-started/talos-configuration/)
for setup.

Already using k9s? Add the [k9s plugin](https://t9s.m11s.io/guides/k9s-integration/)
to open the selected Kubernetes node in t9s.

## Contribute

Bug reports, documentation improvements, and code contributions are welcome.
Start with [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions and project
conventions, or [open an issue](https://github.com/m11s-io/t9s/issues).
For security concerns, use [private vulnerability reporting](https://github.com/m11s-io/t9s/security/advisories/new).

Licensed under [Apache 2.0](LICENSE).
