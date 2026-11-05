#!/usr/bin/env python3
"""Focused SLT runtime checks through PTY bytes and the real adapter fixture.

No installed T3 service, credentials, launcher or Rex configuration is accessed.
Run with uv run --with pyte==0.8.2 python tests/slt_terminal.py --binary target/debug/t3oxide.
"""
import argparse
import base64
import json
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
        for label in ["Unread", "Running", "Pins", "Settled", "Project", "Env", "Ctrl+] send", "Send"]:
            terminal.wait(label)
        click(terminal, "Env")
        terminal.wait("Environment scope")
        terminal.wait("All environments")
        terminal.wait("Fixture host")
        assert "Unknown action" not in terminal.text
        assert not native_payloads(paths), "environment browsing cannot dispatch a mutation"
        terminal.send(ESC)
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
        click(terminal, "Prompt")
        terminal.send(b"\x1b[97;5:1u\x1b[97;5:3u\x1b[99;5:1u\x1b[99;5:3u")
        assert terminal.process.poll() is None
        expected = b"\x1b]52;c;" + base64.b64encode(b"/help")
        assert expected in terminal.raw, "selected editor copy must emit its exact OSC52 text"
        terminal.send(b"\x7f")
        terminal.send(b"\x03")  # No selection: no copied text and no reader-level exit.
        assert terminal.process.poll() is None
        # Pointer activation has the same edit/submit/tail boundary as the chord.
        matches = [(line.index("Send") + 1, row + 1) for row, line in enumerate(terminal.lines) if "Send" in line]
        x, y = matches[-1]
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
        terminal.wait("Ctrl+] send")
        # Exact text before modal save; tail is routed to the next prompt draft.
        click(terminal, "Commands")
        click(terminal, "Enter command")
        terminal.wait("Explicit command entry.")
        terminal.send(b"latest\x1dTAIL", settle=0.4)
        saved = wait_file(terminal, paths["state"], lambda value:
                          value.get("drafts", {}).get("fixture-env/thread-a") == "mouse-nextTAIL")
        assert len(native_payloads(paths)) == 1
        assert "Unknown action" not in terminal.text
        click(terminal, "Commands")
        click(terminal, "Enter command")
        terminal.wait("Explicit command entry.")
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
            "batched Ctrl+P/help/Enter settles palette focus and executes Help without changing draft",
            "focused Control select-all/copy Press+Release emits exact clipboard text; unselected Ctrl+C cannot exit",
            "batched pointer Send separates preceding edits from next draft; modal text/save/tail stays ordered"]


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
        terminal.wait("Input and focus")
        assert json.loads(paths["native"].read_text())["effects"] == 1
        terminal.send(ESC)
        terminal.send(b"\x1bOQ")  # F2: actual human queue, not failed/prose rows.
        terminal.wait("Questions")
        terminal.wait("Question to answer")
        click(terminal, "Search")
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
        "Mode B admits actual questions and source-backed inventory retains a zero-question project",
    ]


def answers(binary, directory):
    paths = fixture(directory / "answers", stream=False)
    terminal = Terminal(binary, paths["config"], paths["state"], mode="b")
    try:
        terminal.wait("Questions")
        click(terminal, "Question to answer")
        terminal.wait("What should happen next?")
        terminal.wait("Ready for review")
        click(terminal, "Answer")
        terminal.wait("Question 1 / 1")
        click(terminal, "[ Continue ]")
        saved = wait_file(terminal, paths["state"], lambda value: bool(value.get("attentionDrafts")))
        assert not saved.get("pending")
        assert not native_payloads(paths)
        click(terminal, "[ Next ]")
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
        checks.extend(ordered_sends(args.binary.resolve(), Path(directory)))
        checks.extend(commands_pointer_and_copy(args.binary.resolve(), Path(directory)))
    print(json.dumps({"scope": "synthetic PTY; native Rex/M4 acceptance remains separate", "checks": checks}, indent=2))


if __name__ == "__main__":
    main()
