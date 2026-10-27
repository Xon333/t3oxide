# UX refinement — 0.7.1

The existing Go/Bubble Tea client now uses a smaller set of visible controls, explicit browse/attachment labels, and reviews that preserve the exact source and local editing state. This is a client presentation and interaction change. T3, the host adapter, companion-v1, orchestration-v2, persisted state and the retained Swift client keep their existing owners and contracts.

## Interaction rules

| Journey | Implemented behavior | Owner |
| --- | --- | --- |
| Choose a conversation | Conversations names the navigation surface; Browse identifies its environment. Filter loaded affects the current page; Search conversations queries the host. Highlighting does not attach. | `internal/ui/navigator.go`, `surface_view.go` |
| Act on a sidebar row | More captures its exact page and selected conversation. Open, New, Settings, details and expansion use that reviewed source. Refresh, replacement, failure, loading or attachment change retires the menu. Disabled controls cannot fall through stale Bubbles filtered items. | `navigator.go` |
| Inspect navigation | Ctrl+L opens complete metadata for the selected source without reading its transcript. Back preserves attachment, draft, query and reading position. | `navigator.go`, `commands.go` |
| Create | Message → title → Create/Cancel precedes optional model fields and Editor. Title Enter focuses Create only. Explicit Create remains the sole dispatch point. Exact launch checkpoint ownership is unchanged. | `browser_launch.go` |
| Compose and read | Native editor semantics, exact bytes, rejected-input recovery and command completion remain intact. Completion is visible only over composing. Textual focus cues complement color. Commands opened by keyboard or toolbar returns to its invoking pane. | `completion.go`, `workspace_layout.go`, `view.go` |
| Settings | Choosers select the staged current value. Reconfirming the same model preserves options. Saved and staged values are distinct; full Details retains complete provider descriptions and source identity. Apply remains the only mutation. | `audit_forms.go` |
| Questions and approvals | Progress, required/optional state, selected answer and reachable controls remain visible in compact reviews. Empty custom editors collapse; clearing a custom answer clears its staged value. Back/Next preserves answers. Esc cancels review; it does not answer or promise persistence across reopening. | `audit_requests.go` |
| Local overlays | Commands preserves a search query/caret or result viewport/copy source. Return state is attachment-bound; nested local choices cannot revive a retired owning prompt. Closed details are no longer a copy target. | `audit_palette.go`, `dialog.go`, `commands.go` |
| Requests, Stop and recovery | Conditional toolbar controls prioritize Requests, Stop and Saved. Separate saved-action and saved-Stop states remain visible before receipt text. Details exposes receipt identity and distinguishes acknowledgement from response. Retry/Resolve availability reflects each lane separately. | `workspace_refinement.go`, `command_catalog.go` |
| Activity/results | The existing bounded source-owned inspector remains. Context scrolls the selected activity into view, shares that geometry with mouse targets, and rejects keyboard inspection if the reviewed item changed. Closing a result returns to the workspace; reopening Activity creates a fresh client-owned picker. | `activity_strip.go`, `workspace_layout.go`, `workspace_mouse.go` |
| Resize and appearance | A disappearing activity/context/navigation pane releases focus to reading without editing the draft. Context uses the local review below 16 rows, keeping activity reachable without invisible selection. Read only and historical live-paused state have text labels. Themes, Terminal inheritance and mouse-off native selection remain. | `model.go`, `workspace_refinement.go`, `workspace_layout.go`, `view.go` |

The compact toolbar does not enumerate every command. The single command catalog retains existing aliases and explicit unavailable reasons. Ctrl+S keeps its independent lane even while an ordinary review is open. In an editor Ctrl+B remains native cursor movement; with transcript focus it toggles the optional context pane. `/navigator` controls the conversation sidebar.

## Existing components and independent source research

The implementation keeps the pinned Bubbles list, textarea, textinput, help and viewport; Bubble Tea event routing; Lip Gloss composition; and Glamour Markdown. No dependency or library fork is added. The prior MIT t3term notices and attribution remain in `NOTICE` and `LICENSES/`.

The current independent research and revision manifest accompany the return package. Patterns were adapted into the existing owners, not imported as a second application:

- t3term's [`Focus`, `cycle`, `sidebar_width` and composer drawing](https://github.com/krishhgg/t3term/blob/d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e/src/tui/mod.rs) support explicit visible-pane focus and compact optional detail. Its [picker](https://github.com/krishhgg/t3term/blob/d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e/src/tui/picker.rs) is a source reference for current-choice selection. t3oxide retains its stronger exact-input, launch, revision and durable-recovery contracts.
- lazygit's [contextual options](https://github.com/jesseduffield/lazygit/blob/4e1eebe278790f257a875da6633234391a5a85b3/pkg/gui/options_map.go) and [context return stack](https://github.com/jesseduffield/lazygit/blob/4e1eebe278790f257a875da6633234391a5a85b3/pkg/gui/context.go) support a short current-context footer plus complete command discovery and deliberate overlay return. Its observed head was dated 2026-10-17. Remote prompt lifetime and attachment validation remain t3oxide's responsibility.
- Yazi's [context-specific default keymap](https://github.com/sxyazi/yazi/blob/40a91421893bfd1ae02ef7a55afd41f886558d8c/yazi-config/preset/keymap-default.toml), observed head dated 2026-10-23, separates filter/find/search/inspection scope and local modal actions. This supports precise t3oxide labels without importing Vim editing, filesystem task semantics or search plugins.

The public heads of these three references were independently checked on 2026-10-27. These are source-backed design references, not runtime or performance acceptance. No new code or dependency from lazygit or Yazi is included. The accompanying research records the full flow trace, exact primary sources and rejected patterns, including t3term's trimmed request answers and composer reuse, which would violate t3oxide's exact-input/review contract.

## Meaningful regressions

| Risk | Regression evidence |
| --- | --- |
| Menu highlight/refresh retargets an operation | `navigation_ux_test.go`: actual initial filtered menu activation, exact lazy target, stale-page rejection, disabled no-match source |
| Enter accidentally creates a conversation | `navigation_ux_test.go`: 40×12/60×16/120×35 explicit confirmation and retained launch input; existing launch integration tests |
| Same-model selection destroys staged options | `seamless_dialogs_test.go`: staged model/option identity, same-model confirmation/cycle, exact Apply |
| Hidden custom answer passes required validation | `seamless_dialogs_test.go`: clear→Submit blocked, offered value remains distinct from custom text |
| Overlay loses search/result or revives stale review | `seamless_dialogs_test.go`: caret, viewport/copy source, attachment generation and nested owner retirement |
| Compact form loses a required control | `seamless_dialogs_test.go`: ordinary/full-page forms at 40×12 and 60×16, complete Details and independent Stop/exit controls |
| Toolbar changes focus or hit target | `workspace_refinement_test.go`: real rendered mouse zone, invoking focus and draft retained |
| Context selection becomes invisible or changes identity | `workspace_refinement_test.go`: 24-item scroll, visible source-bound hit zone and replacement-item rejection |
| Receipt hides one recovery lane | `workspace_refinement_test.go`: both lanes and draft failure at compact sizes, exact receipt retained in Details |
| Hidden pane or completion consumes input | `workspace_refinement_test.go`: stream removal, setting-driven pane removal, completion under navigation and closed-detail copying |
| Find opened from another pane loses keys | `workspace_review_test.go`: actual Commands selection, typing/paste, exclusive native editor, Escape restoration and exact draft/attachment/read anchor |
| Very short context hides an inspectable item | `workspace_review_test.go`: 170×12 with one body row, Context-dialog fallback, no hidden key/mouse target and safe resize recovery |

The existing integration suite continues to cover exact input, causal receipts, startup/browser attachment, source-bound launch/settings, request revisions, queued messages, independent Stop and journal recovery. [Verification](verification.md) records checks actually run and their limits. Fixture cell renders are not native GUI, physical-key or installed-instance acceptance.

## Compatibility and delivery

Version is `0.7.1-bubbletea`. Go modules, checks/build scripts, client execution owners, adapter, wire/config packages and Swift rollback source are unchanged. No state migration, new host component, watcher, service or account is introduced. Existing compatible adapters do not need redeployment for this pass.

The native worker applies the baseline patch to the real checkout, resolves concurrent writers, builds and verifies the native ARM64 release, then uses the existing client installation mechanism. The same reviewed native release is transferred to the second client and atomically published through its established updater. Full private destination instructions are supplied separately from maintained source. Source tests and a cross-build do not establish that installation has happened.
