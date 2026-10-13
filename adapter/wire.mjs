// Effect RPC JSON envelopes, adapted from upstream RpcMessage.ts; see NOTICE.
import { Fault, LIMIT, requireThat } from './core.mjs';

// Upstream client-runtime/rpc/protocol.ts permits 15 seconds without any frame;
// Effect sends application-level Ping every 5 seconds, even on an idle socket.
const PING_INTERVAL = 5000, FRAME_TIMEOUT = 15000;

export class Rpc {
  constructor(socket, onClose = () => {}) {
    this.socket = socket; this.onClose = onClose; this.pending = new Map(); this.next = 0;
    this.closed = false; this.pingTimer = null; this.frameTimer = null;
    this.onMessage = e => {
      if (this.closed) return;
      try {
        requireThat(typeof e.data === 'string' && Buffer.byteLength(e.data) <= LIMIT.upstream, 'Oversized or binary upstream frame.', 'incompatible');
        const data = JSON.parse(e.data);
        for (const frame of Array.isArray(data) ? data : [data]) {
          if (this.closed) break;
          this.receive(frame);
        }
        if (!this.closed) this.armFrameTimeout();
      } catch (e) { this.terminate(e); }
    };
    this.onSocketClose = () => this.terminate(new Fault('disconnected', 'T3 disconnected; an unacknowledged command may have been applied.', true));
    this.onSocketError = () => this.terminate(new Fault('disconnected', 'T3 socket failed; an unacknowledged command may have been applied.', true));
    socket.addEventListener('message', this.onMessage);
    socket.addEventListener('close', this.onSocketClose);
    socket.addEventListener('error', this.onSocketError);
    this.armFrameTimeout(); this.armPing();
  }
  armFrameTimeout() {
    clearTimeout(this.frameTimer);
    this.frameTimer = setTimeout(() => this.terminate(new Fault('timeout', 'T3 stopped responding; outstanding command acceptance is unknown.', true)), FRAME_TIMEOUT);
    this.frameTimer.unref?.();
  }
  armPing() {
    this.pingTimer = setTimeout(() => {
      if (this.closed) return;
      try { this.send({ _tag: 'Ping' }); }
      catch (e) { this.terminate(e); }
      if (!this.closed) this.armPing();
    }, PING_INTERVAL);
    this.pingTimer.unref?.();
  }
  send(frame) {
    requireThat(!this.closed && this.socket.readyState === 1, 'T3 socket is disconnected.', 'disconnected');
    this.socket.send(JSON.stringify(frame));
  }
  receive(frame) {
    requireThat(frame && typeof frame._tag === 'string', 'Malformed RPC response.', 'incompatible');
    if (frame._tag === 'Pong') return;
    if (frame._tag === 'Defect' || frame._tag === 'ClientProtocolError') throw new Fault('incompatible', 'T3 rejected the RPC protocol.');
    const id = String(frame.requestId), p = this.pending.get(id);
    if (!p) return;
    if (frame._tag === 'Chunk') {
      requireThat(Array.isArray(frame.values), 'Invalid RPC chunk.', 'incompatible');
      for (const item of frame.values) {
        if (this.closed || this.pending.get(id) !== p) break;
        p.onValue?.(item);
        if (p.subscription && item?.kind === 'synchronized') { clearTimeout(p.timer); p.timer = null; }
      }
      if (!this.closed && this.pending.get(id) === p) this.send({ _tag: 'Ack', requestId: frame.requestId });
    } else if (frame._tag === 'Exit') {
      // This subscription is open-ended. Even Success means live observation
      // stopped. Retire the channel so the owner reconnects and resynchronizes.
      if (p.subscription) {
        this.terminate(new Fault('stream_ended', 'T3 live subscription ended; reconnecting is required.', true));
        return;
      }
      this.remove(id);
      if (frame.exit?._tag === 'Success') p.resolve(frame.exit.value);
      else {
        // Deliberately do not forward arbitrary error payloads or stack traces.
        const fail = Array.isArray(frame.exit?.cause) ? frame.exit.cause.find(c => c._tag === 'Fail') : null;
        p.reject(fail ? new Fault('upstream_rejected', typeof fail.error?.message === 'string' ? fail.error.message.slice(0,600) : 'T3 rejected this operation.') :
          new Fault('incompatible', 'Malformed or failed acknowledgement; command acceptance is unknown.', true));
      }
    }
  }
  remove(id) {
    const p = this.pending.get(id);
    if (p) { clearTimeout(p.timer); p.timer = null; this.pending.delete(id); }
    return p;
  }
  call(tag, payload, onValue = null, timeout = 20000) {
    const id = String(++this.next); let resolve, reject;
    const promise = new Promise((r, j) => { resolve = r; reject = j; });
    // The owner can attach its handler after a synchronous socket failure.
    // Keep the original rejecting promise so callers still observe the failure.
    promise.catch(() => {});
    const p = { resolve, reject, onValue, timer: null, subscription: Boolean(onValue) && tag === 'orchestration.subscribeThread' };
    this.pending.set(id, p);
    if (!onValue || p.subscription) p.timer = setTimeout(() => {
      if (this.pending.get(id) !== p) return;
      if (p.subscription) {
        this.terminate(new Fault('timeout', 'T3 subscription did not synchronize before its deadline.', true));
        return;
      }
      this.remove(id);
      try { this.send({ _tag: 'Interrupt', requestId: id }); } catch (e) { this.terminate(e); }
      reject(new Fault('timeout', 'No T3 acknowledgement. Do not create a new operation id when retrying.', true));
    }, timeout);
    p.timer?.unref?.();
    try { this.send({ _tag: 'Request', id, tag, payload, headers: [] }); }
    catch (e) {
      // send may throw after transport acceptance; never turn this into a
      // definite write rejection, and notify the owner even without socket EOF.
      if (this.closed) { this.remove(id); reject(new Fault('disconnected', 'T3 socket is disconnected.', true)); }
      else this.terminate(e);
    }
    return { promise, cancel: () => {
      if (!this.remove(id)) return;
      resolve(null);
      try { this.send({ _tag: 'Interrupt', requestId: id }); } catch (e) { this.terminate(e); }
    } };
  }
  failAll(error) {
    // Every queued RPC may already have executed. A broken channel is never a rejection receipt.
    const uncertain = new Fault(error.code ?? 'disconnected', error.message ?? 'RPC channel failed.', true);
    for (const id of this.pending.keys()) this.remove(id).reject(uncertain);
  }
  terminate(error) {
    if (this.closed) return;
    this.closed = true;
    clearTimeout(this.pingTimer); clearTimeout(this.frameTimer);
    this.pingTimer = null; this.frameTimer = null;
    this.socket.removeEventListener('message', this.onMessage);
    this.socket.removeEventListener('close', this.onSocketClose);
    this.socket.removeEventListener('error', this.onSocketError);
    this.failAll(error);
    try { this.socket.close(); } catch {}
    // Do not wait for the closing handshake: it too can stall indefinitely.
    this.onClose(error);
  }
  close() { this.terminate(new Fault('closed', 'Connection closed.', true)); }
}

export async function connectSocket(url, onClose, WebSocketClass = WebSocket) {
  const socket = new WebSocketClass(url);
  const rpc = await new Promise((resolve, reject) => {
    let settled = false;
    const cleanup = () => {
      clearTimeout(timer);
      socket.removeEventListener('open', opened);
      socket.removeEventListener('error', failed);
      socket.removeEventListener('close', failed);
    };
    const fail = error => {
      if (settled) return;
      settled = true; cleanup();
      try { socket.close(); } catch {}
      reject(error);
    };
    const opened = () => {
      if (settled) return;
      settled = true; cleanup(); resolve(new Rpc(socket, onClose));
    };
    const failed = () => fail(new Fault('disconnected', 'T3 WebSocket could not connect.'));
    const timer = setTimeout(() => fail(new Fault('timeout', 'T3 WebSocket did not open.')), 15000);
    socket.addEventListener('open', opened);
    socket.addEventListener('error', failed);
    socket.addEventListener('close', failed);
  });
  if (rpc.closed) throw new Fault('disconnected', 'T3 WebSocket closed while connecting.');
  return rpc;
}
