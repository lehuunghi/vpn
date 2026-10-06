# ---- web build ----
FROM node:26-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- go build ----
FROM golang:1.27-alpine AS build
# The version string the binary reports. Worked out by whoever runs the
# build (CI passes the tag); left empty it says "dev".
ARG VPN20_VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/server/static/dist internal/server/static/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/lehuunghi/vpn/internal/engine.Version=${VPN20_VERSION}" -o /vpn20 ./cmd/vpn20

# ---- runtime ----
FROM alpine:3.22
# nftables does the NAT; wireguard-go is the fallback data plane for hosts
# without the kernel module; wireguard-tools gives `wg show` for debugging.
RUN apk add --no-cache nftables wireguard-go wireguard-tools ca-certificates tzdata \
    && mkdir -p /data
COPY --from=build /vpn20 /usr/local/bin/vpn20
COPY LICENSE NOTICE /usr/share/doc/vpn20/
ENV VPN20_DATA_DIR=/data \
    VPN20_HTTP_LISTEN=:51821
LABEL org.opencontainers.image.title="VPN20" \
    org.opencontainers.image.vendor="Công ty TNHH TN20" \
    org.opencontainers.image.url="https://20.com.vn" \
    org.opencontainers.image.source="https://github.com/lehuunghi/vpn" \
    org.opencontainers.image.licenses="AGPL-3.0-or-later"
VOLUME ["/data"]
EXPOSE 51820/udp 51821/tcp
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
    CMD wget -qO- http://127.0.0.1:51821/api/health || exit 1
ENTRYPOINT ["vpn20"]
