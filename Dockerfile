# syntax=docker/dockerfile:1

ARG NODE_IMAGE=node:22-bookworm-slim
ARG GO_IMAGE=golang:1.27-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

FROM ${NODE_IMAGE} AS frontend-builder

WORKDIR /src/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN NODE_OPTIONS=--max-old-space-size=4096 npm run build


FROM ${GO_IMAGE} AS go-builder

ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org

ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist

RUN CGO_ENABLED=0 GOOS=linux go build \
    -buildvcs=false \
    -tags web \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/go-stock-web \
    .


FROM ${RUNTIME_IMAGE} AS runtime

ARG DEBIAN_MIRROR=https://mirrors.aliyun.com

ENV TZ=Asia/Shanghai \
    GO_STOCK_WEB_ADDR=0.0.0.0:18888 \
    CHROME_BIN=/usr/bin/chromium

COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

RUN sed -i "s|http://deb.debian.org|${DEBIAN_MIRROR}|g" /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Retries=3 update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates \
        chromium \
        curl \
        fonts-noto-cjk \
        sqlite3 \
        tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 go-stock \
    && useradd --uid 10001 --gid go-stock --create-home --home-dir /home/go-stock go-stock \
    && install -d -o go-stock -g go-stock /app/data /app/logs /app/memory /app/skills

WORKDIR /app

COPY --from=go-builder --chown=go-stock:go-stock /out/go-stock-web ./go-stock-web

USER go-stock:go-stock

EXPOSE 18888

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["curl", "--fail", "--silent", "--show-error", "http://127.0.0.1:18888/api/health"]

ENTRYPOINT ["/app/go-stock-web"]
