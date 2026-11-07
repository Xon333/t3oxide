#!/usr/bin/env python3
"""Finite check evidence v1. No discovery, retries, shell, service, or cleanup.

prepare --state-root ROOT reads <=16 KiB request JSON from stdin. ROOT must
already be private, canonical and outside temporary/source roots. run/child/read
take --attempt-dir. Only run launches the pinned wrapper; only child launches
the requested argv. read emits one JSON value, <=48 KiB including its newline.
See the implementation handoff for the caller and evidence coverage contract.
"""
import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import queue
import re
import selectors
import signal
import socket
import stat
import subprocess
import sys
import threading
import time
import uuid

SCHEMA = 't3oxide-check-evidence-v1'
REQUEST_CAP = 16384
RETURN_CAP = 49152
STREAM_CAP = 262144
SOURCE_CAP = 65536
RECORD_CAP = 65536


def encoded(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(',', ':'),
                      allow_nan=False).encode('ascii')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def canonical(path):
    require(isinstance(path, str) and os.path.isabs(path) and '\x00' not in path,
            'absolute path required')
    require(os.path.realpath(path) == path, 'noncanonical or symlink path refused')
    return Path(path)


def private_dir(path):
    path = canonical(str(path))
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid()
            and stat.S_IMODE(info.st_mode) == 0o700, 'directory must be owned mode 0700')
    return path


def sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def open_read(path, *, allow_asset_links=False):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and (allow_asset_links or info.st_nlink == 1),
                'nonregular or hard-linked evidence refused')
        return os.fdopen(fd, 'rb')
    except BaseException:
        os.close(fd)
        raise


def read_bytes(path, cap):
    with open_read(path) as handle:
        info = os.fstat(handle.fileno())
        require(info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o600,
                'evidence file must be owned mode 0600')
        data = handle.read(cap + 1)
    require(len(data) <= cap, 'artifact exceeds bound')
    return data


def file_identity(path, *, allow_asset_links=False):
    path = canonical(str(path))
    digest = hashlib.sha256()
    with open_read(path, allow_asset_links=allow_asset_links) as handle:
        before = os.fstat(handle.fileno())
        while True:
            block = handle.read(65536)
            if not block:
                break
            digest.update(block)
        after = os.fstat(handle.fileno())
    require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
            == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns),
            'asset changed during identity read')
    return {'path': str(path), 'sha256': digest.hexdigest(),
            'device': before.st_dev, 'inode': before.st_ino}


def publish(directory, name, value):
    # Link is atomic and cannot replace an existing final artifact. Keep a
    # unique staging file if publication fails; it may be a recovery input.
    temp = directory / ('.' + name + '.' + uuid.uuid4().hex)
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as handle:
        handle.write(encoded(value))
        handle.flush()
        os.fsync(handle.fileno())
    os.link(temp, directory / name, follow_symlinks=False)
    temp.unlink()
    sync_dir(directory)


def load(directory, name):
    return json.loads(read_bytes(directory / name, RECORD_CAP))


def host():
    return {'hostname': socket.gethostname(), 'uid': os.getuid()}


def validate_request(req, fresh=True):
    require(isinstance(req, dict) and set(req) == {
        'schema', 'attemptId', 'target', 'host', 'worktree', 'argv',
        'timeoutSeconds', 'sourcePaths', 'wrapper', 'assetSha256'}, 'request fields mismatch')
    require(req['schema'] == SCHEMA, 'unsupported schema')
    require(isinstance(req['attemptId'], str) and re.fullmatch('[0-9a-f]{32}', req['attemptId']),
            'attemptId must be 32 lowercase hex characters')
    target = req['target']
    require(isinstance(target, dict) and set(target) == {
        'environmentId', 'projectId', 'threadId', 'sourceRevision'}, 'target fields mismatch')
    require(all(isinstance(v, str) and 0 < len(v.encode()) <= 256 for v in target.values()),
            'target values must be bounded nonempty strings')
    require(isinstance(req['host'], str) and 0 < len(req['host'].encode()) <= 256,
            'invalid executing hostname')
    for key in ('worktree', 'wrapper'):
        path = req[key]
        require(isinstance(path, str) and os.path.isabs(path) and '\x00' not in path,
                'absolute path required')
    if fresh:
        require(req['host'] == socket.gethostname(), 'executing hostname mismatch')
        cwd = canonical(req['worktree'])
        require(cwd.is_dir(), 'worktree must exist')
    require(type(req['timeoutSeconds']) is int and 1 <= req['timeoutSeconds'] <= 900,
            'timeoutSeconds must be integer 1..900')
    argv = req['argv']
    require(isinstance(argv, list) and 1 <= len(argv) <= 64
            and all(isinstance(v, str) and '\x00' not in v for v in argv)
            and argv[0] and sum(len(v.encode()) for v in argv) <= 4096,
            'argv must be a bounded direct argument vector')
    paths = req['sourcePaths']
    require(isinstance(paths, list) and len(paths) <= 8, 'at most eight selected source paths')
    require(all(isinstance(p, str) and 0 < len(p.encode()) <= 256 and '\x00' not in p
                and not p.startswith('/') and all(c not in ('', '.', '..') for c in p.split('/'))
                for p in paths), 'sourcePaths must be relative regular-file paths without traversal')
    require(len(set(paths)) == len(paths), 'duplicate source path')
    require(isinstance(req['assetSha256'], str) and re.fullmatch('[0-9a-f]{64}', req['assetSha256']),
            'invalid assetSha256')
    # Recovery must execute the original hash-pinned asset. It does not need
    # the original interpreter or wrapper bytes to inspect retained evidence.
    require(file_identity(Path(__file__).resolve(), allow_asset_links=True)['sha256'] == req['assetSha256'],
            'adapter asset hash mismatch')
    if fresh:
        canonical(req['wrapper'])


def prepare(root, req):
    validate_request(req)
    root = private_dir(root)
    cwd = Path(req['worktree'])
    require(not root.is_relative_to(cwd), 'state root inside worktree refused')
    temp_roots = ('/tmp', '/var/tmp', '/private/tmp', '/private/var/tmp',
                  '/var/folders', '/private/var/folders', '/dev/shm')
    temp_roots += tuple(value for value in (os.environ.get('TMPDIR'), os.environ.get('TEMP'),
                                           os.environ.get('TMP')) if value and os.path.isabs(value))
    for temp in temp_roots:
        require(not root.is_relative_to(Path(temp).resolve()), 'temporary state root refused')
    # Local/SSD filesystem qualification belongs to the explicit host bootstrap.
    info = cwd.stat()
    binding = {'host': host(), 'worktree': str(cwd),
               'worktreeDevice': info.st_dev, 'worktreeInode': info.st_ino,
               'asset': file_identity(Path(__file__).resolve(), allow_asset_links=True),
               'python': file_identity(Path(sys.executable).resolve()),
               'wrapper': file_identity(req['wrapper']), 'stateRoot': str(root),
               'filesystemLocality': 'unknown', 'preparedAt': now(),
               'requestSha256': sha(encoded(req))}
    require(os.access(req['wrapper'], os.X_OK), 'wrapper not executable')
    for path in req['sourcePaths']:
        try:
            with selected_open(req['worktree'], path):
                pass
        except FileNotFoundError:
            pass  # Explicitly selected absence/deletion is evidence too.
    directory = root / req['attemptId']
    directory.mkdir(mode=0o700)  # Exclusive even when an earlier prepare crashed.
    sync_dir(root)
    publish(directory, 'request.json', req)
    publish(directory, 'binding.json', binding)
    return {'attemptDir': str(directory), 'requestSha256': binding['requestSha256'],
            'attemptId': req['attemptId'], 'host': binding['host']['hostname'],
            'worktree': str(cwd), 'python': binding['python']['path'],
            'assetPath': binding['asset']['path']}


def context(attempt, fresh=True):
    directory = private_dir(attempt)
    req = load(directory, 'request.json')
    validate_request(req, fresh=fresh)
    binding = load(directory, 'binding.json')
    require(directory.name == req['attemptId'] and str(directory.parent) == binding['stateRoot'],
            'attempt path binding mismatch')
    private_dir(directory.parent)
    require(binding['requestSha256'] == sha(encoded(req)), 'immutable request digest mismatch')
    require(binding['worktree'] == req['worktree']
            and binding['host']['hostname'] == req['host']
            and binding['asset']['sha256'] == req['assetSha256']
            and binding['wrapper']['path'] == req['wrapper'], 'immutable binding mismatch')
    if fresh:
        info = Path(req['worktree']).stat()
        require(binding['host'] == host()
                and binding['worktreeDevice'] == info.st_dev and binding['worktreeInode'] == info.st_ino,
                'host/cwd binding mismatch')
        for key, path in (('asset', Path(__file__).resolve()),
                          ('python', Path(sys.executable).resolve()), ('wrapper', req['wrapper'])):
            require(binding[key] == file_identity(path, allow_asset_links=(key == 'asset')), key + ' identity mismatch')
    return directory, req, binding


def current_identity(req, binding):
    """Best-effort current observations; never prerequisites for historical read."""
    result = {'observedAt': now(), 'host': {'state': 'matching' if host() == binding['host']
                                         else 'changed', 'observed': host()}}
    try:
        cwd = canonical(req['worktree'])
        info = cwd.stat()
        observed = {'device': info.st_dev, 'inode': info.st_ino}
        matching = (stat.S_ISDIR(info.st_mode) and info.st_dev == binding['worktreeDevice']
                    and info.st_ino == binding['worktreeInode'])
        result['worktree'] = {'state': 'matching' if matching else 'changed', 'observed': observed}
    except (OSError, ValueError) as error:
        result['worktree'] = {'state': 'unknown', 'error': str(error)[:256]}
    # Apart from authenticating this executing asset, do not open/hash live
    # executables during recovery: arbitrary
    # replacement bytes can be huge or inaccessible. A path alone proves no
    # current byte identity. The original retained binding stays authoritative.
    for key in ('asset', 'python', 'wrapper'):
        result[key] = {'state': 'unknown', 'path': binding[key]['path'],
                       'reason': 'current executable bytes not inspected during historical read'}
    return result


def claim(directory, role, binding):
    publish(directory, role + '-started.json', {'startedAt': now(),
            'requestSha256': binding['requestSha256'], 'host': host(), 'pid': os.getpid()})


def descriptor(role, path, name, data, original_length, original_sha, eof, errors=None):
    return {'role': role, 'path': path, 'retainedRef': name,
            'retainedLength': len(data), 'retainedSha256': sha(data),
            'originalLength': original_length, 'originalSha256': original_sha,
            'eof': eof, 'errors': errors or []}


def selected_open(cwd, path):
    # Resolve every component using directory descriptors; never follow a
    # selected symlink or fall back to a path outside the pinned worktree.
    fd = os.open(cwd, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        parts = path.split('/')
        for part in parts[:-1]:
            next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd)
            fd = next_fd
        leaf = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
        try:
            require(stat.S_ISREG(os.fstat(leaf).st_mode), 'selected source is not a regular file')
            return os.fdopen(leaf, 'rb')
        except BaseException:
            os.close(leaf)
            raise
    finally:
        os.close(fd)


def source_snapshot(directory, req, phase):
    result = {'scope': 'selected-paths-only', 'repositoryIdentity': 'unknown',
              'head': None, 'indexIdentity': 'unknown', 'dirtyIdentity': 'unknown',
              'snapshotIsolation': False, 'observedAt': now(), 'entries': []}
    for index, path in enumerate(req['sourcePaths']):
        entry = {'path': path, 'pathBase64': base64.b64encode(os.fsencode(path)).decode(),
                 'sourceRevision': req['target']['sourceRevision'], 'kind': 'unknown',
                 'mode': None, 'blob': None, 'error': None}
        try:
            with selected_open(req['worktree'], path) as handle:
                before = os.fstat(handle.fileno())
                data = handle.read(SOURCE_CAP)
                eof = handle.read(1) == b''
                after = os.fstat(handle.fileno())
            stable = (before.st_size, before.st_mtime_ns, before.st_ctime_ns) == (
                after.st_size, after.st_mtime_ns, after.st_ctime_ns)
            name = phase + '-source-' + str(index) + '.bin'
            write_blob(directory, name, data)
            entry.update(kind='regular', mode=stat.S_IMODE(before.st_mode),
                         blob=descriptor(phase, path, name, data, before.st_size,
                                         sha(data) if eof and stable else None, eof and stable,
                                         [] if stable else ['source changed during read']))
            if not stable:
                entry['blob']['originalLength'] = None
        except FileNotFoundError:
            entry['kind'] = 'absent'
        except (OSError, ValueError) as error:
            entry['error'] = str(error)
        result['entries'].append(entry)
    publish(directory, phase + '.json', result)
    return result


def write_blob(directory, name, data):
    fd = os.open(directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as handle:
        handle.write(data)
        handle.flush()
        os.fsync(handle.fileno())
    sync_dir(directory)


def unknown():
    return {'state': 'unknown', 'exitCode': None, 'signal': None,
            'spawnError': None, 'timedOut': False}


class Forwarder:
    """Bounded best-effort display. A blocked terminal cannot hold evidence open."""
    def __init__(self, fd):
        self.fd = fd
        self.pending = queue.Queue(maxsize=4)
        self.errors = []
        self.dropped = False
        self.worker = threading.Thread(target=self.work, daemon=True)
        self.worker.start()

    def work(self):
        while True:
            data = self.pending.get()
            if data is None:
                return
            try:
                while data:
                    count = os.write(self.fd, data)
                    if count <= 0:
                        raise OSError('zero display write')
                    data = data[count:]
            except OSError as error:
                self.errors.append('forward: ' + str(error))
                return

    def offer(self, data):
        try:
            self.pending.put_nowait(data)
        except queue.Full:
            self.dropped = True

    def finish(self):
        try:
            self.pending.put_nowait(None)
        except queue.Full:
            self.dropped = True
        self.worker.join(timeout=0.1)
        errors = list(self.errors)
        if self.dropped or self.worker.is_alive():
            errors.append('forward: incomplete display delivery')
        return errors


def capture(directory, role, argv, cwd, timeout):
    """Bound storage; only a workload deadline may terminate the workload group.

    The Rex-owned run process keeps draining Fleet's cleanup owner until exit.
    The originating Rust caller has a separate bounded observation deadline; its
    expiry does not close these pipes or terminate this helper/pane.
    """
    result = unknown()
    result.update(streams=[], errors=[], startedAt=now(), finishedAt=None)
    streams = {}
    selector = selectors.DefaultSelector()
    process = None
    try:
        for channel in ('stdout', 'stderr'):
            name = role + '-' + channel + '.bin'
            fd = os.open(directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            streams[channel] = {'fd': fd, 'name': name, 'retained': 0, 'observed': 0,
                                'digest': hashlib.sha256(), 'retainedDigest': hashlib.sha256(),
                                'eof': False, 'errors': [],
                                'forwarder': Forwarder(1 if channel == 'stdout' else 2)}
        sync_dir(directory)
        try:
            process = subprocess.Popen(argv, cwd=cwd, stdin=subprocess.DEVNULL,
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       start_new_session=True)
        except OSError as error:
            result['spawnError'] = str(error)
            return result
        for channel, pipe in (('stdout', process.stdout), ('stderr', process.stderr)):
            os.set_blocking(pipe.fileno(), False)
            selector.register(pipe, selectors.EVENT_READ, channel)
        deadline = time.monotonic() + timeout if timeout is not None else None
        while selector.get_map() or process.poll() is None:
            if deadline is not None and time.monotonic() >= deadline:
                result['timedOut'] = True
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                break
            wait = min(0.1, max(0, deadline - time.monotonic())) if deadline is not None else 0.1
            for key, _ in selector.select(wait):
                stream = streams[key.data]
                try:
                    block = os.read(key.fd, 65536)
                except BlockingIOError:
                    continue
                except OSError as error:
                    stream['errors'].append('read: ' + str(error))
                    selector.unregister(key.fileobj)
                    continue
                if not block:
                    stream['eof'] = True
                    selector.unregister(key.fileobj)
                    continue
                stream['observed'] += len(block)
                stream['digest'].update(block)
                retained = block[:max(0, STREAM_CAP - stream['retained'])]
                if retained and stream['fd'] is not None:
                    try:
                        # os.write may be short; account only for committed bytes.
                        while retained:
                            count = os.write(stream['fd'], retained)
                            require(count > 0, 'zero spool write')
                            stream['retainedDigest'].update(retained[:count])
                            stream['retained'] += count
                            retained = retained[count:]
                    except (OSError, ValueError) as error:
                        stream['errors'].append('spool: ' + str(error))
                        os.close(stream['fd'])
                        stream['fd'] = None
                stream['forwarder'].offer(block)
        try:
            status = process.wait(timeout=2)
            result['observedReturncode'] = status
            if not result['timedOut']:
                result.update(state='exit-observed', exitCode=status if status >= 0 else None,
                              signal=-status if status < 0 else None)
        except subprocess.TimeoutExpired:
            result['errors'].append('process wait incomplete')
    finally:
        selector.close()
        if process is not None:
            for pipe in (process.stdout, process.stderr):
                pipe.close()
        for channel, stream in streams.items():
            stream['errors'].extend(stream['forwarder'].finish())
            if stream['fd'] is not None:
                try:
                    os.fsync(stream['fd'])
                except OSError as error:
                    stream['errors'].append('spool fsync: ' + str(error))
                finally:
                    os.close(stream['fd'])
            result['streams'].append({
                'role': role + '-' + channel, 'path': channel,
                'retainedRef': stream['name'], 'retainedLength': stream['retained'],
                'retainedSha256': stream['retainedDigest'].hexdigest(),
                'originalLength': stream['observed'],
                'originalSha256': stream['digest'].hexdigest() if stream['eof'] else None,
                'eof': stream['eof'], 'errors': stream['errors']})
        result['finishedAt'] = now()
    return result


def execute(attempt, role):
    directory, req, binding = context(attempt)
    if role == 'child':
        started = load(directory, 'run-started.json')
        require(started['requestSha256'] == binding['requestSha256'], 'run marker mismatch')
    claim(directory, role, binding)
    if role == 'child':
        source_snapshot(directory, req, 'sourceBefore')
        outcome = capture(directory, 'command', req['argv'], req['worktree'], req['timeoutSeconds'])
        source_snapshot(directory, req, 'sourceAfter')
    else:
        argv = [req['wrapper'], '--timeout-seconds', str(req['timeoutSeconds']), '--',
                binding['python']['path'], binding['asset']['path'], 'child',
                '--attempt-dir', str(directory)]
        # Fleet owns its cleanup lifetime. This same task-owned Rex process
        # remains its bounded-storage pipe reader if the caller stops waiting.
        outcome = capture(directory, 'wrapper', argv, req['worktree'], None)
        outcome['teardown'] = 'unknown'
    outcome['requestSha256'] = binding['requestSha256']
    publish(directory, role + '-result.json', outcome)
    return 0  # Process success means evidence publication, never check success.


def optional_record(directory, name, digest, errors):
    try:
        value = load(directory, name)
        require(isinstance(value, dict), 'record must be an object')
        if name.endswith('-result.json') or name.endswith('-started.json'):
            require(value['requestSha256'] == digest, 'record digest mismatch')
        return value
    except FileNotFoundError:
        return None
    except (OSError, ValueError, KeyError, TypeError) as error:
        errors.append(name + ': ' + str(error))
        return None


def read_attempt(attempt, cap):
    directory, req, binding = context(attempt, fresh=False)
    errors = []
    digest = binding['requestSha256']
    started = optional_record(directory, 'run-started.json', digest, errors)
    child_started = optional_record(directory, 'child-started.json', digest, errors)
    child = optional_record(directory, 'child-result.json', digest, errors)
    wrapper = optional_record(directory, 'run-result.json', digest, errors)
    before = optional_record(directory, 'sourceBefore.json', digest, errors)
    after = optional_record(directory, 'sourceAfter.json', digest, errors)
    value = {'schema': SCHEMA, 'attemptId': req['attemptId'], 'requestSha256': digest,
             'attemptDir': str(directory), 'request': req, 'binding': binding,
             'currentIdentity': current_identity(req, binding),
             'phase': 'wrapper-finished' if wrapper else ('started' if started else 'prepared'),
             'startedAt': started['startedAt'] if started else None,
             'finishedAt': wrapper['finishedAt'] if wrapper else None,
             'command': {k: v for k, v in (child or unknown()).items() if k != 'streams'},
             'wrapper': {k: v for k, v in (wrapper or dict(unknown(), teardown='unknown')).items()
                         if k != 'streams'},
             'sourceBefore': before, 'sourceAfter': after, 'blobs': [], 'errors': errors,
             'limitations': ['repository-wide, HEAD and index identity unknown; selected paths only',
                             'no snapshot isolation or absence-of-transient-writes guarantee',
                             'wrapper teardown/cgroup safety unknown; no stream interleaving guarantee',
                             'filesystem locality requires host deployment qualification',
                             'returned bytes may be excerpts; retainedRef is host-local only']}
    if not started and not child_started and not child and not wrapper and not before and not after:
        value['command']['state'] = 'not-started'
    if wrapper and not child:
        errors.append('child final record missing; command outcome unknown')
    descriptors = []
    prefixes = {}
    for record in (child, wrapper):
        if record:
            require(isinstance(record['streams'], list) and len(record['streams']) <= 2,
                    'invalid stream descriptor list')
            descriptors.extend(record['streams'])
    for snapshot in (before, after):
        if snapshot:
            require(isinstance(snapshot['entries'], list)
                    and [entry['path'] for entry in snapshot['entries']] == req['sourcePaths'],
                    'source selection mismatch')
            for entry in snapshot['entries']:
                if entry['blob']:
                    descriptors.append(entry['blob'])
    # A killed collector has no final descriptor: return only point-in-time
    # prefixes from fixed, known spool names, never directory discovery.
    known = {d['retainedRef'] for d in descriptors}
    for role in ('command', 'wrapper'):
        for channel in ('stdout', 'stderr'):
            name = role + '-' + channel + '.bin'
            if name in known:
                continue
            try:
                data = read_bytes(directory / name, STREAM_CAP)
            except FileNotFoundError:
                continue
            except (OSError, ValueError) as error:
                errors.append(name + ': ' + str(error))
                continue
            prefixes[name] = data
            if role == 'command':
                value['command']['state'] = 'unknown'
            descriptors.append(descriptor(role + '-' + channel, channel, name, data,
                                          None, None, False, ['uncommitted point-in-time prefix']))
    payloads = []
    for desc in descriptors:
        try:
            name = desc['retainedRef']
            require(re.fullmatch(r'(command|wrapper)-(stdout|stderr)\.bin|source(Before|After)-source-[0-7]\.bin', name),
                    'invalid retained reference')
            data = prefixes[name] if name in prefixes else read_bytes(
                directory / name, max(STREAM_CAP, SOURCE_CAP))
            require(len(data) == desc['retainedLength'] and sha(data) == desc['retainedSha256'],
                    'retained length/hash mismatch')
            require(type(desc['eof']) is bool and isinstance(desc['errors'], list)
                    and all(isinstance(error, str) for error in desc['errors']),
                    'invalid coverage metadata')
            require(desc['originalLength'] is None or (type(desc['originalLength']) is int
                    and desc['originalLength'] >= len(data)), 'invalid original length')
            if desc['eof'] and desc['originalLength'] == len(data):
                require(desc['originalSha256'] == sha(data), 'full original hash mismatch')
            payloads.append(data)
            value['blobs'].append(dict(desc, byteLength=0, sha256=sha(b''), dataBase64='',
                                       coverage='excerpt', offset=0, omittedBytes=desc['originalLength'],
                                       retainedOmittedBytes=desc['retainedLength']))
        except (OSError, ValueError, KeyError, TypeError) as error:
            errors.append(str(desc.get('retainedRef') if isinstance(desc, dict) else None)
                          + ': ' + str(error))
    # Metadata is bounded by the strict request schema and fixed artifact count.
    # Small caller budgets get an explicit summary, not silently missing fields.
    if len(encoded(value)) + 1 > cap:
        summary = {'schema': SCHEMA, 'attemptId': req['attemptId'], 'requestSha256': digest,
                   'attemptDir': str(directory), 'phase': 'return-budget-exceeded',
                   'command': unknown(), 'wrapper': dict(unknown(), teardown='unknown'),
                   'errors': ['metadata exceeds return budget; inspect retained attempt explicitly'],
                   'blobs': [], 'limitations': ['no evidence admitted in this bounded summary']}
        if len(encoded(summary)) + 1 > cap:
            summary['attemptDir'] = None  # Caller already retains the prepare reference.
        require(len(encoded(summary)) + 1 <= cap, 'summary exceeds return budget')
        return summary
    for index, (blob, data) in enumerate(zip(value['blobs'], payloads)):
        remaining = cap - len(encoded(value)) - 1
        share = remaining // (len(payloads) - index)
        count = min(len(data), max(0, (share - 256) // 4 * 3))
        returned = data[:count]
        blob.update(byteLength=count, sha256=sha(returned),
                    dataBase64=base64.b64encode(returned).decode(),
                    omittedBytes=blob['originalLength'] - count if blob['originalLength'] is not None else None,
                    retainedOmittedBytes=blob['retainedLength'] - count,
                    coverage='full' if count == blob['retainedLength'] == blob['originalLength']
                    and blob['eof'] and not any(e.startswith(('spool:', 'spool fsync:', 'read:'))
                                               for e in blob['errors']) else 'excerpt')
    require(len(encoded(value)) + 1 <= cap, 'return bound exceeded')
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='mode', required=True)
    sub.add_parser('prepare').add_argument('--state-root', required=True)
    for mode in ('run', 'child', 'read'):
        command = sub.add_parser(mode)
        command.add_argument('--attempt-dir', required=True)
        if mode == 'read':
            command.add_argument('--max-bytes', type=int, default=RETURN_CAP)
    args = parser.parse_args()
    try:
        if args.mode == 'prepare':
            raw = sys.stdin.buffer.read(REQUEST_CAP + 1)
            require(len(raw) <= REQUEST_CAP, 'request exceeds 16 KiB')
            value = prepare(args.state_root, json.loads(raw))
        elif args.mode == 'read':
            require(1024 <= args.max_bytes <= RETURN_CAP, 'return cap must be 1024..49152')
            value = read_attempt(args.attempt_dir, args.max_bytes)
        else:
            return execute(args.attempt_dir, args.mode)
        sys.stdout.buffer.write(encoded(value) + b'\n')
        return 0
    except (OSError, ValueError, KeyError, TypeError, AttributeError, RecursionError, OverflowError) as error:
        if args.mode == 'read':
            # Invalid artifacts are uncertainty. Still one bounded JSON value.
            value = {'schema': SCHEMA, 'phase': 'invalid-evidence', 'command': unknown(),
                     'errors': [str(error)[:256]], 'blobs': []}
            sys.stdout.buffer.write(encoded(value) + b'\n')
        else:
            print(str(error), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
