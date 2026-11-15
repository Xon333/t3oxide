use super::*;

impl App {
    pub(super) fn begin_new(&mut self, purpose: NewPurpose) -> Result<()> {
        ensure!(
            purpose != NewPurpose::Batch,
            "Task filing is retained for recovery only; use New for a native conversation"
        );
        let snapshot = self.state.snapshot();
        let pin = &snapshot["creationDestination"];
        let profile = if let Some(p) = self.creation_profile {
            Some(p)
        } else if pin.is_object() {
            let p = self
                .environments
                .iter()
                .position(|e| {
                    e.id == string(pin, "profileId")
                        && e.environment_id == string(pin, "environmentId")
                })
                .context(
                    "Pinned creation destination is unavailable. Change or reset Destination.",
                )?;
            Some(p)
        } else {
            self.environments
                .iter()
                .position(|e| e.default_for_new)
                .or_else(|| (self.environments.len() == 1).then_some(0))
        };
        if let Some(profile) = profile {
            self.choose_project(profile, purpose)
        } else {
            self.menu(
                "Create in environment",
                "Choose the exact source. This is independent of the highlighted conversation.",
                self.environments
                    .iter()
                    .enumerate()
                    .map(|(i, e)| {
                        Choice::new(
                            e.label.clone(),
                            e.environment_id.clone(),
                            Intent::NewEnvironment(i, purpose),
                        )
                    })
                    .collect(),
            );
            Ok(())
        }
    }
    pub(super) fn choose_project(&mut self, profile: usize, purpose: NewPurpose) -> Result<()> {
        self.notice = format!("Reading projects on {}…", self.environments[profile].label);
        self.source_read(
            profile,
            "projects",
            json!({"limit":100}),
            ReadKind::Projects { profile, purpose },
        )
    }
    pub(super) fn choose_project_result(
        &mut self,
        profile: usize,
        project: Value,
        purpose: NewPurpose,
    ) -> Result<()> {
        self.choose_project_form_result(profile, project, purpose, None)
    }
    pub(super) fn choose_project_form_result(
        &mut self,
        profile: usize,
        project: Value,
        purpose: NewPurpose,
        restored_form_key: Option<String>,
    ) -> Result<()> {
        ensure!(
            domain::valid_id(&string(&project, "id")),
            "Project identity is invalid"
        );
        if purpose == NewPurpose::Destination {
            let env = &self.environments[profile];
            self.state.update(|v|{v["creationDestination"]=json!({"profileId":env.id,"environmentId":env.environment_id,"projectId":project["id"]});Ok(())})?;
            self.notice = format!(
                "New will use {} / {} with fresh native defaults.",
                env.label,
                string(&project, "title")
            );
            return Ok(());
        }
        let imported = if purpose == NewPurpose::Batch {
            None
        } else {
            self.imported_launch(profile, &project)
        };
        let explicit_import = self.take_import_request();
        ensure!(
            !explicit_import || imported.is_some(),
            "Saved launch destination changed; choose its original source"
        );
        let snapshot = self.state.snapshot();
        let form_key = restored_form_key.unwrap_or_else(|| self.launch_form_key(profile, &project));
        let saved = &snapshot["rustLaunchForms"][&form_key];
        ensure!(
            !saved["batchBinding"].is_object(),
            "Saved task forms are retained for readback and recovery only"
        );
        ensure!(
            purpose != NewPurpose::Batch,
            "Task filing is retained for recovery only"
        );
        let restore = !explicit_import && saved["projectId"] == project["id"];
        let previous = self
            .launch
            .take()
            .filter(|l| !explicit_import && l.profile == profile && l.form_key == form_key);
        let (title, message, selection, workspace, mut batch) = if let Some(l) = previous {
            (l.title, l.message, l.selection, l.workspace, l.batch)
        } else if restore {
            (
                string(saved, "title"),
                string(saved, "message"),
                saved["selection"].clone(),
                saved["workspace"].as_str().map(str::to_owned),
                None,
            )
        } else if let Some(saved) = &imported {
            (
                string(saved, "title"),
                string(saved, "message"),
                saved["selection"].clone(),
                saved["workspaceStrategy"]["type"].as_str().map(|kind| {
                    if kind == "worktree" {
                        "worktree".into()
                    } else {
                        "local".into()
                    }
                }),
                None,
            )
        } else {
            (String::new(), String::new(), Value::Null, None, None)
        };
        if restore {
            self.restore_launch_bindings(profile, &project, saved);
            if let Some(binding) = &self.workspace.batch {
                batch = Some((binding.before.clone(), binding.consumed.clone()));
            }
        }
        self.launch = Some(Launch {
            profile,
            form_key,
            project: project.clone(),
            defaults: Value::Null,
            title,
            message,
            selection,
            workspace,
            batch,
        });
        if purpose == NewPurpose::NativeDefaults {
            return self.native_defaults();
        }
        if purpose == NewPurpose::Batch {
            let source = self.batch_source(profile, &project);
            let draft = self
                .state
                .draft(&self.environments[profile].environment_id, &source);
            self.text_dialog("File multiple tasks","One human task per line. Each task receives its own explicit native launch review; this is not a background queue.",InputTarget::Batch,draft);
            return Ok(());
        }
        self.load_defaults()
    }
    pub(super) fn load_defaults(&mut self) -> Result<()> {
        let l = self.launch.as_ref().context("Launch form is unavailable")?;
        let mut fields = json!({"projectId":l.project["id"],"isolated":false});
        if let Some(mode) = &l.workspace {
            fields["workspaceMode"] = json!(mode);
        }
        self.source_read(
            l.profile,
            "launch-defaults",
            fields,
            ReadKind::Defaults {
                profile: l.profile,
                project: l.project.clone(),
            },
        )?;
        self.notice = "Resolving target-native defaults…".into();
        Ok(())
    }
    pub(super) fn read_result(&mut self, kind: ReadKind, value: Value) -> Result<()> {
        match kind {
            ReadKind::Projects { profile, purpose } => {
                let projects = array(&value, "projects");
                ensure!(projects.len() <= 200, "Native project page exceeds bounds");
                let snapshot = self.state.snapshot();
                let pin = &snapshot["creationDestination"];
                let same = if purpose == NewPurpose::Same {
                    string(&self.metadata(), "projectId")
                } else {
                    String::new()
                };
                let pinned = if pin["profileId"] == self.environments[profile].id {
                    string(pin, "projectId")
                } else {
                    String::new()
                };
                let target = if !same.is_empty() {
                    same
                } else if !matches!(
                    purpose,
                    NewPurpose::Destination | NewPurpose::NativeDefaults
                ) {
                    pinned
                } else {
                    String::new()
                };
                if let Some(project) = projects.iter().find(|p| p["id"] == target) {
                    return self.choose_project_result(profile, project.clone(), purpose);
                }
                let mut choices = Vec::new();
                for p in projects {
                    ensure!(
                        domain::valid_id(&string(p, "id")),
                        "Invalid native project ID"
                    );
                    choices.push(Choice::new(
                        string(p, "title"),
                        string(p, "workspaceRoot"),
                        Intent::Project(profile, p.clone(), purpose),
                    ));
                }
                if let Some(cursor) = value["nextCursor"].as_u64() {
                    choices.push(Choice::new(
                        "Next projects…",
                        "Load the next native project page",
                        Intent::ProjectsMore(profile, cursor, purpose),
                    ));
                }
                self.menu(format!("Projects on {}",self.environments[profile].label),if target.is_empty(){"Choose an existing native project."}else{"The saved project was not on this page. Choose it explicitly; no automatic substitution."},choices);
            }
            ReadKind::Defaults { profile, project } => {
                ensure!(
                    value["projectId"] == project["id"]
                        && value["revision"].is_string()
                        && value["workspaceRoot"].is_string(),
                    "Native defaults changed their project identity"
                );
                let l = self.launch.as_mut().context("Launch review expired")?;
                ensure!(
                    l.profile == profile && l.project["id"] == project["id"],
                    "Launch destination changed"
                );
                if l.selection.is_null() {
                    l.selection = value["modelSelection"].clone();
                }
                // A staged selection is kept only on its original provider. No account fallback.
                ensure!(
                    l.selection["instanceId"] == value["modelSelection"]["instanceId"],
                    "Native provider changed; review the new defaults before using this saved selection"
                );
                l.defaults = value;
                self.save_launch()?;
                self.launch_menu()?;
            }
            kind @ (ReadKind::Models(scope) | ReadKind::ModelOption(scope, _)) => {
                let a = self.active.as_ref().context("Conversation closed")?;
                ensure!(
                    value["threadId"] == a.thread,
                    "Model catalog belongs to another conversation"
                );
                ensure!(
                    flag(&value, "available"),
                    "Native model provider unavailable"
                );
                let desired = self
                    .model_edit
                    .take()
                    .filter(|edit| edit.catalog.is_null())
                    .map(|edit| edit.selection);
                let next_override = if scope == ModelScope::Next {
                    let key = format!(
                        "{}/{}",
                        self.environments[a.profile].environment_id, a.thread
                    );
                    self.state.snapshot()["modelOverrides"][&key]
                        .get("selection")
                        .cloned()
                } else {
                    None
                };
                let selection = desired
                    .or(next_override)
                    .unwrap_or_else(|| value["current"].clone());
                ensure!(
                    selection["instanceId"] == value["current"]["instanceId"],
                    "Favorite belongs to another native provider instance"
                );
                self.model_edit = Some(ModelEdit {
                    source: Some(workspace::ViewSource::active(self)?),
                    scope,
                    original: value["current"].clone(),
                    selection,
                    catalog: value,
                });
                if let ReadKind::ModelOption(_, option) = kind {
                    self.model_option_menu(&option)?;
                } else {
                    self.model_menu()?;
                }
            }
            ReadKind::Review => self.read_review(value)?,
            ReadKind::Activity => {
                let a = self.active.as_ref().context("Conversation closed")?;
                ensure!(
                    value["threadId"] == a.thread,
                    "Activity belongs to a different conversation"
                );
                let mut choices = array(&value["activity"], "items")
                    .iter()
                    .map(|item| {
                        Choice::new(
                            format!("{} · {}", string(item, "title"), string(item, "status")),
                            string(item, "summary"),
                            Intent::Inspect(item.clone()),
                        )
                    })
                    .collect::<Vec<_>>();
                if let Some(cursor) = value["activity"]["historyCursor"].as_str() {
                    choices.push(Choice::new(
                        "Earlier activity",
                        "Load a bounded historical page",
                        Intent::ActivityEarlier(cursor.into()),
                    ));
                }
                self.menu(
                    "Tools and activity",
                    "Each item keeps its exact native source thread and item identity.",
                    choices,
                );
            }
            ReadKind::Inspect => self.read_inspection(value)?,
            ReadKind::Preview => {
                ensure!(
                    flag(&value, "readOnly"),
                    "Preview response did not establish read-only scope"
                );
                let view = &value["view"];
                let text = array(view, "items")
                    .iter()
                    .map(|r| format!("{}\n{}", string(r, "kind"), string(r, "text")))
                    .collect::<Vec<_>>()
                    .join("\n\n");
                self.detail(format!("Preview · {}",string(view,"title")),format!("Read-only preview. Active composer and Stop target remain unchanged.\n\n{text}"));
            }
            ReadKind::NativeDefaults => {
                let mut choices = vec![
                    Choice::new(
                        "Project defaults",
                        "Show overrides and inherited values",
                        Intent::NativeScope("project".into()),
                    ),
                    Choice::new(
                        "Environment defaults",
                        "Show native environment settings",
                        Intent::NativeScope("environment".into()),
                    ),
                ];
                choices.push(Choice::new("Close", "", Intent::Close));
                self.menu("Native defaults","Persistent default edits remain in native T3 Settings because its current API lacks safe revision-bound writes.",choices);
                if let Some(d) = &mut self.dialog {
                    d.detail = serde_json::to_string_pretty(&value)?;
                }
            }
            ReadKind::History { navigation } => {
                if !self.navigation_ack(navigation) {
                    return Ok(());
                }
                self.notice =
                    "Earlier native page · live detail paused · ] returns to Latest".into();
                self.transcript_scroll = 0;
                self.follow = false;
            }
            ReadKind::Watch { navigation } => {
                if !self.navigation_ack(navigation) {
                    return Ok(());
                }
                self.notice =
                    "Attached to native conversation. Viewing never marks it reviewed.".into();
            }
        }
        self.dirty = true;
        Ok(())
    }

    pub(super) fn save_launch(&self) -> Result<()> {
        if let Some(l) = &self.launch {
            let key = l.form_key.clone();
            if l.title.is_empty()
                && l.message.is_empty()
                && l.workspace.is_none()
                && l.batch.is_none()
                && l.selection == l.defaults["modelSelection"]
                && self.state.snapshot()["rustLaunchForms"].get(&key).is_none()
            {
                return Ok(());
            }
            let mut saved = json!({"projectId":l.project["id"],"title":l.title,"message":l.message,"selection":l.selection,"workspace":l.workspace,"defaultsRevision":l.defaults["revision"]});
            self.save_launch_bindings(l, &mut saved)?;
            self.state.update(|v| {
                if !v["rustLaunchForms"].is_object() {
                    v["rustLaunchForms"] = json!({});
                }
                let revision = v["rustLaunchForms"][&key]["revision"].as_u64().unwrap_or(0);
                saved["revision"] = json!(
                    revision
                        .checked_add(1)
                        .context("Launch form revision exhausted")?
                );
                v["rustLaunchForms"][&key] = saved;
                Ok(())
            })?;
        }
        Ok(())
    }
    pub(super) fn launch_menu(&mut self) -> Result<()> {
        let l = self.launch.as_ref().context("Launch review expired")?;
        let choices = vec![
            Choice::new(
                format!(
                    "Title: {}",
                    if l.title.is_empty() {
                        "(required)"
                    } else {
                        &l.title
                    }
                ),
                "Edit the native conversation title",
                Intent::Edit(InputTarget::LaunchTitle),
            ),
            Choice::new(
                format!(
                    "Message: {}",
                    l.message.lines().next().unwrap_or("(empty thread)")
                ),
                "Edit initial human message; empty filing is supported",
                Intent::Edit(InputTarget::LaunchMessage),
            ),
            Choice::new(
                format!("Model: {}", string(&l.selection, "model")),
                "Model, effort and speed for this launch",
                Intent::LaunchModels,
            ),
            Choice::new(
                format!("Workspace: {}", string(&l.defaults, "workspaceMode")),
                "T3 creates the isolated worktree or uses the explicitly selected root",
                Intent::LaunchWorkspace,
            ),
            Choice::new(
                "Pin this destination",
                "Use this exact environment/project for New",
                Intent::PinDestination,
            ),
            Choice::new(
                "Refresh native defaults",
                "Explicitly reset staged model and workspace to current project defaults",
                Intent::Workspace(workspace::WorkspaceAction::ResetLaunchDefaults),
            ),
            Choice::new(
                "Create conversation",
                "Submit this reviewed native launch once",
                Intent::LaunchCreate,
            ),
            Choice::new("Back", "All form edits remain durable", Intent::Close),
        ];
        self.menu(
            format!(
                "New · {} / {}",
                self.environments[l.profile].label,
                string(&l.project, "title")
            ),
            format!(
                "Native defaults · {} · {}. Create is the only action that launches work.",
                string(&l.defaults, "runtimeMode"),
                string(&l.defaults, "interactionMode")
            ),
            choices,
        );
        Ok(())
    }
    pub(super) fn edit_field(&mut self, target: InputTarget) -> Result<()> {
        match target {
            InputTarget::LaunchTitle => {
                let text = self
                    .launch
                    .as_ref()
                    .context("Launch form expired")?
                    .title
                    .clone();
                self.text_dialog(
                    "Conversation title",
                    "Enter a short title; saving returns to the launch review.",
                    target,
                    text,
                );
            }
            InputTarget::LaunchMessage => {
                let text = self
                    .launch
                    .as_ref()
                    .context("Launch form expired")?
                    .message
                    .clone();
                self.text_dialog("Initial message","Enter inserts a newline. Empty creates a filed conversation without starting a run.",target,text);
            }
            InputTarget::Question => {
                let edit = self
                    .request_edit
                    .as_ref()
                    .context("Question form expired")?;
                let q = array(&edit.request, "questions")
                    .get(edit.index)
                    .context("Choose a question to edit")?;
                let text = custom_answer_text(q, edit.answers.get(&string(q, "id")));
                self.text_dialog(&string(q, "header"), &string(q, "question"), target, text);
            }
            InputTarget::Command => self.text_dialog(
                "Command",
                "Type a supported /command.",
                target,
                String::new(),
            ),
            _ => bail!("Input form is unavailable"),
        }
        Ok(())
    }
    pub(super) fn preserve_form_edit(&mut self, target: &InputTarget, text: &str) -> Result<()> {
        ensure!(
            text.len() <= 64 * 1024 && text.lines().count() <= 9999,
            "Input exceeds the editor limit"
        );
        match target {
            InputTarget::LaunchTitle => {
                ensure!(text.len() <= 512, "Title exceeds 512 bytes");
                self.launch.as_mut().context("Launch form expired")?.title = text.to_owned();
                self.save_launch()?;
            }
            InputTarget::LaunchMessage => {
                self.launch.as_mut().context("Launch form expired")?.message = text.to_owned();
                self.save_launch()?;
            }
            InputTarget::Batch => {
                ensure!(text.len() <= 32 * 1024, "Task list exceeds 32 KiB");
                let l = self.launch.as_ref().context("Task destination expired")?;
                let source = self.batch_source(l.profile, &l.project);
                self.state.set_draft(
                    &self.environments[l.profile].environment_id,
                    &source,
                    text,
                )?;
            }
            InputTarget::Question => {
                let edit = self
                    .request_edit
                    .as_mut()
                    .context("Question form expired")?;
                let q = array(&edit.request, "questions")
                    .get(edit.index)
                    .context("Choose a question to edit")?;
                let id = string(q, "id");
                let custom = flag(q, "allowCustomAnswer") || array(q, "options").is_empty();
                ensure!(custom, "Custom answer is not offered by T3");
                let answer = replace_custom_answer(q, edit.answers.get(&id), text);
                edit.answers.insert(id, answer);
            }
            _ => {}
        }
        self.save_question_draft()?;
        Ok(())
    }
    pub(super) fn submit_text(&mut self, target: InputTarget) -> Result<()> {
        let text = self.modal_editor.lines().join("\n");
        self.preserve_form_edit(&target, &text)?;
        self.dialog = None;
        match target {
            InputTarget::Search => {
                self.filter = text;
                self.refresh(false, true)?;
            }
            InputTarget::LoadedSearch => self.loaded_search(&text)?,
            InputTarget::LaunchTitle | InputTarget::LaunchMessage => self.launch_menu()?,
            InputTarget::Batch => self.prepare_batch(&text)?,
            InputTarget::Question => self.question_next()?,
            InputTarget::Command => self.command(&text)?,
        }
        Ok(())
    }
    pub(super) fn toggle_workspace(&mut self) -> Result<()> {
        let l = self.launch.as_mut().context("Launch review expired")?;
        l.workspace = Some(if string(&l.defaults, "workspaceMode") == "worktree" {
            "local".into()
        } else {
            "worktree".into()
        });
        self.save_launch()?;
        self.load_defaults()
    }
    pub(super) fn create_launch(&mut self) -> Result<()> {
        self.save_launch()?;
        let l = self
            .launch
            .as_ref()
            .context("Launch review expired")?
            .clone();
        ensure!(
            !l.title.trim().is_empty() && l.title.len() <= 512,
            "A title is required"
        );
        ensure!(l.message.len() <= 64 * 1024, "Message exceeds 64 KiB");
        ensure!(
            l.defaults["revision"].is_string(),
            "Review native defaults before creating"
        );
        validate_selection(&l.selection, &l.defaults["catalog"])?;
        let env = &self.environments[l.profile];
        if let Some(allowed) = &env.allowed_provider_instance_ids {
            ensure!(
                allowed.contains(&string(&l.selection, "instanceId")),
                "Native provider is not allowed by this source profile"
            );
        }
        let thread = wire::new_id();
        let mut fields = json!({"threadId":thread,"newThreadId":thread,"projectId":l.project["id"],"title":l.title,"message":l.message,"modelSelection":l.selection,
            "workspaceStrategy":l.defaults["workspaceStrategy"],"workspaceRoot":l.defaults["workspaceRoot"],"workspaceMode":l.workspace.clone().unwrap_or_default(),"isolated":false,"defaultsRevision":l.defaults["revision"]});
        if l.workspace.is_none() {
            fields.as_object_mut().unwrap().remove("workspaceMode");
        }
        self.bind_launch_submission(&l, &mut fields)?;
        self.mutate(l.profile, "launch-project", fields, None)
    }
    pub(super) fn pin_destination(&mut self) -> Result<()> {
        let l = self
            .launch
            .as_ref()
            .context("Launch destination unavailable")?;
        let env = &self.environments[l.profile];
        self.state.update(|v|{v["creationDestination"]=json!({"profileId":env.id,"environmentId":env.environment_id,"projectId":l.project["id"]});Ok(())})?;
        self.notice = "Creation destination pinned. Native defaults remain authoritative.".into();
        self.launch_menu()
    }
    pub(super) fn destination_menu(&mut self) -> Result<()> {
        let pin = self.state.snapshot()["creationDestination"].clone();
        let mut choices = self
            .environments
            .iter()
            .enumerate()
            .map(|(i, e)| {
                Choice::new(
                    format!("Choose project on {}", e.label),
                    e.environment_id.clone(),
                    Intent::NewEnvironment(i, NewPurpose::Destination),
                )
            })
            .collect::<Vec<_>>();
        choices.push(Choice::new(
            "Reset destination",
            "Use explicit CLI target, defaultForNew, or choose",
            Intent::ResetDestination,
        ));
        self.menu(
            "Creation destination",
            if pin.is_object() {
                format!(
                    "Current: {} / {}",
                    string(&pin, "profileId"),
                    string(&pin, "projectId")
                )
            } else {
                "No saved destination".into()
            },
            choices,
        );
        Ok(())
    }
    pub(super) fn native_defaults(&mut self) -> Result<()> {
        let l = self
            .launch
            .as_ref()
            .context("Choose a native project first")?;
        self.source_read(
            l.profile,
            "native-defaults",
            json!({"projectId":l.project["id"],"scope":self.native_scope}),
            ReadKind::NativeDefaults,
        )
    }

    pub(super) fn launch_models(&mut self) -> Result<()> {
        let l = self.launch.as_ref().context("Launch review expired")?;
        self.model_edit = Some(ModelEdit {
            source: None,
            scope: ModelScope::Launch,
            catalog: l.defaults["catalog"].clone(),
            original: l.defaults["modelSelection"].clone(),
            selection: l.selection.clone(),
        });
        self.model_menu()
    }
    pub(super) fn model_menu(&mut self) -> Result<()> {
        let edit = self
            .model_edit
            .as_ref()
            .context("Model selection expired")?;
        let selected = string(&edit.selection, "model");
        let mut choices = Vec::new();
        for (i, model) in array(&edit.catalog, "models").iter().enumerate() {
            let name = string(model, "name");
            let mut choice = Choice::new(
                if name.is_empty() {
                    string(model, "slug")
                } else {
                    name
                },
                "",
                Intent::Model(i),
            );
            choice.checked = model["slug"] == selected;
            choices.push(choice);
        }
        if let Some(model) = array(&edit.catalog, "models")
            .iter()
            .find(|model| model["slug"] == selected)
        {
            for option in array(model, "options") {
                let id = string(option, "id");
                choices.push(Choice::new(
                    format!(
                        "{}: {}",
                        string(option, "label"),
                        option_value_label(&edit.selection, option)
                    ),
                    "",
                    Intent::Option(id),
                ));
            }
        }
        // Saved favorites still need an explicit application after catalog validation.
        if edit.selection != edit.original {
            choices.push(Choice::new("Apply selection", "", Intent::ModelApply));
        }
        if edit.scope == ModelScope::Next {
            choices.push(Choice::new(
                "Use conversation model",
                "",
                Intent::ClearOverride,
            ));
        }
        self.menu(
            if edit.scope == ModelScope::Next {
                "Next message model"
            } else {
                "Model"
            },
            if flag(&edit.catalog, "requiresNewThreadForModelChange")
                && edit.scope != ModelScope::Launch
            {
                "Model changes require a new conversation."
            } else {
                ""
            },
            choices,
        );
        Ok(())
    }
    pub(super) fn choose_model(&mut self, index: usize) -> Result<()> {
        let edit = self
            .model_edit
            .as_mut()
            .context("Model selection expired")?;
        let model = array(&edit.catalog, "models")
            .get(index)
            .context("Model is no longer offered")?;
        if edit.scope != ModelScope::Launch
            && flag(&edit.catalog, "requiresNewThreadForModelChange")
        {
            ensure!(
                model["slug"] == edit.original["model"],
                "This provider requires a new conversation to change models"
            );
        }
        if edit.selection["model"] != model["slug"] {
            let selection = edit
                .selection
                .as_object_mut()
                .context("Invalid native model selection")?;
            selection.insert("model".into(), model["slug"].clone());
            selection.remove("options");
        }
        self.apply_model()
    }
    pub(super) fn model_option_menu(&mut self, kind: &str) -> Result<()> {
        let edit = self
            .model_edit
            .as_ref()
            .context("Model selection expired")?;
        let model = array(&edit.catalog, "models")
            .iter()
            .find(|model| model["slug"] == edit.selection["model"])
            .context("Model is not offered")?;
        let option = array(model, "options")
            .iter()
            .find(|option| option_matches(option, kind))
            .with_context(|| format!("This model does not offer {kind}"))?;
        let id = string(option, "id");
        self.choose_option(&id)
    }
    pub(super) fn choose_option(&mut self, id: &str) -> Result<()> {
        let edit = self
            .model_edit
            .as_ref()
            .context("Model selection expired")?;
        let model = array(&edit.catalog, "models")
            .iter()
            .find(|model| model["slug"] == edit.selection["model"])
            .context("Model is not offered")?;
        let option = array(model, "options")
            .iter()
            .find(|option| option["id"] == id)
            .context("Option is no longer offered")?;
        let selected = array(&edit.selection, "options")
            .iter()
            .find(|value| value["id"] == id)
            .map(|value| &value["value"]);
        let mut default = Choice::new("Default", "", Intent::OptionValue(id.into(), Value::Null));
        default.checked = selected.is_none();
        let mut choices = vec![default];
        let values = if option["type"] == "boolean" {
            vec![
                json!({"id":true,"label":"On"}),
                json!({"id":false,"label":"Off"}),
            ]
        } else {
            array(option, "options").to_vec()
        };
        for value in values {
            let mut choice = Choice::new(
                string(&value, "label"),
                string(&value, "description"),
                Intent::OptionValue(id.into(), value["id"].clone()),
            );
            choice.checked = selected == Some(&value["id"]);
            choices.push(choice);
        }
        let title = if option_matches(option, "effort") {
            "Effort".into()
        } else {
            string(option, "label")
        };
        self.menu(title, "", choices);
        Ok(())
    }
    pub(super) fn set_model_option(&mut self, id: &str, value: Value) -> Result<()> {
        let edit = self
            .model_edit
            .as_mut()
            .context("Model selection expired")?;
        let mut options = array(&edit.selection, "options").to_vec();
        options.retain(|option| option["id"] != id);
        if !value.is_null() {
            options.push(json!({"id":id,"value":value}));
        }
        edit.selection
            .as_object_mut()
            .context("Invalid native model selection")?
            .insert("options".into(), json!(options));
        self.apply_model()
    }
    pub(super) fn apply_model(&mut self) -> Result<()> {
        let edit = self
            .model_edit
            .as_ref()
            .context("Model review expired")?
            .clone();
        validate_selection(&edit.selection, &edit.catalog)?;
        if let Some(source) = &edit.source {
            source.check(self)?;
        }
        if edit.scope != ModelScope::Launch {
            let active = self.active.as_ref().context("Conversation closed")?;
            ensure!(
                edit.catalog["threadId"] == active.thread,
                "Model selection source changed; reopen the picker"
            );
        }
        match edit.scope {
            ModelScope::Launch => {
                self.launch
                    .as_mut()
                    .context("Launch review expired")?
                    .selection = edit.selection;
                self.save_launch()?;
                self.launch_menu()?;
            }
            ModelScope::Next => {
                let a = self.active.as_ref().context("Conversation closed")?;
                let key = format!(
                    "{}/{}",
                    self.environments[a.profile].environment_id, a.thread
                );
                self.state.update(|v| {
                    if !v["modelOverrides"].is_object() {
                        v["modelOverrides"] = json!({});
                    }
                    v["modelOverrides"][&key] =
                        json!({"id":wire::new_id(),"selection":edit.selection});
                    Ok(())
                })?;
                self.notice = "Next-message model saved".into();
            }
            ModelScope::Current => {
                let active = self.active.as_ref().context("Conversation closed")?;
                if edit.selection == edit.original {
                    self.notice = "Model unchanged".into();
                } else {
                    self.mutate(active.profile,"select-model",json!({"threadId":active.thread,"modelSelection":edit.selection,"expectedModelSelection":edit.original}),None)?;
                }
            }
        }
        Ok(())
    }
    pub(super) fn presets(&mut self) -> Result<()> {
        self.favorite_menu()
    }

    pub(super) fn ensure_request(&self, request: &Value) -> Result<()> {
        let a = self.active.as_ref().context("Conversation closed")?;
        ensure!(
            a.requests
                .iter()
                .any(|r| r["id"] == request["id"] && r["revision"] == request["revision"]),
            "Request changed; reopen Requests before answering"
        );
        ensure!(
            flag(request, "supported"),
            "{}",
            string(request, "limitation")
        );
        ensure!(
            request["id"].is_string() && request["revision"].is_string(),
            "Request identity is missing"
        );
        Ok(())
    }
    pub(super) fn review_request(&mut self, request: Value) -> Result<()> {
        self.ensure_request(&request)?;
        match string(&request, "kind").as_str() {
            "approval_request" => {
                let options = array(&request, "options");
                ensure!(
                    !options.is_empty() && options.len() <= 5,
                    "Approval must be reviewed in full T3"
                );
                let mut choices = Vec::new();
                for o in options {
                    let decision = string(o, "decision");
                    ensure!(
                        [
                            "accept",
                            "acceptForSession",
                            "acceptAlways",
                            "decline",
                            "cancel"
                        ]
                        .contains(&decision.as_str()),
                        "Unsupported native approval option"
                    );
                    choices.push(Choice::new(
                        string(o, "label"),
                        string(o, "warning"),
                        Intent::Approval(request.clone(), decision),
                    ));
                }
                self.menu("Native approval", string(&request, "prompt"), choices);
            }
            "user_input_request" => {
                validate_questions(&request)?;
                let active = self.active.as_ref().context("Conversation closed")?;
                let form = crate::attention::Form::from_request(
                    &self.environments[active.profile].environment_id,
                    &active.thread,
                    &request,
                )?;
                let restored = form.load_draft(&self.state)?;
                let mut answers = serde_json::Map::new();
                for q in array(&request, "questions") {
                    if let Some(answer) = q.get("initialAnswer").filter(|answer| {
                        answer.as_str().is_some_and(|s| !s.is_empty())
                            || answer.as_array().is_some_and(|a| !a.is_empty())
                    }) {
                        validate_answer(q, Some(answer))?;
                        answers.insert(string(q, "id"), answer.clone());
                    }
                }
                self.request_edit = Some(RequestEdit {
                    request,
                    index: 0,
                    answers: restored.unwrap_or(answers),
                });
                self.question_menu()?;
            }
            _ => bail!("This native request type requires full T3"),
        }
        Ok(())
    }
    pub(super) fn save_question_draft(&self) -> Result<()> {
        if let Some(edit) = &self.request_edit {
            let active = self.active.as_ref().context("Question source closed")?;
            crate::attention::Form::from_request(
                &self.environments[active.profile].environment_id,
                &active.thread,
                &edit.request,
            )?
            .save_draft(&self.state, &edit.answers)?;
        }
        Ok(())
    }
    pub(super) fn question_menu(&mut self) -> Result<()> {
        self.save_question_draft()?;
        let edit = self
            .request_edit
            .as_ref()
            .context("Question form expired")?;
        self.ensure_request(&edit.request)?;
        let qs = array(&edit.request, "questions");
        if edit.index >= qs.len() {
            let preview = answer_review(&edit.request, &edit.answers);
            self.menu(
                "Review answers",
                preview,
                vec![
                    Choice::new("Submit answers", "", Intent::QuestionSubmit),
                    Choice::new("Edit answers", "", Intent::QuestionBack),
                ],
            );
            return Ok(());
        }
        let q = &qs[edit.index];
        let id = string(q, "id");
        let answer = edit.answers.get(&id);
        let mut choices = Vec::new();
        for o in array(q, "options") {
            let value = if o["value"].is_string() {
                o["value"].clone()
            } else {
                o["label"].clone()
            };
            let mut c = Choice::new(
                string(o, "label"),
                string(o, "description"),
                Intent::QuestionOption(value.clone()),
            );
            c.checked = answer == Some(&value)
                || answer
                    .and_then(Value::as_array)
                    .is_some_and(|v| v.contains(&value));
            choices.push(c);
        }
        if flag(q, "multiSelect")
            && let Some(values) = answer.and_then(Value::as_array)
        {
            for value in values {
                if let Some(text) = value.as_str().filter(|text| !offered_answer(q, text)) {
                    let mut c = Choice::new(
                        format!("Custom: {text}"),
                        "Select to remove this custom answer",
                        Intent::QuestionOption(value.clone()),
                    );
                    c.checked = true;
                    choices.push(c);
                }
            }
        }
        if flag(q, "allowCustomAnswer") || array(q, "options").is_empty() {
            choices.push(Choice::new(
                "Write answer",
                custom_answer_text(q, answer),
                Intent::Edit(InputTarget::Question),
            ));
        }
        if flag(q, "multiSelect") || answer.is_some() || !q["required"].as_bool().unwrap_or(true) {
            choices.push(Choice::new(
                if !q["required"].as_bool().unwrap_or(true) && answer.is_none() {
                    "Skip"
                } else {
                    "Next"
                },
                "",
                Intent::QuestionNext,
            ));
        }
        if edit.index > 0 {
            choices.push(Choice::new("Back", "", Intent::QuestionBack));
        }
        self.menu(
            format!(
                "Question {} / {} · {}",
                edit.index + 1,
                qs.len(),
                string(q, "header")
            ),
            string(q, "question"),
            choices,
        );
        Ok(())
    }
    pub(super) fn question_option(&mut self, value: Value) -> Result<()> {
        let edit = self
            .request_edit
            .as_mut()
            .context("Question form expired")?;
        set_question_option(edit, value)?;
        self.question_menu()
    }
    pub(super) fn question_next(&mut self) -> Result<()> {
        let edit = self
            .request_edit
            .as_mut()
            .context("Question form expired")?;
        let qs = array(&edit.request, "questions");
        if edit.index < qs.len() {
            validate_answer(
                &qs[edit.index],
                edit.answers.get(&string(&qs[edit.index], "id")),
            )?;
            edit.index += 1;
        }
        self.question_menu()
    }
    pub(super) fn submit_questions(&mut self) -> Result<()> {
        let edit = self
            .request_edit
            .as_ref()
            .context("Question form expired")?
            .clone();
        self.save_question_draft()?;
        self.ensure_request(&edit.request)?;
        let active = self.active.as_ref().context("Question source closed")?;
        let form = crate::attention::Form::from_request(
            &self.environments[active.profile].environment_id,
            &active.thread,
            &edit.request,
        )?;
        form.ensure_current(
            &self.environments[active.profile].environment_id,
            &active.thread,
            &active.requests,
        )?;
        form.validate_answers(&edit.answers)?;
        for q in array(&edit.request, "questions") {
            validate_answer(q, edit.answers.get(&string(q, "id")))?;
        }
        let a = self.active.as_ref().unwrap();
        let fields = json!({"threadId":a.thread,"requestId":edit.request["id"],"revision":edit.request["revision"],"answers":edit.answers});
        self.mutate(a.profile, "answer", fields, None)?;
        self.request_edit = None;
        Ok(())
    }
}

fn option_matches(option: &Value, kind: &str) -> bool {
    let identity =
        format!("{} {}", string(option, "id"), string(option, "label")).to_ascii_lowercase();
    match kind {
        "effort" => {
            identity.contains("effort")
                || identity.contains("reasoning")
                || identity.contains("thinking")
        }
        "speed" => {
            identity.contains("speed")
                || identity.contains("servicetier")
                || identity.contains("service_tier")
        }
        _ => option["id"] == kind,
    }
}

fn option_value_label(selection: &Value, option: &Value) -> String {
    let Some(selected) = array(selection, "options")
        .iter()
        .find(|value| value["id"] == option["id"])
    else {
        return "Default".into();
    };
    array(option, "options")
        .iter()
        .find(|value| value["id"] == selected["value"])
        .map(|value| string(value, "label"))
        .filter(|label| !label.is_empty())
        .unwrap_or_else(|| match &selected["value"] {
            Value::String(value) => value.clone(),
            Value::Bool(true) => "On".into(),
            Value::Bool(false) => "Off".into(),
            value => value.to_string(),
        })
}

fn set_question_option(edit: &mut RequestEdit, value: Value) -> Result<()> {
    let question = array(&edit.request, "questions")
        .get(edit.index)
        .context("Question selection expired")?;
    let id = string(question, "id");
    if flag(question, "multiSelect") {
        let mut values = edit
            .answers
            .get(&id)
            .and_then(Value::as_array)
            .cloned()
            .unwrap_or_default();
        if let Some(index) = values.iter().position(|item| item == &value) {
            values.remove(index);
        } else {
            values.push(value);
        }
        edit.answers.insert(id, json!(values));
    } else {
        validate_answer(question, Some(&value))?;
        edit.answers.insert(id, value);
        edit.index += 1;
    }
    Ok(())
}

fn answer_review(request: &Value, answers: &serde_json::Map<String, Value>) -> String {
    array(request, "questions")
        .iter()
        .enumerate()
        .map(|(index, question)| {
            let header = string(question, "header");
            let heading = if header.is_empty() {
                string(question, "question")
            } else {
                header
            };
            let label = |answer: &Value| {
                let text = answer.as_str().unwrap_or_default();
                array(question, "options")
                    .iter()
                    .find(|option| {
                        option["value"]
                            .as_str()
                            .unwrap_or_else(|| option["label"].as_str().unwrap_or_default())
                            == text
                    })
                    .map(|option| string(option, "label"))
                    .unwrap_or_else(|| text.to_owned())
            };
            let answer = match answers.get(&string(question, "id")) {
                Some(Value::Array(values)) if !values.is_empty() => {
                    values.iter().map(label).collect::<Vec<_>>().join(", ")
                }
                Some(Value::String(value)) if !value.is_empty() => {
                    label(&Value::String(value.clone()))
                }
                _ => "Skipped".into(),
            };
            format!("{}. {heading}\n{answer}", index + 1)
        })
        .collect::<Vec<_>>()
        .join("\n\n")
}

fn validate_selection(selection: &Value, catalog: &Value) -> Result<()> {
    ensure!(
        flag(catalog, "available") && selection["instanceId"] == catalog["current"]["instanceId"],
        "Model catalog or provider instance changed"
    );
    let model = array(catalog, "models")
        .iter()
        .find(|m| m["slug"] == selection["model"])
        .context("Model is no longer offered")?;
    for option in array(selection, "options") {
        if selection["model"] == catalog["current"]["model"]
            && array(&catalog["current"], "options").contains(option)
        {
            continue;
        }
        let descriptor = array(model, "options")
            .iter()
            .find(|o| o["id"] == option["id"])
            .context("Model option is no longer offered")?;
        ensure!(
            if descriptor["type"] == "boolean" {
                option["value"].is_boolean()
            } else {
                array(descriptor, "options")
                    .iter()
                    .any(|v| v["id"] == option["value"])
            },
            "Model option value is no longer offered"
        );
    }
    Ok(())
}
fn validate_questions(request: &Value) -> Result<()> {
    let questions = array(request, "questions");
    ensure!(
        !questions.is_empty() && questions.len() <= 8,
        "Question form exceeds supported bounds"
    );
    let mut ids = HashSet::new();
    for q in questions {
        let id = string(q, "id");
        ensure!(
            domain::valid_id(&id) && id.len() <= 128 && ids.insert(id),
            "Question identities are missing or duplicated"
        );
        ensure!(
            array(q, "options").len() <= 24,
            "Question has too many options"
        );
        for flag_name in ["required", "multiSelect", "allowCustomAnswer"] {
            ensure!(
                q.get(flag_name).is_none_or(Value::is_boolean),
                "Unsupported question flag"
            );
        }
        for o in array(q, "options") {
            ensure!(
                o["label"]
                    .as_str()
                    .is_some_and(|s| !s.is_empty() && s.len() <= 256),
                "Unsupported question option"
            );
        }
    }
    Ok(())
}
fn offered_answer(q: &Value, text: &str) -> bool {
    array(q, "options").iter().any(|o| {
        o["value"]
            .as_str()
            .unwrap_or_else(|| o["label"].as_str().unwrap_or(""))
            == text
    })
}
fn custom_answer_text(q: &Value, answer: Option<&Value>) -> String {
    if flag(q, "multiSelect") && !array(q, "options").is_empty() {
        answer
            .and_then(Value::as_array)
            .map(|values| {
                values
                    .iter()
                    .filter_map(Value::as_str)
                    .filter(|text| !offered_answer(q, text))
                    .collect::<Vec<_>>()
                    .join("\n")
            })
            .unwrap_or_default()
    } else {
        answer.and_then(Value::as_str).unwrap_or("").into()
    }
}
fn replace_custom_answer(q: &Value, answer: Option<&Value>, text: &str) -> Value {
    if flag(q, "multiSelect") && !array(q, "options").is_empty() {
        let mut values: Vec<Value> = answer
            .and_then(Value::as_array)
            .into_iter()
            .flatten()
            .filter(|value| value.as_str().is_some_and(|text| offered_answer(q, text)))
            .cloned()
            .collect();
        if !text.trim().is_empty() && !values.iter().any(|value| value == text) {
            values.push(json!(text));
        }
        json!(values)
    } else {
        json!(text)
    }
}
fn validate_answer(q: &Value, answer: Option<&Value>) -> Result<()> {
    let required = q["required"].as_bool().unwrap_or(true);
    let header = string(q, "header");
    let Some(answer) = answer else {
        ensure!(!required, "{header} requires an answer");
        return Ok(());
    };
    let options = array(q, "options");
    let allowed = |s: &str| {
        options.iter().any(|o| {
            o["value"]
                .as_str()
                .unwrap_or_else(|| o["label"].as_str().unwrap_or(""))
                == s
        })
    };
    let custom = flag(q, "allowCustomAnswer") || options.is_empty();
    if flag(q, "multiSelect") && !options.is_empty() {
        let values = answer
            .as_array()
            .context("This question needs a selection list")?;
        ensure!(
            !required || !values.is_empty(),
            "{header} requires an answer"
        );
        let mut seen = HashSet::new();
        ensure!(values.len() <= 32, "Too many answer selections");
        for v in values {
            let s = v.as_str().context("Invalid answer type")?;
            ensure!(
                s.len() <= 64 * 1024
                    && !s.trim().is_empty()
                    && (allowed(s) || custom)
                    && seen.insert(s),
                "Answer was not offered, is empty, too large or duplicated"
            );
        }
    } else {
        let s = answer.as_str().context("This question needs text")?;
        ensure!(
            !required || !s.trim().is_empty(),
            "{header} requires an answer"
        );
        ensure!(
            s.len() <= 64 * 1024 && (custom || allowed(s)),
            "Answer was not offered by T3"
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn answers_respect_native_options_and_required_flags() {
        let q = json!({"id":"q","header":"Choice","options":[{"label":"A"},{"label":"B"}],"required":true});
        assert!(validate_answer(&q, None).is_err());
        assert!(validate_answer(&q, Some(&json!("A"))).is_ok());
        assert!(validate_answer(&q, Some(&json!("C"))).is_err());
    }
    #[test]
    fn multi_answers_cannot_duplicate() {
        let q = json!({"id":"q","multiSelect":true,"options":[{"label":"A"}]});
        assert!(validate_answer(&q, Some(&json!(["A", "A"]))).is_err());
        assert!(validate_answer(&q, Some(&json!(["A"]))).is_ok());
    }
    #[test]
    fn catalog_never_switches_accounts() {
        let c = json!({"available":true,"current":{"instanceId":"a","model":"m"},"models":[{"slug":"m"}]});
        assert!(validate_selection(&json!({"instanceId":"b","model":"m"}), &c).is_err());
    }
    #[test]
    fn custom_multi_answer_edits_retain_native_choices_and_replace_only_custom() {
        let q = json!({"id":"q","multiSelect":true,"allowCustomAnswer":true,"options":[{"label":"A"},{"label":"B"}]});
        let original = json!(["A", "old custom"]);
        assert_eq!(custom_answer_text(&q, Some(&original)), "old custom");
        let changed = replace_custom_answer(&q, Some(&original), "new custom");
        assert_eq!(changed, json!(["A", "new custom"]));
        assert!(validate_answer(&q, Some(&changed)).is_ok());
        assert_eq!(replace_custom_answer(&q, Some(&changed), ""), json!(["A"]));
    }
    #[test]
    fn single_choice_advances_but_multiple_choices_stay_editable() {
        let request = json!({"questions":[
            {"id":"single","options":[{"label":"Yes","value":"approved"}]},
            {"id":"multi","multiSelect":true,"options":[{"label":"A"},{"label":"B"}]}
        ]});
        let mut edit = RequestEdit {
            request,
            index: 0,
            answers: serde_json::Map::new(),
        };
        assert!(set_question_option(&mut edit, json!("unoffered")).is_err());
        assert_eq!(edit.index, 0);
        assert!(edit.answers.is_empty());
        set_question_option(&mut edit, json!("approved")).unwrap();
        assert_eq!(edit.index, 1);
        set_question_option(&mut edit, json!("A")).unwrap();
        set_question_option(&mut edit, json!("B")).unwrap();
        assert_eq!(edit.index, 1, "multiselect waits for explicit Next");
        assert_eq!(edit.answers["multi"], json!(["A", "B"]));
        set_question_option(&mut edit, json!("A")).unwrap();
        assert_eq!(edit.answers["multi"], json!(["B"]));
        edit.index = 2;
        assert!(set_question_option(&mut edit, json!("A")).is_err());
    }

    #[test]
    fn final_answer_review_uses_offered_labels_and_retains_custom_text() {
        let request = json!({"questions":[
            {"id":"single","header":"Decision","options":[{"label":"Proceed","value":"opaque-native-value"}]},
            {"id":"multi","header":"Items","multiSelect":true,"options":[{"label":"First","value":"a"}]},
            {"id":"custom","header":"Notes"},
            {"id":"optional","header":"Later","required":false}
        ]});
        let answers = json!({"single":"opaque-native-value","multi":["a","Custom"],"custom":"  résumé 🙂\n日本語  "});
        let review = answer_review(&request, answers.as_object().unwrap());
        assert!(review.contains("1. Decision\nProceed"));
        assert!(review.contains("2. Items\nFirst, Custom"));
        assert!(review.contains("3. Notes\n  résumé 🙂\n日本語  "));
        assert!(review.contains("4. Later\nSkipped"));
        assert!(!review.contains("opaque-native-value"));
    }

    #[test]
    fn native_option_labels_and_direct_matching_use_catalog_descriptors() {
        let descriptor = json!({"id":"reasoningEffort","label":"Reasoning","options":[
            {"id":"xhigh","label":"Extra high"}
        ]});
        assert!(option_matches(&descriptor, "effort"));
        assert!(!option_matches(&descriptor, "speed"));
        assert!(option_matches(
            &json!({"id":"serviceTier","label":"Speed"}),
            "speed"
        ));
        assert_eq!(
            option_value_label(&json!({"options":[]}), &descriptor),
            "Default"
        );
        assert_eq!(
            option_value_label(
                &json!({"options":[{"id":"reasoningEffort","value":"xhigh"}]}),
                &descriptor
            ),
            "Extra high"
        );
    }
}
