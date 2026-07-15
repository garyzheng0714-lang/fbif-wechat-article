package autolayout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
)

type fakeLayoutAPI struct {
	articles []Article
	failNext bool
}

const (
	testOfficialContent = "<p>官方正文</p><img src=\"https://mmbiz.qpic.cn/body.jpg\">"
	testOfficialCover   = "https://mmbiz.qpic.cn/cover.jpg"
)

func (f *fakeLayoutAPI) SubmitOfficial(_ context.Context, article Article) (Receipt, error) {
	f.articles = append(f.articles, article)
	if f.failNext {
		f.failNext = false
		return Receipt{}, errors.New("temporary layout failure")
	}
	return Receipt{JobID: int64(100 + len(f.articles)), Stage: "rendering"}, nil
}

func savePublishedArticle(t *testing.T, store *archive.Store, articleID, title, articleURL string, now time.Time) {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"total_count": 1,
		"item_count":  1,
		"item": []interface{}{map[string]interface{}{
			"article_id":  articleID,
			"update_time": now.Unix(),
			"content": map[string]interface{}{
				"news_item": []interface{}{map[string]interface{}{
					"title":     title,
					"author":    "作者",
					"content":   testOfficialContent,
					"url":       articleURL,
					"thumb_url": testOfficialCover,
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal official page: %v", err)
	}
	if _, err := store.SaveContentPage(context.Background(), "freepublish", body, now); err != nil {
		t.Fatalf("save official page: %v", err)
	}
}

func saveDraftArticle(t *testing.T, store *archive.Store, mediaID, title, articleType string, now time.Time) {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"total_count": 1,
		"item_count":  1,
		"item": []interface{}{map[string]interface{}{
			"media_id":    mediaID,
			"update_time": now.Unix(),
			"content": map[string]interface{}{
				"news_item": []interface{}{map[string]interface{}{
					"article_type": articleType,
					"title":        title,
					"author":       "作者",
					"content":      testOfficialContent,
					"url":          "https://mp.weixin.qq.com/s/draft-preview",
					"thumb_url":    testOfficialCover,
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal official draft page: %v", err)
	}
	if _, err := store.SaveContentPage(context.Background(), "draft", body, now); err != nil {
		t.Fatalf("save official draft page: %v", err)
	}
}

func TestDispatcherBaselinesExistingAndDeliversOnlyNewOfficialContent(t *testing.T) {
	store, err := archive.Open(t.TempDir() + "/archive.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	saveDraftArticle(t, store, "draft-old", "历史文章", "news", now.Add(-time.Hour))
	savePublishedArticle(t, store, "old", "历史文章", "http://mp.weixin.qq.com/s?__biz=MzA3&mid=1&idx=1&sn=old", now.Add(-time.Hour))
	fake := &fakeLayoutAPI{}
	dispatcher := &Dispatcher{
		Store: store, Client: fake, SourceName: "FBIF食品饮料创新", MaxDeliveries: 20,
		Now: func() time.Time { return now },
	}

	first, err := dispatcher.Sync(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !first.Bootstrapped || first.Baselined != 1 || len(fake.articles) != 0 {
		t.Fatalf("首启应只做历史基线，不投递：%+v calls=%d", first, len(fake.articles))
	}

	newURL := "http://mp.weixin.qq.com/s?sn=new&idx=1&mid=2&__biz=MzA3&chksm=x"
	saveDraftArticle(t, store, "draft-new", "新文章", "news", now.Add(time.Minute))
	savePublishedArticle(t, store, "new", "新文章", newURL, now.Add(time.Minute))
	second, err := dispatcher.Sync(context.Background())
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if second.Discovered != 1 || second.Delivered != 1 || len(fake.articles) != 1 {
		t.Fatalf("新官方文章应只投递一次：%+v calls=%d", second, len(fake.articles))
	}
	got := fake.articles[0]
	if got.URL != newURL || got.Title != "新文章" || got.SourceName != "FBIF食品饮料创新" || got.ContentHTML == "" || got.CoverURL == "" {
		t.Fatalf("投递字段不完整：%+v", got)
	}
	// 历史分页可能在启用后才补入数据库；必须按官方 update_time 过滤，不能按
	// first_seen_at 把旧文章误当新文章。
	saveDraftArticle(t, store, "draft-backfill", "历史补采", "news", now.Add(-24*time.Hour))
	savePublishedArticle(t, store, "backfill", "历史补采", "https://mp.weixin.qq.com/s/backfill", now.Add(-24*time.Hour))
	third, err := dispatcher.Sync(context.Background())
	if err != nil {
		t.Fatalf("idempotent sync: %v", err)
	}
	if third.Discovered != 0 || third.Attempted != 0 || len(fake.articles) != 1 {
		t.Fatalf("重复轮询和历史补采都不得重复投递：%+v calls=%d", third, len(fake.articles))
	}
	stats, err := store.LayoutStats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Baseline != 1 || stats.Delivered != 1 || stats.Pending != 0 || stats.Failed != 0 {
		t.Fatalf("outbox 统计不符：%+v", stats)
	}
}

func TestDispatcherRetriesPersistedFailure(t *testing.T) {
	store, err := archive.Open(t.TempDir() + "/archive.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	fake := &fakeLayoutAPI{failNext: true}
	dispatcher := &Dispatcher{Store: store, Client: fake, MaxDeliveries: 20, Now: func() time.Time { return now }}
	if _, err := dispatcher.Sync(context.Background()); err != nil {
		t.Fatalf("empty bootstrap: %v", err)
	}
	saveDraftArticle(t, store, "draft-retry", "重试文章", "news", now)
	savePublishedArticle(t, store, "retry", "重试文章", "https://mp.weixin.qq.com/s/retry", now)
	failed, err := dispatcher.Sync(context.Background())
	if err == nil || failed.Failed != 1 {
		t.Fatalf("首次投递应持久失败：result=%+v err=%v", failed, err)
	}
	stats, _ := store.LayoutStats(context.Background())
	if stats.Failed != 1 || stats.LastError == "" {
		t.Fatalf("失败状态未持久化：%+v", stats)
	}

	now = now.Add(16 * time.Minute)
	retried, err := dispatcher.Sync(context.Background())
	if err != nil || retried.Delivered != 1 {
		t.Fatalf("到期后应成功重试：result=%+v err=%v", retried, err)
	}
	stats, _ = store.LayoutStats(context.Background())
	if stats.Failed != 0 || stats.Delivered != 1 {
		t.Fatalf("成功后应清除失败态：%+v", stats)
	}
}

func TestDispatcherSkipsNewspicAndHoldsUnclassified(t *testing.T) {
	store, err := archive.Open(t.TempDir() + "/archive.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	fake := &fakeLayoutAPI{}
	dispatcher := &Dispatcher{Store: store, Client: fake, MaxDeliveries: 20, Now: func() time.Time { return now }}
	if _, err := dispatcher.Sync(context.Background()); err != nil {
		t.Fatalf("empty bootstrap: %v", err)
	}

	saveDraftArticle(t, store, "draft-newspic", "小绿书", "newspic", now.Add(time.Minute))
	// newspic 发布时微信会改写正文形态；类型关联不能依赖正文逐字一致。
	if err := store.SaveContentDetail(context.Background(), "draft", "draft-newspic", []byte(`{"news_item":[{"title":"小绿书","author":"作者","content":"发布前图集正文","thumb_url":"https://mmbiz.qpic.cn/cover.jpg"}]}`), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	savePublishedArticle(t, store, "published-newspic", "小绿书", "https://mp.weixin.qq.com/s/newspic", now.Add(time.Minute))
	skipped, err := dispatcher.Sync(context.Background())
	if err != nil {
		t.Fatalf("skip newspic: %v", err)
	}
	if skipped.SkippedNewspic != 1 || skipped.HeldUnclassified != 0 || skipped.Discovered != 0 || len(fake.articles) != 0 {
		t.Fatalf("newspic 必须只记录跳过、不得投递：result=%+v calls=%d", skipped, len(fake.articles))
	}
	status, err := dispatcher.Status(context.Background())
	if err != nil || !status.Ready || status.Outbox.SkippedNewspic != 1 {
		t.Fatalf("已识别 newspic 不应让服务不健康：status=%+v err=%v", status, err)
	}

	savePublishedArticle(t, store, "published-unknown", "类型未知", "https://mp.weixin.qq.com/s/unknown", now.Add(2*time.Minute))
	held, err := dispatcher.Sync(context.Background())
	if err != nil {
		t.Fatalf("hold unknown: %v", err)
	}
	if held.HeldUnclassified != 1 || held.Discovered != 0 || len(fake.articles) != 0 {
		t.Fatalf("无官方类型快照时必须 fail closed：result=%+v calls=%d", held, len(fake.articles))
	}
	status, err = dispatcher.Status(context.Background())
	if err != nil || status.Ready || status.Outbox.HeldUnclassified != 1 {
		t.Fatalf("未分类已发布内容必须进健康告警：status=%+v err=%v", status, err)
	}

	saveDraftArticle(t, store, "draft-unknown", "类型未知", "news", now.Add(3*time.Minute))
	released, err := dispatcher.Sync(context.Background())
	if err != nil {
		t.Fatalf("release confirmed news: %v", err)
	}
	if released.HeldUnclassified != 0 || released.Discovered != 1 || released.Delivered != 1 || len(fake.articles) != 1 {
		t.Fatalf("官方确认 news 后应恢复投递：result=%+v calls=%d", released, len(fake.articles))
	}
}

func TestDispatcherSkipsNewspicWhenOnlyStableIdentityIsTitleAndIndex(t *testing.T) {
	store, err := archive.Open(t.TempDir() + "/archive.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 15, 8, 32, 0, 0, time.UTC)
	fake := &fakeLayoutAPI{}
	dispatcher := &Dispatcher{Store: store, Client: fake, MaxDeliveries: 20, Now: func() time.Time { return now }}
	if _, err := dispatcher.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	draft := []byte(fmt.Sprintf(`{"total_count":1,"item_count":1,"item":[{"media_id":"draft-gallery","update_time":%d,"content":{"news_item":[{"article_type":"newspic","title":"脑洞大开！甜味的骨汤气泡饮料来了","content":"发布前图集正文"}]}}]}`, now.Add(time.Minute).Unix()))
	if _, err := store.SaveContentPage(context.Background(), "draft", draft, now); err != nil {
		t.Fatal(err)
	}
	published := []byte(fmt.Sprintf(`{"total_count":1,"item_count":1,"item":[{"article_id":"published-gallery","update_time":%d,"content":{"news_item":[{"title":"脑洞大开！甜味的骨汤气泡饮料来了","content":"发布后微信改写的正文","url":"https://mp.weixin.qq.com/s/gallery"}]}}]}`, now.Add(3*time.Hour).Unix()))
	if _, err := store.SaveContentPage(context.Background(), "freepublish", published, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	result, err := dispatcher.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedNewspic != 1 || result.Discovered != 0 || len(fake.articles) != 0 {
		t.Fatalf("真实图集形态不得进入排版：result=%+v calls=%d", result, len(fake.articles))
	}
}

func TestHTTPAPISendsAdminPasswordAndOfficialBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Password") != "secret" {
			t.Errorf("admin password header missing")
		}
		var article Article
		if err := json.NewDecoder(r.Body).Decode(&article); err != nil {
			t.Errorf("decode: %v", err)
		}
		if article.ContentHTML != "<p>正文</p>" {
			t.Errorf("official body mismatch: %q", article.ContentHTML)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"job":{"id":88,"stage":"rendering"},"existing":false}`)
	}))
	defer server.Close()
	client := &HTTPAPI{Endpoint: server.URL, AdminPassword: "secret", Client: server.Client()}
	receipt, err := client.SubmitOfficial(context.Background(), Article{URL: "https://mp.weixin.qq.com/s/x", ContentHTML: "<p>正文</p>"})
	if err != nil || receipt.JobID != 88 || receipt.Stage != "rendering" || receipt.Existing {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}

func TestCanonicalSourceKeyMatchesLayoutIdentity(t *testing.T) {
	a, err := CanonicalSourceKey("http://mp.weixin.qq.com/s?__biz=MzA3&mid=2&idx=1&sn=a&chksm=x#rd")
	if err != nil {
		t.Fatalf("canonical a: %v", err)
	}
	b, err := CanonicalSourceKey("https://mp.weixin.qq.com/s?idx=1&mid=2&__biz=MzA3&sn=b")
	if err != nil {
		t.Fatalf("canonical b: %v", err)
	}
	if a != b {
		t.Fatalf("同文不同签名应同 key：%q != %q", a, b)
	}
	short, err := CanonicalSourceKey("https://mp.weixin.qq.com/s/AbCd?scene=1#rd")
	if err != nil || short != "site:https://mp.weixin.qq.com/s/AbCd" {
		t.Fatalf("短链归一错误：%q err=%v", short, err)
	}
}
