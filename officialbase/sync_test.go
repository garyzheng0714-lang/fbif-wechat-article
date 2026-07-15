package officialbase

import (
	"context"
	"errors"
	"strings"
	"testing"

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
