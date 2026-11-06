package bizdata

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// 卡片照团队现有卡片的样子（FBIF-排版工具 审核卡、FBIF-公众号贴图发布 卡片）：schema 2.0、浅蓝 wathet 卡头、
// 日期放副标题、卡头不放图标、不用 emoji；块用白底细外框（grey-100），不用灰底。数字照 feishu-cardkit 的
// data-report 样板：小灰字名目、大号粗体数字、下面一行涨跌。文章是一张表（飞书卡片表格组件，每页最多 10 行，
// 多了翻页），最近发的在最上面。

var weekdayNames = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// 日报要等这几个接口都有一次成功、且微信没标延迟的请求覆盖到昨天，才发。
var reportEndpoints = []string{"getbizsummary", "getarticletotaldetail", "getusersummary", "getusercumulate"}

func (w *Worker) dataReady(ctx context.Context, date string) (bool, error) {
	for _, endpoint := range reportEndpoints {
		ok, err := w.Store.FetchedFinal(ctx, endpoint, date)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func chineseDate(t time.Time) string { return fmt.Sprintf("%d月%d日", t.Month(), t.Day()) }

func formatInt(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits := fmt.Sprintf("%d", n)
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return sign + digits
}

func formatSigned(n int64) string {
	if n > 0 {
		return "+" + formatInt(n)
	}
	return formatInt(n)
}

func percent(rate float64) string {
	if rate <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", rate*100)
}

// change 是和上一期比的涨跌，上一期没有数据时为空。
func change(current, previous int64, ok bool) string {
	if !ok || previous <= 0 {
		return ""
	}
	delta := float64(current-previous) / float64(previous) * 100
	switch {
	case math.Abs(delta) < 0.05:
		return "<font color='grey'>持平</font>"
	case delta > 0:
		return fmt.Sprintf("<font color='green'>↑ %.1f%%</font>", delta)
	default:
		return fmt.Sprintf("<font color='red'>↓ %.1f%%</font>", -delta)
	}
}

func grey(text string) string { return "<font color='grey'>" + text + "</font>" }

type kpi struct {
	label, value, note string
}

// kpiTiles 四块数字，电脑上一排四块，手机上两两一排。每块白底细外框、圆角，整块点开 link（每日数据表）；
// 没有表可开时退成浅灰底块（交互容器必须带跳转）。四块都是三行，高度一样。
func kpiTiles(items []kpi, link string) map[string]interface{} {
	columns := make([]interface{}, 0, len(items))
	for _, item := range items {
		note := item.note
		if note == "" {
			note = grey("—")
		}
		elements := []interface{}{
			map[string]interface{}{"tag": "markdown", "content": grey(item.label), "text_size": "notation"},
			map[string]interface{}{"tag": "markdown", "content": "**" + item.value + "**", "text_size": "heading-1", "margin": "4px 0px 0px 0px"},
			map[string]interface{}{"tag": "markdown", "content": note, "text_size": "notation", "margin": "2px 0px 0px 0px"},
		}
		tile := map[string]interface{}{"tag": "column", "width": "weighted", "weight": 1, "vertical_spacing": "0px"}
		if link != "" {
			tile["elements"] = []interface{}{map[string]interface{}{
				"tag": "interactive_container", "width": "fill", "has_border": true, "border_color": "grey-100",
				"corner_radius": "12px", "padding": "12px 12px 12px 12px", "vertical_spacing": "0px",
				"behaviors": []interface{}{map[string]interface{}{"type": "open_url", "default_url": link}},
				"elements":  elements,
			}}
		} else {
			tile["background_style"] = "grey-50"
			tile["padding"] = "12px 12px 12px 12px"
			tile["elements"] = elements
		}
		columns = append(columns, tile)
	}
	return map[string]interface{}{
		"tag": "column_set", "flex_mode": "bisect", "horizontal_spacing": "8px", "columns": columns,
	}
}

func heading(text string) map[string]interface{} {
	return map[string]interface{}{"tag": "markdown", "content": "**" + text + "**", "margin": "4px 0px 0px 0px"}
}

func articleLink(article archive.BizArticle) string {
	title := strings.NewReplacer("[", "【", "]", "】").Replace(article.Title)
	if article.URL == "" {
		return title
	}
	return fmt.Sprintf("[%s](%s)", title, article.URL)
}

type articleColumn struct {
	name, display, dataType, width string
	value                          func(archive.BizArticle) interface{}
}

// 数字列定宽 80px，标题列吃剩下的宽：电脑上铺满卡片一屏看全，手机上标题列冻结、左右滑看数字。
var (
	colTitle   = articleColumn{"title", "文章", "lark_md", "auto", func(a archive.BizArticle) interface{} { return articleLink(a) }}
	colDate    = articleColumn{"date", "发表", "text", "80px", func(a archive.BizArticle) interface{} { return a.PublishDate[5:] }}
	colRead    = articleColumn{"read", "阅读", "number", "80px", func(a archive.BizArticle) interface{} { return a.ReadUser }}
	colShare   = articleColumn{"share", "分享", "number", "80px", func(a archive.BizArticle) interface{} { return a.ShareUser }}
	colZaikan  = articleColumn{"zaikan", "在看", "number", "80px", func(a archive.BizArticle) interface{} { return a.Zaikan }}
	colFinish  = articleColumn{"finish", "完成率", "text", "80px", func(a archive.BizArticle) interface{} { return percent(a.FinishRate) }}
	colReadSub = articleColumn{"subscribe", "关注", "number", "80px", func(a archive.BizArticle) interface{} { return a.Subscribe }}
)

func articleTable(articles []archive.BizArticle, columns ...articleColumn) map[string]interface{} {
	specs := make([]interface{}, 0, len(columns))
	for _, column := range columns {
		spec := map[string]interface{}{
			"name": column.name, "display_name": column.display, "data_type": column.dataType, "width": column.width,
		}
		switch column.dataType {
		case "number":
			spec["format"] = map[string]interface{}{"precision": 0, "separator": true}
			spec["horizontal_align"] = "right"
		case "text":
			if column.name != "date" {
				spec["horizontal_align"] = "right"
			}
		}
		specs = append(specs, spec)
	}
	rows := make([]interface{}, 0, len(articles))
	for _, article := range articles {
		values := make(map[string]interface{}, len(columns))
		for _, column := range columns {
			values[column.name] = column.value(article)
		}
		rows = append(rows, values)
	}
	pageSize := min(max(len(articles), 1), 10)
	return map[string]interface{}{
		"tag": "table", "page_size": pageSize, "row_height": "auto",
		"freeze_first_column": true,
		"header_style": map[string]interface{}{
			"text_align": "left", "text_size": "normal", "background_style": "grey", "text_color": "grey", "bold": true, "lines": 1,
		},
		"columns": specs, "rows": rows,
	}
}

// newestFirst 最近发的在最上面；同一天按推送里的位置（头条在前）。
func newestFirst(articles []archive.BizArticle) []archive.BizArticle {
	sorted := append([]archive.BizArticle(nil), articles...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].PublishDate != sorted[j].PublishDate {
			return sorted[i].PublishDate > sorted[j].PublishDate
		}
		return sorted[i].MsgID < sorted[j].MsgID
	})
	return sorted
}

func (w *Worker) baseURL(tableID string) string {
	if w.BaseToken == "" || tableID == "" {
		return ""
	}
	return fmt.Sprintf("https://foodtalks.feishu.cn/base/%s?table=%s", w.BaseToken, tableID)
}

// dailyTableURL 数字块点开「每日数据」表；表还没建就开文章表。
func (w *Worker) dailyTableURL(ctx context.Context) string {
	if id, err := w.Store.BizState(ctx, stateDailyTable); err == nil && id != "" && id != deletedInBase {
		if link := w.baseURL(id); link != "" {
			return link
		}
	}
	return w.baseURL(w.ArticleTableID)
}

func (w *Worker) card(title, subtitle string, elements []interface{}) map[string]interface{} {
	if link := w.baseURL(w.ArticleTableID); link != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "button", "text": map[string]interface{}{"tag": "plain_text", "content": "打开数据表"},
			"type": "default", "width": "default", "margin": "4px 0px 0px 0px",
			"behaviors": []interface{}{map[string]interface{}{"type": "open_url", "default_url": link}},
		})
	}
	return map[string]interface{}{
		"schema": "2.0",
		"config": map[string]interface{}{"width_mode": "fill", "summary": map[string]interface{}{"content": title + " · " + subtitle}},
		"header": map[string]interface{}{
			"template": "wathet",
			"title":    map[string]interface{}{"tag": "plain_text", "content": title},
			"subtitle": map[string]interface{}{"tag": "plain_text", "content": subtitle},
		},
		"body": map[string]interface{}{
			"direction": "vertical", "padding": "12px 12px 16px 12px", "vertical_spacing": "12px", "elements": elements,
		},
	}
}

func dayByDate(days []archive.BizDay) map[string]archive.BizDay {
	byDate := make(map[string]archive.BizDay, len(days))
	for _, day := range days {
		byDate[day.Date] = day
	}
	return byDate
}

// followerNote 净增关注那块下面一行：新增、取关各多少。
func followerNote(newUser, cancelUser int64) string {
	return grey(fmt.Sprintf("新增 %s · 取关 %s", formatInt(newUser), formatInt(cancelUser)))
}

// growthNote 累计关注那块下面一行：比 since 那天多了多少；那天没采到就空着。
func growthNote(label string, current archive.BizDay, since archive.BizDay, ok bool) string {
	if !ok || !since.HasCumulate || !current.HasCumulate {
		return ""
	}
	return grey(label + " " + formatSigned(current.Cumulate-since.Cumulate))
}

// DailyCard 是 date（昨天）的日报：昨天账号的四个数，加上近 7 天（含昨天）发的全部文章，最近发的在最上面。
func (w *Worker) DailyCard(ctx context.Context, date time.Time) (map[string]interface{}, error) {
	day := date.Format("2006-01-02")
	previous := date.AddDate(0, 0, -1).Format("2006-01-02")
	weekAgo := date.AddDate(0, 0, -7).Format("2006-01-02")
	days, err := w.Store.BizDays(ctx, weekAgo, day)
	if err != nil {
		return nil, err
	}
	byDate := dayByDate(days)
	today, ok := byDate[day]
	if !ok || !today.HasSummary {
		return nil, fmt.Errorf("no account summary for %s", day)
	}
	before, hasBefore := byDate[previous]
	lastWeek, hasLastWeek := byDate[weekAgo]

	elements := []interface{}{kpiTiles([]kpi{
		{"阅读人数", formatInt(today.ReadUser), change(today.ReadUser, before.ReadUser, hasBefore && before.HasSummary)},
		{"分享人数", formatInt(today.ShareUser), change(today.ShareUser, before.ShareUser, hasBefore && before.HasSummary)},
		{"净增关注", formatSigned(today.NetUser()), followerNote(today.NewUser, today.CancelUser)},
		{"累计关注", formatInt(today.Cumulate), growthNote("近 7 天", today, lastWeek, hasLastWeek)},
	}, w.dailyTableURL(ctx))}

	recent, err := w.Store.BizArticles(ctx, date.AddDate(0, 0, -6).Format("2006-01-02"), day)
	if err != nil {
		return nil, err
	}
	if len(recent) > 0 {
		elements = append(elements, heading(fmt.Sprintf("近 7 天发文 %d 篇", len(recent))),
			articleTable(newestFirst(recent), colTitle, colDate, colRead, colShare, colZaikan, colFinish))
	} else {
		elements = append(elements, heading("近 7 天没有发文"))
	}
	return w.card("公众号日报", chineseDate(date)+" "+weekdayNames[date.Weekday()], elements), nil
}

type weekTotals struct {
	read, share, newUser, cancelUser int64
	days                             int
}

func sumWeek(days []archive.BizDay, from, to string) weekTotals {
	var totals weekTotals
	for _, day := range days {
		if day.Date < from || day.Date > to {
			continue
		}
		totals.days++
		totals.read += day.ReadUser
		totals.share += day.ShareUser
		totals.newUser += day.NewUser
		totals.cancelUser += day.CancelUser
	}
	return totals
}

// WeeklyCard 是 monday 那周（周一到周日）的周报：一周合计和上一周比，加上这周发的全部文章，最近发的在最上面。
func (w *Worker) WeeklyCard(ctx context.Context, monday time.Time) (map[string]interface{}, error) {
	sunday := monday.AddDate(0, 0, 6)
	from, to := monday.Format("2006-01-02"), sunday.Format("2006-01-02")
	lastFrom, lastTo := monday.AddDate(0, 0, -7).Format("2006-01-02"), monday.AddDate(0, 0, -1).Format("2006-01-02")
	fourWeeksAgo := monday.AddDate(0, 0, -22).Format("2006-01-02")
	days, err := w.Store.BizDays(ctx, fourWeeksAgo, to)
	if err != nil {
		return nil, err
	}
	this, last := sumWeek(days, from, to), sumWeek(days, lastFrom, lastTo)
	if this.days == 0 {
		return nil, fmt.Errorf("no account data for week %s", from)
	}
	hasLast := last.days == 7
	byDate := dayByDate(days)
	end, hasEnd := byDate[to]
	start, hasStart := byDate[fourWeeksAgo]
	cumulate := ""
	if hasEnd && end.HasCumulate {
		cumulate = formatInt(end.Cumulate)
	} else {
		cumulate = "—"
	}
	articles, err := w.Store.BizArticles(ctx, from, to)
	if err != nil {
		return nil, err
	}
	elements := []interface{}{kpiTiles([]kpi{
		{"阅读人数", formatInt(this.read), change(this.read, last.read, hasLast)},
		{"分享人数", formatInt(this.share), change(this.share, last.share, hasLast)},
		{"净增关注", formatSigned(this.newUser - this.cancelUser), followerNote(this.newUser, this.cancelUser)},
		{"累计关注", cumulate, growthNote("近 4 周", end, start, hasEnd && hasStart)},
	}, w.dailyTableURL(ctx))}
	if len(articles) > 0 {
		elements = append(elements, heading(fmt.Sprintf("这周发文 %d 篇", len(articles))),
			articleTable(newestFirst(articles), colTitle, colDate, colRead, colShare, colZaikan, colFinish, colReadSub))
	} else {
		elements = append(elements, heading("这周没有发文"))
	}
	return w.card("公众号周报", chineseDate(monday)+"–"+chineseDate(sunday), elements), nil
}

type ReportResult struct {
	Sent    []string `json:"sent,omitempty"`
	Waiting string   `json:"waiting,omitempty"`
}

// sendReports 在昨天的数据采齐后发日报；周一再发上周周报。每份只发一次，记在 SQLite。
func (w *Worker) sendReports(ctx context.Context) (*ReportResult, error) {
	result := &ReportResult{}
	if w.ChatID == "" || w.Send == nil {
		return result, nil
	}
	local := w.now().In(wechat.ShanghaiLoc())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	yesterday := today.AddDate(0, 0, -1)
	date := yesterday.Format("2006-01-02")
	ready, err := w.dataReady(ctx, date)
	if err != nil {
		return result, err
	}
	if !ready {
		result.Waiting = date
		return result, nil
	}

	type report struct {
		key  string
		card func() (map[string]interface{}, error)
	}
	reports := []report{{"daily:" + date, func() (map[string]interface{}, error) { return w.DailyCard(ctx, yesterday) }}}
	if today.Weekday() == time.Monday {
		monday := today.AddDate(0, 0, -7)
		reports = append(reports, report{"weekly:" + monday.Format("2006-01-02"), func() (map[string]interface{}, error) { return w.WeeklyCard(ctx, monday) }})
	}
	for _, r := range reports {
		stateKey := "report:" + r.key
		sent, err := w.Store.BizState(ctx, stateKey)
		if err != nil {
			return result, err
		}
		if sent != "" {
			continue
		}
		card, err := r.card()
		if err != nil {
			return result, fmt.Errorf("build %s card: %w", r.key, err)
		}
		messageID, err := w.Send(w.ChatID, card, "wxdata-"+r.key)
		if err != nil {
			return result, fmt.Errorf("send %s card: %w", r.key, err)
		}
		if err := w.Store.SetBizState(ctx, stateKey, messageID, w.now()); err != nil {
			return result, err
		}
		result.Sent = append(result.Sent, r.key)
	}
	return result, nil
}
