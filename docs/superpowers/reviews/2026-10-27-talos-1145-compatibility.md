# Talos v1.14.5 compatibility review

Reviewed the [upstream v1.14.4 to v1.14.5 diff](https://github.com/siderolabs/talos/compare/v1.14.4...v1.14.5) on 2026-10-27, including the release tags in the local `.third_party/talos` checkout.

- No changes to the machinery API definitions, machine configuration apply server, `upgrade-k8s` command, client version command, or `pkg/cluster/kubernetes` orchestration. The existing RPC contracts, CLI flags, and dry-run warning limitations remain applicable.
- The machinery module requirements are unchanged. Updating the SDK requires no transitive dependency bumps.
- Strategic merges now preserve omitted custom values, including CEL expressions and byte sizes. t9s uses the upstream patcher for kubelet configuration updates and receives this fix through the SDK update.
- The new dynamic proxy resolver is used by Talos server controllers. The standard machinery client connection implementation is unchanged; t9s needs no custom dialer migration.
- Runtime lockdown enum helpers are additive. Kernel module loading, MD boot recovery, EFI/kexec, networking, and etcd changes are handled by Talos itself; no corresponding t9s adapter changes were identified.

Adaptation: pin machinery to v1.14.5, require exactly talosctl v1.14.5 for Kubernetes upgrade preparation and execution, and update current documentation and configuration preview wording. Executable fingerprint checks remain in place. Historical v1.14.4 design and smoke-test records retain their original versions.

Verified on macOS arm64 with Go 1.27.3:

- `go mod tidy` (only the machinery version/checksums changed)
- `go test -shuffle=on ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build -o /tmp/t9s-talos-1.14.5 ./cmd/t9s`
- `bun test` in `docs` (14 tests)

All listed checks passed. Live v1.14.5 cluster testing and hardware-specific coverage remain pending; the existing two-node v1.14.4 smoke test is not evidence of v1.14.5 runtime validation.
