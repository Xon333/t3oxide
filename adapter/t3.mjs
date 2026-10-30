// T3-specific compatibility boundary; never imported by the native app.
import { readFile, lstat } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { Journal, journalScope } from './journal.mjs';
import { Fault, HISTORY_BUDGET, LIMIT, SOURCE, pageRows, clip, command, hash, historyAnchor, identifier, requireThat, targetThread, text, trimView, validateDescriptor, view } from './core.mjs';
import { connectSocket } from './wire.mjs';
import { FEATURES, catalog, launchCommand, modelCommand, modelView, sameSelection } from './models.mjs';
import { threadList } from './navigation.mjs';
import { ACTIVITY_KINDS, ACTIVITY_LIMIT, activityDetail, activityItem, activityPage, trimActivity } from './activity.mjs';
import { projectPage, resolveDefaults, launchProjectCommand, launchWorkspaceMatches, threadActionCommand, nativeRevision, reviewResult, effectiveProjectSetting, projectFileDefaults, nativeDefaultsReport } from './native.mjs';

// Coalesce useful frames without delaying every edit by a third of a second.
// Structural reads have a separate sustained-rate ceiling: a burst can reveal
// new content promptly without turning a busy stream into snapshot polling.
export const SCHEDULING = Object.freeze({ frameMs: 80, structureMs: 120, refreshIntervalMs: 400, probeReceiptMs: 5000 });

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
    this.now = dependencies.now ?? (() => performance.now());
    this.trace = typeof dependencies.trace === 'function' ? dependencies.trace : null;
    this.connectSocket = dependencies.connectSocket ?? connectSocket;
    this.journal = dependencies.journal ?? null;
    this.rpc = null; this.descriptor = null; this.probeReceipt = null; this.watchId = null;
    this.providerIdentities = new Map();
    this.previewGeneration = 0; this.previewController = null;
    this.current = null; this.snapshotSequence = 0; this.lastStreamSequence = 0; this.generation = 0;
    this.subscription = null; this.wantConnection = false; this.closed = false;
    this.emitTimer = null; this.refreshTimer = null; this.retryTimer = null;
    this.nextRefreshAt = 0;
    this.retryDelay = 1000; this.refreshing = false; this.refreshAgain = false; this.refreshUrgent = false;
    this.duringRefresh = new Map(); this.epoch = 0; this.following = true;
    this.controllers = new Set(); this.receipts = new Map();
    this.synchronized = false;
    this.navigation = new AbortController(); this.revision = 0; this.sentView = null; this.forceSnapshot = false;
    this.sentRowJSON = new Map(); this.sentMetadataJSON = null;
    this.stats = { httpBytes: 0, snapshots: 0, rpcEvents: 0, descriptors:0, tickets:0, sockets:0, frames:0, flushes:0, urgentRefreshes:0, rowSerializations:0 };
  }
  mark(stage, extra = {}) {
    // Opt-in test instrumentation only. It never captures payloads, routes,
    // thread/account identities, tokens, drafts or native configuration.
    this.trace?.({stage,at:this.now(),generation:this.generation,epoch:this.epoch,...extra});
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
      if (error instanceof Fault) {
        if (['authorization','identity_mismatch','incompatible'].includes(error.code)) {this.probeReceipt = null;this.providerIdentities.clear();}
        throw error;
      }
      throw new Fault('transport', 'Could not read the local T3 endpoint. No server was started or changed.');
    } finally { clearTimeout(timeout); signal?.removeEventListener('abort', abort); this.controllers.delete(controller); }
  }
  probeBinding() {
    // The receipt contains no reusable transport or mutation admission. Include
    // private credentials only in this in-memory digest, never in diagnostics.
    return hash({environmentId:this.config.environmentId, baseUrl:this.config.baseUrl,
      token:this.config.token, allowWrites:this.config.allowWrites, protocol:2});
  }
  async probe(cacheForConnect = true) {
    this.probeReceipt = null;
    const binding = this.probeBinding(), generation = this.generation;
    if (this.providerIdentityBinding !== binding) this.providerIdentities.clear();
    this.stats.descriptors++; this.mark('descriptor-start');
    const d = await this.http('/.well-known/t3/environment', 'GET', false);
    const descriptor = validateDescriptor(d, this.config.environmentId);
    requireThat(!this.closed && binding === this.probeBinding() && generation === this.generation,
      'Environment configuration changed during verification.', 'cancelled');
    this.descriptor = descriptor; this.mark('descriptor-ready');
    const hello = { protocol: 1, adapterRevision: SOURCE, environmentId: d.environmentId,
      serverVersion: d.serverVersion, orchestrationProtocol: d.orchestrationProtocolVersion, machineKind:this.machineKind(),
      features: { ...FEATURES, modelSelection: d.capabilities?.serverResolvedCommandContext === true,
        nativeWorkflows: d.capabilities?.serverResolvedCommandContext === true,
        nativeDefaults: d.capabilities?.serverResolvedCommandContext === true,
        nativeDefaultsWrite: false, threadPreview:true, serverSearch:true,
        threadPinning:d.capabilities?.threadPinning === true,
        launchModelSelection: d.capabilities?.serverResolvedCommandContext === true,
        messageModelOverride: d.capabilities?.serverResolvedCommandContext === true },
      writesEnabled: this.config.allowWrites === true && d.capabilities?.serverResolvedCommandContext === true };
    if (cacheForConnect && !this.rpc) this.probeReceipt = {hello, descriptor, binding, generation, at:this.now()};
    return hello;
  }
  consumeProbeReceipt(generation) {
    const receipt = this.probeReceipt; this.probeReceipt = null;
    if (!receipt || receipt.generation !== generation || receipt.binding !== this.probeBinding() ||
        this.now() < receipt.at || this.now() - receipt.at > SCHEDULING.probeReceiptMs) return null;
    this.descriptor = receipt.descriptor;
    return receipt.hello;
  }
  async connect() {
    if (this.rpc) return;
    if (this.connecting) return this.connecting;
    this.wantConnection = true;
    this.connecting = (async () => {
      const generation = ++this.generation;
      const hello = this.consumeProbeReceipt(generation - 1) ?? await this.probe(false);
      this.stats.tickets++; this.mark('ticket-start');
      const ticket = await this.http('/api/auth/websocket-ticket', 'POST');
      this.mark('ticket-ready');
      requireThat(typeof ticket.ticket === 'string', 'Unsupported T3 ticket response.', 'incompatible');
      const url = new URL('/ws', this.config.baseUrl);
      url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
      url.searchParams.set('wsTicket', ticket.ticket);
      url.searchParams.set('orchestrationProtocol', '2');
      this.stats.sockets++; this.mark('socket-start');
      const rpc = await this.connectSocket(url.href, () => {
        if (generation === this.generation) this.disconnected();
      });
      if (this.closed || !this.wantConnection || generation !== this.generation) {
        rpc.close(); throw new Fault('disconnected', 'T3 connection ended before synchronization.');
      }
      this.rpc = rpc; this.mark('socket-ready');
      if (this.config.journalDirectory && !this.journal) {
        this.journal = new Journal(this.config.journalDirectory, journalScope(this.config));
      }
      this.emit('status', { state: 'connected', ...hello });
    })();
    try { await this.connecting; } catch (error) { this.probeReceipt = null; throw error; }
    finally { this.connecting = null; }
  }
  disconnected() {
    ++this.generation; this.probeReceipt = null;
    this.providerIdentities.clear();
    this.rpc = null; this.subscription = null;
    this.cancelNavigation();
    this.setState('unavailable'); this.emitView(false, true);
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
  machineKind() {
    const kind = this.descriptor?.platform?.machine;
    return ['server','cloud','linux','desktop','laptop','mac-mini','mac-studio'].includes(kind) ? kind : 'unknown';
  }
  async list(query = '', options = {}) {
    // Ordinary metadata browsing needs no WebSocket or transcript subscription.
    if (!this.descriptor) await this.probe();
    const shell = await this.http('/api/orchestration/shell');
    if (options.serverSearch === true) {
      const q = text(query,800).trim();
      requireThat(q.length >= 2 && q.length <= 200, 'Server search needs 2–200 characters.');
      await this.connect();
      const result = await this.rpc.call('orchestration.searchThreads',{query:q,limit:50}).promise;
      requireThat(Array.isArray(result?.matches) && result.matches.length <= 50, 'Unsupported native search result.', 'incompatible');
      const ids = new Map();
      for (const match of result.matches) {
        identifier(match.threadId,'search thread'); identifier(match.projectId,'search project');
        requireThat(!ids.has(match.threadId) || ids.get(match.threadId) === match.projectId, 'Conflicting native search identity.', 'incompatible');
        ids.set(match.threadId,match.projectId);
      }
      const projected = threadList({...shell,threads:shell.threads.filter(t => ids.get(t.id) === t.projectId)},'',
        {...options,providerIdentities:this.providerIdentities,activeOnly:true,lifecycle:'all',cursor:0,limit:50});
      // This native API returns a capped matches array and no exhaustive/next
      // marker. Never relabel a zero or full page as a complete global search.
      return {...projected,machineKind:this.machineKind(),query:q,serverSearch:true,
        searchScope:'native recent message search',searchIncomplete:true};
    }
    return {...threadList(shell, query, { ...options, providerIdentities:this.providerIdentities, activeOnly: true }),machineKind:this.machineKind()};
  }
  cancelPreview() { ++this.previewGeneration; this.previewController?.abort(); this.previewController=null; }
  async previewThread(a) {
    identifier(a.threadId,'preview thread');
    this.cancelPreview();
    const generation=this.previewGeneration, controller=new AbortController(); this.previewController=controller;
    try {
      if (!this.descriptor) await this.probe();
      requireThat(generation===this.previewGeneration && !this.closed, 'Preview was superseded.', 'cancelled');
      const snapshot=await this.bounded(a.threadId,controller.signal);
      requireThat(generation===this.previewGeneration && !this.closed, 'Preview was superseded.', 'cancelled');
      const preview=modelView(snapshot,a.threadId,this.providerIdentities);
      // No watch, seen mark, socket, request offer, journal or mutation authority.
      preview.state='synchronizing'; preview.live=false; preview.historical=false; preview.revision=1;
      preview.requests=[]; preview.activeRunId=null;
      trimView(preview); trimActivity(preview.activity,preview.sequence);
      const result={threadId:a.threadId,observedAt:preview.observedAt,available:true,readOnly:true,view:preview};
      requireThat(Buffer.byteLength(JSON.stringify(result)) <= LIMIT.frame-8192, 'Preview exceeds the read-only display budget.', 'too_large');
      return result;
    } finally { if (generation===this.previewGeneration) this.previewController=null; }
  }
  async nativeDefaults(a) {
    identifier(a.projectId,'settings project');
    requireThat(['project','environment'].includes(a.scope ?? 'project'), 'Invalid native defaults scope.');
    await this.connect();
    const shell=await this.http('/api/orchestration/shell');
    const config=await this.rpc.call('server.getConfig',{}).promise;
    if (!config.settings) config.settings=await this.rpc.call('server.getSettings',{}).promise;
    const report=nativeDefaultsReport(shell,config,a.projectId,a.scope ?? 'project');
    this.rememberProvider(report.providerIdentity);
    return report;
  }
  async models(threadId) {
    await this.connect();
    const p = targetThread(await this.bounded(identifier(threadId)), threadId);
    return this.modelCatalog(p);
  }
  async projects(a = {}) {
    if (!this.descriptor) await this.probe();
    return projectPage(await this.http('/api/orchestration/shell'), a);
  }
  async launchDefaults(a) {
    identifier(a.projectId, 'project');
    await this.connect();
    requireThat(this.descriptor?.capabilities?.serverResolvedCommandContext === true,
      'Native launch defaults are unavailable on this host.', 'unsupported');
    const shell = await this.http('/api/orchestration/shell');
    const config = await this.rpc.call('server.getConfig', {}).promise;
    if (!config.settings) config.settings = await this.rpc.call('server.getSettings', {}).promise;
    const project = shell.projects?.find(p=>p.id===a.projectId && !p.deletedAt);
    requireThat(project, 'Selected project is unavailable.', 'stale');
    const mode=effectiveProjectSetting(config.settings,project,'defaultThreadEnvMode');
    let projectFile=null;
    if (mode == null) {
      try {
        const file=await this.rpc.call('projects.readFile', {cwd:project.workspaceRoot,relativePath:'t3.json',allowMissing:true}).promise;
        if (file.exists !== false && !file.truncated) projectFile=projectFileDefaults(file.contents);
      } catch (error) {
        // Native treats a missing/invalid project file as absent. Authentication,
        // transport and unknown method failures must not silently pick root.
        if (!['not_found','ENOENT'].includes(error.code) && !/not found|no such file|ENOENT/iu.test(error.message ?? '')) throw error;
      }
    }
    const defaults=resolveDefaults(shell,config,a.projectId,projectFile,a.isolated===true);
    requireThat(a.workspaceMode === undefined || ['local','worktree'].includes(a.workspaceMode), 'Invalid explicit workspace mode.');
    if (a.workspaceMode) defaults.workspaceMode=a.workspaceMode;
    defaults.workspaceStrategy={type:'root'};
    if (defaults.workspaceMode==='worktree') {
      const refs=await this.rpc.call('vcs.listRefs',{cwd:defaults.workspaceRoot,limit:200,refKind:'local'}).promise;
      requireThat(refs?.isRepo===true && Array.isArray(refs.refs), 'Native isolated worktree needs a Git repository.', 'unsupported');
      const base=refs.refs.find(r=>r.isDefault===true && r.isRemote!==true) ?? refs.refs.find(r=>r.current===true && r.isRemote!==true);
      requireThat(base && typeof base.name==='string' && base.name.length>0,
        'Native worktree base is unavailable; choose it in T3.', 'unavailable');
      defaults.workspaceStrategy={type:'worktree',baseRef:base.name};
    }
    defaults.revision=hash(defaults);
    this.rememberProvider(defaults.catalog?.providerIdentity);
    return defaults;
  }
  async reviewThread(a) {
    identifier(a.threadId,'review thread');
    await this.connect();
    const shell=await this.http('/api/orchestration/shell');
    const thread=shell.threads?.find(t=>t.id===a.threadId && !t.deletedAt && !t.archivedAt);
    requireThat(thread,'Native review thread is unavailable.','unavailable');
    requireThat(!a.revision || a.revision===nativeRevision(thread),'Thread changed; open its current review.','stale');
    const project=shell.projects?.find(p=>p.id===thread.projectId && !p.deletedAt);
    const cwd=thread.worktreePath ?? project?.workspaceRoot;
    requireThat(typeof cwd==='string' && cwd.startsWith('/') && Buffer.byteLength(cwd)<=4096 && !/[\x00-\x1f\x7f]/u.test(cwd),
      'Native review workspace is unavailable.','unsupported');
    const result=await this.rpc.call('review.getDiffPreview',{cwd}).promise;
    const fresh=await this.http('/api/orchestration/shell');
    const current=fresh.threads?.find(t=>t.id===thread.id);
    requireThat(current && nativeRevision(current)===nativeRevision(thread), 'Thread changed during review; reopen it.', 'stale');
    return reviewResult(thread,project,result);
  }
  async activity(threadId, cursor) {
    identifier(threadId, 'activity thread');
    if (!this.descriptor) await this.probe();
    if (cursor != null) {
      requireThat(typeof cursor === 'string' && cursor.length > 0 && Buffer.byteLength(cursor) <= 65536,
        'Invalid activity history cursor.');
      const params = new URLSearchParams({cursor, view: 'activity'});
      const page = await this.http(`/api/orchestration/threads/${encodeURIComponent(threadId)}/history?${params}`);
      requireThat(Array.isArray(page.items) && Number.isSafeInteger(page.snapshotSequence), 'Unsupported T3 activity history.', 'incompatible');
      return {threadId, observedAt: new Date().toISOString(), activity: activityPage(page.items,
        {scope:'history', sequence:page.snapshotSequence, cursor:page.nextCursor, hasMore:page.hasMoreHistory})};
    }
    const snapshot = await this.bounded(threadId);
    const projection = targetThread(snapshot, threadId);
    return {threadId, title:clip(projection.thread.title, 512).text, observedAt:new Date().toISOString(),
      activity:activityPage(projection.visibleTurnItems, {sequence:snapshot.snapshotSequence,
        cursor:snapshot.historyCursor, hasMore:snapshot.hasMoreHistory})};
  }
  async inspectActivity(a) {
    identifier(a.threadId, 'activity source thread'); identifier(a.itemId, 'activity item');
    requireThat(a.revision == null || typeof a.revision === 'string' && Buffer.byteLength(a.revision) <= 128,
      'Invalid activity revision.');
    // UI reads name an offered source-qualified item. A CLI read instead names
    // its exact source directly and never creates a detailed subscription.
    const epoch = this.epoch;
    const reviewed = () => !this.closed && (!a.viewThreadId || epoch === this.epoch &&
      this.current?.threadId === a.viewThreadId && this.current.activity?.items.some(item =>
        item.sourceThreadId === a.threadId && item.itemId === a.itemId && item.updatedAt === (a.revision ?? '')));
    if (a.viewThreadId) {
      identifier(a.viewThreadId, 'reviewed thread');
      requireThat(reviewed(), 'This activity page changed. Select the item again.', 'stale');
    }
    await this.connect();
    requireThat(reviewed(), 'This activity page changed. Select the item again.', 'stale');
    const read = this.rpc.call('orchestration.getTurnItem', {threadId:a.threadId, itemId:a.itemId,
      ...(a.revision ? {revision:a.revision} : {})});
    const signal = a.viewThreadId ? this.navigation.signal : null;
    const cancel = () => read.cancel();
    signal?.addEventListener('abort', cancel, {once:true});
    try {
      const result = await read.promise;
      requireThat(reviewed(), 'Activity inspection was cancelled because the page changed.', 'cancelled');
      requireThat(result && Object.hasOwn(result, 'item'), 'This T3 host does not support activity inspection.', 'unsupported');
      requireThat(result.item != null, 'This activity item is no longer available.', 'unavailable');
      const detail = activityDetail(result.item, a);
      requireThat(Buffer.byteLength(JSON.stringify(detail)) <= ACTIVITY_LIMIT.detailFrame, 'Activity details exceed the supported budget.', 'too_large');
      return detail;
    } catch (error) {
      if (signal?.aborted || !reviewed()) throw new Fault('cancelled', 'Activity inspection was cancelled because the page changed.');
      // Reads never acquire an uncertain mutation identity or a journal record.
      if (error instanceof Fault) throw new Fault(error.code, error.code === 'upstream_rejected'
        ? 'T3 could not read this activity item; check installed-host support or open it in full T3.' : error.message);
      throw new Fault('upstream', 'Could not inspect this activity item.');
    } finally { signal?.removeEventListener('abort', cancel); }
  }
  async modelCatalog(p) {
    const config = await this.rpc.call('server.getConfig', {}).promise;
    const result=catalog(config, p);this.rememberProvider(result.providerIdentity);return result;
  }
  rememberProvider(identity) {
    if (!identity) return;
    this.providerIdentityBinding=this.probeBinding();
    // Keep only narrow labels explicitly returned by an already requested
    // native catalog/defaults read. This never starts account discovery.
    this.providerIdentities.delete(identity.instanceId);
    this.providerIdentities.set(identity.instanceId,structuredClone(identity));
    while (this.providerIdentities.size > 64) this.providerIdentities.delete(this.providerIdentities.keys().next().value);
    if (this.current?.modelSelection?.instanceId === identity.instanceId) {
      this.current.providerIdentity=structuredClone(identity);this.emitView();
    }
  }
  async bounded(threadId, signal) {
    this.stats.snapshots++; this.mark('bounded-start');
    const s = await this.http(`/api/orchestration/threads/${encodeURIComponent(threadId)}/bounded`, 'GET', true, signal);
    targetThread(s, threadId);
    requireThat(Number.isSafeInteger(s.snapshotSequence), 'Missing snapshot sequence.', 'incompatible');
    this.mark('bounded-ready'); return s;
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
    this.refreshing = false; this.refreshAgain = false; this.refreshUrgent = false; this.duringRefresh = new Map();
    if (this.current?.live) { this.setState('unavailable'); this.emitView(false, true); }
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
      this.current = modelView(snapshot, threadId,this.providerIdentities); this.following = true;
      this.snapshotSequence = snapshot.snapshotSequence; this.lastStreamSequence = snapshot.snapshotSequence;
      this.setState('synchronizing'); this.emitView(true, true);
      const sub = this.rpc.call('orchestration.subscribeThread', { threadId,
        afterSequence: snapshot.snapshotSequence, requestCompletionMarker: true, acceptBoundedSnapshot: true },
        item => { if (epoch === this.epoch && this.following) this.onEvent(item); });
      this.subscription = sub;
      sub.promise.catch(e => {
        if (epoch === this.epoch && !this.closed) {
          this.synchronized = false; this.setState('unavailable'); this.emitView(false, true);
          this.emit('status', {state:'unavailable',message:e.message});
        }
      });
      return { threadId };
    } catch (error) {
      if (epoch === this.epoch && !this.closed) {
        // A failed Latest must leave the historical page explicitly historical.
        this.following = !historical;
        this.setState('unavailable'); this.emitView(false, true);
      }
      throw error;
    }
  }
  emitView(full = false, urgent = false) {
    if (this.closed) return;
    this.forceSnapshot ||= full;
    if (urgent) { this.flushView(); return; }
    if (this.emitTimer) return;
    this.emitTimer = setTimeout(() => { this.emitTimer = null; this.flushView(); }, SCHEDULING.frameMs);
  }
  flushView() {
    clearTimeout(this.emitTimer); this.emitTimer = null;
    if (!this.current || this.closed) return;
    this.stats.flushes++; this.mark('flush-start');
    // One exact row serialization per flush serves budget validation and
    // comparison. Retain only the current bounded page's immutable copies.
    const rowJSON = new Map();
    trimView(this.current, rowJSON); this.stats.rowSerializations += rowJSON.size;
    trimActivity(this.current.activity, this.current.sequence);
    const {items:currentRows, ...currentMetadata} = this.current;
    const previous = this.sentView, oldRows = new Map((previous?.items ?? []).map(row => [row.key, row]));
    const nextRows = currentRows.map(row => this.sentRowJSON.get(row.key) === rowJSON.get(row) && oldRows.has(row.key)
      ? oldRows.get(row.key) : JSON.parse(rowJSON.get(row)));
    const next = {...structuredClone(currentMetadata), items:nextRows};
    const baseRevision = this.revision, revision = baseRevision + 1;
    const frameSize = (event, data) => Buffer.byteLength(JSON.stringify({v:1,environmentId:this.config.environmentId,event,data})) + 1;
    // Optional tool metadata must not displace exact messages or request
    // controls that already fit the companion-v1 frame. Explicit activity
    // reads still expose this window when the compact view cannot carry it.
    if (next.activity) {
      let total = frameSize('view', {...next, revision});
      const withoutActivityValue = total - Buffer.byteLength(JSON.stringify(next.activity));
      while (total > LIMIT.frame && next.activity.items.length) {
        next.activity.items.shift(); next.activity.limited = true; next.activity.hasMore = true;
        if (next.activity.items.length) next.activity.historyCursor = historyAnchor(next.activity.items[0], next.sequence);
        total = withoutActivityValue + Buffer.byteLength(JSON.stringify(next.activity));
      }
      if (total > LIMIT.frame) delete next.activity;
    }
    const { items, revision: ignored, ...metadata } = next;
    const nextRowJSON = new Map(currentRows.map(row => [row.key, rowJSON.get(row)]));
    const upserts = items.filter(row => nextRowJSON.get(row.key) !== this.sentRowJSON.get(row.key));
    const keys = new Set(items.map(row => row.key));
    const removed = [...oldRows.keys()].filter(key => !keys.has(key));
    let full = this.forceSnapshot || !previous || previous.threadId !== next.threadId;
    this.forceSnapshot = false;
    const metadataJSON = JSON.stringify(metadata);
    const sameOrder = previous?.items.length === items.length && items.every((row,i) => previous.items[i].key === row.key);
    if (!full && upserts.length === 0 && removed.length === 0 && sameOrder && metadataJSON === this.sentMetadataJSON) return;
    const delta = {threadId:next.threadId,baseRevision,revision,upserts,removed,order:items.map(row=>row.key),metadata};
    // Delta order/removal keys can cost more than a full bounded replacement.
    if (!full && frameSize('delta', delta) > LIMIT.frame) full = true;
    this.revision = revision; this.stats.frames++; this.mark('frame-ready',{full,rows:items.length});
    if (full) this.emit('view', { ...next, revision });
    else this.emit('delta', delta);
    this.sentView = next; this.sentRowJSON = nextRowJSON; this.sentMetadataJSON = metadataJSON;
  }
  onEvent(item) {
    if (!this.following || this.closed) return;
    this.stats.rpcEvents++;
    if (item.kind === 'synchronized') { this.mark('subscription-synchronized'); this.synchronized = true; this.retryDelay = 1000; this.setState('live'); this.emitView(false, true); return; }
    if (item.kind === 'snapshot') {
      this.synchronized = false;
      this.current = modelView(item,this.watchId,this.providerIdentities); this.snapshotSequence = item.snapshotSequence; this.lastStreamSequence = item.snapshotSequence;
      this.setState('synchronizing'); this.emitView(true, true); return;
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
    if (event.type === 'turn-item.updated' && ACTIVITY_KINDS.has(payload?.type)) {
      const old = this.current?.activity?.items.find(r => r.sourceThreadId === this.watchId && r.itemId === payload.id);
      if (old) {
        const next = activityItem({sourceThreadId:old.sourceThreadId, sourceItemId:old.itemId, position:old.position, item:payload});
        Object.assign(old, next);
        if (this.refreshing) this.duringRefresh.set('activity:' + old.key, {sequence:item.sequence, activity:{...old}});
        this.emitView(); return;
      }
      this.scheduleRefresh(); return;
    }
    // Existing visible message updates need neither a whole projection nor a snapshot read.
    if (event.type === 'message.updated' || event.type === 'turn-item.updated') {
      const row = this.current?.items.find(r => r.sourceThreadId === this.watchId &&
        (event.type === 'message.updated' ? r.messageId === payload?.id : r.itemId === payload?.id));
      if (row && typeof (payload.text ?? payload.markdown) === 'string') {
        Object.assign(row, clip(payload.text ?? payload.markdown), { streaming: payload.streaming === true });
        // Native queued user items can acquire a run without changing item ID.
        // A provided causal field is authoritative; omission keeps prior data.
        if (Object.hasOwn(payload, 'runId')) {
          delete row.runId;
          if (payload.runId != null) { try { row.runId = identifier(payload.runId, 'run id'); } catch {} }
        }
        if (this.refreshing) this.duringRefresh.set(row.key, { sequence: item.sequence, row: {...row} });
        this.emitView(); return;
      }
      if (event.type === 'turn-item.updated' && !['user_message','assistant_message','proposed_plan','approval_request','user_input_request','error','system_notice','notification','secret_request'].includes(payload?.type)) return;
      if (event.type === 'message.updated') return; // corresponding turn item supplies canonical visibility
    } else if (!/^(thread\.|run\.|run-attempt\.|provider-thread\.|provider-session\.|provider-turn\.|subagent\.|runtime-request\.|context-transfer\.|checkpoint.rollback)/.test(event.type)) return;
    const urgent = /^runtime-request\./u.test(event.type) || event.type === 'turn-item.updated' &&
      ['approval_request','user_input_request','secret_request'].includes(payload?.type);
    this.scheduleRefresh(urgent);
  }
  scheduleRefresh(urgent = false) {
    if (this.closed || !this.following) return;
    this.refreshUrgent ||= urgent;
    if (this.refreshing) { this.refreshAgain = true; return; }
    if (this.refreshTimer) {
      if (!urgent) return;
      clearTimeout(this.refreshTimer); this.refreshTimer = null;
    }
    const delay = this.refreshUrgent ? 0 : Math.max(SCHEDULING.structureMs, Math.min(SCHEDULING.refreshIntervalMs, this.nextRefreshAt - this.now()));
    this.refreshTimer = setTimeout(() => { this.refreshTimer = null; this.refresh().catch(e => { if (e.code !== 'cancelled') this.emit('status',{state:'unavailable',message:e.message}); }); }, delay);
  }
  async refresh() {
    if (this.refreshing || !this.watchId || !this.following) return;
    const epoch = this.epoch, id = this.watchId;
    const urgent = this.refreshUrgent; this.refreshUrgent = false;
    if (urgent) {this.stats.urgentRefreshes++;this.mark('request-refresh-start');}
    this.refreshing = true; this.refreshAgain = false; this.duringRefresh = new Map();
    this.nextRefreshAt = this.now() + SCHEDULING.refreshIntervalMs;
    const pendingUpdates = this.duringRefresh;
    try {
      const s = await this.bounded(id, this.navigation.signal);
      if (epoch !== this.epoch || this.closed || s.snapshotSequence < this.snapshotSequence) return;
      const next = modelView(s,id,this.providerIdentities);
      next.state = this.synchronized ? 'live' : 'synchronizing'; next.live = next.state === 'live'; next.historical = false;
      for (const pending of pendingUpdates.values()) {
        if (pending.sequence <= s.snapshotSequence) continue;
        if (pending.activity) {
          const at = next.activity?.items.findIndex(r => r.key === pending.activity.key) ?? -1;
          if (at >= 0) { next.activity.items[at] = pending.activity; next.sequence = Math.max(next.sequence, pending.sequence); }
          continue;
        }
        const i = next.items.findIndex(r => r.key === pending.row.key);
        if (i >= 0) { next.items[i] = pending.row; next.sequence = Math.max(next.sequence, pending.sequence); }
      }
      this.current = next; this.snapshotSequence = s.snapshotSequence; this.emitView(false, urgent);
    } catch (error) {
      if (epoch === this.epoch && !this.closed) { this.setState('unavailable'); this.emitView(false, true); }
      throw error;
    } finally {
      pendingUpdates.clear();
      if (epoch === this.epoch) {
        this.refreshing = false;
        if (this.refreshAgain) this.scheduleRefresh(this.refreshUrgent);
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
      this.current = { ...this.current, ...pageRows(page.items, sequence, page.nextCursor, page.hasMoreHistory, HISTORY_BUDGET),
        activity:activityPage(page.items, {scope:'history', sequence, cursor:page.nextCursor, hasMore:page.hasMoreHistory}),
        sequence, observedAt: new Date().toISOString() };
      this.setState('historical'); this.emitView(); return { threadId: id };
    } catch (error) {
      if (epoch === this.epoch && !this.closed) { this.setState('unavailable'); this.emitView(false, true); }
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
          if (op === 'launch-project') {
            receipt.payload=launchProjectCommand(a,await this.launchDefaults(a));
          } else {
            const p = targetThread(await this.bounded(identifier(a.threadId)), a.threadId);
            if (op === 'thread-action') {
              if (['pin','unpin'].includes(a.action)) requireThat(this.descriptor?.capabilities?.threadPinning === true,
                'This server does not advertise native thread pinning. Use native T3 or update its verified adapter.', 'unsupported');
              const shell=await this.http('/api/orchestration/shell');
              receipt.payload=threadActionCommand(a,p,shell.threads?.find(t=>t.id===a.threadId));
            } else {
              const choices = op === 'select-model' || ['send', 'launch'].includes(op) && a.modelSelection !== undefined
                ? await this.modelCatalog(p) : null;
              receipt.payload = op === 'launch' ? launchCommand(a,p,this.descriptor,choices) : modelCommand(op,a,p,this.descriptor,choices);
            }
          }
          receipt.tag = ['launch','launch-project'].includes(op) ? 'orchestration.launchThread' : 'orchestration.dispatchCommand';
        }
        requireThat(!this.closed && this.rpc, 'T3 disconnected before dispatch.', 'disconnected');
        if (this.journal && !receipt.durable) {
          // Persist BEFORE network dispatch. Competing helpers converge on the
          // winner's exact payload, even if their preflight snapshots differ.
          const saved = this.journal.access(a.id, fingerprint, receipt);
          receipt.durable = saved; receipt.payload = saved.payload; receipt.tag = saved.tag;
          if (saved.result) { receipt.result = saved.result; return saved.result; }
        }
        const isLaunch=['launch','launch-project'].includes(op);
        let result;
        if (isLaunch && receipt.uncertain) {
          // Explicit recovery first reconciles the persisted exact native
          // thread/message identity. No title search or duplicate redispatch.
          const payload=receipt.payload;
          const p=targetThread(await this.bounded(identifier(payload.threadId)),payload.threadId);
          const same=p.thread.projectId===payload.projectId &&
            p.thread.runtimeMode===payload.runtimeMode && p.thread.interactionMode===payload.interactionMode &&
            sameSelection(p.thread.modelSelection,payload.modelSelection) &&
            launchWorkspaceMatches(p.thread,payload.workspaceStrategy,a.workspaceRoot);
          const hasMessage=!payload.initialMessage || p.runs.some(r=>r.userMessageId===payload.initialMessage.messageId) ||
            p.visibleTurnItems.some(r=>r.item?.messageId===payload.initialMessage.messageId);
          requireThat(same && hasMessage, 'Launch remains uncertain: exact native thread/message identity is not reconciled; no launch was retried.', 'uncertain_launch');
          result={threadId:payload.threadId,reconciled:true};
        } else result = await this.rpc.call(receipt.tag, receipt.payload).promise;
        if (!(isLaunch ? typeof result?.threadId === 'string' && result.threadId===receipt.payload.threadId : Number.isSafeInteger(result?.sequence) && result.sequence >= 0)) throw new Fault('incompatible', 'T3 returned an unfamiliar acknowledgement; inspect the thread before another action.', true);
        const accepted = isLaunch ? {threadId:result.threadId,...(result.reconciled?{reconciled:true}:{})} : {sequence:result.sequence};
        if (op==='thread-action') {
          accepted.threadId=a.threadId; accepted.action=a.action;
          // Action acceptance remains authentic even when the optional
          // post-read fails. In that case no undo revision is offered.
          try {
            const shell=await this.http('/api/orchestration/shell');
            const current=shell.threads?.find(t=>t.id===a.threadId);
            if (current) accepted.nativeRevision=nativeRevision(current);
          } catch { /* do not turn an acknowledged action into a retry */ }
        }
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
    this.closed = true; this.wantConnection = false; this.probeReceipt = null; this.cancelPreview(); this.cancelNavigation();
    for (const timer of [this.emitTimer,this.refreshTimer,this.retryTimer]) clearTimeout(timer);
    this.emitTimer = null; this.refreshTimer = null; this.retryTimer = null;
    this.subscription?.cancel(); this.rpc?.close(); this.rpc = null;
    for (const controller of this.controllers) controller.abort();
    this.current = null; this.sentView = null; this.sentRowJSON.clear(); this.sentMetadataJSON = null; this.duringRefresh.clear(); this.receipts.clear(); this.providerIdentities.clear();
  }
}
