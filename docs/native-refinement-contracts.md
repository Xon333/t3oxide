# Native adapter refinement contracts

This implementation preserves companion protocol 1 and adapts verified T3 orchestration protocol 2. Native T3 still owns runs, queues, model/provider routing, projects, worktrees, requests and mutation receipts. The adapter does not start a provider, replace native orchestration, share a writable transport between sessions, or read desktop account stores.

## Opening and control publication (R12 / P03 / P08)

A successfully verified descriptor can be consumed once by the immediate first connection attempt in the same helper generation. The receipt expires after five seconds and is bound to exact environment identity, loopback URL, loaded credential, write setting and protocol. Changed binding/generation, clock regression, auth failure, failed connection, disconnect and close invalidate it. The receipt is only verification evidence; ticket acquisition, socket connection, bounded read, subscription and synchronized marker retain their existing ownership.

The first bounded snapshot is published immediately as **synchronizing**. Only the native synchronized marker makes it live. Synchronized and unavailable transitions also bypass text cadence. Existing message updates remain coalesced at 80 ms. Canonical request events promote the structural refresh to immediate admission, with one bounded read in flight and at most one pending follow-up for a burst. Any message updates newer than the returned snapshot are reconciled before installation. Ordinary structural events retain the 120 ms debounce and 400 ms sustained rate ceiling. Ordered companion deltas, output byte/queue bounds and fail-close resynchronization remain unchanged.

The native subscription replays persisted events after the bounded snapshot sequence before the completion marker, then tails events from the high-water mark. The maintained fixture now models this replay explicitly. A held-subscription regression inserts an authoritative event after the first non-live frame and proves the event is applied before live authority; the application does not delay first display to hide an incomplete fixture.

Optional `dependencies.trace` instrumentation receives only monotonic stage timestamps, local generation/epoch and bounded counters. It records descriptor, ticket, socket, bounded read, first/publication and request-refresh boundaries. Optional `run` instrumentation additionally records lane admission/start/end, output queue frames/bytes and Writable callback completion. A Writable callback is explicitly distinct from kernel terminal write completion. It records no route, account, token, configuration, request text or message body. This is test instrumentation, not production telemetry.

## Bounded projection (P04)

Each bounded row is serialized once per flush for exact byte-budget validation and equality. Unchanged rows reuse an immutable prior copy. Changed rows are copied exactly from their validated JSON encoding; metadata is defensively cloned, and ordered row keys/removals still participate in the delta. Only the current bounded page and encodings are retained. Frame size is checked before the existing output writer, and large delta key sets still fall back to a complete bounded snapshot. Public Go snapshot semantics remain defensive.

`tests/adapter-performance.mjs SOURCE_ROOT` runs the identical synthetic workload against either source tree. It measures synchronous `flushView` only: 24 rows at 1,800 characters and 80 historical rows at 1,600 characters, sequence-only and tail replacement, 100 warmup and 2,000 timed operations. Raw values include per-process memory/RSS snapshots; these are not allocated-byte or terminal-paint measurements.

## Additive read capabilities (R04 / R08 / R16 / R20)

- `thread-preview` performs one bounded HTTP read, with an independently canceled generation. It never opens a transcript subscription, marks seen, creates a journal or changes active attachment. Its explicit read-only envelope contains a non-live synchronizing display snapshot with no request offers or active Stop target. An unavailable/unsupported read remains visible as a metadata-only fallback in the client.
- `list` with explicit `serverSearch:true` calls the verified `orchestration.searchThreads` RPC with a trimmed 2–200 character query and limit 50. Returned exact thread/project identities are joined against shell metadata. It performs no transcript fetch per result. Native results have no complete/cursor promise, so every server-search result is labeled incomplete; errors do not silently become local filtering.
- `threadPinning` is advertised only when the native descriptor says `capabilities.threadPinning === true`. Pin/unpin use the existing revision-bound durable `thread-action` path. A missing capability disables the operation before dispatch.
- Working membership follows native shell runtime and pending-request presentation. Awaited background tasks, subagents and background tasks that hold completion are distinguished from unawaited commands/monitors. Failed work and actionable settled plans stay outside Working. Live native-managed child approvals propagate to their local ancestors; provider-native child requests, auth refresh and coordinator-owned child questions do not invent parent attention. Shelf placement uses current shell evidence; the adapter does not copy the native client’s timer tracker or scheduler. Cross-environment delegated attention requires source-qualified metadata available to the caller; absence is not proof of a linked host’s state.
- Machine kind is limited to native `server/cloud/linux/desktop/laptop/mac-mini/mac-studio`; everything else is `unknown`. No hostname or environment letter is treated as a machine-kind identity.
- Provider instance IDs remain exact routing IDs. Account label is only native `auth.label` when reported, and its observation time is native provider `checkedAt`. No email/config secret is projected. Until a deliberately requested catalog/defaults read reports a label, account is unreported. A bounded, connection-local map retains only narrow identity labels for at most 64 explicitly read instances; no additional config or per-row discovery is triggered. Disconnect/auth/config binding invalidation clears it.

## Native defaults write boundary (R16: blocked-native)

The scoped native defaults read is complete: explicit project-on-environment and environment scopes, per-field source/inheritance, exact stored selection/options, disabled-provider visibility, model availability, display snapshot revision and native Settings route. It reports desktop-local preferences as unreported. Existing launch resolution remains fresh on every review; destination pins do not freeze provider/model/options/workspace.

**The pinned native settings write API is not safe for the requested field-reset and recovery contract.** `server.updateSettings` accepts a `ServerSettingsPatch` plus optional provider-instance mutation, but has no expected revision/CAS and no durable operation ID. Its project entry merge replaces known fields and has special preservation behavior for provider instances/unknown keys. Rereading before writing cannot eliminate a concurrent update between the read and dispatch. Fabricating a revision field or a local journal ID would not make the server compare revisions or deduplicate the operation.

Consequently `nativeDefaultsWrite:false` is explicit, there is no defaults mutation opcode, and the editor shows this precise reason: “Native server.updateSettings has no revision/CAS or durable operation ID; concurrent field reset and uncertain-write reconciliation are not safe here. Use native T3 Settings.” The read snapshot hash is a display/cache identity, not server concurrency authority. Current-thread model Apply and this-launch staged choices continue through their existing verified commands.

### Exact native completion task

| Item | Required native action/check | Acceptance criterion |
| --- | --- | --- |
| R16 persistent write/reset | Verify the installed host’s versioned native settings schema and authorization. If still equivalent to the pinned schema, add or obtain a native write primitive with expected revision/CAS, field-level override removal, preservation of opaque siblings/options, and durable same-operation-ID reconciliation. Implement only after that contract is verified. | A stale edit retains staged values without losing a concurrent field. Reset removes only the selected project field so a later environment edit is inherited. A lost result reconciles the exact operation; no automatic retry/new ID/substitute account. |
| R16 reproduction / fallback | Open New → Native defaults, switch project/environment scope, inspect source and unavailable saved identity. Save/reset must show the contract reason and native T3 Settings route. | Scoped reads function; no `server.updateSettings`, write journal or dispatch is created. Existing native settings can be edited through native T3. |
| R08 installed capabilities | On each supported installed T3 version, verify bounded preview, native search and `threadPinning` capability. Compare Working/child attention using native-owned shell states, including linked/delegated cases when available. | Preview has no mutation/seen/attachment effect. Unsupported operations remain explicit. Pin uses the reviewed revision once. Native identities and membership agree with supported source data. |
| R12 production transport | Run a fresh helper against the installed host, then reconnect/auth rotation; capture only stage/counter diagnostics. | One initial descriptor, independent ticket/socket/bounded/subscription, live only after marker, fresh validation on invalidation, no mutation replay. Fixture timings are not claimed as production latency. |
| R20 missing account telemetry | Verify the installed native registry’s `auth.label` and `checkedAt` without changing account/model or reading personal stores. | Exactly reported account label/time shown; otherwise “unreported”. Instance ID alone never claims a real-world account. |

## Version-matched primary sources

All native source links below use commit `a773dc38ac75177b6cd133d27df00ca0a83f9480`. They establish the adapter boundary, not an installed-host acceptance claim.

- [RPC settings payload and authorization schemas](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/contracts/src/rpc.ts)
- [Project override settings schema and required scopes](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/contracts/src/settings.ts)
- [Known/unknown field merge and reset behavior](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/shared/src/serverSettings.ts)
- [Native scoped settings writer](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/apps/web/src/components/settings/useScopedSettings.ts)
- [Descriptor machine kind and thread pinning capability](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/contracts/src/environment.ts)
- [Provider identity and optional auth label](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/contracts/src/server.ts)
- [Stable provider instance identity](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/contracts/src/providerInstance.ts)
- [Bounded search schema](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/contracts/src/threadSearch.ts)
- [Snapshot/stream/launch and native initialMessage text schema](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/contracts/src/orchestrationV2.ts)
- [Native initial message dispatch on presence](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/apps/server/src/orchestration-v2/ThreadLaunchService.ts)
- [Working membership rules](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/client-runtime/src/state/threadInbox.ts)
- [Shell runtime and pending-input presentation](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/client-runtime/src/state/models.ts)
- [Local and delegated child-attention derivation](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/client-runtime/src/state/threadShell.ts)
- [Background completion holding semantics](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/packages/shared/src/orchestrationV2PendingBackgroundWork.ts)
- [Native shelf and pin user documentation](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/docs/user/thread-sidebar.md)

Firecrawl developer search was used to discover consequential settings-merge concerns; the implementation decision was verified against the pinned primary schemas and merge code above. No further search service was called merely to duplicate that research.

- [Native replay/high-water/completion-marker implementation](https://github.com/pingdotgg/t3code/blob/a773dc38ac75177b6cd133d27df00ca0a83f9480/apps/server/src/ws.ts)
