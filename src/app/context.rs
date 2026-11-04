//! Explicit context capture and recall over the owned fael store (A4, B3).
use super::*;
use crate::context_store::{self as store, Kind, NativeKind, NativeRecord, Query, Scope, Source};

#[derive(Clone, Debug)]
pub(crate) enum Action {
    Record(store::RecordInput),
    Find(Scope),
    Revisit(store::PackRow),
    Recall(Query),
    Row(store::PackRow),
    Source(String),
    Close(store::PackRow),
}

impl App {
    fn context_store(&self) -> Result<store::Store> {
        let mut path = self.state.path().as_os_str().to_os_string();
        path.push(".context");
        store::Store::open(std::path::Path::new(&path))
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
            .context("No completed native agent message in the loaded page")?;
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

    pub(super) fn context_command(&mut self, name: &str, args: &str) -> Result<bool> {
        if !matches!(
            name,
            "context" | "context-save" | "context-find" | "context-history" | "resume"
        ) {
            return Ok(false);
        }
        let source = self.context_source()?;
        let scope = Scope {
            environment_id: source.environment_id.clone(),
            project_id: Some(source.project_id.clone()),
            thread_id: Some(source.thread_id.clone()),
        };
        match name {
            "context-save" => {
                let captured = self.context_capture(Kind::Evidence)?;
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
                self.menu("Capture source", "", choices);
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
                let text = self.text();
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
                if let Some(active) = &self.active {
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
                let pack = self.context_store()?.resume(&query, &records)?;
                self.context_pack(query, pack);
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
        let pack = self.context_store()?.recall(&query)?;
        self.context_pack(query, pack);
        Ok(())
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
        self.context_store()?
            .revisit(&row.id, &row.source, condition)?;
        self.notice = "Revisit saved".into();
        Ok(())
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
                let id = self.context_store()?.record(&input)?;
                self.notice = format!("Context saved · {id}");
            }
            Action::Recall(query) => {
                let pack = self.context_store()?.recall(&query)?;
                self.context_pack(query, pack);
            }
            Action::Row(row) => {
                let mut choices = vec![Choice::new(
                    "Original source",
                    "",
                    Intent::Context(Action::Source(row.id.clone())),
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
            Action::Source(id) => {
                let captured = self.context_store()?.source(&id)?;
                self.detail(
                    "Captured source",
                    format!(
                        "```json\n{}\n```\nSHA-256: {}\n\n{}",
                        serde_json::to_string_pretty(&captured.source)?,
                        captured.sha256,
                        String::from_utf8_lossy(&captured.bytes)
                    ),
                );
            }
            Action::Close(row) => {
                self.context_store()?.close(
                    &row.id,
                    &row.source,
                    "Explicitly closed in local context",
                )?;
                self.notice = "Local record closed".into();
            }
        }
        Ok(())
    }
}
