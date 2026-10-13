# Architecture and stack decision

An occasional native companion alongside the full T3 experience.

## Stack, settled for this implementation

| Layer | Choice | Reason |
|---|---|---|
| Mac window, text, input | Swift + AppKit | Reuse native text selection, editing, windowing and menus; no browser renderer or cross-language UI bridge. |
| Client → host | macOS OpenSSH process + bounded NDJSON | Reuse existing host aliases, keys, host verification and reachable routes; native transport needs no listener, credentials API or SSH library. |
| Full-browser shortcuts | Separate loopback-only SSH forwards | Use an existing client-reachable URL or a separately managed loopback-only SSH forward. |
| Host adapter | Node 22.4+ standard-library ES modules | Same code on macOS and Linux; reuse the existing runtime, native HTTP/WebSocket and T3's narrow wire/command contracts. No dependency install or adapter transpilation. |
| T3 | Existing unmodified instance | Retains providers, durable history, queues, workspaces and execution ownership. |

The earlier TypeScript recommendation is deliberately narrowed to directly executable `.mjs`: this small adapter does not import the private Effect/T3 monorepo. It adapts its necessary source contracts instead. There is no production dependency graph or separate compilation step to maintain. Runtime bounds and focused protocol tests replace any claim of whole-T3 type safety.

Rust is not rejected on performance grounds. Here it would add a GUI toolkit or Swift bridge and a second T3 protocol translation without an established benefit. Likewise, porting the broad experimental SwiftUI client would increase scope. Binary size and language are not evidence of memory or battery savings.

## Reuse and sources

All T3-specific interpretation is in `adapter/`, anchored to upstream commit `0e7346ccd9427c06161e222319e718e6bd70887f` (12 October 2026). Source adaptations, not a bundled T3 runtime:

- [Client commands](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/client-runtime/src/operations/commands.ts): user-message provenance, delivery intent, queue-holding interrupt, responses, launch/workspace binding.
- [HTTP contracts](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/environmentHttp.ts): descriptor, loopback bearer session/ticket, bounded snapshot, shell and history.
- [V2 contracts](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/orchestrationV2.ts): sequence-tagged events, item identities, runtime requests and command payloads.
- [Effect RPC envelopes](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/.repos/effect-smol/packages/effect/src/rpc/RpcMessage.ts): Request, Chunk/Ack, Exit and subscription Interrupt. Original notices are in [NOTICE](../NOTICE).
- [Node WebSocket](https://nodejs.org/download/release/v22.4.0/docs/api/globals.html#websocket): standard-library client, stable from 22.4.

Alternatives reviewed: [T3 SwiftUI experiment](https://github.com/pingdotgg/t3code/pull/5178) is broad and unmerged in the reviewed source; [ThomasCrund/t3code-mcp](https://github.com/ThomasCrund/t3code-mcp) is an HTTP/MCP control implementation with polling/workspace limits, not verified against these nightlies. Built-in T3 MCP has agent provenance and narrower approval/Stop semantics. These are references, not hidden runtime dependencies.

## Resource ownership

One connection, one selected detailed thread. Configured servers stay ready independently; the companion does not keep two complete UI state trees alive. Hidden/minimized/sleeping app closes its owned helper connection. No automatic agent/server start, migration, default-host fallback, timer service, transcript database or extra model.

The adapter subscribes to the selected thread, directly patches known visible message updates, and coalesces other relevant changes into a fresh bounded snapshot. This intentionally reuses server-owned visibility/history rules rather than copying T3's complete event projector. It is event-triggered, not periodic full-history polling. The tradeoff is an additional loopback bounded read for structural updates and initially unseen messages.

Payload bounds: 16 MiB upstream HTTP/frame check; 384 KiB companion frame; 96 KiB input; 144 KiB transcript row budget; 80 rows; 24 KiB displayed text per item; 200 matching thread summaries; eight displayed pending requests. The native decoder queue is capped at 2 MiB. A WebSocket implementation may allocate a frame before our post-delivery size check; this is not an exact peak-heap bound.

Earlier pages **replace** the current view and pause live detail until Latest. Very long individual content is labeled shortened and opened in full T3, not silently called complete. No rich diff, terminal, browser preview, attachment upload or full Markdown renderer in v0.1. Text remains selectable; Markdown is shown as text. The app follows the system appearance.

## Compatibility and actions

Client protocol v1 contains small user-facing fields, never full T3 projections. Each host validates its configured environment identity and V2 protocol. Server-side delivery selection must be advertised before sends are enabled. Additive fields are tolerated, unsupported requests are surfaced, and missing bounded routes never fall back to full history.

The tested source revision is **not** an installed-version claim or an all-future-nightlies compatibility guarantee. A breaking change is fixed in the host adapter. The app stays unchanged while its v1 contract remains compatible. No updater is installed, no upstream code is downloaded during connection, and the main T3 server is never pinned/downgraded by this client.

Every write has a persistent client operation ID. Reconnect does not automatically resend writes. The adapter checks live controls before the first dispatch, then durably saves the exact command and a fingerprint of the client request in a private host journal. Explicit retries use that command even after a helper restart; reusing an ID with different content is rejected. Records are bound to the authenticated subject, environment and local user, so credential renewal for the same subject preserves recovery.

The installed T3 dispatch path consults its durable command receipt before current control state. That lets a retry recover an already-applied Stop or answer without applying it to a later run or request. An acknowledgement is also saved before it reaches the client. No success is inferred from a stopped run or resolved question alone. The journal has 4,096 slots and never silently evicts or reuses an operation ID; full or damaged storage blocks new writes while preserving existing recovery records. It contains operation payloads, not credentials, and must be preserved with host state. The local resolved-action control remains available for explicit resolution; it does not cancel or replay server work.

Stop means **the reviewed active turn with queue hold**, not a claim of stopping every background workflow/watch after a turn ends. Approval choices are preserved; unknown/truncated/nonresumable requests are not actionable. New conversation means the selected existing project/checkout/model/mode, without copying history or automatically creating a new worktree. Full T3 remains the owner of broader operations.
