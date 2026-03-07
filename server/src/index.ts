import express from 'express';
import { env } from './config/env.js';
import { errorHandler } from './middleware/errors.js';
import { feishuRouter } from './routes/feishu.js';
import { getTokenStatus } from './services/wechatToken.js';
import { readCursor } from './services/syncCursor.js';
import { startScheduler, runDailySync, runBackfillSync } from './services/scheduler.js';

const app = express();

app.use(express.json());

app.get('/health', (_req, res) => {
  res.json({ status: 'ok', tokenStatus: getTokenStatus(), cursor: readCursor() });
});

app.use('/api/feishu', feishuRouter);

app.use(errorHandler);

app.listen(env.SERVER_PORT, () => {
  console.log(`Server running on http://localhost:${env.SERVER_PORT}`);

  startScheduler();

  (async () => {
    try {
      await runDailySync();
      await runBackfillSync();
    } catch (err) {
      console.error('[Startup] Sync failed:', err);
    }
  })();
});
