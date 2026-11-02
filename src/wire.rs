//! Bounded companion-v1 NDJSON transport. A connection owns only its adapter process.
use crate::config::{Environment, valid_id};
use anyhow::{Context, Result};
use crossbeam_channel::{Receiver, Sender, bounded};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::collections::{HashMap, VecDeque};
use std::fmt;
use std::io::{self, BufRead, BufReader, Read, Write};
use std::os::fd::AsRawFd;
use std::process::{Child, ChildStdin, Stdio};
use std::sync::{
    Arc, Mutex,
    atomic::{AtomicBool, AtomicUsize, Ordering},
};
use std::thread;
use std::time::{Duration, Instant};

pub const MAX_FRAME_BYTES: usize = 384 * 1024;
pub const MAX_INPUT_BYTES: usize = 96 * 1024;
const MAX_EVENT_BYTES: usize = 2 * 1024 * 1024;
const REQUEST_TIMEOUT: Duration = Duration::from_secs(50);
const WRITE_TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
pub struct Fault {
    pub code: String,
    pub message: String,
    #[serde(default)]
    pub uncertain: bool,
}
impl Fault {
    pub fn new(code: &str, message: impl Into<String>, uncertain: bool) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
            uncertain,
        }
    }
}
impl fmt::Display for Fault {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}
impl std::error::Error for Fault {}

#[derive(Debug)]
struct EventLease {
    bytes: usize,
    total: Arc<AtomicUsize>,
}
impl Drop for EventLease {
    fn drop(&mut self) {
        self.total.fetch_sub(self.bytes, Ordering::AcqRel);
    }
}

#[derive(Clone, Debug)]
pub struct Packet {
    pub v: u64,
    pub environment_id: String,
    pub id: String,
    pub event: String,
    pub data: Value,
    pub result: Value,
    pub error: Option<Fault>,
    _lease: Option<Arc<EventLease>>,
}
impl Packet {
    pub fn event(environment_id: &str, event: &str, data: Value) -> Self {
        Self {
            v: 1,
            environment_id: environment_id.into(),
            id: String::new(),
            event: event.into(),
            data,
            result: Value::Null,
            error: None,
            _lease: None,
        }
    }
}

pub fn new_id() -> String {
    uuid::Uuid::new_v4().to_string()
}

type Reply = std::result::Result<Value, Fault>;
struct Pending {
    reply: Sender<Reply>,
    stop: bool,
    mutation: bool,
    cancellable: bool,
}
struct Frame {
    bytes: Vec<u8>,
    stop: bool,
}
#[derive(Default)]
struct Queues {
    pending: HashMap<String, Pending>,
    regular: VecDeque<Frame>,
    stop: VecDeque<Frame>,
    regular_writes: usize,
    stop_writes: usize,
}
struct Inner {
    environment: Environment,
    child: Mutex<Child>,
    queues: Mutex<Queues>,
    closed: AtomicBool,
    verified: AtomicBool,
    writable: AtomicBool,
    cancellation: AtomicBool,
    handles: AtomicUsize,
    fault: Mutex<Option<Fault>>,
    wake: Sender<()>,
    events: Mutex<Option<Sender<Packet>>>,
    event_bytes: Arc<AtomicUsize>,
}

pub struct Connection {
    inner: Arc<Inner>,
    events: Receiver<Packet>,
}
impl Clone for Connection {
    fn clone(&self) -> Self {
        self.inner.handles.fetch_add(1, Ordering::Relaxed);
        Self {
            inner: self.inner.clone(),
            events: self.events.clone(),
        }
    }
}
impl Drop for Connection {
    fn drop(&mut self) {
        if self.inner.handles.fetch_sub(1, Ordering::AcqRel) == 1 {
            self.close();
        }
    }
}

impl Connection {
    pub fn start(environment: Environment) -> Result<Self> {
        environment.validate()?;
        let mut child = environment
            .command()
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .context("start configured host adapter")?;
        let input = child.stdin.take().context("adapter input pipe missing")?;
        let output = child.stdout.take().context("adapter output pipe missing")?;
        let diagnostics = child
            .stderr
            .take()
            .context("adapter diagnostics pipe missing")?;
        // Nonblocking writes allow a deadline even when a helper stops consuming stdin.
        let descriptor = input.as_raw_fd();
        // SAFETY: descriptor is a live, owned ChildStdin and F_GETFL/F_SETFL do not take pointers.
        let flags = unsafe { libc::fcntl(descriptor, libc::F_GETFL) };
        if flags < 0
            || unsafe { libc::fcntl(descriptor, libc::F_SETFL, flags | libc::O_NONBLOCK) } < 0
        {
            let _ = child.kill();
            let _ = child.wait();
            return Err(io::Error::last_os_error()).context("set bounded adapter pipe writes");
        }
        let (wake, awake) = bounded(1);
        let (events, receiver) = bounded(1024);
        let inner = Arc::new(Inner {
            environment,
            child: Mutex::new(child),
            queues: Mutex::new(Queues::default()),
            closed: AtomicBool::new(false),
            verified: AtomicBool::new(false),
            writable: AtomicBool::new(false),
            cancellation: AtomicBool::new(false),
            handles: AtomicUsize::new(1),
            fault: Mutex::new(None),
            wake,
            events: Mutex::new(Some(events)),
            event_bytes: Arc::new(AtomicUsize::new(0)),
        });
        let reader = inner.clone();
        thread::Builder::new()
            .name("t3oxide-adapter-read".into())
            .spawn(move || {
                read_output(&reader, output);
                // Reap only after complete stdout frames have been delivered, including a final ACK.
                let _ = reader.child.lock().unwrap().wait();
            })?;
        let writer = inner.clone();
        thread::Builder::new()
            .name("t3oxide-adapter-write".into())
            .spawn(move || write_input(&writer, input, awake))?;
        thread::Builder::new()
            .name("t3oxide-adapter-stderr".into())
            .spawn(move || {
                let _ = io::copy(&mut BufReader::new(diagnostics), &mut io::sink());
            })?;
        Ok(Self {
            inner,
            events: receiver,
        })
    }

    pub fn events(&self) -> Receiver<Packet> {
        self.events.clone()
    }
    pub fn is_closed(&self) -> bool {
        self.inner.closed.load(Ordering::Acquire)
    }
    pub fn is_verified(&self) -> bool {
        self.inner.verified.load(Ordering::Acquire)
    }
    pub fn fault(&self) -> Option<Fault> {
        self.inner.fault.lock().unwrap().clone()
    }
    pub fn environment(&self) -> &Environment {
        &self.inner.environment
    }

    pub fn close(&self) {
        finish(
            &self.inner,
            Fault::new(
                "disconnected",
                "connection closed; unacknowledged operations remain saved",
                true,
            ),
        );
    }

    pub fn hello(&self) -> Reply {
        self.request("hello", json!({}), None)
    }

    /// Never retries. Mutation callers must first commit the exact packet to State.
    pub fn request(&self, op: &str, fields: Value, id: Option<&str>) -> Reply {
        self.request_with_timeout(op, fields, id, REQUEST_TIMEOUT)
    }

    pub fn request_with_timeout(
        &self,
        op: &str,
        fields: Value,
        id: Option<&str>,
        timeout: Duration,
    ) -> Reply {
        let mutation = is_mutation(op);
        if !matches!(
            op,
            "hello"
                | "list"
                | "watch"
                | "history"
                | "models"
                | "activity"
                | "inspect-item"
                | "select-model"
                | "send"
                | "stop"
                | "answer"
                | "launch"
                | "projects"
                | "launch-defaults"
                | "launch-project"
                | "thread-action"
                | "thread-review"
                | "thread-preview"
                | "native-defaults"
        ) {
            return Err(Fault::new(
                "invalid_request",
                "unsupported companion operation",
                false,
            ));
        }
        if op != "hello" && !self.is_verified() {
            return Err(Fault::new(
                "incompatible",
                "verify the environment handshake before requesting work",
                false,
            ));
        }
        if mutation && !self.inner.writable.load(Ordering::Acquire) {
            return Err(Fault::new(
                "read_only",
                "this verified environment has not enabled human mutations",
                false,
            ));
        }
        if mutation && id.is_none() {
            return Err(Fault::new(
                "invalid_request",
                "mutations require an explicit durable operation identity",
                false,
            ));
        }
        let id = id.map(str::to_owned).unwrap_or_else(new_id);
        if !valid_id(&id, 4096) {
            return Err(Fault::new("invalid_request", "invalid request ID", false));
        }
        let Some(mut object) = fields.as_object().cloned() else {
            return Err(Fault::new(
                "invalid_request",
                "request fields must be an object",
                false,
            ));
        };
        object.insert("v".into(), json!(1));
        object.insert("id".into(), json!(id));
        object.insert("op".into(), json!(op));
        object.insert(
            "environmentId".into(),
            json!(self.inner.environment.environment_id),
        );
        let mut bytes = serde_json::to_vec(&object)
            .map_err(|_| Fault::new("invalid_request", "request could not be encoded", false))?;
        if bytes.len() > MAX_INPUT_BYTES {
            return Err(Fault::new(
                "too_large",
                "request exceeds 96 KiB; nothing sent",
                false,
            ));
        }
        bytes.push(b'\n');
        let (reply, response) = bounded(1);
        {
            let mut queues = self.inner.queues.lock().unwrap();
            if self.is_closed() {
                return Err(Fault::new(
                    "disconnected",
                    "not connected; nothing sent",
                    false,
                ));
            }
            let stop = op == "stop";
            let count = queues.pending.values().filter(|p| p.stop == stop).count();
            if queues.pending.contains_key(&id)
                || (stop && (count >= 1 || queues.stop_writes >= 1))
                || (!stop && (count >= 8 || queues.regular_writes >= 8))
            {
                return Err(Fault::new(
                    "backpressure",
                    "request lane is full; nothing sent",
                    false,
                ));
            }
            queues.pending.insert(
                id.clone(),
                Pending {
                    reply,
                    stop,
                    mutation,
                    cancellable: is_read(op),
                },
            );
            let frame = Frame { bytes, stop };
            if stop {
                queues.stop.push_back(frame);
                queues.stop_writes += 1;
            } else {
                queues.regular.push_back(frame);
                queues.regular_writes += 1;
            }
        }
        let _ = self.inner.wake.try_send(());
        let result = match response.recv_timeout(timeout) {
            Ok(result) => result,
            Err(_) => match response.try_recv() {
                Ok(result) => result,
                Err(_) => {
                    if is_read(op) {
                        self.cancel_read(&id);
                    }
                    Err(Fault::new(
                        if self.is_closed() {
                            "disconnected"
                        } else {
                            "timeout"
                        },
                        if mutation {
                            "no acknowledgement; acceptance is unknown—recover only the saved operation ID"
                        } else {
                            "read did not complete; refresh to acquire current native state"
                        },
                        mutation,
                    ))
                }
            },
        };
        self.inner.queues.lock().unwrap().pending.remove(&id);
        if op == "hello" {
            let hello = result?;
            if hello["protocol"] != 1
                || hello["orchestrationProtocol"] != 2
                || hello["environmentId"].as_str()
                    != Some(self.inner.environment.environment_id.as_str())
                || !hello["writesEnabled"].is_boolean()
            {
                let fault = Fault::new(
                    "incompatible",
                    "adapter handshake does not match companion v1 / orchestration v2 / configured environment",
                    false,
                );
                finish(&self.inner, fault.clone());
                return Err(fault);
            }
            self.inner.cancellation.store(
                hello["features"]["readCancellation"] == true,
                Ordering::Release,
            );
            self.inner
                .writable
                .store(hello["writesEnabled"] == true, Ordering::Release);
            self.inner.verified.store(true, Ordering::Release);
            return Ok(hello);
        }
        result
    }

    pub fn cancel_read(&self, id: &str) {
        if !valid_id(id, 4096) || self.is_closed() {
            return;
        }
        let mut queues = self.inner.queues.lock().unwrap();
        let Some(pending) = queues.pending.get(id).filter(|pending| pending.cancellable) else {
            return;
        };
        // A stalled remote read must not occupy a foreground worker after the
        // human dismisses it. Never release or cancel a mutation/hello waiter.
        let _ = pending.reply.try_send(Err(Fault::new(
            "cancelled",
            "read dismissed; no native mutation was cancelled",
            false,
        )));
        if !self.inner.cancellation.load(Ordering::Acquire) || queues.regular_writes >= 16 {
            return;
        }
        let mut bytes = serde_json::to_vec(
            &json!({"v":1,"environmentId":self.inner.environment.environment_id,
            "id":new_id(),"op":"cancel-read","requestId":id}),
        )
        .expect("JSON control frame");
        bytes.push(b'\n');
        queues.regular.push_back(Frame { bytes, stop: false });
        queues.regular_writes += 1;
        let _ = self.inner.wake.try_send(());
    }
}

pub fn is_mutation(op: &str) -> bool {
    matches!(
        op,
        "send" | "stop" | "answer" | "launch" | "select-model" | "launch-project" | "thread-action"
    )
}
fn is_read(op: &str) -> bool {
    op != "hello" && !is_mutation(op)
}

fn finish(inner: &Inner, fault: Fault) {
    if inner.closed.swap(true, Ordering::AcqRel) {
        return;
    }
    inner.verified.store(false, Ordering::Release);
    inner.writable.store(false, Ordering::Release);
    *inner.fault.lock().unwrap() = Some(fault.clone());
    {
        let mut queues = inner.queues.lock().unwrap();
        for (_, pending) in queues.pending.drain() {
            let mut failure = fault.clone();
            failure.uncertain = pending.mutation;
            let _ = pending.reply.try_send(Err(failure));
        }
        queues.regular.clear();
        queues.stop.clear();
    }
    let _ = inner.wake.try_send(());
    // End the event stream after buffered frames; retained public handles must
    // not keep consumers blocked after their helper has failed or closed.
    inner.events.lock().unwrap().take();
    // Only this connection's child is terminated. T3 runs belong to the existing service.
    let _ = inner.child.lock().unwrap().kill();
}

fn write_input(inner: &Inner, mut input: ChildStdin, awake: Receiver<()>) {
    while !inner.closed.load(Ordering::Acquire) {
        let frame = {
            let mut queues = inner.queues.lock().unwrap();
            queues
                .stop
                .pop_front()
                .or_else(|| queues.regular.pop_front())
        };
        let Some(frame) = frame else {
            let _ = awake.recv();
            continue;
        };
        let deadline = Instant::now() + WRITE_TIMEOUT;
        let mut bytes = frame.bytes.as_slice();
        while !bytes.is_empty() && !inner.closed.load(Ordering::Acquire) {
            match input.write(bytes) {
                Ok(0) => {
                    finish(
                        inner,
                        Fault::new("disconnected", "adapter input ended during a frame", true),
                    );
                    return;
                }
                Ok(n) => bytes = &bytes[n..],
                Err(error) if error.kind() == io::ErrorKind::Interrupted => continue,
                Err(error) if error.kind() == io::ErrorKind::WouldBlock => {
                    if Instant::now() >= deadline {
                        finish(
                            inner,
                            Fault::new(
                                "timeout",
                                "adapter input stalled; unacknowledged operations remain saved",
                                true,
                            ),
                        );
                        return;
                    }
                    let mut descriptor = libc::pollfd {
                        fd: input.as_raw_fd(),
                        events: libc::POLLOUT,
                        revents: 0,
                    };
                    // SAFETY: one live descriptor, valid pointer to one pollfd, finite 50ms wait.
                    unsafe {
                        libc::poll(&mut descriptor, 1, 50);
                    }
                }
                Err(_) => {
                    finish(
                        inner,
                        Fault::new(
                            "disconnected",
                            "adapter input closed; acceptance is unknown",
                            true,
                        ),
                    );
                    return;
                }
            }
        }
        let mut queues = inner.queues.lock().unwrap();
        if frame.stop {
            queues.stop_writes = queues.stop_writes.saturating_sub(1);
        } else {
            queues.regular_writes = queues.regular_writes.saturating_sub(1);
        }
    }
}

fn read_frame(reader: &mut impl BufRead) -> io::Result<Option<Vec<u8>>> {
    let mut frame = Vec::new();
    loop {
        let available = reader.fill_buf()?;
        if available.is_empty() {
            return if frame.is_empty() {
                Ok(None)
            } else {
                Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "adapter output ended with an incomplete frame",
                ))
            };
        }
        let newline = available.iter().position(|b| *b == b'\n');
        let count = newline.unwrap_or(available.len());
        if frame.len() + count > MAX_FRAME_BYTES {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "adapter frame exceeds 384 KiB",
            ));
        }
        frame.extend_from_slice(&available[..count]);
        reader.consume(count + usize::from(newline.is_some()));
        if newline.is_some() {
            return Ok(Some(frame));
        }
    }
}

fn read_output(inner: &Inner, output: impl Read) {
    let mut reader = BufReader::with_capacity(32 * 1024, output);
    loop {
        let frame = match read_frame(&mut reader) {
            Ok(Some(frame)) => frame,
            Ok(None) => {
                finish(
                    inner,
                    Fault::new("disconnected", "host adapter ended", true),
                );
                return;
            }
            Err(error) => {
                finish(inner, Fault::new("incompatible", error.to_string(), true));
                return;
            }
        };
        if frame.is_empty() {
            continue;
        }
        let value: Value = match serde_json::from_slice(&frame) {
            Ok(value) => value,
            Err(_) => {
                finish(
                    inner,
                    Fault::new(
                        "incompatible",
                        "adapter frame is not valid UTF-8 JSON",
                        true,
                    ),
                );
                return;
            }
        };
        if !value.is_object()
            || value["v"] != 1
            || value["environmentId"].as_str() != Some(&inner.environment.environment_id)
        {
            finish(
                inner,
                Fault::new(
                    "identity_mismatch",
                    "adapter protocol or environment identity does not match",
                    true,
                ),
            );
            return;
        }
        let id = value["id"].as_str().filter(|id| valid_id(id, 4096));
        let event = value["event"]
            .as_str()
            .filter(|event| valid_id(event, 4096));
        if id.is_some() == event.is_some() {
            finish(
                inner,
                Fault::new(
                    "incompatible",
                    "adapter packet needs exactly one reply or event identity",
                    true,
                ),
            );
            return;
        }
        if let Some(id) = id {
            let result = if value.get("error").is_some_and(|v| !v.is_null()) {
                match serde_json::from_value::<Fault>(value["error"].clone()) {
                    Ok(fault)
                        if fault.code.len() <= 256 && fault.message.len() <= MAX_FRAME_BYTES =>
                    {
                        Err(fault)
                    }
                    _ => {
                        finish(
                            inner,
                            Fault::new("incompatible", "invalid adapter failure", true),
                        );
                        return;
                    }
                }
            } else if value["result"].is_object() {
                Ok(value["result"].clone())
            } else {
                finish(
                    inner,
                    Fault::new(
                        "incompatible",
                        "adapter acknowledgement is not an object",
                        true,
                    ),
                );
                return;
            };
            if let Some(pending) = inner.queues.lock().unwrap().pending.get(id) {
                let _ = pending.reply.try_send(result);
            }
        } else if let Some(event) = event {
            if !value["data"].is_object() {
                finish(
                    inner,
                    Fault::new("incompatible", "adapter event data is not an object", true),
                );
                return;
            }
            if event == "status"
                && let Some(writable) = value["data"]["writesEnabled"].as_bool()
            {
                inner.writable.store(
                    writable && inner.verified.load(Ordering::Acquire),
                    Ordering::Release,
                );
            }
            let previous = inner.event_bytes.fetch_add(frame.len(), Ordering::AcqRel);
            let lease = Arc::new(EventLease {
                bytes: frame.len(),
                total: inner.event_bytes.clone(),
            });
            if previous + frame.len() > MAX_EVENT_BYTES {
                drop(lease);
                finish(
                    inner,
                    Fault::new(
                        "backpressure",
                        "event consumer stalled; reconnect for a fresh snapshot",
                        true,
                    ),
                );
                return;
            }
            let mut packet = Packet::event(
                &inner.environment.environment_id,
                event,
                value["data"].clone(),
            );
            packet._lease = Some(lease);
            let delivered = inner
                .events
                .lock()
                .unwrap()
                .as_ref()
                .is_some_and(|events| events.try_send(packet).is_ok());
            if !delivered {
                finish(
                    inner,
                    Fault::new(
                        "backpressure",
                        "event consumer stalled; reconnect for a fresh snapshot",
                        true,
                    ),
                );
                return;
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;
    #[test]
    fn reader_rejects_unterminated_or_oversized_frames() {
        assert!(read_frame(&mut Cursor::new(b"{\"v\":1}")).is_err());
        assert!(read_frame(&mut Cursor::new(vec![b'x'; MAX_FRAME_BYTES + 1])).is_err());
        assert_eq!(
            read_frame(&mut Cursor::new(b"{}\n")).unwrap(),
            Some(b"{}".to_vec())
        );
    }
    #[test]
    fn event_budget_is_released_only_after_last_packet_clone() {
        let total = Arc::new(AtomicUsize::new(100));
        let mut packet = Packet::event("env", "view", json!({}));
        packet._lease = Some(Arc::new(EventLease {
            bytes: 100,
            total: total.clone(),
        }));
        let other = packet.clone();
        drop(packet);
        assert_eq!(total.load(Ordering::Relaxed), 100);
        drop(other);
        assert_eq!(total.load(Ordering::Relaxed), 0);
    }
}

#[cfg(test)]
mod process_tests {
    use super::*;
    use std::fs;
    use std::path::PathBuf;
    fn helper(mode: &str) -> (PathBuf, Environment) {
        let directory = std::env::temp_dir().join(format!("t3oxide-wire-test-{}", new_id()));
        fs::create_dir(&directory).unwrap();
        let script = directory.join("adapter.mjs");
        fs::write(&script, r#"
import readline from 'node:readline';
const mode = process.argv.at(-1);
const output = object => process.stdout.write(JSON.stringify({v:1,environmentId:'native',...object})+'\n');
for await (const line of readline.createInterface({input:process.stdin})) {
 const r=JSON.parse(line);
 if (r.op==='hello') {
   output({event:'status',data:{state:'connected',writesEnabled:true}});
   output({id:r.id,result:{protocol:mode==='wrong-protocol'?99:1,orchestrationProtocol:2,environmentId:'native',writesEnabled:true,features:{readCancellation:true}}});
 } else if(r.op==='send') {
   output({event:'status',data:{state:'held',id:r.id,message:r.message}});
   if (mode==='ack-exit') { output({id:r.id,result:{sequence:42,operationId:r.id}});process.exit(0); }
 } else if(r.op==='stop') output({id:r.id,result:{sequence:43,operationId:r.id}});
 else if(r.op==='list') {
   if(mode==='cancel') { output({event:'status',data:{state:'read-held',id:r.id}}); }
   else if(mode==='foreign') process.stdout.write(JSON.stringify({v:1,environmentId:'other',id:r.id,result:{}})+'\n');
   else if(mode==='oversize') process.stdout.write('x'.repeat(384*1024+1)+'\n');
   else output({id:r.id,result:{threads:[],received:r}});
 } else if(r.op==='cancel-read') output({event:'status',data:{state:'cancelled-read',requestId:r.requestId}});
}
"#).unwrap();
        let node = std::env::split_paths(&std::env::var_os("PATH").unwrap_or_default())
            .map(|p| p.join("node"))
            .find(|p| p.is_file())
            .expect("Node is required for adapter tests");
        // Configuration is an opaque host path to the transport; the test helper
        // uses its basename as a scenario selector and never reads user state.
        let mode_path = directory.join(mode);
        let env = Environment {
            id: "test-profile".into(),
            label: "Test".into(),
            environment_id: "native".into(),
            ssh_destination: None,
            node: node.to_string_lossy().into(),
            adapter: script.to_string_lossy().into(),
            host_config: mode_path.to_string_lossy().into(),
            web_url: None,
            enabled: true,
            default_for_new: false,
            allowed_provider_instance_ids: None,
        };
        // Normalize the absolute --config argument to the scenario name.
        let script_text = fs::read_to_string(&script).unwrap().replace(
            "process.argv.at(-1);",
            "process.argv.at(-1).split('/').at(-1);",
        );
        fs::write(&script, script_text).unwrap();
        (directory, env)
    }
    fn wait_state(events: &Receiver<Packet>, state: &str) -> Packet {
        let deadline = Instant::now() + Duration::from_secs(3);
        loop {
            let packet = events
                .recv_timeout(deadline.saturating_duration_since(Instant::now()))
                .expect("expected fixture event");
            if packet.data["state"] == state {
                return packet;
            }
        }
    }
    #[test]
    fn stop_overtakes_an_unacknowledged_ordinary_request_without_rewriting_id() {
        let (directory, env) = helper("hold");
        let connection = Connection::start(env).unwrap();
        connection.hello().unwrap();
        let events = connection.events();
        let sending = connection.clone();
        let pending = thread::spawn(move || {
            sending.request_with_timeout(
                "send",
                json!({"threadId":"a","message":"exact 🦀"}),
                Some("saved-send"),
                Duration::from_millis(500),
            )
        });
        let held = wait_state(&events, "held");
        assert_eq!(held.data["id"], "saved-send");
        assert_eq!(held.data["message"], "exact 🦀");
        let stopped = connection
            .request_with_timeout(
                "stop",
                json!({"threadId":"a"}),
                Some("saved-stop"),
                Duration::from_secs(2),
            )
            .unwrap();
        assert_eq!(stopped["operationId"], "saved-stop");
        let failure = pending.join().unwrap().unwrap_err();
        assert!(failure.uncertain);
        connection.close();
        drop(connection);
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn handshake_rejects_early_connected_status_and_wrong_protocol() {
        let (directory, env) = helper("wrong-protocol");
        let connection = Connection::start(env).unwrap();
        assert!(connection.hello().is_err());
        assert!(!connection.is_verified());
        let error = connection
            .request("send", json!({"threadId":"a"}), Some("durable"))
            .unwrap_err();
        assert!(!error.uncertain);
        connection.close();
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn read_timeout_sends_negotiated_cancellation_for_original_id() {
        let (directory, env) = helper("cancel");
        let connection = Connection::start(env).unwrap();
        connection.hello().unwrap();
        let events = connection.events();
        let error = connection
            .request_with_timeout(
                "list",
                json!({}),
                Some("read-original"),
                Duration::from_millis(60),
            )
            .unwrap_err();
        assert!(!error.uncertain);
        let cancelled = wait_state(&events, "cancelled-read");
        assert_eq!(cancelled.data["requestId"], "read-original");
        connection.close();
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn explicit_cancel_releases_waiter_and_cannot_cancel_a_mutation() {
        let (directory, env) = helper("cancel");
        let connection = Connection::start(env).unwrap();
        connection.hello().unwrap();
        let events = connection.events();
        let worker = connection.clone();
        let (result, received) = bounded(1);
        let thread = thread::spawn(move || {
            result
                .send(worker.request_with_timeout(
                    "list",
                    json!({}),
                    Some("dismissed"),
                    Duration::from_secs(30),
                ))
                .unwrap();
        });
        wait_state(&events, "read-held");
        connection.cancel_read("dismissed");
        let error = received
            .recv_timeout(Duration::from_millis(250))
            .unwrap()
            .unwrap_err();
        assert_eq!(error.code, "cancelled");
        assert!(!error.uncertain);
        thread.join().unwrap();
        let worker = connection.clone();
        let (result, received) = bounded(1);
        let thread = thread::spawn(move || {
            result
                .send(worker.request_with_timeout(
                    "send",
                    json!({"threadId":"a","message":"original"}),
                    Some("saved-write"),
                    Duration::from_secs(30),
                ))
                .unwrap();
        });
        wait_state(&events, "held");
        connection.cancel_read("saved-write");
        assert!(received.recv_timeout(Duration::from_millis(50)).is_err());
        connection.close();
        assert!(
            received
                .recv_timeout(Duration::from_secs(1))
                .unwrap()
                .unwrap_err()
                .uncertain
        );
        thread.join().unwrap();
        fs::remove_dir_all(directory).unwrap();
    }
    #[test]
    fn identity_mismatch_and_oversized_frames_fail_closed() {
        for mode in ["foreign", "oversize"] {
            let (directory, env) = helper(mode);
            let connection = Connection::start(env).unwrap();
            connection.hello().unwrap();
            assert!(connection.request("list", json!({}), None).is_err());
            assert!(connection.is_closed());
            connection.close();
            fs::remove_dir_all(directory).unwrap();
        }
    }
    #[test]
    fn final_ack_survives_immediate_adapter_exit() {
        let (directory, env) = helper("ack-exit");
        let connection = Connection::start(env).unwrap();
        connection.hello().unwrap();
        let reply = connection
            .request(
                "send",
                json!({"threadId":"a","message":"saved"}),
                Some("exact-op"),
            )
            .unwrap();
        assert_eq!(reply["operationId"], "exact-op");
        assert_eq!(reply["sequence"], 42);
        connection.close();
        fs::remove_dir_all(directory).unwrap();
    }
}
