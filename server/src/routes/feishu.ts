import { Router } from 'express';
import { runDailySync, runBackfillSync } from '../services/scheduler.js';
import { readCursor, deleteCursor } from '../services/syncCursor.js';
import { clearAllTables } from '../services/feishuBitable.js';

export const feishuRouter = Router();

// ==================== 增量同步（每日） ====================
feishuRouter.post('/sync', async (_req, res, next) => {
  try {
    console.log('[Feishu Route] Triggering daily sync...');
    await runDailySync();
    const cursor = readCursor();
    res.json({
      success: true,
      message: 'Daily sync completed',
      cursor,
    });
  } catch (err) {
    next(err);
  }
});

// ==================== 回填同步 ====================
feishuRouter.post('/backfill', async (_req, res, next) => {
  try {
    console.log('[Feishu Route] Triggering backfill sync...');
    res.json({ success: true, message: 'Backfill started. Check server logs for progress.' });

    runBackfillSync().catch((err) => {
      console.error('[Feishu Route] Backfill failed:', err);
    });
  } catch (err) {
    next(err);
  }
});

// ==================== 查看游标状态 ====================
feishuRouter.get('/cursor', (_req, res) => {
  const cursor = readCursor();
  res.json({ success: true, cursor });
});

// ==================== 重置（清空数据+游标） ====================
feishuRouter.post('/reset', async (_req, res, next) => {
  try {
    console.log('[Feishu Route] Resetting all data...');

    const deleted = await clearAllTables();
    deleteCursor();

    console.log(`[Feishu Route] Reset complete:`, deleted);

    res.json({
      success: true,
      message: 'All data cleared and cursor reset',
      deleted,
    });
  } catch (err) {
    next(err);
  }
});
