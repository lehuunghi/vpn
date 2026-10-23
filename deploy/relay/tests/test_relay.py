#!/usr/bin/env python3
"""Real frps/frpc integration plus profile and authorization boundary tests."""
import concurrent.futures
import contextlib
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import ssl
import subprocess
import tempfile
import threading
import time
import unittest
import urllib.request
import zipfile

MODULE = Path(__file__).resolve().parents[1] / "relay.py"
spec = importlib.util.spec_from_file_location("relay", MODULE)
relay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(relay)


def free(kind=socket.SOCK_STREAM):
    with socket.socket(socket.AF_INET, kind) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def certificate(folder):
    def run(*args):
        subprocess.run(["openssl", *args], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    run("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(folder/"ca.key"),
        "-out", str(folder/"ca.pem"), "-days", "2", "-subj", "/CN=VPN20 relay fixture CA")
    run("req", "-newkey", "rsa:2048", "-nodes", "-keyout", str(folder/"key.pem"),
        "-out", str(folder/"leaf.csr"), "-subj", "/CN=relay.example.test")
    (folder/"ext").write_text("subjectAltName=DNS:relay.example.test\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n")
    run("x509", "-req", "-in", str(folder/"leaf.csr"), "-CA", str(folder/"ca.pem"),
        "-CAkey", str(folder/"ca.key"), "-CAcreateserial", "-out", str(folder/"cert.pem"),
        "-days", "2", "-extfile", str(folder/"ext"))


class Profiles(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.m = relay.Manager(self.root)
        self.m.init("relay.example.test", 443, 20001, 20020, 18921, "1MB")
        (self.root/"ca.pem").write_text("public test CA")

    def tearDown(self):
        self.tmp.cleanup()

    def secret(self, name):
        return json.loads((self.root/"clients"/name/"credentials.json").read_text())["access_key"]

    def test_validation(self):
        for value in ["https://relay.example.test", "foo", "*.example.test", "127.0.0.1", "$(id).example.test", "a..test"]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                relay.domain(value)
        self.assertEqual(relay.domain("Relay.Example.Test."), "relay.example.test")
        for value in ["../bad", "a/b", "bad\nname", ""]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.m.add(value)

    def test_password_registration_retry_rotation_and_revocation(self):
        password = "fixture-enrollment-password"
        self.m.set_password(password)
        self.assertNotIn(password, (self.root/"enrollment.json").read_text())
        claim = secrets.token_hex(32)
        first = self.m.register(password, claim)
        self.assertEqual(first, self.m.register(password, claim))
        self.assertNotEqual(first["accessKey"], first["token"])
        second = self.m.register(password, secrets.token_hex(32))
        self.assertNotEqual(first["remotePort"], second["remotePort"])
        self.assertNotEqual(first["accessKey"], second["accessKey"])
        policy = relay.Policy(self.m)
        login = {"user": first["name"], "metas": {"access_key": first["accessKey"]}}
        policy.authorize("Login", login.copy())
        self.m.set_password("changed-registration-password")
        with self.assertRaises(ValueError):
            self.m.register(password, secrets.token_hex(32))
        policy.authorize("Login", login.copy())  # Rotation affects enrollment only.
        restored = relay.Manager(self.root)
        self.assertEqual(first, restored.register("changed-registration-password", claim))
        restored.enabled(first["name"], False)
        with self.assertRaises(ValueError):
            restored.register("changed-registration-password", claim)
        with self.assertRaises(ValueError):
            policy.authorize("Login", login)
        self.assertEqual(len(self.m.clients()), 2)

    def test_password_concurrent_allocation(self):
        self.m.set_password("concurrent-registration-password")
        claims = [secrets.token_hex(32) for _ in range(8)]
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
            rows = list(executor.map(lambda c: self.m.register("concurrent-registration-password", c), claims))
        self.assertEqual(len({r["remotePort"] for r in rows}), 8)
        self.assertEqual(len({r["accessKey"] for r in rows}), 8)

    def test_enrollment_rate_limit(self):
        limit = relay.EnrollmentLimit()
        for _ in range(5):
            self.assertTrue(limit.allow("192.0.2.1"))
        self.assertFalse(limit.allow("192.0.2.1"))
        for n in range(25):
            self.assertTrue(limit.allow("198.51.100."+str(n)))
        self.assertFalse(limit.allow("203.0.113.1"))

    def test_rerun_preserves_identity_ports_and_private_exports(self):
        archive = self.m.add("office")
        state = self.m.state()
        token = (self.root/"transport-token").read_bytes()
        key = self.secret("office")
        self.m.init("relay.example.test", 443, 20001, 20020, 18921, "1MB")
        self.assertEqual(self.m.state(), state)
        self.assertEqual((self.root/"transport-token").read_bytes(), token)
        with zipfile.ZipFile(archive) as z:
            config = z.read("frpc.toml").decode()
            self.assertIn(key, config)
            self.assertNotIn("credentials.json", z.namelist())
            self.assertNotIn("privkey.pem", z.namelist())
            self.assertIn('transport.tls.trustedCaFile = "ca.pem"', config)
            self.assertIn("loginFailExit = false", config)
        self.assertNotIn(key, (self.root/"state.json").read_text())
        self.assertEqual(archive.stat().st_mode & 0o777, 0o600)
        with self.assertRaises(ValueError):
            self.m.init("relay.example.test", 443, 20010, 20020, 18921, "1MB")
        self.assertEqual(self.m.state(), state)

    def test_concurrent_allocation_and_revoked_ports_not_reused(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
            list(executor.map(self.m.add, ["office"+str(i) for i in range(12)]))
        rows = self.m.state()["clients"]
        self.assertEqual(len({r["port"] for r in rows.values()}), 12)
        self.m.enabled("office0", False)
        with self.assertRaises(ValueError):
            self.m.add("other", rows["office0"]["port"])

    def test_policy_isolates_profiles_and_forces_server_limits(self):
        self.m.add("a")
        self.m.add("b")
        policy = relay.Policy(self.m)
        def login(name, key):
            return policy.authorize("Login", {
                "user": name, "metas": {"access_key": key}, "pool_count": 1, "run_id": "attacker-selected",
            })["content"]
        with self.assertRaises(ValueError):
            login("a", self.secret("b"))
        a = login("a", self.secret("a"))
        b = login("b", self.secret("b"))
        self.assertNotEqual(a["run_id"], "attacker-selected")
        self.assertNotEqual(a["run_id"], b["run_id"])
        self.assertNotIn("access_key", a["metas"])
        identity = {"user": "a", "run_id": a["run_id"]}
        good = {"user": identity, "proxy_name": "a.vpn20", "proxy_type": "udp", "remote_port": 20001}
        result = policy.authorize("NewProxy", dict(good))["content"]
        self.assertEqual(result["bandwidth_limit"], "1MB")
        self.assertEqual(result["bandwidth_limit_mode"], "server")
        for change in [{"remote_port": 20002}, {"proxy_type": "tcp"}, {"proxy_name": "b.vpn20"},
                       {"user": {"user": "a", "run_id": b["run_id"]}}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                policy.authorize("NewProxy", dict(good, **change))
        with self.assertRaises(ValueError):
            policy.authorize("NewWorkConn", {"user": identity, "run_id": b["run_id"]})
        self.m.enabled("a", False)
        for op, content in [
            ("Login", {"user": "a", "metas": {"access_key": self.secret("a")}}),
            ("NewProxy", good), ("Ping", {"user": identity}),
            ("NewWorkConn", {"user": identity, "run_id": a["run_id"]}),
        ]:
            with self.subTest(op=op), self.assertRaises(ValueError):
                policy.authorize(op, content)


@unittest.skipUnless(os.environ.get("FRP_BIN_DIR"), "set FRP_BIN_DIR for real frps/frpc integration")
class Integration(unittest.TestCase):
    def test_real_tls_multi_profile_udp_and_policy(self):
        binaries = Path(os.environ["FRP_BIN_DIR"])
        env = {k: v for k, v in os.environ.items() if k.lower() not in ("http_proxy", "https_proxy", "all_proxy")}
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            certificate(root)
            m = relay.Manager(root)
            first = free(socket.SOCK_DGRAM)
            while first > 65000:
                first = free(socket.SOCK_DGRAM)
            s = m.init("relay.example.test", free(), first, first+8, free(), "1MB")
            (root/"tls").mkdir()
            for destination, source in [("tls/fullchain.pem", "cert.pem"), ("tls/privkey.pem", "key.pem")]:
                (root/destination).write_bytes((root/source).read_bytes())
            relay.validate_certificate(s["domain"], root/"cert.pem", root/"key.pem", root/"ca.pem")
            with self.assertRaises(ValueError):
                relay.validate_certificate("wrong.example.test", root/"cert.pem", root/"key.pem", root/"ca.pem")
            with self.assertRaises(ValueError):
                relay.validate_certificate(s["domain"], root/"cert.pem", root/"ca.key", root/"ca.pem")
            (root/"frps.toml").write_text(m.server_config())
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
            for name in ["office", "second", "wrong-port", "wrong-key", "wrong-tls"]:
                m.add(name, local_port=echo.getsockname()[1])
            processes, logs = [], []
            def spawn(name, config, filename):
                out = (root/filename).open("w")
                logs.append(out)
                proc = subprocess.Popen([str(binaries/name), "-c", str(config)],
                                        cwd=config.parent, stdout=out, stderr=subprocess.STDOUT, env=env)
                processes.append(proc)
                return proc
            def client(name, transform=None):
                config = root/"clients"/name/"frpc.toml"
                text = config.read_text().replace('serverAddr = "relay.example.test"', 'serverAddr = "127.0.0.1"')
                if transform:
                    text = transform(text)
                config.write_text(text)
                return spawn("frpc", config, name+".log")
            def wait_for(filename, text, timeout=12):
                end = time.monotonic()+timeout
                while time.monotonic() < end:
                    if text in (root/filename).read_text():
                        return
                    time.sleep(.1)
                self.fail("Expected event missing: "+filename+" / "+text)
            policy_server = relay.http_server(relay.Policy(m), ("127.0.0.1", s["plugin_port"]))
            policy_thread = threading.Thread(target=policy_server.serve_forever, daemon=True)
            policy_thread.start()
            try:
                subprocess.run([str(binaries/"frps"), "verify", "-c", str(root/"frps.toml")], check=True,
                               stdout=subprocess.DEVNULL)
                spawn("frps", root/"frps.toml", "server.log")
                wait_for("server.log", "frps started successfully")
                for name in ["office", "second"]:
                    client(name)
                    wait_for(name+".log", "start proxy success")
                rows = m.state()["clients"]
                def exchange(i):
                    target = rows["office" if i % 2 else "second"]["port"]
                    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                        sock.settimeout(5)
                        for size in [32, 148, 1452]:
                            payload = bytes([i])*size
                            sock.sendto(payload, ("127.0.0.1", target))
                            answer, _ = sock.recvfrom(65535)
                            self.assertEqual(answer, payload)
                with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
                    list(executor.map(exchange, range(1, 9)))
                bad = client("wrong-key", lambda t: t.replace("metadatas.access_key = ", "#removed access_key = ")
                             .replace("loginFailExit = false", "loginFailExit = true"))
                self.assertNotEqual(bad.wait(timeout=12), 0)
                bad = client("wrong-tls", lambda t: t.replace('serverName = "relay.example.test"', 'serverName = "wrong.example.test"')
                             .replace("loginFailExit = false", "loginFailExit = true"))
                self.assertNotEqual(bad.wait(timeout=12), 0)
                client("wrong-port", lambda t: t.replace("remotePort = "+str(rows["wrong-port"]["port"]),
                                                       "remotePort = "+str(rows["office"]["port"])))
                wait_for("wrong-port.log", "start error")
                m.enabled("office", False)
                wait_for("office.log", "error", timeout=20)
                # Revoked profile cannot return on its next authentication.
                time.sleep(.5)
                self.assertFalse(m.state()["clients"]["office"]["enabled"])
                with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                    sock.settimeout(1)
                    sock.sendto(b"revoked", ("127.0.0.1", rows["office"]["port"]))
                    with self.assertRaises(socket.timeout):
                        sock.recvfrom(65535)
                exchange(2)  # Another profile remains usable after revocation.
                # Policy backend unavailable must reject registration.
                policy_server.shutdown()
                client("wrong-key")
                wait_for("wrong-key.log", "error")
            finally:
                for proc in reversed(processes):
                    if proc.poll() is None:
                        proc.terminate()
                for proc in processes:
                    with contextlib.suppress(subprocess.TimeoutExpired):
                        proc.wait(timeout=5)
                stop.set()
                echo.close()
                worker.join(timeout=1)
                policy_server.shutdown()
                policy_server.server_close()
                policy_thread.join(timeout=2)
                for out in logs:
                    out.close()
                # Logs are inspected only for accidental disclosure; never print secrets.
                for path in root.glob("*.log"):
                    self.assertNotIn((root/"transport-token").read_text().strip(), path.read_text())
                    for name in m.state()["clients"]:
                        secret = json.loads((root/"clients"/name/"credentials.json").read_text())["access_key"]
                        self.assertNotIn(secret, path.read_text())


if __name__ == "__main__":
    unittest.main()
