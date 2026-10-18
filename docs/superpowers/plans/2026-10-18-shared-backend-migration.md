# Shared Go Backend Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate every existing t9s feature to a reusable Go operations API and remove the legacy application reducer/effect runtime.

**Architecture:** Keep normalized domain values, narrow ports and SDK adapters. A concrete application Client opens independent Sessions exposing reads, streams and prepared actions. Bubble Tea owns presentation state, commands and result freshness; future React transport calls the same backend operations.

**Tech Stack:** Go 1.27.2 development/CI, module minimum Go 1.27.1; Talos machinery v1.14.4; Bubble Tea v2.0.6; Bubbles v2.1.0; Lip Gloss v2.0.6; existing testing/fake dependencies.

**Spec:** [Shared backend migration design](../specs/2026-10-18-shared-backend-migration-design.md)

**Status:** Approved by the user on 2026-10-18. Execute with Luna implementation workers and Sol reviewers in the isolated `refactor/shared-backend` worktree.

## Global Constraints

- Migrate all existing t9s functionality; remove the old application reducer/effect runtime before completion.
- Preserve current TUI appearance and keys during this architecture migration.
- Keep existing domain values, narrow ports, SDK adapters and corrected operation outcomes.
- Write permission is trusted session configuration, not a boolean in a mutation request.
- Frontends own replacement slots and request identities; sessions own backend cancellation and mutation exclusion.
- Preserve meaningful partial values, unknown state, confirmation policy and remote outcome uncertainty.
- No generic UI engine, command bus, registry, durable job service, new production dependencies, React implementation or HTTP server.
- No live cluster mutations, publishing, pushing, merging, or unrelated worktree cleanup.
- Preserve the current uncommitted work; record and reconcile its exact state before execution.

## Review Focus

1. A late session-open result, stream-open result, or same-scope read must not revive abandoned work or overwrite a newer view (Tasks 2, 6, 12).
2. A copied, zero, wrong-session or already-used prepared plan must not authorize a mutation (Tasks 7–11).
3. Missing target discovery, conflicting member ID/hostname, or changed disk identity must not bypass policy or broaden confirmed scope (Tasks 7, 9, 10).
4. Caller cancellation/response loss after dispatch must preserve unconfirmed outcomes and retain mutation exclusion through backend termination (Tasks 8–11).
5. Partial observations and independent completion order must not create false healthy states or erase Kubernetes enrichment (Tasks 4, 13).

## Execution and file ownership

Use `gpt-6-luna` implementation workers and `gpt-6.1-sol` reviewers. Give each worker only the spec, its task, prerequisite interface contracts, owned files and relevant tests. Reuse no large inherited conversation. A worker must not spawn agents or edit outside its assigned ownership.

Each task follows: failing behavioral regression; focused failure verification; minimal implementation; focused passing verification; Sol review and corrections; commit only task-owned changes after the baseline has been safely established. Do not commit preexisting user changes. Verification-only Task 1 and final Task 19 do not invent failing tests.

Tasks 3–6 may run concurrently after Task 2, with separate production/test files. Tasks 8–11 are sequential initially because they share mutation lifecycle and policy contracts; parallelize only if Sol verifies disjoint ownership. TUI tasks and CLI cutover are sequential: they share root routing, constructors and fixtures. Maximum three worker/reviewer slots plus the coordinator; do not leave unnecessary agents active.

Backend migration lands first alongside the old caller. TUI cutover then migrates every caller. Temporary build-preserving aliases/coexistence are allowed inside the branch and must have an explicit deletion in Task 18. Do not add a new facade that delegates operations to `Update` or `Runner`. Never expose both old and new mutation executors in the running CLI.

## API and file map

Keep `internal/application`. Existing policy enum names may be retained; existing pending-prompt and frontend state types are not public backend contracts.

| File | Responsibility |
| --- | --- |
| `application/client.go`, `session.go` | Context discovery, session construction, cancellation/close, immutable options |
| `application/read_diagnostics.go`, `read_cluster.go`, `read_resources.go` | Explicit typed reads; no presentation state |
| `application/streams.go` | Session-bound normalized streams and idempotent closure |
| `application/health.go` | Pure diagnosis/enrichment functions with explicit input |
| `application/action_plan.go`, `action_policy.go` | Typed requests/prepared plans, canonical identities, pure risk facts |
| `application/mutation.go` | Shared admission, one-shot consumption, deadlines and target outcomes |
| `application/node_actions.go`, `service_operations.go`, `etcd_actions.go`, `upgrade_operation.go` | Concrete prepare/execute workflows |
| `tui/state.go`, `requests.go`, `commands.go` | Presentation-only state, request stamps, direct command helpers |
| `tui/cluster_commands.go`, `diagnostic_commands.go`, `resource_commands.go`, `stream_commands.go`, `mutation_commands.go` | Feature call/result handling |
| Existing TUI feature files | Feature rendering/input; shared cluster snapshots passed explicitly |
| `cli/run.go` | Client construction and TUI-only launch options |

Use explicit result types. Shared supporting contracts:

```go
type Options struct { WritesEnabled bool }
type Client struct { /* private dependencies */ }
type Session struct { /* private runtime */ }
func NewClient(Dependencies) *Client
func (*Client) Contexts(context.Context) ([]domain.ClusterContext, error)
func (*Client) Open(context.Context, string, Options) (*Session, error)
func (*Session) Close() error

type Assessment struct {
    Targets []string
    Warning, Blocked, RequiredToken string
    SnapshotPath string
    ResetPreview []ResetPreview
}
type Confirmation struct { Approved bool; Token string }
type TargetResult struct {
    Target string
    Outcome domain.OperationOutcome
    Err error
    Warning string
}
type MutationResult struct {
    Targets []TargetResult
    Snapshot *domain.EtcdSnapshotResult
}
```

`Dependencies` retains only context catalog, session factory and Kubernetes resolver. Remove Runner injection callbacks after cutover. Prepared plans expose `Assessment() Assessment` by deep copy, keeping their canonical request/private lifecycle inaccessible. Validation errors use `errors.Is`: `ErrSessionClosed`, `ErrWritesDisabled`, `ErrMutationBusy`, `ErrInvalidPlan`, `ErrPlanUsed`, `ErrConfirmationRequired`, `ErrAssessmentChanged`, `ErrActionBlocked`, and `ErrCapabilityUnavailable`.

`NodeActionRequest` contains Kind, Targets, Image, optional Reset and DeviceWipe options. `ServiceActionRequest` contains Kind, Node, Service. `EtcdActionRequest` contains Kind, Node, MemberID, MemberHostname, SnapshotPath. Reuse normalized port option/enumeration types where they already fit. Snapshot is an etcd action kind. Upgrade uses a prepared node action, but its execution is streaming.

## Task 1: Record the baseline and regression inventory

**Files:** Read current dirty files and existing tests; create `docs/superpowers/reviews/2026-10-18-shared-backend-baseline.md`.
**Interfaces:** Produces the exact execution baseline and capability-to-test checklist; no product API.

- [ ] Record HEAD, branch, `git status --short`, diff summary and the ownership of current changes. Coordinate any active writer before touching shared files; never auto-stash/reset.
- [ ] Map every spec capability to current tests and its migration task. Include all node/service/etcd writes and stream variants.
- [ ] Run `go test ./...`, `go test -race ./...`, `go vet ./...` and record results. Preserve preexisting failures as such.
- [ ] Sol checks that the baseline includes operation timeout/outcome changes and that no capability is omitted.

## Task 2: Direct client and session lifecycle

**Files:** Create `internal/application/{client,session,session_test}.go`; modify `internal/testkit/fakes.go` only for required fake lifecycle hooks.
**Consumes:** `ports.ContextCatalog`, `ports.SessionFactory`, `ports.KubernetesResolver`, `ports.Session`.
**Produces:** Client/session signatures above; private `(*Session).operationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, error)`.

- [ ] Add `TestClientOpensIndependentSessions`, `TestSessionCloseCancelsInflightAndRejectsNewWork`, `TestSessionConcurrentCloseClosesAdapterOnce`, `TestOpenCanceledAfterAdapterReturnsClosesLateSession`, `TestSuccessfulOpenRetainsParentLifetime`, and `TestKubernetesResolutionCancellationClosesSession`. Assert one session cannot cancel another, caller cancellation reaches RPC contexts, and the adapter is closed once on lost open results.
- [ ] Run `go test ./internal/application -run 'Test(Client|Session|OpenCanceled)'`; confirm new tests fail before implementation.
- [ ] Implement context combination without frontend slot keys; immutable write settings. Open's context governs the returned session lifetime: successful return does not cancel it. Ordinary optional Kubernetes resolution errors degrade correlation; parent cancellation closes acquired resources and fails Open. Do not hold a mutex during network calls. Close must not wait indefinitely for arbitrary remote completion.
- [ ] Run focused tests and `go test -race ./internal/application`; Sol reviews lifecycle ownership and the close/open races before commit.

## Task 3: Node diagnostics as ordinary reads

**Files:** Create `internal/application/read_diagnostics.go`, `read_diagnostics_test.go`; reuse existing adapter/port implementations.
**Consumes:** Task 2 context lifetime helper.
**Produces:** Session methods `Processes(ctx context.Context,node string) (domain.ProcessSet,error)`, `Disks(ctx context.Context,node string) (domain.DiskSet,error)`, `Network(ctx context.Context,node string) (domain.NetworkSet,error)`, `Sockets(ctx context.Context,node string) (domain.SocketSet,error)`, `Mounts(ctx context.Context,node string) (domain.MountSet,error)`, `Memory(ctx context.Context,node string) (domain.MemorySnapshot,error)`, `CPU(ctx context.Context,node string) (domain.CPUSnapshot,error)`, `Time(ctx context.Context,node string) (domain.TimeSnapshot,error)`, `CheckTime(ctx context.Context,node,server string) (domain.TimeCheckResult,error)`.

Also produce `CurrentInstallImage(ctx context.Context,node string) (string,error)` for the upgrade prompt's backend-derived default. Test exact target routing, missing capability and cancellation with `TestCurrentInstallImageUsesSessionController`.

- [ ] Add `TestDiagnosticsUseCallerAndSessionContext`, `TestDiagnosticsPreservePartialValues`, `TestMissingDiagnosticCapabilityIsExplicit`, `TestCheckTimeValidatesBeforeRPC`; preserve CPU sample and time permission-denied tests.
- [ ] Run `go test ./internal/application -run 'Test(Diagnostics|MissingDiagnostic|CheckTime)'` and confirm failure.
- [ ] Implement direct methods with existing timeout/validation behavior; no Model, Message, Effect, background contexts or loading status.
- [ ] Run focused tests plus `go test ./internal/adapters/talos`; Sol reviews results and commits only diagnostic files.

## Task 4: Cluster observations and pure health

**Files:** Create `internal/application/read_cluster.go`, `read_cluster_test.go`; refactor `health.go`, `health_test.go`; preserve old callers temporarily using a narrowly named legacy helper if needed.
**Produces:** `NodeServices(context.Context) (domain.NodeSet,domain.ServiceSet,error)`, `Services(context.Context) (domain.ServiceSet,error)`, `Events(context.Context) (domain.EventSet,error)`, `Etcd(context.Context) (domain.EtcdSet,error)`, `KubernetesNodes(context.Context) (map[string]domain.KubernetesNodeSnapshot,error)` on Session. `Etcd` discovers control-plane targets itself; it never trusts UI-supplied targets. Pure `EvaluateObservations(HealthInput) []domain.Diagnosis`, where HealthInput contains `Nodes domain.NodeSet`, `NodesKnown bool`, `Etcd domain.EtcdSet`, `EtcdKnown bool`. Pure `EnrichNodes(domain.NodeSet,map[string]domain.KubernetesNodeSnapshot) domain.NodeSet`.

Move/retain existing pure `ClusterHealthRequestFromNodes(nodes []domain.NodeSnapshot, timeout time.Duration) domain.ClusterHealthRequest` and its private node IP selection helper from update.go without changing that signature. It emits control-plane/worker IP lists plus WaitTimeout; ordinary hostnames must not replace valid IPs. Keep control-plane target discovery for etcd alongside the cluster read operation.

- [ ] Add `TestClusterReadsAreIndependent`, `TestNodeServicesUseUncappedObservation`, `TestEtcdDiscoversTargetsWithoutUI`, `TestHealthUnknownObservationsStayUnknown`, `TestEnrichNodesDoesNotMutateInputs`, `TestClusterHealthRequestPreservesNodeIPSelection` and retain prior health evidence ordering.
- [ ] Run `go test ./internal/application -run 'Test(ClusterReads|NodeServices|EtcdDiscovers|HealthUnknown|EnrichNodes)'` and confirm failure.
- [ ] Preserve shared service fetch/fan-out and existing fallback for sessions lacking NodeServiceReader. Missing etcd targets must return a settled result/error, not perpetually loading state.
- [ ] Run focused health/cluster tests; Sol checks partial availability and Kube-first/nodes-first ordering before commit.

## Task 5: Resource browser methods

**Files:** Create `internal/application/read_resources.go`, `read_resources_test.go`.
**Produces:** Session `ResourceKinds(context.Context) (domain.ResourceKindSet,error)`, `Resources(ctx context.Context,node,kind string) (domain.ResourceInstanceSet,error)`, `Resource(ctx context.Context,node,kind,id string) (domain.ResourceInstanceSnapshot,error)`.

- [ ] Add `TestResourceReadsUseExplicitScope`, `TestResourceListDoesNotLoadYAML`, `TestResourceReadCancellation`; assert exact node/kind/ID reaches the fake, metadata-only lists and session cancellation.
- [ ] Run `go test ./internal/application -run 'TestResource(Read|List)'` and confirm failure.
- [ ] Implement methods preserving sensitive-kind handling and existing adapter boundaries.
- [ ] Run focused application tests and `go test ./internal/adapters/talos -run Resource`; Sol review and commit.

## Task 6: Direct stream lifecycle

**Files:** Create `internal/application/streams.go`, `streams_test.go`; existing port stream interfaces stay normalized.
**Produces:** Session `OpenServiceLogs(context.Context,domain.LogRequest) (ports.ServiceLogStream,error)`, `OpenDmesg(context.Context,domain.DmesgRequest) (ports.DmesgStream,error)`, `FollowEvents(context.Context) (ports.EventStream,error)`, `OpenClusterHealth(context.Context,domain.ClusterHealthRequest) (ports.ClusterHealthStream,error)`.

- [ ] Add `TestSessionCloseUnblocksStreamNext`, `TestStreamCloseIsIdempotent`, `TestCanceledOpenClosesReturnedStream`, and `TestCancelOneStreamKeepsOthersAlive`; include concurrent Next/Close under the race detector.
- [ ] Run `go test ./internal/application -run 'Test(SessionCloseUnblocks|StreamClose|CanceledOpen|CancelOneStream)'` and confirm failure.
- [ ] Implement only necessary lifecycle wrappers; reuse stream batch parsing and retention behavior. Context shutdown must close streams that require Close to release blocked I/O.
- [ ] Run focused tests, `go test -race ./internal/application`, and adapter stream regressions; Sol review and commit.

## Task 7: Typed action plans, identities and mutation admission

**Files:** Create `internal/application/{action_plan,action_policy,mutation,action_plan_test}.go`; extract existing pure policy from `actions.go`; preserve equivalent existing policy tests.
**Consumes:** Task 2 lifetime; Task 4 authoritative reads; Task 3 disk/time reads.
**Produces:** Request/result/assessment/error types above; `PreparedNodeAction`, `PreparedServiceAction`, `PreparedEtcdAction` with shared private one-shot state. Move existing ActionKind/ServiceActionKind/EtcdActionKind definitions into action_plan.go, retaining their enum values and old call sites until cutover. Private `(*Session).admit(ctx context.Context,plan *preparedState,confirmation Confirmation,timeout time.Duration) (context.Context,func(),error)` combines confirmation, session exclusion, one-shot consumption and operation lifetime; returned release is idempotent. Feature-specific fresh fact comparison runs while that lease is held.

- [ ] Add `TestPlanCopySharesOneShotState`, `TestPlanRejectsWrongSessionAndZeroValue`, `TestInvalidConfirmationDoesNotConsumePlan`, `TestBusyDoesNotConsumePlan`, `TestAdmittedAttemptConsumesBeforeSafetyRead`, `TestUnknownNodeCannotBecomeWorker`, `TestConflictingMemberIdentityIsRejected`.
- [ ] Run `go test ./internal/application -run 'Test(Plan|InvalidConfirmation|BusyDoes|Admitted|UnknownNode|ConflictingMember)'` and confirm failure.
- [ ] Deep-copy canonical requests and public assessments. Compare only confirmation-relevant identity/scope/risk. Closed/RO/used/invalid/busy/blocked paths cannot reach a controller. Plans bind to the originating Session, not a UI generation or global selected context.
- [ ] Run focused tests and existing pure quorum/reset/wipe policy tests; Sol checks the public contract before any action implementation proceeds.

## Task 8: Service actions through the shared API

**Files:** Create `internal/application/service_operations.go`, `service_operations_test.go`.
**Produces:** Session `PrepareServiceAction(context.Context,ServiceActionRequest) (*PreparedServiceAction,error)` and `ExecuteServiceAction(context.Context,*PreparedServiceAction,Confirmation) (MutationResult,error)`.

- [ ] Add `TestServiceExecutionNeedsBackendPermissionAndConfirmation`, `TestServicePlanCannotReplay`, `TestConcurrentActionFamiliesShareExclusion`, `TestServiceCanceledAfterDispatchIsUnconfirmed`.
- [ ] Run `go test ./internal/application -run 'Test(ServiceExecution|ServicePlan|ConcurrentActionFamilies|ServiceCanceled)'` and confirm failure.
- [ ] Move existing etcd stop/restart quorum policy and deadlines into direct operations; re-read required policy facts before dispatch, retaining exact node/service targeting and accepted-vs-completed distinctions. Do not invent a new protected-service denylist.
- [ ] Run service operation and adapter tests; Sol review and commit.

## Task 9: Node lifecycle, reset and device wipe

**Files:** Create `internal/application/node_actions.go`, `node_actions_test.go`; reuse reset/wipe policy and adapter implementations.
**Produces:** Session `PrepareNodeAction(context.Context,NodeActionRequest) (*PreparedNodeAction,error)`; `ExecuteNodeAction(ctx context.Context,plan *PreparedNodeAction,confirmation Confirmation,onTarget func(TargetResult)) (MutationResult,error)` for non-upgrade kinds. Nil observer is permitted. Observer calls are serialized, once per target, and must return promptly; TUI observers only enqueue into capacity-sized channels. Aggregate result contains every target even with observer nil. Reject upgrade kinds here; Task 11 owns their execution.

- [ ] Add `TestNodeActionsRecheckCurrentQuorum`, `TestBulkActionHoldsLeaseUntilAllTargetsFinish`, `TestResetNeverExpandsConfirmedDiskScope`, `TestWipeRejectsReusedDeviceIdentity`, `TestUnknownInventoryBlocksDestructiveAction`, and `TestBulkResultPreservesPerTargetUncertainty`; retain existing reset/wipe typed-token tests.
- [ ] Run `go test ./internal/application -run 'Test(NodeActions|BulkAction|ResetNever|WipeRejects|UnknownInventory|BulkResult)'` and confirm failure.
- [ ] Implement explicit target validation, fresh scope/identity/risk checks and existing deadlines. Observer prompt return is a trusted caller precondition; the backend does not promise to interrupt arbitrary callback code. Nil observer preserves all results. Do not automatically retry failures or infer remote failure from cancellation.
- [ ] Run node/reset/wipe focused tests and adapter controller tests; Sol reviews bulk concurrency, observer contract and destructive scope before commit.

## Task 10: Etcd snapshot, maintenance and membership workflows

**Files:** Create `internal/application/etcd_actions.go`; modify/migrate existing `etcd_actions_test.go` and existing membership, safety, leadership and snapshot tests without overwriting them.
**Produces:** Session `PrepareEtcdAction(context.Context,EtcdActionRequest) (*PreparedEtcdAction,error)` and `ExecuteEtcdAction(context.Context,*PreparedEtcdAction,Confirmation) (MutationResult,error)`. Pure default path helper takes context/member names and an explicit `time.Time`.

- [ ] Add `TestSnapshotPlanCannotReplay`, `TestMembershipWaitsForVerifiedSnapshot`, `TestMembershipRechecksAfterSnapshot`, `TestMembershipNeverFollowsChangedMemberIdentity`, `TestMembershipRetainsLeaseAcrossStages`, `TestForfeitUsesMemberNodeAndNeedsNoSnapshot`, and `TestEtcdLocalPathRemainsProcessLocal`.
- [ ] Run matching tests and confirm failure.
- [ ] Move orchestration completely into application methods; retain atomic/checksummed/no-overwrite adapter behavior, write gate, exact routing, voter/learner policy, unknown-health blocks and current deadlines. Return snapshot metadata even if a subsequent membership stage fails. Validation failures require a fresh plan after admission.
- [ ] Run focused application and Talos etcd tests; Sol reviews stage ordering, post-snapshot safety and error/outcome semantics before commit.

## Task 11: Upgrade execution and terminal ownership

**Files:** Create `internal/application/upgrade_operation.go`, `upgrade_operation_test.go`; modify `internal/ports/node.go`, `internal/adapters/talos/node_controller.go`, its upgrade/controller tests, `internal/testkit/fakes.go` and every other existing UpgradeStream implementation/fake found by reference search.
**Produces:** Session `ExecuteUpgrade(context.Context,*PreparedNodeAction,Confirmation) (ports.UpgradeStream,error)`; preparation is Task 9's upgrade branch. Extend `ports.UpgradeStream` with `Wait(context.Context) (ports.UpgradeResult,error)`. Wait is repeatable/concurrent; result.Err is the operation error, the return error is local waiting failure. It completes only after adapter recovery cleanup and producer termination, even without a Results consumer. Stream admission/revalidation follows the shared plan contract; asynchronous rejection is a retained terminal result before a write occurs.

- [ ] Add `TestUpgradeRequiresPreparedCurrentImageAndPolicy`, `TestUpgradeLeaseLivesThroughCleanup`, `TestUpgradeTerminalDoesNotDependOnUIConsumption`, `TestUpgradeWaitRetainsRecoveryResultAfterCancellation`, `TestUpgradeWaitIsRepeatableAndConcurrent`, `TestUpgradeCanceledReaderDoesNotLeak`, `TestUpgradePreservesRecoveryIncomplete`, and retain fallback/cordon ownership tests. Fill/leave progress unread in the terminal tests; do not use a second progress reader as a workaround.
- [ ] Run matching tests and confirm failure.
- [ ] Make producer terminal publication independent of bounded progress consumption. Cancel/session shutdown request cancellation only; retain final result and release exclusion after actual producer/cleanup completion. A local completion watcher may use an uncanceled context solely to await the producer; it is never an RPC context. Close itself remains bounded. Preserve accepted/applied/recovery-warning outcomes and map canceled dispatched work to domain unconfirmed results where remote completion is unknown. Never trigger recovery writes from node/Kubernetes reads.
- [ ] Run upgrade application and adapter tests plus race checks; Sol reviews stream cancellation/terminal buffering and acceptance semantics before commit.

## Task 12: TUI request ownership and direct command foundation

**Files:** Create `internal/tui/{direct_model,state,requests,commands,requests_test}.go`; modify `model.go`, `view_lifecycle.go`, `startup_test.go` only under this task's sequential ownership.
**Produces:** TUI `LaunchOptions{ContextName,NodeFocus string; OpenContextPicker bool; WritesEnabled bool}`; private request stamp `{generation,id uint64}` and per-slot caller context/cancel; direct command helpers wrapping concrete Session calls. Private `newDirectModel(context.Context,*application.Client,LaunchOptions) directModel` constructs the replacement root for tests. The CLI still constructs only the existing legacy model until Task 18. Direct root never calls the reducer/Runner; temporary aliases in state.go let existing feature renderers compile while call sites migrate. Task 18 deletes old root/aliases and promotes directModel to model; no runtime feature flag or two-executor mode is introduced.

- [ ] Add `TestDirectStartupRendersBeforeIO`, `TestDirectRequestRejectsSameNodeOlderResult`, `TestDirectContextSwitchClosesLateSessionOpen`, `TestDirectViewExitCancelsUnstartedCommand`, `TestDirectLateStreamOpenIsClosed` and `TestDirectTeardownCoversRuntimeFailure`.
- [ ] Run `go test ./internal/tui -run 'TestDirect'` and confirm failure.
- [ ] Implement local presentation state and request ownership. Session opening uses a dedicated persistent session context, not a read slot canceled on unary completion. Define exactly one store for shared cluster snapshots. Root routes commands/results; backend never receives UI slot names. Preserve nonblocking first frame and fallback cleanup. List each temporary alias for Task 18 deletion.
- [ ] Run focused tests/race checks; Sol reviews the cutover mechanism and confirms two mutation runtimes cannot become simultaneously active in the CLI.

## Task 13: Cluster screens, context picker and health views

**Files:** Create `internal/tui/cluster_commands.go`; modify `nodes.go`, `nodes_table.go`, `services.go`, `events.go`, `etcd.go`, `contexts.go`, `overview.go`, `problems.go`, `metadata.go` and their tests; sequential `model.go` routing edits.
**Consumes:** Tasks 2, 4, 12; cluster snapshots are TUI-owned once.
**Produces:** Direct command/result paths for every cluster/root view, plus pure health/enrichment projection.

Dispatch as four sequential Luna subtasks, each with focused tests and Sol review: **13a** context/startup/shared snapshot owner; **13b** nodes/services; **13c** events/etcd snapshots; **13d** overview/problems/metadata. One worker owns root routing at a time. Convert relevant test fixtures/call sites in both roots so the legacy CLI still compiles; direct tests use newDirectModel.

- [ ] Add/port tests for independent completion/failure, Kube-before-nodes and nodes-before-Kube, partial health, shared uncapped services, selection by stable identity, context reset of marks/prompts, initial node focus and kube-context picker behavior.
- [ ] Run focused new/ported tests before implementation and verify failure against direct startup.
- [ ] Replace application.Update calls for these flows; pass shared snapshots into render/selection helpers and remove mirrored `.state` storage where it would duplicate the cluster owner. Preserve `:nodes/:services/:events/:etcd/:overview/:problems/:contexts` aliases and badges.
- [ ] Run owning TUI tests and visual goldens unchanged; Sol reviews snapshot ownership and navigation before commit.

## Task 14: All diagnostic and resource screens

**Files:** Create `internal/tui/{diagnostic_commands,resource_commands}.go`; modify `processes.go`, `disks.go`, `network.go`, `netstat.go`, `mounts.go`, `memory.go`, `cpu.go`, `time.go`, `time_prompt.go`, `resources.go`, `resource.go`, `detail.go`, corresponding tests and sequential root routing.
**Consumes:** Tasks 3, 5, 12.
**Produces:** Direct reads and view-local presentation results for every node diagnostic and resource scope.

Dispatch as four sequential Luna subtasks: **14a** processes/disks/network; **14b** sockets/mounts/memory; **14c** CPU/time; **14d** resource kinds/list/detail. Each owns corresponding feature files, root call sites and tests only, with its own focused test cycle and Sol gate.

- [ ] Port/add tests for every screen's loading/partial/failure/empty results, time validation/permission failures, CPU sample cancellation, selected-row detail, A-B-A resource kind and detail selection, canceled work and preserved cursor/filter state.
- [ ] Run affected screen tests against direct startup and verify missing paths fail.
- [ ] Wire ordinary backend calls; remove old result application and duplicate screen synchronization for each feature. Keep metadata-only lists, selected YAML loading and sensitive display handling.
- [ ] Run all affected screen tests and goldens; Sol reviews full capability coverage before commit.

## Task 15: Stream screens and view lifecycle

**Files:** Create `internal/tui/stream_commands.go`; modify `logs.go`, `events.go`, `healthcheck.go`, `resources.go` only where stream/viewports require it; root/view lifecycle and corresponding stream tests.
**Consumes:** Tasks 6, 12.
**Produces:** Direct open/next/close command loops for logs, dmesg, followed events and cluster health.

- [ ] Port/add stream tests covering follow/pause, wrapping/filter/clear/reconnect, clean EOF vs failed health verdict, bounded retention, canceled late batches, closed late opens and all view-exit paths (Esc, q, palette navigation, context switch, quit).
- [ ] Run matching tests against direct startup and verify missing flows fail.
- [ ] TUI stores stream display state; commands read Next off the update loop; cleanup closes exactly once. Resize/content updates preserve actual scroll position. Keep terminal sanitization and upstream framing fixes.
- [ ] Run stream TUI and adapter tests/race checks; Sol reviews lifetime and EOF behavior before commit.

## Task 16: All mutation prompts and outcomes

**Files:** Create `internal/tui/mutation_commands.go`; modify action/upgrade/reset/disk-wipe/snapshot prompt files, `footer.go`, `actions.go`, action flow tests, sequential root routing.
**Consumes:** Tasks 7–12.
**Produces:** Frontend-owned prompt state containing typed prepared plans; direct execution/results/progress for every mutation.

Dispatch as four sequential Luna subtasks: **16a** service and basic node lifecycle; **16b** reset/device wipe; **16c** snapshot/etcd maintenance/membership; **16d** upgrade/current-image default/progress. Keep per-subtask ownership/tests/review explicit; never send one Luna worker the entire mutation TUI.

- [ ] Port/add TUI tests for every action family, rejected preparation, explicit y/n or typed intent, stale assessment requiring reprepare, RO mode, busy state, duplicate confirm, bulk intermediate outcomes, snapshot-then-membership and terminal upgrade warnings.
- [ ] Run action tests against direct startup and verify missing routes fail.
- [ ] Prompt cancel discards local plans; accepted operation is session-scoped, not view-scoped. Keep read/navigation available and disable new write prompts while active. Observer callbacks enqueue per-target results without blocking; late results cannot overwrite a newer session/operation display.
- [ ] Run full TUI action tests and backend mutation tests; Sol reviews every action against the spec table before commit.

## Task 17: Simplify root routing and shared interactions

**Files:** `internal/tui/model.go`, `navigation.go`, `view_lifecycle.go`, `command.go`, feature command files and relevant interaction tests.
**Consumes:** Tasks 13–16.
**Produces:** One direct Bubble Tea root owning active screen/overlay dispatch, with small feature handlers.

- [ ] Port input-precedence tests: active prompt first, then palette/filter/active screen; preserve command aliases, keys, root quit vs child back, splash skip and automatic refresh.
- [ ] Confirm tests fail for any missing direct-root routes, then finish routing. Extract repeated filter editing only where it removes real duplication. Keep listControl and useful Bubbles components; do not introduce screen descriptors/registries or a plugin interface.
- [ ] Run all TUI tests and visual goldens; Sol reviews that root splits reflect responsibilities and do not merely move one huge switch unchanged.

## Task 18: CLI cutover and complete legacy deletion

**Files:** `internal/cli/run.go`, `run_test.go`, TUI constructors/tests; delete old `application/{model,update,dispatch,effects,requests,operations,batch,snapshots,events_follow}.go` after remaining useful functions have moved; remove obsolete feature reducer portions in CPU/time/actions/service files; delete `tui/sync.go` and temporary aliases/startup paths. Preserve policy and newly extracted direct-operation files.
**Consumes:** Complete direct backend and TUI from Tasks 2–17.
**Produces:** `tui.NewWithCleanup(context.Context,*application.Client,LaunchOptions) (tea.Model,tea.Cmd)` as the CLI construction API. No production legacy reducer, Message, Effect or Runner references.

Delete the old model runtime and promote directModel to model (including its private constructor/tests). Replace every state.go legacy alias with its concrete TUI-owned struct. Remove temporary legacy health wrappers, all old constructor plumbing and obsolete startup/test helpers. Retain moved ClusterHealthRequestFromNodes and current-image read paths. Search each temporary alias recorded in Task 12, not just exported reducer names.

- [ ] Port CLI launch, write-env parsing, picker, initial node focus, startup cancellation and terminal runtime failure tests to direct construction; first verify the old composition fails the direct-only assertions.
- [ ] Switch CLI once; delete legacy runtime and temporary bridges, migrating useful tests to behavior-equivalent direct API tests. Do not delete safety tests merely because they used reducer fixtures.
- [ ] Search production code with `rg -n 'application\.(Update|Model|Message|Effect|Runner|NewModel|NewRunner)|syncScreens|syncAllScreens' internal cmd`; expected no matches. Inspect definitions too: no aliases or renamed facade may retain the old engine.
- [ ] Run `go test ./...` and `go test -race ./...`; Sol reviews deletions and constructor lifecycle before commit.

## Task 19: Second-caller proof, documentation and final review

**Files:** Create `internal/application/reuse_test.go` in external package `application_test`; update `CONTRIBUTING.md` and relevant security/architecture documentation. Create final review/evidence record under `docs/superpowers/reviews/`.
**Produces:** Evidence that normalized operations/policy are frontend independent and every capability migrated.

- [ ] Add `TestPlainGoCallerUsesReadsActionsAndStreamsWithoutTUI`: construct client with port fakes, open session, read a diagnostic, prepare/execute service action, consume/close a stream and close the session. Assert identical safety enforcement without any prompt or TUI construction.
- [ ] Verify `go list -deps ./internal/application` contains neither `charm.land/` nor `/internal/tui`; inspect public types to confirm no frontend state or SDK credential types.
- [ ] Update documentation to reflect direct operations, prepared-plan lifecycle, session-local exclusion, cancellation/uncertain remote outcomes and future web transport responsibility for local snapshot paths. Explain backend option trust and observer callback requirements.
- [ ] Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build -o /tmp/t9s-shared-backend ./cmd/t9s`; run `bun test` in `docs` if its script-bearing files changed. Record exact commands/results and architecture reference searches.
- [ ] Compare the final capability checklist to baseline, inspect diff/deleted machinery and net production/test LOC. Explain material growth, but do not use a line-count target to remove safeguards.
- [ ] Fresh Sol whole-change review checks correctness, policy, concurrency, framework fit, architecture and unnecessary abstractions. Luna fixes findings in bounded tasks; rerun checks justified by those changes. Report live compatibility as unverified unless the separate smoke test was actually performed.

## Handoff

The user has already selected the execution method: Luna implementation, Sol review. Ask only for review of the written design and plan, not for model selection again. Implementation starts after that review. The coordinator must stop a worker that silently broadens its task or preserves legacy machinery beyond Task 18.
