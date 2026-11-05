# Input and focus

| Action | Input |
| --- | --- |
| Conversations / questions and reports | A / B; F2 |
| Sidebar | F3 |
| Context sidebar | F4 |
| Next / previous pane | F6 / Shift+F6 |
| Next / previous control | Tab / Shift+Tab |
| Select / open list item | Up/Down; Enter or click |
| Commands | Ctrl+P; Commands |
| Search current project | Ctrl+F; Search |
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
