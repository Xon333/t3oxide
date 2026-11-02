# Rust source decisions

Source review: 2026-11-02. This records inspected implementations and the decisions
they support. Dependency versions are fixed by `Cargo.lock`; source review SHAs
below identify the upstream material considered, not vendored applications.

## Foundation

Use one renderer, Ratatui, with Crossterm input. Ratatui's cell buffer and terminal
diff already solve clipping, wide graphemes, resize and minimal terminal output.
Do not add a second framework or maintain a private ANSI renderer. Application
events own state changes; render only after relevant changes and coalesce bursts.
Use the maintained Ratatui textarea for Unicode editing, selection, undo/redo,
wrapping and mouse cursor mapping. Use tui-markdown without its optional syntax
highlighter: this retains semantic Markdown while avoiding a regex engine and
syntax database in the client.

| Source | Inspected implementation and license | Decision |
| --- | --- | --- |
| [Ratatui](https://github.com/ratatui/ratatui/tree/3fdb4a0b95111b4c0fb1ee9838dfba05b806b8a5) | `ratatui-core/src/terminal/render.rs`, `ratatui-core/src/buffer/buffer.rs`, `ratatui/Cargo.toml`, `examples/apps/async-github/src/main.rs`; MIT | Direct library reuse for layout, buffered diffs, lists, paragraphs, borders and test backend. Keep the entire frame valid on every draw; let Ratatui emit only changed cells. The example's fixed 60 Hz redraw is unnecessary for an idle client. |
| [Ratatui textarea](https://github.com/ratatui/ratatui-textarea/tree/ratatui-textarea-v0.9.3) | `src/textarea.rs`, `src/input/mod.rs`, `examples/minimal.rs`, `examples/wrap.rs`, manifest and `LICENSE.txt`; MIT | Direct dependency for the composer and text forms. Version 0.9.3 matches Ratatui 0.30's core/widgets and Crossterm 0.29. Mouse mapping must use the widget's scroll offset and screen-to-data conversion, including wide characters. |
| [tui-markdown](https://github.com/joshka/tui-markdown/tree/tui-markdown-v0.3.13) | `tui-markdown/src/lib.rs`, `tui-markdown/Cargo.toml`; MIT OR Apache-2.0 | Direct dependency with `default-features = false`. Reuse its pulldown-cmark-based renderer and pane-width table layout; retain exact original message text separately for copying and recovery. Images remain descriptive text; rendering does not fetch resources. |
| [nucleo-matcher](https://github.com/helix-editor/nucleo/tree/355938f0eeeeec040c02b93b8105aa508e6922b5/matcher) | `matcher/Cargo.toml`, `matcher/src/pattern.rs`, `matcher/LICENSE`; MPL-2.0 | Direct, unmodified `nucleo-matcher` 0.3.1 dependency for Unicode-aware fuzzy matching and scored action ranking. Reuse the matcher alone, not the threaded indexing application. The MPL license and an exact source download link accompany distributions; no upstream file is modified. |
| [FrankenTUI](https://github.com/Dicklesworthstone/frankentui/blob/0f5bd2444753d84ab9f5e2ac37033fcc8568d218/LICENSE) | Repository inventory and full license rider | Excluded. The rider withholds rights from specified parties and expressly includes analysis, testing and benchmarking. No code, algorithm translation, assets, dependency or benchmark is incorporated. The license is the decisive boundary; no claim of permitted implementation reuse is made. Ratatui independently supplies the required rendering primitives. |

## Workspace interaction

| Source | Inspected implementation and license | Useful behavior and boundary |
| --- | --- | --- |
| [Herdr](https://github.com/herdrdev/herdr/tree/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde) | `src/render_signal.rs`, `src/client/endpoint/health.rs`, `src/client/shell/workspace_navigation.rs`, `Cargo.toml`; Apache-2.0 | Coalesce render requests; distinguish connection establishment from established transport health; bind navigation to endpoint identity and snapshot generation. Preserve the current selection while a request awaits authoritative state. Do not import Herdr's server, PTY ownership, terminal emulator, agent hooks or session manager: T3 already owns execution. |
| [Herdr T3 Focus](https://github.com/JinJieBeWater/herdr-t3-focus/tree/84c7c7280e7c1ff6c815ee9aa314e32f2bdec32b) | `mods/t3-focus/main.js`, `mods/t3-focus/mod.json`; Apache-2.0 | Attention, Working and Settled are meaningful views of the same conversations. Settling is explicit and bound to the observed turn; merely viewing a conversation is not settling it. New work revives attention. Pinned items and parent/child context remain available. Apply these interaction principles to native T3 identities, not Herdr terminal labels or JavaScript modules. |
| [Herdr T3 layout](https://github.com/JinJieBeWater/herdr-t3/tree/bf05b354f5fc1ed80106a0d3fc7e02f0801d90b6) | `layouts/herdr-t3.json`; Apache-2.0 | Stable sidebar, work surface and focused-agent surface share a layout. Adapt the hierarchy to terminal dimensions with a compact mode and explicit focus. Do not import GPUI, pixel dimensions or the mod system. |
| [TUIOS](https://github.com/Gaurav-Gosain/tuios/tree/13dbd614969eabb6f57f23bf6799f21c96d80830) | `internal/app/command_palette.go`, `command_palette_nav.go`, `inbox_lifecycle.go`, `master_layout.go`, `frame_render.go`; MIT | One discoverable action catalog with visible current shortcuts; keyboard and mouse address the same actions; explicit pane focus and zoom; per-workspace viewing preferences; attention items that lead directly to their conversation. Stale targets cannot mutate; settle/restore is a reversible client view operation. Avoid its daemon, agent inbox authority, PTY session persistence, Go renderer and process management. Ratatui replaces its cell/ANSI implementation. |

Developer and AgentBox should share selection, conversation, draft, request and
recovery models. They change which information has visual priority. They must not
become separate implementations of T3 operations. Tiling and focus are client
presentation, never new execution sessions.

## Transport and Rust engineering

| Source | Inspected implementation and license | Decision |
| --- | --- | --- |
| [Pingora](https://github.com/cloudflare/pingora/tree/1adcb3815b70d6594379db044b5aa67f9bc1af76) | `pingora-core/src/connectors/http/mod.rs`, `pingora-proxy/src/lib.rs`, license; Apache-2.0 | Reject as a dependency. Its HTTP connection pools, proxy sessions and server runtime do not solve a missing requirement in t3oxide's existing T3 adapter transport. Adding a proxy or gateway would add another process and failure boundary. |
| [Google Comprehensive Rust](https://github.com/google/comprehensive-rust/tree/c271ecdcb63c37195fcc9715039a96ed7d758e80) | `src/concurrency/async-pitfalls/cancellation.md`, `src/concurrency/channels/bounded.md`; prose CC-BY-4.0, code Apache-2.0 | Apply bounded message passing, explicit ownership and cancellation-safe transport reads. In particular, do not race a partially consuming `read_line` future against a timer and lose bytes. Persist exact mutation identity before sending; cancellation is not evidence that a mutation failed remotely. Guidance only; no copied course material. |
| [Rust Course](https://github.com/sunface/rust-course/tree/a4a7eead178464dfa2430a9c45e6478523ae549b) | `src/advance/concurrency-with-threads/message-passing.md` and README license section | Apply ownership/message-passing reasoning, with application errors handled explicitly. The project states No License for its original course content, despite separately attributed MIT examples. No code or prose is copied or redistributed. |
| [Awesome Rust](https://github.com/rust-unofficial/awesome-rust/tree/434b62e0e10744034cc3141e3263235e4532ab53) | Command-line, async, serialization and markup sections; CC0 list | Discovery index for Ratatui, Crossterm, Tokio, Serde and pulldown-cmark. Dependencies must be justified by direct upstream API/source review, not by being listed. No additional agents, schedulers or orchestration frameworks. |

Exa was used to discover the maintained Ratatui textarea fork; its manifest,
license and input implementation were then checked directly. Tavily identified
Ratatui-compatible Markdown components; the chosen renderer's upstream manifest
and source were verified. No second search provider was used merely to repeat an
already resolved implementation decision.

## Reuse accounting

Direct dependencies retain their upstream licenses in `LICENSES/rust-third-party.txt`;
`docs/dependencies.json` records the complete locked source inventory and checksums.
The unmodified MPL-2.0 matcher source is available at
https://crates.io/api/v1/crates/nucleo-matcher/0.3.1/download. The release archive
records upstream commit `355938f0eeeeec040c02b93b8105aa508e6922b5`.
Library linking does not transplant its MPL-covered files into application source. The Herdr and TUIOS entries above are interaction references,
not claims that their application code was copied. Source transplantation is not
needed to reuse their strongest compatible ideas. No restricted FrankenTUI or
Rust Course material is present in the application.

The existing t3oxide adapter and current T3 contracts remain the integration
authority. Any retained adapter must be justified by the verified transport and
recovery behavior it preserves, rather than by the previous UI language.
