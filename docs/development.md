# 开发与测试

本项目采用标准 Go 项目工程布局（Standard Go Project Layout），遵循清晰的模块边界与分层架构。

## 环境要求

- **Go 版本**：建议 Go 1.26.0 或更高版本
- **无需 CGO**：全项目使用纯 Go 开发，SQLite 驱动采用 `modernc.org/sqlite`，直接跨平台编译即可，无需系统 C 依赖
- **前端构建**：前端为 Nuxt 4 项目（`frontend/`，pnpm 管理），构建产物输出到 `web/dist` 由 `go:embed` 内嵌，需要 Node 22 + pnpm

## 项目结构

后端按领域拆分为单向无环依赖的 Go 包，前端按功能拆分为分层目录。

```text
.
├── cmd/
│   └── jikeqingpan/          # 应用可执行文件入口
│       └── main.go           # CLI 参数解析、配置装载、优雅停机与服务启动
├── internal/                 # Go 私有业务包（禁止外部导入）
│   ├── config/               # 配置装载、环境变量覆盖与风险校验（零内部依赖）
│   │   ├── config.go         # 配置结构体、加载与归一化校验
│   │   └── config_warn.go    # 有风险但不阻断的配置告警与写超时推导
│   ├── security/             # 安全防护组件
│   │   ├── audit.go          # 审计日志（IP 脱敏、有界滚动）
│   │   ├── evict.go          # 通用近似 LRU 采样淘汰算法
│   │   ├── loginguard.go     # 登录失败 IP 锁定与指数退避防护
│   │   ├── ratelimit.go      # 基于 IP 的令牌桶限流器
│   │   └── session.go        # HMAC-SHA256 会话签名与无状态校验
│   ├── storage/              # 数据存储、缓存与状态持久化
│   │   ├── cache.go          # 目录与文件元数据多级缓存
│   │   ├── dirlink.go        # 目录短链（?d=令牌）管理与过期清理
│   │   ├── persist.go        # SQLite / JSON 状态持久化后端驱动
│   │   ├── shortlink.go      # 下载短链存储、提取码与使用次数
│   │   └── evict.go          # 通用近似 LRU 采样淘汰算法
│   ├── baidu/                # 百度网盘青春版上游客户端封装
│   │   ├── baidu.go          # HTTP 基础客户端、签名请求与错误归一化
│   │   ├── list.go           # 文件列表翻页与文件元数据缓存
│   │   ├── session.go        # Session（uk/sk）引导与维持
│   │   ├── dlink.go          # 下载直链签名与获取
│   │   └── upload.go         # 分片上传（precreate/superfile2/create）
│   └── server/               # HTTP 服务引擎与路由处理器
│       ├── server.go         # 依赖装配与 HTTP Server 生命周期
│       ├── routes.go         # 路由注册与安全选项（鉴权 / CSRF / 限流）
│       ├── middleware.go     # 安全中间件：CSRF、鉴权、限流与安全响应头
│       ├── handlers_files.go    # 文件列表与目录短链沙盒 API
│       ├── handlers_download.go # 下载短链创建与 /d/ 访问
│       ├── handlers_shares.go   # 分享元数据、分享管理与撤销 API
│       ├── handlers_preview.go  # 文本/图片预览与流媒体中转
│       ├── handlers_upload.go   # 分片上传与配额 API
│       ├── handlers_auth.go     # 登录、登出与会话状态 API
│       ├── pathsec.go        # 路径遍历防护与共享范围圈定
│       ├── staticfiles.go    # 静态资源服务（gzip、不可变资源缓存）
│       ├── template.go       # index.html 启动时预渲染为内存整包
│       ├── persistence.go    # 状态持久化调度
│       └── httputil.go       # HTTP / Cookie / CSRF / IP 工具库
├── frontend/                 # Nuxt 4 前端项目（pnpm 管理，SPA 模式 ssr:false）
│   ├── nuxt.config.ts        # 构建配置：产物输出 ../web/dist，devProxy 代理后端
│   ├── app/                  # 页面、组件与 composables（按功能分层）
│   │   ├── components/       # 文件列表、弹窗、工具栏、落地页等视图组件
│   │   ├── composables/      # useAuth / useFiles / usePreview / useUpload 等状态层
│   │   └── assets/css/       # 按视觉领域拆分的样式表（base/components/browser…）
│   └── package.json          # 依赖与构建脚本（packageManager 锁定 pnpm 版本）
├── web/                      # Go 嵌入包与前端构建产物
│   ├── embed.go              # //go:embed all:dist 导出前端产物 FS
│   └── dist/                 # pnpm build 生成的产物（不入库）
├── docs/                     # 完整开发、配置与部署文档
├── .github/
│   └── workflows/
│       ├── ci.yml            # 代码规范、前端语法检查与跨平台编译矩阵
│       └── docker.yml        # 多架构 Docker 镜像自动化发布
├── Dockerfile                # 多阶段极简 Alpine 无根镜像构建
├── docker-compose.yml        # Docker Compose 服务编排模板
├── build.sh                  # 自动化编译发布脚本
├── config.example.json       # 服务配置示例文件
├── .env.example              # 环境变量配置示例
├── go.mod                    # Go 依赖与版本定义
└── go.sum
```

包依赖方向（严格单向，无环）：

```text
cmd/jikeqingpan
      │
      ├──> internal/config
      │
      └──> internal/server
              ├──> internal/config
              ├──> internal/security
              ├──> internal/storage
              └──> internal/baidu
```

### 首页模板预渲染

前端为独立项目 `frontend/`（Nuxt 4 + pnpm，SPA 模式），构建产物输出到 `web/dist/`
并由 `go:embed` 打进单二进制；`internal/server/template.go` 在服务启动时把构建产物的
index.html 一次性渲染成内存字节切片，`GET /` 直接输出，不产生额外网络请求。

## 前端开发（frontend/）

```bash
cd frontend
pnpm install
pnpm dev        # 开发服务器：/api、/d 自动代理到本地后端 127.0.0.1:4172
pnpm build      # 构建产物输出到 ../web/dist
```

先在仓库根启动后端再跑 `pnpm dev`，浏览器访问 Nuxt 给出的开发地址即可热更新。

## 本地后端调试

```bash
cp config.example.json config.json
# 编辑 config.json 填入 baidu_cookie 等必要参数

go run ./cmd/jikeqingpan -config config.json
```

Windows PowerShell：

```powershell
go run ./cmd/jikeqingpan -config config.json
```

后端启动时嵌入 `web/dist`；改动前端后需要 `pnpm build` 并重启 go 进程。

## 自动化测试与检查

```bash
# 代码格式检查
gofmt -l .

# 静态代码分析
go vet ./...

# 编译验证（需先构建前端产物）
bash build.sh

# 运行单元测试
go test ./...
```

## 常用开发环境变量

```bash
export CONFIG_PATH=config.json
export PORT=4172
export BIND_ADDRESS=127.0.0.1
# export BAIDU_COOKIE='...'
# export ACCESS_TOKEN='...'
```

## 杀毒软件提示

部分杀毒软件可能对 Go 编写的网络工具产生启发式误报。在本地开发时，建议将项目工作区与 Go SDK 目录加入排除项列表。
