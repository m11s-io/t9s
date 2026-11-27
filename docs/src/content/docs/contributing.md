---
title: Contributing
description: Build, test, and extend t9s without crossing its architecture boundaries.
---

Go 1.27.3 is required and matches the Linux CI toolchain. The Talos SDK compatibility target is v1.14.7.

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

## Documentation site

The documentation site uses Node.js 26 or newer and pnpm 12.19.0,
pinned in `docs/package.json`. Install pnpm using the
[official installation instructions](https://pnpm.io/installation).
From the repository root:

```bash
cd docs
pnpm install --frozen-lockfile
pnpm test
pnpm run typecheck
pnpm run build
pnpm run dev
```

Commit `docs/pnpm-lock.yaml` when changing dependencies. Dependency build scripts
are explicitly approved in `docs/pnpm-workspace.yaml`; review new build requests
before approving them. The Go application does not require Node.js or pnpm.

## Compatibility and manual validation

Automated tests check SDK integration without connecting to a Talos cluster.
Live smoke testing against Talos v1.14.7 and disposable-cluster validation of
Kubernetes upgrades remain pending.

Use the [manual smoke-test checklist](https://github.com/m11s-io/t9s/blob/main/CONTRIBUTING.md#optional-manual-smoke-test)
on a disposable cluster. Record the t9s commit, Talos version, checks, and
results without including endpoints or credentials. Write operations require
separate validation with `--enable-writes` and must not be exercised against
production as part of a test.

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
