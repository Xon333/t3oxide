# Input and focus

| Action | Input |
| --- | --- |
| Conversations / questions and reports | A / B; F2 |
| Navigation / optional graph | F3; compact picker closes after Open or Esc |
| Context | F4; compact screens use a detail overlay |
| Next / previous pane | F6 / Shift+F6 |
| Next / previous control | Tab / Shift+Tab |
| Select / open list item | Up/Down; Enter or click |
| Commands | Ctrl+P; Commands |
| Switch conversation | Search; Ctrl+F; tab +; /switch; /threads |
| Browse environments | /env |
| Search server conversations | /search; Commands → Search server conversations |
| Change search scope | Scope in Search; /search-scope |
| Prompt newline | Enter |
| Send in Rex | Ctrl+Enter |
| Portable Send | Ctrl+] or Ctrl+\; Send |
| Other Send keys, when forwarded | Cmd+Enter, Shift+Enter, Cmd+Shift+Enter |
| Stop and hold queue | Ctrl+S; Stop |
| Native requests | Ctrl+Y; Questions |
| Model / effort | Model; Effort; /model; /effort |
| Copy thread ID | Cmd+I / Ctrl+I when forwarded; Copy ID |
| Copy whole prompt draft | Ctrl+Alt+C; /copy-draft; Commands → Copy draft |
| Select / copy editor text | Cmd+A/C or Ctrl+A/C when forwarded |
| Settle / restore | Ctrl+T; /settle; /unsettle |
| Close tab, retain draft | Cmd+W / Ctrl+W when forwarded; tab close |
| Context records / capture / resume | /context; /context-save; /resume |
| Finite check / retained result | /check; /check-result |
| Help | F1; ?; /help |
| Save and exit | Ctrl+Q; /quit |

Type `/` at the start of a prompt for commands. Up/Down selects; Enter chooses.
Esc returns to the unsent draft; `//` keeps a literal slash. Pasted commands stay text.

Lists take one Tab stop. Arrows choose a row; a checked value remains separate
from the keyboard cursor. Forms save drafts; Next validates; Submit sends.
Changed-source drafts remain available in Saved answers.

Rex can intercept Cmd+Enter to create a pane; use Ctrl+Enter or portable Send.
Ctrl+I is Tab in legacy terminals; use Copy ID there. Editor copy requires selected text.
Copy draft copies the whole active prompt without selecting text or entering a manual mode.
For configured Rex clients, its command palette offers Enter T3oxide Editing for
Mac editor shortcuts; Escape leaves that manual mode before changing panes.

## Recovery

Recovery retains uncertain operations with their original identity and payload.
Reconcile explicitly; success appears only after acknowledgement. Exiting or
disconnecting does not stop native work. Sent and rejected text stays in Prompt
recall; subsequent edits belong to a separate draft.

## Finite checks

`/check` opens one source-pinned command form. Use, for example,
`--source src/parser.rs -- cargo test parser`. The current native workspace is
the default execution directory; `--cwd /absolute/worktree` overrides it on that
profile's execution host. `--timeout 300` sets a 1–900 second limit. Arguments
are parsed without shell expansion; pipelines require an explicitly chosen
shell command.

In Rex this creates a pane below the invoking pane in the same workspace. It
uses inherited session/block context and the maintained CLI. `T3OXIDE_REX` can
select an absolute CLI path; PATH and the standard macOS Rex installation are
also checked. An unqualified remote Rex server or missing pane context refuses
before launching. Remote native profiles use their existing SSH route and
keep execution-host paths separate from the display Mac.

The original attempt retains bounded output and selected source bytes. The
result and a saved finding stay together; review the staged follow-up, then
choose Send follow-up. The ordinary composer, caret and one-message model
override remain independent. Sending requires the original conversation to be
attached with a verified writable connection; switching away retains the staged
follow-up. Acceptance by T3 is distinct from completed work.
`/check-result` reads the original attempt; it never reruns it. Unknown results,
changed source and excerpts stay labelled. Finished task panes close only after
evidence is retained and their process exit is observed; uncertain panes remain.

The conversation switcher fuzzy-matches loaded titles, projects, environments and exact IDs.
Enter opens the selected source in an internal tab; Escape leaves the current draft intact.
Selected source details retain exact environment, profile, project and thread IDs.
Loading, unavailable and incomplete inventories are labelled; this is not global search.
Use /search separately for native server search.
