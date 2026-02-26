import { useEffect, useRef, useCallback, useState } from 'react';

export function usePolling(
  fetchFn: () => Promise<void>,
  intervalMs: number = 10_000,
  enabled: boolean = true,
) {
  const [lastRefreshed, setLastRefreshed] = useState<Date | null>(null);
  const timerRef = useRef<number>();
  const isMountedRef = useRef(true);

  const execute = useCallback(async () => {
    try {
      await fetchFn();
      if (isMountedRef.current) setLastRefreshed(new Date());
    } catch (err) {
      console.error('Polling error:', err);
    }
  }, [fetchFn]);

  useEffect(() => {
    isMountedRef.current = true;
    if (!enabled) return;

    execute();
    timerRef.current = window.setInterval(execute, intervalMs);

    return () => {
      isMountedRef.current = false;
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [execute, intervalMs, enabled]);

  return { lastRefreshed };
}
