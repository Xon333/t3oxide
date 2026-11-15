//! Source-qualified Markdown reports and bounded TermiFlow flowcharts (A4, B3).

use pulldown_cmark::{CodeBlockKind, Event, Options, Parser, Tag, TagEnd};
use sha2::{Digest, Sha256};
use slt::{Context, Style, palette::tailwind};
use std::{
    collections::{HashSet, VecDeque},
    sync::Arc,
};
use termiflow::{Config, FindingCode, FindingSeverity};
use unicode_segmentation::UnicodeSegmentation;
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
    Markdown(String),
    Mermaid {
        offset: usize,
        original: String,
        rendered: Result<Diagram, String>,
    },
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
                    Part::Markdown(markdown) => {
                        draw_markdown(ctx, markdown, geometry.width);
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
                        ctx.container().wrap().row(|ctx| {
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
                                let style = Style::new().fg(ctx.theme().text);
                                // The containing report owns scrolling for the entire diagram.
                                // Normal text retains SLT's modal paint order.
                                ctx.container()
                                    .w(diagram.width)
                                    .h(diagram.height)
                                    .gap(0)
                                    .col(|ctx| {
                                        for line in &diagram.lines {
                                            ctx.styled(line, style).w(diagram.width).h(1);
                                        }
                                    });
                            }
                            Err(reason) => {
                                ctx.text(format!("Source · {reason}"))
                                    .fg(tailwind::AMBER.c300)
                                    .wrap();
                            }
                        }
                        if expanded || rendered.is_err() {
                            ctx.text(format!("{source} @ {revision} · fence {offset}"))
                                .dim()
                                .wrap();
                            for warning in warnings {
                                ctx.text(warning).fg(tailwind::AMBER.c300).wrap();
                            }
                            draw_source(ctx, original, geometry.width);
                        }
                    }
                }
            }
        });
        // These are transient presentation caches; source text remains controller-owned.
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

/// Render Markdown within an allocated pane. SLT retains ordinary inline links;
/// the existing table library supplies cell wrapping without another renderer.
pub fn draw_markdown(ctx: &mut Context, markdown: &str, width: u32) {
    let width = width.max(1);
    ctx.container().w(width).gap(0).col(|ctx| {
        let mut preceding = 0;
        let mut depth = 0usize;
        let mut block: Option<(usize, usize, TagEnd)> = None;
        let mut code = String::new();
        for (event, range) in Parser::new_ext(markdown, Options::ENABLE_TABLES).into_offset_iter() {
            match event {
                Event::Start(tag) => {
                    if block.is_none()
                        && matches!(
                            tag,
                            Tag::CodeBlock(_)
                                | Tag::Heading { .. }
                                | Tag::BlockQuote(_)
                                | Tag::Table(_)
                        )
                    {
                        block = Some((range.start, depth, tag.to_end()));
                        code.clear();
                    }
                    depth += 1;
                }
                Event::Text(text) if matches!(block, Some((_, _, TagEnd::CodeBlock))) => {
                    code.push_str(&text)
                }
                Event::End(_) => {
                    depth = depth.saturating_sub(1);
                    if block.as_ref().is_some_and(|(_, level, _)| *level == depth) {
                        let (start, _, kind) = block.take().expect("matching block was present");
                        if preceding < start {
                            ctx.markdown(&markdown[preceding..start]);
                        }
                        let source = &markdown[start..range.end];
                        match kind {
                            TagEnd::CodeBlock => draw_source(ctx, &code, width),
                            TagEnd::Heading(_) => {
                                // Retain the original inline syntax and destinations; only
                                // the block marker is replaced by the pane's heading colour.
                                let mut start = source.len();
                                let mut end = 0;
                                for (event, range) in Parser::new(source).into_offset_iter() {
                                    if !matches!(
                                        event,
                                        Event::Start(Tag::Heading { .. })
                                            | Event::End(TagEnd::Heading(_))
                                    ) {
                                        start = start.min(range.start);
                                        end = end.max(range.end);
                                    }
                                }
                                let mut theme = *ctx.theme();
                                theme.text = theme.primary;
                                ctx.container().w(width).gap(0).theme(theme).col(|ctx| {
                                    ctx.markdown(source.get(start..end).unwrap_or_default());
                                });
                            }
                            TagEnd::BlockQuote(_) => {
                                let text = source
                                    .lines()
                                    .map(|line| {
                                        let mut body = line.trim_start();
                                        while let Some(rest) = body.strip_prefix('>') {
                                            body =
                                                rest.strip_prefix(' ').unwrap_or(rest).trim_start();
                                        }
                                        body
                                    })
                                    .collect::<Vec<_>>()
                                    .join("\n");
                                let mut theme = *ctx.theme();
                                theme.text = theme.text_dim;
                                ctx.container().w(width).gap(0).theme(theme).col(|ctx| {
                                    draw_markdown(ctx, &text, width);
                                });
                            }
                            TagEnd::Table => draw_table(ctx, source, width),
                            _ => unreachable!("only bounded Markdown blocks are captured"),
                        }
                        preceding = range.end;
                    }
                }
                _ => {}
            }
        }
        if preceding < markdown.len() {
            ctx.markdown(&markdown[preceding..]);
        }
    });
}

fn draw_source(ctx: &mut Context, source: &str, width: u32) {
    let style = Style::new().fg(ctx.theme().accent);
    for logical in source.split('\n') {
        let mut line = String::new();
        let mut cells = 0;
        for grapheme in logical.graphemes(true) {
            let count = UnicodeWidthStr::width(grapheme) as u32;
            if cells + count > width.max(1) && !line.is_empty() {
                ctx.styled(std::mem::take(&mut line), style)
                    .w(width.max(1))
                    .h(1);
                cells = 0;
            }
            line.push_str(grapheme);
            cells += count;
        }
        ctx.styled(if line.is_empty() { " ".into() } else { line }, style)
            .w(width.max(1))
            .h(1);
    }
}

fn table_rows(markdown: &str) -> Vec<Vec<&str>> {
    let mut rows = Vec::new();
    let mut row = Vec::new();
    for (event, range) in Parser::new_ext(markdown, Options::ENABLE_TABLES).into_offset_iter() {
        match event {
            Event::Start(Tag::TableCell) => row.push(markdown[range].trim()),
            Event::End(TagEnd::TableHead | TagEnd::TableRow) => rows.push(std::mem::take(&mut row)),
            _ => {}
        }
    }
    rows
}

fn draw_table(ctx: &mut Context, markdown: &str, width: u32) {
    let links = Parser::new_ext(markdown, Options::ENABLE_TABLES)
        .any(|event| matches!(event, Event::Start(Tag::Link { .. })));
    let rows = table_rows(markdown);
    let columns = rows.iter().map(Vec::len).max().unwrap_or(0);
    let mut widths = vec![0; columns];
    for row in &rows {
        for (index, cell) in row.iter().enumerate() {
            let label = Parser::new(cell)
                .filter_map(|event| match event {
                    Event::Text(text) | Event::Code(text) => Some(text),
                    _ => None,
                })
                .fold(String::new(), |mut value, text| {
                    value.push_str(&text);
                    value
                });
            widths[index] = widths[index].max(UnicodeWidthStr::width(label.as_str()));
        }
    }
    let natural = widths.iter().sum::<usize>() + columns * 3 + 1;
    if links && natural <= width as usize {
        ctx.markdown(markdown);
        return;
    }
    let options = tui_markdown::Options::new(TableStyles)
        .table_width(u16::try_from(width).unwrap_or(u16::MAX));
    let text = tui_markdown::from_str_with_options(markdown, &options);
    if links || text.width() > width as usize {
        // An overwide linked table becomes header/value rows. This keeps native
        // SLT links and avoids adding visible destinations before cell wrapping.
        if let Some(header) = rows.first() {
            if rows.len() == 1 {
                for cell in header {
                    draw_markdown(ctx, cell, width);
                }
            }
            for row in rows.iter().skip(1) {
                for (index, cell) in row.iter().enumerate() {
                    if let Some(label) = header.get(index) {
                        ctx.markdown(&format!("**{label}**"));
                    }
                    draw_markdown(ctx, cell, width);
                }
                ctx.text(" ");
            }
        }
        return;
    }
    use ratatui::style::Modifier;
    for line in text.lines {
        if line.spans.is_empty() {
            ctx.text(" ");
        } else {
            ctx.line(|ctx| {
                for span in line.spans {
                    let mut style = Style::new().fg(ctx.theme().text);
                    for (source, target) in [
                        (Modifier::BOLD, slt::Modifiers::BOLD),
                        (Modifier::ITALIC, slt::Modifiers::ITALIC),
                        (Modifier::DIM, slt::Modifiers::DIM),
                        (Modifier::UNDERLINED, slt::Modifiers::UNDERLINE),
                        (Modifier::CROSSED_OUT, slt::Modifiers::STRIKETHROUGH),
                    ] {
                        if span.style.add_modifier.contains(source) {
                            style.modifiers |= target;
                        }
                    }
                    ctx.styled(span.content, style);
                }
            });
        }
    }
}

#[derive(Clone)]
struct TableStyles;

impl tui_markdown::StyleSheet for TableStyles {
    fn code(&self) -> ratatui::style::Style {
        ratatui::style::Style::new()
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
                        parts.push(Part::Markdown(markdown[preceding..start].into()));
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
        parts.push(Part::Markdown(markdown[preceding..].into()));
    }
    Document { parts }
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
    fn outer_report_scroll_reaches_complete_diagram_before_remaining() {
        let mut cache = Cache::default();
        let mut outer = slt::ScrollState::default();
        outer.follow_focus = false;
        outer.anchor = false;
        let mut backend = TestBackend::new(86, 24);
        let markdown = "```mermaid\nflowchart TD\n A[Review source] --> B[Check draft]\n B --> C[Submit answer]\n C --> D[Wait for receipt]\n D --> E[Native acknowledgement]\n```\n\n## Remaining\nEnd of report";
        let geometry = Geometry {
            width: 80,
            height: 8,
        };
        let document = cache.prepare("original-source", "original-revision", markdown, geometry);
        let expected_height = diagram(&document).expect("flowchart renders").height;
        assert!(
            expected_height > 18,
            "fixture must exceed the old nested cap"
        );
        let mut render = |ctx: &mut Context, outer: &mut slt::ScrollState| {
            ctx.scrollable(outer).h(22).col(|ctx| {
                cache.draw(
                    ctx,
                    "original-source",
                    "original-revision",
                    markdown,
                    geometry,
                );
            });
        };
        for _ in 0..3 {
            backend.render(|ctx| render(ctx, &mut outer));
        }
        assert!(
            outer.content_height() >= expected_height,
            "outer scroll must own the full diagram"
        );
        outer.scroll_down(usize::MAX);
        for _ in 0..3 {
            backend.render(|ctx| render(ctx, &mut outer));
        }
        backend.assert_contains("Native acknowledgement");
        backend.assert_contains("End of report");
        let (_, ack) = backend.find_text("Native acknowledgement").unwrap();
        let (_, remaining) = backend.find_text("Remaining").unwrap();
        assert!(
            remaining > ack + 1,
            "closing node must precede the next section"
        );
        let closing: String = (0..86)
            .map(|x| backend.buffer().get(x, ack + 1).symbol.as_str())
            .collect();
        assert!(
            closing.contains('─') || closing.contains('━'),
            "missing closing node: {closing}"
        );
    }

    #[test]
    fn markdown_uses_pane_width_and_keeps_heading_quote_table_and_code_tails() {
        let mut cache = Cache::default();
        let mut backend = TestBackend::new(64, 44);
        let markdown = "## A heading that must wrap completely to its headingtail\n\n> A quote with enough detail to reach the final quotetail\n\n| Check | Evidence |\n| --- | --- |\n| Width | A carefully wrapped decision with complete evidence tail |\n\n```rust\nlet original_source = keep_every_character_and_the_final_suffix;\n```";
        for _ in 0..3 {
            backend.render(|ctx| {
                ctx.container().gap(0).row(|ctx| {
                    cache.draw(
                        ctx,
                        "source",
                        "revision",
                        markdown,
                        Geometry {
                            width: 28,
                            height: 44,
                        },
                    );
                    ctx.container().w(30).col(|ctx| {
                        ctx.text("Adjacent Context");
                    });
                });
            });
        }
        for tail in [
            "headingtail",
            "quotetail",
            "tail",
            "suffix;",
            "Adjacent Context",
        ] {
            backend.assert_contains(tail);
        }
        for y in 1..44 {
            for x in 28..64 {
                assert!(
                    backend.buffer().get(x, y).symbol.trim().is_empty(),
                    "report escaped its pane at {x},{y}"
                );
            }
        }
    }

    #[test]
    fn narrow_linked_table_keeps_native_link_without_duplicate_destination() {
        let mut backend = TestBackend::new(50, 30);
        let markdown = "Read [the guide](https://example.com/guide).\n\n| Check | Evidence |\n| --- | --- |\n| A long decision label | [evidence](https://example.com/check) and the complete sourcetail |";
        for _ in 0..3 {
            backend.render(|ctx| {
                ctx.container().gap(0).row(|ctx| {
                    draw_markdown(ctx, markdown, 24);
                    ctx.container().w(20).col(|ctx| {
                        ctx.text("Adjacent Context");
                    });
                });
            });
        }
        backend.assert_contains("sourcetail");
        backend.assert_not_contains("https://example.com");
        let (x, y) = backend
            .find_text("evidence")
            .expect("linked label remains visible");
        assert_eq!(
            backend.buffer().get(x, y).hyperlink.as_deref(),
            Some("https://example.com/check")
        );
        let (x, y) = backend
            .find_text("the guide")
            .expect("ordinary native link remains visible");
        assert_eq!(
            backend.buffer().get(x, y).hyperlink.as_deref(),
            Some("https://example.com/guide")
        );
        for y in 1..30 {
            for x in 24..50 {
                assert!(
                    backend.buffer().get(x, y).symbol.trim().is_empty(),
                    "linked table escaped its pane at {x},{y}"
                );
            }
        }
    }

    #[test]
    fn bounded_headings_and_quotes_retain_inline_links_and_formatting() {
        let mut backend = TestBackend::new(60, 24);
        let markdown = "## Review [heading link](https://example.com/heading) with complete headingtail\n\n> Quoted **important** context with [quote link](https://example.com/quote) and quotetail";
        for _ in 0..3 {
            backend.render(|ctx| {
                ctx.container().gap(0).row(|ctx| {
                    draw_markdown(ctx, markdown, 28);
                    ctx.container().w(28).col(|ctx| {
                        ctx.text("Adjacent Context");
                    });
                });
            });
        }
        for tail in ["headingtail", "quotetail", "important"] {
            backend.assert_contains(tail);
        }
        backend.assert_not_contains("https://example.com");
        backend.assert_not_contains("**important**");
        for (label, target) in [
            ("heading link", "https://example.com/heading"),
            ("quote link", "https://example.com/quote"),
        ] {
            let (x, y) = backend
                .find_text(label)
                .expect("native inline link remains visible");
            assert_eq!(
                backend.buffer().get(x, y).hyperlink.as_deref(),
                Some(target)
            );
        }
        for y in 1..24 {
            for x in 28..60 {
                assert!(
                    backend.buffer().get(x, y).symbol.trim().is_empty(),
                    "heading or quote escaped pane at {x},{y}"
                );
            }
        }
    }

    #[test]
    fn unsupported_diagram_source_wraps_to_pane_and_retains_unicode_suffix() {
        let mut cache = Cache::default();
        let mut backend = TestBackend::new(60, 32);
        let markdown = "```mermaid\nsequenceDiagram\n Alice->>Bob: Keep_the_complete_original_message_and_sourcetail\n 日本語->>Bob: 🙂 grapheme\n```";
        for _ in 0..3 {
            backend.render(|ctx| {
                ctx.container().gap(0).row(|ctx| {
                    cache.draw(
                        ctx,
                        "source",
                        "revision",
                        markdown,
                        Geometry {
                            width: 28,
                            height: 32,
                        },
                    );
                    ctx.container().w(30).col(|ctx| {
                        ctx.text("Adjacent Context");
                    });
                });
            });
        }
        let visible = (0..32)
            .map(|y| {
                (0..28)
                    .map(|x| backend.buffer().get(x, y).symbol.as_str())
                    .collect::<String>()
                    .trim_end()
                    .to_owned()
            })
            .collect::<String>();
        assert!(visible.contains("sourcetail"), "{visible}");
        backend.assert_contains("日本語");
        backend.assert_contains("🙂");
        for y in 1..32 {
            for x in 28..60 {
                assert!(
                    backend.buffer().get(x, y).symbol.trim().is_empty(),
                    "diagram source escaped its pane at {x},{y}"
                );
            }
        }
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
        backend.assert_contains("env/thread/item @ native-revision");
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
