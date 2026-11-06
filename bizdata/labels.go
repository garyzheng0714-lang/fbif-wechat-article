package bizdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
)

// 「品类」「国家」（用户 2026-11-06 定）：直接用资讯订阅给自家文章打的标签，同一套标准、同一个模型，不另判。
// 资讯订阅的接口是 GET /api/internal/wechat-article-labels（口令 FEED_LABELS_TOKEN，中台 token.feed.labels）。
// 程序只填空着的格子，人填的不碰；看过的行按 record_id 记在 official_biz_labels，之后不再碰，
// 所以程序填过、有人清空的不会填回去。资讯订阅还没分类的新文章过 labelRetry 再问，发表超过 labelGiveUp 还没分类的留空。
const (
	categoryColumn = "品类"
	countryColumn  = "国家"
	labelRetry     = 2 * time.Hour
	labelGiveUp    = 7 * 24 * time.Hour
	defaultFeedURL = "https://feed.foodtalks.cn"
)

// labelNames 是资讯订阅的标签 key → 表里的选项，和资讯订阅 label-taxonomy.ts 的显示名一致；
// 深度、创新这类内容属性不是品类，不写。顺序就是写进格子的顺序。
var labelNames = []struct{ key, name string }{
	{"food", "食品"}, {"food.beverage", "饮料"}, {"food.dairy", "乳制品"}, {"food.snack", "零食"},
	{"food.alcohol", "酒"}, {"food.condiment", "调味品"}, {"food.bakery", "烘焙"},
	{"food.functional", "功能性食品"}, {"food.prepared", "预制菜"}, {"retail", "零售"},
	{"finance", "投融资"}, {"finance.ipo", "上市"}, {"finance.fundraising", "融资"}, {"finance.ma", "并购"},
	{"finance.investment", "投资"}, {"finance.earnings", "财报"},
	{"country.cn", "中国"}, {"country.jp", "日本"},
}

// labelOptions 把资讯订阅的标签换成表里的品类、国家选项。
func labelOptions(labels []string) (categories, countries []string) {
	has := make(map[string]bool, len(labels))
	for _, label := range labels {
		has[strings.TrimSpace(label)] = true
	}
	for _, item := range labelNames {
		if !has[item.key] {
			continue
		}
		if strings.HasPrefix(item.key, "country.") {
			countries = append(countries, item.name)
		} else {
			categories = append(categories, item.name)
		}
	}
	return categories, countries
}

type feedArticle struct {
	Mid        string   `json:"mid"`
	Idx        string   `json:"idx"`
	Published  string   `json:"published_at"`
	Classified bool     `json:"classified"`
	Labels     []string `json:"labels"`
}

var feedClient = &http.Client{Timeout: time.Minute}

// feedLabels 取这个号从 since 起所有文章的标签（mid_idx → 文章），一页取不完就接着取。
func (w *Worker) feedLabels(ctx context.Context, biz string, since time.Time) (map[string]feedArticle, error) {
	found := make(map[string]feedArticle)
	cursor := since.UTC().Format(time.RFC3339)
	for page := 0; page < 100; page++ {
		query := url.Values{"biz": {biz}, "since": {cursor}}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(w.FeedURL, "/")+"/api/internal/wechat-article-labels?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+w.FeedToken)
		response, err := feedClient.Do(request)
		if err != nil {
			return nil, fmt.Errorf("资讯订阅标签接口: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("资讯订阅标签接口: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("资讯订阅标签接口 HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body[:min(len(body), 200)])))
		}
		var result struct {
			Articles []feedArticle `json:"articles"`
			More     bool          `json:"more"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("资讯订阅标签接口回包: %w", err)
		}
		for _, article := range result.Articles {
			key := article.Mid + "_" + article.Idx
			if old, ok := found[key]; ok && old.Classified && !article.Classified {
				continue // 同一篇有两条时认分类过的那条
			}
			found[key] = article
		}
		if !result.More || len(result.Articles) == 0 {
			return found, nil
		}
		next := result.Articles[len(result.Articles)-1].Published
		if next == cursor {
			return nil, fmt.Errorf("资讯订阅标签接口翻页卡在 %s", next)
		}
		cursor = next
	}
	return nil, fmt.Errorf("资讯订阅标签接口翻了 100 页还没完")
}

type LabelResult struct {
	Filled  int `json:"filled"`
	Kept    int `json:"kept"`
	None    int `json:"none"`
	Pending int `json:"pending"` // 资讯订阅还没分类，过一会儿再问
}

type labelTask struct {
	recordID, msgID, biz string
	published            time.Time
	needCategory         bool
	needCountry          bool
}

type labelFill struct {
	task                labelTask
	categories, country []string
	status, rule        string
}

// fillLabels 跑一轮：读表里的链接、品类、国家，没看过且有空格子的行，按资讯订阅的标签填上。
func (w *Worker) fillLabels(ctx context.Context) (LabelResult, error) {
	var result LabelResult
	done, err := w.Store.BizLabelRecords(ctx)
	if err != nil {
		return result, err
	}
	records, err := w.Base.SearchRecords(w.ArticleTableID, []string{"内容标题", "发表日期", categoryColumn, countryColumn})
	if err != nil {
		return result, fmt.Errorf("read article table: %w", err)
	}
	var kept []labelFill
	tasksByBiz := make(map[string][]labelTask)
	for _, record := range records {
		if done[record.RecordID] != "" {
			continue
		}
		_, link := linkCell(record.Fields["内容标题"])
		match := articleLinkPattern.FindStringSubmatch(link)
		parsed, err := url.Parse(strings.TrimSpace(link))
		if match == nil || err != nil || parsed.Query().Get("__biz") == "" {
			continue
		}
		task := labelTask{
			recordID: record.RecordID, msgID: match[1] + "_" + match[2], biz: parsed.Query().Get("__biz"),
			published:    dateCell(record.Fields["发表日期"]),
			needCategory: cellText(record.Fields[categoryColumn]) == "",
			needCountry:  cellText(record.Fields[countryColumn]) == "",
		}
		if !task.needCategory && !task.needCountry {
			kept = append(kept, labelFill{task: task, status: archive.AuthorKept})
			continue
		}
		tasksByBiz[task.biz] = append(tasksByBiz[task.biz], task)
	}
	if err := w.writeLabels(ctx, kept, &result); err != nil {
		return result, err
	}
	bizs := make([]string, 0, len(tasksByBiz))
	for biz := range tasksByBiz {
		bizs = append(bizs, biz)
	}
	sort.Strings(bizs)
	for _, biz := range bizs {
		tasks := tasksByBiz[biz]
		since := w.now()
		for _, task := range tasks {
			if !task.published.IsZero() && task.published.Before(since) {
				since = task.published
			}
		}
		articles, err := w.feedLabels(ctx, biz, since.Add(-48*time.Hour)) // 表里是北京日期、资讯订阅按发表时刻，往前多取两天
		if err != nil {
			return result, err
		}
		var batch []labelFill
		for _, task := range tasks {
			article, ok := articles[task.msgID]
			if !ok || !article.Classified {
				if !task.published.IsZero() && w.now().Sub(task.published) < labelGiveUp {
					result.Pending++
					continue
				}
				rule := "资讯订阅没有这篇"
				if ok {
					rule = "资讯订阅没分类"
				}
				batch = append(batch, labelFill{task: task, status: archive.AuthorNone, rule: rule})
				continue
			}
			categories, countries := labelOptions(article.Labels)
			fill := labelFill{task: task, status: archive.AuthorWritten, rule: "资讯订阅"}
			if task.needCategory {
				fill.categories = categories
			}
			if task.needCountry {
				fill.country = countries
			}
			if len(fill.categories)+len(fill.country) == 0 {
				fill.status, fill.rule = archive.AuthorNone, "资讯订阅没标"
			}
			batch = append(batch, fill)
			if len(batch) >= 200 {
				if err := w.writeLabels(ctx, batch, &result); err != nil {
					return result, err
				}
				batch = batch[:0]
			}
		}
		if err := w.writeLabels(ctx, batch, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

// dateCell 读日期格子（飞书回的是毫秒时间戳）。
func dateCell(value interface{}) time.Time {
	switch typed := value.(type) {
	case float64:
		return time.UnixMilli(int64(typed))
	case int64:
		return time.UnixMilli(typed)
	case json.Number:
		if ms, err := typed.Int64(); err == nil {
			return time.UnixMilli(ms)
		}
	}
	return time.Time{}
}

// writeLabels 写一批：先读回这些格子，还空着才写；表里被删了的行记成 gone。
func (w *Worker) writeLabels(ctx context.Context, batch []labelFill, result *LabelResult) error {
	if len(batch) == 0 {
		return nil
	}
	var ids []string
	for _, fill := range batch {
		if fill.status == archive.AuthorWritten {
			ids = append(ids, fill.task.recordID)
		}
	}
	current := make(map[string]map[string]interface{}, len(ids))
	gone := make(map[string]bool)
	if len(ids) > 0 {
		records, absent, err := w.Base.BatchGetRecords(w.ArticleTableID, ids)
		if err != nil {
			return fmt.Errorf("read 品类、国家 before writing: %w", err)
		}
		for _, record := range records {
			current[record.RecordID] = record.Fields
		}
		for _, id := range absent {
			gone[id] = true
		}
	}
	var payload []map[string]interface{}
	for i, fill := range batch {
		if fill.status != archive.AuthorWritten {
			continue
		}
		id := fill.task.recordID
		if gone[id] {
			batch[i].status, batch[i].categories, batch[i].country = archive.AuthorGone, nil, nil
			continue
		}
		fields := map[string]interface{}{}
		if len(fill.categories) > 0 && cellText(current[id][categoryColumn]) == "" {
			fields[categoryColumn] = fill.categories
		} else {
			batch[i].categories = nil
		}
		if len(fill.country) > 0 && cellText(current[id][countryColumn]) == "" {
			fields[countryColumn] = fill.country
		} else {
			batch[i].country = nil
		}
		if len(fields) == 0 {
			batch[i].status, batch[i].rule = archive.AuthorKept, "写之前已有人填"
			continue
		}
		payload = append(payload, map[string]interface{}{"record_id": id, "fields": fields})
	}
	if len(payload) > 0 {
		if err := w.Base.BatchUpdateByRecordID(w.ArticleTableID, payload); err != nil {
			return fmt.Errorf("write 品类、国家: %w", err)
		}
	}
	for _, fill := range batch {
		if err := w.Store.SaveBizLabels(ctx, fill.task.recordID, fill.task.msgID, fill.status,
			strings.Join(fill.categories, ","), strings.Join(fill.country, ","), fill.rule, w.now()); err != nil {
			return err
		}
		switch fill.status {
		case archive.AuthorWritten:
			result.Filled++
		case archive.AuthorKept:
			result.Kept++
		case archive.AuthorNone:
			result.None++
		}
	}
	return nil
}

func (w *Worker) WakeLabels() {
	if w == nil || w.labelWake == nil {
		return
	}
	select {
	case w.labelWake <- struct{}{}:
	default:
	}
}

// labelRound 跑一轮，返回下次最晚多久再跑：有资讯订阅还没分类的新文章就 labelRetry 后再问，否则等叫醒；
// 失败从 1 分钟起翻倍退避，最长到兜底间隔，连续失败第 3 次找人一次。
func (w *Worker) labelRound(ctx context.Context, fallback time.Duration) time.Duration {
	result, err := w.fillLabels(ctx)
	if result.Filled+result.Kept+result.None+result.Pending > 0 {
		log.Printf("[BizData] labels: filled=%d kept=%d none=%d pending=%d", result.Filled, result.Kept, result.None, result.Pending)
	}
	if ctx.Err() != nil {
		return 0
	}
	if err == nil {
		w.labelFailures = 0
		if result.Pending > 0 {
			return labelRetry
		}
		return 0
	}
	w.labelFailures++
	log.Printf("[BizData] labels round failed (%d in a row): %v", w.labelFailures, err)
	if w.labelFailures == 3 && w.Alert != nil {
		w.Alert(fmt.Sprintf("公众号文章品类、国家补填连续 3 次失败：%v", err))
	}
	backoff := firstRetry << (w.labelFailures - 1)
	if backoff > fallback || backoff <= 0 {
		backoff = fallback
	}
	return backoff
}

// labelsFromEnv 读资讯订阅的地址和口令；没有口令就不补品类、国家。
func labelsFromEnv() (feedURL, token string) {
	feedURL = strings.TrimSpace(os.Getenv("FEED_API_URL"))
	if feedURL == "" {
		feedURL = defaultFeedURL
	}
	return feedURL, strings.TrimSpace(os.Getenv("FEED_LABELS_TOKEN"))
}
