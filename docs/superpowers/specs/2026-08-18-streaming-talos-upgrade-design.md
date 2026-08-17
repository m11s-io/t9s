# Streaming Talos Upgrade Design

## Goal

Make t9s upgrades preserve the running node's Image Factory schematic, use
Talos's streaming lifecycle API when the target supports it, expose meaningful
progress and failures in the TUI, and warn before an operator skips a Talos
minor release.

The design follows talosctl v1.13.3's upgrade boundary: nodes in the version
range `>1.13.0-alpha.2 <2.0.0` use `LifecycleService`; older nodes use the
deprecated unary `MachineService.Upgrade` path. A lifecycle stream is only
successful after it emits exit code zero.

## Scope

This increment includes:

- schematic-aware upgrade-image suggestions derived from the running node;
- target-version parsing and skipped-minor warnings;
- image pull, lifecycle install, Kubernetes drain, reboot, readiness
  wait, and uncordon progress;
- legacy fallback for Talos versions outside the lifecycle API range;
- application messages and TUI state for an in-flight upgrade;
- cancellation and error behavior that never reports a failed stream as a
  successful upgrade.

It does not add Kubernetes control-plane upgrades, bulk Talos upgrades,
staged/forced/insecure lifecycle modes, arbitrary registry inspection, or a
general-purpose task framework. Durable Terraform hostname and installer-image
configuration is a separate infrastructure deliverable because it belongs to a
different repository and has an independent validation/apply lifecycle.

## Chosen Approach

t9s will own a small upgrade orchestrator behind the existing Talos adapter.
The orchestrator will use narrow client interfaces for version discovery,
ImageService pull, LifecycleService upgrade, reboot/readiness, and the legacy
upgrade RPC. It will emit normalized `ports.UpgradeEvent` values through a
callback rather than exposing Talos protobuf or gRPC stream types outside the
adapter.

This is preferred over calling talosctl as a subprocess because t9s already
owns authenticated Talos and Kubernetes clients, subprocess output is not a
stable API, and cancellation/errors would be harder to model. It is also
preferred over merely swapping the unary RPC for `LifecycleClient.Upgrade`:
the lifecycle RPC performs the install stage only, so doing that would omit
image-pull, drain, reboot, readiness, and uncordon behavior.

## Upgrade Image Suggestion

`CurrentInstallImage` becomes an upgrade-suggestion operation while preserving
its existing port shape. For the selected node it reads:

1. the live Talos version tag;
2. the installed `ExtensionStatus` resource named `schematic`: its version is
   the schematic ID and its author encodes the installer flavor and Image
   Factory URL;
3. the declared machine-config install image as a final fallback.

The pinned Talos machinery v1.13.3 SDK does not expose the newer first-class
`ImageFactorySchematic` runtime resource, so t9s must not claim or attempt to
read it.

When a non-empty schematic ID and flavor are available, the suggestion follows
Talos's own `images.NewInstallerImage` shape:

`<factory>/<flavor>-installer/<schematic-id>:<running-version>`

For the recovered Proxmox nodes this resolves to
`factory.talos.dev/metal-installer/<schematic-id>:<running-version>`. The
flavor must not be hard-coded: Talos also supports paths such as
`aws-installer`, while some board-specific images use the unprefixed
`installer` flavor.

The UI prefill initially uses the running version so accepting the dialog
cannot accidentally downgrade the node. The operator edits only the tag (or
the full image) to select the target. If schematic discovery is unavailable,
t9s retains the current repository-preserving behavior: replace the declared
image tag with the running version. Digest references remain unchanged because
there is no safe tag substitution.

On v1.13.3, the installed `schematic` extension metadata is the live
schematic-discovery source. If it is unavailable or cannot be decoded, t9s
uses the declared-image fallback instead of inventing a repository.

No Crane, Skopeo, registry HTTP request, or Image Factory mutation is required.

## Version Validation and Warning

Before opening the final confirmation, t9s parses the running version and the
tag in the entered image as semantic versions. A missing or non-semantic target
tag does not block the existing explicit-image workflow, but t9s cannot offer a
minor-version warning for it.

When both versions parse and the target minor is more than one greater than the
running minor, the confirmation includes a warning that intermediate minor
releases are being skipped. Downgrades continue to require the normal explicit
confirmation and are not silently rewritten. Patch upgrades and one-minor
upgrades show no extra warning.

The warning is advisory because Talos validates the actual upgrade path and
image compatibility. t9s does not claim to replace server-side checks.

## Adapter Contract

The node controller upgrade method changes from a terminal-only error return
to a progress callback:

```go
type UpgradePhase string

const (
	UpgradeChecking   UpgradePhase = "checking"
	UpgradePulling    UpgradePhase = "pulling"
	UpgradeInstalling UpgradePhase = "installing"
	UpgradeDraining   UpgradePhase = "draining"
	UpgradeRebooting  UpgradePhase = "rebooting"
	UpgradeWaiting    UpgradePhase = "waiting"
	UpgradeUncordon   UpgradePhase = "uncordoning"
	UpgradeComplete   UpgradePhase = "complete"
)

type UpgradeEvent struct {
	Phase   UpgradePhase
	Message string
	Current int64
	Total   int64
}

type UpgradeProgress func(UpgradeEvent)

Upgrade(ctx context.Context, target, image string, progress UpgradeProgress) error
```

`Current` and `Total` are populated only when Talos supplies byte progress.
Messages are normalized, single-line operator text and must not include
credentials or raw configuration.

The controller checks the selected node's Talos version against talosctl's
range `>1.13.0-alpha.2 <2.0.0`. A version outside that range emits a checking
event explaining the fallback, calls the existing legacy upgrade method, and
returns its result. An error obtaining/parsing the node version is a real error,
not permission to guess which API is supported.

For a lifecycle-capable target, the controller performs these stages in order:

1. pull the selected installer image through ImageService and emit layer/byte
   progress;
2. call `LifecycleService.Upgrade` with the pulled image as its artifact source;
3. forward install messages and require a terminal exit-code event;
4. resolve and drain the Kubernetes node through Talos-provided cluster
   credentials;
5. reboot the Talos node;
6. wait for Talos readiness within the existing action timeout;
7. uncordon a node that t9s cordoned, including after later-stage failure;
8. emit complete only after all required stages succeed.

An EOF before any exit code is an error. A nonzero exit code is an error even
when the stream itself closes cleanly. Multiple errors (for example reboot
failure followed by uncordon failure) are joined so cleanup failure is not
lost.

## Kubernetes Drain Boundary

The drain path mirrors talosctl rather than relying on t9s's optional local
kubeconfig correlation. The Talos client fetches the cluster kubeconfig from a
control-plane endpoint, and the selected node's `Nodename` COSI resource
provides the exact Kubernetes node name. This avoids context-name assumptions
and continues to work when t9s was launched without a local kubeconfig.

Failure to fetch the kubeconfig or resolve the Kubernetes node name stops the
upgrade before reboot; silently skipping drain would violate the selected safe
upgrade workflow. After t9s cordons a node, every later return path attempts to
uncordon it. Drain uses a five-minute timeout, respects PodDisruptionBudgets,
and follows Talos's reusable node-drain behavior for mirror/static and
DaemonSet-managed pods. After reboot, t9s waits for the Kubernetes Node Ready
condition before uncordoning. This write path is reachable only from the
already-gated `--write` upgrade action.

## Application Data Flow

The existing confirmation remains the authority boundary. The current
`application.Effect` contract returns exactly one message, so it is not changed
into a multi-message primitive. Instead, the upgrade start effect creates a
buffered private channel, launches one controller call in a goroutine, and
returns `UpgradeStarted` carrying the receive-only channel. The reducer stores
that channel in private model state and returns an effect that reads exactly one
update. Every `UpgradeProgressed` reduction schedules the next one-item read
until a terminal result closes the stream.

Bubble Tea therefore consumes the operation through the existing
one-effect/one-message path:

```text
Confirm upgrade
  -> UpgradeStarted
  -> UpgradeProgressed (repeated)
  -> UpgradeSucceeded | UpgradeFailed
```

Each message carries the current application generation and target. Stale
messages from a prior context/session are ignored using the same generation
guard as existing asynchronous loads. Only one upgrade may be active at a time.
Other write actions remain disabled while it runs.

The channel carries a private result wrapper containing either one
`ports.UpgradeEvent` or the terminal controller error. It is closed exactly
once by the launching goroutine. A closed channel without a terminal result is
converted to `UpgradeFailed`, never `UpgradeSucceeded`.

Cancellation occurs when the program exits or the session context is replaced.
Leaving the Nodes screen does not cancel an already-confirmed upgrade; progress
remains visible in the global notice area when the operator returns.

## TUI Behavior

The confirmation prompt shows the selected target, image, and any skipped-minor
warning. After confirmation, the normal view remains usable for read-only
navigation, while the footer/notice area shows a compact phase and latest
message. Byte progress is rendered as a bounded percentage when total is known;
otherwise the phase is shown without a fabricated percentage.

Completion produces the existing success notice and refreshes node data.
Failure produces a persistent error notice containing the failed phase and
normalized error. The interface never shows success merely because an RPC was
accepted or a stream ended.

## Error Handling and Safety

- Empty target or image input is rejected before any RPC.
- Lifecycle API selection is based on the running node version, not the t9s SDK
  version.
- Legacy fallback occurs only for the documented version range mismatch, not
  for arbitrary lifecycle errors.
- Context cancellation stops pending RPC/stream work and cleanup uses a bounded
  context when uncordon is required.
- Stream messages and errors are sanitized to single-line notices.
- Upgrade remains behind `--write` and explicit `y` confirmation.
- No credentials, talosconfig content, kubeconfig content, or registry tokens
  are persisted in model state or logs.

## Testing

Adapter tests use fake version, image-pull, lifecycle, reboot/readiness, and
legacy clients to prove:

- exact capability-range boundaries and legacy fallback;
- lifecycle is preferred for v1.13.3;
- pull precedes install, and reboot follows exit code zero;
- pull/stream/reboot errors stop the sequence;
- EOF without exit code and nonzero exit code fail;
- uncordon runs after t9s cordons, including failure paths;
- schematic ID, flavor, and API URL produce the correct Image Factory
  installer suggestion without assuming `metal`;
- `schematic` extension metadata provides the compatibility fallback;
- declared-image fallback and digest behavior remain safe.

Application tests prove ordered progress messages, generation guards,
single-upgrade gating, cancellation, success refresh, and failure notices. TUI
tests prove the warning/confirmation copy, progress rendering with and without
totals, navigation during progress, and golden output. Drain tests prove Talos
kubeconfig and Nodename resolution, cordon/eviction filtering, timeout/error
propagation, readiness waiting, and uncordon.

Final verification is `go test ./...`, `go test -race ./...`, `go vet ./...`,
and `go build ./cmd/t9s`, followed by a manual smoke test against one disposable
Talos node using its current-version image before attempting a real version
change.

## Infrastructure Follow-up

After this t9s increment is complete, a separate design and plan will update the
Talos cluster modules/templates to render explicit `HostnameConfig` documents
for every named node and to set each node's install image from its Terraform
Image Factory schematic ID. That follow-up will be validated with targeted
OpenTofu tests/plans and will not be coupled to the t9s release.
