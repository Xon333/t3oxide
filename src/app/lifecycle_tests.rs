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
            "pendingRequestCount":u64::from(request),"queuedCount":0,"runId":format!("run-{id}"),"pinnedAt":if pinned {"native-pin"} else {""}}})).unwrap()
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
        requests: Vec::new(),
        status: "Live".into(),
    });
    // Coalesce reads so the requested source is observable without networking.
    app.hosts[0].loading = true;
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
    use crate::context_store::{Query, Scope, Store};
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
    fixture.app.dispatch(capture).unwrap();
    let mut context_path = fixture.app.state.path().as_os_str().to_os_string();
    context_path.push(".context");
    let store = Store::open(Path::new(&context_path)).unwrap();
    let query = Query::new(Scope {
        environment_id: "native-0".into(),
        project_id: Some("project-a".into()),
        thread_id: Some("thread-review".into()),
    });
    let pack = store.recall(&query).unwrap();
    assert_eq!(pack.eligible, 1);
    assert_eq!(pack.rows[0].source.environment_id, "native-0");
    assert_eq!(
        pack.rows[0].source.item_id.as_deref(),
        Some("thread-review-answer")
    );
    let captured: Value =
        serde_json::from_slice(&store.source(&pack.rows[0].id).unwrap().bytes).unwrap();
    assert!(string(&captured, "text").contains("日本語 café 🙂"));
    assert_eq!(
        store
            .recall(&Query::new(Scope {
                environment_id: "native-1".into(),
                ..query.scope.clone()
            }))
            .unwrap()
            .eligible,
        0
    );
    drop(store);
    fixture
        .app
        .context_action(context::Action::Source(pack.rows[0].id.clone()))
        .unwrap();
    let detail = &fixture.app.dialog.as_ref().unwrap().detail;
    assert!(detail.contains("native-0") && detail.contains("project-a"));
    assert!(detail.contains("capturedItemOnly") && detail.contains("historical"));
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
    fixture.app.search_projects();
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
fn loaded_navigation_and_restore_read_active_slt_layout() {
    let mut fixture = Fixture::new(0);
    fixture.load_pages();
    fixture.open_thread(0, "thread-review");
    let mut backend = ::slt::TestBackend::new(110, 38);
    let mut surface = crate::slt_ui::SurfaceState::new();
    let render = |app: &mut App,
                  backend: &mut ::slt::TestBackend,
                  surface: &mut crate::slt_ui::SurfaceState| {
        app.populate_surface(surface);
        let view = app.view();
        backend.render(|ctx| {
            crate::slt_ui::draw(
                ctx,
                &view,
                &mut app.composer,
                &mut app.modal_editor,
                None,
                surface,
            );
        });
        app.observe_slt_layout(surface);
    };
    for _ in 0..3 {
        render(&mut fixture.app, &mut backend, &mut surface);
    }
    fixture.app.command("find Ready for review").unwrap();
    fixture.app.command("find-next").unwrap();
    assert!(fixture.app.workspace.pending_jump.is_some());
    render(&mut fixture.app, &mut backend, &mut surface);
    assert!(
        fixture.app.workspace.pending_jump.is_none(),
        "the actual SLT layout consumed the source-qualified jump"
    );
    fixture.app.save_reading_position().unwrap();
    assert!(fixture.app.state.snapshot()["rustReadingPositions"].is_object());
}
