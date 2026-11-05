use crate::{
    app::App,
    config,
    domain::{plain, string, valid_id},
    state::State,
    wire::Connection,
};
use anyhow::{Context, Result, bail, ensure};
use serde_json::{Value, json};
use std::{
    io::IsTerminal,
    path::PathBuf,
    sync::Arc,
    time::{Duration, Instant},
};

pub const HELP: &str = r#"t3oxide — Rust terminal workspace for T3 Code

Usage: t3oxide [COMMAND] [OPTIONS]

  t3oxide                                  open the workspace
  t3oxide --env desk --thread THREAD_ID     open an exact conversation
  t3oxide --mode a                          conversations (b for human questions)

Read-only commands (never submit mutations):
  envs [--json]                            configured environments
  threads --env ID [FILTER] [--json]        bounded native conversation list
  status --env ID [--thread ID]             authenticated adapter / thread status
  doctor [--env ID] [--json]                identity and metadata checks
  capabilities [--env ID]                  client / optional host capabilities
  activity --env ID --thread ID            bounded native tool activity
  inspect --env ID --thread ID --item ID    exact source item inspection

Options:
  --config PATH       environments.json (default ~/.config/t3oxide/)
  --state PATH        Rust state (default ~/.config/t3oxide/rust-state.json)
  --env ID            exact configured profile
  --thread ID         exact native conversation; requires --env
  --mode MODE         a or b; legacy dev/developer/agentbox accepted
  --theme NAME        terminal, violet, cyan or lime
  --no-mouse          keep native terminal selection
  --json              machine-readable read results
  --json-envelope     versioned threads page with completeness and continuation
  --scope NAME        threads: active, all, completed or failed
  --type NAME         threads: all, main, subagents or forks
  --cursor N          threads continuation offset
  --limit N           threads page size 1–200
  --revision KEY      inspect: observed item revision
  --activity-cursor C activity: opaque earlier page cursor
  --version / --help

Ctrl+P commands · Alt+H help · Ctrl+T settle / unsettle · Tab focus
Enter opens a conversation; in composer it inserts a newline.
Ctrl+] send · Ctrl+S Stop + queue hold · Ctrl+Y requests · Ctrl+Q exit.
Drafts are durable. Exit detaches; accepted native work continues.
"#;

#[derive(Clone, Debug)]
pub struct Args {
    pub command: String,
    pub config: PathBuf,
    pub state: PathBuf,
    pub env: String,
    pub thread: String,
    pub mode: String,
    pub theme: String,
    pub no_mouse: bool,
    pub json: bool,
    pub envelope: bool,
    pub scope: String,
    pub kind: String,
    pub cursor: u64,
    pub limit: u64,
    pub item: String,
    pub revision: String,
    pub activity_cursor: String,
    pub query: String,
}
impl Default for Args {
    fn default() -> Self {
        Self {
            command: String::new(),
            config: config::default_environments_path(),
            state: config::default_directory().join("rust-state.json"),
            env: String::new(),
            thread: String::new(),
            mode: String::new(),
            theme: String::new(),
            no_mouse: false,
            json: false,
            envelope: false,
            scope: "all".into(),
            kind: "all".into(),
            cursor: 0,
            limit: 100,
            item: String::new(),
            revision: String::new(),
            activity_cursor: String::new(),
            query: String::new(),
        }
    }
}
impl Args {
    pub fn parse(args: Vec<String>) -> Result<Self> {
        let mut result = Self::default();
        let mut i = 0;
        let mut words = Vec::new();
        while i < args.len() {
            let arg = &args[i];
            i += 1;
            match arg.as_str() {
                "--no-mouse" => result.no_mouse = true,
                "--json" => result.json = true,
                "--json-envelope" => {
                    result.json = true;
                    result.envelope = true;
                }
                "--config" | "--state" | "--env" | "--thread" | "--mode" | "--theme"
                | "--scope" | "--type" | "--cursor" | "--limit" | "--item" | "--revision"
                | "--activity-cursor" => {
                    let value = args
                        .get(i)
                        .with_context(|| format!("{arg} requires a value"))?
                        .clone();
                    i += 1;
                    ensure!(!value.starts_with("--"), "{arg} requires a value");
                    match arg.as_str() {
                        "--config" => result.config = value.into(),
                        "--state" => result.state = value.into(),
                        "--env" => result.env = value,
                        "--thread" => result.thread = value,
                        "--mode" => result.mode = value,
                        "--theme" => result.theme = value,
                        "--scope" => result.scope = value,
                        "--type" => result.kind = value,
                        "--cursor" => {
                            result.cursor = value.parse().context("cursor must be an integer")?
                        }
                        "--limit" => {
                            result.limit = value.parse().context("limit must be an integer")?
                        }
                        "--item" => result.item = value,
                        "--revision" => result.revision = value,
                        "--activity-cursor" => result.activity_cursor = value,
                        _ => unreachable!(),
                    }
                }
                value if value.starts_with('-') => bail!("unknown option {value}; use --help"),
                value if result.command.is_empty() => result.command = value.to_owned(),
                value => words.push(value.to_owned()),
            }
        }
        ensure!(
            [
                "",
                "envs",
                "threads",
                "status",
                "doctor",
                "capabilities",
                "activity",
                "inspect"
            ]
            .contains(&result.command.as_str()),
            "unknown command {}; use --help",
            result.command
        );
        ensure!(
            words.is_empty() || result.command == "threads",
            "unexpected positional arguments"
        );
        result.query = words.join(" ");
        ensure!(
            ["", "a", "b", "dev", "developer", "agentbox"].contains(&result.mode.as_str()),
            "mode must be a or b"
        );
        ensure!(
            ["", "terminal", "violet", "cyan", "lime"].contains(&result.theme.as_str()),
            "theme must be terminal, violet, cyan or lime"
        );
        ensure!(
            ["active", "all", "completed", "failed"].contains(&result.scope.as_str()),
            "invalid conversation scope"
        );
        ensure!(
            ["all", "main", "subagents", "forks"].contains(&result.kind.as_str()),
            "invalid conversation type"
        );
        ensure!(
            (1..=200).contains(&result.limit) && result.cursor <= 1_000_000,
            "invalid page limit or cursor"
        );
        ensure!(
            result.thread.is_empty() || !result.env.is_empty(),
            "--thread requires --env"
        );
        for id in [&result.env, &result.thread, &result.item] {
            ensure!(id.is_empty() || valid_id(id), "invalid source identity");
        }
        ensure!(
            !matches!(result.command.as_str(), "activity" | "inspect") || !result.thread.is_empty(),
            "activity and inspect require --env and --thread"
        );
        ensure!(
            result.command != "inspect" || !result.item.is_empty(),
            "inspect requires --item"
        );
        ensure!(
            result.revision.len() <= 128 && result.activity_cursor.len() <= 64 * 1024,
            "inspection cursor exceeds limit"
        );
        ensure!(
            !result.json || !result.command.is_empty(),
            "--json requires a read-only command"
        );
        ensure!(
            !result.envelope || result.command == "threads",
            "--json-envelope requires threads"
        );
        Ok(result)
    }
}

pub fn run(args: Vec<String>) -> Result<()> {
    if args.iter().any(|a| a == "--help" || a == "-h") {
        print!("{HELP}");
        return Ok(());
    }
    if args.iter().any(|a| a == "--version") {
        println!("t3oxide {} · Rust / SLT", env!("CARGO_PKG_VERSION"));
        return Ok(());
    }
    let args = Args::parse(args)?;
    if args.command == "capabilities" && args.env.is_empty() {
        println!("{}", serde_json::to_string_pretty(&capabilities())?);
        return Ok(());
    }
    let envs = config::load(&args.config).with_context(|| {
        format!(
            "configure {} using config/environments.example.json",
            args.config.display()
        )
    })?;
    if args.command == "envs" {
        if args.json {
            println!("{}", serde_json::to_string_pretty(&envs)?);
        } else {
            for e in envs {
                println!(
                    "{}\t{}\t{}",
                    plain(&e.id),
                    plain(&e.label),
                    plain(&e.environment_id)
                );
            }
        }
        return Ok(());
    }
    ensure!(!envs.is_empty(), "no enabled environments configured");
    if !args.command.is_empty() {
        return headless(&args, &envs);
    }
    ensure!(
        std::io::stdin().is_terminal() && std::io::stdout().is_terminal(),
        "interactive mode requires a terminal; use a read-only command or --help"
    );
    let state = Arc::new(State::open(&args.state)?);
    App::new(args, envs, state)?.run()
}

fn capabilities() -> Value {
    json!({"schemaVersion":1,"client":"t3oxide-rust","version":env!("CARGO_PKG_VERSION"),
    "modes":["dev"],"transport":"companion-v1 stdio / OpenSSH","orchestrationProtocol":2,
    "readCommands":["envs","threads","status","doctor","capabilities","activity","inspect"],
    "writes":"explicit human TUI actions only; host capabilities gate each action","recovery":"durable original IDs; explicit reconciliation only"})
}

fn headless(args: &Args, envs: &[config::Environment]) -> Result<()> {
    let selected: Vec<_> = envs
        .iter()
        .filter(|e| args.env.is_empty() || e.id == args.env)
        .collect();
    ensure!(!selected.is_empty(), "unknown environment {}", args.env);
    if !matches!(args.command.as_str(), "doctor" | "capabilities") {
        ensure!(selected.len() == 1, "select one source with --env");
    }
    let mut checks = Vec::new();
    let mut failed = false;
    for env in selected {
        let started = Instant::now();
        let outcome = (|| -> Result<Value> {
            let connection = Connection::start(env.clone())?;
            let hello = connection.hello()?;
            let result = (|| -> Result<Value> {
                match args.command.as_str() {
                    "doctor" => {
                        let page = connection.request(
                            "list",
                            json!({"limit": 1, "lifecycle": "all"}),
                            None,
                        )?;
                        Ok(json!({
                            "profile": env.id,
                            "ok": true,
                            "hello": hello,
                            "metadataRead": page.get("threads").is_some(),
                            "elapsedMs": started.elapsed().as_millis(),
                        }))
                    }
                    "capabilities" => Ok(json!({"client": capabilities(), "host": hello})),
                    "threads" => {
                        let page = connection.request(
                            "list",
                            json!({
                                "query": args.query,
                                "lifecycle": args.scope,
                                "typeFilter": args.kind,
                                "cursor": args.cursor,
                                "limit": args.limit,
                                "grouped": false,
                                "includeChildren": true,
                            }),
                            None,
                        )?;
                        if args.envelope {
                            Ok(json!({
                                "schemaVersion": 1,
                                "profileId": env.id,
                                "environmentId": env.environment_id,
                                "page": page,
                            }))
                        } else {
                            Ok(page)
                        }
                    }
                    "status" if args.thread.is_empty() => Ok(hello),
                    "status" => {
                        let events = connection.events();
                        connection.request("watch", json!({"threadId": args.thread}), None)?;
                        let deadline = Instant::now() + Duration::from_secs(20);
                        let mut projection = crate::projection::Projection::default();
                        loop {
                            let remaining = deadline.saturating_duration_since(Instant::now());
                            ensure!(
                                !remaining.is_zero(),
                                "no verified live snapshot before deadline"
                            );
                            let event = events
                                .recv_timeout(remaining)
                                .context("thread snapshot unavailable")?;
                            if event.event == "view" {
                                projection.apply_snapshot(event.data, &args.thread)?;
                            } else if event.event == "delta" {
                                projection.apply_delta(event.data)?;
                            }
                            if let Some(s) = projection.snapshot()
                                && s.metadata["live"] == true
                            {
                                return Ok(json!({
                                    "hello": hello,
                                    "view": {
                                        "revision": s.revision,
                                        "metadata": s.metadata,
                                        "rows": s.items.len(),
                                    },
                                }));
                            }
                        }
                    }
                    "activity" => connection
                        .request(
                            "activity",
                            json!({
                                "threadId": args.thread,
                                "cursor": if args.activity_cursor.is_empty() {
                                    Value::Null
                                } else {
                                    json!(args.activity_cursor)
                                },
                            }),
                            None,
                        )
                        .map_err(Into::into),
                    "inspect" => connection
                        .request(
                            "inspect-item",
                            json!({
                                "threadId": args.thread,
                                "sourceThreadId": args.thread,
                                "itemId": args.item,
                                "revision": args.revision,
                            }),
                            None,
                        )
                        .map_err(Into::into),
                    _ => bail!("unsupported read command"),
                }
            })();
            connection.close();
            result
        })();
        match outcome {
            Ok(value) => {
                if args.command == "doctor" {
                    checks.push(value);
                } else if args.command == "threads" && !args.json {
                    for row in value["threads"].as_array().into_iter().flatten() {
                        println!(
                            "{}\t{}\t{}",
                            plain(&string(row, "id")),
                            plain(&string(row, "status")),
                            plain(&string(row, "title")),
                        );
                    }
                    if value["more"] == true {
                        eprintln!(
                            "More conversations available. next cursor: {}",
                            value["nextCursor"]
                        );
                    }
                } else {
                    println!(
                        "{}",
                        serde_json::to_string_pretty(
                            if args.command == "threads" && !args.envelope {
                                &value["threads"]
                            } else {
                                &value
                            }
                        )?,
                    );
                }
            }
            Err(error) if args.command == "doctor" => {
                failed = true;
                checks.push(json!({"profile": env.id, "ok": false, "error": format!("{error:#}")}));
            }
            Err(error) => return Err(error),
        }
    }
    if args.command == "doctor" {
        if args.json {
            println!("{}", serde_json::to_string_pretty(&checks)?)
        } else {
            for c in checks {
                println!(
                    "{}: {}",
                    string(&c, "profile"),
                    if c["ok"] == true {
                        "ready".into()
                    } else {
                        plain(&string(&c, "error"))
                    },
                );
            }
        }
    }
    ensure!(
        !failed,
        "one or more environments unavailable; healthy environments remain independent"
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    fn parse(a: &[&str]) -> Result<Args> {
        Args::parse(a.iter().map(|x| x.to_string()).collect())
    }
    #[test]
    fn cli_is_read_only() {
        assert!(parse(&["send", "hello"]).is_err());
        assert!(parse(&["threads", "--env", "desk", "hello"]).is_ok());
    }
    #[test]
    fn exact_source_required() {
        assert!(parse(&["--thread", "x"]).is_err());
        assert!(parse(&["inspect", "--env", "a", "--thread", "t"]).is_err());
    }
    #[test]
    fn bounds_are_enforced() {
        assert!(parse(&["threads", "--limit", "201"]).is_err());
        assert!(parse(&["threads", "--cursor", "-1"]).is_err());
    }
}
