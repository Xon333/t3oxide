// T3-specific compatibility boundary; never imported by the native app.
import { readFile, lstat } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { Journal, journalScope } from './journal.mjs';
import { Fault, LIMIT, SOURCE, pageRows, clip, command, hash, identifier, launch, requireThat, targetThread, trimView, validateDescriptor, view } from './core.mjs';
import { connectSocket } from './wire.mjs';
import { FEATURES, catalog, modelCommand, modelView } from './models.mjs';
import { threadList } from './navigation.mjs';

export async function readConfig(path) {
  const stat = await lstat(path);
  requireThat(stat.isFile() && stat.size <= 65536 && (stat.mode & 0o077) === 0 && (!process.getuid || stat.uid === process.getuid()), 'Host configuration must be a regular private file (chmod 600).', 'configuration');
  const config = JSON.parse(await readFile(path, 'utf8'));
  const url = new URL(config.baseUrl);
  requireThat(['http:', 'https:'].includes(url.protocol) && ['127.0.0.1','localhost','[::1]'].includes(url.hostname) &&
    !url.username && !url.password && !url.search && !url.hash && url.pathname === '/',
    'The host adapter must use a loopback T3 URL, with no path or credentials.', 'configuration');
  identifier(config.environmentId, 'environment id');
  requireThat(typeof config.tokenFile === 'string' && config.tokenFile.startsWith('/'), 'tokenFile must be an absolute host-local path.', 'configuration');
  const secretStat = await lstat(config.tokenFile);
  requireThat(secretStat.isFile() && secretStat.size <= 16384 && (secretStat.mode & 0o077) === 0 && (!process.getuid || secretStat.uid === process.getuid()), 'Token must be a regular private file (chmod 600).', 'configuration');
  const token = (await readFile(config.tokenFile, 'utf8')).trim();
  requireThat(token.length > 0 && token.length < 16384 && !/[\r\n]/.test(token), 'Invalid token file.', 'configuration');
  return { ...config, baseUrl: url.href, token, journalDirectory: join(dirname(resolve(path)), 'operations-v1') };
}

export class T3 {
  constructor(config, emit, dependencies = {}) {
    this.config = config; this.emit = emit; this.fetch = dependencies.fetch ?? fetch;
    this.connectSocket = dependencies.connectSocket ?? connectSocket;
    this.journal = dependencies.journal ?? null;
    this.rpc = null; this.descriptor = null; this.watchId = null;
    this.current = null; this.snapshotSequence = 0; this.lastStreamSequence = 0; this.generation = 0;
    this.subscription = null; this.wantConnection = false; this.closed = false;
    this.emitTimer = null; this.refreshTimer = null; this.retryTimer = null;
    this.retryDelay = 1000; this.refreshing = false; this.refreshAgain = false;
    this.duringRefresh = new Map(); this.epoch = 0; this.following = true;
    this.controllers = new Set(); this.receipts = new Map();
    this.synchronized = false;
    this.navigation = new AbortController(); this.revision = 0; this.sentView = null; this.forceSnapshot = false;
    this.stats = { httpBytes: 0, snapshots: 0, rpcEvents: 0 };
  }
  async http(path, method = 'GET', authenticated = true, signal) {
    const controller = new AbortController(); this.controllers.add(controller);
    const abort = () => controller.abort();
    signal?.addEventListener('abort', abort, { once: true });
    if (signal?.aborted) controller.abort();
    const timeout = setTimeout(() => controller.abort(), 20000);
    try {
      const response = await this.fetch(new URL(path, this.config.baseUrl), {
        method, redirect: 'error', signal: controller.signal,
        headers: { 'x-t3-orchestration-protocol': '2', ...(authenticated ? { Authorization: `Bearer ${this.config.token}` } : {}) }
      });
      if (!response.ok) throw new Fault(response.status === 401 || response.status === 403 ? 'authorization' :
        response.status === 404 ? 'unavailable' : 'upstream', `T3 HTTP ${response.status}; ${response.status === 401 ? 'renew this host credential' : 'check the host adapter compatibility'}.`);
      requireThat(response.body, 'Empty T3 response.', 'incompatible');
      const reader = response.body.getReader(), parts = []; let total = 0;
      for (;;) {
        const { value, done } = await reader.read(); if (done) break;
        total += value.byteLength;
        if (total > LIMIT.upstream) { await reader.cancel(); throw new Fault('too_large', 'T3 response exceeds the host safety limit; no full-history fallback was attempted.'); }
        parts.push(value);
      }
      this.stats.httpBytes += total;
      return JSON.parse(Buffer.concat(parts).toString('utf8'));
    } catch (error) {
      if (signal?.aborted) throw new Fault('cancelled', 'Navigation was superseded.');
      if (error instanceof Fault) throw error;
      throw new Fault('transport', 'Could not read the local T3 endpoint. No server was started or changed.');
    } finally { clearTimeout(timeout); signal?.removeEventListener('abort', abort); this.controllers.delete(controller); }
  }
  async probe() {
    const d = await this.http('/.well-known/t3/environment', 'GET', false);
    this.descriptor = validateDescriptor(d, this.config.environmentId);
    return { protocol: 1, adapterRevision: SOURCE, environmentId: d.environmentId,
      serverVersion: d.serverVersion, orchestrationProtocol: d.orchestrationProtocolVersion,
      features: { ...FEATURES, modelSelection: d.capabilities?.serverResolvedCommandContext === true,
        messageModelOverride: d.capabilities?.serverResolvedCommandContext === true },
      writesEnabled: this.config.allowWrites === true && d.capabilities?.serverResolvedCommandContext === true };
  }
  async connect() {
    if (this.rpc) return;
    if (this.connecting) return this.connecting;
    this.wantConnection = true;
    this.connecting = (async () => {
      const generation = ++this.generation;
      const hello = await this.probe();
      const ticket = await this.http('/api/auth/websocket-ticket', 'POST');
      requireThat(typeof ticket.ticket === 'string', 'Unsupported T3 ticket response.', 'incompatible');
      const url = new URL('/ws', this.config.baseUrl);
      url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
      url.searchParams.set('wsTicket', ticket.ticket);
      url.searchParams.set('orchestrationProtocol', '2');
      const rpc = await this.connectSocket(url.href, () => {
        if (generation === this.generation) this.disconnected();
      });
      if (this.closed || !this.wantConnection || generation !== this.generation) {
        rpc.close(); throw new Fault('disconnected', 'T3 connection ended before synchronization.');
      }
      this.rpc = rpc;
      if (this.config.journalDirectory && !this.journal) {
        this.journal = new Journal(this.config.journalDirectory, journalScope(this.config));
      }
      this.emit('status', { state: 'connected', ...hello });
    })();
    try { await this.connecting; } finally { this.connecting = null; }
  }
  disconnected() {
    ++this.generation;
    this.rpc = null; this.subscription = null;
    this.cancelNavigation();
    this.setState('unavailable'); this.emitView();
    if (this.closed || !this.wantConnection) return;
    this.emit('status', { state: 'reconnecting', message: 'T3 disconnected. Outstanding writes are not replayed automatically.' });
    if (this.retryTimer) return;
    const delay = this.retryDelay, epoch = this.epoch; this.retryDelay = Math.min(30000, delay * 2);
    this.retryTimer = setTimeout(async () => {
      this.retryTimer = null;
      if (epoch !== this.epoch || this.closed) return;
      try {
        await this.connect();
        if (epoch !== this.epoch || this.closed) return;
        if (this.watchId && this.following) await this.watch(this.watchId);
        else if (this.current?.historical) { this.setState('historical'); this.emitView(true); }
      }
      catch (e) {
        if (['incompatible','identity_mismatch','authorization'].includes(e.code)) {
          this.wantConnection = false; this.emit('status', { state: 'blocked', message: e.message });
        } else this.disconnected();
      }
    }, delay + Math.floor(Math.random() * Math.min(500, delay / 4)));
  }
  async list(query = '', options = {}) {
    await this.connect();
    return threadList(await this.http('/api/orchestration/shell'), query, options);
  }
  async models(threadId) {
    await this.connect();
    const p = targetThread(await this.bounded(identifier(threadId)), threadId);
    return this.modelCatalog(p);
  }
  async modelCatalog(p) {
    const config = await this.rpc.call('server.getConfig', {}).promise;
    return catalog(config, p);
  }
  async bounded(threadId, signal) {
    this.stats.snapshots++;
    const s = await this.http(`/api/orchestration/threads/${encodeURIComponent(threadId)}/bounded`, 'GET', true, signal);
    targetThread(s, threadId);
    requireThat(Number.isSafeInteger(s.snapshotSequence), 'Missing snapshot sequence.', 'incompatible');
    return s;
  }
  setState(state) {
    if (!this.current) return;
    this.current.state = state;
    this.current.live = state === 'live';
    this.current.historical = this.current.historical === true;
  }
  cancelNavigation() {
    ++this.epoch; this.synchronized = false;
    this.navigation.abort(); this.navigation = new AbortController();
    const subscription = this.subscription; this.subscription = null;
    subscription?.cancel();
    clearTimeout(this.refreshTimer); this.refreshTimer = null;
    this.refreshing = false; this.refreshAgain = false; this.duringRefresh = new Map();
    if (this.current?.live) { this.setState('unavailable'); this.emitView(); }
  }
  async watch(threadId) {
    identifier(threadId); this.cancelNavigation();
    clearTimeout(this.retryTimer); this.retryTimer = null;
    const epoch = this.epoch, signal = this.navigation.signal;
    const historical = this.current?.historical === true;
    this.watchId = threadId; this.following = !historical;
    this.setState('synchronizing'); this.emitView();
    try {
      await this.connect();
      if (epoch !== this.epoch || this.closed) return null;
      const snapshot = await this.bounded(threadId, signal);
      if (epoch !== this.epoch || this.closed) return null;
      this.current = modelView(snapshot, threadId); this.following = true;
      this.snapshotSequence = snapshot.snapshotSequence; this.lastStreamSequence = snapshot.snapshotSequence;
      this.setState('synchronizing'); this.emitView(true);
      const sub = this.rpc.call('orchestration.subscribeThread', { threadId,
        afterSequence: snapshot.snapshotSequence, requestCompletionMarker: true, acceptBoundedSnapshot: true },
        item => { if (epoch === this.epoch && this.following) this.onEvent(item); });
      this.subscription = sub;
      sub.promise.catch(e => {
        if (epoch === this.epoch && !this.closed) {
          this.synchronized = false; this.setState('unavailable'); this.emitView();
          this.emit('status', {state:'unavailable',message:e.message});
        }
      });
      return { threadId };
    } catch (error) {
      if (epoch === this.epoch && !this.closed) {
        // A failed Latest must leave the historical page explicitly historical.
        this.following = !historical;
        this.setState('unavailable'); this.emitView();
      }
      throw error;
    }
  }
  emitView(full = false) {
    this.forceSnapshot ||= full;
    if (this.emitTimer) return;
    this.emitTimer = setTimeout(() => { this.emitTimer = null; this.flushView(); }, 350);
  }
  flushView() {
    clearTimeout(this.emitTimer); this.emitTimer = null;
    if (!this.current || this.closed) return;
    trimView(this.current);
    const next = structuredClone(this.current), previous = this.sentView;
    const { items, revision: ignored, ...metadata } = next;
    const oldRows = new Map((previous?.items ?? []).map(row => [row.key, row]));
    const upserts = items.filter(row => JSON.stringify(row) !== JSON.stringify(oldRows.get(row.key)));
    const keys = new Set(items.map(row => row.key));
    const removed = [...oldRows.keys()].filter(key => !keys.has(key));
    const full = this.forceSnapshot || !previous || previous.threadId !== next.threadId;
    this.forceSnapshot = false;
    if (!full && JSON.stringify(next) === JSON.stringify(previous)) return;
    const baseRevision = this.revision, revision = ++this.revision;
    if (full) this.emit('view', { ...next, revision });
    else this.emit('delta', { threadId: next.threadId, baseRevision, revision,
      upserts, removed, order: items.map(row => row.key), metadata });
    this.sentView = next;
  }
  onEvent(item) {
    if (!this.following || this.closed) return;
    this.stats.rpcEvents++;
    if (item.kind === 'synchronized') { this.synchronized = true; this.retryDelay = 1000; this.setState('live'); this.emitView(); return; }
    if (item.kind === 'snapshot') {
      this.synchronized = false;
      this.current = modelView(item,this.watchId); this.snapshotSequence = item.snapshotSequence; this.lastStreamSequence = item.snapshotSequence;
      this.setState('synchronizing'); this.emitView(true); return;
    }
    if (item.kind !== 'event') return;
    requireThat(Number.isSafeInteger(item.sequence), 'Missing T3 event sequence.', 'incompatible');
    if (item.sequence <= Math.max(this.snapshotSequence, this.lastStreamSequence)) return;
    this.lastStreamSequence = item.sequence;
    const event = item.event;
    requireThat(event && typeof event.type === 'string', 'Unsupported T3 stream event.', 'incompatible');
    if (event.threadId !== this.watchId) return;
    if (this.current) this.current.sequence = item.sequence;
    const payload = event.payload;
    // Existing visible message updates need neither a whole projection nor a snapshot read.
    if (event.type === 'message.updated' || event.type === 'turn-item.updated') {
      const row = this.current?.items.find(r => r.sourceThreadId === this.watchId &&
        (event.type === 'message.updated' ? r.messageId === payload?.id : r.itemId === payload?.id));
      if (row && typeof (payload.text ?? payload.markdown) === 'string') {
        Object.assign(row, clip(payload.text ?? payload.markdown), { streaming: payload.streaming === true });
        if (this.refreshing) this.duringRefresh.set(row.key, { sequence: item.sequence, row: {...row} });
        this.emitView(); return;
      }
      if (event.type === 'turn-item.updated' && !['user_message','assistant_message','proposed_plan','approval_request','user_input_request','error','system_notice','notification','secret_request'].includes(payload?.type)) return;
      if (event.type === 'message.updated') return; // corresponding turn item supplies canonical visibility
    } else if (!/^(thread\.|run\.|run-attempt\.|provider-thread\.|provider-session\.|provider-turn\.|subagent\.|runtime-request\.|context-transfer\.|checkpoint.rollback)/.test(event.type)) return;
    if (this.refreshing) { this.refreshAgain = true; return; }
    this.scheduleRefresh();
  }
  scheduleRefresh() {
    if (this.closed || !this.following) return;
    if (this.refreshing) { this.refreshAgain = true; return; }
    if (this.refreshTimer) return;
    this.refreshTimer = setTimeout(() => { this.refreshTimer = null; this.refresh().catch(e => { if (e.code !== 'cancelled') this.emit('status',{state:'unavailable',message:e.message}); }); }, 800);
  }
  async refresh() {
    if (this.refreshing || !this.watchId || !this.following) return;
    const epoch = this.epoch, id = this.watchId;
    this.refreshing = true; this.refreshAgain = false; this.duringRefresh = new Map();
    const pendingUpdates = this.duringRefresh;
    try {
      const s = await this.bounded(id, this.navigation.signal);
      if (epoch !== this.epoch || this.closed || s.snapshotSequence < this.snapshotSequence) return;
      const next = modelView(s,id);
      next.state = this.synchronized ? 'live' : 'synchronizing'; next.live = next.state === 'live'; next.historical = false;
      for (const pending of pendingUpdates.values()) {
        if (pending.sequence <= s.snapshotSequence) continue;
        const i = next.items.findIndex(r => r.key === pending.row.key);
        if (i >= 0) { next.items[i] = pending.row; next.sequence = Math.max(next.sequence, pending.sequence); }
      }
      this.current = next; this.snapshotSequence = s.snapshotSequence; this.emitView();
    } catch (error) {
      if (epoch === this.epoch && !this.closed) { this.setState('unavailable'); this.emitView(); }
      throw error;
    } finally {
      pendingUpdates.clear();
      if (epoch === this.epoch) {
        this.refreshing = false;
        if (this.refreshAgain) this.scheduleRefresh();
      }
    }
  }
  async history(cursor) {
    requireThat(this.current && typeof cursor === 'string' && cursor === this.current.historyCursor,
      'History cursor changed; use Latest and try again.', 'stale');
    this.cancelNavigation();
    const id = this.current.threadId, epoch = this.epoch, signal = this.navigation.signal;
    this.watchId = id; this.following = false;
    this.current.historical = true;
    this.current.requests = []; this.current.activeRunId = null;
    this.setState('synchronizing'); this.emitView();
    try {
      const params = new URLSearchParams({cursor,view:'conversation'});
      const page = await this.http(`/api/orchestration/threads/${encodeURIComponent(id)}/history?${params}`, 'GET', true, signal);
      requireThat(Array.isArray(page.items), 'Unsupported T3 history page.', 'incompatible');
      if (epoch !== this.epoch || this.closed) return null;
      const sequence = page.snapshotSequence ?? this.current.sequence;
      this.current = { ...this.current, ...pageRows(page.items, sequence, page.nextCursor, page.hasMoreHistory),
        sequence, observedAt: new Date().toISOString() };
      this.setState('historical'); this.emitView(); return { threadId: id };
    } catch (error) {
      if (epoch === this.epoch && !this.closed) { this.setState('unavailable'); this.emitView(); }
      throw error;
    }
  }
  async act(op, a) {
    requireThat(this.config.allowWrites === true, 'Writes are disabled in this host configuration.', 'read_only');
    requireThat(a.environmentId === this.config.environmentId, 'Environment identity mismatch.', 'identity_mismatch');
    identifier(a.id, 'operation id');
    const fingerprint = hash({op,...a});
    const old = this.receipts.get(a.id);
    requireThat(!old || old.fingerprint === fingerprint, 'An operation id cannot be reused with different content.', 'invalid_request');
    if (old?.result) return old.result;
    if (old?.promise) return old.promise;
    if (!old && this.receipts.size >= 128) {
      // Never evict a pending or uncertain operation's original payload.
      const disposable = [...this.receipts].find(([, r]) => r.result && !r.promise);
      requireThat(disposable, 'Too many unconfirmed operations; inspect them before sending more.', 'backpressure');
      this.receipts.delete(disposable[0]);
    }
    const receipt = old ?? { fingerprint };
    this.receipts.set(a.id, receipt);
    receipt.promise = (async () => {
      try {
        await this.connect();
        if (this.journal) {
          const saved = this.journal.access(a.id, fingerprint);
          if (saved) {
            receipt.durable = saved;
            receipt.payload = saved.payload; receipt.tag = saved.tag;
            if (saved.result) { receipt.result = saved.result; return saved.result; }
            // A prepared record can have reached T3 before a previous helper
            // died. Never infer success from current control state.
            receipt.uncertain = true;
          }
        }
        if (!receipt.payload) {
          // First dispatch uses fresh control state. An explicit same-ID retry
          // reuses the exact command, even if the original Stop/answer took effect.
          const p = targetThread(await this.bounded(identifier(a.threadId)), a.threadId);
          const choices = op === 'select-model' || op === 'send' && a.modelSelection !== undefined
            ? await this.modelCatalog(p) : null;
          receipt.payload = op === 'launch' ? launch(a,p) : modelCommand(op,a,p,this.descriptor,choices);
          receipt.tag = op === 'launch' ? 'orchestration.launchThread' : 'orchestration.dispatchCommand';
        }
        requireThat(!this.closed && this.rpc, 'T3 disconnected before dispatch.', 'disconnected');
        if (this.journal && !receipt.durable) {
          // Persist BEFORE network dispatch. Competing helpers converge on the
          // winner's exact payload, even if their preflight snapshots differ.
          const saved = this.journal.access(a.id, fingerprint, receipt);
          receipt.durable = saved; receipt.payload = saved.payload; receipt.tag = saved.tag;
          if (saved.result) { receipt.result = saved.result; return saved.result; }
        }
        const result = await this.rpc.call(receipt.tag, receipt.payload).promise;
        if (!(op === 'launch' ? typeof result?.threadId === 'string' : Number.isSafeInteger(result?.sequence) && result.sequence >= 0)) throw new Fault('incompatible', 'T3 returned an unfamiliar acknowledgement; inspect the thread before another action.', true);
        const accepted = op === 'launch' ? {threadId:result.threadId} : {sequence:result.sequence};
        // No success reaches the client until its receipt is durable too.
        // A storage failure after upstream acceptance remains retryable with
        // the original persisted payload, never with freshly derived controls.
        if (receipt.durable) {
          receipt.uncertain = true;
          this.journal.acknowledge(receipt.durable, accepted);
        }
        receipt.result = accepted;
        receipt.uncertain = false;
        delete receipt.payload;
        if (!this.closed && this.following && this.watchId === a.threadId) this.scheduleRefresh();
        return receipt.result;
      } catch (e) {
        // A fresh helper can fail authentication/connection before opening its
        // journal. Check only for prior local evidence here; never expose a
        // cached result or dispatch using an unauthenticated token's claims.
        if (!receipt.uncertain && !receipt.durable && (this.journal || this.config.journalDirectory)) {
          try {
            const journal = this.journal ?? new Journal(this.config.journalDirectory, journalScope(this.config));
            receipt.uncertain = Boolean(journal.access(a.id, fingerprint));
          } catch { receipt.uncertain = true; }
        }
        if (e.uncertain || receipt.uncertain) {
          receipt.uncertain = true;
          throw e.uncertain ? e : new Fault(e.code ?? 'disconnected', e.message, true);
        }
        this.receipts.delete(a.id);
        throw e;
      } finally { receipt.promise = null; }
    })();
    return receipt.promise;
  }
  close() {
    this.closed = true; this.wantConnection = false; this.cancelNavigation();
    for (const timer of [this.emitTimer,this.refreshTimer,this.retryTimer]) clearTimeout(timer);
    this.subscription?.cancel(); this.rpc?.close(); this.rpc = null;
    for (const controller of this.controllers) controller.abort();
    this.current = null; this.sentView = null; this.duringRefresh.clear(); this.receipts.clear();
  }
}
