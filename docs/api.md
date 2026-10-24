# HTTP API 与下载流程

除特别说明外，启用 `access_token` 后，API 与短链下载均需已登录。

鉴权方式（任选其一）：

- 浏览器：登录后的 HttpOnly 会话 Cookie `jkqp_session`（HMAC 签名、内嵌过期，非 token 原文）
- 编程式客户端：请求头 `Authorization: Bearer <token>` 或 `X-Access-Token: <token>`

> 不接受 query 传 token，避免进访问日志。Cookie 中也不再存放 token 原文。

状态变更类 `POST` 需要 CSRF：

- Cookie：`csrf_token`（前端可读）
- 请求头：`X-CSRF-Token` 与 Cookie 值一致

## 下载流程

1. （可选）登录
2. 前端 `POST /api/download` 申请短链
3. 服务端校验文件后返回 `/d/{token}`
4. 浏览器打开短链；服务端按当前 User-Agent 解析/缓存百度直链并 `302`

真实文件路径不会出现在下载 URL 中。

## 鉴权

### 状态

```http
GET /api/auth/status
```

响应还会返回前端展示配置：`show_readme` 和 `show_readme_overview`，分别控制目录 README 和右侧目录概览。

### 登录

```http
POST /api/login
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"token":"<access_token>"}
```

成功返回 `{"ok":true}` 并下发签名会话 Cookie `jkqp_session`。
令牌错误返回 `401 invalid_token`；同一 IP 连续失败会触发指数退避锁定，返回 `429 login_locked` 并带 `Retry-After`（秒）。

### 退出

```http
POST /api/logout
X-CSRF-Token: <csrf_token>
```

## 文件列表

```http
POST /api/files
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"dir": "/", "refresh": false}
```

说明：

- 兼容 `GET /api/files?dir=/&refresh=0`
- 也可用目录短链定位：`{"token": "<32位hex>"}`（与 `dir` 二选一），见 [目录短链](#目录短链)
- **本地缓存与穿透**：默认命中本地目录缓存，响应头返回 `X-Cache: HIT`（未命中或回源返回 `X-Cache: MISS`）；传 `{"refresh": true}` 或 query `?refresh=1` 可强制绕过缓存回源百度并更新本地缓存
- 响应中的 `dlink` 会在返回前端前剥离
- 响应附带 `resolved_dir` 字段（当前列表对应的真实目录，token 请求时前端据此还原路径）
- 自动翻页合并（每页 100，最多约 15 页，可用 `list_max_pages` 调整）
- 若截断会带 `truncated: true`、`list_pages` 等字段

## 目录短链

浏览目录时前端自动把地址栏换为短链形式（`/?d=令牌`），隐藏真实路径；复制地址栏即得分享链接。也可按需创建：

```http
POST /api/dir-link
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"dir": "/Music/Asmr"}
```

响应：

```json
{
  "token": "9f86d081884c7d659a2feaa0c55ad015",
  "url": "/?d=9f86d081884c7d659a2feaa0c55ad015"
}
```

- 令牌 32 位 hex，有效期默认 7 天（`dir_link_ttl_seconds`）
- **隐藏的是路径，不是权限**：打开短链仍需登录，目录权限在使用时按 `allowed_paths` 重新校验
- 配置 `state_backend`（`sqlite`）时，目录短链跨重启保留；未配置时重启失效
- 文件列表接口接受 `{"token": "..."}`，响应中的 `resolved_dir` 为解析出的真实目录
- 常见错误：`dirlink_failed`、`dirlink_not_found`（令牌过期或失效）

### 目录 README

前端会识别当前目录中的 `README.md`、`README.markdown`、`README.txt` 或 `README`，并在文件列表上方安全渲染。接口由前端按需调用：

```http
POST /api/readme
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/示例目录/README.md"}
```

- 优先级为 `README.md`、`README.markdown`、`README.txt`、`README`
- 接口只接受上述四种 README 文件名；内容上限为 512 KB，超出后不展示
- 支持标题、段落、列表、引用、代码、链接等常用 Markdown；原始 HTML 和脚本不会执行
- **内容缓存**：已读取的 README 文本自动在后端缓存（TTL 与 `file_cache_ttl_seconds` 一致，响应头同样返回 `X-Cache: HIT/MISS`）；对应目录刷新时自动失效缓存重拉

## 下载短链

### 申请

```http
POST /api/download
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/示例目录/文件.zip"}
```

响应示例：

```json
{
  "urls": [
    { "url": "/d/0123456789abcdef0123456789abcdef" }
  ]
}
```

### 访问短链

```http
GET /d/{token}
```

- token 为 32 位 hex
- 可配置 TTL 与最大使用次数
- 启用鉴权时，访问短链同样需要已登录

### 图片预览

图片文件由前端点击“预览”后请求，服务端读取图片并以 `inline` 响应，供站内灯箱显示：

```http
POST /api/preview
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/图片目录/example.jpg"}
```

- 仅允许图片 MIME 类型；服务端同时结合文件内容和扩展名识别类型
- 单张图片默认最大 16 MB（`preview_max_mb` 可调，单位 MB）
- 预览内容会经过 VPS，因此会产生 VPS 出站流量；普通下载仍使用百度直链重定向
- 常见错误：`preview_link_failed`、`preview_fetch_failed`、`preview_not_image`、`preview_too_large`

### 文本预览

文本/代码文件在灯箱中查看：

```http
POST /api/text
Content-Type: application/json
X-CSRF-Token: <csrf_token>

{"path": "/代码目录/main.go"}
```

- 仅接受扩展名白名单：`txt` `md` `markdown` `json` `xml` `yaml` `yml` `csv` `log` `ini` `conf` `sh` `bat` `ps1` `py` `js` `ts` `go` `c` `h` `cpp` `hpp` `java` `rs` `sql` `toml` `html` `css`
- 内容上限 512 KB；超出后按 UTF-8 字符边界截断并返回 `truncated: true`
- 非 UTF-8（二进制）内容返回 `415 text_not_text`
- 响应示例：

```json
{
  "found": true,
  "name": "main.go",
  "content": "package main\n...",
  "truncated": false
}
```

- 常见错误：`text_not_allowed`、`text_not_text`、`text_link_failed`、`text_fetch_failed`

### 音视频播放与直接下载

`<video>` / `<audio>` 预览与文件行的“下载”按钮（含双击下载）共用同一个无状态中转接口。前端先带 `X-Stream-Probe` 头探测一次以获得可读错误（不解析直链），通过后再把该 URL 作为媒体 `src` 或触发浏览器下载：

```http
GET /api/stream?path=%2F%E7%A4%BA%E4%BE%8B%E7%9B%AE%E5%BD%95%2Fclip.mp4
X-Stream-Probe: 1
```

- 服务端校验路径与共享范围后 `302` 到百度直链，浏览器自动透传 `Range` 头，支持拖动进度条
- 媒体流量**不经过 VPS 中转**（浏览器直连百度直链；CSP `media-src` 已放行相应域名）
- **不创建任何短链**：不写入短链表、不落持久化状态、不出现在“分享管理”列表。在线预览与直接下载都不会产生对外可分享的链接，只有分享弹窗显式创建分享才会铸造 `/d/{token}`
- 需要登录（与图片/文本预览一致）；携带 `X-Stream-Probe` 的探测请求会记录 `stream_accessed` 审计事件（每个用户动作恰好一条，避免播放中拖动进度条的分段请求刷满日志）
- 常见错误：`invalid_path`、`path_not_allowed`、`file_not_found`、`stream_link_failed`

## 上传与容量

上传功能默认关闭，需在配置中设置 `upload_enabled: true`（或环境变量 `UPLOAD_ENABLED`）开启；开启后前端工具栏才会出现“上传”按钮。上传走**百度网盘青春版接口**（`/youth/api/*`），消耗的是青春版独立空间的容量（与主网盘空间不互通），可配合 `allowed_paths` 限定可上传目录。

### 网盘容量

```http
GET /api/quota
```

响应示例：

```json
{ "total": 1099511627776, "used": 322122547200, "free": 777389080576 }
```

- 字段单位均为字节：`total` 总容量、`used` 已用、`free` 剩余（青春版独立空间）
- 需要登录；前端在上传弹窗中展示，并在每次上传成功后刷新

### 上传文件

```http
POST /api/upload
Content-Type: multipart/form-data; boundary=…
X-CSRF-Token: <csrf_token>
```

multipart 表单字段按序：

1. `dir` — 目标目录（明文路径，如 `/示例目录`）
2. `size` — 文件字节数（前端从 `File.size` 取得）
3. `file` — 文件内容（每次只接受一个文件）

响应示例：

```json
{ "ok": true, "path": "/示例目录/文件.zip", "size": 1048576 }
```

行为说明：

- 服务端完整校验目录与文件名（拒绝路径穿越、控制字符），目标路径同时受 `allowed_paths` 约束
- 单文件上限 `upload_max_mb`（MB，`0` 表示不限），超出返回 `413 upload_too_large`；该上限同时作为服务端请求体硬限
- 网页版上传契约要求先提交全部分片 MD5（precreate）再逐片上传（superfile2）后提交（create），因此服务端会把请求体**先落临时文件再转发**，结束后立即删除；内存只保留一个分片缓冲
- 上传前会做容量预检（`free < size` 直接拒绝）；同名文件已存在返回 `409 file_exists`（可通过预检或百度侧校验触发）
- 目标目录内容变化后，该目录的本地列表缓存自动失效
- 审计事件：成功记录 `upload_completed`
- 常见错误：`upload_disabled`、`upload_too_large`、`size_mismatch`、`invalid_path`、`path_not_allowed`、`dir_not_found`、`file_exists`、`quota_exceeded`、`upload_failed`

## 健康检查

| 路径 | 鉴权 | 说明 |
| --- | --- | --- |
| `GET /healthz` | 否 | 进程存活（同时支持 `HEAD`） |
| `GET /readyz` | 是（若启用 access_token） | 探测百度 Session |

## 错误响应格式

```json
{
  "error": {
    "code": "unauthorized",
    "message": "需要访问令牌"
  }
}
```

常见 code：`unauthorized`、`csrf_invalid`、`invalid_path`、`path_not_allowed`、`rate_limited`、`baidu_list_failed`、`dlink_failed`、`shortlink_not_found`、`dirlink_not_found`、`readme_not_found`、`readme_link_failed`、`readme_too_large`、`readme_not_text`、`preview_not_image`、`preview_too_large`、`text_not_allowed`、`text_not_text`、`text_link_failed`、`text_fetch_failed`、`stream_link_failed`、`upload_disabled`、`upload_too_large`、`size_mismatch`、`dir_not_found`、`file_exists`、`quota_exceeded`、`upload_rejected`、`upload_failed` 等。
