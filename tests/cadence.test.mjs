import test from 'node:test';
import assert from 'node:assert/strict';
import { setImmediate as turn } from 'node:timers/promises';
import { T3, SCHEDULING } from '../adapter/t3.mjs';
import { view } from '../adapter/core.mjs';

const row = text => ({ position: 0, sourceThreadId: 'thread-a', sourceItemId: 'item-a',
  item: { id: 'item-a', threadId: 'thread-a', type: 'assistant_message', messageId: 'message-a', text } });
const snapshot = (text = 'Initial', sequence = 10) => ({ snapshotSequence: sequence,
  projection: { thread: { id: 'thread-a', title: 'Fixture' }, runs: [], runtimeRequests: [], visibleTurnItems: [row(text)] } });

function fixture(t) {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'], now: 1000 });
  const frames = [], reads = [];
  const server = new T3({ environmentId: 'env-a' }, (event, data) => frames.push({ event, data, at: Date.now() }),
    { now: () => Date.now() });
  server.current = { ...view(snapshot(), 'thread-a'), state: 'live', live: true, historical: false };
  server.snapshotSequence = 10; server.lastStreamSequence = 10;
  server.watchId = 'thread-a'; server.synchronized = true;
  server.bounded = async () => { reads.push(Date.now()); return snapshot('Refreshed', 500); };
  t.after(() => server.close());
  return { server, frames, reads, advance: async ms => { t.mock.timers.tick(ms); await turn(); } };
}

function message(server, sequence) {
  server.onEvent({ kind: 'event', sequence, event: { threadId: 'thread-a', type: 'message.updated',
    payload: { id: 'message-a', text: `Token ${sequence}`, streaming: true } } });
}
function structure(server, sequence) {
  server.onEvent({ kind: 'event', sequence, event: { threadId: 'thread-a', type: 'run.updated',
    payload: { id: 'run-a', status: 'running' } } });
}

test('quiet presentation has no recurring view or snapshot work', async t => {
  const { server, frames, reads, advance } = fixture(t);
  await advance(60_000);
  assert.equal(frames.length, 0); assert.equal(reads.length, 0);
  assert.equal(server.emitTimer, null); assert.equal(server.refreshTimer, null);
  // RPC liveness heartbeats belong to wire.mjs and are intentionally separate.
});

test('continuous text updates publish useful latest frames at bounded cadence without a snapshot read', async t => {
  const { server, frames, reads, advance } = fixture(t);
  server.flushView(); frames.length = 0;
  for (let i = 0; i < 80; i++) {
    message(server, 11 + i);
    await advance(10);
  }
  assert.equal(frames.length, 800 / SCHEDULING.frameMs);
  assert.equal(frames[0].at, 1000 + SCHEDULING.frameMs);
  assert.equal(frames[0].data.upserts[0].text, 'Token 18');
  assert.equal(frames.at(-1).data.upserts[0].text, 'Token 90');
  for (let i = 1; i < frames.length; i++) {
    assert.equal(frames[i].at - frames[i - 1].at, SCHEDULING.frameMs);
    assert.equal(frames[i].data.baseRevision, frames[i - 1].data.revision);
  }
  assert.equal(reads.length, 0);
  const count = frames.length;
  await advance(60_000);
  assert.equal(frames.length, count); assert.equal(server.emitTimer, null);
});

test('a first structural change reaches presentation within the two explicit scheduling windows', async t => {
  const { server, frames, reads, advance } = fixture(t);
  server.flushView(); frames.length = 0;
  structure(server, 11);
  await advance(SCHEDULING.structureMs - 1);
  assert.equal(reads.length, 0);
  await advance(1);
  assert.deepEqual(reads, [1000 + SCHEDULING.structureMs]);
  assert.equal(frames.length, 0);
  await advance(SCHEDULING.frameMs);
  assert.equal(frames.length, 1);
  assert.equal(frames[0].at, 1000 + SCHEDULING.structureMs + SCHEDULING.frameMs);
  assert.equal(frames[0].data.upserts[0].text, 'Refreshed');
});

test('sustained structural events are rate bounded rather than a new snapshot per frame', async t => {
  const { server, reads, advance } = fixture(t);
  // Returning the newest stream sequence makes every subsequent fixture event new.
  server.bounded = async () => { reads.push(Date.now()); return snapshot('Refreshed', server.lastStreamSequence); };
  for (let i = 0; i < 200; i++) {
    structure(server, 11 + i);
    await advance(10);
  }
  assert.equal(reads.length, 5);
  for (let i = 1; i < reads.length; i++) {
    assert.ok(reads[i] - reads[i - 1] >= SCHEDULING.refreshIntervalMs);
  }
  await advance(SCHEDULING.refreshIntervalMs);
  const count = reads.length;
  await advance(60_000);
  assert.equal(reads.length, count); assert.equal(server.refreshTimer, null);
});

test('a slow structural read admits only one follow-up and preserves newer text', async t => {
  const { server, reads, advance } = fixture(t);
  let finish;
  server.bounded = () => { reads.push(Date.now()); return new Promise(resolve => { finish = resolve; }); };
  structure(server, 11);
  await advance(SCHEDULING.structureMs);
  for (let i = 0; i < 50; i++) {
    structure(server, 12 + i); await advance(10);
  }
  message(server, 100);
  assert.equal(reads.length, 1); assert.equal(server.refreshAgain, true);
  finish(snapshot('Old snapshot text', 11)); await turn();
  assert.equal(server.current.items[0].text, 'Token 100');
  await advance(SCHEDULING.structureMs);
  assert.equal(reads.length, 2); assert.equal(server.refreshAgain, false);
  finish(snapshot('Token 100', 100)); await turn();
  await advance(60_000);
  assert.equal(reads.length, 2);
});

test('closing cancels scheduled frames and reads and ignores late emit requests', async t => {
  const { server, frames, reads, advance } = fixture(t);
  message(server, 11); structure(server, 12);
  server.close(); server.emitView(); server.scheduleRefresh();
  await advance(60_000);
  assert.equal(frames.length, 0); assert.equal(reads.length, 0);
  assert.equal(server.emitTimer, null); assert.equal(server.refreshTimer, null);
});

test('superseded structural read cannot install or reschedule an old page', async t => {
  const { server, frames, reads, advance } = fixture(t);
  let finish;
  server.bounded = () => { reads.push(Date.now()); return new Promise(resolve => { finish = resolve; }); };
  structure(server, 11); await advance(SCHEDULING.structureMs);
  structure(server, 12);
  server.cancelNavigation();
  server.current = { ...view(snapshot('Replacement', 200), 'thread-a'), live: false, state: 'synchronizing' };
  finish(snapshot('Obsolete page', 12)); await turn();
  await advance(60_000);
  assert.equal(server.current.items[0].text, 'Replacement');
  assert.equal(reads.length, 1);
  assert.ok(frames.every(frame => !JSON.stringify(frame).includes('Obsolete page')));
});
