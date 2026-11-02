//! Local workspace tools. These store presentation and reading coordinates only;
//! native T3 remains the owner of projects, conversations, requests and work.
use super::*;
use base64::{Engine, engine::general_purpose::STANDARD};
use sha2::{Digest, Sha256};

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct ViewSource {
    profile: usize,
    environment: String,
    thread: String,
    generation: u64,
}
impl ViewSource {
    fn active(app: &App) -> Result<Self> {
        let active = app.active.as_ref().context("Open a conversation first")?;
        Ok(Self {
            profile: active.profile,
            environment: app.environments[active.profile].environment_id.clone(),
            thread: active.thread.clone(),
            generation: active.generation,
        })
    }
    fn check(&self, app: &App) -> Result<()> {
        ensure!(
            Self::active(app)? == *self,
            "Conversation changed; reopen this action on its original source"
        );
        Ok(())
    }
    fn display_key(&self, row: &str) -> String {
        format!(
            "{}\0{}\0{}\0{row}",
            self.profile, self.thread, self.generation
        )
    }
}

#[derive(Clone, Debug)]
pub(crate) enum WorkspaceAction {
    Scope(Option<Value>),
    MinimumPriority(u64),
    Rank {
        source: Value,
        revision: u64,
        value: u64,
        project: bool,
    },
    UndoRanks,
    Row {
        source: ViewSource,
        key: String,
        digest: String,
    },
    Jump {
        source: ViewSource,
        key: String,
        digest: String,
    },
    Copy {
        label: String,
        text: String,
    },
    Codes {
        source: String,
        text: String,
    },
    Preset {
        source: ViewSource,
        name: String,
        selection: Value,
    },
    ApplyPreset {
        source: ViewSource,
        selection: Value,
        scope: ModelScope,
    },
    RemovePreset {
        environment: String,
        name: String,
        selection: Value,
    },
    SavedLaunch {
        key: String,
        draft: Value,
    },
    RustLaunch {
        key: String,
        draft: Value,
    },
    RestoreRustLaunch {
        key: String,
        draft: Value,
    },
    DiscardRustLaunch {
        key: String,
        draft: Value,
    },
    SavedBatch {
        key: String,
        text: String,
    },
    ResetLaunchDefaults,
    BatchReceipt(String),
    ResumeBatch(String),
}

#[derive(Clone, Debug)]
struct ReviewContext {
    source: Value,
    digest: String,
    detail_digest: String,
}
#[derive(Clone, Debug, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct BatchBinding {
    pub environment: String,
    pub project: String,
    pub source: String,
    pub receipt: String,
    pub index: usize,
    pub before: String,
    pub consumed: String,
}
#[derive(Default)]
pub(crate) struct WorkspaceState {
    project_scope: Option<Value>,
    minimum_priority: u64,
    search: String,
    ranks: Value,
    last_detail: Option<(String, String)>,
    inspected: Option<(String, Value)>,
    review: Option<ReviewContext>,
    pending_jump: Option<(ViewSource, String, String, usize)>,
    restored_generation: Option<u64>,
    rank_undo: Option<Value>,
    pub batch: Option<BatchBinding>,
    pub legacy_launch: Option<(String, Value)>,
    resuming_batch: Option<(String, String)>,
    import_requested: bool,
}
impl WorkspaceState {
    pub fn new(saved: &Value, _environments: &[Environment]) -> Self {
        Self {
            project_scope: saved
                .get("rustBrowserScope")
                .filter(|v| v.is_object())
                .cloned(),
            ranks: saved["inboxPreferences"].clone(),
            ..Self::default()
        }
    }
}

pub(super) fn migrate_launch_forms(state: &State, environments: &[Environment]) -> Result<()> {
    if !state.snapshot()["rustLaunches"].is_object() {
        return Ok(());
    }
    state.update(|saved| {
        let old = saved["rustLaunches"].as_object().unwrap().clone();
        if !saved["rustLaunchForms"].is_object() {
            saved["rustLaunchForms"] = json!({});
        }
        for (environment, form) in old {
            let Some(env) = environments
                .iter()
                .find(|env| env.environment_id == environment)
            else {
                continue;
            };
            let project = string(&form, "projectId");
            if !domain::valid_id(&project) {
                continue;
            }
            let key = json!([env.id, env.environment_id, project]).to_string();
            if saved["rustLaunchForms"]
                .get(&key)
                .is_none_or(|existing| existing == &form)
            {
                saved["rustLaunchForms"][key] = form;
                saved["rustLaunches"]
                    .as_object_mut()
                    .unwrap()
                    .remove(&environment);
            }
        }
        if saved["rustLaunches"]
            .as_object()
            .is_some_and(|forms| forms.is_empty())
        {
            saved.as_object_mut().unwrap().remove("rustLaunches");
        }
        Ok(())
    })
}

fn digest(text: &str) -> String {
    format!("{:x}", Sha256::digest(text.as_bytes()))
}
fn rank_key(environment: &str, project: &str, thread: &str) -> String {
    json!([environment, project, thread]).to_string()
}
fn source_matches(a: &Value, b: &Value) -> bool {
    ["profileId", "environmentId", "projectId", "threadId"]
        .iter()
        .all(|key| a[*key] == b[*key])
}

impl App {
    pub(super) fn workspace_command(&mut self, name: &str, args: &str) -> Result<bool> {
        match name {
            "find" | "loaded-search" => {
                if args.is_empty() {
                    self.text_dialog("Find in loaded messages", "Search this bounded page. Earlier and native server search remain separate actions.", InputTarget::LoadedSearch, self.workspace.search.clone());
                } else {
                    self.loaded_search(args)?;
                }
            }
            "find-next" | "find-previous" => self.next_loaded_match(name == "find-previous")?,
            "outline" | "loaded-outline" => self.loaded_outline(None)?,
            "loaded-jump" => {
                let index = args
                    .parse::<usize>()
                    .context("Use /loaded-jump with the displayed message number")?;
                let source = ViewSource::active(self)?;
                let snapshot = self
                    .active
                    .as_ref()
                    .and_then(|a| a.projection.snapshot())
                    .context("Conversation is synchronizing")?;
                let row = snapshot
                    .items
                    .get(index.saturating_sub(1))
                    .filter(|_| index > 0)
                    .context("Message number is outside this loaded page")?;
                self.workspace_dispatch(WorkspaceAction::Jump {
                    source,
                    key: row.key.clone(),
                    digest: digest(&row.text),
                })?;
            }
            "copy" => self.copy_menu(args)?,
            "project" | "browse-scope" => self.project_scope_menu()?,
            "priority" => self.rank_menu(false)?,
            "project-order" => self.rank_menu(true)?,
            "priority-filter" => self.menu(
                "Minimum local priority",
                "Filters presentation only; T3 execution and queues are unchanged.",
                [
                    "Every priority",
                    "Medium and higher",
                    "High and urgent",
                    "Urgent only",
                ]
                .iter()
                .enumerate()
                .map(|(i, label)| {
                    Choice::new(
                        *label,
                        "",
                        Intent::Workspace(WorkspaceAction::MinimumPriority(i as u64)),
                    )
                })
                .collect(),
            ),
            "priority-undo" => self.workspace_dispatch(WorkspaceAction::UndoRanks)?,
            "saved-launches" => self.saved_launches()?,
            "launch-defaults-reset" => {
                self.workspace_dispatch(WorkspaceAction::ResetLaunchDefaults)?
            }
            "clear" => {
                let text = self.text();
                let a = self.active.as_ref().context("Open a conversation first")?;
                if !text.is_empty() {
                    self.state.remember_prompt(
                        &self.environments[a.profile].environment_id,
                        &a.thread,
                        "cleared",
                        &text,
                    )?;
                }
                self.replace_text(String::new());
                self.flush_draft()?;
                self.notice = "Draft cleared. Its text remains available in Prompt recall.".into();
            }
            _ => return Ok(false),
        }
        Ok(true)
    }
    pub(super) fn workspace_dispatch(&mut self, action: WorkspaceAction) -> Result<()> {
        match action {
            WorkspaceAction::Scope(scope) => {
                if let Some(source) = &scope {
                    ensure!(
                        self.environments.iter().any(|e| source["profileId"] == e.id
                            && source["environmentId"] == e.environment_id),
                        "Original browsing source is unavailable"
                    );
                }
                self.state.update(|value| {
                    if let Some(scope) = &scope {
                        value["rustBrowserScope"] = scope.clone();
                    } else {
                        value.as_object_mut().unwrap().remove("rustBrowserScope");
                    }
                    Ok(())
                })?;
                self.workspace.project_scope = scope;
                self.scope = None;
                self.rebuild();
                self.notice = format!(
                    "Browsing {}. Creation destination remains independent.",
                    self.workspace_scope_label()
                );
            }
            WorkspaceAction::MinimumPriority(value) => {
                ensure!(value <= 3, "Invalid minimum priority");
                self.workspace.minimum_priority = value;
                self.rebuild();
            }
            WorkspaceAction::Rank {
                source,
                revision,
                value,
                project,
            } => self.change_rank(source, revision, value, project)?,
            WorkspaceAction::UndoRanks => {
                let old = self
                    .workspace
                    .rank_undo
                    .clone()
                    .context("No local priority change to undo")?;
                let expected = old["expectedRevision"]
                    .as_u64()
                    .context("Missing priority revision")?;
                self.state.update(|value| {
                    ensure!(
                        value["inboxPreferences"]["revision"] == expected,
                        "Priorities changed again; old Undo cannot overwrite them"
                    );
                    let mut previous = old["previous"].clone();
                    previous["revision"] = json!(expected + 1);
                    value["inboxPreferences"] = previous;
                    Ok(())
                })?;
                self.workspace.rank_undo = None;
                self.workspace.ranks = self.state.snapshot()["inboxPreferences"].clone();
                self.rebuild();
                self.notice = "Previous local presentation ranks restored.".into();
            }
            WorkspaceAction::Row {
                source,
                key,
                digest: expected,
            } => {
                source.check(self)?;
                let row = self
                    .active
                    .as_ref()
                    .and_then(|a| a.projection.snapshot())
                    .and_then(|s| {
                        s.items
                            .iter()
                            .find(|r| r.key == key && digest(&r.text) == expected)
                    })
                    .context("Message changed; reopen its loaded outline")?
                    .clone();
                let label = format!("{} / {}", source.environment, row.key);
                let choices = vec![
                    Choice::new(
                        "Go to message",
                        "Return to its exact loaded position",
                        Intent::Workspace(WorkspaceAction::Jump {
                            source,
                            key,
                            digest: expected,
                        }),
                    ),
                    Choice::new(
                        "Copy this message",
                        &label,
                        Intent::Workspace(WorkspaceAction::Copy {
                            label: label.clone(),
                            text: row.text.clone(),
                        }),
                    ),
                    Choice::new(
                        "Code blocks",
                        "Choose a complete fenced block",
                        Intent::Workspace(WorkspaceAction::Codes {
                            source: label.clone(),
                            text: row.text.clone(),
                        }),
                    ),
                    Choice::new("Close", "", Intent::Close),
                ];
                self.menu(
                    format!("{} · {}", row.kind, row.item_id),
                    format!(
                        "Source: {label}{}",
                        if row.truncated {
                            " · native bounded excerpt"
                        } else {
                            ""
                        }
                    ),
                    choices,
                );
                if let Some(dialog) = &mut self.dialog {
                    dialog.detail = row.text;
                }
            }
            WorkspaceAction::Jump {
                source,
                key,
                digest: expected,
            } => {
                source.check(self)?;
                ensure!(
                    self.active
                        .as_ref()
                        .and_then(|a| a.projection.snapshot())
                        .is_some_and(|s| s
                            .items
                            .iter()
                            .any(|r| r.key == key && digest(&r.text) == expected)),
                    "Message changed; reopen the outline"
                );
                self.workspace.pending_jump = Some((source, key, expected, 0));
                self.focus = Focus::Transcript;
                self.follow = false;
                self.dirty = true;
            }
            WorkspaceAction::Copy { label, text } => {
                let sequence = clipboard_sequence(&text)?;
                io::stdout().write_all(sequence.as_bytes())?;
                io::stdout().flush()?;
                self.notice = format!(
                    "Copied {} bytes from {label} through terminal OSC52.",
                    text.len()
                );
            }
            WorkspaceAction::Codes { source, text } => {
                let blocks = fenced_blocks(&text);
                ensure!(
                    !blocks.is_empty(),
                    "No complete fenced code blocks in this exact message"
                );
                self.menu(
                    "Copy code block",
                    format!("Source: {source}. Only complete loaded blocks are offered."),
                    blocks
                        .into_iter()
                        .enumerate()
                        .map(|(i, (language, text))| {
                            Choice::new(
                                format!("Block {} · {language}", i + 1),
                                text.lines().next().unwrap_or("").to_owned(),
                                Intent::Workspace(WorkspaceAction::Copy {
                                    label: format!("{source}, block {}", i + 1),
                                    text,
                                }),
                            )
                        })
                        .collect(),
                );
            }
            WorkspaceAction::Preset {
                source,
                name,
                selection,
            } => {
                source.check(self)?;
                let mut choices = vec![
                    Choice::new("Keep favorite", "", Intent::Close),
                    Choice::new(
                        "Review for current conversation",
                        "Fresh catalog and expected native selection",
                        Intent::Workspace(WorkspaceAction::ApplyPreset {
                            source: source.clone(),
                            selection: selection.clone(),
                            scope: ModelScope::Current,
                        }),
                    ),
                    Choice::new(
                        "Review for next message",
                        "One-message override; retires only on matching acknowledgement",
                        Intent::Workspace(WorkspaceAction::ApplyPreset {
                            source: source.clone(),
                            selection: selection.clone(),
                            scope: ModelScope::Next,
                        }),
                    ),
                ];
                choices.push(Choice::new(
                    "Remove this favorite",
                    "Remove only this exact environment-scoped saved selection",
                    Intent::Workspace(WorkspaceAction::RemovePreset {
                        environment: source.environment,
                        name: name.clone(),
                        selection: selection.clone(),
                    }),
                ));
                self.menu(format!("Favorite · {name}"), "Review or explicitly remove the stored selection. A favorite cannot switch native provider accounts.", choices);
                if let Some(dialog) = &mut self.dialog {
                    dialog.detail = serde_json::to_string_pretty(&selection)?;
                }
            }
            WorkspaceAction::ApplyPreset {
                source,
                selection,
                scope,
            } => {
                source.check(self)?;
                self.model_edit = Some(ModelEdit {
                    scope,
                    catalog: Value::Null,
                    original: Value::Null,
                    selection,
                });
                self.read(
                    "models",
                    json!({"threadId": source.thread}),
                    ReadKind::Models(scope),
                )?;
            }
            WorkspaceAction::RemovePreset {
                environment,
                name,
                selection,
            } => {
                self.state.update(|value| {
                    let list = value["modelPresets"][&environment]
                        .as_array_mut()
                        .context("Favorite was removed")?;
                    let index = list
                        .iter()
                        .position(|p| p["name"] == name && p["selection"] == selection)
                        .context("Favorite changed; reopen it before removing")?;
                    list.remove(index);
                    Ok(())
                })?;
                self.notice = format!(
                    "Removed favorite {name} from {environment}. Native selections are unchanged."
                );
            }
            WorkspaceAction::SavedLaunch { key, draft } => self.restore_saved_launch(key, draft)?,
            WorkspaceAction::RustLaunch { key, draft } => {
                ensure!(
                    self.state.snapshot()["rustLaunchForms"][&key] == draft,
                    "Saved form changed; reopen it"
                );
                self.menu(format!("Saved launch · {}",string(&draft,"title")),format!("Source: {key}. Restore opens a fresh native review. Discard removes only this exact local form."),vec![
                    Choice::new("Keep saved","",Intent::Close),
                    Choice::new("Restore launch review","Keep its original environment and project",Intent::Workspace(WorkspaceAction::RestoreRustLaunch{key:key.clone(),draft:draft.clone()})),
                    Choice::new("Discard this local form","Native actions and remaining task lists are unchanged",Intent::Workspace(WorkspaceAction::DiscardRustLaunch{key,draft:draft.clone()}))]);
                if let Some(dialog) = &mut self.dialog {
                    dialog.detail = serde_json::to_string_pretty(&draft)?;
                }
            }
            WorkspaceAction::DiscardRustLaunch { key, draft } => {
                ensure!(
                    !self
                        .state
                        .pending_records()
                        .iter()
                        .any(|pending| pending.record["rustLaunchFormKey"] == key),
                    "An unresolved operation uses this form; review Recovery first"
                );
                self.state.update(|value| {
                    ensure!(
                        value["rustLaunchForms"][&key] == draft,
                        "Saved form changed; reopen it before discarding"
                    );
                    value["rustLaunchForms"]
                        .as_object_mut()
                        .unwrap()
                        .remove(&key);
                    Ok(())
                })?;
                if self.launch.as_ref().is_some_and(|launch| {
                    self.launch_form_key(launch.profile, &launch.project) == key
                }) {
                    self.launch = None;
                    self.workspace.batch = None;
                    self.workspace.legacy_launch = None;
                }
                self.notice="Exact local launch form discarded. Remaining task lists and native work are preserved.".into();
            }
            WorkspaceAction::RestoreRustLaunch { key, draft } => {
                ensure!(
                    self.state.snapshot()["rustLaunchForms"][&key] == draft,
                    "Saved form changed; reopen it"
                );
                let ids: Vec<String> = serde_json::from_str(&key)?;
                ensure!(ids.len() == 3, "Saved launch source is invalid");
                let profile = self
                    .environments
                    .iter()
                    .position(|env| env.id == ids[0] && env.environment_id == ids[1])
                    .context("Original launch profile is unavailable")?;
                self.choose_project_result(
                    profile,
                    json!({"id":ids[2],"title":ids[2]}),
                    NewPurpose::Launch,
                )?;
            }
            WorkspaceAction::SavedBatch { key, text } => {
                ensure!(
                    self.state.snapshot()["drafts"][&key] == text,
                    "Saved task list changed; reopen saved drafts"
                );
                let (environment, suffix) = key
                    .split_once("/\0file-many/")
                    .context("Task list has no exact source")?;
                let profile = self
                    .environments
                    .iter()
                    .position(|e| e.environment_id == environment)
                    .context("Original task-list environment is unavailable")?;
                self.workspace.resuming_batch =
                    Some((environment.into(), format!("\0file-many/{suffix}")));
                self.choose_project(profile, NewPurpose::Batch)?;
            }
            WorkspaceAction::ResetLaunchDefaults => {
                let launch = self.launch.as_mut().context("Open a launch review first")?;
                launch.selection = Value::Null;
                launch.workspace = None;
                self.save_launch()?;
                self.load_defaults()?;
            }
            WorkspaceAction::BatchReceipt(id) => self.batch_receipt(&id)?,
            WorkspaceAction::ResumeBatch(id) => self.resume_batch(&id)?,
        }
        self.dirty = true;
        Ok(())
    }
    pub(super) fn loaded_search(&mut self, text: &str) -> Result<()> {
        ensure!(
            !text.trim().is_empty() && text.len() <= 800,
            "Search needs 1–800 bytes"
        );
        self.workspace.search = text.to_owned();
        self.loaded_outline(Some(text))
    }
    fn loaded_outline(&mut self, query: Option<&str>) -> Result<()> {
        let source = ViewSource::active(self)?;
        let snapshot = self
            .active
            .as_ref()
            .and_then(|a| a.projection.snapshot())
            .context("Conversation is synchronizing")?;
        let query = query.map(str::to_lowercase);
        let mut choices = Vec::new();
        for (index, row) in snapshot.items.iter().enumerate() {
            if query
                .as_ref()
                .is_some_and(|q| !row.text.to_lowercase().contains(q))
            {
                continue;
            }
            choices.push(Choice::new(
                format!(
                    "{} · {} · {}",
                    index + 1,
                    row.kind,
                    row.text
                        .lines()
                        .find(|line| !line.trim().is_empty())
                        .unwrap_or("(empty)")
                        .chars()
                        .take(140)
                        .collect::<String>()
                ),
                format!(
                    "{}{}",
                    row.key,
                    if row.truncated {
                        " · bounded excerpt"
                    } else {
                        ""
                    }
                ),
                Intent::Workspace(WorkspaceAction::Row {
                    source: source.clone(),
                    key: row.key.clone(),
                    digest: digest(&row.text),
                }),
            ));
        }
        self.menu(if query.is_some() { "Matches in loaded messages" } else { "Loaded message outline" }, format!("{} matches on this page. No server search or native mutation. Earlier loads more history.", choices.len()), choices);
        Ok(())
    }
    fn next_loaded_match(&mut self, previous: bool) -> Result<()> {
        ensure!(
            !self.workspace.search.is_empty(),
            "Find text in this page first"
        );
        let source = ViewSource::active(self)?;
        let snapshot = self
            .active
            .as_ref()
            .and_then(|a| a.projection.snapshot())
            .context("Conversation is synchronizing")?;
        let query = self.workspace.search.to_lowercase();
        let mut matches: Vec<_> = snapshot
            .items
            .iter()
            .filter(|r| r.text.to_lowercase().contains(&query))
            .filter_map(|r| {
                self.renderer
                    .transcript_item_offset(&source.display_key(&r.key))
                    .map(|offset| (r, offset))
            })
            .collect();
        ensure!(!matches.is_empty(), "No matches in the loaded page");
        if previous {
            matches.reverse();
        }
        let top = self.renderer.transcript_scroll();
        let (row, _) = matches
            .iter()
            .find(|(_, offset)| {
                if previous {
                    *offset < top
                } else {
                    *offset > top
                }
            })
            .unwrap_or(&matches[0]);
        self.workspace_dispatch(WorkspaceAction::Jump {
            source,
            key: row.key.clone(),
            digest: digest(&row.text),
        })
    }
    fn copy_menu(&mut self, target: &str) -> Result<()> {
        let mut targets: Vec<(&str, String, String)> = Vec::new();
        if let Ok(source) = ViewSource::active(self) {
            targets.push((
                "draft",
                format!("Draft · {} / {}", source.environment, source.thread),
                self.text(),
            ));
            if let Some(snapshot) = self.active.as_ref().and_then(|a| a.projection.snapshot()) {
                targets.push((
                    "page",
                    format!("Loaded page · {} / {}", source.environment, source.thread),
                    snapshot
                        .items
                        .iter()
                        .map(|r| {
                            format!(
                                "{} · {}{}\n{}",
                                r.kind,
                                r.key,
                                if r.truncated { " (excerpt)" } else { "" },
                                r.text
                            )
                        })
                        .collect::<Vec<_>>()
                        .join("\n\n"),
                ));
                if let Some(row) = snapshot
                    .items
                    .iter()
                    .rev()
                    .find(|r| r.kind == "assistant_message")
                {
                    targets.push((
                        "answer",
                        format!(
                            "Latest loaded answer · {} / {}",
                            source.environment, row.key
                        ),
                        row.text.clone(),
                    ));
                }
            }
        }
        if let Some((label, text)) = &self.workspace.last_detail {
            targets.push(("detail", format!("Detail · {label}"), text.clone()));
        }
        if let Some((source, item)) = &self.workspace.inspected {
            targets.push((
                "input",
                format!("Tool input · {source}"),
                string(item, "input"),
            ));
            targets.push((
                "output",
                format!("Tool output · {source}"),
                string(item, "output"),
            ));
        }
        targets.push((
            "error",
            "Current status / error".into(),
            self.notice.clone(),
        ));
        if target == "code" {
            let (_, source, text) = targets
                .into_iter()
                .find(|(kind, _, _)| *kind == "answer")
                .context("No assistant answer in the loaded page")?;
            return self.workspace_dispatch(WorkspaceAction::Codes { source, text });
        }
        if !target.is_empty() {
            let (_, label, text) = targets
                .into_iter()
                .find(|(kind, _, _)| *kind == target || target == "result" && *kind == "output")
                .context("Copy target unavailable; open /copy for exact available sources")?;
            return self.workspace_dispatch(WorkspaceAction::Copy { label, text });
        }
        let mut choices: Vec<_> = targets
            .into_iter()
            .filter(|(_, _, text)| !text.is_empty())
            .map(|(_, label, text)| {
                Choice::new(
                    label.clone(),
                    format!(
                        "{} bytes · explicit terminal OSC52 clipboard write",
                        text.len()
                    ),
                    Intent::Workspace(WorkspaceAction::Copy { label, text }),
                )
            })
            .collect();
        choices.push(Choice::new(
            "Code in latest answer",
            "Choose a complete fenced block",
            Intent::Command("copy code".into()),
        ));
        self.menu("Copy exact text", "Uses the terminal clipboard (OSC52). Each choice captures the displayed source; changing selection cannot retarget it.", choices);
        Ok(())
    }
    fn project_scope_menu(&mut self) -> Result<()> {
        let mut seen = HashSet::new();
        let mut choices = vec![Choice::new(
            "All projects and environments",
            "Reset local browsing scope",
            Intent::Workspace(WorkspaceAction::Scope(None)),
        )];
        for thread in &self.threads {
            if thread.project_id.is_empty()
                || !seen.insert((thread.profile, thread.project_id.clone()))
            {
                continue;
            }
            let env = &self.environments[thread.profile];
            let source = json!({"profileId": env.id, "environmentId": env.environment_id, "projectId": thread.project_id});
            let mut choice = Choice::new(
                format!("{} / {}", env.label, thread.project),
                thread.project_id.clone(),
                Intent::Workspace(WorkspaceAction::Scope(Some(source.clone()))),
            );
            choice.checked = self.workspace.project_scope.as_ref() == Some(&source);
            choices.push(choice);
        }
        self.menu("Browse project", "Projects on loaded metadata pages. More and Refresh discover additional projects. Browsing never changes the creation destination or attached conversation.", choices);
        Ok(())
    }
    fn rank_menu(&mut self, project: bool) -> Result<()> {
        let thread = self
            .selected_thread()
            .context("Select a conversation with a native project")?;
        ensure!(
            domain::valid_id(&thread.project_id),
            "Native project identity is unavailable"
        );
        let env = &self.environments[thread.profile];
        let source = json!({"profileId": env.id, "environmentId": env.environment_id, "projectId": thread.project_id, "threadId": if project { "" } else { &thread.id }});
        let revision = self.state.snapshot()["inboxPreferences"]["revision"]
            .as_u64()
            .unwrap_or(0);
        let options = if project {
            vec![
                (0, "First"),
                (25, "Early"),
                (100, "Normal"),
                (175, "Later"),
                (200, "Last"),
            ]
        } else {
            vec![(0, "Low"), (1, "Medium"), (2, "High"), (3, "Urgent")]
        };
        let mut choices: Vec<_> = options
            .into_iter()
            .map(|(value, label)| {
                Choice::new(
                    label,
                    "Local presentation order only",
                    Intent::Workspace(WorkspaceAction::Rank {
                        source: source.clone(),
                        revision,
                        value,
                        project,
                    }),
                )
            })
            .collect();
        if self.workspace.rank_undo.is_some() {
            choices.push(Choice::new(
                "Undo previous rank change",
                "Revision checked before restoring",
                Intent::Workspace(WorkspaceAction::UndoRanks),
            ));
        }
        self.menu(
            if project {
                "Project order"
            } else {
                "Task priority"
            },
            format!(
                "{} / {} / {}. AgentBox sorts these local ranks; no work is scheduled.",
                env.label, thread.project, thread.title
            ),
            choices,
        );
        Ok(())
    }
    fn change_rank(
        &mut self,
        source: Value,
        revision: u64,
        rank: u64,
        project: bool,
    ) -> Result<()> {
        ensure!(
            self.environments
                .iter()
                .any(|env| source["profileId"] == env.id
                    && source["environmentId"] == env.environment_id),
            "Original source is no longer configured"
        );
        let key = rank_key(
            &string(&source, "environmentId"),
            &string(&source, "projectId"),
            &string(&source, "threadId"),
        );
        let mut previous = Value::Null;
        self.state.update(|value| {
            if !value["inboxPreferences"].is_object() {
                value["inboxPreferences"] =
                    json!({"version": 1, "revision": 0, "taskPriorities": {}, "projectRanks": {}});
            }
            let preferences = &mut value["inboxPreferences"];
            ensure!(
                preferences["revision"] == revision,
                "Local priorities changed; reopen the menu"
            );
            previous = preferences.clone();
            preferences[if project {
                "projectRanks"
            } else {
                "taskPriorities"
            }][&key] = json!(rank);
            preferences["revision"] = json!(revision + 1);
            crate::state::validate_inbox_preferences(preferences)
        })?;
        self.workspace.rank_undo =
            Some(json!({"expectedRevision": revision + 1, "previous": previous}));
        self.workspace.ranks = self.state.snapshot()["inboxPreferences"].clone();
        self.rebuild();
        self.notice =
            "Local presentation rank saved. Native execution and queues are unchanged.".into();
        Ok(())
    }
    pub(super) fn favorite_menu(&mut self) -> Result<()> {
        let source = ViewSource::active(self)?;
        let saved = self.state.snapshot();
        let mut choices: Vec<_> = array(&saved["modelPresets"], &source.environment)
            .iter()
            .map(|preset| {
                Choice::new(
                    string(preset, "name"),
                    format!(
                        "{} · {}",
                        string(&preset["selection"], "model"),
                        string(&preset["selection"], "instanceId")
                    ),
                    Intent::Workspace(WorkspaceAction::Preset {
                        source: source.clone(),
                        name: string(preset, "name"),
                        selection: preset["selection"].clone(),
                    }),
                )
            })
            .collect();
        choices.push(Choice::new(
            "Choose and save a model",
            "Open the native catalog",
            Intent::Command("model".into()),
        ));
        self.menu("Model favorites", "Choose a saved selection to apply to the current conversation, stage for one message, or remove after review.", choices);
        Ok(())
    }
    pub(super) fn read_review(&mut self, value: Value) -> Result<()> {
        let active = self.active.as_ref().context("Conversation closed")?;
        let env = &self.environments[active.profile];
        ensure!(
            value["threadId"] == active.thread
                && value["projectId"] == self.metadata()["projectId"],
            "Review belongs to a different native source"
        );
        let source = json!({"profileId": env.id, "environmentId": env.environment_id, "projectId": value["projectId"], "threadId": active.thread});
        let review_digest = review_digest(&value);
        let saved = self.state.snapshot();
        let previous = array(&saved, "reviewPositions")
            .iter()
            .find(|p| source_matches(p, &source));
        let offset = review_digest
            .as_ref()
            .and_then(|hash| previous.filter(|p| p["digest"] == *hash))
            .and_then(|p| p["offset"].as_u64())
            .unwrap_or(0)
            .min(1_000_000) as usize;
        let mut text = format!(
            "Source: {} / {}\nWorkspace: {}\nBranch: {}\n\n{}",
            env.environment_id,
            active.thread,
            string(&value, "workspace"),
            string(&value, "branch"),
            string(&value, "diff")
        );
        if flag(&value, "diffTruncated") {
            text.push_str("\n\nDiff shortened at native display limit; reading position will not be restored.");
        }
        if !string(&value, "unavailable").is_empty() {
            text.push_str(&format!("\n{}", string(&value, "unavailable")));
        }
        for pr in array(&value, "pullRequests") {
            text.push_str(&format!(
                "\n\nPR #{} {}\n{}",
                pr["number"],
                string(pr, "title"),
                string(pr, "url")
            ));
        }
        if previous.is_some() && offset == 0 {
            text.push_str("\n\nSaved reading coordinate was not applied: native diff hashes changed or are incomplete.");
        }
        self.detail("Native changes and pull requests", text);
        if let Some(hash) = review_digest {
            if let Some(dialog) = &mut self.dialog {
                dialog.scroll = offset;
                self.workspace.review = Some(ReviewContext {
                    source,
                    digest: hash,
                    detail_digest: digest(&dialog.detail),
                });
            }
        } else {
            self.state.update(|saved| {
                let positions: Vec<_> = array(saved, "reviewPositions")
                    .iter()
                    .filter(|old| !source_matches(old, &source))
                    .cloned()
                    .collect();
                saved["reviewPositions"] = json!(positions);
                Ok(())
            })?;
        }
        Ok(())
    }
    pub(super) fn read_inspection(&mut self, value: Value) -> Result<()> {
        let environment = self.active_env()?.environment_id.clone();
        let source = format!(
            "{} / {} / {}",
            environment,
            string(&value, "sourceThreadId"),
            string(&value, "itemId")
        );
        let text = format!(
            "Source: {source}\nStatus: {}{}\n\nINPUT\n{}\n\nOUTPUT\n{}{}",
            string(&value, "status"),
            if flag(&value, "changed") {
                " · changed since listed revision"
            } else {
                ""
            },
            string(&value, "input"),
            string(&value, "output"),
            if flag(&value, "truncated") {
                "\n\nBounded native display; full source remains in T3."
            } else {
                ""
            }
        );
        self.detail(string(&value, "title"), text);
        self.workspace.inspected = Some((source, value));
        Ok(())
    }
    fn saved_launches(&mut self) -> Result<()> {
        let saved = self.state.snapshot();
        let mut choices = Vec::new();
        if let Some(launches) = saved["rustLaunchForms"].as_object() {
            for (key, draft) in launches {
                choices.push(Choice::new(
                    format!("Launch · {}", string(draft, "title")),
                    key.clone(),
                    Intent::Workspace(WorkspaceAction::RustLaunch {
                        key: key.clone(),
                        draft: draft.clone(),
                    }),
                ));
            }
        }
        if let Some(launches) = saved["launchDrafts"].as_object() {
            for (key, draft) in launches {
                choices.push(Choice::new(
                    format!("Saved launch · {}", string(draft, "title")),
                    key.clone(),
                    Intent::Workspace(WorkspaceAction::SavedLaunch {
                        key: key.clone(),
                        draft: draft.clone(),
                    }),
                ));
            }
        }
        if let Some(drafts) = saved["drafts"].as_object() {
            for (key, text) in drafts {
                if key.contains("/\0file-many/") && text.as_str().is_some_and(|s| !s.is_empty()) {
                    choices.push(Choice::new(
                        "Unfiled task list",
                        key.replace('\0', ""),
                        Intent::Workspace(WorkspaceAction::SavedBatch {
                            key: key.clone(),
                            text: text.as_str().unwrap().into(),
                        }),
                    ));
                }
            }
        }
        self.menu("Saved launch forms and task lists", "Imported forms keep their exact source. Opening restores editable text and requires a fresh native launch review; nothing is sent.", choices);
        Ok(())
    }
    fn restore_saved_launch(&mut self, key: String, draft: Value) -> Result<()> {
        ensure!(
            self.state.snapshot()["launchDrafts"][&key] == draft,
            "Saved launch changed; reopen saved forms"
        );
        self.workspace.legacy_launch = Some((key.clone(), draft.clone()));
        self.workspace.batch = None;
        self.workspace.import_requested = true;
        if key == "t3oxide-client/new-message" {
            return self.begin_new(NewPurpose::Launch);
        }
        let profile = self
            .environments
            .iter()
            .enumerate()
            .filter(|(_, e)| key.starts_with(&format!("{}/", e.environment_id)))
            .max_by_key(|(_, e)| e.environment_id.len())
            .map(|(p, _)| p)
            .context("Original launch environment is unavailable; saved form remains untouched")?;
        let project = string(&draft, "projectId");
        if domain::valid_id(&project) {
            self.choose_project_result(
                profile,
                json!({"id": project, "title": project}),
                NewPurpose::Launch,
            )
        } else {
            self.choose_project(profile, NewPurpose::Launch)
        }
    }
    pub(super) fn imported_launch(&mut self, profile: usize, project: &Value) -> Option<Value> {
        let environment = &self.environments[profile].environment_id;
        let saved = self.state.snapshot();
        if self.workspace.legacy_launch.is_none() {
            let exact = format!("{environment}/project-launch/{}", string(project, "id"));
            if let Some(form) = saved["launchDrafts"].get(&exact).filter(|v| v.is_object()) {
                self.workspace.legacy_launch = Some((exact, form.clone()));
            } else if let Some(form) = saved["launchDrafts"]
                .get("t3oxide-client/new-message")
                .filter(|v| v.is_object())
            {
                self.workspace.legacy_launch =
                    Some(("t3oxide-client/new-message".into(), form.clone()));
            }
        }
        let (key, form) = self.workspace.legacy_launch.as_ref()?;
        if (key == "t3oxide-client/new-message" || key.starts_with(&format!("{environment}/")))
            && (string(form, "projectId").is_empty() || form["projectId"] == project["id"])
        {
            Some(form.clone())
        } else {
            None
        }
    }
    pub(super) fn take_import_request(&mut self) -> bool {
        std::mem::take(&mut self.workspace.import_requested)
    }
    pub(super) fn launch_form_key(&self, profile: usize, project: &Value) -> String {
        let env = &self.environments[profile];
        json!([env.id, env.environment_id, string(project, "id")]).to_string()
    }
    pub(super) fn restore_launch_bindings(
        &mut self,
        profile: usize,
        project: &Value,
        saved: &Value,
    ) {
        let environment = &self.environments[profile].environment_id;
        self.workspace.batch =
            serde_json::from_value::<BatchBinding>(saved["batchBinding"].clone())
                .ok()
                .filter(|binding| {
                    &binding.environment == environment && project["id"] == binding.project
                });
        self.workspace.legacy_launch = None;
        if let Some(key) = saved["importedLaunchKey"].as_str()
            && let Some(form) = self.state.snapshot()["launchDrafts"]
                .get(key)
                .filter(|form| digest(&form.to_string()) == string(saved, "importedLaunchDigest"))
        {
            self.workspace.legacy_launch = Some((key.into(), form.clone()));
        }
    }
    pub(super) fn save_launch_bindings(&self, launch: &Launch, saved: &mut Value) -> Result<()> {
        let environment = &self.environments[launch.profile].environment_id;
        if let Some(binding) = &self.workspace.batch
            && &binding.environment == environment
            && launch.project["id"] == binding.project
            && launch.batch.as_ref().is_some_and(|(before, consumed)| {
                before == &binding.before && consumed == &binding.consumed
            })
        {
            saved["batchBinding"] = serde_json::to_value(binding)?;
        }
        if let Some((key, form)) = &self.workspace.legacy_launch
            && (key == "t3oxide-client/new-message" || key.starts_with(&format!("{environment}/")))
            && (string(form, "projectId").is_empty() || form["projectId"] == launch.project["id"])
        {
            saved["importedLaunchKey"] = json!(key);
            saved["importedLaunchDigest"] = json!(digest(&form.to_string()));
        }
        Ok(())
    }
    pub(super) fn bind_launch_submission(&self, launch: &Launch, fields: &mut Value) -> Result<()> {
        let mut bindings = json!({});
        self.save_launch_bindings(launch, &mut bindings)?;
        let form_key = self.launch_form_key(launch.profile, &launch.project);
        let saved = self.state.snapshot();
        let form = saved["rustLaunchForms"]
            .get(&form_key)
            .context("Save this exact launch form before creating")?;
        fields["rustLaunchFormKey"] = json!(form_key);
        fields["rustLaunchFormDigest"] = json!(digest(&form.to_string()));
        if let Some((before, consumed)) = &launch.batch {
            let binding = self
                .workspace
                .batch
                .as_ref()
                .context("Task receipt binding expired; reopen the exact remaining list")?;
            ensure!(
                bindings["batchBinding"].is_object(),
                "Task source changed; reopen the exact remaining list"
            );
            fields["batchDraft"] = json!({"environment": binding.environment, "source": binding.source, "before": before, "consumed": consumed, "receiptId": binding.receipt, "itemIndex": binding.index});
        }
        for key in ["importedLaunchKey", "importedLaunchDigest"] {
            if let Some(value) = bindings.get(key) {
                fields[key] = value.clone();
            }
        }
        Ok(())
    }
    pub(super) fn batch_source(&self, profile: usize, project: &Value) -> String {
        let environment = &self.environments[profile].environment_id;
        if let Some((env, source)) = &self.workspace.resuming_batch
            && env == environment
        {
            return source.clone();
        }
        if let Some(batch) = &self.workspace.batch
            && &batch.environment == environment
            && project["id"] == batch.project
        {
            return batch.source.clone();
        }
        format!("\0file-many/{}", string(project, "id"))
    }
    pub(super) fn prepare_batch(&mut self, text: &str) -> Result<()> {
        let launch = self
            .launch
            .as_ref()
            .context("Choose a native destination")?;
        let profile = launch.profile;
        let project = string(&launch.project, "id");
        let environment = self.environments[profile].environment_id.clone();
        let source = self.batch_source(profile, &launch.project);
        let items = batch_items(text)?;
        let saved = self.state.snapshot();
        let reusable = saved["batchReceipts"].as_object().and_then(|receipts| {
            receipts.values().find_map(|receipt| {
                if receipt["environmentId"] != environment || receipt["source"] != source {
                    return None;
                }
                let items = array(receipt, "items");
                let index = items
                    .iter()
                    .position(|item| item["state"] != "Acknowledged")?;
                let start = items[index]["start"].as_u64()? as usize;
                if receipt["original"].as_str()?.get(start..) != Some(text)
                    || items[index..].iter().any(|item| {
                        item["state"] != "Remaining draft"
                            || !string(item, "operationId").is_empty()
                    })
                {
                    return None;
                }
                Some((
                    string(receipt, "id"),
                    index,
                    string(receipt, "original"),
                    items[index].clone(),
                ))
            })
        });
        let (receipt, index, original, item) = if let Some(found) = reusable {
            found
        } else {
            let id = wire::new_id();
            let created = time::OffsetDateTime::now_utc()
                .format(&time::format_description::well_known::Rfc3339)?;
            let receipt = json!({"id": id, "environmentId": environment, "projectId": project, "source": source, "original": text, "createdAt": created, "items": items});
            self.state.update(|value| {
                ensure!(value["drafts"][format!("{environment}/{source}")] == text, "Task list changed before filing review");
                let receipts = value["batchReceipts"].as_object_mut().unwrap();
                if receipts.len() >= 16 {
                    let oldest = receipts.iter().filter(|(_, receipt)| array(receipt, "items").iter().all(|item| item["state"] == "Acknowledged")).min_by_key(|(_, receipt)| string(receipt, "createdAt")).map(|(id, _)| id.clone()).context("All saved task receipts have unfinished work; keep them and finish a list before creating another")?;
                    receipts.remove(&oldest);
                }
                receipts.insert(id.clone(), receipt); Ok(())
            })?;
            (id, 0, text.to_owned(), items[0].clone())
        };
        let start = item["start"]
            .as_u64()
            .context("Invalid saved task offset")? as usize;
        let end = item["end"].as_u64().context("Invalid saved task offset")? as usize;
        let consumed = original
            .get(start..end)
            .context("Saved task offsets are not UTF-8 boundaries")?
            .to_owned();
        let message = consumed
            .lines()
            .find(|line| !line.trim().is_empty())
            .context("Task is empty")?
            .to_owned();
        let title: String = message
            .trim()
            .chars()
            .scan(0, |bytes, c| {
                *bytes += c.len_utf8();
                (*bytes <= 512).then_some(c)
            })
            .collect();
        self.workspace.batch = Some(BatchBinding {
            environment,
            project,
            source,
            receipt,
            index,
            before: text.into(),
            consumed: consumed.clone(),
        });
        self.workspace.resuming_batch = None;
        self.workspace.legacy_launch = None;
        let launch = self.launch.as_mut().unwrap();
        launch.title = title;
        launch.message = message;
        launch.batch = Some((text.into(), consumed));
        self.save_launch()?;
        self.load_defaults()
    }
    pub(super) fn batch_receipts(&mut self) -> Result<()> {
        let saved = self.state.snapshot();
        let mut receipts: Vec<_> = saved["batchReceipts"]
            .as_object()
            .into_iter()
            .flat_map(|m| m.values())
            .collect();
        receipts.sort_by_key(|receipt| std::cmp::Reverse(string(receipt, "createdAt")));
        let mut choices: Vec<_> = receipts
            .into_iter()
            .map(|receipt| {
                let items = array(receipt, "items");
                let acknowledged = items
                    .iter()
                    .filter(|item| item["state"] == "Acknowledged")
                    .count();
                Choice::new(
                    format!(
                        "{} · {acknowledged}/{} acknowledged",
                        string(receipt, "environmentId"),
                        items.len()
                    ),
                    string(receipt, "createdAt"),
                    Intent::Workspace(WorkspaceAction::BatchReceipt(string(receipt, "id"))),
                )
            })
            .collect();
        choices.push(Choice::new(
            "Saved launch forms and unfiled task lists",
            "Includes original imported drafts",
            Intent::Command("saved-launches".into()),
        ));
        self.menu("Task filing receipts", "Each item records its own native operation identity. Continue always opens a new explicit review.", choices);
        Ok(())
    }
    fn batch_receipt(&mut self, id: &str) -> Result<()> {
        let saved = self.state.snapshot();
        let receipt = saved["batchReceipts"]
            .get(id)
            .context("Receipt is unavailable")?;
        let original = string(receipt, "original");
        let mut choices = vec![Choice::new("Keep saved", "", Intent::Close)];
        let mut detail = format!(
            "Environment: {}\nReceipt: {id}\nCreated: {}\n\n",
            string(receipt, "environmentId"),
            string(receipt, "createdAt")
        );
        for (index, item) in array(receipt, "items").iter().enumerate() {
            let start = item["start"].as_u64().unwrap_or(0) as usize;
            let end = item["end"].as_u64().unwrap_or(0) as usize;
            let title = original.get(start..end).unwrap_or("").trim();
            detail.push_str(&format!(
                "{}. {} · {}\n   {} / {} / {}\n   Operation: {}\n\n",
                index + 1,
                string(item, "state"),
                title,
                string(item, "environmentId"),
                string(item, "projectId"),
                string(item, "threadId"),
                string(item, "operationId")
            ));
            if item["state"] == "Acknowledged"
                && let Some(profile) = self
                    .environments
                    .iter()
                    .position(|env| item["environmentId"] == env.environment_id)
            {
                choices.push(Choice::new(
                    format!("Open acknowledged task {}", index + 1),
                    title,
                    Intent::Open(profile, string(item, "threadId")),
                ));
            }
        }
        if array(receipt, "items")
            .iter()
            .any(|item| item["state"] == "Needs recovery")
        {
            choices.push(Choice::new(
                "Review unresolved operations",
                "Reconcile original IDs before continuing",
                Intent::Command("recover".into()),
            ));
        } else if array(receipt, "items")
            .iter()
            .any(|item| item["state"] == "Remaining draft")
        {
            choices.push(Choice::new(
                "Review next unfiled task",
                "Restore the exact remaining human list",
                Intent::Workspace(WorkspaceAction::ResumeBatch(id.into())),
            ));
        }
        self.menu("Task receipt", "Native work and local draft status are shown independently. Acknowledged tasks are never automatically refiled.", choices);
        if let Some(dialog) = &mut self.dialog {
            dialog.detail = detail;
        }
        Ok(())
    }
    fn resume_batch(&mut self, id: &str) -> Result<()> {
        let saved = self.state.snapshot();
        let receipt = saved["batchReceipts"]
            .get(id)
            .context("Receipt is unavailable")?;
        ensure!(
            !array(receipt, "items")
                .iter()
                .any(|item| item["state"] == "Needs recovery"),
            "Resolve the original operation before continuing this list"
        );
        let environment = string(receipt, "environmentId");
        let profile = self
            .environments
            .iter()
            .position(|env| env.environment_id == environment)
            .context("Original task environment is unavailable")?;
        let source = string(receipt, "source");
        let item = array(receipt, "items")
            .iter()
            .find(|item| item["state"] == "Remaining draft")
            .context("Every task is already acknowledged")?;
        let original = string(receipt, "original");
        let remaining = original
            .get(item["start"].as_u64().unwrap_or(usize::MAX as u64) as usize..)
            .context("Saved task offset is invalid")?;
        ensure!(
            self.state.draft(&environment, &source) == remaining,
            "The remaining draft was edited; open Saved launch forms and task lists to review its newer text"
        );
        self.workspace.resuming_batch = Some((environment, source));
        self.workspace.batch = None;
        let project = string(receipt, "projectId");
        if domain::valid_id(&project) {
            self.choose_project_result(
                profile,
                json!({"id": project, "title": project}),
                NewPurpose::Batch,
            )
        } else {
            self.choose_project(profile, NewPurpose::Batch)
        }
    }
    pub(super) fn preserve_workspace_position(&mut self) -> Result<()> {
        self.save_reading_position()?;
        self.save_review_position()
    }
    pub(super) fn capture_detail(&mut self, title: &str, text: &str) {
        self.workspace.last_detail = Some((title.to_owned(), text.to_owned()));
        self.workspace.review = None;
    }
    pub(super) fn save_reading_position(&self) -> Result<()> {
        let Some(active) = &self.active else {
            return Ok(());
        };
        let Some(snapshot) = active.projection.snapshot() else {
            return Ok(());
        };
        let source = ViewSource::active(self)?;
        let offset = if self.follow {
            self.renderer.transcript_max_scroll()
        } else {
            self.transcript_scroll
        };
        let Some((display_key, inner)) = self.renderer.transcript_item_at(offset) else {
            return Ok(());
        };
        let prefix = source.display_key("");
        let Some(key) = display_key.strip_prefix(&prefix) else {
            return Ok(());
        };
        let Some(row) = snapshot.items.iter().find(|row| row.key == key) else {
            return Ok(());
        };
        let identity = json!([
            self.environments[active.profile].id,
            source.environment,
            source.thread
        ])
        .to_string();
        let position = json!({"rowKey": key, "digest": digest(&row.text), "offset": inner.min(1_000_000), "width": self.hits.transcript.width, "follow": self.follow});
        if self.state.snapshot()["rustReadingPositions"][&identity] == position {
            return Ok(());
        }
        self.state.update(|value| {
            if !value["rustReadingPositions"].is_object() {
                value["rustReadingPositions"] = json!({});
            }
            let positions = value["rustReadingPositions"].as_object_mut().unwrap();
            if !positions.contains_key(&identity)
                && positions.len() >= 64
                && let Some(old) = positions.keys().next().cloned()
            {
                positions.remove(&old);
            }
            positions.insert(identity, position);
            Ok(())
        })
    }
    pub(super) fn restore_reading_position_if_ready(&mut self) {
        let Some(active) = &self.active else { return };
        if active.navigating || self.workspace.restored_generation == Some(active.generation) {
            return;
        }
        let Some(snapshot) = active.projection.snapshot() else {
            return;
        };
        self.workspace.restored_generation = Some(active.generation);
        let Ok(source) = ViewSource::active(self) else {
            return;
        };
        let key = json!([
            self.environments[active.profile].id,
            source.environment,
            source.thread
        ])
        .to_string();
        let saved = self.state.snapshot()["rustReadingPositions"][key].clone();
        if saved.is_null() || flag(&saved, "follow") {
            return;
        }
        let row_key = string(&saved, "rowKey");
        if let Some(row) = snapshot
            .items
            .iter()
            .find(|r| r.key == row_key && digest(&r.text) == string(&saved, "digest"))
        {
            let offset = if saved["width"].as_u64() == Some(u64::from(self.hits.transcript.width)) {
                saved["offset"].as_u64().unwrap_or(0).min(1_000_000) as usize
            } else {
                0
            };
            self.workspace.pending_jump =
                Some((source, row.key.clone(), digest(&row.text), offset));
        }
    }
    pub(super) fn restore_rendered_position(&mut self) -> bool {
        let Some((source, key, expected, inner)) = self.workspace.pending_jump.clone() else {
            return false;
        };
        if source.check(self).is_err() {
            self.workspace.pending_jump = None;
            return false;
        }
        let valid = self
            .active
            .as_ref()
            .and_then(|a| a.projection.snapshot())
            .is_some_and(|s| {
                s.items
                    .iter()
                    .any(|r| r.key == key && digest(&r.text) == expected)
            });
        if !valid {
            self.workspace.pending_jump = None;
            return false;
        }
        let Some(top) = self
            .renderer
            .transcript_item_offset(&source.display_key(&key))
        else {
            return false;
        };
        self.transcript_scroll = top
            .saturating_add(inner)
            .min(self.renderer.transcript_max_scroll());
        self.follow = false;
        self.workspace.pending_jump = None;
        true
    }
    pub(super) fn save_review_position(&self) -> Result<()> {
        let Some(context) = &self.workspace.review else {
            return Ok(());
        };
        let Some(dialog) = &self.dialog else {
            return Ok(());
        };
        if !matches!(dialog.kind, DialogKind::Detail)
            || digest(&dialog.detail) != context.detail_digest
        {
            return Ok(());
        }
        let mut position = context.source.clone();
        position["digest"] = json!(context.digest);
        position["offset"] = json!(dialog.scroll.min(1_000_000));
        if array(&self.state.snapshot(), "reviewPositions").contains(&position) {
            return Ok(());
        }
        self.state.update(|value| {
            let mut positions = array(value, "reviewPositions").to_vec();
            positions.retain(|old| !source_matches(old, &position));
            positions.push(position);
            if positions.len() > 64 {
                positions.remove(0);
            }
            value["reviewPositions"] = json!(positions);
            Ok(())
        })
    }
    pub(super) fn workspace_order(&mut self) {
        let preferences = &self.workspace.ranks;
        self.visible.retain(|(index, _)| {
            let thread = &self.threads[*index];
            let env = &self.environments[thread.profile];
            let source_ok = self.workspace.project_scope.as_ref().is_none_or(|scope| {
                scope["profileId"] == env.id
                    && scope["environmentId"] == env.environment_id
                    && scope["projectId"] == thread.project_id
            });
            let priority = preferences["taskPriorities"]
                [rank_key(&env.environment_id, &thread.project_id, &thread.id)]
            .as_u64()
            .unwrap_or(1);
            source_ok && priority >= self.workspace.minimum_priority
        });
        let present: HashSet<_> = self
            .visible
            .iter()
            .map(|(index, _)| {
                (
                    self.threads[*index].profile,
                    self.threads[*index].id.as_str(),
                )
            })
            .collect();
        let by_id: HashMap<_, _> = self
            .threads
            .iter()
            .map(|thread| ((thread.profile, thread.id.as_str()), thread))
            .collect();
        for (index, depth) in &mut self.visible {
            let mut thread = &self.threads[*index];
            let mut seen = HashSet::new();
            *depth = 0;
            while thread.kind() == "subagent" {
                let Some(parent) = thread
                    .parent()
                    .filter(|parent| present.contains(&(thread.profile, *parent)))
                else {
                    break;
                };
                if !seen.insert((thread.profile, parent)) {
                    *depth = 0;
                    break;
                }
                let Some(next) = by_id.get(&(thread.profile, parent)) else {
                    break;
                };
                *depth = (*depth + 1).min(12);
                thread = next;
            }
        }
        if self.mode == Mode::AgentBox {
            self.visible = rank_tree(&self.visible, &|index| {
                let thread = &self.threads[index];
                let env = &self.environments[thread.profile];
                (
                    preferences["projectRanks"]
                        [rank_key(&env.environment_id, &thread.project_id, "")]
                    .as_u64()
                    .unwrap_or(100),
                    std::cmp::Reverse(
                        preferences["taskPriorities"]
                            [rank_key(&env.environment_id, &thread.project_id, &thread.id)]
                        .as_u64()
                        .unwrap_or(1),
                    ),
                )
            });
        }
    }
    pub(super) fn workspace_expansion(&self) -> Option<HashSet<String>> {
        (self.workspace.project_scope.is_some() || self.workspace.minimum_priority > 0)
            .then(|| self.threads.iter().map(Thread::key).collect())
    }
    pub(super) fn workspace_priority(&self, thread: &Thread) -> &'static str {
        match self.workspace.ranks["taskPriorities"][rank_key(
            &self.environments[thread.profile].environment_id,
            &thread.project_id,
            &thread.id,
        )]
        .as_u64()
        .unwrap_or(1)
        {
            0 => "Low",
            2 => "High",
            3 => "Urgent",
            _ => "Medium",
        }
    }
    pub(super) fn workspace_scope_label(&self) -> String {
        let source = if let Some(scope) = &self.workspace.project_scope {
            format!(
                "{} / {}",
                string(scope, "environmentId"),
                string(scope, "projectId")
            )
        } else {
            self.scope
                .map(|profile| self.environments[profile].label.clone())
                .unwrap_or_else(|| "All sources".into())
        };
        if self.workspace.minimum_priority > 0 {
            format!(
                "{source} · {} or higher",
                ["Low", "Medium", "High", "Urgent"]
                    [self.workspace.minimum_priority.min(3) as usize]
            )
        } else {
            source
        }
    }
}

fn rank_tree<T: Ord>(rows: &[(usize, usize)], rank: &impl Fn(usize) -> T) -> Vec<(usize, usize)> {
    if rows.is_empty() {
        return Vec::new();
    }
    let depth = rows.iter().map(|(_, depth)| *depth).min().unwrap();
    let mut groups: Vec<&[(usize, usize)]> = Vec::new();
    let mut start = 0;
    for index in 1..rows.len() {
        if rows[index].1 <= depth {
            groups.push(&rows[start..index]);
            start = index;
        }
    }
    groups.push(&rows[start..]);
    groups.sort_by_key(|group| rank(group[0].0));
    let mut result = Vec::with_capacity(rows.len());
    for group in groups {
        result.push(group[0]);
        result.extend(rank_tree(&group[1..], rank));
    }
    result
}

fn clipboard_sequence(text: &str) -> Result<String> {
    ensure!(
        !text.is_empty() && text.len() <= 192 * 1024,
        "Clipboard text must contain 1–192 KiB; choose an individual message or code block"
    );
    Ok(format!(
        "\x1b]52;c;{}\x07",
        STANDARD.encode(text.as_bytes())
    ))
}

fn fenced_blocks(text: &str) -> Vec<(String, String)> {
    let mut blocks = Vec::new();
    let mut open: Option<(char, usize, String, String)> = None;
    for line in text.split_inclusive('\n') {
        let body = line.trim_end_matches(['\r', '\n']);
        let trimmed = body.trim_start_matches(' ');
        let indent = body.len() - trimmed.len();
        if let Some((marker, count, _, content)) = &mut open {
            if indent <= 3
                && trimmed.chars().take_while(|c| c == marker).count() >= *count
                && trimmed.trim_start_matches(*marker).trim().is_empty()
            {
                let (_, _, language, content) = open.take().unwrap();
                blocks.push((language, content));
                if blocks.len() == 32 {
                    break;
                }
            } else {
                content.push_str(line);
            }
        } else if indent <= 3
            && let Some(marker @ ('`' | '~')) = trimmed.chars().next()
        {
            let count = trimmed.chars().take_while(|c| *c == marker).count();
            if count >= 3 {
                let language = trimmed[count..].trim();
                open = Some((
                    marker,
                    count,
                    if language.is_empty() {
                        "text".into()
                    } else {
                        language.into()
                    },
                    String::new(),
                ));
            }
        }
    }
    blocks
}

fn review_digest(review: &Value) -> Option<String> {
    if flag(review, "diffTruncated") || !string(review, "unavailable").is_empty() {
        return None;
    }
    let sources = array(review, "sources");
    if sources.is_empty() {
        return None;
    }
    #[derive(serde::Serialize)]
    struct SourceHash<'a> {
        #[serde(rename = "Kind")]
        kind: &'a str,
        #[serde(rename = "Hash")]
        hash: &'a str,
    }
    let mut hashes = Vec::new();
    for source in sources {
        if flag(source, "truncated") {
            return None;
        }
        let kind = source["kind"].as_str()?;
        let hash = source["hash"].as_str()?;
        if hash.is_empty() {
            return None;
        }
        hashes.push(SourceHash { kind, hash });
    }
    Some(format!(
        "{:x}",
        Sha256::digest(serde_json::to_vec(&hashes).ok()?)
    ))
}

fn batch_items(text: &str) -> Result<Vec<Value>> {
    ensure!(text.len() <= 32 * 1024, "Task list exceeds 32 KiB");
    let mut items = Vec::new();
    let mut start = 0;
    let mut end = 0;
    for segment in text.split_inclusive('\n') {
        end += segment.len();
        if segment.trim().is_empty() {
            continue;
        }
        ensure!(
            segment.trim_end_matches('\n').len() <= 8 * 1024,
            "Each task line must fit 8 KiB; expand the brief during launch review"
        );
        items.push(json!({"start": start, "end": end, "state": "Remaining draft"}));
        start = end;
        ensure!(
            items.len() <= 20,
            "File at most 20 explicitly reviewed tasks per list"
        );
    }
    ensure!(!items.is_empty(), "Enter at least one task");
    Ok(items)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn clipboard_is_explicit_bounded_and_encodes_terminal_controls() {
        let text = "line\n\x1b]52;c;bad\x07界";
        let encoded = clipboard_sequence(text).unwrap();
        assert_eq!(encoded.matches('\x1b').count(), 1);
        assert_eq!(
            STANDARD
                .decode(
                    encoded
                        .strip_prefix("\x1b]52;c;")
                        .unwrap()
                        .strip_suffix('\x07')
                        .unwrap()
                )
                .unwrap(),
            text.as_bytes()
        );
        assert!(clipboard_sequence(&"a".repeat(192 * 1024 + 1)).is_err());
    }
    #[test]
    fn code_copy_preserves_exact_complete_fences() {
        let text = "intro\n````rust\na\n```\nb\n````\n~~~sh\necho hi\n~~~\n```incomplete\nno";
        assert_eq!(
            fenced_blocks(text),
            vec![
                ("rust".into(), "a\n```\nb\n".into()),
                ("sh".into(), "echo hi\n".into())
            ]
        );
    }
    #[test]
    fn batch_ranges_preserve_leading_blanks_unicode_and_refuse_excess() {
        let text = "\n 界task\n\nsecond\n  ";
        let items = batch_items(text).unwrap();
        assert_eq!(
            &text[items[0]["start"].as_u64().unwrap() as usize
                ..items[0]["end"].as_u64().unwrap() as usize],
            "\n 界task\n"
        );
        assert_eq!(
            &text[items[1]["start"].as_u64().unwrap() as usize
                ..items[1]["end"].as_u64().unwrap() as usize],
            "\nsecond\n"
        );
        assert!(batch_items(&"task\n".repeat(21)).is_err());
        assert!(batch_items(" \n").is_err());
    }
    #[test]
    fn review_hash_matches_original_serialization_and_rejects_clipping() {
        let mut review = json!({"sources":[{"kind":"working","hash":"abc"},{"kind":"staged","hash":"def"}],"diffTruncated":false});
        assert_eq!(
            review_digest(&review),
            Some(digest(
                r#"[{"Kind":"working","Hash":"abc"},{"Kind":"staged","Hash":"def"}]"#
            ))
        );
        review["sources"][0]["truncated"] = json!(true);
        assert!(review_digest(&review).is_none());
    }
    #[test]
    fn ranking_preserves_native_tree_and_stable_ties() {
        let rows = [(0, 0), (1, 1), (2, 1), (3, 0), (4, 1)];
        let ranks = [2, 2, 1, 1, 0];
        assert_eq!(
            rank_tree(&rows, &|i| ranks[i]),
            vec![(3, 0), (4, 1), (0, 0), (2, 1), (1, 1)]
        );
    }
}
