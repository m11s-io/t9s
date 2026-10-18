# Shared backend migration plan review

Scope: planning documents only; no product implementation or live operations.

Baseline observed at final review: `main` at `8031b90`. The checkout changed from the earlier dirty operation-ownership branch during planning; the documents were updated to the committed baseline.

## Delegation

- Two `gpt-6-luna` workers independently inventoried backend capability/policy and TUI state/lifecycle/call sites. Both were read-only.
- A `gpt-6.1-sol` reviewer assessed the action/session design, reviewed the written spec and plan, then verified corrections to its findings.
- The primary agent wrote and revised the planning documents. Implementation remains subject to user review of those documents.

## Findings resolved

1. Upgrade streams could drop terminal results after cancellation or block on progress consumption. The plan now requires repeatable retained `Wait(ctx)` completion after adapter cleanup, plus port/adapter/fake changes and regressions. Cancel alone cannot release mutation exclusion.
2. Current-install-image reads and cluster-health IP construction were missing from explicit capability coverage. Both are now included with exact signatures and tests.
3. Session Open context lifetime was ambiguous. It is now explicitly the lifetime parent; the TUI uses a dedicated persistent session context and closes late successful opens. Optional correlation errors and parent cancellation are distinguished.
4. An arbitrary synchronous callback cannot be guaranteed nonblocking. Bulk result observation now has an explicit trusted prompt-return precondition and nil-observer aggregate behavior.
5. Temporary TUI cutover plumbing was unspecified. The plan now names directModel/private test construction, temporary state aliases, legacy-only CLI startup during migration, and final promotion/deletion. There is no dual-executor runtime mode.
6. Large TUI tasks were unsuitable for bounded Luna workers. Cluster, diagnostic/resource, and mutation UI work now have sequential feature subtasks with individual tests and Sol review.
7. The old wording invented a protected-service denylist and mislabeled an existing test file as new. It now preserves the actual etcd service stop/restart quorum policy and modifies/migrates the existing tests.

Sol's final verification reported no residual blocker within those findings. This is design/plan review evidence, not evidence that an implementation passes checks.

## Document checks

Both new documents were checked for unresolved placeholders, interface mismatches, missing capability groups, and trailing whitespace. Final execution must establish a fresh baseline and run the verification commands in the plan. No test success from an earlier code state is asserted for the future migration.

## Artifacts

- [Design](../specs/2026-10-18-shared-backend-migration-design.md)
- [Implementation plan](../plans/2026-10-18-shared-backend-migration.md)
