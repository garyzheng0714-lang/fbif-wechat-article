package bizdata

import (
	"context"
	"testing"
)

// GetOrCreateTable 按名字找表，没有就建：「公众号」表固定叫 tblAccount。
func (f *fakeBase) GetOrCreateTable(name string, _ ...string) (string, error) {
	f.calls["accountTable"]++
	if f.fail != nil {
		return "", f.fail
	}
	if name != accountTableName {
		return "", nil
	}
	f.table("tblAccount")
	return "tblAccount", nil
}

var _ BaseTarget = (*fakeBase)(nil)

func linkedIDs(value interface{}) []string {
	var ids []string
	items, _ := value.([]interface{})
	for _, item := range items {
		if id, ok := item.(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// 老表搬进来的行、有人手贴的行，只要链接对得上就认领，不再建一行重复的。
func TestRowsAddedAfterFirstAdoptionAreAdoptedByLink(t *testing.T) {
	worker, store, base := newWorker(t)
	articles := base.table("tblArticles")
	articles["recNew"] = map[string]interface{}{"内容标题": link("900", "1"), "阅读人数": 1.0, "等级": "　"}
	save(t, store, "getarticletotaldetail", "2026-06-12", article("900_1", "2026-06-12", 0, stat("2026-06-12", 10, 0, 0)))
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 之后有人把 2025-12 的老文章搬进表里，接口里也有这篇。
	articles["recMoved"] = map[string]interface{}{"内容标题": link("800", "2"), "阅读人数": 5000.0, "等级": "　", "作者": "Sage"}
	save(t, store, "getarticletotaldetail", "2025-12-03", article("800_2", "2025-12-03", 0, stat("2026-01-01", 12000, 0, 0)))
	result, err := worker.syncBase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Articles.Adopted != 1 || result.Articles.Created != 0 || len(articles) != 2 {
		t.Fatalf("moved row must be adopted, not duplicated: %+v rows=%d", result.Articles, len(articles))
	}
	if moved := articles["recMoved"]; moved["阅读人数"] != 12000.0 || moved["等级"] != "1万" || moved["作者"] != "Sage" {
		t.Fatalf("adopted row should get latest numbers and keep human columns, got %v", moved)
	}
}

// 文章表和每日数据表每行都关联到「公众号」表里那个号；号的行只建一次，之后不再查。
func TestArticleAndDayRowsLinkToAccount(t *testing.T) {
	worker, store, base := newWorker(t)
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-12", 10, 0, 0)))
	seedDay(t, store, "2026-11-05", 19388, 5)
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}

	accounts := base.table("tblAccount")
	if len(accounts) != 1 {
		t.Fatalf("one account row expected, got %v", accounts)
	}
	var accountID string
	for id, row := range accounts {
		accountID = id
		if row["biz"] != "MzA3ODEzMjg5Mg==" {
			t.Fatalf("account row should carry the biz, got %v", row)
		}
	}
	for _, table := range []string{"tblArticles", "tblDaily"} {
		for _, row := range base.table(table) {
			if ids := linkedIDs(row["公众号"]); len(ids) != 1 || ids[0] != accountID {
				t.Fatalf("%s row must link to the account, got %v", table, row["公众号"])
			}
		}
	}

	before := base.totalCalls()
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}
	if base.totalCalls() != before {
		t.Fatalf("account is remembered; nothing changed but Feishu was called %d more times", base.totalCalls()-before)
	}
}

// 「公众号」表里已经有这个号（人建的，带名称）就直接用，不再建一行。
func TestExistingAccountRowIsReused(t *testing.T) {
	worker, store, base := newWorker(t)
	base.table("tblAccount")["recFBIF"] = map[string]interface{}{
		"名称":  []interface{}{map[string]interface{}{"text": "FBIF食品饮料创新", "type": "text"}},
		"biz": []interface{}{map[string]interface{}{"text": "MzA3ODEzMjg5Mg==", "type": "text"}},
	}
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-12", 10, 0, 0)))
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(base.table("tblAccount")) != 1 {
		t.Fatalf("existing account row must be reused, got %v", base.table("tblAccount"))
	}
	for _, row := range base.table("tblArticles") {
		if ids := linkedIDs(row["公众号"]); len(ids) != 1 || ids[0] != "recFBIF" {
			t.Fatalf("article must link to the existing account row, got %v", row["公众号"])
		}
	}
}

func TestBizFromLink(t *testing.T) {
	for in, want := range map[string]string{
		"http://mp.weixin.qq.com/s?__biz=MzA3ODEzMjg5Mg==&mid=1&idx=1#rd":   "MzA3ODEzMjg5Mg==",
		"https://mp.weixin.qq.com/s?__biz=MzA3ODEzMjg5Mg%3D%3D&mid=1&idx=1": "MzA3ODEzMjg5Mg==",
		"https://example.com/a": "",
	} {
		if got := bizFromLink(in); got != want {
			t.Fatalf("bizFromLink(%q) = %q, want %q", in, got, want)
		}
	}
}
