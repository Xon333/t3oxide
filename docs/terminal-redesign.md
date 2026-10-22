# Terminal redesign

Decision recorded 2026-10-22 for the Go terminal UI. The redesign uses the released Charm libraries already pinned in [dependencies.json](dependencies.json) and `go.mod`. It changes presentation and interaction while preserving T3's conversation authority, exact request handling, drafts and operation recovery described in [go-terminal.md](go-terminal.md).

## Decisions

- Keep the conversation central: a centered workspace, distinct message roles and spacing, a bounded reading column, and a composer that grows with its content. Supporting context is optional on wide terminals.
- Make everyday actions discoverable through a palette that accepts typing immediately. Arrow keys move through fuzzy matches; one Enter activates the selected result. Model, effort and speed remain available as advanced actions.
- Use compact overlays with responsive choice descriptions and an explicit full-details view. Narrow layouts may shorten a visible label; F2 exposes the complete question and selected option before answering.
- Reuse native library facilities: Lip Gloss layers/composition, Bubbles lists/help/textareas/viewports, Bubble Tea terminal colors, and Glamour Markdown/syntax rendering. Application code binds these components to T3; it does not replace their editors, filtering algorithm or Markdown parser.
- Keep Stop independent of ordinary commands and dialogs. Local dismissal, reconnect and quit preserve their existing meanings; T3 remains responsible for remote execution.

## Keyboard and workflow

Start with Ctrl+E to choose an environment and Ctrl+T to open a conversation. Drafts are kept separately by environment/thread. The main workflow is:

| Task | Controls and behavior |
|---|---|
| Find an action | Ctrl+P, type a fuzzy query directly, use Up/Down, then press Enter once. No leading `/` or extra filter-confirmation Enter is required. |
| Write | Enter sends. Ctrl+J inserts a newline; Alt+Enter is an alias, and Shift+Enter also works when the terminal sends it distinctly. The composer grows within its visible height while retaining the complete draft. |
| Read | Tab switches between composer and transcript. PgUp/PgDown scroll; Ctrl+Up/Down move one line. With transcript focus, Home or `g` goes to the top; End or `G` returns to the bottom and resumes following. |
| Search this page | Ctrl+F opens search in the displayed transcript. Enter finds; repeating the same query advances. With transcript focus, `n`/`N` moves to the next/previous match. This does not search full history. |
| Browse history | Alt+Left loads an earlier page and pauses live detail. Alt+Right returns to Latest. Scrolling within a page and loading a different page are separate operations. |
| Review requests | Ctrl+R opens T3 approvals/questions. F2 shows the full prompt and selected-option details; F2 or Escape returns. Enter answers a single-choice or text request. For multiple choice, Enter toggles the current answer; Space also toggles outside filter editing. Ctrl+Enter or the Continue row submits the selected answers. |
| Stop remote work | Ctrl+S requests Stop + hold queue for the reviewed active turn. It remains available inside dialogs/details and while an ordinary action is pending. An acknowledgment reports acceptance; an uncertain result uses the existing Stop recovery lane. |
| Leave locally | Ctrl+D quits the terminal client. Remote T3 work continues. Escape dismisses a dialog locally without answering T3. |
| Get help | F1 opens keyboard help. Ctrl+P opens help directly over an active request; Escape returns to that same request. From the main workspace, Ctrl+P, type `help`, Enter opens help; `/help` and `/keys` also work. |
| Inspect complete text | F2 in a dialog opens the complete question and selected option. For a preserved, uneditable draft, F2 reviews it and Ctrl+Y copies it. |

The conversation chooser retains grouped navigation: Space/Right expands children, Left collapses, and F6 changes the thread-type filter. Every printable letter, including `j` and `k`, starts filtering loaded choices; `/` also starts filtering. Arrow keys navigate before and during a query. Ctrl+F outside filter editing runs host thread search, including hidden children. Alt+Up opens the authoritative parent conversation.

Ctrl+C is context-sensitive: it closes details/dialog/search, clears a draft, or quits when empty. It does not request remote Stop. Ctrl+Y normally copies the last assistant message; `/copy page` and `/copy draft` select other copy scopes. Use `/mouse off` for native terminal selection.

Advanced selection uses Alt+M for model, Alt+E for advertised effort and Alt+S for advertised speed/service tier. These default to the current thread. Next-message overrides, presets and evidence about the actual run are covered in [model-navigation.md](model-navigation.md). `/theme` offers terminal, violet, cyan and lime; Ctrl+B toggles the optional wide-terminal sidebar. Terminal inherits the terminal's default foreground/background and ANSI palette, including for code/diff highlighting; fixed themes retain Dracula syntax highlighting.

## Released upstream reuse

All four modules retain their existing MIT licenses and pinned versions. The implementation calls their APIs and configures their provided styles/components; it adds no dependency or interface fork. Existing attribution remains in [NOTICE](../NOTICE) and [LICENSES](../LICENSES/).

| Pinned release | Source used and local application |
|---|---|
| [Lip Gloss v2.0.6](https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.6) | Commit `733ce53541bb688fd735524b665e0d96e35433bb`: [layer.go](https://github.com/charmbracelet/lipgloss/blob/733ce53541bb688fd735524b665e0d96e35433bb/layer.go) provides `NewCompositor`/`NewLayer` for positioned overlays and frame labels. [wrap.go](https://github.com/charmbracelet/lipgloss/blob/733ce53541bb688fd735524b665e0d96e35433bb/wrap.go) provides style-aware wrapping support for transcript rails. |
| [Bubbles v2.2.1](https://github.com/charmbracelet/bubbles/releases/tag/v2.2.1) | Commit `490948109eb4731927ba2f4cd464d8175a9630d7`: [help/help.go](https://github.com/charmbracelet/bubbles/blob/490948109eb4731927ba2f4cd464d8175a9630d7/help/help.go) packs contextual key hints; [list/list.go](https://github.com/charmbracelet/bubbles/blob/490948109eb4731927ba2f4cd464d8175a9630d7/list/list.go) supplies native fuzzy filtering and synchronous `SetFilterText`; [textarea/textarea.go](https://github.com/charmbracelet/bubbles/blob/490948109eb4731927ba2f4cd464d8175a9630d7/textarea/textarea.go) supplies dynamic composer height; [viewport/viewport.go](https://github.com/charmbracelet/bubbles/blob/490948109eb4731927ba2f4cd464d8175a9630d7/viewport/viewport.go) owns scrolling and full details. |
| [Bubble Tea v2.1.0](https://github.com/charmbracelet/bubbletea/releases/tag/v2.1.0) | Commit `f9df43c4f2c021d8740a2628d59cd810a862eb98`: [tea.go](https://github.com/charmbracelet/bubbletea/blob/f9df43c4f2c021d8740a2628d59cd810a862eb98/tea.go), [cursed_renderer.go](https://github.com/charmbracelet/bubbletea/blob/f9df43c4f2c021d8740a2628d59cd810a862eb98/cursed_renderer.go) and [examples/chat/main.go](https://github.com/charmbracelet/bubbletea/blob/f9df43c4f2c021d8740a2628d59cd810a862eb98/examples/chat/main.go) establish View-level foreground/background colors and renderer-owned set/reset. The UI sets these colors through `tea.View`. |
| [Glamour v2.0.1](https://github.com/charmbracelet/glamour/releases/tag/v2.0.1) | Commit `95c93db04489cebf252ec856584445c7e85ee276`: [styles/styles.go](https://github.com/charmbracelet/glamour/blob/95c93db04489cebf252ec856584445c7e85ee276/styles/styles.go) supplies the dark style configuration; [ansi/codeblock.go](https://github.com/charmbracelet/glamour/blob/95c93db04489cebf252ec856584445c7e85ee276/ansi/codeblock.go) defines syntax-theme selection. Clearing `CodeBlock.Chroma` allows the chosen `Theme` (`dracula`) to apply. Finite `WithWordWrap` bounds native table layout before final ANSI-aware wrapping. |

The palette uses Bubbles' synchronous filter API so Enter selects the current query's result. Asynchronous component messages still carry session/dialog identity; stale filter results cannot replace the active query. Transcript rendering retains its bounded page cache and stale width/theme/session rejection. These are T3 integration responsibilities around the upstream components.

## Reference applications and provenance

Research checked GitHub API metadata and released source on 2026-10-22. Star counts below are that dated snapshot. These applications informed behavior; no application source was copied. They were source-checked, not installed or evaluated at runtime for this work.

| Reference release | License and inspected source | Pattern informing the design |
|---|---|---|
| [Crush v0.98.1](https://github.com/charmbracelet/crush/releases/tag/v0.98.1), 28,751 stars; commit `8b1824282eccc78cc62c188c245cf375230a18ce` | [FSL-1.1-MIT LICENSE.md](https://github.com/charmbracelet/crush/blob/8b1824282eccc78cc62c188c245cf375230a18ce/LICENSE.md); [internal/ui/dialog/common.go](https://github.com/charmbracelet/crush/blob/8b1824282eccc78cc62c188c245cf375230a18ce/internal/ui/dialog/common.go), [commands.go](https://github.com/charmbracelet/crush/blob/8b1824282eccc78cc62c188c245cf375230a18ce/internal/ui/dialog/commands.go) | Compact command/dialog workflow and immediate command discovery. Behavioral inspiration only. Its Tea v2.1.0, Bubbles v2.2.1 and Lip Gloss v2.0.6 versions match the existing stack. |
| [Lazygit v0.66.2](https://github.com/jesseduffield/lazygit/releases/tag/v0.66.2), 83,362 stars; commit `63ab89fff478854deb4a35ef8902676b3cc5ca72` | [MIT LICENSE](https://github.com/jesseduffield/lazygit/blob/63ab89fff478854deb4a35ef8902676b3cc5ca72/LICENSE); [pkg/gui/controllers/options_menu_action.go](https://github.com/jesseduffield/lazygit/blob/63ab89fff478854deb4a35ef8902676b3cc5ca72/pkg/gui/controllers/options_menu_action.go), [helpers/search_helper.go](https://github.com/jesseduffield/lazygit/blob/63ab89fff478854deb4a35ef8902676b3cc5ca72/pkg/gui/controllers/helpers/search_helper.go) | Discoverable action menus and a clear distinction between filtering choices and searching content. |
| [fzf v0.74.5](https://github.com/junegunn/fzf/releases/tag/v0.74.5), 83,723 stars; commit `c0a193e031f01fb50bf6f7be4381990a3753f7cf` | [MIT src/LICENSE](https://github.com/junegunn/fzf/blob/c0a193e031f01fb50bf6f7be4381990a3753f7cf/src/LICENSE); [src/terminal.go](https://github.com/junegunn/fzf/blob/c0a193e031f01fb50bf6f7be4381990a3753f7cf/src/terminal.go) | Tracked selection during filtering and responsive preview/detail presentation. |

## Verification boundary

Focused implementation checks and their limits are recorded in [verification.md](verification.md). Source checks establish the behavior of reused APIs; they do not substitute for installed-client visual acceptance. Native deployment evidence is recorded separately after adoption.
