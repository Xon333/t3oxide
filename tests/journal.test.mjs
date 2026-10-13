import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm, chmod, readdir, writeFile, symlink, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawn } from 'node:child_process';
import { Journal, journalScope } from '../adapter/journal.mjs';
import { T3 } from '../adapter/t3.mjs';
import { Fault, hash, requests } from '../adapter/core.mjs';

const scope = hash({ principal: 'fixture', environment: 'env-a' });
const action = { id: 'durable-stop', environmentId: 'env-a', threadId: 'thread-a', runId: 'run-a' };
const prepared = { tag: 'orchestration.dispatchCommand', payload: {
  type: 'run.interrupt', commandId: action.id, threadId: 'thread-a', runId: 'run-a', holdQueue: true } };
async function directory(t) {
  const root = await mkdtemp(join(tmpdir(), 't3oxide-journal-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  return join(root, 'operations-v1');
}
function backend(path, state, otherScope = scope) {
  const journal = new Journal(path, otherScope);
  const server = new T3({ environmentId: 'env-a', allowWrites: true }, () => {}, { journal });
  server.connect = async () => {};
  server.descriptor = { capabilities: { serverResolvedCommandContext: true } };
  server.bounded = async () => {
    state.reads++;
    return { projection: structuredClone(state.projection) };
  };
  server.rpc = { close() {}, call(tag, payload) {
    state.calls.push(structuredClone({ tag, payload }));
    return { promise: state.dispatch(tag, payload), cancel() {} };
  } };
  return server;
}
function state() {
  return { reads: 0, calls: [], dispatch: async () => ({ sequence: 12 }),
    projection: { thread: { id: 'thread-a' }, visibleTurnItems: [], turnItems: [], runtimeRequests: [],
      runs: [{ id: 'run-a', status: 'running' }] } };
}

test('restart replays prepared exact Stop, never derives a new run from fresh state', async t => {
  const path = await directory(t), s = state();
  let server = backend(path, s);
  s.dispatch = async () => { s.projection.runs = [{ id: 'run-b', status: 'running' }]; throw new Fault('disconnected', 'Lost acknowledgement.', true); };
  await assert.rejects(server.act('stop', action), { uncertain: true });
  server.close(); server = backend(path, s);
  s.dispatch = async () => ({ sequence: 12 });
  assert.deepEqual(await server.act('stop', action), { sequence: 12 });
  assert.equal(s.reads, 1);
  assert.deepEqual(s.calls[0], s.calls[1]);
  assert.equal(s.calls[1].payload.runId, 'run-a');
  server.close();
});

test('durable accepted answer survives restart without redispatch or new control validation', async t => {
  const path = await directory(t), s = state();
  s.projection.turnItems = [{ id: 'question', type: 'user_input_request', requestId: 'request-a',
    questions: [{ id: 'q', header: 'Fixture', required: true }] }];
  s.projection.runtimeRequests = [{ id: 'request-a', status: 'pending', responseCapability: { type: 'live' } }];
  const a = { id: 'answer-a', environmentId: 'env-a', threadId: 'thread-a', requestId: 'request-a',
    revision: requests(s.projection)[0].revision, answers: { q: 'Original answer' } };
  let server = backend(path, s);
  const result = await server.act('answer', a);
  server.close(); s.projection.runtimeRequests[0].status = 'resolved';
  server = backend(path, s);
  assert.deepEqual(await server.act('answer', a), result);
  assert.equal(s.calls.length, 1); assert.equal(s.reads, 1);
  for (const changes of [{ answers: { q: 'Other' } }, { requestId: 'new-request' }, { revision: 'new-revision' }, { threadId: 'other' }]) {
    await assert.rejects(server.act('answer', { ...a, ...changes }), { code: 'invalid_request' });
  }
  server.close();
});

test('unacknowledged answer retry preserves exact answers and request after helper replacement', async t => {
  const path = await directory(t), s = state();
  s.projection.turnItems = [{ id: 'question', type: 'user_input_request', requestId: 'request-a',
    questions: [{ id: 'q', header: 'Fixture' }] }];
  s.projection.runtimeRequests = [{ id: 'request-a', status: 'pending', responseCapability: { type: 'message' } }];
  const a = { id: 'answer-a', environmentId: 'env-a', threadId: 'thread-a', requestId: 'request-a',
    revision: requests(s.projection)[0].revision, answers: { q: ['Original', 'answer'] } };
  let server = backend(path, s);
  s.dispatch = async () => { s.projection.runtimeRequests[0].status = 'resolved'; throw new Fault('timeout', 'Lost ACK', true); };
  await assert.rejects(server.act('answer', a), { uncertain: true }); server.close();
  server = backend(path, s); s.dispatch = async () => ({ sequence: 12 });
  assert.deepEqual(await server.act('answer', a), { sequence: 12 });
  assert.deepEqual(s.calls[0], s.calls[1]); assert.equal(s.reads, 1); server.close();
});

test('ack journal failure cannot return success and next helper recovers original upstream receipt', async t => {
  const path = await directory(t), s = state();
  let server = backend(path, s);
  server.journal.acknowledge = () => { throw new Error('Injected disk failure'); };
  await assert.rejects(server.act('stop', action), { uncertain: true }); server.close();
  server = backend(path, s); s.projection.runs = [];
  assert.deepEqual(await server.act('stop', action), { sequence: 12 });
  assert.deepEqual(s.calls[0], s.calls[1]); server.close();
});

test('private permissions, symlinks, corrupt records and environment/account mismatch fail closed', async t => {
  const path = await directory(t), fingerprint = hash(action), journal = new Journal(path, scope);
  const record = journal.access(action.id, fingerprint, prepared);
  assert.equal((await stat(path)).mode & 0o777, 0o700);
  assert.equal((await stat(record.path)).mode & 0o777, 0o600);
  assert.throws(() => new Journal(path, hash('different account')).access(action.id, fingerprint), { code: 'identity_mismatch' });
  await chmod(record.path, 0o644);
  assert.throws(() => journal.access(action.id, fingerprint), { code: 'journal', uncertain: true });
  await chmod(record.path, 0o600);
  await writeFile(record.path, '{broken');
  assert.throws(() => journal.access(action.id, fingerprint), { code: 'journal' });
  await rm(record.path); await symlink('/dev/null', record.path);
  assert.throws(() => journal.access(action.id, fingerprint), { code: 'journal' });
  await chmod(path, 0o755);
  assert.throws(() => journal.access(action.id, fingerprint), { code: 'journal' });
});

test('full journal preserves pending and accepted IDs and permits only their exact retries', async t => {
  const path = await directory(t), journal = new Journal(path, scope, { slots: 2 });
  const first = journal.access(action.id, hash(action), prepared);
  const second = { ...prepared, payload: { ...prepared.payload, commandId: 'other' } };
  const accepted = journal.access('other', hash('other'), second);
  journal.acknowledge(accepted, { sequence: 13 });
  assert.throws(() => journal.access('third', hash('third'), { ...prepared, payload: { ...prepared.payload, commandId: 'third' } }), { code: 'backpressure' });
  assert.equal(journal.access(action.id, hash(action)).payloadHash, first.payloadHash);
  assert.deepEqual(journal.access('other', hash('other')).result, { sequence: 13 });
  assert.throws(() => journal.access('other', hash('changed')), { code: 'invalid_request' });
});

test('renewed signed session preserves principal scope; changed principal/environment/opaque token cannot inherit it', () => {
  const token = claims => Buffer.from(JSON.stringify({ v: 1, kind: 'session', sub: 'same-account', sid: 'session-a', ...claims })).toString('base64url') + '.signature';
  const config = { environmentId: 'env-a', token: token({}) };
  const original = journalScope(config);
  assert.equal(journalScope({ ...config, token: token({ sid: 'session-b', exp: 2000 }) }), original);
  assert.notEqual(journalScope({ ...config, token: token({ sub: 'other-account' }) }), original);
  assert.notEqual(journalScope({ ...config, environmentId: 'env-b' }), original);
  assert.notEqual(journalScope({ ...config, token: 'opaque-one' }), journalScope({ ...config, token: 'opaque-two' }));
});

test('parallel helper processes publish one original payload, converge on a receipt and reject conflicts', async t => {
  const path = await directory(t), fingerprint = hash(action);
  const module = new URL('../adapter/journal.mjs', import.meta.url).href;
  const source = `import { Journal } from ${JSON.stringify(module)};
    const [path,scope,fingerprint,prepared] = process.argv.slice(1);
    const journal = new Journal(path,scope);
    const record = journal.access('durable-stop',fingerprint,JSON.parse(prepared));
    journal.acknowledge(record,{sequence:12});
    console.log(JSON.stringify({payload:record.payload,result:journal.access('durable-stop',fingerprint).result}));`;
  const run = (fp, payload) => new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ['--input-type=module', '-e', source, path, scope, fp, JSON.stringify(payload)]);
    let out = '', err = '';
    child.stdout.on('data', b => { out += b; }); child.stderr.on('data', b => { err += b; });
    child.on('error', reject); child.on('exit', code => resolve({ code, out, err }));
  });
  const results = await Promise.all(Array.from({ length: 8 }, (_, i) => run(fingerprint,
    { ...prepared, payload: { ...prepared.payload, runId: `preflight-${i}` } })));
  for (const result of results) assert.equal(result.code, 0, result.err);
  const payloads = results.map(r => JSON.parse(r.out));
  for (const payload of payloads) assert.deepEqual(payload, payloads[0]);
  const conflict = await run(hash('changed'), prepared);
  assert.notEqual(conflict.code, 0); assert.match(conflict.err, /different content/);
  const files = await readdir(path);
  assert.equal(files.filter(x => x.endsWith('.json')).length, 1);
  assert.equal(files.filter(x => x.endsWith('.ack')).length, 1);
  assert.ok(!files.some(x => x.startsWith('.prepare-')));
});

test('journal creation/storage failure prevents the first dispatch', async t => {
  const path = await directory(t), s = state();
  await writeFile(path, 'not a directory', { mode: 0o600 });
  const server = backend(path, s);
  await assert.rejects(server.act('stop', action), { code: 'journal' });
  assert.equal(s.calls.length, 0); server.close();
});


test('fresh-helper authentication failure preserves prior uncertainty without returning its cached success', async t => {
  const path = await directory(t), s = state();
  let server = backend(path, s);
  await server.act('stop', action); server.close();
  server = backend(path, s);
  server.connect = async () => { throw new Fault('authorization', 'Renew this host credential.'); };
  await assert.rejects(server.act('stop', action), { code: 'authorization', uncertain: true });
  assert.equal(s.calls.length, 1); server.close();
});
