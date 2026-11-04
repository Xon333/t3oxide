# Input and focus

SLT owns Tab/Shift+Tab focus, mouse hit tests, text selection and terminal input. Widgets consume typing before ordinary global actions. Source and action identities survive prior-frame layout; a changed modal/list must settle before activating its old coordinates.

| Action | Input |
| --- | --- |
| Conversations / human questions | A / B buttons; F2 |
| Commands | Ctrl+P; command button |
| Project-first search | Search button; Ctrl+F |
| Focus next / previous | Tab / Shift+Tab |
| Open conversation | Navigator Enter or click; internal tab |
| Prompt newline | Enter |
| Preferred Send, if delivered | Cmd+Enter |
| Accepted Send alternatives | Ctrl+Enter, Shift+Enter, Cmd+Shift+Enter |
| Portable Send | Ctrl+] or Ctrl+\; explicit Send button |
| Native Stop and queue hold | Ctrl+S; Stop button |
| Native requests | Ctrl+Y; Questions button |
| Model and effort | Model · effort button; /model, /effort |
| Copy exact thread ID | Cmd+I / Ctrl+I where delivered; Copy ID button |
| Settle native thread | Ctrl+T; /settle |
| Restore settled thread | /unsettle |
| Close tab, retain draft | Cmd+W / Ctrl+W where delivered; tab close button |
| Context capture / recall / resume | Context controls; /context-save, /context, /resume |
| Help | F1; ? button; /help |
| Save and exit | Ctrl+Q; /quit |

The application accepts richer send key events when the terminal forwards them. Source tests prove exclusive routing after settled editor focus; actual Rex/M1pro/M4 delivery and collision-free pane behavior require separate native checks. No global Rex pane binding is changed. Ctrl+I is Tab in legacy terminals; use Copy ID there.

Typing `/` at the start of a prompt opens completion. Up/Down selects; Enter consumes the command without adding a newline or sending. Commands needing arguments open their native review controls. Esc returns to unsent text; `//` permits literal slash text. Pasted slash commands and pasted newlines remain text. Copy, cut, paste, word movement and undo use SLT's editor support when the terminal forwards their key events.

Forms retain drafts when dismissed, changed or cancelled at the source. Continue validates a field; Submit is a separate native mutation. Widget-local completion never indicates delivery. An acknowledgement must match the original source revision and operation identity. Changed-source drafts remain recoverable.

## Recovery

Disconnect and client exit never stop accepted native work or retry a mutation. Recovery keeps the original ID and exact payload. Stop uses an independent lane. Uncertain answers stay uncertain until an explicit reconciliation returns the matching receipt.
