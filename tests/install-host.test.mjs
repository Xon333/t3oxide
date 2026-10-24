import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, mkdir, writeFile, readFile, readdir, realpath, rm, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
const exec = promisify(execFile);
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

async function fixture(t) {
  const home = await mkdtemp(join(tmpdir(), 't3oxide-install-test-'));
  t.after(() => rm(home, { recursive: true, force: true }));
  const reads = [], descriptor = { environmentId: 'installer-fixture', serverVersion: 'test', orchestrationProtocolVersion: 2,
    capabilities: { serverResolvedCommandContext: true } };
  const server = createServer((req, res) => {
    reads.push([req.method, req.url]);
    res.setHeader('content-type', 'application/json');
    if (req.url === '/.well-known/t3/environment') return res.end(JSON.stringify(descriptor));
    if (req.url === '/api/orchestration/shell' && req.headers.authorization === 'Bearer fixture-token') {
      return res.end(JSON.stringify({ threads: [], projects: [] }));
    }
    res.statusCode = 404; res.end('{}');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { server.closeAllConnections(); server.close(); });
  const dir = join(home, '.config/t3oxide'), journal = join(dir, 'operations-v1');
  await mkdir(journal, { recursive: true, mode: 0o700 });
  const tokenFile = join(home, 'fixture-token');
  await writeFile(tokenFile, 'fixture-token', { mode: 0o600 });
  const config = { baseUrl: `http://127.0.0.1:${server.address().port}`, environmentId: descriptor.environmentId,
    tokenFile, allowWrites: false };
  const configFile = join(dir, 'host.json');
  await writeFile(configFile, JSON.stringify(config), { mode: 0o600 });
  await writeFile(join(journal, 'existing-receipt.json'), 'PRESERVE', { mode: 0o600 });
  const env = { ...process.env, HOME: home, USERPROFILE: home };
  const install = (...args) => exec(process.execPath, [join(root, 'scripts/install-host.mjs'), ...args], { env, timeout: 8000 });
  const base = join(home, '.local/share/t3oxide');
  return { home, dir, journal, config, configFile, base, env, install, reads, descriptor };
}

test('host installer includes additive modules, preserves journals/config, and installed probe runs', { timeout: 15000 }, async t => {
  const f = await fixture(t), { home, dir, journal, config, env, install, reads, descriptor } = f;
  const first = JSON.parse((await install()).stdout);
  assert.equal(first.writesEnabled, false);
  const release = await realpath(join(f.base, 'current'));
  // macOS canonicalizes /tmp to /private/tmp.
  assert.ok(release.startsWith(join(await realpath(home), '.local/share/t3oxide/releases/')));
  assert.deepEqual((await readdir(release)).sort(), ['NOTICE', 'activity.mjs', 'core.mjs', 'journal.mjs', 'main.mjs', 'models.mjs', 'navigation.mjs', 't3.mjs', 'wire.mjs']);
  for (const file of ['core.mjs', 'wire.mjs', 'journal.mjs', 'models.mjs', 'navigation.mjs', 'activity.mjs', 't3.mjs', 'main.mjs']) {
    assert.deepEqual(await readFile(join(release, file)), await readFile(join(root, 'adapter', file)));
  }
  const probe = JSON.parse((await exec(process.execPath, [first.adapter, '--probe'], { env, timeout: 5000 })).stdout);
  assert.equal(probe.result.environmentId, descriptor.environmentId);
  assert.equal(probe.result.features.threadNavigation, true);
  assert.equal(probe.result.features.modelCatalog, true);
  assert.equal(probe.result.features.activityInspection, true);
  assert.deepEqual(JSON.parse(await readFile(f.configFile, 'utf8')), config);
  assert.equal(await readFile(join(journal, 'existing-receipt.json'), 'utf8'), 'PRESERVE');
  // A paused older installer may own this historical staging path.
  await writeFile(join(dir, 'host.json.new'), 'PRESERVE-STAGING', { mode: 0o600 });
  const second = JSON.parse((await install()).stdout);
  assert.equal(second.adapterRevision, first.adapterRevision);
  assert.equal(await realpath(join(f.base, 'current')), release);
  assert.equal(await readFile(join(dir, 'host.json.new'), 'utf8'), 'PRESERVE-STAGING');
  assert.deepEqual((await readdir(dir)).sort(), ['host.json', 'host.json.new', 'operations-v1']);
  assert.deepEqual(await readdir(join(f.base, 'releases')), [first.adapterRevision]);
  assert.ok(reads.every(([method, url]) => method === 'GET' && ['/.well-known/t3/environment', '/api/orchestration/shell'].includes(url)));
});

test('installer rejects changed release content without overwriting rollback or config', async t => {
  const f = await fixture(t);
  await f.install();
  const release = await realpath(join(f.base, 'current')), original = await readFile(f.configFile);
  await writeFile(join(release, 'models.mjs'), 'UNIQUE LOCAL CHANGE');
  await assert.rejects(f.install('--enable-writes'), error => /Existing release differs/.test(error.stderr));
  assert.equal(await readFile(join(release, 'models.mjs'), 'utf8'), 'UNIQUE LOCAL CHANGE');
  assert.equal(await realpath(join(f.base, 'current')), release);
  assert.deepEqual(await readFile(f.configFile), original);
  assert.deepEqual((await readdir(f.dir)).sort(), ['host.json', 'operations-v1']);
});

test('installer refuses an existing install lock without touching its owner or configuration', async t => {
  const f = await fixture(t), original = await readFile(f.configFile);
  await writeFile(join(f.dir, '.install-lock'), 'PAUSED OWNER', { mode: 0o600 });
  await assert.rejects(f.install('--enable-writes'), error => /Another installer owns/.test(error.stderr));
  assert.equal(await readFile(join(f.dir, '.install-lock'), 'utf8'), 'PAUSED OWNER');
  assert.deepEqual(await readFile(f.configFile), original);
  assert.deepEqual(f.reads, []);
});

test('failed descriptor verification removes owned staging and preserves config and journals', async t => {
  const f = await fixture(t), original = await readFile(f.configFile);
  f.descriptor.environmentId = 'different-environment';
  await assert.rejects(f.install('--enable-writes'), error => /not the configured T3 environment/.test(error.stderr));
  assert.deepEqual(await readFile(f.configFile), original);
  assert.equal(await readFile(join(f.journal, 'existing-receipt.json'), 'utf8'), 'PRESERVE');
  assert.deepEqual((await readdir(f.dir)).sort(), ['host.json', 'operations-v1']);
});

test('installer rejects invalid arguments before reading host configuration or contacting T3', async t => {
  const f = await fixture(t);
  for (const args of [['--base-url'], ['--token-file', '--enable-writes'], ['--enable-writes', '--read-only'], ['--unknown'], ['--read-only', '--read-only']]) {
    await assert.rejects(f.install(...args));
  }
  assert.deepEqual(f.reads, []);
  assert.deepEqual((await readdir(f.dir)).sort(), ['host.json', 'operations-v1']);
});

test('installer preserves a symlink configuration and its original target', async t => {
  const f = await fixture(t), target = join(f.home, 'original-config');
  const original = await readFile(f.configFile);
  await writeFile(target, original, { mode: 0o600 });
  await rm(f.configFile); await symlink(target, f.configFile);
  await assert.rejects(f.install('--enable-writes'), error => /regular private file/.test(error.stderr));
  assert.deepEqual(await readFile(target), original);
  assert.equal(await realpath(f.configFile), await realpath(target));
  assert.deepEqual(f.reads, []);
  assert.deepEqual((await readdir(f.dir)).sort(), ['host.json', 'operations-v1']);
});
