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
	Reporter  Reporter

	runMu sync.Mutex

	deferredWake chan struct{}
}

type CombinedRunResult struct {
	Analytics *RunResult            `json:"analytics,omitempty"`
	Content   *ContentRunResult     `json:"content,omitempty"`
	Layout    *autolayout.RunResult `json:"layout,omitempty"`
}

type RuntimeStatus struct {
	Ready                 bool                              `json:"ready"`
	ReadySemantics        string                            `json:"readySemantics"`
	CredentialsConfigured bool                              `json:"credentialsConfigured"`
	Analytics             *Status                           `json:"analytics,omitempty"`
	ContentStates         []archive.ContentState            `json:"contentStates"`
	EndpointQuotas        []wechat.DailyQuotaStatus         `json:"endpointQuotas"`
	HistoricalCoverage    *archive.HistoricalCoverageReport `json:"historicalCoverage"`
	Layout                *autolayout.Status                `json:"layout,omitempty"`
	ReportingConfigured   bool                              `json:"reportingConfigured"`
	Reason                string                            `json:"reason,omitempty"`
	Warnings              []string                          `json:"warnings,omitempty"`
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
		deferredWake: make(chan struct{}, 1),
		Reporter:     NewFeishuReporterFromEnv(),
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
	runNow := time.Now()
	monitorRecent := withinLayoutMonitoringWindow(runNow)

	// 工作时段内的完整采集会刷新最新已发布文章并补充图集类型；
	// 时段外只推进历史详情，不调用最新草稿与发布页。各官方接口的
	// 独立日配额由 wechat quota 层逐次执行，本次 MaxCalls 仅限制单轮负载。
	var contentErr error
	if monitorRecent {
		result.Content, contentErr = r.Content.Run(ctx)
	} else {
		result.Content, contentErr = r.Content.RunBackfill(ctx)
	}
	if result.Content != nil {
		log.Printf("[OfficialContent] monitor_recent=%t max_calls=%d calls=%d succeeded=%d failed=%d", monitorRecent, r.Content.MaxCalls, result.Content.Calls, result.Content.Succeeded, result.Content.Failed)
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

	// 内容归档完成后，文章/粉丝及其他官方数据接口分别使用自己的额度回填。
	var analyticsErr error
	result.Analytics, analyticsErr = r.Analytics.Run(ctx)
	LogRunResult("OfficialAnalytics", result.Analytics, analyticsErr)

	return result, errors.Join(contentErr, layoutErr, analyticsErr)
}

func (r *Runtime) Status(ctx context.Context) (*RuntimeStatus, error) {
	status := &RuntimeStatus{
		CredentialsConfigured: config.Env.WechatAppID != "" && config.Env.WechatSecret != "",
		ReadySemantics:        "ready 仅表示采集服务和当前接口可运行，不表示历史文章全量已核验；历史口径只看 historicalCoverage.verified",
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
	status.EndpointQuotas = wechat.CurrentDailyQuotaStatuses()
	status.HistoricalCoverage, err = r.HistoricalCoverage(ctx)
	if err != nil {
		return nil, err
	}
	if r.Layout != nil {
		status.Layout, err = r.Layout.Status(ctx)
		if err != nil {
			return nil, err
		}
	}
	status.Ready = status.CredentialsConfigured && analyticsStatus.Ready
	if !status.CredentialsConfigured {
		status.Reason = "WeChat AppID/AppSecret 未配置"
	} else if !analyticsStatus.Ready {
		status.Reason = "尚未成功采集全部 15 个现役官方数据接口，或存在接口权限错误；另有 6 个旧接口已由微信下线"
	}
	if status.Layout != nil && !status.Layout.Ready {
		status.Warnings = append(status.Warnings, status.Layout.Reason)
	}
	status.ReportingConfigured = r.Reporter != nil && r.Reporter.Configured()
	if !status.ReportingConfigured {
		status.Warnings = append(status.Warnings, "官方数据日报/告警未配置 OFFICIAL_FEISHU_WEBHOOK_URL")
	}
	if status.HistoricalCoverage != nil && !status.HistoricalCoverage.Verified {
		status.Warnings = append(status.Warnings, "历史文章覆盖尚未核验；Base 写回保持关闭")
	}
	return status, nil
}

func (r *Runtime) HistoricalCoverage(ctx context.Context) (*archive.HistoricalCoverageReport, error) {
	requirements := make([]archive.HistoricalCoverageRequirement, 0)
	for _, endpoint := range wechat.ActiveDataCubeEndpoints() {
		requirements = append(requirements, archive.HistoricalCoverageRequirement{
			Endpoint:     endpoint.Name,
			EarliestDate: endpoint.EarliestDate,
		})
	}
	return r.Store.AuditHistoricalCoverage(ctx, requirements, time.Now())
}

func (r *Runtime) ApproveHistoricalCoverage(ctx context.Context, approvedBy string) (*archive.HistoricalCoverageReport, error) {
	report, err := r.HistoricalCoverage(ctx)
	if err != nil {
		return nil, err
	}
	if !report.EligibleForUserApproval {
		return report, fmt.Errorf("historical coverage is not eligible for user approval")
	}
	if err := r.Store.SaveHistoricalCoverageApproval(ctx, approvedBy, time.Now()); err != nil {
		return report, err
	}
	return r.HistoricalCoverage(ctx)
}

func (r *Runtime) RevokeHistoricalCoverageApproval(ctx context.Context) (*archive.HistoricalCoverageReport, error) {
	if err := r.Store.RevokeHistoricalCoverageApproval(ctx); err != nil {
		return nil, err
	}
	return r.HistoricalCoverage(ctx)
}

func (r *Runtime) RequireHistoricalCoverageForBaseSync(ctx context.Context) error {
	report, err := r.HistoricalCoverage(ctx)
	if err != nil {
		return err
	}
	if !report.BaseSyncAllowed {
		return fmt.Errorf("official Base sync blocked: historical coverage status=%s verified=%t", report.Status, report.Verified)
	}
	return nil
}

func (r *Runtime) Start(stopCh <-chan struct{}) {
	if r.deferredWake == nil {
		r.deferredWake = make(chan struct{}, 1)
	}
	go r.startDeferredRetryWorker(stopCh)
	if r.Layout != nil {
		// 调度器必须独立于耗时的历史回填启动；即使初始全量采集失败或很慢，
		// 08:30—18:30 的 freepublish 最新页轮询仍会按自己的时钟运行。
		go r.startLayoutPolling(stopCh)
	}
	initialDelay := time.Duration(envInt("OFFICIAL_COLLECTOR_INITIAL_DELAY_SECONDS", 5)) * time.Second
	go func() {
		timer := time.NewTimer(initialDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			initialNow := time.Now()
			if r.Layout != nil && withinLayoutMonitoringWindow(initialNow) {
				layoutResult, layoutErr := r.PollLayout(context.Background())
				if layoutResult != nil {
					log.Printf("[AutoLayout] initial poll discovered=%d delivered=%d failed=%d", layoutResult.Discovered, layoutResult.Delivered, layoutResult.Failed)
				}
				if layoutErr != nil {
					log.Printf("[AutoLayout] initial poll completed with errors: %v", layoutErr)
				}
			}
			if dailyCollectionReady(initialNow) {
				result, err := r.Run(context.Background())
				if err != nil {
					log.Printf("[OfficialCollector] Initial run completed with errors: %v", err)
				}
				r.signalDeferredRetry(result)
			} else {
				log.Printf("[OfficialCollector] Initial D-1 collection deferred until 08:05 Asia/Shanghai")
			}
		case <-stopCh:
			return
		}

		for {
			now := time.Now()
			next := nextScheduledCollection(now)
			timer.Reset(next.Sub(now))
			log.Printf("[OfficialCollector] Next run at %s", next.Format("2006-01-02 15:04:05"))
			select {
			case <-timer.C:
				result, err := r.Run(context.Background())
				if err != nil {
					log.Printf("[OfficialCollector] Scheduled run completed with errors: %v", err)
				}
				r.signalDeferredRetry(result)
				r.reportDaily(context.Background(), result, err)
			case <-stopCh:
				return
			}
		}
	}()
}

func (r *Runtime) signalDeferredRetry(result *CombinedRunResult) {
	if result == nil || result.Analytics == nil || result.Analytics.Deferred == 0 || r.deferredWake == nil {
		return
	}
	select {
	case r.deferredWake <- struct{}{}:
	default:
	}
}

func (r *Runtime) startDeferredRetryWorker(stopCh <-chan struct{}) {
	interval := time.Duration(envInt("ANALYTICS_DEFERRED_RETRY_MINUTES", 30)) * time.Minute
	maxRetries := envInt("ANALYTICS_DEFERRED_MAX_RETRIES", 3)
	for {
		select {
		case <-r.deferredWake:
		case <-stopCh:
			return
		}

		for {
			select {
			case <-r.deferredWake:
				continue
			default:
			}
			break
		}

		for attempt := 1; attempt <= maxRetries; attempt++ {
			timer := time.NewTimer(interval)
			select {
			case <-timer.C:
			case <-stopCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}

			result, err := r.RetryDeferred(context.Background())
			LogRunResult(fmt.Sprintf("OfficialAnalyticsDeferredRetry%d", attempt), result, err)
			status, statusErr := r.Analytics.Status(context.Background())
			if statusErr != nil {
				log.Printf("[OfficialAnalytics] deferred status check failed: %v", statusErr)
				continue
			}
			if len(status.DeferredEndpoints) == 0 {
				break
			}
			if attempt == maxRetries {
				log.Printf("[OfficialAnalytics] deferred windows remain after %d bounded retries: %v", maxRetries, status.DeferredEndpoints)
				r.reportAlert(context.Background(), fmt.Sprintf("官方数据延迟窗口在 %d 次有界重试后仍未恢复：%v", maxRetries, status.DeferredEndpoints))
			}
		}
	}
}

func (r *Runtime) RetryDeferred(ctx context.Context) (*RunResult, error) {
	if !r.runMu.TryLock() {
		return nil, fmt.Errorf("official API collector is already running")
	}
	defer r.runMu.Unlock()
	return r.Analytics.RetryDeferred(ctx)
}

func nextScheduledCollection(now time.Time) time.Time {
	localNow := now.In(wechat.ShanghaiLoc())
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 8, 5, 0, 0, wechat.ShanghaiLoc())
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func dailyCollectionReady(now time.Time) bool {
	localNow := now.In(wechat.ShanghaiLoc())
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 8, 5, 0, 0, wechat.ShanghaiLoc())
	return !localNow.Before(start)
}

func (r *Runtime) startLayoutPolling(stopCh <-chan struct{}) {
	interval := time.Duration(envInt("AUTO_LAYOUT_POLL_INTERVAL_MINUTES", 15)) * time.Minute
	next := nextLayoutPoll(time.Now(), interval)
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()
	log.Printf("[AutoLayout] Polling official freepublish every %s between 08:30 and 18:30; next at %s", interval, next.Format("2006-01-02 15:04:05"))
	for {
		select {
		case <-timer.C:
			result, err := r.PollLayout(context.Background())
			if result != nil {
				log.Printf("[AutoLayout] poll discovered=%d skipped_newspic=%d held_unclassified=%d delivered=%d failed=%d", result.Discovered, result.SkippedNewspic, result.HeldUnclassified, result.Delivered, result.Failed)
			}
			if err != nil {
				log.Printf("[AutoLayout] poll completed with errors: %v", err)
			}
			next = nextLayoutPoll(time.Now(), interval)
			timer.Reset(time.Until(next))
		case <-stopCh:
			return
		}
	}
}

func withinLayoutMonitoringWindow(now time.Time) bool {
	localNow := now.In(wechat.ShanghaiLoc())
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 8, 30, 0, 0, wechat.ShanghaiLoc())
	end := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 18, 30, 0, 0, wechat.ShanghaiLoc())
	return !localNow.Before(start) && !localNow.After(end)
}

func nextLayoutPoll(now time.Time, interval time.Duration) time.Time {
	localNow := now.In(wechat.ShanghaiLoc())
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 8, 30, 0, 0, wechat.ShanghaiLoc())
	end := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 18, 30, 0, 0, wechat.ShanghaiLoc())
	if localNow.Before(start) {
		return start
	}
	steps := int(localNow.Sub(start)/interval) + 1
	next := start.Add(time.Duration(steps) * interval)
	if next.After(end) {
		return start.AddDate(0, 0, 1)
	}
	return next
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
