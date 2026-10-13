# Working in t3oxide

Read [README.md](README.md), [NATIVE.md](NATIVE.md), and the relevant parts of [docs/architecture.md](docs/architecture.md) before changing behavior.

- Keep the native client lightweight. Isolate T3 nightly compatibility in `adapter/`; preserve the versioned client protocol or coordinate both sides of a change.
- Reuse existing T3 instances, SSH routes, and runtimes. Do not restart, replace, or reconfigure unrelated services as part of a client change.
- Keep credentials, private host configuration, transcripts, and machine-specific paths out of Git. Use placeholders in examples.
- Preserve drafts, uncertain operations, and durable host journals. Never silently retry mutations or discard recovery records.
- Run affected checks with `scripts/check.sh`; native changes also require the macOS build and relevant native checks. Reuse passed evidence unless a change invalidates it.
- Keep deployment inventories, personal setup history, and agent handoffs in private storage outside this repository; publish only anonymized technical evidence.
- Record meaningful compatibility and installed-instance results in [docs/verification.md](docs/verification.md), distinguishing source tests from actual integration. Do not claim untested GUI behavior or resource savings.
- Keep active checkouts local to each host. When delegating, assign nonoverlapping files and one integration owner.

Historical acceptance records describe the environments tested, not required fleet topology or authority to access another installation.
