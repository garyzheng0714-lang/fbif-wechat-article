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
		response = `{"total_count":1,"item_count":1,"item":[{"media_id":"draft-1","update_time":1,"content":{"news_item":[{"title":"draft"}]}}]}`
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

func TestContentCollectorCallsAllInventoryAndDetailInterfaces(t *testing.T) {
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
	want := []string{
		"draft_count", "material_get_materialcount", "draft_batchget",
		"freepublish_batchget", "material_batchget_material", "draft_get",
		"freepublish_getarticle", "material_get_material",
	}
	for _, name := range want {
		if !containsString(client.calls, name) {
			t.Fatalf("%s was not called; calls=%v", name, client.calls)
		}
	}
	articles, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_content_articles`)
	if err != nil {
		t.Fatal(err)
	}
	if articles < 3 {
		t.Fatalf("articles = %d", articles)
	}
	fetches, err := store.QueryInt64(context.Background(), `SELECT COUNT(*) FROM official_api_fetches`)
	if err != nil {
		t.Fatal(err)
	}
	if fetches != int64(result.Calls) {
		t.Fatalf("fetches=%d calls=%d", fetches, result.Calls)
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
