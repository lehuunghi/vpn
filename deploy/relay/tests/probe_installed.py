#!/usr/bin/env python3
"""Verify the installed systemd service, not an in-process replacement."""
import contextlib
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
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
    folder = Path(tempfile.mkdtemp(prefix="vpn20-enroll-probe-"))
    s = json.loads(Path("/etc/vpn20-relay/state.json").read_text())
    ctx = ssl.create_default_context(cafile="/etc/vpn20-relay/ca.pem")
    ctx.set_alpn_protocols(["http/1.1"])
    request = http.client.HTTPConnection(s["domain"], s["control_port"], timeout=5)
    raw = socket.create_connection(("127.0.0.1", s["control_port"]), timeout=5)
    request.sock = ctx.wrap_socket(raw, server_hostname=s["domain"])
    request.request("POST", "/api/v1/enroll", json.dumps({"password": "native-enrollment-fixture-password", "claim": "d"*64}), {"Content-Type": "application/json"})
    response = request.getresponse()
    assert response.status == 200, "Installed password enrollment failed"
    profile = json.loads(response.read())
    request.close()
    config = folder/"probe.toml"
    config.write_text(f'''serverAddr = "127.0.0.1"
serverPort = {s["control_port"]}
user = {json.dumps(profile["name"])}
metadatas.access_key = {json.dumps(profile["accessKey"])}
auth.method = "token"
auth.token = {json.dumps(profile["token"])}
auth.additionalScopes = ["HeartBeats", "NewWorkConns"]
transport.tls.enable = true
transport.tls.serverName = {json.dumps(s["domain"])}
transport.tls.trustedCaFile = "/etc/vpn20-relay/ca.pem"
transport.heartbeatInterval = 10
transport.heartbeatTimeout = 30
[[proxies]]
name = "vpn20"
type = "udp"
localIP = "127.0.0.1"
localPort = {echo.getsockname()[1]}
remotePort = {profile["remotePort"]}
''')
    env = {k: v for k, v in os.environ.items() if k.lower() not in ["http_proxy", "https_proxy", "all_proxy"]}
    target = profile["remotePort"]
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
    if "folder" in globals():
        import shutil
        shutil.rmtree(folder)
