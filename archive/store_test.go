package archive

import (
	"context"
	"testing"
	"time"
)

func TestSaveFetchPreservesRawAndNormalizesArticleMetrics(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	raw := []byte(`{"list":[{"ref_date":"2026-07-01","msgid":"42_1","title":"test","detail_list":[{"stat_date":"2026-07-02","read_user":123,"share_user":9,"new_metric":{"x":1}}]}],"is_delay":"false"}`)
	fetchedAt := time.Unix(100, 0)
	if _, err := store.SaveFetch(context.Background(), FetchRecord{
		Endpoint:     "getarticletotaldetail",
		Category:     "article",
		BeginDate:    "2026-07-01",
		EndDate:      "2026-07-01",
		RequestJSON:  []byte(`{"begin_date":"2026-07-01","end_date":"2026-07-01"}`),
		ResponseJSON: raw,
		HTTPStatus:   200,
		Success:      true,
		FetchedAt:    fetchedAt,
	}); err != nil {
		t.Fatal(err)
	}

	fetches, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_fetches WHERE response_json = ?`, raw)
	if err != nil {
		t.Fatal(err)
	}
	if fetches != 1 {
		t.Fatalf("raw response rows = %d, want 1", fetches)
	}

	rows, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_rows WHERE endpoint = 'getarticletotaldetail'`)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("normalized rows = %d, want top-level + detail = 2", rows)
	}

	readUser, err := store.QueryInt64(context.Background(), `
		SELECT CAST(read_user AS INTEGER)
		FROM official_article_metrics
		WHERE endpoint = 'getarticletotaldetail' AND row_scope = 'detail_list'`)
	if err != nil {
		t.Fatal(err)
	}
	if readUser != 123 {
		t.Fatalf("read_user = %d, want 123", readUser)
	}
}

func TestSaveFetchKeepsEveryRevision(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, count := range []string{"10", "20"} {
		raw := []byte(`{"list":[{"ref_date":"2026-07-01","msgid":"42_1","int_page_read_count":` + count + `}]}`)
		if _, err := store.SaveFetch(context.Background(), FetchRecord{
			Endpoint:     "getarticlesummary",
			Category:     "article",
			BeginDate:    "2026-07-01",
			EndDate:      "2026-07-01",
			ResponseJSON: raw,
			Success:      true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	fetches, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_fetches`)
	rows, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_rows`)
	latest, _ := store.QueryInt64(context.Background(), `SELECT CAST(read_count AS INTEGER) FROM official_article_metrics`)
	if fetches != 2 || rows != 1 || latest != 20 {
		t.Fatalf("fetches=%d rows=%d latest=%d", fetches, rows, latest)
	}
}

func TestSaveFetchReferencesIdenticalPayloadWithoutDuplicatingBytes(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raw := []byte(`{"list":[{"ref_date":"2026-07-01","msgid":"42_1","read_user":10}]}`)
	for i := 0; i < 2; i++ {
		if _, err := store.SaveFetch(context.Background(), FetchRecord{
			Endpoint:     "getarticleread",
			Category:     "article",
			BeginDate:    "2026-07-01",
			EndDate:      "2026-07-01",
			RequestJSON:  []byte(`{"begin_date":"2026-07-01","end_date":"2026-07-01"}`),
			ResponseJSON: raw,
			Success:      true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	physical, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_fetches WHERE response_json IS NOT NULL`)
	resolved, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_fetch_payloads WHERE response_json IS NOT NULL`)
	if physical != 1 || resolved != 2 {
		t.Fatalf("physical=%d resolved=%d", physical, resolved)
	}
}

func TestEndpointStateTracksBackfillAndFailure(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Unix(100, 0)
	if err := store.MarkSuccess(context.Background(), "getuserread", "article", "2026-01-01", "2026-01-03", "2026-01-04", now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFailure(context.Background(), "getuserread", "article", "48001 api unauthorized", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	state, err := store.GetState(context.Background(), "getuserread")
	if err != nil {
		t.Fatal(err)
	}
	if state.NextBackfillDate != "2026-01-04" || state.LastError == "" || state.ConsecutiveFailures != 1 {
		t.Fatalf("state = %+v", state)
	}
}

func TestSaveContentPagePreservesObjectsAndFullArticleFields(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	raw := []byte(`{"total_count":1,"item_count":1,"item":[{"article_id":"article-1","update_time":100,"content":{"news_item":[{"title":"标题","author":"作者","content":"<p>全文</p>","url":"https://mp.weixin.qq.com/s/x","unknown_new_field":{"keep":true}}]}}]}`)
	info, err := store.SaveContentPage(context.Background(), "freepublish", raw, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if info.TotalCount != 1 || len(info.ObjectIDs) != 1 || info.ObjectIDs[0] != "article-1" {
		t.Fatalf("page info = %+v", info)
	}
	objects, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_objects WHERE source = 'freepublish'`)
	articles, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles WHERE content_html = '<p>全文</p>'`)
	unknown, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles WHERE json_extract(raw_json, '$.unknown_new_field.keep') = 1`)
	if objects != 1 || articles != 1 || unknown != 1 {
		t.Fatalf("objects=%d articles=%d unknown=%d", objects, articles, unknown)
	}
}
