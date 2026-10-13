// Private host-local write-ahead records. The upstream command receipt remains
// the authority for acceptance; a prepared record is never an acknowledgement.
import { constants, openSync, closeSync, fstatSync, fsyncSync, readFileSync, writeFileSync,
  mkdirSync, lstatSync, readdirSync, linkSync, unlinkSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { Fault, LIMIT, hash, requireThat } from './core.mjs';

export const JOURNAL_SLOTS = 4096;
const RECORD_BYTES = LIMIT.input + 16384;
const HEX = /^[a-f0-9]{64}$/;
const privateOwner = s => (s.mode & 0o077) === 0 && (!process.getuid || s.uid === process.getuid());
function fault() { return new Fault('journal', 'The private operation journal is unavailable or invalid. Preserve it and repair host storage before retrying.', true); }

// Receipt results and replay are allowed only after this exact token authenticates
// against the verified environment. Failure handling may check for local evidence
// of uncertainty earlier. Installed T3 signs these claims; decoding is NOT authentication.
// Session IDs/expiry/signatures deliberately do not bind receipts, so renewal
// for the same subject works. Unknown token formats remain credential-isolated.
export function journalScope(config) {
  let principal = { credential: hash(config.token) };
  try {
    const parts = config.token.split('.');
    const claims = JSON.parse(Buffer.from(parts[0], 'base64url').toString('utf8'));
    if (parts.length === 2 && parts[1] && [1,2].includes(claims.v) && claims.kind === 'session' &&
        typeof claims.sub === 'string' && claims.sub.length && typeof claims.sid === 'string') {
      principal = { subject: claims.sub };
    }
  } catch { /* Unknown credentials cannot inherit another credential's records. */ }
  return hash({ environmentId: config.environmentId, uid: process.getuid?.() ?? null, principal });
}

export class Journal {
  constructor(directory, scope, { slots = JOURNAL_SLOTS } = {}) {
    this.directory = directory; this.scope = scope; this.slots = slots;
    requireThat(Number.isInteger(slots) && slots > 0 && slots <= JOURNAL_SLOTS, 'Invalid journal capacity.', 'configuration');
  }
  directoryFd() {
    try {
      mkdirSync(this.directory, { mode: 0o700 });
      const parent = openSync(dirname(this.directory), constants.O_RDONLY | constants.O_NOFOLLOW);
      try { fsyncSync(parent); } finally { closeSync(parent); }
    } catch (e) { if (e.code !== 'EEXIST') throw e; }
    const fd = openSync(this.directory, constants.O_RDONLY | constants.O_NOFOLLOW);
    const stat = fstatSync(fd);
    if (!stat.isDirectory() || !privateOwner(stat)) { closeSync(fd); throw fault(); }
    return fd;
  }
  read(path) {
    let fd;
    try {
      fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
      const stat = fstatSync(fd);
      if (!stat.isFile() || !privateOwner(stat) || stat.size > RECORD_BYTES) throw fault();
      return JSON.parse(readFileSync(fd, 'utf8'));
    } catch (e) { if (e.code === 'ENOENT') return null; throw fault(); }
    finally { if (fd !== undefined) closeSync(fd); }
  }
  validate(record) {
    if (record?.v !== 1 || !HEX.test(record.key) || !HEX.test(record.scope) || !HEX.test(record.fingerprint) ||
        !['orchestration.dispatchCommand', 'orchestration.launchThread'].includes(record.tag) ||
        typeof record.payload?.commandId !== 'string' || hash(record.payload.commandId) !== record.key ||
        hash({ tag: record.tag, payload: record.payload }) !== record.payloadHash) throw fault();
  }
  // Hard-link publication is atomic and never replaces a competing helper's
  // record. Sync file BEFORE publication and directory BEFORE network dispatch.
  // There is no lock to strand on SIGKILL and no read/modify/write lost update.
  publish(path, record, directoryFd) {
    const bytes = JSON.stringify(record);
    requireThat(Buffer.byteLength(bytes) <= RECORD_BYTES, 'Operation exceeds the durable journal byte limit.', 'too_large');
    const temp = join(this.directory, `.prepare-${process.pid}-${randomUUID()}`);
    let fd;
    try {
      fd = openSync(temp, constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW, 0o600);
      writeFileSync(fd, bytes); fsyncSync(fd); closeSync(fd); fd = undefined;
      try { linkSync(temp, path); } catch (e) { if (e.code !== 'EEXIST') throw e; }
      fsyncSync(directoryFd);
    } finally {
      if (fd !== undefined) closeSync(fd);
      try { unlinkSync(temp); } catch (e) { if (e.code !== 'ENOENT') throw e; }
    }
  }
  cleanup() {
    // Only incomplete publication scratch files of dead processes are removed.
    // Published payloads and receipts are never evicted, even at capacity.
    let pending = 0;
    for (const name of readdirSync(this.directory)) {
      const m = /^\.prepare-(\d+)-[a-f0-9-]+$/.exec(name);
      if (!m) continue;
      let alive = true;
      try { process.kill(Number(m[1]), 0); } catch (e) { if (e.code === 'ESRCH') alive = false; }
      if (alive) { pending++; continue; }
      const path = join(this.directory, name);
      let stat;
      try { stat = lstatSync(path); } catch (e) { if (e.code === 'ENOENT') continue; throw e; }
      if (!stat.isFile() || !privateOwner(stat)) throw fault();
      try { unlinkSync(path); } catch (e) { if (e.code !== 'ENOENT') throw e; }
    }
    requireThat(pending < 32, 'Too many concurrent journal writers. Retry the same operation later.', 'backpressure');
  }
  access(id, fingerprint, prepared) {
    let fd;
    try {
      fd = this.directoryFd();
      const key = hash(id), start = parseInt(key.slice(0,8),16) % this.slots;
      for (let n = 0; n < this.slots; n++) {
        const path = join(this.directory, `${(start+n)%this.slots}.json`);
        let record = this.read(path);
        if (!record && prepared) {
          this.cleanup();
          this.publish(path, { v: 1, key, scope: this.scope, fingerprint,
            tag: prepared.tag, payload: prepared.payload,
            payloadHash: hash({ tag: prepared.tag, payload: prepared.payload }) }, fd);
          record = this.read(path);
        }
        if (!record) return null;
        this.validate(record);
        if (record.key !== key) continue;
        requireThat(record.scope === this.scope, 'This operation belongs to a different account or environment.', 'identity_mismatch');
        requireThat(record.fingerprint === fingerprint, 'An operation id cannot be reused with different content.', 'invalid_request');
        const ack = this.read(path + '.ack');
        if (ack && (ack.v !== 1 || ack.key !== key || ack.scope !== record.scope || ack.fingerprint !== fingerprint ||
            ack.payloadHash !== record.payloadHash || !(record.tag === 'orchestration.launchThread' ?
              typeof ack.result?.threadId === 'string' : Number.isSafeInteger(ack.result?.sequence) && ack.result.sequence >= 0))) throw fault();
        // A second helper can observe publication before its writer fsyncs the
        // directory. It must establish durability too before dispatch/reply.
        fsyncSync(fd);
        return { ...record, path, ...(ack ? { result: ack.result } : {}) };
      }
      requireThat(!prepared, 'The operation journal is full. Existing retries remain available; preserve the journal and inspect host storage.', 'backpressure');
      return null;
    } catch (e) { if (e instanceof Fault) throw e; throw fault(); }
    finally { if (fd !== undefined) closeSync(fd); }
  }
  acknowledge(record, result) {
    let fd;
    try {
      fd = this.directoryFd(); this.cleanup();
      this.publish(record.path + '.ack', { v: 1, key: record.key, scope: record.scope,
        fingerprint: record.fingerprint, payloadHash: record.payloadHash, result }, fd);
      const saved = this.read(record.path + '.ack');
      if (hash(saved?.result) !== hash(result)) throw fault();
    } catch (e) { if (e instanceof Fault) throw e; throw fault(); }
    finally { if (fd !== undefined) closeSync(fd); }
  }
}
