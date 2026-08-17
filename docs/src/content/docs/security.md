---
title: Security
description: Protect Talos credentials and understand the gated write path.
---

By default the t9s UI is read-only and offers no mutation operation and no
arbitrary command path. Passing `--enable-writes` (or setting
`T9S_ENABLE_WRITES`) additionally allows reboot, shutdown, rollback, and
upgrade of selected node(s) from the `:nodes` screen (`space` to mark rows,
`R` to reboot, `X` to shut down, `B` to roll back to the previous Talos OS
install, `U` to upgrade to a specified Talos OS image), as well as service
start/stop/restart from the `:services` screen (`S` to start, `T` to stop,
`R` to restart), each gated behind an inline confirmation prompt that flags
control-plane and etcd-quorum risk before it runs. The header's `[RO]`/`[RW]`
badge always reflects whether writes are active for the current session.

Talos upgrade is behind the same `--enable-writes` gate and explicit confirmation. The selected node is drained before reboot and uncordoned after readiness, including cleanup after later-stage failure. Progress and errors are normalized; credentials, talosconfig contents, kubeconfig contents, and registry tokens are not stored in model state or logs. Kubernetes control-plane upgrades are not part of this action.

The supplied Talos credentials can still be privileged. Protect every talosconfig as a sensitive secret and grant only the permissions an operator needs.

- Never commit a real talosconfig, certificate, private key, token, endpoint, or internal hostname.
- Prefer narrowly scoped Talos roles where the cluster policy allows them.
- Keep configuration files readable only by the intended local user.
- Use invented identities, reserved addresses, and fake credentials in tests and examples.

Report security issues through the private security-reporting channel configured on the GitHub repository rather than a public issue.
