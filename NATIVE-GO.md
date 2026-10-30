# Go terminal client

The primary t3oxide client uses Bubble Tea v2 directly, with Bubbles controls, Lip Gloss styling and Glamour Markdown. It connects through the existing T3oxide Node adapter. The retained Swift/AppKit client is a legacy rollback with separate state.

See the [README](README.md) for native captures of the browser, AgentBox, Commands and theme picker.

## Build and open

Build prerequisite: **Go 1.26.10+**. The 0.8 integrated source uses the same pinned toolchain and component contracts; current checks and earlier native evidence are separated in [verification](docs/verification.md). The first build downloads pinned Go modules. The existing host adapter requires Node 22.4+; remote profiles use your normal OpenSSH configuration. See [native remainder](NATIVE-REMAINDER.md) for the focused installed-machine acceptance.

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

With no flags it preserves existing identity, token-file path and write setting, probes the existing loopback server, installs the reviewed adapter modules, including native.mjs as a versioned release, atomically publishes validated `host.json`, and repoints the adapter symlink. Configuration formatting may change; existing `operations-v1` receipts are retained. An interrupted installer leaves `.install-lock` beside `host.json`; preserve it until that installer has finished or its interrupted publication has been reviewed. It does not install, update, start or restart T3. Use the installer output to verify the exact adapter path in existing profiles; retain the existing identity and credential. Helpers load new modules on restart or explicit `/reconnect`; an internal socket reconnect keeps the loaded revision. An older adapter retains its original functions; new controls appear only when supported by its advertised capabilities.

After native acceptance, install the single executable in a directory on your zsh PATH, for example:

```zsh
mkdir -p "$HOME/.local/bin"
install -m 755 dist/t3oxide "$HOME/.local/bin/t3oxide"
```

Keep the legacy Swift app available for rollback. Use one client at a time during adoption.

When distributing the executable, include `NOTICE` and `LICENSES/` alongside it.

## Browse and create

Without `--thread`, Developer starts with all configured environments in a flat recent-activity list and an empty conversation. No transcript is attached until Enter or a click. Type to filter loaded metadata; More → Search conversations or `/threads WORDS` requests broader native title/project search. Each source keeps its own continuation and observation identity. A failed host is shown while healthy rows remain usable. At 80 columns and above the list sits beside the conversation; at 60 columns it occupies the window while focused. Ctrl+O reaches it, and Tab/Shift+Tab cycles visible panes.

Thread rows show bold symbol-and-word status badges: Active, Waiting, Failed, Blocked, Queued, Done, Stopped, Idle or Unknown. Their words remain meaningful without color. Failures stay distinct from pending requests; a queued follow-up remains Queued after the previous run completes. Reviewed and snoozed inbox metadata never imply a completed run.

The default budget is 24 unsettled root conversations per environment. Children require explicit expansion, type choice or search. Search can include settled conversations with the existing 200-row safety limit. Explicit lifecycle/type filters and per-source continuation retain bounded loading. The upstream shell response is still environment-wide; the adapter bounds its client projection, not the native server fetch.

`/new [TITLE]` resolves a creation environment independently of the attached thread. Explicit `--env` or one profile's `defaultForNew: true` takes precedence; a sole environment is unambiguous. Otherwise choose the target because the native desktop's cross-environment default is client-local and unavailable through the verified server API. Choose an existing project, including one with no threads. Native settings supply its model/options/runtime and workspace policy. No account or model is silently substituted when an existing configured default is invalid.

`/new-same [TITLE]` carries only the exact project ID. It does not carry source environment, provider/account, model, options, effort, runtime mode or worktree. If that project is absent on the creation environment, choose an offered existing project. Project creation remains in T3. The reviewed model, environment, project and workspace controls allow deliberate changes before Create.

In the launch review, click First message or Title to focus its editor. Enter inserts a newline in the first message. Enter in Title focuses Create; another explicit activation dispatches once. Tab traverses the editors and offered actions. While an action owns focus, Left/Up moves to the previous action and Right/Down to the next, stopping at either end; editor arrows retain text movement. Click an offered model, effort, tier or workspace action to open its existing chooser. Create and Cancel are directly clickable. An empty message files a native empty thread without inventing a run. A nonempty message starts native work. Title/message bytes remain exact; cancellation and local exit preserve the launch draft. Restoring a form with changed native defaults preserves its edits and requires an explicit Defaults review before Create. A successful launch can be revisited without losing the previous thread's exact draft, caret/selection and reading anchor.

AgentBox adds `/file`, `/file-many`, `/inbox`, `/review` and `/mode agentbox`. Isolated launches use T3's native worktree strategy. Every launch is separately reviewed; no client execution queue exists. See [AgentBox](docs/agentbox.md), [native workflows](docs/native-workflows.md), and [state/recovery](docs/state-recovery.md).

Conversation Settings stages the current thread's advertised model/options and sends one validated Apply. Click a field to open its local chooser, then Apply changes to submit the reviewed selection. A request option click stages its exact offered value, including an approval decision; use Submit decision or Submit answers to answer that request. Multi-question forms retain choices and custom text through Back/Next. Click the answer field to edit it, and use the visible Cancel control or Esc to leave without answering. Ordinary replies and Reviewed never grant permissions.

## Controls

The [complete resolved command/key reference](docs/shortcuts.md) is the maintained table for both modes and the CLI. `/help` and Commands use the same action registry as dispatch. `/info` replaces the display name Details; `/details` remains an alias. An optional manual `shortcuts.json` file is read on startup, with validation and no watcher or configuration UI.

**Enter is newline; Ctrl+] sends.** Distinct Ctrl+Enter and portable Ctrl+\ send literal text, including a known slash command. Commands → Steer and distinct Ctrl+Alt+Enter steer one message without silently changing the saved delivery selection. Ctrl+S remains independent Stop, including over requests, launch reviews, help and errors. Ctrl+Backspace retains deletion. Ctrl+I is Tab, so Info uses Ctrl+L. Delivered Command aliases appear on macOS with a delivery caveat; portable controls and palette routes remain available.

Version 0.8.2 includes a Control fallback for every Command action in its active context. Composer shortcuts include Ctrl+Alt+N New, Ctrl+Alt+T New in same project, Ctrl+Alt+G Settings, Ctrl+Alt+M Model, Ctrl+Alt+D Effort, Ctrl+Alt+R Refresh and Ctrl+Alt+A Activity. Ctrl+Alt+H opens Help; ordinary editor movement and Select All remain unchanged. See the [complete fallback table](docs/shortcuts.md) and `/shortcuts` for focus and visible-thread bindings.

Ctrl+Space is the default Commands shortcut, including over a populated composer; modern Ctrl+P remains a fallback. Cmd+P is not an app alias because Terminal uses it for Print. `/keyboard legacy` retains previous-line Ctrl+P editing and keeps Ctrl+Space for Commands. The composer retains Bubbles' line/word movement, selection, deletion, transpose and Select All. Literal punctuation and AgentBox letters/numbers stay with the focused editor. Application remaps cannot steal reserved editing keys.

Slash completion remains local: Tab completes; explicit Send invokes a known complete command. Unknown slash-prefixed text sends as ordinary text. Input is bounded to 64 KiB and 9,999 editable lines. Text the component would normalize is preserved as an exact buffer or rejected as a whole with its attempted bytes recoverable in `/recall`. `/edit` uses the existing external-editor path; its output is never trimmed or normalized. Ctrl+L reviews an exact buffer and `/copy draft` copies the original bytes. Recall remains source-bound and bounded to 24 entries / 512 KiB.

Mouse capture supports rendered navigation, form fields/actions, AgentBox decisions and exact tool inspection. Wheel movement over long forms reveals their focused rows without activating them; a focused multiline editor keeps its own scrolling. Only visible cells are clickable. Resize, a replaced prompt or source, and mode changes invalidate the previous controls until their current view is drawn. `/mouse off` restores native terminal selection. Clipboard support depends on the terminal; the existing desktop helper and OSC 52 path are unchanged. Local dismissal and exit never mean remote Stop.

Read-only CLI commands are `envs`, `threads`, `status`, `doctor`, `capabilities`, `activity` and `inspect`. Existing JSON envelopes and option validation are preserved. Explicit `--help` works without configuration; bare `--json` cannot start a TUI. `/web` displays the configured T3 address for full native inspection, attachments, artifacts and unsupported administrative/Git actions.

## Themes

Each theme supplies colours for three fixed highlight roles. Changing themes changes their colours while keeping those roles on the same UI elements.

| Theme | Context and selection | Supporting transcript, links and inline code | Operational status and streaming |
|---|---|---|---|
| Violet | Violet | Cyan | Lime |
| Cyan | Cyan | Lime | Violet |
| Lime | Lime | Violet | Cyan |
| Terminal | ANSI blue (4) | ANSI cyan (6) | ANSI magenta (5) |

Warnings and errors retain separate meanings. Terminal selections use the default colour pair in reverse. Textual location and state remain visible without colour; syntax tokens can use additional syntax colours.

`/theme` or Settings → Appearance changes the active browser profile or attached environment. Choose Environment default to clear that profile's override, or use `/theme default`. Defaults derive context accents for up to eight configured profile IDs, independent of browse order, and keep all three highlight slots distinct. The existing saved global theme remains the base; its initial value is violet. Profile count and theme choice do not change the three roles.

Use `/theme terminal` for the current profile to inherit your terminal's default foreground/background and ANSI palette, including syntax highlighting. Selections use inverse colors for light and dark backgrounds. Fixed violet, cyan and lime remain available with Dracula syntax highlighting. A saved global Terminal theme remains Terminal across profiles rather than adding fixed environment colors.

Terminal also resets full help, nested list help, launch-title and inbox/search editor styles. Switching back restores the fixed theme's defaults without changing text, caret or selection.

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

Known pending requests remain visible and reviewable while reading Earlier. Answers are checked against the current request identity and revision before dispatch. The cached count stays until Latest supplies a verified replacement; an answer acknowledgment alone does not establish that every request is resolved. Run and receipt details remain at their last observed state while live detail is paused.

The adapter supplies bounded messages, notices, request controls and attachment counts. Model/effort/tier controls now use the selected provider's advertised choices. Tool results are available through bounded, explicit inspection. Payloads beyond that limit, attachment uploads, provider switching/configuration and full-history search stay in T3. Current-page search and host-side thread filtering are separate controls.

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

## Complete read-only listings

`threads --json` retains its existing array format. Use `threads --json-envelope` for schema version 1 with `threads`, `more`, `observedAt`, lifecycle `scope`, `cursor` and `nextCursor`. `--scope active|all|completed|failed`, `--limit 1..200` and `--cursor N` control bounded requests. Pass the returned continuation cursor with the same query/scope. Older adapters may omit observation and continuation metadata; update the host adapter for the full contract. These commands remain read-only.

The 0.6 client adds complete launch preparation and browse preferences to Go state. Preserve the state and recall sidecar together; older executables may drop fields they do not understand. Retaining an old release is not a migration of these new preferences.


## Sidebar and activity

Ctrl+O focuses the unified recent list. The optional activity pane is separate: `/sidebar` or its resolved shortcut. `/navigator off` hides persistent navigation after attachment; focusing conversations makes it reachable again. Startup always offers metadata navigation.

The list uses compact lifecycle cues: `>` running, `?` waiting/input, `+` completed, `-` idle, `!` failed, `#` blocked, `x` interrupted/cancelled, `~` queued and `.` unreported. Info retains the complete native status, observation, IDs, model and project. Filled project accents remain stable through refresh/reorder. Colour and text cues are independent of the row's selection and attachment marker.

More binds the exact selected source for Open, same-project New and thread Settings. Ordinary New resolves the configured creation default independently. A refresh retires reviewed source actions; another unavailable environment does not block a healthy row. Search/filters/pagination keep exact source metadata and no default child expansion.

The recent activity strip shows its first meaningful command/tool component, with separation from transcript and composer. Enter/click opens the exact offered item; full input/result and omitted/truncated state remain in the bounded inspector. Tab from a tool detail returns locally to visible Activity. No command string is evaluated to shorten its label. Context avoids duplicating title/model/source/status already available through the frame and Info.
