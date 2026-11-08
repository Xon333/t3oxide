//! A source-bound, windowed conversation list with one keyboard focus target.

use super::{Action, action_bar, elide, elide_lines, quiet_tabs};
use crate::{reports::Geometry, ui};
use slt::{Border, Color, Context, KeyCode, SpinnerState, TabsState, palette::tailwind};

#[derive(Debug, Default)]
pub(super) struct State {
    pub(super) demo: bool,
    keys: Vec<String>,
    selected: usize,
    controller_selection: usize,
    top: usize,
    lanes: TabsState,
    tools: usize,
}

pub(super) struct Presentation<'a> {
    pub colors: &'a [(Color, Color)],
    pub attached_key: &'a str,
}

pub(super) fn draw(
    ctx: &mut Context,
    view: &ui::View,
    presentation: Presentation<'_>,
    state: &mut State,
    geometry: Geometry,
    spinner: &SpinnerState,
    actions: &mut Vec<Action>,
) {
    let Geometry { width, height } = geometry;
    let theme = *ctx.theme();
    let shared_scope = view
        .rows
        .first()
        .map(|row| row.subtitle.as_str())
        .filter(|scope| !scope.is_empty() && view.rows.iter().all(|row| row.subtitle == *scope));
    let response = ctx
        .group("t3oxide.navigator")
        .w(width)
        .h(height)
        .border(Border::Rounded)
        .title(if state.demo {
            "Conversations"
        } else {
            "Browse"
        })
        .focus_within_border_fg(theme.primary)
        .col(|ctx| {
            state.lanes.labels = if state.demo && width >= 31 {
                ["● Unread", "◌ Run", "◆ Pins"].map(str::to_owned).to_vec()
            } else if width < 33 {
                ["Unread", "Run", "Pins"].map(str::to_owned).to_vec()
            } else {
                ["Unread", "Running", "Pins"].map(str::to_owned).to_vec()
            };
            state.lanes.selected = match view.lane.as_str() {
                "Running" => 1,
                "Pins" => 2,
                _ => 0,
            };
            let focused = ctx.register_focusable_named("navigator-lanes");
            let tabs = quiet_tabs(ctx, &mut state.lanes);
            // An external lane (e.g. Settled) shares the first tab's cursor.
            // Explicit activation must still select its actual native query.
            if tabs.changed
                || tabs.clicked
                || focused && (ctx.consume_key_code(KeyCode::Enter) || ctx.consume_key(' '))
            {
                actions.push(Action::Lane(
                    ["Open", "Running", "Pins"][state.lanes.selected].into(),
                ));
            }
            match action_bar(
                ctx,
                "navigator-tools",
                &mut state.tools,
                &[
                    ("Project", false, true),
                    ("Env", false, true),
                    ("Settled", view.lane == "Settled", true),
                ],
            ) {
                Some(0) => actions.push(Action::Command("project".into())),
                Some(1) => actions.push(Action::Command("env".into())),
                Some(2) => actions.push(Action::Lane("Settled".into())),
                _ => {}
            }
            if let Some(scope) = shared_scope {
                ctx.text(elide(scope, width.saturating_sub(2) as usize))
                    .dim();
            }
            let separator_rows = if state.demo && height >= 16 {
                2
            } else {
                u32::from(height >= 14)
            };
            ctx.container().h(separator_rows).col(|ctx| {
                if separator_rows > 0 {
                    if state.demo {
                        ctx.text(" ");
                    } else {
                        ctx.text("─".repeat(width.saturating_sub(2) as usize)).dim();
                    }
                }
            });
            let keys: Vec<_> = view.rows.iter().map(|row| row.key.clone()).collect();
            let changed = state.keys != keys;
            if changed || state.controller_selection != view.selected {
                state.selected = view.selected.min(view.rows.len().saturating_sub(1));
                state.controller_selection = view.selected;
                state.keys = keys;
            }
            let focused = ctx.register_focusable_named("navigator");
            let _ = ctx.register_focusable();
            let long_selected = height >= 14
                && view.rows.get(state.selected).is_some_and(|row| {
                    row.key == presentation.attached_key
                        && unicode_width::UnicodeWidthStr::width(row.title.as_str())
                            > width.saturating_sub(6) as usize * 2
                });
            let extra_selected_row =
                u32::from(shared_scope.is_none() && height >= 10) + u32::from(long_selected);
            let record_gap = u32::from(state.demo && height >= 16);
            let capacity = (height.saturating_sub(
                5 + separator_rows + u32::from(shared_scope.is_some()) + extra_selected_row,
            ) / (2 + record_gap))
                .max(1) as usize;
            let mut open = false;
            let before = state.selected;
            let keys: Vec<_> = ctx
                .key_presses_when(focused)
                .map(|(i, key)| (i, key.code.clone()))
                .collect();
            for (index, key) in keys {
                match key {
                    KeyCode::Up => state.selected = state.selected.saturating_sub(1),
                    KeyCode::Down => {
                        state.selected = (state.selected + 1).min(view.rows.len().saturating_sub(1))
                    }
                    KeyCode::PageUp => state.selected = state.selected.saturating_sub(capacity),
                    KeyCode::PageDown => {
                        state.selected =
                            (state.selected + capacity).min(view.rows.len().saturating_sub(1))
                    }
                    KeyCode::Home => state.selected = 0,
                    KeyCode::End => state.selected = view.rows.len().saturating_sub(1),
                    KeyCode::Enter => open = true,
                    _ => continue,
                }
                ctx.consume_event(index);
            }
            if state.selected < state.top {
                state.top = state.selected;
            }
            if state.selected >= state.top + capacity {
                state.top = state.selected + 1 - capacity;
            }
            state.top = state.top.min(view.rows.len().saturating_sub(capacity));
            ctx.container().grow(1).col(|ctx| {
                if view.rows.is_empty() {
                    ctx.text("No conversations").dim();
                }
                let end = (state.top + capacity).min(view.rows.len());
                for index in state.top..end {
                    let row = &view.rows[index];
                    let selected = state.selected == index;
                    let attached = row.key == presentation.attached_key;
                    let filled = selected && attached;
                    let record_bg = if filled || (index % 2 == 0 && !state.demo) {
                        theme.surface
                    } else if state.demo && index % 2 == 0 {
                        theme.surface.blend_f64(theme.bg, 0.35)
                    } else {
                        theme.bg
                    };
                    let show_scope = shared_scope.is_none()
                        && (selected
                            || index == state.top
                            || view.rows[index - 1].subtitle != row.subtitle);
                    let title_rows = if selected && long_selected {
                        3
                    } else if !show_scope || selected && extra_selected_row > 0 {
                        2
                    } else {
                        1
                    };
                    let interaction = ctx
                        .container()
                        .h(2 + u32::from(selected) * extra_selected_row)
                        .bubble_clicks()
                        .bg(record_bg)
                        .col(|ctx| {
                            ctx.row(|ctx| {
                                ctx.text(if focused && selected {
                                    "┃"
                                } else if selected {
                                    "│"
                                } else if attached {
                                    "·"
                                } else {
                                    " "
                                })
                                .fg(
                                    if filled || focused && selected {
                                        theme.primary
                                    } else {
                                        theme.text_dim
                                    },
                                );
                                match row.status.as_str() {
                                    "working" | "running" => {
                                        if state.demo {
                                            ctx.text(spinner.frame(ctx.tick() / 8).to_string())
                                                .dim();
                                            ctx.request_frame_after(
                                                std::time::Duration::from_millis(120),
                                            );
                                        } else {
                                            ctx.spinner(spinner);
                                        }
                                    }
                                    "failed" => {
                                        ctx.text("!").bold().fg(tailwind::RED.c400);
                                    }
                                    "needs_input" | "needs-input" => {
                                        ctx.text("?").bold().fg(tailwind::PURPLE.c400);
                                    }
                                    _ => {
                                        ctx.text(" ");
                                    }
                                }
                                ctx.text(if row.section == "Unread" {
                                    if state.demo { "●" } else { "▶" }
                                } else {
                                    " "
                                })
                                .fg(if state.demo {
                                    tailwind::CYAN.c300
                                } else {
                                    tailwind::GREEN.c400
                                });
                                ctx.text(if row.pinned { "◆" } else { " " })
                                    .fg(tailwind::AMBER.c300);
                                let title = if row.title.is_empty() {
                                    "Conversation"
                                } else {
                                    &row.title
                                };
                                let title =
                                    elide_lines(ctx, title, width.saturating_sub(6), title_rows);
                                ctx.text(title)
                                    .w(width.saturating_sub(6))
                                    .max_h(title_rows)
                                    .wrap()
                                    .fg(theme.text)
                                    .with_if(row.section != "Unread", |text| {
                                        text.dim();
                                    });
                            });
                            let (environment, project) = row
                                .subtitle
                                .split_once(" / ")
                                .unwrap_or((&row.subtitle, ""));
                            let (env_color, project_color) = presentation
                                .colors
                                .get(index)
                                .copied()
                                .unwrap_or((theme.text_dim, theme.text_dim));
                            // Preserve the prior-frame source-line interaction
                            // slot when grouping hides this row's qualifiers.
                            ctx.container().h(u32::from(show_scope)).line(|ctx| {
                                if !show_scope {
                                    return;
                                }
                                ctx.text("    ");
                                let available = width.saturating_sub(6) as usize;
                                let env_width = if project.is_empty() {
                                    available
                                } else {
                                    unicode_width::UnicodeWidthStr::width(environment)
                                        .min(available / 2)
                                };
                                ctx.text(elide(environment, env_width)).fg(env_color);
                                if !project.is_empty() {
                                    ctx.text(" · ").dim();
                                    ctx.text(elide(
                                        project,
                                        available.saturating_sub(env_width + 3),
                                    ))
                                    .fg(project_color);
                                }
                            });
                        });
                    if interaction.clicked && !changed {
                        state.selected = index;
                        open = true;
                        ctx.focus_by_name("navigator");
                    }
                    if record_gap > 0 {
                        ctx.container().h(record_gap).col(|_| {});
                    }
                }
            });
            if !view.rows.is_empty() {
                ctx.text(format!("{} / {}", state.selected + 1, view.rows.len()))
                    .dim();
                if state.selected != before {
                    state.controller_selection = state.selected;
                    actions.push(Action::Highlight(state.selected));
                }
                if open && !changed {
                    actions.push(Action::Open(state.selected));
                }
            }
        });
    if response.scroll_delta != 0 && !view.rows.is_empty() {
        let step = response.scroll_delta.unsigned_abs() as usize;
        state.selected = if response.scroll_delta > 0 {
            state.selected.saturating_sub(step)
        } else {
            (state.selected + step).min(view.rows.len() - 1)
        };
        state.controller_selection = state.selected;
        actions.push(Action::Highlight(state.selected));
        ctx.request_frame();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::slt_ui::{Source, SurfaceState, Tab};
    use slt::{EventBuilder, TestBackend};

    #[test]
    fn empty_settled_lane_returns_to_unread_by_pointer_without_retargeting_the_draft() {
        let mut backend = TestBackend::new(100, 31);
        let source = Source {
            environment: "native-env".into(),
            thread: "thread-a".into(),
            project_id: "project-a".into(),
            project: "Project".into(),
            ..Source::default()
        };
        let mut surface = SurfaceState::new();
        surface.applied_layout = Some(ui::LayoutMode::Split);
        surface.sidebar_hidden = Some(false);
        surface.active_key = "native-env/thread-a".into();
        surface.source = Some(source.clone());
        surface.tabs = vec![Tab {
            key: surface.active_key.clone(),
            title: "Active source".into(),
            source: source.clone(),
        }];
        let draft = "Unsent 日本語  \n\n";
        let mut prompt = ui::editor(draft, "violet", "");
        let mut modal = ui::editor("", "violet", "");
        let row = ui::ThreadView {
            key: "native-env/other-thread".into(),
            title: "Unread conversation".into(),
            section: "Unread".into(),
            ..Default::default()
        };
        let mut view = ui::View {
            lane: "Open".into(),
            rows: vec![row.clone()],
            ..Default::default()
        };
        for _ in 0..3 {
            backend.render(|ctx| {
                crate::slt_ui::draw(ctx, &view, &mut prompt, &mut modal, None, &mut surface);
            });
        }
        let cursor = (surface.composer.cursor_row, surface.composer.cursor_col);
        for _ in 0..2 {
            let (x, y) = backend.find_text("Settled").expect("real Settled control");
            let mut actions = Vec::new();
            backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                actions =
                    crate::slt_ui::draw(ctx, &view, &mut prompt, &mut modal, None, &mut surface);
            });
            assert!(
                matches!(actions.as_slice(), [Action::Lane(lane)] if lane == "Settled"),
                "{actions:?}"
            );
            view.lane = "Settled".into();
            view.rows.clear();
            for _ in 0..3 {
                backend.render(|ctx| {
                    assert!(
                        crate::slt_ui::draw(
                            ctx,
                            &view,
                            &mut prompt,
                            &mut modal,
                            None,
                            &mut surface
                        )
                        .is_empty()
                    );
                });
            }
            backend.assert_contains("No conversations");
            let (x, y) = backend
                .find_text("Unread")
                .expect("visible native Unread tab");
            backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                actions =
                    crate::slt_ui::draw(ctx, &view, &mut prompt, &mut modal, None, &mut surface);
            });
            assert!(
                matches!(actions.as_slice(), [Action::Lane(lane)] if lane == "Open"),
                "empty Settled->Unread: {actions:?}"
            );
            view.lane = "Open".into();
            view.rows = vec![row.clone()];
            for _ in 0..3 {
                backend.render(|ctx| {
                    crate::slt_ui::draw(ctx, &view, &mut prompt, &mut modal, None, &mut surface);
                });
            }
            backend.assert_contains("Unread conversation");
            assert_eq!(surface.source.as_ref(), Some(&source));
            assert_eq!(surface.tabs[0].source, source);
            assert_eq!(surface.active_key, "native-env/thread-a");
            assert_eq!(prompt.lines().join("\n"), draft);
            assert_eq!(
                (surface.composer.cursor_row, surface.composer.cursor_col),
                cursor
            );
        }
    }

    #[test]
    fn external_lanes_accept_unread_activation_and_native_tab_arrows() {
        for lane in ["Settled", "All"] {
            for (key, expected) in [
                (KeyCode::Enter, "Open"),
                (KeyCode::Char(' '), "Open"),
                (KeyCode::Left, "Pins"),
                (KeyCode::Right, "Running"),
            ] {
                let mut backend = TestBackend::new(36, 16);
                let mut state = State::default();
                let view = ui::View {
                    lane: lane.into(),
                    ..Default::default()
                };
                for _ in 0..3 {
                    backend.render(|ctx| {
                        draw(
                            ctx,
                            &view,
                            Presentation {
                                colors: &[],
                                attached_key: "",
                            },
                            &mut state,
                            Geometry {
                                width: 36,
                                height: 16,
                            },
                            &SpinnerState::default(),
                            &mut Vec::new(),
                        );
                        ctx.focus_by_name("navigator-lanes");
                    });
                }
                let mut actions = Vec::new();
                backend.run_with_events(EventBuilder::new().key_code(key.clone()).build(), |ctx| {
                    draw(
                        ctx,
                        &view,
                        Presentation {
                            colors: &[],
                            attached_key: "",
                        },
                        &mut state,
                        Geometry {
                            width: 36,
                            height: 16,
                        },
                        &SpinnerState::default(),
                        &mut actions,
                    );
                });
                assert!(
                    matches!(actions.as_slice(), [Action::Lane(value)] if value == expected),
                    "{lane} {key:?}: {actions:?}"
                );
            }
        }
    }

    #[test]
    fn native_lane_pointer_queries_preserve_running_pins_and_unread_mapping() {
        for (lane, label, expected) in [
            ("Settled", "Unread", "Open"),
            ("All", "Unread", "Open"),
            ("Running", "Unread", "Open"),
            ("Settled", "Running", "Running"),
            ("All", "Pins", "Pins"),
        ] {
            let mut backend = TestBackend::new(36, 16);
            let mut state = State::default();
            let view = ui::View {
                lane: lane.into(),
                ..Default::default()
            };
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(
                        ctx,
                        &view,
                        Presentation {
                            colors: &[],
                            attached_key: "",
                        },
                        &mut state,
                        Geometry {
                            width: 36,
                            height: 16,
                        },
                        &SpinnerState::default(),
                        &mut Vec::new(),
                    );
                });
            }
            let (x, y) = backend.find_text(label).expect("native tab is visible");
            let mut actions = Vec::new();
            backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "",
                    },
                    &mut state,
                    Geometry {
                        width: 36,
                        height: 16,
                    },
                    &SpinnerState::default(),
                    &mut actions,
                );
            });
            assert!(
                matches!(actions.as_slice(), [Action::Lane(value)] if value == expected),
                "{lane}->{label}: {actions:?}"
            );
        }
    }

    #[test]
    fn shared_scope_is_grouped_once_and_releases_a_second_title_line() {
        let mut backend = TestBackend::new(32, 18);
        let view = ui::View {
            lane: "Open".into(),
            rows: vec![
                ui::ThreadView {
                    key: "build/one".into(),
                    title: "Review shipment authorization".into(),
                    subtitle: "Build / Alpha".into(),
                    section: "Unread".into(),
                    ..Default::default()
                },
                ui::ThreadView {
                    key: "build/two".into(),
                    title: "Second conversation".into(),
                    subtitle: "Build / Alpha".into(),
                    ..Default::default()
                },
            ],
            ..Default::default()
        };
        let mut state = State::default();
        let mut actions = Vec::new();
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "",
                    },
                    &mut state,
                    Geometry {
                        width: 32,
                        height: 18,
                    },
                    &SpinnerState::default(),
                    &mut actions,
                );
            });
        }
        backend.assert_contains("authorization");
        backend.assert_contains("Build / Alpha");
        let (x, y) = backend.find_text("Second conversation").unwrap();
        actions.clear();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(
                ctx,
                &view,
                Presentation {
                    colors: &[],
                    attached_key: "",
                },
                &mut state,
                Geometry {
                    width: 32,
                    height: 18,
                },
                &SpinnerState::default(),
                &mut actions,
            );
        });
        assert!(
            actions
                .iter()
                .any(|action| matches!(action, Action::Open(1)))
        );
    }

    #[test]
    fn state_pin_read_and_focus_each_keep_their_own_visual_signal() {
        let view = ui::View {
            rows: vec![
                ui::ThreadView {
                    key: "broken".into(),
                    title: "Broken".into(),
                    status: "failed".into(),
                    pinned: true,
                    section: "Unread".into(),
                    ..ui::ThreadView::default()
                },
                ui::ThreadView {
                    key: "input".into(),
                    title: "Question".into(),
                    status: "needs_input".into(),
                    section: "Unread".into(),
                    ..ui::ThreadView::default()
                },
                ui::ThreadView {
                    key: "opened".into(),
                    title: "Opened".into(),
                    section: "Completed".into(),
                    ..ui::ThreadView::default()
                },
            ],
            ..ui::View::default()
        };
        let mut state = State::default();
        let mut backend = TestBackend::new(36, 16);
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "",
                    },
                    &mut state,
                    Geometry {
                        width: 36,
                        height: 16,
                    },
                    &SpinnerState::default(),
                    &mut Vec::new(),
                );
                ctx.focus_by_name("navigator");
            });
        }
        backend.assert_contains("┃!▶◆Broken");
        for (symbol, color) in [
            ("!", tailwind::RED.c400),
            ("?", tailwind::PURPLE.c400),
            ("▶", tailwind::GREEN.c400),
        ] {
            let (x, y) = backend.find_text(symbol).unwrap();
            assert_eq!(backend.buffer().get(x, y).style.fg, Some(color));
        }
        let (x, y) = backend.find_text("!").unwrap();
        assert!(
            backend
                .buffer()
                .get(x, y)
                .style
                .modifiers
                .contains(slt::Modifiers::BOLD)
        );
        let (x, y) = backend.find_text("Opened").unwrap();
        assert!(
            backend
                .buffer()
                .get(x, y)
                .style
                .modifiers
                .contains(slt::Modifiers::DIM)
        );
        backend.assert_not_contains("●");
    }

    #[test]
    fn arrows_enter_and_mouse_share_selection_without_extra_row_focus() {
        let view = ui::View {
            rows: (0..80)
                .map(|index| ui::ThreadView {
                    key: format!("thread-{index}"),
                    title: format!("Conversation {index}"),
                    section: "Unread".into(),
                    ..ui::ThreadView::default()
                })
                .collect(),
            ..ui::View::default()
        };
        let mut state = State::default();
        let mut backend = TestBackend::new(32, 16);
        let mut actions = Vec::new();
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "",
                    },
                    &mut state,
                    Geometry {
                        width: 32,
                        height: 16,
                    },
                    &SpinnerState::default(),
                    &mut actions,
                );
                ctx.focus_by_name("navigator");
            });
        }
        actions.clear();
        let (x, y) = backend
            .find_text("Conversation 2")
            .expect("third row is visible");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(
                ctx,
                &view,
                Presentation {
                    colors: &[],
                    attached_key: "",
                },
                &mut state,
                Geometry {
                    width: 32,
                    height: 16,
                },
                &SpinnerState::default(),
                &mut actions,
            );
        });
        assert!(
            actions
                .iter()
                .any(|action| matches!(action, Action::Open(2))),
            "pointer must open the displayed row, got {actions:?}"
        );
        actions.clear();
        backend.run_with_events(
            EventBuilder::new()
                .key_code(KeyCode::End)
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "",
                    },
                    &mut state,
                    Geometry {
                        width: 32,
                        height: 16,
                    },
                    &SpinnerState::default(),
                    &mut actions,
                );
            },
        );
        backend.assert_contains("Conversation 79");
        backend.assert_not_contains("more");
        assert!(actions.iter().any(|a| matches!(a, Action::Open(79))));
        let mut focus_count = 0;
        backend.render(|ctx| {
            focus_count = ctx.focus_count();
            draw(
                ctx,
                &view,
                Presentation {
                    colors: &[],
                    attached_key: "",
                },
                &mut state,
                Geometry {
                    width: 32,
                    height: 16,
                },
                &SpinnerState::default(),
                &mut actions,
            );
        });
        assert!(
            focus_count < 8,
            "row count must not grow the Tab cycle: {focus_count}"
        );
    }
    #[test]
    fn wheel_then_source_line_click_opens_the_previously_displayed_conversation() {
        let mut backend = TestBackend::new(36, 20)
            .with_run_config(slt::RunConfig::default().activation_boundaries(true));
        let mut state = State::default();
        let mut view = ui::View {
            selected: 1,
            rows: ["A1", "A2", "A3", "B1"]
                .into_iter()
                .map(|title| ui::ThreadView {
                    key: title.into(),
                    title: title.into(),
                    subtitle: if title.starts_with('A') {
                        "Host / Shared project"
                    } else {
                        "Host / Other project"
                    }
                    .into(),
                    ..Default::default()
                })
                .collect(),
            ..Default::default()
        };
        let mut attached = "A1".to_owned();
        let mut highlights = Vec::new();
        let mut opened = Vec::new();
        let mut callback = |ctx: &mut Context| {
            let mut actions = Vec::new();
            draw(
                ctx,
                &view,
                Presentation {
                    colors: &[],
                    attached_key: &attached,
                },
                &mut state,
                Geometry {
                    width: 36,
                    height: 20,
                },
                &SpinnerState::default(),
                &mut actions,
            );
            // The production controller accepts Highlight before its next draw
            // and resolves Open against the same visible source inventory.
            for action in actions {
                match action {
                    Action::Highlight(index) => {
                        view.selected = index;
                        highlights.push(index);
                    }
                    Action::Open(index) => {
                        attached = view.rows[index].key.clone();
                        opened.push(attached.clone());
                    }
                    other => panic!("unexpected navigator action: {other:?}"),
                }
            }
        };
        for _ in 0..3 {
            backend.render(&mut callback);
        }
        let (x, y) = backend.find_text("A2").unwrap();
        let source_y = y + 1;
        assert!(
            backend
                .to_string_trimmed()
                .lines()
                .nth(source_y as usize)
                .unwrap()
                .contains("Host · Shared project"),
            "the press must target A2's displayed source line"
        );
        backend.run_with_events(
            EventBuilder::new()
                .scroll_down(x, source_y)
                .click(x, source_y)
                .build(),
            &mut callback,
        );
        for _ in 0..4 {
            if !backend.has_pending_input() {
                break;
            }
            backend.render(&mut callback);
        }
        assert!(!backend.has_pending_input());
        drop(callback);
        assert_eq!(opened, ["A2"], "controller highlights: {highlights:?}");
        assert_eq!(highlights, [2, 1]);
        assert_eq!(attached, "A2");
        assert_eq!(view.selected, 1);
    }

    #[test]
    fn two_row_titles_paint_graphemes_and_a_final_overflow_cue_without_changing_source() {
        let title = "Inspect 日本語 e\u{301} 👩‍💻 source changes and preserve every original request revision before delivery";
        let view = ui::View {
            rows: vec![
                ui::ThreadView {
                    key: "env/original".into(),
                    title: title.into(),
                    subtitle: "Host / Alpha".into(),
                    ..Default::default()
                },
                ui::ThreadView {
                    key: "env/other".into(),
                    title: "Short 日本語".into(),
                    subtitle: "Host / Beta".into(),
                    ..Default::default()
                },
            ],
            ..Default::default()
        };
        let mut state = State::default();
        let mut backend = TestBackend::new(30, 22);
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "env/other",
                    },
                    &mut state,
                    Geometry {
                        width: 30,
                        height: 22,
                    },
                    &SpinnerState::default(),
                    &mut Vec::new(),
                );
            });
        }
        let (x, y) = backend.find_text("Inspect").unwrap();
        let text = backend.to_string_trimmed();
        let lines: Vec<_> = text.lines().collect();
        assert!(!lines[y as usize].contains('…'));
        assert!(lines[y as usize + 1].contains('…'), "{text}");
        for full in ["日本語", "e\u{301}", "👩‍💻", "Short 日本語"] {
            backend.assert_contains(full);
        }
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y + 1).build(), |ctx| {
            draw(
                ctx,
                &view,
                Presentation {
                    colors: &[],
                    attached_key: "env/other",
                },
                &mut state,
                Geometry {
                    width: 30,
                    height: 22,
                },
                &SpinnerState::default(),
                &mut actions,
            );
        });
        assert!(matches!(actions.as_slice(), [Action::Open(0)]));
        assert_eq!(view.rows[0].title, title);
        assert_eq!(view.rows[0].key, "env/original");
    }

    #[test]
    fn selected_title_expands_without_attaching_the_browse_cursor() {
        let mut backend = TestBackend::new(30, 22);
        let mut state = State::default();
        let mut view = ui::View {
            rows: vec![
                ui::ThreadView {
                    key: "other".into(),
                    title: "Review authorization before release".into(),
                    subtitle: "Host / Shared project".into(),
                    section: "Unread".into(),
                    ..Default::default()
                },
                ui::ThreadView {
                    key: "active".into(),
                    title: "Attached source".into(),
                    subtitle: "Host / Shared project".into(),
                    ..Default::default()
                },
                ui::ThreadView {
                    key: "third".into(),
                    title: "Third source".into(),
                    subtitle: "Host / Other project".into(),
                    ..Default::default()
                },
            ],
            ..Default::default()
        };
        let mut actions = Vec::new();
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "active",
                    },
                    &mut state,
                    Geometry {
                        width: 30,
                        height: 22,
                    },
                    &SpinnerState::default(),
                    &mut actions,
                );
                ctx.focus_by_name("navigator");
            });
        }
        backend.assert_contains("before release");
        assert_eq!(
            backend.to_string_trimmed().matches("Shared proj").count(),
            1
        );
        let (x, y) = backend.find_text("Review authorization").unwrap();
        assert_ne!(
            backend.buffer().get(x, y).style.bg,
            Some(slt::Theme::default().selected_bg)
        );
        let (x, y) = backend.find_text("Attached source").unwrap();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(
                ctx,
                &view,
                Presentation {
                    colors: &[],
                    attached_key: "active",
                },
                &mut state,
                Geometry {
                    width: 30,
                    height: 22,
                },
                &SpinnerState::default(),
                &mut actions,
            );
        });
        assert!(
            actions
                .iter()
                .any(|action| matches!(action, Action::Open(1)))
        );
        // The controller accepts Highlight(1) before the next surface draw.
        view.selected = 1;
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    Presentation {
                        colors: &[],
                        attached_key: "active",
                    },
                    &mut state,
                    Geometry {
                        width: 30,
                        height: 22,
                    },
                    &SpinnerState::default(),
                    &mut Vec::new(),
                );
            });
        }
        let (x, y) = backend.find_text("Attached source").unwrap();
        assert_eq!(
            backend.buffer().get(x, y).style.bg,
            Some(slt::Theme::default().surface)
        );
        assert_eq!(
            backend.buffer().get(x, y).style.fg,
            Some(slt::Theme::default().text_dim)
        );
        assert!(
            !backend
                .buffer()
                .get(x, y)
                .style
                .modifiers
                .contains(slt::Modifiers::BOLD)
        );
    }
}
