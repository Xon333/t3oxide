# Accepted refinement implementation

This change extends the `1bd8cfa9ff69b560e3b1bd8e77d9ef92cb5688f9` source without changing companion protocol 1, dependency versions, release version, the Swift rollback client, or native T3's execution authority. Native compatibility details and pinned primary sources are in [native refinement contracts](native-refinement-contracts.md). Durable storage is described in [state and recovery](state-recovery.md).

## Responsive rendering and controls

Live assistant bodies render as sanitized plain text while their authoritative row is streaming. Completion enables the existing rich renderer. A source/role/layout/theme keyed body cache separates text from status and streaming decorations. Effective geometry is used for reuse; a rejected intrinsic over-cap row retains a bounded rejection sentinel. Replacement, shrink and final reflow remain explicit invalidations. Reading position uses the source row and text position when it survives the reflow, with a bounded fallback when it does not.

Control and sequence-only events apply native authority without rebuilding the transcript, installing the same viewport content, or rescanning search. Changed content is prepared outside the event loop; the existing maintained viewport still owns scrolling. There is one active render and one latest pending request, preserving useful progress under a continuous stream. This is not viewport virtualization or a replacement Markdown engine.

Modal content and hit geometry are captured from the same actual draw. A fresh offer fingerprint and the existing generation/lifetime checks reject stale clicks without rendering the form again. Paste, native editor caret/selection, independent Stop and explicit request Submit retain their existing owners.

## Navigation and source identity

Metadata from healthy sources becomes usable while slow/offline sources remain visible. Filtering loaded rows is local. A zero match offers **Search server conversations**, an explicit bounded native search whose incomplete coverage is reported per source.

Canonical shelves give Pinned precedence over native Working membership. While the list is focused, existing positions and shelf slots stay fixed and statuses update in place. New arrivals append; an earlier shelf can temporarily continue later in the list. Removed rows remain clearly disabled metadata until order is applied, preventing an old position from authorizing another target. Leaving the list or Refresh applies current canonical order and anchors by exact identity. Native identities shared by configured routes are deduplicated.

Ordinary Up/Down selects without attaching. Outside the filter editor, **p** invokes the exact revision-bound native pin action and **Space** requests one bounded preview. The preview has independent cancellation, no subscription, mutation, seen mark or automatic request presentation. Foreground open, Send, Stop, pin and request admission cancel it. A late response from a retired target cannot replace the current preview or conversation.

The project bubble contains two project characters and a machine-kind mark; unknown kind is neutral. Selected titles use a bounded first/tail expansion so long common prefixes can still be distinguished. Info exposes full source identities. Provider instance identity and reported account telemetry are separate; unreported telemetry is labeled and never inferred from a model or hostname.

Compact navigation names the attached conversation and retains its Requests, Stop, primary run badge and connection state while the picker occupies the screen. These controls continue to target the attachment, independently of the highlighted metadata row.

## Launch, delivery and requests

Creation destination is independent of browsing and attachment. The visible hint follows the same precedence as New: explicit CLI environment, exact persisted environment/project pin, configured default, single environment, or explicit choice. An unavailable target remains visible; it is not replaced. Only destination is pinned. Each launch reloads effective native defaults, and the exact empty message uses **Create thread**. Any nonempty initial message, including spaces or newlines, uses **Create & send**.

The scoped native-defaults view shows each field's source and inheritance and the native Settings route. Its read-only viewport scrolls through every reported value and the exact limitation, while scope, Refresh and Back remain visible. Up/Down, Page Up/Down and wheel scroll; Tab reaches the offered actions and Enter activates only the focused action. Persistent writes and project-field reset are explicitly unavailable under the pinned API: it lacks server-enforced revision/CAS and durable operation IDs. A display digest does not substitute for either primitive. This-launch model/options remain separate staged controls and continue to work.

Busy composers expose Send and Auto / Queue / Steer. Choosing a mode stages the next send; saved delivery is unchanged. Auto says **T3 decides**. A staging revision prevents a late acknowledgement from clearing a newer choice, including the same value restaged. Exact draft/caret state and source-qualified local returns are preserved.

Formal requests immediately expose a cue. An attached live request may automatically present after 1.5 seconds idle or editor blur, while paste, other forms and external editing defer it. Requests opens sooner explicitly, including from Find and AgentBox. Automatic presentation initially leaves controls unarmed; queued editor text returns to its original editor and cannot select or submit an answer. Escape suppresses only that environment/thread/request ID and revision for the current attachment. A new revision is new attention. Historical pages never auto-present, but explicit Requests can still review the client's verified live offer. No Escape action answers, clears or natively dismisses a request.

## Recovery and review

**May already be sent — Review saved action** is available across sources and before attachment. It shows original target, action, payload and operation ID, then uses the existing journal for explicit same-ID reconciliation. An unavailable original route remains unavailable. Reconnection and preview never replay a mutation.

AgentBox uses **Completed** for execution and **Reviewed** for review disposition while retaining native enum `done`. Reviewed/Later acknowledgements stay in the current attachment. Next and Back to inbox are explicit, with source-qualified frozen order and current filter eligibility. Uncertainty or failure cannot silently advance to another item.

Multi-task filing persists a minimal receipt in existing state: Acknowledged, Needs recovery, Remaining draft. It pauses at the first uncertainty/cancellation/error. Acknowledged thread identities and the uncertain operation's original ID/payload survive restart. Reconciliation does not dispatch a remaining task or overwrite an edited human list. Remaining draft is never described as Queued.

Changes show source, observation time, hashes, file count, clipping and native PR sync time or unreported. A bounded local reading offset restores only for the exact source and complete matching diff hashes. Changed, missing or clipped hashes reset the position with an explanation. An empty diff or completed run does not establish tests, merge or human review.

## Evidence boundaries

Maintained tests cover actual Go components, the real Bubble Tea event loop, assembled executable POSIX PTYs and the actual Node adapter against synthetic HTTP/WebSocket fixtures. Content-free opt-in performance probes keep boundary names, sample counts and raw observations. Actual Linux xterm window captures are real target-app screenshots using fixture data. They do not certify an installed native T3 version, real accounts, macOS physical keys/mouse, real terminal pixels from input timestamps, or production latency.

The full implementation return records all accepted requirement statuses, exact checks and before/after raw measurements. Installed-host schema/capability verification, the missing safe defaults-write primitive, retained Swift/macOS build checks, canonical capability verification under its separate authorization, and human physical terminal acceptance remain explicit native completion items. No deployment or merge is part of this change.
