package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/autolayout"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

func TestMonitoringStatusChecksSchedulerDeferredAndOutboxWithoutClaimingCoverage(t *testing.T) {
	ctx := context.Background()
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	endpoint, _ := wechat.DataCubeEndpointByName("getusersummary")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, wechat.ShanghaiLoc())
	if err := store.MarkSuccess(ctx, endpoint.Name, endpoint.Category, "2026-07-16", "2026-07-16", "", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkContentRecentSuccess(ctx, "freepublish", now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if initialized, _, err := store.InitializeLayoutOutbox(ctx, nil, now.Add(-time.Hour)); err != nil || !initialized {
		t.Fatalf("initialize layout: initialized=%v err=%v", initialized, err)
	}
	runtime := &Runtime{
		Store: store,
		Analytics: &Collector{
			Store: store, Endpoints: []wechat.DataCubeEndpoint{endpoint},
		},
		Layout:   &autolayout.Dispatcher{Store: store},
		Reporter: &FeishuWebhookReporter{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"},
	}
	status, err := runtime.MonitoringStatus(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready {
		t.Fatalf("healthy monitoring must not claim historical coverage: %+v", status)
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("historicalCoverage")) {
		t.Fatalf("high-frequency monitoring must not run or expose historical coverage: %s", raw)
	}

	if err := store.MarkDeferred(ctx, endpoint.Name, endpoint.Category, "2026-07-16", "2026-07-16", now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	status, err = runtime.MonitoringStatus(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready || status.OldestDeferredAgeSeconds < 3*60*60 || len(status.DeferredEndpoints) != 1 {
		t.Fatalf("stale deferred window was not detected: %+v", status)
	}
}

func TestMonitoringStatusFlagsStaleOfficialAndFreepublishSchedules(t *testing.T) {
	ctx := context.Background()
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	endpoint, _ := wechat.DataCubeEndpointByName("getusersummary")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, wechat.ShanghaiLoc())
	if err := store.MarkSuccess(ctx, endpoint.Name, endpoint.Category, "2026-07-15", "2026-07-15", "", now.Add(-27*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkContentRecentSuccess(ctx, "freepublish", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, _, _ = store.InitializeLayoutOutbox(ctx, nil, now)
	runtime := &Runtime{
		Store: store,
		Analytics: &Collector{
			Store: store, Endpoints: []wechat.DataCubeEndpoint{endpoint},
		},
		Layout:   &autolayout.Dispatcher{Store: store},
		Reporter: &FeishuWebhookReporter{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"},
	}
	status, err := runtime.MonitoringStatus(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready || len(status.StaleEndpoints) != 1 || len(status.StaleContentStreams) != 1 {
		t.Fatalf("stopped schedules were not detected: %+v", status)
	}
}

func TestMonitoringStatusTreatsFreshQuotaLimitAsDiagnostic(t *testing.T) {
	ctx := context.Background()
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	endpoint, _ := wechat.DataCubeEndpointByName("getinterfacesummaryhour")
	now := time.Date(2026, 7, 17, 15, 0, 0, 0, wechat.ShanghaiLoc())
	if err := store.MarkSuccess(ctx, endpoint.Name, endpoint.Category, "2026-07-16", "2026-07-16", "", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFailure(ctx, endpoint.Name, endpoint.Category, "WeChat API daily quota limit reached for datacube_getinterfacesummaryhour (daily-limit-reached)", now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkContentRecentSuccess(ctx, "freepublish", now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if initialized, _, err := store.InitializeLayoutOutbox(ctx, nil, now.Add(-time.Hour)); err != nil || !initialized {
		t.Fatalf("initialize layout: initialized=%v err=%v", initialized, err)
	}
	runtime := &Runtime{
		Store: store,
		Analytics: &Collector{
			Store: store, Endpoints: []wechat.DataCubeEndpoint{endpoint},
		},
		Layout:   &autolayout.Dispatcher{Store: store},
		Reporter: &FeishuWebhookReporter{WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/test"},
	}
	status, err := runtime.MonitoringStatus(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || len(status.FailedEndpoints) != 0 {
		t.Fatalf("fresh planned quota limit must not trigger P0: %+v", status)
	}
	if len(status.QuotaLimitedEndpoints) != 1 || status.QuotaLimitedEndpoints[0] != endpoint.Name {
		t.Fatalf("quota-limited endpoint must remain visible as diagnostic: %+v", status)
	}

	if err := store.MarkFailure(ctx, endpoint.Name, endpoint.Category, "upstream request failed", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	status, err = runtime.MonitoringStatus(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready || len(status.FailedEndpoints) != 1 || status.FailedEndpoints[0] != endpoint.Name {
		t.Fatalf("unexpected endpoint failure must still trigger P0: %+v", status)
	}
	if len(status.QuotaLimitedEndpoints) != 0 {
		t.Fatalf("unexpected endpoint failure must not be classified as a quota diagnostic: %+v", status)
	}
}
