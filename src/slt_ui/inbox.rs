//! Experimental A companion chrome; native forms keep their existing controller route.
use super::{Action, Source, SurfaceState, action_bar, attention, secondary_button, ui};
use slt::{Anchor, Border, Context, KeyCode, ModalOpts};

#[derive(Debug, Default)]
pub(super) struct State {
    open: bool,
    return_focus: Option<String>,
    focus_list: bool,
}

pub(super) fn is_open(state: &SurfaceState) -> bool {
    state.companion_demo && !state.attention_mode && state.inbox.open
}

fn toggle(ctx: &mut Context, state: &mut SurfaceState) {
    if state.inbox.open {
        close(ctx, state);
    } else {
        state.inbox.return_focus = state.focused_control.clone();
        state.inbox.open = true;
        state.inbox.focus_list = true;
        ctx.request_frame();
    }
}

fn close(ctx: &mut Context, state: &mut SurfaceState) {
    state.inbox.open = false;
    state.inbox.focus_list = false;
    let focus = state
        .inbox
        .return_focus
        .take()
        .unwrap_or_else(|| "composer".into());
    ctx.focus_by_name(&focus);
    ctx.request_frame();
}

pub(super) fn header(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    let width = ctx.width();
    let background = ctx.theme().surface;
    ctx.container().h(2).bg(background).col(|ctx| {
        let items = [
            ("☰", super::sidebar_visible(state), true),
            ("A", !state.attention_mode, true),
            ("Inbox", is_open(state), true),
            (if width < 72 { "Find" } else { "Search" }, false, true),
            (if width < 72 { "⌘" } else { "Commands" }, false, true),
            (
                if width < 72 { "ⓘ" } else { "Context" },
                state.context_expanded
                    || (state.show_context && width >= 112 && view.layout != ui::LayoutMode::Focus),
                true,
            ),
        ];
        match action_bar(
            ctx,
            "toolbar",
            state.bars.entry("toolbar").or_default(),
            &items,
        ) {
            Some(0) => {
                super::toggle_sidebar(ctx, state);
                ctx.request_frame();
            }
            Some(1) => state.attention_mode = false,
            Some(2) => {
                state.attention_mode = false;
                toggle(ctx, state);
            }
            Some(3) => actions.push(Action::Command("switch".into())),
            Some(4) => actions.push(Action::Command("palette".into())),
            Some(5) => {
                super::toggle_context(state, width);
                ctx.request_frame();
            }
            _ => {}
        }
        let source = state.source.clone();
        tools(ctx, state, "demo-tools", source, actions);
    });
}

fn tools(
    ctx: &mut Context,
    state: &mut SurfaceState,
    name: &'static str,
    source: Option<Source>,
    actions: &mut Vec<Action>,
) {
    let compact = ctx.width() < 72;
    ctx.row(|ctx| {
        if !compact {
            ctx.text("Tools").dim();
        }
        let items = [
            ("Graph", false, true),
            (if compact { "Doc" } else { "Guide" }, false, true),
            (if compact { "Rev" } else { "Review" }, false, true),
            (if compact { "Mmd" } else { "Diagram" }, false, true),
            ("Form", false, true),
        ];
        if let Some(index) = action_bar(ctx, name, state.bars.entry(name).or_default(), &items) {
            actions.push(Action::DemoTool {
                tool: ["graph", "guide", "review", "mermaid", "form"][index].into(),
                source,
            });
        }
    });
}

pub(super) fn draw(ctx: &mut Context, state: &mut SurfaceState, actions: &mut Vec<Action>) {
    if !is_open(state) {
        return;
    }
    let width = ctx.width().saturating_sub(2).clamp(1, 58);
    let height = ctx.height().saturating_sub(3).max(1);
    let mut dismiss = false;
    let action_start = actions.len();
    ctx.modal_with(
        ModalOpts::new().anchor(Anchor::TopRight).offset(1, 2),
        |ctx| {
            let background = ctx.theme().bg;
            ctx.container()
                .w(width)
                .h(height)
                .bg(background)
                .col(|ctx| {
                    ctx.row(|ctx| {
                        ctx.text("Inbox").bold().grow(1);
                        dismiss = secondary_button(ctx, "Close").clicked;
                    });
                    ctx.container()
                        .w(width)
                        .h(3)
                        .border(Border::Rounded)
                        .title("Local demo review")
                        .row(|ctx| {
                            ctx.text("Review this demo").grow(1).truncate();
                            if secondary_button(ctx, "Open review").clicked {
                                actions.push(Action::DemoTool {
                                    tool: "review".into(),
                                    source: state.source.clone(),
                                });
                            }
                        });
                    attention::draw_inbox(
                        ctx,
                        state,
                        width,
                        height.saturating_sub(6).max(1),
                        actions,
                    );
                    let source = state
                        .selected_question
                        .as_ref()
                        .and_then(|key| state.questions.iter().find(|item| &item.key == key))
                        .map(|item| item.source.clone())
                        .or_else(|| state.source.clone());
                    tools(ctx, state, "inbox-tools", source, actions);
                    ctx.text("Enter opens native form · Esc closes")
                        .dim()
                        .truncate();
                });
            if std::mem::take(&mut state.inbox.focus_list) {
                ctx.focus_by_name("attention-list");
                ctx.request_frame();
            }
            dismiss |= ctx.consume_key_code(KeyCode::Esc);
        },
    );
    // A native form takes over through the controller; browsing never opens A.
    if actions[action_start..]
        .iter()
        .any(|action| matches!(action, Action::Answer(_) | Action::InspectQuestion(_)))
    {
        dismiss = true;
    }
    if dismiss {
        close(ctx, state);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::slt_ui::{AttentionItem, draw, tests::ResizableBackend};

    #[test]
    fn inbox_browse_and_escape_keep_attached_source_and_exact_draft() {
        for (width, height) in [(140, 38), (70, 22), (40, 12)] {
            let mut state = SurfaceState::new();
            state.companion_demo = true;
            state.active_key = "original".into();
            state.source = Some(Source {
                environment: "original-env".into(),
                thread: "original-thread".into(),
                ..Source::default()
            });
            state.questions = ["first", "second"]
                .into_iter()
                .map(|key| AttentionItem {
                    key: key.into(),
                    title: key.into(),
                    source: Source {
                        environment: "other-env".into(),
                        thread: key.into(),
                        ..Source::default()
                    },
                    ..AttentionItem::default()
                })
                .collect();
            let source = state.source.clone();
            let draft = "Exact café 日本語\nKeep this draft.";
            let mut editor = ui::editor(draft, "violet", "");
            editor.move_cursor(ratatui_textarea::CursorMove::Back);
            let caret = editor.cursor();
            let mut modal = ui::editor("", "violet", "");
            let view = ui::View::default();
            let mut backend = ResizableBackend::new(width, height);
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal, None, &mut state);
                state.composer.cursor_row = 0;
                state.composer.cursor_col = 4;
                ctx.focus_by_name("composer");
                state.focused_control = Some("composer".into());
                toggle(ctx, &mut state);
            });
            for _ in 0..3 {
                backend.render(|ctx| {
                    let actions = draw(ctx, &view, &mut editor, &mut modal, None, &mut state);
                    assert!(
                        actions.is_empty(),
                        "opening Inbox must not dispatch an action"
                    );
                });
            }
            backend.assert_contains("Local demo review");
            backend.assert_not_contains("Library");
            backend.run_with_events(
                slt::EventBuilder::new().key_code(KeyCode::Down).build(),
                |ctx| {
                    assert!(draw(ctx, &view, &mut editor, &mut modal, None, &mut state).is_empty());
                },
            );
            backend.run_with_events(
                slt::EventBuilder::new().key_code(KeyCode::Esc).build(),
                |ctx| {
                    assert!(draw(ctx, &view, &mut editor, &mut modal, None, &mut state).is_empty());
                },
            );
            assert!(!is_open(&state));
            assert_eq!(state.source, source);
            assert_eq!(state.active_key, "original");
            assert_eq!(editor.lines().join("\n"), draft);
            assert_eq!(editor.cursor(), caret);
            assert_eq!(state.composer.value(), draft);
            assert_eq!(
                (state.composer.cursor_row, state.composer.cursor_col),
                (0, 4)
            );
        }
    }

    #[test]
    fn companion_chrome_is_opt_in_and_tools_emit_the_attached_source_directly() {
        let mut state = SurfaceState::new();
        state.source = Some(Source {
            environment: "env".into(),
            thread: "thread".into(),
            ..Source::default()
        });
        let mut backend = ResizableBackend::new(100, 26);
        let mut editor = ui::editor("unchanged", "violet", "");
        let mut modal = ui::editor("", "violet", "");
        let view = ui::View::default();
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal, None, &mut state);
            });
        }
        backend.assert_not_contains("Inbox");
        backend.assert_contains(" B ");
        state.companion_demo = true;
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal, None, &mut state);
            });
        }
        backend.assert_contains("Inbox");
        for (label, expected) in [
            ("Graph", "graph"),
            ("Guide", "guide"),
            ("Review", "review"),
            ("Diagram", "mermaid"),
            ("Form", "form"),
        ] {
            let (x, y) = backend
                .find_text(label)
                .expect("direct companion action painted");
            let mut actions = Vec::new();
            backend.run_with_events(slt::EventBuilder::new().click(x, y).build(), |ctx| {
                actions = draw(ctx, &view, &mut editor, &mut modal, None, &mut state);
            });
            assert!(
                matches!(actions.as_slice(), [Action::DemoTool { tool, source }]
                if tool == expected && source == &state.source)
            );
        }
        assert_eq!(editor.lines().join("\n"), "unchanged");
    }
}
