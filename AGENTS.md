# Working in t3oxide

Mode: `dev`. The [T3 project standard](https://github.com/Xon333/t3/blob/main/docs/policy/PROJECT-STANDARD.md) owns shared source workflow. Work on an isolated branch; do not merge, deploy or change installed services without authorization.

t3oxide is a Rust terminal workspace for existing T3 environments. Read README.md, docs/architecture.md and docs/state-recovery.md. T3 owns threads, execution, requests, permissions, queues and worktrees. The on-demand Node adapter owns the versioned integration boundary and host operation journal. Do not add another runtime, server, scheduler or state authority.

Preserve exact environment/thread/request/operation identities, durable human drafts and uncertain writes. Persist before dispatch. Never retry mutations automatically or erase recovery records to bypass uncertainty. Stop has an independent lane. Verify source and revision before every mutation.

Use Ratatui/Crossterm and existing upstream components. Keep rendering bounded and event-driven. Preserve unknown state fields and original migration files. Keep credentials, host paths and transcripts outside Git.

Run affected checks: scripts/check-rust.sh, scripts/check.sh and tests/rust_terminal.py. Build with scripts/build-rust.sh. Record measured evidence in docs/rust-verification.md; distinguish fixtures, cross-checks and installed-host acceptance. Source work has no installation gate. Review capability records only for an authorized installed action change.
