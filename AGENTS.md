# Working in t3oxide

Mode: `dev`. [T3 project standard](https://github.com/Xon333/t3/blob/main/docs/policy/PROJECT-STANDARD.md) owns shared source workflow and native settings; this repo owns only client-specific behavior.

t3oxide is a Go terminal client for existing T3 environments. Use [README](README.md) for scope, [architecture](docs/architecture.md) for protocol/recovery, and [installation](NATIVE.md) for installs. The Swift/AppKit client remains available as a legacy rollback.

- Isolate nightly compatibility in `adapter/`; preserve the versioned client protocol or coordinate both sides. Use the existing Go/Bubble Tea UI and Node adapter. Preserve the retained Swift/AppKit rollback source and its separate state. Broader operations remain in T3.
- Preserve drafts, uncertain operations and durable host journals. Never silently retry mutations or discard recovery records.
- Reuse existing T3 instances and access routes. A client change does not authorize restarting, replacing or reconfiguring unrelated services.
- Keep credentials, private host configuration, transcripts, inventories, agent handoffs and machine-specific paths outside Git. Use generic examples and anonymized evidence.

Choose affected checks: `scripts/check-go.sh` covers the primary Go client; `scripts/check.sh` covers the shared adapter/scripts and retained Swift checks. Build the primary client with `scripts/build-go.sh`. Native behavior changes need relevant macOS checks/build from a host-local checkout. Documentation alone needs no app build/deployment. Reuse unchanged evidence; hosted CI is not a prerequisite.

[Verification](docs/verification.md) separates compatibility and installed-instance results from source tests; update it when results change. Do not claim untested GUI behavior or resource savings. Historical acceptance neither prescribes a fleet topology nor authorizes access to another installation.
