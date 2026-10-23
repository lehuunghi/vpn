"""Real controlling-terminal regression with installer-style piped Python stdin."""
import fcntl
import os
from pathlib import Path
import pty
import select
import subprocess
import sys
import termios
import time
import unittest

MODULE = Path(__file__).resolve().parents[1] / "relay.py"
IMPORT = '''import importlib.util,sys
spec=importlib.util.spec_from_file_location("relay",sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
'''


@unittest.skipUnless(sys.platform.startswith("linux"), "Linux controlling-terminal test")
class TerminalPassword(unittest.TestCase):
    def terminal(self, second, accepted):
        secret = "terminal-fixture-password"
        source = IMPORT + '''
try:
    with open("/dev/tty","r+"): pass
except OSError:
    print("OLD_PROMPT_FAILED",flush=True)
else:
    raise AssertionError("Expected the old buffered tty open to fail")
try:
    result=m.prompt_password()
except ValueError as exc:
    if sys.argv[2]=="accept": raise
    print("MISMATCH_REFUSED",flush=True)
else:
    assert sys.argv[2]=="accept" and result=="terminal-fixture-password"
    print("PASSWORD_ACCEPTED",flush=True)
'''
        master, slave = pty.openpty()
        def controlling_terminal():
            os.setsid()
            fcntl.ioctl(1, termios.TIOCSCTTY, 0)
        child = subprocess.Popen([sys.executable, "-", str(MODULE), "accept" if accepted else "reject"],
                                 stdin=subprocess.PIPE, stdout=slave, stderr=slave,
                                 preexec_fn=controlling_terminal,
                                 env=dict(os.environ, PYTHONIOENCODING="utf-8"))
        os.close(slave)
        output = b""
        sent = 0
        try:
            child.stdin.write(source.encode())
            child.stdin.close()
            end = time.monotonic() + 10
            while time.monotonic() < end:
                readable, _, _ = select.select([master], [], [], .1)
                if readable:
                    try:
                        chunk = os.read(master, 65536)
                    except OSError:
                        break
                    if not chunk:
                        break
                    output += chunk
                if sent == 0 and "Mật khẩu relay".encode() in output:
                    os.write(master, (secret + "\n").encode())
                    sent = 1
                if sent == 1 and "Nhập lại mật khẩu relay".encode() in output:
                    os.write(master, (second + "\n").encode())
                    sent = 2
            self.assertEqual(child.wait(timeout=2), 0, output.decode(errors="replace"))
            self.assertIn(b"OLD_PROMPT_FAILED", output)
            self.assertIn(b"PASSWORD_ACCEPTED" if accepted else b"MISMATCH_REFUSED", output)
            self.assertNotIn(secret.encode(), output, "Terminal echoed the password")
            self.assertNotIn(second.encode(), output, "Terminal echoed confirmation")
            self.assertNotIn(b"GetPassWarning", output)
        finally:
            if child.poll() is None:
                child.kill()
            child.wait(timeout=2)
            os.close(master)

    def test_hidden_password_with_piped_stdin(self):
        self.terminal("terminal-fixture-password", True)

    def test_mismatch_never_echoes_credentials(self):
        self.terminal("different-fixture-password", False)

    def test_no_terminal_fails_without_stdin_fallback(self):
        source = IMPORT + '''
try: m.prompt_password()
except ValueError as exc:
    assert "--password-file" in str(exc)
    print("NO_TTY_REFUSED")
else: raise AssertionError("Accepted input without a controlling terminal")
'''
        result = subprocess.run([sys.executable, "-c", source, str(MODULE)], stdin=subprocess.DEVNULL,
                                capture_output=True, start_new_session=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr.decode(errors="replace"))
        self.assertIn(b"NO_TTY_REFUSED", result.stdout)
        self.assertNotIn(b"GetPassWarning", result.stderr)


if __name__ == "__main__":
    unittest.main()
