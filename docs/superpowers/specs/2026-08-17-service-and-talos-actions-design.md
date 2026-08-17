# Service and Talos Node Actions Design

## Goal

Extend t9s's write-action framework — established by the existing
Reboot/Shutdown flow on the Nodes screen — to cover two more Talos
capabilities exposed by the machinery client but currently unused: Talos
service lifecycle control (start/stop/restart) on the Services screen, and
Talos OS version control (upgrade/rollback) on the Nodes screen. Kubernetes
control-plane upgrade (`talosctl upgrade-k8s`) is explicitly out of scope —
it is a cluster-wide orchestration routine, not a per-node RPC, and does not
fit this framework; it will get its own design later.

## Scope

Production changes span `internal/ports`, `internal/application`,
`internal/adapters/talos`, and `internal/tui`. No changes to `internal/domain`
are anticipated — service and node identity are already represented there.

In scope:

1. Service start/stop/restart, single-target (cursor row) only, on the
   Services screen, keys `S`/`T`/`R`.
2. Talos Rollback, bulk-capable (marked set, like Reboot/Shutdown), on the
   Nodes screen, key `B`.
3. Talos Upgrade, single-target (cursor row) only, on the Nodes screen, key
   `U`, with a text-input prompt for the target install image prefilled from
   the node's current image.

Out of scope: Kubernetes control-plane upgrade; `stage`/`force`/`legacy`
upgrade flags (talosctl defaults — non-staged, non-forced — are used
unconditionally); bulk Upgrade; bulk service actions; drain-before-upgrade;
any change to how `internal/ports/kubernetes.go` or the Kubernetes node
reader work.

## Ports

`ports.NodeController` (`internal/ports/node.go`) gains three methods:

```go
Rollback(ctx context.Context, target string) error
Upgrade(ctx context.Context, target, image string) error
CurrentInstallImage(ctx context.Context, target string) (string, error)
```

`Rollback` and `Upgrade` follow the exact shape of the existing `Reboot`/
`Shutdown` methods. `CurrentInstallImage` is a read, not a write; it lives on
`NodeController` rather than a new reader port because it exists solely to
prefill the Upgrade prompt and has no other consumer — adding a fourth port
for one caller would be over-engineering.

A new port, `ports.ServiceController` (`internal/ports/service_controller.go`):

```go
type ServiceController interface {
	Start(ctx context.Context, node, service string) error
	Stop(ctx context.Context, node, service string) error
	Restart(ctx context.Context, node, service string) error
}
```

## Adapters (`internal/adapters/talos`)

`node_controller.go`: the `nodeControlClient` interface gains `Rollback(ctx)
error`, `Upgrade(ctx, image string, opts ...talosclient.UpgradeOption) error`
(via `client.UpgradeWithOptions`, passing `client.WithUpgradeImage(image)`),
and `CurrentInstallImage` is implemented directly against `*talosclient.Client`
(not proxied through the narrow test-seam interface, since it needs COSI
access, same as `machineryNetworkClient` in `network.go`) by fetching the
`config.MachineConfig` resource (`github.com/siderolabs/talos/pkg/machinery/resources/config`,
namespace `config.NamespaceName`, ID `config.ActiveID`) via
`safe.StateGet[*config.MachineConfig]` and returning
`.Provider().Machine().Install().Image()`. `nodeController.Rollback` and
`.Upgrade` wrap the client calls with the same `talosclient.WithNode` +
`fmt.Errorf("<verb> %s: %w", target, err)` pattern `Reboot`/`Shutdown` use.

New `service_controller.go`, mirroring `node_controller.go`'s shape: a
`serviceControlClient` interface wrapping `client.ServiceStart`/`ServiceStop`/
`ServiceRestart`, and a `serviceController` implementing `ports.ServiceController`
with `talosclient.WithNode(ctx, node)` + wrapped errors.

`session.go` constructs both controllers alongside the existing
`nodeController` and exposes the new one via a `session.ServiceActions()
ports.ServiceController` method, matching `NodeActions()`.

## Application layer (`internal/application`)

### Node actions (Rollback, Upgrade)

`ActionKind` gains `ActionRollback` and `ActionUpgrade`. `PendingAction` gains
an `Image string` field, populated only for `ActionUpgrade` and ignored
otherwise. `actionEffect` (`actions.go`) gains two cases:

```go
case ActionRollback:
	err = controller.Rollback(ctx, target)
case ActionUpgrade:
	err = controller.Upgrade(ctx, target, pending.Image)
```

`BuildActionEffects` needs `pending.Image` threaded into `actionEffect`'s
closure — its signature changes from `actionEffect(controller, kind, target,
generation)` to `actionEffect(controller, pending, target, generation)`,
reading `pending.Kind` and `pending.Image` internally. This is a pure internal
refactor; `BuildActionEffects`'s own signature and the `RequestAction`/
`ConfirmPendingAction`/`CancelPendingAction` message flow are unchanged, so
Reboot and Shutdown are unaffected.

`computeActionWarning` (currently in `actions.go`) is split: the
member-counting quorum math moves into a new unexported
`computeEtcdQuorumWarning(nodes []domain.NodeSnapshot, etcd EtcdState, targets
[]string) string`, and `computeActionWarning` becomes a thin wrapper that
resolves control-plane membership for `targets` and calls it — used by
Reboot, Shutdown, Rollback, and Upgrade identically (all four reboot the
node, so all four carry the same quorum risk). This same helper is reused by
the service-action warning below.

### Service actions (Start, Stop, Restart)

New types in `model.go`, parallel to but not merged with `PendingAction`:

```go
type ServiceActionKind string

const (
	ServiceActionStart   ServiceActionKind = "start"
	ServiceActionStop    ServiceActionKind = "stop"
	ServiceActionRestart ServiceActionKind = "restart"
)

type PendingServiceAction struct {
	Kind    ServiceActionKind
	Node    string
	Service string
	Warning string
}

type RequestServiceAction struct {
	Kind    ServiceActionKind
	Node    string
	Service string
}
func (RequestServiceAction) applicationMessage() {}
```

`Model` gains `PendingServiceAction *PendingServiceAction`. `RequestServiceAction`
computes `Warning` via `computeEtcdQuorumWarning(model.nodes, model.etcd,
[]string{node})` when `Service == "etcd"` and `Kind != ServiceActionStart`,
otherwise leaves it empty — mirroring how `RequestAction` computes
`PendingAction.Warning` today. `ConfirmPendingAction`/`CancelPendingAction`
are reused as-is for services too (both pending-action fields are cleared on
confirm/cancel; only one is ever non-nil at a time since the TUI only opens
one prompt at a time). A `serviceController ports.ServiceController` field is
added to `Model` and `SessionOpened`, populated the same way `nodeController`
is today. A `serviceActionEffect` + `BuildServiceActionEffect` pair (singular,
not plural — services are never bulk) in a new `service_actions.go` mirrors
`actionEffect`/`BuildActionEffects`.

## TUI layer (`internal/tui`)

### Services screen — `S`/`T`/`R`

`model.go`'s existing `if m.views.top().Kind == viewServices` block gains
three key checks (gated by `m.application.WritesEnabled && !m.services.filtering`),
each reading `m.services.selected()` for the cursor row and dispatching
`RequestServiceAction{Kind: ServiceAction<Start|Stop|Restart>, Node:
service.Node, Service: service.Name}`. The top-level `y`/other-key confirm
block (currently keyed on `m.application.PendingAction != nil`, lines ~146–168
of `model.go`) is extended with a parallel branch for
`m.application.PendingServiceAction != nil`, confirming via
`BuildServiceActionEffect` and clearing via `ConfirmPendingAction`/
`CancelPendingAction` exactly as the node-action branch does. `activePrompt()`
(used to render the confirm text) gains a case for the service pending
action, formatted as e.g. `restart etcd@cp-1? (y/n)`.

`actionHints` (backing `TestK9sCompatibilityActionMatrix`) gains `S`, `T`,
`R` entries for `viewServices` when `WritesEnabled`, matching how `R`/`X`
already only appear in Nodes' hints under that condition.

### Nodes screen — `B`, `U`

`B` mirrors `R`/`X` exactly: `if key == "B" && m.application.WritesEnabled &&
!m.nodes.filtering`, dispatching `RequestAction{Kind: ActionRollback, Targets:
m.nodes.actionTargets()}`.

`U` is a new two-step flow. `model` gains a field `upgradePrompt
*upgradePromptModel` where:

```go
type upgradePromptModel struct {
	target string
	input  textinput.Model
}
```

(new file `internal/tui/upgrade_prompt.go`, using `charm.land/bubbles/v2/textinput`,
the same component `commandModel` already uses). Pressing `U` (gated by
`WritesEnabled`, not filtering, cursor row selected, `m.upgradePrompt == nil`)
issues an effect calling the new `RequestUpgradePrompt{Target string}`
application message, which invokes `CurrentInstallImage` and returns
`UpgradePromptOpened{Target, Image string}`; `model.go` handles that message
by constructing `upgradePromptModel` with `input` prefilled to `Image` (empty
string on fetch error — the prompt still opens, just blank, never blocking
the user from typing a value manually). While `m.upgradePrompt != nil`, key
events route to it first (same precedence as `m.palette.active`): `Esc`
clears it with no side effects; `Enter` dispatches `RequestAction{Kind:
ActionUpgrade, Targets: []string{m.upgradePrompt.target}, Image:
m.upgradePrompt.input.Value()}` and clears `m.upgradePrompt`, handing off to
the existing `PendingAction` confirm flow. Any node marks present are
irrelevant to `U` — it always targets the cursor row, never
`nodes.actionTargets()`'s marked set.

`actionHints` for `viewNodes` gains `B` and `U` under `WritesEnabled`.

## Error handling

All new controller methods return wrapped errors (`fmt.Errorf("<verb> %s:
%w", target, err)`), surfaced through the existing `ActionFailed{Generation,
Target, Err}` message and rendered the same way Reboot/Shutdown failures are
today — no new error-display path. `CurrentInstallImage` failure does not
block the Upgrade prompt from opening (see above); it is not surfaced as an
`ActionFailed`, since fetching a prefill is not itself a user-requested
action.

## Testing

- `internal/application`: table tests for `computeEtcdQuorumWarning`
  extracted from the existing `computeActionWarning` tests (behavior
  unchanged, just relocated); new tests for `RequestServiceAction` producing
  the etcd-quorum warning only for `Service: "etcd"` + Stop/Restart; new
  tests for the Upgrade `PendingAction.Image` round-trip through
  `RequestAction` → `ConfirmPendingAction` → `BuildActionEffects`; a
  `TestRollbackAndUpgradeShareTheQuorumWarning`-style test proving the shared
  helper is actually shared (not duplicated ad hoc).
- `internal/adapters/talos`: new `node_controller_test.go` cases for
  `Rollback`/`Upgrade`/`CurrentInstallImage` against a fake
  `nodeControlClient`, mirroring the existing `TestReboot*`/`TestShutdown*`
  structure; new `service_controller_test.go` against a fake
  `serviceControlClient`.
- `internal/tui`: key-dispatch tests for `S`/`T`/`R` on Services (including
  a `WritesEnabled: false` inert case, mirroring
  `TestRebootKeyWithWritesDisabledIsInert`) and `B` on Nodes; a test for the
  `U` → prefilled-prompt → edit → `Enter` → `PendingAction` chain, and a
  cancel-via-`Esc` test proving no `RequestAction` is dispatched.
  `TestK9sCompatibilityDocumentsReadOnlyTalosDeviations` is updated: its
  comment and assertions are revised to state that Services now has
  Talos-level (not Kubernetes-pod-level) write actions, while continuing to
  assert Delete/Kill/Drain/Edit are absent.
