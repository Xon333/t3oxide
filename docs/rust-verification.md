# Revised Rust/SLT integration — 2026-11-05

Current source integrates maintained Rust `58cb5f8b0dc4df27a2b414fa95903112a58783c8`
and relevant later preserved UX behavior into the isolated SLT client. The
active runtime is `subinium/SuperLightTUI@d4537d74f4b42188e06cb224fbfe7684397897fb`,
v0.26.0, with maintained vendored patch `ordered-activation-v2`. The checked
implementation at `dd11afc` includes persistent navigation Pins and exact
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
| Active SLT PTY | `uv run --with pyte==0.8.2 python tests/slt_terminal.py --binary target/release/t3oxide`: **14 outcomes** passed |
| Locked attribution | 290 upstream packages inventoried, including the local SLT base/patch and modified-file hashes; both upstream MIT notices retained; registry archives have Cargo checksums |

Actual toolchain: Rust/Cargo **1.99.1**, Node **24.21.0**, PTY Python **3.12.15**.
The manifest minimum is deliberately **1.92**, required by ascii-dag; this is not
a claim that a 1.92 toolchain was tested. The release is **8,933,104 bytes**;
SHA-256 `e2a6b6c087ac3a1820aa417e1c7da4fa4cd7cb3e7f59aee8a36ca0ea8eccf4f1`
at `dd11afc`.
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
The correction below is source-checked; native confirmation and M4 activation
remain pending. No task-owned Failed lifecycle or exhaustive native generation
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
Unsettle when the source is absent from loaded rows. Native retesting is pending.

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

Independent consequential Sol review cleared `741d4c2`; the narrow shutdown-lock
fix has its deterministic regression and passing hosted checks. The navigation-pin
correction needs its consequential review, current-head hosted checks and focused
native confirmation; finished-client M4 adoption remains pending. The PTYs are
synthetic endpoint evidence; they never use a provider or certify a native Rex
chord. Internal tabs are supported. A supported draft-safe
native pop-out has not been established; no bridge or global pane binding is
introduced here.

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
