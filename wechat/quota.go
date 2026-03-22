package wechat

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type dailyQuotaState struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type QuotaLimitError struct {
	Endpoint string
}

func (e *QuotaLimitError) Error() string {
	return "WeChat API daily quota limit reached for " + e.Endpoint
}

var (
	quotaMu    sync.Mutex
	quotaCache *dailyQuotaState
)

func quotaFilePath() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, ".wechat-quota.json")
}

func loadQuota(today string) *dailyQuotaState {
	data, err := os.ReadFile(quotaFilePath())
	if err == nil {
		var q dailyQuotaState
		if json.Unmarshal(data, &q) == nil && q.Date == today {
			return &q
		}
	}
	return &dailyQuotaState{Date: today}
}

func saveQuota(q *dailyQuotaState) {
	data, _ := json.Marshal(q)
	_ = os.WriteFile(quotaFilePath(), data, 0644)
}

func dailyQuotaLimit() int {
	if v := os.Getenv("WECHAT_DAILY_QUOTA_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1500
}

func dailyQuotaReserve() int {
	if v := os.Getenv("WECHAT_DAILY_QUOTA_RESERVE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

// checkAndIncrementQuota checks the daily quota and increments the counter.
// Returns QuotaLimitError if the daily limit is reached.
func checkAndIncrementQuota(endpoint string) error {
	quotaMu.Lock()
	defer quotaMu.Unlock()

	today := time.Now().In(ShanghaiLoc()).Format("2006-01-02")
	if quotaCache == nil || quotaCache.Date != today {
		quotaCache = loadQuota(today)
		log.Printf("[Quota] Today (%s) API calls so far: %d/%d", today, quotaCache.Count, dailyQuotaLimit())
	}

	limit := dailyQuotaLimit()
	effectiveLimit := limit - dailyQuotaReserve()
	if effectiveLimit < 0 {
		effectiveLimit = 0
	}
	if quotaCache.Count >= effectiveLimit {
		log.Printf("[Quota] Daily usable limit reached (%d/%d, reserve=%d), blocking call to %s",
			quotaCache.Count, limit, dailyQuotaReserve(), endpoint)
		return &QuotaLimitError{Endpoint: endpoint + " (daily-limit-reached)"}
	}

	quotaCache.Count++
	if quotaCache.Count%100 == 0 || quotaCache.Count == effectiveLimit-1 {
		log.Printf("[Quota] Today's API calls: %d/%d", quotaCache.Count, limit)
	}
	saveQuota(quotaCache)
	return nil
}
