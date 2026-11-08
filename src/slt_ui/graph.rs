//! A frozen loaded-lineage view; closing leaves its invoking surface in place.
use super::{Action, AttentionItem, ContentItem, Source, SurfaceState, action_bar, attention};
use crate::{
    domain,
    project_graph::{Edge, Node, NodeId, ProjectGraph},
};
use slt::{Border, Context, KeyCode, ListState, ScrollState, palette::tailwind};
use std::collections::HashMap;

#[derive(Clone, Debug)]
pub(crate) struct Thread {
    pub source: Source,
    pub title: String,
    pub relationship: String,
    pub parent: Option<String>,
}

#[derive(Clone, Debug)]
enum Target {
    Project(Source),
    Thread(Thread),
    Question(AttentionItem),
    Content(ContentItem),
}

impl Target {
    fn source(&self) -> &Source {
        match self {
            Self::Project(source) => source,
            Self::Thread(thread) => &thread.source,
            Self::Question(question) => &question.source,
            Self::Content(content) => &content.source,
        }
    }

    fn openable(&self) -> bool {
        match self {
            Self::Thread(thread) => matches!(thread.relationship.as_str(), "main" | "fork"),
            Self::Question(_) | Self::Content(_) => true,
            Self::Project(_) => false,
        }
    }
}

#[derive(Debug)]
struct View {
    nodes: Vec<Node>,
    edges: Vec<Edge>,
    targets: HashMap<NodeId, Target>,
    selected: Option<NodeId>,
    scope: Option<Source>,
    coverage: String,
    fallback: ListState,
}

#[derive(Debug, Default)]
pub(super) struct State {
    view: Option<View>,
    renderer: ProjectGraph,
    return_focus: Option<String>,
    tools: usize,
    detail: ScrollState,
    status: String,
    controller_notice: String,
    reading: Option<super::GraphReading>,
    geometry: Option<(u32, u32)>,
}

impl SurfaceState {
    pub(crate) fn complete_graph_open(&mut self) {
        self.graph.view = None;
        self.graph.reading = None;
        self.graph.return_focus = Some("composer".into());
    }

    pub(crate) fn graph_open_error(&mut self, message: String) {
        self.graph.status = message;
        self.graph.detail = ScrollState::new();
    }
}

pub(super) fn is_open(state: &SurfaceState) -> bool {
    state.graph.view.is_some()
}

pub(super) fn restore_focus(ctx: &mut Context, state: &mut SurfaceState) {
    if let Some(name) = state.graph.return_focus.take() {
        ctx.focus_by_name(&name);
        ctx.request_frame();
    }
}

fn project_id(source: &Source) -> NodeId {
    NodeId::Project {
        environment_id: source.environment.clone(),
        project_id: source.project_id.clone(),
    }
}

fn thread_id(source: &Source) -> NodeId {
    NodeId::Thread {
        profile: source.profile,
        environment_id: source.environment.clone(),
        project_id: source.project_id.clone(),
        thread_id: source.thread.clone(),
    }
}

fn valid_source(source: &Source) -> bool {
    domain::valid_id(&source.environment) && domain::valid_id(&source.project_id)
}

fn snapshot(
    state: &SurfaceState,
    scope: Option<Source>,
    selected: Option<NodeId>,
    invoking: Option<&ContentItem>,
) -> View {
    let mut view = View {
        nodes: Vec::new(),
        edges: Vec::new(),
        targets: HashMap::new(),
        selected,
        scope,
        coverage: state.graph_coverage.clone(),
        fallback: ListState::default(),
    };
    let in_scope = |source: &Source| {
        view.scope
            .as_ref()
            .is_none_or(|scope| source.project_key() == scope.project_key())
    };
    let mut skipped = 0;
    for source in state.projects.iter().filter(|source| in_scope(source)) {
        if !valid_source(source) {
            skipped += 1;
            continue;
        }
        let id = project_id(source);
        if view.targets.contains_key(&id) {
            continue;
        }
        view.nodes.push(Node {
            id: id.clone(),
            label: format!("Project · {}", source.project),
            color: tailwind::CYAN.c300,
        });
        view.targets.insert(id, Target::Project(source.clone()));
    }
    for thread in state
        .graph_threads
        .iter()
        .filter(|thread| in_scope(&thread.source))
    {
        let source = &thread.source;
        if !valid_source(source) || !domain::valid_id(&source.thread) {
            skipped += 1;
            continue;
        }
        let id = thread_id(source);
        if view.targets.contains_key(&id) {
            skipped += 1;
            continue;
        }
        let (kind, color) = match thread.relationship.as_str() {
            "main" => ("Conversation", tailwind::EMERALD.c300),
            "fork" => ("Fork", tailwind::BLUE.c300),
            "subagent" => ("Subagent · read only", tailwind::AMBER.c300),
            _ => ("Unknown lineage · read only", tailwind::SLATE.c300),
        };
        view.nodes.push(Node {
            id: id.clone(),
            label: format!("{kind} · {}", thread.title),
            color,
        });
        let project = project_id(source);
        if view.targets.contains_key(&project) {
            view.edges.push(Edge {
                from: project,
                to: id.clone(),
            });
        }
        view.targets.insert(id, Target::Thread(thread.clone()));
    }
    let mut missing_parents = 0;
    for node in &view.nodes {
        let Some(Target::Thread(child)) = view.targets.get(&node.id) else {
            continue;
        };
        if !matches!(child.relationship.as_str(), "fork" | "subagent") {
            continue;
        }
        let Some(parent) = &child.parent else {
            missing_parents += 1;
            continue;
        };
        let parent = view.nodes.iter().find(|node| matches!(&node.id,
            NodeId::Thread { profile, environment_id, thread_id, .. }
                if *profile == child.source.profile && environment_id == &child.source.environment && thread_id == parent));
        if let Some(parent) = parent {
            view.edges.push(Edge {
                from: parent.id.clone(),
                to: node.id.clone(),
            });
        } else {
            missing_parents += 1;
        }
    }
    for question in state.questions.iter().filter(|q| in_scope(&q.source)) {
        let Some(request) = question.request["id"]
            .as_str()
            .filter(|id| domain::valid_id(id))
        else {
            skipped += 1;
            continue;
        };
        if !valid_source(&question.source) || !domain::valid_id(&question.source.thread) {
            skipped += 1;
            continue;
        }
        let id = NodeId::Question {
            environment_id: question.source.environment.clone(),
            project_id: question.source.project_id.clone(),
            thread_id: question.source.thread.clone(),
            request_id: request.into(),
        };
        if view.targets.contains_key(&id) {
            skipped += 1;
            continue;
        }
        view.nodes.push(Node {
            id: id.clone(),
            label: format!("Form · {}", question.title),
            color: tailwind::ROSE.c300,
        });
        let owner = thread_id(&question.source);
        if view.targets.contains_key(&owner) {
            view.edges.push(Edge {
                from: owner,
                to: id.clone(),
            });
        }
        view.targets.insert(id, Target::Question(question.clone()));
    }
    for content in invoking
        .into_iter()
        .chain(state.contents.iter())
        .filter(|c| in_scope(&c.source))
    {
        if !valid_source(&content.source)
            || !domain::valid_id(&content.source.thread)
            || content.key.is_empty()
        {
            skipped += 1;
            continue;
        }
        let id = NodeId::Content {
            environment_id: content.source.environment.clone(),
            project_id: content.source.project_id.clone(),
            thread_id: content.source.thread.clone(),
            source_key: content.key.clone(),
        };
        if view.targets.contains_key(&id) {
            skipped += 1;
            continue;
        }
        view.nodes.push(Node {
            id: id.clone(),
            label: format!("Library · {}", content.title),
            color: tailwind::VIOLET.c300,
        });
        let owner = thread_id(&content.source);
        if view.targets.contains_key(&owner) {
            view.edges.push(Edge {
                from: owner,
                to: id.clone(),
            });
        }
        view.targets.insert(id, Target::Content(content.clone()));
    }
    view.coverage.push_str(&format!("\n{missing_parents} parents not loaded in this view; {skipped} invalid/duplicate records excluded. Project edges mean membership; other edges mean the labelled child relation or source ownership."));
    if !view
        .targets
        .keys()
        .any(|id| Some(id) == view.selected.as_ref())
    {
        view.selected = view.nodes.first().map(|node| node.id.clone());
    }
    view.fallback
        .set_items(view.nodes.iter().map(|node| node.label.clone()).collect());
    view
}

pub(super) fn open(ctx: &mut Context, state: &mut SurfaceState, view: &crate::ui::View) {
    if is_open(state) {
        return;
    }
    let source = if state.attention_mode {
        attention::selected_source(state).cloned().or_else(|| {
            state
                .projects
                .iter()
                .find(|source| Some(&source.project_key()) == state.selected_project.as_ref())
                .cloned()
        })
    } else {
        state.source.clone()
    };
    let selected = state
        .attention_mode
        .then(|| attention::selected_content(state))
        .flatten()
        .map(|content| NodeId::Content {
            environment_id: content.source.environment.clone(),
            project_id: content.source.project_id.clone(),
            thread_id: content.source.thread.clone(),
            source_key: content.key.clone(),
        })
        .or_else(|| {
            source.as_ref().map(|source| {
                if source.thread.is_empty() {
                    project_id(source)
                } else {
                    thread_id(source)
                }
            })
        });
    state.graph.reading = if state.attention_mode {
        None
    } else {
        state.source.clone().map(|source| {
            let anchor = state.transcript_layout.as_ref().and_then(|layout| {
                let (key, start, _) = layout
                    .items
                    .iter()
                    .rev()
                    .find(|(_, start, _)| *start <= layout.top)?;
                let message = view.messages.iter().find(|message| &message.key == key)?;
                Some((
                    key.clone(),
                    crate::reports::content_revision(&message.text),
                    layout.top.saturating_sub(*start),
                ))
            });
            super::GraphReading {
                source,
                anchor,
                follow: view.follow,
            }
        })
    };
    state.graph.geometry = Some((ctx.width(), ctx.height()));
    let invoking = state
        .attention_mode
        .then(|| attention::selected_content(state))
        .flatten();
    state.graph.view = Some(snapshot(state, source, selected, invoking));
    // Entry precedes this frame's controls, so use their settled named owner.
    state.graph.return_focus = state.focused_control.clone();
    state.graph.renderer = ProjectGraph::default();
    state.graph.renderer.center();
    state.graph.detail = ScrollState::new();
    state.graph.status.clear();
    ctx.focus_by_name("graph-view");
    ctx.request_frame();
}

fn detail(view: &View) -> String {
    let mut text = String::new();
    if let Some(target) = view.selected.as_ref().and_then(|id| view.targets.get(id)) {
        let source = target.source();
        text = format!(
            "Environment: {} · profile {}\nProject: {}\nThread: {}",
            source.environment, source.profile, source.project_id, source.thread
        );
        match target {
            Target::Thread(thread) => text.push_str(&format!(
                "\nRelationship: {}\nParent: {}\n{}",
                thread.relationship,
                thread.parent.as_deref().unwrap_or("not supplied"),
                if target.openable() {
                    "Open uses the current exact native source."
                } else {
                    "Read only: child ownership/work detail is not admitted."
                }
            )),
            Target::Question(question) => text.push_str(&format!(
                "\nRequest: {}\nOpen selects this form in B; it does not answer or submit.",
                domain::string(&question.request, "id")
            )),
            Target::Content(content) => text.push_str(&format!(
                "\nSource: {}\nRevision: {}\nOpen selects this exact revision in B.",
                content.key, content.revision
            )),
            Target::Project(_) => {
                text.push_str("\nProject membership; no conversation is opened by selection.")
            }
        }
    }
    format!(
        "{text}\n{}\nArrows select · Shift+arrows/wheel pan · Center reveals selection · Fit never scales text · Enter opens · Esc returns. Refresh reads the currently loaded inventory only.",
        view.coverage
    )
}

pub(super) fn draw(
    ctx: &mut Context,
    state: &mut SurfaceState,
    notice: &str,
    actions: &mut Vec<Action>,
) {
    let Some(mut view) = state.graph.view.take() else {
        return;
    };
    let (width, height) = (ctx.width(), ctx.height());
    if state.graph.controller_notice != notice {
        state.graph.controller_notice = notice.into();
        state.graph.detail = ScrollState::new();
    }
    let inner_width = width.saturating_sub(2).max(1);
    let detail_rows = if height < 16 { 2 } else { 4 };
    let tool_rows = if inner_width < 44 { 2 } else { 1 };
    let chart_height = height.saturating_sub(3 + detail_rows + tool_rows).max(1);
    let mut control = None;
    let mut activated = None;
    ctx.modal(|ctx| {
        let background = ctx.theme().bg;
        ctx.container()
            .w(width)
            .h(height)
            .bg(background)
            .border(Border::Rounded)
            .title("Graph · loaded / partial")
            .gap(0)
            .col(|ctx| {
                let enabled = view
                    .selected
                    .as_ref()
                    .and_then(|id| view.targets.get(id))
                    .is_some_and(Target::openable);
                control = action_bar(
                    ctx,
                    "graph-tools",
                    &mut state.graph.tools,
                    &[
                        ("Open", false, enabled),
                        ("Center", false, true),
                        ("Fit", false, true),
                        ("All", view.scope.is_none(), true),
                        ("Refresh", false, true),
                        ("Close", false, true),
                    ],
                );
                match state.graph.renderer.draw_view(
                    ctx,
                    &view.nodes,
                    &view.edges,
                    view.selected.as_ref(),
                    inner_width,
                    chart_height,
                ) {
                    Ok(response) => {
                        if view.selected != response.selected {
                            state.graph.detail = ScrollState::new();
                        }
                        view.selected = response.selected;
                        activated = response.activated;
                    }
                    Err(error) => {
                        state.graph.status =
                            format!("Layout unavailable: {error}; showing node list.");
                        let focused = ctx.register_focusable_named("graph-view");
                        ctx.container().h(chart_height).col(|ctx| {
                            ctx.list(&mut view.fallback);
                        });
                        view.selected = view
                            .fallback
                            .selected_index()
                            .and_then(|i| view.nodes.get(i))
                            .map(|node| node.id.clone());
                        if focused && ctx.consume_key_code(KeyCode::Enter) {
                            activated = view.selected.clone();
                        }
                    }
                }
                let details_focused = ctx.register_focusable_named("graph-detail");
                state.graph.detail.follow_focus = false;
                let keys: Vec<_> = ctx
                    .key_presses_when(details_focused)
                    .map(|(i, key)| (i, key.code.clone()))
                    .collect();
                for (index, key) in keys {
                    match key {
                        KeyCode::Up => state.graph.detail.scroll_up(1),
                        KeyCode::Down => state.graph.detail.scroll_down(1),
                        KeyCode::PageUp => state.graph.detail.scroll_up(detail_rows as usize),
                        KeyCode::PageDown => state.graph.detail.scroll_down(detail_rows as usize),
                        KeyCode::Home => state.graph.detail.offset = 0,
                        KeyCode::End => state.graph.detail.offset = state.graph.detail.max_offset(),
                        _ => continue,
                    }
                    ctx.consume_event(index);
                }
                ctx.scrollable(&mut state.graph.detail)
                    .h(detail_rows)
                    .col(|ctx| {
                        let status = if state.graph.status.is_empty() {
                            String::new()
                        } else {
                            format!("{}\n", state.graph.status)
                        };
                        let feedback = if notice.is_empty() {
                            String::new()
                        } else {
                            format!("{notice}\n")
                        };
                        ctx.text(format!("{feedback}{status}{}", detail(&view)))
                            .wrap();
                    });
                ctx.text("Shift+arrows pan · Tab details/tools · Esc return")
                    .dim()
                    .truncate();
            });
        if ctx.consume_key_code(KeyCode::Esc) {
            control = Some(5);
        }
    });
    match control {
        Some(0) => activated = view.selected.clone(),
        Some(1) => state.graph.renderer.center(),
        Some(2) => {
            state.graph.status = if state.graph.renderer.fit(inner_width, chart_height) {
                "Whole graph fits at current cell density.".into()
            } else {
                "Graph exceeds viewport; centered selection. Zoom is unavailable.".into()
            };
        }
        Some(3) | Some(4) => {
            let scope = if control == Some(3) {
                None
            } else {
                view.scope.clone()
            };
            view = snapshot(state, scope, view.selected.clone(), None);
            state.graph.status.clear();
            state.graph.detail = ScrollState::new();
        }
        Some(5) => {
            let resized = state.graph.geometry != Some((width, height));
            if let Some(mut reading) = state.graph.reading.take() {
                if resized && let Some((_, _, inner)) = &mut reading.anchor {
                    *inner = 0;
                }
                actions.push(Action::GraphReturn(reading));
            } else if state.attention_mode && resized {
                attention::reset_reading_after_graph_resize(state);
            }
            restore_focus(ctx, state);
            ctx.request_frame();
            return;
        }
        _ => {}
    }
    if let Some(id) = activated {
        match view.targets.get(&id) {
            Some(Target::Thread(thread))
                if matches!(thread.relationship.as_str(), "main" | "fork") =>
            {
                actions.push(Action::GraphOpenSource(thread.source.clone()))
            }
            Some(target @ (Target::Question(_) | Target::Content(_))) => {
                let fresh = match target {
                    Target::Question(original) => state.questions.iter().any(|current| {
                        current.key == original.key
                            && current.source == original.source
                            && current.request == original.request
                    }),
                    Target::Content(original) => state.contents.iter().any(|current| {
                        current.key == original.key
                            && current.source == original.source
                            && current.revision == original.revision
                    }),
                    _ => false,
                };
                if fresh && attention::open_graph_item(state, &id, actions) {
                    state.graph.reading = None;
                    state.attention_mode = true;
                    state.graph.return_focus = Some("attention-content".into());
                    restore_focus(ctx, state);
                    ctx.request_frame();
                    return;
                }
                state.graph.status = "Source or revision changed; refresh before opening.".into();
            }
            _ => state.graph.status = "Selection is read only; no conversation opened.".into(),
        }
    }
    state.graph.view = Some(view);
    if control.is_some() {
        ctx.request_frame();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn source(profile: usize, environment: &str, thread: &str) -> Source {
        Source {
            profile,
            environment: environment.into(),
            project_id: "project".into(),
            project: "Same project".into(),
            thread: thread.into(),
            ..Source::default()
        }
    }

    #[test]
    fn full_graph_snapshot_uses_exact_loaded_parents_and_keeps_empty_projects() {
        let mut state = SurfaceState::new();
        state.projects = vec![source(0, "env-a", ""), source(1, "env-b", "")];
        state.projects.push(Source {
            project_id: "empty".into(),
            ..source(0, "env-a", "")
        });
        for (profile, environment) in [(0, "env-a"), (1, "env-b")] {
            state.graph_threads.push(Thread {
                source: source(profile, environment, "root"),
                title: "Same title".into(),
                relationship: "main".into(),
                parent: None,
            });
            state.graph_threads.push(Thread {
                source: source(profile, environment, "child"),
                title: "Same title".into(),
                relationship: "fork".into(),
                parent: Some("root".into()),
            });
        }
        state.graph_threads.push(Thread {
            source: source(0, "env-a", "missing"),
            title: "Missing parent".into(),
            relationship: "subagent".into(),
            parent: Some("unloaded".into()),
        });
        state.graph_threads.push(Thread {
            source: source(0, "env-a", "unknown"),
            title: "Unknown".into(),
            relationship: "unknown".into(),
            parent: Some("root".into()),
        });
        let view = snapshot(&state, None, None, None);
        assert!(view.targets.contains_key(&project_id(&state.projects[2])));
        for edge in &view.edges {
            if let (
                NodeId::Thread {
                    profile: a,
                    environment_id: x,
                    ..
                },
                NodeId::Thread {
                    profile: b,
                    environment_id: y,
                    ..
                },
            ) = (&edge.from, &edge.to)
            {
                assert_eq!((a, x), (b, y));
            }
        }
        assert_eq!(
            view.edges
                .iter()
                .filter(|e| matches!(e.from, NodeId::Thread { .. }))
                .count(),
            2
        );
        assert!(view.coverage.contains("1 parents not loaded"));
        assert!(
            !view
                .targets
                .get(&thread_id(&source(0, "env-a", "missing")))
                .unwrap()
                .openable()
        );
        assert!(
            !view
                .targets
                .get(&thread_id(&source(0, "env-a", "unknown")))
                .unwrap()
                .openable()
        );
        let scoped = snapshot(&state, Some(source(1, "env-b", "root")), None, None);
        assert!(
            scoped
                .targets
                .values()
                .all(|target| target.source().environment == "env-b")
        );
    }

    #[test]
    fn full_graph_entry_retains_the_invoking_library_revision_until_explicit_refresh() {
        let mut state = SurfaceState::new();
        state.attention_mode = true;
        state.projects = vec![source(0, "env-a", "")];
        let original = ContentItem {
            key: "original-content".into(),
            source: source(0, "env-a", "root"),
            title: "Original report".into(),
            revision: "original-revision".into(),
            ..ContentItem::default()
        };
        state.contents.push(original.clone());
        let id = NodeId::Content {
            environment_id: "env-a".into(),
            project_id: "project".into(),
            thread_id: "root".into(),
            source_key: original.key.clone(),
        };
        assert!(attention::open_graph_item(&mut state, &id, &mut Vec::new()));
        state.contents[0].revision = "replacement-revision".into();
        state.contents[0].title = "Replacement report".into();
        let mut backend = slt::TestBackend::new(70, 22);
        backend.render(|ctx| open(ctx, &mut state, &crate::ui::View::default()));
        assert!(
            matches!(state.graph.view.as_ref().unwrap().targets.get(&id),
            Some(Target::Content(content)) if content == &original)
        );
        for _ in 0..3 {
            backend.render(|ctx| draw(ctx, &mut state, "", &mut Vec::new()));
        }
        let (x, y) = backend.find_text("Open").unwrap();
        let mut actions = Vec::new();
        backend.run_with_events(slt::EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &mut state, "", &mut actions)
        });
        assert!(is_open(&state));
        assert!(state.graph.status.contains("revision changed"));
        assert_eq!(state.selected_content(), Some(&original));
        assert!(actions.is_empty());
        let (x, y) = backend.find_text("Refresh").unwrap();
        backend.run_with_events(slt::EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &mut state, "", &mut actions)
        });
        assert!(
            matches!(state.graph.view.as_ref().unwrap().targets.get(&id),
            Some(Target::Content(content)) if content.revision == "replacement-revision")
        );
    }

    #[test]
    fn full_graph_stale_library_revision_stays_open_and_does_not_select_replacement() {
        let mut state = SurfaceState::new();
        state.projects = vec![source(0, "env-a", "")];
        let content = ContentItem {
            key: "original-content".into(),
            source: source(0, "env-a", "root"),
            title: "Report".into(),
            revision: "original-revision".into(),
            ..ContentItem::default()
        };
        state.contents.push(content.clone());
        let id = NodeId::Content {
            environment_id: "env-a".into(),
            project_id: "project".into(),
            thread_id: "root".into(),
            source_key: content.key.clone(),
        };
        state.graph.view = Some(snapshot(&state, None, Some(id), None));
        let mut backend = slt::TestBackend::new(70, 22);
        for _ in 0..3 {
            backend.render(|ctx| draw(ctx, &mut state, "", &mut Vec::new()));
        }
        state.contents[0].revision = "replacement-revision".into();
        let (x, y) = backend.find_text("Open").unwrap();
        let mut actions = Vec::new();
        backend.run_with_events(slt::EventBuilder::new().click(x, y).build(), |ctx| {
            draw(ctx, &mut state, "", &mut actions)
        });
        assert!(is_open(&state));
        assert!(state.graph.status.contains("revision changed"));
        assert!(actions.is_empty());
        assert!(state.selected_content().is_none());
    }

    #[test]
    fn full_graph_cycle_has_a_recoverable_node_list_and_close() {
        let mut state = SurfaceState::new();
        state.projects = vec![source(0, "env-a", "")];
        for (id, parent) in [("a", "b"), ("b", "a")] {
            state.graph_threads.push(Thread {
                source: source(0, "env-a", id),
                title: id.into(),
                relationship: "fork".into(),
                parent: Some(parent.into()),
            });
        }
        state.graph.view = Some(snapshot(&state, None, None, None));
        let mut backend = slt::TestBackend::new(40, 12);
        for _ in 0..3 {
            backend.render(|ctx| draw(ctx, &mut state, "", &mut Vec::new()));
        }
        assert!(state.graph.status.contains("cycle"));
        backend.assert_contains("Fork · a");
        backend.assert_contains("Fork · b");
        backend.run_with_events(
            slt::EventBuilder::new().key_code(KeyCode::Esc).build(),
            |ctx| draw(ctx, &mut state, "", &mut Vec::new()),
        );
        assert!(!is_open(&state));
    }
}
