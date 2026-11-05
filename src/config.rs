//! Existing environment profiles, shared unchanged with the host adapter.
use anyhow::{Context, Result, bail, ensure};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use std::fs::File;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::Command;

pub const MAX_CONFIG_BYTES: usize = 64 * 1024;

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct Environment {
    pub id: String,
    pub label: String,
    pub environment_id: String,
    #[serde(default)]
    pub ssh_destination: Option<String>,
    pub node: String,
    pub adapter: String,
    pub host_config: String,
    #[serde(default, rename = "webURL")]
    pub web_url: Option<String>,
    #[serde(default)]
    pub enabled: bool,
    #[serde(default)]
    pub default_for_new: bool,
    #[serde(default, rename = "allowedProviderInstanceIds")]
    pub allowed_provider_instance_ids: Option<Vec<String>>,
}

pub fn valid_id(value: &str, limit: usize) -> bool {
    !value.is_empty() && value.len() <= limit && !value.chars().any(|c| c < ' ')
}

impl Environment {
    pub fn validate(&self) -> Result<()> {
        ensure!(
            valid_id(&self.id, 4096) && valid_id(&self.environment_id, 4096),
            "environment needs exact profile and environment identities"
        );
        ensure!(
            self.label.len() <= 4096,
            "environment label exceeds its limit"
        );
        for path in [&self.node, &self.adapter, &self.host_config] {
            ensure!(
                Path::new(path).is_absolute() && !path.contains(['\0', '\r', '\n']),
                "environment {:?} needs absolute host-local executable and configuration paths",
                self.label
            );
        }
        if let Some(alias) = &self.ssh_destination {
            ensure!(
                !alias.is_empty()
                    && !alias.starts_with('-')
                    && alias
                        .bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b"_.@-".contains(&b)),
                "environment needs an existing SSH alias"
            );
        }
        if let Some(url) = &self.web_url {
            let parsed = url::Url::parse(url).context("invalid Full T3 URL")?;
            ensure!(
                matches!(parsed.scheme(), "http" | "https")
                    && parsed.host_str().is_some()
                    && parsed.username().is_empty()
                    && parsed.password().is_none()
                    && parsed.query().is_none()
                    && parsed.fragment().is_none(),
                "Full T3 URL must be HTTP(S), without credentials, query, or fragment"
            );
        }
        if let Some(ids) = &self.allowed_provider_instance_ids {
            let unique: HashSet<_> = ids.iter().collect();
            ensure!(
                !ids.is_empty()
                    && ids.len() <= 64
                    && unique.len() == ids.len()
                    && ids.iter().all(|v| valid_id(v, 4096)),
                "provider policy needs 1–64 unique exact instance IDs"
            );
        }
        Ok(())
    }

    pub fn check_provider_instance(&self, id: &str) -> Result<()> {
        if let Some(ids) = &self.allowed_provider_instance_ids {
            ensure!(
                ids.iter().any(|v| v == id),
                "provider instance {id:?} is not allowed by this environment profile"
            );
        }
        Ok(())
    }

    /// No local shell. The only shell syntax is one correctly quoted SSH command.
    pub fn command(&self) -> Command {
        if let Some(alias) = &self.ssh_destination {
            let mut command = Command::new("/usr/bin/ssh");
            command.args([
                "-T",
                "-o",
                "BatchMode=yes",
                "-o",
                "StrictHostKeyChecking=yes",
                "-o",
                "ConnectTimeout=8",
                "-o",
                "ServerAliveInterval=30",
                "-o",
                "ServerAliveCountMax=2",
                "--",
                alias,
            ]);
            command.arg(format!(
                "exec {} {} --config {}",
                shell_quote(&self.node),
                shell_quote(&self.adapter),
                shell_quote(&self.host_config)
            ));
            command
        } else {
            let mut command = Command::new(&self.node);
            command.args([&self.adapter, "--config", &self.host_config]);
            command
        }
    }
}

fn shell_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
}

pub fn default_directory() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_default()
        .join(".config/t3oxide")
}
pub fn default_environments_path() -> PathBuf {
    default_directory().join("environments.json")
}

pub fn read_bounded(path: &Path, limit: usize) -> Result<Vec<u8>> {
    let file = File::open(path).with_context(|| format!("read {}", path.display()))?;
    let mut bytes = Vec::new();
    file.take(limit as u64 + 1).read_to_end(&mut bytes)?;
    ensure!(
        bytes.len() <= limit,
        "{} exceeds its {} byte limit",
        path.display(),
        limit
    );
    Ok(bytes)
}

pub fn load(path: &Path) -> Result<Vec<Environment>> {
    #[derive(Deserialize)]
    struct Saved {
        version: u32,
        environments: Vec<Environment>,
    }
    let saved: Saved = serde_json::from_slice(&read_bounded(path, MAX_CONFIG_BYTES)?)
        .context("decode environment profiles")?;
    ensure!(
        saved.version == 1 && saved.environments.len() <= 8,
        "expected version 1 and at most eight environments"
    );
    let mut ids = HashSet::new();
    let mut native_ids = HashSet::new();
    let mut defaults = 0;
    let mut enabled = Vec::new();
    for environment in saved.environments {
        environment.validate()?;
        ensure!(
            ids.insert(environment.id.clone())
                && native_ids.insert(environment.environment_id.clone()),
            "profile IDs and native environment identities must be unique"
        );
        if environment.enabled {
            defaults += usize::from(environment.default_for_new);
            if defaults > 1 {
                bail!("only one enabled environment may be defaultForNew");
            }
            enabled.push(environment);
        }
    }
    Ok(enabled)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn environment() -> Environment {
        serde_json::from_value(serde_json::json!({"id":"p", "label":"Local", "environmentId":"native",
            "node":"/usr/bin/node", "adapter":"/tmp/adapter's/main.mjs", "hostConfig":"/tmp/host.json", "enabled":true})).unwrap()
    }
    #[test]
    fn ssh_command_has_no_local_shell_and_quotes_remote_paths() {
        let mut env = environment();
        env.ssh_destination = Some("host-alias".into());
        env.validate().unwrap();
        let command = env.command();
        assert_eq!(command.get_program(), "/usr/bin/ssh");
        let args: Vec<_> = command.get_args().collect();
        assert_eq!(
            args.last().unwrap(),
            &"exec '/usr/bin/node' '/tmp/adapter'\\''s/main.mjs' --config '/tmp/host.json'"
        );
    }
    #[test]
    fn profile_rejects_ssh_options_and_credential_urls() {
        let mut env = environment();
        env.ssh_destination = Some("-oProxyCommand=evil".into());
        assert!(env.validate().is_err());
        env.ssh_destination = None;
        for url in [
            "https://user:pass@example.test",
            "https://example.test/?token=x",
            "file:///tmp/test",
            "https://example.test/#x",
        ] {
            env.web_url = Some(url.into());
            assert!(env.validate().is_err());
        }
        env.web_url = Some("https://example.test/t3".into());
        assert!(env.validate().is_ok());
    }
    #[test]
    fn provider_policy_distinguishes_absence_from_empty() {
        let mut env = environment();
        assert!(env.check_provider_instance("any").is_ok());
        env.allowed_provider_instance_ids = Some(vec![]);
        assert!(env.validate().is_err());
        env.allowed_provider_instance_ids = Some(vec!["exact".into()]);
        assert!(env.validate().is_ok());
        assert!(env.check_provider_instance("exact").is_ok());
        assert!(env.check_provider_instance("other").is_err());
    }
}
