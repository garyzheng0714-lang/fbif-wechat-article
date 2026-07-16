package publishcallback

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/garyzheng0714-lang/fbif-wechat-article/autolayout"
)

type ArticleKind string

const (
	ArticleKindNormal  ArticleKind = "normal"
	ArticleKindNewspic ArticleKind = "newspic"
	ArticleKindUnknown ArticleKind = "unknown"
)

type Classification struct {
	Kind         ArticleKind
	ItemShowType int
	AppMsgType   int
	Reason       string
}

type URLClassifier interface {
	Classify(ctx context.Context, articleURL string) (Classification, error)
}

type HTTPPageClassifier struct {
	Client *http.Client
}

var (
	itemShowTypePattern = regexp.MustCompile(`(?i)\bitem_show_type\b\s*(?::|=)\s*["']?\s*([0-9]+)`)
	appMsgTypePattern   = regexp.MustCompile(`(?i)\bappmsg_type\b\s*(?::|=)\s*["']?\s*([0-9]+)`)
)

func (c *HTTPPageClassifier) Classify(ctx context.Context, articleURL string) (Classification, error) {
	if _, err := autolayout.CanonicalSourceKey(articleURL); err != nil {
		return Classification{Kind: ArticleKindUnknown, Reason: "invalid WeChat article URL"}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, articleURL, nil)
	if err != nil {
		return Classification{Kind: ArticleKindUnknown, Reason: "build page request failed"}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; FBIF-WeChat-Publish-Monitor/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	baseClient := c.Client
	if baseClient == nil {
		baseClient = &http.Client{Timeout: 15 * time.Second}
	}
	client := *baseClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if _, err := autolayout.CanonicalSourceKey(req.URL.String()); err != nil {
			return fmt.Errorf("unsafe article redirect: %w", err)
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return Classification{Kind: ArticleKindUnknown, Reason: "fetch article page failed"}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Classification{Kind: ArticleKindUnknown, Reason: fmt.Sprintf("article page HTTP %d", resp.StatusCode)}, fmt.Errorf("article page HTTP %d", resp.StatusCode)
	}
	const pageLimit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, pageLimit+1))
	if err != nil {
		return Classification{Kind: ArticleKindUnknown, Reason: "read article page failed"}, err
	}
	if len(body) > pageLimit {
		return Classification{Kind: ArticleKindUnknown, Reason: "article page exceeds classification limit"}, fmt.Errorf("article page exceeds classification limit")
	}
	return classifyHTML(body), nil
}

func classifyHTML(body []byte) Classification {
	itemShowType, itemOK := uniqueMarkerValue(itemShowTypePattern, body)
	appMsgType, appOK := uniqueMarkerValue(appMsgTypePattern, body)
	classification := Classification{
		Kind:         ArticleKindUnknown,
		ItemShowType: itemShowType,
		AppMsgType:   appMsgType,
	}
	if !itemOK || !appOK {
		classification.Reason = "missing or ambiguous public-page type markers"
		return classification
	}
	switch {
	case itemShowType == 0 && appMsgType == 9:
		classification.Kind = ArticleKindNormal
		classification.Reason = "public page identifies an ordinary long article"
	case itemShowType == 8 && appMsgType == 10002:
		classification.Kind = ArticleKindNewspic
		classification.Reason = "public page identifies a newspic gallery"
	default:
		classification.Reason = fmt.Sprintf("unrecognized public-page markers item_show_type=%d appmsg_type=%d", itemShowType, appMsgType)
	}
	return classification
}

func uniqueMarkerValue(pattern *regexp.Regexp, body []byte) (int, bool) {
	matches := pattern.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return 0, false
	}
	values := make(map[int]struct{})
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(string(match[1])))
		if err != nil {
			return 0, false
		}
		values[value] = struct{}{}
	}
	if len(values) != 1 {
		return 0, false
	}
	for value := range values {
		return value, true
	}
	return 0, false
}
