#!/usr/bin/env python3
"""Focused SLT runtime checks through PTY bytes and the real adapter fixture.

No installed T3 service, credentials, launcher or Rex configuration is accessed.
Run with uv run --with pyte==0.8.2 python tests/slt_terminal.py --binary target/debug/t3oxide.
"""
import argparse
import base64
import json
import re
from pathlib import Path
import tempfile

from rust_terminal import ENTER, ESC, Terminal, fixture, native_payloads, wait_file

CTRL_ENTER = b"\x1b[13;5:1u\x1b[13;5:3u"


def ordered_sends(binary, directory):
    checks = []
    for outcome, options in [("accepted", {}), ("rejected", {"rejectSend": True}),
                             ("uncertain", {"dropAckOnce": True})]:
        paths = fixture(directory / outcome, stream=False, ackDelayMs=1200, **options)
        terminal = Terminal(binary, paths["config"], paths["state"], columns=86, rows=31)
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
        resumed = Terminal(binary, paths["config"], paths["state"], columns=86, rows=31)
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
    terminal = Terminal(binary, paths["config"], paths["state"], columns=86, rows=31)
    try:
        terminal.wait("Ready for review")
        for label in ["Inbox", "Active", "Pins", "Settled", "Scope", "Env", "Stop"]:
            terminal.wait(label)
        click(terminal, "Env")
        terminal.wait("Environment scope")
        terminal.wait("All environments")
        terminal.wait("Fixture host")
        assert "Unknown action" not in terminal.text
        assert not native_payloads(paths), "environment browsing cannot dispatch a mutation"
        terminal.send(b"\x1b[B\r")
        terminal.wait("Browsing Fixture host")
        terminal.send(b"\x10editor-keys\r")
        terminal.wait("Editor keys")
        saved = json.loads(paths["state"].read_text())
        assert not saved.get("drafts", {}).get("fixture-env/thread-a", ""), "palette query must not enter the prompt draft"
        assert not native_payloads(paths)
        terminal.send(ESC)
        terminal.send(b"/help\r")
        terminal.wait("Commands")
        terminal.send(ESC)
        saved = wait_file(terminal, paths["state"], lambda value:
                          value.get("drafts", {}).get("fixture-env/thread-a") == "/help")
        assert not saved.get("pending") and not native_payloads(paths)
        # Forwarded Control Press+Release must reach a selected focused textarea.
        click(terminal, "Prompt")
        terminal.send(b"\x1b[97;5:1u\x1b[97;5:3u\x1b[99;5:1u\x1b[99;5:3u")
        assert terminal.process.poll() is None
        expected = b"\x1b]52;c;" + base64.b64encode(b"/help")
        assert expected in terminal.raw, "selected editor copy must emit its exact OSC52 text"
        terminal.send(b"\x7f")
        terminal.send(b"\x03")  # No selection: no copied text and no reader-level exit.
        assert terminal.process.poll() is None
        # The compact composer retains exact edit/send/tail ordering without a Send button.
        terminal.send(b"mouse-first" + CTRL_ENTER + b"mouse-next", settle=0.3)
        saved = wait_file(terminal, paths["state"], lambda value:
                          value.get("drafts", {}).get("fixture-env/thread-a") == "mouse-next")
        assert saved["pending"]["fixture-env"]["message"] == "mouse-first"
        wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
        assert [row["text"] for row in native_payloads(paths)] == ["mouse-first"]
        terminal.resize(140, 38)
        terminal.wait("mouse-next")
        terminal.resize(86, 31)
        terminal.wait("Stop")
        # Exact text before modal save; tail is routed to the next prompt draft.
        click(terminal, "Commands")
        terminal.send(b"Enter command\r")
        terminal.wait("Continue")
        terminal.send(b"latest\x1dTAIL", settle=0.4)
        saved = wait_file(terminal, paths["state"], lambda value:
                          value.get("drafts", {}).get("fixture-env/thread-a") == "mouse-nextTAIL")
        assert len(native_payloads(paths)) == 1
        assert "Unknown action" not in terminal.text
        click(terminal, "Commands")
        terminal.send(b"Enter command\r")
        terminal.wait("Continue")
        matches = [(line.index("Continue") + 1, row + 1) for row, line in enumerate(terminal.lines) if "Continue" in line]
        x, y = matches[-1]
        pointer = f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode()
        terminal.send(b"latest" + pointer + b"POINTERTAIL", settle=0.4)
        wait_file(terminal, paths["state"], lambda value:
                  value.get("drafts", {}).get("fixture-env/thread-a") == "mouse-nextTAILPOINTERTAIL")
        assert "Unknown action" not in terminal.text and len(native_payloads(paths)) == 1
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["sidebar Env opens the supported environment scope without dispatching a mutation",
            "batched command Enter consumes without newline/send; compact controls and draft survive resize",
            "batched Ctrl+P/editor-keys/Enter opens compact key reference without changing draft",
            "focused Control select-all/copy Press+Release emits exact clipboard text; unselected Ctrl+C cannot exit",
            "send chord and pointer modal Continue retain exact edit/save/tail ordering"]


def click(terminal, label, last=False):
    terminal.wait(label)
    matches = [(line.index(label) + 1, row + 1) for row, line in enumerate(terminal.lines) if label in line]
    assert matches, f"No rendered target {label!r}"
    terminal.click(*(matches[-1] if last else matches[0]))


def run(binary, directory):
    paths = fixture(directory / "runtime", stream=False)
    terminal = Terminal(binary, paths["config"], paths["state"])
    draft = "/help stays literal · café 日本語\nsecond line"
    try:
        terminal.wait("Ready for review")
        terminal.paste(draft.split("\n")[0])
        terminal.send(ENTER)
        terminal.paste("second line")
        saved = wait_file(terminal, paths["state"], lambda value: value.get("drafts", {}).get("fixture-env/thread-a") == draft)
        assert not saved.get("pending")
        assert not native_payloads(paths)
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
        terminal.wait("Commands")
        assert json.loads(paths["native"].read_text())["effects"] == 1
        terminal.send(ESC)
        terminal.send(b"\x1bOQ")  # F2: actual human queue, not failed/prose rows.
        terminal.wait("Needs you")
        terminal.wait("Question to answer")
        click(terminal, "Search")
        terminal.wait("Conversations")
        terminal.send(b"nonmatching-query")
        click(terminal, "Search T3")
        terminal.wait("Empty project")
        terminal.send(ESC)
        assert "Finished refactor" not in "\n".join(line[:28] for line in terminal.lines)
        assert terminal.close() == 0
    finally:
        terminal.close()
    resumed = Terminal(binary, paths["config"], paths["state"], thread="thread-review")
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
        "conversation tabs autosave independent drafts and restore them after restart",
        "Ctrl+Enter Press+Release dispatches exactly one native send with no added newline",
        "typed command Enter opens help without dispatch or newline and retains unsent text",
        "Mode B admits actual questions; native-search inventory retains an empty project",
    ]


def composer_and_transcript_reflow(binary, directory):
    paths = fixture(directory / "reflow", stream=False, persistInitialState=True)
    bootstrap = Terminal(binary, paths["config"], paths["state"])
    try:
        bootstrap.wait("Ready for review")
        assert bootstrap.close() == 0
    finally:
        bootstrap.close()
    native = json.loads(paths["native"].read_text())
    projection = native["projections"]["thread-a"]
    transcript = "\n\n".join(f"READ-{row:03d} retained reading position." for row in range(80)) + "\n\nTRANSCRIPT TAIL"
    for item in projection["turnItems"]:
        if item["id"] == "thread-a-answer":
            item["text"] = transcript
    for row in projection["visibleTurnItems"]:
        if row["item"]["id"] == "thread-a-answer":
            row["item"]["text"] = transcript
    paths["native"].write_text(json.dumps(native))
    terminal = Terminal(binary, paths["config"], paths["state"], columns=100, rows=44)
    copy_draft = b"\x1b[99;7:1u\x1b[99;7:3u"
    select_all = b"\x1b[97;5:1u\x1b[97;5:3u"

    def reading_rows():
        return re.findall(r"READ-\d{3}", terminal.text)

    def prompt_top():
        return next(row for row, line in enumerate(terminal.lines) if "Prompt" in line)

    draft = "\n".join(f"line-{row:02d} café e\u0301 日本語 👩\u200d💻  preserved  whitespace" for row in range(20)) + "\nLASTROW END"
    try:
        terminal.wait("TRANSCRIPT TAIL")
        initial_top = prompt_top()
        terminal.paste(draft)
        wait_file(terminal, paths["state"], lambda value: value.get("drafts", {}).get("fixture-env/thread-a") == draft)
        terminal.wait("LASTROW END")
        assert prompt_top() < initial_top - 2, "pasted wrapped rows must grow the composer"
        assert 12 <= terminal.rows - prompt_top() <= 18, "composer growth stays bounded to twelve editor rows plus chrome"
        terminal.resize(80, 31)
        terminal.wait("LASTROW END")
        terminal.resize(120, 42)
        terminal.wait("LASTROW END")
        terminal.send(b"\x1b[D")
        start = len(terminal.raw)
        terminal.send(copy_draft)
        expected_copy = b"\x1b]52;c;" + base64.b64encode(draft.encode())
        assert expected_copy in terminal.raw[start:], "whole-draft copy retains exact Unicode, whitespace and newlines"
        terminal.send("X")
        edited = draft[:-1] + "X" + draft[-1]
        wait_file(terminal, paths["state"], lambda value: value.get("drafts", {}).get("fixture-env/thread-a") == edited)
        click(terminal, "Finished refactor")
        terminal.wait("Inspect Finished refactor")
        terminal.paste("other source draft")
        wait_file(terminal, paths["state"], lambda value: value.get("drafts", {}).get("fixture-env/thread-review") == "other source draft")
        click(terminal, "Active implementation")
        terminal.wait("LASTROW ENX")
        start = len(terminal.raw)
        terminal.send(copy_draft)
        assert b"\x1b]52;c;" + base64.b64encode(edited.encode()) in terminal.raw[start:]
        terminal.send(select_all + b"\x7f")
        wait_file(terminal, paths["state"], lambda value: not value.get("drafts", {}).get("fixture-env/thread-a"))
        terminal.send("/help")
        terminal.wait("Keyboard help")
        completion_row = next(row for row, line in enumerate(terminal.lines) if "Keyboard help" in line)
        input_row = next(row for row, line in enumerate(terminal.lines) if "/help" in line and "Keyboard help" not in line)
        assert completion_row < input_row, "completion belongs above the prompt text"
        terminal.send(ESC)
        terminal.send(select_all + b"\x7f")
        wait_file(terminal, paths["state"], lambda value: not value.get("drafts", {}).get("fixture-env/thread-a"))
        click(terminal, "Latest")
        terminal.wait("TRANSCRIPT TAIL")
        click(terminal, "READ-", last=True)
        bottom = reading_rows()
        terminal.send(b"\x1b[5~")
        manual = reading_rows()
        assert manual and manual != bottom and "TRANSCRIPT TAIL" not in terminal.text, "focused PageUp scrolls the actual transcript"
        terminal.resize(86, 31)
        assert reading_rows() and "TRANSCRIPT TAIL" not in terminal.text, "resize must preserve manual reading intent"
        terminal.resize(120, 42)
        assert reading_rows() and "TRANSCRIPT TAIL" not in terminal.text
        before = reading_rows()
        terminal.send(b"\x1b[5~")
        assert reading_rows() != before, "transcript keyboard focus survives real resize"
        before = reading_rows()
        row = prompt_top()
        terminal.click(terminal.lines[row].index("Prompt") + 1, row + 2)
        terminal.send(b"\x1b[6~\x1b[B")
        assert reading_rows() == before, ("composer arrows and PageDown must not scroll the transcript", before, reading_rows(), terminal.text)
        terminal.paste("held under modal")
        wait_file(terminal, paths["state"], lambda value: value.get("drafts", {}).get("fixture-env/thread-a") == "held under modal")
        terminal.command("help")
        terminal.wait("Commands")
        start = len(terminal.raw)
        terminal.send(copy_draft + b"\x1b[6~\x1b[B")
        assert b"\x1b]52;c;" not in terminal.raw[start:], "modal disables whole-draft copy"
        terminal.send(ESC)
        assert reading_rows() == before, "modal navigation must not scroll the underlying transcript"
        assert not native_payloads(paths), "editing, copying and reading never dispatch a native mutation"
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["real SLT PTY preserves pasted Unicode/whitespace and independent source drafts across bounded growth/reflow",
            "whole-draft Ctrl+Alt+C emits exact bytes without moving the caret and is disabled under a modal",
            "focused transcript PageUp survives real resize; composer/modal navigation retains exclusive key ownership"]


def answers(binary, directory):
    paths = fixture(directory / "answers", stream=False)
    terminal = Terminal(binary, paths["config"], paths["state"], mode="b")
    try:
        terminal.wait("Needs you")
        click(terminal, "Question to answer")
        terminal.wait("What should happen next?")
        terminal.wait("Ready for review")
        click(terminal, "Answer")
        terminal.wait("Question 1 / 1")
        click(terminal, "Continue")
        saved = wait_file(terminal, paths["state"], lambda value: bool(value.get("attentionDrafts")))
        assert not saved.get("pending")
        assert not native_payloads(paths)
        terminal.wait("Review answers")
        assert not native_payloads(paths)
        click(terminal, "Submit answers")
        wait_file(terminal, paths["native"], lambda value: value["effects"] == 1)
        saved = wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
        sent = native_payloads(paths)
        assert len(sent) == 1 and sent[0]["type"] == "runtime-request.respond"
        assert sent[0]["requestId"] == "request-question" and sent[0]["answers"] == {"choice": "Continue"}
        assert saved["attentionDrafts"], "acknowledged original answer source remains recoverable"
        assert terminal.close() == 0
    finally:
        terminal.close()
    return ["Mode B source selection loads real wording/support; edits autosave and explicit Submit acknowledges exactly one original native answer"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="t3oxide-slt-pty-") as directory:
        checks = run(args.binary.resolve(), Path(directory))
        checks.extend(answers(args.binary.resolve(), Path(directory)))
        checks.extend(composer_and_transcript_reflow(args.binary.resolve(), Path(directory)))
        checks.extend(ordered_sends(args.binary.resolve(), Path(directory)))
        checks.extend(commands_pointer_and_copy(args.binary.resolve(), Path(directory)))
    print(json.dumps({"scope": "synthetic PTY; native Rex/M4 acceptance remains separate", "checks": checks}, indent=2))


if __name__ == "__main__":
    main()
