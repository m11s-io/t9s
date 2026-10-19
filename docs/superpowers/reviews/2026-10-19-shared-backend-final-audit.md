# Shared backend migration final audit

Date: 2026-10-19
Branch: `refactor/shared-backend`
Baseline: `8031b90` (`fix: retain mutation ownership and bound operation requests`)
Audited implementation head: `7646cb6` (`fix(tui): restore mutation observation refreshes`)

## Architecture result

The migration leaves one Bubble Tea v2 root in `internal/tui`. `tui.NewWithCleanup(context.Context, *application.Client, LaunchOptions)` is the CLI composition point; the TUI owns prompts, navigation, selection, view state, request stamps, and its shared cluster snapshot. Feature commands call typed methods on `application.Session` and render the returned domain values. The application package no longer contains a UI model or reducer runtime.

`internal/application` is a plain Go backend boundary. A `Client` opens independent `Session` values; sessions provide typed reads, normalized streams, assessments, prepared plans, execution, cancellation, and mutation exclusion. Policy and fresh safety checks remain in the backend, while frontends own interaction and display state. The future React UI can use this API through a transport or adapter without depending on Bubble Tea. React, HTTP endpoints, authentication, browser sessions, and snapshot download delivery are not implemented by this migration.

The reducer deletion is complete. The removed application files are `model.go`, `update.go`, `dispatch.go`, `effects.go`, `requests.go`, `operations.go`, `batch.go`, `snapshots.go`, and `events_follow.go`; reducer/effect orchestration was also removed from the old `actions.go`, `cpu.go`, `time.go`, and `service_actions.go`. The TUI legacy root and bridges `model.go`, `sync.go`, and `view_lifecycle.go` were deleted. Useful policy, validation, read, stream, and operation functions remain in focused direct files. Prompt-only pending state now lives under `internal/tui`.

## Boundary checks

The following production reference search returned no matches:

```text
rg -n 'application\.(Update|Model|Message|Effect|Runner|NewModel|NewRunner)|syncScreens|syncAllScreens' internal cmd
```

No application reducer definitions, frontend request slots/generations, or pending prompt state remain. The application package dependency search returned no Charm or TUI dependencies:

```text
GOCACHE=/private/tmp/t9s-task6-gocache go list -deps ./internal/application | rg 'charm.land/|/internal/tui'
# no matches
```

Review of exported application declarations found backend options, dependency ports, client/session methods, typed operation requests and kinds, prepared plans and assessments, mutation results, and explicit health input. It found no frontend state, Charm types, or Talos SDK credential/configuration types. `Dependencies` contains only `ContextCatalog`, `SessionFactory`, and `KubernetesResolver`. The separate caller contract test in external package `application_test` uses only exported Client/Session APIs.

The existing `reuse_test.go` verifies a non-TUI caller can read diagnostics, receive a write-disabled rejection, prepare an action without dispatch, require explicit confirmation, execute the prepared service operation, consume and close a normalized event stream, and close its session. This demonstrates the reuse boundary without adding a second UI framework or a generic command engine.

## Baseline capability reconciliation

The baseline inventory at `8031b90` identified the following behaviors. The named current suites retain them through direct APIs, adapters, and TUI behavior:

| Capability group | Current behavioral evidence |
| --- | --- |
| Context discovery, context selection, Kubernetes context correlation, startup focus, picker and CLI write gate | `internal/application/read_cluster_test.go`, `internal/adapters/kubernetes/*_test.go`, `internal/cli/*_test.go`, `internal/tui/requests_test.go`, direct context and startup tests |
| Session ownership, caller/session cancellation, close, stale results and late stream disposal | `internal/application/session_test.go`, `streams_test.go`, `read_diagnostics_test.go`; `TestDirectRequestRejectsSameNodeOlderResult`, `TestDirectLateStreamOpenIsClosed`, `TestDirectTeardownCoversRuntimeFailure` |
| Node/service observations, etcd discovery, Kubernetes enrichment and partial results | `read_cluster_test.go`, `read_resources_test.go`, adapter node/service/etcd tests, direct node/service/etcd/metadata tests |
| Process, disk, network, socket, mount and memory reads | `read_diagnostics_test.go`, `read_resources_test.go`, corresponding Talos adapter tests and TUI renderer/interaction suites |
| CPU and time reads/checks, validation and cancellation | `TestCPUAndTimeReadsHonorCallerAndSessionCancellation`, `read_diagnostics_test.go`, `internal/tui/direct_cpu_time_test.go`, adapter CPU/time tests |
| Current install image and upgrade lifecycle, progress, timeout, terminal outcome, recovery and post-upgrade observations | `TestCurrentInstallImageUsesSessionController`, `node_actions_test.go`, `upgrade_operation_test.go`, Talos controller tests, `TestDirectUpgradeTerminalOutcomesRemainDistinctAndErrorsStayInspectable` |
| Resource kinds, metadata-only instance listing, selected YAML and cancellation | `read_resources_test.go`, Talos resource tests, direct resource TUI tests |
| Logs, dmesg, event following and cluster-health streams | `streams_test.go`, Talos stream tests, direct TUI stream/health tests for cancellation, overflow, EOF, partial problems and late close |
| Health diagnoses and unknown availability | `health_test.go` now calls `EvaluateObservations(HealthInput)` directly; domain health and direct overview/metadata tests remain |
| Reboot, shutdown, rollback and bulk outcome uncertainty | `node_actions_test.go`, `direct_mutations_test.go`, direct upgrade/action summary tests |
| Reset and device wipe: typed confirmation, exact inventory, scope, quorum, changed identity and accepted-dispatch inventory refresh | `action_validation_test.go`, `node_actions_test.go`, `TestDirectWipeCompletionMarksDiskSnapshotLoading`, `direct_action_compat_test.go` |
| Service start/stop/restart, admission, quorum, cancellation and uncertain outcome | `service_operations_test.go`, direct mutation/service TUI tests |
| Etcd snapshot and maintenance: path, identity, outcome and local snapshot metadata | `etcd_safety_test.go`, `etcd_actions_direct_test.go`, `direct_etcd_mutations_test.go` |
| Etcd remove/leave: mandatory verified snapshot, quorum, voter/learner policy, identity and stage exclusion | `TestMembershipWaitsForVerifiedSnapshot`, `TestMembershipAbortsWhenSnapshotFailsOrReceiptIsInvalid`, `TestMembershipRechecksAfterSnapshot`, `TestMembershipNeverFollowsChangedMemberIdentity`, `TestMembershipRetainsLeaseAcrossStages`, and direct etcd TUI mutation tests |
| TUI input precedence, navigation, filters, prompts, rendering and visual compatibility | Direct root/interaction/feature suites and `TestK9sVisualGolden`; all 11 existing golden files are unchanged |
| Independent caller contract | `TestPlainGoCallerUsesReadsActionsAndStreamsWithoutTUI` |

The initial 18b Sol review found two real coverage gaps: failed etcd snapshots were only tested through the old reducer, and CPU/time tests did not prove cancellation reached the direct ports. Fix round 1 added the direct Session assertions named above. The final whole-branch Sol review caught two dropped observation refreshes, an unmapped recovery outcome, and duplicate prompt renderers. Commit `7646cb6` restores disk and node refreshes without upgrading accepted outcomes to completion, maps recovery warnings, removes the dead prompt path, and keeps the active upgrade warning/version/confirmation visible at 80 columns. Sol approved the final branch after those fixes.

Reducer-only tests were removed with the machinery they exercised. The test mapping is recorded in `.superpowers/sdd/2026-10-18-shared-backend-migration/task-18b-brief-report.md`; safety and outcome assertions were preserved in direct application, adapter, or TUI tests rather than retaining compatibility state solely for tests.

## Go line counts

Physical lines in tracked `.go` files, comparing baseline commit `8031b90` with `7646cb6`; `_test.go` files are counted separately. These counts are descriptive, not a target. The earlier `baseline-loc.json` snapshot at `b771bdd` has matching package totals; adapter counts below split that snapshot into its two packages.

| Package | Production baseline | Production current | Delta | Test baseline | Test current | Delta |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `cmd/t9s` | 12 | 12 | 0 | 0 | 0 | 0 |
| `internal/adapters/kubernetes` | 115 | 115 | 0 | 129 | 129 | 0 |
| `internal/adapters/talos` | 3,739 | 3,777 | +38 | 4,021 | 4,162 | +141 |
| `internal/application` | 5,331 | 3,273 | −2,058 | 5,510 | 3,747 | −1,763 |
| `internal/cli` | 133 | 138 | +5 | 294 | 294 | 0 |
| `internal/config` | 62 | 62 | 0 | 71 | 71 | 0 |
| `internal/domain` | 450 | 450 | 0 | 46 | 46 | 0 |
| `internal/ports` | 427 | 428 | +1 | 126 | 126 | 0 |
| `internal/testkit` | 525 | 535 | +10 | 0 | 0 | 0 |
| `internal/tui` | 6,780 | 9,846 | +3,066 | 5,644 | 8,701 | +3,057 |
| `internal/version` | 11 | 11 | 0 | 27 | 27 | 0 |
| **Total** | **17,585** | **18,647** | **+1,062** | **15,868** | **17,303** | **+1,435** |

The largest movement is the intended boundary shift: the application loses the reducer and its frontend fixtures, while the TUI now owns direct view/input/result handling and its behavioral tests. Direct APIs and meaningful safety coverage remain explicit; the implementation did not add a registry, generic UI engine, command bus, or production dependency. The net growth is concentrated in those explicit direct TUI paths and regression tests. This audit treats line count as context, not an optimization target; further simplification should follow real duplication rather than compressing clear feature code.

## Final verification

All Go commands used `GOCACHE=/private/tmp/t9s-task6-gocache` and test commands used `-timeout=120s`:

| Command | Result |
| --- | --- |
| `go test ./... -timeout=120s` | Pass, all packages |
| `go test -race ./... -timeout=120s` | Pass, all packages |
| `go vet ./...` | Pass |
| `go build -o /tmp/t9s-shared-backend-final ./cmd/t9s` | Exit 0; produced a Mach-O arm64 executable. Go printed a nonfatal module stat-cache permission warning. |
| `git diff --check` | Pass on committed source at `7646cb6` |

No live Talos/Kubernetes cluster smoke test was run. There is no React UI, HTTP/authentication layer, or web snapshot delivery path in this branch.

## Review and implementation commits

- `ad5b6c0` — remove TUI reducer root and presentation bridges; Sol approved.
- `f842a9c` — remove application reducer runtime; Sol approved after fix round 1.
- `7646cb6` — restore accepted mutation observation refreshes and remove duplicate prompt renderers; Sol approved after the final 80-column prompt fix.
- `128d47c` — add the external plain-Go caller proof.
- `a7c2303`, `6e9994f` — document shared-backend safety and clarify execution deadlines/outcomes; Sol approved the documentation follow-up.

Final whole-branch Sol review: approved after the migration fixes; no remaining findings.
