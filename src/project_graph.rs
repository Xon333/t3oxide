//! Source-qualified project navigation through ascii-dag layout and SLT cells (B1).
use anyhow::{Context as _, Result, ensure};
use ascii_dag::{
    BoxedNode, Direction, Graph, HitResult, LayoutConfig, RenderOptions, ScenePlanner,
    TerminalRenderer,
};
use slt::{Color, Context, KeyCode, Rect, Style};
use std::{collections::HashMap, sync::Arc};
use unicode_segmentation::UnicodeSegmentation;
use unicode_width::UnicodeWidthStr;

/// Labels never participate in navigation identity.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum NodeId {
    Project {
        environment_id: String,
        project_id: String,
    },
    Question {
        environment_id: String,
        project_id: String,
        thread_id: String,
        request_id: String,
    },
    Content {
        environment_id: String,
        project_id: String,
        thread_id: String,
        source_key: String,
    },
}

/// A real project or admitted native question, including projects with no questions.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Node {
    pub id: NodeId,
    pub label: String,
    pub color: Color,
}

/// Caller-supplied navigation relationships; none are inferred from labels.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Edge {
    pub from: NodeId,
    pub to: NodeId,
}

/// Selection is local focus; activation is the explicit navigation action.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct GraphResponse {
    pub selected: Option<NodeId>,
    pub activated: Option<NodeId>,
}

#[derive(Debug)]
struct Canvas {
    nodes: Vec<Node>,
    labels: Vec<String>,
    edges: Vec<Edge>,
    label_width: usize,
    width: usize,
    height: usize,
    ink: Vec<Vec<char>>,
    owners: Vec<usize>,
    bounds: Vec<Rect>,
}

impl Canvas {
    fn build(nodes: &[Node], edges: &[Edge], label_width: usize) -> Result<Self> {
        let mut ids = HashMap::new();
        for (index, node) in nodes.iter().enumerate() {
            let fields = match &node.id {
                NodeId::Project {
                    environment_id,
                    project_id,
                } => vec![environment_id, project_id],
                NodeId::Question {
                    environment_id,
                    project_id,
                    thread_id,
                    request_id,
                } => vec![environment_id, project_id, thread_id, request_id],
                NodeId::Content {
                    environment_id,
                    project_id,
                    thread_id,
                    source_key,
                } => {
                    ensure!(
                        !source_key.is_empty() && source_key.len() <= 16 * 1024,
                        "content navigation needs a complete source key"
                    );
                    // Composite source keys are opaque and may contain the same
                    // separators used by controller maps; they are never labels.
                    vec![environment_id, project_id, thread_id]
                }
            };
            ensure!(
                fields.iter().all(|value| !value.is_empty()
                    && value.len() <= 4096
                    && !value.chars().any(char::is_control)),
                "graph navigation needs complete native source identities"
            );
            ensure!(
                ids.insert(node.id.clone(), index).is_none(),
                "duplicate graph source identity"
            );
        }
        let labels: Vec<_> = nodes
            .iter()
            .map(|node| fit_label(&node.label, label_width))
            .collect();
        // Upstream BoxedNode sizes labels by chars. Reserve display columns with
        // spaces; SLT paints the original graphemes into the resulting geometry.
        let spaces: Vec<_> = labels
            .iter()
            .map(|label| " ".repeat(label.width()))
            .collect();
        let mut graph = Graph::new();
        graph.set_direction(Direction::LeftRight);
        for (index, label) in spaces.iter().enumerate() {
            graph.add_node(index, BoxedNode(label));
        }
        for edge in edges {
            let from = ids
                .get(&edge.from)
                .context("graph edge source is unavailable")?;
            let to = ids
                .get(&edge.to)
                .context("graph edge target is unavailable")?;
            graph.add_edge(*from, *to, None);
        }
        // SLT cells are taller than they are wide. One row separates sibling
        // boxes without spreading a sparse project selector down the viewport.
        let mut config = LayoutConfig::standard();
        config.direction = Direction::LeftRight;
        config.node_spacing = 1;
        let mut layout = graph.compute_layout_with_config(&config);
        let options = RenderOptions::plain();
        let mut planner = ScenePlanner::new();
        let mut scene = planner.plan(&layout, &options.plan).quiet()?;
        // Preserve source ancestry instead of panning a wide parent off-screen
        // merely to fit its selected child. Use the library's vertical layout.
        if !edges.is_empty() && scene.width() > label_width + 4 {
            config.direction = Direction::TopDown;
            layout = graph.compute_layout_with_config(&config);
            scene = planner.plan(&layout, &options.plan).quiet()?;
        }
        let width = scene.width();
        let height = scene.height();
        let cells = width
            .checked_mul(height)
            .context("graph scene geometry overflow")?;
        // Bound retained untrusted inventory geometry; never silently drop nodes.
        ensure!(
            cells <= 1_000_000,
            "project graph exceeds the retained cell budget"
        );
        let mut owners = Vec::with_capacity(cells);
        for y in 0..height {
            for x in 0..width {
                owners.push(match scene.hit_test(x, y) {
                    HitResult::Node(id) => id,
                    _ => usize::MAX,
                });
            }
        }
        let mut text = String::new();
        TerminalRenderer::new(
            &options.emit,
            scene.composition_requirements(&options.compose),
        )
        .render(&scene, &mut text)?;
        let mut bounds = vec![Rect::default(); nodes.len()];
        for node in layout.nodes().iter().filter(|node| node.id < nodes.len()) {
            bounds[node.id] = Rect::new(
                node.x.try_into()?,
                node.y.try_into()?,
                node.width.try_into()?,
                node.height.try_into()?,
            );
        }
        Ok(Self {
            nodes: nodes.to_vec(),
            labels,
            edges: edges.to_vec(),
            label_width,
            width,
            height,
            ink: text.lines().map(|line| line.chars().collect()).collect(),
            owners,
            bounds,
        })
    }

    fn hit(&self, x: usize, y: usize) -> Option<&NodeId> {
        if x >= self.width || y >= self.height {
            return None;
        }
        self.nodes
            .get(self.owners[y * self.width + x])
            .map(|node| &node.id)
    }
}

#[derive(Debug)]
struct Displayed {
    canvas: Arc<Canvas>,
    left: usize,
    top: usize,
}

/// One persistent navigation region; level/content ownership stays with its caller.
#[derive(Debug, Default)]
pub struct ProjectGraph {
    canvas: Option<Arc<Canvas>>,
    displayed: Option<Displayed>,
    selected: Option<NodeId>,
    external_selected: Option<NodeId>,
    left: usize,
    top: usize,
}

impl ProjectGraph {
    /// Draw a bounded viewport. Tab enters the graph; arrows select, Enter/Space
    /// or a click activates. Use the same instance when replacing a graph level.
    /// Pass its allocated inner width explicitly; SLT's context width is global.
    ///
    /// # Errors
    /// Rejects duplicate identities, missing edge endpoints or oversized geometry.
    pub fn draw(
        &mut self,
        ctx: &mut Context,
        nodes: &[Node],
        edges: &[Edge],
        selected: Option<&NodeId>,
        width: u32,
        height: u32,
    ) -> Result<GraphResponse> {
        let width = width.max(1);
        let height = height.max(1);
        let label_width = width.saturating_sub(4).max(1) as usize;
        let changed = self.canvas.as_ref().is_none_or(|canvas| {
            canvas.nodes != nodes || canvas.edges != edges || canvas.label_width != label_width
        });
        if changed {
            self.canvas = Some(Arc::new(Canvas::build(nodes, edges, label_width)?));
            self.left = 0;
            self.top = 0;
        }
        let selection_changed = self.external_selected.as_ref() != selected;
        if selection_changed {
            self.selected = selected.cloned();
            self.external_selected = selected.cloned();
        }
        if !nodes
            .iter()
            .any(|node| Some(&node.id) == self.selected.as_ref())
        {
            self.selected = selected
                .filter(|id| nodes.iter().any(|node| &node.id == *id))
                .cloned()
                .or_else(|| nodes.first().map(|node| node.id.clone()));
        }
        let focused = ctx.register_focusable_named("project-navigation");
        ctx.register_focusable();
        let mut activated = None;
        let mut moved = changed || selection_changed;
        if focused && !nodes.is_empty() {
            let previous = ctx.consume_key_code(KeyCode::Up) || ctx.consume_key_code(KeyCode::Left);
            let next = ctx.consume_key_code(KeyCode::Down) || ctx.consume_key_code(KeyCode::Right);
            if previous || next {
                let index = nodes
                    .iter()
                    .position(|node| Some(&node.id) == self.selected.as_ref())
                    .unwrap_or(0);
                let index = if previous {
                    (index + nodes.len() - 1) % nodes.len()
                } else {
                    (index + 1) % nodes.len()
                };
                self.selected = Some(nodes[index].id.clone());
                moved = true;
            }
            if ctx.consume_key_code(KeyCode::Enter) || ctx.consume_key(' ') {
                activated = self.selected.clone();
            }
        }
        let canvas = self
            .canvas
            .as_ref()
            .context("graph canvas is unavailable")?
            .clone();
        if moved
            && let Some(index) = nodes
                .iter()
                .position(|node| Some(&node.id) == self.selected.as_ref())
        {
            let mut bounds = canvas.bounds[index];
            if let Some(parent) = edges.iter().find(|edge| edge.to == nodes[index].id)
                && let Some(parent) = nodes.iter().position(|node| node.id == parent.from)
            {
                let ancestor = canvas.bounds[parent];
                let x = bounds.x.min(ancestor.x);
                let y = bounds.y.min(ancestor.y);
                let right = (bounds.x + bounds.width).max(ancestor.x + ancestor.width);
                let bottom = (bounds.y + bounds.height).max(ancestor.y + ancestor.height);
                if right - x <= width && bottom - y <= height {
                    bounds = Rect::new(x, y, right - x, bottom - y);
                }
            }
            self.left = reveal(
                self.left,
                bounds.x as usize,
                bounds.width as usize,
                width as usize,
            );
            self.top = reveal(
                self.top,
                bounds.y as usize,
                bounds.height as usize,
                height as usize,
            );
        }
        self.left = self.left.min(canvas.width.saturating_sub(width as usize));
        self.top = self.top.min(canvas.height.saturating_sub(height as usize));
        let (left, top) = (self.left, self.top);
        let pointer = ctx.mouse_down();
        let selection = self.selected.as_ref();
        let muted = ctx.theme().text_dim;
        let mut buffer = slt::Buffer::empty(Rect::new(0, 0, width, height));
        for (y, row) in canvas
            .ink
            .iter()
            .enumerate()
            .skip(top)
            .take(height as usize)
        {
            for (x, ch) in row.iter().enumerate().skip(left).take(width as usize) {
                let style = canvas
                    .owners
                    .get(y * canvas.width + x)
                    .and_then(|owner| nodes.get(*owner))
                    .map_or(Style::new().fg(muted), |node| {
                        node_style(node, selection, focused)
                    });
                buffer.set_char((x - left) as u32, (y - top) as u32, *ch, style);
            }
        }
        for (index, node) in nodes.iter().enumerate() {
            let bounds = canvas.bounds[index];
            let y = bounds.y as usize + 1;
            if y < top || y >= top + height as usize {
                continue;
            }
            let mut x = bounds.x as usize + 2;
            for grapheme in canvas.labels[index].graphemes(true) {
                let columns = grapheme.width();
                if x >= left && x + columns <= left + width as usize {
                    buffer.set_string(
                        (x - left) as u32,
                        (y - top) as u32,
                        grapheme,
                        node_style(node, selection, focused),
                    );
                }
                x += columns;
            }
        }
        let response = ctx
            .container()
            .w(width)
            .h(height)
            .gap(0)
            .bubble_clicks()
            .col(|ctx| {
                for y in 0..height {
                    ctx.container().w(width).h(1).line(|ctx| {
                        let mut run = String::new();
                        let mut style = buffer.get(0, y).style;
                        for x in 0..width {
                            let cell = buffer.get(x, y);
                            if cell.is_continuation() {
                                continue;
                            }
                            if cell.style != style {
                                ctx.styled(std::mem::take(&mut run), style);
                                style = cell.style;
                            }
                            run.push_str(&cell.symbol);
                        }
                        if !run.is_empty() {
                            ctx.styled(run, style);
                        }
                    });
                }
            });
        // Both the hit rectangle and canvas are the ones actually displayed on
        // the previous frame. Reordering cannot retarget a click by position.
        if response.clicked
            && let (Some((x, y)), Some(displayed)) = (pointer, &self.displayed)
            && response.rect.contains(x, y)
        {
            let x = (x - response.rect.x) as usize + displayed.left;
            let y = (y - response.rect.y) as usize + displayed.top;
            if let Some(id) = displayed
                .canvas
                .hit(x, y)
                .filter(|id| nodes.iter().any(|node| &node.id == *id))
            {
                self.selected = Some(id.clone());
                activated = Some(id.clone());
                ctx.request_frame();
            }
        }
        if response.hovered {
            if ctx.scroll_up() {
                self.top = self.top.saturating_sub(3);
            }
            if ctx.scroll_down() {
                self.top = self.top.saturating_add(3);
            }
            if ctx.scroll_left() {
                self.left = self.left.saturating_sub(3);
            }
            if ctx.scroll_right() {
                self.left = self.left.saturating_add(3);
            }
            if self.top != top || self.left != left {
                ctx.request_frame();
            }
        }
        self.displayed = Some(Displayed { canvas, left, top });
        Ok(GraphResponse {
            selected: self.selected.clone(),
            activated,
        })
    }
}

fn fit_label(label: &str, width: usize) -> String {
    let text: String = label
        .chars()
        .map(|ch| if ch.is_control() { ' ' } else { ch })
        .collect();
    if text.width() <= width {
        return text;
    }
    let mut output = String::new();
    let mut columns = 0;
    for grapheme in text.graphemes(true) {
        if columns + grapheme.width() >= width {
            break;
        }
        output.push_str(grapheme);
        columns += grapheme.width();
    }
    output.push('…');
    output
}

fn reveal(offset: usize, start: usize, length: usize, viewport: usize) -> usize {
    if start < offset {
        start
    } else if start + length > offset + viewport {
        (start + length).saturating_sub(viewport)
    } else {
        offset
    }
}

fn node_style(node: &Node, selected: Option<&NodeId>, focused: bool) -> Style {
    let style = Style::new().fg(node.color);
    if selected == Some(&node.id) {
        let style = style.bg(slt::palette::tailwind::SLATE.c800);
        if focused { style.bold() } else { style }
    } else {
        style
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use slt::{EventBuilder, TestBackend};

    fn node(environment: &str, project: &str, label: &str) -> Node {
        Node {
            id: NodeId::Project {
                environment_id: environment.into(),
                project_id: project.into(),
            },
            label: label.into(),
            color: slt::palette::tailwind::CYAN.c300,
        }
    }

    #[test]
    fn sparse_projects_share_compact_paint_and_pointer_geometry() {
        let nodes = [node("env", "a", "Alpha"), node("env", "b", "Beta")];
        let mut graph = ProjectGraph::default();
        let mut backend = TestBackend::new(30, 8);
        for _ in 0..3 {
            backend.render(|ctx| {
                graph.draw(ctx, &nodes, &[], None, 30, 8).unwrap();
            });
        }
        let canvas = graph.canvas.as_ref().unwrap();
        assert_eq!(
            canvas.bounds[1].y - (canvas.bounds[0].y + canvas.bounds[0].height),
            1
        );
        backend.assert_contains("Alpha");
        backend.assert_contains("Beta");
        let (x, y) = backend.find_text("Beta").unwrap();
        let mut response = GraphResponse::default();
        backend.run_with_events(EventBuilder::new().click(x, y).build(), |ctx| {
            response = graph.draw(ctx, &nodes, &[], None, 30, 8).unwrap();
        });
        assert_eq!(response.activated, Some(nodes[1].id.clone()));
    }

    #[test]
    fn wide_labels_use_library_hit_geometry_and_clicks_keep_prior_frame_source_identity() {
        let nodes = [
            node("environment-a", "project-a", "日本語 🚀"),
            node("environment-b", "project-a", "日本語 🚀"),
        ];
        let mut graph = ProjectGraph::default();
        let mut backend = TestBackend::new(32, 14);
        backend.render(|ui| {
            graph.draw(ui, &nodes, &[], None, 32, 14).unwrap();
        });
        backend.assert_contains("日本語 🚀");
        let (x, y) = backend
            .find_text("日")
            .expect("original Unicode label rendered");
        let reversed = [nodes[1].clone(), nodes[0].clone()];
        let mut response = GraphResponse::default();
        backend.run_with_events(EventBuilder::new().click(x + 1, y).build(), |ui| {
            response = graph.draw(ui, &reversed, &[], None, 32, 14).unwrap();
        });
        assert_eq!(
            response.activated,
            Some(nodes[0].id.clone()),
            "the wide continuation cell and reordered scene must retain the displayed source"
        );
        assert_eq!(response.selected, Some(nodes[0].id.clone()));
        let canvas = graph.canvas.as_ref().unwrap();
        let label_width = canvas.labels[0].width();
        assert_eq!(canvas.bounds[0].width as usize, label_width + 4);
    }

    #[test]
    fn graph_respects_opaque_modal_and_retains_selected_wide_label_style() {
        let nodes = [node("environment-a", "project-a", "日本語 🚀")];
        let mut graph = ProjectGraph::default();
        let mut backend = TestBackend::new(32, 14);
        for modal in [false, true, false] {
            for _ in 0..2 {
                backend.render(|ctx| {
                    graph.draw(ctx, &nodes, &[], None, 32, 14).unwrap();
                    if modal {
                        ctx.modal(|ctx| {
                            ctx.container().w(32).h(14).bg(Color::Blue).col(|ctx| {
                                ctx.text("Opaque modal");
                            });
                        });
                    }
                });
            }
            if modal {
                backend.assert_contains("Opaque modal");
                backend.assert_not_contains("日本語 🚀");
            } else {
                backend.assert_contains("日本語 🚀");
                let (x, y) = backend.find_text("日").unwrap();
                for x in x..x + 2 {
                    let cell = backend.buffer().get(x, y);
                    assert_eq!(cell.style.bg, Some(slt::palette::tailwind::SLATE.c800));
                    assert_eq!(cell.style.fg, Some(nodes[0].color));
                }
            }
        }
    }

    #[test]
    fn tab_enters_graph_arrows_reveal_nodes_and_empty_level_stays_valid() {
        let nodes = [
            node("environment-a", "a", "日本語日本語"),
            node("environment-a", "b", "Choose target"),
        ];
        let mut graph = ProjectGraph::default();
        let mut backend = TestBackend::new(40, 6);
        backend.render(|ui| {
            ui.button("Outside").show();
            graph.draw(ui, &nodes, &[], None, 9, 5).unwrap();
        });
        backend.assert_contains("日本…");
        let mut response = GraphResponse::default();
        backend.run_with_events(EventBuilder::new().key_code(KeyCode::Tab).build(), |ui| {
            ui.button("Outside").show();
            graph.draw(ui, &nodes, &[], None, 9, 5).unwrap();
        });
        backend.run_with_events(
            EventBuilder::new()
                .key_code(KeyCode::Down)
                .key_code(KeyCode::Enter)
                .build(),
            |ui| {
                ui.button("Outside").show();
                response = graph.draw(ui, &nodes, &[], None, 9, 5).unwrap();
            },
        );
        assert_eq!(response.activated, Some(nodes[1].id.clone()));
        assert_eq!(response.selected, Some(nodes[1].id.clone()));
        backend.render(|ui| {
            response = graph.draw(ui, &[], &[], None, 9, 5).unwrap();
        });
        assert_eq!(response, GraphResponse::default());
    }
}
