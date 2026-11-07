# Rust / SLT verification

## Internal fuzzy switcher — 2026-11-07

Search, Ctrl+F, the internal-tab add control, `/switch` and `/threads` now open
one compact fuzzy picker over loaded conversations. The existing matcher ranks
titles, project/environment labels and exact source IDs; selection opens the
original source through the existing internal-tab path. Loading, unavailable
and incomplete inventories stay labelled. Explicit native message search
remains available through `/search` and Commands, with its existing scope.

Final source passed **315 Rust tests** (300 library, 15 integration),
workspace/vendor formatting and both Clippy checks. Root qualification found
and corrected two gaps beyond the initial delegate regressions: entry from
Commands could leave typing outside the query, and vertical padding hid source
details at 40×12. Both have retained failing regressions and passing corrections.
Consequential review found two further integration defects, both reproduced
before correction: metadata refresh could restore the controller's old row,
and same-title Ctrl+F re-entry could retain list focus. The settled SLT highlight
now feeds back only under a matching source stamp; metadata rebuilds preserve
the exact environment/thread while query edits reset ranked selection. Explicit
Ctrl+F entry requests query focus. Real-App regressions cover query/list arrows,
loading admission/completion, reordering, stale surface rejection, exact
cross-environment attachment and queued typing/paste at all three sizes.
The fresh arm64 release is **9,635,808 bytes**, SHA256
`5a19636a0146fb3a26cf73dc2f5124b8c32d66dc35283bf19ff700dd6fd956a7`;
version and strict signature verification passed. No dependency changed; the
prior minimum-version check remains scoped evidence for unchanged Rust APIs.

The earlier controlled PTY invocation passed **19 checks** and retained 12 frames.
At 140×38, 70×22 and 40×12, keyboard navigation reached exact source details;
filtering duplicate titles by thread ID attached the intended fixture source
and restored the original tab draft on return. Fresh fixture subscriptions
verified exact thread IDs independently of clipped transcript headers. Empty
Enter was inert, and original draft/pending records stayed exact. Shared
Commands, focus, explicit native search, input ordering and restart checks also
passed. All seven product processes were normally waited with exit zero; the
tracked source set remained unchanged during that invocation.

The corrected release then passed three affected PTY scenarios at 140×38,
70×22 and 40×12, retaining six frames. An actual metadata read preserves a
non-first highlight; same-title Ctrl+F accepts queued typing/paste and attaches
the selected original source only after Enter. Drafts, nonempty pending and
pendingStops remain exact, with zero native mutation dispatches. All three
processes exited zero after normal waits, and source bytes remained fixed.
The first correction qualification stopped on an incorrect harness expectation:
`jthread-review` still fuzzy-matches the project-qualified review source. Its
exact query bytes were already visible; the corrected assertion passes without
a product change. Both invocations remain retained.

These are synthetic adapter/SLT checks and terminal-cell captures, using the
retained pyte underline compatibility and rasterizer. Small labels can elide;
full identity remains in scrollable selected-source detail. Earlier failed
invocations remain retained, including two incorrect transcript-text locators.
No installed native service, physical Rex, M4 or whole-product visual acceptance
is established. Both consequential review findings are corrected and verified;
historical R9 acceptance and the other open UI/native/backend limits remain.

## Finite Rex/SSH qualification — 2026-11-07

One real M1 Rex→OMV check used a synthetic source fixture and opaque target,
without a native endpoint request. It ran the maintained wrapper once, retained
the intended command exit **1** separately from wrapper exit **0**, and verified
exact binary stdout/stderr and both selected-source byte observations. Read-only
recovery authenticated the same original attempt. Wrapper teardown remains
unknown; wrapper status is not a command-success or cgroup-safety claim.

That qualification exposed three client issues. Layout admission used full
`block inspect` API output (69,553 bytes), exceeding the 64 KiB control bound;
it now verifies exact pane membership with the smaller maintained block listing.
Finished terminals may omit optional `child`/`foreground` fields, so closing now
requires a valid positive `last_exit` object and accepts either missing or null
process fields. Present processes, malformed/ambiguous identity and unknown
outcomes retain the pane. Finally, formatted creation JSON was parsed line by
line, losing early creation IDs. Streaming JSON decoding now checkpoints each
complete object across read boundaries before process exit.

All three have retained fail-before evidence. Independent consequential review
also found that newline recovery after malformed JSON could treat nested pane
IDs as creation evidence. A regression reproduced that path; control parsing
now fails as uncertain on malformed, truncated or non-object JSON without
resynchronizing. Already committed observations survive, while ordinary raw
subprocess output remains unchanged. No unrelated-pane closure was observed.

Final source passed **305 Rust tests** (290 library, 15 integration),
workspace/vendor formatting, both Clippy checks and whitespace checks. The
formatted-JSON regression holds its child open until the creation checkpoint
arrives, proving observation before exit.
The final fresh arm64 release is **9,618,736 bytes**, SHA256
`3cd63653702e0df0d4325bf1ac774287db254625cb8136ce9d7d518ba1eec536`;
version and strict signature checks passed. Dependency/helper/UI source is
unchanged, so the preceding checks and UI evidence retain their stated scope.
The prior minimum-version check is reused; these corrections add no newer APIs.

The original live check's automatic closure was **PARTIAL** because it preceded
the JSON correction. Its exact creation object was recovered from retained raw
CLI output; corrected source then authenticated a read of that same attempt and
closed only its observed finished child. The check was never rerun. All
task-created panes were closed after evidence retention; the original pane IDs,
active tab and keyboard focus matched the pre-probe layout. This is real
transport/byte-return and guarded-recovery evidence through an API driver,
not final-release App→Context→native-Send or physical UI acceptance.

An executing-host owner created only the absent private t3oxide state child
needed by the helper; its root-owned parent and existing entries were preserved.
No shared wrapper, launcher, policy, service or M4 installation changed.
The two integrated backend attribution attempts stopped on private harness setup
errors and produced **zero samples**. A fresh library/driver build alone is not
a measured baseline, bottleneck attribution or performance gain. Historical R9
acceptance and the earlier native/graph/lifecycle limits remain unchanged.

## Compact UI correction increment — 2026-11-07

The composer now shows its effective model/effort, including a source-qualified
next-message override. The corresponding picker edits that same selection.
Known activity has a quiet indicator; missing evidence stays unknown and Stop
keeps its separate admission rules. Completion sits above Prompt. Navigator
capacity accounts for its separator, Model/Effort menus use content-sized
viewports, and long catalog names can be revealed without changing native
indices. Commands absorbs keyboard help from the maintained shortcut document.
Pending actions retain their complete original payload, separate from drafts.
All four Context tools remain available in a 27-column panel and operate on its
selected source independently of the attached conversation.

The final source passed **302 Rust tests** (287 library, 15 integration),
workspace/vendor formatting, both Clippy checks and `git diff --check`.
The locked Rust **1.92.0** all-target/all-feature compile check passed before
the final dialog-height and Context-background corrections; those corrections
introduce no new Rust APIs. Unchanged adapter/helper/dependency checks are reused.
Consequential source review identified three defects, all corrected with focused
regressions: override loss on Model entry, waiting work classified as inactive,
and missing navigation/editor help. Runtime qualification additionally reproduced
and fixed clipped short-answer review text and transparent compact Context paint.

A fresh arm64 release is **9,618,784 bytes**, SHA256
`17adea3b6245d7749066ac1b58b1865d9a64f4cd96617bddd490e7e6242c2909`;
strict code-signature verification passed. One uninterrupted **full29 PTY run**,
including the mandatory blocked Context-read case, passed on the preceding
`0872b1320ae0f44fe4ac134dda5e60515f80dd530cce23d280432c1826064b0d`
artifact. Its only subsequent production change fills the Context overlay
background. Final-artifact affected focus/Context open-close checks and corrected
captures passed. All **195 Python assertions** remain retained; harness changes
update labels, unambiguous Model locators and explicit Context navigation.
Earlier failed attempts remain evidence rather than being counted as passes.

Independent inspection covered 16 representative terminal-cell frames at
140×38, 70×22 and 40×12. Final-artifact captures verify opaque Context paint,
keyboard access to Commands guidance, and scrolling to original pending-action
identity/message/thread fields. Raw ANSI is retained; the renderer uses pyte
0.8.2 with an explicitly recorded single-underline parser adapter. These are
controlled fixture captures, not physical Rex screenshots. All task product
processes exited normally; protected drafts, pending actions and native bytes
remain conserved by the scoped checks.

This increment does not establish native endpoint Send/Apply, actual Rex→OMV
finite-check return, installed M4 behavior, full visual acceptance or a speedup.
Historical R9 remains **NOT_ACCEPTED** and native/graph/lifecycle evidence remains
**PARTIAL**. The native R7 Enter-pair cause remains **OPEN/INCONCLUSIVE**.
The fuzzy source switcher, on-demand full graph and persistent guidance policy
remain carried work. All original human feedback is retained outside Git.
Separately, a fixed retained-R9 Store A/A control passed its predeclared gates;
that result measures neither this integrated application nor an accepted gain.

## Rex-oriented source increment — 2026-11-07

The accepted focused A/B presentation, selected-Library Capture/Records/Resume,
and finite-check evidence/finding/follow-up paths are implemented in source.
The corrected source passed **290 Rust tests** (275 library, 15 integration),
workspace/vendor formatting, both Clippy checks and the Rust **1.92.0**
all-target/all-feature compile check. Shared-boundary checks passed **259 Node
checks and five measurement-tool tests**. The finite-helper fixture suite passed
**16 tests**, including historical reads after cwd/tool changes, authentic old
helper bytes, delayed wrapper cleanup and closed display pipes with capped
retention. No expected failures are counted as passes.

The cross-environment first-send guard remains intact. A refused follow-up
preserves exact drafts, caret, overrides and state; explicit return to the
original verified target admits one acknowledged native fixture send. Late or
reversed evidence observations cannot retire native acceptance. Partial and
final envelopes have distinct Context revisions and retain both exact payloads.
Interrupted helper publication cannot leave a partial hash-addressed final asset.

The caller's wait is bounded separately from Fleet's cleanup lifetime. If that
wait expires, the original Rex task process continues draining capped wrapper
output; the result and pane remain uncertain until actual exit. This is not a
cleanup-time or teardown guarantee. Recovery uses the originally pinned helper
and current host Python for read-only inspection, without requiring the old cwd
or live wrapper/interpreter identity. Fresh execution retains strict preflight.

A fresh isolated-target release build and its final focused rebuild produced an
arm64 binary of **9,618,752 bytes**, final SHA256
`92709c9e5cc2979aedd9bc14ad9cbdd63716ac73700977b9ffbd1a42d0569c18`.
Version and strict code-signature checks passed. The release UI passed all
**28 PTY checks** with all **195 assertions** retained on the preceding
`a897335c7077e94190863fe083c5d3fa6cc0798ad9a0ff84c67523e7cacb034a`
artifact. Subsequent changes affect finite-check recovery admission and embedded
helper authentication, with focused regression coverage; SLT presentation/input
source is unchanged. The two consequential reviews are complete. Root corrected
the final two residuals: post-ACK partial-result reads and surviving asset
publication links. Acknowledged partial attempts now admit read-only recovery;
asset links are allowed only for authenticated helper identity, while request,
record and blob inputs remain single-linked. The minimum-version compile check
predates these small corrections; they introduce no new Rust APIs. Read-only M1 Rex
metadata confirmed the local Unix endpoint and block-inspection shape. No new
Rex pane, actual OMV wrapper check, native endpoint Send/Apply, M4 install, merge,
service change or performance measurement has occurred in this increment.

All **62 earlier feedback records** and **65 history entries** remain in the
private carry-forward map. This increment does not close the remaining UI/UX
hill-climb cases or admit a backend performance gain. Historical R9 acceptance
remains **NOT_ACCEPTED**, with its native/graph/lifecycle limits unchanged.

## Active correction pass — 2026-11-05

The latest R8 source checkpoint passed **267 Rust tests** (252 library and 15
integration), workspace/vendor formatting, both Clippy checks and
`git diff --check`. Rust/Cargo **1.92.0** compiled all locked targets/features;
that check did not execute tests. Four production regressions failed before the
corrections and pass afterward: compact Context navigation, short-form action
placement, complete acknowledgement diagrams in the outer report scroll, and
source-tab focus retention. Actual forward/reverse Tab traversal and A/B return
preserve the Unicode interior caret, original uncertain operation, native bytes
and journal guards. The six-file correction also bounds the graph preview,
exposes compact navigation through existing controls, distinguishes active tabs
from keyboard focus and clarifies Model focus/Apply paint. All 195 strict Python
assertions and the 12 runtime inputs remain unchanged.

The prior R7 binary passed one full29 run, including the mandatory context gate
and 33 normal primary exits. Its later acceptance remained **NOT_ACCEPTED**:
the blind visual target was unmet, populated graph navigation had compact/focus
limits, and a physical near-concurrent Enter pair produced an extra fixture
setting operation with delivery ordering unproved. No source defect is attributed
to that pair. These results qualify R7; the changed R8 source still needs a new
immutable binary, functional/native/graph checks and fresh visual acceptance.
No correction-pass merge, deployment or M4 switch has occurred.

The compact Model integration source passed **254 Rust tests**, workspace/vendor
formatting, both Clippy checks and `git diff --check`. Rust/Cargo **1.92.0**
compiled all targets with all features; that check did not execute tests. The
production SLT-to-App fixture at 140×38, 70×22 and 40×12 exercises the painted
Model pointer target, a native display-name/action-label collision, native model
and option staging, batched Space/Enter, child Tab/BackTab/Escape, outside-click
dismissal and local favorite saving. Explicit Apply produces one exact pending
operation, matching acknowledgement and native fixture effect, preserving the
Unicode draft and caret. The pointer repair uses the SLT row container's returned
response so its hit rectangle belongs to the painted field. These are source
and synthetic fixture results; this candidate has no new release, PTY, installed
host, physical or visual acceptance evidence.

A fresh independent source review found queued Enter escaping local Model
dismissal and Space activating Model while × owned focus. Production SLT/App
regressions at 70×22 failed before correction for Tab, BackTab, Escape and
outside-pointer dismissal, and for Space on the settled close control. Retained
dismissal ownership now survives SLT's activation layout frame and expires after
input-free settling; later deliberate Enter applies once. The close control
retains its valid focus identity. The corrected source passed **259 Rust tests**,
workspace/vendor formatting, both Clippy checks, `git diff --check` and the
Rust/Cargo **1.92.0** all-target/all-feature compile check. The tests preserve
native bytes before Apply, exact pending/acknowledgement identity, one native
effect and the Unicode draft/caret. The independent review predates these fixes;
their subsequent validation was run by the implementer.

A subsequent focused regression passed through production SLT rendering and
App action dispatch: pointer-opened Model belongs to `composer-tools`, and Space
on the settled × restores that invoking owner. One deliberate Shift+F6 returns
to the composer without moving its actual rendered caret; typed `<caret>` lands
at the original interior position and removal restores the exact Unicode draft.
The original uncertain operation, other-source draft and native bytes remain
unchanged. The Python harness now makes that explicit keyboard return only
before its post-close, outer-Escape and applied caret probes. All 192 pre-existing
assertions (including the 152 in the 18 unchanged functions), generic helpers
and the pending-context gate are preserved; three close-checkpoint assertions
bring the current total to 195. The source passed **260 Rust tests**,
workspace/vendor formatting and both Clippy checks. No new minimum-version APIs
were introduced; the prior 1.92.0 compile evidence is reused, not rerun.

One minimal 70×22 production-entry PTY diagnostic reused the frozen R5 binary
(SHA-256 `05a4b938cb27a4c5b645c76f8f707abebf70e278a76ba553c0f5c202cbd22283`).
It reproduced the absent editor caret while the invoking tools owned focus,
then passed coordinate equality and actual marker insertion/removal after
Shift+F6. During the caret proof, full durable state, Unicode drafts, the original
uncertain pending record, native bytes/mode and journal remained exact; effects
and payloads stayed zero. Normal exit persisted only the tab’s previously absent
display title, preserving its source identity. The single product child exited
with status 0. The earlier compact Model group failure remains retained. That
PTY group, its Apply/acknowledgement and restart/resize cases, and the full-29
recheck were not run in this lane; installed, physical and visual acceptance
remain separate.

A retained-state real-App regression now covers 70×22 → 140×38 → 70×22
with the same SLT `AppState`, an actually resized `Buffer`, activation boundaries
and production layout observation/action dispatch. Before correction, the first
settled narrow frame lost follow and painted the transcript head with a downward
overflow cue. Rebinding the existing controller scroll intent when SLT invalidates
geometry on resize preserves the visible tail and exact durable reading anchor.
The regression also checks complete durable-state equality at the close checkpoint,
intentional wheel scrolling through resize/source return, explicit Latest, and
manual/following positions after reopening the state file in a fresh App. Unicode
interior draft/caret, another source's draft, a nonempty original uncertain
operation, native bytes/mode and the absent host journal remain preserved.

This source passed **261 Rust tests**, workspace/vendor formatting, both Clippy
checks and `git diff --check`. The new regression has retained failing-command
source manifests and logs; its final before-fix run also has a complete source
archive. The strict 195 Python assertions and all runtime inputs are unchanged.
The retained R5 PTY failure remains unresolved at the binary acceptance boundary:
this lane ran no release build, product/PTY group, installed, physical or visual
check. The parent must build the corrected source before the next focused runtime.
The R6 strict full-suite attempt subsequently failed at the original
`focus_and_panes` assertion: Escape from direct Effort invented an unopened
Model configuration, so F3 remained confined to a modal. A production SLT/App
regression reproduced that return-path failure before the controller repair.
An explicit option-origin fact now distinguishes direct entry from a real Model
child without duplicating staged selection. Direct cancellation restores its
invoking controls; a real child still returns one level to its staged Model.
A source-bound direct dismissal tail retains one deferred Enter through SLT's
close/focus activation frame, then expires after settling. It neither reopens a
catalog read nor applies or sends. The original pane sequence, literal draft,
actual keyboard caret return, complete durable state, uncertain operation,
native bytes/mode and absent journal are checked. Direct Escape/Tab/BackTab/
pointer plus Enter, unchanged-value acceptance into Apply review, and a later
real child return are covered. Nested child Escape and outside-pointer
dismissals also retain their queued Enter across the return to the exact saved
configuration. The existing real SLT/App regression fails before that repair
and passes afterward, including Escape+Enter and pointer+Enter, while preserving
its selected native values and single later deliberate Apply. A changed source
or configuration stamp clears that retained input ownership.

This source passed **263 Rust tests** (248 library and 15 integration),
workspace/vendor formatting, both Clippy checks, `git diff --check`, and the
Rust/Cargo **1.92.0** locked all-target/all-feature compile check. The strict
195 Python assertions and all runtime inputs remain exact. The nested-child
failure and its contemporaneous source archive remain preserved.
The prior focused runtime pass qualifies only its old binary; R6 full-suite
acceptance remains **FAIL/PARTIAL**. No new binary, PTY/full-suite, native,
graph, physical, visual or M4 acceptance was run. The parent owns a fresh build
and strict runtime validation.

The correction pass is still in progress. The second frozen release (R2) is
`t3oxide 0.11.0 · Rust / SLT`, SHA-256
`99bbe9f5738c3927cb62e5c677422f13b08eb2b66347139e1086e7215e565e57`,
9,187,104 bytes on macOS ARM64. Its 317-file source manifest has SHA-256
`b0fd9ff72cdbc865e7c1837a8db47f5d16243957d2f3b1699490231444b1701a`.
These results qualify that frozen source, rather than later working-tree changes.

| Evidence | Result and boundary |
| --- | --- |
| Source checks | 199 Rust tests, workspace/vendor formatting and both Clippy checks passed. Official Rust/Cargo 1.92.0 compiled all targets with all features; this was a compile check, without test execution. |
| Shared checks | Unchanged dependency hashes retained the passing 259 Node and 5 Python results. Six affected installer tests were rerun after the NOTICE change; revised Python inputs passed syntax checks. |
| Production-entry PTY | One uninterrupted run passed all 26 checks in 43.572 seconds on the exact release. Source bytes, modes and file set, runtime inputs and protected payloads matched before and after. These are synthetic endpoint results. |
| Physical Rex | Controlled-fixture draft paint/resize, independent tab drafts, whole-draft clipboard bytes, modal guards and explicit answer submission have passing evidence. Direct Settled→Unread pointer activation failed; Run→Unread restored the list. That defect is retained for a later-source correction. |
| Native answer | A fresh two-question submission received its original source/request/revision ACK, with both exact answers returned and no pending operation. Physical review navigation retained the answers. Final handback verified clipboard items, types and bytes and preserved prior session labels, windows and layouts, with an independently reconnected client's owner-metadata change recorded separately. |
| Visual review and adoption | Three fresh blind reviewers and a separate judge ranked R2 first against the preserved baseline and fixed references. The visual acceptance bar was not met; source/detail cues, graph framing, contrast and selection remain correction work. No correction-pass M4 switch has occurred. |

The corrected debug executable
`e11202eddf066bd97fa19cfed3274a7f19925d1b97cac93d27e711e5c0ca5b8e`
previously established all 26 outcomes across nine completed and 17 remaining
checks. Its combined attempts remain failed; the later release run above is the
single complete pass. Setup/selector diagnostics and physical failures remain
retained evidence.

The next source revision separately introduces the accepted bounded context-I/O
worker, navigator activation correction, further presentation corrections and
physically proved Copy draft shortcut metadata. Focused source checks passed:
13 worker regressions, 43 UI tests, three project-graph tests and lib/tests Clippy.
An earlier checkpoint passed seven Store-preservation and 53 lifecycle tests.
Fresh consequential review found native question retirement closing a pending
Context view without invalidating its read token. The new regression failed
before the correction, then passed for replacement and cancellation while
retaining the original answer draft, composer and protected operations. Source
lookup also rejects a changed original digest or reference; its expanded
regression failed before the check and passes afterward.

The worker owns context reads and writes separately from draft storage and drains
accepted outcomes before joining. It adds one in-process thread; a Store scan's
duration remains unbounded. These source results do not establish latency or
resource savings.

The next frozen debug executable has SHA-256
`faad35bbb9bf9e6958dd365d31b9bd87b7de5b25450c77dc15fddeac09456610`.
Its 318-file source snapshot passed 221 Rust tests, workspace/vendor formatting,
both Clippy checks and an official Rust/Cargo 1.92.0 all-target/all-feature compile
check. That minimum-version check did not execute tests. Unchanged shared inputs
retain the passing 259 Node, five Python and six installer results.

All 28 debug PTY outcomes completed across scoped runs. Three earlier attempts
remain failed: exact selectors stopped on the annotated Copy draft row, an
incorrect Unread-lane expectation and a Context-title/composer substring
collision. Narrow harness corrections preserved the product, generic matching
and state assertions. The final context-only continuation passed in 9.868 seconds:
child-local exact-file fault injection proved the read was blocked while real
Esc, an independent exact Unicode draft save/restore and an internal-tab source
change remained usable. After release, only the newly requested source painted.
Durable/native/journal, caret, original pending identity and no-replay checks
passed. Navigator pointer checks separately passed two cycles in both populated
and empty Settled fixtures.

These are scoped debug results, not one uninterrupted 28-check pass. The debug
artifact's original source and runtime snapshots remain frozen; its runtime
harness selector migrations are recorded separately.

Subsequent UI source review found two pointer-navigation regressions: a
conditional scope control could inherit the question list's previous hit target,
and compact project navigation removed its clickable global return. Focused
regressions reproduced both before correction. Scope controls now retain stable
interaction slots, and global return remains clickable in compact layouts.
The corrected tests cover queued Down/click input at both responsive placements
and populated/empty project returns at 46×22 and 40×12 while retaining source,
form/report and unsent draft. The corrected source passed 223 Rust tests,
workspace/vendor formatting, both Clippy checks and the official Rust/Cargo
1.92.0 all-target/all-feature compile check.

These corrected inputs were subsequently frozen in R3 below. The earlier debug
and R2 results do not certify later UI changes; no M4 switch has occurred.

Fresh consequential review found stale form ownership after closing the last tab
and inaccessible keyboard scrolling in Recovery detail with choices. Both failed
their new regressions before the small corrections and passed afterward. The
copy test also established that its first command can initialize the current
reading bookmark; its baseline now establishes that bookmark through a separate
normal action, retaining full durable/native state equality around each copy.

### Third frozen candidate (R3)

The distinct R3 release is `t3oxide 0.11.0 · Rust / SLT`, SHA-256
`6674f76ed994592377f056e6c0cbdf70235ba7d1069a318b256ff6bd61e8b199`,
9,237,072 bytes on macOS ARM64. Its 318-file source manifest has SHA-256
`33aee3faee9b3e16a480814730229b411c473057a02c9473a66876a0ae88881b`.

| Evidence | Result and boundary |
| --- | --- |
| Production-entry PTY | One uninterrupted run passed all 28 checks in 59.500 seconds, including the mandatory exact-file context gate and Navigator pointer cycles in both fixtures. All 23 product children exited normally. Source, protected payloads, runtime and gate inputs matched before/after; all eight stable context snapshots retained full durable/native/journal equality. This is synthetic endpoint evidence. |
| Physical Rex | Controlled-fixture resize, exact clipboard/draft preservation, populated/empty Navigator cycles, search, commands, three distinct sends and an explicitly reviewed answer completed. The four operations received exact controlled-fixture ACKs; genuine-native ACK qualification on the final artifact remains pending. |
| Retained failures | Selecting an empty project replaced a selected report with an older retained Form. Compact global navigation retained a misleading project title. Earlier physical pointers did nothing until an unmodified Esc; the delivery/focus cause remains unresolved. A separate PTY diagnostic stopped on its startup precondition before pointer input. |
| Preservation and visual review | Clipboard items/types/bytes, drafts, prior session layouts and the named R2 window were restored. An initial unnamed transient panel was absent at handback and remains a recorded caveat. Three blind reviewers and the separate judge ranked R3 first; candidate scores span 7–8.5, below the acceptance bar. The judge froze its independent assessment before reading the council. No M4 switch occurred. |

The subsequent source revision preserves the selected detail when an empty
destination has no selection, without changing the report's original source.
It also labels the global graph at compact heights while retaining independent
Browse scope. Both regressions failed against the R3 code before correction and
passed afterward. The report regression uses actual Library and graph pointers,
an older retained question, exact report/source/revision, empty Browse counts,
global return and an unchanged Unicode draft. The compact regression checks the
global title at 46×22 and 40×12.

This later source passed 224 Rust tests, workspace/vendor formatting, both Clippy
checks and the official Rust/Cargo 1.92.0 all-target/all-feature compile check.
The minimum-version check did not execute tests. R3's frozen binary and evidence
remain unchanged; the two later corrections require a new artifact and affected
physical/visual acceptance.

The next UI revision improves selected conversation titles and shared source
labels, distinguishes a browse cursor from attached content, fits complete
compact tab/count controls, bounds the search query and separates question
progress from choice position. Library shortening affects display labels only;
the original content/source/revision remains available. Sparse graph spacing
uses the accepted layout library's public configuration while retaining the
existing viewport and shared paint/hit geometry.

Fresh source review found one P2 in that revision: a wheel event could hide a
source line and shift its interaction slot to another conversation before a
queued click. A regression under production SLT activation boundaries, with
controller Highlight acceptance, clicked A2's displayed source line and opened
A3 before correction. The source line now retains its interaction slot at zero
height when hidden; the same sequence opens only A2 afterward. This reproduced
defect is separate from R3's unresolved physical pointer no-ops.

After that correction, all eight navigator regressions and the integrated
230 Rust tests passed, with workspace/vendor formatting, both Clippy checks
and the official Rust/Cargo 1.92.0 all-target/all-feature compile check. The
minimum-version check did not execute tests. Retained-state resize coverage
includes 140×38, 100×31, 86×31, 70×22, 40×12 and wide restoration. This later
source still requires its own immutable artifact, production-entry PTY and
physical/native/visual acceptance.

### First frozen candidate (R1)

The first frozen candidate is
`t3oxide 0.11.0 · Rust / SLT`, SHA-256
`6b3ec23b109602ad376717210bf3bbe511c7267ec96ccdd82d8ab77fcd44ab10`,
9,203,856 bytes on macOS ARM64. Its source manifest covers 317 files and has
SHA-256 `befbe98e0b009d1f68ba719bf35bd23b495d97ebdb7486abf75e51ff077f2b40`.
Later source changes are outside that artifact's results.

| Evidence | Result and boundary |
| --- | --- |
| Frozen-candidate source checks | 185 Rust tests, workspace/vendor formatting and both Clippy checks passed with Rust/Cargo 1.99.1. Unchanged shared inputs retained the passing 259 Node and 5 Python results. |
| Production-entry PTY | All 24 checks passed on the exact frozen candidate. Binary, source, adapter, fixture, harness and packaged-file hashes matched before and after. This is synthetic endpoint evidence. |
| Declared minimum Rust | Rust/Cargo 1.92.0 compiled production and test targets with all features on a later, unchanged 317-file snapshot. Subsequent UI/vendor changes still need reconciliation. This was a compile check, without test execution. |
| Physical Rex with controlled fixtures | Resize retained the exact multiline Unicode draft and independent internal-tab drafts. Form validation, explicit review and acknowledged submission were observed. The editor also reproduced a hidden draft prefix above blank rows, and the review clipped its second answer. These remain failures for this artifact. |
| Physical Rex with native T3 | A disposable two-field answer received its original source ACK. Ctrl+Enter, portable Send and pointer Send each produced one distinct native dispatch/reply. Plain Enter inserted one newline; slash completion opened Help without dispatch. No pending operation remained in the disposable source. |
| Clipboard | Native source copy and normal Ctrl+A/C copied exact bytes. Cmd+A/C copied the draft only in the preexisting manual Rex editing mode; normal Cmd+A/C copied the terminal. The new whole-draft action needs physical verification on the next artifact. |
| Visual review | Three independent reviewers and a separate judge ranked the candidate first against the preserved baseline and fixed established-TUI references. The acceptance bar was not met; layout, spacing, controls and navigation corrections continue. |
| Installed adoption | No correction-pass M4 switch has occurred. The previous deployment described below remains historical evidence. |

Failed-first source regressions now cover the editor viewport, unresolved
inherited-content graph identity and keyboard access to complete answer reviews.
The production PTY suite gained an editor-paint check that correctly fails
the first frozen candidate and a whole-draft copy check. R2 passed both in its
complete release run and the controlled physical retest. That later evidence
does not change the first candidate's failed results.

Current limits remain explicit: metadata refresh covers a bounded loaded window;
definitive removal requires a complete first-page scope. Native dependency edges
and coverage are unavailable, so live synthesis remains unknown. Native answer
preflight has no server-side revision CAS. Local context can retain already-loaded
completed evidence automatically and retrieve its retained full source; this does
not scan unloaded transcripts or establish a complete inventory. No latency or
resource-saving claim follows from these checks.

## Prior SLT deployment — historical evidence

Current source integrates maintained Rust `58cb5f8b0dc4df27a2b414fa95903112a58783c8`
and relevant later preserved UX behavior into the isolated SLT client. The
active runtime is `subinium/SuperLightTUI@d4537d74f4b42188e06cb224fbfe7684397897fb`,
v0.26.0, with maintained vendored patch `ordered-activation-v2`. The checked
implementation at `cb8decd` includes supported Env routing and accurate browsing
notices, persistent navigation Pins and exact
post-action metadata, retaining the `c0cee63` shutdown-lock correction and
`529d9ec` submission/compact controls, `5941463` geometry/modal/provenance and
`b58644d` search fixes. No installed
launcher, GUI, configuration or T3 service was changed by these checks.

| Check | Current result |
| --- | --- |
| Formatting and whitespace | `cargo fmt --all --check`, standalone vendor fmt, `git diff --check` passed |
| Clippy | `cargo clippy --locked --all-targets --all-features -- -D warnings` and `cargo clippy --locked -p superlighttui --lib -- -D warnings` passed |
| Rust tests | `cargo test --locked --all-targets`: **148 tests** passed across the library, forms, protocol and SLT activation suites |
| Adapter/scripts | `scripts/check.sh`: **257 Node + 5 Python** passed, including syntax/compile checks |
| Rust documentation | `RUSTDOCFLAGS='-D warnings' cargo doc --locked --no-deps` passed |
| Release | `cargo build --locked --release` passed on macOS ARM64 |
| Active SLT PTY | `uv run --with pyte==0.8.2 python tests/slt_terminal.py --binary target/release/t3oxide`: **15 outcomes** passed |
| Locked attribution | 290 upstream packages inventoried, including the local SLT base/patch and modified-file hashes; both upstream MIT notices retained; registry archives have Cargo checksums |

Actual toolchain: Rust/Cargo **1.99.1**, Node **24.21.0**, PTY Python **3.12.15**.
The manifest minimum is deliberately **1.92**, required by ascii-dag; this is not
a claim that a 1.92 toolchain was tested. The release is **8,933,104 bytes**;
SHA-256 `f057821d5957676a89b09ee21e83a45d5590c6da1f86f2babf20f94db6a2b4d2`
at `cb8decd`.
No current performance or resource-saving comparison is claimed.

The first hosted Ubuntu run failed two immediate state-reopen tests because a
parallel helper's inherited descriptor could retain the advisory lock after
the client owner closed. State teardown now explicitly releases its lock before
closing the descriptor. A deterministic duplicate-descriptor regression failed
before the fix and passes afterward: a live owner remains exclusive, owner exit
allows reopening, and closing an old inherited descriptor cannot unlock the new
owner. All 141 Rust tests, formatting and application Clippy passed locally after
this correction. All three hosted checks passed on `c0cee63`: Ubuntu Rust,
macOS Rust, and the native adapter/scripts. The corrected local release also
passed all 14 PTY outcomes. The native trial below used the separately frozen
`39c09d0` artifact, SHA-256
`8ec12829528ffeab999523c13526edc0330047318eff2b76161d1d7d541f0739`.

The release PTYs use the actual `run_async_with` entry point at the configured
60 FPS cap and synthetic native endpoints through the real adapter/journal.
One PTY write containing `first` + Ctrl+Enter Press/Release + `second` produces
payload `first` and next draft `second` before the delayed result. Accepted,
definitely rejected and lost-ACK cases retain the next draft across restart;
uncertainty retains the exact original pending record. No restart retries it.
Rejected attempted text remains source-qualified in Prompt recall, alongside
the existing host journal; it is never restored over the next draft.

The same suite verifies batched Ctrl+P + `help` + Enter and `/help` + Enter
without newline/send, pointer
Send around edits, keyboard and pointer modal Continue around edits and queued
tail, exact multiline Unicode paste/plain Enter, independent restored tab
drafts, source-backed question inventory and one explicit acknowledged answer.
Compact 86×31 controls/metadata remain visible and drafts survive 140×38 ↔ 86×31
resize. These are terminal-cell measurements, not a pixel-window acceptance.
TestBackend regressions verify compact/normal control hit targets, copy focus,
modal focus, opt-in/default batching, multiline paste, key release, Tab/BackTab,
click-on-release and bounded queue progress without fresh input.

SLT's default reader-level Ctrl+C exit previously ran before widget input.
The client now disables that default. A focused selected composer consumes
Control/Super A/C; source tests distinguish unselected and unfocused input.
Release PTY Control A/C Press/Release emits OSC52 with the exact selected draft
and keeps the process alive. Unselected Ctrl+C also keeps it alive. This does
not verify OS clipboard receipt, Rex key forwarding or native Cmd+A/C.

The corrected source binds source-sensitive actions to actual SLT focus before
capturing the mutation target. A focused runtime regression clicks the displayed
conversation's textarea after highlighting another row, then verifies Ctrl+T
settles only the displayed source. The opt-in runtime also isolates focus-changing
Control/Super, Escape and function-key activations before their queued input tail.

Search pages no longer own an already-open source's qualified project identity;
the active snapshot and native project inventory retain Mode B graph navigation
when that source is absent from search results. Additive persisted activity records
retain the last actually observed substantive event through lifecycle-only shells.
Reopen and accepted-page regressions verify read G0 → unopened G1 → running/null
remains unread until G1 is opened. Activity eviction atomically expires matching
read markers; a 1,024-source paging/reopen regression verifies eviction cannot
revive G0. Other environments retain their markers. Unknown generations are
never fabricated.

Native acceptance of frozen `529d9ec` exposed flowcharts painting above opaque
modals and reading jumps returning to the bottom after tab switches. Normal SLT
text rows now paint reports and styled graph runs in overlay order; regressions
cover opaque modal opening/dismissal, wide labels, selection colours and retained
prior-frame click identity. No vendor change was needed for either correction.

The active scroll binding now accepts deferred bounds before applying an explicit
jump, returns accepted feedback instead of its previous input tuple, and persists
the qualified anchor before tab actions. The production callback-order regression
covers both loaded-jump and Find/Go to: bottom → first source → immediate tab switch
→ return; it checks the exact saved row/digest/width/inner offset/follow state and
in-memory return tuple. Resize retains pending navigation until geometry is valid;
idle frames keep the anchor, content growth follows the tail, and manual wheel
scrolling leaves it. The frozen `39c09d0` native retest subsequently passed both
modal composition and immediate jump/tab/resize restoration.

The earlier frozen `529d9ec` M1 trial independently passed physical multiline
Unicode Mac selection/copy/paste in explicit Rex editing mode, exact title-result
reopening with its draft, model Apply, compact 86×31 controls, ordinary terminal
shortcuts after exiting the mode, and one exact physical Ctrl+Return dispatch.
That mode is manual and client-wide. The later frozen `39c09d0` trial also passed
physical Command Palette entry, focus return, visibly selected multiline Unicode
copy/paste, Escape exit and ordinary terminal shortcuts. No automatic editor
lifecycle is claimed. These results qualify their frozen artifacts, not
installed M4 adoption.

The `39c09d0` M1 trial additionally passed exact source/draft reopening after
environment/project searches, displayed-source Ctrl+T targeting, plain Enter,
slash completion, one physical Ctrl+Return without a pane, and a fresh native B
answer acknowledged by its original source. It reproduced one unresolved
acceptance finding twice: settling a pinned disposable conversation clears its
native pin, restore leaves it unpinned, and the client still shows Pins membership.
The navigation-tag correction below subsequently passed focused native
confirmation and installed M4 acceptance. No task-owned Failed lifecycle or exhaustive native generation
coverage was completed; source checks remain the evidence for those paths.

Navigation Pins now use the T3-owned `t3oxide:navigation-pin` tag. One explicit
tag command changes navigation intent without changing native shelf pins,
settlement, snooze, read state or run state. Existing shelf pins are not migrated.
Marker-only changes qualify the source revision even when `updatedAt` is stable.
Pin discovery precedes pagination and includes settled sources and children.
Writes are enabled only for the source-verified nightly `4179` tag contract;
unknown releases retain reads. The source reuse record identifies that contract.
The new client sends additive `navigation-pin` / `navigation-unpin` actions;
older `pin` / `unpin` requests retain native shelf behavior. A shared-adapter
regression verifies older clients cannot accidentally change the navigation tag.

Acknowledged actions return a complete, exact-source row from one authoritative
snapshot. The client replaces retained metadata without resetting search, scope,
drafts or reading positions. A failed post-read keeps the authentic acknowledgement
and exposes stale metadata; it cannot retry the mutation. Focused Rust/Node
regressions cover pin/read/run/settle/restore/unpin, marker-only changes, lost ACKs,
post-read failure, unsupported releases, older pages and another environment.

Consequential review of the pin correction found lane changes during loading,
marker refreshes using an older search, and volatile metadata in durable ACKs.
The correction queues the selected lane even while a read is running, invalidates
the latest queued/in-flight query, and requires matching query/search/Pins scope
before merging loaded rows. Durable receipts now contain only stable acceptance
fields; fresh metadata decorates the response afterward. Focused regressions
exercise both read-order races, all three merge-identity dimensions and two
helpers acknowledging one operation with different authoritative metadata.

The focused `ee6098e` M1 trial confirmed real marker persistence through
settle/restore and restart, native shelf separation, explicit Unpin and exact
absent-page targeting. It exposed Ctrl+T choosing Settle after settled Unpin
removed the displayed source from Pins. The shortcut now reads the same retained
active-source summary as mutation dispatch; its focused regression verifies
Unsettle when the source is absent from loaded rows. The frozen `61dc5d0` M1
retest passed: one physical composer Ctrl+T produced exactly one native Unsettle,
with no extra Settle, and retained tags, shelf fields, draft, reading anchor,
query and scope. Its disposable fixture was restored and task client exited0.

The journal also accepts a validated legacy decorated ACK with the same stable
acceptance fields. It checks the original operation/payload binding and any
optional source/action, preserves the existing receipt, and never redispatches a
saved acceptance. Mixed-helper and conflicting sequence/source/action checks
pass; older helpers themselves retain their historical behavior.

Existing passing coverage retains source/revision-qualified acknowledgements,
same-byte newer drafts, cancellation with fresh unsaved answers, graph
keyboard/mouse/Unicode/clipping, TermiFlow drawing and context provenance,
reopen/history/budgets/degradation/recovery inputs. No performance or latency
budget is inferred from these regressions.

Independent consequential Sol review cleared `741d4c2` and the final pin, refresh
and compatible-receipt changes at `61dc5d0`; all three hosted checks passed on
`61dc5d0`. The narrow shutdown-lock fix has its deterministic regression and
passing hosted checks. Later Env routing and notice corrections retain those
unchanged source results and have focused release-PTY coverage. The PTYs are
synthetic endpoint evidence; they never use a provider or certify a native Rex
chord. Internal tabs are supported. A supported draft-safe
native pop-out has not been established; no bridge or global pane binding is
introduced here.

Installed M4 acceptance used the normal launcher and original profiles through
both execution-host routes. Frozen `61dc5d0` passed A/B source navigation,
Markdown/flowcharts, opaque modals, native question-draft/acknowledged-answer,
physical manual-mode Unicode copy/paste, newline, slash-command Enter, one
Ctrl+Return dispatch without a pane, mouse Send, search/source reopening,
independent drafts and a no-flags normal restart. Ordinary inactive terminal
copy and the existing Cmd+Enter pane action also passed. Each send/answer has
its original durable native receipt; the disposable fixture completed with no
pending requests or operations.

That trial exposed the sidebar Env action naming an unsupported command.
`1b63b95` routes it to the existing environment-scope action. The exact old
release failed the new click-to-dialog PTY regression; the correction passes.
Physical normal/compact Env checks then passed on M1, and the normal adopted M4
client correctly filtered both hosts and All while retaining its attached source
and draft. The test noticed a stale footer saying All sources after an individual
host selection. `cb8decd` refreshes that notice after the effective scope changes;
the release PTY now selects a host and checks its actual name. Native filtering
was already correct. This last wording correction has source/PTY evidence,
without a separate physical retest.

M4's normal release pointer was explicitly swapped only after M1 acceptance;
previous releases remain available. The final `cb8decd` wording-only executable
was subsequently published through that pointer with its exact hash verified;
its adapter and native behavior are unchanged from the physically checked build. Eight drafts and 26 reading positions,
including all seven original drafts and 24 original positions, are preserved.
The bounded recall list rotated four old entries during test sends; after all
test clients closed, a guarded selective cleanup restored the exact original
24-entry history and retained the test history, original snapshots and native
ACKs separately. No unrelated fields were restored over newer state. All task
clients exited0, owned tabs closed, and clipboard, focus, bounds and input methods
were restored. Protected sessions and power guards remained untouched. Separate
Rex/configurator deployment owns later terminal/network configuration changes.

Precise limits:

- Native respond has no revision CAS. Adapter preflight checks the full original
  offer before dispatch; it cannot provide server-side atomic revision checking.
- Current native offers expose no dependency-edge/coverage capability. petgraph
  retains source-qualified reasons/cycles when provided; active coverage is
  **Unknown**, with native admission order retained. Lineage/prose/rank never
  fabricates a dependency or a human obligation.
- TermiFlow renders Mermaid flowcharts only, without per-node source colours.
  Unsupported syntax/configuration, ambient behavior/debug overrides, viewport
  overflow and rendering-budget limits retain original source. Explicit Config
  avoids home configuration; observed process inputs qualify cache identity.
- Local context captures completed non-excerpt source items explicitly. It does
  not ingest transcripts automatically or claim a complete conversation.
  File/directory fsync and recovery checks establish the syscall boundary,
  not sudden-power-loss behavior of Apple storage. Damaged-store totals are
  exact for safely parsed evidence and explicitly incomplete.
- Native shell pages and project inventory are bounded with incompleteness
  visible. Compact shells can omit unseen mid-run replies; no fabricated activity
  timestamp or count is substituted for missing native evidence.

The historical 2026-11-02 record below is retained unchanged as evidence for its Ratatui input. Its test totals, binary digest, PTY captures and comparative resource measurements do not describe this SLT revision.

---

# Rust implementation verification

Verified on **2026-11-02**. All recorded source, protocol and synthetic terminal gates passed. These results do not certify an installed user environment.

## Inputs and build identity

- Existing-client baseline: `e73caacd673d1a9bbceea8a7ed240b2a068ba5a7`.
- Reviewed T3 contracts: `ac8dce0cc3abd8a337f2d806b5082a6834b9b81a`; see [contract evidence](rust-contracts.md).
- Execution host: Linux x86_64; Rust/Cargo 1.99.1, Node 24.22.1, Python 3.12.14. The isolated baseline used Go 1.26.10.
- Rust release: `t3oxide 0.11.0 · Rust / Ratatui`, 3,324,744 bytes. SHA-256: `5f2554961bad5b942fae76c07dc4e26fa6e68c79cc9ef6579805d91b684a8721`.

Rust used the checked-in release profile (thin LTO, one code generation unit, stripped). The isolated unchanged baseline used `CGO_ENABLED=0`, `-trimpath`, `-ldflags='-s -w'`. The new application has no Go runtime.

## Completed gates

Commands, counts, binary identity and artifact hashes are in [checks.json](evidence/checks.json).

| Gate | Result |
| --- | --- |
| Rust formatting | `cargo fmt --all --check` passed |
| Clippy | All targets passed with `-D warnings` |
| Rust tests | **81 library + 3 integration** tests passed; [full log](evidence/rust-tests.log) |
| Companion and scripts | **237 Node + 5 Python** tests, syntax checks and compilation checks passed; [full log](evidence/adapter-and-scripts.log) |
| HTTP/WebSocket/stdio recovery | Four SIGKILL cut points for each of Stop, approval and question passed; [log](evidence/loopback.log) |
| Release and package | Linux release and packaged `dist` binary passed with identical SHA-256; [package log](evidence/packaged-build.log) |
| macOS source checks | All targets compile-checked for ARM64 and Intel; these are not Mac runtime, linking or signature checks |
| Real PTY acceptance | **41 recorded outcomes** passed on the release binary; [complete data](evidence/terminal-evidence.json) |

Real PTYs exercise both modes, control bytes, Unicode paste, mouse/resize, durable drafts/forms, external editing, requests, Stop, empty/messaged launches, model/options/overrides, history and host failure/reconnect. They also verify filtering, preview and server search without retargeting, loaded-text find/copy, local project/rank controls and partial task filing resumed through durable receipts.

Lost-acknowledgement recovery uses the original ID/payload with **one native effect**; disconnect/restart never resends automatically. Lifecycle tests cover newer identical drafts and late acknowledgements after source changes. PTYs exposed and corrected legacy Crossterm Ctrl+4/5 decoding for Ctrl+\/Ctrl+].

## Same-host resource comparison

Three fresh processes per client used the same synthetic native environment, active conversation, 140×38 terminal, three scheduled stream updates and 1.5-second idle window. Values below are **median (minimum–maximum)**. Tree RSS includes the Node companion.

| Measurement | Rust release | Pinned Go baseline |
| --- | ---: | ---: |
| Client RSS, KiB | 5,444 (5,412–5,684) | 29,004 (28,840–29,188) |
| Process-tree RSS, KiB | 47,768 (47,556–47,992) | 71,332 (71,176–71,380) |
| First transcript observed, ms | 156.84 (156.61–157.57) | 209.56 (208.94–213.52) |
| All three stream updates visible, ms | 516.71 (515.17–520.19) | 570.13 (569.01–623.12) |
| Idle client/tree CPU, ms | 0 / 0 (all samples) | 20 / 20 (all samples) |
| Idle terminal output, bytes | 0 (all samples) | 0 (all samples) |
| Live processes | 2 (all samples) | 2 (all samples) |
| Executable size, bytes | 3,324,744 | 15,941,794 |

In this workload, median client RSS was **81.2% lower**, and summed process-tree RSS was **33.0% lower**. These are bounded fixture measurements. RSS can count shared pages more than once; it is not macOS physical footprint. CPU resolution was 10 ms, so zero recorded ticks does not mean zero CPU work. Screen observations poll every 50 ms; filesystem caches were warm. Stream-ready time includes three intentional 100 ms fixture updates. These samples establish no physical paint, production provider, SSH, battery or large-history performance result.

## Reproduction and genuine host requirements

Reproduce with the commands in [contract evidence](rust-contracts.md#verification-fixtures); use `--baseline` for the isolated Go comparison. [Developer](evidence/developer.txt) and [AgentBox](evidence/agentbox.txt) captures contain synthetic data. These are terminal-text captures, not physical screenshots or paint measurements.

Installed authentication, actual SSH routes, provider execution and native terminal behavior still require the machines that own those resources. macOS target checks do not replace those checks. Updated companion files enable `inclusiveThreadList`; older protocol-compatible helpers may omit collapsed children from loaded metadata. Native server search remains separately available when advertised. No installed T3 service, productive thread, user configuration or user state was accessed or changed by these fixtures.

The R8 pointer-tab real-App regression reproduces a blocked Ctrl+Enter Send before the correction and passes after it: pointer activation restores composer focus, while keyboard arrows retain the conversation-tab composite. The fixture verifies the original source, complete Unicode draft and interior caret before Send, one pending operation and matching acknowledgement/native effect with the exact original text, and preservation of the other draft, independent uncertain recovery and existing host journal. The retained keyboard and A/B tab-cycle regression also passes. This is source-fixture evidence only; a fresh binary, full29, native, graph and visual acceptance remain pending. R7 native Enter-pair ordering remains qualified OPEN without an established causal source defect.

R9 adds five real-App SLT regressions with activation boundaries. The baseline reproduces the retained 40×12 form hiding its review action and a nonempty Unicode composer leaving no transcript body. The correction keeps Open form and Open in A visible with context off/on, preserves exact request/source, draft and interior caret, independent pending operations, native bytes/modes and an acknowledged host journal, and rejects changed, unavailable, submitting, uncertain and acknowledged offers. Compact transcript decoration yields to body rows; context geometry settles before scroll feedback. The complete graph focus route and measured-divider drag pass as positive conservation checks: arrows select without activating, Tab away/back retains graph paint, Enter selects the original Empty project identity among duplicate Atlas labels, and the default 30-cell/manual 42-cell widths survive resize. These are source-fixture results; new-binary, native, full terminal, visual and integrated acceptance remain pending. R7 native Enter-pair ordering remains OPEN/INCONCLUSIVE.

The 2026-11-06 keyboard and divider corrections passed **275 Rust tests**
(260 library and 15 integration), workspace/vendor formatting, both Clippy
checks, the locked Rust/Cargo **1.92.0** all-target/all-feature compile check and
`git diff --check`. The retained keyboard regression covers focused scrolling,
source/geometry rebinding, manual/follow intent and context changes. The new
real-App divider regression fails on unchanged production: the requested
42-cell navigator is allocated only 41 visible cells, losing its right border
and starting the main pane one cell early. Rounding the native split's grow
weight upward preserves the complete navigator, one-cell divider and right-pane
border; paint rounding is excluded from keyboard preference deltas. Default 30
and manual 42 survive the 140→86→70→40→140 resize cycle; actual Tab traversal
checks divider arrows at 140 and 129 columns, and resize cancels a captured drag.
The regression preserves exact Unicode drafts/interior carets, manual/follow
intent, nonempty independent pending and pendingStops, the complete durable
state, native bytes/modes and acknowledged host journals. Prior tests remain
unchanged; contemporaneous fail-first archives and logs are retained. The
274-test checkpoint and immutable R9 full29 are baseline evidence; runtime
inputs and strict 195 assertions are unchanged. No binary was rebuilt or run
in this source lane. New-binary, targeted runtime, native, graph and visual
acceptance remain pending; the historical native Enter pair remains
OPEN/INCONCLUSIVE. Optimization remains separate and held.
