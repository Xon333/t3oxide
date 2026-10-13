# Optional resource measurements

Measurements are optional and are not an installation requirement. Nothing here installs tools, changes settings, stresses hardware, schedules work or uploads results.

From this repository while the app is open:

```sh
python3 scripts/measure.py --label t3oxide-active --seconds 120
```

The script finds `t3oxide` and includes its process descendants (for example its selected SSH process), on this machine only. For your already-identified Safari content process, or a selected host adapter, supply the actual PID:

```sh
python3 scripts/measure.py --label safari-reference --seconds 120 --pid 12345
```

Replace `12345`; repeat `--pid` for additional explicitly attributed root processes. Do not sum a shared browser GPU process into two clients and call the totals exclusive. Run the collector on the actual host to measure the remote adapter; a client capture does not include remote host memory.

Each finite run writes a private directory under ignored `metrics/` containing `metadata.json`, `samples.jsonl`, and `summary.json`. When sharing results, include those three files and state what you did (idle, streaming, switching threads, hidden), which environment and which build. No benchmark campaign or prescribed lengthy matrix is required.

Recorded: RSS, interval CPU-time deltas, process count, macOS physical footprint and wakeup counters where accessible, Linux PSS/swap where accessible, and before/after system memory-pressure/swap/battery information. Missing readings are null. PID identity changes are excluded from interval calculations; root identity changes are reported. No command arguments, prompts, file contents, secrets or host usernames are collected.

These are resource observations, not an energy benchmark. `ps` CPU-time resolution limits very short intervals; battery percentages over two minutes cannot establish battery-life improvement. RSS may double-count shared pages; physical footprint, PSS and global pressure are separate measurements, not interchangeable totals. Keep related apps/workload reasonably comparable and report any differences rather than repeatedly calibrating machines.
