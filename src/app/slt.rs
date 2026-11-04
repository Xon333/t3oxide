//! SLT owns terminal input, focus, output and lifecycle; workers carry native data only.
use super::*;
use crate::slt_ui::{self, Action, SurfaceState};

impl App {
    pub(super) fn preserve_open_form(&mut self) -> Result<()> {
        self.flush_form()?;
        self.save_question_draft()?;
        self.preserve_workspace_position()
    }

    pub(super) fn flush_form(&mut self) -> Result<()> {
        self.form_save_at = None;
        if let Some(DialogKind::Text(target)) =
            self.dialog.as_ref().map(|dialog| dialog.kind.clone())
        {
            let text = self.modal_editor.lines().join("\n");
            self.preserve_form_edit(&target, &text)?;
        }
        Ok(())
    }

    pub(super) fn run_slt(mut self) -> Result<()> {
        self.refresh(false, false)?;
        self.restore_tabs()?;
        if !self.args.thread.is_empty() {
            self.open(
                self.creation_profile.context("--thread requires --env")?,
                self.args.thread.clone(),
            )?;
        } else if let Some((profile, thread)) = self.tabs.first().cloned() {
            self.open(profile, thread)?;
        }
        let runtime = tokio::runtime::Builder::new_current_thread()
            .enable_time()
            .build()?;
        let shared = Arc::new(Mutex::new(self));
        let outcome = loop {
            let controller = shared.clone();
            let (mouse, rx) = {
                let app = shared.lock().expect("SLT controller lock poisoned");
                (app.mouse, app.rx.clone())
            };
            let config = ::slt::RunConfig::default()
                .title("T3oxide")
                .mouse(mouse)
                .kitty_keyboard(true)
                // The reader otherwise exits on Ctrl+C before a focused editor
                // can consume a selection copy. Explicit Quit remains Ctrl+Q.
                .handle_ctrl_c(false)
                .activation_boundaries(true)
                .idle_redraw(false)
                .max_fps(60);
            let session = runtime.block_on(async {
                let run =
                    ::slt::run_async_with(config, move |ui, events: &mut Vec<Background>| {
                        let mut app = controller.lock().expect("SLT controller lock poisoned");
                        for event in events.drain(..) {
                            if let Err(error) = app.handle(event) {
                                app.error(error);
                            }
                        }
                        if app
                            .save_at
                            .is_some_and(|deadline| Instant::now() >= deadline)
                            && let Err(error) = app.schedule_save()
                        {
                            app.draft_error = error.to_string();
                            app.save_at = Some(Instant::now() + Duration::from_millis(500));
                            app.error(error);
                        }
                        if app
                            .form_save_at
                            .is_some_and(|deadline| Instant::now() >= deadline)
                            && let Err(error) = app.flush_form()
                        {
                            app.error(error);
                        }
                        if let Err(error) = app.open_pending_question() {
                            app.error(error);
                        }
                        let start_attention = app.args.mode == "b";
                        let state = ui.use_state_keyed("t3oxide.surface", || {
                            let mut state = SurfaceState::new();
                            state.attention_mode = start_attention;
                            state
                        });
                        let mut surface = std::mem::take(state.get_mut(ui));
                        app.populate_surface(&mut surface);
                        let view = app.view();
                        let modal = app.modal();
                        let App {
                            composer,
                            modal_editor,
                            ..
                        } = &mut *app;
                        let actions = slt_ui::draw(
                            ui,
                            &view,
                            composer,
                            modal_editor,
                            modal.as_ref(),
                            &mut surface,
                        );
                        app.observe_slt_layout(&surface);
                        if !actions.is_empty() {
                            // Controller actions change the next frame's widgets,
                            // focus or editor even when no worker/input wakes us.
                            ui.request_frame();
                        }
                        for action in actions {
                            if let Err(error) = app.surface_action(action, &mut surface) {
                                app.error(error);
                            }
                        }
                        if modal.is_none() && app.dialog.is_some() && !surface.attention_mode {
                            // The modal captures its return focus next frame. Keep
                            // subsequent typing in this conversation's next draft.
                            ui.focus_by_name("composer");
                        }
                        if let Some(text) = app.clipboard_request.take() {
                            ui.copy_to_clipboard(text);
                        }
                        *state.get_mut(ui) = surface;
                        if let Some(deadline) =
                            app.save_at.into_iter().chain(app.form_save_at).min()
                        {
                            ui.request_frame_after(
                                deadline.saturating_duration_since(Instant::now()),
                            );
                        }
                        if app.quit || app.pending_external_edit {
                            ui.quit();
                        }
                    })?;
                let sender = run.sender();
                // This bridge reads only worker messages. SLT alone reads the terminal.
                let bridge = thread::spawn(move || {
                    while !sender.is_closed() {
                        match rx.recv_timeout(Duration::from_millis(100)) {
                            Ok(event) => {
                                if sender.blocking_send(event).is_err() {
                                    break;
                                }
                            }
                            Err(crossbeam_channel::RecvTimeoutError::Timeout) => {}
                            Err(crossbeam_channel::RecvTimeoutError::Disconnected) => break,
                        }
                    }
                });
                let result = run.join().await.map_err(std::io::Error::other);
                let _ = bridge.join();
                result
            });
            let mut app = shared.lock().expect("SLT controller lock poisoned");
            if session.is_err() || app.quit || !app.pending_external_edit {
                break session;
            }
            app.pending_external_edit = false;
            if let Err(error) = app.external_editor() {
                app.error(error);
            }
        };
        let mut app = shared.lock().expect("SLT controller lock poisoned");
        let form = app.preserve_open_form();
        let draft = app.flush_draft();
        let ui = app.save_ui();
        if let Some(connection) = app.active.as_ref().and_then(|a| a.connection.as_ref()) {
            connection.close();
        }
        if let Some(connection) = &app.read_connection {
            connection.close();
        }
        outcome?;
        form?;
        draft?;
        ui
    }

    pub(super) fn search_projects(&mut self) {
        let mut choices = Vec::new();
        let mut projects = self.project_sources();
        projects.sort_by(|left, right| {
            left.project
                .cmp(&right.project)
                .then(left.environment.cmp(&right.environment))
                .then(left.project_id.cmp(&right.project_id))
        });
        for source in projects {
            let env = &self.environments[source.profile];
            choices.push(Choice::new(source.project, env.label.clone(), Intent::Workspace(workspace::WorkspaceAction::SearchScope(Some(
                json!({"profileId":env.id,"environmentId":env.environment_id,"projectId":source.project_id})
            )))));
        }
        choices.push(Choice::new(
            "All projects",
            "",
            Intent::Workspace(workspace::WorkspaceAction::SearchScope(None)),
        ));
        for (profile, env) in self.environments.iter().enumerate() {
            choices.push(Choice::new(
                env.label.clone(),
                "Environment",
                Intent::Workspace(workspace::WorkspaceAction::SearchEnvironment(profile)),
            ));
        }
        self.menu("Search · project", "", choices);
    }

    fn restore_tabs(&mut self) -> Result<()> {
        let saved = self.state.snapshot();
        for tab in array(&saved["sltUi"], "tabs") {
            if let Some(profile) = self.environments.iter().position(|env| {
                env.id == tab["profileId"] && env.environment_id == tab["environmentId"]
            }) && let Some(thread) = tab["threadId"].as_str().filter(|id| domain::valid_id(id))
                && !self
                    .tabs
                    .iter()
                    .any(|(p, id)| *p == profile && id == thread)
            {
                self.tabs.push((profile, thread.into()));
            }
        }
        Ok(())
    }

    fn source(&self, profile: usize, thread: &str) -> slt_ui::Source {
        let row = self
            .threads
            .iter()
            .find(|row| row.profile == profile && row.id == thread);
        let project_id = row
            .map(|row| row.project_id.clone())
            .or_else(|| {
                self.active
                    .as_ref()
                    .filter(|active| active.profile == profile && active.thread == thread)
                    .and_then(|active| active.projection.snapshot())
                    .filter(|snapshot| snapshot.thread_id() == thread)
                    .map(|snapshot| string(&snapshot.metadata, "projectId"))
                    .filter(|id| domain::valid_id(id))
            })
            .unwrap_or_default();
        let project = row
            .map(|row| row.project.clone())
            .or_else(|| {
                self.hosts[profile]
                    .projects
                    .iter()
                    .find(|project| project["id"] == project_id)
                    .map(|project| string(project, "title"))
            })
            .unwrap_or_else(|| project_id.clone());
        slt_ui::Source {
            profile,
            thread: thread.into(),
            environment: self.environments[profile].environment_id.clone(),
            environment_label: self.environments[profile].label.clone(),
            project_id,
            project,
        }
    }

    fn project_sources(&self) -> Vec<slt_ui::Source> {
        let mut projects = std::collections::BTreeMap::new();
        for (profile, host) in self.hosts.iter().enumerate() {
            for project in &host.projects {
                let source = slt_ui::Source {
                    profile,
                    thread: String::new(),
                    environment: self.environments[profile].environment_id.clone(),
                    environment_label: self.environments[profile].label.clone(),
                    project_id: string(project, "id"),
                    project: string(project, "title"),
                };
                projects.insert((profile, source.project_id.clone()), source);
            }
        }
        for thread in &self.threads {
            if !thread.project_id.is_empty() {
                projects
                    .entry((thread.profile, thread.project_id.clone()))
                    .or_insert_with(|| {
                        let mut source = self.source(thread.profile, &thread.id);
                        source.thread.clear();
                        source
                    });
            }
        }
        if let Some(active) = &self.active {
            let mut source = self.source(active.profile, &active.thread);
            if !source.project_id.is_empty() {
                source.thread.clear();
                projects
                    .entry((active.profile, source.project_id.clone()))
                    .or_insert(source);
            }
        }
        projects.into_values().collect()
    }

    pub(super) fn populate_surface(&mut self, surface: &mut SurfaceState) {
        surface.transcript_jump = None;
        if let Some((source, key, digest, inner)) = self.workspace.pending_jump.clone() {
            if source.check(self).is_ok()
                && self
                    .active
                    .as_ref()
                    .and_then(|active| active.projection.snapshot())
                    .is_some_and(|snapshot| {
                        snapshot
                            .items
                            .iter()
                            .any(|row| row.key == key && workspace::digest(&row.text) == digest)
                    })
            {
                surface.transcript_jump =
                    Some((source.display_key(&key), format!("sha256:{digest}"), inner));
            } else {
                self.workspace.pending_jump = None;
            }
        }
        surface.projects = self.project_sources();
        surface.thread_sources = self
            .threads
            .iter()
            .map(|row| (row.key(), self.source(row.profile, &row.id)))
            .collect();
        surface.tabs = self
            .tabs
            .iter()
            .map(|(profile, thread)| {
                let source = self.source(*profile, thread);
                let title = self
                    .threads
                    .iter()
                    .find(|row| row.profile == *profile && row.id == *thread)
                    .map(|row| row.title.clone())
                    .unwrap_or_else(|| thread.clone());
                slt_ui::Tab {
                    key: format!("{profile}\0{thread}"),
                    title,
                    source,
                }
            })
            .collect();
        surface.source = self
            .active
            .as_ref()
            .map(|a| self.source(a.profile, &a.thread));
        surface.active_key = self
            .active
            .as_ref()
            .map(|a| format!("{}\0{}", a.profile, a.thread))
            .unwrap_or_default();
        surface.questions = crate::attention::project_queue(&self.threads)
            .into_iter()
            .flat_map(|group| group.items)
            .map(|item| {
                let source = self.source(item.profile, &item.thread_id);
                let full = self
                    .active
                    .as_ref()
                    .filter(|a| a.profile == item.profile && a.thread == item.thread_id)
                    .and_then(|a| {
                        a.requests
                            .iter()
                            .find(|request| request["id"] == item.request_id)
                    })
                    .cloned();
                let request =
                    full.unwrap_or_else(|| json!({"id":item.request_id,"kind":item.kind}));
                let summary = request["questions"]
                    .as_array()
                    .and_then(|qs| qs.first())
                    .map(|q| string(q, "question"))
                    .filter(|text| !text.is_empty())
                    .unwrap_or_else(|| string(&request, "prompt"));
                let supporting_row = self
                    .active
                    .as_ref()
                    .filter(|a| a.profile == item.profile && a.thread == item.thread_id)
                    .and_then(|a| a.projection.snapshot())
                    .and_then(|s| {
                        s.items
                            .iter()
                            .rev()
                            .find(|row| row.kind == "assistant_message" && !row.streaming)
                    });
                let supporting_source_key = supporting_row
                    .map(|row| {
                        format!(
                            "{} / {} / {}{}",
                            source.environment,
                            row.source_thread_id,
                            row.item_id,
                            if row.truncated { " · excerpt" } else { "" }
                        )
                    })
                    .unwrap_or_default();
                let supporting_markdown = supporting_row
                    .map(|row| row.text.clone())
                    .unwrap_or_default();
                let supporting_revision = supporting_row
                    .map(|row| crate::reports::content_revision(&row.text))
                    .unwrap_or_default();
                slt_ui::AttentionItem {
                    key: format!("{}\0{}\0{}", item.profile, item.thread_id, item.request_id),
                    source,
                    title: item.title,
                    summary,
                    supporting_markdown,
                    supporting_source_key,
                    supporting_revision,
                    supporting_truncated: supporting_row.is_some_and(|row| row.truncated),
                    request,
                }
            })
            .collect();
        // A detailed live offer can arrive before its shell summary refresh.
        if let Some(active) = &self.active {
            if !active.navigating
                && active
                    .projection
                    .snapshot()
                    .is_some_and(|snapshot| snapshot.is_live())
            {
                surface.questions.retain(|question| {
                    question.source.profile != active.profile
                        || question.source.thread != active.thread
                        || active
                            .requests
                            .iter()
                            .any(|request| request["id"] == question.request["id"])
                });
            }
            for request in &active.requests {
                if !matches!(
                    request["kind"].as_str(),
                    Some("user_input_request" | "approval_request")
                ) {
                    continue;
                }
                let key = format!(
                    "{}\0{}\0{}",
                    active.profile,
                    active.thread,
                    string(request, "id")
                );
                if surface.questions.iter().any(|item| item.key == key) {
                    continue;
                }
                surface.questions.push(slt_ui::AttentionItem {
                    key,
                    source: self.source(active.profile, &active.thread),
                    title: string(&self.metadata(), "title"),
                    summary: string(request, "prompt"),
                    supporting_markdown: String::new(),
                    supporting_source_key: String::new(),
                    supporting_revision: String::new(),
                    supporting_truncated: false,
                    request: request.clone(),
                });
            }
        }
        if surface.loading_question.is_some()
            && let Some(active) = &self.active
            && !active.navigating
            && active
                .projection
                .snapshot()
                .is_some_and(|snapshot| snapshot.is_live())
        {
            // Upgrade only the explicitly selected source read. Later changes
            // retain the reviewed version until the person selects it again.
            let key = surface.loading_question.take();
            if let Some(question) = surface.questions.iter().find(|question| {
                Some(&question.key) == key.as_ref()
                    && question.source.profile == active.profile
                    && question.source.thread == active.thread
            }) {
                surface.retained_question = Some(question.clone());
            }
        }
        // Current native request offers expose no dependency edges or coverage.
        // Keep the synthesis role source-qualified without inventing independence.
        let qualified: Vec<_> = surface
            .questions
            .iter()
            .filter_map(|question| {
                let revision = string(&question.request, "revision");
                (!revision.is_empty()).then(|| crate::attention_rank::SourceId {
                    environment_id: question.source.environment.clone(),
                    thread_id: question.source.thread.clone(),
                    request_id: string(&question.request, "id"),
                    revision,
                })
            })
            .collect();
        surface.dependency_status = match crate::attention_rank::rank(
            &qualified,
            &[],
            crate::attention_rank::EvidenceCoverage::Unknown,
        ) {
            Ok(_) => "Dependencies · unknown".into(),
            Err(error) => format!("Dependencies unavailable · {error}"),
        };
    }

    pub(super) fn observe_slt_layout(&mut self, surface: &SurfaceState) {
        if let Some(focus) = surface.focus {
            self.focus = focus;
        }
        if let Some(layout) = &surface.transcript_layout {
            self.renderer.update_transcript_layout(layout);
            self.hits.transcript.width = layout.width;
            self.transcript_scroll = layout.top;
            self.follow = layout.top == layout.max;
            if surface.transcript_jump.is_none() {
                self.workspace.pending_jump = None;
            }
        }
    }

    fn open_pending_question(&mut self) -> Result<()> {
        let Some((profile, thread, request, revision)) = self.pending_attention.clone() else {
            return Ok(());
        };
        let Some(active) = self
            .active
            .as_ref()
            .filter(|a| a.profile == profile && a.thread == thread && !a.navigating)
        else {
            return Ok(());
        };
        let Some(snapshot) = active
            .projection
            .snapshot()
            .filter(|snapshot| snapshot.is_live())
        else {
            return Ok(());
        };
        let offer = active
            .requests
            .iter()
            .find(|offer| offer["id"] == request)
            .cloned();
        let _ = snapshot;
        self.pending_attention = None;
        let offer = offer.context("Question changed or was cancelled; saved drafts retained")?;
        ensure!(
            revision.is_empty() || offer["revision"] == revision,
            "Question changed; review its current offer"
        );
        self.review_request(offer)
    }

    pub(super) fn surface_action(
        &mut self,
        action: Action,
        surface: &mut SurfaceState,
    ) -> Result<()> {
        match action {
            Action::Highlight(index) => {
                self.focus = Focus::Navigator;
                self.selected = index.min(self.visible.len().saturating_sub(1));
            }
            Action::Open(index) => {
                let (index, _) = *self
                    .visible
                    .get(index)
                    .context("Conversation list changed")?;
                let row = &self.threads[index];
                self.open(row.profile, row.id.clone())?;
            }
            Action::OpenSource(source) => {
                ensure!(
                    self.environments
                        .get(source.profile)
                        .is_some_and(|env| env.environment_id == source.environment),
                    "Source environment changed"
                );
                surface.attention_mode = false;
                self.open(source.profile, source.thread)?;
            }
            Action::SelectTab(key) => {
                let (profile, thread) = self
                    .tabs
                    .iter()
                    .find(|(p, id)| format!("{p}\0{id}") == key)
                    .cloned()
                    .context("Tab changed")?;
                self.open(profile, thread)?;
            }
            Action::CloseTab(key) => {
                self.preserve_open_form()?;
                self.flush_draft()?;
                let position = self
                    .tabs
                    .iter()
                    .position(|(p, id)| format!("{p}\0{id}") == key)
                    .context("Tab changed")?;
                let removed = self.tabs.remove(position);
                if self
                    .active
                    .as_ref()
                    .is_some_and(|a| (a.profile, a.thread.clone()) == removed)
                {
                    if let Some((profile, thread)) = self
                        .tabs
                        .get(position.min(self.tabs.len().saturating_sub(1)))
                        .cloned()
                    {
                        self.open(profile, thread)?;
                    } else {
                        if let Some(active) = self.active.take()
                            && let Some(connection) = active.connection
                        {
                            connection.close();
                        }
                        self.composer = ui::editor("", &self.theme, "Write a message…");
                        self.generation = self.generation.saturating_add(1);
                    }
                }
                self.save_ui()?;
            }
            Action::Lane(lane) => {
                self.lane = lane;
                self.rebuild();
            }
            Action::Command(command) if command == "palette" => self.palette(),
            Action::Command(command) if command == "internal-tabs" => {
                self.save_ui()?;
            }
            Action::Command(command) => {
                if command == "quit" {
                    self.preserve_open_form()?;
                }
                if command == "reviewed" {
                    let settled = if self.focus == Focus::Navigator {
                        self.selected_thread().is_some_and(Thread::reviewed)
                    } else {
                        self.active
                            .as_ref()
                            .and_then(|active| {
                                self.threads.iter().find(|row| {
                                    row.profile == active.profile && row.id == active.thread
                                })
                            })
                            .is_some_and(Thread::reviewed)
                    };
                    self.command(if settled { "unsettle" } else { "settle" })?;
                } else {
                    self.command(&command)?;
                }
            }
            Action::Answer(item) => {
                let source = item.source;
                ensure!(
                    self.environments
                        .get(source.profile)
                        .is_some_and(|env| env.environment_id == source.environment),
                    "Source environment changed"
                );
                let request = string(&item.request, "id");
                ensure!(
                    domain::valid_id(&request),
                    "Question source identity unavailable"
                );
                if self
                    .active
                    .as_ref()
                    .is_some_and(|a| a.profile == source.profile && a.thread == source.thread)
                    && item.request["supported"] == true
                {
                    self.review_request(item.request)?;
                } else {
                    self.open(source.profile, source.thread.clone())?;
                    self.pending_attention = Some((
                        source.profile,
                        source.thread,
                        request,
                        string(&item.request, "revision"),
                    ));
                }
            }
            Action::InspectQuestion(item) => {
                ensure!(
                    self.environments
                        .get(item.source.profile)
                        .is_some_and(|env| env.environment_id == item.source.environment),
                    "Source environment changed"
                );
                surface.loading_question = Some(item.key);
                if self.active.as_ref().is_none_or(|active| {
                    active.profile != item.source.profile || active.thread != item.source.thread
                }) {
                    self.open(item.source.profile, item.source.thread)?;
                }
            }
            Action::Send => self.command("send")?,
            Action::Stop => {
                let preserved = self.preserve_open_form();
                self.command("stop")?;
                preserved.context("Stop requested; open form could not be saved")?;
            }
            Action::DraftChanged => {
                ensure!(self.active.is_some(), "Open a conversation before writing");
                self.focus = Focus::Composer;
                self.changed_draft();
            }
            Action::ModalEditChanged => {
                if self
                    .dialog
                    .as_ref()
                    .is_some_and(|d| matches!(d.kind, DialogKind::Palette))
                {
                    self.filter_palette();
                } else {
                    self.form_save_at = Some(Instant::now() + Duration::from_millis(200));
                }
            }
            Action::ModalChoice(index) => {
                let choice = self
                    .dialog
                    .as_ref()
                    .and_then(|d| d.choices.get(index))
                    .context("Action changed")?
                    .intent
                    .clone();
                self.dispatch(choice)?;
            }
            Action::ModalSubmit => self.accept_surface_dialog()?,
            Action::CloseModal => {
                self.preserve_open_form()?;
                self.dialog = None;
                self.cancel_modal_read();
            }
            Action::CopyId => {
                if let Some(active) = &self.active {
                    self.clipboard_request = Some(active.thread.clone());
                }
            }
        }
        self.dirty = true;
        Ok(())
    }

    fn accept_surface_dialog(&mut self) -> Result<()> {
        if let Some(dialog) = self.dialog.clone() {
            match dialog.kind {
                DialogKind::Text(target) => self.submit_text(target)?,
                DialogKind::Menu | DialogKind::Palette => {
                    if let Some(choice) = dialog.choices.get(dialog.selected) {
                        self.dispatch(choice.intent.clone())?;
                    }
                }
                DialogKind::Detail => {}
            }
        }
        Ok(())
    }
}
