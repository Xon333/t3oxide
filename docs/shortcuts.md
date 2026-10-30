# Keyboard and commands

Enter inserts a newline in the ordinary composer. **Ctrl+] sends**, **Ctrl+\ sends literal text**, and **Ctrl+Alt+Enter steers the current conversation explicitly**. Normal Send uses the selected delivery preference, which starts as native `auto`; Steer is a one-message choice and leaves that preference unchanged. In a form, Enter activates its focused control; finishing a New title moves focus to Create without creating anything.

Ctrl+S retains the independent Stop + queue-hold lane, including while a normal request or setting is open. Ctrl+C dismisses locally and keeps the draft. Ctrl+Q exits the client; native work continues according to T3's actual execution state.

## Application defaults

Every Command action has a Control fallback in the same context. macOS help prefers the delivered Command alias and marks it with `*`; `/shortcuts` lists every resolved Control binding. Ctrl+Alt chords preserve ordinary Control editing keys. macOS and terminals can consume Command+H/M/N, modified arrows, digits and other shortcuts before the TUI receives them. Modified digits/arrows still require distinct terminal events; the listed ordinary navigation routes remain available. No terminal or OS keybindings are changed by t3oxide. **Commands is the portable alternative for every listed action.**

| Action | Shipped keys | Context / Command alias |
| --- | --- | --- |
| Commands | Ctrl+Space / Ctrl+@; F3; Ctrl+P fallback | Ctrl+Space is the displayed default. Cmd+P belongs to Terminal Print and is not an app alias. Lists keep Ctrl+P as previous row. |
| Switch conversation | Ctrl+O; F6 | Ordinary workspace; delivered Cmd+O. In the sidebar, F6 retains More. |
| More sidebar actions | Alt+A; F6 | Sidebar query/results; the visible More control uses the same actions. |
| Info | Ctrl+L; F2 | All surfaces; delivered Cmd+I. `/details` remains an alias for `/info`. |
| Help | Ctrl+Alt+H; `?` in reading/activity; F1 | `/help` or Commands in any editor; delivered Cmd+H. |
| Send / execute recognized command | Ctrl+] | Composer only; delivered Cmd+Enter. |
| Send literal text | Ctrl+\; Ctrl+Enter when distinct | Composer only; does not interpret slash commands. |
| Steer | Ctrl+Alt+Enter when distinct | Composer only; delivered Cmd+Shift+Enter, or Commands → Steer. |
| Request next / Submit | Ctrl+Enter when distinct | Only an actual request form; Enter on its focused Next/Submit button remains the portable route. This action never sends an ordinary reply or creates a thread. |
| New | Ctrl+N outside multiline editors; Ctrl+Alt+N in the composer | Delivered Cmd+N also works from the composer; Ctrl+N retains next-line editing there. |
| New in same project | Ctrl+Alt+T; Ctrl+Alt+N outside editors | Enhanced Ctrl+Shift+N or delivered Cmd+Shift+N; `/new-same`. |
| Settings | Ctrl+Alt+G; Ctrl+G outside multiline editors | Delivered Cmd+, from ordinary workspace; Ctrl+G remains Select All in multiline editors; the navigator query uses it for Settings. |
| Thread model | Ctrl+Alt+M; Alt+M outside editors | Delivered Cmd+M; `/model`. |
| Effort | Ctrl+Alt+D; Alt+E outside editors | Delivered Cmd+E; D is depth and avoids the navigator's environment shortcut. `/effort`. |
| Speed | Alt+S outside editors | `/speed`. |
| Conversation types | Ctrl+/ or Ctrl+_ outside editors | `/filter`; legacy terminals often encode these alike. |
| Requests | Ctrl+R | Review exact offered questions/approvals; never answered by ordinary Send. |
| Environments | Ctrl+E in reading panes; Ctrl+Alt+E in the navigator | `/env` and the visible environment chooser remain available. Ctrl+E stays line-end in every text editor. |
| Refresh metadata | Ctrl+Alt+R; Alt+R outside editors | Delivered Cmd+R; `/refresh`. AgentBox uses Ctrl+Alt+R for its inbox; Ctrl+R remains Requests. |
| Find loaded transcript | Ctrl+F or `/` while reading | Delivered Cmd+F; `/search`. Navigator/full-browser Ctrl+Alt+F searches host metadata; Ctrl+F stays cursor movement in editable filters and composers. |
| Context/activity pane | Ctrl+Alt+A; Ctrl+B in reading panes | Delivered Cmd+B; `/sidebar`. Ctrl+B remains cursor movement in every text editor, including the navigator filter. |
| Pane focus | Tab / Shift+Tab | Visible panes only; Tab completes an active slash suggestion. |
| Navigation / composer focus | Ctrl+Alt+Left / Ctrl+Alt+Right | Delivered Cmd+Left / Cmd+Right; Tab or Ctrl+O provides ordinary navigation. |
| Visible thread 1–9 | Ctrl+Alt+1…9 | Delivered Cmd+1…9; exact current visible sidebar rows. Arrows + Enter is the portable path when modified digits are unavailable. |
| Parent / earlier / latest | Alt+Up / Alt+Left / Alt+Right while reading | `/parent`, `/earlier`, `/latest`; editor word movement remains local. |
| Copy | Ctrl+Y | Context-specific copy; `/copy` variants expose exact source choices. |
| Clear draft | Ctrl+X | Recoverable via `/recall`; never remote Stop. |
| External editor / history | F4 / F5 | `/edit`, `/recall`, or Commands, so function keys are optional. |
| Mode | Ctrl+Alt+B | `/mode developer`, `/mode agentbox`; delivered Cmd+Shift+B. |
| Stop / exit | Ctrl+S / Ctrl+Q | Separate from ordinary mutation state; no implicit retry. |

`Enter`, Ctrl+J and Option/Alt+Enter are newline bindings in the ordinary composer. Shift+Enter can also insert newline when delivered distinctly. Ctrl+I stays Tab; Ctrl+H stays deletion/backspace. Ctrl+Backspace retains native word deletion rather than becoming Stop. On traditional terminals, Ctrl+Enter may arrive as plain Enter and therefore safely inserts a newline; use Ctrl+\ for literal send. If Ctrl+Alt+Enter is not distinct in your terminal, use Commands → Steer.

Ctrl+Space opens Commands throughout the client. Modern Ctrl+P remains a fallback in the composer and plain answer editors. `/keyboard legacy` gives those editors native previous-line Ctrl+P; Ctrl+Space still opens Commands. `/keyboard modern` restores the Ctrl+P fallback. Ctrl+A/E/B/F, movement, selection, word deletion and paste retain their focused editor ownership. Ctrl+N and Ctrl+G stay next-line and Select All in multiline editors. Editable navigator/browser queries keep Ctrl+B/E/F and Alt+Left/Right; their search uses Ctrl+Alt+F and navigator environment selection uses Ctrl+Alt+E. Earlier/Latest remains in Commands while a query owns those word-movement keys.

## Forms and mouse

The visible form controls work in Developer and AgentBox. Tab/Shift+Tab keeps the existing complete focus order. Mouse capture must be enabled; `/mouse off` disables both clicks and wheel handling.

| Surface | Navigation and activation |
| --- | --- |
| New review | Click First message or Title to edit. Editor arrows move within text. Enter in the message adds a newline; Enter in Title focuses Create. When an action owns focus, Left/Up selects the previous action and Right/Down the next, clamped at either end. Click an offered action or press Enter on it to activate. Create alone dispatches the reviewed launch. |
| Conversation Settings | Click a model/option field or press Enter to open its current chooser. Chooser navigation selects locally; accepting a choice stages it. Apply changes submits once; Cancel leaves the saved selection intact. Existing Left/Right cycling on a settings row remains local. |
| Formal questions and approvals | Click an offered value to stage/toggle it. Click the answer/custom field to edit; Enter there adds a newline. Back/Next preserves staged answers and enforces required fields. Approval warnings remain visible with their offered options. An approval uses Submit decision; questions use Submit answers. Ctrl+Enter, when delivered distinctly, invokes the focused form's next/submit action. Ordinary Send cannot answer a form. |
| Plain text prompts | Click the text field to edit, or use the visible Submit and Cancel controls. Tab reaches both controls. Existing Enter-to-answer and Ctrl+J newline behavior remain available while editing; Enter on Cancel cancels. |
| Long forms | Wheel over the form advances its focused visible rows without selecting an answer or activating an action. Wheel over a focused multiline editor stays with that editor. Hidden or clipped controls are inactive. |

A resize, a replacement prompt, a changed source/offer or a mode switch retires old click geometry. Nested choosers return to the exact owning form. A form blocks clicks from reaching the workspace behind it. Contextual help shows launch action arrows only while actions own focus.

## AgentBox context

These keys apply to the focused inbox/decision, never ordinary reply text or its search editor. Decisions use only actually offered choices. A numbered choice may open a formal request review; it does not approve it. `E` records a supported review disposition and never grants a permission.

Click a visible inbox row to open its exact source. The displayed Changes / Reply / Reviewed or Requests / Conversation / Later controls are also clickable and invoke the same current offered actions as their numbered keys. Summary text is inert. Reply focuses the existing composer; Reviewed and Later retain their existing review menus and native busy/request checks.

`/file-many` opens a local batch input with one task per line. Each task receives its own native launch review and explicit Create action, using isolated work by default. Pending local filing is retained for recovery; this does not add a background scheduler or promise future local dispatch after exit.

| Action | Keys | Meaning |
| --- | --- | --- |
| Previous / next | K/Up, J/Down | Select a real source-bound task row. |
| Open / reply / Developer | Enter, R, O | Open or reply to the same native conversation; O shows it in Developer. |
| File / changes | N, D | Review a native work launch; inspect actual reported changes. |
| Review / Later / undo | E, L, Z | Explicit supported review or visibility changes; undo only genuinely reversible metadata. |
| Summary / views / priority | S, V, P | Local presentation and supported task rank. |
| Offered choice | 1 / 2 / 3 | Only a focused, still-current decision. |
| Search / refresh | `/`, Ctrl+Alt+R | Bounded native metadata search/read. Ctrl+R opens Requests, including while editing search. |
| Focus / return | Tab, Shift+Tab, Esc | Local review navigation; Esc does not cancel native work. |

## Optional manual file

Create `shortcuts.json` beside `terminal.json` (normally `~/.config/t3oxide/shortcuts.json`). This file is optional and read once at startup. `/shortcuts` displays its actual location and resolved bindings. Removing the file restores defaults at the next startup. There is no shortcut settings UI, recorder or file watcher.

```json
{
  "version": 1,
  "bindings": {
    "global": {
      "info": ["alt+i", "cmd+i"]
    },
    "composer": {
      "send": ["ctrl+alt+]", "cmd+enter"]
    },
    "reading": {
      "settings": ["ctrl+alt+g", "cmd+,"]
    }
  }
}
```

Contexts are `global`, `composer`, `reading`, `navigator`, `activity`, `context`, `picker`, `form`, `search`, `detail` and `agentbox`. `global` applies to actions already owned globally; a context override replaces that action's keys only in that context. Missing actions keep their defaults. Keys accept `cmd`/`command` as aliases for `super`, `option`/`opt` for `alt`, and `control` for `ctrl`. Modifier order is normalized. A context cannot introduce an action whose dispatcher is unavailable there.

Stable IDs are listed by `/shortcuts` and defined once in `internal/ui/shortcuts.go`. They include `send`, `send-literal`, `steer`, `request-submit`, `new`, `new-same`, `settings`, `info`, `palette`, `mode`, `thread.1`…`thread.9`, and the `agentbox.*` actions. `request-submit` is available only in `form` and dispatches only while a real native request form owns focus. The same resolved Bubbles bindings drive dispatch, contextual help and palette hints. Bubbles still owns ordinary text editing and list navigation.

Native component controls remain local and immutable: arrows, text editing, Enter on a focused choice/button, Tab between form controls, and generic multi-choice Ctrl+Enter to Continue. The retained full browser's Search and thread-type actions use `browser.search` (Ctrl+Alt+F / delivered Cmd+F) and `browser.filter` (F6 while results own focus); visible More controls provide the portable route. These actions are enabled only for an actual browser/navigation prompt. They never become composer Send or Steer through the shortcut file.

Invalid versions, unknown fields/contexts/actions, malformed keys, alias collisions and conflicting bindings are startup errors with the file/action identified. Reserved editing keys cannot be stolen by global commands. Stop must retain Ctrl+S and local dismiss must retain Esc in every context; extra aliases are allowed. Configuration is bounded to 64 KiB and eight keys per action. No application, terminal or OS remapping runs in the background.

## Source and acceptance

The implementation reuses pinned [Bubbles key bindings and editor ownership](https://github.com/charmbracelet/bubbles/blob/490948109eb4731927ba2f4cd464d8175a9630d7/textarea/textarea.go), [Bubble Tea events and enhancement negotiation](https://github.com/charmbracelet/bubbletea/blob/f9df43c4f2c021d8740a2628d59cd810a862eb98/tea.go), and [Ultraviolet's Command/Super representation](https://github.com/charmbracelet/ultraviolet/blob/6b8d4baf91b4c933e6613b0eefc8f180f6eda002/key.go). The [Kitty keyboard protocol](https://sw.kovidgoyal.net/kitty/keyboard-protocol/) documents legacy ambiguities and enhanced events. Inspected 2026-10-29; no input dependency or upstream code copy was added.

Byte-decoder fixtures and real Bubble Tea program tests establish decoding and application routing. They do not establish that physical Command keys arrive on an installed macOS terminal. Native integration must check the delivered Command aliases, Ctrl+] / Ctrl+\ / Ctrl+S, Enter/newline, text editing, request Submit and focus in the actual supported terminal without changing its global settings.

## Command reference

The palette and slash completion use `internal/ui/command_catalog.go` plus the session's supported command catalog. A recognized **single-line** slash command executes only with explicit Send. Enter remains newline. Multiline text and unknown slash-prefixed text stay literal message content; literal Send bypasses command interpretation. In the tables below, square brackets denote optional arguments.

### Conversations, modes and work

| Command | Effect |
| --- | --- |
| `/new [title]` | Choose an existing project in the creation environment, resolve native defaults, and review before Create. |
| `/new-same [title]` | Preselect only the current project's exact ID; environment, model/options and workspace still use creation defaults. |
| `/switch [words]` | Open conversation navigation; optional words search its native metadata. Highlighting alone does not attach. |
| `/env` | Choose an environment explicitly. |
| `/threads [words]` | Search native conversation titles/projects, including unloaded rows. |
| `/filter [all\|main\|subagents\|forks]` | Choose visible relationship types; child conversations remain explicitly expanded. |
| `/refresh` | Refresh bounded conversation metadata without replaying mutations. |
| `/parent` | Open the exact native parent of the attached fork/subagent when reported. |
| `/mode [developer\|agentbox]` | Switch presentation over the same native conversation/session; no argument toggles the mode. |
| `/inbox [needs-you\|working\|queued\|later\|reviewed\|all]` | Open AgentBox or choose a native-state inbox view. Reviewed is the user-facing name for the native `done` disposition. Its Views control also exposes Unknown, filters and per-environment pages. |
| `/file` | Review one native work item, with isolated native workspace as the initial AgentBox choice. |
| `/file-many` | Enter an explicit list, then review and Create each native task separately. Cancel or uncertainty pauses the saved human list. |
| `/review` | Open exact formal requests when present; otherwise inspect actual native changes for the attached task. |
| `/retry-launch` | Reconcile a saved uncertain launch against its original native identity. It never substitutes a new launch ID or guesses successful creation. |

`defaultForNew`, explicit `--env` and the native-default boundary are documented in [Native workflows](native-workflows.md). Filing a task does not create a client-owned scheduler. Use AgentBox Priority to rank existing native identities for presentation; rank does not reorder native execution.

### Messages, settings and requests

| Command | Effect |
| --- | --- |
| `/send` | Palette/action form sends the current composer draft using the selected delivery preference. Use Ctrl+] for ordinary draft text. |
| `/send-literal` | Palette/action form sends the exact composer text, including a leading slash. Ctrl+\ is the portable direct binding. |
| `/steer` | Explicitly steer one current message without changing the saved delivery preference. |
| `/delivery auto\|queue\|steer` | Set the selected native delivery preference; `auto` is the default. |
| `/requests` | Review current native questions/approvals with their exact IDs, revisions and advertised choices. |
| `/stop` | Stop the reviewed active run with native queue hold through the independent Stop lane. |
| `/settings` | Review model/options and local appearance controls. Apply remains explicit. |
| `/model [next] [model]` | Choose an advertised model; `next` stages a next-message override instead of changing the saved thread selection. |
| `/effort [next] [value]` | Choose a reasoning option actually advertised for the selected model. |
| `/speed [next] [value]` | Choose an advertised service tier/fast option. |
| `/presets [next] [save\|remove\|apply] [name]` | Review or manage environment-scoped model favorites. |
| `/override [model\|effort\|speed\|presets\|clear]` | Inspect, stage or clear the next-message selection. Native T3 may adopt a dispatched selection. |
| `/runtime` | Inspect saved, requested and provider-reported selection separately. |
| `/edit` | Edit the current draft in the configured external editor. |
| `/recall` | Browse local prompt history; choosing a draft remains a local action. |
| `/clear` | Clear the draft locally while retaining recoverability through history. |

Send, literal Send and Steer act on an existing composer draft; choosing them from Commands preserves that draft. Typing their names into the composer does not manufacture an extra message body. Formal Submit belongs to the focused request form and is independent of these message actions.

### Reading, local presentation and recovery

| Command | Effect |
| --- | --- |
| `/earlier`, `/latest` | Read a bounded earlier page (pausing live detail), or resume the latest live page. |
| `/search [text]` | Find within the currently loaded transcript page. This is distinct from native conversation search. |
| `/activity [item-key]` | Inspect bounded source-owned tools/results; explicit activity paging remains separate from message history. |
| `/info` (`/details`) | Inspect exact source, native state, staged/saved settings and receipts. |
| `/context` | Open the attached conversation's context review. |
| `/sidebar [on\|off]` | Toggle the optional context/activity pane. |
| `/navigator [on\|off]` | Toggle conversation navigation separately. |
| `/theme [terminal\|violet\|cyan\|lime]` | Choose terminal inheritance or one of the existing palettes; no argument opens the chooser. `/theme default` resets inherited/default theme selection. |
| `/mouse [on\|off]` | Toggle application mouse handling; off permits native terminal selection. |
| `/copy [page\|draft\|answer\|error\|detail]` | Copy the selected exact source; no argument copies the last loaded agent message. |
| `/keyboard modern\|legacy` | Select Ctrl+P Commands or native previous-line editor ownership. |
| `/shortcuts` | Inspect the resolved action bindings and optional file path. |
| `/help` (`/keys`) | Contextual keyboard help and command discovery. |
| `/pending` | Inspect saved ordinary and Stop operation identities/payloads. |
| `/retry [stop]` | Explicit original-ID recovery for the selected saved lane. Uncertain launches still require authoritative reconciliation. |
| `/resolve [stop]` | Review and explicitly clear a local recovery record; this does not cancel or undo native work. |
| `/error` | Inspect the current detailed error. |
| `/reconnect` | Reconnect the owned helper without automatically replaying a write. |
| `/web` | Show the configured native T3 address for explicit external inspection. |
| `/quit` | Exit locally; existing native execution remains backend-owned. |

Availability depends on the current source, native advertised capabilities and actual state. The palette retains a precise unavailable reason where an action cannot run. For exact recovery behavior, see [State and recovery](state-recovery.md).

## CLI reference

The executable's noninteractive commands are read-only. They do not start native T3 services, dispatch messages or grant permissions. `t3oxide` with no command opens the TUI; `--env PROFILE --thread THREAD_ID` explicitly opens an exact configured conversation. A profile ID names an entry in the local environment configuration, not a guessed native environment ID.

| Invocation | Result |
| --- | --- |
| `t3oxide envs [--json]` | Configured environments; JSON preserves the existing array format. |
| `t3oxide threads --env PROFILE [WORDS] [--json]` | Bounded native conversation metadata matching optional title/project words. |
| `t3oxide threads --env PROFILE --json-envelope` | Versioned metadata envelope with completeness and continuation; inspect it before assuming all native rows were returned. |
| `t3oxide status --env PROFILE [--thread ID]` | Adapter or exact-thread status, always JSON. |
| `t3oxide doctor [--env PROFILE] [--json]` | Verify configured identity and authenticated read paths. |
| `t3oxide capabilities [--env PROFILE] [--json]` | Client contracts; with an environment, also perform a real adapter handshake. A handshake alone does not certify every optional native method. |
| `t3oxide activity --env PROFILE --thread ID [--activity-cursor KEY] [--json]` | Bounded exact-thread activity page; cursor is opaque native continuation. |
| `t3oxide inspect --env PROFILE --thread ID --item ID [--revision KEY] [--json]` | Read one exact source item. Revision is a cache key, not a historical snapshot guarantee. |
| `t3oxide --version`, `t3oxide --help` | Executable version or actual command help. |

General options are `--config PATH` (environment configuration), `--state PATH` (Go draft/recovery file), `--env PROFILE`, `--thread ID`, `--theme terminal|violet|cyan|lime`, and `--no-mouse`. Defaults are `~/.config/t3oxide/environments.json` and `~/.config/t3oxide/go-drafts.json`; the Go and Swift state owners stay separate. `--env` is also an explicit creation target for the interactive session. It does not silently retarget a saved operation.

Only `threads` accepts `--scope active|all|completed|failed`, `--type all|main|subagents|forks`, `--cursor N`, `--limit N` (1–200), and `--json-envelope`. `--item`/`--revision` belong to `inspect`; `--activity-cursor` belongs to `activity`. `--json` needs a read command. Unknown options, invalid values and unsupported combinations fail rather than being silently ignored.

`capabilities`, `activity` and `inspect` use their versioned JSON envelope, including structured read errors. Host credentials remain host-local, and metadata/artifact references are not authorization to execute commands or read arbitrary host paths.
