# fbif-wechat-article

一个轻量的 GO 同步服务，用于把微信公众号已发布文章同步到飞书多维表格。

## 当前定位

- 单一 GO 二进制部署
- 主流程只负责同步公众号文章主数据
- 媒体补齐独立为后台 worker，不阻塞主同步
- 历史素材回填独立为后台 worker，自动断点续传
- 适合部署在低配置 Linux 服务器

## 当前同步内容

主表：`公众号文章`

已同步字段：

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

## 运行机制

### 1. 主同步

主同步使用微信公众号官方 `freepublish/batchget` 接口：

- 启动时自动执行一次轻量同步
- 每天 `09:00` 自动同步
- 默认只扫描最近 `3` 页
- 历史页进度由 `cursor` 记录
- 支持通过 `WECHAT_SYNC_COVER_INLINE` / `WECHAT_SYNC_BODY_IMAGES_INLINE` 在主同步中内联补齐封面和正文图片（默认关闭）

### 2. 媒体 worker

媒体 worker 独立运行（默认启用）：

- 补齐 `封面图链接`
- 补齐 `正文图片链接`
- 图片优先转存到阿里云 OSS；未配置 OSS 时回退到本地 `/media/` 目录
- 不影响主同步稳定性

### 3. 历史素材 worker

历史素材 worker 独立运行（默认启用），通过 `material/batchget_material` 接口回填旧文章：

- 分页遍历素材库中的图文消息
- 进度由 `cursor.materialNewsOffset` 记录，支持断点续传
- 内置配额感知，达到限额自动暂停
- 不影响主同步和媒体 worker 稳定性

### 4. Cursor

`cursor` 是同步进度文件（`.sync-cursor.json`），不是业务数据。

它记录：

- 已发布文章列表扫到第几页（`publishedScannedPages`）
- 已发布文章历史是否已经扫完（`publishedBackfillComplete`）
- 素材库回填偏移量（`materialNewsOffset`）
- 素材库回填是否完成（`materialBackfillComplete`）

作用：

- 避免重复全量抓取
- 服务重启后从上次进度继续

## 资源占用

当前线上实测：

- 常驻 RSS 约 `25MB ~ 30MB`
- 设置 GO 内存上限默认 `512MB`

适合低配服务器运行。

## 环境变量

最少需要：

- `WECHAT_APPID`
- `WECHAT_SECRET`
- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- `FEISHU_BITABLE_APP_TOKEN`
- `API_KEY`

可选：

- `SERVER_PORT`
- `GO_MEMORY_LIMIT_MB`
- `FEISHU_RECORD_BATCH_SIZE`
- `WECHAT_DAILY_QUOTA_LIMIT`
- `WECHAT_DAILY_QUOTA_RESERVE`
- `WECHAT_PUBLISHED_PAGE_SIZE`
- `WECHAT_PUBLISHED_RECENT_PAGES`
- `WECHAT_PUBLISHED_BACKFILL_GROW_PAGES`
- `WECHAT_SYNC_COVER_INLINE`
- `WECHAT_SYNC_BODY_IMAGES_INLINE`
- `PUBLIC_MEDIA_DIR`

媒体 worker（默认启用）：

- `ENABLE_MEDIA_WORKER`
- `MEDIA_WORKER_BATCH_SIZE`
- `MEDIA_WORKER_CONCURRENCY`
- `MEDIA_WORKER_INITIAL_DELAY_SECONDS`
- `MEDIA_WORKER_INTERVAL_MINUTES`

历史素材 worker（默认启用）：

- `ENABLE_HISTORY_WORKER`
- `HISTORY_WORKER_INITIAL_DELAY_SECONDS`
- `HISTORY_WORKER_INTERVAL_MINUTES`
- `MATERIAL_HISTORY_PAGE_SIZE`
- `MATERIAL_HISTORY_MAX_CALLS_PER_RUN`
- `HISTORY_WORKER_WRITE_PAUSE_MS`

阿里云 OSS（未配置时回退到本地 `./media/` 目录）：

- `OSS_ACCESS_KEY_ID`
- `OSS_ACCESS_KEY_SECRET`
- `OSS_BUCKET`
- `OSS_REGION`
- `OSS_BUCKET_DOMAIN`

完整示例见 `.env.example`。

## HTTP 接口

只保留最小接口：

- `GET /health`
- `POST /api/feishu/sync`
- `GET /api/feishu/cursor`

除了 `/health`，其余接口都需要 `API_KEY`。

支持两种传法：

- `Authorization: Bearer <token>`
- `X-API-Key: <token>`

## 构建

本地：

```bash
go test ./...
go build -o wechat-sync .
```

Linux 服务器：

```bash
GOOS=linux GOARCH=amd64 go build -o wechat-sync .
```

## 部署建议

推荐 systemd 方式部署：

- 工作目录只放一个二进制和一个 `.env`
- 使用 systemd 守护
- 不要把重型迁移脚本放进日常流程

## 设计原则

- 主同步必须稳定
- 媒体补齐、历史回填不能阻塞主同步
- 模块可以继续扩展

后续如果增加：

- 每月阅读数据
- 知识库索引
- 问答增强

都应该作为独立模块挂接，不污染主同步链路。
