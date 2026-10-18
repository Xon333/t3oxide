# Working in t3oxide

t3oxide is a lightweight native client for existing T3 environments. Use [README](README.md) for scope, [architecture](docs/architecture.md) for protocol/recovery, and [installation](NATIVE.md) for installs.

- Isolate nightly compatibility in `adapter/`; preserve the versioned client protocol or coordinate both sides. Keep Swift/AppKit for the UI and Node for the adapter's existing T3 interfaces/runtime. Broader operations remain in T3.
- Preserve drafts, uncertain operations and durable host journals. Never silently retry mutations or discard recovery records.
- Reuse existing T3 instances and access routes. A client change does not authorize restarting, replacing or reconfiguring unrelated services.
- Keep credentials, private host configuration, transcripts, inventories, agent handoffs and machine-specific paths outside Git. Use generic examples and anonymized evidence.

Choose affected checks; `scripts/check.sh` is the source-check entry point. Native behavior changes need relevant macOS checks/build from a host-local checkout. Documentation alone needs no app build/deployment. Reuse unchanged evidence; hosted CI is not a prerequisite.

[Verification](docs/verification.md) separates compatibility and installed-instance results from source tests; update it when results change. Do not claim untested GUI behavior or resource savings. Historical acceptance neither prescribes a fleet topology nor authorizes access to another installation.
