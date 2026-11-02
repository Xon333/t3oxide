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
        !string(self.inbox(), "pinnedAt").is_empty()
    }
    pub fn working(&self) -> bool {
        self.inbox()["working"].as_bool().unwrap_or(matches!(
            self.status.as_str(),
            "preparing" | "starting" | "running" | "waiting"
        ))
    }
    pub fn attention(&self) -> bool {
        self.inbox()["pendingRequestCount"].as_u64().unwrap_or(0) > 0
            || flag(self.inbox(), "pendingChildRequest")
            || self.status == "failed"
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
            "All" => true,
            "Unreviewed" => !self.reviewed(),
            "Inbox" => matches!(self.lane(), "Inbox" | "Attention"),
            other => self.lane() == other,
        }
    }
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
    let q = query.trim().to_lowercase();
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
                    .is_none_or(|p| !eligible.contains(p))
        })
        .collect();
    let sort = |a: &usize, b: &usize| {
        threads[*b]
            .pinned()
            .cmp(&threads[*a].pinned())
            .then_with(|| threads[*b].updated_at.cmp(&threads[*a].updated_at))
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
                .filter(|p| eligible.contains(p))
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
        name: "new",
        label: "New conversation",
        description: "Choose native project and defaults; review before creating",
        key: "n",
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
        name: "mode",
        label: "Switch Developer / AgentBox",
        description: "Keep the same conversation, draft and reading position",
        key: "F2",
    },
    Action {
        name: "views",
        label: "Inbox views",
        description: "Attention, Working, Queued, Later, Reviewed and All",
        key: "v",
    },
    Action {
        name: "env",
        label: "Environment scope",
        description: "Browse one or all configured environments",
        key: "e",
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
        key: "Ctrl+F",
    },
    Action {
        name: "preview",
        label: "Preview highlighted conversation",
        description: "Read-only preview; does not change the attached thread",
        key: "Space",
    },
    Action {
        name: "parent",
        label: "Open parent conversation",
        description: "Use authoritative native lineage",
        key: "Alt+Up",
    },
    Action {
        name: "recent",
        label: "Recent conversations",
        description: "Return to an exact source and draft",
        key: "Alt+Left",
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
        key: "Ctrl+]",
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
        key: "[",
    },
    Action {
        name: "latest",
        label: "Latest live conversation",
        description: "Resume the current native subscription",
        key: "]",
    },
    Action {
        name: "activity",
        label: "Tools and activity",
        description: "Inspect bounded tool source, arguments and results",
        key: "a",
    },
    Action {
        name: "review",
        label: "Changes and pull requests",
        description: "Read native workspace diff and linked PRs",
        key: "d",
    },
    Action {
        name: "reviewed",
        label: "Mark reviewed",
        description: "Explicit native disposition; opening alone never marks reviewed",
        key: "x",
    },
    Action {
        name: "unreviewed",
        label: "Restore to inbox",
        description: "Reopen native review disposition",
        key: "",
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
        key: "m",
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
        key: "Alt+R",
    },
    Action {
        name: "edit",
        label: "Edit draft externally",
        description: "Use VISUAL or EDITOR; save back to the same conversation",
        key: "Ctrl+E",
    },
    Action {
        name: "layout",
        label: "Workspace layout",
        description: "Split, focused conversation, or wide inspector",
        key: "F3",
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
        description: "Mouse interaction or native terminal selection",
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
        label: "Keyboard help",
        description: "Context-aware shortcuts and workflow",
        key: "?",
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
        key: "Ctrl+F in transcript",
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
        name: "project",
        label: "Browse project",
        description: "Scope loaded conversations without changing creation destination",
        key: "",
    },
    Action {
        name: "priority",
        label: "Task priority",
        description: "Local AgentBox presentation rank; no execution scheduling",
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
