package bizdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// options 读回多选格子（假表里写进去的值可能是 []string 或 []interface{}）。
func options(value interface{}) []string {
	var out []string
	switch typed := value.(type) {
	case []string:
		out = append(out, typed...)
	case []interface{}:
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
	}
	return out
}

func dayMs(year int, month time.Month, day int) float64 {
	return float64(time.Date(year, month, day, 0, 0, 0, 0, wechat.ShanghaiLoc()).UnixMilli())
}

func TestFillLabelsFromFeedOnlyFillsBlankCells(t *testing.T) {
	worker, base, pages := newAuthorWorker(t)
	var asked []string
	classified := 0
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer feed-token" {
			t.Errorf("unexpected token %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/api/internal/wechat-article-labels/classify" {
			classified++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"articles": []map[string]interface{}{{"key": "500_1", "labels": []string{"food.snack", "country.jp"}}}})
			return
		}
		asked = append(asked, r.URL.Query().Get("biz")+" "+r.URL.Query().Get("since"))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"articles": []map[string]interface{}{
			{"mid": "100", "idx": "1", "classified": true, "labels": []string{"country.cn", "depth", "food.dairy"}},
			{"mid": "200", "idx": "1", "classified": true, "labels": []string{"country.jp", "food.snack"}},
			{"mid": "300", "idx": "1", "classified": true, "labels": []string{}},
			{"mid": "400", "idx": "1", "classified": false, "labels": []string{}},
		}})
	}))
	defer feed.Close()
	worker.FeedURL, worker.FeedToken = feed.URL, "feed-token"

	rows := base.table("tblArticles")
	rows["recHuman"] = map[string]interface{}{"内容标题": titled("900", "1", "人填过的"), "发表日期": dayMs(2026, 11, 5), "品类": []interface{}{"食品"}, "国家": []interface{}{"中国"}}
	rows["recBlank"] = map[string]interface{}{"内容标题": titled("100", "1", "两格都空"), "发表日期": dayMs(2026, 11, 5)}
	rows["recHalf"] = map[string]interface{}{"内容标题": titled("200", "1", "品类有人填了"), "发表日期": dayMs(2026, 11, 4), "品类": []interface{}{"饮料"}}
	rows["recNoLabel"] = map[string]interface{}{"内容标题": titled("300", "1", "资讯订阅没标"), "发表日期": dayMs(2026, 11, 4)}
	rows["recPending"] = map[string]interface{}{"内容标题": titled("400", "1", "资讯订阅还没分类"), "发表日期": dayMs(2026, 11, 5)}
	rows["recOld"] = map[string]interface{}{"内容标题": titled("500", "1", "资讯订阅没有这篇"), "发表日期": dayMs(2026, 10, 1)}
	pages.pages[articleLink("500", "1")] = originalPage(`<p>日本零食</p>`)

	if wait := worker.labelRound(context.Background(), 6*time.Hour); wait != labelRetry {
		t.Fatalf("a recent article not yet classified should be asked again in %s, got %s", labelRetry, wait)
	}
	if !reflect.DeepEqual(options(rows["recBlank"]["品类"]), []string{"乳制品"}) || !reflect.DeepEqual(options(rows["recBlank"]["国家"]), []string{"中国"}) {
		t.Fatalf("blank cells take the feed labels (深度 is not a 品类): %v / %v", rows["recBlank"]["品类"], rows["recBlank"]["国家"])
	}
	if !reflect.DeepEqual(options(rows["recHalf"]["品类"]), []string{"饮料"}) || !reflect.DeepEqual(options(rows["recHalf"]["国家"]), []string{"日本"}) {
		t.Fatalf("human 品类 stays, blank 国家 filled: %v / %v", rows["recHalf"]["品类"], rows["recHalf"]["国家"])
	}
	if !reflect.DeepEqual(options(rows["recHuman"]["品类"]), []string{"食品"}) {
		t.Fatalf("human values stay: %v", rows["recHuman"]["品类"])
	}
	if !reflect.DeepEqual(options(rows["recOld"]["品类"]), []string{"零食"}) || !reflect.DeepEqual(options(rows["recOld"]["国家"]), []string{"日本"}) || classified != 1 {
		t.Fatalf("an old article the feed never collected is classified by its classifier: %v / %v (%d calls)", rows["recOld"]["品类"], rows["recOld"]["国家"], classified)
	}
	for _, id := range []string{"recNoLabel", "recPending"} {
		if _, ok := rows[id]["品类"]; ok {
			t.Fatalf("%s should stay blank, got %v", id, rows[id]["品类"])
		}
	}
	if len(asked) != 1 || asked[0] != "MzA3ODEzMjg5Mg== 2026-09-28T16:00:00Z" {
		t.Fatalf("one call per account, from two days before the oldest row: %v", asked)
	}
	done, err := worker.Store.BizLabelRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"recHuman": archive.AuthorKept, "recBlank": archive.AuthorWritten, "recHalf": archive.AuthorWritten, "recNoLabel": archive.AuthorNone, "recOld": archive.AuthorWritten}
	if !reflect.DeepEqual(done, want) {
		t.Fatalf("states %v, want %v (recPending waits)", done, want)
	}

	// 有人把程序填的清空了：再跑不填回去；还没分类的那篇照样再问。
	rows["recBlank"]["品类"] = nil
	if _, err := worker.fillLabels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows["recBlank"]["品类"] != nil || len(asked) != 2 || classified != 1 {
		t.Fatalf("a cleared cell must not be filled again (品类=%v, calls %d / %d)", rows["recBlank"]["品类"], len(asked), classified)
	}
}

// fakeFeed 是资讯订阅：取标签的接口回 known 里的文章，分类接口按 answers 回标签（没有的就是模型没答），budget 为真时回 429。
type fakeFeed struct {
	t         *testing.T
	known     []map[string]interface{}
	answers   map[string][]string
	budget    bool
	noAccount bool
	requests  [][]classifyArticle
}

func (f *fakeFeed) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/internal/wechat-article-labels/classify" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"articles": f.known})
		return
	}
	var body struct {
		Biz      string            `json:"biz"`
		Articles []classifyArticle `json:"articles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Biz != "MzA3ODEzMjg5Mg==" || len(body.Articles) > classifyBatch {
		f.t.Errorf("bad classify request %v %+v", err, body)
	}
	f.requests = append(f.requests, body.Articles)
	if f.budget {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if f.noAccount {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var out []map[string]interface{}
	for _, article := range body.Articles {
		if labels, ok := f.answers[article.Key]; ok {
			out = append(out, map[string]interface{}{"key": article.Key, "labels": labels})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"articles": out})
}

func newLabelWorker(t *testing.T) (*Worker, map[string]map[string]interface{}, *fakePages, *fakeFeed) {
	worker, base, pages := newAuthorWorker(t)
	feed := &fakeFeed{t: t, answers: map[string][]string{}}
	server := httptest.NewServer(http.HandlerFunc(feed.serve))
	t.Cleanup(server.Close)
	worker.FeedURL, worker.FeedToken = server.URL, "feed-token"
	return worker, base.table("tblArticles"), pages, feed
}

// 资讯订阅没收、或收了没分类的旧文章：正文先用归档，没有才读网页，递给资讯订阅的分类接口，只填空格子。
func TestLabelsTheFeedNeverClassifiedGoThroughItsClassifier(t *testing.T) {
	ctx := context.Background()
	worker, rows, pages, feed := newLabelWorker(t)
	feed.known = []map[string]interface{}{{"mid": "700", "idx": "1", "classified": false, "labels": []string{}}}
	feed.answers = map[string][]string{"600_1": {"food.dairy"}, "700_1": {"retail", "food", "country.cn"}, "990_1": {"food.beverage"}}
	archived := `{"total_count":1,"item_count":1,"item":[{"article_id":"a-600","update_time":100,"content":{"news_item":[{"title":"归档的","content":"<p>归档正文</p>","url":"` + articleLink("600", "1") + `"}]}}]}`
	if _, err := worker.Store.SaveContentPage(ctx, "freepublish", []byte(archived), worker.now()); err != nil {
		t.Fatal(err)
	}
	rows["recArchived"] = map[string]interface{}{"内容标题": titled("600", "1", "归档的"), "发表日期": dayMs(2026, 10, 30)}
	rows["recDup"] = map[string]interface{}{"内容标题": titled("600", "1", "归档的"), "发表日期": dayMs(2026, 10, 30), "国家": []interface{}{"中国"}}
	rows["recUnclassified"] = map[string]interface{}{"内容标题": titled("700", "1", "收了没分类"), "发表日期": dayMs(2026, 10, 29)}
	rows["recGone"] = map[string]interface{}{"内容标题": titled("800", "1", "删了的"), "发表日期": dayMs(2026, 10, 28)}
	rows["recMissing"] = map[string]interface{}{"内容标题": titled("900", "1", "模型不答"), "发表日期": dayMs(2026, 10, 27)}
	rows["recNew"] = map[string]interface{}{"内容标题": titled("950", "1", "昨天发的"), "发表日期": dayMs(2026, 11, 5)}
	rows["recLegacy"] = map[string]interface{}{"内容标题": titled("990", "1", "旧版记成没有这篇"), "发表日期": dayMs(2015, 3, 1)}
	if err := worker.Store.SaveBizLabels(ctx, "recLegacy", "990_1", archive.AuthorNone, "", "", "资讯订阅没有这篇", worker.now()); err != nil {
		t.Fatal(err)
	}
	pages.pages[articleLink("700", "1")] = originalPage(`<p>网页正文</p>`)
	pages.errs[articleLink("800", "1")] = errPageGone
	pages.pages[articleLink("900", "1")] = originalPage(`<p>别的</p>`)
	pages.pages[articleLink("990", "1")] = originalPage(`<p>饮料</p>`)

	if wait := worker.labelRound(ctx, 6*time.Hour); wait != labelBacklogPause {
		t.Fatalf("a row to retry comes back in %s, got %s", labelBacklogPause, wait)
	}
	if len(feed.requests) != 1 {
		t.Fatalf("one batch for this account, got %d", len(feed.requests))
	}
	var keys []string
	for _, article := range feed.requests[0] {
		keys = append(keys, article.Key)
		switch article.Key {
		case "600_1":
			if article.ContentHTML != "<p>归档正文</p>" || article.Title != "归档的" {
				t.Fatalf("archived body is used: %+v", article)
			}
		case "700_1":
			if !strings.HasPrefix(article.ContentHTML, "<p>网页正文</p>") {
				t.Fatalf("the page body is sent as HTML: %+v", article)
			}
		}
	}
	if strings.Join(keys, " ") != "600_1 700_1 900_1 990_1" {
		t.Fatalf("newest first, each article once, nothing recent or deleted: %v", keys)
	}
	if strings.Join(pages.fetched, " ") != strings.Join([]string{articleLink("700", "1"), articleLink("800", "1"), articleLink("900", "1"), articleLink("990", "1")}, " ") {
		t.Fatalf("pages are read only without an archived body: %v", pages.fetched)
	}
	if !reflect.DeepEqual(options(rows["recArchived"]["品类"]), []string{"乳制品"}) || !reflect.DeepEqual(options(rows["recDup"]["品类"]), []string{"乳制品"}) || !reflect.DeepEqual(options(rows["recDup"]["国家"]), []string{"中国"}) {
		t.Fatalf("both rows of one article filled, human cell kept: %v / %v / %v", rows["recArchived"]["品类"], rows["recDup"]["品类"], rows["recDup"]["国家"])
	}
	if !reflect.DeepEqual(options(rows["recUnclassified"]["品类"]), []string{"食品", "零售"}) || !reflect.DeepEqual(options(rows["recUnclassified"]["国家"]), []string{"中国"}) {
		t.Fatalf("got %v / %v", rows["recUnclassified"]["品类"], rows["recUnclassified"]["国家"])
	}
	if !reflect.DeepEqual(options(rows["recLegacy"]["品类"]), []string{"饮料"}) {
		t.Fatalf("rows the old version gave up on are asked again: %v", rows["recLegacy"]["品类"])
	}
	for _, id := range []string{"recGone", "recMissing", "recNew"} {
		if _, ok := rows[id]["品类"]; ok {
			t.Fatalf("%s stays blank, got %v", id, rows[id]["品类"])
		}
	}

	// 模型连着 3 次没答这篇才留空。
	for i := 0; i < 2; i++ {
		worker.labelRound(ctx, 6*time.Hour)
	}
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != labelRetry {
		t.Fatalf("only the recent article is left to ask about, got %s", wait)
	}
	done, err := worker.Store.BizLabelRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"recArchived": archive.AuthorWritten, "recDup": archive.AuthorWritten, "recUnclassified": archive.AuthorWritten,
		"recGone": archive.AuthorNone, "recMissing": archive.AuthorNone, "recLegacy": archive.AuthorWritten}
	if !reflect.DeepEqual(done, want) {
		t.Fatalf("states %v, want %v", done, want)
	}
	if len(feed.requests) != 3 {
		t.Fatalf("only the unanswered one is sent again, got %d requests", len(feed.requests))
	}
}

// 读网页一轮最多 labelPagesPerRound 篇；被微信要求验证整轮停、不记到行上；资讯订阅额度用完就等到明天，不再读网页。
func TestLabelBackfillPacesPagesAndStops(t *testing.T) {
	ctx := context.Background()
	worker, rows, pages, feed := newLabelWorker(t)
	for i := 0; i < labelPagesPerRound+5; i++ {
		mid := strconv.Itoa(1000 + i)
		rows["rec"+mid] = map[string]interface{}{"内容标题": titled(mid, "1", "快讯"+mid), "发表日期": dayMs(2026, 9, 1)}
		pages.pages[articleLink(mid, "1")] = originalPage(`<p>正文</p>`)
		feed.answers[mid+"_1"] = []string{"food"}
	}
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != labelBacklogPause || len(pages.fetched) != labelPagesPerRound {
		t.Fatalf("one round reads at most %d pages (read %d), then waits %s, got %s", labelPagesPerRound, len(pages.fetched), labelBacklogPause, wait)
	}
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != 0 || len(pages.fetched) != labelPagesPerRound+5 {
		t.Fatalf("the rest next round, then wait for new rows: read %d, wait %s", len(pages.fetched), wait)
	}

	var alerts []string
	worker.Alert = func(message string) { alerts = append(alerts, message) }
	for _, mid := range []string{"2001", "2002"} {
		rows["rec"+mid] = map[string]interface{}{"内容标题": titled(mid, "1", "快讯"+mid), "发表日期": dayMs(2026, 9, 2)}
		pages.errs[articleLink(mid, "1")] = errVerification
		feed.answers[mid+"_1"] = []string{"food"}
	}
	rows["rec2003"] = map[string]interface{}{"内容标题": titled("2003", "1", "归档有正文"), "发表日期": dayMs(2026, 9, 1)}
	feed.answers["2003_1"] = []string{"food.dairy"}
	archived := `{"total_count":1,"item_count":1,"item":[{"article_id":"a-2003","update_time":100,"content":{"news_item":[{"title":"归档有正文","content":"<p>奶</p>","url":"` + articleLink("2003", "1") + `"}]}}]}`
	if _, err := worker.Store.SaveContentPage(ctx, "freepublish", []byte(archived), worker.now()); err != nil {
		t.Fatal(err)
	}
	fetched := len(pages.fetched)
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != time.Minute || len(pages.fetched) != fetched+1 || len(alerts) != 0 {
		t.Fatalf("verification stops page reads at once and backs off: wait %s, read %d, alerts %v", wait, len(pages.fetched)-fetched, alerts)
	}
	if !reflect.DeepEqual(options(rows["rec2003"]["品类"]), []string{"乳制品"}) {
		t.Fatalf("an archived body needs no page, so it is filled anyway: %v", rows["rec2003"]["品类"])
	}
	done, err := worker.Store.BizLabelRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if done["rec2001"] != "" || done["rec2002"] != "" {
		t.Fatalf("no row is given up while WeChat blocks the server: %v", done)
	}

	for _, mid := range []string{"2001", "2002"} {
		delete(pages.errs, articleLink(mid, "1"))
		pages.pages[articleLink(mid, "1")] = originalPage(`<p>正文</p>`)
	}
	feed.budget = true
	fetched = len(pages.fetched)
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != labelRetry {
		t.Fatalf("budget used up waits like pending, got %s", wait)
	}
	read := len(pages.fetched) - fetched
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != labelRetry || len(pages.fetched)-fetched != read {
		t.Fatalf("no more pages read until tomorrow: wait %s, read %d then %d", wait, read, len(pages.fetched)-fetched)
	}
	if done, _ := worker.Store.BizLabelRecords(ctx); done["rec2001"] != "" {
		t.Fatalf("rows wait for tomorrow's budget: %v", done)
	}
	worker.Now = func() time.Time { return time.Date(2026, 11, 7, 0, 10, 0, 0, wechat.ShanghaiLoc()) }
	feed.budget = false
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != 0 || !reflect.DeepEqual(options(rows["rec2001"]["品类"]), []string{"食品"}) {
		t.Fatalf("next day the rest is filled: wait %s, %v", wait, rows["rec2001"]["品类"])
	}
}

func TestLabelOptionsFollowFeedTaxonomy(t *testing.T) {
	categories, countries := labelOptions([]string{"finance.ma", "food", "innovation", "country.jp", "retail", "country.us"})
	if !reflect.DeepEqual(categories, []string{"食品", "零售", "并购"}) || !reflect.DeepEqual(countries, []string{"日本"}) {
		t.Fatalf("got %v / %v", categories, countries)
	}
}

// 资讯订阅不收的号它分不了：第一批递过去回 404 就整号留空，不再接着读网页。
func TestLabelsLeaveAccountsTheFeedDoesNotCollectBlank(t *testing.T) {
	ctx := context.Background()
	worker, rows, pages, feed := newLabelWorker(t)
	feed.noAccount = true
	for i := 0; i < classifyBatch+3; i++ {
		mid := strconv.Itoa(3000 + i)
		rows["rec"+mid] = map[string]interface{}{"内容标题": titled(mid, "1", "别的号"+mid), "发表日期": dayMs(2026, 9, 1)}
		pages.pages[articleLink(mid, "1")] = originalPage(`<p>正文</p>`)
	}
	if wait := worker.labelRound(ctx, 6*time.Hour); wait != 0 {
		t.Fatalf("nothing left to do, got %s", wait)
	}
	if len(pages.fetched) != classifyBatch || len(feed.requests) != 1 {
		t.Fatalf("stop reading after the first refused batch: read %d, sent %d", len(pages.fetched), len(feed.requests))
	}
	done, err := worker.Store.BizLabelRecords(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != classifyBatch+3 {
		t.Fatalf("all rows of the account are settled blank: %v", done)
	}
	for id, status := range done {
		if status != archive.AuthorNone {
			t.Fatalf("%s should stay blank, got %s", id, status)
		}
	}
}
