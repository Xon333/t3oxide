import test from 'node:test';
import assert from 'node:assert/strict';
import { Rpc, connectSocket } from '../adapter/wire.mjs';

class Socket extends EventTarget {
  readyState = 1; sent = []; closes = 0; listeners = new Map();
  addEventListener(type, listener, options) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type).add(listener); super.addEventListener(type, listener, options);
  }
  removeEventListener(type, listener) {
    this.listeners.get(type)?.delete(listener); super.removeEventListener(type, listener);
  }
  send(data) { const frame = JSON.parse(data); this.sent.push(frame); this.onSend?.(frame); }
  // Deliberately no close event: a stalled closing handshake must not hold cleanup.
  close() { this.closes++; this.readyState = 3; }
  incoming(frame) { this.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(frame) })); }
  eof() { this.readyState = 3; this.dispatchEvent(new Event('close')); }
  listenerCount() { return [...this.listeners.values()].reduce((n, set) => n + set.size, 0); }
}
function setup(t) {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const socket = new Socket(), closed = [], rpc = new Rpc(socket, e => closed.push(e));
  t.after(() => rpc.close());
  return { socket, closed, rpc, tick: ms => t.mock.timers.tick(ms) };
}
function subscribe(rpc, callback = () => {}, timeout = 20000) {
  return rpc.call('orchestration.subscribeThread', { requestCompletionMarker: true }, callback, timeout);
}
function sync(socket, id = '1') {
  socket.incoming({ _tag: 'Chunk', requestId: id, values: [{ kind: 'synchronized' }] });
}
function clean(rpc, socket) {
  assert.equal(rpc.pending.size, 0);
  assert.equal(rpc.pingTimer, null); assert.equal(rpc.frameTimer, null);
  assert.equal(socket.listenerCount(), 0);
}

test('application Ping receives Pong and a synchronized idle subscription stays healthy', async t => {
  const { socket, rpc, closed, tick } = setup(t), values = [];
  socket.onSend = frame => { if (frame._tag === 'Ping') socket.incoming({ _tag: 'Pong' }); };
  const stream = subscribe(rpc, item => values.push(item)); sync(socket);
  for (let i = 0; i < 12; i++) tick(5000);
  assert.equal(socket.sent.filter(f => f._tag === 'Ping').length, 12);
  assert.deepEqual(values, [{ kind: 'synchronized' }]);
  assert.equal(rpc.closed, false); assert.equal(closed.length, 0);
  stream.cancel(); assert.equal(await stream.promise, null);
});

test('no-frame stall closes promptly, reports once, and preserves uncertain writes', async t => {
  const { socket, rpc, closed, tick } = setup(t);
  const stream = subscribe(rpc), write = rpc.call('orchestration.dispatchCommand', {});
  sync(socket);
  const streamFailure = assert.rejects(stream.promise, e => e.code === 'timeout' && e.uncertain);
  const writeFailure = assert.rejects(write.promise, e => e.code === 'timeout' && e.uncertain);
  tick(5000); tick(5000); tick(4999); assert.equal(closed.length, 0);
  tick(1); await Promise.all([streamFailure, writeFailure]);
  assert.match(closed[0].message, /stopped responding/); assert.equal(closed.length, 1);
  clean(rpc, socket);
  const count = socket.sent.length;
  socket.eof(); rpc.close(); stream.cancel(); write.cancel(); tick(60000);
  assert.equal(socket.sent.length, count); assert.equal(closed.length, 1); assert.equal(socket.closes, 1);
});

test('ordinary incoming frames keep a busy server alive without Pong', async t => {
  const { socket, rpc, closed, tick } = setup(t), values = [];
  const stream = subscribe(rpc, item => values.push(item)); sync(socket);
  for (let i = 0; i < 8; i++) {
    tick(10000);
    socket.incoming({ _tag: 'Chunk', requestId: '1', values: [{ kind: 'event', sequence: i }] });
  }
  assert.equal(closed.length, 0); assert.equal(values.length, 9);
  stream.cancel(); await stream.promise;
});

test('Pong and replay events cannot extend the initial synchronization deadline', async t => {
  const { socket, rpc, closed, tick } = setup(t);
  socket.onSend = frame => { if (frame._tag === 'Ping') socket.incoming({ _tag: 'Pong' }); };
  const stream = subscribe(rpc, () => {}, 12000);
  const rejected = assert.rejects(stream.promise, e => e.code === 'timeout' && /synchronize/.test(e.message));
  for (let i = 0; i < 2; i++) {
    tick(5000);
    socket.incoming({ _tag: 'Chunk', requestId: '1', values: [{ kind: 'event', sequence: i }] });
  }
  tick(1999); assert.equal(closed.length, 0); tick(1);
  await rejected; assert.equal(closed.length, 1); clean(rpc, socket);
});

test('synchronization clears only its own deadline, not another subscription', async t => {
  const { socket, rpc, tick } = setup(t);
  socket.onSend = frame => { if (frame._tag === 'Ping') socket.incoming({ _tag: 'Pong' }); };
  const first = subscribe(rpc, () => {}, 8000), second = subscribe(rpc, () => {}, 12000);
  const failures = [first, second].map(s => assert.rejects(s.promise, /did not synchronize/));
  sync(socket, '1'); tick(5000); tick(5000);
  assert.equal(rpc.closed, false); tick(2000);
  await Promise.all(failures); clean(rpc, socket);
});

test('cancellation settles once, clears deadline, and ignores late stream frames', async t => {
  const { socket, rpc, closed, tick } = setup(t), values = [];
  socket.onSend = frame => { if (frame._tag === 'Ping') socket.incoming({ _tag: 'Pong' }); };
  const stream = subscribe(rpc, item => values.push(item), 1000);
  stream.cancel(); stream.cancel(); assert.equal(await stream.promise, null);
  sync(socket); socket.incoming({ _tag: 'Exit', requestId: '1', exit: { _tag: 'Success' } });
  for (let i = 0; i < 6; i++) tick(5000);
  assert.equal(rpc.pending.size, 0); assert.equal(closed.length, 0); assert.deepEqual(values, []);
  assert.deepEqual(socket.sent.filter(f => f._tag === 'Interrupt'), [{ _tag: 'Interrupt', requestId: '1' }]);
});

for (const synchronized of [false, true]) {
  test(`unexpected successful stream Exit reconnects ${synchronized ? 'after' : 'before'} synchronization`, async t => {
    const { socket, rpc, closed, tick } = setup(t);
    const stream = subscribe(rpc), write = rpc.call('write', {});
    if (synchronized) sync(socket);
    socket.incoming({ _tag: 'Exit', requestId: '1', exit: { _tag: 'Success', value: null } });
    await assert.rejects(stream.promise, e => e.code === 'stream_ended');
    await assert.rejects(write.promise, e => e.uncertain === true);
    tick(60000); assert.equal(closed.length, 1); clean(rpc, socket);
  });
}

test('failed stream Exit notifies reconnect and does not reject unrelated writes definitively', async t => {
  const { socket, rpc, closed } = setup(t), stream = subscribe(rpc), write = rpc.call('write', {});
  socket.incoming({ _tag: 'Exit', requestId: '1', exit: { _tag: 'Failure', cause: [{ _tag: 'Fail', error: { message: 'private detail' } }] } });
  await assert.rejects(stream.promise, e => e.code === 'stream_ended' && !e.message.includes('private detail'));
  await assert.rejects(write.promise, e => e.uncertain === true);
  assert.equal(closed.length, 1); clean(rpc, socket);
});

for (const event of ['close', 'error']) {
  test(`socket ${event} deterministically clears listeners, heartbeat, and requests`, async t => {
    const { socket, rpc, closed, tick } = setup(t), stream = subscribe(rpc), call = rpc.call('write', {});
    socket.dispatchEvent(new Event(event));
    await assert.rejects(stream.promise, e => e.uncertain === true);
    await assert.rejects(call.promise, e => e.uncertain === true);
    tick(60000); assert.equal(closed.length, 1); clean(rpc, socket);
  });
}

test('explicit close settles pending work and rejects subsequent calls without leaking timers', async t => {
  const { socket, rpc, closed, tick } = setup(t), call = rpc.call('write', {});
  rpc.close(); rpc.close(); await assert.rejects(call.promise, e => e.code === 'closed' && e.uncertain);
  const late = rpc.call('write', {}); await assert.rejects(late.promise, e => e.code === 'disconnected' && e.uncertain);
  late.cancel(); tick(60000); assert.equal(closed.length, 1); clean(rpc, socket);
});

test('Ping send failure retires channel without waiting for a close event', async t => {
  const { socket, rpc, closed, tick } = setup(t), call = rpc.call('write', {});
  socket.onSend = frame => { if (frame._tag === 'Ping') throw new Error('transport send failed'); };
  tick(5000); await assert.rejects(call.promise, e => e.uncertain === true);
  assert.equal(closed.length, 1); clean(rpc, socket);
});

test('unary acknowledgement timeout remains independent of healthy Pong traffic', async t => {
  const { socket, rpc, closed, tick } = setup(t);
  socket.onSend = frame => { if (frame._tag === 'Ping') socket.incoming({ _tag: 'Pong' }); };
  const call = rpc.call('write', {}, null, 6000);
  tick(5000); tick(1000); await assert.rejects(call.promise, e => e.code === 'timeout' && e.uncertain);
  assert.equal(rpc.closed, false); assert.equal(closed.length, 0); assert.equal(rpc.pending.size, 0);
  assert.equal(socket.sent.at(-1)._tag, 'Interrupt');
});

for (const failure of ['close', 'error', 'timeout']) {
  test(`connection opening ${failure} cleans temporary handlers and timer`, async t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    let socket;
    class OpeningSocket extends Socket { constructor() { super(); this.readyState = 0; socket = this; } }
    const connecting = connectSocket('ws://fixture', () => assert.fail('never connected'), OpeningSocket);
    const rejected = assert.rejects(connecting, e => e.code === (failure === 'timeout' ? 'timeout' : 'disconnected'));
    if (failure === 'timeout') t.mock.timers.tick(15000);
    else socket.dispatchEvent(new Event(failure));
    await rejected; t.mock.timers.tick(60000);
    assert.equal(socket.closes, 1); assert.equal(socket.listenerCount(), 0);
  });
}

test('connection open installs RPC listeners without leaving opening handlers behind', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let socket, closed = 0;
  class OpeningSocket extends Socket { constructor() { super(); socket = this; } }
  const connecting = connectSocket('ws://fixture', () => closed++, OpeningSocket);
  socket.dispatchEvent(new Event('open'));
  const rpc = await connecting;
  assert.equal(socket.listenerCount(), 3);
  rpc.close(); t.mock.timers.tick(60000);
  assert.equal(closed, 1); clean(rpc, socket);
});
