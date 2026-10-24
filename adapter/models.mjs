// Additive companion-v1 model controls. T3 contract provenance: docs/model-navigation.md.
// No provider process, credentials, or model catalog is owned by this client.
import { Fault, activity, clip, command, identifier, launch, requireThat, stable, text, view } from './core.mjs';

const ACTIVE = new Set(['preparing', 'starting', 'running', 'waiting']);
export const FEATURES = Object.freeze({ modelCatalog: true, modelSelection: true,
  messageModelOverride: true, launchModelSelection: true, runtimeDetails: true, threadNavigation: true });

export function selection(value) {
  requireThat(value && typeof value === 'object' && !Array.isArray(value), 'Missing model selection.');
  const instanceId = identifier(value.instanceId, 'provider instance');
  requireThat(instanceId.trim() === instanceId && !/[\x7f]/u.test(instanceId), 'Invalid provider instance.');
  const model = text(value.model, 256);
  requireThat(model.trim() === model && model.length > 0 && !/[\x00-\x1f\x7f]/u.test(model), 'Invalid model.');
  let options = value.options === undefined ? [] : value.options;
  // The pinned T3 schema also accepts this historical on-disk representation.
  if (options && typeof options === 'object' && !Array.isArray(options)) {
    options = Object.entries(options).map(([id, value]) => ({ id, value }));
  }
  requireThat(Array.isArray(options) && options.length <= 64, 'Model options exceed the supported limit.');
  const seen = new Set();
  options = options.map(option => {
    const id = text(option?.id, 128);
    requireThat(id && id.trim() === id && !/[\x00-\x1f\x7f]/u.test(id) && !seen.has(id), 'Invalid or duplicate model option.');
    seen.add(id);
    const value = option.value;
    requireThat(typeof value === 'boolean' || typeof value === 'string' && value.length > 0 && value.trim() === value && Buffer.byteLength(value) <= 1024 && !/[\x00-\x1f\x7f]/u.test(value), 'Unsupported model option value.');
    return { id, value };
  }).sort((a, b) => a.id.localeCompare(b.id, 'en'));
  const result = { instanceId, model, ...(options.length ? { options } : {}) };
  requireThat(Buffer.byteLength(JSON.stringify(result)) <= 16 * 1024, 'Model selection exceeds 16 KiB.');
  return result;
}
export function optionalSelection(value) {
  try { return selection(value); } catch { return null; }
}
function optionalIdentifier(value) {
  try { return identifier(value); } catch { return null; }
}
export function sameSelection(a, b) {
  return JSON.stringify(stable(selection(a))) === JSON.stringify(stable(selection(b)));
}
export function lineage(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value) || !Object.hasOwn(value, 'parentThreadId') || !Object.hasOwn(value, 'relationshipToParent')) return null;
  const relationshipToParent = value.relationshipToParent;
  if (![null, 'subagent', 'fork'].includes(relationshipToParent)) return null;
  try {
    const parentThreadId = value.parentThreadId == null ? null : identifier(value.parentThreadId);
    const rootThreadId = value.rootThreadId == null ? null : identifier(value.rootThreadId);
    // A missing parent must not turn a known subagent into a main conversation.
    return { relationshipToParent, parentThreadId, rootThreadId };
  } catch { return null; }
}

export function runtimeDetails(p) {
  const observed = activity(p);
  const active = [...p.runs].reverse().find(r => ACTIVE.has(r.status));
  const run = observed.source === 'provider-turn' ? null : active ?? p.runs.at(-1);
  // Never relabel an old provider thread as the runtime of a newly starting run.
  const providerThreadId = run ? run.providerThreadId : p.thread.activeProviderThreadId;
  const providerThread = (p.providerThreads ?? []).find(t => t.id === providerThreadId);
  const session = providerThread && (p.providerSessions ?? []).find(s => s.id === providerThread.providerSessionId);
  return {
    scope: active ? 'active-run' : run ? 'last-run' : providerThread ? 'provider-thread' : 'unreported',
    runId: run?.id ?? null,
    // Native OrchestrationV2Run.userMessageId is the initiating message, not
    // the most recent user row (which can belong to a queued later send).
    runMessageId: optionalIdentifier(run?.userMessageId),
    providerThreadId: providerThread?.id ?? null,
    providerSessionId: session?.id ?? null,
    status: clip(observed.source === 'provider-turn' ? observed.status : run?.status ?? providerThread?.status ?? 'unreported', 64).text,
    activitySource: observed.source,
    runModelSelection: optionalSelection(run?.modelSelection),
    providerReportedSelection: optionalSelection(providerThread?.nativeMetadata?.modelSelection),
    providerReportedAt: clip(providerThread?.nativeMetadata?.updatedAt ?? '', 128).text || null,
    sessionReportedModel: clip(session?.model ?? '', 256).text || null,
  };
}
export function modelView(snapshot, threadId) {
  const result = view(snapshot, threadId), p = snapshot.projection;
  return { ...result, modelSelection: optionalSelection(p.thread.modelSelection),
    lineage: lineage(p.thread.lineage), runtimeDetails: runtimeDetails(p) };
}

function descriptor(raw) {
  requireThat(raw && ['select', 'boolean'].includes(raw.type), 'Unsupported option descriptor.');
  const id = text(raw.id, 128);
  requireThat(id && id.trim() === id && !/[\x00-\x1f\x7f]/u.test(id), 'Invalid option descriptor id.');
  const result = { id, type: raw.type, label: clip(raw.label || id, 128).text,
    description: clip(raw.description ?? '', 512).text };
  if (raw.type === 'boolean') {
    if (typeof raw.currentValue === 'boolean') result.currentValue = raw.currentValue;
  } else {
    requireThat(Array.isArray(raw.options) && raw.options.length > 0 && raw.options.length <= 32, 'Unsupported option choices.');
    const seen = new Set();
    result.options = raw.options.map(o => {
      const value = text(o?.id, 1024);
      requireThat(value && value.trim() === value && !/[\x00-\x1f\x7f]/u.test(value) && !seen.has(value), 'Invalid or duplicate option choice.');
      seen.add(value);
      return { id: value, label: clip(o.label || value, 128).text,
        description: clip(o.description ?? '', 512).text, isDefault: o.isDefault === true };
    });
    if (typeof raw.currentValue === 'string' && seen.has(raw.currentValue)) result.currentValue = raw.currentValue;
  }
  return result;
}

export function catalog(config, p) {
  const current = selection(p.thread.modelSelection);
  requireThat(Array.isArray(config?.providers), 'T3 did not provide its model catalog.', 'incompatible');
  const provider = config.providers.find(provider => provider.instanceId === current.instanceId);
  requireThat(provider && Array.isArray(provider.models), 'The selected provider has no model catalog.', 'unavailable');
  const available = ['ready', 'warning'].includes(provider.status) && provider.availability !== 'unavailable' &&
    provider.enabled !== false && provider.installed !== false && provider.auth?.status !== 'unauthenticated';
  const models = [], seen = new Set(); let bytes = 0, more = false;
  for (const raw of provider.models) {
    try {
      const slug = selection({ instanceId: current.instanceId, model: raw.slug }).model;
      requireThat(!seen.has(slug), 'Duplicate catalog model.');
      seen.add(slug);
      const options = [], optionIDs = new Set();
      const rawOptions = raw.capabilities?.optionDescriptors ?? [];
      requireThat(Array.isArray(rawOptions) && rawOptions.length <= 64, 'Too many option descriptors.');
      for (const source of rawOptions) {
        // Unknown future controls do not prevent selecting a known model.
        if (!['boolean', 'select'].includes(source?.type)) continue;
        const option = descriptor(source);
        requireThat(!optionIDs.has(option.id), 'Duplicate option descriptor.');
        optionIDs.add(option.id); options.push(option);
      }
      const item = { slug, name: clip(raw.name || slug, 256).text, isDefault: raw.isDefault === true,
        isCustom: raw.isCustom === true, options };
      const cost = Buffer.byteLength(JSON.stringify(item));
      if (models.length >= 200 || bytes + cost > 192 * 1024) { more = true; continue; }
      models.push(item); bytes += cost;
    } catch { more = true; } // Never expose a truncated identity as a selectable model.
  }
  return { threadId: p.thread.id, current, instanceId: current.instanceId,
    providerLabel: clip(provider.displayName ?? provider.name ?? provider.title ?? current.instanceId, 256).text,
    requiresNewThreadForModelChange: provider.requiresNewThreadForModelChange === true,
    available, models, more };
}

export function validateChoice(requested, current, modelCatalog, scope = 'current') {
  requireThat(['current', 'launch'].includes(scope), 'Unknown model selection scope.');
  const next = selection(requested), before = selection(current);
  requireThat(next.instanceId === before.instanceId, 'Provider switching is not enabled here; use full T3.', 'unsupported');
  requireThat(modelCatalog.available, 'This provider is unavailable for model selection.', 'unavailable');
  requireThat(scope === 'launch' || !modelCatalog.requiresNewThreadForModelChange || next.model === before.model,
    'This provider requires a new conversation to change models; use full T3.', 'unsupported');
  const model = modelCatalog.models.find(m => m.slug === next.model);
  requireThat(model, 'Model is not in the current T3 catalog; reopen the picker.', 'stale');
  const old = new Map(before.options?.map(o => [o.id, o.value]) ?? []);
  for (const option of next.options ?? []) {
    // Preserve untouched provider-specific options on the same model. Newly
    // introduced/changed options must be explicitly offered by T3.
    if (scope === 'current' && next.model === before.model && old.has(option.id) && old.get(option.id) === option.value) continue;
    const d = model.options.find(d => d.id === option.id);
    requireThat(d && (d.type === 'boolean' ? typeof option.value === 'boolean' : d.options.some(o => o.id === option.value)),
      'Model option is no longer offered by T3; reopen the picker.', 'stale');
  }
  return next;
}

// The native V2 launch RPC accepts modelSelection in the original create
// operation. Launch choices never configure the source thread or follow up
// with another mutation. The caller journals this complete payload unchanged.
export function launchCommand(a, p, d, modelCatalog) {
  if (a.modelSelection === undefined) return launch(a, p);
  requireThat(d.capabilities?.serverResolvedCommandContext === true,
    'T3 launch model selection is unavailable.', 'incompatible');
  return launch(a, p, validateChoice(a.modelSelection, p.thread.modelSelection, modelCatalog, 'launch'));
}

// Called only for a NEW write, before the existing durable host journal is
// prepared. Explicit retry uses the recorded command and skips all new reads.
export function modelCommand(op, a, p, d, modelCatalog) {
  if (op === 'select-model') {
    requireThat(p.thread.id === a.threadId, 'Thread mismatch.', 'identity_mismatch');
    requireThat(!p.thread.deletedAt && !p.thread.archivedAt, 'This thread is deleted or archived.', 'unavailable');
    requireThat(d.capabilities?.serverResolvedCommandContext === true, 'T3 model selection is unavailable.', 'incompatible');
    requireThat(a.expectedModelSelection && sameSelection(a.expectedModelSelection, p.thread.modelSelection),
      'Thread selection changed while the picker was open. Reopen it before applying.', 'stale');
    return { type: 'thread.model-selection.set', commandId: identifier(a.id), threadId: identifier(a.threadId),
      modelSelection: validateChoice(a.modelSelection, p.thread.modelSelection, modelCatalog) };
  }
  const payload = command(op, a, p, d);
  if (op === 'send' && a.modelSelection !== undefined) {
    payload.modelSelection = validateChoice(a.modelSelection, p.thread.modelSelection, modelCatalog);
  }
  return payload;
}
