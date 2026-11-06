# Native attention content (B1–B3, S6)

These definitions precede the Mode B implementation. They adapt existing native
T3 records; they introduce no content synthesis service or new delivery channel.

| Content | Payload and controls | Validation and retained edits |
|---|---|---|
| Human question | Environment, project, original thread, request ID, full-content revision; native question ID, header, question, required/multiple/custom flags and native option values. Choice controls retain an explicit ID, or a deterministic ID derived from question ID and native value. Text, single-choice and multiple-choice fields; explicit Submit, Cancel and Open source. | Only pending native `user_input`/`approval` summaries enter the queue. A summary opens its source for the full live offer. Required values, offered values, types and duplicate selections are checked before dispatch. Every edit saves under the complete source/revision identity. Cancel closes the editor and retains the draft; it does not answer/cancel a native request. A new revision gets a separate draft; old text remains available for recovery and is never silently submitted to changed wording. |
| Supporting Markdown | Original source thread/item ID, source revision, role, full bounded Markdown payload and its native truncation marker. Read-only Markdown; Open source and Copy. | Render existing content separately from navigation. Preserve source text; do not classify prose as a pending human question. Unsupported diagrams remain source text with their actual rendering limit. Opening content neither answers nor settles anything. |
| Completion receipt | The native assistant completion message and its source thread/item ID/revision. Read-only outcome/evidence/next-action content exactly as supplied. | Run completion is not human acceptance, settlement or acknowledgement of an answer. No inferred receipt status or invented summary. |

A filled question example, using generic identities:

```json
{
  "environmentId": "environment-a",
  "projectId": "project-a",
  "threadId": "thread-a",
  "requestId": "request-a",
  "revision": "<sha256 of the full native request and item>",
  "questions": [{
    "id": "release_scope",
    "header": "Release scope",
    "question": "Which release should receive the fix?",
    "required": true,
    "multiSelect": false,
    "allowCustomAnswer": true,
    "options": [
      {"id": "stable", "label": "Stable", "value": "stable"},
      {"id": "next", "label": "Next", "value": "next"}
    ]
  }],
  "draftAnswers": {"release_scope": "stable"}
}
```

The UI displays the native wording and choice labels; IDs are transport/state
identities. Focus, a recommendation and a selected answer are independent.
If a selected question changes or leaves the live queue, its source and supporting
context remain visible with Answer disabled. Open source still works; explicitly
selecting the current queue item refreshes the view before an answer can begin.
Project navigation includes native inventory projects with no human questions;
these projects do not create queue items.

## Independent content selection (B1–B3)

Definition before the correction implementation: loaded assistant content is
available independently of pending questions. A content item carries `key`,
`source`, `title`, `kind`, `markdown`, `revision` and `truncated`. `source` keeps
the exact environment, project and thread identities; `key` also identifies the
original message. `revision` qualifies its unchanged payload. Display titles
never become source identities.

| Kind | Payload and controls | Validation and retained edits |
|---|---|---|
| Explanation | Original Markdown with concise findings, relevant evidence and optional diagrams. Select, read and Open source. | Ordinary assistant output remains an explanation. Reading does not create a human obligation, settle a thread or send an answer. |
| Research | Authored research findings, sources and unresolved questions. Select, read and Open source. | A specialized kind requires an explicit authored heading or supported source metadata. Paragraph wording, retrieval scores and completion state do not infer it. |
| Guide | Authored steps, prerequisites and relevant examples. Select, read and Open source. | Preserve the original steps, source and truncation marker. A guide does not become a checklist of developer obligations. |
| Receipt | Authored outcome, evidence and remaining work. Select, read and Open source. | An explicit receipt heading or supported source metadata qualifies this kind. A completed run alone does not. |

The project navigation region remains present while reading content or answering
a question. Returning to the global project level retains the selected content.
Context reveals detailed provenance separately. Changing selection retains any
answer edits under the original request revision. Content controls are read-only;
native question forms retain their explicit Submit and acknowledged-delivery
boundary above. Unavailable, changed or excerpted sources cannot be presented as
complete current evidence.

Filled examples below are synthetic definitions, not native acceptance evidence.
Each has the same source envelope, for example
`environment-a/project-a/thread-a/message-a`, a payload digest in `revision`, and
`truncated: false`:

```markdown
## Explanation
- Outcome: explicit Submit delivers the selected answer.
- Evidence: the original native request receives an acknowledgement.

## Research
- Finding: the terminal supports the selected send chord.
- Source: the installed terminal's documented input API.
- Open question: physical key delivery still needs a native check.

## Guide
1. Select the original question.
2. Review its supporting evidence.
3. Choose an answer and submit it explicitly.

## Completion receipt
- Outcome: source-revision validation implemented.
- Evidence: changed-source submission is rejected before dispatch.
- Remaining: verify the adopted client through its normal entry point.
```

These are four separate native message examples, not a required combined
template. Authors choose the structure appropriate to their material. Full
owner wording remains in the original source; a display title or concise visible
summary never replaces it.

A filled supporting Markdown example:

```json
{
  "threadId": "thread-a", "itemId": "message-a", "revision": "item-revision-a",
  "kind": "assistant_message", "truncated": false,
  "text": "## Release evidence\n- Fix: source-revision check added.\n- Check: stale answer rejected before dispatch.\n\n```mermaid\nflowchart LR\n  Review --> Submit --> Acknowledged\n```"
}
```

A filled completion receipt is the same native message envelope with, for
example, `text: "Implemented the revision check. Validation: focused recovery
checks passed. Remaining owner: verify native key delivery."`; that content is
read-only evidence and does not become a new queue item.

## Flowchart report controls (A4, B3)

Definition before implementation: a fenced `mermaid` block remains part of its
original native Markdown message. Its identity is the environment/thread/item
source key, source-content revision, fence offset and unchanged source bytes.
TermiFlow 0.2.9 at `cdd62fe1717aad26fe53ca1b0ad07454aa4f0e76` supplies the real
Mermaid flowchart parser, graph layout and Unicode rendering under MIT. SLT
continues to own terminal output, focus and scrolling. Project navigation is a
separate control and never uses the report diagram as its navigation state.

| Control | Behavior | Validation and retained source |
|---|---|---|
| Flowchart | Render supported `graph`/`flowchart` fences as terminal cells; vertically scroll a bounded diagram region. | Strict parsing; complete measured node and edge label widths are explicit. Parse warnings, unsupported syntax, source/graph budgets and viewport overflow show the source with the precise rendering limit. No silent 20-cell label truncation. |
| Source | Toggle the unchanged fenced source and inspect its source key/revision. Copy uses SLT's clipboard path. | The original Markdown and Mermaid remain available after failures and resize. These are read-only controls; they never send, answer or settle a thread. |
| Resize | Re-evaluate the same source for the new content-cell geometry. | Cache by complete source/revision, content digest, viewport geometry and observed renderer runtime inputs. Never reuse another source's rendering or treat a cache hit as freshness. |

Filled native report example:

````json
{
  "environmentId": "environment-a",
  "threadId": "thread-a",
  "itemId": "message-release-check",
  "revision": "native-message-revision-a",
  "truncated": false,
  "text": "## Release check\n```mermaid\nflowchart TD\n  Review[Review source revision] --> Submit[Submit explicit answer]\n  Submit --> Ack[Native acknowledgement]\n```"
}
````

The integration uses public parse/measure/layout APIs with an explicit
`Config`, avoiding TermiFlow's user configuration loader. TermiFlow's private
runtime still captures process environment. Behavior/debug overrides cause an
explicit source fallback; `COLUMNS`/`LINES` are included in cache provenance.
T3oxide does not mutate process environment or start TermiFlow's terminal/TUI.
This proves supported flowcharts only, not general Mermaid diagram support.

Submission uses existing `runtime-request.respond`, with the original command,
thread and request identities. The adapter re-reads and checks the full request
revision before the first dispatch, journals the exact command before sending,
and durably records the native sequence acknowledgement before returning
success. The returned answer acknowledgement identifies environment, thread,
request, source revision and original operation ID. Missing/mismatched
acknowledgement remains unresolved. Recovery is an explicit same-ID operation;
there is no automatic mutation retry.

## Project navigation and dependency evidence (B1, B3)

The graph has source-qualified project nodes (`environmentId`, `projectId`)
from the native inventory, including projects with no questions. Drilling into
one project replaces nodes in the same region with that project and its actual
question nodes (`threadId`, `requestId`). Returning to all projects retains the
selected question and supporting content. Tab focuses the scene; arrows select,
Enter/Space activates, and mouse hit testing resolves the displayed source ID.
Labels may abbreviate to fit the viewport; identity never uses their text.

Example: `environment-a/project-a` contains
`environment-a/project-a/thread-a/request-a`; selecting that question refreshes
its current source revision. A native inventory omission remains an explicit
incomplete-inventory status rather than proof that a project does not exist.

Dependency evidence requires the exact prerequisite and dependent question
revisions plus an original native source item/revision and reason. For example,
`request-a@revision-a` precedes `request-b@revision-b` because
`thread-a/item-dependency@revision-c` explicitly records that requirement.
petgraph retains the reasons, orders known prerequisites and exposes cycle
groups. Current native request offers expose no dependency edge/coverage
capability, so the active queue preserves native admission order and shows
`Dependencies · unknown`. Lineage, prose, local retrieval scores and issue
closure never fabricate edges or resolve a T3 request.
