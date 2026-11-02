#!/usr/bin/env python3
"""Real PTY acceptance and bounded resource comparison using synthetic T3 data.

Requires Python 3, pyte (test tooling only), Node 22.4+, and built client binaries.
No installed T3 server, user profile, credentials or state is accessed.
"""
import argparse
import base64
import codecs
from datetime import datetime, timezone
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import select
import shutil
import signal
import statistics
import struct
import subprocess
import tempfile
import termios
import time

try:
    import pyte
except ImportError as error:
    raise SystemExit("Terminal tests require: python3 -m pip install pyte==0.8.2") from error

ROOT = Path(__file__).resolve().parents[1]
CTRL_P = b"\x10"
CTRL_Q = b"\x11"
CTRL_S = b"\x13"
CTRL_Y = b"\x19"
SEND = b"\x1d"
ESC = b"\x1b"
ENTER = b"\r"
F2 = b"\x1bOQ"
F3 = b"\x1bOR"
DOWN = b"\x1b[B"
UP = b"\x1b[A"
HOME = b"\x1b[H"


def private_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(json.dumps(value), encoding="utf-8")
    path.chmod(0o600)


def fixture(directory, mixed=False, **options):
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    token = directory / "fixture-token"
    token.write_text("fixture-not-a-credential")
    token.chmod(0o600)
    host = directory / "host.json"
    native = directory / "native-state.json"
    log = directory / "wire-log.ndjson"
    private_json(host, {"baseUrl": "http://127.0.0.1:1", "environmentId": "fixture-env",
                       "tokenFile": str(token), "allowWrites": True,
                       "fixture": {"stateFile": str(native), "logFile": str(log), **options}})
    good = {"id": "fixture", "label": "Fixture host", "environmentId": "fixture-env",
            "node": shutil.which("node"), "adapter": str(ROOT / "tests/rust_fixture.mjs"),
            "hostConfig": str(host), "enabled": True}
    environments = [good]
    if mixed:
        failed = directory / "failed-host.json"
        private_json(failed, {"baseUrl": "http://127.0.0.1:1", "environmentId": "failed-env",
                             "tokenFile": str(token), "allowWrites": True, "fixture": {"unavailable": True}})
        environments.append({**good, "id": "failed", "label": "Offline host",
                             "environmentId": "failed-env", "hostConfig": str(failed)})
    config = directory / "environments.json"
    private_json(config, {"version": 1, "environments": environments})
    return {"config": config, "state": directory / "rust-state.json", "native": native, "log": log}


class Terminal:
    def __init__(self, binary, config, state, thread="thread-a", mode=None, columns=140, rows=38, environment=None):
        import pty
        self.master, slave = pty.openpty()
        self.columns, self.rows = columns, rows
        self.screen = pyte.Screen(columns, rows)
        self.stream = pyte.Stream(self.screen)
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.bytes = 0
        self.raw = bytearray()
        self.closed = False
        self.started = time.monotonic()
        self.first_output = None
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
        arguments = [str(binary), "--config", str(config), "--state", str(state), "--env", "fixture"]
        if thread:
            arguments.extend(["--thread", thread])
        if mode:
            arguments.extend(["--mode", mode])
        def terminal_session():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        self.process = subprocess.Popen(arguments, stdin=slave, stdout=slave, stderr=slave,
                                        cwd=ROOT, env={**os.environ, "TERM": "xterm-256color", "COLORTERM": "truecolor", **(environment or {})},
                                        preexec_fn=terminal_session, close_fds=True)
        os.close(slave)

    @property
    def lines(self):
        # Pyte uses empty continuation cells for wide characters. Reading cells
        # directly also tolerates its display() edge case after a narrow resize.
        return ["".join(self.screen.buffer[y][x].data or "" for x in range(self.columns))
                for y in range(self.rows)]

    @property
    def text(self):
        return "\n".join(self.lines)

    def pump(self, duration=0.05):
        deadline = time.monotonic() + duration
        while time.monotonic() < deadline:
            ready, _, _ = select.select([self.master], [], [], max(0, min(0.02, deadline - time.monotonic())))
            if not ready:
                continue
            try:
                data = os.read(self.master, 1024 * 1024)
            except OSError as error:
                if error.errno == errno.EIO:
                    return
                raise
            if not data:
                return
            if self.first_output is None:
                self.first_output = time.monotonic()
            self.bytes += len(data)
            self.raw.extend(data)
            self.stream.feed(self.decoder.decode(data))
            # Answer standard terminal capability queries for the Go baseline.
            for query, response in [
                (b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[?u", b"\x1b[?0u"),
                (b"\x1b[>0c", b"\x1b[>0;276;0c"), (b"\x1b[>c", b"\x1b[>0;276;0c"),
                (b"\x1b[c", b"\x1b[?1;2c"), (b"\x1b[0c", b"\x1b[?1;2c"),
                (b"\x1b]10;?", b"\x1b]10;rgb:dddd/dddd/dddd\x1b\\"),
                (b"\x1b]11;?", b"\x1b]11;rgb:1111/1111/1111\x1b\\"),
            ]:
                if query in data:
                    os.write(self.master, response)

    def wait(self, text, timeout=8):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.pump()
            if text.casefold() in self.text.casefold():
                return
            if self.process.poll() is not None:
                raise AssertionError(f"Client exited {self.process.returncode} waiting for {text!r}\n{self.text}")
        raise AssertionError(f"Timed out waiting for {text!r}\n{self.text}")

    def send(self, data, settle=0.12):
        if isinstance(data, str):
            data = data.encode()
        os.write(self.master, data)
        self.pump(settle)

    def paste(self, text):
        self.send(b"\x1b[200~" + text.encode() + b"\x1b[201~")

    def click(self, x, y):
        self.send(f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m")

    def resize(self, columns, rows):
        self.columns, self.rows = columns, rows
        self.screen.resize(lines=rows, columns=columns)
        fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
        os.kill(self.process.pid, signal.SIGWINCH)
        self.pump(0.2)

    def command(self, query):
        self.send(CTRL_P)
        self.paste(query)
        self.send(ENTER)

    def choose(self, label):
        self.wait(label)
        self.wait("↑↓ choose")
        lines = self.lines
        footer = next((y for y, line in enumerate(lines) if "↑↓ choose" in line), None)
        assert footer is not None, "No rendered menu footer"
        marker = lines[footer].index("↑↓ choose")
        left = lines[footer].rfind("│", 0, marker)
        right = lines[footer].find("│", marker)
        top = next((y for y in range(footer - 1, -1, -1) if len(lines[y]) > left and lines[y][left] == "┌"), 0)
        for row in range(top + 1, footer):
            line = lines[row]
            # Limit clicks to the current modal, excluding identical labels in
            # the workspace beneath it and title text such as "Next step".
            column = line.find(label, left + 1, right)
            if column >= 0:
                self.click(column + 1, row + 1)
                return
        raise AssertionError(f"Choice disappeared: {label}")

    def save_screen(self, destination):
        destination.with_suffix(".txt").write_text(self.text, encoding="utf-8")
        try:
            from PIL import Image, ImageDraw, ImageFont
            font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 14)
            image = Image.new("RGB", (self.columns * 9 + 24, self.rows * 18 + 24), "#181825")
            draw = ImageDraw.Draw(image)
            for index, line in enumerate(self.lines):
                draw.text((12, 12 + index * 18), line, font=font, fill="#cdd6f4")
            image.save(destination.with_suffix(".png"))
        except (ImportError, OSError):
            pass

    def close(self):
        if self.closed:
            return self.process.returncode
        if self.process.poll() is None:
            self.send(CTRL_Q, 0.2)
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                self.process.wait(timeout=3)
        self.pump(0.02)
        os.close(self.master)
        self.closed = True
        return self.process.returncode


def process_sample(pid):
    root = Path(f"/proc/{pid}")
    try:
        fields = (root / "stat").read_text().rsplit(")", 1)[1].split()
        rss = next(int(line.split()[1]) for line in (root / "status").read_text().splitlines() if line.startswith("VmRSS:"))
        cpu = (int(fields[11]) + int(fields[12])) / os.sysconf("SC_CLK_TCK")
        return {"rssKiB": rss, "cpuSeconds": cpu}
    except (OSError, StopIteration, ValueError):
        return None


def process_tree(pid):
    # Some managed Linux kernels omit /proc/PID/task/PID/children. The parent
    # table needs no command arguments, environment or unrelated process data.
    listing = subprocess.run(["ps", "-eo", "pid=,ppid="], capture_output=True, text=True, check=True)
    children = {}
    for line in listing.stdout.splitlines():
        child, parent = map(int, line.split())
        children.setdefault(parent, []).append(child)
    result, queue = {}, [pid]
    while queue:
        current = queue.pop()
        sample = process_sample(current)
        if sample:
            result[current] = sample
            queue.extend(children.get(current, []))
    return result


def measure(binary, directory, label, iterations=3):
    records = []
    for iteration in range(iterations):
        f = fixture(directory / f"{label}-{iteration}")
        terminal = Terminal(binary, f["config"], f["state"])
        try:
            terminal.wait("Ready for review", 12)
            first_transcript_ms = (time.monotonic() - terminal.started) * 1000
            terminal.wait("Streaming update 3", 12)
            ready_ms = (time.monotonic() - terminal.started) * 1000
            terminal.pump(0.5)
            before = process_tree(terminal.process.pid)
            byte_start = terminal.bytes
            started = time.monotonic()
            terminal.pump(1.5)
            elapsed = time.monotonic() - started
            after = process_tree(terminal.process.pid)
            assert terminal.process.pid in before and terminal.process.pid in after
            root = terminal.process.pid
            common = before.keys() & after.keys()
            records.append({"firstTranscriptMs": round(first_transcript_ms, 2),
                            "readyIncludingThreeStreamUpdatesMs": round(ready_ms, 2),
                            "rootRssKiB": after[root]["rssKiB"],
                            "treeRssKiB": sum(v["rssKiB"] for v in after.values()),
                            "liveProcesses": len(after), "idleSeconds": round(elapsed, 3),
                            "idleRootCpuMs": round((after[root]["cpuSeconds"] - before[root]["cpuSeconds"]) * 1000, 2),
                            "idleTreeCpuMs": round(sum(after[p]["cpuSeconds"] - before[p]["cpuSeconds"] for p in common) * 1000, 2),
                            "idleOutputBytes": terminal.bytes - byte_start,
                            "treeStableDuringSample": before.keys() == after.keys()})
        finally:
            terminal.close()
    return {"label": label, "binaryBytes": binary.stat().st_size,
            "binarySha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "samples": records,
            "median": {key: statistics.median(row[key] for row in records)
                       for key in records[0] if isinstance(records[0][key], (int, float)) and not isinstance(records[0][key], bool)}}


def cli_checks(binary, directory):
    f = fixture(directory / "cli", mixed=True, stream=False)
    def command(*args, code=0):
        result = subprocess.run([str(binary), *args, "--config", str(f["config"])], cwd=ROOT,
                                capture_output=True, text=True, timeout=15)
        assert result.returncode == code, (args, result.returncode, result.stderr)
        return json.loads(result.stdout)
    page = command("threads", "--env", "fixture", "--json-envelope")
    assert any(t["id"] == "thread-child" for t in page["page"]["threads"])
    doctors = command("doctor", "--json", code=1)
    assert {d["profile"]: d["ok"] for d in doctors} == {"fixture": True, "failed": False}
    assert command("status", "--env", "fixture", "--thread", "thread-a")["view"]["metadata"]["live"]
    assert command("activity", "--env", "fixture", "--thread", "thread-a")["activity"]["items"]
    detail = command("inspect", "--env", "fixture", "--thread", "thread-a", "--item", "thread-a-command")
    assert detail
    return ["read-only CLI lists inclusive children", "one failed environment does not hide healthy doctor result",
            "live status waits for synchronization", "bounded activity and source-item inspection"]


def wait_file(terminal, path, predicate, timeout=8):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        terminal.pump()
        try:
            data = json.loads(path.read_text())
            if predicate(data):
                return data
        except (OSError, json.JSONDecodeError):
            pass
    raise AssertionError(f"Expected durable state was not observed\n{terminal.text}")


def native_payloads(f):
    if not f["log"].exists():
        return []
    return [record["payload"] for line in f["log"].read_text().splitlines()
            if (record := json.loads(line)).get("native") == "orchestration.dispatchCommand"]


def native_calls(f, name):
    if not f["log"].exists():
        return []
    return [record["payload"] for line in f["log"].read_text().splitlines()
            if (record := json.loads(line)).get("native") == name]


def native_reads_settled(terminal, f, timeout=5):
    """Let initial synchronization's independent metadata refresh finish."""
    deadline = time.monotonic() + timeout
    previous = f["log"].read_text() if f["log"].exists() else ""
    unchanged_since = time.monotonic()
    while time.monotonic() < deadline:
        terminal.pump()
        current = f["log"].read_text() if f["log"].exists() else ""
        if current != previous:
            previous, unchanged_since = current, time.monotonic()
        elif time.monotonic() - unchanged_since >= 0.3:
            return
    raise AssertionError("Synthetic native reads did not become quiet")


def recovery_checks(binary, directory):
    f = fixture(directory / "recovery", dropAckOnce=True, stream=False)
    message = "Recover this exact operation without creating another."
    terminal = Terminal(binary, f["config"], f["state"])
    try:
        terminal.wait("Ready for review")
        terminal.paste(message)
        terminal.send(SEND)
        pending = wait_file(terminal, f["state"], lambda v: bool(v.get("pending")))
        original = pending["pending"]["fixture-env"]
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1 and v["dropped"])
        terminal.wait("unresolved")
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    resumed = Terminal(binary, f["config"], f["state"])
    try:
        resumed.wait("Ready for review")
        assert json.loads(f["state"].read_text())["pending"]["fixture-env"] == original
        before = len(native_payloads(f))
        resumed.command("recover")
        resumed.wait("Saved actions")
        resumed.send(ENTER)
        resumed.choose("Reconcile original action")
        wait_file(resumed, f["state"], lambda v: not v.get("pending"))
        assert json.loads(f["native"].read_text())["effects"] == 1
        payloads = native_payloads(f)
        assert len(payloads) == before + 1
        assert payloads[0] == payloads[-1]
        assert payloads[-1]["commandId"] == original["id"]
        assert payloads[-1]["text"] == message
        assert resumed.close() == 0
    finally:
        if resumed.process.poll() is None:
            resumed.close()
    return ["lost native acknowledgement retains exact pending operation", "fresh-process recovery never auto-sends",
            "explicit reconciliation uses original ID and identical payload with one native effect"]


def stop_check(binary, directory):
    f = fixture(directory / "stop", stream=False)
    terminal = Terminal(binary, f["config"], f["state"])
    try:
        terminal.wait("Ready for review")
        terminal.send(CTRL_P)
        terminal.send(CTRL_S)
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        payload = native_payloads(f)[-1]
        assert payload["type"] == "run.interrupt" and payload["runId"] == "run-thread-a" and payload["holdQueue"] is True
        wait_file(terminal, f["state"], lambda v: not v.get("pendingStops"))
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    return ["global Stop works while command palette is open and holds the native queue"]


def request_checks(binary, directory):
    for thread, question in [("thread-approval", False), ("thread-question", True)]:
        f = fixture(directory / thread, stream=False)
        terminal = Terminal(binary, f["config"], f["state"], thread=thread)
        try:
            terminal.wait("1 requests")
            terminal.send(CTRL_Y)
            if question:
                terminal.choose("Continue")
                terminal.choose("Next")
                terminal.choose("Submit answers")
            else:
                terminal.choose("Approve once")
            wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
            wait_file(terminal, f["state"], lambda v: not v.get("pending"))
            payload = native_payloads(f)[-1]
            assert payload["type"] == "runtime-request.respond"
            assert payload["requestId"] == ("request-question" if question else "request-approval")
            if question:
                assert payload["answers"] == {"choice": "Continue"}
            else:
                assert payload["decision"] == "accept"
            assert terminal.close() == 0
        finally:
            if terminal.process.poll() is None:
                terminal.close()
    return ["native approval displays only offered decisions and submits exact request", "native question options, review and answer submission"]


def launch_check(binary, directory):
    for mode, message in [("agentbox", "Use the native isolated workspace.\nPreserve this exact initial message."),
                          ("developer", "")]:
        f = fixture(directory / f"first-launch-{mode}", stream=False)
        terminal = Terminal(binary, f["config"], f["state"], thread="", mode=mode)
        title = f"First task in empty project ({mode})"
        try:
            terminal.wait("Agents" if mode == "agentbox" else "Conversations")
            terminal.send("n")
            terminal.choose("Empty project")
            terminal.choose("Title:")
            terminal.paste(title)
            terminal.send(ENTER)
            if message:
                terminal.choose("Message:")
                terminal.paste(message)
                terminal.send(SEND)
            terminal.choose("Create conversation")
            native = wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
            wait_file(terminal, f["state"], lambda v: not v.get("pending"))
            launches = native_calls(f, "orchestration.launchThread")
            assert len(launches) == 1
            launch = launches[0]
            assert launch["projectId"] == "project-empty" and launch["title"] == title
            if message:
                assert launch["initialMessage"]["text"] == message and launch["workspaceStrategy"]["type"] == "worktree"
            else:
                assert not launch.get("initialMessage") and launch["workspaceStrategy"]["type"] == "root"
            created = native["projections"][launch["threadId"]]["thread"]
            assert created["projectId"] == "project-empty"
            assert bool(created["activeRunId"]) == bool(message)
            assert terminal.close() == 0
        finally:
            if terminal.process.poll() is None:
                terminal.close()
    return ["both modes create a first thread in an empty native project",
            "AgentBox launch preserves exact title/message and native isolated-worktree strategy",
            "Developer empty filing creates an idle native thread without inventing an initial run"]


def model_check(binary, directory):
    f = fixture(directory / "model", stream=False)
    terminal = Terminal(binary, f["config"], f["state"], thread="thread-review")
    try:
        terminal.wait("Ready for review")
        terminal.command("model")
        terminal.choose("Fixture fast")
        terminal.choose("Reasoning:")
        terminal.choose("High")
        terminal.choose("Speed:")
        terminal.choose("Fast")
        terminal.choose("Apply reviewed selection")
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        wait_file(terminal, f["state"], lambda v: not v.get("pending"))
        payload = native_payloads(f)[-1]
        assert payload["type"] == "thread.model-selection.set"
        assert payload["modelSelection"]["model"] == "fixture-fast"
        assert {o["id"]: o["value"] for o in payload["modelSelection"]["options"]} == {"reasoningEffort": "high", "serviceTier": "fast"}
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    return ["model, reasoning and speed controls use reported catalog options and explicit guarded Apply"]


def navigation_check(binary, directory):
    f = fixture(directory / "navigation", stream=False)
    terminal = Terminal(binary, f["config"], f["state"])
    draft = "Keep this draft addressed to the original active conversation."
    try:
        terminal.wait("Live ·")
        terminal.wait("Fixture host: 7")
        terminal.paste(draft)
        native_reads_settled(terminal, f)
        before_filter = f["log"].read_text()
        terminal.send(b"\x0f/")  # Ctrl+O selects navigator; / filters loaded titles.
        terminal.paste("needle")
        terminal.wait("Hidden child needle")
        assert f["log"].read_text() == before_filter, "Loaded-title filtering performed a native read"
        terminal.send(ENTER)
        subscriptions = len(native_calls(f, "orchestration.subscribeThread"))
        terminal.send(" ")
        terminal.wait("Preview · Hidden child needle")
        assert len(native_calls(f, "orchestration.subscribeThread")) == subscriptions
        terminal.send(ESC)
        terminal.send(ESC)  # Clear the local filter without changing attachment.
        terminal.send(b"\x06")
        terminal.wait("Search native conversations")
        terminal.paste("server-only-marker")
        terminal.send(ENTER)
        terminal.wait("Alternative design")
        calls = native_calls(f, "orchestration.searchThreads")
        assert len(calls) == 1 and calls[0]["query"] == "server-only-marker"
        assert len(native_calls(f, "orchestration.subscribeThread")) == subscriptions
        assert "Active implementation" in terminal.text and draft in terminal.text
        terminal.send(SEND)
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        wait_file(terminal, f["state"], lambda v: not v.get("pending"))
        payload = native_payloads(f)[-1]
        assert payload["threadId"] == "thread-a" and payload["text"] == draft
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    return ["local title filtering finds a collapsed loaded child with zero native reads",
            "read-only child preview does not subscribe or retarget the active conversation",
            "explicit native message search preserves message-only results and the original draft/send target"]


def history_check(binary, directory):
    f = fixture(directory / "history", stream=False)
    terminal = Terminal(binary, f["config"], f["state"])
    draft = "Retain this draft across bounded history and Latest."
    try:
        terminal.wait("Live ·")
        terminal.paste(draft)
        terminal.command("Earlier messages")
        terminal.wait("Older fixture history page.")
        terminal.send(SEND)
        terminal.wait("Return to Latest")
        assert not native_payloads(f), "Historical view admitted a fresh mutation"
        terminal.command("Latest live conversation")
        terminal.wait("Ready for review")
        terminal.wait("Live ·")
        terminal.send(SEND)
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        wait_file(terminal, f["state"], lambda v: not v.get("pending"))
        assert native_payloads(f)[-1]["text"] == draft
        assert len(native_calls(f, "orchestration.subscribeThread")) == 2
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    return ["Earlier reads a bounded historical page and blocks fresh sends",
            "Latest restores verified live detail and preserves the exact draft"]


def override_check(binary, directory):
    f = fixture(directory / "override", stream=False)
    terminal = Terminal(binary, f["config"], f["state"], thread="thread-review")
    try:
        terminal.wait("Live ·")
        terminal.command("One-message model override")
        terminal.choose("Fixture fast")
        terminal.choose("Apply reviewed selection")
        state = wait_file(terminal, f["state"], lambda v: bool(v.get("modelOverrides")))
        staged = state["modelOverrides"]["fixture-env/thread-review"]
        assert staged["selection"]["model"] == "fixture-fast" and not native_payloads(f)
        terminal.paste("Send only this message with the staged model.")
        terminal.send(SEND)
        native = wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        wait_file(terminal, f["state"], lambda v: not v.get("pending") and not v.get("modelOverrides"))
        payload = native_payloads(f)[-1]
        assert payload["type"] == "message.dispatch" and payload["modelSelection"]["model"] == "fixture-fast"
        assert native["projections"]["thread-review"]["thread"]["modelSelection"]["model"] == "fixture-model"
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    return ["next-message model override is staged durably without a native mutation",
            "acknowledgement consumes only that override and leaves the thread model unchanged"]


def disconnect_check(binary, directory):
    f = fixture(directory / "disconnect", disconnectAfterMs=1200)
    terminal = Terminal(binary, f["config"], f["state"])
    draft = "Host failure must preserve this unsent exact message."
    try:
        terminal.wait("Live ·")
        terminal.paste(draft)
        terminal.wait("Disconnected")
        terminal.send(SEND)
        terminal.pump(0.2)
        assert not native_payloads(f), "Disconnected host admitted a new native mutation"
        host = f["config"].parent / "host.json"
        settings = json.loads(host.read_text())
        settings["fixture"].pop("disconnectAfterMs")
        private_json(host, settings)
        terminal.command("Reconnect")
        terminal.wait("Live ·")
        terminal.wait(draft)
        assert not native_payloads(f), "Reconnect replayed a mutation"
        terminal.send(SEND)
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        wait_file(terminal, f["state"], lambda v: not v.get("pending"))
        assert native_payloads(f)[-1]["text"] == draft
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    return ["host disconnect preserves the draft and disables new native writes",
            "explicit reconnect restores the subscription without replaying mutations"]


def editor_check(binary, directory):
    f = fixture(directory / "editor", stream=True)
    edited = "Draft from the external editor.\nExact second line: café 日本語 🙂"
    editor = f["config"].parent / "fixture-editor.py"
    editor.write_text("#!/usr/bin/env python3\nimport pathlib,sys\npathlib.Path(sys.argv[1]).write_text(" + repr(edited) + ", encoding='utf-8')\n")
    editor.chmod(0o700)
    terminal = Terminal(binary, f["config"], f["state"], environment={"VISUAL": str(editor), "EDITOR": str(editor)})
    try:
        terminal.wait("Live ·")
        terminal.paste("Initial editable draft")
        terminal.send(b"\x05")
        terminal.wait("Draft from the external editor.")
        assert not native_payloads(f)
        terminal.send(SEND)
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        wait_file(terminal, f["state"], lambda v: not v.get("pending"))
        assert native_payloads(f)[-1]["text"] == edited
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    return ["external editor suspends and resumes the real terminal, preserving exact source and text"]


def form_recovery_check(binary, directory):
    f = fixture(directory / "form-recovery", stream=False)
    form_key = json.dumps(["fixture", "fixture-env", "project-empty"], separators=(",", ":"))
    title = "Unsubmitted first task survives an abrupt terminal exit"
    message = "Autosaved initial message.\nExact second line stays unsubmitted."
    terminal = Terminal(binary, f["config"], f["state"], thread="", mode="agentbox")
    try:
        terminal.wait("Agents")
        terminal.send("n")
        terminal.choose("Empty project")
        terminal.choose("Title:")
        terminal.paste(title)
        wait_file(terminal, f["state"], lambda v: v.get("rustLaunchForms", {}).get(form_key, {}).get("title") == title)
        terminal.process.kill()
        terminal.process.wait(timeout=3)
        assert not native_calls(f, "orchestration.launchThread")
    finally:
        terminal.close()
    resumed = Terminal(binary, f["config"], f["state"], thread="", mode="agentbox")
    try:
        resumed.wait("Agents")
        resumed.send("n")
        resumed.choose("Empty project")
        resumed.wait(title)
        resumed.choose("Message:")
        resumed.paste(message)
        wait_file(resumed, f["state"], lambda v: v.get("rustLaunchForms", {}).get(form_key, {}).get("message") == message)
        assert not native_calls(f, "orchestration.launchThread")
        assert resumed.close() == 0
        saved = json.loads(f["state"].read_text())["rustLaunchForms"][form_key]
        assert saved["title"] == title and saved["message"] == message
    finally:
        resumed.close()
    return ["unsubmitted launch title is autosaved before SIGKILL and restored on the same project",
            "open multiline launch form survives exit without dispatching a native launch"]


def workspace_check(binary, directory):
    f = fixture(directory / "workspace-tools", stream=False)
    terminal = Terminal(binary, f["config"], f["state"], mode="agentbox")
    try:
        terminal.wait("Live ·")
        terminal.wait("Fixture host: 7")
        native_reads_settled(terminal, f)
        before_find = f["log"].read_text()
        terminal.command("Find in loaded messages")
        terminal.wait("Find in loaded messages")
        terminal.paste("Unicode")
        terminal.send(ENTER)
        terminal.wait("Matches in loaded messages")
        terminal.send(ENTER)
        terminal.choose("Copy this message")
        copies = re.findall(rb"\x1b\]52;c;([A-Za-z0-9+/=]*)\x07", terminal.raw)
        assert copies, "Explicit copy did not emit an OSC52 clipboard payload"
        copied = base64.b64decode(copies[-1]).decode("utf-8")
        expected = ("## Active implementation\n\nReady for review.\n\n"
                    "- Exact thread identity retained\n- Unicode: 日本語 café 🙂\n\n`cargo test` completed.")
        assert copied == expected
        assert f["log"].read_text() == before_find, "Loaded find/copy contacted native endpoints"
        terminal.command("Browse project")
        terminal.choose("Fixture host / Fixture project")
        saved = wait_file(terminal, f["state"], lambda v: bool(v.get("rustBrowserScope")))
        assert saved["rustBrowserScope"] == {"profileId": "fixture", "environmentId": "fixture-env", "projectId": "project-a"}
        assert not saved.get("creationDestination")
        terminal.command("Inbox views")
        terminal.choose("All")
        terminal.send(b"\x0f/")
        terminal.paste("Active implementation")
        terminal.send(ENTER)  # Rank the highlighted exact row, independent of attachment.
        terminal.wait("1 shown")
        terminal.command("Task priority")
        terminal.choose("Urgent")
        saved = wait_file(terminal, f["state"], lambda v: v.get("inboxPreferences", {}).get("revision") == 1)
        key = json.dumps(["fixture-env", "project-a", "thread-a"], separators=(",", ":"))
        assert saved["inboxPreferences"]["taskPriorities"][key] == 3
        terminal.command("Project order")
        terminal.choose("First")
        saved = wait_file(terminal, f["state"], lambda v: v.get("inboxPreferences", {}).get("revision") == 2)
        project_key = json.dumps(["fixture-env", "project-a", ""], separators=(",", ":"))
        assert saved["inboxPreferences"]["projectRanks"][project_key] == 0
        terminal.command("Undo local priority change")
        saved = wait_file(terminal, f["state"], lambda v: v.get("inboxPreferences", {}).get("revision") == 3)
        assert project_key not in saved["inboxPreferences"]["projectRanks"]
        assert saved["inboxPreferences"]["taskPriorities"][key] == 3
        assert not native_payloads(f) and not native_calls(f, "orchestration.launchThread")
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["loaded-message find, exact message review and OSC52 copy use the current bounded text without native reads",
            "project browsing persists exact scope independently of creation destination",
            "task priority, project order and revision-checked Undo remain local presentation changes"]


def batch_check(binary, directory):
    f = fixture(directory / "batch", stream=False)
    first, second = "First independently reviewed native task", "Second task remains a human draft until reviewed"
    original = first + "\n" + second
    draft_key = "fixture-env/\0file-many/project-empty"
    terminal = Terminal(binary, f["config"], f["state"], thread="", mode="agentbox")
    try:
        terminal.wait("Agents")
        terminal.command("File multiple tasks")
        terminal.choose("Empty project")
        terminal.wait("File multiple tasks")
        terminal.paste(original)
        terminal.send(SEND)
        terminal.wait(first)
        terminal.choose("Create conversation")
        wait_file(terminal, f["native"], lambda v: v["effects"] == 1)
        saved = wait_file(terminal, f["state"], lambda v: not v.get("pending") and bool(v.get("batchReceipts")))
        assert len(saved["batchReceipts"]) == 1
        receipt_id, receipt = next(iter(saved["batchReceipts"].items()))
        assert receipt["original"] == original and receipt["projectId"] == "project-empty"
        assert [item["state"] for item in receipt["items"]] == ["Acknowledged", "Remaining draft"]
        assert saved["drafts"][draft_key] == second
        assert terminal.close() == 0
    finally:
        terminal.close()
    resumed = Terminal(binary, f["config"], f["state"], thread="", mode="agentbox")
    try:
        resumed.wait("Agents")
        resumed.command("Task filing receipts")
        resumed.choose("1/2 acknowledged")
        resumed.choose("Review next unfiled task")
        resumed.wait(second)
        assert len(native_calls(f, "orchestration.launchThread")) == 1
        resumed.send(SEND)
        resumed.choose("Create conversation")
        wait_file(resumed, f["native"], lambda v: v["effects"] == 2)
        saved = wait_file(resumed, f["state"], lambda v: not v.get("pending") and all(item["state"] == "Acknowledged" for item in v["batchReceipts"][receipt_id]["items"]))
        assert not saved["drafts"].get(draft_key)
        assert len(saved["batchReceipts"]) == 1
        launches = native_calls(f, "orchestration.launchThread")
        assert [launch["initialMessage"]["text"] for launch in launches] == [first, second]
        assert len({launch["commandId"] for launch in launches}) == 2
        assert [item["operationId"] for item in saved["batchReceipts"][receipt_id]["items"]] == [launch["commandId"] for launch in launches]
        resumed.command("Task filing receipts")
        resumed.choose("2/2 acknowledged")
        resumed.choose("Open acknowledged task 1")
        resumed.wait(first)
        assert len(native_calls(f, "orchestration.launchThread")) == 2
        assert resumed.close() == 0
    finally:
        resumed.close()
    return ["task filing records one exact receipt per explicitly reviewed native launch",
            "partial filing survives restart with the remaining human draft intact",
            "receipt resume reviews only the next unfiled task and opens acknowledged work without refiling"]


def acceptance(binary, directory, output):
    results = cli_checks(binary, directory)
    f = fixture(directory / "workspace", mixed=True)
    draft = "Durable draft: café 日本語 🙂\nSecond line remains unsent."
    terminal = Terminal(binary, f["config"], f["state"], mode="developer")
    try:
        terminal.wait("Streaming update 3")
        terminal.wait("Offline host")
        terminal.paste(draft)
        terminal.wait("Second line remains unsent.")
        terminal.save_screen(output / "developer")
        terminal.send(F2)
        terminal.wait("Agents")
        terminal.save_screen(output / "agentbox")
        terminal.send(F3)
        terminal.resize(70, 22)
        terminal.wait("Second line remains unsent.")
        terminal.resize(20, 7)
        terminal.wait("Resize")
        terminal.resize(140, 38)
        terminal.wait("Second line remains unsent.")
        terminal.send(F3)  # Focus -> Wide, exposing the navigator again.
        terminal.click(15, 1)
        terminal.wait("Conversations")
        terminal.send(CTRL_P)
        terminal.wait("Commands")
        terminal.send(ESC)
        assert terminal.close() == 0
    finally:
        if terminal.process.poll() is None:
            terminal.close()
    saved = json.loads(f["state"].read_text())
    assert saved["drafts"]["fixture-env/thread-a"] == draft
    results.extend(["real PTY streaming view and isolated host failure", "bracketed Unicode multiline paste",
                    "Developer/AgentBox keyboard and mouse switching", "layout cycling and large/small/minimum resize",
                    "command palette opens and dismisses", "exact draft persisted on exit"])
    resumed = Terminal(binary, f["config"], f["state"], mode="developer")
    try:
        resumed.wait("Second line remains unsent.")
        resumed.wait("Live ·")
        resumed.send(SEND)
        resumed.wait("accepted", 8)
        assert resumed.close() == 0
    finally:
        if resumed.process.poll() is None:
            resumed.close()
    native = json.loads(f["native"].read_text())
    assert native["effects"] == 1
    payloads = native_payloads(f)
    assert payloads[-1]["text"] == draft
    assert payloads[-1]["createdBy"] == "user" and payloads[-1]["creationSource"] == "web"
    assert not json.loads(f["state"].read_text())["pending"]
    results.extend(["draft restored in a fresh process", "explicit send preserves exact text/native human command",
                    "acknowledged operation retires its pending record"])
    results.extend(recovery_checks(binary, directory))
    results.extend(stop_check(binary, directory))
    results.extend(request_checks(binary, directory))
    results.extend(launch_check(binary, directory))
    results.extend(model_check(binary, directory))
    results.extend(navigation_check(binary, directory))
    results.extend(history_check(binary, directory))
    results.extend(override_check(binary, directory))
    results.extend(disconnect_check(binary, directory))
    results.extend(editor_check(binary, directory))
    results.extend(form_recovery_check(binary, directory))
    results.extend(workspace_check(binary, directory))
    results.extend(batch_check(binary, directory))
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--baseline", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--iterations", type=int, default=3)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--measure-only", action="store_true")
    mode.add_argument("--acceptance-only", action="store_true")
    args = parser.parse_args()
    assert 1 <= args.iterations <= 5
    args.binary = args.binary.resolve()
    args.output = args.output.resolve()
    args.output.mkdir(parents=True, exist_ok=True)
    result = {"schemaVersion": 1, "measuredAtUtc": datetime.now(timezone.utc).isoformat(),
              "platform": platform.system(), "architecture": platform.machine(),
              "nodeVersion": subprocess.check_output([shutil.which("node"), "--version"], text=True).strip(),
              "kernelCpuTickMs": 1000 / os.sysconf("SC_CLK_TCK"),
              "fixture": "synthetic companion-v1/native-v2, local subprocess, 140x38 PTY",
              "limitations": ["Linux process RSS is not macOS physical footprint", "Node helper is included in tree totals",
                              "summed RSS may count shared pages more than once", "idle CPU has kernel clock-tick resolution",
                              "screen observation uses 50ms polling; processes are fresh but filesystem caches are warm",
                              "ready time includes three intentional 100ms fixture stream updates",
                              "same-host synthetic measurements do not establish SSH or provider latency"]}
    with tempfile.TemporaryDirectory(prefix="t3oxide-rust-terminal-") as temporary:
        directory = Path(temporary)
        if not args.measure_only:
            result["acceptance"] = acceptance(args.binary, directory, args.output)
        if not args.acceptance_only and platform.system() == "Linux":
            result["rust"] = measure(args.binary, directory, "rust", args.iterations)
            if args.baseline:
                result["goBaseline"] = measure(args.baseline.resolve(), directory, "go", args.iterations)
        elif not args.acceptance_only:
            result["metricsSkipped"] = "Resource sampling requires Linux /proc; terminal acceptance still ran."
    path = args.output / "terminal-evidence.json"
    path.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
