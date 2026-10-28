# Single control-plane Talos upgrade

Implement the approved planned-downtime upgrade flow. The ordinary quorum refusal remains the default. An upgrade assessment offers an explicit `d` (allow control-plane downtime) option only when the target is the sole control-plane node and sole known healthy etcd member (no learners, alarms, or errors). Reprepare that upgrade with a local snapshot path and downtime intent, then require a separate `y` confirmation displaying downtime and the snapshot destination.

The application validates intent, snapshots with the existing verified snapshot operation under the mutation lease, and rechecks identity, membership, and health after the snapshot before dispatching the normal upgrade stream. Snapshot failure or changed facts stop dispatch. No Talos force option, PDB bypass, membership mutation, or changes to reboot/reset policy. Unknown membership and unhealthy singleton upgrades remain blocked. Normal graceful drain/reboot/readiness/uncordon is retained; legacy Talos retains its existing accepted/unverified result.

Approaches: broad force bypass rejected because it weakens unrelated safeguards; automatically allowing singleton upgrades rejected because downtime needs explicit consent; selected approach is a narrowly scoped application intent with a separate TUI confirmation and mandatory fresh snapshot.

Implementation choices: snapshot path defaults to the existing context/node/timestamp convention; it is shown before confirmation. Existing snapshot code refuses overwrites and validates checksums. Reject membership changes even if the replacement singleton is healthy by retaining member ID in facts. Keep the local receipt in the terminal upgrade result for successful snapshot + upgrade dispatch.
