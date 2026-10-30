# Go terminal ownership

The CLI entry point is `cmd/t3oxide`. The runtime path is:

**Go CLI/TUI → OpenSSH or local stdio → Node NDJSON adapter → existing T3.**

[Terminal redesign](terminal-redesign.md) records the base layout and [UX reuse](ux-reuse.md) records the current keyboard, composer and recovery choices. F3 opens commands; F6 chooses a conversation. Focused textareas keep their native editing keys. F4 opens an exact external editor, F5 recalls prompts, and Ctrl+X explicitly clears into recall. Ctrl+C keeps a nonempty draft. Ctrl+S requests Stop + hold queue independently of ordinary actions; Ctrl+Q detaches locally. See [controls](../NATIVE-GO.md#controls) for the complete current contract.

## Source boundary

| Package | Responsibility |
|---|---|
| `internal/ui` | Bubble Tea event loop; Bubbles textarea, viewport and lists; Lip Gloss layout/themes; Glamour Markdown; command palette, exact dialog surfaces and terminal lifecycle |
| `internal/client` | Selected T3 session, reconnect, bounded view events, drafts, navigation, exact requests, delivery and explicit recovery operations |
| `internal/wire` | Existing NDJSON framing, identity/revision validation, bounded keyed views, independent Stop admission and owned subprocess cleanup |
| `internal/config` | Existing environment format, strict SSH options, bounded durable drafts, prompt recall, operation records and single-owner lock |
| `adapter/` | T3 protocol integration, bounded model/lineage extensions, authentication and host receipts |

There is no core-tui dependency or local interface fork. Text entry, scrolling and list navigation use the upstream Bubbles components. No local agent runner, model runtime, T3 HTTP client, transcript database, prompt queue, polling service or embedded browser is introduced.

The Go transport and recovery logic reuse tested work from the earlier core-tui alternative. The interface and its application-specific session boundary are independent of that framework. Both alternatives start from T3oxide `91ef2df0c51cf60fc9bacaa85970a173a6f76a0f`; `mac/`, the core command mapper, wire envelopes and journal implementation remain unchanged. The adapter adds bounded model/navigation modules; the installer includes them. Original Node/Swift/Python tests are preserved and the Go loopback fixture is extended.

## Authority and lifetime

T3 remains the source of conversation state. A session emits complete bounded snapshots after strict keyed delta/revision validation. The UI does not append model tokens or retain unbounded history. Earlier replaces the page and pauses live detail; Latest reattaches the live page. Old session, render and action results are rejected after switching.

Sends preserve literal human text and `auto`, `queue` or `steer` intent. Before any mutation, the client saves a stable operation ID and original payload. Uncertain writes remain available for explicit same-ID retry; reconnect only restores reads. Stop uses its own admission and recovery lane and the reviewed run ID. A historical page cannot silently retarget Stop at newer work.

Request dialogs preserve T3 request IDs, revisions and offered values. Dismissal is local; supersession, switching and shutdown cancel the exact pending surface. An approval is never answered through a generic permission policy.

Drafts remain separate by environment/thread. Acknowledgment clears only matching submitted text, preserving later editing and attachment switches. Shared persistence coordination prevents retiring attachments from overwriting newer drafts or restoring text after acknowledgment. The durable state contains drafts, bounded prompt recall, staged selection overrides, environment-scoped presets and recovery records, not remote conversation history. Matching acknowledgment clears only the submitted override ID. The new-conversation acknowledgment is correlated with its source thread while clearing only its separate launch draft.

## Rendering and unsupported operations

Network/action work runs outside Update/View. Markdown rendering uses keyed caches and bounded work, with stale width/theme/session results rejected. The default layout is one centered conversation column with role markers, a growing composer and an optional context sidebar on wide terminals. Lip Gloss layers compose overlays; Bubbles supplies contextual help, fuzzy lists and scrollable full details. Bubble Tea owns terminal foreground/background setup and reset. Fixed dark themes are violet, cyan and lime. The optional Terminal theme leaves default colors to the terminal, uses basic ANSI palette accents and inverse selections, and maps code/diff syntax through Chroma's native `terminal16` formatter without a fixed text color or background. It needs no color probes, Ghostty configuration parsing or theme watcher. Glamour retains finite-width table layout in every theme.

The adapter provides bounded messages, notices, pending requests and attachment counts. Streaming assistant bodies use sanitized plain text; authoritative completion renders rich Markdown through Glamour. Model selection/effort/tier pass through the additive adapter extension described in [model-navigation.md](model-navigation.md). Bounded tool/result inspection is exposed through additive activity reads; results beyond those limits, attachment uploads and provider switching/configuration remain in T3. See [0.7 reuse and ownership](reuse-t3term.md). Transcript Find covers the displayed page. Conversation typing filters loaded metadata locally; **Search server conversations** explicitly invokes bounded native search and labels incomplete sources.

The conversation navigator freezes visible ordering while focused and keeps status updates in place. Pinned precedes the native Working shelf when canonical ordering is applied; Refresh or leaving the list applies pending order and retains the selected identity. Outside the filter editor, **p** reviews a native revision-bound pin/unpin and **Space** requests one bounded read-only preview. `/` enters the filter editor, where p and spaces remain ordinary text. Preview never changes attachment, drafts, Stop target or native seen state.

**New** shows its independent creation destination. Commands → Creation destination pins, changes or resets only environment/project. Every launch reads fresh native defaults. **Create thread** is used only for the exact empty message; any nonempty message, including spaces or newlines, uses **Create & send**. Persistent defaults are scoped and source-labeled; safe writes remain unavailable when the verified native API lacks CAS and durable operation IDs. See [native refinement contracts](native-refinement-contracts.md).

Pending formal requests show an immediate cue. The attached request presents after 1.5 seconds of editor idle or blur, deferring during paste or another modal. Requests opens it explicitly sooner. Escape closes presentation without answering or dismissing the native request, suppressing only that exact ID/revision for this attachment. A new revision is new attention. Automatic presentation starts with unarmed controls; queued input cannot submit an answer.

Dependency versions, upstream commits and release links are in `dependencies.json`. No latency, RAM, battery or footprint advantage is asserted from the language or framework choice.

## Verification

Use `scripts/check-go.sh` for formatting, the complete suite, vet and race detection with the production dependency graph. The integration suite runs the actual Node adapter, Bubble Tea programs and assembled executable in a real PTY against disposable loopback T3 fixtures. On Darwin the PTY comparison excludes only the kernel's transient `PENDIN` flag; every persistent mode and control character remains checked.

The native Mac build also verifies the retained Swift implementation. [Verification](verification.md) records the exact toolchains, outcomes and installed-instance boundary. Go 1.26.0 and all recorded Charm versions remain pinned in `go.mod`/`go.sum`.
