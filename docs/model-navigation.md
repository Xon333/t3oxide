# Model controls and thread navigation

## Delivered scope

Model/effort status, model and reasoning pickers, advertised service tier or fast mode, environment-scoped presets, a one-dispatch composer override, runtime evidence, and lineage-based thread navigation are implemented. Model controls default to **the current thread**. Provider switching, provider installation and global provider configuration are not part of these controls.

All choices come from the selected environment's existing `server.getConfig` catalog. No universal effort list, fixed model list, new provider session or direct Codex command is introduced. An old adapter still supports its original operations; new controls fail explicitly instead of pretending to work.

## Selection and evidence

| Surface | Meaning |
|---|---|
| `Thread:` header | Authoritative saved `thread.modelSelection`, including explicit effort/tier options. Missing options display as default/unreported, not as measured runtime values. |
| `/model`, `/effort`, `/speed` | Apply to the current thread through `thread.model-selection.set`. The UI waits for the authoritative view; it does not optimistically rewrite conversation metadata. |
| `/presets` | Saved model + options for this environment. Revalidated against the current provider catalog before use. The model picker marks favorite model entries. |
| `/model next`, `/effort next`, `/speed next` | Stage a selection with this environment/thread's composer. Nothing is sent until a message is submitted. |
| `/override` | Inspect/edit or explicitly clear the staged selection. `NEXT SEND` stays visible while it is staged. |
| `/runtime` | Separately shows saved selection, run-requested selection, provider-reported native metadata and the session's reported model. Missing telemetry stays unreported. |

**A one-dispatch override is not a temporary server setting.** T3's existing `message.dispatch.modelSelection` may also update the thread preference when the message starts or steers a run. The local override clears after its matching acknowledgment, but this client does not send a hidden restoration command. Consequently later messages may inherit the model T3 adopted. This preserves the real upstream behavior and avoids racing another client or an uncertain write. A true automatically-restored override would need additional authoritative server semantics; it is not claimed here.

Runtime details report their scope (`active-run`, `last-run`, `provider-thread`, or `unreported`) and provider timestamp where supplied. A new starting run without a provider-thread binding never inherits the previous run's telemetry. Historical pages retain historical metadata and are labeled accordingly. Provider telemetry is evidence reported by T3, not an independent measurement of model internals.

Supported option controls are driven by descriptors: `reasoningEffort`/`effort`, `serviceTier`, and `fastMode` where actually offered. Other unchanged provider-specific options survive edits on the same model. Switching models retains only compatible, advertised options. Providers that require a new conversation for model changes keep that restriction; compatible same-model effort and speed edits remain available. Unknown or unavailable choices are not silently substituted.

## Additive companion-v1 extension

The NDJSON envelope remains version 1. Original operations, field names, bounds, history semantics, OpenSSH authentication and host-key checking are unchanged. Swift ignores the additive fields and remains intact.

`hello.result.features` advertises `modelCatalog`, `modelSelection`, `messageModelOverride`, `runtimeDetails` and `threadNavigation`. Write controls also require the existing `writesEnabled` check and upstream server-resolved command context.

New read:

```json
{"v":1,"id":"read-1","op":"models","environmentId":"ENV","threadId":"THREAD"}
```

It reads a fresh bounded thread snapshot, calls existing `server.getConfig`, and returns only the current provider's sanitized model/option descriptors plus the saved selection. Tokens, settings, accounts and unrelated config fields are not forwarded. Catalogs are bounded to 200 entries and a 192 KiB entry budget; omissions are marked `more`.

New durable mutation:

```json
{"v":1,"id":"PERSISTENT-OP-ID","op":"select-model","environmentId":"ENV","threadId":"THREAD","expectedModelSelection":{"instanceId":"PROVIDER","model":"CURRENT"},"modelSelection":{"instanceId":"PROVIDER","model":"CHOSEN","options":[{"id":"reasoningEffort","value":"ADVERTISED-VALUE"}]}}
```

The adapter checks the reviewed selection and current catalog before first dispatch. This stale-selection check is a preflight check, not an invented server-side compare-and-swap. The command is then prepared in the existing journal and sent as `thread.model-selection.set` through `orchestration.dispatchCommand`. A race after preflight remains subject to T3's normal command ordering.

`send` optionally includes canonical `modelSelection` and a local `modelOverrideId`. Only the selection reaches `message.dispatch`; the local ID correlates acknowledgment with the right staged override. Ordinary sends remain byte-for-byte the original command shape. Queue/steer delivery remains server resolved.

New mutations use the existing persistent operation ID, fingerprint, original payload and durable host receipt. An explicit retry bypasses new catalog/control reads and reuses its journaled command. Reconnect never replays writes. Stop retains its independent lane while a model change is awaiting acknowledgment. A later draft or replacement override is never erased by an older acknowledgment. A staged override is separate from a pending write; clearing the former does not delete the latter.

## Lineage navigation

Type is derived solely from `lineage.relationshipToParent` and `parentThreadId`:

| T3 lineage | Display |
|---|---|
| Relationship null, parent null | Main |
| Relationship `subagent` | Subagent, including an orphan whose parent is unavailable |
| Relationship `fork` | Fork, separate from subagents |
| Missing, malformed or unknown relationship | Unknown (`?`), not guessed from a title or ID |

All view shows main roots first, with subagents nested under their actual parents and collapsed initially; forks stay separate roots. Space/Right expands, Left collapses. F6 selects All/Main/Subagents/Forks. Alt+Up opens the selected thread's exact parent, even if that parent is outside the current result window. Parent cycles are rejected or rendered as bounded orphan context, never followed indefinitely.

`list` has optional additive `grouped`, `typeFilter` and `expanded` fields. Classification, filtering and grouping happen before the 200-row limit. The limit reserves space for main/root rows before children so a large subagent family cannot crowd out every main conversation. `more` indicates additional results; use host search or the Subagents filter to reach children outside that window. Expanded/filter choices are remembered per environment during the process lifetime.

Ctrl+F inside the chooser searches on the host, including collapsed descendants; matching children reveal available parents as context. `/` remains the Bubbles filter for the currently loaded chooser rows. Status comes from T3's `activityRunStatus` or shell `status`: running, waiting, completed and failed are shown where reported. Idle is never relabeled completed, and an absent status remains unknown. Reopen/refresh the chooser for a fresh shell list; there is no new periodic polling service.

The selected conversation header and `/runtime` also retain its lineage. Snapshot/delta metadata replacement, revision checks, Earlier/Latest behavior and session-generation rejection are unchanged.

## Source authority

Contract pin: `pingdotgg/t3code` at `0e7346ccd9427c06161e222319e718e6bd70887f`.

- [Model selection](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/modelSelection.ts) and [option descriptors](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/model.ts).
- [Server catalog](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/server.ts) and [RPC names](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/rpc.ts).
- [Thread/run/provider metadata and lineage](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/orchestrationV2.ts).
- [Client command construction](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/client-runtime/src/operations/commands.ts).
- [Server dispatch behavior, including adopting an override](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/server/src/orchestration-v2/Orchestrator.ts#L5215-L5246).

See the delivery `VERIFICATION.md` for executed checks and the pinned Go/UI build blocker. Existing installed-instance records are historical evidence, not acceptance of this upgrade on a live host.
