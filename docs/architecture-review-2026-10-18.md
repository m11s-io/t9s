# Architecture review — 2026-10-18

Review goal: simplify maintenance, remove redundant machinery, and preserve Go operations that a future React interface can call through a backend API.

Sources inspected: main at `6d48901`; phase2-completion at `c9e21bc` plus its unfinished CPU changes; local Bubble Tea v2.0.6 source; cached Talos machinery v1.14.4 source; `.third_party/talos` at `d21101a99`. The Talos checkout is newer than the SDK pin, so its talosctl and server implementations are reference evidence, not a claim about every v1.14.4 server. The worktree was changing during review; CPU observations describe the files read, not a stable committed release.

This is a source review. No tests or live cluster operations were run. Findings below distinguish directly observable control flow from runtime behavior that still needs a regression test.

## Recommendation

Keep normalized domain values, the Talos SDK boundary, ordinary Go interfaces, and Bubble Tea. Concentrate changes on ownership of operations and requests. The present layers are individually understandable, but the same dependency and lifetime are represented in too many places.

Do not start by replacing all reducers, merging all packages, adding a feature registry, or defining a web API. First make one action and one diagnostic work through simpler boundaries; use those changes to establish the pattern.

## Correctness findings that should lead the work

### 1. Upgrade cleanup failure is lost in the terminal outcome

Location: `internal/adapters/talos/node_controller.go`, `runSafeUpgradeMaintenance`, lines 551–603; `UpgradeStream`, lines 370–451.

When cordoning was performed by t9s, deferred cleanup attempts uncordon. If uncordon fails, it emits a progress message. It joins the cleanup error only when `retErr != nil`. Therefore an otherwise successful upgrade returns nil despite failed cleanup, and `UpgradeStream` emits `UpgradeOutcomeApplied` with “upgrade complete”. The application warning/retry path is not enabled for this case.

This behavior is explicitly expected by `TestSafeUpgradeMaintenanceReportsCleanupWarningWithoutFailingCompletedUpgrade`; the issue is that the warning exists only as transient progress and is overwritten by completion.

Change the operation result to retain “OS applied, recovery incomplete” as structured data. Successful installation, successful reboot/readiness, and successful scheduling restoration are separate facts. A cleanup warning must survive the last progress event.

Upstream comparison: `.third_party/talos/cmd/talosctl/cmd/talos/upgrade.go` joins an uncordon error into the returned result even when the prior operation succeeded. Its install → drain → reboot sequence also confirms that t9s's install-before-drain order is intentional upstream behavior; moving drain earlier is not a justified simplification from this review.

### 2. Recovery loses cordon ownership and depends on optional display correlation

Locations: `runSafeUpgradeMaintenance`; `nodeController.Uncordon`; `internal/application/update.go:1128`, `recoveryEffectIfReady`; `internal/adapters/kubernetes/resolver.go`.

Normal upgrade cleanup knows `cordonedByT9S` and preserves an existing cordon. That ownership is absent from the recovery warning/result. A later Kubernetes refresh can call `controller.Uncordon` for any warned target once it appears Ready. This method resolves the node afresh and clears its cordon without knowing whether t9s created it. Thus a preexisting cordon can be removed on the recovery path after a readiness failure.

Recovery also requires `model.kubernetesReader`. Display correlation builds that reader only when local kubeconfig contains a context with the Talos context's name. Upgrade maintenance instead obtains kubeconfig from Talos. An upgrade can therefore run successfully through maintenance while recovery has no display reader to drive the promised retries. The heartbeat only starts refreshes while the nodes root is active and filtering is inactive.

Give the upgrade operation its own recovery record: target identity, whether t9s created the cordon, observed reboot identity, and remaining recovery steps. Run recovery using the same operation's cluster access. A display refresh should observe that operation, rather than schedule mutation as a side effect. Decide explicitly whether changing context cancels recovery; the current unconditional promise of automatic retries is broader than the implementation.

### 3. Resource results can overwrite a newer selection

Location: `internal/application/update.go:974–1014`, resource instance/detail handlers; message definitions in `model.go`.

Resource instance and detail results carry context generation but no request identity. Selecting kind A, then B, then receiving B followed by A accepts both results. SelectedKind remains B while rows become A. Detail requests have the same issue.

Node-scoped diagnostics commonly check generation plus node. That rejects a different node, but does not reject an older refresh for the same node, or an A → B → A sequence. Time/CPU already include request IDs and provide a better local example.

Use generation for cluster-session identity and a monotonically increasing request ID for each independently replaceable result. Capture the full request scope. Cancellation saves work; identity checks establish whether results are still valid. Keep both.

### 4. Logs assume transport chunks are lines

Location: `internal/adapters/talos/logs.go`, `readDataStreamBatch`.

The parser splits each RPC frame independently on newline and treats its suffix as a complete line. Frames `"hel"` and `"lo\n"` become two displayed lines. A UTF-8 character split between frames can also be damaged by conversion/truncation before reassembly. Frames with more than 200 lines silently lose the excess.

Upstream server reference: `internal/app/machined/internal/server/v1alpha1/v1alpha1_server.go`, `serviceLogChunker`, delegates to `pkg/chunker/stream/stream.go`. That chunker reads arbitrary byte buffers, default size 1024; it does not guarantee line boundaries.

Make log decoding stateful inside the adapter: retain an incomplete suffix, emit complete lines, and flush the final unterminated line at EOF. Bound retained partial-line bytes and output batches explicitly. Dmesg shares the existing helper, but its framing should be checked independently before sharing the new line decoder. This behavior needs a split-frame regression.

## Simplifications with clear value

### Independent reads should have independent outcomes

`NodesLoaded` schedules services; `ServicesLoaded` schedules events; event completion schedules etcd; etcd completion schedules Kubernetes. `ServicesFailed` and an ordinary `EventsFailed` stop the chain. Consequently an unrelated failure can leave etcd and Kubernetes unloaded. A successful event refresh also triggers unrelated etcd/Kubernetes reads.

Nodes provide control-plane targets for etcd. Services and events do not. Express that dependency directly. Start independent work together once its inputs exist. Bound concurrency in the operation doing cluster fan-out, and keep failures attached to their own resource state.

Node inspection already fetches services, then the detailed services reader discovers members and fetches services again. Share one uncapped service observation per refresh and derive summaries/details from it. Do not derive health summaries from the currently capped 1000-row display result: truncation must not change health calculations. The services reader currently scans nodes serially; a slow member delays all later members.

### Confirming an action should produce its execution

The TUI captures pending node/service actions, calls `ConfirmPendingAction`, inspects whether the prompt was cleared, and separately calls `BuildActionEffects`/`BuildServiceActionEffect`. Etcd confirmation returns its execution effect directly.

Unify the contract: confirmation validates current policy and produces effects for accepted work. Return multiple effects where necessary, with Bubble Tea adapting them to `tea.Batch`. Remove the public “build effects from a saved pending action” path after migration.

The future backend entry point must enforce writes enabled, current session identity, input validation, and policy itself. For example `ConfirmEtcdSnapshotPrompt` currently validates a path and schedules work without checking writes enabled or requiring a matching pending request; normal TUI usage supplies that discipline. Reusing messages as HTTP commands would expose that assumption.

Give each mutation an operation ID. Current ActionSucceeded/Failed identify only context and target, so a late result from an earlier action in the same context can be appended to a newer action's results after those results have been reset.

### Put lifetimes in one owner

Dependencies currently travel through adapter session → managed session → SessionOpened message → application model → effect closure. Cancellation is distributed across runner session replacement, bound readers, reducers, stream wrappers, and TUI view-exit logic.

Upstream facts matter here:

- Bubble Tea v2.0.6 runs commands in goroutines; Batch provides no result ordering. WithContext stops the program but cannot interrupt arbitrary command bodies.
- Talos machinery v1.14.4 `client.New(_ context.Context, ...)` ignores its context argument. Passing a session context to New does not attach all later RPCs to it. RPC contexts and Client.Close remain necessary.

Keep one explicit backend/session owner and one request-lifetime convention. Derive operation contexts from the session; cancel replaced requests and streams on close. Then remove bound-reader duplication as callers migrate. Do not simply delete wrappers on the assumption the SDK constructor handles cancellation.

Keep UI state as data. The reducer currently creates background contexts, invokes cancellation functions, mutates pointed-to pending actions, and reads time.Now for membership snapshot paths despite being documented as pure. Copying Model by value does not make its pointers/slices immutable. Prefer honest ownership over adding copy-on-write everywhere.

### Use smaller UI state and cheap list controls

The TUI copies application snapshots into individual screen models. Every application result then calls setState across all screens, reevaluates health, and normalizes lists, even for an unrelated log batch. This creates broad synchronization work and opportunities for stale copies.

Keep cursor, filter, viewport and prompt state in screen models; pass current application snapshots into rendering and selection functions. Normalize selection when that screen's rows change. Health evaluation should depend on relevant snapshots.

Many screens use Bubbles table.Model as a cursor: they allocate empty table.Row slices, call cursor movement methods, and render through custom tables. A small list-control helper with cursor/filter state can replace this machinery in those screens. Preserve stable selection by resource ID when rows refresh. Avoid building a general screen framework.

### Make resource lists cheap; load YAML on detail

`resourceInstanceReader.List` converts and marshals every resource to YAML, while `Get` marshals it again when detail opens. The list screen needs metadata. Return metadata rows from List and encode the selected object for detail. This removes unnecessary serialization and retention of full resource contents, particularly useful for larger or sensitive resource types.

### Narrow the Kubernetes client where the use is narrow

Display correlation needs only CoreV1 Nodes, but resolver.go constructs the complete generated Kubernetes clientset. A typed CoreV1 client and a consumer-owned node-list interface are sufficient there. Keep appropriate drain library support for upgrades; replacing kubectl drain with handwritten eviction logic would increase maintenance risk. Do not claim a binary-size reduction without measuring dependency impact.

## Deletions supported by reference searches

- Application Route/RouteNodes: production navigation is already owned by the TUI view stack.
- Runner.activeCtx: assigned and cleared, never read.
- newServiceReader: unused convenience constructor.
- supportsLifecycleUpgradeAPI: duplicate of the error-returning production range check, referenced only by tests.
- Production exports ending in ForTest: expose through test-only files or test in the owning package.
- layoutShell's unused promptActive argument and always-zero PromptHeight, after updating callers/tests.
- Terminal-specific ServiceSummary formatting from domain, moving needed formatting to tui.

Keep safety policy, partial/unknown distinctions, snapshot checksum verification, atomic no-overwrite snapshot writes, bounded retention, and SDK normalization. Their complexity protects real behavior.

## Intended long-term boundary

```text
TUI screen state                 Future React screen state
        |                                 |
        |                           HTTP/stream adapter
        +----------------+----------------+
                         |
             Go operations and policies
                         |
            context-scoped Talos session
                         |
                 Talos/Kubernetes SDKs
```

Reusable Go code should answer concrete requests: read diagnostics, assess action risk, execute a confirmed action, stream operation progress. Filters, keyboard navigation, prompt focus and terminal styling stay in the TUI. React owns its equivalent interaction state. Existing read interfaces are a useful starting point; there is no need to design HTTP, an event bus, a plugin system, or generated client contracts before a second interface exists.

## Suggested sequence

1. Correct upgrade cleanup outcomes and ownership; add focused regressions for cleanup-only failure, preexisting cordon plus recovery, and missing display correlation.
2. Correct resource request identity and log framing with focused regressions.
3. Make confirmation return execution effects; identify mutation results by operation.
4. Migrate one diagnostic, preferably CPU/time, to the single lifetime owner and keep its screen state local. Evaluate the resulting feature wiring before migrating others.
5. Separate initial reads, remove duplicate service fetching, and load resource YAML on demand.
6. Remove unused symbols and replace dummy table cursors incrementally.

Acceptance criteria: a late result cannot change the current request; a UI cannot bypass action policy by omitting a step; context/view exit releases work; recovery preserves operator scheduling choices; one new diagnostic does not require copying its capability through several transport structures. Smaller files alone do not establish these properties.
