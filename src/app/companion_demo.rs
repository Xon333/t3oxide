//! Reversible demo of existing tools beside the native conversation controller.
use super::*;
use std::{path::PathBuf, process::Stdio};

impl App {
    pub(super) fn demo_companion(
        &mut self,
        tool: &str,
        source: Option<crate::slt_ui::Source>,
    ) -> Result<()> {
        ensure!(
            matches!(tool, "graph" | "guide" | "review" | "mermaid" | "form"),
            "Unknown demo tool"
        );
        let launcher = std::env::var_os("T3OXIDE_DEMO_COMPANIONS")
            .map(PathBuf::from)
            .context("Companion demo is not enabled")?;
        ensure!(
            launcher.is_absolute() && launcher.is_file(),
            "Demo launcher unavailable"
        );
        let source = source.or_else(|| {
            self.active
                .as_ref()
                .map(|active| self.source(active.profile, &active.thread))
        });
        let mut request = json!({"tool":tool,"source":null});
        if let Some(source) = source {
            ensure!(
                self.environments
                    .get(source.profile)
                    .is_some_and(|env| env.environment_id == source.environment),
                "Original demo source environment changed"
            );
            let threads: Vec<_> = self.threads.iter()
                .filter(|row| row.profile == source.profile && row.project_id == source.project_id)
                .map(|row| json!({"id":row.id,"title":row.title,"kind":row.kind(),"parent":row.parent()}))
                .collect();
            request["source"] = json!({
                "environmentId":source.environment,"projectId":source.project_id,
                "project":source.project,"threadId":source.thread,
                "coverage":"Loaded native metadata only; partial relationships",
                "threads":threads
            });
        }
        if tool == "graph" {
            ensure!(
                request["source"]["projectId"]
                    .as_str()
                    .is_some_and(domain::valid_id),
                "Open a project conversation before its graph"
            );
        }
        let bytes = serde_json::to_vec(&request)?;
        ensure!(bytes.len() <= 512 * 1024, "Demo snapshot is too large");
        let tool = tool.to_owned();
        let tx = self.tx.clone();
        self.pool.submit(move || {
            let result = (|| -> Result<()> {
                let mut child = std::process::Command::new(launcher)
                    .stdin(Stdio::piped())
                    .stdout(Stdio::piped())
                    .stderr(Stdio::piped())
                    .spawn()
                    .context("Start demo companion launcher")?;
                let write = child
                    .stdin
                    .take()
                    .context("Demo input unavailable")?
                    .write_all(&bytes);
                let output = child.wait_with_output()?;
                write?;
                ensure!(
                    output.status.success(),
                    "{}",
                    String::from_utf8_lossy(&output.stderr).trim()
                );
                Ok(())
            })()
            .map_err(|error| error.to_string());
            let _ = tx.send(Background::DemoCompanion { tool, result });
        })?;
        self.notice = "Opening demo companion…".into();
        Ok(())
    }
}
