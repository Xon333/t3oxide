use super::*;
use sha2::Digest;
use std::{
    fs,
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
};

struct Fixture {
    directory: PathBuf,
    app: App,
}
impl Fixture {
    fn new() -> Self {
        let directory =
            std::env::temp_dir().join(format!("t3oxide-app-recovery-test-{}", wire::new_id()));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let node = std::env::split_paths(&std::env::var_os("PATH").unwrap_or_default())
            .map(|p| p.join("node"))
            .find(|p| p.is_file())
            .unwrap();
        let token = directory.join("token");
        fs::write(&token, "test-token").unwrap();
        fs::set_permissions(&token, fs::Permissions::from_mode(0o600)).unwrap();
        let config = directory.join("host.json");
        fs::write(&config,json!({"baseUrl":"http://127.0.0.1:1","environmentId":"native-test","tokenFile":token,"allowWrites":true,"fixture":{"stream":false,"stateFile":directory.join("native-state.json")}}).to_string()).unwrap();
        fs::set_permissions(&config, fs::Permissions::from_mode(0o600)).unwrap();
        let environment = Environment {
            id: "profile-test".into(),
            label: "Fixture".into(),
            environment_id: "native-test".into(),
            ssh_destination: None,
            node: node.to_string_lossy().into(),
            adapter: Path::new(env!("CARGO_MANIFEST_DIR"))
                .join("tests/rust_fixture.mjs")
                .to_string_lossy()
                .into(),
            host_config: config.to_string_lossy().into(),
            enabled: true,
            default_for_new: true,
            web_url: None,
            allowed_provider_instance_ids: None,
        };
        let state = Arc::new(State::open(&directory.join("rust-state.json")).unwrap());
        let app = App::new(Args::default(), vec![environment], state).unwrap();
        Self { directory, app }
    }
    fn wait_mutation(&mut self) -> bool {
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            let event = self
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("mutation completed");
            if let Background::Mutation { result, .. } = &event {
                let success = result.is_ok();
                self.app.handle(event).unwrap();
                return success;
            }
            self.app.handle(event).unwrap();
        }
    }
    fn until(&mut self, ready: impl Fn(&App) -> bool) {
        let deadline = Instant::now() + Duration::from_secs(5);
        while !ready(&self.app) {
            let event = self
                .app
                .rx
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("fixture background action completed");
            self.app.handle(event).unwrap();
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
        let _ = fs::remove_dir_all(&self.directory);
    }
}

#[test]
fn recovery_never_retargets_or_changes_original_payload() {
    let mut fixture = Fixture::new();
    let record = json!({"id":"saved","op":"send","environmentId":"removed-environment","threadId":"thread-review","message":"exact","mode":"auto"});
    fixture
        .app
        .state
        .save_pending("removed-environment", false, record.clone())
        .unwrap();
    assert!(
        fixture
            .app
            .dispatch(Intent::Recover(record.clone(), false))
            .is_err()
    );
    assert_eq!(
        fixture.app.state.pending("removed-environment", false),
        Some(record)
    );
    let original = json!({"id":"local","op":"send","environmentId":"native-test","threadId":"thread-review","message":"exact","mode":"auto"});
    fixture
        .app
        .state
        .save_pending("native-test", false, original.clone())
        .unwrap();
    let mut changed = original.clone();
    changed["message"] = json!("changed");
    assert!(
        fixture
            .app
            .dispatch(Intent::Recover(changed, false))
            .is_err()
    );
    assert_eq!(
        fixture.app.state.pending("native-test", false),
        Some(original)
    );
}

#[test]
fn local_resolution_requires_opening_exact_record_and_explicit_choice() {
    let mut fixture = Fixture::new();
    let record = json!({"id":"saved","op":"send","environmentId":"native-test","threadId":"thread-review","message":"exact"});
    fixture
        .app
        .state
        .save_pending("native-test", false, record.clone())
        .unwrap();
    fixture.app.command("recover").unwrap();
    let choice = fixture.app.dialog.as_ref().unwrap().choices[0]
        .intent
        .clone();
    assert!(matches!(choice, Intent::ReviewRecovery(..)));
    fixture.app.dispatch(choice).unwrap();
    assert!(
        fixture
            .app
            .dialog
            .as_ref()
            .unwrap()
            .detail
            .contains("exact")
    );
    assert!(matches!(
        fixture.app.dialog.as_ref().unwrap().choices[0].intent,
        Intent::Close
    ));
    assert_eq!(
        fixture.app.state.pending("native-test", false),
        Some(record.clone())
    );
    fixture.app.dispatch(Intent::Close).unwrap();
    assert_eq!(
        fixture.app.state.pending("native-test", false),
        Some(record)
    );
}

#[test]
fn legacy_stop_recovers_in_its_original_normal_lane() {
    let mut fixture = Fixture::new();
    let record = json!({"id":"legacy-stop","op":"stop","environmentId":"native-test","threadId":"thread-a","runId":"run-thread-a"});
    fixture
        .app
        .state
        .update(|v| {
            v["pending"]["native-test"] = record.clone();
            Ok(())
        })
        .unwrap();
    fixture
        .app
        .dispatch(Intent::Recover(record, false))
        .unwrap();
    assert!(fixture.wait_mutation());
    assert!(fixture.app.state.pending("native-test", false).is_none());
    assert!(fixture.app.state.pending("native-test", true).is_none());
}

#[test]
fn negative_retry_keeps_saved_operation_and_draft() {
    let mut fixture = Fixture::new();
    let record = json!({"id":"old-answer","op":"answer","environmentId":"native-test","threadId":"thread-approval","requestId":"request-no-longer-present","revision":"outdated","decision":"accept"});
    fixture
        .app
        .state
        .save_pending("native-test", false, record.clone())
        .unwrap();
    fixture
        .app
        .state
        .set_draft("native-test", "thread-approval", "new draft")
        .unwrap();
    fixture
        .app
        .dispatch(Intent::Recover(record.clone(), false))
        .unwrap();
    assert!(!fixture.wait_mutation());
    assert_eq!(
        fixture.app.state.pending("native-test", false),
        Some(record)
    );
    assert_eq!(
        fixture.app.state.draft("native-test", "thread-approval"),
        "new draft"
    );
}

#[test]
fn launch_acknowledgement_is_bound_to_original_generated_identity() {
    assert!(
        validate_ack(
            "launch",
            &json!({"id":"original-operation"}),
            &json!({"threadId":"different-thread"})
        )
        .is_err()
    );
    assert!(
        validate_ack(
            "launch",
            &json!({"id":"original-operation"}),
            &json!({"threadId":"original-operation"})
        )
        .is_ok()
    );
    assert!(
        validate_ack(
            "send",
            &json!({}),
            &json!({"sequence":9_007_199_254_740_992u64})
        )
        .is_err()
    );
    assert!(validate_ack("send", &json!({}), &json!({"sequence":2.5})).is_err());
}

#[test]
fn retained_task_lists_are_read_only_and_keep_original_recovery() {
    let mut fixture = Fixture::new();
    let source = "\0file-many/project-a";
    let text = "first task\nsecond task\n";
    let pending = json!({"id":"original-batch-operation","op":"launch","environmentId":"native-test","threadId":"original-thread"});
    fixture
        .app
        .state
        .set_draft("native-test", source, text)
        .unwrap();
    fixture
        .app
        .state
        .save_pending("native-test", false, pending.clone())
        .unwrap();
    fixture.app.state.update(|state| {
        state["batchReceipts"]["original-receipt"] = json!({
            "id":"original-receipt", "environmentId":"native-test", "projectId":"project-a",
            "source":source, "original":text, "createdAt":"2026-11-14T00:00:00Z",
            "items":[{"start":0,"end":11,"state":"Remaining draft"},{"start":11,"end":text.len(),"state":"Remaining draft"}]
        });
        Ok(())
    }).unwrap();
    let before = fixture.app.state.snapshot();
    assert!(fixture.app.command("batch").is_err());
    fixture.app.command("batch-receipts").unwrap();
    fixture
        .app
        .dispatch(Intent::Workspace(workspace::WorkspaceAction::BatchReceipt(
            "original-receipt".into(),
        )))
        .unwrap();
    let view = fixture
        .app
        .dialog
        .as_ref()
        .unwrap()
        .choices
        .iter()
        .find(|choice| choice.label == "View remaining draft")
        .unwrap()
        .intent
        .clone();
    fixture.app.dispatch(view).unwrap();
    assert!(fixture.app.dialog.as_ref().unwrap().detail.contains(text));
    fixture
        .app
        .dispatch(Intent::Workspace(workspace::WorkspaceAction::SavedBatch {
            key: format!("native-test/{source}"),
            text: text.into(),
        }))
        .unwrap();
    assert!(fixture.app.dialog.as_ref().unwrap().detail.contains(text));
    assert!(fixture.app.launch.is_none());
    assert_eq!(fixture.app.state.snapshot(), before);
    assert_eq!(
        fixture.app.state.pending("native-test", false),
        Some(pending)
    );
    assert!(
        !fixture.directory.join("native-state.json").exists(),
        "reading a retained list cannot dispatch"
    );
}

#[test]
fn imported_launch_is_accessible_and_project_forms_do_not_overwrite_each_other() {
    let mut fixture = Fixture::new();
    let key = "native-test/project-launch/project-a";
    let imported = json!({"title":"Imported human title","message":"exact original brief","projectId":"project-a"});
    fixture
        .app
        .state
        .update(|v| {
            v["launchDrafts"][key] = imported.clone();
            Ok(())
        })
        .unwrap();
    fixture.app.launch = Some(Launch {
        profile: 0,
        form_key: fixture
            .app
            .launch_form_key(0, &json!({"id":"project-empty"})),
        project: json!({"id":"project-empty"}),
        defaults: Value::Null,
        title: "Other project draft".into(),
        message: "keep me".into(),
        selection: Value::Null,
        workspace: None,
        batch: None,
    });
    fixture.app.save_launch().unwrap();
    fixture
        .app
        .dispatch(Intent::Workspace(workspace::WorkspaceAction::SavedLaunch {
            key: key.into(),
            draft: imported,
        }))
        .unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    assert_eq!(
        fixture.app.launch.as_ref().unwrap().message,
        "exact original brief"
    );
    assert_eq!(
        fixture.app.state.snapshot()["rustLaunchForms"]
            .as_object()
            .unwrap()
            .len(),
        2
    );
    fixture.app.create_launch().unwrap();
    assert!(fixture.wait_mutation());
    let state = fixture.app.state.snapshot();
    assert!(state["launchDrafts"].get(key).is_none());
    let remaining = state["rustLaunchForms"].as_object().unwrap();
    assert_eq!(remaining.len(), 1);
    assert_eq!(remaining.values().next().unwrap()["message"], "keep me");
}

#[test]
fn inactive_model_favorites_cannot_mutate_preserved_selections() {
    let mut fixture = Fixture::new();
    fixture.app.open(0, "thread-review".into()).unwrap();
    fixture.until(|app| {
        app.active
            .as_ref()
            .is_some_and(|a| !a.navigating && a.projection.snapshot().is_some())
    });
    fixture.app.state.update(|v| {
        v["modelPresets"]["native-test"] = json!([{"name":"Focused","selection":{"instanceId":"fixture-provider","model":"fixture-model"}}]);
        Ok(())
    }).unwrap();
    let before = fixture.app.state.snapshot();
    fixture.app.presets().unwrap();
    let old_choice = fixture.app.dialog.as_ref().unwrap().choices[0]
        .intent
        .clone();
    assert!(fixture.app.dispatch(old_choice).is_err());
    assert!(
        fixture
            .app
            .dispatch(Intent::Workspace(
                workspace::WorkspaceAction::RemovePreset {
                    environment: "native-test".into(),
                    name: "Focused".into(),
                    selection: json!({"instanceId":"fixture-provider","model":"fixture-model"}),
                }
            ))
            .is_err()
    );
    assert!(fixture.app.command("presets").is_err());
    assert_eq!(fixture.app.state.snapshot(), before);

    for command in ["model", "model next"] {
        fixture.app.command(command).unwrap();
        fixture.until(|app| {
            app.dialog
                .as_ref()
                .is_some_and(|d| d.title.starts_with("Model ·"))
        });
        let choices = &fixture.app.dialog.as_ref().unwrap().choices;
        assert!(
            !choices
                .iter()
                .any(|c| c.label.to_lowercase().contains("favorite"))
        );
        assert!(
            choices
                .iter()
                .any(|c| matches!(c.intent, Intent::ModelApply))
        );
        fixture.app.dialog = None;
    }
    fixture.app.command("model next").unwrap();
    fixture.until(|app| {
        app.dialog
            .as_ref()
            .is_some_and(|d| d.title.starts_with("Model ·"))
    });
    fixture.app.dispatch(Intent::Model(1)).unwrap();
    fixture.app.dispatch(Intent::ModelApply).unwrap();
    assert_eq!(
        fixture.app.state.snapshot()["modelOverrides"]["native-test/thread-review"]["selection"]["model"],
        "fixture-fast"
    );
    assert_eq!(
        fixture.app.state.snapshot()["modelPresets"],
        before["modelPresets"]
    );

    fixture
        .app
        .choose_project_result(0, json!({"id":"project-a"}), NewPurpose::Launch)
        .unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    fixture.app.launch_models().unwrap();
    let choices = &fixture.app.dialog.as_ref().unwrap().choices;
    assert!(
        !choices
            .iter()
            .any(|c| c.label.to_lowercase().contains("favorite"))
    );
    fixture.app.dispatch(Intent::Model(1)).unwrap();
    fixture.app.dispatch(Intent::ModelApply).unwrap();
    assert_eq!(
        fixture.app.launch.as_ref().unwrap().selection["model"],
        "fixture-fast"
    );
    assert_eq!(
        fixture.app.state.snapshot()["modelPresets"],
        before["modelPresets"]
    );
}

#[test]
fn historical_batch_form_preserves_identity_without_blocking_native_new() {
    let mut fixture = Fixture::new();
    let project = json!({"id":"project-a"});
    let canonical = fixture.app.launch_form_key(0, &project);
    let historical = json!({"projectId":"project-a", "title":"Historical batch", "message":"Exact old task\n日本語", "selection":null, "workspace":null, "batchBinding":{"original":"opaque retained binding"}});
    fixture
        .app
        .state
        .update(|v| {
            v["rustLaunchForms"][&canonical] = historical.clone();
            Ok(())
        })
        .unwrap();
    let old_operation = json!({"id":"old-unknown-launch", "op":"launch-project", "environmentId":"native-test", "threadId":"old-source", "projectId":"project-a", "rustLaunchFormKey":canonical, "rustLaunchFormDigest":format!("{:x}", sha2::Sha256::digest(historical.to_string().as_bytes()))});
    fixture
        .app
        .state
        .save_pending("native-test", false, old_operation.clone())
        .unwrap();
    fixture
        .app
        .dispatch(Intent::Workspace(
            workspace::WorkspaceAction::RestoreRustLaunch {
                key: canonical.clone(),
                draft: historical.clone(),
            },
        ))
        .unwrap();
    assert!(fixture.app.launch.is_none());
    assert!(
        fixture
            .app
            .dialog
            .as_ref()
            .unwrap()
            .detail
            .contains("Exact old task")
    );
    fixture
        .app
        .choose_project_result(0, project.clone(), NewPurpose::Launch)
        .unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    let native_key = fixture.app.launch.as_ref().unwrap().form_key.clone();
    assert_ne!(native_key, canonical);
    assert_eq!(
        serde_json::from_str::<Vec<String>>(&native_key).unwrap()[3],
        "native-conversation"
    );
    let launch = fixture.app.launch.as_mut().unwrap();
    launch.title = "Ordinary native New".into();
    launch.message = "New human brief\n café".into();
    assert!(launch.batch.is_none());
    fixture.app.save_launch().unwrap();
    let native_form = fixture.app.state.snapshot()["rustLaunchForms"][&native_key].clone();
    assert_eq!(
        fixture.app.state.snapshot()["rustLaunchForms"][&canonical],
        historical
    );
    assert_eq!(
        fixture.app.state.pending("native-test", false),
        Some(old_operation)
    );
    assert!(
        !fixture.directory.join("native-state.json").exists(),
        "opening New only reads defaults"
    );

    // A late acknowledgement may retire only its bound historical form.
    fixture
        .app
        .state
        .acknowledge("native-test", false, "old-unknown-launch", "old-source", "")
        .unwrap();
    assert!(
        fixture.app.state.snapshot()["rustLaunchForms"]
            .get(&canonical)
            .is_none()
    );
    assert_eq!(
        fixture.app.state.snapshot()["rustLaunchForms"][&native_key],
        native_form
    );
    assert_eq!(fixture.app.launch_form_key(0, &project), native_key);
    let mut fields = json!({});
    fixture
        .app
        .bind_launch_submission(fixture.app.launch.as_ref().unwrap(), &mut fields)
        .unwrap();
    assert_eq!(fields["rustLaunchFormKey"], native_key);
    fixture.app.launch = None;
    fixture
        .app
        .dispatch(Intent::Workspace(
            workspace::WorkspaceAction::RestoreRustLaunch {
                key: native_key.clone(),
                draft: native_form,
            },
        ))
        .unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    assert_eq!(
        fixture.app.launch.as_ref().unwrap().message,
        "New human brief\n café"
    );
    fixture.app.create_launch().unwrap();
    assert!(fixture.wait_mutation());
    assert!(
        fixture.app.state.snapshot()["rustLaunchForms"]
            .get(&native_key)
            .is_none()
    );
}

#[test]
fn saved_launch_restoration_uses_the_exact_selected_form_slot() {
    let mut fixture = Fixture::new();
    let canonical = fixture.app.launch_form_key(0, &json!({"id":"project-a"}));
    let native = json!([
        "profile-test",
        "native-test",
        "project-a",
        "native-conversation"
    ])
    .to_string();
    let original = json!({"projectId":"project-a", "title":"Original", "message":"Canonical draft", "selection":null, "workspace":null});
    let alternative = json!({"projectId":"project-a", "title":"New", "message":"Native draft", "selection":null, "workspace":null});
    fixture
        .app
        .state
        .update(|v| {
            v["rustLaunchForms"][&canonical] = original.clone();
            v["rustLaunchForms"][&native] = alternative.clone();
            Ok(())
        })
        .unwrap();
    fixture
        .app
        .choose_project_result(0, json!({"id":"project-a"}), NewPurpose::Launch)
        .unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    assert_eq!(fixture.app.launch.as_ref().unwrap().message, "Native draft");
    fixture
        .app
        .dispatch(Intent::Workspace(
            workspace::WorkspaceAction::RestoreRustLaunch {
                key: canonical.clone(),
                draft: original,
            },
        ))
        .unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    assert_eq!(fixture.app.launch.as_ref().unwrap().form_key, canonical);
    assert_eq!(
        fixture.app.launch.as_ref().unwrap().message,
        "Canonical draft"
    );
    fixture.app.launch.as_mut().unwrap().message = "Edited canonical".into();
    fixture.app.save_launch().unwrap();
    let saved = fixture.app.state.snapshot();
    assert_eq!(
        saved["rustLaunchForms"][&canonical]["message"],
        "Edited canonical"
    );
    assert_eq!(saved["rustLaunchForms"][&native]["message"], "Native draft");
    assert!(fixture.app.state.pending("native-test", false).is_none());
}

#[test]
fn native_review_restores_only_matching_source_hashes() {
    let mut fixture = Fixture::new();
    fixture.app.open(0, "thread-review".into()).unwrap();
    fixture.until(|app| {
        app.active
            .as_ref()
            .is_some_and(|a| !a.navigating && a.projection.snapshot().is_some())
    });
    fixture.app.command("review").unwrap();
    fixture.until(|app| {
        app.dialog
            .as_ref()
            .is_some_and(|d| d.title == "Native changes and pull requests")
    });
    fixture.app.dialog.as_mut().unwrap().scroll = 3;
    fixture.app.preserve_workspace_position().unwrap();
    fixture.app.dialog = None;
    assert_eq!(
        fixture.app.state.snapshot()["reviewPositions"][0]["offset"],
        3
    );
    fixture.app.command("review").unwrap();
    fixture.until(|app| {
        app.dialog
            .as_ref()
            .is_some_and(|d| d.title == "Native changes and pull requests")
    });
    assert_eq!(fixture.app.dialog.as_ref().unwrap().scroll, 3);
    fixture
        .app
        .state
        .update(|v| {
            v["reviewPositions"][0]["digest"] = json!("0".repeat(64));
            Ok(())
        })
        .unwrap();
    fixture.app.dialog = None;
    fixture.app.command("review").unwrap();
    fixture.until(|app| {
        app.dialog
            .as_ref()
            .is_some_and(|d| d.title == "Native changes and pull requests")
    });
    assert_eq!(fixture.app.dialog.as_ref().unwrap().scroll, 0);
}

#[test]
fn saved_launch_discard_is_explicit_and_cannot_erase_a_newer_edit() {
    let mut fixture = Fixture::new();
    fixture.app.launch = Some(Launch {
        profile: 0,
        form_key: fixture.app.launch_form_key(0, &json!({"id":"project-a"})),
        project: json!({"id":"project-a"}),
        defaults: Value::Null,
        title: "Human form".into(),
        message: "old".into(),
        selection: Value::Null,
        workspace: None,
        batch: None,
    });
    fixture.app.save_launch().unwrap();
    let key = fixture.app.launch_form_key(0, &json!({"id":"project-a"}));
    let draft = fixture.app.state.snapshot()["rustLaunchForms"][&key].clone();
    fixture
        .app
        .dispatch(Intent::Workspace(workspace::WorkspaceAction::RustLaunch {
            key: key.clone(),
            draft,
        }))
        .unwrap();
    assert!(matches!(
        fixture.app.dialog.as_ref().unwrap().choices[0].intent,
        Intent::Close
    ));
    let discard = fixture
        .app
        .dialog
        .as_ref()
        .unwrap()
        .choices
        .iter()
        .find(|choice| choice.label == "Discard this local form")
        .unwrap()
        .intent
        .clone();
    fixture.app.launch.as_mut().unwrap().message = "newer".into();
    fixture.app.save_launch().unwrap();
    assert!(fixture.app.dispatch(discard).is_err());
    assert_eq!(
        fixture.app.state.snapshot()["rustLaunchForms"][key]["message"],
        "newer"
    );
}

#[test]
fn successful_same_id_recovery_preserves_identical_new_draft_with_or_without_cache() {
    for active_source in [true, false] {
        let mut fixture = Fixture::new();
        let message = "identical human brief";
        let record = json!({
            "id": "accepted-before-client-crash", "op": "send",
            "environmentId": "native-test", "threadId": "thread-review",
            "message": message, "mode": "auto"
        });
        fixture
            .app
            .state
            .set_draft("native-test", "thread-review", message)
            .unwrap();
        fixture
            .app
            .state
            .save_pending("native-test", false, record.clone())
            .unwrap();

        // Native acceptance is real. Leave the local journal unresolved, as if
        // the client exited between the acknowledgement and its local commit.
        let connection = Connection::start(fixture.app.environments[0].clone()).unwrap();
        connection.hello().unwrap();
        connection
            .request("send", record.clone(), Some("accepted-before-client-crash"))
            .unwrap();
        connection.close();

        if active_source {
            fixture.app.open(0, "thread-review".into()).unwrap();
            fixture.until(|app| {
                app.active.as_ref().is_some_and(|active| {
                    !active.navigating && active.projection.snapshot().is_some()
                })
            });
            fixture.app.replace_text(message.into());
            fixture.app.flush_draft().unwrap();
        } else {
            // The newer human draft can belong to a source that has not been
            // opened in this process, so the recovery worker has no cache entry.
            assert!(fixture.app.draft_cache.lock().unwrap().is_empty());
            fixture
                .app
                .state
                .set_draft("native-test", "thread-review", message)
                .unwrap();
        }

        fixture
            .app
            .dispatch(Intent::Recover(record, false))
            .unwrap();
        assert!(
            fixture.wait_mutation(),
            "recovery must receive a successful native ACK"
        );
        assert!(fixture.app.state.pending("native-test", false).is_none());
        assert_eq!(
            fixture.app.state.draft("native-test", "thread-review"),
            message
        );
        if active_source {
            assert_eq!(fixture.app.text(), message);
        }
        let native: Value =
            serde_json::from_slice(&fs::read(fixture.directory.join("native-state.json")).unwrap())
                .unwrap();
        assert_eq!(
            native["effects"], 1,
            "same-ID reconciliation must not send twice"
        );
        let persisted: Value =
            serde_json::from_slice(&fs::read(fixture.app.state.path()).unwrap()).unwrap();
        assert_eq!(persisted["drafts"]["native-test/thread-review"], message);
        assert!(persisted["pending"].get("native-test").is_none());
    }
}
