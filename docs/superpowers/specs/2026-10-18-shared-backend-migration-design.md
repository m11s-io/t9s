# Shared Go backend migration design

Status: approved by the user on 2026-10-18; implementation in progress on `refactor/shared-backend`.

## Purpose and scope

Migrate all existing t9s functionality to ordinary Go application operations that the Bubble Tea interface and a future React backend can both call. React is a committed future interface. Keep the current TUI working throughout migration and remove the old application reducer/effect runtime before completion.

Success means reusable workflows and policy, not merely reusable SDK wrappers. Prefer fewer concepts, explicit feature methods, and cohesive files. Do not replace Bubble Tea, introduce a universal UI engine, or preserve the current reducer behind a facade.

This migration includes every current capability. Building React, HTTP endpoints, authentication, browser sessions, download delivery, or production web hosting is subsequent work. Those transport decisions must not become requirements of the shared Go operations. Snapshot paths currently refer to the Go process's filesystem; a future web adapter must supply its own storage/download policy.

## Baseline and existing work

Current inspected baseline: `main` at `8031b90` (`fix: retain mutation ownership and bound operation requests`). During planning, the preceding `fix/operation-ownership` work based on `5e4640e` was committed and the checkout switched to main externally. That work includes operation outcome distinctions, mutation deadlines, request identities, mutation exclusion, and upgrade recovery warnings. Before implementation, record the exact HEAD/diff and reconcile concurrent work; never reset, stash, overwrite, or commit another task's files automatically.

The earlier `2026-10-18-architecture-simplification.md` describes a smaller completed refactor. This design supersedes its retained application-state/reducer boundary, but preserves its corrected behavior. No existing untracked binary or review document is part of this migration.

## Chosen architecture

Keep `internal/domain`, `internal/ports`, `internal/adapters/talos`, `internal/adapters/kubernetes`, `internal/application`, `internal/tui`, and `internal/cli`. Do not rename the entire package tree.

```text
Bubble Tea screens                  Future React interface
        |                                    |
     tea.Cmd                          HTTP/stream adapter
        |                                    |
        +------------- application ----------+
                       Client / Session
                    reads, policy, actions
                             |
                           ports
                             |
                   Talos / Kubernetes adapters
```

`application.Client` owns immutable configured dependencies and context discovery. It opens independent `application.Session` objects. It has no global "currently selected cluster"; two callers can use different sessions without interfering. Write permission is trusted configuration when opening the session, not a boolean accepted on each mutation request.

The context passed to `Open` is the returned session's lifetime parent, not just an opening deadline. The TUI creates a dedicated context per selected session and keeps it alive after Open returns; it never uses an auto-finished unary read slot for session opening. Cancellation during opening wins even if an adapter returns successfully; close that late session. Optional Kubernetes resolution errors degrade correlation, but parent cancellation fails opening and closes acquired resources.

`application.Session` owns one SDK session, its optional Kubernetes correlation reader, its cancellation context, and mutation exclusion. It exposes explicit synchronous methods for reads and actions, and typed stream handles for streams. Operations accept caller contexts. No application method accepts a Bubble Tea message, view name, screen cursor, prompt pointer, or frontend request generation.

The TUI owns loaded presentation state, typed result messages, selected context, view navigation, input/prompt focus, filters, cursors, and viewport state. `tea.Cmd` calls the application directly and returns typed `tea.Msg` values. No effect runner or second reducer is needed.

Keep the existing small `listControl`, table rendering, Bubbles text inputs, and useful viewports. Preserve current appearance and keys during this architecture migration. Header redesign and k9s visual parity changes are separate product decisions.

## Ownership and concurrency

Session cancellation and frontend request replacement solve different problems:

- The backend derives every operation context from both the caller and its session. Session close cancels all its reads, streams, and accepted operations, rejects new work, and closes the underlying SDK session exactly once.
- The frontend owns independently replaceable read slots. Replacing/closing a screen request cancels that caller context and increments its request identity. Results carry session generation plus request ID; cancellation alone never establishes freshness.
- UI screen names and request-slot strings do not enter the backend. The backend does not maintain a second map of UI request identities.
- Read results can finish in any order. Services/events/Kubernetes reads are independent; etcd target discovery has an explicit node dependency.
- One accepted mutation at a time per application session preserves current behavior. Reads and navigation continue. A caller cannot bypass this by calling a different mutation method. A bulk mutation and a snapshot-to-membership operation hold exclusion until every target/stage has ended.
- Closing a prompt or leaving a view never cancels an accepted mutation. Context/session exit retains the existing cancellation behavior. Cancellation does not imply rollback or establish that the remote mutation did not occur.
- Progress is delivered without goroutine leaks when the caller exits. Stream close is idempotent and may run concurrently with `Next`. A terminal mutation outcome releases exclusion even if the frontend stops displaying progress.

Do not add durable jobs, cross-process locking, a global event bus, automatic remote retries, or persisted operation storage. Different independently opened sessions cannot provide a distributed cluster lock; future multi-user coordination is a separate requirement.

## Read and stream contract

Read methods return existing normalized domain values and Go errors. Preserve meaningful partial values alongside errors; do not replace unknown values with zero-valued "healthy" data. Loading/idle statuses are frontend state.

| Group | Complete capability coverage |
| --- | --- |
| Discovery | Context catalog, selected-context session open, optional Kubernetes Node correlation |
| Cluster snapshots | Combined node/service observation, standalone services, events snapshot, etcd membership/status, Kubernetes nodes |
| Node diagnostics | Processes, disks, network links, sockets, mounts, memory, CPU sampling, time read and time-server check, current install image for upgrade prefill |
| Resources | Resource kinds, metadata-only instance list, selected instance YAML |
| Streams | Service logs, dmesg, event following, cluster-health progress |
| Derived views | Overview/problems, evidence and health diagnoses, Kubernetes enrichment |

Retain shared uncapped service observations for health and detailed services, bounded adapter fan-out, stateful log byte decoding, record-oriented dmesg handling, bounded stream retention, and metadata-only resource lists. Reuse existing normalized port stream interfaces where their lifecycle contract fits; add only the session lifetime wrapper required for cancellation/close.

Health evaluation becomes a pure function over explicit node/etcd observations and availability. It does not accept a TUI model or require terminal load-status types. Kubernetes enrichment is likewise a pure merge of explicit observations; independent arrival order must remain safe.

Preserve normalized cluster-health request construction, including control-plane and worker IP selection currently supplied by `ClusterHealthRequestFromNodes`; it remains a pure application helper over explicit node data.

Timeout defaults retain the current implementation: replaceable unary reads 15 seconds, control operations 30 seconds, snapshots and defragmentation 10 minutes, reset/device wipe 30 minutes. Caller deadlines can shorten these. Preserve existing CPU sampling, time checking, cluster-health, and upgrade-specific behavior rather than imposing a new universal timeout on streams.

## Mutation contract and preserved policy

Use explicit node, service, etcd, snapshot, and upgrade operation methods. Share private lifecycle and policy helpers; do not introduce a generic command bus or configurable workflow engine.

Both assessment and execution live in the application. Assessment returns backend facts, warnings, hard blocks, target/scope information, and any required typed confirmation. Prompts and their navigation remain frontend state. Execution independently enforces write permission, session identity, supported action/options, current safety information, and required confirmation. UI snapshots never authorize a mutation.

Use three typed prepared-plan families: node, service and etcd. Upgrade uses a prepared node plan; snapshot is an etcd action. A plan has a private canonical request, originating session, confirmation-relevant facts and a shared one-shot state pointer. Its public assessment is read-only by value/deep copy. Zero/forged plans are invalid, and copying a plan cannot copy its replay state. No token registry or backend pending prompt is needed.

Execution validates session ownership, lifetime, explicit approval and required typed token, acquires session mutation exclusion, then consumes the plan before fresh policy I/O or side effects. Invalid confirmation or busy rejection leaves the plan usable; an admitted attempt consumes it even if subsequent validation fails. Fresh execution reads reject changed canonical identity, destructive scope, or confirmation-relevant risk with a typed stale-assessment error; a newly blocked action remains blocked. A caller prepares again to retry. Compare relevant facts, not whole-snapshot hashes or volatile timestamps/raft indexes.

Prepare resolves targets from backend data. Missing discovery cannot silently classify a node as a worker. A supplied etcd member ID and hostname must identify the same member. Reset captures actual disk scope and available stable identity attributes; fresh execution never silently adds disks or follows a reused device name. Membership execution owns snapshot, fresh post-snapshot membership/quorum validation, and mutation under one lease. The public plan assessment is a local Go contract; future web serialization and server-side plan handles belong to its transport adapter.

| Mutation group | Required preserved behavior |
| --- | --- |
| Node lifecycle | Reboot, shutdown, rollback; explicit targets, existing warnings and quorum hard blocks; independent bulk target outcomes |
| Reset | Explicit options and actual disks, current inventory and quorum checks, exact single/bulk typed intent, no guessed wipe scope |
| Device wipe | Current exact device identity, system/read-only restrictions, method validation, exact typed device confirmation |
| Service | Start/stop/restart on the exact node/service, existing etcd stop/restart quorum policy and current action warnings |
| Etcd snapshot | Write gate, validated process-local destination, atomic no-overwrite output and checksum validation |
| Etcd maintenance | Defragment, alarm disarm, leadership transfer, correct member-node routing and deadlines |
| Etcd membership | Remove/leave; voter/learner and availability policy; required successful snapshot before membership RPC; hold mutation exclusion across stages |
| Upgrade | Image/version checks, supported lifecycle/fallback behavior, progress, Kubernetes maintenance and cordon ownership, persistent terminal recovery warning |

Preserve distinctions among request accepted, observed completion, remote outcome unconfirmed, and recovery incomplete. A lost RPC response must not be reported as definite remote failure. Never retry a destructive action automatically. Node/Kubernetes display refreshes must not trigger recovery writes. Upgrade cleanup preserves preexisting operator cordons.

Upgrade streams gain `Wait(context.Context) (ports.UpgradeResult, error)`: the result is retained independently of progress consumption and published after adapter cleanup completes. The returned error describes cancellation/failure of the local wait; `result.Err` describes the operation. Wait is repeatable/concurrent. Cancel requests cancellation; only producer completion releases mutation exclusion. A full/unconsumed progress channel cannot block terminal publication. Update every implementation/fake of that port contract.

Bulk node execution retains a trusted optional per-target observer for live TUI outcomes. Observer callbacks are serialized and must return promptly; callers must enqueue into their own bounded capacity-sized buffer. This is a caller precondition, not a promise that the backend can interrupt an arbitrary blocking function. Nil observers still receive all outcomes in the aggregate return.

## TUI state and composition

One TUI owner per stored snapshot: shared cluster observations live in a TUI-owned cluster state; diagnostic results live with the relevant screen. Rendering/selection functions take current shared data explicitly. Do not mirror every snapshot into both root state and screen models, and do not keep `syncScreens` as a permanent bridge.

A small TUI request helper owns cancellation and request stamps. Feature result handlers apply only matching results. A small root router dispatches input to the active screen or overlay. Existing feature files own feature-specific commands, results, and rendering. Shared filter editing is allowed when it deletes actual repetition; a screen registry/plugin system is not.

Context switching clears session-specific state, marks and pending prompts; increments session identity; closes old work; opens the new session asynchronously. A late session-open result must be closed, not installed or leaked. Preserve the initial fast, network-independent frame, context picker, kube-context association, initial node focus, RO/RW badges, root/child navigation, palette command aliases, filtering, follow/pause, and typed confirmation flows.

During the branch only, implement a direct `directModel` in `tui/direct_model.go` behind a private test constructor while the CLI still runs the existing root. Feature renderers can temporarily use aliases to existing presentation structs, but no new operation delegates through the old reducer. Migrate shared feature call sites sequentially and keep intermediate builds working. Final cutover deletes the old root and aliases, promotes/renames the direct root, and switches CLI once. The executable never offers both mutation runtimes.

CLI constructs the application client and passes TUI-only launch options separately. Teardown closes the current session on normal quit, parent cancellation, and Bubble Tea runtime failure.

## Complete migration sequence

1. Establish the reviewed baseline and behavioral inventory; preserve concurrent local changes.
2. Add the direct client/session lifecycle with backend-only tests.
3. Move plain node diagnostics and resource reads to explicit methods.
4. Move shared cluster observations, health and Kubernetes enrichment.
5. Move stream operations and session-owned closure.
6. Extract explicit mutation policy inputs and establish the shared execution contract.
7. Move service/node actions, reset/wipe, snapshots/etcd, and upgrade completely.
8. Replace TUI async effect plumbing with direct commands and frontend request ownership.
9. Migrate cluster screens, diagnostics, resources, streams and mutation prompts to the new operations. Remove the corresponding old application paths as callers migrate.
10. Switch CLI composition; remove all reducer/effect/Runner compatibility paths and duplicate screen synchronization.
11. Verify a second plain Go caller exercises reads, assessments, actions and streams without any TUI imports; update architecture documentation.
12. Run complete verification and Sol whole-change review; resolve findings before declaring completion.

This is a complete migration delivered in reviewable slices, not an experiment that stops after one diagnostic. Temporary coexistence during the branch is permitted; permanent compatibility machinery is not.

## Model allocation and execution rules

The user selected Luna implementation and Sol review. Use `gpt-6-luna` for tightly scoped implementation tasks with explicit input/output contracts, owned files, and exact regressions. Use `gpt-6.1-sol` for architecture/spec review, completed-task reviews, and final integration review. The primary agent coordinates contracts and integration.

Parallel implementation is allowed only after the common session and policy contracts are accepted, and only with disjoint file ownership. Central TUI routing, CLI composition, shared types and final deletion are sequential tasks. No automatic fallback to a more expensive implementation model; report a task that Luna cannot complete.

## Verification and acceptance

- Backend external-package tests call the concrete API using existing port fakes, with no Bubble Tea or TUI dependency.
- Test session isolation, concurrent close/open, closed-session rejection, caller/session cancellation, canceled unstarted work, and stream close during a blocked receive.
- TUI tests cover out-of-order completions, same-node refresh, A-B-A resource selection, late session opens, context switching, prompt input precedence, view exit and teardown fallback.
- Backend action tests cover write gates, duplicate execution, cross-action exclusion, current inventory/risk revalidation, unknown health, per-target bulk outcomes, and complete membership snapshot sequencing.
- Retain outcome/timeout tests including response loss, cancellation after dispatch, upgrade cleanup-only failure and operator cordon preservation.
- Preserve visual goldens and navigation contracts except unavoidable fixture/type construction changes. Do not bless unexplained output changes.
- Final commands: `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build -o /tmp/t9s-shared-backend ./cmd/t9s`; `bun test` in `docs` when documentation scripts are affected.
- Search production code for remaining `application.Update`, `application.Model`, `application.Message`, `application.Effect`, `application.Runner`, pending application prompts, and old synchronization machinery. Completion requires their removal, not aliases around them.
- Every capability in both tables is covered by migrated tests and TUI call sites. Record deleted machinery and explain any material net code growth. Do not force a line-count reduction at the cost of correct behavior.
- No live cluster mutations, publishing, pushing, or merging are part of this task. Live compatibility remains a separately recorded smoke test.
