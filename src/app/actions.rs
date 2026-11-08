use super::*;
use time::{OffsetDateTime, format_description::well_known::Rfc3339};

impl App {
    fn preserve_dialog(&mut self) -> bool {
        if self.dialog.is_none() {
            return true;
        }
        if let Err(error) = self.preserve_open_form() {
            self.error(error);
            return false;
        }
        true
    }

    pub(super) fn menu(
        &mut self,
        title: impl Into<String>,
        description: impl Into<String>,
        choices: Vec<Choice>,
    ) {
        if !self.preserve_dialog() {
            return;
        }
        self.invalidate_context_view();
        self.dialog = Some(Dialog {
            title: title.into(),
            description: description.into(),
            detail: String::new(),
            choices,
            selected: 0,
            scroll: 0,
            kind: DialogKind::Menu,
            footer: "↑↓ choose · Enter select · Esc back · Ctrl+S Stop".into(),
        });
        self.dirty = true;
    }
    pub(super) fn detail(&mut self, title: impl Into<String>, text: impl Into<String>) {
        if !self.preserve_dialog() {
            return;
        }
        self.invalidate_context_view();
        let title = title.into();
        let text = text.into();
        self.capture_detail(&title, &text);
        self.dialog = Some(Dialog {
            title,
            description: String::new(),
            detail: plain(&text),
            choices: Vec::new(),
            selected: 0,
            scroll: 0,
            kind: DialogKind::Detail,
            footer: "↑↓ / PageUp / PageDown scroll · Esc close · Copy via commands".into(),
        });
        self.dirty = true;
    }
    pub(super) fn text_dialog(
        &mut self,
        title: &str,
        description: &str,
        target: InputTarget,
        text: String,
    ) {
        if !self.preserve_dialog() {
            return;
        }
        self.invalidate_context_view();
        self.modal_editor = ui::editor(&text, &self.theme, "Type here…");
        self.dialog = Some(Dialog {
            title: title.into(),
            description: description.into(),
            detail: String::new(),
            choices: Vec::new(),
            selected: 0,
            scroll: 0,
            kind: DialogKind::Text(target),
            footer: "Ctrl+] save · Esc back (edits retained for launch)".into(),
        });
        self.dirty = true;
    }
    pub(super) fn palette(&mut self) {
        if !self.preserve_dialog() {
            return;
        }
        self.invalidate_context_view();
        self.modal_editor = ui::editor("", &self.theme, "Find an action…");
        self.dialog = Some(Dialog {
            title: "Commands".into(),
            description: input_guidance(),
            detail: String::new(),
            choices: Vec::new(),
            selected: 0,
            scroll: 0,
            kind: DialogKind::Palette,
            footer: "Type to filter · ↑↓ choose · Enter run · Esc close".into(),
        });
        self.filter_palette();
        self.dirty = true;
    }
    pub(super) fn switcher(&mut self) {
        if !self.preserve_dialog() {
            return;
        }
        self.invalidate_context_view();
        self.modal_editor = ui::editor("", &self.theme, "Find a loaded conversation…");
        self.dialog = Some(Dialog {
            title: "Switch conversation".into(),
            description: String::new(),
            detail: String::new(),
            choices: Vec::new(),
            selected: 0,
            scroll: 0,
            kind: DialogKind::Switcher,
            footer: "Type to filter · ↑↓ choose · Enter open tab · Esc close".into(),
        });
        self.filter_palette();
    }

    pub(super) fn refresh_switcher(&mut self) {
        let selected = self.dialog.as_ref().and_then(|dialog| {
            dialog.choices.get(dialog.selected).and_then(|choice| {
                if let Intent::Open(profile, thread) = &choice.intent {
                    Some((*profile, thread.clone()))
                } else {
                    None
                }
            })
        });
        self.filter_palette();
        if let Some((profile, thread)) = selected
            && let Some(dialog) = &mut self.dialog
        {
            dialog.selected = dialog.choices.iter().position(|choice| {
                matches!(&choice.intent, Intent::Open(p, id) if *p == profile && id == &thread)
            }).unwrap_or(0);
        }
    }

    pub(super) fn filter_palette(&mut self) {
        use nucleo_matcher::{
            Config, Matcher, Utf32Str,
            pattern::{AtomKind, CaseMatching, Normalization, Pattern},
        };
        let query = self.modal_editor.lines().join(" ");
        let pattern = Pattern::new(
            &query,
            CaseMatching::Ignore,
            Normalization::Smart,
            AtomKind::Fuzzy,
        );
        let mut matcher = Matcher::new(Config::DEFAULT);
        let mut scratch = Vec::new();
        if self
            .dialog
            .as_ref()
            .is_some_and(|d| matches!(d.kind, DialogKind::Switcher))
        {
            let mut matches: Vec<_> = self
                .threads
                .iter()
                .enumerate()
                .filter_map(|(index, thread)| {
                    let env = self.environments.get(thread.profile)?;
                    let label = format!("{} · {} · {}", thread.title, thread.project, env.label);
                    let description = format!(
                        "Environment: {} · profile: {}\nProject: {}\nThread: {}",
                        env.environment_id,
                        env.id,
                        if thread.project_id.is_empty() {
                            "unavailable"
                        } else {
                            &thread.project_id
                        },
                        thread.id,
                    );
                    let text = format!("{label} {description}");
                    pattern
                        .score(Utf32Str::new(&text, &mut scratch), &mut matcher)
                        .map(|score| {
                            (
                                score,
                                index,
                                Choice::new(
                                    label,
                                    description,
                                    Intent::Open(thread.profile, thread.id.clone()),
                                ),
                            )
                        })
                })
                .collect();
            matches.sort_by_key(|(score, index, _)| (std::cmp::Reverse(*score), *index));
            let mut labels = HashMap::new();
            for (_, _, choice) in &matches {
                *labels.entry(choice.label.clone()).or_insert(0) += 1;
            }
            for (_, index, choice) in &mut matches {
                if labels[&choice.label] > 1 {
                    let thread = &self.threads[*index];
                    let env = &self.environments[thread.profile];
                    choice.label = format!(
                        "{}/{}/{} · {}",
                        env.environment_id, thread.project_id, thread.id, choice.label
                    );
                }
            }
            let mut qualifications = Vec::new();
            for (env, host) in self.environments.iter().zip(&self.hosts) {
                let status = if !host.error.is_empty() {
                    Some("unavailable; incomplete")
                } else if host.loading {
                    Some("loading; incomplete")
                } else if host.page.observed_at.is_empty() {
                    Some("not loaded; incomplete")
                } else if host.page.more
                    || host.page.next_cursor.is_some()
                    || host.windowed
                    || host.page.search
                    || host.page.navigation_pins_only
                    || !host.page.inclusive_children
                {
                    Some("incomplete")
                } else {
                    None
                };
                if let Some(status) = status {
                    qualifications
                        .push(format!("{} [{}]: {status}", env.label, env.environment_id));
                }
            }
            let coverage = if self.environments.is_empty()
                || self.hosts.iter().any(|host| !host.error.is_empty())
            {
                "Unavailable sources; incomplete · "
            } else if self.hosts.iter().any(|host| host.loading) {
                "Loading; incomplete · "
            } else if !qualifications.is_empty() {
                "Incomplete · "
            } else {
                ""
            };
            let mut description = format!(
                "{coverage}Loaded conversations only · {} matches. /search uses native server search.",
                matches.len()
            );
            if self.environments.is_empty() {
                description.push_str(" No environments loaded; inventory unavailable.");
            }
            if !qualifications.is_empty() {
                description.push('\n');
                description.push_str(&qualifications.join(" · "));
            }
            if let Some(dialog) = &mut self.dialog {
                dialog.choices = matches.into_iter().map(|(_, _, choice)| choice).collect();
                dialog.selected = 0;
                dialog.scroll = 0;
                dialog.description = description;
            }
            self.dirty = true;
            return;
        }
        let mut matches: Vec<_> = domain::ACTIONS
            .iter()
            .enumerate()
            .filter_map(|(index, action)| {
                let text = format!(
                    "{} {} {} {}",
                    action.name, action.label, action.description, action.key
                );
                pattern
                    .score(Utf32Str::new(&text, &mut scratch), &mut matcher)
                    .map(|score| (score, index, action))
            })
            .collect();
        matches.sort_by_key(|(score, index, _)| (std::cmp::Reverse(*score), *index));
        let choices = matches
            .into_iter()
            .map(|(_, _, action)| {
                Choice::new(
                    format!(
                        "{}{}",
                        action.label,
                        if action.key.is_empty() {
                            String::new()
                        } else {
                            format!("  [{}]", action.key)
                        }
                    ),
                    action.description,
                    Intent::Command(action.name.into()),
                )
            })
            .collect();
        if let Some(dialog) = &mut self.dialog {
            dialog.choices = choices;
            dialog.selected = 0;
            dialog.scroll = 0;
        }
        self.dirty = true;
    }

    pub(super) fn dispatch(&mut self, intent: Intent) -> Result<()> {
        self.preserve_open_form()?;
        self.invalidate_context_view();
        let previous = self.dialog.take();
        self.dirty = true;
        let result = self.dispatch_intent(intent);
        if result.is_err() && self.dialog.is_none() {
            self.dialog = previous;
        }
        result
    }

    fn dispatch_intent(&mut self, intent: Intent) -> Result<()> {
        match intent {
            Intent::Guidance(action) => self.guidance_action(action)?,
            Intent::CheckSend(attempt) => self.check_send(&attempt)?,
            Intent::Context(action) => self.context_action(action)?,
            Intent::Workspace(action) => self.workspace_dispatch(action)?,
            Intent::Command(command) => self.command(&command)?,
            Intent::Lane(lane) => {
                self.set_lane(lane)?;
            }
            Intent::Environment(profile) => {
                self.workspace_dispatch(workspace::WorkspaceAction::Scope(None))?;
                self.scope = profile;
                self.set_lane(self.lane.clone())?;
                self.notice = format!("Browsing {}", self.workspace_scope_label());
            }
            Intent::Type(kind) => {
                self.kind = kind;
                self.rebuild();
            }
            Intent::Theme(theme) => {
                let old = self.theme.clone();
                self.theme = theme;
                if let Err(e) = self.save_ui() {
                    self.theme = old;
                    return Err(e);
                }
                ui::style_editor(&mut self.composer, &self.theme);
            }
            Intent::Layout(layout) => {
                self.layout = layout;
                self.save_ui()?;
            }
            Intent::Open(profile, thread) => self.open(profile, thread)?,
            Intent::NewEnvironment(profile, purpose) => self.choose_project(profile, purpose)?,
            Intent::Project(profile, project, purpose) => {
                self.choose_project_result(profile, project, purpose)?
            }
            Intent::ProjectsMore(profile, cursor, purpose) => self.source_read(
                profile,
                "projects",
                json!({"cursor":cursor,"limit":100}),
                ReadKind::Projects { profile, purpose },
            )?,
            Intent::Edit(target) => self.edit_field(target)?,
            Intent::LaunchCreate => self.create_launch()?,
            Intent::LaunchWorkspace => self.toggle_workspace()?,
            Intent::LaunchModels => self.launch_models()?,
            Intent::PinDestination => self.pin_destination()?,
            Intent::ResetDestination => {
                self.state.update(|v| {
                    v.as_object_mut().unwrap().remove("creationDestination");
                    Ok(())
                })?;
                self.notice = "Creation destination reset to configured native defaults.".into();
            }
            Intent::Model(index) => self.choose_model(index)?,
            Intent::Option(id) => self.choose_option(&id)?,
            Intent::OptionValue(id, value) => {
                let edit = self.model_edit.as_mut().context("Model review expired")?;
                let mut options = array(&edit.selection, "options").to_vec();
                options.retain(|o| o["id"] != id);
                if !value.is_null() {
                    options.push(json!({"id":id,"value":value}));
                }
                edit.selection["options"] = json!(options);
                self.model_menu()?;
            }
            Intent::ModelApply => self.apply_model()?,
            Intent::ModelSave => self.text_dialog(
                "Save model favorite",
                "Name this environment-scoped selection.",
                InputTarget::Favorite,
                String::new(),
            ),
            Intent::ClearOverride => {
                let a = self.active.as_ref().context("Open a conversation first")?;
                let key = format!(
                    "{}/{}",
                    self.environments[a.profile].environment_id, a.thread
                );
                self.state.update(|v| {
                    if let Some(m) = v["modelOverrides"].as_object_mut() {
                        m.remove(&key);
                    }
                    Ok(())
                })?;
                self.notice = "One-message override cleared.".into();
            }
            Intent::Request(request) => self.review_request(request)?,
            Intent::SavedAnswer(record) => self.detail(
                "Saved answer · original source",
                serde_json::to_string_pretty(&record)?,
            ),
            Intent::Approval(request, decision) => {
                self.ensure_request(&request)?;
                let a = self.active.as_ref().unwrap();
                let fields = json!({"threadId":a.thread,"requestId":request["id"],"revision":request["revision"],"decision":decision});
                self.mutate(a.profile, "answer", fields, None)?;
            }
            Intent::QuestionOption(value) => self.question_option(value)?,
            Intent::QuestionNext => self.question_next()?,
            Intent::QuestionBack => {
                if let Some(edit) = &mut self.request_edit {
                    edit.index = edit.index.saturating_sub(1);
                }
                self.question_menu()?;
            }
            Intent::QuestionSubmit => self.submit_questions()?,
            Intent::Confirm {
                profile,
                op,
                fields,
            } => self.mutate(profile, &op, fields, None)?,
            Intent::ReviewRecovery(record, stop) => self.review_saved_action(record, stop)?,
            Intent::Recover(record, stop) => self.retry_record(record, stop)?,
            Intent::Forget(record, stop) => {
                let env = string(&record, "environmentId");
                let id = string(&record, "id");
                ensure!(
                    !self
                        .in_flight
                        .lock()
                        .unwrap()
                        .contains(&(env.clone(), stop)),
                    "Action is still awaiting acknowledgement"
                );
                ensure!(
                    self.state.pending(&env, stop).as_ref() == Some(&record),
                    "Saved action changed; reopen Recovery"
                );
                self.state.clear_pending(&env, stop, &id)?;
                self.notice =
                    "Local recovery record cleared after review. Native state is unchanged.".into();
            }
            Intent::Inspect(item) => {
                let source = string(&item, "sourceThreadId");
                self.read(
                    "inspect-item",
                    json!({"threadId":source,"itemId":item["itemId"],"revision":item["updatedAt"]}),
                    ReadKind::Inspect,
                )?;
                self.notice = "Reading exact native tool item…".into();
            }
            Intent::ActivityEarlier(cursor) => {
                let thread = self
                    .active
                    .as_ref()
                    .context("Open a conversation first")?
                    .thread
                    .clone();
                self.read(
                    "activity",
                    json!({"threadId":thread,"cursor":cursor}),
                    ReadKind::Activity,
                )?;
            }
            Intent::Recall(text) => {
                if !self.text().is_empty() {
                    self.flush_draft()?;
                    let current = self.text();
                    self.state.update(|v| {
                        v["rustRecalledDraft"] = json!({"text":current});
                        Ok(())
                    })?;
                }
                self.replace_text(text);
                self.focus = Focus::Composer;
                self.notice = "Prompt restored as an editable draft. Nothing sent.".into();
            }
            Intent::NativeScope(scope) => {
                self.native_scope = scope;
                self.native_defaults()?;
            }
            Intent::Close => {}
        }
        Ok(())
    }

    pub(super) fn command(&mut self, command: &str) -> Result<()> {
        let (name, args) = command
            .trim()
            .trim_start_matches('/')
            .split_once(' ')
            .unwrap_or((command.trim().trim_start_matches('/'), ""));
        if std::env::var_os("T3OXIDE_DEMO_COMPANIONS").is_some()
            && matches!(name, "graph" | "guide" | "review" | "mermaid" | "form")
        {
            return self.demo_companion(name, None);
        }
        if self.context_command(name, args)? {
            return Ok(());
        }
        if self.check_command(name, args)? {
            return Ok(());
        }
        match name {
            "commands" => self.palette(),
            "command" => self.text_dialog(
                "Enter command",
                "Explicit command entry. Ctrl+] executes the reviewed text; Escape closes without executing.",
                InputTarget::Command,
                String::new(),
            ),
            "quit" => {
                self.flush_draft()?;
                self.begin_context_shutdown();
            },
            "mode" => {
                ensure!(["", "dev", "developer", "agentbox"].contains(&args), "Workspace is Dev");
                self.mode = Mode::Dev;
                self.save_ui()?;
                self.rebuild();
            },
            "views" => {
                let choices = [
                    "Open", "Running", "Pins", "Settled",
                ]
                .into_iter()
                .map(|name| Choice::new(name, "Native conversation state", Intent::Lane(name.into())))
                .collect();
                self.menu("Views", "Dev conversations", choices);
            },
            "switch" | "threads" => self.switcher(),
            "graph" => {
                self.preserve_open_form()?;
                self.graph_requested = true;
                self.dirty = true;
            },
            "env" => {
                let mut choices = vec![Choice::new(
                    "All environments",
                    "Keep unavailable sources visible",
                    Intent::Environment(None),
                )];
                choices.extend(self.environments.iter().enumerate().map(|(i, e)|
                    Choice::new(e.label.clone(), e.id.clone(), Intent::Environment(Some(i)))
                ));
                self.menu(
                    "Environment scope",
                    "Browsing does not change the attached conversation.",
                    choices,
                );
            },
            "filter" => self.menu(
                "Conversation type",
                "Lineage comes from T3.",
                ["all", "main", "subagents", "forks"]
                    .into_iter()
                    .map(|k| Choice::new(k, "", Intent::Type(k.into())))
                    .collect(),
            ),
            "refresh" => self.refresh(false, false)?,
            "more" => self.refresh(true, false)?,
            "search" => {
                if args.is_empty() {
                    self.search_projects();
                } else {
                    self.filter = args.into();
                    self.refresh(false, true)?;
                }
            },
            "search-scope" => self.search_scopes(),
            "preview" => {
                let selected = self.selected_thread().cloned().context("Select a conversation")?;
                self.source_read(
                    selected.profile,
                    "thread-preview",
                    json!({"threadId": selected.id}),
                    ReadKind::Preview,
                )?;
                self.notice = "Reading preview; the active draft and Stop target stay attached.".into();
            },
            "parent" => {
                let a = self.active.as_ref().context("Open a conversation first")?;
                let parent = string(&self.metadata()["lineage"], "parentThreadId");
                ensure!(domain::valid_id(&parent), "No authoritative parent is available");
                self.open(a.profile, parent)?;
            },
            "recent" => {
                let choices = self.recents.iter().map(|(p, t)| {
                    let title = self.threads.iter()
                        .find(|r| r.profile == *p && r.id == *t)
                        .map(|r| r.title.clone())
                        .unwrap_or_else(|| t.clone());
                    Choice::new(title, self.environments[*p].label.clone(), Intent::Open(*p, t.clone()))
                }).collect();
                self.menu("Recent conversations", "Exact environment and thread identities", choices);
            },
            "new" => self.begin_new(NewPurpose::Launch)?,
            "new-same" => self.begin_new(NewPurpose::Same)?,
            "destination" => self.destination_menu()?,
            "native-defaults" => self.begin_new(NewPurpose::NativeDefaults)?,
            "batch" | "file-many" => self.begin_new(NewPurpose::Batch)?,
            "batch-receipts" => self.batch_receipts()?,
            "send" | "literal-send" => self.send_draft()?,
            "stop" => {
                let a = self.active.as_ref().context("Open a conversation first")?;
                let run = string(&self.metadata(), "activeRunId");
                ensure!(!run.is_empty(), "No reviewed active turn. Return to Latest to refresh.");
                self.mutate(a.profile, "stop", json!({"threadId": a.thread, "runId": run}), None)?;
            },
            "delivery" => {
                if ["auto", "queue", "steer"].contains(&args) {
                    self.delivery = args.into();
                } else {
                    self.menu(
                        "Delivery",
                        "T3 owns immediate dispatch and accepted queues.",
                        ["auto", "queue", "steer"].into_iter().map(|mode| Choice::new(
                            mode,
                            match mode {
                                "auto" => "Native automatic delivery",
                                "queue" => "After the active turn",
                                "steer" => "Steer the active turn",
                                _ => "",
                            },
                            Intent::Command(format!("delivery {mode}")),
                        )).collect(),
                    );
                }
            },
            "earlier" => self.navigate(true)?,
            "latest" => self.navigate(false)?,
            "answer-drafts" => {
                let saved = self.state.snapshot();
                let choices = saved["attentionDrafts"].as_object().map(|drafts| drafts.values().map(|record| Choice::new(
                    format!("{} · {}", string(&record["source"],"environmentId"), string(&record["source"],"threadId")),
                    format!("{} · {}", string(&record["source"],"requestId"), string(&record["source"],"revision")),
                    Intent::SavedAnswer(record.clone())
                )).collect()).unwrap_or_default();
                self.menu("Saved answers", "Original source revisions", choices);
            }
            "requests" => {
                let requests = self.active.as_ref().context("Open a conversation first")?.requests.clone();
                ensure!(!requests.is_empty(), "No verified pending requests. Latest refreshes live offers.");
                if requests.len() == 1 {
                    self.review_request(requests[0].clone())?;
                } else {
                    self.menu(
                        "Requests",
                        "Closing this menu leaves every native request pending.",
                        requests.into_iter().map(|r| Choice::new(
                            string(&r, "prompt"), string(&r, "kind"), Intent::Request(r),
                        )).collect(),
                    );
                }
            },
            "activity" => {
                let thread = self.active.as_ref().context("Open a conversation first")?.thread.clone();
                self.read("activity", json!({"threadId": thread}), ReadKind::Activity)?;
                self.notice = "Reading tools and activity…".into();
            },
            "review" => {
                let a = self.active.as_ref().context("Open a conversation first")?;
                self.read("thread-review", json!({"threadId": a.thread}), ReadKind::Review)?;
                self.notice = "Reading native changes…".into();
            },
            "reviewed" | "settle" | "unreviewed" | "unsettle" | "snooze" | "unsnooze" | "pin" => self.thread_action(name)?,
            "model" | "settings" | "effort" | "speed" => {
                let a = self.active.as_ref().context("Open a conversation first")?;
                if name != "effort" {
                    self.model_edit = None;
                }
                let scope = if args == "next" { ModelScope::Next } else { ModelScope::Current };
                self.read(
                    "models",
                    json!({"threadId": a.thread}),
                    if name == "effort" { ReadKind::Effort(scope) } else { ReadKind::Models(scope) },
                )?;
                self.notice = "Reading native model catalog…".into();
            },
            "override" => {
                if args == "clear" {
                    self.dispatch(Intent::ClearOverride)?;
                } else {
                    let a = self.active.as_ref().context("Open a conversation first")?;
                    self.model_edit = None;
                    self.read("models", json!({"threadId": a.thread}), ReadKind::Models(ModelScope::Next))?;
                }
            },
            "orchestration" | "guidance" => self.guidance_menu()?,
            "presets" => self.presets()?,
            "runtime" => {
                let m = self.metadata();
                self.detail("Effective runtime", format!(
                    "Saved selection\n{}\n\nNative run / provider report\n{}\n\nProvider identity\n{}",
                    serde_json::to_string_pretty(&m["modelSelection"])?,
                    serde_json::to_string_pretty(&m["runtimeDetails"])?,
                    serde_json::to_string_pretty(&m["providerIdentity"])?,
                ));
            },
            "recovery" | "recover" | "pending" | "retry" | "retry-launch" | "resolve" => self.recovery_menu()?,
            "reconnect" => {
                let a = self.active.as_ref().context("Open a conversation first")?;
                let target = (a.profile, a.thread.clone());
                self.open(target.0, target.1)?;
            },
            "recall" => self.recall_menu()?,
            "edit" => {
                ensure!(self.active.is_some(), "Open a conversation first");
                self.flush_draft()?;
                self.pending_external_edit = true;
            },
            "layout" => self.menu(
                "Workspace layout",
                "Each layout keeps the same session and composer.",
                vec![
                    Choice::new("Split", "Conversation list, transcript and composer", Intent::Layout(LayoutMode::Split)),
                    Choice::new("Focus", "Give the conversation the full workspace", Intent::Layout(LayoutMode::Focus)),
                    Choice::new("Wide", "Keep native context visible in an inspector", Intent::Layout(LayoutMode::Wide)),
                ],
            ),
            "theme" => self.menu(
                "Appearance",
                "Saved with this Rust client's UI settings.",
                ["violet", "cyan", "lime", "terminal"].into_iter()
                    .map(|t| Choice::new(t, "", Intent::Theme(t.into()))).collect(),
            ),
            "mouse" => {
                self.mouse = !self.mouse;
                self.save_ui()?;
                self.notice = if self.mouse {
                    "Mouse enabled for next launch".into()
                } else {
                    "Mouse disabled for next launch".into()
                };
            },
            "web" => {
                let env = self.active_env()?;
                self.detail("Full T3", env.web_url.clone()
                    .unwrap_or_else(|| "No webURL configured for this environment".into()));
            },
            "help" | "shortcuts" => {
                self.palette();
            },
            "" => {},
            _ => ensure!(self.workspace_command(name, args)?, "Unknown action /{name}; Ctrl+P lists supported actions"),
        }
        self.dirty = true;
        Ok(())
    }

    fn send_draft(&mut self) -> Result<()> {
        let text = self.text();
        ensure!(
            !text.trim().is_empty() && text.len() <= 64 * 1024,
            "Message must contain text and fit 64 KiB"
        );
        let a = self.active.as_ref().context("Open a conversation first")?;
        let profile = a.profile;
        let thread = a.thread.clone();
        let env = &self.environments[profile];
        let key = format!("{}/{}", env.environment_id, thread);
        let snapshot = self.state.snapshot();
        let selection = &snapshot["modelOverrides"][&key];
        let mut fields = json!({"threadId":thread,"message":text,"mode":self.delivery});
        if selection.is_object() {
            ensure!(
                flag(&a.hello["features"], "messageModelOverride"),
                "This host does not support the staged one-message override"
            );
            fields["modelSelection"] = selection["selection"].clone();
            fields["modelOverrideId"] = selection["id"].clone();
        }
        let fields = crate::guidance::prepare_send_fields(&fields, self.send_guidance()?)?;
        self.mutate(profile, "send", fields, None)
    }
    fn thread_action(&mut self, name: &str) -> Result<()> {
        let t = if self.focus == Focus::Navigator {
            self.selected_thread()
                .context("Select a visible conversation first")?
        } else {
            self.active_summary()
                .context("Refresh conversation metadata before this action")?
        };
        let revision = string(t.inbox(), "nativeRevision");
        ensure!(
            !revision.is_empty() && !flag(&t.raw, "_metadataStale"),
            "Native metadata is stale or unavailable; refresh before this action"
        );
        let action = match name {
            "reviewed" => "settle",
            "unreviewed" => "unsettle",
            "pin" if t.pinned() => "navigation-unpin",
            "pin" => "navigation-pin",
            other => other,
        };
        let mut fields = json!({"threadId":t.id,"action":action,"expectedRevision":revision});
        if action == "snooze" {
            fields["until"] =
                json!((OffsetDateTime::now_utc() + time::Duration::hours(1)).format(&Rfc3339)?);
        }
        let profile = t.profile;
        let title = t.title.clone();
        if matches!(action, "settle" | "unsettle") {
            self.flush_draft()?;
            return self.mutate(profile, "thread-action", fields, None);
        }
        let disposition = match action {
            "settle" => {
                "Removes this conversation from Unreviewed and Inbox; keeps it in Settled and All. Active or queued work and pending requests must be resolved first. This is not archive or deletion."
            }
            "unsettle" => "Restores this conversation to the native inbox; does not start a run.",
            "navigation-pin" | "navigation-unpin" => "Persistent navigation pin.",
            _ => "Explicit native disposition.",
        };
        self.menu(
            format!(
                "{} conversation",
                action.strip_prefix("navigation-").unwrap_or(action)
            ),
            format!(
                "{} / {title} · {disposition} T3 revalidates the displayed native revision.",
                self.environments[profile].label
            ),
            vec![
                Choice::new(
                    "Apply",
                    if action == "snooze" {
                        "Later for one hour"
                    } else {
                        "Explicit native disposition"
                    },
                    Intent::Confirm {
                        profile,
                        op: "thread-action".into(),
                        fields,
                    },
                ),
                Choice::new("Cancel", "", Intent::Close),
            ],
        );
        Ok(())
    }
    fn recovery_menu(&mut self) -> Result<()> {
        let choices = self
            .state
            .pending_records()
            .into_iter()
            .map(|pending| {
                let record = pending.record;
                let label = format!(
                    "{} · {} · {}",
                    pending.environment_id,
                    string(&record, "op"),
                    string(&record, "id")
                );
                Choice::new(
                    label,
                    "Inspect original action",
                    Intent::ReviewRecovery(record, pending.stop),
                )
            })
            .collect::<Vec<_>>();
        if choices.is_empty() {
            self.detail("Recovery", "No pending actions. Drafts saved separately.");
        } else {
            self.menu(
                "Pending actions",
                "Delivery unknown · inspect before recovery",
                choices,
            );
        }
        Ok(())
    }
    fn review_saved_action(&mut self, record: Value, stop: bool) -> Result<()> {
        let environment = string(&record, "environmentId");
        ensure!(
            self.state.pending(&environment, stop).as_ref() == Some(&record),
            "Saved action changed; reopen Recovery"
        );
        let detail = serde_json::to_string_pretty(&record)?;
        let mut choices = vec![Choice::new(
            "Keep saved",
            "Retain original record",
            Intent::Close,
        )];
        if self
            .environments
            .iter()
            .any(|e| e.environment_id == environment)
        {
            choices.push(Choice::new(
                "Reconcile original action",
                "Same operation ID and payload",
                Intent::Recover(record.clone(), stop),
            ));
        }
        choices.push(Choice::new(
            "Clear this local recovery record",
            "Inspect native outcome first; clears local record only",
            Intent::Forget(record, stop),
        ));
        self.menu(
            "Pending action",
            format!("{environment} · delivery unknown"),
            choices,
        );
        if let Some(dialog) = &mut self.dialog {
            dialog.detail = detail;
        }
        Ok(())
    }
    fn retry_record(&mut self, record: Value, stop: bool) -> Result<()> {
        let environment = string(&record, "environmentId");
        ensure!(
            self.state.pending(&environment, stop).as_ref() == Some(&record),
            "Saved action changed; reopen Recovery"
        );
        let profile = self
            .environments
            .iter()
            .position(|e| e.environment_id == environment)
            .context("Original environment is not configured; recovery cannot be retargeted")?;
        let id = string(&record, "id");
        let op = string(&record, "op");
        ensure!(!stop || op == "stop", "Saved operation lane mismatch");
        let mut fields = record;
        let object = fields
            .as_object_mut()
            .context("Saved operation must be an object")?;
        object.remove("id");
        object.remove("op");
        object.remove("environmentId");
        self.mutate(profile, &op, fields, Some(id))
    }
    fn recall_menu(&mut self) -> Result<()> {
        let env = self.active_env()?.environment_id.clone();
        let snapshot = self.state.snapshot();
        let mut choices = Vec::new();
        for entry in array(&snapshot, "promptHistory").iter().rev() {
            if entry["environment"] == env {
                let text = string(entry, "text");
                choices.push(Choice::new(
                    text.lines().next().unwrap_or("Prompt").to_owned(),
                    format!("{} · {}", string(entry, "kind"), string(entry, "thread")),
                    Intent::Recall(text),
                ));
            }
        }
        if let Some(text) = snapshot["rustRecalledDraft"]["text"].as_str() {
            choices.push(Choice::new(
                "Draft before recall",
                "Restore the previous composer",
                Intent::Recall(text.into()),
            ));
        }
        self.menu("Prompt recall", "Restoring text never sends it.", choices);
        Ok(())
    }
}

fn input_guidance() -> String {
    // The maintained shortcut document supplies non-command bindings as well.
    // Keep one menu and one source of help data, including editor/navigation keys.
    let mut guidance = String::from("Prompt: Enter newline · Commands: Enter runs\n");
    for line in include_str!("../../docs/shortcuts.md").lines() {
        if !line.starts_with("| ") {
            continue;
        }
        let mut columns = line.trim_matches('|').split('|').map(str::trim);
        if let (Some(action), Some(keys)) = (columns.next(), columns.next())
            && action != "Action"
            && !action.starts_with("---")
            && columns.next().is_none()
        {
            guidance.push_str(&format!("{action} [{}]\n", keys.replace("; ", " | ")));
        }
    }
    guidance
}
