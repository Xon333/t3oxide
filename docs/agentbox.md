# AgentBox mode

AgentBox is a task and decision view over real native T3 threads. Developer remains the default for a new settings file. Use Commands → AgentBox, `/mode agentbox`, or the resolved Mode shortcut; `/mode developer` returns. The selected mode is saved in `terminal.json`.

## File and launch work

`/file` opens the native creation flow. Choose an existing project on the creation environment, review the exact title/message, model/options and workspace, then activate Create. AgentBox requests a native isolated worktree by default. Workspace can explicitly choose shared root; the review names that choice. The client does not create a checkout or start a provider itself.

`/file-many` explicitly treats each nonempty line as one task and reviews each separately. Pasting a list into an ordinary message does not silently split it. Each Create has its own operation ID, native thread ID and authentic receipt. Cancel pauses local filing, and a lost response preserves the original pending operation for reconciliation. The remaining input is a human draft, not a background execution queue. Closing the client cannot dispatch the remaining list.

An empty first message files an empty native thread. A nonempty first message starts native work. You can file and explicitly launch several independent tasks; native T3 owns execution admission and capacity. Project creation, provider setup and administration remain in full T3. Same-project New in Developer carries only project identity; it does not copy source account/model/options/worktree.

## Inbox and decisions

The inbox defaults to **Needs you**. `V` or Commands changes view; `/` filters loaded metadata locally. With no matching rows, **Search server conversations** or Enter explicitly searches bounded native message content across configured sources. This switches the presentation to All while retaining environment, project and minimum priority filters. Healthy sources appear progressively, and pending, failed and incomplete sources remain visible. Native message matches remain visible even when the query is absent from their titles. Editing the query retires old results and returns to local filtering without another read; Enter with loaded matches or Refresh explicitly reloads metadata. Additional native pages remain explicit. Every row keeps environment/project/thread identity and the native revision; duplicate thread IDs across environments cannot alias.

| View | Evidence |
| --- | --- |
| Needs you | A native formal request, blocked/failed work, or unsettled inactive work requiring review. |
| Working | Reported active/preparing/starting/running state. |
| Queued | A native queued state or explicitly reported queued count. No local filed draft is presented as an accepted native queue. |
| Later | Native `snoozedUntil` in the future. |
| Reviewed | Native settled visibility after active work/requests are accounted for. This does not mean changes were merged. The native enum remains `done`; `/inbox reviewed` is the displayed command. |
| All | Every loaded eligible native task row, including states outside the current decision view. |
| Unknown | Missing native inbox projection. Open the conversation for exact available state; absence does not mean success or Inbox Zero. |

J/K or arrows select a row; Enter or a row click opens its exact T3 conversation for a focused decision. The summary is a bounded native assistant message/excerpt, not a newly invented outcome or a parse of prose into authoritative completion. Reported runtime state remains separate. `S` toggles the excerpt. Use `O` for Developer, `/activity` for source-bound tools, and `/web` to inspect full T3 history, artifacts and attachments through the existing configured address. HTML or visual artifacts are not rendered by a hidden browser.

The decision offers Changes, Reply and Reviewed when appropriate. A formal request changes those options to Requests, Conversation and Later. Numerals or a single click activate only the displayed current choice and can open a request review; they do not approve a permission. Summary text is inert. Controls retain their source and revision, and old clicks are rejected after a changed offer, source loading/reordering, resize or mode switch. `/mouse off` disables clicks. A normal reply never clears a still-offered formal request.

## Reply and formal requests

`R` reuses the same native thread and ordinary composer. Enter inserts a newline. Explicit Send uses the selected native delivery (`auto` initially); explicit Steer is separate and does not change that saved delivery preference. During reply, letters, punctuation and numerals are text. Escape returns to the decision while preserving the exact draft.

Requests retain the existing exact request ID, revision, allowed answers and validation. Click an approval or question option to stage its exact value; Submit decision or Submit answers is the explicit final action. Click answer fields to edit, Back/Next to retain staged multi-question answers, or Cancel to leave without answering. Submit is distinct from ordinary Send. A stale/replaced request cannot receive an answer through an old form. Ctrl+S remains an independent reviewed-run Stop with native queue hold, even over another action or dialog.

## Changes, review and Later

Changes uses native `review.getDiffPreview` over the exact thread's reported worktree or project root. The adapter validates returned cwd, clips the diff/file projection to its wire budget, and retains native hashes and linked PR metadata. Truncation and unavailable capabilities are explicit. A model's completion message does not substitute for Git data.

Reviewed uses native settle/unsettle. It refuses active/queued work or a pending formal request. A later native event can move the thread back into Needs you. No commit, push, merge or worktree deletion is performed. PR metadata may be stale or absent according to the native source; opening full T3 is the existing supported route for broader Git actions.

Later uses native snooze/unsnooze with an exact timestamp. Its backend remains responsible for visibility and wake behavior. It does not schedule a new execution. The client gates hiding active work and unresolved formal requests, and the adapter revalidates fresh native state before dispatch.

`Z` can undo a just-acknowledged reversible visibility action only against its returned revision, or restore local rank preferences through compare-and-swap. If another client changed the thread/preferences, undo refuses the stale inverse. Sending a message, granting permission, launching, committing or merging is never represented as locally undoable. Native visibility commands have no atomic expected-revision field; fresh preflight rejects known changes but cannot exclude a native writer racing after that read.

## Priority and execution boundary

`P` sets Low/Medium/High/Urgent or a project rank. Ordering is deterministic: Urgent first, ranked projects before unranked, task priority, recent native activity, then exact identity. The same presentation ordering is used throughout the inbox. It does not interrupt work or decide which provider is allowed to run.

The verified native contract has **no general one-shot task-priority backlog scheduler** with host concurrency/claim semantics for this workflow. Its scheduled tasks are a different recurring/fixed-time/webhook facility. Consequently the client does not claim starvation prevention, automatic urgent-task dispatch, a cross-host execution budget, worker leasing, or future filing-list dispatch after exit. Those acceptance cases require a native T3 capability. Existing native runs and accepted message queues can continue without the TUI; the client adds no permanent service.

## State, refresh and compatibility

One live Session owns the detailed thread. Inbox metadata uses bounded helpers; no helper subscribes every row. Explicit refresh and relevant attached lifecycle transitions refresh the inbox. Streaming tokens do not create a polling loop. Offline or older hosts remain visible with their failures/capability limits.

Switching modes retains the existing attachment, composer/caret/selection, form identity, pending operations, receipts and reading anchors. Exact source-qualified return state survives local thread changes; durable drafts/journals survive restart. See [state and recovery](state-recovery.md) and [native contracts](native-workflows.md).

AgentBox-inspired behavior is independently authored. Pinned upstream source and tests were inspected for behavior, but no GPL code, tests, worker instructions or artwork were copied or translated into this source tree. Existing t3oxide and third-party notices are retained, with the native adapter adaptation scope updated; this source snapshot does not add a blanket license.

Thread status badges use the same labels as Developer: Active, Waiting, Failed, Blocked, Queued, Completed, Stopped, Idle and Unknown. Failed work remains visible as Failed even with a pending request; the decision pane separately shows the request count. Native review/snooze disposition is separate from run completion. The badges retain selection highlighting and use text as well as color.

After a successful Reviewed or Later action, the current attachment stays open with an acknowledgement. **Next** deliberately opens the next eligible source-qualified item in the frozen list order. **Back to inbox** leaves the acknowledgement. A failure or uncertain result keeps the current item and its recovery cue. Ordinary Up/Down selection does not attach a conversation.

The reply composer has a separate **Send** control and **Auto / Queue / Steer** intent menu. Choosing intent stages one message; it does not submit or change the saved delivery setting. Auto means **T3 decides** until native evidence reports an outcome. A late acknowledgement cannot clear a newer draft or override.
