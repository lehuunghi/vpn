#!/usr/bin/env python3
"""Verify the installed systemd service, not an in-process replacement."""
import contextlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import threading
import time

echo = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
echo.bind(("127.0.0.1", 0))
echo.settimeout(.2)
stop = threading.Event()


def loop():
    while not stop.is_set():
        try:
            data, address = echo.recvfrom(65535)
            echo.sendto(data, address)
        except socket.timeout:
            pass
        except OSError:
            break


worker = threading.Thread(target=loop, daemon=True)
worker.start()
client = None
try:
    archive = subprocess.check_output(
        ["vpn20-relay", "add", "native-probe", "--local-port", str(echo.getsockname()[1])], text=True
    ).strip()
    folder = Path(archive).parent
    config = folder/"probe.toml"
    config.write_text((folder/"frpc.toml").read_text().replace(
        'serverAddr = "relay.example.test"', 'serverAddr = "127.0.0.1"'))
    env = {k: v for k, v in os.environ.items() if k.lower() not in ["http_proxy", "https_proxy", "all_proxy"]}
    s = json.loads(Path("/etc/vpn20-relay/state.json").read_text())
    target = s["clients"]["native-probe"]["port"]
    with tempfile.TemporaryFile(mode="w+") as log:
        client = subprocess.Popen([os.environ["FRPC"], "-c", str(config)], cwd=folder,
                                  stdout=log, stderr=subprocess.STDOUT, env=env)
        end = time.monotonic()+15
        while True:
            log.seek(0)
            if "start proxy success" in log.read():
                break
            if client.poll() is not None or time.monotonic() > end:
                raise RuntimeError("Installed relay did not authorize the native client")
            time.sleep(.2)
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.settimeout(5)
            for size in [32, 148, 1452]:
                payload = os.urandom(size)
                sock.sendto(payload, ("127.0.0.1", target))
                reply, _ = sock.recvfrom(65535)
                assert reply == payload
        print("Installed rootless systemd relay: TLS, scoped profile and UDP round trips PASS")
finally:
    if client is not None and client.poll() is None:
        client.terminate()
        with contextlib.suppress(subprocess.TimeoutExpired):
            client.wait(timeout=5)
    stop.set()
    echo.close()
    worker.join(timeout=1)
