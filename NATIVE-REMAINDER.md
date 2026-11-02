# Checks requiring an actual installation

The implementation and synthetic integration checks are complete in this source
branch. [Rust verification](docs/rust-verification.md) lists the exact executed
checks and their limits. No source change authorizes deployment or changes to
existing T3 services or user state.

Only these observations require the owner's existing machines:

- Run the Rust executable in the actual terminal and SSH/VPN route, confirming
  physical key mappings, mouse selection, Unicode width and clipboard acceptance.
- Use the read-only `doctor`, `capabilities` and `status` commands against the
  installed T3 version and configured provider/account identities.
- If separately authorized, exercise disposable native requests, worktree creation
  and exact-ID recovery with the installed provider. Preserve state and journals;
  never use a new operation ID to retry an uncertain original.
- Measure process-tree memory, CPU and terminal latency in the real workload.
  Linux fixture measurements do not establish macOS battery or energy savings.

These are installation-specific verification tasks. They do not contain unfinished
application implementation. Build and configuration instructions are in [NATIVE.md](NATIVE.md).
