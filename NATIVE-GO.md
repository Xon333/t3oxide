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
| Commands / contextual help | **F3** / **F1** or `/help`; Ctrl+P also opens commands in an empty composer or the transcript |
| Conversations / environments | **F6** / `/env`; Ctrl+T also opens conversations in an empty composer or the transcript |
| Filter conversation titles/projects, including collapsed children | `/threads WORDS`; Ctrl+F outside filter editing in the chooser |
| Expand / collapse subagents; choose thread types | Space or Right / Left; F6 in the chooser |
| Parent conversation | `/parent` or Option/Alt+Up |
| Current thread model / reasoning / speed | Option/Alt+M / E / S, or `/model`, `/effort`, `/speed` |
| Presets; next-message selection; runtime evidence | `/presets`; `/model next`, `/effort next`, `/speed next`; `/override`; `/runtime` |
| Send / newline | **Enter** / **Ctrl+J**; Option/Alt+Enter and distinct Shift+Enter also insert a newline |
| Slash completion / literal slash text | Type `/`, Up/Down selects, Tab completes; Enter accepts a partial name before execution. **Ctrl+Enter** sends literal text, including a known command name |
| Exact text / prompt recall | **F4** opens `$VISUAL`, then `$EDITOR`, then `vi`; **F5** recalls prompts |
| Clear / interrupt | **Ctrl+X** clears into recall; Ctrl+C dismisses a surface or keeps a nonempty draft (empty: exit) |
| Delivery | `/delivery auto`, `/delivery queue`, `/delivery steer` |
| Stop reviewed turn and hold queue | **Ctrl+S**, including inside requests and details; `/stop` |
| Review T3 request / full question | **Ctrl+R** / **F2** in the picker |
| Complete multiple selection | **Tab**, then **Enter** on the fixed Continue control; Ctrl+Enter also works, including with zero filter matches |
| Read / write | **Tab** switches transcript/composer focus; PgUp/PgDown scroll the focused component |
| Earlier page / Latest | `/earlier` / `/latest`; Option/Alt+Left/Right only with transcript focus |
| Search current page | `/search TEXT`, or Ctrl+F with transcript focus; n/N moves between matches there |
| Copy | `/copy [page|draft|answer|error|detail]`; Ctrl+Y copies the current detail or preserved answer/draft, otherwise the last assistant message |
| Error details | **F2** or `/error`; **F8** opens errors while F2 reviews an exact draft or preserved answer. Ctrl+Y copies an open detail |
| New conversation / recovery | `/new [TITLE]`; `/pending`, `/retry [stop]`, `/resolve [stop]`, `/reconnect` |
| Theme / mouse / sidebar | `/theme`, `/mouse [on|off]`, `/sidebar [on|off]` |
| Full T3 / local exit | `/web` displays the configured address; **Ctrl+Q** or `/quit` exits. Ctrl+D exits only from an empty composer or transcript |

Focused editors retain Bubbles' editing keys: Ctrl+A/E line start/end, Ctrl+B/F character movement, Ctrl+P/N line movement, Ctrl+T transpose, Ctrl+D forward delete, and decoded Alt+Left/Right word movement. In a filter, Ctrl+P/N moves between results without resetting the query. F3 over a request or page-search editor opens help and preserves that editor.

On macOS, ⌥ means Option. Terminal mappings determine which key events arrive; the supplied Ghostty launcher uses left Option as Alt. Physical Option+Arrow and decoded Alt-arrow events are separate test cases. Palette and slash alternatives remain available.

Unknown slash-prefixed text, including absolute paths, sends as ordinary text. Known single-line command names execute after validation; invalid local arguments remain editable, and remote rejection retains the exact command. Ctrl+Enter sends even a known slash name literally.

Input is bounded to 64 KiB and 9,999 editable lines. Text Bubbles would change is rejected as a whole, with the previous draft intact and the attempted bytes recoverable in F5. Recall selects the exact buffer; F4 edits it externally and Enter sends within bounds. External editor results are never trimmed or normalized. An exact buffer stays outside the normalizing textarea; F2 reviews it and `/copy draft` copies its original bytes. Failed or stale editor returns retain unique attempts; oversized or invalid UTF-8 results retain their private file, whose location is shown in the error details.

Prompt recall contains up to 24 acknowledged prompts, explicit clears and rejected attempts, scoped to the same environment and thread, with a 512 KiB total text budget. Opening or canceling recall leaves the live draft intact; accepting an entry preserves any displaced live draft in recall. A request's uneditable initial answer keeps its original review/copy and explicit Ctrl+U replacement path.

Mouse capture provides transcript scrolling; `/mouse off` enables native terminal selection. Click-to-focus is not implemented. Clipboard behavior depends on the terminal; copy also uses the existing desktop clipboard helper. Local dismissal and exit never mean remote Stop.

`t3oxide envs`, `threads`, `status` and `doctor` are read-only. Use `--json` for machine-readable results. `--help` describes `--config`, `--state`, `--env`, `--thread`, `--theme` and `--no-mouse`.

## Themes

Use `/theme terminal` or launch with `t3oxide --theme terminal` to inherit your terminal's default foreground and background. UI accents and code/diff highlighting use its ANSI palette, so Ghostty's configured colors apply without reading its configuration. Selections use inverse colors for light and dark backgrounds. The fixed violet, cyan and lime themes retain their existing colors and Dracula syntax highlighting; violet remains the default. Your choice is stored in `terminal.json` beside the Go state file. No theme watcher or terminal configuration changes are required.

## State and recovery

The first normal Go launch imports validated Swift `drafts.json` into `go-drafts.json` only when the Go file is absent. It never edits the Swift file. The Go state file includes optional preset/override fields and the `select-model` operation, with a single-owner lock and existing recovery records. Preserve the Go file and its `.prompts.json` recall compatibility file when replacing executables; do not run an older Go build that does not understand these fields and operations against it. Swift rollback retains its separate, untouched state file and does not import new Go drafts or pending actions.

Prompt recall has a targeted compatibility copy at the explicit Go state path plus `.prompts.json`, under the same process lock. It protects recall from older Go clients that drop unknown main-file fields. Interrupted publication can retain up to 48 entries/1 MiB of protected text; the encoded copy is capped at 6 MiB. Invalid or unresolved copies fail closed. It contains no executable operations, and the main file alone owns pending actions and Stop records. Keep both files together for migration or restore.

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
