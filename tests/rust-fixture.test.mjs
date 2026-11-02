import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createFixture } from './rust_fixture.mjs';
import { modelView } from '../adapter/models.mjs';

async function fixture(t, extra = {}) {
  const directory = await mkdtemp(join(tmpdir(), 't3oxide-rust-fixture-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const events = [];
  const config = { baseUrl: 'http://127.0.0.1:1', environmentId: 'fixture-env', token: 'fixture-only',
    allowWrites: true, journalDirectory: join(directory, 'operations-v1'),
    fixture: { stateFile: join(directory, 'fixture-state.json'), ...extra } };
  const backend = createFixture(config, (event, data) => events.push({ event, data }));
  t.after(() => backend.close());
  return { backend, events, config };
}

test('Rust terminal fixture uses real companion projections, streams, requests, history and catalogs', async t => {
  const { backend, events } = await fixture(t);
  const hello = await backend.probe();
  assert.equal(hello.protocol, 1);
  assert.equal(hello.features.inclusiveThreadList, true);
  const list = await backend.list('', { includeChildren: true, grouped: false, lifecycle: 'all' });
  assert.equal(list.threads.length, 7);
  await backend.watch('thread-a');
  await new Promise(resolve => setTimeout(resolve, 450));
  assert.equal(backend.current.live, true);
  assert.ok(events.some(e => e.event === 'delta'));
  assert.match(backend.current.items.at(-1).text, /Streaming update 3/);
  assert.equal((await backend.models('thread-a')).models.length, 2);
  assert.equal((await backend.projects({})).projects.length, 2);
  assert.equal((await backend.activity('thread-a')).activity.items.length, 1);
  await backend.history(backend.current.historyCursor);
  assert.equal(backend.current.historical, true);
  assert.equal(backend.current.items[0].text, 'Older fixture history page.');
  const request = modelView(await backend.bounded('thread-approval'), 'thread-approval').requests[0];
  const ack = await backend.act('answer', { v: 1, id: 'fixture-answer', op: 'answer', environmentId: 'fixture-env',
    threadId: 'thread-approval', requestId: request.id, revision: request.revision, decision: 'accept' });
  assert.equal(typeof ack.sequence, 'number');
  const review = await backend.reviewThread({ threadId: 'thread-review' });
  assert.match(review.diff, /new\(\)/);
});

test('Rust fixture persists native effects and journals exact retries after lost acknowledgement and restart', async t => {
  const { backend, config } = await fixture(t, { dropAckOnce: true, stream: false });
  const action = { v: 1, id: 'exact-fixture-id', op: 'send', environmentId: 'fixture-env',
    threadId: 'thread-a', message: 'Exact unchanged message', mode: 'auto' };
  await assert.rejects(backend.act('send', action), error => error.uncertain === true);
  const accepted = await backend.act('send', action);
  backend.close();
  const resumed = createFixture(config, () => {});
  try {
    assert.deepEqual(await resumed.act('send', action), accepted);
    assert.equal(JSON.parse(await readFile(config.fixture.stateFile, 'utf8')).effects, 1);
    await assert.rejects(resumed.act('send', { ...action, message: 'Changed' }), { code: 'invalid_request' });
  } finally { resumed.close(); }
});

test('Rust fixture performs a first-thread launch from native project defaults', async t => {
  const { backend } = await fixture(t, { stream: false });
  const defaults = await backend.launchDefaults({ projectId: 'project-empty' });
  const action = { v: 1, id: 'launch-operation', op: 'launch-project', environmentId: 'fixture-env',
    threadId: 'new-thread', newThreadId: 'new-thread', projectId: 'project-empty',
    workspaceRoot: defaults.workspaceRoot, defaultsRevision: defaults.revision,
    workspaceStrategy: defaults.workspaceStrategy, title: 'New fixture', message: 'New task' };
  assert.deepEqual(await backend.act('launch-project', action), { threadId: 'new-thread' });
  assert.equal((await backend.bounded('new-thread')).projection.thread.projectId, 'project-empty');
});
