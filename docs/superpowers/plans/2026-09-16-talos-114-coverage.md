# Talos 1.14 coverage plan for t9s

Status: Phases 0 and 1 implemented; Phase 2 partially implemented; Phases 3 and 4 pending.
Created: 2026-09-16
Last reconciled with source: 2026-10-17
Scope: Talos 1.14 coverage; current working-tree SDK target is `pkg/machinery v1.14.4`.
Live v1.14.4 cluster smoke testing is pending; implemented below means present in source, not released or live-verified.
Inputs: five historical read-only gap analyses in `.superpowers/gap-analysis/`.
Their original v1.14.1 findings are background; this document records current status.

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
- **Phase 2 partially implemented:** streaming dmesg, netstat, mounts/filesystem
  usage, memory views, and live event following. CPU metrics,
  directory disk usage, time checks, container inspection, richer service details,
  and force reboot/shutdown controls remain.
- **Phases 3 and 4 pending:** curated Talos networking/storage/system views and
  health rules, configuration editing, disaster recovery, and convenience exports.
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
  effects, reducer (`update.go`), write gating and warning logic (`actions.go`).
- **tui** (`internal/tui/*.go`) — `viewKind` + per-view models + key routing +
  `actionHints`. Reads are resource-first; writes are gated by
  `WritesEnabled` and a confirm prompt.

Two escape hatches already exist: the generic `:resources` COSI browser (read-only,
any resource kind) and the streaming upgrade lifecycle.

### Current coverage

| Area | Exposed |
| --- | --- |
| Read | nodes + detail, services/detail + streaming logs, snapshot/live events, etcd membership/status/alarms, processes, disks, network links/addresses/multipath routes, streaming dmesg, netstat, mounts/filesystem usage, memory, generic COSI browser, health overview/problems, explicit streaming `:healthcheck` |
| Write (`--enable-writes`) | node reboot/shutdown/rollback/reset/upgrade; disk wipe; service start/stop/restart; etcd snapshot/remove/leave/defragment/disarm/forfeit leadership; Kubernetes cordon/drain/uncordon during upgrade |
| Health rules | `node-readiness`, `node-services-degraded`, `etcd-member-unhealthy`, `etcd-member-alarmed` |

## 2. Remaining gap inventory

These are curated UI/workflow gaps. Generic COSI resources may already be
inspectable through `:resources`; that does not imply a dedicated view or rule.
Priorities below preserve the original roadmap, rather than introduce new scope.

| Area | Remaining work | Priority |
| --- | --- | --- |
| Diagnostics | CPU metrics; directory `DiskUsage`; Time / TimeCheck; richer `ServiceInfo` | P1 |
| Containers | Namespace-aware containers, image list and stats; `taloscontainers` selection | P1 / P2 |
| Networking | BGP peer view and health rules | P1 |
| Storage | LVM / MD RAID status, volume lifecycle, trim / XFS scrub views and health rules | P1 |
| System | Workload isolation / `sandboxd` / security profile visibility | P1 |
| Health | Rules and overview rollup for the new network/storage/system evidence | P1 |
| Configuration | ApplyConfiguration with server-side dry-run preview and explicit confirmation | P1 |
| Recovery | Bootstrap / EtcdRecover workflow with mode-specific existing-cluster guards | P1 |
| Convenience | Kubeconfig export; curated etcd Spec/Config/PKI detail | P1 |
| Safety follow-ups | Typed member confirmation; expose force reboot/shutdown; revisit broad lifecycle version gate `<2.0.0` | Follow-up / P2 |
| Operational context | Dedicated system-volume note in node detail; document etcd port 2383 | P2 |
| Optional diagnostics | KubeSpan and curated hardware/security/performance views; file Read/LS | P2 / defer; generic resources remain available |

Time / TimeCheck must distinguish the configured-server query from arbitrary
server checks: Talos v1.14.3+ restricts TimeCheck for `os:reader`. A future UI must
handle permission denial without breaking the ordinary time-status view.

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

### Phase 2 — first-class diagnostics: partially implemented

- [x] **11a. Dmesg:** streaming node view with follow/reconnect controls.
- [x] **11b. Netstat:** node-scoped socket view.
- [x] **11c. Live events:** `s` follows new events across current members, with a
  2,000-event buffer, filterable node failures, and cancellation on exit/context
  switch. Snapshot remains the default; no automatic replay/reconnect.
- [x] **12a. Mounts/filesystem usage:** capacity and usage per mounted filesystem.
- [x] **12b. Memory:** memory and swap statistics; does not provide CPU metrics.
- [ ] **12c. CPU metrics, directory DiskUsage, Time / TimeCheck.**
- [ ] **13. Containers, ImageList, Stats:** namespace-aware inspection, followed by
  richer ServiceInfo; existing service detail is already available.
- [ ] **14. Force reboot/shutdown:** expose the port options in prompts; current
  actions use `RebootDefault` and `Shutdown(..., false)`.

### Phase 3 — Talos 1.14 surface and health: pending

- [ ] **15. BGP peer status** view and health rule.
- [ ] **16. LVM / MD RAID status** in disk detail and health rules.
- [ ] **17. Filesystem trim / XFS scrub status** and health rules.
- [ ] **18. Workload isolation / sandboxd** visibility in node detail.
- [ ] **19. Health rules and overview rollup** for the new resource classes.
- [ ] **20. Dedicated system volumes** note in node detail.

### Phase 4 — config, recovery, convenience: pending

- [ ] **21. ApplyConfiguration:** server-side dry-run preview then confirmation;
  validate modes against the pinned SDK before implementing.
- [ ] **22. Bootstrap / EtcdRecover:** recovery wizard with separate initialization
  and recovery preconditions; do not treat normal bootstrap and recovery as
  interchangeable operations.
- [ ] **23. Kubeconfig export; curated EtcdSpec/Config/PKI detail.**
- [ ] **24. taloscontainers namespace selection; etcd port 2383 documentation.**

## 5. Explicit non-goals

- Kubernetes control-plane version upgrades (already documented as out of scope).
- Kubernetes workload/object management (`kubectl` remains the tool).
- etcd downgrade operations and learner promotion.
- META partition writes, LVM/MD teardown fan-out, credential generation.
- Arbitrary shell/file write access; the generic `:resources` browser stays read-only.
- Automated/scheduled etcd backups (interactive on-demand only).

## 6. Remaining delivery and verification work

- [ ] Run and record a disposable Talos v1.14.4 live smoke test: nodes, services
  and logs, events, etcd, resource browsing, and the diagnostics views. Exercise
  destructive flows only on disposable infrastructure with explicit authorization.
- [ ] Confirm hosted CI for the v1.14.4 dependency, checksum, docs, docs-test,
  and live-event updates after pushing the reviewed changes. Local tests/race tests,
  vet, Go build, docs tests/typecheck/build, and module tidiness passed during
  this update; that does not establish hosted CI or live-cluster compatibility.
- [ ] Align the README's feature inventory with the implemented reset/wipe,
  membership operations, healthcheck, and diagnostic views.
- [ ] Release the verified changes; implement signed artifacts (checksums already
  exist). Neither publishing nor signing is implied by SDK integration passing.

## 7. Candidate next PRs

These are remaining candidates, not newly approved implementation work:

1. `feat(diagnostics): add time status and permission-aware time checks`
2. `feat(diagnostics): add CPU metrics and directory disk usage`
3. `feat(containers): add namespace-aware containers, images, and stats`
4. `feat(etcd): require typed member confirmation`

## Appendix — source reports

- `.superpowers/gap-analysis/01-read-only.md`
- `.superpowers/gap-analysis/02-writes-lifecycle.md`
- `.superpowers/gap-analysis/03-etcd-cluster.md`
- `.superpowers/gap-analysis/04-talos-1.14.md`
- `.superpowers/gap-analysis/05-architecture-integration.md`
