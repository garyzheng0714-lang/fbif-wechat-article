import { Router } from 'express';
import { z } from 'zod';
import { callWechatApi } from '../services/wechatApi.js';
import type { DashboardData } from '../types/wechat.js';

export const wechatRouter = Router();

// Shared date validation schema
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

// Individual endpoints
const endpoints = [
  { path: '/article-summary', api: 'getarticlesummary' },
  { path: '/article-total', api: 'getarticletotal' },
  { path: '/user-read', api: 'getuserread' },
  { path: '/user-read-hour', api: 'getuserreadhour' },
  { path: '/user-share', api: 'getusershare' },
  { path: '/user-share-hour', api: 'getusersharehour' },
] as const;

for (const { path, api } of endpoints) {
  wechatRouter.post(path, async (req, res, next) => {
    try {
      const parsed = dateRangeSchema.parse(req.body);
      const endDate = clampEndDate(parsed.end_date);
      const data = await callWechatApi(api, parsed.begin_date, endDate);
      res.json(data);
    } catch (err) {
      next(err);
    }
  });
}

// Batch endpoint for dashboard
wechatRouter.post('/dashboard-data', async (req, res, next) => {
  try {
    const parsed = dateRangeSchema.parse(req.body);
    const endDate = clampEndDate(parsed.end_date);
    const beginDate = parsed.begin_date;

    const [articleSummary, articleTotal, userRead, userReadHour, userShare, userShareHour] =
      await Promise.allSettled([
        callWechatApi('getarticlesummary', beginDate, endDate),
        callWechatApi('getarticletotal', beginDate, endDate),
        callWechatApi('getuserread', beginDate, endDate),
        callWechatApi('getuserreadhour', beginDate, endDate),
        callWechatApi('getusershare', beginDate, endDate),
        callWechatApi('getusersharehour', beginDate, endDate),
      ]);

    const result: DashboardData = {
      articleSummary: articleSummary.status === 'fulfilled' ? (articleSummary.value as DashboardData['articleSummary']) : null,
      articleTotal: articleTotal.status === 'fulfilled' ? (articleTotal.value as DashboardData['articleTotal']) : null,
      userRead: userRead.status === 'fulfilled' ? (userRead.value as DashboardData['userRead']) : null,
      userReadHour: userReadHour.status === 'fulfilled' ? (userReadHour.value as DashboardData['userReadHour']) : null,
      userShare: userShare.status === 'fulfilled' ? (userShare.value as DashboardData['userShare']) : null,
      userShareHour: userShareHour.status === 'fulfilled' ? (userShareHour.value as DashboardData['userShareHour']) : null,
    };

    // Log any failures
    const allResults = { articleSummary, articleTotal, userRead, userReadHour, userShare, userShareHour };
    for (const [name, r] of Object.entries(allResults)) {
      if (r.status === 'rejected') {
        console.error(`[Dashboard] ${name} failed:`, r.reason?.message || r.reason);
      }
    }

    res.json(result);
  } catch (err) {
    next(err);
  }
});
