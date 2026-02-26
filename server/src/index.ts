import express from 'express';
import cors from 'cors';
import { env } from './config/env.js';
import { errorHandler } from './middleware/errors.js';
import { wechatRouter } from './routes/wechat.js';
import { configRouter } from './routes/config.js';
import { feishuRouter } from './routes/feishu.js';
import { getTokenStatus } from './services/wechatToken.js';

const app = express();

app.use(cors({ origin: env.CLIENT_ORIGIN }));
app.use(express.json());

app.get('/health', (_req, res) => {
  res.json({ status: 'ok', tokenStatus: getTokenStatus() });
});

app.use('/api/wechat', wechatRouter);
app.use('/api/config', configRouter);
app.use('/api/feishu', feishuRouter);

app.use(errorHandler);

app.listen(env.SERVER_PORT, () => {
  console.log(`Server running on http://localhost:${env.SERVER_PORT}`);
});
