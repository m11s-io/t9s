---
title: Contributing
description: Build, test, and extend t9s without crossing its architecture boundaries.
---

Use Go 1.26.3 on Linux to match CI. The supported Talos SDK and cluster version is v1.13.3.

```bash
go build ./cmd/t9s
go test ./...
go test -race ./...
go vet ./...
```

Run the startup benchmark with:

```bash
go test ./internal/tui -run "^$" -bench BenchmarkInitialView -benchmem -count=3
```

## Architecture boundaries

Dependencies point inward:

- `internal/domain` contains normalized values without Talos SDK or terminal dependencies.
- `internal/ports` defines narrow capabilities in domain terms.
- `internal/application` owns pure transitions, typed messages, session generations, and cancellable effects.
- `internal/adapters/talos` is the only Talos SDK boundary.
- `internal/tui` renders state and translates input; rendering remains network independent.
- `cmd/t9s` and `internal/cli` are composition and process-lifecycle roots.

## Tests and fixtures

Normal tests use fakes from `internal/testkit` at port boundaries. Live Talos checks must remain manual and separate from `go test ./...`.

Fixtures belong in the nearest `testdata` directory. Keep them small, deterministic, human readable, and entirely invented. Never copy production credentials, endpoints, hostnames, addresses, or error messages into the repository.
