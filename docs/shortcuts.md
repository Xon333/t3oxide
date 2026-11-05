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
| Rex Send | Ctrl+Enter |
| Other send events, when forwarded | Cmd+Enter, Shift+Enter, Cmd+Shift+Enter |
| Portable Send | Ctrl+] or Ctrl+\; explicit Send button |
| Native Stop and queue hold | Ctrl+S; Stop button |
| Native requests | Ctrl+Y; Questions button |
| Model and effort | Model · effort button; /model, /effort |
| Copy exact thread ID | Cmd+I / Ctrl+I where delivered; Copy ID button |
| Select / copy editor text, if forwarded | Cmd+A/C or Ctrl+A/C; focused editor selection |
| Settle native thread | Ctrl+T; /settle |
| Restore settled thread | /unsettle |
| Close tab, retain draft | Cmd+W / Ctrl+W where delivered; tab close button |
| Context capture / recall / resume | Context controls; /context-save, /context, /resume |
| Help | F1; ? button; /help |
| Save and exit | Ctrl+Q; /quit |

The application accepts richer send key events when the terminal forwards them. Physical Ctrl+Enter on M1 Rex dispatches once without creating a pane. Existing Rex Cmd+Enter creates a pane; its global binding is preserved. M4 finished-client verification remains a separate acceptance gate. Ctrl+I is Tab in legacy terminals; use Copy ID there.

For Mac editor selection/copy on configured Rex clients, open View → Command Palette… (Cmd+Shift+P) and choose **Enter T3oxide Editing**. Cmd+A must visibly select the focused editor text before Cmd+C. Escape exits this manual, client-wide mode; exit before switching panes or leaving the editor. Ordinary terminal selection/copy and pane shortcuts resume outside the mode. Native M1 acceptance verified palette focus return and exact multiline Unicode clipboard bytes; no automatic editor-focus integration is claimed.

Typing `/` at the start of a prompt opens completion. Up/Down selects; Enter consumes the command without adding a newline or sending. Commands needing arguments open their native review controls. Esc returns to unsent text; `//` permits literal slash text. Pasted slash commands and pasted newlines remain text. Copy, cut, paste, word movement and undo use SLT's editor support when the terminal forwards their key events.

The focused editor consumes copy only with a selection. An unselected or
unfocused Ctrl+C does not copy an editor draft or exit the client. Terminal
forwarding, OS clipboard receipt and actual focus remain separate native checks.

Forms retain drafts when dismissed, changed or cancelled at the source. Continue validates a field; Submit is a separate native mutation. Widget-local completion never indicates delivery. An acknowledgement must match the original source revision and operation identity. Changed-source drafts remain recoverable.

## Recovery

Disconnect and client exit never stop accepted native work or retry a mutation. Recovery keeps the original ID and exact payload. Stop uses an independent lane. Uncertain answers stay uncertain until an explicit reconciliation returns the matching receipt.

Sending starts a separate next draft after the original operation and attempted
text are saved. A delayed acknowledgement, rejection or uncertain result cannot
append to or overwrite that draft. Rejected text remains in Prompt recall;
uncertain operations remain in Recovery with their original payload.
