//! Rust client ↔ actual Node companion, backed by deterministic native endpoints.
use serde_json::{Value, json};
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};
use t3oxide::{
    config::Environment,
    projection::Projection,
    state::State,
    wire::{Connection, new_id},
};

struct Fixture {
    directory: PathBuf,
    environment: Environment,
}
impl Fixture {
    fn new(extra: Value) -> Self {
        let directory =
            std::env::temp_dir().join(format!("t3oxide-real-protocol-test-{}", new_id()));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let node = std::env::split_paths(&std::env::var_os("PATH").unwrap_or_default())
            .map(|p| p.join("node"))
            .find(|p| p.is_file())
            .expect("Node is required");
        let adapter = Path::new(file!())
            .canonicalize()
            .unwrap()
            .parent()
            .unwrap()
            .join("rust_fixture.mjs");
        let token = directory.join("fixture-token");
        fs::write(&token, "local-test-token").unwrap();
        fs::set_permissions(&token, fs::Permissions::from_mode(0o600)).unwrap();
        let config = directory.join("host.json");
        let mut options = json!({"stateFile":directory.join("native-state.json"),"logFile":directory.join("native-log.ndjson"),"stream":true});
        options
            .as_object_mut()
            .unwrap()
            .extend(extra.as_object().unwrap().clone());
        fs::write(&config,json!({"baseUrl":"http://127.0.0.1:1","environmentId":"test-native","tokenFile":token,"allowWrites":true,"fixture":options}).to_string()).unwrap();
        fs::set_permissions(&config, fs::Permissions::from_mode(0o600)).unwrap();
        Self {
            environment: Environment {
                id: "test".into(),
                label: "Fixture".into(),
                environment_id: "test-native".into(),
                ssh_destination: None,
                node: node.to_string_lossy().into(),
                adapter: adapter.to_string_lossy().into(),
                host_config: config.to_string_lossy().into(),
                web_url: None,
                enabled: true,
                default_for_new: true,
                allowed_provider_instance_ids: None,
            },
            directory,
        }
    }
    fn connect(&self) -> Connection {
        let connection = Connection::start(self.environment.clone()).unwrap();
        connection.hello().unwrap();
        connection
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.directory);
    }
}

#[test]
fn real_companion_stream_and_history_remain_source_qualified() {
    let fixture = Fixture::new(json!({}));
    let connection = fixture.connect();
    let events = connection.events();
    let page = connection
        .request(
            "list",
            json!({"lifecycle":"all","includeChildren":true}),
            None,
        )
        .unwrap();
    assert!(page["threads"].as_array().unwrap().len() >= 7);
    connection
        .request("watch", json!({"threadId":"thread-a"}), None)
        .unwrap();
    let mut projection = Projection::default();
    projection.select("thread-a");
    let deadline = Instant::now() + Duration::from_secs(4);
    let mut streamed = false;
    while Instant::now() < deadline {
        let packet = events
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .unwrap();
        if packet.event == "view" {
            projection.apply_snapshot(packet.data, "thread-a").unwrap();
        } else if packet.event == "delta" {
            projection.apply_delta(packet.data).unwrap();
        }
        if projection.snapshot().is_some_and(|s| {
            s.items
                .iter()
                .any(|r| r.text.contains("Streaming update 3"))
        }) {
            streamed = true;
            break;
        }
    }
    assert!(streamed, "native stream did not reach Rust projection");
    let live = projection.snapshot().unwrap();
    assert!(live.is_live());
    let cursor = live.metadata["historyCursor"].clone();
    assert!(cursor.is_string());
    connection
        .request("history", json!({"cursor":cursor}), None)
        .unwrap();
    let deadline = Instant::now() + Duration::from_secs(4);
    loop {
        let packet = events
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .unwrap();
        if packet.event == "view" {
            projection.apply_snapshot(packet.data, "thread-a").unwrap();
        } else if packet.event == "delta" {
            projection.apply_delta(packet.data).unwrap();
        }
        if projection.snapshot().unwrap().metadata["state"] == "historical" {
            break;
        }
    }
    assert!(!projection.snapshot().unwrap().is_live());
    connection.close();
}

#[test]
fn lost_ack_survives_restart_and_recovers_with_one_native_effect() {
    let fixture = Fixture::new(json!({"dropAckOnce":true,"stream":false}));
    let state_path = fixture.directory.join("rust-state.json");
    let state = State::open(&state_path).unwrap();
    let connection = fixture.connect();
    let operation_id = new_id();
    let record = json!({"id":operation_id,"op":"send","environmentId":"test-native","threadId":"thread-review","message":"One exact human message 🦀","mode":"auto"});
    state
        .set_draft("test-native", "thread-review", "One exact human message 🦀")
        .unwrap();
    state
        .save_pending("test-native", false, record.clone())
        .unwrap();
    let error = connection
        .request_with_timeout(
            "send",
            record.clone(),
            Some(&operation_id),
            Duration::from_millis(600),
        )
        .unwrap_err();
    assert!(error.uncertain);
    connection.close();
    drop(connection);
    drop(state);
    let reopened = State::open(&state_path).unwrap();
    assert_eq!(reopened.pending("test-native", false).unwrap(), record);
    let second = fixture.connect();
    let accepted = second
        .request("send", record.clone(), Some(&operation_id))
        .unwrap();
    assert!(accepted["sequence"].is_u64());
    reopened
        .acknowledge(
            "test-native",
            false,
            &operation_id,
            "thread-review",
            "One exact human message 🦀",
        )
        .unwrap();
    assert!(reopened.pending("test-native", false).is_none());
    assert_eq!(reopened.draft("test-native", "thread-review"), "");
    second.close();
    drop(second);
    let native: Value =
        serde_json::from_slice(&fs::read(fixture.directory.join("native-state.json")).unwrap())
            .unwrap();
    assert_eq!(
        native["effects"], 1,
        "same-ID recovery must not duplicate native execution"
    );
}

#[test]
fn unavailable_host_does_not_poison_independent_healthy_transport() {
    let healthy = Fixture::new(json!({"stream":false}));
    let unavailable = Fixture::new(json!({"unavailable":true}));
    let failed = Connection::start(unavailable.environment.clone()).unwrap();
    assert!(failed.hello().is_err());
    let connection = healthy.connect();
    let page = connection.request("list", json!({}), None).unwrap();
    assert!(!page["threads"].as_array().unwrap().is_empty());
    failed.close();
    connection.close();
}
