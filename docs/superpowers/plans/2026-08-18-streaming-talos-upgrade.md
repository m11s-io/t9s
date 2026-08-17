# Streaming Talos Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans (inline execution is also valid). Steps use checkbox syntax for tracking.

**Goal:** Add schematic-preserving, lifecycle-streamed Talos upgrades to t9s with safe fallback, drain/reboot cleanup, version warnings, and visible progress.

**Architecture:** Keep Talos protobuf/gRPC types inside internal/adapters/talos. Extend ports with normalized upgrade events and maintenance operations. Bridge the long-running operation through the existing one-effect/one-message application runner by reading one buffered update per effect.

**Tech Stack:** Go 1.26.3, Talos machinery v1.13.3, COSI runtime resources, Kubernetes client-go, Bubble Tea v2, Testify.

## Global Constraints

- Lifecycle capability range: >1.13.0-alpha.2 <2.0.0.
- Lifecycle success requires terminal exit code zero.
- Legacy fallback is only for versions outside that range.
- Installer suggestions preserve Image Factory factory, flavor, and schematic.
- Upgrade remains gated by WritesEnabled and explicit confirmation.
- No credentials or config contents in model state or logs.
- Final checks: go test ./..., go test -race ./..., go vet ./..., go build ./cmd/t9s.

---

### Task 1: Normalize ports and fakes

Files: internal/ports/node.go, internal/ports/kubernetes.go, internal/testkit/fakes.go.

- [ ] Write failing tests for normalized UpgradePhase, UpgradeEvent, UpgradeResult/stream, and maintenance calls.
- [ ] Run focused tests and confirm missing-type failures.
- [ ] Add the smallest types and fake implementations. Keep Talos types out of ports.
- [ ] Run go test ./internal/... and commit: feat: add upgrade progress ports.

### Task 2: Schematic-aware image suggestions

Files: internal/adapters/talos/node_controller.go, node_controller_test.go, session.go if wiring changes.

- [ ] Write failing tests for `ExtensionStatus` `schematic` version/author decoding, metal-installer and aws-installer paths, declared-image fallback, and digest preservation.
- [ ] Run focused adapter tests and confirm failures.
- [ ] Read the installed `ExtensionStatus` resource named `schematic`; its version supplies the schematic ID and its author supplies flavor/factory metadata. The pinned v1.13.3 SDK has no `ImageFactorySchematic` resource. Fall back to the declared image. Use a pure helper matching Talos images.NewInstallerImage and running version tag replacement.
- [ ] Run adapter tests and commit: feat: preserve Talos schematic in upgrade suggestions.

### Task 3: Lifecycle orchestration and legacy fallback

Files: internal/adapters/talos/node_controller.go or new upgrade.go; node_controller_test.go.

- [ ] Write failing fake-client tests for exact semver boundaries, v1.13.3 lifecycle choice, legacy choice, pull-before-install, nonzero exit, EOF without exit, cancellation, reboot failure, and uncordon cleanup.
- [ ] Run focused tests and confirm failures.
- [ ] Implement ImageService.Pull, LifecycleService.Upgrade, terminal exit validation, Talos kubeconfig/Nodename resolution, five-minute drain, reboot/readiness, uncordon, joined cleanup errors, and normalized events.
- [ ] Run adapter and race tests; commit: feat: stream Talos lifecycle upgrades.

### Task 4: Application stream bridge

Files: internal/application/model.go, actions.go, effects.go, update.go, update_test.go, internal/testkit/fakes.go.

- [ ] Write failing tests for UpgradeStarted, ordered UpgradeProgressed events, closed-channel failure, stale generation, single-upgrade gating, cancellation, and success refresh.
- [ ] Run focused application tests and confirm failures.
- [ ] Start one controller stream in a buffered channel; return one start message; store only a receive-only stream; schedule one read effect per progress update; convert close-without-terminal to failure.
- [ ] Run application tests and race tests; commit: feat: bridge streaming upgrades through application effects.

### Task 5: TUI warnings and progress

Files: internal/tui/action_prompt.go, upgrade_prompt.go, model.go, footer/notice renderer, actions_flow_test.go, action_prompt_test.go, upgrade_prompt_test.go, affected goldens.

- [ ] Write failing tests for skipped-minor warning, no warning for patch/one-minor, known/unknown totals, failed-phase notice, cancellation, and read-only navigation during upgrade.
- [ ] Run focused TUI tests and confirm failures.
- [ ] Implement pure semantic-version warning and bounded percentage helpers, then wire state and key routing; disable competing writes only.
- [ ] Regenerate affected goldens, run go test ./internal/tui, and commit: feat: show Talos upgrade progress and warnings.

### Task 6: Session wiring and documentation

Files: internal/adapters/talos/session.go, session tests, README.md, relevant docs pages.

- [ ] Write failing session tests proving the authenticated Talos client supplies maintenance operations without local kubeconfig.
- [ ] Run focused tests and confirm failures.
- [ ] Wire dependencies and document lifecycle progress, sequential-minor warning, schematic-preserving suggestions, and write-mode safety.
- [ ] Run go test ./... and docs tests; commit: docs: document safe Talos upgrades.

### Task 7: Verification and infrastructure checkpoint

- [ ] Run go test ./..., go test -race ./..., go vet ./..., and go build ./cmd/t9s.
- [ ] Review for credential leakage, stale goroutines, accidental bulk upgrades, and wrong Image Factory flavors.
- [ ] Run the disposable Talos smoke test only after automated checks pass.
- [ ] Create a separate infrastructure design/plan for explicit HostnameConfig and schematic-derived installer images; do not couple OpenTofu apply to this t9s change.

