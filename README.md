# t3oxide

A Rust terminal client for existing T3 Code environments. SLT v0.26.0 owns the terminal runtime, focus, editing and output. The retained Node adapter supplies native data, requests and durable operation receipts; T3 owns execution and orchestration.

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

**A** combines Unread and Completed, with unread first. Completed means opened, inactive and unsettled. Running contains active work, needs input and failures; read state remains independent. Pins and the on-demand Settled explorer retain their native identity. Ordering follows meaningful native activity rather than token/tool churn. Context shows the actual native direct-subagent count when available.

Search starts with a project and supports environment/name/native-content search. Opening a conversation reuses its internal tab or adds one; every tab owns its autosaved prompt draft. Closing a tab retains its draft and recovery records. Model and effort controls use the current native catalog and provider identity.

**B** groups actual native human questions by project. Its persistent project-navigation region changes level in place; returning globally retains the selected project and content. Supporting Markdown and supported Mermaid flowcharts remain separate from navigation; unsupported diagram types retain their source. Open in A retains source identity. Answers autosave against the exact source/request revision, validate native values, and require explicit Submit. Success requires an acknowledged sequence plus matching environment, thread, request, revision and operation ID.

Explicit `/context-save` captures a source-qualified decision, evidence, journal or local open loop. `/context` recalls those records; `/resume` adds protected drafts, original operations and native questions without executing them. [Context controls](docs/context-controls.md) describe durability, provenance, totals and degradation. Dependency synthesis requires explicit native evidence; current native offers leave coverage unknown.

The revised presentation preferences live under `sltUi`; old presentation preferences remain recoverable without overriding this contract. Existing accounts, manual names, drafts and uncertain operations are preserved. Internal tabs are always available, including inside Rex.

## Input

Plain Enter inserts a newline. Typing `/` at the prompt opens completion; Enter selects the command. Esc retains unsent text, `//` stays literal, and pasted slash text stays literal. Cmd+Enter is preferred when delivered; Ctrl+Enter, Shift+Enter and Cmd+Shift+Enter are supported alternatives. An explicit Send button and portable Ctrl+] / Ctrl+\ path remain available. Ctrl+S is the independent Stop action. Native Rex delivery and pane-collision checks are separate acceptance work.

See [shortcuts](docs/shortcuts.md), [content controls and examples](docs/content-controls.md), [architecture and flow map](docs/architecture.md), [source reuse](docs/rust-source-reuse.md) and [verification](docs/rust-verification.md).

## Durable operations

Drafts and journals remain in the client state file. Every mutation is saved with its exact operation ID and payload before dispatch. Disconnects never replay writes. Recover reconciles the original identity explicitly; an uncertain ordinary operation does not block Stop. Acknowledgement clears only the matching operation and submitted text, preserving newer edits. Exit detaches the client while native work continues.

```sh
scripts/check-rust.sh
scripts/check.sh
scripts/build-rust.sh
```

[NOTICE](NOTICE), [dependency notices](LICENSES/rust-third-party.txt) and [locked inventory](docs/dependencies.json) retain source attribution. Restricted selected-source integrations and any replacement work are identified explicitly in the source-reuse record; basic SLT Markdown is not a claim of Mermaid rendering.
