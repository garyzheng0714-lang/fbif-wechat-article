// Package bizdata 把已经采到的公众号数据写进业务同事看的多维表格，并发数据汇报卡片。
//
// 和 officialbase 的区别：officialbase 是 13 张原始表的整库镜像，受历史覆盖审计门禁管；
// 这里只写两张给人看的表（文章数据、每日数据）和两种卡片（日报、周报），
// 用户 2026-11-06 定：单独写回，不受那道门禁，也不在表里写「全量」。
package bizdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

const (
	datasetArticles = "biz_articles_v1"
	datasetDays     = "biz_days_v1"

	// 接管的「2026.3-2026.11」表从这天的文章开始；更早的在「2025.1-2026.3」表里，不碰。
	articlesSince = "2026-03-23"
	// getbizsummary 从这天起才有数据。
	daysSince = "2025-11-01"

	dailyTableName  = "每日数据"
	stateDailyTable = "table:daily"

	// deletedInBase 记在 record_id 里，表示这一行在表格里被人删了：不再写、也不重建。
	deletedInBase = "deleted"
)

// Base 写的两张表。文章表是用户已有的表，按链接接管；每日表第一次写时新建。
type BaseTarget interface {
	SearchRecords(tableID string, fieldNames []string) ([]feishu.Record, error)
	BatchGetRecords(tableID string, recordIDs []string) ([]feishu.Record, []string, error)
	BatchCreateByRecordFieldsWithIDs(tableID string, records []map[string]interface{}) ([]string, error)
	BatchUpdateByRecordID(tableID string, records []map[string]interface{}) error
	EnsureFieldsExist(fields []feishu.FieldSpec, tableID string) error
	CreateTable(name string, fields []feishu.FieldSpec) (string, error)
}

func integer(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "0"}}
}

// 文章表里这些列归本服务写；作者、等级以外的人填列（国家、品类、父记录）一概不碰。
// 前 9 列是表里原有的，后 4 列是新加的。
var articleFields = []feishu.FieldSpec{
	{Name: "内容标题", Type: feishu.FieldTypeText},
	{Name: "内容url", Type: feishu.FieldTypeURL},
	integer("发表日期"),
	integer("阅读人数"),
	integer("送达人数"),
	{Name: "送达率", Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "0.00%"}},
	{Name: "阅读完成率", Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "0.00%"}},
	integer("分享"),
	{Name: "关注", Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "1,000"}},
	integer("在看"),
	integer("点赞"),
	integer("收藏"),
	integer("留言"),
}

var dayFields = []feishu.FieldSpec{
	{Name: "日期", Type: feishu.FieldTypeDatetime, Property: map[string]interface{}{"date_formatter": "yyyy/MM/dd"}},
	integer("阅读人数"), integer("分享人数"), integer("在看"), integer("点赞"), integer("留言"), integer("收藏"),
	integer("发文篇数"),
	integer("新增关注"), integer("取消关注"), integer("净增关注"),
	{Name: "累计关注", Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "1,000"}},
	integer("公众号消息阅读"), integer("推荐阅读"), integer("朋友圈阅读"), integer("聊天会话阅读"),
	integer("主页阅读"), integer("搜一搜阅读"), integer("其他阅读"),
}

var daySourceColumns = []struct{ Scene, Column string }{
	{"公众号消息", "公众号消息阅读"}, {"推荐", "推荐阅读"}, {"朋友圈", "朋友圈阅读"}, {"聊天会话", "聊天会话阅读"},
	{"公众号主页", "主页阅读"}, {"搜一搜", "搜一搜阅读"}, {"其他", "其他阅读"},
}

// Grade 是「等级」列的档位：满 1 万一档，10 万封顶，不到 1 万用全角空格（和导入时一致）。
func Grade(readUser int64) string {
	switch {
	case readUser >= 100000:
		return "10万"
	case readUser >= 10000:
		return fmt.Sprintf("%d万", readUser/10000)
	default:
		return "　"
	}
}

type row struct {
	key    string
	create map[string]interface{} // 新建时写的全部列
	update map[string]interface{} // 已有行只更新这些列
	// adjust 在更新前拿到表里的当前值，可以往 update 里加列（等级跟着升档）。
	adjust func(current map[string]interface{}, update map[string]interface{})
}

func articleRows(articles []archive.BizArticle) []row {
	rows := make([]row, 0, len(articles))
	for _, article := range articles {
		if article.StatDate == "" {
			continue // 还没有发表明细
		}
		metrics := map[string]interface{}{
			"阅读人数":  article.ReadUser,
			"送达人数":  article.Delivered(),
			"送达率":   article.DeliveryRate,
			"阅读完成率": article.FinishRate,
			"分享":    article.ShareUser,
			"关注":    article.Subscribe,
			"在看":    article.Zaikan,
			"点赞":    article.Like,
			"收藏":    article.Collection,
			"留言":    article.Comment,
		}
		create := map[string]interface{}{
			"内容标题": article.Title,
			"发表日期": dateNumber(article.PublishDate),
			"等级":   Grade(article.ReadUser),
		}
		if article.URL != "" {
			create["内容url"] = map[string]string{"link": article.URL, "text": article.URL}
		}
		for name, value := range metrics {
			create[name] = value
		}
		newGrade := Grade(article.ReadUser)
		rows = append(rows, row{
			key:    article.MsgID,
			create: create,
			update: metrics,
			adjust: func(current, update map[string]interface{}) {
				// 等级还等于按表里阅读数算出的档，说明没人手改过，才跟着升档。
				currentGrade, _ := current["等级"].(string)
				currentReads, _ := current["阅读人数"].(float64)
				if currentGrade == "" || currentGrade == Grade(int64(currentReads)) {
					update["等级"] = newGrade
				}
			},
		})
	}
	return rows
}

func dayRows(days []archive.BizDay) []row {
	rows := make([]row, 0, len(days))
	for _, day := range days {
		date, err := time.ParseInLocation("2006-01-02", day.Date, wechat.ShanghaiLoc())
		if err != nil {
			continue
		}
		fields := map[string]interface{}{"日期": date.UnixMilli()}
		if day.HasSummary {
			fields["阅读人数"], fields["分享人数"], fields["在看"] = day.ReadUser, day.ShareUser, day.Zaikan
			fields["点赞"], fields["留言"], fields["收藏"], fields["发文篇数"] = day.Like, day.Comment, day.Collection, day.SendPages
			for _, source := range daySourceColumns {
				fields[source.Column] = day.Sources[source.Scene]
			}
		}
		if day.HasFollower {
			fields["新增关注"], fields["取消关注"], fields["净增关注"] = day.NewUser, day.CancelUser, day.NetUser()
		}
		if day.HasCumulate {
			fields["累计关注"] = day.Cumulate
		}
		rows = append(rows, row{key: day.Date, create: fields, update: fields})
	}
	return rows
}

func dateNumber(date string) int64 {
	n, _ := strconv.ParseInt(strings.ReplaceAll(date, "-", ""), 10, 64)
	return n
}

var articleLinkPattern = regexp.MustCompile(`[?&]mid=(\d+)&idx=(\d+)`)

// msgIDFromLink 从公众号文章链接里取 mid 和 idx，拼成微信数据接口用的 msgid。
func msgIDFromLink(value interface{}) string {
	link := ""
	switch typed := value.(type) {
	case map[string]interface{}:
		link, _ = typed["link"].(string)
	case string:
		link = typed
	}
	match := articleLinkPattern.FindStringSubmatch(link)
	if match == nil {
		return ""
	}
	return match[1] + "_" + match[2]
}

func hashFields(fields map[string]interface{}) string {
	encoded, _ := json.Marshal(fields)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

type TableResult struct {
	Created    int `json:"created"`
	Updated    int `json:"updated"`
	Skipped    int `json:"deletedInBase"`
	Pending    int `json:"pending"`
	Adopted    int `json:"adopted,omitempty"`
	Duplicates int `json:"duplicateRowsInBase,omitempty"`
}

// syncTable 只写和上次不一样的行；没有要写的行时一次飞书接口都不调。
func (w *Worker) syncTable(ctx context.Context, dataset, tableID string, fields []feishu.FieldSpec, rows []row) (TableResult, error) {
	var result TableResult
	mapped, err := w.Store.BaseRecords(ctx, dataset)
	if err != nil {
		return result, err
	}
	var creates, updates []row
	for _, r := range rows {
		record, ok := mapped[r.key]
		switch {
		case ok && record.RecordID == deletedInBase:
			result.Skipped++
		case !ok || record.RecordID == "":
			creates = append(creates, r)
		case record.Hash != hashFields(r.update):
			updates = append(updates, r)
		}
	}
	result.Pending = len(creates) + len(updates)
	if result.Pending == 0 {
		return result, nil
	}
	if err := w.Base.EnsureFieldsExist(fields, tableID); err != nil {
		return result, fmt.Errorf("ensure fields: %w", err)
	}

	if len(updates) > 0 {
		ids := make([]string, len(updates))
		for i, r := range updates {
			ids[i] = mapped[r.key].RecordID
		}
		current, absent, err := w.Base.BatchGetRecords(tableID, ids)
		if err != nil {
			return result, fmt.Errorf("read current rows: %w", err)
		}
		gone := make(map[string]bool, len(absent))
		for _, id := range absent {
			gone[id] = true
		}
		currentByID := make(map[string]map[string]interface{}, len(current))
		for _, record := range current {
			currentByID[record.RecordID] = record.Fields
		}
		payload := make([]map[string]interface{}, 0, len(updates))
		kept := make([]row, 0, len(updates))
		for _, r := range updates {
			id := mapped[r.key].RecordID
			if gone[id] {
				log.Printf("[BizData] %s row %s was deleted in the table; leaving it deleted", dataset, r.key)
				if err := w.Store.SeedBaseRecord(ctx, dataset, r.key, deletedInBase); err != nil {
					return result, err
				}
				result.Skipped++
				continue
			}
			values := make(map[string]interface{}, len(r.update)+1)
			for name, value := range r.update {
				values[name] = value
			}
			if r.adjust != nil {
				r.adjust(currentByID[id], values)
			}
			payload = append(payload, map[string]interface{}{"record_id": id, "fields": values})
			kept = append(kept, r)
		}
		if len(payload) > 0 {
			if err := w.Base.BatchUpdateByRecordID(tableID, payload); err != nil {
				return result, fmt.Errorf("update rows: %w", err)
			}
			for _, r := range kept {
				if err := w.Store.SaveBaseRecord(ctx, dataset, r.key, mapped[r.key].RecordID, hashFields(r.update), 0, w.now()); err != nil {
					return result, err
				}
			}
			result.Updated = len(kept)
		}
	}

	if len(creates) > 0 {
		payload := make([]map[string]interface{}, len(creates))
		for i, r := range creates {
			payload[i] = map[string]interface{}{"fields": r.create}
		}
		ids, createErr := w.Base.BatchCreateByRecordFieldsWithIDs(tableID, payload)
		for i, id := range ids {
			if err := w.Store.SaveBaseRecord(ctx, dataset, creates[i].key, id, hashFields(creates[i].update), 0, w.now()); err != nil {
				return result, errors.Join(createErr, err)
			}
		}
		result.Created = len(ids)
		if createErr != nil {
			return result, fmt.Errorf("create rows: %w", createErr)
		}
	}
	result.Pending -= result.Created + result.Updated
	return result, nil
}

// adoptArticleTable 第一次接管已有文章表：按链接把表里每一行和文章对上，之后只按 record_id 写。
func (w *Worker) adoptArticleTable(ctx context.Context) (adopted, duplicates int, err error) {
	count, err := w.Store.BaseRecordCount(ctx, datasetArticles)
	if err != nil || count > 0 {
		return 0, 0, err
	}
	records, err := w.Base.SearchRecords(w.ArticleTableID, []string{"内容url"})
	if err != nil {
		return 0, 0, fmt.Errorf("read article table: %w", err)
	}
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		msgID := msgIDFromLink(record.Fields["内容url"])
		if msgID == "" {
			continue
		}
		if seen[msgID] {
			duplicates++
			continue
		}
		seen[msgID] = true
		if err := w.Store.SeedBaseRecord(ctx, datasetArticles, msgID, record.RecordID); err != nil {
			return adopted, duplicates, err
		}
		adopted++
	}
	log.Printf("[BizData] adopted article table %s: %d rows matched by link, %d duplicate links left alone", w.ArticleTableID, adopted, duplicates)
	return adopted, duplicates, nil
}

func (w *Worker) dailyTableID(ctx context.Context) (string, error) {
	id, err := w.Store.BizState(ctx, stateDailyTable)
	if err != nil || id != "" {
		return id, err
	}
	id, err = w.Base.CreateTable(dailyTableName, dayFields)
	if err != nil {
		return "", fmt.Errorf("create %s table: %w", dailyTableName, err)
	}
	return id, w.Store.SetBizState(ctx, stateDailyTable, id, w.now())
}

type BaseResult struct {
	Articles TableResult `json:"articles"`
	Days     TableResult `json:"days"`
}

func (w *Worker) syncBase(ctx context.Context) (*BaseResult, error) {
	today := w.now().In(wechat.ShanghaiLoc()).Format("2006-01-02")
	result := &BaseResult{}

	adopted, duplicates, err := w.adoptArticleTable(ctx)
	if err != nil {
		return result, err
	}
	articles, err := w.Store.BizArticles(ctx, articlesSince, today)
	if err != nil {
		return result, err
	}
	result.Articles, err = w.syncTable(ctx, datasetArticles, w.ArticleTableID, articleFields, articleRows(articles))
	result.Articles.Adopted, result.Articles.Duplicates = adopted, duplicates
	if err != nil {
		return result, fmt.Errorf("article table: %w", err)
	}

	days, err := w.Store.BizDays(ctx, daysSince, today)
	if err != nil {
		return result, err
	}
	dayRows := dayRows(days)
	if len(dayRows) == 0 {
		return result, nil
	}
	tableID, err := w.dailyTableID(ctx)
	if err != nil {
		return result, err
	}
	result.Days, err = w.syncTable(ctx, datasetDays, tableID, dayFields, dayRows)
	if err != nil {
		return result, fmt.Errorf("daily table: %w", err)
	}
	return result, nil
}
