# t3oxide

A Rust terminal workspace for existing [T3 Code](https://github.com/pingdotgg/t3code) environments. Browse conversations, follow live work, answer native requests and review changes without giving another service control of your agents.

**Developer and AgentBox share one workspace.** Developer emphasizes conversation navigation; AgentBox emphasizes attention, active work and explicit review. Switching views preserves the attached conversation, composer and reading position. T3 remains responsible for threads, agents, execution, permissions, queues and worktrees.

## Build and connect

The client targets Linux and macOS. Build with a current stable Rust toolchain; the manifest declares Rust 1.88 as its minimum. The application has no Go runtime dependency. Each adapter host needs its existing Node 22.4+ installation and an existing T3 environment. Remote profiles use OpenSSH.

```sh
cargo build --locked --release
./target/release/t3oxide envs
./target/release/t3oxide
```

Existing environment profiles remain usable. The bundled companion adds inclusive child metadata for full loaded-child filtering; older compatible companions retain native search and their supported operations until explicitly updated. For a new setup, follow [installation and configuration](NATIVE.md). Building the application does not install or restart T3. The small, dependency-free Node companion is retained for the verified T3 authentication, RPC, compact snapshot and durable receipt boundary; [contract evidence](docs/rust-contracts.md) explains that decision.

```sh
# Open a known source directly.
t3oxide --env desk --thread THREAD_ID

# Use the attention-oriented view or your terminal's own colors.
t3oxide --mode agentbox
t3oxide --theme terminal

# Read-only diagnostics; no interactive mutation commands are exposed by the CLI.
t3oxide doctor --env desk --json
t3oxide threads --env desk --json-envelope --limit 100
```

## One place to find and finish work

**Find a conversation across hosts.** The navigator combines bounded native metadata from configured environments. A failed host leaves healthy sources usable and reports its own failure. Expand authoritative child conversations, filter loaded titles, search native recent messages explicitly, or preview a highlighted conversation without changing the active draft or Stop target. Project scope, priorities and saved reading positions are local presentation preferences.

**Keep the work readable.** Ratatui renders streaming Markdown with a bounded cache. Split, Focus and Wide layouts adapt to terminal size; the highlighted border shows keyboard focus. The composer supports Unicode, wrapping, selection, mouse cursor placement and undo/redo through the maintained Ratatui textarea. Long request prompts and recovery payloads have their own scrollable review pane.

**Act on native state.** Send with native Auto, Queue or Steer delivery, stop the reviewed active run with queue hold, inspect source tools and diffs, and answer the exact approval or question offered by T3. Model and option pickers use the host's live catalog and exact provider instance. Favorites and one-message overrides preserve that identity. Unsupported host capabilities remain explicit.

**File work with a review.** New chooses an existing native project and resolves its defaults before Create. AgentBox requests native isolation by default. Title, message, model/options and workspace remain reviewable; an empty message creates a filed conversation without starting a run. Filing multiple human-authored tasks proceeds through explicit per-task launch reviews and keeps receipts and remaining drafts.

**Review intentionally.** Attention, Working, Reviewed and All are views of the same conversations. Opening a thread never marks it reviewed. Settling, restoring, pinning and snoozing use native revision-bound actions. Local priority changes do not schedule execution.

## Controls that stay consistent

| Action | Key |
| --- | --- |
| Search commands | **Ctrl+P** or **Ctrl+Space** |
| Switch Developer / AgentBox | **F2** |
| Cycle Split / Focus / Wide | **F3** |
| Move pane focus | **Tab** / **Shift+Tab** |
| Conversations / Details | **Ctrl+O** / **Ctrl+L** |
| Select and open a conversation | **↑↓** or **j/k**, then **Enter**; or click |
| Filter loaded conversations | **/** in the navigator |
| Native conversation search / loaded transcript search | **Ctrl+F**, according to focused pane |
| Newline / send | **Enter** / **Ctrl+]** |
| Stop active native run and hold its queue | **Ctrl+S**, including over dialogs |
| Review native requests | **Ctrl+Y** |
| Edit the active draft externally | **Ctrl+E** using `VISUAL` or `EDITOR` |
| Return to live follow | **End** in the transcript |
| Save and exit | **Ctrl+Q** |

The [shortcut reference](docs/shortcuts.md) covers context, mouse interaction, forms, source inspection and read-only CLI commands. The command palette is the complete action catalog. Paste is always literal text; the composer never interprets pasted slash commands.

## Drafts and recovery

The Rust client stores drafts and recovery records in `~/.config/t3oxide/rust-state.json`, independently of T3. When that file is absent, the client validates and imports the previous terminal client's `go-drafts.json` and supported sidecars without changing their contents. An existing Rust state file always wins. Keep unresolved operation records and host-side receipts when updating.

Every mutation is saved with its exact environment, operation ID and payload before dispatch. A disconnect does not trigger a retry. **Recover saved operations** shows the original payload and offers explicit reconciliation with the same identity. Stop has a separate pending lane so an uncertain ordinary operation does not block it. Acknowledgements clear only the matching record and submitted text; a newer draft is retained. Save failures remain visible and prevent a normal quit from discarding edits.

Exiting detaches this client and its helper processes. Native T3 work and accepted queues continue.

## Development and evidence

```sh
cargo fmt --all --check
cargo clippy --locked --all-targets -- -D warnings
cargo test --locked
cargo build --locked --release
node --test --test-concurrency=1 tests/*.test.mjs
python3 tests/rust_terminal.py
```

Read [architecture](docs/architecture.md), [source reuse decisions](docs/rust-source-reuse.md), [current T3 contracts](docs/rust-contracts.md) and the [verification record](docs/rust-verification.md). The record separates deterministic protocol/PTY evidence from checks that require an installed host, its credentials, SSH route or a particular terminal emulator. Full T3 remains available for complete history, attachments and capabilities not advertised through this companion.

[NOTICE](NOTICE), [LICENSES/rust-third-party.txt](LICENSES/rust-third-party.txt) and [the locked source inventory](docs/dependencies.json) contain dependency attribution, including the unmodified MPL-2.0 fuzzy matcher. Third-party notices do not establish a blanket license for t3oxide's original application code.
