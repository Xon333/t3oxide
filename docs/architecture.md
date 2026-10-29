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

All T3-specific interpretation is in `adapter/`. Its historical upstream source anchor is commit `0e7346ccd9427c06161e222319e718e6bd70887f` (12 October 2026). These are source adaptations, not a bundled T3 runtime or evidence of compatibility with a currently installed server:

- [Client commands](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/client-runtime/src/operations/commands.ts): user-message provenance, delivery intent, queue-holding interrupt, responses, launch/workspace binding.
- [HTTP contracts](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/environmentHttp.ts): descriptor, loopback bearer session/ticket, bounded snapshot, shell and history.
- [V2 contracts](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/orchestrationV2.ts): sequence-tagged events, item identities, runtime requests and command payloads.
- [Effect RPC envelopes](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/.repos/effect-smol/packages/effect/src/rpc/RpcMessage.ts): Request, Chunk/Ack, Exit and subscription Interrupt. Original notices are in [NOTICE](../NOTICE).
- [Node WebSocket](https://nodejs.org/download/release/v22.4.0/docs/api/globals.html#websocket): standard-library client, stable from 22.4.

Historical alternatives reviewed: [T3 SwiftUI experiment](https://github.com/pingdotgg/t3code/pull/5178) was broad and unmerged in the reviewed source; [ThomasCrund/t3code-mcp](https://github.com/ThomasCrund/t3code-mcp) was an HTTP/MCP control implementation with polling/workspace limits, without verified nightly compatibility. The reviewed built-in T3 MCP had agent provenance and narrower approval/Stop semantics. These are historical references, not hidden runtime dependencies or current upstream status claims.

## Browser, modes and detailed-session ownership

Developer starts with a flat, recent native-metadata list from all configured environments. The UI issues at most four metadata helpers concurrently and applies each host result independently, retaining failures and incompleteness. At 80 columns and above the list shares the window with an initially empty right pane. Compact terminals display the list when focused. Highlight/filter/refresh never attaches a transcript; explicit Enter/click resolves the exact source page and thread before asynchronous opening.

`Manager` retains profile scope and lazy sessions. `NavigatorPage` carries profile, environment and exact offered thread metadata. Selection and click identities include source profile plus thread ID; duplicate native IDs on different environments cannot alias. Pagination/search/type/lifecycle state remains per native environment. A source refresh retires its reviewed menu; unrelated unavailable hosts do not retire healthy offers. Flat ordering uses native `updatedAt`, with an exact source identity tie. Project accent is a stable function of environment/project identity, not row position.

`browserMetadata` reuses a verified ready connection for its environment or starts a temporary helper, validates hello identity/protocol and requests metadata. It never starts a detail watch. One detailed Session owns the composer, requests and receipts. Replacing it closes that helper but does not stop T3 work. Client attachment generations, operation IDs, form serials and native revisions reject late or wrong-source results.

AgentBox is another presentation in the same Model. Mode changes keep the same live session, editors, selections, requests, receipts and reading anchors. The inbox reads bounded native metadata, projects genuine lifecycle into Needs you/Working/Queued/Later/Done/Unknown, and deliberately opens one thread for decisions/reply. There is no copied transcript, agent process, task ledger, execution scheduler or per-task watcher. Local project/task rank metadata is presentation only. Native accepted queues continue after exit; future priority-backlog dispatch is unavailable in the verified native contract.

The client keeps up to 64 source-qualified editor/reading return records in memory. They contain component state, exact draft and native row-key/visual-line anchor, not transcript history. Restoring an editor requires equality with the Session's current canonical draft. Unchanged incoming draft events do not move a restored caret. Cross-process durable text remains in the existing state owner; transient selection is not represented as durable backend state.

The detailed adapter still subscribes only to its selected thread, patches known visible message events and coalesces other changes into bounded snapshot refreshes. Streaming text does not trigger inbox polling. AgentBox refreshes on relevant attached lifecycle transitions or explicit user refresh; other environments remain snapshots with explicit observation/failure metadata.

## Projection, history and presentation

The latest adapter projection targets **24 readable rows / 48 KiB** of encoded rows. Earlier history pages retain **80 rows / 144 KiB**. `pageRows` and `trimView` enforce these at the adapter boundary; they do not add server query parameters or reduce the server's bounded-fetch contract. A single row may exceed the smaller page byte target rather than be clipped further to fit it: there is no cursor into the remainder of a message. Existing per-item and overall safety limits still apply, and the page is marked limited.

When local trimming omits older readable rows, the adapter constructs an exact native continuation anchor from the first retained row's authoritative source-thread/item identity, snapshot sequence and position. Otherwise it preserves the server cursor. The Go client treats cursors as opaque; the adapter accepts only the current exact cursor for Earlier. This keeps omitted older rows reachable without inventing a client offset or relying on renumbered page positions. Earlier pages **replace** the current view and pause live detail until Latest. A retained historical page survives reconnect, with further paging requiring Latest first.

The session retains request offers from its last verified live projection separately from the visible historical page. History omits request controls without resolving them; only a later verified live projection replaces the cached offers, including an explicit empty projection. The count and review route survive history and reconnect without resuming live detail or changing run/receipt authority. Historical answers keep the exact request ID/revision and pass the adapter's fresh validation. An answer acknowledgment confirms that operation; cached offers remain until verified live replacement establishes their removal.

Shared safety bounds remain: 16 MiB upstream HTTP/frame check; 384 KiB client frame; 96 KiB encoded input; 80 rows / 144 KiB accepted transcript rows; 24 KiB displayed text per item; 200 matching thread summaries; eight displayed pending requests. The Go composer additionally limits editable text to 64 KiB and 9,999 lines. The legacy Swift decoder queue is capped at 2 MiB. A WebSocket implementation may allocate a frame before the post-delivery size check; these are not exact peak-heap bounds or measured resource savings.

The Go client renders Markdown and fenced code/diffs through Glamour. Its keyed row cache is bounded by the displayed page and includes role, width and resolved theme identity; asynchronous results must match the current attachment/theme/width. Rendered page output has a separate 2 MiB cap. `Earlier messages` means older rows are omitted, `Message shortened` marks an actually clipped item, and `Display limit` marks the rendered-page cap. Full T3 remains available for complete content beyond inspection limits, embedded terminals/browser previews, attachment uploads and full-history search.

Wire kinds remain unchanged. An `assistant_message` is presented as Agent. Subagent or Agent · fork is used only when authoritative lineage belongs to that row's source thread and the selected snapshot; borrowed rows do not inherit the containing thread's lineage. Unknown lineage does not establish a subagent relationship. Role headings are quieter than operational status.

Appearance remains client-owned in `terminal.json`. The saved global terminal/violet/cyan/lime theme supplies the base; per-profile overrides are persisted separately and Environment default removes only that override. For a fixed-color base, defaults derive up to eight distinct profile accents from the sorted complete configured profile-ID set, independent of browsing order. An explicit terminal base keeps terminal colors, and `--theme` is a nonpersistent override until an explicit successful theme change. Failed persistence restores the prior setting. Resolved environment theme keys include profile identity to prevent stale cross-environment render reuse.

Three highlight slots retain their UI roles across themes: `accent` for context/selection, `secondary` for supporting transcript content, links and inline code, and `operation` for authoritative status and streaming. Each theme supplies distinct values for those slots; derived profile defaults retain their context sequence and resolve slot collisions through an unused base accent. Warning/error roles remain separate, and textual location/state stays visible regardless of colour. Terminal inherits default foreground/background, uses ANSI slots 4/6/5 and reversed selection; syntax tokens retain their own palette. See [theme controls](../NATIVE-GO.md#themes). The legacy Swift client displays Markdown as plain text and follows system appearance.

## Launch review and model authority

New uses the additive native-workflows extension documented in [native workflows](native-workflows.md). The client chooses a creation environment independently, enumerates existing projects, and asks the adapter for target-native defaults. Same-project New passes only project identity. The adapter resolves verified native settings and project workspace rules; it neither derives launch settings from the active thread nor changes the selected provider/account as a failure fallback.

One review owns exact title/message, staged model/options, explicit project/environment/workspace and native defaults revision. Empty first messages create real empty native threads; nonempty messages use native initialMessage with human provenance. Title Enter focuses Create. Only explicit Create journals a newly generated operation ID and thread ID and calls the native launch handler. AgentBox asks native T3 to create isolated worktrees unless shared root is explicitly chosen. No provider CLI or client worktree creation is involved.

Restored launch forms retain exact edits and source-bound draft slots. Changed native defaults require review before dispatch. Initial Create revalidates target project/default revision/catalog/workspace. Lost responses preserve the original operation/payload/thread/message identity. Explicit `/retry-launch` reads authoritative identity first; absent or mismatched evidence remains uncertain without redispatch. Acknowledgement retires only its own draft/journal. Existing legacy launch records keep their original scope for recovery; a new launch never inherits those bindings.

The protocol stays companion v1 / orchestration v2 with optional `features.nativeWorkflows`; unsupported installed methods fail explicitly. The native server remains the sole permission, worktree, execution and receipt authority. No source/catalog display is proof that a particular installed nightly supports the extension.

## Status and causal receipts

`Status` exposes separate connection/synchronization, run state and active-run identity, request count, delivery mode, model summary, pending-operation/Stop flags, draft-save failure and historical state. The legacy combined `State` string remains for existing consumers; the refined UI uses the separate fields for authority. A local command notice cannot replace run/request truth. Still-offered runtime requests remain visible until authoritative removal or revision replacement; ordinary send acknowledgement does not resolve them.

Send `Receipt` is attachment-local presentation evidence carrying operation, message and thread identities, acknowledgement/observed sequences, run ID and phase. `beginReceiptLocked` records baseline rows. `observeReceiptLocked` requires a live matching thread and the native run's initiating `userMessageId`, exposed as `runtimeDetails.runMessageId`, equal to that send's generated message ID before correlating a run. Responding additionally requires a new or changed `assistant_message` row whose own `runId` matches that correlated run. Both identities are required; a newer sequence, latest user row or old-run text alone cannot associate a queued send with a response.

While the runtime belongs to an uncorrelated or older send, the observer refreshes its baseline to the current rows. Those old-run text changes cannot later become evidence for the queued send when its run starts. The correlated run establishes completed, failed or interrupted. Evidence observed before the network acknowledgement is retained so a late ACK cannot revive the acknowledged phase, including on explicit same-ID retry.

Acknowledged means receipt, not response or completion. Without native initiating-message evidence, presentation remains conservative. A verified current connection/synchronization replaces reconnect presentation; stale attachments or local command success cannot establish health. Durable uncertainty, pending Stop, draft-save failure and current errors retain their recovery owners. Retiring transient feedback never deletes journals or retries work.

## Compatibility and actions

Client protocol v1 contains small user-facing fields, never full T3 projections. Each host validates its configured environment identity and V2 protocol. Server-side delivery selection must be advertised before sends are enabled. Additive fields are tolerated, unsupported requests are surfaced, and missing bounded routes never fall back to full history.

The historical source anchor is **not** an installed-version claim or an all-future-nightlies compatibility guarantee. These sections describe implementation contracts; source checks, compatibility probes, native behavior and installed-instance results are separate evidence in [verification](verification.md). A breaking T3 protocol change is fixed in the host adapter. Client updates are needed when they consume new adapter capabilities or change local behavior. No updater is installed, no upstream code is downloaded during connection, and the main T3 server is never pinned/downgraded by this client.

Every write has a persistent client operation ID. Reconnect does not automatically resend writes. The adapter checks live controls before the first dispatch, then durably saves the exact command and a fingerprint of the client request in a private host journal. Explicit ordinary dispatch retries use that command even after a helper restart; reusing an ID with different content is rejected. Uncertain native launches first reconcile the original authoritative thread/message/workspace identity and are never blindly redispatched. Records are bound to the authenticated subject, environment and local user, so credential renewal for the same subject preserves recovery.

Recovery relies on T3's dispatch path consulting its durable command receipt before current control state. That lets a retry recover an already-applied Stop or answer without applying it to a later run or request. The adapter also saves an acknowledgement before it reaches the client. No success is inferred from a stopped run or resolved question alone. The journal has 4,096 slots and never silently evicts or reuses an operation ID; full or damaged storage blocks new writes while preserving existing recovery records. It contains operation payloads, not credentials, and must be preserved with host state. The local resolved-action control remains available for explicit resolution; it does not cancel or replay server work.

Stop means **the reviewed active turn with queue hold**, not a claim of stopping every background workflow/watch after a turn ends. Its independent lane remains available while another action awaits acknowledgement. Approval choices are preserved; unknown/truncated/nonresumable requests are not actionable. Credentials, private host configuration and transcripts stay outside Git; the client does not add a transcript database. Full T3 remains the owner of broader operations.

## Bounded browser and form refinements

The companion-v1 list operation accepts additive `lifecycle` (`active`, `all`, `completed`, `failed`), `cursor` and `limit` fields. Results retain `observedAt`, lifecycle and an optional `nextCursor`; the Go browser and opt-in CLI envelope carry that completeness metadata. Normal browsing remains small and unsettled-only. Deliberate search/expansion can page through finished children; no transcript is attached by browsing a row. These are adapter projections over the existing shell endpoint, not a new server query or subscription API.

Runless native children derive displayed activity only from the selected provider thread and its correlated provider turns. That display never fabricates an app run or grants the run-targeted Stop operation. Native request revisions and the existing two durable mutation lanes remain authoritative.

Rendering retains a bounded keyed row cache. Completed frames may advance the same page while a newer frame is pending, but attachment/page generation, width and theme reject obsolete results. Reading anchors use a row key and within-row line offset across reflow; explicit Earlier/Latest remains a deliberate page replacement.

Model settings and multi-question forms stage locally. Apply/Submit are the sole mutation points; returning, filtering and Back are local. Complete launch drafts and browser preferences use the existing locked Go state owner. Restored reviewed target, native-default revision and workspace bindings are checked before explicit Create.


## Terminal workspace and bounded inspection

The architecture choice and exact t3term adaptations are recorded in [reuse and redesign](reuse-t3term.md). `internal/client/navigator.go` delegates metadata work to `browserMetadata`, source actions to `withBrowserSource`, and explicit opening to `Target`. `internal/ui/unified_navigator.go` retains one bounded metadata page per configured environment, with at most four concurrent helpers. Completed healthy pages become usable while other hosts are loading. AgentBox uses the same metadata authority with at most three concurrent helpers. Neither mode preloads detailed transcripts, creates a permanent metadata helper or stores a transcript cache.

`adapter/activity.mjs` adds up to 24 activity summaries / 24 KiB alongside readable rows, so tools cannot evict messages. `activity` is a read-only bounded/history HTTP operation. `inspect-item` uses `orchestration.getTurnItem` for one exact source-thread/item; no second subscription is needed. Unknown methods and missing items return explicit failures. Requested revisions are cache keys, not conditional reads; changed results are not cached under old revisions. The client cache holds at most eight details / 256 KiB. Each input/result field is capped at 24 KiB; the complete encoded detail is capped at 72 KiB. No mutation journal is created by either read.

Activity is additive companion v1 metadata. Existing readable rows, cursors, request revisions, command receipts and Stop semantics are unchanged. Source-qualified identities are retained for inherited tool rows. A provider activity row never authorizes a Stop without an app run. Legacy clients ignore the additive fields; an older adapter offers no activity capability and the new client continues its existing conversation flows.

Visual updates coalesce within 80 ms. New structural content starts a bounded read after 120 ms, with at least 400 ms between structural-read starts and one pending follow-up. These are scheduling limits excluding network and rendering time. No idle polling is added. The permanent draft timer is replaced by `draft_autosave.go`: an edit starts one 350 ms save window, continuous typing cannot postpone it, and idle sessions have no draft timer. Local saves also work while disconnected; exact acknowledgment/persistence locking and final flush remain.

## Integrated mode state and reuse

The default new preference is Developer. AgentBox mode is an optional field in the existing terminal settings; task/project priorities use a separate bounded versioned CAS sidecar. No legacy Swift state file is modified. See [state/recovery](state-recovery.md). New AgentBox behavior and tests are independently authored; its GPL source/assets are not copied. Existing T3 and t3term attribution stays in NOTICE/LICENSES. The absence of a top-level t3oxide license remains explicit.
