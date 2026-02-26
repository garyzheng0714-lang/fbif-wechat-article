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
configRouter.post('/credentials', async (req, res) => {
  try {
    const parsed = credentialSchema.parse(req.body);
    await setCredentials(parsed.appid, parsed.secret);
    const creds = getCredentials();
    res.json({
      success: true,
      appid: creds.appid.slice(0, 6) + '****',
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Unknown error';

    // Parse WeChat error codes for specific hints
    const errCodeMatch = message.match(/WeChat token error (\d+)/);
    const errCode = errCodeMatch ? Number(errCodeMatch[1]) : 0;

    let hint = '';
    if (errCode === 40001 || errCode === 40125) {
      hint = 'AppSecret 不正确，请检查微信公众号后台的开发者密码';
    } else if (errCode === 40164) {
      hint = '服务器 IP 未加入白名单，请在微信公众号后台 → 设置与开发 → 基本配置 → IP白名单中添加服务器 IP';
    } else if (errCode === 40013) {
      hint = 'AppID 不正确，请检查微信公众号后台的开发者ID';
    }

    res.status(400).json({
      error: message,
      errCode,
      hint,
    });
  }
});
