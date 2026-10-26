# 百度网盘青春版接口观察记录

本文档记录通过浏览器 DevTools 观察百度网盘青春版网页版（`https://pan.baidu.com/youth/pan/main`）网络请求所得到的接口形态。**它是 `internal/backend/baidu` 适配器的唯一实现依据。**

这些接口不是百度官方开放 API，没有公开文档，也没有稳定性承诺。百度可能在任何时候修改参数、增加校验或封禁请求。记录日期见文末。

## 通用约定

所有请求发往 `https://pan.baidu.com`，共享以下特征：

- 认证完全依赖 `Cookie` 请求头，由服务端注入，绝不返回给客户端。
- 公共查询参数：`app_id=250528`、`clienttype=0`、`web=1`。
- 请求头 `Referer: https://pan.baidu.com/`。
- 响应为 JSON，顶层通常含 `errno`（0 表示成功）与 `show_msg` / `errmsg`（错误描述）。
- 部分接口即使成功也可能返回 HTTP 200 且 `errno != 0`，必须解析 `errno` 而非只看状态码。

## 1. 会话凭据（uk / sk）

下载直链的签名需要两个凭据：`uk`（用户数字 ID）与 `sk`（会话密钥）。

### 1.1 首选来源

```
GET /youth/api/user/getinfo?app_id=250528&clienttype=0&web=1&need_selfinfo=1
```

响应中 `records[0].uk` 与 `records[0].sk` 即为所需值。

### 1.2 回退来源

```
GET /api/gettemplatevariable?fields=["bdstoken","uk","sk"]
```

响应 `result.uk` 与 `result.sk`。

### 1.3 二次回退（仅有 uk 时）

```
GET /youth/api/report/user?app_id=250528&clienttype=0&web=1&action=sapi_auth&timestamp=<毫秒>
```

响应 `uinfo` 字段即 `sk`。

### 1.4 缓存策略

`uk`/`sk` 在服务端缓存，默认 TTL 3600 秒。签名失败时清空缓存并重试一次，这是应对 `sk` 提前失效的唯一手段。

## 2. 文件列表

```
GET /youth/api/list?app_id=250528&clienttype=0&web=1&dir=<URL编码的绝对路径>&order=time&desc=1&num=100&page=<页码>
```

- `dir` 为以 `/` 开头的绝对路径，根目录为 `/`。
- `num` 每页条数，固定 100。
- `page` 从 1 开始。响应无明确的「是否还有下一页」标志，需按返回条数是否等于 `num` 判断。
- 响应 `list` 数组，每项主要字段：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `server_filename` | string | 文件名 |
| `path` | string | 完整绝对路径 |
| `isdir` | 0/1 | 是否目录 |
| `size` | number | 字节数，目录为 0 |
| `server_mtime` | number | 修改时间，Unix 秒 |
| `fs_id` | number | 文件唯一 ID，签名需要 |
| `md5` | string | 文件 MD5，签名需要；目录为空 |

**`dlink` 字段必须从响应中剥离后再返回给客户端**，它包含带凭据的下载地址。

## 3. 下载直链

### 3.1 签名算法

两个参数由客户端计算，服务端校验。

```
rand = hex( SHA1( str(uk) + sk + str(nowMilli) + "0" ) )
sign = hex( MD5( fileMD5 + "_" + str(uk) + "_" + str(fsID) + "_" + str(nowMilli) ) )
```

其中 `nowMilli` 为当前毫秒时间戳，两个签名必须使用同一个 `nowMilli`。

> 算法为百度服务端契约的一部分，SHA-1 与 MD5 在此仅用于生成请求参数，不承担任何本项目的数据安全职责。

### 3.2 请求

```
GET /youth/api/locatedownload
    ?app_id=250528
    &clienttype=0
    &web=1
    &devuid=0
    &dp-logid=<纳秒时间戳>
    &path=<URL编码的绝对路径>
    &rand=<rand>
    &sign=<sign>
    &time=<nowMilli>
```

响应 `url` 字段为直链，`errno` 非 0 表示失败。

### 3.3 直链安全校验

返回的 `url` 必须校验：协议为 `https`，主机为 `baidu.com` / `baidupcs.com` / `bdstatic.com` 或其子域。不满足则拒绝使用，避免开放重定向。

### 3.4 缓存

直链按 `(路径, User-Agent)` 缓存，默认 TTL 300 秒。百度直链与请求时的 UA 绑定，换 UA 会失效。

## 4. 容量

```
GET /youth/api/quota?app_id=250528&clienttype=0&web=1&checkfree=1&checkexpire=1
```

响应含 `total`、`used`（字节）。青春版空间与主网盘独立。

## 5. 分片上传

上传分三步，全部使用 `POST`，凭据同样来自 Cookie。

### 5.1 预创建

```
POST /youth/api/precreate
    ?app_id=250528&clienttype=0&web=1
```

表单字段：

| 字段 | 值 |
| --- | --- |
| `autoinit` | `1` |
| `block_list` | 所有分片 MD5 的 JSON 数组，如 `["abc...","def..."]` |
| `isdir` | `0` |
| `path` | 目标绝对路径 |
| `size` | 文件总字节数 |
| `target_path` | 目标所在目录 |

响应 `uploadid` 供后续步骤使用；`return_type == 2` 表示秒传命中，可跳过分片直接创建。

### 5.2 分片上传

```
POST https://d.pcs.baidu.com/rest/2.0/pcs/superfile2
    ?method=upload
    &app_id=250528&clienttype=0&web=1
    &type=tmpfile
    &path=<目标路径>
    &uploadid=<uploadid>
    &partseq=<分片序号，从 0 开始>
```

`multipart/form-data`，字段名 `file`。单分片默认 4 MB，最大 16 MB。

### 5.3 创建文件

```
POST /youth/api/create?app_id=250528&clienttype=0&web=1&isdir=0
```

表单字段：`block_list`、`path`、`size`、`target_path`、`uploadid`。

### 5.4 错误码

| errno | 含义 | 处理 |
| --- | --- | --- |
| `-8` | 目标已存在同名文件 | 返回冲突 |
| `-7` | 参数非法（路径/文件名不被接受） | 返回参数错误 |
| `-6` | 身份校验失败 | 清空会话重试一次 |
| `4` | 容量不足 | 返回配额错误 |

## 6. 文件写操作

青春版网页版自身的写操作（新建目录、重命名、删除、移动、复制）同样走 `/youth/api/*` 路径下的表单接口。由于这些操作在百度侧存在额外风控与频率限制，且参数随版本变动较大，**本适配器不实现写操作**，`Capabilities().Write` 恒为 `false`。写操作请使用 `local` 后端。

这是有意的范围限制，不是待办事项。

## 7. 稳定性风险

- 接口无版本号、无文档、无兼容承诺。
- 请求频率过高会触发风控，表现为 `errno` 异常或直接返回登录页 HTML。适配器必须对响应内容类型做校验。
- Cookie 失效后所有接口返回未登录错误，需要用户重新在后台填写 Cookie。

---

记录方式：浏览器 DevTools → Network → 筛选 XHR，在青春版网页执行对应操作，记录请求 URL、表单字段与响应结构。
