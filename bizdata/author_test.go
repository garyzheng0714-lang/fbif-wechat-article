package bizdata

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
)

// 网页片段照真实文章页的写法：转载页有 original_primary_nickname，图片消息没有 js_content、正文在 content_noencode。
func originalPage(body string) string {
	return `<script>var copyright_stat = "1";</script><div class="rich_media_content" id="js_content" style="visibility: hidden;">` +
		body + `</div><script>var x=1;</script>`
}

const reprintPage = `<script>window.cgiData={copyright_stat: '2' * 1, source_username: 'gh_90bc6f3429f4'};</script>
<span class="original_primary_nickname">Gooods好味司</span>
<div id="js_content"><p>以下文章来源于 Gooods好味司</p><p>作者：某某</p></div><script></script>`

func TestPickAuthorFollowsRuleOrder(t *testing.T) {
	cases := []struct {
		name, title, page, want, rule string
	}{
		{"标题栏目优先", "FBIF创新周报｜本周新品", originalPage(`<p>编辑：Kiki</p>`), "创新周报", "标题"},
		{"自家活动标题", "Wow 食品探索家来了", originalPage(`<p>编辑：Kiki</p>`), "FBIF活动", "标题"},
		{"转载写原公众号名", "一杯高蛋白牛奶", reprintPage, "Gooods好味司", "转载"},
		{"开头作者署名", "东京香蕉", originalPage(`<p>作者：Mote莫特</p><p>正文</p><p>编辑：Kiki</p>`), "Mote莫特", "作者署名"},
		{"文｜署名也算作者", "标题", originalPage(`<section>文｜kiki</section><p>正文</p>`), "Kiki", "作者署名"},
		{"没有作者看编辑，统一大小写", "标题", originalPage(`<p>正文正文</p><p>编辑：dudu</p>`), "Dudu", "编辑署名"},
		{"署名按人工归类", "标题", originalPage(`<p>作者：Gloria</p><p>正文</p>`), "兼职", "作者署名"},
		{"没见过的署名照原样", "标题", originalPage(`<p>作者：舟舟</p><p>正文</p>`), "舟舟", "作者署名"},
		{"没声明原创看来源", "标题", `<script>copyright_stat: '0'</script><div id="js_content"><p>正文</p><p>来源：笔记侠</p></div><script>`, "笔记侠", "来源"},
		{"自家号不算作者", "标题", originalPage(`<p>正文</p><p>来源：FBIF食品饮料创新</p>`), "", "没有署名"},
		{"图片消息读 content_noencode", "三得利新品", `<script>var a = {copyright_stat: '0' * 1, content_noencode: '三得利推出新品。\x0a\x0a编辑：kiki\x0a\x26amp;',};</script>`, "Kiki", "编辑署名"},
		{"都没有就留空", "标题", originalPage(`<p>只有正文</p>`), "", "没有署名"},
		{"署名后跟自家头衔", "必胜客", originalPage(`<p>作者：Mote莫特 FBIF创始人（微信：motechan）</p><p>正文</p>`), "Mote莫特", "作者署名"},
		{"短文的署名不被切断", "可乐", originalPage(`<p>` + strings.Repeat("正", 393) + `</p><p>作者：Milo</p>`), "Milo", "作者署名"},
	}
	for _, c := range cases {
		got, rule := pickAuthor(c.title, parseArticlePage(c.page))
		if got != c.want || rule != c.rule {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, got, rule, c.want, c.rule)
		}
	}
}

// 转载卡片上的名字是转载那天的：号改过名，老文章和新文章会写成两种。按公众号编号认号，沿用先写下的名字。
func TestReprintsFromOneAccountKeepOneName(t *testing.T) {
	worker, base, pages := newAuthorWorker(t)
	rows := base.table("tblArticles")
	rows["recNew"] = map[string]interface{}{"内容标题": titled("900", "1", "新转载")}
	rows["recOld"] = map[string]interface{}{"内容标题": titled("800", "1", "老转载")}
	rows["recOther"] = map[string]interface{}{"内容标题": titled("700", "1", "别的号")}
	pages.pages[articleLink("900", "1")] = reprintPage
	pages.pages[articleLink("800", "1")] = strings.Replace(reprintPage, "Gooods好味司</span>", "Gooods 好味司</span>", 1)
	pages.pages[articleLink("700", "1")] = strings.NewReplacer("gh_90bc6f3429f4", "gh_44e51cde40c7", "Gooods好味司</span>", "凤凰WEEKLY</span>").Replace(reprintPage)

	if _, err := worker.fillAuthors(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows["recNew"]["作者"] != "Gooods好味司" || rows["recOld"]["作者"] != "Gooods好味司" || rows["recOther"]["作者"] != "凤凰WEEKLY" {
		t.Fatalf("one account one name: %v / %v / %v", rows["recNew"]["作者"], rows["recOld"]["作者"], rows["recOther"]["作者"])
	}

	// 以后这个号又改了名，新文章也沿用表里的名字。
	rows["recLater"] = map[string]interface{}{"内容标题": titled("950", "1", "改名以后")}
	pages.pages[articleLink("950", "1")] = strings.Replace(reprintPage, "Gooods好味司</span>", "好味司Gooods</span>", 1)
	if _, err := worker.fillAuthors(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows["recLater"]["作者"] != "Gooods好味司" {
		t.Fatalf("a renamed account keeps the name already in the table, got %v", rows["recLater"]["作者"])
	}
}

// 2026-11-06 首轮写的转载行没记公众号编号：服务里自己读网页补上，同一个号统一成最早写下的写法；写之后有人改过的格子不碰。
func TestReprintRowsGetTheirSourceAndOneName(t *testing.T) {
	worker, base, pages := newAuthorWorker(t)
	ctx := context.Background()
	rows := base.table("tblArticles")
	other := strings.NewReplacer("gh_90bc6f3429f4", "gh_44e51cde40c7", "Gooods好味司</span>", "凤凰WEEKLY</span>").Replace(reprintPage)
	seed := []struct{ id, mid, value, page string }{
		{"recFirst", "100", "Gooods 好味司", reprintPage}, // 最早写下的写法
		{"recLater", "200", "Gooods好味司", reprintPage},
		{"recEdited", "300", "好味司Gooods", reprintPage}, // 写之后有人改成了 Bobo
		{"recGonePage", "400", "某号", ""},
		{"recDeleted", "500", "某号", reprintPage},
		{"recOther", "600", "凤凰WEEKLY", other},
	}
	first := time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC)
	for i, row := range seed {
		if err := worker.Store.SaveBizAuthor(ctx, row.id, row.mid+"_1", archive.AuthorWritten, row.value, "转载", "", first.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if row.id != "recDeleted" {
			rows[row.id] = map[string]interface{}{"内容标题": titled(row.mid, "1", "转载"+row.mid), "作者": row.value}
		}
		if row.page != "" {
			pages.pages[articleLink(row.mid, "1")] = row.page
		}
	}
	rows["recEdited"]["作者"] = "Bobo"
	pages.errs[articleLink("400", "1")] = errPageGone

	// 被微信要求验证：停下退避，一行都不记。
	pages.errs[articleLink("600", "1")] = errVerification
	if wait := worker.authorRound(ctx, 6*time.Hour); wait != time.Minute || len(pages.fetched) != 1 {
		t.Fatalf("verification stops the backfill: wait %s, fetched %v", wait, pages.fetched)
	}
	if reprints, _ := worker.Store.BizAuthorReprints(ctx); len(reprints) != 6 || reprints[5].Source != "" {
		t.Fatalf("nothing recorded while blocked, got %v", reprints)
	}
	delete(pages.errs, articleLink("600", "1"))

	result, err := worker.fillAuthors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sourced != 4 || result.Unified != 1 || result.Left != 0 {
		t.Fatalf("4 sources found, 1 row unified, got %+v", result)
	}
	if rows["recLater"]["作者"] != "Gooods 好味司" || rows["recFirst"]["作者"] != "Gooods 好味司" || rows["recOther"]["作者"] != "凤凰WEEKLY" {
		t.Fatalf("one account one name, the first written: %v / %v / %v", rows["recFirst"]["作者"], rows["recLater"]["作者"], rows["recOther"]["作者"])
	}
	if rows["recEdited"]["作者"] != "Bobo" {
		t.Fatalf("a cell someone changed is left alone, got %v", rows["recEdited"]["作者"])
	}
	done, _ := worker.Store.BizAuthorRecords(ctx)
	if done["recEdited"] != archive.AuthorKept || done["recDeleted"] != archive.AuthorGone || done["recGonePage"] != archive.AuthorWritten {
		t.Fatalf("edited → kept, deleted row → gone, gone page keeps its name: %v", done)
	}
	reprints, _ := worker.Store.BizAuthorReprints(ctx)
	for _, row := range reprints {
		if row.Source == "" || row.RecordID == "recGonePage" {
			t.Fatalf("every reprint row left has a source, the gone page drops out: %v", reprints)
		}
	}

	fetched := len(pages.fetched)
	again, err := worker.fillAuthors(ctx)
	if err != nil || again.Sourced+again.Unified != 0 || len(pages.fetched) != fetched {
		t.Fatalf("nothing left to do: %+v %v, fetched %d more", again, err, len(pages.fetched)-fetched)
	}
}

func TestJSUnescape(t *testing.T) {
	if got := jsUnescape(`\x3cp\x3e中\x26amp;\'x\'\n`); got != "<p>中&amp;'x'\n" {
		t.Fatalf("jsUnescape = %q", got)
	}
}

type fakePages struct {
	pages   map[string]string
	errs    map[string]error
	fetched []string
	onFetch func(link string)
}

func (f *fakePages) fetch(_ context.Context, link string) (string, error) {
	f.fetched = append(f.fetched, link)
	if f.onFetch != nil {
		f.onFetch(link)
	}
	if err := f.errs[link]; err != nil {
		return "", err
	}
	return f.pages[link], nil
}

func articleLink(mid, idx string) string {
	return "http://mp.weixin.qq.com/s?__biz=MzA3ODEzMjg5Mg==&mid=" + mid + "&idx=" + idx + "&sn=abc#rd"
}

func titled(mid, idx, title string) map[string]interface{} {
	return map[string]interface{}{"link": articleLink(mid, idx), "text": title, "type": "url"}
}

func newAuthorWorker(t *testing.T) (*Worker, *fakeBase, *fakePages) {
	worker, _, base := newWorker(t)
	pages := &fakePages{pages: map[string]string{}, errs: map[string]error{}}
	noGap := time.Duration(0)
	worker.FetchPage, worker.FetchGap = pages.fetch, &noGap
	return worker, base, pages
}

func TestFillAuthorsOnlyFillsBlankCellsAndNeverComesBack(t *testing.T) {
	worker, base, pages := newAuthorWorker(t)
	rows := base.table("tblArticles")
	rows["recHuman"] = map[string]interface{}{"内容标题": titled("100", "1", "人填过的"), "作者": "Bobo"}
	rows["recBlank"] = map[string]interface{}{"内容标题": titled("200", "1", "转载的文章"), "作者": "　"} // 导入时的占位
	rows["recTitle"] = map[string]interface{}{"内容标题": titled("300", "1", "FBIF创新周报｜第 12 期")}
	rows["recGone"] = map[string]interface{}{"内容标题": titled("400", "1", "删了的文章")}
	rows["recRace"] = map[string]interface{}{"内容标题": titled("500", "1", "读网页时有人填了")}
	rows["recNoLink"] = map[string]interface{}{"内容标题": "没有链接", "作者": nil}
	pages.pages[articleLink("200", "1")] = reprintPage
	pages.errs[articleLink("400", "1")] = errPageGone
	pages.pages[articleLink("500", "1")] = originalPage(`<p>作者：Riri</p>`)
	pages.onFetch = func(link string) {
		if link == articleLink("500", "1") {
			rows["recRace"]["作者"] = "Sage"
		}
	}

	result, err := worker.fillAuthors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Filled != 2 || result.Kept != 2 || result.NoByline != 1 {
		t.Fatalf("unexpected result %+v", result)
	}
	if rows["recHuman"]["作者"] != "Bobo" || rows["recRace"]["作者"] != "Sage" {
		t.Fatalf("human values must stay: %v / %v", rows["recHuman"]["作者"], rows["recRace"]["作者"])
	}
	if rows["recBlank"]["作者"] != "Gooods好味司" || rows["recTitle"]["作者"] != "创新周报" {
		t.Fatalf("blank cells should be filled: %v / %v", rows["recBlank"]["作者"], rows["recTitle"]["作者"])
	}
	if _, ok := rows["recGone"]["作者"]; ok {
		t.Fatalf("deleted article stays blank, got %v", rows["recGone"]["作者"])
	}
	if len(pages.fetched) != 3 || pages.fetched[0] != articleLink("500", "1") {
		t.Fatalf("title rule needs no page, newest first; fetched %v", pages.fetched)
	}

	// 有人把程序填的清空了：再跑也不填回去，也不再读网页、不写表。
	rows["recBlank"]["作者"] = nil
	fetched, updates := len(pages.fetched), base.calls["update"]
	if _, err := worker.fillAuthors(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows["recBlank"]["作者"] != nil || len(pages.fetched) != fetched || base.calls["update"] != updates {
		t.Fatalf("a cleared cell must not be filled again (作者=%v, fetches %d→%d, updates %d→%d)",
			rows["recBlank"]["作者"], fetched, len(pages.fetched), updates, base.calls["update"])
	}
}

func TestNewRowsWakeAuthorFilling(t *testing.T) {
	worker, store, _ := newWorker(t)
	worker.authorWake = make(chan struct{}, 1)
	save(t, store, "getarticletotaldetail", "2026-06-12", article("100_1", "2026-06-12", 0, stat("2026-06-13", 10, 1, 0.1)))
	if _, err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.authorWake:
	default:
		t.Fatal("a new article row should wake author filling")
	}
	if _, err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.authorWake:
		t.Fatal("nothing new, author filling should stay asleep")
	default:
	}
}

// 网页一篇都读不成（断网、微信整体打不开）是服务器的事：连着 3 篇没读成就整轮停、退避、找人一次，不算到哪一行头上；恢复后照常补。
func TestAuthorFillingStopsWhenNoPageCanBeReadWithoutGivingUpRows(t *testing.T) {
	worker, base, pages := newAuthorWorker(t)
	rows := base.table("tblArticles")
	for _, mid := range []string{"1", "2", "3", "4", "5"} {
		rows["rec"+mid] = map[string]interface{}{"内容标题": titled(mid, "1", "标题"+mid)}
		pages.errs[articleLink(mid, "1")] = errors.New("dial tcp: i/o timeout")
	}
	var alerts []string
	worker.Alert = func(message string) { alerts = append(alerts, message) }

	var waits []time.Duration
	for i := 0; i < 4; i++ {
		waits = append(waits, worker.authorRound(context.Background(), 6*time.Hour))
	}
	// 第一轮读到第 3 篇停；之后每轮读一篇看恢复没有。
	if len(pages.fetched) != 6 {
		t.Fatalf("stop after 3 failed pages, then one page a round, fetched %d", len(pages.fetched))
	}
	if waits[0] != time.Minute || waits[1] != 2*time.Minute || waits[2] != 4*time.Minute || waits[3] != 8*time.Minute {
		t.Fatalf("should back off from 1 minute, got %v", waits)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "连续 3 篇没读成") {
		t.Fatalf("alert exactly once when pages stop loading, got %v", alerts)
	}
	if done, _ := worker.Store.BizAuthorRecords(context.Background()); len(done) != 0 {
		t.Fatalf("no row should be given up while the server cannot read pages, got %v", done)
	}

	for _, mid := range []string{"1", "2", "3", "4", "5"} {
		delete(pages.errs, articleLink(mid, "1"))
		pages.pages[articleLink(mid, "1")] = originalPage(`<p>作者：Milo</p>`)
	}
	if wait := worker.authorRound(context.Background(), 6*time.Hour); wait != 0 {
		t.Fatalf("round should succeed once pages load again, got wait %s", wait)
	}
	for _, mid := range []string{"1", "2", "3", "4", "5"} {
		if rows["rec"+mid]["作者"] != "Milo" {
			t.Fatalf("rec%s should be filled after recovery, got %v", mid, rows["rec"+mid])
		}
	}
	if len(alerts) != 1 {
		t.Fatalf("recovery does not alert, got %v", alerts)
	}
}

// 只是那几篇自己读不成（别的读得成）：每轮记一次，3 次后留空；连着 3 篇读不成时拿上次读成的那篇对照，读得成就不当作服务器的事。
func TestAuthorRowsThatNeverLoadAreGivenUpAfterThreeTries(t *testing.T) {
	worker, base, pages := newAuthorWorker(t)
	rows := base.table("tblArticles")
	rows["recGood"] = map[string]interface{}{"内容标题": titled("1", "1", "读得成")}
	pages.pages[articleLink("1", "1")] = originalPage(`<p>作者：Milo</p>`)
	if wait := worker.authorRound(context.Background(), 6*time.Hour); wait != 0 {
		t.Fatalf("first round should succeed, got %s", wait)
	}
	for _, mid := range []string{"7", "8", "9"} {
		rows["rec"+mid] = map[string]interface{}{"内容标题": titled(mid, "1", "标题"+mid)}
		pages.errs[articleLink(mid, "1")] = errors.New("article page has no content")
	}
	var alerts []string
	worker.Alert = func(message string) { alerts = append(alerts, message) }
	pages.fetched = nil
	var waits []time.Duration
	for i := 0; i < 3; i++ {
		waits = append(waits, worker.authorRound(context.Background(), 6*time.Hour))
	}
	if waits[0] != labelRetry || waits[1] != labelRetry || waits[2] != 0 {
		t.Fatalf("retry the rows after %s, settle on the third try: %v", labelRetry, waits)
	}
	if len(pages.fetched) != 12 || pages.fetched[3] != articleLink("1", "1") {
		t.Fatalf("each round reads the 3 rows plus the last good page once, fetched %v", pages.fetched)
	}
	if len(alerts) != 0 {
		t.Fatalf("pages of a few rows failing is not the server's problem, got %v", alerts)
	}
	done, _ := worker.Store.BizAuthorRecords(context.Background())
	for _, id := range []string{"rec7", "rec8", "rec9"} {
		if done[id] != archive.AuthorNone {
			t.Fatalf("%s should be given up after 3 tries, got %v", id, done)
		}
	}
	if wait := worker.authorRound(context.Background(), 6*time.Hour); wait != 0 || len(pages.fetched) != 12 {
		t.Fatalf("given-up rows are not read again: wait %s, fetched %d", wait, len(pages.fetched))
	}
}

// 微信要求验证挡的是服务器：整轮马上停、照样退避和告警，但不算到哪一行头上，挡完了这些行照常补。
func TestVerificationPausesAuthorFillingWithoutGivingUpRows(t *testing.T) {
	worker, base, pages := newAuthorWorker(t)
	rows := base.table("tblArticles")
	for _, mid := range []string{"1", "2", "3", "4", "5"} {
		rows["rec"+mid] = map[string]interface{}{"内容标题": titled(mid, "1", "标题"+mid)}
		pages.errs[articleLink(mid, "1")] = errVerification
	}
	var alerts []string
	worker.Alert = func(message string) { alerts = append(alerts, message) }

	var waits []time.Duration
	for i := 0; i < 4; i++ {
		waits = append(waits, worker.authorRound(context.Background(), 6*time.Hour))
	}
	if len(pages.fetched) != 4 {
		t.Fatalf("a verification page should stop the round at once, fetched %d", len(pages.fetched))
	}
	if waits[0] != time.Minute || waits[1] != 2*time.Minute || waits[2] != 4*time.Minute || waits[3] != 8*time.Minute {
		t.Fatalf("should back off from 1 minute, got %v", waits)
	}
	if len(alerts) != 1 || !strings.Contains(alerts[0], "环境异常") {
		t.Fatalf("alert exactly once on the third failed round, got %v", alerts)
	}
	done, err := worker.Store.BizAuthorRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 0 {
		t.Fatalf("no row should be given up while WeChat blocks the server, got %v", done)
	}

	for _, mid := range []string{"1", "2", "3", "4", "5"} {
		delete(pages.errs, articleLink(mid, "1"))
		pages.pages[articleLink(mid, "1")] = originalPage(`<p>作者：Milo</p>`)
	}
	if wait := worker.authorRound(context.Background(), 6*time.Hour); wait != 0 {
		t.Fatalf("round should succeed once WeChat lets us in, got wait %s", wait)
	}
	if got := rows["rec5"]["作者"]; got == nil {
		t.Fatalf("rows should be filled after the block lifts, got %v", rows["rec5"])
	}
}
