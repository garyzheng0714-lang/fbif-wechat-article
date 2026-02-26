import { useState, useCallback } from 'react';
import { api } from '../api/wechat';
import { usePolling } from './usePolling';
import type { DashboardData } from '../types/wechat';

export function useWechatData(
  dateRange: [string, string],
  pollingEnabled: boolean = true,
) {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isFirstLoad, setIsFirstLoad] = useState(true);

  const fetchData = useCallback(async () => {
    try {
      if (isFirstLoad) setLoading(true);
      const result = await api.fetchDashboardData(dateRange[0], dateRange[1]);
      setData(result);
      setError(null);
      setIsFirstLoad(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch data');
    } finally {
      setLoading(false);
    }
  }, [dateRange[0], dateRange[1], isFirstLoad]);

  const { lastRefreshed } = usePolling(fetchData, 10 * 60 * 1000, pollingEnabled);

  return { data, loading, error, lastRefreshed };
}
