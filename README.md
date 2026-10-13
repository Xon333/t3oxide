# t3oxide

A lightweight native macOS companion for existing [T3 Code](https://github.com/pingdotgg/t3code) environments. Read conversations, send messages, answer questions and approvals, and stop an active turn without keeping the full web interface open.

```text
Swift/AppKit client
  └─ SSH or local stdio
      └─ on-demand Node adapter → existing T3 server
```

## Features

- Selectable transcript, composer, thread filter, and multiple environment profiles.
- Bounded incremental updates, earlier history pages, and per-thread drafts.
- Human-authored messages, queue/steer intent, Stop with queue hold, and new conversations using an existing workspace.
- Explicit retry of saved operations, with durable host-side recovery across helper restarts.
- Optional shortcuts to the full T3 web interface.

The client uses AppKit and OpenSSH, with no embedded browser or bundled T3 server. Only the selected environment's helper runs; T3 continues independently when the client disconnects.

## Requirements and setup

- **Client:** Apple Silicon Mac running macOS 14 or later; Apple command-line developer tools to build from source.
- **Host:** an existing compatible T3 instance and Node.js 22.4 or later, on macOS or Linux.
- **Remote access:** working SSH authentication and host-key verification.

Follow the [installation guide](NATIVE.md) to install the host adapter and configure the client. Build with:

```sh
bash scripts/build-mac.sh
```

Outputs are `dist/t3oxide.app` and `dist/t3oxide-macos-arm64.zip`. Builds are ad-hoc signed, not notarized. GitHub Actions also produces an ARM64 artifact.

## Compatibility and limits

The adapter targets T3 orchestration protocol 2. See the [verification record](docs/verification.md) for tested nightly versions and actual acceptance results; compatibility with future nightlies is not guaranteed. T3-specific changes belong in the host adapter.

The transcript displays plain text with bounded history and explicitly shortened long items. Rich diffs, terminals, attachment uploads, and other advanced workflows remain in full T3.

## Development

```sh
bash scripts/check.sh
```

See [architecture](docs/architecture.md), [contributor instructions](AGENTS.md), and the optional [resource measurement guide](docs/measurement.md). No RAM or battery savings are claimed without measurements.
