# Installation: Go terminal client

The primary Go/Bubble Tea client connects to an existing T3 environment through an on-demand host adapter. The legacy Swift app uses the same adapter and environment profiles, with separate client state. Installation does not install, start, upgrade, or restart T3, Node.js, SSH, or a VPN.

## 1. Prepare each host

Use a local checkout of this repository and the ordinary account that owns the T3 instance. Resolve its existing Node.js 22.4+ executable and loopback T3 URL.

For an already configured host, preserve its credential and identity and follow [Updates and recovery](#updates-and-recovery). The credential setup below is for a new installation.

Create a dedicated host-local credential using the **installed T3 CLI for that instance**, with `orchestration:read`, `orchestration:operate` and `filesystem:read` scopes. The read scope lets native launch review inspect the project's `t3.json`, repository refs and changes. CLI flags may vary by nightly; consult that installation's help. For versions supporting the following syntax, first set the Bash array `T3_CMD` to the installed executable and its instance-specific options:

```bash
umask 077
mkdir -p "$HOME/.config/t3oxide"
"${T3_CMD[@]}" auth session issue --token-only --label t3oxide --ttl 30d \
  --scope orchestration:read --scope orchestration:operate --scope filesystem:read \
  > "$HOME/.config/t3oxide/t3.token"
chmod 600 "$HOME/.config/t3oxide/t3.token"
```

Keep the token on the host; do not commit it or copy it into client configuration. Set `HOST_T3_BASE` to the existing loopback origin, then run:

```sh
node scripts/install-host.mjs \
  --base-url "$HOST_T3_BASE" \
  --token-file "$HOME/.config/t3oxide/t3.token" \
  --enable-writes
```

Use `--read-only` instead of `--enable-writes` to disable mutations. The installer verifies environment identity, protocol, and authenticated access, installs a versioned adapter under `~/.local/share/t3oxide/`, and writes private configuration at `~/.config/t3oxide/host.json`.

Save the output fields `environmentId`, `node`, `adapter`, and `hostConfig` for the client profile. Use the exact absolute paths returned by the installer.

## 2. Build and configure the Go client

With Go 1.26.0+ installed:

```sh
bash scripts/build-go.sh
mkdir -p "$HOME/.local/bin" "$HOME/.config/t3oxide"
install -m 755 dist/t3oxide "$HOME/.local/bin/t3oxide"
```

For a new installation only, create the profiles file:

```sh
cp config/environments.example.json "$HOME/.config/t3oxide/environments.json"
```

Preserve an existing `environments.json`. Ensure `~/.local/bin` is on your shell's PATH. Edit the profiles before opening the client:

- Give each profile a unique `id` and a descriptive `label`.
- Fill its environment identity and executable/configuration paths from the host installer output.
- Set `sshDestination` to an existing SSH alias, or `null` for a local adapter.
- Set `enabled` to `true` for configured profiles; remove unused examples.
- Optionally set `webURL` to a full T3 URL reachable **from the client**, without credentials, query, or fragment. Omit it when no browser route is available.

Remote connections use batch authentication and strict host-key checking. Establish SSH access first. A host's loopback URL is not remotely reachable unless an existing forward exposes it on the client; T3 Connect browser access is not itself an SSH route.

Run `t3oxide envs` to inspect configured profiles, then `t3oxide` to open the TUI. See [Go controls and themes](NATIVE-GO.md). The primary CI artifact, `t3oxide-terminal-macos-arm64`, contains the Apple Silicon executable and notices. Include `NOTICE` and `LICENSES/` when redistributing it.

## 3. Verify the connection

Check the selected environment identity, open a thread, and receive a live update. If writes are enabled, use a disposable conversation to verify a harmless send. See the [verification record](docs/verification.md) for the cases exercised against tested installations.

Switching environments or quitting the client ends only the owned helper connection. Existing T3 work continues independently.

## Updates and recovery

Rerun the host installer from the desired source revision to update the adapter. With no flags, `node scripts/install-host.mjs` preserves the configured identity, token-file path and write setting. It selects a versioned release through the `current` symlink; restoring a previous compatible target rolls back the adapter without changing T3. Preserve an active or interrupted `.install-lock` until its installer has finished or its publication has been reviewed. Reconnect existing helpers to load updated modules. Rebuild and replace the Go executable when client source changes.

Renew expired credentials through the same installed T3 CLI, retain the private token path and original session subject, and reconnect. Existing credentials with only the two orchestration scopes can still browse conversations; native project-file, refs and change reads may fail with a missing `filesystem:read` scope. Preserve the existing credential until its same-subject replacement has been verified against the intended environment. Reconnect does not automatically replay writes: retry saved uncertain operations explicitly.

Preserve `~/.config/t3oxide/go-drafts.json` and appearance settings in `terminal.json` on the client, any legacy `drafts.json`, and `operations-v1/` beside the host configuration. The first normal Go launch imports validated Swift drafts only when the Go state file is absent; it never edits the Swift file. The host journal contains original operation payloads and receipts, not tokens; it is recovery state, not disposable cache. Its 4,096-record limit fails closed for new operations rather than evicting recovery records.

To disable writes, rerun the installer with `--read-only`. When decommissioning, revoke only the dedicated companion credential through the installed CLI and preserve any unresolved recovery state. Do not remove the T3 installation or its data.

## Legacy Swift rollback

The retained Swift/AppKit source is in `mac/`; it is no longer the primary client. On an Apple Silicon Mac with macOS 14+ and Apple developer tools:

```sh
bash scripts/build-mac.sh
mkdir -p "$HOME/Applications"
cp -R dist/t3oxide.app "$HOME/Applications/"
```

Open `~/Applications/t3oxide.app` using the existing environment profiles. The build is ad-hoc signed and not notarized; do not disable Gatekeeper globally. `dist/t3oxide-macos-arm64.zip` contains the same app; CI labels its artifact `t3oxide-legacy-swift-macos-arm64`.

Keep `drafts.json` separate from `go-drafts.json`. Swift rollback does not import new Go drafts, presets or pending operations. Review unresolved work before switching and use one client at a time during adoption. Retain both state files and the host journals; replacing the frontend does not resolve or replay outstanding writes.
