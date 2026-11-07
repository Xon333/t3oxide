//! Explicit human input. Paste never enters the command dispatcher, and mouse
//! targets use the identities that were actually drawn rather than a reused row.
use super::*;
use crossterm::event::{
    Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers, MouseButton, MouseEvent, MouseEventKind,
};
use ratatui::layout::{Position, Rect};
use ratatui_textarea::DataCursor;

const MAX_EDITOR_LINES: usize = 9_999;

impl App {
    pub(super) fn input(&mut self, event: Event) -> Result<()> {
        if self.quit {
            return Ok(());
        }
        match event {
            Event::Key(key) if key.kind != KeyEventKind::Release => self.key(key),
            Event::Paste(text) => self.paste(&text),
            Event::Mouse(mouse) if self.mouse => self.mouse_input(mouse),
            Event::Resize(_, _) | Event::FocusGained => {
                self.dirty = true;
                Ok(())
            }
            Event::FocusLost => {
                self.preserve_open_form()?;
                self.flush_draft()
            }
            _ => Ok(()),
        }
    }

    fn key(&mut self, key: KeyEvent) -> Result<()> {
        let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);
        let alt = key.modifiers.contains(KeyModifiers::ALT);
        let repeated = key.kind == KeyEventKind::Repeat;

        if !repeated && (key.code == KeyCode::F(1) || alt && key.code == KeyCode::Char('h')) {
            self.preserve_open_form()?;
            return self.command("help");
        }
        if ctrl && key.code == KeyCode::Char('t') {
            if !repeated {
                ensure!(
                    self.dialog.is_none() && !self.filtering,
                    "Close the dialog or filter before settling a conversation"
                );
                let settled = if self.focus == Focus::Navigator {
                    self.selected_thread()
                        .context("Select a visible conversation first")?
                        .reviewed()
                } else {
                    let active = self.active.as_ref().context("Open a conversation first")?;
                    self.threads
                        .iter()
                        .find(|thread| {
                            thread.profile == active.profile && thread.id == active.thread
                        })
                        .context("Refresh conversation metadata")?
                        .reviewed()
                };
                self.command(if settled { "unsettle" } else { "settle" })?;
            }
            return Ok(());
        }

        // Stop is global and remains addressed to the attached native run even
        // while another conversation is highlighted or a form is open.
        if ctrl && key.code == KeyCode::Char('s') {
            if !repeated {
                let saved = self.preserve_open_form();
                let stopped = self.command("stop");
                stopped?;
                saved.context(
                    "Stop requested, but the open form could not be saved; its text remains open",
                )?;
            }
            return Ok(());
        }
        if ctrl && key.code == KeyCode::Char('q') {
            if !repeated {
                self.preserve_open_form()?;
                self.flush_draft()?;
                self.save_ui()?;
                self.quit = true;
            }
            return Ok(());
        }
        if ctrl && matches!(key.code, KeyCode::Char('p' | ' ') | KeyCode::Null) {
            if !repeated {
                self.preserve_open_form()?;
                self.palette();
            }
            return Ok(());
        }
        // Legacy terminals encode these as GS/FS. Crossterm maps the same
        // control bytes to '5'/'4'; enhanced keyboard protocols preserve ]/\.
        if ctrl && matches!(key.code, KeyCode::Char(']' | '\\' | '5' | '4')) {
            if !repeated {
                if self.dialog.is_some() {
                    self.accept_dialog()?;
                } else {
                    self.command("send")?;
                }
            }
            return Ok(());
        }
        if ctrl {
            let command = match key.code {
                KeyCode::Char('y') => Some("requests"),
                KeyCode::Char('r') => Some("refresh"),
                KeyCode::Char('f') => Some(if self.focus == Focus::Transcript {
                    "find"
                } else {
                    "search"
                }),
                KeyCode::Char('e') => Some("edit"),
                _ => None,
            };
            if let Some(command) = command {
                if !repeated {
                    self.preserve_open_form()?;
                    self.command(command)?;
                }
                return Ok(());
            }
            if matches!(key.code, KeyCode::Char('o' | 'l')) {
                self.preserve_open_form()?;
                self.dialog = None;
                self.cancel_modal_read();
                self.focus = if key.code == KeyCode::Char('o') {
                    Focus::Navigator
                } else {
                    Focus::Inspector
                };
                self.dirty = true;
                return Ok(());
            }
        }
        if !repeated && key.code == KeyCode::F(3) {
            self.layout = match self.layout {
                LayoutMode::Split => LayoutMode::Focus,
                LayoutMode::Focus => LayoutMode::Wide,
                LayoutMode::Wide => LayoutMode::Split,
            };
            self.dirty = true;
            return Ok(());
        }
        if alt && !repeated {
            let command = match key.code {
                KeyCode::Up => Some("parent"),
                KeyCode::Left => Some("recent"),
                KeyCode::Char('r') => Some("recall"),
                _ => None,
            };
            if let Some(command) = command {
                self.preserve_open_form()?;
                self.command(command)?;
                return Ok(());
            }
        }

        if self.dialog.is_some() {
            return self.dialog_key(key);
        }

        if matches!(key.code, KeyCode::Tab | KeyCode::BackTab) && !ctrl && !alt {
            self.focus_next(
                key.code == KeyCode::BackTab || key.modifiers.contains(KeyModifiers::SHIFT),
            );
            return Ok(());
        }

        if self.filtering && self.focus == Focus::Navigator {
            return self.filter_key(key);
        }

        match self.focus {
            Focus::Composer => {
                if key.code == KeyCode::Esc
                    || (ctrl
                        && key.code == KeyCode::Char('c')
                        && self.composer.selection_range().is_none())
                {
                    self.flush_draft()?;
                    self.focus = Focus::Navigator;
                    self.dirty = true;
                } else {
                    ensure!(
                        self.active.is_some(),
                        "Open a conversation or create one before writing a draft"
                    );
                    if editor_key(&mut self.composer, key)? {
                        self.changed_draft();
                    }
                    self.dirty = true;
                }
            }
            Focus::Navigator => self.navigator_key(key)?,
            Focus::Transcript => match key.code {
                KeyCode::Up | KeyCode::Char('k') => self.scroll_transcript(-1),
                KeyCode::Down | KeyCode::Char('j') => self.scroll_transcript(1),
                KeyCode::PageUp => self
                    .scroll_transcript(-(self.renderer.transcript_visible_lines().max(1) as isize)),
                KeyCode::PageDown => {
                    self.scroll_transcript(self.renderer.transcript_visible_lines().max(1) as isize)
                }
                KeyCode::Home => {
                    self.follow = false;
                    self.transcript_scroll = 0;
                    self.dirty = true;
                }
                KeyCode::End => {
                    self.follow = true;
                    self.dirty = true;
                }
                KeyCode::Char('[') if !ctrl && !alt && !repeated => self.command("earlier")?,
                KeyCode::Char(']') if !ctrl && !alt && !repeated => self.command("latest")?,
                KeyCode::Esc => {
                    self.focus = Focus::Navigator;
                    self.dirty = true;
                }
                KeyCode::Char('i') | KeyCode::Enter => {
                    self.focus = Focus::Composer;
                    self.dirty = true;
                }
                KeyCode::Char('?') if !repeated => self.command("help")?,
                _ => {}
            },
            Focus::Inspector => match key.code {
                KeyCode::Esc => {
                    self.focus = Focus::Navigator;
                    self.dirty = true;
                }
                KeyCode::Enter | KeyCode::Char('i') => {
                    self.focus = Focus::Composer;
                    self.dirty = true;
                }
                KeyCode::Char('?') if !repeated => self.command("help")?,
                _ => {}
            },
        }
        Ok(())
    }

    fn navigator_key(&mut self, key: KeyEvent) -> Result<()> {
        let repeated = key.kind == KeyEventKind::Repeat;
        let modified = key
            .modifiers
            .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT);
        if modified {
            return Ok(());
        }
        let count = self.visible.len();
        let page = (usize::from(self.hits.navigator.height.saturating_sub(5)) / 2).max(1);
        match key.code {
            KeyCode::Char('c') if !repeated && self.lane == "Open" && !self.filtering => {
                self.toggle_completed()?
            }
            KeyCode::Down | KeyCode::Char('j') => {
                self.selected = self.selected.saturating_add(1).min(count.saturating_sub(1))
            }
            KeyCode::Up | KeyCode::Char('k') => self.selected = self.selected.saturating_sub(1),
            KeyCode::PageDown => {
                self.selected = self
                    .selected
                    .saturating_add(page)
                    .min(count.saturating_sub(1))
            }
            KeyCode::PageUp => self.selected = self.selected.saturating_sub(page),
            KeyCode::Home => self.selected = 0,
            KeyCode::End => self.selected = count.saturating_sub(1),
            KeyCode::Enter if !repeated => {
                if let Some(thread) = self.selected_thread().cloned() {
                    self.open(thread.profile, thread.id)?;
                }
            }
            KeyCode::Right => {
                if let Some(thread) = self.selected_thread() {
                    self.expanded.insert(thread.key());
                    self.rebuild();
                }
            }
            KeyCode::Left => {
                if let Some(thread) = self.selected_thread().cloned() {
                    if self.expanded.remove(&thread.key()) {
                        self.rebuild();
                    } else if let Some(parent) = thread.parent()
                        && let Some(index) = self.visible.iter().position(|(i, _)| {
                            self.threads[*i].profile == thread.profile
                                && self.threads[*i].id == parent
                        })
                    {
                        self.selected = index;
                    }
                }
            }
            KeyCode::Char('/') => {
                self.server_search = false;
                self.filtering = true;
                self.rebuild();
            }
            KeyCode::Esc => {
                self.filter.clear();
                self.server_search = false;
                self.filtering = false;
                self.rebuild();
            }
            KeyCode::Char('i') => self.focus = Focus::Composer,
            KeyCode::Char(c) if !repeated => {
                let command = match c {
                    'n' => Some("new"),
                    'v' => Some("views"),
                    'e' => Some("env"),
                    'r' => Some("refresh"),
                    'm' => Some("model"),
                    'a' => Some("activity"),
                    'd' => Some("review"),
                    'x' => Some("reviewed"),
                    ' ' => Some("preview"),
                    '?' => Some("help"),
                    ':' => Some("commands"),
                    _ => None,
                };
                if let Some(command) = command {
                    self.command(command)?;
                }
            }
            _ => {}
        }
        self.dirty = true;
        Ok(())
    }

    fn filter_key(&mut self, key: KeyEvent) -> Result<()> {
        self.server_search = false;
        match key.code {
            KeyCode::Esc => {
                self.filter.clear();
                self.filtering = false;
            }
            KeyCode::Enter => self.filtering = false,
            KeyCode::Backspace => {
                self.filter.pop();
            }
            KeyCode::Char(c)
                if !key
                    .modifiers
                    .intersects(KeyModifiers::CONTROL | KeyModifiers::ALT) =>
            {
                ensure!(
                    self.filter.chars().count() < 200,
                    "Filter is limited to 200 Unicode characters"
                );
                self.filter.push(c);
            }
            KeyCode::Down | KeyCode::Up | KeyCode::PageUp | KeyCode::PageDown => {
                return self.navigator_key(key);
            }
            _ => {}
        }
        self.rebuild();
        Ok(())
    }

    fn dialog_key(&mut self, key: KeyEvent) -> Result<()> {
        let Some(kind) = self.dialog.as_ref().map(|dialog| dialog.kind.clone()) else {
            return Ok(());
        };
        if key.code == KeyCode::Esc {
            self.preserve_open_form()?;
            self.dialog = None;
            self.cancel_modal_read();
            self.dirty = true;
            return Ok(());
        }
        if self.hits.modal_detail.height > 0 {
            let page = self.hits.modal_detail.height.saturating_sub(1).max(1) as isize;
            let amount = match key.code {
                KeyCode::PageUp => Some(-page),
                KeyCode::PageDown => Some(page),
                KeyCode::Home if key.modifiers.contains(KeyModifiers::CONTROL) => Some(isize::MIN),
                KeyCode::End if key.modifiers.contains(KeyModifiers::CONTROL) => Some(isize::MAX),
                _ => None,
            };
            if let Some(amount) = amount {
                self.scroll_dialog_detail(amount);
                return Ok(());
            }
        }
        match kind {
            DialogKind::Menu | DialogKind::Palette => match key.code {
                KeyCode::Down | KeyCode::Tab => self.move_dialog(1),
                KeyCode::Up | KeyCode::BackTab => self.move_dialog(-1),
                KeyCode::PageDown => self.move_dialog(8),
                KeyCode::PageUp => self.move_dialog(-8),
                KeyCode::Enter if key.kind != KeyEventKind::Repeat => self.accept_dialog()?,
                KeyCode::Home if matches!(kind, DialogKind::Menu) => {
                    if let Some(dialog) = &mut self.dialog {
                        dialog.selected = 0;
                    }
                }
                KeyCode::End if matches!(kind, DialogKind::Menu) => {
                    if let Some(dialog) = &mut self.dialog {
                        dialog.selected = dialog.choices.len().saturating_sub(1);
                    }
                }
                _ if matches!(kind, DialogKind::Palette) => {
                    if editor_key(&mut self.modal_editor, key)? {
                        self.filter_palette();
                    }
                }
                KeyCode::Char('j') => self.move_dialog(1),
                KeyCode::Char('k') => self.move_dialog(-1),
                _ => {}
            },
            DialogKind::Text(target) => {
                if key.code == KeyCode::Enter
                    && !matches!(target, InputTarget::LaunchMessage | InputTarget::Batch)
                    && !key.modifiers.contains(KeyModifiers::ALT)
                {
                    if key.kind != KeyEventKind::Repeat {
                        self.accept_dialog()?;
                    }
                } else if matches!(key.code, KeyCode::Tab | KeyCode::BackTab) {
                    // Text forms keep focus until saved or dismissed.
                } else if editor_key(&mut self.modal_editor, key)? {
                    self.form_save_at = Some(Instant::now() + Duration::from_millis(200));
                }
            }
            DialogKind::Detail => {
                if let Some(dialog) = &mut self.dialog {
                    match key.code {
                        KeyCode::Up | KeyCode::Char('k') => {
                            dialog.scroll = dialog.scroll.saturating_sub(1)
                        }
                        KeyCode::Down | KeyCode::Char('j') => {
                            dialog.scroll = dialog.scroll.saturating_add(1)
                        }
                        KeyCode::PageUp => dialog.scroll = dialog.scroll.saturating_sub(12),
                        KeyCode::PageDown => dialog.scroll = dialog.scroll.saturating_add(12),
                        KeyCode::Home => dialog.scroll = 0,
                        KeyCode::End => {
                            dialog.scroll = self.hits.modal_detail_max_scroll;
                        }
                        _ => {}
                    }
                    dialog.scroll = dialog.scroll.min(self.hits.modal_detail_max_scroll);
                }
            }
        }
        self.dirty = true;
        Ok(())
    }

    fn accept_dialog(&mut self) -> Result<()> {
        let Some(dialog) = self.dialog.clone() else {
            return Ok(());
        };
        let result = match &dialog.kind {
            DialogKind::Text(target) => self.submit_text(target.clone()),
            DialogKind::Menu | DialogKind::Palette => {
                if let Some(choice) = dialog.choices.get(dialog.selected) {
                    self.dispatch(choice.intent.clone())
                } else {
                    Ok(())
                }
            }
            DialogKind::Detail => Ok(()),
        };
        if result.is_err() && self.dialog.is_none() {
            self.dialog = Some(dialog);
        }
        result
    }

    fn move_dialog(&mut self, amount: isize) {
        if let Some(dialog) = &mut self.dialog {
            dialog.selected = dialog
                .selected
                .saturating_add_signed(amount)
                .min(dialog.choices.len().saturating_sub(1));
        }
        self.dirty = true;
    }

    fn scroll_dialog_detail(&mut self, amount: isize) {
        if let Some(dialog) = &mut self.dialog {
            dialog.scroll = dialog
                .scroll
                .min(self.hits.modal_detail_max_scroll)
                .saturating_add_signed(amount)
                .min(self.hits.modal_detail_max_scroll);
        }
        self.dirty = true;
    }

    fn focus_next(&mut self, reverse: bool) {
        self.focus = match (self.focus, reverse) {
            (Focus::Navigator, false) | (Focus::Composer, true) => Focus::Transcript,
            (Focus::Transcript, false) | (Focus::Inspector, true) => Focus::Composer,
            (Focus::Composer, false) | (Focus::Navigator, true) => Focus::Inspector,
            (Focus::Inspector, false) | (Focus::Transcript, true) => Focus::Navigator,
        };
        self.dirty = true;
    }

    fn scroll_transcript(&mut self, amount: isize) {
        let top = if self.follow {
            self.renderer.transcript_scroll()
        } else {
            self.transcript_scroll
        };
        self.transcript_scroll = top
            .saturating_add_signed(amount)
            .min(self.renderer.transcript_max_scroll());
        self.follow = false;
        self.dirty = true;
    }

    fn paste(&mut self, text: &str) -> Result<()> {
        if let Some(kind) = self.dialog.as_ref().map(|dialog| dialog.kind.clone()) {
            if matches!(kind, DialogKind::Text(_) | DialogKind::Palette)
                && editor_paste(&mut self.modal_editor, text)?
            {
                if matches!(kind, DialogKind::Palette) {
                    self.filter_palette();
                } else {
                    self.form_save_at = Some(Instant::now() + Duration::from_millis(200));
                }
            }
        } else if self.filtering && self.focus == Focus::Navigator {
            self.server_search = false;
            let text = plain(text).replace(['\r', '\n', '\t'], " ");
            ensure!(
                self.filter.chars().count() + text.chars().count() <= 200,
                "Filter exceeds its limit"
            );
            self.filter.push_str(&text);
            self.rebuild();
        } else if self.focus == Focus::Composer {
            ensure!(
                self.active.is_some(),
                "Open a conversation or create one before writing a draft"
            );
            if editor_paste(&mut self.composer, text)? {
                self.changed_draft();
            }
        }
        self.dirty = true;
        Ok(())
    }

    fn mouse_input(&mut self, event: MouseEvent) -> Result<()> {
        let point = Position::new(event.column, event.row);
        if self.dialog.is_some() {
            if contains(self.hits.modal_detail, point) {
                match event.kind {
                    MouseEventKind::ScrollUp => self.scroll_dialog_detail(-3),
                    MouseEventKind::ScrollDown => self.scroll_dialog_detail(3),
                    _ => {}
                }
                return Ok(());
            }
            if contains(self.hits.modal_editor, point) {
                if editor_mouse(&mut self.modal_editor, self.hits.modal_editor, event) {
                    self.preserve_open_form()?;
                }
                self.dirty = true;
                return Ok(());
            }
            match event.kind {
                MouseEventKind::ScrollUp => {
                    if self
                        .dialog
                        .as_ref()
                        .is_some_and(|d| matches!(d.kind, DialogKind::Detail))
                    {
                        if let Some(d) = &mut self.dialog {
                            d.scroll = d.scroll.saturating_sub(3);
                        }
                    } else {
                        self.move_dialog(-3);
                    }
                }
                MouseEventKind::ScrollDown => {
                    if self
                        .dialog
                        .as_ref()
                        .is_some_and(|d| matches!(d.kind, DialogKind::Detail))
                    {
                        if let Some(d) = &mut self.dialog {
                            d.scroll = d
                                .scroll
                                .saturating_add(3)
                                .min(self.hits.modal_detail_max_scroll);
                        }
                    } else {
                        self.move_dialog(3);
                    }
                }
                MouseEventKind::Down(MouseButton::Left) => {
                    if let Some((_, index)) = self
                        .hits
                        .modal_items
                        .iter()
                        .find(|(rect, _)| contains(*rect, point))
                        .copied()
                    {
                        let current = self.modal().map(|modal| ui::modal_fingerprint(&modal));
                        ensure!(
                            current == Some(self.hits.modal_stamp),
                            "The choices changed; review the refreshed dialog before selecting"
                        );
                        if let Some(dialog) = &mut self.dialog {
                            dialog.selected = index;
                        }
                        self.accept_dialog()?;
                    }
                }
                _ => {}
            }
            self.dirty = true;
            return Ok(());
        }
        if matches!(event.kind, MouseEventKind::Down(MouseButton::Left)) {
            if contains(self.hits.completed, point) {
                self.focus = Focus::Navigator;
                return self.toggle_completed();
            }
            if let Some((_, layout)) = self
                .hits
                .layout_tabs
                .iter()
                .find(|(rect, _)| contains(*rect, point))
                .copied()
            {
                return self.dispatch(Intent::Layout(layout));
            }
            if let Some((_, lane)) = self
                .hits
                .lane_tabs
                .iter()
                .find(|(rect, _)| contains(*rect, point))
                .cloned()
            {
                return self.dispatch(Intent::Lane(lane));
            }
            if contains(self.hits.palette, point) {
                self.palette();
                return Ok(());
            }
            if contains(self.hits.follow, point) {
                self.follow = true;
                self.dirty = true;
                return Ok(());
            }
            if let Some((_, key)) = self
                .hits
                .thread_keys
                .iter()
                .find(|(rect, _)| contains(*rect, point))
                .cloned()
            {
                let thread = self
                    .threads
                    .iter()
                    .find(|thread| thread.key() == key)
                    .cloned()
                    .context("That conversation changed; select it from the refreshed list")?;
                if let Some(index) = self
                    .visible
                    .iter()
                    .position(|(i, _)| self.threads[*i].key() == key)
                {
                    self.selected = index;
                }
                return self.open(thread.profile, thread.id);
            }
        }
        if contains(self.hits.composer, point) {
            if matches!(event.kind, MouseEventKind::Down(_)) {
                self.focus = Focus::Composer;
            }
            editor_mouse(&mut self.composer, self.hits.composer, event);
        } else if contains(self.hits.transcript, point) {
            match event.kind {
                MouseEventKind::ScrollUp => self.scroll_transcript(-3),
                MouseEventKind::ScrollDown => self.scroll_transcript(3),
                MouseEventKind::Down(MouseButton::Right) => {
                    if let Some((_, key)) = self
                        .hits
                        .message_keys
                        .iter()
                        .find(|(rect, _)| contains(*rect, point))
                    {
                        let item = self.active.as_ref().and_then(|a| a.projection.snapshot().and_then(|snapshot| snapshot.items.iter().find(|row| format!("{}\0{}\0{}\0{}", a.profile, a.thread, a.generation, row.key) == *key))).cloned().context("That source item changed; inspect it after the transcript refreshes")?;
                        self.dispatch(Intent::Inspect(serde_json::to_value(item)?))?;
                    }
                }
                MouseEventKind::Down(_) => self.focus = Focus::Transcript,
                _ => {}
            }
        } else if contains(self.hits.navigator, point) {
            match event.kind {
                MouseEventKind::ScrollUp => self.selected = self.selected.saturating_sub(3),
                MouseEventKind::ScrollDown => {
                    self.selected = self
                        .selected
                        .saturating_add(3)
                        .min(self.visible.len().saturating_sub(1))
                }
                MouseEventKind::Down(_) => self.focus = Focus::Navigator,
                _ => {}
            }
        } else if contains(self.hits.inspector, point)
            && matches!(event.kind, MouseEventKind::Down(_))
        {
            self.focus = Focus::Inspector;
        }
        self.dirty = true;
        Ok(())
    }
}

fn contains(rect: Rect, point: Position) -> bool {
    rect.width > 0 && rect.height > 0 && rect.contains(point)
}

fn editor_bytes(editor: &TextArea<'_>) -> usize {
    editor
        .lines()
        .iter()
        .map(String::len)
        .sum::<usize>()
        .saturating_add(editor.lines().len().saturating_sub(1))
}

fn editor_key(editor: &mut TextArea<'static>, key: KeyEvent) -> Result<bool> {
    let before_bytes = editor_bytes(editor);
    let before_lines = editor.lines().len();
    let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);
    let copying = ctrl && key.code == KeyCode::Char('v');
    let before = if before_bytes.saturating_add(4) > crate::state::MAX_DRAFT_BYTES
        || before_lines + 1 >= MAX_EDITOR_LINES
        || copying
    {
        Some(editor.clone())
    } else {
        None
    };
    let changed = if ctrl && key.code == KeyCode::Char('a') {
        editor.select_all();
        false
    } else if ctrl && matches!(key.code, KeyCode::Char('z' | 'Z')) {
        if key.modifiers.contains(KeyModifiers::SHIFT) || key.code == KeyCode::Char('Z') {
            editor.redo()
        } else {
            editor.undo()
        }
    } else if copying {
        editor.paste()
    } else {
        editor.input(key)
    };
    if editor_bytes(editor) > crate::state::MAX_DRAFT_BYTES && editor_bytes(editor) > before_bytes
        || editor.lines().len() > MAX_EDITOR_LINES && editor.lines().len() > before_lines
    {
        if let Some(before) = before {
            *editor = before;
        }
        bail!("Editor limit reached: 64 KiB or 9,999 lines; existing text is preserved");
    }
    Ok(changed)
}

fn editor_paste(editor: &mut TextArea<'static>, text: &str) -> Result<bool> {
    ensure!(
        text.len() <= crate::state::MAX_DRAFT_BYTES,
        "Paste exceeds 64 KiB; draft unchanged"
    );
    let normalized = text.replace("\r\n", "\n").replace('\r', "\n");
    let before = editor.clone();
    let changed = editor.insert_str(normalized);
    if editor_bytes(editor) > crate::state::MAX_DRAFT_BYTES
        || editor.lines().len() > MAX_EDITOR_LINES
    {
        *editor = before;
        bail!("Paste exceeds 64 KiB or 9,999 lines; draft unchanged");
    }
    Ok(changed)
}

fn editor_mouse(editor: &mut TextArea<'_>, area: Rect, event: MouseEvent) -> bool {
    let (top, left) = editor.scroll_offset();
    let row = usize::from(event.row.saturating_sub(area.y).saturating_add(top));
    let column = usize::from(
        event
            .column
            .saturating_sub(area.x)
            .saturating_sub(editor.line_number_width())
            .saturating_add(left),
    );
    let DataCursor(row, column) = editor.screen_to_data(row, column);
    let cursor = CursorMove::Jump(
        row.min(u16::MAX as usize) as u16,
        column.min(u16::MAX as usize) as u16,
    );
    match event.kind {
        MouseEventKind::Down(MouseButton::Left) => {
            editor.cancel_selection();
            editor.move_cursor(cursor);
            editor.start_selection();
            false
        }
        MouseEventKind::Drag(MouseButton::Left) => {
            editor.move_cursor(cursor);
            false
        }
        MouseEventKind::Up(MouseButton::Left) => {
            if editor
                .selection_range()
                .is_some_and(|(start, end)| start == end)
            {
                editor.cancel_selection();
            }
            false
        }
        MouseEventKind::ScrollUp | MouseEventKind::ScrollDown => editor.input(event),
        _ => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    fn app() -> (TempDir, App) {
        let dir = TempDir::new().unwrap();
        let state = Arc::new(State::open(&dir.path().join("state.json")).unwrap());
        let app = App::new(Args::default(), vec![], state).unwrap();
        (dir, app)
    }
    fn key(code: KeyCode, modifiers: KeyModifiers) -> Event {
        Event::Key(KeyEvent::new(code, modifiers))
    }

    #[test]
    fn explicit_filtering_prevents_typing_from_changing_navigation_intent() {
        let (_dir, mut app) = app();
        app.input(key(KeyCode::Char('w'), KeyModifiers::NONE))
            .unwrap();
        assert!(app.filter.is_empty());
        app.input(key(KeyCode::Char('/'), KeyModifiers::NONE))
            .unwrap();
        app.input(key(KeyCode::Char('w'), KeyModifiers::NONE))
            .unwrap();
        assert_eq!(app.filter, "w");
        app.input(key(KeyCode::Esc, KeyModifiers::NONE)).unwrap();
        assert!(app.filter.is_empty() && !app.filtering);
    }

    #[test]
    fn stop_is_global_and_does_not_close_or_submit_a_dialog() {
        let (_dir, mut app) = app();
        app.palette();
        let result = app.input(key(KeyCode::Char('s'), KeyModifiers::CONTROL));
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Open a conversation")
        );
        assert!(matches!(
            app.dialog.as_ref().unwrap().kind,
            DialogKind::Palette
        ));
    }

    #[test]
    fn failed_unrelated_form_save_does_not_prevent_stop_dispatch() {
        let (_dir, mut app) = app();
        app.text_dialog(
            "Retained launch",
            "",
            InputTarget::LaunchMessage,
            "keep me".into(),
        );
        let error = app
            .input(key(KeyCode::Char('s'), KeyModifiers::CONTROL))
            .unwrap_err();
        assert!(error.to_string().contains("Open a conversation"));
        assert_eq!(app.modal_editor.lines().join("\n"), "keep me");
        assert!(app.dialog.is_some());
    }

    #[test]
    fn queued_keys_after_quit_cannot_dispatch_an_action() {
        let (_dir, mut app) = app();
        app.quit = true;
        app.input(key(KeyCode::Char('s'), KeyModifiers::CONTROL))
            .unwrap();
        app.input(key(KeyCode::Char('5'), KeyModifiers::CONTROL))
            .unwrap();
        app.input(Event::Paste("late input".into())).unwrap();
        assert!(app.composer.lines().join("\n").is_empty());
        assert!(app.state.pending_records().is_empty());
    }

    #[test]
    fn filter_limits_characters_without_cutting_unicode() {
        let (_dir, mut app) = app();
        app.input(key(KeyCode::Char('/'), KeyModifiers::NONE))
            .unwrap();
        app.input(Event::Paste("日".repeat(200))).unwrap();
        assert!(
            app.input(key(KeyCode::Char('本'), KeyModifiers::NONE))
                .is_err()
        );
        assert_eq!(app.filter.chars().count(), 200);
        assert!(app.input(Event::Paste("x".into())).is_err());
        assert_eq!(app.filter, "日".repeat(200));
    }

    #[test]
    fn reading_review_detail_never_changes_selected_action() {
        let (_dir, mut app) = app();
        app.menu(
            "Saved action",
            "Review original payload",
            vec![
                Choice::new("Keep saved", "", Intent::Close),
                Choice::new("Another choice", "", Intent::Close),
            ],
        );
        app.dialog.as_mut().unwrap().detail = "long original payload\n".repeat(200);
        app.dialog.as_mut().unwrap().selected = 1;
        app.hits.modal_detail = Rect::new(1, 10, 60, 12);
        app.hits.modal_detail_max_scroll = 188;
        app.input(key(KeyCode::PageDown, KeyModifiers::NONE))
            .unwrap();
        assert_eq!(app.dialog.as_ref().unwrap().selected, 1);
        assert_eq!(app.dialog.as_ref().unwrap().scroll, 11);
        app.input(Event::Mouse(MouseEvent {
            kind: MouseEventKind::ScrollDown,
            column: 5,
            row: 12,
            modifiers: KeyModifiers::NONE,
        }))
        .unwrap();
        assert_eq!(app.dialog.as_ref().unwrap().selected, 1);
        assert_eq!(app.dialog.as_ref().unwrap().scroll, 14);
    }

    #[test]
    fn settle_and_unsettle_are_discoverable_without_duplicate_actions() {
        for (query, expected) in [("settle", "reviewed"), ("unsettle", "unreviewed")] {
            let (_dir, mut app) = app();
            app.palette();
            app.input(Event::Paste(query.into())).unwrap();
            let choices = &app.dialog.as_ref().unwrap().choices;
            assert_eq!(
                choices
                    .iter()
                    .filter(
                        |choice| matches!(&choice.intent, Intent::Command(name) if name == expected)
                    )
                    .count(),
                1,
                "{query} must find the existing native disposition action"
            );
            assert!(app.state.pending_records().is_empty());
        }
    }

    #[test]
    fn palette_fuzzy_query_does_not_execute_a_pasted_command() {
        let (_dir, mut app) = app();
        app.palette();
        app.input(Event::Paste("rcvr".into())).unwrap();
        assert!(
            app.dialog
                .as_ref()
                .unwrap()
                .choices
                .iter()
                .any(|choice| matches!(&choice.intent, Intent::Command(name) if name == "recover"))
        );
        app.input(Event::Paste("\n/stop".into())).unwrap();
        assert!(app.active.is_none() && !app.quit);
        assert!(app.modal_editor.lines().join("\n").contains("/stop"));
    }

    #[test]
    fn oversized_paste_is_transactional_and_unicode_safe() {
        let mut editor = ui::editor("keep 日本語", "violet", "");
        editor.select_all();
        assert!(editor_paste(&mut editor, &"x".repeat(crate::state::MAX_DRAFT_BYTES + 1)).is_err());
        assert_eq!(editor.lines().join("\n"), "keep 日本語");
        assert!(editor_paste(&mut editor, "/stop\r\nliteral").unwrap());
        assert_eq!(editor.lines().join("\n"), "/stop\nliteral");
    }

    #[test]
    fn legacy_control_bytes_and_enhanced_send_keys_share_submit_action() {
        for code in [']', '\\', '5', '4'] {
            let (_dir, mut app) = app();
            app.text_dialog(
                "Literal command field",
                "",
                InputTarget::Command,
                "/unsupported-test-command".into(),
            );
            let error = app
                .input(key(KeyCode::Char(code), KeyModifiers::CONTROL))
                .unwrap_err();
            assert!(
                error
                    .to_string()
                    .contains("Unknown action /unsupported-test-command"),
                "{code}: {error}"
            );
            assert!(app.state.pending_records().is_empty());
        }
    }

    #[test]
    fn new_lines_and_focus_switch_do_not_send() {
        let (_dir, mut app) = app();
        app.dialog = Some(Dialog {
            title: "Brief".into(),
            description: String::new(),
            detail: String::new(),
            choices: vec![],
            selected: 0,
            scroll: 0,
            kind: DialogKind::Text(InputTarget::LaunchMessage),
            footer: String::new(),
        });
        // Without a launch target the persistence call may reject, but input must
        // not dispatch an operation or turn the newline into a submit.
        let _ = app.input(key(KeyCode::Enter, KeyModifiers::NONE));
        assert_eq!(app.modal_editor.lines().len(), 2);
        assert!(!app.quit && app.state.pending_records().is_empty());
    }

    #[test]
    fn stale_modal_click_cannot_select_reused_row() {
        let (_dir, mut app) = app();
        app.palette();
        app.hits.modal_items = vec![(Rect::new(2, 2, 10, 2), 0)];
        app.hits.modal_stamp = 1;
        let result = app.input(Event::Mouse(MouseEvent {
            kind: MouseEventKind::Down(MouseButton::Left),
            column: 3,
            row: 2,
            modifiers: KeyModifiers::NONE,
        }));
        assert!(result.unwrap_err().to_string().contains("choices changed"));
        assert!(app.dialog.is_some());
    }

    #[test]
    fn identical_approval_labels_cannot_retarget_a_drawn_request_identity() {
        let (_dir, mut app) = app();
        app.menu(
            "Native approval",
            "Run verification?",
            vec![Choice::new(
                "Approve once",
                "",
                Intent::Approval(
                    json!({"id": "original", "revision": "one"}),
                    "accept".into(),
                ),
            )],
        );
        app.hits.modal_stamp = ui::modal_fingerprint(&app.modal().unwrap());
        app.hits.modal_items = vec![(Rect::new(2, 2, 30, 2), 0)];
        app.dialog.as_mut().unwrap().choices[0].intent = Intent::Approval(
            json!({"id": "replacement", "revision": "two"}),
            "accept".into(),
        );
        let error = app
            .input(Event::Mouse(MouseEvent {
                kind: MouseEventKind::Down(MouseButton::Left),
                column: 3,
                row: 2,
                modifiers: KeyModifiers::NONE,
            }))
            .unwrap_err();
        assert!(error.to_string().contains("choices changed"));
        assert!(app.state.pending_records().is_empty());
    }

    #[test]
    fn current_help_exposes_input_and_recovery_without_changing_unsent_text() {
        let (_dir, mut app) = app();
        app.focus = Focus::Composer;
        app.replace_text("/settle is literal draft text".into());
        app.input(key(KeyCode::Char('h'), KeyModifiers::ALT))
            .unwrap();
        let help = app.dialog.as_ref().unwrap();
        assert_eq!(help.title, "Commands");
        assert!(matches!(help.kind, DialogKind::Palette));
        assert!(help.description.contains("Enter newline"));
        assert!(
            help.choices
                .iter()
                .any(|choice| choice.label.contains("Ctrl+Enter | Ctrl+]"))
        );
        assert!(help.choices.iter().any(
            |choice| matches!(&choice.intent, Intent::Command(command) if command == "recover")
        ));
        let offered: Vec<_> = help
            .choices
            .iter()
            .map(|choice| choice.label.clone())
            .collect();
        assert_eq!(app.text(), "/settle is literal draft text");
        app.dialog = None;
        app.input(key(KeyCode::Tab, KeyModifiers::NONE)).unwrap();
        assert_eq!(app.focus, Focus::Inspector);
        app.input(key(KeyCode::F(1), KeyModifiers::NONE)).unwrap();
        assert_eq!(app.dialog.as_ref().unwrap().title, "Commands");
        app.dialog = None;
        app.command("shortcuts").unwrap();
        assert_eq!(app.dialog.as_ref().unwrap().title, "Commands");
        assert_eq!(
            app.dialog
                .as_ref()
                .unwrap()
                .choices
                .iter()
                .map(|choice| choice.label.clone())
                .collect::<Vec<_>>(),
            offered
        );
        assert!(app.state.pending_records().is_empty());
    }

    #[test]
    fn superseded_preferences_remain_recoverable_without_overriding_new_contract() {
        let (_dir, app) = app();
        app.state
            .update(|saved| {
                saved["rustUi"] = json!({"mode":"agentbox","theme":"cyan","futureSetting":"keep"});
                Ok(())
            })
            .unwrap();
        let migrated = App::new(
            Args {
                mode: "agentbox".into(),
                ..Args::default()
            },
            vec![],
            app.state.clone(),
        )
        .unwrap();
        assert_eq!(migrated.mode, Mode::Dev);
        assert_eq!(migrated.lane, "Open");
        assert!(!migrated.completed_collapsed);
        migrated.save_ui().unwrap();
        let reopened = App::new(Args::default(), vec![], app.state.clone()).unwrap();
        assert_eq!(reopened.mode, Mode::Dev);
        assert!(!reopened.completed_collapsed);
        assert_eq!(reopened.theme, "violet");
        assert_eq!(reopened.state.snapshot()["rustUi"]["futureSetting"], "keep");
    }

    #[test]
    fn pane_cycle_and_layout_are_reversible() {
        let (_dir, mut app) = app();
        app.input(key(KeyCode::Tab, KeyModifiers::NONE)).unwrap();
        assert_eq!(app.focus, Focus::Transcript);
        app.input(key(KeyCode::BackTab, KeyModifiers::SHIFT))
            .unwrap();
        assert_eq!(app.focus, Focus::Navigator);
        for _ in 0..3 {
            app.input(key(KeyCode::F(3), KeyModifiers::NONE)).unwrap();
        }
        assert_eq!(app.layout, LayoutMode::Split);
    }
}
