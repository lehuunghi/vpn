#!/usr/bin/env bash
# Install a verified release or an explicitly supplied local build archive.
set -euo pipefail
[[ $(id -u) == 0 ]] || { echo 'Run with sudo/root.' >&2; exit 1; }
[[ $(uname -s) == Linux ]] && command -v systemctl >/dev/null || { echo 'Native installer requires Linux with systemd; use Docker on other hosts.' >&2; exit 1; }
command -v nft >/dev/null || { echo 'Install nftables and wireguard-tools first; see docs/installation.md.' >&2; exit 1; }
command -v python3 >/dev/null || { echo 'Install python3 first.' >&2; exit 1; }
unit="$(dirname "$0")/vpn20.service"
[[ -f "$unit" ]] || { echo 'Run install.sh from the repository, with vpn20.service beside it.' >&2; exit 1; }
archive=''
version=''
while [[ $# -gt 0 ]]; do
  case "$1" in
    --archive) archive="$(realpath "$2")"; shift 2;;
    --version) version="$2"; shift 2;;
    *) echo 'Usage: sudo ./packaging/linux/install.sh [--version 1.0.0 | --archive /path/VPN20-*.tar.gz]' >&2; exit 1;;
  esac
done
case "$(uname -m)" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo 'Native Linux packages require amd64 or arm64.' >&2; exit 1;; esac
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
if [[ -z "$archive" ]]; then
  command -v curl >/dev/null && command -v python3 >/dev/null || { echo 'Install curl, ca-certificates and python3 first.' >&2; exit 1; }
  if [[ -n "$version" ]]; then
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.-]+)?$ ]] || { echo 'Invalid version' >&2; exit 1; }
    endpoint="https://api.github.com/repos/lehuunghi/vpn/releases/tags/v$version"
  else endpoint='https://api.github.com/repos/lehuunghi/vpn/releases/latest'; fi
  curl --fail --silent --show-error --proto '=https' "$endpoint" -o "$work/release.json"
  python3 - "$work/release.json" "$arch" > "$work/asset" <<'PY'
import json,re,sys
r=json.load(open(sys.argv[1]));tag=r['tag_name'];name=f'VPN20-{tag.removeprefix("v")}-linux-{sys.argv[2]}.tar.gz'
for a in r['assets']:
    if a['name']==name:
        url=a['browser_download_url'];digest=a.get('digest','')
        if not url.startswith(f'https://github.com/lehuunghi/vpn/releases/download/{tag}/') or not re.fullmatch('sha256:[a-f0-9]{64}',digest):raise SystemExit('Release has no trusted URL/digest')
        print(url);print(digest[7:]);break
else:raise SystemExit('No Linux package in this release; select a Linux version using --version')
PY
  mapfile -t asset < "$work/asset"
  archive="$work/release.tar.gz"
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' "${asset[0]}" -o "$archive"
  printf '%s  %s\n' "${asset[1]}" "$archive" | sha256sum --check --status || { echo 'Release checksum mismatch.' >&2; exit 1; }
fi
[[ -f "$archive" ]] || { echo 'Archive not found.' >&2; exit 1; }
# Extract known regular files only; never unpack arbitrary archive paths as root.
python3 - "$archive" "$work" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1],'r:gz') as t:
    for name in ['vpn20','LICENSE','NOTICE']:
        member=t.getmember(name)
        if not member.isfile() or member.size>268435456:raise SystemExit('Invalid archive entry')
        pathlib.Path(sys.argv[2],name).write_bytes(t.extractfile(member).read())
PY
chmod 0755 "$work/vpn20"
"$work/vpn20" version
install -d -m 0700 /etc/vpn20 /var/lib/vpn20
if [[ ! -f /etc/vpn20/vpn20.env ]]; then
  cat > /etc/vpn20/vpn20.env <<'ENV'
VPN20_DATA_DIR=/var/lib/vpn20
VPN20_HTTP_LISTEN=0.0.0.0:51821
VPN20_BACKEND=auto
VPN20_UPDATE_MODE=systemd
ENV
  chmod 0600 /etc/vpn20/vpn20.env
fi
systemctl stop vpn20.service 2>/dev/null || true
if [[ -f /usr/local/bin/vpn20 ]]; then
  install -m 0700 /usr/local/bin/vpn20 /var/lib/vpn20/vpn20-before-install
fi
install -m 0755 "$work/vpn20" /usr/local/bin/vpn20
install -d /usr/share/doc/vpn20
install -m 0644 "$work/LICENSE" "$work/NOTICE" /usr/share/doc/vpn20/
install -m 0644 "$unit" /etc/systemd/system/vpn20.service
systemctl daemon-reload
systemctl enable --now vpn20.service
echo 'Installed. Open http://<server-LAN-IP>:51821; allow TCP 51821 from your LAN in the host firewall. Configure the public endpoint and UDP firewall/router.'
