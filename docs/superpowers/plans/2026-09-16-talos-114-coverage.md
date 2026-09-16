# Talos 1.14 coverage plan for t9s

Status: Phase 0 delivered on branch `phase0-talos-114-coverage` (Phase 1 in progress)
Date: 2026-09-16
Scope: Talos v1.14.1 (t9s pinned to `pkg/machinery v1.14.1`)
Inputs: five parallel read-only gap analyses in `.superpowers/gap-analysis/`
(01 read-only, 02 writes/lifecycle, 03 etcd/cluster, 04 Talos 1.14, 05 architecture).

## Progress

- Phase 0 items 1-4 and 6 (hard gate) are implemented, tested, and committed:
  `ImageFactorySchematic`, hardened quorum warning, multipath routes, etcd alarms,
  and the quorum hard gate.
- An independent fresh-context review (`.superpowers/gap-analysis/reviews/phase0-review.md`)
  found the hard gate did not block the real TUI mutation path; fixed in `f101668`
  (confirm through the reducer first; defense-in-depth `nil` effects; certainty-aware
  gate; re-evaluate at confirm).
- Phase 0 item 5 (`ClusterHealthCheck`) remains. Phase 1 is starting from
  `.superpowers/gap-analysis/blueprints/etcd-safety.md` and
  `.superpowers/gap-analysis/blueprints/read-diagnostics.md`.
- Each blueprint is scoped per capability; member removal/leave (Capability B) is
  held for a separate careful pass.

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
| Read | nodes + detail, services, streaming service logs, machine events (one-shot), etcd membership/status, processes, physical disks, network links/addresses/routes, generic COSI browser, health overview/problems |
| Write (`--enable-writes`) | node reboot, shutdown, rollback, upgrade (streaming + lifecycle); service start/stop/restart; Kubernetes cordon/drain/uncordon during upgrade |
| Health rules | only `node-readiness`, `node-services-degraded`, `etcd-member-unhealthy` |

## 2. Gap inventory (consolidated)

### A. Talos 1.14 correctness and new surface (from report 04)

| Gap | Why it matters | Priority |
| --- | --- | --- |
| Stale comment claims `ImageFactorySchematic` unavailable; code still uses `ExtensionStatus` only | 1.14 stopped publishing `ghcr.io/siderolabs/installer`; factory schematic drives upgrade image suggestions | **P0** |
| BGP (`BGPPeerStatus`) not surfaced | Native BGP is a 1.14 headline; peer down/ASN/BFD/multipath state invisible | P1 |
| LVM / MD RAID (`LVM*Status`, `MDArrayStatus`) not surfaced | Major 1.14 storage feature; degraded arrays invisible | P1 |
| Filesystem trim / XFS scrub (`VolumeTrimSchedule`, `FSScrub*`) not surfaced | Silent data-loss risk on SSD/thin-provisioned clusters | P1 |
| Workload isolation (`sandboxd`, `SecurityProfileConfig`) not surfaced | New clusters default isolated; upgraded clusters differ | P1 |
| Multipath/ECMP routes render with blank gateway | `network.go` reads only `RouteSpec.Gateway`; 1.14 populates `NextHops` for multipath | **P0** (correctness) |
| No health rules for the above | `:overview`/`:problems` are blind to 1.14 failures | P1 |
| `taloscontainers` namespace not selectable in logs/services | New first-class container workloads | P2 |
| Dedicated system volumes / etcd port 2383 not documented | Operational surprises after upgrade | P2 |
| Lifecycle gate `<2.0.0` too loose | False-positive capability claim on future Talos | P2 |

### B. Node lifecycle / maintenance writes (from report 02)

| Gap | Why it matters | Priority |
| --- | --- | --- |
| **Reset / wipe node** (`ResetGeneric`): system/user disks, graceful etcd leave, reboot/halt | Completes the decommission lifecycle t9s already starts with shutdown/reboot | **P0** |
| ApplyConfiguration (with `DryRun`) | Day-2 config changes; dry-run maps to preview→confirm | P1 |
| Bootstrap / EtcdRecover | Disaster recovery | P1 |
| BlockDeviceWipe | Storage maintenance; `:disks` already lists targets | P1 |
| Etcd snapshot-before-destructive helper | Makes every membership change safe | P1 (safety) |
| ImagePull standalone, MetaWrite/Delete, LVM/MD teardown | Rare/niche | P2 / non-goal |

### C. etcd / cluster operations (from report 03)

| Gap | Why it matters | Priority |
| --- | --- | --- |
| **EtcdAlarmList** (`NOSPACE` visibility) + disarm | #1 production etcd failure mode; t9s currently blind | **P0** |
| **EtcdSnapshot** (backup to file) | Prerequisite for all destructive etcd actions and DR | **P0** |
| **EtcdRemoveMemberByID / EtcdLeaveCluster** with quorum hard-gate | Dead/scale-down control-plane node replacement | **P0** |
| **ClusterHealthCheck** (streaming) | Authoritative pre-flight before/after changes | **P0** |
| EtcdForfeitLeadership | Target/drain leader during upgrades | P1 |
| EtcdDefragment | Post-incident space reclaim | P1 |
| EtcdRecover + Bootstrap wizard | Disaster recovery runbook | P1 |
| Kubeconfig export | Removes `talosctl kubeconfig` friction | P1 |
| Curated etcd COSI detail (Spec/Config/PKI) | Deeper inspection without raw YAML | P1 |
| EtcdDowngrade* | Rare, high-risk | P2 / non-goal |
| Learner promotion | No upstream RPC | non-goal |

### D. Read-only diagnostics (from report 01)

| Gap | Why it matters | Priority |
| --- | --- | --- |
| **Dmesg** | Boot/kernel/driver debugging | **P0** (low effort) |
| **Netstat** | Connectivity / socket diagnosis | **P0** (low effort) |
| Mounts, Memory, DiskUsage | Capacity and filesystem truth | P1 |
| Time / TimeCheck | Clock sync verification | P1 |
| Containers, ImageList, Stats | Workload-level introspection | P1 |
| ServiceInfo detail, EventsWatch | Deeper service/event context | P1 |
| Read / LS file browser | Advanced debugging | P2 |
| COSI Hardware/Security/Perf, KubeSpan | Low TUI value; generic browser covers | P2 / non-goal |

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
- **Safety:** conservative is correct. Reset/wipe, etcd membership changes,
  bootstrap/recover, and block-device wipe require a typed target confirmation,
  not just `(y/n)`. Never proceed when the operation would drop etcd below
  quorum (existing `computeEtcdQuorumWarning` becomes a hard gate); learners do
  not count toward quorum. Snapshot before membership surgery.
- **Health:** missing data is `unknown`, never `healthy` (`evaluableStatus`
  guards rules). Add new `RuleID`s for every new resource class.
- **Tests/docs are part of done:** fake in `internal/testkit/fakes.go`, adapter
  test, application test, TUI test, `command.md`/`security.md`/guide updates,
  and `docs/tests/content.test.ts` if a required page is added.

## 4. Prioritized roadmap

### Phase 0 — correctness and safety net (small, do first)

1. **Migrate upgrade-image discovery to `ImageFactorySchematic`** with an
   `ExtensionStatus` fallback for pre-1.14 nodes; delete the stale comment
   (`internal/adapters/talos/node_controller.go:134`). *Correctness for 1.14.*
2. **Harden `computeEtcdQuorumWarning`** (`internal/application/actions.go:38`)
   before it becomes a hard gate: exclude learners (`IsLearner`) from the voter
   count, match targets by member ID/address as well as hostname, and keep
   unknown/unhealthy members counted as at-risk.
3. **Fix multipath/ECMP route rendering** — read `RouteSpec.NextHops` in
   `internal/adapters/talos/network.go` instead of relying on the unset
   top-level `Gateway`.
4. **Etcd alarm visibility** (`EtcdAlarmList`) in `:etcd`. *Low effort, high value.*
5. **ClusterHealthCheck** read path (`internal/ports/cluster.go` +
   `internal/adapters/talos/cluster.go`), surfaced in `:problems`/`:overview`.
6. **Quorum hard-gate + snapshot-before-destructive** helper in
   `internal/application` (built on the hardened warning from item 2).

### Phase 1 — node lifecycle and etcd runbooks (P0)

7. **Reset / wipe** from `:nodes` (typed node-name confirm, wipe-target preview
   from the disks model, quorum warning when graceful).
8. **EtcdSnapshot** to a local file, plus **EtcdRemoveMemberByID /
   EtcdLeaveCluster** with the Phase 0 hard gate and mandatory pre-snapshot.
9. **EtcdForfeitLeadership** and **EtcdDefragment** (single-node scope, `(y/n)` confirm).
10. **BlockDeviceWipe** from `:disks` (typed device-path confirm).

### Phase 2 — first-class diagnostics (read)

11. **Dmesg** (streaming), **EventsWatch** live toggle, and **Netstat** as
    node-scoped views (keys from `:nodes`/node detail).
12. **Mounts / filesystem usage**, **Memory** (CPU/memory headroom), then
    DiskUsage and Time/TimeCheck.
13. **Containers, ImageList, Stats** (namespace-aware), then **ServiceInfo** detail.
14. Bind the unused `RebootRequest_FORCE` / `ShutdownRequest.Force` options to
    the existing reboot/shutdown prompts.

### Phase 3 — Talos 1.14 surface and health

15. **BGP peer status** view/health rule.
16. **LVM / MD RAID status** in disk detail + health rules.
17. **Filesystem trim / XFS scrub status** + health rules.
18. **Workload isolation / `sandboxd`** visibility in node detail.
19. **Health rules** for BGP/LVM/MD/scrub and a curated `:overview` rollup.
20. **Dedicated system volumes** note in node detail.

### Phase 4 — config, recovery, convenience

21. **ApplyConfiguration** with dry-run preview → confirm
    (`--mode=reboot` was removed in 1.14; most config changes no longer reboot).
22. **Bootstrap / EtcdRecover** wizard (blocked when etcd members already exist).
23. **Kubeconfig export**; **EtcdSpec/Config/PKI** curated detail.
24. **`taloscontainers` namespace** support; docs for etcd port 2383.

## 5. Explicit non-goals

- Kubernetes control-plane version upgrades (already documented as out of scope).
- Kubernetes workload/object management (`kubectl` remains the tool).
- etcd downgrade operations and learner promotion.
- META partition writes, LVM/MD teardown fan-out, credential generation.
- Arbitrary shell/file write access; the generic `:resources` browser stays read-only.
- Automated/scheduled etcd backups (interactive on-demand only).

## 6. Suggested first PRs

1. `fix(upgrade): prefer ImageFactorySchematic with ExtensionStatus fallback`
2. `fix(etcd): harden quorum warning for learners and non-hostname targets`
3. `fix(network): render multipath/ECMP next-hops`
4. `feat(etcd): show and disarm etcd alarms`
5. `feat(cluster): stream ClusterHealthCheck into problems/overview`
6. `feat(nodes): reset/wipe action with typed confirmation`
7. `feat(nodes): dmesg, netstat, and live events node-scoped views`

## Appendix — source reports

- `.superpowers/gap-analysis/01-read-only.md`
- `.superpowers/gap-analysis/02-writes-lifecycle.md`
- `.superpowers/gap-analysis/03-etcd-cluster.md`
- `.superpowers/gap-analysis/04-talos-1.14.md`
- `.superpowers/gap-analysis/05-architecture-integration.md`
