# 即刻轻盘

基于**百度网盘青春版**的临时中转网盘。

- **后端**：Go 标准库为主；可选启用状态持久化时使用纯 Go SQLite 驱动（modernc.org/sqlite，无 CGO）
- **前端**：Nuxt 4 单页应用（pnpm 构建，产物 `web/dist` 经 `go:embed` 打包进单二进制，零外部 CDN 依赖）
- **安全**：Cookie 仅服务端托管，HMAC 会话签名防篡改，支持 IP 爆破退避锁定与 CSRF 防护
- **权限**：支持 `access_token` 全局访问保护、共享目录白名单与透明目录短链（`/?d=令牌`）

## 功能特性

- **目录浏览**：支持多层目录深度浏览、自动翻页合并、按名称/大小/时间排序及前进后退
- **本地缓存**：支持目录列表与 README 文本缓存（可存内存或落盘 SQLite/JSON），减少对百度接口的频繁请求并防止限流
- **目录短链**：地址栏自动隐藏真实路径（`/?d=令牌`），方便直接复制分享
- **下载短链**：支持限制有效时长与下载次数，服务端解析直链后 302 重定向
- **文件上传**（默认关闭，`upload_enabled` 开启）：拖拽或选择文件分片上传到青春版网盘（独立空间），展示剩余容量与上传进度，可配置单文件上限与分片大小
- **目录说明**：自动识别目录内的 README 文件并在顶部展示
- **在线预览**：
  - 图片：支持点击放大与前后切换
  - 文本/代码：内联查看并支持行号
  - 视频/音频：自带播放控件，支持进度拖拽与全屏（兼容移动端及 Safari）
- **界面适配**：支持深色模式与移动端布局，按 `/` 键快速聚焦搜索
- **健康检查**：提供 `/healthz`（存活探针）与 `/readyz`（百度会话探测）

## 截图展示

![即刻轻盘界面截图](./docs/images/index.png)

## 快速开始

### 1. 准备配置

```bash
cp config.example.json config.json
```

编辑 `config.json`：至少填写 `baidu_cookie`；公网/VPS 务必设置 `access_token`。

```bash
# 生成高熵访问令牌示例
openssl rand -hex 24
```

> Docker 容器部署时通过环境变量 `BIND_ADDRESS=0.0.0.0` 覆盖监听地址即可。

完整配置项说明见 [docs/configuration.md](docs/configuration.md)。

### 2. 本地运行与构建

后端通过 `go:embed` 嵌入前端产物，首次构建需先生成 `web/dist`：

```bash
# 一键构建（前端 pnpm build + 后端 go build）
bash build.sh
./cmd/bin/main -config config.json
```

前端开发模式（改动热更新）：

```bash
go run ./cmd/jikeqingpan -config config.json   # 后端 :4172
cd frontend && pnpm install && pnpm dev        # 前端 :3000，/api 与 /d 自动代理到后端
```

Windows PowerShell：

```powershell
# 构建前端产物后编译后端
cd frontend; pnpm install; pnpm build; cd ..
go build -o cmd/bin/main.exe ./cmd/jikeqingpan
.\cmd\bin\main.exe -config config.json
```

浏览器访问：`http://127.0.0.1:4172`（直连后端）或开发模式 `http://localhost:3000`

### 3. Docker 部署（推荐）

```bash
cp config.example.json config.json   # 填好 cookie / token
docker compose up -d --build
curl -sS http://127.0.0.1:4172/healthz
```

详细部署指南与反向代理配置见 [docs/deployment.md](docs/deployment.md)。

## 文档索引

| 文档 | 内容说明 |
| --- | --- |
| [docs/configuration.md](docs/configuration.md) | 配置字段、环境变量与默认值 |
| [docs/deployment.md](docs/deployment.md) | Docker Compose / VPS / Nginx / Caddy 反向代理 |
| [docs/api.md](docs/api.md) | HTTP API 规范与下载/短链流程 |
| [docs/security.md](docs/security.md) | 访问控制、CSRF、Cookie 安全与审计日志 |
| [docs/development.md](docs/development.md) | 标准 Go 项目结构、本地调试与测试 |

## 免责声明

1. 本项目仅供个人学习与接口兼容性研究，不对软件的可用性与稳定性作任何担保。
2. 本项目不提供、不存储任何网盘文件，亦不参与任何资源制作或转码。所有文件信息与下载直链均通过使用者自备的百度账号凭据实时获取。
3. 请遵守当地法律法规，严禁将本项目用于商业牟利或侵犯第三方权益。使用中产生的临时缓存请及时清理，涉及版权的内容需自行取得权利方授权。
4. 使用本项目造成的任何直接或间接后果由使用者自行承担，原作者与贡献者概不负责。

## 隐私说明

- **服务纯本地运行**：不收集、不出售任何个人信息，代码中没有任何埋点上报或远程遥测逻辑。
- **凭据自行掌控**：百度 Cookie、访问令牌及缓存数据仅保存在你指定的本地文件（或 SQLite 数据库）中，不向任何第三方传输。

## 开源协议

本项目遵循 [MIT 许可证](LICENSE) 开源。

## 致谢

- [malaohu/jikeqingpan](https://github.com/malaohu/jikeqingpan)：原项目与灵感来源。
- [AList](https://github.com/AlistGo/alist)：百度青春版驱动实现与签名算法参考。
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite)：纯 Go 嵌入式 SQLite 驱动（无 CGO 依赖）。
- [Go](https://go.dev/)：编程语言与标准库生态。

- 仓库地址：<https://github.com/ekishion/jikeqingpan>
