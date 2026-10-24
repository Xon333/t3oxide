#!/usr/bin/env node
// Install only t3oxide's files. Never install/update/start T3, Node, SSH, or a VPN.
import { readFile, writeFile, mkdir, mkdtemp, rename, symlink, lstat, rm, open } from 'node:fs/promises';
import { createHash, randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { dirname, resolve, join } from 'node:path';
import { homedir } from 'node:os';
import { readConfig, T3 } from '../adapter/t3.mjs';
import { Fault, requireThat } from '../adapter/core.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const base = join(homedir(), '.local/share/t3oxide');
const configFile = join(homedir(), '.config/t3oxide/host.json');
const privateOwner = stat => (stat.mode & 0o077) === 0 && (!process.getuid || stat.uid === process.getuid());
async function statOrNull(path) {
  try { return await lstat(path); } catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}
function argumentsFor(args) {
  const options = new Map();
  for (let i = 0; i < args.length; i++) {
    const name = args[i];
    requireThat(['--base-url', '--token-file', '--enable-writes', '--read-only'].includes(name) && !options.has(name), 'Unknown or duplicate installer option.');
    if (['--base-url', '--token-file'].includes(name)) {
      const value = args[++i];
      requireThat(value && !value.startsWith('--'), `Missing value for ${name}.`);
      options.set(name, value);
    } else options.set(name, true);
  }
  requireThat(!(options.has('--enable-writes') && options.has('--read-only')), 'Choose either --enable-writes or --read-only.');
  return options;
}
async function main() {
  const options = argumentsFor(process.argv.slice(2));
  const [major, minor] = process.versions.node.split('.').map(Number);
  requireThat(major > 22 || major === 22 && minor >= 4, 'Use an existing Node 22.4+ binary. No runtime was installed.');
  const configDirectory = dirname(configFile);
  await mkdir(configDirectory, { recursive: true, mode: 0o700 });
  requireThat(privateOwner(await lstat(configDirectory)) && (await lstat(configDirectory)).isDirectory(), 'Host configuration directory must be private and not a symlink.');
  // Serialize installers without guessing that an older or paused owner is dead.
  // An interrupted lock is retained for explicit host-local recovery.
  let lock;
  try { lock = await open(join(configDirectory, '.install-lock'), 'wx', 0o600); }
  catch (error) {
    if (error.code === 'EEXIST') throw new Fault('configuration', 'Another installer owns .install-lock; preserve it until that installer has finished or its interruption is resolved.');
    throw error;
  }
  const staged = join(configDirectory, `.host-${randomUUID()}.new`);
  const link = join(base, `.next-${randomUUID()}`);
  let releaseStage, newRelease, stagedOwned = false, linkOwned = false;
  let original, configPublished = false, activated = false;
  try {
    const existing = await statOrNull(configFile);
    requireThat(!existing || existing.isFile() && existing.size <= 65536 && privateOwner(existing), 'Existing host configuration must be a regular private file; preserve it and repair permissions.');
    original = existing ? await readFile(configFile) : null;
    let config = original ? JSON.parse(original.toString('utf8')) : null;
    requireThat(!existing || config && typeof config === 'object' && !Array.isArray(config), 'Existing host configuration is invalid; preserve it and repair it explicitly.');
    if (!config || options.has('--base-url') || options.has('--token-file')) {
      const url = new URL(options.get('--base-url') ?? config?.baseUrl ?? 'http://127.0.0.1:3773');
      requireThat(['http:', 'https:'].includes(url.protocol) && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) && url.pathname === '/' && !url.username && !url.password && !url.search && !url.hash, 'Supply the existing loopback T3 base URL.');
      const tokenFile = options.get('--token-file') ?? config?.tokenFile;
      requireThat(typeof tokenFile === 'string' && tokenFile.startsWith('/'), 'Supply --token-file with a private host-local token path. Token values never go on the command line.');
      const response = await fetch(new URL('/.well-known/t3/environment', url), { signal: AbortSignal.timeout(10000), redirect: 'error' });
      requireThat(response.ok, 'Existing local T3 descriptor was unavailable. No server was started.');
      const d = await response.json();
      requireThat(d.orchestrationProtocolVersion === 2 && typeof d.environmentId === 'string', 'This source adapter requires T3 protocol 2.');
      requireThat(!config || config.environmentId === d.environmentId, 'Existing t3oxide identity differs; do not silently rebind this host configuration.');
      config = { ...config, baseUrl: url.href, environmentId: d.environmentId, tokenFile, allowWrites: config?.allowWrites === true };
    }
    if (options.has('--enable-writes')) config.allowWrites = true;
    if (options.has('--read-only')) config.allowWrites = false;
    await writeFile(staged, JSON.stringify(config, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
    stagedOwned = true;
    const verified = await readConfig(staged), backend = new T3(verified, () => {});
    try { await backend.probe(); await backend.http('/api/orchestration/shell'); }
    finally { backend.close(); }
    const current = join(base, 'current'), currentStat = await statOrNull(current);
    requireThat(!currentStat || currentStat.isSymbolicLink(), 'The install current path is not a symlink; preserve it and resolve manually.');
    const files = ['core.mjs', 'wire.mjs', 'journal.mjs', 'models.mjs', 'navigation.mjs', 'activity.mjs', 't3.mjs', 'main.mjs'];
    const contents = new Map(), hash = createHash('sha256');
    for (const name of [...files, 'NOTICE']) {
      const bytes = await readFile(name === 'NOTICE' ? join(root, name) : join(root, 'adapter', name));
      contents.set(name, bytes); hash.update(name).update('\0').update(bytes).update('\0');
    }
    const revision = hash.digest('hex').slice(0, 16), releases = join(base, 'releases');
    await mkdir(releases, { recursive: true, mode: 0o700 });
    const release = join(releases, revision), releaseStat = await statOrNull(release);
    if (releaseStat) {
      requireThat(releaseStat.isDirectory() && privateOwner(releaseStat), 'Existing release is not a private directory; preserve it and resolve manually.');
      for (const [name, bytes] of contents) {
        const path = join(release, name), stat = await statOrNull(path);
        requireThat(stat?.isFile() && stat.size === bytes.length && (await readFile(path)).equals(bytes), 'Existing release differs from its source revision; preserve it and resolve manually.');
      }
    } else {
      releaseStage = await mkdtemp(join(releases, '.incoming-'));
      for (const [name, bytes] of contents) await writeFile(join(releaseStage, name), bytes, { flag: 'wx', mode: 0o600 });
      await rename(releaseStage, release); releaseStage = null; newRelease = release;
    }
    await symlink(release, link); linkOwned = true;
    // Detect non-installer writers too; a changed config must be reviewed again.
    const latest = await statOrNull(configFile);
    requireThat(original ? latest?.isFile() && (await readFile(configFile)).equals(original) : !latest, 'Host configuration changed during installation; retry after reviewing it.');
    await rename(staged, configFile); stagedOwned = false; configPublished = true;
    await rename(link, current); linkOwned = false; activated = true;
    console.log(JSON.stringify({ adapterRevision: revision, environmentId: verified.environmentId, node: process.execPath,
      adapter: join(current, 'main.mjs'), hostConfig: configFile, writesEnabled: config.allowWrites === true }, null, 2));
  } finally {
    if (configPublished && !activated) {
      if (original) {
        await writeFile(staged, original, { flag: 'wx', mode: 0o600 }); stagedOwned = true;
        await rename(staged, configFile); stagedOwned = false;
      } else await rm(configFile);
    }
    if (linkOwned) await rm(link);
    if (releaseStage) await rm(releaseStage, { recursive: true });
    if (newRelease && !activated) await rm(newRelease, { recursive: true });
    if (stagedOwned) await rm(staged);
    await lock.close(); await rm(join(configDirectory, '.install-lock'));
  }
}
main().catch(error => {
  console.error(`t3oxide install: ${error instanceof Fault ? error.message : 'Installation failed; inspect host-local paths and permissions.'}`);
  process.exitCode = 1;
});
