import { env } from '../config/env.js';

interface TokenState {
  tenantAccessToken: string;
  expiresAt: number;
}

let cached: TokenState | null = null;

async function fetchTenantToken(): Promise<TokenState> {
  if (!env.FEISHU_APP_ID || !env.FEISHU_APP_SECRET) {
    throw new Error('Feishu credentials not configured. Set FEISHU_APP_ID and FEISHU_APP_SECRET.');
  }

  const res = await fetch('https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      app_id: env.FEISHU_APP_ID,
      app_secret: env.FEISHU_APP_SECRET,
    }),
  });

  const data = (await res.json()) as { code: number; msg: string; tenant_access_token: string; expire: number };

  if (data.code !== 0) {
    throw new Error(`Feishu token error: ${data.msg}`);
  }

  console.log(`[Feishu] Fetched tenant_access_token, expires in ${data.expire}s`);

  return {
    tenantAccessToken: data.tenant_access_token,
    expiresAt: Date.now() + (data.expire - 300) * 1000, // 5min buffer
  };
}

export async function getFeishuToken(): Promise<string> {
  if (cached && Date.now() < cached.expiresAt) {
    return cached.tenantAccessToken;
  }
  cached = await fetchTenantToken();
  return cached.tenantAccessToken;
}
