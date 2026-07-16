package analytics

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

const maxReporterMessageRunes = 15000

const (
	feishuTenantTokenURL = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
	feishuMessageURL     = "https://open.feishu.cn/open-apis/im/v1/messages"
)

type Reporter interface {
	Configured() bool
	Send(context.Context, string) error
}

type FeishuWebhookReporter struct {
	WebhookURL string
	Secret     string
	HTTPClient *http.Client
	Now        func() time.Time
}

func NewFeishuReporterFromEnv() Reporter {
	webhookURL := strings.TrimSpace(os.Getenv("OFFICIAL_FEISHU_WEBHOOK_URL"))
	if webhookURL != "" && !strings.HasPrefix(webhookURL, "https://") {
		log.Printf("[OfficialReporter] OFFICIAL_FEISHU_WEBHOOK_URL must use https")
		webhookURL = ""
	}
	if webhookURL != "" {
		return &FeishuWebhookReporter{
			WebhookURL: webhookURL,
			Secret:     strings.TrimSpace(os.Getenv("OFFICIAL_FEISHU_WEBHOOK_SECRET")),
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
		}
	}
	appReporter := &FeishuAppReporter{
		AppID:      strings.TrimSpace(os.Getenv("FEISHU_APP_ID")),
		AppSecret:  strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET")),
		ChatID:     strings.TrimSpace(os.Getenv("OFFICIAL_FEISHU_CHAT_ID")),
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
	if appReporter.Configured() {
		return appReporter
	}
	return nil
}

// NewFeishuWebhookReporterFromEnv 保留旧调用名；实际会在 webhook 缺失时
// 使用飞书应用向 OFFICIAL_FEISHU_CHAT_ID 发消息。
func NewFeishuWebhookReporterFromEnv() Reporter {
	return NewFeishuReporterFromEnv()
}

func (r *FeishuWebhookReporter) Configured() bool {
	return r != nil && strings.TrimSpace(r.WebhookURL) != ""
}

func (r *FeishuWebhookReporter) Send(ctx context.Context, message string) error {
	if !r.Configured() {
		return fmt.Errorf("official Feishu reporter is not configured")
	}
	message = truncateRunes(strings.TrimSpace(message), maxReporterMessageRunes)
	payload := map[string]interface{}{
		"msg_type": "text",
		"content":  map[string]string{"text": message},
	}
	if r.Secret != "" {
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		timestamp := now.Unix()
		payload["timestamp"] = fmt.Sprintf("%d", timestamp)
		payload["sign"] = feishuWebhookSign(timestamp, r.Secret)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := safeClient.Do(req)
	if err != nil {
		return fmt.Errorf("send official Feishu report: %w", err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("official Feishu report HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if len(responseBody) > 0 && json.Unmarshal(responseBody, &result) == nil && result.Code != 0 {
		return fmt.Errorf("official Feishu report code %d: %s", result.Code, result.Msg)
	}
	return nil
}

type FeishuAppReporter struct {
	AppID      string
	AppSecret  string
	ChatID     string
	TokenURL   string
	MessageURL string
	HTTPClient *http.Client
}

func (r *FeishuAppReporter) Configured() bool {
	return r != nil && strings.TrimSpace(r.AppID) != "" && strings.TrimSpace(r.AppSecret) != "" && strings.TrimSpace(r.ChatID) != ""
}

func (r *FeishuAppReporter) Send(ctx context.Context, message string) error {
	if !r.Configured() {
		return fmt.Errorf("official Feishu app reporter is not configured")
	}
	tokenURL := strings.TrimSpace(r.TokenURL)
	if tokenURL == "" {
		tokenURL = feishuTenantTokenURL
	}
	tokenPayload, err := json.Marshal(map[string]string{"app_id": r.AppID, "app_secret": r.AppSecret})
	if err != nil {
		return err
	}
	tokenRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(tokenPayload))
	if err != nil {
		return err
	}
	tokenRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	tokenResponse, err := r.safeClient().Do(tokenRequest)
	if err != nil {
		return fmt.Errorf("fetch official Feishu app token: %w", err)
	}
	tokenBody, tokenReadErr := io.ReadAll(io.LimitReader(tokenResponse.Body, 8192))
	_ = tokenResponse.Body.Close()
	if tokenReadErr != nil {
		return tokenReadErr
	}
	if tokenResponse.StatusCode < 200 || tokenResponse.StatusCode >= 300 {
		return fmt.Errorf("official Feishu app token HTTP %d", tokenResponse.StatusCode)
	}
	var tokenResult struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
		return fmt.Errorf("decode official Feishu app token: %w", err)
	}
	if tokenResult.Code != 0 || strings.TrimSpace(tokenResult.TenantAccessToken) == "" {
		return fmt.Errorf("official Feishu app token code %d: %s", tokenResult.Code, tokenResult.Msg)
	}

	content, err := json.Marshal(map[string]string{"text": truncateRunes(strings.TrimSpace(message), maxReporterMessageRunes)})
	if err != nil {
		return err
	}
	messageURL := strings.TrimSpace(r.MessageURL)
	if messageURL == "" {
		messageURL = feishuMessageURL
	}
	parsedMessageURL, err := url.Parse(messageURL)
	if err != nil {
		return fmt.Errorf("parse official Feishu message URL: %w", err)
	}
	query := parsedMessageURL.Query()
	query.Set("receive_id_type", "chat_id")
	parsedMessageURL.RawQuery = query.Encode()
	messagePayload, err := json.Marshal(map[string]string{
		"receive_id": r.ChatID,
		"msg_type":   "text",
		"content":    string(content),
	})
	if err != nil {
		return err
	}
	messageRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedMessageURL.String(), bytes.NewReader(messagePayload))
	if err != nil {
		return err
	}
	messageRequest.Header.Set("Authorization", "Bearer "+tokenResult.TenantAccessToken)
	messageRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	messageResponse, err := r.safeClient().Do(messageRequest)
	if err != nil {
		return fmt.Errorf("send official Feishu app report: %w", err)
	}
	messageBody, messageReadErr := io.ReadAll(io.LimitReader(messageResponse.Body, 8192))
	_ = messageResponse.Body.Close()
	if messageReadErr != nil {
		return messageReadErr
	}
	if messageResponse.StatusCode < 200 || messageResponse.StatusCode >= 300 {
		return fmt.Errorf("official Feishu app report HTTP %d", messageResponse.StatusCode)
	}
	var messageResult struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(messageBody, &messageResult); err != nil {
		return fmt.Errorf("decode official Feishu app report: %w", err)
	}
	if messageResult.Code != 0 {
		return fmt.Errorf("official Feishu app report code %d: %s", messageResult.Code, messageResult.Msg)
	}
	return nil
}

func (r *FeishuAppReporter) safeClient() *http.Client {
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &safeClient
}

func feishuWebhookSign(timestamp int64, secret string) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	digest := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(digest.Sum(nil))
}

func (r *Runtime) reportDaily(ctx context.Context, result *CombinedRunResult, runErr error) {
	if r.Reporter == nil || !r.Reporter.Configured() {
		log.Printf("[OfficialReporter] daily report skipped: Feishu reporter is not configured")
		return
	}
	status, err := r.Analytics.Status(ctx)
	if err != nil {
		log.Printf("[OfficialReporter] build daily report status: %v", err)
		return
	}
	coverage, coverageErr := r.HistoricalCoverage(ctx)
	if coverageErr != nil {
		log.Printf("[OfficialReporter] build daily coverage: %v", coverageErr)
	}
	quotas := allEndpointQuotaStatuses()
	estimate := estimateHistoricalCompletion(time.Now(), status, quotas, r.Analytics.MaxCalls, r.Analytics.BackfillStart)
	message := buildDailyReport(time.Now(), result, runErr, status, coverage, quotas, estimate)
	if err := r.Reporter.Send(ctx, message); err != nil {
		log.Printf("[OfficialReporter] daily report failed: %v", err)
	}
}

func (r *Runtime) reportAlert(ctx context.Context, message string) {
	if r.Reporter == nil || !r.Reporter.Configured() {
		log.Printf("[OfficialReporter] alert unsent (Feishu reporter not configured): %s", message)
		return
	}
	if err := r.Reporter.Send(ctx, "【公众号官方数据告警】\n"+message); err != nil {
		log.Printf("[OfficialReporter] alert failed: %v", err)
	}
}

func allEndpointQuotaStatuses() []wechat.DailyQuotaStatus {
	counterKeys := make(map[string]string)
	for _, endpoint := range wechat.ActiveDataCubeEndpoints() {
		// DataCubeClient 的持久配额键带 datacube_ 前缀；日报展示仍使用
		// 官方 endpoint 名称，避免把真实 used 误报为 0。
		counterKeys[endpoint.Name] = "datacube_" + endpoint.Name
	}
	for _, endpoint := range wechat.AllContentEndpoints() {
		counterKeys[endpoint.Name] = endpoint.Name
	}
	ordered := make([]string, 0, len(counterKeys))
	for name := range counterKeys {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	statuses := make([]wechat.DailyQuotaStatus, 0, len(ordered))
	for _, name := range ordered {
		status := wechat.CurrentEndpointQuotaStatus(counterKeys[name])
		status.Endpoint = name
		statuses = append(statuses, status)
	}
	return statuses
}

func buildDailyReport(now time.Time, result *CombinedRunResult, runErr error, status *Status, coverage *archive.HistoricalCoverageReport, quotas []wechat.DailyQuotaStatus, completionEstimate string) string {
	localNow := now.In(wechat.ShanghaiLoc())
	lines := []string{
		fmt.Sprintf("【公众号官方数据日报 %s】", localNow.Format("2006-01-02")),
		"口径：接口调用量为当天持久化的独立 endpoint 尝试次数（含失败）；不代表历史文章全量已核验。",
	}
	if result != nil && result.Analytics != nil {
		a := result.Analytics
		lines = append(lines, fmt.Sprintf("本轮数据接口：调用 %d，成功 %d，失败 %d，延迟 %d；D-1 全接口完成=%t。", a.Calls, a.Succeeded, a.Failed, a.Deferred, a.RecentComplete))
	}
	if result != nil && result.Content != nil {
		c := result.Content
		lines = append(lines, fmt.Sprintf("本轮内容接口：调用 %d，成功 %d，失败 %d。", c.Calls, c.Succeeded, c.Failed))
	}
	if runErr != nil {
		lines = append(lines, "本轮错误："+truncateRunes(runErr.Error(), 500))
	}

	lines = append(lines, "", "窗口与缺口：")
	stateByEndpoint := make(map[string]archive.EndpointState)
	if status != nil {
		for _, state := range status.States {
			stateByEndpoint[state.Endpoint] = state
		}
	}
	for _, endpoint := range wechat.ActiveDataCubeEndpoints() {
		state, ok := stateByEndpoint[endpoint.Name]
		if !ok {
			lines = append(lines, "- "+endpoint.Name+"：尚无成功窗口")
			continue
		}
		backfill := "next=" + valueOrDash(state.NextBackfillDate)
		if state.BackfillComplete {
			backfill = "历史窗口完成"
		}
		detail := fmt.Sprintf("- %s：最近 %s..%s；%s", endpoint.Name, valueOrDash(state.LastSuccessBegin), valueOrDash(state.LastSuccessEnd), backfill)
		if state.DeferredPending {
			detail += fmt.Sprintf("；deferred=%s..%s", state.LastDeferredBegin, state.LastDeferredEnd)
		}
		if state.LastError != "" {
			detail += "；error=" + truncateRunes(state.LastError, 160)
		}
		lines = append(lines, detail)
	}
	if coverage != nil {
		lines = append(lines, fmt.Sprintf("历史覆盖审计：status=%s，verified=%t，必需接口 %d/%d。", coverage.Status, coverage.Verified, coverage.CompletedRequiredEndpointCount, coverage.RequiredEndpointCount))
	}
	if completionEstimate != "" {
		lines = append(lines, completionEstimate)
	}

	lines = append(lines, "", "当日 endpoint 配额（used / 可用上限，reserve）：")
	for _, quota := range quotas {
		usable := quota.Limit - quota.Reserve
		if usable < 0 {
			usable = 0
		}
		lines = append(lines, fmt.Sprintf("- %s：%d/%d，余 %d，reserve %d", quota.Endpoint, quota.Used, usable, quota.UsableRemaining, quota.Reserve))
	}
	return strings.Join(lines, "\n")
}

func estimateHistoricalCompletion(now time.Time, status *Status, quotas []wechat.DailyQuotaStatus, maxCalls int, backfillStart string) string {
	if status == nil {
		return "预计完成日期：暂不可估算（采集状态不可用）。"
	}
	stateByEndpoint := make(map[string]archive.EndpointState, len(status.States))
	for _, state := range status.States {
		stateByEndpoint[state.Endpoint] = state
	}
	quotaUsable := make(map[string]int, len(quotas))
	for _, quota := range quotas {
		usable := quota.Limit - quota.Reserve
		if usable > 0 {
			quotaUsable[quota.Endpoint] = usable
		}
	}
	if maxCalls <= 0 {
		maxCalls = 500
	}
	localNow := now.In(wechat.ShanghaiLoc())
	yesterday := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location()).AddDate(0, 0, -1)
	totalCalls := 0
	requiredDays := 0
	blockers := make([]string, 0)
	for _, endpoint := range wechat.ActiveDataCubeEndpoints() {
		state := stateByEndpoint[endpoint.Name]
		if state.LastError != "" {
			blockers = append(blockers, endpoint.Name+" error")
		}
		if state.DeferredPending {
			blockers = append(blockers, endpoint.Name+" deferred")
		}
		if state.BackfillComplete {
			continue
		}
		earliestDate := endpoint.EarliestDate
		if backfillStart != "" && backfillStart > earliestDate {
			earliestDate = backfillStart
		}
		earliest, err := time.ParseInLocation("2006-01-02", earliestDate, wechat.ShanghaiLoc())
		if err != nil {
			return "预计完成日期：暂不可估算（历史起始日期配置无效）。"
		}
		refreshDays := endpoint.RefreshDays
		if refreshDays < 1 {
			refreshDays = 1
		}
		end := yesterday.AddDate(0, 0, -refreshDays)
		if state.BackfillDirection == "newest_to_oldest" && state.NextBackfillDate != "" {
			end, err = time.ParseInLocation("2006-01-02", state.NextBackfillDate, wechat.ShanghaiLoc())
			if err != nil {
				return "预计完成日期：暂不可估算（历史游标日期无效）。"
			}
		}
		if end.Before(earliest) {
			continue
		}
		spanDays := endpoint.MaxSpanDays
		if spanDays < 1 {
			spanDays = 1
		}
		remainingDates := int(end.Sub(earliest).Hours()/24) + 1
		calls := (remainingDates + spanDays - 1) / spanDays
		totalCalls += calls
		usable := quotaUsable[endpoint.Name]
		if usable <= 0 {
			usable = 800
		}
		if days := (calls + usable - 1) / usable; days > requiredDays {
			requiredDays = days
		}
	}
	if len(blockers) > 0 {
		sort.Strings(blockers)
		return "预计完成日期：暂不可估算（" + strings.Join(blockers, "、") + "；解除后按剩余窗口重算）。"
	}
	if totalCalls == 0 {
		return "预计完成日期：历史窗口已完成；仍须完成覆盖审计并由用户确认后，才能称为已核验全量。"
	}
	if days := (totalCalls + maxCalls - 1) / maxCalls; days > requiredDays {
		requiredDays = days
	}
	if requiredDays < 1 {
		requiredDays = 1
	}
	completion := localNow.AddDate(0, 0, requiredDays).Format("2006-01-02")
	return fmt.Sprintf("预计完成日期：%s（保守估计；假设无新增 is_delay、权限或配额异常，当前约剩 %d 次历史窗口调用）。", completion, totalCalls)
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}
