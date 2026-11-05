# State and recovery

T3 owns conversations, runs, requests, execution, permissions, queues, worktrees
and native inbox disposition. The Rust client stores human input, unresolved
client operation identities and bounded presentation preferences. It does not
persist a second transcript, execution queue or task scheduler.

## Files and ownership

The client reads the existing version 1 `environments.json` profile file. Its
state file defaults to `~/.config/t3oxide/rust-state.json`; `--state` selects an
explicit alternative. The existing profile format and exact `environmentId`
binding remain unchanged. A single writer holds a file lock for each Rust state
path. State is written to a private temporary file, fsynced, renamed atomically
and followed by directory fsync. The whole file is limited to 2 MiB, and a human
draft to 64 KiB. Capacity failures preserve the prior state and report an error.

When the Rust file is absent, the client can import the adjacent `go-drafts.json`
and its prompt-recall and inbox-preference sidecars. It takes the previous
client's lock during import, validates identities and known records, preserves
unknown fields and writes only the new Rust state. Invalid or ambiguous input
fails closed. An existing Rust file always wins, including an intentionally
empty one. Original files are never overwritten or used as an ongoing second
state owner. All migration tests use temporary fixtures.

The on-demand host adapter retains its existing `operations-v1/` journal beside
its host configuration. That journal contains original operation fingerprints
and durable native receipts. It is recovery state, not disposable cache. Neither
client startup nor reconnect deletes it or replays writes.

## Human input

Ordinary drafts are qualified by exact native environment and thread. Launch
forms are also bound to their profile, native environment and project. Changing
a browsing scope does not change a draft's destination or the creation pin.
Developer and AgentBox share the same composer and active connection.

Typing and bracketed paste schedule a 200 ms durable save. Source changes,
explicit actions, focus loss and orderly exit flush relevant input. Storage has
its own bounded worker, so an unavailable host cannot occupy the draft-saving
queue. Failed saves stay visible; sending requires a successful draft checkpoint.
A hard process kill before the debounce expires can lose the last unsaved edit;
no client can promise persistence before it writes.

A send captures the draft's edit generation as well as its exact bytes. Its
acknowledgement clears only that submitted generation. A newer draft, even one
with identical text, is retained in the same atomic transaction that retires the
operation, records sent-prompt recall and consumes the matching one-message
model override. This closes the crash window between acknowledging a message
and restoring newer human input. Late acknowledgements remain bound to their
original source and cannot clear another environment's equal thread ID.

Prompt recall retains human-authored input with source identity and outcome. It
is not a transcript archive. Restoring or clearing a draft never sends it. A
source-qualified editor cache preserves caret, selection and undo within a
running client only when its bytes still equal the canonical draft. Durable
reading anchors use the native row identity and text digest; changed or absent
rows do not restore a misleading offset. Native diff reading positions also
require matching source and content evidence and never imply review completion.

External editing uses `VISUAL`, then `EDITOR`, then `vi`, with an argument parser
and no local shell. The terminal is restored while the editor runs; background
native events continue to drain. Text returns to the original source without
sending. Invalid UTF-8, oversized edits or unsuccessful editor exits preserve a
private recovery file and report its path.

## Mutation admission and receipts

Every mutation is saved before dispatch with its exact operation ID, original
payload, native environment and target. There is one ordinary pending lane and
one independent Stop lane per environment. Stop targets the reviewed native run
and requests native queue hold. A stalled ordinary action cannot consume Stop
admission. Requests can be answered only against the exact currently offered ID,
revision and decision schema; a generic reply or Reviewed action is not approval.

Ordinary conversation mutations require a verified writable connection and a
current live projection. History navigation disables them immediately. A fresh
snapshot and the matching navigation acknowledgement must establish the new
view before writes become available. Temporary dialogs do not cancel this
attachment acknowledgement. A sequence gap triggers fresh synchronization.

Host acknowledgement is distinct from an observed message, a provider response
and completed work. The receipt display correlates native message and run
identities before changing those labels. Thread visibility actions retain the
adapter's expected-revision preflight. The current native API does not provide
atomic compare-and-swap for visibility changes: another native client can still
race after that preflight. No stronger guarantee is claimed.

## Lost acknowledgements

A connection can fail after T3 accepted an operation. The original remains
pending. Startup and Reconnect perform reads only. Recover presents the exact
source, operation ID and payload before an explicit same-ID reconciliation.
A removed environment can be inspected but cannot silently use another profile.
A negative retry response does not prove the original attempt was rejected.

A definite first-attempt rejection may retire an operation that never took
effect. An uncertain result remains saved. Clearing a recovery record requires
explicit review and confirmation; it does not resend, undo or cancel native work,
and it preserves the human draft. Clearing never invents an acknowledged receipt.

A native launch records its new thread ID and complete reviewed defaults before
dispatch. Reconciliation uses that same ID and original payload. Matching native
evidence or the adapter's durable matching receipt establishes acceptance; a
matching title alone does not. A late launch acknowledgement retires only its
matching saved form. It cannot erase a newer form, replace a changed workspace
selection or redirect a user who moved to another conversation or opened another
dialog. Saved and imported forms remain accessible through Saved launch forms.

## AgentBox presentation and filing

Attention, Working, Queued, Later and Reviewed reflect native metadata. Local
priority and project order affect presentation only. They never claim tasks,
start agents or schedule execution. Local rank changes have a revision guard and
an explicit guarded undo. Viewing, scrolling and searching never settle a task.
Reviewed, Restore to inbox, snooze and pin are explicit native actions.

File multiple tasks accepts a human-authored list. Each item receives a separate
project/default/model/workspace review and an explicit Create. The remaining
text is a draft, not a queued operation. Bounded receipts distinguish
Acknowledged, Needs recovery and Remaining draft. Saving a pending launch and
its receipt item is one transaction; acknowledging it and retiring only its exact
unchanged prefix is another. A newer edited list is never overwritten.

Batch receipt and remaining-draft views survive restart. Resuming opens the
original source and human text for review; it never submits the remaining list
automatically. An uncertain item must be reconciled before continuing. Native T3
remains responsible for execution after an accepted launch and after client exit.

## Verification boundary

See [Rust verification](rust-verification.md) for executable test evidence and
[contracts](rust-contracts.md) for pinned native sources. The implementation work
uses disposable state and synthetic native endpoints; it does not touch installed
services or user files. Any later installation must preserve existing state and
journals. Replacing an executable does not undo a native effect or resolve an
uncertain operation.
