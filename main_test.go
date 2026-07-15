package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
