package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/garyzheng0714-lang/fbif-wechat-article/analytics"
	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

func TestLegacyDirectFeishuSyncIsPermanentlyGone(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/feishu/sync", nil)
	syncHandler(recorder, request)
	if recorder.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "historicalCoverage.verified=true") {
		t.Fatalf("response must explain the verified coverage gate: %s", recorder.Body.String())
	}
}

func TestHealthHandlerUsesBoundedLightweightStoreProbe(t *testing.T) {
	previousRuntime := officialRuntime
	previousConfig := config.Env
	t.Cleanup(func() {
		officialRuntime = previousRuntime
		config.Env = previousConfig
	})

	store, err := archive.Open(filepath.Join(t.TempDir(), "official.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	config.Env.WechatAppID = "appid"
	config.Env.WechatSecret = "secret"
	officialRuntime = &analytics.Runtime{Store: store}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Status      string         `json:"status"`
		OfficialAPI map[string]any `json:"officialAPI"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.OfficialAPI["ready"] != true || response.OfficialAPI["storeAvailable"] != true {
		t.Fatalf("health response=%+v", response)
	}
	if _, exists := response.OfficialAPI["historicalCoverage"]; exists {
		t.Fatalf("liveness endpoint must not run or return historical coverage: %+v", response.OfficialAPI)
	}
}

func TestHealthHandlerFailsClosedWhenArchiveStoreMissing(t *testing.T) {
	previousRuntime := officialRuntime
	previousConfig := config.Env
	t.Cleanup(func() {
		officialRuntime = previousRuntime
		config.Env = previousConfig
	})
	config.Env.WechatAppID = "appid"
	config.Env.WechatSecret = "secret"
	officialRuntime = &analytics.Runtime{}

	recorder := httptest.NewRecorder()
	healthHandler(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "官方归档存储未配置") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestMonitoringAuthAcceptsOnlyAPIKeyOrDedicatedServiceToken(t *testing.T) {
	previousConfig := config.Env
	t.Cleanup(func() { config.Env = previousConfig })
	config.Env.APIKey = "operator-api-key"
	t.Setenv("PUBLISH_SYNC_SERVICE_TOKEN", "monitor-service-token")

	called := 0
	handler := requireMonitoringAuth(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})
	tests := []struct {
		name       string
		header     string
		value      string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "wrong", header: "X-Publish-Sync-Token", value: "wrong", wantStatus: http.StatusUnauthorized},
		{name: "api key", header: "X-API-Key", value: "operator-api-key", wantStatus: http.StatusNoContent},
		{name: "service token", header: "X-Publish-Sync-Token", value: "monitor-service-token", wantStatus: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/wechat/official/monitoring", nil)
			if test.header != "" {
				request.Header.Set(test.header, test.value)
			}
			recorder := httptest.NewRecorder()
			handler(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	if called != 2 {
		t.Fatalf("handler called %d times, want 2", called)
	}
}

func TestMonitoringAuthFailsClosedWhenNoCredentialIsConfigured(t *testing.T) {
	previousConfig := config.Env
	t.Cleanup(func() { config.Env = previousConfig })
	config.Env.APIKey = ""
	t.Setenv("PUBLISH_SYNC_SERVICE_TOKEN", "")

	called := false
	handler := requireMonitoringAuth(func(http.ResponseWriter, *http.Request) { called = true })
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/wechat/official/monitoring", nil))
	if recorder.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
}

func TestServiceTokenCannotReachBroadOfficialEndpoints(t *testing.T) {
	previousConfig := config.Env
	t.Cleanup(func() { config.Env = previousConfig })
	config.Env.APIKey = "operator-api-key"
	t.Setenv("PUBLISH_SYNC_SERVICE_TOKEN", "monitor-service-token")

	called := false
	handler := requireAPIKey(func(http.ResponseWriter, *http.Request) { called = true })
	request := httptest.NewRequest(http.MethodGet, "/api/wechat/official/status", nil)
	request.Header.Set("X-Publish-Sync-Token", "monitor-service-token")
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	if recorder.Code != http.StatusUnauthorized || called {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
}

func TestBroadOfficialEndpointsFailClosedWhenAPIKeyIsMissing(t *testing.T) {
	previousConfig := config.Env
	t.Cleanup(func() { config.Env = previousConfig })
	config.Env.APIKey = ""

	called := false
	handler := requireAPIKey(func(http.ResponseWriter, *http.Request) { called = true })
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/wechat/official/status", nil))
	if recorder.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("status=%d called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
}
