package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	stdsync "sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/config"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// ==================== Field Specs ====================

var articleMasterFields = []feishu.FieldSpec{
	{Name: "文章标题", Type: feishu.FieldTypeText},
	{Name: "发布日期", Type: feishu.FieldTypeDatetime},
	{Name: "发布月份", Type: feishu.FieldTypeText},
	{Name: "消息ID", Type: feishu.FieldTypeText},
	{Name: "文章位置", Type: feishu.FieldTypeNumber},
	{Name: "文章链接", Type: feishu.FieldTypeURL},
	{Name: "更新时间", Type: feishu.FieldTypeDatetime},
}

var dailyArticleDataFields = []feishu.FieldSpec{
	{Name: "唯一键", Type: feishu.FieldTypeText},
	{Name: "消息ID", Type: feishu.FieldTypeText},
	{Name: "日期", Type: feishu.FieldTypeDatetime},
	{Name: "图文页阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "图文页阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "原文页阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "原文页阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "分享人数", Type: feishu.FieldTypeNumber},
	{Name: "分享次数", Type: feishu.FieldTypeNumber},
	{Name: "收藏人数", Type: feishu.FieldTypeNumber},
	{Name: "收藏次数", Type: feishu.FieldTypeNumber},
	{Name: "送达人数", Type: feishu.FieldTypeNumber},
	{Name: "会话阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "会话阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "历史消息阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "历史消息阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "朋友圈阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "朋友圈阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "好友转发阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "好友转发阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "其他来源阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "其他来源阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "会话转发分享人数", Type: feishu.FieldTypeNumber},
	{Name: "会话转发分享次数", Type: feishu.FieldTypeNumber},
	{Name: "朋友圈转发分享人数", Type: feishu.FieldTypeNumber},
	{Name: "朋友圈转发分享次数", Type: feishu.FieldTypeNumber},
	{Name: "其他转发分享人数", Type: feishu.FieldTypeNumber},
	{Name: "其他转发分享次数", Type: feishu.FieldTypeNumber},
	{Name: "更新时间", Type: feishu.FieldTypeDatetime},
}

var userGrowthFields = []feishu.FieldSpec{
	{Name: "日期", Type: feishu.FieldTypeDatetime},
	{Name: "用户渠道", Type: feishu.FieldTypeText},
	{Name: "渠道编号", Type: feishu.FieldTypeNumber},
	{Name: "新关注人数", Type: feishu.FieldTypeNumber},
	{Name: "取消关注人数", Type: feishu.FieldTypeNumber},
	{Name: "净增人数", Type: feishu.FieldTypeNumber},
	{Name: "累计关注人数", Type: feishu.FieldTypeNumber},
	{Name: "唯一键", Type: feishu.FieldTypeText},
	{Name: "更新时间", Type: feishu.FieldTypeDatetime},
}

var userReadFields = []feishu.FieldSpec{
	{Name: "日期", Type: feishu.FieldTypeDatetime},
	{Name: "流量来源", Type: feishu.FieldTypeText},
	{Name: "来源编号", Type: feishu.FieldTypeNumber},
	{Name: "图文页阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "图文页阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "原文页阅读人数", Type: feishu.FieldTypeNumber},
	{Name: "原文页阅读次数", Type: feishu.FieldTypeNumber},
	{Name: "分享人数", Type: feishu.FieldTypeNumber},
	{Name: "分享次数", Type: feishu.FieldTypeNumber},
	{Name: "收藏人数", Type: feishu.FieldTypeNumber},
	{Name: "收藏次数", Type: feishu.FieldTypeNumber},
	{Name: "唯一键", Type: feishu.FieldTypeText},
	{Name: "更新时间", Type: feishu.FieldTypeDatetime},
}

var userShareFields = []feishu.FieldSpec{
	{Name: "日期", Type: feishu.FieldTypeDatetime},
	{Name: "分享场景", Type: feishu.FieldTypeText},
	{Name: "场景编号", Type: feishu.FieldTypeNumber},
	{Name: "分享人数", Type: feishu.FieldTypeNumber},
	{Name: "分享次数", Type: feishu.FieldTypeNumber},
	{Name: "唯一键", Type: feishu.FieldTypeText},
	{Name: "更新时间", Type: feishu.FieldTypeDatetime},
}

// ==================== Field Mappers ====================

func extractArticleIndex(msgid string) *int {
	parts := strings.Split(msgid, "_")
	if len(parts) >= 2 {
		if idx, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return &idx
		}
	}
	return nil
}

func toDateMs(dateStr string) int64 {
	t, err := wechat.ParseDate(dateStr)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func toPublishMonth(dateStr string) string {
	t, err := wechat.ParseDate(dateStr)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%02d", t.Year(), t.Month())
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func toArticleMasterFields(item wechat.ArticleSummaryItem, totalItem *wechat.ArticleTotalItem) map[string]interface{} {
	fields := map[string]interface{}{
		"文章标题": item.Title,
		"发布日期": toDateMs(item.RefDate),
		"发布月份": toPublishMonth(item.RefDate),
		"消息ID": item.MsgID,
		"更新时间": nowMs(),
	}

	if idx := extractArticleIndex(item.MsgID); idx != nil {
		fields["文章位置"] = *idx
	}
	if totalItem != nil && totalItem.URL != "" {
		fields["文章链接"] = map[string]string{"link": totalItem.URL, "text": totalItem.URL}
	}

	return fields
}

func toDailyArticleDataFields(item wechat.ArticleSummaryItem, totalItem *wechat.ArticleTotalItem) map[string]interface{} {
	uniqueKey := item.MsgID + "_" + item.RefDate

	fields := map[string]interface{}{
		"唯一键":     uniqueKey,
		"消息ID":    item.MsgID,
		"日期":      toDateMs(item.RefDate),
		"图文页阅读人数": item.IntPageReadUser,
		"图文页阅读次数": item.IntPageReadCount,
		"原文页阅读人数": item.OriPageReadUser,
		"原文页阅读次数": item.OriPageReadCount,
		"分享人数":    item.ShareUser,
		"分享次数":    item.ShareCount,
		"收藏人数":    item.AddToFavUser,
		"收藏次数":    item.AddToFavCount,
		"更新时间":    nowMs(),
	}

	if totalItem != nil && len(totalItem.Details) > 0 {
		d := totalItem.Details[len(totalItem.Details)-1]
		fields["送达人数"] = d.TargetUser
		fields["会话阅读人数"] = d.IntPageFromSessionReadUser
		fields["会话阅读次数"] = d.IntPageFromSessionReadCount
		fields["历史消息阅读人数"] = d.IntPageFromHistMsgReadUser
		fields["历史消息阅读次数"] = d.IntPageFromHistMsgReadCount
		fields["朋友圈阅读人数"] = d.IntPageFromFeedReadUser
		fields["朋友圈阅读次数"] = d.IntPageFromFeedReadCount
		fields["好友转发阅读人数"] = d.IntPageFromFriendsReadUser
		fields["好友转发阅读次数"] = d.IntPageFromFriendsReadCount
		fields["其他来源阅读人数"] = d.IntPageFromOtherReadUser
		fields["其他来源阅读次数"] = d.IntPageFromOtherReadCount
		fields["会话转发分享人数"] = d.FeedShareFromSessionUser
		fields["会话转发分享次数"] = d.FeedShareFromSessionCnt
		fields["朋友圈转发分享人数"] = d.FeedShareFromFeedUser
		fields["朋友圈转发分享次数"] = d.FeedShareFromFeedCnt
		fields["其他转发分享人数"] = d.FeedShareFromOtherUser
		fields["其他转发分享次数"] = d.FeedShareFromOtherCnt
	}

	return fields
}

func toUserGrowthFields(item wechat.UserSummaryItem, cumulateUser *int) map[string]interface{} {
	uniqueKey := fmt.Sprintf("%s_%d", item.RefDate, item.UserSource)

	fields := map[string]interface{}{
		"日期":     toDateMs(item.RefDate),
		"用户渠道":   feishu.LabelOrUnknown(feishu.UserSourceLabels, item.UserSource),
		"渠道编号":   item.UserSource,
		"新关注人数":  item.NewUser,
		"取消关注人数": item.CancelUser,
		"净增人数":   item.NewUser - item.CancelUser,
		"唯一键":    uniqueKey,
		"更新时间":   nowMs(),
	}

	if cumulateUser != nil {
		fields["累计关注人数"] = *cumulateUser
	}

	return fields
}

func toUserReadFields(item wechat.UserReadItem) map[string]interface{} {
	uniqueKey := fmt.Sprintf("%s_%d", item.RefDate, item.UserSource)

	return map[string]interface{}{
		"日期":      toDateMs(item.RefDate),
		"流量来源":    feishu.LabelOrUnknown(feishu.ReadSourceLabels, item.UserSource),
		"来源编号":    item.UserSource,
		"图文页阅读人数": item.IntPageReadUser,
		"图文页阅读次数": item.IntPageReadCount,
		"原文页阅读人数": item.OriPageReadUser,
		"原文页阅读次数": item.OriPageReadCount,
		"分享人数":    item.ShareUser,
		"分享次数":    item.ShareCount,
		"收藏人数":    item.AddToFavUser,
		"收藏次数":    item.AddToFavCount,
		"唯一键":     uniqueKey,
		"更新时间":    nowMs(),
	}
}

func toUserShareFields(item wechat.UserShareItem) map[string]interface{} {
	uniqueKey := fmt.Sprintf("%s_%d", item.RefDate, item.ShareScene)

	return map[string]interface{}{
		"日期":   toDateMs(item.RefDate),
		"分享场景": feishu.LabelOrUnknown(feishu.ShareSceneLabels, item.ShareScene),
		"场景编号": item.ShareScene,
		"分享人数": item.ShareUser,
		"分享次数": item.ShareCount,
		"唯一键":  uniqueKey,
		"更新时间": nowMs(),
	}
}

// ==================== Sync Functions ====================

type ArticleSyncResult struct {
	Master *feishu.SyncResult `json:"master"`
	Daily  *feishu.SyncResult `json:"daily"`
}

func SyncArticles(beginDate, endDate string) (*ArticleSyncResult, error) {
	summaryJSON, err := wechat.CallWechatAPI("getarticlesummary", beginDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("getarticlesummary: %w", err)
	}
	totalJSON, err := wechat.CallWechatAPI("getarticletotal", beginDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("getarticletotal: %w", err)
	}

	var summaryItems []wechat.ArticleSummaryItem
	if err := json.Unmarshal(summaryJSON, &summaryItems); err != nil {
		return nil, fmt.Errorf("parse summary: %w", err)
	}

	var totalItems []wechat.ArticleTotalItem
	if err := json.Unmarshal(totalJSON, &totalItems); err != nil {
		return nil, fmt.Errorf("parse total: %w", err)
	}

	totalMap := make(map[string]*wechat.ArticleTotalItem)
	for i := range totalItems {
		totalMap[totalItems[i].MsgID] = &totalItems[i]
	}

	var masterRecords []feishu.SyncRecord
	var dailyRecords []feishu.SyncRecord

	for _, item := range summaryItems {
		totalItem := totalMap[item.MsgID]

		masterRecords = append(masterRecords, feishu.SyncRecord{
			UniqueKey: item.MsgID,
			Fields:    toArticleMasterFields(item, totalItem),
		})

		dailyRecords = append(dailyRecords, feishu.SyncRecord{
			UniqueKey: item.MsgID + "_" + item.RefDate,
			Fields:    toDailyArticleDataFields(item, totalItem),
		})
	}

	tableID := config.Env.FeishuBitableTableID
	if err := feishu.EnsureFieldsExist(articleMasterFields, tableID); err != nil {
		return nil, fmt.Errorf("ensure master fields: %w", err)
	}
	masterResult, err := feishu.SyncRecordsInsertOnly(masterRecords, "消息ID", tableID)
	if err != nil {
		return nil, fmt.Errorf("sync master: %w", err)
	}

	dailyTableID, err := feishu.GetOrCreateTable("每日文章数据")
	if err != nil {
		return nil, fmt.Errorf("get daily table: %w", err)
	}
	if err := feishu.EnsureFieldsExist(dailyArticleDataFields, dailyTableID); err != nil {
		return nil, fmt.Errorf("ensure daily fields: %w", err)
	}
	dailyResult, err := feishu.SyncRecordsInsertOnly(dailyRecords, "唯一键", dailyTableID)
	if err != nil {
		return nil, fmt.Errorf("sync daily: %w", err)
	}

	return &ArticleSyncResult{Master: masterResult, Daily: dailyResult}, nil
}

type UpsertSyncResult struct {
	Total   int `json:"total"`
	Created int `json:"created"`
	Updated int `json:"updated"`
}

func SyncUsers(beginDate, endDate string) (*UpsertSyncResult, error) {
	summaryJSON, err := wechat.CallWechatAPI("getusersummary", beginDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("getusersummary: %w", err)
	}
	cumulateJSON, err := wechat.CallWechatAPI("getusercumulate", beginDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("getusercumulate: %w", err)
	}

	var summaryItems []wechat.UserSummaryItem
	if err := json.Unmarshal(summaryJSON, &summaryItems); err != nil {
		return nil, fmt.Errorf("parse user summary: %w", err)
	}

	var cumulateItems []wechat.UserCumulateItem
	if err := json.Unmarshal(cumulateJSON, &cumulateItems); err != nil {
		return nil, fmt.Errorf("parse user cumulate: %w", err)
	}

	cumulateMap := make(map[string]int)
	for _, item := range cumulateItems {
		key := fmt.Sprintf("%s_%d", item.RefDate, item.UserSource)
		cumulateMap[key] = item.CumulateUser
	}

	var records []feishu.SyncRecord
	for _, item := range summaryItems {
		key := fmt.Sprintf("%s_%d", item.RefDate, item.UserSource)
		var cu *int
		if v, ok := cumulateMap[key]; ok {
			cu = &v
		}
		records = append(records, feishu.SyncRecord{
			UniqueKey: key,
			Fields:    toUserGrowthFields(item, cu),
		})
	}

	tableID, err := feishu.GetOrCreateTable("粉丝增长")
	if err != nil {
		return nil, err
	}
	if err := feishu.EnsureFieldsExist(userGrowthFields, tableID); err != nil {
		return nil, err
	}
	r, err := feishu.SyncRecordsUpsert(records, "唯一键", tableID)
	if err != nil {
		return nil, err
	}

	return &UpsertSyncResult{Total: len(records), Created: r.Created, Updated: r.Updated}, nil
}

func SyncReads(beginDate, endDate string) (*UpsertSyncResult, error) {
	readJSON, err := wechat.CallWechatAPI("getuserread", beginDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("getuserread: %w", err)
	}

	var readItems []wechat.UserReadItem
	if err := json.Unmarshal(readJSON, &readItems); err != nil {
		return nil, fmt.Errorf("parse user read: %w", err)
	}

	var records []feishu.SyncRecord
	for _, item := range readItems {
		uniqueKey := fmt.Sprintf("%s_%d", item.RefDate, item.UserSource)
		records = append(records, feishu.SyncRecord{
			UniqueKey: uniqueKey,
			Fields:    toUserReadFields(item),
		})
	}

	tableID, err := feishu.GetOrCreateTable("每日阅读概况")
	if err != nil {
		return nil, err
	}
	if err := feishu.EnsureFieldsExist(userReadFields, tableID); err != nil {
		return nil, err
	}
	r, err := feishu.SyncRecordsUpsert(records, "唯一键", tableID)
	if err != nil {
		return nil, err
	}

	return &UpsertSyncResult{Total: len(records), Created: r.Created, Updated: r.Updated}, nil
}

func SyncShares(beginDate, endDate string) (*UpsertSyncResult, error) {
	shareJSON, err := wechat.CallWechatAPI("getusershare", beginDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("getusershare: %w", err)
	}

	var shareItems []wechat.UserShareItem
	if err := json.Unmarshal(shareJSON, &shareItems); err != nil {
		return nil, fmt.Errorf("parse user share: %w", err)
	}

	var records []feishu.SyncRecord
	for _, item := range shareItems {
		uniqueKey := fmt.Sprintf("%s_%d", item.RefDate, item.ShareScene)
		records = append(records, feishu.SyncRecord{
			UniqueKey: uniqueKey,
			Fields:    toUserShareFields(item),
		})
	}

	tableID, err := feishu.GetOrCreateTable("分享场景")
	if err != nil {
		return nil, err
	}
	if err := feishu.EnsureFieldsExist(userShareFields, tableID); err != nil {
		return nil, err
	}
	r, err := feishu.SyncRecordsUpsert(records, "唯一键", tableID)
	if err != nil {
		return nil, err
	}

	return &UpsertSyncResult{Total: len(records), Created: r.Created, Updated: r.Updated}, nil
}

// ==================== Full Sync ====================

type FullSyncResult struct {
	Articles interface{} `json:"articles,omitempty"`
	Users    interface{} `json:"users,omitempty"`
	Reads    interface{} `json:"reads,omitempty"`
	Shares   interface{} `json:"shares,omitempty"`
}

func RunFullSync(beginDate, endDate string) (*FullSyncResult, error) {
	result := &FullSyncResult{}
	var mu stdsync.Mutex
	var quotaErr error

	type task struct {
		name string
		fn   func() (interface{}, error)
	}

	tasks := []task{
		{"articles", func() (interface{}, error) { return SyncArticles(beginDate, endDate) }},
		{"users", func() (interface{}, error) { return SyncUsers(beginDate, endDate) }},
		{"reads", func() (interface{}, error) { return SyncReads(beginDate, endDate) }},
		{"shares", func() (interface{}, error) { return SyncShares(beginDate, endDate) }},
	}

	var wg stdsync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		go func(t task) {
			defer wg.Done()
			r, err := t.fn()

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				if _, ok := err.(*wechat.QuotaLimitError); ok {
					quotaErr = err
					return
				}
				log.Printf("[Scheduler] %s sync failed: %v", t.name, err)
				errVal := map[string]string{"error": err.Error()}
				switch t.name {
				case "articles":
					result.Articles = errVal
				case "users":
					result.Users = errVal
				case "reads":
					result.Reads = errVal
				case "shares":
					result.Shares = errVal
				}
				return
			}
			switch t.name {
			case "articles":
				result.Articles = r
			case "users":
				result.Users = r
			case "reads":
				result.Reads = r
			case "shares":
				result.Shares = r
			}
		}(t)
	}
	wg.Wait()

	if quotaErr != nil {
		return nil, quotaErr
	}
	return result, nil
}

// IsAllEmpty checks if all sync results are empty (no new data).
func IsAllEmpty(result *FullSyncResult) bool {
	if result.Articles != nil {
		if a, ok := result.Articles.(*ArticleSyncResult); ok {
			if a.Master.Created > 0 || a.Daily.Created > 0 {
				return false
			}
		}
	}
	for _, v := range []interface{}{result.Users, result.Reads, result.Shares} {
		if u, ok := v.(*UpsertSyncResult); ok {
			if u.Total > 0 {
				return false
			}
		}
	}
	return true
}
