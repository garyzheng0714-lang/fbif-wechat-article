import cron from 'node-cron';
import { callWechatApi } from './wechatApi.js';
import {
  syncArticleMasterToBitable,
  syncDailyArticleDataToBitable,
  syncUserGrowthToBitable,
  syncUserReadToBitable,
  syncUserShareToBitable,
} from './feishuBitable.js';
import type {
  ArticleMasterRecord,
  DailyArticleDataRecord,
  UserGrowthRecord,
  UserReadRecord,
  UserShareRecord,
} from './feishuBitable.js';
import type {
  ArticleSummaryItem,
  ArticleTotalItem,
  UserSummaryItem,
  UserCumulateItem,
  UserReadItem,
  UserShareItem,
} from '../types/wechat.js';
import { getToken } from './wechatToken.js';
import { env } from '../config/env.js';
import { readCursor, writeCursor } from './syncCursor.js';
import { QuotaLimitError } from './wechatApi.js';

function formatDate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

function getYesterday(): string {
  return formatDate(new Date(Date.now() - 86400000));
}

function addDays(dateStr: string, days: number): string {
  const d = new Date(dateStr);
  d.setDate(d.getDate() + days);
  return formatDate(d);
}

function extractArticleIndex(msgid: string): number | undefined {
  const parts = msgid.split('_');
  if (parts.length >= 2) {
    const idx = parseInt(parts[parts.length - 1], 10);
    if (!isNaN(idx)) return idx;
  }
  return undefined;
}

// ==================== Sync Functions ====================

async function syncArticles(
  beginDate: string,
  endDate: string,
): Promise<{ master: { created: number; skipped: number }; daily: { created: number; skipped: number } }> {
  const [summaryData, totalData] = await Promise.all([
    callWechatApi('getarticlesummary', beginDate, endDate),
    callWechatApi('getarticletotal', beginDate, endDate),
  ]);

  const summaryItems = (summaryData.list || []) as ArticleSummaryItem[];
  const totalItems = (totalData.list || []) as ArticleTotalItem[];
  const totalMap = new Map<string, ArticleTotalItem>();
  for (const item of totalItems) totalMap.set(item.msgid, item);

  const masterRecords: ArticleMasterRecord[] = [];
  const dailyRecords: DailyArticleDataRecord[] = [];

  for (const item of summaryItems) {
    const totalItem = totalMap.get(item.msgid);

    const master: ArticleMasterRecord = {
      title: item.title,
      refDate: item.ref_date,
      msgid: item.msgid,
      articleIndex: extractArticleIndex(item.msgid),
      articleUrl: totalItem?.url,
    };

    masterRecords.push(master);

    const daily: DailyArticleDataRecord = {
      msgid: item.msgid,
      refDate: item.ref_date,
      intPageReadUser: item.int_page_read_user,
      intPageReadCount: item.int_page_read_count,
      oriPageReadUser: item.ori_page_read_user,
      oriPageReadCount: item.ori_page_read_count,
      shareUser: item.share_user,
      shareCount: item.share_count,
      addToFavUser: item.add_to_fav_user,
      addToFavCount: item.add_to_fav_count,
    };

    if (totalItem?.details?.length) {
      const d = totalItem.details[totalItem.details.length - 1];
      daily.targetUser = d.target_user;
      daily.intPageFromSessionReadUser = d.int_page_from_session_read_user;
      daily.intPageFromSessionReadCount = d.int_page_from_session_read_count;
      daily.intPageFromHistMsgReadUser = d.int_page_from_hist_msg_read_user;
      daily.intPageFromHistMsgReadCount = d.int_page_from_hist_msg_read_count;
      daily.intPageFromFeedReadUser = d.int_page_from_feed_read_user;
      daily.intPageFromFeedReadCount = d.int_page_from_feed_read_count;
      daily.intPageFromFriendsReadUser = d.int_page_from_friends_read_user;
      daily.intPageFromFriendsReadCount = d.int_page_from_friends_read_count;
      daily.intPageFromOtherReadUser = d.int_page_from_other_read_user;
      daily.intPageFromOtherReadCount = d.int_page_from_other_read_count;
      daily.feedShareFromSessionUser = d.feed_share_from_session_user;
      daily.feedShareFromSessionCnt = d.feed_share_from_session_cnt;
      daily.feedShareFromFeedUser = d.feed_share_from_feed_user;
      daily.feedShareFromFeedCnt = d.feed_share_from_feed_cnt;
      daily.feedShareFromOtherUser = d.feed_share_from_other_user;
      daily.feedShareFromOtherCnt = d.feed_share_from_other_cnt;
    }

    dailyRecords.push(daily);
  }

  const master = await syncArticleMasterToBitable(masterRecords);
  const daily = await syncDailyArticleDataToBitable(dailyRecords);

  return { master, daily };
}

async function syncUsers(
  beginDate: string,
  endDate: string,
): Promise<{ total: number; created: number; updated: number }> {
  const [summaryData, cumulateData] = await Promise.all([
    callWechatApi('getusersummary', beginDate, endDate),
    callWechatApi('getusercumulate', beginDate, endDate),
  ]);
  const summaryItems = (summaryData.list || []) as UserSummaryItem[];
  const cumulateItems = (cumulateData.list || []) as UserCumulateItem[];
  const cumulateMap = new Map<string, number>();
  for (const item of cumulateItems) {
    cumulateMap.set(`${item.ref_date}_${item.user_source}`, item.cumulate_user);
  }

  const records: UserGrowthRecord[] = summaryItems.map((item) => ({
    refDate: item.ref_date,
    userSource: item.user_source,
    newUser: item.new_user,
    cancelUser: item.cancel_user,
    cumulateUser: cumulateMap.get(`${item.ref_date}_${item.user_source}`),
  }));
  const r = await syncUserGrowthToBitable(records);
  return { total: records.length, ...r };
}

async function syncReads(
  beginDate: string,
  endDate: string,
): Promise<{ total: number; created: number; updated: number }> {
  const readData = await callWechatApi('getuserread', beginDate, endDate);
  const readItems = (readData.list || []) as UserReadItem[];
  const records: UserReadRecord[] = readItems.map((item) => ({
    refDate: item.ref_date,
    userSource: item.user_source,
    intPageReadUser: item.int_page_read_user,
    intPageReadCount: item.int_page_read_count,
    oriPageReadUser: item.ori_page_read_user,
    oriPageReadCount: item.ori_page_read_count,
    shareUser: item.share_user,
    shareCount: item.share_count,
    addToFavUser: item.add_to_fav_user,
    addToFavCount: item.add_to_fav_count,
  }));
  const r = await syncUserReadToBitable(records);
  return { total: records.length, ...r };
}

async function syncShares(
  beginDate: string,
  endDate: string,
): Promise<{ total: number; created: number; updated: number }> {
  const shareData = await callWechatApi('getusershare', beginDate, endDate);
  const shareItems = (shareData.list || []) as UserShareItem[];
  const records: UserShareRecord[] = shareItems.map((item) => ({
    refDate: item.ref_date,
    shareScene: item.share_scene,
    shareUser: item.share_user,
    shareCount: item.share_count,
  }));
  const r = await syncUserShareToBitable(records);
  return { total: records.length, ...r };
}

type SyncResult = Record<string, unknown>;

async function runFullSync(beginDate: string, endDate: string): Promise<SyncResult> {
  const results: SyncResult = {};
  const tasks = [
    { name: 'articles', fn: () => syncArticles(beginDate, endDate) },
    { name: 'users', fn: () => syncUsers(beginDate, endDate) },
    { name: 'reads', fn: () => syncReads(beginDate, endDate) },
    { name: 'shares', fn: () => syncShares(beginDate, endDate) },
  ];

  for (const task of tasks) {
    try {
      results[task.name] = await task.fn();
    } catch (err) {
      if (err instanceof QuotaLimitError) throw err;
      results[task.name] = { error: err instanceof Error ? err.message : String(err) };
      console.error(`[Scheduler] ${task.name} sync failed:`, err);
    }
  }

  return results;
}

function isAllEmpty(results: SyncResult): boolean {
  for (const value of Object.values(results)) {
    if (value && typeof value === 'object' && 'error' in (value as Record<string, unknown>)) continue;
    const v = value as Record<string, unknown>;
    if (v.master) {
      const m = v.master as { created: number };
      const d = v.daily as { created: number };
      if (m.created > 0 || d.created > 0) return false;
    } else if ('total' in v) {
      if ((v.total as number) > 0) return false;
    }
  }
  return true;
}

// ==================== Public Sync Functions ====================

/**
 * Daily sync: only fetch data from cursor.newestSyncedDate+1 to yesterday.
 * If no cursor exists, only syncs yesterday.
 */
export async function runDailySync(): Promise<void> {
  if (!env.WECHAT_APPID || !env.WECHAT_SECRET) {
    console.log('[Scheduler] Skipping sync: WeChat credentials not configured');
    return;
  }

  await getToken();

  const cursor = readCursor();
  const yesterday = getYesterday();

  if (cursor && cursor.newestSyncedDate >= yesterday) {
    console.log(`[Scheduler] Already synced up to ${cursor.newestSyncedDate}, skipping daily sync`);
    return;
  }

  const beginDate = cursor ? addDays(cursor.newestSyncedDate, 1) : yesterday;

  console.log(`[Scheduler] Daily sync: ${beginDate} ~ ${yesterday}`);
  const results = await runFullSync(beginDate, yesterday);
  console.log('[Scheduler] Daily sync complete:', JSON.stringify(results));

  writeCursor({
    oldestSyncedDate: cursor?.oldestSyncedDate || beginDate,
    newestSyncedDate: yesterday,
    backfillComplete: cursor?.backfillComplete || false,
  });
}

/**
 * Backfill sync: continue from cursor.oldestSyncedDate backwards.
 * Each chunk is 7 days. Stops when API returns empty data.
 */
export async function runBackfillSync(): Promise<void> {
  if (!env.WECHAT_APPID || !env.WECHAT_SECRET) {
    console.log('[Scheduler] Skipping backfill: WeChat credentials not configured');
    return;
  }

  await getToken();

  const cursor = readCursor();

  if (cursor?.backfillComplete) {
    console.log('[Scheduler] Backfill already complete, skipping');
    return;
  }

  const startFrom = cursor?.oldestSyncedDate || getYesterday();
  const newestSyncedDate = cursor?.newestSyncedDate || getYesterday();
  const chunkSize = 7;

  console.log(`[Scheduler] Starting backfill from ${startFrom} backwards`);

  let currentEnd = addDays(startFrom, -1);

  for (let i = 0; i < 200; i++) {
    const chunkBegin = addDays(currentEnd, -(chunkSize - 1));

    console.log(`[Scheduler] Backfill chunk: ${chunkBegin} ~ ${currentEnd}`);

    let results: SyncResult;
    try {
      results = await runFullSync(chunkBegin, currentEnd);
    } catch (err) {
      if (err instanceof QuotaLimitError) {
        console.log(`[Scheduler] API quota limit reached, pausing backfill. Will resume next run.`);
        return;
      }
      throw err;
    }

    console.log(`[Scheduler] Chunk done:`, JSON.stringify(results));

    const allEmpty = isAllEmpty(results);

    writeCursor({
      oldestSyncedDate: chunkBegin,
      newestSyncedDate: newestSyncedDate,
      backfillComplete: allEmpty,
    });

    if (allEmpty) {
      console.log(`[Scheduler] Backfill complete: no data before ${chunkBegin}`);
      break;
    }

    currentEnd = addDays(chunkBegin, -1);
  }

  console.log('[Scheduler] Backfill finished');
}

// ==================== Cron ====================

let cronTask: ReturnType<typeof cron.schedule> | null = null;

/**
 * Start the daily cron job. Runs every day at 09:00 AM (China time).
 */
export function startScheduler(): void {
  if (cronTask) {
    console.log('[Scheduler] Already running');
    return;
  }

  cronTask = cron.schedule('0 9 * * *', async () => {
    console.log(`[Scheduler] Cron triggered at ${new Date().toISOString()}`);
    try {
      await runDailySync();
      await runBackfillSync();
    } catch (err) {
      console.error('[Scheduler] Cron sync failed:', err);
    }
  }, { timezone: 'Asia/Shanghai' });

  console.log('[Scheduler] Cron job started: daily sync at 09:00 CST');
}

export function stopScheduler(): void {
  if (cronTask) {
    cronTask.stop();
    cronTask = null;
    console.log('[Scheduler] Cron job stopped');
  }
}
