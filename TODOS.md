# TODOS

## P2: Bitable 批量操作部分失败处理

**What:** `batch_create` / `batch_update` 调用后检查单条记录的成功/失败状态，对失败记录重试或记录日志。

**Why:** 当前静默忽略部分失败，可能导致数据丢失而无感知。

**Effort:** S (human) → S (CC)

**Depends on:** None (Go rewrite complete as of v1.0.0.0)

---

## P2: HTTP 端点添加基础认证

**What:** 给 `/api/feishu/sync`、`/api/feishu/reset`、`/api/feishu/backfill` 添加 API key 或 Bearer token 认证。

**Why:** 当前任何人知道 IP + 端口就能触发同步或清空数据，`/reset` 尤其危险。

**Effort:** S (human) → S (CC)

**Depends on:** None (Go rewrite complete as of v1.0.0.0)
