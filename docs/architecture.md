# Architecture and stack decision

The primary client is a Go terminal application built directly on Bubble Tea v2. It shares the host adapter and T3 authority boundary with the retained legacy Swift/AppKit client. See [Go terminal ownership](go-terminal.md) for package boundaries and [model/navigation semantics](model-navigation.md) for the additive adapter controls.

## Primary stack

| Layer | Choice | Reason |
|---|---|---|
| Terminal UI | Go + Bubble Tea v2, Bubbles, Lip Gloss, Glamour | Upstream event loop, input/list/viewport controls, layout and Markdown rendering; no embedded browser or local UI framework fork. |
| Client → host | OpenSSH process or local stdio + bounded NDJSON | Reuse existing host aliases, keys, host verification and reachable routes; transport needs no listener, credentials API or SSH library. |
| Full T3 access | Configured client-reachable URL | The terminal client displays the configured address; use an existing route or separately managed loopback-only SSH forward when needed. |
| Host adapter | Node 22.4+ standard-library ES modules | Same code on macOS and Linux; reuse the existing runtime, native HTTP/WebSocket and T3's narrow wire/command contracts. No dependency install or adapter transpilation. |
| T3 | Existing unmodified instance | Retains providers, durable history, queues, workspaces and execution ownership. |

The adapter uses directly executable `.mjs` and does not import the private Effect/T3 monorepo. It adapts the necessary source contracts without an adapter dependency install or compilation step. The Go client has its own pinned production dependency graph in `go.mod`/`go.sum`. Runtime bounds and focused protocol tests replace any claim of whole-T3 type safety.

Go owns the CLI, terminal presentation, session coordination and durable client state. T3-specific command and protocol interpretation stays in the shared host adapter. No local agent runner, model runtime, prompt queue, transcript database or polling service is added. Binary size and language are not evidence of memory or battery savings.

## Build and retained rollback

`scripts/build-go.sh` builds the primary `dist/t3oxide` executable. `scripts/check-go.sh` checks formatting, tests, vet and race detection with the production dependency graph. Shared adapter/script checks remain in `scripts/check.sh`. CI publishes `t3oxide-terminal-macos-arm64` with `NOTICE` and `LICENSES/`.

The Swift/AppKit source remains in `mac/` as a legacy rollback. Its native text selection, editing, windowing and menus remain available through `scripts/build-mac.sh`; CI retains its model and transport checks and AppKit build, publishing `t3oxide-legacy-swift-macos-arm64`. See [installation](../NATIVE.md) for both clients.

Go uses `go-drafts.json` and appearance settings in `terminal.json`; Swift keeps `drafts.json`. The first normal Go launch imports validated Swift state only when the Go file is absent and never modifies the Swift original. Later Go drafts, presets and pending operations are not transferred back to Swift. Preserve both client state files and the shared host `operations-v1/` journal when changing clients.

## Reuse and sources

All T3-specific interpretation is in `adapter/`, anchored to upstream commit `0e7346ccd9427c06161e222319e718e6bd70887f` (12 October 2026). Source adaptations, not a bundled T3 runtime:

- [Client commands](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/client-runtime/src/operations/commands.ts): user-message provenance, delivery intent, queue-holding interrupt, responses, launch/workspace binding.
- [HTTP contracts](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/environmentHttp.ts): descriptor, loopback bearer session/ticket, bounded snapshot, shell and history.
- [V2 contracts](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/orchestrationV2.ts): sequence-tagged events, item identities, runtime requests and command payloads.
- [Effect RPC envelopes](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/.repos/effect-smol/packages/effect/src/rpc/RpcMessage.ts): Request, Chunk/Ack, Exit and subscription Interrupt. Original notices are in [NOTICE](../NOTICE).
- [Node WebSocket](https://nodejs.org/download/release/v22.4.0/docs/api/globals.html#websocket): standard-library client, stable from 22.4.

Alternatives reviewed: [T3 SwiftUI experiment](https://github.com/pingdotgg/t3code/pull/5178) is broad and unmerged in the reviewed source; [ThomasCrund/t3code-mcp](https://github.com/ThomasCrund/t3code-mcp) is an HTTP/MCP control implementation with polling/workspace limits, not verified against these nightlies. Built-in T3 MCP has agent provenance and narrower approval/Stop semantics. These are references, not hidden runtime dependencies.

## Resource ownership

One connection, one selected detailed thread. Configured servers stay ready independently. Switching environments or quitting ends the owned helper connection; T3 execution continues independently. The Go session rejects stale view/action results after switches. No automatic agent/server start, default-host fallback, timer service or extra model is introduced. The legacy Swift app also closes its helper when hidden, minimized or sleeping.

The adapter subscribes to the selected thread, directly patches known visible message updates, and coalesces other relevant changes into a fresh bounded snapshot. This intentionally reuses server-owned visibility/history rules rather than copying T3's complete event projector. It is event-triggered, not periodic full-history polling. The tradeoff is an additional loopback bounded read for structural updates and initially unseen messages.

Shared payload bounds: 16 MiB upstream HTTP/frame check; 384 KiB client frame; 96 KiB encoded input; 144 KiB transcript row budget; 80 rows; 24 KiB displayed text per item; 200 matching thread summaries; eight displayed pending requests. The Go composer additionally limits editable text to 64 KiB and 9,999 lines. The legacy Swift decoder queue is capped at 2 MiB. A WebSocket implementation may allocate a frame before our post-delivery size check; this is not an exact peak-heap bound.

Earlier pages **replace** the current view and pause live detail until Latest. Very long individual content is labeled shortened; full T3 remains available for complete content. The Go client renders Markdown and fenced code/diffs through Glamour with bounded, keyed rendering caches. Full tool payloads, embedded terminals/browser previews, attachment uploads and full-history search remain in T3.

The Terminal theme inherits terminal default foreground/background and uses ANSI semantic colors for accents; fixed violet, cyan and lime themes remain available. This does not promise exact syntax colors from the terminal theme. See [theme controls](../NATIVE-GO.md#themes). The legacy Swift client displays Markdown as plain text and follows system appearance.

## Compatibility and actions

Client protocol v1 contains small user-facing fields, never full T3 projections. Each host validates its configured environment identity and V2 protocol. Server-side delivery selection must be advertised before sends are enabled. Additive fields are tolerated, unsupported requests are surfaced, and missing bounded routes never fall back to full history.

The tested source revision is **not** an installed-version claim or an all-future-nightlies compatibility guarantee. A breaking T3 protocol change is fixed in the host adapter. Client updates are needed when they consume new adapter capabilities or change local behavior. No updater is installed, no upstream code is downloaded during connection, and the main T3 server is never pinned/downgraded by this client.

Every write has a persistent client operation ID. Reconnect does not automatically resend writes. The adapter checks live controls before the first dispatch, then durably saves the exact command and a fingerprint of the client request in a private host journal. Explicit retries use that command even after a helper restart; reusing an ID with different content is rejected. Records are bound to the authenticated subject, environment and local user, so credential renewal for the same subject preserves recovery.

The installed T3 dispatch path consults its durable command receipt before current control state. That lets a retry recover an already-applied Stop or answer without applying it to a later run or request. An acknowledgement is also saved before it reaches the client. No success is inferred from a stopped run or resolved question alone. The journal has 4,096 slots and never silently evicts or reuses an operation ID; full or damaged storage blocks new writes while preserving existing recovery records. It contains operation payloads, not credentials, and must be preserved with host state. The local resolved-action control remains available for explicit resolution; it does not cancel or replay server work.

Stop means **the reviewed active turn with queue hold**, not a claim of stopping every background workflow/watch after a turn ends. Approval choices are preserved; unknown/truncated/nonresumable requests are not actionable. New conversation means the selected existing project/checkout/model/mode, without copying history or automatically creating a new worktree. Full T3 remains the owner of broader operations.
