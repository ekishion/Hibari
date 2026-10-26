# 构建前端
FROM node:22-alpine AS web
WORKDIR /src/web
# 固定 pnpm 版本，避免 corepack 随 Node 镜像漂移导致构建行为不一致
RUN corepack enable && corepack prepare pnpm@10.11.0 --activate
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
# 产物直接写到 ../internal/httpapi/assets，供下一步 go:embed 读取
RUN pnpm build

# 构建后端
FROM golang:1.27-alpine AS build
WORKDIR /src
ARG VERSION=dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/httpapi/assets ./internal/httpapi/assets
# 纯 Go SQLite 驱动，无需 CGO
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/hibari ./cmd/hibari

# 运行
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S hibari && adduser -S -G hibari hibari \
    && mkdir -p /data && chown hibari:hibari /data
WORKDIR /data
COPY --from=build /out/hibari /usr/local/bin/hibari

# 默认只监听容器内所有网卡，由端口映射决定对外暴露范围
ENV ADDR=0.0.0.0:4172 \
    DB_PATH=/data/hibari.db \
    LOCAL_ROOT=/data/files

USER hibari
EXPOSE 4172
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:4172/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/hibari"]
