# SuperLightTUI 0.26.0

MIT; full upstream notices are in LICENSE and LICENSE.crossterm.
Base: https://github.com/subinium/SuperLightTUI/tree/d4537d74f4b42188e06cb224fbfe7684397897fb

Patch `ordered-activation-v2` adds the opt-in RunConfig activation_boundaries
policy in src/runtime/config.rs, src/runtime/kernel.rs and src/runtime/frame_state.rs. It renders preceding
edits before Enter, Escape, function/Control/Super key presses or pointer activations,
then renders the activation alone. One input-free frame settles modal/focus
changes before the queued tail. The existing pending_input queue and request_frame
idle wake carry remaining input; no reader, queue or scheduler is added. Default batching, releases, paste, Tab/BackTab and
click_on_release behavior remain upstream behavior.

Patch `textarea-word-wrap-v1` changes src/context/helpers.rs to prefer fitting
whitespace boundaries while preserving all original graphemes and whitespace.
Long tokens still split at whole graphemes. Cursor, mouse, selection and visual
arrow navigation share the existing contiguous segment map. In
src/widgets/text_input.rs, TextareaState::visual_line_count exposes that cached
layout for pane-local editor sizing. Application tests/slt_textarea.rs covers
word breaks, Unicode, source coordinates, selection and resize.

Patch `textarea-viewport-v1` changes src/context/widgets/text_input/textarea.rs
to clamp the existing scroll offset to the last full page after caret-follow.
It reuses the existing code editor's total-row/visible-row bound when a paste
grows the editor or a resize reflows it; caret, selection and source bytes stay
unchanged. Application tests in src/slt_ui.rs exercise actual SLT paste and
settled growth/resize; tests/slt_terminal.py checks painted composer content
through the production entry point. At the unchanged upstream commit above,
this file's SHA-256 is
`851e45d042eebb9624606e0d44fc7e8f694c7bcc3fe4d17012d3a5296ed12a70`.

This standalone package retains upstream src/, README and both licence files. Cargo.toml
retains the upstream library dependencies/features and lints, removes the wasm
workspace, examples/bench targets and dev-only dependencies, and records the
base and patch above. No upstream examples, workspace crates or dependency
sources are bundled. Application regressions exercise the shared frame kernel
through TestBackend and the real run_async_with terminal entry point.
