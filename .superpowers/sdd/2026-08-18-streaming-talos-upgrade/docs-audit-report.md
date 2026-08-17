# Documentation audit: streaming Talos upgrade

## Pages changed

- `README.md`: Talos-only scope, write gating, schematic-preserving image suggestions, lifecycle progress, legacy fallback, and skipped-minor warnings.
- `docs/src/content/docs/guides/nodes.md`: operator workflow, schematic/digest fallback, lifecycle stages, legacy behavior, semantic-version warning, and separate Kubernetes boundary.
- `docs/src/content/docs/security.md`: LifecycleService qualification for drain/readiness/uncordon and a Nodes-guide link.
- `docs/src/content/docs/reference/commands.md`: `U` action and write requirement.
- `docs/src/content/docs/reference/environment-variables.md`: full write-action scope.

## Constraints checked

Upgrades remain gated by `--enable-writes`/`T9S_ENABLE_WRITES` and confirmation. Lifecycle success requires exit code zero plus cleanup; legacy fallback is separate; skipped-minor warnings apply only beyond one minor; digest references remain unchanged; Kubernetes control-plane upgrades are separate. No adapter/application code was changed.

## Verification

- `bun test tests/content.test.ts`: 13 passed, 0 failed.

## Concerns

Pinned Talos machinery v1.13.3 does not expose `ImageFactorySchematic`; live discovery uses the `ExtensionStatus` resource named `schematic`. Keep unavailable or undecodable metadata on the declared-image fallback.
