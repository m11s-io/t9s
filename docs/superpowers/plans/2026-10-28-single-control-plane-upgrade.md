# Single control-plane upgrade implementation plan

Goal: enable explicitly confirmed downtime for healthy singleton control-plane Talos upgrades.
Spec: ../specs/2026-10-28-single-control-plane-upgrade-design.md

1. Add policy and operation regression tests: singleton default refusal and eligibility; opted-in snapshot-before-upgrade; snapshot failure; unknown/unhealthy/multi-member refusal; pre/post-snapshot fact changes and confirmation.
2. Extend NodeActionRequest/Assessment with scoped downtime intent and eligibility, snapshot path, and retained membership identity. Validate options and enforce eligibility in preparation and execution. Snapshot using existing verified receipt validation and recheck after snapshot; preserve receipt in UpgradeResult.
3. Add TUI tests then a `d` choice on eligible blocked upgrades, reprepare with generated snapshot path, and a separate `y` confirmation explaining API/scheduling downtime and snapshot location. Keep cancel/session-generation protections.
4. Update node/security guides. Run focused tests, full go test ./..., race tests for affected packages, and build. Review diff for unintended guard bypasses. Do not run a live upgrade.

Review focus: learners/unknown membership; alarms/unhealthy singleton; changed member ID; snapshot failure/cancellation; stale TUI/session inputs. Each is covered by the policy, operation, or TUI regression tests.

Execution ledger: user authorized implementation of the proposed flow; execute inline in the clean existing checkout. No deployment or cluster mutations.

Completed: all four steps. Added bounded retries for temporary Kubernetes API
unavailability during readiness recovery; regression tests demonstrated the old
immediate failure before the fix. This is necessary for single-control-plane
reboots and preserves timeout, cancellation and boot-identity checks.

Validation: full `go test ./...` passed; `go test -race ./internal/application
./internal/tui ./internal/adapters/talos` passed; `go build -o
/tmp/t9s-single-cp-downtime ./cmd/t9s` and binary `--help` passed; `git diff --check`
passed. Read-only independent review found no Critical/Important issues; its two
minor coverage suggestions (post-snapshot node replacement and stale downtime
prompt inputs) were added and passed. No live Talos upgrade was performed.
