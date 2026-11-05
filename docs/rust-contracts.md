# Rust integration and contract evidence

Reviewed on 2026-11-02 against t3oxide `e73caacd673d1a9bbceea8a7ed240b2a068ba5a7`
and T3 Code `ac8dce0cc3abd8a337f2d806b5082a6834b9b81a`.
This is source compatibility evidence. It does not establish the versions,
credentials, reachability or behavior of an installed user environment.

## Integration decision

The application is Rust. It reuses the existing dependency-free Node companion
on each configured host through the same local process or SSH stdio boundary.
The companion needs Node 22.4 or newer and no npm installation. It connects to an
existing loopback T3 endpoint using its existing private host configuration.

T3 does not require a Node client. Retaining this small boundary is justified by
the verified integration it already owns: environment verification, native
authentication and WebSocket tickets, Effect RPC acknowledgements/cancellation,
lossless bounded-snapshot restoration, request revisions, native launch defaults,
and durable exact-payload receipts. Moving it into Rust would replace a working,
tested compatibility boundary and require changing existing remote-host installs
and recovery storage in the same pass. No additional proxy, runtime, scheduler,
database or daemon is introduced. The Rust application does not implement T3's
thread, agent, worktree, queue or permission authority.

## Verified upstream contracts

All source links are pinned to the reviewed T3 commit.

| Boundary | Source examined | Retained behavior |
| --- | --- | --- |
| Environment identity and protocol | [environment.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/contracts/src/environment.ts), [ServerEnvironment.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/apps/server/src/environment/ServerEnvironment.ts) | Protocol is still `2`. Exact configured `environmentId` must match; writes require `serverResolvedCommandContext`. |
| Authenticated HTTP and tickets | [environmentHttp.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/contracts/src/environmentHttp.ts), [ws.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/apps/server/src/ws.ts) | Descriptor, shell, bounded/history reads and POST WebSocket tickets retain their paths and payloads. Protocol header and WebSocket protocol query remain required. |
| RPC methods and payloads | [rpc.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/contracts/src/rpc.ts), [orchestrationV2.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/contracts/src/orchestrationV2.ts) | `dispatchCommand`, `launchThread`, `subscribeThread`, `searchThreads`, `getTurnItem`, provider catalogs and native project/review reads retain their contracts. New optional upstream fields are not required for existing operations. |
| Native human commands | [commands.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/client-runtime/src/operations/commands.ts) | Sends use `createdBy:user`, `creationSource:web`, native delivery intent or `queue_after_active`. Stop uses the reviewed `runId` and `holdQueue:true`. Answers use the exact native request ID. |
| Compact snapshots | [orchestrationV2IdTable.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/contracts/src/orchestrationV2IdTable.ts), [orchestrationV2BoundedSnapshot.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/shared/src/orchestrationV2BoundedSnapshot.ts), [orchestrationV2DedupedBodies.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/shared/src/orchestrationV2DedupedBodies.ts) | The existing narrow restoration of record IDs, omitted local visible items and deduplicated bodies matches the current opt-in format. Arbitrary tool JSON is not recursively rewritten. |
| Stream liveness | [rpc/protocol.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/client-runtime/src/rpc/protocol.ts) | Existing five-second ping and fifteen-second liveness budget match upstream. Reconnect reads a fresh revision base; mutations are not automatically replayed. |
| Durable native receipts | [CommandReceiptStore.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/apps/server/src/orchestration-v2/CommandReceiptStore.ts) | Native command identity remains authoritative. The companion preserves the exact upstream payload before dispatch and acknowledgement before reporting success. |
| Native settings | [rpc.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/contracts/src/rpc.ts), [settings.ts](https://github.com/pingdotgg/t3code/blob/ac8dce0cc3abd8a337f2d806b5082a6834b9b81a/packages/contracts/src/settings.ts) | `server.updateSettings` still provides no command ID or revision/CAS. Existing read-only defaults inspection remains appropriate; no unsafe settings-write route is invented. |

No breaking drift was found in the exercised existing operations. This review
does not assert compatibility with every T3 method or future protocol revisions.
The companion continues to reject an unknown orchestration version.

## Additive navigation capability

`hello.features.inclusiveThreadList` advertises support for
`list {includeChildren:true, grouped:false, lifecycle:"all", limit:200}`.
This returns native child summaries before pagination so Rust can search loaded
children while their visual parent is collapsed. The new flag does not alter
legacy listing defaults, fetch transcripts, mark conversations read, open a
subscription or mutate T3. `more` and `nextCursor` remain authoritative; a bounded
page must never be described as exhaustive history.

## Verification fixtures

- `node --test --test-concurrency=1 tests/*.test.mjs` exercises companion framing,
  bounded streams, native commands, requests, identity failures, journals and
  installation behavior. `rust-navigation.test.mjs` adds legacy-default,
  inclusive-child, paging/type/lifecycle and metadata-only read checks.
- `python3 tests/loopback_fixture.py` runs the real companion through loopback
  HTTP, WebSocket and stdio. It checks actual SIGKILL boundaries before dispatch,
  before upstream acknowledgement, after upstream acknowledgement and after
  durable acknowledgement for Stop, approval and user-input responses. Exact
  retries must have one effect and must not affect a newer run/request.
- `tests/rust_fixture.mjs --config /private/fixture-host.json` is the deterministic
  companion for Rust transport and terminal tests. It uses the real admission,
  projections, stream reducer and durable journal; only native HTTP/RPC endpoints
  are replaced. It has synthetic projects, nested threads, both request kinds,
  streaming text, catalogs, review, history and first-thread launch. Optional
  `fixture` settings provide `stateFile`, `logFile`, `dropAckOnce`, `ackDelayMs`,
  `disconnectAfterMs`, `unavailable` and `stream`. Logs contain synthetic fixture
  payloads only. `rust-fixture.test.mjs` verifies the fixture itself.
- `tests/slt_terminal.py` drives the active SLT binary through a real PTY and
  the actual adapter fixture. It checks wake-aware attachment/exit, exact
  Unicode multiline drafts, tab autosave/restore, enhanced Ctrl+Enter
  Press+Release with one native effect/no added newline, command Enter,
  actual Mode B admission, empty-project inventory and explicit acknowledged
  native answer submission. Run without global test-tool installation:

  ```sh
  cargo build --release --locked
  uv run --with pyte==0.8.2 python tests/slt_terminal.py --binary target/release/t3oxide
  ```

- `tests/rust_terminal.py` retains the historical Ratatui acceptance/comparison
  inputs and supplies shared PTY/fixture helpers to the active script. It drove
  its identified historical binary through a real PTY,
  including conventional terminal control bytes, bracketed Unicode paste,
  mouse events and resize. Its scenarios use temporary profiles and state,
  actual companion admission/projection/journaling and synthetic native
  endpoints. It requires the test-only `pyte==0.8.2` package; Pillow is optional
  for rendered screen artifacts. For example:

  ```sh
  python3 -m pip install pyte==0.8.2
  cargo build --release --locked
  python3 tests/rust_terminal.py --binary target/release/t3oxide --output /tmp/t3oxide-terminal-evidence
  ```

  Passing `--baseline /absolute/path/to/the/pinned/go-client` measures both
  clients in fresh processes against the same fixture, terminal dimensions,
  streaming schedule and idle window. Root and descendant process RSS/CPU are
  reported separately, so the Node companion is included in the totals. These
  are Linux measurements, not macOS physical-footprint or provider-latency
  claims. On other Unix hosts the script runs terminal acceptance and explicitly
  skips Linux-only resource sampling; `--acceptance-only` selects that scope
  directly. The JSON records the sample values and limitations; the screen text
  and optional monochrome PNGs contain synthetic data only.

The loopback and deterministic fixtures do not contact or change installed T3
services. Real host authentication, the configured SSH route and macOS terminal
behavior still require verification on the machines that own those resources.
