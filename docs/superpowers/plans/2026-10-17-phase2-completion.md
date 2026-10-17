# Phase 2 Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete remaining Phase 2 diagnostics and explicit force maintenance controls.

**Architecture:** Extend the existing domain → ports → Talos adapters → application → TUI boundaries, delivering each capability end to end. Tasks execute sequentially because session, reducer, root TUI, and fake wiring are shared. Keep capability logic in focused new files rather than growing the root switches unnecessarily.

**Tech Stack:** Go 1.27.1 minimum / Go 1.27.2 verification, Talos machinery v1.14.4, COSI, Bubble Tea v2, Astro/Bun docs.

**Spec:** `docs/superpowers/plans/2026-09-16-talos-114-coverage.md`, Phase 2 items 12c–14, plus the approved time design and task decisions below. The user requested planning all remaining Phase 2 work and then explicitly chose subagent implementation on 2026-10-17. This plan records the concrete implementation decisions for those bounded extensions.

## Global Constraints

- Preserve the Talos machinery v1.14.4 pin; no unrelated dependency updates.
- SDK/protobuf/COSI types stay in adapters; ports use domain values and standard-library types only.
- Time, CPU, disk usage, containers/images/stats, and service detail work with writes disabled.
- Force actions require `--enable-writes`, explicit target confirmation, and existing confirm-time quorum gates; force never overrides a block.
- Every async result carries context Generation and request identity; older requests, changed nodes/namespaces, and old contexts cannot replace current data.
- Bind RPCs to session and view/request cancellation; canceled or partial data is never shown as a complete healthy result.
- No live cluster operations, publishing, or push as part of implementation. Local fake-driven checks and disposable-cluster manual checklists only.
- Leave the existing untracked `t9s` executable untouched until a requested rebuild. Build verification outputs to `/tmp/t9s-phase2`.
- TDD for behavior: run a focused failing regression before implementation, then the focused passing check. One full `go test ./...` before each task commit; full race/vet/docs checks after integration.

## Review Focus

- Talos reader credentials: TimeCheck permission denial preserves normal time data (Task 1).
- Counter resets, CPU hotplug, zero sample interval: show unknown rather than a bogus CPU percentage (Task 2).
- Large/slow/erroring directory trees: bound rows/time and preserve partial rows on cancellation (Task 3).
- Node/namespace switching with in-flight results: never mix container or image scopes (Task 4).
- Force mode selection or stale quorum evidence: no mutation before typed confirmation; re-evaluate before dispatch (Task 6).

## Execution and shared wiring

Each task owns its complete end-to-end slice. Shared wiring may modify:
`internal/ports/context.go`, `internal/adapters/talos/session.go`,
`internal/application/{model,effects,update}.go`, `internal/testkit/fakes.go`,
`internal/tui/{model,navigation,actions}.go`, compatibility/golden tests, and
`docs/src/content/docs/{guides/diagnostics,reference/commands,security}.md`.
Use existing memory/mounts/dmesg patterns. New async reads must be canceled on
view exit; do not rely on context switching alone. Never dispatch simultaneous
implementers editing these files.

Each task includes its own docs and roadmap status changes. Mark only that
capability done; Kubernetes upgrades and all Phase 3–5 work remain pending.

### Task 1: Time status and explicit server checks

**Files:** Create `internal/domain/time.go`, `internal/ports/time.go`,
`internal/adapters/talos/time.go`, `internal/application/time.go`,
`internal/tui/time.go`, `internal/tui/time_prompt.go`, and focused `*_test.go` files;
modify the shared wiring above and diagnostic documentation.

**Interfaces:** `TimeReader.Read(ctx context.Context, node string) (domain.TimeSnapshot, error)`;
`TimeReader.Check(ctx context.Context, node, server string) (domain.TimeCheckResult, error)`;
`Session.Time() TimeReader`. `TimeSnapshot` carries synchronization known/value,
server, local/remote UTC times with validity flags, measured time difference,
and partial problems. `TimeCheckResult` is separate from normal status.

- [ ] Write regressions `TestTimeReadPreservesStatusWhenConfiguredServerFails`,
  `TestTimeCheckPermissionDeniedPreservesStatus`, `TestTimeRejectsInvalidTimestamps`,
  and `TestTimeLateNodeResultIgnored`. Assert a check denied with gRPC PermissionDenied
  shows a normalized permission message, leaves prior status untouched, and issues no fallback check.
  Use local time `2026-10-17T12:00:00Z` and remote time 250 ms later: difference
  is `+250ms`, not a synchronization/health verdict. Missing timestamps produce unknown.
- [ ] Run `go test ./internal/adapters/talos ./internal/application ./internal/tui -run Time`;
  verify the new behavioral cases fail before implementation.
- [ ] Implement the interfaces using node-scoped `client.Time`, `client.TimeCheck`,
  and COSI time Status. Fetch status independently so absent/failed configured-server
  measurements do not discard it. Check protobuf timestamps before AsTime.
  Render measured difference as `server - node` and label it a sampled difference,
  not an authoritative NTP offset. No automatic arbitrary-server checks.
- [ ] Bind node key `t` to the view; `r` refreshes normal status, `/` filters,
  `c` opens a server prompt, Enter checks, Esc cancels. Accept a trimmed hostname
  or IP, including bare IPv6; reject empty input, whitespace/control characters,
  URL schemes and paths. Preserve server checks as a separate panel/status; permissions
  are enforced by the server, without gating this read operation on writes mode.
  Give status reads and checks separate request identities and cancel on exit.
- [ ] Run focused tests green and `go test ./...`; update docs and mark the time
  portion of roadmap 12c done. Commit `feat(diagnostics): add time status and server checks`.

### Task 2: CPU utilization and process counters

**Files:** Create `internal/domain/cpu.go`, `internal/ports/cpu.go`,
`internal/adapters/talos/cpu.go`, `internal/application/cpu.go`,
`internal/tui/cpu.go` and their tests; modify shared wiring/docs.

**Interfaces:** `CPUReader.Sample(ctx context.Context, node string) (domain.CPUSnapshot, error)`;
`Session.CPU() CPUReader`. Snapshot contains aggregate/per-core utilization with
known flags, I/O wait, sample time, running/blocked process counts, and problems.

- [ ] Write `TestCPUComputesDeltaWithoutDoubleCountingGuest`,
  `TestCPUResetOrHotplugShowsUnknown`, `TestCPUSampleCancellation`, and
  `TestCPURefreshStaysOnOpenedNode`. Test total delta 100, idle delta 60,
  iowait delta 10 gives 30% busy and 10% iowait. Zero/negative/nonfinite totals,
  decreasing counters, or changed boot time produce unknown. Sparse cores remain unknown.
- [ ] Run `go test ./internal/adapters/talos ./internal/application ./internal/tui -run CPU` red.
- [ ] Implement two node-scoped `MachineClient.SystemStat` samples separated by a
  cancellable one-second timer. Total is User+Nice+System+Idle+Iowait+Irq+SoftIrq+Steal;
  Guest/GuestNice are excluded because already included in user/nice. Busy excludes
  both idle and iowait. Preserve valid process counts when sampling fails partially.
  Keep sample timing injectable at the adapter's external boundary for deterministic tests.
- [ ] Bind node key `u` to CPU; `r` takes a new sample, `/` filters, q/Esc returns.
  No background polling; display the interval/sample time and unknown values honestly.
- [ ] Run focused tests green and `go test ./...`; update docs and mark CPU in 12c
  done. Commit `feat(diagnostics): add sampled CPU utilization`.

### Task 3: Bounded directory disk usage

**Files:** Create `internal/domain/disk_usage.go`, `internal/ports/disk_usage.go`,
`internal/adapters/talos/disk_usage.go`, `internal/application/disk_usage.go`,
`internal/tui/disk_usage.go` and tests; extend mounts selection/key handling and shared wiring.

**Interfaces:** `DiskUsageReader.Open(ctx context.Context, request domain.DiskUsageRequest) (DiskUsageStream, error)`;
`DiskUsageStream.Next(context.Context) (domain.DiskUsageBatch, error)` and `Close() error`;
`Session.DiskUsage() DiskUsageReader`. Request contains Node and Path; batch contains
path/bytes/error rows and EOF/truncated indicators. No filesystem writes or file contents.

- [ ] Write `TestDiskUsageUsesSelectedMountAndDepthOne`,
  `TestDiskUsagePreservesRowsOnPermissionError`, `TestDiskUsageStopsAt5000Rows`,
  and `TestDiskUsageExitCancelsReceive`. Assert one absolute path, depth 1,
  directory-only results; a permission error remains an error row, not zero usage.
  At 5,000 rows stop reading and show truncated. Timeout after 30 seconds preserves
  prior rows and labels the result incomplete.
- [ ] Run focused tests with `-run DiskUsage` red across adapter/application/TUI.
- [ ] Implement the stream through `client.DiskUsage` with `RecursionDepth:1`,
  `All:false`, one cleaned absolute POSIX path. Reject relative paths and control
  characters before dispatch. Use a 30-second request lifetime and a 5,000-row
  cap, closing the underlying stream at either limit, view exit, or context change.
  Do not describe these limits as a bound on the node's directory traversal cost.
- [ ] From mounts, `u` opens usage for the selected mount path. Display the path,
  received rows, partial errors/truncation, `/` filter and `r` refresh. Changing
  selection in the parent cannot retarget an open view; reject stale refreshes.
- [ ] Run focused tests green and `go test ./...`; document scan cost/cancellation
  and mark directory usage in 12c done. Commit `feat(diagnostics): inspect directory disk usage`.

### Task 4: Namespace-aware containers, images, and statistics

**Files:** Create `internal/domain/container.go`, `internal/ports/container.go`,
`internal/adapters/talos/containers.go`, `internal/application/containers.go`,
`internal/tui/containers.go`, `internal/tui/images.go` and tests; modify shared wiring/docs.

**Interfaces:** `ContainerReader.List(ctx context.Context, node string, namespace domain.ContainerNamespace) (domain.ContainerSet,error)`;
`ContainerReader.Images(ctx context.Context, node string, namespace domain.ContainerNamespace) (domain.ImageSet,error)`;
`Session.Containers() ContainerReader`. Domain enum values are `system`, `cri`,
`taloscontainers`. Set contains Node/Namespace, normalized rows, and partial problems.
Each container row includes identity, pod identity, image, PID, status, optional memory
bytes and cumulative CPU time with known flags. Image rows contain reference/digest/size.

- [ ] Write `TestContainersNamespaceDriverMapping`, `TestContainersPartialStatsFailure`,
  `TestContainersLateNamespaceResultIgnored`, `TestImagesPartialEOFAndCap`, and
  `TestContainersReadOnlyKeys`. Assert CRI maps to `k8s.io` + CRI driver; system
  and taloscontainers use their respective namespaces with CONTAINERD driver.
  Same container ID in different namespaces never shares stats or selection.
  Missing stats show unknown, not zero. One-second cumulative CPU usage is rendered
  as CPU TIME `1s`, not utilization percent. No container mutation key is exposed.
- [ ] Run focused tests with `-run 'Container|Image'` red across the three layers.
- [ ] Implement Containers + Stats reads, joining by namespace and ID; preserve
  container rows if stats fail. Use ImageList's namespace enum separately from
  container driver mapping. Bound finite image reads at 5,000 rows/30 seconds;
  preserve partial results and mark truncation/errors. Each node/namespace/mode
  selection cancels the preceding request and invalidates its results.
- [ ] Bind node `c` to containers (default `system`); `n` cycles system → cri →
  taloscontainers; `i` switches containers/images; `r` reloads the exact scope,
  `/` filters, Enter/d opens normalized row detail. Render node and namespace in
  the view title; selection cannot carry across scopes. This does not add container
  logs, arbitrary restart, or Kubernetes object management.
- [ ] Run focused tests green and `go test ./...`; mark container/image/stats in
  item 13 done, note Phase 4 item 24 has container inspection namespace support
  while any remaining service/log selection stays pending. Commit
  `feat(containers): inspect namespaces images and resource stats`.

### Task 5: Rich service detail with event history

**Files:** Modify `internal/domain/service.go`, `internal/ports/service.go`,
`internal/adapters/talos/services.go`, `internal/tui/detail.go`; create
`internal/application/service_detail.go` and focused service-detail tests; shared wiring/docs.

**Interfaces:** Add `ServiceReader.Detail(ctx context.Context,node,service string) (domain.ServiceDetail,error)`.
Detail contains Node/Service, current state, optional health, and the last 50
normalized service events (time/state/message). Missing service is unavailable,
not an empty healthy service. No SDK types escape the adapter.

- [ ] Write `TestServiceDetailUsesNodeAndService`, `TestServiceDetailCapsHistory`,
  `TestServiceDetailMissingService`, and `TestServiceDetailLateSelectionIgnored`.
  Assert exactly the requested node/service is read; 60 ordered events retain
  the newest 50; invalid event timestamps are unknown; changing rows cannot
  replace the currently open detail. Existing service actions and logs still work.
- [ ] Run focused `-run ServiceDetail` tests red across adapter/application/TUI.
- [ ] Implement via node-scoped `client.ServiceInfo`; it internally uses ServiceList,
  so do not invent a new server RPC. Preserve metadata/partial errors and expose
  bounded, ordered event history. Cancel prior reads on detail exit/change/context.
- [ ] Extend existing service Enter/d detail; `r` refreshes the captured identity,
  q/Esc returns. Keep the existing list row as a loading summary; render status,
  health and event-history failures separately. No new top-level command.
- [ ] Run focused tests green and `go test ./...`; mark item 13 complete after
  both containers and service detail ship. Commit `feat(services): show detailed service event history`.

### Task 6: Explicit force reboot and shutdown

**Files:** Modify `internal/ports/node.go`, `internal/application/{model,actions,update}.go`,
`internal/adapters/talos/node_controller.go`, `internal/tui/{model,action_prompt}.go`;
create `internal/tui/force_prompt.go` and focused force-action tests; update security/commands docs.

**Interfaces:** Add `ports.RebootForce` without changing existing RebootDefault
or RebootPowercycle values. Add `Force bool` to PendingAction and RequestAction;
only reboot/shutdown may carry it. Map RebootForce to the SDK's actual FORCE option,
not WithPowerCycle. Existing NodeController signatures are sufficient.

- [ ] Write `TestForceRebootDistinctFromPowercycle`, `TestForceActionsReadOnlyInert`,
  `TestForceRequiresTypedSingleTarget`, `TestForceQuorumRecheckedBeforeDispatch`,
  and `TestForceCancelNeverCallsController`. Assert default reboot/shutdown retain
  their old arguments; forced shutdown passes true; forced reboot uses FORCE;
  a blocked action produces no mutation even after correct typed confirmation.
- [ ] Run focused `-run Force` tests red across adapter/application/TUI.
- [ ] Within an ordinary R/X confirmation, `f` selects force mode only for a
  single target and opens a typed node-target prompt. Correct text plus Enter
  opens the final explicit force `(y/n)` prompt; Esc or incorrect identity does
  not execute. Bulk force is refused with a clear notice; ordinary bulk remains.
  Recompute action risks at final confirmation and preserve write gating.
- [ ] Implement mode-aware effects and SDK translation. Reject Force for rollback,
  reset, upgrade, or wipe requests in the reducer. Force confirmation text must
  state that graceful shutdown/drain may be skipped and workloads interrupted.
  Reset all force prompt state on cancel, context switch, and completion.
- [ ] Run focused tests green and `go test ./...`; update security and command
  docs for the intentional `f` confirmation exception, mark item 14 done, and
  mark Phase 2 complete only once Tasks 1–6 are reviewed. Commit
  `feat(nodes): add explicitly confirmed force maintenance modes`.

## Final verification and handoff

- [ ] Run `go test ./...`, `go test -race ./...`, `go vet ./...`,
  `go build -o /tmp/t9s-phase2 ./cmd/t9s`, `go mod tidy -diff`, `git diff --check`.
- [ ] In docs run `bun test`, `bun run typecheck`, `bun run build`.
- [ ] Review all six task reports and a whole-branch review; fix important findings.
- [ ] Add a manual checklist for reader-role Time permission behavior, CPU sampling,
  usage cancellation, namespace switching, service history, and force confirmation
  on disposable infrastructure. Leave live checks pending unless actually run.
- [ ] Report branch/commits and pending live checks. Do not push or merge without
  a request. Preserve the original checkout's Kubernetes roadmap changes.
