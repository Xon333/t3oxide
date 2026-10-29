// Independently authored bounded client projection over verified T3 protocol-2
// handlers. Native T3 owns projects, worktrees, runs, queues and visibility.
import { LIMIT, clip, hash, identifier, requireThat, text } from './core.mjs';
import { catalog, selection, sameSelection, validateChoice } from './models.mjs';

const ACTIVE = new Set(['preparing', 'starting', 'running', 'waiting']);
const optionalID = value => value == null ? '' : identifier(value);
const safePath = value => typeof value === 'string' && value.startsWith('/') &&
  Buffer.byteLength(value) <= 4096 && !/[\x00-\x1f\x7f]/u.test(value);

// Independent JSONC reader for the pinned native t3.json schema. It handles
// comments/trailing commas without touching quoted URLs or command strings.
// No script is imported or executed: only a validated workspace default leaves
// this boundary. Invalid files follow native parseT3ProjectFile's null result.
export function projectFileDefaults(contents) {
  if(typeof contents!=='string' || Buffer.byteLength(contents)>LIMIT.upstream)return null;
  let plain='',quoted=false,escaped=false;
  for(let i=0;i<contents.length;i++){
    const c=contents[i];
    if(quoted){plain+=c;if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c==='"')quoted=false;continue;}
    if(c==='"'){quoted=true;plain+=c;continue;}
    if(c==='/' && contents[i+1]==='/'){while(i+1<contents.length && contents[i+1]!=='\n')i++;continue;}
    if(c==='/' && contents[i+1]==='*'){
      const end=contents.indexOf('*/',i+2);if(end<0)return null;i=end+1;continue;
    }
    plain+=c;
  }
  let normalized='';quoted=false;escaped=false;
  for(let i=0;i<plain.length;i++){
    const c=plain[i];
    if(quoted){normalized+=c;if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c==='"')quoted=false;continue;}
    if(c==='"'){quoted=true;normalized+=c;continue;}
    if(c===','){let j=i+1;while(j<plain.length && /\s/u.test(plain[j]))j++;if(plain[j]==='}' || plain[j]===']')continue;}
    normalized+=c;
  }
  const object=v=>v!==null && typeof v==='object' && !Array.isArray(v);
  const nonempty=v=>typeof v==='string' && v.trim().length>0;
  let data;try{data=JSON.parse(normalized);}catch{return null;}
  if(!object(data) || data.$schema!==undefined && typeof data.$schema!=='string' ||
    data.iconPath!==undefined && (!nonempty(data.iconPath) || data.iconPath.length>512) ||
    data.defaultThreadEnvMode!==undefined && !['local','worktree'].includes(data.defaultThreadEnvMode) ||
    data.worktreeSubmodules!==undefined && !['recursive','top-level','none'].includes(data.worktreeSubmodules))return null;
  if(data.scripts!==undefined){
    if(!Array.isArray(data.scripts) || data.scripts.length>50)return null;
    for(const script of data.scripts){
      if(!object(script) || !nonempty(script.name) || !nonempty(script.command) ||
        script.icon!==undefined && !['play','test','lint','configure','build','debug'].includes(script.icon) ||
        script.previewUrl!==undefined && !nonempty(script.previewUrl))return null;
      for(const key of ['runOnWorktreeCreate','runOnSettle','async','autoOpenPreview'])if(script[key]!==undefined && typeof script[key]!=='boolean')return null;
    }
  }
  return data.defaultThreadEnvMode===undefined?{}:{defaultThreadEnvMode:data.defaultThreadEnvMode};
}

export function nativeRevision(t) {
  requireThat(typeof t.updatedAt === 'string' && Number.isFinite(Date.parse(t.updatedAt)),
    'Native thread revision is unavailable.', 'unsupported');
  return hash({id:t.id, projectId:t.projectId, updatedAt:t.updatedAt,
    worktreePath:t.worktreePath ?? null, branch:t.branch ?? null,
    settledOverride:t.settledOverride ?? null, snoozedUntil:t.snoozedUntil ?? null,
    pinnedAt:t.pinnedAt ?? null, pinOrderKey:t.pinOrderKey ?? null,
    activeRunId:t.activeRunId ?? null, latestRunId:t.latestRunId ?? null,
    status:t.status ?? null, activityRunStatus:t.activityRunStatus ?? null,
    pendingRuntimeRequest:t.pendingRuntimeRequest ?? null, queuedRunCount:t.queuedRunCount ?? null});
}
export function pullRequests(t) {
  const links = Array.isArray(t.pullRequests) ? t.pullRequests :
    [t.linkedPullRequest, t.branchPullRequest].filter(Boolean);
  const result = [], seen = new Set();
  for (const p of links.slice(0, 32)) {
    if (p.source === 'stack-dismissed') continue;
    let url;
    try { url = new URL(p.url); } catch { continue; }
    if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password ||
        Buffer.byteLength(p.url) > 2048 || !Number.isSafeInteger(p.number) || p.number <= 0 || seen.has(p.url)) continue;
    seen.add(p.url);
    result.push({url:p.url, number:p.number, repository:clip(p.repository, 512).text,
      title:clip(p.snapshot?.title, 512).text, state:clip(p.snapshot?.state, 64).text,
      syncedAt:clip(p.snapshot?.syncedAt, 64).text});
  }
  return result;
}
export function inboxMetadata(t) {
  // Unknown older shells stay readable but cannot offer a revision-bound action.
  if (typeof t.updatedAt !== 'string' || !Number.isFinite(Date.parse(t.updatedAt))) return undefined;
  const latest = t.latestVisibleMessage;
  const message = latest && ['user','assistant','system'].includes(latest.role)
    ? {role:latest.role, ...clip(latest.text, 1024), updatedAt:clip(latest.updatedAt,64).text} : null;
  return {nativeRevision:nativeRevision(t), updatedAt:t.updatedAt,
    settled:t.settledOverride === 'settled', snoozedUntil:clip(t.snoozedUntil,64).text,
    pinnedAt:clip(t.pinnedAt,64).text, runId:optionalID(t.activeRunId ?? t.latestRunId),
    queuedCount:Number.isSafeInteger(t.queuedRunCount) && t.queuedRunCount >= 0 ? t.queuedRunCount : -1,
    // The shell offers one pending-request summary, not a complete count/form.
    pendingRequestCount:t.pendingRuntimeRequest ? 1 : 0,
    pendingRequestId:optionalID(t.pendingRuntimeRequest?.id),
    pendingRequestType:clip(t.pendingRuntimeRequest?.kind,64).text,
    latestMessage:message, workspace:safePath(t.worktreePath) ? t.worktreePath : '',
    branch:clip(t.branch,512).text, pullRequests:pullRequests(t)};
}
export function projectPage(shell, a = {}) {
  requireThat(Array.isArray(shell?.projects), 'T3 did not report existing projects.', 'incompatible');
  const cursor = a.cursor ?? 0, limit = a.limit ?? 64;
  requireThat(Number.isSafeInteger(cursor) && cursor >= 0 && cursor <= 1000000 &&
    Number.isSafeInteger(limit) && limit > 0 && limit <= 200, 'Invalid project page.');
  const projects = shell.projects.filter(p => !p.deletedAt), seen = new Set();
  for (const p of projects) {
    identifier(p.id, 'project'); requireThat(!seen.has(p.id), 'Duplicate native project.', 'incompatible');
    seen.add(p.id);
  }
  let bytes = 0;
  const rows = [];
  for (const p of projects.slice(cursor, cursor + limit)) {
    requireThat(safePath(p.workspaceRoot), 'Native project root is unavailable.', 'incompatible');
    const row = {id:p.id, title:clip(p.title,512).text, workspaceRoot:p.workspaceRoot};
    const cost = Buffer.byteLength(JSON.stringify(row));
    if (bytes + cost > 192*1024) break;
    rows.push(row); bytes += cost;
  }
  const next = cursor + rows.length;
  return {projects:rows, more:next < projects.length, cursor, nextCursor:next < projects.length ? next : null};
}
export function effectiveProjectSetting(settings, p, key) {
  const overrides = settings.projectSettingsOverrides?.[p.id] ?? {};
  if (Object.hasOwn(overrides,key) && overrides[key] !== undefined) return overrides[key];
  if (!settings.projectSettingsFolded && ['defaultModelSelection','defaultThreadEnvMode'].includes(key) && p[key] != null) return p[key];
  return settings[key];
}
export function resolveDefaults(shell, config, projectId, projectFile = null, isolated = false) {
  const p = shell.projects?.find(p => p.id === projectId && !p.deletedAt);
  requireThat(p && safePath(p.workspaceRoot), 'Selected project is unavailable.', 'stale');
  const settings = config?.settings;
  requireThat(settings && Array.isArray(config.providers), 'Native new-thread settings are unavailable.', 'unsupported');
  let selected = effectiveProjectSetting(settings,p,'defaultModelSelection');
  const projectProviders = settings.projectSettingsOverrides?.[p.id]?.providerInstances ?? {};
  const enabled = provider => (projectProviders[provider.instanceId]?.enabled ?? settings.providerInstances?.[provider.instanceId]?.enabled ?? provider.enabled) !== false && provider.availability !== 'unavailable';
  if (selected == null) {
    // Matches native registry order for an unconfigured new selection. Never
    // fall back from an existing configured account/model on failure or quota.
    const provider = config.providers.find(p => enabled(p) && p.status === 'ready') ??
      config.providers.find(p => enabled(p) && p.status !== 'error');
    requireThat(provider, 'No native provider default is available; choose it in T3.', 'unavailable');
    const model = provider.models?.find(m => m.isDefault && !m.isCustom) ??
      provider.models?.find(m => !m.isCustom) ?? provider.models?.[0];
    requireThat(model, 'Native provider has no reported model default.', 'unavailable');
    selected = {instanceId:provider.instanceId, model:model.slug};
  }
  selected = selection(selected);
  const provider = config.providers.find(p => p.instanceId === selected.instanceId);
  requireThat(provider && enabled(provider), 'Configured native provider is unavailable; no account fallback was made.', 'unavailable');
  const modelCatalog = catalog(config,{thread:{id:projectId,modelSelection:selected}});
  requireThat(modelCatalog.available && modelCatalog.models.some(m => m.slug === selected.model),
    'Configured native model is unavailable; no model fallback was made.', 'unavailable');
  const runtimeMode = effectiveProjectSetting(settings,p,'defaultRuntimeMode');
  requireThat(['approval-required','auto-accept-edits','auto','full-access'].includes(runtimeMode),
    'Native runtime default is unavailable.', 'unsupported');
  let envMode = effectiveProjectSetting(settings,p,'defaultThreadEnvMode');
  const inherited = envMode == null;
  if (envMode == null) envMode = projectFile?.defaultThreadEnvMode ?? 'local';
  requireThat(['local','worktree'].includes(envMode), 'Unsupported native workspace default.', 'unsupported');
  // Native first-thread scaffold exception applies only to the environment
  // tier; an explicit project/file workspace choice still wins.
  const overridden = Object.hasOwn(settings.projectSettingsOverrides?.[p.id] ?? {}, 'defaultThreadEnvMode') ||
    !settings.projectSettingsFolded && p.defaultThreadEnvMode != null || inherited && projectFile?.defaultThreadEnvMode != null;
  const root = config.newProjectsRoot?.replace(/\/$/u,'');
  if (!overridden && envMode === 'worktree' && typeof root === 'string' &&
      p.workspaceRoot.startsWith(root+'/') && !p.workspaceRoot.slice(root.length+1).includes('/') &&
      !shell.threads?.some(t => t.projectId === p.id)) envMode = 'local';
  if (isolated) envMode = 'worktree';
  return {projectId:p.id, projectTitle:clip(p.title,512).text, workspaceRoot:p.workspaceRoot,
    modelSelection:selected, runtimeMode, interactionMode:'default', workspaceMode:envMode,
    catalog:modelCatalog};
}
export function launchProjectCommand(a, defaults) {
  requireThat(a.threadId === a.newThreadId, 'New thread identity mismatch.', 'identity_mismatch');
  requireThat(a.workspaceRoot === defaults.workspaceRoot, 'Native project root changed; review the launch again.', 'stale');
  requireThat(a.projectId === defaults.projectId && a.defaultsRevision === defaults.revision,
    'Native launch defaults changed; reopen the review.', 'stale');
  const title = text(a.title,512), message = text(a.message ?? '');
  requireThat(title.trim().length > 0, 'Title must contain text.');
  const requested = a.modelSelection === undefined ? defaults.modelSelection : selection(a.modelSelection);
  // Untouched native defaults retain provider-specific options not described by
  // the UI. Edited options must be in this exact native provider's catalog.
  const next = sameSelection(requested,defaults.modelSelection) ? requested :
    validateChoice(requested,defaults.modelSelection,{...defaults.catalog,requiresNewThreadForModelChange:false});
  requireThat(a.workspaceStrategy && hash(a.workspaceStrategy) === hash(defaults.workspaceStrategy),
    'Workspace choice changed; review it before launch.', 'stale');
  return {commandId:identifier(a.id), threadId:identifier(a.newThreadId,'new thread'),
    projectId:identifier(a.projectId), title, modelSelection:next,
    runtimeMode:defaults.runtimeMode, interactionMode:defaults.interactionMode,
    workspaceStrategy:defaults.workspaceStrategy,
    ...(message.length ? {initialMessage:{messageId:identifier(a.id)+':message',text:message,attachments:[]}} : {})};
}

export function launchWorkspaceMatches(thread, strategy, root) {
  if (strategy?.type === 'root') return thread.worktreePath == null;
  if (strategy?.type === 'existing_worktree') return thread.worktreePath === strategy.worktreePath &&
    (strategy.branch == null || thread.branch === strategy.branch);
  if (strategy?.type === 'worktree') return safePath(thread.worktreePath) && thread.worktreePath !== root &&
    typeof thread.branch === 'string' && thread.branch.trim().length > 0 &&
    (strategy.branch == null || thread.branch === strategy.branch);
  return false;
}
export function threadActionCommand(a, p, shellThread) {
  requireThat(p.thread.id === a.threadId && shellThread?.id === a.threadId,
    'Native thread identity mismatch.', 'identity_mismatch');
  requireThat(a.expectedRevision === nativeRevision(shellThread), 'Thread changed; review the current item.', 'stale');
  requireThat(!p.thread.deletedAt && !p.thread.archivedAt, 'Thread is unavailable.', 'unavailable');
  const actions = new Set(['settle','unsettle','snooze','unsnooze','pin','unpin']);
  requireThat(actions.has(a.action), 'Unsupported native thread action.');
  if (['settle','snooze'].includes(a.action)) {
    requireThat(!p.runtimeRequests.some(r=>r.status==='pending'), 'Answer the formal T3 request before hiding this thread.', 'blocked');
    requireThat(!p.runs.some(r=>ACTIVE.has(r.status) || r.status==='queued'), 'Active or queued work cannot be marked reviewed or snoozed here.', 'blocked');
  }
  const payload = {type:'thread.'+a.action,commandId:identifier(a.id),threadId:identifier(a.threadId)};
  if (['unsettle','unsnooze'].includes(a.action)) payload.reason='user';
  if (a.action === 'snooze') {
    requireThat(typeof a.until==='string' && Number.isFinite(Date.parse(a.until)) && Date.parse(a.until)>Date.now(), 'Snooze wake time must be in the future.');
    payload.snoozedUntil=a.until;
  }
  return payload;
}

export function reviewResult(t, project, result) {
  const workspace = t.worktreePath ?? project?.workspaceRoot;
  requireThat(safePath(workspace) && result?.cwd === workspace && Array.isArray(result.sources),
    'Native review workspace mismatch.', 'identity_mismatch');
  let budget=128*1024, truncated=result.sources.length>8, fileBytes=0;
  const sections=[], files=[], seen=new Set();
  for (const source of result.sources.slice(0,8)) {
    requireThat(['working-tree','branch-range'].includes(source.kind) && typeof source.diff==='string',
      'Unsupported native review source.', 'incompatible');
    const value=clip(source.diff,Math.max(0,budget)); budget-=Buffer.byteLength(value.text);
    truncated ||= value.truncated || source.truncated === true;
    sections.push({kind:source.kind,title:clip(source.title,256).text,diff:value.text,
      hash:clip(source.diffHash,128).text,truncated:value.truncated || source.truncated===true});
    for (const file of (source.files ?? []).slice(0,400)) {
      if (typeof file.path!=='string' || !file.path || Buffer.byteLength(file.path)>4096 || seen.has(file.path)) continue;
      if (files.length>=200) {truncated=true;break;}
      const row={path:file.path,additions:Number.isSafeInteger(file.additions)&&file.additions>=0?file.additions:0,
        deletions:Number.isSafeInteger(file.deletions)&&file.deletions>=0?file.deletions:0};
      const cost=Buffer.byteLength(JSON.stringify(row));
      if(fileBytes+cost>64*1024){truncated=true;continue;}
      seen.add(file.path);files.push(row);fileBytes+=cost;
    }
  }
  const review={threadId:t.id,projectId:t.projectId,revision:nativeRevision(t),workspace,
    branch:clip(t.branch,512).text,diff:sections.map(s=>s.diff).join('\n'),diffTruncated:truncated,
    sources:sections.map(({diff,...s})=>s),files,pullRequests:pullRequests(t),generatedAt:clip(result.generatedAt,64).text};
  // JSON escaping can expand a diff sixfold. Bound the actual wire frame too,
  // leaving space for the versioned reply envelope and exact identities.
  while(Buffer.byteLength(JSON.stringify(review))>LIMIT.frame-8192 && review.diff.length>0){
    review.diff=clip(review.diff,Math.floor(Buffer.byteLength(review.diff)*0.75)).text;
    review.diffTruncated=true;
  }
  requireThat(Buffer.byteLength(JSON.stringify(review))<=LIMIT.frame-8192,
    'Native review metadata exceeds its wire budget.','unsupported');
  return review;
}
