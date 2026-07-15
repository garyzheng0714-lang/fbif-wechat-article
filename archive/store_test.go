package archive

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenMigratesLegacyContentArticlesBeforeBaseViewsAreUsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE official_content_articles (
		source TEXT NOT NULL,
		object_id TEXT NOT NULL,
		article_index INTEGER NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		digest TEXT NOT NULL DEFAULT '',
		content_html TEXT NOT NULL DEFAULT '',
		content_source_url TEXT NOT NULL DEFAULT '',
		url TEXT NOT NULL DEFAULT '',
		thumb_media_id TEXT NOT NULL DEFAULT '',
		thumb_url TEXT NOT NULL DEFAULT '',
		is_deleted INTEGER NOT NULL DEFAULT 0,
		raw_json TEXT NOT NULL,
		first_seen_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL,
		PRIMARY KEY(source, object_id, article_index)
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE VIEW official_article_publications AS SELECT 'legacy' AS old_column`); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO official_content_articles (
		source, object_id, article_index, title, url, raw_json, first_seen_at, last_seen_at
	) VALUES ('freepublish', 'legacy-1', 0, '旧文章',
		'https://mp.weixin.qq.com/s?mid=42&idx=1',
		'{"article_type":"news"}', 1, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var articleType, messageID string
	if err := store.db.QueryRow(`SELECT article_type, message_id FROM official_content_articles
		WHERE source = 'freepublish' AND object_id = 'legacy-1'`).Scan(&articleType, &messageID); err != nil {
		t.Fatal(err)
	}
	if articleType != "news" || messageID != "42_1" {
		t.Fatalf("legacy migration article_type=%q message_id=%q", articleType, messageID)
	}
	columns, err := store.QueryInt64(context.Background(), `
		SELECT COUNT(*) FROM pragma_table_info('official_article_publications')
		WHERE name = 'publication_raw_json'`)
	if err != nil {
		t.Fatal(err)
	}
	if columns != 1 {
		t.Fatal("legacy analytics view was not refreshed")
	}
}

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

func TestOfficialArticleAndFollowerViewsJoinOfficialTables(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fetchedAt := time.Unix(100, 0)
	articleURL := "http://mp.weixin.qq.com/s?mid=42&idx=2"
	metricResponse := []byte(`{"list":[{"ref_date":"2026-07-01","msgid":"42_2","publish_type":1,"title":"测试文章","content_url":"` + articleURL + `","detail_list":[{"stat_date":"2026-07-02","read_user":123,"share_user":9,"read_subscribe_user":5}]}]}`)
	if _, err := store.SaveFetch(context.Background(), FetchRecord{
		Endpoint:     "getarticletotaldetail",
		Category:     "article",
		BeginDate:    "2026-07-01",
		EndDate:      "2026-07-01",
		ResponseJSON: metricResponse,
		Success:      true,
		FetchedAt:    fetchedAt,
	}); err != nil {
		t.Fatal(err)
	}

	contentResponse := []byte(`{"total_count":1,"item_count":1,"item":[{"article_id":"article-42","update_time":100,"content":{"news_item":[{"title":"测试文章","author":"作者","content":"<p>正文</p>","url":"` + articleURL + `"}]}}]}`)
	if _, err := store.SaveContentPage(context.Background(), "freepublish", contentResponse, fetchedAt); err != nil {
		t.Fatal(err)
	}

	for _, response := range []struct {
		endpoint string
		category string
		body     []byte
	}{
		{"getarticleread", "article", []byte(`{"list":[{"ref_date":"2026-07-02","msgid":"42_2","detail":{"read_user":7,"read_user_source":[{"user_count":7,"scene_desc":"全部"}]}}]}`)},
		{"getarticleshare", "article", []byte(`{"list":[{"ref_date":"2026-07-02","msgid":"42_2","detail":{"share_user":2}}]}`)},
		{"getbizsummary", "article", []byte(`{"list":[{"ref_date":"2026-07-02","detail":{"read_user":70,"share_user":20,"send_page_count":3}}]}`)},
		{"getusersummary", "user", []byte(`{"list":[{"ref_date":"2026-07-02","user_source":57,"new_user":12,"cancel_user":3}]}`)},
		{"getusercumulate", "user", []byte(`{"list":[{"ref_date":"2026-07-02","cumulate_user":1000}]}`)},
		{"getupstreammsg", "message", []byte(`{"list":[{"ref_date":"2026-07-02","user_source":0,"msg_type":1,"msg_user":4,"msg_count":8}]}`)},
		{"getinterfacesummary", "interface", []byte(`{"list":[{"ref_date":"2026-07-02","callback_count":20,"fail_count":1,"total_time_cost":80,"max_time_cost":12}]}`)},
	} {
		if _, err := store.SaveFetch(context.Background(), FetchRecord{
			Endpoint:     response.endpoint,
			Category:     response.category,
			BeginDate:    "2026-07-02",
			EndDate:      "2026-07-02",
			ResponseJSON: response.body,
			Success:      true,
			FetchedAt:    fetchedAt,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var msgDataID, articleID string
	var articleIndex int
	if err := store.db.QueryRowContext(context.Background(), `
		SELECT msg_data_id, article_index, article_id
		FROM official_article_catalog WHERE msgid = '42_2'`).Scan(&msgDataID, &articleIndex, &articleID); err != nil {
		t.Fatal(err)
	}
	if msgDataID != "42" || articleIndex != 2 || articleID != "article-42" {
		t.Fatalf("catalog identity = %q/%d/%q", msgDataID, articleIndex, articleID)
	}

	var readUser, readSubscribeUser int
	if err := store.db.QueryRowContext(context.Background(), `
		SELECT CAST(read_user AS INTEGER), CAST(read_subscribe_user AS INTEGER)
		FROM official_article_latest_performance WHERE msgid = '42_2'`).Scan(&readUser, &readSubscribeUser); err != nil {
		t.Fatal(err)
	}
	if readUser != 123 || readSubscribeUser != 5 {
		t.Fatalf("article metrics read=%d subscribe=%d", readUser, readSubscribeUser)
	}

	var netNew, cumulate int
	if err := store.db.QueryRowContext(context.Background(), `
		SELECT
			MAX(COALESCE(net_new_user, 0)),
			MAX(COALESCE(cumulate_user, 0))
		FROM official_follower_metric_facts`).Scan(&netNew, &cumulate); err != nil {
		t.Fatal(err)
	}
	if netNew != 9 || cumulate != 1000 {
		t.Fatalf("follower metrics net=%d cumulate=%d", netNew, cumulate)
	}

	articleRows, err := store.ListBaseSyncCandidates(context.Background(), BaseDatasetArticles, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articleRows) != 1 || articleRows[0].RowKey != "42_2" || articleRows[0].Fields["文章ID"] != "article-42" {
		t.Fatalf("article Base rows = %+v", articleRows)
	}
	if err := store.SaveBaseRecord(context.Background(), BaseDatasetArticles, "42_2", "rec-42", "hash", articleRows[0].SourceSeenAt, fetchedAt); err != nil {
		t.Fatal(err)
	}
	articleRows, err = store.ListBaseSyncCandidates(context.Background(), BaseDatasetArticles, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articleRows) != 0 {
		t.Fatalf("already synced article rows = %d, want 0", len(articleRows))
	}

	for _, dataset := range []string{
		BaseDatasetArticleDaily,
		BaseDatasetArticleCumulative,
		BaseDatasetAccountDaily,
		BaseDatasetFollowerSource,
		BaseDatasetFollowerCumulative,
		BaseDatasetMessageMetrics,
		BaseDatasetInterfaceMetrics,
		BaseDatasetContentAssets,
		BaseDatasetContentArticles,
		BaseDatasetSyncStatus,
	} {
		rows, err := store.ListBaseSyncCandidates(context.Background(), dataset, 10)
		if err != nil {
			t.Fatalf("%s: %v", dataset, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s rows = %d, want 1", dataset, len(rows))
		}
	}
	apiRows, err := store.ListBaseSyncCandidates(context.Background(), BaseDatasetAPIFetches, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(apiRows) < 8 {
		t.Fatalf("API fetch Base rows = %d, want every archived call", len(apiRows))
	}
}

func TestCommentResponsePreservesEveryFieldAndCreatesBaseFact(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Unix(100, 0)
	if _, err := store.SaveFetch(context.Background(), FetchRecord{
		Endpoint:     "comment_list",
		Category:     "comment",
		RequestJSON:  []byte(`{"msg_data_id":"42","index":1,"begin":0,"count":49,"type":0}`),
		ResponseJSON: []byte(`{"errcode":0,"errmsg":"ok","total":1,"comment":[{"user_comment_id":7,"create_time":90,"content":"完整评论","comment_type":1,"openid":"openid-1","reply":{"content":"完整回复","create_time":95},"future_field":{"keep":true}}]}`),
		Success:      true,
		FetchedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}

	var content, reply, raw string
	if err := store.db.QueryRowContext(context.Background(), `
		SELECT content, reply_content, raw_json FROM official_comments
		WHERE row_key = '42|1|7'`).Scan(&content, &reply, &raw); err != nil {
		t.Fatal(err)
	}
	if content != "完整评论" || reply != "完整回复" || !strings.Contains(raw, `"future_field":{"keep":true}`) {
		t.Fatalf("comment content=%q reply=%q raw=%q", content, reply, raw)
	}
	candidates, err := store.ListBaseSyncCandidates(context.Background(), BaseDatasetComments, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Fields["评论者OpenID"] != "openid-1" {
		t.Fatalf("comment Base candidates = %+v", candidates)
	}
}

func TestBaseSyncStatusOnlyIncludesPublishedArticleAndFollowerScope(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	if err := store.MarkSuccess(context.Background(), "getarticleread", "article", "2026-07-14", "2026-07-14", "2026-07-13", now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSuccess(context.Background(), "getupstreammsgdistweek", "message", "2026-07-14", "2026-07-14", "2026-07-13", now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkContentPageSuccess(context.Background(), "freepublish", 20, 273, false, now); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkContentPageSuccess(context.Background(), "draft", 20, 7518, false, now); err != nil {
		t.Fatal(err)
	}

	rows, err := store.ListBaseSyncCandidates(context.Background(), BaseDatasetSyncStatus, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("sync status rows=%d want article API + freepublish only: %+v", len(rows), rows)
	}
	got := map[string]BaseSyncCandidate{}
	for _, row := range rows {
		got[row.RowKey] = row
	}
	if got["api:getarticleread"].Fields["回填方向"] != "newest_to_oldest" {
		t.Fatalf("article backfill direction missing: %+v", got["api:getarticleread"])
	}
	if _, exists := got["api:getupstreammsgdistweek"]; exists {
		t.Fatal("message analytics must not enter Base sync status")
	}
	if _, exists := got["content:draft"]; exists {
		t.Fatal("draft inventory must not enter Base sync status")
	}
}

func TestOpenBackfillsCommentsFromArchivedRawResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`INSERT INTO official_api_fetches (
		endpoint, category, request_json, response_json, response_sha256,
		success, fetched_at
	) VALUES ('comment_list', 'comment', ?, ?, 'hash', 1, 100000)`,
		`{"msg_data_id":"99","index":0}`,
		`{"comment":[{"user_comment_id":3,"content":"历史评论"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	count, err := store.QueryInt64(context.Background(), `
		SELECT COUNT(*) FROM official_comments WHERE row_key = '99|0|3' AND content = '历史评论'`)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backfilled comments = %d, want 1", count)
	}
}

func TestBaseUnresolvedRecordsForceRemoteReconciliation(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.MarkBaseRecordError(ctx, BaseDatasetArticles, "42_1", fmt.Errorf("partial batch failure")); err != nil {
		t.Fatal(err)
	}
	unresolved, err := store.BaseUnresolvedRecordCount(ctx, BaseDatasetArticles)
	if err != nil || unresolved != 1 {
		t.Fatalf("unresolved=%d err=%v", unresolved, err)
	}
	if err := store.SeedBaseRecord(ctx, BaseDatasetArticles, "42_1", "rec-42"); err != nil {
		t.Fatal(err)
	}
	unresolved, err = store.BaseUnresolvedRecordCount(ctx, BaseDatasetArticles)
	if err != nil || unresolved != 0 {
		t.Fatalf("reconciled unresolved=%d err=%v", unresolved, err)
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
	if info.ObjectTotalCount != 1 || len(info.ObjectIDs) != 1 || info.ObjectIDs[0] != "article-1" {
		t.Fatalf("page info = %+v", info)
	}
	objects, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_objects WHERE source = 'freepublish'`)
	articles, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles WHERE content_html = '<p>全文</p>'`)
	unknown, _ := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles WHERE json_extract(raw_json, '$.unknown_new_field.keep') = 1`)
	if objects != 1 || articles != 1 || unknown != 1 {
		t.Fatalf("objects=%d articles=%d unknown=%d", objects, articles, unknown)
	}
}

func TestFreePublishTotalCountIsObjectCountNotArticleCount(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	raw := []byte(`{"total_count":1,"item_count":1,"item":[{"article_id":"publish-object-1","update_time":100,"content":{"news_item":[{"title":"第一篇","url":"https://mp.weixin.qq.com/s/one"},{"title":"第二篇","url":"https://mp.weixin.qq.com/s/two"}]}}]}`)
	info, err := store.SaveContentPage(context.Background(), "freepublish", raw, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	objects, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_objects WHERE source = 'freepublish'`)
	if err != nil {
		t.Fatal(err)
	}
	articles, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles WHERE source = 'freepublish'`)
	if err != nil {
		t.Fatal(err)
	}
	if info.ObjectTotalCount != 1 || objects != 1 || articles != 2 {
		t.Fatalf("freepublish total_count must remain an object count: info=%d objects=%d articles=%d", info.ObjectTotalCount, objects, articles)
	}
}

func TestDraftArticleTypeSurvivesDetailResponseWithoutType(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Unix(100, 0)
	page := []byte(`{"total_count":1,"item_count":1,"item":[{"media_id":"draft-1","update_time":100,"content":{"news_item":[{"article_type":"newspic","title":"小绿书","content":"纯文本","thumb_url":"https://mmbiz.qpic.cn/cover.jpg"}]}}]}`)
	if _, err := store.SaveContentPage(context.Background(), "draft", page, now); err != nil {
		t.Fatal(err)
	}
	detail := []byte(`{"news_item":[{"title":"小绿书","content":"纯文本","thumb_url":"https://mmbiz.qpic.cn/cover.jpg"}]}`)
	if err := store.SaveContentDetail(context.Background(), "draft", "draft-1", detail, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	typed, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles WHERE source = 'draft' AND article_type = 'newspic'`)
	if err != nil || typed != 1 {
		t.Fatalf("newspic type must survive detail response without article_type: typed=%d err=%v", typed, err)
	}
}
