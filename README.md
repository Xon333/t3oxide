# t3oxide

A Go terminal client for existing [T3 Code](https://github.com/pingdotgg/t3code) environments, built directly with **Bubble Tea v2**, Bubbles, Lip Gloss and Glamour. Read conversations, send messages, answer questions and approvals, and stop a reviewed active turn from your terminal. This is the primary client; the Swift/AppKit app remains available as a legacy rollback.

```text
Go CLI / Bubble Tea TUI
  └─ SSH or local stdio
      └─ on-demand Node adapter → existing T3 server
```

## Build and install

Build with **Go 1.26.0+**. Each T3 host needs Node.js 22.4+ and the host adapter; remote profiles use your existing OpenSSH configuration. The primary CI artifact targets Apple Silicon macOS.

```sh
bash scripts/build-go.sh
./dist/t3oxide envs
./dist/t3oxide
```

Follow [installation](NATIVE.md) to prepare the host adapter and environment profiles, then [Go setup and controls](NATIVE-GO.md) to install the executable on your PATH. Distributions include `NOTICE` and `LICENSES/` alongside the binary. Installation does not install, upgrade, start or restart T3.

## Features

- Markdown transcript, composer, command palette, conversation filtering and lineage navigation.
- Multiple environment profiles, bounded history pages, per-thread drafts and explicit recovery of uncertain writes.
- Current-thread and next-message model, reasoning and speed choices from the selected provider's advertised capabilities.
- Human-authored messages with queue/steer intent, Stop with queue hold, and review of T3 requests.
- Fixed violet, cyan and lime themes, plus a Terminal theme that inherits terminal foreground/background and uses ANSI palette accents. See [theme controls](NATIVE-GO.md#themes).
- Read-only `envs`, `threads`, `status` and `doctor` commands, with optional JSON output.

T3 owns providers, execution, durable conversations, queues and workspaces. Only the selected environment's helper runs; existing T3 work continues when the client disconnects. Full tool payloads, attachment uploads, provider configuration and full-history search remain in T3. See [model/navigation semantics](docs/model-navigation.md).

## Development and CI

On an Apple Silicon Mac with Go 1.26.0+, Node 22.4+, Python 3 and Apple command-line developer tools:

```sh
bash scripts/check-go.sh
bash scripts/check.sh
bash scripts/build-go.sh
```

The Go check script covers formatting, tests, vet and race detection. The shared check script retains adapter, script, measurement and available Swift checks. CI publishes the primary `t3oxide-terminal-macos-arm64` artifact and separately checks/builds the legacy Swift app as `t3oxide-legacy-swift-macos-arm64`.

See [architecture](docs/architecture.md), [Go implementation ownership](docs/go-terminal.md), [contributor instructions](AGENTS.md) and the [verification record](docs/verification.md) for pinned dependencies and recorded checks. The adapter targets T3 orchestration protocol 2; compatibility with future nightlies is not guaranteed. No RAM or battery savings are claimed without measurements.

## Legacy Swift rollback

The AppKit source remains in `mac/`. On an Apple Silicon Mac running macOS 14+ with Apple developer tools, build it with `bash scripts/build-mac.sh`. Outputs remain `dist/t3oxide.app` and `dist/t3oxide-macos-arm64.zip`; builds are ad-hoc signed and not notarized. See [legacy installation](NATIVE.md#legacy-swift-rollback).

Swift retains `~/.config/t3oxide/drafts.json`; Go uses the separate `go-drafts.json` and never edits the Swift file. Preserve both state files and host `operations-v1/` recovery journals. Returning to Swift does not transfer new Go drafts or pending actions; review unresolved work before changing clients.
