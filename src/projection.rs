//! Atomic, bounded transcript projections. Revisions belong to one adapter lifetime.
use crate::config::valid_id;
use crate::state::validate_selection;
use crate::wire::MAX_FRAME_BYTES;
use anyhow::{Context, Result, ensure};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::collections::{HashMap, HashSet};

pub const MAX_ROWS: usize = 80;
pub const MAX_ROWS_BYTES: usize = 144 * 1024;
pub const MAX_TEXT_BYTES: usize = 24 * 1024;
const MAX_INTEGER: u64 = 9_007_199_254_740_991;

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Row {
    pub key: String,
    pub source_thread_id: String,
    pub item_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub message_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub run_id: Option<String>,
    pub kind: String,
    pub text: String,
    pub truncated: bool,
    pub streaming: bool,
    pub attachment_count: u64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub position: Option<u64>,
    #[serde(flatten)]
    pub extra: serde_json::Map<String, Value>,
}

#[derive(Clone, Debug, PartialEq)]
pub struct Snapshot {
    pub revision: u64,
    pub items: Vec<Row>,
    pub metadata: Value,
}
impl Snapshot {
    pub fn thread_id(&self) -> &str {
        self.metadata["threadId"].as_str().unwrap_or("")
    }
    pub fn is_live(&self) -> bool {
        self.metadata["live"] == true && self.metadata["historical"] == false
    }
    pub fn as_wire_value(&self) -> Value {
        let mut value = self.metadata.clone();
        value["revision"] = json!(self.revision);
        value["items"] = json!(self.items);
        value
    }
}

#[derive(Default, Debug)]
pub struct Projection {
    selected: String,
    snapshot: Option<Snapshot>,
    pub needs_resnapshot: bool,
    epoch: u64,
}
impl Projection {
    pub fn new() -> Self {
        Self::default()
    }
    pub fn snapshot(&self) -> Option<&Snapshot> {
        self.snapshot.as_ref()
    }
    pub fn epoch(&self) -> u64 {
        self.epoch
    }
    pub fn select(&mut self, thread: &str) {
        self.selected = thread.to_owned();
        self.snapshot = None;
        self.needs_resnapshot = false;
        self.epoch += 1;
    }
    pub fn reset(&mut self) {
        self.select("");
    }
    pub fn apply_snapshot(&mut self, value: Value, expected_thread: &str) -> Result<bool> {
        let result = (|| {
            ensure!(
                serde_json::to_vec(&value)?.len() <= MAX_FRAME_BYTES,
                "transcript frame exceeds 384 KiB"
            );
            ensure!(value.is_object(), "snapshot must be an object");
            let revision = integer(&value, "revision")?;
            let mut metadata = value.clone();
            metadata.as_object_mut().unwrap().remove("revision");
            metadata.as_object_mut().unwrap().remove("items");
            validate_metadata(&metadata)?;
            let thread = metadata["threadId"].as_str().unwrap();
            ensure!(
                thread == expected_thread
                    && (self.selected.is_empty() || self.selected == expected_thread),
                "snapshot belongs to a different conversation"
            );
            let rows = decode_rows(&value, "items")?;
            if let Some(current) = &self.snapshot
                && current.thread_id() == thread
                && revision <= current.revision
            {
                return Ok(false);
            }
            self.selected = expected_thread.into();
            self.snapshot = Some(Snapshot {
                revision,
                items: rows,
                metadata,
            });
            self.needs_resnapshot = false;
            self.epoch += 1;
            Ok(true)
        })();
        if result.is_err() {
            self.needs_resnapshot = true;
        }
        result
    }
    pub fn apply_delta(&mut self, value: Value) -> Result<bool> {
        let result = (|| {
            ensure!(
                serde_json::to_vec(&value)?.len() <= MAX_FRAME_BYTES,
                "delta exceeds frame limit"
            );
            let thread = id(&value, "threadId", 4096)?;
            let base = integer(&value, "baseRevision")?;
            let revision = integer(&value, "revision")?;
            ensure!(revision > base, "delta revision must advance its base");
            let metadata = value["metadata"].clone();
            ensure!(
                metadata.get("items").is_none() && metadata.get("revision").is_none(),
                "delta metadata cannot contain rows or revision"
            );
            validate_metadata(&metadata)?;
            ensure!(
                metadata["threadId"] == thread,
                "delta metadata identity mismatch"
            );
            ensure!(
                self.selected.is_empty() || self.selected == thread,
                "delta belongs to a different conversation"
            );
            let upserts = decode_rows(&value, "upserts")?;
            let removed = keys(&value, "removed")?;
            let order = keys(&value, "order")?;
            ensure!(
                !upserts.iter().any(|row| removed.contains(&row.key)),
                "delta both removes and updates a row"
            );
            let current = self
                .snapshot
                .as_ref()
                .context("delta arrived before its snapshot")?;
            if current.thread_id() == thread && revision <= current.revision {
                return Ok(false);
            }
            ensure!(
                !self.needs_resnapshot && current.thread_id() == thread && base == current.revision,
                "transcript revision mismatch; request Latest to resynchronize"
            );
            let mut indexed: HashMap<String, Row> = current
                .items
                .iter()
                .map(|row| (row.key.clone(), row.clone()))
                .collect();
            for key in removed {
                ensure!(
                    indexed.remove(&key).is_some(),
                    "delta removes a missing row"
                );
            }
            for row in upserts {
                indexed.insert(row.key.clone(), row);
            }
            ensure!(
                indexed.len() == order.len(),
                "delta order must name every retained row"
            );
            let mut items = Vec::with_capacity(order.len());
            for key in order {
                items.push(
                    indexed
                        .remove(&key)
                        .context("delta order names a missing row")?,
                );
            }
            validate_rows(&items)?;
            let candidate = Snapshot {
                revision,
                items,
                metadata,
            };
            ensure!(
                serde_json::to_vec(&candidate.as_wire_value())?.len() <= MAX_FRAME_BYTES,
                "resulting snapshot exceeds frame limit"
            );
            self.snapshot = Some(candidate);
            self.epoch += 1;
            Ok(true)
        })();
        if result.is_err() {
            self.needs_resnapshot = true;
        }
        result
    }
}

fn integer(value: &Value, field: &str) -> Result<u64> {
    let n = value[field]
        .as_u64()
        .with_context(|| format!("invalid transcript integer: {field}"))?;
    ensure!(
        n <= MAX_INTEGER,
        "transcript integer exceeds native safe range: {field}"
    );
    Ok(n)
}
fn id<'a>(value: &'a Value, field: &str, limit: usize) -> Result<&'a str> {
    let text = value[field]
        .as_str()
        .with_context(|| format!("invalid transcript identity: {field}"))?;
    ensure!(
        valid_id(text, limit),
        "invalid transcript identity: {field}"
    );
    Ok(text)
}
fn string(value: &Value, field: &str, limit: usize) -> Result<()> {
    ensure!(
        value[field].as_str().is_some_and(|s| s.len() <= limit),
        "invalid transcript text: {field}"
    );
    Ok(())
}
fn validate_metadata(value: &Value) -> Result<()> {
    ensure!(value.is_object(), "transcript metadata must be an object");
    id(value, "threadId", 4096)?;
    for (field, limit) in [
        ("title", 512),
        ("model", 256),
        ("status", 256),
        ("state", 32),
    ] {
        string(value, field, limit)?;
    }
    for field in ["projectId", "activeRunId"] {
        if value.get(field).is_some_and(|v| !v.is_null()) {
            id(value, field, 4096)?;
        }
    }
    for (field, limit) in [
        ("workspace", 4096),
        ("mode", 256),
        ("historyCursor", 64 * 1024),
        ("observedAt", 256),
    ] {
        if value.get(field).is_some_and(|v| !v.is_null()) {
            string(value, field, limit)?;
        }
    }
    if value.get("sequence").is_some_and(|v| !v.is_null()) {
        integer(value, "sequence")?;
    }
    for field in ["hasMore", "limited", "live", "historical"] {
        ensure!(
            value[field].is_boolean(),
            "invalid transcript flag: {field}"
        );
    }
    let state = value["state"].as_str().unwrap();
    ensure!(
        matches!(
            state,
            "synchronizing" | "live" | "historical" | "unavailable"
        ),
        "unknown transcript state"
    );
    ensure!(
        value["live"] == (state == "live")
            && !(value["live"] == true && value["historical"] == true)
            && (state != "historical" || value["historical"] == true),
        "inconsistent transcript state flags"
    );
    let requests = value["requests"]
        .as_array()
        .context("transcript requests must be a list")?;
    ensure!(
        requests.len() <= 8 && requests.iter().all(Value::is_object),
        "invalid or oversized pending request list"
    );
    if let Some(selection) = value.get("modelSelection").filter(|v| !v.is_null()) {
        validate_selection(selection)?;
    }
    if let Some(lineage) = value.get("lineage").filter(|v| !v.is_null()) {
        ensure!(lineage.is_object(), "invalid lineage");
        for field in ["threadId", "parentThreadId", "rootThreadId"] {
            if lineage.get(field).is_some_and(|v| !v.is_null()) {
                id(lineage, field, 4096)?;
            }
        }
    }
    Ok(())
}
fn decode_rows(value: &Value, field: &str) -> Result<Vec<Row>> {
    let rows = value[field]
        .as_array()
        .with_context(|| format!("transcript {field} must be a list"))?;
    ensure!(rows.len() <= MAX_ROWS, "too many transcript rows");
    ensure!(
        serde_json::to_vec(rows)?.len() <= MAX_ROWS_BYTES,
        "transcript rows exceed 144 KiB"
    );
    let rows = rows
        .iter()
        .map(|v| serde_json::from_value(v.clone()).context("invalid transcript row"))
        .collect::<Result<Vec<Row>>>()?;
    validate_rows(&rows)?;
    Ok(rows)
}
fn validate_rows(rows: &[Row]) -> Result<()> {
    ensure!(
        rows.len() <= MAX_ROWS && serde_json::to_vec(rows)?.len() <= MAX_ROWS_BYTES,
        "transcript row count or byte limit exceeded"
    );
    let mut seen = HashSet::new();
    for row in rows {
        ensure!(
            valid_id(&row.key, 8193)
                && valid_id(&row.source_thread_id, 4096)
                && valid_id(&row.item_id, 4096)
                && row.key == format!("{}/{}", row.source_thread_id, row.item_id)
                && seen.insert(&row.key),
            "invalid or duplicate source-qualified row identity"
        );
        ensure!(
            row.kind.len() <= 256
                && row.text.len() <= MAX_TEXT_BYTES
                && row.attachment_count <= MAX_INTEGER
                && row.position.is_none_or(|n| n <= MAX_INTEGER),
            "invalid or oversized transcript row"
        );
        ensure!(
            row.message_id
                .as_deref()
                .is_none_or(|id| valid_id(id, 4096))
                && row.run_id.as_deref().is_none_or(|id| valid_id(id, 4096)),
            "invalid row message/run identity"
        );
    }
    Ok(())
}
fn keys(value: &Value, field: &str) -> Result<Vec<String>> {
    let list = value[field]
        .as_array()
        .with_context(|| format!("{field} must be a list"))?;
    ensure!(list.len() <= MAX_ROWS, "too many row keys");
    let mut seen = HashSet::new();
    let mut keys = Vec::new();
    for key in list {
        let key = key.as_str().context("row key must be text")?;
        ensure!(
            valid_id(key, 8193) && seen.insert(key),
            "invalid or duplicate row key"
        );
        keys.push(key.into());
    }
    Ok(keys)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn row(thread: &str, id: &str, text: &str) -> Value {
        json!({"key":format!("{thread}/{id}"),"sourceThreadId":thread,"itemId":id,"kind":"assistant_message","text":text,"streaming":false,"truncated":false,"attachmentCount":0})
    }
    fn snapshot(revision: u64) -> Value {
        json!({"revision":revision,"threadId":"a","title":"Example","model":"model","status":"idle","state":"live","hasMore":false,"limited":false,"live":true,"historical":false,"requests":[],"items":[row("a","one","first")]})
    }
    fn delta(base: u64, revision: u64) -> Value {
        let mut metadata = snapshot(base);
        metadata.as_object_mut().unwrap().remove("revision");
        metadata.as_object_mut().unwrap().remove("items");
        json!({"threadId":"a","baseRevision":base,"revision":revision,"upserts":[row("a","one","updated")],"removed":[],"order":["a/one"],"metadata":metadata})
    }
    #[test]
    fn delta_is_atomic_and_requires_exact_revision() {
        let mut projection = Projection::default();
        projection.apply_snapshot(snapshot(1), "a").unwrap();
        projection.apply_delta(delta(1, 2)).unwrap();
        assert_eq!(projection.snapshot().unwrap().items[0].text, "updated");
        let before = projection.snapshot().unwrap().clone();
        assert!(projection.apply_delta(delta(3, 4)).is_err());
        assert_eq!(projection.snapshot(), Some(&before));
        assert!(projection.needs_resnapshot);
        assert!(projection.apply_delta(delta(2, 3)).is_err());
        projection.apply_snapshot(snapshot(4), "a").unwrap();
        assert!(!projection.needs_resnapshot);
    }
    #[test]
    fn duplicate_order_wrong_source_and_oversize_never_replace_view() {
        let mut projection = Projection::default();
        projection.apply_snapshot(snapshot(1), "a").unwrap();
        let mut invalid = delta(1, 2);
        invalid["order"] = json!(["a/one", "a/one"]);
        assert!(projection.apply_delta(invalid).is_err());
        let mut wrong = snapshot(3);
        wrong["threadId"] = json!("b");
        assert!(projection.apply_snapshot(wrong, "a").is_err());
        let mut huge = snapshot(4);
        huge["items"][0]["text"] = json!("x".repeat(MAX_TEXT_BYTES + 1));
        assert!(projection.apply_snapshot(huge, "a").is_err());
        assert_eq!(projection.snapshot().unwrap().revision, 1);
    }
    #[test]
    fn reconnect_restarts_revision_and_borrowed_rows_keep_source_identity() {
        let mut projection = Projection::default();
        projection.apply_snapshot(snapshot(99), "a").unwrap();
        projection.select("a");
        let mut page = snapshot(1);
        page["items"] = json!([row("ancestor", "one", "inherited")]);
        projection.apply_snapshot(page, "a").unwrap();
        assert_eq!(
            projection.snapshot().unwrap().items[0].source_thread_id,
            "ancestor"
        );
        assert!(!projection.apply_snapshot(snapshot(1), "a").unwrap());
    }
}
