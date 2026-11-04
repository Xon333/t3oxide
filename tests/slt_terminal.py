#!/usr/bin/env python3
"""Focused SLT runtime checks through PTY bytes and the real adapter fixture.

No installed T3 service, credentials, launcher or Rex configuration is accessed.
Run with uv run --with pyte==0.8.2 python tests/slt_terminal.py --binary target/debug/t3oxide.
"""
import argparse
import json
from pathlib import Path
import tempfile

from rust_terminal import ENTER, ESC, Terminal, fixture, native_payloads, wait_file


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
    print(json.dumps({"scope": "synthetic PTY; native Rex/M4 acceptance remains separate", "checks": checks}, indent=2))


if __name__ == "__main__":
    main()
