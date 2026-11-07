//! Explicit context capture and recall over the owned fael store (A4, B3).
use super::context_worker::{
    self as worker, Event, Output, Read, ReadRequest, Request, ViewToken, Worker, WriteRequest,
};
use super::*;
use crate::context_store::{self as store, Kind, NativeKind, NativeRecord, Query, Scope, Source};

#[cfg(test)]
pub(crate) struct IoBarrier {
    pub entered: Sender<()>,
    pub release: Receiver<()>,
}

impl Drop for App {
    fn drop(&mut self) {
        if let Err(error) = self.drain_context() {
            eprintln!("Context shutdown failed: {error:#}");
        }
    }
}
#[cfg(test)]
impl IoBarrier {
    pub(super) fn wait(self) {
        let _ = self.entered.send(());
        let _ = self.release.recv();
    }
}

#[derive(Clone, Debug)]
pub(crate) enum Action {
    Record(store::RecordInput),
    Find(Scope),
    Revisit(store::PackRow),
    Recall(Query),
    Row(store::PackRow),
    Source(store::PackRow),
    Close(store::PackRow),
    Retry,
}

#[derive(Default)]
pub(crate) struct ContextIo {
    worker: Option<Worker>,
    view: u64,
    serial: u64,
    current: Option<ViewToken>,
    active_read: Option<u64>,
    desired: Option<ReadRequest>,
    active_write: bool,
    retry: Option<Box<WriteRequest>>,
    failed: Option<Box<WriteRequest>>,
    pub draining: bool,
    shutdown_error: Option<String>,
    revisit_version: u64,
}

pub(super) struct Selection {
    pub source: Option<crate::slt_ui::Source>,
    pub content: Option<(String, String)>,
}

impl App {
    fn context_worker(&mut self) -> &Worker {
        self.context_io.worker.get_or_insert_with(|| {
            let mut path = self.state.path().as_os_str().to_os_string();
            path.push(".context");
            Worker::start(
                path.into(),
                self.tx.clone(),
                #[cfg(test)]
                self.context_test_barrier.take(),
            )
        })
    }

    pub(super) fn invalidate_context_view(&mut self) {
        self.context_io.view = self.context_io.view.wrapping_add(1);
        self.context_io.current = None;
        self.context_io.desired = None;
    }

    fn context_token_current(&self, token: &ViewToken) -> bool {
        !self.context_io.draining
            && token.generation == self.generation
            && token.view == self.context_io.view
            && self.context_io.current.as_ref() == Some(token)
    }

    fn submit_context_read(&mut self, operation: Read) -> Result<()> {
        ensure!(!self.context_io.draining, "Context is shutting down");
        self.preserve_open_form()?;
        self.invalidate_context_view();
        // Keep the existing modal Escape route active while I/O is pending.
        self.dialog = Some(Dialog {
            title: "Context".into(),
            description: "Reading…".into(),
            detail: String::new(),
            choices: Vec::new(),
            selected: 0,
            scroll: 0,
            kind: DialogKind::Menu,
            footer: "Esc close".into(),
        });
        self.context_io.serial = self.context_io.serial.wrapping_add(1);
        let token = ViewToken {
            serial: self.context_io.serial,
            generation: self.generation,
            view: self.context_io.view,
            scope: operation.scope(),
        };
        self.context_io.current = Some(token.clone());
        self.context_io.desired = Some(ReadRequest { token, operation });
        self.notice = "Reading context…".into();
        self.schedule_context_read();
        self.dirty = true;
        Ok(())
    }

    fn schedule_context_read(&mut self) {
        if self.context_io.draining || self.context_io.active_read.is_some() {
            return;
        }
        let Some(request) = self.context_io.desired.take() else {
            return;
        };
        if !self.context_token_current(&request.token) {
            return;
        }
        let serial = request.token.serial;
        match self.context_worker().try_send(Request::Read(request)) {
            Ok(()) => self.context_io.active_read = Some(serial),
            Err(error) => {
                let message = if error.is_full() {
                    "Context busy"
                } else {
                    "Context worker disconnected"
                };
                let Request::Read(request) = *error.into_inner() else {
                    unreachable!()
                };
                self.context_io.desired = Some(request);
                self.notice = message.into();
            }
        }
    }

    pub(super) fn try_context_write(
        &mut self,
        request: WriteRequest,
    ) -> std::result::Result<(), (String, Box<WriteRequest>)> {
        if self.context_io.draining || self.context_io.active_write {
            return Err(("Context busy".into(), Box::new(request)));
        }
        if let Err(error) = request.operation.validate_size() {
            return Err((error.to_string(), Box::new(request)));
        }
        let automatic = matches!(&request.operation, worker::Write::ObserveLoaded(_));
        match self.context_worker().try_send(Request::Write(request)) {
            Ok(()) => {
                self.context_io.active_write = true;
                self.context_capture_inflight = automatic;
                Ok(())
            }
            Err(error) => {
                let message = if error.is_full() {
                    "Context busy"
                } else {
                    "Context worker disconnected"
                };
                let Request::Write(request) = *error.into_inner() else {
                    unreachable!()
                };
                Err((message.into(), Box::new(request)))
            }
        }
    }

    fn submit_context_write(&mut self, operation: worker::Write) -> Result<()> {
        ensure!(!self.context_io.draining, "Context is shutting down");
        ensure!(
            self.context_io.retry.is_none() && self.context_io.failed.is_none(),
            "Original context write retained · /context-retry"
        );
        operation.validate_size()?;
        let request = WriteRequest::new(self.generation, operation);
        if let Err((error, request)) = self.try_context_write(request) {
            self.context_io.retry = Some(request);
            bail!("{error} · original context write retained · /context-retry");
        }
        self.notice = "Saving context…".into();
        self.dirty = true;
        Ok(())
    }

    pub(super) fn context_writes_blocked(&self) -> bool {
        self.context_io.draining
            || self.context_io.active_write
            || self.context_io.retry.is_some()
            || self.context_io.failed.is_some()
    }

    #[cfg(test)]
    pub(super) fn context_has_work(&self) -> bool {
        self.context_io.active_write
            || self.context_io.active_read.is_some()
            || self.context_io.desired.is_some()
    }

    fn retry_context_write(&mut self) -> Result<()> {
        ensure!(
            !self.context_io.draining && !self.context_io.active_write,
            "Context busy · original context write retained"
        );
        let failed = self.context_io.failed.is_some();
        let request = if failed {
            self.context_io.failed.take()
        } else {
            self.context_io.retry.take()
        }
        .context("No retained context write")?;
        if let Err((error, request)) = self.try_context_write(*request) {
            if failed {
                self.context_io.failed = Some(request)
            } else {
                self.context_io.retry = Some(request)
            }
            bail!("{error} · original context write retained · /context-retry");
        }
        self.notice = "Saving context…".into();
        Ok(())
    }

    pub(super) fn context_revisit_edited(
        &mut self,
        row: &store::PackRow,
        text: &str,
    ) -> Result<()> {
        let draft = format!("context-revisit-{}", row.id);
        if self.state.draft(&row.source.environment_id, &draft) != text {
            self.context_io.revisit_version = self.context_io.revisit_version.wrapping_add(1);
        }
        self.state
            .set_draft(&row.source.environment_id, &draft, text)
    }

    pub(super) fn handle_context_event(&mut self, event: Event) -> Result<()> {
        match event {
            Event::Stopped => {
                if let Some(worker) = self.context_io.worker.take() {
                    worker.join()?;
                }
                if self.context_io.draining {
                    if let Some(error) = self.context_io.shutdown_error.clone() {
                        self.context_io.draining = false;
                        self.quit = false;
                        self.error(error);
                    } else {
                        self.quit = true;
                    }
                }
            }
            Event::Finished(finished) => {
                let worker::Finished { request, result } = *finished;
                match request {
                    Request::Read(request) => {
                        if self.context_io.active_read == Some(request.token.serial) {
                            self.context_io.active_read = None;
                        }
                        if self.context_token_current(&request.token) {
                            match result {
                                Ok(Output::Pack(pack)) => {
                                    let Read::Recall { query, .. } = request.operation else {
                                        unreachable!()
                                    };
                                    self.context_pack(query, *pack);
                                }
                                Ok(Output::Source(captured)) => self.detail(
                                    "Captured source",
                                    format!(
                                        "```json\n{}\n```\nSHA-256: {}\n\n{}",
                                        serde_json::to_string_pretty(&captured.source)?,
                                        captured.sha256,
                                        String::from_utf8_lossy(&captured.bytes)
                                    ),
                                ),
                                Err(error) => {
                                    if let Some(dialog) = &mut self.dialog {
                                        dialog.description = error.clone();
                                    }
                                    self.error(error);
                                }
                                _ => bail!("Unrecognized context read completion"),
                            }
                        }
                    }
                    Request::Write(request) => {
                        self.context_io.active_write = false;
                        self.context_capture_inflight = false;
                        match result {
                            Ok(Output::Observed(batch)) => {
                                if self.context_capture_known.len() > 4096 {
                                    self.context_capture_known.clear();
                                }
                                for captured in batch.recorded.iter().chain(&batch.unchanged) {
                                    self.context_capture_known
                                        .insert(content::capture_key(&captured.source)?);
                                }
                                self.context_capture_status = batch
                                    .degradation
                                    .into_iter()
                                    .chain(batch.omitted.into_iter().map(|entry| entry.reason))
                                    .collect::<Vec<_>>()
                                    .join(" · ");
                            }
                            Ok(Output::Saved(id)) => {
                                if request.generation == self.generation
                                    && request.operation.source().is_some_and(|source| {
                                        self.context_source().is_ok_and(|current| {
                                            current.environment_id == source.environment_id
                                                && current.project_id == source.project_id
                                                && current.thread_id == source.thread_id
                                        })
                                    })
                                {
                                    match &request.operation {
                                        worker::Write::Revisit {
                                            row,
                                            condition,
                                            draft_version,
                                        } => {
                                            if self.context_io.revisit_version == *draft_version
                                                && self.state.draft(
                                                    &row.source.environment_id,
                                                    &format!("context-revisit-{}", row.id),
                                                ) == *condition
                                            {
                                                self.notice = "Revisit saved".into();
                                            }
                                        }
                                        worker::Write::Close(_) => {
                                            self.notice = "Local record closed".into()
                                        }
                                        _ => self.notice = format!("Context saved · {id}"),
                                    }
                                }
                            }
                            Err(error) => {
                                let source = request
                                    .operation
                                    .source()
                                    .map(|source| {
                                        format!(
                                            "{} / {} · ",
                                            source.environment_id, source.thread_id
                                        )
                                    })
                                    .unwrap_or_default();
                                let error = format!(
                                    "{source}{error} · original context write retained · /context-retry"
                                );
                                self.context_capture_status = error.clone();
                                if self.context_io.draining {
                                    self.context_io.shutdown_error = Some(error.clone());
                                }
                                self.error(error);
                                self.context_io.failed = Some(Box::new(request));
                            }
                            _ => bail!("Unrecognized context write completion"),
                        }
                    }
                }
                if !self.context_io.draining {
                    self.schedule_context_read();
                    self.schedule_context_capture()?;
                }
            }
        }
        self.dirty = true;
        Ok(())
    }

    pub(super) fn begin_context_shutdown(&mut self) {
        if !self.context_io.draining {
            self.context_io.shutdown_error = (self.context_io.failed.is_some()
                || self.context_io.retry.is_some())
            .then(|| "Original context write retained · /context-retry".to_owned());
        }
        self.invalidate_context_view();
        self.context_io.draining = true;
        if let Some(worker) = &mut self.context_io.worker {
            worker.close();
            self.quit = false;
            self.notice = "Saving context…".into();
        } else if let Some(error) = self.context_io.shutdown_error.clone() {
            self.context_io.draining = false;
            self.quit = false;
            self.error(error);
        } else {
            self.quit = true;
        }
    }

    /// Keep receiving until the worker releases Store and delivers its final marker.
    /// In particular, do not join a worker blocked sending into the bounded event queue.
    pub(super) fn drain_context(&mut self) -> Result<()> {
        if self.context_io.worker.is_none() {
            ensure!(
                self.context_io.failed.is_none() && self.context_io.retry.is_none(),
                "Original context write retained · /context-retry"
            );
            return Ok(());
        }
        self.begin_context_shutdown();
        let mut failure = None;
        while self.context_io.worker.is_some() {
            let event = self
                .rx
                .recv()
                .context("Context completion channel disconnected during shutdown")?;
            let result = match event {
                Background::Context(event) => self.handle_context_event(event),
                Background::Saved {
                    key,
                    version,
                    result,
                } => self.handle(Background::Saved {
                    key,
                    version,
                    result,
                }),
                // Native workers already commit their durable receipts. Teardown
                // cannot reopen views or start reads from queued native packets.
                _ => Ok(()),
            };
            if let Err(error) = result {
                failure = Some(error);
            }
        }
        if let Some(error) = failure {
            return Err(error);
        }
        if let Some(error) = self.context_io.shutdown_error.take() {
            bail!("{error}");
        }
        Ok(())
    }

    fn context_source(&self) -> Result<Source> {
        let active = self.active.as_ref().context("Open a conversation first")?;
        let snapshot = active.projection.snapshot().context("Source unavailable")?;
        let project_id = string(&snapshot.metadata, "projectId");
        let project_id = if project_id.is_empty() {
            self.threads
                .iter()
                .find(|thread| thread.profile == active.profile && thread.id == active.thread)
                .map(|thread| thread.project_id.clone())
                .unwrap_or_default()
        } else {
            project_id
        };
        ensure!(
            !project_id.is_empty(),
            "Native project identity unavailable"
        );
        Ok(Source {
            environment_id: self.environments[active.profile].environment_id.clone(),
            project_id,
            thread_id: active.thread.clone(),
            item_id: None,
            request_id: None,
            revision: format!("projection-{}-{}", active.generation, snapshot.revision),
            observed: json!({"connectionGeneration":active.generation,"projectionRevision":snapshot.revision,
                "live":snapshot.is_live(),"historical":snapshot.metadata["historical"],"state":snapshot.metadata["state"],
                "limited":snapshot.metadata["limited"],"hasMore":snapshot.metadata["hasMore"],"capturedItemOnly":true}),
        })
    }

    fn context_capture(&self, kind: Kind) -> Result<store::RecordInput> {
        let row = self
            .active
            .as_ref()
            .and_then(|active| active.projection.snapshot())
            .and_then(|snapshot| {
                snapshot
                    .items
                    .iter()
                    .rev()
                    .find(|row| row.kind == "assistant_message" && !row.streaming)
            })
            .context("No completed assistant message loaded")?;
        ensure!(
            !row.truncated,
            "Source is an excerpt; retain the complete native message before capture"
        );
        let mut source = self.context_source()?;
        source.thread_id = row.source_thread_id.clone();
        if let Some(original) = self.threads.iter().find(|thread| {
            thread.profile
                == self
                    .active
                    .as_ref()
                    .map_or(usize::MAX, |active| active.profile)
                && thread.id == row.source_thread_id
        }) {
            source.project_id = original.project_id.clone();
        } else {
            ensure!(
                self.active
                    .as_ref()
                    .is_some_and(|active| active.thread == row.source_thread_id),
                "Original source project unavailable; capture retained in native T3"
            );
        }
        source.item_id = Some(row.item_id.clone());
        source.revision = crate::reports::content_revision(&serde_json::to_string(row)?);
        let full_source = serde_json::to_vec(row)?;
        // The compact row is presentation; the exact captured payload is separate.
        let summary: String = row.text.chars().take(1200).collect();
        Ok(store::RecordInput {
            source,
            kind,
            text: summary,
            full_source,
            supersedes: None,
            revisit: None,
        })
    }

    pub(super) fn context_capture_content(
        &self,
        key: &str,
        revision: &str,
        selected: &crate::slt_ui::Source,
    ) -> Result<store::RecordInput> {
        ensure!(
            self.environments
                .get(selected.profile)
                .is_some_and(|environment| { environment.environment_id == selected.environment })
                && domain::valid_id(&selected.project_id),
            "Selected source environment or project unavailable"
        );
        let item = self
            .loaded_contents
            .iter()
            .find(|item| item.key == key && item.revision == revision && item.source == *selected)
            .context("Selected source changed; select its retained revision again")?;
        ensure!(
            !item.truncated && !item.full_source.is_empty(),
            "Source is an excerpt or its original bytes are unavailable"
        );
        let row: crate::projection::Row = serde_json::from_slice(&item.full_source)?;
        let expected_key =
            serde_json::to_string(&[&selected.environment, &selected.thread, &row.item_id])?;
        ensure!(
            expected_key == key
                && row.source_thread_id == selected.thread
                && row.kind == "assistant_message"
                && !row.streaming
                && !row.truncated
                && row.text == item.markdown
                && super::content::row_revision(&row)? == revision,
            "Selected source identity, revision or complete bytes changed"
        );
        Ok(store::RecordInput {
            source: Source {
                environment_id: selected.environment.clone(),
                project_id: selected.project_id.clone(),
                thread_id: selected.thread.clone(),
                item_id: Some(row.item_id),
                request_id: None,
                revision: revision.into(),
                observed: json!({"retainedLoadedRow":true,"capturedItemOnly":true,
                    "streaming":false,"truncated":false,"nativeFreshness":"unknown"}),
            },
            kind: Kind::Evidence,
            text: row.text.chars().take(1200).collect(),
            full_source: item.full_source.to_vec(),
            supersedes: None,
            revisit: None,
        })
    }

    pub(super) fn context_capture_menu(&mut self, captured: store::RecordInput) -> Result<()> {
        ensure!(
            self.context_io.retry.is_none() && self.context_io.failed.is_none(),
            "Original context write retained · /context-retry"
        );
        let detail = format!(
            "{} / {} / {} @ {}",
            captured.source.environment_id,
            captured.source.project_id,
            captured.source.thread_id,
            captured.source.revision
        );
        let choices = [
            ("Decision", Kind::Decision),
            ("Evidence", Kind::Evidence),
            ("Journal", Kind::Journal),
            ("Local open loop", Kind::OpenLoop),
        ]
        .into_iter()
        .map(|(label, kind)| {
            Choice::new(
                label,
                "",
                Intent::Context(Action::Record(store::RecordInput {
                    kind,
                    ..captured.clone()
                })),
            )
        })
        .collect();
        self.menu("Capture source", &detail, choices);
        Ok(())
    }

    pub(super) fn context_command(&mut self, name: &str, args: &str) -> Result<bool> {
        let selected = if matches!(
            name,
            "context" | "context-save" | "context-find" | "context-history" | "resume"
        ) {
            self.context_selection
                .as_ref()
                .map(|selection| {
                    selection
                        .source
                        .clone()
                        .context("Select a source before opening its context")
                })
                .transpose()?
        } else {
            None
        };
        let source = selected
            .as_ref()
            .map(|source| self.retained_context_scope(source))
            .transpose()?;
        self.context_command_scoped(name, args, source)
    }

    pub(super) fn context_selected_source(
        &mut self,
        selected: &crate::slt_ui::Source,
        resume: bool,
    ) -> Result<()> {
        let source = self.retained_context_scope(selected)?;
        self.context_command_scoped(if resume { "resume" } else { "context" }, "", Some(source))?;
        Ok(())
    }

    fn retained_context_scope(&self, selected: &crate::slt_ui::Source) -> Result<Source> {
        ensure!(
            self.environments
                .get(selected.profile)
                .is_some_and(|environment| environment.environment_id == selected.environment)
                && domain::valid_id(&selected.project_id)
                && domain::valid_id(&selected.thread),
            "Selected context source unavailable"
        );
        Ok(Source {
            environment_id: selected.environment.clone(),
            project_id: selected.project_id.clone(),
            thread_id: selected.thread.clone(),
            item_id: None,
            request_id: None,
            revision: "retained-source-scope".into(),
            observed: json!({"retainedScope":true,"nativeFreshness":"unknown"}),
        })
    }

    fn context_command_scoped(
        &mut self,
        name: &str,
        args: &str,
        selected: Option<Source>,
    ) -> Result<bool> {
        if !matches!(
            name,
            "context"
                | "context-save"
                | "context-find"
                | "context-history"
                | "context-retry"
                | "resume"
        ) {
            return Ok(false);
        }
        if name == "context-retry" {
            self.retry_context_write()?;
            return Ok(true);
        }
        let source = if let Some(source) = selected {
            source
        } else {
            self.context_source()?
        };
        let scope = Scope {
            environment_id: source.environment_id.clone(),
            project_id: Some(source.project_id.clone()),
            thread_id: Some(source.thread_id.clone()),
        };
        match name {
            "context-save" => {
                let captured = if let Some(selection) = &self.context_selection {
                    let selected = selection
                        .source
                        .as_ref()
                        .context("Selected source unavailable")?;
                    let (key, revision) = selection
                        .content
                        .as_ref()
                        .context("Select retained Library content before capture")?;
                    self.context_capture_content(key, revision, selected)?
                } else {
                    self.context_capture(Kind::Evidence)?
                };
                self.context_capture_menu(captured)?;
            }
            "context-find" => self.text_dialog(
                "Find context",
                "",
                InputTarget::ContextQuery(scope),
                String::new(),
            ),
            "resume" => {
                let mut query = Query::new(scope);
                query.text = args.into();
                let mut records = Vec::new();
                let active_matches = self.active.as_ref().is_some_and(|active| {
                    self.environments[active.profile].environment_id == source.environment_id
                        && active.thread == source.thread_id
                });
                let text = if active_matches {
                    self.text()
                } else {
                    self.state.draft(&source.environment_id, &source.thread_id)
                };
                if !text.is_empty() {
                    let mut draft_source = source.clone();
                    draft_source.revision = crate::reports::content_revision(&text);
                    records.push(NativeRecord {
                        source: draft_source,
                        kind: NativeKind::Draft,
                        identity: format!("draft:{}", source.thread_id),
                        status: "unsent".into(),
                        summary: text,
                    });
                }
                for stop in [false, true] {
                    if let Some(record) = self.state.pending(&source.environment_id, stop)
                        && record["threadId"] == source.thread_id
                    {
                        let mut pending_source = source.clone();
                        pending_source.request_id = record["requestId"].as_str().map(str::to_owned);
                        if let Some(revision) = record["revision"]
                            .as_str()
                            .filter(|value| !value.is_empty())
                        {
                            pending_source.revision = revision.to_owned();
                        }
                        pending_source.observed["operationPayloadSha256"] = json!(
                            crate::reports::content_revision(&serde_json::to_string(&record)?)
                        );
                        pending_source.observed["operationRevisionAvailable"] = json!(
                            record["revision"]
                                .as_str()
                                .is_some_and(|value| !value.is_empty())
                        );
                        records.push(NativeRecord {
                            source: pending_source,
                            kind: NativeKind::PendingOperation,
                            identity: string(&record, "id"),
                            status: "unresolved".into(),
                            summary: format!(
                                "{} · original operation retained",
                                string(&record, "op")
                            ),
                        });
                    }
                }
                if let Some(active) = self.active.as_ref().filter(|_| active_matches) {
                    for request in &active.requests {
                        let mut request_source = source.clone();
                        request_source.request_id = Some(string(request, "id"));
                        request_source.revision = string(request, "revision");
                        if request_source.revision.is_empty() {
                            continue;
                        }
                        records.push(NativeRecord {
                            source: request_source,
                            kind: NativeKind::HumanQuestion,
                            identity: string(request, "id"),
                            status: "native pending".into(),
                            summary: string(request, "prompt"),
                        });
                    }
                }
                self.submit_context_read(Read::Recall {
                    query,
                    native: records,
                })?;
            }
            _ => self.context_recall(scope, args, name == "context-history", 0)?,
        }
        Ok(true)
    }

    pub(super) fn context_recall(
        &mut self,
        scope: Scope,
        text: &str,
        history: bool,
        offset: usize,
    ) -> Result<()> {
        let mut query = Query::new(scope);
        query.text = text.into();
        query.history = history;
        query.offset = offset;
        self.submit_context_read(Read::Recall {
            query,
            native: Vec::new(),
        })
    }

    fn context_pack(&mut self, query: Query, pack: store::Pack) {
        let mut choices: Vec<_> = pack
            .rows
            .iter()
            .map(|row| {
                Choice::new(
                    row.text
                        .lines()
                        .find(|line| !line.trim().is_empty())
                        .unwrap_or(&row.id),
                    format!(
                        "{:?} · {} @ {}",
                        row.kind, row.source.thread_id, row.source.revision
                    ),
                    Intent::Context(Action::Row(row.clone())),
                )
            })
            .collect();
        if self.context_io.retry.is_some() || self.context_io.failed.is_some() {
            choices.push(Choice::new(
                "Retry context write",
                "",
                Intent::Context(Action::Retry),
            ));
        }
        choices.push(Choice::new(
            "Find",
            "",
            Intent::Context(Action::Find(query.scope.clone())),
        ));
        if query.offset > 0 {
            let mut previous = query.clone();
            previous.offset = previous.offset.saturating_sub(query.limit);
            choices.push(Choice::new(
                "Previous",
                "",
                Intent::Context(Action::Recall(previous)),
            ));
        }
        if pack.offset + pack.shown < pack.eligible {
            let mut next = query.clone();
            next.offset += pack.shown.max(1);
            choices.push(Choice::new(
                "More",
                "",
                Intent::Context(Action::Recall(next)),
            ));
        }
        if !query.history {
            let mut history = query;
            history.history = true;
            history.offset = 0;
            choices.push(Choice::new(
                "Include history",
                "",
                Intent::Context(Action::Recall(history)),
            ));
        }
        self.menu(
            "Context records",
            format!(
                "{} / {}{}{}{}",
                pack.shown,
                pack.eligible,
                if pack.omitted > 0 {
                    format!(" · {} omitted", pack.omitted)
                } else {
                    String::new()
                },
                if pack.native_eligible > 0 {
                    format!(" · native {} / {}", pack.native_shown, pack.native_eligible)
                } else {
                    String::new()
                },
                if pack.complete { "" } else { " · degraded" }
            ),
            choices,
        );
        if let Some(dialog) = &mut self.dialog {
            dialog.detail = pack.markdown;
            for record in &pack.native_records {
                dialog.detail.push_str(&format!(
                    "\n- {:?}: {} · {}\n\n{}\n\n```json\n{}\n```\n",
                    record.kind,
                    record.identity,
                    record.status,
                    record.summary,
                    serde_json::to_string_pretty(&record.source)
                        .expect("source provenance serializes")
                ));
            }
            for warning in &pack.degradation {
                dialog.detail.push_str(&format!("\n{warning}\n"));
            }
            dialog.detail.push_str(&format!("\nPack: {}\n", pack.id));
        }
    }

    pub(super) fn context_revisit(&mut self, row: &store::PackRow, condition: &str) -> Result<()> {
        ensure!(
            !condition.trim().is_empty(),
            "A revisit date or condition is required"
        );
        let draft_version = self.context_io.revisit_version;
        self.submit_context_write(worker::Write::Revisit {
            row: row.clone(),
            condition: condition.into(),
            draft_version,
        })
    }

    pub(super) fn context_action(&mut self, action: Action) -> Result<()> {
        match action {
            Action::Find(scope) => self.text_dialog(
                "Find context",
                "",
                InputTarget::ContextQuery(scope),
                String::new(),
            ),
            Action::Revisit(row) => {
                let draft = self.state.draft(
                    &row.source.environment_id,
                    &format!("context-revisit-{}", row.id),
                );
                self.text_dialog(
                    "Revisit condition",
                    "",
                    InputTarget::ContextRevisit(Box::new(row)),
                    draft,
                );
            }
            Action::Record(input) => {
                self.submit_context_write(worker::Write::Record(input))?;
            }
            Action::Recall(query) => {
                self.submit_context_read(Read::Recall {
                    query,
                    native: Vec::new(),
                })?;
            }
            Action::Row(row) => {
                let mut choices = vec![Choice::new(
                    "Original source",
                    "",
                    Intent::Context(Action::Source(row.clone())),
                )];
                if !row.closed {
                    choices.push(Choice::new(
                        "Close local record",
                        "",
                        Intent::Context(Action::Close(row.clone())),
                    ));
                }
                if !row.closed && !row.superseded {
                    choices.push(Choice::new(
                        "Revisit",
                        "",
                        Intent::Context(Action::Revisit(row.clone())),
                    ));
                    if let Ok(mut replacement) = self.context_capture(row.kind)
                        && replacement.source.environment_id == row.source.environment_id
                        && replacement.source.project_id == row.source.project_id
                        && replacement.source.thread_id == row.source.thread_id
                        && replacement.source.revision != row.source.revision
                    {
                        replacement.supersedes = Some(row.id.clone());
                        choices.push(Choice::new(
                            "Supersede with current source",
                            "",
                            Intent::Context(Action::Record(replacement)),
                        ));
                    }
                }
                self.menu(
                    "Context record",
                    format!("{} @ {}", row.source.thread_id, row.source.revision),
                    choices,
                );
                if let Some(dialog) = &mut self.dialog {
                    dialog.detail = row.text;
                }
            }
            Action::Source(row) => {
                self.submit_context_read(Read::Source(row))?;
            }
            Action::Close(row) => {
                self.submit_context_write(worker::Write::Close(row))?;
            }
            Action::Retry => self.retry_context_write()?,
        }
        Ok(())
    }
}
