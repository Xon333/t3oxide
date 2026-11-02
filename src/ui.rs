//! Terminal presentation. T3 state and mutations stay in the controller.
//!
//! Ratatui owns cell diffing and clipping; ratatui-textarea owns editing and
//! Unicode cursor movement. Workspace arrangements are presentation only.

use std::collections::{HashMap, VecDeque};
use std::hash::{Hash, Hasher};
use std::rc::Rc;

use ratatui::Frame;
use ratatui::layout::{Alignment, Constraint, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span, Text};
use ratatui::widgets::{
    Block, Borders, Clear, Padding, Paragraph, Scrollbar, ScrollbarOrientation, ScrollbarState,
    Wrap,
};
use ratatui_textarea::{TextArea, WrapMode};

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Mode {
    #[default]
    Developer,
    AgentBox,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Focus {
    #[default]
    Navigator,
    Transcript,
    Composer,
    Inspector,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum LayoutMode {
    #[default]
    Split,
    Focus,
    Wide,
}

#[derive(Clone, Debug, Default)]
pub struct View {
    pub title: String,
    pub mode: Mode,
    pub focus: Focus,
    pub layout: LayoutMode,
    pub theme: String,
    pub filter: String,
    pub lane: String,
    pub rows: Vec<ThreadView>,
    pub selected: usize,
    pub messages: Vec<MessageView>,
    pub transcript_scroll: usize,
    pub follow: bool,
    pub status: String,
    pub hint: String,
    pub notice: String,
    pub inspector: Vec<(String, String)>,
    pub requests: usize,
    pub pending: usize,
}

#[derive(Clone, Debug, Default)]
pub struct ThreadView {
    pub key: String,
    pub title: String,
    pub subtitle: String,
    pub status: String,
    pub depth: usize,
    pub pinned: bool,
}

#[derive(Clone, Debug, Default)]
pub struct MessageView {
    pub key: String,
    pub role: String,
    pub text: String,
    pub streaming: bool,
    pub truncated: bool,
}

#[derive(Clone, Debug, Default)]
pub struct Modal {
    pub title: String,
    pub description: String,
    pub items: Vec<ModalItem>,
    pub selected: usize,
    pub scroll: usize,
    pub detail: String,
    pub editor: bool,
    pub footer: String,
    pub action_stamp: u64,
}

#[derive(Clone, Debug, Default)]
pub struct ModalItem {
    pub label: String,
    pub description: String,
    pub checked: bool,
}

/// Editor rectangles are their actual widget areas, without decorative borders.
/// This keeps mouse-to-cursor conversion identical to the textarea example.
#[derive(Clone, Debug, Default)]
pub struct HitMap {
    pub navigator: Rect,
    pub transcript: Rect,
    pub composer: Rect,
    pub inspector: Rect,
    pub rows: Vec<(Rect, usize)>,
    pub thread_keys: Vec<(Rect, String)>,
    pub modal_items: Vec<(Rect, usize)>,
    pub modal_editor: Rect,
    pub modal_detail: Rect,
    pub modal_detail_max_scroll: usize,
    pub modal: Rect,
    pub mode_tabs: Vec<(Rect, Mode)>,
    pub layout_tabs: Vec<(Rect, LayoutMode)>,
    pub lane_tabs: Vec<(Rect, String)>,
    pub message_rows: Vec<(Rect, usize)>,
    pub message_keys: Vec<(Rect, String)>,
    pub modal_stamp: u64,
    pub follow: Rect,
    pub palette: Rect,
}

#[derive(Clone, Copy, Debug)]
struct Theme {
    background: Color,
    panel: Color,
    text: Color,
    muted: Color,
    border: Color,
    accent: Color,
    selection: Color,
}

impl Theme {
    fn named(name: &str) -> Self {
        let mut theme = Self {
            background: Color::Rgb(14, 19, 27),
            panel: Color::Rgb(19, 27, 38),
            text: Color::Rgb(218, 228, 239),
            muted: Color::Rgb(137, 153, 173),
            border: Color::Rgb(44, 58, 76),
            accent: Color::Rgb(187, 165, 242),
            selection: Color::Rgb(48, 43, 74),
        };
        match name.to_ascii_lowercase().as_str() {
            "terminal" => {
                theme.background = Color::Reset;
                theme.panel = Color::Reset;
                theme.text = Color::Reset;
                theme.muted = Color::DarkGray;
                theme.border = Color::DarkGray;
                theme.accent = Color::Cyan;
                theme.selection = Color::DarkGray;
            }
            "cyan" => {
                theme.accent = Color::Rgb(127, 221, 225);
                theme.selection = Color::Rgb(25, 58, 68);
            }
            "lime" => {
                theme.accent = Color::Rgb(191, 217, 141);
                theme.selection = Color::Rgb(48, 60, 39);
            }
            _ => {}
        }
        theme
    }

    fn base(self) -> Style {
        Style::default().fg(self.text).bg(self.background)
    }

    fn quiet(self) -> Style {
        Style::default().fg(self.muted)
    }

    fn strong(self) -> Style {
        Style::default()
            .fg(self.accent)
            .add_modifier(Modifier::BOLD)
    }
}

// Critical state colors stay stable across decorative themes.
const ERROR: Color = Color::Rgb(247, 123, 130);
const WAITING: Color = Color::Rgb(237, 190, 101);
const DONE: Color = Color::Rgb(139, 210, 157);

#[derive(Clone, Debug, Hash, PartialEq, Eq)]
struct MessageKey {
    id: String,
    hash: u64,
    width: u16,
    theme: String,
}

struct CachedText {
    key: MessageKey,
    text: Rc<Text<'static>>,
    bytes: usize,
}

const MAX_CACHE_ITEMS: usize = 128;
const MAX_CACHE_BYTES: usize = 4 * 1024 * 1024;
const MAX_MEASUREMENTS: usize = 4096;

#[derive(Default)]
pub struct Renderer {
    cache: VecDeque<CachedText>,
    cache_bytes: usize,
    measurements: HashMap<MessageKey, usize>,
    measurement_order: VecDeque<MessageKey>,
    navigator_offset: usize,
    transcript_lines: usize,
    transcript_visible: usize,
    transcript_top: usize,
    transcript_items: Vec<(String, usize, usize)>,
}

impl Renderer {
    pub fn transcript_max_scroll(&self) -> usize {
        self.transcript_lines
            .saturating_sub(self.transcript_visible)
    }

    pub fn transcript_scroll(&self) -> usize {
        self.transcript_top
    }

    pub fn transcript_line_count(&self) -> usize {
        self.transcript_lines
    }

    pub fn transcript_visible_lines(&self) -> usize {
        self.transcript_visible
    }

    pub fn transcript_item_offset(&self, key: &str) -> Option<usize> {
        self.transcript_items
            .iter()
            .find(|(id, _, _)| id == key)
            .map(|(_, start, _)| *start)
    }

    pub fn transcript_item_at(&self, offset: usize) -> Option<(String, usize)> {
        self.transcript_items
            .iter()
            .find(|(_, start, height)| offset >= *start && offset < start.saturating_add(*height))
            .map(|(id, start, _)| (id.clone(), offset - *start))
    }

    pub fn draw(
        &mut self,
        frame: &mut Frame,
        view: &View,
        composer: &TextArea<'static>,
        modal: Option<&Modal>,
        modal_editor: &TextArea<'static>,
    ) -> HitMap {
        let area = frame.area();
        let theme = Theme::named(&view.theme);
        let mut hits = HitMap::default();
        frame.render_widget(Block::default().style(theme.base()), area);
        if area.width < 24 || area.height < 8 {
            frame.render_widget(
                Paragraph::new(
                    "t3oxide\nResize the terminal to at least 24 × 8.\nYour draft is retained.",
                )
                .style(theme.quiet())
                .wrap(Wrap { trim: false }),
                area,
            );
            return hits;
        }

        let footer_height = if view.notice.is_empty() { 2 } else { 3 };
        let vertical = Layout::vertical([
            Constraint::Length(2),
            Constraint::Min(3),
            Constraint::Length(footer_height),
        ])
        .split(area);
        draw_header(frame, vertical[0], view, theme, &mut hits);
        draw_footer(frame, vertical[2], view, theme);
        let body = vertical[1];

        let compact = body.width < 78 || view.layout == LayoutMode::Focus;
        if compact && view.focus == Focus::Navigator {
            self.draw_navigator(frame, body, view, theme, &mut hits);
        } else if compact && view.focus == Focus::Inspector {
            draw_inspector(frame, body, view, theme, &mut hits);
        } else if compact {
            self.draw_conversation(frame, body, view, composer, theme, &mut hits);
        } else {
            let sidebar_width = if view.mode == Mode::AgentBox { 34 } else { 30 };
            let show_inspector = body.width >= 132
                || (view.layout == LayoutMode::Wide && body.width >= 108)
                || view.focus == Focus::Inspector;
            let sections = if show_inspector {
                Layout::horizontal([
                    Constraint::Length(sidebar_width.min(body.width / 3)),
                    Constraint::Min(28),
                    Constraint::Length((body.width / 4).clamp(24, 36)),
                ])
                .split(body)
            } else {
                Layout::horizontal([Constraint::Length(sidebar_width), Constraint::Min(28)])
                    .split(body)
            };
            self.draw_navigator(frame, sections[0], view, theme, &mut hits);
            self.draw_conversation(frame, sections[1], view, composer, theme, &mut hits);
            if show_inspector {
                draw_inspector(frame, sections[2], view, theme, &mut hits);
            }
        }

        if let Some(modal) = modal {
            draw_modal(frame, area, modal, modal_editor, theme, &mut hits);
        }
        hits
    }

    fn draw_navigator(
        &mut self,
        frame: &mut Frame,
        area: Rect,
        view: &View,
        theme: Theme,
        hits: &mut HitMap,
    ) {
        hits.navigator = area;
        let title = if view.mode == Mode::AgentBox {
            "Agents"
        } else {
            "Conversations"
        };
        let block = panel(title, view.focus == Focus::Navigator, theme).title_bottom(Line::styled(
            format!(" {} shown ", view.rows.len()),
            theme.quiet(),
        ));
        let inner = block.inner(area);
        frame.render_widget(block, area);
        if inner.height == 0 || inner.width == 0 {
            return;
        }
        let filter = if view.filter.is_empty() {
            Line::from(vec![
                Span::styled(" / ", theme.strong()),
                Span::styled("Filter conversations", theme.quiet()),
            ])
        } else {
            Line::from(vec![
                Span::styled(" / ", theme.strong()),
                Span::raw(safe_text(&view.filter)),
            ])
        };
        frame.render_widget(
            Paragraph::new(filter),
            Rect::new(inner.x, inner.y, inner.width, 1),
        );

        let lane_height = if inner.width >= 42 { 1 } else { 2 };
        let lane_area = Rect::new(
            inner.x,
            inner.y.saturating_add(1),
            inner.width,
            lane_height.min(inner.height.saturating_sub(1)),
        );
        draw_lanes(frame, lane_area, view, theme, hits);
        let top = 2 + lane_height;
        if inner.height <= top {
            return;
        }
        let list = Rect::new(inner.x, inner.y + top, inner.width, inner.height - top);
        if view.rows.is_empty() {
            let empty = if view.filter.is_empty() {
                "Nothing here yet.\n\nOpen commands to connect an environment or create a conversation."
            } else {
                "No matching conversations.\n\nClear the filter to see this lane again."
            };
            frame.render_widget(
                Paragraph::new(empty)
                    .style(theme.quiet())
                    .wrap(Wrap { trim: false }),
                list,
            );
            return;
        }

        let row_height = if view.mode == Mode::AgentBox { 3 } else { 2 };
        let capacity = (usize::from(list.height) / row_height).max(1);
        let selected = view.selected.min(view.rows.len().saturating_sub(1));
        if selected < self.navigator_offset {
            self.navigator_offset = selected;
        } else if selected >= self.navigator_offset.saturating_add(capacity) {
            self.navigator_offset = selected + 1 - capacity;
        }
        self.navigator_offset = self
            .navigator_offset
            .min(view.rows.len().saturating_sub(capacity));

        for (slot, (index, row)) in view
            .rows
            .iter()
            .enumerate()
            .skip(self.navigator_offset)
            .take(capacity)
            .enumerate()
        {
            let y = list.y + (slot * row_height) as u16;
            let height = (row_height as u16).min(list.bottom().saturating_sub(y));
            let rect = Rect::new(list.x, y, list.width, height);
            let selected = index == view.selected;
            let style = Style::default().fg(theme.text).bg(if selected {
                theme.selection
            } else {
                theme.panel
            });
            frame.render_widget(Block::default().style(style), rect);
            let (glyph, color) = status_style(&row.status, theme);
            let indentation = "  ".repeat(row.depth.min(4));
            let marker = if row.pinned { "◆" } else { glyph };
            let title = Line::from(vec![
                Span::styled(if selected { "▎" } else { " " }, theme.strong()),
                Span::raw(indentation),
                Span::styled(format!("{marker} "), Style::default().fg(color)),
                Span::styled(
                    safe_text(&row.title),
                    if selected {
                        Style::default().add_modifier(Modifier::BOLD)
                    } else {
                        Style::default()
                    },
                ),
            ]);
            frame.render_widget(
                Paragraph::new(title),
                Rect::new(rect.x, rect.y, rect.width, 1),
            );
            if height > 1 {
                let subtitle =
                    Line::styled(format!("   {}", safe_text(&row.subtitle)), theme.quiet());
                frame.render_widget(
                    Paragraph::new(subtitle),
                    Rect::new(rect.x, rect.y + 1, rect.width, 1),
                );
            }
            hits.rows.push((rect, index));
            hits.thread_keys.push((rect, row.key.clone()));
        }
    }

    fn draw_conversation(
        &mut self,
        frame: &mut Frame,
        area: Rect,
        view: &View,
        composer: &TextArea<'static>,
        theme: Theme,
        hits: &mut HitMap,
    ) {
        let width = usize::from(area.width.saturating_sub(4).max(1));
        let visual_lines = composer
            .lines()
            .iter()
            .map(|line| Line::raw(line.as_str()).width().max(1).div_ceil(width))
            .sum::<usize>();
        let requested = (visual_lines + 2).clamp(3, 9) as u16;
        let composer_height = requested.min(area.height.saturating_sub(3));
        let pieces =
            Layout::vertical([Constraint::Min(1), Constraint::Length(composer_height)]).split(area);
        self.draw_transcript(frame, pieces[0], view, theme, hits);
        if composer_height == 0 {
            return;
        }
        let block = panel("Message", view.focus == Focus::Composer, theme);
        let inner = block.inner(pieces[1]);
        frame.render_widget(block, pieces[1]);
        frame.render_widget(composer, inner);
        hits.composer = inner;
    }

    fn draw_transcript(
        &mut self,
        frame: &mut Frame,
        area: Rect,
        view: &View,
        theme: Theme,
        hits: &mut HitMap,
    ) {
        hits.transcript = area;
        self.transcript_items.clear();
        let block = panel(
            &safe_text(&view.title),
            view.focus == Focus::Transcript,
            theme,
        );
        let inner = block.inner(area);
        frame.render_widget(block, area);
        let content = Rect::new(
            inner.x,
            inner.y,
            inner.width.saturating_sub(1),
            inner.height.saturating_sub(1),
        );
        self.transcript_visible = usize::from(content.height);
        if content.width == 0 || content.height == 0 {
            return;
        }
        if view.messages.is_empty() {
            self.transcript_lines = 0;
            self.transcript_top = 0;
            frame.render_widget(
                Paragraph::new("A clear place to work.\n\nSelect a conversation or start a new one from commands.\n\nThe message draft follows its conversation across views.")
                    .style(theme.quiet()).wrap(Wrap { trim: false }),
                inset(content, 1, 1),
            );
            return;
        }

        let mut measurements = Vec::with_capacity(view.messages.len());
        let mut total = 0usize;
        for message in &view.messages {
            let key = message_key(message, content.width, &view.theme);
            let height = if let Some(height) = self.measurements.get(&key) {
                *height
            } else {
                let text = self.message_text(&key, message, theme);
                let height = Paragraph::new(borrow_text(text.as_ref(), 0))
                    .wrap(Wrap { trim: false })
                    .line_count(content.width)
                    .max(1);
                if self.measurement_order.len() >= MAX_MEASUREMENTS
                    && let Some(old) = self.measurement_order.pop_front()
                {
                    self.measurements.remove(&old);
                }
                self.measurements.insert(key.clone(), height);
                self.measurement_order.push_back(key.clone());
                height
            };
            measurements.push((key, total, height));
            self.transcript_items
                .push((message.key.clone(), total, height.saturating_add(2)));
            total = total.saturating_add(height).saturating_add(2);
        }
        self.transcript_lines = total;
        self.transcript_top = if view.follow {
            self.transcript_max_scroll()
        } else {
            view.transcript_scroll.min(self.transcript_max_scroll())
        };
        let end = self.transcript_top.saturating_add(self.transcript_visible);

        for (index, ((key, top, body_height), message)) in
            measurements.iter().zip(&view.messages).enumerate()
        {
            let message_end = top.saturating_add(*body_height).saturating_add(2);
            if message_end <= self.transcript_top || *top >= end {
                continue;
            }
            let visible_top = (*top).max(self.transcript_top);
            let visible_end = message_end.min(end);
            hits.message_rows.push((
                Rect::new(
                    content.x,
                    content.y + (visible_top - self.transcript_top) as u16,
                    content.width,
                    (visible_end - visible_top) as u16,
                ),
                index,
            ));
            if let Some((rect, _)) = hits.message_rows.last() {
                hits.message_keys.push((*rect, message.key.clone()));
            }
            if *top >= self.transcript_top {
                let role = role_label(&message.role);
                let color = if role == "You" {
                    theme.accent
                } else if role == "Tool" {
                    theme.muted
                } else {
                    theme.text
                };
                let mut spans = vec![Span::styled(
                    role,
                    Style::default().fg(color).add_modifier(Modifier::BOLD),
                )];
                if message.streaming {
                    spans.push(Span::styled("  ● streaming", theme.strong()));
                }
                frame.render_widget(
                    Paragraph::new(Line::from(spans)),
                    Rect::new(
                        content.x,
                        content.y + (*top - self.transcript_top) as u16,
                        content.width,
                        1,
                    ),
                );
            }
            let body_top = top.saturating_add(1);
            let draw_top = body_top.max(self.transcript_top);
            let draw_end = body_top.saturating_add(*body_height).min(end);
            if draw_end <= draw_top {
                continue;
            }
            let text = self.message_text(key, message, theme);
            let offset = draw_top - body_top;
            let paragraph = scrolled_paragraph(text.as_ref(), offset, content.width);
            frame.render_widget(
                paragraph,
                Rect::new(
                    content.x,
                    content.y + (draw_top - self.transcript_top) as u16,
                    content.width,
                    (draw_end - draw_top) as u16,
                ),
            );
        }

        if total > self.transcript_visible && inner.width > 1 {
            let mut state = ScrollbarState::new(total)
                .position(self.transcript_top)
                .viewport_content_length(self.transcript_visible);
            frame.render_stateful_widget(
                Scrollbar::new(ScrollbarOrientation::VerticalRight)
                    .begin_symbol(None)
                    .end_symbol(None)
                    .track_style(theme.quiet())
                    .thumb_style(theme.strong()),
                Rect::new(inner.x, inner.y, inner.width, content.height),
                &mut state,
            );
        }
        let follow_label = if view.follow {
            "● Following".to_owned()
        } else {
            format!("{} / {} · End follows", self.transcript_top + 1, total)
        };
        hits.follow = Rect::new(inner.x, inner.bottom().saturating_sub(1), inner.width, 1);
        frame.render_widget(
            Paragraph::new(follow_label)
                .alignment(Alignment::Right)
                .style(if view.follow {
                    theme.strong()
                } else {
                    theme.quiet()
                }),
            hits.follow,
        );
    }

    fn message_text(
        &mut self,
        key: &MessageKey,
        message: &MessageView,
        theme: Theme,
    ) -> Rc<Text<'static>> {
        if let Some(index) = self.cache.iter().position(|entry| &entry.key == key)
            && let Some(entry) = self.cache.remove(index)
        {
            let text = Rc::clone(&entry.text);
            self.cache.push_back(entry);
            return text;
        }
        let source = safe_text(&message.text);
        let mut text = if message.role.eq_ignore_ascii_case("tool") {
            Text::from(source.clone())
        } else {
            let options = tui_markdown::Options::new(MarkdownTheme(theme)).table_width(key.width);
            own_text(tui_markdown::from_str_with_options(&source, &options))
        };
        if message.truncated {
            text.lines.push(Line::styled("… T3 returned a bounded excerpt; exact source is available through transcript actions.", Style::default().fg(WAITING)));
        }
        text.style = theme.base().bg(theme.panel);
        let bytes = source.len().saturating_add(
            text.lines
                .iter()
                .map(|line| line.spans.len() * std::mem::size_of::<Span<'static>>())
                .sum::<usize>(),
        );
        let text = Rc::new(text);
        if bytes <= MAX_CACHE_BYTES {
            while self.cache.len() >= MAX_CACHE_ITEMS
                || self.cache_bytes.saturating_add(bytes) > MAX_CACHE_BYTES
            {
                if let Some(old) = self.cache.pop_front() {
                    self.cache_bytes = self.cache_bytes.saturating_sub(old.bytes);
                } else {
                    break;
                }
            }
            self.cache_bytes = self.cache_bytes.saturating_add(bytes);
            self.cache.push_back(CachedText {
                key: key.clone(),
                text: Rc::clone(&text),
                bytes,
            });
        }
        text
    }
}

#[derive(Clone, Copy)]
struct MarkdownTheme(Theme);

impl tui_markdown::StyleSheet for MarkdownTheme {
    fn heading(&self, _level: u8) -> Style {
        self.0.strong()
    }
    fn code(&self) -> Style {
        Style::default().fg(self.0.text).bg(self.0.background)
    }
    fn link(&self) -> Style {
        Style::default()
            .fg(self.0.accent)
            .add_modifier(Modifier::UNDERLINED)
    }
    fn blockquote(&self) -> Style {
        self.0.quiet()
    }
    fn table_header(&self) -> Style {
        self.0.strong()
    }
    fn table_border(&self) -> Style {
        self.0.quiet()
    }
    fn list_marker(&self) -> Style {
        self.0.strong()
    }
}

/// Shared editor construction for persistent composers and temporary forms.
pub fn editor(text: &str, theme: &str, placeholder: &str) -> TextArea<'static> {
    let mut editor = TextArea::new(text.split('\n').map(str::to_owned).collect());
    editor.set_wrap_mode(WrapMode::Word);
    editor.set_tab_length(4);
    editor.set_placeholder_text(placeholder);
    style_editor(&mut editor, theme);
    editor
}

pub fn style_editor(editor: &mut TextArea<'_>, theme: &str) {
    let theme = Theme::named(theme);
    editor.set_style(theme.base().bg(theme.panel));
    editor.set_cursor_line_style(Style::default());
    editor.set_cursor_style(Style::default().fg(theme.background).bg(theme.accent));
    editor.set_selection_style(
        Style::default()
            .bg(theme.selection)
            .add_modifier(Modifier::UNDERLINED),
    );
    editor.set_placeholder_style(theme.quiet());
}

fn draw_header(frame: &mut Frame, area: Rect, view: &View, theme: Theme, hits: &mut HitMap) {
    let top = Rect::new(area.x, area.y, area.width, 1);
    frame.render_widget(Block::default().style(theme.base()), top);
    frame.render_widget(
        Paragraph::new(" t3oxide").style(theme.strong()),
        Rect::new(top.x, top.y, 10.min(top.width), 1),
    );
    let mut x = top.x.saturating_add(10);
    for (label, mode) in [
        (" Developer ", Mode::Developer),
        (" AgentBox ", Mode::AgentBox),
    ] {
        let width = label.len() as u16;
        if x.saturating_add(width) > top.right() {
            break;
        }
        let rect = Rect::new(x, top.y, width, 1);
        let style = if view.mode == mode {
            Style::default()
                .fg(theme.accent)
                .bg(theme.selection)
                .add_modifier(Modifier::BOLD)
        } else {
            theme.quiet()
        };
        frame.render_widget(Paragraph::new(label).style(style), rect);
        hits.mode_tabs.push((rect, mode));
        x += width + 1;
    }
    if area.width >= 70 {
        hits.palette = Rect::new(area.right() - 17, area.y, 16, 1);
        frame.render_widget(
            Paragraph::new("Ctrl+P Commands")
                .alignment(Alignment::Right)
                .style(theme.quiet()),
            hits.palette,
        );
    }
    let second = Rect::new(area.x + 1, area.y + 1, area.width.saturating_sub(2), 1);
    let mut spans = vec![Span::styled(safe_text(&view.status), theme.quiet())];
    if view.requests > 0 {
        spans.push(Span::styled(
            format!("  {} requests", view.requests),
            Style::default().fg(WAITING),
        ));
    }
    if view.pending > 0 {
        spans.push(Span::styled(
            format!("  {} unresolved", view.pending),
            Style::default().fg(WAITING),
        ));
    }
    let mut right = second.right();
    if area.width >= 100 {
        for (label, layout) in [
            (" Wide ", LayoutMode::Wide),
            (" Focus ", LayoutMode::Focus),
            (" Split ", LayoutMode::Split),
        ] {
            let width = label.len() as u16;
            right = right.saturating_sub(width);
            let rect = Rect::new(right, second.y, width, 1);
            frame.render_widget(
                Paragraph::new(label).style(if view.layout == layout {
                    theme.strong()
                } else {
                    theme.quiet()
                }),
                rect,
            );
            hits.layout_tabs.push((rect, layout));
        }
    }
    frame.render_widget(
        Paragraph::new(Line::from(spans)),
        Rect::new(second.x, second.y, right.saturating_sub(second.x), 1),
    );
}

fn draw_footer(frame: &mut Frame, area: Rect, view: &View, theme: Theme) {
    let focus = match view.focus {
        Focus::Navigator => "CONVERSATIONS",
        Focus::Transcript => "TRANSCRIPT",
        Focus::Composer => "MESSAGE",
        Focus::Inspector => "DETAILS",
    };
    let row = Line::from(vec![
        Span::styled(
            format!(" {focus} "),
            Style::default().fg(theme.accent).bg(theme.selection),
        ),
        Span::styled(format!("  {}", safe_text(&view.hint)), theme.quiet()),
    ]);
    frame.render_widget(
        Paragraph::new(row),
        Rect::new(area.x, area.y, area.width, 1),
    );
    if !view.notice.is_empty() && area.height > 1 {
        frame.render_widget(
            Paragraph::new(format!(" {}", safe_text(&view.notice)))
                .style(Style::default().fg(WAITING)),
            Rect::new(area.x, area.y + 1, area.width, 1),
        );
    }
}

fn draw_lanes(frame: &mut Frame, area: Rect, view: &View, theme: Theme, hits: &mut HitMap) {
    let mut x = area.x;
    let mut y = area.y;
    let first = if view.mode == Mode::AgentBox {
        ("Inbox", "Inbox")
    } else {
        ("Active", "Unreviewed")
    };
    for (label, value) in [
        first,
        ("Attention", "Attention"),
        ("Working", "Working"),
        ("Settled", "Reviewed"),
        ("All", "All"),
    ] {
        let width = (label.len() as u16 + 1).min(area.width);
        if x + width > area.right() {
            x = area.x;
            y += 1;
        }
        if y >= area.bottom() {
            break;
        }
        let rect = Rect::new(x, y, width, 1);
        let selected =
            view.lane.eq_ignore_ascii_case(value) || (view.lane.is_empty() && value == "All");
        frame.render_widget(
            Paragraph::new(label).style(if selected {
                theme.strong().add_modifier(Modifier::UNDERLINED)
            } else {
                theme.quiet()
            }),
            rect,
        );
        hits.lane_tabs.push((rect, value.to_owned()));
        x += width;
    }
}

fn draw_inspector(frame: &mut Frame, area: Rect, view: &View, theme: Theme, hits: &mut HitMap) {
    hits.inspector = area;
    let block = panel("Details", view.focus == Focus::Inspector, theme);
    let inner = block.inner(area);
    frame.render_widget(block, area);
    let mut lines = Vec::new();
    if view.inspector.is_empty() {
        lines.push(Line::styled(
            "Select a conversation to inspect its environment, runtime and native requests.",
            theme.quiet(),
        ));
    } else {
        for (key, value) in &view.inspector {
            lines.push(Line::styled(safe_text(key), theme.quiet()));
            for line in safe_text(value).lines() {
                lines.push(Line::raw(line.to_owned()));
            }
            lines.push(Line::raw(""));
        }
    }
    frame.render_widget(Paragraph::new(lines).wrap(Wrap { trim: false }), inner);
}

fn draw_modal(
    frame: &mut Frame,
    area: Rect,
    modal: &Modal,
    editor: &TextArea<'static>,
    theme: Theme,
    hits: &mut HitMap,
) {
    let width = 100u16.min(area.width.saturating_sub(2)).max(1);
    let description = safe_text(&modal.description);
    let description_lines = Paragraph::new(description.as_str())
        .wrap(Wrap { trim: false })
        .line_count(width.saturating_sub(4).max(1));
    // Native prompts and exact recovery payloads must be wholly reviewable.
    // Long prompts join the scroll pane instead of being silently clipped.
    let long_description = description_lines > 3;
    let mut detail = if long_description && !modal.detail.is_empty() {
        format!("{description}\n\n{}", safe_text(&modal.detail))
    } else if long_description {
        description.clone()
    } else {
        safe_text(&modal.detail)
    };
    if let Some(item) = modal.items.get(modal.selected) {
        let available = usize::from(width.saturating_sub(6));
        if Line::raw(item.label.as_str()).width() > available
            || Line::raw(item.description.as_str()).width() > available
            || item.label.contains('\n')
            || item.description.contains('\n')
        {
            detail = format!(
                "Selected choice\n{}\n{}{}{}",
                safe_text(&item.label),
                safe_text(&item.description),
                if detail.is_empty() { "" } else { "\n\n" },
                detail
            );
        }
    }
    let description = if long_description {
        "Full prompt below · PageUp/PageDown or mouse wheel to read".to_owned()
    } else {
        description
    };
    let desired = if !detail.is_empty() {
        usize::from(area.height.saturating_sub(2))
    } else if modal.editor && modal.items.is_empty() {
        24
    } else {
        8 + usize::from(modal.editor) * 3 + modal.items.len().min(10) * 2
    };
    let height = (desired as u16).min(area.height.saturating_sub(2)).max(1);
    let rect = Rect::new(
        area.x + area.width.saturating_sub(width) / 2,
        area.y + area.height.saturating_sub(height) / 2,
        width,
        height,
    );
    hits.modal = rect;
    hits.modal_stamp = modal_fingerprint(modal);
    frame.render_widget(Clear, rect);
    let block = panel(&safe_text(&modal.title), true, theme);
    let inner = block.inner(rect);
    frame.render_widget(block, rect);
    if inner.height == 0 || inner.width == 0 {
        return;
    }
    let description_height = if description.is_empty() {
        0
    } else {
        Paragraph::new(description.as_str())
            .wrap(Wrap { trim: false })
            .line_count(inner.width)
            .min(3) as u16
    }
    .min(inner.height.saturating_sub(1));
    let available = inner.height.saturating_sub(description_height + 1);
    let editor_height = if !modal.editor {
        0
    } else if !modal.items.is_empty() {
        3.min(available)
    } else if !detail.is_empty() {
        (available / 3).max(2).min(available)
    } else {
        available
    };
    let available = available.saturating_sub(editor_height);
    let list_height = if modal.items.is_empty() {
        0
    } else if detail.is_empty() {
        available
    } else {
        (modal.items.len().saturating_mul(2).min(16) as u16)
            .min(available.saturating_sub(3).max(available / 2))
    };
    let detail_height = if detail.is_empty() {
        0
    } else {
        available.saturating_sub(list_height)
    };
    let parts = Layout::vertical([
        Constraint::Length(description_height),
        Constraint::Length(editor_height),
        Constraint::Length(list_height),
        Constraint::Length(detail_height),
        Constraint::Min(1),
    ])
    .split(inner);
    frame.render_widget(
        Paragraph::new(description)
            .style(theme.quiet())
            .wrap(Wrap { trim: false }),
        parts[0],
    );
    if editor_height > 0 {
        let input_block = Block::default()
            .borders(Borders::BOTTOM)
            .border_style(theme.strong())
            .padding(Padding::horizontal(1));
        hits.modal_editor = input_block.inner(parts[1]);
        frame.render_widget(input_block, parts[1]);
        frame.render_widget(editor, hits.modal_editor);
    }
    let capacity = (usize::from(parts[2].height) / 2).max(1);
    // The selected action determines list visibility. Scroll belongs to the
    // detail pane whenever present, so reading cannot alter an action target.
    let mut offset = if detail.is_empty() {
        modal.scroll.min(modal.items.len().saturating_sub(capacity))
    } else {
        0
    };
    if modal.selected < offset {
        offset = modal.selected;
    }
    if modal.selected >= offset + capacity {
        offset = modal.selected + 1 - capacity;
    }
    for (slot, (index, item)) in modal
        .items
        .iter()
        .enumerate()
        .skip(offset)
        .take(capacity)
        .enumerate()
    {
        let y = parts[2].y + (slot * 2) as u16;
        if y >= parts[2].bottom() {
            break;
        }
        let rect = Rect::new(parts[2].x, y, parts[2].width, 2.min(parts[2].bottom() - y));
        let selected = index == modal.selected;
        if selected {
            frame.render_widget(
                Block::default().style(Style::default().bg(theme.selection)),
                rect,
            );
        }
        let marker = if item.checked {
            "✓"
        } else if selected {
            "›"
        } else {
            " "
        };
        frame.render_widget(
            Paragraph::new(format!("{marker} {}", safe_text(&item.label))).style(if selected {
                theme.strong()
            } else {
                Style::default().fg(theme.text)
            }),
            Rect::new(rect.x, rect.y, rect.width, 1),
        );
        if rect.height > 1 {
            frame.render_widget(
                Paragraph::new(format!("  {}", safe_text(&item.description))).style(theme.quiet()),
                Rect::new(rect.x, rect.y + 1, rect.width, 1),
            );
        }
        hits.modal_items.push((rect, index));
    }
    if !detail.is_empty() && parts[3].height > 0 {
        let detail_block = Block::default()
            .borders(Borders::TOP)
            .border_style(Style::default().fg(theme.border));
        let detail_area = detail_block.inner(parts[3]);
        hits.modal_detail = detail_area;
        frame.render_widget(detail_block, parts[3]);
        let content = Text::from(detail);
        let lines = Paragraph::new(borrow_text(&content, 0))
            .wrap(Wrap { trim: false })
            .line_count(detail_area.width.max(1));
        hits.modal_detail_max_scroll = lines.saturating_sub(usize::from(detail_area.height));
        let scroll = modal.scroll.min(hits.modal_detail_max_scroll);
        frame.render_widget(
            scrolled_paragraph(&content, scroll, detail_area.width.max(1)).style(theme.quiet()),
            detail_area,
        );
    }
    let footer = if hits.modal_detail_max_scroll > 0 {
        format!("PgUp/PgDn read · {}", safe_text(&modal.footer))
    } else {
        safe_text(&modal.footer)
    };
    frame.render_widget(Paragraph::new(footer).style(theme.strong()), parts[4]);
}

fn panel(title: &str, focused: bool, theme: Theme) -> Block<'static> {
    Block::default()
        .borders(Borders::ALL)
        .title(Line::styled(
            format!(
                " {} ",
                if title.is_empty() {
                    "Conversation"
                } else {
                    title
                }
            ),
            if focused {
                theme.strong()
            } else {
                Style::default().fg(theme.text)
            },
        ))
        .border_style(Style::default().fg(if focused { theme.accent } else { theme.border }))
        .style(Style::default().fg(theme.text).bg(theme.panel))
        .padding(Padding::horizontal(1))
}

fn inset(area: Rect, horizontal: u16, vertical: u16) -> Rect {
    let x = horizontal.min(area.width / 2);
    let y = vertical.min(area.height / 2);
    Rect::new(
        area.x + x,
        area.y + y,
        area.width.saturating_sub(2 * x),
        area.height.saturating_sub(2 * y),
    )
}

fn role_label(role: &str) -> &str {
    match role {
        "user" | "human" => "You",
        "assistant" | "agent" => "Assistant",
        "tool" => "Tool",
        "system" => "System",
        _ => role,
    }
}

fn status_style(status: &str, theme: Theme) -> (&'static str, Color) {
    let status = status.to_ascii_lowercase();
    if status.contains("error") || status.contains("failed") {
        ("!", ERROR)
    } else if status.contains("request")
        || status.contains("approval")
        || status.contains("blocked")
        || status.contains("attention")
    {
        ("?", WAITING)
    } else if status.contains("working")
        || status.contains("running")
        || status.contains("busy")
        || status.contains("streaming")
    {
        ("●", theme.accent)
    } else if status.contains("done") || status.contains("finished") || status.contains("complete")
    {
        ("✓", DONE)
    } else if status.contains("offline") || status.contains("stale") {
        ("○", WAITING)
    } else {
        ("·", theme.muted)
    }
}

pub fn modal_fingerprint(modal: &Modal) -> u64 {
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    modal.title.hash(&mut hasher);
    modal.description.hash(&mut hasher);
    modal.detail.hash(&mut hasher);
    modal.action_stamp.hash(&mut hasher);
    modal.editor.hash(&mut hasher);
    for item in &modal.items {
        item.label.hash(&mut hasher);
        item.description.hash(&mut hasher);
        item.checked.hash(&mut hasher);
    }
    hasher.finish()
}

fn message_key(message: &MessageView, width: u16, theme: &str) -> MessageKey {
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    message.text.hash(&mut hasher);
    message.role.hash(&mut hasher);
    message.truncated.hash(&mut hasher);
    MessageKey {
        id: message.key.clone(),
        hash: hasher.finish(),
        width,
        theme: theme.to_owned(),
    }
}

fn own_text(text: Text<'_>) -> Text<'static> {
    Text {
        alignment: text.alignment,
        style: text.style,
        lines: text
            .lines
            .into_iter()
            .map(|line| Line {
                alignment: line.alignment,
                style: line.style,
                spans: line
                    .spans
                    .into_iter()
                    .map(|span| Span::styled(span.content.into_owned(), span.style))
                    .collect(),
            })
            .collect(),
    }
}

fn borrow_text<'a>(text: &'a Text<'_>, skip: usize) -> Text<'a> {
    Text {
        alignment: text.alignment,
        style: text.style,
        lines: text
            .lines
            .iter()
            .skip(skip)
            .map(|line| Line {
                alignment: line.alignment,
                style: line.style,
                spans: line
                    .spans
                    .iter()
                    .map(|span| Span::styled(span.content.as_ref(), span.style))
                    .collect(),
            })
            .collect(),
    }
}

fn scrolled_paragraph<'a>(text: &'a Text<'_>, mut offset: usize, width: u16) -> Paragraph<'a> {
    // Ratatui's per-paragraph offset is u16. Drop complete logical lines first,
    // so even a transcript with more than 65,535 lines reaches its actual tail.
    let mut skip = 0;
    if offset > u16::MAX as usize {
        for line in &text.lines {
            let height = Paragraph::new(line.clone())
                .wrap(Wrap { trim: false })
                .line_count(width)
                .max(1);
            if height > offset {
                break;
            }
            offset -= height;
            skip += 1;
            if offset <= u16::MAX as usize {
                break;
            }
        }
    }
    if offset > u16::MAX as usize {
        return Paragraph::new("This single paragraph exceeds the terminal viewport range. Use the full-source transcript action to read or copy it.")
            .wrap(Wrap { trim: false }).style(Style::default().fg(WAITING));
    }
    Paragraph::new(borrow_text(text, skip))
        .wrap(Wrap { trim: false })
        .scroll((offset as u16, 0))
}

/// Never let server text inject terminal control sequences. The controller keeps
/// exact source strings separately; only this display projection is sanitized.
fn safe_text(text: &str) -> String {
    text.chars()
        .filter(|c| !c.is_control() || matches!(c, '\n' | '\t'))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use ratatui::Terminal;
    use ratatui::backend::TestBackend;

    fn fixture() -> View {
        View {
            title: "Release review".into(), mode: Mode::Developer, focus: Focus::Composer,
            status: "M4 connected · OMV reconnecting".into(), hint: "Tab focus · Ctrl+P commands".into(),
            lane: "attention".into(), follow: true, requests: 1, pending: 1,
            rows: (0..18).map(|i| ThreadView { key: format!("m4:{i}"), title: format!("Conversation {i}"), subtitle: "M4 · t3oxide".into(), status: if i == 0 { "approval" } else { "working" }.into(), depth: usize::from(i == 2), pinned: i == 1 }).collect(),
            messages: vec![
                MessageView { key: "u1".into(), role: "user".into(), text: "Review the implementation. 日本語 and 🦀 work too.".into(), ..Default::default() },
                MessageView { key: "a1".into(), role: "assistant".into(), text: "## Ready for review\n\n- Exact operations retained\n- **Drafts** survive reconnects\n\n```rust\nlet ready = true;\n```\n\n| Host | State |\n| --- | --- |\n| M4 | Connected |".into(), streaming: true, ..Default::default() },
            ],
            inspector: vec![("Environment".into(), "M4".into()), ("Provider".into(), "Codex · gpt-6".into())],
            ..Default::default()
        }
    }

    fn buffer_text(terminal: &Terminal<TestBackend>) -> String {
        terminal
            .backend()
            .buffer()
            .content
            .iter()
            .map(|cell| cell.symbol())
            .collect::<String>()
    }

    #[test]
    fn layouts_and_hit_targets_survive_resize_and_both_modes() {
        let composer = editor("A durable draft 日本語", "", "Message");
        let empty = editor("", "", "Search");
        for (width, height) in [
            (1, 1),
            (23, 7),
            (24, 8),
            (40, 12),
            (80, 24),
            (120, 35),
            (180, 50),
        ] {
            for mode in [Mode::Developer, Mode::AgentBox] {
                for focus in [
                    Focus::Navigator,
                    Focus::Transcript,
                    Focus::Composer,
                    Focus::Inspector,
                ] {
                    let mut terminal = Terminal::new(TestBackend::new(width, height)).unwrap();
                    let mut renderer = Renderer::default();
                    let mut view = fixture();
                    view.mode = mode;
                    view.focus = focus;
                    let mut hits = HitMap::default();
                    terminal
                        .draw(|f| hits = renderer.draw(f, &view, &composer, None, &empty))
                        .unwrap();
                    for rect in hits
                        .rows
                        .iter()
                        .map(|r| r.0)
                        .chain(hits.message_rows.iter().map(|r| r.0))
                        .chain([hits.composer, hits.modal_editor])
                    {
                        assert!(
                            rect.right() <= width && rect.bottom() <= height,
                            "{width}x{height}: {rect:?}"
                        );
                    }
                }
            }
        }
    }

    #[test]
    fn modal_selection_is_visible_and_mouse_addressable() {
        let mut terminal = Terminal::new(TestBackend::new(100, 30)).unwrap();
        let mut renderer = Renderer::default();
        let input = editor("queue", "", "Search commands");
        let modal = Modal {
            title: "Commands".into(),
            editor: true,
            selected: 19,
            items: (0..20)
                .map(|i| ModalItem {
                    label: format!("Action {i}"),
                    description: "Native T3 action".into(),
                    checked: false,
                })
                .collect(),
            footer: "Enter choose · Esc close".into(),
            ..Default::default()
        };
        let mut hits = HitMap::default();
        terminal
            .draw(|f| hits = renderer.draw(f, &fixture(), &input, Some(&modal), &input))
            .unwrap();
        assert!(hits.modal_items.iter().any(|(_, i)| *i == 19));
        assert!(hits.modal_editor.width > 0);
        assert!(buffer_text(&terminal).contains("Action 19"));
    }

    #[test]
    fn recovery_payload_and_long_prompt_are_readable_without_changing_choice() {
        let mut terminal = Terminal::new(TestBackend::new(100, 30)).unwrap();
        let mut renderer = Renderer::default();
        let input = editor("", "", "");
        let mut modal = Modal {
            title: "Review exact original action".into(),
            description: "Native prompt with full context. ".repeat(100),
            detail: (0..200)
                .map(|i| format!("Original payload line {i}\n"))
                .collect(),
            items: vec![
                ModalItem {
                    label: "Keep saved".into(),
                    ..Default::default()
                },
                ModalItem {
                    label: "Reconcile original".into(),
                    ..Default::default()
                },
            ],
            selected: 1,
            ..Default::default()
        };
        let mut hits = HitMap::default();
        terminal
            .draw(|f| hits = renderer.draw(f, &fixture(), &input, Some(&modal), &input))
            .unwrap();
        assert!(hits.modal_detail.height > 5);
        assert!(hits.modal_detail_max_scroll > 190);
        assert!(buffer_text(&terminal).contains("Native prompt with full context"));
        modal.scroll = hits.modal_detail_max_scroll;
        terminal
            .draw(|f| hits = renderer.draw(f, &fixture(), &input, Some(&modal), &input))
            .unwrap();
        assert!(buffer_text(&terminal).contains("Original payload line 199"));
        assert!(hits.modal_items.iter().any(|(_, index)| *index == 1));
        assert_eq!(modal.selected, 1);
    }

    #[test]
    fn reading_anchors_use_wrapped_message_identity() {
        let mut terminal = Terminal::new(TestBackend::new(90, 24)).unwrap();
        let mut renderer = Renderer::default();
        let mut view = fixture();
        view.messages[0].text = "wide words 日本語 ".repeat(80);
        let input = editor("", "", "");
        terminal
            .draw(|f| {
                renderer.draw(f, &view, &input, None, &input);
            })
            .unwrap();
        let offset = renderer
            .transcript_item_offset(&view.messages[1].key)
            .unwrap();
        assert!(offset > 10);
        assert_eq!(
            renderer.transcript_item_at(offset + 1),
            Some((view.messages[1].key.clone(), 1))
        );
    }

    #[test]
    fn follow_and_manual_scroll_use_wrapped_line_counts() {
        let mut terminal = Terminal::new(TestBackend::new(90, 24)).unwrap();
        let mut renderer = Renderer::default();
        let mut view = fixture();
        view.messages[1].text = "long response with multiple words ".repeat(200);
        let input = editor("", "", "Message");
        terminal
            .draw(|f| {
                renderer.draw(f, &view, &input, None, &input);
            })
            .unwrap();
        assert!(renderer.transcript_max_scroll() > 100);
        assert_eq!(
            renderer.transcript_scroll(),
            renderer.transcript_max_scroll()
        );
        view.follow = false;
        view.transcript_scroll = 4;
        terminal
            .draw(|f| {
                renderer.draw(f, &view, &input, None, &input);
            })
            .unwrap();
        assert_eq!(renderer.transcript_scroll(), 4);
    }

    #[test]
    fn markdown_cache_is_bounded_and_stream_updates_invalidate() {
        let mut renderer = Renderer::default();
        let theme = Theme::named("");
        let mut message = MessageView {
            key: "stream".into(),
            role: "assistant".into(),
            ..Default::default()
        };
        for i in 0..MAX_CACHE_ITEMS + 30 {
            message.text = format!("Revision **{i}**");
            let key = message_key(&message, 60, "");
            let text = renderer.message_text(&key, &message, theme);
            assert!(text.to_string().contains(&i.to_string()));
        }
        assert!(renderer.cache.len() <= MAX_CACHE_ITEMS);
        assert!(renderer.cache_bytes <= MAX_CACHE_BYTES);
    }

    #[test]
    fn display_controls_are_removed_without_changing_source() {
        let original = "text\x1b]52;c;hidden\x07\n日本語\tend";
        let safe = safe_text(original);
        assert!(!safe.contains('\x1b') && !safe.contains('\x07'));
        assert!(safe.contains("日本語\tend"));
        assert!(original.contains('\x1b'));
    }
}
