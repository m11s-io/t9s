# Shared backend migration baseline

Date: 2026-10-18

## Execution baseline and ownership

- Worktree: `m11s-io-github/t9s/.worktrees/shared-backend`
- Branch: `refactor/shared-backend`
- HEAD: `8031b90ad40dc306441c6c99823ede17242fcdf9` (`fix: retain mutation ownership and bound operation requests`)
- Tracked working tree: clean; staged diff: empty; unstaged diff: empty; diff summary: no tracked changes.
- `git status --short` at inventory time contained only these untracked, coordinator-approved planning artifacts:
  - `docs/superpowers/plans/2026-10-18-shared-backend-migration.md`
  - `docs/superpowers/reviews/2026-10-18-shared-backend-plan-review.md`
  - `docs/superpowers/specs/2026-10-18-shared-backend-migration-design.md`
- The planning artifacts are coordinator-owned; this task does not edit or stage them. The baseline report and its task report are task-owned. No other active writer or product-file diff was present. No files were stashed, reset, or overwritten.

The baseline commit includes the preceding operation-ownership work: operation outcome distinctions (`domain/operation.go`), shared operation timeout/error policy (`application/operation_policy.go`), bounded node/service/etcd operation contexts and request cancellation (`application/actions.go`, `service_actions.go`, `effects.go`, `requests.go`), and upgrade recovery warning handling. Existing related coverage includes `application/actions_test.go`, `requests_test.go`, `upgrade_test.go`, `architecture_test.go`, `tui/actions_flow_test.go`, and Talos controller timeout tests in `adapters/talos/node_controller_test.go`. Those capabilities are part of this baseline and must remain covered while the old runtime is removed.

## Baseline verification

Commands were run against the clean tracked baseline at the HEAD above:

| Command | Result |
| --- | --- |
| `go test ./...` | Fails on four existing expectation mismatches in `internal/application` and `internal/tui`; adapter, CLI, config, domain, ports, and version packages pass. |
| `go test -race ./...` | Same four failures; all other tested packages pass under the race detector. |
| `go vet ./...` | Passes. |

Failing cases, observed in both test commands:

1. `TestActionFailedAppendsErrorResultForCurrentGeneration` (`internal/application/update_test.go:1639`) expects `unreachable`; normalized result is `action failed`.
2. `TestUpgradeBridgeStreamsProgressAndRefreshesNodesOnSuccess` (`internal/application/upgrade_test.go:65,68`) expects empty operation outcomes; actual outcomes are `completed`.
3. `TestConfirmingRebootReportsFailureWithTargetAndError` (`internal/tui/actions_flow_test.go:306`) expects `connection refused`; actual output uses the normalized `request failed; remote outcome unconfirmed` wording.
4. `TestUpgradeFailureKeepsPhaseAndNormalizedErrorInNotice` (`internal/tui/actions_flow_test.go:413`) expects `request failed`; actual normalized error is `request canceled`.

These failures are present before migration edits: the corresponding source and tests are committed at baseline HEAD, and there were no tracked worktree changes. Track them as baseline failures, then decide whether migration changes legitimately update the expectations; do not silently attribute them to migration.

## Task 1a: baseline expectation repair

At migration-plan HEAD `d181c63`, the four above cases were reproduced before edits with the same normalized results recorded in the original baseline. The expectations were stale: `mutationErrorText` deliberately returns `action failed` for confirmed failures; successful upgrades carry `domain.OperationCompleted`; and an unconfirmed reboot hides the transport detail while retaining the target and remote-outcome warning. For an unconfirmed canceled upgrade, it reports `request canceled`.

After updating only these assertions, the focused cases pass. The earlier baseline RED and repaired GREEN are distinct evidence: the failures predate migration work and were resolved by aligning test expectations with the committed operation policy; production behavior was not changed.

| Command | Result after expectation repair |
| --- | --- |
| `go test ./internal/application -run 'TestActionFailedAppendsErrorResultForCurrentGeneration|TestUpgradeBridgeStreamsProgressAndRefreshesNodesOnSuccess' -count=1` | Pass. |
| `go test ./internal/tui -run 'TestConfirmingRebootReportsFailureWithTargetAndError|TestUpgradeFailureKeepsPhaseAndNormalizedErrorInNotice' -count=1` | Pass. |
| `go test ./...` | Pass. |
| `go test -race ./...` | Pass. |
| `go vet ./...` | Pass. |

## Capability-to-test and migration inventory

Test-file mappings below identify the current behavioral suites, including the application contract, adapter behavior where applicable, and TUI call-site/interaction coverage. Migration task numbers refer to the ordered steps in `docs/superpowers/specs/2026-10-18-shared-backend-migration-design.md`.

| Spec capability | Current tests / behavioral evidence | Migration task |
| --- | --- | --- |
| Context catalog, selected context, initial selection, kube-context association | `application/architecture_test.go`, `application/requests_test.go`, `tui/contexts_test.go`, `tui/startup_test.go`, `cli/kube_context_test.go`, `adapters/talos/session_test.go`, `adapters/kubernetes/resolver_test.go` | 2, 8–10 |
| Session cancellation, request replacement, stale-result rejection, close and lifecycle | `application/requests_test.go`, `application/architecture_test.go`, `application/events_follow_test.go`; current ownership is in reducer/runtime and must move to Client/Session plus TUI request owner | 2, 8–10 |
| Combined node/service observations and standalone service reads | `application/architecture_test.go`, `application/health_test.go`, `adapters/talos/nodes_test.go`, `adapters/talos/services_test.go`, `adapters/talos/node_services_test.go`; TUI `nodes_test.go`, `services_test.go`, `service_summary_test.go` | 4, 9 |
| Events snapshot and etcd membership/status | `application/architecture_test.go`, `application/etcd_safety_test.go`, `adapters/talos/events_test.go`, `adapters/talos/etcd_test.go`; TUI `events_test.go`, `etcd_test.go` | 4, 9 |
| Kubernetes node listing, role/IP matching, optional correlation and enrichment | `adapters/kubernetes/nodes_test.go`, `adapters/kubernetes/resolver_test.go`, `application/architecture_test.go`; TUI `nodes_test.go`, `metadata_test.go` | 4, 9 |
| Node processes, disks, network links, sockets, mounts, memory | No dedicated application test suite exists for process, disk, or network-link reads; adjacent application coverage is in `reset_test.go`, `wipe_device_test.go`, `netstat_test.go`, `mounts_test.go`, `memory_test.go`. Talos suites: `processes_test.go`, `disks_test.go`, `network_test.go`, `netstat_test.go`, `mounts_test.go`, `memory_test.go`; TUI: `processes_test.go`, `disks_test.go`, `network_test.go`, `netstat_test.go`, `mounts_test.go`, `memory_test.go` | 3, 9 |
| CPU sampling | `application/cpu_test.go`, `application/cpu_internal_test.go`, `adapters/talos/cpu_test.go`; TUI `cpu_test.go` | 3, 9 |
| Time read and time-server check | `application/time_test.go`, `application/time_internal_test.go`, `adapters/talos/time_test.go`; TUI `time_test.go` | 3, 9 |
| Current install image for upgrade prefill and upgrade workflow | `application/upgrade_test.go`, `application/architecture_test.go`, `adapters/talos/node_controller_test.go`; TUI `actions_flow_test.go`, `upgrade_prompt_test.go`, `footer_test.go` | 3, 7, 9 |
| Resource kinds/metadata, metadata-only instance list, selected YAML | `application/architecture_test.go`, `adapters/talos/resource_kinds_test.go`, `adapters/talos/resource_instances_test.go`; TUI `resources_test.go`, `resource_test.go` | 3, 9 |
| Service logs stream, stateful decoding and bounded retention | `application/logs_test.go`, `adapters/talos/logs_test.go`; TUI `logs_test.go` | 5, 9 |
| Dmesg stream, record parsing and bounded retention | `application/dmesg_test.go`, `adapters/talos/dmesg_test.go`; TUI `dmesg_test.go` | 5, 9 |
| Event following stream and close/reopen behavior | `application/events_follow_test.go`, `adapters/talos/events_follow_test.go`; TUI `events_test.go` | 5, 9 |
| Cluster-health progress stream and normalized request addresses | `application/healthcheck_test.go`, `adapters/talos/cluster_health_test.go`; TUI `healthcheck_test.go` | 5, 9 |
| Overview/problems, health rules, evidence and unknown availability | `application/health_test.go`, `application/healthcheck_test.go`, `domain/health_test.go`; TUI `overview_test.go`, `problems_test.go`, `health_navigation_test.go` | 4, 9 |
| Node reboot, shutdown, rollback, targeted and bulk outcomes/warnings/quorum | `application/actions_test.go`, `application/architecture_test.go`, `adapters/talos/node_controller_test.go`; TUI `actions_flow_test.go` | 6–8, 9 |
| Node reset, disk-scope capture, inventory/quorum checks, typed intent | `application/reset_test.go`, `application/reset_internal_test.go`, `adapters/talos/reset_test.go`; TUI `reset_actions_test.go`, `actions_flow_test.go` | 6–8, 9 |
| Exact device wipe identity, restrictions, method and confirmation | `application/wipe_device_test.go`, `application/wipe_device_internal_test.go`, `adapters/talos/wipe_test.go`; TUI `disks_wipe_actions_test.go` | 6–8, 9 |
| Exact service start/stop/restart plus etcd stop/restart quorum policy | `application/service_actions_test.go`, `adapters/talos/service_controller_test.go`; TUI `actions_flow_test.go`, `services_test.go` | 6–8, 9 |
| Etcd snapshot write gate, path validation, atomic output/checksum | `application/etcd_actions_test.go`, `application/etcd_safety_test.go`, `adapters/talos/etcd_operations_test.go`; TUI `etcd_actions_test.go` | 6–8, 9 |
| Etcd defragment, alarm disarm, leadership transfer and member-node routing | `application/etcd_actions_test.go`, `application/forfeit_leadership_test.go`, `application/forfeit_leadership_internal_test.go`, `adapters/talos/etcd_operations_test.go`; TUI `etcd_actions_test.go`, `etcd_forfeit_actions_test.go` | 6–8, 9 |
| Etcd remove/leave, voter/learner and availability policy, mandatory successful snapshot and staged mutation | `application/etcd_membership_actions_test.go`, `application/etcd_membership_internal_test.go`, `application/etcd_safety_test.go`, `adapters/talos/etcd_operations_test.go`; TUI `etcd_membership_actions_test.go` | 6–8, 9 |
| Upgrade lifecycle/fallback, progress, cordon ownership, terminal outcomes and recovery warnings | `application/upgrade_test.go`, `application/architecture_test.go`, `ports/node_upgrade_test.go`, `adapters/talos/node_controller_test.go`; TUI `upgrade_prompt_test.go`, `actions_flow_test.go`, `footer_test.go` | 6–9 |
| TUI navigation, keys, filters, prompts, rendering, visual compatibility | `tui/*_test.go`, with visual contracts in `tui/k9s_visual_golden_test.go` and `tui/testdata/*.golden`; architecture-sensitive coverage in `tui/sync_test.go`, `view_lifecycle_test.go`, `navigation_test.go`, `contexts_test.go` | 8–10 |
| CLI composition, write gate and teardown | `cli/run_test.go`, `cli/kube_context_test.go`; teardown/cancellation also in `application/requests_test.go` and TUI lifecycle tests | 10 |
| Independent non-TUI Go caller and architecture docs | No current second-caller contract test; `application/architecture_test.go` is reducer/runtime-oriented baseline evidence, not proof of a plain Go client API | 2, 11 |

### Write and stream variant coverage checklist

All current node/service/etcd mutation paths are accounted for above. The baseline code centralizes these paths through application reducer/effect helpers and Talos port adapters; migration must provide explicit session methods and preserve shared policy in task 6–7.

| Write family and variants | Existing tests |
| --- | --- |
| Node lifecycle: reboot, shutdown, rollback; single/bulk targets, permissions, prompt confirmation, normalized accepted/unconfirmed outcomes | `application/actions_test.go`, `application/architecture_test.go`, `adapters/talos/node_controller_test.go`, `tui/actions_flow_test.go` |
| Reset: single/bulk, options and actual disk selection, current inventory and quorum blocks, exact confirmation | `application/reset_test.go`, `application/reset_internal_test.go`, `adapters/talos/reset_test.go`, `tui/reset_actions_test.go` |
| Device wipe: method, exact device, system/read-only protection, confirmation | `application/wipe_device_test.go`, `application/wipe_device_internal_test.go`, `adapters/talos/wipe_test.go`, `tui/disks_wipe_actions_test.go` |
| Service start, stop, restart; exact node/service and etcd quorum safety | `application/service_actions_test.go`, `adapters/talos/service_controller_test.go`, `tui/actions_flow_test.go` |
| Etcd snapshot: explicit target/path, write gate, invalid path, no-overwrite/checksum behavior | `application/etcd_actions_test.go`, `application/etcd_safety_test.go`, `adapters/talos/etcd_operations_test.go`, `tui/etcd_actions_test.go` |
| Etcd maintenance: defragment, alarm disarm, transfer leadership; selected member routing and timeout behavior | `application/etcd_actions_test.go`, `application/forfeit_leadership_test.go`, `application/forfeit_leadership_internal_test.go`, `adapters/talos/etcd_operations_test.go`, `tui/etcd_actions_test.go`, `tui/etcd_forfeit_actions_test.go` |
| Etcd membership: remove/leave member, exact ID/hostname matching, voter/learner and quorum, successful snapshot-before-RPC sequence | `application/etcd_membership_actions_test.go`, `application/etcd_membership_internal_test.go`, `application/etcd_safety_test.go`, `adapters/talos/etcd_operations_test.go`, `tui/etcd_membership_actions_test.go` |
| Upgrade: image/version checks, stream, outcome, cancellation/recovery and cordon cleanup | `application/upgrade_test.go`, `application/architecture_test.go`, `ports/node_upgrade_test.go`, `adapters/talos/node_controller_test.go`, `tui/upgrade_prompt_test.go`, `tui/actions_flow_test.go`, `tui/footer_test.go` |
| Service logs / log stream | `application/logs_test.go`, `adapters/talos/logs_test.go`, `tui/logs_test.go` |
| Dmesg / record-oriented stream | `application/dmesg_test.go`, `adapters/talos/dmesg_test.go`, `tui/dmesg_test.go` |
| Event follow / stream | `application/events_follow_test.go`, `adapters/talos/events_follow_test.go`, `tui/events_test.go` |
| Cluster-health progress / stream | `application/healthcheck_test.go`, `adapters/talos/cluster_health_test.go`, `tui/healthcheck_test.go` |

The tests establish a broad baseline, but they currently test behavior through `application.Update`/`Model`/`Effect` and associated TUI bridge paths. Tasks 2–7 must add external-package tests of the concrete `Client`/`Session` API using ports fakes; tasks 8–10 must move the interaction tests to direct TUI calls and remove the old reducer/effect runtime. Baseline test existence is not evidence that the future direct API contract already exists.
