//! Read-only reports retain the original loaded source, independently of questions.
use super::*;
use crate::{projection::Row, slt_ui::ContentItem};
use pulldown_cmark::{Event, Parser, Tag, TagEnd};

// At most 6 MiB of native bounded message text, plus source envelopes.
const MAX_LOADED_CONTENT: usize = 256;

impl App {
    pub(super) fn observe_loaded_content(&mut self) -> Result<()> {
        if self.context_io.draining {
            return Ok(());
        }
        let Some(active) = &self.active else {
            return Ok(());
        };
        let Some(snapshot) = active.projection.snapshot() else {
            return Ok(());
        };
        let profile = active.profile;
        let observed = json!({"live":snapshot.is_live(),"historical":snapshot.metadata["historical"],
            "state":snapshot.metadata["state"],"limited":snapshot.metadata["limited"],
            "hasMore":snapshot.metadata["hasMore"],"capturedItemOnly":true,"streaming":false});
        let title = string(&snapshot.metadata, "title");
        if !title.trim().is_empty() {
            self.tab_titles
                .insert((profile, active.thread.clone()), domain::plain(&title));
        }
        let rows: Vec<_> = snapshot
            .items
            .iter()
            .filter(|row| {
                row.kind == "assistant_message" && !row.streaming && !row.text.trim().is_empty()
            })
            .cloned()
            .collect();
        for row in rows {
            let source = self.source(profile, &row.source_thread_id);
            let key = serde_json::to_string(&[&source.environment, &source.thread, &row.item_id])?;
            let revision = row_revision(&row)?;
            let capture_key = format!("{key}\0{revision}");
            if !self.context_capture_known.contains(&capture_key)
                && domain::valid_id(&source.project_id)
            {
                if (self.context_capture_pending.len() >= MAX_LOADED_CONTENT
                    && !self.context_capture_pending.contains_key(&capture_key))
                    || serde_json::to_vec(&row)?.len() > super::context_worker::MAX_SOURCE_BYTES
                {
                    self.context_capture_status = format!(
                        "Capture queue full · {} / {} / {} @ {} · source retained in T3",
                        source.environment, source.thread, row.item_id, revision
                    );
                } else {
                    let mut observation = observed.clone();
                    observation["truncated"] = json!(row.truncated);
                    self.context_capture_pending.insert(
                        capture_key,
                        crate::context_store::RecordInput {
                            source: crate::context_store::Source {
                                environment_id: source.environment.clone(),
                                project_id: source.project_id.clone(),
                                thread_id: source.thread.clone(),
                                item_id: Some(row.item_id.clone()),
                                request_id: None,
                                revision: revision.clone(),
                                observed: observation,
                            },
                            kind: crate::context_store::Kind::Evidence,
                            text: row.text.chars().take(1200).collect(),
                            full_source: serde_json::to_vec(&row)?,
                            supersedes: None,
                            revisit: None,
                        },
                    );
                }
            }
            if self
                .loaded_contents
                .iter()
                .any(|item| item.key == key && item.revision == revision)
            {
                continue;
            }
            let heading = authored_heading(&row.text);
            let kind = content_kind(heading.as_deref()).to_owned();
            let title = heading.unwrap_or_else(|| {
                self.tab_titles
                    .get(&(profile, row.source_thread_id.clone()))
                    .cloned()
                    .unwrap_or_else(|| "Agent output".into())
            });
            self.loaded_contents.retain(|item| item.key != key);
            self.loaded_contents.push_front(ContentItem {
                key,
                source,
                title,
                kind,
                markdown: row.text,
                revision,
                truncated: row.truncated,
            });
        }
        self.loaded_contents.truncate(MAX_LOADED_CONTENT);
        self.schedule_context_capture()?;
        Ok(())
    }

    pub(super) fn schedule_context_capture(&mut self) -> Result<()> {
        self.context_capture_pending
            .retain(|key, _| !self.context_capture_known.contains(key));
        if self.context_writes_blocked() || self.context_capture_pending.is_empty() {
            return Ok(());
        }
        let mut keys: Vec<_> = self.context_capture_pending.keys().cloned().collect();
        keys.sort();
        let mut inputs = Vec::new();
        let mut accepted = Vec::new();
        let mut bytes = 0usize;
        for key in keys.into_iter().take(200) {
            let input = &self.context_capture_pending[&key];
            if input.full_source.len()
                > super::context_worker::MAX_SOURCE_BYTES.saturating_sub(bytes)
            {
                break;
            }
            bytes += input.full_source.len();
            inputs.push(
                self.context_capture_pending
                    .remove(&key)
                    .expect("pending source retained"),
            );
            accepted.push(key);
        }
        if inputs.is_empty() {
            return Ok(());
        }
        let request = super::context_worker::WriteRequest::new(
            self.generation,
            super::context_worker::Write::ObserveLoaded(inputs),
        );
        match self.try_context_write(request) {
            Ok(()) => self.context_capture_known.extend(accepted),
            Err((error, request)) => {
                let super::context_worker::Write::ObserveLoaded(inputs) = request.operation else {
                    unreachable!()
                };
                for (key, input) in accepted.into_iter().zip(inputs) {
                    self.context_capture_pending.insert(key, input);
                }
                self.context_capture_status = error;
            }
        }
        Ok(())
    }
}

pub(super) fn row_revision(row: &Row) -> Result<String> {
    Ok(crate::reports::content_revision(&serde_json::to_string(
        row,
    )?))
}

pub(super) fn capture_key(source: &crate::context_store::Source) -> Result<String> {
    let key = serde_json::to_string(&[
        &source.environment_id,
        &source.thread_id,
        source.item_id.as_deref().unwrap_or(""),
    ])?;
    Ok(format!("{key}\0{}", source.revision))
}

fn authored_heading(markdown: &str) -> Option<String> {
    let mut in_heading = false;
    let mut heading = String::new();
    for event in Parser::new(markdown) {
        match event {
            Event::Start(Tag::Heading { .. }) => in_heading = true,
            Event::End(TagEnd::Heading(_)) if in_heading => {
                return (!heading.trim().is_empty()).then(|| heading.chars().take(160).collect());
            }
            Event::Text(text) | Event::Code(text) if in_heading => heading.push_str(&text),
            Event::SoftBreak | Event::HardBreak if in_heading => heading.push(' '),
            _ => {}
        }
    }
    None
}

fn content_kind(heading: Option<&str>) -> &'static str {
    let normalized = heading.unwrap_or("").trim().to_ascii_lowercase();
    let named = |name: &str| {
        normalized == name
            || normalized
                .strip_prefix(name)
                .is_some_and(|tail| tail.starts_with([' ', ':', '—', '–', '-']))
    };
    if named("research") {
        "research"
    } else if named("guide") {
        "guide"
    } else if named("receipt") || named("completion receipt") {
        "receipt"
    } else {
        "explanation"
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_authored_heading_specializes_read_only_content() {
        for (markdown, expected) in [
            (
                "Research suggests using a guide. Completed successfully.",
                "explanation",
            ),
            (
                "```markdown\n## Research\n```\nOrdinary output",
                "explanation",
            ),
            (
                "## **Research**: input forwarding\n- Source: native API",
                "research",
            ),
            ("# Guide\n1. Review the source", "guide"),
            ("## Completion receipt\n- Outcome: implemented", "receipt"),
            ("## Release evidence\n- Checks passed", "explanation"),
        ] {
            assert_eq!(
                content_kind(authored_heading(markdown).as_deref()),
                expected,
                "{markdown}"
            );
        }
    }
}
