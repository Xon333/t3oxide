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

The client reads the existing `~/.config/t3oxide/environments.json`. For a new installation, copy `config/environments.example.json` there and fill it with the exact environment identity, SSH alias and absolute adapter paths. The [installation guide](NATIVE.md) covers host preparation and profile configuration. **The refined launch controls and smaller latest projection require the updated adapter on each host.** From a host-local checkout of the reviewed source revision, run its installer with that host's existing Node binary and configuration:

```zsh
node scripts/install-host.mjs
```

With no flags it preserves existing identity, token-file path and write setting, probes the existing loopback server, installs the seven adapter modules as a versioned release, and repoints only the adapter symlink. Existing `operations-v1` receipts are retained. An interrupted installer leaves `.install-lock` beside `host.json`; preserve it until that installer has finished or its interrupted publication has been reviewed. It does not install, update, start or restart T3. Use the installer output to verify the exact adapter path in existing profiles; retain the existing identity and credential. Helpers load new modules on their normal reconnect. An older adapter retains its original functions; launch-selection editing is offered only when the adapter advertises support from the existing T3 endpoint.

After native acceptance, install the single executable in a directory on your zsh PATH, for example:

```zsh
mkdir -p "$HOME/.local/bin"
install -m 755 dist/t3oxide "$HOME/.local/bin/t3oxide"
```

Keep the legacy Swift app available for rollback. Use one client at a time during adoption.

When distributing the executable, include `NOTICE` and `LICENSES/` alongside it.

## Browse and create

Without `--thread`, the client starts in the browser. Environments selects one or more configured profiles; Enter toggles a row and Tab reaches Continue. Selected profiles remain checked when filtering hides them. Each selected environment has a tab; Tab/Shift+Tab reaches its tab and page actions, and Enter activates the focused action. Loading a tab or choosing Refresh reads bounded conversation metadata. Moving the cursor does not open a transcript. Typing filters the loaded rows; `/threads WORDS` or the browser's Search action asks the host for matching titles/projects, including rows outside the current bounded list.

Open the browser from a conversation with Ctrl+O or `/switch`. The conversation stays attached, its active work continues, and its composer and reading position are retained. Esc or Resume returns to it. Only explicitly opening an offered thread changes the attached conversation. New, Settings, Refresh and agent/thread-type actions are available in the browser. Select an exact conversation before New or conversation settings so its environment and workspace are known; a project label alone is not a launch target.

New or `/new [TITLE]` opens one review with a multiline first message and single-line title. Tab moves from the message to the title, then Editor, supported Model/Effort/Tier controls, Create and Cancel; Shift+Tab moves back. Enter inserts a newline in the first message, leaves the title editor, or activates the focused control. **Create is the explicit submission action.** An empty title defaults to `New conversation`. The title is limited to 512 bytes and the first message to 64 KiB; payload text is preserved without trimming.

Launch settings are local to that review. Changing them does not configure the source thread or change its next-message override. The new conversation uses the selected source's exact existing project/workspace, provider/account and modes; it does not copy history or create a new checkout. Choosing a different launch model uses that model's advertised defaults, and Create revalidates the selection. When explicit launch selection is unavailable, the review explains that the source model is inherited. Cancel and local exit preserve the separate launch message draft and leave the source composer intact. Title and selection survive review round trips in the current client session; the launch message uses the durable separate launch-draft slot, including external-editor returns and context shutdown.

## Controls

| Action | Control |
|---|---|
| Commands / contextual help | **Tab to reading, then Ctrl+P** / `/help`; Ctrl+P also opens commands from an empty composer |
| Browser / environments | **Ctrl+O** or `/switch` / `/env`; Esc or Resume returns to the attached conversation |
| Browser actions / environment tabs | **Tab/Shift+Tab**, then Enter; Left/Right moves between actions only while their strip owns focus |
| Filter conversation titles/projects, including collapsed children | Type in the browser to filter loaded rows; `/threads WORDS` or browser Search queries the host |
| Expand / collapse subagents; choose thread types | Space or Right / Left when the query editor is not active; browser Agents / threads action |
| Parent conversation | `/parent`; Option/Alt+Up with transcript focus |
| Current thread settings | `/settings`, or `/model`, `/effort`, `/speed`; Option/Alt+M / E / S only with transcript focus |
| Presets; next-message selection; runtime evidence | `/presets`; `/model next`, `/effort next`, `/speed next`; `/override`; `/runtime` |
| Send / newline | **Enter** / **Ctrl+J**; Option/Alt+Enter and distinct Shift+Enter also insert a newline |
| Slash completion / literal slash text | Type `/`, Up/Down selects, Tab completes; Enter accepts a partial name before execution. **Ctrl+Enter** sends literal text, including a known command name |
| Exact text / prompt recall | `/edit` opens `$VISUAL`, then `$EDITOR`, then `vi`; `/recall` opens prompt history |
| Clear / interrupt | **Ctrl+X** clears into recall; Ctrl+C dismisses a surface or keeps a nonempty draft (empty: exit) |
| Delivery | `/delivery auto`, `/delivery queue`, `/delivery steer` |
| Stop reviewed turn and hold queue | **Ctrl+S**, including inside requests and details; `/stop` |
| Review T3 request / full question | **Ctrl+R** or `/requests` / **Ctrl+L** in the picker |
| Complete multiple selection | **Tab**, then **Enter** on the fixed Continue control; Ctrl+Enter also works, including with zero filter matches |
| Read / write | **Tab** switches transcript/composer focus; PgUp/PgDown scroll the focused component |
| Earlier page / Latest | `/earlier` / `/latest`; Option/Alt+Left/Right only with transcript focus |
| Search current page | `/search TEXT`, or Ctrl+F with transcript focus; n/N moves between matches there |
| Copy | `/copy [page\|draft\|answer\|error\|detail]`; Ctrl+Y copies the current detail or preserved answer/draft, otherwise the last agent message |
| Details / errors | **Ctrl+L** or `/details` reviews the current detail; `/error` opens errors when an exact draft/answer has detail priority. Ctrl+Y copies an open detail |
| New conversation / recovery | `/new [TITLE]`; `/pending`, `/retry [stop]`, `/resolve [stop]`, `/reconnect` |
| Theme / mouse / sidebar | `/theme [default\|terminal\|violet\|cyan\|lime]`, `/mouse [on\|off]`, `/sidebar [on\|off]` |
| Full T3 / local exit | `/web` displays the configured address; **Ctrl+Q** or `/quit` exits. Ctrl+D exits only from an empty composer or transcript |

The composer retains Bubbles' editing keys: Ctrl+A/E line start/end, Ctrl+B/F character movement, Ctrl+P/N line movement, Ctrl+T transpose, Ctrl+G Select All, Ctrl+D forward delete, and decoded Alt+Left/Right word movement. Option letters and Shift selection stay with focused editors. In a filter, Up/Down, PageUp/PageDown and Ctrl+P/N move through results; Left/Right edits the query. The highlighted row, selection detail and Enter refer to the same result. Checked membership is separate from the current row.

Ctrl+Space and decoded Ctrl+@ are also bound to Commands; a terminal or OS input-source shortcut may intercept them. Recorded Ghostty acceptance confirmed portable Tab → Ctrl+P, while macOS intercepted physical Ctrl+Space and the attempted physical Ctrl+@ chord did not open Commands. Use the portable route when those chords are unavailable. Over a request or page-search editor, the Commands binding opens contextual help and preserves the editor. F1/F2/F3/F4/F5/F6/F8 remain compatible aliases for Help/Details/Commands/Editor/Recall/Browser/Error; no function key is required. Ctrl+T remains a browser alias from an empty composer or reading focus. Ctrl+E opens environments only with reading focus.

On macOS, ⌥ means Option. Terminal mappings determine which key events arrive; the supplied Ghostty launcher uses left Option as Alt. Physical Option+Arrow and decoded Alt-arrow events are separate test cases. Palette and slash alternatives remain available.

Unknown slash-prefixed text, including absolute paths, sends as ordinary text. Known single-line command names execute after validation; invalid local arguments remain editable, and remote rejection retains the exact command. Ctrl+Enter sends even a known slash name literally.

Input is bounded to 64 KiB and 9,999 editable lines. Text Bubbles would change is rejected as a whole, with the previous draft intact and the attempted bytes recoverable in `/recall`. Recall selects the exact buffer; `/edit` edits it externally and Enter sends within bounds. External editor results are never trimmed or normalized. An exact buffer stays outside the normalizing textarea; Ctrl+L reviews it and `/copy draft` copies its original bytes. Failed or stale editor returns retain unique attempts; oversized or invalid UTF-8 results retain their private file, whose location is shown in the error details.

Prompt recall contains up to 24 acknowledged prompts, explicit clears and rejected attempts, scoped to the same environment and thread, with a 512 KiB total text budget. Opening or canceling recall leaves the live draft intact; accepting an entry preserves any displaced live draft in recall. A request's uneditable initial answer keeps its original review/copy and explicit Ctrl+U replacement path.

Mouse capture provides transcript scrolling; `/mouse off` enables native terminal selection. Click-to-focus is not implemented. Clipboard behavior depends on the terminal; copy also uses the existing desktop clipboard helper. Local dismissal and exit never mean remote Stop.

`t3oxide envs`, `threads`, `status` and `doctor` are read-only. Use `--json` for machine-readable results. `--help` describes `--config`, `--state`, `--env`, `--thread`, `--theme` and `--no-mouse`.

## Themes

`/theme` or Settings → Appearance changes the active browser profile or attached environment. Choose Environment default to clear that profile's override, or use `/theme default`. Defaults derive distinct location/selection accents for up to eight configured profile IDs, independent of browse order. The existing saved global theme remains the base; its initial value is violet. Run/receipt activity, warnings and errors keep their semantic colors across environment accents. Environment labels and status text remain visible without color.

Use `/theme terminal` for the current profile to inherit your terminal's default foreground/background and ANSI palette, including syntax highlighting. Selections use inverse colors for light and dark backgrounds. Fixed violet, cyan and lime remain available with Dracula syntax highlighting. A saved global Terminal theme remains Terminal across profiles rather than adding fixed environment colors.

Appearance is stored in `terminal.json` beside the Go state file. `--theme terminal` (or another supported theme) is a nonpersistent launch override; it does not replace saved profile choices. A successful explicit theme change clears that override. No theme watcher or terminal configuration changes are required.

## Status and reading limits

The conversation header separates authoritative run state and unanswered requests from connection/synchronization, delivery mode and the current send receipt. Acknowledged means T3 accepted the operation; it does not mean an agent has responded. Responding requires both the send's initiating-message/run association and new or changed agent output explicitly belonging to that same run. Completion/failure/interruption comes from that run. A late acknowledgment cannot replace already observed response/completion, and activity from an older run does not prove a queued message started. If either correlation identity is absent, the receipt stays conservative. An ordinary send never dismisses a still-offered T3 request.

The latest projection targets 24 readable rows and 48 KiB of encoded rows; Earlier pages retain the 80-row/144-KiB budget. One already capped row may exceed the smaller target instead of being split without a continuation. Omitted older rows remain reachable through Earlier; Latest resumes the live tail. This bounds the client projection, not necessarily the existing server's bounded fetch, and establishes no resource-savings claim.

Earlier messages means older rows are available. Message shortened means an individual row hit its existing text/safety limit; Display limit means the local rendered page reached its cap. Use full T3 for content beyond those limits. Agent headings describe assistant messages; Subagent and fork labels require reported lineage.

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
