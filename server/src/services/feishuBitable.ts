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
  // getarticletotal 来源字段（可选，合并后才有）
  targetUser?: number;
  intPageFromSessionReadUser?: number;
  intPageFromSessionReadCount?: number;
  intPageFromHistMsgReadUser?: number;
  intPageFromHistMsgReadCount?: number;
  intPageFromFeedReadUser?: number;
  intPageFromFeedReadCount?: number;
  intPageFromFriendsReadUser?: number;
  intPageFromFriendsReadCount?: number;
  intPageFromOtherReadUser?: number;
  intPageFromOtherReadCount?: number;
  articleIndex?: number;
}

interface UserGrowthRecord {
  refDate: string;
  userSource: number;
  newUser: number;
  cancelUser: number;
  cumulateUser?: number;
}

const USER_SOURCE_LABELS: Record<number, string> = {
  0: '其他',
  1: '搜索',
  17: '名片分享',
  30: '扫码',
  57: '文章内账号名',
  100: '微信广告',
  161: '他人转载',
  149: '小程序关注',
  200: '视频号',
  201: '直播',
};

function toBitableFields(article: ArticleRecord): Record<string, unknown> {
  const dateMs = new Date(article.refDate).getTime();

  const fields: Record<string, unknown> = {
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

  // 来源渠道字段（来自 getarticletotal）
  if (article.targetUser !== undefined) {
    fields['送达人数'] = article.targetUser;
    fields['会话阅读人数'] = article.intPageFromSessionReadUser ?? 0;
    fields['会话阅读次数'] = article.intPageFromSessionReadCount ?? 0;
    fields['历史消息阅读人数'] = article.intPageFromHistMsgReadUser ?? 0;
    fields['历史消息阅读次数'] = article.intPageFromHistMsgReadCount ?? 0;
    fields['朋友圈阅读人数'] = article.intPageFromFeedReadUser ?? 0;
    fields['朋友圈阅读次数'] = article.intPageFromFeedReadCount ?? 0;
    fields['好友转发阅读人数'] = article.intPageFromFriendsReadUser ?? 0;
    fields['好友转发阅读次数'] = article.intPageFromFriendsReadCount ?? 0;
    fields['其他来源阅读人数'] = article.intPageFromOtherReadUser ?? 0;
    fields['其他来源阅读次数'] = article.intPageFromOtherReadCount ?? 0;
  }

  if (article.articleIndex !== undefined) {
    fields['文章位置'] = article.articleIndex;
  }

  return fields;
}

function toUserGrowthBitableFields(record: UserGrowthRecord): Record<string, unknown> {
  const dateMs = new Date(record.refDate).getTime();
  const sourceLabel = USER_SOURCE_LABELS[record.userSource] ?? `未知(${record.userSource})`;

  const fields: Record<string, unknown> = {
    '日期': dateMs,
    '用户渠道': sourceLabel,
    '渠道编号': record.userSource,
    '新关注人数': record.newUser,
    '取消关注人数': record.cancelUser,
    '净增人数': record.newUser - record.cancelUser,
    '更新时间': Date.now(),
  };

  if (record.cumulateUser !== undefined) {
    fields['累计关注人数'] = record.cumulateUser;
  }

  return fields;
}

async function feishuRequest(method: string, path: string, body?: unknown, tableId?: string): Promise<unknown> {
  const token = await getFeishuToken();
  const tid = tableId || env.FEISHU_BITABLE_TABLE_ID;
  const url = `${BITABLE_API}/${env.FEISHU_BITABLE_APP_TOKEN}/tables/${tid}${path}`;

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

// Fetch existing records and return a map of uniqueKey → record_id
async function getExistingRecords(keyField: string, tableId?: string): Promise<Map<string, string>> {
  const recordMap = new Map<string, string>();
  let pageToken: string | undefined;
  let hasMore = true;

  while (hasMore) {
    const params = new URLSearchParams({ page_size: '500' });
    if (pageToken) params.set('page_token', pageToken);

    const result = (await feishuRequest('GET', `/records?${params}`, undefined, tableId)) as {
      items?: { record_id: string; fields: Record<string, unknown> }[];
      has_more: boolean;
      page_token?: string;
    };

    if (result.items) {
      for (const item of result.items) {
        const key = String(item.fields[keyField] || '');
        if (key) {
          recordMap.set(key, item.record_id);
        }
      }
    }

    hasMore = result.has_more;
    pageToken = result.page_token;
  }

  return recordMap;
}

// Generic sync: upsert records to a Bitable table
async function syncRecordsToBitable(
  records: { uniqueKey: string; fields: Record<string, unknown> }[],
  keyField: string,
  tableId?: string,
): Promise<{ created: number; updated: number }> {
  if (records.length === 0) {
    return { created: 0, updated: 0 };
  }

  const existingRecords = await getExistingRecords(keyField, tableId);

  const createList: BitableRecord[] = [];
  const updateList: { record_id: string; fields: Record<string, unknown> }[] = [];

  for (const record of records) {
    const existingRecordId = existingRecords.get(record.uniqueKey);

    if (existingRecordId) {
      updateList.push({ record_id: existingRecordId, fields: record.fields });
    } else {
      createList.push({ fields: record.fields });
    }
  }

  // Batch create new records (max 500 per request)
  let created = 0;
  for (let i = 0; i < createList.length; i += 500) {
    const batch = createList.slice(i, i + 500);
    await feishuRequest('POST', '/records/batch_create', { records: batch }, tableId);
    created += batch.length;
    console.log(`[Feishu] Batch created ${batch.length} records (${created}/${createList.length})`);
  }

  // Batch update existing records (max 500 per request)
  let updated = 0;
  for (let i = 0; i < updateList.length; i += 500) {
    const batch = updateList.slice(i, i + 500);
    await feishuRequest('POST', '/records/batch_update', { records: batch }, tableId);
    updated += batch.length;
    console.log(`[Feishu] Batch updated ${batch.length} records (${updated}/${updateList.length})`);
  }

  return { created, updated };
}

export async function syncArticlesToBitable(articles: ArticleRecord[]): Promise<{ created: number; updated: number }> {
  const records = articles.map((article) => ({
    uniqueKey: article.msgid,
    fields: toBitableFields(article),
  }));

  return syncRecordsToBitable(records, '消息ID', env.FEISHU_BITABLE_TABLE_ID);
}

export async function syncUserGrowthToBitable(userRecords: UserGrowthRecord[]): Promise<{ created: number; updated: number }> {
  const tableId = env.FEISHU_BITABLE_TABLE_ID_USERS;
  if (!tableId) {
    throw new Error('FEISHU_BITABLE_TABLE_ID_USERS not configured');
  }

  // Unique key: "日期_渠道编号"
  const records = userRecords.map((record) => {
    const uniqueKey = `${record.refDate}_${record.userSource}`;
    const fields = toUserGrowthBitableFields(record);
    fields['唯一键'] = uniqueKey;
    return { uniqueKey, fields };
  });

  return syncRecordsToBitable(records, '唯一键', tableId);
}

export type { ArticleRecord, UserGrowthRecord };
