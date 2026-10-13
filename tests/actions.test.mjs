import test from 'node:test';
import assert from 'node:assert/strict';
import { T3 } from '../adapter/t3.mjs';
import { Fault } from '../adapter/core.mjs';

const action = { id: 'stop-a', environmentId: 'env-a', threadId: 'thread-a', runId: 'run-a' };
function fixture() {
  const server = new T3({ environmentId: 'env-a', allowWrites: true }, () => {});
  const state = { reads: 0, calls: [], status: 'running', dispatch: async () => ({ sequence: 12 }) };
  server.connect = async () => {};
  server.bounded = async () => {
    state.reads++;
    return { projection: { thread: { id: 'thread-a' }, visibleTurnItems: [], runtimeRequests: [],
      runs: [{ id: 'run-a', status: state.status }] } };
  };
  server.rpc = { call(tag, payload) {
    state.calls.push({ tag, payload: structuredClone(payload) });
    return { promise: state.dispatch(), cancel() {} };
  }, close() {} };
  return { server, state };
}

test('concurrent same-ID Stop joins the pending operation before its preflight completes', async () => {
  const { server, state } = fixture();
  let complete;
  state.dispatch = () => new Promise(resolve => { complete = resolve; });
  try {
    const first = server.act('stop', action), second = server.act('stop', { ...action });
    await assert.rejects(server.act('stop', { ...action, runId: 'other' }), { code: 'invalid_request' });
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(state.reads, 1);
    assert.equal(state.calls.length, 1);
    complete({ sequence: 12 });
    assert.deepEqual(await first, { sequence: 12 });
    assert.deepEqual(await second, { sequence: 12 });
  } finally { server.close(); }
});

test('uncertain Stop retries its original payload after the active run has stopped', async () => {
  const { server, state } = fixture();
  try {
    state.dispatch = async () => { state.status = 'interrupted'; throw new Fault('timeout', 'Lost acknowledgement.', true); };
    await assert.rejects(server.act('stop', action), { uncertain: true });
    state.dispatch = async () => ({ sequence: 12 });
    assert.deepEqual(await server.act('stop', action), { sequence: 12 });
    assert.equal(state.reads, 1);
    assert.deepEqual(state.calls[0], state.calls[1]);
    assert.equal(state.calls[1].payload.holdQueue, true);
  } finally { server.close(); }
});

test('a retry connection failure cannot erase an earlier uncertain outcome', async () => {
  const { server, state } = fixture();
  try {
    state.dispatch = async () => { throw new Fault('disconnected', 'Lost channel.', true); };
    await assert.rejects(server.act('stop', action), { uncertain: true });
    server.connect = async () => { throw new Fault('authorization', 'Renew host credential.'); };
    await assert.rejects(server.act('stop', action), { code: 'authorization', uncertain: true });
    assert.ok(server.receipts.get(action.id).payload);
    server.connect = async () => {};
    state.status = 'interrupted';
    state.dispatch = async () => ({ sequence: 12 });
    assert.deepEqual(await server.act('stop', action), { sequence: 12 });
  } finally { server.close(); }
});
