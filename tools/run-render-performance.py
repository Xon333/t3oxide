#!/usr/bin/env python3
"""Run identical bounded foreground workloads in independent Go test processes.

For the accepted baseline only, use an ephemeral Go overlay that adds counters
without changing work, outputs, dependencies, authority, or repository files.
OS/filesystem/compiler caches remain warm: these are not cold application opens.
"""
import argparse
import hashlib
import json
import os
import platform
from pathlib import Path
import subprocess
import sys
import tempfile

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--source', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
p.add_argument('--label', choices=('baseline', 'after', 'preview-disabled', 'preview-enabled'), required=True)
p.add_argument('--runs', type=int, default=3)
p.add_argument('--samples', type=int, default=1100)
p.add_argument('--go', default=os.environ.get('T3OXIDE_GO', 'go'))
a = p.parse_args()
assert 3 <= a.runs <= 10 and 1000 <= a.samples <= 10000
source, output = a.source.resolve(), a.output.resolve()
root = Path(__file__).resolve().parents[1]
assert (source / 'go.mod').is_file()
output.mkdir(parents=True, exist_ok=True)
workload = root / ('internal/ui/preview_performance_test.go' if a.label.startswith('preview-') else 'internal/ui/render_performance_test.go')
assert not a.label.startswith('preview-') or source == root, 'preview is tested on implemented source only'

def replace_once(text, old, new):
    assert text.count(old) == 1, f'counter anchor changed: {old!r}'
    return text.replace(old, new)

with tempfile.TemporaryDirectory(prefix='t3oxide-performance-') as temporary:
    temp = Path(temporary)
    mapping = {}
    if source != root:
        virtual = source / 'internal/ui/refinement_performance_overlay_test.go'
        assert not virtual.exists()
        mapping[str(virtual)] = str(workload)
    if a.label == 'baseline':
        changes = {
            'internal/ui/render.go': [
                ('type transcriptRenderer struct {', 'type transcriptRenderer struct {\n\tperfBodies, perfAssemblies uint64'),
                ('\tr.rows = nil', '\tr.perfAssemblies++\n\tr.rows = nil'),
                ('\t\t\tbody, err := r.markdown.Render(source)', '\t\t\tr.perfBodies++\n\t\t\tbody, err := r.markdown.Render(source)'),
            ],
            'internal/ui/model.go': [
                ('type Model struct {', 'type Model struct {\n\tperfInstalls, perfSearchScans uint64'),
                ('\t\t\t\tm.viewport.SetContent(msg.content)', '\t\t\t\tm.perfInstalls++\n\t\t\t\tm.viewport.SetContent(msg.content)'),
            ],
            'internal/ui/dialog.go': [
                ('\tterm := strings.ToLower(m.searchTerm)', '\tm.perfSearchScans++\n\tterm := strings.ToLower(m.searchTerm)'),
            ],
        }
        for relative, replacements in changes.items():
            original = source / relative
            text = original.read_text()
            for old, new in replacements:
                text = replace_once(text, old, new)
            target = temp / Path(relative).name
            target.write_text(text)
            mapping[str(original)] = str(target)
    overlay = temp / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': mapping}))
    command = [a.go, 'test']
    if mapping:
        command += ['-overlay', str(overlay)]
    pattern = '^TestRefinementDemandPreviewForegroundSamples$' if a.label.startswith('preview-') else '^TestRefinementSteadyForegroundSamples$'
    command += ['./internal/ui', '-run', pattern, '-count=1', '-v']
    env = dict(os.environ)
    env['T3OXIDE_PREVIEW_PERF_SAMPLES' if a.label.startswith('preview-') else 'T3OXIDE_PERF_SAMPLES'] = str(a.samples)
    if a.label.startswith('preview-'):
        env['T3OXIDE_PREVIEW_PERF_MODE'] = a.label.removeprefix('preview-')
    manifests = []
    for run in range(1, a.runs + 1):
        log = output / f'render-{a.label}-matrix-{run}.log'
        print(f'{a.label} independent process {run}/{a.runs}: {log}', flush=True)
        with log.open('w') as stream:
            result = subprocess.run(command, cwd=source, env=env, stdout=stream, stderr=subprocess.STDOUT)
        manifests.append({'run': run, 'log': str(log), 'exit_code': result.returncode, 'log_sha256': hashlib.sha256(log.read_bytes()).hexdigest()})
        if result.returncode:
            raise SystemExit(result.returncode)
    report = {'schema': 1, 'fixture_version': 3, 'label': a.label, 'source': str(source), 'workload_sha256': hashlib.sha256(workload.read_bytes()).hexdigest(), 'command': command, 'reproduce': [sys.executable, str(Path(__file__).resolve()), *sys.argv[1:]], 'environment': {'samples': a.samples, 'go': subprocess.check_output([a.go, 'version'], text=True).strip(), 'platform': platform.platform(), 'GOMAXPROCS': env.get('GOMAXPROCS', 'Go cgroup-aware default')}, 'independent_processes': a.runs, 'warmup_events_per_scenario': 100, 'cache_state': 'warm OS/filesystem/compiler cache; independent Go processes; not cold opens', 'baseline_instrumentation': a.label == 'baseline', 'runs': manifests}
    (output / f'render-{a.label}-matrix-manifest.json').write_text(json.dumps(report, indent=2)+'\n')
