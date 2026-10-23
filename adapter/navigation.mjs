// Bounded thread navigation, derived only from T3's authoritative lineage.
import { clip, identifier, LIMIT, requireThat } from './core.mjs';
import { lineage } from './models.mjs';

export function threadType(thread) {
  const l = lineage(thread.lineage);
  if (!l) return 'unknown';
  if (l.relationshipToParent === 'subagent') return 'subagent';
  if (l.relationshipToParent === 'fork') return 'fork';
  return l.parentThreadId === null ? 'main' : 'unknown';
}

export function threadList(shell, query = '', options = {}) {
  requireThat(Array.isArray(shell?.threads) && Array.isArray(shell?.projects), 'Unsupported T3 shell.', 'incompatible');
  const filter = options.typeFilter ?? 'all';
  requireThat(['all', 'main', 'subagents', 'forks'].includes(filter), 'Unknown thread type filter.');
  const expanded = options.expanded ?? [];
  requireThat(Array.isArray(expanded) && expanded.length <= 200, 'Too many expanded conversations.');
  expanded.forEach(id => identifier(id));
  const open = new Set(expanded);
  const projects = new Map(shell.projects.map(p => [p.id, p.title]));
  const q = String(query).trim().toLocaleLowerCase().slice(0, 200);
  const activeOnly = options.activeOnly === true;
  const rootLimit = activeOnly && !q ? 24 : LIMIT.threads;
  const rowLimit = activeOnly && !q && expanded.length > 0 ? 48 : rootLimit;
  const working = t => ['preparing', 'queued', 'starting', 'running', 'waiting'].includes(t.activityRunStatus ?? t.status);
  const available = shell.threads.filter(t => !t.deletedAt && !t.archivedAt &&
    (!activeOnly || q || t.settledOverride !== 'settled'));
  // Count working children without loading them into the default navigation
  // tree. Only an explicit expansion/type selection/search admits those rows.
  const childCounts = new Map();
  if (activeOnly) for (const t of available) {
    const parent = lineage(t.lineage)?.parentThreadId;
    if (threadType(t) === 'subagent' && parent && working(t)) childCounts.set(parent, (childCounts.get(parent) ?? 0) + 1);
  }
  const eligible = available.filter(t => !activeOnly || threadType(t) !== 'subagent' ||
    (q || working(t) && (filter === 'subagents' || open.has(lineage(t.lineage)?.parentThreadId))))
    .sort((a, b) => String(b.updatedAt).localeCompare(String(a.updatedAt)) || String(a.id).localeCompare(String(b.id)));
  const all = new Map(), children = new Map();
  const positions = new Map(eligible.map((t,i) => [t.id,i]));
  for (const t of eligible) {
    identifier(t.id);
    requireThat(!all.has(t.id), 'Duplicate thread identity in T3 shell.', 'incompatible');
    all.set(t.id, t);
    const l = lineage(t.lineage);
    if (threadType(t) === 'subagent' && l?.parentThreadId) {
      const list = children.get(l.parentThreadId) ?? []; list.push(t); children.set(l.parentThreadId, list);
    }
  }
  const matchesType = t => filter === 'all' || threadType(t) === ({main:'main',subagents:'subagent',forks:'fork'}[filter]);
  const matches = eligible.filter(t => matchesType(t) && (!q || `${t.title} ${projects.get(t.projectId) ?? ''}`.toLocaleLowerCase().includes(q)));
  const found = new Set(matches.map(t => t.id));
  const grouped = options.grouped === true && filter === 'all';
  const included = new Set(found);
  // Search hidden children server-side, adding their known ancestors as context.
  const searchedAncestors = new Set();
  if (grouped && q) for (const t of matches) {
    let next = t;
    while (threadType(next) === 'subagent' && !searchedAncestors.has(next.id)) {
      searchedAncestors.add(next.id);
      const parent = lineage(next.lineage)?.parentThreadId;
      if (!parent || !all.has(parent)) break;
      included.add(parent); open.add(parent); next = all.get(parent);
    }
  }
  const rank = t => ({main:0, fork:1, unknown:2, subagent:3}[threadType(t)]);
  const roots = [...included].map(id => all.get(id)).filter(t => !grouped || threadType(t) !== 'subagent' || !included.has(lineage(t.lineage)?.parentThreadId))
    .sort((a,b) => (grouped ? rank(a)-rank(b) : 0) || positions.get(a.id)-positions.get(b.id));
  // Reserve room for roots before expanding children, preventing a busy
  // subagent family from crowding primary conversations out of the page.
  const chosenRoots = roots.slice(0, rootLimit), nodes = [], visited = new Set();
  let childBudget = rowLimit - chosenRoots.length;
  let more = roots.length > chosenRoots.length;
  function add(t, depth) {
    if (visited.has(t.id)) return;
    if (nodes.length >= rowLimit) { more = true; return; }
    visited.add(t.id); nodes.push({t, depth});
    if (!grouped || !open.has(t.id)) return;
    for (const c of children.get(t.id) ?? []) {
      if (!included.has(c.id) || visited.has(c.id)) continue;
      if (childBudget <= 0) { more = true; continue; }
      childBudget--; add(c, Math.min(12, depth+1));
    }
  }
  for (const t of chosenRoots) add(t, 0);
  // Classify ancestry once per ID, not once per hidden descendant. This
  // keeps a deep collapsed family linear rather than quadratic in shell size.
  const reachesCycle = new Map();
  if (grouped) for (const id of included) {
    if (visited.has(id)) continue;
    let t = all.get(id), cycle = false; const trail = new Set();
    while (t && threadType(t) === 'subagent') {
      if (reachesCycle.has(t.id)) { cycle = reachesCycle.get(t.id); break; }
      if (trail.has(t.id)) { cycle = true; break; }
      trail.add(t.id); t = all.get(lineage(t.lineage)?.parentThreadId);
    }
    for (const member of trail) reachesCycle.set(member, cycle);
    if (cycle) {
      if (nodes.length < rowLimit) { childBudget--; add(all.get(id), 0); }
      else more = true;
    }
  }
  let bytes = 0;
  const rows = [];
  for (const {t, depth} of nodes) {
    const l = lineage(t.lineage), parent = all.get(l?.parentThreadId);
    const row = { id: t.id, title: clip(t.title,512).text,
      ...(t.projectId == null ? {} : { projectId: identifier(t.projectId, 'project id') }),
      project: clip(projects.get(t.projectId) ?? '',256).text,
      status: clip(t.activityRunStatus ?? t.status ?? 'unknown',64).text,
      model: clip(t.modelSelection?.model ?? '',256).text,
      lineage: l, threadType: threadType(t), parentTitle: clip(parent?.title ?? '',512).text,
      childCount: activeOnly ? childCounts.get(t.id) ?? 0 : (children.get(t.id) ?? []).length, depth,
      expanded: open.has(t.id), contextOnly: q !== '' && !found.has(t.id) };
    const cost = Buffer.byteLength(JSON.stringify(row));
    if (bytes+cost > 288*1024) { more = true; break; }
    rows.push(row); bytes += cost;
  }
  return { observedAt: new Date().toISOString(), more, threads: rows,
    typeFilter: filter, grouped, matched: matches.length };
}
