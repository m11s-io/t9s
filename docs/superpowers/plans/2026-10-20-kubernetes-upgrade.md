# Kubernetes Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade Kubernetes using the standard Talos CLI, then safely roll kubelets through a serial drain/update/readiness/uncordon workflow.

**Architecture:** A session-owned upgrade backend composes a constrained `talosctl` subprocess with native Talos machinery and Kubernetes adapters. The application owns prepared-plan binding, backup, mutation admission, serial execution, and terminal outcomes; the TUI only handles safe summaries and structured progress.

**Tech Stack:** Go 1.27.1, pinned Talos machinery v1.14.4, talosctl v1.14.4, existing client-go and Bubble Tea v2.

**Spec:** `docs/superpowers/specs/2026-10-20-kubernetes-upgrade-design.md` (approved by the user).

## Global Constraints

- Use `.third_party/talos` at tag `v1.14.4`, commit `3fb46c378`, for upstream inspection. Keep the Talos root module out of `go.mod`.
- Execute the resolved standard CLI directly with explicit arguments, `--from`, `--to`, `--upgrade-kubelet=false`, and `--pre-pull-images=true`; no shell or arbitrary operator flags.
- Initial supported CLI release: v1.14.4. No automatic binary download. Preview must not execute `upgrade-k8s`, including its dry-run mode.
- Require an explicit stable patch/single-minor target; reject downgrades, skipped minors, unknown sources, unsupported image forms, incompatible Talos nodes, and invalid skew.
- Verify Kubernetes cluster identity using the `kube-system` UID and Talos node correspondence; context names and endpoint equality alone do not establish identity.
- Confirmation expires after five minutes and is single-use, session-bound, and bound to the complete assessed plan.
- Require writes enabled, a verified local etcd snapshot, fresh checks, and shared mutation ownership before mutations.
- Run CLI-controlled manifests before the separate kubelet stage. Reread configuration after CLI execution; pre-pull kubelet images separately.
- Drain serially using eviction and respecting PDBs. Do not force/delete local data; preserve existing cordons, and leave failed/unconfirmed nodes cordoned.
- Ten-minute drain deadline, five-minute kubelet convergence deadline, thirty-minute CLI deadline; respect shorter caller deadlines. Scale the total operation budget with node count.
- Child termination is followed by reaping before lease release. Neither exit zero nor reconnection establishes cluster convergence.
- Credentials, raw subprocess output, config bytes/diffs, and raw transport errors never reach TUI state or persistent operation records.
- No live/disposable cluster upgrade is authorized by implementation. Disposable release evidence remains pending. The user requested no tests, so implementation verification is build-only.

## Review Focus

1. A Kubernetes Node replaced under the same name must fail UID checks before eviction, patch, or uncordon (Tasks 1 and 4).
2. An alias context or second session must not permit a conflicting mutation; unknown identity must conservatively conflict rather than bypass ownership (Task 2).
3. A child that fills both pipes, prints an oversized line, or ignores cancellation must not hang the UI or release its lease early (Tasks 3 and 5).
4. A CLI-completed or partially completed cluster must be rediscovered without downgrading components or replaying ambiguous work as successful; verified manifest-stage evidence is retained only for the current application session (Tasks 1 and 5).
5. An externally cordoned node or a config edited between CLI and kubelet patch must preserve operator intent and stop on observed conflicting drift (Task 4).

## File and interface map

- `internal/domain/kubernetes_upgrade.go`: safe inventory, planned node/image changes, structured stages, terminal results, and pure version/path assessment.
- `internal/ports/kubernetes_upgrade.go`: backend and retained stream contracts; no SDK, Kubernetes API, or `os/exec` types.
- `internal/adapters/kubernetes/upgrade.go`: UID-bound inventory, authorization, eviction, readiness, and scheduling operations using a captured REST configuration.
- `internal/adapters/talos/kubernetes_upgrade_inventory.go`: captured Talos identity/credentials, per-node component evidence, image/manifests summary, and compatibility checks.
- `internal/adapters/talos/kubernetes_upgrade_process.go` plus `_unix.go`/`_windows.go`: executable validation, fixed arguments, private config file, concurrent output consumption, cancellation, and reaping.
- `internal/adapters/talos/kubernetes_upgrade_kubelet.go`: system image pre-pull and version-contract-aware kubelet-only configuration update; composes Kubernetes scheduling operations.
- `internal/application/mutation_registry.go`: process-owned conflict registry, including unknown-scope fallback.
- `internal/application/kubernetes_upgrade.go`: prepare, recheck, backup, pipeline, cancellation, and final verification.
- `internal/tui/kubernetes_upgrade_{model,commands,view}.go`: form, preview, confirmation, progress, terminal recovery details.
- Modify `internal/ports/context.go`, `internal/adapters/talos/session.go`, `internal/application/{session,mutation,action_plan}.go`, and `internal/testkit/fakes.go` for session wiring and shared ownership. Follow existing configuration/upgrade message generation patterns.
- Modify `internal/tui/{command,state,mutation_commands,cluster_commands}.go` for routing and lifecycle integration. Modify `README.md`, `docs/src/content/docs/reference/commands.md`, `docs/src/content/docs/security.md`, and the coverage roadmap when behavior ships.

### Task 1: Define the upgrade contract and trustworthy inventory

**Files:** Create the domain/ports files, the Talos inventory adapter, the Kubernetes upgrade adapter, and corresponding `_test.go` files. Modify `internal/ports/context.go`, `internal/adapters/talos/session.go`, and `internal/testkit/fakes.go`.

**Interfaces:**
- Add `ports.Session.KubernetesUpgrade() ports.KubernetesUpgradeBackend`.
- Define `KubernetesUpgradeBackend` with `Inspect(context.Context, string) (domain.KubernetesUpgradeInventory, error)`, `RunControlPlane(context.Context, domain.KubernetesUpgradePlan, func(domain.KubernetesUpgradeEvent)) error`, and `UpgradeKubelet(context.Context, domain.KubernetesUpgradeNodePlan, func(domain.KubernetesUpgradeEvent)) (domain.KubernetesUpgradeNodeResult, error)`. `Inspect`'s string is the explicit target, not a context name.
- Define `domain.AssessKubernetesUpgrade(KubernetesUpgradeInventory, string) (KubernetesUpgradePlan, error)`; target parsing, component skew, source/target mixtures, deterministic ordering, and skip decisions live here.
- Define `domain.TalosCLIIdentity` with `Path string`, `Version string`, and `SHA256 string`. Inventory uses an injected CLI-inspection function during Task 1; Task 3 supplies its production implementation. Until then, the real backend reports CLI unavailability rather than offering execution, and fake inventories exercise assessment.
- Inventory fields: `ClusterUID`, `TalosClusterID`, `Gateway`, `CLI` (absolute path, release, content digest), `Nodes`, `Manifests`, `Permissions`, `Healthy`, and `ObservedAt`. Each node includes Kubernetes UID/name, canonical Talos ID/address, role, Talos version, API/controller/scheduler/proxy versions and images where applicable, actual kubelet version/image, config fingerprint, readiness, and unschedulable state. Unknown observations remain explicit, never empty-string success.
- Plan fields: `ClusterUID`, `TalosClusterID`, `Gateway`, `CLI`, `SourceVersion`, `TargetVersion`, `Nodes`, `Manifests`, `SkipControlPlane`, and an assessment `Fingerprint`. Node plans include UID/name, Talos ID/address, role, source/target image/version, original scheduling state, and `Skip`. Include matching plan data in the fingerprint; exclude volatile timestamps.
- Events carry `Stage`, `NodeUID`, and safe normalized status only. Node results carry `NodeUID`, `Outcome`, and `LeftCordoned`. Overall results carry `Outcome`, `Nodes`, and snapshot receipt. Stages enumerate snapshot, CLI pre-pull/control-plane/proxy/manifests, kubelet pre-pull/drain/apply/readiness/uncordon, and final verification.

- [ ] **Step 1:** Add table-driven `TestAssessKubernetesUpgradePath` cases asserting rejection of downgrade, minor skips, prerelease, unknown/incompatible worker, unrelated mixed source versions, and duplicate/ambiguous node mapping; assert stable control-plane-before-worker order. Add `TestUpgradeInventoryRejectsWrongClusterAndReplacedNode`, requiring exact namespace UID equality and unique node UID/Talos mapping.
- [ ] **Step 2:** Run `go test ./internal/domain ./internal/adapters/kubernetes ./internal/adapters/talos -run 'Test(AssessKubernetesUpgrade|UpgradeInventory)'`; confirm failures expose the missing contract/behavior.
- [ ] **Step 3:** Implement the types and assessment. Retrieve a plain validated Kubernetes config through captured Talos credentials and use it for upgrade access; compare any independently resolved Kubernetes credentials before use. Discover each control-plane node independently; use machinery's compatibility API for every node. Summarize only manifest identities/scope, not secret-bearing contents. Parse supported image variants; reject unsupported custom/digest transitions. Check RBAC for node list/get/patch, pods list, eviction creation, and managed-object reconciliation, with normalized failures.
- [ ] **Step 4:** Add `TestAssessKubernetesUpgradeResumeRequiresConvergenceEvidence`: skip the CLI only if control-plane/proxy versions, configured images, and managed-manifest reconciliation are independently verified at target. If manifest convergence cannot be established, block automatic resume instead of inferring it from versions alone. Add `TestUpgradeInventoryDoesNotMutate` with spies asserting no config apply, image pull, eviction, object apply, or upgrade subprocess.
- [ ] **Step 5:** Run the focused tests again and `go test ./internal/ports ./internal/testkit`; require PASS. Commit as `feat(upgrade): discover and assess Kubernetes upgrade inventory`.

### Task 2: Extend mutation admission across sessions

**Files:** Create `internal/application/mutation_registry.go` and its tests; modify application session/admission wiring and fakes.

**Interfaces:**
- Define `mutationRegistry.acquire(scope string) (release func(), err error)` with idempotent release. Empty/unknown scope conflicts with every active write; known scopes conflict with the same key and with any unknown lease.
- Add cached verified `clusterUID` to application sessions. Upgrade preparation can bind it only after identity checks. Other mutations retain existing session checks and acquire the registry with the verified UID when available; otherwise they acquire the unknown scope. The registry is shared by all application clients in the process.
- Extend private `preparedState` with a Kubernetes upgrade request/facts slot so upgrade plans pass the same write, confirmation, consumed-plan, and session checks in `admit`.

- [ ] **Step 1:** Add `TestMutationRegistryScopes` asserting same-UID aliases conflict, different known UIDs can run, unknown scopes block all scopes in both acquisition orders, and repeated release is safe. Add `TestUpgradeAdmissionAcrossSessions` asserting an existing configuration/Talos/service/etcd mutation blocks upgrade and the reverse.
- [ ] **Step 2:** Run `go test ./internal/application -run 'Test(MutationRegistry|UpgradeAdmission)'`; require the expected missing-behavior failures.
- [ ] **Step 3:** Implement registry acquisition and release in common admission. Acquire without holding a session mutex across I/O. Release every failed-admission path; once an operation starts, retain both leases until terminal work actually ends. A session identity cannot change while its lease is active. Closed/canceled sessions request cancellation but do not release a still-running operation's lease.
- [ ] **Step 4:** Run `go test ./internal/application -run 'Test(MutationRegistry|UpgradeAdmission|Configuration|.*Upgrade.*)'` and `go test -race ./internal/application`; require PASS. Commit as `fix(operations): exclude conflicting cluster mutations across sessions`.

### Task 3: Run the standard CLI with bounded output and retained cancellation

**Files:** Create the process adapter and platform helpers plus their tests; connect `RunControlPlane` on the backend from Task 1.

**Interfaces:**
- Implement `RunControlPlane(context.Context, domain.KubernetesUpgradePlan, func(domain.KubernetesUpgradeEvent)) error` on the Talos backend. Its return means the child was reaped and all output readers stopped.
- Private `inspectTalosCLI(context.Context) (domain.TalosCLIIdentity, error)` resolves the executable and obtains only the client version (no server probe). Use the actual v1.14.4 `version` flags found in the local checkout; parse and reject other releases.
- Private command builder accepts the captured single-context Talos config and verified gateway; it cannot accept arbitrary operator arguments. Freeze CLI path/content digest and endpoint selection from preparation.

- [ ] **Step 1:** Add `TestKubernetesUpgradeCLIArguments`: assert explicit config/context/endpoints/single gateway, explicit source/target, skip-kubelet and pre-pull flags, no inherited node/context overrides, and no secret argument values. Add `TestKubernetesUpgradeCLIRejectsBinaryReplacement` and `TestKubernetesUpgradeCLIConfigPermissions`, asserting identity recheck and private temporary file cleanup on every exit.
- [ ] **Step 2:** Run `go test ./internal/adapters/talos -run 'TestKubernetesUpgradeCLI'`; confirm expected failures.
- [ ] **Step 3:** Implement direct `exec.Command` execution with a thirty-minute deadline. Capture credentials only in a private 0700 directory/0600 config file. Consume both pipes concurrently; cap individual parser records at 64 KiB and discard oversized records while continuing to drain. Whitelist stage patterns; unknown/raw content is discarded. Keep bounded progress delivery independent of terminal delivery. Implement graceful termination followed by a five-second grace period, then force termination and `Wait`; platform helpers must compile on the repository's supported OSes.
- [ ] **Step 4:** Add helper-process tests `TestKubernetesUpgradeCLIConcurrentOutputAndSecrets`, `TestKubernetesUpgradeCLIOversizedRecord`, and `TestKubernetesUpgradeCLICancelReapsChild`. Assert simultaneous pipe floods complete, secret markers/control sequences never appear in events/errors, oversized input does not stall, and cancellation returns only after child termination, pipe completion, and config cleanup. These tests run local helper binaries, never a Talos command against a cluster.
- [ ] **Step 5:** Run the focused CLI tests and `go test -race ./internal/adapters/talos -run 'TestKubernetesUpgradeCLI'`; require PASS. Commit as `feat(upgrade): run the standard Talos Kubernetes upgrade CLI`.

### Task 4: Drain, patch, and verify one kubelet at a time

**Files:** Create `internal/adapters/talos/kubernetes_upgrade_kubelet.go` and its tests; extend the Kubernetes upgrade adapter/tests from Task 1.

**Interfaces:**
- Implement `UpgradeKubelet(context.Context, domain.KubernetesUpgradeNodePlan, func(domain.KubernetesUpgradeEvent)) (domain.KubernetesUpgradeNodeResult, error)`.
- In `internal/adapters/kubernetes/upgrade.go`, define `UpgradeAccess` with `Cordon(context.Context, string, string) (bool, error)`, `Drain(context.Context, string, string, time.Duration) error`, `WaitKubelet(context.Context, string, string, string, time.Duration) error`, and `Uncordon(context.Context, string, string) error`. String arguments are node name, expected UID, then target version where present. `Cordon` returns whether this operation changed scheduling. Every write binds to expected UID/resourceVersion. The constructor consumes a validated captured `*rest.Config`; its API types never cross ports.

- [ ] **Step 1:** Add `TestUpgradeKubeletSequence` asserting pre-pull in the Talos system namespace before cordon; eviction before patch; Talos service plus exact Kubernetes version/UID/Ready before uncordon. Add `TestUpgradeKubeletPreservesExistingCordon`, asserting no uncordon on an initially unschedulable node or one already independently at target.
- [ ] **Step 2:** Run `go test ./internal/adapters/talos ./internal/adapters/kubernetes -run 'TestUpgradeKubelet'`; confirm expected failures.
- [ ] **Step 3:** Implement the sequence using pinned machinery `stdpatches.WithKubeletImage` and `GuessVersionContractKubelet`. Reread active config after drain; ensure the semantic diff changes only the approved image and preserves every document. Perform an immediate fingerprint recheck before `ApplyConfiguration` with `NO_REBOOT`; reject observed drift. Treat post-dispatch uncertainty as unconfirmed. Apply a ten-minute drain deadline and five-minute convergence deadline under the caller context. Use eviction with UID preconditions, no PDB bypass, no force deletion, and no `emptyDir` deletion. Respect supported DaemonSet/mirror-pod drain behavior; block unmanaged pods that require force.
- [ ] **Step 4:** Add `TestUpgradeKubeletStopsAtPDB`, `TestUpgradeKubeletRejectsReplacedNode`, `TestUpgradeKubeletRereadsConfigAfterCLI`, `TestUpgradeKubeletConfigDrift`, and `TestUpgradeKubeletFailureLeavesCordon`. Assert no patch after failed drain, no name-only node writes, preservation of CLI-produced changes, no apply after observed config drift, and no automatic uncordon after timeout/cancellation/unknown apply. Assert an uncordon failure remains a failed node even if the kubelet reached target.
- [ ] **Step 5:** Run the focused tests and `go test -race ./internal/adapters/talos ./internal/adapters/kubernetes -run 'TestUpgradeKubelet'`; require PASS. Commit as `feat(upgrade): roll kubelets with serial drain and readiness checks`.

### Task 5: Own preparation, backup, and the complete application operation

**Files:** Create `internal/application/kubernetes_upgrade.go` and its tests; extend application plan/admission support and testkit fakes.

**Interfaces:**
- `Session.PrepareKubernetesUpgrade(context.Context, string) (*PreparedKubernetesUpgrade, error)` prepares the explicit target. `PreparedKubernetesUpgrade.Summary() domain.KubernetesUpgradePlan` returns a defensive safe copy; prepared facts and timestamps remain private.
- `Session.ExecuteKubernetesUpgrade(context.Context, *PreparedKubernetesUpgrade, Confirmation, string) (ports.KubernetesUpgradeStream, error)` takes the local snapshot path as its final argument. Validate the path before admission; never overwrite a snapshot.
- `KubernetesUpgradeStream` exposes `Events() <-chan domain.KubernetesUpgradeEvent`, `Cancel()`, and `Wait(context.Context) (domain.KubernetesUpgradeResult, error)`. A caller stopping `Wait` does not abandon the retained operation or release leases. Reuse the retained-upgrade ownership pattern, not its OS-specific result type.

- [ ] **Step 1:** Add `TestPrepareKubernetesUpgradeBinding` asserting five-minute expiry, deterministic typed token `upgrade <clusterUID> to <targetVersion>`, defensive summaries, session binding, one-use consumption, and invalidation after identity/image/node/CLI drift. Read-only preparation may show a plan; execution must enforce writes enabled before snapshot or mutation.
- [ ] **Step 2:** Add `TestExecuteKubernetesUpgradeSnapshotGate` with call-order assertions: acquire -> fresh inspect -> verified snapshot -> fresh inspect -> CLI -> post-CLI verification -> each kubelet -> final verification. Snapshot failure, wrong receipt, or changed post-snapshot identity/version/quorum prevents CLI dispatch. Run `go test ./internal/application -run 'Test(Prepare|Execute)KubernetesUpgrade'`; require initial failures.
- [ ] **Step 3:** Implement preparation/execution using Tasks 1–4 and existing `executeEtcdSnapshot` inside the same admitted lease. Bind the snapshot destination at execution; show its receipt in results. Bound read/check RPCs by the existing read budget. CLI and node stages use their own deadlines, avoiding a fixed short whole-cluster cutoff. Stop on the first failure; mark subsequent nodes not attempted.
- [ ] **Step 4:** Add `TestExecuteKubernetesUpgradeRetainsLeaseUntilReaped`, including no event consumer; `TestExecuteKubernetesUpgradeExitZeroRequiresVerification`; `TestExecuteKubernetesUpgradePartialRetryRediscovery`; and `TestExecuteKubernetesUpgradeCancellation`. Assert no parallel kubelet tasks, no progress-based completion, preserved verified results on later failure, rejected stale retries, fresh assessment of target-completed nodes, and no automatic replay/rollback. Snapshot-only/pre-dispatch failure is distinct from unconfirmed remote mutation.
- [ ] **Step 5:** Run the focused tests, `go test ./internal/application`, and `go test -race ./internal/application`; require PASS. Commit as `feat(upgrade): coordinate backed-up Kubernetes cluster upgrades`.

### Task 6: Expose the workflow and document its operational limits

**Files:** Create the three TUI files and `internal/tui/kubernetes_upgrade_test.go`; modify the routing/lifecycle files and documentation listed in the file map.

**Interfaces:**
- Add `:upgrade-k8s` to the command parser, help, and command registry. It opens a cluster form with explicit target and local snapshot path.
- Preparation/stream messages carry the existing session generation and operation identity, plus safe plan/events/results; never client credentials or configuration bytes. Consume `ports.KubernetesUpgradeStream`; use existing command/message patterns, not a second application engine.

- [ ] **Step 1:** Add `TestKubernetesUpgradeCommandFlow`: assert separate cluster command, unchanged node `U` behavior, explicit target, disabled confirmation when reads/preflight fail or writes are off, exact typed token, displayed stage order, and per-node scheduling recovery details.
- [ ] **Step 2:** Run `go test ./internal/tui -run 'TestKubernetesUpgrade'`; confirm expected failures.
- [ ] **Step 3:** Implement the form, anticipated manifest scope copy, expiring preview, backup/confirmation, event consumption, cancellation, and terminal inventory/results. Context changes cancel and invalidate old work; late messages cannot update a new context. Preserve partial results and snapshot receipt for review. Unknown/sensitive backend failures map to fixed safe notices.
- [ ] **Step 4:** Add `TestKubernetesUpgradeLateMessagesAndSecrets` and `TestKubernetesUpgradeCancelShowsPartial`: assert stale generation/operation messages are ignored, a form edit discards preparation, no secret marker reaches rendered output, cancellation does not display success, and failed nodes needing manual scheduling recovery remain visible.
- [ ] **Step 5:** Update README, command/security guides, and roadmap with the CLI prerequisite, supported version, explicit target, snapshot and serial drain policy, manifests-before-kubelets order, cancellation/partial behavior, and local-only concurrency exclusion. Mark implementation and release validation separately; retain pending disposable validation without running a cluster operation.
- [ ] **Step 6:** Run `go test ./...`, `go vet ./...`, and `go build ./...`; require successful exits. Run `go test -race ./internal/application ./internal/adapters/talos ./internal/adapters/kubernetes ./internal/tui` once for the final integrated change. Use the existing docs package scripts from `docs/package.json` for docs tests/typecheck/build; require successful exits. Do not broaden/repeat validation without new changes or failures.
- [ ] **Step 7:** Review the whole diff against the spec and normalize any accidentally exposed raw output/config data. Confirm `go.mod` still lacks the Talos root module. Commit as `feat(tui): expose Kubernetes upgrade preview and progress`.

## Execution status

The user approved native implementation on the existing feature branch. Tasks 1–6 are implemented in the working tree. Tests and cluster operations were intentionally not run; the full Go build and diff review remain the verification steps. Disposable-cluster release validation remains pending.

## Plan self-review and handoff

- Inventory, identity, compatibility/skew, nonmutating preview: Task 1.
- Multi-session exclusion and safe unknown identity: Task 2.
- Standard CLI, immutable credentials/binary selection, bounded output, cancellation/reaping: Task 3.
- Separate kubelet stage, safe eviction, current configuration, convergence, scheduling intent: Task 4.
- Prepared confirmation, verified snapshot, serial orchestration, fresh verification, retained partial outcomes: Task 5.
- Cluster UX, context lifecycle, documentation and integrated verification: Task 6.
- All five Review Focus cases have explicit owning tests. Types and methods named in later tasks are defined in the earlier interface blocks; private raw data remains adapter-owned.
- Native execution was selected: the six tasks share tightly coupled contracts, so one implementer kept those interfaces consistent. The user approved implementation before product changes began.
