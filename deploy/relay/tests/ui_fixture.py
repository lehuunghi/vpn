"""Disposable real password/TLS/frps/UDP fixture for the VPN20 browser test."""
import importlib.util
import json
import pathlib
import shutil
import socket
import sys
import threading

root = pathlib.Path(sys.argv[1])
root.mkdir(parents=True, exist_ok=True)
source = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("fixtures", source / "tests/test_relay.py")
fixtures = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixtures)
relay = fixtures.relay
fixtures.certificate(root)
ports = set()
def free():
    while True:
        n = fixtures.free()
        if n not in ports:
            ports.add(n)
            return n
remote = fixtures.free(socket.SOCK_DGRAM)
while remote > 65000:
    remote = fixtures.free(socket.SOCK_DGRAM)
m = relay.Manager(root / "vps")
s = m.init("relay.example.test", free(), remote, remote + 20, free(), "1MB")
s["backend_port"] = free()
m.save(s)
m.set_password("browser-relay-fixture-password")
(m.root / "tls").mkdir()
for dest, origin in [("tls/fullchain.pem", "cert.pem"), ("tls/privkey.pem", "key.pem"), ("ca.pem", "ca.pem")]:
    shutil.copyfile(root / origin, m.root / dest)
(m.root / "frps.toml").write_text(m.server_config())
echo = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
echo.bind(("127.0.0.1", 0))
def loop():
    while True:
        data, address = echo.recvfrom(65535)
        echo.sendto(data, address)
threading.Thread(target=loop, daemon=True).start()
(root / "ready.json").write_text(json.dumps({"control": s["control_port"], "local": echo.getsockname()[1]}))
sys.exit(relay.serve(m, sys.argv[2]))
