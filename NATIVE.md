# Installation

The client connects to an existing T3 environment through an on-demand host adapter. Installation does not install, start, upgrade, or restart T3, Node.js, SSH, or a VPN.

## 1. Prepare each host

Use a local checkout of this repository and the ordinary account that owns the T3 instance. Resolve its existing Node.js 22.4+ executable and loopback T3 URL.

Create a dedicated host-local credential using the **installed T3 CLI for that instance**, with `orchestration:read` and `orchestration:operate` scopes. CLI flags may vary by nightly; consult that installation's help. For versions supporting the following syntax, first set the Bash array `T3_CMD` to the installed executable and its instance-specific options:

```bash
umask 077
mkdir -p "$HOME/.config/t3oxide"
"${T3_CMD[@]}" auth session issue --token-only --label t3oxide --ttl 30d \
  --scope orchestration:read --scope orchestration:operate \
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

## 2. Build and configure the Mac client

On an Apple Silicon Mac with macOS 14+ and the Apple developer tools:

```sh
bash scripts/build-mac.sh
mkdir -p "$HOME/Applications" "$HOME/.config/t3oxide"
cp -R dist/t3oxide.app "$HOME/Applications/"
cp config/environments.example.json "$HOME/.config/t3oxide/environments.json"
```

Edit `environments.json` before opening the app:

- Give each profile a unique `id` and a descriptive `label`.
- Fill its environment identity and executable/configuration paths from the host installer output.
- Set `sshDestination` to an existing SSH alias, or `null` for a local adapter.
- Set `enabled` to `true` for configured profiles; remove unused examples.
- Optionally set `webURL` to a full T3 URL reachable **from the client**, without credentials, query, or fragment. Omit it when no browser route is available.

Remote connections use batch authentication and strict host-key checking. Establish SSH access first. A host's loopback URL is not remotely reachable unless an existing forward exposes it on the client; T3 Connect browser access is not itself an SSH route.

Open `~/Applications/t3oxide.app`. The build is ad-hoc signed and not notarized; do not disable Gatekeeper globally. The ZIP at `dist/t3oxide-macos-arm64.zip` contains the same application.

## 3. Verify the connection

Check the selected environment identity, open a thread, and receive a live update. If writes are enabled, use a disposable conversation to verify a harmless send. See the [verification record](docs/verification.md) for the cases exercised against tested installations.

Switching environments or closing the app ends only the owned helper connection. Existing T3 work continues independently.

## Updates and recovery

Rerun the host installer from the desired source revision to update the adapter. It selects a versioned release through the `current` symlink; restoring a previous compatible target rolls back the adapter without changing T3. Rebuild the client when native source changes.

Renew expired credentials through the same installed T3 CLI, retain the private token path, and reconnect. Reconnect does not automatically replay writes: retry saved uncertain operations explicitly.

Preserve `~/.config/t3oxide/drafts.json` on the client and `operations-v1/` beside the host configuration. The host journal contains original operation payloads and receipts, not tokens; it is recovery state, not disposable cache. Its 4,096-record limit fails closed for new operations rather than evicting recovery records.

To disable writes, rerun the installer with `--read-only`. When decommissioning, revoke only the dedicated companion credential through the installed CLI and preserve any unresolved recovery state. Do not remove the T3 installation or its data.
