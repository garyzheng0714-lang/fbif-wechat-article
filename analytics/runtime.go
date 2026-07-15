package analytics

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/autolayout"
	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

type Runtime struct {
	Store     *archive.Store
	Analytics *Collector
	Content   *ContentCollector
	Layout    *autolayout.Dispatcher

	runMu sync.Mutex
}

type CombinedRunResult struct {
	Analytics *RunResult            `json:"analytics,omitempty"`
	Content   *ContentRunResult     `json:"content,omitempty"`
	Layout    *autolayout.RunResult `json:"layout,omitempty"`
}

type RuntimeStatus struct {
	Ready                 bool                   `json:"ready"`
	CredentialsConfigured bool                   `json:"credentialsConfigured"`
	Analytics             *Status                `json:"analytics,omitempty"`
	ContentStates         []archive.ContentState `json:"contentStates"`
	Layout                *autolayout.Status     `json:"layout,omitempty"`
	Reason                string                 `json:"reason,omitempty"`
}

func NewRuntimeFromEnv() (*Runtime, error) {
	databasePath := strings.TrimSpace(os.Getenv("OFFICIAL_API_DB_PATH"))
	if databasePath == "" {
		databasePath = "./data/wechat-official.db"
	}
	store, err := archive.Open(databasePath)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{
		Store: store,
		Analytics: &Collector{
			Client:        wechat.NewDataCubeClient(),
			Store:         store,
			MaxCalls:      envInt("ANALYTICS_MAX_CALLS_PER_RUN", 2000),
			BackfillStart: strings.TrimSpace(os.Getenv("ANALYTICS_BACKFILL_START")),
		},
		Content: &ContentCollector{
			Client:   wechat.NewContentClient(),
			Store:    store,
			MaxCalls: envInt("CONTENT_MAX_CALLS_PER_RUN", 400),
		},
	}
	layoutDispatcher, err := autolayout.NewFromEnv(store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	runtime.Layout = layoutDispatcher
	return runtime, nil
}

func (r *Runtime) Close() error {
	if r == nil || r.Store == nil {
		return nil
	}
	return r.Store.Close()
}

func (r *Runtime) Run(ctx context.Context) (*CombinedRunResult, error) {
	if !r.runMu.TryLock() {
		return nil, fmt.Errorf("official API collector is already running")
	}
	defer r.runMu.Unlock()

	result := &CombinedRunResult{}
	pollInterval := time.Duration(envInt("AUTO_LAYOUT_POLL_INTERVAL_MINUTES", 15)) * time.Minute

	// 先把最新已发布文章、图集类型和文章详情落库。历史详情也按剩余额度执行，
	// 但必须给今天余下的 15 分钟发布轮询留出固定预算。
	contentConfiguredMax := r.Content.MaxCalls
	quotaBeforeContent := wechat.CurrentDailyQuotaStatus()
	contentBudget, contentPollReserve := quotaAwareCallBudget(
		contentConfiguredMax, quotaBeforeContent.UsableRemaining, time.Now(), pollInterval, r.Layout != nil, 2,
	)
	var contentErr error
	if contentBudget > 0 {
		r.Content.MaxCalls = contentBudget
		result.Content, contentErr = r.Content.Run(ctx)
		r.Content.MaxCalls = contentConfiguredMax
	} else {
		nowMs := time.Now().UnixMilli()
		result.Content = &ContentRunResult{StartedAt: nowMs, FinishedAt: nowMs}
	}
	if result.Content != nil {
		log.Printf("[OfficialContent] budget=%d poll_reserve=%d calls=%d succeeded=%d failed=%d", contentBudget, contentPollReserve, result.Content.Calls, result.Content.Succeeded, result.Content.Failed)
	}
	if contentErr != nil {
		log.Printf("[OfficialContent] Completed with errors: %v", contentErr)
	}
	var layoutErr error
	if r.Layout != nil {
		result.Layout, layoutErr = r.Layout.Sync(ctx)
		if result.Layout != nil {
			log.Printf("[AutoLayout] bootstrapped=%v baselined=%d discovered=%d delivered=%d failed=%d", result.Layout.Bootstrapped, result.Layout.Baselined, result.Layout.Discovered, result.Layout.Delivered, result.Layout.Failed)
		}
		if layoutErr != nil {
			log.Printf("[AutoLayout] Completed with errors: %v", layoutErr)
		}
	}

	// 内容归档完成后，文章/粉丝及其他官方数据接口才使用剩余额度回填。
	// 预算实时扣除已用量和今天余下的发布轮询；硬保留的 2% 已由 quota 层排除。
	analyticsConfiguredMax := r.Analytics.MaxCalls
	quotaBeforeAnalytics := wechat.CurrentDailyQuotaStatus()
	analyticsBudget, analyticsPollReserve := quotaAwareCallBudget(
		analyticsConfiguredMax, quotaBeforeAnalytics.UsableRemaining, time.Now(), pollInterval, r.Layout != nil, 0,
	)
	var analyticsErr error
	if analyticsBudget > 0 {
		r.Analytics.MaxCalls = analyticsBudget
		result.Analytics, analyticsErr = r.Analytics.Run(ctx)
		r.Analytics.MaxCalls = analyticsConfiguredMax
	} else {
		nowMs := time.Now().UnixMilli()
		result.Analytics = &RunResult{StartedAt: nowMs, FinishedAt: nowMs, QuotaExhausted: quotaBeforeAnalytics.UsableRemaining == 0}
	}
	log.Printf("[OfficialAnalytics] budget=%d poll_reserve=%d quota_used=%d usable_remaining=%d", analyticsBudget, analyticsPollReserve, quotaBeforeAnalytics.Used, quotaBeforeAnalytics.UsableRemaining)
	LogRunResult("OfficialAnalytics", result.Analytics, analyticsErr)

	return result, errors.Join(contentErr, layoutErr, analyticsErr)
}

// quotaAwareCallBudget 把 98% 可用额度再分为“今天余下的发布轮询”和“本次回填”。
// minimum 只用于内容采集，保证还有额度时至少能完成 draft + freepublish 最新页。
func quotaAwareCallBudget(configuredMax, usableRemaining int, now time.Time, pollInterval time.Duration, layoutEnabled bool, minimum int) (budget int, pollReserve int) {
	if configuredMax <= 0 || usableRemaining <= 0 {
		return 0, 0
	}
	if layoutEnabled && pollInterval > 0 {
		localNow := now.In(wechat.ShanghaiLoc())
		nextMidnight := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, wechat.ShanghaiLoc())
		remaining := nextMidnight.Sub(localNow)
		polls := int((remaining + pollInterval - 1) / pollInterval)
		pollReserve = polls * 2 // 每轮只调用 draft/batchget + freepublish/batchget。
	}
	availableForRun := usableRemaining - pollReserve
	if availableForRun < minimum {
		availableForRun = min(minimum, usableRemaining)
	}
	return min(configuredMax, max(availableForRun, 0)), pollReserve
}

func (r *Runtime) Status(ctx context.Context) (*RuntimeStatus, error) {
	status := &RuntimeStatus{
		CredentialsConfigured: config.Env.WechatAppID != "" && config.Env.WechatSecret != "",
	}
	analyticsStatus, err := r.Analytics.Status(ctx)
	if err != nil {
		return nil, err
	}
	status.Analytics = analyticsStatus
	contentStates, err := r.Store.ListContentStates(ctx)
	if err != nil {
		return nil, err
	}
	status.ContentStates = contentStates
	if r.Layout != nil {
		status.Layout, err = r.Layout.Status(ctx)
		if err != nil {
			return nil, err
		}
	}
	status.Ready = status.CredentialsConfigured && analyticsStatus.Ready && (status.Layout == nil || status.Layout.Ready)
	if !status.CredentialsConfigured {
		status.Reason = "WeChat AppID/AppSecret 未配置"
	} else if !analyticsStatus.Ready {
		status.Reason = "尚未成功采集全部 15 个现役官方数据接口，或存在接口权限错误；另有 6 个旧接口已由微信下线"
	} else if status.Layout != nil && !status.Layout.Ready {
		status.Reason = status.Layout.Reason
	}
	return status, nil
}

func (r *Runtime) Start(stopCh <-chan struct{}) {
	initialDelay := time.Duration(envInt("OFFICIAL_COLLECTOR_INITIAL_DELAY_SECONDS", 5)) * time.Second
	go func() {
		timer := time.NewTimer(initialDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			if _, err := r.Run(context.Background()); err != nil {
				log.Printf("[OfficialCollector] Initial run completed with errors: %v", err)
			}
			if r.Layout != nil {
				go r.startLayoutPolling(stopCh)
			}
		case <-stopCh:
			return
		}

		for {
			now := time.Now().In(wechat.ShanghaiLoc())
			next := time.Date(now.Year(), now.Month(), now.Day(), 8, 30, 0, 0, wechat.ShanghaiLoc())
			if !next.After(now) {
				next = next.AddDate(0, 0, 1)
			}
			timer.Reset(next.Sub(now))
			log.Printf("[OfficialCollector] Next run at %s", next.Format("2006-01-02 15:04:05"))
			select {
			case <-timer.C:
				if _, err := r.Run(context.Background()); err != nil {
					log.Printf("[OfficialCollector] Scheduled run completed with errors: %v", err)
				}
			case <-stopCh:
				return
			}
		}
	}()
}

func (r *Runtime) startLayoutPolling(stopCh <-chan struct{}) {
	interval := time.Duration(envInt("AUTO_LAYOUT_POLL_INTERVAL_MINUTES", 15)) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	log.Printf("[AutoLayout] Polling official draft types + freepublish every %s", interval)
	for {
		select {
		case <-ticker.C:
			result, err := r.PollLayout(context.Background())
			if result != nil {
				log.Printf("[AutoLayout] poll discovered=%d skipped_newspic=%d held_unclassified=%d delivered=%d failed=%d", result.Discovered, result.SkippedNewspic, result.HeldUnclassified, result.Delivered, result.Failed)
			}
			if err != nil {
				log.Printf("[AutoLayout] poll completed with errors: %v", err)
			}
		case <-stopCh:
			return
		}
	}
}

// PollLayout 串行执行一次轻量官方发布轮询和 outbox 投递。与完整日采集共用
// runMu，避免同时写 SQLite 或重复消耗微信 API 配额。
func (r *Runtime) PollLayout(ctx context.Context) (*autolayout.RunResult, error) {
	if r.Layout == nil {
		return nil, fmt.Errorf("auto-layout is disabled")
	}
	if !r.runMu.TryLock() {
		return nil, fmt.Errorf("official API collector is already running")
	}
	defer r.runMu.Unlock()
	_, refreshErr := r.Content.RefreshPublished(ctx)
	result, layoutErr := r.Layout.Sync(ctx)
	return result, errors.Join(refreshErr, layoutErr)
}

func Enabled() bool {
	value := strings.TrimSpace(os.Getenv("ENABLE_OFFICIAL_API_COLLECTOR"))
	return value == "" || value == "1" || strings.EqualFold(value, "true")
}

func envInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}
