use super::*;
use std::{
    fs,
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
};

struct Fixture {
    app: App,
    directory: tempfile::TempDir,
    native_states: Vec<PathBuf>,
    slt_closes: usize,
}

impl Fixture {
    fn load_pages(&mut self) {
        self.app.refresh(false, false).unwrap();
        self.drain_pages();
    }
    fn drain_pages(&mut self) {
        let deadline = Instant::now() + Duration::from_secs(8);
        while self.app.hosts.iter().any(|host| host.loading) {
            let event = self
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("fixture metadata");
            self.app.handle(event).unwrap();
        }
    }
    fn new(ack_delay: u64) -> Self {
        let directory = tempfile::TempDir::new().unwrap();
        let node = std::env::split_paths(&std::env::var_os("PATH").unwrap_or_default())
            .map(|path| path.join("node"))
            .find(|path| path.is_file())
            .expect("Node fixture runtime");
        let mut environments = Vec::new();
        let mut native_states = Vec::new();
        for index in 0..2 {
            let host = directory.path().join(format!("host-{index}"));
            fs::create_dir(&host).unwrap();
            fs::set_permissions(&host, fs::Permissions::from_mode(0o700)).unwrap();
            let token = host.join("token");
            fs::write(&token, "synthetic-test-token").unwrap();
            fs::set_permissions(&token, fs::Permissions::from_mode(0o600)).unwrap();
            let config = host.join("host.json");
            let native = host.join("native.json");
            let environment_id = format!("native-{index}");
            fs::write(
                &config,
                json!({
                    "baseUrl": "http://127.0.0.1:1", "environmentId": environment_id,
                    "tokenFile": token, "allowWrites": true,
                    "fixture": {"stream": false, "stateFile": native, "ackDelayMs": ack_delay}
                })
                .to_string(),
            )
            .unwrap();
            fs::set_permissions(&config, fs::Permissions::from_mode(0o600)).unwrap();
            native_states.push(native);
            environments.push(Environment {
                id: format!("profile-{index}"),
                label: format!("Fixture {index}"),
                environment_id,
                node: node.to_string_lossy().into_owned(),
                adapter: Path::new(env!("CARGO_MANIFEST_DIR"))
                    .join("tests/rust_fixture.mjs")
                    .to_string_lossy()
                    .into_owned(),
                host_config: config.to_string_lossy().into_owned(),
                ssh_destination: None,
                web_url: None,
                enabled: true,
                default_for_new: index == 0,
                allowed_provider_instance_ids: None,
            });
        }
        let state = Arc::new(State::open(&directory.path().join("rust-state.json")).unwrap());
        Self {
            app: App::new(Args::default(), environments, state).unwrap(),
            directory,
            native_states,
            slt_closes: 0,
        }
    }

    fn open(&mut self, profile: usize) {
        self.open_thread(profile, "thread-a");
    }

    fn draw_slt(&mut self, ctx: &mut ::slt::Context, surface: &mut crate::slt_ui::SurfaceState) {
        self.app.populate_surface(surface);
        let view = self.app.view();
        let modal = self.app.modal();
        let actions = crate::slt_ui::draw(
            ctx,
            &view,
            &mut self.app.composer,
            &mut self.app.modal_editor,
            modal.as_ref(),
            surface,
        );
        self.app.observe_slt_layout(surface);
        for action in actions {
            if matches!(action, crate::slt_ui::Action::CloseModal) {
                self.slt_closes += 1;
            }
            self.app.surface_action(action, surface).unwrap();
        }
    }

    fn open_thread(&mut self, profile: usize, thread: &str) {
        self.app.open(profile, thread.into()).unwrap();
        let deadline = Instant::now() + Duration::from_secs(5);
        while self
            .app
            .active
            .as_ref()
            .is_none_or(|active| active.navigating)
        {
            let event = self
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("verified fixture attachment");
            self.app.handle(event).unwrap();
        }
    }

    fn next_mutation(&mut self) -> Background {
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            let event = self
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("fixture mutation acknowledgement");
            if matches!(&event, Background::Mutation { .. }) {
                return event;
            }
            self.app.handle(event).unwrap();
        }
    }

    fn surface_choice(&mut self, surface: &mut crate::slt_ui::SurfaceState, label: &str) {
        let index = self
            .app
            .dialog
            .as_ref()
            .unwrap()
            .choices
            .iter()
            .position(|choice| choice.label == label)
            .expect("native menu choice");
        self.app
            .surface_action(crate::slt_ui::Action::ModalChoice(index), surface)
            .unwrap();
    }

    fn finish_effort_read(&mut self) {
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            let event = self
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("native effort catalog");
            let complete = matches!(
                &event,
                Background::Read {
                    kind: ReadKind::Effort(_),
                    ..
                }
            );
            self.app.handle(event).unwrap();
            if complete {
                break;
            }
        }
    }

    fn drain_context_work(&mut self) {
        let deadline = Instant::now() + Duration::from_secs(8);
        while self.app.context_has_work() {
            let event = self
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("context worker completion");
            self.app.handle(event).unwrap();
        }
    }

    fn context_pack(&mut self, query: crate::context_store::Query) -> crate::context_store::Pack {
        self.app
            .context_action(context::Action::Recall(query))
            .unwrap();
        loop {
            let event = self
                .app
                .rx
                .recv_timeout(Duration::from_secs(8))
                .expect("context recall completion");
            let pack = match &event {
                Background::Context(context_worker::Event::Finished(finished)) => {
                    match &finished.result {
                        Ok(context_worker::Output::Pack(pack)) => Some((**pack).clone()),
                        _ => None,
                    }
                }
                _ => None,
            };
            self.app.handle(event).unwrap();
            if let Some(pack) = pack {
                return pack;
            }
        }
    }

    fn wait_native_effect(&self, profile: usize) {
        let deadline = Instant::now() + Duration::from_secs(3);
        loop {
            if fs::read(&self.native_states[profile])
                .ok()
                .and_then(|bytes| serde_json::from_slice::<Value>(&bytes).ok())
                .is_some_and(|value| value["effects"].as_u64().unwrap_or(0) > 0)
            {
                return;
            }
            assert!(Instant::now() < deadline, "native effect did not arrive");
            thread::sleep(Duration::from_millis(5));
        }
    }
}

#[test]
fn native_search_keeps_matching_children_visible_alongside_their_parent() {
    let mut fixture = Fixture::new(0);
    fixture.app.hosts[0].page.threads = vec![
        domain::Thread::parse(0, json!({"id":"parent", "title":"Acceptance only", "projectId":"project-a", "status":"completed"})).unwrap(),
        domain::Thread::parse(0, json!({"id":"child", "title":"Matching source", "projectId":"project-a", "status":"completed", "lineage":{"parentThreadId":"parent","relationshipToParent":"subagent"}})).unwrap(),
    ];
    fixture.app.lane = "All".into();
    fixture.app.server_search = true;
    fixture.app.filter = "Acceptance only".into();
    fixture.app.rebuild();
    let visible: Vec<_> = fixture
        .app
        .visible
        .iter()
        .map(|(i, _)| fixture.app.threads[*i].id.as_str())
        .collect();
    assert!(
        visible.contains(&"parent") && visible.contains(&"child"),
        "native matching source was hidden: {visible:?}"
    );
    assert!(
        fixture.app.expanded.is_empty(),
        "search must not alter persisted browsing expansion"
    );
}

impl Drop for Fixture {
    fn drop(&mut self) {
        if let Some(active) = &self.app.active
            && let Some(connection) = &active.connection
        {
            connection.close();
        }
        if let Some(connection) = &self.app.read_connection {
            connection.close();
        }
    }
}

#[test]
fn settle_aliases_dispatch_exact_native_revision_and_refresh_visibility() {
    let mut fixture = Fixture::new(0);
    fixture.app.scope = Some(0);
    fixture.open_thread(0, "thread-review");
    fixture.app.replace_text("Retain this unsent draft".into());
    fixture.app.flush_draft().unwrap();
    fixture.app.refresh(false, false).unwrap();
    for (index, (command, action, settled)) in [
        ("settle", "settle", true),
        ("unsettle", "unsettle", false),
        ("reviewed", "settle", true),
        ("unreviewed", "unsettle", false),
    ]
    .into_iter()
    .enumerate()
    {
        let deadline = Instant::now() + Duration::from_secs(5);
        while fixture.app.hosts[0].loading {
            let event = fixture
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("native metadata refresh");
            fixture.app.handle(event).unwrap();
        }
        let target = fixture
            .app
            .threads
            .iter()
            .find(|thread| thread.id == "thread-review")
            .unwrap();
        assert_eq!(target.reviewed(), !settled);
        let revision = string(target.inbox(), "nativeRevision");
        fixture.app.command(command).unwrap();
        assert!(fixture.app.dialog.is_none());
        let event = fixture.next_mutation();
        let Background::Mutation {
            context,
            op,
            fields,
            result,
            ..
        } = &event
        else {
            panic!("disposition must use the durable native operation lane");
        };
        assert_eq!(context.profile, 0);
        assert_eq!(op, "thread-action");
        assert_eq!(fields["threadId"], "thread-review");
        assert_eq!(fields["action"], action);
        assert_eq!(fields["expectedRevision"], revision);
        assert!(result.is_ok(), "{result:?}");
        fixture.app.handle(event).unwrap();
        let native: Value =
            serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
        assert_eq!(native["effects"], index + 1);
        let deadline = Instant::now() + Duration::from_secs(5);
        while fixture.app.hosts[0].loading {
            let event = fixture
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("disposition metadata refresh");
            fixture.app.handle(event).unwrap();
        }
        let target = fixture
            .app
            .threads
            .iter()
            .find(|thread| thread.id == "thread-review")
            .unwrap();
        assert_eq!(target.reviewed(), settled);
        assert_eq!(target.matches_lane("Unreviewed"), !settled);
        assert_eq!(target.matches_lane("Inbox"), !settled);
        assert_eq!(target.matches_lane("Reviewed"), settled);
        assert!(target.matches_lane("All"));
        assert_eq!(fixture.app.active.as_ref().unwrap().thread, "thread-review");
        assert_eq!(fixture.app.text(), "Retain this unsent draft");
        assert!(fixture.app.state.pending_records().is_empty());
    }
}

#[test]
fn unread_and_completed_use_durable_source_generations_and_independent_running_state() {
    let mut fixture = Fixture::new(0);
    let rows = [
        ("fresh", "completed", false, false, false),
        ("read", "completed", false, false, true),
        ("idle", "idle", false, false, false),
        ("stopped", "interrupted", false, false, false),
        ("request", "waiting", true, true, false),
        ("failed", "failed", true, false, false),
        ("running", "running", true, false, false),
        ("waiting", "waiting", false, false, false),
        ("review", "completed", false, false, false),
    ];
    fixture.app.hosts[0].page.threads = rows.into_iter().map(|(id, status, working, request, pinned)| {
        Thread::parse(0, json!({"id":id,"title":id,"status":status,"updatedAt":"2026-11-02T12:00:00Z",
            "lineage":{"parentThreadId":null},"inbox":{"settled":false,"working":working,"pendingReview":id=="review",
            "pendingRequestCount":u64::from(request),"queuedCount":0,"runId":format!("run-{id}"),"navigationPinned":pinned}})).unwrap()
    }).collect();
    let seen = fixture.app.hosts[0]
        .page
        .threads
        .iter()
        .find(|thread| thread.id == "read")
        .unwrap()
        .completion_generation()
        .unwrap();
    fixture
        .app
        .state
        .save_completion_read("native-0", "read", &seen)
        .unwrap();
    for thread in ["idle", "stopped", "waiting"] {
        fixture
            .app
            .state
            .mark_thread_read("native-0", thread, None)
            .unwrap();
    }
    fixture.app.rebuild();
    assert_eq!(
        fixture
            .app
            .visible
            .iter()
            .map(|(index, _)| fixture.app.threads[*index].id.as_str())
            .collect::<Vec<_>>(),
        ["fresh", "idle", "read", "stopped", "waiting"]
    );
    assert_eq!(fixture.app.completed_count, 4);
    assert_eq!(fixture.app.open_section(&fixture.app.threads[0]), "Unread");
    assert!(!fixture.app.completed_collapsed);
    let generation = fixture.app.hosts[0].page.threads[0]
        .completion_generation()
        .unwrap();
    fixture
        .app
        .state
        .mark_thread_read("native-1", "fresh", Some(&generation))
        .unwrap();
    assert_eq!(
        fixture
            .app
            .open_section(&fixture.app.hosts[0].page.threads[0]),
        "Unread"
    );
    fixture
        .app
        .state
        .mark_thread_read("native-0", "fresh", Some(&generation))
        .unwrap();
    assert_eq!(
        fixture
            .app
            .open_section(&fixture.app.hosts[0].page.threads[0]),
        "Completed"
    );
    fixture.app.hosts[0].page.threads[0].raw["inbox"]["runId"] = json!("next-native-run");
    assert_eq!(
        fixture
            .app
            .open_section(&fixture.app.hosts[0].page.threads[0]),
        "Unread"
    );
    fixture
        .app
        .dispatch(Intent::Lane("Running".into()))
        .unwrap();
    assert_eq!(
        fixture
            .app
            .visible
            .iter()
            .map(|(index, _)| fixture.app.threads[*index].id.as_str())
            .collect::<Vec<_>>(),
        ["failed", "request", "review", "running"]
    );
    let failed = fixture
        .app
        .threads
        .iter()
        .find(|thread| thread.id == "failed")
        .unwrap();
    fixture
        .app
        .state
        .mark_thread_read(
            "native-0",
            &failed.id,
            failed.activity_generation().as_deref(),
        )
        .unwrap();
    fixture.app.rebuild();
    assert_eq!(
        fixture.app.visible.len(),
        4,
        "reading cannot finish a native run or clear attention"
    );
    fixture.app.dispatch(Intent::Lane("Pins".into())).unwrap();
    assert_eq!(fixture.app.selected_thread().unwrap().id, "read");
}

#[test]
fn explicit_verified_open_records_only_its_source_read_and_never_settles() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.app.open(0, "thread-review".into()).unwrap();
    assert!(
        fixture.app.state.snapshot()["threadReads"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    let deadline = Instant::now() + Duration::from_secs(8);
    while fixture.app.active.as_ref().unwrap().navigating {
        let event = fixture
            .app
            .rx
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .unwrap();
        fixture.app.handle(event).unwrap();
    }
    fixture.drain_pages();
    assert_eq!(
        fixture.app.state.snapshot()["threadReads"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    let target = fixture
        .app
        .threads
        .iter()
        .find(|thread| thread.profile == 0 && thread.id == "thread-review")
        .unwrap();
    assert_eq!(fixture.app.open_section(target), "Completed");
    assert!(!target.reviewed());
    assert_eq!(
        fixture.app.state.snapshot()["threadReads"][0],
        json!({"environmentId":"native-0","threadId":"thread-review","generation":target.activity_generation()})
    );
    assert!(
        fixture.app.state.snapshot()["completionReads"]
            .as_array()
            .unwrap()
            .is_empty()
    );
    let other = fixture
        .app
        .threads
        .iter()
        .find(|thread| thread.profile == 1 && thread.id == "thread-review")
        .unwrap();
    assert_eq!(fixture.app.open_section(other), "Unread");
    assert!(fixture.app.state.pending_records().is_empty());
    assert!(!fixture.native_states[0].exists());
}

#[test]
fn settle_shortcut_targets_highlighted_source_without_detaching_the_draft() {
    use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open(0);
    fixture
        .app
        .replace_text("Keep the attached draft and its source".into());
    fixture.app.focus = Focus::Navigator;
    fixture.app.selected = fixture
        .app
        .visible
        .iter()
        .position(|(index, _)| {
            fixture.app.threads[*index].profile == 1
                && fixture.app.threads[*index].id == "thread-review"
        })
        .unwrap();
    fixture
        .app
        .input(Event::Key(KeyEvent::new(
            KeyCode::Char('t'),
            KeyModifiers::CONTROL,
        )))
        .unwrap();
    let event = fixture.next_mutation();
    assert!(
        matches!(&event, Background::Mutation { context, fields, result: Ok(_), .. } if context.profile == 1 && fields["threadId"] == "thread-review" && fields["action"] == "settle")
    );
    fixture.app.handle(event).unwrap();
    fixture.drain_pages();
    assert_eq!(fixture.app.active.as_ref().unwrap().thread, "thread-a");
    assert_eq!(fixture.app.text(), "Keep the attached draft and its source");
    assert_eq!(
        fixture.app.state.draft("native-0", "thread-a"),
        fixture.app.text()
    );
    fixture
        .app
        .dispatch(Intent::Lane("Settled".into()))
        .unwrap();
    fixture.app.selected = fixture
        .app
        .visible
        .iter()
        .position(|(index, _)| {
            fixture.app.threads[*index].profile == 1
                && fixture.app.threads[*index].id == "thread-review"
        })
        .unwrap();
    fixture
        .app
        .input(Event::Key(KeyEvent::new(
            KeyCode::Char('t'),
            KeyModifiers::CONTROL,
        )))
        .unwrap();
    let event = fixture.next_mutation();
    assert!(
        matches!(&event, Background::Mutation { fields, result: Ok(_), .. } if fields["action"] == "unsettle")
    );
    fixture.app.handle(event).unwrap();
    fixture.drain_pages();
    fixture.app.focus = Focus::Composer;
    fixture
        .app
        .input(Event::Key(KeyEvent::new(
            KeyCode::Char('t'),
            KeyModifiers::CONTROL,
        )))
        .unwrap();
    let event = fixture.next_mutation();
    assert!(
        matches!(&event, Background::Mutation { fields, result: Err(_), .. } if fields["threadId"] == "thread-a")
    );
    fixture.app.handle(event).unwrap();
    assert!(fixture.app.state.pending_records().is_empty());
}

#[test]
fn uncertain_settlement_retains_original_id_and_reconciles_only_explicitly() {
    let mut fixture = Fixture::new(0);
    let config_path = &fixture.app.environments[0].host_config;
    let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
    config["fixture"]["dropAckOnce"] = json!(true);
    fs::write(config_path, config.to_string()).unwrap();
    fixture.app.scope = Some(0);
    fixture.load_pages();
    fixture.app.focus = Focus::Navigator;
    fixture.app.selected = fixture
        .app
        .visible
        .iter()
        .position(|(index, _)| fixture.app.threads[*index].id == "thread-review")
        .unwrap();
    fixture.app.command("settle").unwrap();
    let event = fixture.next_mutation();
    assert!(matches!(
        &event,
        Background::Mutation { result: Err(_), .. }
    ));
    fixture.app.handle(event).unwrap();
    let original = fixture.app.state.pending("native-0", false).unwrap();
    fixture.load_pages();
    assert_eq!(
        fixture.app.state.pending("native-0", false),
        Some(original.clone())
    );
    let native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
    assert_eq!(native["effects"], 1);
    fixture.app.command("recover").unwrap();
    let review = fixture.app.dialog.as_ref().unwrap().choices[0]
        .intent
        .clone();
    fixture.app.dispatch(review).unwrap();
    let retry = fixture
        .app
        .dialog
        .as_ref()
        .unwrap()
        .choices
        .iter()
        .find(|choice| matches!(choice.intent, Intent::Recover(..)))
        .unwrap()
        .intent
        .clone();
    fixture.app.dispatch(retry).unwrap();
    let event = fixture.next_mutation();
    assert!(
        matches!(&event, Background::Mutation { id, result: Ok(_), .. } if original["id"] == *id)
    );
    fixture.app.handle(event).unwrap();
    assert!(fixture.app.state.pending_records().is_empty());
    let native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
    assert_eq!(native["effects"], 1);
}

#[test]
fn acknowledged_send_preserves_newer_identical_human_draft() {
    let mut fixture = Fixture::new(600);
    fixture.open(0);
    fixture.app.replace_text("same human text".into());
    fixture.app.flush_draft().unwrap();
    let submitted = fixture.app.draft_version;
    fixture.app.command("send").unwrap();
    fixture.wait_native_effect(0);
    fixture.app.replace_text(String::new());
    fixture.app.replace_text("same human text".into());
    fixture.app.flush_draft().unwrap();
    assert!(fixture.app.draft_version > submitted);
    let event = fixture.next_mutation();
    assert!(matches!(&event, Background::Mutation { result: Ok(_), .. }));
    fixture.app.handle(event).unwrap();
    assert_eq!(fixture.app.text(), "same human text");
    assert_eq!(
        fixture.app.state.draft("native-0", "thread-a"),
        "same human text"
    );
    assert!(fixture.app.state.pending("native-0", false).is_none());
    let disk: Value = serde_json::from_slice(
        &fs::read(fixture.directory.path().join("rust-state.json")).unwrap(),
    )
    .unwrap();
    assert_eq!(disk["drafts"]["native-0/thread-a"], "same human text");
}

#[test]
fn late_ack_event_after_host_switch_cannot_clear_same_thread_id_elsewhere() {
    let mut fixture = Fixture::new(0);
    fixture.open(0);
    fixture.app.replace_text("original source".into());
    fixture.app.command("send").unwrap();
    let acknowledgement = fixture.next_mutation();
    assert!(matches!(
        &acknowledgement,
        Background::Mutation { result: Ok(_), .. }
    ));
    fixture.open(1);
    fixture.app.replace_text("second source draft".into());
    fixture.app.flush_draft().unwrap();
    fixture.app.handle(acknowledgement).unwrap();
    assert_eq!(fixture.app.active.as_ref().unwrap().profile, 1);
    assert_eq!(fixture.app.text(), "second source draft");
    assert_eq!(
        fixture.app.state.draft("native-1", "thread-a"),
        "second source draft"
    );
    assert!(fixture.app.state.draft("native-0", "thread-a").is_empty());
}

#[test]
fn delayed_autosave_cannot_resurrect_acknowledged_text() {
    let mut fixture = Fixture::new(0);
    fixture.open(0);
    fixture.app.replace_text("retire once".into());
    fixture.app.flush_draft().unwrap();
    let (release, gate) = bounded::<()>(0);
    fixture
        .app
        .storage_pool
        .submit(move || {
            let _ = gate.recv();
        })
        .unwrap();
    fixture.app.schedule_save().unwrap();
    fixture.app.command("send").unwrap();
    let acknowledgement = fixture.next_mutation();
    assert!(matches!(
        &acknowledgement,
        Background::Mutation { result: Ok(_), .. }
    ));
    // The worker committed the ACK, but the old editor is still drawn. Flush
    // must honor its newer cache marker before the UI consumes the ACK event.
    fixture.app.flush_draft().unwrap();
    assert!(fixture.app.text().is_empty());
    release.send(()).unwrap();
    let deadline = Instant::now() + Duration::from_secs(3);
    loop {
        let event = fixture
            .app
            .rx
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .unwrap();
        let saved = matches!(event, Background::Saved { .. });
        fixture.app.handle(event).unwrap();
        if saved {
            break;
        }
    }
    fixture.app.handle(acknowledgement).unwrap();
    assert!(fixture.app.state.draft("native-0", "thread-a").is_empty());
    assert!(fixture.app.text().is_empty());
}

#[test]
fn stale_host_results_never_replace_a_newer_scope() {
    let mut fixture = Fixture::new(0);
    fixture.app.hosts[0].generation = 2;
    fixture.app.hosts[0].page = ThreadPage::parse(
        0,
        json!({"threads": [{"id": "newer", "title": "Newer view"}]}),
    )
    .unwrap();
    fixture
        .app
        .handle(Background::Page {
            profile: 0,
            generation: 1,
            append: false,
            result: Ok(json!({"threads": [{"id": "old", "title": "Obsolete view"}]})),
        })
        .unwrap();
    assert_eq!(fixture.app.hosts[0].page.threads[0].id, "newer");
}

#[test]
fn two_hanging_hosts_do_not_delay_a_healthy_third_metadata_source() {
    struct HungHelpers(Vec<PathBuf>);
    impl Drop for HungHelpers {
        fn drop(&mut self) {
            for path in &self.0 {
                if let Some(pid) = fs::read_to_string(path)
                    .ok()
                    .and_then(|value| value.parse::<i32>().ok())
                {
                    // SAFETY: the PID file is written only by this test's
                    // private helper, which remains alive until this guard.
                    unsafe {
                        libc::kill(pid, libc::SIGTERM);
                    }
                }
            }
        }
    }
    let mut fixture = Fixture::new(0);
    let adapter = fixture.directory.path().join("hang.mjs");
    fs::write(&adapter, "import {writeFileSync} from 'node:fs'; writeFileSync(process.argv.at(-1)+'.pid',String(process.pid),{mode:0o600}); setInterval(()=>{},1000);\n").unwrap();
    let mut sources = Vec::new();
    let mut pids = Vec::new();
    for index in 0..2 {
        let mut environment = fixture.app.environments[index].clone();
        environment.adapter = adapter.to_string_lossy().into_owned();
        environment.default_for_new = false;
        pids.push(PathBuf::from(format!("{}.pid", environment.host_config)));
        sources.push(environment);
    }
    let mut healthy = fixture.app.environments[0].clone();
    healthy.id = "healthy-third".into();
    // This fixture's native identity still has to match its host configuration.
    sources[0].environment_id = "silent-0".into();
    sources[1].environment_id = "silent-1".into();
    sources.push(healthy);
    let _helpers = HungHelpers(pids.clone());
    fixture.app = App::new(Args::default(), sources, fixture.app.state.clone()).unwrap();
    fixture.app.refresh(false, false).unwrap();
    let deadline = Instant::now() + Duration::from_secs(4);
    while fixture.app.hosts[2].page.threads.is_empty() || pids.iter().any(|path| !path.exists()) {
        assert!(
            Instant::now() < deadline,
            "silent hosts blocked healthy metadata"
        );
        if let Ok(event) = fixture.app.rx.recv_timeout(Duration::from_millis(20)) {
            fixture.app.handle(event).unwrap();
        }
    }
    assert!(fixture.app.hosts[0].loading && fixture.app.hosts[1].loading);
    assert!(
        fixture.app.hosts[2]
            .page
            .threads
            .iter()
            .all(|thread| thread.profile == 2)
    );
}

fn live_snapshot(revision: u64) -> Value {
    json!({"revision": revision, "threadId": "thread-a", "title": "Fixture", "model": "model",
        "status": "running", "activeRunId": "run-a", "state": "live", "live": true,
        "historical": false, "hasMore": false, "limited": false, "requests": [],
        "items": [{"key": "thread-a/answer", "sourceThreadId": "thread-a", "itemId": "answer",
            "kind": "assistant_message", "text": "First", "streaming": true, "truncated": false, "attachmentCount": 0}]})
}

fn synthetic_attachment(app: &mut App) {
    let mut projection = Projection::default();
    projection
        .apply_snapshot(live_snapshot(1), "thread-a")
        .unwrap();
    app.generation = 7;
    app.active = Some(Active {
        profile: 0,
        thread: "thread-a".into(),
        generation: 7,
        navigating: false,
        navigation: None,
        connection: None,
        hello: Value::Null,
        projection,
        summary: None,
        requests: Vec::new(),
        status: "Live".into(),
    });
    // Coalesce reads so the requested source is observable without networking.
    app.hosts[0].loading = true;
}

#[test]
fn surface_copy_draft_uses_the_active_prompt_without_selection_or_native_operations() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    fixture.app.tabs = vec![(0, "thread-a".into()), (1, "other-thread".into())];
    fixture.app.threads = vec![
        Thread::parse(
            1,
            json!({"id":"other-thread","title":"Highlighted elsewhere"}),
        )
        .unwrap(),
    ];
    fixture.app.visible = vec![(0, 0)];
    fixture.app.selected = 0;
    fixture.app.focus = Focus::Navigator;
    fixture
        .app
        .state
        .set_draft("native-0", "thread-a", "Older saved draft")
        .unwrap();
    fixture
        .app
        .state
        .set_draft("native-1", "other-thread", "Other tab draft")
        .unwrap();
    let pending = json!({"id":"original-copy-test-operation","op":"answer","environmentId":"native-1","threadId":"other-thread","requestId":"other-request","revision":"a".repeat(64),"answers":{"choice":"Keep"}});
    fixture
        .app
        .state
        .save_pending("native-1", false, pending.clone())
        .unwrap();
    let saved = fixture.app.state.snapshot();
    let tabs = fixture.app.tabs.clone();
    let mut surface = crate::slt_ui::SurfaceState::new();
    for exact in [
        "\n \tRésumé 🙂 日本語 e\u{301}\n\nlast  \n\n",
        " \t\n\n",
        "",
    ] {
        fixture.app.replace_text(exact.into());
        let draft_version = fixture.app.draft_version;
        let save_at = fixture.app.save_at;
        let cursor = fixture.app.composer.cursor();
        fixture
            .app
            .surface_action(
                crate::slt_ui::Action::Command("copy-draft".into()),
                &mut surface,
            )
            .unwrap();
        assert_eq!(fixture.app.clipboard_request.take().as_deref(), Some(exact));
        assert_eq!(fixture.app.text(), exact);
        assert_eq!(fixture.app.composer.cursor(), cursor);
        assert_eq!(fixture.app.draft_version, draft_version);
        assert_eq!(fixture.app.save_at, save_at);
        assert_eq!(
            fixture.app.draft_cache.lock().unwrap()["native-0/thread-a"].1,
            exact
        );
        let active = fixture.app.active.as_ref().unwrap();
        assert_eq!(
            (active.profile, active.thread.as_str(), active.generation),
            (0, "thread-a", 7)
        );
        assert!(
            active.connection.is_none(),
            "copy needs no native connection"
        );
        assert_eq!(fixture.app.tabs, tabs);
        assert_eq!(fixture.app.selected_thread().unwrap().id, "other-thread");
        assert_eq!(fixture.app.focus, Focus::Navigator);
        assert_eq!(fixture.app.state.snapshot(), saved);
        assert!(fixture.app.in_flight.lock().unwrap().is_empty());
        assert!(fixture.app.rx.is_empty());
    }
    assert_eq!(fixture.app.state.pending("native-1", false), Some(pending));
    assert!(fixture.native_states.iter().all(|path| !path.exists()));
    let action = domain::ACTIONS
        .iter()
        .find(|action| action.name == "copy-draft")
        .unwrap();
    assert_eq!(action.label, "Copy draft");
}

#[test]
fn closing_the_last_question_tab_preserves_answers_and_allows_navigation_and_quit() {
    for dismiss_first in [false, true] {
        let mut fixture = Fixture::new(0);
        fixture.open_thread(0, "thread-question");
        fixture
            .app
            .replace_text("Unsent original prompt 日本語  \n".into());
        let active = fixture.app.active.as_mut().unwrap();
        active.requests[0]["questions"][0]["allowCustomAnswer"] = json!(true);
        let request = active.requests[0].clone();
        let form =
            crate::attention::Form::from_request("native-0", "thread-question", &request).unwrap();
        fixture.app.review_request(request).unwrap();
        fixture.app.edit_field(InputTarget::Question).unwrap();
        let exact = "Fresh résumé 🙂\n日本語  \n  unfinished ";
        fixture.app.modal_editor = ui::editor(exact, &fixture.app.theme, "");
        let mut surface = crate::slt_ui::SurfaceState::new();
        fixture
            .app
            .surface_action(crate::slt_ui::Action::ModalEditChanged, &mut surface)
            .unwrap();
        assert!(fixture.app.form_save_at.is_some());
        if dismiss_first {
            fixture
                .app
                .surface_action(crate::slt_ui::Action::CloseModal, &mut surface)
                .unwrap();
        }
        assert_eq!(fixture.app.tabs, [(0, "thread-question".into())]);
        let saved = fixture.app.state.snapshot();
        let pending = (saved["pending"].clone(), saved["pendingStops"].clone());
        fixture
            .app
            .surface_action(
                crate::slt_ui::Action::CloseTab("0\0thread-question".into()),
                &mut surface,
            )
            .unwrap();
        assert!(fixture.app.active.is_none() && fixture.app.tabs.is_empty());
        assert_eq!(
            form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
            exact
        );
        assert_eq!(
            fixture.app.state.draft("native-0", "thread-question"),
            "Unsent original prompt 日本語  \n"
        );
        fixture
            .app
            .surface_action(
                crate::slt_ui::Action::Command("palette".into()),
                &mut surface,
            )
            .expect("a saved form cannot block actions after its last tab closes");
        fixture
            .app
            .surface_action(
                crate::slt_ui::Action::OpenSource(crate::slt_ui::Source {
                    profile: 0,
                    environment: "native-0".into(),
                    thread: "thread-a".into(),
                    ..Default::default()
                }),
                &mut surface,
            )
            .unwrap();
        assert_eq!(fixture.app.active.as_ref().unwrap().thread, "thread-a");
        fixture
            .app
            .surface_action(crate::slt_ui::Action::Command("quit".into()), &mut surface)
            .unwrap();
        fixture.app.drain_context().unwrap();
        assert!(fixture.app.quit);
        assert_eq!(
            form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
            exact
        );
        let saved = fixture.app.state.snapshot();
        assert_eq!(
            (saved["pending"].clone(), saved["pendingStops"].clone()),
            pending
        );
        assert!(fixture.app.in_flight.lock().unwrap().is_empty());
        for path in &fixture.native_states {
            if let Ok(bytes) = fs::read(path) {
                let native: Value = serde_json::from_slice(&bytes).unwrap();
                assert_eq!(native["effects"].as_u64().unwrap_or(0), 0);
            }
        }
    }
}

#[test]
fn surface_palette_saves_the_latest_answer_before_replacing_its_editor() {
    let mut fixture = Fixture::new(0);
    fixture.open_thread(0, "thread-question");
    let active = fixture.app.active.as_mut().unwrap();
    active.requests[0]["questions"][0]["allowCustomAnswer"] = json!(true);
    let request = active.requests[0].clone();
    fixture.app.review_request(request.clone()).unwrap();
    fixture.app.edit_field(InputTarget::Question).unwrap();
    let freshest = "Résumé 🙂\n日本語  \n  unfinished ";
    fixture.app.modal_editor = ui::editor(freshest, &fixture.app.theme, "");
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalEditChanged, &mut surface)
        .unwrap();
    assert!(fixture.app.form_save_at.is_some());
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("palette".into()),
            &mut surface,
        )
        .unwrap();
    let form =
        crate::attention::Form::from_request("native-0", "thread-question", &request).unwrap();
    assert_eq!(
        form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
        freshest
    );
    assert!(matches!(
        fixture.app.dialog.as_ref().unwrap().kind,
        DialogKind::Palette
    ));
    assert!(fixture.app.form_save_at.is_none());
    assert!(fixture.app.state.pending_records().is_empty());
}

#[test]
fn surface_invalid_next_retains_the_question_dialog_and_focus() {
    let mut fixture = Fixture::new(0);
    fixture.open_thread(0, "thread-question");
    let request = fixture.app.active.as_ref().unwrap().requests[0].clone();
    fixture.app.review_request(request).unwrap();
    let next = fixture
        .app
        .dialog
        .as_ref()
        .unwrap()
        .choices
        .iter()
        .position(|choice| matches!(choice.intent, Intent::QuestionNext))
        .unwrap();
    fixture.app.dialog.as_mut().unwrap().selected = next;
    let before = fixture.app.modal().unwrap();
    let focus = fixture.app.focus;
    let mut surface = crate::slt_ui::SurfaceState::new();
    let error = fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalChoice(next), &mut surface)
        .unwrap_err();
    assert!(error.to_string().contains("requires an answer"), "{error}");
    let after = fixture
        .app
        .modal()
        .expect("invalid Next must keep the form open");
    assert_eq!(after.action_stamp, before.action_stamp);
    assert_eq!(after.selected, next);
    assert_eq!(fixture.app.focus, focus);
    assert_eq!(fixture.app.request_edit.as_ref().unwrap().index, 0);
    assert!(fixture.app.state.pending_records().is_empty());
}

#[test]
fn reopening_requests_keeps_each_question_answer_and_review_uses_native_labels() {
    let mut fixture = Fixture::new(0);
    fixture.open_thread(0, "thread-question");
    let active = fixture.app.active.as_mut().unwrap();
    active.requests[0]["questions"] = json!([
        {"id":"first-internal-id","header":"Release","question":"Which release?","required":true,
            "options":[{"value":"stable-internal-value","label":"Stable release"}]},
        {"id":"second-internal-id","header":"Notes","question":"What should accompany it?","required":true,"allowCustomAnswer":true}
    ]);
    let request = active.requests[0].clone();
    fixture.app.review_request(request).unwrap();
    fixture
        .app
        .question_option(json!("stable-internal-value"))
        .unwrap();
    fixture.app.question_next().unwrap();
    fixture.app.edit_field(InputTarget::Question).unwrap();
    let exact = "Résumé **literal**\n日本語  \n  unfinished ";
    fixture.app.modal_editor = ui::editor(exact, &fixture.app.theme, "");
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalEditChanged, &mut surface)
        .unwrap();
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("requests".into()),
            &mut surface,
        )
        .unwrap();
    let edit = fixture.app.request_edit.as_ref().unwrap();
    assert_eq!(edit.answers["first-internal-id"], "stable-internal-value");
    assert_eq!(edit.answers["second-internal-id"], exact);
    assert_eq!(edit.index, 0);
    for _ in 0..2 {
        let next = fixture
            .app
            .dialog
            .as_ref()
            .unwrap()
            .choices
            .iter()
            .position(|choice| matches!(choice.intent, Intent::QuestionNext))
            .unwrap();
        fixture
            .app
            .surface_action(crate::slt_ui::Action::ModalChoice(next), &mut surface)
            .unwrap();
    }
    let review = &fixture.app.dialog.as_ref().unwrap().description;
    assert_eq!(
        review,
        &format!(
            "Release\nWhich release?\nStable release\n\nNotes\nWhat should accompany it?\n{exact}"
        )
    );
    assert!(!review.contains("internal-id") && !review.contains("stable-internal-value"));
    assert!(
        fixture.app.state.pending_records().is_empty(),
        "review never submits"
    );
}

#[test]
fn pending_question_inspection_cannot_replace_a_newer_selected_source() {
    let mut fixture = Fixture::new(0);
    fixture.open_thread(0, "thread-question");
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    let old = surface.questions[0].clone();
    let newer = crate::slt_ui::AttentionItem {
        key: "newer-question".into(),
        title: "New selection".into(),
        ..old.clone()
    };
    surface.loading_question = Some(old.key);
    surface.selected_question = Some(newer.key.clone());
    surface.retained_question = Some(newer.clone());
    fixture.app.populate_surface(&mut surface);
    assert!(surface.loading_question.is_none());
    assert_eq!(surface.retained_question.as_ref().unwrap().key, newer.key);
}

#[test]
fn automatic_metadata_refresh_admits_new_attention_at_the_cache_bound_and_removes_complete_absences()
 {
    let mut fixture = Fixture::new(0);
    fixture.app.hosts[0].page.threads = (0..1600)
        .map(|index| {
            Thread::parse(
                0,
                json!({
                    "id":format!("old-{index:04}"),"title":"Earlier source","status":"idle",
                    "inbox":{"activityAt":"2026-11-01T00:00:00Z","working":false}
                }),
            )
            .unwrap()
        })
        .collect();
    fixture.app.rebuild();
    let selected = fixture.app.selected_thread().unwrap().id.clone();
    fixture.app.hosts[0].refresh_loaded = true;
    fixture.app.handle(Background::Page {profile:0,generation:0,append:false,result:Ok(json!({
        "threads":[{"id":"new-attention","title":"New human question","status":"waiting", "inbox":{
            "activityAt":"2026-11-05T00:00:00Z","attention":true,"working":false,
            "pendingRequestId":"native-question","pendingRequestType":"user_input"}}],
        "more":true,"nextCursor":200
    }))}).unwrap();
    assert_eq!(fixture.app.hosts[0].page.threads.len(), 1600);
    assert!(
        fixture
            .app
            .threads
            .iter()
            .any(|row| row.id == "new-attention" && row.running())
    );
    assert_eq!(fixture.app.selected_thread().unwrap().id, selected);
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    assert!(
        surface
            .questions
            .iter()
            .any(|question| question.source.thread == "new-attention")
    );
    fixture.app.hosts[0].refresh_loaded = true;
    fixture.app.hosts[0].request_cursor = 0;
    fixture.app.handle(Background::Page {profile:0,generation:0,append:false,result:Ok(json!({
        "threads":[{"id":selected,"title":"Retained selected source","status":"idle","inbox":{"working":false}}],
        "more":false,"nextCursor":null
    }))}).unwrap();
    fixture.app.populate_surface(&mut surface);
    assert_eq!(fixture.app.threads.len(), 1);
    assert!(
        surface.questions.is_empty(),
        "complete authoritative removal retires phantom questions"
    );
}

#[test]
fn metadata_deadline_observes_unopened_other_host_questions_and_failures_without_input() {
    let mut fixture = Fixture::new(0);
    for profile in 0..2 {
        let config_path = fixture
            .directory
            .path()
            .join(format!("host-{profile}/host.json"));
        let mut config: Value = serde_json::from_slice(&fs::read(&config_path).unwrap()).unwrap();
        config["fixture"]["persistInitialState"] = json!(true);
        fs::write(config_path, serde_json::to_vec(&config).unwrap()).unwrap();
    }
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    fixture.app.replace_text("Exact unsent résumé 🙂".into());
    fixture.app.flush_draft().unwrap();
    fixture.drain_pages();
    fixture.app.lane = "Running".into();
    fixture.app.scope = Some(0);
    fixture.app.rebuild();
    let selected = fixture.app.selected_thread().map(Thread::key);
    let original_source = fixture.app.draft_key();
    let original_generation = fixture.app.generation;
    let mut native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[1]).unwrap()).unwrap();
    let mut question = native["projections"]["thread-question"].clone();
    question["thread"]["id"] = json!("thread-review");
    question["thread"]["title"] = json!("External question");
    for item in question["turnItems"].as_array_mut().unwrap() {
        item["threadId"] = json!("thread-review");
    }
    native["projections"]["thread-review"] = question;
    native["projections"]["thread-fork"]["thread"]["status"] = json!("failed");
    native["projections"]["thread-fork"]["runs"][0]["status"] = json!("failed");
    native["projections"]["thread-fork"]["thread"]["latestRunCompletedAt"] =
        json!("2026-11-05T10:00:00.000Z");
    fs::write(
        &fixture.native_states[1],
        serde_json::to_vec(&native).unwrap(),
    )
    .unwrap();
    let due = fixture.app.metadata_refresh_at;
    let generations: Vec<_> = fixture
        .app
        .hosts
        .iter()
        .map(|host| host.generation)
        .collect();
    fixture.app.refresh_metadata_if_due(due);
    assert!(
        fixture
            .app
            .hosts
            .iter()
            .all(|host| host.loading && host.queued.is_none())
    );
    fixture
        .app
        .refresh_metadata_if_due(due + Duration::from_millis(1));
    assert_eq!(
        fixture
            .app
            .hosts
            .iter()
            .map(|host| host.generation)
            .collect::<Vec<_>>(),
        generations
            .iter()
            .map(|generation| generation + 1)
            .collect::<Vec<_>>()
    );
    fixture.drain_pages();
    for id in ["thread-review", "thread-fork"] {
        assert!(
            fixture
                .app
                .threads
                .iter()
                .any(|row| row.profile == 1 && row.id == id && row.running()),
            "unopened {id} must update independently of the attached host"
        );
    }
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    assert!(
        surface
            .questions
            .iter()
            .any(|question| question.source.environment == "native-1"
                && question.source.thread == "thread-review")
    );
    assert_eq!(fixture.app.selected_thread().map(Thread::key), selected);
    assert_eq!(fixture.app.draft_key(), original_source);
    assert_eq!(fixture.app.generation, original_generation);
    assert_eq!(fixture.app.text(), "Exact unsent résumé 🙂");
    assert!(fixture.app.state.pending_records().is_empty());
    assert_eq!(native["effects"], 0);
}

#[test]
fn surface_search_starts_in_the_current_project_and_scope_switch_retains_query() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("search".into()),
            &mut surface,
        )
        .unwrap();
    assert!(matches!(
        fixture.app.dialog.as_ref().unwrap().kind,
        DialogKind::Text(InputTarget::Search)
    ));
    assert_eq!(
        fixture.app.state.snapshot()["sltBrowserScope"],
        json!({"profileId":"profile-0","environmentId":"native-0","projectId":"project-a"})
    );
    fixture.app.modal_editor = ui::editor("Exact unsent query", &fixture.app.theme, "");
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("search-scope".into()),
            &mut surface,
        )
        .unwrap();
    assert_eq!(fixture.app.search_query, "Exact unsent query");
    let all = fixture
        .app
        .dialog
        .as_ref()
        .unwrap()
        .choices
        .iter()
        .position(|choice| choice.label == "All projects")
        .unwrap();
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalChoice(all), &mut surface)
        .unwrap();
    assert_eq!(
        fixture.app.modal_editor.lines().join("\n"),
        "Exact unsent query"
    );
    assert!(fixture.app.state.snapshot()["sltBrowserScope"].is_null());
}

#[test]
fn model_modal_roles_preserve_native_indices_and_cancel_only_the_child() {
    use ui::{ModalItemRole as Role, ModelDialog};
    let mut fixture = Fixture::new(0);
    fixture.open(0);
    let original = json!({"instanceId":"selected-account","model":"native-a","options":[
        {"id":"native-effort","value":"high"}
    ]});
    fixture.app.model_edit = Some(ModelEdit {
        option_origin: None,
        environment_id: "native-0".into(),
        scope: ModelScope::Next,
        catalog: json!({"models":[
            {"slug":"native-a","name":"Apply reviewed selection","options":[
                {"id":"native-effort","label":"Save favorite","type":"select","options":[
                    {"id":"low","label":"Apply reviewed selection"},
                    {"id":"high","label":"Save favorite"}
                ]}
            ]},
            {"slug":"native-b","name":"Save favorite","options":[]}
        ]}),
        original: original.clone(),
        selection: original.clone(),
    });
    fixture.app.model_menu().unwrap();
    let modal = fixture.app.modal().unwrap();
    assert_eq!(modal.model, Some(ModelDialog::Configuration));
    assert_eq!(modal.items.len(), 6);
    assert_eq!(
        modal.items[0].role,
        Role::Model {
            native_id: "native-a".into()
        }
    );
    assert!(modal.items[0].checked && modal.items[0].applied);
    assert_eq!(
        modal.items[1].role,
        Role::Model {
            native_id: "native-b".into()
        }
    );
    assert!(!modal.items[1].checked && !modal.items[1].applied);
    assert_eq!(
        modal.items[2].role,
        Role::ModelOption {
            id: "native-effort".into(),
            label: "Save favorite".into(),
            value: "high".into(),
        }
    );
    assert!(modal.items[2].applied);
    assert_eq!(modal.items[3].role, Role::ModelApply);
    assert_eq!(modal.items[4].role, Role::ModelSave);
    assert_eq!(modal.items[5].role, Role::ModelClear);
    // A catalog replacement at the same controller index must invalidate UI geometry.
    fixture.app.model_edit.as_mut().unwrap().catalog["models"][1]["slug"] = json!("replacement");
    assert_ne!(
        fixture.app.modal().unwrap().action_stamp,
        modal.action_stamp
    );
    fixture.app.model_edit.as_mut().unwrap().catalog["models"][1]["slug"] = json!("native-b");

    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalChoice(2), &mut surface)
        .unwrap();
    let values = fixture.app.modal().unwrap();
    assert_eq!(
        values.model,
        Some(ModelDialog::OptionValues {
            id: "native-effort".into()
        })
    );
    assert!(values.items.iter().all(|item| item.role
        == Role::ModelOptionValue {
            id: "native-effort".into()
        }));
    assert!(values.items[2].checked && values.items[2].applied);
    assert!(!values.items[1].checked && !values.items[1].applied);
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalChoice(1), &mut surface)
        .unwrap();
    let staged = fixture.app.model_edit.as_ref().unwrap().selection.clone();
    assert_eq!(staged["options"][0]["value"], "low");
    assert!(!fixture.app.modal().unwrap().items[2].applied);
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalChoice(2), &mut surface)
        .unwrap();
    let values = fixture.app.modal().unwrap();
    assert!(values.items[1].checked && !values.items[1].applied);
    assert!(!values.items[2].checked && values.items[2].applied);
    fixture
        .app
        .surface_action(crate::slt_ui::Action::CloseModal, &mut surface)
        .unwrap();
    assert_eq!(
        fixture.app.modal().unwrap().model,
        Some(ModelDialog::Configuration)
    );
    assert_eq!(fixture.app.model_edit.as_ref().unwrap().selection, staged);
    assert!(fixture.app.state.pending_records().is_empty());
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalChoice(1), &mut surface)
        .unwrap();
    let modal = fixture.app.modal().unwrap();
    assert!(modal.items[0].applied && !modal.items[0].checked);
    assert!(modal.items[1].checked && !modal.items[1].applied);
    assert!(
        array(
            &fixture.app.model_edit.as_ref().unwrap().selection,
            "options"
        )
        .is_empty()
    );
    fixture
        .app
        .surface_action(crate::slt_ui::Action::CloseModal, &mut surface)
        .unwrap();
    assert!(fixture.app.modal().is_none());
    assert!(fixture.app.state.pending_records().is_empty());

    fixture.app.menu(
        "Model · provider text",
        "",
        vec![
            Choice::new("Apply reviewed selection", "", Intent::Close),
            Choice::new("Save favorite", "", Intent::Close),
        ],
    );
    let unrelated = fixture.app.modal().unwrap();
    assert!(unrelated.model.is_none());
    assert!(unrelated.items.iter().all(|item| item.role == Role::Choice));
}

#[test]
fn option_checked_value_is_independent_of_keyboard_selection_and_defaults() {
    let mut fixture = Fixture::new(0);
    let catalog = json!({"models":[{"slug":"model-a","options":[
        {"id":"effort","type":"enum","options":[{"id":"low","label":"Low"},{"id":"high","label":"High"}]},
        {"id":"enabled","type":"boolean"}
    ]}]});
    for (id, value, expected) in [
        ("effort", json!("high"), "High"),
        ("enabled", json!(false), "false"),
        ("effort", Value::Null, "Native default"),
    ] {
        fixture.app.model_edit = Some(ModelEdit {
            option_origin: None,
            environment_id: "native-0".into(),
            catalog: catalog.clone(),
            scope: ModelScope::Current,
            original: json!({"instanceId":"selected-account","model":"model-a"}),
            selection: json!({"instanceId":"selected-account","model":"model-a","options":
                if value.is_null() {json!([])} else {json!([{"id":id,"value":value}])}}),
        });
        fixture.app.choose_option(id).unwrap();
        let modal = fixture.app.modal().unwrap();
        assert_eq!(modal.selected, 0);
        assert_eq!(
            modal
                .items
                .iter()
                .filter(|item| item.checked)
                .map(|item| item.label.as_str())
                .collect::<Vec<_>>(),
            [expected]
        );
        assert_eq!(
            fixture.app.model_edit.as_ref().unwrap().selection["instanceId"],
            "selected-account"
        );
    }
}

#[test]
fn surface_effort_opens_native_options_and_preserves_staged_model_until_apply() {
    let mut fixture = Fixture::new(0);
    fixture.open(0);
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("effort".into()),
            &mut surface,
        )
        .unwrap();
    fixture.finish_effort_read();
    let modal = fixture.app.modal().unwrap();
    assert_eq!(modal.title, "Reasoning");
    assert_eq!(
        modal
            .items
            .iter()
            .filter(|item| item.checked)
            .map(|item| item.label.as_str())
            .collect::<Vec<_>>(),
        ["Medium"]
    );
    assert_eq!(
        fixture.app.model_edit.as_ref().unwrap().selection["instanceId"],
        "fixture-provider"
    );
    fixture.surface_choice(&mut surface, "High");
    let fast = array(&fixture.app.model_edit.as_ref().unwrap().catalog, "models")
        .iter()
        .position(|model| model["slug"] == "fixture-fast")
        .unwrap();
    fixture.app.choose_model(fast).unwrap();
    let staged_model = fixture.app.model_edit.as_ref().unwrap().selection.clone();
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("effort".into()),
            &mut surface,
        )
        .unwrap();
    fixture.finish_effort_read();
    assert_eq!(
        fixture.app.model_edit.as_ref().unwrap().selection,
        staged_model
    );
    assert_eq!(
        fixture
            .app
            .modal()
            .unwrap()
            .items
            .iter()
            .filter(|item| item.checked)
            .map(|item| item.label.as_str())
            .collect::<Vec<_>>(),
        ["Native default"]
    );
    fixture.surface_choice(&mut surface, "High");
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("effort".into()),
            &mut surface,
        )
        .unwrap();
    fixture.finish_effort_read();
    assert_eq!(
        fixture
            .app
            .modal()
            .unwrap()
            .items
            .iter()
            .filter(|item| item.checked)
            .map(|item| item.label.as_str())
            .collect::<Vec<_>>(),
        ["High"]
    );
    fixture.surface_choice(&mut surface, "Native default");
    assert!(
        array(
            &fixture.app.model_edit.as_ref().unwrap().selection,
            "options"
        )
        .is_empty()
    );
    assert!(
        fixture.app.state.pending_records().is_empty(),
        "choosing effort only stages the selection"
    );
    assert_eq!(
        fixture.app.metadata()["modelSelection"]["model"],
        "fixture-model"
    );
    fixture.surface_choice(&mut surface, "Apply reviewed selection");
    let event = fixture.next_mutation();
    assert!(
        matches!(&event, Background::Mutation {op,fields,result:Ok(_),..}
        if op == "select-model" && fields["threadId"] == "thread-a"
            && fields["modelSelection"]["instanceId"] == "fixture-provider"
            && fields["modelSelection"]["model"] == "fixture-fast"
            && array(&fields["modelSelection"], "options").is_empty())
    );
    fixture.app.handle(event).unwrap();
}

#[test]
fn slt_model_picker_commits_only_the_reviewed_native_selection() {
    fn input(
        backend: &mut ::slt::TestBackend,
        fixture: &mut Fixture,
        surface: &mut crate::slt_ui::SurfaceState,
        events: Vec<::slt::Event>,
    ) {
        backend.run_with_events(events, |ctx| fixture.draw_slt(ctx, surface));
        for _ in 0..4 {
            backend.render(|ctx| fixture.draw_slt(ctx, surface));
        }
        assert!(
            !backend.has_pending_input(),
            "all modal inputs were handled"
        );
    }
    fn click(
        backend: &mut ::slt::TestBackend,
        fixture: &mut Fixture,
        surface: &mut crate::slt_ui::SurfaceState,
        label: &str,
    ) {
        let painted = backend.snapshot();
        let (y, line) = painted
            .lines()
            .enumerate()
            .filter(|(_, line)| line.contains(label))
            .last()
            .unwrap_or_else(|| panic!("visible modal control {label}:\n{painted}"));
        let x = unicode_width::UnicodeWidthStr::width(&line[..line.find(label).unwrap()]);
        input(
            backend,
            fixture,
            surface,
            ::slt::EventBuilder::new().click(x as u32, y as u32).build(),
        );
    }

    for (width, height, keyboard_apply) in [(140, 38, false), (70, 22, true), (40, 12, false)] {
        let mut fixture = Fixture::new(500);
        let config_path = &fixture.app.environments[0].host_config;
        let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
        config["fixture"]["persistInitialState"] = json!(true);
        fs::write(config_path, config.to_string()).unwrap();
        fixture.open(0);
        let draft = "Exact 日本語 café e\u{301}  draft\n\n";
        fixture.app.composer = ui::editor(draft, &fixture.app.theme, "");
        fixture.app.flush_draft().unwrap();
        let caret = fixture.app.composer.cursor();
        let original = fixture.app.metadata()["modelSelection"].clone();
        let native_before = fs::read(&fixture.native_states[0]).unwrap();
        let mut surface = crate::slt_ui::SurfaceState::new();
        fixture
            .app
            .surface_action(crate::slt_ui::Action::Command("model".into()), &mut surface)
            .unwrap();
        while fixture.app.model_edit.is_none() {
            let event = fixture.app.rx.recv_timeout(Duration::from_secs(5)).unwrap();
            fixture.app.handle(event).unwrap();
        }
        // A provider's display name may equal a client action. Keep its real native slug.
        fixture.app.model_edit.as_mut().unwrap().catalog["models"][0]["name"] =
            json!("Apply reviewed selection");
        fixture.app.model_menu().unwrap();
        let mut backend = ::slt::TestBackend::new(width, height)
            .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
        input(&mut backend, &mut fixture, &mut surface, Vec::new());
        backend.assert_contains("Apply reviewed");
        // The native model name collides with a former footer label. Space+Enter
        // chooses that same native value without applying the configuration.
        input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key(' ')
                .key_code(::slt::KeyCode::Enter)
                .build(),
        );
        assert_eq!(fixture.app.model_edit.as_ref().unwrap().selection, original);
        assert!(fixture.app.state.pending_records().is_empty());
        assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
        click(&mut backend, &mut fixture, &mut surface, "Apply reviewed");
        backend.assert_contains("✓ Apply reviewed");
        input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Down)
                .key_code(::slt::KeyCode::Enter)
                .build(),
        );
        let model_only = json!({"instanceId":"fixture-provider","model":"fixture-fast"});
        assert_eq!(
            fixture.app.model_edit.as_ref().unwrap().selection,
            model_only
        );
        // Child dismissal consumes the batch instead of leaking Enter into Apply.
        for key in [
            ::slt::KeyCode::Tab,
            ::slt::KeyCode::BackTab,
            ::slt::KeyCode::Esc,
        ] {
            click(&mut backend, &mut fixture, &mut surface, "Reasoning");
            assert!(matches!(fixture.app.modal().unwrap().model,
                Some(ui::ModelDialog::OptionValues { ref id }) if id == "reasoningEffort"));
            input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(key)
                    .key_code(::slt::KeyCode::Enter)
                    .build(),
            );
            assert_eq!(
                fixture.app.modal().unwrap().model,
                Some(ui::ModelDialog::Configuration)
            );
            assert_eq!(
                fixture.app.model_edit.as_ref().unwrap().selection,
                model_only
            );
            assert!(fixture.app.state.pending_records().is_empty());
            assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
        }
        click(&mut backend, &mut fixture, &mut surface, "Reasoning");
        assert!(fixture.app.modal().unwrap().items[0].checked);
        input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::End)
                .key_code(::slt::KeyCode::Enter)
                .build(),
        );
        click(&mut backend, &mut fixture, &mut surface, "Speed");
        input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::End)
                .key_code(::slt::KeyCode::Enter)
                .build(),
        );
        let staged = json!({"instanceId":"fixture-provider","model":"fixture-fast","options":[
            {"id":"reasoningEffort","value":"high"},{"id":"serviceTier","value":"fast"}
        ]});
        assert_eq!(fixture.app.model_edit.as_ref().unwrap().selection, staged);
        let modal = fixture.app.modal().unwrap();
        assert_eq!(
            modal
                .items
                .iter()
                .filter(|item| item.checked)
                .map(|item| item.label.as_str())
                .collect::<Vec<_>>(),
            ["Fixture fast"]
        );
        assert_eq!(fixture.app.metadata()["modelSelection"], original);
        assert!(fixture.app.state.pending_records().is_empty());
        assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
        input(&mut backend, &mut fixture, &mut surface, Vec::new());

        click(&mut backend, &mut fixture, &mut surface, "Speed");
        let (x, y) = backend.find_text("Apply ↵").unwrap();
        input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .click(x, y)
                .key_code(::slt::KeyCode::Enter)
                .build(),
        );
        assert_eq!(
            fixture.app.modal().unwrap().model,
            Some(ui::ModelDialog::Configuration)
        );
        assert_eq!(fixture.app.model_edit.as_ref().unwrap().selection, staged);
        assert!(fixture.app.state.pending_records().is_empty());
        assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);

        if keyboard_apply {
            click(&mut backend, &mut fixture, &mut surface, "Save favorite");
        } else {
            input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Tab)
                    .key_code(::slt::KeyCode::Tab)
                    .key(' ')
                    .build(),
            );
        }
        assert_eq!(fixture.app.modal().unwrap().title, "Save model favorite");
        fixture.app.modal_editor = ui::editor("Reviewed favorite", &fixture.app.theme, "");
        input(&mut backend, &mut fixture, &mut surface, Vec::new());
        click(&mut backend, &mut fixture, &mut surface, "Continue");
        assert_eq!(
            fixture.app.state.snapshot()["modelPresets"]["native-0"][0]["selection"],
            staged
        );
        assert_eq!(fixture.app.model_edit.as_ref().unwrap().selection, staged);
        assert!(fixture.app.state.pending_records().is_empty());
        assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);

        if keyboard_apply {
            input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Tab)
                    .key_code(::slt::KeyCode::Tab)
                    .key_code(::slt::KeyCode::Tab)
                    .key_code(::slt::KeyCode::Tab)
                    .key_code(::slt::KeyCode::Enter)
                    .build(),
            );
        } else {
            click(&mut backend, &mut fixture, &mut surface, "Apply ↵");
        }
        let pending = fixture.app.state.pending("native-0", false).unwrap();
        assert_eq!(pending["op"], "select-model");
        assert_eq!(pending["threadId"], "thread-a");
        assert_eq!(pending["modelSelection"], staged);
        assert_eq!(pending["expectedModelSelection"], original);
        assert_eq!(fixture.app.state.pending_records().len(), 1);
        let event = fixture.next_mutation();
        assert!(
            matches!(&event, Background::Mutation {context, id, op, fields, result: Ok(_)}
            if context.profile == 0 && *id == pending["id"] && op == "select-model"
                && fields["threadId"] == "thread-a" && fields["modelSelection"] == staged
                && fields["expectedModelSelection"] == original)
        );
        fixture.app.handle(event).unwrap();
        assert!(fixture.app.state.pending_records().is_empty());
        assert_eq!(fixture.app.text(), draft);
        assert_eq!(fixture.app.composer.cursor(), caret);
        assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
        let native: Value =
            serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
        assert_eq!(native["effects"], 1);
    }
}

fn model_tail_input(
    backend: &mut ::slt::TestBackend,
    fixture: &mut Fixture,
    surface: &mut crate::slt_ui::SurfaceState,
    events: Vec<::slt::Event>,
) -> Option<String> {
    backend.run_with_events(events, |ctx| fixture.draw_slt(ctx, surface));
    let mut focus = None;
    for _ in 0..8 {
        backend.render(|ctx| {
            fixture.draw_slt(ctx, surface);
            focus = ctx.focused_name().map(str::to_owned);
        });
    }
    assert!(!backend.has_pending_input(), "modal input tail drained");
    focus
}

fn staged_model_tail_fixture() -> (Fixture, crate::slt_ui::SurfaceState, ::slt::TestBackend) {
    let mut fixture = Fixture::new(500);
    let config_path = &fixture.app.environments[0].host_config;
    let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
    config["fixture"]["persistInitialState"] = json!(true);
    fs::write(config_path, config.to_string()).unwrap();
    fixture.open(0);
    fixture.app.composer = ui::editor(
        "Tail 日本語 café e\u{301} draft\n\n",
        &fixture.app.theme,
        "",
    );
    fixture.app.flush_draft().unwrap();
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(crate::slt_ui::Action::Command("model".into()), &mut surface)
        .unwrap();
    while fixture.app.model_edit.is_none() {
        let event = fixture.app.rx.recv_timeout(Duration::from_secs(5)).unwrap();
        fixture.app.handle(event).unwrap();
    }
    let mut backend = ::slt::TestBackend::new(70, 22)
        .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
    model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key(' ')
            .key_code(::slt::KeyCode::Down)
            .key_code(::slt::KeyCode::Enter)
            .build(),
    );
    assert_eq!(
        fixture.app.model_edit.as_ref().unwrap().selection,
        json!({"instanceId":"fixture-provider","model":"fixture-fast"})
    );
    assert!(fixture.app.state.pending_records().is_empty());
    (fixture, surface, backend)
}

fn assert_local_model_dismissal_tail(key: Option<::slt::KeyCode>) {
    let (mut fixture, mut surface, mut backend) = staged_model_tail_fixture();
    let draft = fixture.app.text();
    let caret = fixture.app.composer.cursor();
    let original = fixture.app.metadata()["modelSelection"].clone();
    let staged = fixture.app.model_edit.as_ref().unwrap().selection.clone();
    let native_before = fs::read(&fixture.native_states[0]).unwrap();
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().key(' ').build(),
    );
    backend.assert_contains("✓ Fixture fast");
    let events = if let Some(key) = &key {
        ::slt::EventBuilder::new()
            .key_code(key.clone())
            .key_code(::slt::KeyCode::Enter)
            .build()
    } else {
        let (x, y) = backend.find_text("Apply ↵").expect("painted outside Apply");
        ::slt::EventBuilder::new()
            .click(x, y)
            .key_code(::slt::KeyCode::Enter)
            .build()
    };
    model_tail_input(&mut backend, &mut fixture, &mut surface, events);
    assert!(
        fixture.app.state.pending_records().is_empty(),
        "child dismissal must consume its queued Enter without a native write"
    );
    assert_eq!(
        fixture.app.modal().unwrap().model,
        Some(ui::ModelDialog::Configuration)
    );
    assert_eq!(fixture.app.model_edit.as_ref().unwrap().selection, staged);
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.composer.cursor(), caret);
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
    // Dismiss again without a queued tail. Its requested idle frames must expire
    // ownership before a later deliberate Enter, including after pointer release.
    let (x, y) = backend
        .find_text("Fixture fast")
        .expect("painted staged Model");
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().click(x, y).build(),
    );
    backend.assert_contains("✓ Fixture fast");
    let events = if let Some(key) = key {
        ::slt::EventBuilder::new().key_code(key).build()
    } else {
        let (x, y) = backend.find_text("Apply ↵").expect("painted outside Apply");
        ::slt::EventBuilder::new().click(x, y).build()
    };
    model_tail_input(&mut backend, &mut fixture, &mut surface, events);
    assert!(fixture.app.state.pending_records().is_empty());
    assert_eq!(
        fixture.app.modal().unwrap().model,
        Some(ui::ModelDialog::Configuration)
    );
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Enter)
            .build(),
    );
    let pending = fixture.app.state.pending("native-0", false).unwrap();
    assert_eq!(pending["op"], "select-model");
    assert_eq!(pending["threadId"], "thread-a");
    assert_eq!(pending["modelSelection"], staged);
    assert_eq!(pending["expectedModelSelection"], original);
    assert_eq!(fixture.app.state.pending_records().len(), 1);
    let event = fixture.next_mutation();
    assert!(
        matches!(&event, Background::Mutation { id, op, fields, result: Ok(_), .. }
        if *id == pending["id"] && op == "select-model"
            && fields["threadId"] == "thread-a" && fields["modelSelection"] == staged
            && fields["expectedModelSelection"] == original)
    );
    fixture.app.handle(event).unwrap();
    assert!(fixture.app.state.pending_records().is_empty());
    let native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
    assert_eq!(native["effects"], 1);
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.composer.cursor(), caret);
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
}

#[test]
fn slt_model_local_tab_dismissal_owns_queued_enter() {
    assert_local_model_dismissal_tail(Some(::slt::KeyCode::Tab));
}

#[test]
fn slt_model_local_backtab_dismissal_owns_queued_enter() {
    assert_local_model_dismissal_tail(Some(::slt::KeyCode::BackTab));
}

#[test]
fn slt_model_local_escape_dismissal_owns_queued_enter() {
    assert_local_model_dismissal_tail(Some(::slt::KeyCode::Esc));
}

#[test]
fn slt_model_local_pointer_dismissal_owns_queued_enter() {
    assert_local_model_dismissal_tail(None);
}

#[test]
fn slt_model_close_focus_survives_settle_and_space_closes_once() {
    let (mut fixture, mut surface, mut backend) = staged_model_tail_fixture();
    let draft = fixture.app.text();
    let caret = fixture.app.composer.cursor();
    let native_before = fs::read(&fixture.native_states[0]).unwrap();
    let focus = model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::BackTab)
            .build(),
    );
    assert_eq!(focus.as_deref(), Some("model-close"));
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().key(' ').build(),
    );
    assert!(
        fixture.app.modal().is_none(),
        "Space on × must close the outer modal"
    );
    assert_eq!(fixture.slt_closes, 1);
    assert!(fixture.app.state.pending_records().is_empty());
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.composer.cursor(), caret);
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
}

#[test]
fn slt_model_pointer_close_restores_tools_then_keyboard_returns_preserved_caret() {
    let mut fixture = Fixture::new(0);
    let config_path = &fixture.app.environments[0].host_config;
    let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
    config["fixture"]["persistInitialState"] = json!(true);
    fs::write(config_path, config.to_string()).unwrap();
    fixture.open(0);
    let draft = "Tail 日本語 café e\u{301} 👩\u{200d}💻  \nold tail  \n";
    fixture.app.composer = ui::editor(draft, &fixture.app.theme, "");
    fixture.app.flush_draft().unwrap();
    let other = "Other source 日本語  \n";
    fixture
        .app
        .state
        .set_draft("native-0", "thread-review", other)
        .unwrap();
    let pending = json!({"id":"model-preserved-operation","environmentId":"native-0",
        "threadId":"thread-review","op":"send","message":"Existing uncertain intent"});
    fixture
        .app
        .state
        .save_pending("native-0", false, pending.clone())
        .unwrap();
    let native_before = fs::read(&fixture.native_states[0]).unwrap();
    let mut surface = crate::slt_ui::SurfaceState::new();
    let mut backend = ::slt::TestBackend::new(70, 22)
        .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
    assert_eq!(
        model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new()).as_deref(),
        Some("composer")
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Up)
            .key_code(::slt::KeyCode::End)
            .key_code(::slt::KeyCode::Left)
            .key_code(::slt::KeyCode::Left)
            .build(),
    );
    backend.assert_contains("old tail▎");
    let caret = backend.find_text("▎").expect("actual interior SLT caret");
    let (x, y) = backend
        .find_text("Model")
        .expect("painted composer Model control");
    assert_eq!(
        model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new().click(x, y).build(),
        )
        .as_deref(),
        Some("composer-tools"),
        "pointer activation gives the invoking action bar ownership"
    );
    while fixture.app.model_edit.is_none() {
        let event = fixture.app.rx.recv_timeout(Duration::from_secs(5)).unwrap();
        fixture.app.handle(event).unwrap();
    }
    model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    assert_eq!(
        model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::BackTab)
                .build(),
        )
        .as_deref(),
        Some("model-close")
    );
    assert_eq!(
        model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new().key(' ').build(),
        )
        .as_deref(),
        Some("composer-tools"),
        "close restores the original invoking owner"
    );
    assert!(fixture.app.modal().is_none());
    assert_eq!(fixture.slt_closes, 1);
    backend.assert_not_contains("▎");
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(
        fixture.app.state.pending("native-0", false),
        Some(pending.clone())
    );
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
    assert_eq!(
        model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_with(::slt::KeyCode::F(6), ::slt::KeyModifiers::SHIFT)
                .build(),
        )
        .as_deref(),
        Some("composer")
    );
    assert_eq!(backend.find_text("▎"), Some(caret));
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key('<')
            .key('c')
            .key('a')
            .key('r')
            .key('e')
            .key('t')
            .key('>')
            .build(),
    );
    assert_eq!(
        fixture.app.text(),
        draft.replace("old tail  \n", "old tail<caret>  \n")
    );
    backend.assert_contains("old tail<caret>▎");
    let mut restore = ::slt::EventBuilder::new();
    for _ in 0.."<caret>".len() {
        restore = restore.key_code(::slt::KeyCode::Backspace);
    }
    model_tail_input(&mut backend, &mut fixture, &mut surface, restore.build());
    fixture.app.flush_draft().unwrap();
    assert_eq!(backend.find_text("▎"), Some(caret));
    assert_eq!(backend.to_string_trimmed().matches('▎').count(), 1);
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
    assert_eq!(fixture.app.state.draft("native-0", "thread-review"), other);
    assert_eq!(fixture.app.state.pending("native-0", false), Some(pending));
    assert_eq!(fixture.app.state.pending_records().len(), 1);
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
}

#[test]
fn slt_direct_effort_cancel_restores_invoker_and_panes_without_phantom_model() {
    let mut fixture = Fixture::new(0);
    let config_path = &fixture.app.environments[0].host_config;
    let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
    config["fixture"]["persistInitialState"] = json!(true);
    fs::write(config_path, config.to_string()).unwrap();
    fixture.load_pages();
    fixture.open(0);
    fixture.drain_pages();
    let mut surface = crate::slt_ui::SurfaceState::new();
    let mut backend = ::slt::TestBackend::new(140, 38)
        .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
    model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    let (x, y) = backend.find_text("Prompt").unwrap();
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .click(x + 1, y + 1)
            .paste("focus-draft")
            .build(),
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Tab)
            .key_code(::slt::KeyCode::BackTab)
            .build(),
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().paste("+tab").build(),
    );
    assert_eq!(fixture.app.text(), "focus-draft+tab");
    fixture.app.flush_draft().unwrap();
    let draft = fixture.app.text();
    let caret = fixture.app.composer.cursor();
    fixture
        .app
        .state
        .set_draft("native-0", "thread-review", "Other 日本語 draft")
        .unwrap();
    let pending = json!({"id":"effort-preserved-operation","environmentId":"native-0",
        "threadId":"thread-review","op":"send","message":"Existing uncertain intent"});
    fixture
        .app
        .state
        .save_pending("native-0", false, pending.clone())
        .unwrap();
    fixture.app.preserve_workspace_position().unwrap();
    let state = fixture.app.state.snapshot();
    let native = fs::read(&fixture.native_states[0]).unwrap();
    let mode = fs::metadata(&fixture.native_states[0])
        .unwrap()
        .permissions()
        .mode();
    let journal = fixture.directory.path().join("host-0/operations-v1");
    assert!(!journal.exists());
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Tab)
            .build(),
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Right)
            .build(),
    );
    assert_eq!(
        model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Enter)
                .build()
        )
        .as_deref(),
        Some("composer-tools")
    );
    fixture.finish_effort_read();
    model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    backend.assert_contains("Reasoning");
    backend.assert_contains("Native default");
    backend.assert_contains("Medium");
    backend.assert_contains("High");
    backend.assert_not_contains("Model configuration");
    let staged = fixture.app.model_edit.as_ref().unwrap().selection.clone();
    let returned_focus = model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Esc)
            .build(),
    );
    assert!(
        fixture.app.modal().is_none(),
        "direct Effort cancellation must not invent a Model parent"
    );
    assert_eq!(returned_focus.as_deref(), Some("composer-tools"));
    backend.assert_not_contains("Model configuration");
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::F(3))
            .build(),
    );
    backend.assert_not_contains("Unread");
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::F(3))
            .build(),
    );
    backend.assert_contains("Unread");
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::F(4))
            .build(),
    );
    backend.assert_contains("Copy ID");
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::F(4))
            .build(),
    );
    backend.assert_not_contains("Copy ID");
    assert_eq!(
        model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_with(::slt::KeyCode::F(6), ::slt::KeyModifiers::SHIFT)
                .build()
        )
        .as_deref(),
        Some("composer")
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().paste("日本語").build(),
    );
    assert_eq!(fixture.app.text(), format!("{draft}日本語"));
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Backspace)
            .key_code(::slt::KeyCode::Backspace)
            .key_code(::slt::KeyCode::Backspace)
            .build(),
    );
    fixture.app.flush_draft().unwrap();
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.composer.cursor(), caret);
    assert_eq!(fixture.app.model_edit.as_ref().unwrap().selection, staged);
    assert_eq!(fixture.app.state.snapshot(), state);
    assert_eq!(fixture.app.state.pending("native-0", false), Some(pending));
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native);
    assert_eq!(
        fs::metadata(&fixture.native_states[0])
            .unwrap()
            .permissions()
            .mode(),
        mode
    );
    assert!(!journal.exists());
}

#[test]
fn slt_direct_effort_dismissal_owns_queued_enter_without_applying_or_sending() {
    for key in [
        Some(::slt::KeyCode::Esc),
        Some(::slt::KeyCode::Tab),
        Some(::slt::KeyCode::BackTab),
        None,
    ] {
        let mut fixture = Fixture::new(0);
        let config_path = &fixture.app.environments[0].host_config;
        let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
        config["fixture"]["persistInitialState"] = json!(true);
        fs::write(config_path, config.to_string()).unwrap();
        fixture.open(0);
        fixture.app.composer = ui::editor("Unsent 日本語 draft", &fixture.app.theme, "");
        fixture.app.flush_draft().unwrap();
        let draft = fixture.app.text();
        let caret = fixture.app.composer.cursor();
        let native = fs::read(&fixture.native_states[0]).unwrap();
        let mut surface = crate::slt_ui::SurfaceState::new();
        let mut backend = ::slt::TestBackend::new(140, 38)
            .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
        model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
        let (x, y) = backend.find_text("Effort").unwrap();
        model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new().click(x, y).build(),
        );
        fixture.finish_effort_read();
        model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
        backend.assert_contains("Reasoning");
        let selection = fixture.app.model_edit.as_ref().unwrap().selection.clone();
        let read_serial = fixture.app.serial;
        let events = if let Some(key) = key.clone() {
            ::slt::EventBuilder::new()
                .key_code(key)
                .key_code(::slt::KeyCode::Enter)
                .build()
        } else {
            ::slt::EventBuilder::new()
                .click(1, 1)
                .key_code(::slt::KeyCode::Enter)
                .build()
        };
        model_tail_input(&mut backend, &mut fixture, &mut surface, events);
        assert!(
            fixture.app.modal().is_none(),
            "direct dismissal {key:?} must close without reopening a modal"
        );
        assert_eq!(
            fixture.app.serial,
            read_serial.wrapping_add(1),
            "dismissal cannot reopen a native catalog read"
        );
        assert_eq!(
            fixture.app.model_edit.as_ref().unwrap().selection,
            selection
        );
        assert!(fixture.app.state.pending_records().is_empty());
        assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native);
        assert_eq!(fixture.app.text(), draft);
        assert_eq!(fixture.app.composer.cursor(), caret);
        if key == Some(::slt::KeyCode::Esc) {
            // Accepting the unchanged direct value still opens explicit Apply review.
            let (x, y) = backend.find_text("Effort").unwrap();
            model_tail_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new().click(x, y).build(),
            );
            fixture.finish_effort_read();
            model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            model_tail_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Enter)
                    .build(),
            );
            assert_eq!(
                fixture.app.modal().unwrap().model,
                Some(ui::ModelDialog::Configuration)
            );
            assert_eq!(
                fixture.app.model_edit.as_ref().unwrap().selection,
                selection
            );
            // The next real child must own Configuration, not the prior direct origin.
            let (x, y) = backend.find_text("Reasoning").unwrap();
            model_tail_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new().click(x, y).build(),
            );
            model_tail_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Esc)
                    .build(),
            );
            assert_eq!(
                fixture.app.modal().unwrap().model,
                Some(ui::ModelDialog::Configuration)
            );
            assert_eq!(
                fixture.app.model_edit.as_ref().unwrap().selection,
                selection
            );
            assert!(fixture.app.state.pending_records().is_empty());
            assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native);
            assert_eq!(fixture.app.text(), draft);
            assert_eq!(fixture.app.composer.cursor(), caret);
        }
    }
}

#[test]
fn surface_effort_next_edits_the_existing_override_without_native_write() {
    let mut fixture = Fixture::new(0);
    fixture.open(0);
    let selection = json!({"instanceId":"fixture-provider","model":"fixture-fast","options":[{"id":"reasoningEffort","value":"high"}]});
    fixture
        .app
        .state
        .update(|state| {
            state["modelOverrides"]["native-0/thread-a"] =
                json!({"id":"existing-override","selection":selection});
            Ok(())
        })
        .unwrap();
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("effort next".into()),
            &mut surface,
        )
        .unwrap();
    fixture.finish_effort_read();
    assert_eq!(
        fixture.app.model_edit.as_ref().unwrap().selection,
        selection
    );
    assert_eq!(
        fixture
            .app
            .modal()
            .unwrap()
            .items
            .iter()
            .filter(|item| item.checked)
            .map(|item| item.label.as_str())
            .collect::<Vec<_>>(),
        ["High"]
    );
    fixture.surface_choice(&mut surface, "Native default");
    fixture.surface_choice(&mut surface, "Apply reviewed selection");
    let staged =
        fixture.app.state.snapshot()["modelOverrides"]["native-0/thread-a"]["selection"].clone();
    assert_eq!(staged["instanceId"], "fixture-provider");
    assert_eq!(staged["model"], "fixture-fast");
    assert!(array(&staged, "options").is_empty());
    assert_eq!(
        fixture.app.metadata()["modelSelection"]["model"],
        "fixture-model"
    );
    assert!(fixture.app.state.pending_records().is_empty());
}

#[test]
fn effort_requires_an_unambiguous_advertised_control_and_keeps_environment_scope() {
    let mut fixture = Fixture::new(0);
    fixture.open(0);
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("effort".into()),
            &mut surface,
        )
        .unwrap();
    fixture.finish_effort_read();
    let catalog = fixture.app.model_edit.as_ref().unwrap().catalog.clone();
    let mut foreign = fixture.app.model_edit.as_ref().unwrap().clone();
    foreign.environment_id = "native-1".into();
    foreign.selection["model"] = json!("fixture-fast");
    fixture.app.model_edit = Some(foreign);
    fixture
        .app
        .read_result(ReadKind::Effort(ModelScope::Current), catalog.clone())
        .unwrap();
    assert_eq!(
        fixture.app.model_edit.as_ref().unwrap().selection,
        catalog["current"]
    );
    for options in [
        json!([{"id":"guess-me","label":"Effort","type":"select","options":[{"id":"value","label":"Value"}]}]),
        json!([
            {"id":"effort","type":"select","options":[{"id":"one","label":"One"}]},
            {"id":"reasoningEffort","type":"select","options":[{"id":"two","label":"Two"}]}
        ]),
    ] {
        let mut unsupported = catalog.clone();
        unsupported["models"][0]["options"] = options;
        fixture
            .app
            .read_result(ReadKind::Effort(ModelScope::Current), unsupported)
            .unwrap();
        let dialog = fixture.app.dialog.as_ref().unwrap();
        assert_eq!(dialog.title, "Effort unavailable");
        assert!(
            matches!(&dialog.choices[0].intent, Intent::Command(command) if command == "model")
        );
        assert!(fixture.app.state.pending_records().is_empty());
    }
}

#[test]
fn surface_answer_delivery_waits_for_exact_ack_and_lost_ack_retains_original_operation() {
    for lost in [false, true] {
        let mut fixture = Fixture::new(1200);
        if lost {
            let path = &fixture.app.environments[0].host_config;
            let mut config: Value = serde_json::from_slice(&fs::read(path).unwrap()).unwrap();
            config["fixture"]["dropAckOnce"] = json!(true);
            fs::write(path, config.to_string()).unwrap();
        }
        fixture.open_thread(0, "thread-question");
        let mut surface = crate::slt_ui::SurfaceState::new();
        fixture.app.populate_surface(&mut surface);
        let question = surface
            .questions
            .iter()
            .find(|item| item.source.thread == "thread-question")
            .unwrap()
            .clone();
        let source = crate::attention::Source::from_request(
            "native-0",
            "thread-question",
            &question.request,
        )
        .unwrap();
        let key = source.key().unwrap();
        surface.retained_question = Some(question.clone());
        fixture
            .app
            .surface_action(crate::slt_ui::Action::Answer(question), &mut surface)
            .unwrap();
        fixture.surface_choice(&mut surface, "Continue");
        fixture.surface_choice(&mut surface, "Next");
        assert!(fixture.app.state.pending_records().is_empty());
        fixture.surface_choice(&mut surface, "Submit answers");
        let pending = fixture.app.state.pending("native-0", false).unwrap();
        assert_eq!(pending["op"], "answer");
        assert_eq!(pending["requestId"], source.request_id);
        assert_eq!(pending["revision"], source.revision);
        assert_eq!(pending["answers"]["choice"], "Continue");
        fixture.app.populate_surface(&mut surface);
        assert_eq!(surface.answer_delivery[&key], "submitting");
        fixture.wait_native_effect(0);
        // The native effect can remove the offer before its delayed ACK returns.
        fixture.app.active.as_mut().unwrap().requests.clear();
        fixture.app.notice = "Unrelated host refreshed".into();
        fixture.app.populate_surface(&mut surface);
        assert!(surface.questions.is_empty());
        assert_eq!(surface.answer_delivery[&key], "submitting");
        assert_eq!(
            fixture.app.state.pending("native-0", false),
            Some(pending.clone())
        );
        let event = fixture.next_mutation();
        assert!(
            matches!(&event, Background::Mutation {id, fields, result, ..}
            if pending["id"] == *id && fields["revision"] == pending["revision"] && result.is_err() == lost)
        );
        fixture.app.handle(event).unwrap();
        fixture.app.populate_surface(&mut surface);
        assert_eq!(
            surface.answer_delivery[&key],
            if lost { "uncertain" } else { "acknowledged" }
        );
        assert!(surface.retained_question.is_some());
        if lost {
            assert_eq!(
                fixture.app.state.pending("native-0", false),
                Some(pending.clone())
            );
            fixture.app.answer_deliveries.clear();
            fixture.app.populate_surface(&mut surface);
            assert_eq!(
                surface.answer_delivery[&key], "uncertain",
                "durable pending survives presentation reset"
            );
            fixture.load_pages();
            assert_eq!(fixture.app.state.pending("native-0", false), Some(pending));
            let native: Value =
                serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
            assert_eq!(
                native["effects"], 1,
                "refresh must not replay an uncertain answer"
            );
        } else {
            assert!(fixture.app.state.pending_records().is_empty());
        }
        for changed in [
            crate::attention::Source {
                environment_id: "native-1".into(),
                ..source.clone()
            },
            crate::attention::Source {
                thread_id: "thread-other".into(),
                ..source.clone()
            },
            crate::attention::Source {
                request_id: "request-other".into(),
                ..source.clone()
            },
            crate::attention::Source {
                revision: "a".repeat(64),
                ..source.clone()
            },
        ] {
            assert!(
                !surface
                    .answer_delivery
                    .contains_key(&changed.key().unwrap())
            );
        }
    }
}

#[test]
fn malformed_answer_ack_never_marks_success_and_known_rejection_can_retry() {
    let mut fixture = Fixture::new(0);
    let revision = "b".repeat(64);
    let fields = json!({"threadId":"thread-question","requestId":"request-question","revision":revision,"answers":{"choice":"Continue"}});
    let mut pending = fields.clone();
    pending["id"] = json!("original-answer-operation");
    pending["op"] = json!("answer");
    pending["environmentId"] = json!("native-0");
    fixture
        .app
        .state
        .save_pending("native-0", false, pending.clone())
        .unwrap();
    let source = crate::attention::Source::from_request(
        "native-0",
        "thread-question",
        &json!({"id":"request-question","revision":revision}),
    )
    .unwrap();
    let key = source.key().unwrap();
    let context = MutationContext {
        generation: 0,
        profile: 0,
        draft_version: None,
    };
    for field in [
        "environmentId",
        "threadId",
        "requestId",
        "revision",
        "operationId",
    ] {
        let mut ack = json!({"sequence":2,"acknowledgement":{"environmentId":"native-0","threadId":"thread-question","requestId":"request-question","revision":revision,"operationId":"original-answer-operation"}});
        ack["acknowledgement"][field] = json!("mismatched");
        assert!(
            fixture
                .app
                .handle(Background::Mutation {
                    context,
                    id: "original-answer-operation".into(),
                    op: "answer".into(),
                    fields: fields.clone(),
                    result: Ok(ack)
                })
                .is_err()
        );
        let mut surface = crate::slt_ui::SurfaceState::new();
        fixture.app.populate_surface(&mut surface);
        assert_eq!(surface.answer_delivery[&key], "uncertain");
        assert_eq!(
            fixture.app.state.pending("native-0", false),
            Some(pending.clone())
        );
    }
    fixture
        .app
        .state
        .reject_pending("native-0", false, "original-answer-operation")
        .unwrap();
    fixture
        .app
        .handle(Background::Mutation {
            context,
            id: "original-answer-operation".into(),
            op: "answer".into(),
            fields: fields.clone(),
            result: Err("Known native rejection".into()),
        })
        .unwrap();
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    assert_eq!(surface.answer_delivery[&key], "rejected");
    fixture.app.remember_answer_delivery(
        0,
        "new-answer-operation",
        &fields,
        AnswerPhase::Submitting,
    );
    fixture
        .app
        .handle(Background::Mutation {
            context,
            id: "original-answer-operation".into(),
            op: "answer".into(),
            fields,
            result: Err("Late old rejection".into()),
        })
        .unwrap();
    fixture.app.populate_surface(&mut surface);
    assert_eq!(
        surface.answer_delivery[&key], "submitting",
        "old results cannot replace a later operation's phase"
    );
}

#[test]
fn inherited_content_resolves_project_metadata_without_a_new_message_revision() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    let active = fixture.app.active.as_ref().unwrap();
    let generation = active.generation;
    let mut snapshot = active.projection.snapshot().unwrap().as_wire_value();
    snapshot["revision"] = json!(snapshot["revision"].as_u64().unwrap() + 1);
    let mut inherited = snapshot["items"]
        .as_array()
        .unwrap()
        .iter()
        .find(|row| row["kind"] == "assistant_message")
        .unwrap()
        .clone();
    inherited["sourceThreadId"] = json!("unloaded-parent");
    inherited["key"] = json!(format!(
        "unloaded-parent/{}",
        inherited["itemId"].as_str().unwrap()
    ));
    snapshot["items"].as_array_mut().unwrap().push(inherited);
    fixture
        .app
        .handle(Background::Packet {
            generation,
            packet: Packet::event("native-0", "view", snapshot),
        })
        .unwrap();
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    let before = surface
        .contents
        .iter()
        .find(|item| item.source.thread == "unloaded-parent")
        .expect("unresolved inherited content remains readable")
        .clone();
    assert!(before.source.project_id.is_empty());
    let mut parent = fixture
        .app
        .threads
        .iter()
        .find(|row| row.id == "thread-review")
        .unwrap()
        .clone();
    parent.id = "unloaded-parent".into();
    fixture.app.threads.push(parent);
    fixture.app.populate_surface(&mut surface);
    let after = surface
        .contents
        .iter()
        .find(|item| item.key == before.key)
        .unwrap();
    assert_eq!(after.source.project_id, "project-a");
    assert_eq!(
        (&after.revision, &after.markdown),
        (&before.revision, &before.markdown)
    );
    assert!(fixture.app.state.pending_records().is_empty());
}

#[test]
fn loaded_reports_are_independent_source_qualified_and_retrievable_after_tab_switch() {
    use crate::context_store::{Query, Scope};
    let mut fixture = Fixture::new(0);
    fixture.open_thread(0, "thread-review");
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    assert!(surface.questions.is_empty());
    let report = surface
        .contents
        .iter()
        .find(|item| item.source.environment == "native-0")
        .expect("a report needs no pending question")
        .clone();
    assert_eq!(report.kind, "explanation");
    assert_eq!(report.source.thread, "thread-review");
    assert_eq!(report.source.project_id, "project-a");
    assert!(report.markdown.contains("日本語 café 🙂"));
    let title = surface.tabs[0].title.clone();
    assert_eq!(title, "Finished refactor");
    fixture.open_thread(1, "thread-fork");
    let active = fixture.app.active.as_ref().unwrap();
    let generation = active.generation;
    let mut snapshot = active.projection.snapshot().unwrap().as_wire_value();
    snapshot["revision"] = json!(snapshot["revision"].as_u64().unwrap() + 1);
    let message = snapshot["items"]
        .as_array_mut()
        .unwrap()
        .iter_mut()
        .find(|row| row["kind"] == "assistant_message")
        .unwrap();
    message["text"] = json!(format!(
        "## Guide\n{}\nFull-payload-needle",
        "source evidence ".repeat(150)
    ));
    message["truncated"] = json!(true);
    fixture
        .app
        .handle(Background::Packet {
            generation,
            packet: Packet::event("native-1", "view", snapshot),
        })
        .unwrap();
    fixture.app.populate_surface(&mut surface);
    assert!(
        surface
            .contents
            .iter()
            .any(|item| item.key == report.key && item.revision == report.revision)
    );
    let guide = surface
        .contents
        .iter()
        .find(|item| item.source.environment == "native-1" && item.kind == "guide")
        .unwrap();
    assert!(guide.truncated && guide.markdown.ends_with("Full-payload-needle"));
    assert_eq!(
        surface.tabs[0].title, title,
        "snapshot title survives absent metadata pages"
    );
    let deadline = Instant::now() + Duration::from_secs(8);
    while fixture.app.context_capture_inflight || !fixture.app.context_capture_pending.is_empty() {
        let event = fixture
            .app
            .rx
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .expect("durable loaded evidence");
        fixture.app.handle(event).unwrap();
    }
    let mut query = Query::new(Scope {
        environment_id: "native-1".into(),
        project_id: Some("project-a".into()),
        thread_id: Some("thread-fork".into()),
    });
    query.text = "Full-payload-needle".into();
    let pack = fixture.context_pack(query);
    assert_eq!(
        pack.eligible, 1,
        "recall searches evidence beyond its compact summary"
    );
    assert_eq!(pack.rows[0].source.observed["truncated"], true);
    assert!(pack.truncated, "an excerpt cannot become complete evidence");
    assert!(fixture.app.state.pending_records().is_empty());
    assert!(
        !fixture.native_states.iter().any(|path| path.exists()),
        "content reads never dispatch native mutations"
    );
}

#[test]
fn changed_or_cancelled_request_preserves_fresh_unsaved_answer_on_its_original_source() {
    for cancelled in [false, true] {
        let mut fixture = Fixture::new(0);
        let log = fixture.directory.path().join("native-requests.jsonl");
        let config_path = fixture.directory.path().join("host-0/host.json");
        let mut config: Value = serde_json::from_slice(&fs::read(&config_path).unwrap()).unwrap();
        config["fixture"]["logFile"] = json!(log);
        fs::write(&config_path, serde_json::to_vec(&config).unwrap()).unwrap();
        fixture.open_thread(0, "thread-question");

        let active = fixture.app.active.as_ref().unwrap();
        let generation = active.generation;
        let mut page = active.projection.snapshot().unwrap().as_wire_value();
        let revision = page["revision"].as_u64().unwrap();
        let mut original = active.requests[0].clone();
        original["revision"] = json!("a".repeat(64));
        original["questions"][0]["allowCustomAnswer"] = json!(true);
        page["revision"] = json!(revision + 1);
        page["requests"] = json!([original.clone()]);
        fixture
            .app
            .handle(Background::Packet {
                generation,
                packet: Packet::event("native-0", "view", page.clone()),
            })
            .unwrap();
        fixture.app.review_request(original.clone()).unwrap();
        fixture.app.question_option(json!("Continue")).unwrap();
        let form =
            crate::attention::Form::from_request("native-0", "thread-question", &original).unwrap();
        assert_eq!(
            form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
            "Continue"
        );

        fixture.app.edit_field(InputTarget::Question).unwrap();
        fixture.app.modal_editor.select_all();
        let freshest = "Fresh résumé 🙂\n日本語  \n  unfinished ";
        fixture
            .app
            .input(crossterm::event::Event::Paste(freshest.into()))
            .unwrap();
        assert!(
            fixture.app.form_save_at.is_some(),
            "answer edit must still await its debounce"
        );
        assert_eq!(
            fixture.app.request_edit.as_ref().unwrap().answers["choice"],
            "Continue"
        );
        assert_eq!(
            form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
            "Continue"
        );

        let mut replacement = original.clone();
        replacement["revision"] = json!("b".repeat(64));
        replacement["questions"][0]["question"] = json!("Changed native wording");
        page["revision"] = json!(revision + 2);
        page["requests"] = if cancelled {
            json!([])
        } else {
            json!([replacement.clone()])
        };
        fixture
            .app
            .handle(Background::Packet {
                generation,
                packet: Packet::event("native-0", "view", page),
            })
            .unwrap();

        assert!(fixture.app.request_edit.is_none() && fixture.app.dialog.is_none());
        assert!(fixture.app.form_save_at.is_none());
        assert_eq!(
            form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
            freshest,
            "packet invalidation must flush the freshest editor text, cancelled={cancelled}"
        );
        let saved = fixture.app.state.snapshot();
        let drafts = saved["attentionDrafts"].as_object().unwrap();
        assert_eq!(drafts.len(), 1);
        assert_eq!(
            drafts[&form.source.key().unwrap()]["source"],
            json!({
                "environmentId":"native-0", "threadId":"thread-question", "requestId":original["id"], "revision":original["revision"]
            })
        );
        assert!(
            crate::attention::Form::from_request("native-0", "thread-question", &replacement)
                .unwrap()
                .load_draft(&fixture.app.state)
                .unwrap()
                .is_none()
        );
        assert!(fixture.app.state.pending_records().is_empty());
        let native_effects = fs::read(&fixture.native_states[0])
            .ok()
            .map(|bytes| {
                serde_json::from_slice::<Value>(&bytes).unwrap()["effects"]
                    .as_u64()
                    .unwrap()
            })
            .unwrap_or(0);
        assert_eq!(
            native_effects, 0,
            "source invalidation must never submit an answer"
        );
        let native_calls = fs::read_to_string(&log).unwrap();
        assert!(
            native_calls.lines().all(|line| {
                let call: Value = serde_json::from_str(line).unwrap();
                call["native"] != "orchestration.dispatchCommand"
                    && call["native"] != "orchestration.launchThread"
            }),
            "no native mutation may be dispatched by source invalidation"
        );
    }
}

#[test]
fn lifecycle_refresh_is_source_specific_and_ignores_text_chunks_and_search_pages() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    let mut value = live_snapshot(2);
    value["items"][0]["text"] = json!("A streaming text update");
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", value.clone()),
        })
        .unwrap();
    assert!(fixture.app.hosts[0].queued.is_none());
    value["revision"] = json!(3);
    value["status"] = json!("completed");
    value["activeRunId"] = Value::Null;
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", value.clone()),
        })
        .unwrap();
    assert_eq!(
        fixture.app.hosts[0].queued,
        Some((false, false, String::new(), true))
    );
    assert!(fixture.app.hosts[1].queued.is_none() && !fixture.app.hosts[1].loading);
    fixture.app.hosts[0].queued = None;
    fixture.app.server_search = true;
    value["revision"] = json!(4);
    value["requests"] = json!([{"id": "new-request", "revision": "new-offer"}]);
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", value),
        })
        .unwrap();
    assert!(fixture.app.hosts[0].queued.is_none());
}

#[test]
fn obsolete_navigation_ack_cannot_unlock_the_current_live_transition() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    fixture.app.follow = true;
    let active = fixture.app.active.as_mut().unwrap();
    active.navigating = true;
    active.navigation = Some(Navigation {
        token: 4,
        historical: false,
        base: 1,
        acknowledged: false,
        saw_sync: false,
    });
    fixture
        .app
        .read_result(ReadKind::History { navigation: 3 }, json!({}))
        .unwrap();
    assert!(fixture.app.follow);
    assert!(
        !fixture
            .app
            .active
            .as_ref()
            .unwrap()
            .navigation
            .as_ref()
            .unwrap()
            .acknowledged
    );
    fixture
        .app
        .read_result(ReadKind::Watch { navigation: 4 }, json!({}))
        .unwrap();
    assert!(fixture.app.active.as_ref().unwrap().navigating);
    let mut sync = live_snapshot(2);
    sync["state"] = json!("synchronizing");
    sync["live"] = json!(false);
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", sync),
        })
        .unwrap();
    assert!(fixture.app.active.as_ref().unwrap().navigating);
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", live_snapshot(3)),
        })
        .unwrap();
    assert!(!fixture.app.active.as_ref().unwrap().navigating);
    assert!(fixture.app.active.as_ref().unwrap().navigation.is_none());
}

#[test]
fn context_capture_and_resume_keep_original_native_scope_without_dispatch() {
    use crate::context_store::{Query, Scope};
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    fixture.app.command("context-save").unwrap();
    let capture = fixture
        .app
        .dialog
        .as_ref()
        .unwrap()
        .choices
        .iter()
        .find(|choice| choice.label == "Evidence")
        .unwrap()
        .intent
        .clone();
    fixture.open_thread(1, "thread-review");
    fixture.drain_context_work();
    fixture.app.dispatch(capture).unwrap();
    let query = Query::new(Scope {
        environment_id: "native-0".into(),
        project_id: Some("project-a".into()),
        thread_id: Some("thread-review".into()),
    });
    let pack = fixture.context_pack(query.clone());
    assert!(pack.eligible >= 1);
    assert_eq!(pack.rows[0].source.environment_id, "native-0");
    assert_eq!(
        pack.rows[0].source.item_id.as_deref(),
        Some("thread-review-answer")
    );
    let other = fixture.context_pack(Query::new(Scope {
        environment_id: "native-1".into(),
        ..query.scope.clone()
    }));
    assert!(
        other
            .rows
            .iter()
            .all(|row| row.source.environment_id == "native-1")
    );
    fixture
        .app
        .context_action(context::Action::Source(pack.rows[0].clone()))
        .unwrap();
    fixture.drain_context_work();
    let detail = &fixture.app.dialog.as_ref().unwrap().detail;
    assert!(detail.contains("native-0") && detail.contains("project-a"));
    assert!(detail.contains("capturedItemOnly") && detail.contains("historical"));
    assert!(detail.contains("日本語 café 🙂"));
    fixture.open_thread(0, "thread-review");
    fixture.app.replace_text("Unsent exact draft 日本語".into());
    fixture.app.flush_draft().unwrap();
    let pending = json!({"id":"original-context-operation","environmentId":"native-0","threadId":"thread-review","op":"answer","requestId":"original-request","revision":"original-request-revision","answers":{"field":"value"}});
    fixture
        .app
        .state
        .save_pending("native-0", false, pending.clone())
        .unwrap();
    fixture.app.command("resume").unwrap();
    fixture.drain_context_work();
    let dialog = fixture.app.dialog.as_ref().unwrap();
    assert!(dialog.detail.contains("original-context-operation"));
    assert!(dialog.detail.contains("original-request-revision"));
    assert!(dialog.detail.contains("original-request"));
    assert!(dialog.detail.contains("Unsent exact draft 日本語"));
    assert_eq!(fixture.app.state.pending("native-0", false), Some(pending));
    assert_eq!(fixture.app.text(), "Unsent exact draft 日本語");
    for file in &fixture.native_states {
        assert!(
            !file.exists(),
            "context capture/recall/resume must not dispatch native effects"
        );
    }
}

#[test]
fn source_inventory_keeps_empty_projects_and_rejects_malformed_replacement_atomically() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.app.search_scopes();
    assert!(
        fixture
            .app
            .dialog
            .as_ref()
            .unwrap()
            .choices
            .iter()
            .any(|choice| choice.label == "Empty project")
    );
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    assert!(
        surface
            .projects
            .iter()
            .any(|source| source.environment == "native-0" && source.project_id == "project-empty")
    );
    let previous = fixture.app.hosts[0].projects.clone();
    let generation = fixture.app.hosts[0].generation;
    fixture.app.handle(Background::Page {profile:0,generation,append:false,result:Ok(json!({
        "projects":[{"id":"project-a","title":"First"},{"id":"project-a","title":"Duplicate"}],"projectsMore":false,"threads":[],"more":false
    }))}).unwrap();
    assert_eq!(fixture.app.hosts[0].projects, previous);
    assert!(
        fixture.app.hosts[0]
            .error
            .contains("duplicate native project identity")
    );
}

#[test]
fn active_question_keeps_snapshot_project_after_search_removes_its_row() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-question");
    for host in &mut fixture.app.hosts {
        host.page.threads.retain(|row| row.id == "thread-fork");
    }
    fixture.app.rebuild();
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture.app.populate_surface(&mut surface);
    let question = surface
        .questions
        .iter()
        .find(|q| q.source.thread == "thread-question")
        .unwrap();
    assert_eq!(question.source.project_id, "project-a");
    assert_eq!(question.source.project, "Fixture project");
    assert_eq!(question.source.environment, "native-0");
    assert_eq!(surface.source.as_ref().unwrap(), &question.source);
    surface.attention_mode = true;
    let mut backend = ::slt::TestBackend::new(110, 38);
    for _ in 0..2 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    backend.assert_contains("Fixture project");
    backend.assert_not_contains("needs complete native source identities");
}

#[test]
fn metadata_running_page_does_not_clear_observed_unopened_completion() {
    let mut fixture = Fixture::new(0);
    let first = "a".repeat(64);
    let next = "b".repeat(64);
    for (generation, status, expected_unread) in [
        (Some(first.as_str()), "completed", false),
        (Some(next.as_str()), "completed", true),
        (None, "running", true),
    ] {
        let host_generation = fixture.app.hosts[0].generation;
        fixture.app.handle(Background::Page {profile:0,generation:host_generation,append:false,result:Ok(json!({"threads":[{
            "id":"thread-review","title":"Observed source","projectId":"project-a","status":status,
            "inbox":{"activityGeneration":generation,"activityAt":"2026-11-04T12:00:00Z","working":status=="running"}
        }],"more":false}))}).unwrap();
        if generation == Some(first.as_str()) {
            fixture
                .app
                .state
                .mark_thread_read("native-0", "thread-review", generation)
                .unwrap();
        }
        assert_eq!(
            fixture.app.open_section(&fixture.app.threads[0]) == "Unread",
            expected_unread
        );
    }
    assert!(
        fixture.app.threads[0].running(),
        "read state must not hide native running membership"
    );
}

#[test]
fn returning_slt_focus_to_prompt_settles_displayed_source_without_editing() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    fixture.drain_pages();
    let mut backend = ::slt::TestBackend::new(110, 38)
        .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
    let mut surface = crate::slt_ui::SurfaceState::new();
    for _ in 0..3 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    backend.run_with_events(
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Esc)
            .build(),
        |ctx| fixture.draw_slt(ctx, &mut surface),
    );
    backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    fixture.app.selected = fixture
        .app
        .visible
        .iter()
        .position(|(i, _)| {
            fixture.app.threads[*i].profile == 0 && fixture.app.threads[*i].id == "thread-fork"
        })
        .unwrap();
    fixture.app.focus = Focus::Navigator;
    backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    let (x, y) = backend.find_text("Prompt").unwrap();
    backend.run_with_events(
        ::slt::EventBuilder::new().click(x + 1, y + 2).build(),
        |ctx| fixture.draw_slt(ctx, &mut surface),
    );
    while backend.has_pending_input() {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    backend.run_with_events(
        ::slt::EventBuilder::new()
            .key_with(::slt::KeyCode::Char('t'), ::slt::KeyModifiers::CONTROL)
            .build(),
        |ctx| fixture.draw_slt(ctx, &mut surface),
    );
    for _ in 0..4 {
        if !backend.has_pending_input() {
            break;
        }
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    let pending = fixture.app.state.pending("native-0", false).unwrap();
    assert_eq!(pending["threadId"], "thread-review");
    assert_eq!(fixture.app.active.as_ref().unwrap().thread, "thread-review");
    assert_eq!(fixture.app.text(), "");
    let event = fixture.next_mutation();
    fixture.app.handle(event).unwrap();
    let native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
    assert_eq!(
        native["projections"]["thread-review"]["thread"]["settledOverride"],
        "settled"
    );
    assert!(native["projections"]["thread-fork"]["thread"]["settledOverride"].is_null());
}

#[test]
fn retained_slt_resize_preserves_tail_follow_and_manual_reading_position() {
    fn restart(mut fixture: Fixture) -> Fixture {
        let environments = fixture.app.environments.clone();
        let native_states = fixture.native_states.clone();
        let directory =
            std::mem::replace(&mut fixture.directory, tempfile::TempDir::new().unwrap());
        drop(fixture);
        let state = Arc::new(State::open(&directory.path().join("rust-state.json")).unwrap());
        Fixture {
            app: App::new(Args::default(), environments, state).unwrap(),
            directory,
            native_states,
            slt_closes: 0,
        }
    }

    let mut fixture = Fixture::new(0);
    let config_path = &fixture.app.environments[0].host_config;
    let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
    config["fixture"]["persistInitialState"] = json!(true);
    fs::write(config_path, config.to_string()).unwrap();
    fixture.open(0);
    let draft = "Tail 日本語 café e\u{301} 👩\u{200d}💻  \nold tail  \n";
    fixture.app.composer = ui::editor(draft, &fixture.app.theme, "");
    fixture.app.flush_draft().unwrap();
    let other = "Other source 日本語  \n";
    fixture
        .app
        .state
        .set_draft("native-0", "thread-review", other)
        .unwrap();
    let pending = json!({"id":"resize-preserved-operation","environmentId":"native-0",
        "threadId":"thread-review","op":"send","message":"Existing uncertain intent"});
    fixture
        .app
        .state
        .save_pending("native-0", false, pending.clone())
        .unwrap();
    let native_before = fs::read(&fixture.native_states[0]).unwrap();
    let native_mode = fs::metadata(&fixture.native_states[0])
        .unwrap()
        .permissions()
        .mode();
    let journal = fixture.directory.path().join("host-0/operations-v1");
    assert!(
        !journal.exists(),
        "read-only fixture starts without a host journal"
    );
    let mut surface = crate::slt_ui::SurfaceState::new();
    let mut backend =
        crate::slt_ui::tests::ResizableBackend::new(70, 22).with_activation_boundaries();
    for _ in 0..5 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    backend.run_with_events(
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Up)
            .key_code(::slt::KeyCode::End)
            .key_code(::slt::KeyCode::Left)
            .key_code(::slt::KeyCode::Left)
            .build(),
        |ctx| fixture.draw_slt(ctx, &mut surface),
    );
    for _ in 0..4 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    backend.assert_contains("old tail▎");
    backend.assert_contains("cargo test");
    assert!(fixture.app.follow);
    assert!(
        fixture.app.transcript_scroll > 0,
        "compact transcript overflows"
    );
    fixture.app.save_ui().unwrap();
    let identity = json!(["profile-0", "native-0", "thread-a"]).to_string();
    let saved = fixture.app.state.snapshot()["rustReadingPositions"][&identity].clone();
    assert_eq!(saved["follow"], true);
    assert_eq!(saved["rowKey"], "thread-a/thread-a-answer");
    let durable_before = fixture.app.state.snapshot();
    let caret = fixture.app.composer.cursor();
    let painted_caret = backend.find_text("▎").unwrap();
    for (width, height) in [(140, 38), (70, 22)] {
        backend.run_with_events(
            ::slt::EventBuilder::new().resize(width, height).build(),
            |ctx| fixture.draw_slt(ctx, &mut surface),
        );
        assert!(
            fixture.app.follow,
            "resize {width}x{height} must retain follow intent"
        );
        for frame in 0..5 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
            assert!(
                fixture.app.follow,
                "layout feedback lost tail follow at {width}x{height}, frame {frame}:\n{}",
                backend.text()
            );
        }
        if width == 140 {
            assert_eq!(
                surface.transcript_layout.as_ref().unwrap().max,
                0,
                "wide viewport must fit the complete transcript"
            );
            backend.assert_contains("Inspect Active implementation.");
        }
        backend.assert_contains("cargo test");
        assert_eq!(fixture.app.composer.cursor(), caret);
        assert_eq!(fixture.app.text(), draft);
    }
    assert_eq!(backend.find_text("▎"), Some(painted_caret));
    fixture.app.save_ui().unwrap();
    assert_eq!(
        fixture.app.state.snapshot()["rustReadingPositions"][&identity],
        saved,
        "normal close checkpoint must preserve the exact source-qualified tail anchor"
    );
    assert_eq!(
        fixture.app.state.snapshot(),
        durable_before,
        "resize and normal close checkpoint preserve the complete durable state"
    );
    // A real wheel gesture must still leave follow mode, including across a
    // fit/overflow transition and a source return through the production App.
    let (x, y) = backend.find_text("cargo test").unwrap();
    let mut wheel = ::slt::EventBuilder::new();
    for _ in 0..20 {
        wheel = wheel.scroll_up(x, y);
    }
    backend.run_with_events(wheel.build(), |ctx| fixture.draw_slt(ctx, &mut surface));
    for _ in 0..4 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    assert!(
        !fixture.app.follow,
        "manual wheel scrolling disengages follow"
    );
    assert_eq!(fixture.app.transcript_scroll, 0);
    fixture.app.save_ui().unwrap();
    let manual = fixture.app.state.snapshot()["rustReadingPositions"][&identity].clone();
    assert_eq!(manual["follow"], false);
    assert_eq!(manual["rowKey"], "thread-a/thread-a-user");
    for (width, height) in [(140, 38), (70, 22)] {
        backend.run_with_events(
            ::slt::EventBuilder::new().resize(width, height).build(),
            |ctx| fixture.draw_slt(ctx, &mut surface),
        );
        for _ in 0..5 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
            assert!(
                !fixture.app.follow,
                "resize must retain manual reading intent"
            );
        }
    }
    fixture.app.save_ui().unwrap();
    assert_eq!(
        fixture.app.state.snapshot()["rustReadingPositions"][&identity],
        manual
    );
    fixture.open_thread(0, "thread-review");
    for _ in 0..4 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    fixture.open(0);
    for _ in 0..5 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    assert!(!fixture.app.follow);
    backend.assert_contains("Inspect Active implementation.");
    fixture.app.save_ui().unwrap();
    assert_eq!(
        fixture.app.state.snapshot()["rustReadingPositions"][&identity],
        manual
    );
    assert_eq!(fixture.app.composer.cursor(), caret);
    fixture = restart(fixture);
    fixture.open(0);
    surface = crate::slt_ui::SurfaceState::new();
    backend = crate::slt_ui::tests::ResizableBackend::new(70, 22).with_activation_boundaries();
    for _ in 0..5 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    assert!(
        !fixture.app.follow,
        "restart restores the manual source-qualified anchor"
    );
    backend.assert_contains("Inspect Active implementation.");
    fixture.app.save_ui().unwrap();
    assert_eq!(
        fixture.app.state.snapshot()["rustReadingPositions"][&identity],
        manual
    );
    fixture.app.command("latest").unwrap();
    for _ in 0..5 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    assert!(fixture.app.follow, "explicit Latest resumes tail following");
    backend.assert_contains("cargo test");
    fixture.app.save_ui().unwrap();
    assert_eq!(
        fixture.app.state.snapshot()["rustReadingPositions"][&identity],
        saved
    );
    fixture = restart(fixture);
    fixture.open(0);
    surface = crate::slt_ui::SurfaceState::new();
    backend = crate::slt_ui::tests::ResizableBackend::new(70, 22).with_activation_boundaries();
    for _ in 0..5 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    assert!(fixture.app.follow, "restart retains tail-follow intent");
    backend.assert_contains("cargo test");
    fixture.app.save_ui().unwrap();
    assert_eq!(
        fixture.app.state.snapshot()["rustReadingPositions"][&identity],
        saved
    );
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.state.draft("native-0", "thread-review"), other);
    assert_eq!(fixture.app.state.pending("native-0", false), Some(pending));
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
    assert_eq!(
        fs::metadata(&fixture.native_states[0])
            .unwrap()
            .permissions()
            .mode(),
        native_mode
    );
    assert!(
        !journal.exists(),
        "resize/reading must not create operation records"
    );
}

#[test]
fn loaded_navigation_and_restore_read_active_slt_layout() {
    for find in [false, true] {
        let mut fixture = Fixture::new(0);
        fixture.load_pages();
        fixture.open_thread(0, "thread-review");
        let mut snapshot = fixture
            .app
            .active
            .as_ref()
            .unwrap()
            .projection
            .snapshot()
            .unwrap()
            .as_wire_value();
        snapshot["revision"] = json!(snapshot["revision"].as_u64().unwrap() + 100);
        snapshot["items"][1]["text"] = json!(
            (0..60)
                .map(|index| format!("Long source paragraph {index}.\n\n"))
                .collect::<String>()
        );
        fixture
            .app
            .active
            .as_mut()
            .unwrap()
            .projection
            .apply_snapshot(snapshot.clone(), "thread-review")
            .unwrap();
        let mut backend = ::slt::TestBackend::new(110, 38).with_run_config(
            ::slt::RunConfig::default()
                .activation_boundaries(true)
                .idle_redraw(false),
        );
        let mut surface = crate::slt_ui::SurfaceState::new();
        for _ in 0..4 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
        }
        assert!(fixture.app.follow);
        assert!(fixture.app.transcript_scroll > 0);
        if find {
            fixture
                .app
                .command("find Inspect Finished refactor")
                .unwrap();
            fixture.app.command("find-next").unwrap();
        } else {
            fixture.app.command("loaded-jump 1").unwrap();
        }
        for _ in 0..4 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
            if fixture.app.workspace.pending_jump.is_none() {
                break;
            }
        }
        assert!(
            fixture.app.workspace.pending_jump.is_none(),
            "valid layout accepted the jump"
        );
        assert!(
            !fixture.app.follow,
            "accepted jump preserves navigation intent"
        );
        backend.assert_contains("Inspect Finished refactor.");
        let identity = json!(["profile-0", "native-0", "thread-review"]).to_string();
        let saved = fixture.app.state.snapshot()["rustReadingPositions"][&identity].clone();
        assert_eq!(
            saved["follow"], false,
            "persist accepted position before another action"
        );
        assert_eq!(saved["offset"], 0);
        assert!(
            saved["digest"]
                .as_str()
                .is_some_and(|digest| digest.len() == 64)
        );
        fixture.open_thread(0, "thread-fork");
        let returned = &fixture.app.return_positions["0\0thread-review"];
        assert_eq!((returned.0, returned.1), (0, false));
        for _ in 0..3 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
        }
        fixture.open_thread(0, "thread-review");
        fixture
            .app
            .active
            .as_mut()
            .unwrap()
            .projection
            .apply_snapshot(snapshot, "thread-review")
            .unwrap();
        for _ in 0..4 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
        }
        backend.assert_contains("Inspect Finished refactor.");
        assert_eq!(fixture.app.transcript_scroll, 0);
        assert!(!fixture.app.follow);
        assert_eq!(
            fixture.app.state.snapshot()["rustReadingPositions"][&identity],
            saved
        );
        fixture.app.command("loaded-jump 1").unwrap();
        backend.run_with_events(::slt::EventBuilder::new().resize(86, 31).build(), |ctx| {
            fixture.draw_slt(ctx, &mut surface);
        });
        assert!(
            fixture.app.workspace.pending_jump.is_some(),
            "resize waits for valid geometry"
        );
        for _ in 0..3 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
        }
        assert!(fixture.app.workspace.pending_jump.is_none());
        assert!(!fixture.app.follow);
        backend.assert_contains("Inspect Finished refactor.");
        let position = fixture.app.state.snapshot()["rustReadingPositions"][&identity].clone();
        for _ in 0..3 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
        }
        assert_eq!(
            fixture.app.state.snapshot()["rustReadingPositions"][&identity],
            position,
            "idle frames do not change the anchor"
        );
        fixture.app.follow = true;
        for _ in 0..3 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
        }
        let old_bottom = fixture.app.transcript_scroll;
        let mut growth = fixture
            .app
            .active
            .as_ref()
            .unwrap()
            .projection
            .snapshot()
            .unwrap()
            .as_wire_value();
        growth["revision"] = json!(growth["revision"].as_u64().unwrap() + 1);
        let text = format!(
            "{}{}",
            growth["items"][1]["text"].as_str().unwrap(),
            "New streamed paragraph.\n\n".repeat(10)
        );
        growth["items"][1]["text"] = json!(text);
        fixture
            .app
            .active
            .as_mut()
            .unwrap()
            .projection
            .apply_snapshot(growth, "thread-review")
            .unwrap();
        for _ in 0..4 {
            backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
        }
        assert!(fixture.app.follow);
        assert!(
            fixture.app.transcript_scroll > old_bottom,
            "following tail tracks new content"
        );
        let (x, y) = backend.find_text("New streamed paragraph.").unwrap();
        backend.run_with_events(::slt::EventBuilder::new().scroll_up(x, y).build(), |ctx| {
            fixture.draw_slt(ctx, &mut surface);
        });
        assert!(!fixture.app.follow, "manual scrolling leaves the tail");
    }
}

#[test]
fn navigation_pin_replaces_older_paged_source_without_resetting_search_scope_or_draft() {
    let mut fixture = Fixture::new(0);
    let config_path = &fixture.app.environments[0].host_config;
    let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
    config["fixture"]["persistInitialState"] = json!(true);
    fs::write(config_path, config.to_string()).unwrap();
    fixture.load_pages();
    let mut native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
    let original = native["projections"]["thread-review"].clone();
    native["projections"]["thread-review"]["thread"]["pinnedAt"] = json!("native-shelf-pin");
    native["projections"]["thread-review"]["thread"]["pinOrderKey"] = json!("native-shelf-order");
    for index in 0..240 {
        let id = format!("recent-{index:03}");
        let mut recent = original.clone();
        recent["thread"]["id"] = json!(id);
        recent["thread"]["createdAt"] = json!("2026-11-04T12:00:00Z");
        native["projections"][&id] = recent;
    }
    fs::write(&fixture.native_states[0], native.to_string()).unwrap();
    fixture.load_pages();
    assert!(
        !fixture.app.hosts[0]
            .page
            .threads
            .iter()
            .any(|row| row.id == "thread-review")
    );
    fixture.open_thread(0, "thread-review");
    fixture.app.refresh(true, false).unwrap();
    fixture.drain_pages();
    assert!(
        fixture.app.hosts[0]
            .page
            .threads
            .iter()
            .any(|row| row.id == "thread-review")
    );
    fixture
        .app
        .replace_text("Preserve the exact unsent draft\n🦀".into());
    fixture.app.flush_draft().unwrap();
    fixture.app.transcript_scroll = 3;
    fixture.app.follow = false;
    fixture.app.focus = Focus::Composer;
    fixture
        .app
        .workspace_dispatch(workspace::WorkspaceAction::Scope(Some(json!({
            "profileId":"profile-0","environmentId":"native-0","projectId":"project-a"
        }))))
        .unwrap();
    let project_scope = fixture.app.workspace_scope_label();
    let other = fixture.app.hosts[1]
        .page
        .threads
        .iter()
        .find(|row| row.id == "thread-review")
        .unwrap()
        .raw
        .clone();
    for (index, command) in ["pin", "settle", "unsettle", "pin"].into_iter().enumerate() {
        fixture.app.command(command).unwrap();
        if command == "pin" {
            let intent = fixture.app.dialog.as_ref().unwrap().choices[0]
                .intent
                .clone();
            fixture.app.dispatch(intent).unwrap();
        }
        let event = fixture.next_mutation();
        assert!(
            matches!(&event, Background::Mutation {context, result:Ok(value), ..}
            if context.profile == 0 && value["metadata"]["status"] == "fresh")
        );
        // Acknowledgement arrives while the human is browsing another host's
        // server search. The active source, scope and independent draft stay put.
        fixture.app.scope = Some(1);
        fixture.app.filter = "server-only-marker".into();
        fixture.app.server_search = true;
        fixture.app.hosts[1].page.search = true;
        fixture.app.hosts[1].page.query = "server-only-marker".into();
        fixture.app.handle(event).unwrap();
        let target = fixture.app.active_summary().unwrap();
        assert_eq!(target.profile, 0);
        assert_eq!(target.id, "thread-review");
        assert_eq!(target.pinned(), index != 3);
        assert_eq!(target.reviewed(), index == 1);
        if index > 0 {
            assert!(string(target.inbox(), "pinnedAt").is_empty());
        } else {
            assert_eq!(string(target.inbox(), "pinnedAt"), "native-shelf-pin");
        }
        assert_eq!(fixture.app.scope, Some(1));
        assert!(fixture.app.server_search);
        assert_eq!(fixture.app.filter, "server-only-marker");
        assert_eq!(fixture.app.hosts[1].page.query, "server-only-marker");
        assert_eq!(fixture.app.workspace_scope_label(), project_scope);
        assert_eq!(fixture.app.text(), "Preserve the exact unsent draft\n🦀");
        assert_eq!(
            fixture.app.state.draft("native-0", "thread-review"),
            fixture.app.text()
        );
        assert_eq!(
            (fixture.app.transcript_scroll, fixture.app.follow),
            (3, false)
        );
        assert_eq!(
            fixture.app.hosts[1]
                .page
                .threads
                .iter()
                .find(|row| row.id == "thread-review")
                .unwrap()
                .raw,
            other
        );
        assert!(fixture.app.state.pending_records().is_empty());
        if index == 0 {
            let retained = fixture.app.hosts[0]
                .page
                .threads
                .iter()
                .find(|row| row.id == "thread-review")
                .unwrap();
            assert_eq!(retained.raw, target.raw);
            // A search can omit the attached old source; later actions still
            // use its complete summary without inserting it into search results.
            fixture.app.hosts[0]
                .page
                .threads
                .retain(|row| row.id != "thread-review");
            fixture.app.rebuild();
        }
    }
    let native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
    assert_eq!(native["effects"], 4);
    assert_eq!(
        native["projections"]["thread-review"]["thread"]["tags"],
        json!([])
    );
    assert!(native["projections"]["thread-review"]["thread"]["pinnedAt"].is_null());
    let mut external = native;
    let thread = &mut external["projections"]["thread-review"]["thread"];
    thread["tags"] = json!(["t3oxide:navigation-pin"]);
    thread["settledOverride"] = json!("settled");
    thread["lineage"] = json!({"parentThreadId":"recent-000","rootThreadId":"recent-000","relationshipToParent":"subagent"});
    fs::write(&fixture.native_states[0], external.to_string()).unwrap();
    fixture.app.scope = Some(0);
    fixture.app.server_search = false;
    fixture.app.filter.clear();
    fixture.app.set_lane("Pins".into()).unwrap();
    fixture.drain_pages();
    assert_eq!(fixture.app.hosts[0].page.threads.len(), 1);
    let row = &fixture.app.hosts[0].page.threads[0];
    assert_eq!(row.id, "thread-review");
    assert!(row.pinned() && row.reviewed());
    assert_eq!(row.kind(), "subagent");
    let stamp = external["projections"]["thread-review"]["thread"]["updatedAt"].clone();
    external["projections"]["thread-review"]["thread"]["tags"] = json!([]);
    fs::write(&fixture.native_states[0], external.to_string()).unwrap();
    fixture.app.set_lane("Pins".into()).unwrap();
    fixture.drain_pages();
    assert!(fixture.app.hosts[0].page.threads.is_empty());
    assert!(!fixture.app.active_summary().unwrap().pinned());
    assert_eq!(
        fixture.app.active_summary().unwrap().raw["updatedAt"],
        stamp
    );
}

#[test]
fn stale_action_metadata_preserves_ack_and_disables_only_its_exact_source() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    let before = fixture.app.active_summary().unwrap().raw.clone();
    let other = fixture.app.hosts[1]
        .page
        .threads
        .iter()
        .find(|row| row.id == "thread-review")
        .unwrap()
        .raw
        .clone();
    for metadata in [
        json!({"status":"stale","environmentId":"native-0","threadId":"thread-review"}),
        json!({"status":"fresh","environmentId":"native-1","threadId":"thread-review","row":before}),
    ] {
        fixture.app.mutation_done(MutationContext{profile:0,generation:fixture.app.generation,draft_version:None},
            "authentic-ack","thread-action",&json!({"threadId":"thread-review","action":"navigation-pin"}),
            json!({"sequence":42,"threadId":"thread-review","action":"navigation-pin","metadata":metadata})).unwrap();
        assert!(fixture.app.notice.contains("accepted by T3"));
        assert!(fixture.app.notice.contains("stale"));
        assert!(flag(
            &fixture.app.active_summary().unwrap().raw,
            "_metadataStale"
        ));
        assert_eq!(
            fixture.app.active_summary().unwrap().inbox(),
            &before["inbox"]
        );
        assert_eq!(
            fixture.app.hosts[1]
                .page
                .threads
                .iter()
                .find(|row| row.id == "thread-review")
                .unwrap()
                .raw,
            other
        );
        fixture.app.focus = Focus::Composer;
        assert!(fixture.app.command("pin").is_err());
        assert!(fixture.app.state.pending_records().is_empty());
    }
    fixture.load_pages();
    assert!(!flag(
        &fixture.app.active_summary().unwrap().raw,
        "_metadataStale"
    ));
}

#[test]
fn duplicate_paged_summary_replaces_complete_marker_metadata_without_updated_at_change() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    let mut fresh = fixture.app.active_summary().unwrap().raw.clone();
    fresh["inbox"]["navigationPinned"] = json!(true);
    fresh["inbox"]["nativeRevision"] = json!("marker-only-revision");
    fresh["inbox"].as_object_mut().unwrap().remove("pinnedAt");
    let stamp = fresh["updatedAt"].clone();
    let generation = fixture.app.hosts[0].generation;
    fixture
        .app
        .handle(Background::Page {
            profile: 0,
            generation,
            append: true,
            result: Ok(json!({"threads":[fresh],"more":false})),
        })
        .unwrap();
    let active = fixture.app.active_summary().unwrap();
    assert!(active.pinned());
    assert_eq!(active.raw["updatedAt"], stamp);
    assert!(active.inbox().get("pinnedAt").is_none());
    let rows: Vec<_> = fixture.app.hosts[0]
        .page
        .threads
        .iter()
        .filter(|row| row.id == "thread-review")
        .collect();
    assert_eq!(rows.len(), 1);
    assert_eq!(rows[0].raw, active.raw);
}

#[test]
fn external_marker_stream_change_refreshes_original_search_without_changing_scope() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    fixture.app.server_search = true;
    fixture.app.scope = Some(1);
    fixture.app.hosts[0].page.search = true;
    fixture.app.hosts[0].page.query = "original native search".into();
    let mut snapshot = live_snapshot(2);
    snapshot["navigationPinned"] = json!(true);
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", snapshot),
        })
        .unwrap();
    assert_eq!(
        fixture.app.hosts[0].queued,
        Some((false, true, "original native search".into(), true))
    );
    assert!(fixture.app.hosts[1].queued.is_none());
    assert_eq!(fixture.app.scope, Some(1));
    assert!(fixture.app.server_search);
}

#[test]
fn leaving_pins_during_loading_queues_the_ordinary_page() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.app.scope = Some(0);
    fixture.app.set_lane("Pins".into()).unwrap();
    assert!(fixture.app.hosts[0].loading);
    fixture.app.set_lane("Open".into()).unwrap();
    assert!(fixture.app.hosts[0].queued.is_some());
    fixture.drain_pages();
    assert_eq!(fixture.app.lane, "Open");
    assert!(!fixture.app.hosts[0].page.navigation_pins_only);
    assert!(
        fixture.app.hosts[0]
            .page
            .threads
            .iter()
            .any(|row| !row.pinned())
    );
}

#[test]
fn marker_invalidation_uses_the_latest_search_request_and_never_merges_other_queries() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    fixture.app.server_search = true;
    fixture.app.scope = Some(0);
    fixture.app.hosts[0].page.search = true;
    fixture.app.hosts[0].page.query = "old query".into();
    fixture.app.hosts[0].request = Some((false, true, "in-flight query".into(), false));
    let mut snapshot = live_snapshot(2);
    snapshot["navigationPinned"] = json!(true);
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", snapshot.clone()),
        })
        .unwrap();
    assert_eq!(
        fixture.app.hosts[0].queued,
        Some((false, true, "in-flight query".into(), true))
    );
    fixture
        .app
        .refresh_profile(0, false, true, "latest query".into(), false)
        .unwrap();
    snapshot["revision"] = json!(3);
    snapshot["navigationPinned"] = json!(false);
    fixture
        .app
        .handle(Background::Packet {
            generation: 7,
            packet: Packet::event("native-0", "view", snapshot),
        })
        .unwrap();
    assert_eq!(
        fixture.app.hosts[0].queued,
        Some((false, true, "latest query".into(), false))
    );
    // Exercise the response merge independently of input timing. Query/search/
    // Pins identity must all match before retaining rows from another page.
    for mismatch in ["query", "search", "pins"] {
        let host = &mut fixture.app.hosts[0];
        host.queued = None;
        host.refresh_loaded = true;
        host.page.search = true;
        host.page.query = "old query".into();
        host.page.navigation_pins_only = false;
        host.page.threads = vec![Thread::parse(0, json!({"id":"old-result"})).unwrap()];
        let generation = host.generation;
        let response = json!({"threads":[{"id":"new-result"}],"more":false,
            "serverSearch":mismatch != "search", "_clientQuery":if mismatch == "query" {"new query"} else {"old query"},
            "navigationPinsOnly":mismatch == "pins"});
        fixture
            .app
            .handle(Background::Page {
                profile: 0,
                generation,
                append: false,
                result: Ok(response),
            })
            .unwrap();
        assert_eq!(
            fixture.app.hosts[0].page.threads.len(),
            1,
            "scope mismatch {mismatch}"
        );
        assert_eq!(
            fixture.app.hosts[0].page.threads[0].id, "new-result",
            "scope mismatch {mismatch}"
        );
    }
}

#[test]
fn restore_shortcut_uses_retained_settled_summary_after_unpin_removes_the_row() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    let active = fixture.app.active.as_mut().unwrap();
    let row = active.summary.as_mut().unwrap();
    row.raw["inbox"]["settled"] = json!(true);
    row.raw["inbox"]["navigationPinned"] = json!(false);
    fixture.app.hosts[0]
        .page
        .threads
        .retain(|row| row.id != "thread-review");
    fixture.app.rebuild();
    assert!(
        fixture
            .app
            .threads
            .iter()
            .all(|row| row.id != "thread-review" || row.profile != 0)
    );
    let mut surface = crate::slt_ui::SurfaceState::default();
    surface.focus = Some(Focus::Composer);
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::Command("reviewed".into()),
            &mut surface,
        )
        .unwrap();
    let event = fixture.next_mutation();
    assert!(
        matches!(&event,Background::Mutation{context,fields,..} if context.profile==0 && fields["threadId"]=="thread-review" && fields["action"]=="unsettle")
    );
}

#[test]
fn context_worker_io_barrier_leaves_controller_and_draft_saver_responsive() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    let mut snapshot = live_snapshot(2);
    snapshot["projectId"] = json!("project-a");
    fixture
        .app
        .active
        .as_mut()
        .unwrap()
        .projection
        .apply_snapshot(snapshot, "thread-a")
        .unwrap();
    let (entered, waiting) = bounded(1);
    let (release, barrier) = bounded(1);
    fixture.app.context_test_barrier = Some(context::IoBarrier {
        entered,
        release: barrier,
    });
    let (ready, progress) = bounded(1);
    let controller = thread::spawn(move || {
        fixture.app.command("context").unwrap();
        fixture
            .app
            .replace_text("Independent unsent draft 日本語\n  ".into());
        fixture.app.schedule_save().unwrap();
        while fixture.app.state.draft("native-0", "thread-a")
            != "Independent unsent draft 日本語\n  "
        {
            let event = fixture.app.rx.recv_timeout(Duration::from_secs(5)).unwrap();
            fixture.app.handle(event).unwrap();
        }
        ready.send(()).unwrap();
        fixture
    });
    waiting
        .recv_timeout(Duration::from_secs(5))
        .expect("store operation reached held I/O barrier");
    let responsive = progress.recv_timeout(Duration::from_secs(1)).is_ok();
    release.send(()).unwrap();
    let _fixture = controller.join().unwrap();
    assert!(
        responsive,
        "context I/O blocked controller input and its independent draft save behind the held store barrier"
    );
}

#[test]
fn context_worker_quit_drains_an_accepted_write_with_a_full_completion_queue() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    let (entered, waiting) = bounded(1);
    let (release, barrier) = bounded(1);
    fixture.app.context_test_barrier = Some(context::IoBarrier {
        entered,
        release: barrier,
    });
    let input = crate::context_store::RecordInput {
        source: crate::context_store::Source {
            environment_id: "native-0".into(),
            project_id: "project-a".into(),
            thread_id: "thread-a".into(),
            item_id: Some("accepted-item".into()),
            request_id: None,
            revision: "original-source-revision".into(),
            observed: json!({"streaming":false}),
        },
        kind: crate::context_store::Kind::Evidence,
        text: "Accepted capture".into(),
        full_source: b"Original accepted source bytes".to_vec(),
        supersedes: None,
        revisit: None,
    };
    fixture
        .app
        .context_capture_pending
        .insert("accepted-capture".into(), input);
    fixture.app.schedule_context_capture().unwrap();
    waiting.recv_timeout(Duration::from_secs(5)).unwrap();
    assert!(fixture.app.context_capture_inflight);
    while fixture
        .app
        .tx
        .try_send(Background::Saved {
            key: "unrelated".into(),
            version: 0,
            result: Ok(()),
        })
        .is_ok()
    {}
    assert_eq!(fixture.app.rx.len(), 128);
    fixture.app.command("quit").unwrap();
    let premature_quit = fixture.app.quit;
    release.send(()).unwrap();
    fixture.app.drain_context().unwrap();
    assert!(!fixture.app.context_capture_inflight);
    assert!(fixture.app.quit);
    assert!(
        !premature_quit,
        "Quit returned before the accepted context write's completion was drained"
    );
}

fn context_input(item: &str) -> crate::context_store::RecordInput {
    crate::context_store::RecordInput {
        source: crate::context_store::Source {
            environment_id: "native-0".into(),
            project_id: "project-a".into(),
            thread_id: "thread-a".into(),
            item_id: Some(item.into()),
            request_id: None,
            revision: format!("revision-{item}"),
            observed: json!({"streaming":false}),
        },
        kind: crate::context_store::Kind::Evidence,
        text: format!("Evidence {item}"),
        full_source: format!("Exact original {item} 日本語\n  ").into_bytes(),
        supersedes: None,
        revisit: None,
    }
}

fn context_scope() -> crate::context_store::Scope {
    crate::context_store::Scope {
        environment_id: "native-0".into(),
        project_id: Some("project-a".into()),
        thread_id: Some("thread-a".into()),
    }
}

fn context_path(fixture: &Fixture) -> PathBuf {
    let mut path = fixture.app.state.path().as_os_str().to_os_string();
    path.push(".context");
    path.into()
}

fn hold_context(fixture: &mut Fixture) -> (Receiver<()>, Sender<()>) {
    let (entered, waiting) = bounded(1);
    let (release, barrier) = bounded(1);
    fixture.app.context_test_barrier = Some(context::IoBarrier {
        entered,
        release: barrier,
    });
    (waiting, release)
}

fn next_context(fixture: &mut Fixture) -> Background {
    loop {
        let event = fixture.app.rx.recv_timeout(Duration::from_secs(5)).unwrap();
        if matches!(event, Background::Context(_)) {
            return event;
        }
        fixture.app.handle(event).unwrap();
    }
}

#[test]
fn context_worker_coalesces_reads_to_original_inflight_and_latest_desired() {
    let mut fixture = Fixture::new(0);
    let (waiting, release) = hold_context(&mut fixture);
    fixture
        .app
        .context_recall(context_scope(), "first", false, 0)
        .unwrap();
    waiting.recv_timeout(Duration::from_secs(5)).unwrap();
    for offset in 1..50 {
        fixture
            .app
            .context_recall(context_scope(), &format!("query-{offset}"), true, offset)
            .unwrap();
    }
    release.send(()).unwrap();
    let first = next_context(&mut fixture);
    assert!(
        matches!(&first, Background::Context(context_worker::Event::Finished(done))
        if matches!(&done.result, Ok(context_worker::Output::Pack(pack)) if pack.query == "first"))
    );
    fixture.app.handle(first).unwrap();
    assert_eq!(
        fixture.app.dialog.as_ref().unwrap().title,
        "Context",
        "superseded read replaced the pending view"
    );
    let latest = next_context(&mut fixture);
    assert!(
        matches!(&latest, Background::Context(context_worker::Event::Finished(done))
        if matches!(&done.result, Ok(context_worker::Output::Pack(pack))
            if pack.query == "query-49" && pack.offset == 49 && pack.history && pack.scope == context_scope()))
    );
    fixture.app.handle(latest).unwrap();
    assert_eq!(
        fixture.app.dialog.as_ref().unwrap().title,
        "Context records"
    );
    assert!(!fixture.app.context_has_work());
    assert!(
        fixture.app.rx.is_empty(),
        "intermediate reads were submitted"
    );
}

#[test]
fn context_worker_late_reads_cannot_reopen_closed_or_replaced_or_switched_views() {
    for replacement in ["close", "palette", "detail", "tab"] {
        let mut fixture = Fixture::new(0);
        synthetic_attachment(&mut fixture.app);
        let (waiting, release) = hold_context(&mut fixture);
        fixture
            .app
            .context_recall(context_scope(), "old query", false, 0)
            .unwrap();
        waiting.recv_timeout(Duration::from_secs(5)).unwrap();
        let mut surface = crate::slt_ui::SurfaceState::default();
        match replacement {
            "close" => fixture
                .app
                .surface_action(crate::slt_ui::Action::CloseModal, &mut surface)
                .unwrap(),
            "palette" => fixture
                .app
                .surface_action(
                    crate::slt_ui::Action::Command("palette".into()),
                    &mut surface,
                )
                .unwrap(),
            "detail" => fixture.app.detail("Other view", "Retained current content"),
            "tab" => fixture.open(1),
            _ => unreachable!(),
        }
        let current = fixture
            .app
            .dialog
            .as_ref()
            .map(|dialog| dialog.title.clone());
        release.send(()).unwrap();
        let old = next_context(&mut fixture);
        fixture.app.handle(old).unwrap();
        assert_eq!(
            fixture
                .app
                .dialog
                .as_ref()
                .map(|dialog| dialog.title.clone()),
            current,
            "{replacement}"
        );
        fixture.drain_context_work();
        let pack = fixture.context_pack(crate::context_store::Query::new(context_scope()));
        assert_eq!(
            pack.scope,
            context_scope(),
            "new recall must use its original explicit scope"
        );
    }
}

#[test]
fn context_worker_busy_write_retains_original_until_explicit_retry() {
    let mut fixture = Fixture::new(0);
    let (waiting, release) = hold_context(&mut fixture);
    fixture
        .app
        .context_action(context::Action::Record(context_input("accepted")))
        .unwrap();
    waiting.recv_timeout(Duration::from_secs(5)).unwrap();
    assert_eq!(fixture.app.notice, "Saving context…");
    let error = fixture
        .app
        .context_action(context::Action::Record(context_input("retained")))
        .unwrap_err();
    assert!(error.to_string().contains("Context busy"));
    assert!(
        fixture
            .app
            .context_action(context::Action::Record(context_input("replacement")))
            .unwrap_err()
            .to_string()
            .contains("Original context write retained")
    );
    release.send(()).unwrap();
    fixture.drain_context_work();
    let before = fixture.context_pack(crate::context_store::Query::new(context_scope()));
    assert_eq!(before.eligible, 1);
    fixture.app.command("quit").unwrap();
    assert!(fixture.app.drain_context().is_err());
    assert!(!fixture.app.quit, "Quit discarded a queue-rejected write");
    fixture.app.command("quit").unwrap();
    assert!(
        !fixture.app.quit,
        "Repeated Quit discarded the retained write"
    );
    fixture.app.command("context-retry").unwrap();
    fixture.drain_context_work();
    assert!(
        !fixture.app.context_writes_blocked(),
        "Retry failed: {}",
        fixture.app.context_capture_status
    );
    let after = fixture.context_pack(crate::context_store::Query::new(context_scope()));
    assert_eq!(after.eligible, 2);
    assert!(
        after
            .rows
            .iter()
            .any(|row| row.source.item_id.as_deref() == Some("retained"))
    );
    assert!(
        !after
            .rows
            .iter()
            .any(|row| row.source.item_id.as_deref() == Some("replacement"))
    );
    assert!(
        fixture.app.command("context-retry").is_err(),
        "accepted write must not replay"
    );
}

#[test]
fn context_worker_prepared_failure_retries_original_event_after_source_switch() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    let original = context_input("prepared-original");
    let path = context_path(&fixture);
    let store = crate::context_store::Store::open(&path).unwrap();
    let mut request = context_worker::Request::Write(context_worker::WriteRequest::new(
        fixture.app.generation,
        context_worker::Write::Record(original.clone()),
    ));
    let error = {
        let fault = crate::context_store::tests::DirectorySyncFailure::arm(&path.join("log"));
        let error = context_worker::execute(&store, &mut request).unwrap_err();
        fault.assert_consumed();
        error
    };
    let before = store
        .recall(&crate::context_store::Query::new(context_scope()))
        .unwrap();
    let row = before
        .rows
        .iter()
        .find(|row| row.source == original.source)
        .unwrap()
        .clone();
    let event_path = path.join("log").join(format!("{}.jsonl", row.id));
    let event_bytes = fs::read(&event_path).unwrap();
    drop(store);
    // Feed the real failed worker execution into the normal retained-request path.
    fixture
        .app
        .handle(Background::Context(context_worker::Event::Finished(
            Box::new(context_worker::Finished {
                request,
                result: Err(format!("{error:#}")),
            }),
        )))
        .unwrap();
    fixture.open(1);
    let draft = "New source 日本語\n\tkeeps spaces  ";
    fixture.app.replace_text(draft.into());
    fixture.app.command("context-retry").unwrap();
    fixture.app.notice = "Current source notice".into();
    let event = next_context(&mut fixture);
    fixture.app.handle(event).unwrap();
    assert_eq!(fixture.app.notice, "Current source notice");
    fixture.drain_context_work();
    let after = fixture.context_pack(crate::context_store::Query::new(context_scope()));
    let recovered: Vec<_> = after
        .rows
        .iter()
        .filter(|row| row.source == original.source)
        .collect();
    assert_eq!(recovered.len(), 1);
    assert_eq!(recovered[0].id, row.id);
    assert_eq!(fs::read(event_path).unwrap(), event_bytes);
    assert_eq!(
        fs::read(path.join(&row.source_ref)).unwrap(),
        original.full_source
    );
    assert_eq!(fixture.app.composer.lines().join("\n"), draft);
    assert!(
        fixture.app.command("context-retry").is_err(),
        "successful event must not replay"
    );
}

#[test]
fn context_worker_original_source_write_ack_survives_switch_without_foreign_success() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    let (waiting, release) = hold_context(&mut fixture);
    fixture
        .app
        .context_action(context::Action::Record(context_input("original")))
        .unwrap();
    waiting.recv_timeout(Duration::from_secs(5)).unwrap();
    fixture.open(1);
    fixture.app.notice = "Current source notice".into();
    release.send(()).unwrap();
    let event = next_context(&mut fixture);
    let current_notice = fixture.app.notice.clone();
    fixture.app.handle(event).unwrap();
    assert_eq!(fixture.app.notice, current_notice);
    fixture.drain_context_work();
    let pack = fixture.context_pack(crate::context_store::Query::new(context_scope()));
    let row = pack
        .rows
        .iter()
        .find(|row| row.source.item_id.as_deref() == Some("original"))
        .unwrap()
        .clone();
    fixture
        .app
        .context_action(context::Action::Source(row))
        .unwrap();
    fixture.drain_context_work();
    assert!(
        fixture
            .app
            .dialog
            .as_ref()
            .unwrap()
            .detail
            .contains("Exact original original 日本語\n  ")
    );
}

#[test]
fn context_worker_open_error_preserves_exact_write_and_quit_reports_failure() {
    let mut fixture = Fixture::new(0);
    let path = context_path(&fixture);
    fs::write(&path, b"Not a context directory").unwrap();
    let original = context_input("failed-original");
    fixture
        .app
        .context_action(context::Action::Record(original.clone()))
        .unwrap();
    fixture.app.command("quit").unwrap();
    let error = fixture.app.drain_context().unwrap_err().to_string();
    assert!(error.contains("original context write retained"));
    assert!(!fixture.app.quit);
    assert!(fixture.app.notice.contains("context-retry"));
    fs::remove_file(path).unwrap();
    fixture.app.command("context-retry").unwrap();
    fixture.drain_context_work();
    let pack = fixture.context_pack(crate::context_store::Query::new(context_scope()));
    assert_eq!(pack.eligible, 1);
    assert_eq!(pack.rows[0].source, original.source);
    fixture
        .app
        .context_action(context::Action::Source(pack.rows[0].clone()))
        .unwrap();
    fixture.drain_context_work();
    assert!(
        fixture
            .app
            .dialog
            .as_ref()
            .unwrap()
            .detail
            .contains("Exact original failed-original 日本語")
    );
}

#[test]
fn context_worker_failure_before_quit_survives_repeated_quit_and_retry() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    let draft = "Unsent 日本語\n\twith trailing spaces  ";
    fixture.app.replace_text(draft.into());
    let path = context_path(&fixture);
    fs::write(&path, b"Not a context directory").unwrap();
    let original = context_input("failed-before-quit");
    fixture
        .app
        .context_action(context::Action::Record(original.clone()))
        .unwrap();
    fixture.drain_context_work();
    assert!(
        fixture
            .app
            .notice
            .contains("original context write retained")
    );

    for _ in 0..2 {
        fixture.app.command("quit").unwrap();
        let result = fixture.app.drain_context();
        assert!(!fixture.app.quit, "Quit discarded an already-failed write");
        assert!(result.unwrap_err().to_string().contains("context-retry"));
        assert!(!fixture.app.context_io.draining);
        assert!(fixture.app.notice.contains("context-retry"));
    }

    // A retry that fails again must retain the same original payload.
    fixture.app.command("context-retry").unwrap();
    fixture.drain_context_work();
    fixture.app.command("quit").unwrap();
    assert!(fixture.app.drain_context().is_err());
    assert!(!fixture.app.quit);

    fs::remove_file(path).unwrap();
    fixture.app.command("context-retry").unwrap();
    fixture.drain_context_work();
    let pack = fixture.context_pack(crate::context_store::Query::new(context_scope()));
    assert_eq!(pack.eligible, 1);
    assert_eq!(pack.rows[0].source, original.source);
    assert_eq!(pack.rows[0].text, original.text);
    fixture
        .app
        .context_action(context::Action::Source(pack.rows[0].clone()))
        .unwrap();
    fixture.drain_context_work();
    assert!(
        fixture
            .app
            .dialog
            .as_ref()
            .unwrap()
            .detail
            .contains(std::str::from_utf8(&original.full_source).unwrap())
    );
    assert_eq!(fixture.app.composer.lines().join("\n"), draft);
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
    assert!(fixture.app.command("context-retry").is_err());
    fixture.app.command("quit").unwrap();
    fixture.app.drain_context().unwrap();
    assert!(
        fixture.app.quit,
        "Successful explicit retry should allow Quit"
    );
}

#[test]
fn context_worker_source_read_is_async_and_exact_provenance_is_required() {
    let mut fixture = Fixture::new(0);
    let row = {
        let store = crate::context_store::Store::open(&context_path(&fixture)).unwrap();
        store.record(&context_input("source")).unwrap();
        store
            .recall(&crate::context_store::Query::new(context_scope()))
            .unwrap()
            .rows
            .remove(0)
    };
    let (waiting, release) = hold_context(&mut fixture);
    fixture
        .app
        .context_action(context::Action::Source(row.clone()))
        .unwrap();
    waiting.recv_timeout(Duration::from_secs(5)).unwrap();
    fixture.app.detail("New detail", "Still current");
    release.send(()).unwrap();
    fixture.drain_context_work();
    assert_eq!(fixture.app.dialog.as_ref().unwrap().title, "New detail");
    for changed in ["revision", "digest", "reference"] {
        let mut foreign = row.clone();
        match changed {
            "revision" => foreign.source.revision = "other-revision".into(),
            "digest" => foreign.source_sha256 = "0".repeat(64),
            "reference" => foreign.source_ref = format!("sources/{}.bin", "0".repeat(64)),
            _ => unreachable!(),
        }
        fixture
            .app
            .context_action(context::Action::Source(foreign))
            .unwrap();
        fixture.drain_context_work();
        assert!(
            fixture.app.notice.contains("source/revision changed"),
            "changed original {changed} must not open source bytes"
        );
        assert_eq!(fixture.app.dialog.as_ref().unwrap().title, "Context");
        assert!(
            fixture
                .app
                .dialog
                .as_ref()
                .unwrap()
                .description
                .contains("source/revision changed")
        );
    }
    fixture
        .app
        .context_action(context::Action::Source(row))
        .unwrap();
    fixture.drain_context_work();
    assert_eq!(
        fixture.app.dialog.as_ref().unwrap().title,
        "Captured source"
    );
}

#[test]
fn context_worker_explicit_lifecycle_and_drafts_drain_with_full_completion_queue() {
    for operation in ["record", "close", "revisit"] {
        let mut fixture = Fixture::new(0);
        synthetic_attachment(&mut fixture.app);
        let mut snapshot = live_snapshot(2);
        snapshot["projectId"] = json!("project-a");
        fixture
            .app
            .active
            .as_mut()
            .unwrap()
            .projection
            .apply_snapshot(snapshot, "thread-a")
            .unwrap();
        let original = context_input("lifecycle");
        let row = {
            let store = crate::context_store::Store::open(&context_path(&fixture)).unwrap();
            store.record(&original).unwrap();
            store
                .recall(&crate::context_store::Query::new(context_scope()))
                .unwrap()
                .rows
                .remove(0)
        };
        let (waiting, release) = hold_context(&mut fixture);
        match operation {
            "record" => fixture
                .app
                .context_action(context::Action::Record(context_input("accepted-record")))
                .unwrap(),
            "close" => fixture
                .app
                .context_action(context::Action::Close(row.clone()))
                .unwrap(),
            "revisit" => {
                fixture
                    .app
                    .context_revisit_edited(&row, "Original condition")
                    .unwrap();
                fixture
                    .app
                    .context_revisit(&row, "Original condition")
                    .unwrap();
            }
            _ => unreachable!(),
        }
        waiting.recv_timeout(Duration::from_secs(5)).unwrap();
        assert_eq!(fixture.app.notice, "Saving context…");
        // Context I/O remains held while the independent draft worker commits.
        fixture
            .app
            .replace_text("Independent exact draft 日本語\n  ".into());
        fixture.app.schedule_save().unwrap();
        loop {
            let event = fixture.app.rx.recv_timeout(Duration::from_secs(5)).unwrap();
            let saved = matches!(event, Background::Saved { .. });
            fixture.app.handle(event).unwrap();
            if saved {
                break;
            }
        }
        assert_eq!(
            fixture.app.state.draft("native-0", "thread-a"),
            "Independent exact draft 日本語\n  "
        );
        if operation == "revisit" {
            fixture
                .app
                .context_revisit_edited(&row, "Newer unsent condition 日本語")
                .unwrap();
        }
        fixture
            .app
            .context_capture_pending
            .insert("never-admitted".into(), context_input("never-admitted"));
        while fixture
            .app
            .tx
            .try_send(Background::Saved {
                key: "unrelated".into(),
                version: 0,
                result: Ok(()),
            })
            .is_ok()
        {}
        assert_eq!(fixture.app.rx.len(), 128);
        fixture.app.command("quit").unwrap();
        assert!(!fixture.app.quit);
        release.send(()).unwrap();
        fixture.app.drain_context().unwrap();
        assert!(fixture.app.quit);
        assert_eq!(
            fixture.app.context_capture_pending.len(),
            1,
            "draining rescheduled capture"
        );
        // A successful second open establishes that Store ownership was released
        // only after its accepted write completion and termination were consumed.
        let store = crate::context_store::Store::open(&context_path(&fixture)).unwrap();
        let mut query = crate::context_store::Query::new(context_scope());
        query.history = true;
        let pack = store.recall(&query).unwrap();
        assert!(
            !pack
                .rows
                .iter()
                .any(|row| row.source.item_id.as_deref() == Some("never-admitted"))
        );
        match operation {
            "record" => assert!(
                pack.rows
                    .iter()
                    .any(|row| row.source.item_id.as_deref() == Some("accepted-record"))
            ),
            "close" => assert!(
                pack.rows
                    .iter()
                    .any(|candidate| candidate.id == row.id && candidate.closed)
            ),
            "revisit" => {
                assert!(pack.rows.iter().any(|candidate| candidate.id == row.id
                    && candidate.revisit.as_deref() == Some("Original condition")));
                assert_eq!(
                    fixture
                        .app
                        .state
                        .draft("native-0", &format!("context-revisit-{}", row.id)),
                    "Newer unsent condition 日本語"
                );
                assert_ne!(fixture.app.notice, "Revisit saved");
            }
            _ => unreachable!(),
        }
        assert_eq!(store.source(&row.id).unwrap().bytes, original.full_source);
    }
}

#[test]
fn context_worker_real_slt_escape_cancels_pending_query_and_new_recall_remains_usable() {
    let mut fixture = Fixture::new(0);
    synthetic_attachment(&mut fixture.app);
    let draft = "  Unsent exact prompt 日本語 🙂\nnext line\n  ";
    fixture.app.replace_text(draft.into());
    fixture.app.flush_draft().unwrap();
    let pending = json!({"id":"original-protected-operation","environmentId":"native-0","op":"answer", "threadId":"thread-a", "requestId":"original-request", "revision":"original-revision", "answers":{"answer":"Keep exact"}});
    fixture
        .app
        .state
        .save_pending("native-0", false, pending.clone())
        .unwrap();
    let source = fixture.app.source(0, "thread-a");
    let generation = fixture.app.generation;
    let caret = fixture.app.composer.cursor();
    let (waiting, release) = hold_context(&mut fixture);
    fixture
        .app
        .context_action(context::Action::Find(context_scope()))
        .unwrap();
    fixture.app.modal_editor = ui::editor("old pending query", &fixture.app.theme, "");
    let mut surface = crate::slt_ui::SurfaceState::new();
    fixture
        .app
        .surface_action(crate::slt_ui::Action::ModalSubmit, &mut surface)
        .unwrap();
    waiting.recv_timeout(Duration::from_secs(5)).unwrap();
    assert_eq!(fixture.app.dialog.as_ref().unwrap().title, "Context");
    let mut backend = ::slt::TestBackend::new(110, 38)
        .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
    for _ in 0..3 {
        backend.render(|ctx| fixture.draw_slt(ctx, &mut surface));
    }
    backend.run_with_events(
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Esc)
            .build(),
        |ctx| fixture.draw_slt(ctx, &mut surface),
    );
    assert!(
        fixture.app.dialog.is_none(),
        "physical Esc path must close and invalidate the pending view"
    );
    release.send(()).unwrap();
    fixture.drain_context_work();
    assert!(
        fixture.app.dialog.is_none(),
        "old completion reopened the escaped view"
    );
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.composer.cursor(), caret);
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
    assert_eq!(
        fixture.app.state.pending("native-0", false),
        Some(pending.clone())
    );
    assert_eq!(fixture.app.source(0, "thread-a"), source);
    assert_eq!(fixture.app.active.as_ref().unwrap().profile, 0);
    assert_eq!(fixture.app.active.as_ref().unwrap().thread, "thread-a");
    assert_eq!(fixture.app.generation, generation);
    fixture.open(1);
    let mut other_scope = context_scope();
    other_scope.environment_id = "native-1".into();
    let pack = fixture.context_pack(crate::context_store::Query::new(other_scope.clone()));
    assert_eq!(pack.scope, other_scope);
    assert_eq!(
        fixture.app.dialog.as_ref().unwrap().title,
        "Context records"
    );
    assert_eq!(fixture.app.state.pending("native-0", false), Some(pending));
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
    for path in &fixture.native_states {
        if let Ok(bytes) = fs::read(path) {
            let native: Value = serde_json::from_slice(&bytes).unwrap();
            assert_eq!(native["effects"].as_u64().unwrap_or(0), 0);
        }
    }
}

#[test]
fn context_worker_retired_native_question_cannot_reopen_its_closed_read_view() {
    for cancelled in [false, true] {
        let mut fixture = Fixture::new(0);
        synthetic_attachment(&mut fixture.app);
        let original = json!({
            "id":"original-question", "revision":"a".repeat(64),
            "kind":"user_input_request", "supported":true,
            "questions":[{"id":"choice", "question":"Keep the original answer?",
                "allowCustomAnswer":true, "options":[{"label":"Continue"}]}]
        });
        let mut page = live_snapshot(2);
        page["projectId"] = json!("project-a");
        page["requests"] = json!([original.clone()]);
        let generation = fixture.app.generation;
        fixture
            .app
            .handle(Background::Packet {
                generation,
                packet: Packet::event("native-0", "view", page.clone()),
            })
            .unwrap();
        let draft = "  Original unsent prompt 日本語 🙂\nlast line  ";
        fixture.app.replace_text(draft.into());
        fixture.app.flush_draft().unwrap();
        let caret = fixture.app.composer.cursor();
        let source = fixture.app.source(0, "thread-a");
        let pending = json!({"id":"protected-retirement-operation", "op":"answer",
            "environmentId":"native-1", "threadId":"other-thread", "requestId":"other-request",
            "revision":"original-revision", "answers":{"choice":"Retain exact"}});
        fixture
            .app
            .state
            .save_pending("native-1", false, pending.clone())
            .unwrap();
        fixture.app.review_request(original.clone()).unwrap();
        fixture.app.edit_field(InputTarget::Question).unwrap();
        let freshest = "Fresh retained answer 日本語 🙂\n  unfinished ";
        fixture.app.modal_editor = ui::editor(freshest, &fixture.app.theme, "");
        let mut surface = crate::slt_ui::SurfaceState::new();
        fixture
            .app
            .surface_action(crate::slt_ui::Action::ModalEditChanged, &mut surface)
            .unwrap();
        fixture
            .app
            .surface_action(crate::slt_ui::Action::CloseModal, &mut surface)
            .unwrap();
        assert!(fixture.app.request_edit.is_some());
        let form = crate::attention::Form::from_request("native-0", "thread-a", &original).unwrap();
        assert_eq!(
            form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
            freshest
        );
        let (waiting, release) = hold_context(&mut fixture);
        fixture
            .app
            .context_recall(context_scope(), "held original read", false, 0)
            .unwrap();
        waiting.recv_timeout(Duration::from_secs(5)).unwrap();
        assert_eq!(fixture.app.dialog.as_ref().unwrap().title, "Context");
        let mut replacement = original.clone();
        replacement["revision"] = json!("b".repeat(64));
        page["revision"] = json!(3);
        page["requests"] = if cancelled {
            json!([])
        } else {
            json!([replacement.clone()])
        };
        fixture
            .app
            .handle(Background::Packet {
                generation,
                packet: Packet::event("native-0", "view", page),
            })
            .unwrap();
        assert!(fixture.app.request_edit.is_none() && fixture.app.dialog.is_none());
        // Always release before asserting late completion behavior so a failed
        // regression cannot strand the Store owner during fixture teardown.
        release.send(()).unwrap();
        fixture.drain_context_work();
        assert!(
            fixture.app.dialog.is_none(),
            "retired native question reopened its old context view, cancelled={cancelled}"
        );
        assert_eq!(fixture.app.text(), draft);
        assert_eq!(fixture.app.composer.cursor(), caret);
        assert_eq!(fixture.app.generation, generation);
        assert_eq!(fixture.app.source(0, "thread-a"), source);
        assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
        assert_eq!(
            fixture.app.state.pending("native-1", false),
            Some(pending.clone())
        );
        assert_eq!(
            form.load_draft(&fixture.app.state).unwrap().unwrap()["choice"],
            freshest
        );
        assert!(
            crate::attention::Form::from_request("native-0", "thread-a", &replacement)
                .unwrap()
                .load_draft(&fixture.app.state)
                .unwrap()
                .is_none()
        );
        let pack = fixture.context_pack(crate::context_store::Query::new(context_scope()));
        assert_eq!(pack.scope, context_scope());
        assert_eq!(
            fixture.app.dialog.as_ref().unwrap().title,
            "Context records"
        );
        assert_eq!(fixture.app.state.pending("native-1", false), Some(pending));
        assert!(fixture.native_states.iter().all(|path| !path.exists()));
    }
}

#[test]
fn r8_pointer_tab_restores_composer_and_sends_original_unicode_draft_once() {
    fn journal_files(root: &Path) -> std::collections::BTreeMap<PathBuf, (Vec<u8>, u32)> {
        let mut files = std::collections::BTreeMap::new();
        for entry in fs::read_dir(root).unwrap() {
            let path = entry.unwrap().path();
            if path.is_dir() {
                for (name, value) in journal_files(&path) {
                    files.insert(PathBuf::from(path.file_name().unwrap()).join(name), value);
                }
            } else {
                files.insert(
                    path.file_name().unwrap().into(),
                    (
                        fs::read(&path).unwrap(),
                        fs::metadata(&path).unwrap().permissions().mode(),
                    ),
                );
            }
        }
        files
    }
    fn settle_source(fixture: &mut Fixture) {
        let deadline = Instant::now() + Duration::from_secs(5);
        while fixture
            .app
            .active
            .as_ref()
            .is_none_or(|active| active.navigating)
        {
            let event = fixture
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("pointer-selected native source attachment");
            fixture.app.handle(event).unwrap();
        }
    }

    let mut fixture = Fixture::new(500);
    for environment in &fixture.app.environments {
        let mut config: Value =
            serde_json::from_slice(&fs::read(&environment.host_config).unwrap()).unwrap();
        config["fixture"]["persistInitialState"] = json!(true);
        fs::write(&environment.host_config, config.to_string()).unwrap();
    }
    fixture.load_pages();
    // Retain a genuine acknowledged host journal and an independent uncertain lane.
    fixture.open(1);
    fixture.app.replace_text("Retained journal intent".into());
    fixture.app.command("send").unwrap();
    let event = fixture.next_mutation();
    fixture.app.handle(event).unwrap();
    let other_native = fs::read(&fixture.native_states[1]).unwrap();
    let other_journal = fixture.directory.path().join("host-1/operations-v1");
    let journal_before = journal_files(&other_journal);
    assert!(!journal_before.is_empty());
    let recovery = json!({"id":"r8-other-uncertain","environmentId":"native-1","threadId":"thread-a","op":"send","message":"Original uncertain intent"});
    fixture
        .app
        .state
        .save_pending("native-1", false, recovery.clone())
        .unwrap();
    fixture.open(0);
    fixture
        .app
        .surface_action(
            crate::slt_ui::Action::CloseTab("1\0thread-a".into()),
            &mut crate::slt_ui::SurfaceState::new(),
        )
        .unwrap();
    let source = fixture.app.source(0, "thread-a");
    let native_before = fs::read(&fixture.native_states[0]).unwrap();
    let draft = "/help stays literal · café 日本語 alpha beta gamma alpha beta gamma\nsecond line  e\u{301} 👩\u{200d}💻  ";
    let other_draft = "independent tab draft";
    let mut surface = crate::slt_ui::SurfaceState::new();
    let mut backend = ::slt::TestBackend::new(140, 38)
        .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
    assert_eq!(
        model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new()).as_deref(),
        Some("composer")
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .paste(draft)
            .key_code(::slt::KeyCode::Left)
            .key_code(::slt::KeyCode::Left)
            .key_code(::slt::KeyCode::Left)
            .build(),
    );
    fixture.app.flush_draft().unwrap();
    let caret = fixture.app.composer.cursor();
    assert_ne!(caret, (1, draft.lines().last().unwrap().chars().count()));
    let (x, y) = backend
        .find_text("Finished refactor")
        .expect("navigator source");
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().click(x, y).build(),
    );
    settle_source(&mut fixture);
    model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    assert_eq!(fixture.app.active.as_ref().unwrap().thread, "thread-review");
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().paste(other_draft).build(),
    );
    fixture.app.flush_draft().unwrap();
    let (x, y) = backend
        .find_text("Active implementation")
        .expect("original conversation tab");
    assert_eq!(y, 1, "activate the conversation tab, not navigator content");
    let mut click_owners = Vec::new();
    backend.run_with_events(::slt::EventBuilder::new().click(x, y).build(), |ctx| {
        fixture.draw_slt(ctx, &mut surface);
        click_owners.push(ctx.focused_name().map(str::to_owned));
    });
    settle_source(&mut fixture);
    let owner = model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    assert_eq!(surface.source.as_ref(), Some(&source));
    assert_eq!(fixture.app.text(), draft);
    assert_eq!(fixture.app.composer.cursor(), caret);
    assert_eq!(fixture.app.state.draft("native-0", "thread-a"), draft);
    assert_eq!(
        fixture.app.state.draft("native-0", "thread-review"),
        other_draft
    );
    assert!(fixture.app.state.pending("native-0", false).is_none());
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
    let events = [::slt::KeyEventKind::Press, ::slt::KeyEventKind::Release]
        .into_iter()
        .map(|kind| {
            let mut event =
                ::slt::Event::key_mod(::slt::KeyCode::Enter, ::slt::KeyModifiers::CONTROL);
            if let ::slt::Event::Key(key) = &mut event {
                key.kind = kind;
            }
            event
        })
        .collect();
    model_tail_input(&mut backend, &mut fixture, &mut surface, events);
    let pending = fixture.app.state.pending("native-0", false)
        .unwrap_or_else(|| panic!("pointer tab must admit Ctrl+Enter Send; settled owner={owner:?}, click owners={click_owners:?}"));
    assert_eq!(owner.as_deref(), Some("composer"));
    assert_eq!(pending["op"], "send");
    assert_eq!(pending["threadId"], "thread-a");
    assert_eq!(pending["message"], draft);
    assert_eq!(fixture.app.state.pending_records().len(), 2);
    let event = fixture.next_mutation();
    assert!(
        matches!(&event, Background::Mutation { context, id, op, fields, result: Ok(_) }
        if context.profile == 0 && *id == pending["id"] && op == "send"
            && fields["threadId"] == "thread-a" && fields["message"] == draft)
    );
    fixture.app.handle(event).unwrap();
    model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    assert!(fixture.app.state.pending("native-0", false).is_none());
    assert_eq!(fixture.app.text(), "");
    let native: Value =
        serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
    assert_eq!(native["effects"], 1);
    assert_eq!(native["receipts"].as_object().unwrap().len(), 1);
    let sent: Vec<_> = native["projections"]["thread-a"]["turnItems"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|item| item["type"] == "user_message" && item["text"] == draft)
        .collect();
    assert_eq!(sent.len(), 1);
    assert_eq!(
        fixture.app.state.draft("native-0", "thread-review"),
        other_draft
    );
    assert_eq!(fixture.app.state.pending("native-1", false), Some(recovery));
    assert_eq!(fixture.app.state.pending_records().len(), 1);
    assert_eq!(fs::read(&fixture.native_states[1]).unwrap(), other_native);
    assert_eq!(journal_files(&other_journal), journal_before);
}

#[test]
fn r8_actual_tab_cycle_keeps_source_tab_owner_and_original_drafts() {
    let mut fixture = Fixture::new(0);
    let config_path = &fixture.app.environments[0].host_config;
    let mut config: Value = serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
    config["fixture"]["persistInitialState"] = json!(true);
    fs::write(config_path, config.to_string()).unwrap();
    fixture.load_pages();
    fixture.open(0);
    fixture.open_thread(0, "thread-question");
    fixture.open_thread(0, "thread-review");
    let draft = "Interior 日本語 café  \nold tail  \n";
    fixture.app.composer = ui::editor(draft, &fixture.app.theme, "");
    fixture.app.flush_draft().unwrap();
    let pending = json!({"id":"r8-original-operation","environmentId":"native-0","threadId":"thread-a","op":"send","message":"Original uncertain intent"});
    fixture
        .app
        .state
        .save_pending("native-0", false, pending.clone())
        .unwrap();
    let native_before = fs::read(&fixture.native_states[0]).unwrap();
    let stops_before = fixture.app.state.snapshot()["pendingStops"].clone();
    let mut surface = crate::slt_ui::SurfaceState::new();
    surface.show_context = true;
    let mut backend = ::slt::TestBackend::new(140, 38)
        .with_run_config(::slt::RunConfig::default().activation_boundaries(true));
    assert_eq!(
        model_tail_input(&mut backend, &mut fixture, &mut surface, Vec::new()).as_deref(),
        Some("composer")
    );
    let mut owners = Vec::new();
    for _ in 0..30 {
        let owner = model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Tab)
                .build(),
        );
        owners.push(owner.clone());
        if owner.as_deref() == Some("composer") {
            break;
        }
    }
    for expected in [
        "toolbar",
        "conversation-tabs",
        "navigator",
        "transcript",
        "context",
    ] {
        assert!(
            owners
                .iter()
                .any(|owner| owner.as_deref() == Some(expected)),
            "missing {expected}: {owners:?}"
        );
    }
    assert_eq!(owners.last().unwrap().as_deref(), Some("composer"));
    let mut a_reverse = Vec::new();
    for _ in 0..30 {
        let owner = model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::BackTab)
                .build(),
        );
        a_reverse.push(owner.clone());
        if owner.as_deref() == Some("composer") {
            break;
        }
    }
    let mut a_forward: Vec<_> = owners.iter().filter_map(|owner| owner.as_deref()).collect();
    let mut a_back: Vec<_> = a_reverse
        .iter()
        .filter_map(|owner| owner.as_deref())
        .collect();
    a_forward.sort();
    a_back.sort();
    assert_eq!(a_forward, a_back);
    let mut unique = a_forward.clone();
    unique.dedup();
    assert_eq!(a_forward, unique, "one stop per named composite");
    for _ in 0..30 {
        if model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::BackTab)
                .build(),
        )
        .as_deref()
            == Some("conversation-tabs")
        {
            break;
        }
    }
    let owner = model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Left)
            .build(),
    );
    assert_eq!(
        owner.as_deref(),
        Some("conversation-tabs"),
        "source activation must retain the tab composite"
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Right)
            .build(),
    );
    for _ in 0..30 {
        if model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Tab)
                .build(),
        )
        .as_deref()
            == Some("composer")
        {
            break;
        }
    }
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Up)
            .key_code(::slt::KeyCode::End)
            .key_code(::slt::KeyCode::Left)
            .key_code(::slt::KeyCode::Left)
            .build(),
    );
    for _ in 0..30 {
        if model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Tab)
                .build(),
        )
        .as_deref()
            == Some("toolbar")
        {
            break;
        }
    }
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Right)
            .key_code(::slt::KeyCode::Right)
            .key_code(::slt::KeyCode::Enter)
            .build(),
    );
    assert!(surface.attention_mode);
    let mut b_owners = Vec::new();
    for _ in 0..30 {
        let owner = model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Tab)
                .build(),
        );
        b_owners.push(owner.clone());
        if owner.as_deref() == Some("attention-list") {
            break;
        }
    }
    for expected in [
        "project-navigation",
        "attention-content",
        "attention-tabs",
        "toolbar",
        "context",
    ] {
        assert!(
            b_owners
                .iter()
                .any(|owner| owner.as_deref() == Some(expected)),
            "missing {expected}: {b_owners:?}"
        );
    }
    let mut reverse = Vec::new();
    for _ in 0..30 {
        let owner = model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::BackTab)
                .build(),
        );
        reverse.push(owner.clone());
        if owner.as_deref() == Some("attention-list") {
            break;
        }
    }
    let mut forward_names: Vec<_> = b_owners
        .iter()
        .filter_map(|owner| owner.as_deref())
        .collect();
    let mut reverse_names: Vec<_> = reverse
        .iter()
        .filter_map(|owner| owner.as_deref())
        .collect();
    forward_names.sort();
    reverse_names.sort();
    assert_eq!(forward_names, reverse_names);
    for _ in 0..30 {
        if model_tail_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Tab)
                .build(),
        )
        .as_deref()
            == Some("toolbar")
        {
            break;
        }
    }
    let returned = model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Left)
            .key_code(::slt::KeyCode::Enter)
            .build(),
    );
    assert!(!surface.attention_mode);
    assert_eq!(returned.as_deref(), Some("composer"));
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().key('X').build(),
    );
    assert_eq!(
        fixture.app.text(),
        draft.replace("old tail  \n", "old tailX  \n")
    );
    model_tail_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Backspace)
            .build(),
    );
    assert_eq!(fs::read(&fixture.native_states[0]).unwrap(), native_before);
    assert_eq!(fixture.app.state.snapshot()["pendingStops"], stops_before);
    assert_eq!(fixture.app.state.draft("native-0", "thread-review"), draft);
    assert_eq!(fixture.app.state.pending("native-0", false), Some(pending));
}

fn r9_input(
    backend: &mut crate::slt_ui::tests::ResizableBackend,
    fixture: &mut Fixture,
    surface: &mut crate::slt_ui::SurfaceState,
    events: Vec<::slt::Event>,
) -> Option<String> {
    backend.run_with_events(events, |ctx| fixture.draw_slt(ctx, surface));
    let mut owner = None;
    for _ in 0..8 {
        backend.render(|ctx| {
            fixture.draw_slt(ctx, surface);
            owner = ctx.focused_name().map(str::to_owned);
        });
    }
    owner
}

#[test]
fn r9_compact_retained_form_keeps_review_and_source_actions_without_mutation() {
    for context in [false, true] {
        for keyboard in [false, true] {
            let mut fixture = Fixture::new(0);
            let config_path = &fixture.app.environments[0].host_config;
            let mut config: Value =
                serde_json::from_slice(&fs::read(config_path).unwrap()).unwrap();
            config["fixture"]["persistInitialState"] = json!(true);
            fs::write(config_path, config.to_string()).unwrap();
            fixture.open(1);
            fixture
                .app
                .replace_text("Retained acknowledged journal intent".into());
            fixture.app.command("send").unwrap();
            let event = fixture.next_mutation();
            fixture.app.handle(event).unwrap();
            let journal_root = fixture.directory.path().join("host-1/operations-v1");
            let journal = r9_files(&journal_root);
            assert!(!journal.is_empty());
            fixture.load_pages();
            fixture.open_thread(0, "thread-question");
            let mut surface = crate::slt_ui::SurfaceState::new();
            let mut backend =
                crate::slt_ui::tests::ResizableBackend::new(140, 38).with_activation_boundaries();
            r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            let draft = "Retained 日本語 café e\u{301} 👩\u{200d}💻  \ninterior caret  ";
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .paste(draft)
                    .key_code(::slt::KeyCode::Left)
                    .key_code(::slt::KeyCode::Left)
                    .build(),
            );
            fixture.app.flush_draft().unwrap();
            let caret = fixture.app.composer.cursor();
            let source = fixture.app.source(0, "thread-question");
            let pending = json!({"id":"r9-original-uncertain", "environmentId":"native-1", "threadId":"thread-a", "op":"send", "message":"exact original intent"});
            fixture
                .app
                .state
                .save_pending("native-1", false, pending.clone())
                .unwrap();
            let recovery = fixture.app.state.snapshot();
            let native = fixture
                .native_states
                .iter()
                .map(|p| {
                    (
                        fs::read(p).ok(),
                        fs::metadata(p).ok().map(|m| m.permissions().mode()),
                    )
                })
                .collect::<Vec<_>>();
            let question = surface
                .questions
                .iter()
                .find(|item| item.source.thread == "thread-question")
                .unwrap()
                .clone();
            surface.selected_question = Some(question.key.clone());
            surface.retained_question = Some(question);
            surface.attention_mode = true;
            r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            let original = surface.retained_question.as_ref().unwrap().request.clone();
            surface.selected_project = Some("native-0\0project-empty".into());
            surface.show_context = context;
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new().resize(40, 12).build(),
            );
            backend.assert_contains("What should happen next?");
            backend.assert_contains("Retained");
            backend.assert_contains("Fixture project");
            let (x, y) = backend.find_text("Open form").unwrap_or_else(|| {
                panic!(
                    "compact retained review action missing, context={context}:\n{}",
                    backend.text()
                )
            });
            backend.assert_contains("Open in A");
            if keyboard {
                let mut owners = Vec::new();
                for _ in 0..30 {
                    let owner = r9_input(
                        &mut backend,
                        &mut fixture,
                        &mut surface,
                        ::slt::EventBuilder::new()
                            .key_code(::slt::KeyCode::Tab)
                            .build(),
                    );
                    owners.push(owner);
                    if owners.last().unwrap().as_deref() == Some("form-review") {
                        break;
                    }
                }
                assert_eq!(
                    owners.last().unwrap().as_deref(),
                    Some("form-review"),
                    "{owners:?}"
                );
                r9_input(
                    &mut backend,
                    &mut fixture,
                    &mut surface,
                    ::slt::EventBuilder::new()
                        .key_code(::slt::KeyCode::Enter)
                        .build(),
                );
            } else {
                r9_input(
                    &mut backend,
                    &mut fixture,
                    &mut surface,
                    ::slt::EventBuilder::new().click(x, y).build(),
                );
            }
            assert!(
                fixture.app.request_edit.is_some(),
                "Open form enters native review"
            );
            assert_eq!(fixture.app.request_edit.as_ref().unwrap().request, original);
            assert_eq!(fixture.app.text(), draft);
            assert_eq!(fixture.app.composer.cursor(), caret);
            assert_eq!(fixture.app.source(0, "thread-question"), source);
            assert_eq!(fixture.app.state.snapshot()["pending"], recovery["pending"]);
            assert_eq!(
                fixture.app.state.snapshot()["pendingStops"],
                recovery["pendingStops"]
            );
            assert_eq!(fixture.app.state.pending("native-1", false), Some(pending));
            assert_eq!(
                fixture
                    .native_states
                    .iter()
                    .map(|p| (
                        fs::read(p).ok(),
                        fs::metadata(p).ok().map(|m| m.permissions().mode())
                    ))
                    .collect::<Vec<_>>(),
                native
            );
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Esc)
                    .build(),
            );
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new().resize(86, 31).build(),
            );
            backend.assert_contains("Answer");
            backend.assert_contains("Open in A");
            assert_eq!(
                surface.retained_question.as_ref().unwrap().request,
                original
            );
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new().resize(40, 12).build(),
            );
            let (x, y) = backend.find_text("Open in A").unwrap();
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new().click(x, y).build(),
            );
            assert!(!surface.attention_mode);
            assert_eq!(fixture.app.source(0, "thread-question"), source);
            assert_eq!(fixture.app.text(), draft);
            assert_eq!(fixture.app.composer.cursor(), caret);
            assert_eq!(r9_files(&journal_root), journal);
        }
    }
}

#[test]
fn r9_small_a_keeps_transcript_body_with_unicode_interior_caret() {
    for context in [false, true] {
        let mut fixture = Fixture::new(0);
        fixture.open(0);
        let mut surface = crate::slt_ui::SurfaceState::new();
        surface.show_context = context;
        let mut backend =
            crate::slt_ui::tests::ResizableBackend::new(140, 38).with_activation_boundaries();
        r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
        let draft = "日本語 café e\u{301} 👩\u{200d}💻 retained draft  \nsecond interior line  ";
        r9_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .paste(draft)
                .key_code(::slt::KeyCode::Left)
                .key_code(::slt::KeyCode::Left)
                .build(),
        );
        fixture.app.flush_draft().unwrap();
        let caret = fixture.app.composer.cursor();
        let source = fixture.app.source(0, "thread-a");
        let follow = fixture.app.follow;
        r9_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new().resize(40, 12).build(),
        );
        assert!(
            surface
                .transcript_layout
                .as_ref()
                .is_some_and(|layout| layout.visible > 0),
            "useful transcript viewport required, context={context}:\n{}",
            backend.text()
        );
        assert_eq!(fixture.app.text(), draft);
        assert_eq!(fixture.app.composer.cursor(), caret);
        assert_eq!(fixture.app.source(0, "thread-a"), source);
        assert_eq!(
            fixture.app.follow,
            follow,
            "context={context}, layout={:?}\n{}",
            surface.transcript_layout,
            backend.text()
        );
        for toggled in [!context, context] {
            surface.show_context = toggled;
            r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            assert!(
                surface
                    .transcript_layout
                    .as_ref()
                    .is_some_and(|layout| layout.visible > 0)
            );
            assert_eq!(fixture.app.follow, follow, "context toggle retains follow");
            assert_eq!(fixture.app.text(), draft);
            assert_eq!(fixture.app.composer.cursor(), caret);
        }
        assert!(fixture.app.state.pending_records().is_empty());
        assert!(fixture.native_states.iter().all(|p| !p.exists()));
    }
}

#[test]
fn r9_actual_divider_drag_retains_default_and_manual_width_across_resize() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open(0);
    let mut surface = crate::slt_ui::SurfaceState::new();
    let mut backend =
        crate::slt_ui::tests::ResizableBackend::new(140, 38).with_activation_boundaries();
    r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    let mut nav = None;
    backend.render(|ctx| {
        fixture.draw_slt(ctx, &mut surface);
        nav = ctx.measured_layout_rect("t3oxide.navigator");
    });
    let rect = nav.unwrap();
    assert_eq!(rect.width, 30);
    // The hit belongs to the rendered edge, whose origin includes the container offset.
    let handle = rect.right();
    let target = rect.x + 42;
    for events in [
        ::slt::EventBuilder::new().click(handle, rect.y + 5).build(),
        ::slt::EventBuilder::new().drag(target, rect.y + 5).build(),
        ::slt::EventBuilder::new()
            .mouse_up(target, rect.y + 5)
            .build(),
    ] {
        r9_input(&mut backend, &mut fixture, &mut surface, events);
    }
    for width in [140, 86, 70, 40, 140] {
        r9_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new().resize(width, 38).build(),
        );
        backend.render(|ctx| {
            fixture.draw_slt(ctx, &mut surface);
            nav = ctx.measured_layout_rect("t3oxide.navigator");
        });
        if width >= 86 {
            assert_eq!(nav.unwrap().width, 42, "manual preference at width {width}");
        }
    }
    assert!(fixture.app.state.pending_records().is_empty());
    assert!(fixture.native_states.iter().all(|p| !p.exists()));
}

#[test]
fn r9_complete_graph_focus_cycle_uses_original_project_identity() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    for host in &mut fixture.app.hosts {
        for project in &mut host.projects {
            if project["id"] == "project-a" {
                project["title"] = json!("Atlas");
            }
        }
    }
    fixture.open_thread(0, "thread-question");
    let mut surface = crate::slt_ui::SurfaceState::new();
    let mut backend =
        crate::slt_ui::tests::ResizableBackend::new(140, 38).with_activation_boundaries();
    r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    assert_eq!(
        surface
            .projects
            .iter()
            .filter(|p| p.project == "Atlas")
            .count(),
        2
    );
    surface.attention_mode = true;
    surface.selected_project = Some("native-0\0project-a".into());
    surface.graph_global = false;
    r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
    let original_source = fixture.app.source(0, "thread-question");
    let retained = surface.retained_question.clone();
    let (x, y) = backend.find_text("All projects").unwrap();
    r9_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new().click(x, y).build(),
    );
    assert!(surface.graph_global);
    let mut owners = Vec::new();
    for _ in 0..30 {
        let owner = r9_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new()
                .key_code(::slt::KeyCode::Tab)
                .build(),
        );
        owners.push(owner.clone());
        if owner.as_deref() == Some("project-navigation") {
            break;
        }
    }
    assert_eq!(
        owners.last().unwrap().as_deref(),
        Some("project-navigation"),
        "{owners:?}"
    );
    r9_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Down)
            .build(),
    );
    assert_eq!(
        surface.selected_project.as_deref(),
        Some("native-0\0project-a")
    );
    assert_eq!(fixture.app.source(0, "thread-question"), original_source);
    backend.assert_contains("Empty project");
    let selected_paint = backend.text();
    r9_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Tab)
            .build(),
    );
    let owner = r9_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::BackTab)
            .build(),
    );
    assert_eq!(owner.as_deref(), Some("project-navigation"));
    assert_eq!(
        backend.text(),
        selected_paint,
        "Tab away/back keeps graph viewport and selection paint"
    );
    r9_input(
        &mut backend,
        &mut fixture,
        &mut surface,
        ::slt::EventBuilder::new()
            .key_code(::slt::KeyCode::Enter)
            .build(),
    );
    assert_eq!(
        surface.selected_project.as_deref(),
        Some("native-0\0project-empty")
    );
    assert!(!surface.graph_global);
    assert_eq!(
        surface.retained_question.as_ref().map(|q| &q.request),
        retained.as_ref().map(|q| &q.request)
    );
    assert_eq!(fixture.app.source(0, "thread-question"), original_source);
    assert!(fixture.app.state.pending_records().is_empty());
    assert!(fixture.native_states.iter().all(|p| !p.exists()));
}

fn r9_files(root: &Path) -> std::collections::BTreeMap<PathBuf, (Vec<u8>, u32)> {
    let mut files = std::collections::BTreeMap::new();
    for entry in fs::read_dir(root).unwrap() {
        let path = entry.unwrap().path();
        let name = PathBuf::from(path.file_name().unwrap());
        if path.is_dir() {
            for (child, value) in r9_files(&path) {
                files.insert(name.join(child), value);
            }
        } else {
            files.insert(
                name,
                (
                    fs::read(&path).unwrap(),
                    fs::metadata(&path).unwrap().permissions().mode(),
                ),
            );
        }
    }
    files
}

#[test]
fn r9_compact_retained_form_fails_closed_after_source_or_delivery_changes() {
    for condition in [
        "changed",
        "unavailable",
        "submitting",
        "uncertain",
        "acknowledged",
    ] {
        let mut fixture = Fixture::new(0);
        fixture.load_pages();
        fixture.open_thread(0, "thread-question");
        let mut surface = crate::slt_ui::SurfaceState::new();
        fixture.app.populate_surface(&mut surface);
        let question = surface
            .questions
            .iter()
            .find(|q| q.source.thread == "thread-question")
            .unwrap()
            .clone();
        surface.selected_question = Some(question.key.clone());
        surface.retained_question = Some(question.clone());
        surface.attention_mode = true;
        let mut backend =
            crate::slt_ui::tests::ResizableBackend::new(40, 12).with_activation_boundaries();
        r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
        let (x, y) = backend.find_text("Open form").unwrap();
        if condition == "changed" || condition == "unavailable" {
            let active = fixture.app.active.as_ref().unwrap();
            let generation = active.generation;
            let mut page = active.projection.snapshot().unwrap().as_wire_value();
            page["revision"] = json!(page["revision"].as_u64().unwrap() + 1);
            let mut replacement = question.request.clone();
            replacement["revision"] = json!("b".repeat(64));
            page["requests"] = if condition == "changed" {
                json!([replacement])
            } else {
                json!([])
            };
            fixture
                .app
                .handle(Background::Packet {
                    generation,
                    packet: Packet::event("native-0", "view", page),
                })
                .unwrap();
        } else {
            let phase = match condition {
                "submitting" => AnswerPhase::Submitting,
                "uncertain" => AnswerPhase::Uncertain,
                _ => AnswerPhase::Acknowledged,
            };
            fixture.app.remember_answer_delivery(0, "original-answer-operation", &json!({"threadId":"thread-question", "requestId":question.request["id"], "revision":question.request["revision"]}), phase);
        }
        r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
        r9_input(
            &mut backend,
            &mut fixture,
            &mut surface,
            ::slt::EventBuilder::new().click(x, y).build(),
        );
        assert!(
            fixture.app.request_edit.is_none(),
            "{condition} admitted an obsolete form"
        );
        assert!(fixture.app.state.pending_records().is_empty());
        assert!(fixture.native_states.iter().all(|p| !p.exists()));
        assert_eq!(
            surface.retained_question.as_ref().unwrap().request,
            question.request
        );
    }
}

fn transcript_tab_to(
    backend: &mut crate::slt_ui::tests::ResizableBackend,
    fixture: &mut Fixture,
    surface: &mut crate::slt_ui::SurfaceState,
    target: &str,
    reverse: bool,
) {
    let mut owners = Vec::new();
    for _ in 0..40 {
        let owner = r9_input(
            backend,
            fixture,
            surface,
            ::slt::EventBuilder::new()
                .key_code(if reverse {
                    ::slt::KeyCode::BackTab
                } else {
                    ::slt::KeyCode::Tab
                })
                .build(),
        );
        owners.push(owner.clone());
        if owner.as_deref() == Some(target) {
            return;
        }
    }
    panic!("Tab traversal did not reach {target}: {owners:?}");
}

fn transcript_body(
    backend: &mut crate::slt_ui::tests::ResizableBackend,
    fixture: &mut Fixture,
    surface: &mut crate::slt_ui::SurfaceState,
) -> String {
    let mut rect = None;
    backend.render(|ctx| {
        fixture.draw_slt(ctx, surface);
        rect = ctx.measured_layout_rect(&format!("t3oxide.transcript:{}", surface.active_key));
    });
    let rect = rect.expect("accepted transcript geometry");
    backend
        .text()
        .lines()
        .skip(rect.y as usize)
        .take(rect.height as usize)
        .collect::<Vec<_>>()
        .join("\n")
}

#[test]
fn focused_transcript_keyboard_scroll_preserves_source_draft_and_recovery() {
    const BODY: &str = concat!(
        "## Research\n\n### Release evidence\n",
        "- Source revision is retained.\n- The next decision has two explicit options.\n\n",
        "| Check | Result |\n|---|---|\n| Draft recovery | Preserved |\n",
        "| Delivery | Requires acknowledgement |\n\n```mermaid\nflowchart TD\n",
        "  Review[Review source] --> Submit[Submit answer]\n",
        "  Submit --> Ack[Native acknowledgement]\n```\n\n",
        "### Remaining\nVerify the physical terminal input path."
    );
    for (width, height) in [(40, 12), (70, 22)] {
        for context in [false, true] {
            let mut fixture = Fixture::new(0);
            for environment in &fixture.app.environments {
                let mut config: Value =
                    serde_json::from_slice(&fs::read(&environment.host_config).unwrap()).unwrap();
                config["fixture"]["persistInitialState"] = json!(true);
                fs::write(&environment.host_config, config.to_string()).unwrap();
            }
            fixture.open(1);
            fixture.app.replace_text("Retained journal intent".into());
            fixture.app.command("send").unwrap();
            let event = fixture.next_mutation();
            fixture.app.handle(event).unwrap();
            fixture.load_pages();
            fixture.open(0);
            // Reattachment must read the same original body as the first frame.
            let mut native_state: Value =
                serde_json::from_slice(&fs::read(&fixture.native_states[0]).unwrap()).unwrap();
            native_state["projections"]["thread-a"]["visibleTurnItems"][1]["item"]["text"] =
                json!(BODY);
            native_state["projections"]["thread-a"]["turnItems"][1]["text"] = json!(BODY);
            fs::write(&fixture.native_states[0], native_state.to_string()).unwrap();
            fixture.open_thread(0, "thread-review");
            fixture.open(0);
            let mut surface = crate::slt_ui::SurfaceState::new();
            let mut backend =
                crate::slt_ui::tests::ResizableBackend::new(140, 38).with_activation_boundaries();
            r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            let draft =
                "日本語 café e\u{301} 👩\u{200d}💻 retained draft  \nsecond interior line  ";
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .paste(draft)
                    .key_code(::slt::KeyCode::Left)
                    .key_code(::slt::KeyCode::Left)
                    .build(),
            );
            fixture.app.flush_draft().unwrap();
            let caret = fixture.app.composer.cursor();
            fixture
                .app
                .state
                .set_draft("native-0", "thread-review", "Independent 日本語 draft  \n")
                .unwrap();
            for (stop, operation) in [(false, "send"), (true, "stop")] {
                fixture.app.state.save_pending("native-1", stop, json!({
                    "id":format!("original-uncertain-{operation}"), "environmentId":"native-1",
                    "threadId":"thread-a", "op":operation, "message":"Exact independent intent"
                })).unwrap();
            }
            let recovery = fixture.app.state.snapshot();
            let native = fixture
                .native_states
                .iter()
                .map(|p| {
                    (
                        fs::read(p).unwrap(),
                        fs::metadata(p).unwrap().permissions().mode(),
                    )
                })
                .collect::<Vec<_>>();
            let journals = (0..2)
                .map(|index| {
                    let root = fixture
                        .directory
                        .path()
                        .join(format!("host-{index}/operations-v1"));
                    let files = root.exists().then(|| r9_files(&root));
                    (root, files)
                })
                .collect::<Vec<_>>();
            assert!(
                journals[1]
                    .1
                    .as_ref()
                    .is_some_and(|files| !files.is_empty())
            );
            let source = fixture.app.source(0, "thread-a");
            surface.show_context = context;
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new().resize(width, height).build(),
            );
            transcript_tab_to(
                &mut backend,
                &mut fixture,
                &mut surface,
                "transcript",
                false,
            );
            let owner = r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            assert_eq!(owner.as_deref(), Some("transcript"));
            let tail = surface.transcript_layout.as_ref().unwrap().top;
            assert!(tail > 2, "overflow at {width}x{height}, context={context}");
            let before = transcript_body(&mut backend, &mut fixture, &mut surface);
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Up)
                    .build(),
            );
            assert_eq!(
                fixture.app.transcript_scroll,
                tail - 1,
                "focused Up must move accepted layout at {width}x{height}, context={context}:\n{}",
                backend.text()
            );
            assert!(!fixture.app.follow, "keyboard reading leaves follow");
            assert_ne!(
                transcript_body(&mut backend, &mut fixture, &mut surface),
                before,
                "Up must move actual transcript body rows"
            );
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Down)
                    .build(),
            );
            assert_eq!(fixture.app.transcript_scroll, tail);
            assert!(
                !fixture.app.follow,
                "manual Down to the tail retains manual intent"
            );
            let page = surface.transcript_layout.as_ref().unwrap().visible.max(1);
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::PageUp)
                    .build(),
            );
            assert_eq!(fixture.app.transcript_scroll, tail.saturating_sub(page));
            assert_ne!(
                transcript_body(&mut backend, &mut fixture, &mut surface),
                before
            );
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::PageDown)
                    .build(),
            );
            assert_eq!(fixture.app.transcript_scroll, tail);
            assert!(!fixture.app.follow);
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::PageUp)
                    .build(),
            );
            fixture.app.save_ui().unwrap();
            let identity = json!(["profile-0", "native-0", "thread-a"]).to_string();
            let manual_before_resize =
                fixture.app.state.snapshot()["rustReadingPositions"][&identity].clone();
            for (w, h) in [(140, 38), (width, height)] {
                r9_input(
                    &mut backend,
                    &mut fixture,
                    &mut surface,
                    ::slt::EventBuilder::new().resize(w, h).build(),
                );
                assert!(
                    !fixture.app.follow,
                    "resize preserves manual reading intent"
                );
            }
            surface.show_context = !context;
            r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            surface.show_context = context;
            r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            assert!(!fixture.app.follow);
            fixture.app.save_ui().unwrap();
            let manual = fixture.app.state.snapshot()["rustReadingPositions"][&identity].clone();
            // Reflow can clamp a row offset; source identity and manual intent
            // survive, and returning must restore this accepted position.
            for key in ["rowKey", "digest", "follow"] {
                assert_eq!(manual[key], manual_before_resize[key]);
            }
            fixture.open_thread(0, "thread-review");
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::PageUp)
                    .build(),
            );
            assert!(
                fixture.app.follow,
                "old transcript input cannot scroll the new source"
            );
            fixture.open(0);
            r9_input(&mut backend, &mut fixture, &mut surface, Vec::new());
            assert!(!fixture.app.follow, "source return retains manual intent");
            fixture.app.save_ui().unwrap();
            assert_eq!(
                fixture.app.state.snapshot()["rustReadingPositions"][&identity],
                manual
            );
            transcript_tab_to(
                &mut backend,
                &mut fixture,
                &mut surface,
                "transcript-tools",
                true,
            );
            r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Home)
                    .key_code(::slt::KeyCode::Right)
                    .key_code(::slt::KeyCode::Enter)
                    .build(),
            );
            assert!(fixture.app.follow, "explicit Latest resumes follow");
            assert_eq!(
                fixture.app.transcript_scroll,
                surface.transcript_layout.as_ref().unwrap().max
            );
            let tail = fixture.app.transcript_scroll;
            let owner = r9_input(
                &mut backend,
                &mut fixture,
                &mut surface,
                ::slt::EventBuilder::new()
                    .key_code(::slt::KeyCode::Up)
                    .key_code(::slt::KeyCode::PageUp)
                    .key_code(::slt::KeyCode::Down)
                    .key_code(::slt::KeyCode::PageDown)
                    .build(),
            );
            assert_eq!(owner.as_deref(), Some("transcript-tools"));
            assert_eq!(
                fixture.app.transcript_scroll, tail,
                "tools do not own transcript scrolling"
            );
            assert!(fixture.app.follow);
            assert_eq!(
                fixture
                    .app
                    .active
                    .as_ref()
                    .unwrap()
                    .projection
                    .snapshot()
                    .unwrap()
                    .as_wire_value()["items"][1]["text"],
                BODY
            );
            assert_eq!(fixture.app.text(), draft);
            assert_eq!(fixture.app.composer.cursor(), caret);
            assert_eq!(fixture.app.source(0, "thread-a"), source);
            assert!(fixture.app.dialog.is_none());
            assert!(fixture.app.request_edit.is_none());
            assert!(!surface.attention_mode);
            let after = fixture.app.state.snapshot();
            for key in ["drafts", "pending", "pendingStops"] {
                assert_eq!(after[key], recovery[key], "preserve exact {key}");
            }
            for (path, original) in fixture.native_states.iter().zip(native) {
                assert_eq!(
                    (
                        fs::read(path).unwrap(),
                        fs::metadata(path).unwrap().permissions().mode()
                    ),
                    original
                );
            }
            for (root, files) in journals {
                assert_eq!(root.exists().then(|| r9_files(&root)), files);
            }
        }
    }
}
