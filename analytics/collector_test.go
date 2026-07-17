package analytics

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

type fakeClient struct {
	calls []wechat.DateWindow
	names []string
	fail  map[string]error
	delay map[string]bool
}

func (f *fakeClient) Call(_ context.Context, endpoint wechat.DataCubeEndpoint, beginDate, endDate string) (*wechat.RawAPIResponse, error) {
	f.calls = append(f.calls, wechat.DateWindow{Begin: beginDate, End: endDate})
	f.names = append(f.names, endpoint.Name)
	if f.delay[endpoint.Name] {
		return &wechat.RawAPIResponse{
			Endpoint:    endpoint.Name,
			RequestBody: []byte(fmt.Sprintf(`{"begin_date":%q,"end_date":%q}`, beginDate, endDate)),
			Body:        []byte(`{"list":[],"is_delay":"true"}`),
			HTTPStatus:  200,
		}, nil
	}
	if err := f.fail[endpoint.Name]; err != nil {
		return &wechat.RawAPIResponse{
			Endpoint:    endpoint.Name,
			RequestBody: []byte(fmt.Sprintf(`{"begin_date":%q,"end_date":%q}`, beginDate, endDate)),
			Body:        []byte(`{"errcode":48001,"errmsg":"api unauthorized"}`),
			HTTPStatus:  200,
			ErrCode:     48001,
			ErrMsg:      "api unauthorized",
		}, err
	}
	return &wechat.RawAPIResponse{
		Endpoint:    endpoint.Name,
		RequestBody: []byte(fmt.Sprintf(`{"begin_date":%q,"end_date":%q}`, beginDate, endDate)),
		Body:        []byte(fmt.Sprintf(`{"list":[{"ref_date":%q}]}`, beginDate)),
		HTTPStatus:  200,
	}, nil
}

func TestRunFetchesYesterdayForAll15ActiveEndpointsBeforeBackfill(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := &fakeClient{}
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, wechat.ShanghaiLoc())
	collector := &Collector{
		Client:   client,
		Store:    store,
		Now:      func() time.Time { return now },
		MaxCalls: 15,
	}

	result, err := collector.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Calls != 15 || !result.RecentComplete {
		t.Fatalf("result = %+v", result)
	}
	for index, window := range client.calls {
		if window.Begin != "2026-07-13" || window.End != "2026-07-13" {
			t.Fatalf("call %d was not yesterday: %+v", index, window)
		}
	}
	status, err := collector.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.HealthyEndpoints != 15 || status.DocumentedEndpoints != 21 || len(status.RetiredEndpoints) != 6 {
		t.Fatalf("status = %+v", status)
	}
}

func TestRunAdvancesBackfillOnlyAfterDurableSuccess(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := &fakeClient{}
	endpoint, _ := wechat.DataCubeEndpointByName("getusersummary")
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, wechat.ShanghaiLoc())
	collector := &Collector{
		Client:    client,
		Store:     store,
		Now:       func() time.Time { return now },
		MaxCalls:  2,
		Endpoints: []wechat.DataCubeEndpoint{endpoint},
	}

	result, err := collector.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Calls != 2 {
		t.Fatalf("calls = %d", result.Calls)
	}
	if client.calls[1] != (wechat.DateWindow{Begin: "2026-07-06", End: "2026-07-12"}) {
		t.Fatalf("backfill window = %+v", client.calls[1])
	}
	state, err := store.GetState(context.Background(), endpoint.Name)
	if err != nil {
		t.Fatal(err)
	}
	if state.NextBackfillDate != "2026-07-05" || state.BackfillDirection != "newest_to_oldest" || state.BackfillComplete {
		t.Fatalf("state = %+v", state)
	}
	if _, err := collector.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.calls[3] != (wechat.DateWindow{Begin: "2026-06-29", End: "2026-07-05"}) {
		t.Fatalf("second backfill window = %+v", client.calls[3])
	}
}

func TestRunArchivesPermissionFailureAndReportsNotReady(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	endpoint, _ := wechat.DataCubeEndpointByName("getarticleread")
	client := &fakeClient{fail: map[string]error{endpoint.Name: errors.New("48001 api unauthorized")}}
	collector := &Collector{
		Client:    client,
		Store:     store,
		Now:       func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, wechat.ShanghaiLoc()) },
		MaxCalls:  1,
		Endpoints: []wechat.DataCubeEndpoint{endpoint},
	}

	result, err := collector.Run(context.Background())
	if err == nil || result.Failed != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	failedFetches, queryErr := store.QueryInt64(context.Background(), `
		SELECT COUNT(*) FROM official_api_fetches
		WHERE endpoint = 'getarticleread' AND success = 0 AND wechat_errcode = 48001`)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if failedFetches != 1 {
		t.Fatalf("failed fetches = %d", failedFetches)
	}
	status, statusErr := collector.Status(context.Background())
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status.Ready || status.FailedEndpoints[endpoint.Name] == "" {
		t.Fatalf("status = %+v", status)
	}
}

func TestRunContinuesOtherEndpointsWhenOneQuotaIsExhausted(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	limited, _ := wechat.DataCubeEndpointByName("getarticleread")
	healthy, _ := wechat.DataCubeEndpointByName("getarticleshare")
	client := &fakeClient{fail: map[string]error{
		limited.Name: &wechat.QuotaLimitError{Endpoint: limited.Name},
	}}
	collector := &Collector{
		Client:    client,
		Store:     store,
		Now:       func() time.Time { return time.Date(2026, 7, 17, 9, 0, 0, 0, wechat.ShanghaiLoc()) },
		MaxCalls:  2,
		Endpoints: []wechat.DataCubeEndpoint{limited, healthy},
	}

	result, err := collector.Run(context.Background())
	if err == nil {
		t.Fatal("quota exhaustion should remain visible as a run error")
	}
	if result.Calls != 2 || result.Succeeded != 1 || result.Failed != 1 {
		t.Fatalf("result=%+v", result)
	}
	if fmt.Sprint(result.QuotaExhaustedEndpoints) != fmt.Sprint([]string{limited.Name}) {
		t.Fatalf("quota endpoints=%v", result.QuotaExhaustedEndpoints)
	}
	state, err := store.GetState(context.Background(), healthy.Name)
	if err != nil || state == nil || state.LastSuccessAt == 0 {
		t.Fatalf("healthy endpoint was blocked by another quota: state=%+v err=%v calls=%v", state, err, client.names)
	}
}

func TestRunPersistsDelayedResponseWithoutAdvancingAndContinuesOtherEndpoints(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	delayed, _ := wechat.DataCubeEndpointByName("getarticleread")
	healthy, _ := wechat.DataCubeEndpointByName("getarticleshare")
	client := &fakeClient{delay: map[string]bool{delayed.Name: true}}
	collector := &Collector{
		Client:    client,
		Store:     store,
		Now:       func() time.Time { return time.Date(2026, 7, 17, 9, 0, 0, 0, wechat.ShanghaiLoc()) },
		MaxCalls:  2,
		Endpoints: []wechat.DataCubeEndpoint{delayed, healthy},
	}

	result, err := collector.Run(context.Background())
	if err != nil {
		t.Fatalf("is_delay is an expected deferred state, not a run failure: %v", err)
	}
	if result.Calls != 2 || result.Succeeded != 1 || result.Deferred != 1 || result.Failed != 0 || result.RecentComplete {
		t.Fatalf("result=%+v", result)
	}
	delayedState, err := store.GetState(context.Background(), delayed.Name)
	if err != nil || delayedState == nil || !delayedState.DeferredPending || delayedState.LastSuccessAt != 0 {
		t.Fatalf("delayed state=%+v err=%v", delayedState, err)
	}
	healthyState, err := store.GetState(context.Background(), healthy.Name)
	if err != nil || healthyState == nil || healthyState.LastSuccessAt == 0 {
		t.Fatalf("healthy state=%+v err=%v", healthyState, err)
	}
	deferredFetches, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_fetches WHERE endpoint = ? AND deferred = 1`, delayed.Name)
	if err != nil || deferredFetches != 1 {
		t.Fatalf("deferred fetches=%d err=%v", deferredFetches, err)
	}
}

func TestRetryDeferredUsesExactWindowAndClearsMatchingPendingState(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	endpoint, _ := wechat.DataCubeEndpointByName("getarticleread")
	client := &fakeClient{delay: map[string]bool{endpoint.Name: true}}
	now := time.Date(2026, 7, 17, 9, 0, 0, 0, wechat.ShanghaiLoc())
	collector := &Collector{
		Client:    client,
		Store:     store,
		Now:       func() time.Time { return now },
		MaxCalls:  1,
		Endpoints: []wechat.DataCubeEndpoint{endpoint},
	}

	result, err := collector.Run(context.Background())
	if err != nil || result.Deferred != 1 {
		t.Fatalf("initial result=%+v err=%v", result, err)
	}
	client.delay[endpoint.Name] = false
	now = now.Add(30 * time.Minute)
	retry, err := collector.RetryDeferred(context.Background())
	if err != nil || retry.Calls != 1 || retry.Succeeded != 1 || retry.Deferred != 0 {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	if got := client.calls[len(client.calls)-1]; got != (wechat.DateWindow{Begin: "2026-07-16", End: "2026-07-16"}) {
		t.Fatalf("retried window=%+v", got)
	}
	state, err := store.GetState(context.Background(), endpoint.Name)
	if err != nil || state == nil || state.DeferredPending || state.LastSuccessBegin != "2026-07-16" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}
