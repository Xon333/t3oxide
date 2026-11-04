//! SLT owns active terminal rendering, widget focus and editor input (A1–A5, B1–B3).
//! Controller actions retain native source identities; a widget never acknowledges a write.

use crate::{domain, project_graph, reports, ui};
use ratatui_textarea::TextArea;
use serde_json::Value;
use slt::{
    Border, ButtonVariant, Color, Context, KeyCode, KeyModifiers, ListState, ScrollState,
    SpinnerState, TextareaState, WidgetColors, palette::tailwind,
};
use std::{
    cell::Cell,
    collections::{BTreeMap, HashMap},
};
use unicode_segmentation::UnicodeSegmentation;

/// Stable source shared by conversation tabs and actual human requests.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Source {
    pub profile: usize,
    pub thread: String,
    pub environment: String,
    pub environment_label: String,
    pub project_id: String,
    pub project: String,
}

impl Source {
    fn project_key(&self) -> String {
        format!("{}\0{}", self.environment, self.project_id)
    }
}

/// One conversation per tab. Draft ownership remains with the controller/state store.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Tab {
    pub key: String,
    pub title: String,
    pub source: Source,
}

/// Source-qualified native question; reports and diagrams are supporting content only.
#[derive(Clone, Debug, Default)]
pub struct AttentionItem {
    pub key: String,
    pub source: Source,
    pub title: String,
    pub summary: String,
    pub supporting_markdown: String,
    pub supporting_source_key: String,
    pub supporting_revision: String,
    pub supporting_truncated: bool,
    pub request: Value,
}

/// UI intent. Only the controller may dispatch or acknowledge native operations.
#[derive(Clone, Debug)]
pub enum Action {
    Highlight(usize),
    Open(usize),
    OpenSource(Source),
    SelectTab(String),
    CloseTab(String),
    Lane(String),
    Command(String),
    Answer(AttentionItem),
    InspectQuestion(AttentionItem),
    Send,
    Stop,
    DraftChanged,
    ModalEditChanged,
    ModalChoice(usize),
    ModalSubmit,
    CloseModal,
    CopyId,
}

/// Presentation state survives mode switches; native and durable state stays elsewhere.
#[derive(Debug, Default)]
pub struct SurfaceState {
    pub tabs: Vec<Tab>,
    pub active_key: String,
    pub source: Option<Source>,
    pub thread_sources: BTreeMap<String, Source>,
    pub projects: Vec<Source>,
    pub questions: Vec<AttentionItem>,
    pub dependency_status: String,
    pub attention_mode: bool,
    pub internal_tabs: bool,
    pub show_context: bool,
    pub selected_project: Option<String>,
    pub selected_question: Option<String>,
    pub graph_global: bool,
    composer: TextareaState,
    composer_key: String,
    tab_editors: HashMap<String, TextareaState>,
    tabs_scroll: ScrollState,
    modal_editor: TextareaState,
    modal_stamp: Option<u64>,
    modal_selected: usize,
    navigator: ListState,
    navigator_keys: Vec<String>,
    navigator_selection: usize,
    transcript: ScrollState,
    pub(crate) transcript_layout: Option<ui::TranscriptLayout>,
    pub(crate) transcript_jump: Option<(String, String, usize)>,
    controller_scroll: Option<(usize, bool)>,
    attention_scroll: ScrollState,
    support_scroll: ScrollState,
    project_graph: project_graph::ProjectGraph,
    modal_scroll: ScrollState,
    context_scroll: ScrollState,
    command_selected: usize,
    command_escape: Option<String>,
    command_open: bool,
    pub retained_question: Option<AttentionItem>,
    pub loading_question: Option<String>,
    reports: reports::Cache,
    spinner: SpinnerState,
}

impl SurfaceState {
    pub fn new() -> Self {
        Self {
            internal_tabs: true,
            graph_global: true,
            transcript: ScrollState::new().stick_to_bottom(true),
            ..Self::default()
        }
    }
}

/// Render a frame and return source-bound actions without dispatching native writes.
pub fn draw(
    ctx: &mut Context,
    view: &ui::View,
    composer: &mut TextArea<'static>,
    modal_editor: &mut TextArea<'static>,
    modal: Option<&ui::Modal>,
    state: &mut SurfaceState,
) -> Vec<Action> {
    let mut actions = Vec::new();
    state.transcript_layout = None;
    if state.composer_key != state.active_key {
        if !state.composer_key.is_empty() {
            state.tab_editors.insert(
                state.composer_key.clone(),
                std::mem::take(&mut state.composer),
            );
        }
        state.composer = state
            .tab_editors
            .remove(&state.active_key)
            .unwrap_or_default();
        state.composer_key = state.active_key.clone();
        state.transcript = ScrollState::new().stick_to_bottom(true);
        state.controller_scroll = None;
        state.command_escape = None;
        state.command_open = false;
        ctx.focus_by_name("composer");
        ctx.request_frame();
    }
    sync_editor(&mut state.composer, composer);
    let width = ctx.width();
    let height = ctx.height();
    ctx.disabled(modal.is_some(), |ctx| {
        ctx.container().grow(1).col(|ctx| {
            ctx.row(|ctx| {
                ctx.text("T3oxide").bold().fg(tailwind::VIOLET.c300);
                mode_button(ctx, "A", !state.attention_mode, || {
                    state.attention_mode = false
                });
                mode_button(ctx, "B", state.attention_mode, || {
                    state.attention_mode = true
                });
                ctx.text(&view.title).truncate().grow(1);
                if ctx.button("Search").clicked {
                    actions.push(Action::Command("search".into()));
                }
                if ctx.button("Commands").clicked {
                    actions.push(Action::Command("palette".into()));
                }
                if ctx.button("Context").clicked {
                    state.show_context = !state.show_context;
                }
            });
            if state.show_context && (width < 110 || state.attention_mode) {
                draw_context(ctx, view, state, &mut actions);
            } else if state.attention_mode {
                draw_attention(ctx, state, height, &mut actions);
            } else {
                draw_tabs(ctx, state, &mut actions);
                ctx.container().grow(1).row(|ctx| {
                    if width >= 65 {
                        draw_navigator(ctx, view, state, height, &mut actions);
                    }
                    ctx.container().grow(1).col(|ctx| {
                        draw_transcript(ctx, view, state, &mut actions);
                        draw_composer(ctx, view, composer, state, height, &mut actions);
                    });
                    if state.show_context && width >= 110 {
                        ctx.container()
                            .w(26)
                            .border(Border::Rounded)
                            .title("Context")
                            .p(1)
                            .col(|ctx| {
                                draw_context(ctx, view, state, &mut actions);
                            });
                    }
                });
            }
            ctx.row(|ctx| {
                ctx.text(&view.status).dim().truncate().grow(1);
                if view.pending > 0 && ctx.button(format!("Recovery {}", view.pending)).clicked {
                    actions.push(Action::Command("recovery".into()));
                }
                if ctx.button("?").clicked {
                    actions.push(Action::Command("help".into()));
                }
            });
            if !view.notice.is_empty() {
                ctx.text(&view.notice).dim().truncate();
            }
        });
    });
    if let Some(modal) = modal {
        draw_modal(ctx, modal, modal_editor, state, &mut actions);
    } else {
        state.modal_stamp = None;
    }
    global_actions(
        ctx,
        modal.is_some(),
        view.lane == "Settled",
        state,
        &mut actions,
    );
    actions
}

fn mode_button(ctx: &mut Context, label: &str, selected: bool, click: impl FnOnce()) {
    let variant = if selected {
        ButtonVariant::Primary
    } else {
        ButtonVariant::Default
    };
    if ctx.button(label).variant(variant).clicked {
        click();
    }
}

fn draw_context(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    ctx.row(|ctx| {
        ctx.text("Context").bold().grow(1);
        if ctx.button("×").clicked {
            state.show_context = false;
        }
    });
    ctx.scrollable(&mut state.context_scroll)
        .grow(1)
        .col(|ctx| {
            for (label, value) in &view.inspector {
                if !value.is_empty() {
                    ctx.text(label).dim();
                    ctx.text(value).wrap();
                }
            }
            for (label, command) in [
                ("Records", "context"),
                ("Capture source", "context-save"),
                ("Resume", "resume"),
            ] {
                if ctx.button(label).clicked {
                    actions.push(Action::Command(command.into()));
                }
            }
        });
    ctx.row(|ctx| {
        if ctx.button("Copy ID").clicked {
            actions.push(Action::CopyId);
        }
        if ctx.button("Internal tabs ✓").clicked {
            state.internal_tabs = true;
            actions.push(Action::Command("internal-tabs".into()));
        }
    });
}

fn draw_tabs(ctx: &mut Context, state: &mut SurfaceState, actions: &mut Vec<Action>) {
    ctx.container().h(1).col(|ctx| {
        ctx.scroll_row(&mut state.tabs_scroll, |ctx| {
            for tab in &state.tabs {
                let variant = if state.active_key == tab.key {
                    ButtonVariant::Primary
                } else {
                    ButtonVariant::Default
                };
                ctx.register_focusable_named(&format!("tab:{}", tab.key));
                if ctx.button(&tab.title).variant(variant).clicked {
                    actions.push(Action::SelectTab(tab.key.clone()));
                }
            }
            if !state.active_key.is_empty() && ctx.button("×").clicked {
                actions.push(Action::CloseTab(state.active_key.clone()));
            }
            if ctx.button("+").clicked {
                actions.push(Action::Command("search".into()));
            }
        });
    });
}

fn draw_navigator(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let width = if ctx.width() >= 100 { 36 } else { 27 };
    ctx.container().w(width).border(Border::Rounded).col(|ctx| {
        let compact = width < 36;
        ctx.row(|ctx| {
            for (lane, label) in [("Open", "Unread"), ("Running", "Running"), ("Pins", "Pins")] {
                if compact && lane == "Pins" {
                    continue;
                }
                let variant = if view.lane == lane {
                    ButtonVariant::Primary
                } else {
                    ButtonVariant::Default
                };
                if ctx.button(label).variant(variant).clicked {
                    actions.push(Action::Lane(lane.into()));
                }
            }
        });
        ctx.row(|ctx| {
            if compact
                && ctx
                    .button("Pins")
                    .variant(if view.lane == "Pins" {
                        ButtonVariant::Primary
                    } else {
                        ButtonVariant::Default
                    })
                    .clicked
            {
                actions.push(Action::Lane("Pins".into()));
            }
            if ctx.button("Settled").clicked {
                actions.push(Action::Lane("Settled".into()));
            }
        });
        ctx.row(|ctx| {
            if ctx.button("Project").clicked {
                actions.push(Action::Command("project".into()));
            }
            if ctx.button("Env").clicked {
                actions.push(Action::Command("environments".into()));
            }
        });
        let keys: Vec<_> = view.rows.iter().map(|row| row.key.clone()).collect();
        let layout_changed = keys != state.navigator_keys;
        if keys != state.navigator_keys {
            state.navigator.set_items(keys.clone());
            state.navigator.set_item_heights(vec![2; keys.len()]);
            state.navigator_keys = keys;
        }
        if state.navigator_selection != view.selected {
            state.navigator.set_selected_index(view.selected);
            state.navigator_selection = view.selected;
        }
        let clicked = Cell::new(None);
        let selected = state.navigator.selected_index();
        ctx.register_focusable_named("navigator");
        let response = ctx.virtual_list_variable(
            &mut state.navigator,
            height.saturating_sub(12).max(3),
            |ctx, index| {
                let row = &view.rows[index];
                let attention = matches!(
                    row.status.as_str(),
                    "failed" | "needs_input" | "needs-input"
                );
                let colors = WidgetColors::new().fg(if attention {
                    tailwind::AMBER.c300
                } else if row.section == "Completed" {
                    tailwind::SLATE.c400
                } else {
                    tailwind::SLATE.c100
                });
                ctx.row(|ctx| {
                    let marker = if selected == Some(index) {
                        "▸"
                    } else if row.pinned {
                        "◆"
                    } else if row.section == "Unread" {
                        "●"
                    } else {
                        " "
                    };
                    ctx.text(marker).fg(if attention {
                        tailwind::AMBER.c300
                    } else {
                        tailwind::VIOLET.c300
                    });
                    ctx.register_focusable_named(&format!("thread:{}", row.key));
                    if ctx.button(&row.title).colors(&colors).clicked && !layout_changed {
                        clicked.set(Some(index));
                    }
                });
                let (environment, project) = row
                    .subtitle
                    .split_once(" / ")
                    .unwrap_or((&row.subtitle, ""));
                let project_identity = state
                    .thread_sources
                    .get(&row.key)
                    .map_or_else(|| project.to_owned(), Source::project_key);
                ctx.line(|ctx| {
                    ctx.text("  ");
                    let environment_identity = state
                        .thread_sources
                        .get(&row.key)
                        .map_or(environment, |source| source.environment.as_str());
                    ctx.text(environment)
                        .fg(identity_color(environment_identity));
                    ctx.text(" · ").dim();
                    ctx.text(project).fg(identity_color(&project_identity));
                    if !row.status.is_empty() {
                        ctx.text(format!(" · {}", row.status)).dim();
                    }
                });
            },
        );
        if response.changed
            && let Some(index) = state.navigator.selected_index()
        {
            state.navigator_selection = index;
            actions.push(Action::Highlight(index));
        }
        if let Some(index) = clicked.get() {
            actions.push(Action::Open(index));
        } else if !layout_changed
            && response.focused
            && ctx.consume_key_code(KeyCode::Enter)
            && let Some(index) = state.navigator.selected_index()
        {
            actions.push(Action::Open(index));
        }
        if view.rows.is_empty() {
            ctx.text("No conversations").dim();
        }
    });
}

fn draw_transcript(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    // Context::width is terminal-wide; reserve the real A-side panels and chrome.
    let sidebar = if ctx.width() >= 100 {
        36
    } else if ctx.width() >= 65 {
        27
    } else {
        0
    };
    let context = if state.show_context && ctx.width() >= 110 {
        26
    } else {
        0
    };
    let geometry = reports::Geometry {
        width: ctx.width().saturating_sub(sidebar + context + 4).max(1),
        height: ctx.height().saturating_sub(14).max(3),
    };
    ctx.row(|ctx| {
        if view.requests > 0 && ctx.button(format!("Questions {}", view.requests)).clicked {
            actions.push(Action::Command("requests".into()));
        }
        if ctx.button("Earlier").clicked {
            actions.push(Action::Command("earlier".into()));
        }
        if ctx.button("Latest").clicked {
            state.transcript.offset = state.transcript.max_offset();
            actions.push(Action::Command("latest".into()));
        }
        ctx.spacer();
        if ctx.button("Copy ID").clicked {
            actions.push(Action::CopyId);
        }
    });
    if state.controller_scroll != Some((view.transcript_scroll, view.follow)) {
        state.transcript.offset = if view.follow {
            usize::MAX
        } else {
            view.transcript_scroll
        };
        state.controller_scroll = Some((view.transcript_scroll, view.follow));
    }
    let group = format!("t3oxide.transcript:{}", state.active_key);
    let measured = ctx.measured_layout_rect(&group);
    let items: Option<Vec<_>> = measured.and_then(|viewport| {
        view.messages
            .iter()
            .map(|message| {
                let rect = ctx.measured_layout_rect(&transcript_message_group(message))?;
                Some((
                    message.key.clone(),
                    rect.y.saturating_sub(viewport.y + 2) as usize,
                    rect.height as usize,
                ))
            })
            .collect()
    });
    if let Some((key, revision, inner)) = &state.transcript_jump {
        if let Some(message) = view.messages.iter().find(|message| {
            &message.key == key && &reports::content_revision(&message.text) == revision
        }) && let Some((_, offset, height)) = items
            .as_ref()
            .and_then(|items| items.iter().find(|(id, _, _)| id == &message.key))
        {
            state.transcript.offset = offset.saturating_add((*inner).min(height.saturating_sub(1)));
            state.transcript_jump = None;
            ctx.request_frame();
        } else {
            ctx.request_frame();
        }
    }
    ctx.group(&group).grow(1).col(|ctx| {
        ctx.scrollable(&mut state.transcript)
            .grow(1)
            .border(Border::Rounded)
            .p(1)
            .col(|ctx| {
                for message in &view.messages {
                    ctx.group(&transcript_message_group(message)).col(|ctx| {
                        ctx.text(&message.role).bold().fg(if message.role == "You" {
                            tailwind::CYAN.c300
                        } else {
                            tailwind::VIOLET.c300
                        });
                        state.reports.draw(
                            ctx,
                            &message.key,
                            &reports::content_revision(&message.text),
                            &message.text,
                            geometry,
                        );
                        if message.truncated {
                            ctx.text("Excerpt").dim();
                        }
                        if message.streaming {
                            ctx.spinner(&state.spinner);
                        }
                    });
                }
            });
    });
    if let (Some(items), Some(rect)) = (items, measured) {
        state.transcript_layout = Some(ui::TranscriptLayout {
            items,
            top: state.transcript.offset,
            content: state.transcript.content_height() as usize,
            visible: state.transcript.viewport_height() as usize,
            max: state.transcript.max_offset(),
            width: u16::try_from(rect.width.saturating_sub(4)).unwrap_or(u16::MAX),
        });
    } else {
        ctx.request_frame();
    }
}

fn transcript_message_group(message: &ui::MessageView) -> String {
    format!(
        "t3oxide.message:{}:{}",
        message.key,
        reports::content_revision(&message.text)
    )
}

fn draw_composer(
    ctx: &mut Context,
    view: &ui::View,
    legacy: &mut TextArea<'static>,
    state: &mut SurfaceState,
    height: u32,
    actions: &mut Vec<Action>,
) {
    ctx.container()
        .border(Border::Rounded)
        .title("Prompt")
        .col(|ctx| {
            let available = ctx.width().saturating_sub(8).max(10);
            state.composer.wrap_width = Some(available);
            let focused = ctx.register_focusable_named("composer");
            let typed_slash = ctx.key_presses_when(focused).any(|(_, key)| {
                key.code == KeyCode::Char('/') && key.modifiers == KeyModifiers::NONE
            });
            composer_keys(ctx, focused, state, actions);
            if ctx
                .textarea(&mut state.composer, if height < 24 { 3 } else { 5 })
                .changed
            {
                *legacy = ui::editor(&state.composer.value(), &view.theme, "Write a message…");
                state.command_selected = 0;
                if typed_slash && state.composer.value().starts_with('/') {
                    state.command_open = true;
                    state.command_escape = None;
                }
                actions.push(Action::DraftChanged);
            }
            let indices = if state.command_open {
                completion_indices(&state.composer.value(), state.command_escape.as_deref())
            } else {
                Vec::new()
            };
            if !indices.is_empty() {
                for (position, index) in indices
                    .iter()
                    .enumerate()
                    .skip(state.command_selected.saturating_sub(3))
                    .take(4)
                {
                    let action = &domain::ACTIONS[*index];
                    let variant = if position == state.command_selected {
                        ButtonVariant::Primary
                    } else {
                        ButtonVariant::Default
                    };
                    if ctx
                        .button(format!("/{}  {}", action.name, action.label))
                        .variant(variant)
                        .clicked
                    {
                        actions.push(Action::Command(action.name.into()));
                        state.command_escape = Some(state.composer.value());
                        state.command_open = false;
                    }
                }
            }
            let compact = ctx.width() < 110 || state.show_context;
            if compact {
                ctx.text("↵ newline · Ctrl↵ / Ctrl+] send").dim();
            }
            ctx.row(|ctx| {
                if ctx.button("Model · effort").clicked {
                    actions.push(Action::Command("model".into()));
                }
                if compact {
                    ctx.spacer();
                } else {
                    ctx.text("↵ newline · Ctrl↵ / Ctrl+] send")
                        .dim()
                        .truncate()
                        .grow(1);
                }
                if ctx.button("Stop").clicked {
                    actions.push(Action::Stop);
                }
                let disabled =
                    state.active_key.is_empty() || state.composer.value().trim().is_empty();
                if ctx
                    .disabled(disabled, |ctx| {
                        ctx.button("Send").variant(ButtonVariant::Primary).show()
                    })
                    .clicked
                {
                    actions.push(Action::Send);
                    ctx.focus_by_name("composer");
                    ctx.request_frame();
                }
            });
        });
}

fn draw_attention(
    ctx: &mut Context,
    state: &mut SurfaceState,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let mut projects: BTreeMap<String, (String, String, usize)> = BTreeMap::new();
    for source in &state.projects {
        projects.entry(source.project_key()).or_insert((
            source.project.clone(),
            source.environment_label.clone(),
            0,
        ));
    }
    for question in &state.questions {
        let entry = projects.entry(question.source.project_key()).or_insert((
            question.source.project.clone(),
            question.source.environment_label.clone(),
            0,
        ));
        entry.2 += 1;
    }
    if state.retained_question.as_ref().map(|q| &q.key) != state.selected_question.as_ref() {
        state.retained_question = state
            .selected_question
            .as_deref()
            .and_then(|key| state.questions.iter().find(|q| q.key == key))
            .cloned();
    }
    let geometry = reports::Geometry {
        width: ctx.width().saturating_sub(32).max(1),
        height: height.saturating_sub(11).max(3),
    };
    ctx.container().grow(1).row(|ctx| {
        ctx.container().w(28).col(|ctx| {
            ctx.scrollable(&mut state.attention_scroll)
                .grow(1)
                .border(Border::Rounded)
                .title("Questions")
                .col(|ctx| {
                    for (project, (name, environment, count)) in &projects {
                        if *count == 0 {
                            continue;
                        }
                        ctx.text(format!("{name} · {environment} · {count}"))
                            .bold()
                            .fg(identity_color(project));
                        for question in state
                            .questions
                            .iter()
                            .filter(|q| &q.source.project_key() == project)
                        {
                            ctx.register_focusable_named(&format!("question:{}", question.key));
                            if ctx.button(&question.title).clicked {
                                state.selected_project = Some(project.clone());
                                state.selected_question = Some(question.key.clone());
                                state.retained_question = Some(question.clone());
                                actions.push(Action::InspectQuestion(question.clone()));
                            }
                        }
                    }
                    if state.questions.is_empty() {
                        ctx.text("No loaded questions").dim();
                    }
                });
            // B1: level changes retain this region and the selected report.
            let graph_height = (height / 3).clamp(6, 11);
            ctx.container()
                .h(graph_height)
                .border(Border::Rounded)
                .title("Projects")
                .col(|ctx| {
                    if !state.graph_global && ctx.button("↑ All projects").clicked {
                        state.graph_global = true;
                    }
                    let mut sources = state.projects.clone();
                    for question in &state.questions {
                        if !sources
                            .iter()
                            .any(|source| source.project_key() == question.source.project_key())
                        {
                            sources.push(question.source.clone());
                        }
                    }
                    let mut nodes = Vec::new();
                    let mut edges = Vec::new();
                    for source in &sources {
                        if !state.graph_global
                            && state.selected_project.as_deref() != Some(&source.project_key())
                        {
                            continue;
                        }
                        let project_id = project_node(source);
                        nodes.push(project_graph::Node {
                            id: project_id.clone(),
                            label: source.project.clone(),
                            color: identity_color(&source.project_key()),
                        });
                        if !state.graph_global {
                            for question in state.questions.iter().filter(|question| {
                                question.source.project_key() == source.project_key()
                            }) {
                                let id = question_node(question);
                                nodes.push(project_graph::Node {
                                    id: id.clone(),
                                    label: question.title.clone(),
                                    color: identity_color(&source.project_key()),
                                });
                                edges.push(project_graph::Edge {
                                    from: project_id.clone(),
                                    to: id,
                                });
                            }
                        }
                    }
                    let selected = if state.graph_global {
                        sources
                            .iter()
                            .find(|source| {
                                state.selected_project.as_deref() == Some(&source.project_key())
                            })
                            .map(project_node)
                    } else {
                        state
                            .questions
                            .iter()
                            .find(|question| {
                                Some(&question.key) == state.selected_question.as_ref()
                            })
                            .map(question_node)
                    };
                    match state.project_graph.draw(
                        ctx,
                        &nodes,
                        &edges,
                        selected.as_ref(),
                        26,
                        graph_height.saturating_sub(if state.graph_global { 2 } else { 3 }),
                    ) {
                        Ok(response) => {
                            if let Some(activated) = response.activated {
                                match activated {
                                    project_graph::NodeId::Project {
                                        environment_id,
                                        project_id,
                                    } => {
                                        let key = format!("{environment_id}\0{project_id}");
                                        if state.selected_project.as_ref() != Some(&key) {
                                            let question = state
                                                .questions
                                                .iter()
                                                .find(|question| {
                                                    question.source.project_key() == key
                                                })
                                                .cloned();
                                            state.selected_question = question
                                                .as_ref()
                                                .map(|question| question.key.clone());
                                            state.retained_question = question.clone();
                                            if let Some(question) = question {
                                                actions.push(Action::InspectQuestion(question));
                                            }
                                        }
                                        state.selected_project = Some(key);
                                        state.graph_global = false;
                                    }
                                    id @ project_graph::NodeId::Question { .. } => {
                                        if let Some(question) = state
                                            .questions
                                            .iter()
                                            .find(|question| question_node(question) == id)
                                        {
                                            state.selected_project =
                                                Some(question.source.project_key());
                                            state.selected_question = Some(question.key.clone());
                                            state.retained_question = Some(question.clone());
                                            actions.push(Action::InspectQuestion(question.clone()));
                                        }
                                    }
                                }
                            }
                        }
                        Err(error) => {
                            ctx.text(error.to_string()).fg(tailwind::AMBER.c300).wrap();
                        }
                    }
                });
        });
        ctx.container()
            .grow(1)
            .border(Border::Rounded)
            .p(1)
            .col(|ctx| {
                let selected = state.retained_question.as_ref();
                if let Some(question) = selected {
                    let current = state.questions.iter().find(|live| live.key == question.key);
                    let unchanged = current.is_some_and(|live| {
                        live.request == question.request && live.source == question.source
                    });
                    ctx.text(&question.title).bold();
                    source_tags(ctx, &question.source);
                    if !state.dependency_status.is_empty() {
                        ctx.text(&state.dependency_status).dim().wrap();
                    }
                    ctx.text(&question.summary).wrap();
                    if !unchanged {
                        ctx.text(if current.is_some() {
                            "Source changed · select question again"
                        } else {
                            "Source unavailable"
                        })
                        .fg(tailwind::AMBER.c300)
                        .wrap();
                    }
                    ctx.row(|ctx| {
                        if ctx
                            .disabled(!unchanged, |ctx| {
                                ctx.button("Answer").variant(ButtonVariant::Primary).show()
                            })
                            .clicked
                        {
                            actions.push(Action::Answer(question.clone()));
                        }
                        if ctx.button("Open in A").clicked {
                            actions.push(Action::OpenSource(question.source.clone()));
                            state.attention_mode = false;
                        }
                    });
                    ctx.separator();
                    ctx.scrollable(&mut state.support_scroll)
                        .grow(1)
                        .title("Supporting content")
                        .col(|ctx| {
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
                        });
                } else {
                    ctx.text(if state.selected_project.is_some() {
                        "No loaded questions"
                    } else {
                        "Choose a question"
                    })
                    .dim();
                }
            });
    });
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

fn draw_modal(
    ctx: &mut Context,
    modal: &ui::Modal,
    legacy: &mut TextArea<'static>,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    sync_editor(&mut state.modal_editor, legacy);
    let layout_changed = state.modal_stamp != Some(modal.action_stamp);
    if layout_changed {
        state.modal_selected = modal.selected;
        ctx.request_frame();
    }
    let palette = modal.editor && modal.title == "Commands";
    ctx.modal(|ctx| {
        let width = ctx.width().saturating_sub(8).clamp(30, 90);
        let height = ctx.height().saturating_sub(4);
        let background = ctx.theme().bg;
        ctx.group("t3oxide.modal")
            .bg(background)
            .w(width)
            .max_h(height)
            .p(1)
            .col(|ctx| {
                ctx.row(|ctx| {
                    ctx.text(&modal.title).bold().grow(1);
                    if ctx.button("×").clicked && !layout_changed {
                        actions.push(Action::CloseModal);
                    }
                });
                if !modal.description.is_empty() {
                    ctx.text(&modal.description).wrap();
                }
                if modal.editor {
                    let focused = ctx.register_focusable_named("modal-editor");
                    palette_keys(
                        ctx,
                        focused && (!layout_changed || palette),
                        palette,
                        modal,
                        state,
                        actions,
                    );
                    if ctx
                        .disabled(layout_changed && !palette, |ctx| {
                            ctx.textarea(&mut state.modal_editor, 3)
                        })
                        .changed
                    {
                        *legacy = ui::editor(&state.modal_editor.value(), "violet", "Type here…");
                        actions.push(Action::ModalEditChanged);
                    }
                    if ctx
                        .button("Continue")
                        .variant(ButtonVariant::Primary)
                        .clicked
                        && !layout_changed
                    {
                        actions.push(Action::ModalSubmit);
                    }
                }
                let detail_height = ctx.height().saturating_sub(13).max(3);
                ctx.scrollable(&mut state.modal_scroll)
                    .max_h(detail_height)
                    .col(|ctx| {
                        if !modal.detail.is_empty() {
                            ctx.markdown(&modal.detail);
                        }
                        for (index, item) in modal.items.iter().enumerate() {
                            let variant =
                                if item.checked || palette && index == state.modal_selected {
                                    ButtonVariant::Primary
                                } else {
                                    ButtonVariant::Default
                                };
                            ctx.register_focusable_named(&format!(
                                "modal-choice:{}:{index}",
                                modal.action_stamp
                            ));
                            if ctx.button(&item.label).variant(variant).clicked && !layout_changed {
                                actions.push(Action::ModalChoice(index));
                            }
                            if !item.description.is_empty() {
                                ctx.text(&item.description).dim().wrap();
                            }
                        }
                    });
                if !modal.footer.is_empty() {
                    ctx.text(&modal.footer).dim().wrap();
                }
            });
    });
    if state.modal_stamp != Some(modal.action_stamp) {
        state.modal_stamp = Some(modal.action_stamp);
        state.modal_scroll = ScrollState::new();
        if modal.editor {
            ctx.focus_by_name("modal-editor");
        }
    }
}

// The focused widget owns this key layer before textarea's editing handler.
fn composer_keys(
    ctx: &mut Context,
    focused: bool,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    let value = state.composer.value();
    if !value.is_empty() && !value.starts_with('/') {
        state.command_open = false;
    }
    if value.is_empty()
        && ctx
            .key_presses_when(focused)
            .any(|(_, key)| key.code == KeyCode::Char('/') && key.modifiers == KeyModifiers::NONE)
    {
        state.command_open = true;
        state.command_escape = None;
    }
    let completion = if state.command_open {
        completion_indices(&value, state.command_escape.as_deref())
    } else {
        Vec::new()
    };
    let mut consumed = Vec::new();
    for (index, key) in ctx.key_presses_when(focused) {
        if send_key(&key.code, key.modifiers)
            && !(key.code == KeyCode::Enter && !completion.is_empty())
        {
            actions.push(Action::Send);
            consumed.push(index);
        } else if !completion.is_empty() {
            match key.code {
                KeyCode::Enter => {
                    let choice = completion[state.command_selected.min(completion.len() - 1)];
                    actions.push(Action::Command(domain::ACTIONS[choice].name.into()));
                    state.command_escape = Some(state.composer.value());
                    state.command_open = false;
                    consumed.push(index);
                }
                KeyCode::Up => {
                    state.command_selected = state.command_selected.saturating_sub(1);
                    consumed.push(index);
                }
                KeyCode::Down => {
                    state.command_selected = (state.command_selected + 1).min(completion.len() - 1);
                    consumed.push(index);
                }
                KeyCode::Esc => {
                    state.command_escape = Some(state.composer.value());
                    state.command_open = false;
                    consumed.push(index);
                }
                _ => {}
            }
        }
    }
    for index in consumed {
        ctx.consume_event(index);
    }
}

fn palette_keys(
    ctx: &mut Context,
    focused: bool,
    palette: bool,
    modal: &ui::Modal,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    if focused && !palette {
        let submit: Vec<_> = ctx
            .key_presses_when(true)
            .filter(|(_, key)| send_key(&key.code, key.modifiers))
            .map(|(index, _)| index)
            .collect();
        for index in submit {
            ctx.consume_event(index);
            actions.push(Action::ModalSubmit);
        }
    }
    if palette && focused {
        let mut consumed = Vec::new();
        for (index, key) in ctx.key_presses_when(focused) {
            match key.code {
                KeyCode::Enter if !modal.items.is_empty() => {
                    actions.push(Action::ModalChoice(
                        state.modal_selected.min(modal.items.len() - 1),
                    ));
                    consumed.push(index);
                }
                KeyCode::Up => {
                    state.modal_selected = state.modal_selected.saturating_sub(1);
                    consumed.push(index);
                }
                KeyCode::Down => {
                    state.modal_selected =
                        (state.modal_selected + 1).min(modal.items.len().saturating_sub(1));
                    consumed.push(index);
                }
                _ => {}
            }
        }
        for index in consumed {
            ctx.consume_event(index);
        }
    }
}

fn global_actions(
    ctx: &mut Context,
    modal: bool,
    settled: bool,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    let mut consumed = Vec::new();
    let keys: Vec<_> = ctx
        .key_presses_when(true)
        .map(|(index, key)| (index, key.clone()))
        .collect();
    for (index, key) in keys {
        let control = key.modifiers.contains(KeyModifiers::CONTROL);
        let command = control || key.modifiers.contains(KeyModifiers::SUPER);
        let action = match key.code {
            KeyCode::Char('s') if control => Some(Action::Stop),
            KeyCode::Char('q') if command => Some(Action::Command("quit".into())),
            KeyCode::Char('p') if command => Some(Action::Command("palette".into())),
            KeyCode::Char('f') if command => Some(Action::Command("search".into())),
            KeyCode::Char('r') if control => Some(Action::Command("refresh".into())),
            KeyCode::Char('y') if control => Some(Action::Command("requests".into())),
            KeyCode::Char('i') if command => Some(Action::CopyId),
            KeyCode::Char('w') if command => Some(Action::CloseTab(state.active_key.clone())),
            KeyCode::Char('t') if control => Some(Action::Command(
                if settled { "unreviewed" } else { "reviewed" }.into(),
            )),
            KeyCode::F(1) => Some(Action::Command("help".into())),
            KeyCode::F(2) if !modal => {
                state.attention_mode = !state.attention_mode;
                None
            }
            KeyCode::Esc if modal => Some(Action::CloseModal),
            KeyCode::Esc => {
                ctx.focus_by_name("navigator");
                None
            }
            _ => None,
        };
        if let Some(action) = action {
            actions.push(action);
            consumed.push(index);
        }
    }
    for index in consumed {
        ctx.consume_event(index);
    }
}

fn sync_editor(editor: &mut TextareaState, legacy: &TextArea<'static>) {
    let text = legacy.lines().join("\n");
    if editor.value() != text {
        editor.set_value(text);
        editor.cursor_row = editor.lines.len().saturating_sub(1);
        editor.cursor_col = editor.lines[editor.cursor_row].graphemes(true).count();
    }
}

fn completion_indices(text: &str, escaped: Option<&str>) -> Vec<usize> {
    if escaped == Some(text)
        || !text.starts_with('/')
        || text.starts_with("//")
        || text.contains('\n')
    {
        return Vec::new();
    }
    let query = text[1..].to_ascii_lowercase();
    if query.contains(char::is_whitespace) {
        return Vec::new();
    }
    domain::ACTIONS
        .iter()
        .enumerate()
        .filter(|(_, action)| action.name.starts_with(&query))
        .map(|(index, _)| index)
        .collect()
}

fn send_key(code: &KeyCode, modifiers: KeyModifiers) -> bool {
    let control = modifiers.contains(KeyModifiers::CONTROL);
    matches!(code, KeyCode::Enter)
        && (control
            || modifiers.contains(KeyModifiers::SUPER)
            || modifiers.contains(KeyModifiers::SHIFT))
        || control && matches!(code, KeyCode::Char(']' | '\\' | '5' | '4'))
}

fn identity_color(identity: &str) -> Color {
    let colors = [
        tailwind::CYAN.c300,
        tailwind::VIOLET.c300,
        tailwind::EMERALD.c300,
        tailwind::AMBER.c300,
        tailwind::ROSE.c300,
        tailwind::BLUE.c300,
    ];
    let hash = identity.bytes().fold(0usize, |hash, byte| {
        hash.wrapping_mul(31).wrapping_add(byte as usize)
    });
    colors[hash % colors.len()]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn modal_open_requests_layout_and_focus_before_typing() {
        let mut backend = slt::TestBackend::new(140, 38);
        let mut state = SurfaceState::new();
        state.active_key = "env/thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("draft", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        let modal = ui::Modal {
            title: "Commands".into(),
            editor: true,
            action_stamp: 1,
            ..ui::Modal::default()
        };
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&modal),
                    &mut state,
                );
            });
        }
        backend.run_with_events(slt::EventBuilder::new().paste("command").build(), |ctx| {
            draw(
                ctx,
                &view,
                &mut editor,
                &mut modal_editor,
                Some(&modal),
                &mut state,
            );
        });
        assert_eq!(modal_editor.lines().join("\n"), "command");
    }

    #[test]
    fn focused_composer_selects_and_consumes_copy_without_an_app_action() {
        for modifiers in [KeyModifiers::CONTROL, KeyModifiers::SUPER] {
            let mut backend = slt::TestBackend::new(86, 31);
            let mut state = SurfaceState::new();
            state.active_key = "env/thread".into();
            let view = ui::View::default();
            let mut editor = ui::editor("café 日本語\nnext line", "violet", "");
            let mut modal_editor = ui::editor("", "violet", "");
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                });
            }
            let mut actions = Vec::new();
            backend.run_with_events(
                slt::EventBuilder::new()
                    .key_with(KeyCode::Char('c'), modifiers)
                    .build(),
                |ctx| {
                    actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    assert!(
                        ctx.key_mod('c', modifiers),
                        "no selection leaves copy unclaimed"
                    );
                },
            );
            assert!(state.composer.selected_text().is_none());
            assert!(actions.is_empty());
            backend.run_with_events(
                slt::EventBuilder::new()
                    .key_with(KeyCode::Char('a'), modifiers)
                    .key_with(KeyCode::Char('c'), modifiers)
                    .build(),
                |ctx| {
                    actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    assert!(!ctx.key_mod('a', modifiers));
                    assert!(
                        !ctx.key_mod('c', modifiers),
                        "selected focused editor owns copy"
                    );
                },
            );
            assert_eq!(
                state.composer.selected_text().as_deref(),
                Some("café 日本語\nnext line")
            );
            assert_eq!(editor.lines().join("\n"), "café 日本語\nnext line");
            assert!(actions.is_empty());
            backend.render(|ctx| {
                ctx.focus_by_name("navigator");
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
            backend.run_with_events(
                slt::EventBuilder::new()
                    .key_with(KeyCode::Char('c'), modifiers)
                    .build(),
                |ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    assert!(
                        ctx.key_mod('c', modifiers),
                        "an unfocused selection cannot claim copy"
                    );
                },
            );
        }
    }

    #[test]
    fn compact_and_normal_controls_are_visible_and_addressable() {
        for (width, height) in [(86, 31), (140, 38)] {
            let mut backend = slt::TestBackend::new(width, height);
            let mut state = SurfaceState::new();
            state.active_key = "env/thread".into();
            let view = ui::View::default();
            let mut editor = ui::editor("retained draft", "violet", "");
            let mut modal_editor = ui::editor("", "violet", "");
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                });
            }
            for label in [
                "Unread",
                "Running",
                "Pins",
                "Settled",
                "Project",
                "Env",
                "Model · effort",
                "Ctrl+] send",
                "Stop",
                "Send",
            ] {
                backend.assert_contains(label);
            }
            let (x, y) = backend
                .find_text("Env")
                .expect("environment control visible");
            let mut actions = Vec::new();
            backend.run_with_events(slt::EventBuilder::new().click(x, y).build(), |ctx| {
                actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
            assert!(actions.iter().any(
                |action| matches!(action, Action::Command(command) if command == "environments")
            ));
            backend.run_with_events(
                slt::EventBuilder::new().resize(width, height).build(),
                |ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                },
            );
            assert_eq!(editor.lines().join("\n"), "retained draft");
        }
    }

    #[test]
    fn source_qualified_jump_uses_full_slt_measurements_after_layout_and_resize() {
        let mut backend = slt::TestBackend::new(110, 38);
        let mut state = SurfaceState::new();
        state.active_key = "env/thread".into();
        let mut view = ui::View {
            follow: true,
            ..ui::View::default()
        };
        view.messages = (0..12)
            .map(|index| ui::MessageView {
                key: format!("env/thread/generation/item-{index}"),
                text: format!("Message {index}\n\n{}", "Paragraph\n\n".repeat(6)),
                ..ui::MessageView::default()
            })
            .collect();
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        let layout = state.transcript_layout.as_ref().unwrap();
        assert_eq!(
            layout.items.len(),
            12,
            "offscreen messages retain full measurements"
        );
        assert!(layout.max > 0);
        let expected = layout.items[5].1;
        assert!(expected > 0 && expected < layout.max);
        view.follow = false;
        let target = &view.messages[5];
        state.transcript_jump = Some((
            target.key.clone(),
            reports::content_revision(&target.text),
            0,
        ));
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(state.transcript_jump.is_none());
        assert_eq!(state.transcript_layout.as_ref().unwrap().top, expected);
        backend.assert_contains("Message 5");
        view.transcript_scroll = expected;
        state.transcript_jump = Some((
            target.key.clone(),
            reports::content_revision(&target.text),
            1,
        ));
        backend.run_with_events(slt::EventBuilder::new().resize(110, 38).build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(
            state.transcript_jump.is_some(),
            "resize invalidates previous geometry without discarding the jump"
        );
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(state.transcript_jump.is_none());
        assert_eq!(state.transcript_layout.as_ref().unwrap().top, expected + 1);
    }

    #[test]
    fn modal_save_chord_submits_only_the_focused_settled_editor() {
        let mut backend = slt::TestBackend::new(110, 38);
        let mut state = SurfaceState::new();
        let view = ui::View::default();
        let modal = ui::Modal {
            title: "Find in loaded messages".into(),
            editor: true,
            action_stamp: 1,
            ..ui::Modal::default()
        };
        let mut editor = ui::editor("draft", "violet", "");
        let mut modal_editor = ui::editor("match", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&modal),
                    &mut state,
                );
            });
        }
        let mut actions = Vec::new();
        backend.run_with_events(
            slt::EventBuilder::new()
                .key_with(KeyCode::Char(']'), KeyModifiers::CONTROL)
                .build(),
            |ctx| {
                actions = draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&modal),
                    &mut state,
                );
            },
        );
        assert_eq!(modal_editor.lines().join("\n"), "match");
        assert_eq!(editor.lines().join("\n"), "draft");
        assert_eq!(
            actions
                .iter()
                .filter(|action| matches!(action, Action::ModalSubmit))
                .count(),
            1
        );
        assert!(actions.iter().all(|action| !matches!(action, Action::Send)));
    }

    #[test]
    fn modal_clears_underlying_transcript_in_its_entire_rectangle() {
        let mut backend = slt::TestBackend::new(110, 38);
        let mut state = SurfaceState::new();
        let view = ui::View {
            messages: vec![ui::MessageView {
                key: "underlay".into(),
                text: (0..40)
                    .map(|_| "▒".repeat(90))
                    .collect::<Vec<_>>()
                    .join("\n"),
                ..ui::MessageView::default()
            }],
            ..ui::View::default()
        };
        let modal = ui::Modal {
            title: "Opaque modal".into(),
            detail: "Visible controls".into(),
            ..ui::Modal::default()
        };
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        let mut rect = None;
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&modal),
                    &mut state,
                );
                rect = ctx.measured_layout_rect("t3oxide.modal");
            });
        }
        let rect = rect.expect("modal has settled geometry");
        for y in rect.y..rect.y + rect.height {
            for x in rect.x..rect.x + rect.width {
                assert_ne!(
                    backend.buffer().get(x, y).symbol.as_str(),
                    "▒",
                    "underlay at {x},{y}"
                );
            }
        }
    }

    #[test]
    fn graph_mouse_navigation_retains_content_and_exact_source() {
        use slt::{EventBuilder, TestBackend};
        let mut state = SurfaceState::new();
        state.attention_mode = true;
        let source = Source {
            profile: 2,
            thread: "source-thread".into(),
            environment: "native-environment".into(),
            environment_label: "Environment".into(),
            project_id: "project-id".into(),
            project: "Project".into(),
        };
        state.questions.push(AttentionItem {
            key: "source/revision/question".into(),
            source: source.clone(),
            title: "Choose target".into(),
            summary: "Where should the build run?".into(),
            supporting_markdown: "# Evidence\n- Scoped native request".into(),
            request: serde_json::json!({"id":"request","revision":"exact"}),
            ..AttentionItem::default()
        });
        state.questions.push(AttentionItem {
            key: "remote/question".into(),
            source: Source {
                environment: "remote-environment".into(),
                environment_label: "Remote".into(),
                ..source.clone()
            },
            title: "Remote target".into(),
            ..AttentionItem::default()
        });
        let mut backend = TestBackend::new(110, 38);
        let view = ui::View::default();
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.assert_contains("Remote");
        let (x, y) = backend
            .find_text("Choose target")
            .expect("native question is visible");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.render(|ctx| {
            ctx.focus_by_name("question:remote/question");
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Tab).build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        assert!(!state.graph_global);
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        let (x, y) = backend
            .find_text("↑ All projects")
            .expect("same navigation region offers global return");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(state.graph_global);
        assert_eq!(
            state.selected_project.as_deref(),
            Some(source.project_key().as_str())
        );
        assert_eq!(
            state.selected_question.as_deref(),
            Some("source/revision/question")
        );
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.assert_contains("Evidence");
        let (x, y) = backend
            .find_text("Open in A")
            .expect("source action visible beside supporting content");
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(
            actions
                .iter()
                .any(|action| matches!(action, Action::OpenSource(actual) if actual == &source))
        );
        assert!(!state.attention_mode);
    }

    #[test]
    fn compact_modes_keep_primary_controls_visible() {
        use slt::TestBackend;
        let mut backend = TestBackend::new(60, 16);
        let mut state = SurfaceState::new();
        state.active_key = "environment/thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("draft", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.assert_contains("Prompt");
        backend.assert_contains("Send");
        state.attention_mode = true;
        state.questions.push(AttentionItem {
            key: "question".into(),
            source: Source {
                project_id: "project".into(),
                project: "Project".into(),
                environment: "environment".into(),
                environment_label: "Env".into(),
                ..Source::default()
            },
            title: "Native question".into(),
            summary: "Choose a build target".into(),
            ..AttentionItem::default()
        });
        state.selected_question = Some("question".into());
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.assert_contains("Questions");
        backend.assert_contains("Projects");
        backend.assert_contains("Answer");
        backend.assert_contains("Open in A");
    }

    #[test]
    fn changed_or_removed_question_retains_context_without_answering() {
        use slt::{EventBuilder, TestBackend};
        let question = AttentionItem {
            key: "env/thread/request".into(),
            source: Source {
                environment: "env".into(),
                thread: "thread".into(),
                project_id: "project".into(),
                project: "Project".into(),
                ..Source::default()
            },
            title: "Choose target".into(),
            summary: "Original question wording".into(),
            supporting_markdown: "Retained native evidence".into(),
            supporting_source_key: "env/thread/item".into(),
            supporting_revision: "item-revision".into(),
            supporting_truncated: true,
            request: serde_json::json!({"id":"request", "question":"Original question wording"}),
        };
        let mut state = SurfaceState::new();
        state.attention_mode = true;
        state.projects.push(Source {
            project_id: "idle".into(),
            project: "Idle project".into(),
            environment: "env".into(),
            ..Source::default()
        });
        state.questions.push(question.clone());
        state.selected_question = Some(question.key.clone());
        let mut backend = TestBackend::new(110, 38);
        let view = ui::View::default();
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.assert_contains("Idle project");
        state.questions[0].summary = "Changed question wording".into();
        state.questions[0].request["question"] = Value::String("Changed question wording".into());
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.assert_contains("Source changed");
        backend.assert_contains("Original question wording");
        backend.assert_contains("Retained native evidence");
        backend.assert_contains("Excerpt");
        let (x, y) = backend
            .find_text("Answer")
            .expect("disabled answer stays visible");
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(
            actions
                .iter()
                .all(|action| !matches!(action, Action::Answer(_)))
        );
        state.questions.clear();
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.assert_contains("Source unavailable");
        backend.assert_contains("Retained native evidence");
        let (x, y) = backend
            .find_text("Open in A")
            .expect("retained source remains openable");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(actions.iter().any(
            |action| matches!(action, Action::OpenSource(source) if source == &question.source)
        ));
    }

    #[test]
    fn editor_owns_enter_completion_and_send_across_settled_frames() {
        use slt::{EventBuilder, TestBackend};
        let mut backend = TestBackend::new(110, 38);
        let mut state = SurfaceState::new();
        state.active_key = "environment/thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("draft", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.render(|ctx| {
            ctx.focus_by_name("composer");
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        let mut actions = Vec::new();
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        assert_eq!(editor.lines().join("\n"), "draft\n");
        assert!(actions.iter().all(|action| !matches!(action, Action::Send)));
        for (code, modifiers) in [
            (KeyCode::Enter, KeyModifiers::SUPER),
            (KeyCode::Enter, KeyModifiers::CONTROL),
            (KeyCode::Enter, KeyModifiers::SHIFT),
            (
                KeyCode::Enter,
                KeyModifiers(KeyModifiers::SUPER.0 | KeyModifiers::SHIFT.0),
            ),
            (KeyCode::Char(']'), KeyModifiers::CONTROL),
            (KeyCode::Char('\\'), KeyModifiers::CONTROL),
            (KeyCode::Char('5'), KeyModifiers::CONTROL),
            (KeyCode::Char('4'), KeyModifiers::CONTROL),
        ] {
            backend.run_with_events(
                EventBuilder::new()
                    .key_with(code.clone(), modifiers)
                    .build(),
                |ctx| {
                    actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                },
            );
            assert_eq!(
                editor.lines().join("\n"),
                "draft\n",
                "{code:?} {modifiers:?} inserted text"
            );
            assert_eq!(
                actions
                    .iter()
                    .filter(|action| matches!(action, Action::Send))
                    .count(),
                1,
                "{code:?} {modifiers:?}"
            );
        }
        backend.run_with_events(
            EventBuilder::new()
                .key_with(KeyCode::Char('s'), KeyModifiers::CONTROL)
                .build(),
            |ctx| {
                actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        assert!(actions.iter().any(|action| matches!(action, Action::Stop)));
        assert!(actions.iter().all(|action| !matches!(action, Action::Send)));
        editor = ui::editor("", "violet", "");
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.run_with_events(EventBuilder::new().key('/').build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.run_with_events(
            EventBuilder::new()
                .key('m')
                .key('o')
                .key('d')
                .key('e')
                .key('l')
                .build(),
            |ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        assert_eq!(editor.lines().join("\n"), "/model");
        assert!(
            actions
                .iter()
                .any(|action| matches!(action, Action::Command(command) if command == "model"))
        );
        assert!(actions.iter().all(|action| !matches!(action, Action::Send)));
        editor = ui::editor("", "violet", "");
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.run_with_events(EventBuilder::new().paste("/model").build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        assert_eq!(editor.lines().join("\n"), "/model\n");
        assert!(
            actions
                .iter()
                .all(|action| !matches!(action, Action::Command(_) | Action::Send))
        );
    }
}
