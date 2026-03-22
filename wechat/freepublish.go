package wechat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const freePublishAPIBase = "https://api.weixin.qq.com/cgi-bin/freepublish"

type ArticleMetadata struct {
	CoverURL    string
	PublishTime time.Time
}

var (
	articleMetaCache   = make(map[string]*ArticleMetadata)
	articleMetaCacheMu sync.Mutex
)

var (
	reMetaOGImage   = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)["']`)
	reMetaTwImage   = regexp.MustCompile(`(?is)<meta[^>]+name=["']twitter:image["'][^>]+content=["']([^"']+)["']`)
	reMsgCDNURL     = regexp.MustCompile(`(?is)(?:var\s+msg_cdn_url\s*=|msg_cdn_url\s*:)\s*["']([^"']+)["']`)
	rePublishCT     = regexp.MustCompile(`(?is)\b(?:var\s+ct\s*=|ct\s*:)\s*["']?(\d{10})["']?`)
	rePublishOGTime = regexp.MustCompile(`(?is)<meta[^>]+property=["']article:published_time["'][^>]+content=["']([^"']+)["']`)
)

func BatchGetPublishedArticles(offset, count int, noReturnContent bool) (*PublishedArticleBatch, error) {
	if err := checkAndIncrementQuota("freepublish_batchget"); err != nil {
		return nil, err
	}

	token, err := GetToken()
	if err != nil {
		return nil, err
	}

	body, _ := json.Marshal(map[string]interface{}{
		"offset":            offset,
		"count":             count,
		"no_return_content": noReturnContent,
	})

	fullURL := fmt.Sprintf("%s/batchget?access_token=%s", freePublishAPIBase, token)
	resp, err := httpClient.Post(fullURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("freepublish batchget: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("freepublish HTTP %d: %s", resp.StatusCode, snippet)
	}

	var result PublishedArticleBatch
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode freepublish response: %w", err)
	}
	if result.ErrCode != 0 {
		return nil, fmt.Errorf("freepublish error %d: %s", result.ErrCode, result.ErrMsg)
	}
	return &result, nil
}

func MessageIDFromArticleURL(articleURL string) string {
	u, err := url.Parse(articleURL)
	if err != nil {
		return ""
	}
	query := u.Query()
	mid := query.Get("mid")
	idx := query.Get("idx")
	if mid == "" || idx == "" {
		return ""
	}
	return mid + "_" + idx
}

func ArticleIndexFromURL(articleURL string) *int {
	u, err := url.Parse(articleURL)
	if err != nil {
		return nil
	}
	idxStr := u.Query().Get("idx")
	if idxStr == "" {
		return nil
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return nil
	}
	return &idx
}

func FetchArticleMetadata(articleURL string) (*ArticleMetadata, error) {
	articleMetaCacheMu.Lock()
	if cached, ok := articleMetaCache[articleURL]; ok {
		articleMetaCacheMu.Unlock()
		return cached, nil
	}
	articleMetaCacheMu.Unlock()

	req, err := http.NewRequest(http.MethodGet, articleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build article metadata request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; fbif-wechat-sync/1.0)")

	resp, err := contentClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch article HTML: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read article HTML: %w", err)
	}
	if resp.StatusCode >= 300 {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("article HTML HTTP %d: %s", resp.StatusCode, snippet)
	}

	meta := parseArticleMetadataHTML(string(body))
	articleMetaCacheMu.Lock()
	articleMetaCache[articleURL] = meta
	articleMetaCacheMu.Unlock()

	return meta, nil
}

func parseArticleMetadataHTML(html string) *ArticleMetadata {
	meta := &ArticleMetadata{}

	for _, re := range []*regexp.Regexp{reMetaOGImage, reMetaTwImage, reMsgCDNURL} {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			meta.CoverURL = strings.TrimSpace(m[1])
			break
		}
	}

	if m := rePublishCT.FindStringSubmatch(html); len(m) > 1 {
		if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			meta.PublishTime = time.Unix(sec, 0)
			return meta
		}
	}

	if m := rePublishOGTime.FindStringSubmatch(html); len(m) > 1 {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(m[1])); err == nil {
			meta.PublishTime = t
		}
	}

	return meta
}
