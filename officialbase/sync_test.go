package officialbase

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
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

func openGatedSyncer(t *testing.T) (*Syncer, *archive.Store) {
	t.Helper()
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return &Syncer{Store: store, BeforeSync: func(context.Context) error { return nil }}, store
}

func TestRoundWithNothingToWriteNeverCallsFeishu(t *testing.T) {
	syncer, _ := openGatedSyncer(t)
	result, err := syncer.Sync(context.Background())
	if err != nil {
		t.Fatalf("idle round must not reach Feishu (no credentials are configured in tests): %v", err)
	}
	for key, dataset := range result.Datasets {
		if dataset != (DatasetResult{}) {
			t.Fatalf("%s: idle round touched rows: %+v", key, dataset)
		}
	}
}

func TestRoundWithNewArchiveRowWritesToFeishu(t *testing.T) {
	syncer, store := openGatedSyncer(t)
	if _, err := store.SaveFetch(context.Background(), archive.FetchRecord{
		Endpoint:     "getusersummary",
		Category:     "user",
		ResponseJSON: []byte(`{"list":[]}`),
		HTTPStatus:   200,
		Success:      true,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := syncer.Sync(context.Background())
	if err == nil || !strings.Contains(result.Errors[archive.BaseDatasetAPIFetches], "Feishu credentials not configured") {
		t.Fatalf("a new archived row must be sent to Feishu, got %v", err)
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
