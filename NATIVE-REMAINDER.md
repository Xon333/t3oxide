# Native remainder — t3oxide 0.7

The implementation, portable tests and Darwin/ARM64 cross-build are complete. This pass used Linux, disposable fixtures and synthetic state. It has no macOS terminal/AX session, Apple signing identity, installed T3 host access, existing SSH authentication or personal state. No installation, live migration, provider action or publication occurred.

Use the supplied source and verification record. No further architecture decision or ordinary implementation is assigned here.

## 1. Native client and terminal acceptance

- Verify the package hashes, Mach-O ARM64 architecture and `0.7.0-bubbletea` version on an Apple Silicon Mac. The supplied binary contains Go's linker-generated ad-hoc code signature. Apply/verify the existing project's native signing policy with macOS tools and retain the post-sign hash; native signature validation and notarization were not performed here. Rebuilding is optional; source requires Go 1.26+ and the existing pinned modules.
- Use disposable state first, in the actual supported terminal at 60×16, 80×24, 120×35 and a wide context layout. Verify physical Ctrl+O, Ctrl+P, Tab/Shift+Tab, Escape, Ctrl+S and Ctrl+Q; composer text/selection and clipboard/external-editor round trips; resize while reading a streaming page; mouse focus/choices/tool inspection and `/mouse off` native selection. Forms use their existing keyboard controllers.
- Verify main attached identity versus browsed environment, visible focus and textual status with the existing themes, including Terminal light/dark defaults. Check the actual terminal's accessibility/AX exposure and physical key delivery; synthetic program tests cannot prove these.
- Confirm a recent activity strip opens the displayed item; an item retired during focus cannot turn Enter into Send. Check terminal restoration on exit. Avoid replaying unrelated, unchanged 0.6 acceptance.

## 2. Installed host compatibility and existing routes

- On each existing Mac/Linux execution host, inspect the exact T3 revision, Node version, private `host.json`/token permissions and verified environment identity. Keep credentials, T3 home/database, workspaces and provider accounts on that host. M4 stays client-only.
- Stage the reviewed helper through the existing host-local `node scripts/install-host.mjs` flow. It validates the existing loopback T3 instance, publishes all eight modules plus NOTICE, atomically writes validated `host.json`, and replaces the current-release symlink. Without override flags, fields are preserved but formatting may change. Preserve previous releases, journals and any unresolved installer lock; do not blanket-clean them.
- Through the user's existing local/SSH profiles, run `doctor --env PROFILE --json`, `capabilities --env PROFILE --json`, bounded `threads --env PROFILE --json`, then `activity --env PROFILE --thread ID --json` and `inspect --env PROFILE --thread SOURCE_ID --item ITEM_ID --revision REV --json` for an offered item. Verify exact environment/source IDs, protocol 2, live provider descriptors and the installed host's item/history endpoints. An older host may explicitly refuse new inspection while existing chat remains available.
- Verify one attached session, temporary metadata helpers on other environments, and cleanup after cancel/exit/broken SSH. No forward port or always-on proxy is needed. Renew expired host authorization with the existing host workflow, then restart the helper or use explicit `/reconnect`; internal WebSocket reconnect keeps the loaded token. Authentication/identity failure must not trigger mutation replay.

## 3. Personal-state adoption and focused live acceptance

- Before any promotion, record the current executable/helper release targets and take a consistent preservation snapshot of environment profiles, `go-drafts.json`, its `.prompts.json` sidecar, `terminal.json`, any state compatibility copies, lock ownership, all in-flight/uncertain ordinary and Stop records, and host `operations-v1` journals. Record existing absences. Keep legacy Swift files/releases and its separate `drafts.json`. Do not publish private receipts or credentials.
- Use one writer per state path. There is no 0.6→0.7 schema migration: new appearance fields are optional. Confirm complete composer/launch drafts, presets, next-send options, browse preferences and pending identities survive. Resolve existing uncertain operations through their original IDs; a new executable is not evidence that an effect did or did not happen.
- In a deliberately chosen disposable conversation on the existing host, verify the changed paths: browse another environment without changing the attached draft/reading position; explicitly expand working children and reach settled history/search/paging; inspect one tool and Earlier activity; review source-bound New/Settings with exact workspace/model options. Confirm ACK versus response/completion and reviewed-run Stop/queue hold using the established fixture or a suitable disposable real run. Actual live effects require the native task's explicit scope.
- Promote only through the existing versioned-release route after these checks. Preserve prior client/helper releases and ship NOTICE/LICENSES. Compare preserved state immediately and at the end, accounting only for deliberately accepted actions.

## Rollback

Restore the recorded client and helper release targets; do not delete new or old state/journals. An existing helper retains its loaded code until restart. Preserve new pending operations and sidecars before using an older client; do not alternate incompatible writers or restore an old state snapshot over unresolved effects. Swift rollback keeps its own state and cannot import Go pending operations. If a host endpoint is incompatible, retain the older helper; new inspection remains unavailable while existing conversation behavior stays intact.

## Measurement before a transport rewrite or performance claim

This is optional for adoption and required before claiming fleet resource savings. Use `scripts/measure.py` on both the client and the identified host helper for equivalent 0.6/0.7 idle, streaming and switching workloads, with three short repetitions. Separate client, SSH, helper and shared T3/provider work; RSS sums are not unique memory. Add native event-to-presentation timing if that is the decision criterion. The supplied absence-only observations and deterministic 80/120/400 ms scheduling tests are not RAM, battery or end-to-end latency measurements.
# Native remainder from the portable 0.7 handoff

This records the machine-bound work left by the portable implementation pass. The subsequent adoption and its actual limits are recorded in [verification](docs/verification.md#native-adoption-2026-10-24); these instructions do not require replaying completed checks.

