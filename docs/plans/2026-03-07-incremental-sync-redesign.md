# 飞书多维表格增量同步重构 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将飞书同步改为事实表+维度表模型，添加截断点机制避免重复API调用，支持从新到旧分批全量回填。

**Architecture:** 文章总表（维度表，每篇文章一行，去重写入）+ 每日阅读数据表（事实表，msgid+refDate 唯一键，纯增量追加）。用 `.sync-cursor.json` 文件持久化截断点（已同步到的最早日期），每天只追 1 天新数据，后台从截断点继续往更早回填，API 返回空数据时停止。cursor 文件放在 APP_ROOT 下（跨 release 持久化）。

**Tech Stack:** TypeScript, Express, node-cron, Feishu Bitable API

---

## 数据模型变更

### 文章总表（维度表） — 使用现有 `tblXqpIPwCVxAOw3`
- 唯一键: `msgid`（消息ID 字段，已存在）
- 保留字段: 文章标题、发布日期、发布月份、消息ID、文章位置、文章链接、作者、摘要、封面图、几点发布
- 删除字段: 所有阅读/分享/收藏数据字段（这些移到事实表）
- 新增字段: 无（后续通过飞书查找引用关联事实表求和）
- 写入逻辑: 存在就跳过（不更新），不存在就新增

### 每日阅读数据表（事实表） — 新建 `每日文章数据`
- 唯一键: `msgid_refDate`（唯一键字段）
- 字段: 唯一键、消息ID（用于查找引用）、日期、图文页阅读人数/次数、原文页阅读人数/次数、分享人数/次数、收藏人数/次数、送达人数、会话/历史消息/朋友圈/好友转发/其他来源 阅读人数/次数、更新时间
- 写入逻辑: 唯一键已存在就跳过，不存在就新增

### 粉丝增长、每日阅读概况、分享场景 — 保持不变

---

## Task 1: 创建 SyncCursor 模块

**Files:**
- Create: `server/src/services/syncCursor.ts`

**Step 1: 实现 cursor 读写**

```typescript
// server/src/services/syncCursor.ts
import { readFileSync, writeFileSync, existsSync } from 'fs';
import { resolve } from 'path';

interface SyncCursor {
  oldestSyncedDate: string;  // 已同步到的最早日期 YYYY-MM-DD
  newestSyncedDate: string;  // 已同步到的最新日期 YYYY-MM-DD
  backfillComplete: boolean; // API 返回空数据，回填完成
}

// cursor 文件路径：APP_ROOT/.sync-cursor.json（跨 release 持久化）
function getCursorPath(): string {
  const appRoot = process.env.APP_DIR || resolve(process.cwd(), '..');
  // 如果当前目录就是项目根目录（非 release 部署模式），就放当前目录
  const cursorDir = existsSync(resolve(appRoot, '.sync-cursor.json'))
    ? appRoot
    : process.cwd();
  return resolve(cursorDir, '.sync-cursor.json');
}

export function readCursor(): SyncCursor | null {
  // 尝试多个可能的路径
  const paths = [
    resolve(process.cwd(), '.sync-cursor.json'),
    resolve(process.cwd(), '..', '.sync-cursor.json'),
  ];
  for (const p of paths) {
    if (existsSync(p)) {
      const raw = readFileSync(p, 'utf-8');
      return JSON.parse(raw) as SyncCursor;
    }
  }
  return null;
}

export function writeCursor(cursor: SyncCursor): void {
  const p = resolve(process.cwd(), '.sync-cursor.json');
  writeFileSync(p, JSON.stringify(cursor, null, 2), 'utf-8');
}
```

**Step 2: 提交**

```bash
git add server/src/services/syncCursor.ts
git commit -m "feat: add sync cursor module for tracking backfill progress"
```

---

## Task 2: 重构 feishuBitable.ts — 拆分文章总表和每日数据表

**Files:**
- Modify: `server/src/services/feishuBitable.ts`

**核心变更:**

1. `ArticleRecord` 类型拆分为 `ArticleMasterRecord`（维度表字段）和 `DailyArticleDataRecord`（事实表字段）
2. `syncArticlesToBitable` 改为只写入维度表（去重，存在跳过）
3. 新增 `syncDailyArticleDataToBitable` 写入事实表（唯一键 = `msgid_refDate`，存在跳过）
4. 事实表字段定义 `DAILY_ARTICLE_DATA_FIELDS`
5. 维度表字段定义精简（移除阅读/分享数据字段）
6. 新增 `clearAllRecords` 函数用于清空表

**Step 1: 修改类型和字段定义**

将 `ArticleRecord` 拆为两个接口:
- `ArticleMasterRecord`: title, refDate, msgid, articleIndex, articleUrl, author, digest, thumbUrl, contentSourceUrl, publishMonth, publishHour
- `DailyArticleDataRecord`: msgid, refDate, intPageReadUser/Count, oriPageReadUser/Count, shareUser/Count, addToFavUser/Count, targetUser, 各来源字段

**Step 2: 精简文章总表字段**

```typescript
const ARTICLE_MASTER_FIELDS: FieldSpec[] = [
  { name: '文章标题', type: FIELD_TYPE_TEXT },
  { name: '发布日期', type: FIELD_TYPE_DATETIME },
  { name: '发布月份', type: FIELD_TYPE_TEXT },
  { name: '消息ID', type: FIELD_TYPE_TEXT },
  { name: '文章位置', type: FIELD_TYPE_NUMBER },
  { name: '文章链接', type: FIELD_TYPE_URL },
  { name: '作者', type: FIELD_TYPE_TEXT },
  { name: '摘要', type: FIELD_TYPE_TEXT },
  { name: '封面图', type: FIELD_TYPE_URL },
  { name: '几点发布', type: FIELD_TYPE_TEXT },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
];
```

**Step 3: 新增每日数据表字段**

```typescript
const DAILY_ARTICLE_DATA_FIELDS: FieldSpec[] = [
  { name: '唯一键', type: FIELD_TYPE_TEXT },
  { name: '消息ID', type: FIELD_TYPE_TEXT },
  { name: '日期', type: FIELD_TYPE_DATETIME },
  { name: '图文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '图文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '分享人数', type: FIELD_TYPE_NUMBER },
  { name: '分享次数', type: FIELD_TYPE_NUMBER },
  { name: '收藏人数', type: FIELD_TYPE_NUMBER },
  { name: '收藏次数', type: FIELD_TYPE_NUMBER },
  { name: '送达人数', type: FIELD_TYPE_NUMBER },
  { name: '会话阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '会话阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '历史消息阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '历史消息阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '朋友圈阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '朋友圈阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '好友转发阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '好友转发阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '其他来源阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '其他来源阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
];
```

**Step 4: 重写 syncArticlesToBitable**

改为只写入维度表，去重逻辑：已存在的 msgid 跳过不更新。

**Step 5: 新增 syncDailyArticleDataToBitable**

写入事实表 `每日文章数据`，唯一键 = `msgid_refDate`，已存在跳过。

**Step 6: 新增 clearAllRecords 函数**

用于首次运行时清空旧数据。通过分页获取所有 record_id 然后 batch_delete。

**Step 7: 提交**

```bash
git add server/src/services/feishuBitable.ts
git commit -m "refactor: split article table into master (dimension) + daily data (fact) tables"
```

---

## Task 3: 重构 scheduler.ts — 截断点驱动的同步策略

**Files:**
- Modify: `server/src/services/scheduler.ts`

**核心变更:**

1. `runDailySync` 改为只同步 cursor.newestSyncedDate+1 到昨天（增量追新）
2. `runBackfillSync` 改为从 cursor.oldestSyncedDate 继续往更早拉，每次 7 天 chunk，拉完更新 cursor
3. 每个 chunk 完成后更新 cursor 文件
4. API 返回空数据时标记 backfillComplete
5. syncArticles 拆分为 syncArticleMaster + syncDailyArticleData 两步
6. 启动时不再无条件回填 60 天，而是根据 cursor 决定行为

**Step 1: 修改 syncArticles 函数**

拆分为两步调用:
1. 构建 ArticleMasterRecord 列表，调 syncArticlesToBitable（去重写入维度表）
2. 构建 DailyArticleDataRecord 列表，调 syncDailyArticleDataToBitable（增量写入事实表）

**Step 2: 重写 runDailySync**

```typescript
export async function runDailySync(): Promise<void> {
  const cursor = readCursor();
  const yesterday = getYesterday();

  if (cursor && cursor.newestSyncedDate >= yesterday) {
    console.log('[Scheduler] Already synced up to yesterday, skipping');
    return;
  }

  // 从 cursor.newestSyncedDate 的第二天开始，到昨天
  const beginDate = cursor
    ? formatDate(new Date(new Date(cursor.newestSyncedDate).getTime() + 86400000))
    : yesterday; // 无 cursor 时只拉昨天

  console.log(`[Scheduler] Daily sync: ${beginDate} ~ ${yesterday}`);
  await runFullSync(beginDate, yesterday);

  // 更新 cursor
  writeCursor({
    oldestSyncedDate: cursor?.oldestSyncedDate || beginDate,
    newestSyncedDate: yesterday,
    backfillComplete: cursor?.backfillComplete || false,
  });
}
```

**Step 3: 重写 runBackfillSync**

```typescript
export async function runBackfillSync(): Promise<void> {
  const cursor = readCursor();

  if (cursor?.backfillComplete) {
    console.log('[Scheduler] Backfill already complete');
    return;
  }

  // 从 cursor.oldestSyncedDate 往更早拉
  const startFrom = cursor?.oldestSyncedDate || getYesterday();
  const chunkSize = 7;
  let currentEnd = formatDate(new Date(new Date(startFrom).getTime() - 86400000));

  for (let i = 0; i < 52; i++) { // 最多 52 周 ≈ 1 年安全上限
    const chunkBeginDate = new Date(new Date(currentEnd).getTime() - (chunkSize - 1) * 86400000);
    const chunkBegin = formatDate(chunkBeginDate);

    console.log(`[Scheduler] Backfill chunk: ${chunkBegin} ~ ${currentEnd}`);
    const results = await runFullSync(chunkBegin, currentEnd);

    // 检查是否所有 API 都返回空数据
    const allEmpty = Object.values(results).every(
      r => 'total' in r && r.total === 0
    );

    // 更新 cursor
    writeCursor({
      oldestSyncedDate: chunkBegin,
      newestSyncedDate: cursor?.newestSyncedDate || getYesterday(),
      backfillComplete: allEmpty,
    });

    if (allEmpty) {
      console.log(`[Scheduler] Backfill complete: no data before ${chunkBegin}`);
      break;
    }

    currentEnd = formatDate(new Date(chunkBeginDate.getTime() - 86400000));
  }
}
```

**Step 4: 提交**

```bash
git add server/src/services/scheduler.ts
git commit -m "refactor: cursor-driven incremental sync, no duplicate API calls"
```

---

## Task 4: 更新 feishu.ts 路由 + index.ts 启动逻辑

**Files:**
- Modify: `server/src/routes/feishu.ts`
- Modify: `server/src/index.ts`

**Step 1: 更新 feishu.ts**

- `/sync` 路由拆分为写入维度表 + 事实表两步
- `/backfill` 路由改为调用新的 cursor 驱动的 backfillSync
- 新增 `/reset` 路由：清空所有表数据 + 删除 cursor 文件，用于首次全量重拉
- 新增 `/cursor` GET 路由：查看当前截断点状态

**Step 2: 更新 index.ts 启动逻辑**

```typescript
// 启动时：
// 1. 如果有 cursor → 先 dailySync 追新，再后台 backfill 继续往更早拉
// 2. 如果无 cursor → 后台启动 backfill（从昨天开始往前拉）
startScheduler();

const cursor = readCursor();
if (cursor) {
  runDailySync().then(() => runBackfillSync()).catch(err => {
    console.error('[Startup] Sync failed:', err);
  });
} else {
  runBackfillSync().catch(err => {
    console.error('[Startup] Initial backfill failed:', err);
  });
}
```

**Step 3: 提交**

```bash
git add server/src/routes/feishu.ts server/src/index.ts
git commit -m "feat: add /reset and /cursor endpoints, cursor-driven startup"
```

---

## Task 5: 构建、部署、验证

**Step 1: 本地构建验证**

```bash
cd server && npm run build
```

确保 TypeScript 编译无错误。

**Step 2: 提交并推送到 main**

```bash
git push origin HEAD:main
```

等待 GitHub Actions 自动部署。

**Step 3: 部署后验证**

1. 检查 health: `curl http://localhost:3002/health`
2. 查看 cursor 状态: `curl http://localhost:3002/api/feishu/cursor`
3. 检查 PM2 日志确认同步正常启动

**Step 4: 清空旧数据并启动全量回填（可选）**

如果用户确认要清空重来:
```bash
curl -X POST http://localhost:3002/api/feishu/reset
```

---

## Task 6: 手动在飞书配置查找引用（用户操作，非代码）

在飞书多维表格中：
1. 打开文章总表
2. 添加"查找引用"字段，关联"每日文章数据"表，匹配字段 = 消息ID
3. 在查找引用基础上添加"汇总"字段，对"图文页阅读人数"求和 → 得到累计阅读量
4. 同理可汇总分享、收藏等

---

## 实现顺序总结

1. Task 1: syncCursor.ts — 新文件
2. Task 2: feishuBitable.ts — 拆分维度表+事实表
3. Task 3: scheduler.ts — 截断点驱动同步
4. Task 4: feishu.ts + index.ts — 路由和启动逻辑
5. Task 5: 构建部署验证
6. Task 6: 飞书手动配置查找引用
