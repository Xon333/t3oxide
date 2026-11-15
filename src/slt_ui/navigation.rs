//! Compact source-qualified conversation and native-question navigation.
use super::{
    Action, AttentionItem, Border, ButtonVariant, Context, KeyCode, ListState, ScrollState, Source,
    SurfaceState, identity_color, quiet_button, reports, tailwind, ui,
};
use std::{
    cell::Cell,
    collections::{BTreeMap, BTreeSet},
};

#[derive(Debug, Default)]
pub(super) struct State {
    collapsed: BTreeSet<String>,
    list: ListState,
    keys: Vec<String>,
    observed_selection: Option<String>,
    displayed: Option<(String, String, String, String)>,
    content_geometry: Option<(u32, u32)>,
    list_scroll: ScrollState,
    content_scroll: ScrollState,
}

#[derive(Debug, PartialEq, Eq)]
enum Entry {
    Project {
        key: String,
        title: String,
        count: usize,
    },
    Thread(usize),
}

fn entries(view: &ui::View, state: &SurfaceState) -> Vec<Entry> {
    let mut groups: Vec<(String, String, Vec<usize>)> = Vec::new();
    let mut positions = BTreeMap::new();
    for (index, row) in view.rows.iter().enumerate() {
        let source = state.thread_sources.get(&row.key);
        let key = source.map_or_else(|| row.subtitle.clone(), Source::project_key);
        let title = source
            .map(|source| source.project.clone())
            .filter(|name| !name.is_empty())
            .or_else(|| {
                row.subtitle
                    .split_once(" / ")
                    .map(|(_, project)| project.to_owned())
            })
            .filter(|name| !name.is_empty())
            .unwrap_or_else(|| "Conversations".into());
        let position = *positions.entry(key.clone()).or_insert_with(|| {
            groups.push((key.clone(), title, Vec::new()));
            groups.len() - 1
        });
        groups[position].2.push(index);
    }
    let mut entries = Vec::new();
    for (key, title, indices) in groups {
        let collapsed = state.navigation.collapsed.contains(&key);
        entries.push(Entry::Project {
            key,
            title,
            count: indices.len(),
        });
        if !collapsed {
            entries.extend(indices.into_iter().map(Entry::Thread));
        }
    }
    entries
}

fn entry_key(entry: &Entry, view: &ui::View) -> String {
    match entry {
        Entry::Project { key, .. } => format!("project:{key}"),
        Entry::Thread(index) => format!("thread:{}", view.rows[*index].key),
    }
}

fn toggle_project(state: &mut State, key: &str) {
    if !state.collapsed.remove(key) {
        state.collapsed.insert(key.to_owned());
    }
}

pub(super) fn draw_navigator(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let width = if ctx.width() >= 110 { 32 } else { 30 };
    ctx.container().w(width).border(Border::Rounded).col(|ctx| {
        ctx.row(|ctx| {
            for (lane, label) in [("Open", "Inbox"), ("Running", "Active"), ("Pins", "Pins")] {
                if quiet_button(ctx, label, view.lane == lane, tailwind::VIOLET.c300) {
                    actions.push(Action::Lane(lane.into()));
                }
            }
        });
        ctx.row(|ctx| {
            if quiet_button(ctx, "Scope", false, tailwind::VIOLET.c300) {
                actions.push(Action::Command("project".into()));
            }
            if quiet_button(ctx, "Env", false, tailwind::VIOLET.c300) {
                actions.push(Action::Command("env".into()));
            }
            if quiet_button(ctx, "Settled", false, tailwind::VIOLET.c300) {
                actions.push(Action::Lane("Settled".into()));
            }
        });
        let entries = entries(view, state);
        let keys: Vec<_> = entries.iter().map(|entry| entry_key(entry, view)).collect();
        let layout_changed = keys != state.navigation.keys;
        let previous_key = state
            .navigation
            .list
            .selected_index()
            .and_then(|index| state.navigation.keys.get(index))
            .cloned();
        if layout_changed {
            state.navigation.list.set_items(keys.clone());
            state.navigation.list.set_item_heights(
                entries
                    .iter()
                    .map(|entry| {
                        if matches!(entry, Entry::Project { .. }) {
                            1
                        } else {
                            2
                        }
                    })
                    .collect(),
            );
            state.navigation.keys = keys;
            let preserved =
                previous_key
                    .as_ref()
                    .and_then(|key| state.navigation.keys.iter().position(|item| item == key))
                    .or_else(|| {
                        view.rows
                            .get(view.selected)
                            .and_then(|row| state.thread_sources.get(&row.key))
                            .and_then(|source| {
                                state.navigation.keys.iter().position(|key| {
                                    key == &format!("project:{}", source.project_key())
                                })
                            })
                    });
            if let Some(index) = preserved {
                state.navigation.list.set_selected_index(index);
            }
        }
        let external = view.rows.get(view.selected).map(|row| row.key.clone());
        if state.navigation.observed_selection != external {
            if let Some(index) = external.as_ref().and_then(|key| {
                entries.iter().position(
                    |entry| matches!(entry, Entry::Thread(index) if view.rows[*index].key == *key),
                )
            }) {
                state.navigation.list.set_selected_index(index);
            }
            state.navigation.observed_selection = external;
        }
        let clicked = Cell::new(None);
        let selected = state.navigation.list.selected_index();
        let collapsed = &state.navigation.collapsed;
        ctx.register_focusable_named("navigator");
        let response = ctx.virtual_list_variable(
            &mut state.navigation.list,
            height.saturating_sub(9).max(2),
            |ctx, entry_index| match &entries[entry_index] {
                Entry::Project { key, title, count } => {
                    ctx.register_focusable_named(&format!("project:{key}"));
                    if quiet_button(
                        ctx,
                        &format!(
                            "{} {title}  {count}",
                            if collapsed.contains(key) {
                                "▸"
                            } else {
                                "▾"
                            }
                        ),
                        true,
                        identity_color(key),
                    ) && !layout_changed
                    {
                        clicked.set(Some(entry_index));
                    }
                }
                Entry::Thread(index) => {
                    let row = &view.rows[*index];
                    let attention = matches!(
                        row.status.as_str(),
                        "failed" | "needs_input" | "needs-input"
                    );
                    let accent = if attention {
                        tailwind::AMBER.c300
                    } else {
                        tailwind::VIOLET.c300
                    };
                    let marker = if selected == Some(entry_index) {
                        "▸"
                    } else if row.pinned {
                        "◆"
                    } else if row.section == "Unread" {
                        "●"
                    } else {
                        " "
                    };
                    ctx.row(|ctx| {
                        ctx.text(marker).fg(accent);
                        ctx.register_focusable_named(&format!("thread:{}", row.key));
                        if quiet_button(
                            ctx,
                            &row.title,
                            selected == Some(entry_index) || attention,
                            accent,
                        ) && !layout_changed
                        {
                            clicked.set(Some(entry_index));
                        }
                    });
                    ctx.line(|ctx| {
                        ctx.text("  ");
                        if let Some(source) = state.thread_sources.get(&row.key) {
                            ctx.text(&source.environment_label)
                                .fg(identity_color(&source.environment));
                        } else {
                            ctx.text(
                                row.subtitle
                                    .split_once(" / ")
                                    .map_or(row.subtitle.as_str(), |(env, _)| env),
                            )
                            .dim();
                        }
                        let status = match row.status.as_str() {
                            "needs_input" | "needs-input" => "needs you",
                            "running" | "starting" | "preparing" => "running",
                            "failed" => "failed",
                            _ if row.section == "Completed" => "read",
                            _ => "",
                        };
                        if !status.is_empty() {
                            ctx.text(format!(" · {status}")).dim();
                        }
                    });
                }
            },
        );
        if response.changed
            && let Some(index) = state.navigation.list.selected_index()
            && let Entry::Thread(source_index) = entries[index]
        {
            state.navigation.observed_selection = Some(view.rows[source_index].key.clone());
            actions.push(Action::Highlight(source_index));
        }
        let activated = clicked.get().or_else(|| {
            (!layout_changed && response.focused && ctx.consume_key_code(KeyCode::Enter))
                .then(|| state.navigation.list.selected_index())
                .flatten()
        });
        if let Some(index) = activated {
            match &entries[index] {
                Entry::Project { key, .. } => {
                    toggle_project(&mut state.navigation, key);
                    ctx.request_frame();
                }
                Entry::Thread(source_index) => actions.push(Action::Open(*source_index)),
            }
        }
        if view.rows.is_empty() {
            ctx.text("No conversations").dim();
        }
    });
}

pub(super) fn draw_attention(
    ctx: &mut Context,
    state: &mut SurfaceState,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let mut projects: BTreeMap<String, (String, Vec<&AttentionItem>)> = BTreeMap::new();
    for question in &state.questions {
        projects
            .entry(question.source.project_key())
            .or_insert_with(|| (question.source.project.clone(), Vec::new()))
            .1
            .push(question);
    }
    if state.retained_question.as_ref().map(|q| &q.key) != state.selected_question.as_ref() {
        state.retained_question = state
            .selected_question
            .as_deref()
            .and_then(|key| state.questions.iter().find(|q| q.key == key))
            .cloned();
    }
    let visible: Vec<_> = projects
        .iter()
        .filter(|(key, _)| {
            !state
                .navigation
                .collapsed
                .contains(&format!("questions:{key}"))
        })
        .flat_map(|(_, (_, questions))| questions.iter().copied())
        .collect();
    let width = ctx.width();
    let rail_width = if width >= 90 { 28 } else { 22.min(width / 3) };
    let content_rows = height.saturating_sub(11).max(1);
    let content_width = width.saturating_sub(rail_width + 4).max(1);
    ctx.container().w(width).grow(1).row(|ctx| {
        ctx.scrollable(&mut state.navigation.list_scroll)
            .w(rail_width)
            .grow(1)
            .border(Border::Rounded)
            .title("Needs you")
            .col(|ctx| {
                let focused = ctx.register_focusable_named("attention-list");
                ctx.register_focusable();
                let selection = state
                    .selected_question
                    .as_ref()
                    .and_then(|key| visible.iter().position(|question| &question.key == key))
                    .unwrap_or(0);
                let mut next = selection;
                let mut activate = false;
                let mut consumed = Vec::new();
                for (index, key) in ctx.key_presses_when(focused) {
                    if key.modifiers != super::KeyModifiers::NONE {
                        continue;
                    }
                    match key.code {
                        KeyCode::Up => next = next.saturating_sub(1),
                        KeyCode::Down => next = (next + 1).min(visible.len().saturating_sub(1)),
                        KeyCode::Enter => activate = true,
                        _ => continue,
                    }
                    consumed.push(index);
                }
                for index in consumed {
                    ctx.consume_event(index);
                }
                if (next != selection || activate)
                    && let Some(question) = visible.get(next)
                {
                    state.selected_project = Some(question.source.project_key());
                    state.selected_question = Some(question.key.clone());
                    state.retained_question = Some((**question).clone());
                    actions.push(Action::InspectQuestion((**question).clone()));
                }
                for (project, (name, questions)) in &projects {
                    let collapse_key = format!("questions:{project}");
                    let collapsed = state.navigation.collapsed.contains(&collapse_key);
                    ctx.register_focusable_named(&format!("question-project:{project}"));
                    if quiet_button(
                        ctx,
                        &format!(
                            "{} {name}  {}",
                            if collapsed { "▸" } else { "▾" },
                            questions.len()
                        ),
                        true,
                        identity_color(project),
                    ) {
                        toggle_project(&mut state.navigation, &collapse_key);
                    }
                    if collapsed {
                        continue;
                    }
                    for question in questions {
                        ctx.register_focusable_named(&format!("question:{}", question.key));
                        if quiet_button(
                            ctx,
                            &question.title,
                            state.selected_question.as_deref() == Some(&question.key),
                            tailwind::VIOLET.c300,
                        ) {
                            state.selected_project = Some(project.clone());
                            state.selected_question = Some(question.key.clone());
                            state.retained_question = Some((*question).clone());
                            actions.push(Action::InspectQuestion((*question).clone()));
                        }
                        ctx.text(&question.source.environment_label)
                            .fg(identity_color(&question.source.environment))
                            .dim();
                    }
                }
                if state.questions.is_empty() {
                    ctx.text("No questions waiting").dim();
                }
            });
        ctx.container()
            .w(width.saturating_sub(rail_width))
            .grow(1)
            .border(Border::Rounded)
            .p(1)
            .col(|ctx| {
                if let Some(question) = &state.retained_question {
                    let current = state.questions.iter().find(|live| live.key == question.key);
                    let unchanged = current.is_some_and(|live| {
                        live.request == question.request && live.source == question.source
                    });
                    let content_rows = content_rows.saturating_sub(u32::from(!unchanged)).max(1);
                    let geometry = reports::Geometry {
                        width: content_width,
                        height: content_rows,
                    };
                    let displayed = (
                        question.key.clone(),
                        question.request["revision"].to_string(),
                        question.supporting_source_key.clone(),
                        question.supporting_revision.clone(),
                    );
                    let source_changed = state.navigation.displayed.as_ref() != Some(&displayed);
                    if source_changed {
                        state.navigation.content_scroll = ScrollState::default();
                        state.navigation.displayed = Some(displayed);
                    }
                    let current_geometry = (content_width, content_rows);
                    let geometry_changed =
                        state.navigation.content_geometry != Some(current_geometry);
                    state.navigation.content_geometry = Some(current_geometry);
                    ctx.text(&question.title).bold().truncate();
                    if !unchanged {
                        ctx.text(if current.is_some() {
                            "Question changed · select again"
                        } else {
                            "Source unavailable"
                        })
                        .fg(tailwind::AMBER.c300)
                        .truncate();
                    }
                    content_keys(
                        ctx,
                        &mut state.navigation.content_scroll,
                        !source_changed && !geometry_changed,
                        content_rows,
                    );
                    ctx.scrollable(&mut state.navigation.content_scroll)
                        .h(content_rows)
                        .col(|ctx| {
                            if !question.summary.is_empty() && question.summary != question.title {
                                ctx.text(&question.summary).wrap();
                            }
                            if !question.supporting_markdown.is_empty()
                                && question.supporting_markdown.trim() != question.summary.trim()
                            {
                                ctx.separator();
                                if question.supporting_truncated {
                                    ctx.text("Excerpt").dim();
                                }
                                state.reports.draw(
                                    ctx,
                                    &question.supporting_source_key,
                                    &question.supporting_revision,
                                    &question.supporting_markdown,
                                    geometry,
                                );
                            }
                        });
                    source_tags(ctx, &question.source);
                    ctx.row(|ctx| {
                        if ctx
                            .disabled(!unchanged, |ctx| {
                                ctx.button("Answer").variant(ButtonVariant::Primary).show()
                            })
                            .clicked
                        {
                            actions.push(Action::Answer(question.clone()));
                        }
                        if quiet_button(ctx, "Conversation", false, tailwind::VIOLET.c300) {
                            actions.push(Action::OpenSource(question.source.clone()));
                            state.attention_mode = false;
                        }
                    });
                } else {
                    ctx.text("Choose a question").dim();
                }
            });
    });
}

fn source_tags(ctx: &mut Context, source: &Source) {
    ctx.line(|ctx| {
        ctx.text(if source.environment_label.is_empty() {
            &source.environment
        } else {
            &source.environment_label
        })
        .max_w(16)
        .truncate()
        .fg(identity_color(&source.environment));
        ctx.text(" · ").dim();
        ctx.text(&source.project)
            .fg(identity_color(&source.project_key()));
    });
}

fn content_keys(ctx: &mut Context, scroll: &mut ScrollState, current: bool, page: u32) {
    let focused = ctx.register_focusable_named("attention-content");
    ctx.register_focusable();
    scroll.follow_focus = false;
    if !focused || !current || scroll.viewport_height() == 0 {
        return;
    }
    let mut consumed = Vec::new();
    for (index, key) in ctx.key_presses_when(focused) {
        if key.modifiers != super::KeyModifiers::NONE {
            continue;
        }
        match key.code {
            KeyCode::Up => scroll.scroll_up(1),
            KeyCode::Down => scroll.scroll_down(1),
            KeyCode::PageUp => scroll.scroll_up(page.saturating_sub(1).max(1) as usize),
            KeyCode::PageDown => scroll.scroll_down(page.saturating_sub(1).max(1) as usize),
            KeyCode::Home => scroll.offset = 0,
            KeyCode::End => scroll.offset = scroll.max_offset(),
            _ => continue,
        }
        consumed.push(index);
    }
    for index in consumed {
        ctx.consume_event(index);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn collapse_preserves_source_indices_and_distinct_environment_projects() {
        let mut state = SurfaceState::new();
        let view = ui::View {
            rows: vec![
                ui::ThreadView {
                    key: "0\0first".into(),
                    title: "First".into(),
                    ..Default::default()
                },
                ui::ThreadView {
                    key: "1\0same".into(),
                    title: "Remote".into(),
                    ..Default::default()
                },
                ui::ThreadView {
                    key: "0\0second".into(),
                    title: "Second".into(),
                    ..Default::default()
                },
            ],
            ..Default::default()
        };
        for (key, env) in [("0\0first", "a"), ("1\0same", "b"), ("0\0second", "a")] {
            state.thread_sources.insert(
                key.into(),
                Source {
                    environment: env.into(),
                    project_id: "same".into(),
                    project: "Project".into(),
                    ..Default::default()
                },
            );
        }
        let before = entries(&view, &state);
        assert_eq!(
            before
                .iter()
                .filter(|entry| matches!(entry, Entry::Project { .. }))
                .count(),
            2
        );
        assert_eq!(
            before
                .iter()
                .filter_map(|entry| if let Entry::Thread(index) = entry {
                    Some(*index)
                } else {
                    None
                })
                .collect::<Vec<_>>(),
            vec![0, 2, 1]
        );
        toggle_project(&mut state.navigation, "a\0same");
        let collapsed = entries(&view, &state);
        assert_eq!(
            collapsed
                .iter()
                .filter_map(|entry| if let Entry::Thread(index) = entry {
                    Some(*index)
                } else {
                    None
                })
                .collect::<Vec<_>>(),
            vec![1]
        );
        toggle_project(&mut state.navigation, "a\0same");
        assert_eq!(entries(&view, &state), before);
    }
    #[test]
    fn question_reading_resets_by_source_revision_and_owns_only_plain_keys() {
        use slt::{EventBuilder, KeyModifiers, TestBackend};
        let mut backend = TestBackend::new(100, 28);
        let mut state = SurfaceState::new();
        let first = AttentionItem {
            key: "source-a".into(),
            title: "Question".into(),
            summary: (0..60)
                .map(|index| format!("Summary row {index}\n"))
                .collect(),
            supporting_revision: "revision-a".into(),
            source: Source {
                environment: "a".into(),
                project_id: "same".into(),
                project: "Project".into(),
                ..Default::default()
            },
            ..Default::default()
        };
        state.questions = vec![first.clone()];
        state.selected_question = Some(first.key.clone());
        state.retained_question = Some(first.clone());
        for _ in 0..3 {
            backend.render(|ctx| {
                draw_attention(ctx, &mut state, 28, &mut Vec::new());
                ctx.focus_by_name("attention-content");
            });
        }
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::PageDown).build(),
            |ctx| {
                draw_attention(ctx, &mut state, 28, &mut Vec::new());
            },
        );
        let offset = state.navigation.content_scroll.offset;
        assert!(
            offset > 0,
            "focused question content pages through long summaries"
        );
        backend.run_with_events(
            EventBuilder::new()
                .key_with(KeyCode::Home, KeyModifiers::SHIFT)
                .build(),
            |ctx| {
                draw_attention(ctx, &mut state, 28, &mut Vec::new());
            },
        );
        assert_eq!(
            state.navigation.content_scroll.offset, offset,
            "modified keys remain available to their owner"
        );
        let revised = AttentionItem {
            request: serde_json::json!({"revision":"changed-native-offer"}),
            ..first
        };
        state.questions = vec![revised.clone()];
        state.retained_question = Some(revised);
        backend.render(|ctx| {
            draw_attention(ctx, &mut state, 28, &mut Vec::new());
        });
        assert_eq!(
            state.navigation.content_scroll.offset, 0,
            "new content revision starts at the beginning"
        );
        assert!(!state.navigation.content_scroll.follow_focus);
    }
}
