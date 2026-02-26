import { env } from '../config/env.js';
import { getFeishuToken } from './feishuToken.js';

const BITABLE_API = 'https://open.feishu.cn/open-apis/bitable/v1/apps';

interface BitableRecord {
  fields: Record<string, unknown>;
}

interface ArticleRecord {
  title: string;
  refDate: string;
  msgid: string;
  intPageReadUser: number;
  intPageReadCount: number;
  oriPageReadUser: number;
  oriPageReadCount: number;
  shareUser: number;
  shareCount: number;
  addToFavUser: number;
  addToFavCount: number;
}

function toBitableFields(article: ArticleRecord): Record<string, unknown> {
  const dateMs = new Date(article.refDate).getTime();

  return {
    '文章标题': article.title,
    '发布日期': dateMs,
    '消息ID': article.msgid,
    '图文页阅读人数': article.intPageReadUser,
    '图文页阅读次数': article.intPageReadCount,
    '原文页阅读人数': article.oriPageReadUser,
    '原文页阅读次数': article.oriPageReadCount,
    '分享人数': article.shareUser,
    '分享次数': article.shareCount,
    '收藏人数': article.addToFavUser,
    '收藏次数': article.addToFavCount,
    '更新时间': Date.now(),
  };
}

async function feishuRequest(method: string, path: string, body?: unknown): Promise<unknown> {
  const token = await getFeishuToken();
  const url = `${BITABLE_API}/${env.FEISHU_BITABLE_APP_TOKEN}/tables/${env.FEISHU_BITABLE_TABLE_ID}${path}`;

  const res = await fetch(url, {
    method,
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  const data = (await res.json()) as { code: number; msg: string; data?: unknown };

  if (data.code !== 0) {
    throw new Error(`Feishu Bitable API error ${data.code}: ${data.msg}`);
  }

  return data.data;
}

// Fetch existing records and return a map of msgid → record_id
async function getExistingRecords(): Promise<Map<string, string>> {
  const recordMap = new Map<string, string>();
  let pageToken: string | undefined;
  let hasMore = true;

  while (hasMore) {
    const params = new URLSearchParams({ page_size: '500' });
    if (pageToken) params.set('page_token', pageToken);

    const result = (await feishuRequest('GET', `/records?${params}`)) as {
      items?: { record_id: string; fields: Record<string, unknown> }[];
      has_more: boolean;
      page_token?: string;
    };

    if (result.items) {
      for (const item of result.items) {
        const msgid = String(item.fields['消息ID'] || '');
        if (msgid) {
          recordMap.set(msgid, item.record_id);
        }
      }
    }

    hasMore = result.has_more;
    pageToken = result.page_token;
  }

  return recordMap;
}

export async function syncArticlesToBitable(articles: ArticleRecord[]): Promise<{ created: number; updated: number }> {
  if (articles.length === 0) {
    return { created: 0, updated: 0 };
  }

  // Get existing records: msgid → record_id
  const existingRecords = await getExistingRecords();

  const createList: BitableRecord[] = [];
  const updateList: { record_id: string; fields: Record<string, unknown> }[] = [];

  for (const article of articles) {
    const fields = toBitableFields(article);
    const existingRecordId = existingRecords.get(article.msgid);

    if (existingRecordId) {
      updateList.push({ record_id: existingRecordId, fields });
    } else {
      createList.push({ fields });
    }
  }

  // Batch create new records (max 500 per request)
  let created = 0;
  for (let i = 0; i < createList.length; i += 500) {
    const batch = createList.slice(i, i + 500);
    await feishuRequest('POST', '/records/batch_create', { records: batch });
    created += batch.length;
    console.log(`[Feishu] Batch created ${batch.length} records (${created}/${createList.length})`);
  }

  // Batch update existing records (max 500 per request)
  let updated = 0;
  for (let i = 0; i < updateList.length; i += 500) {
    const batch = updateList.slice(i, i + 500);
    await feishuRequest('POST', '/records/batch_update', { records: batch });
    updated += batch.length;
    console.log(`[Feishu] Batch updated ${batch.length} records (${updated}/${updateList.length})`);
  }

  return { created, updated };
}

export type { ArticleRecord };
