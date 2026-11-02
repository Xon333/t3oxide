# Installation and configuration

The Rust terminal client connects to existing T3 Code environments through a small host-local Node companion. The client does not start or own T3. A local profile launches the companion directly; a remote profile launches the same companion through an existing OpenSSH route.

## Build the client

Use a current stable Rust toolchain on Linux or macOS. `Cargo.toml` declares Rust 1.88; the exact toolchain used for validation is recorded in [verification](docs/rust-verification.md). There is no Go build or runtime requirement.

```sh
cargo build --locked --release
./target/release/t3oxide --version
./target/release/t3oxide --help
```

For a user-local installation:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 target/release/t3oxide "$HOME/.local/bin/t3oxide"
```

Ensure that directory is on your shell's `PATH`. Distributions must retain `NOTICE` and `LICENSES/`. Building or installing the executable does not update a host companion, alter T3 or open a connection.

## Reuse existing environment profiles

The default profile file remains `~/.config/t3oxide/environments.json`. Preserve an existing file. The Rust client reads the established version 1 format:

```json
{
  "version": 1,
  "environments": [
    {
      "id": "desk",
      "label": "Desk T3",
      "environmentId": "EXACT_NATIVE_ENVIRONMENT_ID",
      "sshDestination": null,
      "node": "/absolute/path/to/node",
      "adapter": "/absolute/path/to/t3oxide/current/main.mjs",
      "hostConfig": "/absolute/path/to/t3oxide/host.json",
      "enabled": true,
      "defaultForNew": true
    }
  ]
}
```

Use the actual absolute paths and environment identity returned by the host installer. The executable and configuration paths belong to that host. Set `sshDestination` to an existing SSH alias for a remote profile; leave it `null` for a local companion.

| Field | Meaning |
| --- | --- |
| `id` | Unique client profile identity used by `--env`. |
| `label` | Human-readable name. |
| `environmentId` | Exact native identity verified during every handshake. |
| `node`, `adapter`, `hostConfig` | Absolute paths on the adapter host. |
| `sshDestination` | Existing alias or destination; no command-line options. |
| `enabled` | Only enabled profiles participate. |
| `defaultForNew` | Optional explicit creation preference; it does not retarget saved operations. |
| `webURL` | Optional client-reachable HTTP(S) address, without credentials, query or fragment. |
| `allowedProviderInstanceIds` | Optional nonempty allowlist of exact provider instances. Absence permits the configured native instance; an empty list is rejected. |

New users can start from `config/environments.example.json`, replacing placeholders and enabling only configured sources. The client does not rewrite profiles. Remote connections use batch authentication, strict host-key checking and a bounded connection timeout. Confirm your SSH route and known host key independently; the client does not create an SSH key or configure a VPN.

```sh
t3oxide envs
t3oxide doctor --env desk --json
t3oxide --env desk
```

An unavailable host is reported independently. It does not make a healthy environment's metadata disappear or authorize a fallback mutation on another host.

## Prepare a new adapter host

This section is for a new host. Existing installations should preserve their identity, credential and operation journal and follow the update section below.

Use the ordinary account that owns the existing T3 instance, a checkout of this source, an existing Node **22.4 or newer** executable and the instance's loopback URL. The companion has no npm dependencies. It uses the host-local credential for authenticated native HTTP and WebSocket operations; credentials never belong in `environments.json`.

Create a dedicated credential through that installation's own T3 CLI, with `orchestration:read`, `orchestration:operate` and `filesystem:read` scopes. Consult the installed CLI's help because installation paths and supported flags vary. For versions supporting this command form, set `T3_CMD` to the installed executable and its instance options first:

```bash
umask 077
mkdir -p "$HOME/.config/t3oxide"
"${T3_CMD[@]}" auth session issue --token-only --label t3oxide --ttl 30d \
  --scope orchestration:read --scope orchestration:operate --scope filesystem:read \
  > "$HOME/.config/t3oxide/t3.token"
chmod 600 "$HOME/.config/t3oxide/t3.token"
```

The read scope for files supports native project defaults, refs and diff inspection. Keep the credential on the adapter host. Set `HOST_T3_BASE` to the existing instance's loopback origin, then explicitly choose the allowed write policy:

```sh
node scripts/install-host.mjs \
  --base-url "$HOST_T3_BASE" \
  --token-file "$HOME/.config/t3oxide/t3.token" \
  --enable-writes
```

Use `--read-only` in place of `--enable-writes` for read-only access. The installer verifies native identity, protocol and authenticated access before publishing a versioned companion under `~/.local/share/t3oxide/`. It keeps the host configuration private. Copy its output's `environmentId`, `node`, `adapter` and `hostConfig` values into the corresponding client profile, preserving exact spelling.

This is a host-local installation step, not a prerequisite for compiling or exercising fixture tests. The installer does not install Node, start T3 or replace native services.

## Appearance and interaction

Choose **Appearance** from the command palette, or launch with `--theme terminal|violet|cyan|lime`. Terminal uses your terminal palette; the other choices use fixed colors with consistent error, waiting and success roles. UI choices are saved in the Rust state file.

F2 changes Developer/AgentBox without changing the active source. F3 cycles Split, Focus and Wide. Compact terminals show the focused pane; Tab still moves among navigator, transcript, composer and Details. Use `--no-mouse` or **Toggle mouse** for native terminal text selection. Mouse capture otherwise enables conversation selection, pane focus, transcript scroll and Unicode-aware composer selection.

The composer uses Enter for newlines and Ctrl+] for send. `VISUAL` or `EDITOR` supplies an external editor through Ctrl+E; the edited text returns to the original conversation and remains a draft. An editor failure preserves the original draft and reports the retained temporary file when needed.

See [shortcuts](docs/shortcuts.md) for all contexts. No shortcut configuration file is needed for the default workspace.

## State and migration

The defaults are:

| Data | Location and owner |
| --- | --- |
| Profiles | `~/.config/t3oxide/environments.json`, read by the Rust client. |
| Rust drafts, recovery and UI preferences | `~/.config/t3oxide/rust-state.json`, exclusively locked by one Rust client. |
| Host credential and configuration | Private files on each adapter host. |
| Exact adapter operation receipts | `operations-v1/` beside the host configuration. |
| Threads, runs, permissions, worktrees and queues | Native T3. |

`--config PATH` and `--state PATH` select explicit client files. A custom state path is useful for an isolated trial; keep it in a separate directory if you want an empty workspace without importing previous terminal state.

When the Rust state file does not exist, the client validates the sibling `go-drafts.json` and supported prompt/inbox sidecars, takes a shared source lock and writes the imported data to the Rust file. The original files' contents are retained. Close the previous client before migration. If the Rust file already exists, it is authoritative and is not reimported. Invalid or unsupported state fails explicitly; it is not replaced with an empty object. Unknown fields survive supported transactions.

Compose and launch edits save after a short idle interval and are flushed before leaving the relevant form, changing source or quitting. Each composer draft is limited to 64 KiB and 9,999 lines. Ordinary operation and Stop recovery records are separate. After a save error, correct the filesystem problem and retry leaving the form; a normal Ctrl+Q will not silently discard edits.

A saved uncertain operation may already have taken effect. Open **Recover saved operations**, review its complete original environment, identity and payload, and use **Reconcile original action**. The client never reconnects and silently resends. Clearing a local record is a separate explicit choice after checking the native result; it does not undo T3 work.

## Updates and verification on your hosts

Rebuild the Rust executable from the desired revision. The bundled companion advertises inclusive child metadata; update it explicitly to expose collapsed child summaries to local filtering. An older compatible companion can still provide its advertised operations and native search. To update a configured companion, run `node scripts/install-host.mjs` on that host from the same source revision. With no policy/identity flags, the installer retains the existing configuration, token-file path and write setting and publishes a versioned target. Reconnect the client to load the new modules. Retain the previous compatible target and any installer lock until publication is complete or reviewed.

Preserve all unresolved client records and `operations-v1/`. Its bounded durable receipts are recovery records, not a disposable cache. Replacing the frontend does not acknowledge or resolve an operation. Credential renewal should preserve the intended environment and session subject; verify the replacement before retiring the existing credential.

Run `doctor` for exact identity and metadata access, then open a conversation and observe a live update. Where writes are enabled, use a disposable native conversation to verify send, Stop and requests under that host's offered capabilities. Those checks require the actual local credential and SSH/terminal environment. The [verification record](docs/rust-verification.md) identifies which results were obtained with fixtures and which remain host-local acceptance checks.
