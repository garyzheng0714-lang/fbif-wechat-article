package wechat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAllDataCubeEndpointsAreCompleteAndUnique(t *testing.T) {
	endpoints := AllDataCubeEndpoints()
	if len(endpoints) != 21 {
		t.Fatalf("endpoint count = %d, want 21", len(endpoints))
	}
	seen := make(map[string]bool)
	for _, endpoint := range endpoints {
		if seen[endpoint.Name] {
			t.Fatalf("duplicate endpoint %q", endpoint.Name)
		}
		seen[endpoint.Name] = true
		if endpoint.EarliestDate == "" || endpoint.MaxSpanDays < 1 || endpoint.Documentation == "" {
			t.Fatalf("incomplete endpoint metadata: %+v", endpoint)
		}
	}
	if len(ActiveDataCubeEndpoints()) != 15 || len(RetiredDataCubeEndpoints()) != 6 {
		t.Fatalf("active=%d retired=%d", len(ActiveDataCubeEndpoints()), len(RetiredDataCubeEndpoints()))
	}
}

func TestSplitDataCubeRangeUsesInclusiveMaxSpan(t *testing.T) {
	endpoint, _ := DataCubeEndpointByName("getusersummary")
	windows, err := SplitDataCubeRange(endpoint, "2026-01-01", "2026-01-16")
	if err != nil {
		t.Fatal(err)
	}
	want := []DateWindow{
		{Begin: "2026-01-01", End: "2026-01-07"},
		{Begin: "2026-01-08", End: "2026-01-14"},
		{Begin: "2026-01-15", End: "2026-01-16"},
	}
	if len(windows) != len(want) {
		t.Fatalf("windows = %+v, want %+v", windows, want)
	}
	for i := range want {
		if windows[i] != want[i] {
			t.Fatalf("window %d = %+v, want %+v", i, windows[i], want[i])
		}
	}
}

func TestDataCubeClientPreservesRawResponse(t *testing.T) {
	raw := `{"list":[{"ref_date":"2026-07-13","msgid":"123_1","new_field":{"nested":7}}],"is_delay":"false"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getarticleread" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"begin_date":"2026-07-13"`) {
			t.Fatalf("request body = %s", body)
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()

	client := &DataCubeClient{
		BaseURL:      server.URL,
		HTTPClient:   server.Client(),
		GetToken:     func() (string, error) { return "token", nil },
		RefreshToken: func() (string, error) { return "new-token", nil },
	}
	endpoint, _ := DataCubeEndpointByName("getarticleread")
	response, err := client.Call(context.Background(), endpoint, "2026-07-13", "2026-07-13")
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Body) != raw {
		t.Fatalf("raw body changed: %q", response.Body)
	}
}

func TestDataCubeClientRefreshesExpiredTokenOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("access_token") == "expired" {
			_, _ = w.Write([]byte(`{"errcode":40001,"errmsg":"invalid credential"}`))
			return
		}
		_, _ = w.Write([]byte(`{"list":[]}`))
	}))
	defer server.Close()

	client := &DataCubeClient{
		BaseURL:      server.URL,
		HTTPClient:   server.Client(),
		GetToken:     func() (string, error) { return "expired", nil },
		RefreshToken: func() (string, error) { return "fresh", nil },
	}
	endpoint, _ := DataCubeEndpointByName("getarticlesummary")
	if _, err := client.Call(context.Background(), endpoint, "2026-07-13", "2026-07-13"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}
