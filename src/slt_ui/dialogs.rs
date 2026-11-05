//! Bounded dialogs share SLT's list, editor and modal focus ownership.

use super::{
    Action, SurfaceState, elide_lines, primary_button, secondary_button, send_key, sync_editor,
};
use crate::{reports, ui};
use ratatui_textarea::TextArea;
use slt::{
    Align, Border, Context, KeyCode, ListState, ScrollState, Style, TextInputState, WidgetColors,
};
use unicode_segmentation::UnicodeSegmentation;
use unicode_width::UnicodeWidthStr;

/// Local widget state; native choices and retained drafts remain controller-owned.
#[derive(Debug, Default)]
pub(super) struct State {
    choices: ListState,
    search: TextInputState,
    description: ScrollState,
    detail: ScrollState,
    choice_detail: ScrollState,
    model: ModelPicker,
    direct_dismiss_tail: Option<(String, DismissalTail)>,
    reports: reports::Cache,
}

// This retains presentation only. The controller owns every staged value and index.
#[derive(Debug, Default)]
struct ModelPicker {
    configuration: Option<ui::Modal>,
    stamp: Option<u64>,
    phase: Option<ui::ModelDialog>,
    focus: String,
    expanded: bool,
    dismiss_tail: DismissalTail,
    values: ListState,
    indices: Vec<usize>,
    displayed_indices: Vec<usize>,
    geometry: Option<(u32, u32)>,
}

#[derive(Debug, Default, PartialEq, Eq)]
enum DismissalTail {
    #[default]
    None,
    Input,
    ActivationSettle,
}

#[derive(Clone)]
struct ModelField {
    name: String,
    label: String,
    value: String,
    choice: Option<usize>,
    staged: bool,
}

fn model_fields(modal: &ui::Modal) -> Vec<ModelField> {
    let mut fields = vec![ModelField {
        name: "model-field:model".into(),
        label: "Model".into(),
        value: modal
            .items
            .iter()
            .find(|item| matches!(item.role, ui::ModalItemRole::Model { .. }) && item.checked)
            .map_or("Unavailable", |item| item.label.as_str())
            .into(),
        choice: None,
        staged: modal.items.iter().any(|item| {
            matches!(item.role, ui::ModalItemRole::Model { .. }) && item.checked && !item.applied
        }),
    }];
    for (index, item) in modal.items.iter().enumerate() {
        if let ui::ModalItemRole::ModelOption { id, label, value } = &item.role {
            fields.push(ModelField {
                name: format!("model-field:option:{id}"),
                label: label.clone(),
                value: value.clone(),
                choice: Some(index),
                staged: !item.applied,
            });
        }
    }
    fields
}

fn reset_model_values(picker: &mut ModelPicker, modal: &ui::Modal) {
    picker.displayed_indices.clear();
    picker.indices = modal
        .items
        .iter()
        .enumerate()
        .filter_map(|(index, item)| {
            matches!(
                item.role,
                ui::ModalItemRole::Model { .. } | ui::ModalItemRole::ModelOptionValue { .. }
            )
            .then_some(index)
        })
        .collect();
    picker.values = ListState::new(
        picker
            .indices
            .iter()
            .map(|&index| modal.items[index].label.clone())
            .collect::<Vec<_>>(),
    );
    if let Some(selected) = picker
        .indices
        .iter()
        .position(|&index| modal.items[index].checked)
    {
        picker.values.set_selected_index(selected);
    }
}

// Only a directly dismissed native option owns this one deferred activation.
pub(super) fn direct_dismissal_tail(ctx: &mut Context, state: &mut SurfaceState, modal: bool) {
    let Some((source, mut tail)) = state.dialog.direct_dismiss_tail.take() else {
        return;
    };
    if modal || source != state.active_key || ctx.is_disabled() {
        return;
    }
    if ctx.raw_events().next().is_none() {
        tail = match tail {
            DismissalTail::ActivationSettle => DismissalTail::Input,
            _ => DismissalTail::None,
        };
    } else {
        let enter = ctx
            .key_presses_when(true)
            .find(|(_, key)| key.code == KeyCode::Enter && key.modifiers == slt::KeyModifiers::NONE)
            .map(|(index, _)| index);
        if let Some(index) = enter {
            ctx.consume_event(index);
            tail = DismissalTail::None;
        } else if ctx.key_presses_when(true).next().is_some() {
            tail = DismissalTail::None;
        }
    }
    if tail != DismissalTail::None {
        state.dialog.direct_dismiss_tail = Some((source, tail));
        ctx.request_frame();
    }
}

fn draw_model(
    ctx: &mut Context,
    modal: &ui::Modal,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    let mut picker = std::mem::take(&mut state.dialog.model);
    let new_session = state.modal_stamp.is_none();
    let changed = picker.stamp != Some(modal.action_stamp) || new_session;
    if new_session {
        picker.configuration = None;
    }
    let geometry = (ctx.width(), ctx.height());
    let resized = picker
        .geometry
        .replace(geometry)
        .is_some_and(|old| old != geometry);
    let option = match &modal.model {
        Some(ui::ModelDialog::OptionValues { id }) => Some(id.clone()),
        _ => None,
    };
    if changed {
        let returning =
            matches!(picker.phase, Some(ui::ModelDialog::OptionValues { .. })) && option.is_none();
        // A child dismissal owns its deferred activation only when returning
        // to the exact configuration; changed sources or offers reset it.
        if !returning
            || picker
                .configuration
                .as_ref()
                .is_none_or(|parent| parent.action_stamp != modal.action_stamp)
        {
            picker.dismiss_tail = DismissalTail::None;
        }
        picker.expanded = option.is_some();
        if let Some(id) = &option {
            picker.focus = format!("model-field:option:{id}");
        } else if !returning && !picker.focus.starts_with("model-field:") {
            picker.focus = "model-field:model".into();
        }
        reset_model_values(&mut picker, modal);
        picker.stamp = Some(modal.action_stamp);
        picker.phase.clone_from(&modal.model);
        ctx.request_frame();
    }
    if option.is_none() {
        picker.configuration = Some(modal.clone());
    }
    let fields = picker
        .configuration
        .as_ref()
        .map(model_fields)
        .unwrap_or_else(|| {
            vec![ModelField {
                name: picker.focus.clone(),
                label: modal.title.clone(),
                value: String::new(),
                choice: None,
                staged: false,
            }]
        });
    if !fields.iter().any(|field| field.name == picker.focus)
        && !picker.focus.starts_with("model-action:")
        && picker.focus != "model-close"
    {
        picker.focus.clone_from(&fields[0].name);
    }
    let apply = modal
        .items
        .iter()
        .position(|item| item.role == ui::ModalItemRole::ModelApply);
    let configuration = picker.configuration.as_ref().unwrap_or(modal);
    let controls: Vec<_> = configuration
        .items
        .iter()
        .enumerate()
        .filter_map(|(index, item)| {
            let label = match item.role {
                ui::ModalItemRole::ModelApply => "Apply ↵",
                ui::ModalItemRole::ModelSave => "☆ Save favorite",
                ui::ModalItemRole::ModelClear => "Clear",
                _ => return None,
            };
            Some((index, label, format!("model-action:{index}")))
        })
        .collect();
    let mut order: Vec<_> = fields.iter().map(|field| field.name.clone()).collect();
    order.extend(controls.iter().map(|(_, _, name)| name.clone()));
    order.push("model-close".into());
    let width = ctx.width().saturating_sub(4).clamp(1, 54);
    let inner = width.saturating_sub(4).max(1);
    let height = ctx.height().saturating_sub(2).max(1);
    let rows = height.saturating_sub(fields.len() as u32 + 7).clamp(1, 8);
    let mut focus_request = changed.then(|| {
        if picker.expanded {
            "model-values".into()
        } else {
            picker.focus.clone()
        }
    });
    let expanded_before = picker.expanded;
    let mut dispatched = false;
    ctx.modal_with(
        slt::ModalOpts::new()
            .anchor(slt::Anchor::BottomRight)
            .offset(2, 1),
        |ctx| {
            // SLT defers navigation tails; Escape/pointer activation inserts one
            // empty layout frame first. Retain ownership through that frame,
            // then let an input-free settle release a later deliberate Enter.
            if ctx.raw_events().next().is_none() {
                picker.dismiss_tail = match picker.dismiss_tail {
                    DismissalTail::ActivationSettle => DismissalTail::Input,
                    _ => DismissalTail::None,
                };
            }
            if picker.dismiss_tail != DismissalTail::None {
                ctx.request_frame();
            }
            if !picker.expanded
                && let Some(name) = ctx
                    .focused_name()
                    .filter(|name| order.iter().any(|item| item == name))
            {
                picker.focus = name.into();
            }
            let keys: Vec<_> = ctx
                .key_presses_when(true)
                .map(|(index, key)| (index, key.clone()))
                .collect();
            for (event, key) in keys {
                let plain = key.modifiers == slt::KeyModifiers::NONE;
                let backwards = key.code == KeyCode::BackTab
                    || key.code == KeyCode::Tab && key.modifiers.contains(slt::KeyModifiers::SHIFT);
                let owned = plain
                    && matches!(
                        key.code,
                        KeyCode::Enter
                            | KeyCode::Esc
                            | KeyCode::Up
                            | KeyCode::Down
                            | KeyCode::Left
                            | KeyCode::Right
                            | KeyCode::Home
                            | KeyCode::End
                            | KeyCode::PageUp
                            | KeyCode::PageDown
                            | KeyCode::Char(_)
                            | KeyCode::Backspace
                    )
                    || key.code == KeyCode::Tab
                    || backwards;
                if !owned {
                    continue;
                }
                if ctx.is_disabled() {
                    continue;
                }
                if changed || dispatched {
                    ctx.consume_event(event);
                    continue;
                }
                let dismiss_enter =
                    key.code == KeyCode::Enter && picker.dismiss_tail != DismissalTail::None;
                picker.dismiss_tail = DismissalTail::None;
                if dismiss_enter {
                    ctx.consume_event(event);
                    continue;
                }
                if key.code == KeyCode::Tab || backwards {
                    if picker.expanded && option.is_some() {
                        actions.push(Action::CloseModal);
                        dispatched = true;
                        picker.dismiss_tail = DismissalTail::Input;
                        if state.direct_effort_option {
                            state.dialog.direct_dismiss_tail =
                                Some((state.active_key.clone(), DismissalTail::Input));
                        }
                    } else if picker.expanded {
                        picker.dismiss_tail = DismissalTail::Input;
                    }
                    picker.expanded = false;
                    let position = order
                        .iter()
                        .position(|name| name == &picker.focus)
                        .unwrap_or(0);
                    let next = if backwards {
                        (position + order.len() - 1) % order.len()
                    } else {
                        (position + 1) % order.len()
                    };
                    picker.focus.clone_from(&order[next]);
                    focus_request = Some(picker.focus.clone());
                } else if picker.expanded {
                    match key.code {
                        KeyCode::Enter => {
                            if let Some(index) = picker
                                .values
                                .selected_index()
                                .and_then(|index| picker.indices.get(index))
                                .copied()
                            {
                                state.modal_selected = index;
                                actions.push(Action::ModalChoice(index));
                                picker.expanded = false;
                                dispatched = true;
                                focus_request = Some(picker.focus.clone());
                            }
                        }
                        KeyCode::Esc => {
                            picker.expanded = false;
                            if option.is_some() {
                                actions.push(Action::CloseModal);
                                dispatched = true;
                                picker.dismiss_tail = DismissalTail::ActivationSettle;
                                if state.direct_effort_option {
                                    state.dialog.direct_dismiss_tail = Some((
                                        state.active_key.clone(),
                                        DismissalTail::ActivationSettle,
                                    ));
                                }
                            } else {
                                picker.dismiss_tail = DismissalTail::ActivationSettle;
                            }
                            focus_request = Some(picker.focus.clone());
                        }
                        KeyCode::Up
                        | KeyCode::Down
                        | KeyCode::Home
                        | KeyCode::End
                        | KeyCode::PageUp
                        | KeyCode::PageDown => {
                            let current = picker.values.selected_view_index();
                            let last = picker.values.visible_indices().len().saturating_sub(1);
                            let next = match key.code {
                                KeyCode::Up => current.saturating_sub(1),
                                KeyCode::Down => (current + 1).min(last),
                                KeyCode::PageUp => current.saturating_sub(rows as usize),
                                KeyCode::PageDown => (current + rows as usize).min(last),
                                KeyCode::Home => 0,
                                _ => last,
                            };
                            picker.values.set_selected_view_index(next);
                        }
                        KeyCode::Char(ch) => {
                            let mut filter = picker.values.filter().to_owned();
                            if filter.len() < 256 {
                                filter.push(ch);
                                picker.values.set_filter(filter);
                            }
                        }
                        KeyCode::Backspace => {
                            let filter = picker.values.filter();
                            let end = filter
                                .grapheme_indices(true)
                                .next_back()
                                .map_or(0, |(end, _)| end);
                            picker.values.set_filter(filter[..end].to_owned());
                        }
                        _ => {}
                    }
                } else {
                    match key.code {
                        KeyCode::Enter => {
                            if let Some(index) = apply {
                                actions.push(Action::ModalChoice(index));
                                dispatched = true;
                            }
                        }
                        KeyCode::Esc => {
                            actions.push(Action::CloseModal);
                            dispatched = true;
                        }
                        KeyCode::Char(' ') => {
                            if let Some(field) =
                                fields.iter().find(|field| field.name == picker.focus)
                            {
                                if let Some(index) = field.choice {
                                    actions.push(Action::ModalChoice(index));
                                    dispatched = true;
                                } else {
                                    picker.expanded = true;
                                    reset_model_values(&mut picker, modal);
                                    focus_request = Some("model-values".into());
                                }
                            } else if let Some((index, _, _)) =
                                controls.iter().find(|(_, _, name)| *name == picker.focus)
                            {
                                actions.push(Action::ModalChoice(*index));
                                dispatched = true;
                            } else if picker.focus == "model-close" {
                                actions.push(Action::CloseModal);
                                dispatched = true;
                            }
                        }
                        KeyCode::Up | KeyCode::Down | KeyCode::Left | KeyCode::Right => {
                            let position = order
                                .iter()
                                .position(|name| name == &picker.focus)
                                .unwrap_or(0);
                            let previous = matches!(key.code, KeyCode::Up | KeyCode::Left);
                            let next = if previous {
                                (position + order.len() - 1) % order.len()
                            } else {
                                (position + 1) % order.len()
                            };
                            picker.focus.clone_from(&order[next]);
                            focus_request = Some(picker.focus.clone());
                        }
                        _ => {}
                    }
                }
                ctx.consume_event(event);
                ctx.request_frame();
            }
            // An expanded list owns every press. Dismissing it cannot activate a
            // configuration button or click through the modal into the workspace.
            let list_rect = ctx.measured_rect("model-values-region");
            let displayed_rows: Vec<_> = picker
                .displayed_indices
                .iter()
                .filter_map(|&index| {
                    ctx.measured_rect(&format!("model-value:{index}"))
                        .map(|rect| (rect, index))
                })
                .collect();
            let mouse: Vec<_> = ctx
                .raw_events()
                .enumerate()
                .filter_map(|(index, event)| match event {
                    slt::Event::Mouse(mouse) => Some((index, mouse.clone())),
                    _ => None,
                })
                .collect();
            for (event_index, mouse) in mouse {
                if ctx.is_disabled()
                    || changed
                    || resized
                    || dispatched
                    || expanded_before != picker.expanded
                {
                    ctx.consume_event(event_index);
                } else if expanded_before
                    && matches!(mouse.kind, slt::MouseKind::Down(slt::MouseButton::Left))
                    && let Some((_, index)) = displayed_rows
                        .iter()
                        .find(|(rect, _)| rect.contains(mouse.x, mouse.y))
                {
                    ctx.consume_event(event_index);
                    actions.push(Action::ModalChoice(*index));
                    state.modal_selected = *index;
                    picker.expanded = false;
                    dispatched = true;
                    focus_request = Some(picker.focus.clone());
                    ctx.request_frame();
                } else if expanded_before
                    && !list_rect.is_some_and(|rect| rect.contains(mouse.x, mouse.y))
                {
                    ctx.consume_event(event_index);
                    if matches!(mouse.kind, slt::MouseKind::Down(slt::MouseButton::Left)) {
                        picker.expanded = false;
                        if option.is_some() {
                            actions.push(Action::CloseModal);
                            dispatched = true;
                            picker.dismiss_tail = DismissalTail::ActivationSettle;
                            if state.direct_effort_option {
                                state.dialog.direct_dismiss_tail = Some((
                                    state.active_key.clone(),
                                    DismissalTail::ActivationSettle,
                                ));
                            }
                        } else {
                            picker.dismiss_tail = DismissalTail::ActivationSettle;
                        }
                        focus_request = Some(picker.focus.clone());
                        ctx.request_frame();
                    }
                }
            }
            let theme = *ctx.theme();
            ctx.group("t3oxide.model")
                .w(width)
                .max_h(height)
                .bg(theme.surface)
                .border(Border::Rounded)
                .border_style(Style::new().fg(theme.border))
                .focus_within_border_fg(theme.primary)
                .px(1)
                .col(|ctx| {
                    ctx.text(&modal.title).w(inner).truncate();
                    for field in &fields {
                        let focused = ctx.register_focusable_named(&field.name);
                        ctx.register_focusable();
                        let hit = ctx
                            .container()
                            .w(inner)
                            .h(1)
                            .bg(if focused && !picker.expanded {
                                theme.surface_hover
                            } else {
                                theme.surface
                            })
                            .row(|ctx| {
                                ctx.text(if focused && !picker.expanded {
                                    "│"
                                } else {
                                    " "
                                })
                                .fg(theme.primary);
                                ctx.text(&field.label)
                                    .w(10.min(inner / 2))
                                    .fg(if focused && !picker.expanded {
                                        theme.primary
                                    } else {
                                        theme.surface_text
                                    })
                                    .truncate();
                                ctx.text(format!(
                                    "{}{} ▾",
                                    if field.staged { "· " } else { "" },
                                    field.value
                                ))
                                .grow(1)
                                .truncate()
                                .fg(theme.text);
                            });
                        if hit.clicked && !changed && !dispatched && !expanded_before {
                            picker.focus.clone_from(&field.name);
                            if let Some(index) = field.choice {
                                actions.push(Action::ModalChoice(index));
                                dispatched = true;
                            } else {
                                picker.expanded = true;
                                reset_model_values(&mut picker, modal);
                                focus_request = Some("model-values".into());
                                ctx.request_frame();
                            }
                        }
                    }
                    ctx.group("model-values-region")
                        .w(inner)
                        .h(if picker.expanded { rows + 2 } else { 0 })
                        .col(|ctx| {
                            let displayed = std::cell::RefCell::new(Vec::new());
                            let selected = picker.values.selected_index();
                            let labels: Vec<_> = picker
                                .indices
                                .iter()
                                .map(|&index| {
                                    let item = &modal.items[index];
                                    format!(
                                        "{} {}{}",
                                        if item.checked { "✓" } else { " " },
                                        item.label,
                                        if item.applied { "  •" } else { "" }
                                    )
                                })
                                .collect();
                            ctx.disabled(!picker.expanded || changed, |ctx| {
                                ctx.register_focusable_named("model-values");
                                ctx.container().h(1).col(|ctx| {
                                    if picker.expanded {
                                        ctx.text(if picker.values.filter().is_empty() {
                                            "Filter…"
                                        } else {
                                            picker.values.filter()
                                        })
                                        .fg(theme.text_dim)
                                        .truncate();
                                    }
                                });
                                ctx.virtual_list(
                                    &mut picker.values,
                                    rows.saturating_sub(2).max(1),
                                    |ctx, index| {
                                        let original = picker.indices[index];
                                        displayed.borrow_mut().push(original);
                                        ctx.group(&format!("model-value:{original}"))
                                            .w(inner)
                                            .h(1)
                                            .col(|ctx| {
                                                ctx.text(elide_lines(
                                                    ctx,
                                                    &labels[index],
                                                    inner,
                                                    1,
                                                ))
                                                .w(inner)
                                                .bg(if Some(index) == selected {
                                                    theme.selected_bg
                                                } else {
                                                    theme.surface
                                                })
                                                .fg(if Some(index) == selected {
                                                    theme.selected_fg
                                                } else {
                                                    theme.text
                                                });
                                            });
                                    },
                                );
                                if picker.expanded && picker.values.visible_indices().is_empty() {
                                    ctx.text("No matches").fg(theme.text_dim);
                                }
                            });
                            picker.displayed_indices = displayed.into_inner();
                        });
                    ctx.disabled(
                        picker.expanded || expanded_before || changed || dispatched,
                        |ctx| {
                            ctx.container()
                                .w(inner)
                                .wrap()
                                .gap(1)
                                .row_gap(0)
                                .row(|ctx| {
                                    for (index, label, name) in &controls {
                                        ctx.register_focusable_named(name);
                                        let hit = if modal.items.get(*index).is_some_and(|item| {
                                            item.role == ui::ModalItemRole::ModelApply
                                        }) {
                                            primary_button(ctx, label, true)
                                        } else {
                                            secondary_button(ctx, label)
                                        };
                                        if hit.clicked && !dispatched {
                                            actions.push(Action::ModalChoice(*index));
                                            dispatched = true;
                                        }
                                    }
                                    ctx.register_focusable_named("model-close");
                                    if secondary_button(ctx, "×").clicked && !dispatched {
                                        actions.push(Action::CloseModal);
                                        dispatched = true;
                                    }
                                });
                        },
                    );
                });
            if let Some(name) = &focus_request {
                ctx.focus_by_name(name);
                ctx.request_frame();
            }
        },
    );
    state.modal_stamp = Some(modal.action_stamp);
    picker.phase.clone_from(&modal.model);
    state.dialog.model = picker;
}

pub(super) fn draw(
    ctx: &mut Context,
    modal: &ui::Modal,
    legacy: &mut TextArea<'static>,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    if modal.model.is_some() {
        draw_model(ctx, modal, state, actions);
        return;
    }
    state.dialog.model.expanded = false;
    state.dialog.model.configuration = None;
    let palette = modal.editor && modal.title == "Commands";
    let search = modal.editor && modal.title == "Search conversations";
    let opening = state.modal_stamp.is_none();
    let changed = state.modal_stamp != Some(modal.action_stamp);
    let natural_width = modal
        .items
        .iter()
        .map(|item| item.label.width() as u32 + 8)
        .max()
        .unwrap_or(36)
        .max(36);
    let width = ctx.width().saturating_sub(4).clamp(
        1,
        if palette {
            60
        } else if !modal.editor && modal.detail.is_empty() {
            natural_width.min(64)
        } else {
            78
        },
    );
    let height = ctx.height().saturating_sub(2).max(1);
    let inner_width = width.saturating_sub(4).max(1);
    let spacing = u32::from(height >= 22 && !modal.detail.is_empty());
    if changed {
        state.modal_selected = modal.selected.min(modal.items.len().saturating_sub(1));
        state.dialog.description = ScrollState::new();
        state.dialog.detail = ScrollState::new();
        state.dialog.choice_detail = ScrollState::new();
        ctx.request_frame();
    }
    let choices: Vec<usize> = modal
        .items
        .iter()
        .enumerate()
        .filter_map(|(index, item)| (!item.action).then_some(index))
        .collect();
    let form_actions: Vec<usize> = modal
        .items
        .iter()
        .enumerate()
        .filter_map(|(index, item)| item.action.then_some(index))
        .collect();
    let footer_actions = &form_actions;
    let rows: Vec<String> = choices
        .iter()
        .map(|&index| {
            let item = &modal.items[index];
            let mut row = fit(
                &format!("{} {}", if item.checked { "✓" } else { " " }, item.label),
                inner_width.saturating_sub(2) as usize,
            );
            let padding = inner_width.saturating_sub(2) as usize - row.width();
            row.extend(std::iter::repeat_n(' ', padding));
            row
        })
        .collect();
    if state.dialog.choices.items() != rows {
        state.dialog.choices.set_items(rows);
    }
    state.dialog.choices.set_selected_index(
        choices
            .iter()
            .position(|index| *index == state.modal_selected)
            .unwrap_or(0),
    );
    if palette {
        let value = legacy.lines().join(" ");
        if state.dialog.search.value != value {
            state.dialog.search.set_value(value);
        }
        state.dialog.search.placeholder = "Search commands".into();
    } else if modal.editor {
        sync_editor(&mut state.modal_editor, legacy);
        state.modal_editor.wrap_width = Some(if search {
            inner_width.saturating_sub(2).max(1)
        } else {
            inner_width
        });
    }

    ctx.modal(|ctx| {
        let theme = *ctx.theme();
        ctx.group("t3oxide.modal")
            .w(width)
            .max_h(height)
            .border(Border::Rounded)
            .border_style(Style::new().fg(theme.border))
            .focus_within_border_fg(theme.primary)
            .bg(theme.bg)
            .align(Align::Start)
            .p(1)
            .gap(spacing)
            .col(|ctx| {
                ctx.container().w(inner_width).bg(theme.surface).row(|ctx| {
                    ctx.text(&modal.title).truncate().grow(1);
                    if !choices.is_empty() && form_actions.is_empty() {
                        ctx.text(format!(
                            "{}/{}",
                            state.dialog.choices.selected_index().unwrap_or(0) + 1,
                            choices.len()
                        ))
                        .dim();
                    }
                });
                if modal.editor && modal.title == "Search conversations" {
                    ctx.container().w(inner_width).gap(1).row(|ctx| {
                        ctx.register_focusable_named("search-scope");
                        if secondary_button(ctx, "Scope").clicked && !changed {
                            actions.push(Action::Command("search-scope".into()));
                        }
                        ctx.text(&state.browsing_scope)
                            .w(inner_width.saturating_sub(10))
                            .truncate();
                    });
                }

                let description_rows = if palette || modal.description.is_empty() {
                    0
                } else {
                    wrapped_rows(&modal.description, inner_width)
                        .min(3)
                        .min(height.saturating_sub(10).max(1))
                };
                if description_rows > 0 {
                    let overflowing =
                        wrapped_rows(&modal.description, inner_width) > description_rows;
                    let focused = overflowing && ctx.register_focusable_named("modal-description");
                    if overflowing {
                        ctx.register_focusable();
                        detail_navigation(
                            ctx,
                            focused,
                            &mut state.dialog.description,
                            description_rows,
                        );
                    }
                    ctx.scrollable(&mut state.dialog.description)
                        .h(description_rows)
                        .w(inner_width)
                        .bg(if focused { theme.surface } else { theme.bg })
                        .col(|ctx| {
                            ctx.text(&modal.description).wrap();
                        });
                }

                let editor_rows = if palette || search {
                    3
                } else if modal.editor {
                    let visual_rows =
                        u32::try_from(state.modal_editor.visual_line_count()).unwrap_or(u32::MAX);
                    visual_rows.clamp(3, height.saturating_sub(10).clamp(3, 8))
                } else {
                    0
                };
                if palette {
                    let focused = ctx.register_focusable_named("modal-editor");
                    palette_navigation(ctx, focused, modal, state, actions);
                    let mut response = slt::Response::default();
                    ctx.group("t3oxide.query").w(inner_width).col(|ctx| {
                        response = ctx.text_input(&mut state.dialog.search);
                    });
                    if response.changed {
                        *legacy = ui::editor(&state.dialog.search.value, "violet", "");
                        state.modal_selected = 0;
                        state.dialog.choices.set_selected_index(0);
                        actions.push(Action::ModalEditChanged);
                    }
                } else if modal.editor {
                    let focused = ctx.register_focusable_named("modal-editor");
                    if !changed {
                        let submits: Vec<_> = ctx
                            .key_presses_when(focused)
                            .filter(|(_, key)| send_key(&key.code, key.modifiers))
                            .map(|(index, _)| index)
                            .collect();
                        if !submits.is_empty() {
                            for index in submits {
                                ctx.consume_event(index);
                            }
                            actions.push(Action::ModalSubmit);
                        }
                    }
                    let response = if search {
                        let mut response = slt::Response::default();
                        ctx.group("t3oxide.query")
                            .w(inner_width)
                            .border(Border::Rounded)
                            .col(|ctx| {
                                response = ctx.disabled(changed, |ctx| {
                                    ctx.textarea(&mut state.modal_editor, 1)
                                });
                            });
                        response
                    } else {
                        ctx.disabled(changed, |ctx| {
                            ctx.textarea(&mut state.modal_editor, editor_rows)
                        })
                    };
                    if response.changed {
                        *legacy = ui::editor(&state.modal_editor.value(), "violet", "");
                        actions.push(Action::ModalEditChanged);
                    }
                }

                let available = height
                    .saturating_sub(7 + description_rows + editor_rows + spacing * 5)
                    .max(1);
                if !choices.is_empty() {
                    let has_detail = !modal.detail.is_empty();
                    let detail_rows = if has_detail {
                        (available / 3).max(1)
                    } else {
                        0
                    };
                    let list_rows = available.saturating_sub(detail_rows + 2).clamp(1, 12);
                    let focused = ctx.register_focusable_named("modal-choices");
                    list_navigation(ctx, focused, &mut state.dialog.choices, list_rows);
                    // Selection remains visible while the palette input owns focus.
                    let colors = WidgetColors::new().accent(theme.selected_bg);
                    let mut response = slt::Response::default();
                    ctx.container()
                        .w(inner_width)
                        .h(list_rows.min(choices.len() as u32))
                        .bg(theme.surface)
                        .col(|ctx| {
                            response = ctx.list_colored(&mut state.dialog.choices, &colors);
                        });
                    let selected = choices[state.dialog.choices.selected_index().unwrap_or(0)];
                    if selected != state.modal_selected {
                        state.dialog.choice_detail = ScrollState::new();
                    }
                    if !changed
                        && (focused
                            || response.clicked
                            || response.changed
                            || choices.contains(&state.modal_selected))
                    {
                        state.modal_selected = selected;
                    }
                    if response.changed {
                        ctx.request_frame();
                    }
                    // The list uses one focus slot and its own row hit map.
                    // A changed offer may have different actions at old coordinates.
                    let activate = !changed && response.clicked;
                    if activate
                        || focused
                            && !changed
                            && (ctx.consume_key_code(KeyCode::Enter) || ctx.consume_key(' '))
                    {
                        actions.push(Action::ModalChoice(selected));
                    }
                    let description = &modal.items[selected].description;
                    let show_description = !description.is_empty()
                        && (!palette
                            || state.dialog.search.value.split_whitespace().any(|word| {
                                !modal.items[selected]
                                    .label
                                    .to_lowercase()
                                    .contains(&word.to_lowercase())
                            }));
                    let description_rows = if palette {
                        // A secondary match must not recenter the modal and
                        // move rows underneath an already queued pointer.
                        available.saturating_sub(list_rows).clamp(1, 2)
                    } else if show_description {
                        wrapped_rows(description, inner_width)
                            .min(2)
                            .min(available.saturating_sub(list_rows).max(1))
                    } else {
                        0
                    };
                    // Reserve the same container slot when a selected command
                    // gains/loses secondary text, so queued clicks keep identity.
                    ctx.scrollable(&mut state.dialog.choice_detail)
                        .h(description_rows)
                        .w(inner_width)
                        .col(|ctx| {
                            if show_description {
                                let concise = if palette {
                                    elide_lines(ctx, description, inner_width, description_rows)
                                } else {
                                    description.clone()
                                };
                                ctx.text(concise).dim().wrap();
                            }
                        });
                    if has_detail {
                        let focused = ctx.register_focusable_named("modal-detail");
                        ctx.register_focusable();
                        detail_navigation(ctx, focused, &mut state.dialog.detail, detail_rows);
                        draw_detail(
                            ctx,
                            modal,
                            &mut state.dialog.detail,
                            &mut state.dialog.reports,
                            inner_width,
                            detail_rows,
                        );
                    }
                } else if !modal.detail.is_empty() {
                    let focused = ctx.register_focusable_named("modal-detail");
                    ctx.register_focusable();
                    detail_navigation(ctx, focused, &mut state.dialog.detail, available);
                    draw_detail(
                        ctx,
                        modal,
                        &mut state.dialog.detail,
                        &mut state.dialog.reports,
                        inner_width,
                        available,
                    );
                } else if palette {
                    ctx.text("No matching commands").dim();
                }

                ctx.container()
                    .w(inner_width)
                    .wrap()
                    .gap(1)
                    .row_gap(0)
                    .row(|ctx| {
                        for (position, &index) in footer_actions.iter().enumerate() {
                            let item = &modal.items[index];
                            ctx.register_focusable_named(&format!("modal-action-{index}"));
                            let response = if position == 0 {
                                primary_button(ctx, &item.label, true)
                            } else {
                                secondary_button(ctx, &item.label)
                            };
                            if response.clicked && !changed {
                                actions.push(Action::ModalChoice(index));
                            }
                        }
                        ctx.register_focusable_named("modal-close");
                        if secondary_button(ctx, "Close").clicked && !changed {
                            actions.push(Action::CloseModal);
                        }
                        if modal.editor && !palette {
                            ctx.spacer();
                            ctx.register_focusable_named("modal-submit");
                            if primary_button(ctx, "Continue", true).clicked && !changed {
                                actions.push(Action::ModalSubmit);
                            }
                        }
                    });
                if ctx.consume_key_code(KeyCode::Esc) {
                    actions.push(Action::CloseModal);
                }
            });
    });
    state.modal_stamp = Some(modal.action_stamp);
    if opening || changed && !palette {
        let focus = if modal.editor {
            "modal-editor".into()
        } else if footer_actions.contains(&state.modal_selected) {
            format!("modal-action-{}", state.modal_selected)
        } else if !choices.is_empty() {
            "modal-choices".into()
        } else if !modal.detail.is_empty() {
            "modal-detail".into()
        } else {
            "modal-close".into()
        };
        ctx.focus_by_name(&focus);
        ctx.request_frame();
    }
}

fn detail_navigation(ctx: &mut Context, focused: bool, scroll: &mut ScrollState, rows: u32) {
    let mut consumed = Vec::new();
    for (index, key) in ctx.key_presses_when(focused) {
        match key.code {
            KeyCode::Up => scroll.scroll_up(1),
            KeyCode::Down => scroll.scroll_down(1),
            KeyCode::PageUp => scroll.scroll_up(rows as usize),
            KeyCode::PageDown => scroll.scroll_down(rows as usize),
            KeyCode::Home => scroll.offset = 0,
            KeyCode::End => scroll.offset = scroll.max_offset(),
            _ => continue,
        }
        consumed.push(index);
    }
    if !consumed.is_empty() {
        ctx.request_frame();
        for index in consumed {
            ctx.consume_event(index);
        }
    }
}

fn wrapped_rows(text: &str, columns: u32) -> u32 {
    text.lines()
        .map(|line| (line.width().max(1) as u32).div_ceil(columns.max(1)))
        .sum::<u32>()
        .max(1)
}

fn draw_detail(
    ctx: &mut Context,
    modal: &ui::Modal,
    scroll: &mut ScrollState,
    reports: &mut reports::Cache,
    width: u32,
    height: u32,
) {
    ctx.scrollable(scroll).h(height).w(width).col(|ctx| {
        reports.draw(
            ctx,
            &format!("modal:{}", modal.title),
            &modal.action_stamp.to_string(),
            &modal.detail,
            reports::Geometry { width, height },
        );
    });
}

fn list_navigation(ctx: &mut Context, focused: bool, list: &mut ListState, rows: u32) {
    let mut selected = list.selected_index().unwrap_or(0);
    let last = list.len().saturating_sub(1);
    let mut consumed = Vec::new();
    for (index, key) in ctx.key_presses_when(focused) {
        selected = match key.code {
            KeyCode::Home => 0,
            KeyCode::End => last,
            KeyCode::PageUp => selected.saturating_sub(rows as usize),
            KeyCode::PageDown => selected.saturating_add(rows as usize).min(last),
            _ => continue,
        };
        consumed.push(index);
    }
    list.set_selected_index(selected);
    if !consumed.is_empty() {
        ctx.request_frame();
        for index in consumed {
            ctx.consume_event(index);
        }
    }
}

fn palette_navigation(
    ctx: &mut Context,
    focused: bool,
    modal: &ui::Modal,
    state: &mut SurfaceState,
    actions: &mut Vec<Action>,
) {
    let last = modal.items.len().saturating_sub(1);
    let mut consumed = Vec::new();
    for (index, key) in ctx.key_presses_when(focused) {
        match key.code {
            KeyCode::Up => state.modal_selected = state.modal_selected.saturating_sub(1),
            KeyCode::Down => {
                state.modal_selected = state.modal_selected.saturating_add(1).min(last)
            }
            KeyCode::PageUp => state.modal_selected = state.modal_selected.saturating_sub(8),
            KeyCode::PageDown => {
                state.modal_selected = state.modal_selected.saturating_add(8).min(last)
            }
            KeyCode::Enter if !modal.items.is_empty() => {
                actions.push(Action::ModalChoice(state.modal_selected.min(last)));
            }
            // Empty command results must not turn Enter into a text submission.
            KeyCode::Enter => {}
            _ => continue,
        }
        consumed.push(index);
    }
    state
        .dialog
        .choices
        .set_selected_index(state.modal_selected);
    if !consumed.is_empty() {
        ctx.request_frame();
        for index in consumed {
            ctx.consume_event(index);
        }
    }
}

fn fit(text: &str, columns: usize) -> String {
    if text.width() <= columns {
        return text.to_owned();
    }
    let mut result = String::new();
    let mut width = 0;
    for cluster in text.graphemes(true) {
        if width + cluster.width() >= columns {
            break;
        }
        result.push_str(cluster);
        width += cluster.width();
    }
    if columns > 0 {
        result.push('…');
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use slt::{EventBuilder, TestBackend};

    fn menu() -> ui::Modal {
        ui::Modal {
            title: "Choose model".into(),
            items: (0..40)
                .map(|index| ui::ModalItem {
                    label: format!("Choice {index:02}"),
                    checked: index == 3,
                    ..ui::ModalItem::default()
                })
                .collect(),
            action_stamp: 1,
            ..ui::Modal::default()
        }
    }

    fn warm(
        backend: &mut TestBackend,
        modal: &ui::Modal,
        editor: &mut TextArea<'static>,
        state: &mut SurfaceState,
    ) {
        for _ in 0..3 {
            backend.render(|ctx| draw(ctx, modal, editor, state, &mut Vec::new()));
        }
    }

    #[test]
    fn menu_arrows_reveal_selection_and_tab_has_one_list_stop() {
        let mut backend = TestBackend::new(90, 24);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("", "violet", "");
        let modal = menu();
        warm(&mut backend, &modal, &mut editor, &mut state);
        backend.assert_contains("✓ Choice 03");
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::End).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
        });
        backend.assert_contains("Choice 39");
        backend.assert_not_contains("↑");
        let mut actions = Vec::new();
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            },
        );
        assert!(matches!(actions.as_slice(), [Action::ModalChoice(39)]));
        actions.clear();
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Tab).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            },
        );
        assert!(matches!(actions.as_slice(), [Action::CloseModal]));
    }

    #[test]
    fn mouse_selects_the_displayed_row_and_changed_offer_rejects_old_hit() {
        let mut backend = TestBackend::new(90, 24);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("", "violet", "");
        let mut modal = menu();
        warm(&mut backend, &modal, &mut editor, &mut state);
        let (x, y) = backend
            .find_text("Choice 03")
            .expect("checked model row is visible");
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(matches!(actions.as_slice(), [Action::ModalChoice(3)]));
        modal.action_stamp += 1;
        actions.clear();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(
            actions.is_empty(),
            "old coordinates cannot choose a changed native offer"
        );
    }

    #[test]
    fn native_next_option_and_form_actions_keep_distinct_rows_and_original_indices() {
        for (width, height) in [(140, 38), (70, 22), (40, 12)] {
            let mut backend = TestBackend::new(width, height);
            let mut state = SurfaceState::new();
            let mut editor = ui::editor("retained answer", "violet", "");
            let mut modal = ui::Modal {
                title: "Choose release".into(),
                description: "Which release should ship?".into(),
                items: vec![
                    ui::ModalItem {
                        label: "Back".into(),
                        action: true,
                        ..Default::default()
                    },
                    ui::ModalItem {
                        label: "Next".into(),
                        checked: true,
                        ..Default::default()
                    },
                    ui::ModalItem {
                        label: "Next".into(),
                        action: true,
                        ..Default::default()
                    },
                ],
                selected: 1,
                action_stamp: 5,
                ..Default::default()
            };
            warm(&mut backend, &modal, &mut editor, &mut state);
            backend.assert_contains("Which release should ship?");
            backend.assert_contains("✓ Next");
            assert_eq!(state.dialog.choices.len(), 1);
            let (x, y) = backend.find_text("✓ Next").unwrap();
            let mut actions = Vec::new();
            backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            });
            assert!(matches!(actions.as_slice(), [Action::ModalChoice(1)]));
            actions.clear();
            backend.run_with_events(EventBuilder::new().key_code(KeyCode::Tab).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            });
            backend.run_with_events(
                EventBuilder::new().key_code(KeyCode::Enter).build(),
                |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                },
            );
            assert!(matches!(actions.as_slice(), [Action::ModalChoice(0)]));
            actions.clear();
            modal.selected = 2;
            modal.action_stamp += 1;
            warm(&mut backend, &modal, &mut editor, &mut state);
            backend.run_with_events(
                EventBuilder::new().key_code(KeyCode::Enter).build(),
                |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                },
            );
            assert!(
                matches!(actions.as_slice(), [Action::ModalChoice(2)]),
                "{width}x{height}: {actions:?}"
            );
            actions.clear();
            let painted = backend.to_string_trimmed();
            let (y, line) = painted
                .lines()
                .enumerate()
                .filter(|(_, line)| line.contains(" Next "))
                .last()
                .expect("navigation action has its own button target");
            let x = line.find(" Next ").unwrap() as u32;
            let y = y as u32;
            backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            });
            assert!(matches!(actions.as_slice(), [Action::ModalChoice(2)]));
            assert_eq!(editor.lines().join("\n"), "retained answer");
        }
    }

    #[test]
    fn help_table_preserves_the_input_column_in_the_modal_width() {
        for width in [140, 100, 70, 40] {
            let mut backend = TestBackend::new(width, 26);
            let mut state = SurfaceState::new();
            let mut editor = ui::editor("", "violet", "");
            let modal = ui::Modal {
                title: "Keyboard help".into(),
                detail: "| Action | Input |\n| --- | --- |\n| Prompt newline | Enter |\n| Send in Rex | Ctrl+Enter |\n| Other Send keys, when forwarded | Cmd+Enter, Shift+Enter, Cmd+Shift+Enter |\n| Copy whole prompt draft | /copy-draft |".into(),
                action_stamp: 6,
                ..Default::default()
            };
            warm(&mut backend, &modal, &mut editor, &mut state);
            backend.assert_contains("Input");
            let mut painted = backend.to_string_trimmed();
            backend.run_with_events(EventBuilder::new().key_code(KeyCode::End).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
            });
            painted.push_str(&backend.to_string_trimmed());
            for expected in ["Ctrl+Enter", "Shift+Enter", "/copy-draft"] {
                assert!(
                    painted.contains(expected),
                    "missing {expected} at width {width}: {painted}"
                );
            }
        }
    }

    #[test]
    fn command_search_owns_enter_and_scrolls_without_continue_or_mechanics() {
        let mut backend = TestBackend::new(54, 20);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("", "violet", "");
        let mut modal = menu();
        modal.title = "Commands".into();
        modal.editor = true;
        modal.description = "Actions use the current source. Type to filter.".into();
        warm(&mut backend, &modal, &mut editor, &mut state);
        backend.assert_not_contains("Continue");
        backend.assert_not_contains("Actions use");
        for _ in 0..18 {
            backend.run_with_events(EventBuilder::new().key_code(KeyCode::Down).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
            });
        }
        backend.assert_contains("Choice 18");
        let mut actions = Vec::new();
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            },
        );
        assert!(matches!(actions.as_slice(), [Action::ModalChoice(18)]));
        assert_eq!(editor.lines().join("\n"), "");
    }

    #[test]
    fn detail_scrolls_by_keyboard_in_a_compact_dialog() {
        let mut backend = TestBackend::new(40, 16);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("", "violet", "");
        let modal = ui::Modal {
            title: "Evidence".into(),
            detail: (0..30).map(|i| format!("Record {i:02}\n")).collect(),
            action_stamp: 2,
            ..ui::Modal::default()
        };
        warm(&mut backend, &modal, &mut editor, &mut state);
        backend.assert_contains("Record 00");
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::End).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
        });
        backend.assert_contains("Record 29");
        backend.assert_not_contains("Record 00");
    }

    #[test]
    fn recovery_choices_leave_the_complete_payload_keyboard_accessible() {
        for (width, height) in [(90, 24), (40, 16)] {
            let mut backend = TestBackend::new(width, height);
            let mut state = SurfaceState::new();
            let mut editor = ui::editor("Retained prompt 日本語", "violet", "");
            let modal = ui::Modal {
                title: "Review saved action".into(),
                detail: format!(
                    "PAYLOAD-START\n\n{}PAYLOAD-END 日本語",
                    (0..40)
                        .map(|index| format!("field{index:02}: Saved résumé\n\n"))
                        .collect::<String>()
                ),
                items: ["Keep pending", "Reconcile", "Clear"]
                    .into_iter()
                    .map(|label| ui::ModalItem {
                        label: label.into(),
                        ..Default::default()
                    })
                    .collect(),
                action_stamp: 9,
                ..Default::default()
            };
            warm(&mut backend, &modal, &mut editor, &mut state);
            backend.assert_contains("PAYLOAD-START");
            let mut actions = Vec::new();
            for code in [KeyCode::Tab, KeyCode::PageDown, KeyCode::End] {
                backend.run_with_events(EventBuilder::new().key_code(code).build(), |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                });
            }
            backend.assert_contains("PAYLOAD-END");
            assert_eq!(state.modal_selected, 0);
            assert!(
                actions.is_empty(),
                "payload navigation cannot recover or clear an operation"
            );
            backend.run_with_events(EventBuilder::new().key_code(KeyCode::Home).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            });
            backend.assert_contains("PAYLOAD-START");
            assert!(actions.is_empty());
            assert_eq!(editor.lines().join("\n"), "Retained prompt 日本語");
        }
    }

    #[test]
    fn answer_review_reaches_all_fields_with_keyboard_without_submitting() {
        for (width, height) in [(90, 24), (40, 16)] {
            let mut backend = TestBackend::new(width, height);
            let mut state = SurfaceState::new();
            let mut editor = ui::editor("", "violet", "");
            let modal = ui::Modal {
                title: "Review answers".into(),
                description: format!(
                    "First question\n{}\nSecond question\nLast answer 日本語 🧑‍💻",
                    "Exact multiline answer café\n".repeat(8)
                ),
                items: vec![ui::ModalItem {
                    label: "Submit answers".into(),
                    action: true,
                    ..ui::ModalItem::default()
                }],
                action_stamp: 4,
                ..ui::Modal::default()
            };
            warm(&mut backend, &modal, &mut editor, &mut state);
            backend.assert_contains("First question");
            let mut actions = Vec::new();
            backend.run_with_events(
                EventBuilder::new().key_code(KeyCode::BackTab).build(),
                |ctx| draw(ctx, &modal, &mut editor, &mut state, &mut actions),
            );
            backend.run_with_events(
                EventBuilder::new().key_code(KeyCode::PageDown).build(),
                |ctx| draw(ctx, &modal, &mut editor, &mut state, &mut actions),
            );
            backend.run_with_events(EventBuilder::new().key_code(KeyCode::End).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            });
            backend.assert_contains("Last answer");
            assert!(
                actions.is_empty(),
                "review navigation cannot submit an answer"
            );
            backend.run_with_events(EventBuilder::new().key_code(KeyCode::Home).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions)
            });
            backend.assert_contains("First question");
        }
    }

    #[test]
    fn palette_selection_stays_visible_while_the_query_owns_keyboard_focus() {
        let mut backend = TestBackend::new(100, 31);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("", "violet", "");
        let mut modal = menu();
        modal.title = "Commands".into();
        modal.editor = true;
        let theme = super::super::surface_theme("violet");
        let mut focus = None;
        for _ in 0..3 {
            backend.render(|ctx| {
                ctx.set_theme(theme);
                draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
                focus = ctx.focused_name().map(str::to_owned);
            });
        }
        assert_eq!(focus.as_deref(), Some("modal-editor"));
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Down).build(), |ctx| {
            ctx.set_theme(theme);
            draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
            focus = ctx.focused_name().map(str::to_owned);
        });
        assert_eq!(focus.as_deref(), Some("modal-editor"));
        let (x, y) = backend.find_text("Choice 01").unwrap();
        let selected = backend.buffer().get(x, y).style;
        assert_eq!(selected.bg, Some(theme.selected_bg));
        assert_eq!(selected.fg, Some(theme.selected_fg));
        let (x, y) = backend.find_text("Choice 00").unwrap();
        assert_ne!(backend.buffer().get(x, y).style.bg, selected.bg);
    }

    fn configuration() -> ui::Modal {
        ui::Modal {
            title: "Model · current conversation".into(),
            model: Some(ui::ModelDialog::Configuration),
            items: vec![
                ui::ModalItem {
                    label: "Apply reviewed selection".into(),
                    role: ui::ModalItemRole::Model {
                        native_id: "native-a".into(),
                    },
                    checked: true,
                    applied: true,
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Reasoning".into(),
                    role: ui::ModalItemRole::Model {
                        native_id: "native-b".into(),
                    },
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Reasoning: Medium".into(),
                    role: ui::ModalItemRole::ModelOption {
                        id: "effort-native".into(),
                        label: "Reasoning".into(),
                        value: "Medium".into(),
                    },
                    applied: true,
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Speed: Native default".into(),
                    role: ui::ModalItemRole::ModelOption {
                        id: "speed-native".into(),
                        label: "Speed".into(),
                        value: "Native default".into(),
                    },
                    applied: true,
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Apply reviewed selection".into(),
                    role: ui::ModalItemRole::ModelApply,
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Save favorite".into(),
                    role: ui::ModalItemRole::ModelSave,
                    ..Default::default()
                },
            ],
            action_stamp: 30,
            ..Default::default()
        }
    }

    #[test]
    fn model_selectors_route_batched_keys_same_value_and_apply_to_original_indices() {
        for (width, height) in [(140, 38), (100, 31), (86, 31), (70, 22), (40, 12)] {
            let mut backend = TestBackend::new(width, height);
            let mut state = SurfaceState::new();
            let mut editor = ui::editor("Exact 日本語 draft  \n\n", "violet", "");
            let draft = editor.lines().join("\n");
            let modal = configuration();
            warm(&mut backend, &modal, &mut editor, &mut state);
            backend.assert_contains("Apply ↵");
            let mut actions = Vec::new();
            backend.run_with_events(
                EventBuilder::new()
                    .key(' ')
                    .key_code(KeyCode::Enter)
                    .build(),
                |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                },
            );
            assert!(
                matches!(actions.as_slice(), [Action::ModalChoice(0)]),
                "{width}x{height}: {actions:?}"
            );
            assert!(!state.dialog.model.expanded);
            actions.clear();
            backend.run_with_events(
                EventBuilder::new().key_code(KeyCode::Enter).build(),
                |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                },
            );
            assert!(
                matches!(actions.as_slice(), [Action::ModalChoice(4)]),
                "{actions:?}"
            );
            actions.clear();
            backend.run_with_events(
                EventBuilder::new()
                    .key(' ')
                    .key_code(KeyCode::Down)
                    .key_code(KeyCode::Enter)
                    .build(),
                |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                },
            );
            assert!(
                matches!(actions.as_slice(), [Action::ModalChoice(1)]),
                "{actions:?}"
            );
            actions.clear();
            // A model called Reasoning is still a native model, not the effort field.
            backend.run_with_events(
                EventBuilder::new().key(' ').key_code(KeyCode::Esc).build(),
                |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                },
            );
            assert!(actions.is_empty());
            backend.run_with_events(
                EventBuilder::new().key_code(KeyCode::Tab).key(' ').build(),
                |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                },
            );
            for _ in 0..4 {
                if !backend.has_pending_input() {
                    break;
                }
                backend.render(|ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions);
                });
            }
            assert!(
                matches!(actions.as_slice(), [Action::ModalChoice(2)]),
                "{actions:?}"
            );
            assert_eq!(editor.lines().join("\n"), draft);
        }
    }

    #[test]
    fn model_child_cancel_tab_and_original_value_acceptance_keep_semantic_field_focus() {
        let mut backend = TestBackend::new(100, 31);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("source draft  \n", "violet", "");
        let mut root = configuration();
        warm(&mut backend, &root, &mut editor, &mut state);
        let child = ui::Modal {
            title: "Reasoning".into(),
            model: Some(ui::ModelDialog::OptionValues {
                id: "effort-native".into(),
            }),
            items: vec![
                ui::ModalItem {
                    label: "Native default".into(),
                    role: ui::ModalItemRole::ModelOptionValue {
                        id: "effort-native".into(),
                    },
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Apply reviewed selection".into(),
                    checked: true,
                    applied: true,
                    role: ui::ModalItemRole::ModelOptionValue {
                        id: "effort-native".into(),
                    },
                    ..Default::default()
                },
            ],
            action_stamp: 31,
            ..Default::default()
        };
        warm(&mut backend, &child, &mut editor, &mut state);
        let mut actions = Vec::new();
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                draw(ctx, &child, &mut editor, &mut state, &mut actions);
            },
        );
        assert!(matches!(actions.as_slice(), [Action::ModalChoice(1)]));
        root.action_stamp = 32;
        warm(&mut backend, &root, &mut editor, &mut state);
        assert_eq!(state.dialog.model.focus, "model-field:option:effort-native");
        actions.clear();
        warm(&mut backend, &child, &mut editor, &mut state);
        backend.run_with_events(
            EventBuilder::new()
                .key_code(KeyCode::Tab)
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| {
                draw(ctx, &child, &mut editor, &mut state, &mut actions);
            },
        );
        assert!(
            matches!(actions.as_slice(), [Action::CloseModal]),
            "{actions:?}"
        );
        warm(&mut backend, &root, &mut editor, &mut state);
        assert_eq!(state.dialog.model.focus, "model-field:option:speed-native");
        actions.clear();
        warm(&mut backend, &child, &mut editor, &mut state);
        backend.run_with_events(
            EventBuilder::new()
                .key_code(KeyCode::BackTab)
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| draw(ctx, &child, &mut editor, &mut state, &mut actions),
        );
        assert!(
            matches!(actions.as_slice(), [Action::CloseModal]),
            "{actions:?}"
        );
        warm(&mut backend, &root, &mut editor, &mut state);
        assert_eq!(state.dialog.model.focus, "model-field:model");
        actions.clear();
        warm(&mut backend, &child, &mut editor, &mut state);
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Esc).build(), |ctx| {
            draw(ctx, &child, &mut editor, &mut state, &mut actions);
        });
        assert!(matches!(actions.as_slice(), [Action::CloseModal]));
        assert_eq!(editor.lines().join("\n"), "source draft  \n");
    }

    #[test]
    fn model_long_catalog_filters_reveals_unicode_and_rejects_stale_or_outside_presses() {
        let mut backend = TestBackend::new(86, 31);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("draft unchanged", "violet", "");
        let mut modal = configuration();
        modal.items.splice(
            2..2,
            (2..50).map(|i| ui::ModalItem {
                label: format!("Native {i:02} 日本語 e\u{301}"),
                role: ui::ModalItemRole::Model {
                    native_id: format!("native-{i}"),
                },
                ..Default::default()
            }),
        );
        warm(&mut backend, &modal, &mut editor, &mut state);
        let mut actions = Vec::new();
        backend.run_with_events(
            EventBuilder::new().key(' ').key_code(KeyCode::End).build(),
            |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            },
        );
        warm(&mut backend, &modal, &mut editor, &mut state);
        backend.assert_contains("Native 49 日本語 e\u{301}");
        let (x, y) = backend.find_text("Native 49").unwrap();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(
            matches!(actions.as_slice(), [Action::ModalChoice(49)]),
            "{actions:?}"
        );
        actions.clear();
        warm(&mut backend, &modal, &mut editor, &mut state);
        backend.run_with_events(
            EventBuilder::new()
                .key(' ')
                .key('z')
                .key('z')
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            },
        );
        backend.assert_contains("No matches");
        assert!(actions.is_empty());
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Esc).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        // A press on Save while a child is open dismisses only that child.
        backend.run_with_events(EventBuilder::new().key(' ').build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        warm(&mut backend, &modal, &mut editor, &mut state);
        let (x, y) = backend.find_text("Save favorite").unwrap();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(
            actions.is_empty(),
            "outside click must not invoke Save: {actions:?}"
        );
        assert!(!state.dialog.model.expanded);
        backend.run_with_events(EventBuilder::new().key(' ').build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        warm(&mut backend, &modal, &mut editor, &mut state);
        let (x, y) = backend.find_text("✓ Apply reviewed selection").unwrap();
        modal.action_stamp += 1;
        modal.items[0].role = ui::ModalItemRole::Model {
            native_id: "replaced-at-same-index".into(),
        };
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(actions.is_empty());
        assert!(!state.dialog.model.expanded);
        assert_eq!(editor.lines().join("\n"), "draft unchanged");
    }

    #[test]
    fn model_actions_and_disabled_shortcuts_keep_input_ownership() {
        let mut backend = TestBackend::new(100, 31);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("literal  \n", "violet", "");
        let modal = configuration();
        warm(&mut backend, &modal, &mut editor, &mut state);
        let (x, y) = backend.find_text("Save favorite").unwrap();
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(matches!(actions.as_slice(), [Action::ModalChoice(5)]));
        actions.clear();
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                ctx.disabled(true, |ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut actions)
                });
            },
        );
        assert!(actions.is_empty());
        // The same keystrokes in a naming editor retain text ownership.
        let naming = ui::Modal {
            title: "Save model favorite".into(),
            editor: true,
            action_stamp: 50,
            ..Default::default()
        };
        warm(&mut backend, &naming, &mut editor, &mut state);
        backend.run_with_events(
            EventBuilder::new()
                .key(' ')
                .key('x')
                .key_code(KeyCode::Enter)
                .build(),
            |ctx| {
                draw(ctx, &naming, &mut editor, &mut state, &mut actions);
            },
        );
        assert!(
            !actions
                .iter()
                .any(|action| matches!(action, Action::ModalChoice(_)))
        );
        assert!(editor.lines().join("\n").contains(" x"));
    }

    #[test]
    fn compact_model_preserves_source_and_semantic_focus_through_actual_buffer_resize() {
        let mut backend = super::super::tests::ResizableBackend::new(140, 38);
        let mut state = SurfaceState::new();
        state.active_key = "exact-environment/original-thread".into();
        let mut editor = ui::editor("Exact café 日本語  \n\n", "violet", "");
        let draft = editor.lines().join("\n");
        let modal = configuration();
        let mut focus = None;
        for (width, height) in [
            (140, 38),
            (100, 31),
            (86, 31),
            (70, 22),
            (40, 12),
            (140, 38),
        ] {
            backend.run_with_events(EventBuilder::new().resize(width, height).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
            });
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
                    focus = ctx.focused_name().map(str::to_owned);
                });
            }
            backend.assert_contains("Apply ↵");
            backend.assert_contains("Save favorite");
            backend.assert_contains("Reasoning");
            assert_eq!(focus.as_deref(), Some("model-field:model"));
            assert_eq!(editor.lines().join("\n"), draft);
            assert_eq!(state.active_key, "exact-environment/original-thread");
        }
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Tab).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        backend.render(|ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            focus = ctx.focused_name().map(str::to_owned);
        });
        assert_eq!(focus.as_deref(), Some("model-field:option:effort-native"));
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::BackTab).build(),
            |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            },
        );
        backend.render(|ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            focus = ctx.focused_name().map(str::to_owned);
        });
        assert_eq!(focus.as_deref(), Some("model-field:model"));
        assert!(actions.is_empty());
    }

    #[test]
    fn palette_secondary_descriptor_keeps_list_pointer_identity_after_queued_navigation() {
        let mut backend = TestBackend::new(86, 31)
            .with_run_config(slt::RunConfig::default().activation_boundaries(true));
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("model", "violet", "");
        let modal=ui::Modal { title:"Commands".into(),editor:true,action_stamp:35,
            items:vec![
                ui::ModalItem {label:"Model".into(),description:"Review native model options".into(),..Default::default()},
                ui::ModalItem {label:"Search conversations".into(),description:"Find by model or source while preserving this draft and original conversation identity with a deliberately long secondary descriptor".into(),..Default::default()},
            ],..Default::default()
        };
        warm(&mut backend, &modal, &mut editor, &mut state);
        let (x, y) = backend.find_text("  Model").unwrap();
        let mut actions = Vec::new();
        let mut callback = |ctx: &mut Context| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        };
        backend.run_with_events(
            EventBuilder::new()
                .key_code(KeyCode::Down)
                .click(x + 2, y)
                .build(),
            &mut callback,
        );
        for _ in 0..4 {
            backend.render(&mut callback);
        }
        assert!(
            matches!(actions.as_slice(), [Action::ModalChoice(0)]),
            "{actions:?}"
        );
    }

    #[test]
    fn question_progress_is_not_a_choice_position_and_palette_exposes_secondary_matches() {
        let mut backend = TestBackend::new(86, 31);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor("", "violet", "");
        let modal = ui::Modal {
            title: "Question 1 / 1 · Next step".into(),
            items: vec![
                ui::ModalItem {
                    label: "Next".into(),
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Continue".into(),
                    ..Default::default()
                },
                ui::ModalItem {
                    label: "Next".into(),
                    action: true,
                    ..Default::default()
                },
            ],
            action_stamp: 10,
            ..Default::default()
        };
        warm(&mut backend, &modal, &mut editor, &mut state);
        backend.assert_contains("Question 1 / 1");
        backend.assert_not_contains("1/2");
        let modal = ui::Modal {
            title: "Commands".into(),
            editor: true,
            action_stamp: 11,
            items: vec![ui::ModalItem {
                label: "Search server conversations".into(),
                description: "Find conversations by title, model or environment".into(),
                ..Default::default()
            }],
            ..Default::default()
        };
        editor = ui::editor("model", "violet", "");
        warm(&mut backend, &modal, &mut editor, &mut state);
        backend.assert_contains("title, model or environment");
        let mut actions = Vec::new();
        backend.run_with_events(
            EventBuilder::new().key_code(KeyCode::Enter).build(),
            |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut actions);
            },
        );
        assert!(matches!(actions.as_slice(), [Action::ModalChoice(0)]));
    }

    #[test]
    fn search_query_has_pane_local_bounds_and_preserves_exact_edits_after_resize() {
        let mut backend = super::super::tests::ResizableBackend::new(140, 38);
        let mut state = SurfaceState::new();
        let mut editor = ui::editor(
            "Unicode 日本語 café and a long query that wraps within its field",
            "violet",
            "",
        );
        let original = editor.lines().join("\n");
        let modal = ui::Modal {
            title: "Search conversations".into(),
            editor: true,
            action_stamp: 12,
            ..Default::default()
        };
        for (width, height) in [(140, 38), (86, 31), (70, 22), (40, 12), (140, 38)] {
            backend.run_with_events(EventBuilder::new().resize(width, height).build(), |ctx| {
                draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
            });
            for _ in 0..3 {
                backend.render(|ctx| {
                    draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
                });
            }
            backend.render(|ctx| {
                let modal_rect = ctx.measured_layout_rect("t3oxide.modal").unwrap();
                let query = ctx.measured_layout_rect("t3oxide.query").unwrap();
                assert!(
                    query.x > modal_rect.x
                        && query.x + query.width < modal_rect.x + modal_rect.width
                );
                assert_eq!(query.height, 3);
                assert_eq!(
                    state.modal_editor.wrap_width,
                    Some(query.width.saturating_sub(2).max(1))
                );
                draw(ctx, &modal, &mut editor, &mut state, &mut Vec::new());
            });
            assert_eq!(editor.lines().join("\n"), original);
        }
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().paste(" exact 🙂 ").build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(matches!(actions.as_slice(), [Action::ModalEditChanged]));
        assert_eq!(editor.lines().join("\n"), format!("{original} exact 🙂 "));
    }

    #[test]
    fn search_scope_opens_native_scope_menu_without_submitting_query() {
        let mut backend = TestBackend::new(70, 22);
        let mut state = SurfaceState::new();
        state.browsing_scope = "Build · Alpha".into();
        let mut editor = ui::editor("release fix", "violet", "");
        let modal = ui::Modal {
            title: "Search conversations".into(),
            editor: true,
            action_stamp: 3,
            ..ui::Modal::default()
        };
        warm(&mut backend, &modal, &mut editor, &mut state);
        let (x, y) = backend
            .find_text("Scope")
            .expect("search exposes scope control");
        assert_eq!(backend.find_text("Build · Alpha").unwrap().1, y);
        let mut actions = Vec::new();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &modal, &mut editor, &mut state, &mut actions);
        });
        assert!(matches!(actions.as_slice(), [Action::Command(name)] if name == "search-scope"));
        assert_eq!(editor.lines().join("\n"), "release fix");
    }
}
