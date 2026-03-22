package sync

import (
	"fmt"
	"log"
	"os"
	"strings"
	stdsync "sync"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

var historyWorkerMu stdsync.Mutex

func StartHistoryWorker(stopCh <-chan struct{}) {
	if !historyWorkerEnabled() {
		log.Println("[HistoryWorker] Disabled")
		return
	}

	go func() {
		timer := time.NewTimer(historyWorkerInitialDelay())
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				if err := RunMaterialHistoryBackfill(); err != nil {
					log.Printf("[HistoryWorker] Run failed: %v", err)
				}
				timer.Reset(historyWorkerInterval())
			case <-stopCh:
				log.Println("[HistoryWorker] Stopped")
				return
			}
		}
	}()

	log.Printf("[HistoryWorker] Started: interval=%s initial_delay=%s",
		historyWorkerInterval().Round(time.Second), historyWorkerInitialDelay().Round(time.Second))
}

func RunMaterialHistoryBackfill() error {
	if !historyWorkerMu.TryLock() {
		log.Println("[HistoryWorker] Already running, skipping")
		return nil
	}
	defer historyWorkerMu.Unlock()

	cursor, err := ReadCursor()
	if err != nil {
		return err
	}
	if cursor == nil {
		cursor = &SyncCursor{}
	}
	if cursor.MaterialBackfillComplete {
		log.Println("[HistoryWorker] Material history already complete")
		return nil
	}

	counts, err := wechat.GetMaterialCount()
	if err != nil {
		return err
	}
	total := counts.NewsCount
	if total == 0 {
		cursor.MaterialBackfillComplete = true
		return WriteCursor(cursor)
	}

	tableID, err := PublishedArticleTableID()
	if err != nil {
		return err
	}
	if err := ensurePublishedArticleFields(tableID); err != nil {
		return err
	}
	existingByKey, err := feishu.GetExistingRecords("唯一键", tableID)
	if err != nil {
		return fmt.Errorf("load existing keys for history backfill: %w", err)
	}
	createdInRun := make(map[string]struct{})

	pageSize := envIntDefault("MATERIAL_HISTORY_PAGE_SIZE", 20)
	maxCalls := envIntDefault("MATERIAL_HISTORY_MAX_CALLS_PER_RUN", 400)
	offset := cursor.MaterialNewsOffset
	calls := 0
	totalRecords := 0

	for offset < total && calls < maxCalls {
		batch, err := wechat.BatchGetMaterialNews(offset, pageSize)
		if err != nil {
			if _, ok := err.(*wechat.QuotaLimitError); ok {
				log.Println("[HistoryWorker] Quota reserve reached, pausing history backfill")
				break
			}
			return err
		}
		if len(batch.Item) == 0 {
			cursor.MaterialBackfillComplete = true
			break
		}

		records := make([]feishu.SyncRecord, 0, len(batch.Item)*2)
		for _, item := range batch.Item {
			for idx, article := range item.Content.NewsItem {
				record, ok := toMaterialArticleRecord(item, article, idx+1)
				if ok {
					records = append(records, record)
				}
			}
		}
		records = dedupeSyncRecords(records)
		created, updated, err := upsertMaterialRecords(records, tableID, existingByKey, createdInRun)
		if err != nil {
			if isRetryableHistoryBackfillError(err) {
				log.Printf("[HistoryWorker] Transient error at offset=%d, will resume later: %v", offset, err)
				break
			}
			return fmt.Errorf("sync material history: %w", err)
		}

		offset += len(batch.Item)
		calls++
		totalRecords += created + updated
		cursor.MaterialNewsOffset = offset
		if offset >= batch.TotalCount || offset >= total {
			cursor.MaterialBackfillComplete = true
		}
		if err := WriteCursor(cursor); err != nil {
			return err
		}

		time.Sleep(historyWorkerWritePause())
	}

	log.Printf("[HistoryWorker] Material backfill progress: offset=%d/%d records_upserted=%d calls=%d complete=%v",
		cursor.MaterialNewsOffset, total, totalRecords, calls, cursor.MaterialBackfillComplete)
	return nil
}

func toMaterialArticleRecord(item wechat.MaterialNewsItem, article wechat.MaterialNewsArticle, fallbackIndex int) (feishu.SyncRecord, bool) {
	if strings.TrimSpace(article.URL) == "" {
		return feishu.SyncRecord{}, false
	}

	uniqueKey := wechat.MessageIDFromArticleURL(article.URL)
	if uniqueKey == "" {
		uniqueKey = strings.TrimSpace(item.MediaID)
		if uniqueKey != "" {
			uniqueKey = fmt.Sprintf("%s_%d", uniqueKey, fallbackIndex)
		}
	}
	if uniqueKey == "" {
		return feishu.SyncRecord{}, false
	}

	fields := map[string]interface{}{
		"文章标题":   article.Title,
		"唯一键":    uniqueKey,
		"文章ID":   item.MediaID,
		"同步时间":   nowMs(),
		"作者":     article.Author,
		"摘要":     article.Digest,
		"封面素材ID": article.ThumbMediaID,
		"显示封面图":  article.ShowCoverPic,
		"是否已删除":  0,
		"更新时间戳":  item.UpdateTime,
		"文章链接":   map[string]string{"link": article.URL, "text": article.URL},
		"正文来源":   "material_history",
	}

	if msgID := wechat.MessageIDFromArticleURL(article.URL); msgID != "" {
		fields["消息ID"] = msgID
	}
	if idx := wechat.ArticleIndexFromURL(article.URL); idx != nil {
		fields["文章位置"] = *idx
	} else {
		fields["文章位置"] = fallbackIndex
	}

	publishAt := time.Unix(item.UpdateTime, 0).In(wechat.ShanghaiLoc())
	dateStr := publishAt.Format("2006-01-02")
	fields["发布日期"] = publishAt.UnixMilli()
	fields["发布月份"] = toPublishMonth(dateStr)
	fields["更新时间"] = publishAt.UnixMilli()

	if htmlContent := trimForBitable(article.Content, 8000); htmlContent != "" {
		fields["正文HTML"] = htmlContent
	}
	if content := strings.TrimSpace(wechat.CleanHTMLToPlainText(article.Content)); content != "" {
		fields["文章内容"] = trimForBitable(content, 20000)
	}
	if article.ThumbURL != "" {
		fields["封面图链接"] = map[string]string{"link": article.ThumbURL, "text": article.ThumbURL}
	}

	return feishu.SyncRecord{UniqueKey: uniqueKey, Fields: fields}, true
}

func historyWorkerEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ENABLE_HISTORY_WORKER"))
	if v == "" {
		return true
	}
	return v == "1" || strings.EqualFold(v, "true")
}

func historyWorkerInitialDelay() time.Duration {
	return time.Duration(envIntDefault("HISTORY_WORKER_INITIAL_DELAY_SECONDS", 20)) * time.Second
}

func historyWorkerInterval() time.Duration {
	return time.Duration(envIntDefault("HISTORY_WORKER_INTERVAL_MINUTES", 120)) * time.Minute
}

func historyWorkerWritePause() time.Duration {
	return time.Duration(envIntDefault("HISTORY_WORKER_WRITE_PAUSE_MS", 500)) * time.Millisecond
}

func isRetryableBitableDataNotReady(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "1254607") || strings.Contains(msg, "Data not ready")
}

func isRetryableHistoryBackfillError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if isRetryableBitableDataNotReady(err) {
		return true
	}
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "Client.Timeout exceeded") ||
		strings.Contains(msg, "feishu HTTP 429") ||
		strings.Contains(strings.ToLower(msg), "too many requests")
}

func upsertMaterialRecords(records []feishu.SyncRecord, tableID string, existingByKey map[string]string, createdInRun map[string]struct{}) (created int, updated int, err error) {
	if len(records) == 0 {
		return 0, 0, nil
	}

	createList := make([]map[string]interface{}, 0, len(records))
	updateList := make([]map[string]interface{}, 0, len(records))

	for _, record := range records {
		if record.UniqueKey == "" {
			continue
		}

		if recordID, exists := existingByKey[record.UniqueKey]; exists && strings.TrimSpace(recordID) != "" {
			updateList = append(updateList, map[string]interface{}{
				"record_id": recordID,
				"fields":    record.Fields,
			})
			continue
		}

		if _, seen := createdInRun[record.UniqueKey]; seen {
			continue
		}
		createList = append(createList, map[string]interface{}{"fields": record.Fields})
		createdInRun[record.UniqueKey] = struct{}{}
	}

	if len(updateList) > 0 {
		if err := feishu.BatchUpdateByRecordID(tableID, updateList); err != nil {
			return 0, 0, err
		}
		updated = len(updateList)
	}

	if len(createList) > 0 {
		if err := feishu.BatchCreateByRecordFields(tableID, createList); err != nil {
			return 0, 0, err
		}
		created = len(createList)
	}

	return created, updated, nil
}
