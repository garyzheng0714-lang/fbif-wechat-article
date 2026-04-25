# fbif-wechat-article

![类型：微信工具](https://img.shields.io/badge/%E7%B1%BB%E5%9E%8B-%E5%BE%AE%E4%BF%A1%E5%B7%A5%E5%85%B7-2f6fdd)
![语言：Go](https://img.shields.io/badge/%E8%AF%AD%E8%A8%80-Go-00ADD8)
![状态：维护中](https://img.shields.io/badge/%E7%8A%B6%E6%80%81-%E7%BB%B4%E6%8A%A4%E4%B8%AD-2ea44f)
![README：中文](https://img.shields.io/badge/README-%E4%B8%AD%E6%96%87-d73a49)

`fbif-wechat-article` 是一个 Go 同步服务，用于把微信公众号已发布文章同步到飞书多维表格，并补齐封面、正文和媒体链接信息。

## 仓库定位

- 分类：微信工具 / FBIF 内容运营自动化。
- 面向对象：需要把微信公众号文章沉淀到飞书多维表格的内容、运营和工程团队。
- 使用边界：本仓库负责文章同步、媒体补齐和历史素材回填；不负责公众号后台编辑、阅读数据分析或知识库问答。

## 功能概览

- 使用微信公众号 `freepublish/batchget` 接口同步已发布文章。
- 将文章元数据、正文内容、封面信息和同步状态写入飞书多维表格。
- 启动后自动执行一次同步，并由内置 scheduler 每天 `09:00` 再次同步。
- 使用 `.sync-cursor.json` 记录扫描进度，支持服务重启后续跑。
- 媒体 worker 可后台补齐封面图链接和正文图片链接。
- 历史素材 worker 可通过 `material/batchget_material` 回填素材库旧文章。
- 图片可优先转存到阿里云 OSS；未配置 OSS 时回退到本地 `/media/` 静态目录。
- 提供健康检查、手动触发同步和查看 cursor 的 HTTP API。

## 同步字段

默认写入的主表为 `公众号文章`。当前同步字段包括：

- `文章标题`
- `唯一键`
- `文章ID`
- `消息ID`
- `文章位置`
- `作者`
- `摘要`
- `文章链接`
- `封面素材ID`
- `显示封面图`
- `是否已删除`
- `更新时间戳`
- `更新时间`
- `发布日期`
- `发布月份`
- `正文HTML`
- `文章内容`
- `正文来源`
- `封面图链接`
- `正文图片链接`
- `同步时间`

## 技术栈

- Go `1.26.1`
- 标准库 `net/http`
- 微信公众号官方 API
- 飞书开放平台与多维表格 API
- 可选阿里云 OSS 媒体存储

## 项目结构

```text
.
├── main.go              # HTTP 服务、worker 启动和运行时配置
├── config/              # 环境变量和 .env 加载
├── sync/                # 主同步、scheduler、cursor、媒体和历史 worker
├── wechat/              # 微信 API、token、素材、正文和图片处理
├── feishu/              # 飞书 token 与多维表格写入
├── .env.example         # 环境变量示例
└── go.mod
```

## 快速开始

准备配置：

```bash
cp .env.example .env
```

填写微信公众号、飞书和 API key 配置后运行：

```bash
go test ./...
go run .
```

构建二进制：

```bash
go build -o wechat-sync .
```

Linux amd64 构建示例：

```bash
GOOS=linux GOARCH=amd64 go build -o wechat-sync .
```

## 配置

最少需要配置：

- `WECHAT_APPID`
- `WECHAT_SECRET`
- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- `FEISHU_BITABLE_APP_TOKEN`
- `API_KEY`

常用可选项：

- `SERVER_PORT`，默认 `3002`
- `GO_MEMORY_LIMIT_MB`，默认 `512`
- `FEISHU_RECORD_BATCH_SIZE`
- `WECHAT_DAILY_QUOTA_LIMIT`
- `WECHAT_DAILY_QUOTA_RESERVE`
- `WECHAT_PUBLISHED_PAGE_SIZE`
- `WECHAT_PUBLISHED_RECENT_PAGES`
- `WECHAT_PUBLISHED_BACKFILL_GROW_PAGES`
- `WECHAT_SYNC_COVER_INLINE`
- `WECHAT_SYNC_BODY_IMAGES_INLINE`
- `PUBLIC_MEDIA_DIR`

媒体 worker：

- `ENABLE_MEDIA_WORKER`
- `MEDIA_WORKER_BATCH_SIZE`
- `MEDIA_WORKER_CONCURRENCY`
- `MEDIA_WORKER_INITIAL_DELAY_SECONDS`
- `MEDIA_WORKER_INTERVAL_MINUTES`

历史素材 worker：

- `ENABLE_HISTORY_WORKER`
- `HISTORY_WORKER_INITIAL_DELAY_SECONDS`
- `HISTORY_WORKER_INTERVAL_MINUTES`
- `MATERIAL_HISTORY_PAGE_SIZE`
- `MATERIAL_HISTORY_MAX_CALLS_PER_RUN`
- `HISTORY_WORKER_WRITE_PAUSE_MS`

阿里云 OSS：

- `OSS_ACCESS_KEY_ID`
- `OSS_ACCESS_KEY_SECRET`
- `OSS_BUCKET`
- `OSS_REGION`
- `OSS_BUCKET_DOMAIN`

完整示例见 [.env.example](.env.example)。

## HTTP API

| Method | Path | 说明 | 鉴权 |
| --- | --- | --- | --- |
| `GET` | `/health` | 健康检查，返回 token 状态和 cursor 摘要。 | 不需要 |
| `POST` | `/api/feishu/sync` | 手动触发一次同步。 | `API_KEY` |
| `GET` | `/api/feishu/cursor` | 查看同步进度 cursor。 | `API_KEY` |

受保护接口支持两种鉴权方式：

```http
Authorization: Bearer <token>
X-API-Key: <token>
```

## 运行机制

### 主同步

- 启动时自动执行一次轻量同步。
- 每天 `09:00` 自动同步。
- 默认只扫描最近 `3` 页。
- 历史页进度由 cursor 记录。
- 可通过 `WECHAT_SYNC_COVER_INLINE` 和 `WECHAT_SYNC_BODY_IMAGES_INLINE` 在主同步中内联补齐图片，默认关闭。

### 媒体 worker

- 默认启用。
- 补齐 `封面图链接` 和 `正文图片链接`。
- 优先写入 OSS；未配置 OSS 时写入本地 `PUBLIC_MEDIA_DIR` 或 `./media`。
- 不阻塞主同步。

### 历史素材 worker

- 默认启用。
- 分页遍历素材库中的图文消息。
- 使用 `cursor.materialNewsOffset` 断点续传。
- 内置配额感知，达到限制后自动暂停。

## 部署建议

- 使用 systemd 或类似进程管理器托管编译后的二进制。
- 工作目录保留二进制、`.env`、`.sync-cursor.json` 和可选 `media/`。
- 不要把一次性迁移或重型脚本放进常驻同步服务的启动流程。

## 注意事项

- `.sync-cursor.json` 是本地同步进度文件，不是业务数据。
- 主同步链路优先保证稳定，媒体补齐和历史回填不应影响主同步。
- 后续阅读数据、知识库索引或问答增强能力应作为独立模块接入。
