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
}

func (f *fakeClient) Call(_ context.Context, endpoint wechat.DataCubeEndpoint, beginDate, endDate string) (*wechat.RawAPIResponse, error) {
	f.calls = append(f.calls, wechat.DateWindow{Begin: beginDate, End: endDate})
	f.names = append(f.names, endpoint.Name)
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
	if client.calls[1] != (wechat.DateWindow{Begin: "2014-12-01", End: "2014-12-07"}) {
		t.Fatalf("backfill window = %+v", client.calls[1])
	}
	state, err := store.GetState(context.Background(), endpoint.Name)
	if err != nil {
		t.Fatal(err)
	}
	if state.NextBackfillDate != "2014-12-08" {
		t.Fatalf("state = %+v", state)
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
