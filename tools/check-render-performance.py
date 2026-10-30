#!/usr/bin/env python3
"""Content-free regression gate from independent raw foreground samples.

First --write-budgets after baseline collection, before after measurements.
Budgets are the worst independent baseline run's p95/p99 plus 15% and 0.5/1ms
scheduler allowance, rounded upward to 0.1ms. They are shared-executor regression
limits at the named synthetic boundary, never native or physical-input promises.
"""
import argparse
import hashlib
import json
import math
from pathlib import Path
import statistics

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--baseline', type=Path, nargs='+', required=True)
p.add_argument('--after', type=Path, nargs='*', default=[])
p.add_argument('--budgets', type=Path, required=True)
p.add_argument('--write-budgets', action='store_true')
p.add_argument('--output', type=Path, required=True)
p.add_argument('--preview-comparison', action='store_true')
a=p.parse_args()

def records(paths):
    groups={}
    for path in paths:
        text=path.read_text()
        assert '\nPASS\n' in text and '\nFAIL\n' not in text, f'failed/incomplete run {path}'
        seen=set()
        for line in text.splitlines():
            start=line.find('{"')
            if start<0: continue
            row=json.loads(line[start:])
            if 'fixture_version' not in row: continue
            scenario=row['scenario']
            assert scenario not in seen, f'run duplicated {scenario}'
            seen.add(scenario)
            assert row['fixture_version']==3 and row['samples']>=1000 and row['warmup']==100 and row['failures']==0
            assert len(row['update_ns'])==row['samples']==len(row['view_ns'])
            assert row['worker_high_water']<=1 and row['pending_high_water']<=1
            groups.setdefault(scenario,[]).append(row)
        assert seen, f'no samples {path}'
    assert len(paths)>=3, 'at least3 independent processes required'
    assert all(len(runs)==len(paths) for runs in groups.values()), 'missing failed/offline/scenario sample'
    return groups

def quantile(values,p):
    values=sorted(values)
    return values[math.ceil(len(values)*p)-1]

def summary(runs):
    out={'independent_runs': len(runs), 'samples': sum(r['samples'] for r in runs)}
    for metric in ('update','view'):
        pooled=[v for row in runs for v in row[f'{metric}_ns']]
        p99s=[quantile(row[f'{metric}_ns'],.99) for row in runs]
        out[metric+'_ns']={'median':quantile(pooled,.5),'p95':quantile(pooled,.95),'p99':quantile(pooled,.99),'worst':max(pooled),'independent_run_p99_min':min(p99s),'independent_run_p99_max':max(p99s),'independent_run_p99_stdev':statistics.stdev(p99s)}
    for key in ('allocated_bytes','allocations','heap_after_bytes','heap_peak_sampled_bytes','gc_cycles','gc_pause_ns','rss_before_bytes','rss_peak_sampled_bytes','useful_frames','retired_frames','cancellations','pending_high_water','render_requests','render_commands','prepared_view_total_bytes','prepared_view_max_bytes','helper_processes','hello_reads','preview_reads','wire_writes','wire_bytes','helper_rss_peak_bytes','combined_rss_peak_upper_bound_bytes'):
        if all(key in row for row in runs):
            values=[row[key] for row in runs]
            out[key]={'min':min(values),'median':statistics.median(values),'max':max(values)}
    if all('counts' in row for row in runs):
        out['counts']={key:[row['counts'].get(key,0) for row in runs] for key in sorted(set().union(*(row['counts'] for row in runs)))}
    for key in ('render_work_ns','render_result_wait_ns','frame_install_update_ns','render_admission_ns','preview_roundtrip_ns'):
        values=[v for row in runs for v in row.get(key,[])]
        if values: out[key]={'samples':len(values),'median':quantile(values,.5),'p95':quantile(values,.95),'p99':quantile(values,.99),'worst':max(values)}
    return out

baseline=records(a.baseline)
if not a.preview_comparison:
    required={'short-100x32','mixed8k-search-220x70','plain24k-100x32','history80-220x70','rapid-resize','same-effective-width','badge-control-search-220x70','burst-control-search-220x70','table-cap-search-220x70'}
    assert set(baseline)==required, 'required foreground matrix incomplete'
if a.preview_comparison:
    assert set(baseline)=={'preview-disabled'}
    baseline={'preview-demand':baseline['preview-disabled']}
if a.write_budgets:
    assert not a.after, 'set baseline budgets before collecting after samples'
    budgets={}
    for scenario,runs in baseline.items():
        budgets[scenario]={}
        for metric in ('update','view'):
            budgets[scenario][metric]={}
            for label,pct,allowance in (('p95',.95,500000),('p99',.99,1000000)):
                worst=max(quantile(row[f'{metric}_ns'],pct) for row in runs)
                budgets[scenario][metric][label]=math.ceil((worst*1.15+allowance)/100000)*100000
    a.budgets.write_text(json.dumps({'schema':1,'boundary':'synthetic input -> Model.Update; separate View','rationale':'worst independent baseline run percentile +15% +0.5ms p95/1ms p99 scheduler allowance, rounded upward0.1ms; shared Linux executor only','baseline_logs':{str(path):hashlib.sha256(path.read_bytes()).hexdigest() for path in a.baseline},'budgets_ns':budgets},indent=2)+'\n')
report={'schema':1,'fixture_version':3,'boundary':'synthetic input -> Model.Update; separate View; no terminal or physical input','uncertainty':'Three independent Go processes, warm caches; raw samples include scheduler/GC tails. Per-run p99 range and standard deviation reported; no production confidence interval inferred from correlated synthetic events.','baseline':{key:summary(value) for key,value in baseline.items()},'after':{},'checks':[]}
if a.after:
    after=records(a.after)
    if a.preview_comparison:
        assert set(after)=={'preview-enabled'}
        after={'preview-demand':after['preview-enabled']}
    assert set(after)==set(baseline), 'before/after scenario mismatch'
    budgets=json.loads(a.budgets.read_text())['budgets_ns']
    for scenario,runs in after.items():
        out=report['after'][scenario]=summary(runs)
        for metric in ('update','view'):
            for percentile in ('p95','p99'):
                value=out[metric+'_ns'][percentile]
                bound=budgets[scenario][metric][percentile]
                report['checks'].append({'scenario':scenario,'metric':metric+'_'+percentile,'value_ns':value,'budget_ns':bound,'pass':value<=bound})
        if scenario in ('badge-control-search-220x70','burst-control-search-220x70','same-effective-width'):
            report['checks'].append({'scenario':scenario,'metric':'no_render_command','pass':all(row['render_commands']==0 and 'plain' in row['counts'] and 'rich' in row['counts'] for row in runs)})
            for counter in ('body','assembly','install','search'):
                report['checks'].append({'scenario':scenario,'metric':counter+'_zero','pass':all(row['counts'][counter]==0 for row in runs)})
        if scenario == 'table-cap-search-220x70':
            report['checks'].append({'scenario':scenario,'metric':'unchanged_cap_body_not_reconverted','pass':all(row['counts']['body']<=row['render_commands'] for row in runs)})
        if scenario not in ('badge-control-search-220x70','burst-control-search-220x70','same-effective-width'):
            report['checks'].append({'scenario':scenario,'metric':'useful_progress','pass':all(row['useful_frames']>0 for row in runs)})
report['pass']=all(row['pass'] for row in report['checks'])
a.output.write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps({'pass':report['pass'],'checks':len(report['checks']),'output':str(a.output)},indent=2))
raise SystemExit(0 if report['pass'] else 1)
