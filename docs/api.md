# HTTP API

所有接口以 `/api` 为前缀，请求与响应均为 JSON（上传除外）。除公开接口外，均要求有效的会话 Cookie。

## 约定

### 认证

登录成功后服务端下发 `hibari_session` Cookie（HttpOnly、SameSite=Lax）。后续请求由浏览器自动携带，客户端不需要也不应该读取它的值。

会话令牌是 32 字节随机串，服务端只存 SHA-256 摘要。修改密码会吊销该用户的全部会话。

### 请求格式

- `Content-Type: application/json; charset=utf-8`
- 未知字段会被拒绝（`400 bad_request`），防止拼错字段被静默忽略。
- 请求体上限：普通接口 4–16 KB，上传接口由 `upload_max_mb` 决定。

### 错误格式

```json
{ "error": { "code": "not_found", "message": "路径不存在" } }
```

`code` 是稳定集合，客户端按它分支；`message` 面向人，可能调整。

| code | HTTP | 含义 |
| --- | --- | --- |
| `bad_request` | 400 | 参数非法、路径不合法、JSON 解析失败 |
| `unauthorized` | 401 | 未登录、会话过期、密码错误、提取码错误 |
| `forbidden` | 403 | 跨站请求、路径不在白名单内 |
| `not_found` | 404 | 文件或分享不存在、分享已过期或耗尽 |
| `conflict` | 409 | 同名项已存在、系统已完成初始化 |
| `too_large` | 413 | 上传超过大小上限 |
| `rate_limited` | 429 | 触发限流，响应带 `Retry-After` |
| `unsupported` | 501 | 当前后端不支持该操作 |
| `upstream_failed` | 502 | 上游存储服务异常 |
| `internal` | 500 | 服务内部错误 |

### CSRF

非 `GET`/`HEAD`/`OPTIONS` 请求若带 `Origin` 头且与本站主机不一致，返回 `403 forbidden`。不带 `Origin` 的请求（curl、服务端调用）放行——这类客户端不受 CSRF 影响。

---

## 公开接口

### `GET /api/status`

前端启动时调用一次，决定渲染登录页、初始化向导还是主界面。

```json
{
  "needs_setup": false,
  "version": "1.0.0",
  "backend": "local",
  "caps": { "write": true, "upload": true, "quota": true },
  "authenticated": true,
  "user": { "id": "u-1a2b3c4d", "username": "admin", "role": "admin" }
}
```

`authenticated` 为 `false` 时不返回 `user` 字段。`needs_setup` 为 `true` 时只关心这一项。

### `POST /api/setup`

创建首个管理员。系统已有账号时返回 `409 conflict`，此入口永久关闭。

```json
{ "username": "admin", "password": "至少八个字符" }
```

成功返回 `201` 并自动登录（下发 Cookie）：

```json
{ "user": { "id": "u-1a2b3c4d", "username": "admin", "role": "admin" } }
```

密码长度 8–128 字符，违反返回 `400 bad_request`。

### `POST /api/login`

```json
{ "username": "admin", "password": "..." }
```

成功返回 `200` 与 `{ "user": {...} }`，同时下发会话 Cookie。凭据错误统一返回 `401 unauthorized`，不区分「用户名不存在」与「密码错误」。失败响应会延迟 300 ms，抬高暴力破解成本。

### `POST /api/logout`

删除当前会话并清除 Cookie。幂等：无有效会话时也返回 `200`。

### `POST /api/share/info`

分享落地页查询短链元数据。

```json
{ "token": "a1b2...", "password": "" }
```

需要提取码而未提供（或提供错误）时，仍返回 `200`，但只暴露最小信息：

```json
{ "token": "a1b2...", "has_password": true, "unlocked": false }
```

解锁后：

```json
{
  "token": "a1b2...",
  "kind": "dir",
  "name": "文档",
  "has_password": true,
  "unlocked": true,
  "expires_at": 1767225600
}
```

> 未解锁时不会返回 `kind`、`name`、`expires_at`，避免通过试探提取码获知分享内容。

### `POST /api/share/browse`

列出目录分享的某一层。仅 `kind=dir` 的分享可用。

```json
{ "token": "a1b2...", "password": "", "dir": "/子目录" }
```

`dir` 是相对于分享根目录的路径，`/` 表示分享根本身。响应中的 `path` 同样是相对路径，不暴露服务器真实目录结构：

```json
{
  "dir": "/子目录",
  "entries": [
    { "name": "报告.pdf", "path": "/子目录/报告.pdf", "is_dir": false, "size": 20480, "mod_time": 1767225600 }
  ]
}
```

路径越界（`..`、绝对路径逃逸、白名单外）返回 `400 bad_request`。

### `GET /api/share/{token}`

分享短链的浏览器入口。

- 不带参数：`302` 重定向到落地页 `/s/{token}`（若带 `pwd` 参数则透传）。
- `?download=1`：校验提取码后下载文件。文件分享直接返回内容；目录分享返回 `400 bad_request`，且不消耗使用次数。每次成功下载消耗一次使用次数。

### `GET /api/share/{token}/raw`

访客在分享沙盒内预览或下载单个文件。

| 参数 | 说明 |
| --- | --- |
| `path` | 相对分享根的路径，必填 |
| `pwd` | 提取码，分享设了提取码时必填 |
| `download` | `1` 时以附件形式下载，否则内联展示 |

响应是文件流，`Content-Type` 按扩展名推断。可 seek 的后端支持 `Range` 请求并返回 `206`，浏览器因此能拖动视频进度；不可 seek 的后端整段返回。

只有 `download=1` 消耗一次使用次数。内联预览不计次——一次播放会发出多次 `Range` 请求，计次会让配额瞬间耗尽。目录浏览同样不计次。

---

## 需要登录的接口

### `GET /api/files?dir=/path`

列出目录内容。`dir` 省略时视为 `/`。

```json
{
  "dir": "/文档",
  "entries": [
    { "name": "说明.md", "path": "/文档/说明.md", "is_dir": false, "size": 106, "mod_time": 1767225600 }
  ]
}
```

`mod_time` 是 Unix 秒。结果按白名单过滤，白名单外的兄弟目录不会出现。

### `GET /api/files/raw?path=/path`

读取文件内容。

| 参数 | 说明 |
| --- | --- |
| `path` | 绝对路径，必填 |
| `download` | `1` 时以附件下载，否则内联 |

若后端支持直链（百度），本接口 `302` 到上游地址，浏览器直连；否则由本服务流式代理。

### `GET /api/quota`

```json
{ "total": 107374182400, "used": 20480, "free": 107374161920 }
```

`total` 为 `0` 表示后端没有容量上限（本地磁盘即如此），此时只报告 `used`。后端不声明容量能力时返回 `501 unsupported`。

### `POST /api/files/mkdir`

```json
{ "dir": "/文档", "name": "新建文件夹" }
```

成功返回 `201` 与 `{ "path": "/文档/新建文件夹" }`。同名存在返回 `409 conflict`。

### `POST /api/files/rename`

```json
{ "path": "/文档/旧名.md", "new_name": "新名.md" }
```

成功返回 `200` 与 `{ "path": "/文档/新名.md" }`。`new_name` 不能含 `/`、`\` 或控制字符。

### `POST /api/files/remove`

```json
{ "paths": ["/文档/a.md", "/文档/b.md"] }
```

单次最多 100 项。成功返回 `{ "removed": 2 }`。目录会递归删除，不可撤销。

### `POST /api/upload`

`multipart/form-data`，字段：

| 字段 | 说明 |
| --- | --- |
| `dir` | 目标目录绝对路径 |
| `name` | 文件名（不含路径） |
| `file` | 文件内容 |

成功返回 `201`：

```json
{ "path": "/文档/报告.pdf", "size": 20480 }
```

超过 `upload_max_mb` 返回 `413 too_large`。上传先写临时文件再原子重命名，中断不会留下半个文件。

### `POST /api/shares`

创建分享短链。

```json
{ "path": "/文档", "password": "1234", "ttl_seconds": 604800, "max_uses": 0 }
```

- `kind` 可选，省略时按目标实际类型推断；显式给出时必须与目标一致。
- `password` 留空表示不设提取码。
- `ttl_seconds` 省略时用配置的 `share_ttl_seconds`。
- `max_uses` 为 `0` 表示不限次数。

成功返回 `201`：

```json
{
  "token": "<32 位十六进制串>",
  "kind": "dir",
  "path": "/文档",
  "name": "文档",
  "has_password": true,
  "max_uses": 0,
  "uses": 0,
  "expires_at": 1767225600,
  "created_at": 1767139200
}
```

### `GET /api/shares`

列出所有未过期的分享，结构同上，包在 `{ "shares": [...] }` 中。

### `DELETE /api/shares/{token}`

撤销分享，立即失效。已发出的链接无法再访问。成功返回 `{ "ok": true }`。

### `POST /api/password`

修改当前账号密码。

```json
{ "current_password": "...", "new_password": "..." }
```

需要重新校验当前密码。成功后吊销该用户全部会话并清除 Cookie，前端应跳转登录页。

### `GET /api/audit?limit=100`

返回最近的审计记录，`limit` 上限 500。

```json
{
  "events": [
    {
      "event": "share.create",
      "path": "/文档",
      "client_ip": "203.0.113.x",
      "user_agent": "Mozilla/5.0 ...",
      "created_at": "2026-10-26T15:51:21Z"
    }
  ]
}
```

`client_ip` 已掩码（IPv4 末段、IPv6 后 80 位归零）。记录保留 30 天，由后台任务每小时清理一次。

事件名：`setup`、`login`、`password.change`、`mkdir`、`rename`、`remove`、`upload`、`share.create`、`share.revoke`、`share.download`、`share.browse`。

---

## 运维接口

### `GET /healthz`

```json
{ "status": "ok", "version": "1.0.0", "uptime": 3600 }
```

不需要认证，不限流，供探针使用。`uptime` 为秒。
