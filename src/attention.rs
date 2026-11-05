//! Source-qualified native human questions and durable, revision-bound answer drafts.
use crate::domain::{Thread, array, flag, string, valid_id};
use crate::state::{MAX_DRAFT_BYTES, State};
use anyhow::{Context, Result, ensure};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, HashSet};

/// The complete identity of the native wording a person reviewed.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Source {
    pub environment_id: String,
    pub thread_id: String,
    pub request_id: String,
    pub revision: String,
}
impl Source {
    /// Construct a source from a full native offer, never a shell summary.
    pub fn from_request(environment: &str, thread: &str, request: &Value) -> Result<Self> {
        let source = Self {
            environment_id: environment.into(),
            thread_id: thread.into(),
            request_id: string(request, "id"),
            revision: string(request, "revision"),
        };
        ensure!(
            [
                &source.environment_id,
                &source.thread_id,
                &source.request_id
            ]
            .into_iter()
            .all(|id| valid_id(id))
                && source.revision.len() == 64
                && source.revision.bytes().all(|b| b.is_ascii_hexdigit()),
            "Native request source or revision is unavailable"
        );
        Ok(source)
    }
    /// Hash every identity component rather than joining potentially ambiguous IDs.
    pub fn key(&self) -> Result<String> {
        Ok(format!("{:x}", Sha256::digest(serde_json::to_vec(self)?)))
    }
}

/// A shell offer points to a real human request without pretending to contain its form.
#[derive(Clone, Debug)]
pub struct QueueItem {
    pub profile: usize,
    pub thread_id: String,
    pub request_id: String,
    pub title: String,
    pub kind: String,
}
/// Native project identity remains qualified by environment/profile.
#[derive(Clone, Debug)]
pub struct ProjectQueue {
    pub profile: usize,
    pub project_id: String,
    pub project: String,
    pub items: Vec<QueueItem>,
}

/// Admit only explicit native questions/approvals; failure and prose are not questions.
pub fn project_queue(threads: &[Thread]) -> Vec<ProjectQueue> {
    let mut groups = BTreeMap::<(usize, String), ProjectQueue>::new();
    for thread in threads {
        let request_id = string(thread.inbox(), "pendingRequestId");
        let kind = string(thread.inbox(), "pendingRequestType");
        if !valid_id(&request_id) || !matches!(kind.as_str(), "user_input" | "approval") {
            continue;
        }
        let group = groups
            .entry((thread.profile, thread.project_id.clone()))
            .or_insert_with(|| ProjectQueue {
                profile: thread.profile,
                project_id: thread.project_id.clone(),
                project: thread.project.clone(),
                items: Vec::new(),
            });
        group.items.push(QueueItem {
            profile: thread.profile,
            thread_id: thread.id.clone(),
            request_id,
            title: thread.title.clone(),
            kind,
        });
    }
    let mut groups: Vec<_> = groups.into_values().collect();
    groups.sort_by(|left, right| {
        (&left.project, left.profile, &left.project_id).cmp(&(
            &right.project,
            right.profile,
            &right.project_id,
        ))
    });
    groups
}

/// Stable widget identity is separate from the value sent to the native request.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Choice {
    pub id: String,
    pub value: String,
    pub label: String,
    pub description: String,
}
/// The native question definition; selection/focus are deliberately separate state.
#[derive(Clone, Debug)]
pub struct Field {
    pub id: String,
    pub header: String,
    pub question: String,
    pub required: bool,
    pub multiple: bool,
    pub custom: bool,
    pub choices: Vec<Choice>,
}
/// A full, supported offer whose immutable payload also accompanies its saved draft.
#[derive(Clone, Debug)]
pub struct Form {
    pub source: Source,
    pub fields: Vec<Field>,
    pub request: Value,
}
impl Form {
    /// Validate native fields once before rendering or accepting edits.
    pub fn from_request(environment: &str, thread: &str, request: &Value) -> Result<Self> {
        ensure!(
            flag(request, "supported") && request["kind"] == "user_input_request",
            "Open this request in native T3"
        );
        let source = Source::from_request(environment, thread, request)?;
        let questions = array(request, "questions");
        ensure!(
            !questions.is_empty() && questions.len() <= 8,
            "Unsupported native question count"
        );
        let mut fields = Vec::new();
        let mut seen = HashSet::new();
        for question in questions {
            let id = string(question, "id");
            ensure!(
                valid_id(&id) && id.len() <= 128 && seen.insert(id.clone()),
                "Missing or duplicate native field ID"
            );
            for name in ["required", "multiSelect", "allowCustomAnswer"] {
                ensure!(
                    question.get(name).is_none_or(Value::is_boolean),
                    "Unsupported native field flag"
                );
            }
            ensure!(
                question.get("options").is_none_or(Value::is_array),
                "Unsupported native choice definition"
            );
            let options = array(question, "options");
            ensure!(options.len() <= 24, "Unsupported native choice count");
            let mut choices = Vec::new();
            let mut choice_ids = HashSet::new();
            let mut choice_values = HashSet::new();
            for option in options {
                let label = string(option, "label");
                ensure!(
                    option.get("value").is_none_or(Value::is_string),
                    "Unsupported native choice value type"
                );
                let value = option["value"].as_str().unwrap_or(&label).to_owned();
                let choice_id = match option.get("id") {
                    Some(value) => value
                        .as_str()
                        .context("Invalid native choice ID")?
                        .to_owned(),
                    None => format!("{:x}", Sha256::digest(serde_json::to_vec(&(&id, &value))?)),
                };
                ensure!(
                    !label.is_empty()
                        && label.len() <= 256
                        && valid_id(&choice_id)
                        && !value.trim().is_empty()
                        && value.len() <= MAX_DRAFT_BYTES
                        && choice_ids.insert(choice_id.clone())
                        && choice_values.insert(value.clone()),
                    "Missing, duplicate or oversized native choice"
                );
                choices.push(Choice {
                    id: choice_id,
                    value,
                    label,
                    description: string(option, "description"),
                });
            }
            fields.push(Field {
                id,
                header: string(question, "header"),
                question: string(question, "question"),
                required: question["required"].as_bool().unwrap_or(true),
                multiple: flag(question, "multiSelect") && !choices.is_empty(),
                custom: flag(question, "allowCustomAnswer") || choices.is_empty(),
                choices,
            });
        }
        Ok(Self {
            source,
            fields,
            request: request.clone(),
        })
    }

    /// Require unchanged wording and identity before enabling an explicit Submit.
    pub fn ensure_current(
        &self,
        environment: &str,
        thread: &str,
        requests: &[Value],
    ) -> Result<()> {
        ensure!(
            self.source.environment_id == environment && self.source.thread_id == thread,
            "Question belongs to another source conversation"
        );
        ensure!(
            requests
                .iter()
                .any(|request| request["id"] == self.source.request_id
                    && request["revision"] == self.source.revision
                    && flag(request, "supported")),
            "Question changed or was cancelled; saved draft retained"
        );
        Ok(())
    }

    /// Check all required answers and native value constraints before dispatch.
    pub fn validate_answers(&self, answers: &Map<String, Value>) -> Result<()> {
        self.validate_edits(answers)?;
        for field in &self.fields {
            let Some(answer) = answers.get(&field.id) else {
                ensure!(!field.required, "{} requires an answer", field.header);
                continue;
            };
            ensure!(
                !field.required
                    || if field.multiple {
                        answer.as_array().is_some_and(|values| !values.is_empty())
                    } else {
                        answer.as_str().is_some_and(|text| !text.trim().is_empty())
                    },
                "{} requires an answer",
                field.header
            );
            if !field.multiple && !field.custom {
                ensure!(
                    answer.as_str().is_some_and(|text| field
                        .choices
                        .iter()
                        .any(|choice| choice.value == text)),
                    "Answer was not offered by native T3"
                );
            }
        }
        Ok(())
    }

    fn validate_edits(&self, answers: &Map<String, Value>) -> Result<()> {
        ensure!(
            answers
                .keys()
                .all(|id| self.fields.iter().any(|field| field.id == *id)),
            "Unknown native field ID"
        );
        ensure!(
            serde_json::to_vec(answers)?.len() <= MAX_DRAFT_BYTES,
            "Answer draft exceeds 64 KiB"
        );
        for field in &self.fields {
            let Some(answer) = answers.get(&field.id) else {
                continue;
            };
            let allowed = |text: &str| {
                text.len() <= MAX_DRAFT_BYTES
                    && (field.custom
                        || text.is_empty()
                        || field.choices.iter().any(|choice| choice.value == text))
            };
            if field.multiple {
                let values = answer
                    .as_array()
                    .context("Native multiple-choice answer needs a list")?;
                let mut seen = HashSet::new();
                ensure!(
                    values.len() <= 32
                        && values
                            .iter()
                            .all(|value| value.as_str().is_some_and(|text| !text
                                .trim()
                                .is_empty()
                                && allowed(text)
                                && seen.insert(text))),
                    "Invalid or duplicate answer selection"
                );
            } else {
                ensure!(
                    answer.as_str().is_some_and(allowed),
                    "Answer was not offered by native T3"
                );
            }
        }
        Ok(())
    }

    /// Save incomplete edits without erasing drafts for older source revisions.
    pub fn save_draft(&self, state: &State, answers: &Map<String, Value>) -> Result<()> {
        self.validate_edits(answers)?;
        let key = self.source.key()?;
        let record = json!({"source":self.source,"request":self.request,"answers":answers});
        if state.snapshot()["attentionDrafts"][&key] == record {
            return Ok(());
        }
        state.update(|value| {
            if value.get("attentionDrafts").is_none() {
                value["attentionDrafts"] = json!({});
            }
            let drafts = value["attentionDrafts"]
                .as_object_mut()
                .context("Invalid saved answer drafts; preserve and repair state")?;
            drafts.insert(key, record);
            Ok(())
        })
    }

    /// Restore only the exact source revision, leaving changed-source drafts untouched.
    pub fn load_draft(&self, state: &State) -> Result<Option<Map<String, Value>>> {
        let snapshot = state.snapshot();
        let Some(record) = snapshot["attentionDrafts"].get(self.source.key()?) else {
            return Ok(None);
        };
        ensure!(
            serde_json::from_value::<Source>(record["source"].clone())? == self.source
                && record["request"] == self.request,
            "Saved answer draft source changed; original retained"
        );
        let answers = record["answers"]
            .as_object()
            .context("Invalid saved answer draft")?
            .clone();
        self.validate_edits(&answers)?;
        Ok(Some(answers))
    }

    /// Return the existing adapter's revision-qualified answer fields.
    pub fn answer_fields(&self, answers: &Map<String, Value>) -> Result<Value> {
        self.validate_answers(answers)?;
        Ok(
            json!({"threadId":self.source.thread_id,"requestId":self.source.request_id,
            "revision":self.source.revision,"answers":answers}),
        )
    }
}

/// Success requires the native sequence and every original answer identity.
pub fn acknowledged(source: &Source, operation: &str, result: &Value) -> bool {
    let receipt = &result["acknowledgement"];
    result["sequence"]
        .as_u64()
        .is_some_and(|sequence| sequence <= 9_007_199_254_740_991)
        && receipt["environmentId"] == source.environment_id
        && receipt["threadId"] == source.thread_id
        && receipt["requestId"] == source.request_id
        && receipt["revision"] == source.revision
        && receipt["operationId"] == operation
}
