#!/usr/bin/env bash
# Standalone UDP relay; does not install/change the VPN20 application.
set -Eeuo pipefail
umask 077
export DEBIAN_FRONTEND=noninteractive
root=/etc/vpn20-relay
lib=/usr/local/lib/vpn20-relay
unit=vpn20-relay.service
domain=''
control=''
first=''
last=''
limit=''
plugin=''
client=office
email=''
cert=''
key=''
ca=''
source_dir=''
archive=''
helper_hash=74c479d793b44a0981774583a34764b9411d562eacf58da9586dbead186bda50
helper_blob=55d875b81b6b1adbb7895c649f41fdd6447cfc0c
frp_version=0.71.1
work=''
transaction=0
committed=0
previous_active=0
previous_enabled=0
previous_tls_enabled=0
paths=(
  /etc/vpn20-relay
  /usr/local/lib/vpn20-relay
  /usr/local/bin/vpn20-relay
  /etc/systemd/system/vpn20-relay.service
  /etc/systemd/system/vpn20-relay-tls.service
  /etc/systemd/system/vpn20-relay-tls.timer
  /etc/letsencrypt/renewal-hooks/deploy/vpn20-relay
)
die() { echo "VPN20 relay: $*" >&2; exit 1; }
usage() {
  cat <<'EOF'
sudo bash install.sh --domain relay.example.com [options]
  --domain HOST         Domain DNS only trỏ về VPS; hỏi khi chạy ở terminal.
  --control-port PORT   Cổng frpc kết nối ra, mặc định 443/TCP.
  --udp-start PORT      Đầu dải cổng public, mặc định 20001.
  --udp-end PORT        Cuối dải cổng public, mặc định 20100.
  --limit 1MB           Giới hạn mỗi máy VPN20; MB/KB là byte/giây; 0 bỏ giới hạn.
  --client NAME         Tên profile đầu tiên, mặc định office.
  --email EMAIL         Email ACME tùy chọn. Trống: đăng ký không email.
  --cert-file PATH      Dùng chứng chỉ hiện có thay HTTP-01; đi cùng --key-file.
  --key-file PATH       Khóa TLS. Tự đồng bộ khi các tệp nguồn thay đổi.
  --ca-file PATH        CA tin cậy tùy chọn; mặc định bundle CA hệ thống.
  --plugin-port PORT    Cổng chính sách nội bộ, mặc định 18921/loopback.
  --source-dir PATH     Dùng relay.py từ checkout; vẫn kiểm tra SHA-256.
  --frp-archive PATH    Archive frps cục bộ; vẫn kiểm tra SHA-256.
  --help

Ubuntu 22.04/24.04/26.04, Debian 12/13; amd64/arm64; systemd.
Tự cấp/gia hạn Let's Encrypt cần inbound TCP 80 trên VPS.
Mở TCP control-port và dải UDP trong security group của nhà cung cấp.
Chạy lại giữ khóa, profile và cổng đã cấp. Không tự bật/tắt firewall hiện có.
EOF
}
while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h) usage; exit 0;;
    --domain|--control-port|--udp-start|--udp-end|--limit|--client|--email|--cert-file|--key-file|--ca-file|--source-dir|--frp-archive|--plugin-port)
      [[ $# -ge 2 && -n "$2" ]] || die "Thiếu giá trị cho $1"
      case "$1" in
        --domain) domain="$2";; --control-port) control="$2";;
        --udp-start) first="$2";; --udp-end) last="$2";; --limit) limit="$2";;
        --client) client="$2";; --email) email="$2";;
        --cert-file) cert="$2";; --key-file) key="$2";; --ca-file) ca="$2";;
        --source-dir) source_dir="$2";; --frp-archive) archive="$2";;
        --plugin-port) plugin="$2";;
      esac
      shift 2;;
    *) die "Tham số không hợp lệ: $1";;
  esac
done
[[ $(id -u) == 0 ]] || die "Chạy bằng sudo/root."
[[ $(uname -s) == Linux && -f /etc/os-release ]] || die "Cần Linux Ubuntu/Debian."
# shellcheck source=/dev/null
. /etc/os-release
case "$ID:$VERSION_ID" in
  ubuntu:22.04|ubuntu:24.04|ubuntu:26.04|debian:12|debian:13) ;;
  *) die "Hệ điều hành chưa hỗ trợ: $ID $VERSION_ID";;
esac
[[ -d /run/systemd/system ]] || die "Cần systemd đang chạy trên VPS."
command -v flock >/dev/null || die "Cần util-linux (flock)."
exec 9>/run/lock/vpn20-relay-install.lock
flock -n 9 || die "Một bộ cài relay khác đang chạy."
case "$(uname -m)" in
  x86_64) arch=amd64; frp_hash=1d9875210972a5d1a62b2dc4c48df45ab0af05d3ca1b5e494a014428259df803;;
  aarch64|arm64) arch=arm64; frp_hash=8edf590b0c396fa42f9c147121a76af83cfc2002b08f5aef0ea1008c43482019;;
  *) die "Cần VPS amd64 hoặc arm64.";;
esac
if [[ -d "$root" && ! -f "$root/state.json" ]]; then
  [[ -z "$(find "$root" -mindepth 1 -maxdepth 1 -print -quit)" ]] ||
    die "$root có dữ liệu ngoài bộ cài; kiểm tra trước."
fi
cleanup() {
  result=$?
  trap - EXIT
  if [[ "$transaction" == 1 && "$committed" == 0 ]]; then
    echo 'Cài relay chưa thành công; khôi phục cấu hình và dịch vụ trước đó.' >&2
    systemctl stop "$unit" vpn20-relay-tls.timer 2>/dev/null || true
    systemctl disable "$unit" vpn20-relay-tls.timer 2>/dev/null || true
    i=0
    for path in "${paths[@]}"; do
      rm -rf -- "$path"
      if [[ -e "$work/backup/$i" ]]; then
        mkdir -p -- "$(dirname -- "$path")"
        cp -a -- "$work/backup/$i" "$path"
      fi
      i=$((i + 1))
    done
    systemctl daemon-reload || true
    [[ "$previous_enabled" == 0 ]] || systemctl enable "$unit" >/dev/null 2>&1 || true
    [[ "$previous_active" == 0 ]] || systemctl start "$unit" || true
    [[ "$previous_tls_enabled" == 0 ]] || systemctl enable --now vpn20-relay-tls.timer >/dev/null 2>&1 || true
  fi
  [[ -z "$work" ]] || rm -rf -- "$work"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
work="$(mktemp -d /tmp/vpn20-relay.XXXXXXXX)"
echo 'Cài các gói cần thiết...'
apt-get update -qq
apt-get install -y --no-install-recommends ca-certificates curl openssl python3 iproute2 certbot tar
if ! id vpn20-relay >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --shell /usr/sbin/nologin vpn20-relay
fi
if [[ -n "$source_dir" ]]; then
  cp -- "$source_dir/relay.py" "$work/relay.py"
else
  curl --fail --location --silent --show-error --retry 3 --proto '=https' --proto-redir '=https' \
    --header 'Accept: application/vnd.github.raw+json' \
    "https://api.github.com/repos/lehuunghi/vpn/git/blobs/$helper_blob" -o "$work/relay.py"
fi
echo "$helper_hash  $work/relay.py" | sha256sum --check --status ||
  die "SHA-256 của bộ quản lý không khớp; tải lại bộ cài hiện hành."
if [[ -f "$root/state.json" ]]; then
  mapfile -t old < <(python3 - "$root/state.json" <<'PY'
import json,sys
s=json.load(open(sys.argv[1]))
for k in ["domain","control_port","port_start","port_end","default_limit","plugin_port","tls_source","cert_source","key_source","ca_source"]:
    print(s.get(k,""))
PY
  )
  [[ -n "$domain" ]] || domain="${old[0]}"
  [[ -n "$control" ]] || control="${old[1]}"
  [[ -n "$first" ]] || first="${old[2]}"
  [[ -n "$last" ]] || last="${old[3]}"
  [[ -n "$limit" ]] || limit="${old[4]}"
  [[ -n "$plugin" ]] || plugin="${old[5]}"
  if [[ -z "$cert" && "${old[6]}" == external && "$domain" == "${old[0]}" ]]; then
    cert="${old[7]}"; key="${old[8]}"
    [[ -n "$ca" ]] || ca="${old[9]}"
  fi
fi
if [[ -z "$domain" ]]; then
  [[ -r /dev/tty ]] || die "Thêm --domain relay.example.com khi chạy không có terminal."
  read -r -p 'Domain relay (ví dụ relay.20.com.vn): ' domain </dev/tty
fi
control="${control:-443}"; first="${first:-20001}"; last="${last:-20100}"
limit="${limit:-1MB}"; plugin="${plugin:-18921}"
[[ -z "$email" || "$email" =~ ^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$ ]] || die "Email không hợp lệ."
[[ -z "$cert" && -z "$key" || -n "$cert" && -n "$key" ]] || die "--cert-file cần đi cùng --key-file."
install -d -m 0700 "$work/stage"
[[ ! -d "$root" ]] || cp -a "$root/." "$work/stage/"
python3 - "$work/relay.py" "$work/stage" "$domain" "$control" "$first" "$last" "$plugin" "$limit" <<'PY'
import grp,importlib.util,sys
spec=importlib.util.spec_from_file_location("relay",sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
manager=m.Manager(sys.argv[2],grp.getgrnam("vpn20-relay").gr_gid)
s=manager.init(sys.argv[3],*sys.argv[4:9])
print("Domain:",s["domain"],"TCP:",s["control_port"],"UDP:",str(s["port_start"])+"–"+str(s["port_end"]))
PY
domain="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["domain"])' "$work/stage/state.json")"
[[ "$client" =~ ^[a-z0-9][a-z0-9-]{0,39}$ ]] || die "Tên client không hợp lệ."
python3 - "$control" "$plugin" <<'PY'
import re,subprocess,sys
pid=subprocess.run(["systemctl","show","vpn20-relay.service","--property=MainPID","--value"],capture_output=True,text=True).stdout.strip()
allowed={pid} if pid.isdigit() and pid!="0" else set()
if allowed:
    try: allowed.update(open("/proc/"+pid+"/task/"+pid+"/children").read().split())
    except OSError: pass
    cgroup=subprocess.run(["systemctl","show","vpn20-relay.service","--property=ControlGroup","--value"],capture_output=True,text=True).stdout.strip()
    if cgroup.startswith("/") and ".." not in cgroup:
        try: allowed.update(open("/sys/fs/cgroup"+cgroup+"/cgroup.procs").read().split())
        except OSError: pass
for p in sys.argv[1:]:
    output=subprocess.check_output(["ss","-H","-ltnp","sport = :"+p],text=True)
    for line in output.splitlines():
        owners=set(re.findall(r"pid=(\d+)",line))
        if not owners or not owners.issubset(allowed):
            raise SystemExit("TCP "+p+" đang được dịch vụ khác dùng; chọn --control-port/--plugin-port khác.")
PY
if [[ -z "$cert" ]]; then
  [[ -z "$(ss -H -ltn 'sport = :80')" ]] ||
    die "TCP 80 đang được dùng. Dùng --cert-file/--key-file với chứng chỉ do webserver hiện có quản lý."
  python3 - "$domain" <<'PY'
import ipaddress,socket,sys
try:
    addresses={ipaddress.ip_address(x[4][0]) for x in socket.getaddrinfo(sys.argv[1],80,type=socket.SOCK_STREAM)}
except OSError:
    raise SystemExit("Domain chưa phân giải được; trỏ DNS A/AAAA về VPS, chọn DNS only rồi chạy lại.")
if not addresses or any(not a.is_global for a in addresses):
    raise SystemExit("Domain phải phân giải tới IP public của VPS.")
PY
  if command -v ufw >/dev/null && ufw status | grep -q '^Status: active'; then
    ufw allow 80/tcp comment 'VPN20 relay ACME'
  fi
  if command -v firewall-cmd >/dev/null && firewall-cmd --state >/dev/null 2>&1; then
    firewall-cmd --add-port=80/tcp
    firewall-cmd --permanent --add-port=80/tcp
  fi
  cert_args=(certonly --standalone --non-interactive --agree-tos --keep-until-expiring --cert-name "$domain" -d "$domain")
  if [[ -n "$email" ]]; then cert_args+=(--email "$email"); else cert_args+=(--register-unsafely-without-email); fi
  echo "Cấp/kiểm tra SSL cho $domain; cần TCP 80 từ Internet vào VPS..."
  certbot "${cert_args[@]}" || die "Chưa cấp được SSL. Kiểm tra DNS only, A/AAAA, CAA và TCP 80/security group; chạy lại giữ dữ liệu cũ."
  cert="/etc/letsencrypt/live/$domain/fullchain.pem"
  key="/etc/letsencrypt/live/$domain/privkey.pem"
  tls_source=certbot
else
  tls_source=external
  cert="$(realpath -s "$cert")"; key="$(realpath -s "$key")"
fi
ca="${ca:-/etc/ssl/certs/ca-certificates.crt}"
ca="$(realpath -s "$ca")"
python3 - "$work/relay.py" "$work/stage" "$root" "$cert" "$key" "$ca" "$tls_source" "$client" <<'PY'
import grp,importlib.util,pathlib,sys
spec=importlib.util.spec_from_file_location("relay",sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
manager=m.Manager(sys.argv[2],grp.getgrnam("vpn20-relay").gr_gid)
s=manager.state()
s.update(cert_source=sys.argv[4],key_source=sys.argv[5],ca_source=sys.argv[6],tls_source=sys.argv[7])
m.validate_certificate(s["domain"],s["cert_source"],s["key_source"],s["ca_source"])
manager.save(s)
for name,source in [("tls/fullchain.pem",s["cert_source"]),("tls/privkey.pem",s["key_source"]),("ca.pem",s["ca_source"])]:
    m.atomic(manager.root/name,pathlib.Path(source).read_bytes(),gid=manager.gid)
m.atomic(manager.root/"frps.toml",m.Manager(sys.argv[3]).server_config(s),gid=manager.gid)
if sys.argv[8] not in s["clients"]: manager.add(sys.argv[8])
for name in manager.state()["clients"]: manager.profile(name)
PY
if [[ -n "$archive" ]]; then
  cp -- "$archive" "$work/frp.tar.gz"
else
  curl --fail --location --silent --show-error --retry 3 --proto '=https' --proto-redir '=https' \
    "https://github.com/fatedier/frp/releases/download/v$frp_version/frp_$frp_version""_linux_$arch.tar.gz" -o "$work/frp.tar.gz"
fi
echo "$frp_hash  $work/frp.tar.gz" | sha256sum --check --status || die "SHA-256 của frps không khớp."
python3 - "$work/frp.tar.gz" "$work/frps" "frp_$frp_version""_linux_$arch/frps" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1],"r:gz") as t:
    item=t.getmember(sys.argv[3])
    if not item.isfile() or item.size>134217728:raise SystemExit("Archive frps không hợp lệ")
    pathlib.Path(sys.argv[2]).write_bytes(t.extractfile(item).read())
PY
chmod 0755 "$work/frps"
"$work/frps" verify -c "$work/stage/frps.toml"
systemctl is-active --quiet "$unit" && previous_active=1 || true
systemctl is-enabled --quiet "$unit" && previous_enabled=1 || true
systemctl is-enabled --quiet vpn20-relay-tls.timer && previous_tls_enabled=1 || true
install -d -m 0700 "$work/backup"
i=0
for path in "${paths[@]}"; do
  [[ ! -e "$path" ]] || cp -a -- "$path" "$work/backup/$i"
  i=$((i + 1))
done
transaction=1
systemctl stop "$unit" vpn20-relay-tls.timer 2>/dev/null || true
install -d -o root -g vpn20-relay -m 0750 "$root" "$root/tls" "$lib"
cp -a "$work/stage/." "$root/"
chown root:vpn20-relay "$root" "$root/tls"
chmod 0750 "$root" "$root/tls"
install -o root -g root -m 0755 "$work/frps" "$lib/frps"
install -o root -g root -m 0755 "$work/relay.py" "$lib/relay.py"
cat > /usr/local/bin/vpn20-relay <<'EOF'
#!/bin/sh
exec /usr/bin/python3 /usr/local/lib/vpn20-relay/relay.py "$@"
EOF
chmod 0755 /usr/local/bin/vpn20-relay
cat > /etc/systemd/system/vpn20-relay.service <<'EOF'
[Unit]
Description=VPN20 standalone UDP relay
Wants=network-online.target
After=network-online.target
[Service]
User=vpn20-relay
Group=vpn20-relay
ExecStart=/usr/local/bin/vpn20-relay serve
Restart=always
RestartSec=5
TimeoutStopSec=15
KillMode=control-group
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
UMask=0077
[Install]
WantedBy=multi-user.target
EOF
cat > /etc/systemd/system/vpn20-relay-tls.service <<'EOF'
[Unit]
Description=Synchronize VPN20 relay TLS certificate
[Service]
Type=oneshot
ExecStart=/usr/local/bin/vpn20-relay sync-tls
UMask=0077
EOF
cat > /etc/systemd/system/vpn20-relay-tls.timer <<'EOF'
[Unit]
Description=Check VPN20 relay certificate updates
[Timer]
OnBootSec=10min
OnUnitActiveSec=1h
Persistent=true
[Install]
WantedBy=timers.target
EOF
install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
cat > /etc/letsencrypt/renewal-hooks/deploy/vpn20-relay <<'EOF'
#!/bin/sh
set -eu
exec /usr/local/bin/vpn20-relay sync-tls
EOF
chmod 0700 /etc/letsencrypt/renewal-hooks/deploy/vpn20-relay
systemctl daemon-reload
systemctl enable --now "$unit" vpn20-relay-tls.timer
if [[ "$tls_source" == certbot ]]; then systemctl enable --now certbot.timer; fi
if command -v ufw >/dev/null && ufw status | grep -q '^Status: active'; then
  ufw allow "$control/tcp" comment 'VPN20 relay control'
  ufw allow "$first:$last/udp" comment 'VPN20 relay UDP'
fi
if command -v firewall-cmd >/dev/null && firewall-cmd --state >/dev/null 2>&1; then
  for specification in "$control/tcp" "$first-$last/udp"; do
    firewall-cmd --add-port="$specification"
    firewall-cmd --permanent --add-port="$specification"
  done
fi
python3 - "$root" <<'PY'
import json,pathlib,socket,ssl,sys,time
root=pathlib.Path(sys.argv[1]);s=json.loads((root/"state.json").read_text())
ctx=ssl.create_default_context(cafile=str(root/"ca.pem"))
for _ in range(30):
    try:
        with socket.create_connection(("127.0.0.1",s["control_port"]),timeout=2) as raw:
            with ctx.wrap_socket(raw,server_hostname=s["domain"]):break
    except (OSError,ssl.SSLError):
        time.sleep(.3)
else:raise SystemExit("Relay chưa sẵn sàng hoặc TLS không hợp lệ; xem journalctl -u vpn20-relay.")
PY
systemctl is-active --quiet "$unit"
committed=1
echo
echo "Đã cài VPN20 relay 1.0.0: $domain:$control/TCP; UDP $first–$last."
echo "Profile đầu tiên: $root/clients/$client/vpn20-relay-$client.zip"
echo 'Chép ZIP riêng tới máy VPN20 rồi chạy script cài client trong ZIP.'
echo 'Thêm máy: sudo vpn20-relay add vanphong2'
echo 'Danh sách: sudo vpn20-relay list'
echo "Xem log: sudo journalctl -u $unit -n 50 --no-pager"
echo "Security group của nhà cung cấp cần TCP $control và UDP $first–$last."
[[ "$tls_source" != certbot ]] || echo 'Giữ TCP 80 từ Internet vào VPS để gia hạn SSL.'
