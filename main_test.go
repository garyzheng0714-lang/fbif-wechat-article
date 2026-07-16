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
