package wechat

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dailyQuotaState struct {
	Version int            `json:"version,omitempty"`
	Date    string         `json:"date"`
	Count   int            `json:"count,omitempty"`
	Counts  map[string]int `json:"counts,omitempty"`
}

type DailyQuotaStatus struct {
	Endpoint        string `json:"endpoint,omitempty"`
	Date            string `json:"date"`
	Limit           int    `json:"limit"`
	Reserve         int    `json:"reserve"`
	Used            int    `json:"used"`
	UsableRemaining int    `json:"usableRemaining"`
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
	if configured := strings.TrimSpace(os.Getenv("WECHAT_QUOTA_FILE")); configured != "" {
		return configured
	}
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
	return &dailyQuotaState{Version: 2, Date: today, Counts: make(map[string]int)}
}

func saveQuota(q *dailyQuotaState) error {
	q.Version = 2
	data, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("encode WeChat quota: %w", err)
	}
	path := quotaFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create WeChat quota directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".wechat-quota-*")
	if err != nil {
		return fmt.Errorf("create temporary WeChat quota file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary WeChat quota file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary WeChat quota file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary WeChat quota file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary WeChat quota file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace WeChat quota file: %w", err)
	}
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}

func dailyQuotaLimit() int {
	for _, name := range []string{"WECHAT_ENDPOINT_DAILY_QUOTA_LIMIT", "WECHAT_DAILY_QUOTA_LIMIT"} {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 1000 {
				return 1000
			}
			return n
		}
	}
	return 1000
}

func dailyQuotaReserve() int {
	for _, name := range []string{"WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE_PERCENT", "WECHAT_DAILY_QUOTA_RESERVE_PERCENT"} {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			continue
		}
		if percent, err := strconv.ParseFloat(v, 64); err == nil && percent >= 0 && percent < 100 {
			return int(math.Ceil(float64(dailyQuotaLimit()) * percent / 100))
		}
	}
	for _, name := range []string{"WECHAT_ENDPOINT_DAILY_QUOTA_RESERVE", "WECHAT_DAILY_QUOTA_RESERVE"} {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 200
}

func quotaUsed(q *dailyQuotaState, endpoint string) int {
	used := q.Count
	if q.Counts != nil && q.Counts[endpoint] > used {
		used = q.Counts[endpoint]
	}
	return used
}

func currentQuotaState() (string, *dailyQuotaState) {
	today := time.Now().In(ShanghaiLoc()).Format("2006-01-02")
	if quotaCache == nil || quotaCache.Date != today {
		quotaCache = loadQuota(today)
	}
	if quotaCache.Counts == nil {
		quotaCache.Counts = make(map[string]int)
	}
	return today, quotaCache
}

func quotaStatus(today string, q *dailyQuotaState, endpoint string) DailyQuotaStatus {
	limit := dailyQuotaLimit()
	reserve := dailyQuotaReserve()
	used := quotaUsed(q, endpoint)
	usableRemaining := limit - reserve - used
	if usableRemaining < 0 {
		usableRemaining = 0
	}
	return DailyQuotaStatus{
		Endpoint:        endpoint,
		Date:            today,
		Limit:           limit,
		Reserve:         reserve,
		Used:            used,
		UsableRemaining: usableRemaining,
	}
}

// CurrentEndpointQuotaStatus 返回单个官方接口的持久计数快照，不消耗额度。
func CurrentEndpointQuotaStatus(endpoint string) DailyQuotaStatus {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	today, q := currentQuotaState()
	return quotaStatus(today, q, endpoint)
}

// CurrentDailyQuotaStatuses 返回当天已经调用过的各接口配额快照。
func CurrentDailyQuotaStatuses() []DailyQuotaStatus {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	today, q := currentQuotaState()
	endpoints := make([]string, 0, len(q.Counts))
	for endpoint := range q.Counts {
		endpoints = append(endpoints, endpoint)
	}
	sort.Strings(endpoints)
	statuses := make([]DailyQuotaStatus, 0, len(endpoints))
	for _, endpoint := range endpoints {
		statuses = append(statuses, quotaStatus(today, q, endpoint))
	}
	return statuses
}

// CurrentDailyQuotaStatus 保留旧调用兼容；Used 取所有接口中的最大值，
// 不再代表可跨接口共享的全局混池。
func CurrentDailyQuotaStatus() DailyQuotaStatus {
	quotaMu.Lock()
	defer quotaMu.Unlock()
	today, q := currentQuotaState()
	maxUsed := q.Count
	for _, count := range q.Counts {
		if count > maxUsed {
			maxUsed = count
		}
	}
	compat := *q
	compat.Count = maxUsed
	return quotaStatus(today, &compat, "")
}

// checkAndIncrementQuota checks the daily quota and increments the counter.
// Returns QuotaLimitError if the daily limit is reached.
func checkAndIncrementQuota(endpoint string) error {
	quotaMu.Lock()
	defer quotaMu.Unlock()

	today := time.Now().In(ShanghaiLoc()).Format("2006-01-02")
	if quotaCache == nil || quotaCache.Date != today {
		quotaCache = loadQuota(today)
		log.Printf("[Quota] Loaded endpoint quota counters for %s", today)
	}
	if quotaCache.Counts == nil {
		quotaCache.Counts = make(map[string]int)
	}

	limit := dailyQuotaLimit()
	effectiveLimit := limit - dailyQuotaReserve()
	if effectiveLimit < 0 {
		effectiveLimit = 0
	}
	used := quotaUsed(quotaCache, endpoint)
	if used >= effectiveLimit {
		log.Printf("[Quota] Endpoint usable limit reached for %s (%d/%d, reserve=%d)",
			endpoint, used, limit, dailyQuotaReserve())
		return &QuotaLimitError{Endpoint: endpoint + " (daily-limit-reached)"}
	}

	previous, existed := quotaCache.Counts[endpoint]
	used++
	quotaCache.Counts[endpoint] = used
	if err := saveQuota(quotaCache); err != nil {
		if existed {
			quotaCache.Counts[endpoint] = previous
		} else {
			delete(quotaCache.Counts, endpoint)
		}
		return fmt.Errorf("persist WeChat quota reservation for %s: %w", endpoint, err)
	}
	if used%100 == 0 || used == effectiveLimit-1 {
		log.Printf("[Quota] Today's %s calls: %d/%d", endpoint, used, limit)
	}
	return nil
}
