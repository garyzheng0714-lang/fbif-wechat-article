# fbif-wechat-article

![类型：微信工具](https://img.shields.io/badge/%E7%B1%BB%E5%9E%8B-%E5%BE%AE%E4%BF%A1%E5%B7%A5%E5%85%B7-2f6fdd)
![语言：Go](https://img.shields.io/badge/%E8%AF%AD%E8%A8%80-Go-00ADD8)
![状态：维护中](https://img.shields.io/badge/%E7%8A%B6%E6%80%81-%E7%BB%B4%E6%8A%A4%E4%B8%AD-2ea44f)
![README：中文](https://img.shields.io/badge/README-%E4%B8%AD%E6%96%87-d73a49)

`fbif-wechat-article` 是一个只使用微信公众号官方 API 的归档与数据采集服务。默认自动归档已发布文章、文章指标和粉丝指标，并可把普通图文送入排版待审流程，或在本地数据完整后批量同步到飞书多维表格。

> **数据口径红线：** `freepublish/batchget.total_count` 是发布对象数，不是公众号历史已发布文章总量。任何“历史全量”结论都必须经过多个官方 API、多表关联、去重及覆盖审计；审计完成并经确认前，飞书 Base 同步保持关闭。详见 [已发布文章真值合同](docs/PUBLISHED-ARTICLE-TRUTH-CONTRACT.md)。

## 仓库定位

- 分类：微信工具 / FBIF 内容运营自动化。
- 面向对象：需要把微信公众号文章沉淀到飞书多维表格的内容、运营和工程团队。
- 使用边界：本仓库负责官方接口归档、阅读数据采集、历史补采、自动排版投递和飞书同步；不抓公众号网页，不使用 RSS/订阅源，不负责公众号后台编辑或知识库问答。

## 功能概览

- 使用微信公众号 `freepublish/batchget` 接口同步已发布文章。
- 覆盖微信文档中的 21 个数据接口：15 个现役接口每日采集，6 个已下线旧接口保留一次 `47009` 官方响应和替代接口说明。
- 采集文章阅读、分享、在看、点赞、评论、收藏、赞赏、阅读完成率、阅读来源，以及粉丝新增、取关、净增和累计数据。
- 自动内容归档只遍历 `freepublish/batchget` 已发布文章，并逐篇调用 `freepublish/getarticle` 保存详情接口全部字段；草稿最新页只用于读取 `article_type`，素材库不参与历史同步。
- 所有响应先按原始字节写入 SQLite，再生成可查询的文章指标行；微信新增字段无需改结构即可留存。
- 每天 `00:05` 在上海时区额度刷新后立即抓昨天及最近 30 天仍会变化的数据，再在保留每日 2% 官方额度后，从新到旧断点回填。
- 每天北京时间 `08:30` 至 `18:30`（含首尾）每 `15` 分钟独立轮询官方 `freepublish/batchget`；区间外不轮询新文章，`draft` 接口失败不会阻断已发布轮询。只有被可靠类型信号确认为普通图文的新文章才经持久化 outbox 投递给排版服务；小绿书/图片消息 `newspic` 永久跳过，类型不明时 fail closed。
- 已发布文章、文章指标、粉丝指标、评论和同步状态先完整落 SQLite；Base 同步显式开启后再批量增量写入。
- 官方 API 采集器启动后自动执行一次，此后每天 `00:05` 再次执行；旧直连飞书 scheduler 已停用。
- 使用 `.sync-cursor.json` 记录扫描进度，支持服务重启后续跑。
- 媒体 worker 可后台补齐封面图链接和正文图片链接。
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
- SQLite（`modernc.org/sqlite`，无 CGO）
- 微信公众号官方 API
- 飞书开放平台与多维表格 API
- 可选阿里云 OSS 媒体存储

## 项目结构

```text
.
├── main.go              # HTTP 服务、worker 启动和运行时配置
├── config/              # 环境变量和 .env 加载
├── analytics/           # 21 个数据接口、内容接口采集器和调度
├── archive/             # SQLite 原始响应、指标行、游标和内容归档
├── autolayout/          # 官方发表正文的基线、持久 outbox 与排版投递
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

官方采集器：

- `OFFICIAL_API_DB_PATH`，默认 `./data/wechat-official.db`
- `ANALYTICS_MAX_CALLS_PER_RUN`，默认 `2000`；实际运行会动态扣除当天余下的发布轮询预算
- `CONTENT_MAX_CALLS_PER_RUN`，默认 `400`；优先补齐已发布列表与逐篇详情
- `ANALYTICS_BACKFILL_START`，可限制历史补采起点；不填时按各接口官方起始日期
- `OFFICIAL_COLLECTOR_INITIAL_DELAY_SECONDS`，默认 `5`
- `ENABLE_OFFICIAL_API_COLLECTOR`，默认开启
- `ENABLE_FEISHU_SYNC` 是已退役的直接写 Base 链路，代码中永久 fail closed；不得启用

官方数据写回 Base（显式开启）：

- `ENABLE_OFFICIAL_BASE_SYNC=1`
- `OFFICIAL_BASE_SYNC_INTERVAL_MINUTES`，默认 `5`
- `OFFICIAL_BASE_SYNC_ROWS_PER_DATASET`，默认每轮每张表 `500`
- `OFFICIAL_BASE_SYNC_INITIAL_DELAY_SECONDS`，默认 `20`
- 即使开启 worker，`historicalCoverage.verified` 未经完整审计和人工确认时也会在调用飞书接口前拒绝写入

自动排版（显式开启）：

- `ENABLE_AUTO_LAYOUT=1`
- `LAYOUT_OFFICIAL_SYNC_URL`，排版服务的 `/api/publish/official-sync` 完整地址
- `LAYOUT_SYNC_TOKEN`，排版服务 `PUBLISH_SYNC_SERVICE_TOKEN` 的同值，随请求发在
  `X-Publish-Sync-Token` 头上。取代 `LAYOUT_ADMIN_PASSWORD`：排版服务 2026-08-04
  把密码鉴权换成会话角色后，旧的 `X-Admin-Password` 已无对端支持

> 投递载荷**只有文章链接加类型证据**，不含正文、标题、作者与封面——排版服务只接收
> 链接并自己抓原文，喂二手 HTML 会绕开它按账号规则包取参数的解析路径。
- `LAYOUT_SOURCE_NAME`，默认 `FBIF食品饮料创新`
- `AUTO_LAYOUT_POLL_INTERVAL_MINUTES`，默认 `15`；只在北京时间 `08:30` 至 `18:30`（含首尾）生效
- `AUTO_LAYOUT_MAX_DELIVERIES_PER_RUN`，默认 `20`

公众号群发结果回调（默认关闭）：

- 仅配置 `WECHAT_CALLBACK_TOKEN` 后才启用 `/api/wechat/publish-callback`；安全模式再配置 `WECHAT_CALLBACK_AES_KEY`，`WECHAT_CALLBACK_APP_ID` 不填时沿用 `WECHAT_APPID`
- 该回调用于补充 `freepublish` 无法覆盖的群发结果事件；启用公众号统一“服务器配置”前，必须先迁移并验证现有自动回复、菜单及其他回调，禁止直接覆盖线上配置

常用可选项：

- `SERVER_PORT`，默认 `3002`
- `GO_MEMORY_LIMIT_MB`，默认 `512`
- `FEISHU_RECORD_BATCH_SIZE`
- `WECHAT_DAILY_QUOTA_LIMIT`
- `WECHAT_DAILY_QUOTA_RESERVE_PERCENT`，默认 `2`
- `WECHAT_DAILY_QUOTA_RESERVE`，旧版绝对值兼容项；百分比配置优先
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

历史素材 worker（旧版可选能力，默认不启用）：

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
| `POST` | `/api/feishu/sync` | 已退役，固定返回 `410`，防止绕过 SQLite 和覆盖审计直接写 Base。 | `API_KEY` |
| `POST` | `/api/feishu/official-sync` | 仅在历史覆盖审计已通过并经人工确认后，批量增量写入 Base。 | `API_KEY` |
| `GET` | `/api/feishu/cursor` | 查看同步进度 cursor。 | `API_KEY` |
| `GET` | `/api/wechat/official/status` | 查看 15 个现役接口、6 个下线接口、回填游标和存储统计。 | `API_KEY` |
| `GET` | `/api/wechat/official/coverage` | 查看多接口文章身份并集、关联缺口、回填覆盖和 Base 门禁状态。 | `API_KEY` |
| `POST` | `/api/wechat/official/coverage` | 仅在 `eligibleForUserApproval=true` 后，携带合同版本、确认短语和确认人完成显式确认。 | `API_KEY` |
| `DELETE` | `/api/wechat/official/coverage` | 撤销历史覆盖确认并立即关闭 Base 门禁。 | `API_KEY` |
| `GET` | `/api/wechat/official/endpoints` | 查看全部数据与内容接口清单、生命周期和必填标识符。 | `API_KEY` |
| `POST` | `/api/wechat/official/collect` | 立即执行一次完整增量采集。 | `API_KEY` |
| `POST` | `/api/wechat/official/call` | 调用白名单内单个官方接口并归档原始响应。 | `API_KEY` |
| `GET/POST` | `/api/wechat/publish-callback` | 微信群发结果回调；默认关闭，启用后按微信签名或安全模式 AES 验证。 | 微信回调签名 |

受保护接口支持两种鉴权方式：

```http
Authorization: Bearer <token>
X-API-Key: <token>
```

## 运行机制

### 官方 API 采集

- 服务启动后自动采集一次，此后每天北京时间 `00:05` 执行。
- `ready` 只表示采集服务与当前接口可运行，绝不表示历史文章全量已核验；历史口径只看 `historicalCoverage.verified`。
- `getarticletotaldetail` 会重复刷新最近 30 个发表日；其他接口按官方最大跨度拆分并断点回填。
- 正文、身份与指标分表保存：`official_content_articles` 保存发布正文；`official_article_publications` 用 `msgid=msg_data_id_index` 保存文章身份；`official_article_metric_facts` 关联阅读、分享、阅读后关注等文章事实；`official_follower_metric_facts` 保存账号新增、取关、净增和累计粉丝。`official_article_catalog` 通过官方 `content_url` 关联正文，`official_article_latest_performance` 提供每篇文章最新累计表现。
- `official_known_article_catalog` 使用 `msgid` 把阅读、分享、发表详情、旧接口留档和 `freepublish` 正文合并为“已知文章身份并集”。其中 `knownArticleIdentities` 只能按这个名称汇报；在完整回填、缺口审计和人工确认前，不得改名为公众号历史文章总量。
- 官方窗口限制按接口独立执行：`freepublish/batchget` 每页 1–20 个发布对象；新版文章阅读、分享、发表详情从 `2025-11-01` 起提供且查询结束日最多为昨日，其中阅读/分享每次 1 天、发表详情每篇只统计发表后 30 天；用户增减与累计数据从 `2014-12-01` 起提供，用户增减按来源记录，账号净增不得直接归因给单篇文章。
- 每次请求和响应都留档。相同请求得到完全相同的响应时，后续记录引用第一份原始字节，避免重复正文写满磁盘。
- 可用 `./wechat-sync collect-once` 做一次性采集和服务器验收。

### 官方数据写回多维表格

- `ENABLE_OFFICIAL_BASE_SYNC=1` 只启动 worker；只有 `historicalCoverage.verified=true` 才允许把 SQLite 中已归档数据增量写回 `FEISHU_BITABLE_APP_TOKEN`。
- 自动维护 13 张可关联的仪表盘数据表：`文章主档`、`文章每日指标`、`文章累计指标`、`账号内容日报`、`粉丝来源日报`、`粉丝累计日报`、`上行消息指标`、`接口性能指标`、`发布对象原始档`、`发布正文原始档`、`文章评论`、`官方API调用留档`、`接口同步状态`。草稿及图片/图文/视频/语音素材不进入 Base；发布对象和已发布正文原始字段保留。
- 每条记录使用官方 `msgid`、日期和来源维度组成稳定唯一键；本地保存 Base `record_id` 与 payload hash，只同步新增或变化的记录，不在每轮扫描后全表重写。
- 所有已知官方字段结构化写入，嵌套数组与未知新增字段同时保留在原始 JSON 字段。Base 单个文本单元格超过平台上限时会写入带 SHA-256 的可追踪截断值，SQLite 始终保留完整原始字节。
- 新发布正文按轮询间隔同步；微信文章与粉丝统计接口的结束日期最大为昨日，因此指标在官方提供后立即同步，不能伪造成同日实时数据。

### 自动排版

- 第一次启用时，库内已有 `freepublish` 文章只登记为历史基线，不会批量创建旧稿。
- 此后仅在北京时间 `08:30` 至 `18:30`（含首尾）每 `15` 分钟刷新 `freepublish/batchget` 最新页；`draft/batchget` 只由完整内容采集用于补充官方类型快照，不再是已发布轮询的前置条件。区间外不轮询新文章，多图文中的每篇文章独立去重和投递。
- `article_type=news` 才允许进网站；`newspic` 只记录跳过；已发布接口未返回类型且无法与官方草稿快照严格匹配时，服务健康状态报警且绝不自动投递。
- `freepublish` 历史分页在详情和评论之前获得专用预算，避免每日调用上限导致历史永久无法回填。
- 标题、作者、正文 HTML、封面和文章 URL 全部取自官方响应；只有链接、没有官方正文的数据分析记录不会进入排版。
- outbox 在 SQLite 中持久化，网络失败或服务重启后继续重试；公众号 URL 的 `sn/chksm/scene` 变化不会造成重复稿。
- 排版服务自动流程只到 `awaiting_review`，不会代替人工批准，也不会自动公开到 FoodTalks。

### 已退役的直接同步

- 旧 `ENABLE_FEISHU_SYNC` 链路会绕过 SQLite 全量关联和人工确认，现已永久 fail closed。
- `/api/feishu/sync` 固定返回 `410`；唯一允许的 Base 写入入口是带历史覆盖门禁的 `/api/feishu/official-sync`。

### 媒体 worker

- 默认启用。
- 补齐 `封面图链接` 和 `正文图片链接`。
- 优先写入 OSS；未配置 OSS 时写入本地 `PUBLIC_MEDIA_DIR` 或 `./media`。
- 不阻塞主同步。

### 历史素材 worker（旧版可选能力）

- 默认不启用，也不属于当前“已发布文章”同步范围。
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
- 6 个旧文章分析接口已由微信返回 `47009 this api is offline, please use the new api`；服务不会无限重试，而是记录下线响应并使用新接口。
- `freepublish/batchget` 仅提供一条官方发布对象采集流；它的分页完成不等于历史文章全量已经核验。本服务不用网页或订阅源补造数据，历史全量结论按 [已发布文章真值合同](docs/PUBLISHED-ARTICLE-TRUTH-CONTRACT.md) 执行。
