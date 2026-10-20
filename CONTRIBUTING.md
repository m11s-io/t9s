# Development

## Requirements and commands

Use Go 1.27.2 on Linux to match CI (Go 1.27.1 is the module minimum).
The target Talos SDK and cluster version for this slice is v1.14.4.
Automated checks cover SDK integration; live v1.14.4 compatibility remains
pending until the manual smoke test below is recorded.

```bash
go build ./cmd/t9s
go test ./...
go test -race ./...
go vet ./...
```

Run the startup benchmark with:

```bash
go test ./internal/tui -run '^$' -bench BenchmarkInitialView -benchmem -count=3
```

On the development workstation used for the initial foundation verification,
the three Linux/amd64 runs had a median of **70,952 ns/op**, **24,710 B/op**,
and **59 allocs/op**. These figures are an informational baseline, not a test
threshold: timing and allocation results vary with the Go toolchain, hardware,
and dependency versions. The behavioral guarantee is that every benchmark
iteration constructs and renders the 120x40 initial view without opening a
session or performing network I/O.

## Architecture boundaries

Dependencies point inward:

- `internal/domain` contains small normalized values and imports no Talos SDK
  or terminal packages.
- `internal/ports` defines narrow capabilities in terms of domain values.
- `internal/application` is a frontend-independent backend API. A `Client`
  discovers contexts and opens a `Session`; callers pass contexts to explicit
  typed reads and stream operations. The context passed to `Open` bounds the
  persistent session lifetime, while each operation also accepts its own
  context. `Session.Close` cancels that lifetime and releases its adapter.
  Application public values contain normalized domain and port types, not
  screen state or Talos SDK credentials.
- `internal/adapters/talos` is the only Talos SDK boundary. SDK, protobuf, COSI,
  and credential-bearing configuration types must be converted there and must
  not enter application or TUI state.
- `internal/tui` owns prompts, navigation, request identities, status, and
  viewport state. Its Tea commands make explicit application calls and return
  results to the TUI update loop; they do not invoke a shared UI engine. Keep
  useful Bubbles components for inputs and viewports. `View` remains a pure,
  network-independent projection.
- `cmd/t9s` and `internal/cli` are the composition and process-lifecycle roots.

The session stores its adapter handles privately. Reads return normalized data;
streams are session-bound, cancellable, and idempotently closable. A session
admits at most one mutation at a time, independently of any frontend prompt.
Prepare node, service, and etcd actions before showing confirmation: the
returned prepared plan keeps its canonical request private and one-shot, and
`Assessment()` returns a deep copy for display. Execute rechecks current target
identity, scope, risk facts, and confirmation against the prepared assessment.
Write permission comes from trusted `application.Options` when opening the
session; a mutation request cannot enable writes itself.

Mutation results preserve per-target outcomes when execution returns an error.
For basic bulk node actions, `accepted` means the RPC acknowledged the request
without independently verifying remote completion, and `unconfirmed` means a
dispatched RPC's result is uncertain. If cancellation prevents a target from
being dispatched, its result may have no outcome set. A completed local etcd
snapshot reports `completed`. Upgrade streams use the separate
`ports.UpgradeOutcome` values `accepted`, `applied`, and
`applied-with-recovery-warning`; the warning outcome may include an inspectable
error. The TUI maps that warning to its `domain.OperationRecoveryIncomplete`
presentation outcome, which is not a `MutationResult` status. Cancellation
stops local work but does not roll back a remote request. Never retry a mutation
only because its response was lost. Upgrade streams retain their terminal result
and release the mutation lease after terminal cleanup. `Wait(ctx)` returning
`ctx.Err()` only means that caller stopped waiting; a later `Wait` can still
return the retained port result and its separate wait error.

Bulk node execution invokes its optional per-target observer serially as results
arrive. Callbacks must return promptly; callers that forward events to another
goroutine should use a buffer sized for the targets or otherwise ensure the
observer cannot block result collection. The TUI owns operation stamps and
checks them before applying late results, so a superseded request cannot replace
current presentation state. The TUI also owns any shared cluster snapshot it
passes among views. A future web frontend must define its own transport and
policy for local snapshot paths; no React frontend, HTTP server, authentication,
or download transport is provided here.

Application operations own their client deadlines. Targets in one bulk node
execution share one deadline: 30 seconds for ordinary actions and 30 minutes
for reset or device wipe. A caller deadline or session cancellation can shorten
that budget; targets do not receive separate per-target budgets. The initial
budgets live in `application/operation_policy.go`, with upgrade stage budgets
in the Talos adapter. Keep raw RPC errors out of presentation state.

Normal tests use `internal/testkit` fakes at port boundaries. A test that needs
a live Talos endpoint is a manual smoke test and must be explicitly separated
from `go test ./...` and CI.

## Fixture rules

Fixtures belong in the nearest package's `testdata` directory. They must be
small, deterministic, human-readable, and contain only invented identities,
reserved/example addresses, and fake credentials. Never copy a developer or
production talosconfig, certificate, key, token, hostname, address, or error
message into the repository.

`internal/adapters/talos/testdata/talosconfig.yaml` is a parser fixture only.
Tests may load it to exercise the adapter boundary, but must never use its
endpoints for a connection. Prefer `internal/testkit` fakes for application and
TUI flows, and assert on normalized domain values rather than Talos SDK types.

When a fixture shape changes, keep credentials obviously fake, verify that
serialized domain results do not disclose credential fields or values, and run
the owning package plus the full test suite.

## Optional manual smoke test

On a disposable v1.14.4 cluster, check `:nodes`, `:services` and service
logs, `:events`, `:etcd`, `:bgp`, `:overview`, and `:problems`; inspect node
detail for system volumes, workload isolation, and `sandboxd`; inspect disk
detail for LVM, MD RAID, trim, and scrub status; and browse `:resources` in the
default read-only mode.
Record the t9s commit, Talos version, checks performed, and results without
including endpoints or credentials.

When a disposable Talos v1.14.4 cluster is available, verify that the first
frame appears before node results, unreachable endpoints do not freeze input,
context switching never displays old-context nodes, partial failures preserve
reachable rows, `/` filters without changing the snapshot, and no key or
command offers mutation or arbitrary execution. This live check is intentionally
not part of normal CI.

When `--enable-writes` is set, additionally verify: `space` marks/unmarks
a row and the mark persists across a `/` filter round-trip, `R`/`X` open
an inline confirm prompt naming the correct target(s) and any
control-plane/etcd-quorum warning, any key other than `y` cancels without
calling the cluster, `y` fires the action and the footer reports
per-target success/failure, and that omitting `--enable-writes` leaves
`R`/`X`/`space` fully inert with the header still reading `[RO]`.

## Release packaging

Release builds are produced by [GoReleaser](https://goreleaser.com/) via
`.goreleaser.yml`, triggered in CI by pushing a `v*` tag
(`.github/workflows/release.yml`). To validate changes to `.goreleaser.yml`
without tagging or publishing anything, install GoReleaser locally and run a
snapshot build:

```bash
go install github.com/goreleaser/goreleaser/v2@latest
goreleaser release --snapshot --clean
```

This builds all configured platform/architecture targets, archives, and
checksums the same way a real release does, skipping the GitHub Release
publish step. Inspect the `dist/` directory it produces (gitignored) to
confirm the output.
