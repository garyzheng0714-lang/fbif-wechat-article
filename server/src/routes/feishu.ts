import { Router } from 'express';
import { z } from 'zod';
import { callWechatApi, fetchPublishedArticles } from '../services/wechatApi.js';
import {
  syncArticlesToBitable,
  syncUserGrowthToBitable,
  syncUserReadToBitable,
  syncUserShareToBitable,
} from '../services/feishuBitable.js';
import type { ArticleRecord, UserGrowthRecord, UserReadRecord, UserShareRecord } from '../services/feishuBitable.js';
import type {
  ArticleSummaryItem,
  ArticleTotalItem,
  UserSummaryItem,
  UserCumulateItem,
  UserReadItem,
  UserShareItem,
} from '../types/wechat.js';

export const feishuRouter = Router();

const dateRangeSchema = z.object({
  begin_date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/),
  end_date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/),
});

function getYesterday(): string {
  return new Date(Date.now() - 86400000).toISOString().slice(0, 10);
}

function clampEndDate(endDate: string): string {
  const yesterday = getYesterday();
  return endDate > yesterday ? yesterday : endDate;
}

function extractArticleIndex(msgid: string): number | undefined {
  const parts = msgid.split('_');
  if (parts.length >= 2) {
    const idx = parseInt(parts[parts.length - 1], 10);
    if (!isNaN(idx)) return idx;
  }
  return undefined;
}

// ==================== 文章同步 ====================
feishuRouter.post('/sync', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu Sync] Fetching WeChat data for ${beginDate} ~ ${endDate}`);

    // Fetch article summary, article total, and published articles metadata in parallel
    const [summaryData, totalData, publishedMap] = await Promise.all([
      callWechatApi('getarticlesummary', beginDate, endDate),
      callWechatApi('getarticletotal', beginDate, endDate),
      fetchPublishedArticles(),
    ]);

    const summaryItems = (summaryData.list || []) as ArticleSummaryItem[];
    const totalItems = (totalData.list || []) as ArticleTotalItem[];

    if (summaryItems.length === 0) {
      res.json({ success: true, message: 'No articles found in date range', total: 0, created: 0, updated: 0 });
      return;
    }

    // Build msgid → getarticletotal map
    const totalMap = new Map<string, ArticleTotalItem>();
    for (const item of totalItems) {
      totalMap.set(item.msgid, item);
    }

    let matchedUrls = 0;

    // Merge all data sources
    const articles: ArticleRecord[] = summaryItems.map((item) => {
      const record: ArticleRecord = {
        title: item.title,
        refDate: item.ref_date,
        msgid: item.msgid,
        intPageReadUser: item.int_page_read_user,
        intPageReadCount: item.int_page_read_count,
        oriPageReadUser: item.ori_page_read_user,
        oriPageReadCount: item.ori_page_read_count,
        shareUser: item.share_user,
        shareCount: item.share_count,
        addToFavUser: item.add_to_fav_user,
        addToFavCount: item.add_to_fav_count,
      };

      // Merge getarticletotal data
      const totalItem = totalMap.get(item.msgid);
      if (totalItem && totalItem.details && totalItem.details.length > 0) {
        const detail = totalItem.details[totalItem.details.length - 1];
        record.targetUser = detail.target_user;
        record.intPageFromSessionReadUser = detail.int_page_from_session_read_user;
        record.intPageFromSessionReadCount = detail.int_page_from_session_read_count;
        record.intPageFromHistMsgReadUser = detail.int_page_from_hist_msg_read_user;
        record.intPageFromHistMsgReadCount = detail.int_page_from_hist_msg_read_count;
        record.intPageFromFeedReadUser = detail.int_page_from_feed_read_user;
        record.intPageFromFeedReadCount = detail.int_page_from_feed_read_count;
        record.intPageFromFriendsReadUser = detail.int_page_from_friends_read_user;
        record.intPageFromFriendsReadCount = detail.int_page_from_friends_read_count;
        record.intPageFromOtherReadUser = detail.int_page_from_other_read_user;
        record.intPageFromOtherReadCount = detail.int_page_from_other_read_count;
      }

      // Merge freepublish metadata (URL, author, digest, etc.)
      const published = publishedMap.get(item.title);
      if (published) {
        record.articleUrl = published.url;
        record.author = published.author;
        record.digest = published.digest;
        record.thumbUrl = published.thumb_url;
        record.contentSourceUrl = published.content_source_url;
        matchedUrls++;
      }

      record.articleIndex = extractArticleIndex(item.msgid);

      return record;
    });

    console.log(`[Feishu Sync] Found ${articles.length} articles, ${matchedUrls} with URLs, syncing...`);

    const result = await syncArticlesToBitable(articles);

    console.log(`[Feishu Sync] Done. Created: ${result.created}, Updated: ${result.updated}`);

    res.json({
      success: true,
      message: `Synced ${result.created} new, updated ${result.updated} existing articles (${matchedUrls} with URLs)`,
      total: articles.length,
      created: result.created,
      updated: result.updated,
      matchedUrls,
    });
  } catch (err) {
    next(err);
  }
});

// ==================== 粉丝增长同步 ====================
feishuRouter.post('/sync-users', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu User Sync] Fetching data for ${beginDate} ~ ${endDate}`);

    const [summaryData, cumulateData] = await Promise.all([
      callWechatApi('getusersummary', beginDate, endDate),
      callWechatApi('getusercumulate', beginDate, endDate),
    ]);

    const summaryItems = (summaryData.list || []) as UserSummaryItem[];
    const cumulateItems = (cumulateData.list || []) as UserCumulateItem[];

    const cumulateMap = new Map<string, number>();
    for (const item of cumulateItems) {
      cumulateMap.set(item.ref_date, item.cumulate_user);
    }

    const userRecords: UserGrowthRecord[] = summaryItems.map((item) => ({
      refDate: item.ref_date,
      userSource: item.user_source,
      newUser: item.new_user,
      cancelUser: item.cancel_user,
      cumulateUser: cumulateMap.get(item.ref_date),
    }));

    if (userRecords.length === 0) {
      res.json({ success: true, message: 'No user data found', total: 0, created: 0, updated: 0 });
      return;
    }

    console.log(`[Feishu User Sync] Found ${userRecords.length} records, syncing...`);
    const result = await syncUserGrowthToBitable(userRecords);
    console.log(`[Feishu User Sync] Done. Created: ${result.created}, Updated: ${result.updated}`);

    res.json({
      success: true,
      message: `Synced ${result.created} new, updated ${result.updated} existing user records`,
      total: userRecords.length,
      ...result,
    });
  } catch (err) {
    next(err);
  }
});

// ==================== 每日阅读概况同步 ====================
feishuRouter.post('/sync-reads', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu Read Sync] Fetching data for ${beginDate} ~ ${endDate}`);

    const readData = await callWechatApi('getuserread', beginDate, endDate);
    const readItems = (readData.list || []) as UserReadItem[];

    const readRecords: UserReadRecord[] = readItems.map((item) => ({
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

    if (readRecords.length === 0) {
      res.json({ success: true, message: 'No read data found', total: 0, created: 0, updated: 0 });
      return;
    }

    console.log(`[Feishu Read Sync] Found ${readRecords.length} records, syncing...`);
    const result = await syncUserReadToBitable(readRecords);
    console.log(`[Feishu Read Sync] Done. Created: ${result.created}, Updated: ${result.updated}`);

    res.json({
      success: true,
      message: `Synced ${result.created} new, updated ${result.updated} existing read records`,
      total: readRecords.length,
      ...result,
    });
  } catch (err) {
    next(err);
  }
});

// ==================== 分享场景同步 ====================
feishuRouter.post('/sync-shares', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu Share Sync] Fetching data for ${beginDate} ~ ${endDate}`);

    const shareData = await callWechatApi('getusershare', beginDate, endDate);
    const shareItems = (shareData.list || []) as UserShareItem[];

    const shareRecords: UserShareRecord[] = shareItems.map((item) => ({
      refDate: item.ref_date,
      shareScene: item.share_scene,
      shareUser: item.share_user,
      shareCount: item.share_count,
    }));

    if (shareRecords.length === 0) {
      res.json({ success: true, message: 'No share data found', total: 0, created: 0, updated: 0 });
      return;
    }

    console.log(`[Feishu Share Sync] Found ${shareRecords.length} records, syncing...`);
    const result = await syncUserShareToBitable(shareRecords);
    console.log(`[Feishu Share Sync] Done. Created: ${result.created}, Updated: ${result.updated}`);

    res.json({
      success: true,
      message: `Synced ${result.created} new, updated ${result.updated} existing share records`,
      total: shareRecords.length,
      ...result,
    });
  } catch (err) {
    next(err);
  }
});

// ==================== 一键全部同步 ====================
feishuRouter.post('/sync-all', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu Sync All] Starting full sync for ${beginDate} ~ ${endDate}`);

    const results: Record<string, { total: number; created: number; updated: number } | { error: string }> = {};

    // Run all syncs, catch errors individually so one failure doesn't block others
    const syncTasks = [
      {
        name: 'articles',
        fn: async () => {
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
              record.articleUrl = published.url; record.author = published.author;
              record.digest = published.digest; record.thumbUrl = published.thumb_url;
              record.contentSourceUrl = published.content_source_url;
            }
            record.articleIndex = extractArticleIndex(item.msgid);
            return record;
          });
          const r = await syncArticlesToBitable(articles);
          return { total: articles.length, ...r };
        },
      },
      {
        name: 'users',
        fn: async () => {
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
        },
      },
      {
        name: 'reads',
        fn: async () => {
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
        },
      },
      {
        name: 'shares',
        fn: async () => {
          const shareData = await callWechatApi('getusershare', beginDate, endDate);
          const shareItems = (shareData.list || []) as UserShareItem[];
          const records: UserShareRecord[] = shareItems.map((item) => ({
            refDate: item.ref_date, shareScene: item.share_scene,
            shareUser: item.share_user, shareCount: item.share_count,
          }));
          const r = await syncUserShareToBitable(records);
          return { total: records.length, ...r };
        },
      },
    ];

    for (const task of syncTasks) {
      try {
        results[task.name] = await task.fn();
      } catch (err) {
        results[task.name] = { error: err instanceof Error ? err.message : String(err) };
        console.error(`[Feishu Sync All] ${task.name} failed:`, err);
      }
    }

    console.log(`[Feishu Sync All] Done.`, JSON.stringify(results));

    res.json({ success: true, results });
  } catch (err) {
    next(err);
  }
});
