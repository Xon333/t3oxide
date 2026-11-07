"""Opt-in fixture checks only: temporary private state and a synthetic wrapper.

No real t3-check, Rex, native service, Rust build, or installed state is used.
Execution remains subject to the implementation owner's current test hold.
"""
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import uuid

sys.dont_write_bytecode = True
ASSET = Path(__file__).resolve().parents[1] / 'scripts/check-evidence.py'
SPEC = importlib.util.spec_from_file_location('check_evidence', ASSET)
evidence = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(evidence)


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        # Intentionally outside OS temporary roots: production refuses those.
        self.temp = tempfile.TemporaryDirectory(prefix='.check-evidence-fixture-', dir=Path.home())
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.asset = ASSET
        self.root = self.base / 'state'
        self.root.mkdir(mode=0o700)
        self.cwd = self.base / 'work'
        self.cwd.mkdir()
        self.source = self.cwd / 'selected.py'
        self.source.write_bytes(b'original\xff\x00\n')
        self.wrapper = self.base / 'fake-wrapper'
        self.set_wrapper("""
import subprocess
result = subprocess.run(sys.argv[sys.argv.index('--') + 1:])
sys.exit(75)
""")
        self.request = {
            'schema': evidence.SCHEMA, 'attemptId': uuid.uuid4().hex,
            'target': {'environmentId': 'fixture-env', 'projectId': 'fixture-project',
                       'threadId': 'fixture-thread', 'sourceRevision': 'fixture-revision'},
            'host': socket.gethostname(), 'worktree': str(self.cwd.resolve()),
            'argv': [sys.executable, '-c', 'import sys; sys.exit(1)'],
            'timeoutSeconds': 5, 'sourcePaths': ['selected.py'],
            'wrapper': str(self.wrapper.resolve()),
            'assetSha256': hashlib.sha256(ASSET.read_bytes()).hexdigest()}

    def set_wrapper(self, body):
        self.wrapper.write_text('#!' + str(Path(sys.executable).resolve()) + '\n'
                                'import sys, os\nfrom pathlib import Path\n'
                                'with open(Path(__file__).with_suffix(".count"), "ab") as f: f.write(b"1")\n'
                                + body)
        self.wrapper.chmod(0o700)

    def use_alternate_helper(self):
        self.asset = self.base / 'alternate-helper.py'
        self.asset.write_bytes(ASSET.read_bytes() + b'\n# synthetic historical helper revision\n')
        self.asset.chmod(0o600)
        self.request['assetSha256'] = hashlib.sha256(self.asset.read_bytes()).hexdigest()

    def cli(self, *args, data=None):
        return subprocess.run([sys.executable, '-B', str(self.asset), *args],
                              input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                              timeout=15)

    def prepare(self):
        result = self.cli('prepare', '--state-root', str(self.root.resolve()),
                          data=json.dumps(self.request).encode())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.prepared = json.loads(result.stdout)
        self.attempt = Path(self.prepared['attemptDir'])
        return self.prepared

    def run_and_read(self):
        self.prepare()
        result = self.cli('run', '--attempt-dir', str(self.attempt))
        self.assertEqual(result.returncode, 0, result.stderr)
        result = self.cli('read', '--attempt-dir', str(self.attempt), '--max-bytes', '49152')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertLessEqual(len(result.stdout), 49152)
        return json.loads(result.stdout)

    def test_exclusive_prepare_run_and_child_and_private_files(self):
        value = self.run_and_read()
        self.assertEqual(value['command']['exitCode'], 1)
        self.assertEqual(value['wrapper']['exitCode'], 75)
        self.assertEqual(value['wrapper']['teardown'], 'unknown')
        for mode in ('run', 'child'):
            self.assertNotEqual(self.cli(mode, '--attempt-dir', str(self.attempt)).returncode, 0)
        repeated = self.cli('prepare', '--state-root', str(self.root.resolve()),
                            data=json.dumps(self.request).encode())
        self.assertNotEqual(repeated.returncode, 0)
        self.assertEqual(self.wrapper.with_suffix('.count').read_bytes(), b'1')
        self.assertEqual(self.attempt.stat().st_mode & 0o777, 0o700)
        for path in self.attempt.iterdir():
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_exact_non_utf8_source_streams_stdin_eof_and_after_content(self):
        self.request['argv'] = [sys.executable, '-c',
            'import sys; from pathlib import Path; '
            'assert sys.stdin.buffer.read() == b""; '
            'sys.stdout.buffer.write(bytes([255,0,254])); '
            'sys.stderr.buffer.write(bytes([128,129])); '
            'Path("selected.py").write_bytes(bytes([0,253])); sys.exit(1)']
        value = self.run_and_read()
        blobs = {(b['role'], b['path']): b for b in value['blobs']}
        for key, expected in ((('command-stdout', 'stdout'), b'\xff\x00\xfe'),
                              (('command-stderr', 'stderr'), b'\x80\x81'),
                              (('sourceBefore', 'selected.py'), b'original\xff\x00\n'),
                              (('sourceAfter', 'selected.py'), b'\x00\xfd')):
            blob = blobs[key]
            decoded = base64.b64decode(blob['dataBase64'], validate=True)
            self.assertEqual(decoded, expected)
            self.assertEqual(blob['sha256'], hashlib.sha256(decoded).hexdigest())
            self.assertEqual(blob['byteLength'], len(decoded))
            self.assertEqual(blob['coverage'], 'full')
        self.assertEqual(value['sourceBefore']['repositoryIdentity'], 'unknown')
        self.assertIsNone(value['sourceBefore']['head'])

    def test_retention_excerpts_and_aggregate_return_bound(self):
        self.source.write_bytes(b'\xfe' * 70000)
        self.request['argv'] = [sys.executable, '-c',
                                'import sys; sys.stdout.buffer.write(b"x" * 300000)']
        value = self.run_and_read()
        blob = next(b for b in value['blobs'] if b['role'] == 'command-stdout')
        self.assertEqual(blob['originalLength'], 300000)
        self.assertEqual(blob['originalSha256'], hashlib.sha256(b'x' * 300000).hexdigest())
        self.assertEqual(blob['retainedLength'], evidence.STREAM_CAP)
        self.assertLess(blob['byteLength'], blob['retainedLength'])
        self.assertEqual(blob['coverage'], 'excerpt')
        source = next(b for b in value['blobs'] if b['role'] == 'sourceBefore')
        self.assertGreater(source['byteLength'], 0)
        self.assertFalse(source['eof'])
        self.assertIsNone(source['originalSha256'])
        small = self.cli('read', '--attempt-dir', str(self.attempt), '--max-bytes', '1024')
        self.assertLessEqual(len(small.stdout), 1024)
        self.assertEqual(json.loads(small.stdout)['phase'], 'return-budget-exceeded')

    def test_missing_child_record_is_unknown_and_read_never_launches(self):
        self.set_wrapper('sys.exit(0)\n')
        value = self.run_and_read()
        self.assertEqual(value['command']['state'], 'unknown')
        self.assertEqual(value['wrapper']['exitCode'], 0)
        for _ in range(2):
            self.assertEqual(self.cli('read', '--attempt-dir', str(self.attempt)).returncode, 0)
        self.assertEqual(self.wrapper.with_suffix('.count').read_bytes(), b'1')

    def test_killed_collector_returns_uncommitted_prefix_unknown(self):
        self.request['argv'] = [sys.executable, '-c',
            'import os,time; from pathlib import Path; '
            'os.write(1,b"partial\\xff"); Path("ready").write_text(str(os.getpid())); time.sleep(30)']
        self.set_wrapper("""
import subprocess, time, signal
child = subprocess.Popen(sys.argv[sys.argv.index('--') + 1:])
deadline = time.monotonic() + 4
while not Path('ready').exists() and time.monotonic() < deadline:
    time.sleep(0.01)
try:
    os.kill(child.pid, signal.SIGKILL)
    if Path('ready').exists():
        os.killpg(int(Path('ready').read_text()), signal.SIGKILL)
finally:
    child.wait(timeout=2)
sys.exit(75)
""")
        value = self.run_and_read()
        self.assertEqual(value['command']['state'], 'unknown')
        self.assertFalse((self.attempt / 'child-result.json').exists())
        blob = next(b for b in value['blobs'] if b['role'] == 'command-stdout')
        self.assertEqual(blob['coverage'], 'excerpt')
        self.assertFalse(blob['eof'])
        self.assertIsNone(blob['originalLength'])

    def test_timeout_and_spawn_error_do_not_invent_exit(self):
        self.request['timeoutSeconds'] = 1
        self.request['argv'] = [sys.executable, '-c', 'import time; time.sleep(30)']
        value = self.run_and_read()
        self.assertEqual(value['command']['state'], 'unknown')
        self.assertTrue(value['command']['timedOut'])
        self.request['attemptId'] = uuid.uuid4().hex
        self.request['argv'] = ['/nonexistent-fixture-program']
        value = self.run_and_read()
        self.assertEqual(value['command']['state'], 'unknown')
        self.assertIsNone(value['command']['exitCode'])
        self.assertIsNotNone(value['command']['spawnError'])

    def test_path_traversal_leaf_and_parent_symlinks_refused(self):
        (self.cwd / 'link').symlink_to(self.source)
        (self.cwd / 'dirlink').symlink_to(self.cwd, target_is_directory=True)
        for path in ('../selected.py', '/etc/passwd', 'link', 'dirlink/selected.py'):
            with self.subTest(path=path):
                self.request['attemptId'] = uuid.uuid4().hex
                self.request['sourcePaths'] = [path]
                result = self.cli('prepare', '--state-root', str(self.root.resolve()),
                                  data=json.dumps(self.request).encode())
                self.assertNotEqual(result.returncode, 0)

    def test_request_host_asset_and_cwd_identity_are_checked(self):
        for key, value in (('host', 'not-the-executing-host'), ('assetSha256', '0' * 64),
                           ('timeoutSeconds', 901)):
            req = dict(self.request, **{key: value})
            result = self.cli('prepare', '--state-root', str(self.root.resolve()),
                              data=json.dumps(req).encode())
            self.assertNotEqual(result.returncode, 0)
        self.prepare()
        raw = json.loads((self.attempt / 'request.json').read_bytes())
        raw['argv'] = ['modified']
        (self.attempt / 'request.json').write_bytes(evidence.encoded(raw))
        result = self.cli('run', '--attempt-dir', str(self.attempt))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.attempt / 'run-started.json').exists())

    def test_alternate_helper_fixture_has_authentic_distinct_asset_hash(self):
        self.use_alternate_helper()
        value = self.run_and_read()
        digest = hashlib.sha256(self.asset.read_bytes()).hexdigest()
        self.assertNotEqual(digest, hashlib.sha256(ASSET.read_bytes()).hexdigest())
        self.assertEqual(value['request']['assetSha256'], digest)
        self.assertEqual(value['binding']['asset']['sha256'], digest)
        self.assertEqual(value['binding']['asset']['path'], str(self.asset))
        self.assertEqual(value['command']['exitCode'], 1)
        self.assertEqual(value['wrapper']['exitCode'], 75)
        self.assertEqual(self.wrapper.with_suffix('.count').read_bytes(), b'1')

    def test_delayed_wrapper_cleanup_must_outlive_workload_plus_five(self):
        # The task-owned helper must keep draining until Fleet cleanup exits.
        self.request['timeoutSeconds'] = 1
        self.request['argv'] = [sys.executable, '-c', 'pass']
        self.set_wrapper("""
import subprocess, time
result = subprocess.run(sys.argv[sys.argv.index('--') + 1:])
time.sleep(6.5)
Path('cleanup-complete').write_text('done')
os.write(1, b'cleanup complete\\n')
sys.exit(75)
""")
        value = self.run_and_read()
        self.assertEqual(value['command']['exitCode'], 0)
        self.assertTrue((self.cwd / 'cleanup-complete').exists(),
                        'collector killed the fake cleanup owner before cleanup completed')
        self.assertEqual(value['wrapper']['exitCode'], 75)

    def test_closed_display_does_not_close_wrapper_cleanup_pipes(self):
        self.request['argv'] = [sys.executable, '-c', 'pass']
        self.set_wrapper("""
import subprocess, time
subprocess.run(sys.argv[sys.argv.index('--') + 1:])
time.sleep(0.3)
os.write(1, b'x' * 400000)
Path('cleanup-complete').write_text('done')
sys.exit(75)
""")
        self.prepare()
        process = subprocess.Popen([sys.executable, '-B', str(self.asset), 'run',
                                    '--attempt-dir', str(self.attempt)],
                                   stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE)
        process.stdout.close()
        process.stdout = None
        try:
            _, stderr = process.communicate(timeout=10)
            self.assertEqual(process.returncode, 0, stderr)
        finally:
            process.stderr.close()
            if process.poll() is None:
                process.kill()
                process.wait()
        value = json.loads(self.cli('read', '--attempt-dir', str(self.attempt)).stdout)
        self.assertTrue((self.cwd / 'cleanup-complete').exists())
        self.assertEqual(value['wrapper']['exitCode'], 75)
        spool = self.attempt / 'wrapper-stdout.bin'
        self.assertEqual(len(spool.read_bytes()), evidence.STREAM_CAP)

    def test_surviving_asset_publication_link_does_not_weaken_evidence_authentication(self):
        self.use_alternate_helper()
        abandoned = self.base / '.publish-interrupted'
        os.link(self.asset, abandoned)
        self.assertEqual(self.asset.stat().st_nlink, 2)
        value = self.run_and_read()
        self.assertEqual(value['wrapper']['exitCode'], 75)
        self.assertTrue(abandoned.exists(), 'recovery must preserve the surviving stage')
        os.link(self.attempt / 'request.json', self.base / 'unexpected-request-link')
        refused = self.cli('read', '--attempt-dir', str(self.attempt))
        self.assertNotEqual(refused.returncode, 0, 'request evidence must remain single-linked')
        self.assertEqual(self.wrapper.with_suffix('.count').read_bytes(), b'1')

    def test_historical_read_survives_removed_and_replaced_worktree_and_wrapper(self):
        original = self.run_and_read()
        retained = {path.name: path.read_bytes() for path in self.attempt.iterdir()}
        self.cwd.rename(self.base / 'old-work')
        self.wrapper.unlink()
        missing = self.cli('read', '--attempt-dir', str(self.attempt))
        self.assertEqual(missing.returncode, 0, missing.stderr)
        value = json.loads(missing.stdout)
        self.assertEqual(value['currentIdentity']['worktree']['state'], 'unknown')
        self.assertEqual(value['command'], original['command'])
        self.cwd.mkdir()
        self.set_wrapper('raise RuntimeError("historical read must never launch")\n')
        replaced = self.cli('read', '--attempt-dir', str(self.attempt))
        self.assertEqual(replaced.returncode, 0, replaced.stderr)
        value = json.loads(replaced.stdout)
        self.assertEqual(value['currentIdentity']['worktree']['state'], 'changed')
        self.assertEqual(value['binding'], original['binding'])
        self.assertEqual(value['blobs'], original['blobs'])
        self.assertEqual(self.wrapper.with_suffix('.count').read_bytes(), b'1')
        self.assertEqual({path.name: path.read_bytes() for path in self.attempt.iterdir()}, retained)
        # The same drift is still a hard refusal for any fresh execution.
        self.assertNotEqual(self.cli('run', '--attempt-dir', str(self.attempt)).returncode, 0)
        self.assertNotEqual(self.cli('child', '--attempt-dir', str(self.attempt)).returncode, 0)

    def test_historical_read_needs_no_current_executable_identity(self):
        original = self.run_and_read()
        changed_asset = self.base / 'changed-helper.py'
        changed_asset.write_bytes(ASSET.read_bytes())
        result = subprocess.run([sys.executable, '-B', str(changed_asset), 'read',
                                 '--attempt-dir', str(self.attempt)], stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)['binding'], original['binding'])
        changed_asset.write_bytes(ASSET.read_bytes() + b'\n# synthetic later revision\n')
        wrong_asset = subprocess.run([sys.executable, '-B', str(changed_asset), 'read',
                                     '--attempt-dir', str(self.attempt)], stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, timeout=5)
        self.assertEqual(wrong_asset.returncode, 2)
        self.assertIn('adapter asset hash mismatch', json.loads(wrong_asset.stdout)['errors'])
        # A changed/missing interpreter path must not make historical read try
        # to inspect or launch that interpreter, wrapper, or replacement asset.
        with patch.object(evidence.sys, 'executable', '/missing-fixture-python'), \
             patch.object(evidence, 'file_identity', wraps=evidence.file_identity) as identity, \
             patch.object(evidence.socket, 'gethostname', return_value='changed-fixture-host'):
            value = evidence.read_attempt(str(self.attempt), evidence.RETURN_CAP)
        identity.assert_called_once_with(ASSET, allow_asset_links=True)
        self.assertEqual(value['currentIdentity']['host']['state'], 'changed')
        for key in ('asset', 'python', 'wrapper'):
            self.assertEqual(value['currentIdentity'][key]['state'], 'unknown')
        self.assertEqual(value['binding'], original['binding'])
        self.assertEqual(value['blobs'], original['blobs'])
        self.assertEqual(self.wrapper.with_suffix('.count').read_bytes(), b'1')

    def test_historical_read_still_authenticates_request_binding_and_blob_bytes(self):
        self.run_and_read()
        request_path = self.attempt / 'request.json'
        original = request_path.read_bytes()
        changed = json.loads(original)
        changed['argv'] = ['tampered']
        request_path.write_bytes(evidence.encoded(changed))
        result = self.cli('read', '--attempt-dir', str(self.attempt))
        self.assertEqual(result.returncode, 2)
        self.assertIn('immutable request digest mismatch', json.loads(result.stdout)['errors'])
        request_path.write_bytes(original)
        binding_path = self.attempt / 'binding.json'
        binding_bytes = binding_path.read_bytes()
        binding = json.loads(binding_bytes)
        binding['asset']['sha256'] = '0' * 64
        binding_path.write_bytes(evidence.encoded(binding))
        result = self.cli('read', '--attempt-dir', str(self.attempt))
        self.assertEqual(result.returncode, 2)
        self.assertIn('immutable binding mismatch', json.loads(result.stdout)['errors'])
        binding_path.write_bytes(binding_bytes)
        (self.attempt / 'sourceBefore-source-0.bin').write_bytes(b'tampered')
        result = self.cli('read', '--attempt-dir', str(self.attempt))
        self.assertEqual(result.returncode, 0)
        value = json.loads(result.stdout)
        self.assertTrue(any('retained length/hash mismatch' in error for error in value['errors']))
        self.assertFalse(any(blob['role'] == 'sourceBefore' for blob in value['blobs']))
        self.assertEqual(self.wrapper.with_suffix('.count').read_bytes(), b'1')

    def test_spool_failure_is_explicit_separate_from_exit(self):
        self.prepare()
        original_write = os.write

        def fail_spool(fd, data):
            if fd not in (1, 2):
                raise OSError('fixture disk full')
            return original_write(fd, data)

        with patch.object(evidence.os, 'write', side_effect=fail_spool):
            result = evidence.capture(self.attempt, 'command',
                [sys.executable, '-c', 'import os; os.write(1,b"fixture")'], str(self.cwd), 3)
        self.assertEqual(result['exitCode'], 0)
        stream = next(s for s in result['streams'] if s['path'] == 'stdout')
        self.assertTrue(any(error.startswith('spool:') for error in stream['errors']))
        self.assertEqual(stream['retainedLength'], 0)


def rust_fixture(alternate_helper=False):
    fixture = EvidenceTests()
    fixture.setUp()
    try:
        if alternate_helper:
            fixture.use_alternate_helper()
        value = fixture.run_and_read()
        print(json.dumps({'envelope': value, 'preparation': fixture.prepared}))
    finally:
        fixture.doCleanups()


if __name__ == '__main__':
    if len(sys.argv) > 1 and sys.argv[1] == '--rust-fixture':
        import argparse
        parser = argparse.ArgumentParser(description='Emit ephemeral fake-wrapper evidence for Rust verification')
        parser.add_argument('--rust-fixture', action='store_true')
        parser.add_argument('--alternate-helper', action='store_true')
        args = parser.parse_args()
        rust_fixture(args.alternate_helper)
    else:
        unittest.main()
