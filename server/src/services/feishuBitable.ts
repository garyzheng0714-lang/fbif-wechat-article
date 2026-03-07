import { env } from '../config/env.js';
import { getFeishuToken } from './feishuToken.js';

const BITABLE_API = 'https://open.feishu.cn/open-apis/bitable/v1/apps';

// ==================== Types ====================

interface BitableRecord {
  fields: Record<string, unknown>;
}

export interface ArticleMasterRecord {
  title: string;
  refDate: string;
  msgid: string;
  articleIndex?: number;
  articleUrl?: string;
  author?: string;
  digest?: string;
  thumbUrl?: string;
  contentSourceUrl?: string;
}

export interface DailyArticleDataRecord {
  msgid: string;
  refDate: string;
  intPageReadUser: number;
  intPageReadCount: number;
  oriPageReadUser: number;
  oriPageReadCount: number;
  shareUser: number;
  shareCount: number;
  addToFavUser: number;
  addToFavCount: number;
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
  feedShareFromSessionUser?: number;
  feedShareFromSessionCnt?: number;
  feedShareFromFeedUser?: number;
  feedShareFromFeedCnt?: number;
  feedShareFromOtherUser?: number;
  feedShareFromOtherCnt?: number;
}

export interface UserGrowthRecord {
  refDate: string;
  userSource: number;
  newUser: number;
  cancelUser: number;
  cumulateUser?: number;
}

export interface UserReadRecord {
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

export interface UserShareRecord {
  refDate: string;
  shareScene: number;
  shareUser: number;
  shareCount: number;
}

// ==================== Label Maps ====================

const USER_SOURCE_LABELS: Record<number, string> = {
  0: '其他', 1: '搜索', 17: '名片分享', 30: '扫码',
  57: '文章内账号名', 100: '微信广告', 161: '他人转载',
  149: '小程序关注', 200: '视频号', 201: '直播',
};

const READ_SOURCE_LABELS: Record<number, string> = {
  0: '会话', 1: '好友', 2: '朋友圈', 4: '历史消息',
  5: '其他', 6: '看一看', 7: '搜一搜', 99999999: '全部',
};

const SHARE_SCENE_LABELS: Record<number, string> = {
  1: '好友转发', 2: '朋友圈', 255: '其他',
};

// ==================== Field Mappers ====================

function toArticleMasterFields(record: ArticleMasterRecord): Record<string, unknown> {
  const dateMs = new Date(record.refDate).getTime();
  const publishDate = new Date(record.refDate);
  const publishMonth = `${publishDate.getFullYear()}-${String(publishDate.getMonth() + 1).padStart(2, '0')}`;

  const fields: Record<string, unknown> = {
    '文章标题': record.title,
    '发布日期': dateMs,
    '发布月份': publishMonth,
    '消息ID': record.msgid,
    '更新时间': Date.now(),
  };

  if (record.articleIndex !== undefined) fields['文章位置'] = record.articleIndex;
  if (record.articleUrl) fields['文章链接'] = { link: record.articleUrl, text: record.articleUrl };
  if (record.author) fields['作者'] = record.author;
  if (record.digest) fields['摘要'] = record.digest;
  if (record.thumbUrl) fields['封面图'] = { link: record.thumbUrl, text: record.thumbUrl };
  if (record.contentSourceUrl) fields['阅读原文链接'] = { link: record.contentSourceUrl, text: record.contentSourceUrl };

  return fields;
}

function toDailyArticleDataFields(record: DailyArticleDataRecord): Record<string, unknown> {
  const dateMs = new Date(record.refDate).getTime();
  const uniqueKey = `${record.msgid}_${record.refDate}`;

  const fields: Record<string, unknown> = {
    '唯一键': uniqueKey,
    '消息ID': record.msgid,
    '日期': dateMs,
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

  if (record.targetUser !== undefined) {
    fields['送达人数'] = record.targetUser;
    fields['会话阅读人数'] = record.intPageFromSessionReadUser ?? 0;
    fields['会话阅读次数'] = record.intPageFromSessionReadCount ?? 0;
    fields['历史消息阅读人数'] = record.intPageFromHistMsgReadUser ?? 0;
    fields['历史消息阅读次数'] = record.intPageFromHistMsgReadCount ?? 0;
    fields['朋友圈阅读人数'] = record.intPageFromFeedReadUser ?? 0;
    fields['朋友圈阅读次数'] = record.intPageFromFeedReadCount ?? 0;
    fields['好友转发阅读人数'] = record.intPageFromFriendsReadUser ?? 0;
    fields['好友转发阅读次数'] = record.intPageFromFriendsReadCount ?? 0;
    fields['其他来源阅读人数'] = record.intPageFromOtherReadUser ?? 0;
    fields['其他来源阅读次数'] = record.intPageFromOtherReadCount ?? 0;
    fields['会话转发分享人数'] = record.feedShareFromSessionUser ?? 0;
    fields['会话转发分享次数'] = record.feedShareFromSessionCnt ?? 0;
    fields['朋友圈转发分享人数'] = record.feedShareFromFeedUser ?? 0;
    fields['朋友圈转发分享次数'] = record.feedShareFromFeedCnt ?? 0;
    fields['其他转发分享人数'] = record.feedShareFromOtherUser ?? 0;
    fields['其他转发分享次数'] = record.feedShareFromOtherCnt ?? 0;
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

const tableIdCache = new Map<string, string>();

async function getOrCreateTable(tableName: string): Promise<string> {
  const cached = tableIdCache.get(tableName);
  if (cached) return cached;

  const result = (await feishuAppRequest('GET', '/tables')) as {
    items?: { name: string; table_id: string }[];
    has_more: boolean;
  };

  if (result.items) {
    for (const item of result.items) {
      tableIdCache.set(item.name, item.table_id);
    }
  }

  const existing = tableIdCache.get(tableName);
  if (existing) {
    console.log(`[Feishu] Found existing table "${tableName}" (${existing})`);
    return existing;
  }

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

const ARTICLE_MASTER_FIELDS: FieldSpec[] = [
  { name: '文章标题', type: FIELD_TYPE_TEXT },
  { name: '发布日期', type: FIELD_TYPE_DATETIME },
  { name: '发布月份', type: FIELD_TYPE_TEXT },
  { name: '消息ID', type: FIELD_TYPE_TEXT },
  { name: '文章位置', type: FIELD_TYPE_NUMBER },
  { name: '文章链接', type: FIELD_TYPE_URL },
  { name: '作者', type: FIELD_TYPE_TEXT },
  { name: '摘要', type: FIELD_TYPE_TEXT },
  { name: '封面图', type: FIELD_TYPE_URL },
  { name: '阅读原文链接', type: FIELD_TYPE_URL },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
];

const DAILY_ARTICLE_DATA_FIELDS: FieldSpec[] = [
  { name: '唯一键', type: FIELD_TYPE_TEXT },
  { name: '消息ID', type: FIELD_TYPE_TEXT },
  { name: '日期', type: FIELD_TYPE_DATETIME },
  { name: '图文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '图文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读人数', type: FIELD_TYPE_NUMBER },
  { name: '原文页阅读次数', type: FIELD_TYPE_NUMBER },
  { name: '分享人数', type: FIELD_TYPE_NUMBER },
  { name: '分享次数', type: FIELD_TYPE_NUMBER },
  { name: '收藏人数', type: FIELD_TYPE_NUMBER },
  { name: '收藏次数', type: FIELD_TYPE_NUMBER },
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
  { name: '会话转发分享人数', type: FIELD_TYPE_NUMBER },
  { name: '会话转发分享次数', type: FIELD_TYPE_NUMBER },
  { name: '朋友圈转发分享人数', type: FIELD_TYPE_NUMBER },
  { name: '朋友圈转发分享次数', type: FIELD_TYPE_NUMBER },
  { name: '其他转发分享人数', type: FIELD_TYPE_NUMBER },
  { name: '其他转发分享次数', type: FIELD_TYPE_NUMBER },
  { name: '更新时间', type: FIELD_TYPE_DATETIME },
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

async function syncRecordsInsertOnly(
  records: { uniqueKey: string; fields: Record<string, unknown> }[],
  keyField: string,
  tableId: string,
): Promise<{ created: number; skipped: number }> {
  if (records.length === 0) return { created: 0, skipped: 0 };

  const existingRecords = await getExistingRecords(keyField, tableId);

  const createList: BitableRecord[] = [];
  let skipped = 0;

  for (const record of records) {
    if (existingRecords.has(record.uniqueKey)) {
      skipped++;
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

  return { created, skipped };
}

async function syncRecordsUpsert(
  records: { uniqueKey: string; fields: Record<string, unknown> }[],
  keyField: string,
  tableId: string,
): Promise<{ created: number; updated: number }> {
  if (records.length === 0) return { created: 0, updated: 0 };

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

// ==================== Clear Records ====================

export async function clearTableRecords(tableId: string): Promise<number> {
  let deleted = 0;
  let hasMore = true;

  while (hasMore) {
    const result = (await feishuRequest('GET', '/records?page_size=500', undefined, tableId)) as {
      items?: { record_id: string }[];
      has_more: boolean;
    };

    if (!result.items || result.items.length === 0) break;

    const ids = result.items.map((item) => item.record_id);
    await feishuRequest('POST', '/records/batch_delete', { records: ids }, tableId);
    deleted += ids.length;
    console.log(`[Feishu] Deleted ${ids.length} records (total: ${deleted})`);

    hasMore = result.has_more;
  }

  return deleted;
}

// ==================== Public Sync Functions ====================

export async function syncArticleMasterToBitable(
  articles: ArticleMasterRecord[],
): Promise<{ created: number; skipped: number }> {
  const tableId = env.FEISHU_BITABLE_TABLE_ID;
  await ensureFieldsExist(ARTICLE_MASTER_FIELDS, tableId);

  const records = articles.map((article) => ({
    uniqueKey: article.msgid,
    fields: toArticleMasterFields(article),
  }));

  return syncRecordsInsertOnly(records, '消息ID', tableId);
}

export async function syncDailyArticleDataToBitable(
  data: DailyArticleDataRecord[],
): Promise<{ created: number; skipped: number }> {
  const tableId = await getOrCreateTable('每日文章数据');
  await ensureFieldsExist(DAILY_ARTICLE_DATA_FIELDS, tableId);

  const records = data.map((record) => ({
    uniqueKey: `${record.msgid}_${record.refDate}`,
    fields: toDailyArticleDataFields(record),
  }));

  return syncRecordsInsertOnly(records, '唯一键', tableId);
}

export async function syncUserGrowthToBitable(
  userRecords: UserGrowthRecord[],
): Promise<{ created: number; updated: number }> {
  const tableId = await getOrCreateTable('粉丝增长');
  await ensureFieldsExist(USER_GROWTH_FIELDS, tableId);

  const records = userRecords.map((record) => {
    const uniqueKey = `${record.refDate}_${record.userSource}`;
    const fields = toUserGrowthBitableFields(record);
    fields['唯一键'] = uniqueKey;
    return { uniqueKey, fields };
  });

  return syncRecordsUpsert(records, '唯一键', tableId);
}

export async function syncUserReadToBitable(
  readRecords: UserReadRecord[],
): Promise<{ created: number; updated: number }> {
  const tableId = await getOrCreateTable('每日阅读概况');
  await ensureFieldsExist(USER_READ_FIELDS, tableId);

  const records = readRecords.map((record) => {
    const uniqueKey = `${record.refDate}_${record.userSource}`;
    const fields = toUserReadBitableFields(record);
    fields['唯一键'] = uniqueKey;
    return { uniqueKey, fields };
  });

  return syncRecordsUpsert(records, '唯一键', tableId);
}

export async function syncUserShareToBitable(
  shareRecords: UserShareRecord[],
): Promise<{ created: number; updated: number }> {
  const tableId = await getOrCreateTable('分享场景');
  await ensureFieldsExist(USER_SHARE_FIELDS, tableId);

  const records = shareRecords.map((record) => {
    const uniqueKey = `${record.refDate}_${record.shareScene}`;
    const fields = toUserShareBitableFields(record);
    fields['唯一键'] = uniqueKey;
    return { uniqueKey, fields };
  });

  return syncRecordsUpsert(records, '唯一键', tableId);
}

export async function clearAllTables(): Promise<Record<string, number>> {
  const tables = [
    { name: '文章主表', getTableId: async () => env.FEISHU_BITABLE_TABLE_ID },
    { name: '每日文章数据', getTableId: () => getOrCreateTable('每日文章数据') },
    { name: '粉丝增长', getTableId: () => getOrCreateTable('粉丝增长') },
    { name: '每日阅读概况', getTableId: () => getOrCreateTable('每日阅读概况') },
    { name: '分享场景', getTableId: () => getOrCreateTable('分享场景') },
  ];

  const result: Record<string, number> = {};
  for (const table of tables) {
    const tableId = await table.getTableId();
    result[table.name] = await clearTableRecords(tableId);
  }
  return result;
}
