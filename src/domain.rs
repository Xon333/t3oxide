//! Client projections and discoverable actions. Native facts remain native facts.
use anyhow::{Result, bail, ensure};
use serde_json::Value;
use std::collections::{HashMap, HashSet};
use time::{OffsetDateTime, format_description::well_known::Rfc3339};

pub fn string(value: &Value, key: &str) -> String {
    value
        .get(key)
        .and_then(Value::as_str)
        .unwrap_or("")
        .to_owned()
}
pub fn flag(value: &Value, key: &str) -> bool {
    value.get(key).and_then(Value::as_bool).unwrap_or(false)
}
pub fn array<'a>(value: &'a Value, key: &str) -> &'a [Value] {
    value
        .get(key)
        .and_then(Value::as_array)
        .map(Vec::as_slice)
        .unwrap_or(&[])
}
pub fn valid_id(value: &str) -> bool {
    !value.is_empty() && value.len() <= 4096 && !value.chars().any(char::is_control)
}
pub fn plain(value: &str) -> String {
    value
        .chars()
        .filter(|c| !c.is_control() || matches!(c, '\n' | '\t'))
        .collect()
}

#[derive(Clone, Debug)]
pub struct Thread {
    pub profile: usize,
    pub id: String,
    pub title: String,
    pub project: String,
    pub project_id: String,
    pub status: String,
    pub model: String,
    pub updated_at: String,
    pub raw: Value,
}
impl Thread {
    pub fn parse(profile: usize, raw: Value) -> Result<Self> {
        let id = string(&raw, "id");
        ensure!(valid_id(&id), "invalid native thread identity");
        for (key, limit) in [
            ("title", 512),
            ("project", 256),
            ("status", 64),
            ("model", 256),
            ("updatedAt", 64),
        ] {
            ensure!(
                raw.get(key)
                    .is_none_or(|v| v.is_string() && v.as_str().unwrap().len() <= limit),
                "invalid thread field {key}"
            );
        }
        let project_id = string(&raw, "projectId");
        ensure!(
            project_id.is_empty() || valid_id(&project_id),
            "invalid project identity"
        );
        Ok(Self {
            profile,
            id,
            title: plain(&string(&raw, "title")),
            project: plain(&string(&raw, "project")),
            project_id,
            status: string(&raw, "status"),
            model: string(&raw, "model"),
            updated_at: string(&raw, "updatedAt"),
            raw,
        })
    }
    pub fn key(&self) -> String {
        format!("{}\0{}", self.profile, self.id)
    }
    pub fn inbox(&self) -> &Value {
        &self.raw["inbox"]
    }
    pub fn kind(&self) -> &str {
        match self.raw["lineage"]["relationshipToParent"].as_str() {
            Some("subagent") => "subagent",
            Some("fork") => "fork",
            None if self.raw["lineage"].is_object()
                && self.raw["lineage"]["parentThreadId"].is_null() =>
            {
                "main"
            }
            _ => "unknown",
        }
    }
    pub fn parent(&self) -> Option<&str> {
        self.raw["lineage"]["parentThreadId"]
            .as_str()
            .filter(|s| valid_id(s))
    }
    pub fn reviewed(&self) -> bool {
        flag(self.inbox(), "settled")
    }
    pub fn pinned(&self) -> bool {
        flag(self.inbox(), "navigationPinned")
    }
    pub fn working(&self) -> bool {
        !self.attention()
            && self.inbox()["working"].as_bool().unwrap_or_else(|| {
                matches!(
                    self.status.as_str(),
                    "preparing" | "queued" | "starting" | "running"
                ) || self.status == "waiting"
                    && self.inbox()["activeRunId"].as_str().is_some_and(valid_id)
            })
    }
    pub fn attention(&self) -> bool {
        self.status == "failed"
            || self.inbox()["attention"].as_bool().unwrap_or_else(|| {
                self.inbox()["pendingRequestCount"].as_u64().unwrap_or(0) > 0
                    || flag(self.inbox(), "pendingChildRequest")
                    || flag(self.inbox(), "pendingReview")
            })
    }
    /// Presentation follows qualified live membership without inventing a run ID.
    pub fn presentation_status(&self) -> &str {
        if self.status == "failed" {
            "failed"
        } else if self.attention() {
            "needs_input"
        } else if self.working() {
            if matches!(
                self.status.as_str(),
                "preparing" | "queued" | "starting" | "running" | "waiting"
            ) {
                &self.status
            } else {
                "running"
            }
        } else {
            &self.status
        }
    }
    /// Running view membership is independent of read and settlement state.
    pub fn running(&self) -> bool {
        self.working() || self.attention()
    }
    /// Native meaningful-event time; adapter observations never promote a row.
    pub fn activity_at(&self) -> &str {
        if let Some(at) = self.inbox()["activityAt"].as_str() {
            return at;
        }
        // Older adapters may expose fewer lifecycle timestamps. Missing native
        // evidence stays unordered rather than promoting on observation churn.
        [
            "latestRunCompletedAt",
            "latestUserMessageAt",
            "pendingRequestCreatedAt",
            "latestRunStartedAt",
            "latestRunRequestedAt",
        ]
        .iter()
        .filter_map(|field| self.inbox()[*field].as_str())
        .chain(self.raw["createdAt"].as_str())
        .max()
        .unwrap_or("")
    }
    /// A source-qualified reply, request or failure marker, never token churn.
    pub fn activity_generation(&self) -> Option<String> {
        if let Some(generation) = self.inbox().get("activityGeneration") {
            return generation
                .as_str()
                .filter(|generation| valid_generation(generation))
                .map(str::to_owned);
        }
        use sha2::{Digest, Sha256};
        let request = string(self.inbox(), "pendingRequestId");
        if !request.is_empty() {
            return Some(format!(
                "{:x}",
                Sha256::digest(format!("{}\0request\0{request}", self.id))
            ));
        }
        if self.status == "failed" {
            let run = string(self.inbox(), "runId");
            if !run.is_empty() {
                return Some(format!(
                    "{:x}",
                    Sha256::digest(format!("{}\0failure\0{run}", self.id))
                ));
            }
        }
        (!string(self.inbox(), "runId").is_empty()
            || !string(self.inbox(), "latestRunCompletedAt").is_empty())
        .then(|| self.completion_generation())
        .flatten()
    }
    /// Full-shell direct subagent count; absence stays distinct from zero.
    pub fn child_count(&self) -> Option<u64> {
        self.raw["knownChildCount"].as_u64()
    }
    pub fn completion_generation(&self) -> Option<String> {
        use sha2::{Digest, Sha256};
        if self.status != "completed"
            || self.attention()
            || self.working()
            || self.inbox()["queuedCount"].as_u64().unwrap_or(0) > 0
        {
            return None;
        }
        let run = string(self.inbox(), "runId");
        let completed = string(self.inbox(), "latestRunCompletedAt");
        let fallback = if run.is_empty() && completed.is_empty() {
            self.updated_at.clone()
        } else {
            String::new()
        };
        if run.is_empty() && completed.is_empty() && fallback.is_empty() {
            return None;
        }
        Some(format!(
            "{:x}",
            Sha256::digest(serde_json::to_vec(&[run, completed, fallback]).ok()?)
        ))
    }
    pub fn later(&self) -> bool {
        OffsetDateTime::parse(&string(self.inbox(), "snoozedUntil"), &Rfc3339)
            .is_ok_and(|date| date > OffsetDateTime::now_utc())
    }
    pub fn lane(&self) -> &'static str {
        if self.inbox().is_null() {
            "Unknown"
        } else if self.attention() {
            "Attention"
        } else if self.working() {
            "Working"
        } else if self.later() {
            "Later"
        } else if self.inbox()["queuedCount"].as_i64().unwrap_or(0) > 0 {
            "Queued"
        } else if self.reviewed() {
            "Reviewed"
        } else {
            "Inbox"
        }
    }
    pub fn matches_lane(&self, lane: &str) -> bool {
        match lane {
            "Open" | "Unread / Completed" => !self.reviewed() && !self.running(),
            "Running" => self.running(),
            "Pins" => self.pinned(),
            "Settled" => self.reviewed(),
            "All" => true,
            "Unreviewed" => !self.reviewed(),
            "Inbox" => matches!(self.lane(), "Inbox" | "Attention"),
            other => self.lane() == other,
        }
    }
}

fn valid_generation(generation: &str) -> bool {
    generation.len() == 64
        && generation
            .bytes()
            .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
}

#[derive(Clone, Debug, Default)]
pub struct ThreadPage {
    pub threads: Vec<Thread>,
    pub more: bool,
    pub next_cursor: Option<u64>,
    pub observed_at: String,
    pub search: bool,
    pub inclusive_children: bool,
    pub query: String,
    pub navigation_pins_only: bool,
}
impl ThreadPage {
    pub fn parse(profile: usize, value: Value) -> Result<Self> {
        let Some(items) = value["threads"].as_array() else {
            bail!("host returned no conversation list")
        };
        ensure!(
            items.len() <= 200,
            "host conversation page exceeds 200 rows"
        );
        let mut ids = HashSet::new();
        let mut threads = Vec::with_capacity(items.len());
        for item in items {
            let row = Thread::parse(profile, item.clone())?;
            ensure!(
                ids.insert(row.id.clone()),
                "duplicate native thread identity"
            );
            threads.push(row);
        }
        let next = value["nextCursor"].as_u64();
        ensure!(
            next.is_none_or(|n| n <= 1_000_000),
            "invalid conversation cursor"
        );
        Ok(Self {
            threads,
            more: flag(&value, "more") || flag(&value, "searchIncomplete"),
            next_cursor: next,
            observed_at: string(&value, "observedAt"),
            search: flag(&value, "serverSearch"),
            inclusive_children: value["_clientInclusiveChildren"].as_bool().unwrap_or(true),
            query: string(&value, "_clientQuery"),
            navigation_pins_only: flag(&value, "navigationPinsOnly"),
        })
    }
}

/// Produce a stable, source-qualified local tree. Filtering includes collapsed
/// children; no title can retarget a click or join identities across hosts.
pub fn visible_threads(
    threads: &[Thread],
    query: &str,
    lane: &str,
    kind: &str,
    profile: Option<usize>,
    expanded: &HashSet<String>,
) -> Vec<(usize, usize)> {
    visible_threads_with_unread(
        threads,
        query,
        lane,
        kind,
        profile,
        expanded,
        &HashSet::new(),
    )
}

/// View-one unread priority uses exact qualified keys and retains native lineage.
pub fn visible_threads_with_unread(
    threads: &[Thread],
    query: &str,
    lane: &str,
    kind: &str,
    profile: Option<usize>,
    expanded: &HashSet<String>,
    unread: &HashSet<String>,
) -> Vec<(usize, usize)> {
    let q = query.trim().to_lowercase();
    let priority = matches!(lane, "Open" | "Unread / Completed");
    let same_read_group = |a: usize, b: usize| {
        !priority || unread.contains(&threads[a].key()) == unread.contains(&threads[b].key())
    };
    let by_id: HashMap<(usize, &str), usize> = threads
        .iter()
        .enumerate()
        .map(|(i, t)| ((t.profile, t.id.as_str()), i))
        .collect();
    let eligible: HashSet<usize> = threads
        .iter()
        .enumerate()
        .filter(|(_, t)| {
            profile.is_none_or(|p| p == t.profile)
                && t.matches_lane(lane)
                && (kind == "all" || t.kind() == kind.trim_end_matches('s'))
                && (q.is_empty()
                    || format!("{} {} {}", t.title, t.project, t.id)
                        .to_lowercase()
                        .contains(&q))
        })
        .map(|(i, _)| i)
        .collect();
    let mut roots: Vec<usize> = eligible
        .iter()
        .copied()
        .filter(|i| {
            let t = &threads[*i];
            t.kind() != "subagent"
                || t.parent()
                    .and_then(|id| by_id.get(&(t.profile, id)))
                    .is_none_or(|p| !eligible.contains(p) || !same_read_group(*i, *p))
        })
        .collect();
    let sort = |a: &usize, b: &usize| {
        (priority && unread.contains(&threads[*b].key()))
            .cmp(&(priority && unread.contains(&threads[*a].key())))
            .then_with(|| threads[*b].activity_at().cmp(threads[*a].activity_at()))
            .then_with(|| threads[*a].key().cmp(&threads[*b].key()))
    };
    roots.sort_by(sort);
    let mut children: HashMap<usize, Vec<usize>> = HashMap::new();
    for i in &eligible {
        let t = &threads[*i];
        if t.kind() == "subagent"
            && let Some(p) = t
                .parent()
                .and_then(|id| by_id.get(&(t.profile, id)))
                .filter(|p| eligible.contains(p) && same_read_group(*i, **p))
        {
            children.entry(*p).or_default().push(*i);
        }
    }
    for items in children.values_mut() {
        items.sort_by(sort);
    }
    let mut result = Vec::new();
    let mut visited = HashSet::new();
    let mut stack: Vec<(usize, usize)> = roots.into_iter().rev().map(|i| (i, 0)).collect();
    while let Some((i, depth)) = stack.pop() {
        if !visited.insert(i) {
            continue;
        }
        result.push((i, depth));
        if (!q.is_empty() || expanded.contains(&threads[i].key()))
            && let Some(kids) = children.get(&i)
        {
            for kid in kids.iter().rev() {
                stack.push((*kid, (depth + 1).min(12)));
            }
        }
    }
    // Malformed/cyclic native lineage remains reachable without inventing ancestry.
    let mut cycles: Vec<usize> = eligible
        .iter()
        .copied()
        .filter(|i| {
            !visited.contains(i) && {
                let mut current = *i;
                let mut seen = HashSet::new();
                while let Some(p) = threads[current]
                    .parent()
                    .and_then(|id| by_id.get(&(threads[current].profile, id)))
                {
                    if !seen.insert(current) {
                        return true;
                    }
                    if !eligible.contains(p) {
                        return false;
                    }
                    current = *p;
                }
                false
            }
        })
        .collect();
    cycles.sort_by(sort);
    result.extend(cycles.into_iter().map(|i| (i, 0)));
    result
}

pub struct Action {
    pub name: &'static str,
    pub label: &'static str,
    pub description: &'static str,
    pub key: &'static str,
}
pub const ACTIONS: &[Action] = &[
    Action {
        name: "check",
        label: "Run check",
        description: "One finite check beside this pane; retain output and selected source for a targeted follow-up",
        key: "",
    },
    Action {
        name: "check-result",
        label: "Check result",
        description: "Read the original attempt and its finding without rerunning",
        key: "",
    },
    Action {
        name: "context",
        label: "Context records",
        description: "Recall captured decisions, evidence, journals and local open loops",
        key: "",
    },
    Action {
        name: "context-save",
        label: "Capture source",
        description: "Explicitly retain the latest native agent message and its provenance",
        key: "",
    },
    Action {
        name: "resume",
        label: "Resume context",
        description: "Review local context with protected drafts, original operations and native questions",
        key: "",
    },
    Action {
        name: "answer-drafts",
        label: "Saved answers",
        description: "Review retained answers and original source revisions",
        key: "",
    },
    Action {
        name: "new",
        label: "New conversation",
        description: "Choose native project and defaults; review before creating",
        key: "",
    },
    Action {
        name: "commands",
        label: "Commands",
        description: "Find any action",
        key: "Ctrl+P",
    },
    Action {
        name: "command",
        label: "Enter command",
        description: "Enter an explicit slash command with arguments",
        key: "",
    },
    Action {
        name: "views",
        label: "Conversation tabs",
        description: "Unread and Completed, Running, Pins and Settled",
        key: "",
    },
    Action {
        name: "switch",
        label: "Switch conversation",
        description: "Fuzzy find loaded conversations and open the exact source in an internal tab",
        key: "Ctrl+F · /switch · /threads",
    },
    Action {
        name: "env",
        label: "Environment scope",
        description: "Browse one or all configured environments",
        key: "",
    },
    Action {
        name: "graph",
        label: "Loaded relationship graph",
        description: "Full view of loaded native lineage; selection, camera and source return stay independent",
        key: "Ctrl+G · /graph",
    },
    Action {
        name: "filter",
        label: "Thread type",
        description: "All, main conversations, subagents or forks",
        key: "",
    },
    Action {
        name: "refresh",
        label: "Refresh conversations",
        description: "Read native metadata independently for every host",
        key: "Ctrl+R",
    },
    Action {
        name: "more",
        label: "More conversations",
        description: "Load the next bounded native metadata pages",
        key: "",
    },
    Action {
        name: "search",
        label: "Search server conversations",
        description: "Explicit native recent-message search; results are bounded",
        key: "/search",
    },
    Action {
        name: "search-scope",
        label: "Search scope",
        description: "Choose a project, environment or all projects",
        key: "",
    },
    Action {
        name: "preview",
        label: "Preview highlighted conversation",
        description: "Read-only preview; does not change the attached thread",
        key: "",
    },
    Action {
        name: "parent",
        label: "Open parent conversation",
        description: "Use authoritative native lineage",
        key: "",
    },
    Action {
        name: "recent",
        label: "Recent conversations",
        description: "Return to an exact source and draft",
        key: "",
    },
    Action {
        name: "requests",
        label: "Pending requests",
        description: "Review exact native approval or question revisions",
        key: "Ctrl+Y",
    },
    Action {
        name: "send",
        label: "Send draft",
        description: "Send the human message with native delivery semantics",
        key: "Ctrl+Enter | Ctrl+] | Ctrl+\\",
    },
    Action {
        name: "stop",
        label: "Stop active turn",
        description: "Independent Stop lane; native queue hold",
        key: "Ctrl+S",
    },
    Action {
        name: "delivery",
        label: "Delivery mode",
        description: "Auto, queue after active, or steer",
        key: "",
    },
    Action {
        name: "earlier",
        label: "Earlier messages",
        description: "Read an earlier native page; pause live detail",
        key: "",
    },
    Action {
        name: "latest",
        label: "Latest live conversation",
        description: "Resume the current native subscription",
        key: "",
    },
    Action {
        name: "activity",
        label: "Tools and activity",
        description: "Inspect bounded tool source, arguments and results",
        key: "",
    },
    Action {
        name: "review",
        label: "Changes and pull requests",
        description: "Read native workspace diff and linked PRs",
        key: "",
    },
    Action {
        name: "reviewed",
        label: "Mark reviewed / Settle",
        description: "Settle the selected or attached conversation; retained in Settled, not deleted",
        key: "Ctrl+T",
    },
    Action {
        name: "unreviewed",
        label: "Restore to inbox / Unsettle",
        description: "Unsettle the selected or attached conversation using its native revision",
        key: "Ctrl+T in Settled",
    },
    Action {
        name: "snooze",
        label: "Move to Later",
        description: "Use native snooze; no client scheduler",
        key: "",
    },
    Action {
        name: "unsnooze",
        label: "Return from Later",
        description: "Clear native snooze with the current revision",
        key: "",
    },
    Action {
        name: "pin",
        label: "Pin / unpin conversation",
        description: "Use the current native revision",
        key: "",
    },
    Action {
        name: "model",
        label: "Model and options",
        description: "Stage model, effort and service tier from native catalog",
        key: "",
    },
    Action {
        name: "override",
        label: "One-message model override",
        description: "Stage model/options for just the next message",
        key: "",
    },
    Action {
        name: "presets",
        label: "Model favorites",
        description: "Save and apply environment-scoped selections",
        key: "",
    },
    Action {
        name: "runtime",
        label: "Effective runtime",
        description: "Compare saved, requested and provider-reported selections",
        key: "",
    },
    Action {
        name: "destination",
        label: "Creation destination",
        description: "Pin exact environment/project independently of navigation",
        key: "",
    },
    Action {
        name: "native-defaults",
        label: "Native defaults",
        description: "Inspect inherited project and environment settings",
        key: "",
    },
    Action {
        name: "new-same",
        label: "New in same project",
        description: "Carry project identity; resolve native defaults afresh",
        key: "",
    },
    Action {
        name: "batch",
        label: "File multiple tasks",
        description: "Review separate native launches from human-authored briefs",
        key: "",
    },
    Action {
        name: "batch-receipts",
        label: "Task filing receipts",
        description: "Inspect acknowledged launches and remaining drafts",
        key: "",
    },
    Action {
        name: "recover",
        label: "Recover saved operations",
        description: "Review original intent; reconcile with the same operation ID",
        key: "",
    },
    Action {
        name: "reconnect",
        label: "Reconnect",
        description: "Replace this adapter connection; never replay writes",
        key: "",
    },
    Action {
        name: "recall",
        label: "Recall a sent prompt",
        description: "Restore a source-qualified human prompt into the composer",
        key: "",
    },
    Action {
        name: "edit",
        label: "Edit draft externally",
        description: "Use VISUAL or EDITOR; save back to the same conversation",
        key: "",
    },
    Action {
        name: "layout",
        label: "Workspace layout",
        description: "Split, focused conversation, or wide inspector",
        key: "",
    },
    Action {
        name: "theme",
        label: "Appearance",
        description: "Terminal, violet, cyan or lime",
        key: "",
    },
    Action {
        name: "mouse",
        label: "Toggle mouse",
        description: "Save mouse interaction or native terminal selection for the next launch",
        key: "",
    },
    Action {
        name: "web",
        label: "Full T3 address",
        description: "Show the configured source environment URL",
        key: "",
    },
    Action {
        name: "help",
        label: "Commands and shortcuts",
        description: "Context-aware shortcuts and workflow",
        key: "F1 | Alt+H",
    },
    Action {
        name: "quit",
        label: "Exit",
        description: "Save drafts; native work continues",
        key: "Ctrl+Q",
    },
    Action {
        name: "find",
        label: "Find in loaded messages",
        description: "Search the current bounded transcript page",
        key: "",
    },
    Action {
        name: "find-next",
        label: "Next loaded message match",
        description: "Jump through the current page without a host request",
        key: "",
    },
    Action {
        name: "find-previous",
        label: "Previous loaded message match",
        description: "Return to the previous local match",
        key: "",
    },
    Action {
        name: "outline",
        label: "Loaded message outline",
        description: "Inspect or jump to exact source-qualified messages",
        key: "",
    },
    Action {
        name: "copy",
        label: "Copy exact text",
        description: "Choose answer, draft, page, detail, code or inspected tool text",
        key: "",
    },
    Action {
        name: "copy-draft",
        label: "Copy draft",
        description: "Copy the whole active prompt draft",
        key: "Ctrl+Alt+C",
    },
    Action {
        name: "project",
        label: "Browse project",
        description: "Scope loaded conversations without changing creation destination",
        key: "",
    },
    Action {
        name: "priority",
        label: "Task priority",
        description: "Local presentation rank; no execution scheduling",
        key: "",
    },
    Action {
        name: "project-order",
        label: "Project order",
        description: "Arrange exact native projects locally",
        key: "",
    },
    Action {
        name: "priority-filter",
        label: "Minimum local priority",
        description: "Show all, Medium, High or Urgent tasks",
        key: "",
    },
    Action {
        name: "priority-undo",
        label: "Undo local priority change",
        description: "Restore prior ranks only if their revision is unchanged",
        key: "",
    },
    Action {
        name: "saved-launches",
        label: "Saved launch forms and task lists",
        description: "Restore imported unsent forms and remaining human task lists",
        key: "",
    },
    Action {
        name: "clear",
        label: "Clear draft with recall",
        description: "Keep cleared text in durable human prompt recall",
        key: "",
    },
    Action {
        name: "launch-defaults-reset",
        label: "Refresh staged launch defaults",
        description: "Explicitly reset model and workspace while preserving human text",
        key: "",
    },
];

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    fn thread(profile: usize, id: &str, parent: Option<&str>) -> Thread {
        Thread::parse(profile,json!({"id":id,"title":id,"lineage":{"parentThreadId":parent,"relationshipToParent":parent.map(|_|"subagent")},"inbox":{"settled":false}})).unwrap()
    }
    #[test]
    fn running_honors_native_background_work_and_older_adapter_fallback() {
        for status in ["completed", "waiting"] {
            let row = Thread::parse(
                0,
                json!({"id":"background","status":status,"inbox":{"working":true}}),
            )
            .unwrap();
            assert!(row.running(), "native background work: {status}");
            assert!(matches!(row.presentation_status(), "running" | "waiting"));
            assert!(
                !row.matches_lane("Open"),
                "background work is not Completed"
            );
        }
        let nonblocking = Thread::parse(
            0,
            json!({"id":"active","status":"running",
            "inbox":{"working":true,"attention":false,"pendingRequestCount":1}}),
        )
        .unwrap();
        assert!(nonblocking.working() && !nonblocking.attention());
        assert_eq!(nonblocking.presentation_status(), "running");
        for status in [
            "completed",
            "stopped",
            "interrupted",
            "idle",
            "failed",
            "waiting",
        ] {
            let row = Thread::parse(
                0,
                json!({"id":"exact","status":status,"inbox":{"working":false,"runId":"old-run"}}),
            )
            .unwrap();
            assert!(!row.working(), "{status}");
        }
        for status in ["preparing", "queued", "starting", "running", "waiting"] {
            let mut row = Thread::parse(
                0,
                json!({"id":"exact","status":status,"inbox":{"activeRunId":"active-run"}}),
            )
            .unwrap();
            assert!(row.working(), "{status}");
            row.raw["inbox"]["pendingRequestCount"] = json!(1);
            assert!(!row.working() && row.attention());
        }
        let mut row = Thread::parse(0, json!({"id":"done","status":"completed","updatedAt":"2026-11-02T12:00:00Z","inbox":{"runId":"native-run","latestRunCompletedAt":"2026-11-02T11:59:00Z"}})).unwrap();
        let original = row.completion_generation().unwrap();
        row.updated_at = "2026-11-02T13:00:00Z".into();
        assert_eq!(row.completion_generation().as_ref(), Some(&original));
        row.raw["inbox"]["latestRunCompletedAt"] = json!("2026-11-02T12:01:00Z");
        assert_ne!(row.completion_generation().as_ref(), Some(&original));
    }

    #[test]
    fn collapsed_children_remain_searchable_and_hosts_do_not_alias() {
        let rows = vec![
            thread(0, "root", None),
            thread(0, "needle", Some("root")),
            thread(1, "root", None),
        ];
        assert_eq!(
            visible_threads(&rows, "", "All", "all", None, &HashSet::new()).len(),
            2
        );
        assert_eq!(
            visible_threads(&rows, "needle", "All", "all", None, &HashSet::new()),
            vec![(1, 0)]
        );
        let open = HashSet::from([rows[0].key()]);
        assert_eq!(
            visible_threads(&rows, "", "All", "all", None, &open).len(),
            3
        );
    }
    #[test]
    fn newest_order_uses_meaningful_activity_and_pins_have_a_separate_scope() {
        let mut rows: Vec<_> = [
            ("completed", "2026-11-02T10:00:00Z"),
            ("running", "2026-11-02T09:00:00Z"),
            ("completed", "2026-11-02T08:00:00Z"),
        ]
        .into_iter()
        .enumerate()
        .map(|(index, (status, updated))| {
            Thread::parse(
                0,
                json!({"id":format!("thread-{index}"),"status":status,"updatedAt":updated,
                    "lineage":{"parentThreadId":null},"inbox":{"settled":false,"activityAt":updated}}),
            )
            .unwrap()
        })
        .collect();
        let visible = |threads: &[Thread]| {
            visible_threads(threads, "", "Unreviewed", "all", None, &HashSet::new())
        };
        assert_eq!(visible(&rows), vec![(0, 0), (1, 0), (2, 0)]);
        rows[1].updated_at = "2026-11-02T11:00:00Z".into();
        assert_eq!(visible(&rows), vec![(0, 0), (1, 0), (2, 0)]);
        rows[1].raw["inbox"]["activityAt"] = json!("2026-11-02T11:00:00Z");
        assert_eq!(visible(&rows), vec![(1, 0), (0, 0), (2, 0)]);
        rows[2].raw["inbox"]["navigationPinned"] = json!(true);
        assert_eq!(visible(&rows), vec![(1, 0), (0, 0), (2, 0)]);
        assert_eq!(
            visible_threads(&rows, "", "Pins", "all", None, &HashSet::new()),
            vec![(2, 0)]
        );
        rows[2].raw["inbox"]["settled"] = json!(true);
        assert_eq!(visible(&rows), vec![(1, 0), (0, 0)]);
    }

    #[test]
    fn read_priority_is_independent_of_running_and_settlement() {
        let rows: Vec<_> = [
            ("completed", false, 0, "2026-11-02T10:00:00Z"),
            ("idle", false, 0, "2026-11-02T08:00:00Z"),
            ("running", false, 0, "2026-11-02T11:00:00Z"),
            ("waiting", false, 1, "2026-11-02T12:00:00Z"),
            ("failed", true, 0, "2026-11-02T13:00:00Z"),
        ]
        .into_iter()
        .enumerate()
        .map(|(index, (status, settled, requests, at))| {
            Thread::parse(
                0,
                json!({"id":format!("thread-{index}"),"status":status,
                "lineage":{"parentThreadId":null},"inbox":{"settled":settled,
                "pendingRequestCount":requests,"activityAt":at}}),
            )
            .unwrap()
        })
        .collect();
        let unread = HashSet::from([rows[1].key()]);
        assert_eq!(
            visible_threads_with_unread(&rows, "", "Open", "all", None, &HashSet::new(), &unread),
            vec![(1, 0), (0, 0)]
        );
        assert_eq!(
            visible_threads(&rows, "", "Running", "all", None, &HashSet::new()),
            vec![(4, 0), (3, 0), (2, 0)]
        );
        assert_eq!(
            visible_threads(&rows, "", "Settled", "all", None, &HashSet::new()),
            vec![(4, 0)]
        );
        assert!(rows[4].reviewed() && rows[4].running());
    }

    #[test]
    fn unread_child_stays_reachable_ahead_of_its_read_parent() {
        let rows = vec![
            thread(0, "parent", None),
            thread(0, "child", Some("parent")),
        ];
        let unread = HashSet::from([rows[1].key()]);
        assert_eq!(
            visible_threads_with_unread(&rows, "", "Open", "all", None, &HashSet::new(), &unread),
            vec![(1, 0), (0, 0)]
        );
    }

    #[test]
    fn viewing_does_not_settle_and_unknown_is_explicit() {
        let t = thread(0, "x", None);
        assert_eq!(t.lane(), "Inbox");
        assert!(!t.reviewed());
        let t = Thread::parse(0, json!({"id":"x"})).unwrap();
        assert_eq!(t.lane(), "Unknown");
    }
    #[test]
    fn duplicate_identity_is_rejected() {
        assert!(ThreadPage::parse(0, json!({"threads":[{"id":"x"},{"id":"x"}]})).is_err());
    }
    #[test]
    fn cycle_is_reachable() {
        let rows = vec![thread(0, "a", Some("b")), thread(0, "b", Some("a"))];
        assert_eq!(
            visible_threads(&rows, "", "All", "all", None, &HashSet::new()).len(),
            2
        );
    }
}
