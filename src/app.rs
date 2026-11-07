//! One event-driven client workspace. Background reads never acquire mutation
//! authority; every write is journaled before it reaches the adapter.
mod actions;
mod checks;
mod content;
mod context;
mod context_worker;
#[cfg(test)]
mod input;
#[cfg(test)]
mod lifecycle_tests;
mod native;
#[cfg(test)]
mod recovery_tests;
mod slt;
mod workspace;

use crate::{
    cli::Args,
    config::Environment,
    domain::{self, Thread, ThreadPage, array, flag, plain, string},
    projection::Projection,
    state::State,
    ui::{self, Focus, LayoutMode, Mode},
    wire::{self, Connection, Packet},
};
use anyhow::{Context, Result, anyhow, bail, ensure};
use crossbeam_channel::{Receiver, Sender, bounded};
#[cfg(test)]
use crossterm::event::Event;
use ratatui_textarea::{CursorMove, TextArea};
use serde_json::{Value, json};
use std::{
    collections::{HashMap, HashSet, VecDeque},
    io::Write,
    sync::{Arc, Mutex},
    thread,
    time::{Duration, Instant},
};

pub(super) type Job = Box<dyn FnOnce() + Send + 'static>;
// Refresh unopened sources without a subscription per conversation or a second loop.
const METADATA_REFRESH_INTERVAL: Duration = Duration::from_secs(5);
pub(super) struct Pool {
    tx: Sender<Job>,
}
impl Pool {
    fn new(workers: usize, capacity: usize) -> Self {
        let (tx, rx) = bounded::<Job>(capacity);
        for _ in 0..workers {
            let rx = rx.clone();
            thread::spawn(move || {
                while let Ok(job) = rx.recv() {
                    job();
                }
            });
        }
        Self { tx }
    }
    fn submit(&self, job: impl FnOnce() + Send + 'static) -> Result<()> {
        self.tx
            .try_send(Box::new(job))
            .map_err(|_| anyhow!("read queue is full; wait for the current reads"))
    }
}

pub(super) enum Background {
    Page {
        profile: usize,
        generation: u64,
        append: bool,
        result: Result<Value, String>,
    },
    Opened {
        generation: u64,
        connection: Result<(Connection, Value), String>,
    },
    Packet {
        generation: u64,
        packet: Packet,
    },
    Disconnected {
        generation: u64,
    },
    Read {
        generation: u64,
        serial: u64,
        kind: ReadKind,
        result: Result<Value, String>,
    },
    Mutation {
        context: MutationContext,
        id: String,
        op: String,
        fields: Value,
        result: Result<Value, String>,
    },
    Saved {
        key: String,
        version: u64,
        result: Result<(), String>,
    },
    Context(context_worker::Event),
    Check {
        attempt: String,
        result: Result<(), String>,
    },
}
#[derive(Clone, Copy, Debug)]
pub(super) struct MutationContext {
    generation: u64,
    profile: usize,
    draft_version: Option<u64>,
}
#[derive(Clone, Debug)]
pub(super) enum ReadKind {
    Projects { profile: usize, purpose: NewPurpose },
    Defaults { profile: usize, project: Value },
    Models(ModelScope),
    Effort(ModelScope),
    Review,
    Activity,
    Inspect,
    Preview,
    NativeDefaults,
    History { navigation: u64 },
    Watch { navigation: u64 },
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum NewPurpose {
    Launch,
    Same,
    Destination,
    Batch,
    NativeDefaults,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum ModelScope {
    Current,
    Next,
    Launch,
}
#[derive(Clone, Debug)]
pub(super) enum InputTarget {
    CheckCommand(crate::slt_ui::Source),
    CheckFinding(String),
    ContextQuery(crate::context_store::Scope),
    ContextRevisit(Box<crate::context_store::PackRow>),
    Search,
    LoadedSearch,
    LaunchTitle,
    LaunchMessage,
    Batch,
    Question,
    Favorite,
    Command,
}
#[derive(Clone, Debug)]
pub(super) enum Intent {
    CheckSend(String),
    Context(context::Action),
    Workspace(workspace::WorkspaceAction),
    Command(String),
    Lane(String),
    Environment(Option<usize>),
    Type(String),
    Theme(String),
    Layout(LayoutMode),
    Open(usize, String),
    NewEnvironment(usize, NewPurpose),
    Project(usize, Value, NewPurpose),
    ProjectsMore(usize, u64, NewPurpose),
    Edit(InputTarget),
    LaunchCreate,
    LaunchWorkspace,
    LaunchModels,
    PinDestination,
    ResetDestination,
    Model(usize),
    Option(String),
    OptionValue(String, Value),
    ModelApply,
    ModelSave,
    ClearOverride,
    Request(Value),
    SavedAnswer(Value),
    Approval(Value, String),
    QuestionOption(Value),
    QuestionNext,
    QuestionBack,
    QuestionSubmit,
    Confirm {
        profile: usize,
        op: String,
        fields: Value,
    },
    ReviewRecovery(Value, bool),
    Recover(Value, bool),
    Forget(Value, bool),
    Inspect(Value),
    ActivityEarlier(String),
    Recall(String),
    NativeScope(String),
    Close,
}
#[derive(Clone, Debug)]
pub(super) struct Choice {
    label: String,
    description: String,
    checked: bool,
    intent: Intent,
}
impl Choice {
    fn new(label: impl Into<String>, description: impl Into<String>, intent: Intent) -> Self {
        Self {
            label: label.into(),
            description: description.into(),
            checked: false,
            intent,
        }
    }
}
#[derive(Clone, Debug)]
pub(super) enum DialogKind {
    Menu,
    Palette,
    Text(InputTarget),
    Detail,
}
#[derive(Clone, Debug)]
pub(super) struct Dialog {
    title: String,
    description: String,
    detail: String,
    choices: Vec<Choice>,
    selected: usize,
    scroll: usize,
    kind: DialogKind,
    footer: String,
}
#[derive(Clone, Debug)]
pub(super) struct Launch {
    profile: usize,
    project: Value,
    defaults: Value,
    title: String,
    message: String,
    selection: Value,
    workspace: Option<String>,
    batch: Option<(String, String)>,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum ModelOptionOrigin {
    Direct,
    Configuration,
}
#[derive(Clone, Debug)]
pub(super) struct ModelEdit {
    environment_id: String,
    scope: ModelScope,
    // Only the open native option layer owns this return fact; selection stays here.
    option_origin: Option<ModelOptionOrigin>,
    catalog: Value,
    original: Value,
    selection: Value,
}
#[derive(Clone, Debug)]
pub(super) struct RequestEdit {
    request: Value,
    index: usize,
    answers: serde_json::Map<String, Value>,
}
#[derive(Clone, Debug)]
pub(super) struct AnswerDelivery {
    source: crate::attention::Source,
    operation: String,
    phase: AnswerPhase,
}
#[derive(Clone, Copy, Debug)]
pub(super) enum AnswerPhase {
    Submitting,
    Uncertain,
    Acknowledged,
    Rejected,
}
impl AnswerPhase {
    fn as_str(self) -> &'static str {
        match self {
            Self::Submitting => "submitting",
            Self::Uncertain => "uncertain",
            Self::Acknowledged => "acknowledged",
            Self::Rejected => "rejected",
        }
    }
}
pub(super) struct Navigation {
    token: u64,
    historical: bool,
    base: u64,
    acknowledged: bool,
    saw_sync: bool,
}
pub(super) struct Active {
    profile: usize,
    thread: String,
    generation: u64,
    navigating: bool,
    navigation: Option<Navigation>,
    connection: Option<Connection>,
    hello: Value,
    projection: Projection,
    requests: Vec<Value>,
    status: String,
    summary: Option<Thread>,
}
#[derive(Default)]
pub(super) struct Host {
    generation: u64,
    page: ThreadPage,
    error: String,
    loading: bool,
    queued: Option<(bool, bool, String, bool)>,
    request: Option<(bool, bool, String, bool)>,
    windowed: bool,
    refresh_loaded: bool,
    refresh_cursor: u64,
    request_cursor: u64,
    projects: Vec<Value>,
    projects_more: bool,
}
pub(super) type DraftCache = Arc<Mutex<HashMap<String, (u64, String)>>>;

pub struct App {
    pub(super) args: Args,
    pub(super) environments: Vec<Environment>,
    pub(super) state: Arc<State>,
    pub(super) hosts: Vec<Host>,
    pub(super) threads: Vec<Thread>,
    pub(super) visible: Vec<(usize, usize)>,
    pub(super) selected: usize,
    pub(super) active: Option<Active>,
    pub(super) generation: u64,
    pub(super) serial: u64,
    pub(super) navigation_serial: u64,
    pub(super) mode: Mode,
    pub(super) focus: Focus,
    pub(super) layout: LayoutMode,
    pub(super) theme: String,
    pub(super) filter: String,
    pub(super) search_query: String,
    pub(super) filtering: bool,
    pub(super) server_search: bool,
    pub(super) lane: String,
    pub(super) completed_collapsed: bool,
    pub(super) completed_count: usize,
    pub(super) opened_completion: Option<(usize, String, Option<String>)>,
    pub(super) kind: String,
    pub(super) scope: Option<usize>,
    pub(super) expanded: HashSet<String>,
    pub(super) composer: TextArea<'static>,
    pub(super) modal_editor: TextArea<'static>,
    pub(super) dialog: Option<Dialog>,
    pub(super) launch: Option<Launch>,
    pub(super) model_edit: Option<ModelEdit>,
    pub(super) request_edit: Option<RequestEdit>,
    pub(super) answer_deliveries: VecDeque<AnswerDelivery>,
    pub(super) creation_profile: Option<usize>,
    pub(super) delivery: String,
    pub(super) notice: String,
    pub(super) dirty: bool,
    pub(super) quit: bool,
    pub(super) mouse: bool,
    pub(super) transcript_scroll: usize,
    pub(super) follow: bool,
    pub(super) renderer: ui::Renderer,
    pub(super) hits: ui::HitMap,
    pub(super) tx: Sender<Background>,
    pub(super) rx: Receiver<Background>,
    pub(super) pool: Pool,
    pub(super) metadata_pool: Pool,
    pub(super) storage_pool: Pool,
    pub(super) draft_cache: DraftCache,
    pub(super) draft_version: u64,
    pub(super) save_at: Option<Instant>,
    pub(super) form_save_at: Option<Instant>,
    pub(super) metadata_refresh_at: Instant,
    pub(super) draft_error: String,
    pub(super) in_flight: Arc<Mutex<HashSet<(String, bool)>>>,
    pub(super) recents: VecDeque<(usize, String)>,
    pub(super) tabs: Vec<(usize, String)>,
    pub(super) tab_titles: HashMap<(usize, String), String>,
    pub(super) loaded_contents: VecDeque<crate::slt_ui::ContentItem>,
    pub(super) context_capture_pending: HashMap<String, crate::context_store::RecordInput>,
    pub(super) context_capture_known: HashSet<String>,
    pub(super) context_capture_inflight: bool,
    pub(super) context_capture_status: String,
    pub(super) context_io: context::ContextIo,
    context_selection: Option<context::Selection>,
    check_io: HashSet<String>,
    #[cfg(test)]
    pub(super) context_test_barrier: Option<context::IoBarrier>,
    pub(super) return_positions: HashMap<String, (usize, bool, TextArea<'static>)>,
    pub(super) read_connection: Option<Connection>,
    pub(super) read_cancel: Option<(Connection, String)>,
    pub(super) clipboard_request: Option<String>,
    pub(super) pending_external_edit: bool,
    pub(super) pending_attention: Option<(usize, String, String, String)>,
    pub(super) receipt: Option<Value>,
    pub(super) native_scope: String,
    pub(super) workspace: workspace::WorkspaceState,
}

impl App {
    pub fn new(args: Args, environments: Vec<Environment>, state: Arc<State>) -> Result<Self> {
        workspace::migrate_launch_forms(&state, &environments)?;
        let snapshot = state.snapshot();
        let preferences = &snapshot["sltUi"];
        let mode = Mode::Dev;
        let theme = if args.theme.is_empty() {
            let t = string(preferences, "theme");
            if t.is_empty() { "violet".into() } else { t }
        } else {
            args.theme.clone()
        };
        let creation_profile = if args.env.is_empty() {
            None
        } else {
            Some(
                environments
                    .iter()
                    .position(|e| e.id == args.env)
                    .context("unknown configured environment")?,
            )
        };
        let (tx, rx) = bounded(128);
        let hosts = (0..environments.len()).map(|_| Host::default()).collect();
        let workspace = workspace::WorkspaceState::new(&snapshot, &environments);
        let host_workers = environments.len().clamp(1, 8);
        Ok(Self {
            mouse: !args.no_mouse && preferences["mouse"].as_bool().unwrap_or(true),
            args,
            environments,
            state,
            hosts,
            threads: Vec::new(),
            visible: Vec::new(),
            selected: 0,
            active: None,
            generation: 0,
            serial: 0,
            navigation_serial: 0,
            mode,
            focus: Focus::Navigator,
            layout: match preferences["layout"].as_str() {
                Some("focus") => LayoutMode::Focus,
                Some("wide") => LayoutMode::Wide,
                _ => LayoutMode::Split,
            },
            theme: theme.clone(),
            filter: String::new(),
            search_query: String::new(),
            filtering: false,
            server_search: false,
            lane: "Open".into(),
            completed_collapsed: false,
            completed_count: 0,
            opened_completion: None,
            kind: "all".into(),
            scope: None,
            expanded: HashSet::new(),
            composer: ui::editor("", &theme, "Write a message…"),
            modal_editor: ui::editor("", &theme, "Type here…"),
            dialog: None,
            launch: None,
            model_edit: None,
            request_edit: None,
            answer_deliveries: VecDeque::new(),
            creation_profile,
            delivery: "auto".into(),
            notice: String::new(),
            dirty: true,
            quit: false,
            transcript_scroll: 0,
            follow: true,
            renderer: ui::Renderer::default(),
            hits: ui::HitMap::default(),
            tx,
            rx,
            pool: Pool::new(2, 16),
            metadata_pool: Pool::new(host_workers, 16),
            storage_pool: Pool::new(1, 4),
            draft_cache: Arc::new(Mutex::new(HashMap::new())),
            draft_version: 0,
            save_at: None,
            form_save_at: None,
            metadata_refresh_at: Instant::now() + METADATA_REFRESH_INTERVAL,
            draft_error: String::new(),
            in_flight: Arc::new(Mutex::new(HashSet::new())),
            recents: VecDeque::new(),
            tabs: Vec::new(),
            tab_titles: HashMap::new(),
            loaded_contents: VecDeque::new(),
            context_capture_pending: HashMap::new(),
            context_capture_known: HashSet::new(),
            context_capture_inflight: false,
            context_capture_status: String::new(),
            context_io: context::ContextIo::default(),
            context_selection: None,
            check_io: HashSet::new(),
            #[cfg(test)]
            context_test_barrier: None,
            return_positions: HashMap::new(),
            read_connection: None,
            read_cancel: None,
            clipboard_request: None,
            pending_external_edit: false,
            pending_attention: None,
            receipt: None,
            native_scope: "project".into(),
            workspace,
        })
    }

    pub fn run(self) -> Result<()> {
        self.run_slt()
    }

    fn external_editor(&mut self) -> Result<()> {
        use std::io::Read;
        let active = self.active.as_ref().context("Open a conversation first")?;
        let profile = active.profile;
        let thread = active.thread.clone();
        let env = self.environments[profile].environment_id.clone();
        let original = self.text();
        let mut file = tempfile::Builder::new()
            .prefix("t3oxide-draft-")
            .suffix(".md")
            .tempfile_in(
                self.state
                    .path()
                    .parent()
                    .unwrap_or_else(|| std::path::Path::new(".")),
            )?;
        file.write_all(original.as_bytes())?;
        file.as_file().sync_all()?;
        let command = std::env::var("VISUAL")
            .or_else(|_| std::env::var("EDITOR"))
            .unwrap_or_else(|_| "vi".into());
        let argv = shell_words::split(&command).context("VISUAL / EDITOR has invalid quoting")?;
        ensure!(!argv.is_empty(), "VISUAL / EDITOR is empty");
        let mut child = std::process::Command::new(&argv[0])
            .args(&argv[1..])
            .arg(file.path())
            .spawn()
            .context("Could not start external editor")?;
        let status = loop {
            if let Some(status) = child.try_wait()? {
                break status;
            }
            if let Ok(event) = self.rx.recv_timeout(Duration::from_millis(50))
                && let Err(e) = self.handle(event)
            {
                self.error(e);
            }
        };
        if !status.success() {
            let (_, path) = file.keep()?;
            bail!(
                "Editor exited unsuccessfully. Original draft preserved; edited file: {}",
                path.display()
            )
        }
        let mut bytes = Vec::new();
        if let Err(error) = std::fs::File::open(file.path())
            .and_then(|f| f.take(64 * 1024 + 1).read_to_end(&mut bytes))
        {
            let (_, path) = file.keep()?;
            bail!(
                "Edited draft could not be read ({error}); preserved at {}",
                path.display()
            )
        }
        if bytes.len() > 64 * 1024 {
            let (_, path) = file.keep()?;
            bail!(
                "Edited draft exceeds 64 KiB; preserved at {}",
                path.display()
            )
        }
        let text = match String::from_utf8(bytes) {
            Ok(text) => text,
            Err(error) => {
                let (_, path) = file.keep()?;
                bail!(
                    "Edited draft is not UTF-8 ({error}); preserved at {}",
                    path.display()
                )
            }
        };
        if text.lines().count() > 9999 {
            let (_, path) = file.keep()?;
            bail!(
                "Edited draft exceeds the supported line limit; preserved at {}",
                path.display()
            )
        }
        if text != original {
            if self
                .active
                .as_ref()
                .is_some_and(|a| a.profile == profile && a.thread == thread)
            {
                self.replace_text(text);
                self.flush_draft()?;
            } else {
                self.state.set_draft(&env, &thread, &text)?;
            }
        }
        self.notice = "External edit saved to its original conversation. Nothing sent.".into();
        Ok(())
    }

    pub(super) fn error(&mut self, error: impl std::fmt::Display) {
        self.notice = plain(&format!("{error}"));
        self.dirty = true;
    }
    pub(super) fn selected_thread(&self) -> Option<&Thread> {
        self.visible
            .get(self.selected)
            .and_then(|(i, _)| self.threads.get(*i))
    }
    pub(super) fn active_env(&self) -> Result<&Environment> {
        self.active
            .as_ref()
            .map(|a| &self.environments[a.profile])
            .context("Open a conversation first")
    }
    pub(super) fn metadata(&self) -> Value {
        self.active
            .as_ref()
            .and_then(|a| a.projection.snapshot())
            .map(|s| s.metadata.clone())
            .unwrap_or(Value::Null)
    }
    pub(super) fn draft_key(&self) -> Option<String> {
        self.active.as_ref().map(|a| {
            format!(
                "{}/{}",
                self.environments[a.profile].environment_id, a.thread
            )
        })
    }
    pub(super) fn text(&self) -> String {
        self.composer.lines().join("\n")
    }
    pub(super) fn replace_text(&mut self, text: String) {
        self.composer = ui::editor(&text, &self.theme, "Write a message…");
        self.composer.move_cursor(CursorMove::Bottom);
        self.composer.move_cursor(CursorMove::End);
        self.changed_draft();
    }
    pub(super) fn changed_draft(&mut self) {
        if let Some(key) = self.draft_key() {
            let mut cache = self.draft_cache.lock().unwrap();
            self.draft_version = self
                .draft_version
                .max(cache.get(&key).map(|(v, _)| *v).unwrap_or(0))
                .saturating_add(1);
            cache.insert(key, (self.draft_version, self.text()));
            self.save_at = Some(Instant::now() + Duration::from_millis(200));
        }
        self.dirty = true;
    }
    pub(super) fn flush_draft(&mut self) -> Result<()> {
        self.save_at = None;
        if let Some(active) = &self.active {
            let env = &self.environments[active.profile].environment_id;
            let mut cache = self.draft_cache.lock().unwrap();
            let text = self.text();
            // An ACK can reach the worker before the UI consumes its event.
            // Its newer empty cache marker must win over the still drawn editor.
            if cache
                .get(&format!("{env}/{}", active.thread))
                .is_some_and(|(version, text)| *version > self.draft_version && text.is_empty())
            {
                self.composer = ui::editor("", &self.theme, "Write a message…");
                self.draft_version = cache[&format!("{env}/{}", active.thread)].0;
                self.draft_error.clear();
                return Ok(());
            }
            cache.insert(
                format!("{env}/{}", active.thread),
                (self.draft_version, text.clone()),
            );
            self.state
                .set_draft(env, &active.thread, &text)
                .context("Draft could not be saved; keep this client open")?;
        }
        self.draft_error.clear();
        Ok(())
    }
    fn schedule_save(&mut self) -> Result<()> {
        self.save_at = None;
        let Some(active) = &self.active else {
            return Ok(());
        };
        let env = self.environments[active.profile].environment_id.clone();
        let thread = active.thread.clone();
        let key = format!("{env}/{thread}");
        let version = self.draft_version;
        let state = self.state.clone();
        let cache = self.draft_cache.clone();
        let tx = self.tx.clone();
        self.storage_pool.submit(move || {
            let result = (|| -> Result<()> {
                let cache = cache.lock().unwrap();
                if let Some((current, text)) = cache.get(&key)
                    && *current == version
                {
                    state.set_draft(&env, &thread, text)?;
                }
                Ok(())
            })();
            let _ = tx.send(Background::Saved {
                key,
                version,
                result: result.map_err(|e| e.to_string()),
            });
        })
    }
    pub(super) fn save_ui(&self) -> Result<()> {
        self.save_reading_position()?;
        self.save_review_position()?;
        let theme = self.theme.clone();
        self.state.update(|v| {
            if !v["sltUi"].is_object() {
                v["sltUi"] = json!({});
            }
            v["sltUi"]["theme"] = json!(theme);
            v["sltUi"]["mouse"] = json!(self.mouse);
            v["sltUi"]["layout"] = json!(match self.layout {
                LayoutMode::Split => "split", LayoutMode::Focus => "focus", LayoutMode::Wide => "wide",
            });
            v["sltUi"]["tabPlacement"] = json!("internal");
            v["sltUi"]["tabs"] = json!(self.tabs.iter().map(|(profile, thread)| {
                let env = &self.environments[*profile];
                json!({"profileId":env.id,"environmentId":env.environment_id,"threadId":thread,"title":self.tab_titles.get(&(*profile,thread.clone()))})
            }).collect::<Vec<_>>());
            Ok(())
        })
    }

    pub(super) fn active_summary(&self) -> Option<&Thread> {
        let active = self.active.as_ref()?;
        active.summary.as_ref().or_else(|| {
            self.threads
                .iter()
                .find(|row| row.profile == active.profile && row.id == active.thread)
        })
    }

    pub(super) fn set_lane(&mut self, lane: String) -> Result<()> {
        self.lane = lane;
        self.rebuild();
        let pins = self.lane == "Pins";
        for profile in 0..self.environments.len() {
            if self.scope.is_none_or(|scope| scope == profile)
                && (self.hosts[profile].page.navigation_pins_only != pins
                    || pins
                    || self.hosts[profile].loading)
            {
                let (search, query) = self.metadata_query(profile);
                self.refresh_profile(profile, false, search, query, false)?;
            }
        }
        Ok(())
    }

    fn metadata_query(&self, profile: usize) -> (bool, String) {
        let host = &self.hosts[profile];
        host.queued
            .as_ref()
            .or(host.request.as_ref())
            .map(|(_, search, query, _)| (*search, query.clone()))
            .unwrap_or_else(|| (host.page.search, host.page.query.clone()))
    }

    fn refresh_metadata_if_due(&mut self, now: Instant) {
        if now < self.metadata_refresh_at {
            return;
        }
        self.metadata_refresh_at = now + METADATA_REFRESH_INTERVAL;
        for profile in 0..self.environments.len() {
            // Explicit pending reads own their original scope. A timer never
            // replaces or queues behind them, even while browsing another host.
            if self.hosts[profile].loading {
                continue;
            }
            let (search, query) = self.metadata_query(profile);
            let cursor = self.hosts[profile].refresh_cursor;
            if let Err(error) =
                self.refresh_profile_at(profile, false, search, query, true, Some(cursor))
            {
                self.hosts[profile].error = error.to_string();
                self.dirty = true;
            }
        }
    }

    pub(super) fn refresh(&mut self, append: bool, search: bool) -> Result<()> {
        if search {
            ensure!(
                (2..=200).contains(&self.filter.chars().count()) && self.filter.len() <= 800,
                "Server search needs 2–200 characters"
            );
            self.lane = "All".into();
        }
        if !append {
            self.server_search = search;
        }
        for profile in 0..self.environments.len() {
            if self.scope.is_none_or(|p| p == profile) {
                let native_search = if append {
                    self.hosts[profile].page.search
                } else {
                    search
                };
                let query = if append {
                    self.hosts[profile].page.query.clone()
                } else {
                    self.filter.clone()
                };
                self.refresh_profile(profile, append, native_search, query, false)?;
            }
        }
        self.notice = if search {
            "Searching names and native recent messages…".into()
        } else {
            "Refreshing native conversation metadata…".into()
        };
        self.dirty = true;
        Ok(())
    }
    fn refresh_profile(
        &mut self,
        profile: usize,
        append: bool,
        search: bool,
        query: String,
        refresh_loaded: bool,
    ) -> Result<()> {
        self.refresh_profile_at(profile, append, search, query, refresh_loaded, None)
    }

    fn refresh_profile_at(
        &mut self,
        profile: usize,
        append: bool,
        search: bool,
        query: String,
        refresh_loaded: bool,
        cursor_override: Option<u64>,
    ) -> Result<()> {
        let refresh_loaded = refresh_loaded && self.lane != "Pins";
        let include_thread = self
            .active
            .as_ref()
            .filter(|active| active.profile == profile)
            .map(|active| active.thread.clone());
        let host = &mut self.hosts[profile];
        if host.loading {
            if !refresh_loaded || host.queued.is_none() {
                host.queued = Some((append, search, query, refresh_loaded));
            }
            return Ok(());
        }
        if append {
            ensure!(
                host.page.search == search && host.page.query == query,
                "Native search changed; request More again for its current page"
            );
        }
        let cursor = if append {
            let Some(cursor) = host.page.next_cursor else {
                return Ok(());
            };
            cursor
        } else {
            cursor_override.filter(|_| refresh_loaded).unwrap_or(0)
        };
        host.generation += 1;
        let generation = host.generation;
        host.loading = true;
        host.refresh_loaded = refresh_loaded;
        host.request_cursor = cursor;
        let env = self.environments[profile].clone();
        let tx = self.tx.clone();
        host.request = Some((append, search, query.clone(), refresh_loaded));
        let navigation_pins_only = self.lane == "Pins";
        let task=self.metadata_pool.submit(move||{
            let result=(||->Result<Value>{let c=Connection::start(env)?;let result=(||{
                let hello=c.hello()?;
                let mut value=c.request("list",json!({"query":if search{query.clone()}else{String::new()},"serverSearch":search,"limit":200,"cursor":cursor,"lifecycle":"all","grouped":false,"includeChildren":true,"navigationPinsOnly":navigation_pins_only,"includeThreadId":include_thread}),None)?;
                value["_clientInclusiveChildren"]=json!(flag(&hello["features"],"inclusiveThreadList"));value["_clientQuery"]=json!(if search{query}else{String::new()});Ok(value)
            })();c.close();result})();
            let _=tx.send(Background::Page{profile,generation,append,result:result.map_err(|e|e.to_string())});
        });
        if let Err(e) = task {
            host.loading = false;
            host.error = e.to_string();
            return Err(e);
        }
        Ok(())
    }

    pub(super) fn rebuild(&mut self) {
        let previous = self.selected_thread().map(Thread::key);
        self.threads = self
            .hosts
            .iter()
            .flat_map(|h| h.page.threads.iter().cloned())
            .collect();
        // Server matches may come from messages, not titles: do not erase them
        // by applying the loaded-title filter a second time.
        let search = self.server_search;
        let mut expanded = self
            .workspace_expansion()
            .unwrap_or_else(|| self.expanded.clone());
        if search {
            // Each returned row is an explicit search match, including children
            // whose parent also matched. Keep browsing expansion independent.
            expanded.extend(self.threads.iter().map(Thread::key));
        }
        let unread = self
            .threads
            .iter()
            .filter(|thread| {
                self.state.is_thread_unread(
                    &self.environments[thread.profile].environment_id,
                    &thread.id,
                    thread.activity_generation().as_deref(),
                )
            })
            .map(Thread::key)
            .collect();
        self.visible = domain::visible_threads_with_unread(
            &self.threads,
            if search { "" } else { &self.filter },
            &self.lane,
            &self.kind,
            self.scope,
            &expanded,
            &unread,
        );
        self.workspace_order();
        self.selected = previous
            .and_then(|key| {
                self.visible
                    .iter()
                    .position(|(i, _)| self.threads[*i].key() == key)
            })
            .unwrap_or(self.selected)
            .min(self.visible.len().saturating_sub(1));
        self.dirty = true;
    }
    pub(super) fn open(&mut self, profile: usize, thread: String) -> Result<()> {
        self.preserve_workspace_position()?;
        ensure!(
            profile < self.environments.len() && domain::valid_id(&thread),
            "Invalid conversation destination"
        );
        self.preserve_open_form()?;
        self.flush_draft()?;
        if !self
            .tabs
            .iter()
            .any(|(p, id)| *p == profile && id == &thread)
        {
            self.tabs.push((profile, thread.clone()));
        }
        self.opened_completion = match self
            .threads
            .iter()
            .find(|row| row.profile == profile && row.id == thread)
        {
            Some(row) => row
                .activity_generation()
                .map(|generation| (profile, thread.clone(), Some(generation)))
                .or_else(|| Some((profile, thread.clone(), None))),
            None => Some((profile, thread.clone(), None)),
        };
        if let Some(old) = self.active.take() {
            let key = format!("{}\0{}", old.profile, old.thread);
            self.return_positions.insert(
                key,
                (self.transcript_scroll, self.follow, self.composer.clone()),
            );
            if let Some(c) = old.connection {
                c.close();
            }
        }
        while self.return_positions.len() > 64 {
            if let Some(k) = self.return_positions.keys().next().cloned() {
                self.return_positions.remove(&k);
            }
        }
        self.cancel_modal_read();
        self.generation += 1;
        let generation = self.generation;
        let env = self.environments[profile].clone();
        let (saved, version) = {
            let mut cache = self.draft_cache.lock().unwrap();
            let key = format!("{}/{}", env.environment_id, thread);
            let saved = self.state.draft(&env.environment_id, &thread);
            let version = cache
                .get(&key)
                .filter(|(_, text)| text == &saved)
                .map(|(v, _)| *v)
                .unwrap_or_else(|| {
                    self.draft_version
                        .max(cache.get(&key).map(|(v, _)| *v).unwrap_or(0))
                        .saturating_add(1)
                });
            cache.insert(key, (version, saved.clone()));
            (saved, version)
        };
        let key = format!("{profile}\0{thread}");
        let restored = self.return_positions.remove(&key);
        self.composer = ui::editor(&saved, &self.theme, "Write a message…");
        self.transcript_scroll = 0;
        self.follow = true;
        if let Some((scroll, follow, editor)) = restored {
            self.transcript_scroll = scroll;
            self.follow = follow;
            if editor.lines().join("\n") == saved {
                self.composer = editor;
            }
        }
        self.draft_version = version;
        let summary = self
            .threads
            .iter()
            .find(|row| row.profile == profile && row.id == thread)
            .cloned();
        self.active = Some(Active {
            summary,
            profile,
            thread: thread.clone(),
            generation,
            navigating: true,
            navigation: Some(Navigation {
                token: 0,
                historical: false,
                base: 0,
                acknowledged: false,
                saw_sync: false,
            }),
            connection: None,
            hello: Value::Null,
            projection: Projection::default(),
            requests: Vec::new(),
            status: "Connecting".into(),
        });
        self.receipt = None;
        self.dialog = None;
        self.request_edit = None;
        self.focus = Focus::Composer;
        self.dirty = true;
        self.recents.retain(|(p, t)| *p != profile || t != &thread);
        self.recents.push_front((profile, thread));
        self.recents.truncate(32);
        // Track a connecting helper immediately so switching sources closes
        // its handshake even when another foreground read is still retiring.
        let connection = Connection::start(env)?;
        self.active.as_mut().unwrap().connection = Some(connection.clone());
        self.save_ui()?;
        let tx = self.tx.clone();
        self.pool.submit(move || {
            let result = match connection.hello() {
                Ok(hello) => Ok((connection, hello)),
                Err(error) => {
                    connection.close();
                    Err(error.to_string())
                }
            };
            if let Err(crossbeam_channel::SendError(Background::Opened {
                connection: Ok((c, _)),
                ..
            })) = tx.send(Background::Opened {
                generation,
                connection: result,
            }) {
                c.close();
            }
        })
    }

    fn handle(&mut self, event: Background) -> Result<()> {
        match event {
            Background::Check { attempt, result } => self.check_done(&attempt, result)?,
            Background::Context(event) => self.handle_context_event(event)?,
            Background::Saved {
                key,
                version,
                result,
            } => {
                if self.draft_key().as_deref() == Some(&key) && version == self.draft_version {
                    match result {
                        Ok(()) => self.draft_error.clear(),
                        Err(e) => {
                            self.draft_error = e.clone();
                            self.error(e);
                        }
                    }
                }
            }
            Background::Page {
                profile,
                generation,
                append,
                result,
            } => {
                let protected: HashSet<_> = self
                    .selected_thread()
                    .filter(|row| row.profile == profile)
                    .map(|row| row.id.clone())
                    .into_iter()
                    .chain(
                        self.active
                            .as_ref()
                            .filter(|active| active.profile == profile)
                            .map(|active| active.thread.clone()),
                    )
                    .collect();
                let host = &mut self.hosts[profile];
                if generation != host.generation {
                    return Ok(());
                }
                host.loading = false;
                host.request = None;
                match result.and_then(|v| {
                    let inventory = (|| -> Result<_> {
                        let Some(projects) = v.get("projects") else {
                            return Ok((None, true));
                        };
                        let projects = projects
                            .as_array()
                            .context("Invalid native project inventory")?;
                        ensure!(
                            projects.len() <= 200
                                && serde_json::to_vec(projects)?.len() <= 50 * 1024,
                            "Native project inventory exceeds its bounds"
                        );
                        let more = v["projectsMore"]
                            .as_bool()
                            .context("Missing project inventory completeness")?;
                        let mut ids = HashSet::new();
                        for project in projects {
                            let id = string(project, "id");
                            ensure!(
                                project.is_object()
                                    && domain::valid_id(&id)
                                    && ids.insert(id)
                                    && project["title"]
                                        .as_str()
                                        .is_some_and(|title| title.len() <= 512),
                                "Invalid or duplicate native project identity"
                            );
                        }
                        Ok((Some(projects.clone()), more))
                    })()
                    .map_err(|error| error.to_string())?;
                    let source_id = string(&v, "sourceThreadId");
                    let source = v
                        .get("sourceThread")
                        .filter(|value| !value.is_null())
                        .map(|row| Thread::parse(profile, row.clone()))
                        .transpose()
                        .map_err(|error| error.to_string())?;
                    if source.as_ref().is_some_and(|source| source.id != source_id) {
                        return Err("Attached metadata changed its source".to_owned());
                    }
                    ThreadPage::parse(profile, v)
                        .map(|page| (page, inventory, source_id, source))
                        .map_err(|e| e.to_string())
                }) {
                    Ok((mut page, (projects, projects_more), source_id, source)) => {
                        if let Some(active) = self
                            .active
                            .as_mut()
                            .filter(|active| active.profile == profile)
                            && let Some(row) = source
                                .as_ref()
                                .filter(|row| row.id == active.thread)
                                .or_else(|| page.threads.iter().find(|row| row.id == active.thread))
                        {
                            active.summary = Some(row.clone());
                        }
                        if source.is_none()
                            && let Some(active) = self.active.as_mut().filter(|active| {
                                active.profile == profile && active.thread == source_id
                            })
                            && let Some(row) = &mut active.summary
                        {
                            row.raw["_metadataStale"] = json!(true);
                        }
                        for row in &mut host.page.threads {
                            if let Some(source) =
                                source.as_ref().filter(|source| source.id == row.id)
                            {
                                *row = source.clone();
                            } else if source.is_none() && row.id == source_id {
                                row.raw["_metadataStale"] = json!(true);
                            }
                        }
                        let events: Vec<_> = page
                            .threads
                            .iter()
                            .filter_map(|row| {
                                row.activity_generation()
                                    .map(|generation| (row.id.clone(), generation))
                            })
                            .collect();
                        self.state.observe_thread_activity(
                            &self.environments[profile].environment_id,
                            &events,
                        )?;
                        if let Some(projects) = projects {
                            host.projects = projects;
                        }
                        host.projects_more = projects_more;
                        host.error.clear();
                        let same_scope = host.page.search == page.search
                            && host.page.query == page.query
                            && host.page.navigation_pins_only == page.navigation_pins_only;
                        host.refresh_cursor = if host.refresh_loaded && same_scope {
                            page.next_cursor
                                .filter(|cursor| *cursor < host.page.threads.len() as u64)
                                .unwrap_or(0)
                        } else {
                            0
                        };
                        if host.refresh_loaded && same_scope && !host.page.threads.is_empty() {
                            if host.request_cursor == 0 && !page.more && page.next_cursor.is_none()
                            {
                                let present: HashSet<_> =
                                    page.threads.iter().map(|row| row.id.as_str()).collect();
                                host.page
                                    .threads
                                    .retain(|row| present.contains(row.id.as_str()));
                            }
                            for fresh in page.threads {
                                if let Some(current) =
                                    host.page.threads.iter_mut().find(|row| row.id == fresh.id)
                                {
                                    *current = fresh
                                } else {
                                    host.page.threads.push(fresh)
                                }
                            }
                            host.page.observed_at = page.observed_at;
                            host.page.inclusive_children = page.inclusive_children;
                            if host.page.threads.len() > 1600 {
                                host.page.threads.sort_by(|left, right| {
                                    protected
                                        .contains(&right.id)
                                        .cmp(&protected.contains(&left.id))
                                        .then_with(|| right.activity_at().cmp(left.activity_at()))
                                        .then_with(|| left.id.cmp(&right.id))
                                });
                                host.page.threads.truncate(1600);
                                host.windowed = true;
                            }
                        } else if append {
                            for fresh in page.threads.drain(..) {
                                if let Some(current) =
                                    host.page.threads.iter_mut().find(|row| row.id == fresh.id)
                                {
                                    *current = fresh;
                                } else {
                                    host.page.threads.push(fresh);
                                }
                            }
                            host.page.more = page.more;
                            host.page.next_cursor = page.next_cursor;
                            host.page.observed_at = page.observed_at;
                            host.page.inclusive_children = page.inclusive_children;
                            if host.page.threads.len() > 1600 {
                                host.page.threads.drain(..host.page.threads.len() - 1600);
                                host.windowed = true;
                            }
                        } else {
                            host.page = page;
                            host.windowed = false;
                        }
                    }
                    Err(error) => host.error = error,
                }
                let queued = host.queued.take();
                self.rebuild();
                self.observe_completion()?;
                self.notice = self.host_status();
                if let Some((append, search, query, refresh_loaded)) = queued {
                    self.refresh_profile(profile, append, search, query, refresh_loaded)?;
                }
            }
            Background::Opened {
                generation,
                connection,
            } => {
                if self
                    .active
                    .as_ref()
                    .is_none_or(|a| a.generation != generation)
                {
                    if let Ok((c, _)) = connection {
                        c.close();
                    }
                    return Ok(());
                }
                let (c, hello) = connection.map_err(|e| anyhow!(e))?;
                let events = c.events();
                let tx = self.tx.clone();
                let thread = self.active.as_ref().unwrap().thread.clone();
                let active = self.active.as_mut().unwrap();
                active.hello = hello;
                active.connection = Some(c.clone());
                active.status = "Synchronizing".into();
                thread::spawn(move || {
                    while let Ok(packet) = events.recv() {
                        if tx.send(Background::Packet { generation, packet }).is_err() {
                            break;
                        }
                    }
                    let _ = tx.send(Background::Disconnected { generation });
                });
                self.read_on(
                    c,
                    "watch",
                    json!({"threadId":thread}),
                    ReadKind::Watch { navigation: 0 },
                    false,
                )?;
            }
            Background::Packet { generation, packet } => {
                if self
                    .active
                    .as_ref()
                    .is_some_and(|active| active.generation == generation)
                    && self.request_edit.is_some()
                {
                    self.flush_form()?;
                    self.save_question_draft()?;
                }
                let Some(a) = self.active.as_mut().filter(|a| a.generation == generation) else {
                    return Ok(());
                };
                let before = lifecycle_key(&a.projection);
                let profile = a.profile;
                let mut retired_request_view = false;
                match packet.event.as_str() {
                    "view" => {
                        a.projection.apply_snapshot(packet.data, &a.thread)?;
                    }
                    "delta" => {
                        if let Err(error) = a.projection.apply_delta(packet.data) {
                            a.status = "Resynchronizing".into();
                            self.error(error);
                            self.navigate(false)?;
                            return Ok(());
                        }
                    }
                    "status" => {
                        a.status = string(&packet.data, "state");
                        if packet.data.get("writesEnabled").is_some() {
                            a.hello["writesEnabled"] = packet.data["writesEnabled"].clone();
                        }
                        if let Some(features) = packet.data.get("features") {
                            a.hello["features"] = features.clone();
                        }
                    }
                    _ => {}
                }
                if packet.event != "status"
                    && let Some(s) = a.projection.snapshot()
                {
                    if flag(&s.metadata, "live") && !flag(&s.metadata, "historical") {
                        a.requests = array(&s.metadata, "requests").to_vec();
                        a.status = "Live".into();
                    } else {
                        a.status = string(&s.metadata, "state");
                    }
                    if let Some(edit) = &self.request_edit {
                        let id = string(&edit.request, "id");
                        let rev = string(&edit.request, "revision");
                        if !a
                            .requests
                            .iter()
                            .any(|r| string(r, "id") == id && string(r, "revision") == rev)
                        {
                            self.request_edit = None;
                            self.dialog = None;
                            retired_request_view = true;
                            self.notice =
                                "That request changed; open Requests to review the current offer."
                                    .into();
                        }
                    }
                }
                let after = lifecycle_key(&a.projection);
                if retired_request_view {
                    self.invalidate_context_view();
                }
                let pin_changed = before
                    .as_ref()
                    .zip(after.as_ref())
                    .is_some_and(|(before, after)| before[3] != after[3]);
                if pin_changed {
                    let (search, query) = self.metadata_query(profile);
                    self.refresh_profile(profile, false, search, query, true)?;
                } else if after.is_some()
                    && after != before
                    && !self.server_search
                    && !self.hosts[profile].page.search
                {
                    self.refresh_profile(profile, false, false, String::new(), true)?;
                }
                self.observe_loaded_content()?;
                self.observe_navigation();
                self.observe_completion()?;
                self.restore_reading_position_if_ready();
                self.observe_receipt();
                self.dirty = true;
            }
            Background::Disconnected { generation } => {
                if let Some(a) = self.active.as_mut().filter(|a| a.generation == generation) {
                    a.status = "Disconnected · use Reconnect".into();
                    a.hello["writesEnabled"] = json!(false);
                    self.dirty = true;
                }
            }
            Background::Read {
                generation,
                serial,
                kind,
                result,
            } => {
                if generation != self.generation || serial != 0 && serial != self.serial {
                    return Ok(());
                }
                match result {
                    Ok(value) => self.read_result(kind, value)?,
                    Err(error) => {
                        self.notice = error;
                        self.dirty = true;
                    }
                }
            }
            Background::Mutation {
                context,
                id,
                op,
                fields,
                result,
            } => match result {
                Ok(value) => self.mutation_done(context, &id, &op, &fields, value)?,
                Err(error) => {
                    if op == "answer" {
                        let phase = if self
                            .state
                            .pending(&self.environments[context.profile].environment_id, false)
                            .is_some_and(|record| record["id"] == id)
                        {
                            AnswerPhase::Uncertain
                        } else {
                            AnswerPhase::Rejected
                        };
                        self.remember_answer_delivery(context.profile, &id, &fields, phase);
                    }
                    if let Some(receipt) = &mut self.receipt
                        && receipt["id"] == id
                    {
                        receipt["phase"] = json!("Unconfirmed — review recovery");
                    }
                    self.error(error);
                }
            },
        }
        Ok(())
    }

    pub(super) fn read_on(
        &mut self,
        c: Connection,
        op: &str,
        fields: Value,
        kind: ReadKind,
        modal: bool,
    ) -> Result<()> {
        let op = op.to_owned();
        let tx = self.tx.clone();
        let generation = self.generation;
        if modal {
            self.cancel_modal_read();
        }
        let serial = if modal { self.serial } else { 0 };
        let id = wire::new_id();
        if modal {
            self.read_cancel = Some((c.clone(), id.clone()));
        }
        self.pool.submit(move || {
            let result = c.request(&op, fields, Some(&id)).map_err(|e| e.to_string());
            let _ = tx.send(Background::Read {
                generation,
                serial,
                kind,
                result,
            });
        })
    }
    pub(super) fn read(&mut self, op: &str, fields: Value, kind: ReadKind) -> Result<()> {
        let c = self
            .active
            .as_ref()
            .and_then(|a| a.connection.clone())
            .context("Connection is not ready")?;
        self.read_on(c, op, fields, kind, true)
    }
    pub(super) fn source_read(
        &mut self,
        profile: usize,
        op: &str,
        fields: Value,
        kind: ReadKind,
    ) -> Result<()> {
        self.cancel_modal_read();
        let serial = self.serial;
        let generation = self.generation;
        let op = op.to_owned();
        let tx = self.tx.clone();
        let c = Connection::start(self.environments[profile].clone())?;
        self.read_connection = Some(c.clone());
        self.pool.submit(move || {
            let result: Result<Value> = {
                let value = (|| {
                    c.hello()?;
                    c.request(&op, fields, None).map_err(Into::into)
                })();
                c.close();
                value
            };
            let _ = tx.send(Background::Read {
                generation,
                serial,
                kind,
                result: result.map_err(|e| e.to_string()),
            });
        })
    }
    pub(super) fn cancel_modal_read(&mut self) {
        self.invalidate_context_view();
        self.serial = self.serial.wrapping_add(1);
        if let Some((c, id)) = self.read_cancel.take() {
            c.cancel_read(&id);
        }
        if let Some(c) = self.read_connection.take() {
            c.close();
        }
    }
    pub(super) fn navigate(&mut self, earlier: bool) -> Result<()> {
        let a = self.active.as_ref().context("Open a conversation first")?;
        let c = a.connection.clone().context("Connection is not ready")?;
        self.navigation_serial = self.navigation_serial.wrapping_add(1);
        let token = self.navigation_serial;
        let (op, fields, kind) = if earlier {
            let cursor = a
                .projection
                .snapshot()
                .map(|s| s.metadata["historyCursor"].clone())
                .filter(|v| v.is_string())
                .context("No earlier page is available")?;
            (
                "history",
                json!({"cursor":cursor}),
                ReadKind::History { navigation: token },
            )
        } else {
            (
                "watch",
                json!({"threadId":a.thread}),
                ReadKind::Watch { navigation: token },
            )
        };
        // History/watch acknowledgements belong to navigation, independently of
        // temporary menus. Opening Commands must not cancel a live attachment.
        self.read_on(c, op, fields, kind, false)?;
        if let Some(active) = &mut self.active {
            active.navigating = true;
            active.status = "Synchronizing".into();
            active.navigation = Some(Navigation {
                token,
                historical: earlier,
                base: active
                    .projection
                    .snapshot()
                    .map(|s| s.revision)
                    .unwrap_or(0),
                acknowledged: false,
                saw_sync: false,
            });
        }
        self.follow = !earlier;
        self.transcript_scroll = 0;
        self.dirty = true;
        Ok(())
    }
    pub(super) fn navigation_ack(&mut self, token: u64) -> bool {
        let Some(nav) = self
            .active
            .as_mut()
            .and_then(|active| active.navigation.as_mut())
            .filter(|nav| nav.token == token)
        else {
            return false;
        };
        nav.acknowledged = true;
        self.observe_navigation();
        if let Err(error) = self.observe_completion() {
            self.error(error);
        }
        self.restore_reading_position_if_ready();
        true
    }
    fn observe_navigation(&mut self) {
        let Some(active) = &mut self.active else {
            return;
        };
        let Some(nav) = &mut active.navigation else {
            return;
        };
        let Some(snapshot) = active.projection.snapshot() else {
            return;
        };
        if snapshot.revision > nav.base {
            nav.saw_sync |= snapshot.metadata["state"] == "synchronizing";
            let arrived = if nav.historical {
                flag(&snapshot.metadata, "historical")
            } else {
                nav.saw_sync && snapshot.is_live()
            };
            if nav.acknowledged && arrived {
                active.navigating = false;
                active.navigation = None;
            }
        }
    }

    pub(super) fn mutate(
        &mut self,
        profile: usize,
        op: &str,
        mut fields: Value,
        retry: Option<String>,
    ) -> Result<()> {
        ensure!(
            profile < self.environments.len() && wire::is_mutation(op),
            "Unknown mutation source or operation"
        );
        ensure!(fields.is_object(), "Invalid mutation payload");
        let check_follow_up = fields.get("t3oxideCheckFollowUp").is_some();
        if check_follow_up {
            ensure!(
                op == "send",
                "Check follow-up must use the native send lane"
            );
            self.validate_check_send(profile, &fields, retry.is_some())?;
        }
        let env = self.environments[profile].clone();
        let id = retry.clone().unwrap_or_else(wire::new_id);
        let stop = if retry.is_some() {
            let candidates: Vec<_> = self
                .state
                .pending_records()
                .into_iter()
                .filter(|p| p.environment_id == env.environment_id && p.record["id"] == id)
                .collect();
            ensure!(
                candidates.len() == 1 && candidates[0].record["op"] == op,
                "Original saved action changed or is unavailable; recovery cannot invent an operation"
            );
            candidates[0].stop // Includes a legacy Stop recorded in the ordinary lane.
        } else {
            op == "stop"
        };
        let existing = self.state.pending(&env.environment_id, stop);
        ensure!(
            existing.is_none()
                || retry
                    .as_ref()
                    .is_some_and(|id| existing.as_ref().unwrap()["id"] == *id),
            "This environment has an unresolved action. Open Recover before sending another."
        );
        if !stop && retry.is_none() {
            self.flush_draft()?;
        }
        if retry.is_none() {
            ensure!(
                domain::valid_id(&string(&fields, "threadId")),
                "Mutation needs an exact thread identity"
            );
            if op == "thread-action" {
                let target = self
                    .active_summary()
                    .filter(|thread| {
                        thread.profile == profile && thread.raw["id"] == fields["threadId"]
                    })
                    .or_else(|| {
                        self.threads.iter().find(|thread| {
                            thread.profile == profile && thread.raw["id"] == fields["threadId"]
                        })
                    })
                    .context(
                        "Refresh the displayed conversation before changing its disposition",
                    )?;
                ensure!(
                    !flag(&target.raw, "_metadataStale")
                        && target.inbox()["nativeRevision"] == fields["expectedRevision"]
                        && !string(target.inbox(), "nativeRevision").is_empty(),
                    "Native disposition revision changed; refresh before trying again"
                );
                ensure!(
                    self.hosts[profile].error.is_empty(),
                    "Host metadata unavailable; refresh before changing disposition"
                );
            } else if op != "launch-project" {
                let active = self
                    .active
                    .as_ref()
                    .context("Open the target conversation first")?;
                ensure!(
                    active.profile == profile && fields["threadId"] == active.thread,
                    "Mutation target changed"
                );
                let connection = active
                    .connection
                    .as_ref()
                    .context("Host connection is not ready")?;
                ensure!(
                    flag(&active.hello, "writesEnabled")
                        && connection.is_verified()
                        && !connection.is_closed(),
                    "Host writes unavailable"
                );
                let snapshot = active
                    .projection
                    .snapshot()
                    .context("Wait for a verified conversation snapshot")?;
                let metadata = &snapshot.metadata;
                if !matches!(op, "stop" | "answer") {
                    ensure!(
                        !active.navigating
                            && !active.projection.needs_resnapshot
                            && snapshot.is_live()
                            && !matches!(
                                active.status.as_str(),
                                "Connecting"
                                    | "Synchronizing"
                                    | "Resynchronizing"
                                    | "synchronizing"
                                    | "disconnected"
                            ),
                        "Return to Latest before changing this conversation"
                    );
                }
                if op == "stop" {
                    ensure!(
                        domain::valid_id(&string(&fields, "runId"))
                            && fields["runId"] == metadata["activeRunId"],
                        "The reviewed active run changed; refresh before Stop"
                    );
                }
                if op == "answer" {
                    ensure!(
                        active
                            .requests
                            .iter()
                            .any(|r| r["id"] == fields["requestId"]
                                && r["revision"] == fields["revision"]
                                && flag(r, "supported")),
                        "Request changed; reopen its exact native offer"
                    );
                }
                if env.allowed_provider_instance_ids.is_some() {
                    match op {
                        "send" | "launch" | "select-model" => {
                            let selection = fields
                                .get("modelSelection")
                                .unwrap_or(&metadata["modelSelection"])
                                .clone();
                            crate::state::validate_selection(&selection)?;
                            env.check_provider_instance(&string(&selection, "instanceId"))?;
                            if fields.get("modelSelection").is_none()
                                && matches!(op, "send" | "launch")
                            {
                                let capability = if op == "send" {
                                    "messageModelOverride"
                                } else {
                                    "launchModelSelection"
                                };
                                ensure!(
                                    flag(&active.hello["features"], capability),
                                    "Provider policy requires exact native model selection support"
                                );
                                fields["modelSelection"] = selection;
                            }
                        }
                        "answer"
                            if !matches!(
                                fields["decision"].as_str(),
                                Some("decline" | "cancel")
                            ) =>
                        {
                            let selection = &metadata["runtimeDetails"]["runModelSelection"];
                            crate::state::validate_selection(selection).context("Provider policy requires a reported run selection before resuming this request")?;
                            env.check_provider_instance(&string(selection, "instanceId"))?;
                        }
                        _ => {}
                    }
                }
            } else if env.allowed_provider_instance_ids.is_some() {
                crate::state::validate_selection(&fields["modelSelection"])?;
                env.check_provider_instance(&string(&fields["modelSelection"], "instanceId"))?;
            }
        }
        let op = op.to_owned();
        let mut record = fields.clone();
        record["id"] = json!(id);
        record["op"] = json!(op);
        record["environmentId"] = json!(env.environment_id);
        if retry.is_some() {
            ensure!(
                existing.as_ref() == Some(&record),
                "Recovery must retain the exact original payload and lane"
            );
        }
        let lane = (env.environment_id.clone(), stop);
        {
            let mut lanes = self.in_flight.lock().unwrap();
            ensure!(
                lanes.insert(lane.clone()),
                "An action in this lane is awaiting acknowledgement"
            );
        }
        let submitted_version = self.draft_version;
        let saved = if op == "send" && retry.is_none() && !check_follow_up {
            // Hold the autosave cache lock across the durable handoff so an old
            // save cannot resurrect submitted text as the next draft.
            let key = format!("{}/{}", env.environment_id, string(&fields, "threadId"));
            let mut cache = self.draft_cache.lock().unwrap();
            let saved = self
                .state
                .save_send_detaching_draft(&env.environment_id, record);
            if saved.is_ok() {
                self.draft_version = self.draft_version.saturating_add(1);
                cache.insert(key, (self.draft_version, String::new()));
                self.composer = ui::editor("", &self.theme, "Write a message…");
                self.save_at = None;
            }
            saved
        } else {
            self.state.save_pending(&env.environment_id, stop, record)
        };
        if let Err(error) = saved {
            self.in_flight.lock().unwrap().remove(&lane);
            return Err(error);
        }
        if op == "answer" {
            self.remember_answer_delivery(profile, &id, &fields, AnswerPhase::Submitting);
        }
        let active = self.active.as_ref().filter(|a| a.profile == profile);
        let reusable = active
            .and_then(|a| a.connection.as_ref())
            .filter(|c| c.is_verified() && !c.is_closed())
            .cloned();
        let verified_hello = active.map(|a| a.hello.clone()).unwrap_or(Value::Null);
        let tx = self.tx.clone();
        let state = self.state.clone();
        let cache = self.draft_cache.clone();
        let lanes = self.in_flight.clone();
        if op == "send"
            && self
                .active
                .as_ref()
                .is_some_and(|a| a.profile == profile && fields["threadId"] == a.thread)
        {
            self.receipt = Some(
                json!({"id":id,"threadId":fields["threadId"],"messageId":format!("{id}:message"),"phase":"Sending"}),
            );
        }
        self.notice = format!("{op} awaiting native acknowledgement");
        self.dirty = true;
        let context = MutationContext {
            generation: self.generation,
            profile,
            draft_version: if op == "send" && retry.is_none() && !check_follow_up {
                Some(submitted_version)
            } else {
                None
            },
        };
        thread::spawn(move || {
            let mut attempted = false;
            let result = (|| -> Result<Value> {
                let owned = reusable.is_none();
                let connection = if let Some(c) = reusable {
                    c
                } else {
                    Connection::start(env.clone())?
                };
                let result = (|| -> Result<Value> {
                    let hello = if owned {
                        connection.hello()?
                    } else {
                        verified_hello
                    };
                    ensure!(
                        flag(&hello, "writesEnabled"),
                        "Host writes unavailable; original operation remains saved"
                    );
                    if retry.is_none() {
                        let capability = match op.as_str() {
                            "launch-project" | "thread-action" => Some("nativeWorkflows"),
                            "select-model" => Some("modelSelection"),
                            "send" if fields.get("modelSelection").is_some() => {
                                Some("messageModelOverride")
                            }
                            "launch" if fields.get("modelSelection").is_some() => {
                                Some("launchModelSelection")
                            }
                            _ => None,
                        };
                        if let Some(capability) = capability {
                            ensure!(
                                flag(&hello["features"], capability),
                                "This host does not advertise {capability}"
                            );
                        }
                        if op == "thread-action"
                            && matches!(
                                fields["action"].as_str(),
                                Some("navigation-pin" | "navigation-unpin")
                            )
                        {
                            ensure!(
                                flag(&hello["features"], "navigationPins"),
                                "Navigation Pin is unavailable: this host has no verified thread.tag.set contract"
                            );
                        }
                    }
                    attempted = true;
                    match connection.request(&op, fields.clone(), Some(&id)) {
                        Err(error) => {
                            if retry.is_none() && !error.uncertain {
                                state.reject_pending(&env.environment_id, stop, &id)?;
                            }
                            Err(error.into())
                        }
                        Ok(value) => {
                            let mut identity = fields.clone();
                            identity["id"] = json!(id);
                            identity["environmentId"] = json!(env.environment_id);
                            validate_ack(&op, &identity, &value)?;
                            let thread = string(&fields, "threadId");
                            let message =
                                if matches!(op.as_str(), "send" | "launch" | "launch-project") {
                                    string(&fields, "message")
                                } else {
                                    String::new()
                                };
                            let mut cached = cache.lock().unwrap();
                            let key = format!("{}/{}", env.environment_id, thread);
                            let preserved = if op == "send" {
                                cached
                                    .get(&key)
                                    .filter(|(version, _)| context.draft_version != Some(*version))
                                    .map(|(_, text)| text.clone())
                                    .or_else(|| {
                                        retry
                                            .is_some()
                                            .then(|| state.draft(&env.environment_id, &thread))
                                    })
                            } else {
                                None
                            };
                            // Receipt, recall, override retirement and newer human
                            // input commit together: no crash window between files.
                            state.acknowledge_preserving_draft(
                                &env.environment_id,
                                stop,
                                &id,
                                &thread,
                                &message,
                                preserved.as_deref(),
                            )?;
                            if op == "send"
                                && let Some((version, text)) = cached.get_mut(&key)
                                && context.draft_version == Some(*version)
                                && text == &message
                                && !message.is_empty()
                            {
                                text.clear();
                                *version = version.saturating_add(1);
                            }
                            Ok(value)
                        }
                    }
                })();
                if owned {
                    connection.close();
                }
                result
            })();
            let result = if result.is_err() && !attempted && retry.is_none() {
                match state.reject_pending(&env.environment_id, stop, &id) {
                    Ok(()) => result,
                    Err(e) => {
                        Err(e.context("Pre-dispatch failure; recovery state could not be retired"))
                    }
                }
            } else {
                result
            };
            lanes.lock().unwrap().remove(&lane);
            let result = result.map_err(|e| {
                format!(
                    "{e:#}{}",
                    if state.pending(&env.environment_id, stop).is_some() {
                        " · original operation saved; Recover for explicit reconciliation"
                    } else {
                        ""
                    }
                )
            });
            let _ = tx.send(Background::Mutation {
                context,
                id,
                op,
                fields,
                result,
            });
        });
        Ok(())
    }

    fn mutation_done(
        &mut self,
        context: MutationContext,
        id: &str,
        op: &str,
        fields: &Value,
        value: Value,
    ) -> Result<()> {
        let MutationContext {
            generation,
            profile,
            draft_version,
        } = context;
        if op == "answer" {
            let mut identity = fields.clone();
            identity["id"] = json!(id);
            identity["environmentId"] = json!(self.environments[profile].environment_id);
            validate_ack(op, &identity, &value)?;
            self.remember_answer_delivery(profile, id, fields, AnswerPhase::Acknowledged);
        }
        if op == "send"
            && self
                .active
                .as_ref()
                .is_some_and(|a| a.profile == profile && fields["threadId"] == a.thread)
        {
            // Only the submitted edit generation may be cleared. A new draft
            // with identical bytes is still new human input, not this receipt.
            if draft_version == Some(self.draft_version) && self.text() == string(fields, "message")
            {
                self.composer = ui::editor("", &self.theme, "Write a message…");
                self.changed_draft();
            }
            if let Some(receipt) = &mut self.receipt
                && receipt["id"] == id
            {
                receipt["phase"] = json!("Accepted by T3");
                receipt["sequence"] = value["sequence"].clone();
            }
        }
        self.notice = if op == "stop" {
            "Stop accepted · native queue held".into()
        } else {
            format!("{op} accepted by T3")
        };
        if matches!(op, "launch" | "launch-project") {
            let thread = string(&value, "threadId");
            let current = self.launch.as_ref().is_none_or(|l| {
                l.profile == profile
                    && l.project["id"] == fields["projectId"]
                    && l.title == string(fields, "title")
                    && l.message == string(fields, "message")
                    && l.selection == fields["modelSelection"]
                    && l.defaults["revision"] == fields["defaultsRevision"]
                    && l.workspace.as_deref() == fields.get("workspaceMode").and_then(Value::as_str)
            });
            if current && generation == self.generation && self.dialog.is_none() {
                self.launch = None;
                self.open(profile, thread)?;
            } else {
                self.notice = format!(
                    "Conversation created on {}: {}",
                    self.environments[profile].label, thread
                );
            }
        }
        if op == "thread-action" {
            self.apply_action_metadata(profile, fields, &value);
        } else if matches!(op, "launch" | "launch-project") {
            self.refresh(false, false)?;
        }
        if op == "select-model"
            && self
                .active
                .as_ref()
                .is_some_and(|a| a.profile == profile && fields["threadId"] == a.thread)
        {
            self.navigate(false)?;
        }
        self.observe_receipt();
        self.dirty = true;
        Ok(())
    }
    fn apply_action_metadata(&mut self, profile: usize, fields: &Value, value: &Value) {
        let metadata = &value["metadata"];
        let thread = string(fields, "threadId");
        let fresh = (|| -> Result<Thread> {
            ensure!(
                metadata["status"] == "fresh"
                    && metadata["environmentId"] == self.environments[profile].environment_id
                    && metadata["threadId"] == thread,
                "Affected metadata is stale or belongs to another source"
            );
            let row = Thread::parse(profile, metadata["row"].clone())?;
            ensure!(
                row.id == thread && !string(row.inbox(), "nativeRevision").is_empty(),
                "Affected metadata has no exact native revision"
            );
            Ok(row)
        })()
        .ok();
        let host = &mut self.hosts[profile];
        // A page begun before this snapshot cannot put old disposition back.
        // Preserve a user's pending query and rerun that read only.
        let pending = if host.loading {
            host.generation += 1;
            host.loading = false;
            host.queued.take().or_else(|| host.request.take())
        } else {
            None
        };
        for row in &mut host.page.threads {
            if row.id == thread {
                if let Some(fresh) = &fresh {
                    *row = fresh.clone();
                } else {
                    row.raw["_metadataStale"] = json!(true);
                }
            }
        }
        if let Some(active) = self
            .active
            .as_mut()
            .filter(|active| active.profile == profile && active.thread == thread)
        {
            if let Some(fresh) = &fresh {
                active.summary = Some(fresh.clone());
            } else if let Some(row) = &mut active.summary {
                row.raw["_metadataStale"] = json!(true);
            }
        }
        if fresh.is_none() {
            self.notice = "Action accepted by T3 · source metadata stale; Refresh before another disposition action".into();
        }
        self.rebuild();
        if let Some((append, search, query, refresh_loaded)) = pending
            && let Err(error) = self.refresh_profile(profile, append, search, query, refresh_loaded)
        {
            self.hosts[profile].error = error.to_string();
        }
    }

    fn observe_receipt(&mut self) {
        let Some(receipt) = &mut self.receipt else {
            return;
        };
        let Some(a) = &self.active else { return };
        let Some(s) = a.projection.snapshot() else {
            return;
        };
        if receipt["threadId"] != a.thread || !flag(&s.metadata, "live") {
            return;
        }
        if s.metadata["runtimeDetails"]["runMessageId"] == receipt["messageId"]
            && receipt["messageId"].is_string()
        {
            receipt["phase"] = json!(match s.metadata["runtimeDetails"]["status"]
                .as_str()
                .unwrap_or("")
            {
                "completed" => "Response completed",
                "failed" => "Run failed",
                "interrupted" | "cancelled" => "Run interrupted",
                "running" | "starting" | "preparing" => "Responding",
                "waiting" => "Waiting for input",
                _ => "Native run observed",
            });
        } else if s
            .items
            .iter()
            .any(|row| row.message_id.as_deref() == receipt["messageId"].as_str())
            && receipt["phase"] == "Accepted by T3"
        {
            receipt["phase"] = json!("Message observed · awaiting native run");
        }
    }

    fn host_status(&self) -> String {
        self.hosts
            .iter()
            .enumerate()
            .map(|(i, h)| {
                format!(
                    "{}: {}",
                    self.environments[i].label,
                    if h.loading {
                        "loading".into()
                    } else if !h.error.is_empty() {
                        format!("unavailable ({})", h.error)
                    } else {
                        format!(
                            "{}{}{}{}{}",
                            h.page.threads.len(),
                            if h.page.more { "+" } else { "" },
                            if h.windowed {
                                " (loaded window; Refresh starts at newest)"
                            } else {
                                ""
                            },
                            if !h.page.inclusive_children {
                                " · older companion: use server search for children"
                            } else {
                                ""
                            },
                            if h.projects_more {
                                " · project inventory incomplete"
                            } else {
                                ""
                            }
                        )
                    }
                )
            })
            .collect::<Vec<_>>()
            .join(" · ")
    }
    fn modal(&self) -> Option<ui::Modal> {
        use std::hash::{Hash, Hasher};
        fn option_value<'a>(selection: &'a Value, id: &str) -> Option<&'a Value> {
            array(selection, "options")
                .iter()
                .find(|option| option["id"] == id)
                .map(|option| &option["value"])
        }
        self.dialog.as_ref().map(|d| {
            let edit = self.model_edit.as_ref();
            let selected_model = edit.and_then(|edit| {
                array(&edit.catalog, "models")
                    .iter()
                    .find(|model| model["slug"] == edit.selection["model"])
            });
            let items: Vec<_> = d
                .choices
                .iter()
                .map(|choice| {
                    use ui::ModalItemRole as Role;
                    let (role, applied) = match &choice.intent {
                        Intent::Model(index) => edit
                            .and_then(|edit| {
                                array(&edit.catalog, "models").get(*index).map(|model| {
                                    (
                                        Role::Model {
                                            native_id: string(model, "slug"),
                                        },
                                        model["slug"] == edit.original["model"],
                                    )
                                })
                            })
                            .unwrap_or_default(),
                        Intent::Option(id) => edit
                            .zip(selected_model)
                            .and_then(|(edit, model)| {
                                array(model, "options")
                                    .iter()
                                    .find(|option| option["id"] == *id)
                                    .map(|option| {
                                        let value = option_value(&edit.selection, id);
                                        (
                                            Role::ModelOption {
                                                id: id.clone(),
                                                label: string(option, "label"),
                                                value: value
                                                    .map(|value| {
                                                        value
                                                            .as_str()
                                                            .map(str::to_owned)
                                                            .unwrap_or_else(|| value.to_string())
                                                    })
                                                    .unwrap_or_else(|| "Native default".into()),
                                            },
                                            edit.selection["model"] == edit.original["model"]
                                                && value == option_value(&edit.original, id),
                                        )
                                    })
                            })
                            .unwrap_or_default(),
                        Intent::OptionValue(id, value) => (
                            Role::ModelOptionValue { id: id.clone() },
                            edit.is_some_and(|edit| {
                                edit.selection["model"] == edit.original["model"]
                                    && option_value(&edit.original, id)
                                        .map_or(value.is_null(), |original| original == value)
                            }),
                        ),
                        Intent::ModelApply => (Role::ModelApply, false),
                        Intent::ModelSave => (Role::ModelSave, false),
                        Intent::ClearOverride => (Role::ModelClear, false),
                        _ => (Role::Choice, false),
                    };
                    ui::ModalItem {
                        label: choice.label.clone(),
                        description: choice.description.clone(),
                        checked: choice.checked,
                        action: matches!(
                            choice.intent,
                            Intent::QuestionNext | Intent::QuestionBack | Intent::QuestionSubmit
                        ),
                        role,
                        applied,
                    }
                })
                .collect();
            let model = if edit.is_some() && matches!(d.kind, DialogKind::Menu) {
                use ui::ModalItemRole as Role;
                if items.iter().any(|item| item.role == Role::ModelApply)
                    && items.iter().any(|item| item.role == Role::ModelSave)
                    && items.iter().all(|item| {
                        matches!(item.role, Role::Model { .. } | Role::ModelOption { .. }
                            | Role::ModelApply | Role::ModelSave | Role::ModelClear)
                    })
                {
                    Some(ui::ModelDialog::Configuration)
                } else if let Some(ui::ModalItem {
                    role: Role::ModelOptionValue { id }, ..
                }) = items.first()
                    && items.iter().all(|item| {
                        matches!(&item.role, Role::ModelOptionValue { id: other } if other == id)
                    })
                {
                    Some(ui::ModelDialog::OptionValues { id: id.clone() })
                } else {
                    None
                }
            } else {
                None
            };
            let mut identity = std::collections::hash_map::DefaultHasher::new();
            self.generation.hash(&mut identity);
            if let Some(active) = &self.active {
                let env = &self.environments[active.profile];
                (
                    active.profile,
                    &env.id,
                    &env.environment_id,
                    &active.thread,
                    active.generation,
                )
                    .hash(&mut identity);
            }
            model.hash(&mut identity);
            for (choice, item) in d.choices.iter().zip(&items) {
                format!("{:?}", choice.intent).hash(&mut identity);
                item.role.hash(&mut identity);
                item.checked.hash(&mut identity);
                item.applied.hash(&mut identity);
            }
            ui::Modal {
                title: d.title.clone(),
                description: d.description.clone(),
                items,
                selected: d.selected,
                scroll: d.scroll,
                detail: d.detail.clone(),
                editor: matches!(d.kind, DialogKind::Text(_) | DialogKind::Palette),
                footer: d.footer.clone(),
                action_stamp: identity.finish(),
                model,
            }
        })
    }
    fn view(&self) -> ui::View {
        let metadata = self.metadata();
        let env = self.active.as_ref().map(|a| &self.environments[a.profile]);
        let title = if let Some(e) = env {
            format!("{} / {}", e.label, string(&metadata, "title"))
        } else {
            "Your T3 workspace".into()
        };
        let rows = self
            .visible
            .iter()
            .map(|(i, depth)| {
                let t = &self.threads[*i];
                ui::ThreadView {
                    key: t.key(),
                    title: t.title.clone(),
                    subtitle: format!(
                        "{} / {}{}",
                        self.environments[t.profile].label,
                        t.project,
                        if t.kind() == "fork" { " · fork" } else { "" }
                    ),
                    status: t.presentation_status().into(),
                    depth: *depth,
                    pinned: t.pinned(),
                    section: self.open_section(t).into(),
                }
            })
            .collect();
        let messages = self
            .active
            .as_ref()
            .and_then(|a| {
                a.projection.snapshot().map(|s| {
                    s.items
                        .iter()
                        .map(|r| ui::MessageView {
                            key: format!(
                                "{}\0{}\0{}\0{}",
                                a.profile, a.thread, a.generation, r.key
                            ),
                            role: match r.kind.as_str() {
                                "user_message" => "You",
                                "assistant_message" if r.source_thread_id == a.thread => {
                                    match s.metadata["lineage"]["relationshipToParent"].as_str() {
                                        Some("subagent") => "Subagent",
                                        Some("fork") => "Agent · fork",
                                        _ => "Agent",
                                    }
                                }
                                "assistant_message" => "Agent",
                                other => other,
                            }
                            .to_owned(),
                            text: r.text.clone(),
                            streaming: r.streaming,
                            truncated: r.truncated,
                        })
                        .collect()
                })
            })
            .unwrap_or_default();
        let requests = self.active.as_ref().map(|a| a.requests.len()).unwrap_or(0);
        let pending = self.state.pending_records().len();
        let status = if let Some(a) = &self.active {
            format!(
                "{} · {} · {} · {}",
                a.status,
                string(&metadata, "status"),
                string(&metadata, "model"),
                self.delivery
            )
        } else {
            self.host_status()
        };
        let notice = if !self.draft_error.is_empty() {
            format!("Draft not saved: {}", self.draft_error)
        } else if let Some(receipt) = &self.receipt {
            format!("{} · {}", string(receipt, "phase"), self.notice)
        } else {
            self.notice.clone()
        };
        let mut inspector = vec![
            (
                "Environment".into(),
                env.map(|e| e.label.clone()).unwrap_or_default(),
            ),
            ("Thread".into(), string(&metadata, "threadId")),
            ("Workspace".into(), string(&metadata, "workspace")),
            ("Model".into(), string(&metadata, "model")),
            ("Run".into(), string(&metadata, "status")),
            ("Delivery".into(), self.delivery.clone()),
            ("Requests".into(), requests.to_string()),
            ("Recovery".into(), pending.to_string()),
            ("Hosts".into(), self.host_status()),
        ];
        if !self.context_capture_status.is_empty() {
            inspector.push((
                "Context capture".into(),
                self.context_capture_status.clone(),
            ));
        }
        if flag(&metadata, "limited") {
            inspector.push((
                "History".into(),
                "Excerpt · Earlier and native source actions remain available".into(),
            ));
        }
        if let Some(selection) = metadata.get("modelSelection") {
            for o in array(selection, "options") {
                inspector.push((
                    string(o, "id"),
                    o["value"]
                        .as_str()
                        .map(str::to_owned)
                        .unwrap_or_else(|| o["value"].to_string()),
                ));
            }
        }
        if let Some(row) = self.active_summary() {
            if row.inbox()["workingKnown"] == false {
                inspector.push(("Execution evidence".into(), "Unknown".into()));
            }
            if row.inbox()["backgroundHolding"] == true {
                inspector.push(("Background work".into(), "Waiting".into()));
            }
            let native_status = string(row.inbox(), "nativeStatus");
            if !native_status.is_empty() && native_status != row.presentation_status() {
                inspector.push(("Native status".into(), native_status));
            }
            inspector.push((
                "Navigation pin".into(),
                if row.pinned() { "Pinned" } else { "Unpinned" }.into(),
            ));
            inspector.push(("Native shelf pin".into(), string(row.inbox(), "pinnedAt")));
            inspector.push((
                "Native shelf order".into(),
                string(row.inbox(), "pinOrderKey"),
            ));
            if flag(&row.raw, "_metadataStale") {
                inspector.push(("Source metadata".into(), "Stale · Refresh".into()));
            }
            inspector.push((
                "Subagents".into(),
                row.child_count()
                    .map(|count| count.to_string())
                    .unwrap_or_else(|| "Unavailable".into()),
            ));
        }
        inspector.push(("Browsing".into(), self.workspace_scope_label()));
        ui::View {
            title,
            mode: self.mode,
            focus: self.focus,
            layout: self.layout,
            theme: self.theme.clone(),
            filter: self.filter.clone(),
            lane: self.lane.clone(),
            rows,
            selected: self.selected,
            completed_count: self.completed_count,
            completed_collapsed: self.completed_collapsed,
            messages,
            transcript_scroll: self.transcript_scroll,
            follow: self.follow,
            status,
            hint: if self.filtering {
                "Filter loaded titles · Enter done · Ctrl+F search server · Esc clear".into()
            } else {
                match self.focus{Focus::Navigator=>"↑↓ select · Enter open · Ctrl+T settle / unsettle · c Completed · Alt+H help",Focus::Composer=>"Enter newline · Ctrl+] send · Ctrl+S stop · Ctrl+T settle · Alt+H help",Focus::Transcript=>"↑↓ scroll · [ earlier · ] latest · Ctrl+T settle · Alt+H help",Focus::Inspector=>"Ctrl+P commands · Ctrl+T settle · Alt+H help"}.into()
            },
            notice,
            inspector,
            requests,
            pending,
        }
    }
}

fn lifecycle_key(projection: &Projection) -> Option<Value> {
    let s = projection.snapshot().filter(|s| s.is_live())?;
    Some(json!([
        s.metadata["activeRunId"],
        s.metadata["status"],
        array(&s.metadata, "requests")
            .iter()
            .map(|r| json!([r["id"], r["revision"]]))
            .collect::<Vec<_>>(),
        s.metadata["navigationPinned"]
    ]))
}

pub(super) fn validate_ack(op: &str, fields: &Value, value: &Value) -> Result<()> {
    ensure!(
        value.is_object(),
        "Unrecognized acknowledgement; operation remains saved"
    );
    if matches!(op, "launch" | "launch-project") {
        let expected = if op == "launch-project" {
            string(fields, "newThreadId")
        } else {
            fields
                .get("newThreadId")
                .and_then(Value::as_str)
                .unwrap_or_else(|| fields["id"].as_str().unwrap_or(""))
                .to_owned()
        };
        ensure!(
            domain::valid_id(&expected) && value["threadId"] == expected,
            "Launch acknowledgement changed its exact thread identity; operation remains saved"
        );
    } else {
        ensure!(
            value["sequence"]
                .as_u64()
                .is_some_and(|v| v <= 9_007_199_254_740_991),
            "Unrecognized acknowledgement sequence; operation remains saved"
        );
    }
    if op == "answer" {
        let source = crate::attention::Source {
            environment_id: string(fields, "environmentId"),
            thread_id: string(fields, "threadId"),
            request_id: string(fields, "requestId"),
            revision: string(fields, "revision"),
        };
        ensure!(
            crate::attention::acknowledged(&source, &string(fields, "id"), value),
            "Answer acknowledgement changed source, revision or operation; original operation remains saved"
        );
    }
    if op == "thread-action" {
        ensure!(
            value["threadId"] == fields["threadId"] && value["action"] == fields["action"],
            "Native action acknowledgement changed its target"
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn acknowledgements_cannot_retarget() {
        assert!(
            validate_ack(
                "launch-project",
                &json!({"newThreadId":"a"}),
                &json!({"threadId":"b"})
            )
            .is_err()
        );
        assert!(
            validate_ack(
                "thread-action",
                &json!({"threadId":"a","action":"settle"}),
                &json!({"sequence":2,"threadId":"a","action":"unsettle"})
            )
            .is_err()
        );
        assert!(validate_ack("send", &json!({}), &json!({"sequence":-1})).is_err());
    }
}
