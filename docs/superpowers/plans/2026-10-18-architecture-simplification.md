# Architecture simplification implementation plan

**Goal:** Complete the source review recommendations on the current merged baseline, keeping reusable Go operations and UI-specific interaction state.

**Architecture:** Preserve domain values and SDK adapters. Centralize request ownership and action dispatch, remove accidental read dependencies, and simplify presentation controls. No new web server, registry, event bus or dependency framework.

**Scope:** User authorized full implementation after the architecture review and first upgrade recovery fix. Continue in the existing isolated worktree on `fix/upgrade-recovery`. Preserve the completed manual-recovery simplification.

## Tasks

- [x] Upgrade outcomes: preserve cleanup warnings; remove post-terminal display-driven writes.
- [x] Stateful log decoding; record-oriented dmesg; metadata-only resource lists.
- [x] Explicit per-request identity, cancellation and timeouts for independently replaceable reads; eliminate obsolete dependency transport/duplicated binding where practical.
- [x] Action confirmation produces execution; operation identity rejects older mutation results; snapshot confirmation enforces policy.
- [x] Independent snapshot loading; share service observations without health truncation; bounded member fan-out.
- [x] Small list controls, selective screen synchronization, normalized resource data remains application-owned.
- [x] Remove unused routes, fields, helpers, test-only exports and domain terminal formatting; narrow Kubernetes reader.
- [x] Full Go tests, race checks, vet, build, documentation checks and independent final review.

## Checks and constraints

Use focused failing regressions for new behavior, then focused passing tests. Keep SDK types and credentials in adapters. Requests must retain generation plus request identity; canceling a view/session releases I/O, and a late result cannot overwrite its replacement. Preserve unknown/partial results, atomic snapshot writes, explicit write gates and quorum checks. No live operations, publishing, pushing or merging as part of this implementation.

## Execution record

Implementation continues directly under the user's instruction to implement the full review. Independent adapter/list work is delegated; shared application and root UI changes remain with the primary agent. Existing baseline full Go suite passed before the first change.


## Completion evidence

- Full `go test ./...` and `go test -race ./...` passed on the final implementation.
- `go vet ./...` passed; build output is `/tmp/t9s-architecture`.
- Documentation checks: `bun test` passed (14 tests).
- Independent review identified composite-failure loading state and stale sibling prompts. Both were reproduced with failing regressions and fixed; the full suites passed afterward.
- Additional regressions cover superseded same-node reads, parent/detail scope changes, unstarted canceled work, snapshot confirmation policy/duplication, stale mutation results, pre-stream upgrade gating, independent snapshot completion order, and stream cleanup through command navigation.
- Runtime handles remain private; screen snapshots remain normalized. Existing screen constructors retain their data views for compatibility, while result synchronization now touches only affected screens. No new frontend framework or API server was introduced.
- No live cluster operations, publishing, pushing, or merging were performed.
