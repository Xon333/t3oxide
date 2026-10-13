"""Collector unit checks with synthetic process data only; no resource sampling."""
import importlib.util
import json
from pathlib import Path
import plistlib
import sys
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('measure', Path(__file__).resolve().parents[1] / 'scripts/measure.py')
measure = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(measure)


class MeasurementTests(unittest.TestCase):
    def test_explicit_roots_must_all_exist(self):
        rows = {11: {'name': 't3oxide'}, 12: {'name': 'other'}}
        self.assertEqual(measure.select_roots(rows, {12}), {12})
        for roots in ({11, 99}, {0}, {-1}):
            with self.assertRaises(ValueError):
                measure.select_roots(rows, roots)

    def test_native_autoselection_is_unambiguous(self):
        self.assertEqual(measure.select_roots({11: {'name': 't3oxide'}}, set()), {11})
        with self.assertRaises(ValueError):
            measure.select_roots({11: {'name': 't3oxide'}, 12: {'name': 't3oxide'}}, set())
        with self.assertRaises(ValueError):
            measure.select_roots({11: {'name': 'WebContent'}}, set())

    def test_missing_is_not_zero_and_overlapping_roots_deduplicate(self):
        self.assertIsNone(measure.complete_sum([], 'rss_bytes'))
        self.assertIsNone(measure.complete_sum([{'rss_bytes': 4}, {'rss_bytes': None}], 'rss_bytes'))
        self.assertEqual(measure.complete_sum([{'rss_bytes': 0}], 'rss_bytes'), 0)
        self.assertEqual(measure.children({11: {'ppid': 1}, 12: {'ppid': 11}}, {11, 12}), {11, 12})

    def test_installed_build_comes_from_bundle_not_checkout(self):
        with tempfile.TemporaryDirectory() as temp:
            contents = Path(temp) / 'fixture.app/Contents'
            contents.mkdir(parents=True)
            (contents / 'Info.plist').write_bytes(plistlib.dumps({
                'CFBundleShortVersionString': '9.8.7', 'CFBundleVersion': '42',
                'CFBundleIdentifier': 'test.fixture'}))
            with patch.object(measure, 'run', return_value=str(contents / 'MacOS/t3oxide')):
                info = measure.installed_app_build(11)
            self.assertEqual((info['version'], info['build']), ('9.8.7', '42'))
            self.assertNotIn(temp, json.dumps(info))
        for executable in (None, 't3oxide', '/usr/bin/node', '/missing.app/Contents/MacOS/fixture'):
            with patch.object(measure, 'run', return_value=executable):
                self.assertIsNone(measure.installed_app_build(11))

    def test_capture_metadata_and_exit_use_only_mocked_metrics(self):
        process = {11: {'pid': 11, 'ppid': 1, 'name': 't3oxide', 'rss_bytes': 64, 'cpu_seconds_total': 1.0}}
        extra = {'footprint_bytes': 80, 'resident_bytes': 64, 'start_identity': 7,
                 'idle_wakeups_total': 0, 'interrupt_wakeups_total': 0}
        with tempfile.TemporaryDirectory() as temp, \
             patch.object(sys, 'argv', ['measure.py', '--seconds', '1', '--output', temp]), \
             patch.object(measure, 'process_table', side_effect=[process, process, {}]), \
             patch.object(measure, 'mac_rusage', return_value=extra), \
             patch.object(measure, 'system_snapshot', return_value={'battery': 'fixture only'}), \
             patch.object(measure, 'installed_app_build', return_value={'version': '9.8.7', 'build': '42'}), \
             patch.object(measure.platform, 'system', return_value='Darwin'), \
             patch.object(measure.time, 'monotonic', side_effect=[0.0, 0.0, 1.0, 2.0]), \
             patch.object(measure.time, 'sleep'), patch('builtins.print'):
            measure.main()
            directory = next(Path(temp).iterdir())
            metadata = json.loads((directory / 'metadata.json').read_text())
            samples = [json.loads(line) for line in (directory / 'samples.jsonl').read_text().splitlines()]
            self.assertEqual(metadata['installed_app_builds']['11']['build'], '42')
            self.assertEqual(len(metadata['collector_sha256']), 64)
            self.assertEqual(samples[-1]['process_count'], 0)
            self.assertIsNone(samples[-1]['rss_sum_bytes'])
            self.assertEqual(samples[-1]['root_pids_not_observed'], [11])
            self.assertEqual(directory.stat().st_mode & 0o777, 0o700)


if __name__ == '__main__':
    unittest.main()
