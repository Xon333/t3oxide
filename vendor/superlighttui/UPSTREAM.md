# SuperLightTUI 0.26.0

MIT; full upstream notices are in LICENSE and LICENSE.crossterm.
Base: https://github.com/subinium/SuperLightTUI/tree/d4537d74f4b42188e06cb224fbfe7684397897fb

Patch `ordered-activation-v1` adds the opt-in RunConfig activation_boundaries
policy in src/runtime/config.rs, src/runtime/kernel.rs and src/runtime/frame_state.rs. It renders preceding
edits before Enter Press, portable Ctrl+send presses or pointer activations,
then renders the activation alone. One input-free frame settles modal/focus
changes before the queued tail. The existing pending_input queue and request_frame
idle wake carry remaining input; no reader, queue or scheduler is added. Default batching, releases, paste, Tab/BackTab and
click_on_release behavior remain upstream behavior.

This standalone package retains upstream src/, README and both licence files. Cargo.toml
retains the upstream library dependencies/features and lints, removes the wasm
workspace, examples/bench targets and dev-only dependencies, and records the
base and patch above. No upstream examples, workspace crates or dependency
sources are bundled. Application regressions exercise the shared frame kernel
through TestBackend and the real run_async_with terminal entry point.
