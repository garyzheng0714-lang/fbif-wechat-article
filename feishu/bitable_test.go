package feishu

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFieldListIsReadOnceUntilAWriteFails(t *testing.T) {
	config.Env.FeishuAppID, config.Env.FeishuAppSecret, config.Env.FeishuBitableAppToken = "app", "secret", "base"
	originalTransport := httpClient.Transport
	t.Cleanup(func() {
		httpClient.Transport = originalTransport
		cachedToken = nil
		config.Env.FeishuAppID, config.Env.FeishuAppSecret, config.Env.FeishuBitableAppToken = "", "", ""
	})
	var fieldReads atomic.Int32
	httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"code":0,"data":{}}`
		switch {
		case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
			body = `{"code":0,"tenant_access_token":"t","expire":7200}`
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/fields"):
			fieldReads.Add(1)
			body = `{"code":0,"data":{"items":[{"field_name":"唯一键"}],"has_more":false}}`
		case strings.HasSuffix(r.URL.Path, "/records/batch_update"):
			body = `{"code":1254045,"msg":"FieldNameNotFound"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})

	required := []FieldSpec{{Name: "唯一键", Type: FieldTypeText}}
	for range 3 {
		if err := EnsureFieldsExist(required, "tbl1"); err != nil {
			t.Fatal(err)
		}
	}
	if got := fieldReads.Load(); got != 1 {
		t.Fatalf("field list read %d times across 3 runs, want 1", got)
	}

	if err := BatchUpdateByRecordID("tbl1", []map[string]interface{}{{"record_id": "rec1", "fields": map[string]interface{}{"唯一键": "x"}}}); err == nil {
		t.Fatal("fake Base rejects the write")
	}
	if err := EnsureFieldsExist(required, "tbl1"); err != nil {
		t.Fatal(err)
	}
	if got := fieldReads.Load(); got != 2 {
		t.Fatalf("a failed write must force one fresh field read, got %d reads", got)
	}
}
