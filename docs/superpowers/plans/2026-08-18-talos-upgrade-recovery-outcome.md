# Talos Upgrade Recovery Outcome Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Distinguish a successfully applied Talos upgrade from delayed Kubernetes recovery, using a persistent recovery warning instead of a false upgrade failure.

**Architecture:** The Talos adapter records pre-upgrade boot identity, performs a bounded recovery state machine, and emits a typed applied-with-warning terminal result when recovery times out. Application state maps that outcome separately from `UpgradeFailed`; the TUI renders a sanitized persistent warning.

**Tech Stack:** Go, Talos machinery client, Kubernetes client-go, Bubble Tea v2, testify.

**Spec:** `docs/superpowers/specs/2026-08-18-talos-upgrade-recovery-outcome-design.md`

## Global Constraints

- Recovery uses a bounded 15-minute deadline.
- A stale `Ready=True` observation from before reboot cannot complete recovery.
- `Err` remains reserved for installer/API/cancellation failures.
- Uncordon retries use fresh Kubernetes node reads until the recovery deadline.
- Warning text is single-line, bounded, sanitized, and contains no raw RPC/config data.
- No additional node write action is enabled while recovery is pending.

---

### Task 1: Define the typed upgrade outcome

**Files:**
- Modify: `internal/ports/node.go`
- Test: `internal/ports/node_upgrade_test.go`

**Interfaces:**
- Produce `UpgradeOutcome` with values `UpgradeOutcomeApplied` and `UpgradeOutcomeAppliedWithRecoveryWarning`.
- Add `Outcome UpgradeOutcome` and `Warning string` to `ports.UpgradeResult` while retaining `Err error` for hard failures.

- [ ] **Step 1: Write failing port tests**

Add tests that construct applied and applied-with-warning results and assert the outcome and warning survive unchanged.

- [ ] **Step 2: Run the focused tests**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/ports -run UpgradeOutcome -count=1`.
Expected: FAIL because the outcome type and fields do not exist.

- [ ] **Step 3: Implement the minimal port contract**

Add the enum and fields without changing existing result consumers; zero outcome remains backward-compatible for existing tests.

- [ ] **Step 4: Run focused and package tests**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/ports` and confirm PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ports/node.go internal/ports/node_upgrade_test.go
git commit -m "feat: model Talos recovery warning outcomes"
```

### Task 2: Make adapter recovery fresh, bounded, and typed

**Files:**
- Modify: `internal/adapters/talos/node_controller.go`
- Test: `internal/adapters/talos/node_controller_test.go`

**Interfaces:**
- `machineryUpgradeMaintenance` stores `preUpgradeBootID string` captured before cordon/reboot.
- `WaitKubernetesReady` requires `Ready=True` and a changed non-empty boot ID when a pre-upgrade boot ID was captured.
- Successful lifecycle install plus recovery deadline produces `ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAppliedWithRecoveryWarning, Warning: "Talos upgrade applied; node recovery is still pending; node may remain cordoned.", Done: true}`.

- [ ] **Step 1: Add failing adapter tests**

Add tests for: unchanged boot ID not being accepted as recovered; changed boot ID plus Ready being accepted; delayed uncordon retry; and a recovery deadline returning a warning outcome with `Err == nil`.

- [ ] **Step 2: Run the focused adapter tests**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/adapters/talos -run 'Boot|Recovery|Uncordon' -count=1`.
Expected: FAIL because current maintenance has no boot identity or typed warning path.

- [ ] **Step 3: Capture identity and implement recovery**

Fetch the node in `prepareUpgradeMaintenance`, store `status.nodeInfo.bootID`, use a 15-minute readiness deadline, require a fresh boot ID/heartbeat, and keep uncordon retries bounded by the same cleanup context. Emit progress phase `recovering` before the checks.

- [ ] **Step 4: Emit typed terminal outcomes**

On full recovery emit `UpgradeOutcomeApplied`. If Talos install succeeded but recovery reaches its deadline, emit `UpgradeOutcomeAppliedWithRecoveryWarning` and the fixed safe warning text; retain hard installer/API/cancellation errors in `Err`.

- [ ] **Step 5: Run adapter tests and race tests**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/adapters/talos` and `env GOMAXPROCS=2 go test -p 1 -race ./internal/adapters/talos`.

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/talos/node_controller.go internal/adapters/talos/node_controller_test.go
git commit -m "feat: track Talos recovery outcomes"
```

### Task 3: Map recovery warnings through application state

**Files:**
- Modify: `internal/application/actions.go`, `internal/application/messages.go`, `internal/application/update.go`
- Test: `internal/application/upgrade_test.go`, `internal/application/update_test.go`

**Interfaces:**
- Add `UpgradeAppliedWithRecoveryWarning` application message carrying generation, target, and warning.
- `UpgradeSucceeded` remains the normal completed path; warning completion finishes the active upgrade without populating `Upgrade.Err`.
- `ActionResult` stores the warning separately from its error field.

- [ ] **Step 1: Add failing application tests**

Feed an applied-with-warning port result through the stream bridge and assert the model becomes inactive, retains the target, leaves `Upgrade.Err` empty, and stores the warning in the action result.

- [ ] **Step 2: Run focused application tests**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/application -run 'Upgrade.*Warning|RecoveryWarning' -count=1`.
Expected: FAIL because the bridge currently maps every non-empty terminal result to success or `UpgradeFailed`.

- [ ] **Step 3: Implement message/effect mapping**

Map `ports.UpgradeOutcomeAppliedWithRecoveryWarning` to the warning message, preserve stale-generation guards, and keep write gating closed until the terminal message is processed.

- [ ] **Step 4: Verify application behavior**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/application` and `env GOMAXPROCS=2 go test -p 1 -race ./internal/application`.

- [ ] **Step 5: Commit**

```bash
git add internal/application/actions.go internal/application/messages.go internal/application/update.go internal/application/upgrade_test.go internal/application/update_test.go
git commit -m "feat: preserve Talos recovery warnings in application state"
```

### Task 4: Render and verify the recovery warning in the TUI

**Files:**
- Modify: `internal/tui/footer.go`, `internal/tui/table_layout.go` or the existing upgrade-notice renderer
- Test: `internal/tui/footer_test.go`, `internal/tui/action_prompt_test.go`

**Interfaces:**
- Render normal completion and `Applied — recovery warning` as separate notices.
- Apply the existing `sanitizeUntrustedText` and bounded single-line rendering to warning text.

- [ ] **Step 1: Add failing TUI tests**

Assert that a recovery warning is visible after the upgrade ends, includes the target, excludes ANSI/newline control characters, and does not render as `upgrade failed`.

- [ ] **Step 2: Run focused TUI tests**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/tui -run 'RecoveryWarning|UpgradeNotice' -count=1`.
Expected: FAIL because the current footer only knows success/failure text.

- [ ] **Step 3: Implement sanitized warning rendering**

Render the warning through `sanitizeUntrustedText`, preserve the target and recovery phase, and keep progress bounded.

- [ ] **Step 4: Run TUI tests and race tests**

Run `env GOMAXPROCS=2 go test -p 1 ./internal/tui` and `env GOMAXPROCS=2 go test -p 1 -race ./internal/tui`.

- [ ] **Step 5: Run the complete verification suite**

Run `env GOMAXPROCS=2 go test -p 1 ./...`, `env GOMAXPROCS=2 go test -p 1 -race ./internal/adapters/talos ./internal/application ./internal/tui`, `git diff --check`, and `env GOMAXPROCS=1 go build -p 1 -o t9s ./cmd/t9s`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/footer.go internal/tui/table_layout.go internal/tui/footer_test.go internal/tui/action_prompt_test.go
git commit -m "feat: show Talos recovery warnings in the TUI"
```

