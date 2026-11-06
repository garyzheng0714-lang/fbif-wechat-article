package bizdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	worker, base, _ := newAuthorWorker(t)
	var asked []string
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/wechat-article-labels" || r.Header.Get("Authorization") != "Bearer feed-token" {
			t.Errorf("unexpected call %s %q", r.URL.Path, r.Header.Get("Authorization"))
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
	for _, id := range []string{"recNoLabel", "recPending", "recOld"} {
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
	want := map[string]string{"recHuman": archive.AuthorKept, "recBlank": archive.AuthorWritten, "recHalf": archive.AuthorWritten, "recNoLabel": archive.AuthorNone, "recOld": archive.AuthorNone}
	if !reflect.DeepEqual(done, want) {
		t.Fatalf("states %v, want %v (recPending waits)", done, want)
	}

	// 有人把程序填的清空了：再跑不填回去；还没分类的那篇照样再问。
	rows["recBlank"]["品类"] = nil
	if _, err := worker.fillLabels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows["recBlank"]["品类"] != nil || len(asked) != 2 {
		t.Fatalf("a cleared cell must not be filled again (品类=%v, calls %d)", rows["recBlank"]["品类"], len(asked))
	}
}

func TestLabelOptionsFollowFeedTaxonomy(t *testing.T) {
	categories, countries := labelOptions([]string{"finance.ma", "food", "innovation", "country.jp", "retail", "country.us"})
	if !reflect.DeepEqual(categories, []string{"食品", "零售", "并购"}) || !reflect.DeepEqual(countries, []string{"日本"}) {
		t.Fatalf("got %v / %v", categories, countries)
	}
}
