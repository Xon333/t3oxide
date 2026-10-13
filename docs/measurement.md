# User-run resource measurements

Use the existing collector for short, optional observations—not installation acceptance or a battery-life claim. Run it yourself with the installed client already open. Do not rebuild/reinstall the app to measure it, restart T3, install collectors, or add services.

## Start from current public source

Use a fresh checkout, not a checkout or Git history from a different repository:

```sh
git clone --single-branch https://github.com/Xon333/t3oxide.git t3oxide-measurements
cd t3oxide-measurements
```

Choose a new destination if that directory exists. These commands only obtain the script; they do not alter the installed app or adapters. Use an existing Python 3 installation.

## Native client — on the client Mac

```sh
python3 scripts/measure.py --label native-active --seconds 120
```

The script selects the single running `t3oxide` process and includes its local descendants, such as its SSH channels. With multiple app instances, identify the intended PID and pass `--pid` explicitly. Shared OS services are outside this process tree. This capture does **not** include remote adapters, T3 servers, or agents.

`metadata.json` records the installed app's version/build from the running executable's on-disk `Info.plist`, without retaining the bundle path. Do not replace the app during capture. Check `installed_app_builds`; if unavailable/null, record the running app's version and build privately from its bundle information. The collector's SHA-256 identifies the script separately; repository history does not identify the installed build.

## Browser comparison — on the same client Mac

Identify the T3 content process in Activity Monitor or the browser's own process view **before** sampling. Do not select all WebKit processes or an arbitrary browser process. Reidentify it after a reload or process replacement.

```sh
printf 'Verified T3 browser-content PID: '; read -r BROWSER_PID
python3 scripts/measure.py --label browser-active --seconds 120 --pid "$BROWSER_PID"
```

This is a selected content-process comparison, **not total browser overhead**. Repeat `--pid` only for other explicitly attributed roots. Overlapping descendants are counted once within a run, but shared GPU/network processes cannot be assigned exclusively to multiple tabs. Record browser version and attribution separately in your private notes.

Use the same conversation, viewing/streaming activity and roughly comparable conditions. Note unavoidable differences rather than running a prescribed test matrix. Keep the other client inactive for the comparison where practical; do not stop server work just to measure it.

## Host adapter — separately on the host that runs it

Use the host's existing authorized terminal/SSH access and a fresh public checkout there. Identify the **on-demand t3oxide adapter PID**, not the T3 server, every Node process, or your SSH login shell. Leave the client connected so its adapter remains running.

```sh
printf 'Verified adapter PID on this host: '; read -r ADAPTER_PID
python3 scripts/measure.py --label host-adapter --seconds 120 --pid "$ADAPTER_PID"
```

Run separately for each host of interest; a client-side capture cannot see remote-host consumption. Host OS, selected adapter release, T3 version, and any machine-specific PID lookup commands belong in private notes. Do not infer the installed adapter from this checkout or start an adapter just to satisfy the collector.

## Private output and interpretation

Each run prints its directory, by default outside the checkout:

```text
~/.local/state/t3oxide/measurements/<label>-<UTC timestamp>/
  metadata.json
  samples.jsonl
  summary.json
```

The final run directory is owner-only. To use an established private storage location instead, add `--output "$PRIVATE_MEASUREMENTS"` after setting that variable. Never commit raw output; an ignored directory inside a public checkout is not the preferred storage boundary.

Keep the three files together with a short private note: actual installed build, scenario (idle/reading/streaming/switching/hidden), selected environment, process attribution, and power source. Run only the scenarios needed for your question. Hidden/minimized mode may end the on-demand adapter; that is a lifecycle observation, not an unexplained reduction in memory.

| Metric | Meaning and limits |
|---|---|
| RSS | Resident bytes for the selected processes; sums can double-count shared pages. |
| Physical footprint (macOS) | Separately reported process memory charge; not interchangeable with RSS or Linux PSS. Do not add it to RSS. |
| PSS / process swap (Linux) | Proportionally attributed resident shared pages / process swap, when readable. |
| CPU | Interval process CPU-time deltas; 100% is one logical core, not the whole machine. Short intervals are limited by `ps` time resolution. |
| Wakeups (macOS) | Available cumulative idle/interrupt wakeups and idle-wakeup rate. Counters/rates are not watts or energy use. |
| System / battery | Whole-machine memory pressure, swap and battery observations before/after; not attributable solely to the selected client. |

Missing metrics are null. Missing requested PIDs fail before capture; roots that disappear or become unverifiable during capture are flagged in samples. Do not interpret incomplete process coverage or a PID replacement as an improvement. A two-minute battery-percentage change cannot establish battery-life savings.

Raw captures, personal paths, host details and tailored instructions stay in private storage. Public discussion may contain only generic instructions and deliberately anonymized findings. The script does not collect command arguments, prompts or credentials, and uploads nothing.
