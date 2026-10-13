#!/usr/bin/env python3
"""User-operated, finite, read-only capture. No install, sudo, cleanup or upload.
macOS footprint ABI: apple-oss-distributions/xnu, bsd/sys/resource.h, rusage_info_v0.
This captures resource evidence, not a claim of battery/energy savings.
"""
import argparse
import ctypes
import datetime as dt
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import time


def run(*args):
    try:
        p = subprocess.run(args, text=True, capture_output=True, timeout=5, env={**os.environ, 'LC_ALL': 'C'})
        return p.stdout.strip() if p.returncode == 0 else None
    except (OSError, subprocess.TimeoutExpired):
        return None


def cpu_seconds(value):
    days, _, rest = value.rpartition('-')
    parts = [float(x) for x in (rest if days else value).split(':')]
    total = 0.0
    for part in parts:
        total = total * 60 + part
    return total + (float(days) * 86400 if days else 0)


class Rusage(ctypes.Structure):
    _fields_ = [('uuid', ctypes.c_uint8 * 16)] + [(k, ctypes.c_uint64) for k in (
        'user_time', 'system_time', 'pkg_idle_wkups', 'interrupt_wkups', 'pageins',
        'wired_size', 'resident_size', 'phys_footprint', 'proc_start_abstime', 'proc_exit_abstime')]


def mac_rusage(pid):
    try:
        lib = ctypes.CDLL('/usr/lib/libproc.dylib', use_errno=True)
        lib.proc_pid_rusage.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_void_p]
        lib.proc_pid_rusage.restype = ctypes.c_int
        data = Rusage()
        if lib.proc_pid_rusage(pid, 0, ctypes.byref(data)) != 0:
            return {'footprint_bytes': None, 'rusage_error': ctypes.get_errno()}
        return {'footprint_bytes': data.phys_footprint, 'resident_bytes': data.resident_size,
                'idle_wakeups_total': data.pkg_idle_wkups, 'interrupt_wakeups_total': data.interrupt_wkups,
                'start_identity': data.proc_start_abstime}
    except (OSError, AttributeError):
        return {'footprint_bytes': None, 'rusage_error': 'API unavailable'}


def linux_extra(pid):
    result = {'pss_bytes': None, 'swap_bytes': None, 'start_identity': None}
    try:
        stat = Path(f'/proc/{pid}/stat').read_text()
        result['start_identity'] = int(stat[stat.rfind(')')+2:].split()[19])
        for line in Path(f'/proc/{pid}/smaps_rollup').read_text().splitlines():
            key, _, value = line.partition(':')
            if key in ('Pss', 'Swap'):
                result['pss_bytes' if key == 'Pss' else 'swap_bytes'] = int(value.split()[0]) * 1024
    except (OSError, ValueError, IndexError):
        pass
    return result


def process_table():
    rows = {}
    output = run('ps', '-axo', 'pid=,ppid=,rss=,time=,comm=') or ''
    for line in output.splitlines():
        try:
            pid, parent, rss, cpu, executable = line.strip().split(None, 4)
            # Deliberately store basename only: no arguments, usernames, paths or prompts.
            rows[int(pid)] = {'pid': int(pid), 'ppid': int(parent), 'name': Path(executable).name,
                              'rss_bytes': int(rss) * 1024, 'cpu_seconds_total': cpu_seconds(cpu)}
        except (ValueError, IndexError):
            continue
    return rows


def system_snapshot():
    if platform.system() == 'Darwin':
        return {'swap': run('sysctl', '-n', 'vm.swapusage'),
                'pressure_level': run('sysctl', '-n', 'kern.memorystatus_vm_pressure_level'),
                'vm_stat': run('vm_stat'), 'battery': run('pmset', '-g', 'batt')}
    result = {}
    for key, path in [('meminfo', '/proc/meminfo'), ('memory_pressure', '/proc/pressure/memory')]:
        try:
            result[key] = Path(path).read_text().strip()
        except OSError:
            result[key] = None
    return result


def children(rows, roots):
    chosen = set(roots) & rows.keys()
    while True:
        added = {pid for pid, row in rows.items() if row['ppid'] in chosen} - chosen
        if not added:
            return chosen
        chosen |= added


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--label', default='t3oxide')
    parser.add_argument('--seconds', type=float, default=120)
    parser.add_argument('--interval', type=float, default=5)
    parser.add_argument('--pid', type=int, action='append', default=[], help='Root PID; repeat for explicitly attributed processes.')
    parser.add_argument('--output', type=Path, default=Path('metrics'))
    args = parser.parse_args()
    if not 0 < args.seconds <= 3600 or not 1 <= args.interval <= 60:
        parser.error('Use 0 < seconds <= 3600 and 1 <= interval <= 60.')
    label = re.sub(r'[^a-zA-Z0-9_.-]', '-', args.label)[:80]
    start = time.monotonic()
    stamp = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    destination = args.output / f'{label}-{stamp}'
    previous = {}; summary_rows = []; explicit = set(args.pid)
    first_table = process_table()
    if any(pid <= 0 for pid in explicit):
        parser.error('PIDs must be positive.')
    roots = explicit or {pid for pid, r in first_table.items() if r['name'].lower() == 't3oxide'}
    if not roots or not roots.intersection(first_table):
        parser.error('No t3oxide process found. Open the app, or specify --pid for a host adapter or reference process.')
    destination.mkdir(parents=True, exist_ok=False, mode=0o700)
    root_identities = {pid: (mac_rusage(pid) if platform.system() == 'Darwin' else linux_extra(pid)).get('start_identity') for pid in roots}
    changed_roots = set()
    metadata = {'schema_version': 1, 'label': label, 'os': platform.system(), 'os_release': platform.release(),
                'architecture': platform.machine(), 'logical_cpus': os.cpu_count(), 'root_pids': sorted(roots),
                'units': {'memory': 'bytes', 'cpu_percent': '100 = one CPU core; interval CPU-time delta'},
                'limitations': ['Short-lived processes between samples may be missed.',
                               'RSS sums may double-count shared memory; footprint and PSS are separate fields.',
                               'Missing metrics are null, not zero. This is not an energy or battery-life benchmark.',
                               'Only this host is measured. Remote adapters/agents are not attributed to the client.',
                               'The collector itself is excluded from the selected process totals.'],
                'system_before': system_snapshot()}
    (destination / 'metadata.json').write_text(json.dumps(metadata, indent=2)+'\n')
    next_sample = start
    with (destination / 'samples.jsonl').open('w') as out:
        while True:
            now = time.monotonic(); rows = process_table()
            valid_roots = set()
            for pid in roots.intersection(rows):
                observed = (mac_rusage(pid) if platform.system() == 'Darwin' else linux_extra(pid)).get('start_identity')
                if root_identities[pid] is not None and observed != root_identities[pid]:
                    changed_roots.add(pid)
                else:
                    valid_roots.add(pid)
            selected = children(rows, valid_roots)
            samples = []
            for pid in sorted(selected):
                row = rows[pid]
                row.update(mac_rusage(pid) if platform.system() == 'Darwin' else linux_extra(pid))
                prior = previous.get(pid); delta = now - prior[0] if prior else 0
                same = prior and row.get('start_identity') is not None and prior[1].get('start_identity') == row.get('start_identity')
                row['cpu_percent_interval'] = max(0, (row['cpu_seconds_total'] - prior[1]['cpu_seconds_total']) / delta * 100) if same and delta else None
                if same and delta and 'idle_wakeups_total' in row:
                    row['idle_wakeups_per_second'] = max(0, (row['idle_wakeups_total'] - prior[1].get('idle_wakeups_total', 0)) / delta)
                previous[pid] = (now, dict(row)); samples.append(row)
            def complete_sum(key):
                return sum(r[key] for r in samples) if samples and all(r.get(key) is not None for r in samples) else None
            sample = {'utc': dt.datetime.now(dt.timezone.utc).isoformat(), 'elapsed_seconds': round(now-start, 3),
                      'processes': samples, 'rss_sum_bytes': sum(r['rss_bytes'] for r in samples),
                      'footprint_sum_bytes': complete_sum('footprint_bytes'), 'pss_sum_bytes': complete_sum('pss_bytes'),
                      'cpu_percent_interval_sum': complete_sum('cpu_percent_interval'), 'process_count': len(samples)}
            out.write(json.dumps(sample)+'\n'); out.flush(); summary_rows.append({key: sample[key] for key in ('rss_sum_bytes', 'footprint_sum_bytes', 'pss_sum_bytes', 'cpu_percent_interval_sum')})
            if now - start >= args.seconds:
                break
            next_sample += args.interval
            time.sleep(max(0, min(next_sample, start + args.seconds) - time.monotonic()))
    def stats(key):
        values = [r[key] for r in summary_rows if r.get(key) is not None]
        return {'first': values[0], 'last': values[-1], 'peak': max(values), 'mean': sum(values)/len(values)} if values else None
    summary = {'schema_version': 1, 'label': label, 'sample_count': len(summary_rows),
               **{key: stats(key) for key in ('rss_sum_bytes','footprint_sum_bytes','pss_sum_bytes','cpu_percent_interval_sum')},
               'reused_or_unverifiable_root_pids': sorted(changed_roots), 'system_after': system_snapshot()}
    (destination / 'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
    print(destination)
    print('Return metadata.json, samples.jsonl and summary.json; no upload was performed.')


if __name__ == '__main__':
    try:
        main()
    except KeyboardInterrupt:
        print('Stopped; partial samples remain in the output directory.')
