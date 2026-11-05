//! Dependency ordering of admitted human questions from explicit native evidence (B3).
use anyhow::{Result, ensure};
use petgraph::{
    Direction::Incoming,
    algo::{kosaraju_scc, toposort},
    stable_graph::StableDiGraph,
    visit::{EdgeRef, IntoEdgeReferences},
};
use std::{
    cmp::Reverse,
    collections::{BinaryHeap, HashMap, HashSet},
};

/// A full native question revision, admitted by the native attention boundary.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct SourceId {
    pub environment_id: String,
    pub thread_id: String,
    pub request_id: String,
    pub revision: String,
}

/// The exact native record that supplies dependency or coverage evidence.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct EvidenceRef {
    pub environment_id: String,
    pub thread_id: String,
    pub item_id: String,
    pub revision: String,
}

/// Preserve the native reason rather than inferring it from request text.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DependencyReason {
    pub text: String,
    pub evidence: EvidenceRef,
}

/// The prerequisite must precede the dependent question; this is not scheduling.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Dependency {
    pub prerequisite: SourceId,
    pub dependent: SourceId,
    pub reason: DependencyReason,
}

/// An empty native edge set proves independence only with complete coverage.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub enum EvidenceCoverage {
    #[default]
    Unknown,
    Partial(EvidenceRef),
    Complete(EvidenceRef),
}

/// Native cycles remain visible groups; their internal order has no priority meaning.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CycleGroup {
    pub members: Vec<SourceId>,
    pub dependencies: Vec<Dependency>,
}

/// Known prerequisites precede dependents; ties retain native admission order.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Ranking {
    pub ordered: Vec<SourceId>,
    pub dependencies: Vec<Dependency>,
    pub cycles: Vec<CycleGroup>,
    pub unknown: Vec<SourceId>,
    pub coverage: EvidenceCoverage,
}

/// Order only already-admitted questions. No lineage, prose or retrieval score
/// enters this graph. Missing dependency capability must use `Unknown` coverage.
///
/// # Errors
/// Rejects duplicate/stale sources, missing edge endpoints and unqualified evidence.
pub fn rank(
    questions: &[SourceId],
    dependencies: &[Dependency],
    coverage: EvidenceCoverage,
) -> Result<Ranking> {
    let mut graph = StableDiGraph::<SourceId, DependencyReason>::new();
    let mut indices = HashMap::new();
    let mut requests = HashSet::new();
    for question in questions {
        ensure!(
            source_valid(question),
            "question ranking needs a complete native source revision"
        );
        ensure!(
            !indices.contains_key(question),
            "duplicate admitted question source"
        );
        ensure!(
            requests.insert((
                &question.environment_id,
                &question.thread_id,
                &question.request_id
            )),
            "multiple revisions of one native question cannot be ranked together"
        );
        indices.insert(question.clone(), graph.add_node(question.clone()));
    }
    match &coverage {
        EvidenceCoverage::Unknown => {}
        EvidenceCoverage::Partial(evidence) | EvidenceCoverage::Complete(evidence) => {
            ensure!(
                evidence_valid(evidence),
                "dependency coverage needs source-qualified native evidence"
            );
        }
    }
    for dependency in dependencies {
        ensure!(
            evidence_valid(&dependency.reason.evidence)
                && !dependency.reason.text.trim().is_empty()
                && dependency.reason.text.len() <= 4096,
            "dependency needs a native reason and source revision"
        );
        let from = indices.get(&dependency.prerequisite);
        let to = indices.get(&dependency.dependent);
        ensure!(
            from.is_some() && to.is_some(),
            "dependency refers to an unadmitted or changed question source"
        );
        if let (Some(from), Some(to)) = (from, to) {
            graph.add_edge(*from, *to, dependency.reason.clone());
        }
    }
    // Collapse strongly connected groups before sorting: a native cycle must
    // neither disappear nor prevent unrelated questions from remaining usable.
    let mut components = kosaraju_scc(&graph);
    for component in &mut components {
        component.sort_by_key(|node| node.index());
    }
    let mut condensed = StableDiGraph::<usize, ()>::new();
    let component_nodes: Vec<_> = (0..components.len())
        .map(|component| condensed.add_node(component))
        .collect();
    let mut component_of = HashMap::new();
    for (component, members) in components.iter().enumerate() {
        for member in members {
            component_of.insert(*member, component);
        }
    }
    let mut edges = HashSet::new();
    for edge in graph.edge_references() {
        let from = component_of[&edge.source()];
        let to = component_of[&edge.target()];
        if from != to && edges.insert((from, to)) {
            condensed.add_edge(component_nodes[from], component_nodes[to], ());
        }
    }
    toposort(&condensed, None)
        .map_err(|_| anyhow::anyhow!("dependency condensation unexpectedly retained a cycle"))?;
    // Petgraph defines the topology. Choose ready components by native
    // admission order, so unrelated questions do not jump behind whole levels.
    let mut remaining: Vec<_> = component_nodes
        .iter()
        .map(|node| condensed.neighbors_directed(*node, Incoming).count())
        .collect();
    let mut ready = BinaryHeap::new();
    for (component, count) in remaining.iter().enumerate() {
        if *count == 0 {
            ready.push(Reverse((components[component][0].index(), component)));
        }
    }
    let mut component_order = Vec::with_capacity(components.len());
    while let Some(Reverse((_, component))) = ready.pop() {
        component_order.push(component);
        for dependent in condensed.neighbors(component_nodes[component]) {
            let next = condensed[dependent];
            remaining[next] -= 1;
            if remaining[next] == 0 {
                ready.push(Reverse((components[next][0].index(), next)));
            }
        }
    }
    let mut ordered = Vec::with_capacity(questions.len());
    let mut cycles = Vec::new();
    for component in component_order {
        let members: Vec<_> = components[component]
            .iter()
            .map(|node| graph[*node].clone())
            .collect();
        let internal: Vec<_> = dependencies
            .iter()
            .filter(|dependency| {
                members.contains(&dependency.prerequisite)
                    && members.contains(&dependency.dependent)
            })
            .cloned()
            .collect();
        if members.len() > 1
            || internal
                .iter()
                .any(|dependency| dependency.prerequisite == dependency.dependent)
        {
            cycles.push(CycleGroup {
                members: members.clone(),
                dependencies: internal,
            });
        }
        ordered.extend(members);
    }
    let unknown = if matches!(coverage, EvidenceCoverage::Complete(_)) {
        Vec::new()
    } else {
        questions.to_vec()
    };
    Ok(Ranking {
        ordered,
        dependencies: dependencies.to_vec(),
        cycles,
        unknown,
        coverage,
    })
}

fn valid(value: &str) -> bool {
    !value.is_empty() && value.len() <= 4096 && !value.chars().any(char::is_control)
}
fn source_valid(source: &SourceId) -> bool {
    [
        &source.environment_id,
        &source.thread_id,
        &source.request_id,
        &source.revision,
    ]
    .into_iter()
    .all(|value| valid(value))
}
fn evidence_valid(evidence: &EvidenceRef) -> bool {
    [
        &evidence.environment_id,
        &evidence.thread_id,
        &evidence.item_id,
        &evidence.revision,
    ]
    .into_iter()
    .all(|value| valid(value))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn source(environment: &str, request: &str) -> SourceId {
        SourceId {
            environment_id: environment.into(),
            thread_id: "thread-a".into(),
            request_id: request.into(),
            revision: "native-revision".into(),
        }
    }
    fn evidence() -> EvidenceRef {
        EvidenceRef {
            environment_id: "environment-a".into(),
            thread_id: "thread-a".into(),
            item_id: "dependency-item".into(),
            revision: "item-revision".into(),
        }
    }
    fn dependency(from: &SourceId, to: &SourceId) -> Dependency {
        Dependency {
            prerequisite: from.clone(),
            dependent: to.clone(),
            reason: DependencyReason {
                text: "Release approval requires target choice".into(),
                evidence: evidence(),
            },
        }
    }

    #[test]
    fn missing_evidence_remains_unknown_and_preserves_native_queue_order() {
        let questions = [
            source("environment-b", "request-b"),
            source("environment-a", "request-a"),
        ];
        let result = rank(&questions, &[], EvidenceCoverage::Unknown).unwrap();
        assert_eq!(result.ordered, questions);
        assert_eq!(result.unknown, questions);
        assert!(result.dependencies.is_empty() && result.cycles.is_empty());
        let complete = rank(&questions, &[], EvidenceCoverage::Complete(evidence())).unwrap();
        assert!(complete.unknown.is_empty());
    }

    #[test]
    fn explicit_dependencies_retain_reasons_and_do_not_reorder_ready_unrelated_questions() {
        let questions = [
            source("environment-a", "request-a"),
            source("environment-b", "request-a"),
            source("environment-a", "request-c"),
        ];
        let edge = dependency(&questions[0], &questions[1]);
        let result = rank(
            &questions,
            std::slice::from_ref(&edge),
            EvidenceCoverage::Partial(evidence()),
        )
        .unwrap();
        assert_eq!(result.ordered, questions);
        assert_eq!(result.dependencies, [edge]);
        let reversed = [questions[1].clone(), questions[0].clone()];
        let result = rank(
            &reversed,
            &[dependency(&questions[0], &questions[1])],
            EvidenceCoverage::Unknown,
        )
        .unwrap();
        assert_eq!(result.ordered, [questions[0].clone(), questions[1].clone()]);
        let mut stale = questions[0].clone();
        stale.revision = "changed-revision".into();
        assert!(
            rank(
                &questions,
                &[dependency(&stale, &questions[1])],
                EvidenceCoverage::Unknown
            )
            .is_err()
        );
    }

    #[test]
    fn cycles_are_explicit_without_hiding_their_downstream_questions() {
        let questions = [
            source("environment-a", "request-c"),
            source("environment-a", "request-b"),
            source("environment-a", "request-a"),
        ];
        let dependencies = [
            dependency(&questions[1], &questions[2]),
            dependency(&questions[2], &questions[1]),
            dependency(&questions[1], &questions[0]),
        ];
        let result = rank(
            &questions,
            &dependencies,
            EvidenceCoverage::Complete(evidence()),
        )
        .unwrap();
        assert_eq!(
            result.ordered,
            [
                questions[1].clone(),
                questions[2].clone(),
                questions[0].clone()
            ]
        );
        assert_eq!(result.cycles.len(), 1);
        assert_eq!(
            result.cycles[0].members,
            [questions[1].clone(), questions[2].clone()]
        );
        assert_eq!(result.cycles[0].dependencies, dependencies[..2]);
    }
}
