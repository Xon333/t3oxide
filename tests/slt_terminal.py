#!/usr/bin/env python3
"""Focused SLT runtime checks through PTY bytes and the real adapter fixture.

No installed T3 service, credentials, launcher or Rex configuration is accessed.
Run with uv run --with pyte==0.8.2 python tests/slt_terminal.py --binary target/debug/t3oxide.
"""
import argparse
import base64
import hashlib
import json
import os
import select
from pathlib import Path
import shutil
import sys
import tempfile
import time

from rust_terminal import ENTER, ESC, Terminal, fixture, native_payloads, private_json, wait_file

CTRL_ENTER = b"\x1b[13;5:1u\x1b[13;5:3u"
COPY_DRAFT = b"\x1b[99;7:1u\x1b[99;7:3u"  # Candidate Ctrl+Alt+C; synthetic forwarding only.
F2, F3, F4, F6 = b"\x1bOQ", b"\x1bOR", b"\x1bOS", b"\x1b[17~"
PREVIOUS_PANE = b"\x1b[17;2~"
TAB, BACKTAB, UP, DOWN, RIGHT, END = b"\t", b"\x1b[Z", b"\x1b[A", b"\x1b[B", b"\x1b[C", b"\x1b[F"
PTY_ENV = {"NO_COLOR": ""}  # Fixture process only; exercise actual focus colours.
PHYSICAL_PASTE_DRAFT = (
    "Review the release scope with the original request preserved. Keep the current source "
    "and the next action visible while this sentence wraps inside the actual editor pane.\n"
    "Unicode: café 日本語.\n"
    "Long_token_abcdefghijklmnopqrstuvwxyz_0123456789_abcdefghijklmnopqrstuvwxyz_0123456789"
)


def targets(terminal, label, choice=False):
    """Map rendered text to PTY cells, including wide/combining prefixes."""
    terminal.wait(label)
    points = []
    for row in range(terminal.rows):
        symbols = [terminal.screen.buffer[row][x].data or "" for x in range(terminal.columns)]
        line = "".join(symbols)
        columns = [x for x, symbol in enumerate(symbols) for _ in symbol]
        offset = line.find(label)
        while offset >= 0:
            left, right = line.rfind("│", 0, offset) + 1, line.find("│", offset)
            part = line[left:right if right >= 0 else len(line)]
            # Exact menu rows exclude titles (e.g. Next step), details and the
            # underlying workspace. Lean SLT lists do not bracket each choice.
            if not choice or part.strip(" ▸✓[]") == label:
                points.append((columns[offset] + 1, row + 1))
            offset = line.find(label, offset + len(label))
    assert points, f"No rendered target {label!r}\n{terminal.text}"
    return points


def click(terminal, label, last=False):
    points = targets(terminal, label)
    terminal.click(*(points[-1] if last else points[0]))


def choose(terminal, label):
    terminal.click(*targets(terminal, label, choice=True)[-1])


def dialog_text(terminal, title):
    x, y = targets(terminal, title)[-1]
    cells = terminal.screen.buffer
    left = max(c for c in range(x - 1) if cells[y - 1][c].data == "│")
    right = next(c for c in range(x, terminal.columns) if cells[y - 1][c].data == "│")
    bottom = next(r for r in range(y, terminal.rows) if cells[r][left].data == "╰")
    return "\n".join("".join(cells[r][c].data or "" for c in range(left + 1, right))
                     for r in range(y - 1, bottom))


def dialog_action(terminal, title, label):
    """Wait for the complete modal action row, excluding titles and notices."""
    terminal.wait(title)
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        terminal.pump()
        try:
            x, y = targets(terminal, title)[-1]
            cells = terminal.screen.buffer
            left = max(c for c in range(x - 1) if cells[y - 1][c].data == "│")
            right = next(c for c in range(x, terminal.columns) if cells[y - 1][c].data == "│")
            bottom = next(r for r in range(y, terminal.rows) if cells[r][left].data == "╰")
        except (ValueError, StopIteration):
            continue  # A title may arrive before the rest of the terminal frame.
        rows = [r + 1 for r in range(y, bottom)
                if "Close" in "".join(cells[r][c].data or "" for c in range(left + 1, right))
                and label in "".join(cells[r][c].data or "" for c in range(left + 1, right))]
        if rows:
            points = [(px, py) for px, py in targets(terminal, label)
                      if py in rows and left + 1 < px <= right]
            assert len(points) == 1, f"Ambiguous modal action {label!r}: {points}\n{terminal.text}"
            terminal.click(*points[0])
            return
    raise AssertionError(f"No complete {title!r} action row for {label!r}\n{terminal.text}")


def focus_prompt(terminal):
    x, y = targets(terminal, "╭─Prompt")[-1]
    terminal.click(x + 1, y + 1)  # Textarea body, not its non-interactive border.


def draft_is(terminal, paths, text, thread="thread-a"):
    return wait_file(terminal, paths["state"], lambda value:
                     value.get("drafts", {}).get(f"fixture-env/{thread}") == text)


def editor_paint_visibility(binary, directory):
    """A saved draft alone cannot prove the grown editor painted its first rows."""
    assert len(PHYSICAL_PASTE_DRAFT.encode()) == 283
    assert hashlib.sha256(PHYSICAL_PASTE_DRAFT.encode()).hexdigest() == "11c3a0522c981bec97d78a649c7f43bea3409a94ce5cd83d02c62feeb9463f10"
    paths = fixture(directory / "editor-paint", stream=False)
    terminal = Terminal(binary, paths["config"], paths["state"], environment=PTY_ENV)
    try:
        terminal.wait("Ready for review")
        focus_prompt(terminal)
        terminal.paste(PHYSICAL_PASTE_DRAFT)
        for geometry in [None, (86, 31), (70, 31), (70, 22), (140, 38)]:
            if geometry is not None:
                terminal.resize(*geometry)
            draft_is(terminal, paths, PHYSICAL_PASTE_DRAFT)
            terminal.pump(0.25)  # Settle the paste-growth/reflow layout without moving the caret.
            x, y = targets(terminal, "╭─Prompt")[-1]
            cells = terminal.screen.buffer
            right = next(c for c in range(x, terminal.columns) if cells[y - 1][c].data == "╮")
            bottom = next(r for r in range(y, terminal.rows) if cells[r][x - 1].data == "╰")
            painted = "\n".join("".join(cells[r][c].data or "" for c in range(x, right))
                                for r in range(y, bottom))
            # These viewports fit the entire 283-byte draft. Inspect only the
            # actual composer body, excluding the transcript and durable state.
            for fragment in ["Review the release scope", "Unicode: café 日本語.", "Long_token_"]:
                assert fragment in painted, (
                    f"Composer lost painted {fragment!r} after "
                    f"{'paste growth' if geometry is None else 'resize'} at "
                    f"{terminal.columns}x{terminal.rows}; exact draft is saved\n{painted}"
                )
            assert not native_payloads(paths), "paste/growth/resize cannot send"
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["exact 283-byte physical paste paints opening, Unicode and token after settled editor growth and width/height restoration"]


def whole_prompt_copy(binary, directory):
    paths = fixture(directory / "whole-prompt-copy", stream=False, persistInitialState=True)
    # Existing private recovery input must survive browsing/copying without replay.
    pending = {"id": "copy-preserved-operation", "environmentId": "fixture-env",
               "threadId": "thread-review", "op": "send", "message": "Previously uncertain send"}
    private_json(paths["state"], {"drafts": {}, "pending": {"fixture-env": pending}, "pendingStops": {}})
    terminal = Terminal(binary, paths["config"], paths["state"], environment=PTY_ENV)
    first = "\n \tRésumé 🙂 日本語 e\u0301 👩\u200d💻\n\nfirst tail  \n\n"
    second = "  Second café 日本語 👩\u200d💻\nsecond tail \t \n\n"

    def bookmark_after_paste(text, thread, title):
        terminal.pump(0.2)
        before = draft_is(terminal, paths, text, thread)
        native = json.loads(paths["native"].read_text())
        # Ordinary controller commands persist the loaded reading position.
        # Establish it separately so copy's full-state comparison has a baseline.
        terminal.send(b"\x1bOP")  # F1 Help, without invoking clipboard operations.
        terminal.wait("Input and focus")
        terminal.send(ESC)
        terminal.pump(0.2)
        terminal.wait(f"Inspect {title}")
        saved = draft_is(terminal, paths, text, thread)
        identity = json.dumps(["fixture", "fixture-env", thread], separators=(",", ":"))
        assert identity in saved["rustReadingPositions"]
        assert saved["drafts"] == before["drafts"]
        assert saved["pending"] == before["pending"] == {"fixture-env": pending}
        assert json.loads(paths["native"].read_text()) == native and not native_payloads(paths)

    def copy_result(action, expected):
        terminal.pump(0.2)
        before = json.loads(paths["state"].read_text())
        native = json.loads(paths["native"].read_text())
        assert before["pending"] == {"fixture-env": pending}
        assert native["effects"] == 0 and not native_payloads(paths)
        start = len(terminal.raw)
        action()
        terminal.pump(0.25)
        after = json.loads(paths["state"].read_text())
        native_after = json.loads(paths["native"].read_text())
        diagnostics = paths["state"].parent / "copy-diagnostics"
        for name, value in [("state-before", before), ("state-after", after),
                            ("native-before", native), ("native-after", native_after)]:
            private_json(diagnostics / f"{name}.json", value)
        changed = {key: {"before": before.get(key), "after": after.get(key)}
                   for key in sorted(before.keys() | after.keys()) if before.get(key) != after.get(key)}
        private_json(diagnostics / "state-delta.json", changed)
        payloads = [chunk.split(b"\x1b", 1)[0].split(b"\x07", 1)[0]
                    for chunk in bytes(terminal.raw[start:]).split(b"\x1b]52;c;")[1:]]
        copied = [base64.b64decode(payload, validate=True) for payload in payloads]
        assert copied == ([] if expected is None else [expected.encode()]), (
            f"Expected exact whole-prompt OSC52 {expected!r}, got {copied!r}\n{terminal.text}"
        )
        assert terminal.process.poll() is None
        assert after == before, f"copy cannot change durable state: {changed!r}"
        assert native_after == native and not native_payloads(paths), "copy cannot dispatch or replay"

    try:
        terminal.wait("Ready for review")
        focus_prompt(terminal)
        terminal.paste(first)  # No selection or terminal copy mode.
        draft_is(terminal, paths, first)
        bookmark_after_paste(first, "thread-a", "Active implementation")
        copy_result(lambda: terminal.send(COPY_DRAFT), first)

        click(terminal, "Finished refactor")
        terminal.wait("Inspect Finished refactor")
        focus_prompt(terminal)
        terminal.paste(second)
        saved = draft_is(terminal, paths, second, "thread-review")
        assert {(tab["environmentId"], tab["threadId"]) for tab in saved["sltUi"]["tabs"]} == {
            ("fixture-env", "thread-a"), ("fixture-env", "thread-review")}
        assert saved["drafts"]["fixture-env/thread-a"] == first
        bookmark_after_paste(second, "thread-review", "Finished refactor")
        copy_result(lambda: terminal.send(COPY_DRAFT), second)

        # Select the actual internal tab row, not the same-titled Navigator row.
        tabs = [(x, y) for x, y in targets(terminal, "Active implementation")
                if "Finished refactor" in terminal.lines[y - 1]]
        assert len(tabs) == 1, f"Expected both source titles on one internal tab row\n{terminal.text}"
        terminal.click(*tabs[0])
        terminal.wait("Inspect Active implementation")
        focus_prompt(terminal)
        terminal.send(F6 + END)  # Highlight a different source without opening it.
        assert any("┃" in terminal.lines[y - 1][:x] for x, y in targets(terminal, "Finished refactor")), terminal.text
        terminal.wait("Inspect Active implementation")
        copy_result(lambda: terminal.send(COPY_DRAFT), first)

        terminal.send(b"\x10")
        terminal.wait("Search commands")
        terminal.paste("copy-draft")
        terminal.wait("Copy draft")
        copy_result(lambda: terminal.send(COPY_DRAFT), None)  # Modal must guard the underlying prompt.
        assert "copy-draft" in dialog_text(terminal, "Commands"), "guarded chord cannot edit the modal query"
        def copy_command():
            label = "Copy draft  Ctrl+Alt+C"
            assert dialog_text(terminal, "Commands").count(label) == 1, terminal.text
            points = targets(terminal, label, choice=True)
            assert len(points) == 1, f"Ambiguous annotated Copy draft row: {points}\n{terminal.text}"
            terminal.click(*points[0])

        copy_result(copy_command, first)  # Explicit command fallback is intentional.
        saved = json.loads(paths["state"].read_text())
        assert saved["drafts"]["fixture-env/thread-a"] == first
        assert saved["drafts"]["fixture-env/thread-review"] == second
        assert saved["pending"] == {"fixture-env": pending}
        assert terminal.close() == 0
    except Exception:
        retained = Path(tempfile.mkdtemp(prefix="t3oxide-copy-failure-"))
        (retained / "terminal.raw").write_bytes(terminal.raw)
        (retained / "terminal.txt").write_text(terminal.text)
        terminal.close()
        shutil.copytree(paths["state"].parent, retained / "fixture")
        print(f"Copy failure fixture and state deltas retained at {retained}", file=sys.stderr)
        raise
    finally:
        terminal.close()
    assert not native_payloads(paths), "closing cannot replay the retained recovery operation"
    return ["synthetically forwarded Ctrl+Alt+C copies exact whole Unicode/whitespace drafts from the displayed internal-tab source without selection; Navigator highlight and modal guard cannot retarget it, explicit Copy draft works, and state/recovery/native effects stay unchanged"]


def ordered_sends(binary, directory):
    checks = []
    for outcome, options in [("accepted", {}), ("rejected", {"rejectSend": True}),
                             ("uncertain", {"dropAckOnce": True})]:
        paths = fixture(directory / outcome, stream=False, ackDelayMs=1200, **options)
        terminal = Terminal(binary, paths["config"], paths["state"], columns=86, rows=31, environment=PTY_ENV)
        try:
            terminal.wait("Ready for review")
            # One PTY write: preceding edits, activation Press/Release, next draft.
            terminal.send(b"first" + CTRL_ENTER + b"second", settle=0.3)
            saved = wait_file(terminal, paths["state"], lambda value:
                              value.get("drafts", {}).get("fixture-env/thread-a") == "second")
            original = saved["pending"]["fixture-env"]
            assert original["message"] == "first" and original["threadId"] == "thread-a", original
            assert original["environmentId"] == "fixture-env" and original["op"] == "send"
            assert any(row["kind"] == "attempt" and row["text"] == "first" and
                       row["environment"] == "fixture-env" and row["thread"] == "thread-a"
                       for row in saved["promptHistory"])
            if outcome == "uncertain":
                wait_file(terminal, paths["native"], lambda value: value.get("dropped"))
                terminal.wait("Unconfirmed")
                saved = json.loads(paths["state"].read_text())
                assert saved["pending"]["fixture-env"] == original
            else:
                if outcome == "rejected":
                    terminal.wait("Fixture rejected this exact send.")
                saved = wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
            assert saved["drafts"]["fixture-env/thread-a"] == "second", saved
            payloads = native_payloads(paths)
            assert len(payloads) == 1 and payloads[0]["text"] == "first", payloads
            assert payloads[0]["commandId"] == original["id"]
            effects = json.loads(paths["native"].read_text())["effects"] if paths["native"].exists() else 0
            assert effects == (0 if outcome == "rejected" else 1)
            assert terminal.close() == 0
        finally:
            terminal.close()
        resumed = Terminal(binary, paths["config"], paths["state"], columns=86, rows=31, environment=PTY_ENV)
        try:
            resumed.wait("second")
            resumed.pump(0.2)
            assert len(native_payloads(paths)) == 1, "restart never retries a mutation"
            if outcome == "uncertain":
                assert json.loads(paths["state"].read_text())["pending"]["fixture-env"] == original
            assert resumed.close() == 0
        finally:
            resumed.close()
        checks.append(f"single-batch first/send/second retains exact source/operation and next draft under delayed {outcome} result and restart")
    return checks


def commands_pointer_and_copy(binary, directory):
    paths = fixture(directory / "ordered-ui", stream=False, ackDelayMs=600)
    terminal = Terminal(binary, paths["config"], paths["state"], columns=86, rows=31, environment=PTY_ENV)
    try:
        terminal.wait("Ready for review")
        for label in ["Unread", "Running", "Pins", "Settled", "Project", "Env", "Model", "Effort", "Send"]:
            terminal.wait(label)
        click(terminal, "Env")
        terminal.wait("Environment scope")
        terminal.wait("All environments")
        terminal.wait("Fixture host")
        assert "Unknown action" not in terminal.text
        assert not native_payloads(paths), "environment browsing cannot dispatch a mutation"
        choose(terminal, "Fixture host")
        terminal.wait("Browsing Fixture host")
        terminal.send(b"\x10help\r")
        terminal.wait("Input and focus")
        saved = json.loads(paths["state"].read_text())
        assert not saved.get("drafts", {}).get("fixture-env/thread-a", ""), "palette query must not enter the prompt draft"
        assert not native_payloads(paths)
        terminal.send(ESC)
        terminal.send(b"/help\r")
        terminal.wait("Input and focus")
        terminal.send(ESC)
        saved = wait_file(terminal, paths["state"], lambda value:
                          value.get("drafts", {}).get("fixture-env/thread-a") == "/help")
        assert not saved.get("pending") and not native_payloads(paths)
        # Forwarded Control Press+Release must reach a selected focused textarea.
        focus_prompt(terminal)
        terminal.send(b"\x1b[97;5:1u\x1b[97;5:3u\x1b[99;5:1u\x1b[99;5:3u")
        assert terminal.process.poll() is None
        expected = b"\x1b]52;c;" + base64.b64encode(b"/help")
        assert expected in terminal.raw, "selected editor copy must emit its exact OSC52 text"
        terminal.send(b"\x7f")
        terminal.send(b"\x03")  # No selection: no copied text and no reader-level exit.
        assert terminal.process.poll() is None
        # Pointer activation has the same edit/submit/tail boundary as the chord.
        x, y = targets(terminal, "Send")[-1]
        pointer = f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode()
        terminal.send(b"mouse-first" + pointer + b"mouse-next", settle=0.3)
        saved = wait_file(terminal, paths["state"], lambda value:
                          value.get("drafts", {}).get("fixture-env/thread-a") == "mouse-next")
        assert saved["pending"]["fixture-env"]["message"] == "mouse-first"
        wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
        assert [row["text"] for row in native_payloads(paths)] == ["mouse-first"]
        terminal.resize(140, 38)
        terminal.wait("mouse-next")
        terminal.resize(86, 31)
        terminal.wait("Effort")
        # Exact text before modal save; tail is routed to the next prompt draft.
        click(terminal, "Commands")
        choose(terminal, "Enter command")
        terminal.wait("Explicit command entry.")
        terminal.send(b"latest\x1dTAIL", settle=0.4)
        saved = wait_file(terminal, paths["state"], lambda value:
                          value.get("drafts", {}).get("fixture-env/thread-a") == "mouse-nextTAIL")
        assert len(native_payloads(paths)) == 1
        assert "Unknown action" not in terminal.text
        click(terminal, "Commands")
        choose(terminal, "Enter command")
        terminal.wait("Explicit command entry.")
        x, y = targets(terminal, "Continue")[-1]
        pointer = f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode()
        terminal.send(b"latest" + pointer + b"POINTERTAIL", settle=0.4)
        wait_file(terminal, paths["state"], lambda value:
                  value.get("drafts", {}).get("fixture-env/thread-a") == "mouse-nextTAILPOINTERTAIL")
        assert "Unknown action" not in terminal.text and len(native_payloads(paths)) == 1
        focus_prompt(terminal)
        terminal.send(b"\x1d")  # Verified portable Ctrl+] path in the prompt itself.
        wait_file(terminal, paths["native"], lambda value: value["effects"] == 2)
        wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
        assert [row["text"] for row in native_payloads(paths)] == ["mouse-first", "mouse-nextTAILPOINTERTAIL"]
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["sidebar Env opens the supported environment scope without dispatching a mutation",
            "batched command Enter consumes without newline/send; compact controls and draft survive resize",
            "batched Ctrl+P/help/Enter settles palette focus and executes Help without changing draft",
            "focused Control select-all/copy Press+Release emits exact clipboard text; unselected Ctrl+C cannot exit",
            "batched pointer Send separates preceding edits from next draft; modal text/save/tail stays ordered",
            "portable Ctrl+] sends the exact accumulated prompt through the same native acknowledgement path"]


def run(binary, directory):
    paths = fixture(directory / "runtime", stream=False)
    terminal = Terminal(binary, paths["config"], paths["state"], environment=PTY_ENV)
    draft = "/help stays literal · café 日本語 alpha beta gamma alpha beta gamma\nsecond line  e\u0301 👩\u200d💻  "
    try:
        terminal.wait("Ready for review")
        terminal.paste(draft.split("\n")[0])
        terminal.send(ENTER)
        terminal.paste(draft.split("\n")[1])
        saved = wait_file(terminal, paths["state"], lambda value: value.get("drafts", {}).get("fixture-env/thread-a") == draft)
        assert not saved.get("pending")
        assert not native_payloads(paths)
        for columns, rows in [(86, 31), (54, 18), (140, 38)]:
            terminal.resize(columns, rows)
            draft_is(terminal, paths, draft)
            assert not native_payloads(paths), "resize cannot dispatch or normalize a draft"
        click(terminal, "Finished refactor")
        terminal.wait("Inspect Finished refactor")
        terminal.paste("independent tab draft")
        wait_file(terminal, paths["state"], lambda value: value.get("drafts", {}).get("fixture-env/thread-review") == "independent tab draft")
        click(terminal, "Active implementation")
        terminal.wait("second line")
        # Enhanced Ctrl+Enter Press + Release: exactly one native send, no newline.
        terminal.send(b"\x1b[13;5:1u\x1b[13;5:3u")
        wait_file(terminal, paths["native"], lambda value: value["effects"] == 1)
        wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
        sent = native_payloads(paths)
        assert len(sent) == 1 and sent[0]["text"] == draft, sent
        terminal.pump(0.3)
        assert json.loads(paths["native"].read_text())["effects"] == 1
        # A typed slash enters command mode; Enter opens help and preserves text.
        terminal.send("/")
        terminal.send("help")
        terminal.send(ENTER)
        terminal.wait("Input and focus")
        assert json.loads(paths["native"].read_text())["effects"] == 1
        terminal.send(ESC)
        terminal.send(F2)  # Actual human queue, not failed/prose rows.
        terminal.wait("Forms")
        terminal.wait("Question to answer")
        assert "Finished refactor" not in "\n".join(line[:28] for line in terminal.lines)
        terminal.send(F2)
        terminal.wait("Prompt")
        click(terminal, "Search")
        terminal.wait("Search conversations")
        terminal.wait("Scope")
        terminal.paste("refactor")
        click(terminal, "Continue", last=True)
        terminal.wait("Finished refactor")
        assert len(native_payloads(paths)) == 1, "search remains read-only"
        terminal.send(ESC)
        click(terminal, "Search")
        click(terminal, "Scope")
        terminal.wait("Empty project")
        terminal.send(ESC)
        assert terminal.close() == 0
    finally:
        terminal.close()
    resumed = Terminal(binary, paths["config"], paths["state"], thread="thread-review", environment=PTY_ENV)
    try:
        resumed.wait("independent tab draft")
        saved = json.loads(paths["state"].read_text())
        assert len(saved["sltUi"]["tabs"]) == 2
        assert saved["drafts"]["fixture-env/thread-a"] == "/help"
        assert json.loads(paths["native"].read_text())["effects"] == 1
        assert resumed.close() == 0
    finally:
        resumed.close()
    return [
        "SLT wake-aware runtime attaches real adapter data and exits normally",
        "plain Enter and pasted slash/newlines preserve exact Unicode draft without dispatch",
        "narrow/wide resize preserves repeated spaces, combining marks, CJK and ZWJ draft bytes",
        "conversation tabs autosave independent drafts and restore them after restart",
        "Ctrl+Enter Press+Release dispatches exactly one native send with no added newline",
        "typed command Enter opens help without dispatch or newline and retains unsent text",
        "search starts with current-project text entry and Scope retains a zero-question project",
    ]


def answers(binary, directory, uncertain=False):
    paths = fixture(directory / ("answers-uncertain" if uncertain else "answers"),
                    stream=False, ackDelayMs=1200, dropAckOnce=uncertain)
    terminal = Terminal(binary, paths["config"], paths["state"], mode="b", environment=PTY_ENV)
    try:
        terminal.wait("Forms")
        click(terminal, "Question to answer")
        terminal.wait("What should happen next?")
        terminal.wait("Ready for review")
        click(terminal, "Answer")
        terminal.wait("Question 1 / 1")
        dialog_action(terminal, "Question 1 / 1", "Next")
        terminal.wait("Next step requires an answer")
        terminal.wait("Question 1 / 1")
        assert not native_payloads(paths), "invalid Next cannot submit"
        choose(terminal, "Continue")
        saved = wait_file(terminal, paths["state"], lambda value: bool(value.get("attentionDrafts")))
        original_drafts = saved["attentionDrafts"]
        assert any(record["answers"] == {"choice": "Continue"} and
                   record["source"]["threadId"] == "thread-question"
                   for record in original_drafts.values()), original_drafts
        assert not saved.get("pending")
        assert not native_payloads(paths)
        # Open/type/activate in one batch before the palette has a layout frame.
        # The retained answer source must survive that focus transition.
        terminal.send(b"\x10help\r")
        terminal.wait("Input and focus")
        assert json.loads(paths["state"].read_text())["attentionDrafts"] == original_drafts
        terminal.send(ESC)
        click(terminal, "Answer")
        terminal.wait("Question 1 / 1")
        dialog_action(terminal, "Question 1 / 1", "Next")
        terminal.wait("Review answers")
        # Review must use human wording and selected labels rather than JSON IDs.
        review = dialog_text(terminal, "Review answers")
        assert '"choice"' not in review and "What should happen next?" in review and "Continue" in review, review
        assert not native_payloads(paths)
        dialog_action(terminal, "Review answers", "Submit answers")
        pending = wait_file(terminal, paths["state"], lambda value: bool(value.get("pending")))
        operation = pending["pending"]["fixture-env"]
        assert operation["op"] == "answer" and operation["threadId"] == "thread-question"
        assert operation["requestId"] == "request-question" and operation["answers"] == {"choice": "Continue"}
        assert operation["environmentId"] == "fixture-env"
        assert any(record["source"]["revision"] == operation["revision"] for record in original_drafts.values())
        terminal.wait("Submitting")
        assert "Submitted" not in terminal.text, "success cannot precede acknowledgement"
        wait_file(terminal, paths["native"], lambda value: value["effects"] == 1)
        if uncertain:
            wait_file(terminal, paths["native"], lambda value: value.get("dropped"))
            terminal.wait("Unconfirmed")
            terminal.wait("Recovery")
            assert "Submitted" not in terminal.text, "a native effect with a lost ACK is not delivery confirmation"
            saved = json.loads(paths["state"].read_text())
            assert saved["pending"]["fixture-env"] == operation
        else:
            saved = wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
            terminal.wait("Submitted")
        sent = native_payloads(paths)
        assert len(sent) == 1 and sent[0]["type"] == "runtime-request.respond"
        assert sent[0]["requestId"] == "request-question" and sent[0]["answers"] == {"choice": "Continue"}
        assert sent[0]["threadId"] == "thread-question" and sent[0]["commandId"] == operation["id"]
        assert saved["attentionDrafts"] == original_drafts, "acknowledged original answer source remains recoverable"
        click(terminal, "Answer")
        assert "Question 1 / 1" not in terminal.text, "submitted or uncertain delivery cannot reopen a duplicate answer"
        assert len(native_payloads(paths)) == 1
        assert terminal.close() == 0
    finally:
        terminal.close()
    resumed = Terminal(binary, paths["config"], paths["state"], mode="b", environment=PTY_ENV)
    try:
        resumed.wait("Permission needed")
        resumed.pump(0.2)
        saved = json.loads(paths["state"].read_text())
        assert saved["attentionDrafts"] == original_drafts
        if uncertain:
            assert saved["pending"]["fixture-env"] == operation
        assert len(native_payloads(paths)) == 1, "restart never replays an answer"
        assert resumed.close() == 0
    finally:
        resumed.close()
    if uncertain:
        return ["lost answer ACK renders Unconfirmed and Recovery, retains the exact pending source/operation and draft, and never replays on restart"]
    return ["Mode B invalid Next stays on the original form without a mutation",
            "autosaved answer survives batched early palette entry and restores on reopening",
            "human-worded review and explicit Submit acknowledge exactly one original source/operation; draft survives restart without replay"]


def focus_and_panes(binary, directory):
    paths = fixture(directory / "focus", stream=False)
    terminal = Terminal(binary, paths["config"], paths["state"], environment=PTY_ENV)
    try:
        terminal.wait("Ready for review")
        focus_prompt(terminal)
        terminal.paste("focus-draft")
        draft_is(terminal, paths, "focus-draft")
        terminal.send(TAB + BACKTAB)
        terminal.paste("+tab")
        draft_is(terminal, paths, "focus-draft+tab")
        # The next SLT focus stop is the compact composer action bar. Its
        # horizontal arrows operate controls, not the prompt cursor.
        terminal.send(TAB)
        terminal.send(RIGHT)
        x, y = targets(terminal, "Effort")[-1]
        assert terminal.screen.buffer[y - 1][x - 1].underscore, "arrow selection must be visible"
        terminal.send(ENTER)
        terminal.wait("Reasoning")
        effort = dialog_text(terminal, "Reasoning")
        assert all(label in effort for label in ["Native default", "Medium", "High"]), effort
        assert "Model · current conversation" not in terminal.text, "Effort opens its native descriptor directly"
        assert not native_payloads(paths), "opening Effort cannot select a native model"
        terminal.send(ESC)
        terminal.send(F3)
        assert "Unread" not in terminal.text, terminal.text
        terminal.send(F3)
        terminal.wait("Unread")
        terminal.send(F4)
        terminal.wait("Copy ID")
        terminal.send(F4)
        assert "Copy ID" not in terminal.text, terminal.text
        focus_prompt(terminal)
        terminal.send(END)
        terminal.send(PREVIOUS_PANE)  # Composer -> transcript.
        terminal.send(b"ignored")
        draft_is(terminal, paths, "focus-draft+tab")
        terminal.send(F6)  # Transcript -> composer.
        terminal.paste("+pane")
        draft_is(terminal, paths, "focus-draft+tab+pane")
        terminal.send(F6 + END + UP + DOWN + ENTER)  # Navigator arrows, then open last source.
        terminal.wait("Inspect Finished refactor")
        focus_prompt(terminal)
        terminal.paste("keyboard-source-draft")
        draft_is(terminal, paths, "keyboard-source-draft", "thread-review")
        # Both keyboard navigation and pane moves leave the first source intact.
        saved = json.loads(paths["state"].read_text())
        assert saved["drafts"]["fixture-env/thread-a"] == "focus-draft+tab+pane"
        assert not native_payloads(paths)
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["Tab/Shift+Tab restore textarea focus; action-bar arrows open the separate Effort control",
            "F3 sidebar/F4 context and F6/Shift+F6 pane navigation preserve exact source drafts",
            "navigator arrows/End/Enter open the intended conversation without mutations"]


def library_without_questions(binary, directory):
    paths = fixture(directory / "library", stream=False, persistInitialState=True)
    # Seed only the private native fixture, then restart through the production
    # entry point. No test-only App handler or fabricated UI state is invoked.
    seed = Terminal(binary, paths["config"], paths["state"], thread="thread-review", environment=PTY_ENV)
    try:
        seed.wait("Ready for review")
        assert seed.close() == 0
    finally:
        seed.close()
    native = json.loads(paths["native"].read_text())
    for projection in native["projections"].values():
        projection["runtimeRequests"] = []
        projection["thread"]["pendingRuntimeRequest"] = None
        projection["turnItems"] = [row for row in projection["turnItems"]
                                   if row["type"] not in ["user_input_request", "approval_request"]]
    source = native["projections"]["thread-review"]
    source["thread"]["projectId"] = "project-empty"
    markdown = "## Research: wrapping evidence\n\n- Retained original source.\n\n```mermaid\nflowchart LR\nA[Source] --> B[Decision]\n```"
    for row in source["turnItems"]:
        if row["type"] == "assistant_message":
            row["text"] = markdown
    for row in source["visibleTurnItems"]:
        if row["item"]["type"] == "assistant_message":
            row["item"]["text"] = markdown
    paths["native"].write_text(json.dumps(native))
    terminal = Terminal(binary, paths["config"], paths["state"], thread="thread-review", environment=PTY_ENV)
    try:
        terminal.wait("wrapping evidence")
        terminal.send(F2)
        terminal.wait("Forms 0")
        terminal.wait("Library")
        click(terminal, "Empty project", last=True)
        click(terminal, "Library")
        terminal.wait("wrapping evidence")
        terminal.wait("Retained original source.")
        terminal.wait("Source")
        terminal.wait("Decision")
        assert "Answer" not in terminal.text, "read-only Library content cannot become a form"
        click(terminal, "Open in A")
        terminal.wait("Prompt")
        focus_prompt(terminal)
        terminal.paste("report-source-draft")
        draft_is(terminal, paths, "report-source-draft", "thread-review")
        assert not native_payloads(paths), "Library/project/source browsing is read-only"
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["zero-question project exposes independent Library report/diagram and opens the original source in A without dispatch"]


def compact_model_interactions(binary, directory):
    """One result group, with an occupied recovery lane and four clean Apply cases."""
    evidence = Path(tempfile.mkdtemp(prefix="t3oxide-model-evidence-"))
    private_json(directory / "compact-model-evidence.json", {"directory": str(evidence)})
    title, escape = "Model · current conversation", b"\x1b[27u"
    draft = "Tail 日本語 café e\u0301 👩\u200d💻  \nold tail  \n"
    other = "Other source 日本語  \n"
    original = {"instanceId": "fixture-provider", "model": "fixture-model",
                "options": [{"id": "reasoningEffort", "value": "medium"}]}
    model_only = {"instanceId": "fixture-provider", "model": "fixture-fast"}
    staged = {**model_only, "options": [{"id": "reasoningEffort", "value": "high"},
                                       {"id": "serviceTier", "value": "fast"}]}
    pending = {"id": "model-preserved-operation", "environmentId": "fixture-env",
               "threadId": "thread-review", "op": "send", "message": "Existing uncertain intent"}
    methods = [("tab", TAB), ("backtab", BACKTAB), ("escape", escape), ("pointer", None)]
    completed = []
    private_json(evidence / "group.json", {"status": "RUNNING", "binary": str(binary),
                 "binarySha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                 "cases": ["guarded"] + [name for name, _ in methods]})
    for case, dismissal in [("guarded", None)] + methods:
        guarded = case == "guarded"
        paths = fixture(evidence / case / "fixture", stream=False,
                        persistInitialState=True, ackDelayMs=1200)
        private_json(paths["state"], {"drafts": {"fixture-env/thread-a": draft,
                     "fixture-env/thread-review": other},
                     "pending": {"fixture-env": pending} if guarded else {}, "pendingStops": {}})
        case_dir = evidence / case
        terminal, child, actions = None, 0, []

        def snapshot(label):
            journal = paths["state"].parent / "operations-v1"
            value = {"state": json.loads(paths["state"].read_text()),
                     "native": json.loads(paths["native"].read_text()),
                     "nativeSha256": hashlib.sha256(paths["native"].read_bytes()).hexdigest(),
                     "nativeMode": oct(paths["native"].stat().st_mode & 0o777),
                     "journal": {"exists": journal.exists(), "entries": {
                         str(path.relative_to(journal)): {
                             "mode": oct(path.stat().st_mode & 0o777),
                             "sha256": hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None}
                         for path in sorted([journal, *journal.rglob("*")]) if path.exists()}},
                     "payloads": native_payloads(paths)}
            private_json(case_dir / f"{len(actions):03d}-{label}.json", value)
            return value

        def retain(label):
            prefix = case_dir / f"child-{child}-{len(actions):03d}-{label}"
            prefix.with_suffix(".raw").write_bytes(terminal.raw)
            prefix.with_suffix(".txt").write_text(terminal.text)
            private_json(prefix.with_suffix(".json"), {"child": child,
                         "pid": terminal.process.pid, "argv": terminal.process.args,
                         "config": str(paths["config"]), "state": str(paths["state"]),
                         "geometry": [terminal.columns, terminal.rows],
                         "exit": terminal.process.poll(), "actions": actions})

        def send(label, data):
            actions.append({"label": label, "child": child,
                            "seconds": time.monotonic(), "bytesHex": data.hex()})
            private_json(case_dir / "actions.json", actions)
            terminal.send(data)
            retain(label)

        def pointer(label, text):
            points = targets(terminal, text)
            x, y = points[-1]
            send(label, f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode())

        def configuration():
            terminal.wait(title)
            terminal.pump(0.2)
            assert "Filter…" not in terminal.text, terminal.text
            return dialog_text(terminal, title)

        def unchanged(label):
            actual = snapshot(label)
            assert actual["state"] == expected_state, f"{case}/{label}: durable state changed"
            for key in ["native", "nativeSha256", "nativeMode", "journal", "payloads"]:
                assert actual[key] == baseline[key], f"{case}/{label}: {key} changed"
            return actual

        def caret():
            x, y = targets(terminal, "╭─Prompt")[-1]
            cells = terminal.screen.buffer
            right = next(c for c in range(x, terminal.columns) if cells[y - 1][c].data == "╮")
            bottom = next(r for r in range(y, terminal.rows) if cells[r][x - 1].data == "╰")
            points = [(c, r) for r in range(y, bottom) for c in range(x, right)
                      if cells[r][c].data == "▎"]
            assert len(points) == 1, f"Expected actual prompt caret\n{terminal.text}"
            return points[0]

        def prove_caret(label):
            assert caret() == original_caret, f"{case}/{label}: caret moved"
            marker = "<caret>"
            send(label + "-insert", marker.encode())
            draft_is(terminal, paths, draft.replace("old tail  \n", "old tail<caret>  \n"))
            send(label + "-restore", b"\x7f" * len(marker))
            draft_is(terminal, paths, draft)
            assert caret() == original_caret

        def open_model():
            pointer("open-model", "Model")
            configuration()

        def open_local():
            pointer("expand-local-model", "Fixture fast")
            terminal.wait("✓ Fixture fast")
            terminal.wait("Filter…")

        def dismiss(label, key, tail):
            if key is None:
                x, y = targets(terminal, "Apply ↵")[-1]
                data = f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode()
            else:
                data = key
            # Esc is CSI-u, so this SAME write cannot become Alt+Enter.
            send(label, data + (ENTER if tail else b""))
            text = configuration()
            assert "· Fixture fast ▾" in text, text
            unchanged(label)

        try:
            terminal = Terminal(binary, paths["config"], paths["state"],
                                columns=70, rows=22, environment=PTY_ENV)
            terminal.wait("cargo test completed.")
            focus_prompt(terminal)
            send("interior-caret", UP * 3 + b"\x1b[H" + DOWN + END + b"\x1b[D" * 2)
            send("bookmark-help", b"\x1bOP")
            terminal.wait("Input and focus")
            send("bookmark-close", escape)
            terminal.pump(0.2)
            original_caret = caret()
            draft_is(terminal, paths, draft)
            baseline = snapshot("baseline")
            expected_state = baseline["state"]
            assert expected_state["pending"] == ({"fixture-env": pending} if guarded else {})
            assert expected_state["drafts"]["fixture-env/thread-review"] == other
            assert baseline["native"]["projections"]["thread-a"]["thread"]["modelSelection"] == original
            assert baseline["native"]["effects"] == 0 and not baseline["payloads"]
            open_model()
            send("accept-original-model", b" " + ENTER)
            configuration()
            unchanged("same-value-model")
            send("stage-fast-model", b" " + DOWN + ENTER)
            assert "· Fixture fast ▾" in configuration(), terminal.text
            unchanged("staged-fast-model")
            if guarded:
                for name, key in methods:
                    open_local()
                    dismiss("local-" + name + "-queued-enter", key, True)
                for name, key in methods:
                    pointer("expand-reasoning-" + name, "Reasoning")
                    terminal.wait("Native default")
                    dismiss("option-" + name + "-queued-enter", key, True)
                for label, value in [("Reasoning", "high"), ("Speed", "fast")]:
                    pointer("expand-" + label.lower(), label)
                    terminal.wait("Native default")
                    send("stage-" + label.lower(), END + ENTER)
                    text = configuration()
                    assert any(label in line and value in line for line in text.splitlines()), text
                    unchanged("staged-" + label.lower())
                    pointer("reopen-" + label.lower(), label)
                    terminal.wait("✓ " + ("High" if label == "Reasoning" else "Fast"))
                    send("same-value-" + label.lower(), ENTER)
                    configuration()
                    unchanged("same-value-" + label.lower())
                for keyboard, name in [(True, "Keyboard model fixture"), (False, "Pointer model fixture")]:
                    if keyboard:
                        # Speed -> Apply -> Save. Space acts on Save; Enter applies.
                        send("focused-save-space", TAB + TAB + b" ")
                    else:
                        pointer("pointer-save", "Save favorite")
                    terminal.wait("Save model favorite")
                    unchanged("favorite-editor-local")
                    send("favorite-name", b"\x1b[200~" + name.encode() + b"\x1b[201~")
                    pointer("save-favorite-continue", "Continue")
                    configuration()
                    saved = wait_file(terminal, paths["state"], lambda state:
                                      any(row.get("name") == name for row in
                                          state.get("modelPresets", {}).get("fixture-env", [])))
                    expected = json.loads(json.dumps(expected_state))
                    expected.setdefault("modelPresets", {}).setdefault("fixture-env", []).append(
                        {"name": name, "selection": staged})
                    assert saved == expected, "Save may add only the reviewed local favorite"
                    expected_state = expected
                    unchanged("favorite-saved")
                # Return ownership to the real Model field before cycling to ×.
                open_local()
                send("retain-staged-model-before-close", ENTER)
                configuration()
                unchanged("before-close-focus")
                send("focus-close", BACKTAB)
                terminal.pump(0.2)  # Preserve the actual × keyboard owner across idle settling.
                targets(terminal, "×")
                retain("focused-close")
                send("space-close", b" ")
                assert title not in terminal.text and "Filter…" not in terminal.text, terminal.text
                prove_caret("close-focus-caret")
                unchanged("close-focus")
                open_model()
                send("outer-escape", escape)
                assert title not in terminal.text, terminal.text
                prove_caret("outer-escape-caret")
                unchanged("outer-escape")
            else:
                open_local()
                dismiss("local-queued-enter", dismissal, True)
                open_local()
                dismiss("local-no-tail", dismissal, False)
                # Input-free settling must expire dismissal ownership. No Apply retry.
                terminal.pump(0.25)
                unchanged("idle-before-apply")
                send("deliberate-apply-once", ENTER)
                saved = wait_file(terminal, paths["state"], lambda state: bool(state.get("pending")))
                assert set(saved["pending"]) == {"fixture-env"}
                operation = saved["pending"]["fixture-env"]
                assert operation["id"] and operation["op"] == "select-model"
                assert operation["environmentId"] == "fixture-env" and operation["threadId"] == "thread-a"
                assert operation["modelSelection"] == model_only
                assert operation["expectedModelSelection"] == original
                private_json(case_dir / "observed-pending.json", saved)
                wait_file(terminal, paths["native"], lambda value: value["effects"] == 1)
                snapshot("exact-pending")
                saved = wait_file(terminal, paths["state"], lambda state: not state.get("pending"))
                payloads = native_payloads(paths)
                assert payloads == [{"type": "thread.model-selection.set", "commandId": operation["id"],
                                     "threadId": "thread-a", "modelSelection": model_only}], payloads
                # expectedModelSelection is a preflight guard, not a native payload field.
                accepted = snapshot("acknowledged")
                assert accepted["native"]["effects"] == 1
                assert accepted["native"]["projections"]["thread-a"]["thread"]["modelSelection"] == model_only
                assert saved["drafts"] == expected_state["drafts"]
                assert saved["pendingStops"] == expected_state["pendingStops"]
                assert saved["sltUi"]["tabs"] == expected_state["sltUi"]["tabs"]
                journal = paths["state"].parent / "operations-v1"
                records = [(path, json.loads(path.read_text())) for path in journal.glob("*.json")]
                assert len(records) == 1, records
                path, receipt = records[0]
                assert receipt["payload"] == payloads[0] and receipt["tag"] == "orchestration.dispatchCommand"
                ack = wait_file(terminal, Path(str(path) + ".ack"), lambda value:
                                value.get("payloadHash") == receipt["payloadHash"])
                assert all(ack[key] == receipt[key] for key in ["key", "scope", "fingerprint", "payloadHash"])
                native_receipt = accepted["native"]["receipts"][operation["id"]]
                assert ack["result"] == native_receipt["result"]
                assert ack["result"]["sequence"] == accepted["native"]["sequence"]
                assert accepted["native"]["sequence"] == baseline["native"]["sequence"] + 1
                assert native_receipt["fingerprint"] == receipt["payloadHash"]
                expected_native = json.loads(json.dumps(baseline["native"]))
                expected_native["effects"] = 1
                expected_native["sequence"] += 1
                expected_native["receipts"][operation["id"]] = native_receipt
                thread = expected_native["projections"]["thread-a"]["thread"]
                thread["modelSelection"] = model_only
                thread["updatedAt"] = accepted["native"]["projections"]["thread-a"]["thread"]["updatedAt"]
                assert accepted["native"] == expected_native, "Apply may change only its original native source/receipt"
                # Close the review if still present before inspecting the textarea.
                if title in terminal.text:
                    send("close-after-apply", escape)
                prove_caret("applied-caret")
            # A real geometry round trip must retain draft/caret, not just durable bytes.
            terminal.resize(140, 38)
            actions.append({"label": "resize-wide", "child": child, "geometry": [140, 38]})
            draft_is(terminal, paths, draft)
            retain("resize-wide")
            terminal.resize(70, 22)
            actions.append({"label": "resize-restore", "child": child, "geometry": [70, 22]})
            prove_caret("resize-caret")
            if guarded:
                unchanged("resized")
            before_restart = snapshot("before-restart")
            assert terminal.close() == 0
            retain("closed")
            terminal = None
            child = 1
            terminal = Terminal(binary, paths["config"], paths["state"],
                                columns=70, rows=22, environment=PTY_ENV)
            terminal.wait("cargo test completed.")
            draft_is(terminal, paths, draft)
            terminal.pump(0.2)
            restarted = snapshot("restarted")
            assert restarted == before_restart, "restart cannot change state/native/journal or replay"
            assert len(restarted["payloads"]) == (0 if guarded else 1)
            assert terminal.close() == 0
            retain("restart-closed")
            completed.append(case)
            private_json(case_dir / "result.json", {"status": "PASS", "case": case,
                         "children": [0, 1], "subcases": [row["label"] for row in actions]})
        except BaseException as error:
            private_json(case_dir / "result.json", {"status": "FAIL", "case": case,
                         "error": repr(error), "actions": actions})
            private_json(evidence / "result.json", {"status": "FAIL", "completed": completed,
                         "failedCase": case, "error": repr(error)})
            if terminal is not None:
                try:
                    retain("failure")
                    if paths["state"].exists() and paths["native"].exists():
                        snapshot("failure")
                except BaseException as capture_error:
                    private_json(case_dir / "failure-capture.json", {"error": repr(capture_error),
                                 "originalError": repr(error)})
            print(f"Compact Model failure evidence retained at {evidence}", file=sys.stderr)
            raise
        finally:
            if terminal is not None:
                terminal.close()
                retain("final")
    assert completed == ["guarded", "tab", "backtab", "escape", "pointer"]
    private_json(evidence / "result.json", {"status": "PASS", "completed": completed,
                 "scope": "one compact Model group; five fixtures, ten production children; synthetic PTY"})
    return ["compact Model preserves guarded drafts/recovery while staging, dismissing queued input and saving local favorites; each clean dismissal case admits exactly one later deliberate Apply and never replays"]


def navigator_lane_pointer(binary, directory):
    """The native Unread tab must activate even when its cursor already is zero."""
    draft = "\n Lane café 日本語 e\u0301 👩\u200d💻  \n"
    for empty in [False, True]:
        paths = fixture(directory / ("lanes-empty" if empty else "lanes-populated"),
                        stream=False, persistInitialState=True)
        if empty:
            # Seed only this private native fixture, as in the Library check.
            seed = Terminal(binary, paths["config"], paths["state"], environment=PTY_ENV)
            try:
                seed.wait("Ready for review")
                assert seed.close() == 0
            finally:
                seed.close()
            native = json.loads(paths["native"].read_text())
            for projection in native["projections"].values():
                projection["thread"]["settledOverride"] = None
            private_json(paths["native"], native)
        pending = {"id": "lane-preserved-operation", "environmentId": "fixture-env",
                   "threadId": "thread-review", "op": "send", "message": "Existing uncertain intent"}
        private_json(paths["state"], {"drafts": {}, "pending": {"fixture-env": pending}, "pendingStops": {}})
        terminal = Terminal(binary, paths["config"], paths["state"], environment=PTY_ENV)

        def wait_rows(present, absent):
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                terminal.pump()
                try:
                    # Quiet lane tabs contain internal │ separators. The tools
                    # row has the same outer Navigator border and no separators.
                    pane = dialog_text(terminal, "Project")
                except (ValueError, StopIteration):
                    continue
                if all(label in pane for label in present) and all(label not in pane for label in absent):
                    return
            raise AssertionError(f"Navigator did not paint {present!r} without {absent!r}\n{terminal.text}")

        try:
            terminal.wait("Ready for review")
            focus_prompt(terminal)
            terminal.paste(draft)
            before = draft_is(terminal, paths, draft)
            native = json.loads(paths["native"].read_text())
            assert native["effects"] == 0 and not native_payloads(paths)
            for _ in range(2):
                click(terminal, "Settled")
                wait_rows(("No conversations",) if empty else ("Settled work",),
                          ("Alternative design", "Finished refactor", "Active implementation"))
                click(terminal, "Unread")  # Real pointer activation, without keyboard substitution.
                # Open/Unread contains unreviewed completed sources; running
                # Active implementation belongs to the separate Run lane.
                present = ("Alternative design", "Finished refactor") + (("Settled work",) if empty else ())
                absent = ("No conversations", "Active implementation") + (() if empty else ("Settled work",))
                wait_rows(present, absent)
                terminal.wait("Inspect Active implementation")
                saved = draft_is(terminal, paths, draft)
                assert saved["drafts"] == before["drafts"]
                assert saved["pending"] == before["pending"] == {"fixture-env": pending}
                assert saved["pendingStops"] == before["pendingStops"]
                assert saved["promptHistory"] == before["promptHistory"]
                assert saved["sltUi"]["tabs"] == before["sltUi"]["tabs"]
                assert json.loads(paths["native"].read_text()) == native
                assert not native_payloads(paths), "lane browsing cannot dispatch or replay the retained operation"
            assert terminal.close() == 0
        finally:
            terminal.close()
        assert json.loads(paths["state"].read_text())["pending"] == {"fixture-env": pending}
        assert not native_payloads(paths), "closing after lane browsing cannot replay"
    return ["actual Settled→Unread pointer activation restores Navigator rows from populated and empty lanes twice, preserving displayed source, Unicode draft, tabs and original pending operation without dispatch or replay"]


def context_pending_read(binary, evidence, gate_manifest):
    """Optional macOS child-only fault injection; require actual exact-file entry."""
    spec = json.loads(gate_manifest.read_text())
    dylib = (gate_manifest.parent / spec["dylib"]).resolve()
    assert sys.platform == "darwin", "the private dyld gate requires macOS"
    assert hashlib.sha256(dylib.read_bytes()).hexdigest() == spec["sha256"]
    directory = Path(tempfile.mkdtemp(prefix="context-gate-", dir=evidence))
    paths = fixture(directory / "fixture", stream=False, persistInitialState=True)
    first = "  Context café 日本語 e\u0301 👩\u200d💻\nold tail  \n"
    second = "  Other source 日本語 🙂\nsecond tail  \n"
    pending = {"id": "context-preserved-operation", "environmentId": "fixture-env",
               "threadId": "thread-review", "op": "send", "message": "Original uncertain intent"}
    private_json(paths["state"], {"drafts": {"fixture-env/thread-review": first,
                 "fixture-env/thread-fork": second}, "pending": {"fixture-env": pending}, "pendingStops": {}})
    events, terminal, pipes, released = [], None, [], False
    started = time.monotonic()
    baseline = None

    def event(kind, value):
        events.append({"seconds": time.monotonic() - started, "kind": kind, "value": value})
        private_json(directory / "timeline.json", events)

    def delta(before, after, pointer=""):
        if isinstance(before, dict) and isinstance(after, dict):
            result = []
            for key in sorted(before.keys() | after.keys()):
                path = pointer + "/" + key.replace("~", "~0").replace("/", "~1")
                if key not in before or key not in after:
                    result.append({"pointer": path, "before": before.get(key), "after": after.get(key),
                                   "beforePresent": key in before, "afterPresent": key in after})
                else:
                    result.extend(delta(before[key], after[key], path))
            return result
        return [] if before == after else [{"pointer": pointer, "before": before, "after": after}]

    def snapshot(label, compare=False, check=True):
        state = json.loads(paths["state"].read_text())
        native = json.loads(paths["native"].read_text())
        journal = paths["state"].parent / "operations-v1"
        value = {"state": state, "native": native, "journal": {"exists": journal.exists(), "files": {
            str(path.relative_to(journal)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted(journal.rglob("*")) if path.is_file()}}}
        private_json(directory / f"{label}.json", value)
        if baseline is not None:
            changes = delta(baseline, value)
            private_json(directory / f"{label}-delta.json", changes)
            if compare:
                assert value == baseline, f"context read changed durable/native/journal state: {changes!r}"
        if check:
            assert state["pending"] == {"fixture-env": pending}
            assert native["effects"] == 0 and not native_payloads(paths), "context cannot dispatch or replay"
        return value

    def command(text):
        click(terminal, "Commands")
        choose(terminal, "Enter command")
        terminal.wait("Explicit command entry.")
        terminal.paste(text)
        terminal.send(b"\x1d")  # Existing explicit reviewed-command submit.
        event("command", text)

    def current_pack(title, other):
        terminal.wait("Context records")
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            terminal.pump()
            try:
                painted = dialog_text(terminal, "Context records")
            except (ValueError, StopIteration):
                continue
            assert f"## {other}" not in painted, f"stale/wrong-source recall painted\n{painted}"
            if f"## {title}" in painted and "1 / 1" in painted:
                event("pack", painted)
                return
        raise AssertionError(f"Expected exact captured {title!r} scope\n{terminal.text}")

    def source_tab(title):
        points = [(x, y) for x, y in targets(terminal, title)
                  if "Finished refactor" in terminal.lines[y - 1]
                  and "Alternative design" in terminal.lines[y - 1]]
        assert len(points) == 1, f"Expected actual internal source tab {title!r}\n{terminal.text}"
        terminal.click(*points[0])
        terminal.wait(f"Inspect {title}")
        event("source-tab", title)

    def pending_context_text():
        points = targets(terminal, "Context", choice=True)
        assert len(points) == 1, f"Expected unique exact pending Context title: {points}\n{terminal.text}"
        x, y = points[0]
        cells = terminal.screen.buffer
        left = max(c for c in range(x - 1) if cells[y - 1][c].data == "│")
        right = next(c for c in range(x, terminal.columns) if cells[y - 1][c].data == "│")
        assert "".join(cells[y - 1][c].data or "" for c in range(left + 1, right)).strip() == "Context"
        top = next(r for r in range(y - 2, -1, -1)
                   if cells[r][left].data == "╭" and cells[r][right].data == "╮")
        bottom = next(r for r in range(y, terminal.rows)
                      if cells[r][left].data == "╰" and cells[r][right].data == "╯")
        close = [(cx, cy) for cx, cy in targets(terminal, "Close", choice=True)
                 if left + 1 < cx <= right and y < cy <= bottom]
        assert len(close) == 1 and top < y - 1 < bottom, terminal.text
        return "\n".join("".join(cells[r][c].data or "" for c in range(left + 1, right))
                         for r in range(top + 1, bottom))

    def caret():
        # SLT paints its own caret glyph; the terminal's hardware cursor is hidden.
        x, y = targets(terminal, "╭─Prompt")[-1]
        cells = terminal.screen.buffer
        right = next(c for c in range(x, terminal.columns) if cells[y - 1][c].data == "╮")
        bottom = next(r for r in range(y, terminal.rows) if cells[r][x - 1].data == "╰")
        points = [(c, r) for r in range(y, bottom) for c in range(x, right)
                  if cells[r][c].data == "▎"]
        assert len(points) == 1, f"Expected focused prompt caret\n{terminal.text}"
        return points[0]

    def status(expected):
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            terminal.pump(0.02)
            ready, _, _ = select.select([status_r], [], [], 0)
            if ready:
                byte = os.read(status_r, 1)
                event("gate-status", byte.decode("ascii", errors="replace"))
                assert byte == expected.encode(), f"Expected gate {expected}, got {byte!r}"
                return
            assert terminal.process.poll() is None, f"Client exited before actual gate {expected}\n{terminal.text}"
        raise AssertionError(f"No actual gate {expected}; a pending notice is insufficient\n{terminal.text}")

    def release():
        nonlocal released
        if pipes and not released:
            released = True
            try:
                os.write(release_w, b"R")
                event("gate-release", "R")
            except BrokenPipeError:
                event("gate-release", "child already closed its release pipe")

    try:
        # Build the tiny corpus through normal capture of maintained fixture
        # messages, not invented fael events or a production test hook.
        terminal = Terminal(binary, paths["config"], paths["state"], thread="thread-review", environment=PTY_ENV)
        terminal.wait("Inspect Finished refactor")
        command("/context")
        current_pack("Finished refactor", "Alternative design")
        terminal.send(ESC)
        click(terminal, "Alternative design")
        terminal.wait("Inspect Alternative design")
        command("/context")
        current_pack("Alternative design", "Finished refactor")
        terminal.send(ESC)
        source_tab("Finished refactor")
        command("/context")
        current_pack("Finished refactor", "Alternative design")
        terminal.send(ESC)
        assert terminal.close() == 0
        (directory / "seed-terminal.raw").write_bytes(terminal.raw)
        (directory / "seed-terminal.txt").write_text(terminal.text)
        terminal = None
        context_root = Path(str(paths["state"]) + ".context")
        corpus = {str(path.relative_to(context_root)): hashlib.sha256(path.read_bytes()).hexdigest()
                  for path in sorted(context_root.rglob("*")) if path.is_file()}
        private_json(directory / "corpus-before.json", corpus)
        records = [json.loads(line) for path in (context_root / "log").glob("*.jsonl")
                   for line in path.read_text().splitlines()]
        captures = {record["t3oxide"]["source"]["threadId"]: record["t3oxide"] for record in records}
        assert len(records) == 2 and set(captures) == {"thread-review", "thread-fork"}
        for thread, capture in captures.items():
            assert capture["source"]["environmentId"] == "fixture-env"
            assert capture["source"]["projectId"] == "project-a"
            payload = (context_root / capture["sourceRef"]).read_bytes()
            assert hashlib.sha256(payload).hexdigest() == capture["sourceSha256"]
            assert json.loads(payload)["sourceThreadId"] == thread
        private_json(directory / "captured-provenance.json", captures)
        target = (context_root / captures["thread-review"]["sourceRef"]).resolve()
        info = target.stat()
        assert target.is_file() and not target.is_symlink() and info.st_uid == os.geteuid()
        status_r, status_w = os.pipe()
        release_r, release_w = os.pipe()
        arm_r, arm_w = os.pipe()
        pipes = [status_r, status_w, release_r, release_w, arm_r, arm_w]
        environment = {**PTY_ENV, "DYLD_INSERT_LIBRARIES": str(dylib), "T3OXIDE_PTY_IO_PATH": str(target),
                       "T3OXIDE_PTY_IO_DEV": str(info.st_dev), "T3OXIDE_PTY_IO_INO": str(info.st_ino),
                       "T3OXIDE_PTY_IO_STATUS_FD": str(status_w), "T3OXIDE_PTY_IO_RELEASE_FD": str(release_r),
                       "T3OXIDE_PTY_IO_ARM_FD": str(arm_r)}
        private_json(directory / "injection.json", {"manifest": str(gate_manifest), "spec": spec,
            "binarySha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "environmentOverrides": environment,
            "pass_fds": [status_w, release_r, arm_r], "targetSha256": hashlib.sha256(target.read_bytes()).hexdigest(),
            "targetDevice": info.st_dev, "targetInode": info.st_ino})
        terminal = Terminal(binary, paths["config"], paths["state"], thread="thread-review",
                            environment=environment, pass_fds=(status_w, release_r, arm_r))
        for fd in (status_w, release_r, arm_r):
            os.close(fd)
            pipes.remove(fd)
        status("L")  # Loaded/configured does not establish a pending operation.
        terminal.wait("Inspect Finished refactor")
        command("/context")
        current_pack("Finished refactor", "Alternative design")  # Ungated completion orders startup capture.
        terminal.send(ESC)
        focus_prompt(terminal)
        terminal.send(DOWN + END + b"\x1b[D" * 2)  # Interior caret in the ASCII tail, after Unicode.
        terminal.send(b"\x1bOP")  # Separate read-only Help command establishes the reading bookmark.
        terminal.wait("Input and focus")
        terminal.send(ESC)
        terminal.pump(0.2)
        original_caret = caret()
        draft_is(terminal, paths, first, "thread-review")
        baseline = snapshot("before")
        assert baseline["state"]["drafts"]["fixture-env/thread-fork"] == second
        os.write(arm_w, b"A")
        event("gate-arm", "A after ungated current-scope completion")
        command("/context")
        status("E")  # This is the pending proof; never infer entry from timing.
        assert "Reading…" in pending_context_text(), terminal.text
        snapshot("entered", compare=True)
        terminal.send(ESC)
        terminal.pump(0.2)
        assert "Reading…" not in terminal.text and "Context records" not in terminal.text
        assert caret() == original_caret
        snapshot("escaped", compare=True)
        marker = "<caret>"
        terminal.send(marker)
        draft_is(terminal, paths, first.replace("old tail  \n", f"old tail{marker}  \n"), "thread-review")
        event("independent-draft-save", "exact interior edit persisted while old context I/O is held")
        terminal.send(b"\x7f" * len(marker))
        draft_is(terminal, paths, first, "thread-review")
        assert caret() == original_caret
        snapshot("restored-while-held", compare=True)
        source_tab("Alternative design")
        draft_is(terminal, paths, second, "thread-fork")
        snapshot("other-source", compare=True)
        command("/context")  # Latest desired read captures the current source while old I/O remains held.
        assert "Reading…" in pending_context_text(), terminal.text
        snapshot("fresh-pending", compare=True)
        release()
        status("D")
        current_pack("Alternative design", "Finished refactor")
        choose(terminal, "## Alternative design")
        terminal.wait("Context record")
        record = dialog_text(terminal, "Context record")
        assert "thread-fork @" in record and captures["thread-fork"]["source"]["revision"] in record, record
        snapshot("fresh-current-scope", compare=True)
        terminal.send(ESC)
        source_tab("Finished refactor")
        draft_is(terminal, paths, first, "thread-review")
        assert caret() == original_caret
        snapshot("returned-original", compare=True)
        assert terminal.close() == 0
        snapshot("closed", compare=True)
        assert hashlib.sha256(dylib.read_bytes()).hexdigest() == spec["sha256"]
        assert (target.stat().st_dev, target.stat().st_ino) == (info.st_dev, info.st_ino)
        assert hashlib.sha256(target.read_bytes()).hexdigest() == corpus[str(target.relative_to(context_root))]
        corpus_after = {str(path.relative_to(context_root)): hashlib.sha256(path.read_bytes()).hexdigest()
                        for path in sorted(context_root.rglob("*")) if path.is_file()}
        private_json(directory / "corpus-after.json", corpus_after)
        assert corpus_after == corpus, "unchanged captured revisions must not append or replace context records"
        private_json(directory / "result.json", {"result": "PASS", "scope": "synthetic PTY with private child-only exact-file I/O fault injection",
                     "seconds": time.monotonic() - started, "events": events})
    except BaseException as error:
        # Unblock before attempting orderly product shutdown on EVERY failure.
        release()
        if terminal is not None:
            (directory / "failure-terminal.raw").write_bytes(terminal.raw)
            (directory / "failure-terminal.txt").write_text(terminal.text)
            if paths["native"].exists():
                snapshot("failure", check=False)
        private_json(directory / "result.json", {"result": "FAIL", "error": repr(error), "events": events})
        print(f"Context gate failure fixture and full state/native/journal deltas retained at {directory}", file=sys.stderr)
        raise
    finally:
        release()
        if terminal is not None:
            terminal.close()
            (directory / "terminal.raw").write_bytes(terminal.raw)
            (directory / "terminal.txt").write_text(terminal.text)
        for fd in pipes:
            os.close(fd)
    return ["actual exact-file pending context read accepts Esc and an independent exact Unicode draft/caret save, preserves full durable/native/journal state and original pending identity, then fresh internal-tab recall paints only the captured current source without stale reopen or native replay"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--context-gate", type=Path, help="Optional private macOS I/O-gate manifest; adds fault-injected context check")
    parser.add_argument("--context-gate-evidence", type=Path, help="Persistent private evidence directory required with --context-gate")
    args = parser.parse_args()
    if bool(args.context_gate) != bool(args.context_gate_evidence):
        parser.error("--context-gate and --context-gate-evidence must be supplied together")
    if args.context_gate_evidence:
        args.context_gate_evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix="t3oxide-slt-pty-") as directory:
        checks = run(args.binary.resolve(), Path(directory))
        checks.extend(editor_paint_visibility(args.binary.resolve(), Path(directory)))
        checks.extend(whole_prompt_copy(args.binary.resolve(), Path(directory)))
        checks.extend(answers(args.binary.resolve(), Path(directory)))
        checks.extend(answers(args.binary.resolve(), Path(directory), uncertain=True))
        checks.extend(ordered_sends(args.binary.resolve(), Path(directory)))
        checks.extend(commands_pointer_and_copy(args.binary.resolve(), Path(directory)))
        checks.extend(focus_and_panes(args.binary.resolve(), Path(directory)))
        checks.extend(library_without_questions(args.binary.resolve(), Path(directory)))
        checks.extend(compact_model_interactions(args.binary.resolve(), Path(directory)))
        checks.extend(navigator_lane_pointer(args.binary.resolve(), Path(directory)))
    if args.context_gate:
        checks.extend(context_pending_read(args.binary.resolve(), args.context_gate_evidence.resolve(), args.context_gate.resolve()))
    print(json.dumps({"scope": "synthetic PTY; native Rex/M4 acceptance remains separate", "checks": checks}, indent=2))


if __name__ == "__main__":
    main()
