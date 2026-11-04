#!/usr/bin/env node
// Deterministic native-contract fixture for the Rust client's terminal tests.
// It runs the REAL companion admission, T3 projection, stream reducer and host
// journal. Only native HTTP/RPC endpoints are replaced; no T3 service is used.
// Configure through a private --config host.json. Optional fixture fields:
// { stateFile, logFile, dropAckOnce, rejectSend, ackDelayMs, disconnectAfterMs, unavailable, stream }.
import { readFileSync, writeFileSync, appendFileSync, existsSync, realpathSync, renameSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { run } from '../adapter/main.mjs';
import { T3 } from '../adapter/t3.mjs';
import { Fault, hash } from '../adapter/core.mjs';

const initialTime = Date.parse('2026-11-02T12:00:00.000Z');
const selection = { instanceId: 'fixture-provider', model: 'fixture-model', options: [{ id: 'reasoningEffort', value: 'medium' }] };
const optionDescriptors = [
  { id: 'reasoningEffort', type: 'select', label: 'Reasoning', options: [
    { id: 'medium', label: 'Medium' }, { id: 'high', label: 'High' },
  ] },
  { id: 'serviceTier', type: 'select', label: 'Speed', options: [
    { id: 'standard', label: 'Standard' }, { id: 'fast', label: 'Fast' },
  ] },
];
const serverConfig = {
  settings: { defaultModelSelection: selection, defaultRuntimeMode: 'approval-required',
    defaultThreadEnvMode: 'local', projectSettingsFolded: true, projectSettingsOverrides: {} },
  providers: [{ instanceId: 'fixture-provider', displayName: 'Fixture provider', driver: 'codex',
    enabled: true, installed: true, status: 'ready', auth: { status: 'authenticated', label: 'Fixture account' },
    models: ['fixture-model', 'fixture-fast'].map((slug, index) => ({ slug,
      name: index ? 'Fixture fast' : 'Fixture model', isDefault: !index, capabilities: { optionDescriptors } })) }],
};
const wrap = (threadId, item) => ({ sourceThreadId: threadId, sourceItemId: item.id,
  visibility: 'local', position: 0, item: { threadId, ...item } });

function initialState() {
  const rows = new Map();
  const make = (id, title, status, relationshipToParent = null) => {
    const runId = `run-${id}`;
    const thread = { id, title, projectId: 'project-a', modelSelection: structuredClone(selection),
      runtimeMode: 'approval-required', interactionMode: 'default', worktreePath: null,
      updatedAt: new Date(initialTime).toISOString(), status, latestRunId: runId,
      activeRunId: ['running', 'waiting'].includes(status) ? runId : null,
      lineage: { relationshipToParent, parentThreadId: relationshipToParent ? 'thread-a' : null, rootThreadId: 'thread-a' },
      settledOverride: null, queuedRunCount: 0,
    };
    const visibleTurnItems = [
      wrap(id, { id: `${id}-user`, messageId: `${id}-message`, runId, type: 'user_message', text: `Inspect ${title}.` }),
      wrap(id, { id: `${id}-answer`, messageId: `${id}-response`, runId, type: 'assistant_message',
        text: `## ${title}\n\nReady for review.\n\n- Exact thread identity retained\n- Unicode: 日本語 café 🙂\n\n\`cargo test\` completed.`, streaming: status === 'running' }),
      wrap(id, { id: `${id}-command`, runId, type: 'command_execution', command: 'cargo test',
        output: 'fixture tests passed\n', status: 'completed', exitCode: 0 }),
    ];
    const projection = { thread, visibleTurnItems, turnItems: visibleTurnItems.map(r => r.item),
      runtimeRequests: [], runs: [{ id: runId, status, userMessageId: `${id}-message`, modelSelection: selection }],
      providerThreads: [], providerSessions: [] };
    rows.set(id, projection);
    return projection;
  };
  make('thread-a', 'Active implementation', 'running');
  const approval = make('thread-approval', 'Permission needed', 'waiting');
  const approvalItem = { id: 'approval-item', threadId: 'thread-approval', type: 'approval_request',
    requestId: 'request-approval', requestKind: 'command', prompt: 'Run the fixture verification?',
    options: [{ decision: 'accept', label: 'Approve once' }, { decision: 'decline', label: 'Decline' }] };
  approval.turnItems.push(approvalItem);
  approval.runtimeRequests.push({ id: approvalItem.requestId, kind: 'approval', status: 'pending', responseCapability: { type: 'live', providerSessionId: 'fixture-session' } });
  approval.thread.pendingRuntimeRequest = { id: approvalItem.requestId, kind: 'approval' };
  const question = make('thread-question', 'Question to answer', 'waiting');
  const questionItem = { id: 'question-item', threadId: 'thread-question', type: 'user_input_request',
    requestId: 'request-question', prompt: 'Choose the next fixture step.', questions: [
      { id: 'choice', header: 'Next step', question: 'What should happen next?', required: true,
        options: [{ label: 'Continue', description: 'Continue the fixture.' }, { label: 'Review', description: 'Review first.' }] },
    ] };
  question.turnItems.push(questionItem);
  question.runtimeRequests.push({ id: questionItem.requestId, kind: 'user_input', status: 'pending', responseCapability: { type: 'live', providerSessionId: 'fixture-session' } });
  question.thread.pendingRuntimeRequest = { id: questionItem.requestId, kind: 'user_input' };
  make('thread-review', 'Finished refactor', 'completed');
  make('thread-child', 'Hidden child needle', 'running', 'subagent');
  const fork = make('thread-fork', 'Alternative design', 'completed', 'fork');
  fork.visibleTurnItems[1].item.text += '\nA server-only-marker lives inside this native message.';
  make('thread-settled', 'Settled work', 'completed').thread.settledOverride = 'settled';
  return { sequence: 10, effects: 0, dropped: false, receipts: {},
    projects: [{ id: 'project-a', title: 'Fixture project', workspaceRoot: '/fixture/project' },
      { id: 'project-empty', title: 'Empty project', workspaceRoot: '/fixture/empty' }],
    projections: Object.fromEntries(rows) };
}

export function createFixture(config, emit) {
  const options = config.fixture ?? {};
  const ackDelayMs = options.ackDelayMs ?? 0;
  if (!Number.isSafeInteger(ackDelayMs) || ackDelayMs < 0 || ackDelayMs > 2000)
    throw new Fault('invalid_request', 'Fixture acknowledgement delay must be 0–2000ms.');
  let state = options.stateFile && existsSync(options.stateFile)
    ? JSON.parse(readFileSync(options.stateFile, 'utf8')) : initialState();
  const timers = new Set(), streams = new Set();
  const log = value => { if (options.logFile) appendFileSync(options.logFile, JSON.stringify(value) + '\n', { mode: 0o600 }); };
  const persist = () => {
    if (!options.stateFile) return;
    // Metadata helpers may read while the active synthetic stream commits.
    // Publish a whole JSON value, never an empty/truncated intermediate file.
    const temporary = `${options.stateFile}.${process.pid}.tmp`;
    writeFileSync(temporary, JSON.stringify(state), { mode: 0o600 });
    renameSync(temporary, options.stateFile);
  };
  const later = (fn, delay) => { const timer = setTimeout(() => { timers.delete(timer); fn(); }, delay); timers.add(timer); return timer; };
  const snapshot = id => {
    const projection = state.projections[id];
    if (!projection) throw new Fault('unavailable', 'Fixture thread not found.');
    return structuredClone({ snapshotSequence: state.sequence, projection, historyCursor: `history-${id}`, hasMoreHistory: true });
  };
  const broadcast = (threadId, type, payload) => {
    state.sequence++;
    for (const stream of streams) if (stream.threadId === threadId)
      stream.callback({ kind: 'event', sequence: state.sequence, event: { threadId, type, payload: structuredClone(payload) } });
  };
  const streamAnswer = id => {
    if (options.stream === false) return;
    for (let step = 1; step <= 3; step++) later(() => {
      const p = state.projections[id];
      if (!p) return;
      const row = p.visibleTurnItems.filter(r => r.item.type === 'assistant_message').at(-1);
      if (!row) return;
      row.item.text += `\nStreaming update ${step}.`;
      row.item.streaming = step < 3;
      broadcast(id, 'turn-item.updated', row.item);
      persist();
    }, step * 100);
  };
  const native = async (tag, payload) => {
    log({ native: tag, payload });
    if (tag === 'server.getConfig') return structuredClone(serverConfig);
    if (tag === 'server.getSettings') return structuredClone(serverConfig.settings);
    if (tag === 'projects.readFile') return { exists: false, contents: '', truncated: false };
    if (tag === 'vcs.listRefs') return { isRepo: true, refs: [{ name: 'main', current: true, isDefault: true, isRemote: false }] };
    if (tag === 'review.getDiffPreview') return { cwd: payload.cwd, generatedAt: new Date(initialTime).toISOString(),
      sources: [{ kind: 'working-tree', title: 'Fixture changes', diffHash: 'fixture-diff', truncated: false,
        diff: 'diff --git a/src/main.rs b/src/main.rs\n--- a/src/main.rs\n+++ b/src/main.rs\n@@ -1 +1 @@\n-old()\n+new()\n',
        files: [{ path: 'src/main.rs', additions: 1, deletions: 1 }] }] };
    if (tag === 'orchestration.getTurnItem') return { item: snapshot(payload.threadId).projection.turnItems.find(i => i.id === payload.itemId) ?? null };
    if (tag === 'orchestration.searchThreads') return { matches: Object.values(state.projections)
      .filter(p => p.thread.title.toLowerCase().includes(payload.query.toLowerCase()) || p.visibleTurnItems.some(r => r.item.text?.toLowerCase().includes(payload.query.toLowerCase())))
      .slice(0, payload.limit ?? 50).map(p => ({ threadId: p.thread.id, projectId: p.thread.projectId })) };
    if (!['orchestration.dispatchCommand', 'orchestration.launchThread'].includes(tag)) throw new Fault('unsupported', `Unknown fixture RPC: ${tag}`);
    const fingerprint = hash({ tag, payload });
    const prior = state.receipts[payload.commandId];
    if (prior) {
      if (prior.fingerprint !== fingerprint) throw new Fault('invalid_request', 'Fixture command ID reused with different payload.');
      return prior.result;
    }
    let p = state.projections[payload.threadId];
    if (tag === 'orchestration.launchThread') {
      const runId = `run-${payload.threadId}`;
      const thread = { id: payload.threadId, projectId: payload.projectId, title: payload.title,
        modelSelection: payload.modelSelection, runtimeMode: payload.runtimeMode, interactionMode: payload.interactionMode,
        worktreePath: payload.workspaceStrategy.type === 'root' ? null : `/fixture/worktrees/${payload.threadId}`,
        branch: payload.workspaceStrategy.type === 'root' ? null : 'fixture-new',
        status: payload.initialMessage ? 'running' : 'idle', updatedAt: new Date(initialTime + state.sequence * 1000).toISOString(),
        lineage: { relationshipToParent: null, parentThreadId: null, rootThreadId: payload.threadId },
        latestRunId: payload.initialMessage ? runId : null, activeRunId: payload.initialMessage ? runId : null };
      const visible = payload.initialMessage ? [wrap(thread.id, { id: `${thread.id}-user`, messageId: payload.initialMessage.messageId,
        runId, type: 'user_message', text: payload.initialMessage.text })] : [];
      p = { thread, visibleTurnItems: visible, turnItems: visible.map(r => r.item), runtimeRequests: [],
        runs: payload.initialMessage ? [{ id: runId, status: 'running', userMessageId: payload.initialMessage.messageId }] : [] };
      state.projections[thread.id] = p;
    } else {
      if (!p) throw new Fault('unavailable', 'Fixture thread not found.');
      if (payload.type === 'message.dispatch') {
        if (options.rejectSend) {
          if (ackDelayMs) await new Promise(resolve => later(resolve, ackDelayMs));
          throw new Fault('invalid_request', 'Fixture rejected this exact send.');
        }
        const row = wrap(p.thread.id, { id: `${payload.commandId}-user`, messageId: payload.messageId, type: 'user_message', text: payload.text });
        p.visibleTurnItems.push(row); p.turnItems.push(row.item);
        const answer = wrap(p.thread.id, { id: `${payload.commandId}-answer`, messageId: `${payload.commandId}-response`, type: 'assistant_message', text: 'Accepted exact fixture message.', streaming: true });
        p.visibleTurnItems.push(answer); p.turnItems.push(answer.item);
        broadcast(p.thread.id, 'turn-item.updated', row.item);
        streamAnswer(p.thread.id);
      } else if (payload.type === 'run.interrupt') {
        const run = p.runs.find(r => r.id === payload.runId);
        if (run) run.status = 'interrupted';
        p.thread.status = 'interrupted'; p.thread.activeRunId = null;
        broadcast(p.thread.id, 'run.updated', run);
      } else if (payload.type === 'runtime-request.respond') {
        const request = p.runtimeRequests.find(r => r.id === payload.requestId);
        if (request) request.status = 'resolved';
        p.thread.pendingRuntimeRequest = null;
        broadcast(p.thread.id, 'runtime-request.updated', request);
      } else if (payload.type === 'thread.model-selection.set') p.thread.modelSelection = payload.modelSelection;
      else if (payload.type === 'thread.settle') p.thread.settledOverride = 'settled';
      else if (payload.type === 'thread.unsettle') p.thread.settledOverride = 'active';
      else if (payload.type === 'thread.snooze') p.thread.snoozedUntil = payload.snoozedUntil;
      else if (payload.type === 'thread.unsnooze') p.thread.snoozedUntil = null;
      else if (payload.type === 'thread.pin') p.thread.pinnedAt = new Date(initialTime).toISOString();
      else if (payload.type === 'thread.unpin') p.thread.pinnedAt = null;
      else throw new Fault('unsupported', `Unknown fixture mutation: ${payload.type}`);
    }
    state.effects++;
    state.sequence++;
    p.thread.updatedAt = new Date(initialTime + state.sequence * 1000).toISOString();
    const result = tag === 'orchestration.launchThread' ? { threadId: payload.threadId } : { sequence: state.sequence };
    state.receipts[payload.commandId] = { fingerprint, result };
    persist();
    if (ackDelayMs) await new Promise(resolve => later(resolve, ackDelayMs));
    if (options.dropAckOnce && !state.dropped) {
      state.dropped = true; persist();
      throw new Fault('timeout', 'Fixture lost acknowledgement after the native effect.', true);
    }
    return result;
  };
  const backend = new T3(config, emit, {
    fetch: async (url, init) => {
      if (options.unavailable) throw new Error('Fixture host unavailable');
      const path = new URL(url).pathname;
      log({ http: init?.method ?? 'GET', path });
      let value;
      if (path === '/.well-known/t3/environment') value = { environmentId: config.environmentId, serverVersion: '0.0.0-fixture',
        orchestrationProtocolVersion: 2, platform: { machine: 'server' }, capabilities: { serverResolvedCommandContext: true, threadPinning: true } };
      else if (path === '/api/auth/websocket-ticket') value = { ticket: 'fixture-ticket' };
      else if (path === '/api/orchestration/shell') value = { snapshotSequence: state.sequence, projects: state.projects, threads: Object.values(state.projections).map(p => p.thread) };
      else if (path.endsWith('/bounded')) value = snapshot(decodeURIComponent(path.split('/')[4]));
      else if (path.endsWith('/history')) {
        const id = decodeURIComponent(path.split('/')[4]);
        value = { snapshotSequence: state.sequence, items: [wrap(id, { id: 'older', type: 'assistant_message', text: 'Older fixture history page.' })], nextCursor: null, hasMoreHistory: false };
      } else return new Response('{}', { status: 404 });
      return new Response(JSON.stringify(value), { headers: { 'content-type': 'application/json' } });
    },
    connectSocket: async () => ({
      call(tag, payload, callback) {
        if (tag !== 'orchestration.subscribeThread') return { promise: native(tag, payload), cancel() {} };
        log({ native: tag, payload });
        const stream = { threadId: payload.threadId, callback };
        streams.add(stream);
        let finish;
        const promise = new Promise(resolve => { finish = resolve; });
        later(() => { if (streams.has(stream)) { callback({ kind: 'synchronized' }); streamAnswer(payload.threadId); } }, 1);
        return { promise, cancel() { streams.delete(stream); finish(); } };
      },
      close() { streams.clear(); for (const timer of timers) clearTimeout(timer); timers.clear(); },
    }),
  });
  if (options.disconnectAfterMs) later(() => process.exit(75), options.disconnectAfterMs);
  return backend;
}

if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
  run(process.argv.slice(2), { createBackend: createFixture }).catch(error => {
    process.stderr.write(`Fixture failed: ${error.message}\n`); process.exitCode = 1;
  });
}
