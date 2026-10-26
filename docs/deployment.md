# 部署

## 直接运行

```bash
# 构建（需要 Go 1.27+、Node 20+、pnpm）
cd web && pnpm install && pnpm build && cd ..
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always)" -o hibari ./cmd/hibari

# 运行
./hibari -config /etc/hibari/config.json
```

前端产物通过 `go:embed` 打进二进制，部署时只需一个文件加一个数据目录。

开发期可以跳过嵌入，让 Vite 直接服务前端：

```bash
cd web && pnpm dev          # 终端 1
go run ./cmd/hibari         # 终端 2
```

Vite 在 5173 端口，`/api` 代理到 4172。

## Docker

```bash
docker build -t hibari .
docker run -d \
  --name hibari \
  -p 127.0.0.1:4172:4172 \
  -v hibari-data:/data \
  -e ADDR=0.0.0.0:4172 \
  hibari
```

容器内 `ADDR` 必须改成 `0.0.0.0:4172`，否则端口映射无效。数据目录 `/data` 同时存放数据库与文件，挂载卷即可持久化。

百度后端额外传 Cookie：

```bash
docker run -d --name hibari -p 127.0.0.1:4172:4172 -v hibari-data:/data \
  -e ADDR=0.0.0.0:4172 \
  -e BACKEND=baidu \
  -e BAIDU_COOKIE='你的 Cookie' \
  hibari
```

## systemd

```ini
[Unit]
Description=云雀
After=network.target

[Service]
Type=simple
User=hibari
Group=hibari
WorkingDirectory=/var/lib/hibari
ExecStart=/usr/local/bin/hibari -config /etc/hibari/config.json
Restart=on-failure
RestartSec=5

# 加固：进程只需要读写自己的工作目录
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/hibari

[Install]
WantedBy=multi-user.target
```

`WorkingDirectory` 决定相对路径的解析基准，与 `config.json` 中的 `local_root`、`db_path` 配合使用。

## 反向代理

服务默认只监听 `127.0.0.1`，对外暴露应通过反向代理终止 TLS。

### Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name hibari.example.com;

    ssl_certificate     /etc/ssl/hibari/fullchain.pem;
    ssl_certificate_key /etc/ssl/hibari/privkey.pem;

    # 与 upload_max_mb 保持一致，略留余量
    client_max_body_size 1100m;

    location / {
        proxy_pass http://127.0.0.1:4172;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Real-IP $remote_addr;

        # 大文件上传与下载需要关闭缓冲，否则会占满内存
        proxy_request_buffering off;
        proxy_buffering off;

        # 长传输不能被提前掐断
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
```

同时在 `config.json` 里配置：

```json
{
  "trusted_proxies": ["127.0.0.1"],
  "secure_cookie": true
}
```

不配 `trusted_proxies` 时，服务看到的来源都是 `127.0.0.1`，限流会把所有访客算成同一个 IP；审计日志里的 IP 也失去意义。

`proxy_read_timeout` 是长传输的兜底。服务端本身不设 `WriteTimeout`——它是整个响应的截止时间，会把大文件下载拦腰切断，而它又挡不住慢读客户端。因此「只发请求不收响应」的连接要靠反向代理断开，别把这两行删掉或调到很小。

### Caddy

```
hibari.example.com {
    reverse_proxy 127.0.0.1:4172 {
        flush_interval -1
    }
    request_body {
        max_size 1100MB
    }
}
```

Caddy 默认会传 `X-Forwarded-For`，同样需要在配置里把 Caddy 的地址加入 `trusted_proxies`。Caddy 的默认超时足够宽松，通常不用额外调整；若前面还有一层 CDN，那一层的读超时才需要单独确认。

### Cloudflare

服务下发严格的 CSP，浏览器会拒绝任何非本站脚本。Cloudflare 默认会在边缘往 HTML 里注入两样东西，二者都会被拦下并在控制台报 CSP 违规：

| 功能 | 注入内容 | 关闭位置 |
| --- | --- | --- |
| Rocket Loader | 一段内联 `<script>`，用于延迟加载其他脚本 | Speed → Optimization → Rocket Loader |
| Web Analytics | `static.cloudflareinsights.com/beacon.min.js` | Analytics → Web Analytics |

这两个功能对本服务没有用处（单页应用自己会加载脚本），建议直接关闭。若确实需要 Web Analytics，只能放宽 `script-src` 与 `connect-src`，但那会削弱 XSS 防护，不推荐。

另外 CDN 不应缓存 HTML，否则 CSP 与前端更新都会滞后；`/api/*` 与 `/healthz` 也应绕过缓存。

## 首次启动

1. 启动服务，日志会打印监听地址。
2. 浏览器打开该地址，自动跳转到 `/setup`。
3. 设置管理员用户名与密码（至少 8 个字符）。
4. 提交后自动登录并进入文件页。

初始化入口在创建首个账号后永久关闭，之后访问 `/setup` 会跳转登录页。

## 健康检查

`GET /healthz` 返回 `200` 与运行时长，不需要认证，可用于容器探针：

```yaml
healthcheck:
  test: ["CMD", "wget", "-qO-", "http://127.0.0.1:4172/healthz"]
  interval: 30s
  timeout: 3s
  retries: 3
```

## 备份

需要备份两样东西：

- 数据库：`db_path` 指向的文件，连同 `-wal`、`-shm`。停服务后复制最稳妥。
- 文件：`local_root` 目录（仅本地后端）。

分享链接的令牌存在数据库里，恢复数据库后旧链接继续有效。

## 升级

替换二进制后重启即可。数据库 schema 在启动时自动迁移，每个版本在独立事务中执行，失败即回滚。

升级前建议备份数据库。降级不受支持——旧版本无法识别新版本的 schema 版本号，会拒绝启动。
