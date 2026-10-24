# 0.7: terminal workspace and t3term reuse

## Decision

**Keep Go/Bubble Tea and the narrow, on-demand host helper. Adapt t3term's workspace layout and lazy tool inspection into t3oxide's existing behavior owners.** The result remains t3oxide; the existing themes, environment accents, routes and local state remain authoritative. This is an implementation decision, not a claim that Go is intrinsically faster than Rust.

Source baseline: t3oxide [`ed43546c175e6d33467aaa89dae2503bb969b99f`](https://github.com/Xon333/t3oxide/tree/ed43546c175e6d33467aaa89dae2503bb969b99f), version 0.6.0; t3term [`d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e`](https://github.com/krishhgg/t3term/tree/d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e). Both public heads still matched these pins when inspected on 24 October 2026. No newer t3oxide or t3term revision was adopted.

The strongest objection is that two application languages and a host-side Node process remain. That cost is real. A direct remote client must replace host authentication, verified environment identity, durable receipts, session renewal and remote cleanup as well as its socket. The supplied three-minute observations saw no running client and establish no active-client cost. The implemented product improvements do not depend on making that high-risk migration first.

### Four practical alternatives

| Approach | Strongest benefit | Concrete cost or missing behavior | Decision |
|---|---|---|---|
| Go/Bubble Tea with native transport | Retains working presentation/state while removing a translation process. t3term's `src/rpc.rs` and `src/client.rs` demonstrate Effect RPC and lazy item reads. | A client-side M4 RPC connection needs an owned SSH forward and host-issued/renewed authentication, plus compatibility with existing host journals. A process removed on the host is not automatically a client-machine saving. | **Fallback:** native Go host bridge behind the same stdio/journal contract, if equivalent-workload measurements justify it. No need to move credentials to M4. |
| t3term Rust as principal base | Stronger integrated sidebar, transcript tool grouping, responsive details and mouse geometry; already working direct local RPC. | Actual `src/tui/environments.rs` inventories saved environments but does not switch to them. There is no equivalent transcript Earlier/Latest cursor workflow or durable existing t3oxide state/journal migration. Ordinary `Client::dispatch` creates command IDs; `LaunchRequest` does retain an immutable in-memory creation intent, but that is not crash-persistent host recovery. | Rejected for this release: too much proven operational behavior must be ported before it is an equivalent product. |
| Staged Rust t3oxide with selective t3term reuse | Reuses Rust protocol, bounded RPC, discovery and transcript code without accepting the whole product. | Still needs remote auth/recovery and migration, while maintaining two UI implementations. A language transition alone does not produce the desired interaction model. | Rejected now; no unique result outweighs that transition cost. |
| Go plus narrow host helper | Keeps exact remote routes, host credentials, compatibility isolation and journals; no new listener or daemon. | Node and host delivery remain. Installed T3 compatibility still needs a host check. | **Primary**, with substantive layout, inspection and idle/scheduling improvements. |

This comparison uses source, not an unfinished README list. t3term is stronger at information layout and broad interactive inspection. t3oxide is stronger for this fleet's existing multi-environment attachment, exact draft/launch persistence, earlier history and durable uncertain operations. t3term's scriptable mutation CLI is broader; this release improves discovery and read-only machine inspection while retaining t3oxide's interactive write boundary.

## Preserve, reuse, replace

| Source area | Treatment | Result and boundary |
|---|---|---|
| `internal/client/{actions,requests,launch,models,receipt}.go`, `internal/config/`, `adapter/journal.mjs` | Preserve existing owners and tests. | One execution authority, exact source/run/request identity, staged settings and retained multi-question answers, complete launch drafts and durable explicit retry. |
| `internal/config/config.go::Environment.Command`, `internal/wire/connection.go` | Preserve OpenSSH/stdio lifecycle; add only read operations to admission. | Existing local and remote profiles work. No remote port or credential appears on M4. |
| t3term `src/tui/mod.rs::{columns,sidebar_width,cycle}` and `src/tui/details.rs::{layout,beside,pane_area}` | Adapt into `internal/ui/{navigator,workspace_layout,workspace_mouse}.go`, using Bubbles/Lip Gloss. | Visible navigation, attached content, context and geometry-bound click targets. Rust widget code is tied to Ratatui and its large App state; that code is not a drop-in Go component. |
| t3term `src/transcript.rs::{tool_argument,output_text,tool_output,describe_as}` | Port narrow description/output rules into `adapter/activity.mjs`. | Commands, dynamic tools, file changes/searches, web searches, subagents and tasks have bounded readable summaries/results. Original MIT notice retained. |
| t3term `src/client.rs::turn_item`, `src/tui/mod.rs::Outputs` | Adapt on-demand result retrieval and revision-aware caching in `internal/client/activity.go`. | One source-qualified item per deliberate inspection. No eager result fetching or new subscription. Requested revision is a cache key; changed results stay explicit. |
| Existing `browserMetadata`, `withBrowserSource`, `Target` | Reuse through `internal/client/navigator.go`. | One bounded metadata page; explicit search/history/children/paging; highlighted rows do not attach. Source Settings/New retains the attached conversation. |
| `adapter/t3.mjs::{emitView,scheduleRefresh,refresh}` | Replace delayed cadence, preserve bounded coalescing. | 80 ms visual window; first structural read after 120 ms, at least 400 ms between read starts; one active plus one follow-up. No idle read. |
| `Session.consume` draft ticker | Replace with `draft_autosave.go`; explicitly flush before source New/Settings. | One edit-triggered 350 ms save window; no draft timer while idle. Saving works during reconnect and does not block the stream-consumer loop. Source reviews save the attached draft before acquiring a temporary connection. |
| `cmd/t3oxide/inspection.go` | Add machine discovery and exact read outcomes. | `capabilities`, `activity`, `inspect`; versioned success/failure envelopes. Existing JSON arrays stay compatible. |
| `mac/`, themes and environment color derivation | Preserve. | Swift rollback remains separate; no palette work or silent state conversion. |
| t3term's full `ThreadState::apply`, local login/account policies and broad workspace/PR/provider surfaces | Keep separate. | Avoid a second full T3 projector and policies that conflict with existing host/account boundaries. Broad administration stays in T3. |

Attribution is in `NOTICE` and `LICENSES/t3term-LICENSE.txt`. Dependencies remain the pinned, unmodified Charm modules; there is no Ratatui runtime, UI-library fork or new Node dependency.

## Interaction contract

The screen has a conversation sidebar, attached transcript/composer, and optional context. Wide terminals show the sidebar; compact terminals use the existing full browser. The main identity always names the attached conversation. A sidebar environment label never changes the target of Stop, send, requests or the main Settings action. Separate selected-row Settings/New actions open the exact source review.

The initial list remains up to 24 unsettled root conversations. Children are absent until explicit expansion/type/search; forks stay separate. A query filters loaded rows immediately; Search all carries that query to the host. Lifecycle, thread type and continuation are explicit. Other rows are observed snapshots; only the attached row is reconciled from the existing stream. No highlight reads a transcript.

Tab/Shift+Tab cycles the visible sidebar, composer, transcript, optional context and activity strip. Ctrl+O focuses/leaves navigation or opens the compact browser. Ctrl+P remains Commands, Ctrl+R Requests, Ctrl+S Stop with queue hold, Ctrl+Q local exit. Escape backs out locally. Existing editor keys and exact input preservation remain. No function/Option/enhanced key is required. Mouse controls use the last rendered geometry and exact identities; a resize or state change invalidates stale hit targets. Mouse-off keeps native selection. Form fields retain their keyboard controllers; clicks cannot activate the attachment behind a form.

The main actions are visible and compact. Settings still stages advertised model/effort/tier together and applies once; quick commands stay available. Approvals retain only offered choices. Questions retain Back/Next/Submit and revision-bound answers. Recovery keeps separate ordinary/Stop lanes, exact original identity, explicit retry and local-record resolution. The redesign does not add a second approval gate.

Activity has its own bounded metadata budget, so a run of tool calls cannot remove readable messages. A visible summary and the activity/context controls expose real work. Opening a result reads one item, with source identity and honest missing/changed/shortened status. Earlier/Latest activity reads do not replace the readable transcript or silently resume its live subscription. Input and result text are displayed safely and never executed.

## Connection and resource design

| State | Client machine | Host | Subscriptions/work |
|---|---|---|---|
| Remote attachment | Go UI + owned SSH client; existing jump routes may add processes | On-demand Node adapter + SSH session workers; existing T3/providers stay separate | One detailed attached thread. |
| Local attachment | Go UI + Node helper on the same machine | Existing T3/providers | Same one detailed thread. |
| Same-environment navigation | Reuses attached transport after verification | Existing helper | Explicit shell read; no extra thread subscription. |
| Other-environment metadata | Temporary SSH child | Temporary helper/SSH session | Descriptor + authenticated shell only; no WebSocket ticket. Reaped on finish/cancel. |
| Explicit Settings/New on another source | One temporary source connection in addition to attachment | One temporary source helper | One extra detailed source while that review needs it. |
| Activity overview | Existing transport, or one CLI-owned temporary helper | Bounded/history HTTP read | No new subscription. |
| Tool result | Existing connection, or one CLI-owned temporary helper | One `getTurnItem` RPC | No result stream/subscription. |

These are lifecycle expectations, not measured fleet process totals. Rust would still need SSH on remote routes. Client-machine cost, host adapter cost and shared server/provider work must be measured separately.

Authentication stays in private host-local files. `readConfig` accepts only loopback T3 URLs and validates ownership/mode/identity. `connect` requests a socket ticket and verifies protocol 2; it does not copy a T3 home, provider credentials or database. Authorization/protocol/identity failures block reconnect. Host credential renewal remains an explicit host task; helper restart or explicit `/reconnect` reloads the renewed credential without discarding same-principal journals. Internal WebSocket reconnect uses the helper's already-loaded token. Ordinary transport reconnect never replays writes.

The Go process owns and reaps its stdio/SSH child. Remote `exec` avoids a lasting shell wrapper; EOF closes the adapter and its socket. Keepalives detect broken routes. No forward listener, always-on proxy, remote daemon or service manager is added.

Known readable budgets remain 24 rows/48 KiB latest and 80 rows/144 KiB Earlier. Activity is at most 24 summaries/24 KiB within the existing 384 KiB envelope; activity must yield space to established messages/requests. Input and result fields are individually capped at 24 KiB; an encoded detail is capped at 72 KiB. The detail cache is at most eight entries/256 KiB, keyed by environment, source thread, item and revision. These are logical/encoded bounds, not measured peak heap.

The 80/120/400 ms timing tests establish scheduling behavior, excluding HTTP and rendering time. Removing the old 350 ms ticker eliminates that recurring draft wakeup; it does not establish a percentage CPU, RAM, energy or battery improvement. The packet's absence observations and t3term author's desktop comparison are not an apples-to-apples performance result.

**Smallest decisive measurement:** use the existing `scripts/measure.py` for the same idle, representative streaming and environment-switch workload on 0.6 and 0.7, with the same terminal dimensions, server and conversation. Capture client and verified host helper simultaneously. Separate cold/warm launch and run three short repetitions. Record launch-to-ready, event-to-presentation p50/p95, snapshot/render counts, CPU-time deltas and process RSS/footprint or PSS. Never sum RSS as unique physical memory. A native-bridge port is justified only if those results identify helper work worth replacing.

## Implemented vertical slices and focused acceptance

| Slice / owner | Dependency | Acceptance and principal risk | Rollback |
|---|---|---|---|
| Bounded activity (`adapter/activity.mjs`, wire/client activity modules) | Existing T3 protocol-2 item/history contracts | Real adapter fixture validates source, requested vs returned revision, missing output, history, clipping, cancellation and no mutation/subscription. Risk: installed host lacks method; explicit unsupported result, existing chat stays usable. | Older helper disables new capabilities; old readable protocol remains. |
| Integrated workspace (`client/navigator.go`, UI navigator/layout/mouse) | Existing source/target/session owners and activity metadata | Layout/focus/selection/resize tests plus executable fixture; draft, reading position and Stop target survive browsing. Risk: geometry under a physical Mac terminal; native check only. | `/navigator off`, context off or compact browser; previous executable retained. |
| Responsiveness and idle work (adapter cadence, client autosave) | Existing coalescing and exact save/ack locks | Deterministic burst/idle/overlap tests and race tests; offline draft persistence and error-recovery transition. Risk: new structural reads more frequent; minimum start interval bounds that work. | Restore prior executable/helper revision, keep journals and state. |
| Machine inspection and distribution (`cmd/t3oxide`, host installer, notices) | Additive read capabilities | Offline discovery, exact JSON failures, real-adapter CLI, installer preservation in a temporary home, Linux and Darwin/ARM64 builds. Risk: unknown old adapter capability; fail explicitly without a write. | Existing commands retain formats; no state schema conversion. |

All four are implemented in this source. Broader provider administration, upload, embedded terminal/browser, unbounded full-history search and a second implementation language are outside this release, not unfinished ordinary implementation assigned to native workers.

## Compatibility and migration

No data migration is needed from 0.6: environment profiles, Go drafts, full launch drafts, preferences, presets, next-send overrides, prompt-history sidecars and ordinary/Stop pending records stay with their existing owners. New appearance fields are optional defaults. Keep the complete existing state and sidecars when adopting or rolling back; do not reset or clean unknown state. Older builds may drop fields they do not understand when saving, so do not alternate writers against the same unreviewed state copy.

Host journals retain their environment/principal identity and exact command payloads. Additive activity reads never enter those journals. Preserve in-flight and uncertain records across executable/helper changes. An unresolved action remains an explicit recovery task using its original ID; installing code is not proof that the remote effect did or did not happen.

The installer includes all eight adapter modules, validates the existing host descriptor/config, stages a content-addressed release, atomically publishes the validated `host.json` and replaces the current release symlink. With no override flags, configuration fields are preserved, although JSON formatting may change. Existing releases and journals remain. An already-running helper uses its loaded revision until it restarts; an internal socket reconnect does not reload modules. This source package does not restart it. The M4 remains client-only.

The Swift rollback and `drafts.json` remain byte-for-byte separate from Go state. No Go draft/recovery transfer into Swift is attempted. The package includes cross-built Darwin/ARM64 output with the Go linker's ad-hoc signature (not natively verified); native signing, existing-route checks, installed-host acceptance and publication are the subsequent native owner's tasks.

## Upstream contract evidence

The existing T3 anchor [`0e7346ccd9427c06161e222319e718e6bd70887f`](https://github.com/pingdotgg/t3code/tree/0e7346ccd9427c06161e222319e718e6bd70887f) already exposes `OrchestrationV2GetTurnItemInput` and HTTP activity history. Its `revision` comment explicitly describes a client cache key, not a conditional fetch. We also inspected current [`aaf4a901c490da4f0943afe1d941f3a8d58dc1c8`](https://github.com/pingdotgg/t3code/blob/aaf4a901c490da4f0943afe1d941f3a8d58dc1c8/packages/contracts/src/orchestrationV2.ts); those inputs/results remain compatible. No whole upstream runtime or projector was adopted. Installed-host support remains a separate native observation.

Executed checks and actual limitations are recorded in [verification](verification.md); physical Mac acceptance is deliberately separate from portable source/fixture verification.
