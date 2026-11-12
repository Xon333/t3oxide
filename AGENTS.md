# t3oxide

Mode: `dev`. Follow the [T3 standard](https://github.com/Xon333/t3/blob/main/docs/policy/PROJECT-STANDARD.md). Isolate work; merge/install/deploy only with authorization.

Read README.md, docs/architecture.md and docs/state-recovery.md. T3 owns execution/state; Node adapter owns versioned boundary/host journal. Add no runtime/server/scheduler/state authority. Preserve deprecated Swift source/state; do not develop/build/test it.

Verify source/revision before mutations. Persist exact identities, drafts and uncertain writes before dispatch; never auto-retry mutations or erase recovery records. Stop is independent. Preserve unknown fields/original migrations; keep credentials, host paths and transcripts outside Git.

Use SLT v0.26.0 for terminal/focus/widgets/output; workers never read stdin or own output. Bound rendering; wake on demand. Trace A1–A5, B1–B3, S1–S6. For gaps, evaluate Rust reuse via Sol High/Terra Xhigh; reuse evidence for glue. Decision 10a permits permissive equivalents; no uncleared sources.

Affected checks: scripts/check-rust.sh, scripts/check.sh, tests/slt_terminal.py (pyte 0.8.2); build: scripts/build-rust.sh. tests/rust_terminal.py cannot certify SLT. Log fixture/cross-check/host evidence in docs/rust-verification.md. No source install gate; review capabilities only for installed changes.

Commit and PR titles describe concrete changed behavior and follow repository history. Add commit bodies only for rationale or compatibility details; report observed checks accurately.
