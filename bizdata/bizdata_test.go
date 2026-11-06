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
	articles["recA"] = map[string]interface{}{"内容url": link("100", "1"), "阅读人数": 5843.0, "等级": "　", "作者": "　"}
	articles["recB"] = map[string]interface{}{"内容url": link("200", "1"), "阅读人数": 12000.0, "等级": "10万", "作者": "Mote莫特"}

	save(t, store, "getarticletotaldetail", "2026-06-12",
		article("100_1", "2026-06-12", 0, stat("2026-06-12", 5843, 4000, 0.01), stat("2026-07-11", 26301, 8742, 0.0135791515931)),
		article("200_1", "2026-06-12", 0, stat("2026-07-11", 15000, 100, 0.001)),
		article("300_1", "2026-06-12", 0, stat("2026-07-11", 9000, 0, 0)),
		article("400_1", "2026-06-12", 1, stat("2026-07-11", 500, 0, 0)), // 只发表不群发，不进表
	)
	save(t, store, "getarticletotaldetail", "2026-03-01", article("500_1", "2026-03-01", 0, stat("2026-03-02", 1, 0, 0))) // 早于接管范围

	result, err := worker.syncBase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Articles.Adopted != 2 || result.Articles.Updated != 2 || result.Articles.Created != 1 {
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
	if len(articles) != 3 {
		t.Fatalf("only the群发 article 300_1 is new, table has %d rows", len(articles))
	}
	for id, row := range articles {
		if id != "recA" && id != "recB" && (row["等级"] != "　" || row["发表日期"] != 20260612.0 || row["送达人数"] != 0.0) {
			t.Fatalf("new row fields wrong: %v", row)
		}
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
	articles["recA"] = map[string]interface{}{"内容url": link("100", "1"), "阅读人数": 1.0, "等级": "　"}
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

func TestDailyCardWaitsForDataAndIsSentOnce(t *testing.T) {
	worker, store, _ := newWorker(t)
	worker.ChatID = "oc_test"
	var sent []map[string]interface{}
	var uuids []string
	worker.Send = func(_ string, card interface{}, uuid string) (string, error) {
		sent = append(sent, card.(map[string]interface{}))
		uuids = append(uuids, uuid)
		return "om_1", nil
	}
	seedDay(t, store, "2026-11-04", 15000, 4)
	seedDay(t, store, "2026-11-05", 19388, 5)
	yesterday := []interface{}{
		article("2652943556_2", "2026-11-05", 0, stat("2026-11-05", 1371, 100, 0.001)),
		article("2652943556_1", "2026-11-05", 0, stat("2026-11-05", 10927, 8742, 0.0135791515931)),
	}
	// 微信先回了 is_delay：昨天的文章数据还没出完，不能发。
	saveFetch(t, store, "getarticletotaldetail", "2026-11-05", true, yesterday...)
	save(t, store, "getarticletotaldetail", "2026-11-01", article("2652943000_1", "2026-11-01", 0, stat("2026-11-05", 30000, 0, 0)))
	save(t, store, "getarticletotaldetail", "2026-10-29", article("2652942000_1", "2026-10-29", 0, stat("2026-11-05", 99999, 0, 0))) // 早于近 7 天

	result, err := worker.sendReports(context.Background())
	if err != nil || len(sent) != 0 || result.Waiting != "2026-11-05" {
		t.Fatalf("must wait until yesterday is final: err=%v sent=%d result=%+v", err, len(sent), result)
	}
	save(t, store, "getarticletotaldetail", "2026-11-05", yesterday...)
	// 历史回填接着写了很早的窗口，不能把「昨天已采完」盖掉。
	if err := store.MarkSuccess(context.Background(), "getusersummary", "test", "2025-12-01", "2025-12-07", "2025-11-30", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.sendReports(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.sendReports(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || uuids[0] != "wxdata-daily:2026-11-05" {
		t.Fatalf("daily card must be sent exactly once, got %d %v", len(sent), uuids)
	}
	encoded, _ := json.Marshal(sent[0])
	text := string(encoded)
	for _, want := range []string{`"template":"wathet"`, `"content":"公众号日报"`, "11月5日 周四", "19,388", "↑ 29.3%", "+60", "新增 70 取关 10", "645,251",
		"interactive_container", "近 7 天发文 3 篇", `"tag":"table"`, "打开数据表"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "标题2652942000_1") {
		t.Fatalf("articles older than 7 days must not be listed: %s", text)
	}
	first, second, third := strings.Index(text, "标题2652943556_1"), strings.Index(text, "标题2652943556_2"), strings.Index(text, "标题2652943000_1")
	if first < 0 || !(first < second && second < third) {
		t.Fatalf("articles must be newest first, lead article first: %d %d %d", first, second, third)
	}
}

func TestWeeklyCardOnlyOnMonday(t *testing.T) {
	worker, store, _ := newWorker(t)
	worker.ChatID = "oc_test"
	var keys []string
	worker.Send = func(_ string, _ interface{}, uuid string) (string, error) {
		keys = append(keys, uuid)
		return "om", nil
	}
	for day := 26; day <= 31; day++ {
		seedDay(t, store, fmt.Sprintf("2026-10-%02d", day), 10000, 3)
	}
	for day := 1; day <= 8; day++ {
		seedDay(t, store, fmt.Sprintf("2026-11-%02d", day), 12000, 3)
	}
	save(t, store, "getarticletotaldetail", "2026-11-08")
	worker.Now = func() time.Time { return time.Date(2026, 11, 9, 8, 10, 0, 0, wechat.ShanghaiLoc()) } // 周一
	if _, err := worker.sendReports(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, ",") != "wxdata-daily:2026-11-08,wxdata-weekly:2026-11-02" {
		t.Fatalf("Monday sends daily + last week's weekly, got %v", keys)
	}
	card, err := worker.WeeklyCard(context.Background(), time.Date(2026, 11, 2, 0, 0, 0, 0, wechat.ShanghaiLoc()))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(card)
	if text := string(encoded); !strings.Contains(text, "11月2日–11月8日") || !strings.Contains(text, "84,000") || !strings.Contains(text, "↑ 16.7%") {
		t.Fatalf("weekly totals wrong: %s", encoded)
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
