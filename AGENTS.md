# Working in t3oxide

Mode: `dev`. The [T3 project standard](https://github.com/Xon333/t3/blob/main/docs/policy/PROJECT-STANDARD.md) owns shared source workflow. Work on an isolated branch; do not merge, deploy or change installed services without authorization.

t3oxide is a Rust terminal workspace for existing T3 environments. Read README.md, docs/architecture.md and docs/state-recovery.md. T3 owns threads, execution, requests, permissions, queues and worktrees. The on-demand Node adapter owns the versioned integration boundary and host operation journal. Do not add another runtime, server, scheduler or state authority.

Preserve exact environment/thread/request/operation identities, durable human drafts and uncertain writes. Persist before dispatch. Never retry mutations automatically or erase recovery records to bypass uncertainty. Stop has an independent lane. Verify source and revision before every mutation.

Use pinned SLT v0.26.0 for the active terminal runtime, focus, widgets and output. Native data workers never read stdin or own terminal output. Keep rendering bounded and wake-aware. Preserve unknown state fields and original migration files. Keep credentials, host paths and transcripts outside Git.

For the experimental A-first Rex demo, use the prepared external tools described in docs/rex-companions.md. Default auxiliary views to a new pane in the originating tab using explicit session/pane context. Keep native mutations in the original controller; local form/reviewer saves are not native delivery.

Trace changes to A1–A5, B1–B3 and S1–S6 in docs/architecture.md. Before filling a real implementation gap, evaluate maintained Rust reuse with the authorized Sol High/Terra Xhigh research route; reuse existing evidence for routine glue. Owner decision 10a allows permissive Rust equivalents for restricted selected roles; do not copy or execute uncleared sources.

Run affected checks: scripts/check-rust.sh, scripts/check.sh and tests/slt_terminal.py (test-only pyte 0.8.2). Build with scripts/build-rust.sh. Historical Ratatui PTY/comparison scenarios are retained in tests/rust_terminal.py; they do not certify SLT. Record measured evidence in docs/rust-verification.md; distinguish fixtures, cross-checks and installed-host acceptance. Source work has no installation gate. Review capability records only for an authorized installed action change.
