# Verification

This public record describes software behavior and compatibility. Private deployment inventories, machine identities, access routes, and delivery locations are maintained separately and must not be committed here.

## Tested compatibility

- Native client: version 0.1.1, build 2; ARM64, macOS 14 minimum; built with Apple Swift 6.4.
- T3: `0.0.46-nightly.20261009.2861`, orchestration protocol 2, tested on macOS and Linux.
- Distribution: ad-hoc signed application; not notarized.

These are tested versions, not a guarantee of compatibility with future T3 nightlies. No resource or battery savings are claimed.

## Source and CI checks

Before publication of this clean source snapshot, CI passed the adapter and Mac jobs, including native model/transport checks, the ARM64 AppKit build, and artifact upload. Original run records are retained privately; this statement does not claim a new run on this repository.

Focused checks covered:

- Bounded keyed deltas, revision mismatch recovery, metadata replacement, UTF-8 byte budgets, and long upstream identifiers.
- Historical pagination and reconnect state, independent Stop capacity, uncertain-operation persistence, and RPC liveness.
- Native small-frame pipe reads, EOF, cancellation, blocked writes, stale connections, and descriptor/process cleanup.
- Durable journal publication, authenticated identity isolation, conflicting retry rejection, and Stop bypassing a stalled normal-send acknowledgement.
- Twelve HTTP/WebSocket/stdio fault-injection cases: Stop, approval, and question recovery at four dispatch/acknowledgement boundaries. Fresh helpers recovered exact operations without affecting later work or exposing tokens.

The loopback fixture uses a simulated T3 endpoint. Its results are separate from installed-instance acceptance.

## Installed-instance acceptance

Actual compatible installations exercised synchronized views and incremental updates, human-authored messages, provider questions and answers, queued Stop, explicit same-ID retry after interrupted acknowledgement, earlier history, live and historical reconnect, and idle Ping/Pong.

Native AppKit checks exercised text insertion/selection/copy, draft isolation, Earlier/Latest navigation, cached reconnect state, independent Stop availability, light/dark text appearance, and window/helper lifecycle. These controls were exercised in process against production AppKit source and real helpers; this is not a claim of external mouse/keyboard automation. The packaged application also launched successfully.

For actual question-answer and Stop recovery, a helper was killed after the server accepted the command but before local acknowledgement. A fresh ordinary helper recovered the original authoritative receipt; another fresh helper returned the same receipt. The operation outcome was not inferred solely from current UI state.

T3 services continued running during acceptance. Private host configuration and deployment receipts are intentionally excluded from this public record.
