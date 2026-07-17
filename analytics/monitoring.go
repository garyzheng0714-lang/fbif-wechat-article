package analytics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

const (
	officialEndpointStaleAfter = 26 * time.Hour
	officialDeferredStaleAfter = 2 * time.Hour
	officialLayoutStaleAfter   = 20 * time.Minute
	officialPublishedPollStale = 45 * time.Minute
)

type MonitoringStatus struct {
	Ready                    bool                      `json:"ready"`
	CheckedAt                int64                     `json:"checkedAt"`
	StaleEndpoints           []string                  `json:"staleEndpoints,omitempty"`
	FailedEndpoints          []string                  `json:"failedEndpoints,omitempty"`
	QuotaLimitedEndpoints    []string                  `json:"quotaLimitedEndpoints,omitempty"`
	DeferredEndpoints        []string                  `json:"deferredEndpoints,omitempty"`
	OldestDeferredAgeSeconds int64                     `json:"oldestDeferredAgeSeconds"`
	StaleContentStreams      []string                  `json:"staleContentStreams,omitempty"`
	LayoutOutbox             archive.LayoutOutboxStats `json:"layoutOutbox"`
	OldestLayoutOutboxAge    int64                     `json:"oldestLayoutOutboxAgeSeconds"`
	ReportingConfigured      bool                      `json:"reportingConfigured"`
	Issues                   []string                  `json:"issues,omitempty"`
}

func (r *Runtime) MonitoringStatus(ctx context.Context, now time.Time) (*MonitoringStatus, error) {
	if r == nil || r.Store == nil || r.Analytics == nil {
		return nil, fmt.Errorf("official runtime is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	status := &MonitoringStatus{CheckedAt: now.UnixMilli()}
	analyticsStatus, err := r.Analytics.Status(ctx)
	if err != nil {
		return nil, err
	}
	byEndpoint := make(map[string]archive.EndpointState, len(analyticsStatus.States))
	for _, state := range analyticsStatus.States {
		byEndpoint[state.Endpoint] = state
	}
	for _, endpoint := range r.Analytics.activeEndpoints() {
		state, ok := byEndpoint[endpoint.Name]
		if !ok || state.LastSuccessAt == 0 || now.Sub(time.UnixMilli(state.LastSuccessAt)) > officialEndpointStaleAfter {
			status.StaleEndpoints = append(status.StaleEndpoints, endpoint.Name)
		}
		if ok && state.LastError != "" {
			if isQuotaLimitMessage(state.LastError) {
				status.QuotaLimitedEndpoints = append(status.QuotaLimitedEndpoints, endpoint.Name)
			} else {
				status.FailedEndpoints = append(status.FailedEndpoints, endpoint.Name)
			}
		}
		if ok && state.DeferredPending {
			status.DeferredEndpoints = append(status.DeferredEndpoints, endpoint.Name)
			age := ageSecondsFromMillis(now, state.LastDeferredAt)
			if age > status.OldestDeferredAgeSeconds {
				status.OldestDeferredAgeSeconds = age
			}
		}
	}

	contentStates, err := r.Store.ListContentStates(ctx)
	if err != nil {
		return nil, err
	}
	contentByStream := make(map[string]archive.ContentState, len(contentStates))
	for _, state := range contentStates {
		contentByStream[state.Stream] = state
	}
	publishedMaxAge := officialEndpointStaleAfter
	if withinPublishedWatchdogWindow(now) {
		publishedMaxAge = officialPublishedPollStale
	}
	freepublish, ok := contentByStream["freepublish"]
	if !ok || freepublish.LastRecentSuccessAt == 0 || now.Sub(time.UnixMilli(freepublish.LastRecentSuccessAt)) > publishedMaxAge {
		status.StaleContentStreams = append(status.StaleContentStreams, "freepublish")
	}

	status.LayoutOutbox, err = r.Store.LayoutStats(ctx)
	if err != nil {
		return nil, err
	}
	status.OldestLayoutOutboxAge = maxInt64(
		ageSecondsFromMillis(now, status.LayoutOutbox.OldestPendingAt),
		ageSecondsFromMillis(now, status.LayoutOutbox.OldestFailedAt),
	)
	status.ReportingConfigured = r.Reporter != nil && r.Reporter.Configured()
	// 高频外部探针只检查调度、端点新鲜度和持久队列。历史覆盖审计会扫描
	// 多张大表，必须由受保护的 /coverage 或日报低频执行，不能拖慢看门狗。

	if len(status.StaleEndpoints) > 0 {
		status.Issues = append(status.Issues, fmt.Sprintf("stale_endpoints:%d", len(status.StaleEndpoints)))
	}
	if len(status.FailedEndpoints) > 0 {
		status.Issues = append(status.Issues, fmt.Sprintf("failed_endpoints:%d", len(status.FailedEndpoints)))
	}
	if status.OldestDeferredAgeSeconds > int64(officialDeferredStaleAfter/time.Second) {
		status.Issues = append(status.Issues, fmt.Sprintf("deferred_stale:%ds", status.OldestDeferredAgeSeconds))
	}
	if len(status.StaleContentStreams) > 0 {
		status.Issues = append(status.Issues, "freepublish_poll_stale")
	}
	if r.Layout == nil {
		status.Issues = append(status.Issues, "official_layout_not_configured")
	} else if status.LayoutOutbox.InitializedAt == 0 {
		status.Issues = append(status.Issues, "official_layout_not_initialized")
	}
	if status.OldestLayoutOutboxAge > int64(officialLayoutStaleAfter/time.Second) {
		status.Issues = append(status.Issues, fmt.Sprintf("official_layout_outbox_stale:%ds", status.OldestLayoutOutboxAge))
	}
	if status.LayoutOutbox.Failed > 0 {
		status.Issues = append(status.Issues, fmt.Sprintf("official_layout_failed:%d", status.LayoutOutbox.Failed))
	}
	if !status.ReportingConfigured {
		status.Issues = append(status.Issues, "official_reporter_not_configured")
	}
	sort.Strings(status.StaleEndpoints)
	sort.Strings(status.FailedEndpoints)
	sort.Strings(status.QuotaLimitedEndpoints)
	sort.Strings(status.DeferredEndpoints)
	sort.Strings(status.StaleContentStreams)
	sort.Strings(status.Issues)
	status.Ready = len(status.Issues) == 0
	return status, nil
}

func isQuotaLimitMessage(message string) bool {
	return strings.Contains(strings.ToLower(message), "daily quota limit reached")
}

func ageSecondsFromMillis(now time.Time, millis int64) int64 {
	if millis <= 0 {
		return 0
	}
	then := time.UnixMilli(millis)
	if then.After(now) {
		return 0
	}
	return int64(now.Sub(then) / time.Second)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func withinPublishedWatchdogWindow(now time.Time) bool {
	local := now.In(wechat.ShanghaiLoc())
	start := time.Date(local.Year(), local.Month(), local.Day(), 9, 15, 0, 0, wechat.ShanghaiLoc())
	end := time.Date(local.Year(), local.Month(), local.Day(), 19, 15, 0, 0, wechat.ShanghaiLoc())
	return !local.Before(start) && !local.After(end)
}
