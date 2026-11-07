# Rust workspace architecture

The application is one event-driven Rust client. T3 remains the authority for native threads, agents, execution, permissions, queues, worktrees, requests and settings. t3oxide stores only presentation preferences, human drafts and exact client-side recovery records. Its Node companion remains a compatibility and durable receipt boundary on the host; it is not another agent service.

The reference baseline is t3oxide `e73caacd673d1a9bbceea8a7ed240b2a068ba5a7`. The current authoritative T3 source review is `ac8dce0cc3abd8a337f2d806b5082a6834b9b81a`; [contract evidence](rust-contracts.md) pins the exact upstream files. [Source decisions](rust-source-reuse.md) records every supplied source and its licensing boundary.

## Modules and ownership

| Module | Responsibility |
| --- | --- |
| `src/main.rs`, `src/cli.rs` | Entry point, validated arguments and read-only headless commands. |
| `src/config.rs` | Existing environment profiles, exact identity policy, local process/OpenSSH construction. |
| `src/wire.rs` | Bounded companion-v1 NDJSON transport, negotiated handshake, request identity, process lifetime, independent Stop admission and cancellation. |
| `src/projection.rs` | Atomic bounded snapshots and revision-checked deltas; native source IDs remain intact. |
| `src/domain.rs` | Source-qualified metadata tree, native attention/review projections and discoverable action catalog. |
| `src/state.rs` | Locked, validated, atomic client state; migration, drafts, exact operation journals, prompt history and acknowledgements. |
| `src/app.rs` | Event loop, asynchronous reads, connection generations, draft saves, navigation and mutation lifecycle. |
| `src/app/slt.rs`, `src/slt_ui.rs` | Active SLT runtime, terminal presentation, exclusive input routing and controller intents. |
| `src/app/input.rs` | Retained legacy input regressions, compiled for tests only. |
| `src/attention.rs` | Actual native question admission, stable controls, revision-bound drafts and receipt validation. |
| `src/app/actions.rs`, `src/app/native.rs` | Action dispatch, model/request/launch forms, native capability checks and reviewed operations. |
| `src/app/workspace.rs` | Local project/priority views, reading coordinates, loaded-content navigation and source copying. |
| `src/ui.rs` | Shared presentation records and retained Ratatui editor/renderer compatibility helpers. |
| `adapter/` | Existing verified native integration, bounded source projection and durable host receipts. |

Developer and AgentBox are views over these same models. They share the active connection, draft, projection, requests, recovery and creation forms. Mode changes alter visual priority and default lanes; layouts alter pane space and focus. Neither creates a native session or a separate execution owner.

## Component decisions

SuperLightTUI 0.26.0 owns the active terminal runtime, layout, focus, input and
output. Its textarea state owns active editing, and its Markdown widgets render
ordinary content. Ratatui 0.30.2, `ratatui-textarea` 0.9.3 and `tui-markdown`
0.3.13 remain linked for shared editor conversion and retained compatibility
helpers; their previous terminal loop is inactive. Crossterm supplies terminal
events. `nucleo-matcher` 0.3.1 supplies fuzzy action scoring without its threaded
application index. [Source decisions](rust-source-reuse.md) distinguishes live
callers from retained paths.

Ordinary threads and bounded Crossbeam channels carry process/stdio work.
Separate pools handle two foreground reads, one metadata read per configured
source (at most eight) and one storage task at a time, with bounded queues of
16, 16 and 4. Each metadata source has an available slot even when other hosts
hang, and unavailable sources cannot occupy the workers needed to attach a
conversation or save a draft. A current-thread Tokio runtime runs SLT's
`run_async_with`; the worker bridge delivers typed background events through
its wake-aware sender. There is one terminal owner. Herdr and TUIOS supply
interaction references; their daemon and PTY ownership remain outside this
client's scope. Restricted FrankenTUI code is not incorporated.

Cargo.lock pins the complete dependency graph. `docs/dependencies.json` records source archives/checksums and `LICENSES/rust-third-party.txt` retains license texts. The MPL-covered fuzzy matcher is an unmodified library with its corresponding source link supplied; no covered source file is copied into t3oxide's own modules.

## SLT surfaces and flow map

The active runtime is pinned `subinium/SuperLightTUI@d4537d74f4b42188e06cb224fbfe7684397897fb`, v0.26.0 (MIT). `src/app/slt.rs` owns the SLT session handle and joins it to observe I/O/panic results. Its bridge forwards bounded worker messages through the wake-aware sender; it never reads terminal input. `idle_redraw(false)` uses async-message wakes and explicit draft deadlines. The retained Ratatui model/editor helpers and renderer tests are compatibility/recovery scaffolding; they do not run a second terminal loop.

| Surface / flow | State owner | Update / invalidation | Rendering and input |
| --- | --- | --- | --- |
| Navigator A1–A3, S1–S3 | Native shell + durable source/event read markers | Host generations; meaningful reply/request/failure/run activity | SLT list; unread first in A; stable keys; prior-frame reorder guard |
| Conversation A4, S4 | Native source-qualified projection; one tab per conversation | Exact snapshot/delta revision; reconnect resets projection base | SLT Markdown/scroll region; bounded adapter excerpt; source IDs retained |
| Prompt A4–A5 | Conversation draft + SLT TextareaState | Human edits, 200ms durable save, source switch/exit flush | SLT focus; public indexed key consumption before textarea for command/send; plain Enter newline |
| Models A4 | Native catalog and exact thread/provider selection | Native catalog revision and acknowledgement | Existing native review controls; defaults require a separate explicit action |
| Human queue B1–B3, S5–S6 | Actual native request summary, full offer and revision | Native pending-request changes; failure/prose alone never creates a question | Project-grouped SLT region; mouse/Tab; source opens in A |
| Form B3 | Exact environment/thread/request/revision draft | Every field edit/choice; current-offer validation; explicit Submit | Stable field/choice values; local completion never delivery |
| Supporting content B3 | Existing native source items | Source/content revision and excerpt flags, geometry and renderer process inputs | Separate SLT Markdown + TermiFlow flowcharts; unsupported/overflow preserves original source |
| Project graph B1 | Native inventory, stable project/question identities | Inventory/revision/geometry, previous displayed hit owners | ascii-dag scene adapted to clipped SLT cells, Tab/arrows/Enter/mouse; zero-question projects retained |
| Context B3 | Owned local fael store | Immutable source-qualified events and verified full-source blobs | Bounded observation of already-loaded evidence; explicit typed capture, full-source retrieval, history/revisit/supersession; local close has no native effect |
| Dependency evidence B3 | Explicit admitted native question revisions and edge reasons | Qualified edges are required before synthesis | petgraph analysis is available but current native offers expose no edge/coverage source; the live queue preserves admission order |
| Recovery S6 | Client pending records + host durable receipt journal | Explicit original-ID reconciliation only | Matched acknowledgement; independent Stop; no automatic retries |

Read state, native run state and settlement are separate. Source-qualified observed events survive lifecycle-only shells and restart; evicting an observation also expires its read markers conservatively. Open verifies a live source before recording its event as read, and never settles it. Running includes running/needs-input/failed even after reading. Unread and Completed share view 1; Pins is independent and Settled is an on-demand explorer. Host pagination and local ordering use meaningful activity, excluding shell `updatedAt`/token/tool churn. The compact shell cannot expose unseen mid-run replies it omits; it cannot be treated as a full transcript. Actual subagent counts come from `knownChildCount`, never the loaded-page length. Project inventory is bounded to 200 entries / 48 KiB of record content, with explicit incompleteness; older helpers that omit inventory do not establish completeness.

The focused editor's command layer uses public SLT `key_presses_when` and `consume_event(index)` after its named focus registration and before textarea. Unlike shortcut queries, that indexed API can consume owned Enter exclusively. Tests settle focus before checking plain Enter, every accepted modified Enter, command completion and literal paste. Rex can intercept keys before SLT; native key delivery remains a separate acceptance boundary.

Internal tabs retain exact conversation identity and independent autosaved drafts. Closing a tab never deletes draft or operation recovery. `sltUi` replaces old presentation defaults while preserving displaced values. Native pop-out is unimplemented until a draft-safe supported handoff is established; internal tabs remain the supported setting.

## Source identity and independent hosts

A native thread ID is meaningful only within its environment. Profiles must have unique client IDs and native environment IDs. Navigator keys qualify a native thread by profile; item display keys also contain the attached thread and connection generation. Drawn mouse targets carry those keys instead of relying on a current row index. Native source thread/item IDs are retained separately and are the IDs sent to the companion.

Each metadata result carries a host generation. Each active transcript/read result carries the connection generation; modal reads also have an action serial. Replies from obsolete scopes are ignored. Dismissing a modal cancels its negotiated read or closes its temporary helper. Switching source closes the previous owned subscription and saves its draft/reading position. Preview uses an independent read and cannot acquire mutation authority.

Metadata reads run independently across configured hosts. A host failure leaves its error visible and does not replace another source's identity or status. Root/child relationships use authoritative lineage. Collapsed loaded children remain searchable; the additive `inclusiveThreadList` adapter capability exposes summaries before pagination without fetching transcript bodies. Pagination and search incompleteness are explicit. More remains bound to the original native page query even after local filtering. Loaded metadata uses a rolling 1,600-row bound per environment; Refresh returns to the newest page. Live run/request lifecycle changes refresh only their source and merge into the loaded window. Text chunks do not cause metadata reads, and lifecycle refreshes do not overwrite an explicit server-search page.

## Verified transport

The Rust process either starts the configured Node executable directly or invokes `/usr/bin/ssh` with a validated destination, strict host-key checking, batch authentication and a bounded connection timeout. Local process invocation does not use a shell. Remote executable/configuration paths are quoted for the single remote `exec` command. Tokens and native loopback authentication remain host-local.

A connection owns only its adapter child and pipes. Protocol version, environment identity and verified handshake state must match before writes are admitted. NDJSON lines, input payloads, queued events and pending requests are bounded. Nonblocking pipe writes have a deadline, so an unresponsive helper cannot block the UI indefinitely. Reading never races a partially consumed line against a timer. Final acknowledgements are drained even if the helper exits immediately afterward.

Transport failures distinguish known rejection from uncertain acceptance. Reconnect establishes a new projection base. It does not replay writes. Read cancellation is negotiated and carries the original request identity; cancellation never means a mutation failed remotely.

The retained companion handles native descriptor/authentication, WebSocket tickets, Effect RPC acknowledgements, compact snapshot restoration, request revisions, project defaults and receipt lookup. T3 does not require Node as its client language. This boundary is retained because replacing the verified native codec, journals and remote installation contract would add another integration migration without improving UI ownership or eliminating a T3 responsibility. There is no new background host service.

## Projection and browsing limits

| Boundary | Enforced limit |
| --- | --- |
| Configured environments | 8 profiles; native and client identities unique. |
| Environment configuration | 64 KiB. |
| Native metadata page | 200 rows, with explicit continuation. |
| Companion output frame | 384 KiB. |
| Companion input / saved operation payload | 96 KiB. |
| Buffered event bytes | 2 MiB per connection. |
| Transcript projection | 80 rows, 144 KiB serialized rows, 24 KiB text per row. |
| Latest companion page | 24 rows / 48 KiB; earlier pages use the larger history budget. |
| Composer | 64 KiB / 9,999 lines. |
| Client state | 2 MiB; bounded draft, operation, preset and history collections. |
| Legacy renderer cache | Retained regression helper only; no active SLT performance claim. |

The limits prevent a disconnected host, huge result or long transcript from turning into unbounded frontend work. Truncated item display is explicit. Exact inspection can read the supported native source within its own bounds; complete material remains in T3. Historical pages do not authorize ordinary writes until a verified latest-live view is re-established.

Snapshots and deltas validate complete candidate state before replacing the current projection. Revision gaps, duplicate row keys, wrong sources and invalid order reject atomically. A gap requests resynchronization rather than guessing the missing changes.

## Human mutation path

An explicit action first verifies source, live navigation state, offered capability and provider policy. New launches use a chosen native project and a reviewed defaults revision. Existing-thread operations use the attached thread; approvals and questions retain the offered request ID/revision. Stop retains the reviewed run ID and native queue hold. Local labels, highlights and browser scope cannot substitute for these identities.

Before transport admission, the state transaction saves the operation's original ID, operation name, exact environment and complete payload. The operation then uses the verified connection or an exactly matched source connection. Ordinary operations and Stop have separate admission and recovery lanes. This lets Stop proceed while another operation is uncertain without changing either identity.

Acknowledgement must match the operation that was sent. One atomic transaction clears the corresponding journal record and preserves any newer draft generation. Only the submitted edit generation and exact bytes may be retired; even clearing and retyping identical text creates a distinct draft that survives. Draft edits made after dispatch survive. Native launch acknowledgement must identify the generated thread; conflicting thread identity is not accepted as completion. Batch filing advances only the acknowledged human text slice and retains the rest.

Failure leaves the original record available. Recovery first displays the whole saved action, then offers explicit same-ID reconciliation against the original environment. It cannot substitute a new host, change payload fields or invent a new ID. Clearing a record is a separately reviewed local action; it is not native cancellation. Viewing conversations never marks them settled. Native review, pin and snooze operations revalidate the displayed revision.

## Durable client state

The Rust state file is exclusively locked for the process lifetime. Updates clone the current value, apply and validate a transaction, write a private temporary file, synchronize it, rename atomically and synchronize the directory. Failures preserve the previous recoverable state; unknown schema fields are retained. Symlink state/lock paths are rejected.

The previous terminal state is imported only when the Rust file is absent, after taking a shared source lock and validating supported drafts, prompt checkpoints and inbox preferences. Its contents and host journals remain unchanged. Existing Rust state, including intentionally empty state, is never overwritten by migration.

Composer autosaves are debounced and versioned. Launch-form autosaves are also debounced; leaving a form, losing focus or quitting flushes applicable edits. Acknowledgements use the current version/text, not a stale editor snapshot. The external editor temporarily restores the terminal, keeps native background events flowing and returns text only to the original draft. Failure or oversized output is retained for recovery rather than silently discarded.

Unsent launch forms use `rustLaunchForms`, keyed by canonical `[profileId, environmentId, projectId]` identity, with a local revision and a 40-form bound. A pending launch carries the exact form key and digest; its acknowledgement retires only that unchanged form in the same atomic transaction. Supported imported launch forms remain reachable through Saved launch forms.

Local project scope, priority, reading anchor and review coordinates describe presentation only. Transcript anchors restore a wrapped offset only when the same source row and content digest remain on the loaded page; diff anchors require the same complete native source hashes. Exact source and content digests prevent an old coordinate or click from becoming a different native action. They neither schedule native work nor replace its state.

## Local source context

Completed assistant envelopes already loaded by the client are retained as
evidence through one dedicated in-process context worker. Capture is revision-deduplicated,
bounded and serialized; native truncation and page metadata accompany the exact
captured envelope. An excerpt is never promoted to complete source.
`/context-save` explicitly records a typed decision, evidence, journal or local
open loop. Automatic observation does not infer those lifecycle choices.
The private store is an explicit state-sibling directory, not an auto-discovered
repository journal.
fael-core supplies actual row, open/history, supersession and ranked query
semantics; no hooks, crew, daemon or new orchestration is enabled. Full captured
payloads are content-addressed separately from fael's 10 KiB row limit.
`/context-find` uses fael matching across full retained payloads and compact rows;
source-read failures remain explicit degradation.

Each immutable event and source blob uses a private temporary file, file fsync,
atomic no-clobber publication and directory fsync. Source blobs commit before
referencing rows. Interrupted temporary/orphan inputs remain recoverable;
traversal, parse and hash failures mark recall degraded. This is the tested
filesystem syscall boundary, not a hardware power-loss guarantee. Bounded
structured packs retain exact known totals, omissions, source/content revisions,
observation metadata and pack identity. `/resume` adds actual protected drafts,
original pending operations and native requests; nothing is executed or resolved.

The context worker lazily and exclusively owns Store and all its filesystem
operations: ObserveLoaded, Record, Close, Revisit, Recall, Resume and Original
source. It uses the existing crossbeam-channel dependency and a four-slot request
queue; draft `storage_pool` remains independent. One read is submitted at a time,
with one latest desired replacement. Exact serial/scope/app/view tokens prevent
stale results from repainting closed or replaced dialogs. Pending reads use an
existing compact Context modal so the real SLT Esc path invalidates their token.

Source-bearing writes are limited to one accepted request and 16 MiB per source
or observation batch, with a bounded pending observation set and one retained
unaccepted explicit write. Admission failure distinguishes Full from Disconnected
and returns the original payload. `/context-retry` is explicit; accepted failures
also retain their original request. Write completion is handled for its original
source, with source/generation and revisit edit-epoch/text checks before visible
success. No native operation is dispatched or replayed by this worker.

Normal Quit closes context admission and invalidates desired reads. It suppresses
completion-triggered scheduling, drains accepted results through the worker's
termination marker, and joins only after Store has been released. The bounded
SLT bridge retains batches outside its wake-only channel and recovers queued and
not-yet-forwarded packets when a terminal session closes. Final form/draft/UI
flush remains independent; write errors remain visible. Context I/O runs outside
the controller and draft worker, but whole-store scan duration remains unbounded;
no latency/resource saving or hard cancellation deadline is claimed.

## Verification boundaries

### Finite check evidence (A4, B3, S6)

`src/check_evidence.rs` and `scripts/check-evidence.py` supply one finite caller
and execution-host evidence asset. They do not change Fleet's `t3-check`, Rex
configuration, native scheduling or the Node compatibility boundary. The
controller saves the original native target, helper digest and unpredictable attempt identity
before preparation, then the qualified host reference before a single launch.
Rex creation observations are saved independently. Existing SSH profiles carry
remote execution; local Rex layout ownership is qualified before creating a
split. Display host and execution host are distinct.

The helper has independent exclusive run/child start markers. Command output
and selected regular-file bytes are bounded and retained outside source and
temporary roots. One <=48 KiB JSON return carries exact base64 bytes, hashes,
coverage and explicit unknown fields. Repository-wide HEAD/index/dirty identity
and snapshot isolation remain unknown. Command exit, wrapper exit, teardown,
pane creation and native acknowledgement are separate observations. Recovery
reads the same attempt with its original hash-pinned helper, using the available
host Python for inspection. Historical reads retain their original binding even
when the worktree or live tools have changed; fresh execution still requires
the original identities. Reads never install, prepare or launch another check.
The caller's observation deadline does not terminate Fleet's cleanup owner.
The same task-owned Rex helper keeps draining capped streams until wrapper exit;
if cleanup hangs, the pane and its attempt stay retained with an unknown result.
No automatic pane closure or replay resolves that uncertainty.

`src/app/checks.rs` retains the result and a separate human finding in existing
client State and admits each distinct returned envelope as its own Context
revision. In-process reads coalesce per attempt and older read generations cannot
replace newer evidence. Native acceptance is retained independently of later
collection outcomes. Native
follow-up uses the existing ordinary mutation lane with an origin discriminator;
it cannot retire an unrelated composer draft or its staged model override.
The explicit final send still validates original environment/project/thread and
current writable native state, persists the operation and matches its receipt.
It does not imply native task completion or automatic incorporation.

Selected Library content retains its original serialized native row through
shared immutable bytes. Context Capture validates its selected source/key/revision
and freezes those bytes before kind selection. Records and Resume use that
same source scope; an unrelated attached conversation cannot supply its draft
or questions. Loaded evidence and durable Context history remain distinct.

Rust unit tests exercise parser bounds, source identity, revision application, persistence/acknowledgement, keyboard semantics and rendering. Process tests exercise real child pipes, handshake rejection, deadlines, cancellation and Stop precedence. Deterministic native fixtures and PTY tests exercise both modes, resize, streaming, forms, native requests, failures and recovery through actual terminal bytes. Node/loopback tests exercise the retained native boundary and crash windows.

These tests are implementation evidence, not claims that a user's machine has a particular credential, native version, SSH route, terminal keyboard mode or provider account. [Verification](rust-verification.md) records the commands and actual results separately from the remaining installed-host checks. Those checks require integration access; no unfinished frontend implementation is delegated to a local native agent.
