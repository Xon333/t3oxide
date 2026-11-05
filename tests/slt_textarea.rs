//! Word wrapping keeps the same source coordinates for editing and selection.
use slt::{EventBuilder, KeyCode, KeyModifiers, TestBackend, TextareaState};

#[test]
fn wraps_at_whitespace_without_changing_the_draft() {
    let mut backend = TestBackend::new(30, 8);
    let mut editor = TextareaState::new().word_wrap(10);
    editor.set_value("alpha beta gamma");
    backend.render(|ctx| {
        ctx.textarea(&mut editor, 5);
    });
    backend.assert_contains("beta gamma");
    assert_eq!(editor.value(), "alpha beta gamma");
}

#[test]
fn row_measurement_uses_the_editor_map_including_spaces_and_wide_clusters() {
    for (text, width, rows) in [
        ("alpha beta gamma", 10, 2),
        ("alpha beta gamma", 11, 2),
        ("a  b   ", 3, 3),
        ("abcdef", 2, 3),
        ("👩\u{200d}💻a", 1, 2),
        ("日本語", 3, 3),
        (" abc💻", 4, 3),
        (" a日語", 4, 3),
        ("\n", 5, 2),
        ("", 0, 1),
    ] {
        let mut editor = TextareaState::new().word_wrap(width);
        editor.set_value(text);
        assert_eq!(editor.visual_line_count(), rows, "{text:?} at {width}");
        assert_eq!(editor.value(), text);
    }
}

#[test]
fn wide_cluster_after_a_word_break_keeps_each_row_within_the_wrap_width() {
    let mut backend = TestBackend::new(20, 8);
    let mut editor = TextareaState::new().word_wrap(4);
    editor.set_value(" abc💻");
    editor.cursor_col = 0;
    for _ in 0..2 {
        backend.render(|ctx| {
            ctx.textarea(&mut editor, 5);
        });
    }
    let (x, y) = backend.find_text("💻").expect("whole wide cluster");
    backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
        ctx.textarea(&mut editor, 5);
    });
    assert_eq!((editor.cursor_row, editor.cursor_col), (0, 4));
    assert_eq!(editor.value(), " abc💻");
}

#[test]
fn wrapped_mouse_arrows_resize_and_selection_keep_source_coordinates() {
    let mut backend = TestBackend::new(30, 8);
    let original = "alpha beta gamma\n日本語  e\u{301} 👩\u{200d}💻  ";
    let mut editor = TextareaState::new().word_wrap(10);
    editor.set_value(original);
    editor.cursor_row = 0;
    editor.cursor_col = 0;
    for _ in 0..2 {
        backend.render(|ctx| {
            ctx.textarea(&mut editor, 6);
        });
    }
    let (x, y) = backend.find_text("beta gamma").expect("second visual line");
    backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
        ctx.textarea(&mut editor, 6);
    });
    assert_eq!((editor.cursor_row, editor.cursor_col), (0, 6));
    backend.run_with_events(EventBuilder::new().key_code(KeyCode::Up).build(), |ctx| {
        ctx.textarea(&mut editor, 6);
    });
    assert_eq!((editor.cursor_row, editor.cursor_col), (0, 0));
    backend.run_with_events(EventBuilder::new().key_code(KeyCode::Down).build(), |ctx| {
        ctx.textarea(&mut editor, 6);
    });
    assert_eq!((editor.cursor_row, editor.cursor_col), (0, 6));
    editor.wrap_width = Some(7);
    backend.run_with_events(EventBuilder::new().resize(20, 8).build(), |ctx| {
        ctx.textarea(&mut editor, 6);
    });
    backend.run_with_events(
        EventBuilder::new()
            .key_with(KeyCode::Char('a'), KeyModifiers::CONTROL)
            .build(),
        |ctx| {
            ctx.textarea(&mut editor, 6);
        },
    );
    assert_eq!(editor.selected_text().as_deref(), Some(original));
    assert_eq!(
        editor.value(),
        original,
        "wrapping never normalizes source whitespace or graphemes"
    );
}
