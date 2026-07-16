package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

type fakeContentClient struct {
	calls []string
}

func (f *fakeContentClient) Call(_ context.Context, endpoint wechat.ContentEndpoint, payload interface{}) (*wechat.RawAPIResponse, error) {
	f.calls = append(f.calls, endpoint.Name)
	request, _ := json.Marshal(payload)
	response := `{"errcode":0,"errmsg":"ok"}`
	switch endpoint.Name {
	case "draft_count":
		response = `{"total_count":1}`
	case "material_get_materialcount":
		response = `{"news_count":1,"image_count":0,"voice_count":0,"video_count":0}`
	case "draft_batchget":
		response = `{"total_count":1,"item_count":1,"item":[{"media_id":"draft-1","update_time":1,"content":{"news_item":[{"article_type":"news","title":"draft"}]}}]}`
	case "freepublish_batchget":
		response = `{"total_count":1,"item_count":1,"item":[{"article_id":"article-1","update_time":1,"content":{"news_item":[{"title":"published","url":"https://mp.weixin.qq.com/s/x"}]}}]}`
	case "material_batchget_material":
		materialType := ""
		if body, ok := payload.(map[string]interface{}); ok {
			materialType, _ = body["type"].(string)
		}
		if materialType == "news" {
			response = `{"total_count":1,"item_count":1,"item":[{"media_id":"material-1","update_time":1,"content":{"news_item":[{"title":"material"}]}}]}`
		} else {
			response = `{"total_count":0,"item_count":0,"item":[]}`
		}
	case "draft_get", "freepublish_getarticle", "material_get_material":
		response = `{"news_item":[{"title":"detail","new_field":"kept"}]}`
	}
	return &wechat.RawAPIResponse{
		Endpoint:    endpoint.Name,
		RequestBody: request,
		Body:        []byte(response),
		HTTPStatus:  200,
	}, nil
}

type orderedPublishedClient struct {
	detailIDs []string
}

func (f *orderedPublishedClient) Call(_ context.Context, endpoint wechat.ContentEndpoint, payload interface{}) (*wechat.RawAPIResponse, error) {
	request, _ := json.Marshal(payload)
	body := []byte(`{"errcode":0,"errmsg":"ok"}`)
	switch endpoint.Name {
	case "draft_batchget":
		body = []byte(`{"total_count":0,"item_count":0,"item":[]}`)
	case "freepublish_batchget":
		body = []byte(`{"total_count":3,"item_count":3,"item":[{"article_id":"newest","update_time":300,"content":{"news_item":[{"title":"newest","content":"<p>newest</p>","url":"https://mp.weixin.qq.com/s/newest"}]}},{"article_id":"middle","update_time":200,"content":{"news_item":[{"title":"middle","content":"<p>middle</p>","url":"https://mp.weixin.qq.com/s/middle"}]}},{"article_id":"oldest","update_time":100,"content":{"news_item":[{"title":"oldest","content":"<p>oldest</p>","url":"https://mp.weixin.qq.com/s/oldest"}]}}]}`)
	case "freepublish_getarticle":
		values, _ := payload.(map[string]string)
		f.detailIDs = append(f.detailIDs, values["article_id"])
		body = []byte(`{"news_item":[{"title":"detail","future_field":{"kept":true}}]}`)
	}
	return &wechat.RawAPIResponse{Endpoint: endpoint.Name, RequestBody: request, Body: body, HTTPStatus: 200}, nil
}

func TestContentCollectorArchivesOnlyPublishedInventoryAndLatestDraftType(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := &fakeContentClient{}
	collector := &ContentCollector{
		Client:   client,
		Store:    store,
		Now:      func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, wechat.ShanghaiLoc()) },
		MaxCalls: 20,
	}
	result, err := collector.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 0 {
		t.Fatalf("result = %+v", result)
	}
	want := []string{"draft_batchget", "freepublish_batchget", "freepublish_getarticle"}
	for _, name := range want {
		if !containsString(client.calls, name) {
			t.Fatalf("%s was not called; calls=%v", name, client.calls)
		}
	}
	for _, name := range []string{
		"draft_count", "draft_get", "material_get_materialcount",
		"material_batchget_material", "material_get_material",
	} {
		if containsString(client.calls, name) {
			t.Fatalf("%s is outside the published-article archive scope; calls=%v", name, client.calls)
		}
	}
	articles, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles`)
	if err != nil {
		t.Fatal(err)
	}
	if articles != 2 {
		t.Fatalf("articles = %d", articles)
	}
	fetches, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_fetches`)
	if err != nil {
		t.Fatal(err)
	}
	if fetches != int64(result.Calls) {
		t.Fatalf("fetches=%d calls=%d", fetches, result.Calls)
	}
	state, err := store.GetContentState(context.Background(), "freepublish")
	if err != nil || state == nil || !state.ObjectInventoryComplete {
		t.Fatalf("已发布历史分页必须在详情/评论前获得预留预算：state=%+v err=%v", state, err)
	}
}

func TestRefreshPublishedDoesNotDependOnDraftAPI(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := &fakeContentClient{}
	collector := &ContentCollector{Client: client, Store: store}
	if _, err := collector.RefreshPublished(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"freepublish_batchget"}
	if len(client.calls) != len(want) {
		t.Fatalf("calls=%v want=%v", client.calls, want)
	}
	for i := range want {
		if client.calls[i] != want[i] {
			t.Fatalf("已发布轮询不得被草稿 API 阻断：calls=%v", client.calls)
		}
	}
	published, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles WHERE source = 'freepublish'`)
	if err != nil || published != 1 {
		t.Fatalf("official published page not persisted: published=%d err=%v", published, err)
	}
}

func TestContentCollectorFetchesEveryPublishedDetailNewestToOldest(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := &orderedPublishedClient{}
	collector := &ContentCollector{Client: client, Store: store, MaxCalls: 10}
	result, err := collector.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"newest", "middle", "oldest"}
	if fmt.Sprint(client.detailIDs) != fmt.Sprint(want) {
		t.Fatalf("detail order=%v want=%v", client.detailIDs, want)
	}
	if result.Calls != 5 {
		t.Fatalf("calls=%d want draft page + published page + 3 details", result.Calls)
	}
	pending, err := store.ListObjectsNeedingDetail(context.Background(), "freepublish", 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("published details not complete: pending=%v err=%v", pending, err)
	}
}

func TestContentBackfillDoesNotPollRecentDraftOrPublishedPages(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := &fakeContentClient{}
	collector := &ContentCollector{Client: client, Store: store, MaxCalls: 2}
	if _, err := collector.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.calls = nil
	collector.MaxCalls = 10
	if _, err := collector.RunBackfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if containsString(client.calls, "draft_batchget") || containsString(client.calls, "freepublish_batchget") {
		t.Fatalf("workday outside backfill must not poll recent pages: calls=%v", client.calls)
	}
	if !containsString(client.calls, "freepublish_getarticle") {
		t.Fatalf("historical detail backfill did not continue: calls=%v", client.calls)
	}
}

func TestContentBackfillWithNoInventoryWaitsForWorkdayMonitor(t *testing.T) {
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	client := &fakeContentClient{}
	collector := &ContentCollector{Client: client, Store: store, MaxCalls: 10}
	result, err := collector.RunBackfill(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Calls != 0 || len(client.calls) != 0 {
		t.Fatalf("outside-window backfill must not initialize from latest pages: result=%+v calls=%v", result, client.calls)
	}
}

func TestSplitMessageID(t *testing.T) {
	msgDataID, index, ok := splitMessageID("2247490098_2")
	if !ok || msgDataID != "2247490098" || index != 1 {
		t.Fatalf("got %q %d %v", msgDataID, index, ok)
	}
	if _, _, ok := splitMessageID(fmt.Sprint("invalid")); ok {
		t.Fatal("invalid msgid accepted")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
