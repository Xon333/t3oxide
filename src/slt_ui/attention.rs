//! Project navigation, source-qualified questions and independently selected content.

use super::{
    Action, AttentionItem, ContentItem, Source, SurfaceState, action_bar, elide, identity_color,
    primary_button, quiet_tabs, secondary_button,
};
use crate::{domain, project_graph, reports, ui};
use slt::{
    Border, Context, KeyCode, ListState, ScrollState, SpinnerState, TabsState, WidgetColors,
    palette::tailwind,
};
use std::collections::BTreeMap;
use unicode_width::UnicodeWidthStr;

#[derive(Clone, Debug, PartialEq, Eq)]
enum Selection {
    Question(String),
    Content(String),
}

/// Local widget state; source data and pending answers remain controller-owned.
#[derive(Debug, Default)]
pub(super) struct State {
    tabs: TabsState,
    list: ListState,
    rows: Vec<Selection>,
    selected: Option<Selection>,
    selected_content: Option<String>,
    retained_content: Option<ContentItem>,
    content_scroll: ScrollState,
    displayed: Option<(String, String)>,
    graph: project_graph::ProjectGraph,
    project_selections: BTreeMap<String, Selection>,
    navigation_open: Option<bool>,
    navigation_cursor: usize,
}

pub(super) fn navigation_open(state: &SurfaceState) -> bool {
    state.attention.navigation_open.is_some()
}

pub(super) fn selected_source(state: &SurfaceState) -> Option<&Source> {
    match state.attention.selected.as_ref()? {
        Selection::Question(key) => state
            .retained_question
            .as_ref()
            .filter(|question| &question.key == key)
            .map(|question| &question.source),
        Selection::Content(_) => selected_content(state).map(|content| &content.source),
    }
}

pub(super) fn selected_content(state: &SurfaceState) -> Option<&ContentItem> {
    let Selection::Content(key) = state.attention.selected.as_ref()? else {
        return None;
    };
    state
        .attention
        .retained_content
        .as_ref()
        .filter(|content| &content.key == key)
        .or_else(|| state.contents.iter().find(|content| &content.key == key))
}

pub(super) fn draw(
    ctx: &mut Context,
    _view: &ui::View,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    if let Some(retained) = &mut state.attention.retained_content
        && let Some(current) = state
            .contents
            .iter()
            .find(|content| content.key == retained.key && content.revision == retained.revision)
    {
        if state.selected_project.as_ref() == Some(&retained.source.project_key()) {
            state.selected_project = Some(current.source.project_key());
        }
        retained.source = current.source.clone();
    }
    initialize_selection(state);
    let width = width.max(1);
    let height = height.max(1);
    if height < 12 || state.sidebar_hidden != Some(false) || navigation_open(state) {
        let in_scope = |source: &Source| {
            state
                .selected_project
                .as_ref()
                .is_none_or(|project| *project == source.project_key())
        };
        let forms = state
            .questions
            .iter()
            .filter(|item| in_scope(&item.source))
            .count();
        let library = state
            .contents
            .iter()
            .filter(|item| in_scope(&item.source))
            .count();
        let strip = if width < 52 { 2 } else { 1 };
        let navigating = navigation_open(state);
        ctx.disabled(navigating, |ctx| {
            ctx.container().w(width).h(height).col(|ctx| {
                ctx.container().h(strip).col(|ctx| {
                    let items = [
                        (
                            format!("Forms {forms}"),
                            state.attention.tabs.selected == 0,
                            true,
                        ),
                        (
                            format!("Library {library}"),
                            state.attention.tabs.selected == 1,
                            true,
                        ),
                        (
                            if state.graph_global {
                                "Projects · All"
                            } else {
                                "Projects"
                            }
                            .into(),
                            false,
                            true,
                        ),
                        (
                            "All projects".into(),
                            state.graph_global,
                            !state.graph_global,
                        ),
                    ];
                    let refs: Vec<_> = items
                        .iter()
                        .map(|(label, selected, enabled)| (label.as_str(), *selected, *enabled))
                        .collect();
                    match action_bar(
                        ctx,
                        "attention-navigation",
                        &mut state.attention.navigation_cursor,
                        &refs,
                    ) {
                        Some(index @ 0..=1) => {
                            state.attention.tabs.selected = index;
                            state.attention.rows.clear();
                            state.attention.navigation_open = Some(false);
                        }
                        Some(2) => state.attention.navigation_open = Some(true),
                        Some(3) => state.graph_global = true,
                        _ => {}
                    }
                });
                draw_canvas(ctx, state, width, height.saturating_sub(strip), actions);
            });
        });
        if let Some(graph) = state.attention.navigation_open {
            ctx.modal(|ctx| {
                let panel_width = ctx.width().saturating_sub(2).max(1);
                let panel_height = ctx.height().saturating_sub(3).max(6);
                if graph {
                    draw_graph(ctx, state, panel_width, panel_height, true, actions);
                } else {
                    draw_library(ctx, state, panel_width, panel_height, actions);
                }
                if secondary_button(ctx, "Close").clicked || ctx.consume_key_code(KeyCode::Esc) {
                    state.attention.navigation_open = None;
                    ctx.focus_by_name("attention-navigation");
                    ctx.request_frame();
                }
            });
        }
    } else if width >= 64 {
        let navigation_width = (width / 3).clamp(30, 48);
        ctx.container().w(width).h(height).gap(1).row(|ctx| {
            ctx.container().w(navigation_width).h(height).col(|ctx| {
                // Keep a complete selected node and return control, without
                // reserving a square canvas for a sparse navigation preview.
                let graph_height = (height / 3).clamp(6, 10).min(height.saturating_sub(5));
                draw_library(
                    ctx,
                    state,
                    navigation_width,
                    height.saturating_sub(graph_height),
                    actions,
                );
                draw_graph(ctx, state, navigation_width, graph_height, false, actions);
            });
            draw_canvas(
                ctx,
                state,
                width.saturating_sub(navigation_width + 1),
                height,
                actions,
            );
        });
    } else {
        // Keep both navigation and the canvas available in compact terminals.
        let navigation_height = (height / 3).clamp(5, 9).min(height.saturating_sub(5));
        let graph_width = (width / 2).clamp(16, 28).min(width / 2);
        ctx.container().w(width).h(height).col(|ctx| {
            ctx.container()
                .w(width)
                .h(navigation_height)
                .gap(1)
                .row(|ctx| {
                    draw_graph(ctx, state, graph_width, navigation_height, true, actions);
                    draw_library(
                        ctx,
                        state,
                        width.saturating_sub(graph_width + 1),
                        navigation_height,
                        actions,
                    );
                });
            draw_canvas(
                ctx,
                state,
                width,
                height.saturating_sub(navigation_height),
                actions,
            );
        });
    }
}

fn initialize_selection(state: &mut SurfaceState) {
    if state.attention.selected.is_some() {
        return;
    }
    if let Some(key) = &state.selected_question
        && state
            .retained_question
            .as_ref()
            .is_some_and(|question| &question.key == key)
    {
        state.attention.selected = Some(Selection::Question(key.clone()));
    } else if let Some(question) = state.questions.iter().find(|question| {
        state
            .selected_project
            .as_ref()
            .is_none_or(|project| question.source.project_key() == *project)
    }) {
        state.attention.selected = Some(Selection::Question(question.key.clone()));
        state.selected_question = Some(question.key.clone());
        state.retained_question = Some(question.clone());
    } else if let Some(content) = state.contents.iter().find(|content| {
        state
            .selected_project
            .as_ref()
            .is_none_or(|project| content.source.project_key() == *project)
    }) {
        state.attention.selected = Some(Selection::Content(content.key.clone()));
        state.attention.selected_content = Some(content.key.clone());
        state.attention.retained_content = Some(content.clone());
        state.attention.tabs.selected = 1;
    }
}

fn draw_library(
    ctx: &mut Context,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let accent = ctx.theme().primary;
    let scope = state
        .selected_project
        .as_ref()
        .map_or("All projects", |project| {
            state
                .projects
                .iter()
                .chain(state.questions.iter().map(|item| &item.source))
                .chain(state.contents.iter().map(|item| &item.source))
                .find(|source| source.project_key() == *project)
                .map_or("Project", |source| source.project.as_str())
        })
        .to_owned();
    ctx.group("t3oxide.library")
        .w(width)
        .h(height)
        .border(Border::Rounded)
        .title(elide(
            &format!("Browse · {scope}"),
            width.saturating_sub(4) as usize,
        ))
        .focus_within_border_fg(accent)
        .col(|ctx| {
            let in_scope = |source: &Source| {
                state
                    .selected_project
                    .as_ref()
                    .is_none_or(|project| *project == source.project_key())
            };
            let forms = state
                .questions
                .iter()
                .filter(|item| in_scope(&item.source))
                .count();
            let contents = state
                .contents
                .iter()
                .filter(|item| in_scope(&item.source))
                .count();
            state.attention.tabs.labels =
                vec![format!("Forms {forms}"), format!("Library {contents}")];
            let mut changed_lane = false;
            let scoped = state.selected_project.is_some();
            ctx.row(|ctx| {
                let focused = ctx.register_focusable_named("attention-tabs");
                let tabs_width: usize = state
                    .attention
                    .tabs
                    .labels
                    .iter()
                    .map(|label| label.width() + 4)
                    .sum::<usize>()
                    + 1;
                if tabs_width > width.saturating_sub(2) as usize {
                    let keyboard = focused
                        && (ctx.consume_key_code(KeyCode::Left)
                            || ctx.consume_key_code(KeyCode::Right)
                            || ctx.consume_key_code(KeyCode::Enter)
                            || ctx.consume_key(' '));
                    let mut active = TabsState::new(vec![format!(
                        "{} ›",
                        state.attention.tabs.labels[state.attention.tabs.selected]
                    )]);
                    let response = quiet_tabs(ctx, &mut active);
                    changed_lane = response.clicked || keyboard;
                    if changed_lane {
                        state.attention.tabs.selected = 1 - state.attention.tabs.selected;
                        ctx.focus_by_name("attention-tabs");
                        ctx.request_frame();
                    }
                } else {
                    changed_lane = quiet_tabs(ctx, &mut state.attention.tabs).changed;
                }
                if width >= 40 {
                    ctx.spacer();
                    if ctx
                        .disabled(!scoped, |ctx| secondary_button(ctx, "All"))
                        .clicked
                    {
                        state.selected_project = None;
                        ctx.request_frame();
                    }
                }
            });
            if width < 40
                && height >= 7
                && ctx
                    .disabled(!scoped, |ctx| secondary_button(ctx, "All"))
                    .clicked
            {
                state.selected_project = None;
                ctx.request_frame();
            }
            if changed_lane {
                state.attention.rows.clear();
            }
            let mut rows = Vec::new();
            let mut labels = Vec::new();
            if state.attention.tabs.selected == 0 {
                let mut groups = BTreeMap::<String, Vec<&AttentionItem>>::new();
                for question in &state.questions {
                    let project = question.source.project_key();
                    if state
                        .selected_project
                        .as_ref()
                        .is_some_and(|selected| selected != &project)
                    {
                        continue;
                    }
                    groups.entry(project).or_default().push(question);
                }
                for questions in groups.values() {
                    for (index, question) in questions.iter().enumerate() {
                        labels.push(elide(
                            &if index == 0 && state.selected_project.is_none() {
                                format!("{} · {}", question.source.project, question.title)
                            } else {
                                question.title.clone()
                            },
                            width.saturating_sub(4) as usize,
                        ));
                        rows.push(Selection::Question(question.key.clone()));
                    }
                }
            } else {
                for content in &state.contents {
                    if state
                        .selected_project
                        .as_ref()
                        .is_some_and(|selected| selected != &content.source.project_key())
                    {
                        continue;
                    }
                    labels.push(elide(
                        &content_label(content),
                        width.saturating_sub(4) as usize,
                    ));
                    rows.push(Selection::Content(content.key.clone()));
                }
            }
            let layout_changed = rows != state.attention.rows;
            if layout_changed || labels != state.attention.list.items() {
                state.attention.list.set_items(labels);
                state.attention.rows.clone_from(&rows);
                let selection = if state.attention.tabs.selected == 1 {
                    state
                        .attention
                        .selected_content
                        .as_ref()
                        .map(|key| Selection::Content(key.clone()))
                } else {
                    state
                        .selected_question
                        .as_ref()
                        .map(|key| Selection::Question(key.clone()))
                };
                if let Some(index) = rows.iter().position(|row| Some(row) == selection.as_ref()) {
                    state.attention.list.set_selected_index(index);
                }
            }
            if rows.is_empty() {
                ctx.text(if state.attention.tabs.selected == 0 {
                    "No pending forms"
                } else {
                    "No loaded content"
                })
                .dim()
                .w(width.saturating_sub(2))
                .wrap();
                return;
            }
            if changed_lane
                && let Some(selection) = state
                    .attention
                    .list
                    .selected_index()
                    .and_then(|index| rows.get(index))
                    .cloned()
            {
                select(state, selection, false, actions);
            }
            let focused = ctx.register_focusable_named("attention-list");
            let owns_detail = state
                .attention
                .list
                .selected_index()
                .and_then(|index| rows.get(index))
                == state.attention.selected.as_ref();
            // A remaining queue row is a browsing cursor, not the retained form.
            // Keyboard focus remains a cursor even when the old form left the queue.
            let colors = if owns_detail {
                WidgetColors::new()
            } else {
                WidgetColors::new()
                    .accent(ctx.theme().surface)
                    .fg(if focused {
                        ctx.theme().primary
                    } else {
                        ctx.theme().text_dim
                    })
            };
            let response = ctx.list_colored(&mut state.attention.list, &colors);
            let activate = !layout_changed
                && (response.clicked || (response.focused && ctx.consume_key_code(KeyCode::Enter)));
            if (response.changed || activate)
                && let Some(row) = state
                    .attention
                    .list
                    .selected_index()
                    .and_then(|index| rows.get(index))
            {
                // Cursor movement selects content locally; only a form activation asks
                // the controller to re-read its live source before answering.
                let selection = row.clone();
                select(state, selection.clone(), false, actions);
                if activate {
                    if let Selection::Question(key) = selection
                        && let Some(question) = state.questions.iter().find(|item| item.key == key)
                        && !matches!(
                            question_delivery(state, question),
                            Some("submitting" | "uncertain" | "acknowledged")
                        )
                    {
                        actions.push(Action::Answer(question.clone()));
                    }
                    if state.attention.navigation_open.take().is_some() {
                        ctx.focus_by_name("attention-navigation");
                    }
                }
                ctx.request_frame();
            }
        });
}

fn select(
    state: &mut SurfaceState,
    selection: Selection,
    activate: bool,
    actions: &mut Vec<Action>,
) {
    match &selection {
        Selection::Question(key) => {
            if let Some(question) = state.questions.iter().find(|question| &question.key == key) {
                if state.loading_question.as_ref() != Some(key) {
                    state.loading_question = None;
                }
                state.selected_project = Some(question.source.project_key());
                state.selected_question = Some(key.clone());
                state.retained_question = Some(question.clone());
                if activate {
                    actions.push(Action::InspectQuestion(question.clone()));
                }
            }
            state.attention.tabs.selected = 0;
        }
        Selection::Content(key) => {
            if let Some(content) = state.contents.iter().find(|content| &content.key == key) {
                state.selected_project = Some(content.source.project_key());
                state.attention.selected_content = Some(key.clone());
                state.attention.retained_content = Some(content.clone());
            }
            state.attention.tabs.selected = 1;
        }
    }
    if let Some(project) = state.selected_project.as_ref() {
        state
            .attention
            .project_selections
            .insert(project.clone(), selection.clone());
    }
    state.attention.selected = Some(selection);
}

fn select_project(state: &mut SurfaceState, project: String, actions: &mut Vec<Action>) {
    if let Some(source) = selected_source(state) {
        let key = source.project_key();
        if let Some(selection) = state.attention.selected.as_ref() {
            state
                .attention
                .project_selections
                .insert(key, selection.clone());
        }
    }
    let selection = state
        .attention
        .project_selections
        .get(&project)
        .cloned()
        .or_else(|| {
            state
                .questions
                .iter()
                .find(|question| question.source.project_key() == project)
                .map(|question| Selection::Question(question.key.clone()))
        })
        .or_else(|| {
            state
                .contents
                .iter()
                .find(|content| content.source.project_key() == project)
                .map(|content| Selection::Content(content.key.clone()))
        });
    state.selected_project = Some(project);
    state.graph_global = false;
    if let Some(selection) = selection {
        select(state, selection, true, actions);
    }
}

fn draw_canvas(
    ctx: &mut Context,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let accent = ctx.theme().primary;
    let title = match state.attention.selected.as_ref() {
        Some(Selection::Question(_))
            if state.retained_question.as_ref().is_some_and(|question| {
                state
                    .selected_project
                    .as_ref()
                    .is_some_and(|project| *project != question.source.project_key())
            }) =>
        {
            "Form · Retained"
        }
        Some(Selection::Question(_)) => "Form",
        Some(Selection::Content(_)) => selected_content(state)
            .map(|content| kind_label(&content.kind))
            .unwrap_or("Content"),
        None => "Content",
    };
    ctx.container()
        .w(width)
        .h(height)
        .border(Border::Rounded)
        .title(title)
        .focus_within_border_fg(accent)
        .px(1)
        .col(|ctx| {
            let inner_width = width.saturating_sub(4).max(1);
            let selection = state.attention.selected.clone();
            match selection {
                Some(Selection::Question(key)) => {
                    if let Some(question) = state.retained_question.take() {
                        if question.key == key {
                            draw_question(
                                ctx,
                                state,
                                &question,
                                inner_width,
                                height.saturating_sub(2),
                                actions,
                            );
                        } else {
                            ctx.text("Form unavailable").fg(tailwind::AMBER.c300);
                        }
                        state.retained_question = Some(question);
                    } else {
                        ctx.text("Form unavailable").fg(tailwind::AMBER.c300);
                    }
                }
                Some(Selection::Content(key)) => {
                    let retained = state.attention.retained_content.take();
                    if let Some(content) = retained.filter(|content| content.key == key) {
                        let current = state.contents.iter().find(|live| live.key == key);
                        let unchanged = current == Some(&content);
                        if !has_authored_title(&content) {
                            ctx.text(&content.title).bold().w(inner_width).wrap();
                        }
                        if state
                            .selected_project
                            .as_ref()
                            .is_some_and(|project| *project != content.source.project_key())
                        {
                            ctx.text("Retained").dim();
                        }
                        source_tags(ctx, &content.source, inner_width);
                        ctx.row(|ctx| {
                            ctx.spacer();
                            if secondary_button(ctx, "Open in A").clicked {
                                actions.push(Action::OpenSource(content.source.clone()));
                                state.attention_mode = false;
                            }
                        });
                        if !unchanged {
                            ctx.text(if current.is_some() {
                                "Source changed · select again"
                            } else {
                                "Source unavailable"
                            })
                            .fg(tailwind::AMBER.c300)
                            .w(inner_width)
                            .wrap();
                        }
                        let displayed = (content.key.clone(), content.revision.clone());
                        if state.attention.displayed.as_ref() != Some(&displayed) {
                            state.attention.content_scroll = ScrollState::default();
                            state.attention.displayed = Some(displayed);
                        }
                        content_keys(
                            ctx,
                            &mut state.attention.content_scroll,
                            height.saturating_sub(5),
                        );
                        ctx.scrollable(&mut state.attention.content_scroll)
                            .grow(1)
                            .w(inner_width)
                            .col(|ctx| {
                                if content.truncated {
                                    ctx.text("Excerpt").dim();
                                }
                                state.reports.draw(
                                    ctx,
                                    &content.key,
                                    &content.revision,
                                    &content.markdown,
                                    reports::Geometry {
                                        width: inner_width,
                                        height: height.saturating_sub(5).max(1),
                                    },
                                );
                            });
                        state.attention.retained_content = Some(content);
                    } else {
                        ctx.text("Content unavailable").fg(tailwind::AMBER.c300);
                    }
                }
                None => {
                    ctx.text(if state.selected_project.is_some() {
                        "No loaded content"
                    } else {
                        "Select a project"
                    })
                    .dim();
                }
            }
        });
}

fn draw_question(
    ctx: &mut Context,
    state: &mut SurfaceState,
    question: &AttentionItem,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let current = state.questions.iter().find(|live| live.key == question.key);
    let unchanged = current
        .is_some_and(|live| live.request == question.request && live.source == question.source);
    let retained_delivery = question_delivery(state, question).map(str::to_owned);
    let delivery = retained_delivery.as_deref();
    let answer_blocked =
        !unchanged || matches!(delivery, Some("submitting" | "uncertain" | "acknowledged"));
    let compact = height < 6 || width < 40;
    if !compact || question.summary.is_empty() {
        ctx.text(&question.title).bold().w(width).truncate();
    }
    if !compact && let Some(delivery) = delivery {
        ctx.row(|ctx| delivery_feedback(ctx, delivery, &state.spinner, actions));
    }
    if delivery.is_none() && !unchanged {
        ctx.text(if current.is_some() {
            "Source changed · reload form"
        } else {
            "Source unavailable"
        })
        .fg(tailwind::AMBER.c300)
        .w(width)
        .wrap();
    }
    let displayed = (question.key.clone(), question.supporting_revision.clone());
    if state.attention.displayed.as_ref() != Some(&displayed) {
        state.attention.content_scroll = ScrollState::default();
        state.attention.displayed = Some(displayed);
    }
    let content_rows = height
        .saturating_sub(
            2 + u32::from(!compact || question.summary.is_empty())
                + u32::from((delivery.is_none() && !unchanged) || (delivery.is_some() && !compact)),
        )
        .max(1);
    content_keys(ctx, &mut state.attention.content_scroll, content_rows);
    ctx.scrollable(&mut state.attention.content_scroll)
        .max_h(content_rows)
        .w(width)
        .col(|ctx| {
            if !question.summary.is_empty() && (compact || question.summary != question.title) {
                ctx.text(&question.summary).w(width).wrap();
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
                    reports::Geometry {
                        width,
                        height: content_rows,
                    },
                );
            }
        });
    if !compact
        && state
            .selected_project
            .as_ref()
            .is_some_and(|project| *project != question.source.project_key())
    {
        ctx.text("Retained").dim();
    }
    source_tags(ctx, &question.source, width);
    ctx.container().gap(1).row(|ctx| {
        if compact && let Some(delivery) = delivery {
            delivery_feedback(ctx, delivery, &state.spinner, actions);
        }
        if !compact || delivery.is_none() || delivery == Some("rejected") {
            ctx.register_focusable_named("form-review");
            if primary_button(
                ctx,
                if compact { "Open form" } else { "Answer" },
                !answer_blocked,
            )
            .clicked
            {
                actions.push(Action::Answer(question.clone()));
            }
        }
        ctx.register_focusable_named("form-source");
        if secondary_button(ctx, "Open in A").clicked {
            actions.push(Action::OpenSource(question.source.clone()));
            state.attention_mode = false;
        }
    });
}

fn question_delivery<'a>(state: &'a SurfaceState, question: &AttentionItem) -> Option<&'a str> {
    crate::attention::Source::from_request(
        &question.source.environment,
        &question.source.thread,
        &question.request,
    )
    .and_then(|source| source.key())
    .ok()
    .and_then(|key| state.answer_delivery.get(&key))
    .map(String::as_str)
    .filter(|phase| {
        matches!(
            *phase,
            "submitting" | "uncertain" | "acknowledged" | "rejected"
        )
    })
}

fn delivery_feedback(
    ctx: &mut Context,
    phase: &str,
    spinner: &SpinnerState,
    actions: &mut Vec<Action>,
) {
    match phase {
        "submitting" => {
            ctx.spinner(spinner);
            ctx.text("Submitting…");
        }
        "uncertain" => {
            ctx.text("Unconfirmed").fg(tailwind::AMBER.c300);
            if secondary_button(ctx, "Recovery").clicked {
                actions.push(Action::Command("recovery".into()));
            }
        }
        "acknowledged" => {
            ctx.text("Submitted").fg(tailwind::GREEN.c300);
        }
        "rejected" => {
            ctx.text("Not sent").fg(tailwind::RED.c300);
        }
        _ => {}
    }
}

fn draw_graph(
    ctx: &mut Context,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    compact: bool,
    actions: &mut Vec<Action>,
) {
    // In the compact navigation row the Library already owns this leaf.
    // Keep its parent and every other source reachable in the graph.
    let duplicate_content = selected_content(state)
        .filter(|content| {
            compact
                && state.attention.tabs.selected == 1
                && state.selected_project.as_ref() == Some(&content.source.project_key())
        })
        .map(|content| content.key.clone());
    let mut sources = BTreeMap::new();
    for source in state
        .projects
        .iter()
        .chain(state.questions.iter().map(|question| &question.source))
        .chain(state.contents.iter().map(|content| &content.source))
        .filter(|source| {
            domain::valid_id(&source.environment) && domain::valid_id(&source.project_id)
        })
    {
        sources
            .entry(source.project_key())
            .or_insert_with(|| source.clone());
    }
    let accent = ctx.theme().primary;
    let project_label = state
        .selected_project
        .as_ref()
        .and_then(|key| sources.get(key))
        .map(|source| source.project.as_str());
    let title = if state.graph_global {
        "Projects · All"
    } else {
        project_label.unwrap_or("Projects")
    };
    let show_return = !state.graph_global;
    let region = ctx
        .group("t3oxide.projects")
        .w(width)
        .h(height)
        .focus_within_border_fg(accent);
    let region = if compact {
        region
            .border(Border::Rounded)
            .title(elide(title, width.saturating_sub(4) as usize))
    } else {
        region
    };
    region.col(|ctx| {
        if !compact && !show_return {
            ctx.text(title).fg(accent);
        }
        if show_return {
            ctx.container().h(1).row(|ctx| {
                if secondary_button(ctx, "All projects").clicked {
                    state.graph_global = true;
                }
            });
        }
        let mut nodes = Vec::new();
        let mut edges = Vec::new();
        for (key, source) in &sources {
            if !state.graph_global && state.selected_project.as_ref() != Some(key) {
                continue;
            }
            let id = project_node(source);
            nodes.push(project_graph::Node {
                id: id.clone(),
                label: source.project.clone(),
                color: identity_color(key),
            });
            if !state.graph_global {
                for question in state
                    .questions
                    .iter()
                    .filter(|question| question.source.project_key() == *key)
                {
                    let question_id = question_node(question);
                    nodes.push(project_graph::Node {
                        id: question_id.clone(),
                        label: question.title.clone(),
                        color: tailwind::PURPLE.c300,
                    });
                    edges.push(project_graph::Edge {
                        from: id.clone(),
                        to: question_id,
                    });
                }
                for content in state.contents.iter().filter(|content| {
                    content.source.project_key() == *key
                        && Some(&content.key) != duplicate_content.as_ref()
                }) {
                    let content_id = content_node(content);
                    nodes.push(project_graph::Node {
                        id: content_id.clone(),
                        label: content.title.clone(),
                        color: identity_color(key),
                    });
                    edges.push(project_graph::Edge {
                        from: id.clone(),
                        to: content_id,
                    });
                }
            }
        }
        let selected = if state.graph_global {
            state
                .selected_project
                .as_ref()
                .and_then(|key| sources.get(key))
                .map(project_node)
        } else {
            match state.attention.selected.as_ref() {
                Some(Selection::Question(key)) => state
                    .questions
                    .iter()
                    .find(|question| &question.key == key)
                    .map(question_node),
                Some(Selection::Content(key)) => state
                    .contents
                    .iter()
                    .find(|content| &content.key == key)
                    .map(content_node),
                None => None,
            }
        };
        match state.attention.graph.draw(
            ctx,
            &nodes,
            &edges,
            selected.as_ref(),
            width.saturating_sub(if compact { 2 } else { 0 }),
            height.saturating_sub(if compact {
                2 + u32::from(show_return)
            } else {
                1
            }),
        ) {
            Ok(response) => {
                if let Some(activated) = response.activated {
                    match activated {
                        project_graph::NodeId::Project {
                            environment_id,
                            project_id,
                        } => {
                            select_project(
                                state,
                                format!("{environment_id}\0{project_id}"),
                                actions,
                            );
                        }
                        id @ project_graph::NodeId::Question { .. } => {
                            if let Some(question) = state
                                .questions
                                .iter()
                                .find(|question| question_node(question) == id)
                            {
                                let key = question.key.clone();
                                select(state, Selection::Question(key), true, actions);
                            }
                        }
                        id @ project_graph::NodeId::Content { .. } => {
                            if let Some(content) = state
                                .contents
                                .iter()
                                .find(|content| content_node(content) == id)
                            {
                                let key = content.key.clone();
                                select(state, Selection::Content(key), false, actions);
                            }
                        }
                    }
                    ctx.request_frame();
                }
            }
            Err(error) => {
                ctx.text(error.to_string())
                    .fg(tailwind::AMBER.c300)
                    .w(width.saturating_sub(2))
                    .wrap();
            }
        }
        if ctx.focused_name() == Some("project-navigation")
            && ctx.consume_key_code(KeyCode::Backspace)
        {
            state.graph_global = true;
            ctx.request_frame();
        }
    });
}

fn content_keys(ctx: &mut Context, scroll: &mut ScrollState, page: u32) {
    let focused = ctx.register_focusable_named("attention-content");
    // A custom region consumes its own named focus slot rather than lending it
    // to the first link or diagram control rendered in its body.
    ctx.register_focusable();
    scroll.follow_focus = false;
    if focused {
        let page = page.saturating_sub(1).max(1) as usize;
        if ctx.consume_key_code(KeyCode::Up) {
            scroll.scroll_up(1);
        }
        if ctx.consume_key_code(KeyCode::Down) {
            scroll.scroll_down(1);
        }
        if ctx.consume_key_code(KeyCode::PageUp) {
            scroll.scroll_up(page);
        }
        if ctx.consume_key_code(KeyCode::PageDown) {
            scroll.scroll_down(page);
        }
        if ctx.consume_key_code(KeyCode::Home) {
            scroll.offset = 0;
        }
        if ctx.consume_key_code(KeyCode::End) {
            scroll.offset = scroll.max_offset();
        }
    }
}

fn kind_label(kind: &str) -> &str {
    match kind {
        "research" => "Research",
        "guide" => "Guide",
        "receipt" | "completion_receipt" => "Receipt",
        "report" => "Report",
        _ => "Explanation",
    }
}

fn content_label(content: &ContentItem) -> String {
    let kind = kind_label(&content.kind);
    let title = content
        .title
        .split_once(':')
        .filter(|(prefix, _)| prefix.trim().eq_ignore_ascii_case(kind))
        .map_or(content.title.as_str(), |(_, title)| title.trim_start());
    format!("{kind} · {title}")
}

fn source_tags(ctx: &mut Context, source: &Source, width: u32) {
    let environment = if source.environment_label.is_empty() {
        "Environment"
    } else {
        &source.environment_label
    };
    let project = if source.project.is_empty() {
        "Project"
    } else {
        &source.project
    };
    let environment_width = (environment.width() as u32)
        .min(16)
        .min(width.saturating_sub(4) / 2);
    ctx.container().w(width).line(|ctx| {
        ctx.text(environment)
            .w(environment_width)
            .truncate()
            .fg(identity_color(&source.environment));
        ctx.text(" · ").dim();
        ctx.text(project)
            .w(width.saturating_sub(environment_width + 3))
            .truncate()
            .fg(identity_color(&source.project_key()));
    });
}

fn has_authored_title(content: &ContentItem) -> bool {
    let Some(first) = content
        .markdown
        .lines()
        .find(|line| !line.trim().is_empty())
    else {
        return false;
    };
    let heading = first.trim_start_matches('#');
    heading.len() != first.len()
        && heading.starts_with(' ')
        && heading.trim() == content.title.trim()
}

fn project_node(source: &Source) -> project_graph::NodeId {
    project_graph::NodeId::Project {
        environment_id: source.environment.clone(),
        project_id: source.project_id.clone(),
    }
}

fn question_node(question: &AttentionItem) -> project_graph::NodeId {
    project_graph::NodeId::Question {
        environment_id: question.source.environment.clone(),
        project_id: question.source.project_id.clone(),
        thread_id: question.source.thread.clone(),
        request_id: domain::string(&question.request, "id"),
    }
}

fn content_node(content: &ContentItem) -> project_graph::NodeId {
    project_graph::NodeId::Content {
        environment_id: content.source.environment.clone(),
        project_id: content.source.project_id.clone(),
        thread_id: content.source.thread.clone(),
        source_key: content.key.clone(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use slt::{EventBuilder, TestBackend};

    fn explorer_state() -> SurfaceState {
        let mut state = SurfaceState::new();
        state.applied_layout = Some(ui::LayoutMode::Split);
        state.sidebar_hidden = Some(false);
        state
    }

    fn source(project: &str) -> Source {
        Source {
            environment: "environment-a".into(),
            environment_label: "Build".into(),
            project_id: project.into(),
            project: project.into(),
            thread: format!("thread-{project}"),
            ..Source::default()
        }
    }

    fn content(project: &str, key: &str, kind: &str, title: &str, markdown: &str) -> ContentItem {
        ContentItem {
            key: format!("environment-a\0thread-{project}\0{key}"),
            source: source(project),
            title: title.into(),
            kind: kind.into(),
            markdown: markdown.into(),
            revision: reports::content_revision(markdown),
            truncated: false,
            ..ContentItem::default()
        }
    }

    fn render(backend: &mut TestBackend, state: &mut SurfaceState) -> Vec<Action> {
        let mut actions = Vec::new();
        backend.render(|ctx| {
            draw(
                ctx,
                &ui::View::default(),
                state,
                ctx.width(),
                ctx.height(),
                &mut actions,
            );
        });
        actions
    }

    fn surface_frame(
        backend: &mut super::super::tests::ResizableBackend,
        state: &mut SurfaceState,
        editor: &mut ratatui_textarea::TextArea<'static>,
        events: Vec<slt::Event>,
    ) -> Vec<Action> {
        let mut actions = Vec::new();
        let mut modal = ui::editor("", "violet", "");
        backend.run_with_events(events, |ctx| {
            actions =
                super::super::draw(ctx, &ui::View::default(), editor, &mut modal, None, state);
        });
        actions
    }

    #[test]
    fn focused_form_picker_returns_focus_without_changing_the_attached_draft() {
        for (width, height) in [(40, 12), (70, 22), (140, 38)] {
            let mut state = SurfaceState::new();
            state.active_key = "other-environment/thread".into();
            state.attention_mode = true;
            state.questions = vec![delivery_question()];
            let original = state.questions[0].clone();
            let draft = "Attached A 日本語 draft  ";
            let mut editor = ui::editor(draft, "violet", "");
            let mut modal = ui::editor("", "violet", "");
            let mut backend = TestBackend::new(width, height);
            for _ in 0..3 {
                backend.render(|ctx| {
                    assert!(
                        super::super::draw(
                            ctx,
                            &ui::View::default(),
                            &mut editor,
                            &mut modal,
                            None,
                            &mut state
                        )
                        .is_empty()
                    );
                });
            }
            let caret = (state.composer.cursor_row, state.composer.cursor_col);
            let (x, y) = backend
                .find_text("Forms 1")
                .expect("direct Forms disclosure");
            backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                assert!(
                    super::super::draw(
                        ctx,
                        &ui::View::default(),
                        &mut editor,
                        &mut modal,
                        None,
                        &mut state
                    )
                    .is_empty()
                );
            });
            for _ in 0..3 {
                backend.render(|ctx| {
                    super::super::draw(
                        ctx,
                        &ui::View::default(),
                        &mut editor,
                        &mut modal,
                        None,
                        &mut state,
                    );
                });
            }
            assert!(navigation_open(&state));
            backend.render(|ctx| {
                super::super::draw(
                    ctx,
                    &ui::View::default(),
                    &mut editor,
                    &mut modal,
                    None,
                    &mut state,
                );
                assert!(ctx.focus_by_name("attention-list"));
            });
            backend.render(|ctx| {
                super::super::draw(
                    ctx,
                    &ui::View::default(),
                    &mut editor,
                    &mut modal,
                    None,
                    &mut state,
                );
                assert_eq!(ctx.focused_name(), Some("attention-list"));
            });
            backend.run_with_events(EventBuilder::new().key_code(KeyCode::Esc).build(), |ctx| {
                assert!(
                    super::super::draw(
                        ctx,
                        &ui::View::default(),
                        &mut editor,
                        &mut modal,
                        None,
                        &mut state
                    )
                    .is_empty()
                );
            });
            for _ in 0..3 {
                backend.render(|ctx| {
                    super::super::draw(
                        ctx,
                        &ui::View::default(),
                        &mut editor,
                        &mut modal,
                        None,
                        &mut state,
                    );
                });
            }
            backend.render(|ctx| {
                super::super::draw(
                    ctx,
                    &ui::View::default(),
                    &mut editor,
                    &mut modal,
                    None,
                    &mut state,
                );
                assert_eq!(ctx.focused_name(), Some("attention-navigation"));
            });
            assert!(!navigation_open(&state));
            assert_eq!(editor.lines().join("\n"), draft);
            assert_eq!(
                (state.composer.cursor_row, state.composer.cursor_col),
                caret
            );
            let retained = state.retained_question.as_ref().unwrap();
            assert_eq!(retained.key, original.key);
            assert_eq!(retained.source, original.source);
            assert_eq!(retained.request, original.request);
        }
    }

    #[test]
    fn focused_form_picker_opens_native_review_without_graph_or_submission() {
        let mut state = SurfaceState::new();
        state.attention_mode = true;
        state.questions = vec![delivery_question()];
        let expected = state.questions[0].clone();
        let mut backend = TestBackend::new(70, 22);
        for _ in 0..3 {
            render(&mut backend, &mut state);
        }
        backend.assert_contains("Choose a release");
        backend.render(|ctx| {
            draw(
                ctx,
                &ui::View::default(),
                &mut state,
                70,
                22,
                &mut Vec::new(),
            );
            assert!(ctx.measured_layout_rect("t3oxide.library").is_none());
            assert!(ctx.measured_layout_rect("t3oxide.projects").is_none());
        });
        let (x, y) = backend.find_text("Forms 1").expect("direct Forms picker");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(
                ctx,
                &ui::View::default(),
                &mut state,
                70,
                22,
                &mut Vec::new(),
            );
        });
        for _ in 0..3 {
            render(&mut backend, &mut state);
        }
        assert!(navigation_open(&state));
        let (x, y) = backend
            .find_text("Release scope")
            .expect("native request in picker");
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &ui::View::default(), &mut state, 70, 22, &mut actions);
        });
        assert!(matches!(actions.as_slice(), [Action::Answer(item)]
            if item.key == expected.key && item.source == expected.source
                && item.request == expected.request));
        assert!(!navigation_open(&state));
        let retained = state
            .retained_question
            .as_ref()
            .expect("request retained after review");
        assert_eq!(retained.key, expected.key);
        assert_eq!(retained.source, expected.source);
        assert_eq!(retained.request, expected.request);
    }

    #[test]
    fn focused_library_disclosure_preserves_the_retained_request_until_selection() {
        let mut state = SurfaceState::new();
        state.questions = vec![delivery_question()];
        state.contents = vec![content(
            "Alpha",
            "guide",
            "guide",
            "Release guide",
            "Evidence",
        )];
        let mut backend = TestBackend::new(70, 22);
        for _ in 0..3 {
            render(&mut backend, &mut state);
        }
        let expected = state
            .retained_question
            .as_ref()
            .map(|item| (item.key.clone(), item.source.clone(), item.request.clone()));
        let (x, y) = backend
            .find_text("Library 1")
            .expect("direct evidence picker");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(
                ctx,
                &ui::View::default(),
                &mut state,
                70,
                22,
                &mut Vec::new(),
            );
        });
        for _ in 0..3 {
            render(&mut backend, &mut state);
        }
        assert_eq!(
            state
                .retained_question
                .as_ref()
                .map(|item| { (item.key.clone(), item.source.clone(), item.request.clone()) }),
            expected
        );
        assert!(selected_content(&state).is_none());
        backend.assert_contains("Release guide");
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Esc).build(), |ctx| {
            draw(
                ctx,
                &ui::View::default(),
                &mut state,
                70,
                22,
                &mut Vec::new(),
            );
        });
        assert!(!navigation_open(&state));
        assert_eq!(
            state
                .retained_question
                .as_ref()
                .map(|item| { (item.key.clone(), item.source.clone(), item.request.clone()) }),
            expected
        );
    }

    #[test]
    fn r8_small_context_keeps_navigation_and_retained_source_addressable() {
        let mut state = explorer_state();
        state.attention_mode = true;
        state.show_context = true;
        state.questions = vec![delivery_question()];
        state.contents = vec![content(
            "Alpha",
            "guide",
            "guide",
            "Release guide",
            "Original evidence",
        )];
        let mut editor = ui::editor("Exact 日本語  \n", "violet", "");
        let mut backend =
            super::super::tests::ResizableBackend::new(40, 12).with_activation_boundaries();
        for _ in 0..4 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        backend.assert_contains("Forms 1");
        backend.assert_contains("Library 1");
        backend.assert_contains("Projects");
        backend.assert_contains("ⓘ");
        let (x, y) = backend.find_text("Projects").unwrap();
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().click(x, y).build(),
        );
        for _ in 0..4 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        backend.assert_contains("│ Alpha │");
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().key_code(KeyCode::Esc).build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        let (x, y) = backend.find_text("ⓘ").unwrap();
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().click(x, y).build(),
        );
        for _ in 0..4 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        backend.assert_contains("Copy ID");
        backend.assert_contains("Alpha");
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().key_code(KeyCode::Esc).build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        assert_eq!(editor.lines().join("\n"), "Exact 日本語  \n");
    }

    #[test]
    fn r8_short_form_action_follows_content_and_retained_source() {
        let mut state = explorer_state();
        state.questions = vec![delivery_question()];
        let mut backend = TestBackend::new(140, 38);
        for _ in 0..3 {
            render(&mut backend, &mut state);
        }
        let (_, body) = backend.find_text("Choose a release").unwrap();
        let (_, action) = backend.find_text("Answer").unwrap();
        assert!(
            action > body && action - body <= 2,
            "short action at {action}, body at {body}"
        );
        state.selected_project = Some(source("Empty").project_key());
        for _ in 0..3 {
            render(&mut backend, &mut state);
        }
        backend.assert_contains("Retained");
        assert_eq!(selected_source(&state).unwrap().project, "Alpha");
    }

    #[test]
    fn review_scope_control_cannot_steal_a_queued_question_click() {
        for width in [100, 140] {
            let mut state = explorer_state();
            state.attention_mode = true;
            state.show_context = false;
            state.questions = ["First request", "Second request"]
                .into_iter()
                .enumerate()
                .map(|(index, title)| AttentionItem {
                    key: format!("request-{index}"),
                    title: title.into(),
                    source: Source {
                        thread: format!("thread-{index}"),
                        ..source("Alpha")
                    },
                    request: json!({"id":format!("request-{index}"),"revision":"original"}),
                    ..AttentionItem::default()
                })
                .collect();
            let expected = state.questions[0].clone();
            let mut editor = ui::editor("Unsent 日本語  \n", "violet", "");
            let mut backend =
                super::super::tests::ResizableBackend::new(width, 31).with_activation_boundaries();
            for _ in 0..3 {
                surface_frame(&mut backend, &mut state, &mut editor, vec![]);
            }
            backend.render(|ctx| {
                let mut modal = ui::editor("", "violet", "");
                super::super::draw(
                    ctx,
                    &ui::View::default(),
                    &mut editor,
                    &mut modal,
                    None,
                    &mut state,
                );
                assert!(ctx.focus_by_name("attention-list"));
            });
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
            let (x, y) = backend.find_text("Alpha · First request").unwrap();
            assert!(state.selected_project.is_none());
            let mut actions = surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new()
                    .key_code(KeyCode::Down)
                    .click(x, y)
                    .build(),
            );
            for _ in 0..4 {
                actions.extend(surface_frame(&mut backend, &mut state, &mut editor, vec![]));
            }
            assert_eq!(state.selected_project, Some(expected.source.project_key()));
            assert_eq!(state.selected_question.as_ref(), Some(&expected.key));
            assert_eq!(selected_source(&state), Some(&expected.source));
            assert!(
                matches!(actions.as_slice(), [Action::Answer(item)]
                    if item.key == expected.key && item.source == expected.source
                        && item.request == expected.request),
                "queued row click at width {width} must retain its source: {actions:?}"
            );
            assert_eq!(editor.lines().join("\n"), "Unsent 日本語  \n");
        }
    }

    #[test]
    fn review_empty_project_preserves_report_after_retaining_a_question() {
        for (width, height) in [(140, 38), (100, 31)] {
            let mut state = explorer_state();
            state.attention_mode = true;
            state.show_context = false;
            state.active_key = "environment-a/original-source".into();
            state.projects = vec![source("Alpha"), source("Empty")];
            let question = AttentionItem {
                key: "original-request".into(),
                source: source("Alpha"),
                title: "Permission needed".into(),
                request: json!({"id":"original","revision":"retained"}),
                ..AttentionItem::default()
            };
            state.questions.push(question.clone());
            let report = content(
                "Alpha",
                "report",
                "research",
                "Release report",
                "Exact retained report 日本語",
            );
            state.contents.push(report.clone());
            let draft = "  Unsent café 日本語\t \n\n";
            let mut editor = ui::editor(draft, "violet", "");
            let mut backend = super::super::tests::ResizableBackend::new(width, height)
                .with_activation_boundaries();
            let mut actions = Vec::new();
            for _ in 0..3 {
                actions.extend(surface_frame(&mut backend, &mut state, &mut editor, vec![]));
            }
            assert_eq!(state.selected_question.as_ref(), Some(&question.key));
            let (x, y) = backend.find_text("Library 1").unwrap();
            actions.extend(surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().click(x, y).build(),
            ));
            for _ in 0..3 {
                actions.extend(surface_frame(&mut backend, &mut state, &mut editor, vec![]));
            }
            let (x, y) = backend.find_text("Research · Release report").unwrap();
            actions.extend(surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().click(x, y).build(),
            ));
            for _ in 0..3 {
                actions.extend(surface_frame(&mut backend, &mut state, &mut editor, vec![]));
            }
            assert_eq!(selected_content(&state), Some(&report));
            let (x, y) = backend.find_text("│ Empty │").unwrap();
            actions.extend(surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().click(x + 3, y).build(),
            ));
            for _ in 0..3 {
                actions.extend(surface_frame(&mut backend, &mut state, &mut editor, vec![]));
            }
            assert!(!state.graph_global);
            assert_eq!(state.selected_project, Some(source("Empty").project_key()));
            assert_eq!(
                selected_content(&state),
                Some(&report),
                "empty Browse must retain the exact report at {width}x{height}"
            );
            backend.assert_contains("Browse · Empty");
            backend.assert_contains("Forms 0");
            backend.assert_contains("Library 0");
            backend.assert_contains("No loaded content");
            backend.assert_contains("Exact retained report 日本語");
            let (x, y) = backend.find_text("All projects").unwrap();
            actions.extend(surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().click(x, y).build(),
            ));
            for _ in 0..3 {
                actions.extend(surface_frame(&mut backend, &mut state, &mut editor, vec![]));
            }
            assert!(state.graph_global);
            assert_eq!(state.selected_project, Some(source("Empty").project_key()));
            assert_eq!(selected_content(&state), Some(&report));
            assert_eq!(state.selected_question.as_ref(), Some(&question.key));
            let retained = state.retained_question.as_ref().unwrap();
            assert_eq!(retained.key, question.key);
            assert_eq!(retained.source, question.source);
            assert_eq!(retained.request, question.request);
            assert_eq!(state.active_key, "environment-a/original-source");
            assert_eq!(editor.lines().join("\n"), draft);
            assert!(
                actions.is_empty(),
                "browsing must not dispatch: {actions:?}"
            );
        }
    }

    #[test]
    fn review_compact_pointer_return_retains_project_content_and_draft() {
        for (width, height) in [(46, 22), (40, 12)] {
            for project in ["Alpha", "Empty"] {
                for form in [false, true] {
                    let mut state = explorer_state();
                    state.attention_mode = true;
                    state.show_context = false;
                    state.active_key = "environment-a/original-source".into();
                    state.projects = vec![source("Alpha"), source("Empty")];
                    if form {
                        state.questions.push(AttentionItem {
                            key: "original-request".into(),
                            source: source("Alpha"),
                            title: "Choose target".into(),
                            request: json!({"id":"original","revision":"retained"}),
                            ..AttentionItem::default()
                        });
                    } else {
                        state.contents = vec![content(
                            "Alpha",
                            "report",
                            "research",
                            "Release report",
                            "Exact retained report",
                        )];
                    }
                    let mut editor = ui::editor("Unsent 日本語  \n\n", "violet", "");
                    let mut backend = super::super::tests::ResizableBackend::new(140, 38)
                        .with_activation_boundaries();
                    for _ in 0..3 {
                        surface_frame(&mut backend, &mut state, &mut editor, vec![]);
                    }
                    let (x, y) = backend.find_text(&format!("│ {project} │")).unwrap();
                    surface_frame(
                        &mut backend,
                        &mut state,
                        &mut editor,
                        EventBuilder::new().click(x + 3, y).build(),
                    );
                    for _ in 0..3 {
                        surface_frame(&mut backend, &mut state, &mut editor, vec![]);
                    }
                    assert!(!state.graph_global);
                    let selected = state.attention.selected.clone();
                    let retained = state.attention.retained_content.clone();
                    let question = state
                        .retained_question
                        .as_ref()
                        .map(|item| (item.key.clone(), item.source.clone(), item.request.clone()));
                    surface_frame(
                        &mut backend,
                        &mut state,
                        &mut editor,
                        EventBuilder::new().resize(width, height).build(),
                    );
                    for _ in 0..3 {
                        surface_frame(&mut backend, &mut state, &mut editor, vec![]);
                    }
                    let (x, y) = backend.find_text("All projects").unwrap_or_else(|| {
                        panic!(
                            "{width}x{height} project {project} needs pointer return:\n{}",
                            backend.text()
                        )
                    });
                    let mut actions = surface_frame(
                        &mut backend,
                        &mut state,
                        &mut editor,
                        EventBuilder::new().click(x, y).build(),
                    );
                    for _ in 0..3 {
                        actions.extend(surface_frame(
                            &mut backend,
                            &mut state,
                            &mut editor,
                            vec![],
                        ));
                    }
                    assert!(state.graph_global);
                    backend.assert_contains("Projects · All");
                    assert_eq!(state.selected_project, Some(source(project).project_key()));
                    assert_eq!(state.attention.selected, selected);
                    assert_eq!(state.attention.retained_content, retained);
                    assert_eq!(
                        state.retained_question.as_ref().map(|item| (
                            item.key.clone(),
                            item.source.clone(),
                            item.request.clone()
                        )),
                        question
                    );
                    assert_eq!(state.active_key, "environment-a/original-source");
                    assert_eq!(editor.lines().join("\n"), "Unsent 日本語  \n\n");
                    assert!(
                        actions.is_empty(),
                        "global return cannot dispatch: {actions:?}"
                    );
                    surface_frame(
                        &mut backend,
                        &mut state,
                        &mut editor,
                        EventBuilder::new().resize(140, 38).build(),
                    );
                    for _ in 0..3 {
                        surface_frame(&mut backend, &mut state, &mut editor, vec![]);
                    }
                    backend.assert_contains("Projects · All");
                    assert_eq!(state.attention.selected, selected);
                }
            }
        }
    }

    #[test]
    fn unresolved_content_does_not_disable_valid_project_navigation() {
        let mut state = explorer_state();
        state.projects = vec![source("Alpha")];
        let mut inherited = content(
            "",
            "inherited",
            "explanation",
            "Parent report",
            "Parent body",
        );
        inherited.source.thread = "unloaded-parent".into();
        state.contents = vec![inherited];
        let mut backend = TestBackend::new(100, 28);
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        backend.assert_contains("Parent body");
        backend.assert_not_contains("graph navigation needs");
        let selected = state.attention.selected.clone();
        state.contents[0].source.project_id = "Alpha".into();
        state.contents[0].source.project = "Alpha".into();
        render(&mut backend, &mut state);
        backend.assert_contains("Parent body");
        backend.assert_not_contains("Source changed");
        assert_eq!(state.attention.selected, selected);
        assert_eq!(selected_source(&state).unwrap().project_id, "Alpha");
        let (x, y) = backend
            .find_text("│ Alpha │")
            .expect("valid project stays navigable");
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x + 3, y).build(), |ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
        });
        assert_eq!(state.selected_project, Some(source("Alpha").project_key()));
        assert!(!state.graph_global);
        assert!(actions.is_empty());
    }

    #[test]
    fn library_is_independent_of_questions_and_keeps_selection_through_context_and_resize() {
        let mut state = explorer_state();
        state.contents = vec![
            content(
                "Alpha",
                "research",
                "research",
                "Check",
                "## Evidence\nFirst research body",
            ),
            content(
                "Alpha",
                "guide",
                "guide",
                "Deploy",
                "## Steps\nSecond guide body\n1. Build\n2. Verify",
            ),
        ];
        let mut backend = TestBackend::new(100, 28);
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        backend.assert_contains("First research body");
        backend.assert_contains("Projects");
        let mut actions = Vec::new();
        backend.render(|ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
            ctx.focus_by_name("attention-list");
        });
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Down).build(), |ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
        });
        render(&mut backend, &mut state);
        backend.assert_contains("Second guide body");
        assert!(
            actions.is_empty(),
            "reading a guide never sends or creates a human obligation"
        );
        let selected = state.attention.selected.clone();
        state.graph_global = true;
        state.show_context = true;
        let mut compact = TestBackend::new(46, 24);
        render(&mut compact, &mut state);
        render(&mut compact, &mut state);
        compact.assert_contains("Projects");
        compact.assert_contains("Second guide body");
        compact.assert_not_contains("environment-a");
        assert_eq!(state.attention.selected, selected);
        assert_eq!(selected_source(&state).unwrap().project_id, "Alpha");
        state.contents.clear();
        render(&mut compact, &mut state);
        render(&mut compact, &mut state);
        compact.assert_contains("Source unavailable");
        compact.assert_contains("Second guide body");
        assert_eq!(selected_source(&state).unwrap().thread, "thread-Alpha");
    }

    #[test]
    fn graph_changes_projects_and_returns_to_global_without_clearing_the_selected_report() {
        let mut state = explorer_state();
        state.projects = vec![source("Alpha"), source("Beta")];
        state.contents = vec![
            content(
                "Alpha",
                "receipt-a",
                "receipt",
                "Alpha result",
                "Alpha completion evidence",
            ),
            content(
                "Beta",
                "receipt-b",
                "receipt",
                "Beta result",
                "Beta completion evidence",
            ),
        ];
        let mut backend = TestBackend::new(100, 28);
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        let mut actions = Vec::new();
        let (x, y) = backend
            .find_text("│ Alpha │")
            .expect("the project graph exposes its painted node");
        backend.run_with_events(EventBuilder::new().click(x + 3, y).build(), |ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
        });
        assert!(
            !state.graph_global,
            "clicking a graph node enters that source-qualified project"
        );
        assert_eq!(state.selected_project, Some(source("Alpha").project_key()));
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        let (x, y) = backend
            .find_text("All projects")
            .expect("global return stays in the graph region");
        backend.run_with_events(EventBuilder::new().click(x + 1, y).build(), |ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
        });
        assert!(state.graph_global);
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        backend.render(|ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
            ctx.focus_by_name("project-navigation");
        });
        backend.run_with_events(
            EventBuilder::new()
                .key_code(KeyCode::Down)
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| {
                draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
            },
        );
        render(&mut backend, &mut state);
        assert!(!state.graph_global);
        assert_eq!(state.selected_project, Some(source("Beta").project_key()));
        backend.assert_contains("Beta completion evidence");
        let selected = state.attention.selected.clone();
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Backspace).build(),
            |ctx| {
                draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
            },
        );
        render(&mut backend, &mut state);
        assert!(state.graph_global);
        assert_eq!(state.attention.selected, selected);
        backend.assert_contains("Beta completion evidence");
        assert!(actions.is_empty());
    }

    #[test]
    fn changed_request_keeps_source_visible_but_cannot_answer_a_stale_offer() {
        let mut state = explorer_state();
        let question = AttentionItem {
            key: "native-request-a".into(),
            source: source("Alpha"),
            title: "Release scope".into(),
            summary: "Choose a release".into(),
            request: json!({"id":"request-a", "revision":"old"}),
            ..AttentionItem::default()
        };
        state.questions.push(question);
        let mut backend = TestBackend::new(100, 28);
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        state.questions[0].request["revision"] = json!("new");
        render(&mut backend, &mut state);
        backend.assert_contains("Source changed");
        backend.assert_contains("Open in A");
        let (x, y) = backend.find_text("Answer").unwrap();
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x + 1, y).build(), |ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
        });
        assert!(
            !actions
                .iter()
                .any(|action| matches!(action, Action::Answer(_)))
        );
        assert_eq!(selected_source(&state).unwrap().thread, "thread-Alpha");
    }

    fn delivery_question() -> AttentionItem {
        AttentionItem {
            key: "native-request-a".into(),
            source: source("Alpha"),
            title: "Release scope".into(),
            summary: "Choose a release".into(),
            request: json!({"id":"request-a", "revision":"a".repeat(64)}),
            ..AttentionItem::default()
        }
    }

    #[test]
    fn delivery_detail_does_not_highlight_an_unrelated_remaining_permission() {
        let mut state = explorer_state();
        state.selected_project = Some(source("Alpha").project_key());
        let question = delivery_question();
        let key = crate::attention::Source::from_request(
            &question.source.environment,
            &question.source.thread,
            &question.request,
        )
        .unwrap()
        .key()
        .unwrap();
        state.questions = vec![question.clone()];
        let mut backend = TestBackend::new(100, 28);
        render(&mut backend, &mut state);
        state.questions = vec![AttentionItem {
            key: "other-permission".into(),
            title: "Permission needed".into(),
            source: Source {
                thread: "permission-thread".into(),
                ..source("Alpha")
            },
            request: json!({"id":"other-permission", "revision":"b".repeat(64)}),
            ..Default::default()
        }];
        for (phase, label) in [
            ("submitting", "Submitting…"),
            ("uncertain", "Unconfirmed"),
            ("acknowledged", "Submitted"),
        ] {
            state.answer_delivery.insert(key.clone(), phase.into());
            for _ in 0..3 {
                render(&mut backend, &mut state);
            }
            let (x, y) = backend.find_text("Permission needed").unwrap();
            assert_ne!(
                backend.buffer().get(x, y).style.bg,
                Some(slt::Theme::dark().selected_bg)
            );
            assert_eq!(
                backend.buffer().get(x, y).style.fg,
                Some(slt::Theme::dark().text_dim)
            );
            backend.assert_contains(label);
            backend.assert_contains("Release scope");
            backend.assert_contains("Browse · Alpha");
            assert_eq!(selected_source(&state), Some(&question.source));
            assert_eq!(
                state.retained_question.as_ref().unwrap().request,
                question.request
            );
            assert_eq!(
                state.attention.selected,
                Some(Selection::Question(question.key.clone()))
            );
            assert_eq!(state.attention.list.selected_index(), Some(0));
            if phase == "uncertain" {
                let (x, y) = backend.find_text("Recovery").unwrap();
                let mut actions = Vec::new();
                backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                    draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
                });
                assert!(
                    matches!(actions.as_slice(), [Action::Command(command)] if command == "recovery")
                );
            }
        }
    }

    #[test]
    fn compact_report_leaf_is_not_duplicated_or_retargeted_by_a_queued_graph_click() {
        let mut state = explorer_state();
        state.attention_mode = true;
        state.active_key = "environment-a/original".into();
        state.projects = vec![source("Alpha"), source("Empty project")];
        let question = delivery_question();
        state.questions = vec![question.clone()];
        state.selected_question = Some(question.key.clone());
        state.retained_question = Some(question.clone());
        state.contents = vec![content(
            "Alpha",
            "leaf",
            "report",
            "Leaf report",
            "## Leaf report\nExact source evidence",
        )];
        let report = state.contents[0].clone();
        select(
            &mut state,
            Selection::Content(report.key.clone()),
            false,
            &mut Vec::new(),
        );
        state.graph_global = false;
        // Browsing Forms leaves the report available in the graph. Switching
        // to Library removes only its duplicate graph leaf on the next draw.
        state.attention.tabs.selected = 0;
        let mut editor = ui::editor("Original 日本語 draft  \n\n", "violet", "");
        let draft = editor.lines().join("\n");
        let mut backend =
            super::super::tests::ResizableBackend::new(46, 22).with_activation_boundaries();
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        backend.render(|ctx| {
            super::super::draw(
                ctx,
                &ui::View::default(),
                &mut editor,
                &mut ui::editor("", "violet", ""),
                None,
                &mut state,
            );
            assert!(ctx.focus_by_name("attention-tabs"));
        });
        surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        let (x, y) = backend.find_text("Leaf report").unwrap();
        assert!(y < 7, "old graph leaf must be painted before the canvas");
        let mut actions = surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new()
                .key_code(KeyCode::Right)
                .click(x, y)
                .build(),
        );
        for _ in 0..4 {
            actions.extend(surface_frame(&mut backend, &mut state, &mut editor, vec![]));
        }
        assert!(
            actions.is_empty(),
            "removed old graph leaf must not activate the form now under it: {actions:?}"
        );
        assert_eq!(selected_content(&state), Some(&report));
        let nav = backend
            .text()
            .lines()
            .take(7)
            .collect::<Vec<_>>()
            .join("\n");
        assert_eq!(nav.matches("Leaf").count(), 1, "{nav}");
        backend.assert_contains("Alpha");
        let (x, y) = backend.find_text("All projects").unwrap();
        assert!(
            surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().click(x, y).build()
            )
            .is_empty()
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        assert!(state.graph_global);
        assert_eq!(selected_content(&state), Some(&report));
        assert_eq!(
            state.retained_question.as_ref().unwrap().request,
            question.request
        );
        assert_eq!(editor.lines().join("\n"), draft);
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().resize(140, 38).build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        let (x, y) = backend.find_text("│ Alpha │").unwrap();
        assert!(
            surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().click(x + 2, y).build()
            )
            .is_empty()
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        backend.assert_contains("│ Leaf report │");
        assert_eq!(selected_content(&state), Some(&report));
    }

    #[test]
    fn compact_browse_tabs_keep_complete_counts_and_report_identity_through_resize() {
        let mut state = explorer_state();
        state.attention_mode = true;
        state.questions = vec![delivery_question()];
        state.contents = vec![content(
            "Alpha",
            "research",
            "research",
            "Research: release evidence 日本語 with a long distinguishing suffix",
            "## Research: release evidence\nOriginal report",
        )];
        let report = state.contents[0].clone();
        let mut editor = ui::editor("Exact draft 日本語  \n\n", "violet", "");
        let draft = editor.lines().join("\n");
        let mut backend =
            super::super::tests::ResizableBackend::new(140, 38).with_activation_boundaries();
        for (width, height) in [(140, 38), (100, 31), (86, 31), (70, 22), (40, 12)] {
            surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().resize(width, height).build(),
            );
            for _ in 0..3 {
                surface_frame(&mut backend, &mut state, &mut editor, vec![]);
            }
        }
        backend.assert_contains("Forms 1");
        backend.assert_contains("Library 1");
        let (x, y) = backend.find_text("Library 1").unwrap();
        let actions = surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().click(x, y).build(),
        );
        assert!(actions.is_empty());
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        let (x, y) = backend
            .find_text("Research · release")
            .expect("retained report in Library picker");
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().click(x, y).build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        assert_eq!(selected_content(&state), Some(&report));
        let label = &state.attention.list.items()[0];
        assert!(label.width() <= 34 && label.contains("release"), "{label}");
        assert!(!label.contains("Research · Research"));
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().key_code(KeyCode::Esc).build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        // The compact strip is one composite: arrows move its cursor, Enter opens its actual list.
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new()
                .key_code(KeyCode::Left)
                .key_code(KeyCode::Enter)
                .build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        backend.assert_contains("Forms 1");
        backend.assert_contains("Release scope");
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().key_code(KeyCode::Esc).build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new()
                .key_code(KeyCode::Right)
                .key_code(KeyCode::Enter)
                .build(),
        );
        for _ in 0..3 {
            surface_frame(&mut backend, &mut state, &mut editor, vec![]);
        }
        assert_eq!(selected_content(&state), Some(&report));
        surface_frame(
            &mut backend,
            &mut state,
            &mut editor,
            EventBuilder::new().key_code(KeyCode::Esc).build(),
        );
        for (width, height) in [(70, 22), (86, 31), (140, 38)] {
            surface_frame(
                &mut backend,
                &mut state,
                &mut editor,
                EventBuilder::new().resize(width, height).build(),
            );
            for _ in 0..3 {
                surface_frame(&mut backend, &mut state, &mut editor, vec![]);
            }
            assert_eq!(selected_content(&state), Some(&report));
            assert_eq!(editor.lines().join("\n"), draft);
        }
        backend.assert_contains("Research · release evidence");
    }

    #[test]
    fn project_ancestry_and_retained_report_survive_actual_resize_and_global_return() {
        let mut state = explorer_state();
        state.attention_mode = true;
        state.active_key = "environment-a/retained-thread".into();
        state.projects = vec![source("Empty project")];
        state.selected_project = Some(source("Empty project").project_key());
        state.graph_global = false;
        state.contents = vec![content(
            "Empty project",
            "report",
            "research",
            "Research: release evidence",
            "## Research: release evidence\nExact retained report",
        )];
        let retained = state.contents[0].clone();
        let mut backend = super::super::tests::ResizableBackend::new(140, 38);
        let mut editor = ui::editor("Unsent 日本語  \n\n", "violet", "");
        let mut modal = ui::editor("", "violet", "");
        for (width, height) in [
            (140, 38),
            (100, 31),
            (86, 31),
            (70, 22),
            (40, 12),
            (140, 38),
        ] {
            backend.run_with_events(EventBuilder::new().resize(width, height).build(), |ctx| {
                super::super::draw(
                    ctx,
                    &ui::View::default(),
                    &mut editor,
                    &mut modal,
                    None,
                    &mut state,
                );
            });
            for _ in 0..3 {
                backend.render(|ctx| {
                    super::super::draw(
                        ctx,
                        &ui::View::default(),
                        &mut editor,
                        &mut modal,
                        None,
                        &mut state,
                    );
                });
            }
            backend.assert_contains("Empty project");
            if height >= 31 {
                backend.assert_contains("│ Empty project │");
                backend.assert_contains("Research: release");
            }
            assert_eq!(selected_content(&state), Some(&retained));
            assert_eq!(editor.lines().join("\n"), "Unsent 日本語  \n\n");
        }
        let (x, y) = backend.find_text("All projects").unwrap();
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            actions = super::super::draw(
                ctx,
                &ui::View::default(),
                &mut editor,
                &mut modal,
                None,
                &mut state,
            );
        });
        for _ in 0..3 {
            backend.render(|ctx| {
                super::super::draw(
                    ctx,
                    &ui::View::default(),
                    &mut editor,
                    &mut modal,
                    None,
                    &mut state,
                );
            });
        }
        assert!(state.graph_global);
        backend.assert_contains("Projects · All");
        backend.assert_contains("Browse · Empty project");
        assert_eq!(selected_content(&state), Some(&retained));
        assert!(actions.is_empty());
        let (x, y) = backend.find_text("│ Empty project │").unwrap();
        backend.run_with_events(EventBuilder::new().click(x + 3, y).build(), |ctx| {
            actions = super::super::draw(
                ctx,
                &ui::View::default(),
                &mut editor,
                &mut modal,
                None,
                &mut state,
            );
        });
        assert!(!state.graph_global);
        assert_eq!(state.selected_project, Some(retained.source.project_key()));
        assert_eq!(selected_content(&state), Some(&retained));
        assert!(actions.is_empty());
    }

    #[test]
    fn scoped_counts_and_graph_geometry_do_not_retarget_the_retained_form() {
        let mut state = explorer_state();
        state.projects = vec![source("Alpha"), source("Beta")];
        let alpha = delivery_question();
        let beta = AttentionItem {
            key: "native-request-b".into(),
            source: source("Beta"),
            ..alpha.clone()
        };
        state.questions = vec![alpha, beta];
        state.contents = vec![
            content("Alpha", "a", "guide", "Alpha guide", "Alpha content"),
            content("Beta", "b", "guide", "Beta guide", "Beta content"),
        ];
        state.selected_project = Some(source("Beta").project_key());
        let mut backend = TestBackend::new(140, 38);
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        let retained = state.attention.selected.clone();
        state.selected_project = Some(source("Alpha").project_key());
        render(&mut backend, &mut state);
        render(&mut backend, &mut state);
        backend.assert_contains("Forms 1");
        backend.assert_contains("Library 1");
        assert_eq!(selected_source(&state).unwrap().project, "Beta");
        assert_eq!(state.attention.selected, retained);
        let mut graph = None;
        backend.render(|ctx| {
            draw(
                ctx,
                &ui::View::default(),
                &mut state,
                140,
                38,
                &mut Vec::new(),
            );
            graph = ctx.measured_layout_rect("t3oxide.projects");
        });
        let graph = graph.expect("persistent graph has native layout geometry");
        assert!(
            graph.width >= 40 && (6..=10).contains(&graph.height),
            "{graph:?}"
        );
        backend.assert_contains("│ Alpha │");
        backend.assert_contains("│ Beta │");
        state.selected_project = None;
        render(&mut backend, &mut state);
        backend.assert_contains("Forms 2");
        backend.assert_contains("Library 2");
        assert_eq!(state.attention.selected, retained);
        assert_eq!(selected_source(&state).unwrap().project, "Beta");
    }

    #[test]
    fn smallest_surface_prioritizes_question_and_opens_review_without_submitting() {
        let mut state = explorer_state();
        state.attention_mode = true;
        state.questions = vec![delivery_question()];
        let mut backend = super::super::tests::ResizableBackend::new(140, 38);
        let mut editor = ui::editor("retained prompt", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for (width, height) in [
            (140, 38),
            (100, 31),
            (86, 31),
            (70, 22),
            (40, 12),
            (140, 38),
        ] {
            backend.run_with_events(EventBuilder::new().resize(width, height).build(), |ctx| {
                super::super::draw(
                    ctx,
                    &ui::View::default(),
                    &mut editor,
                    &mut modal_editor,
                    None,
                    &mut state,
                );
            });
            for _ in 0..3 {
                backend.render(|ctx| {
                    super::super::draw(
                        ctx,
                        &ui::View::default(),
                        &mut editor,
                        &mut modal_editor,
                        None,
                        &mut state,
                    );
                });
            }
            backend.assert_contains("Choose a release");
            if width == 40 {
                backend.assert_not_contains("Answer");
                let (x, y) = backend
                    .find_text("Open form")
                    .expect("compact form opens a readable review");
                let mut actions = Vec::new();
                backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                    actions = super::super::draw(
                        ctx,
                        &ui::View::default(),
                        &mut editor,
                        &mut modal_editor,
                        None,
                        &mut state,
                    );
                });
                assert!(
                    matches!(actions.as_slice(), [Action::Answer(item)] if item.request == state.questions[0].request)
                );
            }
            assert_eq!(selected_source(&state).unwrap().project, "Alpha");
            assert_eq!(editor.lines().join("\n"), "retained prompt");
        }
    }

    #[test]
    fn retained_form_shows_authoritative_delivery_without_inferring_ack_from_queue_removal() {
        let mut state = explorer_state();
        let question = delivery_question();
        let source = crate::attention::Source::from_request(
            &question.source.environment,
            &question.source.thread,
            &question.request,
        )
        .unwrap();
        let key = source.key().unwrap();
        state.questions.push(question.clone());
        let mut backend = TestBackend::new(100, 28);
        render(&mut backend, &mut state);
        state.questions.clear();
        render(&mut backend, &mut state);
        backend.assert_contains("Source unavailable");
        backend.assert_not_contains("Submitted");

        for offered in [true, false] {
            state.questions = if offered {
                vec![question.clone()]
            } else {
                Vec::new()
            };
            for (phase, label) in [
                ("submitting", "Submitting…"),
                ("uncertain", "Unconfirmed"),
                ("acknowledged", "Submitted"),
            ] {
                state.answer_delivery.insert(key.clone(), phase.into());
                render(&mut backend, &mut state);
                backend.assert_contains(label);
                backend.assert_contains("Choose a release");
                backend.assert_not_contains("Source unavailable");
                if phase != "acknowledged" {
                    backend.assert_not_contains("Submitted");
                }
                let (x, y) = backend.find_text("Answer").unwrap();
                let mut actions = Vec::new();
                backend.run_with_events(EventBuilder::new().click(x + 1, y).build(), |ctx| {
                    draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
                });
                assert!(
                    !actions
                        .iter()
                        .any(|action| matches!(action, Action::Answer(_))),
                    "{phase} cannot repeat the answer even if the offer is still present"
                );
                if phase == "uncertain" {
                    let (x, y) = backend.find_text("Recovery").unwrap();
                    backend.run_with_events(EventBuilder::new().click(x + 1, y).build(), |ctx| {
                        draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
                    });
                    assert!(actions.iter().any(
                        |action| matches!(action, Action::Command(command) if command == "recovery")
                    ));
                }
            }
        }
        assert_eq!(state.retained_question.unwrap().request, question.request);
    }

    #[test]
    fn delivery_receipt_requires_the_exact_environment_thread_request_and_revision() {
        let mut state = explorer_state();
        let question = delivery_question();
        let source = crate::attention::Source::from_request(
            &question.source.environment,
            &question.source.thread,
            &question.request,
        )
        .unwrap();
        state.questions.push(question.clone());
        let mut backend = TestBackend::new(100, 28);
        render(&mut backend, &mut state);
        state.questions.clear();
        for component in 0..4 {
            let mut other = source.clone();
            match component {
                0 => other.environment_id.push_str("-other"),
                1 => other.thread_id.push_str("-other"),
                2 => other.request_id.push_str("-other"),
                _ => other.revision = "b".repeat(64),
            }
            state
                .answer_delivery
                .insert(other.key().unwrap(), "acknowledged".into());
        }
        state
            .answer_delivery
            .insert(source.key().unwrap(), "unknown".into());
        render(&mut backend, &mut state);
        backend.assert_contains("Source unavailable");
        backend.assert_not_contains("Submitted");
        state.questions.push(question);
        state
            .answer_delivery
            .insert(source.key().unwrap(), "rejected".into());
        render(&mut backend, &mut state);
        backend.assert_contains("Not sent");
        let (x, y) = backend.find_text("Answer").unwrap();
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x + 1, y).build(), |ctx| {
            draw(ctx, &ui::View::default(), &mut state, 100, 28, &mut actions);
        });
        assert!(
            actions
                .iter()
                .any(|action| matches!(action, Action::Answer(_)))
        );
    }
    #[test]
    fn narrow_context_tools_capture_and_resume_the_selected_source() {
        let mut backend = TestBackend::new(27, 12);
        let mut state = SurfaceState::new();
        state.attention_mode = true;
        state.active_key = "env/alpha".into();
        state.source = Some(Source {
            environment: "env".into(),
            thread: "alpha".into(),
            ..Default::default()
        });
        let source = Source {
            environment: "env".into(),
            environment_label: "Env".into(),
            thread: "beta".into(),
            project: "Beta".into(),
            ..Default::default()
        };
        let item = ContentItem {
            key: "env/beta/report".into(),
            revision: "original-revision".into(),
            source: source.clone(),
            title: "Selected report".into(),
            kind: "Evidence".into(),
            ..Default::default()
        };
        state.contents.push(item.clone());
        state.attention.selected = Some(Selection::Content(item.key.clone()));
        let view = ui::View::default();
        for _ in 0..3 {
            backend.render(|ctx| {
                super::super::draw_context(ctx, &view, &mut state, 27, 12, &mut Vec::new());
            });
        }
        for label in ["Capture", "Records", "Resume", "Copy ID"] {
            let (x, y) = backend
                .find_text(label)
                .expect("all four source actions fit");
            assert!(y < 11, "tool must remain inside Context's border");
            let mut actions = Vec::new();
            backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                super::super::draw_context(ctx, &view, &mut state, 27, 12, &mut actions);
            });
            match label {
                "Capture" => assert!(
                    matches!(actions.as_slice(), [Action::CaptureContent { key, revision, source: actual }] if key == &item.key && revision == &item.revision && actual == &source)
                ),
                "Records" | "Resume" => assert!(
                    matches!(actions.as_slice(), [Action::ContextSource { source: actual, resume }] if actual == &source && *resume == (label == "Resume"))
                ),
                "Copy ID" => assert!(actions.is_empty()),
                _ => unreachable!(),
            }
            assert_eq!(state.active_key, "env/alpha");
            assert_eq!(selected_content(&state).unwrap().key, item.key);
            assert_eq!(selected_content(&state).unwrap().revision, item.revision);
        }
        backend.render(|ctx| {
            super::super::draw_context(ctx, &view, &mut state, 27, 12, &mut Vec::new());
            ctx.focus_by_name("context-tools");
        });
        let mut actions = Vec::new();
        backend.run_with_events(
            EventBuilder::new()
                .key_code(KeyCode::End)
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| {
                super::super::draw_context(ctx, &view, &mut state, 27, 12, &mut actions);
            },
        );
        assert!(
            matches!(actions.as_slice(), [Action::ContextSource { source: actual, resume: true }] if actual == &source)
        );
    }
}
