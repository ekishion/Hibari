# 配置

配置来源有两个：`config.json` 文件与环境变量。环境变量优先级更高，容器部署时通常只用环境变量。

配置文件默认路径是启动目录下的 `config.json`，可用 `-config` 参数指定其它位置。文件不存在不算错误——此时所有字段取默认值，服务照常启动并进入初始化向导。

所有字段都可省略，省略即取默认值。

## 示例

完整示例见仓库根目录的 [config.example.json](../config.example.json)。

```json
{
  "addr": "127.0.0.1:4172",
  "backend": "local",
  "local_root": "data/files",
  "db_path": "data/hibari.db",
  "upload_max_mb": 1024,
  "session_ttl_seconds": 604800,
  "share_ttl_seconds": 86400
}
```

## 字段

### 监听

| 字段 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `addr` | `ADDR` | `127.0.0.1:4172` | 监听地址。默认只监听回环，反向代理部署时保持默认即可 |

### 存储后端

| 字段 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `backend` | `BACKEND` | `local` | 只能是 `local` 或 `baidu` |
| `local_root` | `LOCAL_ROOT` | `data/files` | 本地后端对外暴露的根目录，不存在时自动创建 |

一次部署只激活一个后端。切换后端只需改配置并重启，数据库里的账号与分享记录不受影响，但分享路径可能在新后端中不存在。

#### 百度后端专用

| 字段 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `baidu_cookie` | `BAIDU_COOKIE` | 空 | 必填。从浏览器复制的青春版 Cookie |
| `baidu_app_id` | `BAIDU_APP_ID` | `250528` | 上游 app_id，通常不用改 |
| `baidu_base_url` | `BAIDU_BASE_URL` | `https://pan.baidu.com` | 仅测试时改为本地地址；非 https 且非回环地址会被拒绝 |

`baidu_cookie` 是敏感凭据，不要提交到版本库。它只在服务端使用，任何接口都不会把它返回给客户端。

### 数据

| 字段 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `db_path` | `DB_PATH` | `data/hibari.db` | SQLite 文件路径，父目录不存在时自动创建 |

数据库保存账号、会话、分享与审计记录。备份时连同 `-wal`、`-shm` 文件一起复制，或在停止服务后复制。

### 访问控制

| 字段 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `allowed_paths` | — | 空 | 允许访问的路径白名单，空表示不限制 |
| `rate_limit_per_second` | `RATE_LIMIT_PER_SECOND` | `10` | 每 IP 每秒请求数，突发上限为两倍 |
| `trusted_proxies` | — | 空 | 可信代理的 IP 或 CIDR，只有来自这些地址的请求才信任 `X-Forwarded-For` |
| `secure_cookie` | `SECURE_COOKIE` | `false` | 强制给会话 Cookie 加 `Secure`。TLS 直连时会自动开启 |

`allowed_paths` 里的每一项必须是 `path.Clean` 之后的绝对路径，不带末尾斜杠：

```json
{ "allowed_paths": ["/public", "/team/docs"] }
```

白名单只影响浏览与写入范围。根目录 `/` 始终可见，但列出时会逐项过滤，白名单外的条目不会出现。

`trusted_proxies` 只在使用反向代理时需要配置。不配置时，所有请求的来源 IP 都取直连地址，`X-Forwarded-For` 会被忽略——这是安全的默认值，但代理后所有请求看起来都来自代理，限流会误伤。

```json
{ "trusted_proxies": ["127.0.0.1", "10.0.0.0/8"] }
```

### 时长与上限

| 字段 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `session_ttl_seconds` | `SESSION_TTL_SECONDS` | `604800`（7 天） | 登录会话有效期 |
| `share_ttl_seconds` | `SHARE_TTL_SECONDS` | `86400`（1 天） | 新建分享的默认有效期，创建时可覆盖 |
| `upload_max_mb` | `UPLOAD_MAX_MB` | `1024` | 单文件上传上限 |
| `share_max_uses` | `SHARE_MAX_USES` | `0` | 新建分享的默认使用上限，`0` 表示不限 |

## 校验

启动时配置会经过校验，不合法直接退出并打印原因：

- `backend` 不在允许集合内
- `rate_limit_per_second` 小于 1
- `allowed_paths` 含非法路径（非绝对、含 `.` 或 `..`、未规范化）
- `backend=baidu` 且 `baidu_cookie` 为空

## 环境变量覆盖规则

- 字符串直接覆盖，首尾空格被去掉。
- 整数与布尔值解析失败时静默忽略，保留文件里的值。这样容器里拼错变量名不会让服务起不来。
- 环境变量不参与 `allowed_paths` 与 `trusted_proxies`——这两个是列表，用环境变量表达容易出错，只支持配置文件。

## 相对路径

`local_root` 与 `db_path` 的相对路径基于进程工作目录解析，不是基于配置文件所在目录。用 systemd 或 Docker 部署时注意工作目录。
