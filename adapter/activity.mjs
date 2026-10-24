// Tool description/output extraction ported and adapted from t3term
// d1d33e89ae16dd09a4ff1e2a9f1570b3416a0c1e src/transcript.rs
// (tool_argument, output_text, tool_output, describe_as). MIT, Krish Garg.
// See LICENSES/t3term-LICENSE.txt and NOTICE. No T3 projector is copied.
import { clip, historyAnchor, identifier, requireThat } from './core.mjs';

export const ACTIVITY_LIMIT = Object.freeze({ items: 24, page: 24 * 1024,
  title: 256, summary: 768, detail: 24 * 1024, detailFrame: 72 * 1024 });
export const ACTIVITY_KINDS = new Set(['command_execution', 'file_change', 'file_search',
  'web_search', 'dynamic_tool', 'subagent', 'todo_list']);
const ARGUMENT_KEYS = ['file_path', 'filePath', 'path', 'command', 'pattern', 'query', 'url', 'skill', 'name', 'prompt'];
const OUTPUT_KEYS = ['text', 'content', 'stdout', 'output', 'result', 'file'];
const scalar = value => typeof value === 'string' ? value : '';
const json = value => value == null ? '' : JSON.stringify(value);
const firstLine = value => scalar(value).split(/\r?\n/, 1)[0];

// Same preference order as t3term. Unknown structured input remains readable.
export function toolArgument(item) {
  const input = item.input;
  if (typeof input === 'string') return firstLine(input);
  if (input && typeof input === 'object' && !Array.isArray(input)) {
    for (const key of ARGUMENT_KEYS) if (typeof input[key] === 'string') return firstLine(input[key]);
    return Object.keys(input).length ? json(input) : '';
  }
  return '';
}

// Text, nested file results, and MCP text-content blocks share this extraction.
// Bound recursion and visited nodes before constructing an inspection response.
export function outputText(value, budget = { nodes: 4096, depth: 0 }) {
  if (--budget.nodes < 0 || budget.depth > 32) { budget.truncated = true; return '…'; }
  if (typeof value === 'string') return value;
  if (value == null) return '';
  if (Array.isArray(value)) {
    const parts = []; let bytes = 0;
    for (const child of value) {
      budget.depth++;
      const next = outputText(child, budget);
      budget.depth--;
      parts.push(next); bytes += Buffer.byteLength(next) + 1;
      if (bytes > ACTIVITY_LIMIT.detail || budget.nodes < 0) { budget.truncated = true; parts.push('…'); break; }
    }
    return parts.join('\n');
  }
  if (typeof value === 'object') {
    for (const key of OUTPUT_KEYS) {
      const nested = value[key];
      if (typeof nested === 'string') return nested;
      if (nested && typeof nested === 'object') {
        budget.depth++;
        const next = outputText(nested, budget);
        budget.depth--;
        return next;
      }
    }
    return json(value);
  }
  return String(value);
}

function describe(item) {
  let title = scalar(item.title), summary = '';
  switch (item.type) {
    case 'command_execution': title ||= 'Command'; summary = firstLine(item.input); break;
    case 'file_change': title ||= 'File change'; summary = scalar(item.fileName); break;
    case 'file_search': title ||= 'File search'; summary = scalar(item.pattern); break;
    case 'web_search': title ||= 'Web search'; summary = (Array.isArray(item.patterns) ? item.patterns : []).filter(v => typeof v === 'string').join(', '); break;
    case 'dynamic_tool': title ||= scalar(item.toolName) || 'Tool'; summary = toolArgument(item); break;
    case 'subagent': title ||= 'Subagent'; summary = scalar(item.progress) || scalar(item.prompt); break;
    case 'todo_list': title ||= 'Tasks'; summary = taskText(item); break;
  }
  return { title, summary };
}
function taskText(item) {
  const tasks = item.items ?? item.steps ?? item.todos ?? [];
  if (!Array.isArray(tasks)) return '';
  return tasks.slice(0, 24).map(t => `${scalar(t?.status) || 'pending'}  ${scalar(t?.text) || scalar(t?.step) || scalar(t?.description)}`).join('\n');
}

export function activityItem(row) {
  const item = row?.item;
  if (!ACTIVITY_KINDS.has(item?.type)) return null;
  const sourceThreadId = identifier(row.sourceThreadId ?? item.threadId, 'activity source thread');
  const itemId = identifier(row.sourceItemId ?? item.id, 'activity item');
  const description = describe(item), title = clip(description.title, ACTIVITY_LIMIT.title);
  const summary = clip(description.summary, ACTIVITY_LIMIT.summary);
  let runId = '';
  if (item.runId != null) { try { runId = identifier(item.runId, 'activity run'); } catch {} }
  return { key: `${sourceThreadId}/${itemId}`, sourceThreadId, itemId, runId,
    kind: item.type, title: title.text, summary: summary.text,
    status: clip(scalar(item.status) || 'unreported', 64).text,
    updatedAt: clip(scalar(item.updatedAt), 128).text, outputOmitted: item.outputOmitted === true,
    truncated: title.truncated || summary.truncated,
    position: Number.isSafeInteger(row.position) && row.position >= 0 ? row.position : 0 };
}

export function activityPage(rows, options = {}) {
  const candidates = (rows ?? []).map(activityItem).filter(Boolean);
  const items = []; let bytes = 2;
  for (const item of [...candidates].reverse()) {
    const cost = Buffer.byteLength(JSON.stringify(item)) + 1;
    requireThat(items.length > 0 || cost + 2 <= ACTIVITY_LIMIT.page,
      'An activity identity exceeds the supported page budget; inspect it in full T3.', 'too_large');
    if (items.length >= ACTIVITY_LIMIT.items || bytes + cost > ACTIVITY_LIMIT.page) break;
    items.unshift(item); bytes += cost;
  }
  const trimmed = candidates.length > items.length;
  const cursor = trimmed && items.length && Number.isSafeInteger(options.sequence)
    ? historyAnchor(items[0], options.sequence) : options.cursor ?? null;
  return { scope: options.scope ?? 'page', items, total: candidates.length,
    limited: trimmed || items.some(item => item.truncated),
    hasMore: trimmed || options.hasMore === true, historyCursor: cursor };
}

// Streaming summaries can grow without another snapshot. Reapply the same
// finite metadata budget without altering the readable transcript or its cursor.
export function trimActivity(page, sequence) {
  if (!page) return;
  let bytes = 2; const items = [];
  for (const item of [...page.items].reverse()) {
    const cost = Buffer.byteLength(JSON.stringify(item)) + 1;
    requireThat(items.length > 0 || cost + 2 <= ACTIVITY_LIMIT.page,
      'An activity identity exceeds the supported page budget; inspect it in full T3.', 'too_large');
    if (items.length >= ACTIVITY_LIMIT.items || bytes + cost > ACTIVITY_LIMIT.page) break;
    items.unshift(item); bytes += cost;
  }
  if (items.length < page.items.length) {
    page.limited = true; page.hasMore = true;
    if (items.length && Number.isSafeInteger(sequence)) page.historyCursor = historyAnchor(items[0], sequence);
  }
  page.items = items;
  page.limited = page.total > items.length || items.some(item => item.truncated);
}

function fileChanges(item) {
  const operations = Array.isArray(item.changes) ? item.changes : [];
  const lines = operations.slice(0, 24).map(change => [scalar(change?.operation),
    scalar(change?.oldPath) ? `${scalar(change.oldPath)} →` : '', scalar(change?.path)].filter(Boolean).join(' '));
  if (operations.length > lines.length) lines.push(`… ${operations.length - lines.length} more changes`);
  if (!lines.length && item.fileName) lines.push(scalar(item.fileName));
  if (Number.isSafeInteger(item.additions) || Number.isSafeInteger(item.deletions)) {
    lines.push(`+${item.additions ?? 0} −${item.deletions ?? 0}`);
  }
  // Upstream can omit patches. A failed edit may carry its error in diffStr.
  if (typeof item.diffStr === 'string') lines.push(item.diffStr);
  return lines.join('\n');
}

export function activityDetail(item, request) {
  requireThat(item && item.id === request.itemId && item.threadId === request.threadId,
    'T3 returned a different activity item.', 'identity_mismatch');
  requireThat(ACTIVITY_KINDS.has(item.type), 'This item type is not supported by the activity inspector.', 'unsupported');
  const description = describe(item);
  let input = '', output = '', outputPresent = false;
  switch (item.type) {
    case 'command_execution': input = scalar(item.input); break;
    case 'dynamic_tool': input = typeof item.input === 'string' ? item.input : json(item.input); break;
    case 'subagent': input = scalar(item.prompt); output = scalar(item.result) || scalar(item.progress); outputPresent = item.result != null || Boolean(item.progress); break;
    case 'file_change': input = scalar(item.fileName); output = fileChanges(item); outputPresent = Boolean(output); break;
    case 'file_search': input = scalar(item.pattern); output = json(item.results); outputPresent = Array.isArray(item.results); break;
    case 'web_search': input = json(item.patterns); output = json(item.results); outputPresent = Array.isArray(item.results); break;
    case 'todo_list': output = taskText(item); outputPresent = Boolean(output); break;
  }
  const budget = {nodes:4096, depth:0, truncated:false};
  if (Object.hasOwn(item, 'output') && item.output != null) { output = outputText(item.output, budget); outputPresent = true; }
  const clippedInput = clip(input, ACTIVITY_LIMIT.detail), clippedOutput = clip(output, ACTIVITY_LIMIT.detail);
  const title = clip(description.title, ACTIVITY_LIMIT.title), updatedAt = clip(scalar(item.updatedAt), 128).text;
  const requestedRevision = typeof request.revision === 'string' ? request.revision : '';
  const detail = { key: `${request.threadId}/${request.itemId}`, sourceThreadId: request.threadId, itemId: request.itemId,
    kind: item.type, title: title.text, status: clip(scalar(item.status) || 'unreported', 64).text,
    input: clippedInput.text, output: clippedOutput.text, outputPresent,
    outputOmitted: item.outputOmitted === true, updatedAt, requestedRevision,
    changed: Boolean(requestedRevision && updatedAt && requestedRevision !== updatedAt),
    truncated: title.truncated || clippedInput.truncated || clippedOutput.truncated || budget.truncated ||
      (Array.isArray(item.changes) && item.changes.length > 24) ||
      (item.type === 'todo_list' && Array.isArray(item.items ?? item.steps ?? item.todos) &&
        (item.items ?? item.steps ?? item.todos).length > 24) };
  // JSON escaping can cost more bytes than the text. Keep the wire frame
  // finite even for control-heavy output; never clip or rewrite source IDs.
  while (Buffer.byteLength(JSON.stringify(detail)) > ACTIVITY_LIMIT.detailFrame) {
    const field = Buffer.byteLength(JSON.stringify(detail.input)) > Buffer.byteLength(JSON.stringify(detail.output)) ? 'input' : 'output';
    requireThat(detail[field].length > 0, 'Activity identity exceeds the supported detail budget.', 'too_large');
    detail[field] = clip(detail[field], Math.floor(Buffer.byteLength(detail[field]) / 2)).text;
    detail.truncated = true;
  }
  return detail;
}
