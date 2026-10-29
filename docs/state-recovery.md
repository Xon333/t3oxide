# State, migration and recovery

The integrated client preserves the existing Go and Swift ownership boundary. Native T3 owns threads, runs, requests, execution, worktrees, review/snooze state and authentic receipts. Local files retain human input, client mutation recovery and a small amount of presentation preference.

## Files and versions

| Owner | State | Compatibility |
| --- | --- | --- |
| Native T3 | Conversations, runs, requests, queues, worktrees, settled/snoozed state, PR metadata | Never copied into a second client database. |
| Go State | `go-drafts.json`: source-bound drafts, complete launch forms, next-send options and normal/Stop pending records | Existing versioned owner and exclusive writer lock remain. New launch-project records are explicitly validated. Older clients may refuse new records; preserve them instead of resetting state. |
| Prompt recall | `go-drafts.json.prompts.json` | Existing bounded source-bound history; no transcript archive. |
| Adapter | Host `operations-v1/` journal | Existing original operation/payload fingerprints and durable receipts. Never delete to resolve uncertainty. |
| Presentation ranks | `go-drafts.json.inbox-v1.json` | Version 1; 256 KiB, at most 1,000 task priorities and 200 project ranks. Exact JSON-array environment/project/thread keys. Revision compare-and-swap rejects stale updates/undo. Unknown/malformed input is preserved and reported. |
| Appearance/mode | `terminal.json` | Optional mode, navigation and existing theme fields. Missing mode means Developer. Other appearance values are preserved on mode changes. |
| Shortcut file | `shortcuts.json` beside terminal settings | Optional version 1; startup-only validated context/action overrides. Remove it to restore defaults. No watcher or UI editor. |
| Swift rollback | Separate `drafts.json` and existing legacy app/state | No new Go write targets the Swift file. Do not expect Swift to import new Go pending records. |

Batch filing retains remaining human input through the existing source-bound draft/checkpoint owner. It is not a task database or scheduler. Acknowledged items are retired only after their exact native receipt; unresolved launch identity remains in the normal recovery lane. Finishing local input is not a promise to dispatch future tasks after exit.

No automatic live-state migration or installation is performed by this source return. Before native adoption, preserve existing files, absences, lock ownership, release pointers and host journals using that host's established process. Do not replace current user state with test fixtures or an earlier snapshot.

## Native-default launch migration

New now resolves the creation target independently of the active thread. Set at most one profile's optional `defaultForNew: true`, or use explicit `--env`. A single configured environment is unambiguous. With multiple environments and no explicit creation default, the client asks; it does not infer a default from the newest row or last attachment.

Existing legacy launch drafts and uncertain records remain source-bound. New native project launch forms use a separate project-launch scope. Restored form text, title and options stay exact. If the native defaults revision changed, the review requires explicit Defaults before Create. This avoids silently rebasing a saved draft onto new model/workspace policy.

## Lost acknowledgements

A send/answer/selection/Stop or native visibility action can succeed remotely while its response is lost. The original operation remains saved until its existing explicit recovery path establishes an authentic result or the user reviews resolution. Reconnect and startup never silently retry mutations. Ordinary and Stop lanes remain independent, so a pending ordinary action cannot consume Stop admission.

A native project launch additionally records the new native thread ID before dispatch. `/retry-launch` selects an original saved launch, opens its exact environment, and reuses its persisted operation/payload identity for reconciliation. The adapter reads that native thread and checks project, model/options, modes, workspace placement and original initial-message identity when present. Matching evidence acknowledges the original effect without creating another thread. Absent, inaccessible, partial or mismatched evidence leaves it uncertain; do not use a fresh Create as a retry.

A durable host receipt already acknowledges its exact fingerprint. A successful-looking current run, a matching title, or an empty pending-request list does not prove that an arbitrary earlier operation succeeded. No UI label upgrades acknowledgement into response or completion. Receipt correlation still requires the native initiating message and run identity.

## Source-bound forms and local return

Forms carry exact offered request/revision or launch/default identity. Returning from Commands/Info keeps their query, draft, caret/selection and focus while the form is live. Retired prompts cannot be restored. Formal approval is possible only through its exact allowed answer, never generic Reviewed, a plain reply or a numeral without the offered request review.

Developer and AgentBox share the same editor and native attachment. Local return records are bounded to 64 source identities and contain editor/reading position only. A cached editor is restored only if its exact bytes still equal the Session's canonical draft. Native row-key/line anchors account for changed width; the transcript itself is reacquired from T3. Process restart restores durable text and pending identities; it does not claim persistent terminal caret/selection from an earlier process.

## Undo and multiple clients

Visibility undo is a new explicit inverse native action against the post-acknowledgement revision. If a different client modifies that thread, the inverse is stale and refused. Local priority undo uses its sidecar revision. Neither mechanism reverses provider execution or Git effects. Native visibility commands do not expose an atomic expected-revision field: the adapter rejects a mismatch in a fresh preflight read, but cannot exclude a different native client racing after that read. Strict atomic exclusion needs a native API change.

One writer owns each Go state path. Clients with different state paths may maintain different presentation ranks; native state remains authoritative for both. They do not claim work, dispatch a shared backlog or overwrite one another's execution ledger. Native commands and the host journal retain their exact identity checks.

## Rollback

Restore the accepted prior executable/helper release pointers using the host's existing atomic process. Preserve all new state and pending operations first. Do not run an older writer against new records it cannot understand, erase an unresolved journal, overwrite state with a pre-action backup or assume a release rollback undoes native effects. Swift uses its own state and remains available; unresolved Go actions still need review in their original environment.
