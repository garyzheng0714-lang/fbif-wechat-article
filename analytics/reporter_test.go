package analytics

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFeishuWebhookReporterSendsSignedText(t *testing.T) {
	var payload map[string]interface{}
	reporter := &FeishuWebhookReporter{
		WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test",
		Secret:     "secret",
		Now:        func() time.Time { return time.Unix(1599360473, 0) },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(request.Body)
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success"}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	if err := reporter.Send(context.Background(), "测试日报"); err != nil {
		t.Fatal(err)
	}
	if payload["timestamp"] != "1599360473" || payload["sign"] == "" || payload["msg_type"] != "text" {
		t.Fatalf("payload=%v", payload)
	}
	content, _ := payload["content"].(map[string]interface{})
	if content["text"] != "测试日报" {
		t.Fatalf("content=%v", content)
	}
}

func TestFeishuAppReporterSendsTextToConfiguredChat(t *testing.T) {
	var messagePayload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			var tokenPayload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&tokenPayload); err != nil {
				t.Fatal(err)
			}
			if tokenPayload["app_id"] != "cli_test" || tokenPayload["app_secret"] != "app-secret" {
				t.Fatalf("token payload=%v", tokenPayload)
			}
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"tenant-token"}`)
		case "/messages":
			if r.URL.Query().Get("receive_id_type") != "chat_id" || r.Header.Get("Authorization") != "Bearer tenant-token" {
				t.Fatalf("message request query=%q authorization=%q", r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			if err := json.NewDecoder(r.Body).Decode(&messagePayload); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"code":0,"msg":"ok"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	reporter := &FeishuAppReporter{
		AppID: "cli_test", AppSecret: "app-secret", ChatID: "oc_review",
		TokenURL: server.URL + "/token", MessageURL: server.URL + "/messages", HTTPClient: server.Client(),
	}
	if err := reporter.Send(context.Background(), "测试日报"); err != nil {
		t.Fatal(err)
	}
	if messagePayload["receive_id"] != "oc_review" || messagePayload["msg_type"] != "text" {
		t.Fatalf("message payload=%v", messagePayload)
	}
	var content map[string]string
	if err := json.Unmarshal([]byte(messagePayload["content"]), &content); err != nil {
		t.Fatal(err)
	}
	if content["text"] != "测试日报" {
		t.Fatalf("message content=%v", content)
	}
}

func TestFeishuReporterFromEnvFallsBackToAppChat(t *testing.T) {
	t.Setenv("OFFICIAL_FEISHU_WEBHOOK_URL", "")
	t.Setenv("FEISHU_APP_ID", "cli_test")
	t.Setenv("FEISHU_APP_SECRET", "app-secret")
	t.Setenv("OFFICIAL_FEISHU_CHAT_ID", "oc_review")
	if _, ok := NewFeishuReporterFromEnv().(*FeishuAppReporter); !ok {
		t.Fatal("expected Feishu app reporter fallback")
	}
}

func TestDailyReportStatesExactQuotaAndCoverageSemantics(t *testing.T) {
	endpoint, _ := wechat.DataCubeEndpointByName("getarticleread")
	report := buildDailyReport(
		time.Date(2026, 7, 17, 9, 0, 0, 0, wechat.ShanghaiLoc()),
		&CombinedRunResult{Analytics: &RunResult{Calls: 2, Succeeded: 1, Deferred: 1}},
		nil,
		&Status{States: []archive.EndpointState{{
			Endpoint:          endpoint.Name,
			LastSuccessBegin:  "2026-07-15",
			LastSuccessEnd:    "2026-07-15",
			DeferredPending:   true,
			LastDeferredBegin: "2026-07-16",
			LastDeferredEnd:   "2026-07-16",
		}}},
		&archive.HistoricalCoverageReport{Status: "collecting", Verified: false, CompletedRequiredEndpointCount: 2, RequiredEndpointCount: 15},
		[]wechat.DailyQuotaStatus{{Endpoint: endpoint.Name, Limit: 1000, Reserve: 200, Used: 3, UsableRemaining: 797}},
		"预计完成日期：2026-07-18（测试估计）。",
	)

	for _, expected := range []string{
		"不代表历史文章全量已核验",
		"getarticleread：最近 2026-07-15..2026-07-15",
		"deferred=2026-07-16..2026-07-16",
		"status=collecting，verified=false，必需接口 2/15",
		"getarticleread：3/800，余 797，reserve 200",
		"预计完成日期：2026-07-18（测试估计）",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report missing %q:\n%s", expected, report)
		}
	}
}

func TestEstimateHistoricalCompletionUsesWindowAndEndpointBudgets(t *testing.T) {
	states := make([]archive.EndpointState, 0)
	for _, endpoint := range wechat.ActiveDataCubeEndpoints() {
		state := archive.EndpointState{Endpoint: endpoint.Name, BackfillDirection: "newest_to_oldest", BackfillComplete: true}
		if endpoint.Name == "getarticleread" {
			state.BackfillComplete = false
			state.NextBackfillDate = "2026-07-14"
		}
		states = append(states, state)
	}
	estimate := estimateHistoricalCompletion(
		time.Date(2026, 7, 17, 9, 0, 0, 0, wechat.ShanghaiLoc()),
		&Status{States: states},
		[]wechat.DailyQuotaStatus{{Endpoint: "getarticleread", Limit: 1000, Reserve: 200}},
		2000,
		"2026-07-12",
	)
	if !strings.Contains(estimate, "2026-07-18") || !strings.Contains(estimate, "约剩 3 次") {
		t.Fatalf("estimate=%q", estimate)
	}
}

func TestEstimateHistoricalCompletionFailsClosedOnDeferredWindow(t *testing.T) {
	estimate := estimateHistoricalCompletion(
		time.Date(2026, 7, 17, 9, 0, 0, 0, wechat.ShanghaiLoc()),
		&Status{States: []archive.EndpointState{{Endpoint: "getarticleread", DeferredPending: true}}},
		nil,
		2000,
		"2026-07-01",
	)
	if !strings.Contains(estimate, "暂不可估算") || !strings.Contains(estimate, "getarticleread deferred") {
		t.Fatalf("estimate=%q", estimate)
	}
}

func TestFeishuWebhookReporterDoesNotFollowRedirects(t *testing.T) {
	targetCalls := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	reporter := &FeishuWebhookReporter{WebhookURL: redirect.URL, Secret: "secret", HTTPClient: redirect.Client()}
	if err := reporter.Send(context.Background(), "test"); err == nil {
		t.Fatal("redirect must remain an explicit webhook error")
	}
	if targetCalls != 0 {
		t.Fatalf("redirect target calls=%d want 0", targetCalls)
	}
}

func TestAllEndpointQuotaStatusesUseOfficialDisplayNames(t *testing.T) {
	statuses := allEndpointQuotaStatuses()
	for _, status := range statuses {
		if strings.HasPrefix(status.Endpoint, "datacube_") {
			t.Fatalf("daily report leaked internal quota key: %+v", status)
		}
	}
}
