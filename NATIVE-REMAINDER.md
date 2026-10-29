# Native integration remainder — t3oxide 0.8

The portable implementation contains both Developer refinements and native-backed AgentBox. [Verification](docs/verification.md) separates the actual Linux/fixture results from installed-host acceptance. Historical native records in that document remain evidence only for their recorded versions and unchanged paths.

The 2026-10-29 M1 integration passed the complete native Go/race checks, shared Swift/Python checks, all 196 Node tests and the signed ARM64 build. Installed project/default/ref/diff reads passed against T3 `0.1.5-nightly.20261029.3867`; the local executable and adapter were updated with state and rollback preserved. The owner subsequently authorized M4 publication: its existing release pointer was switched atomically, installed checks and both profile doctor reads passed, and state/rollback comparisons matched. The remaining physical-terminal and live-mutation cases below retain their separate scope.

## Focused client acceptance

Use the actual supported Apple Silicon terminal with disposable state first. Verify 60×16, 80×24, 120×35 and wide layouts, filled selection and reduced-color/Terminal themes. Confirm physical Command aliases when delivered, portable Ctrl+]/Ctrl+\, independent Ctrl+S, Enter newline, separate Steer and request Submit, list text editing and visible-pane focus. Do not change global terminal or OS settings to make a key test pass.

Exercise all-environment metadata startup with an offline host, empty conversation until explicit open, first thread in an existing project, native-default New and project-only same-project New. Verify the exact previous composer/caret/selection/reading anchor after forms, thread changes and both mode directions, including reconnect, streaming and pending formal requests. Test a displayed tool click against its source item; do not infer physical acceptance from decoded events or a local Program fixture.

Build/check with the maintained scripts on the native host, including Swift model/transport checks and native signing verification. Source requires Go 1.26.10 or a supported newer patched toolchain. The returned Darwin ARM64 cross-build evidence is not an installed/signature/notarization result.

## Execution-host acceptance

Record the actual native T3 revision, authenticated environment identity, configured defaults and adapter release before integration. The inspected target is nightly `v0.1.4-nightly.20261029.3853` / `783581d461dc72cdd8f78fc8abb4ad536dfc4547`; it is not a statement about installed hosts. Native differences belong in `adapter/`. Preserve unrelated live edits and current auth/host-key policy.

Verify project enumeration/defaults (including `t3.json`), first-thread scaffold behavior, model/options and isolated workspace binding. File two separately reviewed tasks and verify different native worktrees; choose shared root only explicitly. Check genuine pending approvals, one-message steer/queue/Stop, actual changes/PR metadata, settle/snooze and a stale inverse after another client changes the thread. Unknown capabilities must remain precise errors with ordinary conversation paths usable.

Drop a launch response in an authorized disposable workflow and reconcile the original thread/message/workspace identity. An absent or mismatched result must remain uncertain with no second launch. Verify normal and Stop recovery lanes independently. Closing the TUI must leave existing native work owned by T3; unsubmitted filing-list drafts must not dispatch after exit.

The canonical knowledge-host `scripts/check-capabilities.sh` and review of all four affected existing records passed during M1 integration. Repeat only when their declared inputs change again. The portable environment's earlier missing-helper result remains historical evidence.

## Native capability gaps

- The examined API does not expose the native desktop's preferred default environment/project. The client honors explicit `--env`, a unique optional `defaultForNew`, a sole configured environment, or asks. A supported native bridge is required for exact automatic cross-client mirroring.
- No general durable priority-based future-launch queue, admission budget or host-capacity API was verified. Inbox ranks are presentation only. Native recurring scheduled tasks are not repurposed as this workflow's scheduler.
- Native visibility commands lack an atomic expected-revision field. Fresh preflight and post-action identity protect supported stale/undo paths; strict exclusion of another writer racing after preflight requires a native compare-and-swap contract.
- Structured task outcomes/artifact inventories are only shown when reported. A latest assistant excerpt is not a formal task receipt or completion proof.

## Preservation and later publication

M1 adoption preserved executable/helper targets, state files and absences, locks, complete pending operation identities, presentation sidecars and host journals. Keep that rollback material and the legacy Swift app's separate state. A later authorized receiving-client publication needs its own immediate preservation snapshot. See [state and recovery](docs/state-recovery.md).

Publication is a separate owner action after accepted native integration and current explicit authorization. Verify signed architecture/version/help and exact manifests; take the immediate preservation snapshot; stage alongside current releases; use same-filesystem promotion and the supported atomic pointer switch. Preserve old releases, launchers, journals and rollback. Never partially overwrite an active installation or restore old state over unresolved native effects.
