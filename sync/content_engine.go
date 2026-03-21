package sync

import (
	"fmt"
	"log"
	stdsync "sync"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// articleContentFields defines the extra fields added to 文章主表 for content.
var articleContentFields = []feishu.FieldSpec{
	{Name: "文章内容", Type: feishu.FieldTypeText},
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
//  1. Concurrently fetch all articles (max 3 in-flight at once)
//  2. Collect all results in memory
//  3. Batch-write everything to Feishu in one shot
//
// Images: wsrv.nl proxy by default; SM.MS permanent upload if SMMS_API_TOKEN set.
func SyncArticleContent() (*ContentSyncResult, error) {
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

	// ── Concurrent fetch (max 3 in-flight) ──────────────────────────────────
	type fetched struct {
		recordID string
		content  string
	}

	resultsCh := make(chan fetched, len(articles))
	sem := make(chan struct{}, 3) // concurrency limit

	var wg stdsync.WaitGroup
	for _, art := range articles {
		wg.Add(1)
		go func(a feishu.ArticleForContent) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content, err := wechat.FetchArticleContent(a.ArticleURL)
			if err != nil {
				log.Printf("[ContentSync] Failed %s: %v", a.UniqueKey, err)
				return
			}
			resultsCh <- fetched{a.RecordID, content}
		}(art)
	}
	wg.Wait()
	close(resultsCh)

	// ── Collect ──────────────────────────────────────────────────────────────
	var toUpdate []map[string]interface{}
	for r := range resultsCh {
		toUpdate = append(toUpdate, map[string]interface{}{
			"record_id": r.recordID,
			"fields":    map[string]interface{}{"文章内容": r.content},
		})
	}

	result.Failed = len(articles) - len(toUpdate)
	if len(toUpdate) == 0 {
		log.Printf("[ContentSync] All fetches failed")
		return result, nil
	}

	// ── Batch write ──────────────────────────────────────────────────────────
	log.Printf("[ContentSync] Batch writing %d records to Feishu...", len(toUpdate))
	if err := feishu.BatchUpdateByRecordID(tableID, toUpdate); err != nil {
		return nil, fmt.Errorf("batch write content: %w", err)
	}

	result.Updated = len(toUpdate)
	log.Printf("[ContentSync] Done: total=%d updated=%d failed=%d",
		result.Total, result.Updated, result.Failed)
	return result, nil
}
