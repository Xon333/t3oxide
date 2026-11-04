#!/usr/bin/env python3
"""Historical recovery-checkout UX evidence for its Ratatui presentation.

Use slt_terminal.py for the revised active SLT contract. All native operations
here remain fixtures; this historical entry point is not a current pass claim.
"""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import tempfile

from rust_terminal import (
    DOWN, ENTER, ESC, Terminal, fixture, native_calls, native_payloads,
    native_reads_settled, private_json, wait_file,
)

SETTLE = b"\x14"
NAVIGATOR = b"\x0f"


def choose_tab(terminal, name):
    terminal.command("Conversation tabs")
    terminal.choose(name)


def select_title(terminal, title):
    terminal.send(NAVIGATOR + b"/")
    terminal.paste(title)
    terminal.wait(title)
    terminal.send(ENTER)


def click_completed(terminal, marker):
    terminal.wait(marker + " Completed")
    for row, line in enumerate(terminal.lines):
        column = line.find(marker + " Completed")
        if column >= 0:
            terminal.click(column + 2, row + 1)
            return
    raise AssertionError("Completed header is not mouse-addressable")


def workspace(binary, directory, output):
    paths = fixture(directory / "workspace", stream=False)
    generation = hashlib.sha256(json.dumps(
        ["run-thread-fork", "", ""], separators=(",", ":")
    ).encode()).hexdigest()
    private_json(paths["state"], {
        "drafts": {}, "pending": {}, "pendingStops": {},
        "rustUi": {"mode": "agentbox", "futureSetting": "preserve"},
        "completionReads": [{"environmentId": "fixture-env",
                             "threadId": "thread-fork", "generation": generation}],
    })
    draft = "/settle stays literal. Keep this café 日本語 draft on thread-a."
    terminal = Terminal(binary, paths["config"], paths["state"], mode="agentbox")
    try:
        terminal.wait("Live ·")
        terminal.wait("Finished")
        terminal.wait("Attention")
        terminal.wait("▸ Completed (1)")
        assert "Alternative design" not in terminal.text
        assert "AgentBox" not in terminal.text and "Developer" not in terminal.text
        assert "bounded view" not in terminal.text and "needs review" not in terminal.text
        navigator = "\n".join(line[:34] for line in terminal.lines)
        assert navigator.index("Open") < navigator.index("Running") < navigator.index("Pins") < navigator.index("Settled")
        terminal.send(NAVIGATOR)
        for height in [10, 11, 12]:
            terminal.resize(140, height)
            terminal.wait("Conversations")
        terminal.resize(140, 38)
        terminal.send(b"\t\t")
        terminal.paste(draft)
        terminal.send(NAVIGATOR)
        terminal.send("c")
        terminal.wait("Alternative design")
        click_completed(terminal, "▾")
        terminal.wait("▸ Completed (1)")
        assert "Alternative design" not in terminal.text
        terminal.send(b"\x1bh")
        terminal.wait("Keyboard help")
        terminal.wait("Alt+H / F1")
        terminal.send(ESC)
        choose_tab(terminal, "Running")
        terminal.wait("Active implementation")
        navigator = "\n".join(line[:34] for line in terminal.lines)
        assert "Finished refactor" not in navigator and "Permission needed" not in navigator
        choose_tab(terminal, "Pins")
        terminal.wait("Nothing here yet")
        choose_tab(terminal, "Open")
        select_title(terminal, "Finished refactor")
        terminal.send(SETTLE)
        wait_file(terminal, paths["native"], lambda value: value["effects"] == 1)
        saved = wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
        assert saved["drafts"]["fixture-env/thread-a"] == draft
        assert native_payloads(paths)[-1]["type"] == "thread.settle"
        assert native_payloads(paths)[-1]["threadId"] == "thread-review"
        assert "Apply" not in terminal.text
        choose_tab(terminal, "Settled")
        terminal.wait("Finished refactor")
        terminal.send(SETTLE)
        wait_file(terminal, paths["native"], lambda value: value["effects"] == 2)
        wait_file(terminal, paths["state"], lambda value: not value.get("pending"))
        assert native_payloads(paths)[-1]["type"] == "thread.unsettle"
        choose_tab(terminal, "Open")
        terminal.wait("Finished refactor")
        terminal.send(ENTER)
        saved = wait_file(terminal, paths["state"], lambda value: len(value.get("completionReads", [])) == 2)
        assert saved["drafts"]["fixture-env/thread-a"] == draft
        assert saved["rustUi"]["completedCollapsed"] is True
        assert saved["rustUi"]["futureSetting"] == "preserve"
        assert not saved.get("pending")
        native_reads_settled(terminal, paths)
        native = json.loads(paths["native"].read_text())
        assert native["effects"] == 2
        assert native["projections"]["thread-review"]["thread"]["settledOverride"] != "settled"
        terminal.save_screen(output / "dev-open")
        assert terminal.close() == 0
    finally:
        terminal.close()
    resumed = Terminal(binary, paths["config"], paths["state"], thread="")
    try:
        resumed.wait("▸ Completed (2)")
        assert "Finished refactor" not in resumed.text
        assert "Alternative design" not in resumed.text
        resumed.send("c")
        resumed.wait("Finished refactor")
        resumed.wait("Alternative design")
        assert json.loads(paths["native"].read_text())["effects"] == 2
        assert resumed.close() == 0
    finally:
        resumed.close()
    return [
        "Dev migration, exact Open/Running/Pins/Settled order and accurate running membership",
        "Finished/Attention grouping and Completed keyboard/mouse collapse persist across restart",
        "Ctrl+T directly settles/unsettles highlighted native source without detaching or changing its draft",
        "verified opening consumes only local source/run completion marker, never native disposition",
        "literal composer slash text, Alt+H help and lean chrome preserve errors and state",
    ]


def uncertain_settle(binary, directory):
    paths = fixture(directory / "uncertain", stream=False, dropAckOnce=True)
    terminal = Terminal(binary, paths["config"], paths["state"], thread="")
    try:
        terminal.wait("Finished refactor")
        select_title(terminal, "Finished refactor")
        terminal.send(SETTLE)
        original = wait_file(terminal, paths["state"], lambda value: bool(value.get("pending")))["pending"]["fixture-env"]
        wait_file(terminal, paths["native"], lambda value: value["effects"] == 1 and value["dropped"])
        terminal.wait("unresolved")
        assert terminal.close() == 0
    finally:
        terminal.close()
    resumed = Terminal(binary, paths["config"], paths["state"], thread="")
    try:
        resumed.wait("Conversations")
        native_reads_settled(resumed, paths)
        assert json.loads(paths["state"].read_text())["pending"]["fixture-env"] == original
        assert len(native_payloads(paths)) == 1
        resumed.command("recover")
        resumed.wait("Saved actions")
        resumed.send(ENTER)
        resumed.choose("Reconcile original action")
        wait_file(resumed, paths["state"], lambda value: not value.get("pending"))
        payloads = native_payloads(paths)
        assert len(payloads) == 2 and payloads[0] == payloads[1]
        assert payloads[-1]["commandId"] == original["id"]
        assert json.loads(paths["native"].read_text())["effects"] == 1
        assert resumed.close() == 0
    finally:
        resumed.close()
    return ["uncertain native settle survives restart without replay; explicit same-ID reconciliation has one effect"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="t3oxide-ux-terminal-") as temporary:
        directory = Path(temporary)
        checks = workspace(binary, directory, args.output)
        checks.extend(uncertain_settle(binary, directory))
    result = {"platform": platform.system(), "architecture": platform.machine(),
              "binarySha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
              "checks": checks, "scope": "installed/release binary, synthetic native fixture; no provider execution"}
    (args.output / "ux-acceptance.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
