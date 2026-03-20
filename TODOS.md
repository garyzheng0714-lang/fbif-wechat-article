# TODOS

## P1: 文章详情回填功能 (xReader + freepublish)

**What:** 添加文章详情回填服务：Phase 1 通过 freepublish API 匹配文章链接/作者/摘要/封面图，Phase 2 通过 xReader 获取文章全文。

**Why:** 当前同步只有文章统计数据，缺少文章链接和正文内容，限制了数据分析的价值。

**Effort:** M (human) → S (CC)

**Depends on:** None

---

## P2: Bitable 批量操作部分失败处理

**What:** `batch_create` / `batch_update` 调用后检查单条记录的成功/失败状态，对失败记录重试或记录日志。

**Why:** 当前静默忽略部分失败，可能导致数据丢失而无感知。

**Effort:** S (human) → S (CC)

**Depends on:** None

---

## P2: HTTP 端点添加基础认证

**What:** 给 `/api/feishu/sync`、`/api/feishu/reset`、`/api/feishu/backfill` 添加 API key 或 Bearer token 认证。

**Why:** 当前任何人知道 IP + 端口就能触发同步或清空数据，`/reset` 尤其危险。

**Effort:** S (human) → S (CC)

**Depends on:** None

---

## P3: Bitable 缓存过期机制

**What:** 为 tableIDCache 添加 TTL（如 1 小时），避免长期运行时缓存与飞书 UI 手动修改不一致。

**Why:** 服务长期运行时，如果有人在飞书 UI 手动修改了表或字段，内存缓存不会更新。

**Effort:** S (human) → S (CC)

**Depends on:** None
