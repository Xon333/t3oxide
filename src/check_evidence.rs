//! Finite, source-pinned check transport; recovery only reads the original attempt.
use crate::config::Environment;
use anyhow::{Context, Result, ensure};
use base64::{Engine as _, engine::general_purpose::STANDARD};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use std::fmt::Write as _;
use std::io::{Read, Write};
use std::os::fd::AsRawFd;
use std::os::unix::process::ExitStatusExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

pub const MAX_ENVELOPE_BYTES: usize = 48 * 1024;
const MAX_PIPE_BYTES: usize = 64 * 1024;
const CONTROL_SECONDS: u64 = 30;
const CLEANUP_SECONDS: u64 = 90;
const ASSET: &str = include_str!("../scripts/check-evidence.py");

/// Explicit process-inherited placement; never inferred from visible clients.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct RexContext {
    pub executable: PathBuf,
    pub session: String,
    pub block: String,
    pub server: Option<String>,
}
impl RexContext {
    pub fn inherited(executable: PathBuf) -> Result<Self> {
        let context = Self {
            executable,
            session: std::env::var("REX_SESSION")
                .context("/check requires inherited REX_SESSION")?,
            block: std::env::var("REX_BLOCK").context("/check requires inherited REX_BLOCK")?,
            server: std::env::var("REX_SERVER").ok(),
        };
        context.validate()?;
        Ok(context)
    }
    fn validate(&self) -> Result<()> {
        ensure!(
            self.executable.is_absolute(),
            "Rex executable must be absolute"
        );
        for value in [&self.session, &self.block]
            .into_iter()
            .chain(self.server.iter())
        {
            ensure!(
                !value.is_empty() && value.len() <= 4096 && !value.contains(['\0', '\r', '\n']),
                "unsupported Rex context"
            );
        }
        Ok(())
    }
    fn command(&self) -> Command {
        let mut command = Command::new(&self.executable);
        command.arg("--autostart=false");
        if let Some(server) = &self.server {
            command.args(["--server", server]);
        }
        command
    }
}

/// Input admitted only by the explicit `/check` action.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Launch {
    pub source: Value,
    pub environment: Environment,
    pub worktree: String,
    pub argv: Vec<String>,
    pub source_paths: Vec<String>,
    pub timeout_seconds: u64,
    pub rex: RexContext,
}

/// Persist before preparing assets; this identity never changes on recovery.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Attempt {
    pub attempt_id: String,
    pub asset_sha256: String,
    pub launch: Launch,
}
impl Attempt {
    pub fn new(launch: Launch) -> Result<Self> {
        launch.environment.validate()?;
        launch.rex.validate()?;
        ensure!(
            launch.source.is_object(),
            "check source must be an opaque native target object"
        );
        ensure!(
            launch.source.get("environmentId").and_then(Value::as_str)
                == Some(launch.environment.environment_id.as_str()),
            "check target environment mismatch"
        );
        ensure!(
            Path::new(&launch.worktree).is_absolute() && !launch.worktree.contains('\0'),
            "check requires an absolute host worktree"
        );
        ensure!(
            (1..=900).contains(&launch.timeout_seconds),
            "check timeout must be 1–900 seconds"
        );
        ensure!(
            !launch.argv.is_empty()
                && launch.argv.len() <= 64
                && !launch.argv[0].is_empty()
                && launch.argv.iter().all(|s| !s.contains('\0'))
                && launch.argv.iter().map(String::len).sum::<usize>() <= 4096,
            "check needs a bounded valid argv"
        );
        ensure!(
            !launch.source_paths.is_empty() && launch.source_paths.len() <= 8,
            "select 1–8 source paths"
        );
        for path in &launch.source_paths {
            ensure!(
                !path.is_empty()
                    && path.len() <= 256
                    && path.split('/').all(|p| !p.is_empty() && p != ".")
                    && !Path::new(path).is_absolute()
                    && !path.contains('\0')
                    && !Path::new(path)
                        .components()
                        .any(|c| matches!(c, std::path::Component::ParentDir)),
                "source paths must remain relative to the worktree"
            );
        }
        ensure!(
            serde_json::to_vec(&launch)?.len() <= MAX_ENVELOPE_BYTES,
            "check request exceeds 48 KiB"
        );
        Ok(Self {
            attempt_id: uuid::Uuid::new_v4().simple().to_string(),
            asset_sha256: digest(ASSET.as_bytes()),
            launch,
        })
    }
}

/// Persist before `run`; all recovery uses this exact source-host reference.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct PreparedAttempt {
    pub attempt: Attempt,
    pub attempt_dir: String,
    pub asset_path: String,
    pub python: String,
    pub request_sha256: String,
    pub host: Value,
    pub worktree: String,
    pub preparation: Value,
}

/// Bounded exact process observations; status never implies workload status.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ProcessObservation {
    pub exit_code: Option<i32>,
    pub signal: Option<i32>,
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct VerifiedBlob {
    pub metadata: Value,
    pub bytes: Vec<u8>,
}
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Evidence {
    pub envelope: Value,
    pub exact_json: Vec<u8>,
    pub blobs: Vec<VerifiedBlob>,
    pub request_sha256: String,
    pub provenance: Value,
}
#[derive(Debug, Serialize, Deserialize)]
pub struct Completion {
    pub launch: std::result::Result<ProcessObservation, String>,
    pub evidence: std::result::Result<Evidence, String>,
}

fn digest(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
}
fn host_command(environment: &Environment, argv: &[String]) -> Result<Command> {
    ensure!(!argv.is_empty(), "empty host command");
    environment.validate()?;
    if let Some(destination) = &environment.ssh_destination {
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
            "ServerAliveInterval=10",
            "-o",
            "ServerAliveCountMax=2",
            "--",
            destination,
        ]);
        command.arg(format!(
            "exec {}",
            argv.iter().map(|v| quote(v)).collect::<Vec<_>>().join(" ")
        ));
        Ok(command)
    } else {
        let mut command = Command::new(&argv[0]);
        command.args(&argv[1..]);
        Ok(command)
    }
}

// This bootstrap runs only in prepare, never startup/recovery. HOME is observed
// from the executing account, not taken from a requested host label or TMPDIR.
const BOOTSTRAP: &str = r#"
import os,sys,json,base64,hashlib,pwd,stat,socket,tempfile
p=json.load(sys.stdin)
home=pwd.getpwuid(os.getuid()).pw_dir
root=os.path.join(home,'.local','state','t3oxide','check-evidence')
def private(path):
 os.makedirs(path,mode=0o700,exist_ok=True)
 s=os.lstat(path)
 if not stat.S_ISDIR(s.st_mode) or s.st_uid!=os.getuid() or s.st_mode&0o077: raise RuntimeError('nonprivate state directory')
 if os.path.realpath(path)!=path: raise RuntimeError('symlink state directory')
private(root); private(root+'/assets'); private(root+'/attempts')
if sys.platform=='darwin':
 if not os.statvfs(root).f_flag & getattr(os,'ST_LOCAL',4096): raise RuntimeError('state filesystem not local')
elif sys.platform.startswith('linux'):
 mounts=[]
 with open('/proc/self/mountinfo') as f:
  for line in f:
   left,right=line.rstrip().split(' - ',1); mount=left.split()[4]
   if root==mount or root.startswith(mount.rstrip('/')+'/'): mounts.append((len(mount),right.split()[0]))
 if not mounts or max(mounts)[1] not in ('ext4','xfs','btrfs','f2fs','zfs'): raise RuntimeError('state filesystem not qualified local durable storage')
else: raise RuntimeError('unsupported state filesystem qualification')
asset=base64.b64decode(p['asset'],validate=True)
sha=hashlib.sha256(asset).hexdigest()
if sha!=p['assetSha256']: raise RuntimeError('asset digest mismatch')
path=root+'/assets/'+sha+'.py'
fd,staged=tempfile.mkstemp(prefix='.publish-',dir=root+'/assets')
with os.fdopen(fd,'wb') as f: f.write(asset); f.flush(); os.fsync(f.fileno())
try: os.link(staged,path,follow_symlinks=False)
except FileExistsError: pass
s=os.lstat(path)
if not stat.S_ISREG(s.st_mode) or s.st_uid!=os.getuid() or s.st_mode&0o077: raise RuntimeError('unsafe asset')
with open(path,'rb') as f:
 if hashlib.sha256(f.read(len(asset)+1)).hexdigest()!=sha: raise RuntimeError('installed asset mismatch')
os.unlink(staged)
d=os.open(root+'/assets',os.O_RDONLY); os.fsync(d); os.close(d)
p['request']['assetSha256']=sha
p['request']['host']=socket.gethostname()
p['request']['wrapper']=os.path.realpath(os.path.join(home,'.local','bin','t3-check'))
p['request']['worktree']=os.path.realpath(p['request']['worktree'])
raw=json.dumps(p['request'],ensure_ascii=True,sort_keys=True,separators=(',',':')).encode('ascii')
if len(raw)>16384: raise RuntimeError('request exceeds 16 KiB')
# An anonymous private host-state file avoids pipe-capacity blocking at exec.
with tempfile.TemporaryFile(dir=root) as f:
 f.write(raw); f.flush(); f.seek(0); os.dup2(f.fileno(),0)
 os.execv(sys.executable,[sys.executable,path,'prepare','--state-root',root+'/attempts'])
"#;

/// Installs only a private hash-pinned asset and prepares the immutable request.
pub fn prepare(attempt: &Attempt) -> Result<PreparedAttempt> {
    ensure!(
        attempt.asset_sha256 == digest(ASSET.as_bytes()),
        "Original helper revision cannot be prepared by this build; recovery is read-only"
    );
    qualify_layout_owner(&attempt.launch.rex)?;
    attempt.launch.rex.validate()?;
    let request = json!({"schema": "t3oxide-check-evidence-v1", "attemptId": attempt.attempt_id, "target": attempt.launch.source,
        "worktree": attempt.launch.worktree, "argv": attempt.launch.argv,
        "sourcePaths": attempt.launch.source_paths, "timeoutSeconds": attempt.launch.timeout_seconds,
        "assetSha256": attempt.asset_sha256});
    let input = serde_json::to_vec(
        &json!({"asset": STANDARD.encode(ASSET), "assetSha256": attempt.asset_sha256, "request": request}),
    )?;
    let mut command = host_command(
        &attempt.launch.environment,
        &["python3".into(), "-c".into(), BOOTSTRAP.into()],
    )?;
    let output = execute(
        &mut command,
        &input,
        Duration::from_secs(CONTROL_SECONDS),
        &mut |_| Ok(()),
    )?;
    ensure!(
        output.exit_code == Some(0),
        "check preparation failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let value: Value = serde_json::from_slice(&output.stdout).context("decode preparation")?;
    prepared_from_value(attempt, value)
}

fn prepared_from_value(attempt: &Attempt, value: Value) -> Result<PreparedAttempt> {
    let field = |key: &str| -> Result<String> {
        Ok(value
            .get(key)
            .and_then(Value::as_str)
            .with_context(|| format!("missing preparation {key}"))?
            .to_owned())
    };
    let prepared = PreparedAttempt {
        attempt: attempt.clone(),
        attempt_dir: field("attemptDir")?,
        asset_path: field("assetPath")?,
        python: field("python")?,
        request_sha256: field("requestSha256")?,
        host: value
            .get("host")
            .cloned()
            .context("missing observed host")?,
        worktree: field("worktree")?,
        preparation: value.clone(),
    };
    for path in [
        &prepared.attempt_dir,
        &prepared.asset_path,
        &prepared.python,
        &prepared.worktree,
    ] {
        ensure!(
            Path::new(path).is_absolute() && !path.contains('\0'),
            "invalid prepared host path"
        );
    }
    ensure!(
        value["attemptId"] == attempt.attempt_id
            && prepared
                .attempt_dir
                .ends_with(&format!("/{}", attempt.attempt_id)),
        "preparation attempt identity mismatch"
    );
    ensure!(
        prepared.request_sha256.len() == 64
            && prepared
                .request_sha256
                .bytes()
                .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase()),
        "invalid prepared request digest"
    );
    ensure!(
        prepared
            .asset_path
            .ends_with(&format!("/{}.py", attempt.asset_sha256)),
        "prepared asset hash path mismatch"
    );
    Ok(prepared)
}

/// Recover a lost preparation response using only the original host-local attempt.
/// This path neither installs assets nor prepares or launches a command.
fn read_original(attempt: &Attempt) -> Result<Vec<u8>> {
    ensure!(
        attempt.asset_sha256.len() == 64
            && attempt
                .asset_sha256
                .bytes()
                .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase()),
        "Original helper digest unavailable; attempt retained without execution"
    );
    const READ_ONLY: &str = r#"
import os,sys,pwd,stat,json,hashlib
p=json.load(sys.stdin)
root=os.path.join(pwd.getpwuid(os.getuid()).pw_dir,'.local','state','t3oxide','check-evidence')
path=root+'/assets/'+p['assetSha256']+'.py'
a=root+'/attempts/'+p['attemptId']
for d in (root,root+'/assets',root+'/attempts',a):
 s=os.lstat(d)
 if os.path.realpath(d)!=d or not stat.S_ISDIR(s.st_mode) or s.st_uid!=os.getuid() or stat.S_IMODE(s.st_mode)!=0o700: raise RuntimeError('unsafe retained state')
fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
with os.fdopen(fd,'rb') as f:
 s=os.fstat(f.fileno())
 if not stat.S_ISREG(s.st_mode) or s.st_uid!=os.getuid() or stat.S_IMODE(s.st_mode)!=0o600: raise RuntimeError('unsafe retained asset')
 data=f.read(65537)
 if len(data)>65536 or hashlib.sha256(data).hexdigest()!=p['assetSha256']: raise RuntimeError('retained asset mismatch')
os.execv(sys.executable,[sys.executable,path,'read','--attempt-dir',a,'--max-bytes','49152'])
"#;
    let input = serde_json::to_vec(
        &json!({"attemptId":attempt.attempt_id,"assetSha256":attempt.asset_sha256}),
    )?;
    let mut command = host_command(
        &attempt.launch.environment,
        &["python3".into(), "-c".into(), READ_ONLY.into()],
    )?;
    let output = execute(
        &mut command,
        &input,
        Duration::from_secs(CONTROL_SECONDS),
        &mut |_| Ok(()),
    )?;
    ensure!(
        output.exit_code == Some(0),
        "Original attempt cannot be read; no preparation or execution retried"
    );
    Ok(output.stdout)
}

/// Inspects a lost preparation response with the originally pinned helper, without launch.
pub fn recover(attempt: &Attempt) -> Result<(PreparedAttempt, Evidence)> {
    let bytes = read_original(attempt)?;
    let envelope: Value = serde_json::from_slice(&bytes)?;
    let prepared = prepared_from_value(
        attempt,
        json!({"attemptId":attempt.attempt_id,
        "attemptDir":envelope["attemptDir"],"requestSha256":envelope["requestSha256"],
        "host":envelope["binding"]["host"]["hostname"],"worktree":envelope["binding"]["worktree"],
        "python":envelope["binding"]["python"]["path"],"assetPath":envelope["binding"]["asset"]["path"],
        "recoveredPreparationResponse":true}),
    )?;
    let evidence = verify(&prepared, bytes)?;
    Ok((prepared, evidence))
}

fn qualify_layout_owner(rex: &RexContext) -> Result<()> {
    let mut command = rex.command();
    command.args(["whoami", "--json"]);
    let output = execute(&mut command, &[], Duration::from_secs(10), &mut |_| Ok(()))?;
    ensure!(output.exit_code == Some(0), "Rex layout owner unavailable");
    let value: Value = serde_json::from_slice(&output.stdout)?;
    ensure!(
        value["principal"]["transport"] == "unix"
            && value["endpoint"]
                .as_str()
                .is_some_and(|endpoint| endpoint.starts_with("unix://")),
        "Rex server execution host is not qualified as local; no pane or workload was launched"
    );
    let mut inspect = rex.command();
    inspect.args([
        "block",
        "inspect",
        "--session",
        &rex.session,
        &rex.block,
        "--json",
    ]);
    let output = execute(&mut inspect, &[], Duration::from_secs(10), &mut |_| Ok(()))?;
    ensure!(
        output.exit_code == Some(0),
        "Original Rex pane is unavailable; no workload launched"
    );
    let value: Value = serde_json::from_slice(&output.stdout)?;
    ensure!(
        value["block"]["block_id"] == rex.block,
        "Rex pane identity changed; no workload launched"
    );
    Ok(())
}

/// Close only a newly created terminal with an observed finished process.
/// Call after the controller durably retains its evidence and unique output.
pub fn close_finished(
    prepared: &PreparedAttempt,
    observations: &[Value],
) -> Result<Option<String>> {
    let rex = &prepared.attempt.launch.rex;
    qualify_layout_owner(rex)?;
    let mut ids = std::collections::BTreeSet::new();
    for observation in observations {
        for block in observation["block_ids"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(Value::as_str)
        {
            if block != rex.block {
                ids.insert(block.to_owned());
            }
        }
    }
    ensure!(
        ids.len() <= 1,
        "Rex creation IDs are ambiguous; panes retained"
    );
    let Some(id) = ids.into_iter().next() else {
        return Ok(None);
    };
    let mut inspect = rex.command();
    inspect.args([
        "block",
        "call",
        "--session",
        &rex.session,
        "--block",
        &id,
        "process",
    ]);
    let output = execute(&mut inspect, &[], Duration::from_secs(10), &mut |_| Ok(()))?;
    ensure!(
        output.exit_code == Some(0),
        "Task pane process state unavailable; retained"
    );
    let value: Value = serde_json::from_slice(&output.stdout)?;
    ensure!(
        value.get("child").is_some_and(Value::is_null)
            && value.get("foreground").is_some_and(Value::is_null)
            && value.get("last_exit").is_some_and(|value| !value.is_null()),
        "Task pane has no verified final process state; retained"
    );
    let mut close = rex.command();
    close.args(["block", "close", "--session", &rex.session, &id]);
    let output = execute(&mut close, &[], Duration::from_secs(10), &mut |_| Ok(()))?;
    ensure!(
        output.exit_code == Some(0),
        "Task pane close was not acknowledged; original IDs retained"
    );
    Ok(Some(id))
}

fn helper_argv(prepared: &PreparedAttempt, mode: &str) -> Vec<String> {
    vec![
        prepared.python.clone(),
        prepared.asset_path.clone(),
        mode.into(),
        "--attempt-dir".into(),
        prepared.attempt_dir.clone(),
    ]
}

/// Launches once; checkpoint promptly retains each Rex JSON creation observation.
/// Always attempts exactly one evidence read, including after launch/checkpoint errors.
pub fn run(
    prepared: &PreparedAttempt,
    mut checkpoint: impl FnMut(&Value) -> Result<()>,
) -> Completion {
    let launch = (|| -> Result<ProcessObservation> {
        checkpoint(&json!({"phase": "launch-intent", "attemptId": prepared.attempt.attempt_id, "attemptDir": prepared.attempt_dir}))?;
        let launch = &prepared.attempt.launch;
        launch.rex.validate()?;
        let helper = helper_argv(prepared, "run");
        let host = host_command(&launch.environment, &helper)?;
        let mut command = launch.rex.command();
        command.args(["run", "--session", &launch.rex.session, "--block", &launch.rex.block, "--split", "below", "--focus=false", "--keep-open", "--wait", "--json", "--shell=none"]);
        // A remote worktree is never passed to the local Rex server. The companion
        // executes SSH from the inherited workspace's host, using its existing route.
        if launch.environment.ssh_destination.is_none() { command.args(["--cwd", &prepared.worktree]); }
        command.arg("--").arg(host.get_program()).args(host.get_args());
        execute(&mut command, &[], Duration::from_secs(launch.timeout_seconds + CLEANUP_SECONDS), &mut checkpoint)
    })().map_err(|error| format!("{error:#}"));
    let evidence = collect(prepared).map_err(|error| format!("{error:#}"));
    Completion { launch, evidence }
}

/// Reads the retained source-host attempt once; never installs or launches anything.
pub fn collect(prepared: &PreparedAttempt) -> Result<Evidence> {
    verify(prepared, read_original(&prepared.attempt)?)
}

fn verify(prepared: &PreparedAttempt, bytes: Vec<u8>) -> Result<Evidence> {
    ensure!(
        bytes.len() <= MAX_ENVELOPE_BYTES,
        "evidence envelope exceeds 48 KiB"
    );
    let envelope: Value = serde_json::from_slice(&bytes).context("invalid evidence JSON")?;
    ensure!(
        envelope["schema"] == "t3oxide-check-evidence-v1",
        "unsupported evidence schema"
    );
    ensure!(
        envelope["attemptId"] == prepared.attempt.attempt_id
            && envelope["requestSha256"] == prepared.request_sha256,
        "evidence request identity mismatch"
    );
    ensure!(
        envelope["request"]["target"] == prepared.attempt.launch.source
            && envelope["binding"]["host"]["hostname"] == prepared.host
            && envelope["binding"]["worktree"] == prepared.worktree,
        "evidence provenance mismatch"
    );
    ensure!(
        envelope["request"]["argv"] == json!(prepared.attempt.launch.argv)
            && envelope["request"]["timeoutSeconds"] == prepared.attempt.launch.timeout_seconds,
        "evidence invocation mismatch"
    );
    ensure!(
        envelope["attemptDir"] == prepared.attempt_dir,
        "evidence attempt path mismatch"
    );
    let request = &envelope["request"];
    ensure!(
        request["schema"] == "t3oxide-check-evidence-v1"
            && request["attemptId"] == prepared.attempt.attempt_id
            && request["worktree"] == prepared.worktree
            && request["sourcePaths"] == json!(prepared.attempt.launch.source_paths),
        "evidence request fields mismatch"
    );
    ensure!(
        request["assetSha256"] == prepared.attempt.asset_sha256
            && envelope["binding"]["asset"]["sha256"] == prepared.attempt.asset_sha256
            && envelope["binding"]["asset"]["path"] == prepared.asset_path
            && envelope["binding"]["python"]["path"] == prepared.python,
        "evidence asset binding mismatch"
    );
    ensure!(
        digest(&canonical_request(request)?) == prepared.request_sha256
            && envelope["binding"]["requestSha256"] == prepared.request_sha256,
        "immutable request digest mismatch"
    );
    let mut blobs = Vec::new();
    for blob in envelope["blobs"]
        .as_array()
        .context("missing evidence blobs")?
    {
        let payload = blob["dataBase64"]
            .as_str()
            .context("missing blob payload")?;
        let decoded = STANDARD.decode(payload).context("invalid blob base64")?;
        ensure!(
            blob["byteLength"].as_u64() == Some(decoded.len() as u64)
                && blob["sha256"].as_str() == Some(digest(&decoded).as_str()),
            "blob byte length/hash mismatch"
        );
        ensure!(
            matches!(blob["coverage"].as_str(), Some("full" | "excerpt")),
            "invalid blob coverage"
        );
        let offset = blob["offset"].as_u64().context("missing blob offset")?;
        let retained = blob["retainedLength"]
            .as_u64()
            .context("missing retained length")?;
        ensure!(
            offset
                .checked_add(decoded.len() as u64)
                .is_some_and(|end| end <= retained),
            "blob range exceeds retained bytes"
        );
        if blob["coverage"] == "full" {
            ensure!(
                offset == 0
                    && retained == decoded.len() as u64
                    && blob["originalLength"].as_u64() == Some(retained)
                    && blob["eof"] == true
                    && blob["retainedSha256"] == blob["sha256"]
                    && blob["originalSha256"] == blob["sha256"],
                "invalid full blob coverage"
            );
        }
        blobs.push(VerifiedBlob {
            metadata: blob.clone(),
            bytes: decoded,
        });
    }
    let provenance = json!({"source": envelope["request"]["target"], "host": envelope["binding"]["host"], "worktree": envelope["binding"]["worktree"], "attemptDir": prepared.attempt_dir, "assetSha256": prepared.attempt.asset_sha256});
    Ok(Evidence {
        envelope,
        exact_json: bytes,
        blobs,
        request_sha256: prepared.request_sha256.clone(),
        provenance,
    })
}

// Match the helper's ensure_ascii=True, sorted-key compact JSON exactly.
fn canonical_request(value: &Value) -> Result<Vec<u8>> {
    let serialized = serde_json::to_string(value)?;
    let mut ascii = String::with_capacity(serialized.len());
    for character in serialized.chars() {
        if u32::from(character) < 0x7f {
            ascii.push(character);
        } else {
            let mut units = [0; 2];
            for unit in character.encode_utf16(&mut units) {
                write!(&mut ascii, "\\u{unit:04x}")?;
            }
        }
    }
    Ok(ascii.into_bytes())
}

fn nonblocking(fd: std::os::fd::RawFd) -> Result<()> {
    // SAFETY: caller owns the live pipe descriptor; fcntl takes no pointers here.
    let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
    ensure!(
        flags >= 0,
        "read pipe flags: {}",
        std::io::Error::last_os_error()
    );
    // SAFETY: the same live descriptor is used; flags preserve its previous bits.
    ensure!(
        unsafe { libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK) } >= 0,
        "set nonblocking pipe: {}",
        std::io::Error::last_os_error()
    );
    Ok(())
}

fn drain(reader: &mut impl Read, bytes: &mut Vec<u8>) -> Result<bool> {
    let mut buffer = [0; 4096];
    // Bound each turn even if a producer continuously fills the pipe.
    for _ in 0..16 {
        match reader.read(&mut buffer) {
            Ok(0) => return Ok(true),
            Ok(n) => {
                ensure!(
                    bytes.len() + n <= MAX_PIPE_BYTES,
                    "subprocess output exceeded 64 KiB; attempt remains uncertain"
                );
                bytes.extend_from_slice(&buffer[..n]);
            }
            Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => break,
            Err(error) if error.kind() == std::io::ErrorKind::Interrupted => continue,
            Err(error) => return Err(error.into()),
        }
    }
    Ok(false)
}

fn execute(
    command: &mut Command,
    input: &[u8],
    timeout: Duration,
    checkpoint: &mut impl FnMut(&Value) -> Result<()>,
) -> Result<ProcessObservation> {
    ensure!(
        input.len() <= 256 * 1024,
        "subprocess input exceeds 256 KiB"
    );
    let mut child = command
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .context("start finite check transport")?;
    let result = (|| -> Result<ProcessObservation> {
        let stdin = child.stdin.take().context("missing stdin")?;
        let mut stdout = child.stdout.take().context("missing stdout")?;
        let mut stderr = child.stderr.take().context("missing stderr")?;
        nonblocking(stdin.as_raw_fd())?;
        nonblocking(stdout.as_raw_fd())?;
        nonblocking(stderr.as_raw_fd())?;
        let mut stdin = if input.is_empty() { None } else { Some(stdin) };
        let (mut written, mut scanned) = (0, 0);
        let (mut out, mut err) = (Vec::new(), Vec::new());
        let (mut out_eof, mut err_eof) = (false, false);
        let deadline = Instant::now() + timeout;
        loop {
            ensure!(
                Instant::now() < deadline,
                "finite check transport deadline; original attempt requires read-only recovery"
            );
            if let Some(pipe) = &mut stdin {
                match pipe.write(&input[written..]) {
                    Ok(n) => written += n,
                    Err(error)
                        if error.kind() == std::io::ErrorKind::WouldBlock
                            || error.kind() == std::io::ErrorKind::Interrupted => {}
                    Err(error) => return Err(error).context("write check request"),
                }
                if written == input.len() {
                    stdin = None;
                }
            }
            if !out_eof {
                out_eof = drain(&mut stdout, &mut out)?;
            }
            if !err_eof {
                err_eof = drain(&mut stderr, &mut err)?;
            }
            while let Some(offset) = out[scanned..].iter().position(|b| *b == b'\n') {
                let end = scanned + offset;
                if let Ok(value) = serde_json::from_slice::<Value>(&out[scanned..end]) {
                    checkpoint(&value)?;
                }
                scanned = end + 1;
            }
            if let Some(status) = child.try_wait()?
                && out_eof
                && err_eof
            {
                if scanned < out.len()
                    && let Ok(value) = serde_json::from_slice::<Value>(&out[scanned..])
                {
                    checkpoint(&value)?;
                }
                return Ok(ProcessObservation {
                    exit_code: status.code(),
                    signal: status.signal(),
                    stdout: out,
                    stderr: err,
                });
            }
            std::thread::sleep(Duration::from_millis(10));
        }
    })();
    if result.is_err() {
        // Kill only the owned transport process, never an uncertain Rex pane or
        // remote workload. Reaping itself has a finite grace period.
        let _ = child.kill();
        let deadline = Instant::now() + Duration::from_secs(2);
        while Instant::now() < deadline {
            if !matches!(child.try_wait(), Ok(None)) {
                break;
            }
            std::thread::sleep(Duration::from_millis(10));
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn interrupted_asset_publication_does_not_poison_the_hash_name() {
        let directory = tempfile::tempdir().unwrap();
        let root = directory.path().canonicalize().unwrap();
        std::fs::create_dir(root.join("assets")).unwrap();
        let publication = BOOTSTRAP
            .split("asset=base64.b64decode")
            .nth(1)
            .unwrap()
            .split("p['request']['assetSha256']")
            .next()
            .unwrap();
        let prelude = format!(
            "import os,stat,json,hashlib,tempfile,base64\nroot={}\np=json.loads({})\nasset=base64.b64decode",
            serde_json::to_string(&root.to_string_lossy()).unwrap(),
            serde_json::to_string(
                &serde_json::to_string(
                    &json!({"asset":STANDARD.encode(ASSET),"assetSha256":digest(ASSET.as_bytes())})
                )
                .unwrap()
            )
            .unwrap()
        );
        let script = format!("{prelude}{publication}");
        let interrupted = script.replace(
            "try: os.link(staged,path,follow_symlinks=False)",
            "try: raise OSError('fixture publication interruption')",
        );
        let mut command = Command::new("python3");
        command.args(["-c", &interrupted]);
        assert_ne!(
            execute(&mut command, &[], Duration::from_secs(5), &mut |_| Ok(()))
                .unwrap()
                .exit_code,
            Some(0)
        );
        let asset = root
            .join("assets")
            .join(format!("{}.py", digest(ASSET.as_bytes())));
        assert!(!asset.exists());
        let after_link = script.replace(
            "os.unlink(staged)",
            "raise OSError('fixture post-link interruption')",
        );
        let mut command = Command::new("python3");
        command.args(["-c", &after_link]);
        assert_ne!(
            execute(&mut command, &[], Duration::from_secs(5), &mut |_| Ok(()))
                .unwrap()
                .exit_code,
            Some(0)
        );
        assert!(
            asset.exists(),
            "published bytes survive the post-link interruption"
        );
        use std::os::unix::fs::MetadataExt as _;
        assert_eq!(std::fs::metadata(&asset).unwrap().nlink(), 2);
        let mut command = Command::new("python3");
        command.args(["-c", &script]);
        let result = execute(&mut command, &[], Duration::from_secs(5), &mut |_| Ok(())).unwrap();
        assert_eq!(
            result.exit_code,
            Some(0),
            "{}",
            String::from_utf8_lossy(&result.stderr)
        );
        assert_eq!(std::fs::read(asset).unwrap(), ASSET.as_bytes());
    }

    #[test]
    fn real_helper_return_is_verified_and_tampered_bytes_or_target_are_refused() {
        // Reuse the isolated Python fixture: no bootstrap, installed state,
        // real wrapper, Rex server or native endpoint is involved.
        let script = r#"
import runpy,json
m=runpy.run_path('tests/check_evidence_test.py')
t=m['EvidenceTests']()
t.setUp()
try:
 t.use_alternate_helper()
 value=t.run_and_read()
 print(json.dumps({'envelope':value,'preparation':t.prepared}))
finally: t.doCleanups()
"#;
        let output = Command::new("python3")
            .args(["-B", "-c", script])
            .current_dir(env!("CARGO_MANIFEST_DIR"))
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let fixture: Value = serde_json::from_slice(&output.stdout).unwrap();
        let envelope = fixture["envelope"].clone();
        let request = &envelope["request"];
        let mut attempt = Attempt::new(Launch {
            source: request["target"].clone(),
            environment: Environment {
                id: "fixture-profile".into(),
                label: "Fixture".into(),
                environment_id: "fixture-env".into(),
                ssh_destination: None,
                node: "/usr/bin/true".into(),
                adapter: "/fixture/adapter".into(),
                host_config: "/fixture/config".into(),
                web_url: None,
                enabled: true,
                default_for_new: false,
                allowed_provider_instance_ids: None,
            },
            worktree: request["worktree"].as_str().unwrap().into(),
            argv: serde_json::from_value(request["argv"].clone()).unwrap(),
            source_paths: serde_json::from_value(request["sourcePaths"].clone()).unwrap(),
            timeout_seconds: 5,
            rex: RexContext {
                executable: "/usr/bin/true".into(),
                session: "fixture-session".into(),
                block: "fixture-block".into(),
                server: None,
            },
        })
        .unwrap();
        attempt.attempt_id = request["attemptId"].as_str().unwrap().into();
        attempt.asset_sha256 = request["assetSha256"].as_str().unwrap().into();
        assert_ne!(attempt.asset_sha256, digest(ASSET.as_bytes()));
        let preparation = &fixture["preparation"];
        let prepared = PreparedAttempt {
            attempt,
            attempt_dir: envelope["attemptDir"].as_str().unwrap().into(),
            asset_path: preparation["assetPath"].as_str().unwrap().into(),
            python: preparation["python"].as_str().unwrap().into(),
            request_sha256: envelope["requestSha256"].as_str().unwrap().into(),
            host: preparation["host"].clone(),
            worktree: preparation["worktree"].as_str().unwrap().into(),
            preparation: preparation.clone(),
        };
        let evidence = verify(&prepared, serde_json::to_vec(&envelope).unwrap()).unwrap();
        assert_eq!(
            evidence.provenance["assetSha256"],
            prepared.attempt.asset_sha256
        );
        assert_eq!(evidence.envelope["command"]["exitCode"], 1);
        assert_eq!(evidence.envelope["wrapper"]["exitCode"], 75);
        assert!(
            evidence
                .blobs
                .iter()
                .any(|blob| blob.bytes == b"original\xff\x00\n")
        );
        let mut wrong_target = envelope.clone();
        wrong_target["request"]["target"]["threadId"] = json!("another-thread");
        assert!(verify(&prepared, serde_json::to_vec(&wrong_target).unwrap()).is_err());
        let mut wrong_bytes = envelope;
        wrong_bytes["blobs"][0]["dataBase64"] = json!("dGFtcGVyZWQ=");
        assert!(verify(&prepared, serde_json::to_vec(&wrong_bytes).unwrap()).is_err());
    }

    #[test]
    fn transport_enforces_output_bounds_and_observes_nonzero_exit_independently() {
        let mut command = Command::new("python3");
        command.args(["-c", "import sys; print('exact'); sys.exit(75)"]);
        let result = execute(&mut command, &[], Duration::from_secs(5), &mut |_| Ok(())).unwrap();
        assert_eq!(result.exit_code, Some(75));
        assert_eq!(result.stdout, b"exact\n");
        let mut noisy = Command::new("python3");
        noisy.args(["-c", "import sys; sys.stdout.write('x'*70000)"]);
        assert!(execute(&mut noisy, &[], Duration::from_secs(5), &mut |_| Ok(())).is_err());
    }
}
