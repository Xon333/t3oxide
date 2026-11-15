//! SLT owns active terminal rendering, widget focus and editor input (A1–A5, B1–B3).
//! Controller actions retain native source identities; a widget never acknowledges a write.

mod navigation;

use crate::{domain, reports, ui};
use ratatui_textarea::TextArea;
use serde_json::Value;
use slt::{
    Border, ButtonVariant, Color, Context, KeyCode, KeyModifiers, ListState, ScrollState,
    SpinnerState, TextareaState, WidgetColors, palette::tailwind,
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
    pub attention_mode: bool,
    pub internal_tabs: bool,
    pub composer_model: String,
    pub composer_effort: String,
    pub composer_override: bool,
    pub run_active: Option<bool>,
    navigation: navigation::State,
    pub show_context: bool,
    pub navigator_visible: Option<bool>,
    pub navigator_shown: bool,
    pub focus_cycle: Option<bool>,
    pub(crate) focus: Option<ui::Focus>,
    pub selected_project: Option<String>,
    pub selected_question: Option<String>,
    pub projects_global: bool,
    composer: TextareaState,
    composer_key: String,
    tab_editors: HashMap<String, TextareaState>,
    tabs_scroll: ScrollState,
    modal_editor: TextareaState,
    modal_stamp: Option<u64>,
    modal_selected: usize,
    transcript: ScrollState,
    pub(crate) transcript_layout: Option<ui::TranscriptLayout>,
    pub(crate) transcript_jump: Option<(String, String, usize)>,
    controller_scroll: Option<(usize, bool)>,
    viewport: Option<(u32, u32, bool, u32, u32, bool)>,
    focused_control: Option<String>,
    pending_transcript_down: Option<usize>,
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
            projects_global: true,
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
    let source_changed = state.composer_key != state.active_key;
    if source_changed {
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
        state.pending_transcript_down = None;
        state.command_escape = None;
        state.command_open = false;
        ctx.focus_by_name("composer");
        ctx.request_frame();
    }
    sync_editor(&mut state.composer, composer);
    if modal.is_none() && !state.active_key.is_empty() {
        let copy_draft = KeyModifiers(KeyModifiers::CONTROL.0 | KeyModifiers::ALT.0);
        let presses: Vec<_> = ctx
            .key_presses_when(true)
            .filter(|(_, key)| key.code == KeyCode::Char('c') && key.modifiers == copy_draft)
            .map(|(index, _)| index)
            .collect();
        for index in presses {
            // Claim the whole draft before textarea's selection-copy handler.
            ctx.consume_event(index);
            ctx.copy_to_clipboard(state.composer.value());
        }
    }
    let width = ctx.width();
    let height = ctx.height();
    state.navigator_shown = state.navigator_visible.unwrap_or(width >= 65);
    state.composer.wrap_width = Some(pane_width(width, state).saturating_sub(3).max(1));
    let conversation_height = height.saturating_sub(3 + u32::from(!view.notice.is_empty()));
    let rows = editor_rows(&mut state.composer, conversation_height);
    let completion_rows = if state.command_open {
        completion_indices(&state.composer.value(), state.command_escape.as_deref())
            .len()
            .min(4) as u32
    } else {
        0
    };
    let geometry = (
        width,
        height,
        state.show_context,
        rows,
        completion_rows,
        state.navigator_shown,
    );
    let resized = state
        .viewport
        .replace(geometry)
        .is_some_and(|old| old != geometry);
    let restore_focus = (resized && !source_changed)
        .then(|| state.focused_control.clone())
        .flatten();
    if resized {
        // Fresh SLT bounds must rebind the controller's follow/manual intent.
        state.controller_scroll = None;
        state.pending_transcript_down = None;
    }
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
                navigation::draw_attention(ctx, state, height, &mut actions);
            } else {
                draw_tabs(ctx, state, &mut actions);
                ctx.container().w(width).grow(1).row(|ctx| {
                    if state.navigator_shown {
                        navigation::draw_navigator(ctx, view, state, height, &mut actions);
                    }
                    if width >= 65 || !state.navigator_shown {
                        ctx.container()
                            .w(pane_width(width, state))
                            .grow(1)
                            .col(|ctx| {
                                draw_transcript(ctx, view, state, &mut actions);
                                draw_composer(ctx, view, composer, state, rows, &mut actions);
                            });
                    }
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
                if view.pending > 0 && ctx.button(format!("Unconfirmed {}", view.pending)).clicked {
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
    global_actions(ctx, modal.is_some(), state, &mut actions);
    if modal.is_none() {
        if let Some(reverse) = state.focus_cycle.take() {
            cycle_panes(ctx, state, reverse);
        }
        if width < 65
            && actions.iter().any(|action| {
                matches!(
                    action,
                    Action::Open(_) | Action::OpenSource(_) | Action::SelectTab(_)
                )
            })
        {
            state.navigator_visible = None;
        }
        // Buttons and content outside navigation act on the displayed source.
        // Preserve the invoking context while a modal owns SLT focus.
        if let Some(name) = restore_focus {
            if !ctx.focus_by_name(&name) && !ctx.focus_by_name("composer") {
                ctx.focus_by_name("navigator");
            }
            ctx.request_frame();
        }
        state.focused_control = ctx.focused_name().map(str::to_owned);
        state.focus = Some(match ctx.focused_name() {
            Some("navigator") => ui::Focus::Navigator,
            Some("transcript") => ui::Focus::Transcript,
            Some(name) if name.starts_with("thread:") => ui::Focus::Navigator,
            _ => ui::Focus::Composer,
        });
    }
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
    ctx.register_focusable_named("context");
    ctx.scrollable(&mut state.context_scroll)
        .grow(1)
        .col(|ctx| {
            for (label, value) in &view.inspector {
                if !value.is_empty() {
                    ctx.text(label).dim();
                    ctx.text(value).wrap();
                }
            }
        });
    ctx.row(|ctx| {
        if ctx.button("Copy ID").clicked {
            actions.push(Action::CopyId);
        }
        ctx.text("Internal tabs").dim();
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

fn draw_transcript(
    ctx: &mut Context,
    view: &ui::View,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    let geometry = reports::Geometry {
        width: pane_width(ctx.width(), state).saturating_sub(4).max(1),
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
    let mut keyboard_down = false;
    let before_binding = state.transcript.offset;
    let wheel_down = !ctx.is_disabled()
        && state.controller_scroll.is_some()
        && measured.is_some_and(|rect| {
            ctx.events().any(|event| {
                matches!(event,
                slt::Event::Mouse(mouse) if mouse.kind == slt::MouseKind::ScrollDown
                    && rect.contains(mouse.x, mouse.y))
            })
        });
    let down_origin = if wheel_down {
        Some(before_binding)
    } else {
        state.pending_transcript_down.take()
    };
    state.transcript.follow_focus = false;
    state.transcript.stick_to_bottom = view.follow && state.transcript_jump.is_none();
    ctx.group(&group).grow(1).col(|ctx| {
        let focused = ctx.register_focusable_named("transcript");
        // An old focus slot cannot scroll a replacement source or stale geometry.
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
            state.transcript.offset = state.transcript.offset.min(state.transcript.max_offset());
            let page = (state.transcript.viewport_height() as usize).max(1);
            match code {
                KeyCode::Up => state.transcript.scroll_up(1),
                KeyCode::Down => state.transcript.scroll_down(1),
                KeyCode::PageUp => state.transcript.scroll_up(page),
                KeyCode::PageDown => state.transcript.scroll_down(page),
                _ => unreachable!("filtered transcript navigation key"),
            }
            keyboard_scrolled = true;
            keyboard_down = matches!(code, KeyCode::Down | KeyCode::PageDown);
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
        viewport.grow(1).border(Border::Rounded).p(1).col(|ctx| {
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
    if let (Some(items), Some(rect)) = (items, measured)
        && state.transcript.viewport_height() > 0
    {
        let top = state.transcript.offset;
        let max = state.transcript.max_offset();
        let followed_down =
            keyboard_down && top > before_binding || down_origin.is_some_and(|origin| top > origin);
        let follow = !jumped
            && state.transcript_jump.is_none()
            && top == max
            && (view.follow && !keyboard_scrolled || followed_down);
        state.pending_transcript_down = if wheel_down && top == before_binding {
            Some(before_binding)
        } else {
            None
        };
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
            width: u16::try_from(rect.width.saturating_sub(4)).unwrap_or(u16::MAX),
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

fn pane_width(width: u32, state: &SurfaceState) -> u32 {
    let sidebar = if state.navigator_shown {
        if width >= 110 { 32 } else { 30 }
    } else {
        0
    };
    let context = if state.show_context && width >= 110 {
        26
    } else {
        0
    };
    width.saturating_sub(sidebar + context)
}

fn editor_rows(editor: &mut TextareaState, height: u32) -> u32 {
    let lines = u32::try_from(editor.visual_line_count()).unwrap_or(u32::MAX);
    let max = height
        .saturating_sub(7)
        .min((height / 3).clamp(1, 12))
        .max(1);
    lines.clamp(2.min(max), max)
}

fn draw_composer(
    ctx: &mut Context,
    view: &ui::View,
    legacy: &mut TextArea<'static>,
    state: &mut SurfaceState,
    rows: u32,
    actions: &mut Vec<Action>,
) {
    let indices = if state.command_open {
        completion_indices(&state.composer.value(), state.command_escape.as_deref())
    } else {
        Vec::new()
    };
    if !indices.is_empty() {
        ctx.container().max_w(58).col(|ctx| {
            for (position, index) in indices
                .iter()
                .enumerate()
                .skip(state.command_selected.saturating_sub(3))
                .take(4)
            {
                let action = &domain::ACTIONS[*index];
                let selected = position == state.command_selected;
                let label = format!(
                    "{} /{}  {}",
                    if selected { "›" } else { " " },
                    action.name,
                    action.label
                );
                if quiet_button(ctx, &label, selected, accent(&view.theme)) {
                    actions.push(Action::Command(action.name.into()));
                    state.command_escape = Some(state.composer.value());
                    state.command_open = false;
                }
            }
        });
    }
    ctx.container()
        .border(Border::Rounded)
        .title("Prompt")
        .col(|ctx| {
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
            }
        });
    ctx.row(|ctx| {
        if state.run_active == Some(true) {
            ctx.text(state.spinner.frame(ctx.tick() / 8).to_string())
                .fg(tailwind::EMERALD.c300);
            ctx.request_frame_after(std::time::Duration::from_millis(120));
        }
        let available = ctx.width().saturating_sub(16).max(8) as usize;
        let model_label = if state.composer_override {
            format!("Next: {}", state.composer_model)
        } else {
            state.composer_model.clone()
        };
        let model = elide(
            if model_label.is_empty() {
                "Model ?"
            } else {
                &model_label
            },
            available.saturating_sub(10).max(5),
        );
        if quiet_button(ctx, &model, false, accent(&view.theme)) {
            actions.push(Action::Command(
                if state.composer_override {
                    "model next"
                } else {
                    "model"
                }
                .into(),
            ));
        }
        let effort = if state.composer_effort.is_empty() {
            "Effort ?"
        } else {
            &state.composer_effort
        };
        if quiet_button(ctx, &elide(effort, 10), false, accent(&view.theme)) {
            actions.push(Action::Command(
                if state.composer_override {
                    "effort next"
                } else {
                    "effort"
                }
                .into(),
            ));
        }
        ctx.spacer();
        if quiet_button(ctx, "Stop", false, tailwind::ROSE.c300) {
            actions.push(Action::Stop);
        }
    });
}

fn accent(theme: &str) -> Color {
    match theme {
        "cyan" | "terminal" => tailwind::CYAN.c300,
        "lime" => tailwind::LIME.c300,
        "amber" => tailwind::AMBER.c300,
        _ => tailwind::VIOLET.c300,
    }
}

fn quiet_button(ctx: &mut Context, label: &str, selected: bool, color: Color) -> bool {
    let colors = WidgetColors {
        bg: Some(Color::Reset),
        fg: Some(if selected { color } else { ctx.theme().text }),
        accent: Some(color),
        ..WidgetColors::default()
    };
    ctx.button(label)
        .variant(ButtonVariant::Default)
        .colors(&colors)
        .clicked
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

fn draw_modal(
    ctx: &mut Context,
    modal: &ui::Modal,
    legacy: &mut TextArea<'static>,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    use unicode_width::UnicodeWidthStr;
    sync_editor(&mut state.modal_editor, legacy);
    let layout_changed = state.modal_stamp != Some(modal.action_stamp);
    if layout_changed {
        state.modal_selected = modal.selected.min(modal.items.len().saturating_sub(1));
        state.modal_scroll = ScrollState::new();
        ctx.request_frame();
    }
    let searchable = modal.editor && matches!(modal.title.as_str(), "Commands" | "Conversations");
    let single_line = !modal.multiline;
    let color = tailwind::CYAN.c300;
    let content_width = modal
        .items
        .iter()
        .map(|item| item.label.width() + 5)
        .chain([modal.title.width() + 8])
        .max()
        .unwrap_or(28);
    let preferred = if modal.editor || !modal.detail.is_empty() {
        64
    } else {
        content_width.clamp(28, 64)
    };
    let width = (preferred as u32).min(ctx.width().saturating_sub(4).max(1));
    let height = ctx.height().saturating_sub(2).max(1);
    let editor_rows = if single_line {
        1
    } else {
        3.min(height.saturating_sub(6).max(1))
    };
    let reserved = 4 + if modal.editor {
        editor_rows + u32::from(!searchable)
    } else {
        0
    };
    let body_height = height.saturating_sub(reserved).max(1);
    ctx.modal(|ctx| {
        let background = ctx.theme().bg;
        ctx.group("t3oxide.modal")
            .bg(background)
            .w(width)
            .max_h(height)
            .p(1)
            .col(|ctx| {
                ctx.row(|ctx| {
                    ctx.text(&modal.title).bold().fg(color).grow(1);
                    if quiet_button(ctx, "×", false, color) && !layout_changed {
                        actions.push(Action::CloseModal);
                    }
                });
                if modal.editor {
                    let focused = ctx.register_focusable_named("modal-editor");
                    let mut editor_actions = Vec::new();
                    palette_keys(
                        ctx,
                        focused && (!layout_changed || searchable),
                        searchable,
                        modal,
                        state,
                        &mut editor_actions,
                    );
                    if single_line && !searchable && !layout_changed {
                        let submits: Vec<_> = ctx
                            .key_presses_when(focused)
                            .filter(|(_, key)| {
                                key.code == KeyCode::Enter && key.modifiers == KeyModifiers::NONE
                            })
                            .map(|(i, _)| i)
                            .collect();
                        for i in submits {
                            ctx.consume_event(i);
                            actions.push(Action::ModalSubmit);
                        }
                    }
                    if ctx
                        .disabled(layout_changed && !searchable, |ctx| {
                            ctx.textarea(&mut state.modal_editor, editor_rows)
                        })
                        .changed
                    {
                        *legacy = ui::editor(&state.modal_editor.value(), "violet", "Type here…");
                        actions.push(Action::ModalEditChanged);
                        if searchable {
                            state.modal_selected = 0;
                            for action in &mut editor_actions {
                                if let Action::ModalChoice(index) = action {
                                    *index = 0;
                                }
                            }
                        }
                    }
                    actions.extend(editor_actions);
                }
                let body_focused = ctx.register_focusable_named("modal-body");
                ctx.register_focusable();
                state.modal_scroll.follow_focus = !body_focused;
                let selection_before = state.modal_selected;
                let activate =
                    !modal.editor && !layout_changed && modal_menu_keys(ctx, modal, state);
                let selection_moved = selection_before != state.modal_selected;
                ctx.scrollable(&mut state.modal_scroll)
                    .max_h(body_height)
                    .col(|ctx| {
                        if !modal.description.is_empty() {
                            ctx.text(&modal.description).wrap().dim();
                        }
                        if !modal.detail.is_empty() {
                            reports::draw_markdown(ctx, &modal.detail, width.saturating_sub(2));
                        }
                        for (index, item) in modal.items.iter().enumerate() {
                            let name = format!("modal-choice:{}:{index}", modal.action_stamp);
                            let focused = ctx.register_focusable_named(&name);
                            if focused && !searchable && !selection_moved {
                                state.modal_selected = index;
                            }
                            let selected = index == state.modal_selected;
                            let label = format!(
                                "{} {}",
                                if item.checked {
                                    "✓"
                                } else if selected {
                                    "›"
                                } else {
                                    " "
                                },
                                item.label
                            );
                            if quiet_button(ctx, &label, selected, color) && !layout_changed {
                                actions.push(Action::ModalChoice(index));
                            }
                            if selected && !item.description.is_empty() {
                                ctx.text(&item.description).dim().wrap();
                            }
                        }
                    });
                if activate {
                    actions.push(Action::ModalChoice(state.modal_selected));
                }
                if modal.editor
                    && !searchable
                    && quiet_button(ctx, "Continue", true, color)
                    && !layout_changed
                {
                    actions.push(Action::ModalSubmit);
                }
                if !modal.footer.is_empty() {
                    ctx.text(&modal.footer).dim().truncate();
                }
            });
    });
    if layout_changed {
        state.modal_stamp = Some(modal.action_stamp);
        if modal.editor {
            ctx.focus_by_name("modal-editor");
        } else if modal.items.is_empty() {
            ctx.focus_by_name("modal-body");
        } else {
            ctx.focus_by_name(&format!(
                "modal-choice:{}:{}",
                modal.action_stamp, state.modal_selected
            ));
        }
    }
}

fn modal_menu_keys(ctx: &mut Context, modal: &ui::Modal, state: &mut SurfaceState) -> bool {
    let mut activate = false;
    let mut consumed = Vec::new();
    let old = state.modal_selected;
    let reading = modal.items.is_empty() || ctx.focused_name() == Some("modal-body");
    let mut read = false;
    for (index, key) in ctx.key_presses_when(true) {
        if key.modifiers != KeyModifiers::NONE {
            continue;
        }
        match key.code {
            KeyCode::Up if reading => {
                state.modal_scroll.scroll_up(1);
                read = true;
            }
            KeyCode::Down if reading => {
                state.modal_scroll.scroll_down(1);
                read = true;
            }
            KeyCode::Up => state.modal_selected = state.modal_selected.saturating_sub(1),
            KeyCode::Down => {
                state.modal_selected =
                    (state.modal_selected + 1).min(modal.items.len().saturating_sub(1))
            }
            KeyCode::Home if reading => {
                state.modal_scroll.set_offset(0);
                read = true;
            }
            KeyCode::End if reading => {
                state
                    .modal_scroll
                    .set_offset(state.modal_scroll.max_offset());
                read = true;
            }
            KeyCode::Home if !modal.items.is_empty() => state.modal_selected = 0,
            KeyCode::End if !modal.items.is_empty() => state.modal_selected = modal.items.len() - 1,
            KeyCode::PageUp => {
                state.modal_scroll.scroll_up(5);
                read = true;
            }
            KeyCode::PageDown => {
                state.modal_scroll.scroll_down(5);
                read = true;
            }
            KeyCode::Enter if reading || read => {}
            KeyCode::Enter if !modal.items.is_empty() => activate = true,
            _ => continue,
        }
        consumed.push(index);
    }
    for index in consumed {
        ctx.consume_event(index);
    }
    if read {
        ctx.focus_by_name("modal-body");
        state.modal_scroll.follow_focus = false;
        ctx.request_frame();
    }
    if old != state.modal_selected {
        ctx.focus_by_name(&format!(
            "modal-choice:{}:{}",
            modal.action_stamp, state.modal_selected
        ));
        ctx.request_frame();
    }
    activate
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
                KeyCode::Enter => {
                    actions.push(Action::ModalChoice(
                        state
                            .modal_selected
                            .min(modal.items.len().saturating_sub(1)),
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

fn cycle_panes(ctx: &mut Context, state: &SurfaceState, reverse: bool) {
    let panes: Vec<&str> = if state.show_context && (ctx.width() < 110 || state.attention_mode) {
        vec!["context"]
    } else if state.attention_mode {
        vec!["attention-list", "attention-content"]
    } else {
        let mut names = Vec::new();
        if state.navigator_shown {
            names.push("navigator");
        }
        if ctx.width() >= 65 || !state.navigator_shown {
            names.extend(["transcript", "composer"]);
        }
        if state.show_context && ctx.width() >= 110 {
            names.push("context");
        }
        names
    };
    let focused = ctx.focused_name().unwrap_or_default();
    let current = panes.iter().position(|name| {
        focused == *name || *name == "navigator" && focused.starts_with("thread:")
    });
    let target = current.map_or(
        if reverse {
            panes.len().saturating_sub(1)
        } else {
            0
        },
        |index| {
            if reverse {
                (index + panes.len() - 1) % panes.len()
            } else {
                (index + 1) % panes.len()
            }
        },
    );
    for step in 0..panes.len() {
        let index = if reverse {
            (target + panes.len() - step) % panes.len()
        } else {
            (target + step) % panes.len()
        };
        if ctx.focus_by_name(panes[index]) {
            break;
        }
    }
    ctx.request_frame();
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
            KeyCode::F(3) if !modal => Some(Action::Command("sidebar".into())),
            KeyCode::F(4) if !modal => Some(Action::Command("inspector".into())),
            KeyCode::F(6) if !modal => {
                state.focus_cycle = Some(key.modifiers.contains(KeyModifiers::SHIFT));
                consumed.push(index);
                None
            }
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
    }

    #[test]
    fn growing_composer_measures_shared_rows_and_keeps_the_draft_through_reflow() {
        let mut backend = ResizableBackend::new(86, 31);
        let mut state = SurfaceState::new();
        state.active_key = "env/thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        assert_eq!(editor_rows(&mut state.composer, 28), 2);
        let draft = "café 日本語 e\u{301} 👩\u{200d}💻  \n".repeat(20);
        backend.run_with_events(slt::EventBuilder::new().paste(&draft).build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        assert_eq!(editor.lines().join("\n"), draft);
        assert_eq!(editor_rows(&mut state.composer, 28), 9);
        let cursor = (state.composer.cursor_row, state.composer.cursor_col);
        for (width, height) in [(140, 50), (65, 22), (86, 31)] {
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
            assert_eq!(editor.lines().join("\n"), draft);
            assert_eq!(
                (state.composer.cursor_row, state.composer.cursor_col),
                cursor
            );
            assert_eq!(
                state.composer.wrap_width,
                Some(pane_width(width, &state) - 3)
            );
            let rows = editor_rows(&mut state.composer, height - 3);
            assert!(rows <= 12 && rows <= (height - 3) / 3);
            assert!(
                state.composer.scroll_offset > 0,
                "overflow keeps the caret in the bounded viewport"
            );
        }
    }

    #[test]
    fn completion_is_above_the_prompt_and_whole_copy_preserves_editor_ownership() {
        let mut backend = slt::TestBackend::new(110, 38);
        let mut state = SurfaceState::new();
        state.active_key = "environment/current-thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.run_with_events(slt::EventBuilder::new().key('/').build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.run_with_events(
            slt::EventBuilder::new()
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
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        assert_eq!(
            editor.lines().join("\n"),
            "/model",
            "completion controls retain editor typing"
        );
        let completion = backend
            .find_text("/model")
            .expect("slash completion visible");
        let cursor = backend.find_text("▎").expect("composer caret visible");
        assert!(
            completion.1 < cursor.1,
            "completion stays above the editable prompt"
        );
        backend.run_with_events(
            slt::EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                let actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                assert!(
                    actions
                        .iter()
                        .any(|a| matches!(a, Action::Command(name) if name == "model"))
                );
                assert!(!actions.iter().any(|a| matches!(a, Action::Send)));
            },
        );
        let draft = "café 日本語 e\u{301} 👩\u{200d}💻  \nsecond line  ";
        editor = ui::editor(draft, "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.run_with_events(
            slt::EventBuilder::new().key_code(KeyCode::Left).build(),
            |ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            },
        );
        let caret = (state.composer.cursor_row, state.composer.cursor_col);
        let modifiers = KeyModifiers(KeyModifiers::CONTROL.0 | KeyModifiers::ALT.0);
        backend.run_with_events(
            slt::EventBuilder::new()
                .key_with(KeyCode::Char('c'), modifiers)
                .build(),
            |ctx| {
                let actions = draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
                assert!(
                    !ctx.key_mod('c', modifiers),
                    "whole-draft copy claims the chord"
                );
                assert!(actions.is_empty());
            },
        );
        assert_eq!(editor.lines().join("\n"), draft);
        assert_eq!(
            (state.composer.cursor_row, state.composer.cursor_col),
            caret
        );
        assert_eq!(state.active_key, "environment/current-thread");
        assert!(state.composer.selected_text().is_none());
        let modal = ui::Modal {
            title: "Write answer".into(),
            editor: true,
            action_stamp: 7,
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
        backend.run_with_events(
            slt::EventBuilder::new()
                .key_with(KeyCode::Char('c'), modifiers)
                .build(),
            |ctx| {
                draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&modal),
                    &mut state,
                );
                assert!(ctx.raw_events().any(|event| matches!(event, slt::Event::Key(key) if key.code == KeyCode::Char('c') && key.modifiers == modifiers)), "a modal excludes whole-draft copy");
            },
        );
        assert_eq!(editor.lines().join("\n"), draft);
        assert_eq!(
            (state.composer.cursor_row, state.composer.cursor_col),
            caret
        );
    }

    #[test]
    fn source_switch_with_a_growing_draft_returns_focus_to_the_composer() {
        let mut backend = slt::TestBackend::new(110, 38);
        let mut state = SurfaceState::new();
        state.active_key = "env/first".into();
        let view = ui::View::default();
        let mut editor = ui::editor("", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.render(|ctx| {
            ctx.focus_by_name("transcript");
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert_eq!(state.focused_control.as_deref(), Some("transcript"));
        state.active_key = "env/second".into();
        editor = ui::editor(&"Different source draft\n".repeat(10), "violet", "");
        for _ in 0..3 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        assert_eq!(state.focused_control.as_deref(), Some("composer"));
        backend.run_with_events(slt::EventBuilder::new().key('X').build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        assert!(editor.lines().join("\n").ends_with('X'));
        assert_eq!(state.active_key, "env/second");
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
                "Inbox", "Active", "Pins", "Settled", "Scope", "Env", "Model ?", "Effort ?", "Stop",
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
    fn project_list_navigation_retains_content_and_exact_source() {
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
        let (x, y) = backend
            .find_text("▾ Project")
            .expect("project group header");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.render(|ctx| {
            draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
        });
        backend.assert_contains("Evidence");
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
            .find_text("Conversation")
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
        backend.assert_contains("Stop");
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
        backend.assert_contains("Needs you");
        backend.assert_contains("Project");
        backend.assert_contains("Answer");
        backend.assert_contains("Conversation");
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
        backend.assert_contains("Choose target");
        state.questions[0].summary = "Changed question wording".into();
        state.questions[0].request["question"] = Value::String("Changed question wording".into());
        for _ in 0..2 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        backend.assert_contains("Question changed");
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
            .find_text("Conversation")
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
    #[test]
    fn compact_commands_keep_editor_choices_and_close_reachable() {
        let mut backend = slt::TestBackend::new(40, 12);
        let mut state = SurfaceState::new();
        state.active_key = "env/thread".into();
        let view = ui::View::default();
        let mut editor = ui::editor("keep my draft", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        let modal = ui::Modal {
            title: "Commands".into(),
            editor: true,
            action_stamp: 20,
            description: "Long guidance ".repeat(30),
            items: vec![
                ui::ModalItem {
                    label: "First".into(),
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Second".into(),
                    ..Default::default()
                },
            ],
            footer: "↑↓ choose · Enter run · Esc close".into(),
            ..Default::default()
        };
        for _ in 0..4 {
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
        backend.assert_contains("Commands");
        backend.assert_contains("×");
        backend.assert_contains("Esc close");
        let mut actions = Vec::new();
        backend.run_with_events(
            slt::EventBuilder::new()
                .key_code(KeyCode::Down)
                .key_code(KeyCode::Enter)
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
            actions
                .iter()
                .any(|action| matches!(action, Action::ModalChoice(1)))
        );
        assert_eq!(editor.lines().join("\n"), "keep my draft");
        assert_eq!(modal_editor.lines().join("\n"), "");
    }

    #[test]
    fn menu_arrows_select_once_and_multiline_answers_keep_enter() {
        let mut backend = slt::TestBackend::new(70, 22);
        let mut state = SurfaceState::new();
        let view = ui::View::default();
        let mut editor = ui::editor("draft", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        let modal = ui::Modal {
            title: "Effort".into(),
            action_stamp: 21,
            items: vec![
                ui::ModalItem {
                    label: "Low".into(),
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "High".into(),
                    ..Default::default()
                },
            ],
            ..Default::default()
        };
        for _ in 0..4 {
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
                .key_code(KeyCode::Down)
                .key_code(KeyCode::Enter)
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
        assert_eq!(
            actions
                .iter()
                .filter(|action| matches!(action, Action::ModalChoice(1)))
                .count(),
            1
        );
        assert!(actions.iter().all(|action| !matches!(action, Action::Send)));
        let custom = ui::Modal {
            title: "Decision".into(),
            editor: true,
            multiline: true,
            action_stamp: 22,
            ..Default::default()
        };
        for _ in 0..4 {
            backend.render(|ctx| {
                draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&custom),
                    &mut state,
                );
            });
        }
        backend.run_with_events(
            slt::EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                actions = draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&custom),
                    &mut state,
                );
            },
        );
        assert_eq!(modal_editor.lines().join("\n"), "\n");
        assert!(
            actions
                .iter()
                .all(|action| !matches!(action, Action::ModalSubmit | Action::Send))
        );
    }
    #[test]
    fn recovery_body_pages_without_activating_its_submit_choice() {
        let mut backend = slt::TestBackend::new(60, 18);
        let mut state = SurfaceState::new();
        let view = ui::View::default();
        let mut editor = ui::editor("draft", "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        let modal = ui::Modal {
            title: "Review answers".into(),
            action_stamp: 25,
            detail: (0..70)
                .map(|i| format!("Answer row {i}\n\n"))
                .collect::<String>(),
            items: vec![ui::ModalItem {
                label: "Submit answers".into(),
                ..Default::default()
            }],
            ..Default::default()
        };
        for _ in 0..4 {
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
                .key_code(KeyCode::PageDown)
                .key_code(KeyCode::Enter)
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
        assert!(state.modal_scroll.offset > 0);
        backend.run_with_events(
            slt::EventBuilder::new()
                .key_code(KeyCode::End)
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| {
                actions.extend(draw(
                    ctx,
                    &view,
                    &mut editor,
                    &mut modal_editor,
                    Some(&modal),
                    &mut state,
                ));
            },
        );
        assert!(
            actions
                .iter()
                .all(|a| !matches!(a, Action::ModalChoice(_) | Action::ModalSubmit))
        );
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
        backend.assert_contains("Answer row 69");
        assert_eq!(editor.lines().join("\n"), "draft");
    }

    #[test]
    fn hiding_navigator_reflows_real_transcript_and_composer_width() {
        let mut backend = slt::TestBackend::new(86, 31);
        let mut state = SurfaceState::new();
        state.active_key = "env/thread".into();
        let view = ui::View {
            messages: vec![ui::MessageView {
                key: "message".into(),
                text: "word ".repeat(60),
                ..Default::default()
            }],
            ..Default::default()
        };
        let mut editor = ui::editor("some wrapped draft ".repeat(8).as_str(), "violet", "");
        let mut modal_editor = ui::editor("", "violet", "");
        for _ in 0..4 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        let before = state.transcript_layout.as_ref().unwrap().width;
        let draft = editor.lines().join("\n");
        state.navigator_visible = Some(false);
        for _ in 0..4 {
            backend.render(|ctx| {
                draw(ctx, &view, &mut editor, &mut modal_editor, None, &mut state);
            });
        }
        assert_eq!(state.transcript_layout.as_ref().unwrap().width, before + 30);
        assert_eq!(state.composer.wrap_width, Some(83));
        assert_eq!(editor.lines().join("\n"), draft);
    }
}
