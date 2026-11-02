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
| `src/app/input.rs` | Explicit key/mouse/paste semantics and save-before-leave behavior. |
| `src/app/actions.rs`, `src/app/native.rs` | Action dispatch, model/request/launch forms, native capability checks and reviewed operations. |
| `src/app/workspace.rs` | Local project/priority views, reading coordinates, loaded-content navigation and source copying. |
| `src/ui.rs` | Pure presentation, responsive layouts, bounded Markdown reuse, editor styling and exact drawn hit targets. |
| `adapter/` | Existing verified native integration, bounded source projection and durable host receipts. |

Developer and AgentBox are views over these same models. They share the active connection, draft, projection, requests, recovery and creation forms. Mode changes alter visual priority and default lanes; layouts alter pane space and focus. Neither creates a native session or a separate execution owner.

## Component decisions

Ratatui 0.30.2 and Crossterm 0.29 provide the only terminal framework. Ratatui handles cell-buffer differences, clipping and layout; the application does not maintain a parallel ANSI renderer. `ratatui-textarea` 0.9.3 supplies Unicode editor movement, wrapping, selection, undo/redo and screen-to-data mouse mapping. `tui-markdown` 0.3.13 supplies Markdown rendering with optional syntax-highlighting dependencies disabled. `nucleo-matcher` 0.3.1 supplies fuzzy action scoring without its threaded application index.

Ordinary threads and bounded Crossbeam channels are sufficient for this process/stdio transport. Separate pools handle two foreground reads, one metadata read per configured source (at most eight) and one storage task at a time, with bounded queues of 16, 16 and 4. Each metadata source has an available slot even when other hosts hang, and unavailable sources cannot occupy the workers needed to attach a conversation or save a draft; the UI receives typed background events. No async executor, HTTP proxy, terminal multiplexer, database, scheduler or second UI framework is introduced. Pingora does not address a missing transport need. Herdr and TUIOS supply useful interaction principles while their daemon and PTY ownership remain outside this client's scope. Restricted FrankenTUI code is not incorporated.

Cargo.lock pins the complete dependency graph. `docs/dependencies.json` records source archives/checksums and `LICENSES/rust-third-party.txt` retains license texts. The MPL-covered fuzzy matcher is an unmodified library with its corresponding source link supplied; no covered source file is copied into t3oxide's own modules.

## Event-driven presentation

Terminal input, host metadata, transcript events, read completions, acknowledgements and draft-save results flow to one application owner. Bursts are coalesced into redraws with a 16 ms lower bound between frames; an idle screen does not run a fixed-rate rendering loop. Rendering reads a view model and returns hit targets. It does not initiate network requests or write state.

Markdown is cached by exact displayed message identity, source text/role/truncation, pane width and theme. The cache has both an entry limit and a byte limit. Wrapped measurements have a separate bounded cache. Only visible transcript rows are drawn, with accurate wrapped scroll offsets. A streaming revision invalidates the affected row's cached representation. Resize changes the width key and recomputes layout; it does not detach or rewrite a draft.

Split gives the navigator, transcript and composer stable space. Wide adds the Details pane when there is room; Focus gives space to the current surface. Small terminals show the focused pane and retain the same focus cycle. Long original prompts and recovery records have an independent scroll area; reading them never changes the selected mutation choice. Source/control characters are sanitized for presentation while original draft, item and operation text remains intact.

The same action catalog feeds the fuzzy palette and visible shortcut hints. Paste reaches an editor as literal content. It never enters command dispatch. Keyboard repeats are accepted for navigation/editing and rejected for repeated submit/Stop/quit actions. Legacy control-byte aliases and enhanced key identities share explicit submit behavior.

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
| Markdown cache | 128 entries / 4 MiB; wrapped measurements capped separately. |

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

## Verification boundaries

Rust unit tests exercise parser bounds, source identity, revision application, persistence/acknowledgement, keyboard semantics and rendering. Process tests exercise real child pipes, handshake rejection, deadlines, cancellation and Stop precedence. Deterministic native fixtures and PTY tests exercise both modes, resize, streaming, forms, native requests, failures and recovery through actual terminal bytes. Node/loopback tests exercise the retained native boundary and crash windows.

These tests are implementation evidence, not claims that a user's machine has a particular credential, native version, SSH route, terminal keyboard mode or provider account. [Verification](rust-verification.md) records the commands and actual results separately from the remaining installed-host checks. Those checks require integration access; no unfinished frontend implementation is delegated to a local native agent.
