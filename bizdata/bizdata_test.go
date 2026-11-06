package bizdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// fakeBase 是内存里的多维表格，记下每种调用的次数。
type fakeBase struct {
	tables  map[string]map[string]map[string]interface{} // tableID → recordID → fields
	calls   map[string]int
	nextID  int
	created []string
	fail    error
}

func newFakeBase() *fakeBase {
	return &fakeBase{tables: map[string]map[string]map[string]interface{}{}, calls: map[string]int{}}
}

func (f *fakeBase) table(id string) map[string]map[string]interface{} {
	if f.tables[id] == nil {
		f.tables[id] = map[string]map[string]interface{}{}
	}
	return f.tables[id]
}

func (f *fakeBase) totalCalls() int {
	total := 0
	for _, n := range f.calls {
		total += n
	}
	return total
}

func (f *fakeBase) SearchRecords(tableID string, _ []string) ([]feishu.Record, error) {
	f.calls["search"]++
	if f.fail != nil {
		return nil, f.fail
	}
	records := make([]feishu.Record, 0)
	for id, fields := range f.table(tableID) {
		records = append(records, feishu.Record{RecordID: id, Fields: fields})
	}
	return records, nil
}

func (f *fakeBase) BatchGetRecords(tableID string, ids []string) ([]feishu.Record, []string, error) {
	f.calls["get"]++
	var records []feishu.Record
	var absent []string
	for _, id := range ids {
		if fields, ok := f.table(tableID)[id]; ok {
			records = append(records, feishu.Record{RecordID: id, Fields: fields})
		} else {
			absent = append(absent, id)
		}
	}
	return records, absent, nil
}

func (f *fakeBase) BatchCreateByRecordFieldsWithIDs(tableID string, records []map[string]interface{}) ([]string, error) {
	f.calls["create"]++
	ids := make([]string, 0, len(records))
	for _, record := range records {
		f.nextID++
		id := fmt.Sprintf("recnew%d", f.nextID)
		f.table(tableID)[id] = roundTrip(record["fields"])
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *fakeBase) BatchUpdateByRecordID(tableID string, records []map[string]interface{}) error {
	f.calls["update"]++
	for _, record := range records {
		row := f.table(tableID)[record["record_id"].(string)]
		for name, value := range roundTrip(record["fields"]) {
			row[name] = value
		}
	}
	return nil
}

func (f *fakeBase) EnsureFieldsExist([]feishu.FieldSpec, string) error {
	f.calls["fields"]++
	return nil
}

func (f *fakeBase) CreateTable(name string, _ []feishu.FieldSpec) (string, error) {
	f.calls["table"]++
	f.created = append(f.created, name)
	return "tblDaily", nil
}

// roundTrip 让值和飞书读回来的一样（数字都是 float64）。
func roundTrip(value interface{}) map[string]interface{} {
	encoded, _ := json.Marshal(value)
	var fields map[string]interface{}
	_ = json.Unmarshal(encoded, &fields)
	return fields
}

func link(mid, idx string) map[string]interface{} {
	url := "http://mp.weixin.qq.com/s?__biz=MzA3ODEzMjg5Mg==&mid=" + mid + "&idx=" + idx + "&sn=abc#rd"
	return map[string]interface{}{"link": url, "text": url, "type": "url"}
}

func save(t *testing.T, store *archive.Store, endpoint, date string, list ...interface{}) {
	t.Helper()
	saveFetch(t, store, endpoint, date, false, list...)
}

// saveFetch 照采集器的样子留一次请求；deferred 是微信回了 is_delay（数据还没出完）。
func saveFetch(t *testing.T, store *archive.Store, endpoint, date string, deferred bool, list ...interface{}) {
	t.Helper()
	response, _ := json.Marshal(map[string]interface{}{"list": list})
	if _, err := store.SaveFetch(context.Background(), archive.FetchRecord{
		Endpoint: endpoint, Category: "test", BeginDate: date, EndDate: date,
		RequestJSON: []byte(`{}`), ResponseJSON: response, HTTPStatus: 200, Success: true, Deferred: deferred,
		FetchedAt: time.Date(2026, 11, 6, 8, 6, 0, 0, wechat.ShanghaiLoc()),
	}); err != nil {
		t.Fatal(err)
	}
}

func article(msgid, publishDate string, publishType int, stats ...map[string]interface{}) map[string]interface{} {
	mid, idx, _ := strings.Cut(msgid, "_")
	return map[string]interface{}{
		"ref_date": publishDate, "msgid": msgid, "publish_type": publishType, "title": "标题" + msgid,
		"content_url": "http://mp.weixin.qq.com/s?__biz=MzA3ODEzMjg5Mg==&mid=" + mid + "&idx=" + idx + "&sn=abc#rd",
		"detail_list": stats,
	}
}

func stat(date string, reads int64, messageReads int64, deliveryRate float64) map[string]interface{} {
	return map[string]interface{}{
		"stat_date": date, "read_user": reads, "share_user": reads / 10, "read_subscribe_user": 3,
		"read_delivery_rate": deliveryRate, "read_finish_rate": 0.12, "zaikan_user": 4, "like_user": 5,
		"collection_user": 6, "comment_count": 7,
		"read_user_source": []interface{}{map[string]interface{}{"scene_desc": "公众号消息", "user_count": messageReads}},
	}
}

func newWorker(t *testing.T) (*Worker, *archive.Store, *fakeBase) {
	t.Helper()
	store, err := archive.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	base := newFakeBase()
	worker := &Worker{
		Store: store, Base: base, BaseToken: "bascn", ArticleTableID: "tblArticles",
		Now: func() time.Time { return time.Date(2026, 11, 6, 8, 10, 0, 0, wechat.ShanghaiLoc()) },
	}
	return worker, store, base
}

func TestGradeMatchesImportedTable(t *testing.T) {
	for reads, want := range map[int64]string{233: "　", 9810: "　", 10054: "1万", 19092: "1万", 98730: "9万", 105959: "10万", 177913: "10万"} {
		if got := Grade(reads); got != want {
			t.Fatalf("Grade(%d) = %q, want %q", reads, got, want)
		}
	}
}

func TestAdoptsExistingTableUpdatesOnlyMetricsAndLeavesHumanColumns(t *testing.T) {
	worker, store, base := newWorker(t)
	articles := base.table("tblArticles")
	// 导入时的旧快照：等级按阅读数算的档；另一行有人手改了等级、填了作者。
	articles["recA"] = map[string]interface{}{"内容标题": link("100", "1"), "阅读人数": 5843.0, "等级": "　", "作者": "　"}
	articles["recB"] = map[string]interface{}{"内容标题": link("200", "1"), "阅读人数": 12000.0, "等级": "10万", "作者": "Mote莫特"}

	save(t, store, "getarticletotaldetail", "2026-06-12",
		article("100_1", "2026-06-12", 0, stat("2026-06-12", 5843, 4000, 0.01), stat("2026-07-11", 26301, 8742, 0.0135791515931)),
		article("200_1", "2026-06-12", 0, stat("2026-07-11", 15000, 100, 0.001)),
		article("300_1", "2026-06-12", 0, stat("2026-07-11", 9000, 0, 0)),
		article("400_1", "2026-06-12", 1, stat("2026-07-11", 500, 0, 0)), // 只发表、没通知粉丝，也进表
	)
	save(t, store, "getarticletotaldetail", "2025-10-31", article("500_1", "2025-10-31", 0, stat("2025-11-01", 1, 0, 0))) // 早于接口最早有数的那天

	result, err := worker.syncBase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Articles.Adopted != 2 || result.Articles.Updated != 2 || result.Articles.Created != 2 {
		t.Fatalf("unexpected result: %+v", result.Articles)
	}
	a := articles["recA"]
	if a["阅读人数"] != 26301.0 || a["等级"] != "2万" || a["作者"] != "　" {
		t.Fatalf("recA should get latest 30-day value and follow grade, got %v", a)
	}
	if got := a["送达人数"].(float64); got != 643781 {
		t.Fatalf("送达人数 = 公众号消息阅读 / 送达率, got %v", got)
	}
	b := articles["recB"]
	if b["阅读人数"] != 15000.0 || b["等级"] != "10万" || b["作者"] != "Mote莫特" {
		t.Fatalf("hand-set 等级 and 作者 must stay, got %v", b)
	}
	if len(articles) != 4 {
		t.Fatalf("300_1 and 400_1 are new, table has %d rows", len(articles))
	}
	june12 := float64(time.Date(2026, 6, 12, 0, 0, 0, 0, wechat.ShanghaiLoc()).UnixMilli())
	kinds := map[string]int{}
	for id, row := range articles {
		if id == "recA" || id == "recB" {
			continue
		}
		title, _ := row["内容标题"].(map[string]interface{})
		if row["等级"] != "　" || row["发表日期"] != june12 || row["送达人数"] != 0.0 || msgIDFromLink(title) == "" || title["text"] == "" {
			t.Fatalf("new row fields wrong: %v", row)
		}
		kinds[row["发表方式"].(string)]++
	}
	if kinds["群发"] != 1 || kinds["仅发表"] != 1 {
		t.Fatalf("发表方式 should tell 群发 from 仅发表, got %v", kinds)
	}

	before := base.totalCalls()
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}
	if base.totalCalls() != before {
		t.Fatalf("nothing changed, but Feishu was called %d more times", base.totalCalls()-before)
	}
}

func TestRowDeletedInTableIsNotRecreated(t *testing.T) {
	worker, store, base := newWorker(t)
	articles := base.table("tblArticles")
	articles["recA"] = map[string]interface{}{"内容标题": link("100", "1"), "阅读人数": 1.0, "等级": "　"}
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-12", 10, 0, 0)))
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}

	delete(articles, "recA") // 有人把这行删了
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-13", 20, 0, 0)))
	result, err := worker.syncBase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Articles.Skipped != 1 || len(articles) != 0 {
		t.Fatalf("deleted row must stay deleted: %+v, rows=%d", result.Articles, len(articles))
	}
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-14", 30, 0, 0)))
	if _, err := worker.syncBase(context.Background()); err != nil || len(articles) != 0 {
		t.Fatalf("deleted row came back: err=%v rows=%d", err, len(articles))
	}
}

func seedDay(t *testing.T, store *archive.Store, date string, reads int64, pages int64) {
	t.Helper()
	save(t, store, "getbizsummary", date, map[string]interface{}{"ref_date": date, "detail": map[string]interface{}{
		"read_user": reads, "share_user": reads / 15, "zaikan_user": 1, "like_user": 2, "comment_count": 3,
		"collection_user": 4, "send_page_count": pages,
		"read_user_source": []interface{}{map[string]interface{}{"scene_desc": "推荐", "user_count": 100}},
	}})
	save(t, store, "getusersummary", date,
		map[string]interface{}{"ref_date": date, "user_source": 0, "new_user": 50, "cancel_user": 10},
		map[string]interface{}{"ref_date": date, "user_source": 30, "new_user": 20, "cancel_user": 0})
	save(t, store, "getusercumulate", date, map[string]interface{}{"ref_date": date, "user_source": 0, "cumulate_user": 645251})
}

func TestDailyTableIsCreatedOnceAndFilled(t *testing.T) {
	worker, store, base := newWorker(t)
	seedDay(t, store, "2026-11-05", 19388, 5)
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(base.created) != 1 || base.created[0] != dailyTableName {
		t.Fatalf("daily table should be created once, got %v", base.created)
	}
	var row map[string]interface{}
	for _, r := range base.table("tblDaily") {
		row = r
	}
	if row["阅读人数"] != 19388.0 || row["净增关注"] != 60.0 || row["累计关注"] != 645251.0 || row["推荐阅读"] != 100.0 {
		t.Fatalf("daily row wrong: %v", row)
	}
	if _, err := worker.syncBase(context.Background()); err != nil || len(base.created) != 1 {
		t.Fatalf("second round must reuse the table: err=%v created=%v", err, base.created)
	}
}

func TestAlertsOnceOnThirdFailureAndBacksOff(t *testing.T) {
	worker, store, base := newWorker(t)
	base.fail = errors.New("feishu 99991663")
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-12", 10, 0, 0)))
	var alerts []string
	worker.Alert = func(message string) { alerts = append(alerts, message) }
	fallback := 6 * time.Hour
	var waits []time.Duration
	for i := 0; i < 5; i++ {
		waits = append(waits, worker.round(fallback))
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "连续 3 次失败") {
		t.Fatalf("must alert exactly once at the 3rd failure, got %v", alerts)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("backoff %d = %s, want %s", i, waits[i], want[i])
		}
	}
	base.fail = nil
	if wait := worker.round(fallback); wait != fallback || len(alerts) != 1 {
		t.Fatalf("recovery waits the fallback and does not alert again: wait=%s alerts=%d", wait, len(alerts))
	}
}

// 微信给的文章字段都写上：阅读来源、跳出位置、平均阅读时长、赞赏、第几条、数据截至哪天。
func TestArticleRowCarriesEveryWeChatField(t *testing.T) {
	worker, store, base := newWorker(t)
	detail := stat("2026-07-11", 408, 363, 0.000563823035918)
	detail["praise_money"] = 1250
	detail["read_avg_activetime"] = 0.380555123091
	detail["read_user_source"] = []interface{}{
		map[string]interface{}{"scene_desc": "公众号消息", "user_count": 363},
		map[string]interface{}{"scene_desc": "推荐", "user_count": 14},
		map[string]interface{}{"scene_desc": "搜一搜", "user_count": 1},
		map[string]interface{}{"scene_desc": "全部", "user_count": 408},
	}
	detail["read_jump_position"] = []interface{}{
		map[string]interface{}{"position": 3, "rate": 0.0105},
		map[string]interface{}{"position": 5, "rate": 0.9526},
	}
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_2", "2026-06-12", 0, detail))
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}
	var row map[string]interface{}
	for _, r := range base.table("tblArticles") {
		row = r
	}
	july11 := float64(time.Date(2026, 7, 11, 0, 0, 0, 0, wechat.ShanghaiLoc()).UnixMilli())
	want := map[string]interface{}{
		"第几条": 2.0, "赞赏金额": 12.5, "平均阅读秒数": 23.0, "数据截至": july11,
		"公众号消息阅读": 363.0, "推荐阅读": 14.0, "搜一搜阅读": 1.0, "朋友圈阅读": 0.0,
		"跳出0-20%": 0.0, "跳出40-60%": 0.0105, "跳出80-100%": 0.9526,
	}
	for name, value := range want {
		if row[name] != value {
			t.Fatalf("%s = %v, want %v (row %v)", name, row[name], value, row)
		}
	}
}

// 每日数据也带上点「阅读原文」的人数和新增关注按来源拆开；文档外的来源算进「其他关注」。
func TestDayRowSplitsFollowSources(t *testing.T) {
	worker, store, base := newWorker(t)
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-12", 10, 0, 0)))
	save(t, store, "getbizsummary", "2026-11-05", map[string]interface{}{"ref_date": "2026-11-05", "detail": map[string]interface{}{
		"read_user": 19388, "redirect_ori_page_user": 2, "send_page_count": 5,
	}})
	save(t, store, "getusersummary", "2026-11-05",
		map[string]interface{}{"ref_date": "2026-11-05", "user_source": 57, "new_user": 40, "cancel_user": 0},
		map[string]interface{}{"ref_date": "2026-11-05", "user_source": 1, "new_user": 15, "cancel_user": 0},
		map[string]interface{}{"ref_date": "2026-11-05", "user_source": 0, "new_user": 3, "cancel_user": 9},
		map[string]interface{}{"ref_date": "2026-11-05", "user_source": 999, "new_user": 2, "cancel_user": 0},
	)
	if _, err := worker.syncBase(context.Background()); err != nil {
		t.Fatal(err)
	}
	var row map[string]interface{}
	for _, r := range base.table("tblDaily") {
		row = r
	}
	want := map[string]interface{}{
		"阅读原文": 2.0, "新增关注": 60.0, "取消关注": 9.0, "文章内关注": 40.0, "搜索关注": 15.0, "其他关注": 5.0, "视频号关注": 0.0,
	}
	for name, value := range want {
		if row[name] != value {
			t.Fatalf("%s = %v, want %v (row %v)", name, row[name], value, row)
		}
	}
}
