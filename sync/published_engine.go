package sync

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	stdsync "sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

const publishedArticleTableName = "公众号文章"

var publishedArticleFields = []feishu.FieldSpec{
	{Name: "文章标题", Type: feishu.FieldTypeText},
	{Name: "唯一键", Type: feishu.FieldTypeText},
	{Name: "文章ID", Type: feishu.FieldTypeText},
	{Name: "消息ID", Type: feishu.FieldTypeText},
	{Name: "文章位置", Type: feishu.FieldTypeNumber},
	{Name: "作者", Type: feishu.FieldTypeText},
	{Name: "摘要", Type: feishu.FieldTypeText},
	{Name: "文章链接", Type: feishu.FieldTypeURL},
	{Name: "封面素材ID", Type: feishu.FieldTypeText},
	{Name: "显示封面图", Type: feishu.FieldTypeNumber},
	{Name: "是否已删除", Type: feishu.FieldTypeNumber},
	{Name: "更新时间戳", Type: feishu.FieldTypeNumber},
	{Name: "更新时间", Type: feishu.FieldTypeDatetime},
	{Name: "发布日期", Type: feishu.FieldTypeDatetime},
	{Name: "发布月份", Type: feishu.FieldTypeText},
	{Name: "正文HTML", Type: feishu.FieldTypeText},
	{Name: "文章内容", Type: feishu.FieldTypeText},
	{Name: "正文来源", Type: feishu.FieldTypeText},
	{Name: "封面图链接", Type: feishu.FieldTypeURL},
	{Name: "正文图片链接", Type: feishu.FieldTypeText},
	{Name: "同步时间", Type: feishu.FieldTypeDatetime},
}

var publishedSyncMu stdsync.Mutex

type PublishedSyncResult struct {
	PagesScanned int `json:"pagesScanned"`
	RecordsSeen  int `json:"recordsSeen"`
	Created      int `json:"created"`
	Updated      int `json:"updated"`
}

func SyncPublishedArticles() (*PublishedSyncResult, error) {
	if !publishedSyncMu.TryLock() {
		log.Println("[PublishedSync] Already running, skipping this invocation")
		return &PublishedSyncResult{}, nil
	}
	defer publishedSyncMu.Unlock()

	tableID, err := PublishedArticleTableID()
	if err != nil {
		return nil, fmt.Errorf("get published article table: %w", err)
	}
	if err := ensurePublishedArticleFields(tableID); err != nil {
		return nil, fmt.Errorf("ensure published article fields: %w", err)
	}

	cursor, err := ReadCursor()
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		cursor = &SyncCursor{}
	}

	pageSize := envInt("WECHAT_PUBLISHED_PAGE_SIZE", 20)
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 20 {
		pageSize = 20
	}

	recentPages := envInt("WECHAT_PUBLISHED_RECENT_PAGES", 3)
	if recentPages <= 0 {
		recentPages = 3
	}

	backfillGrowPages := envInt("WECHAT_PUBLISHED_BACKFILL_GROW_PAGES", 5)
	if backfillGrowPages <= 0 {
		backfillGrowPages = 5
	}

	pagesToScan := recentPages
	if !cursor.PublishedBackfillComplete {
		if cursor.PublishedScannedPages > recentPages {
			pagesToScan = cursor.PublishedScannedPages + backfillGrowPages
		} else {
			pagesToScan = recentPages + backfillGrowPages
		}
	}

	log.Printf("[PublishedSync] Scanning %d pages of published articles (page_size=%d, historical_complete=%v)",
		pagesToScan, pageSize, cursor.PublishedBackfillComplete)

	records := make([]feishu.SyncRecord, 0, pagesToScan*pageSize)
	result := &PublishedSyncResult{}
	actualPages := 0
	backfillComplete := cursor.PublishedBackfillComplete

	for page := 0; page < pagesToScan; page++ {
		offset := page * pageSize
		batch, err := wechat.BatchGetPublishedArticles(offset, pageSize, false)
		if err != nil {
			return nil, fmt.Errorf("batchget published articles at offset=%d: %w", offset, err)
		}
		if len(batch.Item) == 0 {
			backfillComplete = true
			break
		}
		actualPages = page + 1

		for _, item := range batch.Item {
			for fallbackIndex, article := range item.Content.NewsItem {
				record, ok := toPublishedArticleRecord(item, article, fallbackIndex+1)
				if !ok {
					continue
				}
				records = append(records, record)
				result.RecordsSeen++
			}
		}

		if batch.TotalCount <= (page+1)*pageSize || len(batch.Item) < pageSize {
			backfillComplete = true
			break
		}
	}

	upsertResult, err := feishu.SyncRecordsUpsert(records, "唯一键", tableID)
	if err != nil {
		return nil, fmt.Errorf("upsert published articles: %w", err)
	}

	result.PagesScanned = actualPages
	result.Created = upsertResult.Created
	result.Updated = upsertResult.Updated

	if actualPages > cursor.PublishedScannedPages {
		cursor.PublishedScannedPages = actualPages
	}
	if backfillComplete {
		cursor.PublishedBackfillComplete = true
	}
	if err := WriteCursor(cursor); err != nil {
		return nil, fmt.Errorf("write cursor after published sync: %w", err)
	}

	log.Printf("[PublishedSync] Done: pages=%d records=%d created=%d updated=%d historical_complete=%v",
		result.PagesScanned, result.RecordsSeen, result.Created, result.Updated, cursor.PublishedBackfillComplete)

	return result, nil
}

func PublishedArticleTableID() (string, error) {
	return feishu.GetOrCreateTable(publishedArticleTableName, "文章标题")
}

func ensurePublishedArticleFields(tableID string) error {
	return feishu.EnsureFieldsExist(publishedArticleFields, tableID)
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func toPublishedArticleRecord(item wechat.PublishedArticleItem, article wechat.PublishedArticle, fallbackIndex int) (feishu.SyncRecord, bool) {
	if article.IsDeleted || strings.TrimSpace(article.URL) == "" {
		return feishu.SyncRecord{}, false
	}

	uniqueKey := wechat.MessageIDFromArticleURL(article.URL)
	if uniqueKey == "" {
		uniqueKey = strings.TrimSpace(item.ArticleID)
	}
	if uniqueKey == "" {
		uniqueKey = strings.TrimSpace(article.URL)
	}
	if uniqueKey == "" {
		return feishu.SyncRecord{}, false
	}

	fields := map[string]interface{}{
		"文章标题":   article.Title,
		"唯一键":    uniqueKey,
		"文章ID":   item.ArticleID,
		"同步时间":   nowMs(),
		"作者":     article.Author,
		"摘要":     article.Digest,
		"封面素材ID": article.ThumbMediaID,
		"显示封面图":  article.ShowCoverPic,
		"是否已删除":  boolToNumber(article.IsDeleted),
		"更新时间戳":  item.UpdateTime,
	}

	if msgID := wechat.MessageIDFromArticleURL(article.URL); msgID != "" {
		fields["消息ID"] = msgID
	}

	if idx := wechat.ArticleIndexFromURL(article.URL); idx != nil {
		fields["文章位置"] = *idx
	} else if fallbackIndex > 0 {
		fields["文章位置"] = fallbackIndex
	}

	fields["文章链接"] = map[string]string{"link": article.URL, "text": article.URL}

	var publishAt time.Time
	if item.UpdateTime > 0 {
		publishAt = time.Unix(item.UpdateTime, 0).In(wechat.ShanghaiLoc())
	}
	if os.Getenv("WECHAT_SYNC_COVER_INLINE") == "1" {
		if meta, err := wechat.FetchArticleMetadata(article.URL); err == nil {
			if meta.CoverURL != "" {
				coverURL := meta.CoverURL
				if mirroredCoverURL, mirrorErr := wechat.MirrorRemoteImage(meta.CoverURL); mirrorErr == nil && mirroredCoverURL != "" {
					coverURL = mirroredCoverURL
				} else if mirrorErr != nil {
					log.Printf("[PublishedSync] Cover mirror failed for %s: %v", uniqueKey, mirrorErr)
				}
				fields["封面图链接"] = map[string]string{"link": coverURL, "text": coverURL}
			}
			if !meta.PublishTime.IsZero() {
				publishAt = meta.PublishTime.In(wechat.ShanghaiLoc())
			}
		} else {
			log.Printf("[PublishedSync] Metadata fetch failed for %s: %v", article.URL, err)
		}
	}

	if !publishAt.IsZero() {
		dateStr := publishAt.Format("2006-01-02")
		fields["发布日期"] = publishAt.UnixMilli()
		fields["发布月份"] = toPublishMonth(dateStr)
		fields["更新时间"] = publishAt.UnixMilli()
	} else if item.UpdateTime > 0 {
		fields["更新时间"] = time.Unix(item.UpdateTime, 0).UnixMilli()
	}

	if htmlContent := trimForBitable(article.Content, 8000); htmlContent != "" {
		fields["正文HTML"] = htmlContent
	}
	if content := strings.TrimSpace(wechat.CleanHTMLToPlainText(article.Content)); content != "" {
		fields["文章内容"] = trimForBitable(content, 20000)
		fields["正文来源"] = "official_api"
	}
	if os.Getenv("WECHAT_SYNC_BODY_IMAGES_INLINE") == "1" {
		if imageURLs, err := wechat.MirrorArticleImages(article.Content); len(imageURLs) > 0 {
			fields["正文图片链接"] = strings.Join(imageURLs, ",")
			if err != nil {
				log.Printf("[PublishedSync] Image mirror partially failed for %s: %v", uniqueKey, err)
			}
		} else if err != nil {
			log.Printf("[PublishedSync] Image mirror failed for %s: %v", uniqueKey, err)
		}
	}

	return feishu.SyncRecord{
		UniqueKey: uniqueKey,
		Fields:    fields,
	}, true
}

func trimForBitable(s string, limit int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if limit <= 0 {
		limit = 4000
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "\n\n[内容已截断]"
}

func boolToNumber(v bool) int {
	if v {
		return 1
	}
	return 0
}
