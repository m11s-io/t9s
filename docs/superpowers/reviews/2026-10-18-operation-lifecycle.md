# Operation lifecycle audit

Reviewed: 2026-10-18, local main at `5e4640e`.

Scope: source inspection of application effects, action dispatch, session
teardown, Talos controllers, snapshot persistence, and upgrade streaming.
The pinned Talos machinery v1.14.4 client source was also inspected. No tests,
benchmarks, live requests, or cluster mutations were performed for this audit.
Findings describe observable control flow; runtime scenarios below remain to be
reproduced before implementation changes.

### Follow-up implementation

Branch `fix/operation-ownership` addresses finding 1: accepted mutations retain
their target counts independently of prompts, conflicting write requests and
confirmations are ignored while active, and membership progress no longer
intercepts read-only navigation. Async prompt lookups are invalidated on
acceptance. Existing operator cordon behavior is unchanged.

The existing prompt-replacement test was updated to the new contract. No tests
were added or run in this follow-up. Compilation succeeded with
`go build -o /tmp/t9s-operation-ownership ./cmd/t9s`; a focused Luna medium source
review found two issues (repeated target counts and membership prompt rendering),
both corrected before its final review. Runtime and live-cluster validation
remain pending.

The next slice adds per-target deadlines (control 30 seconds; snapshot and
defragment 10 minutes; reset/wipe 30 minutes), a 20-minute pull/install budget,
and 30-second upgrade preparation/cordon/reboot budgets. Existing readiness,
drain, and cleanup budgets remain separate. Normalized results distinguish
accepted requests, observed completion, unconfirmed outcomes, and incomplete
recovery. Legacy upgrades preserve acceptance; raw mutation/snapshot errors are
not copied into presentation state. The snapshot cancellation comment is
corrected. Initial budgets still need runtime validation; existing assertions
were adjusted, with no new tests or test runs. Finding 3 (joined cleanup
lifetime) and typed member confirmation remain follow-up work. Finding 4's
outcome distinctions are present; finer safe rejection categories remain future
work. The inventory and findings below describe the reviewed baseline.

## Assessment

Keep the existing domain/port/adapter boundaries and Bubble Tea command adapter.
Request identities, explicit read deadlines, bounded service fan-out, metadata-only
resource lists, and stateful log decoding are useful improvements. The next work
should address mutation ownership and outcomes before expanding write workflows.

### Current lifecycle inventory

All operations below receive a context bound to the frontend lifecycle and
session through `application/operations.go`. Session replacement and runner close
cancel that context and close the Talos client. Cancellation is not rollback.

| Operation | Explicit client deadline | What success currently establishes |
| --- | --- | --- |
| Reboot, shutdown, rollback | None added by t9s | Controller RPC returned successfully; no subsequent readiness/power verification |
| Reset, device wipe | None added by t9s | Controller RPC returned successfully; wipe triggers a disk refresh |
| Service start/stop/restart | None added by t9s | Controller RPC returned successfully; no separate desired-state wait |
| Etcd snapshot | None added by t9s | Stream copied, checksum verified, file synced and committed without overwrite; directory sync is best effort |
| Etcd remove/leave | None added by t9s | Mandatory snapshot succeeded, then membership RPC returned successfully; etcd refresh follows |
| Etcd defragment/disarm/forfeit | None added by t9s | Maintenance RPC returned successfully; etcd refresh follows |
| Legacy OS upgrade | Capability query: 15 seconds; upgrade RPC: no t9s deadline | Legacy upgrade accepted, not observed installation/reboot completion |
| Lifecycle OS upgrade | Capability query: 15 seconds; drain: 5 minutes; each readiness wait: 25 minutes; cleanup: separate 25 minutes | Lifecycle installation, reboot, Talos readiness and Kubernetes readiness completed; cleanup-only failure is a retained warning |

The lifecycle pull/install streams, maintenance preparation, cordon and reboot
do not have their own t9s deadlines. The two readiness waits each get their own
budget; `upgradeRecoveryTimeout` is not a shared total recovery deadline.

Evidence: `application/actions.go` (`actionEffect`, upgrade bridge),
`application/service_actions.go`, `application/effects.go` (`runEtcd*`),
`adapters/talos/node_controller.go`, `service_controller.go`, and
`etcd_operations.go`. SDK methods forward supplied contexts to RPCs; passing a
cancelable context does not itself establish a deadline.

## Findings and next changes

### 1. Important: prompt identity also replaces active operation visibility

`update.go` increments the single `operationID` when a new node, service, or etcd
prompt is opened. `dispatch.go` drops results whose operation ID is no longer
current. `operationEffect` does not cancel an older RPC when that ID changes.
Only upgrades have a general active-operation write gate.

Consequently, a new prompt can hide completion of an earlier mutation while that
mutation continues. It can also cause the result of an in-flight membership
backup to be ignored, preventing its next stage from being dispatched. This is
safe against accidentally continuing the wrong pipeline, but operation progress
and prompt state have become coupled.

Recommended first implementation: track accepted mutation activity separately
from prompts. Reject a conflicting write while it is active, retain its result
until completion, and allow read-only navigation. Keep one accepted bulk action
with per-target results. A new prompt must not change an executing operation's
identity. Avoid a job scheduler or persistent operation registry for this scope.

Regression scenarios: open a second prompt while a fake service RPC blocks;
attempt another write during the membership snapshot stage; finish a multi-target
action out of order; change context while work is active. Assert both calls made
and retained outcomes, not only stale-result rejection.

### 2. Important: most mutations can wait indefinitely

The shared operation wrapper uses `WithCancel`, not `WithTimeout`. The ordinary
controllers and etcd operations add no timeout. A responsive terminal can
therefore retain an indefinitely blocked operation until session exit.

Add explicit budgets near concrete operation execution: short control RPCs,
snapshot transfer, defragmentation, and upgrade stages need separate choices.
Record initial values and validate them on a disposable cluster; do not copy the
15-second diagnostic timeout onto every mutation. Keep the caller's shorter
deadline. Do not add automatic retries for non-idempotent mutations.

Regression scenarios: fake RPC observes a deadline and cancellation; timeout
completes local operation state; snapshot timeout prevents membership mutation;
install-stream stall is bounded. A timeout after submission must report an
unconfirmed remote outcome and require observation before retry.

This follows upstream [gRPC deadline guidance](https://grpc.io/docs/guides/deadlines/),
which recommends explicit deadlines selected using measured behavior.

### 3. Important: detached upgrade cleanup has no joined lifetime

`runSafeUpgradeMaintenance` deliberately uses `WithoutCancel` plus a 25-minute
timeout to restore a cordon created by t9s. `Uncordon` uses its own Kubernetes
clientset; closing the Talos client does not directly close that clientset.
However, the application stream bridge exits on cancellation, and runner
teardown does not wait for the adapter goroutine or retain its final cleanup
outcome. Context switching can leave old-cluster cleanup running without a
visible final result. Process exit can terminate it before completion.

Choose and document one bounded shutdown policy. A simple option is to retain
ownership of the active upgrade until its cleanup completes or a short shutdown
budget expires, then show an explicit manual-recovery warning. Do not promise
cleanup survives process exit, and do not reintroduce display-driven uncordon.
Preserve existing operator cordons.

Regression scenarios: cancel after t9s cordons, block uncordon, observe when
cleanup ends and when shutdown returns; verify existing cordons are untouched;
ensure cancellation cannot silently remove the only recovery notice.

### 4. Important: terminal outcomes lose useful distinctions

Ordinary actions normalize RPC success into `ActionSucceeded`. Legacy upgrade
emits “legacy upgrade accepted”, but the terminal message takes the ordinary
upgrade-success path. `safeUpgradeError` discards every underlying error and
returns “upgrade failed”. Recovery warnings are retained for readiness and
cleanup failures, but failures after installation and before those waits can
still become a generic failure.

Retain small structured outcome facts: request accepted, completion observed,
recovery incomplete, or remote outcome unconfirmed. Retain the last meaningful
stage. Map safe error categories to operator messages without exposing raw SDK
errors, endpoints, or credentials. A canceled/disconnected RPC must not imply
that a remote reset, wipe, reboot, or installation was undone.

Regression scenarios: legacy acceptance is not labeled verified completion;
cancel after install; reboot RPC returns an ambiguous transport error; readiness
fails; cleanup-only failure. Verify normalized data and final operator text.

Upstream [gRPC cancellation guidance](https://grpc.io/docs/guides/cancellation/)
describes cancellation as a signal; it does not provide transactional rollback.

### 5. Small correction: snapshot cancellation comment overstates its guarantee

`contextReader.Read` checks cancellation before delegating to `reader.Read`.
It cannot interrupt an arbitrary read already blocked inside that call. The
production SDK stream is created with the same context, which provides the
transport cancellation path. Update the comment when touching snapshot code;
exercise a blocking stream in its cancellation regression. Local filesystem
write/sync operations are also not made interruptible by this wrapper.

Preserve the current checksum validation, exclusive staging creation, atomic
no-overwrite commit, and partial-file cleanup.

## Proposed delivery slices

1. Separate accepted operation activity from pending prompts, with a simple
   conflict gate and retained terminal result. Include typed etcd member confirmation.
2. Introduce per-operation/stage deadlines and explicit uncertain/accepted
   outcomes. Keep frontend formatting outside adapters and policy inside Go.
3. Define bounded upgrade cancellation/cleanup behavior and exercise it through
   the frontend lifecycle. Then validate against disposable infrastructure.

Implement each slice with focused regressions after its behavior is agreed.
There is no need for an event bus, background daemon, HTTP server, or general
operation framework to address these findings.

## Disposable-cluster validation record

Status: pending. A disposable cluster and its supported Talos/Kubernetes version
range have not been supplied for this audit. Do not use a developer's current
context as an implicit test target.

Record commit, Talos/Kubernetes versions, credential role, scenario, observed
result, and sanitized evidence. Keep identities and credentials out of the repo.

- Read-only: startup with a slow/unreachable member; context switch during reads;
  log/event stream exit; split/long log lines; resource-kind changes; CPU refresh;
  ordinary time status and denied explicit time check using restricted credentials.
- Writes on explicitly authorized disposable infrastructure: service action;
  snapshot checksum and cancellation; typed membership confirmation and mandatory
  snapshot failure; upgrade readiness/cleanup warning; cancellation after cordon.
- Establish outcomes by rediscovery. Reconnection alone is not completion.
- Record hosted CI separately; local success does not establish hosted CI or
  live compatibility. Release only after supported paths have evidence.
