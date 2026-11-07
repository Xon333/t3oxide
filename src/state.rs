//! Private, fsynced client drafts and exact unresolved operation identities.
//! This is not a transcript store or an execution queue.
use crate::config::{default_directory, read_bounded, valid_id};
use crate::wire::{MAX_INPUT_BYTES, is_mutation, new_id};
use anyhow::{Context, Result, bail, ensure};
use serde_json::{Map, Value, json};
use sha2::{Digest, Sha256};
use std::collections::HashSet;
use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::os::fd::AsRawFd;
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

pub const MAX_STATE_BYTES: usize = 2 * 1024 * 1024;
pub const MAX_DRAFT_BYTES: usize = 64 * 1024;
const MAX_THREAD_READS: usize = 1024;

#[derive(Clone, Debug)]
pub struct PendingRecord {
    pub environment_id: String,
    pub stop: bool,
    pub record: Value,
}

pub struct State {
    path: PathBuf,
    _lock: File,
    saved: Mutex<Value>,
}

impl Drop for State {
    fn drop(&mut self) {
        // A helper's fork can retain this open-file description until exec,
        // even with close-on-exec. Release ownership before closing our file.
        // SAFETY: the descriptor is owned and valid; LOCK_UN takes no pointers.
        unsafe { libc::flock(self._lock.as_raw_fd(), libc::LOCK_UN) };
    }
}

pub fn default_state_path() -> PathBuf {
    default_directory().join("rust-state.json")
}

fn object<'a>(value: &'a Value, key: &str) -> Result<&'a Map<String, Value>> {
    value
        .get(key)
        .and_then(Value::as_object)
        .with_context(|| format!("saved {key} must be an object"))
}
fn object_mut<'a>(value: &'a mut Value, key: &str) -> Result<&'a mut Map<String, Value>> {
    value
        .get_mut(key)
        .and_then(Value::as_object_mut)
        .with_context(|| format!("saved {key} must be an object"))
}
fn key(environment: &str, thread: &str) -> String {
    format!("{environment}/{thread}")
}
fn lane(stop: bool) -> &'static str {
    if stop { "pendingStops" } else { "pending" }
}

impl State {
    /// Import the former client state once, without changing its file or host journals.
    /// Existing Rust state always wins, including an intentionally empty state.
    pub fn open(path: &Path) -> Result<Self> {
        let directory = path
            .parent()
            .filter(|p| !p.as_os_str().is_empty())
            .unwrap_or_else(|| Path::new("."));
        fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(directory)?;
        ensure!(!path.is_symlink(), "state path must not be a symlink");
        let lock_path = PathBuf::from(format!("{}.lock", path.display()));
        ensure!(!lock_path.is_symlink(), "state lock must not be a symlink");
        let lock = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .mode(0o600)
            .open(lock_path)?;
        // SAFETY: valid owned file descriptor and flock takes no pointers.
        ensure!(
            unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) } == 0,
            "another Rust client owns this draft/recovery file"
        );
        let source = directory.join("go-drafts.json");
        let (mut saved, imported) = if path.exists() {
            (
                serde_json::from_slice(&read_bounded(path, MAX_STATE_BYTES)?).with_context(
                    || format!("preserve and repair invalid state {}", path.display()),
                )?,
                false,
            )
        } else if source.exists() && source != path {
            let source_lock = PathBuf::from(format!("{}.lock", source.display()));
            ensure!(
                !source_lock.is_symlink(),
                "source state lock must not be a symlink"
            );
            let import_lock = OpenOptions::new()
                .read(true)
                .write(true)
                .create(true)
                .truncate(false)
                .mode(0o600)
                .open(source_lock)?;
            // The Go owner uses the same lock. Do not race an active migration source.
            ensure!(
                unsafe { libc::flock(import_lock.as_raw_fd(), libc::LOCK_SH | libc::LOCK_NB) } == 0,
                "close the previous client before importing its drafts and recovery records"
            );
            let bytes = read_bounded(&source, MAX_STATE_BYTES)?;
            let mut value: Value = serde_json::from_slice(&bytes)
                .context("original drafts/actions could not be imported; original preserved")?;
            import_prompt_sidecar(&source, &bytes, &mut value)?;
            import_inbox_preferences(&source, &mut value)?;
            (value, true)
        } else {
            (json!({"drafts":{}, "pending":{}, "pendingStops":{}}), false)
        };
        normalize(&mut saved)?;
        validate(&saved)?;
        let state = Self {
            path: path.to_owned(),
            _lock: lock,
            saved: Mutex::new(saved),
        };
        if imported {
            state.update(|_| Ok(()))?;
        }
        Ok(state)
    }

    pub fn path(&self) -> &Path {
        &self.path
    }
    pub fn snapshot(&self) -> Value {
        self.saved.lock().unwrap().clone()
    }
    /// Retain observed substantive events across lifecycle-only metadata and restart.
    pub fn observe_thread_activity(
        &self,
        environment: &str,
        events: &[(String, String)],
    ) -> Result<()> {
        let changed = {
            let saved = self.saved.lock().unwrap();
            events.iter().any(|(thread, generation)| {
                observed_generation(&saved, environment, thread) != Some(generation.as_str())
            })
        };
        if !changed {
            return Ok(());
        }
        self.update(|saved| {
            let observed = saved["threadActivity"]
                .as_array_mut()
                .context("invalid thread activity")?;
            for (thread, generation) in events {
                observed.retain(|event| {
                    event["environmentId"] != environment || event["threadId"] != *thread
                });
                observed.push(
                    json!({"environmentId":environment,"threadId":thread,"generation":generation}),
                );
            }
            if observed.len() > MAX_THREAD_READS {
                let expired: Vec<_> = observed
                    .drain(..observed.len() - MAX_THREAD_READS)
                    .collect();
                // Losing a newer observation must not revive an older read marker.
                // Expire both compatibility marker sets in this same transaction.
                for field in ["threadReads", "completionReads"] {
                    saved[field]
                        .as_array_mut()
                        .context("invalid thread read markers")?
                        .retain(|read| {
                            !expired.iter().any(|event| {
                                read["environmentId"] == event["environmentId"]
                                    && read["threadId"] == event["threadId"]
                            })
                        });
                }
            }
            Ok(())
        })
    }
    /// Reading qualifies one exact source event and never changes native state.
    pub fn is_thread_unread(
        &self,
        environment: &str,
        thread: &str,
        generation: Option<&str>,
    ) -> bool {
        let saved = self.saved.lock().unwrap();
        let generation = generation.or_else(|| observed_generation(&saved, environment, thread));
        !["threadReads", "completionReads"].iter().any(|field| {
            saved[*field].as_array().is_some_and(|reads| {
                reads.iter().any(|read| {
                    read["environmentId"] == environment
                        && read["threadId"] == thread
                        && generation.is_none_or(|generation| read["generation"] == generation)
                })
            })
        })
    }

    /// Persist an explicit verified open; absent events record only the open.
    pub fn mark_thread_read(
        &self,
        environment: &str,
        thread: &str,
        generation: Option<&str>,
    ) -> Result<()> {
        ensure!(
            valid_id(environment, 4096) && valid_id(thread, 4096),
            "read marker needs exact source identity"
        );
        let generation = generation.map(str::to_owned).or_else(|| {
            observed_generation(&self.saved.lock().unwrap(), environment, thread).map(str::to_owned)
        });
        if !self.is_thread_unread(environment, thread, generation.as_deref()) {
            return Ok(());
        }
        self.update(|value| {
            let reads = value["threadReads"]
                .as_array_mut()
                .context("invalid thread reads")?;
            let read =
                json!({"environmentId":environment,"threadId":thread,"generation":generation});
            if !reads.contains(&read) {
                reads.push(read);
                if reads.len() > MAX_THREAD_READS {
                    reads.remove(0);
                }
            }
            Ok(())
        })
    }
    pub fn completion_read(&self, environment: &str, thread: &str, generation: &str) -> bool {
        self.saved.lock().unwrap()["completionReads"]
            .as_array()
            .is_some_and(|reads| {
                reads.iter().any(|read| {
                    read["environmentId"] == environment
                        && read["threadId"] == thread
                        && read["generation"] == generation
                })
            })
    }
    pub fn save_completion_read(
        &self,
        environment: &str,
        thread: &str,
        generation: &str,
    ) -> Result<()> {
        if self.completion_read(environment, thread, generation) {
            return Ok(());
        }
        self.update(|value| {
            let reads = value["completionReads"]
                .as_array_mut()
                .context("invalid completion reads")?;
            reads.push(
                json!({"environmentId":environment,"threadId":thread,"generation":generation}),
            );
            if reads.len() > 512 {
                reads.remove(0);
            }
            Ok(())
        })
    }

    /// One copy-on-write transaction. Unknown schema fields survive every commit.
    pub fn update(&self, change: impl FnOnce(&mut Value) -> Result<()>) -> Result<()> {
        let mut saved = self.saved.lock().unwrap();
        let mut next = saved.clone();
        change(&mut next)?;
        normalize(&mut next)?;
        validate(&next)?;
        let mut encoded = serde_json::to_vec_pretty(&next)?;
        encoded.push(b'\n');
        ensure!(
            encoded.len() <= MAX_STATE_BYTES,
            "saved drafts/actions exceed 2 MiB; previous state preserved"
        );
        if let Err(error) = atomic_write(&self.path, &encoded) {
            // A directory fsync may fail after rename. Preserve that published
            // state in memory too, so a later transaction cannot erase its journal.
            if fs::read(&self.path).ok().as_deref() == Some(encoded.as_slice()) {
                *saved = next;
            }
            return Err(error);
        }
        *saved = next;
        Ok(())
    }

    pub fn draft(&self, environment: &str, thread: &str) -> String {
        self.saved.lock().unwrap()["drafts"][key(environment, thread)]
            .as_str()
            .unwrap_or("")
            .to_owned()
    }
    pub fn set_draft(&self, environment: &str, thread: &str, text: &str) -> Result<()> {
        ensure!(text.len() <= MAX_DRAFT_BYTES, "draft exceeds 64 KiB");
        ensure!(
            valid_id(environment, 4096) && !thread.is_empty() && thread.len() <= 8192,
            "draft needs exact source identity"
        );
        self.update(|value| {
            let drafts = object_mut(value, "drafts")?;
            if text.is_empty() {
                drafts.remove(&key(environment, thread));
            } else {
                drafts.insert(key(environment, thread), json!(text));
            }
            Ok(())
        })
    }
    pub fn clear_draft_if_equal(&self, environment: &str, thread: &str, text: &str) -> Result<()> {
        self.update(|value| {
            let drafts = object_mut(value, "drafts")?;
            if drafts
                .get(&key(environment, thread))
                .and_then(Value::as_str)
                == Some(text)
            {
                drafts.remove(&key(environment, thread));
            }
            Ok(())
        })
    }
    pub fn pending(&self, environment: &str, stop: bool) -> Option<Value> {
        self.saved.lock().unwrap()[lane(stop)]
            .get(environment)
            .cloned()
    }
    pub fn has_pending(&self) -> bool {
        let saved = self.saved.lock().unwrap();
        ["pending", "pendingStops"]
            .iter()
            .any(|key| saved[key].as_object().is_some_and(|v| !v.is_empty()))
    }
    pub fn pending_records(&self) -> Vec<PendingRecord> {
        let saved = self.saved.lock().unwrap();
        let mut records = Vec::new();
        for stop in [false, true] {
            if let Some(values) = saved[lane(stop)].as_object() {
                for (environment_id, record) in values {
                    records.push(PendingRecord {
                        environment_id: environment_id.clone(),
                        stop,
                        record: record.clone(),
                    });
                }
            }
        }
        records.sort_by(|a, b| (&a.environment_id, a.stop).cmp(&(&b.environment_id, b.stop)));
        records
    }
    /// Refuses replacement of any unresolved operation, even if its ID is reused.
    pub fn save_pending(&self, environment: &str, stop: bool, record: Value) -> Result<()> {
        self.stage_pending(environment, stop, record, false)
    }

    /// Persist a new send and its recall before starting a separate next draft.
    /// A rejection may retire the operation, but never its recoverable human text.
    pub fn save_send_detaching_draft(&self, environment: &str, record: Value) -> Result<()> {
        self.stage_pending(environment, false, record, true)
    }

    fn stage_pending(
        &self,
        environment: &str,
        stop: bool,
        record: Value,
        detach_draft: bool,
    ) -> Result<()> {
        validate_record(environment, &record)?;
        ensure!(
            !stop || record["op"] == "stop",
            "only Stop may use the reserved recovery lane"
        );
        self.update(|value| {
            if detach_draft {
                let thread = record["threadId"].as_str().context("send needs a thread")?;
                let message = record["message"].as_str().context("send needs text")?;
                ensure!(
                    record["op"] == "send"
                        && value[lane(stop)].get(environment).is_none()
                        && value["drafts"][key(environment, thread)] == message,
                    "submitted draft changed; preserve it before sending"
                );
                remember_prompt(value, environment, thread, "attempt", message)?;
                object_mut(value, "drafts")?.remove(&key(environment, thread));
            }
            if record["op"] == "stop" {
                if stop {
                    ensure!(value["pending"].get(environment).is_none_or(|v|v["op"] != "stop"), "an older Stop remains saved in the ordinary lane; recover its exact identity first");
                } else {
                    ensure!(value["pending"].get(environment) == Some(&record), "new Stop operations must use the reserved recovery lane");
                }
            }
            let pending = object_mut(value, lane(stop))?;
            if let Some(old) = pending.get(environment) { ensure!(old == &record, "an unresolved operation already occupies this lane; preserve its exact ID and payload"); }
            pending.insert(environment.to_owned(), record.clone());
            update_batch_receipt(value, &record, "Needs recovery")?;
            Ok(())
        })
    }
    /// Only a definite rejection (or an explicitly reviewed local-clear action) may call this.
    pub fn clear_pending(&self, environment: &str, stop: bool, id: &str) -> Result<()> {
        self.update(|value| {
            let pending = object_mut(value, lane(stop))?;
            if let Some(old) = pending.get(environment) {
                ensure!(
                    old["id"] == id,
                    "saved operation changed; refusing to clear another action"
                );
            }
            pending.remove(environment);
            Ok(())
        })
    }
    pub fn reject_pending(&self, environment: &str, stop: bool, id: &str) -> Result<()> {
        self.update(|value| {
            if let Some(old) = value[lane(stop)].get(environment).cloned() {
                ensure!(
                    old["id"] == id,
                    "saved operation changed; refusing to reject another action"
                );
                update_batch_receipt(value, &old, "Remaining draft")?;
                object_mut(value, lane(stop))?.remove(environment);
            }
            Ok(())
        })
    }
    /// Atomically retire this exact operation and only its unchanged submitted draft.
    pub fn acknowledge(
        &self,
        environment: &str,
        stop: bool,
        id: &str,
        thread: &str,
        message: &str,
    ) -> Result<()> {
        self.acknowledge_preserving_draft(environment, stop, id, thread, message, None)
    }

    /// A newer cached edit, including a same-byte replacement, is committed in
    /// the same fsynced transaction as acknowledgement and override retirement.
    /// `None` retains the ordinary exact-submitted-draft clearing semantics.
    pub fn acknowledge_preserving_draft(
        &self,
        environment: &str,
        stop: bool,
        id: &str,
        thread: &str,
        message: &str,
        draft_after_ack: Option<&str>,
    ) -> Result<()> {
        ensure!(
            draft_after_ack.is_none_or(|text| text.len() <= MAX_DRAFT_BYTES),
            "draft exceeds 64 KiB"
        );
        self.update(|value| {
            let old = value[lane(stop)]
                .get(environment)
                .cloned()
                .context("no saved operation to acknowledge")?;
            ensure!(
                old["id"] == id && (thread.is_empty() || old["threadId"] == thread),
                "saved operation identity changed; refusing another action's acknowledgement"
            );
            let op = old["op"].as_str().unwrap_or("");
            let check_follow_up = old["t3oxideCheckFollowUp"].as_str();
            object_mut(value, lane(stop))?.remove(environment);
            if op == "send" {
                if !message.is_empty() && old["message"] == message {
                    remember_prompt(value, environment, thread, "sent", message)?;
                }
                let overrides = object_mut(value, "modelOverrides")?;
                let override_key = key(environment, thread);
                if check_follow_up.is_none()
                    && overrides.get(&override_key).is_some_and(|v| {
                        v["id"] == old["modelOverrideId"] && !old["modelOverrideId"].is_null()
                    })
                {
                    overrides.remove(&override_key);
                }
            }
            if (op == "send" && check_follow_up.is_none()) || op == "launch" {
                let draft_thread = if op == "launch" {
                    format!("\0new/{thread}")
                } else {
                    thread.into()
                };
                let drafts = object_mut(value, "drafts")?;
                if !message.is_empty()
                    && drafts
                        .get(&key(environment, &draft_thread))
                        .and_then(Value::as_str)
                        == Some(message)
                {
                    drafts.remove(&key(environment, &draft_thread));
                }
            }
            if op == "launch" || op == "launch-project" {
                if let Some(form_key) = old["rustLaunchFormKey"].as_str() {
                    let ids: Vec<String> = serde_json::from_str(form_key)
                        .context("invalid saved launch form source")?;
                    ensure!(
                        ids.len() == 3 && ids[1] == environment && old["projectId"] == ids[2],
                        "launch form acknowledgement changed its original source"
                    );
                    if let Some(form) = value["rustLaunchForms"].get(form_key) {
                        let hash = format!("{:x}", Sha256::digest(serde_json::to_vec(form)?));
                        if old["rustLaunchFormDigest"] == hash {
                            value["rustLaunchForms"]
                                .as_object_mut()
                                .unwrap()
                                .remove(form_key);
                        }
                    }
                }
                let source = old["formSource"].as_str().unwrap_or(thread);
                clear_matching_launch(value, environment, source, &old)?;
                if old["compactMessageDraft"] == true {
                    clear_matching_launch(value, "t3oxide-client", "new-message", &old)?;
                }
                if let Some(imported_key) = old["importedLaunchKey"].as_str() {
                    ensure!(
                        imported_key == "t3oxide-client/new-message"
                            || imported_key.starts_with(&format!("{environment}/")),
                        "imported launch acknowledgement has a different original source"
                    );
                    if let Some(form) = value["launchDrafts"].get(imported_key).cloned() {
                        let hash = format!("{:x}", Sha256::digest(serde_json::to_vec(&form)?));
                        if old["importedLaunchDigest"] == hash {
                            object_mut(value, "launchDrafts")?.remove(imported_key);
                            let (form_environment, form_source) =
                                if imported_key == "t3oxide-client/new-message" {
                                    ("t3oxide-client", "new-message")
                                } else {
                                    (environment, &imported_key[environment.len() + 1..])
                                };
                            let draft_key = key(form_environment, &format!("\0new/{form_source}"));
                            if value["drafts"][&draft_key] == form["message"] {
                                object_mut(value, "drafts")?.remove(&draft_key);
                            }
                        }
                    }
                }
                update_batch_receipt(value, &old, "Acknowledged")?;
                advance_batch_draft(value, &old)?;
            }
            if let Some(text) = draft_after_ack {
                ensure!(
                    op == "send",
                    "only a human send acknowledgement can retain a newer composer draft"
                );
                let drafts = object_mut(value, "drafts")?;
                if text.is_empty() {
                    drafts.remove(&key(environment, thread));
                } else {
                    drafts.insert(key(environment, thread), json!(text));
                }
            }
            if let Some(attempt) = check_follow_up {
                let record = value["checkAttempts"]
                    .get_mut(attempt)
                    .context("Check follow-up lost its retained attempt")?;
                ensure!(
                    record["target"]["environmentId"] == environment
                        && record["target"]["threadId"] == thread
                        && record["followUp"] == message,
                    "Check acknowledgement changed its original target or text"
                );
                record["phase"] = json!("acknowledged");
                record["operationId"] = json!(id);
            }
            Ok(())
        })
    }

    pub fn remember_prompt(
        &self,
        environment: &str,
        thread: &str,
        kind: &str,
        text: &str,
    ) -> Result<()> {
        self.update(|value| remember_prompt(value, environment, thread, kind, text))
    }
}

fn observed_generation<'a>(saved: &'a Value, environment: &str, thread: &str) -> Option<&'a str> {
    saved["threadActivity"].as_array()?.iter().rev()
        .find(|event| event["environmentId"] == environment && event["threadId"] == thread)?["generation"].as_str()
}

fn normalize(value: &mut Value) -> Result<()> {
    let root = value.as_object_mut().context("state must be an object")?;
    ensure!(
        root.get("drafts").is_some_and(Value::is_object)
            && root.get("pending").is_some_and(Value::is_object),
        "unsupported saved state; drafts/actions preserved"
    );
    for name in [
        "pendingStops",
        "modelOverrides",
        "modelPresets",
        "launchDrafts",
        "batchReceipts",
    ] {
        if root.get(name).is_none_or(Value::is_null) {
            root.insert(name.into(), json!({}));
        }
    }
    if root.get("promptHistory").is_none_or(Value::is_null) {
        root.insert("promptHistory".into(), json!([]));
    }
    if root.get("completionReads").is_none_or(Value::is_null) {
        root.insert("completionReads".into(), json!([]));
    }
    if root.get("threadReads").is_none_or(Value::is_null) {
        root.insert("threadReads".into(), json!([]));
    }
    if root.get("threadActivity").is_none_or(Value::is_null) {
        root.insert("threadActivity".into(), json!([]));
    }
    Ok(())
}

fn validate_record(environment: &str, record: &Value) -> Result<()> {
    ensure!(
        valid_id(environment, 4096) && record.is_object() && record["environmentId"] == environment,
        "saved operation environment does not match its key"
    );
    for field in ["id", "threadId", "op"] {
        ensure!(
            record[field].as_str().is_some_and(|v| valid_id(v, 4096)),
            "saved operation has invalid {field}"
        );
    }
    ensure!(
        is_mutation(record["op"].as_str().unwrap_or("")),
        "unsupported saved operation"
    );
    ensure!(
        serde_json::to_vec(record)?.len() <= MAX_INPUT_BYTES,
        "saved operation exceeds 96 KiB"
    );
    Ok(())
}

fn validate(value: &Value) -> Result<()> {
    let observed = value["threadActivity"]
        .as_array()
        .context("invalid thread activity")?;
    ensure!(
        observed.len() <= MAX_THREAD_READS,
        "thread activity limit exceeded"
    );
    let mut sources = HashSet::new();
    for event in observed {
        let environment = event["environmentId"].as_str().unwrap_or("");
        let thread = event["threadId"].as_str().unwrap_or("");
        let generation = event["generation"].as_str().unwrap_or("");
        ensure!(
            valid_id(environment, 4096)
                && valid_id(thread, 4096)
                && generation.len() == 64
                && generation
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
                && sources.insert((environment, thread)),
            "invalid thread activity identity"
        );
    }
    let thread_reads = value["threadReads"]
        .as_array()
        .context("invalid thread reads")?;
    ensure!(
        thread_reads.len() <= MAX_THREAD_READS,
        "thread read limit exceeded"
    );
    let mut seen = HashSet::new();
    for read in thread_reads {
        let environment = read["environmentId"].as_str().unwrap_or("");
        let thread = read["threadId"].as_str().unwrap_or("");
        let generation = read["generation"].as_str();
        ensure!(
            valid_id(environment, 4096)
                && valid_id(thread, 4096)
                && (read["generation"].is_null()
                    || generation.is_some_and(|generation| {
                        generation.len() == 64
                            && generation
                                .bytes()
                                .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
                    }))
                && seen.insert((environment, thread, generation)),
            "invalid thread read identity"
        );
    }
    let reads = value["completionReads"]
        .as_array()
        .context("invalid completion reads")?;
    ensure!(reads.len() <= 512, "completion read limit exceeded");
    let mut seen = HashSet::new();
    for read in reads {
        let environment = read["environmentId"].as_str().unwrap_or("");
        let thread = read["threadId"].as_str().unwrap_or("");
        let generation = read["generation"].as_str().unwrap_or("");
        ensure!(
            valid_id(environment, 4096)
                && valid_id(thread, 4096)
                && generation.len() == 64
                && generation
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
                && seen.insert((environment, thread, generation)),
            "invalid completion read identity"
        );
    }
    let drafts = object(value, "drafts")?;
    ensure!(
        drafts.len() <= 40,
        "saved state exceeds 40 draft slots; preserve existing text"
    );
    for (key, text) in drafts {
        ensure!(
            !key.is_empty()
                && key.len() <= 12288
                && text.as_str().is_some_and(|v| v.len() <= MAX_DRAFT_BYTES),
            "invalid or oversized saved draft"
        );
    }
    for stop in [false, true] {
        let records = object(value, lane(stop))?;
        ensure!(records.len() <= 8, "saved state exceeds operation limits");
        for (environment, record) in records {
            validate_record(environment, record)?;
            ensure!(
                !stop || record["op"] == "stop",
                "non-Stop operation in Stop recovery lane"
            );
        }
    }
    let launches = object(value, "launchDrafts")?;
    ensure!(launches.len() <= 40, "launch draft limit exceeded");
    for (key, draft) in launches {
        ensure!(
            !key.is_empty() && key.len() <= 8192 && draft.is_object(),
            "invalid launch draft identity"
        );
        for (field, limit) in [
            ("title", 512),
            ("message", MAX_DRAFT_BYTES),
            ("projectId", 4096),
            ("workspace", 4096),
            ("runtimeMode", 64),
            ("interactionMode", 64),
        ] {
            ensure!(
                draft
                    .get(field)
                    .is_none_or(|v| v.as_str().is_some_and(|s| s.len() <= limit)),
                "invalid launch draft {field}"
            );
        }
        if let Some(strategy) = draft.get("workspaceStrategy").filter(|v| !v.is_null()) {
            validate_workspace(strategy)?;
        }
        if let Some(selection) = draft.get("selection").filter(|v| !v.is_null()) {
            validate_selection(selection)?;
        }
    }
    let overrides = object(value, "modelOverrides")?;
    ensure!(overrides.len() <= 40, "model override limit exceeded");
    for v in overrides.values() {
        ensure!(
            v["id"].as_str().is_some_and(|s| valid_id(s, 128)),
            "invalid saved model override identity"
        );
        validate_selection(&v["selection"])?;
    }
    let presets = object(value, "modelPresets")?;
    ensure!(
        presets.len() <= 8,
        "model preset environment limit exceeded"
    );
    for (environment, list) in presets {
        ensure!(valid_id(environment, 4096), "invalid preset environment");
        let list = list.as_array().context("presets must be a list")?;
        ensure!(list.len() <= 32, "preset limit exceeded");
        let mut seen = HashSet::new();
        for preset in list {
            let name = preset["name"].as_str().context("missing preset name")?;
            ensure!(
                valid_id(name, 80) && seen.insert(name.to_lowercase()),
                "invalid or duplicate preset name"
            );
            validate_selection(&preset["selection"])?;
        }
    }
    if let Some(destination) = value.get("creationDestination").filter(|v| !v.is_null()) {
        for field in ["profileId", "environmentId"] {
            ensure!(
                destination[field]
                    .as_str()
                    .is_some_and(|s| valid_id(s, 4096)),
                "creation destination needs exact source identities"
            );
        }
        ensure!(
            destination.get("projectId").is_none_or(|v| v
                .as_str()
                .is_some_and(|s| s.is_empty() || valid_id(s, 4096))),
            "invalid destination project"
        );
    }
    if let Some(preferences) = value.get("browserPreferences").filter(|v| !v.is_null()) {
        let ids = preferences["profileIds"]
            .as_array()
            .context("browser profile scope must be a list")?;
        let mut seen = HashSet::new();
        ensure!(ids.len() <= 40, "browser preference limit exceeded");
        for id in ids {
            ensure!(
                id.as_str()
                    .is_some_and(|s| valid_id(s, 4096) && seen.insert(s)),
                "invalid or duplicate browser profile scope"
            );
        }
        let last = preferences
            .get("lastProfileId")
            .and_then(Value::as_str)
            .unwrap_or("");
        ensure!(
            last.is_empty() || seen.contains(last),
            "last browser profile must belong to selected scope"
        );
    }
    let history = value["promptHistory"]
        .as_array()
        .context("prompt history must be a list")?;
    // Imports may retain two interrupted checkpoints. New recall is compacted to 24.
    validate_prompt_entries(history, 48, 1024 * 1024)?;
    let receipts = object(value, "batchReceipts")?;
    ensure!(receipts.len() <= 16, "batch receipt limit exceeded");
    for (id, receipt) in receipts {
        validate_batch_receipt(id, receipt)?;
    }
    if let Some(positions) = value.get("reviewPositions").filter(|v| !v.is_null()) {
        let positions = positions
            .as_array()
            .context("review positions must be a list")?;
        ensure!(positions.len() <= 64, "review position limit exceeded");
        let mut seen = HashSet::new();
        for position in positions {
            for field in [
                "profileId",
                "environmentId",
                "projectId",
                "threadId",
                "digest",
            ] {
                ensure!(
                    position[field].as_str().is_some_and(|s| valid_id(s, 4096)),
                    "review position needs exact source/hash identity"
                );
            }
            ensure!(
                position["digest"].as_str().unwrap().len() == 64
                    && position["offset"].as_u64().is_some_and(|v| v <= 1_000_000),
                "invalid review position"
            );
            ensure!(
                seen.insert(format!(
                    "{}\0{}\0{}\0{}",
                    position["profileId"],
                    position["environmentId"],
                    position["projectId"],
                    position["threadId"]
                )),
                "duplicate review position"
            );
        }
    }
    if let Some(preferences) = value.get("inboxPreferences") {
        validate_inbox_preferences(preferences)?;
    }
    if let Some(forms) = value.get("rustLaunchForms") {
        let forms = forms
            .as_object()
            .context("launch forms must be an object")?;
        ensure!(
            forms.len() <= 40,
            "launch form limit exceeded; existing drafts preserved"
        );
        for (key, form) in forms {
            let ids: Vec<String> =
                serde_json::from_str(key).context("launch form needs exact source identity")?;
            ensure!(
                ids.len() == 3
                    && ids.iter().all(|id| valid_id(id, 4096))
                    && serde_json::to_string(&ids)? == *key
                    && form["projectId"] == ids[2],
                "invalid launch form source"
            );
            ensure!(
                form["title"].as_str().is_some_and(|s| s.len() <= 512)
                    && form["message"]
                        .as_str()
                        .is_some_and(|s| s.len() <= MAX_DRAFT_BYTES),
                "oversized launch form text"
            );
            if !form["selection"].is_null() {
                validate_selection(&form["selection"])?;
            }
            ensure!(
                form["workspace"].is_null()
                    || matches!(form["workspace"].as_str(), Some("local" | "worktree")),
                "invalid staged workspace mode"
            );
            ensure!(
                form.get("revision")
                    .is_none_or(|v| v.as_u64().is_some_and(|n| n <= 9_007_199_254_740_991)),
                "invalid launch form revision"
            );
        }
    }
    if let Some(scope) = value.get("rustBrowserScope") {
        for field in ["profileId", "environmentId", "projectId"] {
            ensure!(
                scope[field].as_str().is_some_and(|id| valid_id(id, 4096)),
                "invalid browsing source"
            );
        }
    }
    if let Some(positions) = value.get("rustReadingPositions") {
        let positions = positions
            .as_object()
            .context("reading coordinates must be an object")?;
        ensure!(positions.len() <= 64, "reading coordinate limit exceeded");
        for (key, position) in positions {
            let ids: Vec<String> =
                serde_json::from_str(key).context("reading coordinate needs exact source")?;
            ensure!(
                ids.len() == 3
                    && ids.iter().all(|id| valid_id(id, 4096))
                    && serde_json::to_string(&ids)? == *key,
                "invalid reading source"
            );
            ensure!(
                position["rowKey"]
                    .as_str()
                    .is_some_and(|s| valid_id(s, 8193))
                    && position["digest"]
                        .as_str()
                        .is_some_and(|s| s.len() == 64 && s.bytes().all(|b| b.is_ascii_hexdigit()))
                    && position["offset"].as_u64().is_some_and(|n| n <= 1_000_000)
                    && position["width"]
                        .as_u64()
                        .is_some_and(|n| n <= u16::MAX.into())
                    && position["follow"].is_boolean(),
                "invalid bounded reading coordinate"
            );
        }
    }
    Ok(())
}

/// Local presentation ranks are separate from native task/run disposition.
/// Import the Go sidecar once under its already-held process lock.
fn import_inbox_preferences(source: &Path, value: &mut Value) -> Result<()> {
    let path = PathBuf::from(format!("{}.inbox-v1.json", source.display()));
    if !path.exists() || value.get("inboxPreferences").is_some() {
        return Ok(());
    }
    let preferences: Value = serde_json::from_slice(&read_bounded(&path, 256 * 1024)?)
        .context("invalid original inbox preferences; source preserved")?;
    validate_inbox_preferences(&preferences)?;
    value["inboxPreferences"] = preferences;
    Ok(())
}

pub fn validate_inbox_preferences(value: &Value) -> Result<()> {
    ensure!(
        value["version"] == 1
            && value["revision"]
                .as_u64()
                .is_some_and(|v| v <= 9_007_199_254_740_991),
        "unsupported inbox presentation preferences"
    );
    for (field, count, max_rank, project) in [
        ("taskPriorities", 1000, 3, false),
        ("projectRanks", 200, 200, true),
    ] {
        let ranks = value[field]
            .as_object()
            .context("inbox ranks must be an object")?;
        ensure!(
            ranks.len() <= count,
            "inbox presentation rank limit exceeded"
        );
        for (key, rank) in ranks {
            let ids: Vec<String> =
                serde_json::from_str(key).context("invalid inbox source identity")?;
            ensure!(
                ids.len() == 3
                    && valid_id(&ids[0], 4096)
                    && valid_id(&ids[1], 4096)
                    && (if project {
                        ids[2].is_empty()
                    } else {
                        valid_id(&ids[2], 4096)
                    })
                    && serde_json::to_string(&ids)? == *key,
                "inbox rank needs exact canonical environment/project/thread identity"
            );
            ensure!(
                rank.as_u64().is_some_and(|n| n <= max_rank),
                "invalid inbox presentation rank"
            );
        }
    }
    ensure!(
        serde_json::to_vec(value)?.len() <= 256 * 1024,
        "inbox preferences exceed 256 KiB"
    );
    Ok(())
}

fn validate_batch_receipt(id: &str, receipt: &Value) -> Result<()> {
    ensure!(
        valid_id(id, 4096)
            && receipt["id"] == id
            && receipt["environmentId"]
                .as_str()
                .is_some_and(|s| valid_id(s, 4096)),
        "invalid batch receipt identity"
    );
    ensure!(
        receipt["source"]
            .as_str()
            .is_some_and(|s| s.starts_with("\0file-many/") && s.len() <= 4096),
        "invalid batch source"
    );
    let original = receipt["original"]
        .as_str()
        .context("invalid original batch text")?;
    let items = receipt["items"].as_array().context("invalid batch items")?;
    ensure!(
        original.len() <= 32 * 1024 && !items.is_empty() && items.len() <= 20,
        "batch receipt exceeds its limits"
    );
    ensure!(
        receipt
            .get("pauseReason")
            .is_none_or(|v| v.as_str().is_some_and(|s| s.len() <= 1024)),
        "invalid batch pause reason"
    );
    ensure!(
        receipt["createdAt"]
            .as_str()
            .is_some_and(|s| !s.is_empty() && s.len() <= 128),
        "invalid batch receipt time"
    );
    let mut previous = 0;
    for item in items {
        let start = item["start"].as_u64().context("invalid batch start")? as usize;
        let end = item["end"].as_u64().context("invalid batch end")? as usize;
        ensure!(
            start == previous && end > start && original.get(start..end).is_some(),
            "invalid batch input range"
        );
        previous = end;
        let state = item["state"].as_str().context("invalid batch state")?;
        ensure!(
            matches!(state, "Remaining draft" | "Acknowledged" | "Needs recovery"),
            "invalid batch disposition"
        );
        for field in ["operationId", "environmentId", "projectId", "threadId"] {
            let text = item.get(field).and_then(Value::as_str).unwrap_or("");
            ensure!(
                text.is_empty() || valid_id(text, 4096),
                "invalid batch action identity"
            );
            ensure!(
                state == "Remaining draft" || !text.is_empty(),
                "batch action lacks exact native identity"
            );
        }
    }
    ensure!(
        original
            .get(previous..)
            .is_some_and(|s| s.trim().is_empty()),
        "batch receipt omits human task text"
    );
    Ok(())
}

pub fn validate_selection(selection: &Value) -> Result<()> {
    ensure!(selection.is_object(), "model selection must be an object");
    ensure!(
        selection["instanceId"]
            .as_str()
            .is_some_and(|s| valid_id(s, 4096)),
        "invalid native provider instance"
    );
    ensure!(
        selection["model"]
            .as_str()
            .is_some_and(|s| valid_id(s, 256) && s.trim() == s),
        "invalid native model"
    );
    if let Some(options) = selection.get("options").filter(|v| !v.is_null()) {
        let options = options.as_array().context("model options must be a list")?;
        ensure!(options.len() <= 64, "model option limit exceeded");
        let mut seen = HashSet::new();
        for option in options {
            let id = option["id"]
                .as_str()
                .context("model option needs identity")?;
            ensure!(
                valid_id(id, 128) && seen.insert(id),
                "invalid or duplicate model option"
            );
            ensure!(
                option["value"].is_boolean()
                    || option["value"].as_str().is_some_and(|s| valid_id(s, 1024)),
                "unsupported model option value"
            );
        }
    }
    ensure!(
        serde_json::to_vec(selection)?.len() <= 16 * 1024,
        "model selection exceeds 16 KiB"
    );
    Ok(())
}
pub fn validate_workspace(value: &Value) -> Result<()> {
    match value["type"].as_str() {
        Some("root") => ensure!(
            value.get("baseRef").is_none_or(|v| v == ""),
            "root workspace cannot carry a base ref"
        ),
        Some("worktree") => ensure!(
            value["baseRef"].as_str().is_some_and(|s| valid_id(s, 4096)
                && s.trim() == s
                && !s.contains(['\0', '\r', '\n'])),
            "worktree needs exact base ref"
        ),
        _ => bail!("invalid native workspace strategy"),
    }
    Ok(())
}

fn atomic_write(path: &Path, encoded: &[u8]) -> Result<()> {
    let parent = path
        .parent()
        .filter(|p| !p.as_os_str().is_empty())
        .unwrap_or_else(|| Path::new("."));
    let temp = parent.join(format!(".t3oxide-state-{}", new_id()));
    let result = (|| -> Result<()> {
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&temp)?;
        file.write_all(encoded)?;
        file.sync_all()?;
        drop(file);
        fs::rename(&temp, path)?;
        File::open(parent)?.sync_all()?;
        Ok(())
    })();
    if result.is_err() {
        let _ = fs::remove_file(temp);
    }
    result.context("could not save drafts/recovery state")
}

fn prompt_entry(environment: &str, thread: &str, kind: &str, text: &str) -> Value {
    let id = format!(
        "{:x}",
        Sha256::digest(format!("{environment}\0{thread}\0{kind}\0{text}").as_bytes())
    );
    json!({"id":id,"environment":environment,"thread":thread,"kind":kind,"text":text})
}
fn validate_prompt_entries(entries: &[Value], count: usize, bytes: usize) -> Result<()> {
    ensure!(entries.len() <= count, "prompt recall count exceeded");
    let mut size = 0;
    let mut seen = HashSet::new();
    for entry in entries {
        let text = entry["text"].as_str().context("invalid prompt text")?;
        size += text.len();
        ensure!(
            text.len() <= MAX_DRAFT_BYTES
                && entry["environment"].is_string()
                && entry["thread"].is_string()
                && matches!(entry["kind"].as_str(), Some("sent" | "attempt" | "cleared")),
            "invalid prompt recall entry"
        );
        ensure!(
            entry["id"].as_str().is_some_and(|s| seen.insert(s)),
            "duplicate prompt recall identity"
        );
        ensure!(
            entry["environment"].as_str().is_some_and(|s| !s.is_empty())
                && entry["thread"].as_str().is_some_and(|s| !s.is_empty()),
            "missing prompt recall source"
        );
        ensure!(
            entry
                == &prompt_entry(
                    entry["environment"].as_str().unwrap(),
                    entry["thread"].as_str().unwrap(),
                    entry["kind"].as_str().unwrap(),
                    text
                ),
            "prompt recall identity mismatch"
        );
    }
    ensure!(size <= bytes, "prompt recall byte limit exceeded");
    Ok(())
}
fn remember_prompt(
    value: &mut Value,
    environment: &str,
    thread: &str,
    kind: &str,
    text: &str,
) -> Result<()> {
    if text.is_empty() {
        return Ok(());
    }
    ensure!(
        matches!(kind, "sent" | "attempt" | "cleared") && text.len() <= MAX_DRAFT_BYTES,
        "invalid prompt recall"
    );
    let entry = prompt_entry(environment, thread, kind, text);
    let old = value["promptHistory"]
        .as_array()
        .cloned()
        .unwrap_or_default();
    let mut entries = vec![entry.clone()];
    let mut size = text.len();
    for existing in old {
        if existing["id"] == entry["id"] {
            continue;
        }
        let next_size = existing["text"].as_str().unwrap_or("").len();
        if entries.len() >= 24 || size + next_size > 512 * 1024 {
            break;
        }
        size += next_size;
        entries.push(existing);
    }
    value["promptHistory"] = json!(entries);
    Ok(())
}
fn import_prompt_sidecar(source: &Path, main: &[u8], value: &mut Value) -> Result<()> {
    let path = PathBuf::from(format!("{}.prompts.json", source.display()));
    if !path.exists() {
        return Ok(());
    }
    let saved: Value = serde_json::from_slice(&read_bounded(&path, 6 * 1024 * 1024)?)?;
    ensure!(
        saved["version"] == 1,
        "unsupported prompt recall checkpoint; original preserved"
    );
    let committed = saved["committed"]
        .as_array()
        .context("invalid committed prompt recall")?;
    let prepared = saved
        .get("prepared")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    validate_prompt_entries(committed, 48, 1024 * 1024)?;
    validate_prompt_entries(&prepared, 24, 512 * 1024)?;
    let checkpoint = saved
        .get("stateSHA256")
        .and_then(Value::as_str)
        .unwrap_or("");
    ensure!(
        (checkpoint.is_empty() && prepared.is_empty())
            || (checkpoint.len() == 64 && checkpoint.bytes().all(|b| b.is_ascii_hexdigit())),
        "invalid prepared prompt checkpoint digest"
    );
    let main_entries = value
        .get("promptHistory")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let digest = format!("{:x}", Sha256::digest(main));
    if saved["stateSHA256"].as_str() == Some(&digest) {
        ensure!(
            prepared == main_entries,
            "prepared prompt recall does not match its main transaction"
        );
        return Ok(());
    }
    let confirmed: HashSet<_> = main_entries
        .iter()
        .chain(committed.iter())
        .filter_map(|v| v["id"].as_str())
        .collect();
    let mut entries = main_entries.clone();
    for mut entry in prepared {
        if entry["kind"] == "sent" && !confirmed.contains(entry["id"].as_str().unwrap_or("")) {
            entry = prompt_entry(
                entry["environment"].as_str().unwrap(),
                entry["thread"].as_str().unwrap(),
                "attempt",
                entry["text"].as_str().unwrap(),
            );
        }
        if !entries.iter().any(|v| v["id"] == entry["id"]) {
            entries.push(entry);
        }
    }
    for entry in committed {
        if !entries.iter().any(|v| v["id"] == entry["id"]) {
            entries.push(entry.clone());
        }
    }
    validate_prompt_entries(&entries, 48, 1024 * 1024)?;
    value["promptHistory"] = json!(entries);
    Ok(())
}

fn clear_matching_launch(
    value: &mut Value,
    environment: &str,
    source: &str,
    old: &Value,
) -> Result<()> {
    let form_key = key(environment, source);
    let Some(draft) = value["launchDrafts"].get(&form_key).cloned() else {
        return Ok(());
    };
    let matches = draft["title"] == old["title"]
        && draft["message"] == old["message"]
        && ((old["op"] == "launch" && draft["selection"] == old["modelSelection"])
            || environment == "t3oxide-client"
            || (draft["projectId"] == old["projectId"]
                && draft["nativeDefaultsRevision"] == old["defaultsRevision"]
                && draft["selection"] == old["modelSelection"]
                && draft["workspaceStrategy"] == old["workspaceStrategy"]));
    if matches {
        object_mut(value, "launchDrafts")?.remove(&form_key);
        let message_key = key(environment, &format!("\0new/{source}"));
        let drafts = object_mut(value, "drafts")?;
        if drafts.get(&message_key) == Some(&draft["message"]) {
            drafts.remove(&message_key);
        }
    }
    Ok(())
}
fn update_batch_receipt(value: &mut Value, record: &Value, status: &str) -> Result<()> {
    let Some(batch) = record.get("batchDraft").filter(|v| v.is_object()) else {
        return Ok(());
    };
    let Some(receipt_id) = batch.get("receiptId") else {
        return Ok(());
    };
    let receipt_id = receipt_id
        .as_str()
        .context("invalid saved batch receipt identity")?;
    let receipt = value["batchReceipts"]
        .get_mut(receipt_id)
        .context("saved batch receipt is missing; preserve original action")?;
    let index = batch["itemIndex"]
        .as_u64()
        .context("invalid saved batch item")? as usize;
    let original = receipt["original"]
        .as_str()
        .context("invalid batch original")?
        .to_owned();
    let source = receipt["source"].clone();
    let environment = receipt["environmentId"].clone();
    let items = receipt["items"]
        .as_array_mut()
        .context("invalid batch items")?;
    ensure!(index < items.len(), "invalid saved batch item index");
    let start = items[index]["start"]
        .as_u64()
        .context("invalid batch range")? as usize;
    let end = items[index]["end"]
        .as_u64()
        .context("invalid batch range")? as usize;
    ensure!(
        batch["environment"] == environment
            && batch["source"] == source
            && original
                .get(start..end)
                .is_some_and(|s| batch["consumed"] == s)
            && original.get(start..).is_some_and(|s| batch["before"] == s),
        "saved batch receipt source/input mismatch"
    );
    if status == "Needs recovery" {
        ensure!(
            items[..index]
                .iter()
                .all(|item| item["state"] == "Acknowledged"),
            "earlier batch item is not acknowledged; further filing stays paused"
        );
    }
    let item = &mut items[index];
    ensure!(
        item["operationId"]
            .as_str()
            .is_none_or(|v| v.is_empty() || record["id"] == v),
        "batch receipt belongs to another operation"
    );
    ensure!(
        item["state"] != "Acknowledged" || status == "Acknowledged",
        "acknowledged batch item cannot be replayed"
    );
    item["state"] = json!(status);
    item["operationId"] = record["id"].clone();
    item["environmentId"] = record["environmentId"].clone();
    item["projectId"] = record["projectId"].clone();
    item["threadId"] = record["newThreadId"].clone();
    Ok(())
}
fn advance_batch_draft(value: &mut Value, record: &Value) -> Result<()> {
    let Some(batch) = record.get("batchDraft") else {
        return Ok(());
    };
    let environment = batch["environment"]
        .as_str()
        .context("invalid batch environment")?;
    let source = batch["source"].as_str().context("invalid batch source")?;
    let before = batch["before"].as_str().context("invalid batch original")?;
    let consumed = batch["consumed"]
        .as_str()
        .context("invalid consumed batch item")?;
    ensure!(
        !environment.is_empty()
            && environment.len() <= 1024
            && source.starts_with("\0file-many/")
            && source.len() <= 4096
            && before.len() <= 32 * 1024
            && !consumed.is_empty()
            && before.starts_with(consumed)
            && (consumed == before || consumed.ends_with('\n'))
            && consumed.lines().filter(|v| !v.trim().is_empty()).count() == 1,
        "invalid saved batch draft binding"
    );
    let drafts = object_mut(value, "drafts")?;
    if drafts
        .get(&key(environment, source))
        .and_then(Value::as_str)
        == Some(before)
    {
        let rest = &before[consumed.len()..];
        if rest.trim().is_empty() {
            drafts.remove(&key(environment, source));
        } else {
            drafts.insert(key(environment, source), json!(rest));
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;
    fn temp() -> PathBuf {
        let p = std::env::temp_dir().join(format!("t3oxide-state-test-{}", new_id()));
        fs::create_dir(&p).unwrap();
        p
    }
    fn operation(id: &str, op: &str) -> Value {
        json!({"id":id,"environmentId":"env","threadId":"thread","op":op,"message":"submitted"})
    }
    #[test]
    fn source_event_reads_survive_reopen_without_settling_or_losing_recovery() {
        let directory = temp();
        let path = directory.join("rust-state.json");
        let state = State::open(&path).unwrap();
        state
            .set_draft("env", "thread", "unsent human text")
            .unwrap();
        state
            .save_pending("env", false, operation("original", "send"))
            .unwrap();
        state
            .update(|value| {
                value["futureNativePresentation"] = json!({"preserve":"exact"});
                Ok(())
            })
            .unwrap();
        let first = "a".repeat(64);
        let next = "b".repeat(64);
        assert!(state.is_thread_unread("env", "thread", None));
        state
            .mark_thread_read("env", "thread", Some(&first))
            .unwrap();
        assert!(!state.is_thread_unread("env", "thread", None));
        assert!(!state.is_thread_unread("env", "thread", Some(&first)));
        assert!(state.is_thread_unread("env", "thread", Some(&next)));
        assert!(state.is_thread_unread("other-env", "thread", Some(&first)));
        state
            .mark_thread_read("env", "thread", Some(&next))
            .unwrap();
        state
            .mark_thread_read("env", "thread", Some(&next))
            .unwrap();
        assert_eq!(state.snapshot()["threadReads"].as_array().unwrap().len(), 2);
        drop(state);
        let reopened = State::open(&path).unwrap();
        assert!(!reopened.is_thread_unread("env", "thread", Some(&first)));
        assert!(!reopened.is_thread_unread("env", "thread", Some(&next)));
        assert_eq!(reopened.draft("env", "thread"), "unsent human text");
        assert_eq!(reopened.pending("env", false).unwrap()["id"], "original");
        assert_eq!(
            reopened.snapshot()["futureNativePresentation"]["preserve"],
            "exact"
        );
        assert!(reopened.snapshot().get("settled").is_none());
        drop(reopened);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn lifecycle_only_updates_retain_unopened_event_until_exact_read_and_restart() {
        let directory = tempfile::TempDir::new().unwrap();
        let path = directory.path().join("state.json");
        let state = State::open(&path).unwrap();
        let first = "a".repeat(64);
        let next = "b".repeat(64);
        state
            .observe_thread_activity("env", &[("thread".into(), first.clone())])
            .unwrap();
        state
            .mark_thread_read("env", "thread", Some(&first))
            .unwrap();
        assert!(
            !state.is_thread_unread("env", "thread", None),
            "a running shell must not invent an event"
        );
        state
            .observe_thread_activity("env", &[("thread".into(), next.clone())])
            .unwrap();
        assert!(
            state.is_thread_unread("env", "thread", None),
            "starting another run cannot clear unopened G1"
        );
        assert!(state.is_thread_unread("other-env", "thread", None));
        drop(state);
        let state = State::open(&path).unwrap();
        assert!(state.is_thread_unread("env", "thread", None));
        state.mark_thread_read("env", "thread", None).unwrap();
        assert!(!state.is_thread_unread("env", "thread", Some(&next)));
        assert_eq!(state.snapshot()["threadReads"][1]["generation"], next);
        assert!(state.pending_records().is_empty());
    }
    #[test]
    fn state_lock_release_follows_owner_with_an_inherited_helper_descriptor() {
        let directory = tempfile::TempDir::new().unwrap();
        let path = directory.path().join("state.json");
        let state = State::open(&path).unwrap();
        // A fork temporarily retains the same open-file description, even with
        // close-on-exec. Duplicate it here without a timing-dependent child.
        let inherited = state._lock.try_clone().unwrap();
        assert!(
            State::open(&path).is_err(),
            "a live owner remains exclusive"
        );
        drop(state);
        let next =
            State::open(&path).expect("an inherited descriptor cannot extend owner lifetime");
        drop(inherited);
        assert!(
            State::open(&path).is_err(),
            "old descriptor cannot unlock the new owner"
        );
        drop(next);
        assert!(State::open(&path).is_ok());
    }

    #[test]
    fn activity_eviction_expires_stale_read_markers_atomically_across_restart() {
        let directory = tempfile::TempDir::new().unwrap();
        let path = directory.path().join("state.json");
        let state = State::open(&path).unwrap();
        let first = "a".repeat(64);
        let next = "b".repeat(64);
        state
            .mark_thread_read("env", "thread", Some(&first))
            .unwrap();
        state.save_completion_read("env", "thread", &first).unwrap();
        state
            .mark_thread_read("other-env", "thread", Some(&first))
            .unwrap();
        state
            .observe_thread_activity("env", &[("thread".into(), next)])
            .unwrap();
        for start in (0..MAX_THREAD_READS).step_by(200) {
            let page: Vec<_> = (start..(start + 200).min(MAX_THREAD_READS))
                .map(|index| (format!("thread-{index}"), first.clone()))
                .collect();
            state.observe_thread_activity("env", &page).unwrap();
        }
        assert!(state.is_thread_unread("env", "thread", None));
        assert!(!state.completion_read("env", "thread", &first));
        assert!(!state.is_thread_unread("other-env", "thread", None));
        drop(state);
        let state = State::open(&path).unwrap();
        assert!(state.is_thread_unread("env", "thread", None));
        assert!(!state.is_thread_unread("other-env", "thread", None));
        state.mark_thread_read("env", "thread", None).unwrap();
        assert!(!state.is_thread_unread("env", "thread", None));
    }

    #[test]
    fn ack_atomically_preserves_newer_same_byte_draft_and_newer_override() {
        let directory = temp();
        let path = directory.join("rust-state.json");
        let state = State::open(&path).unwrap();
        state.set_draft("env", "thread", "submitted").unwrap();
        let mut record = operation("send-original", "send");
        record["modelOverrideId"] = json!("old-override");
        state.save_pending("env", false, record).unwrap();
        state
            .update(|value| {
                value["modelOverrides"]["env/thread"] =
                    json!({"id":"new-override","selection":{"instanceId":"provider","model":"m"}});
                Ok(())
            })
            .unwrap();
        state
            .acknowledge_preserving_draft(
                "env",
                false,
                "send-original",
                "thread",
                "submitted",
                Some("submitted"),
            )
            .unwrap();
        drop(state);
        let reopened = State::open(&path).unwrap();
        assert_eq!(reopened.draft("env", "thread"), "submitted");
        assert!(reopened.pending("env", false).is_none());
        assert_eq!(
            reopened.snapshot()["modelOverrides"]["env/thread"]["id"],
            "new-override"
        );
        assert_eq!(reopened.snapshot()["promptHistory"][0]["kind"], "sent");
        drop(reopened);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn imports_exact_local_priorities_without_changing_original_sidecar() {
        let directory = temp();
        let source = directory.join("go-drafts.json");
        fs::write(&source, r#"{"drafts":{},"pending":{}}"#).unwrap();
        let key = json!(["env", "project", "thread"]).to_string();
        let preferences =
            json!({"version":1,"revision":7,"taskPriorities":{key.clone():3},"projectRanks":{}})
                .to_string();
        let sidecar = directory.join("go-drafts.json.inbox-v1.json");
        fs::write(&sidecar, &preferences).unwrap();
        let state = State::open(&directory.join("rust-state.json")).unwrap();
        assert_eq!(
            state.snapshot()["inboxPreferences"]["taskPriorities"][&key],
            3
        );
        assert_eq!(fs::read_to_string(sidecar).unwrap(), preferences);
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn imported_form_ack_retires_only_unchanged_original_and_preserves_newer_text() {
        let directory = temp();
        let state = State::open(&directory.join("rust-state.json")).unwrap();
        let key = "env/project-launch/project";
        let form = json!({"title":"Original","message":"old text","projectId":"project"});
        state
            .update(|value| {
                value["launchDrafts"][key] = form.clone();
                Ok(())
            })
            .unwrap();
        let mut record = operation("launch-original", "launch-project");
        record["projectId"] = json!("project");
        record["newThreadId"] = json!("thread");
        record["title"] = json!("Reviewed");
        record["importedLaunchKey"] = json!(key);
        record["importedLaunchDigest"] = json!(format!(
            "{:x}",
            Sha256::digest(serde_json::to_vec(&form).unwrap())
        ));
        state.save_pending("env", false, record).unwrap();
        state
            .update(|value| {
                value["launchDrafts"][key]["message"] = json!("newer text");
                Ok(())
            })
            .unwrap();
        state
            .acknowledge("env", false, "launch-original", "thread", "submitted")
            .unwrap();
        assert_eq!(
            state.snapshot()["launchDrafts"][key]["message"],
            "newer text"
        );
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn locking_ack_and_separate_stop_preserve_concurrent_edits() {
        let directory = temp();
        let path = directory.join("rust-state.json");
        let state = State::open(&path).unwrap();
        assert!(State::open(&path).is_err());
        state.set_draft("env", "thread", "submitted").unwrap();
        state
            .save_pending("env", false, operation("send-1", "send"))
            .unwrap();
        state
            .save_pending("env", true, operation("stop-1", "stop"))
            .unwrap();
        assert!(
            state
                .save_pending("env", false, operation("send-2", "send"))
                .is_err()
        );
        assert!(state.clear_pending("env", false, "other").is_err());
        state.set_draft("env", "thread", "newer edit").unwrap();
        state
            .acknowledge("env", false, "send-1", "thread", "submitted")
            .unwrap();
        assert_eq!(state.draft("env", "thread"), "newer edit");
        assert!(state.pending("env", true).is_some());
        assert_eq!(
            fs::metadata(&path).unwrap().permissions().mode() & 0o777,
            0o600
        );
        drop(state);
        let reopened = State::open(&path).unwrap();
        assert_eq!(reopened.draft("env", "thread"), "newer edit");
        drop(reopened);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn import_retains_unknown_fields_and_does_not_change_original() {
        let directory = temp();
        let source = directory.join("go-drafts.json");
        let original=json!({"drafts":{"env/thread":"exact 🦀"},"pending":{"env":operation("original","send")},"future":{"opaque":9007199254740991u64}}).to_string();
        fs::write(&source, &original).unwrap();
        let state = State::open(&directory.join("rust-state.json")).unwrap();
        state.set_draft("env", "other", "different").unwrap();
        assert_eq!(fs::read_to_string(&source).unwrap(), original);
        assert_eq!(state.snapshot()["future"]["opaque"], 9007199254740991u64);
        assert_eq!(state.pending("env", false).unwrap()["id"], "original");
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn failed_update_preserves_memory_and_disk() {
        let directory = temp();
        let path = directory.join("rust-state.json");
        let state = State::open(&path).unwrap();
        state.set_draft("env", "thread", "safe").unwrap();
        let before = fs::read(&path).unwrap();
        assert!(
            state
                .update(|v| {
                    v["drafts"]["env/thread"] = json!("x".repeat(MAX_DRAFT_BYTES + 1));
                    Ok(())
                })
                .is_err()
        );
        assert_eq!(state.draft("env", "thread"), "safe");
        assert_eq!(fs::read(&path).unwrap(), before);
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn same_id_with_changed_payload_is_rejected() {
        let directory = temp();
        let state = State::open(&directory.join("rust-state.json")).unwrap();
        let record = operation("one", "answer");
        state.save_pending("env", false, record.clone()).unwrap();
        state.save_pending("env", false, record.clone()).unwrap();
        let mut changed = record;
        changed["message"] = json!("changed");
        assert!(state.save_pending("env", false, changed).is_err());
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn interrupted_prompt_checkpoint_imports_attempt_without_touching_source() {
        let directory = temp();
        let source = directory.join("go-drafts.json");
        let acknowledged = prompt_entry("env", "thread", "sent", "old prompt");
        let staged = prompt_entry("env", "thread", "sent", "lost acknowledgement prompt");
        let main =
            json!({"drafts":{},"pending":{},"promptHistory":[acknowledged.clone()]}).to_string();
        fs::write(&source, &main).unwrap();
        let sidecar=json!({"version":1,"committed":[acknowledged],"prepared":[staged],"stateSHA256":"a".repeat(64)}).to_string();
        let sidecar_path = directory.join("go-drafts.json.prompts.json");
        fs::write(&sidecar_path, &sidecar).unwrap();
        let state = State::open(&directory.join("rust-state.json")).unwrap();
        let history = state.snapshot()["promptHistory"]
            .as_array()
            .unwrap()
            .clone();
        assert!(
            history
                .iter()
                .any(|v| v["text"] == "lost acknowledgement prompt" && v["kind"] == "attempt")
        );
        assert_eq!(fs::read_to_string(&source).unwrap(), main);
        assert_eq!(fs::read_to_string(sidecar_path).unwrap(), sidecar);
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn malformed_checkpoint_blocks_import_and_preserves_pending() {
        let directory = temp();
        let source = directory.join("go-drafts.json");
        fs::write(
            &source,
            json!({"drafts":{},"pending":{"env":operation("lost","send")}}).to_string(),
        )
        .unwrap();
        fs::write(
            directory.join("go-drafts.json.prompts.json"),
            json!({"version":1,"committed":[],"prepared":[],"stateSHA256":"bad"}).to_string(),
        )
        .unwrap();
        let path = directory.join("rust-state.json");
        assert!(State::open(&path).is_err());
        assert!(!path.exists());
        assert!(source.exists());
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn model_presets_and_launches_use_the_native_selection_schema() {
        let directory = temp();
        let state = State::open(&directory.join("rust-state.json")).unwrap();
        let selection = json!({"instanceId":"account-1","model":"native-model","options":[{"id":"effort","value":"high"},{"id":"fastMode","value":true}]});
        state
            .update(|v| {
                v["modelPresets"]["env"] = json!([{"name":"Work","selection":selection}]);
                Ok(())
            })
            .unwrap();
        let mut invalid = selection.clone();
        invalid["options"][1]["id"] = json!("effort");
        assert!(validate_selection(&invalid).is_err());
        assert!(
            state.snapshot()["modelPresets"]["env"][0]["selection"]["options"][1]["value"] == true
        );
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn acknowledged_batch_advances_only_its_exact_human_text() {
        let directory = temp();
        let state = State::open(&directory.join("rust-state.json")).unwrap();
        let source = "\0file-many/project";
        let original = "one\ntwo\n";
        state.set_draft("env", source, original).unwrap();
        state.update(|v|{v["batchReceipts"]["batch"]=json!({"id":"batch","environmentId":"env","source":source,"original":original,"createdAt":"2026-11-02T00:00:00Z","items":[{"start":0,"end":4,"state":"Remaining draft"},{"start":4,"end":8,"state":"Remaining draft"}]});Ok(())}).unwrap();
        let record = json!({"id":"launch-first","op":"launch-project","environmentId":"env","threadId":"source-thread","newThreadId":"new-thread","projectId":"project","batchDraft":{"receiptId":"batch","itemIndex":0,"environment":"env","source":source,"before":original,"consumed":"one\n"}});
        state.save_pending("env", false, record).unwrap();
        assert_eq!(
            state.snapshot()["batchReceipts"]["batch"]["items"][0]["state"],
            "Needs recovery"
        );
        state
            .acknowledge("env", false, "launch-first", "source-thread", "")
            .unwrap();
        assert_eq!(state.draft("env", source), "two\n");
        assert_eq!(
            state.snapshot()["batchReceipts"]["batch"]["items"][0]["state"],
            "Acknowledged"
        );
        drop(state);
        fs::remove_dir_all(directory).unwrap();
    }
}
