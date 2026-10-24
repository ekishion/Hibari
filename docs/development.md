# 开发与测试

本项目采用标准 Go 项目工程布局（Standard Go Project Layout），遵循清晰的模块边界与分层架构。

## 环境要求

- **Go 版本**：建议 Go 1.26.0 或更高版本
- **无需 CGO**：全项目使用纯 Go 开发，SQLite 驱动采用 `modernc.org/sqlite`，直接跨平台编译即可，无需系统 C 依赖
- **无前端构建依赖**：前端直接写原生 HTML / CSS / JS，无需 npm 编译，构建时通过 `go:embed` 直接内嵌

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
│   │   └── baidu.go          # 文件列表翻页、Session 维持、直链签名与抓取
│   └── server/               # HTTP 服务引擎与路由处理器
│       ├── server.go         # 依赖装配、安全中间件管道、静态路由与状态落盘
│       ├── handlers.go       # 文件浏览、下载、分享与撤销 API
│       ├── handlers_auth.go  # 登录、登出与会话状态 API
│       ├── handlers_preview.go # README、文本与图片预览流式转发
│       ├── httputil.go       # HTTP / Cookie / CSRF / IP 工具库
│       ├── pathsec.go        # 路径遍历防护与共享范围圈定
│       └── template.go       # index.html + partials 启动时预渲染为内存整包
├── web/                      # 前端静态资源与 Go 嵌入包
│   ├── embed.go              # //go:embed static/* 导出前端静态资源 FS
│   └── static/               # 前端静态文件（HTML / CSS / JS）
│       ├── index.html        # HTML 外壳：按序注入 partials 模板并引入脚本
│       ├── css/              # 按视觉领域拆分的样式表
│       │   ├── app.css       # 样式入口：@import 汇聚以下各模块
│       │   ├── base.css      # 设计令牌、重置、深色模式与排版
│       │   ├── components.css # 按钮、下拉框、Toast、Spinner 等通用控件
│       │   ├── browser.css   # 文件列表、面包屑、工具栏与搜索
│       │   ├── modals.css    # 登录、创建分享、分享管理、解锁弹窗
│       │   ├── preview.css   # 灯箱、音视频播放器与 Markdown 渲染
│       │   └── landing.css   # 单文件分享落地卡片与提取码输入
│       ├── js/               # 按分层组织的脚本库
│       │   ├── app.js        # 应用入口：鉴权状态、事件绑定与启动调度
│       │   ├── core/         # 底层工具与算法
│       │   │   ├── api.js        # Fetch 封装、CSRF 注入与错误归一化
│       │   │   ├── components.js # Toast、下拉控制器与剪贴板
│       │   │   ├── icons.js      # 内联 SVG 图标注册表
│       │   │   ├── markdown.js   # README 纯 DOM 安全 Markdown 渲染器
│       │   │   └── qrcode.js     # 二维码矩阵生成
│       │   └── modules/     # 业务功能控制器
│       │       ├── file_browser.js # 列表渲染、目录进退、排序、搜索与多选
│       │       ├── preview.js      # 多媒体灯箱视口与播放器控制
│       │       ├── share_modal.js  # 分享创建、二维码与活跃分享管理
│       │       └── share_page.js   # 单文件落地页与提取码校验
│       └── partials/         # 拆分出的 HTML 组件模板片段
│           ├── login_modal.html    # 访问权限验证登录弹窗
│           ├── lightbox.html       # 全屏多媒体与代码灯箱预览器
│           ├── share_modal.html    # 创建分享链接/提取码表单与结果卡片
│           ├── shares_manage.html  # 活跃分享查看与手动撤销管理弹窗
│           ├── unlock_modal.html   # 受限目录访问码解锁弹窗
│           ├── app_main.html       # 主界面：Header、面包屑、工具栏、文件列表
│           └── share_landing.html  # 独立单文件分享落地卡片容器
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

`index.html` 只是外壳，通过 `{{template "xxx" .}}` 按序注入 `partials/` 下的组件。
前端为独立项目 `frontend/`（Nuxt 4 + pnpm，SPA 模式），构建产物输出到 `web/dist/`
并由 `go:embed` 打进单二进制；`internal/server/template.go` 在服务启动时把 index.html
一次性渲染成内存字节切片，`GET /` 直接输出，不产生额外网络请求。

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
