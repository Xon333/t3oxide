# Native workflows and recovery

Developer and AgentBox use the same authenticated T3 environments, projects and conversations. The companion presents and journals deliberate client operations; T3 creates worktrees, owns provider sessions and runs, dispatches queued messages, evaluates permissions, and retains its own receipts. Closing t3oxide does not stop accepted native work.

## Contract and compatibility

This is **native workflow extension 1**, additive to companion NDJSON protocol **1** and T3 orchestration protocol **2**. Existing operations and the Swift rollback client retain their protocol. The companion advertises `features.nativeWorkflows`; its absence gates new workflow operations before any write; existing conversation paths remain usable. The adapter's `SOURCE`/`adapterRevision` continues to identify the native compatibility anchor, not a claim that the adapter files are unchanged. The host installer derives a separate release identity from all adapter modules, now including `native.mjs`, and preserves previous releases and `operations-v1`.

The implementation was checked against real source at the following revisions on **2026-10-29**:

| Authority | Verified revision and implementation |
| --- | --- |
| Supplied native contract anchor | [`pingdotgg/t3code@0e7346ccd9427c06161e222319e718e6bd70887f`](https://github.com/pingdotgg/t3code/tree/0e7346ccd9427c06161e222319e718e6bd70887f) |
| Current published nightly examined | [`v0.1.4-nightly.20261029.3853`](https://github.com/pingdotgg/t3code/releases/tag/v0.1.4-nightly.20261029.3853), targeting [`783581d461dc72cdd8f78fc8abb4ad536dfc4547`](https://github.com/pingdotgg/t3code/tree/783581d461dc72cdd8f78fc8abb4ad536dfc4547) |
| Native HTTP/RPC registration | [`apps/server/src/ws.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/server/src/ws.ts), [`packages/contracts/src/rpc.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/rpc.ts) |
| Launch handler and receipt semantics | [`ThreadLaunchService.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/server/src/orchestration-v2/ThreadLaunchService.ts), [`ThreadLaunchService.test.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/server/src/orchestration-v2/ThreadLaunchService.test.ts), [`ThreadMessageIntake.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/server/src/orchestration-v2/ThreadMessageIntake.ts) |
| Project and new-thread defaults | [`projectSettings.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/shared/src/projectSettings.ts), [`useHandleNewThread.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/web/src/hooks/useHandleNewThread.ts), [`providerInstances.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/web/src/providerInstances.ts) |
| Repository defaults file | [`t3ProjectFile` contract](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/t3ProjectFile.ts), [`native parser`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/shared/src/t3ProjectFile.ts), [`JSONC rules`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/shared/src/schemaJson.ts) |
| Thread lifecycle and metadata | [`orchestrationV2.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/orchestrationV2.ts), [`Orchestrator.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/apps/server/src/orchestration-v2/Orchestrator.ts) |
| Diff, repository refs and PR links | [`review.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/review.ts), [`git.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/git.ts), [`threadPullRequest.ts`](https://github.com/pingdotgg/t3code/blob/0e7346ccd9427c06161e222319e718e6bd70887f/packages/contracts/src/threadPullRequest.ts) |

The subsequent M1 integration verified native project/default/ref/diff reads against installed T3 `0.1.5-nightly.20261029.3867`; [verification](verification.md) records its exact limits. A current public revision is compatibility evidence, not acceptance of another host. The native integrator must verify each authorized host's exact version, identity, authentication and handler behavior. A server's `serverResolvedCommandContext` descriptor allows this extension to be offered; it does not certify every optional RPC on older nightlies. Each call validates its actual result. Missing methods, unsupported results and unavailable defaults remain explicit failures with no invented success or fallback launch.

### Additive companion operations

| Operation | Native authority and boundary |
| --- | --- |
| `projects` | Reads `/api/orchestration/shell`; returns existing exact project IDs and roots. At most 200 projects and 192 KiB per client page; projects with no threads remain selectable. |
| `launch-defaults` | Reads `server.getConfig`/`server.getSettings`, exact project metadata, optional fixed `t3.json`, and native `vcs.listRefs` when a new worktree is chosen. Returns model/options, modes, workspace strategy and a digest of the complete reviewed defaults. |
| `launch-project` | Validates those defaults again, then calls native `orchestration.launchThread` once using separate stable command and thread IDs. The optional initial message has a stable ID derived from the command ID. |
| `thread-action` | Calls `orchestration.dispatchCommand` with native settle/unsettle, snooze/unsnooze or pin/unpin commands. It retains the offered environment, project/thread identity and observed metadata revision. |
| `thread-review` | Calls native `review.getDiffPreview` for the exact native thread worktree or project root; verifies the thread revision before and after the read. It does not fetch an arbitrary client-supplied path. |
| Existing `list` | Adds bounded `updatedAt` and `inbox` metadata. Reading or selecting a row does not subscribe its transcript or open its complete request form. |

The shell's pending runtime request is one summary, with its exact native request ID and kind. Its count is therefore zero or one; full forms are fetched only after explicit opening. Queue length is `-1` when the server does not report it. That value means **unreported**, never an empty queue. Thread, run and request IDs remain separate. No client task ID is synthesized.

## New and native defaults

`New` resolves its creation environment independently of the highlighted or attached conversation. Its order is:

1. An explicitly supplied CLI `--env` target.
2. The exact saved creation environment/project pin, when present.
3. The one enabled profile marked `"defaultForNew": true` in `environments.json`.
4. The sole enabled configured environment.
5. An explicit environment choice when multiple profiles remain ambiguous.

`defaultForNew` is an optional client preference in the existing version-1 profile file. At most one enabled profile may set it. It does not change any native account/model setting, require a watcher, or create a second configuration UI.

The native web client's preferred project/environment is resolved from client UI project ordering; this preference is not exposed by the examined server API. Consequently, automatically mirroring that desktop preference across clients remains a **native API gap**. t3oxide cannot honestly infer it from the last attachment or a personal example. The explicit profile default or prompt is the portable boundary until the native API exposes this preference.

After resolving the environment, New selects an existing native project. Project creation remains in T3. `New in same project` carries only the exact project ID; if that ID is absent from the creation environment it offers existing projects there. It does not match titles, reuse the source worktree, carry a source model/option override, or carry the source environment.

Model selection, model options and runtime mode come from the project's native overrides over the environment defaults. The adapter respects the legacy project fields only while native `projectSettingsFolded` is false. For workspace defaults, a null project/environment tier defers to the repository's native `t3.json`, then to native built-in local mode. The independent parser follows the pinned JSONC and schema behavior, including comments, trailing commas and invalid-file fallback. It reads the fixed repository file through the native API; it does not run its scripts. T3 remains responsible for its own configured setup scripts during a real native launch.

The native first-thread scaffold exception is preserved: a newly scaffolded direct child of the native `newProjectsRoot` uses its root for its first thread when only the environment default requested a worktree. Explicit project/file choices remain authoritative. When no model selection is configured at all, the adapter follows reported native provider order and reported model defaults. If a configured provider/model is disabled, absent or unavailable, creation fails visibly; no account, model or usage-limit fallback is performed.

The review allows explicit environment, project, workspace, model and advertised option choices. A different project/environment resolves its own defaults; only the draft title/message follows the deliberate change. An unchanged native option not described by the current picker is preserved, and editing a different offered option on the same model preserves that untouched native value. New or altered options must be advertised by the exact provider's current catalog. Selecting another model starts with that model's advertised options.

## Filing and workspace policy

AgentBox filing uses the same native launch form with a new worktree as its initial choice. The native backend creates and binds that checkout. The form also permits a deliberate shared project-root choice. A reviewed empty first message creates a real native thread without dispatching an agent run. A nonempty first message dispatches through the native launch handler only when the user chooses Create.

For a new worktree, the adapter uses native local refs and selects the reported default branch, or the current branch when no default is reported. It never guesses `main`, reuses another task's checkout, runs `git worktree` itself, or starts a nested provider CLI. A missing repository or unreported base remains unavailable. The native handler owns branch generation, setup and binding.

The reviewed form persists title, exact message bytes, selection, native-default revision, native project root, workspace strategy and runtime/interaction modes in existing client state. A later native default/root change leaves the original form intact and disables Create until the user explicitly reviews Defaults. Cancelling and reopening does not silently accept changed defaults. Pending launch receipts clear only the unchanged submitted form; newer edits survive.

## Inbox, decisions and review

Inbox rows are real native conversations. Developer and AgentBox open the same thread through the established attachment owner. The client stores only narrow presentation preferences and draft/checkpoint state; it does not clone transcripts or retain a second task/run database.

Snooze uses native `thread.snooze` with an explicit future timestamp; wake-up visibility is native state. Settle/unsettle and pin/unpin are also native commands. Marking a thread reviewed is a visibility decision, not proof that code was merged, a deployment succeeded, or a user accepted a formal approval. The client blocks settle/snooze while the attached projection contains active/queued work or a pending formal request. An ordinary reply uses the established native message path and cannot answer an approval.

Each action validates the offered metadata digest against a fresh native shell and uses an original journalled command ID. Accepted actions can include a freshly observed post-action digest for a source-bound inverse action. If that optional post-read fails, the original accepted receipt remains accepted and no undo revision is offered. The examined native command schema has **no compare-and-swap revision field**. These checks detect stale preflight/undo selections; they do not claim an atomic exclusion against another client changing state after preflight. Native permissions and lifecycle enforcement remain final.

Review displays native diff sources, native file-change counts and native PR links. Diff text is capped at 128 KiB before encoded-frame budgeting, with file/source limits and explicit truncation. Escaped text also fits the existing 384 KiB companion frame. Links come only from native thread PR records, including their reported state/time; the UI does not infer PR success from a branch name. Review is read-only: there is no automatic merge, push, deletion or claimed acceptance.

## Uncertainty, reconnect and exit

The existing client pending-operation owner and private host `operations-v1` journal record the complete original operation before native dispatch. A request/connection failure after a possibly accepted dispatch remains uncertain. Reconnect and UI restart do not replay it automatically. Stop has its existing independent admission, transport and state lane even while a launch receipt is pending.

Use `/retry-launch` for an explicitly saved native launch. It opens the original configured environment and retains the original command, thread and initial-message IDs. Before returning success it reconciles the exact native thread, project, model/options, runtime/interaction modes, workspace class/binding and initial message. Root launches require no worktree binding; isolated launches require a native absolute worktree path and branch distinct from the reviewed root; an existing-worktree legacy launch requires its original exact path/branch. The native launch result reports only a thread ID, so the client does not invent independent proof of an unreported generated checkout's original base SHA.

When authoritative identity and message evidence match, recovery acknowledges that original launch without redispatch. Absent, ambiguous, changed or incomplete evidence leaves the operation saved and uncertain. In particular, a prepared operation whose helper died before it reached T3 cannot be distinguished locally from a lost response. It needs native receipt reconciliation; creating another ID would risk duplicate work. Similarly, later native edits or history outside the bounded projection can make this conservative recovery inconclusive. Preserve the record and inspect native receipt/state rather than clear it or turn it into an automatic retry.

Native queued messages already accepted by T3 and existing runs continue under the backend after the TUI exits. **Future priority-based dispatch of unlaunched briefs after exit is not implemented by the verified native contract.** Native recurring `scheduledTasks` are a different product contract and are not repurposed into an AgentBox backlog scheduler. There is no permanent client process, provider supervisor, execution queue authority or host watcher added here. Prepared client drafts do not execute after exit.

## Adoption and pending native checks

Existing profile/state files remain valid. New optional form and preference fields use the existing state owner; native launches/actions use the existing operation lanes and host journal. Preserve the whole state directory, recall sidecars, operation journals and previous installed releases. A newer client is required to recover the newly named operations; the retained Swift rollback client and its separate state are not a migration tool for them. Finish or preserve pending operations before rolling back the Go client.

Portable tests exercise the actual Go client, Bubble Tea program, Node adapter, HTTP/RPC framing and native-shaped local fixtures. They are not physical terminal-key evidence or an installed-host claim. Native adoption still requires the authorized M1 integrator to verify project/default discovery, native empty filing and isolated launch, exact approvals, held launch plus independent Stop, stale/changed multi-client recovery, diff/PR availability and native work continuing after client exit. Separately authorized remote-host checks must use that host's exact identities and installed revision.

After a capability change, run `scripts/check-capabilities.sh` on the canonical knowledge host and review affected existing records with the prescribed helper. That host-specific helper is not supplied by this source tree. Its earlier portable absence and the completed M1 record review are recorded separately in [verification](verification.md). Another host's integration or client publication requires its own authorized scope and preservation checks. The subsequent owner-authorized M4 publication is recorded there as installed-file and read-only connection evidence.

## Creation destination and scoped defaults

`/destination` pins an exact configured profile/environment/project independently
of the highlighted or attached conversation. The pin contains identity only and
is atomically saved by the existing private state owner. New resolves current
native model/account/options/workspace defaults again. An explicit CLI creation
profile wins; otherwise the saved pin precedes `defaultForNew`, a single configured
environment, and an explicit choice. A removed or identity-changed profile is an
error requiring Change/Reset; it never retargets by display label. Reset restores
that precedence. `new-same` carries only the source project into the chosen
creation environment.

New offers **Pin destination**, **Creation destination**, and **Native defaults**.
The latter separates **Project on environment** from **Environment**, shows each
field's source and whether it overrides an inherited value, and retains an
unavailable stored provider/model. This-launch controls remain staged only for
that launch. A project field reset means removing that override, not saving a
copy of today's inherited value.

Persistent default writes are deliberately unavailable for the inspected pinned
T3 contract: `server.updateSettings` accepts a patch and optional provider
mutation but no revision/CAS or operation ID, and project overrides replace a
whole entry. The adapter returns the exact limitation and native Settings route.
The client cannot safely preserve concurrent opaque siblings or reconcile an
uncertain write under that contract. Reads, scoped provenance and creation-target
pinning work; enabling a future write flag alone grants no authority. Native
Settings remains the supported edit/reset route until a versioned safe write
contract is implemented and verified. No desktop-local preferences are read.

Explicit **Search server conversations** uses the bounded native search contract;
ordinary loaded-list typing does not. Both Developer and AgentBox offer it when
the loaded filter has no matches. AgentBox switches explicitly to All while
retaining environment/project/priority filters. Returned native message matches
are not filtered away by their titles. Editing the query cancels the old read,
retires its result generation and filters the already loaded metadata locally;
it does not issue another native search. Its response is marked incomplete because
the inspected native search response has no completeness cursor. Space preview
uses a separately canceled read-only helper, clears all request/run authority,
and never watches, marks seen, changes the attachment, or replaces its draft/Stop
target. Older helpers show the metadata-only limitation. Pin/unpin requires the
advertised native pin capability and the displayed source revision; it uses the
ordinary saved-operation lane and never automatically retries a stale offer.
