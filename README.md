<p align="center">
  <img src="./assets/readme/hero.svg" width="100%" alt="fbif-wechat-article：只走公众号官方 API 归档已发布文章与阅读、分享、在看等指标，原始响应先按字节落 SQLite">
</p>

<p align="center">
  <img src="https://img.shields.io/badge/%E7%B1%BB%E5%9E%8B-%E5%BE%AE%E4%BF%A1%E5%BD%92%E6%A1%A3%E4%B8%8E%E9%87%87%E9%9B%86-2f6fdd?style=flat-square" alt="微信归档与采集">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/SQLite-%E6%97%A0%20CGO-003B57?style=flat-square&logo=sqlite&logoColor=white" alt="SQLite 无 CGO">
  <img src="https://img.shields.io/badge/%E6%8E%A5%E5%8F%A3-15%20%E4%B8%AA%E7%8E%B0%E5%BD%B9-16a34a?style=flat-square" alt="15 个现役接口">
</p>

# fbif-wechat-article · 公众号文章归档

**只使用微信公众号官方 API** 的归档与数据采集服务。默认自动归档已发布文章、文章指标和粉丝指标，并可把普通图文送入排版待审流程，或在本地数据完整后批量同步到飞书多维表格。

所有响应先按原始字节写入 SQLite，再生成可查询的指标行——**微信新增字段不需要改表结构就能留存**。

> **数据口径红线：** `freepublish/batchget.total_count` 是发布对象数，**不是**公众号历史已发布文章总量。任何「历史全量」结论都必须经过多个官方 API、多表关联、去重及覆盖审计；审计完成并经确认前，飞书 Base 同步保持关闭。详见 [已发布文章真值合同](docs/PUBLISHED-ARTICLE-TRUTH-CONTRACT.md)。

## 仓库定位

- 分类：微信工具 / FBIF 内容运营自动化。
- 面向对象：需要把微信公众号文章沉淀到飞书多维表格的内容、运营和工程团队。
- 使用边界：本仓库负责官方接口归档、阅读数据采集、历史补采、自动排版投递和飞书同步；不抓公众号网页，不使用 RSS/订阅源，不负责公众号后台编辑或知识库问答。

## 功能概览

- 使用微信公众号 `freepublish/batchget` 接口归档已发布文章。
- 覆盖微信文档中的 21 个数据接口：15 个现役接口每日采集，6 个已下线旧接口保留一次 `47009` 官方响应和替代接口说明。
- 采集文章阅读、分享、在看、点赞、评论、收藏、赞赏、阅读完成率、阅读来源，以及粉丝新增、取关、净增和累计数据。
- 自动内容归档只遍历 `freepublish/batchget` 已发布文章，并逐篇调用 `freepublish/getarticle` 保存详情接口全部字段；草稿最新页只用于读取 `article_type`，素材库不参与历史同步。
- 所有响应先按原始字节写入 SQLite，再生成可查询的文章指标行；微信新增字段无需改结构即可留存。
- 每天 `08:05`（上海时区）抓取昨天及最近 30 天仍会变化的数据，再在每接口保留 `200` 次额度后，从新到旧断点回填。
- 文章数据（`getarticletotaldetail`）每篇只统计到发表后第 30 天。哪个发表日出了 30 天刷新范围还没采到第 30 天（那天采集失败、或问得太早微信还没出数），同一轮里在回填之前补查一次，从新到旧、一轮最多 10 个发表日；满 31 天后成功查过一次仍不全的（比如文章删了）不再查。平时没有缺口就不多花额度。
- 每天北京时间 `08:30` 至 `18:30`（含首尾）每 `15` 分钟独立轮询官方 `freepublish/batchget`；区间外不轮询新文章，`draft` 接口失败不会阻断已发布轮询。只有被可靠类型信号确认为普通图文的新文章才经持久化 outbox 投递给排版服务；小绿书/图片消息 `newspic` 永久跳过，类型不明时 fail closed。
- 已发布文章、文章指标、粉丝指标、评论和同步状态先完整落 SQLite；Base 同步显式开启后再批量增量写入。
- 官方 API 采集器每天 `08:05` 执行；`08:05` 后重启时，今天那一轮已经跑完（每个现役接口在 `08:05` 之后都成功拉过）就不再补跑，没跑完或有接口失败才补跑一次；`08:05` 前启动会等待当天窗口。2026-11-06 一天重启 17 次、每次都补跑一整轮，文章数据接口用到 460 次，之后改成这样。
- 提供健康检查、手动触发采集与 Base 写回的 HTTP API。

## 技术栈

- Go `1.26.1`
- 标准库 `net/http`
- SQLite（`modernc.org/sqlite`，无 CGO）
- 微信公众号官方 API
- 飞书开放平台与多维表格 API

## 项目结构

```text
.
├── main.go              # HTTP 服务、worker 启动和运行时配置
├── config/              # 环境变量和 .env 加载
├── analytics/           # 21 个数据接口、内容接口采集器和调度
├── archive/             # SQLite 原始响应、指标行、游标和内容归档
├── autolayout/          # 官方发表正文的基线、持久 outbox 与排版投递
├── sync/                # 旧直连同步留下的 cursor 读取（/health、/api/feishu/cursor）
├── wechat/              # 微信 API、token、配额
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

部署到生产（资讯机，FBIF-内部工具服务器 101.133.154.40）：

```bash
scripts/deploy-cloud-assistant.sh
```

它在本地交叉编译 Linux amd64，经阿里云云助手分片上传、校验 SHA-256、替换重启并做健康冒烟，失败自动回滚。前提是本机 `aliyun` CLI 已用 OAuth 登录（过期时 `aliyun configure --profile new-account --mode OAuth`）。不需要 SSH、公网 IP 白名单或 VPN。

## 配置

最少需要配置：

- `WECHAT_APPID`
- `WECHAT_SECRET`
- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- `FEISHU_BITABLE_APP_TOKEN`
- `API_KEY`

轮询和定时的时间在 FBIF 中台总账本「定时任务和回调」改「多久一次」，中台写进 `/etc/fbif-hub/env/wechat-sync.env`，不写进 `.env`。变量没下发或看不懂时用默认值；启动日志 `[Schedule]` 那一行列出现在按什么跑。

| 总账本那一行 | 变量 | 默认 |
| --- | --- | --- |
| 公众号数据抓取 `reczz28NbDlb780a` | `ANALYTICS_TIMES` | `08:05`，逗号隔开几个时刻就每个时刻都跑 |
| 公众号发文轮询 `reczz28NbDlb8bqz` | `AUTO_LAYOUT_POLL_WINDOW`、`AUTO_LAYOUT_POLL_SECONDS` | `08:30-18:45`、`900`；结尾那一刻不跑，最后一次 `18:30` |
| 文章数据写回兜底 `reczz28UubKiJIss` | `OFFICIAL_BASE_SYNC_FALLBACK_SECONDS` | `21600` |

`freepublish` 心跳告警跟着发文轮询走：时段内超过 3 个间隔没成功才报。

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
- `OFFICIAL_BASE_SYNC_ROWS_PER_DATASET`，默认每轮每张表 `500`
- `OFFICIAL_BASE_SYNC_INITIAL_DELAY_SECONDS`，默认 `20`
- 即使开启 worker，`historicalCoverage.verified` 未经完整审计和人工确认时也会在调用飞书接口前拒绝写入

业务数据表（中台「配置项」下发，不填就不跑）：

- `BIZ_BASE_APP_TOKEN`、`BIZ_BASE_ARTICLE_TABLE_ID`：同事看数据的多维表格和其中的文章表
- 用 `FEISHU_APP_ID`、`FEISHU_APP_SECRET` 那个应用（「公众号数据助手」）写表，应用要是这个多维表格的可编辑协作者

自动排版（显式开启）：

- `ENABLE_AUTO_LAYOUT=1`
- `LAYOUT_OFFICIAL_SYNC_URL`，排版服务的 `/api/publish/official-sync` 完整地址
- `PUBLISH_SYNC_SERVICE_TOKEN`，仅用于排版服务提交任务和紧凑状态读取的服务令牌

> 投递载荷**只有文章链接加类型证据**，不含正文、标题、作者与封面——排版服务两个
> 发布入口只接收链接并自己抓原文（其侧红线 33），带正文字段会被 400 拒收。
- `LAYOUT_SOURCE_NAME`，默认 `FBIF食品饮料创新`
- `AUTO_LAYOUT_MAX_DELIVERIES_PER_RUN`，默认 `20`

日报与停摆告警：

- 报告通道二选一：优先使用 `OFFICIAL_FEISHU_WEBHOOK_URL`（飞书自定义机器人 HTTPS 地址）与可选的 `OFFICIAL_FEISHU_WEBHOOK_SECRET`；未配置 webhook 时，使用 `FEISHU_APP_ID`、`FEISHU_APP_SECRET`、`OFFICIAL_FEISHU_CHAT_ID` 由应用机器人发到指定群
- `ANALYTICS_DEFERRED_RETRY_MINUTES`，官方返回 `is_delay` 后的重试间隔，默认 `30`
- `ANALYTICS_DEFERRED_MAX_RETRIES`，延迟窗口有界重试次数，默认 `3`；耗尽后告警，不推进覆盖游标
- `GET /api/wechat/official/monitoring` 是紧凑监控入口，只接受运维 `API_KEY` 或最小权限 `PUBLISH_SYNC_SERVICE_TOKEN`；服务令牌不能访问 status、coverage 或任何写接口。探测这个入口的看门狗归 FoodTalks_Feed 的 `monitoring/watchdog.py`（见下文部署建议），本仓库不再自带看门狗

公众号群发结果回调（兼容代码保留，本方案不启用）：

- 当前自动同步依赖 `freepublish` 最新页轮询，不配置 `WECHAT_CALLBACK_TOKEN`，也不改动公众号后台“服务器配置”
- 兼容能力仅在未来另行批准并配置 `WECHAT_CALLBACK_TOKEN` 后才启用 `/api/wechat/publish-callback`；安全模式再配置 `WECHAT_CALLBACK_AES_KEY`，`WECHAT_CALLBACK_APP_ID` 不填时沿用 `WECHAT_APPID`
- 该回调用于补充 `freepublish` 无法覆盖的群发结果事件；启用公众号统一“服务器配置”前，必须先迁移并验证现有自动回复、菜单及其他回调，禁止直接覆盖线上配置

常用可选项：

- `SERVER_PORT`，默认 `3002`
- `GO_MEMORY_LIMIT_MB`，默认 `512`
- `FEISHU_RECORD_BATCH_SIZE`
- `WECHAT_ENDPOINT_DAILY_QUOTA_LIMIT`，每个官方接口各自的日上限，默认且最高 `1000`；旧名 `WECHAT_DAILY_QUOTA_LIMIT` 仅兼容
- `WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE`，每接口默认保留 `200`，因此默认可用预算为 `800`；也可用 `WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE_PERCENT` 覆盖
- `WECHAT_QUOTA_FILE`，按接口持久化预算计数的文件；写入失败会回滚本次内存预占并 fail closed
- 官方额度实查（2026-11-06，[`openapi/quota/get`](https://developers.weixin.qq.com/doc/offiaccount/openApi/get_api_quota.html)）：`/datacube/getusersummary`、`/cgi-bin/freepublish/batchget` 每天 `1000`，`/cgi-bin/comment/list` 每天 `10000`；用得最多的 `/datacube/getarticletotaldetail` 查不到（返回 `76021 cgi_path not found`），它的真实上限未核实，程序按 `1000` 算
- `getarticletotaldetail` 一次只查一个发表日，每篇只统计发表后 30 天，2025-11-01 以前没有数（[官方文档](https://developers.weixin.qq.com/doc/subscription/api/wedata/news/api_getarticletotaldetail.html)）；所以每天约 30 次花在刷新最近 30 个发表日上
- 手动补某一天用 `POST /api/wechat/official/call`（`{"endpoint":"getarticletotaldetail","begin_date":"…","end_date":"…"}`，一个发表日一次）；`POST /api/wechat/official/collect` 会跑一整轮采集，别拿它补单日

完整示例见 [.env.example](.env.example)。

## HTTP API

| Method | Path | 说明 | 鉴权 |
| --- | --- | --- | --- |
| `GET` | `/health` | 健康检查，返回 token 状态和 cursor 摘要。 | 不需要 |
| `POST` | `/api/feishu/sync` | 已退役，固定返回 `410`，防止绕过 SQLite 和覆盖审计直接写 Base。 | `API_KEY` |
| `POST` | `/api/biz/run` | 立即跑一轮业务数据表写入。 | `API_KEY` |
| `POST` | `/api/feishu/official-sync` | 仅在历史覆盖审计已通过并经人工确认后，批量增量写入 Base。 | `API_KEY` |
| `GET` | `/api/feishu/cursor` | 查看同步进度 cursor。 | `API_KEY` |
| `GET` | `/api/wechat/official/status` | 查看 15 个现役接口、6 个下线接口、回填游标和存储统计。 | `API_KEY` |
| `GET` | `/api/wechat/official/monitoring` | 查看端点新鲜度、`freepublish` 最新页心跳、延迟窗口、自动排版 outbox 与日报配置。 | `API_KEY` 或 `PUBLISH_SYNC_SERVICE_TOKEN` |
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

- 服务若在北京时间 `08:05` 之后启动会自动采集一次；`08:05` 之前启动只等待当日 `08:05`，不会提前请求 D-1 阅读/分享数据。此后每天 `08:05` 执行并发送日报；延迟窗口按有界策略独立重试。
- 日报按接口列出当前窗口、deferred、缺口、独立配额余量，并给出基于剩余窗口、单轮上限和每接口可用预算的保守预计完成日期；存在延迟或接口错误时明确写“暂不可估算”。
- `08:30`–`18:30` 的自动排版轮询独立于完整历史采集启动；它先刷新 `freepublish` 最新页，再处理 outbox，草稿接口失败或历史回填缓慢不能阻断已发布文章发现。
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
- 不定时轮询：每次写完归档（`08:05` 日采集、`08:30`–`18:30` 每 `15` 分钟的发布轮询、延迟窗口重试、手动采集或调用）都会叫醒写回，等 `5` 秒合并后跑一轮；一轮里只对有待写行的表调飞书，没有待写行时一次飞书接口都不调。单表待写超过一轮上限时接着再跑，直到写完。兜底每 `6` 小时跑一轮，同样没有待写行就不调飞书。
- 表 ID 和字段列表进程内缓存，只在第一次写某张表、或写入报错之后重读；字段被人改名或删掉时，下一轮会重读并补建。
- 微信文章与粉丝统计接口的结束日期最大为昨日，因此指标在官方提供后的那次采集写完就同步，不能伪造成同日实时数据。

### 业务数据表

这是给同事看的业务表，和上面 13 张原始镜像表是两条路：不受 `historicalCoverage.verified` 门禁管，也不宣称「全量」，只写 SQLite 里已归档到的数据。

- 文章表 `文章数据`（`BIZ_BASE_ARTICLE_TABLE_ID`，原「2026.3起」；2026-11-06 把「公众号数据统计2025.1-2026.3」的 1584 篇并了进来，旧表原样留着，仪表盘还读它）。程序写 `2025-11-01`（微信新版文章接口最早有数的那天）起的全部文章，群发的和只发表、没通知粉丝的（`publish_type=1`）都写，`发表方式` 列分「群发」「仅发表」；更早的是导入时的数，接口拿不到，程序不碰。首列 `内容标题` 是超链接，点开就是原文；`发表日期` 是日期列。
- 只要有文章在本地还没对上表里的行，就读一遍 `内容标题` 里的链接，按 `mid`+`idx` 认领已有行（第一次接管、老表搬进来的、有人手贴的都算），之后只按记下的 `record_id` 更新，不建重复的行。
- 微信 `getarticletotaldetail` 给的都写（用户 2026-11-06：字段应多尽多）：发表方式、第几条、阅读人数、送达人数（公众号消息阅读 ÷ 送达率）、送达率、阅读完成率、平均阅读秒数（接口给的是分钟）、分享、关注（阅读后关注）、在看、点赞、收藏、留言、赞赏金额（元，接口给的是分）、七个阅读来源、五段跳出位置（读到文章 0-20% 到 80-100% 处跳出的人各占多少）、数据截至（这些累计值是哪天的，发表后第 30 天起定住），以及关联列 `公众号`。品类、国家、作者只填空格子（见下）；`等级` 只在它还等于按表里阅读数算出的档时才跟着变，有人手改过就不动。新文章追加一行，标题（带链接）、发表日期、等级只在新建时写。
- 表里还留着老表导入的阅读次数类列（总阅读次数、总分享次数、首次分享次数、分享产生阅读次数、每次分享带来阅读次数、首次分享率、公众号消息阅读次数、送达阅读率），只有 2025-11 以前的文章有值，新版接口不给，程序不写，在视图里隐藏。
- 表里有人删掉的行记作删除，以后不写、不重建。
- `品类`、`国家` 是多选，选项照资讯订阅的分类（`label-taxonomy.ts`）：品类是食品、饮料、乳制品、零食、酒、调味品、烘焙、功能性食品、预制菜、零售、投融资、上市、融资、并购、投资、财报；国家只有中国、日本，别的国家按资讯订阅的规矩不打。2026-11-06 一次性回填过空格子：2026-03 起的文章用资讯订阅「FBIF食品饮料创新」来源已打的标签（按 mid+idx 对上 811 篇）；更早的用老表 AI 列的旧值，按资讯订阅分类的定义映射（如乳品→乳制品、休闲食品→零食、速冻食品→预制菜、餐饮→食品，美妆、服饰等非食饮不打），AI 写的长段落只认首行明确的结论；两边都没有的 272 篇当时留空。
  之后由程序照资讯订阅的标签补空格子（用户 2026-11-06 定：同一套标准、同一个模型，不另判；`bizdata/labels.go`）：调资讯订阅 `GET /api/internal/wechat-article-labels?biz=&since=`（`Authorization: Bearer $FEED_LABELS_TOKEN`，口令在中台 `token.feed.labels`；地址默认 `https://feed.foodtalks.cn`，`FEED_API_URL` 可改），按链接 mid+idx 对上；只填空着的格子，人填的不碰；深度、创新这类内容属性不是品类，不写。看过的行记在 SQLite `official_biz_labels`（状态同作者），填过被人清空的不填回去。启动后跑一轮，之后只在新建或认领了文章行时被叫醒；资讯订阅还没分类的新文章 2 小时后再问；连续 3 次失败经运维日报通道告警一次。没配口令就不补。
  资讯订阅没收、或收了没分类的（发表满 2 天，多是单发快讯），把标题和正文 HTML 递给资讯订阅的只分类接口 `POST /api/internal/wechat-article-labels/classify`（同一口令，一次 12 篇；用户 2026-11-07 定：已有标签的直接用，剩下的再补）：同一个分类器、同一个模型、同一套摘要截法，不进资讯订阅的库（免得旧文章被当新闻走热点和推送），额度记在资讯订阅的历史补算通道，用完回 429 就等到第二天。正文先用归档 `official_content_articles` 里的，没有才读公开网页；递过去的只是正文开头（去掉属性、注释，留到够 1000 个字，一篇最多 64KB）：资讯订阅的摘要只用去掉标签后的前 180 个字，前面 nginx 一次请求最多 1MB，整页递过去会被回 413（2026-11-07 实测 20 篇，开头和整页算出的摘要全部一样）；读网页和补作者共用一个闸口（两边加起来每 6 秒最多一篇），一轮最多读 50 篇，没补完的隔 1 小时接着补。文章已删的留空；模型或网页连着 3 次没结果的留空；被微信要求验证时这一轮不再读网页（归档有正文的照补）、不记到行上，连着 3 次被要求验证告警一次；资讯订阅不收的号（接口回 404、409）整号留空。
  覆盖范围（2026-11-07 首轮实测）：表里 2668 行，两格都已有值 1329 行不动；资讯订阅分过类、能补的只剩 5 行，已补；资讯订阅分过类但既不是这些品类、也不是中国或日本的 443 行留空；另有 885 行资讯订阅根本没收（每个月都缺三到四成，抽查 6 篇按标题在资讯订阅库里也搜不到，标题多是单发的快讯），2026-11-07 起改走上面的只分类接口补（其中 97 篇归档里有正文，其余读网页）。
- `作者`（单选）由程序补空格子（用户 2026-11-06 定），导入时占位的全角空格也算空；人填的不碰。看过的行按 `record_id` 记在 SQLite `official_biz_authors`（`kept` 本来就有、`written` 程序填了、`none` 找不到署名或文章已删、`gone` 行被删了），之后不再碰，所以程序填过、有人清空的不会填回去。依据按顺序：
  1. 标题认得出的栏目：含「创新周报」「短视频」「百强榜」的照写，FBIF 展会、议程、确认参加、Wow 食品之类写 `FBIF活动`；
  2. 转载文章（网页 `copyright_stat=2`）写原公众号名，照网页上的写法（用户定：统一用公众号原名）。转载卡片上的名字是转载那天的，号改过名就会出两种写法（实测 Gooods好味司 和 Gooods 好味司 是同一个号 `gh_90bc6f3429f4`），所以按网页里的公众号编号 `source_username` 认号，记在状态表 `source` 列，同一个号沿用表里先写下的名字；名字像但编号不同的是两个号，分开写（如 凤凰WEEKLY 和 凤凰WEEKLY财经）；
  3. 没声明原创的写正文里的「来源：」；
  4. 正文开头的「作者：/撰文：/文：」，再看结尾；没有再看「编辑：」。署名照表里人工的归类写（`bizdata/author.go` 的 `bylineLabels`，从 2026-11-06 人工已填的 525 篇群发文章学来，如 kiki→Kiki、Gloria/Mamo→兼职、EZ→投稿、Dan/pai/ossewa→Mote莫特、panda→橙子；百强榜、FBIF活动是按题材归的，不按署名学），没见过的署名照原样；自家号名不算作者；
  5. 都没有就留空。
  读的是公开文章页 `mp.weixin.qq.com/s?…`，不走微信接口、不占接口额度；每 6 秒最多读一篇，先补最近的，每 20 篇写一次，写之前再读一遍这些格子，期间有人填了就不动。启动后跑一轮（接着补历史），之后只在写表那边新建或认领了文章行时被叫醒；一轮里连着 3 篇打不开就停下，从 1 分钟起翻倍退避，连续 3 轮失败经运维日报通道告警一次；同一行 3 次打不开就不再试。单选里没有的名字，飞书写入时自动加成新选项。
  2026-11-06 按「同一个号只用一种写法」改过一次人工值：逐篇读了人工已填的 525 篇网页，转载号（或没声明原创时正文「来源：」）和人工写法是同一个号、写法不同的 31 行改成网页原名（如 Good好味司→Gooods好味司、中国经营连锁协会→中国连锁经营协会、降噪nonosie→降噪NoNoise、财经天下→财经天下WEEKLY、刀法→刀法研究所、数英网→数英DIGITALING），再删掉已没有行在用的 25 个旧写法选项（含卡门、福德利德、降噪noise）；人工按题材归的（百强榜、FBIF活动、运营等）不动。同日程序补完历史后又按公众号编号核对了程序写的转载名：Gooods 好味司→Gooods好味司（38 行）、青山资本→青山资本 Cyanhill Capital，正文「来源：」的 英敏特→Mintel英敏特；切断的署名 Mi→Milo、Ri→Riri。
- `每日数据` 表：第一次跑时自动新建，一天一行，从 `2025-11-01` 起，含账号阅读、互动、发文篇数、各来源阅读、点「阅读原文」的人数、新增/取消/净增/累计关注，以及新增关注按来源拆开（文章内、搜索、名片分享、扫码、视频号、直播、他人转载、广告、小程序、支付后、其他；微信文档外的来源算进其他）。
- `公众号` 表：一号一行（名称、biz）。文章表和每日数据表的 `公众号` 列关联到它，用来按号筛选、分组；文章按链接里的 `__biz` 认号，每日数据记在本号名下。程序第一次用时按表名找到它、按 biz 找到那一行并记下，之后不再查；表里没有这个号时只建一行填 biz，名称留给人填。
- 写入和原始镜像一样靠归档叫醒，本地记 payload hash，没变化的行不调飞书；兜底间隔共用 `OFFICIAL_BASE_SYNC_FALLBACK_SECONDS`。
- 连续失败从 1 分钟起翻倍重试，第 3 次失败时经运维日报通道告警一次。
- 仪表盘 `公众号数据看板`（2026-11-06 卡片停发后，把卡片的思路搬到这里）：
  - 指标卡：上周阅读人数、上周分享人数、上周净增关注，用飞书自带的同环比，时间范围选「上周」（周一到周日），和前一周比；小图是最近 3 个月每天的数。累计关注取表里最新一天。
  - 趋势图：每周阅读人数、每周新增和取关，只画 7 天都有数的周。
  - 不用「最近 7 天」：飞书把今天也算进去，今天还没数，只有 6 天，比前一期偏低。周一 08:05 采集前，「上周」也只有 6 天。
  - 同环比和小图的设置接口读写不到，只能在飞书界面里改。
  - 飞书仪表盘没有表格组件，文章列表看文章表，默认视图最新发的在上面。
- `每日数据` 里两列公式是给看板用的，程序只补缺的列，不动它们：
  - `整周`：这天所在那周的周一；那周 7 天没到齐就空着。
  - `最新一天`：表里最新那天写「是」。

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
- `/api/feishu/sync` 固定返回 `410`；原始镜像表的写入入口只有带历史覆盖门禁的 `/api/feishu/official-sync`。业务数据表走上面单独那条路。

## 部署建议

- 使用 systemd 或类似进程管理器托管编译后的二进制。
- 工作目录保留二进制、`.env` 和 `.sync-cursor.json`。
- 2026-09-15 起服务运行在资讯机（FBIF-内部工具服务器 101.133.154.40），与 feed.foodtalks.cn 同机；工作目录 `/opt/fbif-wechat-article-dashboard/bin`，归档库 `/root/mp-archive/mp-archive.db`。此前的 GitHub Actions SSH 部署已移除：资讯机 22 端口只对少数运维 IP 开放，统一改走 `scripts/deploy-cloud-assistant.sh`。环境文件 `.env` 由服务器持有，不再经 Secrets 下发。`WECHAT_SECRET`、`API_KEY`、`PUBLISH_SYNC_SERVICE_TOKEN` 2026-10-26 起由 FBIF 中台的本机代理写进 `/etc/fbif-hub/env/wechat-sync.env`，systemd drop-in `wechat-sync.service.d/zz-fbif-hub.conf` 把它带进进程环境；程序读 `.env` 时不覆盖环境里已有的值，所以以中台为准，`.env` 里不再写这三项。换值在内部工具服务器上 `hubctl put <编号>`，见 FBIF-One-Api `docs/hub/公共变量和密钥.md`。
- 监控探针（FoodTalks_Feed 的 `monitoring/watchdog.py`）与本服务同机，直接探 `127.0.0.1:3002`，不再经 Caddy 跨机反代。
- 不要把一次性迁移或重型脚本放进常驻同步服务的启动流程。

## 注意事项

- `.sync-cursor.json` 是旧直连同步留下的进度文件，不是业务数据；现行代码只读不写。
- 6 个旧文章分析接口已由微信返回 `47009 this api is offline, please use the new api`；服务不会无限重试，而是记录下线响应并使用新接口。
- `freepublish/batchget` 仅提供一条官方发布对象采集流；它的分页完成不等于历史文章全量已经核验。本服务不用网页或订阅源补造数据，历史全量结论按 [已发布文章真值合同](docs/PUBLISHED-ARTICLE-TRUTH-CONTRACT.md) 执行。
