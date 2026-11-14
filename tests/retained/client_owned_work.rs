// Historical regressions for the removed client-owned context and batch paths.
// Retained as source evidence; these require their former app test fixtures and
// are intentionally outside Cargo test target discovery.

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
fn batch_receipts_advance_exact_items_and_require_each_native_launch_review() {
    let mut fixture = Fixture::new();
    fixture
        .app
        .choose_project_result(
            0,
            json!({"id":"project-a","title":"Fixture project"}),
            NewPurpose::Batch,
        )
        .unwrap();
    fixture.app.modal_editor = ui::editor("\nfirst task\n\nsecond task\n", &fixture.app.theme, "");
    fixture.app.submit_text(InputTarget::Batch).unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    let saved = fixture.app.state.snapshot();
    let receipts = saved["batchReceipts"].as_object().unwrap();
    assert_eq!(receipts.len(), 1);
    let receipt_id = receipts.keys().next().unwrap().clone();
    fixture.app.create_launch().unwrap();
    assert!(fixture.wait_mutation());
    let saved = fixture.app.state.snapshot();
    let receipt = &saved["batchReceipts"][&receipt_id];
    assert_eq!(receipt["items"][0]["state"], "Acknowledged");
    assert_eq!(receipt["items"][1]["state"], "Remaining draft");
    assert!(receipt["items"][0]["operationId"].is_string());
    assert_eq!(
        fixture
            .app
            .state
            .draft("native-test", "\0file-many/project-a"),
        "\nsecond task\n"
    );
    fixture
        .app
        .dispatch(Intent::Workspace(workspace::WorkspaceAction::ResumeBatch(
            receipt_id.clone(),
        )))
        .unwrap();
    assert_eq!(
        fixture.app.modal_editor.lines().join("\n"),
        "\nsecond task\n"
    );
    fixture.app.submit_text(InputTarget::Batch).unwrap();
    fixture.until(|app| {
        app.launch
            .as_ref()
            .is_some_and(|l| l.defaults["revision"].is_string())
    });
    assert!(fixture.app.state.pending("native-test", false).is_none());
    assert_eq!(
        fixture.app.state.snapshot()["batchReceipts"]
            .as_object()
            .unwrap()
            .len(),
        1
    );
    fixture.app.create_launch().unwrap();
    assert!(fixture.wait_mutation());
    assert!(
        fixture
            .app
            .state
            .draft("native-test", "\0file-many/project-a")
            .is_empty()
    );
    assert!(
        array(
            &fixture.app.state.snapshot()["batchReceipts"][&receipt_id],
            "items"
        )
        .iter()
        .all(|item| item["state"] == "Acknowledged")
    );
}
