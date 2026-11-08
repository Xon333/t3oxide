//! One finite check return and an isolated, source-pinned human follow-up.
use super::*;
use std::path::Path;

const MAX_ATTEMPTS: usize = 16;

// Evidence reads can finish after native acceptance. Collection never owns that receipt.
fn retain_observation(record: &mut Value, generation: u64, evidence: Result<&Value, &str>) {
    if record["readGeneration"].as_u64() != Some(generation) {
        return;
    }
    let phase = match evidence {
        Ok(envelope) => {
            record["evidence"] = envelope.clone();
            "collected"
        }
        Err(error) => {
            record["error"] = json!(error);
            "unknown"
        }
    };
    record["evidencePhase"] = json!(phase);
    if record["operationId"].is_null() && record["phase"] != "acknowledged" {
        record["phase"] = json!(phase);
    }
}

struct CommandInput {
    worktree: String,
    sources: Vec<String>,
    timeout: u64,
    argv: Vec<String>,
}

fn parse_command(text: &str, workspace: &str) -> Result<CommandInput> {
    let words = shell_words::split(text)?;
    let boundary = words
        .iter()
        .position(|word| word == "--")
        .context("Use --source relative/path -- command args; --cwd sets the execution worktree")?;
    let mut worktree = workspace.to_owned();
    let mut sources = Vec::new();
    let mut timeout = 300;
    let (options, remainder) = words[..boundary].as_chunks::<2>();
    ensure!(remainder.is_empty(), "Check option needs a value");
    for pair in options {
        match pair[0].as_str() {
            "--cwd" => worktree.clone_from(&pair[1]),
            "--source" => sources.push(pair[1].clone()),
            "--timeout" => timeout = pair[1].parse()?,
            _ => bail!("Unknown check option {}", pair[0]),
        }
    }
    ensure!(
        Path::new(&worktree).is_absolute() && !worktree.contains(['\0', '\n', '\r']),
        "Check needs an absolute execution-host worktree"
    );
    ensure!(
        (1..=900).contains(&timeout),
        "Check timeout must be 1–900 seconds"
    );
    ensure!(
        !sources.is_empty()
            && sources.len() <= 8
            && sources.iter().all(|source| {
                !source.is_empty()
                    && Path::new(source)
                        .components()
                        .all(|part| matches!(part, std::path::Component::Normal(_)))
            }),
        "Select 1–8 relative source paths without traversal"
    );
    let argv = words[boundary + 1..].to_vec();
    ensure!(
        !argv.is_empty() && argv.iter().all(|arg| !arg.contains('\0')),
        "Check needs a command argument vector"
    );
    Ok(CommandInput {
        worktree,
        sources,
        timeout,
        argv,
    })
}

impl App {
    pub(super) fn check_start(&mut self, text: &str) -> Result<()> {
        use crate::check_evidence::{self, Attempt, Launch, RexContext};
        let active = self
            .active
            .as_ref()
            .context("Open the native target conversation first")?;
        let snapshot = active
            .projection
            .snapshot()
            .context("Wait for the native target snapshot")?;
        ensure!(
            snapshot.is_live() && !active.navigating,
            "Return to Latest before starting a check"
        );
        let source = self.source(active.profile, &active.thread);
        ensure!(
            domain::valid_id(&source.project_id),
            "Original native project unavailable"
        );
        let parsed = parse_command(text, &string(&snapshot.metadata, "workspace"))?;
        let rex_path = std::env::var_os("T3OXIDE_REX")
            .map(std::path::PathBuf::from)
            .or_else(|| {
                std::env::split_paths(&std::env::var_os("PATH").unwrap_or_default())
                    .map(|path| path.join("rex"))
                    .find(|path| path.is_file())
            })
            .or_else(|| {
                if cfg!(target_os="macos") {
                    std::env::var_os("HOME").map(std::path::PathBuf::from).map(|home|
                        home.join("Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS/rex"))
                        .filter(|path|path.is_file())
                } else {None}
            })
            .context(
                "Rex CLI unavailable; set T3OXIDE_REX to its absolute maintained executable",
            )?;
        let attempt = Attempt::new(Launch {
            source: json!({"environmentId":source.environment,"projectId":source.project_id,"threadId":source.thread,
                "sourceRevision":format!("projection-{}-{}",active.generation,snapshot.revision)}),
            environment: self.environments[active.profile].clone(),
            worktree: parsed.worktree,
            argv: parsed.argv,
            source_paths: parsed.sources,
            timeout_seconds: parsed.timeout,
            rex: RexContext::inherited(rex_path)?,
        })?;
        let id = attempt.attempt_id.clone();
        self.flush_draft()?;
        self.state.update(|saved| {
            if saved.get("checkAttempts").is_none() {
                saved["checkAttempts"] = json!({});
            }
            let attempts = saved["checkAttempts"]
                .as_object_mut()
                .context("Invalid retained check records")?;
            ensure!(
                attempts.len() < MAX_ATTEMPTS,
                "Check evidence limit reached; retain/export completed attempts before making room"
            );
            let sequence = attempts
                .values()
                .filter_map(|record| record["sequence"].as_u64())
                .max()
                .unwrap_or(0)
                + 1;
            attempts.insert(
                id.clone(),
                json!({"attempt":attempt,"target":attempt.launch.source,"sequence":sequence,
                "phase":"preparing","finding":"","delivery":self.delivery,"readGeneration":1}),
            );
            Ok(())
        })?;
        self.check_io.insert(id.clone());
        let state = self.state.clone();
        let tx = self.tx.clone();
        thread::spawn(move || {
            let result = (|| -> Result<()> {
                let prepared = check_evidence::prepare(&attempt)?;
                state.update(|saved| {
                    saved["checkAttempts"][&id]["prepared"] = serde_json::to_value(&prepared)?;
                    saved["checkAttempts"][&id]["phase"] = json!("prepared"); Ok(())
                })?;
                let completion = check_evidence::run(&prepared, |observation| state.update(|saved| {
                    let record=&mut saved["checkAttempts"][&id];
                    if observation["phase"]=="launch-intent" {
                        ensure!(record["dispatched"]!=true,"Original check dispatch is already uncertain; read only");
                        record["dispatched"]=json!(true);
                        record["phase"]=json!("launch-uncertain");
                    } else {
                        if record.get("rexObservations").is_none() {record["rexObservations"]=json!([]);}
                        let observations=record["rexObservations"].as_array_mut().context("Invalid retained Rex observations")?;
                        ensure!(observations.len()<16,"Rex creation observation limit reached; original retained");
                        observations.push(observation.clone());
                    }
                    Ok(())
                })) ;
                state.update(|saved| {
                    let record=&mut saved["checkAttempts"][&id];
                    record["transport"] = match &completion.launch {
                        Ok(observed)=>json!({"exitCode":observed.exit_code,"signal":observed.signal,
                            "stdoutBase64":base64::Engine::encode(&base64::engine::general_purpose::STANDARD,&observed.stdout),
                            "stderrBase64":base64::Engine::encode(&base64::engine::general_purpose::STANDARD,&observed.stderr)}),
                        Err(error)=>json!({"unknown":error}),
                    };
                    retain_observation(record,1,completion.evidence.as_ref().map(|e|&e.envelope).map_err(String::as_str));
                    Ok(())
                })?;
                if completion.launch.is_ok() && completion.evidence.as_ref().is_ok_and(|evidence|
                    evidence.envelope["command"]["state"]=="exit-observed" && evidence.envelope["wrapper"]["state"]=="exit-observed") {
                    let retained=state.snapshot()["checkAttempts"][&id].clone();
                    let observations=array(&retained,"rexObservations");
                    let closed=check_evidence::close_finished(&prepared,observations);
                    state.update(|saved| {
                        saved["checkAttempts"][&id]["paneDisposition"]=match &closed {
                            Ok(Some(id))=>json!({"closed":id}),Ok(None)=>json!({"retained":"no unambiguous creation ID"}),
                            Err(error)=>json!({"retained":format!("{error:#}")}),
                        };Ok(())
                    })?;
                }
                completion.evidence.map(|_|()).map_err(anyhow::Error::msg)
            })().map_err(|error|format!("{error:#}"));
            if let Err(error) = &result {
                let _ = state.update(|saved| {
                    saved["checkAttempts"][&id]["error"] = json!(error);
                    Ok(())
                });
            }
            let _ = tx.send(Background::Check {
                attempt: id,
                result,
            });
        });
        self.notice = "Check starting once · original target retained".into();
        self.dirty = true;
        Ok(())
    }

    fn check_recover(&mut self, id: &str) -> Result<()> {
        if self.check_io.contains(id) {
            self.notice = "Original check read is in progress · no duplicate read or run".into();
            return Ok(());
        }
        let record = self.check_attempt(id)?;
        if record["evidence"].is_object()
            && record["evidence"]["command"]["state"] == "exit-observed"
            && record["evidence"]["wrapper"]["state"] == "exit-observed"
        {
            return self.check_finding(id);
        }
        let prepared: Option<crate::check_evidence::PreparedAttempt> = record
            .get("prepared")
            .cloned()
            .map(serde_json::from_value)
            .transpose()?;
        let original: crate::check_evidence::Attempt =
            serde_json::from_value(record["attempt"].clone())?;
        let generation = record["readGeneration"]
            .as_u64()
            .unwrap_or(0)
            .checked_add(1)
            .context("Check read generation exhausted")?;
        self.state.update(|saved| {
            saved["checkAttempts"][id]["readGeneration"] = json!(generation);
            Ok(())
        })?;
        self.check_io.insert(id.to_owned());
        let state = self.state.clone();
        let tx = self.tx.clone();
        let id = id.to_owned();
        thread::spawn(move || {
            let result = (|| -> Result<()> {
                let (prepared, evidence) = if let Some(prepared) = prepared {
                    let evidence = crate::check_evidence::collect(&prepared)?;
                    (prepared, evidence)
                } else {
                    crate::check_evidence::recover(&original)?
                };
                state.update(|saved| {
                    saved["checkAttempts"][&id]["prepared"] = serde_json::to_value(&prepared)?;
                    retain_observation(
                        &mut saved["checkAttempts"][&id],
                        generation,
                        Ok(&evidence.envelope),
                    );
                    Ok(())
                })
            })()
            .map_err(|error| format!("{error:#}"));
            let _ = tx.send(Background::Check {
                attempt: id,
                result,
            });
        });
        self.notice = "Reading original check attempt · no rerun".into();
        Ok(())
    }

    pub(super) fn check_command(&mut self, name: &str, args: &str) -> Result<bool> {
        match name {
            "check" if args.trim().is_empty() => {
                let active = self
                    .active
                    .as_ref()
                    .context("Open the original native target first")?;
                let source = self.source(active.profile, &active.thread);
                let key = serde_json::to_string(&[&source.environment, &source.thread])?;
                let draft = string(&self.state.snapshot()["checkCommandDrafts"], &key);
                self.text_dialog("Run check",&format!("{} / {}\n--source src/file.rs -- cargo test name\nOptional: --cwd /execution/worktree --timeout 300. Runs once beside this pane.",source.environment_label,source.thread),InputTarget::CheckCommand(source),draft);
            }
            "check" => self.check_start(args)?,
            "check-result" => {
                let active = self
                    .active
                    .as_ref()
                    .context("Open the original conversation")?;
                let environment = &self.environments[active.profile].environment_id;
                let saved = self.state.snapshot();
                let attempts = saved["checkAttempts"]
                    .as_object()
                    .context("No retained check attempt")?;
                let id = attempts
                    .iter()
                    .filter(|(_, value)| {
                        value["target"]["environmentId"] == *environment
                            && value["target"]["threadId"] == active.thread
                    })
                    .max_by_key(|(_, value)| value["sequence"].as_u64().unwrap_or(0))
                    .map(|(id, _)| id.clone())
                    .context("No check for this conversation")?;
                self.check_recover(&id)?;
            }
            _ => return Ok(false),
        }
        Ok(true)
    }

    fn check_attempt(&self, attempt: &str) -> Result<Value> {
        self.state.snapshot()["checkAttempts"]
            .get(attempt)
            .cloned()
            .context("Original check attempt unavailable")
    }

    pub(super) fn check_save_command(
        &self,
        source: &crate::slt_ui::Source,
        text: &str,
    ) -> Result<()> {
        ensure!(text.len() <= 8192, "Check command draft exceeds 8 KiB");
        let key = serde_json::to_string(&[&source.environment, &source.thread])?;
        self.state.update(|saved| {
            if saved.get("checkCommandDrafts").is_none() {
                saved["checkCommandDrafts"] = json!({});
            }
            let drafts = saved["checkCommandDrafts"]
                .as_object_mut()
                .context("Invalid check command drafts")?;
            ensure!(
                drafts.contains_key(&key) || drafts.len() < MAX_ATTEMPTS,
                "Check command draft limit reached; originals retained"
            );
            drafts.insert(key.clone(), json!(text));
            Ok(())
        })
    }

    pub(super) fn check_done(&mut self, attempt: &str, result: Result<(), String>) -> Result<()> {
        self.check_io.remove(attempt);
        let retained = self.check_attempt(attempt)?;
        if retained["evidence"].is_object() && !self.context_io.draining {
            let evidence = &retained["evidence"];
            let source = crate::context_store::Source {
                environment_id: string(&retained["target"], "environmentId"),
                project_id: string(&retained["target"], "projectId"),
                thread_id: string(&retained["target"], "threadId"),
                item_id: Some(attempt.into()),
                request_id: None,
                revision: crate::reports::content_revision(&serde_json::to_string(evidence)?),
                observed: json!({"finiteCheck":true,"command":evidence["command"],"wrapper":evidence["wrapper"],
                    "host":evidence["binding"]["host"],"worktree":evidence["binding"]["worktree"],"sourceScope":"selected paths only"}),
            };
            let key = super::content::capture_key(&source)?;
            if !self.context_capture_known.contains(&key) {
                self.context_capture_pending.insert(
                    key,
                    crate::context_store::RecordInput {
                        source,
                        kind: crate::context_store::Kind::Evidence,
                        text: format!("Finite check {attempt} · {}", evidence["command"]),
                        full_source: serde_json::to_vec(evidence)?,
                        supersedes: None,
                        revisit: None,
                    },
                );
                self.schedule_context_capture()?;
            }
        }
        if let Err(error) = result {
            self.notice = format!(
                "Check return unknown · {error} · /check-result reads the original attempt"
            );
        } else if self.dialog.is_none()
            && self.active.as_ref().is_some_and(|active| {
                self.environments[active.profile].environment_id
                    == retained["target"]["environmentId"]
                    && active.thread == retained["target"]["threadId"]
            })
            && !self.context_io.draining
        {
            self.check_finding(attempt)?;
        } else {
            self.notice = "Check retained · /check-result".into();
        }
        self.dirty = true;
        Ok(())
    }

    fn check_finding(&mut self, attempt: &str) -> Result<()> {
        let record = self.check_attempt(attempt)?;
        let envelope = &record["evidence"];
        ensure!(
            envelope.is_object(),
            "Check return is unknown; retained attempt can be read without rerunning"
        );
        if record["phase"] == "acknowledged" {
            self.detail(
                "Check follow-up accepted",
                format!(
                    "T3 accepted operation {}. This does not mean the work is complete.",
                    string(&record, "operationId")
                ),
            );
            return Ok(());
        }
        if self
            .state
            .pending(&string(&record["target"], "environmentId"), false)
            .is_some_and(|pending| pending["t3oxideCheckFollowUp"] == attempt)
        {
            self.detail("Check follow-up needs recovery", "Original native operation remains saved. Use Recover for explicit same-ID reconciliation.");
            return Ok(());
        }
        let description = format!(
            "{} / {}\nCommand: {} · exit {} · Wrapper: {}\n{} · Add a finding; Ctrl+] reviews the follow-up.",
            string(&record["target"], "environmentId"),
            string(&record["target"], "threadId"),
            string(&envelope["command"], "state"),
            envelope["command"]["exitCode"],
            envelope["wrapper"]["exitCode"],
            source_applicability(envelope)
        );
        self.text_dialog(
            "Check finding",
            &description,
            InputTarget::CheckFinding(attempt.into()),
            string(&record, "finding"),
        );
        let preview = evidence_preview(envelope);
        if let Some(dialog) = &mut self.dialog {
            dialog.detail = preview;
        }
        Ok(())
    }

    pub(super) fn check_save_finding(&self, attempt: &str, text: &str) -> Result<()> {
        ensure!(text.len() <= 8192, "Check finding exceeds 8 KiB");
        let current = self.check_attempt(attempt)?;
        let pending = self
            .state
            .pending(&string(&current["target"], "environmentId"), false)
            .is_some_and(|pending| pending["t3oxideCheckFollowUp"] == attempt);
        self.state.update(|saved| {
            let record = saved["checkAttempts"]
                .get_mut(attempt)
                .context("Original check unavailable")?;
            if record["finding"] != text && !pending {
                record
                    .as_object_mut()
                    .context("Invalid check record")?
                    .remove("followUp");
            }
            record["finding"] = json!(text);
            Ok(())
        })
    }

    pub(super) fn check_follow_up_review(&mut self, attempt: &str) -> Result<()> {
        let record = self.check_attempt(attempt)?;
        let finding = string(&record, "finding");
        ensure!(
            !finding.trim().is_empty(),
            "Add a finding before staging the follow-up"
        );
        let envelope = record["evidence"].clone();
        ensure!(envelope.is_object(), "Original check evidence unavailable");
        let message = format!(
            "Investigate this finite check using the returned source and output bytes.\nHuman finding: {finding}\nSource applicability: {}.\n\nEvidence is a bounded past observation, not a current source snapshot. Unknown outcomes and excerpts remain unknown/incomplete. Local retained paths do not grant access; the base64 blobs below are the delivered bytes. Do not silently rerun.\n{}",
            source_applicability(&envelope),
            serde_json::to_string(&envelope)?
        );
        ensure!(
            message.len() <= 64 * 1024,
            "Follow-up exceeds native message limit; originals retained"
        );
        self.state.update(|saved| {
            saved["checkAttempts"][attempt]["followUp"] = json!(message);
            Ok(())
        })?;
        let target = &record["target"];
        let choice = Choice::new(
            "Send follow-up",
            "Uses the original native conversation; keeps the composer and its model override",
            Intent::CheckSend(attempt.into()),
        );
        self.menu(
            "Check follow-up",
            format!(
                "{} / {}",
                string(target, "environmentId"),
                string(target, "threadId")
            ),
            vec![choice],
        );
        if let Some(dialog) = &mut self.dialog {
            dialog.detail = plain(&message);
        }
        Ok(())
    }

    pub(super) fn validate_check_send(
        &self,
        profile: usize,
        fields: &Value,
        retry: bool,
    ) -> Result<()> {
        let attempt = fields["t3oxideCheckFollowUp"]
            .as_str()
            .context("Invalid check follow-up origin")?;
        let record = self.check_attempt(attempt)?;
        ensure!(
            self.environments
                .get(profile)
                .is_some_and(|env| record["target"]["environmentId"] == env.environment_id)
                && fields["threadId"] == record["target"]["threadId"]
                && fields["message"] == record["followUp"]
                && fields["message"]
                    .as_str()
                    .is_some_and(|text| !text.is_empty())
                && fields.get("modelOverrideId").is_none(),
            "Check follow-up source or staged text changed"
        );
        ensure!(
            retry || (record["phase"] != "acknowledged" && record["operationId"].is_null()),
            "Check follow-up already accepted; no duplicate send"
        );
        if !retry {
            ensure!(
                self.source(profile, &string(&record["target"], "threadId"))
                    .project_id
                    == string(&record["target"], "projectId"),
                "Check target project changed; original evidence and finding retained"
            );
        }
        Ok(())
    }

    pub(super) fn check_send(&mut self, attempt: &str) -> Result<()> {
        let record = self.check_attempt(attempt)?;
        let environment = string(&record["target"], "environmentId");
        let profile = self
            .environments
            .iter()
            .position(|env| env.environment_id == environment)
            .context("Original check environment unavailable")?;
        let fields = json!({"threadId":record["target"]["threadId"],"message":record["followUp"],"mode":record["delivery"],"t3oxideCheckFollowUp":attempt});
        self.mutate(profile, "send", fields, None)
    }
}

fn evidence_preview(envelope: &Value) -> String {
    use base64::Engine as _;
    let mut preview = format!(
        "{}\nRepository-wide HEAD, index and dirty identity are unknown. No snapshot isolation or transient-write guarantee.\n",
        source_applicability(envelope)
    );
    for blob in array(envelope, "blobs") {
        if let Some(payload) = blob["dataBase64"].as_str()
            && let Ok(bytes) = base64::engine::general_purpose::STANDARD.decode(payload)
        {
            preview.push_str(&format!(
                "\n{} / {} · {} · {} returned bytes @ {}\n{}\n",
                string(blob, "role"),
                string(blob, "path"),
                string(blob, "coverage"),
                bytes.len(),
                string(blob, "sha256"),
                plain(&String::from_utf8_lossy(&bytes))
                    .chars()
                    .take(2000)
                    .collect::<String>()
            ));
        }
    }
    preview.push_str("\nDisplay excerpts use replacement characters for invalid UTF-8. Exact delivered bytes and coverage are retained in the evidence envelope.");
    preview
}

fn source_applicability(envelope: &Value) -> &'static str {
    let Some(paths) = envelope["request"]["sourcePaths"]
        .as_array()
        .filter(|paths| !paths.is_empty())
    else {
        return "Source applicability unknown";
    };
    let Some(before) = envelope["sourceBefore"]["entries"]
        .as_array()
        .filter(|entries| entries.len() == paths.len())
    else {
        return "Source applicability unknown";
    };
    let Some(after) = envelope["sourceAfter"]["entries"]
        .as_array()
        .filter(|entries| entries.len() == paths.len())
    else {
        return "Source applicability unknown";
    };
    let mut unknown = false;
    for ((before, after), path) in before.iter().zip(after).zip(paths) {
        if before["path"] != *path
            || after["path"] != *path
            || !before["error"].is_null()
            || !after["error"].is_null()
        {
            unknown = true;
            continue;
        }
        match (before["kind"].as_str(), after["kind"].as_str()) {
            (Some("absent"), Some("absent")) => {}
            (Some("regular"), Some("absent")) | (Some("absent"), Some("regular")) => {
                return "Selected source changed during check";
            }
            (Some("regular"), Some("regular")) => {
                match (
                    before["blob"]["originalSha256"].as_str(),
                    after["blob"]["originalSha256"].as_str(),
                ) {
                    (Some(left), Some(right))
                        if left != right || before["mode"] != after["mode"] =>
                    {
                        return "Selected source changed during check";
                    }
                    (Some(_), Some(_)) => {}
                    _ => unknown = true,
                }
            }
            _ => unknown = true,
        }
    }
    if unknown {
        "Source applicability unknown"
    } else {
        "Selected source matches before/after observations"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn late_and_reversed_evidence_reads_preserve_native_acceptance() {
        let mut record = json!({"phase":"acknowledged","operationId":"native-operation","readGeneration":2,"evidence":{"observation":"new"}});
        let accepted = record.clone();
        retain_observation(&mut record, 1, Ok(&json!({"observation":"old"})));
        assert_eq!(
            record, accepted,
            "older read must not replace admitted bytes"
        );
        retain_observation(&mut record, 2, Ok(&json!({"observation":"final"})));
        assert_eq!(record["phase"], "acknowledged");
        assert_eq!(record["operationId"], "native-operation");
        retain_observation(&mut record, 2, Err("late collection error"));
        assert_eq!(record["phase"], "acknowledged");
        assert_eq!(record["evidence"]["observation"], "final");
    }

    #[test]
    fn check_arguments_are_explicit_and_never_shell_expanded() {
        let parsed = parse_command(
            "--source 'src/a b.rs' --timeout 42 -- cargo test '$unexpanded'",
            "/work tree",
        )
        .unwrap();
        assert_eq!(parsed.sources, ["src/a b.rs"]);
        assert_eq!(parsed.argv, ["cargo", "test", "$unexpanded"]);
        assert_eq!(parsed.timeout, 42);
        for text in [
            "-- cargo test",
            "--source ../secret -- cargo test",
            "--timeout 901 --source a -- cargo test",
            "--source a --",
            "--source a --cwd -- cargo",
        ] {
            assert!(parse_command(text, "/work").is_err(), "{text}");
        }
    }
}
