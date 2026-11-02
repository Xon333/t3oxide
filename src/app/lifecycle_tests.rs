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
        self.app.open(profile, "thread-a".into()).unwrap();
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
