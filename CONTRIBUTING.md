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
- `internal/application` owns typed messages, screen transitions, session
  generations, request identities, and cancellable effects. `Update` coordinates
  the session request owner and dispatches to `updateModel` for screen state
  transitions; it can cancel superseded work. Backend calls run inside effects.
- `internal/adapters/talos` is the only Talos SDK boundary. SDK, protobuf, COSI,
  and credential-bearing configuration types must be converted there and must
  not enter application or TUI state.
- `internal/tui` renders application state and translates user input into typed
  messages. `View` must remain a pure, network-independent projection; effects
  run only from Bubble Tea commands.
- `cmd/t9s` and `internal/cli` are the composition and process-lifecycle roots.

Application `Model` exposes normalized screen data. Its private session runtime
holds port readers, controllers, streams, and request ownership; those handles
are not presentation data. A session owns cancellation, each replaceable read
has its own request identity, and unary reads have timeouts. Leaving a diagnostic
or changing context releases its work. Generation and request checks reject late results.

`EffectBatch` describes independent work that the frontend must schedule as
separate effects. The TUI maps its children to Bubble Tea commands, so a blocked
read does not hold up another snapshot. Node health and detailed services share
one uncapped service observation per refresh; only the detailed table is capped.

Confirmation messages produce execution effects after application policy checks.
Operation identities isolate mutation results from older operations. The TUI
owns prompts, navigation, filters, and selection; it does not construct backend
actions from display text. Port operations and SDK adapters can be reused by a
future API frontend. This application does not add a web server.

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
logs, `:events`, `:etcd`, and `:resources` in the default read-only mode.
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
