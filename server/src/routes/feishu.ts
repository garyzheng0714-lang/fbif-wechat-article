import { Router } from 'express';
import { z } from 'zod';
import { callWechatApi } from '../services/wechatApi.js';
import { syncArticlesToBitable } from '../services/feishuBitable.js';
import type { ArticleRecord } from '../services/feishuBitable.js';
import type { ArticleSummaryItem } from '../types/wechat.js';

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

// Sync WeChat article data to Feishu Bitable
feishuRouter.post('/sync', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    console.log(`[Feishu Sync] Fetching WeChat data for ${beginDate} ~ ${endDate}`);

    // Fetch article summary data from WeChat
    const summaryData = await callWechatApi('getarticlesummary', beginDate, endDate);
    const items = (summaryData.list || []) as ArticleSummaryItem[];

    if (items.length === 0) {
      res.json({ success: true, message: 'No articles found in date range', created: 0, skipped: 0 });
      return;
    }

    // Map WeChat data to Bitable records
    const articles: ArticleRecord[] = items.map((item) => ({
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
    }));

    console.log(`[Feishu Sync] Found ${articles.length} articles, syncing to Bitable...`);

    const result = await syncArticlesToBitable(articles);

    console.log(`[Feishu Sync] Done. Created: ${result.created}, Skipped: ${result.skipped}`);

    res.json({
      success: true,
      message: `Synced ${result.created} new articles to Feishu Bitable`,
      total: articles.length,
      created: result.created,
      skipped: result.skipped,
    });
  } catch (err) {
    next(err);
  }
});
