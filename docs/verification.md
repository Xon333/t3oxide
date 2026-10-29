# Verification

## README native captures, 2026-10-29

The four `docs/screenshots/native-violet-*.png` images were captured from the installed Go client in Terminal on M1 Pro. The app used `--theme violet` with `TERM=xterm-256color`, `COLORTERM=truecolor` and `NO_COLOR` unset. An earlier launch inherited `NO_COLOR=1` and rendered monochrome; it was gracefully closed and only the capture client was relaunched. No T3 service was restarted.

Sources were native window PNGs at 1316 × 934, with 68 pixels of title/tab chrome cropped to produce 1316 × 866 images. These are native 1× logical-display captures, without artificial upscaling or recolouring; Retina/2× capture was not obtained. The Developer browser, theme picker, AgentBox inbox and Commands views were visually reviewed. Lists were filtered to the single public “Public repo update” task; no transcript was opened. These images demonstrate those visible surfaces, not Send, approval, tool-detail or recovery acceptance. Earlier synthetic fixtures remain separately named.

Mode switching for capture rewrote client appearance settings, then returned to Developer default with the saved violet theme retained. Profiles and drafts were not written. The themed client was left running.


This public record describes software behavior and compatibility. Private deployment inventories, machine identities, access routes, and delivery locations are maintained separately and must not be committed here.

## UX refinement, 2026-10-27

Version `0.7.1-bubbletea` implements the [UX refinement](ux-refinement.md) against packet revision `af58afb69d38653eb26170c8ce1b933cf5c7c0f9`. All 215 supplied source-manifest entries matched the immutable baseline before implementation. The pass changes Go presentation/event routing and its regressions; adapter, client execution, wire/config, shared scripts, module files, notices/licenses and retained Swift source are unchanged (104 relevant baseline files compared byte-for-byte).

### Source and disposable-fixture verification

The final source check passed on Linux amd64 with Go `1.26.10`, Node `24.22.1` and Python `3.12.14`:

- `bash scripts/check-go.sh`: gofmt check, all six Go packages, `go vet ./...`, and the complete race run. The integration package passed in `47.897s` normally and `65.011s` with race detection. Tests exercise the actual unchanged Node adapter against disposable simulated T3 servers, direct Bubble Tea programs, and the assembled executable through a POSIX PTY. They do not contact a productive environment.
- New regressions cover source-bound sidebar More activation through the actual filtered list; page/attachment retirement; metadata inspection without attachment; explicit Create; same-model option retention; required-answer clearing; compact settings/questions; search/caret and detail/copy restoration; nested prompt retirement; both saved operation lanes; rendered toolbar/mouse agreement; visible/source-qualified context selection; pane removal; and search opened from navigator/context/activity.
- The short-wide `170×12` regression reproduces a one-row body. Context falls back to the existing local review instead of accepting an invisible activity selection; resize preserves the draft, attachment, operation and activity identity.
- Existing causal receipt, independent Stop, exact input/recall, request revision, history, adapter journal and config compatibility checks remain in the passing full suite. Integration readiness now waits for real Commands/Conversations frames instead of the same words in persistent controls. Changed title, progress and launch traversal expectations retain exact-input, target and dispatch-count assertions.
- `go mod verify` and `git diff --check` passed. No module version changed. `scripts/check.sh` was not run because adapter/shared-script/Swift code was not changed.

An initial sandboxed suite was blocked by the environment denying loopback socket creation. The permitted fixture run exposed outdated heading/tab-order assertions and a dialog readiness ambiguity; these were corrected and the five affected program tests passed before the final full run. A focused rerun also corrected a test's optional-question label and an extra reverse traversal introduced while diagnosing readiness. These are recorded as failed intermediate checks, not silently counted as successes. Focused source review found and fixed search routing from a retained invoking pane and the one-row context case before the final check.

### Portable build and render evidence

`bash scripts/build-go.sh` passed for Linux amd64 and, with `GOOS=darwin GOARCH=arm64`, for a Darwin ARM64 cross-build. `file` identified ELF x86-64 and Mach-O ARM64 respectively. Linux `--version`, `--help`, and local `capabilities --json` passed; explicit nonexistent config/state paths remained absent. The version output is `t3oxide 0.7.1-bubbletea · Bubble Tea v2.1.0`.

| Portable build | SHA-256 |
| --- | --- |
| Linux amd64 | `0baf49f3f48c12acee0ec5c38ff67d66bdc4f983b8e68a9f16f5a1e22741269c` |
| Darwin ARM64 cross-build | `1df00ae41bda6341bfc58217656b6df9fa77a8f3b06ad5404f9af8123d1e79a0` |

Fifteen synthetic fixtures render the actual `Model.View()` into bounded ANSI/text/cell grids and reproducible offscreen PNGs. They cover normal/wide workspace and browser, Commands, launch, model settings/choice, question/approval, result, error, `40×12` settings/question and `60×16` independent saved-action/Stop recovery. Visual inspection found reachable primary controls and distinct browse/attached scope; compact copy may wrap or ellipsize, with complete Details still available. These captures establish model-cell layout only, not a native terminal, physical-key, GUI, clipboard, accessibility or novice-usability result.

### Native source verification and delivery limits

On 2026-10-27 the return was integrated into the real macOS checkout at the stated baseline after all 215 baseline hashes and the return checksums matched. All 221 returned source hashes matched after applying the patch. Native `scripts/check-go.sh` passed formatting, all six Go packages, vet and the full race run. A native Apple Silicon build with Go `1.27.2` passed `scripts/build-go.sh`, ARM64 Mach-O identification, version, help, offline JSON capabilities and strict signature verification. The Go linker signature is ad hoc; no notarization is claimed. The native executable SHA-256 is `ff71610f178c1d3f043db04c847b0696415dc72c8da652096b30567688d5b6f5`. The offline capabilities check used an isolated empty home and created no state.

### Installed-client delivery

The same native executable above was published on two existing Apple Silicon clients: a verified same-directory temporary executable and atomic rename on the build client, then one complete 35-file bundle transfer through the required verified SMB share and an atomic swap of the receiving client’s existing release/current symlink. Native ARM64, strict signature, version and the complete executable hash matched after publication. Notice/license hashes matched the reviewed bundle. Existing prior releases and the receiving client’s launcher, command symlink and legacy app remained intact. No client process was terminated.

The installed executable on each client passed read-only identity/authenticated-list doctor and capability checks against the existing host route: companion protocol 1, orchestration protocol 2, T3 `0.1.3-nightly.20261027.3754`, adapter revision `0e7346ccd9427c06161e222319e718e6bd70887f`. All eight installed adapter modules matched the unchanged source baseline; no adapter deployment was required. Checks opened only short-lived metadata helpers, without attaching a productive transcript.

Existing Go state locks were acquired nonblocking for publication preservation checks. Profiles, Go drafts and recall sidecar, appearance, Swift drafts and lock-file hashes matched before/after publication and after the installed read-only checks. Recovery records remained in place; no state copy, migration, retry or restoration occurred. Host journals, services, active jobs, provider settings and legacy rollback were not changed. Private paths and preservation receipts remain outside Git.

No GUI, physical-key, mouse, clipboard, accessibility, productive-write or performance acceptance is claimed from this delivery. Native source tests and synthetic captures retain their separate scope.

Historical evidence below remains as supplied and was not relabeled as newly executed.

## Terminal workspace and t3term reuse, 2026-10-24

Version `0.7.0-bubbletea` is implemented against the supplied t3oxide `ed43546c175e6d33467aaa89dae2503bb969b99f` and t3term `d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e` baselines. All 380 input-manifest entries matched before and after work. [The architecture/reuse decision](reuse-t3term.md) records the exact ports and alternatives. The portable implementation pass accessed no live installation or personal state; the subsequent native adoption is recorded separately below.

### Native adoption, 2026-10-24

All supplied package hashes and all 215 source entries matched. The source was integrated without runtime changes. A fresh Apple Silicon build with the existing Go 1.27.2 toolchain passed native version/help, ARM64 architecture and strict macOS ad-hoc signature verification. Its accepted SHA-256 is `da262d52239b05c87bd335b55b8debb53a67ecc5bd7135a970f674972d2f5ce3`. The downloaded cross-build's quarantine blocked its initial launch; the original artifact and its portable evidence retain their original identity. No notarization or Developer ID signing is claimed.

Four actual Ghostty clients used disposable fixture state and the actual Node adapter. Direct launches followed the actual 60×16, 80×24, 120×35 and wide layouts; an earlier nested `script` PTY dimension mismatch was a harness issue. Native checks exercised Commands/browser shortcuts, focus, environment selection while retaining attachment/draft, settings staging and exact Apply, source-bound New and cancel/reopen drafts, working-child expansion, settled search, list continuation, tool inspection, clipboard/selection and a multiline external-editor round trip. Retiring a focused activity item made Enter request a new selection without sending. A bounded Stop-after-editor reproduction produced exactly one interrupt for the reviewed run with queue hold. Its earlier missed key remains an unconfirmed timing observation, not evidence of a persistent shortcut failure.

Scope limits remain explicit: paste was ignored in the wide navigator's local filter, while physical typing and explicit search worked; dedicated browser-search paste was not exercised. Terminal dark and violet were exercised; native terminal light defaults were not. AX exposed terminal text but reported no selected-text value; native mouse selection was visible. Streaming reflow retained the documented item key and within-row visual-line offset, rather than a word position. All four clients exited with status 0 and restored canonical, echo, signal and other terminal fields; strict saved flags differed by the kernel `PENDIN` bit on direct exits, so byte-for-byte terminal restoration is not claimed. One later chained harness action continued after a focus refusal and sent a synthetic fixture message; it was retained as a harness error, not a productive action or product failure. The optional question-form native path from that batch is unclaimed.

Existing Mac and Linux hosts running `0.1.3-nightly.20261023.3579` passed verified environment identity, protocol 2, authenticated bounded reads, live model descriptors, recent/earlier activity and source/revision-qualified item inspection. Existing Node versions were 24.21.0 and 24.13.1. The eight-module helper plus NOTICE was installed through the native installer as revision `d96e5f18d15ea9a1`; configuration fields, previous releases and journals were preserved. Installed clients on both Apple Silicon Macs passed version and read-only checks through their existing local/SSH profiles, with activity inspection advertised.

Disposable copies of both clients' actual saved state and recall sidecars passed a focused 0.7 load/save/reopen check, retaining drafts, launch preparation, presets, options, browser preferences and pending records. Immediate preservation comparisons found no unexpected changes. Client promotion used the existing atomic executable/release routes and retained the previous 0.6 client and helper releases. No productive provider action, service restart, account change, state reset or automatic mutation replay was performed. No fleet resource, energy or end-to-end latency improvement is claimed.

Executed on Linux/amd64 with the official SHA-256-verified Go 1.26.10 toolchain and Node 24.19.0:

- All six Go package suites passed, including actual Node adapter, Bubble Tea program and assembled executable/PTY tests. The full normal run had 768 passing test/subtest outcomes and no failures/skips; integration passed in 42.715 seconds. Three final CLI presentation tests and their cases were then added and the complete CLI package passed again in 0.511 seconds.
- The **complete settled-source race run** passed all six packages: **776 passing test/subtest outcomes, 394 top-level tests, zero failures/skips**. Integration passed in 62.918 seconds. No data-race report occurred. `go vet ./...` and the formatting check passed after the final source edits.
- The complete Node suite passed **182 tests**, including 14 activity cases and seven deterministic scheduling cases, with no failures/skips. The environment used `node --test --test-isolation=none --test-reporter=tap tests/*.test.mjs`. Adapter/installer JavaScript and shell syntax checks passed.
- The supplied Python HTTP/WebSocket/stdio fixture passed all **12 SIGKILL recovery cases** across Stop, approval and question dispatch/acknowledgement boundaries, including fresh-helper exact retry. All **five measurement-script tests** passed.
- `go mod verify` passed and `go mod tidy -diff` produced no dependency diff. Go module pins and sums are unchanged. The pinned `govulncheck@v1.9.0` scan found **zero reachable vulnerabilities**; it also reported seven advisories in imported packages and nine in required modules outside the detected reachable call paths. This is static call-path evidence.
- Linux/amd64 and Darwin/ARM64 release builds passed with `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false` and `-s -w`. Linux version/help/offline-capability smoke checks passed. File inspection confirmed Mach-O ARM64; the Go linker emitted its ad-hoc code signature. No Darwin execution, native `codesign` verification, Apple signing/notarization or installation is claimed.

New integration evidence verifies no eager output reads, exact source-qualified lazy inspection, revision differences, bounded output/frame preservation, independent activity history, zero mutation/subscription effects for metadata reads, and actual rendered tool clicks. Sidebar filtering preserves the attached history/draft, and only explicit opening switches the attachment. Existing source-New preservation initially exposed a missing explicit draft-save boundary under faster updates; the source boundary now flushes the attached draft, with the original assertion retained and passing under race detection. The old wide full-Browser test now enters through Commands because Ctrl+O intentionally focuses the new sidebar; all its original More/Help/Refresh assertions remain.

The actual `View` generated and passed cell-width/height assertions at 60×16, 80×24, 120×35 and 170×40. The PNGs are renders of those synthetic ANSI cells, not native terminal screenshots. Existing theme definitions and all Swift source/tests remain byte-for-byte unchanged. Native key/mouse/AX behavior, installed-host endpoints/authentication and state-preserving adoption remain in [NATIVE-REMAINDER](../NATIVE-REMAINDER.md).

Sandbox restrictions initially blocked loopback sockets and public module downloads; the isolated tests and downloads succeeded with the permitted execution escalation. There is no remaining portable build/test blocker. Earlier native and installation evidence below retains its original version; none is relabeled as 0.7 acceptance. No resource, battery or end-to-end latency benchmark is claimed.

## Interaction and state correctness, 2026-10-24

Version `0.6.0-bubbletea` consolidates model/options into one staged settings review with searchable field choosers and one Apply. Picker sizing uses the available panel rows. Commands index canonical names, aliases and bound Copy/normal-recovery/Stop-recovery variants. Ctrl+P opens Commands by default; `/keyboard legacy` retains editor Ctrl+P. Browser and settings surfaces name the attached Stop target; browsing never retargets Stop.

The pass adds revision-bound question Back/Next and inline required validation, complete source-bound launch drafts, offline local preset management, identity-based browser selection, lifecycle scope, snapshot observation time and bounded continuation. The quiet 24-conversation default remains. Runless native children use correlated provider activity without inventing a Stop run ID. Rendering admits monotonic completed frames within the same page and preserves row-key reading anchors through reflow and rolling-window updates.

Source checks passed on the existing Go 1.27.2 darwin/arm64 and Node 24.21.0 toolchains: all six Go package suites, vet and complete race runs; the 161-test Node suite; adapter/script syntax; module verification and tidy-diff. The actual adapter/Bubble Tea/executable-PTY integration package passed normally (95.948s) and under race detection (113.124s). Focused regressions cover slow-render liveness, page/theme/attachment retirement, anchors, 60×16/120×35 menu and form layout, exact staged settings and answer dispatch, custom multi-answer retention, legacy shortcuts and complete launch restoration. The final page-size metadata adjustment receives a separate client race/vet check; unchanged layers retain the complete-script result.

`golang.org/x/text` is pinned to v0.39.0 for GO-2026-5970 and Goldmark to v1.7.17 for the reachable GO-2026-5320 scanner finding. Existing license texts are unchanged; the license version index is updated. The pinned `scripts/check-security.sh` source scan reports zero reachable vulnerabilities; this is static call-path evidence, not an exploitability claim.

The ARM64 bundle is ad-hoc signed and version/help/strict-signature checked. These source and native PTY checks do not claim a new physical-key, Ghostty GUI, resource or latency benchmark. The accepted executable was installed on the build client and atomically published through an existing receiving client’s versioned-release/current route. All 34 bundle files, ARM64, strict signature and version/help passed; immediate/final preservation snapshots matched for configuration, drafts, sidecars, lock, launcher, legacy app and all previous releases. No receiving-client productive connection or GUI check was performed; no active client was observed or terminated. The preceding 0.5.2 release remains rollback. Owned staging was consumed and temporary source/publication helpers removed after receipt consolidation. Earlier acceptance below retains its original build identity.

## Lean browser loading, 2026-10-23

Version `0.5.2-bubbletea` defaults to at most 24 unsettled conversations per environment. Subagents enter the navigation tree only through explicit expansion, type selection or search; ordinary expansion lists working children. Search retains access to settled conversations. Metadata-only list requests no longer acquire a WebSocket ticket or connection. The existing T3 HTTP shell endpoint still returns environment-wide metadata to the adapter; this change bounds the client projection, not the upstream fetch. No measured loading-time improvement or new GUI acceptance is claimed. The earlier 0.5.1 acceptance below retains its original identity.

Focused checks passed: the 13 existing adapter-navigation cases, active-list checks with 192 settled conversations, explicit working-child expansion and settled search, no-WebSocket list behavior, and the Go client/CLI package suites. The ARM64 executable was built for the next installation. No full-stack or native interaction replay was performed for this narrow change.

## Further interaction refinement, 2026-10-23

Version `0.5.1-bubbletea` aligns browser controls with traversal, puts secondary actions and local Help in More, makes Title Enter focus Create without submitting, and reduces repeated status/editor/help text. Contextual Ctrl+P and reading-only `?`/`/` retain editor ownership. Known request offers remain available in history, and three semantic highlight roles keep their UI meaning across themes. The CLI has current portable examples, configuration-free explicit help and a bare-JSON guard. [Reuse](ux-reuse.md) records inspected upstream patterns and retained APIs.

The runtime source is commit `1643d0fb157b6354f1ff8bda0116edfe43c14792`. `bash scripts/check-go.sh` passed on candidate `a8095952…7b31e`: formatting, all six package suites, vet and the complete race run, including the actual adapter, Bubble Tea programs and executable PTY tests. The complete integration suite passed normally (101.701s) and with race detection (117.244s). An existing request-program driver now waits for the visible empty composer after an ACK; five consecutive focused race runs and the complete script passed with exact answer/effect assertions retained.

The accepted binary differs from that candidate in one production file, `internal/ui/view.go`, which budgets the compact full-page frame's borders and padding. Its strengthened regression first reproduced clipped corners at 60×16. After correction, the complete UI suite (2.742s), UI race run (18.943s), UI vet and all intuitive actual-program race checks (11.665s) passed. Independent reviews through the narrow frame correction reported no remaining must-fix findings. The unchanged client/adapter/wire/config layers retain the complete-script evidence above; prior shared-script and installed-adapter results keep their original release labels.

The final bundle reports `t3oxide 0.5.1-bubbletea · Bubble Tea v2.1.0`; ARM64 architecture, strict ad-hoc signature, version/help and exact 34-file notice/license checks passed. Binary SHA-256: `689ffd97e1e5bc901016486e8df7d3c55b79107bfe314b44d7ffea52b8449fbc`. All 49 runtime-manifest entries match the frozen source. Earlier native findings and successful cases retain their original build identities.

Before execution, the operator independently verified all 49 source entries and the exact 34-file bundle. The final record has 50 screenshots and 46 indexed actions: 42 final-build actions and four correctly labeled older-build retirements. No additional final-build remote effect, dispatch or uncertain operation occurred. The final four captures were inspected.

The final hash passed bounded synthetic native replay at 120×35 and 60×16: More → Help and compact Details each render one panel with all frame corners and complete right borders. Esc restores the exact More/browser queries; Resume preserves the Earlier page, draft, caret and selected bytes. All four owned clients reopened normally on the final binary, retaining the created conversation attachment. A corrected fixed-theme capture shows actual page-search highlighting with `Find 1/1`, supplementing the native filled/inverse selected-row, editor and Terminal search proofs; the earlier mislabeled command-filter frame is excluded from successful search evidence.

Unchanged native cases from `a8095952…7b31e` retain that identity: contextual Commands/Details/Search; Title Enter moving focus without dispatch, followed by one exact source-bound Create; verified pending offers and Ctrl+R throughout history/review cancellation; and a newly correlated acknowledged → responding → completed receipt. All four theme comparisons used process-only overrides and preserved saved appearance bytes. The earlier `634ff5` editor → quit/reopen draft recovery, full 60×16 Tab order and zero-additional-effect Cancel retain their original labels. These reused results are not new final-build timing/race or productive-account acceptance.

The accepted bundle was installed on a separate Apple Silicon client through one same-filesystem release promotion and atomic `current` publication. Staged and installed ARM64 architecture, strict signature, version/help and all 34 files matched the approved hash and manifest. Stable immediate/final double snapshots confirmed unchanged configuration, state, sidecars, lock, recorded absences, command symlink, launcher, legacy app and all five prior releases. The previous 0.5 bundle remains the immediate rollback.

No active client was observed before or after publication; none was terminated. No productive connection, receiving-client GUI session or service change occurred. Consumed staging, the temporary publication symlink and owned scratch were removed after durable evidence consolidation. No adapter update, installer or compatibility replay was performed. Receiving-client results cover installed files and nonproductive CLI smoke; the separate native terminal evidence above retains its own scope.

## Browser, launch and status refinement, 2026-10-23

Version `0.5.0-bubbletea` adds a separate metadata browser, one local launch review, per-environment appearance and correlated send status while retaining the Go/Bubble Tea pins, Node adapter boundary, versioned client protocol and separate Swift state. [Controls and launch scope](../NATIVE-GO.md) and [architecture](architecture.md) describe the behavior. Source checks, focused synthetic native acceptance, installed-adapter compatibility and client delivery passed within the separate scopes below.

### Source and build verification

The accepted runtime source is commit `f00fc612844423ac5a4f33163e520a5ef49345d7`. The complete scripts passed on the earlier candidate `a1c47d1a92eca9882fbc385803cdf1f3070f92e81a262b06a8fe17f325ba915a`:

- `bash scripts/check-go.sh`, exit 0: formatting, all six package suites, vet and the complete race run, including real Node adapter, Bubble Tea program and assembled-executable PTY integration.
- `bash scripts/check.sh`, exit 0: all 157 Node tests, adapter/installer syntax checks, Swift model typecheck and App.swift parse, compiled Swift model/transport checks, and all five Python measurement tests.

The accepted final binary differs from that source in exactly two production files: `internal/ui/model.go` corrects startup browser adoption, and `internal/ui/browser_launch.go` orders the exact offered launch controls before Create. The final 49-file source manifest verifies that delta. Adapter, client, wire, Swift and config production files retain the full-script evidence above. After both UI corrections, the complete UI suite, UI race run, UI vet, actual-startup/settings-order program race cases and affected integration race checks passed. Focused independent reviews reported no outstanding must-fix findings, including after the launch durability, run-ID authority and hidden-wheel fixes. The changed startup and launch paths also passed the final native replay below.

The release reports `t3oxide 0.5.0-bubbletea · Bubble Tea v2.1.0`. Mach-O ARM64 architecture, strict ad-hoc signature and version checks passed. Binary SHA-256: `d4f29983f72c38577d9e054d4fcea8f69f491682316030ebee8c1aacaba4af24`. The canonical bundle and native-test copy matched that hash; the duplicate test copy was removed after acceptance. All 34 release-manifest entries, including `NOTICE` and `LICENSES/`, independently matched the exact package contents. Source/release manifests and the earlier build's evidence are retained privately with their original identities.

### Focused native terminal acceptance

The final hash passed in four simultaneous Ghostty windows using disposable state, the maintained synthetic causal fixture and the actual Node adapter. One operator serialized input. All 49 source-manifest entries matched before acceptance; the receipt indexes 55 actions and 56 screenshots. These are synthetic runtime results, not productive installed-instance validation.

- Startup without `--thread` displayed the browser without a transcript/composer; opening an offered conversation adopted it. Deliberate Browser stayed open during correlated live output. Esc and Resume preserved the multiline draft, selection/caret and Earlier-history reading view.
- Portable Tab → Ctrl+P opened Commands. Filtering, arrows, highlighted row, detail/footer and Enter resolved to the same action; returning from Details retained the query. Colored current-page search displayed one readable match without exposed ANSI fragments or repeated rows, and contextual help retained its query.
- Launch review followed Message → Title → Editor → Model → Effort → Tier → Create → Cancel. A successful external-editor return followed by Ctrl+Q and normal reopen recovered the exact multiline first message. Local model/effort/tier choices produced no source setting effect; explicit Create produced one launch with exact title/message, provider/modes and existing-workspace binding. Controls remained reachable at 60×16. Explicit Cancel produced no additional launch or source effect. Title/settings persistence across process exit is not claimed; they remain session-local.
- Matched initiating-message and row run-ID evidence advanced the receipt from acknowledged to responding and completed. A genuinely pending question stayed authoritative and reviewable after ordinary send and cancellation of its review; that smoke sent no answer command.

Successful unchanged native evidence from the earlier `a1c…` build retains that hash: hidden checked profiles, tab query/selection/theme restoration, physical Ctrl+G/Shift selection and Option+E composition, Terminal/violet search, Earlier/Latest, exact offered approval decline, independent Stop, disconnect without replay and explicit same-ID retry. It is reused for those unchanged paths, not relabeled as new final-build native race coverage. The original baseline's recovery/Resolve evidence also retains its original labels.

Physical Ctrl+Space was intercepted by macOS input-source switching, and the attempted physical Ctrl+@ chord did not open Commands. Portable Tab → Ctrl+P worked on both builds. Terminal/global mappings and grants were unchanged. Hidden-wheel timing, ACK overtaking, stale attachment/delivery and pathological editor-byte permutations remain source/program/PTY evidence rather than new native race results. No productive-account compatibility, receiving-client GUI acceptance, resource savings or isolated app latency was measured.

### Installed adapter and client delivery

The reviewed adapters were installed on existing macOS and Linux hosts with each host's existing Node runtime. Both select content-addressed release `847fa47abf574eda`; each no-flags installer ran once and exited successfully. Stable before/after checks confirmed unchanged configuration bytes, environment identity, credential path and metadata, write setting, state/sidecars, journal files and rollback releases. Installer locks and staging were absent afterward.

Both installed hello probes reported T3 `0.1.2-nightly.20261021.3473`, client protocol 1 and orchestration protocol 2, with launch model selection and the existing model, override, runtime and navigation capabilities advertised. The reviewed installers' authenticated shell reads passed. These checks requested no productive thread list, watch, history, transcript or mutation. Existing helpers were left running and load the new modules on their owner's normal reconnect; no T3 restart or provider/account change occurred.

The accepted ARM64 bundle was installed on a separate Apple Silicon client through its existing versioned-release and atomic `current` route. The staged and installed binary matched the full release hash above, and all 34 files, including `NOTICE` and `LICENSES/`, matched the approved manifest. Architecture, strict signature and version/help checks passed before and after publication.

Stable immediate preservation snapshots matched the awake baseline. Client configuration, drafts, sidecars, settings, locks, recorded absences, launcher, legacy app and prior releases remained unchanged; the command symlink was preserved. No client was running or terminated, no productive connection was opened, and no service changed. Consumed staging and the temporary publication symlink were removed. This installation smoke does not claim receiving-client GUI or physical-key acceptance.

Historical installation results below remain attached to their original releases.

## Native component UX reuse, 2026-10-23

Version 0.4.0 keeps the existing Go/Bubble Tea pins and Node adapter boundary. [UX reuse](ux-reuse.md) maps the changes to inspected upstream APIs and behavior; [controls](../NATIVE-GO.md#controls) records the current keyboard contract. Swift source/state and adapter protocol/receipts are unchanged.

Source and disposable-fixture checks passed on macOS 27.0.1, Go 1.27.2 darwin/arm64 and Node 24.21.0:

- `bash scripts/check-go.sh`: formatting, all six packages, vet and the complete race run, including real Node adapter, Bubble Tea program and assembled executable PTY integration. The final pass includes the quoted-authorization and full-capacity prompt-recovery fixes.
- Top-level router regressions cover editor keys, palette reentry/context help, slash completion, literal paths/known-command text, invalid command retention, local interrupt/clear/exit and immediate typing after exact-draft acknowledgment.
- New program integration sends a literal absolute path and recovers a tab/CRLF/U+FFFD paste through recall into an exact canonical send. Existing uncertain-action, independent Stop, same-ID retry, offered request IDs and terminal-restoration checks pass.
- Config/client tests cover exact editor/recall bytes and targeted `.prompts.json` compatibility retention across previous-Go JSON rewrites and interrupted publications, including full 24-entry/512 KiB histories, protected retention limits, malformed copies and unchanged pending/Stop lanes.
- Error tests cover complete viewport/copy diagnostics, quoted/escaped credential fields and authorization tokens, separate exact human-text copy, preserved request editors and context-dependent multiselect help. Compact pickers retain query, current selection, selected count and portable completion under filtering.
- `go mod verify` and whitespace checks passed. No dependency versions changed. Adapter/shared scripts and Swift paths are unchanged, so their previously recorded checks are retained without a qualification replay.
- `bash scripts/build-go.sh`: native Mach-O ARM64 release; version/help and strict ad-hoc signature verified. SHA-256: `bb27c891abb2ad84690aaf25e52a391326008b6bc9c9232e88df0d74009a127f`.

Focused independent review findings were resolved and covered by regressions. The final review found no additional P1/P2 findings in its redaction and recovery scope.

### Native terminal acceptance

Actual Ghostty acceptance used isolated client state, local loopback mutations and an existing live connection restricted to read-only access. Broad keyboard/palette/composer acceptance first used an earlier candidate. The subsequent `a6402eec…02c0` candidate repeated affected exact-text, error, request and recovery flows. The final `bb27c891…a127f` build changes only duplicated modal footer hints; actual normal modal/detail and 60×16 filtered multiselect checks repeated that presentation and verified completion, independent Stop and local exit. The complete source/race/PTY suite above also passed after this final change.

- Physical control-key editing, palette query preservation, slash suggestion acceptance, literal paths/known-command text and invalid editable arguments passed. Physical Option word motion and decoded Alt events were checked separately; terminal configuration still determines physical chord decoding.
- Native clipboard paste, rejected-attempt recall and the external-editor lifecycle preserved tabs, CRLF and valid U+FFFD. Exact detail copy and fixture sending matched canonical bytes; acknowledgment restored immediate typing. Unsent exact drafts and settings survived exit/reopen.
- Populated requests, preserved-answer review and context help retained their state. Multiselect query, selected count and Continue remained usable at 120×35, 80×24 and 60×16, including zero matches. The final compact footer showed local controls once and kept Stop/hold and local exit visible.
- Full error display and diagnostic copy passed a synthetic definite rejection containing multiline 401 text and quoted/escaped credential values. All injected credential fragments were redacted while reported identity and recovery steps remained complete. Exact human-text copy remained separate. This did not recreate a real expired provider session or perform a new login.
- Fixture receipts confirmed no hidden replay, explicit retry with the original operation ID/payload, exact offered request responses and independent Stop with queue hold while request/filter state remained intact. Mouse wheel reading and native terminal selection passed; click-to-focus remains outside the supported interaction contract.
- Read-only live checks reported writes disabled. Original config, drafts, compatibility copies and journal hashes were unchanged. Task clients, fixture owners and their exact terminal windows exited; private mirrors and raw live captures were removed. Sanitized evidence and hash receipts are retained outside the repository.

Ghostty scripted control modifiers did not produce the intended control events, so physical accessibility events supplied those checks. Its scripted text path also removed U+FFFD before application delivery; native clipboard paste supplied that exact-text case. Neither automation limitation is attributed to the client. No isolated latency, resource savings, sustained-load, screen-reader, physical power-loss or novice-usability claim is made.

### Installed-client delivery

The final ARM64 bundle was installed on a separate Apple Silicon client through its existing versioned-release and atomic `current` symlink route. The installed binary, `current` and unchanged command symlink resolve to the full release hash above. Architecture, strict signature, version/help and notice/license contents passed after publication. Original profiles, credentials, drafts, compatibility-copy absence, settings, launcher, legacy app and three existing rollback bundles matched their pre-install hashes.

No active client needed termination, no productive connection was opened for the installation smoke check, and no service or provider configuration changed. Consumed staging and task scratch were removed. This delivery check does not claim new terminal/GUI or physical-key acceptance on the receiving client; the native terminal results above were collected separately.

## Primary Go client and Terminal theme, 2026-10-22

The Go/Bubble Tea client supersedes Swift as the primary documented installation and CI artifact. Swift source, build checks and separate recovery state remain available for legacy rollback.

The optional `/theme terminal` and `--theme terminal` inherit terminal-default foreground/background and basic ANSI palette colors. Code/diff highlighting uses a named Chroma style with its native `terminal16` formatter and no base text/background colors. The default remains violet; violet, cyan and lime keep their fixed palettes and Dracula syntax highlighting. No new dependency versions, color probes, configuration readers or theme watchers were introduced.

Checks passed on Go 1.27.2 darwin/arm64, Node 24.21.0 and Apple Swift 6.4:

- `bash scripts/check-go.sh`: formatting, all six packages, vet and the complete race run, including actual adapter, Bubble Tea and executable PTY integration.
- `bash scripts/check.sh`: 135 Node tests, adapter/installer syntax, Swift model/type/transport checks and five Python measurement tests.
- New regressions cover terminal theme CLI acceptance/persistence, Markdown and syntax using only basic ANSI/default colors, composer selections, filtered dialogs/pagination, fixed-theme round trips and unchanged drafts. Native Bubble Tea output confirms initial terminal mode does not set default colors and fixed-to-terminal changes emit foreground/background resets.
- `go mod tidy -diff`, `go mod verify`, shell syntax and staged whitespace checks passed. Chroma's existing pinned version is now a direct dependency.
- `bash scripts/build-go.sh`: ARM64 release executable, version/help, architecture and strict ad-hoc signature verified. Local build SHA-256: `97588b71f39a62718ad45cc6baf7be15d3bff4c16afc3656277afdc3d7420cd2`.

Plus subagents reviewed terminal color ownership, publication contents and the final implementation. These are source/disposable-fixture results; installed clients, productive conversations and terminal configuration were not changed. A fresh hosted CI run and new Ghostty visual acceptance are separate evidence.

## Earlier Go verification, 2026-10-22

### Terminal UX redesign

The subsequent [terminal redesign](terminal-redesign.md) retains the pinned dependency graph and all client/wire/config, adapter, Swift and recovery implementations. Prior full-stack evidence below applies to those unchanged layers. New checks on Go 1.27.2 darwin/arm64 were limited to affected paths:

- The complete `internal/ui` package passed after integrating the layout, theme, command lifecycle, rendering and filtering changes (1.260s). The three final `TestWorkspace` regressions passed after review fixes (0.349s): cursor-only composer resizing, help over exact requests without hidden editing, and completion hints while filtering multiple choices.
- Four existing Bubble Tea program tests passed against disposable loopback fixtures (14.399s): composer/theme/resize/quit, exact approvals/questions, model pickers/grouped navigation, and independent Stop during a request. Terminal teardown now also checks foreground/background color reset.
- `go vet ./internal/ui` and `git diff --check` passed. No full Node, Swift, recovery, race or fleet qualification replay was performed for this presentation change.
- The release was built once with `scripts/build-go.sh`; `--version`, `--help`, Mach-O ARM64 architecture and strict ad-hoc signature verification passed. SHA-256: `ff62cc1a383c38c5b041f120eb7503732806a17a4739e7f1ee155606cb676fb5`.

The rendering and dialog checks cover bounded Unicode display widths, real syntax highlighting, native table-layout amplification, stale query results, disabled actions and preserved exact IDs/text. The source-level checks did not send, stop or change settings in productive T3 conversations. Installed Ghostty visual acceptance and deployment are separate evidence.

### Prior full-stack baseline

The complete implementation was built and tested natively on Apple Silicon with macOS 27.0.1, Go 1.27.2, Node 24.21.0, Apple Swift 6.4.0 and Xcode 27.0. The Go 1.26.0 minimum and all dependency versions remain unchanged; the existing clipboard module is now classified as a direct dependency. See [Go adoption](../NATIVE-GO.md) and [source ownership](go-terminal.md).

Passed checks:

- `bash scripts/check-go.sh`: formatting, the full six-package suite, vet, and the full race run, including the actual Node adapter, Bubble Tea programs, and assembled executable in Darwin PTYs.
- `bash scripts/check.sh`: all 135 Node tests, adapter/installer syntax checks, Swift model typecheck and App.swift parse, compiled Swift model/transport tests, and all five Python measurement tests.
- `bash scripts/build-go.sh`: release ARM64 executable; version, Mach-O architecture and ad-hoc signature verified. `go mod tidy -diff` and `go mod verify` passed.
- `CI=true bash scripts/build-mac.sh`: retained ARM64 Swift app and ZIP; plist, ad-hoc signature, ZIP integrity, extracted signature and byte-identical payload verified. Swift source and tests remain unchanged from the supplied package.
- `python3 tests/loopback_fixture.py`, using a disposable virtual environment for its test-only `aiohttp` dependency: all 12 SIGKILL recovery cases passed across dispatch and acknowledgement boundaries for Stop, approvals and questions.

Focused regressions cover picker filtering/pagination, repeated expand/collapse, focus and details surfaces, whole-input paste rejection, presets, provider model-change restrictions, independent Stop, exact override acknowledgements, stale choices and recovery. The installer tests also verify paused locks/staging, modified releases, symlink configurations and failed publication cleanup. Darwin PTY restoration compares every persistent terminal flag and control character; it excludes only the kernel's transient `PENDIN` bookkeeping.

Read-only `doctor` checks passed through the existing local and SSH host profiles against T3 `0.1.2-nightly.20261021.3473`, orchestration protocol 2. A temporary client profile pointing at the new source adapter read the current live conversation with model selection, runtime evidence and lineage. The native terminal client opened its catalog-backed model controls on that same connection. Existing profile files, selected models, credentials, drafts and host recovery journals were preserved.

Mutation acceptance uses disposable simulated T3 hosts. This run did not install the new adapters into existing environments, change live conversation settings, or send/interrupt productive work. Physical clipboard behavior, a particular terminal/tmux combination, resource measurements and a fresh hosted CI run are not claimed. The workflow now includes the full Go checks and ARM64 terminal artifact alongside the existing adapter and Swift jobs.

The following sections are the **original Swift implementation's historical record**, retained for rollback context. They are not new native Go acceptance claims.

## Tested compatibility

- Native client: version 0.1.1, build 2; ARM64, macOS 14 minimum; built with Apple Swift 6.4.
- T3: `0.0.46-nightly.20261009.2861`, orchestration protocol 2, tested on macOS and Linux.
- Distribution: ad-hoc signed application; not notarized.

These are tested versions, not a guarantee of compatibility with future T3 nightlies. No resource or battery savings are claimed.

## Source and CI checks

Before publication of this clean source snapshot, CI passed the adapter and Mac jobs, including native model/transport checks, the ARM64 AppKit build, and artifact upload. Original run records are retained privately; this statement does not claim a new run on this repository.

Focused checks covered:

- Bounded keyed deltas, revision mismatch recovery, metadata replacement, UTF-8 byte budgets, and long upstream identifiers.
- Historical pagination and reconnect state, independent Stop capacity, uncertain-operation persistence, and RPC liveness.
- Native small-frame pipe reads, EOF, cancellation, blocked writes, stale connections, and descriptor/process cleanup.
- Durable journal publication, authenticated identity isolation, conflicting retry rejection, and Stop bypassing a stalled normal-send acknowledgement.
- Twelve HTTP/WebSocket/stdio fault-injection cases: Stop, approval, and question recovery at four dispatch/acknowledgement boundaries. Fresh helpers recovered exact operations without affecting later work or exposing tokens.

The loopback fixture uses a simulated T3 endpoint. Its results are separate from installed-instance acceptance.

## Installed-instance acceptance

Actual compatible installations exercised synchronized views and incremental updates, human-authored messages, provider questions and answers, queued Stop, explicit same-ID retry after interrupted acknowledgement, earlier history, live and historical reconnect, and idle Ping/Pong.

Native AppKit checks exercised text insertion/selection/copy, draft isolation, Earlier/Latest navigation, cached reconnect state, independent Stop availability, light/dark text appearance, and window/helper lifecycle. These controls were exercised in process against production AppKit source and real helpers; this is not a claim of external mouse/keyboard automation. The packaged application also launched successfully.

For actual question-answer and Stop recovery, a helper was killed after the server accepted the command but before local acknowledgement. A fresh ordinary helper recovered the original authoritative receipt; another fresh helper returned the same receipt. The operation outcome was not inferred solely from current UI state.

T3 services continued running during acceptance. Private host configuration and deployment receipts are intentionally excluded from this public record.
