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

// 卡片用飞书卡片 JSON 2.0 的表格组件（tag: table）。一张卡最多 5 个表格，每页最多 10 行。

var weekdayNames = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// 日报要等这几个接口都采完昨天、且昨天没有被微信标成延迟，才发。
var reportEndpoints = []string{"getbizsummary", "getarticletotaldetail", "getusersummary", "getusercumulate"}

func (w *Worker) dataReady(ctx context.Context, date string) (bool, error) {
	for _, endpoint := range reportEndpoints {
		state, err := w.Store.GetState(ctx, endpoint)
		if err != nil {
			return false, err
		}
		if state == nil || state.LastSuccessEnd < date {
			return false, nil
		}
		if state.DeferredPending && state.LastDeferredEnd >= date {
			return false, nil
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

// change 是和上一期比的涨跌，前一期没有数据时不显示。
func change(current, previous int64, ok bool) string {
	if !ok || previous <= 0 {
		return ""
	}
	delta := float64(current-previous) / float64(previous) * 100
	switch {
	case math.Abs(delta) < 0.05:
		return "<font color='grey'>持平</font>"
	case delta > 0:
		return fmt.Sprintf("<font color='green'>↑%.1f%%</font>", delta)
	default:
		return fmt.Sprintf("<font color='red'>↓%.1f%%</font>", -delta)
	}
}

type kpi struct {
	label, value, change string
}

func kpiRow(items []kpi) map[string]interface{} {
	columns := make([]interface{}, 0, len(items))
	for _, item := range items {
		content := fmt.Sprintf("<font color='grey'>%s</font>\n**%s**", item.label, item.value)
		if item.change != "" {
			content += "\n" + item.change
		}
		columns = append(columns, map[string]interface{}{
			"tag": "column", "width": "weighted", "weight": 1, "vertical_align": "top",
			"elements": []interface{}{map[string]interface{}{"tag": "markdown", "content": content}},
		})
	}
	return map[string]interface{}{
		"tag": "column_set", "flex_mode": "bisect", "horizontal_spacing": "12px", "columns": columns,
	}
}

func heading(text string) map[string]interface{} {
	return map[string]interface{}{"tag": "markdown", "content": "**" + text + "**", "margin": "8px 0 0 0"}
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
	pageSize := len(articles)
	if pageSize > 10 {
		pageSize = 10
	}
	if pageSize < 1 {
		pageSize = 1
	}
	return map[string]interface{}{
		"tag": "table", "page_size": pageSize, "row_height": "auto",
		"freeze_first_column": true,
		"header_style": map[string]interface{}{
			"text_align": "left", "text_size": "normal", "background_style": "grey", "text_color": "grey", "bold": true, "lines": 1,
		},
		"columns": specs, "rows": rows,
	}
}

func (w *Worker) tableURL() string {
	if w.BaseToken == "" {
		return ""
	}
	return fmt.Sprintf("https://foodtalks.feishu.cn/base/%s?table=%s", w.BaseToken, w.ArticleTableID)
}

func (w *Worker) card(title, template string, elements []interface{}) map[string]interface{} {
	if link := w.tableURL(); link != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "button", "text": map[string]interface{}{"tag": "plain_text", "content": "打开数据表"},
			"type": "default", "size": "medium", "margin": "8px 0 0 0",
			"behaviors": []interface{}{map[string]interface{}{"type": "open_url", "default_url": link}},
		})
	}
	return map[string]interface{}{
		"schema": "2.0",
		"config": map[string]interface{}{"width_mode": "fill", "summary": map[string]interface{}{"content": title}},
		"header": map[string]interface{}{
			"title":    map[string]interface{}{"tag": "plain_text", "content": title},
			"template": template,
		},
		"body": map[string]interface{}{"elements": elements},
	}
}

func dayByDate(days []archive.BizDay) map[string]archive.BizDay {
	byDate := make(map[string]archive.BizDay, len(days))
	for _, day := range days {
		byDate[day.Date] = day
	}
	return byDate
}

func topByReads(articles []archive.BizArticle, limit int) []archive.BizArticle {
	sorted := append([]archive.BizArticle(nil), articles...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ReadUser > sorted[j].ReadUser })
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

// DailyCard 是 date（昨天）的日报：账号当天的数、昨天发的文章、前 7 天文章累计阅读前 5。
func (w *Worker) DailyCard(ctx context.Context, date time.Time) (map[string]interface{}, error) {
	day := date.Format("2006-01-02")
	previous := date.AddDate(0, 0, -1).Format("2006-01-02")
	days, err := w.Store.BizDays(ctx, previous, day)
	if err != nil {
		return nil, err
	}
	byDate := dayByDate(days)
	today, ok := byDate[day]
	if !ok || !today.HasSummary {
		return nil, fmt.Errorf("no account summary for %s", day)
	}
	before, hasBefore := byDate[previous]

	elements := []interface{}{kpiRow([]kpi{
		{"阅读人数", formatInt(today.ReadUser), change(today.ReadUser, before.ReadUser, hasBefore && before.HasSummary)},
		{"分享人数", formatInt(today.ShareUser), change(today.ShareUser, before.ShareUser, hasBefore && before.HasSummary)},
		{"净增关注", formatSigned(today.NetUser()), ""},
		{"累计关注", formatInt(today.Cumulate), ""},
	})}

	published, err := w.Store.BizArticles(ctx, day, day)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(published, func(i, j int) bool { return published[i].MsgID < published[j].MsgID })
	if len(published) > 0 {
		elements = append(elements, heading(fmt.Sprintf("当天发文 %d 篇", len(published))),
			articleTable(published, colTitle, colRead, colShare, colZaikan, colFinish))
	} else {
		elements = append(elements, heading("当天没有发文"))
	}

	recent, err := w.Store.BizArticles(ctx, date.AddDate(0, 0, -7).Format("2006-01-02"), previous)
	if err != nil {
		return nil, err
	}
	if len(recent) > 0 {
		elements = append(elements, heading("前 7 天文章 · 阅读前 5"),
			articleTable(topByReads(recent, 5), colTitle, colDate, colRead, colShare, colFinish))
	}
	title := fmt.Sprintf("公众号日报 · %s %s", chineseDate(date), weekdayNames[date.Weekday()])
	return w.card(title, "blue", elements), nil
}

type weekTotals struct {
	read, share, net, pages int64
	cumulate                int64
	days                    int
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
		totals.pages += day.SendPages
		totals.net += day.NetUser()
		if day.HasCumulate {
			totals.cumulate = day.Cumulate
		}
	}
	return totals
}

// WeeklyCard 是 monday 那周（周一到周日）的周报：一周合计、和上一周比，以及这周文章阅读前 10。
func (w *Worker) WeeklyCard(ctx context.Context, monday time.Time) (map[string]interface{}, error) {
	sunday := monday.AddDate(0, 0, 6)
	from, to := monday.Format("2006-01-02"), sunday.Format("2006-01-02")
	lastFrom, lastTo := monday.AddDate(0, 0, -7).Format("2006-01-02"), monday.AddDate(0, 0, -1).Format("2006-01-02")
	days, err := w.Store.BizDays(ctx, lastFrom, to)
	if err != nil {
		return nil, err
	}
	this, last := sumWeek(days, from, to), sumWeek(days, lastFrom, lastTo)
	if this.days == 0 {
		return nil, fmt.Errorf("no account data for week %s", from)
	}
	hasLast := last.days == 7
	articles, err := w.Store.BizArticles(ctx, from, to)
	if err != nil {
		return nil, err
	}
	elements := []interface{}{kpiRow([]kpi{
		{"阅读人数", formatInt(this.read), change(this.read, last.read, hasLast)},
		{"分享人数", formatInt(this.share), change(this.share, last.share, hasLast)},
		{"净增关注", formatSigned(this.net), ""},
		{"累计关注", formatInt(this.cumulate), ""},
	})}
	if len(articles) > 0 {
		top := topByReads(articles, 10)
		elements = append(elements, heading(fmt.Sprintf("本周发文 %d 篇 · 阅读前 %d", len(articles), len(top))),
			articleTable(top, colTitle, colDate, colRead, colShare, colZaikan, colFinish, colReadSub))
	}
	title := fmt.Sprintf("公众号周报 · %s–%s", chineseDate(monday), chineseDate(sunday))
	return w.card(title, "indigo", elements), nil
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
