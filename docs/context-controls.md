# Local context controls (B3, S6)

The owned context store uses `fael-core` 0.0.1 from
`zecalis/fael@b00e312cc2c313e0595bb0a32214e914439105a8` (MIT). Only its row,
log, validation and query library is used. Completed assistant envelopes already
loaded through the client are automatically retained as source-qualified evidence.
This does not scan external transcripts or enable a hook, daemon, CLI, crew or
scheduler.

| Control | Fields | Meaning and validation |
|---|---|---|
| Observe loaded evidence | Exact loaded native item envelope, source identity, payload revision, truncation and page metadata. | Background capture through the dedicated context worker, one write in flight. Deduplicate identical revisions; retain changed revisions. Evidence/journal only: no inferred decisions, open loops, obligations or native mutations. Batches are bounded to 200 new rows and 16 MiB; omissions and failures remain visible in Context. |
| Record | Kind: decision, evidence, journal or open loop; concise text; native environment/project/thread and optional item/request IDs; source revision; observation metadata; full captured source bytes; optional explicit supersedes/revisit. | Explicit user action. Validate source IDs and retain the complete captured source in a content-addressed private file. Store only its reference/hash and a bounded summary in fael's 10 KiB row. Reject an oversized row rather than silently shortening source material. |
| Recall | Native environment, optional project/thread, search text, open/history, offset, row limit, actual byte limit. | Use fael's ranked query and paging. History explicitly includes closed/superseded context records. Report exact known eligible/shown/omitted totals; incomplete traversal or parsing is degradation, never proof of no evidence. |
| Resume | The same scope plus actual native human questions and protected draft/pending-operation metadata supplied by the client. | Return a structured, bounded pack. Preserve original operation/request identities and source revisions. Local open-loop notes are recorded context, never newly authorized human obligations. |
| Close / supersede / revisit | Exact local record ID, source identity/revision and explicit reason or revisit value. | Affect only local context records. Retain prior records and full source content. Never answer, settle, retry or close a native T3 request. |
| Open source | Exact context record ID and original source/revision. | Verify the content hash and return the captured bytes and provenance. Native truncation/degradation remains in observation metadata; a complete capture of a bounded native response does not imply a complete transcript. |

A filled record example:

```json
{
  "kind": "decision",
  "text": "Use the stable release for this fix.",
  "source": {
    "environmentId": "environment-a", "projectId": "project-a",
    "threadId": "thread-a", "itemId": "message-a", "requestId": "request-a",
    "revision": "native-request-content-revision-a",
    "observed": {"revision": 17, "state": "live", "limited": false}
  },
  "fullSource": "The exact captured native source envelope and full retained owner wording.",
  "supersedes": null,
  "revisit": "next release"
}
```

A filled recall example is project-scoped `text: "stable release"`,
`history: false`, `offset: 0`, `limit: 20`, `byteLimit: 16384`. Its pack identifies
the source revision and content hash for every shown row, the exact pre-page
count, omitted rows, native-record counts, warnings, completeness and a pack
identity. When completeness is false, counts describe the readable, admitted
records; unreadable records have no invented total. The byte limit covers the complete compact JSON pack, including metadata
and Markdown; fael's token estimate is not treated as a byte bound.

A filled protected resume item is `kind: "pendingOperation"`,
`identity: "original-operation-a"`, `status: "uncertain"`,
`summary: "Answer acknowledgement pending"`, with the original request's source
identity/revision. It is supplied from the client's protected journal; finding a
similar local note does not create or resolve this operation.

The store receives an explicit private directory beside the client state. It
holds a local exclusive lock. Each event is an immutable valid fael JSONL file:
write a complete temporary record, fsync it, atomically publish it, then fsync
its directory. Captured source blobs are durably published before referencing
rows. A crash can retain an unreferenced source blob or staged temporary file;
reads report these recovery inputs and never delete them. Success is returned
only after the relevant directory fsync. This boundary replaces fael's weaker
append durability without changing its query or row semantics.
This checks the file/directory fsync and atomic-publication boundary; it does
not claim a measured sudden-power-loss guarantee for Apple storage hardware.

The client exposes `/context-save`, `/context`, `/context-find`,
`/context-history` and `/resume`. Context record controls open the original
captured source, close the local record, save a revisit condition or explicitly
supersede it with the current native source when the source changed within the
same native scope. Revisit edits retain a separate autosaved draft. More and
Previous preserve the original scope/query; closed and superseded records need
Include history. Capture uses the original source payload, not an inferred
agent summary; the visible compact prefix never replaces that payload.
`/context-find` applies fael's all-word matching to the full retained payload as
well as its compact row, reading one source at a time. Missing, corrupt or
non-UTF-8 payloads produce explicit degradation; they do not establish that no
matching evidence exists. Explicit `/context-save` remains available for typed
decisions, journals and local open loops; these lifecycle choices are never
inferred by automatic observation.


All context filesystem reads and writes run in one bounded in-process worker,
which lazily opens and exclusively owns Store. Draft autosave keeps its separate
storage worker. The context request queue has four slots; the controller admits
one read at a time and retains only the latest desired replacement. Source and
observation-batch payloads are checked against the 16 MiB limit before admission;
the pending observation set retains at most 256 exact source revisions. Accepted
source-bearing writes run one at a time, without a cloned payload job backlog.

Pending Recall, Resume and Original source use the existing Context dialog with
`Reading…`. Esc closes it; a later result cannot reopen it. Query, page/history,
source/tab and modal changes invalidate the original serial, scope and view token.
Work already running on the filesystem finishes; its duration is not bounded by
read coalescing. A fresh read keeps its explicit original scope.

Queue admission is not save success. A busy or disconnected submission retains
its original explicit write intent in this process, and another explicit capture
cannot replace it. `/context-retry` (also available as Retry context write in the
records menu) explicitly resubmits that original intent. An accepted write failure
also retains the original payload for explicit retry and reports the failure;
it is never automatically replayed. Automatic observation can reschedule only
never-accepted inputs. These in-process retry slots add no new durable recovery
schema; existing source publication, protected journals and draft storage remain
unchanged.

Write acknowledgements follow Store's file and directory fsync completion. They
update bookkeeping for the original source after a view switch; another source
cannot receive a success notice. Revisit acknowledgement additionally checks the
submitted edit epoch and exact original-row draft text. Any intervening revisit
edit suppresses that notice; completion never clears a newer revisit draft.

Quit closes admission and drops unsubmitted read intent. It keeps receiving all
accepted write outcomes through the worker's termination marker, suppresses new
capture scheduling, and only then joins and releases Store. Even a full completion
queue is drained before join. A write failure keeps the client open for recovery.
Terminal suspension retains queued completions outside the terminal wake channel.
Final form, draft and UI persistence remain separate. No timeout or cancelled
thread is reported as a completed write. This establishes an ownership boundary,
not a measured latency or resource improvement; one worker thread is added.
