#!/usr/bin/env python3
"""Test-only PTY bridge; runs the actual executable, never a fake TUI.

Control input is NDJSON. Output is NDJSON containing base64 PTY bytes and the
child's exit status/termios restoration result. Only Python's stdlib is used.
"""

import base64
import fcntl
import json
import os
import pty
import selectors
import signal
import struct
import subprocess
import sys
import termios
import time


def emit(value):
    print(json.dumps(value), flush=True)


master, slave = pty.openpty()
before = termios.tcgetattr(slave)
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 34, 100, 0, 0))
child = subprocess.Popen(sys.argv[1:], stdin=slave, stdout=slave, stderr=slave,
                         start_new_session=True, close_fds=True)
os.set_blocking(master, False)
selector = selectors.DefaultSelector()
selector.register(master, selectors.EVENT_READ, "output")
selector.register(sys.stdin, selectors.EVENT_READ, "control")
control_buffer = b""
deadline = time.monotonic() + 60
try:
    emit({"started": child.pid})
    while child.poll() is None:
        if time.monotonic() > deadline:
            raise TimeoutError("test executable exceeded PTY deadline")
        for key, _ in selector.select(0.05):
            if key.data == "output":
                try:
                    data = os.read(master, 8192)
                except BlockingIOError:
                    continue
                except OSError:
                    data = b""
                if data:
                    emit({"output": base64.b64encode(data).decode("ascii")})
            else:
                chunk = os.read(sys.stdin.fileno(), 65536)
                if not chunk:
                    raise EOFError("test controller disconnected")
                control_buffer += chunk
                while b"\n" in control_buffer:
                    line, control_buffer = control_buffer.split(b"\n", 1)
                    command = json.loads(line)
                    if "write" in command:
                        os.write(master, command["write"].encode("utf-8"))
                    if "resize" in command:
                        columns, rows = command["resize"]
                        fcntl.ioctl(slave, termios.TIOCSWINSZ,
                                    struct.pack("HHHH", rows, columns, 0, 0))
                        os.kill(child.pid, signal.SIGWINCH)
                    if command.get("signal") == "TERM":
                        child.send_signal(signal.SIGTERM)
    while True:
        try:
            data = os.read(master, 8192)
        except (BlockingIOError, OSError):
            break
        if not data:
            break
        emit({"output": base64.b64encode(data).decode("ascii")})
    after = termios.tcgetattr(slave)
    # Darwin sets PENDIN when canonical input is restored, even for a plain
    # tty.setraw/tcsetattr round trip. It is pending-input bookkeeping rather
    # than a terminal mode; compare every persistent flag and control value.
    if sys.platform == "darwin":
        before[3] &= ~termios.PENDIN
        after[3] &= ~termios.PENDIN
    changes = {str(index): {"before": repr(saved), "after": repr(actual)}
               for index, (saved, actual) in enumerate(zip(before, after))
               if saved != actual}
    emit({"exit": child.returncode, "restored": not changes,
          "termiosChanges": changes})
finally:
    if child.poll() is None:
        child.terminate()
        try:
            child.wait(timeout=3)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait(timeout=3)
    selector.close()
    os.close(master)
    os.close(slave)
