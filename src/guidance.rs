//! Client-owned prompt guidance; native orchestration contracts remain authoritative.
use crate::config::valid_id;
use crate::state::MAX_DRAFT_BYTES;
use crate::wire::MAX_INPUT_BYTES;
use anyhow::{Context, Result, ensure};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

const PREPARATION: &str = "t3oxidePromptPreparation";
const MAX_SUFFIX_BYTES: usize = 4096;
const MAX_PREFERENCES_BYTES: usize = 128 * 1024;
const MAX_SCOPES: usize = 256;
pub(crate) const PREFERENCES: &str = "orchestrationGuidance";

/// An explicit strategy preference, never a runtime policy or model selection.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum Guidance {
    Off,
    Graphing,
    Classic,
}

impl Guidance {
    /// Exact appended bytes, including the separator shown in the preference UI.
    pub fn suffix(self) -> &'static str {
        macro_rules! suffix {
            ($strategy:literal) => {
                concat!("\n\nOrchestration preference: follow the current Native T3 Orchestrator V2 contracts and injected instructions. Preserve the selected account, main model and options. Choose useful delegation and nesting from the task and current state. No user-imposed agent-count cap applies. This is prompt guidance, not runtime enforcement.\n", $strategy)
            };
        }
        match self {
            Self::Off => "",
            Self::Graphing => suffix!(
                "Prefer graphing with independent owners and useful nesting beyond one level, using native delegation and thread capabilities where permitted by current instructions."
            ),
            Self::Classic => {
                suffix!("Prefer classic subagents using the current native delegation contracts.")
            }
        }
    }
}

/// Prepare only a new ordinary send. Recovery must reuse its original fields.
/// Unset/Off preserves the fields exactly; no human-prose deduplication is done.
/// App admission also bounds the complete wire envelope before State staging
/// can detach a draft. Recovery keeps its original saved record unchanged.
pub fn prepare_send_fields(fields: &Value, guidance: Option<Guidance>) -> Result<Value> {
    ensure!(fields.is_object(), "send fields must be an object");
    ensure!(
        fields.get(PREPARATION).is_none(),
        "send is already prepared"
    );
    ensure!(
        fields.get("op").is_none_or(|op| op == "send")
            && fields.get("t3oxideCheckFollowUp").is_none(),
        "guidance applies only to new ordinary sends"
    );
    let human = fields["message"].as_str().context("send needs text")?;
    let suffix = guidance.map_or("", Guidance::suffix);
    ensure!(
        human.len() + suffix.len() <= MAX_DRAFT_BYTES,
        "prepared prompt exceeds 64 KiB"
    );
    let mut prepared = fields.clone();
    if !suffix.is_empty() {
        prepared["message"] = json!(format!("{human}{suffix}"));
        prepared[PREPARATION] = json!({"version":1,"originalBytes":human.len(),"suffix":suffix});
    }
    ensure!(
        serde_json::to_vec(&prepared)?.len() <= MAX_INPUT_BYTES,
        "prepared operation exceeds 96 KiB"
    );
    Ok(prepared)
}

/// Return the exact human prefix, or the full legacy message without metadata.
/// Malformed preparation is an error and must never be cleared or reconstructed.
pub fn submitted_text(record: &Value) -> Result<&str> {
    let message = record["message"].as_str().context("send needs text")?;
    let Some(preparation) = record.get(PREPARATION) else {
        return Ok(message);
    };
    ensure!(
        record["op"] == "send" && record.get("t3oxideCheckFollowUp").is_none(),
        "prepared metadata requires an ordinary send"
    );
    ensure!(
        preparation.is_object() && preparation["version"].as_u64() == Some(1),
        "unsupported prompt preparation"
    );
    let original = preparation["originalBytes"]
        .as_u64()
        .and_then(|n| usize::try_from(n).ok())
        .context("invalid original prompt byte length")?;
    let suffix = preparation["suffix"]
        .as_str()
        .context("invalid frozen prompt suffix")?;
    ensure!(
        message.len() <= MAX_DRAFT_BYTES && suffix.len() <= MAX_SUFFIX_BYTES,
        "prepared prompt exceeds bounds"
    );
    let human = message
        .get(..original)
        .context("original prompt length is not a UTF-8 boundary")?;
    ensure!(
        message.get(original..) == Some(suffix),
        "prepared prompt differs from its frozen suffix"
    );
    Ok(human)
}

pub(crate) fn scope_key(environment: &str, id: &str) -> Result<String> {
    ensure!(
        valid_id(environment, 4096) && valid_id(id, 4096),
        "guidance needs exact native source identities"
    );
    Ok(serde_json::to_string(&[environment, id])?)
}

pub(crate) fn choice(entry: &Value) -> Result<Guidance> {
    serde_json::from_value(entry["guidance"].clone()).context("invalid guidance strategy")
}

pub(crate) fn validate_preferences(root: &Value) -> Result<()> {
    let Some(preferences) = root.get(PREFERENCES) else {
        return Ok(());
    };
    ensure!(
        preferences.is_object() && preferences["version"].as_u64() == Some(1),
        "unsupported orchestration guidance preferences"
    );
    ensure!(
        serde_json::to_vec(preferences)?.len() <= MAX_PREFERENCES_BYTES,
        "guidance preferences exceed 128 KiB"
    );
    for collection in ["projects", "conversations"] {
        let entries = preferences[collection]
            .as_object()
            .context("guidance scopes must be objects")?;
        ensure!(entries.len() <= MAX_SCOPES, "guidance scope limit exceeded");
        for (key, entry) in entries {
            let ids: Vec<String> =
                serde_json::from_str(key).context("invalid guidance scope tuple")?;
            ensure!(
                ids.len() == 2 && scope_key(&ids[0], &ids[1])? == *key && entry.is_object(),
                "invalid guidance source identity"
            );
            choice(entry)?;
            if collection == "conversations" {
                ensure!(
                    entry["projectId"]
                        .as_str()
                        .is_some_and(|id| valid_id(id, 4096)),
                    "conversation guidance needs original project provenance"
                );
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn preparation_preserves_unicode_exact_bytes_and_rejects_invalid_metadata() {
        let original = json!({"op":"send","message":"é猫\nexact human text","mode":"steer","modelSelection":{"model":"selected"}});
        let prepared = prepare_send_fields(&original, Some(Guidance::Graphing)).unwrap();
        assert_eq!(
            submitted_text(&prepared).unwrap(),
            original["message"].as_str().unwrap()
        );
        assert_eq!(prepared["mode"], original["mode"]);
        assert_eq!(prepared["modelSelection"], original["modelSelection"]);
        assert!(prepare_send_fields(&prepared, Some(Guidance::Classic)).is_err());
        for metadata in [
            json!({"version":1,"originalBytes":1,"suffix":"x"}),
            json!({"version":2,"originalBytes":0,"suffix":"é猫"}),
            json!({"version":1,"originalBytes":100000,"suffix":""}),
            json!({"version":1,"originalBytes":2,"suffix":"wrong suffix"}),
            json!({"version":1,"originalBytes":0,"suffix":"x".repeat(MAX_SUFFIX_BYTES + 1)}),
        ] {
            let mut bad = prepared.clone();
            bad[PREPARATION] = metadata;
            assert!(submitted_text(&bad).is_err());
        }
        let mut non_send = prepared.clone();
        non_send["op"] = json!("stop");
        assert!(submitted_text(&non_send).is_err());
        let mut follow_up = prepared;
        follow_up["t3oxideCheckFollowUp"] = json!("attempt");
        assert!(submitted_text(&follow_up).is_err());
        assert!(prepare_send_fields(&follow_up, None).is_err());
    }

    #[test]
    fn final_message_and_serialized_limits_include_preparation() {
        let suffix = Guidance::Classic.suffix();
        let at_limit = json!({"op":"send","message":"x".repeat(MAX_DRAFT_BYTES - suffix.len())});
        let prepared = prepare_send_fields(&at_limit, Some(Guidance::Classic)).unwrap();
        assert_eq!(prepared["message"].as_str().unwrap().len(), MAX_DRAFT_BYTES);
        let too_long =
            json!({"op":"send","message":"x".repeat(MAX_DRAFT_BYTES - suffix.len() + 1)});
        assert!(prepare_send_fields(&too_long, Some(Guidance::Classic)).is_err());
        let escaped = json!({"op":"send","message":"\\".repeat(50 * 1024)});
        assert!(prepare_send_fields(&escaped, Some(Guidance::Classic)).is_err());
        let legacy = json!({"op":"send","message":"unaltered", "unknown":7});
        assert_eq!(prepare_send_fields(&legacy, None).unwrap(), legacy);
        assert_eq!(
            prepare_send_fields(&legacy, Some(Guidance::Off)).unwrap(),
            legacy
        );
    }
}
