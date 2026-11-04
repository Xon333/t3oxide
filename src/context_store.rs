//! Explicit local context capture and bounded recall; native T3 retains all action authority.
use anyhow::{Context, Result, bail, ensure};
use fael_core::{Config, Cut, Filter, Log, Row};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};
use std::collections::HashSet;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::os::fd::AsRawFd;
use std::os::unix::fs::{DirBuilderExt, MetadataExt, OpenOptionsExt};
use std::path::{Path, PathBuf};

const MAX_SOURCE_BYTES: usize = 16 * 1024 * 1024;
const MAX_PACK_BYTES: usize = 384 * 1024;

/// Original native provenance, including the state observed when content was captured.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Source {
    pub environment_id: String,
    pub project_id: String,
    pub thread_id: String,
    pub item_id: Option<String>,
    pub request_id: Option<String>,
    pub revision: String,
    pub observed: Value,
}
impl Source {
    fn validate(&self) -> Result<()> {
        ensure!(
            [
                &self.environment_id,
                &self.project_id,
                &self.thread_id,
                &self.revision
            ]
            .into_iter()
            .all(|value| identity(value))
                && [&self.item_id, &self.request_id]
                    .into_iter()
                    .all(|value| value.as_deref().is_none_or(identity))
                && self.observed.is_object(),
            "Context requires exact native source identities, revision and observation metadata"
        );
        Ok(())
    }
}

/// Scope uses native identities; omitted project/thread fields widen only that dimension.
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Scope {
    pub environment_id: String,
    pub project_id: Option<String>,
    pub thread_id: Option<String>,
}
impl Scope {
    fn matches(&self, source: &Source) -> bool {
        self.environment_id == source.environment_id
            && self
                .project_id
                .as_ref()
                .is_none_or(|id| *id == source.project_id)
            && self
                .thread_id
                .as_ref()
                .is_none_or(|id| *id == source.thread_id)
    }
}

/// Context kinds describe recorded material and never create a native obligation.
#[derive(Clone, Copy, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum Kind {
    Decision,
    Evidence,
    Journal,
    OpenLoop,
}
impl Kind {
    fn fael(self) -> &'static str {
        match self {
            Self::Decision => "decision",
            Self::OpenLoop => "issue",
            Self::Evidence | Self::Journal => "note",
        }
    }
}

/// An explicit capture, with full source bytes separate from the compact fael row.
#[derive(Clone, Debug)]
pub struct RecordInput {
    pub source: Source,
    pub kind: Kind,
    pub text: String,
    pub full_source: Vec<u8>,
    pub supersedes: Option<String>,
    pub revisit: Option<String>,
}

/// Bounded recall parameters; history opts into closed and superseded local context.
#[derive(Clone, Debug)]
pub struct Query {
    pub scope: Scope,
    pub text: String,
    pub history: bool,
    pub offset: usize,
    pub limit: usize,
    pub byte_limit: usize,
}
impl Query {
    /// Project/thread callers choose scope explicitly; no machine-global search is implicit.
    pub fn new(scope: Scope) -> Self {
        Self {
            scope,
            text: String::new(),
            history: false,
            offset: 0,
            limit: 20,
            byte_limit: 16 * 1024,
        }
    }
}

/// Protected/native records are supplied by the client rather than inferred from context.
#[derive(Clone, Copy, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum NativeKind {
    Draft,
    PendingOperation,
    HumanQuestion,
}

/// Resume preserves original source and operation/request identity without executing anything.
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NativeRecord {
    pub source: Source,
    pub kind: NativeKind,
    pub identity: String,
    pub status: String,
    pub summary: String,
}

/// A compact row always points to its full captured source and content hash.
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PackRow {
    pub id: String,
    pub kind: Kind,
    pub text: String,
    pub source: Source,
    pub source_ref: String,
    pub source_sha256: String,
    pub closed: bool,
    pub superseded: bool,
    pub revisit: Option<String>,
}

/// Counts cover the whole query before paging; completeness qualifies damaged-store totals.
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Pack {
    pub id: String,
    pub scope: Scope,
    pub query: String,
    pub history: bool,
    pub offset: usize,
    pub eligible: usize,
    pub shown: usize,
    pub omitted: usize,
    pub native_eligible: usize,
    pub native_shown: usize,
    pub native_omitted: usize,
    pub complete: bool,
    pub truncated: bool,
    pub degradation: Vec<String>,
    pub rows: Vec<PackRow>,
    pub native_records: Vec<NativeRecord>,
    pub markdown: String,
}

/// Full captured bytes are returned only by explicit source lookup.
#[derive(Clone, Debug)]
pub struct CapturedSource {
    pub source: Source,
    pub sha256: String,
    pub bytes: Vec<u8>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct Provenance {
    source: Source,
    kind: Kind,
    source_ref: String,
    source_sha256: String,
}

/// A private, explicitly selected state-sibling directory; no upstream hooks are installed.
#[derive(Debug)]
pub struct Store {
    root: PathBuf,
    _lock: File,
}
impl Store {
    /// Open only the supplied private directory and retain its exclusive writer lock.
    pub fn open(root: &Path) -> Result<Self> {
        ensure!(
            root.is_absolute(),
            "Context directory must be an explicit absolute state-sibling path"
        );
        private_directory(root)?;
        let lock = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .mode(0o600)
            .custom_flags(libc::O_NOFOLLOW)
            .open(root.join(".lock"))?;
        private_metadata(&lock.metadata()?)?;
        ensure!(
            lock.metadata()?.is_file(),
            "Context lock must be a regular file"
        );
        // SAFETY: the descriptor is an owned open file; flock takes no pointer.
        ensure!(
            unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) } == 0,
            "Another client owns this context store"
        );
        private_directory(&root.join("log"))?;
        private_directory(&root.join("sources"))?;
        File::open(root)?.sync_all()?;
        Ok(Self {
            root: root.to_owned(),
            _lock: lock,
        })
    }

    /// Capture only explicitly selected source material and return its stable local row ID.
    pub fn record(&self, input: &RecordInput) -> Result<String> {
        self.check_roots()?;
        input.source.validate()?;
        ensure!(
            !input.full_source.is_empty() && input.full_source.len() <= MAX_SOURCE_BYTES,
            "Captured source must contain 1 byte–16 MiB of complete bytes; nothing was truncated"
        );
        let digest = hash(&input.full_source);
        let meta = Provenance {
            source: input.source.clone(),
            kind: input.kind,
            source_ref: format!("sources/{digest}.bin"),
            source_sha256: digest,
        };
        let anchor = hash(&serde_json::to_vec(&(
            &input.source.environment_id,
            &input.source.project_id,
            &input.source.thread_id,
        ))?);
        let mut row = Row::new(
            "t3oxide",
            input.kind.fael(),
            &input.text,
            vec![format!("t3:{anchor}")],
        );
        row.revisit = input.revisit.clone();
        row.supersedes = input.supersedes.clone();
        row.extra
            .insert("t3oxide".into(), serde_json::to_value(&meta)?);
        if let Some(id) = &row.supersedes {
            let log = self.load()?;
            let old = log
                .rows
                .iter()
                .find(|row| row.id == *id)
                .context("Exact context record to supersede not found")?;
            let old = provenance(old)?.source;
            ensure!(
                old.environment_id == input.source.environment_id
                    && old.project_id == input.source.project_id
                    && old.thread_id == input.source.thread_id,
                "Superseding context must retain its original native scope"
            );
            row.decision_source = Some("caller:flag".into());
        }
        fael_core::validate(&row, &Config::default()).map_err(anyhow::Error::msg)?;
        self.publish_source(&meta, &input.full_source)?;
        self.publish_row(&row)?;
        Ok(row.id)
    }

    /// Close only a local context row; no native request or operation is touched.
    pub fn close(&self, id: &str, source: &Source, reason: &str) -> Result<String> {
        let old = self.exact_row(id, source)?;
        let mut row = Row::close("t3oxide", id, reason);
        row.extra
            .insert("t3oxide".into(), old.extra["t3oxide"].clone());
        fael_core::validate_close(&row, &Config::default()).map_err(anyhow::Error::msg)?;
        self.publish_row(&row)?;
        Ok(row.id)
    }

    /// Record an explicit revisit event while retaining the original row and source.
    pub fn revisit(&self, id: &str, source: &Source, when: &str) -> Result<String> {
        ensure!(
            !when.trim().is_empty(),
            "A revisit date or condition is required"
        );
        let old = self.exact_row(id, source)?;
        let mut row = Row::bumped("t3oxide", id);
        row.revisit = Some(when.into());
        row.extra
            .insert("t3oxide".into(), old.extra["t3oxide"].clone());
        let log = self.load()?;
        ensure!(
            !fael_core::closed(&log).contains(id) && !fael_core::superseded(&log).contains(id),
            "Revisit applies only to open local context; history remains retained"
        );
        if let Some(last) = log
            .rows
            .iter()
            .filter(|event| event.bumps.as_deref() == Some(id))
            .map(|event| event.id.as_str())
            .max()
            .filter(|last| row.id.as_str() <= *last)
        {
            row.id = fael_core::ulid_at(fael_core::ulid_ms(last).unwrap_or(0) + 1);
        }
        fael_core::validate_bump(&row, &Config::default()).map_err(anyhow::Error::msg)?;
        self.publish_row(&row)?;
        Ok(row.id)
    }

    /// Ranked, scoped local recall using the upstream log/query engine.
    pub fn recall(&self, query: &Query) -> Result<Pack> {
        self.resume(query, &[])
    }

    /// Combine recorded context with actual protected/native metadata, without changing it.
    pub fn resume(&self, query: &Query, native: &[NativeRecord]) -> Result<Pack> {
        ensure!(
            identity(&query.scope.environment_id)
                && [&query.scope.project_id, &query.scope.thread_id]
                    .into_iter()
                    .all(|value| value.as_deref().is_none_or(identity)),
            "Recall needs an exact native scope"
        );
        ensure!(
            (1024..=MAX_PACK_BYTES).contains(&query.byte_limit)
                && query.limit > 0
                && query.limit <= 200,
            "Context packs need a 1–384 KiB byte limit and 1–200 rows"
        );
        let mut log = self.load()?;
        log.rows
            .retain(|row| provenance(row).is_ok_and(|meta| query.scope.matches(&meta.source)));
        log.closes
            .retain(|row| provenance(row).is_ok_and(|meta| query.scope.matches(&meta.source)));
        let log = fael_core::fold_bumps(log);
        let filter = Filter {
            text: (!query.text.trim().is_empty()).then(|| query.text.clone()),
            all: query.history,
            ..Filter::default()
        };
        let (mut page, total) = fael_core::page(
            fael_core::find(&log, &filter),
            Some(query.limit),
            query.offset,
        );
        let mut native_records = Vec::new();
        for record in native
            .iter()
            .filter(|record| query.scope.matches(&record.source))
        {
            record.source.validate()?;
            ensure!(
                identity(&record.identity)
                    && !record.status.is_empty()
                    && (!matches!(record.kind, NativeKind::HumanQuestion)
                        || record.source.request_id.is_some()),
                "Resume needs actual protected/native record identities"
            );
            native_records.push(record.clone());
        }
        let closed = fael_core::closed(&log);
        let superseded = fael_core::superseded(&log);
        let mut pack = Pack {
            id: String::new(),
            scope: query.scope.clone(),
            query: query.text.clone(),
            history: query.history,
            offset: query.offset,
            eligible: total,
            shown: 0,
            omitted: total,
            native_eligible: native_records.len(),
            native_shown: 0,
            native_omitted: 0,
            complete: log.warnings.is_empty(),
            truncated: false,
            degradation: log.warnings.clone(),
            rows: Vec::new(),
            native_records,
            markdown: String::new(),
        };
        loop {
            pack.rows = page
                .iter()
                .map(|row| {
                    let meta = provenance(row)?;
                    Ok(PackRow {
                        id: row.id.clone(),
                        kind: meta.kind,
                        text: row.text.clone(),
                        source: meta.source,
                        source_ref: meta.source_ref,
                        source_sha256: meta.source_sha256,
                        closed: closed.contains(row.id.as_str()),
                        superseded: superseded.contains(row.id.as_str()),
                        revisit: row.revisit.clone(),
                    })
                })
                .collect::<Result<_>>()?;
            pack.shown = pack.rows.len();
            pack.omitted = total - pack.shown;
            pack.native_shown = pack.native_records.len();
            pack.native_omitted = pack.native_eligible - pack.native_shown;
            pack.truncated = pack.omitted > 0
                || pack.native_omitted > 0
                || pack.rows.iter().any(|row| source_limited(&row.source))
                || pack
                    .native_records
                    .iter()
                    .any(|record| source_limited(&record.source));
            pack.markdown = fael_core::render_page(
                &log,
                &page,
                usize::MAX / 8,
                Cut {
                    total,
                    offset: query.offset,
                    next: &|offset| format!("More records · offset {offset}"),
                },
            );
            pack.id.clear();
            pack.id = hash(&serde_json::to_vec(&pack)?);
            if serde_json::to_vec(&pack)?.len() <= query.byte_limit {
                return Ok(pack);
            }
            if page.pop().is_some() {
                continue;
            }
            if pack.native_records.pop().is_some() {
                continue;
            }
            bail!(
                "Context metadata/degradation exceeds the byte limit; increase the pack limit (no evidence was discarded)"
            );
        }
    }

    /// Verify and return the complete captured source rather than its compact summary.
    pub fn source(&self, id: &str) -> Result<CapturedSource> {
        let log = self.load()?;
        let row = log
            .rows
            .iter()
            .find(|row| row.id == id)
            .context("Context record not found")?;
        let meta = provenance(row)?;
        let bytes = read_private(&self.root.join(&meta.source_ref), MAX_SOURCE_BYTES)?;
        ensure!(
            hash(&bytes) == meta.source_sha256,
            "Captured source hash mismatch; preserve the file for recovery"
        );
        Ok(CapturedSource {
            source: meta.source,
            sha256: meta.source_sha256,
            bytes,
        })
    }

    fn exact_row(&self, id: &str, source: &Source) -> Result<Row> {
        source.validate()?;
        let log = self.load()?;
        let row = log
            .rows
            .into_iter()
            .find(|row| row.id == id)
            .context("Exact context record not found")?;
        ensure!(
            provenance(&row)?.source == *source,
            "Context source/revision changed; prior record retained"
        );
        Ok(row)
    }

    fn publish_source(&self, meta: &Provenance, bytes: &[u8]) -> Result<()> {
        let path = self.root.join(&meta.source_ref);
        match fs::symlink_metadata(&path) {
            Ok(metadata) => {
                ensure!(
                    metadata.is_file() && !metadata.file_type().is_symlink(),
                    "Captured source must be a regular owned file"
                );
                ensure!(
                    read_private(&path, MAX_SOURCE_BYTES)? == bytes,
                    "Existing captured source hash conflicts; original retained"
                );
                return Ok(());
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {}
            Err(error) => return Err(error.into()),
        }
        publish(&path, bytes)
    }
    fn publish_row(&self, row: &Row) -> Result<()> {
        let suffix = if row.reference.is_some() {
            ".close.jsonl"
        } else {
            ".jsonl"
        };
        publish(
            &self.root.join("log").join(format!("{}{suffix}", row.id)),
            format!("{}\n", row.to_line()).as_bytes(),
        )
    }

    fn load(&self) -> Result<Log> {
        self.check_roots()?;
        let mut warnings = Vec::new();
        let mut files = Vec::new();
        scan(&self.root.join("log"), &mut files, &mut warnings)?;
        let mut sources = Vec::new();
        scan(&self.root.join("sources"), &mut sources, &mut warnings)?;
        files.sort();
        // Upstream read follows symlinks and silently skips unreadable directories.
        // A clean owned-tree scan permits it; otherwise parse only safely admitted files.
        let mut log = if warnings.is_empty() {
            fael_core::read(&self.root)
        } else {
            let mut log = Log::default();
            for file in &files {
                if file
                    .extension()
                    .is_none_or(|extension| extension != "jsonl")
                {
                    continue;
                }
                match read_private(file, 64 * 1024) {
                    Ok(bytes) => {
                        let rows = if file
                            .file_name()
                            .is_some_and(|name| name.to_string_lossy().ends_with(".close.jsonl"))
                        {
                            &mut log.closes
                        } else {
                            &mut log.rows
                        };
                        fael_core::parse(
                            &bytes,
                            &file.display().to_string(),
                            rows,
                            &mut log.warnings,
                        );
                    }
                    Err(error) => log.warnings.push(format!("{}: {error}", file.display())),
                }
            }
            log
        };
        log.warnings.append(&mut warnings);
        let mut referenced = HashSet::new();
        for row in log.rows.iter().chain(&log.closes) {
            match provenance(row) {
                Ok(meta) => {
                    if referenced.insert(meta.source_ref.clone()) {
                        match read_private(&self.root.join(&meta.source_ref), MAX_SOURCE_BYTES) {
                            Ok(bytes) if hash(&bytes) == meta.source_sha256 => {}
                            Ok(_) => log.warnings.push(format!(
                                "Captured source {} hash mismatch; original retained for recovery",
                                meta.source_sha256
                            )),
                            Err(error) => log.warnings.push(format!(
                                "Captured source {} unavailable: {error}",
                                meta.source_sha256
                            )),
                        }
                    }
                }
                Err(error) => log.warnings.push(format!(
                    "Unqualified context row {} retained: {error}",
                    row.id
                )),
            }
        }
        for source in sources {
            let reference = source
                .strip_prefix(&self.root)?
                .to_string_lossy()
                .into_owned();
            if !referenced.contains(&reference) {
                log.warnings.push(format!(
                    "Unreferenced captured source retained for recovery: {reference}"
                ));
            }
        }
        log.warnings.sort();
        log.warnings.dedup();
        Ok(log)
    }

    fn check_roots(&self) -> Result<()> {
        for path in [
            &self.root,
            &self.root.join("log"),
            &self.root.join("sources"),
        ] {
            let metadata = fs::symlink_metadata(path)?;
            ensure!(
                metadata.is_dir() && !metadata.file_type().is_symlink(),
                "Context directory {} changed; preserve and repair its original tree",
                path.display()
            );
            private_metadata(&metadata)?;
        }
        Ok(())
    }
}

fn identity(value: &str) -> bool {
    !value.is_empty() && value.len() <= 4096 && !value.chars().any(char::is_control)
}
fn hash(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn provenance(row: &Row) -> Result<Provenance> {
    let meta: Provenance = serde_json::from_value(
        row.extra
            .get("t3oxide")
            .context("Missing native context provenance")?
            .clone(),
    )?;
    meta.source.validate()?;
    ensure!(
        meta.source_sha256.len() == 64
            && meta
                .source_sha256
                .bytes()
                .all(|byte| byte.is_ascii_hexdigit())
            && meta.source_ref == format!("sources/{}.bin", meta.source_sha256),
        "Invalid captured source reference/hash"
    );
    Ok(meta)
}
fn private_metadata(metadata: &fs::Metadata) -> Result<()> {
    // SAFETY: geteuid takes no arguments and has no preconditions.
    ensure!(
        metadata.uid() == unsafe { libc::geteuid() } && metadata.mode() & 0o077 == 0,
        "Context files/directories must be private and owned by the execution account"
    );
    Ok(())
}
fn private_directory(path: &Path) -> Result<()> {
    match fs::symlink_metadata(path) {
        Ok(metadata) => {
            ensure!(
                metadata.is_dir() && !metadata.file_type().is_symlink(),
                "Context directory must not be a symlink"
            );
            private_metadata(&metadata)
        }
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            fs::DirBuilder::new().mode(0o700).create(path)?;
            File::open(path.parent().context("Context directory needs a parent")?)?.sync_all()?;
            Ok(())
        }
        Err(error) => Err(error.into()),
    }
}
fn read_private(path: &Path, limit: usize) -> Result<Vec<u8>> {
    let mut file = OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NOFOLLOW | libc::O_NONBLOCK)
        .open(path)?;
    let metadata = file.metadata()?;
    ensure!(
        metadata.is_file() && metadata.len() <= limit as u64,
        "Context file is not regular or exceeds its read limit"
    );
    private_metadata(&metadata)?;
    let mut bytes = Vec::new();
    Read::by_ref(&mut file)
        .take(limit as u64 + 1)
        .read_to_end(&mut bytes)?;
    ensure!(
        bytes.len() <= limit,
        "Context file grew beyond its read limit"
    );
    Ok(bytes)
}
fn source_limited(source: &Source) -> bool {
    source.observed["limited"] == true || source.observed["truncated"] == true
}
fn publish(path: &Path, bytes: &[u8]) -> Result<()> {
    let parent = path
        .parent()
        .context("Context publication needs an owned directory")?;
    private_directory(parent)?;
    let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
    temporary.write_all(bytes)?;
    temporary.as_file().sync_all()?;
    temporary.persist_noclobber(path).map_err(|error| {
        anyhow::anyhow!("Context publication failed; original retained: {error}")
    })?;
    File::open(parent)?.sync_all().with_context(|| format!("Context record {} was published but durability is uncertain; inspect its exact identity before recording again", path.display()))
}
fn scan(directory: &Path, files: &mut Vec<PathBuf>, warnings: &mut Vec<String>) -> Result<()> {
    let metadata = fs::symlink_metadata(directory)
        .with_context(|| format!("Context directory {} unavailable", directory.display()))?;
    ensure!(
        metadata.is_dir() && !metadata.file_type().is_symlink(),
        "Context directory {} is not a regular owned directory",
        directory.display()
    );
    private_metadata(&metadata)?;
    let entries = match fs::read_dir(directory) {
        Ok(entries) => entries,
        Err(error) => {
            warnings.push(format!(
                "{}: unreadable directory: {error}",
                directory.display()
            ));
            return Ok(());
        }
    };
    for entry in entries {
        let entry = match entry {
            Ok(entry) => entry,
            Err(error) => {
                warnings.push(format!("Directory entry unreadable: {error}"));
                continue;
            }
        };
        let path = entry.path();
        let metadata = match fs::symlink_metadata(&path) {
            Ok(metadata) => metadata,
            Err(error) => {
                warnings.push(format!("{}: {error}", path.display()));
                continue;
            }
        };
        if metadata.file_type().is_symlink() {
            warnings.push(format!(
                "Symlink excluded from context store: {}",
                path.display()
            ));
            continue;
        }
        if let Err(error) = private_metadata(&metadata) {
            warnings.push(format!("{}: {error}", path.display()));
            continue;
        }
        if metadata.is_dir() {
            scan(&path, files, warnings)?;
        } else if metadata.is_file() {
            if path
                .file_name()
                .is_some_and(|name| name.to_string_lossy().starts_with('.'))
            {
                warnings.push(format!(
                    "Staged context input retained for recovery: {}",
                    path.display()
                ));
            } else {
                files.push(path);
            }
        } else {
            warnings.push(format!(
                "Non-file excluded from context store: {}",
                path.display()
            ));
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::os::unix::fs::{PermissionsExt, symlink};

    fn source(revision: &str) -> Source {
        Source {
            environment_id: "environment-a".into(),
            project_id: "project-a".into(),
            thread_id: "thread-a".into(),
            item_id: Some("message-a".into()),
            request_id: Some("request-a".into()),
            revision: revision.into(),
            observed: json!({"revision":17,"state":"live","limited":false}),
        }
    }
    fn query() -> Query {
        Query::new(Scope {
            environment_id: "environment-a".into(),
            project_id: Some("project-a".into()),
            thread_id: Some("thread-a".into()),
        })
    }
    fn input(kind: Kind, revision: &str, text: &str) -> RecordInput {
        RecordInput {
            source: source(revision),
            kind,
            text: text.into(),
            full_source: b"Original native source envelope and retained owner wording.".to_vec(),
            supersedes: None,
            revisit: None,
        }
    }

    #[test]
    fn durable_capture_reopens_with_full_source_and_explicit_local_lifecycle() {
        let temporary = tempfile::tempdir().unwrap();
        let path = temporary.path().join("context-v1");
        let store = Store::open(&path).unwrap();
        assert!(
            Store::open(&path).is_err(),
            "a second store cannot race the writer"
        );
        let mut original = input(
            Kind::Decision,
            "request-revision-a",
            "Use the stable release.",
        );
        original.full_source = "Full owner wording 日本語 🙂\n".repeat(900).into_bytes();
        let original_id = store.record(&original).unwrap();
        assert!(original.full_source.len() > fael_core::ROW_BYTES_MAX);
        drop(store);
        let store = Store::open(&path).unwrap();
        let captured = store.source(&original_id).unwrap();
        assert_eq!(captured.bytes, original.full_source);
        assert_eq!(captured.sha256, hash(&original.full_source));
        assert_eq!(captured.source, original.source);
        let mut replacement = input(
            Kind::Decision,
            "request-revision-b",
            "Use the next release.",
        );
        replacement.supersedes = Some(original_id.clone());
        let replacement_id = store.record(&replacement).unwrap();
        store
            .revisit(&replacement_id, &replacement.source, "next release")
            .unwrap();
        assert!(
            store
                .close(&replacement_id, &original.source, "wrong revision")
                .is_err()
        );
        store
            .close(
                &replacement_id,
                &replacement.source,
                "Local decision retired",
            )
            .unwrap();
        let open = store.recall(&query()).unwrap();
        assert_eq!(
            open.eligible, 0,
            "local close/supersession affect open context only"
        );
        assert!(open.complete);
        let mut history = query();
        history.history = true;
        let history = store.recall(&history).unwrap();
        assert_eq!(history.eligible, 2);
        assert!(
            history
                .rows
                .iter()
                .any(|row| row.id == original_id && row.superseded)
        );
        assert!(history.rows.iter().any(|row| row.id == replacement_id
            && row.closed
            && row.revisit.as_deref() == Some("next release")));
        assert_eq!(
            store.source(&original_id).unwrap().bytes,
            original.full_source
        );
    }

    #[test]
    fn ranked_packs_have_exact_totals_real_byte_bounds_and_native_resume_identity() {
        let temporary = tempfile::tempdir().unwrap();
        let store = Store::open(&temporary.path().join("context-v1")).unwrap();
        for index in 0..8 {
            store
                .record(&input(
                    Kind::Journal,
                    &format!("revision-{index}"),
                    &format!("Journal {index}: {}", "evidence 日本語 ".repeat(35)),
                ))
                .unwrap();
        }
        let mut other = input(
            Kind::Evidence,
            "other-environment",
            "Not in this native scope",
        );
        other.source.environment_id = "environment-b".into();
        store.record(&other).unwrap();
        let mut request = query();
        request.offset = 2;
        request.limit = 3;
        let page = store.recall(&request).unwrap();
        assert_eq!((page.eligible, page.shown, page.omitted), (8, 3, 5));
        assert!(
            page.rows
                .iter()
                .all(|row| row.source.environment_id == "environment-a")
        );
        assert_eq!(
            page.id,
            store.recall(&request).unwrap().id,
            "unchanged source/query yields a stable pack identity"
        );
        let native = vec![NativeRecord {
            source: source("native-original-revision"),
            kind: NativeKind::PendingOperation,
            identity: "original-operation-a".into(),
            status: "uncertain".into(),
            summary: "Answer acknowledgement pending".into(),
        }];
        request.offset = 0;
        request.limit = 20;
        request.byte_limit = 2048;
        let pack = store.resume(&request, &native).unwrap();
        assert!(serde_json::to_vec(&pack).unwrap().len() <= request.byte_limit);
        assert_eq!(pack.eligible, 8);
        assert_eq!(pack.shown + pack.omitted, 8);
        assert!(pack.truncated && pack.shown < 8);
        assert_eq!(pack.native_eligible, 1);
        assert_eq!(pack.native_shown + pack.native_omitted, 1);
        assert_eq!(pack.native_records[0].identity, "original-operation-a");
        assert_eq!(
            pack.native_records[0].source.revision,
            "native-original-revision"
        );
        assert_eq!(
            native[0].status, "uncertain",
            "resume never resolves protected records"
        );
        let mut oversized = native[0].clone();
        oversized.summary = "x".repeat(6000);
        let bounded = store.resume(&request, &[oversized]).unwrap();
        assert!(serde_json::to_vec(&bounded).unwrap().len() <= request.byte_limit);
        assert_eq!(
            (
                bounded.native_eligible,
                bounded.native_shown,
                bounded.native_omitted
            ),
            (1, 0, 1),
            "protected-record summaries cannot bypass the complete pack byte bound"
        );
    }

    #[test]
    fn damaged_logs_symlinks_and_unreadable_subtrees_are_reported_as_degraded() {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("context-v1");
        let store = Store::open(&root).unwrap();
        let id = store
            .record(&input(
                Kind::Evidence,
                "revision-a",
                "Known source evidence",
            ))
            .unwrap();
        let outside = temporary.path().join("outside");
        fs::write(&outside, b"outside content must never be retrieved").unwrap();
        symlink(&outside, root.join("log/symlink.jsonl")).unwrap();
        let torn = root.join("log/torn.jsonl");
        fs::write(&torn, b"{\"id\":\"torn").unwrap();
        fs::set_permissions(&torn, fs::Permissions::from_mode(0o600)).unwrap();
        let unreadable = root.join("log/unreadable");
        fs::create_dir(&unreadable).unwrap();
        fs::set_permissions(&unreadable, fs::Permissions::from_mode(0o0)).unwrap();
        let pack = store.recall(&query()).unwrap();
        fs::set_permissions(&unreadable, fs::Permissions::from_mode(0o700)).unwrap();
        assert!(!pack.complete && pack.eligible == 1);
        assert!(
            pack.degradation
                .iter()
                .any(|warning| warning.contains("Symlink excluded"))
        );
        assert!(
            pack.degradation
                .iter()
                .any(|warning| warning.contains("torn last line"))
        );
        assert!(
            !serde_json::to_string(&pack)
                .unwrap()
                .contains("outside content")
        );
        let log = store.load().unwrap();
        let meta = provenance(log.rows.iter().find(|row| row.id == id).unwrap()).unwrap();
        fs::write(root.join(meta.source_ref), b"Changed captured source").unwrap();
        let corrupted = store.recall(&query()).unwrap();
        assert!(
            !corrupted.complete
                && corrupted
                    .degradation
                    .iter()
                    .any(|warning| warning.contains("hash mismatch")),
            "recall verifies complete captured source hashes too"
        );
        assert!(
            store.source(&id).is_err(),
            "full source reads verify the retained content hash"
        );
    }

    #[test]
    fn interrupted_publication_retains_recovery_inputs_and_rejects_oversized_rows() {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("context-v1");
        let store = Store::open(&root).unwrap();
        let original = input(
            Kind::OpenLoop,
            "revision-a",
            "Recorded open loop, not a native question",
        );
        let id = store.record(&original).unwrap();
        let too_large = input(
            Kind::Decision,
            "revision-large",
            &"x".repeat(fael_core::ROW_BYTES_MAX),
        );
        assert!(store.record(&too_large).is_err());
        let orphan = b"Complete source published before an interrupted row commit";
        let orphan_path = root.join("sources").join(format!("{}.bin", hash(orphan)));
        publish(&orphan_path, orphan).unwrap();
        let staged = root.join("log/.interrupted-stage");
        fs::write(&staged, b"partial unpublished row").unwrap();
        fs::set_permissions(&staged, fs::Permissions::from_mode(0o600)).unwrap();
        drop(store);
        let store = Store::open(&root).unwrap();
        let pack = store.recall(&query()).unwrap();
        assert_eq!(pack.eligible, 1);
        assert_eq!(pack.rows[0].id, id);
        assert!(!pack.complete);
        assert!(
            pack.degradation
                .iter()
                .any(|warning| warning.contains("Unreferenced captured source"))
        );
        assert!(
            pack.degradation
                .iter()
                .any(|warning| warning.contains("Staged context input"))
        );
        assert_eq!(fs::read(&orphan_path).unwrap(), orphan);
        assert!(staged.exists(), "recovery inputs are not pruned");
        assert!(
            pack.native_records.is_empty(),
            "local issues never create native obligations"
        );
    }
}
