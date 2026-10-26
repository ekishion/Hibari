# 云雀 · Hibari

一个可自托管的临时文件中转站。后端为 Go 单二进制，前端为 Vite + Vue 3 单页应用，两者通过 `go:embed` 打包在一起，无需外部 CDN 或独立前端服务。

名字取自「云雀」——云端之上的一只小雀，衔着文件来去，停一停就走。罗马音 `Hibari` 用作模块名、二进制名与镜像名。

## 核心特性

- **可插拔存储后端**：内置本地磁盘后端，另提供百度网盘青春版适配器。一次部署激活一个后端，安装时选定。
- **单二进制部署**：`go:embed` 内嵌前端产物，无 CGO 依赖，交叉编译即可。
- **分享短链**：文件与目录均可生成带有效期的短链，支持提取码与下载次数限制。
- **目录沙盒**：目录短链访问者被严格限制在授权子树内，无法越界浏览。
- **在线预览**：图片、文本/代码、视频、音频，服务端流式转发，不落盘。
- **路径白名单**：通过 `allowed_paths` 限定可浏览与可写入范围。
- **审计日志**：记录分享创建、访问、登录等关键事件，IP 脱敏存储。

## 快速开始

```bash
# 1. 构建前端
cd web && pnpm install && pnpm build && cd ..

# 2. 编译并运行
go build -o hibari ./cmd/hibari
./hibari
```

首次启动会进入安装向导，在浏览器打开 `http://127.0.0.1:4172/setup` 完成配置。

## 配置

配置通过 `config.json` 或环境变量提供，环境变量优先级更高。完整字段见 [docs/configuration.md](docs/configuration.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/protocol.md](docs/protocol.md) | 百度青春版接口协议观察记录 |
| [docs/api.md](docs/api.md) | HTTP API 规范 |
| [docs/configuration.md](docs/configuration.md) | 配置字段与环境变量 |
| [docs/deployment.md](docs/deployment.md) | Docker 与反向代理部署 |
| [docs/security.md](docs/security.md) | 访问控制与会话模型 |

## 存储后端说明

| 后端 | 状态 | 说明 |
| --- | --- | --- |
| `local` | 默认 | 本地磁盘目录，零外部依赖 |
| `baidu` | 可选 | 百度网盘青春版，**非官方接口** |

`baidu` 适配器通过观察百度网盘青春版网页版的前端请求实现，不属于官方开放 API。使用该后端需要自行承担与百度服务条款相关的风险。项目的默认配置与文档均以本地磁盘后端为准。

## 许可证

[MIT](LICENSE)。第三方组件的许可声明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
