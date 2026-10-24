#!/usr/bin/env node
import { resolve } from 'node:path';
import { homedir } from 'node:os';
import { pathToFileURL } from 'node:url';
import { realpathSync } from 'node:fs';
import { Fault, LIMIT, identifier, requireThat } from './core.mjs';
import { T3, readConfig } from './t3.mjs';

// Stop has reserved admission and its own serial lane. All other operations keep
// their input order; in particular no mutation can overtake another mutation.
const MAX_REGULAR = 8, MAX_STOP = 2, MAX_OUTPUT = 12;
const MAX_OUTPUT_BYTES = 4 * LIMIT.frame;
const NAVIGATION = new Set(['watch', 'history']);

export async function run(argv = process.argv.slice(2), {
  input = process.stdin, output = process.stdout,
  createBackend = (config, emit) => new T3(config, emit),
  onFailure = () => { process.exitCode = 1; },
} = {}) {
  const at = argv.indexOf('--config');
  const configPath = at >= 0 ? argv[at+1] : resolve(homedir(),'.config/t3oxide/host.json');
  requireThat(configPath, 'Missing --config path.');
  const [major,minor] = process.versions.node.split('.').map(Number);
  requireThat(major > 22 || major === 22 && minor >= 4, 'Node 22.4 or newer is required.', 'configuration');
  const config = await readConfig(configPath);
  let blocked = false, queued = [], queuedBytes = 0, ended = false;
  let backend, buffer = Buffer.alloc(0), navigation = 0;
  const lanes = { regular: { tail: Promise.resolve(), pending: 0, limit: MAX_REGULAR },
    stop: { tail: Promise.resolve(), pending: 0, limit: MAX_STOP } };
  function close() {
    if (ended) return;
    ended = true; ++navigation;
    queued = []; queuedBytes = 0; buffer = Buffer.alloc(0);
    backend?.close(); input.pause();
    input.off('data', receive); input.off('end', close); input.off('error', fail);
    output.off('drain', drain);
    process.off('SIGTERM', terminate); process.off('SIGINT', terminate);
  }
  function fail(error) {
    if (ended) return;
    close();
    // A dropped delta would silently corrupt the view. Close the pipe instead:
    // the client must reconnect and acquire a new snapshot and revision base.
    input.destroy(); output.destroy();
    onFailure(error);
  }
  function terminate() { close(); input.destroy(); output.destroy(); }
  function write(packet) {
    if (ended) return;
    const encoded = JSON.stringify({v:1,environmentId:config.environmentId,...packet})+'\n';
    const bytes = Buffer.byteLength(encoded);
    requireThat(bytes <= LIMIT.frame, 'Companion output exceeded its byte limit.', 'too_large');
    if (blocked) {
      requireThat(queued.length < MAX_OUTPUT && queuedBytes + bytes <= MAX_OUTPUT_BYTES,
        'Companion reader is stalled; reconnect for a fresh snapshot.', 'backpressure');
      queued.push(encoded); queuedBytes += bytes;
    } else blocked = !output.write(encoded);
  }
  function drain() {
    try {
      blocked = false;
      while (queued.length && !blocked) {
        const next = queued.shift(); queuedBytes -= Buffer.byteLength(next);
        blocked = !output.write(next);
      }
    } catch (error) { fail(error); }
  }
  function replyError(id, error) {
    write({id,error:{code:error.code ?? 'internal',
      message:error instanceof Fault ? error.message : 'Adapter failed. Inspect local setup; no automatic command retry was made.',
      uncertain:error.uncertain === true}});
  }
  output.on('drain', drain); output.on('error', fail);
  backend = createBackend(config, (event,data) => {
    try { write({event,data}); } catch (error) { fail(error); }
  });
  process.on('SIGTERM', terminate); process.on('SIGINT', terminate);
  if (argv.includes('--probe')) {
    try { write({id:'probe',result:await backend.probe()}); } finally { close(); }
    return { close };
  }
  async function handle(a, generation) {
    const superseded = () => generation !== null && generation !== navigation;
    try {
      if (superseded()) throw new Fault('cancelled','Navigation was superseded.');
      let result;
      switch (a.op) {
        case 'hello': result = await backend.probe(); break;
        case 'list': result = await backend.list(a.query ?? '', a); break;
        case 'models': result = await backend.models(a.threadId); break;
        case 'activity': result = await backend.activity(a.threadId, a.cursor); break;
        case 'inspect-item': result = await backend.inspectActivity(a); break;
        case 'watch': result = await backend.watch(a.threadId); break;
        case 'history': result = await backend.history(a.cursor); break;
        case 'send': case 'stop': case 'answer': case 'launch': case 'select-model': result = await backend.act(a.op,a); break;
        default: throw new Fault('invalid_request','Unsupported companion operation.');
      }
      if (superseded()) throw new Fault('cancelled','Navigation was superseded.');
      write({id:a.id,result:result ?? {}});
    } catch (error) {
      replyError(a.id, superseded() ? new Fault('cancelled','Navigation was superseded.') : error);
    }
  }
  function accept(a) {
    let id = null;
    try {
      id = identifier(a?.id); requireThat(a.v === 1, 'Unsupported companion protocol.', 'incompatible');
      if (a.op !== 'hello') requireThat(a.environmentId === config.environmentId, 'Environment identity mismatch.', 'identity_mismatch');
      const lane = a.op === 'stop' ? lanes.stop : lanes.regular;
      requireThat(lane.pending < lane.limit, 'Too many pending companion requests.', 'backpressure');
      let generation = null;
      if (NAVIGATION.has(a.op)) {
        if (a.op === 'watch') identifier(a.threadId, 'thread id');
        else requireThat(typeof a.cursor === 'string' && a.cursor.length > 0, 'Missing history cursor.');
        // Cancel at admission, before waiting for the normal lane. The backend
        // owns aborting the read and guarding installation of its stale result.
        generation = ++navigation;
        backend.cancelNavigation();
      }
      lane.pending++;
      lane.tail = lane.tail.then(() => ended ? undefined : handle(a, generation))
        .catch(fail).finally(() => { lane.pending--; });
    } catch (error) { replyError(id, error); }
  }
  function receive(chunk) {
    try {
      buffer = Buffer.concat([buffer,chunk]);
      let end;
      while (!ended && (end = buffer.indexOf(10)) >= 0) {
        requireThat(end <= LIMIT.input,'Companion input exceeds the byte limit.','too_large');
        const line = buffer.subarray(0,end); buffer = buffer.subarray(end+1);
        if (line.length) accept(JSON.parse(line.toString('utf8')));
      }
      requireThat(buffer.length <= LIMIT.input,'Unterminated companion input exceeds its limit.','too_large');
    } catch (error) { fail(error); }
  }
  input.on('data', receive); input.on('end', close); input.on('error', fail);
  return { close };
}
// The host installer selects a release through current/. Node resolves that
// symlink for import.meta.url but leaves argv[1] as the invoked client path.
if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
  run().catch(e => { process.stderr.write(`t3oxide: ${e instanceof Fault ? e.message : 'Could not initialize host configuration.'}\n`); process.exitCode = 1; });
}
