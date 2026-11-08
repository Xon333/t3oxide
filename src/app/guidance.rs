//! Source-qualified local preferences for new ordinary composer messages.
use super::*;
use crate::{guidance::Guidance, slt_ui::Source};

#[derive(Clone, Copy, Debug)]
pub(crate) enum Scope {
    Project,
    Conversation,
}

#[derive(Clone, Debug)]
pub(crate) enum Action {
    Set(Source, Scope, Option<Guidance>),
    Inspect(Source),
}

fn label(choice: Option<Guidance>) -> &'static str {
    match choice {
        None => "Unset",
        Some(Guidance::Off) => "Off",
        Some(Guidance::Graphing) => "Graphing",
        Some(Guidance::Classic) => "Classic subagents",
    }
}

impl App {
    fn guidance_source(&self) -> Result<Source> {
        let active = self.active.as_ref().context("Open a conversation first")?;
        let snapshot = active
            .projection
            .snapshot()
            .context("Refresh the conversation before changing guidance")?;
        ensure!(
            !active.navigating && !active.projection.needs_resnapshot && snapshot.is_live(),
            "Return to Latest before changing guidance"
        );
        let mut source = self.source(active.profile, &active.thread);
        source.project_id = string(&snapshot.metadata, "projectId");
        ensure!(
            domain::valid_id(&source.environment)
                && domain::valid_id(&source.project_id)
                && domain::valid_id(&source.thread),
            "Guidance needs the attached conversation's native project; refresh first"
        );
        Ok(source)
    }

    fn check_guidance_source(&self, original: &Source) -> Result<Source> {
        let current = self.guidance_source()?;
        ensure!(
            current.profile == original.profile
                && current.environment == original.environment
                && current.project_id == original.project_id
                && current.thread == original.thread,
            "Guidance source changed; reopen it on the intended conversation"
        );
        Ok(current)
    }

    pub(super) fn send_guidance(&self) -> Result<Option<Guidance>> {
        let active = self.active.as_ref().context("Open a conversation first")?;
        let environment = &self.environments[active.profile].environment_id;
        let project = string(&self.metadata(), "projectId");
        if !domain::valid_id(&project) {
            // Legacy/unavailable project metadata cannot choose another scope.
            // With no preference for this source, existing sends stay unchanged.
            let saved = self.state.snapshot();
            let preferences = &saved["orchestrationGuidance"];
            let projects = preferences["projects"].as_object().is_some_and(|entries| {
                entries.keys().any(|key| {
                    serde_json::from_str::<Vec<String>>(key)
                        .is_ok_and(|ids| ids.first() == Some(environment))
                })
            });
            let conversation = crate::guidance::scope_key(environment, &active.thread)?;
            ensure!(
                !projects && preferences["conversations"].get(conversation).is_none(),
                "Guidance project is unavailable; refresh before sending"
            );
            return Ok(None);
        }
        self.state
            .effective_guidance(environment, &project, &active.thread)
    }

    pub(super) fn guidance_label(&self) -> String {
        let Some(active) = &self.active else {
            return String::new();
        };
        let project = string(&self.metadata(), "projectId");
        let environment = &self.environments[active.profile].environment_id;
        match self.send_guidance() {
            Ok(None) => String::new(),
            Ok(Some(preference)) => {
                let overridden = self
                    .state
                    .conversation_guidance(environment, &project, &active.thread)
                    .ok()
                    .flatten()
                    .is_some();
                format!(
                    "{} · {}",
                    label(Some(preference)),
                    if overridden { "override" } else { "project" }
                )
            }
            Err(_) => "Guidance unavailable".into(),
        }
    }

    pub(super) fn guidance_menu(&mut self) -> Result<()> {
        let source = self.guidance_source()?;
        let project = self
            .state
            .project_guidance(&source.environment, &source.project_id)?;
        let conversation = self.state.conversation_guidance(
            &source.environment,
            &source.project_id,
            &source.thread,
        )?;
        let effective = conversation.or(project);
        let mut choices = vec![Choice::new(
            "Exact appended text",
            "Inspect the effective text, including its separator. Viewing never sends.",
            Intent::Guidance(Action::Inspect(source.clone())),
        )];
        for (scope, name, selected) in [
            (Scope::Project, "Project", project),
            (Scope::Conversation, "Conversation", conversation),
        ] {
            for preference in [Guidance::Graphing, Guidance::Classic, Guidance::Off] {
                let mut choice = Choice::new(
                    format!("{name}: {}", label(Some(preference))),
                    "Save this local preference. It does not send or alter the selected account/model.",
                    Intent::Guidance(Action::Set(source.clone(), scope, Some(preference))),
                );
                choice.checked = selected == Some(preference);
                choices.push(choice);
            }
        }
        let mut inherit = Choice::new(
            "Conversation: Use project default",
            "Remove this conversation's override.",
            Intent::Guidance(Action::Set(source, Scope::Conversation, None)),
        );
        inherit.checked = conversation.is_none();
        choices.push(inherit);
        self.menu("Orchestration",format!("Effective: {} · {}\nProject: {} · Conversation: {}\nPrompt guidance; native instructions and limits apply. Ordinary composer sends only.",
            label(effective),if conversation.is_some(){"conversation override"}else{"project default"},label(project),
            if conversation.is_some(){label(conversation)}else{"Inherit"}),choices);
        Ok(())
    }

    pub(super) fn guidance_action(&mut self, action: Action) -> Result<()> {
        match action {
            Action::Set(original, scope, preference) => {
                let source = self.check_guidance_source(&original)?;
                match scope {
                    Scope::Project => self.state.set_project_guidance(
                        &source.environment,
                        &source.project_id,
                        preference,
                    )?,
                    Scope::Conversation => self.state.set_conversation_guidance(
                        &source.environment,
                        &source.project_id,
                        &source.thread,
                        preference,
                    )?,
                }
                self.guidance_menu()?;
            }
            Action::Inspect(original) => {
                let source = self.check_guidance_source(&original)?;
                let preference = self.state.effective_guidance(
                    &source.environment,
                    &source.project_id,
                    &source.thread,
                )?;
                let suffix = preference.map_or("", Guidance::suffix);
                self.detail("Exact orchestration text",if suffix.is_empty() {
                    "No text is added to new ordinary composer messages.".into()
                } else {
                    format!("Exact suffix begins with two newline bytes and has no trailing newline. Ordinary composer sends only; this is prompt guidance, not runtime enforcement.\n\n```text\n{suffix}\n```\nPrompt bytes are frozen before dispatch; recall retains only your original prompt.")
                });
            }
        }
        Ok(())
    }
}
