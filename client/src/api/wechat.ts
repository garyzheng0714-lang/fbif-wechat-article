import type { DashboardData, ArticleTotalItem } from '../types/wechat';

async function post<T>(path: string, body: object): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json();
}

export const api = {
  fetchDashboardData(beginDate: string, endDate: string) {
    return post<DashboardData>('/api/wechat/dashboard-data', {
      begin_date: beginDate,
      end_date: endDate,
    });
  },

  fetchArticleTotal(beginDate: string, endDate: string) {
    return post<{ list: ArticleTotalItem[] }>('/api/wechat/article-total', {
      begin_date: beginDate,
      end_date: endDate,
    });
  },

  getConfigStatus() {
    return get<{ configured: boolean; appid: string }>('/api/config/status');
  },

  saveCredentials(appid: string, secret: string) {
    return post<{ success: boolean; appid: string }>('/api/config/credentials', {
      appid,
      secret,
    });
  },
};
