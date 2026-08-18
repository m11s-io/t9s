# Talos upgrade recovery outcome design

## Problem

After a Talos install and reboot, a control-plane node can take longer to
return to Kubernetes than the current readiness/cleanup sequence expects. The
node may eventually become healthy, while t9s reports `upgrade failed` using
the last visible phase (`uncordoning`). This conflates installer failure with
post-reboot recovery and can leave operators with a misleading result.

## Goal

Represent a successful Talos image application whose Kubernetes recovery is
still pending as a distinct terminal outcome: `AppliedWithRecoveryWarning`.
Installer/API failures and cancellation remain hard, distinct failures.

## State flow

```text
checking -> pulling/installing -> rebooting -> recovering
                                             |-- applied successfully
                                             |-- applied with recovery warning
```

Recovery requires a fresh post-upgrade boot identity/heartbeat, Talos API
readiness, Kubernetes `Ready=True`, and successful uncordon. A stale
`Ready=True` from before reboot is insufficient. Recovery uses a bounded
15-minute deadline and retries uncordon with fresh node reads.

## Component contracts

- `internal/ports`: add an explicit upgrade outcome/warning representation;
  `Err` remains reserved for installer/API/cancellation failures.
- `internal/adapters/talos`: own boot identity capture and recovery state
  machine. Emit progress plus the recovery warning without converting a
  completed install into `Err`.
- `internal/application`: map applied-with-warning to a completed action with
  a persistent warning; do not enter `UpgradeFailed`.
- `internal/tui`: render `Applied — recovery warning` distinctly and sanitize
  the warning text.

## Safety rules

- Recovery cannot complete from a stale Kubernetes readiness observation.
- While recovery is pending, no additional node write action is enabled.
- Uncordon retries are bounded by the recovery deadline.
- Actual lifecycle install/API errors remain hard failures.
- Cancellation remains distinct from both success outcomes.
- Warning text is single-line, bounded, and contains no raw RPC/config data.

## Verification

Add tests for stale versus fresh boot identity, delayed readiness, recovery
deadline warning, uncordon retries, typed application mapping, and TUI warning
rendering. Run the full Go suite and race tests for adapter, application, and
TUI packages before completion.
