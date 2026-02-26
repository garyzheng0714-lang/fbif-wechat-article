import { env } from '../config/env.js';

interface TokenState {
  accessToken: string;
  expiresAt: number;
}

interface Credentials {
  appid: string;
  secret: string;
}

// Runtime credentials — initialized from env, can be updated via API
let credentials: Credentials = {
  appid: env.WECHAT_APPID,
  secret: env.WECHAT_SECRET,
};

let cached: TokenState | null = null;
let refreshTimer: ReturnType<typeof setTimeout> | null = null;

export function getCredentials(): Credentials {
  return { ...credentials };
}

export async function setCredentials(appid: string, secret: string): Promise<void> {
  credentials = { appid, secret };
  // Clear existing token and timer
  cached = null;
  if (refreshTimer) {
    clearTimeout(refreshTimer);
    refreshTimer = null;
  }
  // Immediately validate by fetching a new token
  await getToken();
  console.log(`[Token] Credentials updated, appid=${appid.slice(0, 6)}****`);
}

async function fetchToken(): Promise<TokenState> {
  if (!credentials.appid || !credentials.secret) {
    throw new Error('WeChat credentials not configured. Please set AppID and AppSecret.');
  }

  const url = new URL('https://api.weixin.qq.com/cgi-bin/token');
  url.searchParams.set('grant_type', 'client_credential');
  url.searchParams.set('appid', credentials.appid);
  url.searchParams.set('secret', credentials.secret);

  const res = await fetch(url.toString());
  const data = await res.json();

  if (data.errcode) {
    throw new Error(`WeChat token error ${data.errcode}: ${data.errmsg}`);
  }

  const expiresAt = Date.now() + (data.expires_in - 600) * 1000; // 10min buffer
  console.log(`[Token] Fetched new access_token, expires in ${data.expires_in}s`);

  return { accessToken: data.access_token, expiresAt };
}

function scheduleRefresh() {
  if (refreshTimer) clearTimeout(refreshTimer);
  if (!cached) return;

  const delay = Math.max(0, cached.expiresAt - Date.now() - 60_000);
  refreshTimer = setTimeout(async () => {
    try {
      cached = await fetchToken();
      scheduleRefresh();
    } catch (err) {
      console.error('[Token] Auto-refresh failed:', err);
      // Retry in 30s
      refreshTimer = setTimeout(() => scheduleRefresh(), 30_000);
    }
  }, delay);
}

export async function getToken(): Promise<string> {
  if (cached && Date.now() < cached.expiresAt) {
    return cached.accessToken;
  }
  cached = await fetchToken();
  scheduleRefresh();
  return cached.accessToken;
}

export async function refreshTokenNow(): Promise<string> {
  cached = await fetchToken();
  scheduleRefresh();
  return cached.accessToken;
}

export function getTokenStatus(): 'valid' | 'expired' | 'uninitialized' {
  if (!cached) return 'uninitialized';
  return Date.now() < cached.expiresAt ? 'valid' : 'expired';
}
