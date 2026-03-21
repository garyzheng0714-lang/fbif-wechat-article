package sync

import (
	"fmt"
	"log"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/feishu"
	"github.com/garyzheng0714-lang/fbif-wechat-article/wechat"
)

// articleContentFields defines the extra fields added to 文章主表 for content sync.
var articleContentFields = []feishu.FieldSpec{
	{Name: "文章内容", Type: feishu.FieldTypeText},
}

// ContentSyncResult summarises a content sync run.
type ContentSyncResult struct {
	Total   int `json:"total"`   // articles that needed content
	Updated int `json:"updated"` // successfully fetched and stored
	Failed  int `json:"failed"`  // fetch or write errors
}

// SyncArticleContent fetches plain-text content for articles in 文章主表 that
// have a URL (文章链接) but no content (文章内容) yet.
//
// Images are handled automatically:
//   - If SMMS_API_TOKEN env var is set: permanently uploaded to SM.MS
//   - Otherwise: proxied via wsrv.nl (bypasses WeChat anti-hotlinking, no storage)
//
// A 1-second delay between Jina fetches keeps us within polite rate limits.
func SyncArticleContent() (*ContentSyncResult, error) {
	tableID, err := ArticleMasterTableID()
	if err != nil {
		return nil, fmt.Errorf("get master table: %w", err)
	}

	// Ensure 文章内容 field exists
	if err := feishu.EnsureFieldsExist(articleContentFields, tableID); err != nil {
		return nil, fmt.Errorf("ensure content fields: %w", err)
	}

	articles, err := feishu.GetArticlesNeedingContent(tableID)
	if err != nil {
		return nil, fmt.Errorf("list articles needing content: %w", err)
	}

	result := &ContentSyncResult{Total: len(articles)}
	log.Printf("[ContentSync] %d articles need content fetching", len(articles))

	for i, art := range articles {
		log.Printf("[ContentSync] Fetching %d/%d: %s", i+1, len(articles), art.UniqueKey)

		content, err := wechat.FetchArticleContent(art.ArticleURL)
		if err != nil {
			log.Printf("[ContentSync] Failed to fetch %s: %v", art.UniqueKey, err)
			result.Failed++
			time.Sleep(1 * time.Second)
			continue
		}

		if err := feishu.UpdateRecordFields(tableID, art.RecordID, map[string]interface{}{
			"文章内容": content,
		}); err != nil {
			log.Printf("[ContentSync] Failed to update record %s: %v", art.RecordID, err)
			result.Failed++
		} else {
			result.Updated++
		}

		// Polite rate-limit: Jina Reader is a free public service
		time.Sleep(1 * time.Second)
	}

	log.Printf("[ContentSync] Done: updated=%d failed=%d", result.Updated, result.Failed)
	return result, nil
}
