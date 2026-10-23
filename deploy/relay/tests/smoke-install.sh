#!/usr/bin/env bash
# Run only on disposable CI runners: installs the actual service and test CA.
set -euo pipefail
[[ $(id -u) == 0 && "${GITHUB_ACTIONS:-}" == true ]] || { echo 'Disposable GitHub Actions runner only'; exit 1; }
source_dir="$(cd -- "$(dirname -- "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export PYTHONDONTWRITEBYTECODE=1
python3 - "$source_dir/tests/test_relay.py" "$work" <<'PY'
import importlib.util,pathlib,sys
spec=importlib.util.spec_from_file_location("fixtures",sys.argv[1])
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
m.certificate(pathlib.Path(sys.argv[2]))
PY
args=(--domain relay.example.test --source-dir "$source_dir"
  --frp-archive "$FRP_ARCHIVE" --cert-file "$work/cert.pem"
  --key-file "$work/key.pem" --ca-file "$work/ca.pem" --password-file "$work/password")
printf '%s\n' 'native-enrollment-fixture-password' > "$work/password"
if [[ "${VPN20_TEST_RELAY_UPGRADE:-0}" == 1 ]]; then
  mkdir "$work/legacy"
  for file in install.sh relay.py; do
    curl --fail --location --silent --show-error --retry 3 \
      "https://raw.githubusercontent.com/lehuunghi/vpn/7cebbdf0f1c6eb39defe97af04c4481a3bcb5532/deploy/relay/$file" -o "$work/legacy/$file"
  done
  echo '40be908872f9ca919972a563604ff649066b76ed7eb9b72e946059e594524daa  '"$work/legacy/relay.py" | sha256sum --check --status
  echo '5c5df1e5f0a7fd5fa5f6a73b20ec9a4e952b67389497b81ae158e4ee50e7dfd2  '"$work/legacy/install.sh" | sha256sum --check --status
  bash "$work/legacy/install.sh" --domain relay.example.test --source-dir "$work/legacy" \
    --frp-archive "$FRP_ARCHIVE" --cert-file "$work/cert.pem" --key-file "$work/key.pem" --ca-file "$work/ca.pem"
  sha256sum /etc/vpn20-relay/transport-token /etc/vpn20-relay/clients/office/credentials.json \
    /etc/vpn20-relay/clients/office/vpn20-relay-office.zip > "$work/legacy-identity"
fi
bash "$source_dir/install.sh" "${args[@]}"
[[ ! -f "$work/legacy-identity" ]] || sha256sum --check "$work/legacy-identity"
systemctl is-active --quiet vpn20-relay.service
systemctl is-enabled --quiet vpn20-relay.service
systemctl is-enabled --quiet vpn20-relay-tls.timer
test "$(systemctl show vpn20-relay.service -p User --value)" = vpn20-relay
# Legacy exports remain supported for already deployed 1.3.0 clients only.
if [[ ! -f /etc/vpn20-relay/clients/office/credentials.json ]]; then vpn20-relay add office >/dev/null; fi
test "$(stat -c '%a' /etc/vpn20-relay/clients/office/vpn20-relay-office.zip)" = 600
FRPC="$FRPC" python3 "$source_dir/tests/probe_installed.py"
sha256sum /etc/vpn20-relay/transport-token /etc/vpn20-relay/state.json \
  /etc/vpn20-relay/clients/office/credentials.json /etc/vpn20-relay/enrollment.json \
  /var/lib/vpn20-relay/clients.db > "$work/identity"
# Repeat without re-supplying domain or certificate settings.
bash "$source_dir/install.sh" --source-dir "$source_dir" --frp-archive "$FRP_ARCHIVE"
sha256sum --check "$work/identity"
FRPC="$FRPC" python3 "$source_dir/tests/probe_installed.py"
before="$(sha256sum /etc/vpn20-relay/state.json)"
if bash "$source_dir/install.sh" "${args[@]}" --key-file "$work/ca.key" > "$work/rejected" 2>&1; then
  echo 'Installer accepted a mismatched key'; exit 1
fi
test "$before" = "$(sha256sum /etc/vpn20-relay/state.json)"
systemctl is-active --quiet vpn20-relay.service
if bash "$source_dir/install.sh" "${args[@]}" --udp-start 20100 > "$work/rejected" 2>&1; then
  echo 'Installer discarded allocated ports'; exit 1
fi
test "$before" = "$(sha256sum /etc/vpn20-relay/state.json)"
# Fail exactly one activation; real systemd still performs all operations.
mkdir "$work/bin"
real_systemctl="$(command -v systemctl)"
cat > "$work/bin/systemctl" <<EOF
#!/bin/bash
if [[ "\$*" == "enable --now vpn20-relay.service vpn20-relay-tls.timer" && ! -e "$work/failed-once" ]]; then
  touch "$work/failed-once"
  exit 1
fi
exec "$real_systemctl" "\$@"
EOF
chmod 0755 "$work/bin/systemctl"
if PATH="$work/bin:$PATH" bash "$source_dir/install.sh" "${args[@]}" > "$work/rollback" 2>&1; then
  echo 'Expected activation failure'; exit 1
fi
test -e "$work/failed-once"
test "$before" = "$(sha256sum /etc/vpn20-relay/state.json)"
systemctl is-active --quiet vpn20-relay.service
vpn20-relay sync-tls
test -x /etc/letsencrypt/renewal-hooks/deploy/vpn20-relay
python3 - "$work" <<'PY' &
import pathlib,socket,sys,time
s=socket.socket();s.bind(("0.0.0.0",44444));s.listen()
pathlib.Path(sys.argv[1],"listener-ready").touch()
time.sleep(45)
PY
listener=$!
for _ in $(seq 1 30); do [[ -e "$work/listener-ready" ]] && break; sleep .1; done
if bash "$source_dir/install.sh" "${args[@]}" --control-port 44444 > "$work/rejected" 2>&1; then
  kill "$listener"; echo 'Installer took over an unrelated port'; exit 1
fi
kill -0 "$listener"
kill "$listener"
wait "$listener" 2>/dev/null || true
systemctl is-active --quiet vpn20-relay.service
echo 'Native install, rerun, preservation, authorization, TLS, conflicts and rollback PASS'
