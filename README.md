# t3oxide

A lean Rust terminal client for existing T3 Code conversations and native questions. SLT v0.26.0 owns the terminal runtime, focus, editing and output. The retained Node adapter supplies native data, requests and durable operation receipts; T3 owns execution and orchestration.

## Build and connect

```sh
cargo build --locked --release
./target/release/t3oxide envs
./target/release/t3oxide
./target/release/t3oxide --env desk --thread THREAD_ID
./target/release/t3oxide --mode b
```

Use a current stable Rust toolchain (declared minimum 1.92), existing environment profiles and Node 22.4+ on adapter hosts. Remote profiles use existing OpenSSH routes. See [installation](NATIVE.md). Building does not install a client or restart T3.

## Conversations and human questions

**A** combines Unread and Completed, with unread first. Completed means opened, inactive and unsettled. Running contains active work, needs input and failures; read state remains independent. Pins and the on-demand Settled explorer retain their native identity. Ordering follows meaningful native activity rather than token/tool churn. Native metadata supplies delegated-work visibility; absent evidence stays unknown.

Ctrl+F opens a live fuzzy switcher over loaded conversations, matching titles, projects, environments and IDs. Search T3 opens project-scoped native content search. Opening a conversation reuses its internal tab or adds one; every tab owns its autosaved prompt draft. Closing a tab retains its draft and recovery records. Model and effort controls use the current native catalog and provider identity.

**B** lists actual native human questions by project. Select the source and answer its interactive native choices; Open in A retains the exact conversation. Answers autosave against the source/request revision and require explicit Submit. Success requires an acknowledged sequence plus matching environment, thread, request, revision and operation ID. Supporting Markdown and manually invoked Mermaid rendering remain available; they do not create requests or schedule work.

Native T3 owns graphs, context, orchestration, queues and recovery authority. The active client has no local Context/fael store, dependency-ranking pass, project graph or batch-task creation loop. Historical source and existing data remain preserved; saved launch forms, batch receipts and exact pending operations stay accessible for deliberate recovery. Outgoing messages use the reviewed human draft without an orchestration suffix.

T3/ChatGPT previews and native annotations are the default for UI feedback. A manually invoked image review in Rex remains a small independent utility: a local artifact, preserved comments and deliberate return to the original thread. It needs no Artifact Library API, permanent backend or automatic Finish.
The revised presentation preferences live under `sltUi`; old presentation preferences remain recoverable without overriding this contract. Existing accounts, manual names, drafts and uncertain operations are preserved. Internal tabs are always available, including inside Rex.

## Input

Plain Enter inserts a newline. Typing `/` at the prompt opens completion; Enter selects the command. Esc retains unsent text, `//` stays literal, and pasted slash text stays literal. Cmd+Enter is preferred when delivered; Ctrl+Enter, Shift+Enter and Cmd+Shift+Enter are supported alternatives. The portable Ctrl+] / Ctrl+\ path remains available. Ctrl+S is the independent Stop action. Physical Rex checks on M1 and the adopted M4 route verified one Ctrl+Enter send without creating a pane. Rex Cmd+Enter retains its pane action. On configured Rex clients, use the native **Enter T3oxide Editing** palette action for Mac selection/copy and Escape to leave its client-wide mode.

F1 opens the compact searchable Commands/keybind menu. F3 toggles conversation navigation, including in narrow terminals; F4 toggles context and F6 cycles the visible panes.

See [shortcuts](docs/shortcuts.md), [content controls and examples](docs/content-controls.md), [architecture and flow map](docs/architecture.md), [source reuse](docs/rust-source-reuse.md) and [verification](docs/rust-verification.md).

## Durable operations

Drafts and journals remain in the client state file. Every mutation is saved with its exact operation ID and payload before dispatch. Disconnects never replay writes. Recover reconciles the original identity explicitly; an uncertain ordinary operation does not block Stop. Acknowledgement clears only the matching operation and submitted text, preserving newer edits. Exit detaches the client while native work continues.

```sh
scripts/check-rust.sh
scripts/check.sh
scripts/build-rust.sh
```

[NOTICE](NOTICE), [dependency notices](LICENSES/rust-third-party.txt) and [locked inventory](docs/dependencies.json) retain source attribution. Restricted selected-source integrations and any replacement work are identified explicitly in the source-reuse record; basic SLT Markdown is not a claim of Mermaid rendering.
