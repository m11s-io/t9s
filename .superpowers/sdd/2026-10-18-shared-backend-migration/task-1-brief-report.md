# Task 1 report: baseline and regression inventory

Status: DONE

The baseline report records HEAD, branch, exact tracked/untracked state and ownership, capability-to-test mappings, and node/service/etcd mutation plus stream variants. It confirms that operation outcome distinctions, timeout policy, and recovery-warning behavior are already in the baseline. No product files were changed.

Verification at baseline `8031b90ad40dc306441c6c99823ede17242fcdf9`:

- `go test ./...`: four existing expectation mismatches in application/TUI suites.
- `go test -race ./...`: same four failures; no additional failure.
- `go vet ./...`: passed.

See [baseline inventory](../../../docs/superpowers/reviews/2026-10-18-shared-backend-baseline.md) for the exact test names and results. Commit only these two task-owned reports after review.
