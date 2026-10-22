# Go terminal client

The primary t3oxide client uses Bubble Tea v2 directly, with Bubbles controls, Lip Gloss styling and Glamour Markdown. It connects through the existing T3oxide Node adapter. The retained Swift/AppKit client is a legacy rollback with separate state.

## Build and open

Build prerequisite: **Go 1.26.0+**. Native verification uses Go 1.27.2 on Apple Silicon; see [verification](docs/verification.md). Run the full pinned-toolchain checks below before adoption. The first build downloads pinned Go modules. The existing host adapter requires Node 22.4+; remote profiles use your normal OpenSSH configuration.

```zsh
cd /path/to/source
bash scripts/build-go.sh
./dist/t3oxide envs
./dist/t3oxide
```

The client reads the existing `~/.config/t3oxide/environments.json`. For a new installation, copy `config/environments.example.json` there and fill it with the exact environment identity, SSH alias and absolute adapter paths. The [installation guide](NATIVE.md) covers host preparation and profile configuration. **Model and navigation controls require the updated host adapter on each connected host.** From a host-local checkout, run the existing installer with that host's existing Node binary and configuration:

```zsh
node scripts/install-host.mjs
```

With no flags it preserves existing identity, token-file path and write setting, probes the existing loopback server, installs the seven adapter modules as a versioned release, and repoints only the adapter symlink. Existing `operations-v1` receipts are retained. An interrupted installer leaves `.install-lock` beside `host.json`; preserve it until that installer has finished or its interrupted publication has been reviewed. It does not install, update, start or restart T3. Use the installer output to verify the exact adapter path in existing environment profiles; do not invent a replacement identity or credential. Existing helpers should be detached/reconnected to load the new modules. An older adapter still supports its original functions but the new controls are unavailable.

After native acceptance, install the single executable in a directory on your zsh PATH, for example:

```zsh
mkdir -p "$HOME/.local/bin"
install -m 755 dist/t3oxide "$HOME/.local/bin/t3oxide"
```

Keep the legacy Swift app available for rollback. Use one client at a time during adoption.

When distributing the executable, include `NOTICE` and `LICENSES/` alongside it.

## Controls

| Action | Control |
|---|---|
| Command palette / shortcut help | **Ctrl+P** / `/help` |
| Conversations / environments | **Ctrl+T** / **Ctrl+E**; `/switch` also works |
| Filter conversation title/project, including collapsed children | `/threads SEARCH WORDS`; **Ctrl+F** inside the chooser |
| Expand / collapse subagents | **Space** or **Right** / **Left** in thread chooser |
| All / Main / Subagents / Forks | **F6** in chooser, or `/filter` |
| Jump to authoritative parent | **Alt+Up** or `/parent` |
| Current thread model / reasoning / speed | **Alt+M** / **Alt+E** / **Alt+S**, or `/model`, `/effort`, `/speed` |
| Presets / favorites | `/presets`; `/presets save NAME` saves the current choice |
| Stage next message model / effort / speed | `/model next`, `/effort next`, `/speed next` |
| Inspect / clear staged choice | `/override`, `/override clear` |
| Saved selection vs runtime evidence | `/runtime` |
| Submit / insert newline | **Enter** / **Ctrl+J** (Alt+Enter is also supported) |
| T3 delivery intent | `/delivery auto`, `/delivery queue`, `/delivery steer` |
| Stop reviewed turn and hold queue | **Ctrl+S**, including while a request is open; `/stop` |
| Review approval or question | **Ctrl+R** or `/requests`; Escape dismisses locally |
| Read full selected request/choice details | **F2** in the picker; Escape returns without answering |
| Focus transcript/composer | **Tab** |
| Earlier page / live Latest | **Alt+Left** / **Alt+Right**, or `/earlier` / `/latest` |
| Search displayed page | **Ctrl+F** or `/search TEXT` |
| Copy last assistant message / displayed page | **Ctrl+Y** or `/copy`; `/copy page` |
| New conversation in current workspace | `/new`, optionally followed by a title |
| Inspect / retry saved action | `/pending`; `/retry` or `/retry stop` |
| Explicitly resolve local retry record | `/resolve` or `/resolve stop`, after reviewing T3 |
| Reconnect without replay | `/reconnect` |
| Theme | `/theme` → terminal, violet, cyan or lime; choice persists |
| Mouse capture / native selection | `/mouse`; `/mouse off` restores native terminal selection |
| Optional context sidebar | **Ctrl+B** |
| Full T3 address | `/web` displays the configured URL |
| Quit locally | **Ctrl+D** or `/quit`; Ctrl+C clears nonempty input, then quits when empty |

Clipboard and selection behavior depends on the terminal; the UI exposes copy controls and standard paste through Bubbles. Test your terminal/tmux combination during native acceptance. Quitting or canceling locally never means remote Stop.

Input is bounded to 64 KiB and 9,999 editable lines. Oversized edits and pasted text that Bubbles would normalize are rejected as a whole, including through Ctrl+V. A restored draft that Bubbles would truncate or normalize remains preserved verbatim in a read-only state: F2 inspects it, Ctrl+Y copies it, and Ctrl+C explicitly clears it. A provider's initial answer has the same protection; Ctrl+U explicitly replaces it or Escape dismisses the request. Neither is silently shortened or submitted as altered text.

`t3oxide envs`, `threads`, `status` and `doctor` are read-only. Use `--json` for machine-readable results. `--help` describes `--config`, `--state`, `--env`, `--thread`, `--theme` and `--no-mouse`.

## Themes

Use `/theme terminal` or launch with `t3oxide --theme terminal` to inherit your terminal's default foreground and background. UI accents and code/diff highlighting use its ANSI palette, so Ghostty's configured colors apply without reading its configuration. Selections use inverse colors for light and dark backgrounds. The fixed violet, cyan and lime themes retain their existing colors and Dracula syntax highlighting; violet remains the default. Your choice is stored in `terminal.json` beside the Go state file. No theme watcher or terminal configuration changes are required.

## State and recovery

The first normal Go launch imports validated Swift `drafts.json` into `go-drafts.json` only when the Go file is absent. It never edits the Swift file. The Go state file includes optional preset/override fields and the `select-model` operation, with a single-owner lock and existing recovery records. Preserve the Go file when replacing executables; do not run an older Go build that does not understand these fields and operations against it. Swift rollback retains its separate, untouched state file and does not import new Go drafts or pending actions.

Drafts remain until an authoritative acknowledgment; newer typing survives delayed replies and switches. Normal actions and Stop have independent saved-operation lanes. A saved retry uses the original ID, payload and run/request target. Reconnect does not replay it. Damaged state fails closed rather than resetting recovery history.

Earlier replaces the visible page and pauses live detail. Reconnect keeps a cached earlier page; use Latest to resume live data. Stop can target the last reviewed active run from history, but the adapter rejects a changed run rather than interrupting newer work.

The adapter supplies bounded messages, notices, request controls and attachment counts. Model/effort/tier controls now use the selected provider's advertised choices. Full tool payloads, attachment uploads, provider switching/configuration and full-history search stay in T3. Current-page search and host-side thread filtering are separate controls.

## Model scope

Controls default to **apply to current thread**. Explicit `next` choices are persisted with that thread's composer and submitted with one message. **T3 may adopt the dispatched override as its saved thread preference.** The client clears only the acknowledged local override; it never sends a hidden restoration write. Presets are environment-scoped and revalidated before use. `/runtime` distinguishes requested settings from provider-reported evidence; absent effective effort is not guessed. See [extension semantics](docs/model-navigation.md).

## Source checks

```zsh
bash scripts/check-go.sh
bash scripts/check.sh
GOOS=darwin GOARCH=arm64 T3OXIDE_OUTPUT=dist/t3oxide-darwin-arm64 bash scripts/build-go.sh
```

Complete tests need local Node 22.4+ and Python 3; the optional original `python3 tests/loopback_fixture.py` also uses `aiohttp`. Python is not needed by the built client. Test fixtures use disposable loopback hosts and temporary configuration. No test uses your fleet profiles.

CI treats the Go build as primary and uploads `t3oxide-terminal-macos-arm64` with its notices. Adapter checks and the legacy Swift model/transport checks and AppKit build remain enabled; the Swift artifact is named `t3oxide-legacy-swift-macos-arm64`.

In a restricted Go 1.23.2+ environment, `bash scripts/check-portable-core.sh` (also `--race` or `--vet`) checks the actual core and real Node adapter using a temporary module file. It does **not** validate the Charm UI/CLI build and never changes production dependency pins.
