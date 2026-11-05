//! Source-qualified Markdown reports and bounded TermiFlow flowcharts (A4, B3).

use pulldown_cmark::{CodeBlockKind, Event, Parser, Tag, TagEnd};
use ratatui::style::{Color as TextColor, Modifier, Style as TextStyle};
use sha2::{Digest, Sha256};
use slt::{Context, ScrollState, Style, palette::tailwind};
use std::{
    collections::{HashMap, HashSet, VecDeque},
    sync::Arc,
};
use termiflow::{Config, FindingCode, FindingSeverity};
use unicode_width::UnicodeWidthStr;

const MAX_SOURCE_BYTES: usize = 16 * 1024;
const MAX_NODES: usize = 64;
const MAX_EDGES: usize = 128;
const MAX_GROUPS: usize = 32;
const MAX_LABEL_CELLS: usize = 256;
const MAX_CANVAS_ROWS: usize = 600;
const MAX_CANVAS_CELLS: usize = 120_000;
const MAX_CACHE_ENTRIES: usize = 24;
const AMBIENT_OVERRIDES: &[&str] = &[
    "TERMIFLOW_OPTIMIZE_RENDER",
    "TERMIFLOW_DISABLE_PORTALS",
    "TERMIFLOW_RENDER_REPAIR_PASSES",
    "TERMIFLOW_LAYOUT_REPAIR_PASSES",
    "TERMIFLOW_DEBUG_TIMING",
    "TERMIFLOW_DEBUG_ROUTES",
    "TERMIFLOW_DEBUG_CROSSING",
    "TERMIFLOW_DEBUG_CRITIC",
    "DEBUG_FANIN",
    "DEBUG_FANOUT",
    "DEBUG_CROSS",
];

/// Available report cells; resize participates in memoization and overflow checks.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Geometry {
    pub width: u32,
    pub height: u32,
}

/// Full-content revision when the native projection has no per-item revision.
pub fn content_revision(content: &str) -> String {
    format!("sha256:{:x}", Sha256::digest(content.as_bytes()))
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct CacheKey {
    source: String,
    revision: String,
    content: String,
    geometry: Geometry,
    runtime: String,
}

#[derive(Debug)]
struct Document {
    parts: Vec<Part>,
}

#[derive(Debug)]
enum Part {
    Markdown(Vec<Vec<MarkdownSpan>>),
    Mermaid {
        offset: usize,
        original: String,
        rendered: Result<Diagram, String>,
    },
}

#[derive(Debug)]
struct MarkdownSpan {
    text: String,
    style: TextStyle,
    href: Option<String>,
}

#[derive(Debug)]
struct Diagram {
    lines: Vec<String>,
    width: u32,
    height: u32,
    warnings: Vec<String>,
}

#[derive(Debug)]
struct RuntimeInputs {
    fingerprint: String,
    overrides: Vec<&'static str>,
}

impl RuntimeInputs {
    fn capture() -> Self {
        let mut digest = Sha256::new();
        let mut overrides = Vec::new();
        for name in AMBIENT_OVERRIDES
            .iter()
            .copied()
            .chain(["COLUMNS", "LINES"])
        {
            digest.update(name.as_bytes());
            if let Some(value) = std::env::var_os(name) {
                digest.update([1]);
                digest.update(value.to_string_lossy().as_bytes());
                if AMBIENT_OVERRIDES.contains(&name) {
                    overrides.push(name);
                }
            } else {
                digest.update([0]);
            }
            digest.update([0]);
        }
        Self {
            fingerprint: format!("{:x}", digest.finalize()),
            overrides,
        }
    }
}

/// Bounded source/geometry cache. It owns no terminal runtime or native state.
#[derive(Debug, Default)]
pub struct Cache {
    entries: VecDeque<(CacheKey, Arc<Document>)>,
    expanded: HashSet<String>,
    scroll: HashMap<String, ScrollState>,
}

impl Cache {
    /// Render existing Markdown and supported Mermaid flowcharts in the SLT frame.
    pub fn draw(
        &mut self,
        ctx: &mut Context,
        source: &str,
        revision: &str,
        markdown: &str,
        geometry: Geometry,
    ) {
        let document = self.prepare(source, revision, markdown, geometry);
        ctx.container().w(geometry.width.max(1)).gap(0).col(|ctx| {
            for part in &document.parts {
                match part {
                    Part::Markdown(lines) => {
                        for line in lines {
                            if line.is_empty() {
                                ctx.text(" ");
                            } else {
                                ctx.line_wrap(|ctx| {
                                    for span in line {
                                        if let Some(href) = &span.href {
                                            ctx.link(&span.text, href);
                                        } else {
                                            let style = slt_style(span.style, ctx);
                                            ctx.styled(&span.text, style);
                                        }
                                    }
                                });
                            }
                        }
                    }
                    Part::Mermaid {
                        offset,
                        original,
                        rendered,
                    } => {
                        let identity = format!("{source}\0{revision}\0{offset}");
                        let mut expanded = self.expanded.contains(&identity);
                        let warnings = rendered
                            .as_ref()
                            .map_or(&[][..], |diagram| diagram.warnings.as_slice());
                        ctx.row(|ctx| {
                            ctx.text(if rendered.is_err() {
                                "Diagram source"
                            } else if warnings.is_empty() {
                                "Flowchart"
                            } else {
                                "Flowchart · review"
                            })
                            .fg(
                                if warnings.is_empty() && rendered.is_ok() {
                                    tailwind::CYAN.c300
                                } else {
                                    tailwind::AMBER.c300
                                },
                            );
                            if ctx
                                .button(if expanded { "Hide source" } else { "Source" })
                                .clicked
                            {
                                expanded = !expanded;
                            }
                            if ctx.button("Copy source").clicked {
                                ctx.copy_to_clipboard(original);
                            }
                        });
                        if expanded {
                            self.expanded.insert(identity.clone());
                        } else {
                            self.expanded.remove(&identity);
                        }
                        match rendered {
                            Ok(diagram) => {
                                let scroll = self.scroll.entry(identity.clone()).or_default();
                                let visible_rows = diagram
                                    .height
                                    .min(geometry.height.saturating_sub(2).clamp(3, 18));
                                let style = Style::new().fg(ctx.theme().text);
                                ctx.scrollable(scroll).h(visible_rows).col(|ctx| {
                                    // Normal text participates in SLT overlay order. Deferred
                                    // buffer copies run after modals and would overwrite them.
                                    ctx.container()
                                        .w(diagram.width)
                                        .h(diagram.height)
                                        .gap(0)
                                        .col(|ctx| {
                                            for line in &diagram.lines {
                                                ctx.styled(line, style).w(diagram.width).h(1);
                                            }
                                        });
                                });
                            }
                            Err(reason) => {
                                ctx.text(format!("Source · {reason}"))
                                    .fg(tailwind::AMBER.c300)
                                    .wrap();
                            }
                        }
                        if expanded || rendered.is_err() {
                            for warning in warnings {
                                ctx.text(warning).fg(tailwind::AMBER.c300).wrap();
                            }
                            ctx.text(original)
                                .fg(tailwind::CYAN.c300)
                                .w(geometry.width.max(1))
                                .wrap();
                        }
                    }
                }
            }
        });
        // These are transient presentation caches; source text remains controller-owned.
        if self.scroll.len() > MAX_CACHE_ENTRIES * 4 {
            self.scroll.clear();
        }
        if self.expanded.len() > MAX_CACHE_ENTRIES * 4 {
            self.expanded.clear();
        }
    }

    fn prepare(
        &mut self,
        source: &str,
        revision: &str,
        markdown: &str,
        geometry: Geometry,
    ) -> Arc<Document> {
        let runtime = RuntimeInputs::capture();
        let key = CacheKey {
            source: source.into(),
            revision: revision.into(),
            content: content_revision(markdown),
            geometry,
            runtime: runtime.fingerprint.clone(),
        };
        if let Some(index) = self
            .entries
            .iter()
            .position(|(candidate, _)| candidate == &key)
        {
            let entry = self
                .entries
                .remove(index)
                .expect("cache position came from its entries");
            let document = Arc::clone(&entry.1);
            self.entries.push_back(entry);
            return document;
        }
        let document = Arc::new(prepare_document(markdown, geometry, &runtime));
        self.entries.push_back((key, Arc::clone(&document)));
        while self.entries.len() > MAX_CACHE_ENTRIES {
            self.entries.pop_front();
        }
        document
    }
}

fn prepare_document(markdown: &str, geometry: Geometry, runtime: &RuntimeInputs) -> Document {
    let mut parts = Vec::new();
    let mut preceding = 0;
    let mut fence: Option<(usize, String)> = None;
    for (event, range) in Parser::new(markdown).into_offset_iter() {
        match event {
            Event::Start(Tag::CodeBlock(CodeBlockKind::Fenced(language)))
                if language.trim() == "mermaid" =>
            {
                fence = Some((range.start, String::new()));
            }
            Event::Text(text) if fence.is_some() => {
                if let Some((_, code)) = &mut fence {
                    code.push_str(&text);
                }
            }
            Event::End(TagEnd::CodeBlock) => {
                if let Some((start, code)) = fence.take() {
                    if start > preceding {
                        parts.push(markdown_part(&markdown[preceding..start], geometry));
                    }
                    let original = markdown[start..range.end].to_owned();
                    parts.push(Part::Mermaid {
                        offset: start,
                        original,
                        rendered: render_flowchart(&code, geometry, runtime),
                    });
                    preceding = range.end;
                }
            }
            _ => {}
        }
    }
    if preceding < markdown.len() {
        parts.push(markdown_part(&markdown[preceding..], geometry));
    }
    Document { parts }
}

// Reuse the maintained Markdown parser and pane-width table layout already in
// the client. SLT still owns every output cell, overlay, focus and scroll region.
fn markdown_part(markdown: &str, geometry: Geometry) -> Part {
    let destinations: HashSet<_> = Parser::new(markdown)
        .filter_map(|event| {
            if let Event::Start(Tag::Link { dest_url, .. }) = event {
                Some(dest_url.into_string())
            } else {
                None
            }
        })
        .collect();
    let options = tui_markdown::Options::new(ReportStyles)
        .table_width(u16::try_from(geometry.width.max(1)).unwrap_or(u16::MAX));
    let text = tui_markdown::from_str_with_options(markdown, &options);
    Part::Markdown(
        text.lines
            .into_iter()
            .map(|line| {
                let mut spans: Vec<_> = line
                    .spans
                    .into_iter()
                    .map(|span| MarkdownSpan {
                        text: span.content.into_owned(),
                        style: text.style.patch(line.style).patch(span.style),
                        href: None,
                    })
                    .collect();
                restore_links(&mut spans, &destinations);
                spans
            })
            .collect(),
    )
}

fn restore_links(spans: &mut Vec<MarkdownSpan>, destinations: &HashSet<String>) {
    let mut index = 0;
    while index < spans.len() {
        if !destinations.contains(&spans[index].text)
            || !spans[index]
                .style
                .add_modifier
                .contains(Modifier::UNDERLINED)
        {
            index += 1;
            continue;
        }
        let destination = spans[index].text.clone();
        // tui-markdown 0.3.13 emits label, " (", destination, ")" as
        // separate spans. Restore SLT's native link control without a second
        // visible URL; wrapped table fragments keep their complete source.
        if index >= 2
            && spans[index - 1].text == " ("
            && spans.get(index + 1).is_some_and(|span| span.text == ")")
        {
            let mut start = index - 1;
            while start > 0
                && spans[start - 1].href.is_none()
                && spans[start - 1]
                    .style
                    .add_modifier
                    .contains(Modifier::UNDERLINED)
            {
                start -= 1;
            }
            if start < index - 1 {
                for span in &mut spans[start..index - 1] {
                    span.href = Some(destination.clone());
                }
                spans.drain(index - 1..=index + 1);
                index = start + 1;
                continue;
            }
        }
        spans[index].href = Some(destination);
        index += 1;
    }
}

#[derive(Clone)]
struct ReportStyles;

impl tui_markdown::StyleSheet for ReportStyles {
    fn heading(&self, _level: u8) -> TextStyle {
        TextStyle::new()
            .fg(TextColor::Cyan)
            .add_modifier(Modifier::BOLD)
    }

    fn heading_marker(&self, _level: u8) -> &str {
        ""
    }
    fn code_block_fence(&self) -> &str {
        ""
    }
    fn code(&self) -> TextStyle {
        TextStyle::new().fg(TextColor::Cyan)
    }
    fn blockquote(&self) -> TextStyle {
        TextStyle::new().add_modifier(Modifier::DIM)
    }
}

fn slt_style(style: TextStyle, ctx: &Context) -> Style {
    let mut converted = Style::new().fg(match style.fg {
        Some(TextColor::Cyan | TextColor::LightCyan | TextColor::Blue | TextColor::LightBlue) => {
            ctx.theme().primary
        }
        Some(TextColor::Red | TextColor::LightRed) => ctx.theme().error,
        Some(TextColor::Green | TextColor::LightGreen) => ctx.theme().success,
        Some(TextColor::Yellow | TextColor::LightYellow) => ctx.theme().warning,
        Some(TextColor::Magenta | TextColor::LightMagenta) => ctx.theme().secondary,
        Some(TextColor::DarkGray) => ctx.theme().text_dim,
        Some(TextColor::Rgb(r, g, b)) => slt::Color::Rgb(r, g, b),
        Some(TextColor::Indexed(index)) => slt::Color::Indexed(index),
        _ => ctx.theme().text,
    });
    for (source, target) in [
        (Modifier::BOLD, slt::Modifiers::BOLD),
        (Modifier::DIM, slt::Modifiers::DIM),
        (Modifier::ITALIC, slt::Modifiers::ITALIC),
        (Modifier::UNDERLINED, slt::Modifiers::UNDERLINE),
        (Modifier::CROSSED_OUT, slt::Modifiers::STRIKETHROUGH),
    ] {
        if style.add_modifier.contains(source) {
            converted.modifiers |= target;
        }
    }
    converted
}

fn render_flowchart(
    source: &str,
    geometry: Geometry,
    runtime: &RuntimeInputs,
) -> Result<Diagram, String> {
    let render = || -> anyhow::Result<Diagram> {
        anyhow::ensure!(
            runtime.overrides.is_empty(),
            "renderer process overrides active: {}",
            runtime.overrides.join(", ")
        );
        anyhow::ensure!(
            source.len() <= MAX_SOURCE_BYTES,
            "diagram source exceeds 16 KiB render budget"
        );
        let header = source
            .lines()
            .map(str::trim)
            .find(|line| !line.is_empty() && !line.starts_with("%%"))
            .unwrap_or("");
        anyhow::ensure!(
            matches!(
                header.split_whitespace().next(),
                Some("graph" | "flowchart")
            ),
            "only Mermaid graph/flowchart diagrams render"
        );
        let parsed = termiflow::parse(source, true)?;
        let mut graph = parsed.graph;
        anyhow::ensure!(
            !graph.nodes.is_empty()
                && graph.nodes.len() <= MAX_NODES
                && graph.edges.len() <= MAX_EDGES
                && graph.subgraphs.len() <= MAX_GROUPS,
            "diagram exceeds 64-node/128-edge/32-group render budget"
        );
        let directives = parsed.config;
        anyhow::ensure!(
            directives.style.is_none()
                && directives.max_label.is_none()
                && directives.max_edge_label.is_none()
                && directives.wrap_labels.is_none()
                && directives.max_label_lines.is_none()
                && directives.spacing_mode.is_none()
                && directives.optimize_render.is_none()
                && directives.render_repair_passes.is_none()
                && directives.layout_repair_passes.is_none()
                && directives.debug_critic.is_none(),
            "TermiFlow configuration directives require source review"
        );
        let node_width = graph
            .nodes
            .iter()
            .map(|node| termiflow::display_width(&node.label))
            .max()
            .unwrap_or(1)
            .max(1);
        let edge_width = graph
            .edges
            .iter()
            .filter_map(|edge| edge.label.as_ref())
            .map(|label| termiflow::display_width(label))
            .max()
            .unwrap_or(1)
            .max(1);
        let group_width = graph
            .subgraphs
            .iter()
            .filter_map(|group| group.title.as_deref())
            .map(termiflow::display_width)
            .max()
            .unwrap_or(0);
        anyhow::ensure!(
            node_width.max(edge_width).max(group_width) <= MAX_LABEL_CELLS,
            "label exceeds 256 terminal cells; source retained"
        );
        let mut config = Config {
            max_label_width: node_width,
            max_edge_label_width: edge_width,
            wrap_labels: true,
            max_label_lines: MAX_SOURCE_BYTES,
            strict_parsing: true,
            ..Config::default()
        };
        config.spacing = config.spacing.for_direction(graph.direction);
        config.spacing.max_label_width = config.max_label_width;
        config.spacing.max_canvas_width = geometry.width.max(1) as usize;
        config.spacing.max_canvas_height =
            MAX_CANVAS_ROWS.min(MAX_CANVAS_CELLS / geometry.width.max(1) as usize);
        termiflow::measure::measure_graph(&mut graph, &config);
        let graph = termiflow::layout::coarse_waterfall_with_config(
            graph,
            termiflow::layout::CoarseLayoutConfig::from_spacing(&config.spacing),
        )?;
        let width = graph
            .nodes
            .iter()
            .map(|node| node.x + node.width)
            .chain(
                graph
                    .subgraphs
                    .iter()
                    .map(|group| group.bounds.x + group.bounds.width),
            )
            .max()
            .unwrap_or(0);
        let height = graph
            .nodes
            .iter()
            .map(|node| node.y + node.height)
            .chain(
                graph
                    .subgraphs
                    .iter()
                    .map(|group| group.bounds.y + group.bounds.height),
            )
            .max()
            .unwrap_or(0);
        anyhow::ensure!(
            width <= geometry.width as usize,
            "flowchart needs {width} cells; available {}",
            geometry.width
        );
        anyhow::ensure!(
            height <= config.spacing.max_canvas_height,
            "flowchart exceeds bounded canvas rows"
        );
        let outcome = termiflow::render_canvas_with_feedback(&graph, &config)?;
        anyhow::ensure!(
            !outcome
                .critic_report
                .findings
                .iter()
                .any(|finding| finding.code == FindingCode::CanvasClipped
                    || finding.severity == FindingSeverity::Error),
            "renderer reported clipped or invalid geometry"
        );
        let lines: Vec<_> = outcome.output.lines().map(str::to_owned).collect();
        let width = lines
            .iter()
            .map(|line| UnicodeWidthStr::width(line.as_str()))
            .max()
            .unwrap_or(0);
        anyhow::ensure!(
            width > 0
                && width <= geometry.width as usize
                && lines.len() <= config.spacing.max_canvas_height,
            "rendered flowchart exceeds available geometry"
        );
        anyhow::ensure!(
            RuntimeInputs::capture().fingerprint == runtime.fingerprint,
            "renderer process inputs changed during layout"
        );
        let mut warnings = graph.warnings;
        warnings.extend(outcome.warnings);
        warnings.extend(
            outcome
                .critic_report
                .findings
                .iter()
                .map(|finding| format!("{:?}: {}", finding.severity, finding.message)),
        );
        warnings.sort();
        warnings.dedup();
        Ok(Diagram {
            width: width as u32,
            height: lines.len() as u32,
            lines,
            warnings,
        })
    };
    render().map_err(|error| error.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use slt::{EventBuilder, TestBackend};

    const FLOWCHART: &str = "Before\n\n```mermaid\nflowchart TD\n A[Complete source revision label] --> B[日本語 🦀 ready]\n```\n\nAfter";
    const GEOMETRY: Geometry = Geometry {
        width: 100,
        height: 35,
    };

    fn diagram(document: &Document) -> Result<&Diagram, &str> {
        document
            .parts
            .iter()
            .find_map(|part| match part {
                Part::Mermaid { rendered, .. } => Some(rendered.as_ref().map_err(String::as_str)),
                _ => None,
            })
            .expect("test contains a Mermaid fence")
    }

    #[test]
    fn markdown_uses_allocated_pane_width_and_keeps_adjacent_content_clear() {
        let mut cache = Cache::default();
        let mut backend = TestBackend::new(80, 18);
        let markdown = "## Check\nA **carefully styled** report wraps to the available pane width and preserves the final words.\n\n- A useful decision with supporting evidence and a selected option";
        for _ in 0..2 {
            backend.render(|ctx| {
                ctx.row(|ctx| {
                    ctx.container().w(30).col(|ctx| {
                        cache.draw(
                            ctx,
                            "source",
                            "revision",
                            markdown,
                            Geometry {
                                width: 30,
                                height: 18,
                            },
                        );
                    });
                    ctx.container().w(30).col(|ctx| {
                        ctx.text("Adjacent Context");
                    });
                });
            });
        }
        backend.assert_contains("words.");
        backend.assert_contains("selected option");
        backend.assert_contains("Adjacent Context");
        for y in 1..18 {
            for x in 30..80 {
                assert!(
                    backend.buffer().get(x, y).symbol.trim().is_empty(),
                    "report painted into adjacent pane at {x},{y}"
                );
            }
        }
    }

    #[test]
    fn table_cells_and_code_wrap_without_losing_their_trailing_content() {
        let mut cache = Cache::default();
        let mut backend = TestBackend::new(64, 30);
        let markdown = "| Check | Evidence |\n| --- | --- |\n| Width | A carefully wrapped decision with complete evidence tail |\n\n```rust\nlet original_source = keep_every_character_and_the_final_suffix;\n```";
        for _ in 0..2 {
            backend.render(|ctx| {
                ctx.row(|ctx| {
                    cache.draw(
                        ctx,
                        "source",
                        "revision",
                        markdown,
                        Geometry {
                            width: 28,
                            height: 30,
                        },
                    );
                    ctx.container().w(30).col(|ctx| {
                        ctx.text("Adjacent Context");
                    });
                });
            });
        }
        backend.assert_contains("tail");
        backend.assert_contains("suffix;");
        backend.assert_contains("Adjacent Context");
        for y in 1..30 {
            for x in 28..64 {
                assert!(
                    backend.buffer().get(x, y).symbol.trim().is_empty(),
                    "table/code painted into adjacent pane at {x},{y}"
                );
            }
        }
    }

    #[test]
    fn markdown_links_keep_the_native_target_without_duplicate_visible_urls() {
        let Part::Markdown(lines) = markdown_part(
            "Read [**the guide**](https://example.com/guide) and [evidence](https://example.com/check).",
            GEOMETRY,
        ) else {
            panic!("expected Markdown");
        };
        let spans: Vec<_> = lines.iter().flatten().collect();
        let guide = spans.iter().find(|span| span.text == "the guide").unwrap();
        assert_eq!(guide.href.as_deref(), Some("https://example.com/guide"));
        let evidence = spans.iter().find(|span| span.text == "evidence").unwrap();
        assert_eq!(evidence.href.as_deref(), Some("https://example.com/check"));
        assert!(!spans.iter().any(|span| span.text.contains("https://")));
    }

    #[test]
    fn termiflow_unicode_labels_reach_slt_and_source_remains_available() {
        let mut cache = Cache::default();
        let document = cache.prepare("env/thread/item", "native-revision", FLOWCHART, GEOMETRY);
        let rendered = diagram(&document).expect("actual TermiFlow flowchart renders");
        let text = rendered.lines.join("\n");
        assert!(text.contains("Complete source revision label"), "{text}");
        assert!(text.contains("日本語 🦀 ready"), "{text}");
        let mut backend = TestBackend::new(100, 45);
        for _ in 0..2 {
            backend.render(|ctx| {
                cache.draw(
                    ctx,
                    "env/thread/item",
                    "native-revision",
                    FLOWCHART,
                    GEOMETRY,
                )
            });
        }
        backend.assert_contains("Complete source revision label");
        backend.assert_contains("日本語 🦀 ready");
        let (x, y) = backend.find_text("Source").expect("source toggle rendered");
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            cache.draw(
                ctx,
                "env/thread/item",
                "native-revision",
                FLOWCHART,
                GEOMETRY,
            );
        });
        backend.render(|ctx| {
            cache.draw(
                ctx,
                "env/thread/item",
                "native-revision",
                FLOWCHART,
                GEOMETRY,
            )
        });
        backend.assert_not_contains("env/thread/item @ native-revision");
        backend.assert_contains("flowchart TD");
    }

    #[test]
    fn flowchart_respects_opaque_modal_painting_and_dismissal() {
        let mut cache = Cache::default();
        let mut backend = TestBackend::new(100, 45);
        for modal in [false, true, false] {
            for _ in 0..2 {
                backend.render(|ctx| {
                    cache.draw(ctx, "source", "revision", FLOWCHART, GEOMETRY);
                    if modal {
                        ctx.modal(|ctx| {
                            ctx.container()
                                .w(100)
                                .h(45)
                                .bg(slt::Color::Blue)
                                .col(|ctx| {
                                    ctx.text("Opaque modal");
                                });
                        });
                    }
                });
            }
            if modal {
                backend.assert_contains("Opaque modal");
                backend.assert_not_contains("Complete source revision label");
                backend.assert_not_contains("日本語 🦀 ready");
                backend.assert_style_at(20, 10, Style::new().bg(slt::Color::Blue));
            } else {
                backend.assert_contains("Complete source revision label");
                backend.assert_contains("日本語 🦀 ready");
            }
        }
    }

    #[test]
    fn cache_qualifies_source_revision_content_geometry_and_preserves_overflow() {
        let mut cache = Cache::default();
        let first = cache.prepare("source", "revision", FLOWCHART, GEOMETRY);
        let again = cache.prepare("source", "revision", FLOWCHART, GEOMETRY);
        assert!(Arc::ptr_eq(&first, &again));
        assert!(diagram(&first).is_ok());
        for changed in [
            cache.prepare("other-source", "revision", FLOWCHART, GEOMETRY),
            cache.prepare("source", "other-revision", FLOWCHART, GEOMETRY),
            cache.prepare(
                "source",
                "revision",
                &FLOWCHART.replace("Before", "New content"),
                GEOMETRY,
            ),
        ] {
            assert!(!Arc::ptr_eq(&first, &changed));
        }
        let narrow = cache.prepare(
            "source",
            "revision",
            FLOWCHART,
            Geometry {
                width: 10,
                ..GEOMETRY
            },
        );
        assert!(!Arc::ptr_eq(&first, &narrow));
        assert!(diagram(&narrow).unwrap_err().contains("available"));
        assert!(
            matches!(&narrow.parts[1], Part::Mermaid { original, .. } if original.contains("日本語 🦀 ready"))
        );
        assert!(Arc::ptr_eq(
            &first,
            &cache.prepare("source", "revision", FLOWCHART, GEOMETRY)
        ));
    }

    #[test]
    fn unsupported_diagram_and_ambient_overrides_keep_original_source() {
        let markdown = "```mermaid\nsequenceDiagram\n Alice->>Bob: Question\n```";
        let runtime = RuntimeInputs::capture();
        let document = prepare_document(markdown, GEOMETRY, &runtime);
        assert!(
            diagram(&document)
                .unwrap_err()
                .contains("only Mermaid graph/flowchart")
        );
        assert!(
            matches!(&document.parts[0], Part::Mermaid { original, .. } if original == markdown)
        );
        let blocked_runtime = RuntimeInputs {
            fingerprint: runtime.fingerprint,
            overrides: vec!["TERMIFLOW_DEBUG_ROUTES"],
        };
        let document = prepare_document(FLOWCHART, GEOMETRY, &blocked_runtime);
        assert!(
            diagram(&document)
                .unwrap_err()
                .contains("TERMIFLOW_DEBUG_ROUTES")
        );
    }
}
