# Keyboard, mouse and commands

**Ctrl+P** or **Ctrl+Space** opens the fuzzy command palette. Its entries and shortcut labels come from the same action catalog as the dispatcher. Type fragments such as `rcvr` to find recovery; press Enter to run the selected action. Typing or pasting alone never dispatches an action.

The active pane has a highlighted border. Keyboard behavior follows that focus; mouse hit targets carry the source identity that was actually drawn.

## Global controls

| Key | Behavior |
| --- | --- |
| Ctrl+P / Ctrl+Space | Search commands. |
| F2 | Switch Developer / AgentBox in the same session. |
| F3 | Cycle Split, Focus and Wide layouts. |
| Tab / Shift+Tab | Next / previous pane: navigator, transcript, composer, Details. |
| Ctrl+O / Ctrl+L | Focus navigator / Details and dismiss the current dialog after preserving edits. |
| Ctrl+R | Refresh native conversation metadata. |
| Ctrl+F | Search loaded messages when the transcript has focus; otherwise open explicit native conversation search. |
| Ctrl+Y | Review exact native pending requests. |
| Ctrl+] | Send the active composer, or submit the open text form / selected dialog action. |
| Ctrl+\ | Same explicit submit path; composer text is always literal. |
| Ctrl+S | Stop the reviewed active run and hold its native queue, including while a dialog is open. |
| Ctrl+E | Open the active composer in `VISUAL` or `EDITOR`; returning only updates the draft. |
| Alt+Up / Alt+Left | Open the authoritative parent / recent conversation picker. |
| Alt+R | Recall a human prompt as editable draft text. |
| Ctrl+Q | Save drafts, open launch edits and UI state, then detach. Save failure keeps the client open. |

Legacy terminals send Ctrl+] and Ctrl+\ as control bytes that Crossterm reports as Ctrl+5 and Ctrl+4. Both representations use the same explicit submit action. This makes the default work without an enhanced keyboard protocol. The client does not depend on operating-system Command-key forwarding.

Stop always uses the attached conversation and reviewed run identity. Moving the highlight or opening a read-only preview does not retarget it. Stop does not require closing a dialog first, and an unrelated form save failure does not suppress the Stop attempt.

## Navigator

| Key | Behavior |
| --- | --- |
| ↑↓ or j/k | Move the highlight. |
| PageUp / PageDown | Move one visible page. |
| Home / End | First / last loaded conversation. |
| Enter | Open the exact highlighted environment/thread. |
| Right / Left | Expand native children / collapse or move to visible parent. |
| / | Start a local title/project/ID filter, including loaded collapsed children. |
| Enter while filtering | Keep the filter and finish editing it. |
| Esc while filtering | Clear the filter. |
| Space | Preview highlighted conversation without moving the composer or Stop target. |
| i | Focus the composer. |
| n | New conversation, with explicit project/defaults review. |
| v / e | Choose a view / environment scope. |
| r | Refresh native metadata. |
| m / a / d | Model options / native activity / changes and pull requests for the attached conversation. |
| x | Review the native mark-reviewed action. |
| ? / : | Help / command palette. |

A local filter only examines loaded metadata. **Search server conversations** queries native recent-message search explicitly and reports bounded results. **More conversations** requests the next native page. No bounded page is represented as complete history.

The attached conversation is the action target. Highlighting another navigator row is preparation to open or preview it; it does not silently change the target of model edits, requests, sends or Stop.

## Reading

| Key | Behavior |
| --- | --- |
| ↑↓ or j/k | Scroll wrapped lines; pause following. |
| PageUp / PageDown | Scroll one transcript viewport. |
| Home | Jump to the start of the loaded page. |
| End | Follow the latest visible content again. |
| [ | Load an earlier native page and pause live detail. |
| ] | Return to the latest live subscription. |
| Ctrl+F | Find text within loaded messages. |
| i or Enter | Focus the composer. |
| Esc | Return focus to the navigator. |

Use the palette for message outline, source copying and loaded-message actions. Source tools retain native thread/item IDs and observed revisions. Right-clicking a visible message opens its native source inspector. Full T3 remains the route to complete history, attachments and embedded capabilities outside the companion's supported surface.

## Workspace actions in the palette

| Action / command | Scope |
| --- | --- |
| `find`, `find-next`, `find-previous` | Search and revisit matches in the loaded transcript. |
| `outline` / `loaded-outline`, `loaded-jump N` | Select a source row or jump to a loaded row number. |
| `copy` | Choose an exact answer, draft, loaded page, detail, code block or inspected input/output/error to copy. |
| `project` / `browse-scope` | Scope the browser to a native project while keeping the attached conversation. |
| `priority`, `project-order`, `priority-filter`, `priority-undo` | Local inbox ordering and reversible priority preferences. |
| `presets` | Review a saved native model selection for Current or Next, or remove that favorite. |
| `saved-launches` | Recover supported saved creation forms into a fresh native-defaults review. |
| `batch-receipts` | Inspect acknowledged thread identities and review the next unfiled task. |
| `clear` | Review clearing the active composer. |
| `launch-defaults-reset` | Review resetting staged launch choices to fresh target-native defaults. |

Use **Enter command** in the palette when an action needs arguments, such as `/loaded-jump 12` or `/copy code`. These are client presentation and explicit native workflow actions, not a task scheduler. Copying uses an explicit OSC52 clipboard action, capped at 192 KiB and subject to terminal support; rendering source text does not execute it or follow embedded terminal escape sequences.

## Composer and forms

Enter inserts a newline in the composer, initial-message form and multiple-task form. **Ctrl+] is the explicit submit key.** Short fields use Enter to save and return to their review; Alt+Enter permits an explicit newline in a text field. Saving a launch field does not create a conversation: **Create conversation** is a separate reviewed action.

The upstream Ratatui textarea provides Unicode-aware movement, selection, deletion and word operations. The client adds these portable conventions:

| Key | Behavior |
| --- | --- |
| Ctrl+A | Select all editor text. |
| Ctrl+Z / Ctrl+Shift+Z | Undo / redo. |
| Ctrl+C with a selection | Copy within the textarea's editing buffer. |
| Ctrl+V | Paste the textarea buffer. Terminal bracketed paste also works independently. |
| Esc or Ctrl+C without selection in composer | Flush the draft and focus the navigator. |
| Esc in a launch form | Preserve the edited field and close the form. |

Bracketed paste is literal. Slash commands pasted into the composer remain message text. Use the palette for actions. OS clipboard shortcuts are terminal-specific; **Toggle mouse** or `--no-mouse` enables native terminal selection.

Editors cap input at 64 KiB and 9,999 lines; specific native fields have tighter validation. A rejected oversized paste preserves the existing text. Launch titles/messages and task lists save after a short idle interval, then flush before leaving or quitting. Drafts remain source-qualified.

## Dialogs and complete review

Arrows select choices; Enter activates the displayed choice; Escape dismisses after preserving applicable edits. Choice clicks validate the current dialog against the one that was drawn, so a changed request cannot reuse a stale row target.

Long prompts, original operation payloads, diffs and source output are scrollable. **PageUp/PageDown** scroll the detail pane without changing the selected action. **Ctrl+Home/Ctrl+End** jump to its first/last line. The mouse wheel over the detail pane also scrolls it. In a detail-only view, arrows, Home and End work directly.

Approval dialogs offer only the native decisions. Questions retain the exact request revision and support single/multiple choices and custom text only where the native offer allows it. **Next** advances to a complete answer review; **Submit answers** is explicit. Escape leaves the native request pending.

## Mouse

Click mode, layout or lane tabs to change the client view. Click a conversation to open its exact source; click a pane to focus it. The navigator wheel moves the highlight; the transcript wheel scrolls and pauses follow. Click **Following / End follows** to resume follow.

Click or drag in a composer/form to move the cursor and select text. Mouse coordinates use the textarea's wrapped Unicode mapping. Click dialog choices to select and activate them; scroll long original payloads in their own pane. **Toggle mouse** switches between these interactions and your terminal's native text selection.

## Read-only CLI

| Command | Result |
| --- | --- |
| `t3oxide envs [--json]` | Enabled configured profiles, without connecting. |
| `t3oxide threads --env PROFILE [FILTER] [--json]` | Bounded native conversation metadata. |
| `t3oxide threads --env PROFILE --json-envelope` | Versioned page with source identity, continuation and completeness fields. |
| `t3oxide status --env PROFILE [--thread ID]` | Authenticated adapter status or a verified live exact-thread snapshot summary. |
| `t3oxide doctor [--env PROFILE] [--json]` | Independent identity and metadata checks, reporting unavailable sources. |
| `t3oxide capabilities [--env PROFILE]` | Client contracts; with a source, its actual handshake/capabilities. |
| `t3oxide activity --env PROFILE --thread ID [--activity-cursor KEY]` | Bounded native tool/activity page. |
| `t3oxide inspect --env PROFILE --thread ID --item ID [--revision KEY]` | Exact native source-item read. |

`--config PATH`, `--state PATH`, `--mode developer|agentbox`, `--theme terminal|violet|cyan|lime` and `--no-mouse` configure the interactive client. `--thread ID` requires `--env PROFILE`. Conversation pagination accepts `--scope active|all|completed|failed`, `--type all|main|subagents|forks`, `--cursor N` and `--limit N` (1–200). Use `--help` for the executable's current options.

No headless command sends, approves, launches, changes settings or retries an operation. Those require an explicit human TUI action and host capability checks.
