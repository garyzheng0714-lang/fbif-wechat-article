import { Router } from 'express';
import { z } from 'zod';
import { callWechatApi } from '../services/wechatApi.js';
import { syncArticlesToBitable, syncUserGrowthToBitable } from '../services/feishuBitable.js';
import type { ArticleRecord, UserGrowthRecord } from '../services/feishuBitable.js';
import type { ArticleSummaryItem, ArticleTotalItem, UserSummaryItem, UserCumulateItem } from '../types/wechat.js';

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

// Extract article index from msgid (format: "mid_idx" e.g. "123456_1")
function extractArticleIndex(msgid: string): number | undefined {
  const parts = msgid.split('_');
  if (parts.length >= 2) {
    const idx = parseInt(parts[parts.length - 1], 10);
    if (!isNaN(idx)) return idx;
  }
  return undefined;
}

// Sync WeChat article data to Feishu Bitable
feishuRouter.post('/sync', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu Sync] Fetching WeChat data for ${beginDate} ~ ${endDate}`);

    // Fetch both article summary and article total data in parallel
    const [summaryData, totalData] = await Promise.all([
      callWechatApi('getarticlesummary', beginDate, endDate),
      callWechatApi('getarticletotal', beginDate, endDate),
    ]);

    const summaryItems = (summaryData.list || []) as ArticleSummaryItem[];
    const totalItems = (totalData.list || []) as ArticleTotalItem[];

    if (summaryItems.length === 0) {
      res.json({ success: true, message: 'No articles found in date range', total: 0, created: 0, updated: 0 });
      return;
    }

    // Build a map from msgid → getarticletotal data (last day's detail = final cumulative)
    const totalMap = new Map<string, ArticleTotalItem>();
    for (const item of totalItems) {
      totalMap.set(item.msgid, item);
    }

    // Map WeChat data to Bitable records, merging summary + total
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

      // Merge getarticletotal data if available
      const totalItem = totalMap.get(item.msgid);
      if (totalItem && totalItem.details && totalItem.details.length > 0) {
        // Take the last detail entry (latest cumulative data)
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

      // Extract article position index from msgid
      record.articleIndex = extractArticleIndex(item.msgid);

      return record;
    });

    console.log(`[Feishu Sync] Found ${articles.length} articles (with ${totalItems.length} total details), syncing to Bitable...`);

    const result = await syncArticlesToBitable(articles);

    console.log(`[Feishu Sync] Done. Created: ${result.created}, Updated: ${result.updated}`);

    res.json({
      success: true,
      message: `Synced ${result.created} new, updated ${result.updated} existing articles`,
      total: articles.length,
      created: result.created,
      updated: result.updated,
    });
  } catch (err) {
    next(err);
  }
});

// Sync WeChat user growth data to Feishu Bitable
feishuRouter.post('/sync-users', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu User Sync] Fetching WeChat user data for ${beginDate} ~ ${endDate}`);

    // Fetch user summary and cumulate data in parallel
    const [summaryData, cumulateData] = await Promise.all([
      callWechatApi('getusersummary', beginDate, endDate),
      callWechatApi('getusercumulate', beginDate, endDate),
    ]);

    const summaryItems = (summaryData.list || []) as UserSummaryItem[];
    const cumulateItems = (cumulateData.list || []) as UserCumulateItem[];

    // Build a map of date → cumulate_user
    const cumulateMap = new Map<string, number>();
    for (const item of cumulateItems) {
      cumulateMap.set(item.ref_date, item.cumulate_user);
    }

    // Map to UserGrowthRecord, merging cumulate data
    const userRecords: UserGrowthRecord[] = summaryItems.map((item) => ({
      refDate: item.ref_date,
      userSource: item.user_source,
      newUser: item.new_user,
      cancelUser: item.cancel_user,
      cumulateUser: cumulateMap.get(item.ref_date),
    }));

    if (userRecords.length === 0) {
      res.json({ success: true, message: 'No user data found in date range', total: 0, created: 0, updated: 0 });
      return;
    }

    console.log(`[Feishu User Sync] Found ${userRecords.length} user records, syncing to Bitable...`);

    const result = await syncUserGrowthToBitable(userRecords);

    console.log(`[Feishu User Sync] Done. Created: ${result.created}, Updated: ${result.updated}`);

    res.json({
      success: true,
      message: `Synced ${result.created} new, updated ${result.updated} existing user records`,
      total: userRecords.length,
      created: result.created,
      updated: result.updated,
    });
  } catch (err) {
    next(err);
  }
});
