import { getToken, refreshTokenNow } from './wechatToken.js';

const WECHAT_API_BASE = 'https://api.weixin.qq.com/datacube';

// In-memory response cache
const cache = new Map<string, { data: unknown; expiresAt: number }>();

function getCacheTTL(endDate: string): number {
  const today = new Date().toISOString().slice(0, 10);
  const yesterday = new Date(Date.now() - 86400000).toISOString().slice(0, 10);
  // Yesterday's data may still update (available after 8AM), use short TTL
  if (endDate >= yesterday) return 60_000; // 60s
  // Older data never changes
  return 3600_000; // 1 hour
}

function getCacheKey(endpoint: string, beginDate: string, endDate: string): string {
  return `${endpoint}:${beginDate}:${endDate}`;
}

// Simple concurrency limiter
async function withConcurrency<T>(tasks: (() => Promise<T>)[], limit: number): Promise<T[]> {
  const results: T[] = [];
  const executing: Promise<void>[] = [];

  for (const task of tasks) {
    const p = task().then((r) => {
      results.push(r);
    });
    executing.push(p);

    if (executing.length >= limit) {
      await Promise.race(executing);
      executing.splice(
        executing.findIndex((e) => e === p),
        1,
      );
    }
  }

  await Promise.all(executing);
  return results;
}

// Get all dates in range as YYYY-MM-DD strings
function getDateRange(beginDate: string, endDate: string): string[] {
  const dates: string[] = [];
  const start = new Date(beginDate);
  const end = new Date(endDate);
  for (let d = new Date(start); d <= end; d.setDate(d.getDate() + 1)) {
    dates.push(d.toISOString().slice(0, 10));
  }
  return dates;
}

async function callWechatApiSingle(
  endpoint: string,
  token: string,
  beginDate: string,
  endDate: string,
): Promise<unknown> {
  const url = `${WECHAT_API_BASE}/${endpoint}?access_token=${token}`;
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ begin_date: beginDate, end_date: endDate }),
  });
  return res.json();
}

// Max date span per API
const MAX_SPAN: Record<string, number> = {
  getarticlesummary: 1,
  getarticletotal: 1,
  getuserread: 3,
  getuserreadhour: 1,
  getusershare: 7,
  getusersharehour: 1,
};

export async function callWechatApi(
  endpoint: string,
  beginDate: string,
  endDate: string,
): Promise<{ list: unknown[] }> {
  // Check cache
  const cacheKey = getCacheKey(endpoint, beginDate, endDate);
  const cached = cache.get(cacheKey);
  if (cached && Date.now() < cached.expiresAt) {
    return cached.data as { list: unknown[] };
  }

  const token = await getToken();
  const maxSpan = MAX_SPAN[endpoint] || 1;
  const dates = getDateRange(beginDate, endDate);

  let allItems: unknown[] = [];

  if (dates.length <= maxSpan) {
    // Single request
    const data = (await callWechatApiSingle(endpoint, token, beginDate, endDate)) as Record<string, unknown>;

    // Handle token expiry - retry once
    if (data.errcode === 40001) {
      const newToken = await refreshTokenNow();
      const retryData = (await callWechatApiSingle(endpoint, newToken, beginDate, endDate)) as Record<string, unknown>;
      if (retryData.errcode) {
        throw new Error(`WeChat API error ${retryData.errcode}: ${retryData.errmsg}`);
      }
      allItems = (retryData.list as unknown[]) || [];
    } else if (data.errcode) {
      throw new Error(`WeChat API error ${data.errcode}: ${data.errmsg}`);
    } else {
      allItems = (data.list as unknown[]) || [];
    }
  } else {
    // Split into chunks and fetch in parallel
    const chunks: { begin: string; end: string }[] = [];
    for (let i = 0; i < dates.length; i += maxSpan) {
      const chunk = dates.slice(i, i + maxSpan);
      chunks.push({ begin: chunk[0], end: chunk[chunk.length - 1] });
    }

    const results = await withConcurrency(
      chunks.map(({ begin, end }) => async () => {
        const t = await getToken();
        const data = (await callWechatApiSingle(endpoint, t, begin, end)) as Record<string, unknown>;
        if (data.errcode === 40001) {
          const newToken = await refreshTokenNow();
          const retryData = (await callWechatApiSingle(endpoint, newToken, begin, end)) as Record<string, unknown>;
          if (retryData.errcode) {
            console.error(`WeChat API error for ${endpoint} [${begin}~${end}]:`, retryData.errmsg);
            return [];
          }
          return (retryData.list as unknown[]) || [];
        }
        if (data.errcode) {
          console.error(`WeChat API error for ${endpoint} [${begin}~${end}]:`, data.errmsg);
          return [];
        }
        return (data.list as unknown[]) || [];
      }),
      5,
    );

    allItems = results.flat();
  }

  const result = { list: allItems };

  // Cache the result
  cache.set(cacheKey, {
    data: result,
    expiresAt: Date.now() + getCacheTTL(endDate),
  });

  return result;
}
