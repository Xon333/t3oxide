# Verification

## M1 source integration for 0.9.0, 2026-10-30

The prepared native checkout reconciled to PR #2 at `6d581ae13e476b6aebaf5198b8b154270cdb52c0` without replacing later source. The owner selected `0.9.0-bubbletea`, then made Ctrl+Space the displayed Commands default after Terminal consumed Cmd+P as Print. The shipped Cmd+P alias was removed; contextual Ctrl+P fallback, legacy editor ownership and explicit manual overrides remain. Registry-driven hints, CLI help and current control documentation were aligned. Creation-destination documentation now includes the existing saved-pin precedence. No dependency, protocol, Swift rollback, installer or native service change is part of these corrections.

Checks ran on macOS **27.0.1**, Go **1.27.2 darwin/arm64**, Node **24.21.0** and Apple Swift **6.4**:

- `bash scripts/check-go.sh` completed formatting, all six package suites, vet and the complete race run before and after the version change and again after the Ctrl+Space correction. The final integration suite took 108.280 seconds normally and 196.096 seconds under race; the UI suite took 7.342 and 57.633 seconds. The last logging wrapper attempted to assign zsh's read-only `status` variable after the gate completed, so its numeric exit was not captured; the complete log contains all twelve successful package results and the completed vet-to-race transition. No stage was skipped or weakened. These are suite durations, not interaction latency.
- `bash scripts/check.sh` passed all **219 Node tests**, syntax checks, Swift model/type/parse and compiled macOS transport checks, and all five Python measurement tests. These shared inputs were unchanged by the version correction.
- `bash scripts/check-security.sh` reported **zero reachable vulnerabilities**. It also reported nine vulnerabilities in imported packages and ten in required modules that this code does not appear to call. This is not a blanket dependency-security claim.
- The final `bash scripts/build-go.sh` candidate after the Ctrl+Space correction reports `0.9.0-bubbletea`; help, Mach-O ARM64 inspection and strict ad-hoc signature verification passed. SHA-256: `fb13bd82f85cf7617921bfe5cf51d12b6adfd610c82644b20fad9c8e3a50cf93`. The earlier version-only `a9fe9a7659e5b5676dfbd545d489d2f5fd17b9724442998939dc96f598c45e13` candidate remains the identified input to unchanged native recovery and earlier physical checks.
- `CI=true bash scripts/build-mac.sh` passed the retained ARM64 Swift build and transport checks. Strict signatures, plist, ZIP integrity and extracted payload equality passed. Swift executable SHA-256: `3eca617852a0714984991513c578b4e0e2cfe4531dabec7e3c1d04f92eb7e1c4`.
- The maintained twelve-case HTTP/WebSocket/stdio SIGKILL fixture passed fresh-helper recovery for Stop, approval and question operations at all four dispatch/acknowledgement boundaries, including exact original identities and conflict rejection. `go mod verify`, `go mod tidy -diff` and whitespace checks passed.

The content-free adapter projection and delayed-Writable probes also completed with raw samples retained outside Git. Their boundaries are synchronous `flushView` and Node Writable callback completion. They do not measure physical input, terminal paint or production latency, and establish no comparative performance or resource-saving claim.

An authenticated source-adapter handshake against the running M1 server reported T3 **`0.1.6-nightly.20261030.3904`**, companion protocol 1 and orchestration protocol 2. The separately installed shell CLI reported an older nightly and was not used as the running-server version. Preview/search/pin advertisement and `nativeDefaultsWrite:false` alone do not establish native workflow acceptance. R16 remains the explicit read-only fallback described in [native refinement contracts](native-refinement-contracts.md).

### Disposable installed-server workflows

The source adapter subsequently exercised actual native operations against that running server with isolated client state and host journals. Two reviewed empty launches reached distinct native threads, worktrees and branches with zero runs, messages and requests. The installed handler completes workspace preparation asynchronously: an initial null placement was treated as pending, then verified through native reads without redispatch. Revision-bound pin/unpin received authentic native sequence receipts. Bounded preview remained non-live/read-only with no request/Stop authority, attachment, extra socket/ticket, journal change or shell-revision change. Explicit native search returned both exact targets and retained its incomplete-coverage label.

Both project and environment defaults reads passed. The exposed project fields inherited environment values; stored selection/options and mode fields were unchanged. Persistent save/reset stayed disabled. Native provider `auth.label` and `checkedAt` matched the maintained identity projection exactly; desktop-local identity stayed unreported. A fresh helper returned original durable receipts without a native mutation RPC, and closing only its owned socket caused fresh descriptor/ticket/socket acquisition without replay or changed thread/journal counts.

The native ARM64 acceptance client built at `6d581ae` (`f4cf496162b6218445ed5a28dc26e98dbb4c2c28d0c74b4bad788a3868c33c80`, reporting `0.8.2-bubbletea`) exercised the native operation/recovery code retained by these corrections. Later Go changes update the version and Commands binding/hints. Developer preserved an exact draft across exit/restart, then switched to AgentBox. A real two-item batch explicitly launched one reply-only task; it completed with the requested response. Cancel paused the second item before dispatch. Restart retained the same batch ID, original text, acknowledged operation/target and remaining human draft. No second batch item was dispatched.

An initial isolated copy of an original empty-launch journal, with only that copy's local ACK omitted, reconciled the exact original request to its existing live thread without launch/dispatch or duplicates. Original acknowledged journals were preserved. This initial check is controlled missing-local-receipt evidence, not network ACK loss. A reconstructed batch request had a different fingerprint and was never dispatched; no substitute request or ID was invented.

The remaining native gates subsequently passed on the version-only `a9fe…` binary, with its native operation/recovery code unchanged by the later Commands correction:

- A native-managed Codex Pro Terra medium Plan question was listed, read, answered and completed through supported native tools. While the question was pending, the adapter excluded that child from Working; the coordinator remained Working with `pendingChildRequest:false`, matching the installed shell evidence and the documented coordinator-owned question exclusion. Genuine provider-approval propagation remains unproven: the full-access runtime supplied no such approval, and none was fabricated. This does not certify cross-environment child attention.
- Real temporary scoped companion credentials rotated for the same temporary subject. Descriptor validation ran afresh, cached account labels cleared, a fresh socket authenticated, and an explicit identity read restored only reported telemetry. Revoking the old credential produced HTTP 401 before socket admission. No mutation replay occurred. All four temporary sessions were revoked, copied credentials removed, and productive host/token hashes remained unchanged. This tests companion credential rotation, not provider-account rotation.
- A private loopback fault transport dropped one actual native success response for a separately authorized empty launch after its exact pending operation and host journal were durable. Go displayed **May already be sent — Review saved action**. Pending bytes survived exit/restart. Explicit `/recover` reconciled the original target and journal, wrote the authentic reconciled receipt and cleared pending. The transport counted exactly one native mutation request throughout; the native thread had zero runs, messages and requests. No new-ID retry occurred. This is controlled actual-native network-fault acceptance through the Go PTY UI, separate from physical keys.

The first fault harness missed a compressed ACK and received a normal success; it is not counted as lost-ACK evidence. An experiment using a fresh empty journal was rejected before native dispatch because launch defaults had changed; that guard was not bypassed. The corrected test used a separate deliberately authorized original operation and its preserved prepared journal. All four additional disposable threads were settled, with no unresolved operations, live runs or requests. Their root/shared bindings created no additional worktree. The task's transport and clients were stopped; prior bound trees and journals were preserved.

Exactly three disposable threads were created and natively settled; none has active runs, pending requests or unresolved operations. Three clean native-bound worktrees remain for supported native retirement because the verified adapter exposes no bound-tree deletion operation. Exact resource identities, complete test state and receipts are retained privately; no unrelated concurrent thread was touched. Physical input and production performance remain separate from these native operations.

### External macOS Terminal acceptance

Actual external System Events keyboard and CoreGraphics mouse/wheel input exercised owned Terminal windows against disposable synthetic fixtures. The first functional session used the `f4cf4961…c33c80` native candidate above; a fresh focused continuation used the version-only `a9fe9a76…c45e13` candidate. Direct window PNGs and exact fixture command receipts are retained privately. These results are separate from the live installed-server workflows and do not claim human-operated input or terminal-paint latency.

Passed cases include metadata selection without attachment, independent Stop/queue hold over pending requests, explicit text-question Submit, mouse selection plus explicit approval Submit, Enter newline versus separate multiline Steer, portable Ctrl+] Send and Ctrl+\\ literal Send, exact tool-source inspection by external click, wheel scrolling, and direct-terminal reflow at 60×16, 80×24, 120×35 and 160×42. Editor caret and functional selection survived both mode directions. A fresh request-active mode round trip preserved the selected draft. Violet and Terminal palettes and filled selection were visually verified at 80×24 and 120×35 after removing inherited color-disabling variables from the test process only; no numerical accessibility-contrast standard is claimed.

Cmd+P opened Terminal's native Print sheet; Escape canceled it and nothing printed. This observation prompted the owner's subsequent Ctrl+Space default correction. Other physical Command aliases remain terminal-dependent and are not certified by those checks. Existing Automation/Accessibility delivery succeeded; no TCC database, OS keybinding or global input-source setting was changed. Owned windows/clients/fixture processes were closed, with the original user window and unique recovery evidence preserved.

The rebuilt `fb13bd82…50cf93` candidate received a focused external keyboard check, independently repeated by the release owner. Its visible hint is Ctrl+Space, but System Events Control+Space did not open Commands in this Terminal environment. Interception versus non-delivery was not established. Control+P opened Commands, Escape preserved the exact draft, and Control+Q exited successfully. The owner accepted this explicit portable fallback; no global input-source setting was changed to force a pass. Actual Bubble Tea NUL decoding and source routing tests pass, but they are not substituted for successful physical Control+Space delivery. Only the test-owned window/client/helper were closed; unrelated windows and source state were preserved.

A known LF multiline clipboard payload reached the client as CR-containing text in this Terminal setup. The client rejected the complete editor-normalizing insertion and retained the prior draft plus exact attempted bytes in recall. Explicit **Replace draft with exact rejected input** recovered those received bytes; one canonical Send matched them byte-for-byte with exactly one fixture effect. This establishes supported exact-text recovery/send, **not LF-preserving native clipboard delivery**. That session also observed a pending request action requiring explicit Reconnect before Send; this is retained as a focused acceptance qualification, not a proven normalization or dispatch defect. No hidden retry or extra mutation was observed.

The host-local capability helper checked four enrolled records and proposed input-hash changes only, with no generated drift. **Knowledge update pending under the owner's Obsidian hold:** no record was accepted, edited or activated. Its unaccepted plans remain outside Git. Native behavioral and physical acceptance are separate gates; source tests and signed artifacts do not substitute for them. No release, merge, active installation or fleet upgrade was performed by this integration worker.

## Accepted refinement, portable implementation, 2026-10-30

This source extends the exact `1bd8cfa9ff69b560e3b1bd8e77d9ef92cb5688f9` baseline with the accepted R01–R20 decisions, including 18a, and the P01–P08 responsiveness work. The original ZIP, source manifest and current upstream agree on all 271 baseline file contents and modes. Initial local extraction omitted eight executable bits; they were restored from the original ZIP before exact patch verification. No later upstream source was discarded. Dependency pins, release version and the separate Swift rollback remain unchanged.

The new portable evidence uses Go **1.27.2 linux/amd64**, Node **24.22.1**, Python **3.12.14** and Linux x86_64. It exercises production Model.Update/View, real Bubble Tea programs, the assembled executable, Linux PTYs, the actual Node adapter and disposable HTTP/WebSocket fixtures. The complete shared check passes 219 Node tests, syntax checks and five Python tests. The maintained twelve-case SIGKILL fixture passes exact fresh-helper recovery for Stop, approval and question operations at all four dispatch/acknowledgement boundaries. The final full Go/test/vet/race, build, dependency and pinned security receipts are recorded in the implementation return's `report/checks.json` with their actual logs, including intermediate failures and corrections.

The new tests cover plain-live/rich-final rendering and semantic reading anchors, zero unnecessary control-only work, bounded workers and rejected-body caching, exact modal geometry, initial descriptor reuse and synchronized authority, progressive multi-source navigation, native content search, pin preflight and demand-only preview. They also exercise creation-destination precedence, the scrolling read-only native-defaults fallback, exact empty-versus-whitespace launch semantics, staged delivery and same-value late-ACK safety, idle request presentation and local suppression, global original-ID recovery, Reviewed/Later acknowledgement, durable partial batch receipts and source/hash-qualified review positions. Keyboard and actual-drawn mouse checks cover 60×16, 80×24, 120×35 and 170×40 where applicable; compact controls/defaults also cover 40×12. Terminal/monochrome, Unicode clipping, exact payloads, unchanged drafts and zero unintended effects remain explicit assertions.

Opt-in performance fixtures retain content-free raw samples and separate measurement boundaries: direct Update and complete View, render work/result wait/install, actual helper probe/request stages, and PTY key admission through the first kernel write after a prepared View. The PTY boundary includes event-loop queueing and terminal output, but the first write may include renderer housekeeping and is not a pixel/paint timestamp. Fresh helper opening traces use separate Node and Go monotonic clock domains; timestamps from different domains are never subtracted. Independent baseline/after cohorts, baseline-derived regression budgets, delayed/offline source states, preview-disabled/enabled reads, allocations/RSS/GC and count assertions are in the return's `report/performance.md`. No native production, physical input, M1/M4 or battery gain follows from these fixtures.

Actual Linux xterm screenshots show the running target executable with the real Node adapter and maintained synthetic data. They are captured window PNGs, with originals, build identity, reproduction and image hashes in the return, not reconstructed application layouts. The supplied ten macOS baseline images remain separately labeled and unchanged. Neither set establishes productive native mutation acceptance, owner account/default identity or human physical keyboard/mouse delivery.

**Native boundary:** at pinned T3 source `a773dc38ac75177b6cd133d27df00ca0a83f9480`, `server.updateSettings` has no server-enforced expected revision/CAS or durable operation ID. R16 persistent save/reset/recovery therefore remains disabled with an exact reason and native Settings route; the full bounded project/environment read, source/inheritance, unavailable selection and this-launch controls work. Native pin uses a fresh adapter preflight plus native durable command ID, not server CAS. [Native refinement contracts](native-refinement-contracts.md) records the version-matched primary sources and supported fallbacks. Installed-version capability checks, target macOS signature/Swift/AppKit checks and physical acceptance remain separate from Linux evidence. The canonical capability helper is unavailable here, and the packet's Vault hold is preserved. No merge, installation, deployment, provider/account substitution or productive native mutation was performed for this implementation.

All dated installed/native receipts below apply to their original builds. They do not certify this source revision.

## Installed form and status refinement, 2026-10-29

After explicit owner authorization, the native ARM64 build from `b5308d3a33720b84a6087e01203a26a4a85d24cf` replaced the existing client on both Apple Silicon installations. Both installed commands match SHA-256 `0fd947a19a59ed99ed457a6df6d54ca02edb23e7938522b2fe6456b457c2921a`; the version label remains `0.8.2-bubbletea`. Architecture, strict ad-hoc signature, version/help and configuration-free capabilities passed before and after publication. Read-only doctor checks passed for every configured profile on both clients.

The direct executable was replaced atomically with its original retained for rollback. The receiving client's existing command symlink and launcher were preserved, and its content-addressed release pointer was switched atomically after all 38 bundle files matched their manifest. Immediate before/after comparisons found unchanged configuration, drafts, sidecars, locks and host journals. Neither client had an active window/process to terminate. Adapters, T3 services, provider settings, the separate Swift rollback and Vault records were unchanged.

These checks establish installed-file identity and authenticated metadata access. Physical mouse/wheel and Command-key behavior and productive live mutation/recovery acceptance remain unverified. The preceding source-only pending-publication statement below describes the earlier integration stage and is superseded by this installation receipt.

## Native integration of form and decision refinement, 2026-10-29

The returned implementation was verified against its immutable 265-file baseline and applied as a patch to the matching clean dev checkout. All 269 returned source files matched the return manifest's hashes and Unix modes before integration fixes. Four independent reviews covered request ownership, rendered geometry/AgentBox, regression quality and the return contract. One blocking regression was corrected: the structured approval form now displays each offered warning beside its option, wraps it within the form, and preserves the selected consequence while Submit has focus. Clicking warning text stages that exact offered option and cannot submit it.

Native source checks use Go **1.27.2** darwin/arm64, Node **24.21.0** and Apple Swift **6.4**. The check results below supersede the corresponding cloud-only pending checks; they do not establish physical terminal or installed-instance acceptance. Program mouse tests decode ordered input through `io.Pipe`; actual assembled-executable PTY tests provide separate terminal-lifecycle coverage.

Before the additional status-label refinement, `bash scripts/check-go.sh` passed formatting, all six package suites, vet and the complete race run on the integrated source. Integration took 87.376 seconds normally and 176.276 seconds under race. `bash scripts/check.sh` passed 196 Node tests, adapter/script syntax, shell parsing, five Python tests, Swift model typecheck/App parse, and the compiled Swift model and macOS transport tests. `go mod verify` and `go mod tidy -diff` passed unchanged dependencies. The native ARM64 build passed version/help and strict ad-hoc signature verification.

The warning regression failed against the returned decision renderer before correction. Its corrected UI cases passed across all four supported sizes, including a wrapped consequence that remains visible at Submit. The actual-program request mouse matrix passed under race in 24.435 seconds, preserving exact request/decision values, dispatch counts and source drafts; the complete final UI race run after word-wrapping correction passed in 60.917 seconds.

The owner additionally requested clearer thread-state indicators. Conversation and AgentBox rows now share bold, right-aligned symbol-and-word badges with existing operational, warning and error color roles. Failed, Waiting and Blocked are distinct; queued follow-ups are not hidden by a previous completed run; reviewed/unreported metadata never implies Done. Focus/selection, project identity, mouse target geometry and native state ownership remain unchanged. Renderer tests cover selected/unselected rows and terminal/default palettes at 22–34 columns, plus complete AgentBox views at 60×16, 80×24, 120×35 and 170×40. An initial combined run exposed title clipping from the wider badges and one assertion tied to the previous request label. The list panes now reserve the added badge width instead of taking it from ordinary titles; source-bound mouse opening and exact launch/request program tests passed again in 24.183 seconds. The retirement assertion retains its exact one-request check using the new visible Waiting label. No identity, payload or dispatch-count assertion was removed.

The final combined `bash scripts/check-go.sh` passed formatting, all six package suites, vet and the full race run after the status-layout corrections. Integration took 89.343 seconds normally and 175.358 seconds under race; the UI package passed in 6.967 and 61.629 seconds respectively. The unchanged shared layer retains the native Node/Swift results above. The final native ARM64 build passed version/help, Mach-O inspection and strict ad-hoc signature verification; executable SHA-256: `0fd947a19a59ed99ed457a6df6d54ca02edb23e7938522b2fe6456b457c2921a`.

Physical mouse/wheel and Command-key delivery, installed-instance publication and disposable live mutation/recovery acceptance remain unverified for this source change. No installation, service restart, productive native mutation, capability-backend change or Vault write was performed.

## Portable form and decision refinement, 2026-10-29

This source implements F01–F03 from the intuitive-flow packet against exact upstream baseline `6a7960aaa0fe52a03120c7c26e631f06b6e0fe3b`. Version remains `0.8.2-bubbletea`; there is no release or installation claim for these changes. Go modules, adapter/transport contracts, check/build scripts, native defaults, recovery storage, notices and retained Swift source are unchanged.

The cloud checks used Go **1.26.10** linux/amd64, Node **24.22.1**, Python **3.12.14** and Git **2.51.1**. The existing pinned toolchain cache was reused, with version, binary hash and build module information recorded. Tests use the actual top-level UI router, Bubble Tea programs, Node adapter and disposable native-shaped HTTP/WebSocket fixtures. Executable PTY tests are included in the complete suite. They do not establish physical input or installed-native acceptance.

| Check | Actual result |
| --- | --- |
| `bash scripts/check-go.sh` | Passed formatting, all six package suites, `go vet ./...` and the complete race run. Total 304.122 seconds; integration 95.360 seconds normally and 200.284 seconds with race detection. No test subset replaced the production gate. |
| `bash scripts/check.sh` | Passed all 196 Node tests, adapter/script syntax, shell parsing and five Python tests. `swiftc` is absent, so conditional Swift/AppKit checks did not run. |
| `go mod verify`; `go mod tidy -diff` | Passed; dependency lockfiles remain identical to the input. |
| `bash scripts/build-go.sh` | Passed Linux amd64 production build and version output. |
| `GOOS=darwin GOARCH=arm64 T3OXIDE_OUTPUT=dist/t3oxide-darwin-arm64 bash scripts/build-go.sh` | Passed the Darwin ARM64 cross-build; file inspection reports Mach-O arm64. This executable was not run or signed on macOS. |
| Focused before/after regressions | The same `intuitive_flows_test.go` bytes fail all six original missing-behavior cases on the supplied baseline and pass after the changes. The extended safety suite also passes. |

New `TestIntuitiveFlow…` cases locate controls in actual composed views and reject stale geometry, retired/replaced prompts, changed model selections, scrolled-out options, old Next/Submit coordinates and mode/resize/generation transitions. They cover exact text, single/multi-selection, clearing custom answers, required validation, disabled submission, nested chooser ownership, wheel access and mouse-off. Settings hit identity retains every option, including native options omitted by its display summary.

`TestIntuitiveMouseProgram…` verifies launch fields and local choosers, cancellation/reopening, one exact Create; staged model/effort/tier and one exact Apply; approval selection followed by explicit Submit decision; question Back/Next/custom text and exact final Submit answers; plain text Submit/Cancel and durable input; and AgentBox Changes, Reply and Reviewed. It uses decoded SGR clicks and ordered bracketed paste at **60×16, 80×24, 120×35 and 170×40**, in Developer and AgentBox wherever applicable. The extended unified-startup program test verifies Enter/click parity for duplicate thread IDs across environments at those sizes, including metadata-only startup and preserved scoped drafts.

All earlier delivery, queue/steer, request retirement, native-default validation, independent Stop, late/uncertain acknowledgement, explicit original-ID recovery, palette and local terminal-exit regressions remain in the complete run. Navigation and staging assert zero mutation; explicit submission asserts exact native payload/count. No new polling, watcher, provider selector or scheduling backend was added, and no comparative latency/resource claim follows from these tests.

The return contains command/exit-code logs, the acceptance map, the exact patch, source hashes/modes and fresh-baseline reconstruction evidence. Initial sandbox loopback/subprocess restrictions and intermediate test-driver corrections are recorded separately from passing final gates. The archive audit also restored original executable modes from the supplied ZIP without changing input bytes; the disposable comparison commit was corrected to those original modes before patch generation.

**Native pending:** retained Swift/AppKit checks, macOS build/signature and installed-instance checks, physical mouse/scroll and Command-key delivery, and authorized disposable live mutation/recovery acceptance on the designated native host. No deployment, service restart, live account/model change or Vault write was performed. There was no capability-backend change requiring the canonical capability-record helper.

## Public release 0.8.2, 2026-10-29

The public release is standardized on `0.8.2-bubbletea`, including the Command-arrow and compact-layout fixes after 0.8.1. The Apple Silicon release build passed version/help, ARM64 inspection and strict ad-hoc signature verification. Earlier test and installed-instance evidence below retains its original version scope.

## README native captures, 2026-10-29

The four `docs/screenshots/native-violet-*.png` images were captured from the installed Go client in Terminal on M1 Pro. The app used `--theme violet` with `TERM=xterm-256color`, `COLORTERM=truecolor` and `NO_COLOR` unset. An earlier launch inherited `NO_COLOR=1` and rendered monochrome; it was gracefully closed and only the capture client was relaunched. No T3 service was restarted.

Sources were native window PNGs at 1316 × 934, with 68 pixels of title/tab chrome cropped to produce 1316 × 866 images. These are native 1× logical-display captures, without artificial upscaling or recolouring; Retina/2× capture was not obtained. The Developer browser, theme picker, AgentBox inbox and Commands views were visually reviewed. Lists were filtered to the single public “Public repo update” task; no transcript was opened. These images demonstrate those visible surfaces, not Send, approval, tool-detail or recovery acceptance. Earlier synthetic fixtures remain separately named.

Mode switching for capture rewrote client appearance settings, then returned to Developer default with the saved violet theme retained. Profiles and drafts were not written. The themed client was left running.

## Control fallbacks and Terminal restyling, 2026-10-29

Version `0.8.1-bubbletea` gives every Command action a Control fallback in its active context, including composer New/Settings/Model/Effort/Refresh, pane focus and visible-thread shortcuts. Existing Enter, Stop, Control editing, Command aliases and manual overrides retain their owners. The [complete table](shortcuts.md) identifies the fallbacks and terminal-delivery limits. Registry collision/coverage checks, real Model action tests and byte-decoder cases passed.

Terminal mode now resets full and nested list help, launch-title and AgentBox query editors, and navigator suggestions to terminal-default text/background with ANSI accents. Round-trip regressions verify restored fixed-theme defaults and unchanged text/caret; existing Markdown/syntax palette regressions pass. No dependency changed.

The final `scripts/check-go.sh` passed formatting, all six package suites, vet and the complete race run on Go `1.27.2` darwin/arm64. Integration passed in 58.682 seconds normally and 85.779 seconds under race detection. The native build passed ARM64 inspection, strict ad-hoc signature verification and version output. Executable SHA-256: `314b5c8b6635227dab87fc3cbcc368094ea9306fd41ecbd83878ec54c302b04b`. Unchanged adapter, Swift and dependency layers retain the 0.8 native evidence below.

On the receiving client's Ghostty 1.4.0, a focused `cmd+enter=unbind` removed only its inherited fullscreen mapping; native config validation and running-app `reload_config` passed. A disposable native terminal probe received synthetic Cmd+Enter, Ctrl+Alt+M and Ctrl+Alt+H as distinct `ESC[13;9u`, `ESC[109;7u` and `ESC[104;7u` events. It used no productive conversation or existing terminal input. This is native event-delivery evidence, not a physical-key or full GUI acceptance claim.

The same 0.8.1 executable is installed on the build client and published through the receiving client's existing release/current pointer. All 38 bundle files, ARM64 identity, strict signature, version/help and configuration-free capabilities passed. Both configured profiles passed read-only doctor checks. Immediate preservation and final comparison found unchanged configuration, drafts, sidecars, locks, command symlink, launcher, legacy app and earlier releases. One existing client process was preserved and uses its loaded release until relaunch. The preceding release remains available for rollback; consumed staging was removed. Physical Send and light/dark GUI acceptance remain unverified.

## Native integration, 2026-10-29

The 0.8 return was applied to its exact working-tree baseline. All 261 returned source files matched the final manifest's bytes and modes before native corrections. The accepted pre-existing documentation edit was retained.

Read-only checks against the running M1 server `0.1.5-nightly.20261029.3867` verified environment identity, authenticated project enumeration, the registered source project, native configuration/model catalog, root and isolated-worktree launch defaults, and native diff-preview reads. These checks created no thread, worktree, provider run or mutation journal. They do not establish live launch, approval, visibility, Stop or recovery acceptance.

The existing two-scope credential permits metadata reads but native project-file reads require `filesystem:read`. Same-subject temporary credentials limited to `orchestration:read` and `filesystem:read` verified the new read contracts and were revoked after use. The installed credential was then renewed with only that additional scope, preserving its subject, recovery identity, expiration ceiling and original credential for rollback. [Installation](../NATIVE.md) documents the scope and renewal. A live missing-file failure exposed the need for native `projects.readFile`'s `allowMissing: true`; the adapter now requests it and accepts `exists: false`. A regression verifies absent-file defaults, isolated-worktree resolution and continued rejection of authorization, transport and unsupported-method failures. All 14 native fixture tests pass.

The canonical capability helper reviewed all four enrolled Personal records. Their existing open/quit/Stop explanations remain accurate; only source fingerprints changed, and the final capability check reports no affected records. Knowledge synchronization and client publication retain separate receipts.

On Go `1.27.2` darwin/arm64 and Node `24.21.0`, the final `scripts/check-go.sh` passed formatting, all six package suites, vet and the complete race run. Integration passed in 57.739 seconds normally and 85.536 seconds under race detection. The full shared check passed 195 Node tests, Swift model typecheck, App parse, compiled model and macOS transport tests, and five Python tests; after the native optional-file regression, all 196 Node tests passed again. The dependency verification and tidy-diff passed. The pinned security scan reported zero reachable vulnerabilities, with nine imported-package and ten required-module advisories outside detected call paths.

Focused review found a Changes read that remained loading after switching conversations. Session changes now retire its loading state and serial; a regression verifies that a fresh review opens while the previous response cannot clear or replace it. The actual-program composer regression now checks the invalid-command validation text across platforms instead of assuming a Control-only hint on macOS. Both fixes are included in the final complete Go run.

The native build passed ARM64 Mach-O inspection, strict ad-hoc signature verification, version/help and offline capabilities without creating the supplied absent configuration/state paths. Executable SHA-256: `dc97f6b6c2ded4407f7a3c8a0b2583582f808c3b54cf00af7248af6b7891714a`. The M1 executable and versioned adapter were updated, with installed doctor/capability reads passing. The previous executable, adapter and credential remain available; configuration semantics, state inventory, drafts, lock, sidecars and host journals were preserved. No T3 service or provider setting changed.

After explicit owner authorization, the same executable was published on M4 through its existing versioned-release/current pointer. All 38 bundle files matched the approved manifest; installed ARM64, strict signature, version/help and configuration-free capabilities passed. Both configured environments passed read-only doctor checks. Immediate preservation and final comparison verified unchanged configuration, drafts, sidecars, locks, command symlink, launcher, legacy app and previous releases. No active client was terminated. The preceding release remains rollback; consumed staging was removed after verification.

Physical terminal delivery and disposable live mutation cases remain untested. No OMV adapter update or compatibility claim follows these M1 checks. M4's doctor result verifies authenticated metadata access through its existing profiles, without a productive transcript attachment or mutation.

This public record describes software behavior and compatibility. Private deployment inventories, machine identities, access routes, and delivery locations are maintained separately and must not be committed here.

## Integrated Developer + AgentBox, 2026-10-29

Version **0.8.0-bubbletea** extends the supplied **222-file working-tree snapshot** at `9ff7dd39ffdf4330fac896f0205e8f63c792b5ad`, including its accepted uncommitted UX proposal. It was not replaced by upstream HEAD and the informational baseline patch was not reapplied. The original private notes and thirteen annotated images were reviewed but remain outside this public source tree. The packet verifier passed all 252 entries, including 222 source files, three original notes and thirteen images.

### Portable production checks

The final production implementation was checked on Linux x86-64 with **Go 1.26.10**, **Node 24.22.1**, **Python 3.12.14** and Git 2.51.1. No installed native T3 execution host, macOS GUI or physical terminal was used.

| Command | Actual result and boundary |
| --- | --- |
| `bash scripts/check-go.sh` | Passed the complete six-package test run, `go vet ./...`, and complete race run. The final frozen-source run took 145.15 seconds; integration package 55.934 seconds normal and 86.406 seconds under race (`checks/final-go-3.log` in the return package). |
| `bash scripts/check.sh` | Passed 195 Node tests, all adapter/script syntax checks, shell parse and five Python measurement tests. `swiftc` was absent; the script's conditional Swift paths did not execute and remain native pending. |
| `bash scripts/build-go.sh` | Passed full pinned production Linux amd64 build and Darwin ARM64 cross-build (`GOOS=darwin GOARCH=arm64`). No restricted-core substitute was used. |
| `bash scripts/check-security.sh` | Pinned `govulncheck@v1.9.0` found **zero reachable vulnerabilities** with Go 1.26.10. It also reported nine vulnerabilities in imported packages and nine in required modules that this code does not appear to call; this is not a blanket claim of vulnerability-free dependencies. |
| `go mod verify`; `go mod tidy -diff` | Passed; no application dependency versions or `go.sum` changes. |
| Executable `--version`, `--help`, offline `capabilities --json` | Passed with deliberately nonexistent config/state paths remaining absent. Offline capabilities declare modes/extension boundaries and do not claim an installed native scheduler or default-environment bridge. |
| `python3 VERIFY-PACKET.py` | Passed again after implementation; input packet remained intact. |
| `bash scripts/check-capabilities.sh` | **Pending native helper:** exit 2, `capability-records is unavailable; reconcile the affected records on the canonical knowledge host.` No capability knowledge record was updated here. |

The initial Go 1.26.0 scan reported reachable standard-library advisories GO-2026-6218, GO-2026-6088 and GO-2026-4601. The minimum toolchain was raised to Go **1.26.10**, verified against the official archive checksum. Existing Charm module pins and other runtime dependency versions were retained. This is a real toolchain security fix, not a scanner suppression.

Maintained scripts were not weakened. Earlier environmental failures (direct execution of supplied non-executable scripts, initial missing toolchain, restricted loopback/subprocess execution), intermediate outdated expectations, and the first integrated resynchronization-readiness failure remain recorded separately in the return package. Approved disposable local fixture checks subsequently ran successfully. One added reconnect test initially sent during native synchronization; the production guard correctly retained the draft. The view now exposes that synchronization state and the test waits for authoritative native resynchronization. Request-retirement test readiness likewise waits for the newly rendered source before sending another key.

### Changed-contract evidence

Tests use actual Go client/session/program paths, the actual Node adapter and deliberately small local native-contract HTTP/WebSocket fixtures. They cover all-environment metadata-only startup, explicit source-qualified mouse/Enter open, duplicate IDs and partial/offline hosts; native project/default resolution, first-thread and project-only New, exact form restoration; byte/event decoding, editor ownership and distinct Send/Literal/Steer/request Submit; AgentBox single/batch isolated filing and explicit shared root, focused inbox/reply/formal review/changes/settle/snooze, same-session mode return and reconnect; stale requests, mismatched ACKs, helper restart, conservative lost-launch reconciliation, rank CAS/undo and independent Stop while an ordinary acknowledgement is held.

The final focused AgentBox race run includes 27 top-level tests and four size subtests across config, client, UI and integration. Native fixture projections are explicitly synthetic; no result is described as a live-host or physical-key test. Existing native work remains backend-owned after UI exit, while an unsubmitted filing list is only a durable human draft and does not dispatch later.

Twelve actual `Model.View()` cases cover Developer workspace, empty-conversation startup and AgentBox at 60×16, 80×24, 120×35 and wide sizes (170×40 or 180×45). Their ANSI/text captures are rendered offscreen to PNG using one fixed monospace font. Public README images contain only generic synthetic content. Cell bounds, real filled/reverse selection and hit targets are tested; the compact full-width navigator return reflows the conversation. These captures are not macOS terminal screenshots or font-size-per-component claims.

| Portable executable | SHA-256 |
| --- | --- |
| Linux amd64 | `adecc34f171a366607e234f4a6c4039545a2eeac73e2ba19cedb3291cd6c8441` |
| Darwin ARM64 cross-build | `c7b575d3f26868c5594f498aa7a8ab07179bff2de9da362fda362734e96e3de6` |

The return package contains exact command logs, build metadata, source hashes/modes, a binary-safe patch against the supplied working tree and fresh-baseline reconstruction equality. Executable hashes identify the portable builds; they do not establish macOS signature validation, installed layout or deployment acceptance.

### Local resource evidence

Fifteen finite Linux captures used three independent three-second runs per case, the actual compiled baseline/current TUI and Node adapter, and a synthetic 500-root/eight-project metadata fixture. Mean root RSS was 25.31 MiB for baseline Developer startup, 25.01 MiB for integrated Developer startup, 26.17 MiB for integrated AgentBox, 46.73 MiB for the separately held baseline Node adapter and 46.92 MiB for the separately held current adapter. PSS, ranges, commands and unaltered collector samples are in the return package's `checks/resources/REPORT.md` and raw files.

These figures describe short idle metadata states with different baseline/current startup presentations. They establish no performance improvement, energy saving, native capacity or streaming latency. TUI helpers had exited before sampled idle states; short-lived helper cost is not zero or captured by that idle number. No coarse `ps` CPU increment was observed, which is not zero CPU cost. The synthetic fixture/collector were excluded; no real T3/provider backend, SSH host or macOS footprint was measured. All 80 production build inputs matched the final implementation.

### Installed/native gates

The inspected upstream target is `v0.1.4-nightly.20261029.3853` at `783581d461dc72cdd8f78fc8abb4ad536dfc4547`; the inherited native source anchor is `0e7346ccd9427c06161e222319e718e6bd70887f`. Neither establishes the current installed version on any host. Native workflow extension 1 is additive to companion v1/orchestration v2; unsupported installed methods return precise errors.

The native desktop's preferred default environment/project has no verified supported bridge. New honors explicit `--env`, a unique `defaultForNew`, a sole configured environment, or asks. A generic durable priority-based future-launch queue/capacity API is also absent from the verified contracts; no client scheduler is substituted. Native visibility commands lack atomic expected-revision exclusion, so fresh preflight protects known stale state but cannot exclude a different writer racing after that read. Structured outcomes/artifact indexes remain missing unless reported by native T3.

Native acceptance must test actual project/default/worktree/request/queue/steer/review contracts, exact recovery and physical Command/control delivery, perform affected macOS Swift checks, and run the canonical capability helper. [Native remainder](../NATIVE-REMAINDER.md) and the return package give focused host-specific steps. No private-host connection, installation, service restart, push/merge or publication occurred. A later M4 publication still requires accepted M1 integration, current explicit owner authorization, exact signed manifests, preservation/rollback and atomic publication.

---

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
