# Dependency notices

The complete dependency graph is pinned by `go.mod` and `go.sum`. This source delivery does not vendor or modify Charm. License texts for every direct and indirect requirement listed in `go.mod` are included here; extra trailing blank lines are normalized. The original attribution and retained T3/Effect license texts remain in the root `NOTICE`.

| Module | Version | License text |
|---|---|---|
| `charm.land/bubbles/v2` | `v2.2.1` | [bubbles-MIT.txt](bubbles-MIT.txt) |
| `charm.land/bubbletea/v2` | `v2.1.0` | [bubbletea-MIT.txt](bubbletea-MIT.txt) |
| `charm.land/glamour/v2` | `v2.0.1` | [glamour-MIT.txt](glamour-MIT.txt) |
| `charm.land/lipgloss/v2` | `v2.0.6` | [lipgloss-MIT.txt](lipgloss-MIT.txt) |
| `github.com/charmbracelet/x/ansi` | `v0.11.9` | [github.com_charmbracelet_x_ansi-LICENSE.txt](github.com_charmbracelet_x_ansi-LICENSE.txt) |
| `github.com/alecthomas/chroma/v2` | `v2.14.0` | [github.com_alecthomas_chroma_v2-LICENSE.txt](github.com_alecthomas_chroma_v2-LICENSE.txt) |
| `github.com/atotto/clipboard` | `v0.1.4` | [github.com_atotto_clipboard-LICENSE.txt](github.com_atotto_clipboard-LICENSE.txt) |
| `github.com/aymerick/douceur` | `v0.2.0` | [github.com_aymerick_douceur-LICENSE.txt](github.com_aymerick_douceur-LICENSE.txt) |
| `github.com/charmbracelet/colorprofile` | `v0.4.3` | [github.com_charmbracelet_colorprofile-LICENSE.txt](github.com_charmbracelet_colorprofile-LICENSE.txt) |
| `github.com/charmbracelet/ultraviolet` | `v0.0.0-20261008173134-6b8d4baf91b4` | [github.com_charmbracelet_ultraviolet-LICENSE.txt](github.com_charmbracelet_ultraviolet-LICENSE.txt) |
| `github.com/charmbracelet/x/exp/slice` | `v0.0.0-20250327172914-2fdc97757edf` | [github.com_charmbracelet_x_exp_slice-LICENSE.txt](github.com_charmbracelet_x_exp_slice-LICENSE.txt) |
| `github.com/charmbracelet/x/term` | `v0.2.2` | [github.com_charmbracelet_x_term-LICENSE.txt](github.com_charmbracelet_x_term-LICENSE.txt) |
| `github.com/charmbracelet/x/termios` | `v0.1.1` | [github.com_charmbracelet_x_termios-LICENSE.txt](github.com_charmbracelet_x_termios-LICENSE.txt) |
| `github.com/charmbracelet/x/windows` | `v0.2.2` | [github.com_charmbracelet_x_windows-LICENSE.txt](github.com_charmbracelet_x_windows-LICENSE.txt) |
| `github.com/clipperhouse/displaywidth` | `v0.11.0` | [github.com_clipperhouse_displaywidth-LICENSE.txt](github.com_clipperhouse_displaywidth-LICENSE.txt) |
| `github.com/clipperhouse/uax29/v2` | `v2.7.0` | [github.com_clipperhouse_uax29_v2-LICENSE.txt](github.com_clipperhouse_uax29_v2-LICENSE.txt) |
| `github.com/dlclark/regexp2` | `v1.11.0` | [github.com_dlclark_regexp2-LICENSE.txt](github.com_dlclark_regexp2-LICENSE.txt) |
| `github.com/gorilla/css` | `v1.0.1` | [github.com_gorilla_css-LICENSE.txt](github.com_gorilla_css-LICENSE.txt) |
| `github.com/lucasb-eyer/go-colorful` | `v1.4.1` | [github.com_lucasb-eyer_go-colorful-LICENSE.txt](github.com_lucasb-eyer_go-colorful-LICENSE.txt) |
| `github.com/mattn/go-runewidth` | `v0.0.30` | [github.com_mattn_go-runewidth-LICENSE.txt](github.com_mattn_go-runewidth-LICENSE.txt) |
| `github.com/microcosm-cc/bluemonday` | `v1.0.27` | [github.com_microcosm-cc_bluemonday-LICENSE.txt](github.com_microcosm-cc_bluemonday-LICENSE.txt) |
| `github.com/muesli/cancelreader` | `v0.2.2` | [github.com_muesli_cancelreader-LICENSE.txt](github.com_muesli_cancelreader-LICENSE.txt) |
| `github.com/rivo/uniseg` | `v0.4.7` | [github.com_rivo_uniseg-LICENSE.txt](github.com_rivo_uniseg-LICENSE.txt) |
| `github.com/sahilm/fuzzy` | `v0.1.3` | [github.com_sahilm_fuzzy-LICENSE.txt](github.com_sahilm_fuzzy-LICENSE.txt) |
| `github.com/xo/terminfo` | `v1.0.0` | [github.com_xo_terminfo-LICENSE.txt](github.com_xo_terminfo-LICENSE.txt) |
| `github.com/yuin/goldmark` | `v1.7.17` | [github.com_yuin_goldmark-LICENSE.txt](github.com_yuin_goldmark-LICENSE.txt) |
| `github.com/yuin/goldmark-emoji` | `v1.0.5` | [github.com_yuin_goldmark-emoji-LICENSE.txt](github.com_yuin_goldmark-emoji-LICENSE.txt) |
| `golang.org/x/net` | `v0.39.0` | [golang.org_x_net-LICENSE.txt](golang.org_x_net-LICENSE.txt) |
| `golang.org/x/sync` | `v0.22.0` | [golang.org_x_sync-LICENSE.txt](golang.org_x_sync-LICENSE.txt) |
| `golang.org/x/sys` | `v0.47.0` | [golang.org_x_sys-LICENSE.txt](golang.org_x_sys-LICENSE.txt) |
| `golang.org/x/text` | `v0.39.0` | [golang.org_x_text-LICENSE.txt](golang.org_x_text-LICENSE.txt) |

Tests can fetch additional upstream test-only dependencies through the standard Go module graph. No dependency source or generated Go module cache is packaged.

## Adapted source

The workspace layout/focus patterns and bounded tool description/output rules adapt
[t3term](https://github.com/krishhgg/t3term/tree/d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e)
at `d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e`. Its original
[MIT license](t3term-LICENSE.txt) is retained here and in `NOTICE`, including the
host-helper distribution. [The reuse map](../docs/reuse-t3term.md) identifies the
specific source functions and adaptations. No Rust dependency graph is bundled.
