//! SLT owns active terminal rendering, widget focus and editor input (A1–A5, B1–B3).
//! Controller actions retain native source identities; a widget never acknowledges a write.

mod attention;
mod dialogs;
mod navigator;

use crate::{domain, reports, ui};
use ratatui_textarea::TextArea;
use serde_json::Value;
use slt::{
    Border, ButtonVariant, Color, Context, KeyCode, KeyModifiers, Response, ScrollState,
    SpinnerState, SplitPaneState, TabsState, TextareaState, Theme, WidgetColors, palette::tailwind,
};
use std::collections::{BTreeMap, HashMap};
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

/// Read-only native content, qualified by its original source and revision.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ContentItem {
    pub key: String,
    pub source: Source,
    pub title: String,
    pub kind: String,
    pub markdown: String,
    pub revision: String,
    pub truncated: bool,
    /// Original serialized native row, shared across per-frame view snapshots.
    pub full_source: std::sync::Arc<[u8]>,
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
    CaptureContent {
        key: String,
        revision: String,
        source: Source,
    },
    ContextSource {
        source: Source,
        resume: bool,
    },
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
    /// Controller-owned delivery phases keyed by the complete native request source hash.
    pub answer_delivery: BTreeMap<String, String>,
    pub contents: Vec<ContentItem>,
    pub dependency_status: String,
    pub(crate) direct_effort_option: bool,
    pub attention_mode: bool,
    pub internal_tabs: bool,
    pub show_context: bool,
    pub(crate) focus: Option<ui::Focus>,
    pub selected_project: Option<String>,
    pub selected_question: Option<String>,
    pub graph_global: bool,
    composer: TextareaState,
    composer_key: String,
    tab_editors: HashMap<String, TextareaState>,
    tabs_control: TabsState,
    tabs_start: usize,
    pointer_tab_activation: bool,
    modal_editor: TextareaState,
    modal_stamp: Option<u64>,
    modal_selected: usize,
    navigator: navigator::State,
    sidebar_hidden: Option<bool>,
    explorer_return_focus: Option<String>,
    compact_return_focus: Option<String>,
    navigation_split: SplitPaneState,
    sidebar_width: Option<u32>,
    applied_layout: Option<ui::LayoutMode>,
    viewport: Option<(u32, u32)>,
    focused_control: Option<String>,
    last_attention_mode: bool,
    conversation_focus: Option<String>,
    attention_focus: Option<String>,
    context_expanded: bool,
    attention: attention::State,
    dialog: dialogs::State,
    overlay_open: bool,
    transcript: ScrollState,
    pub(crate) transcript_layout: Option<ui::TranscriptLayout>,
    pub(crate) transcript_jump: Option<(String, String, usize)>,
    controller_scroll: Option<(usize, bool)>,
    context_scroll: ScrollState,
    command_selected: usize,
    command_escape: Option<String>,
    command_open: bool,
    pub retained_question: Option<AttentionItem>,
    pub loading_question: Option<String>,
    reports: reports::Cache,
    spinner: SpinnerState,
    bars: HashMap<&'static str, usize>,
    browsing_scope: String,
}

impl SurfaceState {
    pub(crate) fn selected_content(&self) -> Option<&ContentItem> {
        attention::selected_content(self)
    }

    pub(crate) fn selected_source(&self) -> Option<&Source> {
        attention::selected_source(self)
    }
    pub fn new() -> Self {
        Self {
            internal_tabs: true,
            graph_global: true,
            transcript: ScrollState::new().stick_to_bottom(true),
            navigation_split: SplitPaneState::new(0.27),
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
    dialogs::direct_dismissal_tail(ctx, state, modal.is_some());
    let geometry = (ctx.width(), ctx.height());
    let resized = state
        .viewport
        .replace(geometry)
        .is_some_and(|old| old != geometry);
    if resized {
        // SLT clears measured geometry on resize. Rebind the controller intent
        // before fresh bounds can turn a clamped offset into manual scrolling.
        state.controller_scroll = None;
    }
    let restore_focus = (resized
        && modal.is_none()
        && !state.overlay_open
        && state.composer_key == state.active_key
        && !ctx.raw_events().any(|event| {
            matches!(
                event,
                slt::Event::Key(_) | slt::Event::Mouse(_) | slt::Event::Paste(_)
            )
        }))
    .then(|| state.focused_control.clone())
    .flatten();
    let pointer_tab_activation = std::mem::take(&mut state.pointer_tab_activation);
    let retain_tab_focus = state.composer_key != state.active_key
        && !pointer_tab_activation
        && state.focused_control.as_deref() == Some("conversation-tabs");
    state.transcript_layout = None;
    state.overlay_open =
        modal.is_some() || state.context_expanded || attention::navigation_open(state);
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
        if !retain_tab_focus {
            ctx.focus_by_name(
                if !state.attention_mode && sidebar_visible(state) && geometry.0 < 74 {
                    "navigator"
                } else {
                    "composer"
                },
            );
        }
        ctx.request_frame();
    }
    sync_editor(&mut state.composer, composer);
    if modal.is_none() {
        let copy_draft = KeyModifiers(KeyModifiers::CONTROL.0 | KeyModifiers::ALT.0);
        let presses: Vec<_> = ctx
            .key_presses_when(true)
            .filter(|(_, key)| key.code == KeyCode::Char('c') && key.modifiers == copy_draft)
            .map(|(index, _)| index)
            .collect();
        for index in presses {
            // Claim the whole-draft command before textarea's selection-copy layer.
            ctx.consume_event(index);
            actions.push(Action::Command("copy-draft".into()));
        }
    }
    let width = ctx.width();
    let height = ctx.height();
    if state.applied_layout != Some(view.layout) {
        match view.layout {
            ui::LayoutMode::Focus => state.sidebar_hidden = Some(true),
            ui::LayoutMode::Wide => {
                state.sidebar_hidden = Some(false);
                state.show_context = true;
            }
            ui::LayoutMode::Split => state.sidebar_hidden = None,
        }
        state.applied_layout = Some(view.layout);
    }
    ctx.set_theme(surface_theme(&view.theme));
    state.browsing_scope = browsing_scope(view, state);
    ctx.disabled(modal.is_some() || state.context_expanded, |ctx| {
        ctx.container().w(width).h(height).col(|ctx| {
            draw_header(ctx, view, state, &mut actions);
            let footer_height = 1 + u32::from(!view.notice.is_empty());
            let body_height = height.saturating_sub(1 + footer_height).max(1);
            let context_side =
                state.show_context && width >= 112 && view.layout != ui::LayoutMode::Focus;
            let content_width = width.saturating_sub(if context_side { 27 } else { 0 });
            ctx.container().h(body_height).row(|ctx| {
                ctx.container().w(content_width).h(body_height).col(|ctx| {
                    if state.attention_mode {
                        attention::draw(ctx, view, state, content_width, body_height, &mut actions);
                    } else {
                        draw_conversations(
                            ctx,
                            view,
                            composer,
                            state,
                            content_width,
                            body_height,
                            &mut actions,
                        );
                    }
                });
                if context_side {
                    draw_context(ctx, view, state, 27, body_height, &mut actions);
                }
            });
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
                let warning = ctx.theme().warning;
                ctx.text(&view.notice).fg(warning).truncate();
            }
        });
    });
    if state.context_expanded && modal.is_none() {
        ctx.modal(|ctx| {
            draw_context(
                ctx,
                view,
                state,
                width.saturating_sub(2).max(1),
                height.saturating_sub(3).max(1),
                &mut actions,
            );
            if secondary_button(ctx, "Close").clicked || ctx.consume_key_code(KeyCode::Esc) {
                state.context_expanded = false;
                ctx.focus_by_name("toolbar");
                ctx.request_frame();
            }
        });
    }
    if let Some(modal) = modal {
        dialogs::draw(ctx, modal, modal_editor, state, &mut actions);
    } else {
        state.modal_stamp = None;
    }
    global_actions(
        ctx,
        modal.is_some() || state.context_expanded || attention::navigation_open(state),
        state,
        &mut actions,
    );
    if modal.is_none() {
        if retain_tab_focus {
            ctx.focus_by_name("conversation-tabs");
            ctx.request_frame();
        }
        if state.last_attention_mode != state.attention_mode {
            let target = if state.attention_mode {
                if state.sidebar_hidden == Some(false) && height >= 15 {
                    state.attention_focus.as_deref().unwrap_or("attention-list")
                } else {
                    "attention-navigation"
                }
            } else {
                state.conversation_focus.as_deref().unwrap_or("composer")
            };
            ctx.focus_by_name(target);
            state.last_attention_mode = state.attention_mode;
            ctx.request_frame();
        }
        if let Some(name) = restore_focus {
            // Responsive panes can add named controls before the old focus slot.
            // Re-resolve that name through SLT after the new layout is registered.
            let navigator_visible = sidebar_visible(state);
            let compact_picker = !state.attention_mode && navigator_visible && width < 74;
            let name = if compact_picker
                && matches!(
                    name.as_str(),
                    "composer" | "composer-tools" | "transcript" | "pane-divider"
                ) {
                state.compact_return_focus = Some(name);
                "navigator".into()
            } else if !compact_picker && !state.attention_mode {
                state.compact_return_focus.take().unwrap_or_else(|| {
                    if (name.starts_with("navigator") || name == "pane-divider")
                        && !navigator_visible
                    {
                        "composer".into()
                    } else {
                        name
                    }
                })
            } else {
                name
            };
            ctx.focus_by_name(&name);
            state.focused_control = Some(name);
            ctx.request_frame();
        } else {
            state.focused_control = ctx.focused_name().map(str::to_owned);
        }
        if let Some(name) = state.focused_control.as_deref() {
            if matches!(
                name,
                "composer" | "navigator" | "transcript" | "conversation-tabs"
            ) {
                state.conversation_focus = Some(name.to_owned());
            } else if matches!(
                name,
                "attention-list"
                    | "attention-tabs"
                    | "project-navigation"
                    | "attention-content"
                    | "attention-navigation"
            ) {
                state.attention_focus = Some(name.to_owned());
            }
        }
        // Buttons and content outside navigation act on the displayed source.
        // Preserve the invoking context while a modal owns SLT focus.
        state.focus = Some(match state.focused_control.as_deref() {
            Some("navigator") => ui::Focus::Navigator,
            Some("transcript") => ui::Focus::Transcript,
            Some("context") => ui::Focus::Inspector,
            _ => ui::Focus::Composer,
        });
    }
    actions
}

fn surface_theme(name: &str) -> Theme {
    let mut theme = Theme::dark();
    match name.to_ascii_lowercase().as_str() {
        "terminal" => {
            theme.bg = Color::Reset;
            theme.text = Color::Reset;
            theme.text_dim = Color::DarkGray;
            theme.border = Color::DarkGray;
        }
        "cyan" => {
            theme.primary = tailwind::CYAN.c300;
            theme.accent = tailwind::CYAN.c300;
        }
        "lime" => {
            theme.primary = tailwind::LIME.c300;
            theme.accent = tailwind::LIME.c300;
        }
        "light" => theme = Theme::light(),
        "nord" => theme = Theme::nord(),
        _ => {
            theme.primary = tailwind::VIOLET.c300;
            theme.accent = tailwind::VIOLET.c300;
        }
    }
    if theme.is_dark {
        theme.border = tailwind::SLATE.c700;
    }
    theme.selected_bg = theme.primary;
    theme.selected_fg = if theme.is_dark {
        tailwind::SLATE.c950
    } else {
        Color::White
    };
    theme
}

fn browsing_scope(view: &ui::View, state: &SurfaceState) -> String {
    let Some((_, scope)) = view.inspector.iter().find(|(key, _)| key == "Browsing") else {
        return "All sources".into();
    };
    for source in &state.projects {
        let identity = format!("{} / {}", source.environment, source.project_id);
        if let Some(suffix) = scope.strip_prefix(&identity)
            && (suffix.is_empty() || suffix.starts_with(" · "))
        {
            return format!("{} · {}{suffix}", source.environment_label, source.project);
        }
    }
    scope.clone()
}

fn secondary_button(ctx: &mut Context, label: &str) -> Response {
    let colors = WidgetColors::new()
        .fg(ctx.theme().surface_text)
        .bg(ctx.theme().surface)
        .accent(ctx.theme().primary);
    ctx.button(label)
        .variant(ButtonVariant::Primary)
        .colors(&colors)
        .show()
}

fn primary_button(ctx: &mut Context, label: &str, enabled: bool) -> Response {
    ctx.disabled(!enabled, |ctx| {
        if enabled {
            let colors = WidgetColors::new().fg(ctx.theme().selected_fg);
            ctx.button(label)
                .variant(ButtonVariant::Primary)
                .colors(&colors)
                .show()
        } else {
            secondary_button(ctx, label)
        }
    })
}

/// One Tab stop for a compact action group; arrows choose, Enter or click acts.
fn action_bar(
    ctx: &mut Context,
    name: &str,
    cursor: &mut usize,
    items: &[(&str, bool, bool)],
) -> Option<usize> {
    if items.is_empty() {
        return None;
    }
    *cursor = (*cursor).min(items.len() - 1);
    let focused = ctx.register_focusable_named(name);
    let _ = ctx.register_focusable();
    let keys: Vec<_> = ctx
        .key_presses_when(focused)
        .map(|(i, k)| (i, k.clone()))
        .collect();
    let mut activated = None;
    for (i, key) in keys {
        match key.code {
            KeyCode::Left => *cursor = cursor.saturating_sub(1),
            KeyCode::Right => *cursor = (*cursor + 1).min(items.len() - 1),
            KeyCode::Home => *cursor = 0,
            KeyCode::End => *cursor = items.len() - 1,
            KeyCode::Enter | KeyCode::Char(' ') => {
                if items[*cursor].2 {
                    activated = Some(*cursor);
                }
            }
            _ => continue,
        }
        ctx.consume_event(i);
        ctx.request_frame();
    }
    let theme = *ctx.theme();
    ctx.container().wrap().gap(1).row_gap(0).row(|ctx| {
        for (i, (label, selected, enabled)) in items.iter().enumerate() {
            let response = ctx.interaction();
            ctx.text(format!(" {label} "))
                .fg(if !enabled {
                    theme.text_dim
                } else if *selected {
                    theme.selected_fg
                } else {
                    theme.text
                })
                .bg(if *selected {
                    theme.selected_bg
                } else {
                    theme.surface
                })
                .with_if(focused && *cursor == i, |text| {
                    text.fg(theme.selected_fg).bg(theme.primary).underline();
                })
                .with_if(!enabled, |text| {
                    text.dim();
                });
            if response.clicked && *enabled {
                *cursor = i;
                activated = Some(i);
                ctx.focus_by_name(name);
                ctx.request_frame();
            }
        }
    });
    activated
}

/// One focus owner, with a persistent active tab distinct from its keyboard edge.
fn quiet_tabs(ctx: &mut Context, tabs: &mut TabsState) -> Response {
    if tabs.labels.is_empty() {
        return Response::default();
    }
    tabs.selected = tabs.selected.min(tabs.labels.len() - 1);
    let before = tabs.selected;
    let focused = ctx.register_focusable();
    let keys: Vec<_> = ctx
        .key_presses_when(focused)
        .map(|(i, key)| (i, key.code.clone()))
        .collect();
    for (event, key) in keys {
        match key {
            KeyCode::Left => {
                tabs.selected = (tabs.selected + tabs.labels.len() - 1) % tabs.labels.len()
            }
            KeyCode::Right => tabs.selected = (tabs.selected + 1) % tabs.labels.len(),
            _ => continue,
        }
        ctx.consume_event(event);
        ctx.request_frame();
    }
    let theme = *ctx.theme();
    let mut response = Response {
        focused,
        ..Response::default()
    };
    ctx.row(|ctx| {
        for (index, label) in tabs.labels.iter().enumerate() {
            if index > 0 {
                ctx.text("│").fg(theme.border);
            }
            let hit = ctx.interaction();
            ctx.text(format!(
                " {}{label} ",
                if index == tabs.selected { "• " } else { "" }
            ))
            .fg(if index == tabs.selected {
                theme.text
            } else {
                theme.surface_text
            })
            .bg(if index == tabs.selected {
                theme.surface_hover
            } else {
                theme.surface
            })
            .with_if(focused && index == tabs.selected, |text| {
                text.underline().fg(theme.primary);
            });
            if hit.clicked {
                tabs.selected = index;
                response.clicked = true;
                ctx.request_frame();
            }
        }
    });
    response.changed = tabs.selected != before;
    response
}

fn draw_header(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    let width = ctx.width();
    let primary = ctx.theme().primary;
    let surface = ctx.theme().surface;
    ctx.container().h(1).bg(surface).row(|ctx| {
        if width >= 80 {
            ctx.text("T3oxide ").fg(primary);
        }
        let sidebar = sidebar_visible(state);
        let items = [
            ("☰", sidebar, true),
            ("A", !state.attention_mode, true),
            ("B", state.attention_mode, true),
            ("Search", false, true),
            (if width < 60 { "⌘" } else { "Commands" }, false, true),
            (
                if width < 50 { "ⓘ" } else { "Context" },
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
                toggle_sidebar(ctx, state);
                ctx.request_frame();
            }
            Some(1) => state.attention_mode = false,
            Some(2) => state.attention_mode = true,
            Some(3) => actions.push(Action::Command("search".into())),
            Some(4) => actions.push(Action::Command("palette".into())),
            Some(5) => {
                toggle_context(state, width);
                ctx.request_frame();
            }
            _ => {}
        }
    });
}

fn sidebar_visible(state: &SurfaceState) -> bool {
    // Without an attachment, A falls back to its existing source picker.
    !state.sidebar_hidden.unwrap_or(
        state.attention_mode
            || !state.active_key.is_empty()
            || state.applied_layout == Some(ui::LayoutMode::Focus),
    )
}

fn toggle_sidebar(ctx: &mut Context, state: &mut SurfaceState) {
    let closing = sidebar_visible(state);
    state.sidebar_hidden = Some(closing);
    if !state.attention_mode {
        let target = if closing {
            state.compact_return_focus = None;
            state
                .explorer_return_focus
                .take()
                .unwrap_or_else(|| "composer".into())
        } else {
            state.explorer_return_focus = ctx.focused_name().map(str::to_owned);
            "navigator".into()
        };
        ctx.focus_by_name(&target);
    }
}

fn toggle_context(state: &mut SurfaceState, width: u32) {
    if width < 112 || state.applied_layout == Some(ui::LayoutMode::Focus) {
        state.context_expanded = !state.context_expanded;
    } else {
        state.show_context = !state.show_context;
    }
}

fn draw_context(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let focus_color = ctx.theme().primary;
    ctx.group("t3oxide.context")
        .w(width)
        .h(height)
        .border(Border::Rounded)
        .focus_within_border_fg(focus_color)
        .title("Context")
        .col(|ctx| {
            ctx.register_focusable_named("context");
            ctx.scrollable(&mut state.context_scroll)
                .grow(1)
                .col(|ctx| {
                    let selected_source = if state.attention_mode {
                        attention::selected_source(state)
                    } else {
                        state.source.as_ref()
                    };
                    if let Some(source) = selected_source {
                        source_tags(ctx, source);
                        if !source.thread.is_empty() {
                            ctx.text(&source.thread).dim().wrap();
                        }
                    }
                    if state.attention_mode
                        && let Some(content) = attention::selected_content(state)
                    {
                        for (label, value) in [
                            ("Kind", content.kind.clone()),
                            ("Item", content.key.replace('\0', " / ")),
                            ("Revision", content.revision.clone()),
                        ] {
                            ctx.text(label).dim();
                            ctx.text(value).wrap();
                        }
                        if content.truncated {
                            ctx.text("Excerpt").dim();
                        }
                    }
                    if !state.attention_mode {
                        for (label, value) in &view.inspector {
                            if !value.is_empty() {
                                ctx.row(|ctx| {
                                    ctx.text(label).dim().w(9);
                                    ctx.text(value).wrap().grow(1);
                                });
                            }
                        }
                    }
                });
            let items = if width < 40 {
                vec![("Copy ID", false, true), ("Records", false, true)]
            } else {
                vec![
                    ("Copy ID", false, true),
                    (
                        "Records",
                        false,
                        !state.attention_mode || attention::selected_source(state).is_some(),
                    ),
                    (
                        "Capture",
                        false,
                        !state.attention_mode || attention::selected_content(state).is_some(),
                    ),
                    (
                        "Resume",
                        false,
                        !state.attention_mode || attention::selected_source(state).is_some(),
                    ),
                ]
            };
            match action_bar(
                ctx,
                "context-tools",
                state.bars.entry("context-tools").or_default(),
                &items,
            ) {
                Some(0) => {
                    if state.attention_mode {
                        if let Some(source) = attention::selected_source(state) {
                            ctx.copy_to_clipboard(source.thread.clone());
                        }
                    } else {
                        actions.push(Action::CopyId);
                    }
                }
                Some(1) => context_source_action(state, false, actions),
                Some(2) => {
                    if state.attention_mode {
                        if let Some(item) = attention::selected_content(state) {
                            actions.push(Action::CaptureContent {
                                key: item.key.clone(),
                                revision: item.revision.clone(),
                                source: item.source.clone(),
                            });
                        }
                    } else {
                        actions.push(Action::Command("context-save".into()));
                    }
                }
                Some(3) => context_source_action(state, true, actions),
                _ => {}
            }
        });
}

fn context_source_action(state: &SurfaceState, resume: bool, actions: &mut Vec<Action>) {
    if state.attention_mode {
        if let Some(source) = attention::selected_source(state) {
            actions.push(Action::ContextSource {
                source: source.clone(),
                resume,
            });
        }
    } else {
        actions.push(Action::Command(
            if resume { "resume" } else { "context" }.into(),
        ));
    }
}

fn draw_tabs(ctx: &mut Context, state: &mut SurfaceState, width: u32, actions: &mut Vec<Action>) {
    if state.tabs.is_empty() {
        return;
    }
    let active = state
        .tabs
        .iter()
        .position(|tab| tab.key == state.active_key)
        .unwrap_or(0);
    let capacity = ((width.saturating_sub(16)) / 24).max(1) as usize;
    if active < state.tabs_start {
        state.tabs_start = active;
    }
    if active >= state.tabs_start + capacity {
        state.tabs_start = active + 1 - capacity;
    }
    state.tabs_start = state.tabs_start.min(state.tabs.len().saturating_sub(1));
    let start = state.tabs_start;
    let end = (start + capacity).min(state.tabs.len());
    let label_width = (width.saturating_sub(16) / (end - start).max(1) as u32)
        .saturating_sub(5)
        .clamp(4, 32);
    state.tabs_control.labels = state.tabs[start..end]
        .iter()
        .map(|tab| {
            let title = if tab.title.is_empty() || tab.title == tab.source.thread {
                if tab.source.project.is_empty() {
                    "Conversation"
                } else {
                    &tab.source.project
                }
            } else {
                &tab.title
            };
            elide(title, label_width as usize)
        })
        .collect();
    state.tabs_control.selected = active.saturating_sub(start);
    ctx.container().h(1).row(|ctx| {
        let previous = ctx.interaction();
        ctx.text(" ‹ ").with_if(start == 0, |text| {
            text.dim();
        });
        if previous.clicked && start > 0 {
            actions.push(Action::SelectTab(state.tabs[start - 1].key.clone()));
        }
        let focused = ctx.register_focusable_named("conversation-tabs");
        let keys: Vec<_> = ctx
            .key_presses_when(focused)
            .map(|(i, key)| (i, key.code.clone()))
            .collect();
        let mut target = active;
        for (i, key) in keys {
            match key {
                KeyCode::Left => target = target.saturating_sub(1),
                KeyCode::Right => target = (target + 1).min(state.tabs.len() - 1),
                KeyCode::Home => target = 0,
                KeyCode::End => target = state.tabs.len() - 1,
                _ => continue,
            }
            ctx.consume_event(i);
        }
        if target != active {
            actions.push(Action::SelectTab(state.tabs[target].key.clone()));
        }
        let response = quiet_tabs(ctx, &mut state.tabs_control);
        if response.changed {
            actions.push(Action::SelectTab(
                state.tabs[start + state.tabs_control.selected].key.clone(),
            ));
        }
        let next = ctx.interaction();
        ctx.text(" › ").with_if(end == state.tabs.len(), |text| {
            text.dim();
        });
        if next.clicked && end < state.tabs.len() {
            actions.push(Action::SelectTab(state.tabs[end].key.clone()));
        }
        if response.clicked
            || (previous.clicked && start > 0)
            || (next.clicked && end < state.tabs.len())
        {
            // SLT resolves named focus next frame, when the new source is restored.
            state.pointer_tab_activation = true;
            ctx.focus_by_name("composer");
            ctx.request_frame();
        }
        let close = ctx.interaction();
        ctx.text(" × ").dim();
        if close.clicked {
            actions.push(Action::CloseTab(state.active_key.clone()));
        }
        let add = ctx.interaction();
        ctx.text(" + ").dim();
        if add.clicked {
            actions.push(Action::Command("search".into()));
        }
    });
    if ctx.focused_name() == Some("conversation-tabs") && ctx.consume_key_code(KeyCode::Delete) {
        actions.push(Action::CloseTab(state.active_key.clone()));
    }
}

fn elide(text: &str, width: usize) -> String {
    use unicode_width::UnicodeWidthStr;
    if text.width() <= width {
        return text.to_owned();
    }
    let mut out = String::new();
    for grapheme in text.graphemes(true) {
        if out.width() + grapheme.width() >= width {
            break;
        }
        out.push_str(grapheme);
    }
    out.push('…');
    out
}

/// Fit display text using SLT's own word-wrap measurement, preserving graphemes.
fn elide_lines(ctx: &Context, text: &str, width: u32, rows: u32) -> String {
    if width == 0 || rows == 0 {
        return String::new();
    }
    let columns = Some(u16::try_from(width).unwrap_or(u16::MAX));
    if u32::from(ctx.measure_text(text, columns).1) <= rows {
        return text.to_owned();
    }
    let mut shown = elide(text, width.saturating_mul(rows) as usize);
    if !shown.ends_with('…') {
        shown.push('…');
    }
    while u32::from(ctx.measure_text(&shown, columns).1) > rows {
        shown.pop();
        let end = shown
            .grapheme_indices(true)
            .next_back()
            .map_or(0, |(end, _)| end);
        shown.truncate(end);
        shown.truncate(shown.trim_end().len());
        shown.push('…');
    }
    shown
}

fn draw_conversations(
    ctx: &mut Context,
    view: &ui::View,
    composer: &mut TextArea<'static>,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    let tabs_height = u32::from(!state.tabs.is_empty());
    draw_tabs(ctx, state, width, actions);
    let height = height.saturating_sub(tabs_height);
    let sidebar = sidebar_visible(state);
    if sidebar && width >= 74 {
        let attached_key = state.active_key.clone();
        let mut nav = std::mem::take(&mut state.navigator);
        let mut split = std::mem::take(&mut state.navigation_split);
        // Keep the preferred cell width separate from a temporary resize bound.
        // Only a native divider gesture changes that preference.
        let available = width.saturating_sub(1);
        let min_sidebar = 27;
        let max_sidebar = available.saturating_sub(38).max(min_sidebar);
        let nav_width = state
            .sidebar_width
            .unwrap_or(30)
            .clamp(min_sidebar, max_sidebar);
        split.min_ratio = f64::from(min_sidebar) / f64::from(available);
        // SLT quantizes split ratios to 1000 grow weights, then floors the
        // first pane's cell share. Round the weight up so its fixed-width
        // navigator is not clipped by a one-cell-smaller allocation.
        const SPLIT_GROW_SCALE: f64 = 1000.0;
        let ratio = (f64::from(nav_width) * SPLIT_GROW_SCALE / f64::from(available)).ceil()
            / SPLIT_GROW_SCALE;
        split.set_ratio(ratio);
        let main_width = width.saturating_sub(nav_width + 1);
        let colors: Vec<_> = view
            .rows
            .iter()
            .map(|row| {
                let source = state.thread_sources.get(&row.key);
                let (env, project) = row
                    .subtitle
                    .split_once(" / ")
                    .unwrap_or((&row.subtitle, ""));
                (
                    identity_color(source.map_or(env, |source| source.environment.as_str())),
                    identity_color(&source.map_or_else(|| project.to_owned(), Source::project_key)),
                )
            })
            .collect();
        let mut nav_actions = Vec::new();
        let spinner = state.spinner.clone();
        if state.overlay_open {
            // SLT's draggable handle uses deferred raw drawing, after modal paint.
            // Keep the settled pane geometry but compose this divider as normal text.
            ctx.container().w(width).h(height).row(|ctx| {
                navigator::draw(
                    ctx,
                    view,
                    navigator::Presentation {
                        colors: &colors,
                        attached_key: &attached_key,
                    },
                    &mut nav,
                    reports::Geometry {
                        width: nav_width,
                        height,
                    },
                    &spinner,
                    &mut nav_actions,
                );
                ctx.container().w(1).h(height).col(|ctx| {
                    for _ in 0..height {
                        ctx.text("│").dim();
                    }
                });
                draw_conversation(ctx, view, composer, state, main_width, height, actions);
            });
        } else {
            ctx.register_focusable_named("pane-divider");
            let response = ctx.split_pane(
                &mut split,
                |ctx| {
                    navigator::draw(
                        ctx,
                        view,
                        navigator::Presentation {
                            colors: &colors,
                            attached_key: &attached_key,
                        },
                        &mut nav,
                        reports::Geometry {
                            width: nav_width,
                            height,
                        },
                        &spinner,
                        &mut nav_actions,
                    );
                },
                |ctx| {
                    draw_conversation(ctx, view, composer, state, main_width, height, actions);
                },
            );
            if response.changed {
                // The grow-weight rounding is paint-only; keyboard deltas keep
                // their original cell rounding and never enter the preference.
                let width = (f64::from(nav_width) + f64::from(available) * (response.ratio - ratio))
                    .round() as u32;
                state.sidebar_width = Some(width.clamp(min_sidebar, max_sidebar));
                ctx.request_frame();
            }
        }
        state.navigator = nav;
        state.navigation_split = split;
        actions.extend(nav_actions);
    } else if sidebar {
        let colors = vec![(tailwind::CYAN.c300, tailwind::VIOLET.c300); view.rows.len()];
        navigator::draw(
            ctx,
            view,
            navigator::Presentation {
                colors: &colors,
                attached_key: &state.active_key,
            },
            &mut state.navigator,
            reports::Geometry { width, height },
            &state.spinner,
            actions,
        );
        if actions
            .iter()
            .any(|action| matches!(action, Action::Open(_)))
        {
            state.sidebar_hidden = Some(true);
            state.explorer_return_focus = None;
            state.compact_return_focus = None;
            ctx.focus_by_name("composer");
            ctx.request_frame();
        }
    } else {
        draw_conversation(ctx, view, composer, state, width, height, actions);
    }
}

fn draw_conversation(
    ctx: &mut Context,
    view: &ui::View,
    composer: &mut TextArea<'static>,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    ctx.group("t3oxide.conversation")
        .w(width)
        .h(height)
        .col(|ctx| {
            let editor_width = width.saturating_sub(3).max(1);
            state.composer.wrap_width = Some(editor_width);
            let rows = editor_rows(&mut state.composer, height);
            let completion_rows = if state.command_open {
                completion_indices(&state.composer.value(), state.command_escape.as_deref())
                    .len()
                    .min(4) as u32
            } else {
                0
            }
            .min(height.saturating_sub(rows + 4));
            let composer_height = rows + 3 + completion_rows;
            draw_transcript(
                ctx,
                view,
                state,
                width,
                height.saturating_sub(composer_height),
                actions,
            );
            draw_composer(
                ctx,
                view,
                composer,
                state,
                ComposerLayout {
                    width,
                    rows,
                    completions: completion_rows,
                },
                actions,
            );
        });
}

fn editor_rows(editor: &mut TextareaState, height: u32) -> u32 {
    let lines = u32::try_from(editor.visual_line_count()).unwrap_or(u32::MAX);
    let max = height
        .saturating_sub(5)
        .min((height / 3).clamp(1, 12))
        .max(1);
    lines.clamp(2.min(max), max)
}

fn draw_transcript(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    width: u32,
    height: u32,
    actions: &mut Vec<Action>,
) {
    if height == 0 {
        return;
    }
    let bordered = height >= 4;
    let inset = u32::from(bordered) + u32::from(height >= 6);
    let inner_width = width.saturating_sub(2 + 2 * u32::from(bordered));
    let geometry = reports::Geometry {
        width: inner_width.max(1),
        height: height.saturating_sub(5).max(1),
    };
    let questions = format!("Questions {}", view.requests);
    let mut tools = vec![("Earlier", false, true), ("Latest", view.follow, true)];
    if view.requests > 0 {
        tools.push((questions.as_str(), false, true));
    }
    match action_bar(
        ctx,
        "transcript-tools",
        state.bars.entry("transcript-tools").or_default(),
        &tools,
    ) {
        Some(0) => actions.push(Action::Command("earlier".into())),
        Some(1) => {
            state.transcript.offset = state.transcript.max_offset();
            actions.push(Action::Command("latest".into()));
        }
        Some(2) => actions.push(Action::Command("requests".into())),
        _ => {}
    }
    if height == 1 {
        return;
    }
    let group = format!("t3oxide.transcript:{}", state.active_key);
    let measured = ctx.measured_layout_rect(&group);
    let geometry_changed =
        measured.is_some_and(|rect| rect.width != width || rect.height != height.saturating_sub(1));
    if geometry_changed {
        state.controller_scroll = None;
    }
    if state.controller_scroll != Some((view.transcript_scroll, view.follow)) {
        state.transcript.offset = if view.follow {
            usize::MAX
        } else {
            view.transcript_scroll
        };
    }
    let items: Option<Vec<_>> = measured.and_then(|viewport| {
        view.messages
            .iter()
            .map(|message| {
                let rect = ctx.measured_layout_rect(&transcript_message_group(message))?;
                Some((
                    message.key.clone(),
                    rect.y.saturating_sub(viewport.y + inset) as usize,
                    rect.height as usize,
                ))
            })
            .collect()
    });
    let jump = state
        .transcript_jump
        .as_ref()
        .and_then(|(key, revision, inner)| {
            let message = view.messages.iter().find(|message| {
                &message.key == key && &reports::content_revision(&message.text) == revision
            })?;
            let (_, offset, height) = items
                .as_ref()?
                .iter()
                .find(|(id, _, _)| id == &message.key)?;
            Some(offset.saturating_add((*inner).min(height.saturating_sub(1))))
        });
    let mut jumped = false;
    let mut keyboard_scrolled = false;
    let before_binding = state.transcript.offset;
    state.transcript.follow_focus = false;
    state.transcript.stick_to_bottom = view.follow && state.transcript_jump.is_none();
    ctx.group(&group)
        .w(width)
        .h(height.saturating_sub(1))
        .col(|ctx| {
            let focused = ctx.register_focusable_named("transcript");
            // A source/geometry rebind has no accepted bounds yet. Its old
            // focus slot must not deliver scrolling to the replacement view.
            let keys: Vec<_> = ctx
                .key_presses_when(focused && state.controller_scroll.is_some())
                .filter(|(_, key)| {
                    key.modifiers == KeyModifiers::NONE
                        && matches!(
                            key.code,
                            KeyCode::Up | KeyCode::Down | KeyCode::PageUp | KeyCode::PageDown
                        )
                })
                .map(|(index, key)| (index, key.code.clone()))
                .collect();
            for (index, code) in keys {
                state.transcript.offset =
                    state.transcript.offset.min(state.transcript.max_offset());
                let page = (state.transcript.viewport_height() as usize).max(1);
                match code {
                    KeyCode::Up => state.transcript.scroll_up(1),
                    KeyCode::Down => state.transcript.scroll_down(1),
                    KeyCode::PageUp => state.transcript.scroll_up(page),
                    KeyCode::PageDown => state.transcript.scroll_down(page),
                    _ => unreachable!("filtered transcript navigation key"),
                }
                keyboard_scrolled = true;
                state.transcript.stick_to_bottom = false;
                ctx.consume_event(index);
                ctx.request_frame();
            }
            let mut viewport = ctx.scrollable(&mut state.transcript);
            // Bind first so SLT can accept deferred adjustments and fresh bounds.
            // Then the explicit, source-qualified jump overrides that accepted offset.
            if let Some(target) = jump
                && state.transcript.viewport_height() > 0
            {
                state.transcript.offset = target.min(state.transcript.max_offset());
                viewport = viewport.scroll_offset(state.transcript.offset as u32);
                jumped = true;
            }
            let viewport = viewport
                .grow(1)
                .focus_within_border_fg(tailwind::VIOLET.c300)
                .px(1)
                .py(u32::from(height >= 6));
            let viewport = if bordered {
                viewport.border(Border::Rounded)
            } else {
                viewport
            };
            viewport.col(|ctx| {
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
                        ctx.text("");
                    });
                }
            });
        });
    if let (Some(items), Some(rect)) = (items, measured)
        && state.transcript.viewport_height() > 0
        && !geometry_changed
    {
        let top = state.transcript.offset;
        let max = state.transcript.max_offset();
        let follow = !keyboard_scrolled
            && !jumped
            && state.transcript_jump.is_none()
            && top == max
            && (view.follow || top > before_binding);
        // Feedback describes accepted state, so the next controller frame does
        // not rewrite SLT's pending adjustment with its previous input tuple.
        state.controller_scroll = Some((top, follow));
        state.transcript_layout = Some(ui::TranscriptLayout {
            items,
            top,
            follow,
            content: state.transcript.content_height() as usize,
            visible: state.transcript.viewport_height() as usize,
            max,
            width: u16::try_from(rect.width.saturating_sub(2 + 2 * u32::from(bordered)))
                .unwrap_or(u16::MAX),
        });
        if jumped {
            state.transcript_jump = None;
            ctx.request_frame();
        }
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

struct ComposerLayout {
    width: u32,
    rows: u32,
    completions: u32,
}

fn draw_composer(
    ctx: &mut Context,
    view: &ui::View,
    legacy: &mut TextArea<'static>,
    state: &mut SurfaceState,
    layout: ComposerLayout,
    actions: &mut Vec<Action>,
) {
    let ComposerLayout {
        width,
        rows,
        completions,
    } = layout;
    let focus_color = ctx.theme().primary;
    ctx.group("t3oxide.composer")
        .w(width)
        .border(Border::Rounded)
        .focus_within_border_fg(focus_color)
        .title("Prompt")
        .col(|ctx| {
            let indices = if state.command_open {
                completion_indices(&state.composer.value(), state.command_escape.as_deref())
            } else {
                Vec::new()
            };
            if !indices.is_empty() {
                let selected = state.command_selected.min(indices.len() - 1);
                let start = selected.saturating_sub(completions.saturating_sub(1) as usize);
                for (position, index) in indices
                    .iter()
                    .enumerate()
                    .skip(start)
                    .take(completions as usize)
                {
                    let command = &domain::ACTIONS[*index];
                    let response = ctx.interaction();
                    ctx.text(elide(
                        &format!(
                            " {} /{}",
                            if position == selected { "▸" } else { " " },
                            command.name
                        ),
                        width.saturating_sub(2) as usize,
                    ))
                    .with_if(position == selected, |text| {
                        text.fg(focus_color).bold();
                    });
                    if response.clicked {
                        actions.push(Action::Command(command.name.into()));
                        state.command_escape = Some(state.composer.value());
                        state.command_open = false;
                        ctx.focus_by_name("composer");
                        ctx.request_frame();
                    }
                }
            }
            state.composer.wrap_width = Some(width.saturating_sub(3).max(1));
            let focused = ctx.register_focusable_named("composer");
            let typed_slash = ctx.key_presses_when(focused).any(|(_, key)| {
                key.code == KeyCode::Char('/') && key.modifiers == KeyModifiers::NONE
            });
            composer_keys(ctx, focused, state, actions);
            if ctx.textarea(&mut state.composer, rows).changed {
                *legacy = ui::editor(&state.composer.value(), &view.theme, "Write a message…");
                state.command_selected = 0;
                if typed_slash && state.composer.value().starts_with('/') {
                    state.command_open = true;
                    state.command_escape = None;
                }
                actions.push(Action::DraftChanged);
                ctx.request_frame();
            }
            let can_send =
                !state.active_key.is_empty() && !state.composer.value().trim().is_empty();
            ctx.row(|ctx| {
                match action_bar(
                    ctx,
                    "composer-tools",
                    state.bars.entry("composer-tools").or_default(),
                    &[
                        ("Model", false, true),
                        ("Effort", false, true),
                        ("Stop", false, true),
                    ],
                ) {
                    Some(0) => actions.push(Action::Command("model".into())),
                    Some(1) => actions.push(Action::Command("effort".into())),
                    Some(2) => actions.push(Action::Stop),
                    _ => {}
                }
                ctx.spacer();
                ctx.register_focusable_named("composer-send");
                if primary_button(ctx, "Send", can_send).clicked {
                    actions.push(Action::Send);
                    ctx.focus_by_name("composer");
                    ctx.request_frame();
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
    let keys: Vec<_> = ctx
        .key_presses_when(focused)
        .map(|(index, key)| (index, key.clone()))
        .collect();
    for (index, key) in keys {
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
                    ctx.request_frame();
                }
                KeyCode::Up => {
                    state.command_selected = state.command_selected.saturating_sub(1);
                    consumed.push(index);
                    ctx.request_frame();
                }
                KeyCode::Down => {
                    state.command_selected = (state.command_selected + 1).min(completion.len() - 1);
                    consumed.push(index);
                    ctx.request_frame();
                }
                KeyCode::Esc => {
                    state.command_escape = Some(state.composer.value());
                    state.command_open = false;
                    consumed.push(index);
                    ctx.request_frame();
                }
                _ => {}
            }
        }
    }
    for index in consumed {
        ctx.consume_event(index);
    }
}

fn global_actions(
    ctx: &mut Context,
    modal: bool,
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
            KeyCode::Char('t') if control => Some(Action::Command("reviewed".into())),
            KeyCode::F(1) => Some(Action::Command("help".into())),
            KeyCode::F(2) if !modal => {
                state.attention_mode = !state.attention_mode;
                consumed.push(index);
                ctx.request_frame();
                None
            }
            KeyCode::F(3) if !modal => {
                toggle_sidebar(ctx, state);
                consumed.push(index);
                ctx.request_frame();
                None
            }
            KeyCode::F(4) if !modal => {
                toggle_context(state, ctx.width());
                consumed.push(index);
                ctx.request_frame();
                None
            }
            KeyCode::F(6) if !modal => {
                let mut panes = if state.attention_mode {
                    if state.sidebar_hidden == Some(false) && ctx.height() >= 15 {
                        vec!["attention-list", "project-navigation", "attention-content"]
                    } else {
                        vec!["attention-navigation", "attention-content"]
                    }
                } else {
                    let mut names = Vec::new();
                    if sidebar_visible(state) {
                        names.push("navigator");
                    }
                    if !sidebar_visible(state) || ctx.width() >= 74 {
                        names.extend(["transcript", "composer"]);
                    }
                    names
                };
                if state.show_context
                    && ctx.width() >= 112
                    && state.applied_layout != Some(ui::LayoutMode::Focus)
                {
                    panes.push("context");
                }
                let current = ctx
                    .focused_name()
                    .and_then(|name| panes.iter().position(|pane| *pane == name));
                let next = if key.modifiers.contains(KeyModifiers::SHIFT) {
                    current
                        .unwrap_or(0)
                        .checked_sub(1)
                        .unwrap_or(panes.len() - 1)
                } else {
                    current.map_or(0, |index| (index + 1) % panes.len())
                };
                ctx.focus_by_name(panes[next]);
                consumed.push(index);
                ctx.request_frame();
                None
            }
            KeyCode::Esc if modal => Some(Action::CloseModal),
            KeyCode::Esc if !state.attention_mode && sidebar_visible(state) && ctx.width() < 74 => {
                toggle_sidebar(ctx, state);
                consumed.push(index);
                ctx.request_frame();
                None
            }
            KeyCode::Esc => {
                ctx.focus_by_name(if state.attention_mode {
                    if state.sidebar_hidden == Some(false) && ctx.height() >= 15 {
                        "attention-list"
                    } else {
                        "attention-navigation"
                    }
                } else if !sidebar_visible(state) {
                    "composer"
                } else {
                    "navigator"
                });
                consumed.push(index);
                ctx.request_frame();
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
    let mut indices: Vec<_> = domain::ACTIONS
        .iter()
        .enumerate()
        .filter(|(_, action)| action.name.starts_with(&query))
        .map(|(index, _)| index)
        .collect();
    const PRIORITY: &[&str] = &[
        "new", "search", "model", "requests", "views", "refresh", "pin", "latest", "recover",
        "help",
    ];
    indices.sort_by_key(|index| {
        PRIORITY
            .iter()
            .position(|name| *name == domain::ACTIONS[*index].name)
            .unwrap_or(PRIORITY.len())
    });
    indices
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
pub(crate) mod tests {
    use super::*;

    struct Cells(slt::Buffer);

    impl slt::Backend for Cells {
        fn size(&self) -> (u32, u32) {
            (self.0.area.width, self.0.area.height)
        }
        fn buffer_mut(&mut self) -> &mut slt::Buffer {
            &mut self.0
        }
        fn flush(&mut self) -> std::io::Result<()> {
            Ok(())
        }
    }

    /// The public SLT frame path with a real geometry change and retained focus/layout state.
    pub(crate) struct ResizableBackend {
        cells: Cells,
        state: slt::AppState,
        config: slt::RunConfig,
    }

    impl ResizableBackend {
        pub(crate) fn new(width: u32, height: u32) -> Self {
            Self {
                cells: Cells(slt::Buffer::empty(slt::Rect::new(0, 0, width, height))),
                state: slt::AppState::new(),
                config: slt::RunConfig::default(),
            }
        }
        pub(crate) fn with_activation_boundaries(mut self) -> Self {
            self.config = self.config.activation_boundaries(true);
            self
        }
        pub(crate) fn render(&mut self, draw: impl FnMut(&mut Context)) {
            self.run_with_events(Vec::new(), draw);
        }
        pub(crate) fn run_with_events(
            &mut self,
            events: Vec<slt::Event>,
            mut draw: impl FnMut(&mut Context),
        ) {
            for event in &events {
                if let slt::Event::Resize(width, height) = *event {
                    self.cells.0.resize(slt::Rect::new(0, 0, width, height));
                }
            }
            self.cells.0.reset();
            slt::frame_owned(
                &mut self.cells,
                &mut self.state,
                &self.config,
                events,
                &mut draw,
            )
            .unwrap();
        }
        fn line(&self, y: u32) -> String {
            (0..self.cells.0.area.width)
                .map(|x| self.cells.0.get(x, y).symbol.as_str())
                .collect()
        }
        pub(crate) fn symbol(&self, x: u32, y: u32) -> &str {
            self.cells.0.get(x, y).symbol.as_str()
        }
        pub(crate) fn text(&self) -> String {
            (0..self.cells.0.area.height)
                .map(|y| self.line(y))
                .collect::<Vec<_>>()
                .join("\n")
        }
        pub(crate) fn find_text(&self, expected: &str) -> Option<(u32, u32)> {
            (0..self.cells.0.area.height).find_map(|y| {
                let line = self.line(y);
                line.find(expected).map(|byte| {
                    (
                        unicode_width::UnicodeWidthStr::width(&line[..byte]) as u32,
                        y,
                    )
                })
            })
        }
        pub(crate) fn assert_contains(&self, expected: &str) {
            assert!(
                self.find_text(expected).is_some(),
                "missing {expected:?}:\n{}",
                self.text()
            );
        }
        pub(crate) fn assert_not_contains(&self, expected: &str) {
            assert!(
                self.find_text(expected).is_none(),
                "unexpected {expected:?}:\n{}",
                self.text()
            );
        }
    }

    const PHYSICAL_PASTE_DRAFT: &str = concat!(
        "Review the release scope with the original request preserved. Keep the current source ",
        "and the next action visible while this sentence wraps inside the actual editor pane.\n",
        "Unicode: café 日本語.\n",
        "Long_token_abcdefghijklmnopqrstuvwxyz_0123456789_abcdefghijklmnopqrstuvwxyz_0123456789"
    );

    #[test]
    fn shared_control_roles_and_tabs_keep_focus_separate_from_persistent_selection() {
        let mut backend = slt::TestBackend::new(80, 12);
        let mut tabs = TabsState::new(vec!["First thread", "Second thread"]);
        let mut bar = 0;
        let mut focus = None;
        let mut activations = Vec::new();
        let mut callback = |ctx: &mut Context| {
            action_bar(
                ctx,
                "toolbar-controls",
                &mut bar,
                &[("Search", false, true), ("Commands", false, true)],
            );
            ctx.register_focusable_named("tabs");
            if quiet_tabs(ctx, &mut tabs).changed {
                activations.push(tabs.selected);
            }
            ctx.register_focusable_named("action");
            secondary_button(ctx, "Save");
            focus = ctx.focused_name().map(str::to_owned);
        };
        for _ in 0..3 {
            backend.render(&mut callback);
        }
        backend.assert_not_contains("[ First thread ]");
        backend.assert_contains("• First thread");
        let (x, y) = backend.find_text("First thread").unwrap();
        let initial = backend.buffer().get(x, y).style;
        assert!(!initial.modifiers.contains(slt::Modifiers::BOLD));
        backend.run_with_events(
            slt::EventBuilder::new().key_code(KeyCode::Tab).build(),
            &mut callback,
        );
        backend.render(&mut callback);
        drop(callback);
        // The tab composite has one focusable slot; no per-title stops.
        assert_eq!(focus.as_deref(), Some("tabs"));
        assert!(activations.is_empty());
        assert_eq!(tabs.selected, 0);
        backend.assert_contains("• First thread");
    }

    #[test]
    fn primary_actions_paint_explicit_contrasting_text_and_keep_disabled_guard() {
        for name in ["violet", "cyan", "lime", "light", "terminal", "nord"] {
            let theme = surface_theme(name);
            let mut backend = slt::TestBackend::new(40, 8);
            for _ in 0..3 {
                backend.render(|ctx| {
                    ctx.set_theme(theme);
                    primary_button(ctx, "Submit", true);
                    primary_button(ctx, "Unavailable", false);
                });
            }
            let (x, y) = backend.find_text("Submit").unwrap();
            let style = backend.buffer().get(x, y).style;
            assert_eq!(style.fg, Some(theme.selected_fg), "{name}");
            assert_eq!(style.bg, Some(theme.primary), "{name}");
            assert_ne!(style.fg, Some(Color::Reset), "{name}");
            let (x, y) = backend.find_text("Unavailable").unwrap();
            assert_ne!(backend.buffer().get(x, y).style.bg, style.bg, "{name}");
            let mut activated = false;
            backend.run_with_events(slt::EventBuilder::new().click(x, y).build(), |ctx| {
                ctx.set_theme(theme);
                primary_button(ctx, "Submit", true);
                activated = primary_button(ctx, "Unavailable", false).clicked;
            });
            assert!(!activated, "{name} disabled action");
        }
    }

    #[test]
    fn search_scope_uses_the_actual_browsing_identity_and_keeps_human_names() {
        let mut state = SurfaceState::new();
        state.projects = vec![Source {
            environment: "native-0".into(),
            environment_label: "Build".into(),
            project_id: "project-a".into(),
            project: "Release".into(),
            ..Default::default()
        }];
        let view = ui::View {
            inspector: vec![(
                "Browsing".into(),
                "native-0 / project-a · High or higher".into(),
            )],
            ..Default::default()
        };
        assert_eq!(
            browsing_scope(&view, &state),
            "Build · Release · High or higher"
        );
        state.source = Some(Source {
            project: "Other active source".into(),
            ..Default::default()
        });
        assert_eq!(
            browsing_scope(&view, &state),
            "Build · Release · High or higher"
        );
    }

    #[test]
    fn focused_attachment_keeps_composer_space_and_opens_narrow_context_on_demand() {
        let mut backend = ResizableBackend::new(140, 38);
        let mut state = SurfaceState::new();
        state.active_key = "environment/thread".into();
        let view = ui::View::default();
        let draft = "Exact 日本語  \nUnsent";
        let mut editor = ui::editor(draft, "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for (width, height) in [(140, 38), (70, 22), (40, 12), (140, 38)] {
            backend.run_with_events(
                slt::EventBuilder::new().resize(width, height).build(),
                |ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                },
            );
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    assert!(ctx.measured_layout_rect("t3oxide.navigator").is_none());
                    assert!(ctx.measured_layout_rect("t3oxide.context").is_none());
                    if let Some(rect) = ctx.measured_layout_rect("t3oxide.composer") {
                        assert_eq!(rect.width, width, "composer at {width}x{height}");
                    }
                });
            }
            backend.assert_contains("Send");
            backend.assert_contains("Model");
            assert_eq!(editor.lines().join("\n"), draft);
            if width == 40 {
                backend.run_with_events(
                    slt::EventBuilder::new().key_code(KeyCode::F(4)).build(),
                    |ctx| {
                        draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    },
                );
                for _ in 0..3 {
                    backend.render(|ctx| {
                        draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    });
                }
                backend.assert_contains("Copy ID");
                assert!(state.context_expanded);
                backend.run_with_events(
                    slt::EventBuilder::new().key_code(KeyCode::Esc).build(),
                    |ctx| {
                        draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    },
                );
                assert!(!state.context_expanded);
                assert_eq!(editor.lines().join("\n"), draft);
            }
        }
    }

    #[test]
    fn explicit_explorer_owns_focus_and_returns_to_the_original_composer() {
        for (width, height) in [(40, 12), (70, 22), (140, 38)] {
            let mut backend = slt::TestBackend::new(width, height);
            let mut state = SurfaceState::new();
            state.active_key = "env/thread".into();
            let view = ui::View::default();
            let draft = "Exact 日本語 café  ";
            let mut editor = ui::editor(draft, "violet", "");
            let mut modal_editor = ui::editor("", "violet", "");
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                });
            }
            let caret = (state.composer.cursor_row, state.composer.cursor_col);
            for open in [true, false] {
                let close_key = if !open && width < 74 {
                    KeyCode::Esc
                } else {
                    KeyCode::F(3)
                };
                backend.run_with_events(
                    slt::EventBuilder::new().key_code(close_key).build(),
                    |ctx| {
                        assert!(
                            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state)
                                .is_empty()
                        );
                    },
                );
                for _ in 0..3 {
                    backend.render(|ctx| {
                        assert!(
                            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state)
                                .is_empty()
                        );
                    });
                }
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    assert_eq!(
                        ctx.focused_name(),
                        Some(if open { "navigator" } else { "composer" })
                    );
                });
                assert_eq!(sidebar_visible(&state), open);
                assert_eq!(editor.lines().join("\n"), draft);
                assert_eq!(
                    (state.composer.cursor_row, state.composer.cursor_col),
                    caret
                );
            }
        }
    }

    #[test]
    fn sidebar_resize_keeps_default_width_and_restores_an_explicit_native_drag() {
        let mut backend = ResizableBackend::new(140, 38);
        let mut state = SurfaceState::new();
        state.applied_layout = Some(ui::LayoutMode::Split);
        state.sidebar_hidden = Some(false);
        state.active_key = "environment/thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("retained draft", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        let mut sidebar = None;
        for (width, height) in [(140, 38), (86, 31), (140, 38)] {
            backend.run_with_events(
                slt::EventBuilder::new().resize(width, height).build(),
                |ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                },
            );
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    sidebar = ctx.measured_layout_rect("t3oxide.navigator");
                });
            }
            assert_eq!(
                sidebar.unwrap().width,
                30,
                "default sidebar at {width}x{height}"
            );
        }
        let rect = sidebar.unwrap();
        backend.run_with_events(
            slt::EventBuilder::new()
                .click(rect.right(), rect.y + 5)
                .drag(50, rect.y + 5)
                .mouse_up(50, rect.y + 5)
                .build(),
            |ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        let preferred = state
            .sidebar_width
            .expect("native drag sets the cell preference");
        assert!(preferred >= 48, "drag changed the narrow default");
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            ctx.focus_by_name("composer");
        });
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        for (width, height) in [(86, 31), (70, 22), (40, 12), (140, 38)] {
            backend.run_with_events(
                slt::EventBuilder::new().resize(width, height).build(),
                |ctx| {
                    assert_eq!((ctx.width(), ctx.height()), (width, height));
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                },
            );
            let mut focused = Vec::new();
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    sidebar = ctx.measured_layout_rect("t3oxide.navigator");
                    focused.push(ctx.focused_name().map(str::to_owned));
                });
            }
            assert_eq!(
                focused.last().unwrap().as_deref(),
                Some(if width < 74 { "navigator" } else { "composer" }),
                "{width}x{height}: {focused:?}"
            );
            assert_eq!(state.sidebar_width, Some(preferred));
            assert_eq!(editor.lines().join("\n"), "retained draft");
            if width == 140 {
                assert_eq!(sidebar.unwrap().width, preferred);
            }
        }
        let (x, y) = backend
            .find_text("Env")
            .expect("resized navigator's actual action target");
        let mut actions = Vec::new();
        backend.run_with_events(slt::EventBuilder::new().click(x, y).build(), |ctx| {
            actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(matches!(actions.as_slice(), [Action::Command(name)] if name == "env"));
        for hidden in [true, false] {
            backend.run_with_events(
                slt::EventBuilder::new().key_code(KeyCode::F(3)).build(),
                |ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                },
            );
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    sidebar = ctx.measured_layout_rect("t3oxide.navigator");
                });
            }
            assert_eq!(state.sidebar_hidden, Some(hidden));
            assert_eq!(state.sidebar_width, Some(preferred));
            if hidden {
                backend.assert_not_contains("Unread");
            } else {
                assert_eq!(sidebar.unwrap().width, preferred);
            }
        }
        assert_eq!(editor.lines().join("\n"), "retained draft");
    }

    #[test]
    fn textarea_paste_growth_reveals_earlier_rows_without_moving_the_caret() {
        let mut backend = ResizableBackend::new(80, 12);
        let mut editor = TextareaState::new().word_wrap(76);
        backend.render(|ctx| {
            ctx.textarea(&mut editor, 2);
        });
        backend.run_with_events(
            slt::EventBuilder::new().paste(PHYSICAL_PASTE_DRAFT).build(),
            |ctx| {
                ctx.textarea(&mut editor, 2);
            },
        );
        assert_eq!(PHYSICAL_PASTE_DRAFT.len(), 283);
        assert_eq!(
            reports::content_revision(PHYSICAL_PASTE_DRAFT),
            "sha256:11c3a0522c981bec97d78a649c7f43bea3409a94ce5cd83d02c62feeb9463f10"
        );
        assert_eq!(editor.value(), PHYSICAL_PASTE_DRAFT);
        assert!(
            editor.scroll_offset > 0,
            "the initial two-row editor follows the paste caret"
        );
        let cursor = (editor.cursor_row, editor.cursor_col);
        for _ in 0..3 {
            let rows = editor.visual_line_count() as u32;
            backend.render(|ctx| {
                ctx.textarea(&mut editor, rows);
            });
        }
        backend.assert_contains("Review the release scope");
        backend.assert_contains("Unicode: café 日本語.");
        assert_eq!(editor.scroll_offset, 0, "the whole draft now fits");
        assert_eq!((editor.cursor_row, editor.cursor_col), cursor);
        assert!(editor.selection().is_none());

        backend.run_with_events(
            slt::EventBuilder::new()
                .key_with(KeyCode::Left, KeyModifiers::SHIFT)
                .build(),
            |ctx| {
                ctx.textarea(&mut editor, 2);
            },
        );
        let cursor = (editor.cursor_row, editor.cursor_col);
        let selection = editor.selection();
        for (width, rows) in [(45, 7), (36, 3), (76, 6)] {
            editor.wrap_width = Some(width);
            backend.run_with_events(
                slt::EventBuilder::new().resize(width + 4, 12).build(),
                |ctx| {
                    ctx.textarea(&mut editor, rows);
                },
            );
            for _ in 0..3 {
                backend.render(|ctx| {
                    ctx.textarea(&mut editor, rows);
                });
            }
            assert!(
                editor.scroll_offset <= editor.visual_line_count().saturating_sub(rows as usize)
            );
            assert_eq!((editor.cursor_row, editor.cursor_col), cursor);
            assert_eq!(editor.selection(), selection);
            assert_eq!(editor.selected_text().as_deref(), Some("9"));
            assert_eq!(editor.value(), PHYSICAL_PASTE_DRAFT);
        }
        backend.assert_contains("Review the release scope");
        backend.assert_contains("Unicode: café 日本語.");
        backend.assert_contains("012345678▎9");

        editor.set_selection((1, 0), (1, 0));
        editor.scroll_offset = 1;
        backend.render(|ctx| {
            ctx.textarea(&mut editor, 4);
        });
        assert_eq!(editor.scroll_offset, 1, "a valid viewport stays in place");
        assert_eq!((editor.cursor_row, editor.cursor_col), (1, 0));
    }

    #[test]
    fn composer_paste_grows_and_restores_all_rows_after_settled_resize() {
        let mut backend = ResizableBackend::new(140, 38);
        let mut state = SurfaceState::new();
        state.active_key = "env/thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                ctx.focus_by_name("composer");
            });
        }
        backend.run_with_events(
            slt::EventBuilder::new().paste(PHYSICAL_PASTE_DRAFT).build(),
            |ctx| {
                let actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                assert!(actions.iter().all(|action| !matches!(action, Action::Send)));
            },
        );
        let cursor = (state.composer.cursor_row, state.composer.cursor_col);
        for (width, height) in [(140, 38), (86, 31), (70, 31), (70, 22), (140, 38)] {
            backend.run_with_events(
                slt::EventBuilder::new().resize(width, height).build(),
                |ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                },
            );
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                });
            }
            assert_eq!(editor.lines().join("\n"), PHYSICAL_PASTE_DRAFT);
            assert_eq!(state.composer.value(), PHYSICAL_PASTE_DRAFT);
            assert_eq!(
                (state.composer.cursor_row, state.composer.cursor_col),
                cursor
            );
            assert!(state.composer.selection().is_none());
            if width == 140 {
                backend.assert_contains("Review the release scope");
                backend.assert_contains("Unicode: café 日本語.");
                backend.assert_contains("Long_token_");
                assert_eq!(state.composer.scroll_offset, 0);
            }
        }
    }

    #[test]
    fn compact_resize_retains_draft_and_keeps_b_content_with_context() {
        let draft = "alpha beta  日本語 👩‍💻\n".repeat(16);
        let mut state = SurfaceState::new();
        state.active_key = "env/thread".into();
        state.show_context = true;
        state.tabs.push(Tab {
            key: state.active_key.clone(),
            title: "Conversation title".into(),
            ..Tab::default()
        });
        let source = Source {
            environment: "env".into(),
            environment_label: "Env".into(),
            project_id: "project".into(),
            project: "Project".into(),
            thread: "thread".into(),
            ..Source::default()
        };
        state.contents.push(ContentItem {
            key: "env/thread/report".into(),
            source,
            title: "Release guide".into(),
            kind: "guide".into(),
            markdown: "# Release guide\n\nReview the evidence.".into(),
            revision: "rev".into(),
            ..ContentItem::default()
        });
        let mut legacy = ui::editor(&draft, "violet", "");
        let mut modal = ui::editor("", "violet", "");
        let view = ui::View::default();
        let mut backend = ResizableBackend::new(140, 38);
        for (width, height) in [(140, 38), (70, 22), (40, 12), (140, 38)] {
            state.attention_mode = false;
            backend.run_with_events(
                slt::EventBuilder::new().resize(width, height).build(),
                |ctx| {
                    draw(ctx, &view, &mut legacy, &mut modal, None, &mut state);
                },
            );
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut legacy, &mut modal, None, &mut state);
                });
            }
            backend.assert_contains("Prompt");
            backend.assert_contains("Send");
            assert_eq!(
                legacy.lines().join("\n"),
                draft,
                "draft at {width}x{height}"
            );
            state.attention_mode = true;
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &view, &mut legacy, &mut modal, None, &mut state);
                });
            }
            backend.assert_contains("Projects");
            backend.assert_contains("Release guide");
            backend.assert_contains(if width < 50 { "ⓘ" } else { "Context" });
            assert_eq!(
                attention::selected_content(&state).unwrap().key,
                "env/thread/report"
            );
        }
    }

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
    fn copy_draft_chord_precedes_editor_copy_and_is_guarded_by_app_modals() {
        let chord = KeyModifiers(KeyModifiers::CONTROL.0 | KeyModifiers::ALT.0);
        let draft = "  café 日本語\nsecond line  \n\n";
        let mut backend = slt::TestBackend::new(100, 31);
        let mut state = SurfaceState::new();
        state.applied_layout = Some(ui::LayoutMode::Split);
        state.sidebar_hidden = Some(false);
        state.active_key = "environment/current-thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor(draft, "violet", "");
        let mut modal_editor = ui::editor("modal draft", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        for focus in ["composer", "navigator"] {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                assert!(ctx.focus_by_name(focus));
            });
            let cursor = (state.composer.cursor_row, state.composer.cursor_col);
            let mut actions = Vec::new();
            backend.run_with_events(
                slt::EventBuilder::new()
                    .key_with(KeyCode::Char('c'), chord)
                    .build(),
                |ctx| {
                    actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                    assert_eq!(ctx.focused_name(), Some(focus));
                    assert!(
                        !ctx.key_mod('c', chord),
                        "whole-draft command claims the event"
                    );
                },
            );
            assert!(matches!(actions.as_slice(), [Action::Command(name)] if name == "copy-draft"));
            assert_eq!(editor.lines().join("\n"), draft);
            assert_eq!(
                (state.composer.cursor_row, state.composer.cursor_col),
                cursor
            );
            assert!(state.composer.selected_text().is_none());
            assert_eq!(state.active_key, "environment/current-thread");
        }
        let modal = ui::Modal {
            title: "Write answer".into(),
            editor: true,
            action_stamp: 7,
            ..Default::default()
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
        let mut actions = Vec::new();
        backend.run_with_events(
            slt::EventBuilder::new()
                .key_with(KeyCode::Char('c'), chord)
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
        assert!(
            actions.is_empty(),
            "a modal never copies or changes the whole prompt"
        );
        assert_eq!(editor.lines().join("\n"), draft);
        assert_eq!(modal_editor.lines().join("\n"), "modal draft");
    }

    #[test]
    fn focused_composer_selects_and_consumes_copy_without_an_app_action() {
        for modifiers in [KeyModifiers::CONTROL, KeyModifiers::SUPER] {
            let mut backend = slt::TestBackend::new(86, 31);
            let mut state = SurfaceState::new();
            state.applied_layout = Some(ui::LayoutMode::Split);
            state.sidebar_hidden = Some(false);
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
            state.applied_layout = Some(ui::LayoutMode::Split);
            state.sidebar_hidden = Some(false);
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
                "Unread", "Run", "Pins", "Settled", "Project", "Env", "Model", "Effort", "Stop",
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
            assert!(
                actions
                    .iter()
                    .any(|action| matches!(action, Action::Command(command) if command == "env"))
            );
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
        let mut backend = slt::TestBackend::new(100, 32);
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
        let divider = (99.0 * state.navigation_split.ratio).round() as u32;
        for y in rect.y + 1..rect.y + rect.height - 1 {
            assert_ne!(
                backend.buffer().get(divider, y).symbol.as_str(),
                "│",
                "split handle painted through modal at {divider},{y}"
            );
        }
    }

    #[test]
    fn graph_mouse_navigation_retains_content_and_exact_source() {
        use slt::{EventBuilder, TestBackend};
        let mut state = SurfaceState::new();
        state.applied_layout = Some(ui::LayoutMode::Split);
        state.sidebar_hidden = Some(false);
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
            ctx.focus_by_name("project-navigation");
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.run_with_events(EventBuilder::new().build(), |ctx| {
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
            .find_text("All projects")
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
        backend.assert_contains("Forms");
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
        state.applied_layout = Some(ui::LayoutMode::Split);
        state.sidebar_hidden = Some(false);
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
