// Package bizdata 把已经采到的公众号数据写进业务同事看的多维表格，并发数据汇报卡片。
//
// 和 officialbase 的区别：officialbase 是 13 张原始表的整库镜像，受历史覆盖审计门禁管；
// 这里只写两张给人看的表（文章数据、每日数据，都关联到「公众号」表）和两种卡片（日报、周报），
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
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

const (
	datasetArticles = "biz_articles_v1"
	datasetDays     = "biz_days_v1"

	// 两张表都从微信新版接口最早有数的那天写起（见 wechat.DataCubeEndpointByName）；
	// 更早的文章是用户导入的，接口拿不到，程序不碰。
	articleEndpoint = "getarticletotaldetail"
	dayEndpoint     = "getbizsummary"

	dailyTableName  = "每日数据"
	stateDailyTable = "table:daily"

	// 「公众号」表一号一行，文章表、每日数据表的「公众号」列关联到它，按号筛选、分组。
	accountTableName   = "公众号"
	accountColumn      = "公众号"
	stateAccountTable  = "table:account"
	stateAccountPrefix = "account:"

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
	GetOrCreateTable(tableName string, primaryFieldName ...string) (string, error)
}

func earliest(endpoint string) string {
	spec, _ := wechat.DataCubeEndpointByName(endpoint)
	return spec.EarliestDate
}

func accountField(accountTableID string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: accountColumn, Type: feishu.FieldTypeLink, Property: map[string]interface{}{"table_id": accountTableID, "multiple": false}}
}

// withAccount 在要确保存在的列里加上关联「公众号」表的那一列；还不知道公众号表时不加。
func withAccount(fields []feishu.FieldSpec, accountTableID string) []feishu.FieldSpec {
	if accountTableID == "" {
		return fields
	}
	return append(append([]feishu.FieldSpec{}, fields...), accountField(accountTableID))
}

func integer(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "0"}}
}

func percentField(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "0.00%"}}
}

func dateField(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeDatetime, Property: map[string]interface{}{"date_formatter": "yyyy/MM/dd"}}
}

const (
	notifiedKind   = "群发"
	unnotifiedKind = "仅发表"
)

// jumpColumns 对应微信 read_jump_position 的 position 1-5：读到文章哪一段跳出的人占多少。
var jumpColumns = []string{"跳出0-20%", "跳出20-40%", "跳出40-60%", "跳出60-80%", "跳出80-100%"}

// sourceColumns 是阅读来源（read_user_source 的 scene_desc）对应的列，文章表和每日数据表共用。
var sourceColumns = []struct{ Scene, Column string }{
	{"公众号消息", "公众号消息阅读"}, {"推荐", "推荐阅读"}, {"朋友圈", "朋友圈阅读"}, {"聊天会话", "聊天会话阅读"},
	{"公众号主页", "主页阅读"}, {"搜一搜", "搜一搜阅读"}, {"其他", "其他阅读"},
}

// 文章表里这些列归本服务写，微信给的都写上（用户 2026-11-06：字段应多尽多）；
// 作者、国家、品类这些人填的列一概不碰。首列「内容标题」是超链接，点开就是原文。
var articleFields = func() []feishu.FieldSpec {
	fields := []feishu.FieldSpec{
		{Name: "内容标题", Type: feishu.FieldTypeURL},
		dateField("发表日期"),
		{Name: "发表方式", Type: feishu.FieldTypeSingleSelect, Property: map[string]interface{}{
			"options": []map[string]interface{}{{"name": notifiedKind}, {"name": unnotifiedKind}},
		}},
		integer("第几条"),
		integer("阅读人数"),
		integer("送达人数"),
		percentField("送达率"),
		percentField("阅读完成率"),
		integer("平均阅读秒数"),
		integer("分享"),
		{Name: "关注", Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "1,000"}},
		integer("在看"),
		integer("点赞"),
		integer("收藏"),
		integer("留言"),
		{Name: "赞赏金额", Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "0.00"}},
	}
	for _, source := range sourceColumns {
		fields = append(fields, integer(source.Column))
	}
	for _, column := range jumpColumns {
		fields = append(fields, percentField(column))
	}
	return append(fields, dateField("数据截至"))
}()

// followSourceColumns 是 getusersummary 的 user_source（微信文档里的取值）对应的新增关注列；
// 文档外的取值算进「其他关注」。
var followSourceColumns = []struct {
	Sources []int
	Column  string
}{
	{[]int{57}, "文章内关注"}, {[]int{1}, "搜索关注"}, {[]int{17}, "名片分享关注"}, {[]int{30}, "扫码关注"},
	{[]int{200}, "视频号关注"}, {[]int{201}, "直播关注"}, {[]int{161}, "他人转载关注"}, {[]int{100}, "广告关注"},
	{[]int{149}, "小程序关注"}, {[]int{51}, "支付后关注"}, {[]int{0}, "其他关注"},
}

var dayFields = func() []feishu.FieldSpec {
	fields := []feishu.FieldSpec{
		dateField("日期"),
		integer("阅读人数"), integer("分享人数"), integer("在看"), integer("点赞"), integer("留言"), integer("收藏"),
		integer("发文篇数"),
		integer("新增关注"), integer("取消关注"), integer("净增关注"),
		{Name: "累计关注", Type: feishu.FieldTypeNumber, Property: map[string]interface{}{"formatter": "1,000"}},
	}
	for _, source := range sourceColumns {
		fields = append(fields, integer(source.Column))
	}
	fields = append(fields, integer("阅读原文"))
	for _, source := range followSourceColumns {
		fields = append(fields, integer(source.Column))
	}
	return fields
}()

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

// articleRows 把文章变成要写的行；accounts 是 biz → 「公众号」表里那一行的 record_id。
func articleRows(articles []archive.BizArticle, accounts map[string]string) []row {
	rows := make([]row, 0, len(articles))
	for _, article := range articles {
		if article.StatDate == "" {
			continue // 还没有发表明细
		}
		kind := notifiedKind
		if !article.Notified {
			kind = unnotifiedKind
		}
		// 已有的行（接管来的、老表搬来的）也只拿到 update 这些列，所以微信给的都放这里。
		metrics := map[string]interface{}{
			"发表方式":   kind,
			"第几条":    article.Position,
			"阅读人数":   article.ReadUser,
			"送达人数":   article.Delivered(),
			"送达率":    article.DeliveryRate,
			"阅读完成率":  article.FinishRate,
			"平均阅读秒数": int64(math.Round(article.AvgMinutes * 60)),
			"分享":     article.ShareUser,
			"关注":     article.Subscribe,
			"在看":     article.Zaikan,
			"点赞":     article.Like,
			"收藏":     article.Collection,
			"留言":     article.Comment,
			"赞赏金额":   float64(article.PraiseMoney) / 100,
			"数据截至":   dayMillis(article.StatDate),
		}
		for _, source := range sourceColumns {
			metrics[source.Column] = article.Sources[source.Scene]
		}
		for i, rate := range article.JumpRates {
			metrics[jumpColumns[i]] = rate
		}
		if id := accounts[bizFromLink(article.URL)]; id != "" {
			metrics[accountColumn] = []string{id}
		}
		create := map[string]interface{}{
			"发表日期": dayMillis(article.PublishDate),
			"等级":   Grade(article.ReadUser),
		}
		if article.URL != "" {
			create["内容标题"] = map[string]string{"text": article.Title, "link": article.URL}
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

// dayRows 把每日数据变成要写的行；accountID 是本号在「公众号」表里那一行。
func dayRows(days []archive.BizDay, accountID string) []row {
	rows := make([]row, 0, len(days))
	for _, day := range days {
		millis := dayMillis(day.Date)
		if millis == 0 {
			continue
		}
		fields := map[string]interface{}{"日期": millis}
		if accountID != "" {
			fields[accountColumn] = []string{accountID}
		}
		if day.HasSummary {
			fields["阅读人数"], fields["分享人数"], fields["在看"] = day.ReadUser, day.ShareUser, day.Zaikan
			fields["点赞"], fields["留言"], fields["收藏"], fields["发文篇数"] = day.Like, day.Comment, day.Collection, day.SendPages
			for _, source := range sourceColumns {
				fields[source.Column] = day.Sources[source.Scene]
			}
			fields["阅读原文"] = day.RedirectOriginal
		}
		if day.HasFollower {
			fields["新增关注"], fields["取消关注"], fields["净增关注"] = day.NewUser, day.CancelUser, day.NetUser()
			counted := int64(0)
			for _, source := range followSourceColumns {
				n := int64(0)
				for _, code := range source.Sources {
					n += day.NewBySource[code]
				}
				fields[source.Column] = n
				counted += n
			}
			fields["其他关注"] = fields["其他关注"].(int64) + day.NewUser - counted
		}
		if day.HasCumulate {
			fields["累计关注"] = day.Cumulate
		}
		rows = append(rows, row{key: day.Date, create: fields, update: fields})
	}
	return rows
}

// dayMillis 把 YYYY-MM-DD 变成飞书日期列要的毫秒时间戳（上海当天零点）；认不出来给 0。
func dayMillis(day string) int64 {
	parsed, err := time.ParseInLocation("2006-01-02", day, wechat.ShanghaiLoc())
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}

var (
	articleLinkPattern = regexp.MustCompile(`[?&]mid=(\d+)&idx=(\d+)`)
	bizPattern         = regexp.MustCompile(`[?&]__biz=([^&#]+)`)
)

// bizFromLink 从公众号文章链接里取 __biz，认是哪个号。
func bizFromLink(link string) string {
	match := bizPattern.FindStringSubmatch(link)
	if match == nil {
		return ""
	}
	biz, err := url.QueryUnescape(match[1])
	if err != nil {
		return match[1]
	}
	return biz
}

// textValue 读回一个文本格子：飞书回的是分段数组，测试和老数据里也可能是字符串。
func textValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []interface{}:
		var text strings.Builder
		for _, segment := range typed {
			if part, ok := segment.(map[string]interface{}); ok {
				piece, _ := part["text"].(string)
				text.WriteString(piece)
			}
		}
		return strings.TrimSpace(text.String())
	}
	return ""
}

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

// adoptByLink 把表里已有、还没记下 record_id 的文章行按链接（mid+idx）认领过来，之后只按 record_id 写。
// 只在有文章还没对上行时才读表：第一次接管、老表搬进来的、有人手贴的行都按这条认领，不建重复的行。
// 记成「被人删了」的文章不算没对上，删了就不再认领、不重建。
func (w *Worker) adoptByLink(ctx context.Context, rows []row) (adopted, duplicates int, err error) {
	mapped, err := w.Store.BaseRecords(ctx, datasetArticles)
	if err != nil {
		return 0, 0, err
	}
	unmapped := make(map[string]bool)
	for _, r := range rows {
		if record, ok := mapped[r.key]; !ok || record.RecordID == "" {
			unmapped[r.key] = true
		}
	}
	if len(unmapped) == 0 {
		return 0, 0, nil
	}
	taken := make(map[string]bool, len(mapped))
	for _, record := range mapped {
		taken[record.RecordID] = true
	}
	records, err := w.Base.SearchRecords(w.ArticleTableID, []string{"内容标题"})
	if err != nil {
		return 0, 0, fmt.Errorf("read article table: %w", err)
	}
	for _, record := range records {
		msgID := msgIDFromLink(record.Fields["内容标题"])
		if !unmapped[msgID] || taken[record.RecordID] {
			continue
		}
		if mapped[msgID].RecordID != "" {
			duplicates++
			continue
		}
		if err := w.Store.SeedBaseRecord(ctx, datasetArticles, msgID, record.RecordID); err != nil {
			return adopted, duplicates, err
		}
		mapped[msgID] = archive.BaseRecord{RecordID: record.RecordID}
		adopted++
	}
	if adopted > 0 || duplicates > 0 {
		log.Printf("[BizData] article table %s: adopted %d rows by link, %d duplicate links left alone", w.ArticleTableID, adopted, duplicates)
	}
	return adopted, duplicates, nil
}

// accountLinks 找到每个 biz 在「公众号」表里那一行：先用本地记下的，没有才读表；
// 表里也没有就建一行，只填 biz，名称留给人填（程序拿不到号的正式名称，不瞎写）。
// 返回 biz → record_id 和公众号表的 table_id；bizs 为空时不调飞书。
func (w *Worker) accountLinks(ctx context.Context, bizs []string) (map[string]string, string, error) {
	links := make(map[string]string, len(bizs))
	tableID, err := w.Store.BizState(ctx, stateAccountTable)
	if err != nil || len(bizs) == 0 {
		return links, tableID, err
	}
	var missing []string
	for _, biz := range bizs {
		id, err := w.Store.BizState(ctx, stateAccountPrefix+biz)
		if err != nil {
			return nil, "", err
		}
		if id == "" {
			missing = append(missing, biz)
		}
		links[biz] = id
	}
	if len(missing) == 0 && tableID != "" {
		return links, tableID, nil
	}
	if tableID == "" {
		if tableID, err = w.Base.GetOrCreateTable(accountTableName, "名称"); err != nil {
			return nil, "", fmt.Errorf("find %s table: %w", accountTableName, err)
		}
		if err := w.Base.EnsureFieldsExist([]feishu.FieldSpec{{Name: "biz", Type: feishu.FieldTypeText}}, tableID); err != nil {
			return nil, "", fmt.Errorf("ensure %s fields: %w", accountTableName, err)
		}
		if err := w.Store.SetBizState(ctx, stateAccountTable, tableID, w.now()); err != nil {
			return nil, "", err
		}
	}
	records, err := w.Base.SearchRecords(tableID, []string{"biz"})
	if err != nil {
		return nil, "", fmt.Errorf("read %s table: %w", accountTableName, err)
	}
	existing := make(map[string]string, len(records))
	for _, record := range records {
		if biz := textValue(record.Fields["biz"]); biz != "" && existing[biz] == "" {
			existing[biz] = record.RecordID
		}
	}
	for _, biz := range missing {
		id := existing[biz]
		if id == "" {
			ids, err := w.Base.BatchCreateByRecordFieldsWithIDs(tableID, []map[string]interface{}{{"fields": map[string]interface{}{"biz": biz}}})
			if err != nil || len(ids) != 1 {
				return nil, "", fmt.Errorf("create %s row for %s: %v", accountTableName, biz, err)
			}
			id = ids[0]
			log.Printf("[BizData] added %s row %s for biz %s; its 名称 is left for a person to fill", accountTableName, id, biz)
		}
		if err := w.Store.SetBizState(ctx, stateAccountPrefix+biz, id, w.now()); err != nil {
			return nil, "", err
		}
		links[biz] = id
	}
	return links, tableID, nil
}

// articleBizs 是文章链接里出现的号，最后一个是最近发文的号（每日数据记在它名下）。
func articleBizs(articles []archive.BizArticle) []string {
	var bizs []string
	seen := make(map[string]bool)
	for i := len(articles) - 1; i >= 0; i-- {
		if biz := bizFromLink(articles[i].URL); biz != "" && !seen[biz] {
			seen[biz] = true
			bizs = append([]string{biz}, bizs...)
		}
	}
	return bizs
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

	articles, err := w.Store.BizArticlesAll(ctx, earliest(articleEndpoint), today)
	if err != nil {
		return result, err
	}
	bizs := articleBizs(articles)
	accounts, accountTable, err := w.accountLinks(ctx, bizs)
	if err != nil {
		return result, err
	}
	rows := articleRows(articles, accounts)
	adopted, duplicates, err := w.adoptByLink(ctx, rows)
	if err != nil {
		return result, err
	}
	result.Articles, err = w.syncTable(ctx, datasetArticles, w.ArticleTableID, withAccount(articleFields, accountTable), rows)
	result.Articles.Adopted, result.Articles.Duplicates = adopted, duplicates
	if err != nil {
		return result, fmt.Errorf("article table: %w", err)
	}

	days, err := w.Store.BizDays(ctx, earliest(dayEndpoint), today)
	if err != nil {
		return result, err
	}
	ownAccount := ""
	if len(bizs) > 0 {
		ownAccount = accounts[bizs[len(bizs)-1]]
	}
	dayRows := dayRows(days, ownAccount)
	if len(dayRows) == 0 {
		return result, nil
	}
	tableID, err := w.dailyTableID(ctx)
	if err != nil {
		return result, err
	}
	result.Days, err = w.syncTable(ctx, datasetDays, tableID, withAccount(dayFields, accountTable), dayRows)
	if err != nil {
		return result, fmt.Errorf("daily table: %w", err)
	}
	return result, nil
}
