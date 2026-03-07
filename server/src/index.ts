import express from 'express';
import cors from 'cors';
import { fileURLToPath } from 'url';
import { dirname, resolve } from 'path';
import { env } from './config/env.js';
import { errorHandler } from './middleware/errors.js';
import { wechatRouter } from './routes/wechat.js';
import { configRouter } from './routes/config.js';
import { feishuRouter } from './routes/feishu.js';
import { getTokenStatus } from './services/wechatToken.js';
import { startScheduler, runDailySync, runBackfillSync } from './services/scheduler.js';

const __dirname = dirname(fileURLToPath(import.meta.url));
const app = express();

app.use(cors({ origin: env.CLIENT_ORIGIN }));
app.use(express.json());

app.get('/health', (_req, res) => {
  res.json({ status: 'ok', tokenStatus: getTokenStatus() });
});

app.use('/api/wechat', wechatRouter);
app.use('/api/config', configRouter);
app.use('/api/feishu', feishuRouter);

// Serve client static files in production
if (env.NODE_ENV === 'production') {
  const clientDist = resolve(__dirname, '../../client/dist');
  app.use(express.static(clientDist));
  app.get('*', (_req, res) => {
    res.sendFile(resolve(clientDist, 'index.html'));
  });
}

app.use(errorHandler);

app.listen(env.SERVER_PORT, () => {
  console.log(`Server running on http://localhost:${env.SERVER_PORT}`);

  startScheduler();

  // Cursor-driven startup: daily sync first, then backfill
  (async () => {
    try {
      await runDailySync();
      await runBackfillSync();
    } catch (err) {
      console.error('[Startup] Sync failed:', err);
    }
  })();
});
