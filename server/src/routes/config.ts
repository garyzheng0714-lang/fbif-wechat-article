import { Router } from 'express';
import { z } from 'zod';
import { getCredentials, setCredentials } from '../services/wechatToken.js';

export const configRouter = Router();

const credentialSchema = z.object({
  appid: z.string().min(1, 'AppID is required'),
  secret: z.string().min(1, 'AppSecret is required'),
});

// Get current credential status (never expose the secret)
configRouter.get('/status', (_req, res) => {
  const creds = getCredentials();
  res.json({
    configured: !!(creds.appid && creds.secret),
    appid: creds.appid ? creds.appid.slice(0, 6) + '****' : '',
  });
});

// Save credentials and immediately try to get a token
configRouter.post('/credentials', async (req, res, next) => {
  try {
    const parsed = credentialSchema.parse(req.body);
    await setCredentials(parsed.appid, parsed.secret);
    const creds = getCredentials();
    res.json({
      success: true,
      appid: creds.appid.slice(0, 6) + '****',
    });
  } catch (err) {
    next(err);
  }
});
