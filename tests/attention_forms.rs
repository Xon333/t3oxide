use serde_json::{Value, json};
use t3oxide::attention::{Form, acknowledged, project_queue};
use t3oxide::domain::Thread;
use t3oxide::state::State;

fn request() -> Value {
    json!({"id":"request-a","revision":"a".repeat(64),"kind":"user_input_request","supported":true,
        "questions":[{"id":"release_scope","header":"Release scope","question":"Which release?","required":true,
            "options":[{"label":"Stable","value":"stable"},{"label":"Next","value":"next"}]}]})
}

#[test]
fn only_native_human_requests_enter_project_queue() {
    let rows: Vec<_> = [
        ("question", "user_input", "request-question", "failed"),
        ("approval", "approval", "request-approval", "running"),
        ("auth", "auth_refresh", "request-auth", "waiting"),
        ("failed", "", "", "failed"),
    ]
    .into_iter()
    .map(|(id, kind, request, status)| {
        Thread::parse(
            0,
            json!({
        "id":id,"project":"Example","projectId":"project-a","status":status,
        "inbox":{"pendingRequestId":request,"pendingRequestType":kind}}),
        )
        .unwrap()
    })
    .collect();
    let queue = project_queue(&rows);
    assert_eq!(queue.len(), 1);
    assert_eq!(queue[0].project_id, "project-a");
    assert_eq!(
        queue[0]
            .items
            .iter()
            .map(|item| item.thread_id.as_str())
            .collect::<Vec<_>>(),
        vec!["question", "approval"]
    );
    let mut other_environment = rows[0].clone();
    other_environment.profile = 1;
    let both = project_queue(&[rows[0].clone(), other_environment]);
    assert_eq!(
        both.len(),
        2,
        "same-looking project names across environments retain their identity"
    );
}

#[test]
fn answer_drafts_survive_cancel_restart_and_changed_source_without_rebinding() {
    let temporary = tempfile::tempdir().unwrap();
    let path = temporary.path().join("state.json");
    let state = State::open(&path).unwrap();
    let original = Form::from_request("environment-a", "thread-a", &request()).unwrap();
    let answers = json!({"release_scope":"stable"})
        .as_object()
        .unwrap()
        .clone();
    original.save_draft(&state, &answers).unwrap();
    drop(state); // Closing/cancelling the form never invokes a native answer.
    let state = State::open(&path).unwrap();
    assert_eq!(original.load_draft(&state).unwrap(), Some(answers.clone()));
    let mut changed = request();
    changed["revision"] = json!("b".repeat(64));
    changed["questions"][0]["question"] = json!("Changed wording");
    let replacement = Form::from_request("environment-a", "thread-a", &changed).unwrap();
    assert_eq!(replacement.load_draft(&state).unwrap(), None);
    assert!(
        original
            .ensure_current("environment-a", "thread-a", &[changed])
            .is_err()
    );
    assert!(
        original
            .ensure_current("environment-a", "thread-a", &[])
            .is_err()
    );
    assert_eq!(original.load_draft(&state).unwrap(), Some(answers));
    let empty = json!({"release_scope":""}).as_object().unwrap().clone();
    replacement.save_draft(&state, &empty).unwrap();
    assert!(replacement.validate_answers(&empty).is_err());
    assert_eq!(
        state.snapshot()["attentionDrafts"]
            .as_object()
            .unwrap()
            .len(),
        2
    );
    let mut custom = request();
    custom["revision"] = json!("c".repeat(64));
    custom["questions"][0]["allowCustomAnswer"] = json!(true);
    let custom = Form::from_request("environment-a", "thread-a", &custom).unwrap();
    let partial = json!({"release_scope":"Unfinished custom answer\n🙂  "})
        .as_object()
        .unwrap()
        .clone();
    custom.save_draft(&state, &partial).unwrap();
    assert_eq!(
        custom.load_draft(&state).unwrap(),
        Some(partial),
        "unfinished text preserves whitespace and Unicode"
    );
}

#[test]
fn field_and_choice_identities_survive_reordering_and_validate_native_values() {
    let form = Form::from_request("environment-a", "thread-a", &request()).unwrap();
    let mut reordered = request();
    reordered["questions"][0]["options"]
        .as_array_mut()
        .unwrap()
        .reverse();
    reordered["questions"][0]["options"][1]["label"] = json!("Stable release");
    let reordered = Form::from_request("environment-a", "thread-a", &reordered).unwrap();
    assert_eq!(form.fields[0].id, reordered.fields[0].id);
    assert_eq!(
        form.fields[0].choices[0].id,
        reordered.fields[0].choices[1].id
    );
    let invalid = json!({"release_scope":"unknown"})
        .as_object()
        .unwrap()
        .clone();
    assert!(form.answer_fields(&invalid).is_err());
    let valid = json!({"release_scope":"stable"})
        .as_object()
        .unwrap()
        .clone();
    assert_eq!(
        form.answer_fields(&valid).unwrap()["requestId"],
        "request-a"
    );
    let mut duplicate = request();
    duplicate["questions"][0]["options"][1]["value"] = json!("stable");
    assert!(Form::from_request("environment-a", "thread-a", &duplicate).is_err());
    let mut unsupported = request();
    unsupported["questions"][0]["options"][0]["value"] = json!(42);
    assert!(Form::from_request("environment-a", "thread-a", &unsupported).is_err());
    unsupported["questions"][0]["options"] = json!("unsupported controls");
    assert!(Form::from_request("environment-a", "thread-a", &unsupported).is_err());
}

#[test]
fn local_widget_completion_and_mismatched_receipts_never_count_as_delivery() {
    let form = Form::from_request("environment-a", "thread-a", &request()).unwrap();
    assert!(!acknowledged(
        &form.source,
        "operation-a",
        &json!({"submitted":true})
    ));
    assert!(!acknowledged(
        &form.source,
        "operation-a",
        &json!({"sequence":12})
    ));
    let accepted = json!({"sequence":12,"acknowledgement":{"environmentId":"environment-a","threadId":"thread-a",
        "requestId":"request-a","revision":"a".repeat(64),"operationId":"operation-a"}});
    assert!(acknowledged(&form.source, "operation-a", &accepted));
    for field in [
        "environmentId",
        "threadId",
        "requestId",
        "revision",
        "operationId",
    ] {
        let mut wrong = accepted.clone();
        wrong["acknowledgement"][field] = json!("other");
        assert!(
            !acknowledged(&form.source, "operation-a", &wrong),
            "mismatched {field}"
        );
    }
}
