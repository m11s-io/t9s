# Kubernetes upgrades through Talos

**Status:** Approved design; implementation is in progress, with disposable-cluster validation pending

**Date:** 2026-10-20

**Scope:** Phase 5, cluster-wide Kubernetes upgrades

## Goal and agreed decisions

Give t9s operators a cluster-scoped Kubernetes upgrade workflow with an explicit target, a useful preview, a verified etcd backup, progress, and clear partial outcomes. The existing node `U` action remains a Talos OS upgrade.

The operator requested the standard `talosctl upgrade-k8s` subprocess instead of importing Talos's internal orchestration library, and chose draining around kubelet updates. t9s therefore runs the standard command with kubelet updates disabled, then handles kubelets sequentially with drain, update, health/version verification, and restoration of scheduling state.

This document specifies product behavior. It does not authorize a development upgrade of an operator's cluster.

## Upstream basis

Inspect the local `.third_party/talos` checkout at tag `v1.14.4`, commit `3fb46c378`. It was fetched and checked out explicitly for this work; earlier observations from its v1.15-alpha `main` checkout are not the release reference.

Relevant files, relative to that checkout:

- `cmd/talosctl/cmd/talos/upgrade-k8s.go`: supports explicit `--from`, `--to`, and `--upgrade-kubelet=false`.
- `pkg/cluster/kubernetes/talos_managed.go`: pre-pulls images, updates API servers, controller managers, schedulers, kube-proxy, optionally kubelets, then synchronizes managed manifests. Disabling kubelets also skips their image pre-pull.
- `pkg/cluster/kubernetes/kubelet.go`: patches configuration with the target kubelet image, preserves recognized `-fat`/`-slim` variants, and waits for service and Kubernetes node convergence. This path does not perform draining.
- `pkg/cluster/kubernetes/patch.go`: applies the patched configuration in `NO_REBOOT` mode.
- `pkg/machinery/config/generate/stdpatches`: supplies version-contract-aware kubelet image patches in the already-pinned machinery module.
- `pkg/machinery/compatibility/kubernetes_version.go`: exposes Kubernetes/Talos compatibility checks in the already-pinned module.

The CLI dry-run path is not the preview engine. In this release, image pre-pulling is not guarded by dry-run, and the static-pod/config patch path can still send `ApplyConfiguration` without its server-side dry-run field. Preview must not execute `upgrade-k8s`, even with `--dry-run`.

## Operator flow

1. Open `:upgrade-k8s`, a separate cluster command. Show current versions, cluster identity, and CLI availability. Enter an explicit stable target version; do not infer the latest release or use the CLI's default target.
2. Prepare a nonmutating plan. Show every control-plane and worker node, observed component versions, Talos compatibility, proposed images, managed-manifest reconciliation scope, drain policy, health/permission failures, and the selected CLI version.
3. Explain the actual order: CLI-managed control plane, kube-proxy, and manifests first; t9s-managed kubelets afterward, one node at a time. This is an intentional consequence of using the supported skip-kubelet flag; it differs from the all-in-one CLI's manifest-after-kubelet order.
4. Require write mode, a local snapshot destination, and typing the displayed cluster identity plus target version. Bind confirmation to the exact prepared plan, including node identities and target images.
5. Under the mutation lease, repeat discovery and safety checks, create and verify the etcd snapshot, then recheck cluster identity, versions, membership, and health before dispatching the CLI.
6. Run the CLI and show normalized progress. On successful exit, independently verify control-plane/proxy convergence before starting kubelets.
7. Complete the per-node kubelet sequence below. Finish with a fresh full inventory and health check; only verified convergence is success.

Preparation expires after five minutes. Form changes, context changes, session closure, changed identities or component versions, and incompatible drift invalidate it. A plan is single-use; retry always starts with rediscovery.

## CLI integration

Use a narrow cluster-upgrade port and a dedicated process adapter. Execute the resolved `talosctl` executable directly with an argument vector; never pass an operator-composed command string to a shell. Do not add the Talos root module dependency.

The initial supported executable release is `v1.14.4`, matching the inspected source and machinery dependency. Fail preparation if it is missing or has an unsupported version. Resolve its absolute path and bind its identity to the plan; recheck it before execution. Additional supported releases require source review and an explicit compatibility policy. Do not download or replace the executable automatically.

Build arguments explicitly: `upgrade-k8s`, `--from=<observed-source>`, `--to=<target>`, `--upgrade-kubelet=false`, and `--pre-pull-images=true`, with the captured Talos context, endpoints, and a single verified control-plane gateway. Do not inherit node selection or a different context from ambient environment variables. Keep upstream manifest reconciliation, waiting, and pruning defaults; do not offer force/adopt-all flags in the first release.

Capture the selected Talos client configuration for the operation. If the subprocess requires a file, serialize only the captured context into a private temporary directory (0700) and file (0600); delete it after the child is reaped. Credentials do not appear in command arguments, logs, TUI state, or persistent operation records. The SDK and child must use the same captured credentials and verified endpoint selection.

Consume stdout and stderr concurrently with bounded buffering. Map recognized stage messages to structured events; discard unmatched raw text and terminal control sequences. Raw manifest diffs, configuration text, and transport errors may contain secrets and must not enter the UI. Unknown output must not block pipe consumption or execution. Human-readable output is a progress hint, never evidence of completion; the process result and subsequent observations determine outcomes.

Cancellation requests child termination and waits for it to exit, with bounded escalation to killing the child/process group where supported. Keep the mutation lease until the child has been reaped and all local work has stopped. A killed process can leave server-side changes in progress; record a partial/unconfirmed result.

## Inventory, identity, and preflight

Inventory every API server and control-plane component from per-node evidence, every kubelet, kube-proxy, Talos node versions, Kubernetes node UIDs/roles, and the correspondence between Talos and Kubernetes nodes. A load-balanced `/version` response alone cannot establish every API server version. Unknown, missing, ambiguous, or unreachable participating nodes block execution.

Use the Kubernetes `kube-system` namespace UID as the stable cluster key, paired with the Talos cluster identity and canonical node mapping. Retrieve Kubernetes credentials through the selected Talos cluster, as the standard CLI does, and compare the namespace UID with the Kubernetes credentials used for drain/readiness operations. Matching names, context labels, or endpoint strings alone are insufficient. If a separately resolved Kubernetes context is used, it must pass the same UID comparison. Freeze credential selection for the operation.

Support stable patch upgrades and one minor step forward. Reject downgrades, minor skips, prereleases, unknown versions, and unsupported image/version forms. Apply machinery's pinned Kubernetes/Talos compatibility check to every participating node, including workers omitted from the CLI's skip-kubelet compatibility scope. Respect Kubernetes component skew throughout both stages.

Display mixed versions explicitly. The initial implementation permits only a coherent source version or a partially completed source-to-target transition in which each relevant component is at that source or target and all skew constraints hold. Derive the source from the complete inventory; do not silently choose a lowest version to hide unrelated drift. Rediscovery may identify an already completed CLI stage and plan only remaining kubelets. Other mixed states require operator repair outside this workflow.

Check etcd quorum and alarms, control-plane/service health, Node Ready conditions, absence of another local mutation, and permissions needed for inventory, CLI mutations, cordoning, pod listing/eviction, and uncordoning. Permission checks and healthy observations cannot guarantee later eviction or write success. Observe and report PDB constraints without promising that a drain will succeed.

Preview reads live configuration/resources to summarize affected component images and managed-manifest object identities. Label manifest reconciliation as an anticipated scope: exact server-side diffs and conflict resolution remain execution-time work. Do not claim to have run an upstream dry-run or validated the entire upstream upgrade by displaying this summary. Preview performs no image pulls, config apply, eviction, object apply, or child upgrade command.

## Kubelet stage owned by t9s

Process control-plane nodes, then workers, in a stable displayed order, with at most one node in progress. Before each node, refresh its UID, mapping, configuration, current kubelet image/version, scheduling state, and health. Stop on drift that changes the confirmed action. Nodes already verified at the target need no drain or patch.

For each node requiring an update:

1. Resolve the exact target kubelet image and pre-pull it into Talos's system containerd namespace before disrupting scheduling. Follow the release's supported image family and preserve recognized variants. Reject unrecognized custom/digest-only image transitions rather than guessing. Registry mirrors continue to be handled by Talos.
2. Record whether the node was already unschedulable. Cordon it if needed and drain through Kubernetes eviction, respecting PDBs. Do not force-delete pods, bypass eviction, delete `emptyDir` data, or override blocking unmanaged workloads. Report blockers and stop the workflow on failure.
3. Reread the active machine configuration after drain. Build only the kubelet image change using the pinned machinery `stdpatches` version-contract helpers; preserve every other configuration document and field. Do not reuse full configuration bytes captured before the CLI stage, because the CLI has just changed control-plane configuration.
4. Apply that configuration in `NO_REBOOT` mode, after verifying that the generated change is limited to the approved kubelet image. Track the config fingerprint across the immediate read/patch/apply window and fail on observed drift. This cannot eliminate races with external operators.
5. Wait for the Talos kubelet service to be running and healthy, and for the same Kubernetes Node UID to report the target kubelet version and Ready. Existing readiness alone is insufficient; observe convergence after dispatch.
6. Uncordon only if this operation cordoned the node. Preserve an operator's preexisting cordon. Verify the resulting scheduling state before continuing.

Any drain, image pull, patch, readiness, or uncordon failure stops dispatch to subsequent nodes. If a node was cordoned by t9s and its update fails or becomes unconfirmed, leave it cordoned and identify it explicitly for recovery. Cancellation follows the same rule; do not automatically uncordon a node whose health or update outcome is unknown.

Use a ten-minute drain deadline, a five-minute kubelet convergence deadline, and a bounded CLI deadline of thirty minutes, subject to a shorter caller deadline. Report timeout stages accurately. Node-level deadlines scale the whole operation with the planned node count; a fixed short overall timeout must not silently truncate a large cluster rollout.

## Application ownership and outcomes

The application layer owns the immutable prepared plan, write gate, confirmation, backup checks, operation lifecycle, and results. Keep the existing dependency direction: TUI -> application -> ports <- adapters. Machinery, client-go, and subprocess details stay in adapters.

Extend the session's shared mutation ownership to cover the entire backup/CLI/kubelet workflow and reject conflicting Talos upgrades, configuration applies, and maintenance. Coordinate cluster-keyed leases across sessions within the t9s process, including other mutations that identify that cluster. This does not exclude another t9s process or an external administrator; explain that limitation and recheck observed state before dispatch.

Reuse the verified local etcd snapshot implementation and its checksum receipt within the same lease. Failure to create, finalize, or verify the snapshot prevents the CLI from starting. Display the backup path and digest; keep backups after a failed or canceled upgrade. A snapshot is a recovery artifact, not an automatic rollback plan.

Track stages and per-node results as pending, running, verified, failed before dispatch, unconfirmed after dispatch, or not attempted. Retain successful observations when a later stage fails. CLI exit zero alone does not establish a completed cluster upgrade. Any cancellation, connection loss, unknown child outcome, or failed final verification produces a partial/unconfirmed result and requires a fresh plan before retry.

Do not automatically rerun the CLI, roll back component versions, uncordon an unhealthy node, or resume solely because connectivity returns. During the current application session, a successful CLI exit followed by verified target control-plane observations is retained as evidence that the upstream managed-manifest stage completed, allowing a fresh plan to continue kubelets. This evidence is memory-only. If the session ends before kubelets finish, and managed-manifest convergence cannot be independently rediscovered, block automatic resume and require Talos recovery procedures and fresh operator review. Never turn ambiguous progress into success.

## Delivery and validation scope

No live cluster operation is part of design or implementation authorization. The operator previously declined extra smoke testing; no cluster test is run by preparing this design. Before release, retain the roadmap's requirement for evidence on disposable supported upgrade paths.

Implementation verification should focus on actual boundaries: exact subprocess arguments and credentials, version rejection, nonmutating preview, wrong-cluster rejection, all-node compatibility, mixed/partial inventory, stale confirmation, backup failure, mutation ownership, secret-bearing child output, cancellation/reaping, sequential eviction and PDB failures, preservation of preexisting cordons, post-CLI configuration rereads, and partial results. Do not use a live cluster to exercise failures.

The command and security guides now describe the CLI prerequisite, stage order, drain policy, backup behavior, cancellation limits, and manual recovery guidance. Keep Phase 5 release validation pending until supported upgrade paths have evidence from disposable Talos clusters.

## Self-review

- The selected integration is the standard CLI subprocess; there is no Talos root-module dependency or duplicated control-plane upgrade engine.
- The separate kubelet phase deliberately runs after CLI manifest reconciliation, uses the configuration produced by the CLI, and performs its own image pre-pull.
- Draining is serial, PDB-aware, preserves prior scheduling intent, and never automatically restores an unverified failed node.
- Preview avoids the inspected CLI dry-run hazards and labels the limits of manifest prediction.
- Cluster matching, all-node compatibility, local concurrency limits, backup verification, and partial outcomes are explicit.
- Product code is implemented in the working tree; build-only validation and whole-diff review remain in progress. Manifest-stage resume evidence is retained only for the current application session. Disposable-cluster release validation remains outstanding.
