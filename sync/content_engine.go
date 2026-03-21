package sync

import (
	"fmt"
	"log"
	"time"
	stdsync "sync"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// contentStatus constants match the Bitable single-select field options.
const (
	contentStatusPending  = "pending"
	contentStatusDone     = "done"
	contentStatusFailed  = "failed"
)

// contentSyncMu prevents concurrent content sync runs (synchronisation guard).
var contentSyncMu stdsync.Mutex

// articleContentFields defines the extra fields added to 文章主表 for content.
// Note: content_status is a text field (not single-select) for maximum flexibility
// — no need to pre-define select options in Feishu UI.
var articleContentFields = []feishu.FieldSpec{
	{Name: "文章内容", Type: feishu.FieldTypeText},
	{Name: "content_status", Type: feishu.FieldTypeText},             // pending / done / failed
	{Name: "last_fetch_time", Type: feishu.FieldTypeDatetime},       // Unix ms
	{Name: "fetch_retry_count", Type: feishu.FieldTypeNumber},       // 重试次数
	{Name: "fetch_error", Type: feishu.FieldTypeText},              // 上次错误信息
}

// ContentSyncResult summarises a content sync run.
type ContentSyncResult struct {
	Total   int `json:"total"`   // articles that needed content
	Updated int `json:"updated"` // successfully fetched and batch-written
	Failed  int `json:"failed"`  // fetch errors
}

// SyncArticleContent fetches plain-text content for articles in 文章主表 that
// have 文章链接 but no 文章内容 yet.
//
// Strategy:
//  1. Serialize concurrent runs via mutex (silent skip if already running)
//  2. Find articles with 文章内容 empty (server-side filter via field_names)
//  3. Mark them as content_status="pending" before fetching
//  4. Concurrently fetch content (max 3 in-flight, 200ms between starts)
//  5. Batch-write: done → content_status="done"; failed → content_status="failed" + retry_count++
//
// Images: wsrv.nl proxy by default; SM.MS permanent upload if SMMS_API_TOKEN set.
func SyncArticleContent() (*ContentSyncResult, error) {
	// ── Mutex guard: prevent concurrent runs ────────────────────────────────
	if !contentSyncMu.TryLock() {
		log.Println("[ContentSync] Already running, skipping this invocation")
		return &ContentSyncResult{}, nil
	}
	defer contentSyncMu.Unlock()

	tableID, err := ArticleMasterTableID()
	if err != nil {
		return nil, fmt.Errorf("get master table: %w", err)
	}

	if err := feishu.EnsureFieldsExist(articleContentFields, tableID); err != nil {
		return nil, fmt.Errorf("ensure content fields: %w", err)
	}

	articles, err := feishu.GetArticlesNeedingContent(tableID)
	if err != nil {
		return nil, fmt.Errorf("list articles needing content: %w", err)
	}

	result := &ContentSyncResult{Total: len(articles)}
	if len(articles) == 0 {
		log.Printf("[ContentSync] No articles need content fetching")
		return result, nil
	}
	log.Printf("[ContentSync] Fetching content for %d articles...", len(articles))

	// ── Mark all as pending (prevent double-fetch if handler called again mid-run) ──
	nowMs := time.Now().UnixMilli()
	pendingRecords := make([]map[string]interface{}, 0, len(articles))
	for _, art := range articles {
		pendingRecords = append(pendingRecords, map[string]interface{}{
			"record_id": art.RecordID,
			"fields": map[string]interface{}{
				"content_status":  contentStatusPending,
				"last_fetch_time": nowMs,
			},
		})
	}
	if err := feishu.BatchUpdateByRecordID(tableID, pendingRecords); err != nil {
		log.Printf("[ContentSync] Warning: failed to mark articles as pending: %v", err)
		// Non-fatal: continue anyway
	}

	// ── Concurrent fetch (max 3 in-flight, 200ms start interval) ─────────
	type fetchResult struct {
		recordID   string
		content    string
		fetchError string
	}

	resultsCh := make(chan fetchResult, len(articles))
	sem := make(chan struct{}, 3) // concurrency limit

	var wg stdsync.WaitGroup
	for i, art := range articles {
		wg.Add(1)
		go func(a feishu.ArticleForContent, idx int) {
			defer wg.Done()
			// 200ms interval between goroutine starts (not after each completion)
			if idx > 0 {
				time.Sleep(200 * time.Millisecond)
			}
			sem <- struct{}{}
			defer func() { <-sem }()

			content, err := wechat.FetchArticleContent(a.ArticleURL)
			if err != nil {
				log.Printf("[ContentSync] Failed %s: %v", a.UniqueKey, err)
				resultsCh <- fetchResult{recordID: a.RecordID, fetchError: err.Error()}
				return
			}
			if content == "" {
				// jina.ai returned empty content (article deleted, paywalled, etc.)
				log.Printf("[ContentSync] Empty content for %s (article may be deleted or paywalled)", a.UniqueKey)
				resultsCh <- fetchResult{recordID: a.RecordID, fetchError: "empty content from jina.ai"}
				return
			}
			resultsCh <- fetchResult{recordID: a.RecordID, content: content}
		}(art, i)
	}
	wg.Wait()
	close(resultsCh)

	// ── Collect and batch-write results ────────────────────────────────────
	var doneRecords, failedRecords []map[string]interface{}
	failedCount := 0
	for r := range resultsCh {
		fields := map[string]interface{}{
			"文章内容": r.content,
		}
		if r.fetchError != "" {
			fields["content_status"] = contentStatusFailed
			fields["fetch_error"] = r.fetchError
			failedRecords = append(failedRecords, map[string]interface{}{
				"record_id": r.recordID,
				"fields":    fields,
			})
			failedCount++
		} else {
			fields["content_status"] = contentStatusDone
			doneRecords = append(doneRecords, map[string]interface{}{
				"record_id": r.recordID,
				"fields":    fields,
			})
		}
	}

	result.Failed = failedCount
	result.Updated = len(doneRecords)

	if len(doneRecords) > 0 {
		log.Printf("[ContentSync] Batch writing %d done records to Feishu...", len(doneRecords))
		if err := feishu.BatchUpdateByRecordID(tableID, doneRecords); err != nil {
			return nil, fmt.Errorf("batch write done records: %w", err)
		}
	}

	if len(failedRecords) > 0 {
		log.Printf("[ContentSync] Batch writing %d failed records to Feishu...", len(failedRecords))
		if err := feishu.BatchUpdateByRecordID(tableID, failedRecords); err != nil {
			log.Printf("[ContentSync] Warning: failed to write failed-records: %v", err)
		}
	}

	log.Printf("[ContentSync] Done: total=%d updated=%d failed=%d",
		result.Total, result.Updated, result.Failed)
	return result, nil
}
