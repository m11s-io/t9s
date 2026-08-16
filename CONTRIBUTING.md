# Development

## Requirements and commands

Use Go 1.26.3 on Linux to match CI. The supported Talos SDK and cluster version
for this slice is v1.13.3.

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
- `internal/application` owns pure state transitions, typed messages, session
  generations, and cancellable effects. Its reducer performs no I/O.
- `internal/adapters/talos` is the only Talos SDK boundary. SDK, protobuf, COSI,
  and credential-bearing configuration types must be converted there and must
  not enter application or TUI state.
- `internal/tui` renders application state and translates user input into typed
  messages. `View` must remain a pure, network-independent projection; effects
  run only from Bubble Tea commands.
- `cmd/t9s` and `internal/cli` are the composition and process-lifecycle roots.

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

When a disposable Talos v1.13.3 cluster is available, verify that the first
frame appears before node results, unreachable endpoints do not freeze input,
context switching never displays old-context nodes, partial failures preserve
reachable rows, `/` filters without changing the snapshot, and no key or
command offers mutation or arbitrary execution. This live check is intentionally
not part of normal CI.

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
