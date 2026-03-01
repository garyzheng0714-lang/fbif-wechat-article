import { env } from '../config/env.js';
import { getFeishuToken } from './feishuToken.js';

const BITABLE_API = 'https://open.feishu.cn/open-apis/bitable/v1/apps';

// ==================== Types ====================

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
  // getarticletotal 来源字段
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
  // freepublish 文章元数据
  articleUrl?: string;
  author?: string;
  digest?: string;
  thumbUrl?: string;
  contentSourceUrl?: string;
}

interface UserGrowthRecord {
  refDate: string;
  userSource: number;
  newUser: number;
  cancelUser: number;
  cumulateUser?: number;
}

interface UserReadRecord {
  refDate: string;
  userSource: number;
  intPageReadUser: number;
  intPageReadCount: number;
  oriPageReadUser: number;
  oriPageReadCount: number;
  shareUser: number;
  shareCount: number;
  addToFavUser: number;
  addToFavCount: number;
}

interface UserShareRecord {
  refDate: string;
  shareScene: number;
  shareUser: number;
  shareCount: number;
}

// ==================== Label Maps ====================

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

const READ_SOURCE_LABELS: Record<number, string> = {
  0: '会话',
  1: '好友',
  2: '朋友圈',
  4: '历史消息',
  5: '其他',
  6: '看一看',
  7: '搜一搜',
  99999999: '全部',
};

const SHARE_SCENE_LABELS: Record<number, string> = {
  1: '好友转发',
  2: '朋友圈',
  255: '其他',
};

// ==================== Field Mappers ====================

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

  if (article.articleIndex !== undefined) fields['文章位置'] = article.articleIndex;
  if (article.articleUrl) fields['文章链接'] = { link: article.articleUrl, text: article.articleUrl };
  if (article.author) fields['作者'] = article.author;
  if (article.digest) fields['摘要'] = article.digest;
  if (article.thumbUrl) fields['封面图'] = { link: article.thumbUrl, text: article.thumbUrl };
  if (article.contentSourceUrl) fields['阅读原文链接'] = { link: article.contentSourceUrl, text: article.contentSourceUrl };

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

function toUserReadBitableFields(record: UserReadRecord): Record<string, unknown> {
  const dateMs = new Date(record.refDate).getTime();
  const sourceLabel = READ_SOURCE_LABELS[record.userSource] ?? `未知(${record.userSource})`;

  return {
    '日期': dateMs,
    '流量来源': sourceLabel,
    '来源编号': record.userSource,
    '图文页阅读人数': record.intPageReadUser,
    '图文页阅读次数': record.intPageReadCount,
    '原文页阅读人数': record.oriPageReadUser,
    '原文页阅读次数': record.oriPageReadCount,
    '分享人数': record.shareUser,
    '分享次数': record.shareCount,
    '收藏人数': record.addToFavUser,
    '收藏次数': record.addToFavCount,
    '更新时间': Date.now(),
  };
}

function toUserShareBitableFields(record: UserShareRecord): Record<string, unknown> {
  const dateMs = new Date(record.refDate).getTime();
  const sceneLabel = SHARE_SCENE_LABELS[record.shareScene] ?? `未知(${record.shareScene})`;

  return {
    '日期': dateMs,
    '分享场景': sceneLabel,
    '场景编号': record.shareScene,
    '分享人数': record.shareUser,
    '分享次数': record.shareCount,
    '更新时间': Date.now(),
  };
}

// ==================== Feishu API Helpers ====================

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

// App-level request (not table-scoped)
async function feishuAppRequest(method: string, path: string, body?: unknown): Promise<unknown> {
  const token = await getFeishuToken();
  const url = `${BITABLE_API}/${env.FEISHU_BITABLE_APP_TOKEN}${path}`;

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

// ==================== Auto Table Creation ====================

// In-memory cache: table name → table_id
const tableIdCache = new Map<string, string>();

async function getOrCreateTable(tableName: string): Promise<string> {
  // Check cache
  const cached = tableIdCache.get(tableName);
  if (cached) return cached;

  // List all existing tables
  const result = (await feishuAppRequest('GET', '/tables')) as {
    items?: { name: string; table_id: string }[];
    has_more: boolean;
  };

  if (result.items) {
    for (const item of result.items) {
      tableIdCache.set(item.name, item.table_id);
    }
  }

  // Found existing table
  const existing = tableIdCache.get(tableName);
  if (existing) {
    console.log(`[Feishu] Found existing table "${tableName}" (${existing})`);
    return existing;
  }

  // Create new table
  console.log(`[Feishu] Creating new table: ${tableName}`);
  const createResult = (await feishuAppRequest('POST', '/tables', {
    table: { name: tableName },
  })) as { table_id: string };

  const tableId = createResult.table_id;
  tableIdCache.set(tableName, tableId);
  console.log(`[Feishu] Created table "${tableName}" (${tableId})`);

  return tableId;
}

// ==================== Field Management ====================

const FIELD_TYPE_TEXT = 1;
const FIELD_TYPE_NUMBER = 2;
const FIELD_TYPE_DATETIME = 5;
const FIELD_TYPE_URL = 15;

interface FieldSpec {
  name: string;
  type: number;
}

async function getExistingFields(tableId: string): Promise<Set<string>> {
  const fieldNames = new Set<string>();
  let pageToken: string | undefined;
  let hasMore = true;

  while (hasMore) {
    const params = new URLSearchParams({ page_size: '100' });
    if (pageToken) params.set('page_token', pageToken);

    const result = (await feishuRequest('GET', `/fields?${params}`, undefined, tableId)) as {
      items?: { field_name: string }[];
      has_more: boolean;
      page_token?: string;
    };

    if (result.items) {
      for (const item of result.items) {
        fieldNames.add(item.field_name);
      }
    }

    hasMore = result.has_more;
    pageToken = result.page_token;
  }

  return fieldNames;
}

async function ensureFieldsExist(requiredFields: FieldSpec[], tableId: string): Promise<void> {
  const existingFields = await getExistingFields(tableId);

  for (const field of requiredFields) {
    if (!existingFields.has(field.name)) {
      console.log(`[Feishu] Creating field: ${field.name} (type: ${field.type})`);
      await feishuRequest('POST', '/fields', { field_name: field.name, type: field.type }, tableId);
    }
  }
}

// ==================== Field Specs ====================

const ARTICLE_FIELDS: FieldSpec[] = [
  { name: '文章标题', type: FIELD_TYPE_TEXT },
  { name: '发布日期', type: FIELD_TYPE_DATETIME },
  { name: '消息ID', type: FIELD_TYPE_TEXT },
  { name: '图文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '图文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '分享人数', type: FIELD_TYPE_NUMBER },
  { name: '分享次数', type: FIELD_TYPE_NUMBER },
  { name: '收藏人数', type: FIELD_TYPE_NUMBER },
  { name: '收藏次数', type: FIELD_TYPE_NUMBER },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
  { name: '送达人数', type: FIELD_TYPE_NUMBER },
  { name: '会话阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '会话阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '历史消息阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '历史消息阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '朋友圈阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '朋友圈阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '好友转发阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '好友转发阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '其他来源阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '其他来源阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '文章位置', type: FIELD_TYPE_NUMBER },
  { name: '文章链接', type: FIELD_TYPE_URL },
  { name: '作者', type: FIELD_TYPE_TEXT },
  { name: '摘要', type: FIELD_TYPE_TEXT },
  { name: '封面图', type: FIELD_TYPE_URL },
  { name: '阅读原文链接', type: FIELD_TYPE_URL },
];

const USER_GROWTH_FIELDS: FieldSpec[] = [
  { name: '日期', type: FIELD_TYPE_DATETIME },
  { name: '用户渠道', type: FIELD_TYPE_TEXT },
  { name: '渠道编号', type: FIELD_TYPE_NUMBER },
  { name: '新关注人数', type: FIELD_TYPE_NUMBER },
  { name: '取消关注人数', type: FIELD_TYPE_NUMBER },
  { name: '净增人数', type: FIELD_TYPE_NUMBER },
  { name: '累计关注人数', type: FIELD_TYPE_NUMBER },
  { name: '唯一键', type: FIELD_TYPE_TEXT },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
];

const USER_READ_FIELDS: FieldSpec[] = [
  { name: '日期', type: FIELD_TYPE_DATETIME },
  { name: '流量来源', type: FIELD_TYPE_TEXT },
  { name: '来源编号', type: FIELD_TYPE_NUMBER },
  { name: '图文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '图文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '分享人数', type: FIELD_TYPE_NUMBER },
  { name: '分享次数', type: FIELD_TYPE_NUMBER },
  { name: '收藏人数', type: FIELD_TYPE_NUMBER },
  { name: '收藏次数', type: FIELD_TYPE_NUMBER },
  { name: '唯一键', type: FIELD_TYPE_TEXT },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
];

const USER_SHARE_FIELDS: FieldSpec[] = [
  { name: '日期', type: FIELD_TYPE_DATETIME },
  { name: '分享场景', type: FIELD_TYPE_TEXT },
  { name: '场景编号', type: FIELD_TYPE_NUMBER },
  { name: '分享人数', type: FIELD_TYPE_NUMBER },
  { name: '分享次数', type: FIELD_TYPE_NUMBER },
  { name: '唯一键', type: FIELD_TYPE_TEXT },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
];

// ==================== Generic Sync ====================

async function getExistingRecords(keyField: string, tableId: string): Promise<Map<string, string>> {
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

async function syncRecordsToBitable(
  records: { uniqueKey: string; fields: Record<string, unknown> }[],
  keyField: string,
  tableId: string,
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

  let created = 0;
  for (let i = 0; i < createList.length; i += 500) {
    const batch = createList.slice(i, i + 500);
    await feishuRequest('POST', '/records/batch_create', { records: batch }, tableId);
    created += batch.length;
    console.log(`[Feishu] Batch created ${batch.length} records (${created}/${createList.length})`);
  }

  let updated = 0;
  for (let i = 0; i < updateList.length; i += 500) {
    const batch = updateList.slice(i, i + 500);
    await feishuRequest('POST', '/records/batch_update', { records: batch }, tableId);
    updated += batch.length;
    console.log(`[Feishu] Batch updated ${batch.length} records (${updated}/${updateList.length})`);
  }

  return { created, updated };
}

// ==================== Public Sync Functions ====================

export async function syncArticlesToBitable(articles: ArticleRecord[]): Promise<{ created: number; updated: number }> {
  const tableId = env.FEISHU_BITABLE_TABLE_ID;
  await ensureFieldsExist(ARTICLE_FIELDS, tableId);

  const records = articles.map((article) => ({
    uniqueKey: article.msgid,
    fields: toBitableFields(article),
  }));

  return syncRecordsToBitable(records, '消息ID', tableId);
}

export async function syncUserGrowthToBitable(userRecords: UserGrowthRecord[]): Promise<{ created: number; updated: number }> {
  const tableId = await getOrCreateTable('粉丝增长');
  await ensureFieldsExist(USER_GROWTH_FIELDS, tableId);

  const records = userRecords.map((record) => {
    const uniqueKey = `${record.refDate}_${record.userSource}`;
    const fields = toUserGrowthBitableFields(record);
    fields['唯一键'] = uniqueKey;
    return { uniqueKey, fields };
  });

  return syncRecordsToBitable(records, '唯一键', tableId);
}

export async function syncUserReadToBitable(readRecords: UserReadRecord[]): Promise<{ created: number; updated: number }> {
  const tableId = await getOrCreateTable('每日阅读概况');
  await ensureFieldsExist(USER_READ_FIELDS, tableId);

  const records = readRecords.map((record) => {
    const uniqueKey = `${record.refDate}_${record.userSource}`;
    const fields = toUserReadBitableFields(record);
    fields['唯一键'] = uniqueKey;
    return { uniqueKey, fields };
  });

  return syncRecordsToBitable(records, '唯一键', tableId);
}

export async function syncUserShareToBitable(shareRecords: UserShareRecord[]): Promise<{ created: number; updated: number }> {
  const tableId = await getOrCreateTable('分享场景');
  await ensureFieldsExist(USER_SHARE_FIELDS, tableId);

  const records = shareRecords.map((record) => {
    const uniqueKey = `${record.refDate}_${record.shareScene}`;
    const fields = toUserShareBitableFields(record);
    fields['唯一键'] = uniqueKey;
    return { uniqueKey, fields };
  });

  return syncRecordsToBitable(records, '唯一键', tableId);
}

export type { ArticleRecord, UserGrowthRecord, UserReadRecord, UserShareRecord };
