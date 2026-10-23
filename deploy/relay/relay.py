#!/usr/bin/env python3
"""VPN20 standalone relay: profile management and loopback frps authorization."""
import argparse
import contextlib
import fcntl
import grp
import hashlib
import hmac
import http.server
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.parse
import zipfile

VERSION = "1.0.0"
FRP_VERSION = "0.71.1"
FRP_HASHES = {
    "linux_amd64": "1d9875210972a5d1a62b2dc4c48df45ab0af05d3ca1b5e494a014428259df803",
    "linux_arm64": "8edf590b0c396fa42f9c147121a76af83cfc2002b08f5aef0ea1008c43482019",
    "windows_amd64": "b90074b2b6a1d792a7a40f085ac6d276bcab2d1bc60201f8a86ca53ce65ce821",
}
NAME = re.compile(r"[a-z0-9][a-z0-9-]{0,39}\Z")


def domain(value):
    value = value.strip().lower().rstrip(".")
    labels = value.split(".")
    if len(value) > 253 or len(labels) < 2 or any(
        not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", part)
        for part in labels
    ):
        raise ValueError("Domain không hợp lệ; nhập hostname, không có https:// hoặc cổng")
    try:
        ipaddress.ip_address(value)
    except ValueError:
        return value
    raise ValueError("Cần domain để xác minh TLS")


def port(value):
    value = int(value)
    if not 1 <= value <= 65535:
        raise ValueError("Cổng phải trong khoảng 1–65535")
    return value


def bandwidth(value):
    if not re.fullmatch(r"(?:0|[1-9][0-9]{0,5}(?:KB|MB))", value):
        raise ValueError("Giới hạn dùng 1MB, 512KB hoặc 0 (không giới hạn)")
    return value


def validate_certificate(host, cert, key, ca):
    """Validate name, current validity, full chain and matching unencrypted key."""
    def openssl(*args):
        result = subprocess.run(["openssl", *args], stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
        if result.returncode:
            raise ValueError("TLS không hợp lệ: kiểm tra hostname, thời hạn, fullchain, CA và khóa không mã hóa")
        return result.stdout
    openssl("x509", "-in", str(cert), "-noout", "-checkend", "0")
    openssl("verify", "-purpose", "sslserver", "-verify_hostname", host,
            "-CAfile", str(ca), "-untrusted", str(cert), str(cert))
    if openssl("x509", "-in", str(cert), "-pubkey", "-noout") != openssl(
        "pkey", "-in", str(key), "-pubout", "-passin", "pass:"
    ):
        raise ValueError("Chứng chỉ và khóa TLS không khớp")


def atomic(path, data, mode=0o640, gid=None):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".new-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data.encode() if isinstance(data, str) else data)
            f.flush()
            os.fchmod(f.fileno(), mode)
            if gid is not None:
                os.fchown(f.fileno(), 0, gid)
            os.fsync(f.fileno())
        os.replace(name, path)
    finally:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(name)


class Manager:
    def __init__(self, root="/etc/vpn20-relay", gid=None):
        self.root = Path(root)
        self.gid = gid

    def state(self):
        with (self.root / "state.json").open() as f:
            data = json.load(f)
        if data.get("schema") != 1:
            raise ValueError("Schema relay chưa được hỗ trợ")
        return data

    def save(self, data):
        atomic(self.root / "state.json", json.dumps(data, indent=2) + "\n", gid=self.gid)

    @contextlib.contextmanager
    def lock(self):
        self.root.mkdir(parents=True, exist_ok=True)
        with (self.root / "state.lock").open("a") as f:
            os.chmod(f.name, 0o600)
            fcntl.flock(f, fcntl.LOCK_EX)
            yield

    def init(self, host, control, first, last, plugin, limit):
        host, control, first, last, plugin = domain(host), port(control), port(first), port(last), port(plugin)
        limit = bandwidth(limit)
        if first > last or control == plugin or first <= plugin <= last:
            raise ValueError("Dải cổng hoặc cổng điều phối không hợp lệ")
        with self.lock():
            old = self.state() if (self.root / "state.json").exists() else None
            if old and any(not first <= row["port"] <= last for row in old["clients"].values()):
                raise ValueError("Dải cổng mới loại bỏ cổng đã cấp; giữ lại dải hiện có")
            data = {
                "schema": 1, "version": VERSION, "domain": host, "control_port": control,
                "port_start": first, "port_end": last, "plugin_port": plugin,
                "default_limit": limit, "clients": old["clients"] if old else {},
            }
            if not (self.root / "transport-token").exists():
                if old:
                    raise ValueError("Thiếu khóa vận chuyển; khôi phục bản sao lưu")
                atomic(self.root / "transport-token", secrets.token_hex(32) + "\n", gid=self.gid)
            self.save(data)
            return data

    def server_config(self, data=None):
        s = data or self.state()
        q = json.dumps
        return f'''bindAddr = "0.0.0.0"
bindPort = {s["control_port"]}
auth.method = "token"
auth.tokenSource.type = "file"
auth.tokenSource.file.path = {q(str(self.root / "transport-token"))}
auth.additionalScopes = ["HeartBeats", "NewWorkConns"]
allowPorts = [{{ start = {s["port_start"]}, end = {s["port_end"]} }}]
maxPortsPerClient = 1
transport.tls.force = true
transport.tls.certFile = {q(str(self.root / "tls/fullchain.pem"))}
transport.tls.keyFile = {q(str(self.root / "tls/privkey.pem"))}
transport.heartbeatTimeout = 30
detailedErrorsToClient = false
log.to = "console"
log.level = "info"
[[httpPlugins]]
name = "vpn20-profile-policy"
addr = "127.0.0.1:{s["plugin_port"]}"
path = "/authorize"
ops = ["Login", "NewProxy", "Ping", "NewWorkConn"]
'''

    def client_config(self, name, key, s=None):
        s = s or self.state()
        c = s["clients"][name]
        q = json.dumps
        token = (self.root / "transport-token").read_text().strip()
        return f'''# VPN20 relay {VERSION}; frpc {FRP_VERSION}
serverAddr = {q(s["domain"])}
serverPort = {s["control_port"]}
user = {q(name)}
loginFailExit = false
metadatas.access_key = {q(key)}
auth.method = "token"
auth.token = {q(token)}
auth.additionalScopes = ["HeartBeats", "NewWorkConns"]
transport.tls.enable = true
transport.tls.serverName = {q(s["domain"])}
transport.tls.trustedCaFile = "ca.pem"
transport.heartbeatInterval = 10
transport.heartbeatTimeout = 30
[[proxies]]
name = "vpn20"
type = "udp"
localIP = {q(c["local_ip"])}
localPort = {c["local_port"]}
remotePort = {c["port"]}
transport.bandwidthLimit = {q(c["limit"])}
transport.bandwidthLimitMode = "server"
'''

    def profile(self, name, s=None):
        s = s or self.state()
        c = s["clients"][name]
        folder = self.root / "clients" / name
        key = json.loads((folder / "credentials.json").read_text())["access_key"]
        if not hmac.compare_digest(hashlib.sha256(key.encode()).hexdigest(), c["key_hash"]):
            raise ValueError("Khóa profile không khớp")
        atomic(folder / "frpc.toml", self.client_config(name, key, s), 0o600)
        atomic(folder / "ca.pem", (self.root / "ca.pem").read_bytes(), 0o600)
        atomic(folder / "install-client.sh", LINUX_INSTALL.replace("@NAME@", name), 0o700)
        atomic(folder / "Install-Client.ps1", WINDOWS_INSTALL.replace("@NAME@", name), 0o600)
        text = f"""VPN20 relay {VERSION} — {name}
Endpoint WireGuard: {s["domain"]}:{c["port"]}
Đích cục bộ: {c["local_ip"]}:{c["local_port"]}/UDP
Giới hạn cho cả máy VPN20: {c["limit"]}; MB là byte/giây (1 MB xấp xỉ 8 Mbps).

Giải nén trên máy cài VPN20, giữ các tệp cùng thư mục.
Windows x64: mở PowerShell Administrator tại thư mục, chạy:
  powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\\Install-Client.ps1
Ubuntu/Debian amd64/arm64: sudo bash ./install-client.sh
Các script tải frpc {FRP_VERSION} chính thức, kiểm tra SHA-256 và tự khởi động lại.

Sửa Endpoint trong bản .conf thử nghiệm thành {s["domain"]}:{c["port"]}.
Giữ nguyên khóa, AllowedIPs và DNS của thiết bị. UDP WireGuard trên máy VPN20
vẫn là {c["local_port"]}; trang quản trị vẫn dùng cổng đang cấu hình (51821 mặc định).
Đường relay này chỉ chuyển UDP; truy cập quản trị Internet cần đường HTTPS riêng.
VPN20 hiện chưa tích hợp quản lý frpc; cấu hình tải lại từ app có thể dùng Endpoint cũ.

Profile có khóa riêng: chỉ gửi cho chủ máy VPN20 này. Không công khai ZIP.
Linux xem log: journalctl -u vpn20-relay-client-{name} -n 50 --no-pager
Windows xem task: VPN20-Relay-{name}
"""
        atomic(folder / "README.txt", text, 0o600)
        with tempfile.NamedTemporaryFile(dir=folder, delete=False) as f:
            archive = Path(f.name)
        try:
            with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
                for filename in ["frpc.toml", "ca.pem", "install-client.sh", "Install-Client.ps1", "README.txt"]:
                    z.write(folder / filename, filename)
            archive.chmod(0o600)
            destination = folder / f"vpn20-relay-{name}.zip"
            os.replace(archive, destination)
        finally:
            archive.unlink(missing_ok=True)
        return destination

    def add(self, name, assigned=None, local_ip="127.0.0.1", local_port=51820, limit=None):
        if not NAME.fullmatch(name):
            raise ValueError("Tên dùng 1–40 ký tự a-z, 0-9, dấu gạch nối")
        local_ip = str(ipaddress.ip_address(local_ip))
        local_port = port(local_port)
        with self.lock():
            s = self.state()
            if name in s["clients"]:
                raise ValueError("Tên đã có; dùng export để lấy lại profile, không đổi khóa tự động")
            used = {c["port"] for c in s["clients"].values()}
            if assigned is None:
                assigned = next((n for n in range(s["port_start"], s["port_end"] + 1) if n not in used), None)
                if assigned is None:
                    raise ValueError("Hết cổng trong dải đã cấp")
            assigned = port(assigned)
            if not s["port_start"] <= assigned <= s["port_end"] or assigned in used:
                raise ValueError("Cổng không thuộc dải hoặc đã được cấp")
            # Reserve credentials before committing registration. A crash never
            # leaves an enabled profile whose secret was not saved.
            key = secrets.token_hex(32)
            folder = self.root / "clients" / name
            folder.mkdir(parents=True, exist_ok=True, mode=0o700)
            folder.chmod(0o700)
            atomic(folder / "credentials.json", json.dumps({"access_key": key}), 0o600)
            s["clients"][name] = {
                "port": assigned, "local_ip": local_ip, "local_port": local_port,
                "limit": bandwidth(limit or s["default_limit"]), "enabled": True,
                "key_hash": hashlib.sha256(key.encode()).hexdigest(),
            }
            self.save(s)
            return self.profile(name, s)

    def enabled(self, name, value):
        with self.lock():
            s = self.state()
            if name not in s["clients"]:
                raise ValueError("Không có profile này")
            s["clients"][name]["enabled"] = value
            self.save(s)

    def sync_tls(self):
        with self.lock():
            s = self.state()
            validate_certificate(s["domain"], s["cert_source"], s["key_source"], s["ca_source"])
            data = {
                self.root / "tls/fullchain.pem": Path(s["cert_source"]).read_bytes(),
                self.root / "tls/privkey.pem": Path(s["key_source"]).read_bytes(),
                self.root / "ca.pem": Path(s["ca_source"]).read_bytes(),
            }
            old = {path: path.read_bytes() for path in data}
            if all(old[path] == value for path, value in data.items()):
                return False
            try:
                for path, value in data.items():
                    atomic(path, value, gid=self.gid)
                subprocess.run(["systemctl", "try-restart", "vpn20-relay.service"], check=True, timeout=45)
            except (OSError, subprocess.SubprocessError):
                for path, value in old.items():
                    atomic(path, value, gid=self.gid)
                subprocess.run(["systemctl", "try-restart", "vpn20-relay.service"], timeout=45, check=False)
                raise ValueError("Không nạp được TLS mới; đã khôi phục tệp chứng chỉ cũ")
            for name in s["clients"]:
                self.profile(name, s)
            return True


class Policy:
    """Only frps on loopback calls this; it is never a public enrollment API."""
    def __init__(self, manager):
        self.manager = manager
        self.sessions = {}
        self.lock = threading.Lock()

    def authorize(self, op, content):
        if not isinstance(content, dict):
            raise ValueError("invalid request")
        login = op == "Login"
        user = content.get("user") if login else content.get("user", {}).get("user")
        if not isinstance(user, str) or not NAME.fullmatch(user):
            raise ValueError("unauthorized")
        row = self.manager.state()["clients"].get(user)
        if not row or not row["enabled"]:
            raise ValueError("unauthorized")
        if login:
            metas = content.get("metas") or {}
            key = metas.get("access_key", "")
            if not isinstance(key, str) or len(key) != 64 or not hmac.compare_digest(
                hashlib.sha256(key.encode()).hexdigest(), row["key_hash"]
            ):
                raise ValueError("unauthorized")
            # A client cannot supply another profile's run_id to take over its
            # control connection. Fresh unguessable IDs also authorize work
            # streams; the transport token alone grants no profile access.
            run_id = secrets.token_hex(16)
            with self.lock:
                self.sessions[user] = run_id
            content["run_id"] = run_id
            content["pool_count"] = min(max(int(content.get("pool_count", 1)), 0), 2)
            content["metas"] = {k: v for k, v in metas.items() if k != "access_key"}
            return {"unchange": False, "content": content}
        identity = content.get("user", {})
        run_id = identity.get("run_id", "")
        with self.lock:
            expected = self.sessions.get(user)
        if not expected or not isinstance(run_id, str) or not hmac.compare_digest(run_id, expected):
            raise ValueError("unauthorized")
        if op == "NewWorkConn":
            if not hmac.compare_digest(str(content.get("run_id", "")), expected):
                raise ValueError("unauthorized")
        elif op == "NewProxy":
            if (content.get("proxy_type") != "udp" or content.get("remote_port") != row["port"]
                    or content.get("proxy_name") != user + ".vpn20" or content.get("group")):
                raise ValueError("profile port/proxy is not allowed")
            content["bandwidth_limit"] = row["limit"]
            content["bandwidth_limit_mode"] = "server"
            content["use_compression"] = False
            content["use_encryption"] = False  # verified TLS already protects transport
            return {"unchange": False, "content": content}
        elif op != "Ping":
            raise ValueError("unsupported operation")
        return {"unchange": True}


def http_server(policy, address):
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass  # Never log headers, auth metadata or bodies.

        def setup(self):
            super().setup()
            self.connection.settimeout(5)

        def do_GET(self):
            self.send_error(404)

        def do_POST(self):
            response = {"reject": True, "reject_reason": "relay authorization denied"}
            try:
                u = urllib.parse.urlsplit(self.path)
                if u.path != "/authorize":
                    raise ValueError("path")
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 < size <= 32768 or self.headers.get("Transfer-Encoding"):
                    raise ValueError("body")
                op = urllib.parse.parse_qs(u.query).get("op", [""])[0]
                response = policy.authorize(op, json.loads(self.rfile.read(size))["content"])
            except (ValueError, KeyError, TypeError, AttributeError, OSError, OverflowError):
                pass  # Missing state/backend errors deny access.
            data = json.dumps(response).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    class Server(http.server.ThreadingHTTPServer):
        daemon_threads = True
        request_queue_size = 16
        block_on_close = False

    return Server(address, Handler)


def serve(manager, executable):
    s = manager.state()
    server = http_server(Policy(manager), ("127.0.0.1", s["plugin_port"]))
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    child = None
    ending = threading.Event()

    def stop(_signum, _frame):
        ending.set()
        if child is not None and child.poll() is None:
            child.terminate()

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        child = subprocess.Popen([executable, "-c", str(manager.root / "frps.toml")], stdin=subprocess.DEVNULL)
        result = child.wait()
        return 0 if ending.is_set() else result or 1
    finally:
        if child is not None and child.poll() is None:
            child.kill()
            child.wait()
        server.shutdown()
        server.server_close()
        worker.join(timeout=5)


LINUX_INSTALL = r'''#!/usr/bin/env bash
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo "Chạy bằng sudo bash ./install-client.sh"; exit 1; }
command -v systemctl >/dev/null || { echo "Cần Linux systemd"; exit 1; }
command -v apt-get >/dev/null || { echo "Bộ cài client này cần Ubuntu/Debian"; exit 1; }
source_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
name="@NAME@"
case "$(uname -m)" in
  x86_64) arch=amd64; hash=1d9875210972a5d1a62b2dc4c48df45ab0af05d3ca1b5e494a014428259df803;;
  aarch64|arm64) arch=arm64; hash=8edf590b0c396fa42f9c147121a76af83cfc2002b08f5aef0ea1008c43482019;;
  *) echo "Cần Linux amd64/arm64"; exit 1;;
esac
apt-get update
apt-get install -y ca-certificates curl tar
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
curl --fail --location --silent --show-error --proto '=https' --proto-redir '=https' \
  "https://github.com/fatedier/frp/releases/download/v0.71.1/frp_0.71.1_linux_$arch.tar.gz" -o "$work/frp.tar.gz"
echo "$hash  $work/frp.tar.gz" | sha256sum -c -
tar -xzf "$work/frp.tar.gz" -C "$work" "frp_0.71.1_linux_$arch/frpc"
folder="/opt/vpn20-relay-client/$name"
install -d -m 0700 "$folder"
systemctl stop "vpn20-relay-client-$name.service" 2>/dev/null || true
install -m 0755 "$work/frp_0.71.1_linux_$arch/frpc" "$folder/frpc"
for file in frpc.toml ca.pem; do install -m 0600 "$source_dir/$file" "$folder/$file"; done
cd "$folder"
./frpc verify -c frpc.toml
cat > "/etc/systemd/system/vpn20-relay-client-$name.service" <<EOF
[Unit]
Description=VPN20 relay client $name
Wants=network-online.target
After=network-online.target
[Service]
WorkingDirectory=$folder
ExecStart=$folder/frpc -c $folder/frpc.toml
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now "vpn20-relay-client-$name.service"
echo "Đã cài client. Xem log: journalctl -u vpn20-relay-client-$name -n 50 --no-pager"
'''

WINDOWS_INSTALL = r'''#Requires -RunAsAdministrator
$ErrorActionPreference = 'Stop'
if (-not [Environment]::Is64BitOperatingSystem) { throw 'Bộ tải tự động này cần Windows x64.' }
$name = '@NAME@'
$folder = Join-Path $env:ProgramData "VPN20-Relay\$name"
$taskName = "VPN20-Relay-$name"
$temp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Force $temp, $folder | Out-Null
& icacls.exe $folder /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Không đặt được quyền thư mục.' }
try {
    $zip = Join-Path $temp 'frp.zip'
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -UseBasicParsing -Uri 'https://github.com/fatedier/frp/releases/download/v0.71.1/frp_0.71.1_windows_amd64.zip' -OutFile $zip
    if ((Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant() -ne 'b90074b2b6a1d792a7a40f085ac6d276bcab2d1bc60201f8a86ca53ce65ce821') { throw 'SHA-256 của frpc không khớp.' }
    Expand-Archive -LiteralPath $zip -DestinationPath $temp
    if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
        Stop-ScheduledTask -TaskName $taskName
        Start-Sleep -Seconds 1
    }
    Copy-Item (Join-Path $temp 'frp_0.71.1_windows_amd64\frpc.exe') $folder -Force
    foreach ($file in @('frpc.toml','ca.pem')) { Copy-Item (Join-Path $PSScriptRoot $file) $folder -Force }
    $exe = Join-Path $folder 'frpc.exe'
    $config = Join-Path $folder 'frpc.toml'
    Push-Location $folder
    try {
        & $exe verify -c $config
        if ($LASTEXITCODE -ne 0) { throw 'Cấu hình frpc không hợp lệ.' }
    } finally { Pop-Location }
    $action = New-ScheduledTaskAction -Execute $exe -Argument ('-c "' + $config + '"') -WorkingDirectory $folder
    $trigger = New-ScheduledTaskTrigger -AtStartup
    $settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -User 'SYSTEM' -RunLevel Highest -Force | Out-Null
    Start-ScheduledTask -TaskName $taskName
    Write-Host "Đã cài client. Task: $taskName. Cấu hình: $folder"
} finally { Remove-Item -LiteralPath $temp -Recurse -Force }
'''


def main():
    parser = argparse.ArgumentParser(description="VPN20 standalone relay")
    parser.add_argument("--root", default="/etc/vpn20-relay", help=argparse.SUPPRESS)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("list")
    sub.add_parser("sync-tls")
    for name in ["export", "revoke", "enable"]:
        p = sub.add_parser(name)
        p.add_argument("name")
    p = sub.add_parser("add")
    p.add_argument("name")
    p.add_argument("--port", type=port)
    p.add_argument("--local-ip", default="127.0.0.1")
    p.add_argument("--local-port", type=port, default=51820)
    p.add_argument("--limit", type=bandwidth)
    p = sub.add_parser("serve")
    p.add_argument("--frps", default="/usr/local/lib/vpn20-relay/frps")
    args = parser.parse_args()
    if args.command != "serve" and os.geteuid() != 0:
        parser.error("Chạy lệnh quản lý bằng sudo/root")
    gid = grp.getgrnam("vpn20-relay").gr_gid if os.geteuid() == 0 else None
    m = Manager(args.root, gid)
    if args.command == "serve":
        return serve(m, args.frps)
    if args.command == "sync-tls":
        print("Đã nạp TLS mới" if m.sync_tls() else "TLS không thay đổi")
    elif args.command == "list":
        s = m.state()
        for name, row in sorted(s["clients"].items()):
            print(f'{name}\t{s["domain"]}:{row["port"]}\t{row["limit"]}\t{"enabled" if row["enabled"] else "revoked"}')
    elif args.command in ["revoke", "enable"]:
        m.enabled(args.name, args.command == "enable")
        print("Đã lưu; quyền được kiểm tra lại ở heartbeat kế tiếp (khoảng 10–30 giây)")
    elif args.command == "export":
        print(m.profile(args.name))
    elif args.command == "add":
        print(m.add(args.name, args.port, args.local_ip, args.local_port, args.limit))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError, subprocess.SubprocessError) as exc:
        print("VPN20 relay:", str(exc), file=sys.stderr)
        sys.exit(1)
