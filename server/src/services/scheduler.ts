import cron from 'node-cron';
import { callWechatApi, fetchPublishedArticles } from './wechatApi.js';
import {
  syncArticlesToBitable,
  syncUserGrowthToBitable,
  syncUserReadToBitable,
  syncUserShareToBitable,
} from './feishuBitable.js';
import type { ArticleRecord, UserGrowthRecord, UserReadRecord, UserShareRecord } from './feishuBitable.js';
import type {
  ArticleSummaryItem,
  ArticleTotalItem,
  UserSummaryItem,
  UserCumulateItem,
  UserReadItem,
  UserShareItem,
} from '../types/wechat.js';
import { getTokenStatus } from './wechatToken.js';

function formatDate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

function getYesterday(): string {
  return formatDate(new Date(Date.now() - 86400000));
}

function daysAgo(n: number): string {
  return formatDate(new Date(Date.now() - n * 86400000));
}

function extractArticleIndex(msgid: string): number | undefined {
  const parts = msgid.split('_');
  if (parts.length >= 2) {
    const idx = parseInt(parts[parts.length - 1], 10);
    if (!isNaN(idx)) return idx;
  }
  return undefined;
}

async function syncArticles(beginDate: string, endDate: string): Promise<{ total: number; created: number; updated: number }> {
  const [summaryData, totalData, publishedMap] = await Promise.all([
    callWechatApi('getarticlesummary', beginDate, endDate),
    callWechatApi('getarticletotal', beginDate, endDate),
    fetchPublishedArticles(),
  ]);

  const summaryItems = (summaryData.list || []) as ArticleSummaryItem[];
  const totalItems = (totalData.list || []) as ArticleTotalItem[];
  const totalMap = new Map<string, ArticleTotalItem>();
  for (const item of totalItems) totalMap.set(item.msgid, item);

  const articles: ArticleRecord[] = summaryItems.map((item) => {
    const record: ArticleRecord = {
      title: item.title, refDate: item.ref_date, msgid: item.msgid,
      intPageReadUser: item.int_page_read_user, intPageReadCount: item.int_page_read_count,
      oriPageReadUser: item.ori_page_read_user, oriPageReadCount: item.ori_page_read_count,
      shareUser: item.share_user, shareCount: item.share_count,
      addToFavUser: item.add_to_fav_user, addToFavCount: item.add_to_fav_count,
    };
    const totalItem = totalMap.get(item.msgid);
    if (totalItem?.details?.length) {
      const d = totalItem.details[totalItem.details.length - 1];
      record.targetUser = d.target_user;
      record.intPageFromSessionReadUser = d.int_page_from_session_read_user;
      record.intPageFromSessionReadCount = d.int_page_from_session_read_count;
      record.intPageFromHistMsgReadUser = d.int_page_from_hist_msg_read_user;
      record.intPageFromHistMsgReadCount = d.int_page_from_hist_msg_read_count;
      record.intPageFromFeedReadUser = d.int_page_from_feed_read_user;
      record.intPageFromFeedReadCount = d.int_page_from_feed_read_count;
      record.intPageFromFriendsReadUser = d.int_page_from_friends_read_user;
      record.intPageFromFriendsReadCount = d.int_page_from_friends_read_count;
      record.intPageFromOtherReadUser = d.int_page_from_other_read_user;
      record.intPageFromOtherReadCount = d.int_page_from_other_read_count;
    }
    const published = publishedMap.get(item.title);
    if (published) {
      record.articleUrl = published.url;
      record.author = published.author;
      record.digest = published.digest;
      record.thumbUrl = published.thumb_url;
      record.contentSourceUrl = published.content_source_url;
    }
    record.articleIndex = extractArticleIndex(item.msgid);
    return record;
  });

  const r = await syncArticlesToBitable(articles);
  return { total: articles.length, ...r };
}

async function syncUsers(beginDate: string, endDate: string): Promise<{ total: number; created: number; updated: number }> {
  const [summaryData, cumulateData] = await Promise.all([
    callWechatApi('getusersummary', beginDate, endDate),
    callWechatApi('getusercumulate', beginDate, endDate),
  ]);
  const summaryItems = (summaryData.list || []) as UserSummaryItem[];
  const cumulateItems = (cumulateData.list || []) as UserCumulateItem[];
  const cumulateMap = new Map<string, number>();
  for (const item of cumulateItems) cumulateMap.set(item.ref_date, item.cumulate_user);

  const records: UserGrowthRecord[] = summaryItems.map((item) => ({
    refDate: item.ref_date, userSource: item.user_source,
    newUser: item.new_user, cancelUser: item.cancel_user,
    cumulateUser: cumulateMap.get(item.ref_date),
  }));
  const r = await syncUserGrowthToBitable(records);
  return { total: records.length, ...r };
}

async function syncReads(beginDate: string, endDate: string): Promise<{ total: number; created: number; updated: number }> {
  const readData = await callWechatApi('getuserread', beginDate, endDate);
  const readItems = (readData.list || []) as UserReadItem[];
  const records: UserReadRecord[] = readItems.map((item) => ({
    refDate: item.ref_date, userSource: item.user_source,
    intPageReadUser: item.int_page_read_user, intPageReadCount: item.int_page_read_count,
    oriPageReadUser: item.ori_page_read_user, oriPageReadCount: item.ori_page_read_count,
    shareUser: item.share_user, shareCount: item.share_count,
    addToFavUser: item.add_to_fav_user, addToFavCount: item.add_to_fav_count,
  }));
  const r = await syncUserReadToBitable(records);
  return { total: records.length, ...r };
}

async function syncShares(beginDate: string, endDate: string): Promise<{ total: number; created: number; updated: number }> {
  const shareData = await callWechatApi('getusershare', beginDate, endDate);
  const shareItems = (shareData.list || []) as UserShareItem[];
  const records: UserShareRecord[] = shareItems.map((item) => ({
    refDate: item.ref_date, shareScene: item.share_scene,
    shareUser: item.share_user, shareCount: item.share_count,
  }));
  const r = await syncUserShareToBitable(records);
  return { total: records.length, ...r };
}

type SyncResult = Record<string, { total: number; created: number; updated: number } | { error: string }>;

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
      results[task.name] = { error: err instanceof Error ? err.message : String(err) };
      console.error(`[Scheduler] ${task.name} sync failed:`, err);
    }
  }

  return results;
}

/**
 * Run daily sync for the past N days. Called on startup and by cron.
 *
 * Args:
 *     days: Number of days to look back from yesterday
 */
export async function runDailySync(days: number = 7): Promise<void> {
  if (getTokenStatus() === 'uninitialized') {
    console.log('[Scheduler] Skipping sync: WeChat credentials not configured');
    return;
  }

  const endDate = getYesterday();
  const beginDate = daysAgo(days);

  console.log(`[Scheduler] Starting daily sync: ${beginDate} ~ ${endDate}`);
  const results = await runFullSync(beginDate, endDate);
  console.log('[Scheduler] Daily sync complete:', JSON.stringify(results));
}

/**
 * Run a backfill sync for a larger date range (e.g., 30/60/90 days).
 * Processes in weekly chunks to avoid API rate limits.
 *
 * Args:
 *     totalDays: Total number of days to backfill from yesterday
 */
export async function runBackfillSync(totalDays: number = 60): Promise<void> {
  if (getTokenStatus() === 'uninitialized') {
    console.log('[Scheduler] Skipping backfill: WeChat credentials not configured');
    return;
  }

  const yesterday = getYesterday();
  const chunkSize = 7;

  console.log(`[Scheduler] Starting backfill sync: ${totalDays} days back from ${yesterday}`);

  for (let offset = 0; offset < totalDays; offset += chunkSize) {
    const chunkEnd = daysAgo(offset + 1);
    const chunkBegin = daysAgo(Math.min(offset + chunkSize, totalDays));

    if (chunkBegin > chunkEnd) continue;

    console.log(`[Scheduler] Backfill chunk: ${chunkBegin} ~ ${chunkEnd}`);
    const results = await runFullSync(chunkBegin, chunkEnd);
    console.log(`[Scheduler] Chunk done:`, JSON.stringify(results));
  }

  console.log(`[Scheduler] Backfill complete (${totalDays} days)`);
}

let cronTask: ReturnType<typeof cron.schedule> | null = null;

/**
 * Start the daily cron job. Runs every day at 09:00 AM (China time).
 * WeChat data for yesterday becomes available after 8:00 AM.
 */
export function startScheduler(): void {
  if (cronTask) {
    console.log('[Scheduler] Already running');
    return;
  }

  // Every day at 09:00 Beijing time (UTC+8 = 01:00 UTC)
  cronTask = cron.schedule('0 9 * * *', async () => {
    console.log(`[Scheduler] Cron triggered at ${new Date().toISOString()}`);
    try {
      await runDailySync(3);
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
