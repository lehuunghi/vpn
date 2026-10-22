#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
version="${VPN20_VERSION:-$(tr -d '\r\n' < VERSION)}"
arch="${1:-$(go env GOHOSTARCH)}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][a-zA-Z0-9.-]+)?$ ]] || { echo 'Invalid version' >&2; exit 1; }
case "$arch" in amd64|arm64) ;; *) echo 'Supported Linux packages: amd64, arm64' >&2; exit 1;; esac
stage="dist/linux-$arch"
mkdir -p "$stage/licenses" dist/release
(cd web && npm ci --ignore-scripts --no-audit --no-fund && npm run test:locale && npm run build)
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="-s -w -X github.com/lehuunghi/vpn/internal/engine.Version=$version" -o "$stage/vpn20" ./cmd/vpn20
cp LICENSE NOTICE "$stage/"
printf 'Corresponding source: https://github.com/lehuunghi/vpn/tree/%s\n' "$(git rev-parse HEAD)" > "$stage/SOURCE.txt"
cp docs/installation.md "$stage/INSTALL.md"
while IFS='|' read -r module directory; do
  [[ -d "$directory" ]] || continue
  target="$stage/licenses/${module//\//_}"
  for file in "$directory"/LICENSE* "$directory"/COPYING* "$directory"/NOTICE*; do
    [[ -f "$file" ]] || continue
    mkdir -p "$target"
    cp "$file" "$target/"
  done
done < <(go list -m -f '{{.Path}}|{{.Dir}}' all)
python3 - "$stage/licenses" <<'PY'
import pathlib,shutil,sys
out=pathlib.Path(sys.argv[1])/'npm'
for p in pathlib.Path('web/node_modules').rglob('*'):
    if p.is_file() and p.name.upper().startswith(('LICENSE','COPYING','NOTICE','COPYRIGHT')):
        dest=out/p.relative_to('web/node_modules');dest.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(p,dest)
PY
# The updater uses the exact root filename, without an alternate extraction path.
tar -C "$stage" --owner=0 --group=0 -czf "dist/release/VPN20-$version-linux-$arch.tar.gz" vpn20 LICENSE NOTICE SOURCE.txt INSTALL.md licenses
(cd dist/release && sha256sum "VPN20-$version-linux-$arch.tar.gz" > "SHA256SUMS-linux-$arch.txt")
