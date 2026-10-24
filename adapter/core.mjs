// Narrow T3 control/view adaptation. Upstream provenance and MIT notice: ../NOTICE.
import { createHash } from 'node:crypto';

export const SOURCE = '0e7346ccd9427c06161e222319e718e6bd70887f';
export const LIMIT = Object.freeze({ frame: 384 * 1024, input: 96 * 1024,
  upstream: 16 * 1024 * 1024, text: 24 * 1024, view: 144 * 1024,
  items: 80, latestItems: 24, latestView: 48 * 1024, threads: 200, prompt: 64 * 1024 });
export class Fault extends Error {
  constructor(code, message, uncertain = false) { super(message); this.code = code; this.uncertain = uncertain; }
}
export function requireThat(ok, message, code = 'invalid_request') {
  if (!ok) throw new Fault(code, message);
}
export function identifier(value, name = 'id') {
  // Native T3 item/run identities embed escaped thread and request identities.
  // Real delegated-thread responses exceed 256 bytes; keep them intact.
  requireThat(typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= 4096 && !/[\x00-\x1f]/u.test(value), `Invalid ${name}.`);
  return value;
}
export function text(value, max = LIMIT.prompt) {
  requireThat(typeof value === 'string' && Buffer.byteLength(value) <= max, 'Text exceeds the supported byte limit.');
  return value;
}
export function clip(value, bytes = LIMIT.text) {
  const raw = Buffer.from(typeof value === 'string' ? value : '', 'utf8');
  if (raw.length <= bytes) return { text: raw.toString(), truncated: false };
  let end = bytes;
  while (end > 0 && (raw[end] & 0xc0) === 0x80) end--;
  return { text: raw.subarray(0, end).toString(), truncated: true };
}
export function stable(value) {
  if (Array.isArray(value)) return value.map(stable);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(k => [k, stable(value[k])]));
  return value;
}
export function hash(value) { return createHash('sha256').update(JSON.stringify(stable(value))).digest('hex'); }
export function validateDescriptor(d, expected) {
  requireThat(d && d.environmentId === expected, 'The endpoint is not the configured T3 environment.', 'identity_mismatch');
  requireThat(d.orchestrationProtocolVersion === 2, 'This adapter requires T3 orchestration protocol 2. Update the host adapter, not the native client.', 'incompatible');
  requireThat(typeof d.serverVersion === 'string', 'Missing T3 server version.', 'incompatible');
  return d;
}
export function targetThread(snapshot, expected) {
  const p = snapshot?.projection;
  requireThat(p?.thread?.id === expected && Array.isArray(p.visibleTurnItems) && Array.isArray(p.runtimeRequests) && Array.isArray(p.runs), 'Unsupported bounded T3 snapshot.', 'incompatible');
  return p;
}
const READABLE = new Set(['user_message', 'assistant_message', 'proposed_plan', 'system_notice', 'notification', 'error']);
export function messageRow(row) {
  const item = row?.item;
  if (!item || !READABLE.has(item.type)) return null;
  const source = identifier(row.sourceThreadId ?? item.threadId, 'source thread');
  const id = identifier(row.sourceItemId ?? item.id, 'source item');
  const body = item.text ?? item.markdown ?? item.message ?? item.summary ?? item.failure?.message ?? '';
  let runId;
  if (item.runId != null) { try { runId = identifier(item.runId, 'run id'); } catch {} }
  return { key: `${source}/${id}`, sourceThreadId: source, itemId: id,
    ...(runId ? { runId } : {}),
    messageId: item.messageId ?? null, kind: item.type,
    position: Number.isSafeInteger(row.position) && row.position >= 0 ? row.position : 0,
    ...clip(body), streaming: item.streaming === true,
    attachmentCount: Array.isArray(item.attachments) ? item.attachments.length : 0 };
}
function fitRow(row) {
  while (Buffer.byteLength(JSON.stringify(row)) + 3 > LIMIT.view) {
    requireThat(row.text.length > 0, 'Message metadata exceeds the view budget.', 'too_large');
    row = { ...row, ...clip(row.text, Math.floor(Buffer.byteLength(row.text) / 2)), truncated: true };
  }
  return row;
}
const latestBudget = Object.freeze({ items: LIMIT.latestItems, view: LIMIT.latestView });
export const HISTORY_BUDGET = Object.freeze({ items: LIMIT.items, view: LIMIT.view });
export function boundedRows(rows, budget = latestBudget) {
  const result = []; let bytes = 2;
  for (let i = rows.length - 1; i >= 0 && result.length < budget.items; i--) {
    const candidate = messageRow(rows[i]);
    if (!candidate) continue;
    const row = fitRow(candidate);
    const cost = Buffer.byteLength(JSON.stringify(row)) + 1;
    // The page target must not shorten a single row beyond its existing cap:
    // there is no continuation cursor for the remainder of a message.
    if (result.length && bytes + cost > budget.view) break;
    result.unshift(row); bytes += cost;
  }
  return result;
}
// Adapted from pinned upstream threadHistoryPaging.ts encodeThreadHistoryCursor.
// Only this host compatibility boundary constructs anchors; clients treat them
// as opaque. Identity is authoritative (page positions are renumbered upstream).
export function historyAnchor(row, sequence) {
  requireThat(Number.isSafeInteger(sequence) && sequence >= 0, 'Missing history sequence.', 'incompatible');
  return Buffer.from(JSON.stringify({ v: 1, seq: sequence, st: row.sourceThreadId,
    si: row.itemId, p: row.position ?? 0 })).toString('base64url');
}
export function trimView(v) {
  const budget = v.historical ? HISTORY_BUDGET : latestBudget;
  let bytes = 2; const kept = [];
  for (const candidate of [...v.items].reverse()) {
    const row = fitRow(candidate);
    const cost = Buffer.byteLength(JSON.stringify(row)) + 1;
    if (kept.length >= budget.items || kept.length && bytes + cost > budget.view) break;
    kept.unshift(row); bytes += cost;
  }
  if (kept.length < v.items.length) {
    v.limited = true; v.hasMore = true;
    v.historyCursor = historyAnchor(kept[0], v.sequence);
  }
  if (bytes > budget.view) v.limited = true;
  v.items = kept; return v;
}
export function pageRows(rows, sequence, cursor, hasMore, budget = latestBudget) {
  const items = boundedRows(rows, budget);
  const trimmed = rows.filter(r => READABLE.has(r.item?.type)).length > items.length;
  return { items, historyCursor: trimmed ? historyAnchor(items[0], sequence) : cursor ?? null,
    hasMore: trimmed || hasMore === true,
    limited: trimmed || Buffer.byteLength(JSON.stringify(items)) > budget.view };
}
const DECISIONS = new Set(['accept', 'acceptForSession', 'acceptAlways', 'decline', 'cancel']);
export function requests(p) {
  const candidates = new Map([...(p.turnItems ?? []), ...p.visibleTurnItems.map(r => r.item)]
    .filter(i => i?.requestId).map(i => [i.requestId, i]));
  return p.runtimeRequests.filter(r => r.status === 'pending').slice(0, 8).map(r => {
    const item = candidates.get(r.id);
    const complete = { request: r, item: item ?? null };
    const body = item?.prompt ?? item?.title ?? 'Review this request in T3.';
    const short = clip(body, 12 * 1024);
    const standardApproval = ['command', 'file-read', 'file-change'].includes(item?.requestKind);
    const options = item?.options ?? (standardApproval ? [{ decision: 'accept', label: 'Approve once' }, { decision: 'decline', label: 'Decline' }] : []);
    const supported = ['live', 'message'].includes(r.responseCapability?.type) && item &&
      ['approval_request', 'user_input_request'].includes(item.type) &&
      (item.type !== 'approval_request' || options.length > 0);
    const questions = item?.questions ?? [];
    const tooLarge = Buffer.byteLength(JSON.stringify({ options, questions })) > 12 * 1024;
    const safeOptions = options.filter(o => DECISIONS.has(o.decision)).map(o => ({ decision: o.decision, label: o.label, ...(o.warning ? {warning: o.warning} : {}) }));
    return { id: r.id, revision: hash(complete), kind: item?.type ?? r.kind,
      prompt: short.text, supported: Boolean(supported && !short.truncated && !tooLarge),
      options: tooLarge ? [] : safeOptions,
      questions: tooLarge ? [] : questions,
      limitation: !supported ? 'Use full T3 for this request type.' :
        short.truncated || tooLarge ? 'Full request exceeds this compact view; review in T3.' : null };
  });
}
// Native provider children do not necessarily own an app run. Display their
// correlated provider activity without inventing a run ID or Stop capability.
export function activity(p) {
  const active = [...p.runs].reverse().find(r => ['preparing', 'starting', 'running', 'waiting'].includes(r.status));
  if (active) return { status: active.status, activeRunId: active.id, source: 'app-run' };
  const last = p.runs.at(-1);
  const nativeChild = p.thread.lineage?.relationshipToParent === 'subagent';
  const provider = (p.providerThreads ?? []).find(t => t.id === p.thread.activeProviderThreadId &&
    (t.appThreadId == null || t.appThreadId === p.thread.id));
  if (nativeChild && (!last || provider && provider.id !== last.providerThreadId)) {
    if (!provider) return { status: 'unreported', activeRunId: null, source: 'unreported' };
    const turns = (p.providerTurns ?? []).filter(t => t.providerThreadId === provider.id)
      .sort((a,b) => (b.ordinal ?? 0) - (a.ordinal ?? 0));
    const turn = turns[0];
    let status = turn?.status;
    if (status === 'pending') status = 'preparing';
    if (!status) status = ({active:'running',idle:'idle',error:'failed'})[provider.status] ?? 'unreported';
    if (['preparing','running'].includes(status) && p.runtimeRequests.some(r => r.status === 'pending')) status = 'waiting';
    return { status, activeRunId: null, source: 'provider-turn', providerThreadId: provider.id, providerTurnId: turn?.id ?? null };
  }
  return { status: last?.status ?? 'idle', activeRunId: null, source: last ? 'app-run' : 'none' };
}
export function view(snapshot, expected) {
  const p = targetThread(snapshot, expected);
  const path = p.thread.worktreePath;
  // Display the same authoritative binding launch() uses. Never clip a path
  // into a different apparent workspace or turn malformed data into root.
  const workspace = path == null ? 'root' : typeof path === 'string' && path.length > 0 &&
    Buffer.byteLength(path) <= 4096 && !/[\x00-\x1f\x7f]/u.test(path) ? path : null;
  const currentActivity = activity(p);
  const pending = requests(p), page = pageRows(p.visibleTurnItems, snapshot.snapshotSequence, snapshot.historyCursor, snapshot.hasMoreHistory);
  return { threadId: expected, title: clip(p.thread.title, 512).text,
    projectId: p.thread.projectId, ...(workspace === null ? {} : { workspace }),
    model: clip(p.thread.modelSelection?.model ?? '',256).text,
    mode: p.thread.runtimeMode, status: currentActivity.status,
    activeRunId: currentActivity.activeRunId, requests: pending,
    ...page,
    limited: page.limited || snapshot.payloadBudgetExceeded === true || p.runtimeRequests.filter(r => r.status === 'pending').length > 8,
    sequence: snapshot.snapshotSequence, observedAt: new Date().toISOString() };
}
// Commands adapted from T3 packages/client-runtime/src/operations/commands.ts.
// Never route user prompts through agent MCP or inject provider-native commands.
export function command(op, a, p, d) {
  const id = identifier(a.id, 'operation id'), threadId = identifier(a.threadId, 'thread id');
  requireThat(p.thread.id === threadId, 'Thread mismatch.', 'identity_mismatch');
  requireThat(!p.thread.deletedAt && !p.thread.archivedAt, 'This thread is deleted or archived.', 'unavailable');
  if (op === 'send') {
    const message = text(a.message); requireThat(message.trim().length > 0, 'Empty message.');
    const mode = a.mode ?? 'auto'; requireThat(['auto', 'queue', 'steer'].includes(mode), 'Invalid delivery mode.');
    requireThat(d.capabilities?.serverResolvedCommandContext === true, 'Server-side delivery selection is unavailable; update the host adapter.', 'incompatible');
    return { type: 'message.dispatch', commandId: id, threadId, messageId: `${id}:message`,
      createdBy: 'user', creationSource: 'web', text: message, attachments: [],
      dispatchMode: { type: mode === 'queue' ? 'queue_after_active' : 'start_immediately' },
      ...(mode === 'queue' ? {} : { deliveryIntent: mode }) };
  }
  if (op === 'stop') {
    // Match the ordinary client's active-turn Stop, including queue hold.
    const run = [...p.runs].reverse().find(r => ['preparing', 'starting', 'running', 'waiting'].includes(r.status));
    requireThat(run && run.id === a.runId, 'The active turn changed. Review its current state before stopping.', 'stale');
    return { type: 'run.interrupt', commandId: id, threadId, runId: run.id, holdQueue: true };
  }
  if (op === 'answer') {
    const current = requests(p).find(r => r.id === a.requestId);
    requireThat(current?.supported && current.revision === a.revision, 'The request changed or is unsupported. Refresh and review it.', 'stale');
    if (current.kind === 'approval_request') {
      requireThat(current.options.some(o => o.decision === a.decision), 'That approval option is not offered.');
      return { type: 'runtime-request.respond', commandId: id, threadId, requestId: current.id, decision: a.decision };
    }
    requireThat(a.answers && typeof a.answers === 'object' && !Array.isArray(a.answers), 'Missing answers.');
    requireThat(Buffer.byteLength(JSON.stringify(a.answers)) <= LIMIT.prompt, 'Answers exceed the byte limit.');
    requireThat(Object.keys(a.answers).every(k => current.questions.some(q => q.id === k)), 'Unknown question.');
    for (const q of current.questions) {
      const answer = a.answers[q.id];
      requireThat(q.required === false || answer !== undefined, `Answer required: ${q.header}`);
    }
    return { type: 'runtime-request.respond', commandId: id, threadId, requestId: current.id, answers: a.answers };
  }
  throw new Fault('invalid_request', 'Unknown command.');
}
export function launch(a, p, modelSelection = p.thread?.modelSelection) {
  const id = identifier(a.id), message = text(a.message), title = text(a.title, 512);
  requireThat(title.trim() && message.trim(), 'Title and message are required.');
  requireThat(p.thread?.projectId && p.thread.modelSelection, 'Select an existing project thread first.');
  requireThat(p.thread.id === a.threadId, 'Thread mismatch.', 'identity_mismatch');
  requireThat(!p.thread.deletedAt && !p.thread.archivedAt, 'This thread is deleted or archived.', 'unavailable');
  return { commandId: id, creationSource: 'web', threadId: id,
    projectId: p.thread.projectId, title, modelSelection,
    runtimeMode: p.thread.runtimeMode, interactionMode: p.thread.interactionMode,
    workspaceStrategy: p.thread.worktreePath ? { type: 'existing_worktree', worktreePath: p.thread.worktreePath,
      ...(p.thread.branch ? { branch: p.thread.branch } : {}) } : { type: 'root' },
    initialMessage: { messageId: `${id}:message`, text: message, attachments: [] } };
}
