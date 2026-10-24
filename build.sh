#!/usr/bin/env bash
# 一键构建：先构建前端（frontend → web/dist），再编译嵌入产物的后端单二进制。
set -euo pipefail
cd "$(dirname "$0")"

version=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

echo "==> [1/2] building frontend (pnpm)"
(cd frontend && pnpm install --frozen-lockfile && pnpm build)

echo "==> [2/2] building backend (go)"
mkdir -p cmd/bin
go build -trimpath -ldflags="-s -w -X main.version=${version}" -o cmd/bin/main ./cmd/jikeqingpan

echo "built: cmd/bin/main (version: ${version}, frontend embedded from web/dist)"
