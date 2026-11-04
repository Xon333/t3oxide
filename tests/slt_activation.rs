//! Regression boundary for the maintained SLT activation policy, independent of T3.
use slt::{EventBuilder, KeyCode, KeyModifiers, RunConfig, TestBackend, TextareaState};

#[test]
fn opt_in_keeps_paste_submit_release_and_tail_in_order() {
    let mut backend = TestBackend::new(40, 8)
        .with_run_config(RunConfig::default().activation_boundaries(true).max_fps(60));
    let mut editor = TextareaState::default();
    let mut sent = Vec::new();
    let mut draw = |ctx: &mut slt::Context| {
        if ctx.key_mod(']', KeyModifiers::CONTROL) {
            sent.push(editor.value());
            editor.set_value("");
        }
        ctx.textarea(&mut editor, 3);
    };
    backend.render(&mut draw);
    backend.run_with_events(
        EventBuilder::new()
            .paste("first\npasted")
            .key_with(KeyCode::Char(']'), KeyModifiers::CONTROL)
            .key_release(']')
            .paste("second")
            .build(),
        &mut draw,
    );
    let mut frames = 1;
    while backend.has_pending_input() {
        assert!(frames < 6, "tail must make progress without new input");
        backend.render(&mut draw);
        frames += 1;
    }
    assert_eq!(sent, ["first\npasted"]);
    assert_eq!(editor.value(), "second");
    assert_eq!(frames, 4, "edits, activation, settled geometry, tail");
}

#[test]
fn default_batching_and_opt_in_tab_navigation_remain_available() {
    let mut backend = TestBackend::new(40, 8);
    let mut keys = Vec::new();
    backend.run_with_events(
        EventBuilder::new()
            .key('a')
            .key_code(KeyCode::Enter)
            .key('b')
            .build(),
        |ctx| {
            keys = ctx
                .key_presses_when(true)
                .map(|(_, key)| key.code.clone())
                .collect();
        },
    );
    assert_eq!(
        keys,
        [KeyCode::Char('a'), KeyCode::Enter, KeyCode::Char('b')]
    );
    assert!(
        !backend.has_pending_input(),
        "default policy remains upstream batching"
    );

    let mut backend =
        TestBackend::new(40, 8).with_run_config(RunConfig::default().activation_boundaries(true));
    let mut first = TextareaState::default();
    let mut second = TextareaState::default();
    let mut draw = |ctx: &mut slt::Context| {
        ctx.textarea(&mut first, 2);
        ctx.textarea(&mut second, 2);
    };
    backend.render(&mut draw);
    backend.run_with_events(
        EventBuilder::new()
            .paste("first")
            .key_code(KeyCode::Tab)
            .paste("second")
            .key_code(KeyCode::BackTab)
            .paste("third")
            .build(),
        &mut draw,
    );
    for _ in 0..3 {
        backend.render(&mut draw);
    }
    assert!(!backend.has_pending_input());
    assert_eq!(first.value(), "firstthird");
    assert_eq!(second.value(), "second");
}

#[test]
fn activation_policy_preserves_click_on_release() {
    let mut backend = TestBackend::new(40, 8).with_run_config(
        RunConfig::default()
            .activation_boundaries(true)
            .click_on_release(true),
    );
    let mut clicks = 0;
    let mut draw = |ctx: &mut slt::Context| {
        if ctx.button("Submit").clicked {
            clicks += 1;
        }
    };
    backend.render(&mut draw);
    backend.render(&mut draw);
    let (x, y) = backend.find_text("Submit").unwrap();
    backend.run_with_events(EventBuilder::new().click(x, y).build(), &mut draw);
    backend.render(&mut draw);
    assert_eq!(clicks, 0, "press cannot activate release-mode controls");
    let mut draw = |ctx: &mut slt::Context| {
        if ctx.button("Submit").clicked {
            clicks += 1;
        }
    };
    backend.run_with_events(EventBuilder::new().mouse_up(x, y).build(), &mut draw);
    backend.render(&mut draw);
    assert_eq!(clicks, 1);
}
