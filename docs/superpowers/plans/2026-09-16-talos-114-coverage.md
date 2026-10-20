# Talos 1.14 coverage plan for t9s

Status: Phases 0–3 implemented in the working tree; Phase 4 pending; Phase 5 Kubernetes upgrades newly planned.
Created: 2026-09-16
Last reconciled with source: 2026-10-20 (Phase 3 implementation, disposable smoke, and hosted CI/Docs validation recorded below)
Scope: Talos 1.14 coverage; current working-tree SDK target is `pkg/machinery v1.14.4`.
The Phase 3 Docker smoke used Talos v1.14.4 on two nodes. BGP, LVM/MD, trim/scrub, `sandboxd`, and SystemDisk resources were absent from that cluster; hardware-backed coverage remains pending. Implemented below means present in source, not released.
Inputs: five historical read-only gap analyses in `.superpowers/gap-analysis/`.
Their original v1.14.1 findings are background; this document records current status.
Kubernetes upgrades were added to planned scope at user request on 2026-10-17.

## Progress

- **Phase 0 implemented:** authoritative schematic discovery with legacy fallback,
  learner-aware/address-aware quorum assessment, confirm-time gating, multipath
  routes, etcd alarm visibility, and streaming cluster health checks.
- **Health-check scope:** `:healthcheck` / `:hc` is a separate explicit stream. It
  does not feed `:overview` / `:problems` or automatically gate writes. That differs
  from the original integration proposal and is the delivered behavior.
- **Phase 1 implemented:** typed node reset and device wipe, local etcd snapshots,
  member removal/leave with mandatory pre-snapshot and quorum checks, leadership
  transfer, defragmentation, and alarm disarm. Membership confirmation remains
  `(y/n)`; typed member confirmation is a follow-up.
- **Phase 2 implemented:** streaming dmesg, netstat, mounts/filesystem usage,
  memory views, live event following, sampled CPU utilization, permission-aware
  Time / TimeCheck, bounded directory disk usage, namespace-aware containers and
  images with resource stats, service event history, and explicit force
  reboot/shutdown controls. Disposable-cluster smoke checks remain pending.
- **Phase 3 implemented in the working tree:** BGP peer status, LVM/MD RAID,
  volume trim and XFS scrub status, workload isolation and `sandboxd` visibility,
  conservative health rules and overview rollups, and dedicated system-volume
  details. The disposable smoke verified node/service discovery, BGP and health
  views, system-volume/isolation details, and resource/disk views; configured
  BGP/storage maintenance evidence was not present. Hosted Go CI and Docs
  validation passed for the Phase 3 PR.
- **Phase 4 pending:** configuration editing, disaster recovery, kubeconfig export,
  and curated etcd resource details.
- **New planned scope:** cluster-wide Kubernetes upgrades through Talos, separate
  from node-level Talos OS upgrades. Requires its own design and implementation
  plan; no Kubernetes upgrade action exists in t9s yet.
- The historical Phase 0 review's TUI quorum bypass was fixed in `f101668`;
  the old review is not a current open-blocker list.

## 1. How t9s maps Talos today

Dependency direction is strictly inward:

```
cli -> tui -> application -> ports <- adapters/talos
                                     <- adapters/kubernetes
                             domain
```

A capability only reaches the operator by passing through all layers:

- **domain** (`internal/domain/*.go`) — view-model structs.
- **ports** (`internal/ports/*.go`) — reader/controller interfaces; `ports.Session`
  is the single accessor surface.
- **adapters/talos** (`internal/adapters/talos/*.go`) — converts RPC/COSI responses
  into domain structs; `session.go` wires each subsystem.
- **application** (`internal/application/*.go`) — generation-stamped messages,
  request identities, session-owned cancellation, effects, state transitions
  (`update.go`), dispatch (`dispatch.go`), write gating and warning logic (`actions.go`).
- **tui** (`internal/tui/*.go`) — `viewKind` + per-view models + key routing +
  `actionHints`. Reads are resource-first; writes are gated by
  `WritesEnabled` and a confirm prompt.

Two escape hatches already exist: the generic `:resources` COSI browser (read-only,
any resource kind) and the streaming upgrade lifecycle.

### Current coverage

| Area | Exposed |
| --- | --- |
| Read | nodes + detail, services/detail + streaming logs, snapshot/live events, etcd membership/status/alarms, processes, disks with LVM/MD and volume details, network links/addresses/multipath routes, BGP peers, streaming dmesg, netstat, mounts/filesystem usage, memory, sampled CPU, Time / TimeCheck, containers/images/stats, generic COSI browser, health overview/problems, explicit streaming `:healthcheck` |
| Write (`--enable-writes`) | node reboot/shutdown/rollback/reset/upgrade; disk wipe; service start/stop/restart; etcd snapshot/remove/leave/defragment/disarm/forfeit leadership; Kubernetes cordon/drain/uncordon during upgrade |
| Health rules | `node-readiness`, `node-services-degraded`, `etcd-member-unhealthy`, `etcd-member-alarmed`, `bgp-peer-not-established`, `lvm-volume-missing`, `lvm-volume-group-partial`, `md-array-unhealthy`, `volume-failed`, `volume-trim-overdue`, `filesystem-scrub-failed`, `sandboxd-service-unhealthy` |

## 2. Remaining gap inventory

These are curated UI/workflow gaps. Generic COSI resources may already be
inspectable through `:resources`; that does not imply a dedicated view or rule.
Priorities preserve the original roadmap except the explicitly requested
Kubernetes upgrade workflow added on 2026-10-17.

| Area | Remaining work | Priority |
| --- | --- | --- |
| Networking | BGP peer view and health rules | Implemented Phase 3 |
| Storage | LVM / MD RAID status, volume lifecycle, trim / XFS scrub views and health rules | Implemented Phase 3 |
| System | Workload isolation / `sandboxd` / security profile visibility | Implemented Phase 3 |
| Health | Rules and overview rollup for the new network/storage/system evidence | Implemented Phase 3 |
| Kubernetes upgrades | Cluster version inventory, compatibility checks, dry-run preview, backup, confirmed upgrade and progress | User-requested; Phase 5 |
| Configuration | ApplyConfiguration with server-side dry-run preview and explicit confirmation | P1 |
| Recovery | Bootstrap / EtcdRecover workflow with mode-specific existing-cluster guards | P1 |
| Convenience | Kubeconfig export; curated etcd Spec/Config/PKI detail | P1 |
| Safety follow-ups | Typed member confirmation; expose force reboot/shutdown; revisit broad lifecycle version gate `<2.0.0` | Follow-up / P2 |
| Operational context | Dedicated system-volume details in node detail; document etcd port 2383 | System volumes implemented Phase 3; etcd port docs pending |
| Optional diagnostics | KubeSpan and curated hardware/security/performance views; file Read/LS | P2 / defer; generic resources remain available |

Time / TimeCheck distinguishes ordinary status from explicit server checks.
Permission failures have independent result panels; live verification with
`os:reader` remains pending.

## 3. Cross-cutting design rules

These apply to every new capability (from report 05):

- **Read feature checklist:** domain type → port interface → `ports.Session`
  accessor → adapter file → session wiring → application state/messages/effect/
  reducer → TUI view model → `viewKind` → `actionHints` → root model field/key
  routing/render → tests (fake + adapter + TUI) → docs + `commands.md`.
- **Write feature checklist:** all of the above plus controller port method,
  adapter controller, `ActionKind`, gated reducer (`!WritesEnabled` or upgrade
  active), warning computation, effect builder, key bound only when
  `writeActionsEnabled()`, confirm prompt, and `security.md`.
- **Safety:** conservative is correct. Block an operation when the assessment
  establishes that it would drop etcd below quorum; learners
  do not count toward quorum. Membership changes and resets fail closed when
  their required quorum assessment is unavailable; ordinary node/service
  maintenance distinguishes uncertain status from confirmed quorum loss.
  Snapshot before membership surgery. Typed target confirmation is implemented
  for node reset and device wipe and remains required for future bootstrap/recover workflows. Etcd membership changes
  currently ship with an explicit `(y/n)` confirm plus the mandatory
  snapshot-first pipeline and quorum hard gate; upgrading them to a typed
  target confirmation is a tracked follow-up (blueprint open question #2).
- **Health:** missing data is `unknown`, never `healthy` (`evaluableStatus`
  guards rules). Add new `RuleID`s for every new resource class.
- **Tests/docs are part of done:** fake in `internal/testkit/fakes.go`, adapter
  test, application test, TUI test, `command.md`/`security.md`/guide updates,
  and `docs/tests/content.test.ts` if a required page is added.

## 4. Prioritized roadmap

### Delivery order after architecture simplification

The phase numbers below retain historical scope; they are not a requirement to
finish every feature in one phase before addressing another. Use this order:

1. Resolve the operation-lifecycle findings in
   [the current audit](../reviews/2026-10-18-operation-lifecycle.md), add typed
   member confirmation, and record disposable-cluster compatibility checks.
   Confirm hosted CI and reconcile the user-facing feature inventory before release.
2. Phase 2 diagnostics, containers/images/stats, service detail, and force
   reboot/shutdown are implemented; disposable smoke checks remain pending.
3. Phase 3 networking/storage/system health views are implemented in the current
   working tree. Keep occasional inspection in the generic resource browser.
4. Complete the Phase 5 upstream integration study before implementing cluster
   upgrades. Establish supported versions and operation ownership first.
5. Design configuration apply and disaster recovery separately. Bootstrap and
   etcd recovery need distinct preconditions and disposable-cluster validation;
   they must not be bundled with convenience exports.

Keep Go request/result values and policies reusable. Extract frontend-independent
operations when a second frontend needs them; avoid an HTTP contract for the
whole application model, a feature registry, or a general screen framework.

### Phase 0 — correctness and safety net: implemented

- [x] **1. Schematic discovery:** prefer `ImageFactorySchematic`, with legacy
  `ExtensionStatus` and declared-image fallbacks (`node_controller.go`).
- [x] **2. Quorum assessment:** exclude learners, match IDs/addresses/hostnames,
  distinguish uncertain status, and re-evaluate at confirmation (`actions.go`).
- [x] **3. Multipath/ECMP routes:** render next-hop gateways and links (`network.go`).
- [x] **4. Etcd alarm visibility:** list alarms and diagnose active alarms (`etcd.go`, `health.go`).
- [x] **5. ClusterHealthCheck:** `cluster_health.go` adapter/port and `healthcheck.go`
  TUI, exposed separately through `:healthcheck` / `:hc`.
- [x] **6. Mutation safety:** real TUI confirmation gates and snapshot-before-
  membership-change pipeline (`actions.go`, `update.go`, `tui/model.go`).

### Phase 1 — node lifecycle and etcd runbooks: implemented

- [x] **7. Node reset/wipe:** typed node confirmation, disk preview, reset options,
  and quorum checks (`reset_prompt.go`, `reset_actions_test.go`, `reset_test.go`).
- [x] **8. Etcd snapshot/remove/leave:** atomic checksum-verified local snapshots,
  mandatory pre-snapshot for membership changes, quorum gate, post-action refresh
  (`etcd_operations.go`, `etcd_membership_actions_test.go`).
- [x] **9. Leadership transfer and defragmentation:** single-member confirmed actions;
  alarm disarm also implemented (`etcd_forfeit_actions_test.go`, `etcd_actions_test.go`).
- [x] **10. Block-device wipe:** typed device confirmation and guarded target
  selection (`disk_wipe_prompt.go`, `disks_wipe_actions_test.go`, `wipe_test.go`).
- [ ] **Follow-up:** replace membership `(y/n)` with typed member confirmation.

### Phase 2 — first-class diagnostics: implemented; live smoke checks pending

- [x] **11a. Dmesg:** streaming node view with follow/reconnect controls.
- [x] **11b. Netstat:** node-scoped socket view.
- [x] **11c. Live events:** `s` follows new events across current members, with a
  2,000-event buffer, filterable node failures, and cancellation on exit/context
  switch. Snapshot remains the default; no automatic replay/reconnect.
- [x] **12a. Mounts/filesystem usage:** capacity and usage per mounted filesystem.
- [x] **12b. Memory:** memory and swap statistics; does not provide CPU metrics.
- [x] **12c. CPU metrics, directory DiskUsage, Time / TimeCheck.**
  - [x] Time / TimeCheck: node-scoped status and UTC samples, explicit checks,
    independent result panels and request cancellation. Fake-driven tests pass;
    disposable-cluster smoke checks remain pending.
  - [x] CPU utilization and process counts: two cancellable node-scoped samples,
    aggregate/per-core busy and I/O wait, explicit refresh without polling.
    Fake-driven tests pass; disposable-cluster smoke checks remain pending.
  - [x] Directory DiskUsage: selected-mount depth-one scan with row/time bounds,
    partial error rows, and cancellation. Fake-driven tests pass; live scan remains pending.
- [x] **13. Containers, ImageList, Stats, and service detail:** namespace-aware
  container/image inspection with resource stats and bounded event history in
  service detail. Fake-driven checks pass; disposable-cluster checks remain pending.
- [x] **14. Force reboot/shutdown:** typed exact-target confirmation, writes gate,
  confirm-time quorum checks, and Talos `FORCE` mapping. No force action bypasses
  a blocker. Disposable-cluster prompt smoke checks remain pending.

### Phase 3 — Talos 1.14 surface and health: implemented in working tree; Docker smoke run

- [x] **15. BGP peer status** cluster view and non-established-session health rule.
- [x] **16. LVM / MD RAID status** in disk detail, with missing/partial/array health rules.
- [x] **17. Filesystem trim / XFS scrub status** in volume details and health rules.
- [x] **18. Workload isolation / sandboxd** visibility in node detail, with explicit unhealthy `sandboxd` diagnosis.
- [x] **19. Health rules and overview rollup** for BGP, storage, volumes, and system status. Unsupported groups remain unknown.
- [x] **20. Dedicated system volumes** from Talos system-volume labels in node detail.

### Phase 4 — config, recovery, convenience: pending

- [ ] **21. ApplyConfiguration:** server-side dry-run preview then confirmation;
  validate modes against the pinned SDK before implementing.
- [ ] **22. Bootstrap / EtcdRecover:** recovery wizard with separate initialization
  and recovery preconditions; do not treat normal bootstrap and recovery as
  interchangeable operations.
- [ ] **23. Kubeconfig export; curated EtcdSpec/Config/PKI detail.**
- [ ] **24. taloscontainers namespace selection; etcd port 2383 documentation.**

### Phase 5 — Kubernetes upgrades through Talos: newly planned

This changes the original product scope: Kubernetes upgrades are now planned,
while the existing node `U` action remains a Talos OS upgrade. This is an
architectural workflow requiring a separate reviewed spec and implementation
plan before product code changes. Adding it here authorizes roadmap planning,
not execution against a cluster.

Version reference checked on 2026-10-17: the [Kubernetes release page](https://kubernetes.io/releases/)
lists v1.37.1, and the [Talos v1.14.4 release images](https://github.com/siderolabs/talos/releases/tag/v1.14.4)
include v1.37.1 control-plane components, kube-proxy, and kubelet. These are
reference targets, not evidence of any user's running Kubernetes version.
Discover installed versions and validate compatibility again at execution time;
do not hard-code v1.37.1 as the newest available or universally valid target.

- [ ] **25. Cluster version inventory:** discover every API server and kubelet
  version, cluster identity, node roles, and Talos versions. Surface mixed or
  unavailable versions explicitly. Verify that Talos and Kubernetes credentials
  identify the same cluster before offering a mutation.
- [ ] **26. Upstream orchestration integration:** evaluate reuse of the pinned
  Talos implementation behind `talosctl upgrade-k8s`. The inspected upstream
  orchestration lives in `pkg/cluster/kubernetes`, outside the currently pinned
  `pkg/machinery` module; establish availability, API shape, provider interfaces,
  and dependency cost at the chosen release before selecting an integration.
  Keep it behind a narrow cluster-upgrade port and normalize progress/errors.
  The design must choose a Go-library integration or a constrained, version-
  checked talosctl adapter; it must not add arbitrary shell execution.
- [ ] **27. Upgrade preview and preflight:** require explicit target selection;
  check Talos compatibility on all participating nodes and Kubernetes version
  skew. Support patch upgrades and one minor step at a time; reject downgrades,
  minor-version skips, and unknown/incompatible source versions. Show a dry-run
  plan, affected control-plane/worker nodes, image references, manifest changes,
  and permission/health failures before enabling confirmation. Verify that the
  preview path does not mutate the cluster.
- [ ] **28. Confirmed cluster operation:** require `--enable-writes`, a verified
  etcd snapshot before mutation, fresh health/version checks, and typed cluster
  identity plus target-version confirmation. Prevent concurrent Kubernetes/Talos
  upgrades and conflicting maintenance operations against the same cluster.
  Give this a separate cluster-scoped entry point; do not reuse per-node `U`.
- [ ] **29. Progress and partial failure:** preserve upstream component sequencing
  and readiness checks for API servers, controller managers, schedulers,
  kube-proxy, kubelets, and managed manifests. Show image pre-pull, drain/eviction
  where applicable, per-component progress, and final version/health verification.
  Cancellation or disconnection must report a potentially partial upgrade and
  require rediscovery before retry; do not promise automatic rollback or treat
  reconnecting as evidence of completion.
- [ ] **30. Validation and documentation:** cover read-only gating, wrong-cluster
  credentials, unsupported/skipped versions, mixed versions, snapshot failure,
  concurrent operations, stale confirmation, cancellation, and partial completion.
  Validate supported upgrade paths on disposable Talos clusters before release.
  Update the README/security/commands guides when the feature actually ships;
  current documentation must continue to describe the existing OS-only action.

References for the design: [upstream upgrade-k8s command](https://github.com/siderolabs/talos/blob/v1.14.4/cmd/talosctl/cmd/talos/upgrade-k8s.go),
[upstream Kubernetes orchestration](https://github.com/siderolabs/talos/tree/v1.14.4/pkg/cluster/kubernetes),
and [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/).

## 5. Explicit non-goals

- Kubernetes upgrade implementation remains pending Phase 5; the existing Talos
  OS upgrade action does not upgrade Kubernetes. Kubernetes upgrades are no longer
  a permanent non-goal.
- Kubernetes workload/object management (`kubectl` remains the tool).
- etcd downgrade operations and learner promotion.
- META partition writes, LVM/MD teardown fan-out, credential generation.
- Arbitrary shell/file write access; the generic `:resources` browser stays read-only.
- Automated/scheduled etcd backups (interactive on-demand only).

## 6. Remaining delivery and verification work

- [x] Run and record a disposable Talos v1.14.4 Docker smoke: two nodes reported
  healthy at v1.14.4; t9s loaded services and etcd overview data; `:bgp`,
  `:overview`, `:problems`, node detail/system volumes/isolation, `:resources`,
  and `:disks` rendered. The overview showed the two `META` volumes waiting.
  No BGP peers, LVM/MD arrays, trim/scrub schedules, `sandboxd` service, or
  `SystemDisk` resource existed in this Docker cluster; hardware-backed cases
  still need a suitable Talos setup.
- [ ] Complete the broader read-only smoke matrix for service logs, events,
  etcd detail, and diagnostic subviews. Exercise destructive flows only on
  disposable infrastructure.
- [x] Confirm hosted CI for the Phase 3 PR: [branch Go CI](https://github.com/m11s-io/t9s/actions/runs/39442950193), [PR Go CI](https://github.com/m11s-io/t9s/actions/runs/39443001625), and [Docs validation](https://github.com/m11s-io/t9s/actions/runs/39443001644) passed. The draft PR Docs deploy job was skipped.
- [x] Local tests/race tests, vet, Go build, docs tests/typecheck/build, and
  module tidiness passed. Hardware-backed resource coverage remains pending.
- [x] Align the README's feature inventory with implemented Phase 0–3 read and
  write coverage.
- [ ] Release the verified changes; implement signed artifacts (checksums already
  exist). Neither publishing nor signing is implied by SDK integration passing.

## 7. Candidate next PRs

These are remaining candidates, not newly approved implementation work:

1. Operation ownership/deadlines/outcomes follow-ups from the lifecycle audit,
   followed by disposable-cluster validation. CPU and time are already implemented.
2. `feat(etcd): require typed member confirmation`
3. Document etcd port 2383 and close remaining Phase 4 convenience gaps.
4. Kubernetes upgrade design/integration study (Phase 5 items 25–30; separate
   reviewed spec and implementation plan before feature development).

## Appendix — source reports

- `.superpowers/gap-analysis/01-read-only.md`
- `.superpowers/gap-analysis/02-writes-lifecycle.md`
- `.superpowers/gap-analysis/03-etcd-cluster.md`
- `.superpowers/gap-analysis/04-talos-1.14.md`
- `.superpowers/gap-analysis/05-architecture-integration.md`
