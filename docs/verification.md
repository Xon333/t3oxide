# Verification

This public record describes software behavior and compatibility. Private deployment inventories, machine identities, access routes, and delivery locations are maintained separately and must not be committed here.

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
