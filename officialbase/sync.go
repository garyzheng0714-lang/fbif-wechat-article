package officialbase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/archive"
	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
)

type Dataset struct {
	Key          string
	TableName    string
	PrimaryField string
	Fields       []feishu.FieldSpec
}

var datasets = []Dataset{
	{
		Key:          archive.BaseDatasetArticles,
		TableName:    "文章主档",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("消息ID"), text("消息数据ID"), number("文章位置"), datetime("发布日期"),
			number("发表类型"), text("文章标题"), urlField("文章链接"), text("文章ID"), text("作者"),
			text("摘要"), text("正文HTML"), urlField("原文链接"), text("封面素材ID"), urlField("封面图链接"),
			text("文章类型"), number("是否删除"), text("发布原始JSON"), text("正文原始JSON"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetArticleDaily,
		TableName:    "文章每日指标",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), datetime("统计日期"), text("消息ID"), text("文章标题"), urlField("文章链接"),
			number("阅读人数"), text("阅读来源JSON"), number("分享人数"), text("阅读原始JSON"), text("分享原始JSON"),
			datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetArticleCumulative,
		TableName:    "文章累计指标",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("消息ID"), text("消息数据ID"), number("文章位置"), datetime("发布日期"),
			datetime("统计日期"), text("文章标题"), urlField("文章链接"), text("文章ID"), number("阅读人数"),
			number("阅读次数"), number("原文阅读人数"), number("原文阅读次数"), text("阅读来源JSON"), number("分享人数"),
			number("分享次数"), number("收藏人数旧口径"), number("收藏次数旧口径"), number("爱心赞人数"), number("拇指赞人数"),
			number("留言条数"), number("微信收藏人数"), number("赞赏金额分"), number("阅读后关注人数"), number("阅读送达率"),
			number("阅读完成率"), number("平均阅读时长分钟"), text("跳出位置JSON"), text("原始JSON"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetAccountDaily,
		TableName:    "账号内容日报",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), datetime("统计日期"), number("阅读人数"), text("阅读来源JSON"), number("分享人数"),
			number("爱心赞人数"), number("拇指赞人数"), number("留言条数"), number("微信收藏人数"), number("跳转原文人数"),
			number("发布篇数"), text("原始JSON"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetFollowerSource,
		TableName:    "粉丝来源日报",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), datetime("统计日期"), number("来源代码"), text("来源名称"), number("新增粉丝"),
			number("取关粉丝"), number("净增粉丝"), text("原始JSON"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetFollowerCumulative,
		TableName:    "粉丝累计日报",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), datetime("统计日期"), number("累计粉丝"), text("原始JSON"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetMessageMetrics,
		TableName:    "消息互动指标",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("接口"), text("统计粒度"), datetime("统计日期"), number("统计小时"),
			number("用户来源代码"), number("消息类型代码"), text("消息类型"), number("消息数量区间"),
			number("上行消息人数"), number("上行消息条数"), text("维度JSON"), text("原始JSON"),
			datetime("首次获取时间"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetInterfaceMetrics,
		TableName:    "接口性能指标",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("接口"), text("统计粒度"), datetime("统计日期"), number("统计小时"),
			number("回调次数"), number("失败次数"), number("总耗时毫秒"), number("最大耗时毫秒"),
			text("维度JSON"), text("原始JSON"), datetime("首次获取时间"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetContentAssets,
		TableName:    "内容资产主档",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("内容来源"), text("对象ID"), datetime("内容更新时间"), text("列表原始JSON"),
			text("详情类型"), number("详情字节数"), text("详情原始JSON"), datetime("详情获取时间"),
			datetime("首次获取时间"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetContentArticles,
		TableName:    "内容条目主档",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("内容来源"), text("对象ID"), number("条目位置"), text("文章类型"), text("消息ID"),
			text("文章标题"), text("作者"), text("摘要"), text("正文HTML"), urlField("原文链接"), urlField("文章链接"),
			text("封面素材ID"), urlField("封面图链接"), number("是否删除"), text("原始JSON"),
			datetime("首次获取时间"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetComments,
		TableName:    "文章评论",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("消息数据ID"), number("文章位置"), text("评论ID"), datetime("评论时间"),
			text("评论内容"), number("评论类型"), text("评论者OpenID"), text("回复内容"), datetime("回复时间"),
			text("原始JSON"), datetime("首次获取时间"), datetime("来源更新时间"),
		),
	},
	{
		Key:          archive.BaseDatasetAPIFetches,
		TableName:    "API调用日志",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), number("调用ID"), text("接口"), text("分类"), text("开始日期"), text("结束日期"),
			text("请求JSON"), text("响应SHA256"), number("响应引用ID"), number("HTTP状态码"), number("微信错误码"),
			text("微信错误信息"), number("是否成功"), text("内部错误"), text("响应类型"), number("响应字节数"),
			text("响应原始JSON"), datetime("调用时间"),
		),
	},
	{
		Key:          archive.BaseDatasetSyncStatus,
		TableName:    "接口同步状态",
		PrimaryField: "唯一键",
		Fields: fields(
			text("唯一键"), text("数据集"), text("类型"), text("分类"), text("下次回填位置"), number("总数"),
			number("是否完成"), datetime("最后成功时间"), text("最后错误"), number("连续失败次数"), datetime("状态更新时间"),
		),
	},
}

func fields(values ...feishu.FieldSpec) []feishu.FieldSpec { return values }
func text(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeText}
}
func number(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeNumber}
}
func datetime(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeDatetime}
}
func urlField(name string) feishu.FieldSpec {
	return feishu.FieldSpec{Name: name, Type: feishu.FieldTypeURL}
}

type DatasetResult struct {
	Scanned   int `json:"scanned"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
}

type RunResult struct {
	StartedAt  int64                    `json:"startedAt"`
	FinishedAt int64                    `json:"finishedAt"`
	Datasets   map[string]DatasetResult `json:"datasets"`
	Errors     map[string]string        `json:"errors,omitempty"`
}

type Syncer struct {
	Store          *archive.Store
	RowsPerDataset int
	Now            func() time.Time

	runMu sync.Mutex
}

type preparedCandidate struct {
	archive.BaseSyncCandidate
	Hash string
}

func (s *Syncer) Sync(ctx context.Context) (*RunResult, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("official Base sync store is not configured")
	}
	if !s.runMu.TryLock() {
		return nil, fmt.Errorf("official Base sync is already running")
	}
	defer s.runMu.Unlock()

	now := s.now()
	result := &RunResult{
		StartedAt: now.UnixMilli(),
		Datasets:  make(map[string]DatasetResult, len(datasets)),
		Errors:    make(map[string]string),
	}
	limit := s.RowsPerDataset
	if limit <= 0 {
		limit = 500
	}
	var runErrors []error
	for _, dataset := range datasets {
		datasetResult, err := s.syncDataset(ctx, dataset, limit)
		result.Datasets[dataset.Key] = datasetResult
		if err != nil {
			result.Errors[dataset.Key] = err.Error()
			runErrors = append(runErrors, fmt.Errorf("%s: %w", dataset.Key, err))
		}
	}
	result.FinishedAt = s.now().UnixMilli()
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, errors.Join(runErrors...)
}

func (s *Syncer) syncDataset(ctx context.Context, dataset Dataset, limit int) (DatasetResult, error) {
	var result DatasetResult
	tableID, err := feishu.GetOrCreateTable(dataset.TableName, dataset.PrimaryField)
	if err != nil {
		return result, fmt.Errorf("get or create table: %w", err)
	}
	if err := feishu.EnsureFieldsExist(dataset.Fields, tableID); err != nil {
		return result, fmt.Errorf("ensure fields: %w", err)
	}

	mapped, err := s.Store.BaseRecordCount(ctx, dataset.Key)
	if err != nil {
		return result, err
	}
	unresolved, err := s.Store.BaseUnresolvedRecordCount(ctx, dataset.Key)
	if err != nil {
		return result, err
	}
	if mapped == 0 || unresolved > 0 {
		existing, err := feishu.GetExistingRecords(dataset.PrimaryField, tableID)
		if err != nil {
			return result, fmt.Errorf("bootstrap existing records: %w", err)
		}
		for rowKey, recordID := range existing {
			if err := s.Store.SeedBaseRecord(ctx, dataset.Key, rowKey, recordID); err != nil {
				return result, err
			}
		}
	}

	candidates, err := s.Store.ListBaseSyncCandidates(ctx, dataset.Key, limit)
	if err != nil {
		return result, err
	}
	result.Scanned = len(candidates)
	fieldTypes := make(map[string]int, len(dataset.Fields))
	for _, field := range dataset.Fields {
		fieldTypes[field.Name] = field.Type
	}

	creates := make([]preparedCandidate, 0)
	updates := make([]preparedCandidate, 0)
	for _, candidate := range candidates {
		prepareFields(candidate.Fields, fieldTypes)
		hash := payloadHash(candidate.Fields)
		prepared := preparedCandidate{BaseSyncCandidate: candidate, Hash: hash}
		if candidate.RecordID != "" && candidate.PayloadSHA256 == hash {
			if err := s.Store.SaveBaseRecord(ctx, dataset.Key, candidate.RowKey, candidate.RecordID, hash, candidate.SourceSeenAt, s.now()); err != nil {
				return result, err
			}
			result.Unchanged++
			continue
		}
		if candidate.RecordID == "" {
			creates = append(creates, prepared)
		} else {
			updates = append(updates, prepared)
		}
	}

	if len(creates) > 0 {
		records := make([]map[string]interface{}, len(creates))
		for index, candidate := range creates {
			records[index] = map[string]interface{}{"fields": candidate.Fields}
		}
		recordIDs, err := feishu.BatchCreateByRecordFieldsWithIDs(tableID, records)
		if err != nil {
			for index, recordID := range recordIDs {
				candidate := creates[index]
				if saveErr := s.Store.SaveBaseRecord(ctx, dataset.Key, candidate.RowKey, recordID, candidate.Hash, candidate.SourceSeenAt, s.now()); saveErr != nil {
					return result, errors.Join(err, saveErr)
				}
				result.Created++
			}
			for _, candidate := range creates[len(recordIDs):] {
				_ = s.Store.MarkBaseRecordError(ctx, dataset.Key, candidate.RowKey, err)
			}
			return result, err
		}
		for index, candidate := range creates {
			if err := s.Store.SaveBaseRecord(ctx, dataset.Key, candidate.RowKey, recordIDs[index], candidate.Hash, candidate.SourceSeenAt, s.now()); err != nil {
				return result, err
			}
		}
		result.Created = len(creates)
	}

	if len(updates) > 0 {
		records := make([]map[string]interface{}, len(updates))
		for index, candidate := range updates {
			records[index] = map[string]interface{}{
				"record_id": candidate.RecordID,
				"fields":    candidate.Fields,
			}
		}
		if err := feishu.BatchUpdateByRecordID(tableID, records); err != nil {
			for _, candidate := range updates {
				_ = s.Store.MarkBaseRecordError(ctx, dataset.Key, candidate.RowKey, err)
			}
			return result, err
		}
		for _, candidate := range updates {
			if err := s.Store.SaveBaseRecord(ctx, dataset.Key, candidate.RowKey, candidate.RecordID, candidate.Hash, candidate.SourceSeenAt, s.now()); err != nil {
				return result, err
			}
		}
		result.Updated = len(updates)
	}
	return result, nil
}

func prepareFields(values map[string]interface{}, fieldTypes map[string]int) {
	for name, value := range values {
		fieldType, known := fieldTypes[name]
		if !known || value == nil {
			delete(values, name)
			continue
		}
		switch fieldType {
		case feishu.FieldTypeURL:
			textValue := strings.TrimSpace(fmt.Sprintf("%v", value))
			if textValue == "" {
				delete(values, name)
				continue
			}
			values[name] = map[string]string{"link": textValue, "text": textValue}
		case feishu.FieldTypeText:
			textValue := value
			switch value.(type) {
			case map[string]interface{}, []interface{}:
				encoded, _ := json.Marshal(value)
				textValue = string(encoded)
			}
			values[name] = truncateText(fmt.Sprintf("%v", textValue), 99000)
		case feishu.FieldTypeDatetime:
			switch typed := value.(type) {
			case float64:
				if typed <= 0 {
					delete(values, name)
				} else {
					values[name] = int64(typed)
				}
			case int64:
				if typed <= 0 {
					delete(values, name)
				}
			}
		}
	}
}

func truncateText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	hash := sha256.Sum256([]byte(value))
	suffix := "\n[Base 单元格上限截断；SQLite 保留完整值；sha256=" + hex.EncodeToString(hash[:]) + "]"
	return string(runes[:limit-len([]rune(suffix))]) + suffix
}

func payloadHash(fields map[string]interface{}) string {
	encoded, _ := json.Marshal(fields)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func Enabled() bool {
	value := strings.TrimSpace(os.Getenv("ENABLE_OFFICIAL_BASE_SYNC"))
	return value == "1" || strings.EqualFold(value, "true")
}

func NewFromEnv(store *archive.Store) *Syncer {
	return &Syncer{
		Store:          store,
		RowsPerDataset: envInt("OFFICIAL_BASE_SYNC_ROWS_PER_DATASET", 500),
	}
}

func Start(stopCh <-chan struct{}, syncer *Syncer) {
	if syncer == nil {
		return
	}
	initialDelay := time.Duration(envInt("OFFICIAL_BASE_SYNC_INITIAL_DELAY_SECONDS", 20)) * time.Second
	interval := time.Duration(envInt("OFFICIAL_BASE_SYNC_INTERVAL_MINUTES", 5)) * time.Minute
	go func() {
		timer := time.NewTimer(initialDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-stopCh:
			return
		}
		for {
			result, err := syncer.Sync(context.Background())
			if result != nil {
				log.Printf("[OfficialBase] datasets=%d started=%d finished=%d", len(result.Datasets), result.StartedAt, result.FinishedAt)
			}
			if err != nil {
				log.Printf("[OfficialBase] sync completed with errors: %v", err)
			}
			timer.Reset(interval)
			select {
			case <-timer.C:
			case <-stopCh:
				return
			}
		}
	}()
	log.Printf("[OfficialBase] worker started: interval=%s rows_per_dataset=%d", interval, syncer.RowsPerDataset)
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
