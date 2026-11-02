import test from 'node:test';
import assert from 'node:assert/strict';
import { threadList } from '../adapter/navigation.mjs';
import { T3 } from '../adapter/t3.mjs';

const stamp = '2026-11-02T12:00:00Z';
const thread = (id, title, relationshipToParent = null, status = 'completed') => ({
  id, title, projectId: 'project', updatedAt: stamp, status, latestRunId: `run-${id}`,
  modelSelection: { instanceId: 'provider', model: 'model' },
  lineage: { relationshipToParent, parentThreadId: relationshipToParent ? 'parent' : null, rootThreadId: 'parent' },
});
const shell = { projects: [{ id: 'project', title: 'Project' }], threads: [
  thread('parent', 'Main task'), thread('hidden', 'Needle child', 'subagent'),
  thread('running', 'Running child', 'subagent', 'running'), thread('fork', 'Separate fork', 'fork'),
] };

test('inclusive summary listing exposes collapsed children without changing legacy expansion', () => {
  const options = { activeOnly: true, grouped: false, lifecycle: 'all' };
  const ordinary = threadList(shell, '', options);
  assert.deepEqual(ordinary.threads.map(t => t.id).sort(), ['fork', 'parent']);
  const inclusive = threadList(shell, '', { ...options, includeChildren: true });
  assert.deepEqual(inclusive.threads.map(t => t.id).sort(), ['fork', 'hidden', 'parent', 'running']);
  assert.equal(inclusive.threads.filter(t => t.title.includes('Needle'))[0].id, 'hidden');
  assert.equal(inclusive.threads.find(t => t.id === 'hidden').lineage.parentThreadId, 'parent');
});

test('inclusive summary listing stays bounded and preserves native type/lifecycle constraints', () => {
  const options = { activeOnly: true, grouped: false, lifecycle: 'all', includeChildren: true, limit: 2 };
  const first = threadList(shell, '', options);
  assert.equal(first.threads.length, 2);
  assert.equal(first.more, true);
  const next = threadList(shell, '', { ...options, cursor: first.nextCursor });
  assert.equal(new Set([...first.threads, ...next.threads].map(t => t.id)).size, 4);
  assert.equal(next.more, false);
  const onlyChildren = threadList(shell, '', { ...options, limit: 200, typeFilter: 'subagents' });
  assert.deepEqual(onlyChildren.threads.map(t => t.id).sort(), ['hidden', 'running']);
  const archived = structuredClone(shell);
  archived.threads[1].archivedAt = stamp;
  assert.equal(threadList(archived, '', { ...options, limit: 200 }).threads.some(t => t.id === 'hidden'), false);
});

test('inclusive list negotiates capability and reads shell only, without a transcript subscription', async () => {
  const paths = [];
  const backend = new T3({ environmentId: 'env', allowWrites: false }, () => {});
  backend.http = async path => {
    paths.push(path);
    return path === '/.well-known/t3/environment'
      ? { environmentId: 'env', serverVersion: 'fixture', orchestrationProtocolVersion: 2, capabilities: {} }
      : structuredClone(shell);
  };
  backend.connect = async () => { throw new Error('Metadata listing must not connect a WebSocket'); };
  try {
    assert.equal((await backend.probe()).features.inclusiveThreadList, true);
    const result = await backend.list('', { includeChildren: true, grouped: false, lifecycle: 'all', limit: 200 });
    assert.equal(result.threads.length, 4);
    assert.deepEqual(paths, ['/.well-known/t3/environment', '/api/orchestration/shell']);
    assert.equal(backend.watchId, null);
  } finally { backend.close(); }
});
