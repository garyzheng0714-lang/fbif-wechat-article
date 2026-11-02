package officialbase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
)

func TestSyncFailsClosedBeforeAnyBaseCallWithoutVerifiedCoverage(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	syncer := &Syncer{Store: store}
	if _, err := syncer.Sync(context.Background()); err == nil || !strings.Contains(err.Error(), "coverage gate") {
		t.Fatalf("missing gate must fail closed, got %v", err)
	}
	blocked := errors.New("coverage is collecting")
	syncer.BeforeSync = func(context.Context) error { return blocked }
	if _, err := syncer.Sync(context.Background()); !errors.Is(err, blocked) {
		t.Fatalf("coverage rejection must happen before Base access, got %v", err)
	}
}

func TestDatasetRegistryCoversEveryArchivedDomain(t *testing.T) {
	want := map[string]bool{
		archive.BaseDatasetArticles:           true,
		archive.BaseDatasetArticleDaily:       true,
		archive.BaseDatasetArticleCumulative:  true,
		archive.BaseDatasetAccountDaily:       true,
		archive.BaseDatasetFollowerSource:     true,
		archive.BaseDatasetFollowerCumulative: true,
		archive.BaseDatasetMessageMetrics:     true,
		archive.BaseDatasetInterfaceMetrics:   true,
		archive.BaseDatasetContentAssets:      true,
		archive.BaseDatasetContentArticles:    true,
		archive.BaseDatasetComments:           true,
		archive.BaseDatasetAPIFetches:         true,
		archive.BaseDatasetSyncStatus:         true,
	}
	for _, dataset := range datasets {
		if !want[dataset.Key] {
			t.Fatalf("unexpected or duplicate dataset %q", dataset.Key)
		}
		delete(want, dataset.Key)
		if dataset.TableName == "" || dataset.PrimaryField == "" || len(dataset.Fields) == 0 {
			t.Fatalf("incomplete dataset schema: %+v", dataset)
		}
	}
	if len(want) != 0 {
		t.Fatalf("datasets missing from Base registry: %v", want)
	}
}

func TestPrepareFieldsPreservesStructuredDataForBase(t *testing.T) {
	values := map[string]interface{}{
		"链接":   "https://example.com/a",
		"日期":   float64(1720000000000),
		"JSON": map[string]interface{}{"new_field": []interface{}{1.0, "x"}},
		"未知字段": "不会写入",
	}
	prepareFields(values, map[string]int{
		"链接":   feishu.FieldTypeURL,
		"日期":   feishu.FieldTypeDatetime,
		"JSON": feishu.FieldTypeText,
	})
	link, ok := values["链接"].(map[string]string)
	if !ok || link["link"] != "https://example.com/a" {
		t.Fatalf("link = %#v", values["链接"])
	}
	if values["日期"] != int64(1720000000000) {
		t.Fatalf("date = %#v", values["日期"])
	}
	if values["JSON"] != `{"new_field":[1,"x"]}` {
		t.Fatalf("JSON = %#v", values["JSON"])
	}
	if _, exists := values["未知字段"]; exists {
		t.Fatal("field absent from the declared Base schema must not be sent")
	}
}

func TestTruncateTextKeepsBaseWriteValidAndTraceable(t *testing.T) {
	value := strings.Repeat("数", 100100)
	got := truncateText(value, 99000)
	if len([]rune(got)) != 99000 {
		t.Fatalf("length = %d, want 99000", len([]rune(got)))
	}
	if !strings.Contains(got, "SQLite 保留完整值") || !strings.Contains(got, "sha256=") {
		t.Fatalf("missing traceable truncation suffix: %q", got[len(got)-120:])
	}
}

func TestPayloadHashIsStableAcrossMapOrder(t *testing.T) {
	left := map[string]interface{}{"a": 1, "b": "x"}
	right := map[string]interface{}{"b": "x", "a": 1}
	if payloadHash(left) != payloadHash(right) {
		t.Fatal("JSON map key order must not cause a spurious Base update")
	}
}

// fakeBase answers every Feishu call the Base writer makes, so no test in this
// package can reach the real open.feishu.cn, and counts calls by kind.
type fakeBase struct {
	mu      sync.Mutex
	calls   map[string]int
	created []string
}

func (f *fakeBase) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	body := `{"code":0,"data":{}}`
	kind := r.Method + " " + path[strings.LastIndex(path, "/"):]
	switch {
	case strings.HasSuffix(path, "/tenant_access_token/internal"):
		kind = "token"
		body = `{"code":0,"tenant_access_token":"t","expire":7200}`
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/tables"):
		items := make([]string, len(datasets))
		for index, dataset := range datasets {
			items[index] = fmt.Sprintf(`{"name":%q,"table_id":"tbl%d"}`, dataset.TableName, index)
		}
		body = `{"code":0,"data":{"items":[` + strings.Join(items, ",") + `],"has_more":false}}`
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/fields"):
		var index int
		fmt.Sscanf(path[strings.Index(path, "/tables/tbl")+len("/tables/tbl"):], "%d", &index)
		items := make([]string, 0, len(datasets[index].Fields))
		for _, field := range datasets[index].Fields {
			items = append(items, fmt.Sprintf(`{"field_name":%q}`, field.Name))
		}
		body = `{"code":0,"data":{"items":[` + strings.Join(items, ",") + `],"has_more":false}}`
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/records"):
		body = `{"code":0,"data":{"items":[],"has_more":false}}`
	case strings.HasSuffix(path, "/records/batch_create"):
		var request struct {
			Records []struct {
				Fields map[string]interface{} `json:"fields"`
			} `json:"records"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		ids := make([]string, len(request.Records))
		for index, record := range request.Records {
			key := fmt.Sprint(record.Fields["唯一键"])
			f.created = append(f.created, key)
			ids[index] = fmt.Sprintf(`{"record_id":"rec-%s"}`, key)
		}
		body = `{"code":0,"data":{"records":[` + strings.Join(ids, ",") + `]}}`
	}
	if kind != "token" {
		f.calls[kind]++
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

func (f *fakeBase) take() (map[string]int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls, created := f.calls, f.created
	f.calls, f.created = map[string]int{}, nil
	return calls, created
}

var base = &fakeBase{calls: map[string]int{}}

func TestMain(m *testing.M) {
	http.DefaultTransport = base
	config.Env.FeishuAppID, config.Env.FeishuAppSecret, config.Env.FeishuBitableAppToken = "app", "secret", "base"
	os.Exit(m.Run())
}

func openGatedSyncer(t *testing.T) (*Syncer, *archive.Store) {
	t.Helper()
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	base.take()
	return &Syncer{Store: store, BeforeSync: func(context.Context) error { return nil }, wake: make(chan struct{}, 1)}, store
}

func archiveFetch(t *testing.T, store *archive.Store, endpoint string) {
	t.Helper()
	if _, err := store.SaveFetch(context.Background(), archive.FetchRecord{
		Endpoint:     endpoint,
		Category:     "user",
		ResponseJSON: []byte(`{"list":[]}`),
		HTTPStatus:   200,
		Success:      true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRoundWithNothingToWriteNeverCallsFeishu(t *testing.T) {
	syncer, _ := openGatedSyncer(t)
	if _, err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls, _ := base.take(); len(calls) != 0 {
		t.Fatalf("idle round called Feishu: %v", calls)
	}
}

func TestArchiveWriteWakesWorkerAndOnlyThenWritesBase(t *testing.T) {
	syncer, store := openGatedSyncer(t)
	settle := func() (map[string]int, []string) {
		t.Helper()
		time.Sleep(300 * time.Millisecond)
		return base.take()
	}
	stop := make(chan struct{})
	defer close(stop)
	go syncer.run(stop, time.Millisecond, 20*time.Millisecond, time.Hour)
	if calls, _ := settle(); len(calls) != 0 {
		t.Fatalf("startup round with an empty archive called Feishu: %v", calls)
	}

	archiveFetch(t, store, "getusersummary")
	syncer.Wake()
	calls, created := settle()
	if len(created) == 0 || created[0] != "fetch:1" {
		t.Fatalf("new archive row was not written to Base: created=%v calls=%v", created, calls)
	}
	if calls["GET /fields"] == 0 {
		t.Fatalf("first write to a table must verify its fields: %v", calls)
	}

	syncer.Wake()
	if calls, _ := settle(); len(calls) != 0 {
		t.Fatalf("wake with nothing new called Feishu: %v", calls)
	}

	archiveFetch(t, store, "getusercumulate")
	syncer.Wake()
	calls, created = settle()
	if len(created) != 1 || created[0] != "fetch:2" {
		t.Fatalf("second archive row: created=%v calls=%v", created, calls)
	}
	if calls["GET /fields"] != 0 || calls["GET /tables"] != 0 || calls["GET /records"] != 0 {
		t.Fatalf("known tables must be written without re-reading tables, fields or records: %v", calls)
	}
}

func TestWorkerSyncsOnWakeAndOnFallbackOnly(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var rounds atomic.Int32
	syncer := &Syncer{
		Store:      store,
		BeforeSync: func(context.Context) error { rounds.Add(1); return errors.New("gate closed") },
		wake:       make(chan struct{}, 1),
	}
	waitFor := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for rounds.Load() < want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		if got := rounds.Load(); got != want {
			t.Fatalf("rounds = %d, want %d", got, want)
		}
	}

	stop := make(chan struct{})
	go syncer.run(stop, time.Hour, 20*time.Millisecond, time.Hour)
	waitFor(0)
	syncer.Wake()
	syncer.Wake()
	syncer.Wake()
	waitFor(1)
	syncer.Wake()
	waitFor(2)
	close(stop)

	fallbackStop := make(chan struct{})
	defer close(fallbackStop)
	rounds.Store(0)
	go syncer.run(fallbackStop, time.Millisecond, time.Hour, 150*time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for rounds.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if rounds.Load() < 3 {
		t.Fatalf("fallback timer must keep running rounds without wakes, got %d", rounds.Load())
	}
}

func TestWakeOnDisabledSyncerIsNoOp(t *testing.T) {
	var syncer *Syncer
	syncer.Wake()
}
