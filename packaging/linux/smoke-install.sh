#!/usr/bin/env bash
# Disposable CI host only: exercises a real systemd install and update helper.
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true && $(id -u) == 0 ]] || { echo 'This test is for an elevated disposable GitHub Actions runner.' >&2; exit 1; }
[[ ! -e /usr/local/bin/vpn20 && ! -e /var/lib/vpn20 ]] || { echo 'Refusing to change an existing installation.' >&2; exit 1; }
archive="$(realpath "$1")"
cleanup() {
  systemctl disable --now vpn20.service >/dev/null 2>&1 || true
  rm -f /etc/systemd/system/vpn20.service /usr/local/bin/vpn20
  systemctl daemon-reload
}
trap cleanup EXIT
install -d -m 0700 /etc/vpn20
cat > /etc/vpn20/vpn20.env <<'ENV'
VPN20_DATA_DIR=/var/lib/vpn20
VPN20_HTTP_LISTEN=127.0.0.1:51821
VPN20_BACKEND=mock
VPN20_MANAGE_FIREWALL=false
VPN20_MANAGE_SYSCTL=false
VPN20_UPDATE_MODE=systemd
ENV
chmod 0600 /etc/vpn20/vpn20.env
bash packaging/linux/install.sh --archive "$archive"
set -a
source /etc/vpn20/vpn20.env
set +a
for attempt in {1..40}; do /usr/local/bin/vpn20 healthcheck && break || sleep 0.5; done
/usr/local/bin/vpn20 healthcheck
work="/var/lib/vpn20/updates"
mkdir -p "$work"
curl --fail --silent --show-error -c "$work/cookies" -H 'Content-Type: application/json' -d '{"username":"releaseadmin","password":"integration-test-password-only","endpointHost":"vpn.example.com"}' http://127.0.0.1:51821/api/setup > /dev/null
curl --fail --silent --show-error -b "$work/cookies" -H 'Content-Type: application/json' -d '{"name":"retained device"}' http://127.0.0.1:51821/api/peers > /dev/null
for outcome in success rollback; do
  if [[ "$outcome" == success ]]; then
    id=111111111111111111111111
    version=1.1.1-integration
  else id=222222222222222222222222; version=1.1.2-bad; fi
  job="$work/$id"
  mkdir -p "$job/stage"
  if [[ "$outcome" == success ]]; then
    CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w -X github.com/lehuunghi/vpn/internal/engine.Version=$version" -o "$job/stage/vpn20" ./cmd/vpn20
  else printf 'not an executable\n' > "$job/stage/vpn20"; fi
  tar -C "$job/stage" -czf "$job/update.tar.gz" vpn20
  python3 - "$job" "$id" "$version" <<'PY'
import hashlib,json,pathlib,sys
d=pathlib.Path(sys.argv[1]);payload=d/'update.tar.gz'
plan={'Job':{'id':sys.argv[2],'phase':'installing','version':sys.argv[3]},'DataDir':'/var/lib/vpn20','DBPath':'/var/lib/vpn20/vpn20.db','Method':'systemd','Version':sys.argv[3],'Executable':'/usr/local/bin/vpn20','Payload':str(payload),'Digest':'sha256:'+hashlib.sha256(payload.read_bytes()).hexdigest(),'HTTPURL':'http://127.0.0.1:51821/api/health'}
(d/'plan.json').write_text(json.dumps(plan))
PY
  chmod 0600 "$job/plan.json"
  systemd-run --unit="vpn20-update-smoke-$id" --wait --collect /usr/local/bin/vpn20 apply-update "$job/plan.json"
  python3 - "$work/state.json" "$outcome" <<'PY'
import json,sys
s=json.load(open(sys.argv[1]));expected={'success':'complete','rollback':'rolled_back'}[sys.argv[2]]
assert s['phase']==expected,s
PY
  /usr/local/bin/vpn20 healthcheck
  curl --fail --silent --show-error -c "$work/cookies" -H 'Content-Type: application/json' -d '{"username":"releaseadmin","password":"integration-test-password-only"}' http://127.0.0.1:51821/api/auth/login > /dev/null
  curl --fail --silent --show-error -b "$work/cookies" http://127.0.0.1:51821/api/peers > "$work/peers.json"
  python3 - "$work/peers.json" <<'PY'
import json,sys
p=json.load(open(sys.argv[1]));assert len(p)==1 and p[0]['name']=='retained device',p
PY
done
echo 'PASS: real Linux systemd installation, complete binary/UI upgrade, retained account/device, failed-update rollback and healthcheck'
