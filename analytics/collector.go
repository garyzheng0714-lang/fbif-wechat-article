package analytics

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

type APIClient interface {
	Call(ctx context.Context, endpoint wechat.DataCubeEndpoint, beginDate, endDate string) (*wechat.RawAPIResponse, error)
}

type Collector struct {
	Client        APIClient
	Store         *archive.Store
	Now           func() time.Time
	MaxCalls      int
	BackfillStart string
	Endpoints     []wechat.DataCubeEndpoint

	runMu sync.Mutex
}

type RunResult struct {
	StartedAt      int64             `json:"startedAt"`
	FinishedAt     int64             `json:"finishedAt"`
	Calls          int               `json:"calls"`
	Succeeded      int               `json:"succeeded"`
	Failed         int               `json:"failed"`
	RecentComplete bool              `json:"recentComplete"`
	QuotaExhausted bool              `json:"quotaExhausted"`
	RetiredProbed  int               `json:"retiredProbed"`
	Errors         map[string]string `json:"errors,omitempty"`
}

func (c *Collector) CollectRange(ctx context.Context, endpointName, beginDate, endDate string) (int, error) {
	endpoint, ok := wechat.DataCubeEndpointByName(endpointName)
	if !ok {
		return 0, fmt.Errorf("unknown analytics endpoint %q", endpointName)
	}
	windows, err := wechat.SplitDataCubeRange(endpoint, beginDate, endDate)
	if err != nil {
		return 0, err
	}
	if !c.runMu.TryLock() {
		return 0, fmt.Errorf("analytics collector is already running")
	}
	defer c.runMu.Unlock()

	var callErrors []error
	for _, window := range windows {
		if err := c.collectWindow(ctx, endpoint, window.Begin, window.End, ""); err != nil {
			callErrors = append(callErrors, err)
		}
	}
	return len(windows), errors.Join(callErrors...)
}

func (c *Collector) Run(ctx context.Context) (*RunResult, error) {
	if c.Client == nil || c.Store == nil {
		return nil, fmt.Errorf("analytics collector is not configured")
	}
	if !c.runMu.TryLock() {
		return nil, fmt.Errorf("analytics collector is already running")
	}
	defer c.runMu.Unlock()

	now := c.now()
	result := &RunResult{
		StartedAt: now.UnixMilli(),
		Errors:    make(map[string]string),
	}
	maxCalls := c.MaxCalls
	if maxCalls <= 0 {
		maxCalls = 500
	}
	endpoints := c.activeEndpoints()
	yesterday := startOfDay(now.In(wechat.ShanghaiLoc())).AddDate(0, 0, -1)
	failedEndpoint := make(map[string]bool)
	var runErrors []error

	// Pass 1: every endpoint gets yesterday first. This prevents a historical
	// backfill from consuming quota needed for current operational data.
	for _, endpoint := range endpoints {
		if result.Calls >= maxCalls {
			break
		}
		if dateBefore(yesterday, endpoint.EarliestDate) {
			continue
		}
		date := yesterday.Format("2006-01-02")
		err := c.collectWindow(ctx, endpoint, date, date, "")
		result.Calls++
		if err != nil {
			result.Failed++
			failedEndpoint[endpoint.Name] = true
			result.Errors[endpoint.Name] = err.Error()
			runErrors = append(runErrors, err)
			if isQuotaError(err) {
				result.QuotaExhausted = true
				break
			}
			continue
		}
		result.Succeeded++
	}
	result.RecentComplete = recentPassComplete(endpoints, result, failedEndpoint, yesterday)

	// The six legacy endpoints currently return official error 47009 (offline).
	// Probe each once and archive that response, but never waste daily quota by
	// retrying a globally retired interface.
	if !result.QuotaExhausted {
		for _, endpoint := range c.retiredEndpoints() {
			if result.Calls >= maxCalls {
				break
			}
			terminal, err := c.Store.HasTerminalFetch(ctx, endpoint.Name)
			if err != nil {
				runErrors = append(runErrors, err)
				continue
			}
			if terminal {
				continue
			}
			date := yesterday.Format("2006-01-02")
			expectedRetired, err := c.probeRetired(ctx, endpoint, date)
			result.Calls++
			if expectedRetired {
				result.RetiredProbed++
				continue
			}
			if err != nil {
				result.Failed++
				result.Errors[endpoint.Name] = err.Error()
				runErrors = append(runErrors, err)
				if isQuotaError(err) {
					result.QuotaExhausted = true
					break
				}
			}
		}
	}

	// Pass 2: total-series endpoints keep changing for 7/30 days after publish.
	// Refresh those publish dates newest-first before starting historical work.
	if !result.QuotaExhausted {
		for _, endpoint := range endpoints {
			if endpoint.RefreshDays <= 1 || failedEndpoint[endpoint.Name] {
				continue
			}
			for daysAgo := 1; daysAgo < endpoint.RefreshDays && result.Calls < maxCalls; daysAgo++ {
				dateTime := yesterday.AddDate(0, 0, -daysAgo)
				if dateBefore(dateTime, endpoint.EarliestDate) {
					break
				}
				date := dateTime.Format("2006-01-02")
				err := c.collectWindow(ctx, endpoint, date, date, "")
				result.Calls++
				if err != nil {
					result.Failed++
					failedEndpoint[endpoint.Name] = true
					result.Errors[endpoint.Name] = err.Error()
					runErrors = append(runErrors, err)
					if isQuotaError(err) {
						result.QuotaExhausted = true
					}
					break
				}
				result.Succeeded++
			}
			if result.QuotaExhausted || result.Calls >= maxCalls {
				break
			}
		}
	}

	// Pass 3: round-robin historical backfill. Each endpoint advances only
	// after a successful, durably stored response.
	for !result.QuotaExhausted && result.Calls < maxCalls {
		progressed := false
		for _, endpoint := range endpoints {
			if result.Calls >= maxCalls || failedEndpoint[endpoint.Name] {
				continue
			}
			window, nextDate, complete, err := c.nextBackfillWindow(ctx, endpoint, yesterday)
			if err != nil {
				failedEndpoint[endpoint.Name] = true
				result.Errors[endpoint.Name] = err.Error()
				runErrors = append(runErrors, err)
				continue
			}
			if complete {
				continue
			}

			progressed = true
			err = c.collectWindow(ctx, endpoint, window.Begin, window.End, nextDate)
			result.Calls++
			if err != nil {
				result.Failed++
				failedEndpoint[endpoint.Name] = true
				result.Errors[endpoint.Name] = err.Error()
				runErrors = append(runErrors, err)
				if isQuotaError(err) {
					result.QuotaExhausted = true
					break
				}
				continue
			}
			result.Succeeded++
		}
		if !progressed {
			break
		}
	}

	result.FinishedAt = c.now().UnixMilli()
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, errors.Join(runErrors...)
}

func (c *Collector) probeRetired(ctx context.Context, endpoint wechat.DataCubeEndpoint, date string) (bool, error) {
	fetchedAt := c.now()
	response, callErr := c.Client.Call(ctx, endpoint, date, date)
	record := archive.FetchRecord{
		Endpoint:  endpoint.Name,
		Category:  endpoint.Category,
		BeginDate: date,
		EndDate:   date,
		FetchedAt: fetchedAt,
		Success:   callErr == nil,
	}
	if response != nil {
		record.RequestJSON = response.RequestBody
		record.ResponseJSON = response.Body
		record.HTTPStatus = response.HTTPStatus
		record.WechatErrCode = response.ErrCode
		record.WechatErrMsg = response.ErrMsg
	}
	if callErr != nil {
		record.Error = callErr.Error()
	}
	if _, err := c.Store.SaveFetch(ctx, record); err != nil {
		return false, err
	}
	if response != nil && response.ErrCode == 47009 {
		return true, nil
	}
	return false, callErr
}

func (c *Collector) collectWindow(ctx context.Context, endpoint wechat.DataCubeEndpoint, beginDate, endDate, nextBackfillDate string) error {
	fetchedAt := c.now()
	response, callErr := c.Client.Call(ctx, endpoint, beginDate, endDate)
	record := archive.FetchRecord{
		Endpoint:  endpoint.Name,
		Category:  endpoint.Category,
		BeginDate: beginDate,
		EndDate:   endDate,
		FetchedAt: fetchedAt,
		Success:   callErr == nil,
	}
	if response != nil {
		record.RequestJSON = response.RequestBody
		record.ResponseJSON = response.Body
		record.HTTPStatus = response.HTTPStatus
		record.WechatErrCode = response.ErrCode
		record.WechatErrMsg = response.ErrMsg
	}
	if callErr != nil {
		record.Error = callErr.Error()
	}
	if _, err := c.Store.SaveFetch(ctx, record); err != nil {
		return fmt.Errorf("persist %s %s..%s: %w", endpoint.Name, beginDate, endDate, err)
	}

	if callErr != nil {
		if err := c.Store.MarkFailure(ctx, endpoint.Name, endpoint.Category, callErr.Error(), fetchedAt); err != nil {
			return fmt.Errorf("persist %s failure status: %w", endpoint.Name, err)
		}
		return fmt.Errorf("%s %s..%s: %w", endpoint.Name, beginDate, endDate, callErr)
	}
	if err := c.Store.MarkSuccess(ctx, endpoint.Name, endpoint.Category, beginDate, endDate, nextBackfillDate, fetchedAt); err != nil {
		return fmt.Errorf("persist %s success status: %w", endpoint.Name, err)
	}
	return nil
}

func (c *Collector) nextBackfillWindow(ctx context.Context, endpoint wechat.DataCubeEndpoint, yesterday time.Time) (wechat.DateWindow, string, bool, error) {
	state, err := c.Store.GetState(ctx, endpoint.Name)
	if err != nil {
		return wechat.DateWindow{}, "", false, err
	}
	startDate := endpoint.EarliestDate
	if c.BackfillStart != "" && c.BackfillStart > startDate {
		startDate = c.BackfillStart
	}
	if state != nil && state.NextBackfillDate != "" {
		startDate = state.NextBackfillDate
	}

	start, err := time.ParseInLocation("2006-01-02", startDate, wechat.ShanghaiLoc())
	if err != nil {
		return wechat.DateWindow{}, "", false, fmt.Errorf("invalid backfill date for %s: %w", endpoint.Name, err)
	}
	if start.After(yesterday) {
		return wechat.DateWindow{}, "", true, nil
	}
	end := start.AddDate(0, 0, endpoint.MaxSpanDays-1)
	if end.After(yesterday) {
		end = yesterday
	}
	next := end.AddDate(0, 0, 1).Format("2006-01-02")
	return wechat.DateWindow{
		Begin: start.Format("2006-01-02"),
		End:   end.Format("2006-01-02"),
	}, next, false, nil
}

type Status struct {
	Ready               bool                    `json:"ready"`
	DocumentedEndpoints int                     `json:"documentedEndpoints"`
	ExpectedEndpoints   int                     `json:"expectedEndpoints"`
	HealthyEndpoints    int                     `json:"healthyEndpoints"`
	RetiredEndpoints    []string                `json:"retiredEndpoints"`
	MissingEndpoints    []string                `json:"missingEndpoints,omitempty"`
	FailedEndpoints     map[string]string       `json:"failedEndpoints,omitempty"`
	States              []archive.EndpointState `json:"states"`
	Storage             archive.StoreStats      `json:"storage"`
}

func (c *Collector) Status(ctx context.Context) (*Status, error) {
	states, err := c.Store.ListStates(ctx)
	if err != nil {
		return nil, err
	}
	stats, err := c.Store.Stats(ctx)
	if err != nil {
		return nil, err
	}
	byEndpoint := make(map[string]archive.EndpointState, len(states))
	for _, state := range states {
		byEndpoint[state.Endpoint] = state
	}
	status := &Status{
		DocumentedEndpoints: len(c.configuredEndpoints()),
		ExpectedEndpoints:   len(c.activeEndpoints()),
		FailedEndpoints:     make(map[string]string),
		States:              states,
		Storage:             stats,
	}
	for _, endpoint := range c.retiredEndpoints() {
		status.RetiredEndpoints = append(status.RetiredEndpoints, endpoint.Name)
	}
	for _, endpoint := range c.activeEndpoints() {
		state, ok := byEndpoint[endpoint.Name]
		if !ok {
			status.MissingEndpoints = append(status.MissingEndpoints, endpoint.Name)
			continue
		}
		if state.LastError != "" {
			status.FailedEndpoints[endpoint.Name] = state.LastError
			continue
		}
		if state.LastSuccessAt == 0 {
			status.MissingEndpoints = append(status.MissingEndpoints, endpoint.Name)
			continue
		}
		status.HealthyEndpoints++
	}
	sort.Strings(status.MissingEndpoints)
	sort.Strings(status.RetiredEndpoints)
	status.Ready = status.HealthyEndpoints == status.ExpectedEndpoints
	if len(status.FailedEndpoints) == 0 {
		status.FailedEndpoints = nil
	}
	return status, nil
}

func (c *Collector) configuredEndpoints() []wechat.DataCubeEndpoint {
	if len(c.Endpoints) > 0 {
		result := make([]wechat.DataCubeEndpoint, len(c.Endpoints))
		copy(result, c.Endpoints)
		return result
	}
	return wechat.AllDataCubeEndpoints()
}

func (c *Collector) activeEndpoints() []wechat.DataCubeEndpoint {
	result := make([]wechat.DataCubeEndpoint, 0)
	for _, endpoint := range c.configuredEndpoints() {
		if endpoint.Lifecycle != "retired" {
			result = append(result, endpoint)
		}
	}
	return result
}

func (c *Collector) retiredEndpoints() []wechat.DataCubeEndpoint {
	result := make([]wechat.DataCubeEndpoint, 0)
	for _, endpoint := range c.configuredEndpoints() {
		if endpoint.Lifecycle == "retired" {
			result = append(result, endpoint)
		}
	}
	return result
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func startOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func dateBefore(value time.Time, earliest string) bool {
	return value.Format("2006-01-02") < earliest
}

func recentPassComplete(endpoints []wechat.DataCubeEndpoint, result *RunResult, failed map[string]bool, yesterday time.Time) bool {
	eligible := 0
	for _, endpoint := range endpoints {
		if !dateBefore(yesterday, endpoint.EarliestDate) {
			eligible++
		}
	}
	return result.Calls >= eligible && len(failed) == 0
}

func isQuotaError(err error) bool {
	var quotaError *wechat.QuotaLimitError
	if errors.As(err, &quotaError) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "quota")
}

func LogRunResult(prefix string, result *RunResult, err error) {
	if result == nil {
		log.Printf("[%s] Failed before run: %v", prefix, err)
		return
	}
	log.Printf("[%s] calls=%d succeeded=%d failed=%d recent_complete=%v quota_exhausted=%v",
		prefix, result.Calls, result.Succeeded, result.Failed, result.RecentComplete, result.QuotaExhausted)
	if err != nil {
		log.Printf("[%s] Completed with errors: %v", prefix, err)
	}
}
