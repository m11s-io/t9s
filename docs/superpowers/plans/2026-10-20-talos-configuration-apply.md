# Talos Configuration Apply Implementation Plan

> **For agentic workers:** Use the inline implementation workflow for this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `:config` workflow that previews a local Talos machine configuration on selected nodes and applies the same bytes after a bound confirmation.

**Architecture:** Keep Talos SDK calls behind a new configuration controller port. The application session owns validated prepared plans, previews, the write gate, stale-plan checks, and serialized execution; the TUI holds only a plan handle and safe summaries. The Talos adapter drops raw server diff and error strings, returning normalized status and private fingerprints only.

**Tech Stack:** Go 1.27.1, Bubble Tea v2, Talos machinery v1.14.4, Astro docs.

**Spec:** `docs/superpowers/specs/2026-10-20-configuration-apply-design.md`

## Global Constraints

- `t9s` stays read-only unless started with `--enable-writes` or `T9S_ENABLE_WRITES`.
- Offer only `AUTO`, `NO_REBOOT`, and `STAGED`; omit deprecated `REBOOT` and defer `TRY`.
- Read a nonempty regular file of at most 4 MiB; reject empty or ambiguous target sets.
- Show no raw configuration, Talos diff, warning, or validation error text in TUI state.
- Keep prepared plans session-bound, single-use, and valid for five minutes.
- Re-preview all canonical targets under the session mutation lease before the first apply RPC.
- Use 30-second per-RPC deadlines and a ten-minute overall apply deadline; preserve shorter caller deadlines.
- Apply sequentially, stop after first failure, and never retry automatically.
- Do not validate by applying configuration to a live cluster.

## Review Focus

- Secret-bearing Talos `ModeDetails` or error text must be normalized or discarded in the adapter before application or TUI state sees it.
- `AUTO` dry-run returns `NO_REBOOT`; compare returned mode to the expected resolved mode.
- Plan expiry, session closure, mutation contention, and a second execution must prevent an apply RPC.
- Context cancellation after dispatch leaves that target unconfirmed and later targets not attempted.
- Apply errors must preserve already accepted targets and must never expose raw server text.

---

### Task 1: Add the configuration port and Talos adapter

**Files:**
- Create: `internal/domain/configuration.go`
- Create: `internal/ports/configuration.go`
- Modify: `internal/ports/context.go`
- Create: `internal/adapters/talos/configuration_controller.go`
- Modify: `internal/adapters/talos/session.go`
- Modify: `internal/testkit/fakes.go`

**Interfaces:**
- Produce domain modes `ConfigurationModeAuto`, `ConfigurationModeNoReboot`, `ConfigurationModeStaged`; normalized response mode, warning count, and `[32]byte` private response fingerprint.
- Produce `ports.ConfigurationController` with `Preview(ctx, node string, data []byte, mode domain.ConfigurationMode) (domain.ConfigurationPreview, error)` and `Apply(ctx, node string, data []byte, mode domain.ConfigurationMode) (domain.ConfigurationApplyResult, error)`.
- `ports.Session.Configuration()` returns that controller.

- [x] Add only SDK-independent configuration mode/result types and the controller port.
- [x] Add an injectable Talos machine configuration client with `ApplyConfiguration` so the adapter maps dry-run/apply flags without coupling tests to a live client.
- [x] Implement preview mapping, hashing raw response details privately, counting warnings, discarding raw warning/detail/error strings, and resolving requested `AUTO` to the expected `NO_REBOOT` response mode.
- [x] Implement apply mapping and return only normalized mode/status/fingerprint data; convert SDK errors to safe error categories.
- [x] Wire the new controller through Talos session construction and accessors.

### Task 2: Add prepared application plans and execution policy

**Files:**
- Create: `internal/application/configuration_apply.go`
- Modify: `internal/application/action_plan.go`
- Modify: `internal/application/mutation.go`
- Modify: `internal/application/session.go`

**Interfaces:**
- Add `ConfigurationApplyRequest{Data []byte, Targets []string, Mode domain.ConfigurationMode}`.
- Add `PreparedConfiguration` with `Assessment() ConfigurationAssessment` and safe per-target `Preview()` summaries; keep data, digests, response fingerprints, expiry, and one-use state private.
- Add `Session.PrepareConfiguration(ctx, request) (*PreparedConfiguration, error)` and `Session.ExecuteConfiguration(ctx, plan, confirmation, onTarget) (MutationResult, error)`.

- [x] Extend private prepared state and `admit` so configuration plans are session-bound, writes-gated, one-use, and use the shared mutation lease.
- [x] Validate payload length (1 byte through 4 MiB), supported mode, nonempty unique canonical targets, and the session configuration capability before preview.
- [x] Resolve canonical targets, issue a 30-second dry-run to every target, normalize outcomes, and block plan creation if any preview fails or returns an unexpected mode.
- [x] Bind the plan to a private SHA-256 payload digest, canonical targets, mode, per-target response fingerprints, and a five-minute expiry; retain an owned payload copy.
- [x] On execution, admit the plan, reject expired plans, re-resolve targets, and re-run all dry runs under the mutation lease before any apply RPC.
- [x] Compare identities and response fingerprints; on any drift, return assessment-changed without applying.
- [x] Apply targets sequentially with per-RPC deadlines and a ten-minute overall deadline; stop after failure/cancellation and preserve accepted, staged, failed, unconfirmed, and not-attempted results.
- [x] Clear plan-owned byte references on plan disposal and after execution; return only safe normalized errors and summaries.

### Task 3: Build the `:config` TUI workflow

**Files:**
- Create: `internal/tui/configuration_model.go`
- Create: `internal/tui/configuration_commands.go`
- Create: `internal/tui/configuration_view.go`
- Modify: `internal/tui/command.go`
- Modify: `internal/tui/cluster_commands.go`
- Modify: `internal/tui/direct_model.go`
- Modify: `internal/tui/state.go`
- Modify: `internal/tui/frame.go`
- Modify: `internal/tui/shell.go`

**Interfaces:**
- `:config` resolves to a closed `commandConfiguration` value.
- The view collects a local path, selected node set, and explicit mode; it stores only the application plan handle and safe summaries.
- Async commands return typed messages with operation/generation IDs and never include raw file bytes.

- [x] Add the `:config` palette entry and route it to a dedicated configuration view.
- [x] Add file path input and an asynchronous bounded reader that accepts only nonempty regular files up to 4 MiB, sanitizes the displayed basename, and transfers bytes directly to `PrepareConfiguration` without placing them in a Bubble Tea message or model.
- [x] Populate node selection from the active session; require at least one unambiguous canonical node and an explicit `AUTO`, `NO_REBOOT`, or `STAGED` mode.
- [x] Render only the basename, first 12 SHA-256 hex characters, canonical targets, requested/returned modes, normalized preview status, and warning counts when available; state clearly that diffs and warning text are hidden.
- [x] Enable typed confirmation only after all previews succeed; bind the phrase to mode and target count and execute the exact prepared plan.
- [x] Render progress and per-target accepted/staged/failed/unconfirmed/not-attempted outcomes; mark cancellation and post-dispatch disconnect as potentially partial.
- [x] Invalidate and release the plan when the form changes, the view closes, the context changes, the session closes, or the application plan expires.
- [x] Keep `:config` unavailable for preparation and apply when writes are disabled, with an actionable notice.

### Task 4: Document the shipped workflow

**Files:**
- Modify: `docs/src/content/docs/reference/commands.md`
- Modify: `docs/src/content/docs/security.md`

**Interfaces:**
- Documentation describes only shipped command and key behavior; no undocumented modes or guarantees.

- [x] Document `:config`, local file requirements, target/mode selection, preview summary, typed confirmation, and the write gate in the commands reference.
- [x] Document secret handling, hidden diff/warning text, per-node application, partial outcomes, and required inspection before retry in the security guide.
- [x] Check that the documentation does not imply atomic apply, rollback, warning-free configuration, or successful node convergence.

## Self-review

- **Spec coverage:** Input validation, mode restrictions, preview, redaction, plan binding, write gate, re-preview, deadlines, cancellation, partial outcomes, TUI flow, and documentation each map to Tasks 1–4. Disposable-cluster validation remains a release check; no live-cluster operation is permitted.
- **Step scan:** Every checkbox names one code or documentation action with exact files, values, or interface output.
- **Type consistency:** Port results are domain types; application plans retain secrets privately; TUI receives only the plan handle and safe summaries.
- **Review focus:** The five high-risk conditions appear in the adapter, application policy, and TUI result handling tasks above.
- **Proportion:** Four tasks align to the four deliverables and keep UI, Talos SDK, application policy, and docs responsibilities separated.
