//! One in-process owner for the private context store. No native operation runs here.
use super::Background;
#[cfg(test)]
use super::context::IoBarrier;
use crate::context_store::{self as store, NativeRecord, PackRow, Query, Scope, Source};
use crossbeam_channel::{Sender, TrySendError, bounded};
use std::{
    path::PathBuf,
    thread::{self, JoinHandle},
};

pub(super) const CAPACITY: usize = 4;
pub(super) const MAX_SOURCE_BYTES: usize = 16 * 1024 * 1024;

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct ViewToken {
    pub serial: u64,
    pub generation: u64,
    pub view: u64,
    pub scope: Scope,
}

#[derive(Debug)]
pub(crate) enum Read {
    Recall {
        query: Query,
        native: Vec<NativeRecord>,
    },
    Source(PackRow),
}
impl Read {
    pub fn scope(&self) -> Scope {
        match self {
            Self::Recall { query, .. } => query.scope.clone(),
            Self::Source(row) => scope(&row.source),
        }
    }
}

#[derive(Debug)]
pub(crate) struct ReadRequest {
    pub token: ViewToken,
    pub operation: Read,
}

#[derive(Debug)]
pub(crate) enum Write {
    ObserveLoaded(Vec<store::RecordInput>),
    Record(store::RecordInput),
    Close(PackRow),
    Revisit {
        row: PackRow,
        condition: String,
        draft_version: u64,
    },
}
impl Write {
    pub fn source(&self) -> Option<&Source> {
        match self {
            Self::ObserveLoaded(_) => None,
            Self::Record(input) => Some(&input.source),
            Self::Close(row) | Self::Revisit { row, .. } => Some(&row.source),
        }
    }
    pub fn validate_size(&self) -> anyhow::Result<()> {
        let inputs = match self {
            Self::ObserveLoaded(inputs) => inputs.as_slice(),
            Self::Record(input) => std::slice::from_ref(input),
            _ => return Ok(()),
        };
        anyhow::ensure!(inputs.len() <= 200, "Context capture exceeds 200 sources");
        let mut bytes = 0usize;
        for input in inputs {
            anyhow::ensure!(!input.full_source.is_empty(), "Context source is empty");
            bytes = bytes
                .checked_add(input.full_source.len())
                .ok_or_else(|| anyhow::anyhow!("Context capture exceeds 16 MiB"))?;
            anyhow::ensure!(bytes <= MAX_SOURCE_BYTES, "Context capture exceeds 16 MiB");
        }
        Ok(())
    }
}

#[derive(Debug)]
pub(crate) struct WriteRequest {
    pub generation: u64,
    pub operation: Write,
    prepared: Option<Box<store::PreparedEvent>>,
}
impl WriteRequest {
    pub fn new(generation: u64, operation: Write) -> Self {
        Self {
            generation,
            operation,
            prepared: None,
        }
    }
}
#[derive(Debug)]
pub(crate) enum Request {
    Read(ReadRequest),
    Write(WriteRequest),
}
#[derive(Debug)]
pub(crate) enum Output {
    Pack(Box<store::Pack>),
    Source(store::CapturedSource),
    Saved(String),
    Observed(store::CaptureBatch),
}
#[derive(Debug)]
pub(crate) struct Finished {
    pub request: Request,
    pub result: Result<Output, String>,
}
#[derive(Debug)]
pub(crate) enum Event {
    Finished(Box<Finished>),
    Stopped,
}

pub(crate) struct Worker {
    requests: Option<Sender<Box<Request>>>,
    thread: Option<JoinHandle<()>>,
}
impl Worker {
    pub fn start(
        root: PathBuf,
        completions: Sender<Background>,
        #[cfg(test)] barrier: Option<IoBarrier>,
    ) -> Self {
        let (requests, pending) = bounded::<Box<Request>>(CAPACITY);
        let thread = thread::spawn(move || {
            let mut store = None;
            #[cfg(test)]
            let mut barrier = barrier;
            while let Ok(request) = pending.recv() {
                let mut request = *request;
                #[cfg(test)]
                if let Some(barrier) = barrier.take() {
                    barrier.wait();
                }
                let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(
                    || -> anyhow::Result<Output> {
                        if store.is_none() {
                            store = Some(store::Store::open(&root)?);
                        }
                        execute(store.as_ref().expect("store initialized"), &mut request)
                    },
                ))
                .unwrap_or_else(|_| {
                    Err(anyhow::anyhow!(
                        "Context worker failed; original request retained"
                    ))
                })
                .map_err(|error| format!("{error:#}"));
                if completions
                    .send(Background::Context(Event::Finished(Box::new(Finished {
                        request,
                        result,
                    }))))
                    .is_err()
                {
                    return;
                }
            }
            drop(store);
            let _ = completions.send(Background::Context(Event::Stopped));
        });
        Self {
            requests: Some(requests),
            thread: Some(thread),
        }
    }
    pub fn try_send(&self, request: Request) -> Result<(), TrySendError<Box<Request>>> {
        match &self.requests {
            Some(sender) => sender.try_send(Box::new(request)),
            None => Err(TrySendError::Disconnected(Box::new(request))),
        }
    }
    pub fn close(&mut self) {
        self.requests.take();
    }
    /// Call only after Stopped has been received; no completion send can block join.
    pub fn join(mut self) -> anyhow::Result<()> {
        self.close();
        if let Some(thread) = self.thread.take() {
            thread
                .join()
                .map_err(|_| anyhow::anyhow!("Context worker failed during shutdown"))?;
        }
        Ok(())
    }
}

pub(super) fn execute(store: &store::Store, request: &mut Request) -> anyhow::Result<Output> {
    Ok(match request {
        Request::Read(ReadRequest {
            operation: Read::Recall { query, native },
            ..
        }) => Output::Pack(Box::new(store.resume(query, native)?)),
        Request::Read(ReadRequest {
            operation: Read::Source(row),
            ..
        }) => {
            let captured = store.source(&row.id)?;
            anyhow::ensure!(
                captured.source == row.source
                    && captured.sha256 == row.source_sha256
                    && row.source_ref == format!("sources/{}.bin", captured.sha256),
                "Context source/revision changed; prior record retained"
            );
            Output::Source(captured)
        }
        Request::Write(WriteRequest {
            operation,
            prepared,
            ..
        }) => {
            operation.validate_size()?;
            if let Write::ObserveLoaded(inputs) = operation {
                Output::Observed(store.observe_loaded(inputs)?)
            } else {
                if prepared.is_none() {
                    // Retain identity/serialized bytes in this owned request BEFORE side effects.
                    *prepared = Some(Box::new(match operation {
                        Write::Record(input) => store.prepare_record(input)?,
                        Write::Close(row) => store.prepare_close(
                            &row.id,
                            &row.source,
                            "Explicitly closed in local context",
                        )?,
                        Write::Revisit { row, condition, .. } => {
                            store.prepare_revisit(&row.id, &row.source, condition)?
                        }
                        Write::ObserveLoaded(_) => unreachable!(),
                    }));
                }
                let (source, bytes) = match operation {
                    Write::Record(input) => (&input.source, Some(input.full_source.as_slice())),
                    Write::Close(row) | Write::Revisit { row, .. } => (&row.source, None),
                    Write::ObserveLoaded(_) => unreachable!(),
                };
                Output::Saved(
                    store.commit_prepared(
                        prepared
                            .as_ref()
                            .expect("event prepared before publication"),
                        source,
                        bytes,
                    )?,
                )
            }
        }
    })
}

pub(super) fn scope(source: &Source) -> Scope {
    Scope {
        environment_id: source.environment_id.clone(),
        project_id: Some(source.project_id.clone()),
        thread_id: Some(source.thread_id.clone()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn input(item: &str, bytes: usize) -> store::RecordInput {
        store::RecordInput {
            source: Source {
                environment_id: "environment-a".into(),
                project_id: "project-a".into(),
                thread_id: "thread-a".into(),
                item_id: Some(item.into()),
                request_id: None,
                revision: "original-revision".into(),
                observed: json!({"streaming": false}),
            },
            kind: store::Kind::Evidence,
            text: "Original intent".into(),
            full_source: vec![b'x'; bytes],
            supersedes: None,
            revisit: None,
        }
    }

    fn event_files(root: &std::path::Path) -> std::collections::BTreeMap<PathBuf, Vec<u8>> {
        std::fs::read_dir(root.join("log"))
            .unwrap()
            .map(|entry| {
                let path = entry.unwrap().path();
                let bytes = std::fs::read(&path).unwrap();
                (path, bytes)
            })
            .collect()
    }

    fn uncertain_retry(kind: &str) {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("context");
        let store = store::Store::open(&root).unwrap();
        let original = input("original-uncertain", 1024);
        let operation = if kind == "record" {
            Write::Record(original.clone())
        } else {
            let target = store.record(&original).unwrap();
            let mut query = Query::new(scope(&original.source));
            query.history = true;
            let row = store
                .recall(&query)
                .unwrap()
                .rows
                .into_iter()
                .find(|row| row.id == target)
                .unwrap();
            if kind == "close" {
                Write::Close(row)
            } else {
                Write::Revisit {
                    row,
                    condition: "日本語 tomorrow  ".into(),
                    draft_version: 17,
                }
            }
        };
        let before = event_files(&root);
        let mut request = Request::Write(WriteRequest::new(93, operation));
        let mut first = None;
        let mut prepared_identity = None;
        let payload_pointer = match &request {
            Request::Write(WriteRequest {
                operation: Write::Record(input),
                ..
            }) => Some(input.full_source.as_ptr()),
            _ => None,
        };
        for _ in 0..2 {
            let fault = store::tests::DirectorySyncFailure::arm(&root.join("log"));
            let error = execute(&store, &mut request).unwrap_err();
            fault.assert_consumed();
            assert!(error.to_string().contains("durability is uncertain"));
            let Request::Write(retained) = &request else {
                unreachable!()
            };
            let (id, line) = retained
                .prepared
                .as_ref()
                .expect("identity retained before publication")
                .identity();
            let identity = (id.to_owned(), line.to_vec());
            if let Some(prior) = &prepared_identity {
                assert_eq!(prior, &identity);
            }
            prepared_identity = Some(identity);
            if let Write::Record(input) = &retained.operation {
                assert_eq!(Some(input.full_source.as_ptr()), payload_pointer);
            }
            let published: Vec<_> = event_files(&root)
                .into_iter()
                .filter(|(path, _)| !before.contains_key(path))
                .collect();
            assert_eq!(
                published.len(),
                1,
                "explicit retry must keep exactly one original event"
            );
            if let Some(prior) = &first {
                assert_eq!(prior, &published[0], "original event identity/line changed");
            }
            first = Some(published[0].clone());
        }
        let Output::Saved(id) = execute(&store, &mut request).unwrap() else {
            panic!("saved event");
        };
        let published: Vec<_> = event_files(&root)
            .into_iter()
            .filter(|(path, _)| !before.contains_key(path))
            .collect();
        assert_eq!(published.len(), 1);
        assert_eq!(Some(&published[0]), first.as_ref());
        assert!(
            published[0]
                .0
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with(&id)
        );
        let Request::Write(retained) = request else {
            panic!("retained original write");
        };
        assert_eq!(retained.generation, 93);
        assert_eq!(retained.operation.source(), Some(&original.source));
        let mut query = Query::new(scope(&original.source));
        query.history = true;
        let target = store
            .recall(&query)
            .unwrap()
            .rows
            .into_iter()
            .find(|row| row.source == original.source && row.id != id)
            .map(|row| row.id)
            .unwrap_or(id);
        assert_eq!(store.source(&target).unwrap().bytes, original.full_source);
        assert_eq!(prepared_identity.as_ref().unwrap().1, published[0].1);
        // This is an intentional NEW action after success, so preparation must be fresh.
        let mut intentional = Request::Write(WriteRequest::new(93, retained.operation));
        let Output::Saved(next_id) = execute(&store, &mut intentional).unwrap() else {
            panic!("new event");
        };
        assert_ne!(prepared_identity.unwrap().0, next_id);
        assert_eq!(event_files(&root).len(), before.len() + 2);
    }

    #[test]
    fn uncertain_record_retry_reconciles_original_event() {
        uncertain_retry("record");
    }
    #[test]
    fn uncertain_close_retry_reconciles_original_event() {
        uncertain_retry("close");
    }
    #[test]
    fn uncertain_revisit_retry_reconciles_original_event() {
        uncertain_retry("revisit");
    }

    fn lifecycle_retry_requires_original_source(kind: &str) {
        use std::os::unix::fs::{PermissionsExt, symlink};
        let mut acknowledged = Vec::new();
        for damage in ["content", "privacy", "symlink", "provenance"] {
            let temporary = tempfile::tempdir().unwrap();
            let root = temporary.path().join("context");
            let store = store::Store::open(&root).unwrap();
            let original = input("lifecycle-source", 1024);
            let target = store.record(&original).unwrap();
            let alternate = store.record(&input("other-source", 2048)).unwrap();
            let alternate = store.source(&alternate).unwrap();
            let mut query = Query::new(scope(&original.source));
            query.history = true;
            let row = store
                .recall(&query)
                .unwrap()
                .rows
                .into_iter()
                .find(|row| row.id == target)
                .unwrap();
            let source_path = root.join(&row.source_ref);
            let target_path = root.join("log").join(format!("{target}.jsonl"));
            let target_line = std::fs::read(&target_path).unwrap();
            let operation = if kind == "close" {
                Write::Close(row)
            } else {
                Write::Revisit {
                    row,
                    condition: "Original 日本語 review".into(),
                    draft_version: 17,
                }
            };
            let mut request = Request::Write(WriteRequest::new(93, operation));
            {
                let fault = store::tests::DirectorySyncFailure::arm(&root.join("log"));
                assert!(execute(&store, &mut request).is_err());
                fault.assert_consumed();
            }
            let files = event_files(&root);
            let Request::Write(retained) = &request else {
                unreachable!()
            };
            let (id, line) = retained.prepared.as_ref().unwrap().identity();
            let identity = (id.to_owned(), line.to_vec());
            let event_path = files
                .iter()
                .find(|(_, bytes)| **bytes == identity.1)
                .unwrap()
                .0
                .clone();
            let outside = temporary.path().join("outside");
            match damage {
                "content" => std::fs::write(&source_path, b"corrupt source").unwrap(),
                "privacy" => {
                    std::fs::set_permissions(&source_path, std::fs::Permissions::from_mode(0o644))
                        .unwrap()
                }
                "symlink" => {
                    std::fs::write(&outside, &original.full_source).unwrap();
                    std::fs::set_permissions(&outside, std::fs::Permissions::from_mode(0o600))
                        .unwrap();
                    std::fs::remove_file(&source_path).unwrap();
                    symlink(&outside, &source_path).unwrap();
                }
                "provenance" => {
                    let mut target: serde_json::Value =
                        serde_json::from_slice(&target_line).unwrap();
                    target["t3oxide"]["sourceSha256"] = json!(alternate.sha256);
                    target["t3oxide"]["sourceRef"] =
                        json!(format!("sources/{}.bin", alternate.sha256));
                    std::fs::write(&target_path, format!("{target}\n")).unwrap();
                }
                _ => unreachable!(),
            }
            if execute(&store, &mut request).is_ok() {
                acknowledged.push(damage);
            }
            let Request::Write(retained) = &request else {
                unreachable!()
            };
            assert_eq!(retained.generation, 93);
            assert_eq!(retained.operation.source(), Some(&original.source));
            let (id, line) = retained.prepared.as_ref().unwrap().identity();
            assert_eq!((id, line), (identity.0.as_str(), identity.1.as_slice()));
            assert_eq!(std::fs::read(&event_path).unwrap(), identity.1);
            assert_eq!(event_files(&root).len(), files.len());
            // Repair only the synthetic damage, then explicitly retry the same request.
            if damage == "symlink" {
                assert!(
                    std::fs::symlink_metadata(&source_path)
                        .unwrap()
                        .file_type()
                        .is_symlink()
                );
                assert_eq!(std::fs::read(&outside).unwrap(), original.full_source);
                std::fs::remove_file(&source_path).unwrap();
            }
            std::fs::write(&source_path, &original.full_source).unwrap();
            std::fs::set_permissions(&source_path, std::fs::Permissions::from_mode(0o600)).unwrap();
            std::fs::write(&target_path, &target_line).unwrap();
            let Output::Saved(saved) = execute(&store, &mut request).unwrap() else {
                panic!("repaired original event must be acknowledged");
            };
            assert_eq!(saved, identity.0);
            assert_eq!(event_files(&root), files);
            assert_eq!(store.source(&target).unwrap().bytes, original.full_source);
        }
        assert!(
            acknowledged.is_empty(),
            "{kind} retry acknowledged invalid original source: {acknowledged:?}"
        );
    }

    #[test]
    fn lifecycle_close_retry_requires_original_source() {
        lifecycle_retry_requires_original_source("close");
    }

    #[test]
    fn lifecycle_revisit_retry_requires_original_source() {
        lifecycle_retry_requires_original_source("revisit");
    }

    #[test]
    fn source_sync_failure_retains_prepared_event_and_one_source_allocation() {
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("context");
        let store = store::Store::open(&root).unwrap();
        let mut request = Request::Write(WriteRequest::new(
            93,
            Write::Record(input("source-sync", 1024)),
        ));
        let mut identity = None;
        let mut pointer = None;
        for _ in 0..2 {
            let fault = store::tests::DirectorySyncFailure::arm(&root.join("sources"));
            assert!(execute(&store, &mut request).is_err());
            fault.assert_consumed();
            let Request::Write(retained) = &request else {
                unreachable!()
            };
            let Write::Record(input) = &retained.operation else {
                unreachable!()
            };
            let current = retained.prepared.as_ref().unwrap().identity();
            let current = (current.0.to_owned(), current.1.to_vec());
            if let Some(prior) = &identity {
                assert_eq!(prior, &current);
            }
            if let Some(prior) = pointer {
                assert_eq!(prior, input.full_source.as_ptr());
            }
            identity = Some(current);
            pointer = Some(input.full_source.as_ptr());
            assert!(event_files(&root).is_empty());
            let sources: Vec<_> = std::fs::read_dir(root.join("sources")).unwrap().collect();
            assert_eq!(sources.len(), 1);
            assert_eq!(
                std::fs::read(sources[0].as_ref().unwrap().path()).unwrap(),
                input.full_source
            );
        }
        let Output::Saved(id) = execute(&store, &mut request).unwrap() else {
            panic!("saved original");
        };
        assert_eq!(id, identity.unwrap().0);
        assert_eq!(event_files(&root).len(), 1);
    }

    #[test]
    fn prepared_retry_rejects_conflicts_and_changed_security_source_or_payload() {
        use std::os::unix::fs::{PermissionsExt, symlink};
        let temporary = tempfile::tempdir().unwrap();
        let root = temporary.path().join("context");
        let store = store::Store::open(&root).unwrap();
        let mut request = Request::Write(WriteRequest::new(
            93,
            Write::Record(input("conflict", 1024)),
        ));
        {
            let fault = store::tests::DirectorySyncFailure::arm(&root.join("log"));
            assert!(execute(&store, &mut request).is_err());
            fault.assert_consumed();
        }
        let files = event_files(&root);
        let (path, line) = files.iter().next().unwrap();
        let source_path = std::fs::read_dir(root.join("sources"))
            .unwrap()
            .next()
            .unwrap()
            .unwrap()
            .path();
        let source_bytes = std::fs::read(&source_path).unwrap();
        std::fs::write(path, b"conflicting event bytes").unwrap();
        assert!(
            execute(&store, &mut request)
                .unwrap_err()
                .to_string()
                .contains("identity conflicts")
        );
        assert_eq!(std::fs::read(path).unwrap(), b"conflicting event bytes");
        std::fs::write(path, line).unwrap();
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o644)).unwrap();
        assert!(execute(&store, &mut request).is_err());
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600)).unwrap();
        let outside = temporary.path().join("outside");
        std::fs::write(&outside, b"outside source").unwrap();
        std::fs::remove_file(path).unwrap();
        symlink(&outside, path).unwrap();
        assert!(execute(&store, &mut request).is_err());
        assert!(
            std::fs::symlink_metadata(path)
                .unwrap()
                .file_type()
                .is_symlink()
        );
        assert_eq!(std::fs::read(&outside).unwrap(), b"outside source");
        std::fs::remove_file(path).unwrap();
        std::fs::write(path, line).unwrap();
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600)).unwrap();
        std::fs::write(&source_path, b"changed captured source").unwrap();
        assert!(execute(&store, &mut request).is_err());
        assert_eq!(
            std::fs::read(&source_path).unwrap(),
            b"changed captured source"
        );
        std::fs::write(&source_path, &source_bytes).unwrap();
        std::fs::set_permissions(&root, std::fs::Permissions::from_mode(0o755)).unwrap();
        assert!(execute(&store, &mut request).is_err());
        std::fs::set_permissions(&root, std::fs::Permissions::from_mode(0o700)).unwrap();
        let Request::Write(retained) = &mut request else {
            unreachable!()
        };
        let Write::Record(input) = &mut retained.operation else {
            unreachable!()
        };
        input.source.revision = "foreign revision".into();
        assert!(execute(&store, &mut request).is_err());
        let Request::Write(retained) = &mut request else {
            unreachable!()
        };
        let Write::Record(input) = &mut retained.operation else {
            unreachable!()
        };
        input.source.revision = "original-revision".into();
        input.full_source[0] = b'y';
        assert!(execute(&store, &mut request).is_err());
        let Request::Write(retained) = &mut request else {
            unreachable!()
        };
        let Write::Record(input) = &mut retained.operation else {
            unreachable!()
        };
        input.full_source[0] = b'x';
        assert!(execute(&store, &mut request).is_ok());
        assert_eq!(event_files(&root), files);
        assert_eq!(std::fs::read(source_path).unwrap(), source_bytes);
    }

    #[test]
    fn full_and_disconnected_return_the_original_source_payload() {
        let (sender, receiver) = bounded(CAPACITY);
        let worker = Worker {
            requests: Some(sender),
            thread: None,
        };
        for index in 0..CAPACITY {
            worker
                .try_send(Request::Write(WriteRequest::new(
                    11,
                    Write::Record(input(&index.to_string(), 8)),
                )))
                .unwrap();
        }
        for disconnected in [false, true] {
            if disconnected {
                while receiver.try_recv().is_ok() {}
            }
            let request = Request::Write(WriteRequest::new(
                93,
                Write::Record(input("retained", 1024)),
            ));
            // Closing admission exercises the same Disconnected variant as a
            // stopped receiver, while retaining all request ownership.
            let error = if disconnected {
                let mut closed = Worker {
                    requests: None,
                    thread: None,
                };
                closed.close();
                closed.try_send(request).unwrap_err()
            } else {
                worker.try_send(request).unwrap_err()
            };
            assert_eq!(error.is_full(), !disconnected);
            assert_eq!(error.is_disconnected(), disconnected);
            let Request::Write(request) = *error.into_inner() else {
                panic!("original write")
            };
            assert_eq!(request.generation, 93);
            assert!(
                request.prepared.is_none(),
                "rejected admission has no prepared event or publication"
            );
            let Write::Record(retained) = request.operation else {
                panic!("original record")
            };
            assert_eq!(retained.source, input("retained", 0).source);
            assert_eq!(retained.full_source, vec![b'x'; 1024]);
        }
    }

    #[test]
    fn source_and_batch_limits_are_checked_before_admission() {
        assert!(
            Write::Record(input("exact", MAX_SOURCE_BYTES))
                .validate_size()
                .is_ok()
        );
        assert!(
            Write::Record(input("oversized", MAX_SOURCE_BYTES + 1))
                .validate_size()
                .is_err()
        );
        assert!(
            Write::ObserveLoaded(vec![
                input("a", MAX_SOURCE_BYTES / 2),
                input("b", MAX_SOURCE_BYTES / 2 + 1)
            ])
            .validate_size()
            .is_err()
        );
        assert!(
            Write::ObserveLoaded((0..201).map(|index| input(&index.to_string(), 1)).collect())
                .validate_size()
                .is_err()
        );
        assert!(Write::Record(input("empty", 0)).validate_size().is_err());
    }
}
