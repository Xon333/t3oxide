# Terminal redesign

Decision recorded 2026-10-22 for the Go terminal UI. The redesign uses the released Charm libraries already pinned in [dependencies.json](dependencies.json) and `go.mod`. It changes presentation and interaction while preserving T3's conversation authority, exact request handling, drafts and operation recovery described in [go-terminal.md](go-terminal.md).

## Decisions

- Keep the conversation central: a centered workspace, distinct message roles and spacing, a bounded reading column, and a composer that grows with its content. Supporting context is optional on wide terminals.
- Make everyday actions discoverable through a palette that accepts typing immediately. Arrow keys move through fuzzy matches; one Enter activates the selected result. Model, effort and speed remain available as advanced actions.
- Use compact overlays with responsive choice descriptions and an explicit full-details view. Narrow layouts may shorten a visible label; F2 exposes the complete question and selected option before answering.
- Reuse native library facilities: Lip Gloss layers/composition, Bubbles lists/help/textareas/viewports, Bubble Tea terminal colors, and Glamour Markdown/syntax rendering. Application code binds these components to T3; it does not replace their editors, filtering algorithm or Markdown parser.
- Keep Stop independent of ordinary commands and dialogs. Local dismissal, reconnect and quit preserve their existing meanings; T3 remains responsible for remote execution.

## Keyboard and workflow

The 23 October [UX reuse pass](ux-reuse.md) supersedes the initial shortcuts. Use the current [controls](../NATIVE-GO.md#controls): F3 commands, F6 conversations, native focused-editor movement, F4 exact editing, F5 prompt recall, Ctrl+X recoverable clear and Ctrl+Q local exit. Request selection and Stop retain their authoritative T3 identities and independent lanes.

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
